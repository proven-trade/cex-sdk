package futures

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// InstrumentStatus는 상품의 가격 괴리와 변동성 상태다.
type InstrumentStatus struct {
	Tradeable                                string          `json:"tradeable"`
	ExperiencingDislocation                  bool            `json:"experiencingDislocation"`
	PriceDislocationDirection                string          `json:"priceDislocationDirection"`
	ExperiencingExtremeVolatility            bool            `json:"experiencingExtremeVolatility"`
	ExtremeVolatilityInitialMarginMultiplier int             `json:"extremeVolatilityInitialMarginMultiplier"`
	Raw                                      json.RawMessage `json:"-"`
}

// InstrumentStatusRequest는 전체 상품 상태 조회의 계약 종류 필터다.
type InstrumentStatusRequest struct {
	ContractTypes []ContractType
}

// TradingInstrumentsRequest는 접근 가능한 상품 조회의 계약 종류 필터다.
type TradingInstrumentsRequest struct {
	ContractTypes []ContractType
}

// HistoricalFundingRatesRequest는 perpetual 상품의 과거 펀딩비 조회 조건이다.
type HistoricalFundingRatesRequest struct {
	Symbol string
}

// HistoricalFundingRate는 한 구간의 펀딩비다.
type HistoricalFundingRate struct {
	Timestamp           string  `json:"timestamp"`
	FundingRate         Decimal `json:"fundingRate"`
	RelativeFundingRate Decimal `json:"relativeFundingRate"`
}

// HistoryPageRequest는 /api/history/v3 이력 endpoint 공통 페이지 조건이다.
type HistoryPageRequest struct {
	Since             *time.Time
	Before            *time.Time
	Descending        bool
	Ascending         bool
	ContinuationToken string
	// Count는 최대 1000이며 0이면 서버 기본값을 사용한다.
	Count int
}

// HistoryPage는 이력 한 페이지다. ContinuationToken이 비어 있지 않으면 같은 sort로 이어서 조회한다.
type HistoryPage struct {
	AccountUID        string            `json:"accountUid"`
	Length            int64             `json:"len"`
	ServerTime        string            `json:"serverTime"`
	Elements          []json.RawMessage `json:"elements"`
	ContinuationToken string            `json:"continuationToken"`
	Raw               json.RawMessage   `json:"-"`
}

// MarketHistoryKind는 공개 시장 이력 종류다.
type MarketHistoryKind string

const (
	MarketHistoryExecutions MarketHistoryKind = "executions"
	MarketHistoryOrders     MarketHistoryKind = "orders"
	MarketHistoryPrice      MarketHistoryKind = "price"
)

// MarketHistoryRequest는 공개 시장 체결·주문·마크가격 이벤트 이력 조회 조건이다.
type MarketHistoryRequest struct {
	Kind      MarketHistoryKind
	Tradeable string
	Page      HistoryPageRequest
}

// AccountHistoryKind는 계정 이력 종류다.
type AccountHistoryKind string

const (
	AccountHistoryExecutions AccountHistoryKind = "executions"
	AccountHistoryOrders     AccountHistoryKind = "orders"
	AccountHistoryTriggers   AccountHistoryKind = "triggers"
	AccountHistoryPositions  AccountHistoryKind = "positions"
)

// AccountHistoryRequest는 계정 체결·주문·트리거·포지션 이벤트 이력 조회 조건이다.
// Opened/Closed는 orders와 triggers, 나머지 불리언 필터는 positions에만 유효하다.
type AccountHistoryRequest struct {
	Kind      AccountHistoryKind
	Tradeable string
	Page      HistoryPageRequest

	Opened, Closed                           bool
	Increased, Decreased, Reversed, NoChange bool
	Trades, FundingRealization, Settlement   bool
}

// AccountLogRequest는 계정 로그 조회 조건이다. 시간(Since/Before)과 ID(From/To) 범위를 조합할 수 있다.
type AccountLogRequest struct {
	Since             *time.Time
	Before            *time.Time
	FromID, ToID      int64
	Descending        bool
	Ascending         bool
	Info              []string
	Count             int
	ConversionDetails bool
}

