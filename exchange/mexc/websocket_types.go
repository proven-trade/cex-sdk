package mexc

import "encoding/json"

// StreamChannel은 MEXC Spot V3 Protobuf WebSocket 채널이다.
type StreamChannel string

const (
	StreamChannelAggregateTrades StreamChannel = "aggregate_trades"
	StreamChannelCandles         StreamChannel = "candles"
	StreamChannelDiffDepth       StreamChannel = "diff_depth"
	StreamChannelPartialDepth    StreamChannel = "partial_depth"
	StreamChannelBookTicker      StreamChannel = "book_ticker"
	StreamChannelAccount         StreamChannel = "account"
	StreamChannelAccountDeals    StreamChannel = "account_deals"
	StreamChannelAccountOrders   StreamChannel = "account_orders"
)

// StreamUpdateInterval은 합산 체결·호가 통지 주기다.
type StreamUpdateInterval string

const (
	StreamUpdate10Millis  StreamUpdateInterval = "10ms"
	StreamUpdate100Millis StreamUpdateInterval = "100ms"
)

// StreamCandleInterval은 MEXC Protobuf 캔들 채널의 구간 이름이다.
type StreamCandleInterval string

const (
	StreamCandle1Minute   StreamCandleInterval = "Min1"
	StreamCandle5Minutes  StreamCandleInterval = "Min5"
	StreamCandle15Minutes StreamCandleInterval = "Min15"
	StreamCandle30Minutes StreamCandleInterval = "Min30"
	StreamCandle1Hour     StreamCandleInterval = "Min60"
	StreamCandle4Hours    StreamCandleInterval = "Hour4"
	StreamCandle8Hours    StreamCandleInterval = "Hour8"
	StreamCandle1Day      StreamCandleInterval = "Day1"
	StreamCandle1Week     StreamCandleInterval = "Week1"
	StreamCandle1Month    StreamCandleInterval = "Month1"
)

// StreamDepthLevel은 부분 호가에 포함할 가격 단계 수다.
type StreamDepthLevel int

const (
	StreamDepth5  StreamDepthLevel = 5
	StreamDepth10 StreamDepthLevel = 10
	StreamDepth20 StreamDepthLevel = 20
)

// StreamSubscription은 채널과 거래쌍·주기·깊이 선택 값을 정의한다.
type StreamSubscription struct {
	Channel        StreamChannel
	Symbol         string
	UpdateInterval StreamUpdateInterval
	CandleInterval StreamCandleInterval
	Depth          StreamDepthLevel
}

// StreamRequest는 연결 직후와 재연결 때 복구할 구독 목록이다.
type StreamRequest struct {
	Subscriptions []StreamSubscription
}

// StreamControl은 구독·구독 해제 응답 또는 PONG text 메시지다.
type StreamControl struct {
	ID      uint64
	Code    int
	Message string
	Raw     json.RawMessage
}

// StreamMessage는 JSON 제어 응답 또는 binary Protobuf 이벤트 한 건이다.
type StreamMessage struct {
	Channel    string
	Symbol     string
	SymbolID   string
	CreateTime int64
	SendTime   int64

	Control         *StreamControl
	AggregateTrades *StreamAggregateTrades
	DiffDepth       *StreamDiffDepth
	PartialDepth    *StreamPartialDepth
	BookTicker      *StreamBookTicker
	Candle          *StreamCandle
	Account         *StreamAccount
	AccountDeal     *StreamAccountDeal
	AccountOrder    *StreamAccountOrder
	Raw             []byte
}

// StreamAggregateTrades는 한 통지에 묶여 전달된 공개 체결 목록이다.
type StreamAggregateTrades struct {
	Deals     []StreamAggregateTrade
	EventType string
	Raw       []byte
}

// StreamAggregateTrade는 MEXC가 합산해 보낸 공개 체결 한 건이다.
type StreamAggregateTrade struct {
	Price     string
	Quantity  string
	TradeType StreamTradeType
	Time      int64
	TradeID   string
	Raw       []byte
}

