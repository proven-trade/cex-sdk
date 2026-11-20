package okx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// 공개 데이터 조회에서만 사용하는 추가 상품 유형이다.
const (
	InstrumentTypeFutures InstrumentType = "FUTURES"
	InstrumentTypeOption  InstrumentType = "OPTION"
	InstrumentTypeMargin  InstrumentType = "MARGIN"
)

// extPublicCall은 인증 없는 GET endpoint의 data 배열을 항목별 Raw와 함께 decode한다.
func extPublicCall[T any](
	ctx context.Context,
	client *Client,
	path string,
	query url.Values,
	limit endpointLimit,
	setRaw func(*T, []byte),
	options ...trade.RequestOption,
) ([]T, commonexchange.Response, error) {
	response, _, err := client.executePublic(ctx, http.MethodGet, path, query, limit, options...)
	if err != nil {
		return nil, response, err
	}
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return nil, response, err
	}
	items, err := decodeItems(data, setRaw)
	if err != nil {
		return nil, response, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return items, response, nil
}

func extPublicList[T any](
	ctx context.Context,
	client *Client,
	path string,
	query url.Values,
	limit endpointLimit,
	setRaw func(*T, []byte),
	options ...trade.RequestOption,
) ([]T, error) {
	items, _, err := extPublicCall(ctx, client, path, query, limit, setRaw, options...)
	return items, err
}

// extSignedCall은 서명 요청을 보내고 data 배열을 decode한다. 응답은 빈 결과 오류 분류에 쓰인다.
func extSignedCall[T any](
	ctx context.Context,
	client *Client,
	method, path string,
	query url.Values,
	body []byte,
	limit endpointLimit,
	permission credential.Permission,
	operation commonexchange.OperationKind,
	setRaw func(*T, []byte),
	options ...trade.RequestOption,
) ([]T, commonexchange.Response, error) {
	response, _, err := client.executeSigned(
		ctx, method, path, query, body, limit, permission, operation, options...,
	)
	if err != nil {
		return nil, response, err
	}
	data, err := client.responseData(response, operation)
	if err != nil {
		return nil, response, err
	}
	items, err := decodeItems(data, setRaw)
	if err != nil {
		return nil, response, client.decodeBodyError(response, operation, err)
	}
	return items, response, nil
}

func extSignedList[T any](
	ctx context.Context,
	client *Client,
	method, path string,
	query url.Values,
	body []byte,
	limit endpointLimit,
	permission credential.Permission,
	operation commonexchange.OperationKind,
	setRaw func(*T, []byte),
	options ...trade.RequestOption,
) ([]T, error) {
	items, _, err := extSignedCall(
		ctx, client, method, path, query, body, limit, permission, operation, setRaw, options...,
	)
	return items, err
}

// extSignedOne은 data 배열의 첫 항목만 반환하며 비어 있으면 실행 상태를 보존한 오류를 반환한다.
func extSignedOne[T any](
	ctx context.Context,
	client *Client,
	method, path string,
	query url.Values,
	body []byte,
	limit endpointLimit,
	permission credential.Permission,
	operation commonexchange.OperationKind,
	setRaw func(*T, []byte),
	options ...trade.RequestOption,
) (T, error) {
	var zero T
	items, response, err := extSignedCall(
		ctx, client, method, path, query, body, limit, permission, operation, setRaw, options...,
	)
	if err != nil {
		return zero, err
	}
	if len(items) == 0 {
		return zero, client.decodeBodyError(
			response, operation, fmt.Errorf("OKX %s response is empty", path),
		)
	}
	return items[0], nil
}

func extPublicOne[T any](
	ctx context.Context,
	client *Client,
	path string,
	query url.Values,
	limit endpointLimit,
	setRaw func(*T, []byte),
	options ...trade.RequestOption,
) (T, error) {
	var zero T
	items, response, err := extPublicCall(ctx, client, path, query, limit, setRaw, options...)
	if err != nil {
		return zero, err
	}
	if len(items) == 0 {
		return zero, client.decodeBodyError(
			response, commonexchange.OperationRead, fmt.Errorf("OKX %s response is empty", path),
		)
	}
	return items[0], nil
}

// extJoinCSV는 쉼표로 구분하는 목록 파라미터를 검증해 하나의 문자열로 만든다.
func extJoinCSV(name string, values []string, maximum int) (string, error) {
	if len(values) > maximum {
		return "", validationError("%s accepts at most %d values", name, maximum)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateRequiredText(name, value); err != nil {
			return "", err
		}
		if strings.Contains(value, ",") {
			return "", validationError("%s value cannot contain a comma", name)
		}
		if _, exists := seen[value]; exists {
			return "", validationError("duplicate %s %q", name, value)
		}
		seen[value] = struct{}{}
	}
	return strings.Join(values, ","), nil
}