// AccountLogEntry는 계정 로그 한 건이다.
type AccountLogEntry struct {
	ID                 int64           `json:"id"`
	BookingUID         string          `json:"booking_uid"`
	Date               string          `json:"date"`
	Asset              string          `json:"asset"`
	Info               string          `json:"info"`
	Contract           string          `json:"contract"`
	Collateral         string          `json:"collateral"`
	Execution          string          `json:"execution"`
	MarginAccount      string          `json:"margin_account"`
	OldBalance         Decimal         `json:"old_balance"`
	NewBalance         Decimal         `json:"new_balance"`
	Fee                Decimal         `json:"fee"`
	FundingRate        Decimal         `json:"funding_rate"`
	MarkPrice          Decimal         `json:"mark_price"`
	TradePrice         Decimal         `json:"trade_price"`
	RealizedPNL        Decimal         `json:"realized_pnl"`
	RealizedFunding    Decimal         `json:"realized_funding"`
	OldAverageEntry    Decimal         `json:"old_average_entry_price"`
	NewAverageEntry    Decimal         `json:"new_average_entry_price"`
	LiquidationFee     Decimal         `json:"liquidation_fee"`
	ExchangeRate       Decimal         `json:"exchange_rate"`
	ExchangeRateFrom   Decimal         `json:"exchange_rate_from"`
	ConversionFee      Decimal         `json:"conversion_fee"`
	ConversionSpreadPC Decimal         `json:"conversion_spread_percentage"`
	Raw                json.RawMessage `json:"-"`
}

// AccountLogPage는 계정 로그 한 페이지다.
type AccountLogPage struct {
	AccountUID string            `json:"accountUid"`
	Logs       []AccountLogEntry `json:"logs"`
	Raw        json.RawMessage   `json:"-"`
}

// ChartTickType은 차트 메타데이터 조회에 쓰는 tick type이다.
type ChartTickType = CandleTickType

// AnalyticsType은 시장 분석 시계열 종류다.
type AnalyticsType string

const (
	AnalyticsOpenInterest        AnalyticsType = "open-interest"
	AnalyticsAggressorDifference AnalyticsType = "aggressor-differential"
	AnalyticsTradeVolume         AnalyticsType = "trade-volume"
	AnalyticsTradeCount          AnalyticsType = "trade-count"
	AnalyticsLiquidationVolume   AnalyticsType = "liquidation-volume"
	AnalyticsRollingVolatility   AnalyticsType = "rolling-volatility"
	AnalyticsLongShortRatio      AnalyticsType = "long-short-ratio"
	AnalyticsLongShortInfo       AnalyticsType = "long-short-info"
	AnalyticsCVD                 AnalyticsType = "cvd"
	AnalyticsTopTraders          AnalyticsType = "top-traders"
	AnalyticsOrderbook           AnalyticsType = "orderbook"
	AnalyticsSpreads             AnalyticsType = "spreads"
	AnalyticsLiquidity           AnalyticsType = "liquidity"
	AnalyticsSlippage            AnalyticsType = "slippage"
	AnalyticsFutureBasis         AnalyticsType = "future-basis"
	AnalyticsFunding             AnalyticsType = "funding"
)

// AnalyticsRequest는 시장 분석 시계열 조회 조건이다.
type AnalyticsRequest struct {
	Symbol          string
	Type            AnalyticsType
	Since           time.Time
	IntervalSeconds int
	To              *time.Time
}

// LiquidityPoolRequest는 유동성 풀 통계 조회 조건이다.
type LiquidityPoolRequest struct {
	Since           time.Time
	IntervalSeconds int
	To              *time.Time
}

// Analytics는 시간 bucket별 분석 결과다. 유형별로 Data 구조가 달라 원문 JSON으로 제공한다.
type Analytics struct {
	Timestamps []int64         `json:"timestamp"`
	More       bool            `json:"more"`
	Data       json.RawMessage `json:"data"`
	Raw        json.RawMessage `json:"-"`
}

