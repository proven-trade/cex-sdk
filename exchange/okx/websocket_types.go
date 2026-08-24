package okx

import (
	"encoding/json"
	"fmt"
)

// StreamEndpoint는 public market과 business WebSocket endpoint를 구분한다.
type StreamEndpoint string

const (
	StreamEndpointPublic   StreamEndpoint = "public"
	StreamEndpointBusiness StreamEndpoint = "business"
)

// StreamArgument는 OKX WebSocket 구독 채널 식별자다.
type StreamArgument struct {
	Channel          string         `json:"channel"`
	InstrumentType   InstrumentType `json:"instType,omitempty"`
	InstrumentFamily string         `json:"instFamily,omitempty"`
	InstrumentID     string         `json:"instId,omitempty"`
}

// PublicStreamRequest는 public 또는 business 연결의 구독 목록이다.
type PublicStreamRequest struct {
	Endpoint  StreamEndpoint
	Arguments []StreamArgument
}

// PrivateStreamRequest는 private 연결의 구독 목록이다.
type PrivateStreamRequest struct {
	Arguments []StreamArgument
}

// StreamMessage는 OKX 제어 응답, heartbeat 또는 channel data 한 건이다.
type StreamMessage struct {
	RequestID    string
	Event        string
	Code         string
	Message      string
	ConnectionID string
	Argument     StreamArgument
	Action       string
	Data         json.RawMessage
	Pong         bool
	Raw          json.RawMessage
}

// DecodeData는 channel data를 지정 타입으로 변환한다.
func (message StreamMessage) DecodeData(target any) error {
	if target == nil {
		return fmt.Errorf("OKX stream decode target is nil")
	}
	if message.Pong || len(message.Data) == 0 {
		return fmt.Errorf("OKX stream message does not contain channel data")
	}
	if err := json.Unmarshal(message.Data, target); err != nil {
		return fmt.Errorf("decode OKX stream data: %w", err)
	}
	return nil
}

// StreamTrade는 public trades channel의 체결 한 건이다.
type StreamTrade struct {
	InstrumentID string `json:"instId"`
	TradeID      string `json:"tradeId"`
	Price        string `json:"px"`
	Quantity     string `json:"sz"`
	Side         Side   `json:"side"`
	Timestamp    string `json:"ts"`
	Count        string `json:"count"`
	Source       string `json:"source"`
	SequenceID   int64  `json:"seqId"`
}

// StreamBalance은 balance_and_position channel의 통화별 잔고 변경이다.
type StreamBalance struct {
	Currency    string `json:"ccy"`
	CashBalance string `json:"cashBal"`
	UpdateTime  string `json:"uTime"`
}

// StreamPosition은 balance_and_position channel의 포지션 변경이다.
type StreamPosition struct {
	PositionID     string         `json:"posId"`
	TradeID        string         `json:"tradeId"`
	InstrumentID   string         `json:"instId"`
	InstrumentType InstrumentType `json:"instType"`
	MarginMode     TradeMode      `json:"mgnMode"`
	PositionSide   PositionSide   `json:"posSide"`
	Position       string         `json:"pos"`
	PositionCcy    string         `json:"posCcy"`
	AveragePrice   string         `json:"avgPx"`
	UpdateTime     string         `json:"uTime"`
}

// StreamBalanceAndPosition은 잔고와 포지션을 함께 전달하는 private 데이터다.
type StreamBalanceAndPosition struct {
	PositionTime string           `json:"pTime"`
	EventType    string           `json:"eventType"`
	Balances     []StreamBalance  `json:"balData"`
	Positions    []StreamPosition `json:"posData"`
}

// StreamLoginError는 private WebSocket 로그인이 명시적으로 거절된 오류다.
type StreamLoginError struct {
	Code    string
	Message string
}

// Error는 로그인 거절 코드와 메시지를 반환한다.
func (streamError *StreamLoginError) Error() string {
	return fmt.Sprintf("OKX stream login failed with code %s: %s", streamError.Code, streamError.Message)
}
