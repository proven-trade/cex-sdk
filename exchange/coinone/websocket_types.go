package coinone

import (
	"encoding/json"
	"fmt"
)

// StreamChannel은 코인원 WebSocket 구독 채널이다.
type StreamChannel string

const (
	StreamChannelOrderBook StreamChannel = "ORDERBOOK"
	StreamChannelTicker    StreamChannel = "TICKER"
	StreamChannelTrade     StreamChannel = "TRADE"
	StreamChannelChart     StreamChannel = "CHART"
	StreamChannelMyOrder   StreamChannel = "MYORDER"
	StreamChannelMyAsset   StreamChannel = "MYASSET"
)

// StreamFormat은 WebSocket 이벤트 필드 형식이다.
type StreamFormat string

const (
	StreamFormatDefault StreamFormat = "DEFAULT"
	StreamFormatShort   StreamFormat = "SHORT"
)

// StreamTopic은 구독할 거래쌍과 선택적인 캔들 구간이다.
type StreamTopic struct {
	QuoteCurrency  string         `json:"quote_currency"`
	TargetCurrency string         `json:"target_currency"`
	Interval       CandleInterval `json:"interval,omitempty"`
}

// StreamSubscription은 채널, topic과 응답 형식을 정의한다.
type StreamSubscription struct {
	Channel StreamChannel
	Topics  []StreamTopic
	Format  StreamFormat
}

// StreamRequest는 연결 직후 복구할 구독 목록이다.
type StreamRequest struct {
	Subscriptions []StreamSubscription
}

// StreamMessage는 데이터, 연결 상태, 구독 응답, PONG 또는 오류 한 건이다.
type StreamMessage struct {
	ResponseType string
	Channel      StreamChannel
	ErrorCode    int
	ErrorMessage string
	Data         json.RawMessage
	Short        bool
	Raw          json.RawMessage
}

// Decode는 DATA 이벤트 payload를 지정한 타입으로 변환한다.
func (message StreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("Coinone stream decode target is nil")
	}
	if message.ResponseType != "DATA" || len(message.Data) == 0 {
		return fmt.Errorf("Coinone stream message does not contain a data event")
	}
	if message.Short {
		return decodeShortStreamData(message.Channel, message.Data, target)
	}
	if err := json.Unmarshal(message.Data, target); err != nil {
		return fmt.Errorf("decode Coinone stream event: %w", err)
	}
	return nil
}

// StreamSession은 연결 완료 메시지의 세션 식별 정보다.
type StreamSession struct {
	SessionID string `json:"session_id"`
}

// StreamOrderBook은 실시간 호가 스냅샷이다.
type StreamOrderBook struct {
	QuoteCurrency  string           `json:"quote_currency"`
	TargetCurrency string           `json:"target_currency"`
	Timestamp      int64            `json:"timestamp"`
	ID             string           `json:"id"`
	Asks           []OrderBookLevel `json:"asks"`
	Bids           []OrderBookLevel `json:"bids"`
}

// StreamTicker는 실시간 현재가와 24시간·전일 통계다.
type StreamTicker struct {
	QuoteCurrency         string  `json:"quote_currency"`
	TargetCurrency        string  `json:"target_currency"`
	Timestamp             int64   `json:"timestamp"`
	QuoteVolume           Decimal `json:"quote_volume"`
	TargetVolume          Decimal `json:"target_volume"`
	High                  Decimal `json:"high"`
	Low                   Decimal `json:"low"`
	First                 Decimal `json:"first"`
	Last                  Decimal `json:"last"`
	VolumePower           Decimal `json:"volume_power"`
	AskBestPrice          Decimal `json:"ask_best_price"`
	AskBestQuantity       Decimal `json:"ask_best_qty"`
	BidBestPrice          Decimal `json:"bid_best_price"`
	BidBestQuantity       Decimal `json:"bid_best_qty"`
	ID                    string  `json:"id"`
	YesterdayHigh         Decimal `json:"yesterday_high"`
	YesterdayLow          Decimal `json:"yesterday_low"`
	YesterdayFirst        Decimal `json:"yesterday_first"`
	YesterdayLast         Decimal `json:"yesterday_last"`
	YesterdayQuoteVolume  Decimal `json:"yesterday_quote_volume"`
	YesterdayTargetVolume Decimal `json:"yesterday_target_volume"`
}