// InstrumentsStatus는 모든 상품의 가격 괴리·변동성 상태를 조회한다.
// Required permission: none (public).
func (client *Client) InstrumentsStatus(
	ctx context.Context,
	request InstrumentStatusRequest,
	options ...trade.RequestOption,
) ([]InstrumentStatus, error) {
	values, err := contractTypeValues(request.ContractTypes)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		InstrumentStatus []json.RawMessage `json:"instrumentStatus"`
	}
	response, err := client.publicRead(ctx, derivativesPrefix+"instruments/status", values, &envelope, options...)
	if err != nil {
		return nil, err
	}
	statuses, err := decodeRawItems(
		envelope.InstrumentStatus, func(item *InstrumentStatus, raw []byte) { item.Raw = raw },
	)
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return statuses, nil
}

// InstrumentStatus는 단일 상품의 가격 괴리·변동성 상태를 조회한다.
// Required permission: none (public).
func (client *Client) InstrumentStatus(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (InstrumentStatus, error) {
	if err := validateSymbol(symbol); err != nil {
		return InstrumentStatus{}, err
	}
	var status InstrumentStatus
	response, err := client.publicRead(
		ctx, derivativesPrefix+"instruments/"+url.PathEscape(symbol)+"/status", nil, &status, options...,
	)
	if err != nil {
		return InstrumentStatus{}, err
	}
	status.Raw = cloneBytes(response.Body)
	return status, nil
}

// TradingInstruments는 인증된 계정이 접근 가능한 시장·지수 사양을 조회한다.
// Required permission: credential.PermissionRead.
func (client *Client) TradingInstruments(
	ctx context.Context,
	request TradingInstrumentsRequest,
	options ...trade.RequestOption,
) ([]Instrument, error) {
	values, err := contractTypeValues(request.ContractTypes)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Instruments []json.RawMessage `json:"instruments"`
	}
	response, err := client.privateRead(
		ctx, http.MethodGet, derivativesPrefix+"trading/instruments", values, defaultReadCost, &envelope, options...,
	)
	if err != nil {
		return nil, err
	}
	instruments, err := decodeRawItems(envelope.Instruments, func(item *Instrument, raw []byte) { item.Raw = raw })
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return instruments, nil
}

// HistoricalFundingRates는 perpetual 상품의 과거 펀딩비를 시간 오름차순으로 조회한다.
// Required permission: none (public).
func (client *Client) HistoricalFundingRates(
	ctx context.Context,
	request HistoricalFundingRatesRequest,
	options ...trade.RequestOption,
) ([]HistoricalFundingRate, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return nil, err
	}
	var envelope struct {
		Rates []HistoricalFundingRate `json:"rates"`
	}
	if _, err := client.publicRead(
		ctx, derivativesPrefix+"historical-funding-rates",
		url.Values{"symbol": []string{request.Symbol}}, &envelope, options...,
	); err != nil {
		return nil, err
	}
	return envelope.Rates, nil
}

// MarketHistory는 공개 시장의 체결·주문·마크가격 이벤트 이력을 조회한다.
// Required permission: none (public).
func (client *Client) MarketHistory(
	ctx context.Context,
	request MarketHistoryRequest,
	options ...trade.RequestOption,
) (HistoryPage, error) {
	switch request.Kind {
	case MarketHistoryExecutions, MarketHistoryOrders, MarketHistoryPrice:
	default:
		return HistoryPage{}, validationError("unsupported market history kind %q", request.Kind)
	}
	if err := validateSymbol(request.Tradeable); err != nil {
		return HistoryPage{}, err
	}
	values, err := request.Page.values()
	if err != nil {
		return HistoryPage{}, err
	}
	path := fmt.Sprintf(
		"%s/market/%s/%s", historyPrefix, url.PathEscape(request.Tradeable), request.Kind,
	)
	return client.publicHistory(ctx, path, values, options...)
}

