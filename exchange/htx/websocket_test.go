package htx

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

type htxWebSocketReadResult struct {
	message corestream.Message
	err     error
}

type htxWebSocketTestConnection struct {
	reads     chan htxWebSocketReadResult
	writes    chan corestream.Message
	closed    chan struct{}
	closeOnce sync.Once
}

func newHTXWebSocketTestConnection() *htxWebSocketTestConnection {
	return &htxWebSocketTestConnection{
		reads:  make(chan htxWebSocketReadResult, 16),
		writes: make(chan corestream.Message, 16), closed: make(chan struct{}),
	}
}

func (connection *htxWebSocketTestConnection) Read(
	ctx context.Context,
) (corestream.Message, error) {
	select {
	case <-ctx.Done():
		return corestream.Message{}, ctx.Err()
	case <-connection.closed:
		return corestream.Message{}, corestream.ErrSessionClosed
	case result := <-connection.reads:
		return result.message, result.err
	}
}

func (connection *htxWebSocketTestConnection) Write(
	ctx context.Context,
	message corestream.Message,
) error {
	message.Data = cloneBytes(message.Data)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-connection.closed:
		return corestream.ErrSessionClosed
	case connection.writes <- message:
		return nil
	}
}

func (connection *htxWebSocketTestConnection) Ping(context.Context) error { return nil }

func (connection *htxWebSocketTestConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type htxWebSocketTestConnector struct {
	mu          sync.Mutex
	connections []*htxWebSocketTestConnection
	routes      []transport.EgressRouteID
	requests    []corestream.DialRequest
}

func (connector *htxWebSocketTestConnector) Connect(
	_ context.Context,
	routeID transport.EgressRouteID,
	request corestream.DialRequest,
) (corestream.Connection, error) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	connector.routes = append(connector.routes, routeID)
	connector.requests = append(connector.requests, corestream.DialRequest{
		Endpoint: request.Endpoint, Header: request.Header.Clone(),
	})
	if len(connector.connections) == 0 {
		return nil, errors.New("no HTX test connection")
	}
	connection := connector.connections[0]
	connector.connections = connector.connections[1:]
	return connection, nil
}

func (connector *htxWebSocketTestConnector) snapshot() (
	[]transport.EgressRouteID,
	[]corestream.DialRequest,
) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	requests := make([]corestream.DialRequest, len(connector.requests))
	for index, request := range connector.requests {
		requests[index] = corestream.DialRequest{
			Endpoint: request.Endpoint, Header: request.Header.Clone(),
		}
	}
	return slices.Clone(connector.routes), requests
}

type htxStreamCommand struct {
	Subscribe   string `json:"sub"`
	Unsubscribe string `json:"unsub"`
	ID          string `json:"id"`
}

func TestHTXPublicStreamRestoresSubscriptionOnSelectedRoute(t *testing.T) {
	t.Parallel()

	first := newHTXWebSocketTestConnection()
	second := newHTXWebSocketTestConnection()
	connector := &htxWebSocketTestConnector{
		connections: []*htxWebSocketTestConnection{first, second},
	}
	client := newTestHTXStreamClient(t, connector)
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelTicker, Symbol: "btcusdt",
	}}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var ticker StreamTicker
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Channel != StreamChannelTicker {
				return nil
			}
			if err := message.Decode(&ticker); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	firstCommand := decodeHTXStreamCommand(t, waitForHTXWebSocketWrite(t, first))
	assertHTXStreamCommand(t, firstCommand, "subscribe", "market.btcusdt.ticker")
	first.reads <- htxWebSocketReadResult{err: errors.New("connection lost")}
	secondCommand := decodeHTXStreamCommand(t, waitForHTXWebSocketWrite(t, second))
	assertHTXStreamCommand(t, secondCommand, "subscribe", "market.btcusdt.ticker")
	second.reads <- htxWebSocketReadResult{message: gzipHTXStreamMessage(t,
		`{"ch":"market.btcusdt.ticker","ts":1700000000000,"tick":{"open":62000,"high":65000,"low":61000,"close":64000,"amount":10.5,"vol":670000,"count":42,"bid":63999,"bidSize":0.4,"ask":64001,"askSize":0.3,"lastPrice":64000,"lastSize":0.1}}`,
	)}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("public Run() did not finish")
	}
	if ticker.Close != "64000" || ticker.BidPrice != "63999" || ticker.Count != 42 {
		t.Fatalf("ticker = %+v", ticker)
	}
	if public.Generation() != 2 || public.EgressRouteID() != "route-b" {
		t.Fatalf("generation = %d, route = %q", public.Generation(), public.EgressRouteID())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) ||
		len(requests) != 2 {
		t.Fatalf("routes = %v, requests = %d", routes, len(requests))
	}
	for _, request := range requests {
		if request.Endpoint != "ws://stream.example.test/ws" || len(request.Header) != 0 {
			t.Fatalf("dial request = %+v", request)
		}
	}
	_ = public.Close()
}

