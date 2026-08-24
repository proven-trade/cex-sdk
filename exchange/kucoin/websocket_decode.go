package kucoin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	corestream "github.com/proven-trade/proven-trade-sdk/stream"
)

type streamWireMessage struct {
	ID          json.RawMessage `json:"id"`
	Type        string          `json:"type"`
	Topic       string          `json:"topic"`
	Subject     string          `json:"subject"`
	ChannelType string          `json:"channelType"`
	UserID      string          `json:"userId"`
	Code        json.RawMessage `json:"code"`
	Message     string          `json:"msg"`
	Data        json.RawMessage `json:"data"`
}

// DecodeStreamMessage는 KuCoin WebSocket frame을 control 또는 data 메시지로 분류한다.
func DecodeStreamMessage(message corestream.Message) (StreamMessage, error) {
	trimmed := bytes.TrimSpace(message.Data)
	if len(trimmed) == 0 || !json.Valid(trimmed) || trimmed[0] == '[' {
		return StreamMessage{}, fmt.Errorf("invalid KuCoin stream JSON object")
	}
	var wire streamWireMessage
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return StreamMessage{}, fmt.Errorf("decode KuCoin stream envelope: %w", err)
	}
	if wire.Type == "" {
		return StreamMessage{}, fmt.Errorf("KuCoin stream message has no type")
	}
	id, err := optionalScalarText(wire.ID)
	if err != nil {
		return StreamMessage{}, fmt.Errorf("decode KuCoin stream message ID: %w", err)
	}
	code, err := optionalScalarText(wire.Code)
	if err != nil {
		return StreamMessage{}, fmt.Errorf("decode KuCoin stream error code: %w", err)
	}
	return StreamMessage{
		ID: id, Type: wire.Type, Topic: wire.Topic, Subject: wire.Subject,
		Channel: streamChannelFromTopic(wire.Topic), Private: wire.ChannelType == "private",
		UserID: wire.UserID, ErrorCode: code, ErrorMessage: wire.Message,
		Data: cloneBytes(wire.Data), Raw: cloneBytes(trimmed),
	}, nil
}

func streamChannelFromTopic(topic string) StreamChannel {
	switch {
	case strings.HasPrefix(topic, "/market/ticker:"):
		return StreamChannelTicker
	case strings.HasPrefix(topic, "/market/level2:"):
		return StreamChannelLevel2
	case strings.HasPrefix(topic, "/spotMarket/level2Depth5:"):
		return StreamChannelOrderBook5
	case strings.HasPrefix(topic, "/spotMarket/level2Depth50:"):
		return StreamChannelOrderBook50
	case strings.HasPrefix(topic, "/market/candles:"):
		return StreamChannelCandles
	case strings.HasPrefix(topic, "/market/match:"):
		return StreamChannelTrade
	case topic == "/spotMarket/tradeOrdersV2":
		return StreamChannelOrders
	case topic == "/account/balance":
		return StreamChannelBalance
	default:
		return ""
	}
}

func optionalScalarText(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return "", err
		}
		return value, nil
	}
	if bytes.Equal(trimmed, []byte("true")) || bytes.Equal(trimmed, []byte("false")) {
		return string(trimmed), nil
	}
	var number json.Number
	if err := json.Unmarshal(trimmed, &number); err != nil {
		return "", err
	}
	return number.String(), nil
}