// AccountHistory는 계정 체결·주문·트리거·포지션 이벤트 이력을 조회한다. 비용은 호출당 토큰 1이다.
// Required permission: credential.PermissionRead.
func (client *Client) AccountHistory(
	ctx context.Context,
	request AccountHistoryRequest,
	options ...trade.RequestOption,
) (HistoryPage, error) {
	switch request.Kind {
	case AccountHistoryExecutions, AccountHistoryOrders, AccountHistoryTriggers, AccountHistoryPositions:
	default:
		return HistoryPage{}, validationError("unsupported account history kind %q", request.Kind)
	}
	values, err := request.Page.values()
	if err != nil {
		return HistoryPage{}, err
	}
	if request.Tradeable != "" {
		if err := validateSymbol(request.Tradeable); err != nil {
			return HistoryPage{}, err
		}
		values.Set("tradeable", request.Tradeable)
	}
	switch request.Kind {
	case AccountHistoryOrders, AccountHistoryTriggers:
		setTrue(values, "opened", request.Opened)
		setTrue(values, "closed", request.Closed)
	case AccountHistoryPositions:
		setTrue(values, "opened", request.Opened)
		setTrue(values, "closed", request.Closed)
		setTrue(values, "increased", request.Increased)
		setTrue(values, "decreased", request.Decreased)
		setTrue(values, "reversed", request.Reversed)
		setTrue(values, "no_change", request.NoChange)
		setTrue(values, "trades", request.Trades)
		setTrue(values, "funding_realization", request.FundingRealization)
		setTrue(values, "settlement", request.Settlement)
	}
	var page HistoryPage
	response, err := client.historyRead(ctx, historyPrefix+"/"+string(request.Kind), values, 1, &page, options...)
	if err != nil {
		return HistoryPage{}, err
	}
	page.ContinuationToken = firstNonEmpty(response.Header.Get("Next-Continuation-Token"), page.ContinuationToken)
	page.Raw = cloneBytes(response.Body)
	return page, nil
}

// AccountLog는 계정 잔고·포지션 변경 로그를 시간 또는 ID 범위로 조회한다.
// 비용은 count에 따라 1~10 토큰이다(기본 500은 3).
// Required permission: credential.PermissionRead.
func (client *Client) AccountLog(
	ctx context.Context,
	request AccountLogRequest,
	options ...trade.RequestOption,
) (AccountLogPage, error) {
	if request.Ascending && request.Descending {
		return AccountLogPage{}, validationError("sort cannot be both ascending and descending")
	}
	if request.FromID < 0 || request.ToID < 0 || request.Count < 0 || request.Count > 10000 {
		return AccountLogPage{}, validationError("account log ids and count must be non-negative, count at most 10000")
	}
	if request.FromID > 0 && request.ToID > 0 && request.FromID > request.ToID {
		return AccountLogPage{}, validationError("account log from ID cannot exceed to ID")
	}
	if request.Since != nil && request.Before != nil && request.Since.After(*request.Before) {
		return AccountLogPage{}, validationError("account log since cannot be after before")
	}
	values := make(url.Values)
	setTimeMillis(values, "since", request.Since)
	setTimeMillis(values, "before", request.Before)
	if request.FromID > 0 {
		values.Set("from", strconv.FormatInt(request.FromID, 10))
	}
	if request.ToID > 0 {
		values.Set("to", strconv.FormatInt(request.ToID, 10))
	}
	if request.Ascending {
		values.Set("sort", "asc")
	}
	if request.Descending {
		values.Set("sort", "desc")
	}
	for _, info := range request.Info {
		if err := validateOptionalText("info", info); err != nil || info == "" {
			return AccountLogPage{}, validationError("account log info filter has an invalid format")
		}
		values.Add("info", info)
	}
	if request.Count > 0 {
		values.Set("count", strconv.Itoa(request.Count))
	}
	setTrue(values, "conversion_details", request.ConversionDetails)
	var envelope struct {
		AccountUID string            `json:"accountUid"`
		Logs       []json.RawMessage `json:"logs"`
	}
	response, err := client.historyRead(
		ctx, historyPrefix+"/account-log", values, accountLogCost(request.Count), &envelope, options...,
	)
	if err != nil {
		return AccountLogPage{}, err
	}
	logs, err := decodeRawItems(envelope.Logs, func(item *AccountLogEntry, raw []byte) { item.Raw = raw })
	if err != nil {
		return AccountLogPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return AccountLogPage{AccountUID: envelope.AccountUID, Logs: logs, Raw: cloneBytes(response.Body)}, nil
}

// ChartTickTypes는 차트가 지원하는 tick type 목록을 조회한다.
// Required permission: none (public).
func (client *Client) ChartTickTypes(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]string, error) {
	var tickTypes []string
	if err := client.chartJSON(ctx, chartsPrefix+"/", nil, &tickTypes, options...); err != nil {
		return nil, err
	}
	return tickTypes, nil
}