func extValidateLimit(name string, limit, maximum int) error {
	if limit < 0 || limit > maximum {
		return validationError("%s limit must be between 1 and %d or zero for default", name, maximum)
	}
	return nil
}

func extValidateInstrumentType(instrumentType InstrumentType, allowed ...InstrumentType) error {
	for _, candidate := range allowed {
		if instrumentType == candidate {
			return nil
		}
	}
	names := make([]string, len(allowed))
	for index, candidate := range allowed {
		names[index] = string(candidate)
	}
	return validationError("instrument type must be one of %s", strings.Join(names, ", "))
}

func extValidatePageTimes(name string, after, before *time.Time) error {
	if after != nil && before != nil && !before.Before(*after) {
		return validationError("%s before must be earlier than after", name)
	}
	return nil
}

// ---- 마크가격 ----

// MarkPriceRequest는 마크가격 조회 조건이다.
type MarkPriceRequest struct {
	InstrumentType   InstrumentType
	InstrumentFamily string
	InstrumentID     string
}

// MarkPrice는 상품의 마크가격이다.
type MarkPrice struct {
	InstrumentType InstrumentType  `json:"instType"`
	InstrumentID   string          `json:"instId"`
	MarkPrice      string          `json:"markPx"`
	Timestamp      string          `json:"ts"`
	Raw            json.RawMessage `json:"-"`
}

func (request MarkPriceRequest) validate() error {
	if err := extValidateInstrumentType(
		request.InstrumentType, InstrumentTypeMargin, InstrumentTypeSwap, InstrumentTypeFutures, InstrumentTypeOption,
	); err != nil {
		return err
	}
	if err := validateOptionalText("instFamily", request.InstrumentFamily); err != nil {
		return err
	}
	return validateOptionalText("instId", request.InstrumentID)
}

func (request MarkPriceRequest) values() url.Values {
	values := instrumentTypeValues(request.InstrumentType)
	setIfNotEmpty(values, "instFamily", request.InstrumentFamily)
	setIfNotEmpty(values, "instId", request.InstrumentID)
	return values
}

// MarkPrices는 공개 마크가격을 조회한다. 공개 endpoint이며 인증이 필요 없다.
func (client *Client) MarkPrices(
	ctx context.Context,
	request MarkPriceRequest,
	options ...trade.RequestOption,
) ([]MarkPrice, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return extPublicList(
		ctx, client, "/api/v5/public/mark-price", request.values(),
		publicLimit(10, 2*time.Second, request.InstrumentID),
		func(item *MarkPrice, raw []byte) { item.Raw = raw }, options...,
	)
}

// ---- 펀딩비 ----

// FundingRateRequest는 현재 펀딩비 조회 조건이다. InstrumentID에 ANY를 쓰면 전체 perp를 반환한다.
type FundingRateRequest struct {
	InstrumentID string
}

// FundingRate는 현재 펀딩비 정보다.
type FundingRate struct {
	InstrumentType        InstrumentType  `json:"instType"`
	InstrumentID          string          `json:"instId"`
	Method                string          `json:"method"`
	FormulaType           string          `json:"formulaType"`
	FundingRate           string          `json:"fundingRate"`
	NextFundingRate       string          `json:"nextFundingRate"`
	FundingTime           string          `json:"fundingTime"`
	NextFundingTime       string          `json:"nextFundingTime"`
	MinimumFundingRate    string          `json:"minFundingRate"`
	MaximumFundingRate    string          `json:"maxFundingRate"`
	InterestRate          string          `json:"interestRate"`
	ImpactValue           string          `json:"impactValue"`
	SettlementState       string          `json:"settState"`
	SettlementFundingRate string          `json:"settFundingRate"`
	Premium               string          `json:"premium"`
	Timestamp             string          `json:"ts"`
	Raw                   json.RawMessage `json:"-"`
}

// FundingRates는 현재 펀딩비를 조회한다.
func (client *Client) FundingRates(
	ctx context.Context,
	request FundingRateRequest,
	options ...trade.RequestOption,
) ([]FundingRate, error) {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return nil, err
	}
	values := make(url.Values)
	values.Set("instId", request.InstrumentID)
	return extPublicList(
		ctx, client, "/api/v5/public/funding-rate", values,
		publicLimit(10, 2*time.Second, request.InstrumentID),
		func(item *FundingRate, raw []byte) { item.Raw = raw }, options...,
	)
}

