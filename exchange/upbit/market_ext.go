package upbit

import (
	"context"
	"encoding/json"
	"strings"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// CandlesRequest는 초·주·월·연 캔들이 공유하는 조회 조건이다.
// To는 조회 종료 시각(ISO 8601)이며 비우면 요청 시각 기준 최근 캔들을 조회한다.
type CandlesRequest struct {
	Market string
	To     string
	Count  int
}

// DayCandlesRequest는 일 캔들 조회 조건이다.
type DayCandlesRequest struct {
	Market string
	To     string
	Count  int
	// ConvertingPriceUnit을 지정하면 종가를 해당 통화(예: KRW)로 환산한 값이 converted_trade_price로 추가된다.
	ConvertingPriceUnit string
}

// QuoteTickersRequest는 마켓(호가 자산) 단위 현재가 조회 조건이다.
type QuoteTickersRequest struct {
	// QuoteCurrencies는 조회할 마켓의 통화 코드 목록이다. 예: KRW, BTC, USDT
	QuoteCurrencies []string
}

// OrderBookInstrumentsRequest는 호가 정책을 조회할 페어 목록이다.
type OrderBookInstrumentsRequest struct {
	Markets []string
}

// DayCandle은 일 캔들 한 건이다. 전일 종가 대비 변화는 UTC 0시 기준 전일 종가로 계산된다.
type DayCandle struct {
	Market                 string          `json:"market"`
	CandleDateTimeUTC      string          `json:"candle_date_time_utc"`
	CandleDateTimeKST      string          `json:"candle_date_time_kst"`
	OpeningPrice           Decimal         `json:"opening_price"`
	HighPrice              Decimal         `json:"high_price"`
	LowPrice               Decimal         `json:"low_price"`
	TradePrice             Decimal         `json:"trade_price"`
	Timestamp              int64           `json:"timestamp"`
	AccumulatedTradePrice  Decimal         `json:"candle_acc_trade_price"`
	AccumulatedTradeVolume Decimal         `json:"candle_acc_trade_volume"`
	PreviousClosingPrice   Decimal         `json:"prev_closing_price"`
	ChangePrice            Decimal         `json:"change_price"`
	ChangeRate             Decimal         `json:"change_rate"`
	ConvertedTradePrice    Decimal         `json:"converted_trade_price"`
	Raw                    json.RawMessage `json:"-"`
}

// PeriodCandle은 주·월·연 캔들 한 건이다. FirstDayOfPeriod는 집계 기간의 시작일(yyyy-MM-dd)이다.
type PeriodCandle struct {
	Market                 string          `json:"market"`
	CandleDateTimeUTC      string          `json:"candle_date_time_utc"`
	CandleDateTimeKST      string          `json:"candle_date_time_kst"`
	OpeningPrice           Decimal         `json:"opening_price"`
	HighPrice              Decimal         `json:"high_price"`
	LowPrice               Decimal         `json:"low_price"`
	TradePrice             Decimal         `json:"trade_price"`
	Timestamp              int64           `json:"timestamp"`
	AccumulatedTradePrice  Decimal         `json:"candle_acc_trade_price"`
	AccumulatedTradeVolume Decimal         `json:"candle_acc_trade_volume"`
	FirstDayOfPeriod       string          `json:"first_day_of_period"`
	Raw                    json.RawMessage `json:"-"`
}

// OrderBookInstrument는 페어의 호가 단위와 호가 모아보기 단위다.
// SupportedLevels의 0은 기본 호가 단위이며 모아보기는 원화 마켓에서만 지원된다.
type OrderBookInstrument struct {
	Market          string          `json:"market"`
	QuoteCurrency   string          `json:"quote_currency"`
	TickSize        Decimal         `json:"tick_size"`
	SupportedLevels []Decimal       `json:"supported_levels"`
	Raw             json.RawMessage `json:"-"`
}

func (request CandlesRequest) validate() error {
	if err := validateMarket(request.Market); err != nil {
		return err
	}
	return validateCandleCount(request.Count)
}

func (request CandlesRequest) parameters() parameters {
	values := parameters{{key: "market", value: request.Market}}
	values.add("to", request.To)
	values.addInt("count", request.Count)
	return values
}

func (request DayCandlesRequest) validate() error {
	if err := validateMarket(request.Market); err != nil {
		return err
	}
	if err := validateCandleCount(request.Count); err != nil {
		return err
	}
	if request.ConvertingPriceUnit != "" {
		return validateCurrencyCode("converting price unit", request.ConvertingPriceUnit)
	}
	return nil
}

func (request DayCandlesRequest) parameters() parameters {
	values := parameters{{key: "market", value: request.Market}}
	values.add("to", request.To)
	values.addInt("count", request.Count)
	values.add("converting_price_unit", request.ConvertingPriceUnit)
	return values
}

func (request QuoteTickersRequest) validate() error {
	return validateCurrencyList("quote currency", request.QuoteCurrencies)
}

func (request QuoteTickersRequest) parameters() parameters {
	return parameters{{key: "quote_currencies", value: strings.Join(request.QuoteCurrencies, ",")}}
}

func (request OrderBookInstrumentsRequest) validate() error {
	return validateMarkets(request.Markets)
}

func (request OrderBookInstrumentsRequest) parameters() parameters {
	return parameters{{key: "markets", value: strings.Join(request.Markets, ",")}}
}

// SecondCandles는 지정한 마켓의 초 단위 OHLCV 캔들을 조회한다.
// 초 캔들은 요청 시점 기준 최대 3개월 이전 데이터만 제공하며 응답에 unit은 없으므로 Candle.Unit은 0이다.
func (client *Client) SecondCandles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return listCandles(ctx, client, "/v1/candles/seconds", request.parameters(), func(item *Candle, raw []byte) {
		item.Raw = raw
	}, options...)
}