// ChartMarkets는 tick type별 차트 시장 심볼 목록을 조회한다.
// Required permission: none (public).
func (client *Client) ChartMarkets(
	ctx context.Context,
	tickType ChartTickType,
	options ...trade.RequestOption,
) ([]string, error) {
	if !tickType.valid() {
		return nil, validationError("unsupported candle tick type %q", tickType)
	}
	var symbols []string
	if err := client.chartJSON(
		ctx, chartsPrefix+"/"+url.PathEscape(string(tickType)), nil, &symbols, options...,
	); err != nil {
		return nil, err
	}
	return symbols, nil
}

// ChartResolutions는 tick type과 시장별 지원 캔들 구간을 조회한다.
// Required permission: none (public).
func (client *Client) ChartResolutions(
	ctx context.Context,
	tickType ChartTickType,
	symbol string,
	options ...trade.RequestOption,
) ([]CandleResolution, error) {
	if !tickType.valid() {
		return nil, validationError("unsupported candle tick type %q", tickType)
	}
	if err := validateSymbol(symbol); err != nil {
		return nil, err
	}
	var resolutions []CandleResolution
	if err := client.chartJSON(
		ctx, chartsPrefix+"/"+url.PathEscape(string(tickType))+"/"+url.PathEscape(symbol), nil,
		&resolutions, options...,
	); err != nil {
		return nil, err
	}
	return resolutions, nil
}

// MarketAnalytics는 시장 분석 시계열(open interest, 거래량, 펀딩 등)을 bucket 단위로 조회한다.
// Required permission: none (public).
func (client *Client) MarketAnalytics(
	ctx context.Context,
	request AnalyticsRequest,
	options ...trade.RequestOption,
) (Analytics, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return Analytics{}, err
	}
	if !request.Type.valid() {
		return Analytics{}, validationError("unsupported analytics type %q", request.Type)
	}
	values, err := analyticsValues(request.Since, request.IntervalSeconds, request.To)
	if err != nil {
		return Analytics{}, err
	}
	return client.analytics(
		ctx, chartsPrefix+"/analytics/"+url.PathEscape(request.Symbol)+"/"+url.PathEscape(string(request.Type)),
		values, options...,
	)
}

// LiquidityPoolAnalytics는 유동성 풀 통계를 조회한다.
// Required permission: none (public).
func (client *Client) LiquidityPoolAnalytics(
	ctx context.Context,
	request LiquidityPoolRequest,
	options ...trade.RequestOption,
) (Analytics, error) {
	values, err := analyticsValues(request.Since, request.IntervalSeconds, request.To)
	if err != nil {
		return Analytics{}, err
	}
	return client.analytics(ctx, chartsPrefix+"/analytics/liquidity-pool", values, options...)
}

func (client *Client) analytics(
	ctx context.Context,
	path string,
	values url.Values,
	options ...trade.RequestOption,
) (Analytics, error) {
	var envelope struct {
		Result Analytics `json:"result"`
	}
	response, err := client.executePublic(ctx, path, values, options...)
	if err != nil {
		return Analytics{}, err
	}
	if err := client.decodePlainJSON(response, commonexchange.OperationRead, &envelope); err != nil {
		return Analytics{}, err
	}
	envelope.Result.Raw = cloneBytes(response.Body)
	return envelope.Result, nil
}

func (client *Client) chartJSON(
	ctx context.Context,
	path string,
	values url.Values,
	target any,
	options ...trade.RequestOption,
) error {
	response, err := client.executePublic(ctx, path, values, options...)
	if err != nil {
		return err
	}
	return client.decodePlainJSON(response, commonexchange.OperationRead, target)
}

