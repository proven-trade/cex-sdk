package upbit

import (
	"bytes"
	"encoding/json"
	"fmt"

	corestream "github.com/proven-trade/proven-trade-sdk/stream"
)

type streamWireMessage struct {
	Type             string       `json:"type"`
	SimpleType       string       `json:"ty"`
	Code             string       `json:"code"`
	SimpleCode       string       `json:"cd"`
	StreamType       string       `json:"stream_type"`
	SimpleStreamType string       `json:"st"`
	Timestamp        int64        `json:"timestamp"`
	SimpleTimestamp  int64        `json:"tms"`
	Status           string       `json:"status"`
	Error            *StreamError `json:"error"`
}

// DecodeStreamMessage는 DEFAULT, SIMPLE 또는 JSON_LIST 메시지 envelope를 분류한다.
func DecodeStreamMessage(message corestream.Message) (StreamMessage, error) {
	trimmed := bytes.TrimSpace(message.Data)
	if !json.Valid(trimmed) {
		return StreamMessage{}, fmt.Errorf("invalid Upbit stream JSON")
	}
	payload := trimmed
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return StreamMessage{}, fmt.Errorf("decode Upbit JSON_LIST stream: %w", err)
		}
		if len(items) != 1 {
			return StreamMessage{}, fmt.Errorf("Upbit JSON_LIST message has %d events, want 1", len(items))
		}
		payload = items[0]
	}
	var wire streamWireMessage
	if err := json.Unmarshal(payload, &wire); err != nil {
		return StreamMessage{}, fmt.Errorf("decode Upbit stream envelope: %w", err)
	}
	typeValue := wire.Type
	if typeValue == "" {
		typeValue = wire.SimpleType
	}
	code := wire.Code
	if code == "" {
		code = wire.SimpleCode
	}
	streamType := wire.StreamType
	if streamType == "" {
		streamType = wire.SimpleStreamType
	}
	timestamp := wire.Timestamp
	if timestamp == 0 {
		timestamp = wire.SimpleTimestamp
	}
	if typeValue == "" && wire.Status == "" && wire.Error == nil {
		return StreamMessage{}, fmt.Errorf("Upbit stream message has no type, status, or error")
	}
	return StreamMessage{
		Type:       typeValue,
		Code:       code,
		StreamType: streamType,
		Timestamp:  timestamp,
		Status:     wire.Status,
		Error:      cloneStreamError(wire.Error),
		Payload:    cloneBytes(payload),
		Raw:        cloneBytes(trimmed),
	}, nil
}

func cloneStreamError(source *StreamError) *StreamError {
	if source == nil {
		return nil
	}
	copyValue := *source
	return &copyValue
}
