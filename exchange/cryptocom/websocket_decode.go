package cryptocom

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	corestream "github.com/proven-trade/cex-sdk/stream"
)

const maximumCryptoComStreamMessageBytes = 16 << 20

type streamWireMessage struct {
	ID       json.RawMessage `json:"id"`
	Method   string          `json:"method"`
	Code     json.RawMessage `json:"code"`
	Result   json.RawMessage `json:"result"`
	Message  string          `json:"message"`
	Original string          `json:"original"`
}

type streamWireResult struct {
	InstrumentName string          `json:"instrument_name"`
	Subscription   string          `json:"subscription"`
	Channel        string          `json:"channel"`
	Depth          Integer         `json:"depth"`
	Data           json.RawMessage `json:"data"`
}

// DecodeStreamMessage는 Crypto.com text frame을 명령 응답·heartbeat·시세·사용자 데이터로 분류한다.
func DecodeStreamMessage(message corestream.Message) (StreamMessage, error) {
	if message.Type != corestream.MessageText {
		return StreamMessage{}, fmt.Errorf("unsupported Crypto.com stream frame type %d", message.Type)
	}
	trimmed := bytes.TrimSpace(message.Data)
	if len(trimmed) == 0 || len(trimmed) > maximumCryptoComStreamMessageBytes ||
		!json.Valid(trimmed) || trimmed[0] != '{' {
		return StreamMessage{}, fmt.Errorf("invalid Crypto.com stream JSON object")
	}
	var wire streamWireMessage
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return StreamMessage{}, fmt.Errorf("decode Crypto.com stream envelope: %w", err)
	}
	id, err := optionalScalarText(wire.ID)
	if err != nil {
		return StreamMessage{}, fmt.Errorf("decode Crypto.com stream message ID: %w", err)
	}
	if id == "" {
		return StreamMessage{}, fmt.Errorf("Crypto.com stream message ID is missing")
	}
	code, err := optionalScalarText(wire.Code)
	if err != nil {
		return StreamMessage{}, fmt.Errorf("decode Crypto.com stream response code: %w", err)
	}
	if code == "" {
		return StreamMessage{}, fmt.Errorf("Crypto.com stream response code is missing")
	}
	if wire.Method == "" {
		return StreamMessage{}, fmt.Errorf("Crypto.com stream method is missing")
	}
	result := StreamMessage{
		ID: id, Method: wire.Method, Code: code, Raw: cloneBytes(trimmed),
	}
	if code != "0" {
		result.Error = &StreamError{Code: code, Message: wire.Message, Original: wire.Original}
		return result, nil
	}
	if wire.Message != "" || wire.Original != "" {
		return StreamMessage{}, fmt.Errorf("successful Crypto.com stream response contains error fields")
	}
	if wire.Method == "public/heartbeat" {
		if len(bytes.TrimSpace(wire.Result)) != 0 {
			return StreamMessage{}, fmt.Errorf("Crypto.com heartbeat contains an unexpected result")
		}
		result.Heartbeat = true
		return result, nil
	}
	resultRaw := bytes.TrimSpace(wire.Result)
	if len(resultRaw) == 0 || bytes.Equal(resultRaw, []byte("null")) ||
		bytes.Equal(resultRaw, []byte("{}")) {
		if wire.Method != "subscribe" && wire.Method != "unsubscribe" &&
			wire.Method != "public/respond-heartbeat" && wire.Method != "public/auth" {
			return StreamMessage{}, fmt.Errorf("unsupported Crypto.com stream method %q", wire.Method)
		}
		return result, nil
	}
	if resultRaw[0] != '{' {
		return StreamMessage{}, fmt.Errorf("Crypto.com stream result is not an object")
	}
	var wireResult streamWireResult
	if err := json.Unmarshal(resultRaw, &wireResult); err != nil {
		return StreamMessage{}, fmt.Errorf("decode Crypto.com stream result: %w", err)
	}
	if wireResult.Subscription == "" {
		if wireResult.InstrumentName == "" && wireResult.Channel == "" && wireResult.Depth == 0 &&
			len(bytes.TrimSpace(wireResult.Data)) == 0 &&
			(wire.Method == "subscribe" || wire.Method == "unsubscribe" ||
				wire.Method == "public/respond-heartbeat" || wire.Method == "public/auth") {
			return result, nil
		}
		return StreamMessage{}, fmt.Errorf("Crypto.com stream data subscription is missing")
	}
	channel, instrumentName, depth, err := parseCryptoComStreamSubscription(wireResult.Subscription)
	if err != nil {
		return StreamMessage{}, err
	}
	if wire.Method != "subscribe" || wireResult.InstrumentName != instrumentName ||
		wireResult.Channel != cryptoComStreamChannelName(channel) {
		return StreamMessage{}, fmt.Errorf("Crypto.com stream data metadata is inconsistent")
	}
	resultDepth, err := intFromInteger(wireResult.Depth, "stream book depth")
	if err != nil {
		return StreamMessage{}, err
	}
	if channel == StreamChannelBook && resultDepth != depth ||
		channel != StreamChannelBook && resultDepth != 0 {
		return StreamMessage{}, fmt.Errorf("Crypto.com stream data depth is inconsistent")
	}
	dataRaw := bytes.TrimSpace(wireResult.Data)
	if len(dataRaw) == 0 || dataRaw[0] != '[' {
		return StreamMessage{}, fmt.Errorf("Crypto.com stream data is not an array")
	}
	var dataItems []json.RawMessage
	if err := json.Unmarshal(dataRaw, &dataItems); err != nil {
		return StreamMessage{}, fmt.Errorf("decode Crypto.com stream data array: %w", err)
	}
	result.InstrumentName = instrumentName
	result.Subscription = wireResult.Subscription
	result.Channel = channel
	result.Depth = depth
	result.Private = cryptoComPrivateStreamChannel(channel)
	result.Data = cloneBytes(dataRaw)
	return result, nil
}