func (client *Client) publicHistory(
	ctx context.Context,
	path string,
	values url.Values,
	options ...trade.RequestOption,
) (HistoryPage, error) {
	var page HistoryPage
	response, err := client.executePublic(ctx, path, values, options...)
	if err != nil {
		return HistoryPage{}, err
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &page); err != nil {
		return HistoryPage{}, err
	}
	page.ContinuationToken = firstNonEmpty(response.Header.Get("Next-Continuation-Token"), page.ContinuationToken)
	page.Raw = cloneBytes(response.Body)
	return page, nil
}

func (request HistoryPageRequest) values() (url.Values, error) {
	if request.Ascending && request.Descending {
		return nil, validationError("sort cannot be both ascending and descending")
	}
	if request.Count < 0 || request.Count > 1000 {
		return nil, validationError("history count must be between 1 and 1000 or zero for default")
	}
	if request.Since != nil && request.Before != nil && request.Since.After(*request.Before) {
		return nil, validationError("history since cannot be after before")
	}
	if err := validateOptionalText("continuation_token", request.ContinuationToken); err != nil {
		return nil, err
	}
	values := make(url.Values)
	setTimeMillis(values, "since", request.Since)
	setTimeMillis(values, "before", request.Before)
	if request.Ascending {
		values.Set("sort", "asc")
	}
	if request.Descending {
		values.Set("sort", "desc")
	}
	setIfNotEmpty(values, "continuation_token", request.ContinuationToken)
	if request.Count > 0 {
		values.Set("count", strconv.Itoa(request.Count))
	}
	return values, nil
}

func contractTypeValues(contractTypes []ContractType) (url.Values, error) {
	values := make(url.Values)
	seen := make(map[ContractType]struct{}, len(contractTypes))
	for _, contractType := range contractTypes {
		switch contractType {
		case ContractTypeInverse, ContractTypeVanilla, ContractTypeFlexible, ContractTypeOptions, ContractTypeAll:
		default:
			return nil, validationError("unsupported contract type %q", contractType)
		}
		if _, exists := seen[contractType]; exists {
			return nil, validationError("duplicate contract type %q", contractType)
		}
		seen[contractType] = struct{}{}
		values.Add("contractType", string(contractType))
	}
	return values, nil
}

func analyticsValues(since time.Time, intervalSeconds int, to *time.Time) (url.Values, error) {
	if since.IsZero() {
		return nil, validationError("analytics since is required")
	}
	switch intervalSeconds {
	case 60, 300, 900, 1800, 3600, 14400, 43200, 86400, 604800:
	default:
		return nil, validationError("unsupported analytics interval %d", intervalSeconds)
	}
	if to != nil && since.After(*to) {
		return nil, validationError("analytics since cannot be after to")
	}
	values := url.Values{
		"since":    []string{strconv.FormatInt(since.Unix(), 10)},
		"interval": []string{strconv.Itoa(intervalSeconds)},
	}
	setTimeSeconds(values, "to", to)
	return values, nil
}

func (value AnalyticsType) valid() bool {
	switch value {
	case AnalyticsOpenInterest, AnalyticsAggressorDifference, AnalyticsTradeVolume, AnalyticsTradeCount,
		AnalyticsLiquidationVolume, AnalyticsRollingVolatility, AnalyticsLongShortRatio, AnalyticsLongShortInfo,
		AnalyticsCVD, AnalyticsTopTraders, AnalyticsOrderbook, AnalyticsSpreads, AnalyticsLiquidity,
		AnalyticsSlippage, AnalyticsFutureBasis, AnalyticsFunding:
		return true
	}
	return false
}

// accountLogCost는 count 구간별 history 토큰 비용이다(기본 count 500은 3).
func accountLogCost(count int) int {
	switch {
	case count == 0:
		return 3
	case count <= 25:
		return 1
	case count <= 50:
		return 2
	case count <= 1000:
		return 3
	case count <= 5000:
		return 6
	default:
		return 10
	}
}
