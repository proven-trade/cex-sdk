package htx

import (
	"encoding/json"
	"fmt"
)

// StreamChannel은 HTX 공개 시세 또는 private 계정 WebSocket 채널이다.
type StreamChannel string

const (
	StreamChannelTicker   StreamChannel = "ticker"
	StreamChannelDepth    StreamChannel = "depth"
	StreamChannelBBO      StreamChannel = "bbo"
	StreamChannelTrades   StreamChannel = "trades"
	StreamChannelCandles  StreamChannel = "candles"
	StreamChannelOrders   StreamChannel = "orders"
	StreamChannelClearing StreamChannel = "trade_clearing"
	StreamChannelAccounts StreamChannel = "accounts"
)

// StreamMode는 private 체결·취소 또는 계정 잔고 통지 방식이다.
type StreamMode int

const (
	StreamModeTradesOnly             StreamMode = 0
	StreamModeTradesAndCancellations StreamMode = 1
	StreamModeBalanceOnly            StreamMode = 0
	StreamModeBalanceOrAvailable     StreamMode = 1
	StreamModeBalanceAndAvailable    StreamMode = 2
)

// StreamSubscription은 시세 채널과 거래쌍 및 채널별 선택 값을 정의한다.
type StreamSubscription struct {
	Channel        StreamChannel
	Symbol         string
	DepthType      DepthType
	CandleInterval CandleInterval
	Mode           StreamMode
}

// StreamRequest는 연결 직후 복구할 공개 또는 private 구독 목록이다.
type StreamRequest struct {
	Subscriptions []StreamSubscription
}

// StreamError는 구독 또는 해지 요청을 거절한 서버 오류다.
type StreamError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// StreamMessage는 시세·계정 데이터, 인증·구독 응답 또는 heartbeat 한 건이다.
type StreamMessage struct {
	ID           string
	Status       string
	Topic        string
	Channel      StreamChannel
	Symbol       string
	Timestamp    int64
	Ping         *int64
	Action       string
	Code         int
	Message      string
	Private      bool
	Mode         StreamMode
	Subscribed   string
	Unsubscribed string
	Error        *StreamError
	Tick         json.RawMessage
	Data         json.RawMessage
	Raw          json.RawMessage
}

// Decode는 공개 tick 또는 private data 이벤트를 지정한 타입으로 변환한다.
func (message StreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("HTX stream decode target is nil")
	}
	if message.Error != nil || message.Topic == "" {
		return fmt.Errorf("HTX stream message does not contain a data event")
	}
	payload := message.Tick
	if message.Private {
		if message.Action != "push" && message.Action != "" {
			return fmt.Errorf("HTX stream message does not contain a data event")
		}
		payload = message.Data
	}
	if len(payload) == 0 {
		return fmt.Errorf("HTX stream message does not contain a data event")
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("decode HTX stream event: %w", err)
	}
	return nil
}

// StreamTicker는 실시간 현재가·최우선 호가와 24시간 통계다.
type StreamTicker struct {
	Open      Decimal `json:"open"`
	High      Decimal `json:"high"`
	Low       Decimal `json:"low"`
	Close     Decimal `json:"close"`
	Amount    Decimal `json:"amount"`
	Volume    Decimal `json:"vol"`
	Count     int64   `json:"count"`
	BidPrice  Decimal `json:"bid"`
	BidSize   Decimal `json:"bidSize"`
	AskPrice  Decimal `json:"ask"`
	AskSize   Decimal `json:"askSize"`
	LastPrice Decimal `json:"lastPrice"`
	LastSize  Decimal `json:"lastSize"`
}

// StreamDepth는 집계 단계별 호가 snapshot이다.
type StreamDepth struct {
	Timestamp int64       `json:"ts"`
	Version   Scalar      `json:"version"`
	Bids      []BookLevel `json:"bids"`
	Asks      []BookLevel `json:"asks"`
}

// StreamBBO는 실시간 최우선 매수·매도 호가다.
type StreamBBO struct {
	SequenceID Scalar  `json:"seqId"`
	AskPrice   Decimal `json:"ask"`
	AskSize    Decimal `json:"askSize"`
	BidPrice   Decimal `json:"bid"`
	BidSize    Decimal `json:"bidSize"`
	QuoteTime  int64   `json:"quoteTime"`
	Symbol     string  `json:"symbol"`
}

// StreamTradeBatch는 같은 tick에 포함된 공개 체결 묶음이다.
type StreamTradeBatch struct {
	ID        Scalar        `json:"id"`
	Timestamp int64         `json:"ts"`
	Trades    []StreamTrade `json:"data"`
}

