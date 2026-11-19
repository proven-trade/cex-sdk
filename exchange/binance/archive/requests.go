package archive

import (
	"fmt"
	"regexp"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/exchange/binance"
)

// symbolPattern은 exchangeInfo가 주는 심볼을 받는다. Binance Spot에는 한자 심볼(币安人生USDT 등)도 있고
// 저장소도 그 이름으로 게시한다. 소문자·구분자·경로 문자는 거부한다.
var symbolPattern = regexp.MustCompile(`^[\p{Lu}\p{Lo}0-9]{2,30}$`)

// Market은 공개 데이터 저장소의 상품 구분이다.
type Market string

const (
	// MarketSpot은 data/spot 아래의 Spot 덤프다.
	MarketSpot Market = "spot"
	// MarketUSDM은 data/futures/um 아래의 USDⓈ-M 선물 덤프다.
	MarketUSDM Market = "futures/um"
)

// 저장소가 데이터 종류마다 나누는 디렉터리 이름이다.
const (
	datasetKlines             = "klines"
	datasetMarkPriceKlines    = "markPriceKlines"
	datasetIndexPriceKlines   = "indexPriceKlines"
	datasetPremiumIndexKlines = "premiumIndexKlines"
	datasetAggTrades          = "aggTrades"
	datasetTrades             = "trades"
)

// 저장소가 게시 주기마다 나누는 디렉터리 이름이다.
const (
	frequencyMonthly = "monthly"
	frequencyDaily   = "daily"
)

// KlinesRequest는 캔들 덤프 파일 하나의 조회 조건이다.
// Day가 0이면 월 파일(monthly), 1 이상이면 그 날짜의 일 파일(daily)을 연다.
// 월 파일은 달이 끝난 뒤 게시되므로 당월 구간은 일 파일로 채운다.
// 공식 다운로드 스크립트의 DAILY_INTERVALS는 3d·1w·1M 구간을 제외하므로 이 구간은 월 파일만 받고
// Day를 지정하면 검증 오류다.
type KlinesRequest struct {
	Market   Market
	Symbol   string
	Interval binance.KlineInterval
	Year     int
	Month    time.Month
	Day      int
}

// MarkPriceKlinesRequest는 USDⓈ-M 마크가 캔들 덤프 파일 하나의 조회 조건이다.
// 필드 의미는 KlinesRequest와 같다. 이 덤프는 USDⓈ-M(data/futures/um)에만 있어 Market 필드가 없고
// 1s 구간은 게시하지 않는다.
type MarkPriceKlinesRequest struct {
	Symbol   string
	Interval binance.KlineInterval
	Year     int
	Month    time.Month
	Day      int
}

// IndexPriceKlinesRequest는 USDⓈ-M 지수가 캔들 덤프 파일 하나의 조회 조건이다.
// 필드 의미와 제약은 MarkPriceKlinesRequest와 같다.
type IndexPriceKlinesRequest struct {
	Symbol   string
	Interval binance.KlineInterval
	Year     int
	Month    time.Month
	Day      int
}

// PremiumIndexKlinesRequest는 USDⓈ-M 프리미엄 지수 캔들 덤프 파일 하나의 조회 조건이다.
// 필드 의미와 제약은 MarkPriceKlinesRequest와 같다.
type PremiumIndexKlinesRequest struct {
	Symbol   string
	Interval binance.KlineInterval
	Year     int
	Month    time.Month
	Day      int
}

// AggTradesRequest는 집계 체결 덤프 파일 하나의 조회 조건이다.
// Day가 0이면 월 파일, 1 이상이면 그 날짜의 일 파일을 연다. 월 파일은 달이 끝난 뒤 게시된다.
type AggTradesRequest struct {
	Market Market
	Symbol string
	Year   int
	Month  time.Month
	Day    int
}

// TradesRequest는 개별 체결 덤프 파일 하나의 조회 조건이다. 필드 의미는 AggTradesRequest와 같다.
type TradesRequest struct {
	Market Market
	Symbol string
	Year   int
	Month  time.Month
	Day    int
}

func (request KlinesRequest) path() (string, error) {
	return klinesPath(
		datasetKlines, request.Market, request.Symbol, request.Interval, request.Year, request.Month, request.Day,
	)
}

func (request MarkPriceKlinesRequest) path() (string, error) {
	return klinesPath(
		datasetMarkPriceKlines, MarketUSDM, request.Symbol, request.Interval, request.Year, request.Month, request.Day,
	)
}

func (request IndexPriceKlinesRequest) path() (string, error) {
	return klinesPath(
		datasetIndexPriceKlines, MarketUSDM, request.Symbol, request.Interval, request.Year, request.Month, request.Day,
	)
}

