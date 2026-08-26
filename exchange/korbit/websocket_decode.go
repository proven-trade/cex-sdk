package korbit

import (
	"bytes"
	"encoding/json"
	"fmt"

	corestream "github.com/proven-trade/cex-sdk/stream"
)

type streamWireMessage struct {
	RequestID   *int64          `json:"requestId"`
	Status      string          `json:"status"`
	Code        string          `json:"code"`
	Message     string          `json:"message"`
	Type        StreamChannel   `json:"type"`
	ChannelType StreamChannel   `json:"channelType"`
	Timestamp   int64           `json:"timestamp"`
	Symbol      string          `json:"symbol"`
	Snapshot    *bool           `json:"snapshot"`
	Data        json.RawMessage `json:"data"`
	Order       json.RawMessage `json:"order"`
	Trade       json.RawMessage `json:"trade"`
	Asset       json.RawMessage `json:"asset"`
}

// DecodeStreamMessage는 코빗 WebSocket 제어 응답과 데이터 이벤트를 분류한다.
func DecodeStreamMessage(message corestream.Message) (StreamMessage, error) {
	trimmed := bytes.TrimSpace(message.Data)
	if len(trimmed) == 0 || trimmed[0] == '[' || !json.Valid(trimmed) {
		return StreamMessage{}, fmt.Errorf("invalid Korbit stream JSON object")
	}
	var wire streamWireMessage
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return StreamMessage{}, fmt.Errorf("decode Korbit stream envelope: %w", err)
	}
	result := StreamMessage{
		RequestID: cloneInt64Pointer(wire.RequestID), Status: wire.Status,
		Code: wire.Code, ErrorMessage: wire.Message, Timestamp: wire.Timestamp,
		Symbol: wire.Symbol, Snapshot: cloneBoolPointer(wire.Snapshot), Raw: cloneBytes(trimmed),
	}
	if wire.Status != "" {
		if wire.Status != "success" && wire.Status != "fail" && wire.Status != "error" {
			return StreamMessage{}, fmt.Errorf("unsupported Korbit stream status %q", wire.Status)
		}
		return result, nil
	}
	result.Channel = wire.Type
	result.Data = cloneBytes(wire.Data)
	if wire.ChannelType != "" {
		result.Channel = wire.ChannelType
		switch wire.ChannelType {
		case StreamChannelMyOrder:
			result.Data = cloneBytes(wire.Order)
		case StreamChannelMyTrade:
			result.Data = cloneBytes(wire.Trade)
		case StreamChannelMyAsset:
			result.Data = cloneBytes(wire.Asset)
		default:
			return StreamMessage{}, fmt.Errorf("unsupported Korbit private stream channel %q", wire.ChannelType)
		}
	}
	if !result.Channel.valid() {
		return StreamMessage{}, fmt.Errorf("unsupported Korbit stream channel %q", result.Channel)
	}
	if len(result.Data) == 0 || bytes.Equal(bytes.TrimSpace(result.Data), []byte("null")) {
		return StreamMessage{}, fmt.Errorf("Korbit stream event has no data")
	}
	return result, nil
}

func cloneInt64Pointer(source *int64) *int64 {
	if source == nil {
		return nil
	}
	value := *source
	return &value
}

func cloneBoolPointer(source *bool) *bool {
	if source == nil {
		return nil
	}
	value := *source
	return &value
}

func (channel StreamChannel) valid() bool {
	switch channel {
	case StreamChannelTicker, StreamChannelOrderBook, StreamChannelTrade,
		StreamChannelMyOrder, StreamChannelMyTrade, StreamChannelMyAsset:
		return true
	default:
		return false
	}
}

func (channel StreamChannel) private() bool {
	return channel == StreamChannelMyOrder || channel == StreamChannelMyTrade ||
		channel == StreamChannelMyAsset
}
