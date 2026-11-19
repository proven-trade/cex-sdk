package archive

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	trade "github.com/proven-trade/cex-sdk"
)

const (
	// Spot 파일은 USDⓈ-M 파일보다 isBestMatch 열이 하나 더 있다.
	spotAggTradeFields = 8
	usdmAggTradeFields = 7
	spotTradeFields    = 7
	usdmTradeFields    = 6
	// maxDecimalExponent는 지수 표기를 풀어 쓸 때 허용하는 지수의 절댓값이다.
	maxDecimalExponent = 30
)

// tradeDecimalPattern은 일반 십진수와 지수 표기를 받는다.
// USDⓈ-M 체결 파일은 0.001보다 작거나 10,000,000 이상인 값을 7.72E-6, 5.5738058E7처럼 기록한다.
var tradeDecimalPattern = regexp.MustCompile(`^([0-9]+)(?:\.([0-9]+))?(?:[eE]([-+]?[0-9]+))?$`)

// AggTrade는 집계 체결 덤프 한 줄이다. Time은 파일 단위와 관계없이 Unix millisecond이고
// Price와 Quantity는 지수 표기를 풀어 쓴 REST와 같은 십진 문자열이다.
type AggTrade struct {
	AggregateID  int64
	Price        string
	Quantity     string
	FirstTradeID int64
	LastTradeID  int64
	Time         int64
	BuyerMaker   bool
	// BestMatch는 Spot 파일의 isBestMatch 열이다. USDⓈ-M 파일에는 이 열이 없어 항상 false다.
	BestMatch bool
}

// Trade는 개별 체결 덤프 한 줄이다. Time은 파일 단위와 관계없이 Unix millisecond이고
// Price, Quantity, QuoteQuantity는 지수 표기를 풀어 쓴 REST와 같은 십진 문자열이다.
type Trade struct {
	ID            int64
	Price         string
	Quantity      string
	QuoteQuantity string
	Time          int64
	BuyerMaker    bool
	// BestMatch는 Spot 파일의 isBestMatch 열이다. USDⓈ-M 파일에는 이 열이 없어 항상 false다.
	BestMatch bool
}

// AggTradeReader는 집계 체결 덤프를 파일 순서(체결 순서)대로 한 줄씩 읽는다.
type AggTradeReader struct {
	stream *csvStream
}

// Next는 다음 집계 체결을 반환하고 파일 끝에서 io.EOF를 반환한다.
// 최근 USDⓈ-M 파일의 헤더 행은 건너뛴다.
func (reader *AggTradeReader) Next() (AggTrade, error) {
	record, err := reader.stream.next()
	if err != nil {
		return AggTrade{}, err
	}
	item, err := parseAggTrade(record)
	if err != nil {
		return AggTrade{}, reader.stream.recordError(err)
	}
	return item, nil
}

// Close는 압축 해제 스트림을 닫는다.
func (reader *AggTradeReader) Close() error {
	return reader.stream.close()
}

// TradeReader는 개별 체결 덤프를 파일 순서(체결 순서)대로 한 줄씩 읽는다.
type TradeReader struct {
	stream *csvStream
}

// Next는 다음 체결을 반환하고 파일 끝에서 io.EOF를 반환한다.
// 최근 USDⓈ-M 파일의 헤더 행은 건너뛴다.
func (reader *TradeReader) Next() (Trade, error) {
	record, err := reader.stream.next()
	if err != nil {
		return Trade{}, err
	}
	item, err := parseTrade(record)
	if err != nil {
		return Trade{}, reader.stream.recordError(err)
	}
	return item, nil
}

// Close는 압축 해제 스트림을 닫는다.
func (reader *TradeReader) Close() error {
	return reader.stream.close()
}

// AggTrades는 집계 체결 덤프 파일 하나를 내려받아 SHA-256을 검증한 뒤 행 단위 reader를 반환한다.
// 체크섬 파일이 없거나 덤프가 없으면 ErrFileNotFound를 반환한다.
// 인기 상품의 월 파일은 수백 MB에서 1GB를 넘으므로 필요하면 Config.MaxFileBytes를 올리거나 일 파일로 받는다.
func (client *Client) AggTrades(
	ctx context.Context,
	request AggTradesRequest,
	options ...trade.RequestOption,
) (*AggTradeReader, error) {
	filePath, err := request.path()
	if err != nil {
		return nil, err
	}
	entry, err := client.open(ctx, filePath, options)
	if err != nil {
		return nil, err
	}
	fields := usdmAggTradeFields
	if request.Market == MarketSpot {
		fields = spotAggTradeFields
	}
	return &AggTradeReader{stream: newCSVStream(filePath, entry, fields, "agg_trade_id")}, nil
}

