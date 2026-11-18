package archive

import (
	"fmt"
	"regexp"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/exchange/binance"
)

var symbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,30}$`)

// Market은 공개 데이터 저장소의 상품 구분이다.
type Market string

const (
	// MarketSpot은 data/spot 아래의 Spot 덤프다.
	MarketSpot Market = "spot"
	// MarketUSDM은 data/futures/um 아래의 USDⓈ-M 선물 덤프다.
	MarketUSDM Market = "futures/um"
)

// KlinesRequest는 캔들 덤프 파일 하나의 조회 조건이다.
// Day가 0이면 월 파일(monthly), 1 이상이면 그 날짜의 일 파일(daily)을 연다.
// 월 파일은 달이 끝난 뒤 게시되므로 당월 구간은 일 파일로 채운다.
type KlinesRequest struct {
	Market   Market
	Symbol   string
	Interval binance.KlineInterval
	Year     int
	Month    time.Month
	Day      int
}

func (request KlinesRequest) path() (string, error) {
	switch request.Market {
	case MarketSpot, MarketUSDM:
	default:
		return "", validationError("unsupported archive market %q", request.Market)
	}
	if !symbolPattern.MatchString(request.Symbol) {
		return "", validationError("archive symbol must contain uppercase letters and digits")
	}
	interval, err := archiveInterval(request.Market, request.Interval)
	if err != nil {
		return "", err
	}
	if request.Year < 2017 || request.Year > 9999 {
		return "", validationError("archive year must be between 2017 and 9999")
	}
	if request.Month < time.January || request.Month > time.December {
		return "", validationError("archive month must be between 1 and 12")
	}
	period, frequency := fmt.Sprintf("%04d-%02d", request.Year, int(request.Month)), "monthly"
	if request.Day != 0 {
		date := time.Date(request.Year, request.Month, request.Day, 0, 0, 0, 0, time.UTC)
		if request.Day < 0 || date.Month() != request.Month {
			return "", validationError("archive day %d is outside %s", request.Day, period)
		}
		period, frequency = date.Format(time.DateOnly), "daily"
	}
	return fmt.Sprintf(
		"data/%s/%s/klines/%s/%s/%s-%s-%s.zip",
		request.Market, frequency, request.Symbol, interval, request.Symbol, interval, period,
	), nil
}

// archiveInterval은 REST 구간 문자열을 저장소 디렉터리 이름으로 바꾼다.
// 저장소는 1개월 구간을 1M 대신 1mo로 표기하고 1초 캔들은 Spot에만 게시한다.
func archiveInterval(market Market, interval binance.KlineInterval) (string, error) {
	switch interval {
	case binance.Kline1Second:
		if market != MarketSpot {
			return "", validationError("1s archive klines are only published for Spot")
		}
		return string(interval), nil
	case binance.Kline1Minute, binance.Kline3Minutes, binance.Kline5Minutes, binance.Kline15Minutes,
		binance.Kline30Minutes, binance.Kline1Hour, binance.Kline2Hours, binance.Kline4Hours,
		binance.Kline6Hours, binance.Kline8Hours, binance.Kline12Hours, binance.Kline1Day,
		binance.Kline3Days, binance.Kline1Week:
		return string(interval), nil
	case binance.Kline1Month:
		return "1mo", nil
	default:
		return "", validationError("unsupported archive kline interval %q", interval)
	}
}

func validationError(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", trade.ErrValidation, fmt.Sprintf(format, arguments...))
}
