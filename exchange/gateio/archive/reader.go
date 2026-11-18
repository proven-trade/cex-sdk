package archive

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/unified"
)

var decimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// Trade는 체결 덤프 한 줄이다.
type Trade struct {
	ID string
	// Time은 원문 Unix 초 decimal이며 microsecond까지 보존한다.
	Time      string
	Timestamp int64
	Price     string
	// Amount는 Spot에서 기준 통화 수량, Futures에서 계약 수의 절댓값이다.
	Amount string
	Side   unified.Side
}

// Unified는 캔들 합성에 사용할 공통 체결로 변환한다.
func (item Trade) Unified() unified.PublicTrade {
	return unified.PublicTrade{
		ID: item.ID, Price: item.Price, Quantity: item.Amount,
		Side: item.Side, Timestamp: item.Timestamp,
	}
}

// Candle은 캔들 덤프 한 줄이다. StartTime은 Unix millisecond다.
// Volume은 Spot에서 기준 통화 거래량, Futures에서 계약 수다. Spot 덤프의 거래량은
// 저장소가 부동소수점으로 기록해 6.137529907399998처럼 원문 자릿수를 그대로 보존한다.
type Candle struct {
	StartTime int64
	Open      string
	High      string
	Low       string
	Close     string
	Volume    string
}

// TradeReader는 체결 덤프를 한 줄씩 읽는다. 파일 순서를 그대로 반환하며
// 저장소의 오래된 파일은 최신 체결부터, 최근 파일은 오래된 체결부터 기록돼 있다.
type TradeReader struct {
	stream *csvStream
	market Market
}

// Next는 다음 체결을 반환하고 파일 끝에서 io.EOF를 반환한다.
func (reader *TradeReader) Next() (Trade, error) {
	record, err := reader.stream.next()
	if err != nil {
		return Trade{}, err
	}
	item, err := parseTrade(reader.market, record)
	if err != nil {
		return Trade{}, reader.stream.recordError(err)
	}
	return item, nil
}

// Close는 압축 해제 스트림과 HTTP 본문을 닫는다.
func (reader *TradeReader) Close() error {
	return reader.stream.close()
}

// CandleReader는 캔들 덤프를 시간 오름차순 파일 순서대로 한 줄씩 읽는다.
type CandleReader struct {
	stream *csvStream
}

// Next는 다음 캔들을 반환하고 파일 끝에서 io.EOF를 반환한다.
func (reader *CandleReader) Next() (Candle, error) {
	record, err := reader.stream.next()
	if err != nil {
		return Candle{}, err
	}
	item, err := parseCandle(record)
	if err != nil {
		return Candle{}, reader.stream.recordError(err)
	}
	return item, nil
}

// Close는 압축 해제 스트림과 HTTP 본문을 닫는다.
func (reader *CandleReader) Close() error {
	return reader.stream.close()
}

// Trades는 한 달치 체결 덤프를 연다. 반환된 reader는 호출자가 닫아야 한다.
func (client *Client) Trades(
	ctx context.Context,
	request TradesRequest,
	options ...trade.RequestOption,
) (*TradeReader, error) {
	path, err := request.path()
	if err != nil {
		return nil, err
	}
	fields := 4
	if request.Market == MarketSpot {
		fields = 5
	}
	stream, err := client.open(ctx, path, fields, options...)
	if err != nil {
		return nil, err
	}
	return &TradeReader{stream: stream, market: request.Market}, nil
}

// Candles는 한 달치 캔들 덤프를 연다. 반환된 reader는 호출자가 닫아야 한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) (*CandleReader, error) {
	path, err := request.path()
	if err != nil {
		return nil, err
	}
	stream, err := client.open(ctx, path, 6, options...)
	if err != nil {
		return nil, err
	}
	return &CandleReader{stream: stream}, nil
}

type csvStream struct {
	path         string
	body         io.Closer
	decompressed *gzip.Reader
	reader       *csv.Reader
}

func (stream *csvStream) next() ([]string, error) {
	record, err := stream.reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, io.EOF
	}
	if err != nil {
		return nil, stream.recordError(err)
	}
	return record, nil
}

func (stream *csvStream) recordError(err error) error {
	line, _ := stream.reader.FieldPos(0)
	return fmt.Errorf("decode Gate.io archive %s line %d: %w", stream.path, line, err)
}

func (stream *csvStream) close() error {
	decompressErr := stream.decompressed.Close()
	bodyErr := stream.body.Close()
	return errors.Join(decompressErr, bodyErr)
}

func parseTrade(market Market, record []string) (Trade, error) {
	timestamp, err := secondsToMillis(record[0])
	if err != nil {
		return Trade{}, err
	}
	if _, err := strconv.ParseUint(record[1], 10, 64); err != nil {
		return Trade{}, fmt.Errorf("invalid trade ID %q", record[1])
	}
	if !decimalPattern.MatchString(record[2]) {
		return Trade{}, fmt.Errorf("invalid trade price %q", record[2])
	}
	item := Trade{ID: record[1], Time: record[0], Timestamp: timestamp, Price: record[2]}
	if market == MarketSpot {
		if !decimalPattern.MatchString(record[3]) {
			return Trade{}, fmt.Errorf("invalid trade amount %q", record[3])
		}
		item.Amount = record[3]
		switch record[4] {
		case "1":
			item.Side = unified.SideBuy
		case "2":
			item.Side = unified.SideSell
		default:
			return Trade{}, fmt.Errorf("invalid Spot trade side %q", record[4])
		}
		return item, nil
	}
	size, negative := strings.CutPrefix(record[3], "-")
	if !decimalPattern.MatchString(size) || strings.Trim(size, "0.") == "" {
		return Trade{}, fmt.Errorf("invalid Futures trade size %q", record[3])
	}
	item.Amount, item.Side = size, unified.SideBuy
	if negative {
		item.Side = unified.SideSell
	}
	return item, nil
}

func parseCandle(record []string) (Candle, error) {
	seconds, err := strconv.ParseInt(record[0], 10, 64)
	if err != nil || seconds < 0 || seconds > (1<<63-1)/1000 {
		return Candle{}, fmt.Errorf("invalid candle timestamp %q", record[0])
	}
	// 저장소 열 순서는 REST API와 같은 timestamp, volume, close, high, low, open이다.
	for index, name := range []string{"volume", "close", "high", "low", "open"} {
		if !decimalPattern.MatchString(record[index+1]) {
			return Candle{}, fmt.Errorf("invalid candle %s %q", name, record[index+1])
		}
	}
	return Candle{
		StartTime: seconds * 1000, Volume: record[1], Close: record[2],
		High: record[3], Low: record[4], Open: record[5],
	}, nil
}

// secondsToMillis는 Unix 초 decimal을 millisecond로 내림한다.
// 월 단위 덤프는 수백만 줄이므로 임의 정밀도 연산 대신 자릿수로 변환한다.
func secondsToMillis(value string) (int64, error) {
	if !decimalPattern.MatchString(value) {
		return 0, fmt.Errorf("invalid timestamp %q", value)
	}
	whole, fraction, _ := strings.Cut(value, ".")
	seconds, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || seconds > (1<<63-1)/1000-1 {
		return 0, fmt.Errorf("timestamp %q is outside int64 milliseconds", value)
	}
	if len(fraction) > 3 {
		fraction = fraction[:3]
	}
	fraction += strings.Repeat("0", 3-len(fraction))
	milliseconds, _ := strconv.ParseInt(fraction, 10, 64)
	return seconds*1000 + milliseconds, nil
}
