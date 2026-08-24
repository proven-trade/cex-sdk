package kucoin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	trade "github.com/proven-trade/proven-trade-sdk"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	proOrderBookTopic = "obu.SPOT"
	proOrderBookDepth = "increment@10ms"
)

// ProOrderBookEvent는 상위 500호가 snapshot 또는 절대 수량 delta다.
type ProOrderBookEvent struct {
	SequenceStart int64       `json:"O"`
	SequenceEnd   int64       `json:"C"`
	MatchingTime  int64       `json:"M"`
	Symbol        string      `json:"s"`
	Asks          []BookLevel `json:"a"`
	Bids          []BookLevel `json:"b"`
}

// ProOrderBookMessage는 Pro public 제어 응답 또는 orderbook 이벤트다.
type ProOrderBookMessage struct {
	ID           string
	Result       string
	ControlType  string
	ErrorCode    string
	ErrorMessage string
	Topic        string
	Depth        string
	UpdateType   string
	PublishTime  int64
	Data         ProOrderBookEvent
	Raw          json.RawMessage
}

// ProOrderBookHandler는 Pro orderbook 제어 응답과 데이터 이벤트를 처리한다.
type ProOrderBookHandler func(context.Context, ProOrderBookMessage) error

// ProOrderBookStream은 현재 권장 Increment Best 500 Spot 호가 세션이다.
type ProOrderBookStream struct {
	session *corestream.Session
	symbol  string
	nextID  func() string
}

type proOrderBookWireMessage struct {
	ID          json.RawMessage `json:"id"`
	Result      json.RawMessage `json:"result"`
	ControlType string          `json:"type"`
	Code        json.RawMessage `json:"code"`
	Message     string          `json:"msg"`
	MessageAlt  string          `json:"message"`
	Topic       string          `json:"T"`
	Depth       string          `json:"dp"`
	UpdateType  string          `json:"t"`
	PublishTime int64           `json:"P"`
	Data        json.RawMessage `json:"d"`
}

// ProOrderBookStream은 token 없는 Pro public endpoint에서 현재 권장 Spot 호가 세션을 생성한다.
func (client *StreamClient) ProOrderBookStream(
	symbol string,
	options ...trade.RequestOption,
) (*ProOrderBookStream, error) {
	symbol = strings.ToUpper(symbol)
	if err := validateSymbol(symbol); err != nil {
		return nil, err
	}
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	stream := &ProOrderBookStream{
		symbol: symbol,
		nextID: func() string {
			return strconv.FormatInt(client.nextMessageID.Add(1), 10)
		},
	}
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		Request: corestream.DialRequest{Endpoint: client.proPublicURL}, OnConnect: stream.subscribe,
		Observer: client.observer, ReconnectPolicy: client.streamReconnectPolicy,
		Backoff: client.backoff, MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval: client.pingInterval, PingTimeout: client.pingTimeout,
	})
	if err != nil {
		return nil, err
	}
	stream.session = session
	return stream, nil
}

// Run은 Pro orderbook 메시지를 순서대로 decode해 handler에 전달한다.
func (stream *ProOrderBookStream) Run(ctx context.Context, handler ProOrderBookHandler) error {
	if handler == nil {
		return fmt.Errorf("KuCoin Pro order book stream handler is required")
	}
	return stream.session.Run(ctx, func(handlerContext context.Context, message corestream.Message) error {
		decoded, err := DecodeProOrderBookMessage(message)
		if err != nil {
			return err
		}
		return handler(handlerContext, decoded)
	})
}

// Close는 Pro public orderbook 세션을 종료한다.
func (stream *ProOrderBookStream) Close() error { return stream.session.Close() }

// Generation은 성공한 Pro public 연결 세대 번호를 반환한다.
func (stream *ProOrderBookStream) Generation() uint64 { return stream.session.Generation() }

// EgressRouteID는 Pro public 연결과 재연결에 고정된 송신 경로를 반환한다.
func (stream *ProOrderBookStream) EgressRouteID() transport.EgressRouteID {
	return stream.session.EgressRouteID()
}

func (stream *ProOrderBookStream) reconnect() error { return stream.session.Reconnect() }

func (stream *ProOrderBookStream) subscribe(
	ctx context.Context,
	connection corestream.Connection,
) error {
	payload, err := json.Marshal(struct {
		ID        string `json:"id"`
		Action    string `json:"action"`
		Channel   string `json:"channel"`
		TradeType string `json:"tradeType"`
		Symbol    string `json:"symbol"`
		Depth     string `json:"depth"`
		RPIFilter int    `json:"rpiFilter"`
	}{
		ID: stream.nextID(), Action: "SUBSCRIBE", Channel: "obu",
		TradeType: "SPOT", Symbol: stream.symbol, Depth: proOrderBookDepth,
	})
	if err != nil {
		return fmt.Errorf("encode KuCoin Pro order book subscription: %w", err)
	}
	return connection.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload})
}

// DecodeProOrderBookMessage는 Pro public orderbook 제어 응답과 이벤트를 분류한다.
func DecodeProOrderBookMessage(message corestream.Message) (ProOrderBookMessage, error) {
	trimmed := bytes.TrimSpace(message.Data)
	if len(trimmed) == 0 || !json.Valid(trimmed) || trimmed[0] == '[' {
		return ProOrderBookMessage{}, fmt.Errorf("invalid KuCoin Pro order book JSON object")
	}
	var wire proOrderBookWireMessage
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return ProOrderBookMessage{}, fmt.Errorf("decode KuCoin Pro order book envelope: %w", err)
	}
	id, err := optionalScalarText(wire.ID)
	if err != nil {
		return ProOrderBookMessage{}, fmt.Errorf("decode KuCoin Pro message ID: %w", err)
	}
	result, err := optionalScalarText(wire.Result)
	if err != nil {
		return ProOrderBookMessage{}, fmt.Errorf("decode KuCoin Pro result: %w", err)
	}
	code, err := optionalScalarText(wire.Code)
	if err != nil {
		return ProOrderBookMessage{}, fmt.Errorf("decode KuCoin Pro error code: %w", err)
	}
	decoded := ProOrderBookMessage{
		ID: id, Result: result, ControlType: wire.ControlType,
		ErrorCode: code, ErrorMessage: wire.Message,
		Topic: wire.Topic, Depth: wire.Depth, UpdateType: wire.UpdateType,
		PublishTime: wire.PublishTime, Raw: cloneBytes(trimmed),
	}
	if decoded.ErrorMessage == "" {
		decoded.ErrorMessage = wire.MessageAlt
	}
	if decoded.Result != "" || decoded.ControlType != "" || decoded.ErrorCode != "" {
		return decoded, nil
	}
	if len(bytes.TrimSpace(wire.Data)) > 0 && !bytes.Equal(bytes.TrimSpace(wire.Data), []byte("null")) {
		if err := json.Unmarshal(wire.Data, &decoded.Data); err != nil {
			return ProOrderBookMessage{}, fmt.Errorf("decode KuCoin Pro order book data: %w", err)
		}
	}
	if !strings.EqualFold(decoded.Topic, proOrderBookTopic) || decoded.Depth != proOrderBookDepth ||
		(decoded.UpdateType != "snapshot" && decoded.UpdateType != "delta") {
		return ProOrderBookMessage{}, fmt.Errorf("unsupported KuCoin Pro order book message")
	}
	if decoded.PublishTime <= 0 || decoded.Data.Symbol == "" {
		return ProOrderBookMessage{}, fmt.Errorf("invalid KuCoin Pro order book event")
	}
	return decoded, nil
}
