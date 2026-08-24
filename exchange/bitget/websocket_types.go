package bitget

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
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
	Asks             []BookLevel `json:"a"`
	Bids             []BookLevel `json:"b"`
	PreviousSequence int64       `json:"pseq"`
	Sequence         int64       `json:"seq"`
	MaxDepth         string      `json:"maxDepth"`
	Timestamp        string      `json:"ts"`
	// Checksum은 제거 전 v3 payload decode 호환용이며 현재 정합성 검증에는 사용하지 않는다.
	Checksum int64 `json:"checksum,omitempty"`
}

// UnmarshalJSON은 현재 v3의 a/b·seq/pseq와 이전 depth 필드명을 함께 해석한다.
func (book *StreamOrderBook) UnmarshalJSON(data []byte) error {
	var wire struct {
		Asks             []BookLevel     `json:"a"`
		Bids             []BookLevel     `json:"b"`
		LegacyAsks       []BookLevel     `json:"asks"`
		LegacyBids       []BookLevel     `json:"bids"`
		PreviousSequence json.RawMessage `json:"pseq"`
		Sequence         json.RawMessage `json:"seq"`
		MaxDepth         json.RawMessage `json:"maxDepth"`
		Timestamp        json.RawMessage `json:"ts"`
		Checksum         json.RawMessage `json:"checksum"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("decode Bitget stream order book: %w", err)
	}
	if wire.Asks == nil {
		wire.Asks = wire.LegacyAsks
	}
	if wire.Bids == nil {
		wire.Bids = wire.LegacyBids
	}
	previousSequence, err := optionalStreamInteger("pseq", wire.PreviousSequence)
	if err != nil {
		return err
	}
	sequence, err := optionalStreamInteger("seq", wire.Sequence)
	if err != nil {
		return err
	}
	checksum, err := optionalStreamInteger("checksum", wire.Checksum)
	if err != nil {
		return err
	}
	maxDepth, err := optionalStreamText("maxDepth", wire.MaxDepth)
	if err != nil {
		return err
	}
	timestamp, err := optionalStreamText("ts", wire.Timestamp)
	if err != nil {
		return err
	}
	book.Asks = wire.Asks
	book.Bids = wire.Bids
	book.PreviousSequence = previousSequence
	book.Sequence = sequence
	book.MaxDepth = maxDepth
	book.Timestamp = timestamp
	book.Checksum = checksum
	return nil
}

func optionalStreamInteger(name string, raw json.RawMessage) (int64, error) {
	text, err := optionalStreamText(name, raw)
	if err != nil || text == "" {
		return 0, err
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("decode Bitget stream %s: %w", name, err)
	}
	return value, nil
}

func optionalStreamText(name string, raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	value, err := decimalText(trimmed)
	if err != nil {
		return "", fmt.Errorf("decode Bitget stream %s: %w", name, err)
	}
	return value, nil
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