// Trades는 개별 체결 덤프 파일 하나를 내려받아 SHA-256을 검증한 뒤 행 단위 reader를 반환한다.
// 체크섬 파일이 없거나 덤프가 없으면 ErrFileNotFound를 반환한다.
// 인기 상품의 월 파일은 수백 MB에서 1GB를 넘으므로 필요하면 Config.MaxFileBytes를 올리거나 일 파일로 받는다.
func (client *Client) Trades(
	ctx context.Context,
	request TradesRequest,
	options ...trade.RequestOption,
) (*TradeReader, error) {
	filePath, err := request.path()
	if err != nil {
		return nil, err
	}
	entry, err := client.open(ctx, filePath, options)
	if err != nil {
		return nil, err
	}
	fields := usdmTradeFields
	if request.Market == MarketSpot {
		fields = spotTradeFields
	}
	return &TradeReader{stream: newCSVStream(filePath, entry, fields, "id")}, nil
}

// parseAggTrade는 agg_trade_id, price, quantity, first_trade_id, last_trade_id,
// transact_time, is_buyer_maker 열과 Spot 파일의 is_best_match 열을 해석한다.
func parseAggTrade(record []string) (AggTrade, error) {
	aggregateID, err := parseInteger("aggregate trade ID", record[0])
	if err != nil {
		return AggTrade{}, err
	}
	price, err := plainDecimal("price", record[1])
	if err != nil {
		return AggTrade{}, err
	}
	quantity, err := plainDecimal("quantity", record[2])
	if err != nil {
		return AggTrade{}, err
	}
	firstTradeID, err := parseInteger("first trade ID", record[3])
	if err != nil {
		return AggTrade{}, err
	}
	lastTradeID, err := parseInteger("last trade ID", record[4])
	if err != nil {
		return AggTrade{}, err
	}
	tradeTime, err := parseMillis("trade time", record[5])
	if err != nil {
		return AggTrade{}, err
	}
	buyerMaker, err := parseBool("buyer maker flag", record[6])
	if err != nil {
		return AggTrade{}, err
	}
	item := AggTrade{
		AggregateID: aggregateID, Price: price, Quantity: quantity, FirstTradeID: firstTradeID,
		LastTradeID: lastTradeID, Time: tradeTime, BuyerMaker: buyerMaker,
	}
	if len(record) == spotAggTradeFields {
		if item.BestMatch, err = parseBool("best match flag", record[7]); err != nil {
			return AggTrade{}, err
		}
	}
	return item, nil
}

// parseTrade는 id, price, qty, quote_qty, time, is_buyer_maker 열과 Spot 파일의 is_best_match 열을 해석한다.
func parseTrade(record []string) (Trade, error) {
	id, err := parseInteger("trade ID", record[0])
	if err != nil {
		return Trade{}, err
	}
	price, err := plainDecimal("price", record[1])
	if err != nil {
		return Trade{}, err
	}
	quantity, err := plainDecimal("quantity", record[2])
	if err != nil {
		return Trade{}, err
	}
	quoteQuantity, err := plainDecimal("quote quantity", record[3])
	if err != nil {
		return Trade{}, err
	}
	tradeTime, err := parseMillis("trade time", record[4])
	if err != nil {
		return Trade{}, err
	}
	buyerMaker, err := parseBool("buyer maker flag", record[5])
	if err != nil {
		return Trade{}, err
	}
	item := Trade{
		ID: id, Price: price, Quantity: quantity, QuoteQuantity: quoteQuantity, Time: tradeTime, BuyerMaker: buyerMaker,
	}
	if len(record) == spotTradeFields {
		if item.BestMatch, err = parseBool("best match flag", record[6]); err != nil {
			return Trade{}, err
		}
	}
	return item, nil
}

// plainDecimal은 체결 파일의 십진 문자열을 REST와 같은 일반 십진 문자열로 돌려준다.
// 지수 표기는 부동소수점을 거치지 않고 자릿수만 옮겨 풀어 쓰므로 7.72E-6은 0.00000772가 된다.
// 지수가 없는 값은 원문 그대로 반환한다.
func plainDecimal(name, value string) (string, error) {
	match := tradeDecimalPattern.FindStringSubmatch(value)
	if match == nil {
		return "", fmt.Errorf("invalid %s %q", name, value)
	}
	if match[3] == "" {
		return value, nil
	}
	exponent, err := strconv.Atoi(match[3])
	if err != nil || exponent < -maxDecimalExponent || exponent > maxDecimalExponent {
		return "", fmt.Errorf("invalid %s %q", name, value)
	}
	digits := match[1] + match[2]
	point := len(match[1]) + exponent
	var integer, fraction string
	switch {
	case point <= 0:
		integer, fraction = "0", strings.Repeat("0", -point)+digits
	case point >= len(digits):
		integer = digits + strings.Repeat("0", point-len(digits))
	default:
		integer, fraction = digits[:point], digits[point:]
	}
	integer = strings.TrimLeft(integer, "0")
	if integer == "" {
		integer = "0"
	}
	if fraction == "" {
		return integer, nil
	}
	return integer + "." + fraction, nil
}
