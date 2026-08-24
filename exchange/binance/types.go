// Package binance는 Binance Spot REST API 어댑터를 제공한다.
package binance

import "encoding/json"

// Side는 주문의 매수 또는 매도 방향이다.
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// OrderType은 Binance Spot 주문 종류다.
type OrderType string

const (
	OrderTypeLimit           OrderType = "LIMIT"
	OrderTypeMarket          OrderType = "MARKET"
	OrderTypeStopLoss        OrderType = "STOP_LOSS"
	OrderTypeStopLossLimit   OrderType = "STOP_LOSS_LIMIT"
	OrderTypeTakeProfit      OrderType = "TAKE_PROFIT"
	OrderTypeTakeProfitLimit OrderType = "TAKE_PROFIT_LIMIT"
	OrderTypeLimitMaker      OrderType = "LIMIT_MAKER"
)

// TimeInForce는 주문의 유효 기간 정책이다.
type TimeInForce string

const (
	TimeInForceGTC TimeInForce = "GTC"
	TimeInForceIOC TimeInForce = "IOC"
	TimeInForceFOK TimeInForce = "FOK"
)

// NewOrderResponseType은 신규 주문 응답의 상세 수준이다.
type NewOrderResponseType string

const (
	NewOrderResponseACK    NewOrderResponseType = "ACK"
	NewOrderResponseResult NewOrderResponseType = "RESULT"
	NewOrderResponseFull   NewOrderResponseType = "FULL"
)

// OrderStatus는 Binance Spot 주문 상태다.
type OrderStatus string

const (
	OrderStatusNew             OrderStatus = "NEW"
	OrderStatusPendingNew      OrderStatus = "PENDING_NEW"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusCanceled        OrderStatus = "CANCELED"
	OrderStatusPendingCancel   OrderStatus = "PENDING_CANCEL"
	OrderStatusRejected        OrderStatus = "REJECTED"
	OrderStatusExpired         OrderStatus = "EXPIRED"
	OrderStatusExpiredInMatch  OrderStatus = "EXPIRED_IN_MATCH"
)

// CancelRestriction은 주문 취소가 허용되는 상태를 제한한다.
type CancelRestriction string

const (
	CancelOnlyNew             CancelRestriction = "ONLY_NEW"
	CancelOnlyPartiallyFilled CancelRestriction = "ONLY_PARTIALLY_FILLED"
)

// RateLimit은 exchangeInfo가 반환하는 요청 제한 규칙이다.
type RateLimit struct {
	Type        string `json:"rateLimitType"`
	Interval    string `json:"interval"`
	IntervalNum int    `json:"intervalNum"`
	Limit       int    `json:"limit"`
}

// ExchangeInfo는 거래 규칙과 상품 메타데이터다.
type ExchangeInfo struct {
	Timezone        string            `json:"timezone"`
	ServerTime      int64             `json:"serverTime"`
	RateLimits      []RateLimit       `json:"rateLimits"`
	ExchangeFilters []json.RawMessage `json:"exchangeFilters"`
	Symbols         []Symbol          `json:"symbols"`
	Raw             json.RawMessage   `json:"-"`
}

// Symbol은 Binance Spot 상품의 거래 설정이다.
type Symbol struct {
	Symbol                          string            `json:"symbol"`
	Status                          string            `json:"status"`
	BaseAsset                       string            `json:"baseAsset"`
	BaseAssetPrecision              int               `json:"baseAssetPrecision"`
	QuoteAsset                      string            `json:"quoteAsset"`
	QuoteAssetPrecision             int               `json:"quoteAssetPrecision"`
	BaseCommissionPrecision         int               `json:"baseCommissionPrecision"`
	QuoteCommissionPrecision        int               `json:"quoteCommissionPrecision"`
	OrderTypes                      []OrderType       `json:"orderTypes"`
	IcebergAllowed                  bool              `json:"icebergAllowed"`
	OCOAllowed                      bool              `json:"ocoAllowed"`
	OTOAllowed                      bool              `json:"otoAllowed"`
	QuoteOrderQuantityMarketAllowed bool              `json:"quoteOrderQtyMarketAllowed"`
	AllowTrailingStop               bool              `json:"allowTrailingStop"`
	CancelReplaceAllowed            bool              `json:"cancelReplaceAllowed"`
	AmendAllowed                    bool              `json:"amendAllowed"`
	PegInstructionsAllowed          bool              `json:"pegInstructionsAllowed"`
	SpotTradingAllowed              bool              `json:"isSpotTradingAllowed"`
	MarginTradingAllowed            bool              `json:"isMarginTradingAllowed"`
	Filters                         []json.RawMessage `json:"filters"`
	Permissions                     []string          `json:"permissions"`
	PermissionSets                  [][]string        `json:"permissionSets"`
	DefaultSelfTradePreventionMode  string            `json:"defaultSelfTradePreventionMode"`
	AllowedSelfTradePreventionModes []string          `json:"allowedSelfTradePreventionModes"`
}

