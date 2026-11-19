package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/exchange/binance"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

type recordingSender struct {
	mu     sync.Mutex
	routes []transport.EgressRouteID
	paths  []string
}

func (sender *recordingSender) Do(
	ctx context.Context,
	routeID transport.EgressRouteID,
	request *http.Request,
) (*http.Response, error) {
	sender.mu.Lock()
	sender.routes = append(sender.routes, routeID)
	sender.paths = append(sender.paths, request.URL.Path)
	sender.mu.Unlock()
	return http.DefaultClient.Do(request.WithContext(ctx))
}

func (sender *recordingSender) snapshot() ([]transport.EgressRouteID, []string) {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	return append([]transport.EgressRouteID(nil), sender.routes...), append([]string(nil), sender.paths...)
}

type archiveFile struct {
	csv      string
	entry    string
	checksum string
}

func TestKlinesReadsMicrosecondSpotMonthlyFile(t *testing.T) {
	t.Parallel()

	filePath := "/data/spot/monthly/klines/BTCUSDT/1m/BTCUSDT-1m-2025-02.zip"
	server, sender := newArchiveServer(t, map[string]archiveFile{filePath: {csv: strings.Join([]string{
		"1738368000000000,102429.56000000,102431.38000000,102333.00000000,102333.01000000,13.95242000,1738368059999999,1428274.95948110,3208,2.41966000,247693.80458960,0",
		"1738368060000000,102333.01000000,102360.02000000,102328.29000000,102340.67000000,6.13914000,1738368119999999,628315.57433470,1939,3.99198000,408551.32104090,0",
	}, "\n") + "\n"}})
	client := newTestClient(t, server.URL, sender, 0)

	reader, err := client.Klines(context.Background(), KlinesRequest{
		Market: MarketSpot, Symbol: "BTCUSDT", Interval: binance.Kline1Minute, Year: 2025, Month: time.February,
	}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("Klines() error = %v", err)
	}
	defer reader.Close()
	first, err := reader.Next()
	want := binance.Kline{
		OpenTime: 1_738_368_000_000, Open: "102429.56000000", High: "102431.38000000", Low: "102333.00000000",
		Close: "102333.01000000", Volume: "13.95242000", CloseTime: 1_738_368_059_999,
		QuoteAssetVolume: "1428274.95948110", TradeCount: 3208,
		TakerBuyBaseAssetVolume: "2.41966000", TakerBuyQuoteAssetVolume: "247693.80458960",
	}
	if err != nil || first != want {
		t.Fatalf("first kline = %+v, error = %v, want %+v", first, err, want)
	}
	if second, err := reader.Next(); err != nil || second.OpenTime != 1_738_368_060_000 {
		t.Fatalf("second kline = %+v, error = %v", second, err)
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("third Next() error = %v, want EOF", err)
	}
	routes, paths := sender.snapshot()
	if len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" ||
		paths[0] != filePath+".CHECKSUM" || paths[1] != filePath {
		t.Fatalf("requests = %v %v", routes, paths)
	}
}

func TestKlinesReadsNonASCIISymbol(t *testing.T) {
	t.Parallel()

	filePath := "/data/spot/daily/klines/币安人生USDT/1m/币安人生USDT-1m-2026-11-16.zip"
	server, sender := newArchiveServer(t, map[string]archiveFile{filePath: {csv: "1794787200000000,0.1,0.2,0.1,0.2,10,1794787259999999,1.5,3,5,0.75,0\n"}})
	client := newTestClient(t, server.URL, sender, 0)

	reader, err := client.Klines(context.Background(), KlinesRequest{
		Market: MarketSpot, Symbol: "币安人生USDT", Interval: binance.Kline1Minute, Year: 2026, Month: time.November, Day: 16,
	})
	if err != nil {
		t.Fatalf("Klines() error = %v", err)
	}
	defer reader.Close()
	if first, err := reader.Next(); err != nil || first.OpenTime != 1_794_787_200_000 || first.TradeCount != 3 {
		t.Fatalf("first kline = %+v, error = %v", first, err)
	}
	if _, paths := sender.snapshot(); len(paths) != 2 || paths[1] != filePath {
		t.Fatalf("requests = %v", paths)
	}
}

