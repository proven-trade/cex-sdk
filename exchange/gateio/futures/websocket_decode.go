package futures

import (
	"bytes"
	"encoding/json"
	"fmt"

	corestream "github.com/proven-trade/proven-trade-sdk/stream"
)

type streamWireMessage struct {
	ID        json.RawMessage `json:"id"`
	Time      int64           `json:"time"`
	TimeMilli int64           `json:"time_ms"`
	Channel   string          `json:"channel"`
	Event     string          `json:"event"`
	Error     *StreamError    `json:"error"`
	Result    json.RawMessage `json:"result"`
}

// DecodeStreamMessage는 Gate.io Futures WebSocket text frame을 제어 또는 데이터 메시지로 분류한다.
func DecodeStreamMessage(message corestream.Message) (StreamMessage, error) {
	if message.Type != corestream.MessageText {
		return StreamMessage{}, fmt.Errorf("Gate.io Futures JSON stream only accepts text frames")
	}
	trimmed := bytes.TrimSpace(message.Data)
	if len(trimmed) == 0 || !json.Valid(trimmed) || trimmed[0] == '[' {
		return StreamMessage{}, fmt.Errorf("invalid Gate.io Futures stream JSON object")
	}
	var wire streamWireMessage
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return StreamMessage{}, fmt.Errorf("decode Gate.io Futures stream envelope: %w", err)
	}
	if wire.Channel == "" {
		return StreamMessage{}, fmt.Errorf("Gate.io Futures stream message has no channel")
	}
	id, err := streamScalarText(wire.ID)
	if err != nil {
		return StreamMessage{}, fmt.Errorf("decode Gate.io Futures stream message ID: %w", err)
	}
	channel := streamChannelFromName(wire.Channel)
	return StreamMessage{
		ID: id, Time: wire.Time, TimeMilli: wire.TimeMilli,
		ChannelName: wire.Channel, Channel: channel, Event: wire.Event,
		Private: streamChannelPrivate(channel), Error: wire.Error,
		Result: cloneBytes(wire.Result), Raw: cloneBytes(trimmed),
	}, nil
}

func streamScalarText(raw json.RawMessage) (string, error) {
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
	var number json.Number
	if err := json.Unmarshal(trimmed, &number); err != nil {
		return "", err
	}
	return number.String(), nil
}

func streamChannelFromName(name string) StreamChannel {
	switch name {
	case "futures.tickers":
		return StreamChannelTicker
	case "futures.trades":
		return StreamChannelTrades
	case "futures.candlesticks":
		return StreamChannelCandles
	case "futures.book_ticker":
		return StreamChannelBookTicker
	case "futures.order_book_update":
		return StreamChannelOrderBookUpdate
	case "futures.orders":
		return StreamChannelOrders
	case "futures.usertrades":
		return StreamChannelUserTrades
	case "futures.balances":
		return StreamChannelBalances
	case "futures.positions":
		return StreamChannelPositions
	default:
		return ""
	}
}

func streamChannelPrivate(channel StreamChannel) bool {
	return channel == StreamChannelOrders || channel == StreamChannelUserTrades ||
		channel == StreamChannelBalances || channel == StreamChannelPositions
}
