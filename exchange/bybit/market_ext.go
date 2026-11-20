package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// DataPeriod는 미결제약정과 롱숏 비율 조회의 집계 구간이다.
type DataPeriod string

const (
	DataPeriod5Minutes  DataPeriod = "5min"
	DataPeriod15Minutes DataPeriod = "15min"
	DataPeriod30Minutes DataPeriod = "30min"
	DataPeriod1Hour     DataPeriod = "1h"
	DataPeriod4Hours    DataPeriod = "4h"
	DataPeriod1Day      DataPeriod = "1d"
)

func (period DataPeriod) valid() bool {
	switch period {
	case DataPeriod5Minutes, DataPeriod15Minutes, DataPeriod30Minutes,
		DataPeriod1Hour, DataPeriod4Hours, DataPeriod1Day:
		return true
	default:
		return false
	}
}

// PriceCandle은 마크·지수·프리미엄 지수 가격 캔들 한 건이다.
// 거래량이 없으므로 OHLC만 담으며 프리미엄 지수 값은 음수일 수 있다.
type PriceCandle struct {
	StartTime string
	Open      string
	High      string
	Low       string
	Close     string
}

// UnmarshalJSON은 위치 기반 Bybit 가격 캔들 배열을 PriceCandle로 변환한다.
func (candle *PriceCandle) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode Bybit price candle: %w", err)
	}
	if len(fields) < 5 {
		return fmt.Errorf("Bybit price candle has %d fields, want at least 5", len(fields))
	}
	targets := []*string{&candle.StartTime, &candle.Open, &candle.High, &candle.Low, &candle.Close}
	for index, target := range targets {
		if err := json.Unmarshal(fields[index], target); err != nil {
			return fmt.Errorf("decode Bybit price candle field %d: %w", index, err)
		}
	}
	return nil
}

// FundingRateHistoryRequest는 Linear 펀딩비 이력 조회 조건이다.
// StartTime만 지정하면 Bybit가 오류를 반환하므로 로컬에서 먼저 거부한다.
// 둘 다 비우면 현재 시각까지 최근 200건, EndTime만 지정하면 EndTime까지 200건을 반환한다.
type FundingRateHistoryRequest struct {
	Category  Category
	Symbol    string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

// FundingRateRecord는 정산된 펀딩비 한 건이다.
type FundingRateRecord struct {
	Symbol               string          `json:"symbol"`
	FundingRate          string          `json:"fundingRate"`
	FundingRateTimestamp string          `json:"fundingRateTimestamp"`
	Raw                  json.RawMessage `json:"-"`
}

// OpenInterestRequest는 Linear 미결제약정 이력 조회 조건이다.
type OpenInterestRequest struct {
	Category     Category
	Symbol       string
	IntervalTime DataPeriod
	StartTime    *time.Time
	EndTime      *time.Time
	Limit        int
	Cursor       string
}

// OpenInterestRecord는 미결제약정 한 건이며 값은 양방향 합계다.
type OpenInterestRecord struct {
	OpenInterest string          `json:"openInterest"`
	Timestamp    string          `json:"timestamp"`
	Raw          json.RawMessage `json:"-"`
}

// OpenInterestPage는 미결제약정 조회 한 페이지다.
type OpenInterestPage struct {
	Category       Category
	Symbol         string
	Records        []OpenInterestRecord
	NextPageCursor string
	Raw            json.RawMessage
}

// InsuranceRequest는 보험기금 조회 조건이다. Coin을 비우면 모든 코인을 반환한다.
type InsuranceRequest struct {
	Coin string
}

// InsuranceBalance는 보험기금 코인별 잔고다.
type InsuranceBalance struct {
	Coin    string          `json:"coin"`
	Balance string          `json:"balance"`
	Value   string          `json:"value"`
	Raw     json.RawMessage `json:"-"`
}

// InsurancePool은 24시간 주기로 갱신되는 보험기금 조회 결과다.
type InsurancePool struct {
	UpdatedTime string
	Balances    []InsuranceBalance
	Raw         json.RawMessage
}

// RiskLimitRequest는 Linear 리스크 한도 티어 조회 조건이다. Symbol을 비우면 전체 심볼을 반환한다.
type RiskLimitRequest struct {
	Category Category
	Symbol   string
}

// RiskLimitTier는 심볼의 리스크 한도 구간 하나다.
// MaintenanceMargin과 InitialMargin은 Bybit가 숫자 또는 문자열로 보내므로 DecimalString으로 받는다.
type RiskLimitTier struct {
	ID                int             `json:"id"`
	Symbol            string          `json:"symbol"`
	RiskLimitValue    string          `json:"riskLimitValue"`
	MaintenanceMargin DecimalString   `json:"maintenanceMargin"`
	InitialMargin     DecimalString   `json:"initialMargin"`
	IsLowestRisk      int             `json:"isLowestRisk"`
	MaxLeverage       string          `json:"maxLeverage"`
	Raw               json.RawMessage `json:"-"`
}

// DecimalString은 JSON 숫자 또는 문자열로 오는 십진수를 원문 문자열 그대로 담는다.
type DecimalString string

// UnmarshalJSON은 JSON 숫자와 문자열을 모두 원문 그대로 받는다. null은 빈 문자열이 된다.
func (value *DecimalString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*value = ""
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return fmt.Errorf("decode Bybit decimal string: %w", err)
		}
		*value = DecimalString(text)
		return nil
	}
	*value = DecimalString(data)
	return nil
}

