package binance

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	trade "github.com/proven-trade/cex-sdk"
)

// MyTradesRequest는 계정 체결 내역의 조회 범위다.
// 공식 문서가 허용하는 조합은 symbol 단독, orderId, startTime·endTime, fromId, orderId와 fromId다.
// OrderID 또는 FromID는 StartTime·EndTime과 함께 쓸 수 없고, 두 시각의 간격은 24시간을 넘을 수 없다.
type MyTradesRequest struct {
	Symbol    string
	OrderID   *int64
	FromID    *int64
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

func (request MyTradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.OrderID != nil && *request.OrderID <= 0 {
		return validationError("myTrades orderId must be positive")
	}
	if request.FromID != nil && *request.FromID < 0 {
		return validationError("myTrades fromId cannot be negative")
	}
	if (request.StartTime != nil || request.EndTime != nil) && (request.OrderID != nil || request.FromID != nil) {
		return validationError("myTrades startTime and endTime cannot be combined with orderId or fromId")
	}
	if err := validateDayWindow("myTrades", request.StartTime, request.EndTime); err != nil {
		return err
	}
	return validateOptionalLimit("myTrades", request.Limit, 1000)
}

func (request MyTradesRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	setInt64(values, "orderId", request.OrderID)
	setInt64(values, "fromId", request.FromID)
	setTimeMillis(values, "startTime", request.StartTime)
	setTimeMillis(values, "endTime", request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// weight는 orderId를 지정하면 5, 지정하지 않으면 20이다.
func (request MyTradesRequest) weight() int {
	if request.OrderID != nil {
		return 5
	}
	return 20
}

// AccountTrade는 계정의 체결 한 건이다.
type AccountTrade struct {
	Symbol          string          `json:"symbol"`
	ID              int64           `json:"id"`
	OrderID         int64           `json:"orderId"`
	OrderListID     int64           `json:"orderListId"`
	Price           string          `json:"price"`
	Quantity        string          `json:"qty"`
	QuoteQuantity   string          `json:"quoteQty"`
	Commission      string          `json:"commission"`
	CommissionAsset string          `json:"commissionAsset"`
	Time            int64           `json:"time"`
	Buyer           bool            `json:"isBuyer"`
	Maker           bool            `json:"isMaker"`
	BestMatch       bool            `json:"isBestMatch"`
	Raw             json.RawMessage `json:"-"`
}

// MyTrades는 지정한 상품의 계정 체결 내역을 조회한다.
func (client *Client) MyTrades(
	ctx context.Context,
	request MyTradesRequest,
	options ...trade.RequestOption,
) ([]AccountTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := client.readSigned(ctx, "/api/v3/myTrades", request.values(), request.weight(), options...)
	if err != nil {
		return nil, err
	}
	return decodeList(body, func(accountTrade *AccountTrade, raw json.RawMessage) { accountTrade.Raw = raw })
}

// MyAllocationsRequest는 SOR 배분 내역의 조회 범위다.
// 공식 문서가 허용하는 조합은 symbol 단독, startTime·endTime, fromAllocationId, orderId, orderId와 fromAllocationId다.
// OrderID 또는 FromAllocationID는 StartTime·EndTime과 함께 쓸 수 없고, 두 시각의 간격은 24시간을 넘을 수 없다.
type MyAllocationsRequest struct {
	Symbol           string
	OrderID          *int64
	FromAllocationID *int64
	StartTime        *time.Time
	EndTime          *time.Time
	Limit            int
}

func (request MyAllocationsRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.OrderID != nil && *request.OrderID <= 0 {
		return validationError("myAllocations orderId must be positive")
	}
	if request.FromAllocationID != nil && *request.FromAllocationID < 0 {
		return validationError("myAllocations fromAllocationId cannot be negative")
	}
	hasTimeRange := request.StartTime != nil || request.EndTime != nil
	if hasTimeRange && (request.OrderID != nil || request.FromAllocationID != nil) {
		return validationError("myAllocations startTime and endTime cannot be combined with orderId or fromAllocationId")
	}
	if err := validateDayWindow("myAllocations", request.StartTime, request.EndTime); err != nil {
		return err
	}
	return validateOptionalLimit("myAllocations", request.Limit, 1000)
}

func (request MyAllocationsRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	setInt64(values, "orderId", request.OrderID)
	setInt64(values, "fromAllocationId", request.FromAllocationID)
	setTimeMillis(values, "startTime", request.StartTime)
	setTimeMillis(values, "endTime", request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// Allocation은 SOR 주문에서 발생한 배분 한 건이다.
type Allocation struct {
	Symbol          string          `json:"symbol"`
	AllocationID    int64           `json:"allocationId"`
	AllocationType  string          `json:"allocationType"`
	OrderID         int64           `json:"orderId"`
	OrderListID     int64           `json:"orderListId"`
	Price           string          `json:"price"`
	Quantity        string          `json:"qty"`
	QuoteQuantity   string          `json:"quoteQty"`
	Commission      string          `json:"commission"`
	CommissionAsset string          `json:"commissionAsset"`
	Time            int64           `json:"time"`
	Buyer           bool            `json:"isBuyer"`
	Maker           bool            `json:"isMaker"`
	Allocator       bool            `json:"isAllocator"`
	Raw             json.RawMessage `json:"-"`
}

// MyAllocations는 SOR 주문 배치로 발생한 배분 내역을 조회한다.
func (client *Client) MyAllocations(
	ctx context.Context,
	request MyAllocationsRequest,
	options ...trade.RequestOption,
) ([]Allocation, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := client.readSigned(ctx, "/api/v3/myAllocations", request.values(), 20, options...)
	if err != nil {
		return nil, err
	}
	return decodeList(body, func(allocation *Allocation, raw json.RawMessage) { allocation.Raw = raw })
}

// MyPreventedMatchesRequest는 자기 체결 방지(STP)로 만료된 주문의 조회 조건이다.
// 공식 문서가 허용하는 조합은 PreventedMatchID 단독, OrderID, OrderID와 FromPreventedMatchID, 여기에 Limit을 더한 경우다.
// PreventedMatchID와 OrderID 중 정확히 하나가 필요하고 FromPreventedMatchID와 Limit은 OrderID와만 함께 쓴다.
type MyPreventedMatchesRequest struct {
	Symbol               string
	PreventedMatchID     *int64
	OrderID              *int64
	FromPreventedMatchID *int64
	Limit                int
}

func (request MyPreventedMatchesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if (request.PreventedMatchID == nil) == (request.OrderID == nil) {
		return validationError("myPreventedMatches requires exactly one of preventedMatchId or orderId")
	}
	if request.PreventedMatchID != nil && *request.PreventedMatchID <= 0 {
		return validationError("myPreventedMatches preventedMatchId must be positive")
	}
	if request.OrderID != nil && *request.OrderID <= 0 {
		return validationError("myPreventedMatches orderId must be positive")
	}
	if request.FromPreventedMatchID != nil && *request.FromPreventedMatchID < 0 {
		return validationError("myPreventedMatches fromPreventedMatchId cannot be negative")
	}
	if request.PreventedMatchID != nil && (request.FromPreventedMatchID != nil || request.Limit != 0) {
		return validationError("myPreventedMatches fromPreventedMatchId and limit require orderId")
	}
	return validateOptionalLimit("myPreventedMatches", request.Limit, 1000)
}

func (request MyPreventedMatchesRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	setInt64(values, "preventedMatchId", request.PreventedMatchID)
	setInt64(values, "orderId", request.OrderID)
	setInt64(values, "fromPreventedMatchId", request.FromPreventedMatchID)
	setLimit(values, request.Limit)
	return values
}

// weight는 preventedMatchId로 조회하면 2, orderId로 조회하면 20이다.
func (request MyPreventedMatchesRequest) weight() int {
	if request.PreventedMatchID != nil {
		return 2
	}
	return 20
}

// PreventedMatch는 자기 체결 방지로 만료된 매칭 한 건이다.
// MakerPreventedQuantity는 maker 쪽 수량이 만료된 경우에만 채워진다.
type PreventedMatch struct {
	Symbol                  string          `json:"symbol"`
	PreventedMatchID        int64           `json:"preventedMatchId"`
	TakerOrderID            int64           `json:"takerOrderId"`
	MakerSymbol             string          `json:"makerSymbol"`
	MakerOrderID            int64           `json:"makerOrderId"`
	TradeGroupID            int64           `json:"tradeGroupId"`
	SelfTradePreventionMode string          `json:"selfTradePreventionMode"`
	Price                   string          `json:"price"`
	MakerPreventedQuantity  string          `json:"makerPreventedQuantity"`
	TransactionTime         int64           `json:"transactTime"`
	Raw                     json.RawMessage `json:"-"`
}

// MyPreventedMatches는 자기 체결 방지로 만료된 주문 목록을 조회한다.
func (client *Client) MyPreventedMatches(
	ctx context.Context,
	request MyPreventedMatchesRequest,
	options ...trade.RequestOption,
) ([]PreventedMatch, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := client.readSigned(ctx, "/api/v3/myPreventedMatches", request.values(), request.weight(), options...)
	if err != nil {
		return nil, err
	}
	return decodeList(body, func(match *PreventedMatch, raw json.RawMessage) { match.Raw = raw })
}

// AccountCommissionRequest는 수수료율을 조회할 상품을 지정한다.
type AccountCommissionRequest struct {
	Symbol string
}

// AccountCommission은 상품에 적용되는 계정 수수료율이다.
type AccountCommission struct {
	Symbol             string             `json:"symbol"`
	StandardCommission CommissionRates    `json:"standardCommission"`
	SpecialCommission  CommissionRates    `json:"specialCommission"`
	TaxCommission      CommissionRates    `json:"taxCommission"`
	Discount           CommissionDiscount `json:"discount"`
	Raw                json.RawMessage    `json:"-"`
}

// AccountCommission은 상품별 계정 수수료율과 BNB 수수료 할인 설정을 조회한다.
func (client *Client) AccountCommission(
	ctx context.Context,
	request AccountCommissionRequest,
	options ...trade.RequestOption,
) (AccountCommission, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return AccountCommission{}, err
	}
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	body, err := client.readSigned(ctx, "/api/v3/account/commission", values, 20, options...)
	if err != nil {
		return AccountCommission{}, err
	}
	var commission AccountCommission
	if err := decodeJSON(body, &commission); err != nil {
		return AccountCommission{}, err
	}
	commission.Raw = cloneBytes(body)
	return commission, nil
}

// RateLimitCount는 현재 사용량이 포함된 요청 제한 규칙이다.
type RateLimitCount struct {
	Type        string `json:"rateLimitType"`
	Interval    string `json:"interval"`
	IntervalNum int    `json:"intervalNum"`
	Limit       int    `json:"limit"`
	Count       int    `json:"count"`
}

// OrderRateLimits는 모든 구간의 미체결 주문 count를 조회한다.
func (client *Client) OrderRateLimits(ctx context.Context, options ...trade.RequestOption) ([]RateLimitCount, error) {
	body, err := client.readSigned(ctx, "/api/v3/rateLimit/order", nil, 40, options...)
	if err != nil {
		return nil, err
	}
	var limits []RateLimitCount
	if err := decodeJSON(body, &limits); err != nil {
		return nil, err
	}
	return limits, nil
}

// OrderAmendmentsRequest는 주문 정정 이력의 조회 조건이다. Symbol과 OrderID는 필수다.
type OrderAmendmentsRequest struct {
	Symbol          string
	OrderID         int64
	FromExecutionID *int64
	Limit           int
}

func (request OrderAmendmentsRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.OrderID <= 0 {
		return validationError("order amendments orderId must be positive")
	}
	if request.FromExecutionID != nil && *request.FromExecutionID < 0 {
		return validationError("order amendments fromExecutionId cannot be negative")
	}
	return validateOptionalLimit("order amendments", request.Limit, 1000)
}

func (request OrderAmendmentsRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	setInt64(values, "orderId", &request.OrderID)
	setInt64(values, "fromExecutionId", request.FromExecutionID)
	setLimit(values, request.Limit)
	return values
}

// OrderAmendment는 주문 수량 정정 이력 한 건이다.
type OrderAmendment struct {
	Symbol                string          `json:"symbol"`
	OrderID               int64           `json:"orderId"`
	ExecutionID           int64           `json:"executionId"`
	OriginalClientOrderID string          `json:"origClientOrderId"`
	NewClientOrderID      string          `json:"newClientOrderId"`
	OriginalQuantity      string          `json:"origQty"`
	NewQuantity           string          `json:"newQty"`
	Time                  int64           `json:"time"`
	Raw                   json.RawMessage `json:"-"`
}

// OrderAmendments는 주문 한 건의 수량 정정 이력을 조회한다.
func (client *Client) OrderAmendments(
	ctx context.Context,
	request OrderAmendmentsRequest,
	options ...trade.RequestOption,
) ([]OrderAmendment, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := client.readSigned(ctx, "/api/v3/order/amendments", request.values(), 4, options...)
	if err != nil {
		return nil, err
	}
	return decodeList(body, func(amendment *OrderAmendment, raw json.RawMessage) { amendment.Raw = raw })
}

// MyFiltersRequest는 계정에 적용된 필터를 조회할 상품을 지정한다.
type MyFiltersRequest struct {
	Symbol string
}

// AssetFilter는 계정에 적용된 자산 필터다. 현재 MAX_ASSET만 정의되어 있다.
type AssetFilter struct {
	FilterType string `json:"filterType"`
	Limit      string `json:"limit"`
	Asset      string `json:"asset"`
}

// AccountFilters는 상품에 대해 계정에 적용되는 필터와 요청 제한 사용량이다.
// 거래소 필터와 상품 필터는 ExchangeInfo와 같이 원본 JSON으로 보존한다.
type AccountFilters struct {
	ExchangeFilters []json.RawMessage `json:"exchangeFilters"`
	SymbolFilters   []json.RawMessage `json:"symbolFilters"`
	AssetFilters    []AssetFilter     `json:"assetFilters"`
	RateLimits      []RateLimitCount  `json:"rateLimits"`
	Raw             json.RawMessage   `json:"-"`
}

// MyFilters는 지정한 상품에서 계정에 적용되는 필터를 조회한다.
// 계정에 MAX_ASSET 필터가 걸려 있는지는 이 엔드포인트로만 확인할 수 있다.
func (client *Client) MyFilters(
	ctx context.Context,
	request MyFiltersRequest,
	options ...trade.RequestOption,
) (AccountFilters, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return AccountFilters{}, err
	}
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	body, err := client.readSigned(ctx, "/api/v3/myFilters", values, 40, options...)
	if err != nil {
		return AccountFilters{}, err
	}
	var filters AccountFilters
	if err := decodeJSON(body, &filters); err != nil {
		return AccountFilters{}, err
	}
	filters.Raw = cloneBytes(body)
	return filters, nil
}
