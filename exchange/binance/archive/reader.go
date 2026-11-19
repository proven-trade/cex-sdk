package archive

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/proven-trade/cex-sdk/exchange/binance"
)

const (
	klineFields = 12
	// microsecondThreshold보다 큰 시각은 microsecond로 본다. 2025년 이후 Spot 파일이 microsecond를 쓴다.
	// millisecond 시각은 서기 5138년까지 이 값보다 작다.
	microsecondThreshold int64 = 100_000_000_000_000
)

var (
	decimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
	// signedDecimalPattern은 음수가 가능한 열을 받는다. 프리미엄 지수는 선물이 지수보다 싸면 음수다.
	signedDecimalPattern = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)
)

// csvStream은 압축을 푼 CSV 항목을 한 줄씩 읽는 공통 구현이다.
type csvStream struct {
	path   string
	entry  io.ReadCloser
	reader *csv.Reader
	// header는 헤더 행의 첫 열 이름이다. 최근 USDⓈ-M 파일만 첫 줄이 헤더이고 Spot 파일은 헤더가 없다.
	header string
	first  bool
}

func newCSVStream(filePath string, entry io.ReadCloser, fields int, header string) *csvStream {
	reader := csv.NewReader(entry)
	reader.FieldsPerRecord = fields
	reader.ReuseRecord = true
	return &csvStream{path: filePath, entry: entry, reader: reader, header: header, first: true}
}

// next는 다음 데이터 행을 반환하고 파일 끝에서 io.EOF를 반환한다. 헤더 행은 건너뛴다.
// 반환한 slice는 다음 호출에서 재사용되므로 호출자는 값을 복사해 보관한다.
func (stream *csvStream) next() ([]string, error) {
	for {
		record, err := stream.reader.Read()
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		if err != nil {
			// 읽기 실패는 줄 정보가 없을 수 있어 csv 오류에 담긴 줄 번호를 그대로 쓴다.
			return nil, fmt.Errorf("read Binance archive %s: %w", stream.path, err)
		}
		if stream.first {
			stream.first = false
			if record[0] == stream.header {
				continue
			}
		}
		return record, nil
	}
}

// recordError는 방금 읽은 행의 해석 오류에 파일 경로와 줄 번호를 붙인다.
func (stream *csvStream) recordError(err error) error {
	line, _ := stream.reader.FieldPos(0)
	return fmt.Errorf("decode Binance archive %s line %d: %w", stream.path, line, err)
}

func (stream *csvStream) close() error {
	return stream.entry.Close()
}

// KlineReader는 캔들 덤프를 파일 순서(시간 오름차순)대로 한 줄씩 읽는다.
// 반환하는 시각은 파일 단위와 관계없이 REST와 같은 Unix millisecond다.
// 마크가·지수가·프리미엄 지수 캔들 덤프도 같은 열 구성이며 거래량·거래대금·taker 열은 0이다.
type KlineReader struct {
	stream *csvStream
	// signedPrices는 프리미엄 지수 캔들처럼 시가·고가·저가·종가가 음수일 수 있는 파일이면 true다.
	signedPrices bool
}

func newKlineReader(filePath string, entry io.ReadCloser, signedPrices bool) *KlineReader {
	return &KlineReader{
		stream:       newCSVStream(filePath, entry, klineFields, "open_time"),
		signedPrices: signedPrices,
	}
}

// Next는 다음 캔들을 반환하고 파일 끝에서 io.EOF를 반환한다.
// 최근 USDⓈ-M 파일의 헤더 행은 건너뛴다.
func (reader *KlineReader) Next() (binance.Kline, error) {
	record, err := reader.stream.next()
	if err != nil {
		return binance.Kline{}, err
	}
	kline, err := parseKline(record, reader.signedPrices)
	if err != nil {
		return binance.Kline{}, reader.stream.recordError(err)
	}
	return kline, nil
}

// Close는 압축 해제 스트림을 닫는다.
func (reader *KlineReader) Close() error {
	return reader.stream.close()
}

// parseKline은 open_time, open, high, low, close, volume, close_time, quote_volume,
// count, taker_buy_volume, taker_buy_quote_volume, ignore 열을 해석한다.
// signedPrices가 true이면 open, high, low, close 열의 음수를 허용한다.
func parseKline(record []string, signedPrices bool) (binance.Kline, error) {
	openTime, err := parseMillis("open time", record[0])
	if err != nil {
		return binance.Kline{}, err
	}
	closeTime, err := parseMillis("close time", record[6])
	if err != nil {
		return binance.Kline{}, err
	}
	pricePattern := decimalPattern
	if signedPrices {
		pricePattern = signedDecimalPattern
	}
	if err := checkDecimals(record, pricePattern, 1, 2, 3, 4); err != nil {
		return binance.Kline{}, err
	}
	if err := checkDecimals(record, decimalPattern, 5, 7, 9, 10); err != nil {
		return binance.Kline{}, err
	}
	count, err := parseInteger("trade count", record[8])
	if err != nil {
		return binance.Kline{}, err
	}
	return binance.Kline{
		OpenTime: openTime, Open: record[1], High: record[2], Low: record[3], Close: record[4],
		Volume: record[5], CloseTime: closeTime, QuoteAssetVolume: record[7], TradeCount: count,
		TakerBuyBaseAssetVolume: record[9], TakerBuyQuoteAssetVolume: record[10],
	}, nil
}

// checkDecimals는 지정한 열이 pattern에 맞는 십진 문자열인지 확인한다.
func checkDecimals(record []string, pattern *regexp.Regexp, columns ...int) error {
	for _, index := range columns {
		if !pattern.MatchString(record[index]) {
			return fmt.Errorf("invalid decimal %q in column %d", record[index], index+1)
		}
	}
	return nil
}

// parseInteger는 0 이상의 정수 열을 해석한다.
func parseInteger(name, value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid %s %q", name, value)
	}
	return parsed, nil
}

// parseMillis는 Unix 시각 열을 millisecond로 해석한다. microsecond 시각은 millisecond로 내린다.
func parseMillis(name, value string) (int64, error) {
	parsed, err := parseInteger(name, value)
	if err != nil {
		return 0, err
	}
	if parsed >= microsecondThreshold {
		return parsed / 1000, nil
	}
	return parsed, nil
}

// parseBool은 true·false 열을 해석한다. Spot 파일은 True·False, USDⓈ-M 파일은 true·false로 기록한다.
func parseBool(name, value string) (bool, error) {
	if strings.EqualFold(value, "true") {
		return true, nil
	}
	if strings.EqualFold(value, "false") {
		return false, nil
	}
	return false, fmt.Errorf("invalid %s %q", name, value)
}