// DeliveryPriceRequest는 Linear 선물 인도가격 조회 조건이다.
type DeliveryPriceRequest struct {
	Category Category
	Symbol   string
	Limit    int
	Cursor   string
}

// DeliveryPriceRecord는 인도가격 한 건이다.
type DeliveryPriceRecord struct {
	Symbol        string          `json:"symbol"`
	DeliveryPrice string          `json:"deliveryPrice"`
	DeliveryTime  string          `json:"deliveryTime"`
	Raw           json.RawMessage `json:"-"`
}

// DeliveryPricePage는 인도가격 조회 한 페이지다.
type DeliveryPricePage struct {
	Category       Category
	Records        []DeliveryPriceRecord
	NextPageCursor string
	Raw            json.RawMessage
}

// AccountRatioRequest는 Linear 롱숏 비율 조회 조건이다.
type AccountRatioRequest struct {
	Category Category
	Symbol   string
	Period   DataPeriod
	Limit    int
}

// AccountRatioRecord는 순매수·순매도 포지션 보유 사용자 비율 한 건이다.
type AccountRatioRecord struct {
	Symbol    string          `json:"symbol"`
	BuyRatio  string          `json:"buyRatio"`
	SellRatio string          `json:"sellRatio"`
	Timestamp string          `json:"timestamp"`
	Raw       json.RawMessage `json:"-"`
}

// MarkPriceCandles는 Linear 마크가격 캔들을 조회한다. 결과는 startTime 역순이다.
func (client *Client) MarkPriceCandles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]PriceCandle, error) {
	return client.priceCandles(ctx, "/v5/market/mark-price-kline", request, options...)
}

// IndexPriceCandles는 Linear 지수가격 캔들을 조회한다. 결과는 startTime 역순이다.
func (client *Client) IndexPriceCandles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]PriceCandle, error) {
	return client.priceCandles(ctx, "/v5/market/index-price-kline", request, options...)
}

// PremiumIndexPriceCandles는 Linear 프리미엄 지수 가격 캔들을 조회한다. 결과는 startTime 역순이다.
func (client *Client) PremiumIndexPriceCandles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]PriceCandle, error) {
	return client.priceCandles(ctx, "/v5/market/premium-index-price-kline", request, options...)
}

func (client *Client) priceCandles(
	ctx context.Context,
	path string,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]PriceCandle, error) {
	if err := validatePriceCandles(request); err != nil {
		return nil, err
	}
	response, result, err := client.executePublicResult(ctx, path, request.values(), publicLimit(20), options...)
	if err != nil {
		return nil, err
	}
	var page struct {
		Items []PriceCandle `json:"list"`
	}
	if err := decodeResult(result, &page); err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return page.Items, nil
}

// FundingRateHistory는 Linear 심볼의 과거 펀딩비를 조회한다.
func (client *Client) FundingRateHistory(
	ctx context.Context,
	request FundingRateHistoryRequest,
	options ...trade.RequestOption,
) ([]FundingRateRecord, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.executePublicResult(
		ctx, "/v5/market/funding/history", request.values(), publicLimit(20), options...,
	)
	if err != nil {
		return nil, err
	}
	_, records, _, err := decodeListPage(
		client, response, result, func(item *FundingRateRecord, raw json.RawMessage) { item.Raw = raw },
	)
	return records, err
}