func parseCryptoComStreamSubscription(
	value string,
) (StreamChannel, string, int, error) {
	parts := strings.Split(value, ".")
	var channel StreamChannel
	var instrumentName string
	depth := 0
	switch {
	case len(parts) == 2 && parts[0] == "user" && parts[1] == "order":
		channel = StreamChannelUserOrders
	case len(parts) == 3 && parts[0] == "user" && parts[1] == "order":
		channel = StreamChannelUserOrders
		instrumentName = parts[2]
	case len(parts) == 2 && parts[0] == "user" && parts[1] == "trade":
		channel = StreamChannelUserTrades
	case len(parts) == 3 && parts[0] == "user" && parts[1] == "trade":
		channel = StreamChannelUserTrades
		instrumentName = parts[2]
	case len(parts) == 2 && parts[0] == "user" && parts[1] == "balance":
		channel = StreamChannelUserBalances
	case len(parts) == 2 && parts[0] == string(StreamChannelTicker):
		channel = StreamChannelTicker
		instrumentName = parts[1]
	case len(parts) == 2 && parts[0] == string(StreamChannelTrades):
		channel = StreamChannelTrades
		instrumentName = parts[1]
	case len(parts) == 3 && parts[0] == string(StreamChannelCandles) &&
		CandleTimeframe(parts[1]).valid():
		channel = StreamChannelCandles
		instrumentName = parts[2]
	case len(parts) == 3 && parts[0] == string(StreamChannelBook):
		parsedDepth, err := strconv.Atoi(parts[2])
		if err != nil || !StreamBookDepth(parsedDepth).valid() {
			return "", "", 0, fmt.Errorf("invalid Crypto.com stream subscription %q", value)
		}
		channel = StreamChannelBook
		instrumentName = parts[1]
		depth = parsedDepth
	default:
		return "", "", 0, fmt.Errorf("unsupported Crypto.com stream subscription %q", value)
	}
	if instrumentName != "" {
		if err := validateInstrumentName(instrumentName); err != nil {
			return "", "", 0, err
		}
	}
	return channel, instrumentName, depth, nil
}

func cryptoComPrivateStreamChannel(channel StreamChannel) bool {
	return channel == StreamChannelUserOrders || channel == StreamChannelUserTrades ||
		channel == StreamChannelUserBalances
}

func cryptoComStreamChannelName(channel StreamChannel) string {
	if cryptoComPrivateStreamChannel(channel) {
		return strings.SplitN(string(channel), ".", 2)[0]
	}
	return string(channel)
}