// StreamTradeType은 WebSocket 체결·주문의 매수 또는 매도 방향이다.
type StreamTradeType int32

const (
	StreamTradeBuy  StreamTradeType = 1
	StreamTradeSell StreamTradeType = 2
)

// StreamDiffDepth는 version 범위와 절대 수량을 가진 증분 호가다.
type StreamDiffDepth struct {
	Asks                []BookLevel
	Bids                []BookLevel
	EventType           string
	FromVersion         string
	ToVersion           string
	LastOrderCreateTime int64
	Raw                 []byte
}

// StreamPartialDepth는 지정 단계의 완전 호가와 현재 version이다.
type StreamPartialDepth struct {
	Asks                []BookLevel
	Bids                []BookLevel
	EventType           string
	Version             string
	LastOrderCreateTime int64
	Raw                 []byte
}

// StreamBookTicker는 최우선 매수·매도 호가와 version이다.
type StreamBookTicker struct {
	BidPrice            string
	BidQuantity         string
	AskPrice            string
	AskQuantity         string
	Version             string
	LastOrderCreateTime int64
	Raw                 []byte
}

// StreamCandle은 현재 구간의 Spot OHLCV 갱신이다.
type StreamCandle struct {
	Interval    StreamCandleInterval
	WindowStart int64
	Open        string
	Close       string
	High        string
	Low         string
	Volume      string
	Amount      string
	WindowEnd   int64
	Raw         []byte
}

// StreamAccount는 변경된 자산의 사용 가능·동결 잔고다.
type StreamAccount struct {
	Asset         string
	CoinID        string
	Balance       string
	BalanceChange string
	Frozen        string
	FrozenChange  string
	ChangeType    string
	Time          int64
	Raw           []byte
}

// StreamAccountDeal은 계정 주문에서 발생한 체결과 수수료다.
type StreamAccountDeal struct {
	Price         string
	Quantity      string
	Amount        string
	TradeType     StreamTradeType
	Maker         bool
	SelfTrade     bool
	TradeID       string
	ClientOrderID string
	OrderID       string
	FeeAmount     string
	FeeCurrency   string
	Time          int64
	Raw           []byte
}

// StreamAccountOrder는 계정 주문의 최신 상태와 체결 누계다.
type StreamAccountOrder struct {
	ID                 string
	ClientOrderID      string
	Price              string
	Quantity           string
	Amount             string
	AveragePrice       string
	OrderType          StreamOrderType
	TradeType          StreamTradeType
	Maker              bool
	RemainingAmount    string
	RemainingQuantity  string
	LastDealQuantity   string
	CumulativeQuantity string
	CumulativeAmount   string
	Status             StreamOrderStatus
	CreatedAt          int64
	Market             string
	TriggerType        int32
	TriggerPrice       string
	State              int32
	OCOID              string
	RouteFactor        string
	SymbolID           string
	MarketID           string
	MarketCurrencyID   string
	CurrencyID         string
	Raw                []byte
}

// StreamOrderType은 private 주문 이벤트의 주문 실행 방식이다.
type StreamOrderType int32

const (
	StreamOrderLimit             StreamOrderType = 1
	StreamOrderPostOnly          StreamOrderType = 2
	StreamOrderImmediateOrCancel StreamOrderType = 3
	StreamOrderFillOrKill        StreamOrderType = 4
	StreamOrderMarket            StreamOrderType = 5
	StreamOrderStop              StreamOrderType = 100
)

// StreamOrderStatus는 private 주문 이벤트의 수명주기 상태다.
type StreamOrderStatus int32

const (
	StreamOrderNew               StreamOrderStatus = 1
	StreamOrderFilled            StreamOrderStatus = 2
	StreamOrderPartiallyFilled   StreamOrderStatus = 3
	StreamOrderCanceled          StreamOrderStatus = 4
	StreamOrderPartiallyCanceled StreamOrderStatus = 5
)