// OpenInterest는 Linear 심볼의 미결제약정 이력을 cursor 기반으로 조회한다.
func (client *Client) OpenInterest(
	ctx context.Context,
	request OpenInterestRequest,
	options ...trade.RequestOption,
) (OpenInterestPage, error) {
	if err := request.validate(); err != nil {
		return OpenInterestPage{}, err
	}
	response, result, err := client.executePublicResult(
		ctx, "/v5/market/open-interest", request.values(), publicLimit(20), options...,
	)
	if err != nil {
		return OpenInterestPage{}, err
	}
	var header struct {
		Symbol string `json:"symbol"`
	}
	if err := decodeResult(result, &header); err != nil {
		return OpenInterestPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	category, records, cursor, err := decodeListPage(
		client, response, result, func(item *OpenInterestRecord, raw json.RawMessage) { item.Raw = raw },
	)
	if err != nil {
		return OpenInterestPage{}, err
	}
	return OpenInterestPage{
		Category: category, Symbol: header.Symbol, Records: records,
		NextPageCursor: cursor, Raw: cloneBytes(result),
	}, nil
}

// Insurance는 Bybit 보험기금 잔고를 조회한다.
func (client *Client) Insurance(
	ctx context.Context,
	request InsuranceRequest,
	options ...trade.RequestOption,
) (InsurancePool, error) {
	if err := request.validate(); err != nil {
		return InsurancePool{}, err
	}
	response, result, err := client.executePublicResult(
		ctx, "/v5/market/insurance", request.values(), publicLimit(20), options...,
	)
	if err != nil {
		return InsurancePool{}, err
	}
	var header struct {
		UpdatedTime string `json:"updatedTime"`
	}
	if err := decodeResult(result, &header); err != nil {
		return InsurancePool{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	_, balances, _, err := decodeListPage(
		client, response, result, func(item *InsuranceBalance, raw json.RawMessage) { item.Raw = raw },
	)
	if err != nil {
		return InsurancePool{}, err
	}
	return InsurancePool{UpdatedTime: header.UpdatedTime, Balances: balances, Raw: cloneBytes(result)}, nil
}

// RiskLimits는 Linear 심볼의 리스크 한도 티어를 조회한다.
func (client *Client) RiskLimits(
	ctx context.Context,
	request RiskLimitRequest,
	options ...trade.RequestOption,
) ([]RiskLimitTier, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.executePublicResult(
		ctx, "/v5/market/risk-limit", request.values(), publicLimit(20), options...,
	)
	if err != nil {
		return nil, err
	}
	_, tiers, _, err := decodeListPage(
		client, response, result, func(item *RiskLimitTier, raw json.RawMessage) { item.Raw = raw },
	)
	return tiers, err
}

// DeliveryPrices는 Linear 선물의 인도가격을 cursor 기반으로 조회한다.
func (client *Client) DeliveryPrices(
	ctx context.Context,
	request DeliveryPriceRequest,
	options ...trade.RequestOption,
) (DeliveryPricePage, error) {
	if err := request.validate(); err != nil {
		return DeliveryPricePage{}, err
	}
	response, result, err := client.executePublicResult(
		ctx, "/v5/market/delivery-price", request.values(), publicLimit(20), options...,
	)
	if err != nil {
		return DeliveryPricePage{}, err
	}
	category, records, cursor, err := decodeListPage(
		client, response, result, func(item *DeliveryPriceRecord, raw json.RawMessage) { item.Raw = raw },
	)
	if err != nil {
		return DeliveryPricePage{}, err
	}
	return DeliveryPricePage{
		Category: category, Records: records, NextPageCursor: cursor, Raw: cloneBytes(result),
	}, nil
}

// AccountRatios는 Linear 심볼의 롱숏 사용자 비율을 조회한다.
func (client *Client) AccountRatios(
	ctx context.Context,
	request AccountRatioRequest,
	options ...trade.RequestOption,
) ([]AccountRatioRecord, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.executePublicResult(
		ctx, "/v5/market/account-ratio", request.values(), publicLimit(20), options...,
	)
	if err != nil {
		return nil, err
	}
	_, records, _, err := decodeListPage(
		client, response, result, func(item *AccountRatioRecord, raw json.RawMessage) { item.Raw = raw },
	)
	return records, err
}

// executePublicResult는 서명 없는 GET 조회를 보내고 result를 반환한다.
func (client *Client) executePublicResult(
	ctx context.Context,
	path string,
	query url.Values,
	limit endpointLimit,
	options ...trade.RequestOption,
) (commonexchange.Response, json.RawMessage, error) {
	response, _, err := client.executePublic(ctx, http.MethodGet, path, query, limit, options...)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	result, _, err := client.responseResult(response, commonexchange.OperationRead)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	return response, result, nil
}

func validatePriceCandles(request CandlesRequest) error {
	if request.Category != CategoryLinear {
		return validationError("price candles support only linear category")
	}
	return request.validate()
}

func validateTimeOrder(name string, start, end *time.Time) error {
	if start != nil && end != nil && start.After(*end) {
		return validationError("%s start time cannot be after end time", name)
	}
	return nil
}

func validateLinearMarketCategory(operation string, category Category) error {
	if category != CategoryLinear {
		return validationError("%s supports only linear category", operation)
	}
	return nil
}

func (request FundingRateHistoryRequest) validate() error {
	if err := validateLinearMarketCategory("funding rate history", request.Category); err != nil {
		return err
	}
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	if request.StartTime != nil && request.EndTime == nil {
		return validationError("funding rate history requires endTime when startTime is set")
	}
	if err := validateTimeOrder("funding rate history", request.StartTime, request.EndTime); err != nil {
		return err
	}
	return validatePage(request.Limit, 200, "")
}

func (request FundingRateHistoryRequest) values() url.Values {
	values := categoryValues(request.Category)
	values.Set("symbol", request.Symbol)
	setTime(values, "startTime", request.StartTime)
	setTime(values, "endTime", request.EndTime)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request OpenInterestRequest) validate() error {
	if err := validateLinearMarketCategory("open interest", request.Category); err != nil {
		return err
	}
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	if !request.IntervalTime.valid() {
		return validationError("unsupported open interest interval %q", request.IntervalTime)
	}
	if err := validateTimeOrder("open interest", request.StartTime, request.EndTime); err != nil {
		return err
	}
	return validatePage(request.Limit, 200, request.Cursor)
}

func (request OpenInterestRequest) values() url.Values {
	values := categoryValues(request.Category)
	values.Set("symbol", request.Symbol)
	values.Set("intervalTime", string(request.IntervalTime))
	setTime(values, "startTime", request.StartTime)
	setTime(values, "endTime", request.EndTime)
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "cursor", request.Cursor)
	return values
}

func (request InsuranceRequest) validate() error {
	return validateOptionalText("coin", request.Coin)
}

func (request InsuranceRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "coin", request.Coin)
	return values
}

func (request RiskLimitRequest) validate() error {
	if err := validateLinearMarketCategory("risk limit", request.Category); err != nil {
		return err
	}
	return validateOptionalText("symbol", request.Symbol)
}

func (request RiskLimitRequest) values() url.Values {
	values := categoryValues(request.Category)
	setIfNotEmpty(values, "symbol", request.Symbol)
	return values
}

func (request DeliveryPriceRequest) validate() error {
	if err := validateLinearMarketCategory("delivery price", request.Category); err != nil {
		return err
	}
	if err := validateOptionalText("symbol", request.Symbol); err != nil {
		return err
	}
	return validatePage(request.Limit, 200, request.Cursor)
}

func (request DeliveryPriceRequest) values() url.Values {
	values := categoryValues(request.Category)
	setIfNotEmpty(values, "symbol", request.Symbol)
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "cursor", request.Cursor)
	return values
}

func (request AccountRatioRequest) validate() error {
	if err := validateLinearMarketCategory("account ratio", request.Category); err != nil {
		return err
	}
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	if !request.Period.valid() {
		return validationError("unsupported account ratio period %q", request.Period)
	}
	return validatePage(request.Limit, 500, "")
}

func (request AccountRatioRequest) values() url.Values {
	values := categoryValues(request.Category)
	values.Set("symbol", request.Symbol)
	values.Set("period", string(request.Period))
	setPositiveInt(values, "limit", request.Limit)
	return values
}