// StreamTrade는 실시간 공개 체결 한 건이다.
type StreamTrade struct {
	QuoteCurrency  string  `json:"quote_currency"`
	TargetCurrency string  `json:"target_currency"`
	ID             string  `json:"id"`
	Timestamp      int64   `json:"timestamp"`
	Price          Decimal `json:"price"`
	Quantity       Decimal `json:"qty"`
	IsSellerMaker  bool    `json:"is_seller_maker"`
}

// StreamCandle은 변경된 최신 캔들 한 건이다.
type StreamCandle struct {
	QuoteCurrency   string         `json:"quote_currency"`
	TargetCurrency  string         `json:"target_currency"`
	Interval        CandleInterval `json:"interval"`
	Timestamp       int64          `json:"timestamp"`
	ID              string         `json:"id"`
	CandleTimestamp int64          `json:"candle_timestamp"`
	High            Decimal        `json:"high"`
	Low             Decimal        `json:"low"`
	First           Decimal        `json:"first"`
	Last            Decimal        `json:"last"`
	QuoteVolume     Decimal        `json:"quote_volume"`
	TargetVolume    Decimal        `json:"target_volume"`
}

// StreamOrderSide는 private 주문 이벤트의 매수 또는 매도 방향이다.
type StreamOrderSide string

const (
	StreamOrderSideBid StreamOrderSide = "BID"
	StreamOrderSideAsk StreamOrderSide = "ASK"
)

// StreamOrderStatus는 private 주문 이벤트 처리 상태다.
type StreamOrderStatus string

const (
	StreamOrderStatusWait           StreamOrderStatus = "wait"
	StreamOrderStatusWatch          StreamOrderStatus = "watch"
	StreamOrderStatusNotTriggered   StreamOrderStatus = "not_triggered"
	StreamOrderStatusTrade          StreamOrderStatus = "trade"
	StreamOrderStatusTradeDone      StreamOrderStatus = "trade_done"
	StreamOrderStatusCancel         StreamOrderStatus = "cancel"
	StreamOrderStatusCancelPostOnly StreamOrderStatus = "cancel_post_only"
)

// MyOrderEvent는 내 주문 생성·체결·취소 이벤트다.
type MyOrderEvent struct {
	QuoteCurrency     string            `json:"quote_currency"`
	TargetCurrency    string            `json:"target_currency"`
	OrderID           string            `json:"order_id"`
	Type              OrderType         `json:"type"`
	Status            StreamOrderStatus `json:"status"`
	Side              StreamOrderSide   `json:"side"`
	OrderPrice        Decimal           `json:"order_price"`
	OrderQuantity     Decimal           `json:"order_qty"`
	OrderAmount       Decimal           `json:"order_amount"`
	TradeID           string            `json:"trade_id"`
	IsMaker           *bool             `json:"is_maker"`
	ExecutedPrice     Decimal           `json:"executed_price"`
	ExecutedQuantity  Decimal           `json:"executed_qty"`
	ExecutedFee       Decimal           `json:"executed_fee"`
	RemainingQuantity Decimal           `json:"remain_qty"`
	RemainingAmount   Decimal           `json:"remain_amount"`
	UserOrderID       string            `json:"user_order_id"`
	PreventedQuantity Decimal           `json:"prevented_qty"`
	ExecutedTimestamp *int64            `json:"executed_timestamp"`
	OrderTimestamp    *int64            `json:"order_timestamp"`
	Timestamp         int64             `json:"timestamp"`
}

// StreamAsset은 private 자산 이벤트의 통화별 변경 후 잔고다.
type StreamAsset struct {
	Currency  string  `json:"currency"`
	Available Decimal `json:"available"`
	Limit     Decimal `json:"limit"`
}

// MyAssetEvent는 내 자산 변경 이벤트다.
type MyAssetEvent struct {
	OrderID     string        `json:"order_id"`
	UserOrderID string        `json:"user_order_id"`
	TradeID     string        `json:"trade_id"`
	Assets      []StreamAsset `json:"assets"`
	Type        string        `json:"type"`
	Timestamp   int64         `json:"timestamp"`
}
