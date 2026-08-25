package htx

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	corestream "github.com/proven-trade/proven-trade-sdk/stream"
)

const maximumStreamMessageBytes = 16 << 20

type streamWireMessage struct {
	ID        json.RawMessage `json:"id"`
	Status    string          `json:"status"`
	Channel   string          `json:"ch"`
	Time      int64           `json:"ts"`
	Ping      *int64          `json:"ping"`
	Subbed    string          `json:"subbed"`
	Unsubbed  string          `json:"unsubbed"`
	ErrorCode string          `json:"err-code"`
	ErrorText string          `json:"err-msg"`
	Tick      json.RawMessage `json:"tick"`
}

// DecodeStreamMessage는 HTX text 또는 gzip binary frame을 control과 시세 데이터로 분류한다.
func DecodeStreamMessage(message corestream.Message) (StreamMessage, error) {
	payload, err := decodeStreamFrame(message)
	if err != nil {
		return StreamMessage{}, err
	}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || len(trimmed) > maximumStreamMessageBytes ||
		!json.Valid(trimmed) || trimmed[0] != '{' {
		return StreamMessage{}, fmt.Errorf("invalid HTX stream JSON object")
	}
	var wire streamWireMessage
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return StreamMessage{}, fmt.Errorf("decode HTX stream envelope: %w", err)
	}
	id, err := optionalScalarText(wire.ID)
	if err != nil {
		return StreamMessage{}, fmt.Errorf("decode HTX stream message ID: %w", err)
	}
	result := StreamMessage{
		ID: id, Status: wire.Status, Timestamp: wire.Time, Ping: wire.Ping,
		Subscribed: wire.Subbed, Unsubscribed: wire.Unsubbed,
		Tick: cloneBytes(wire.Tick), Raw: cloneBytes(trimmed),
	}
	if wire.ErrorCode != "" || wire.ErrorText != "" || wire.Status == "error" {
		result.Error = &StreamError{Code: wire.ErrorCode, Message: wire.ErrorText}
	}
	if wire.Ping != nil {
		if id != "" || wire.Channel != "" || len(wire.Tick) != 0 || wire.Subbed != "" ||
			wire.Unsubbed != "" || result.Error != nil {
			return StreamMessage{}, fmt.Errorf("HTX heartbeat contains unexpected fields")
		}
		return result, nil
	}
	if wire.Channel != "" {
		if len(wire.Tick) == 0 || id != "" || wire.Subbed != "" || wire.Unsubbed != "" ||
			result.Error != nil {
			return StreamMessage{}, fmt.Errorf("HTX data event envelope is invalid")
		}
		channel, symbol, err := parseStreamTopic(wire.Channel)
		if err != nil {
			return StreamMessage{}, err
		}
		result.Topic = wire.Channel
		result.Channel = channel
		result.Symbol = symbol
		return result, nil
	}
	if id == "" || wire.Status == "" {
		return StreamMessage{}, fmt.Errorf("HTX stream message has no recognized event")
	}
	topic := wire.Subbed
	if topic == "" {
		topic = wire.Unsubbed
	}
	if result.Error == nil && topic == "" {
		return StreamMessage{}, fmt.Errorf("HTX successful control response has no topic")
	}
	if topic != "" {
		channel, symbol, err := parseStreamTopic(topic)
		if err != nil {
			return StreamMessage{}, err
		}
		result.Topic = topic
		result.Channel = channel
		result.Symbol = symbol
	}
	return result, nil
}

func decodeStreamFrame(message corestream.Message) ([]byte, error) {
	switch message.Type {
	case corestream.MessageText:
		if len(message.Data) > maximumStreamMessageBytes {
			return nil, fmt.Errorf("HTX stream text frame exceeds size limit")
		}
		return cloneBytes(message.Data), nil
	case corestream.MessageBinary:
		reader, err := gzip.NewReader(bytes.NewReader(message.Data))
		if err != nil {
			return nil, fmt.Errorf("open HTX gzip stream frame: %w", err)
		}
		payload, readErr := io.ReadAll(io.LimitReader(reader, maximumStreamMessageBytes+1))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, fmt.Errorf("decompress HTX stream frame: %w", readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close HTX gzip stream frame: %w", closeErr)
		}
		if len(payload) > maximumStreamMessageBytes {
			return nil, fmt.Errorf("HTX stream binary frame exceeds decompressed size limit")
		}
		return payload, nil
	default:
		return nil, fmt.Errorf("unsupported HTX stream frame type %d", message.Type)
	}
}

func parseStreamTopic(topic string) (StreamChannel, string, error) {
	parts := strings.Split(topic, ".")
	if len(parts) < 3 || parts[0] != "market" {
		return "", "", fmt.Errorf("invalid HTX stream topic %q", topic)
	}
	symbol := parts[1]
	if err := validateSymbol(symbol); err != nil {
		return "", "", err
	}
	var channel StreamChannel
	switch {
	case len(parts) == 3 && parts[2] == "ticker":
		channel = StreamChannelTicker
	case len(parts) == 4 && parts[2] == "depth" && DepthType(parts[3]).valid():
		channel = StreamChannelDepth
	case len(parts) == 3 && parts[2] == "bbo":
		channel = StreamChannelBBO
	case len(parts) == 4 && parts[2] == "trade" && parts[3] == "detail":
		channel = StreamChannelTrades
	case len(parts) == 4 && parts[2] == "kline" && CandleInterval(parts[3]).valid():
		channel = StreamChannelCandles
	default:
		return "", "", fmt.Errorf("unsupported HTX stream topic %q", topic)
	}
	return channel, symbol, nil
}