// FundingRateHistoryRequest는 펀딩비 이력 조회 조건이다. After/Before는 fundingTime 기준이다.
type FundingRateHistoryRequest struct {
	InstrumentID string
	After        *time.Time
	Before       *time.Time
	Limit        int
}

// FundingRateRecord는 정산된 펀딩비 이력 한 건이다.
type FundingRateRecord struct {
	InstrumentType InstrumentType  `json:"instType"`
	InstrumentID   string          `json:"instId"`
	FormulaType    string          `json:"formulaType"`
	FundingRate    string          `json:"fundingRate"`
	RealizedRate   string          `json:"realizedRate"`
	FundingTime    string          `json:"fundingTime"`
	Method         string          `json:"method"`
	Raw            json.RawMessage `json:"-"`
}

func (request FundingRateHistoryRequest) validate() error {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	if err := extValidatePageTimes("funding rate history", request.After, request.Before); err != nil {
		return err
	}
	return extValidateLimit("funding rate history", request.Limit, 400)
}

func (request FundingRateHistoryRequest) values() url.Values {
	values := make(url.Values)
	values.Set("instId", request.InstrumentID)
	setTime(values, "after", request.After)
	setTime(values, "before", request.Before)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

// FundingRateHistory는 최근 3개월 펀딩비 정산 이력을 조회한다.
func (client *Client) FundingRateHistory(
	ctx context.Context,
	request FundingRateHistoryRequest,
	options ...trade.RequestOption,
) ([]FundingRateRecord, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return extPublicList(
		ctx, client, "/api/v5/public/funding-rate-history", request.values(),
		publicLimit(10, 2*time.Second, request.InstrumentID),
		func(item *FundingRateRecord, raw []byte) { item.Raw = raw }, options...,
	)
}

// ---- 미결제약정 ----

// OpenInterestRequest는 미결제약정 조회 조건이다. OPTION은 InstrumentFamily가 필요하다.
type OpenInterestRequest struct {
	InstrumentType   InstrumentType
	InstrumentFamily string
	InstrumentID     string
}

// OpenInterest는 상품의 미결제약정이다.
type OpenInterest struct {
	InstrumentType   InstrumentType  `json:"instType"`
	InstrumentID     string          `json:"instId"`
	OpenInterest     string          `json:"oi"`
	OpenInterestCoin string          `json:"oiCcy"`
	OpenInterestUSD  string          `json:"oiUsd"`
	Timestamp        string          `json:"ts"`
	Raw              json.RawMessage `json:"-"`
}

func (request OpenInterestRequest) validate() error {
	if err := extValidateInstrumentType(
		request.InstrumentType, InstrumentTypeSwap, InstrumentTypeFutures, InstrumentTypeOption,
	); err != nil {
		return err
	}
	if err := validateOptionalText("instFamily", request.InstrumentFamily); err != nil {
		return err
	}
	if err := validateOptionalText("instId", request.InstrumentID); err != nil {
		return err
	}
	if request.InstrumentType == InstrumentTypeOption && request.InstrumentFamily == "" {
		return validationError("OPTION open interest requires instFamily")
	}
	return nil
}

func (request OpenInterestRequest) values() url.Values {
	values := instrumentTypeValues(request.InstrumentType)
	setIfNotEmpty(values, "instFamily", request.InstrumentFamily)
	setIfNotEmpty(values, "instId", request.InstrumentID)
	return values
}

// OpenInterests는 미결제약정을 조회한다.
func (client *Client) OpenInterests(
	ctx context.Context,
	request OpenInterestRequest,
	options ...trade.RequestOption,
) ([]OpenInterest, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return extPublicList(
		ctx, client, "/api/v5/public/open-interest", request.values(),
		publicLimit(20, 2*time.Second, request.InstrumentID),
		func(item *OpenInterest, raw []byte) { item.Raw = raw }, options...,
	)
}

// ---- 가격 제한 ----

// PriceLimit는 상품의 주문 가능 가격 상·하한이다.
type PriceLimit struct {
	InstrumentType InstrumentType  `json:"instType"`
	InstrumentID   string          `json:"instId"`
	BuyLimit       string          `json:"buyLmt"`
	SellLimit      string          `json:"sellLmt"`
	Timestamp      string          `json:"ts"`
	Enabled        bool            `json:"enabled"`
	Raw            json.RawMessage `json:"-"`
}

// PriceLimits는 상품의 최고 매수가·최저 매도가를 조회한다.
func (client *Client) PriceLimits(
	ctx context.Context,
	instrumentID string,
	options ...trade.RequestOption,
) (PriceLimit, error) {
	if err := validateRequiredText("instId", instrumentID); err != nil {
		return PriceLimit{}, err
	}
	values := make(url.Values)
	values.Set("instId", instrumentID)
	return extPublicOne(
		ctx, client, "/api/v5/public/price-limit", values, publicLimit(20, 2*time.Second, ""),
		func(item *PriceLimit, raw []byte) { item.Raw = raw }, options...,
	)
}

// ---- 전체 깊이 호가 ----

// OrderBookFullRequest는 전체 깊이 호가 조회 조건이다. Size는 최대 5,000이다.
type OrderBookFullRequest struct {
	InstrumentID string
	Size         int
}

// OrderBookFull은 /market/books-full로 최대 5,000단계의 호가를 조회한다.
func (client *Client) OrderBookFull(
	ctx context.Context,
	request OrderBookFullRequest,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return OrderBook{}, err
	}
	if err := extValidateLimit("order book depth", request.Size, 5000); err != nil {
		return OrderBook{}, err
	}
	values := make(url.Values)
	values.Set("instId", request.InstrumentID)
	setPositiveInt(values, "sz", request.Size)
	return extPublicOne(
		ctx, client, "/api/v5/market/books-full", values, publicLimit(10, 2*time.Second, ""),
		func(item *OrderBook, raw []byte) { item.Raw = raw }, options...,
	)
}