func TestHTXPublicStreamAnswersApplicationHeartbeat(t *testing.T) {
	t.Parallel()

	connection := newHTXWebSocketTestConnection()
	client := newTestHTXStreamClient(t, &htxWebSocketTestConnector{
		connections: []*htxWebSocketTestConnection{connection},
	})
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelBBO, Symbol: "ethusdt",
	}}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	seen := make(chan struct{}, 1)
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Ping != nil {
				seen <- struct{}{}
			}
			return nil
		})
	}()
	_ = waitForHTXWebSocketWrite(t, connection)
	connection.reads <- htxWebSocketReadResult{message: gzipHTXStreamMessage(t, `{"ping":1700000000123}`)}
	select {
	case <-seen:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat was not delivered")
	}
	pong := waitForHTXWebSocketWrite(t, connection)
	if pong.Type != corestream.MessageText || string(pong.Data) != `{"pong":1700000000123}` {
		t.Fatalf("pong = type %d, %s", pong.Type, pong.Data)
	}
	cancel()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("public Run() did not finish")
	}
}

func TestHTXStreamRollsBackRejectedDynamicSubscription(t *testing.T) {
	t.Parallel()

	connection := newHTXWebSocketTestConnection()
	client := newTestHTXStreamClient(t, &htxWebSocketTestConnector{
		connections: []*htxWebSocketTestConnection{connection},
	})
	initial := StreamSubscription{Channel: StreamChannelTicker, Symbol: "btcusdt"}
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{initial}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	rejected := make(chan struct{}, 1)
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Error != nil {
				rejected <- struct{}{}
			}
			return nil
		})
	}()
	_ = waitForHTXWebSocketWrite(t, connection)
	dynamic := StreamSubscription{Channel: StreamChannelBBO, Symbol: "ethusdt"}
	if err := public.Subscribe(ctx, dynamic); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	command := decodeHTXStreamCommand(t, waitForHTXWebSocketWrite(t, connection))
	assertHTXStreamCommand(t, command, "subscribe", "market.ethusdt.bbo")
	connection.reads <- htxWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"id":"` + command.ID + `","status":"error","err-code":"bad-request","err-msg":"subscription denied"}`),
	}}
	select {
	case <-rejected:
	case <-time.After(2 * time.Second):
		t.Fatal("rejected subscription response was not delivered")
	}
	if subscriptions := public.managed.snapshotSubscriptions(); len(subscriptions) != 1 || subscriptions[0] != initial {
		t.Fatalf("subscriptions = %+v", subscriptions)
	}
	cancel()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("public Run() did not finish")
	}
}

func TestHTXDecodeStreamMessageSupportsPublicChannels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		channel StreamChannel
		symbol  string
		target  any
	}{
		{
			name: "ticker", channel: StreamChannelTicker, symbol: "btcusdt",
			payload: `{"ch":"market.btcusdt.ticker","ts":1,"tick":{"open":1,"high":2,"low":0.5,"close":1.5,"amount":10,"vol":15,"count":3,"bid":1.4,"bidSize":2,"ask":1.6,"askSize":4,"lastPrice":1.5,"lastSize":1}}`,
			target:  &StreamTicker{},
		},
		{
			name: "depth", channel: StreamChannelDepth, symbol: "btcusdt",
			payload: `{"ch":"market.btcusdt.depth.step0","ts":1,"tick":{"ts":1,"version":99,"bids":[[1,2]],"asks":[[3,4]]}}`,
			target:  &StreamDepth{},
		},
		{
			name: "bbo", channel: StreamChannelBBO, symbol: "ethusdt",
			payload: `{"ch":"market.ethusdt.bbo","ts":1,"tick":{"seqId":5,"ask":3,"askSize":4,"bid":1,"bidSize":2,"quoteTime":1,"symbol":"ethusdt"}}`,
			target:  &StreamBBO{},
		},
		{
			name: "trades", channel: StreamChannelTrades, symbol: "btcusdt",
			payload: `{"ch":"market.btcusdt.trade.detail","ts":1,"tick":{"id":2,"ts":1,"data":[{"id":2,"tradeId":3,"amount":1,"price":2,"ts":1,"direction":"buy"}]}}`,
			target:  &StreamTradeBatch{},
		},
		{
			name: "candles", channel: StreamChannelCandles, symbol: "btcusdt",
			payload: `{"ch":"market.btcusdt.kline.1min","ts":1,"tick":{"id":1,"open":1,"close":2,"low":0.5,"high":3,"amount":4,"vol":8,"count":2}}`,
			target:  &StreamCandle{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			message, err := DecodeStreamMessage(gzipHTXStreamMessage(t, test.payload))
			if err != nil {
				t.Fatalf("DecodeStreamMessage() error = %v", err)
			}
			if message.Channel != test.channel || message.Symbol != test.symbol ||
				message.Topic == "" || len(message.Raw) == 0 {
				t.Fatalf("message = %+v", message)
			}
			if err := message.Decode(test.target); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
		})
	}
}

func TestHTXDecodeStreamMessageClassifiesControlAndRejectsMalformedFrames(t *testing.T) {
	t.Parallel()

	ack, err := DecodeStreamMessage(corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"id":"htx-1","status":"ok","subbed":"market.btcusdt.ticker","ts":1}`),
	})
	if err != nil {
		t.Fatalf("DecodeStreamMessage() error = %v", err)
	}
	if ack.ID != "htx-1" || ack.Status != "ok" || ack.Subscribed == "" ||
		ack.Channel != StreamChannelTicker {
		t.Fatalf("ack = %+v", ack)
	}
	invalid := []corestream.Message{
		{Type: corestream.MessageBinary, Data: []byte("not-gzip")},
		{Type: corestream.MessageText, Data: []byte(`[]`)},
		{Type: corestream.MessageText, Data: []byte(`{"ch":"market.btcusdt.unknown","tick":{}}`)},
		{Type: corestream.MessageText, Data: []byte(`{"id":"htx-1","status":"ok"}`)},
	}
	for _, message := range invalid {
		if _, err := DecodeStreamMessage(message); err == nil {
			t.Fatalf("DecodeStreamMessage(%q) error = nil", message.Data)
		}
	}
}