func (request PremiumIndexKlinesRequest) path() (string, error) {
	return klinesPath(
		datasetPremiumIndexKlines, MarketUSDM, request.Symbol, request.Interval, request.Year, request.Month, request.Day,
	)
}

func (request AggTradesRequest) path() (string, error) {
	return tradesPath(datasetAggTrades, request.Market, request.Symbol, request.Year, request.Month, request.Day)
}

func (request TradesRequest) path() (string, error) {
	return tradesPath(datasetTrades, request.Market, request.Symbol, request.Year, request.Month, request.Day)
}

// klinesPath는 구간 디렉터리를 두는 캔들류 덤프의 저장소 경로를 만든다.
func klinesPath(
	dataset string,
	market Market,
	symbol string,
	interval binance.KlineInterval,
	year int,
	month time.Month,
	day int,
) (string, error) {
	frequency, period, err := validateFile(market, symbol, year, month, day)
	if err != nil {
		return "", err
	}
	directory, err := archiveInterval(market, frequency == frequencyDaily, interval)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"data/%s/%s/%s/%s/%s/%s-%s-%s.zip", market, frequency, dataset, symbol, directory, symbol, directory, period,
	), nil
}

// tradesPath는 구간 디렉터리가 없는 체결류 덤프의 저장소 경로를 만든다.
func tradesPath(dataset string, market Market, symbol string, year int, month time.Month, day int) (string, error) {
	frequency, period, err := validateFile(market, symbol, year, month, day)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"data/%s/%s/%s/%s/%s-%s-%s.zip", market, frequency, dataset, symbol, symbol, dataset, period,
	), nil
}

// validateFile은 모든 덤프가 공유하는 상품·심볼·기간 조건을 검증하고
// 저장소의 게시 주기 디렉터리와 파일명에 쓰는 기간 문자열을 반환한다.
func validateFile(
	market Market,
	symbol string,
	year int,
	month time.Month,
	day int,
) (frequency string, period string, err error) {
	switch market {
	case MarketSpot, MarketUSDM:
	default:
		return "", "", validationError("unsupported archive market %q", market)
	}
	if !symbolPattern.MatchString(symbol) {
		return "", "", validationError("archive symbol must contain uppercase letters, CJK letters and digits")
	}
	if year < 2017 || year > 9999 {
		return "", "", validationError("archive year must be between 2017 and 9999")
	}
	if month < time.January || month > time.December {
		return "", "", validationError("archive month must be between 1 and 12")
	}
	period, frequency = fmt.Sprintf("%04d-%02d", year, int(month)), frequencyMonthly
	if day != 0 {
		date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
		if day < 0 || date.Month() != month {
			return "", "", validationError("archive day %d is outside %s", day, period)
		}
		period, frequency = date.Format(time.DateOnly), frequencyDaily
	}
	return frequency, period, nil
}

// archiveInterval은 REST 구간 문자열을 저장소 디렉터리 이름으로 바꾼다.
// 저장소는 1개월 구간을 1M 대신 1mo로 표기하고 1초 캔들은 Spot에만 게시한다.
// 일 파일은 3d·1w·1M 구간을 받지 않는다. 공식 다운로드 스크립트의 DAILY_INTERVALS가 이 구간을 제외하며
// Spot에는 이 구간의 일 파일이 없고 USDⓈ-M에는 게시가 중단된 과거 파일이 일부 남아 있을 뿐이다.
func archiveInterval(market Market, daily bool, interval binance.KlineInterval) (string, error) {
	switch interval {
	case binance.Kline1Second:
		if market != MarketSpot {
			return "", validationError("1s archive klines are only published for Spot")
		}
		return string(interval), nil
	case binance.Kline1Minute, binance.Kline3Minutes, binance.Kline5Minutes, binance.Kline15Minutes,
		binance.Kline30Minutes, binance.Kline1Hour, binance.Kline2Hours, binance.Kline4Hours,
		binance.Kline6Hours, binance.Kline8Hours, binance.Kline12Hours, binance.Kline1Day:
		return string(interval), nil
	case binance.Kline3Days, binance.Kline1Week, binance.Kline1Month:
		if daily {
			return "", validationError("%s archive klines are only published as monthly files", interval)
		}
		if interval == binance.Kline1Month {
			return "1mo", nil
		}
		return string(interval), nil
	default:
		return "", validationError("unsupported archive kline interval %q", interval)
	}
}

func validationError(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", trade.ErrValidation, fmt.Sprintf(format, arguments...))
}
