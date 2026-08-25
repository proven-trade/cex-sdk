package cryptocom

import (
	"encoding/json"
	"fmt"
)

// StreamChannel은 Crypto.com 공개 시세와 private 사용자 WebSocket 채널이다.
type StreamChannel string

const (
	StreamChannelTicker       StreamChannel = "ticker"
	StreamChannelTrades       StreamChannel = "trade"
	StreamChannelCandles      StreamChannel = "candlestick"
	StreamChannelBook         StreamChannel = "book"
	StreamChannelUserOrders   StreamChannel = "user.order"
	StreamChannelUserTrades   StreamChannel = "user.trade"
	StreamChannelUserBalances StreamChannel = "user.balance"
)

// StreamBookDepth는 명시적으로 지원하는 Crypto.com 호가 단계 수다.
type StreamBookDepth int

const (
	StreamBookDepth10 StreamBookDepth = 10
	StreamBookDepth50 StreamBookDepth = 50
)

// StreamBookSubscriptionType은 호가 전체 이미지 또는 전체 이미지와 증분 갱신 방식이다.
type StreamBookSubscriptionType string

const (
	StreamBookSnapshot          StreamBookSubscriptionType = "SNAPSHOT"
	StreamBookSnapshotAndUpdate StreamBookSubscriptionType = "SNAPSHOT_AND_UPDATE"
)

// StreamBookUpdateFrequency는 호가 메시지 갱신 간격의 밀리초 문자열이다.
type StreamBookUpdateFrequency string

const (
	StreamBookUpdate10Milliseconds  StreamBookUpdateFrequency = "10"
	StreamBookUpdate100Milliseconds StreamBookUpdateFrequency = "100"
	StreamBookUpdate500Milliseconds StreamBookUpdateFrequency = "500"
)

// StreamSubscription은 공개 시세 또는 private 사용자 채널과 거래쌍 및 채널별 설정을 정의한다.
type StreamSubscription struct {
	Channel              StreamChannel
	InstrumentName       string
	CandleTimeframe      CandleTimeframe
	BookDepth            StreamBookDepth
	BookSubscriptionType StreamBookSubscriptionType
	BookUpdateFrequency  StreamBookUpdateFrequency
}

// StreamRequest는 연결 직후와 재연결 때 복구할 구독 목록이다.
type StreamRequest struct {
	Subscriptions []StreamSubscription
}

// StreamError는 WebSocket 명령을 거절한 Crypto.com 오류다.
type StreamError struct {
	Code     string
	Message  string
	Original string
}

// StreamMessage는 시세·사용자 데이터, 명령 응답 또는 heartbeat 한 건이다.
type StreamMessage struct {
	ID             string
	Method         string
	Code           string
	InstrumentName string
	Subscription   string
	Channel        StreamChannel
	Depth          int
	Private        bool
	Heartbeat      bool
	Error          *StreamError
	Data           json.RawMessage
	Raw            json.RawMessage
}

// Decode는 공개 시세 또는 private 사용자 데이터 배열을 지정한 타입으로 변환한다.
func (message StreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("Crypto.com stream decode target is nil")
	}
	if message.Error != nil || message.Heartbeat || message.Subscription == "" || len(message.Data) == 0 {
		return fmt.Errorf("Crypto.com stream message does not contain a data event")
	}
	if err := json.Unmarshal(message.Data, target); err != nil {
		return fmt.Errorf("decode Crypto.com stream event: %w", err)
	}
	return nil
}

// StreamTicker는 실시간 현재가·최우선 호가와 24시간 통계다.
type StreamTicker struct {
	InstrumentName string  `json:"i"`
	High           Decimal `json:"h"`
	Low            Decimal `json:"l"`
	LatestPrice    Decimal `json:"a"`
	BaseVolume     Decimal `json:"v"`
	VolumeValueUSD Decimal `json:"vv"`
	OpenInterest   Decimal `json:"oi"`
	PriceChange    Decimal `json:"c"`
	BestBid        Decimal `json:"b"`
	BestAsk        Decimal `json:"k"`
	BestBidSize    Decimal `json:"bs"`
	BestAskSize    Decimal `json:"ks"`
	Timestamp      Scalar  `json:"t"`
}

// StreamTrade는 실시간 공개 체결 한 건이다.
type StreamTrade struct {
	TradeID        Scalar    `json:"d"`
	MatchID        Scalar    `json:"m"`
	Timestamp      Scalar    `json:"t"`
	Price          Decimal   `json:"p"`
	Quantity       Decimal   `json:"q"`
	Side           TradeSide `json:"s"`
	InstrumentName string    `json:"i"`
}

// StreamCandle은 변경된 최신 Spot OHLCV 캔들이다.
type StreamCandle struct {
	Open      Decimal `json:"o"`
	High      Decimal `json:"h"`
	Low       Decimal `json:"l"`
	Close     Decimal `json:"c"`
	Volume    Decimal `json:"v"`
	Timestamp Scalar  `json:"t"`
}

// StreamBookEvent는 호가 전체 이미지 또는 이전 sequence와 연결되는 증분 갱신이다.
type StreamBookEvent struct {
	Bids             []BookLevel      `json:"bids"`
	Asks             []BookLevel      `json:"asks"`
	Timestamp        Scalar           `json:"t"`
	TransactionTime  Scalar           `json:"tt"`
	Sequence         Scalar           `json:"u"`
	PreviousSequence Scalar           `json:"pu"`
	Update           *StreamBookDelta `json:"update"`
}

// StreamBookDelta는 가격별 절대 수량 기반 호가 변경 목록이다.
type StreamBookDelta struct {
	Bids []BookLevel `json:"bids"`
	Asks []BookLevel `json:"asks"`
}
