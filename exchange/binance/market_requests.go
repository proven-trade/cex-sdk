package binance

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// OrderBookRequest는 호가 조회 조건이다.
type OrderBookRequest struct {
	Symbol string
	Limit  int
}

func (request OrderBookRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 5000 {
		return validationError("order book limit must be between 1 and 5000 or zero for default")
	}
	return nil
}

func (request OrderBookRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	return values
}

func (request OrderBookRequest) weight() int {
	switch {
	case request.Limit == 0 || request.Limit <= 100:
		return 5
	case request.Limit <= 500:
		return 25
	case request.Limit <= 1000:
		return 50
	default:
		return 250
	}
}

// RecentTradesRequest는 최근 공개 체결 조회 조건이다.
type RecentTradesRequest struct {
	Symbol string
	Limit  int
}

func (request RecentTradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("recent trades limit must be between 1 and 1000 or zero for default")
	}
	return nil
}

func (request RecentTradesRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	return values
}

// BookTickerRequest는 최우선 호가를 조회할 단일 상품을 지정한다.
type BookTickerRequest struct {
	Symbol string
}

// KlinesRequest는 OHLCV 캔들 조회 조건이다.
type KlinesRequest struct {
	Symbol    string
	Interval  KlineInterval
	StartTime *time.Time
	EndTime   *time.Time
	TimeZone  string
	Limit     int
}

func (request KlinesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported kline interval %q", request.Interval)
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("kline limit must be between 1 and 1000 or zero for default")
	}
	if request.StartTime != nil && request.EndTime != nil && request.StartTime.After(*request.EndTime) {
		return validationError("kline startTime cannot be after endTime")
	}
	if request.TimeZone != "" && strings.TrimSpace(request.TimeZone) != request.TimeZone {
		return validationError("kline timeZone cannot have surrounding whitespace")
	}
	return nil
}

func (request KlinesRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	values.Set("interval", string(request.Interval))
	if request.StartTime != nil {
		values.Set("startTime", strconv.FormatInt(request.StartTime.UnixMilli(), 10))
	}
	if request.EndTime != nil {
		values.Set("endTime", strconv.FormatInt(request.EndTime.UnixMilli(), 10))
	}
	setIfNotEmpty(values, "timeZone", request.TimeZone)
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	return values
}

func (interval KlineInterval) valid() bool {
	switch interval {
	case Kline1Second,
		Kline1Minute,
		Kline3Minutes,
		Kline5Minutes,
		Kline15Minutes,
		Kline30Minutes,
		Kline1Hour,
		Kline2Hours,
		Kline4Hours,
		Kline6Hours,
		Kline8Hours,
		Kline12Hours,
		Kline1Day,
		Kline3Days,
		Kline1Week,
		Kline1Month:
		return true
	default:
		return false
	}
}
