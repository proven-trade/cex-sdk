package archive

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"

	"github.com/proven-trade/cex-sdk/exchange/binance"
)

const (
	klineFields = 12
	// microsecondThreshold보다 큰 시각은 microsecond로 본다. 2025년 이후 Spot 파일이 microsecond를 쓴다.
	// millisecond 시각은 서기 5138년까지 이 값보다 작다.
	microsecondThreshold int64 = 100_000_000_000_000
)

var decimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// KlineReader는 캔들 덤프를 파일 순서(시간 오름차순)대로 한 줄씩 읽는다.
// 반환하는 시각은 파일 단위와 관계없이 REST와 같은 Unix millisecond다.
type KlineReader struct {
	path   string
	entry  io.ReadCloser
	reader *csv.Reader
	first  bool
}

func newKlineReader(filePath string, entry io.ReadCloser) *KlineReader {
	reader := csv.NewReader(entry)
	reader.FieldsPerRecord = klineFields
	reader.ReuseRecord = true
	return &KlineReader{path: filePath, entry: entry, reader: reader, first: true}
}

// Next는 다음 캔들을 반환하고 파일 끝에서 io.EOF를 반환한다.
// 최근 USDⓈ-M 파일의 헤더 행은 건너뛴다.
func (reader *KlineReader) Next() (binance.Kline, error) {
	for {
		record, err := reader.reader.Read()
		if errors.Is(err, io.EOF) {
			return binance.Kline{}, io.EOF
		}
		if err != nil {
			return binance.Kline{}, reader.recordError(err)
		}
		if reader.first {
			reader.first = false
			if record[0] == "open_time" {
				continue
			}
		}
		kline, err := parseKline(record)
		if err != nil {
			return binance.Kline{}, reader.recordError(err)
		}
		return kline, nil
	}
}

// Close는 압축 해제 스트림을 닫는다.
func (reader *KlineReader) Close() error {
	return reader.entry.Close()
}

func (reader *KlineReader) recordError(err error) error {
	line, _ := reader.reader.FieldPos(0)
	return fmt.Errorf("decode Binance archive %s line %d: %w", reader.path, line, err)
}

// parseKline은 open_time, open, high, low, close, volume, close_time, quote_volume,
// count, taker_buy_volume, taker_buy_quote_volume, ignore 열을 해석한다.
func parseKline(record []string) (binance.Kline, error) {
	openTime, err := parseMillis("open time", record[0])
	if err != nil {
		return binance.Kline{}, err
	}
	closeTime, err := parseMillis("close time", record[6])
	if err != nil {
		return binance.Kline{}, err
	}
	for _, index := range []int{1, 2, 3, 4, 5, 7, 9, 10} {
		if !decimalPattern.MatchString(record[index]) {
			return binance.Kline{}, fmt.Errorf("invalid decimal %q in column %d", record[index], index+1)
		}
	}
	count, err := strconv.ParseInt(record[8], 10, 64)
	if err != nil || count < 0 {
		return binance.Kline{}, fmt.Errorf("invalid trade count %q", record[8])
	}
	return binance.Kline{
		OpenTime: openTime, Open: record[1], High: record[2], Low: record[3], Close: record[4],
		Volume: record[5], CloseTime: closeTime, QuoteAssetVolume: record[7], TradeCount: count,
		TakerBuyBaseAssetVolume: record[9], TakerBuyQuoteAssetVolume: record[10],
	}, nil
}

func parseMillis(name, value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid %s %q", name, value)
	}
	if parsed >= microsecondThreshold {
		return parsed / 1000, nil
	}
	return parsed, nil
}
