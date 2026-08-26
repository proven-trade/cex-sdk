package mexc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

type mexcWebSocketReadResult struct {
	message corestream.Message
	err     error
}

type mexcWebSocketTestConnection struct {
	reads     chan mexcWebSocketReadResult
	writes    chan corestream.Message
	closed    chan struct{}
	closeOnce sync.Once
}

func newMEXCWebSocketTestConnection() *mexcWebSocketTestConnection {
	return &mexcWebSocketTestConnection{
		reads:  make(chan mexcWebSocketReadResult, 16),
		writes: make(chan corestream.Message, 32),
		closed: make(chan struct{}),
	}
}

func (connection *mexcWebSocketTestConnection) Read(
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

func (connection *mexcWebSocketTestConnection) Write(
	ctx context.Context,
	message corestream.Message,
) error {
	copyMessage := corestream.Message{
		Type: message.Type, Data: append([]byte(nil), message.Data...),
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-connection.closed:
		return corestream.ErrSessionClosed
	case connection.writes <- copyMessage:
		return nil
	}
}

func (connection *mexcWebSocketTestConnection) Ping(context.Context) error { return nil }

func (connection *mexcWebSocketTestConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type mexcWebSocketTestConnector struct {
	mu          sync.Mutex
	connections []*mexcWebSocketTestConnection
	routes      []transport.EgressRouteID
	requests    []corestream.DialRequest
}

func (connector *mexcWebSocketTestConnector) Connect(
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
		return nil, errors.New("no MEXC test connection")
	}
	connection := connector.connections[0]
	connector.connections = connector.connections[1:]
	return connection, nil
}

func (connector *mexcWebSocketTestConnector) snapshot() (
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

type mexcStreamCommand struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
}

func TestMEXCPublicStreamRestoresProtobufSubscriptionOnSelectedRoute(t *testing.T) {
	t.Parallel()
	first := newMEXCWebSocketTestConnection()
	second := newMEXCWebSocketTestConnection()
	connector := &mexcWebSocketTestConnector{
		connections: []*mexcWebSocketTestConnection{first, second},
	}
	client := newTestMEXCStreamClient(t, connector, nil, 0, 0)
	subscription, err := AggregateTradesStream("BTCUSDT", StreamUpdate100Millis)
	if err != nil {
		t.Fatalf("AggregateTradesStream() error = %v", err)
	}
	public, err := client.PublicStream(
		StreamRequest{Subscriptions: []StreamSubscription{subscription}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var received StreamAggregateTrade
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.AggregateTrades == nil || len(message.AggregateTrades.Deals) == 0 {
				return nil
			}
			received = message.AggregateTrades.Deals[0]
			cancel()
			return nil
		})
	}()
	wantChannel := streamSubscriptionName(subscription)
	assertMEXCStreamCommand(
		t, waitForMEXCWebSocketWrite(t, first), "SUBSCRIPTION", wantChannel,
	)
	first.reads <- mexcWebSocketReadResult{err: errors.New("connection lost")}
	assertMEXCStreamCommand(
		t, waitForMEXCWebSocketWrite(t, second), "SUBSCRIPTION", wantChannel,
	)
	tradeBody := protobufTestMessage(
		protobufTestString(1, "64000"), protobufTestString(2, "0.1"),
		protobufTestVarint(3, 1), protobufTestVarint(4, 1_700_000_000_000),
		protobufTestString(5, "trade-1"),
	)
	second.reads <- mexcWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageBinary,
		Data: protobufTestMessage(
			protobufTestString(1, wantChannel),
			protobufTestBytes(314, protobufTestBytes(1, tradeBody)),
			protobufTestString(3, "BTCUSDT"),
		),
	}}
	waitForMEXCStreamRun(t, done, context.Canceled)
	if received.TradeID != "trade-1" || received.Price != "64000" {
		t.Fatalf("received trade = %+v", received)
	}
	if public.Generation() != 2 || public.EgressRouteID() != "route-b" {
		t.Fatalf(
			"generation = %d, route = %q", public.Generation(), public.EgressRouteID(),
		)
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

func TestMEXCStreamSendsApplicationPingAndRollsBackRejectedSubscription(t *testing.T) {
	t.Parallel()
	connection := newMEXCWebSocketTestConnection()
	connector := &mexcWebSocketTestConnector{
		connections: []*mexcWebSocketTestConnection{connection},
	}
	client := newTestMEXCStreamClient(t, connector, nil, 10*time.Millisecond, 0)
	initial, _ := AggregateTradesStream("BTCUSDT", StreamUpdate100Millis)
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{initial}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	controls := make(chan int, 2)
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Control != nil {
				controls <- message.Control.Code
			}
			return nil
		})
	}()
	assertMEXCStreamCommand(
		t, waitForMEXCWebSocketWrite(t, connection),
		"SUBSCRIPTION", streamSubscriptionName(initial),
	)
	connection.reads <- mexcWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"id":0,"code":0,"msg":""}`),
	}}
	select {
	case code := <-controls:
		if code != 0 {
			t.Fatalf("initial subscription code = %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("initial subscription response was not handled")
	}
	rejected, _ := BookTickerStream("BTCUSDT", StreamUpdate10Millis)
	if err := public.Subscribe(context.Background(), rejected); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	assertMEXCStreamCommand(
		t, waitForMEXCWebSocketWrite(t, connection),
		"SUBSCRIPTION", streamSubscriptionName(rejected),
	)
	connection.reads <- mexcWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"id":0,"code":1,"msg":"invalid channel"}`),
	}}
	select {
	case code := <-controls:
		if code != 1 {
			t.Fatalf("rejected subscription code = %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rejected subscription response was not handled")
	}
	if subscriptions := public.managed.snapshotSubscriptions(); len(subscriptions) != 1 || subscriptions[0] != initial {
		t.Fatalf("subscriptions = %+v", subscriptions)
	}
	for {
		message := waitForMEXCWebSocketWrite(t, connection)
		if message.Type == corestream.MessageText && string(message.Data) == string(pingCommand) {
			break
		}
	}
	cancel()
	waitForMEXCStreamRun(t, done, context.Canceled)
}