// ---- 지수 ----

// IndexTickersRequest는 지수 시세 조회 조건이다. QuoteCurrency 또는 InstrumentID 중 하나가 필요하다.
type IndexTickersRequest struct {
	QuoteCurrency string
	InstrumentID  string
}

// IndexTicker는 지수 시세다.
type IndexTicker struct {
	InstrumentID string          `json:"instId"`
	IndexPrice   string          `json:"idxPx"`
	High24h      string          `json:"high24h"`
	Low24h       string          `json:"low24h"`
	Open24h      string          `json:"open24h"`
	StartUTC0    string          `json:"sodUtc0"`
	StartUTC8    string          `json:"sodUtc8"`
	Timestamp    string          `json:"ts"`
	Raw          json.RawMessage `json:"-"`
}

// IndexTickers는 지수 시세를 조회한다.
func (client *Client) IndexTickers(
	ctx context.Context,
	request IndexTickersRequest,
	options ...trade.RequestOption,
) ([]IndexTicker, error) {
	if err := validateOptionalText("quoteCcy", request.QuoteCurrency); err != nil {
		return nil, err
	}
	if err := validateOptionalText("instId", request.InstrumentID); err != nil {
		return nil, err
	}
	if request.QuoteCurrency == "" && request.InstrumentID == "" {
		return nil, validationError("quoteCcy or instId is required")
	}
	values := make(url.Values)
	setIfNotEmpty(values, "quoteCcy", request.QuoteCurrency)
	setIfNotEmpty(values, "instId", request.InstrumentID)
	return extPublicList(
		ctx, client, "/api/v5/market/index-tickers", values, publicLimit(20, 2*time.Second, ""),
		func(item *IndexTicker, raw []byte) { item.Raw = raw }, options...,
	)
}

// PriceCandlesRequest는 지수·마크가격 캔들 조회 조건이다. Limit 최대값은 100이다.
type PriceCandlesRequest struct {
	InstrumentID string
	Interval     CandleInterval
	After        *time.Time
	Before       *time.Time
	Limit        int
}

// PriceCandle은 [ts,o,h,l,c,confirm] 형태의 지수·마크가격 캔들이다.
type PriceCandle struct {
	Timestamp string
	Open      string
	High      string
	Low       string
	Close     string
	Confirmed bool
}

// UnmarshalJSON은 위치 기반 OKX 지수·마크가격 캔들 배열을 PriceCandle로 변환한다.
func (candle *PriceCandle) UnmarshalJSON(data []byte) error {
	var fields []string
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode OKX price candle: %w", err)
	}
	if len(fields) < 6 {
		return fmt.Errorf("OKX price candle has %d fields, want at least 6", len(fields))
	}
	candle.Timestamp = fields[0]
	candle.Open = fields[1]
	candle.High = fields[2]
	candle.Low = fields[3]
	candle.Close = fields[4]
	candle.Confirmed = fields[5] == "1"
	return nil
}

