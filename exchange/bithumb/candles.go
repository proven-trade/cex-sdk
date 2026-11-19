package bithumb

import (
	"context"
	"encoding/json"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// ConvertingPriceUnitKRW는 일봉 종가 환산에 공식적으로 지원하는 유일한 화폐 단위다.
const ConvertingPriceUnitKRW = "KRW"

// DayCandlesRequest는 일봉 조회 조건이다.
// ConvertingPriceUnit을 지정하면 원화 마켓이 아닌 마켓의 종가를 해당 화폐로 환산한 값이
// DayCandle.ConvertedTradePrice로 함께 반환된다.
type DayCandlesRequest struct {
	Market              string
	To                  string
	Count               int
	ConvertingPriceUnit string
}

// PeriodCandlesRequest는 주봉과 월봉 조회 조건이다.
type PeriodCandlesRequest struct {
	Market string
	To     string
	Count  int
}

// DayCandle은 일 단위 OHLCV 캔들 한 건이다.
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

// PeriodCandle은 주 또는 월 단위 OHLCV 캔들 한 건이다.
// FirstDayOfPeriod는 캔들 기간의 시작일이다.
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

// DayCandles는 지정한 마켓의 일 단위 OHLCV 캔들을 최신순으로 조회한다.
func (client *Client) DayCandles(
	ctx context.Context,
	request DayCandlesRequest,
	options ...trade.RequestOption,
) ([]DayCandle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, "/v1/candles/days", request.parameters(), rateLimitPublicCandle, options...)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *DayCandle, raw []byte) {
		item.Raw = raw
	})
}

// WeekCandles는 지정한 마켓의 주 단위 OHLCV 캔들을 최신순으로 조회한다.
func (client *Client) WeekCandles(
	ctx context.Context,
	request PeriodCandlesRequest,
	options ...trade.RequestOption,
) ([]PeriodCandle, error) {
	return client.periodCandles(ctx, "/v1/candles/weeks", request, options...)
}

// MonthCandles는 지정한 마켓의 월 단위 OHLCV 캔들을 최신순으로 조회한다.
func (client *Client) MonthCandles(
	ctx context.Context,
	request PeriodCandlesRequest,
	options ...trade.RequestOption,
) ([]PeriodCandle, error) {
	return client.periodCandles(ctx, "/v1/candles/months", request, options...)
}

func (client *Client) periodCandles(
	ctx context.Context,
	path string,
	request PeriodCandlesRequest,
	options ...trade.RequestOption,
) ([]PeriodCandle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, path, request.parameters(), rateLimitPublicCandle, options...)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *PeriodCandle, raw []byte) {
		item.Raw = raw
	})
}

func (request DayCandlesRequest) validate() error {
	if err := validateCandleQuery(request.Market, request.To, request.Count); err != nil {
		return err
	}
	if request.ConvertingPriceUnit != "" && request.ConvertingPriceUnit != ConvertingPriceUnitKRW {
		return validationError("convertingPriceUnit must be KRW or empty")
	}
	return nil
}

func (request DayCandlesRequest) parameters() parameters {
	values := parameters{{key: "market", value: request.Market}}
	values.add("to", request.To)
	values.addInt("count", request.Count)
	values.add("convertingPriceUnit", request.ConvertingPriceUnit)
	return values
}

func (request PeriodCandlesRequest) validate() error {
	return validateCandleQuery(request.Market, request.To, request.Count)
}

func (request PeriodCandlesRequest) parameters() parameters {
	values := parameters{{key: "market", value: request.Market}}
	values.add("to", request.To)
	values.addInt("count", request.Count)
	return values
}

// validateCandleQuery는 일·주·월봉이 공유하는 market, to, count 조건을 검증한다.
func validateCandleQuery(market, to string, count int) error {
	if err := validateMarket(market); err != nil {
		return err
	}
	if count < 0 || count > 200 {
		return validationError("candle count must be between 1 and 200 or zero for default")
	}
	return validateParameterValue("to", to)
}