func TestMEXCUserDataStreamKeepsListenKeyAndDecodesPrivateOrder(t *testing.T) {
	t.Parallel()
	keepaliveSeen := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path != userDataStreamPath ||
			request.Header.Get("X-MEXC-APIKEY") != "test-api-key" {
			http.Error(writer, `{"code":400,"msg":"invalid request"}`, http.StatusBadRequest)
			return
		}
		switch request.Method {
		case http.MethodPost:
			_, _ = io.WriteString(writer, `{"listenKey":"`+testMEXCListenKey+`"}`)
		case http.MethodPut:
			if request.URL.Query().Get("listenKey") != testMEXCListenKey {
				http.Error(writer, `{"code":400,"msg":"invalid key"}`, http.StatusBadRequest)
				return
			}
			keepaliveSeen <- struct{}{}
			_, _ = io.WriteString(writer, `{"listenKey":"`+testMEXCListenKey+`"}`)
		default:
			http.Error(writer, `{"code":400,"msg":"invalid method"}`, http.StatusBadRequest)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	restClient, _ := newPrivateTestClient(
		t, server.URL, sender, provider,
		[]transport.EgressRouteID{"route-a", "route-b"},
		[]credential.Permission{credential.PermissionRead}, time.Now(),
	)
	connection := newMEXCWebSocketTestConnection()
	connector := &mexcWebSocketTestConnector{
		connections: []*mexcWebSocketTestConnection{connection},
	}
	streamClient := newTestMEXCStreamClient(
		t, connector, restClient, 0, 20*time.Millisecond,
	)
	private, err := streamClient.UserDataStream(
		StreamRequest{Subscriptions: []StreamSubscription{AccountOrdersStream()}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("UserDataStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var received *StreamAccountOrder
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.AccountOrder == nil {
				return nil
			}
			received = message.AccountOrder
			cancel()
			return nil
		})
	}()
	wantChannel := streamSubscriptionName(AccountOrdersStream())
	assertMEXCStreamCommand(
		t, waitForMEXCWebSocketWrite(t, connection), "SUBSCRIPTION", wantChannel,
	)
	select {
	case <-keepaliveSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("listen key keepalive was not sent")
	}
	orderBody := protobufTestMessage(
		protobufTestString(1, "order-1"), protobufTestString(2, "strategy-1"),
		protobufTestString(3, "64000"), protobufTestString(4, "0.1"),
		protobufTestVarint(7, 1), protobufTestVarint(8, 1),
		protobufTestVarint(15, 1), protobufTestVarint(16, 1_700_000_000_000),
	)
	connection.reads <- mexcWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageBinary,
		Data: protobufTestMessage(
			protobufTestString(1, wantChannel), protobufTestBytes(304, orderBody),
			protobufTestString(3, "BTCUSDT"),
		),
	}}
	waitForMEXCStreamRun(t, done, context.Canceled)
	if received == nil || received.ID != "order-1" || received.Status != StreamOrderNew {
		t.Fatalf("private order = %+v", received)
	}
	if private.ListenKey() != testMEXCListenKey || private.EgressRouteID() != "route-b" {
		t.Fatalf("listen key = %q, route = %q", private.ListenKey(), private.EgressRouteID())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b"}) || len(requests) != 1 {
		t.Fatalf("WebSocket routes = %v, requests = %d", routes, len(requests))
	}
	parsed, parseErr := url.Parse(requests[0].Endpoint)
	if parseErr != nil || parsed.Scheme != "ws" || parsed.Host != "stream.example.test" ||
		parsed.Query().Get("listenKey") != testMEXCListenKey {
		t.Fatalf("private endpoint = %q, error = %v", requests[0].Endpoint, parseErr)
	}
	for _, route := range sender.snapshot() {
		if route != "route-b" {
			t.Fatalf("listen key REST route = %q, want route-b", route)
		}
	}
	if calls, apiKey, secret := provider.snapshot(); calls < 2 || !allZero(apiKey) || !allZero(secret) {
		t.Fatalf(
			"provider calls = %d, key zero = %v, secret zero = %v",
			calls, allZero(apiKey), allZero(secret),
		)
	}
	_ = private.Close()
}

func TestMEXCUserDataStreamRejectsRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()
	provider := &recordingProvider{}
	restClient, _ := newPrivateTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"},
		[]credential.Permission{credential.PermissionRead}, time.Now(),
	)
	client := newTestMEXCStreamClient(
		t, &mexcWebSocketTestConnector{}, restClient, 0, 0,
	)
	_, err := client.UserDataStream(
		StreamRequest{Subscriptions: []StreamSubscription{AccountStream()}},
		trade.WithEgressRoute("route-b"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("UserDataStream() error = %v, want authorization", err)
	}
	if calls, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestMEXCStreamValidation(t *testing.T) {
	t.Parallel()
	invalid := []struct {
		subscription StreamSubscription
		private      bool
	}{
		{StreamSubscription{Channel: StreamChannelAggregateTrades, Symbol: "btcusdt", UpdateInterval: StreamUpdate10Millis}, false},
		{StreamSubscription{Channel: StreamChannelAggregateTrades, Symbol: "BTCUSDT"}, false},
		{StreamSubscription{Channel: StreamChannelCandles, Symbol: "BTCUSDT", CandleInterval: "Min3"}, false},
		{StreamSubscription{Channel: StreamChannelPartialDepth, Symbol: "BTCUSDT", Depth: 50}, false},
		{StreamSubscription{Channel: StreamChannelBookTicker, Symbol: "BTCUSDT", UpdateInterval: StreamUpdate10Millis, Depth: 5}, false},
		{StreamSubscription{Channel: StreamChannelAccount, Symbol: "BTCUSDT"}, true},
		{StreamSubscription{Channel: StreamChannelDiffDepth, Symbol: "BTCUSDT", UpdateInterval: StreamUpdate10Millis}, true},
	}
	for index, test := range invalid {
		if err := validateStreamSubscription(test.subscription, test.private); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("invalid subscription %d error = %v, want validation", index, err)
		}
	}
	many := make([]StreamSubscription, maximumStreamSubscriptions+1)
	for index := range many {
		many[index] = StreamSubscription{
			Channel:        StreamChannelAggregateTrades,
			Symbol:         "BTC" + string(rune('A'+index)) + "USDT",
			UpdateInterval: StreamUpdate10Millis,
		}
	}
	if _, err := validateStreamSubscriptions(many, false, true); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("subscription limit error = %v, want validation", err)
	}
}

func newTestMEXCStreamClient(
	t *testing.T,
	connector corestream.Connector,
	restClient *Client,
	pingInterval time.Duration,
	keepaliveInterval time.Duration,
) *StreamClient {
	t.Helper()
	config := StreamClientConfig{
		Connector: connector, RESTClient: restClient, DefaultEgressRouteID: "route-a",
		WebSocketURL: "ws://stream.example.test/ws", AllowInsecureWebSocket: true,
		Backoff: func(int) time.Duration { return 0 }, PingInterval: pingInterval,
		ListenKeyKeepaliveInterval: keepaliveInterval,
	}
	client, err := NewStreamClient(config)
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client
}

func waitForMEXCWebSocketWrite(
	t *testing.T,
	connection *mexcWebSocketTestConnection,
) corestream.Message {
	t.Helper()
	select {
	case message := <-connection.writes:
		return message
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for MEXC WebSocket write")
		return corestream.Message{}
	}
}

func assertMEXCStreamCommand(
	t *testing.T,
	message corestream.Message,
	method string,
	channel string,
) {
	t.Helper()
	var command mexcStreamCommand
	if message.Type != corestream.MessageText || json.Unmarshal(message.Data, &command) != nil ||
		command.Method != method || !slices.Equal(command.Params, []string{channel}) {
		t.Fatalf("stream command = type %d, payload %s", message.Type, message.Data)
	}
}

func waitForMEXCStreamRun(t *testing.T, done <-chan error, want error) {
	t.Helper()
	select {
	case err := <-done:
		if !errors.Is(err, want) {
			t.Fatalf("Run() error = %v, want %v", err, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("MEXC WebSocket Run() did not finish")
	}
}