func (request PriceCandlesRequest) validate() error {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	if err := validateOptionalText("bar", string(request.Interval)); err != nil {
		return err
	}
	if err := extValidatePageTimes("price candle", request.After, request.Before); err != nil {
		return err
	}
	return extValidateLimit("price candle", request.Limit, 100)
}

func (request PriceCandlesRequest) values() url.Values {
	values := make(url.Values)
	values.Set("instId", request.InstrumentID)
	setIfNotEmpty(values, "bar", string(request.Interval))
	setTime(values, "after", request.After)
	setTime(values, "before", request.Before)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (client *Client) priceCandles(
	ctx context.Context,
	path string,
	request PriceCandlesRequest,
	limit endpointLimit,
	options ...trade.RequestOption,
) ([]PriceCandle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return extPublicList[PriceCandle](ctx, client, path, request.values(), limit, nil, options...)
}

// IndexCandles는 최근 1,440개 지수 캔들을 조회한다.
func (client *Client) IndexCandles(
	ctx context.Context,
	request PriceCandlesRequest,
	options ...trade.RequestOption,
) ([]PriceCandle, error) {
	return client.priceCandles(
		ctx, "/api/v5/market/index-candles", request, publicLimit(20, 2*time.Second, ""), options...,
	)
}

// HistoryIndexCandles는 과거 지수 캔들을 조회한다.
func (client *Client) HistoryIndexCandles(
	ctx context.Context,
	request PriceCandlesRequest,
	options ...trade.RequestOption,
) ([]PriceCandle, error) {
	return client.priceCandles(
		ctx, "/api/v5/market/history-index-candles", request, publicLimit(10, 2*time.Second, ""), options...,
	)
}

// MarkPriceCandles는 최근 1,440개 마크가격 캔들을 조회한다.
func (client *Client) MarkPriceCandles(
	ctx context.Context,
	request PriceCandlesRequest,
	options ...trade.RequestOption,
) ([]PriceCandle, error) {
	return client.priceCandles(
		ctx, "/api/v5/market/mark-price-candles", request, publicLimit(20, 2*time.Second, ""), options...,
	)
}

// HistoryMarkPriceCandles는 과거 마크가격 캔들을 조회한다.
func (client *Client) HistoryMarkPriceCandles(
	ctx context.Context,
	request PriceCandlesRequest,
	options ...trade.RequestOption,
) ([]PriceCandle, error) {
	return client.priceCandles(
		ctx, "/api/v5/market/history-mark-price-candles", request,
		publicLimit(20, 2*time.Second, ""), options...,
	)
}

// ---- 프리미엄·추정가 ----

// PremiumHistoryRequest는 프리미엄 지수 이력 조회 조건이다. SWAP에만 적용된다.
type PremiumHistoryRequest struct {
	InstrumentID string
	After        *time.Time
	Before       *time.Time
	Limit        int
}

// PremiumRecord는 프리미엄 지수 한 건이다.
type PremiumRecord struct {
	InstrumentID string          `json:"instId"`
	Premium      string          `json:"premium"`
	Timestamp    string          `json:"ts"`
	Raw          json.RawMessage `json:"-"`
}

// PremiumHistory는 SWAP 프리미엄 지수 이력을 조회한다.
func (client *Client) PremiumHistory(
	ctx context.Context,
	request PremiumHistoryRequest,
	options ...trade.RequestOption,
) ([]PremiumRecord, error) {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return nil, err
	}
	if err := extValidatePageTimes("premium history", request.After, request.Before); err != nil {
		return nil, err
	}
	if err := extValidateLimit("premium history", request.Limit, 100); err != nil {
		return nil, err
	}
	values := make(url.Values)
	values.Set("instId", request.InstrumentID)
	setTime(values, "after", request.After)
	setTime(values, "before", request.Before)
	setPositiveInt(values, "limit", request.Limit)
	return extPublicList(
		ctx, client, "/api/v5/public/premium-history", values, publicLimit(20, 2*time.Second, ""),
		func(item *PremiumRecord, raw []byte) { item.Raw = raw }, options...,
	)
}

// EstimatedPrice는 만기 선물·옵션의 추정 인도/행사 가격이다.
type EstimatedPrice struct {
	InstrumentType InstrumentType  `json:"instType"`
	InstrumentID   string          `json:"instId"`
	SettlePrice    string          `json:"settlePx"`
	Timestamp      string          `json:"ts"`
	Raw            json.RawMessage `json:"-"`
}

