package binance

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	trade "github.com/proven-trade/proven-trade-sdk"
)

var (
	positiveDecimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
	clientOrderIDPattern   = regexp.MustCompile(`^[.A-Z:/a-z0-9_-]{1,36}$`)
)

// ExchangeInfoRequest는 상품 메타데이터 조회 조건이다.
type ExchangeInfoRequest struct {
	Symbol string
}

func (request ExchangeInfoRequest) values() url.Values {
	values := make(url.Values)
	if request.Symbol != "" {
		values.Set("symbol", request.Symbol)
	}
	return values
}

// TickerPriceRequest는 최신 가격을 조회할 단일 상품을 지정한다.
type TickerPriceRequest struct {
	Symbol string
}

// AccountRequest는 계정 정보 조회 옵션이다.
type AccountRequest struct {
	OmitZeroBalances bool
}

// NewOrderRequest는 Binance Spot 신규 주문 파라미터다.
type NewOrderRequest struct {
	Symbol                  string
	Side                    Side
	Type                    OrderType
	TimeInForce             TimeInForce
	Quantity                string
	QuoteOrderQuantity      string
	Price                   string
	ClientOrderID           string
	StrategyID              *int64
	StrategyType            *int64
	StopPrice               string
	TrailingDelta           *int64
	IcebergQuantity         string
	ResponseType            NewOrderResponseType
	SelfTradePreventionMode string
	PegPriceType            string
	PegOffsetValue          *int
	PegOffsetType           string
}

func (request NewOrderRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("side must be BUY or SELL")
	}
	if !request.Type.valid() {
		return validationError("unsupported order type %q", request.Type)
	}
	for name, value := range map[string]string{
		"quantity":      request.Quantity,
		"quoteOrderQty": request.QuoteOrderQuantity,
		"price":         request.Price,
		"stopPrice":     request.StopPrice,
		"icebergQty":    request.IcebergQuantity,
	} {
		if value != "" {
			if err := validatePositiveDecimal(name, value); err != nil {
				return err
			}
		}
	}
	if request.Quantity != "" && request.QuoteOrderQuantity != "" {
		return validationError("quantity and quoteOrderQty cannot both be set")
	}
	if request.ClientOrderID != "" && !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("newClientOrderId has an invalid format")
	}
	if request.StrategyType != nil && *request.StrategyType < 1_000_000 {
		return validationError("strategyType must be at least 1000000")
	}
	if request.TrailingDelta != nil && *request.TrailingDelta <= 0 {
		return validationError("trailingDelta must be positive")
	}
	if request.ResponseType != "" && !request.ResponseType.valid() {
		return validationError("unsupported response type %q", request.ResponseType)
	}
	if request.TimeInForce != "" && !request.TimeInForce.valid() {
		return validationError("unsupported time in force %q", request.TimeInForce)
	}
	if request.IcebergQuantity != "" && request.TimeInForce != TimeInForceGTC {
		return validationError("iceberg order requires GTC time in force")
	}
	if (request.PegOffsetValue == nil) != (request.PegOffsetType == "") {
		return validationError("pegOffsetValue and pegOffsetType must be set together")
	}
	if request.PegPriceType != "" && request.PegPriceType != "PRIMARY_PEG" && request.PegPriceType != "MARKET_PEG" {
		return validationError("pegPriceType must be PRIMARY_PEG or MARKET_PEG")
	}
	if request.PegOffsetType != "" && request.PegOffsetType != "PRICE_LEVEL" {
		return validationError("pegOffsetType must be PRICE_LEVEL")
	}
	if request.PegOffsetValue != nil && (*request.PegOffsetValue < 0 || *request.PegOffsetValue > 100) {
		return validationError("pegOffsetValue must be between 0 and 100")
	}

	hasTrigger := request.StopPrice != "" || request.TrailingDelta != nil
	switch request.Type {
	case OrderTypeLimit:
		if request.TimeInForce == "" || request.Quantity == "" ||
			(request.Price == "" && request.PegPriceType == "") {
			return validationError("LIMIT requires timeInForce, quantity, and price or pegPriceType")
		}
	case OrderTypeMarket:
		if (request.Quantity == "") == (request.QuoteOrderQuantity == "") {
			return validationError("MARKET requires exactly one of quantity or quoteOrderQty")
		}
		if request.Price != "" || request.TimeInForce != "" || request.PegPriceType != "" || request.IcebergQuantity != "" {
			return validationError("MARKET does not accept price, timeInForce, peg, or iceberg parameters")
		}
	case OrderTypeLimitMaker:
		if request.Quantity == "" || (request.Price == "" && request.PegPriceType == "") {
			return validationError("LIMIT_MAKER requires quantity and price or pegPriceType")
		}
		if request.TimeInForce != "" {
			return validationError("LIMIT_MAKER does not accept timeInForce")
		}
	case OrderTypeStopLoss, OrderTypeTakeProfit:
		if request.Quantity == "" || !hasTrigger {
			return validationError("%s requires quantity and stopPrice or trailingDelta", request.Type)
		}
	case OrderTypeStopLossLimit, OrderTypeTakeProfitLimit:
		if request.TimeInForce == "" || request.Quantity == "" ||
			(request.Price == "" && request.PegPriceType == "") || !hasTrigger {
			return validationError(
				"%s requires timeInForce, quantity, price or pegPriceType, and stopPrice or trailingDelta",
				request.Type,
			)
		}
	}
	return nil
}