// StreamTrade는 실시간 공개 체결 한 건이다.
type StreamTrade struct {
	ID        Scalar         `json:"id"`
	TradeID   Scalar         `json:"tradeId"`
	Amount    Decimal        `json:"amount"`
	Price     Decimal        `json:"price"`
	Timestamp int64          `json:"ts"`
	Direction TradeDirection `json:"direction"`
}

// StreamCandle은 변경된 최신 Spot OHLCV 캔들이다.
type StreamCandle struct {
	OpenTime    int64   `json:"id"`
	Open        Decimal `json:"open"`
	Close       Decimal `json:"close"`
	Low         Decimal `json:"low"`
	High        Decimal `json:"high"`
	BaseVolume  Decimal `json:"amount"`
	QuoteVolume Decimal `json:"vol"`
	TradeCount  int64   `json:"count"`
}

// StreamOrderEvent는 주문 생성·체결·취소와 조건부 주문 상태 변경이다.
type StreamOrderEvent struct {
	EventType        string    `json:"eventType"`
	Symbol           string    `json:"symbol"`
	AccountID        Scalar    `json:"accountId"`
	OrderID          Scalar    `json:"orderId"`
	ClientOrderID    string    `json:"clientOrderId"`
	OrderSource      string    `json:"orderSource"`
	OrderPrice       *Decimal  `json:"orderPrice"`
	OrderSize        *Decimal  `json:"orderSize"`
	OrderValue       *Decimal  `json:"orderValue"`
	Type             OrderType `json:"type"`
	OrderSide        Side      `json:"orderSide"`
	OrderStatus      string    `json:"orderStatus"`
	OrderCreateTime  int64     `json:"orderCreateTime"`
	TradePrice       *Decimal  `json:"tradePrice"`
	TradeVolume      *Decimal  `json:"tradeVolume"`
	TradeID          Scalar    `json:"tradeId"`
	TradeTime        int64     `json:"tradeTime"`
	Aggressor        *bool     `json:"aggressor"`
	RemainingAmount  *Decimal  `json:"remainAmt"`
	ExecutedAmount   *Decimal  `json:"execAmt"`
	LastActivityTime int64     `json:"lastActTime"`
	ErrorCode        int       `json:"errCode"`
	ErrorMessage     string    `json:"errMessage"`
}

// StreamClearingEvent는 주문 체결 또는 청산 후 취소 세부 정보다.
type StreamClearingEvent struct {
	EventType       string    `json:"eventType"`
	Symbol          string    `json:"symbol"`
	AccountID       Scalar    `json:"accountId"`
	OrderID         Scalar    `json:"orderId"`
	ClientOrderID   string    `json:"clientOrderId"`
	OrderSide       Side      `json:"orderSide"`
	OrderType       OrderType `json:"orderType"`
	OrderStatus     string    `json:"orderStatus"`
	Source          string    `json:"source"`
	OrderPrice      *Decimal  `json:"orderPrice"`
	OrderSize       *Decimal  `json:"orderSize"`
	OrderValue      *Decimal  `json:"orderValue"`
	OrderCreateTime int64     `json:"orderCreateTime"`
	TradePrice      *Decimal  `json:"tradePrice"`
	TradeVolume     *Decimal  `json:"tradeVolume"`
	TradeID         Scalar    `json:"tradeId"`
	TradeTime       int64     `json:"tradeTime"`
	Aggressor       *bool     `json:"aggressor"`
	TransactionFee  *Decimal  `json:"transactFee"`
	FeeDeduction    *Decimal  `json:"feeDeduct"`
	FeeDeductType   string    `json:"feeDeductType"`
	FeeCurrency     string    `json:"feeCurrency"`
	StopPrice       *Decimal  `json:"stopPrice"`
	Operator        string    `json:"operator"`
	RemainingAmount *Decimal  `json:"remainAmt"`
}

// StreamAccountEvent는 통화별 총잔고 또는 사용 가능 잔고 변경이다.
type StreamAccountEvent struct {
	Currency    string   `json:"currency"`
	AccountID   Scalar   `json:"accountId"`
	Balance     *Decimal `json:"balance"`
	Available   *Decimal `json:"available"`
	ChangeType  string   `json:"changeType"`
	AccountType string   `json:"accountType"`
	Sequence    Scalar   `json:"seqNum"`
	ChangeTime  int64    `json:"changeTime"`
}