// EstimatedPrices는 FUTURES/OPTION 상품의 추정 인도·행사 가격을 조회한다.
func (client *Client) EstimatedPrices(
	ctx context.Context,
	instrumentID string,
	options ...trade.RequestOption,
) (EstimatedPrice, error) {
	if err := validateRequiredText("instId", instrumentID); err != nil {
		return EstimatedPrice{}, err
	}
	values := make(url.Values)
	values.Set("instId", instrumentID)
	return extPublicOne(
		ctx, client, "/api/v5/public/estimated-price", values, publicLimit(10, 2*time.Second, instrumentID),
		func(item *EstimatedPrice, raw []byte) { item.Raw = raw }, options...,
	)
}

// ---- 담보 할인율·차입 이자 ----

// DiscountRateTier는 담보 할인율 구간이다.
type DiscountRateTier struct {
	DiscountRate       string `json:"discountRate"`
	LiquidationPenalty string `json:"liqPenaltyRate"`
	MaximumAmount      string `json:"maxAmt"`
	MinimumAmount      string `json:"minAmt"`
	Tier               string `json:"tier"`
	DiscountEquity     string `json:"disCcyEq"`
}

// DiscountRate는 통화별 담보 할인율과 무이자 한도다.
type DiscountRate struct {
	Currency            string             `json:"ccy"`
	CollateralRestrict  string             `json:"colRes"`
	InterestFreeQuota   string             `json:"amt"`
	MinimumDiscountRate string             `json:"minDiscountRate"`
	Details             []DiscountRateTier `json:"details"`
	Raw                 json.RawMessage    `json:"-"`
}

// DiscountRateInterestFreeQuota는 담보 할인율과 무이자 한도를 조회한다. Currency는 선택이다.
func (client *Client) DiscountRateInterestFreeQuota(
	ctx context.Context,
	currency string,
	options ...trade.RequestOption,
) ([]DiscountRate, error) {
	if err := validateOptionalText("ccy", currency); err != nil {
		return nil, err
	}
	values := make(url.Values)
	setIfNotEmpty(values, "ccy", currency)
	return extPublicList(
		ctx, client, "/api/v5/public/discount-rate-interest-free-quota", values,
		publicLimit(2, 2*time.Second, ""),
		func(item *DiscountRate, raw []byte) { item.Raw = raw }, options...,
	)
}

// LoanQuotaBasic은 통화별 기본 차입 이자율과 한도다.
type LoanQuotaBasic struct {
	Currency string `json:"ccy"`
	Quota    string `json:"quota"`
	Rate     string `json:"rate"`
}

// LoanQuotaLevel은 VIP·일반 등급별 이자 할인과 한도 계수다.
type LoanQuotaLevel struct {
	Level                string `json:"level"`
	InterestDiscount     string `json:"irDiscount"`
	LoanQuotaCoefficient string `json:"loanQuotaCoef"`
}

// InterestRateLoanQuota는 차입 이자율과 한도 정보다.
type InterestRateLoanQuota struct {
	Basic   []LoanQuotaBasic `json:"basic"`
	VIP     []LoanQuotaLevel `json:"vip"`
	Regular []LoanQuotaLevel `json:"regular"`
	Raw     json.RawMessage  `json:"-"`
}

// InterestRateLoanQuotas는 차입 이자율과 한도를 조회한다.
func (client *Client) InterestRateLoanQuotas(
	ctx context.Context,
	options ...trade.RequestOption,
) (InterestRateLoanQuota, error) {
	return extPublicOne(
		ctx, client, "/api/v5/public/interest-rate-loan-quota", nil, publicLimit(2, 2*time.Second, ""),
		func(item *InterestRateLoanQuota, raw []byte) { item.Raw = raw }, options...,
	)
}

// ---- 포지션 티어·보험기금 ----

// PositionTiersRequest는 포지션 티어 조회 조건이다.
// SWAP/FUTURES/OPTION은 InstrumentFamily, MARGIN은 InstrumentID 또는 Currency가 필요하다.
type PositionTiersRequest struct {
	InstrumentType   InstrumentType
	TradeMode        TradeMode
	InstrumentFamily string
	InstrumentID     string
	Currency         string
	Tier             string
}

