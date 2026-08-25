package cryptocom

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
)

var instrumentNamePattern = regexp.MustCompile(`^[A-Z0-9]{1,20}_[A-Z0-9]{1,20}$`)

// OrderBookRequest는 Spot 호가 snapshot 조회 조건이다.
type OrderBookRequest struct {
	InstrumentName string
	Depth          int
}

// TradesRequest는 최근 공개 Spot 체결 조회 조건이다.
type TradesRequest struct {
	InstrumentName string
	Count          int
	Start          *time.Time
	End            *time.Time
}

// CandleTimeframe은 Crypto.com Spot 캔들 구간이다.
type CandleTimeframe string

const (
	Candle1Minute   CandleTimeframe = "1m"
	Candle5Minutes  CandleTimeframe = "5m"
	Candle15Minutes CandleTimeframe = "15m"
	Candle30Minutes CandleTimeframe = "30m"
	Candle1Hour     CandleTimeframe = "1h"
	Candle2Hours    CandleTimeframe = "2h"
	Candle4Hours    CandleTimeframe = "4h"
	Candle12Hours   CandleTimeframe = "12h"
	Candle1Day      CandleTimeframe = "1D"
	Candle7Days     CandleTimeframe = "7D"
	Candle14Days    CandleTimeframe = "14D"
	Candle1Month    CandleTimeframe = "1M"
)

// CandlesRequest는 시간 범위와 개수 기반 Spot OHLCV 조회 조건이다.
type CandlesRequest struct {
	InstrumentName string
	Timeframe      CandleTimeframe
	Count          int
	Start          *time.Time
	End            *time.Time
}

func (request OrderBookRequest) validate() error {
	if err := validateInstrumentName(request.InstrumentName); err != nil {
		return err
	}
	if request.Depth < 1 || request.Depth > 50 {
		return validationError("order book depth must be between 1 and 50")
	}
	return nil
}

func (request OrderBookRequest) values() url.Values {
	return url.Values{
		"instrument_name": {request.InstrumentName},
		"depth":           {strconv.Itoa(request.Depth)},
	}
}

func (request TradesRequest) validate() error {
	if err := validateInstrumentName(request.InstrumentName); err != nil {
		return err
	}
	if request.Count < 0 {
		return validationError("trade count must be positive or zero")
	}
	return validateTimeRange(request.Start, request.End)
}

func (request TradesRequest) values() url.Values {
	values := url.Values{"instrument_name": {request.InstrumentName}}
	setPositiveInt(values, "count", request.Count)
	setTimeRange(values, request.Start, request.End)
	return values
}

func (request CandlesRequest) validate() error {
	if err := validateInstrumentName(request.InstrumentName); err != nil {
		return err
	}
	if !request.Timeframe.valid() {
		return validationError("unsupported Crypto.com candle timeframe %q", request.Timeframe)
	}
	if request.Count < 0 {
		return validationError("candle count must be positive or zero")
	}
	return validateTimeRange(request.Start, request.End)
}

func (request CandlesRequest) values() url.Values {
	values := url.Values{
		"instrument_name": {request.InstrumentName},
		"timeframe":       {string(request.Timeframe)},
	}
	setPositiveInt(values, "count", request.Count)
	setTimeRange(values, request.Start, request.End)
	return values
}

func (timeframe CandleTimeframe) valid() bool {
	switch timeframe {
	case Candle1Minute, Candle5Minutes, Candle15Minutes, Candle30Minutes,
		Candle1Hour, Candle2Hours, Candle4Hours, Candle12Hours,
		Candle1Day, Candle7Days, Candle14Days, Candle1Month:
		return true
	default:
		return false
	}
}

func validateInstrumentName(value string) error {
	if !instrumentNamePattern.MatchString(value) {
		return validationError("invalid Crypto.com instrument name %q", value)
	}
	return nil
}

func validateTimeRange(start, end *time.Time) error {
	if start != nil && start.UnixMilli() <= 0 || end != nil && end.UnixMilli() <= 0 {
		return validationError("query timestamps must be after the Unix epoch")
	}
	if start != nil && end != nil && !start.Before(*end) {
		return validationError("query start must be before end")
	}
	return nil
}

func setTimeRange(values url.Values, start, end *time.Time) {
	if start != nil {
		values.Set("start_ts", strconv.FormatInt(start.UnixMilli(), 10))
	}
	if end != nil {
		values.Set("end_ts", strconv.FormatInt(end.UnixMilli(), 10))
	}
}

func setPositiveInt(values url.Values, key string, value int) {
	if value > 0 {
		values.Set(key, strconv.Itoa(value))
	}
}

func validationError(format string, arguments ...any) error {
	return &trade.APIError{
		Category: trade.ErrorValidation, Exchange: model.ExchangeCryptoCom,
		Cause: fmt.Errorf(format, arguments...),
	}
}
