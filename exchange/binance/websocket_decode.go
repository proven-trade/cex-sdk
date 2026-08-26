package binance

import (
	"encoding/json"
	"fmt"

	corestream "github.com/proven-trade/cex-sdk/stream"
)

type marketWireMessage struct {
	Stream  string          `json:"stream"`
	Data    json.RawMessage `json:"data"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Code    *int            `json:"code"`
	Message string          `json:"msg"`
}

type userDataWireMessage struct {
	ID             json.RawMessage `json:"id"`
	Status         int             `json:"status"`
	Result         json.RawMessage `json:"result"`
	Error          *WebSocketError `json:"error"`
	SubscriptionID *int64          `json:"subscriptionId"`
	Event          json.RawMessage `json:"event"`
}

type eventMetadata struct {
	Type string `json:"e"`
	Time int64  `json:"E"`
}

// DecodeMarketStreamMessage는 Binance public stream JSON을 이벤트 또는 제어 응답으로 분류한다.
func DecodeMarketStreamMessage(message corestream.Message) (MarketStreamMessage, error) {
	if !json.Valid(message.Data) {
		return MarketStreamMessage{}, fmt.Errorf("invalid Binance market stream JSON")
	}
	raw := cloneBytes(message.Data)
	var wire marketWireMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		return MarketStreamMessage{}, fmt.Errorf("decode Binance market stream envelope: %w", err)
	}
	if len(wire.ID) > 0 || wire.Code != nil {
		response := &WebSocketResponse{
			ID:     cloneBytes(wire.ID),
			Result: cloneBytes(wire.Result),
			Raw:    cloneBytes(raw),
		}
		if wire.Code != nil {
			response.Error = &WebSocketError{Code: *wire.Code, Message: wire.Message}
		}
		return MarketStreamMessage{Response: response, Raw: raw}, nil
	}

	payload := raw
	if wire.Stream != "" {
		if len(wire.Data) == 0 || string(wire.Data) == "null" {
			return MarketStreamMessage{}, fmt.Errorf("Binance combined stream event is missing data")
		}
		payload = cloneBytes(wire.Data)
	}
	var metadata eventMetadata
	if err := json.Unmarshal(payload, &metadata); err != nil {
		return MarketStreamMessage{}, fmt.Errorf("decode Binance market stream metadata: %w", err)
	}
	return MarketStreamMessage{
		Stream:    wire.Stream,
		EventType: metadata.Type,
		EventTime: metadata.Time,
		Payload:   payload,
		Raw:       raw,
	}, nil
}

// DecodeUserDataStreamMessage는 Binance WebSocket API JSON을 계정 이벤트 또는 응답으로 분류한다.
func DecodeUserDataStreamMessage(message corestream.Message) (UserDataStreamMessage, error) {
	if !json.Valid(message.Data) {
		return UserDataStreamMessage{}, fmt.Errorf("invalid Binance user data stream JSON")
	}
	raw := cloneBytes(message.Data)
	var wire userDataWireMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		return UserDataStreamMessage{}, fmt.Errorf("decode Binance user data stream envelope: %w", err)
	}
	if len(wire.ID) > 0 {
		response := &WebSocketResponse{
			ID:     cloneBytes(wire.ID),
			Status: wire.Status,
			Result: cloneBytes(wire.Result),
			Error:  cloneWebSocketError(wire.Error),
			Raw:    cloneBytes(raw),
		}
		return UserDataStreamMessage{Response: response, Raw: raw}, nil
	}
	if len(wire.Event) == 0 || string(wire.Event) == "null" {
		return UserDataStreamMessage{}, fmt.Errorf("Binance user data stream message is missing event")
	}
	var metadata eventMetadata
	if err := json.Unmarshal(wire.Event, &metadata); err != nil {
		return UserDataStreamMessage{}, fmt.Errorf("decode Binance user data stream metadata: %w", err)
	}
	subscriptionID := int64(0)
	if wire.SubscriptionID != nil {
		subscriptionID = *wire.SubscriptionID
	}
	return UserDataStreamMessage{
		SubscriptionID: subscriptionID,
		EventType:      metadata.Type,
		EventTime:      metadata.Time,
		Payload:        cloneBytes(wire.Event),
		Raw:            raw,
	}, nil
}

func cloneWebSocketError(source *WebSocketError) *WebSocketError {
	if source == nil {
		return nil
	}
	copyValue := *source
	copyValue.Data = cloneBytes(source.Data)
	return &copyValue
}
