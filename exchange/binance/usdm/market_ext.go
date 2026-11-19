package usdm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
)

const (
	// defaultKlineLimit은 캔들 계열 엔드포인트가 limit을 생략할 때 서버가 적용하는 기본값이다.
	defaultKlineLimit = 500
	// marketBucketWindow는 엔드포인트 전용 IP 제한의 rolling window다.
	marketBucketWindow = 5 * time.Minute
)

// marketBucket은 요청 weight와 별개로 IP 단위 5분 창에서 공유하는 엔드포인트 제한이다.
type marketBucket struct {
	name  string
	limit int
}

// fundingBucket은 fundingRate와 fundingInfo가 공유하는 IP당 500회/5분 제한이다.
var fundingBucket = marketBucket{name: "funding", limit: 500}

// futuresDataBucket은 /futures/data 통계 엔드포인트에 공식 문서가 적는 IP당 1,000회/5분 제한이다.
// 문서는 엔드포인트별 한도인지 공유 한도인지 밝히지 않으므로 SDK는 보수적으로 공유 한도로 차감한다.
var futuresDataBucket = marketBucket{name: "futures_data", limit: 1000}

// marketCall은 Market Data 엔드포인트별 경로, 요청 weight와 인증·제한 규칙이다.
type marketCall struct {
	path             string
	weight           int
	bucket           marketBucket
	apiKey           bool
	ignoreUsedWeight bool
}

// executeMarket은 공개 또는 API Key 전용(MARKET_DATA) Market Data GET 요청을 실행한다.
// 서명은 하지 않으며 응답의 사용 weight 헤더가 부정확한 엔드포인트는 limiter 관측에서 제외한다.
func (client *Client) executeMarket(ctx context.Context, call marketCall, values url.Values, options ...trade.RequestOption) (commonexchange.Response, error) {
	resolved, err := client.resolveOptions(options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	accountID := ""
	if call.apiKey {
		if client.credentials == nil || client.credentialProvider == nil {
			return commonexchange.Response{}, &trade.APIError{Category: trade.ErrorAuthentication, Exchange: model.ExchangeBinance, Cause: errors.New("Binance USD-M MARKET_DATA request requires an API key")}
		}
		accountID = client.credentials.AccountID
		if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
			return commonexchange.Response{}, &trade.APIError{Category: trade.ErrorAuthorization, Exchange: model.ExchangeBinance, AccountID: accountID, Cause: err}
		}
		if err := client.credentials.RequirePermission(credential.PermissionRead); err != nil {
			return commonexchange.Response{}, &trade.APIError{Category: trade.ErrorAuthorization, Exchange: model.ExchangeBinance, AccountID: accountID, Cause: err}
		}
	}
	limiter := client.executor.Limiter()
	charges, err := client.limits.charges(limiter, resolved.EgressRouteID, accountID, call.weight, 0)
	if err != nil {
		return commonexchange.Response{}, err
	}
	if call.bucket.limit > 0 {
		key := rateLimitKey("route", string(resolved.EgressRouteID), call.bucket.name, "5minutes")
		if err := limiter.SetRule(ratelimit.Rule{Key: key, Limit: call.bucket.limit, Window: marketBucketWindow}); err != nil {
			return commonexchange.Response{}, err
		}
		charges = append(charges, ratelimit.Charge{Key: key, Units: 1})
	}
	var material credential.Material
	defer material.Destroy()
	response, err := client.executor.Execute(ctx, commonexchange.Execution{Exchange: model.ExchangeBinance, AccountID: accountID, EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout, Charges: charges, Operation: commonexchange.OperationRead, Build: func(buildContext context.Context) (*http.Request, error) {
		request, requestErr := client.newRequest(http.MethodGet, call.path, values)
		if requestErr != nil {
			return nil, requestErr
		}
		if !call.apiKey {
			return request, nil
		}
		resolvedMaterial, resolveErr := client.credentialProvider.Resolve(buildContext, client.credentials.SecretRef)
		material = resolvedMaterial
		if resolveErr != nil {
			return nil, &trade.APIError{Category: trade.ErrorAuthentication, Exchange: model.ExchangeBinance, AccountID: accountID, Cause: resolveErr}
		}
		if len(material.APIKey) == 0 {
			return nil, &trade.APIError{Category: trade.ErrorAuthentication, Exchange: model.ExchangeBinance, AccountID: accountID, Cause: errors.New("Binance USD-M API key is required")}
		}
		request.Header.Set("X-MBX-APIKEY", string(material.APIKey))
		return request, nil
	}})
	if err == nil && !call.ignoreUsedWeight {
		observeHeaders(limiter, resolved.EgressRouteID, accountID, response.Header)
	}
	return response, err
}

// rawSetter는 원본 JSON을 보존하는 Market Data 응답 항목이 만족하는 제약이다.
type rawSetter[T any] interface {
	*T
	setRaw(json.RawMessage)
}

// decodeMarketItems는 객체 한 건 또는 객체 배열 응답을 항목별 Raw와 함께 목록으로 변환한다.
// symbol 생략 여부에 따라 응답 형태가 달라지는 엔드포인트도 같은 목록 타입을 반환한다.
func decodeMarketItems[T any, P rawSetter[T]](client *Client, response commonexchange.Response) ([]T, error) {
	var payload json.RawMessage
	if err := client.decode(response, commonexchange.OperationRead, &payload); err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(payload)
	rawItems := []json.RawMessage{trimmed}
	if len(trimmed) > 0 && trimmed[0] == '[' {
		rawItems = nil
		if err := json.Unmarshal(trimmed, &rawItems); err != nil {
			return nil, client.decodeError(response, commonexchange.OperationRead, err)
		}
	}
	items := make([]T, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeError(response, commonexchange.OperationRead, err)
		}
		P(&items[index]).setRaw(cloneBytes(raw))
	}
	return items, nil
}

