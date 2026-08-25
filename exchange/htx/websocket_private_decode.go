package htx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	corestream "github.com/proven-trade/proven-trade-sdk/stream"
)

type privateStreamWireMessage struct {
	Action  string          `json:"action"`
	Code    int             `json:"code"`
	Channel string          `json:"ch"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// DecodePrivateStreamMessage는 HTX v2 text frame을 인증·heartbeat·구독 응답·계정 이벤트로 분류한다.
func DecodePrivateStreamMessage(message corestream.Message) (StreamMessage, error) {
	if message.Type != corestream.MessageText {
		return StreamMessage{}, fmt.Errorf("HTX private stream only accepts text frames")
	}
	trimmed := bytes.TrimSpace(message.Data)
	if len(trimmed) == 0 || len(trimmed) > maximumStreamMessageBytes ||
		!json.Valid(trimmed) || trimmed[0] != '{' {
		return StreamMessage{}, fmt.Errorf("invalid HTX private stream JSON object")
	}
	var wire privateStreamWireMessage
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return StreamMessage{}, fmt.Errorf("decode HTX private stream envelope: %w", err)
	}
	result := StreamMessage{
		Action: wire.Action, Code: wire.Code, Message: wire.Message, Private: true,
		Data: cloneBytes(wire.Data), Raw: cloneBytes(trimmed),
	}
	if wire.Code != 0 && wire.Code != 200 {
		result.Error = &StreamError{Code: strconv.Itoa(wire.Code), Message: wire.Message}
	}
	if wire.Action == "ping" {
		if wire.Channel != "" || wire.Code != 0 || wire.Message != "" {
			return StreamMessage{}, fmt.Errorf("HTX private heartbeat contains unexpected fields")
		}
		var heartbeat struct {
			Timestamp *int64 `json:"ts"`
		}
		if err := json.Unmarshal(wire.Data, &heartbeat); err != nil || heartbeat.Timestamp == nil {
			return StreamMessage{}, fmt.Errorf("decode HTX private heartbeat")
		}
		result.Ping = heartbeat.Timestamp
		return result, nil
	}
	if wire.Channel == "auth" {
		if wire.Action != "req" || wire.Code == 0 ||
			(result.Error == nil && len(wire.Data) == 0) {
			return StreamMessage{}, fmt.Errorf("HTX private authentication response is invalid")
		}
		result.Topic = wire.Channel
		return result, nil
	}
	if wire.Channel == "" {
		return StreamMessage{}, fmt.Errorf("HTX private stream message has no channel")
	}
	channel, symbol, mode, err := parsePrivateStreamTopic(wire.Channel)
	if err != nil {
		return StreamMessage{}, err
	}
	result.Topic = wire.Channel
	result.Channel = channel
	result.Symbol = symbol
	result.Mode = mode
	switch wire.Action {
	case "sub", "unsub":
		if wire.Code == 0 || (result.Error == nil && len(wire.Data) == 0) {
			return StreamMessage{}, fmt.Errorf("HTX private control response is invalid")
		}
	case "push", "":
		if wire.Code != 0 || result.Error != nil || len(wire.Data) == 0 {
			return StreamMessage{}, fmt.Errorf("HTX private data event envelope is invalid")
		}
	default:
		return StreamMessage{}, fmt.Errorf("unsupported HTX private stream action %q", wire.Action)
	}
	return result, nil
}

func parsePrivateStreamTopic(topic string) (StreamChannel, string, StreamMode, error) {
	parts := strings.Split(topic, "#")
	switch parts[0] {
	case "orders":
		if len(parts) != 2 || !privateStreamSymbolValid(parts[1]) {
			return "", "", 0, fmt.Errorf("invalid HTX private order topic %q", topic)
		}
		return StreamChannelOrders, parts[1], 0, nil
	case "trade.clearing":
		if len(parts) != 3 || !privateStreamSymbolValid(parts[1]) {
			return "", "", 0, fmt.Errorf("invalid HTX private clearing topic %q", topic)
		}
		mode, err := strconv.Atoi(parts[2])
		if err != nil || mode < 0 || mode > 1 {
			return "", "", 0, fmt.Errorf("invalid HTX private clearing topic %q", topic)
		}
		return StreamChannelClearing, parts[1], StreamMode(mode), nil
	case "accounts.update":
		if len(parts) == 1 {
			return StreamChannelAccounts, "", StreamModeBalanceOnly, nil
		}
		if len(parts) != 2 {
			return "", "", 0, fmt.Errorf("invalid HTX private account topic %q", topic)
		}
		mode, err := strconv.Atoi(parts[1])
		if err != nil || mode < 0 || mode > 2 {
			return "", "", 0, fmt.Errorf("invalid HTX private account topic %q", topic)
		}
		return StreamChannelAccounts, "", StreamMode(mode), nil
	default:
		return "", "", 0, fmt.Errorf("unsupported HTX private stream topic %q", topic)
	}
}

func privateStreamSymbolValid(symbol string) bool {
	return symbol == "*" || symbolPattern.MatchString(symbol)
}
