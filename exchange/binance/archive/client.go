// Package archive는 Binance 공개 데이터 저장소(data.binance.vision)의 캔들·체결 덤프를 제공한다.
// REST로 상장 시점부터 받으면 weight 한도 때문에 오래 걸리는 초기 백필을 월·일 파일로 줄인다.
package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	DefaultBaseURL       = "https://data.binance.vision"
	DefaultHeaderTimeout = 30 * time.Second
	// DefaultMaxFileBytes는 압축 파일 하나의 기본 최대 크기다. 1분봉 월 파일은 수 MB이고
	// 인기 상품의 체결 월 파일은 1GB를 넘으므로 이 파일은 MaxFileBytes를 올려 받는다.
	DefaultMaxFileBytes int64 = 256 << 20
	maxChecksumBytes          = 4 << 10
)

var (
	// ErrFileNotFound는 요청한 파일이 저장소에 없음을 나타낸다.
	// 상장 이전 구간, 아직 게시되지 않은 월·일, 상장 폐지 이후 구간에서 발생한다.
	ErrFileNotFound = errors.New("Binance archive file not found")
	// ErrChecksumMismatch는 내려받은 파일의 SHA-256이 .CHECKSUM 파일과 다름을 나타낸다.
	ErrChecksumMismatch = errors.New("Binance archive checksum mismatch")
)

// Config는 Binance 공개 데이터 클라이언트 설정이다.
type Config struct {
	Executor             *commonexchange.Executor
	DefaultEgressRouteID transport.EgressRouteID
	BaseURL              string
	AllowInsecureHTTP    bool
	// HeaderTimeout은 요청마다 응답 헤더 수신까지의 기본 제한 시간이다. 본문 수신 시간은 context로 제어한다.
	HeaderTimeout time.Duration
	// MaxFileBytes는 압축 파일 하나의 최대 크기다. 0이면 DefaultMaxFileBytes를 사용한다.
	MaxFileBytes int64
}

// Client는 요청별 송신 경로를 유지하며 체크섬을 검증한 덤프 파일을 읽는다.
type Client struct {
	executor             *commonexchange.Executor
	defaultEgressRouteID transport.EgressRouteID
	baseURL              *url.URL
	headerTimeout        time.Duration
	maxFileBytes         int64
}

// New는 검증된 Binance 공개 데이터 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("Binance archive executor is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || parsedBaseURL.Host == "" || parsedBaseURL.User != nil ||
		parsedBaseURL.RawQuery != "" || parsedBaseURL.Fragment != "" ||
		(parsedBaseURL.Path != "" && parsedBaseURL.Path != "/") {
		return nil, fmt.Errorf("invalid Binance archive base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("Binance archive base URL must use HTTPS")
	}
	parsedBaseURL.Path = ""
	if config.HeaderTimeout == 0 {
		config.HeaderTimeout = DefaultHeaderTimeout
	}
	if config.HeaderTimeout < 0 {
		return nil, fmt.Errorf("Binance archive header timeout cannot be negative")
	}
	if config.MaxFileBytes == 0 {
		config.MaxFileBytes = DefaultMaxFileBytes
	}
	if config.MaxFileBytes < 0 {
		return nil, fmt.Errorf("Binance archive maximum file size cannot be negative")
	}
	return &Client{
		executor: config.Executor, defaultEgressRouteID: defaultRouteID,
		baseURL: parsedBaseURL, headerTimeout: config.HeaderTimeout, maxFileBytes: config.MaxFileBytes,
	}, nil
}

// Klines는 캔들 덤프 파일 하나를 내려받아 SHA-256을 검증한 뒤 행 단위 reader를 반환한다.
// 체크섬 파일이 없거나 덤프가 없으면 ErrFileNotFound를 반환한다.
// zip은 중앙 디렉터리가 파일 끝에 있고 검증 전에는 내용을 신뢰할 수 없으므로
// 압축 파일은 메모리에 받고, 압축 해제와 CSV 해석은 Next 호출마다 진행한다.
func (client *Client) Klines(
	ctx context.Context,
	request KlinesRequest,
	options ...trade.RequestOption,
) (*KlineReader, error) {
	filePath, err := request.path()
	if err != nil {
		return nil, err
	}
	return client.openKlines(ctx, filePath, false, options)
}

// MarkPriceKlines는 USDⓈ-M 마크가 캔들 덤프 파일 하나를 읽는다.
// 검증과 오류는 Klines와 같다.
func (client *Client) MarkPriceKlines(
	ctx context.Context,
	request MarkPriceKlinesRequest,
	options ...trade.RequestOption,
) (*KlineReader, error) {
	filePath, err := request.path()
	if err != nil {
		return nil, err
	}
	return client.openKlines(ctx, filePath, false, options)
}

// IndexPriceKlines는 USDⓈ-M 지수가 캔들 덤프 파일 하나를 읽는다.
// 검증과 오류는 Klines와 같다.
func (client *Client) IndexPriceKlines(
	ctx context.Context,
	request IndexPriceKlinesRequest,
	options ...trade.RequestOption,
) (*KlineReader, error) {
	filePath, err := request.path()
	if err != nil {
		return nil, err
	}
	return client.openKlines(ctx, filePath, false, options)
}

// PremiumIndexKlines는 USDⓈ-M 프리미엄 지수 캔들 덤프 파일 하나를 읽는다.
// 프리미엄 지수는 선물이 지수보다 싸면 음수이므로 시가·고가·저가·종가의 음수를 허용한다.
// 검증과 오류는 Klines와 같다.
func (client *Client) PremiumIndexKlines(
	ctx context.Context,
	request PremiumIndexKlinesRequest,
	options ...trade.RequestOption,
) (*KlineReader, error) {
	filePath, err := request.path()
	if err != nil {
		return nil, err
	}
	return client.openKlines(ctx, filePath, true, options)
}