// ContractType은 연속계약 캔들과 베이시스 조회에 쓰는 계약 분류다.
type ContractType string

const (
	ContractTypePerpetual       ContractType = "PERPETUAL"
	ContractTypeCurrentQuarter  ContractType = "CURRENT_QUARTER"
	ContractTypeNextQuarter     ContractType = "NEXT_QUARTER"
	ContractTypeTradFiPerpetual ContractType = "TRADIFI_PERPETUAL"
)

func (value ContractType) validContinuous() bool {
	switch value {
	case ContractTypePerpetual, ContractTypeCurrentQuarter, ContractTypeNextQuarter, ContractTypeTradFiPerpetual:
		return true
	default:
		return false
	}
}

// validBasis는 베이시스 조회가 허용하는 계약 분류만 통과시킨다. TradFi 무기한은 제외된다.
func (value ContractType) validBasis() bool {
	return value != ContractTypeTradFiPerpetual && value.validContinuous()
}

// StatisticsPeriod는 /futures/data 통계와 베이시스의 집계 구간이다.
type StatisticsPeriod string

const (
	StatisticsPeriod5Minutes  StatisticsPeriod = "5m"
	StatisticsPeriod15Minutes StatisticsPeriod = "15m"
	StatisticsPeriod30Minutes StatisticsPeriod = "30m"
	StatisticsPeriod1Hour     StatisticsPeriod = "1h"
	StatisticsPeriod2Hours    StatisticsPeriod = "2h"
	StatisticsPeriod4Hours    StatisticsPeriod = "4h"
	StatisticsPeriod6Hours    StatisticsPeriod = "6h"
	StatisticsPeriod12Hours   StatisticsPeriod = "12h"
	StatisticsPeriod1Day      StatisticsPeriod = "1d"
)

func (period StatisticsPeriod) valid() bool {
	switch period {
	case StatisticsPeriod5Minutes, StatisticsPeriod15Minutes, StatisticsPeriod30Minutes,
		StatisticsPeriod1Hour, StatisticsPeriod2Hours, StatisticsPeriod4Hours,
		StatisticsPeriod6Hours, StatisticsPeriod12Hours, StatisticsPeriod1Day:
		return true
	default:
		return false
	}
}

// Candle1Second는 연속계약 캔들만 지원하는 1초 구간이다.
const Candle1Second CandleInterval = "1s"

// AggregateTradesRequest는 집계 공개 체결 조회 조건이다.
// FromID와 StartTime·EndTime은 함께 보낼 수 없고, 두 시각을 모두 보내면 간격이 1시간 미만이어야 한다.
type AggregateTradesRequest struct {
	Symbol    string
	FromID    *int64
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

// AggregateTrade는 같은 가격·방향으로 100ms 안에 체결된 공개 체결을 집계한 한 건이다.
type AggregateTrade struct {
	AggregateID    int64           `json:"a"`
	Price          string          `json:"p"`
	Quantity       string          `json:"q"`
	NormalQuantity string          `json:"nq"`
	FirstTradeID   int64           `json:"f"`
	LastTradeID    int64           `json:"l"`
	Time           int64           `json:"T"`
	BuyerMaker     bool            `json:"m"`
	Raw            json.RawMessage `json:"-"`
}

func (item *AggregateTrade) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request AggregateTradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.FromID != nil {
		if *request.FromID < 0 {
			return validationError("fromId cannot be negative")
		}
		if request.StartTime != nil || request.EndTime != nil {
			return validationError("fromId cannot be combined with startTime or endTime")
		}
	}
	if err := validateLimit("aggregate trade", request.Limit, 1000); err != nil {
		return err
	}
	if err := validateTimeRange(request.StartTime, request.EndTime); err != nil {
		return err
	}
	if request.StartTime != nil && request.EndTime != nil && request.EndTime.Sub(*request.StartTime) >= time.Hour {
		return validationError("aggregate trade range must be shorter than one hour")
	}
	return nil
}

