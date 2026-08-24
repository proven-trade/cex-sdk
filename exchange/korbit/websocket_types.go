package korbit

import (
	"encoding/json"
	"fmt"
)

// StreamChannel은 코빗 WebSocket 구독 채널이다.
type StreamChannel string

const (
	StreamChannelTicker    StreamChannel = "ticker"
	StreamChannelOrderBook StreamChannel = "orderbook"
	StreamChannelTrade     StreamChannel = "trade"
	StreamChannelMyOrder   StreamChannel = "myOrder"
	StreamChannelMyTrade   StreamChannel = "myTrade"
	StreamChannelMyAsset   StreamChannel = "myAsset"
)

// StreamSubscription은 채널과 거래쌍, 호가 묶음 단위, 하위 계정 범위를 정의한다.
type StreamSubscription struct {
	Channel     StreamChannel
	Symbols     []string
	Level       string
	AccountSeqs []int
}

// StreamRequest는 연결 직후 복구할 구독 목록이다.
type StreamRequest struct {
	Subscriptions []StreamSubscription
}

// StreamMessage는 데이터 이벤트 또는 구독 제어 응답 한 건이다.
type StreamMessage struct {
	RequestID    *int64
	Status       string
	Code         string
	ErrorMessage string
	Channel      StreamChannel
	Timestamp    int64
	Symbol       string
	Snapshot     *bool
	Data         json.RawMessage
	Raw          json.RawMessage
}

// Decode는 데이터 이벤트 payload를 지정한 타입으로 변환한다.
func (message StreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("Korbit stream decode target is nil")
	}
	if message.Status != "" || message.Channel == "" || len(message.Data) == 0 {
		return fmt.Errorf("Korbit stream message does not contain a data event")
	}
	if err := json.Unmarshal(message.Data, target); err != nil {
		return fmt.Errorf("decode Korbit stream event: %w", err)
	}
	return nil
}

// StreamTicker는 실시간 현재가와 24시간 통계다.
type StreamTicker struct {
	Open               string `json:"open"`
	High               string `json:"high"`
	Low                string `json:"low"`
	Close              string `json:"close"`
	PreviousClose      string `json:"prevClose"`
	PriceChange        string `json:"priceChange"`
	PriceChangePercent string `json:"priceChangePercent"`
	Volume             string `json:"volume"`
	QuoteVolume        string `json:"quoteVolume"`
	BestBidPrice       string `json:"bestBidPrice"`
	BestAskPrice       string `json:"bestAskPrice"`
	LastTradedAt       int64  `json:"lastTradedAt"`
}

// StreamOrderBook은 실시간 전체 호가 스냅샷이다.
type StreamOrderBook struct {
	Timestamp int64            `json:"timestamp"`
	Bids      []OrderBookLevel `json:"bids"`
	Asks      []OrderBookLevel `json:"asks"`
}

// StreamPublicTrade는 실시간 공개 체결 한 건이다.
type StreamPublicTrade struct {
	Timestamp    int64  `json:"timestamp"`
	Price        string `json:"price"`
	Qty          string `json:"qty"`
	IsBuyerTaker bool   `json:"isBuyerTaker"`
	TradeID      int64  `json:"tradeId"`
}

// StreamOrderStatus는 private 주문 이벤트의 처리 상태다.
type StreamOrderStatus string

const (
	StreamOrderStatusPending                 StreamOrderStatus = "pending"
	StreamOrderStatusUnfilled                StreamOrderStatus = "unfilled"
	StreamOrderStatusFilled                  StreamOrderStatus = "filled"
	StreamOrderStatusCanceled                StreamOrderStatus = "canceled"
	StreamOrderStatusPartiallyFilled         StreamOrderStatus = "partiallyFilled"
	StreamOrderStatusPartiallyFilledCanceled StreamOrderStatus = "partiallyFilledCanceled"
	StreamOrderStatusExpired                 StreamOrderStatus = "expired"
)

// StreamOrder는 private 주문 변경 한 건이다.
type StreamOrder struct {
	OrderID       int64             `json:"orderId"`
	Status        StreamOrderStatus `json:"status"`
	Side          Side              `json:"side"`
	OrderType     OrderType         `json:"orderType"`
	TimeInForce   TimeInForce       `json:"timeInForce"`
	Price         string            `json:"price"`
	Qty           string            `json:"qty"`
	FilledQty     string            `json:"filledQty"`
	Amount        string            `json:"amt"`
	FilledAmount  string            `json:"filledAmt"`
	AveragePrice  string            `json:"avgPrice"`
	CreatedAt     int64             `json:"createdAt"`
	LastFilledAt  *int64            `json:"lastFilledAt"`
	ClientOrderID string            `json:"clientOrderId"`
}

// MyOrderEvent는 하위 계정의 주문 변경 묶음이다.
type MyOrderEvent struct {
	AccountSeq *int          `json:"accountSeq"`
	Orders     []StreamOrder `json:"orders"`
}

// StreamPrivateTrade는 내 주문의 체결 한 건이다.
type StreamPrivateTrade struct {
	TradeID     int64  `json:"tradeId"`
	OrderID     int64  `json:"orderId"`
	Side        Side   `json:"side"`
	Price       string `json:"price"`
	Qty         string `json:"qty"`
	Fee         string `json:"fee"`
	FeeCurrency string `json:"feeCurrency"`
	FilledAt    int64  `json:"filledAt"`
	IsTaker     bool   `json:"isTaker"`
}

// MyTradeEvent는 하위 계정의 주문 체결 묶음이다.
type MyTradeEvent struct {
	AccountSeq *int                 `json:"accountSeq"`
	Trades     []StreamPrivateTrade `json:"trades"`
}

// StreamAsset은 private 자산 변경 후 잔고다.
type StreamAsset struct {
	Currency        string `json:"currency"`
	Balance         string `json:"balance"`
	Available       string `json:"available"`
	TradeInUse      string `json:"tradeInUse"`
	WithdrawalInUse string `json:"withdrawalInUse"`
	AveragePrice    string `json:"avgPrice"`
	UpdatedAt       int64  `json:"updatedAt"`
}

// MyAssetEvent는 하위 계정의 자산 변경 묶음이다.
type MyAssetEvent struct {
	AccountSeq *int          `json:"accountSeq"`
	Assets     []StreamAsset `json:"assets"`
}