func (client *Client) openKlines(
	ctx context.Context,
	filePath string,
	signedPrices bool,
	options []trade.RequestOption,
) (*KlineReader, error) {
	entry, err := client.open(ctx, filePath, options)
	if err != nil {
		return nil, err
	}
	return newKlineReader(filePath, entry, signedPrices), nil
}

// open은 덤프 파일 하나의 SHA-256을 .CHECKSUM 파일로 검증한 뒤 zip 안의 CSV 항목을 연다.
// 체크섬 파일이 없거나 덤프가 없으면 ErrFileNotFound를 반환한다.
// zip은 중앙 디렉터리가 파일 끝에 있고 검증 전에는 내용을 신뢰할 수 없으므로
// 압축 파일은 메모리에 받고, 압축 해제와 CSV 해석은 읽는 만큼만 진행한다.
func (client *Client) open(
	ctx context.Context,
	filePath string,
	options []trade.RequestOption,
) (io.ReadCloser, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultEgressRouteID, options...)
	if err != nil {
		return nil, err
	}
	if resolved.Timeout == 0 {
		resolved.Timeout = client.headerTimeout
	}
	checksum, err := client.download(ctx, resolved, filePath+".CHECKSUM", maxChecksumBytes)
	if err != nil {
		return nil, err
	}
	want, err := parseChecksum(checksum, path.Base(filePath))
	if err != nil {
		return nil, archiveError(filePath, true, err)
	}
	data, err := client.download(ctx, resolved, filePath, client.maxFileBytes)
	if err != nil {
		return nil, err
	}
	if got := sha256.Sum256(data); got != want {
		return nil, archiveError(filePath, true, fmt.Errorf(
			"%w: got %s, want %s", ErrChecksumMismatch, hex.EncodeToString(got[:]), hex.EncodeToString(want[:]),
		))
	}
	return openEntry(filePath, data)
}

func (client *Client) download(
	ctx context.Context,
	resolved trade.RequestOptions,
	filePath string,
	limit int64,
) ([]byte, error) {
	requestURL := *client.baseURL
	requestURL.Path = "/" + filePath
	response, err := client.executor.Stream(ctx, commonexchange.Execution{
		Exchange: model.ExchangeBinance, EndpointID: "GET archive",
		EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout,
		Operation: commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			request, requestErr := http.NewRequest(http.MethodGet, requestURL.String(), nil)
			if requestErr != nil {
				return nil, fmt.Errorf("create Binance archive request: %w", requestErr)
			}
			request.Header.Set("User-Agent", "cex-sdk-go/0")
			return request, nil
		},
	})
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, statusError(response.StatusCode, filePath)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, archiveError(filePath, true, fmt.Errorf("read body: %w", err))
	}
	if int64(len(data)) > limit {
		return nil, archiveError(filePath, false, fmt.Errorf("file exceeds %d bytes", limit))
	}
	return data, nil
}

// parseChecksum은 sha256sum 형식 "<hex>  <파일명>" 한 줄을 해석한다.
func parseChecksum(data []byte, fileName string) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	fields := strings.Fields(string(data))
	if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != fileName {
		return sum, fmt.Errorf("unexpected checksum file content")
	}
	decoded, err := hex.DecodeString(fields[0])
	if err != nil || len(decoded) != sha256.Size {
		return sum, fmt.Errorf("invalid SHA-256 checksum %q", fields[0])
	}
	copy(sum[:], decoded)
	return sum, nil
}

// openEntry는 검증을 마친 zip에서 덤프 CSV 항목 하나를 연다.
func openEntry(filePath string, data []byte) (io.ReadCloser, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, archiveError(filePath, false, fmt.Errorf("open zip: %w", err))
	}
	entryName := strings.TrimSuffix(path.Base(filePath), ".zip") + ".csv"
	if len(archive.File) != 1 || archive.File[0].Name != entryName {
		return nil, archiveError(filePath, false, fmt.Errorf("zip must contain only %s", entryName))
	}
	entry, err := archive.File[0].Open()
	if err != nil {
		return nil, archiveError(filePath, false, fmt.Errorf("open %s: %w", entryName, err))
	}
	return entry, nil
}

func statusError(status int, filePath string) error {
	apiError := &trade.APIError{
		Category: trade.ErrorExchangeUnavailable, Exchange: model.ExchangeBinance, HTTPStatus: status,
		Cause: fmt.Errorf("Binance archive %s returned HTTP %d", filePath, status),
	}
	switch {
	case status == http.StatusNotFound:
		apiError.Category = trade.ErrorUnsupportedCapability
		apiError.Cause = fmt.Errorf("%w: %s", ErrFileNotFound, strings.TrimSuffix(filePath, ".CHECKSUM"))
	case status == http.StatusTooManyRequests:
		apiError.Category, apiError.Retryable = trade.ErrorRateLimited, true
	case status >= http.StatusInternalServerError:
		apiError.Retryable = true
	}
	return apiError
}

func archiveError(filePath string, retryable bool, cause error) error {
	return &trade.APIError{
		Category: trade.ErrorExchangeUnavailable, Exchange: model.ExchangeBinance, Retryable: retryable,
		Cause: fmt.Errorf("Binance archive %s: %w", filePath, cause),
	}
}
