package bitget

import (
	"encoding/json"
	"fmt"
)

// StreamArgument는 Bitget v3 WebSocket 구독 채널 식별자다.
type StreamArgument struct {
	InstrumentType string `json:"instType"`
	Topic          string `json:"topic"`
	Symbol         string `json:"symbol,omitempty"`
	Interval       string `json:"interval,omitempty"`
}

// StreamRequest는 한 연결에서 구독할 채널 목록이다.
type StreamRequest struct {
	Arguments []StreamArgument
}

// StreamMessage는 Bitget 제어 응답, heartbeat 또는 channel data 한 건이다.
type StreamMessage struct {
	Event        string
	Code         string
	Message      string
	ConnectionID string
	Argument     StreamArgument
	Action       string
	Timestamp    int64
	Data         json.RawMessage
	Pong         bool
	Raw          json.RawMessage
}

// DecodeData는 channel data 배열을 지정 타입으로 변환한다.
func (message StreamMessage) DecodeData(target any) error {
	if target == nil {
		return fmt.Errorf("Bitget stream decode target is nil")
	}
	if message.Pong || len(message.Data) == 0 {
		return fmt.Errorf("Bitget stream message does not contain channel data")
	}
	if err := json.Unmarshal(message.Data, target); err != nil {
		return fmt.Errorf("decode Bitget stream data: %w", err)
	}
	return nil
}

// StreamTicker는 Spot 또는 Futures ticker channel 데이터다.
type StreamTicker struct {
	LastPrice          string `json:"lastPrice"`
	OpenPrice24h       string `json:"openPrice24h"`
	HighPrice24h       string `json:"highPrice24h"`
	LowPrice24h        string `json:"lowPrice24h"`
	BestAskPrice       string `json:"ask1Price"`
	BestAskQuantity    string `json:"ask1Size"`
	BestBidPrice       string `json:"bid1Price"`
	BestBidQuantity    string `json:"bid1Size"`
	PriceChange24hRate string `json:"price24hPcnt"`
	Volume24h          string `json:"volume24h"`
	Turnover24h        string `json:"turnover24h"`
	IndexPrice         string `json:"indexPrice"`
	MarkPrice          string `json:"markPrice"`
	FundingRate        string `json:"fundingRate"`
	OpenInterest       string `json:"openInterest"`
	Timestamp          string `json:"ts"`
}

// StreamOrderBook은 order book snapshot 또는 update 데이터다.
type StreamOrderBook struct {
	Asks      []BookLevel `json:"asks"`
	Bids      []BookLevel `json:"bids"`
	Checksum  int64       `json:"checksum"`
	Timestamp string      `json:"ts"`
}

// StreamPublicTrade는 public trade channel 체결 데이터다.
type StreamPublicTrade struct {
	ExecutionID string `json:"execId"`
	Price       string `json:"price"`
	Quantity    string `json:"size"`
	Side        Side   `json:"side"`
	Timestamp   string `json:"ts"`
	RPI         string `json:"isRPI"`
}

// StreamAccount는 private account channel 계정 데이터다.
type StreamAccount struct {
	AccountEquity     string  `json:"accountEquity"`
	USDTEquity        string  `json:"usdtEquity"`
	UnrealisedPnL     string  `json:"unrealisedPnl"`
	EffectiveEquity   string  `json:"effEquity"`
	MaintenanceMargin string  `json:"mmr"`
	InitialMargin     string  `json:"imr"`
	MarginRatio       string  `json:"mgnRatio"`
	Assets            []Asset `json:"assets"`
}

// StreamFill은 private fill channel 체결 데이터다.
type StreamFill struct {
	ExecutionID   string       `json:"execId"`
	OrderID       string       `json:"orderId"`
	ClientOrderID string       `json:"clientOid"`
	Category      Category     `json:"category"`
	Symbol        string       `json:"symbol"`
	Side          Side         `json:"side"`
	PositionSide  PositionSide `json:"posSide"`
	Price         string       `json:"price"`
	Quantity      string       `json:"size"`
	Fee           string       `json:"fee"`
	FeeCoin       string       `json:"feeCoin"`
	ExecutionType string       `json:"execType"`
	Timestamp     string       `json:"ts"`
}
