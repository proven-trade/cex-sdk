package binance

import (
	"context"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/cex-sdk"
)

// SOROrderRequest는 스마트 주문 라우팅(SOR)으로 낼 주문 파라미터다.
// SOR은 LIMIT과 MARKET 주문만 지원하고 quoteOrderQty를 지원하지 않으므로 Quantity가 필수다.
type SOROrderRequest struct {
	Symbol                  string
	Side                    Side
	Type                    OrderType
	TimeInForce             TimeInForce
	Quantity                string
	Price                   string
	ClientOrderID           string
	StrategyID              *int64
	StrategyType            *int64
	IcebergQuantity         string
	ResponseType            NewOrderResponseType
	SelfTradePreventionMode string
}

func (request SOROrderRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateSide("side", request.Side); err != nil {
		return err
	}
	if request.Type != OrderTypeLimit && request.Type != OrderTypeMarket {
		return validationError("SOR order type must be LIMIT or MARKET")
	}
	if err := validatePositiveDecimal("quantity", request.Quantity); err != nil {
		return err
	}
	if err := validateOptionalDecimal("price", request.Price); err != nil {
		return err
	}
	if err := validateOptionalDecimal("icebergQty", request.IcebergQuantity); err != nil {
		return err
	}
	if request.ClientOrderID != "" && !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("newClientOrderId has an invalid format")
	}
	if request.StrategyType != nil && *request.StrategyType < 1_000_000 {
		return validationError("strategyType must be at least 1000000")
	}
	if request.TimeInForce != "" && !request.TimeInForce.valid() {
		return validationError("unsupported time in force %q", request.TimeInForce)
	}
	if request.ResponseType != "" && !request.ResponseType.valid() {
		return validationError("unsupported response type %q", request.ResponseType)
	}
	switch request.Type {
	case OrderTypeLimit:
		if request.Price == "" || request.TimeInForce == "" {
			return validationError("SOR LIMIT requires price and timeInForce")
		}
	case OrderTypeMarket:
		if request.Price != "" || request.TimeInForce != "" || request.IcebergQuantity != "" {
			return validationError("SOR MARKET does not accept price, timeInForce, or iceberg parameters")
		}
	}
	return nil
}

func (request SOROrderRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	values.Set("side", string(request.Side))
	values.Set("type", string(request.Type))
	setIfNotEmpty(values, "timeInForce", string(request.TimeInForce))
	values.Set("quantity", request.Quantity)
	setIfNotEmpty(values, "price", request.Price)
	setIfNotEmpty(values, "newClientOrderId", request.ClientOrderID)
	setInt64(values, "strategyId", request.StrategyID)
	setInt64(values, "strategyType", request.StrategyType)
	setIfNotEmpty(values, "icebergQty", request.IcebergQuantity)
	setIfNotEmpty(values, "newOrderRespType", string(request.ResponseType))
	setIfNotEmpty(values, "selfTradePreventionMode", request.SelfTradePreventionMode)
	return values
}

// SORFill은 SOR 주문의 개별 체결이다. 일반 체결에 체결 유형과 배분 ID가 더해진다.
type SORFill struct {
	MatchType       string `json:"matchType"`
	Price           string `json:"price"`
	Quantity        string `json:"qty"`
	Commission      string `json:"commission"`
	CommissionAsset string `json:"commissionAsset"`
	TradeID         int64  `json:"tradeId"`
	AllocationID    int64  `json:"allocId"`
}

// SOROrderResult는 SOR 주문 응답이다. Order의 공통 필드에 SOR 전용 정보가 더해진다.
// Fills는 Order.Fills를 가려서 SOR 체결 형식으로 디코딩된다.
type SOROrderResult struct {
	Order
	Fills        []SORFill `json:"fills"`
	WorkingFloor string    `json:"workingFloor"`
	UsedSOR      bool      `json:"usedSor"`
}

// SOROrder는 스마트 주문 라우팅으로 주문을 낸다.
// 불명확한 전송 오류는 자동 재시도하지 않고 ErrUnknownExecutionState로 반환한다.
func (client *Client) SOROrder(
	ctx context.Context,
	request SOROrderRequest,
	options ...trade.RequestOption,
) (SOROrderResult, error) {
	if err := request.validate(); err != nil {
		return SOROrderResult{}, err
	}
	body, err := client.mutateSigned(ctx, http.MethodPost, "/api/v3/sor/order", request.values(), 1, 1, options...)
	if err != nil {
		return SOROrderResult{}, err
	}
	var result SOROrderResult
	if err := decodeJSON(body, &result); err != nil {
		return SOROrderResult{}, err
	}
	result.Order.Raw = cloneBytes(body)
	return result, nil
}

// TestSOROrderRequest는 SOR 주문 검증 요청이다. 주문은 매칭 엔진에 전달되지 않는다.
// ComputeCommissionRates를 켜면 weight 20을 사용하고 주문에 적용될 수수료율을 함께 반환한다.
type TestSOROrderRequest struct {
	Order                  SOROrderRequest
	ComputeCommissionRates bool
}

func (request TestSOROrderRequest) validate() error {
	return request.Order.validate()
}

func (request TestSOROrderRequest) values() url.Values {
	values := request.Order.values()
	if request.ComputeCommissionRates {
		values.Set("computeCommissionRates", "true")
	}
	return values
}

// TestSOROrder는 SOR 주문을 만들지 않고 생성 가능 여부만 검증한다.
// 공식 문서의 SOR 테스트 응답 예시에는 specialCommissionForOrder가 없으므로 SpecialCommissionForOrder는 보통 nil이다.
func (client *Client) TestSOROrder(
	ctx context.Context,
	request TestSOROrderRequest,
	options ...trade.RequestOption,
) (TestOrderResult, error) {
	if err := request.validate(); err != nil {
		return TestOrderResult{}, err
	}
	body, err := client.mutateSigned(
		ctx,
		http.MethodPost,
		"/api/v3/sor/order/test",
		request.values(),
		testOrderWeight(request.ComputeCommissionRates),
		0,
		options...,
	)
	if err != nil {
		return TestOrderResult{}, err
	}
	return decodeTestOrderResult(body)
}