func TestKlinesSkipsUSDMDailyHeader(t *testing.T) {
	t.Parallel()

	filePath := "/data/futures/um/daily/klines/BTCUSDT/1m/BTCUSDT-1m-2026-11-16.zip"
	server, sender := newArchiveServer(t, map[string]archiveFile{filePath: {csv: "open_time,open,high,low,close,volume,close_time,quote_volume,count,taker_buy_volume,taker_buy_quote_volume,ignore\n" +
		"1794787200000,65309.80,65317.40,65269.50,65300.00,90.483,1794787259999,5908508.84380,3567,33.761,2204594.05640,0\n"}})
	client := newTestClient(t, server.URL, sender, 0)

	reader, err := client.Klines(context.Background(), KlinesRequest{
		Market: MarketUSDM, Symbol: "BTCUSDT", Interval: binance.Kline1Minute,
		Year: 2026, Month: time.November, Day: 16,
	})
	if err != nil {
		t.Fatalf("Klines() error = %v", err)
	}
	defer reader.Close()
	kline, err := reader.Next()
	if err != nil || kline.OpenTime != 1_794_787_200_000 || kline.Close != "65300.00" || kline.TradeCount != 3567 {
		t.Fatalf("kline = %+v, error = %v", kline, err)
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("Next() error = %v, want EOF", err)
	}
}

func TestKlinesErrors(t *testing.T) {
	t.Parallel()

	row := "1502942400000,4261.48,4261.48,4261.48,4261.48,1.775183,1502942459999,7564.90685084,3,0.075183,320.39085084,7960.54017996\n"
	files := map[string]archiveFile{
		"/data/spot/monthly/klines/BTCUSDT/1m/BTCUSDT-1m-2017-08.zip": {csv: row, checksum: strings.Repeat("0", 64)},
		"/data/spot/monthly/klines/BTCUSDT/1m/BTCUSDT-1m-2017-09.zip": {csv: row + "1502942460000,1e3,1,1,1,1,1502942519999,1,1,1,1,0\n"},
		"/data/spot/monthly/klines/BTCUSDT/1m/BTCUSDT-1m-2017-10.zip": {csv: row, entry: "other.csv"},
		"/data/spot/monthly/klines/BTCUSDT/1m/BTCUSDT-1m-2017-11.zip": {csv: strings.Repeat(row, 200)},
	}
	server, sender := newArchiveServer(t, files)
	client := newTestClient(t, server.URL, sender, 0)
	ctx := context.Background()
	month := func(value time.Month) KlinesRequest {
		return KlinesRequest{Market: MarketSpot, Symbol: "BTCUSDT", Interval: binance.Kline1Minute, Year: 2017, Month: value}
	}

	if _, err := client.Klines(ctx, month(time.July)); !errors.Is(err, ErrFileNotFound) ||
		!errors.Is(err, trade.ErrUnsupportedCapability) {
		t.Fatalf("missing file error = %v", err)
	}
	if _, err := client.Klines(ctx, month(time.August)); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("checksum error = %v", err)
	}
	reader, err := client.Klines(ctx, month(time.September))
	if err != nil {
		t.Fatalf("Klines(September) error = %v", err)
	}
	if _, err := reader.Next(); err != nil {
		t.Fatalf("first row error = %v", err)
	}
	if _, err := reader.Next(); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("scientific notation error = %v", err)
	}
	_ = reader.Close()
	if _, err := client.Klines(ctx, month(time.October)); !strings.Contains(causeText(err), "zip must contain") {
		t.Fatalf("unexpected entry error = %v", err)
	}
	small := newTestClient(t, server.URL, sender, 64)
	if _, err := small.Klines(ctx, month(time.November)); !strings.Contains(causeText(err), "exceeds 64 bytes") {
		t.Fatalf("size limit error = %v", err)
	}

	_, before := sender.snapshot()
	for name, request := range map[string]KlinesRequest{
		"market":      {Market: "options", Symbol: "BTCUSDT", Interval: binance.Kline1Minute, Year: 2024, Month: 1},
		"symbol":      {Market: MarketSpot, Symbol: "btc-usdt", Interval: binance.Kline1Minute, Year: 2024, Month: 1},
		"path symbol": {Market: MarketSpot, Symbol: "BTC/../ETH", Interval: binance.Kline1Minute, Year: 2024, Month: 1},
		"escape":      {Market: MarketSpot, Symbol: "BTC%2FETH", Interval: binance.Kline1Minute, Year: 2024, Month: 1},
		"interval":    {Market: MarketSpot, Symbol: "BTCUSDT", Interval: "7m", Year: 2024, Month: 1},
		"usdm 1s":     {Market: MarketUSDM, Symbol: "BTCUSDT", Interval: binance.Kline1Second, Year: 2024, Month: 1},
		"year":        {Market: MarketSpot, Symbol: "BTCUSDT", Interval: binance.Kline1Minute, Year: 2016, Month: 1},
		"month":       {Market: MarketSpot, Symbol: "BTCUSDT", Interval: binance.Kline1Minute, Year: 2024, Month: 13},
		"february 30": {Market: MarketSpot, Symbol: "BTCUSDT", Interval: binance.Kline1Minute, Year: 2024, Month: 2, Day: 30},
		"negative":    {Market: MarketSpot, Symbol: "BTCUSDT", Interval: binance.Kline1Minute, Year: 2024, Month: 2, Day: -1},
		"daily 3d":    {Market: MarketUSDM, Symbol: "BTCUSDT", Interval: binance.Kline3Days, Year: 2024, Month: 1, Day: 5},
		"daily 1w":    {Market: MarketSpot, Symbol: "BTCUSDT", Interval: binance.Kline1Week, Year: 2024, Month: 1, Day: 5},
		"daily 1mo":   {Market: MarketSpot, Symbol: "BTCUSDT", Interval: binance.Kline1Month, Year: 2024, Month: 1, Day: 5},
	} {
		if _, err := client.Klines(ctx, request); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s error = %v, want ErrValidation", name, err)
		}
	}
	if _, after := sender.snapshot(); len(after) != len(before) {
		t.Fatalf("validation errors reached the network: %v", after[len(before):])
	}
}

