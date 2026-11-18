package archive

import (
	"fmt"
	"regexp"
	"time"

	trade "github.com/proven-trade/cex-sdk"
)

var symbolPattern = regexp.MustCompile(`^[A-Z0-9]+_[A-Z0-9]+$`)

// Market은 덤프 저장소의 사업 구분이다.
type Market string

const (
	MarketSpot        Market = "spot"
	MarketFuturesUSDT Market = "futures_usdt"
	MarketFuturesBTC  Market = "futures_btc"
)

// CandleInterval은 저장소가 게시하는 캔들 덤프 구간이다.
type CandleInterval string

const (
	Candle1Minute  CandleInterval = "1m"
	Candle5Minutes CandleInterval = "5m"
	Candle1Hour    CandleInterval = "1h"
	Candle4Hours   CandleInterval = "4h"
	Candle1Day     CandleInterval = "1d"
)

// TradesRequest는 한 달치 체결 덤프 조회 조건이다.
// Symbol은 Spot 거래쌍 또는 Futures 계약명이며 BTC_USDT 형식이다.
type TradesRequest struct {
	Market Market
	Symbol string
	Year   int
	Month  time.Month
}

// CandlesRequest는 한 달치 캔들 덤프 조회 조건이다.
type CandlesRequest struct {
	Market   Market
	Symbol   string
	Interval CandleInterval
	Year     int
	Month    time.Month
}

func (request TradesRequest) path() (string, error) {
	if err := validateFile(request.Market, request.Symbol, request.Year, request.Month); err != nil {
		return "", err
	}
	dataset := "trades"
	if request.Market == MarketSpot {
		dataset = "deals"
	}
	return filePath(request.Market, dataset, request.Symbol, request.Year, request.Month), nil
}

func (request CandlesRequest) path() (string, error) {
	if err := validateFile(request.Market, request.Symbol, request.Year, request.Month); err != nil {
		return "", err
	}
	switch request.Interval {
	case Candle1Minute, Candle5Minutes, Candle1Hour, Candle4Hours, Candle1Day:
	default:
		return "", validationError("unsupported archive candle interval %q", request.Interval)
	}
	dataset := "candlesticks_" + string(request.Interval)
	return filePath(request.Market, dataset, request.Symbol, request.Year, request.Month), nil
}

func validateFile(market Market, symbol string, year int, month time.Month) error {
	switch market {
	case MarketSpot, MarketFuturesUSDT, MarketFuturesBTC:
	default:
		return validationError("unsupported archive market %q", market)
	}
	if !symbolPattern.MatchString(symbol) {
		return validationError("archive symbol must look like BTC_USDT")
	}
	if year < 2013 || year > 9999 {
		return validationError("archive year must be between 2013 and 9999")
	}
	if month < time.January || month > time.December {
		return validationError("archive month must be between 1 and 12")
	}
	return nil
}

func filePath(market Market, dataset, symbol string, year int, month time.Month) string {
	period := fmt.Sprintf("%04d%02d", year, int(month))
	return fmt.Sprintf("%s/%s/%s/%s-%s.csv.gz", market, dataset, period, symbol, period)
}

func validationError(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", trade.ErrValidation, fmt.Sprintf(format, arguments...))
}