// DayCandles는 지정한 마켓의 일 단위 OHLCV 캔들을 조회한다.
func (client *Client) DayCandles(
	ctx context.Context,
	request DayCandlesRequest,
	options ...trade.RequestOption,
) ([]DayCandle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return listCandles(ctx, client, "/v1/candles/days", request.parameters(), func(item *DayCandle, raw []byte) {
		item.Raw = raw
	}, options...)
}

// WeekCandles는 지정한 마켓의 주 단위 OHLCV 캔들을 조회한다.
func (client *Client) WeekCandles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]PeriodCandle, error) {
	return client.periodCandles(ctx, "/v1/candles/weeks", request, options...)
}

// MonthCandles는 지정한 마켓의 월 단위 OHLCV 캔들을 조회한다.
func (client *Client) MonthCandles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]PeriodCandle, error) {
	return client.periodCandles(ctx, "/v1/candles/months", request, options...)
}

// YearCandles는 지정한 마켓의 연 단위 OHLCV 캔들을 조회한다.
func (client *Client) YearCandles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]PeriodCandle, error) {
	return client.periodCandles(ctx, "/v1/candles/years", request, options...)
}

// QuoteTickers는 지정한 마켓(호가 자산) 안의 모든 페어 현재가를 조회한다.
// 응답의 UTC 0시 기준 누적 거래 금액(acc_trade_price)과 거래량(acc_trade_volume)은 Raw에서 확인한다.
func (client *Client) QuoteTickers(
	ctx context.Context,
	request QuoteTickersRequest,
	options ...trade.RequestOption,
) ([]Ticker, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, "/v1/ticker/all", request.parameters(), tickerRate, options...)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *Ticker, raw []byte) {
		item.Raw = raw
	})
}

// OrderBookInstruments는 지정한 페어의 호가 단위(tick_size)와 호가 모아보기 단위를 조회한다.
func (client *Client) OrderBookInstruments(
	ctx context.Context,
	request OrderBookInstrumentsRequest,
	options ...trade.RequestOption,
) ([]OrderBookInstrument, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, "/v1/orderbook/instruments", request.parameters(), orderBookRate, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *OrderBookInstrument, raw []byte) {
		item.Raw = raw
	})
}

func (client *Client) periodCandles(
	ctx context.Context,
	path string,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]PeriodCandle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return listCandles(ctx, client, path, request.parameters(), func(item *PeriodCandle, raw []byte) {
		item.Raw = raw
	}, options...)
}

// listCandles는 candle 그룹 한도로 공개 캔들 목록을 조회해 항목별 원문과 함께 decode한다.
func listCandles[T any](
	ctx context.Context,
	client *Client,
	path string,
	params parameters,
	setRaw func(*T, []byte),
	options ...trade.RequestOption,
) ([]T, error) {
	response, err := client.executePublic(ctx, path, params, candleRate, options...)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, setRaw)
}

func validateCandleCount(count int) error {
	if count < 0 || count > 200 {
		return validationError("candle count must be between 1 and 200 or zero for default")
	}
	return nil
}