// PositionTier는 포지션 티어별 증거금률과 최대 레버리지다.
type PositionTier struct {
	Underlying       string          `json:"uly"`
	InstrumentFamily string          `json:"instFamily"`
	InstrumentID     string          `json:"instId"`
	Tier             string          `json:"tier"`
	MinimumSize      string          `json:"minSz"`
	MaximumSize      string          `json:"maxSz"`
	MaintenanceRate  string          `json:"mmr"`
	InitialRate      string          `json:"imr"`
	MaximumLeverage  string          `json:"maxLever"`
	OptionFactor     string          `json:"optMgnFactor"`
	QuoteMaxLoan     string          `json:"quoteMaxLoan"`
	BaseMaxLoan      string          `json:"baseMaxLoan"`
	Raw              json.RawMessage `json:"-"`
}

func (request PositionTiersRequest) validate() error {
	if err := extValidateInstrumentType(
		request.InstrumentType, InstrumentTypeMargin, InstrumentTypeSwap, InstrumentTypeFutures, InstrumentTypeOption,
	); err != nil {
		return err
	}
	if request.TradeMode != TradeModeCross && request.TradeMode != TradeModeIsolated {
		return validationError("trade mode must be cross or isolated")
	}
	for name, value := range map[string]string{
		"instFamily": request.InstrumentFamily, "instId": request.InstrumentID,
		"ccy": request.Currency, "tier": request.Tier,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return err
		}
	}
	if request.InstrumentType == InstrumentTypeMargin {
		if request.InstrumentID == "" && request.Currency == "" {
			return validationError("MARGIN position tiers require instId or ccy")
		}
	} else if request.InstrumentFamily == "" {
		return validationError("%s position tiers require instFamily", request.InstrumentType)
	}
	return nil
}

func (request PositionTiersRequest) values() url.Values {
	values := instrumentTypeValues(request.InstrumentType)
	values.Set("tdMode", string(request.TradeMode))
	setIfNotEmpty(values, "instFamily", request.InstrumentFamily)
	setIfNotEmpty(values, "instId", request.InstrumentID)
	setIfNotEmpty(values, "ccy", request.Currency)
	setIfNotEmpty(values, "tier", request.Tier)
	return values
}

// PositionTiers는 포지션 티어와 최대 레버리지 정보를 조회한다.
func (client *Client) PositionTiers(
	ctx context.Context,
	request PositionTiersRequest,
	options ...trade.RequestOption,
) ([]PositionTier, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return extPublicList(
		ctx, client, "/api/v5/public/position-tiers", request.values(), publicLimit(10, 2*time.Second, ""),
		func(item *PositionTier, raw []byte) { item.Raw = raw }, options...,
	)
}

// InsuranceFundRequest는 보험기금 조회 조건이다.
// FUTURES/SWAP/OPTION은 InstrumentFamily가 필요하고 MARGIN은 Currency를 사용한다.
type InsuranceFundRequest struct {
	InstrumentType   InstrumentType
	Type             string
	InstrumentFamily string
	Currency         string
	After            *time.Time
	Before           *time.Time
	Limit            int
}

// InsuranceFundDetail은 보험기금 변동 한 건이다.
type InsuranceFundDetail struct {
	Balance          string `json:"balance"`
	Amount           string `json:"amt"`
	Currency         string `json:"ccy"`
	Type             string `json:"type"`
	MaximumBalance   string `json:"maxBal"`
	MaximumBalanceTs string `json:"maxBalTs"`
	DeclineRate      string `json:"decRate"`
	ADLType          string `json:"adlType"`
	Timestamp        string `json:"ts"`
}

// InsuranceFund는 보험기금 총액과 변동 내역이다.
type InsuranceFund struct {
	Total            string                `json:"total"`
	InstrumentFamily string                `json:"instFamily"`
	InstrumentType   InstrumentType        `json:"instType"`
	Details          []InsuranceFundDetail `json:"details"`
	Raw              json.RawMessage       `json:"-"`
}

func (request InsuranceFundRequest) validate() error {
	if err := extValidateInstrumentType(
		request.InstrumentType, InstrumentTypeMargin, InstrumentTypeSwap, InstrumentTypeFutures, InstrumentTypeOption,
	); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"type": request.Type, "instFamily": request.InstrumentFamily, "ccy": request.Currency,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return err
		}
	}
	if request.InstrumentType != InstrumentTypeMargin && request.InstrumentFamily == "" {
		return validationError("%s insurance fund requires instFamily", request.InstrumentType)
	}
	if err := extValidatePageTimes("insurance fund", request.After, request.Before); err != nil {
		return err
	}
	return extValidateLimit("insurance fund", request.Limit, 100)
}