func TestHTXStreamValidation(t *testing.T) {
	t.Parallel()

	connector := &htxWebSocketTestConnector{}
	if _, err := NewStreamClient(StreamClientConfig{
		Connector: connector, DefaultEgressRouteID: "route-a",
		PublicWebSocketURL: "ws://stream.example.test/ws",
	}); err == nil {
		t.Fatal("NewStreamClient() insecure URL error = nil")
	}
	client := newTestHTXStreamClient(t, connector)
	invalid := []StreamSubscription{
		{Channel: StreamChannelTicker, Symbol: "BTCUSDT"},
		{Channel: StreamChannelDepth, Symbol: "btcusdt"},
		{Channel: StreamChannelCandles, Symbol: "btcusdt", CandleInterval: "3min"},
		{Channel: StreamChannelBBO, Symbol: "btcusdt", DepthType: DepthStep0},
	}
	for _, subscription := range invalid {
		_, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{subscription}})
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("PublicStream(%+v) error = %v, want validation", subscription, err)
		}
	}
	_, err := client.PublicStream(
		StreamRequest{Subscriptions: []StreamSubscription{{
			Channel: StreamChannelTicker, Symbol: "btcusdt",
		}}},
		trade.WithTimeout(time.Second),
	)
	if err == nil {
		t.Fatal("PublicStream() timeout error = nil")
	}
}

func newTestHTXStreamClient(
	t *testing.T,
	connector corestream.Connector,
) *StreamClient {
	t.Helper()
	client, err := NewStreamClient(StreamClientConfig{
		Connector: connector, DefaultEgressRouteID: "route-a",
		PublicWebSocketURL: "ws://stream.example.test/ws", AllowInsecureWebSocket: true,
		Backoff: func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client
}

func gzipHTXStreamMessage(t *testing.T, payload string) corestream.Message {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("gzip.Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip.Close() error = %v", err)
	}
	return corestream.Message{Type: corestream.MessageBinary, Data: compressed.Bytes()}
}

func waitForHTXWebSocketWrite(
	t *testing.T,
	connection *htxWebSocketTestConnection,
) corestream.Message {
	t.Helper()
	select {
	case message := <-connection.writes:
		return message
	case <-time.After(2 * time.Second):
		t.Fatal("HTX WebSocket write timed out")
		return corestream.Message{}
	}
}

func decodeHTXStreamCommand(t *testing.T, message corestream.Message) htxStreamCommand {
	t.Helper()
	if message.Type != corestream.MessageText {
		t.Fatalf("command frame type = %d, want text", message.Type)
	}
	var command htxStreamCommand
	if err := json.Unmarshal(message.Data, &command); err != nil {
		t.Fatalf("decode command: %v", err)
	}
	if command.ID == "" {
		t.Fatal("command ID is empty")
	}
	return command
}

func assertHTXStreamCommand(
	t *testing.T,
	command htxStreamCommand,
	operation string,
	topic string,
) {
	t.Helper()
	if operation == "subscribe" &&
		(command.Subscribe != topic || command.Unsubscribe != "") {
		t.Fatalf("subscribe command = %+v", command)
	}
	if operation == "unsubscribe" &&
		(command.Unsubscribe != topic || command.Subscribe != "") {
		t.Fatalf("unsubscribe command = %+v", command)
	}
}
