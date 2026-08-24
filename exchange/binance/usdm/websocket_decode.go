package usdm

import (
	"bytes"
	"encoding/json"
	"fmt"

	corestream "github.com/proven-trade/proven-trade-sdk/stream"
)

type marketWireMessage struct {
	Stream  string          `json:"stream"`
	Data    json.RawMessage `json:"data"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Code    *int            `json:"code"`
	Message string          `json:"msg"`
}

type streamEventMetadata struct {
	Type            string `json:"e"`
	EventTime       int64  `json:"E"`
	TransactionTime int64  `json:"T"`
}

// DecodeMarketStreamMessage는 Binance USDⓈ-M Futures JSON을 이벤트 또는 제어 응답으로 분류한다.
func DecodeMarketStreamMessage(message corestream.Message) (MarketStreamMessage, error) {
	if message.Type != corestream.MessageText {
		return MarketStreamMessage{}, fmt.Errorf("Binance USD-M JSON stream only accepts text frames")
	}
	trimmed := bytes.TrimSpace(message.Data)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return MarketStreamMessage{}, fmt.Errorf("invalid Binance USD-M market stream JSON object")
	}
	raw := cloneBytes(trimmed)
	var wire marketWireMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		return MarketStreamMessage{}, fmt.Errorf("decode Binance USD-M market stream envelope: %w", err)
	}
	if len(wire.ID) > 0 || wire.Code != nil {
		response := &WebSocketResponse{
			ID: cloneBytes(wire.ID), Result: cloneBytes(wire.Result), Raw: cloneBytes(raw),
		}
		if wire.Code != nil {
			response.Error = &WebSocketError{Code: *wire.Code, Message: wire.Message}
		}
		return MarketStreamMessage{Response: response, Raw: raw}, nil
	}
	payload := raw
	if wire.Stream != "" {
		if len(wire.Data) == 0 || bytes.Equal(bytes.TrimSpace(wire.Data), []byte("null")) {
			return MarketStreamMessage{}, fmt.Errorf("Binance USD-M combined stream event is missing data")
		}
		payload = cloneBytes(wire.Data)
	}
	var metadata streamEventMetadata
	if err := json.Unmarshal(payload, &metadata); err != nil {
		return MarketStreamMessage{}, fmt.Errorf("decode Binance USD-M market stream metadata: %w", err)
	}
	if metadata.Type == "" {
		return MarketStreamMessage{}, fmt.Errorf("Binance USD-M market stream event type is empty")
	}
	return MarketStreamMessage{
		Stream: wire.Stream, EventType: metadata.Type, EventTime: metadata.EventTime,
		Payload: payload, Raw: raw,
	}, nil
}

// DecodeUserDataStreamMessage는 Binance USDⓈ-M Futures private JSON 이벤트를 분류한다.
func DecodeUserDataStreamMessage(message corestream.Message) (UserDataStreamMessage, error) {
	if message.Type != corestream.MessageText {
		return UserDataStreamMessage{}, fmt.Errorf("Binance USD-M JSON stream only accepts text frames")
	}
	trimmed := bytes.TrimSpace(message.Data)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return UserDataStreamMessage{}, fmt.Errorf("invalid Binance USD-M user data stream JSON object")
	}
	var metadata streamEventMetadata
	if err := json.Unmarshal(trimmed, &metadata); err != nil {
		return UserDataStreamMessage{}, fmt.Errorf("decode Binance USD-M user data metadata: %w", err)
	}
	if metadata.Type == "" {
		return UserDataStreamMessage{}, fmt.Errorf("Binance USD-M user data event type is empty")
	}
	raw := cloneBytes(trimmed)
	return UserDataStreamMessage{
		EventType: metadata.Type, EventTime: metadata.EventTime,
		TransactionTime: metadata.TransactionTime, Payload: cloneBytes(trimmed), Raw: raw,
	}, nil
}