// TickerPrice는 상품의 최신 가격이다.
type TickerPrice struct {
	Symbol string          `json:"symbol"`
	Price  string          `json:"price"`
	Raw    json.RawMessage `json:"-"`
}

// CommissionRates는 계정에 적용되는 수수료율이다.
type CommissionRates struct {
	Maker  string `json:"maker"`
	Taker  string `json:"taker"`
	Buyer  string `json:"buyer"`
	Seller string `json:"seller"`
}

// Balance는 자산의 사용 가능 및 잠금 수량이다.
type Balance struct {
	Asset  string `json:"asset"`
	Free   string `json:"free"`
	Locked string `json:"locked"`
}

// Account는 Binance Spot 계정 정보다.
type Account struct {
	MakerCommission            int             `json:"makerCommission"`
	TakerCommission            int             `json:"takerCommission"`
	BuyerCommission            int             `json:"buyerCommission"`
	SellerCommission           int             `json:"sellerCommission"`
	CommissionRates            CommissionRates `json:"commissionRates"`
	CanTrade                   bool            `json:"canTrade"`
	CanWithdraw                bool            `json:"canWithdraw"`
	CanDeposit                 bool            `json:"canDeposit"`
	Brokered                   bool            `json:"brokered"`
	RequireSelfTradePrevention bool            `json:"requireSelfTradePrevention"`
	PreventSOR                 bool            `json:"preventSor"`
	UpdateTime                 int64           `json:"updateTime"`
	AccountType                string          `json:"accountType"`
	Balances                   []Balance       `json:"balances"`
	Permissions                []string        `json:"permissions"`
	UID                        int64           `json:"uid"`
	Raw                        json.RawMessage `json:"-"`
}

// Fill은 신규 주문 응답에 포함되는 개별 체결이다.
type Fill struct {
	Price           string `json:"price"`
	Quantity        string `json:"qty"`
	Commission      string `json:"commission"`
	CommissionAsset string `json:"commissionAsset"`
	TradeID         int64  `json:"tradeId"`
}

// Order는 주문 생성, 조회, 취소 응답의 공통 필드를 보존한다.
type Order struct {
	Symbol                  string          `json:"symbol"`
	OrderID                 int64           `json:"orderId"`
	OrderListID             int64           `json:"orderListId"`
	ClientOrderID           string          `json:"clientOrderId"`
	OriginalClientOrderID   string          `json:"origClientOrderId"`
	TransactionTime         int64           `json:"transactTime"`
	Price                   string          `json:"price"`
	OriginalQuantity        string          `json:"origQty"`
	ExecutedQuantity        string          `json:"executedQty"`
	OriginalQuoteQuantity   string          `json:"origQuoteOrderQty"`
	CumulativeQuoteQuantity string          `json:"cummulativeQuoteQty"`
	Status                  OrderStatus     `json:"status"`
	TimeInForce             TimeInForce     `json:"timeInForce"`
	Type                    OrderType       `json:"type"`
	Side                    Side            `json:"side"`
	StopPrice               string          `json:"stopPrice"`
	IcebergQuantity         string          `json:"icebergQty"`
	Time                    int64           `json:"time"`
	UpdateTime              int64           `json:"updateTime"`
	WorkingTime             int64           `json:"workingTime"`
	Working                 bool            `json:"isWorking"`
	SelfTradePreventionMode string          `json:"selfTradePreventionMode"`
	Fills                   []Fill          `json:"fills"`
	Raw                     json.RawMessage `json:"-"`
}
