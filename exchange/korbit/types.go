package korbit

import "encoding/json"

// Ticker는 거래쌍의 24시간 현재가와 거래량이다.
type Ticker struct {
	Symbol             string          `json:"symbol"`
	Open               string          `json:"open"`
	High               string          `json:"high"`
	Low                string          `json:"low"`
	Close              string          `json:"close"`
	PreviousClose      string          `json:"prevClose"`
	PriceChange        string          `json:"priceChange"`
	PriceChangePercent string          `json:"priceChangePercent"`
	Volume             string          `json:"volume"`
	QuoteVolume        string          `json:"quoteVolume"`
	BestBidPrice       string          `json:"bestBidPrice"`
	BestAskPrice       string          `json:"bestAskPrice"`
	LastTradedAt       int64           `json:"lastTradedAt"`
	Raw                json.RawMessage `json:"-"`
}

// OrderBookLevel은 가격별 합산 호가 수량과 선택적인 금액이다.
type OrderBookLevel struct {
	Price  string `json:"price"`
	Qty    string `json:"qty"`
	Amount string `json:"amt"`
}

// OrderBook은 거래쌍의 호가 스냅샷이다.
type OrderBook struct {
	Timestamp int64            `json:"timestamp"`
	Bids      []OrderBookLevel `json:"bids"`
	Asks      []OrderBookLevel `json:"asks"`
	Raw       json.RawMessage  `json:"-"`
}

// PublicTrade는 공개 최근 체결 한 건이다.
type PublicTrade struct {
	Timestamp    int64           `json:"timestamp"`
	Price        string          `json:"price"`
	Qty          string          `json:"qty"`
	IsBuyerTaker bool            `json:"isBuyerTaker"`
	TradeID      int64           `json:"tradeId"`
	Raw          json.RawMessage `json:"-"`
}

// Candle은 한 구간의 OHLCV 정보다.
type Candle struct {
	Timestamp int64           `json:"timestamp"`
	Open      string          `json:"open"`
	High      string          `json:"high"`
	Low       string          `json:"low"`
	Close     string          `json:"close"`
	Volume    string          `json:"volume"`
	Raw       json.RawMessage `json:"-"`
}

// CurrencyPair는 거래쌍 상태와 주문 금액 범위다.
type CurrencyPair struct {
	Symbol            string          `json:"symbol"`
	Status            string          `json:"status"`
	BaseCurrency      string          `json:"baseCurrency"`
	QuoteCurrency     string          `json:"quoteCurrency"`
	MinimumOrderValue string          `json:"minOrderValue"`
	MaximumOrderValue string          `json:"maxOrderValue"`
	Raw               json.RawMessage `json:"-"`
}

// TickSizeTier는 가격 구간별 최소 호가 단위다.
type TickSizeTier struct {
	PriceGreaterThanOrEqual string `json:"priceGte"`
	TickSize                string `json:"tickSize"`
}

// TickSizePolicy는 거래쌍의 호가 단위 정책과 호가 묶음 단위다.
type TickSizePolicy struct {
	Symbol          string          `json:"symbol"`
	TickSizePolicy  []TickSizeTier  `json:"tickSizePolicy"`
	OrderBookLevels []string        `json:"orderbookLevels"`
	Raw             json.RawMessage `json:"-"`
}

// ServerTime은 코빗 서버 Unix millisecond 시각이다.
type ServerTime struct {
	Time int64           `json:"time"`
	Raw  json.RawMessage `json:"-"`
}

// Balance는 자산별 총액과 사용 가능·잠금 수량이다.
type Balance struct {
	Currency        string          `json:"currency"`
	Balance         string          `json:"balance"`
	Available       string          `json:"available"`
	TradeInUse      string          `json:"tradeInUse"`
	WithdrawalInUse string          `json:"withdrawalInUse"`
	AveragePrice    string          `json:"avgPrice"`
	Raw             json.RawMessage `json:"-"`
}

// Side는 주문과 체결의 매수 또는 매도 방향이다.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// OrderType은 주문 가격 결정 방식이다.
type OrderType string

const (
	OrderTypeLimit  OrderType = "limit"
	OrderTypeMarket OrderType = "market"
	OrderTypeBest   OrderType = "best"
)

// TimeInForce는 주문 체결 및 만료 정책이다.
type TimeInForce string

const (
	TimeInForceGTC      TimeInForce = "gtc"
	TimeInForceIOC      TimeInForce = "ioc"
	TimeInForceFOK      TimeInForce = "fok"
	TimeInForcePostOnly TimeInForce = "po"
)

// OrderStatus는 주문 처리 상태다.
type OrderStatus string

const (
	OrderStatusPending                 OrderStatus = "pending"
	OrderStatusOpen                    OrderStatus = "open"
	OrderStatusFilled                  OrderStatus = "filled"
	OrderStatusCanceled                OrderStatus = "canceled"
	OrderStatusPartiallyFilled         OrderStatus = "partiallyFilled"
	OrderStatusPartiallyFilledCanceled OrderStatus = "partiallyFilledCanceled"
	OrderStatusExpired                 OrderStatus = "expired"
)

// Order는 주문 상태와 누적 체결 정보다.
type Order struct {
	OrderID       int64           `json:"orderId"`
	ClientOrderID string          `json:"clientOrderId"`
	Symbol        string          `json:"symbol"`
	OrderType     OrderType       `json:"orderType"`
	Side          Side            `json:"side"`
	TimeInForce   TimeInForce     `json:"timeInForce"`
	Price         string          `json:"price"`
	Qty           string          `json:"qty"`
	Amount        string          `json:"amt"`
	FilledQty     string          `json:"filledQty"`
	FilledAmount  string          `json:"filledAmt"`
	AveragePrice  string          `json:"avgPrice"`
	CreatedAt     int64           `json:"createdAt"`
	LastFilledAt  *int64          `json:"lastFilledAt"`
	TriggeredAt   *int64          `json:"triggeredAt"`
	Status        OrderStatus     `json:"status"`
	Raw           json.RawMessage `json:"-"`
}

// OrderReference는 주문 생성 접수 결과다.
type OrderReference struct {
	OrderID int64
	Raw     json.RawMessage
}

// PrivateTrade는 내 계정의 최근 체결 한 건이다.
type PrivateTrade struct {
	Symbol      string          `json:"symbol"`
	TradeID     int64           `json:"tradeId"`
	OrderID     int64           `json:"orderId"`
	Side        Side            `json:"side"`
	Price       string          `json:"price"`
	Qty         string          `json:"qty"`
	Amount      string          `json:"amt"`
	TradedAt    int64           `json:"tradedAt"`
	IsTaker     bool            `json:"isTaker"`
	FeeCurrency string          `json:"feeCurrency"`
	FeeQty      string          `json:"feeQty"`
	Raw         json.RawMessage `json:"-"`
}

// CancelResult는 취소 요청이 거래소에 접수됐음을 나타낸다.
type CancelResult struct {
	Accepted bool
	Raw      json.RawMessage
}