func (request AggregateTradesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	if request.FromID != nil {
		values.Set("fromId", strconv.FormatInt(*request.FromID, 10))
	}
	setTimeRange(values, request.StartTime, request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// AggregateTrades는 단일 계약의 집계 공개 체결을 조회한다. 공개 엔드포인트이며 IP weight는 20이다.
// 최근 48시간 이내 체결만 조회할 수 있다.
func (client *Client) AggregateTrades(ctx context.Context, request AggregateTradesRequest, options ...trade.RequestOption) ([]AggregateTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/aggTrades", weight: 20}, request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[AggregateTrade](client, response)
}

// HistoricalTradesRequest는 과거 공개 체결 조회 조건이다. FromID를 비우면 가장 최근 체결부터 반환한다.
type HistoricalTradesRequest struct {
	Symbol string
	FromID *int64
	Limit  int
}

func (request HistoricalTradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.FromID != nil && *request.FromID < 0 {
		return validationError("fromId cannot be negative")
	}
	return validateLimit("historical trade", request.Limit, 500)
}

func (request HistoricalTradesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	if request.FromID != nil {
		values.Set("fromId", strconv.FormatInt(*request.FromID, 10))
	}
	setLimit(values, request.Limit)
	return values
}

// HistoricalTrades는 단일 계약의 과거 공개 체결을 조회한다. 최근 1개월 데이터만 지원한다.
// MARKET_DATA 엔드포인트라 API Key(credential.PermissionRead)가 필요하며 서명 없이 X-MBX-APIKEY만 보낸다.
// IP weight는 200이다.
func (client *Client) HistoricalTrades(ctx context.Context, request HistoricalTradesRequest, options ...trade.RequestOption) ([]PublicTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/historicalTrades", weight: 200, apiKey: true}, request.values(), options...)
	if err != nil {
		return nil, err
	}
	var trades []PublicTrade
	if err := client.decode(response, commonexchange.OperationRead, &trades); err != nil {
		return nil, err
	}
	return trades, nil
}

// ContinuousCandlesRequest는 연속계약 캔들 조회 조건이다. Interval은 1초(Candle1Second)도 허용한다.
type ContinuousCandlesRequest struct {
	Pair         string
	ContractType ContractType
	Interval     CandleInterval
	StartTime    *time.Time
	EndTime      *time.Time
	Limit        int
}

func (request ContinuousCandlesRequest) validate() error {
	if err := validatePair(request.Pair); err != nil {
		return err
	}
	if !request.ContractType.validContinuous() {
		return validationError("unsupported contract type %q", request.ContractType)
	}
	if request.Interval != Candle1Second && !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	return validateKlineWindow(request.Limit, request.StartTime, request.EndTime)
}

func (request ContinuousCandlesRequest) values() url.Values {
	values := url.Values{"pair": {request.Pair}, "contractType": {string(request.ContractType)}, "interval": {string(request.Interval)}}
	setTimeRange(values, request.StartTime, request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// ContinuousCandles는 계약 분류별 연속계약 캔들을 조회한다. 공개 엔드포인트다.
// IP weight는 limit 1~99는 1, 100~499는 2, 500~1000은 5, 1001 이상은 10이며 limit 생략은 기본값 500으로 계산한다.
func (client *Client) ContinuousCandles(ctx context.Context, request ContinuousCandlesRequest, options ...trade.RequestOption) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.queryCandles(ctx, "/fapi/v1/continuousKlines", request.values(), request.Limit, options...)
}

// IndexPriceCandlesRequest는 지수가 캔들 조회 조건이다.
type IndexPriceCandlesRequest struct {
	Pair      string
	Interval  CandleInterval
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

func (request IndexPriceCandlesRequest) validate() error {
	if err := validatePair(request.Pair); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	return validateKlineWindow(request.Limit, request.StartTime, request.EndTime)
}

func (request IndexPriceCandlesRequest) values() url.Values {
	values := url.Values{"pair": {request.Pair}, "interval": {string(request.Interval)}}
	setTimeRange(values, request.StartTime, request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// IndexPriceCandles는 pair의 지수가 캔들을 조회한다. 공개 엔드포인트이며 weight 규칙은 ContinuousCandles와 같다.
// 거래량 계열 필드는 공식 문서상 의미가 없는 값이다.
func (client *Client) IndexPriceCandles(ctx context.Context, request IndexPriceCandlesRequest, options ...trade.RequestOption) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.queryCandles(ctx, "/fapi/v1/indexPriceKlines", request.values(), request.Limit, options...)
}

// MarkPriceCandlesRequest는 마크가 캔들 조회 조건이다.
type MarkPriceCandlesRequest struct {
	Symbol    string
	Interval  CandleInterval
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

func (request MarkPriceCandlesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	return validateKlineWindow(request.Limit, request.StartTime, request.EndTime)
}

func (request MarkPriceCandlesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}, "interval": {string(request.Interval)}}
	setTimeRange(values, request.StartTime, request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// MarkPriceCandles는 계약의 마크가 캔들을 조회한다. 공개 엔드포인트이며 weight 규칙은 ContinuousCandles와 같다.
// 거래량 계열 필드는 공식 문서상 의미가 없는 값이다.
func (client *Client) MarkPriceCandles(ctx context.Context, request MarkPriceCandlesRequest, options ...trade.RequestOption) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.queryCandles(ctx, "/fapi/v1/markPriceKlines", request.values(), request.Limit, options...)
}

// PremiumIndexCandlesRequest는 프리미엄 지수 캔들 조회 조건이다.
type PremiumIndexCandlesRequest struct {
	Symbol    string
	Interval  CandleInterval
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

func (request PremiumIndexCandlesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	return validateKlineWindow(request.Limit, request.StartTime, request.EndTime)
}

func (request PremiumIndexCandlesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}, "interval": {string(request.Interval)}}
	setTimeRange(values, request.StartTime, request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// PremiumIndexCandles는 계약의 프리미엄 지수 캔들을 조회한다. 공개 엔드포인트이며 weight 규칙은 ContinuousCandles와 같다.
// 가격 필드는 프리미엄 지수 값이고 거래량 계열 필드는 공식 문서상 의미가 없는 값이다.
func (client *Client) PremiumIndexCandles(ctx context.Context, request PremiumIndexCandlesRequest, options ...trade.RequestOption) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.queryCandles(ctx, "/fapi/v1/premiumIndexKlines", request.values(), request.Limit, options...)
}

func (client *Client) queryCandles(ctx context.Context, path string, values url.Values, limit int, options ...trade.RequestOption) ([]Candle, error) {
	response, err := client.executeMarket(ctx, marketCall{path: path, weight: klineWeight(limit)}, values, options...)
	if err != nil {
		return nil, err
	}
	var candles []Candle
	if err := client.decode(response, commonexchange.OperationRead, &candles); err != nil {
		return nil, err
	}
	return candles, nil
}

// klineWeight는 limit을 생략하면 서버 기본값 500을 적용해 캔들 계열 weight를 계산한다.
func klineWeight(limit int) int {
	if limit == 0 {
		limit = defaultKlineLimit
	}
	return candleWeight(limit)
}

// MarkPricesRequest는 마크가·펀딩 조회 범위다. Symbol을 비우면 전체 계약을 조회한다.
type MarkPricesRequest struct{ Symbol string }

// MarkPrice는 계약의 마크가, 지수가와 현재 펀딩 정보다.
type MarkPrice struct {
	Symbol               string          `json:"symbol"`
	MarkPrice            string          `json:"markPrice"`
	IndexPrice           string          `json:"indexPrice"`
	EstimatedSettlePrice string          `json:"estimatedSettlePrice"`
	LastFundingRate      string          `json:"lastFundingRate"`
	InterestRate         string          `json:"interestRate"`
	NextFundingTime      int64           `json:"nextFundingTime"`
	Time                 int64           `json:"time"`
	Raw                  json.RawMessage `json:"-"`
}

func (item *MarkPrice) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request MarkPricesRequest) validate() error { return validateOptionalSymbol(request.Symbol) }

func (request MarkPricesRequest) values() url.Values { return optionalSymbolValues(request.Symbol) }

// MarkPrices는 마크가, 지수가와 현재 펀딩 정보를 조회한다. 공개 엔드포인트다.
// IP weight는 Symbol을 지정하면 1, 생략하면 10이다. 단일 계약 응답도 한 건짜리 목록으로 반환한다.
func (client *Client) MarkPrices(ctx context.Context, request MarkPricesRequest, options ...trade.RequestOption) ([]MarkPrice, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/premiumIndex", weight: symbolWeight(request.Symbol, 1, 10)}, request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[MarkPrice](client, response)
}

// FundingRateHistoryRequest는 펀딩비 이력 조회 조건이다.
// 시각을 모두 생략하면 최근 200건을 오름차순으로 반환한다.
type FundingRateHistoryRequest struct {
	Symbol    string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

// FundingRate는 펀딩비 정산 한 건이다. RateType은 일반 펀딩(Regular)과 배당 특별 펀딩(Special)을 구분한다.
type FundingRate struct {
	Symbol      string          `json:"symbol"`
	FundingRate string          `json:"fundingRate"`
	FundingTime int64           `json:"fundingTime"`
	MarkPrice   string          `json:"markPrice"`
	RateType    string          `json:"rateType"`
	Raw         json.RawMessage `json:"-"`
}

func (item *FundingRate) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request FundingRateHistoryRequest) validate() error {
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateLimit("funding rate", request.Limit, 1000); err != nil {
		return err
	}
	return validateTimeRange(request.StartTime, request.EndTime)
}

func (request FundingRateHistoryRequest) values() url.Values {
	values := optionalSymbolValues(request.Symbol)
	setTimeRange(values, request.StartTime, request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// FundingRateHistory는 펀딩비 이력을 조회한다. 공개 엔드포인트다.
// 요청 weight는 쓰지 않고 fundingInfo와 IP당 500회/5분 제한을 공유하며 SDK가 route별로 차감한다.
func (client *Client) FundingRateHistory(ctx context.Context, request FundingRateHistoryRequest, options ...trade.RequestOption) ([]FundingRate, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/fundingRate", bucket: fundingBucket}, request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[FundingRate](client, response)
}

// FundingInfo는 펀딩 상·하한 또는 정산 주기가 조정된 계약의 펀딩 설정이다.
type FundingInfo struct {
	Symbol                   string          `json:"symbol"`
	AdjustedFundingRateCap   string          `json:"adjustedFundingRateCap"`
	AdjustedFundingRateFloor string          `json:"adjustedFundingRateFloor"`
	FundingIntervalHours     int             `json:"fundingIntervalHours"`
	Raw                      json.RawMessage `json:"-"`
}

func (item *FundingInfo) setRaw(raw json.RawMessage) { item.Raw = raw }

// FundingInfo는 펀딩 상·하한 또는 정산 주기가 조정된 계약의 펀딩 설정을 조회한다. 공개 엔드포인트다.
// 요청 weight는 0이고 fundingRate와 IP당 500회/5분 제한을 공유한다.
func (client *Client) FundingInfo(ctx context.Context, options ...trade.RequestOption) ([]FundingInfo, error) {
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/fundingInfo", bucket: fundingBucket}, nil, options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[FundingInfo](client, response)
}

// Ticker24hrRequest는 24시간 통계 조회 범위다. Symbol을 비우면 전체 계약을 조회한다.
type Ticker24hrRequest struct{ Symbol string }

// Ticker24hr는 24시간 롤링 가격 변동 통계다.
type Ticker24hr struct {
	Symbol             string          `json:"symbol"`
	PriceChange        string          `json:"priceChange"`
	PriceChangePercent string          `json:"priceChangePercent"`
	WeightedAvgPrice   string          `json:"weightedAvgPrice"`
	LastPrice          string          `json:"lastPrice"`
	LastQuantity       string          `json:"lastQty"`
	OpenPrice          string          `json:"openPrice"`
	HighPrice          string          `json:"highPrice"`
	LowPrice           string          `json:"lowPrice"`
	Volume             string          `json:"volume"`
	QuoteVolume        string          `json:"quoteVolume"`
	OpenTime           int64           `json:"openTime"`
	CloseTime          int64           `json:"closeTime"`
	FirstTradeID       int64           `json:"firstId"`
	LastTradeID        int64           `json:"lastId"`
	TradeCount         int64           `json:"count"`
	Raw                json.RawMessage `json:"-"`
}

func (item *Ticker24hr) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request Ticker24hrRequest) validate() error { return validateOptionalSymbol(request.Symbol) }

func (request Ticker24hrRequest) values() url.Values { return optionalSymbolValues(request.Symbol) }

// Ticker24hr는 24시간 가격 변동 통계를 조회한다. 공개 엔드포인트다.
// IP weight는 Symbol을 지정하면 1, 생략하면 40이므로 전체 조회는 신중히 사용한다.
func (client *Client) Ticker24hr(ctx context.Context, request Ticker24hrRequest, options ...trade.RequestOption) ([]Ticker24hr, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/ticker/24hr", weight: symbolWeight(request.Symbol, 1, 40)}, request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[Ticker24hr](client, response)
}

// BookTickersRequest는 최우선 호가 조회 범위다. Symbol을 비우면 전체 계약을 조회한다.
type BookTickersRequest struct{ Symbol string }

// BookTicker는 계약의 최우선 매수·매도 호가와 수량이다. RPI 주문은 포함하지 않는다.
type BookTicker struct {
	Symbol      string          `json:"symbol"`
	BidPrice    string          `json:"bidPrice"`
	BidQuantity string          `json:"bidQty"`
	AskPrice    string          `json:"askPrice"`
	AskQuantity string          `json:"askQty"`
	Time        int64           `json:"time"`
	Raw         json.RawMessage `json:"-"`
}

func (item *BookTicker) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request BookTickersRequest) validate() error { return validateOptionalSymbol(request.Symbol) }

func (request BookTickersRequest) values() url.Values { return optionalSymbolValues(request.Symbol) }

// BookTickers는 최우선 호가를 REST로 조회한다. 공개 엔드포인트다.
// IP weight는 Symbol을 지정하면 2, 생략하면 5다. 이 엔드포인트의 X-MBX-USED-WEIGHT-1M 헤더는
// 공식 문서가 부정확하다고 밝히므로 limiter에 반영하지 않는다.
func (client *Client) BookTickers(ctx context.Context, request BookTickersRequest, options ...trade.RequestOption) ([]BookTicker, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/ticker/bookTicker", weight: symbolWeight(request.Symbol, 2, 5), ignoreUsedWeight: true}, request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[BookTicker](client, response)
}

// OpenInterestRequest는 현재 미결제약정을 조회할 계약이다.
type OpenInterestRequest struct{ Symbol string }

// OpenInterest는 계약의 현재 미결제약정이다.
type OpenInterest struct {
	Symbol       string          `json:"symbol"`
	OpenInterest string          `json:"openInterest"`
	Time         int64           `json:"time"`
	Raw          json.RawMessage `json:"-"`
}

func (request OpenInterestRequest) validate() error { return validateSymbol(request.Symbol) }

func (request OpenInterestRequest) values() url.Values {
	return url.Values{"symbol": {request.Symbol}}
}

// OpenInterest는 단일 계약의 현재 미결제약정을 조회한다. 공개 엔드포인트이며 IP weight는 1이다.
func (client *Client) OpenInterest(ctx context.Context, request OpenInterestRequest, options ...trade.RequestOption) (OpenInterest, error) {
	if err := request.validate(); err != nil {
		return OpenInterest{}, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/openInterest", weight: 1}, request.values(), options...)
	if err != nil {
		return OpenInterest{}, err
	}
	var result OpenInterest
	if err := client.decode(response, commonexchange.OperationRead, &result); err != nil {
		return OpenInterest{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// MarketStatisticsRequest는 /futures/data 통계 엔드포인트의 공통 조회 조건이다.
// 시각을 모두 생략하면 최근 데이터를 반환하며 최근 30일(미결제약정 이력은 1개월) 데이터만 조회된다.
type MarketStatisticsRequest struct {
	Symbol    string
	Period    StatisticsPeriod
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

func (request MarketStatisticsRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if !request.Period.valid() {
		return validationError("unsupported statistics period %q", request.Period)
	}
	if err := validateLimit("statistics", request.Limit, 500); err != nil {
		return err
	}
	return validateTimeRange(request.StartTime, request.EndTime)
}

func (request MarketStatisticsRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}, "period": {string(request.Period)}}
	setTimeRange(values, request.StartTime, request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// executeStatistics는 /futures/data 통계 요청을 검증하고 공유 IP 제한(1,000회/5분)과 함께 실행한다.
func (client *Client) executeStatistics(ctx context.Context, path string, request MarketStatisticsRequest, apiKey bool, options ...trade.RequestOption) (commonexchange.Response, error) {
	if err := request.validate(); err != nil {
		return commonexchange.Response{}, err
	}
	return client.executeMarket(ctx, marketCall{path: path, bucket: futuresDataBucket, apiKey: apiKey}, request.values(), options...)
}

// OpenInterestStatistic은 구간별 미결제약정 통계 한 건이다.
type OpenInterestStatistic struct {
	Symbol               string          `json:"symbol"`
	SumOpenInterest      string          `json:"sumOpenInterest"`
	SumOpenInterestValue string          `json:"sumOpenInterestValue"`
	CirculatingSupply    string          `json:"CMCCirculatingSupply"`
	Timestamp            int64           `json:"timestamp"`
	Raw                  json.RawMessage `json:"-"`
}

func (item *OpenInterestStatistic) setRaw(raw json.RawMessage) { item.Raw = raw }

// OpenInterestHistory는 구간별 미결제약정 통계 이력을 조회한다. 공개 엔드포인트다.
// 요청 weight는 0이고 /futures/data IP 제한(1,000회/5분)을 차감한다. 거래가 없던 구간은 응답에서 빠진다.
func (client *Client) OpenInterestHistory(ctx context.Context, request MarketStatisticsRequest, options ...trade.RequestOption) ([]OpenInterestStatistic, error) {
	response, err := client.executeStatistics(ctx, "/futures/data/openInterestHist", request, false, options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[OpenInterestStatistic](client, response)
}

// LongShortRatio는 롱숏 비율 통계 한 건이다.
// 계정 비율 엔드포인트에서는 계정 수 비율이고 포지션 비율 엔드포인트에서는 포지션 비율이다.
type LongShortRatio struct {
	Symbol         string          `json:"symbol"`
	LongShortRatio string          `json:"longShortRatio"`
	LongAccount    string          `json:"longAccount"`
	ShortAccount   string          `json:"shortAccount"`
	Timestamp      int64           `json:"timestamp"`
	Raw            json.RawMessage `json:"-"`
}

func (item *LongShortRatio) setRaw(raw json.RawMessage) { item.Raw = raw }

// TopTraderAccountRatio는 증거금 상위 20% 트레이더의 롱·숏 계정 비율을 조회한다.
// MARKET_DATA 엔드포인트라 API Key(credential.PermissionRead)가 필요하며 요청 weight는 0이다.
func (client *Client) TopTraderAccountRatio(ctx context.Context, request MarketStatisticsRequest, options ...trade.RequestOption) ([]LongShortRatio, error) {
	response, err := client.executeStatistics(ctx, "/futures/data/topLongShortAccountRatio", request, true, options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[LongShortRatio](client, response)
}

// TopTraderPositionRatio는 증거금 상위 20% 트레이더의 롱·숏 포지션 비율을 조회한다.
// MARKET_DATA 엔드포인트라 API Key(credential.PermissionRead)가 필요하며 요청 weight는 0이다.
func (client *Client) TopTraderPositionRatio(ctx context.Context, request MarketStatisticsRequest, options ...trade.RequestOption) ([]LongShortRatio, error) {
	response, err := client.executeStatistics(ctx, "/futures/data/topLongShortPositionRatio", request, true, options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[LongShortRatio](client, response)
}

// GlobalLongShortAccountRatio는 전체 트레이더의 롱·숏 계정 비율을 조회한다.
// 공개 엔드포인트이며 요청 weight는 0이다.
func (client *Client) GlobalLongShortAccountRatio(ctx context.Context, request MarketStatisticsRequest, options ...trade.RequestOption) ([]LongShortRatio, error) {
	response, err := client.executeStatistics(ctx, "/futures/data/globalLongShortAccountRatio", request, false, options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[LongShortRatio](client, response)
}

// TakerBuySellVolume은 구간별 테이커 매수·매도 거래량 한 건이다.
type TakerBuySellVolume struct {
	BuySellRatio string          `json:"buySellRatio"`
	BuyVolume    string          `json:"buyVol"`
	SellVolume   string          `json:"sellVol"`
	Timestamp    int64           `json:"timestamp"`
	Raw          json.RawMessage `json:"-"`
}

func (item *TakerBuySellVolume) setRaw(raw json.RawMessage) { item.Raw = raw }

// TakerBuySellVolumes는 구간별 테이커 매수·매도 거래량을 조회한다.
// 공개 엔드포인트이며 요청 weight는 0이다.
func (client *Client) TakerBuySellVolumes(ctx context.Context, request MarketStatisticsRequest, options ...trade.RequestOption) ([]TakerBuySellVolume, error) {
	response, err := client.executeStatistics(ctx, "/futures/data/takerlongshortRatio", request, false, options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[TakerBuySellVolume](client, response)
}

// BasisRequest는 선물 베이시스 조회 조건이다. 최근 30일 데이터만 조회된다.
type BasisRequest struct {
	Pair         string
	ContractType ContractType
	Period       StatisticsPeriod
	StartTime    *time.Time
	EndTime      *time.Time
	Limit        int
}

// Basis는 선물 가격과 지수가의 베이시스 한 건이다.
type Basis struct {
	IndexPrice          string          `json:"indexPrice"`
	ContractType        string          `json:"contractType"`
	BasisRate           string          `json:"basisRate"`
	FuturesPrice        string          `json:"futuresPrice"`
	AnnualizedBasisRate string          `json:"annualizedBasisRate"`
	Basis               string          `json:"basis"`
	Pair                string          `json:"pair"`
	Timestamp           int64           `json:"timestamp"`
	Raw                 json.RawMessage `json:"-"`
}

func (item *Basis) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request BasisRequest) validate() error {
	if err := validatePair(request.Pair); err != nil {
		return err
	}
	if !request.ContractType.validBasis() {
		return validationError("basis contractType must be PERPETUAL, CURRENT_QUARTER, or NEXT_QUARTER")
	}
	if !request.Period.valid() {
		return validationError("unsupported statistics period %q", request.Period)
	}
	if err := validateLimit("basis", request.Limit, 500); err != nil {
		return err
	}
	return validateTimeRange(request.StartTime, request.EndTime)
}

func (request BasisRequest) values() url.Values {
	values := url.Values{"pair": {request.Pair}, "contractType": {string(request.ContractType)}, "period": {string(request.Period)}}
	setTimeRange(values, request.StartTime, request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// Basis는 선물 베이시스 통계를 조회한다. 공개 엔드포인트이며 요청 weight는 0이다.
func (client *Client) Basis(ctx context.Context, request BasisRequest, options ...trade.RequestOption) ([]Basis, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/futures/data/basis"}, request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[Basis](client, response)
}

// DeliveryPricesRequest는 분기물 정산가를 조회할 pair다.
type DeliveryPricesRequest struct{ Pair string }

// DeliveryPrice는 분기물 정산 시각과 정산가다. 정산가는 JSON 숫자 표기를 그대로 보존한다.
type DeliveryPrice struct {
	DeliveryTime  int64           `json:"deliveryTime"`
	DeliveryPrice json.Number     `json:"deliveryPrice"`
	Raw           json.RawMessage `json:"-"`
}

func (item *DeliveryPrice) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request DeliveryPricesRequest) validate() error { return validatePair(request.Pair) }

func (request DeliveryPricesRequest) values() url.Values {
	return url.Values{"pair": {request.Pair}}
}

// DeliveryPrices는 분기물 정산가 이력을 조회한다. 공개 엔드포인트이며 요청 weight는 0이다.
func (client *Client) DeliveryPrices(ctx context.Context, request DeliveryPricesRequest, options ...trade.RequestOption) ([]DeliveryPrice, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/futures/data/delivery-price"}, request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[DeliveryPrice](client, response)
}

// CompositeIndexRequest는 복합지수 구성 조회 범위다. Symbol을 비우면 전체 복합지수를 조회한다.
type CompositeIndexRequest struct{ Symbol string }

// CompositeIndexAsset은 복합지수를 구성하는 기초자산과 비중이다.
type CompositeIndexAsset struct {
	BaseAsset          string `json:"baseAsset"`
	QuoteAsset         string `json:"quoteAsset"`
	WeightInQuantity   string `json:"weightInQuantity"`
	WeightInPercentage string `json:"weightInPercentage"`
}

// CompositeIndex는 복합지수 심볼의 구성 정보다.
type CompositeIndex struct {
	Symbol        string                `json:"symbol"`
	Time          int64                 `json:"time"`
	Component     string                `json:"component"`
	BaseAssetList []CompositeIndexAsset `json:"baseAssetList"`
	Raw           json.RawMessage       `json:"-"`
}

func (item *CompositeIndex) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request CompositeIndexRequest) validate() error { return validateOptionalSymbol(request.Symbol) }

func (request CompositeIndexRequest) values() url.Values {
	return optionalSymbolValues(request.Symbol)
}

// CompositeIndexInfo는 복합지수 심볼의 구성 정보를 조회한다. 공개 엔드포인트이며 IP weight는 1이다.
func (client *Client) CompositeIndexInfo(ctx context.Context, request CompositeIndexRequest, options ...trade.RequestOption) ([]CompositeIndex, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/indexInfo", weight: 1}, request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[CompositeIndex](client, response)
}

// AssetIndexRequest는 멀티에셋 자산 지수 조회 범위다. Symbol을 비우면 전체 자산 쌍을 조회한다.
type AssetIndexRequest struct{ Symbol string }

// AssetIndex는 멀티에셋 모드의 자산 지수와 환산 버퍼·환율이다.
type AssetIndex struct {
	Symbol                string          `json:"symbol"`
	Time                  int64           `json:"time"`
	Index                 string          `json:"index"`
	BidBuffer             string          `json:"bidBuffer"`
	AskBuffer             string          `json:"askBuffer"`
	BidRate               string          `json:"bidRate"`
	AskRate               string          `json:"askRate"`
	AutoExchangeBidBuffer string          `json:"autoExchangeBidBuffer"`
	AutoExchangeAskBuffer string          `json:"autoExchangeAskBuffer"`
	AutoExchangeBidRate   string          `json:"autoExchangeBidRate"`
	AutoExchangeAskRate   string          `json:"autoExchangeAskRate"`
	Raw                   json.RawMessage `json:"-"`
}

func (item *AssetIndex) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request AssetIndexRequest) validate() error { return validateOptionalSymbol(request.Symbol) }

func (request AssetIndexRequest) values() url.Values { return optionalSymbolValues(request.Symbol) }

// AssetIndices는 멀티에셋 모드 자산 지수를 조회한다. 공개 엔드포인트다.
// IP weight는 Symbol을 지정하면 1, 생략하면 10이다.
func (client *Client) AssetIndices(ctx context.Context, request AssetIndexRequest, options ...trade.RequestOption) ([]AssetIndex, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/assetIndex", weight: symbolWeight(request.Symbol, 1, 10)}, request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[AssetIndex](client, response)
}

// IndexConstituentsRequest는 지수가 구성 거래소를 조회할 계약이다.
type IndexConstituentsRequest struct{ Symbol string }

// IndexConstituent는 지수가를 구성하는 거래소별 심볼, 가격과 비중이다.
// TradFi 무기한 계약의 구성 가격은 문서상 -1로 숨겨진다.
type IndexConstituent struct {
	Exchange string `json:"exchange"`
	Symbol   string `json:"symbol"`
	Price    string `json:"price"`
	Weight   string `json:"weight"`
}

// IndexConstituents는 계약 지수가의 구성 거래소 목록이다.
type IndexConstituents struct {
	Symbol       string             `json:"symbol"`
	Time         int64              `json:"time"`
	Constituents []IndexConstituent `json:"constituents"`
	Raw          json.RawMessage    `json:"-"`
}

func (request IndexConstituentsRequest) validate() error { return validateSymbol(request.Symbol) }

func (request IndexConstituentsRequest) values() url.Values {
	return url.Values{"symbol": {request.Symbol}}
}

// IndexConstituents는 단일 계약 지수가의 구성 거래소를 조회한다. 공개 엔드포인트이며 IP weight는 2다.
func (client *Client) IndexConstituents(ctx context.Context, request IndexConstituentsRequest, options ...trade.RequestOption) (IndexConstituents, error) {
	if err := request.validate(); err != nil {
		return IndexConstituents{}, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/constituents", weight: 2}, request.values(), options...)
	if err != nil {
		return IndexConstituents{}, err
	}
	var result IndexConstituents
	if err := client.decode(response, commonexchange.OperationRead, &result); err != nil {
		return IndexConstituents{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// InsuranceFundRequest는 보험기금 잔액 조회 범위다. Symbol을 비우면 전체 보험기금을 조회한다.
type InsuranceFundRequest struct{ Symbol string }

// InsuranceFundAsset은 보험기금의 담보 자산별 잔액 스냅샷이다.
type InsuranceFundAsset struct {
	Asset         string `json:"asset"`
	MarginBalance string `json:"marginBalance"`
	UpdateTime    int64  `json:"updateTime"`
}

// InsuranceFund는 같은 보험기금을 공유하는 계약 목록과 자산별 잔액이다.
type InsuranceFund struct {
	Symbols []string             `json:"symbols"`
	Assets  []InsuranceFundAsset `json:"assets"`
	Raw     json.RawMessage      `json:"-"`
}

func (item *InsuranceFund) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request InsuranceFundRequest) validate() error { return validateOptionalSymbol(request.Symbol) }

func (request InsuranceFundRequest) values() url.Values {
	return optionalSymbolValues(request.Symbol)
}

// InsuranceFundBalance는 보험기금 잔액 스냅샷을 조회한다. 공개 엔드포인트이며 IP weight는 1이다.
// 단일 계약 응답도 한 건짜리 목록으로 반환한다.
func (client *Client) InsuranceFundBalance(ctx context.Context, request InsuranceFundRequest, options ...trade.RequestOption) ([]InsuranceFund, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/insuranceBalance", weight: 1}, request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[InsuranceFund](client, response)
}

// SymbolAdlRiskRequest는 심볼별 ADL 위험도 조회 범위다. Symbol을 비우면 전체 계약을 조회한다.
type SymbolAdlRiskRequest struct{ Symbol string }

// SymbolAdlRisk는 계약의 ADL 위험 등급(high, medium, low)이며 30분마다 갱신된다.
type SymbolAdlRisk struct {
	Symbol     string          `json:"symbol"`
	AdlRisk    string          `json:"adlRisk"`
	UpdateTime int64           `json:"updateTime"`
	Raw        json.RawMessage `json:"-"`
}

func (item *SymbolAdlRisk) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request SymbolAdlRiskRequest) validate() error { return validateOptionalSymbol(request.Symbol) }

func (request SymbolAdlRiskRequest) values() url.Values {
	return optionalSymbolValues(request.Symbol)
}

// SymbolAdlRisks는 계약별 ADL 위험 등급을 조회한다. 공개 엔드포인트이며 IP weight는 1이다.
// 단일 계약 응답도 한 건짜리 목록으로 반환한다.
func (client *Client) SymbolAdlRisks(ctx context.Context, request SymbolAdlRiskRequest, options ...trade.RequestOption) ([]SymbolAdlRisk, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/symbolAdlRisk", weight: 1}, request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[SymbolAdlRisk](client, response)
}

// RPIOrderBookRequest는 RPI 주문을 포함한 호가 조회 조건이다. Limit은 비우거나 1000만 허용한다.
type RPIOrderBookRequest struct {
	Symbol string
	Limit  int
}

func (request RPIOrderBookRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Limit != 0 && request.Limit != 1000 {
		return validationError("rpi depth limit must be 1000 or zero")
	}
	return nil
}

func (request RPIOrderBookRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setLimit(values, request.Limit)
	return values
}

// RPIOrderBook은 RPI(Retail Price Improvement) 주문을 집계해 포함한 호가 스냅샷을 조회한다.
// 교차 가격 구간은 숨겨진다. 공개 엔드포인트이며 IP weight는 20이다.
func (client *Client) RPIOrderBook(ctx context.Context, request RPIOrderBookRequest, options ...trade.RequestOption) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/rpiDepth", weight: 20}, request.values(), options...)
	if err != nil {
		return OrderBook{}, err
	}
	var book OrderBook
	if err := client.decode(response, commonexchange.OperationRead, &book); err != nil {
		return OrderBook{}, err
	}
	book.Raw = cloneBytes(response.Body)
	return book, nil
}

// TradingSession은 TradFi 기초자산 시장의 거래 세션 한 구간이다.
// Type은 PRE_MARKET, REGULAR, AFTER_MARKET, OVERNIGHT, NO_TRADING 중 하나다.
type TradingSession struct {
	StartTime int64  `json:"startTime"`
	EndTime   int64  `json:"endTime"`
	Type      string `json:"type"`
}

// MarketSchedule은 TradFi 기초자산 시장 하나의 세션 목록이다.
type MarketSchedule struct {
	Sessions []TradingSession `json:"sessions"`
}

// TradingSchedule은 TradFi 무기한 계약 기초자산 시장의 전후 1주일 거래 세션 일정이다.
// MarketSchedules 키는 EQUITY, COMMODITY, KR_EQUITY, HK_EQUITY, CN_EQUITY, FX처럼 시장 분류를 따른다.
type TradingSchedule struct {
	UpdateTime      int64                     `json:"updateTime"`
	MarketSchedules map[string]MarketSchedule `json:"marketSchedules"`
	Raw             json.RawMessage           `json:"-"`
}

// TradingSchedule은 TradFi 무기한 계약 기초자산 시장의 거래 세션 일정을 조회한다.
// 공개 엔드포인트이며 IP weight는 5다.
func (client *Client) TradingSchedule(ctx context.Context, options ...trade.RequestOption) (TradingSchedule, error) {
	response, err := client.executeMarket(ctx, marketCall{path: "/fapi/v1/tradingSchedule", weight: 5}, nil, options...)
	if err != nil {
		return TradingSchedule{}, err
	}
	var schedule TradingSchedule
	if err := client.decode(response, commonexchange.OperationRead, &schedule); err != nil {
		return TradingSchedule{}, err
	}
	schedule.Raw = cloneBytes(response.Body)
	return schedule, nil
}

func validatePair(value string) error {
	if value == "" || strings.TrimSpace(value) != value {
		return validationError("pair is required without surrounding whitespace")
	}
	return nil
}

func validateOptionalSymbol(value string) error {
	if value == "" {
		return nil
	}
	return validateSymbol(value)
}

func validateLimit(name string, limit, maximum int) error {
	if limit < 0 || limit > maximum {
		return validationError("%s limit must be at most %d", name, maximum)
	}
	return nil
}

func validateTimeRange(start, end *time.Time) error {
	if start != nil && end != nil && start.After(*end) {
		return validationError("startTime cannot be after endTime")
	}
	return nil
}

func validateKlineWindow(limit int, start, end *time.Time) error {
	if err := validateLimit("candle", limit, 1500); err != nil {
		return err
	}
	return validateTimeRange(start, end)
}

func optionalSymbolValues(symbol string) url.Values {
	values := make(url.Values)
	set(values, "symbol", symbol)
	return values
}

func setTimeRange(values url.Values, start, end *time.Time) {
	if start != nil {
		values.Set("startTime", strconv.FormatInt(start.UnixMilli(), 10))
	}
	if end != nil {
		values.Set("endTime", strconv.FormatInt(end.UnixMilli(), 10))
	}
}

func setLimit(values url.Values, limit int) {
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
}

// symbolWeight는 symbol 지정 여부에 따라 달라지는 요청 weight를 고른다.
func symbolWeight(symbol string, single, all int) int {
	if symbol == "" {
		return all
	}
	return single
}
