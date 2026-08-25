package mexc

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
)

var symbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,40}$`)

// ExchangeInfoRequest는 전체·단일·복수 Spot 거래쌍 규칙 조회 조건이다.
type ExchangeInfoRequest struct {
	Symbol  string
	Symbols []string
}

// OrderBookRequest는 Spot 호가 snapshot 조회 조건이다.
type OrderBookRequest struct {
	Symbol string
	Limit  int
}

// TradesRequest는 최근 공개 Spot 체결 조회 조건이다.
type TradesRequest struct {
	Symbol string
	Limit  int
}

// AggregateTradesRequest는 시간 범위 기반 합산 Spot 체결 조회 조건이다.
type AggregateTradesRequest struct {
	Symbol string
	Start  *time.Time
	End    *time.Time
	Limit  int
}

// CandleInterval은 MEXC Spot V3 캔들 구간이다.
type CandleInterval string

const (
	Candle1Minute   CandleInterval = "1m"
	Candle5Minutes  CandleInterval = "5m"
	Candle15Minutes CandleInterval = "15m"
	Candle30Minutes CandleInterval = "30m"
	Candle1Hour     CandleInterval = "60m"
	Candle4Hours    CandleInterval = "4h"
	Candle1Day      CandleInterval = "1d"
	Candle1Week     CandleInterval = "1W"
	Candle1Month    CandleInterval = "1M"
)

// CandlesRequest는 시간 범위와 개수 기반 Spot OHLCV 조회 조건이다.
type CandlesRequest struct {
	Symbol   string
	Interval CandleInterval
	Start    *time.Time
	End      *time.Time
	Limit    int
}

func (request ExchangeInfoRequest) validate() error {
	if request.Symbol != "" && len(request.Symbols) > 0 {
		return validationError("exchange info accepts symbol or symbols, not both")
	}
	if request.Symbol != "" {
		return validateSymbol(request.Symbol)
	}
	seen := make(map[string]struct{}, len(request.Symbols))
	for _, symbol := range request.Symbols {
		if err := validateSymbol(symbol); err != nil {
			return err
		}
		if _, exists := seen[symbol]; exists {
			return validationError("exchange info symbols contain a duplicate")
		}
		seen[symbol] = struct{}{}
	}
	return nil
}

func (request ExchangeInfoRequest) values() url.Values {
	values := make(url.Values)
	if request.Symbol != "" {
		values.Set("symbol", request.Symbol)
	}
	if len(request.Symbols) > 0 {
		values.Set("symbols", strings.Join(request.Symbols, ","))
	}
	return values
}

func (request OrderBookRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 5000 {
		return validationError("order book limit must be between 1 and 5000 or zero")
	}
	return nil
}

func (request OrderBookRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request TradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	return validateLimit(request.Limit)
}

func (request TradesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request AggregateTradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateLimit(request.Limit); err != nil {
		return err
	}
	if (request.Start == nil) != (request.End == nil) {
		return validationError("aggregate trade start and end must be used together")
	}
	if request.Start != nil {
		if request.Start.UnixMilli() <= 0 || request.End.UnixMilli() <= 0 {
			return validationError("aggregate trade timestamps must be after the Unix epoch")
		}
		if request.Start.After(*request.End) {
			return validationError("aggregate trade start cannot be after end")
		}
	}
	return nil
}

func (request AggregateTradesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setPositiveInt(values, "limit", request.Limit)
	if request.Start != nil {
		values.Set("startTime", strconv.FormatInt(request.Start.UnixMilli(), 10))
		values.Set("endTime", strconv.FormatInt(request.End.UnixMilli(), 10))
	}
	return values
}

func (request CandlesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	if err := validateLimit(request.Limit); err != nil {
		return err
	}
	if request.Start != nil && request.Start.UnixMilli() <= 0 ||
		request.End != nil && request.End.UnixMilli() <= 0 {
		return validationError("candle timestamps must be after the Unix epoch")
	}
	if request.Start != nil && request.End != nil && !request.Start.Before(*request.End) {
		return validationError("candle start must be before end")
	}
	return nil
}

func (request CandlesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}, "interval": {string(request.Interval)}}
	setPositiveInt(values, "limit", request.Limit)
	if request.Start != nil {
		values.Set("startTime", strconv.FormatInt(request.Start.UnixMilli(), 10))
	}
	if request.End != nil {
		values.Set("endTime", strconv.FormatInt(request.End.UnixMilli(), 10))
	}
	return values
}

func (interval CandleInterval) valid() bool {
	switch interval {
	case Candle1Minute, Candle5Minutes, Candle15Minutes, Candle30Minutes,
		Candle1Hour, Candle4Hours, Candle1Day, Candle1Week, Candle1Month:
		return true
	default:
		return false
	}
}

func validateSymbol(symbol string) error {
	if !symbolPattern.MatchString(symbol) {
		return validationError("invalid MEXC symbol %q", symbol)
	}
	return nil
}

func validateLimit(limit int) error {
	if limit < 0 || limit > 1000 {
		return validationError("limit must be between 1 and 1000 or zero")
	}
	return nil
}

func setPositiveInt(values url.Values, key string, value int) {
	if value > 0 {
		values.Set(key, strconv.Itoa(value))
	}
}

func validationError(format string, values ...any) error {
	return &trade.APIError{
		Category: trade.ErrorValidation, Exchange: model.ExchangeMEXC,
		Cause: fmt.Errorf(format, values...),
	}
}