func TestKlinesRequestPaths(t *testing.T) {
	t.Parallel()

	tests := map[string]KlinesRequest{
		"data/spot/monthly/klines/ETHBTC/1mo/ETHBTC-1mo-2024-01.zip": {
			Market: MarketSpot, Symbol: "ETHBTC", Interval: binance.Kline1Month, Year: 2024, Month: time.January,
		},
		"data/spot/daily/klines/BTCUSDT/1s/BTCUSDT-1s-2024-02-29.zip": {
			Market: MarketSpot, Symbol: "BTCUSDT", Interval: binance.Kline1Second, Year: 2024, Month: time.February, Day: 29,
		},
		"data/futures/um/monthly/klines/BTCUSDT/3d/BTCUSDT-3d-2024-01.zip": {
			Market: MarketUSDM, Symbol: "BTCUSDT", Interval: binance.Kline3Days, Year: 2024, Month: time.January,
		},
		"data/spot/monthly/klines/BTCUSDT/1w/BTCUSDT-1w-2024-01.zip": {
			Market: MarketSpot, Symbol: "BTCUSDT", Interval: binance.Kline1Week, Year: 2024, Month: time.January,
		},
		"data/futures/um/daily/klines/BTCUSDT/1d/BTCUSDT-1d-2024-01-05.zip": {
			Market: MarketUSDM, Symbol: "BTCUSDT", Interval: binance.Kline1Day, Year: 2024, Month: time.January, Day: 5,
		},
	}
	for want, request := range tests {
		if got, err := request.path(); err != nil || got != want {
			t.Fatalf("path() = %q, %v, want %q", got, err, want)
		}
	}
}

func TestParseChecksum(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte("x"))
	encoded := hex.EncodeToString(sum[:])
	if got, err := parseChecksum([]byte(encoded+"  A.zip\n"), "A.zip"); err != nil || got != sum {
		t.Fatalf("parseChecksum() = %x, %v", got, err)
	}
	for _, content := range []string{encoded + "  B.zip", encoded[:10] + "  A.zip", "", encoded + " A.zip extra"} {
		if _, err := parseChecksum([]byte(content), "A.zip"); err == nil {
			t.Fatalf("parseChecksum(%q) error = nil", content)
		}
	}
}

func newArchiveServer(t *testing.T, files map[string]archiveFile) (*httptest.Server, *recordingSender) {
	t.Helper()
	bodies := make(map[string][]byte, len(files)*2)
	for filePath, file := range files {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		entry := file.entry
		if entry == "" {
			entry = strings.TrimSuffix(path.Base(filePath), ".zip") + ".csv"
		}
		content, err := writer.Create(entry)
		if err != nil {
			t.Fatalf("zip create error = %v", err)
		}
		if _, err := content.Write([]byte(file.csv)); err != nil {
			t.Fatalf("zip write error = %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("zip close error = %v", err)
		}
		checksum := file.checksum
		if checksum == "" {
			sum := sha256.Sum256(buffer.Bytes())
			checksum = hex.EncodeToString(sum[:])
		}
		bodies[filePath] = buffer.Bytes()
		bodies[filePath+".CHECKSUM"] = []byte(checksum + "  " + path.Base(filePath) + "\n")
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, exists := bodies[request.URL.Path]
		if !exists {
			http.Error(writer, "<Error><Code>NoSuchKey</Code></Error>", http.StatusNotFound)
			return
		}
		_, _ = writer.Write(body)
	}))
	t.Cleanup(server.Close)
	return server, &recordingSender{}
}

func newTestClient(t *testing.T, baseURL string, sender commonexchange.Sender, maxFileBytes int64) *Client {
	t.Helper()
	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{
		Sender: sender, Limiter: limiter, MaxResponseBodyBytes: 16,
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	client, err := New(Config{
		Executor: executor, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true, MaxFileBytes: maxFileBytes,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

// causeText는 민감 정보를 숨기는 trade.APIError 문자열 대신 원인 문자열을 반환한다.
func causeText(err error) string {
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.Cause == nil {
		return ""
	}
	return apiError.Cause.Error()
}