func (request NewOrderRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	values.Set("side", string(request.Side))
	values.Set("type", string(request.Type))
	setIfNotEmpty(values, "timeInForce", string(request.TimeInForce))
	setIfNotEmpty(values, "quantity", request.Quantity)
	setIfNotEmpty(values, "quoteOrderQty", request.QuoteOrderQuantity)
	setIfNotEmpty(values, "price", request.Price)
	setIfNotEmpty(values, "newClientOrderId", request.ClientOrderID)
	setInt64(values, "strategyId", request.StrategyID)
	setInt64(values, "strategyType", request.StrategyType)
	setIfNotEmpty(values, "stopPrice", request.StopPrice)
	setInt64(values, "trailingDelta", request.TrailingDelta)
	setIfNotEmpty(values, "icebergQty", request.IcebergQuantity)
	setIfNotEmpty(values, "newOrderRespType", string(request.ResponseType))
	setIfNotEmpty(values, "selfTradePreventionMode", request.SelfTradePreventionMode)
	setIfNotEmpty(values, "pegPriceType", request.PegPriceType)
	if request.PegOffsetValue != nil {
		values.Set("pegOffsetValue", strconv.Itoa(*request.PegOffsetValue))
	}
	setIfNotEmpty(values, "pegOffsetType", request.PegOffsetType)
	return values
}

// QueryOrderRequest는 주문 ID 또는 client order ID로 주문을 조회한다.
type QueryOrderRequest struct {
	Symbol                string
	OrderID               *int64
	OriginalClientOrderID string
}

func (request QueryOrderRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	return validateOrderIdentity(request.OrderID, request.OriginalClientOrderID)
}

func (request QueryOrderRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	setInt64(values, "orderId", request.OrderID)
	setIfNotEmpty(values, "origClientOrderId", request.OriginalClientOrderID)
	return values
}

// CancelOrderRequest는 활성 주문 취소 조건이다.
type CancelOrderRequest struct {
	Symbol                string
	OrderID               *int64
	OriginalClientOrderID string
	NewClientOrderID      string
	Restriction           CancelRestriction
}

func (request CancelOrderRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateOrderIdentity(request.OrderID, request.OriginalClientOrderID); err != nil {
		return err
	}
	if request.Restriction != "" &&
		request.Restriction != CancelOnlyNew &&
		request.Restriction != CancelOnlyPartiallyFilled {
		return validationError("unsupported cancel restriction %q", request.Restriction)
	}
	if request.NewClientOrderID != "" && !clientOrderIDPattern.MatchString(request.NewClientOrderID) {
		return validationError("newClientOrderId has an invalid format")
	}
	return nil
}

func (request CancelOrderRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	setInt64(values, "orderId", request.OrderID)
	setIfNotEmpty(values, "origClientOrderId", request.OriginalClientOrderID)
	setIfNotEmpty(values, "newClientOrderId", request.NewClientOrderID)
	setIfNotEmpty(values, "cancelRestrictions", string(request.Restriction))
	return values
}

func validateSymbol(symbol string) error {
	if strings.TrimSpace(symbol) == "" || strings.TrimSpace(symbol) != symbol {
		return validationError("symbol is required and cannot have surrounding whitespace")
	}
	return nil
}

func validateOrderIdentity(orderID *int64, clientOrderID string) error {
	cleanClientOrderID := strings.TrimSpace(clientOrderID)
	if orderID == nil && cleanClientOrderID == "" {
		return validationError("orderId or origClientOrderId is required")
	}
	if orderID != nil && *orderID <= 0 {
		return validationError("orderId must be positive")
	}
	if clientOrderID != "" && (cleanClientOrderID != clientOrderID || !clientOrderIDPattern.MatchString(clientOrderID)) {
		return validationError("origClientOrderId has an invalid format")
	}
	return nil
}

func validatePositiveDecimal(name, value string) error {
	if !positiveDecimalPattern.MatchString(value) || strings.Trim(value, "0.") == "" {
		return validationError("%s must be a positive decimal string", name)
	}
	return nil
}

func validationError(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", trade.ErrValidation, fmt.Sprintf(format, arguments...))
}

func setIfNotEmpty(values url.Values, key, value string) {
	if value != "" {
		values.Set(key, value)
	}
}

func setInt64(values url.Values, key string, value *int64) {
	if value != nil {
		values.Set(key, strconv.FormatInt(*value, 10))
	}
}

func (orderType OrderType) valid() bool {
	switch orderType {
	case OrderTypeLimit,
		OrderTypeMarket,
		OrderTypeStopLoss,
		OrderTypeStopLossLimit,
		OrderTypeTakeProfit,
		OrderTypeTakeProfitLimit,
		OrderTypeLimitMaker:
		return true
	default:
		return false
	}
}

func (timeInForce TimeInForce) valid() bool {
	return timeInForce == TimeInForceGTC || timeInForce == TimeInForceIOC || timeInForce == TimeInForceFOK
}

func (responseType NewOrderResponseType) valid() bool {
	return responseType == NewOrderResponseACK ||
		responseType == NewOrderResponseResult ||
		responseType == NewOrderResponseFull
}