func (request InsuranceFundRequest) values() url.Values {
	values := instrumentTypeValues(request.InstrumentType)
	setIfNotEmpty(values, "type", request.Type)
	setIfNotEmpty(values, "instFamily", request.InstrumentFamily)
	setIfNotEmpty(values, "ccy", request.Currency)
	setTime(values, "after", request.After)
	setTime(values, "before", request.Before)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

// InsuranceFunds는 보험기금 잔고와 변동 내역을 조회한다.
func (client *Client) InsuranceFunds(
	ctx context.Context,
	request InsuranceFundRequest,
	options ...trade.RequestOption,
) ([]InsuranceFund, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return extPublicList(
		ctx, client, "/api/v5/public/insurance-fund", request.values(), publicLimit(10, 2*time.Second, ""),
		func(item *InsuranceFund, raw []byte) { item.Raw = raw }, options...,
	)
}

// ---- 시스템 상태·공지 ----

// SystemStatusRequest는 시스템 점검 상태 조회 조건이다. State는 scheduled, ongoing, pre_open, completed, canceled 중 하나다.
type SystemStatusRequest struct {
	State string
}

// SystemMaintenance는 시스템 점검 안내 한 건이다.
type SystemMaintenance struct {
	Title        string          `json:"title"`
	State        string          `json:"state"`
	Begin        string          `json:"begin"`
	End          string          `json:"end"`
	PreOpenBegin string          `json:"preOpenBegin"`
	Href         string          `json:"href"`
	ServiceType  string          `json:"serviceType"`
	System       string          `json:"system"`
	Description  string          `json:"scheDesc"`
	MaintType    string          `json:"maintType"`
	Environment  string          `json:"env"`
	ID           string          `json:"id"`
	Raw          json.RawMessage `json:"-"`
}

// SystemStatus는 /system/status로 예정·진행 중인 시스템 점검을 조회한다.
func (client *Client) SystemStatus(
	ctx context.Context,
	request SystemStatusRequest,
	options ...trade.RequestOption,
) ([]SystemMaintenance, error) {
	if err := validateOptionalText("state", request.State); err != nil {
		return nil, err
	}
	values := make(url.Values)
	setIfNotEmpty(values, "state", request.State)
	return extPublicList(
		ctx, client, "/api/v5/system/status", values, publicLimit(1, 5*time.Second, ""),
		func(item *SystemMaintenance, raw []byte) { item.Raw = raw }, options...,
	)
}

// AnnouncementsRequest는 공지 조회 조건이다. AnnouncementType은 AnnouncementTypes 응답의 annType이다.
type AnnouncementsRequest struct {
	AnnouncementType string
	Page             int
}

// AnnouncementItem은 공지 한 건이다.
type AnnouncementItem struct {
	Title            string `json:"title"`
	AnnouncementType string `json:"annType"`
	BusinessTime     string `json:"businessPTime"`
	PublishTime      string `json:"pTime"`
	URL              string `json:"url"`
}

// AnnouncementPage는 공지 한 페이지다.
type AnnouncementPage struct {
	TotalPage string             `json:"totalPage"`
	Details   []AnnouncementItem `json:"details"`
	Raw       json.RawMessage    `json:"-"`
}

// Announcements는 OKX 공지를 페이지 단위로 조회한다.
func (client *Client) Announcements(
	ctx context.Context,
	request AnnouncementsRequest,
	options ...trade.RequestOption,
) (AnnouncementPage, error) {
	if err := validateOptionalText("annType", request.AnnouncementType); err != nil {
		return AnnouncementPage{}, err
	}
	if request.Page < 0 {
		return AnnouncementPage{}, validationError("announcement page cannot be negative")
	}
	values := make(url.Values)
	setIfNotEmpty(values, "annType", request.AnnouncementType)
	setPositiveInt(values, "page", request.Page)
	return extPublicOne(
		ctx, client, "/api/v5/support/announcements", values, publicLimit(5, 2*time.Second, ""),
		func(item *AnnouncementPage, raw []byte) { item.Raw = raw }, options...,
	)
}

// AnnouncementType은 공지 분류다.
type AnnouncementType struct {
	Type        string          `json:"annType"`
	Description string          `json:"annTypeDesc"`
	Raw         json.RawMessage `json:"-"`
}

// AnnouncementTypes는 공지 분류 목록을 조회한다.
func (client *Client) AnnouncementTypes(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]AnnouncementType, error) {
	return extPublicList(
		ctx, client, "/api/v5/support/announcement-types", nil, publicLimit(1, 2*time.Second, ""),
		func(item *AnnouncementType, raw []byte) { item.Raw = raw }, options...,
	)
}
