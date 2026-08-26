package bitget

import (
	"bytes"
	"encoding/json"
	"fmt"

	corestream "github.com/proven-trade/cex-sdk/stream"
)

type streamWireMessage struct {
	Event        string          `json:"event"`
	Code         string          `json:"code"`
	Message      string          `json:"msg"`
	ConnectionID string          `json:"connId"`
	Argument     StreamArgument  `json:"arg"`
	Action       string          `json:"action"`
	Timestamp    int64           `json:"ts"`
	Data         json.RawMessage `json:"data"`
}

// DecodeStreamMessage는 Bitget JSON 또는 application pong을 공통 메시지로 변환한다.
func DecodeStreamMessage(message corestream.Message) (StreamMessage, error) {
	trimmed := bytes.TrimSpace(message.Data)
	if bytes.Equal(trimmed, []byte("pong")) || bytes.Equal(trimmed, []byte(`"pong"`)) {
		return StreamMessage{Pong: true, Raw: cloneBytes(trimmed)}, nil
	}
	if !json.Valid(trimmed) {
		return StreamMessage{}, fmt.Errorf("invalid Bitget stream JSON")
	}
	var wire streamWireMessage
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return StreamMessage{}, fmt.Errorf("decode Bitget stream envelope: %w", err)
	}
	if wire.Event == "" && wire.Argument.Topic == "" && len(wire.Data) == 0 {
		return StreamMessage{}, fmt.Errorf("Bitget stream message has no event or channel data")
	}
	return StreamMessage{
		Event:        wire.Event,
		Code:         wire.Code,
		Message:      wire.Message,
		ConnectionID: wire.ConnectionID,
		Argument:     wire.Argument,
		Action:       wire.Action,
		Timestamp:    wire.Timestamp,
		Data:         cloneBytes(wire.Data),
		Raw:          cloneBytes(trimmed),
	}, nil
}

// LoginError는 Bitget private WebSocket 로그인 거절을 나타낸다.
type LoginError struct {
	Code    string
	Message string
}

// Error는 로그인 실패를 민감 정보 없이 표현한다.
func (loginError *LoginError) Error() string {
	if loginError == nil {
		return "<nil>"
	}
	if loginError.Code != "" {
		return fmt.Sprintf("Bitget WebSocket login failed with code %s: %s", loginError.Code, loginError.Message)
	}
	return "Bitget WebSocket login failed"
}
