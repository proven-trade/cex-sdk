package binance

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/model"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

type streamReadResult struct {
	message corestream.Message
	err     error
}

type testStreamConnection struct {
	reads     chan streamReadResult
	written   chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newTestStreamConnection() *testStreamConnection {
	return &testStreamConnection{
		reads:   make(chan streamReadResult, 8),
		written: make(chan []byte, 8),
		closed:  make(chan struct{}),
	}
}

func (connection *testStreamConnection) Read(ctx context.Context) (corestream.Message, error) {
	select {
	case <-ctx.Done():
		return corestream.Message{}, ctx.Err()
	case <-connection.closed:
		return corestream.Message{}, corestream.ErrSessionClosed
	case result := <-connection.reads:
		return result.message, result.err
	}
}

func (connection *testStreamConnection) Write(ctx context.Context, message corestream.Message) error {
	payload := append([]byte(nil), message.Data...)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-connection.closed:
		return corestream.ErrSessionClosed
	case connection.written <- payload:
		return nil
	}
}

func (connection *testStreamConnection) Ping(context.Context) error { return nil }

func (connection *testStreamConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type testStreamConnector struct {
	mu          sync.Mutex
	connections []*testStreamConnection
	routes      []transport.EgressRouteID
	requests    []corestream.DialRequest
}

func (connector *testStreamConnector) Connect(
	_ context.Context,
	routeID transport.EgressRouteID,
	request corestream.DialRequest,
) (corestream.Connection, error) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	connector.routes = append(connector.routes, routeID)
	connector.requests = append(connector.requests, corestream.DialRequest{
		Endpoint: request.Endpoint,
		Header:   request.Header.Clone(),
	})
	if len(connector.connections) == 0 {
		return nil, errors.New("no test connection")
	}
	connection := connector.connections[0]
	connector.connections = connector.connections[1:]
	return connection, nil
}

func (connector *testStreamConnector) snapshot() ([]transport.EgressRouteID, []corestream.DialRequest) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	routes := slices.Clone(connector.routes)
	requests := make([]corestream.DialRequest, len(connector.requests))
	for index, request := range connector.requests {
		requests[index] = corestream.DialRequest{Endpoint: request.Endpoint, Header: request.Header.Clone()}
	}
	return routes, requests
}

type streamCredentialProvider struct {
	mu    sync.Mutex
	calls int
}

func (provider *streamCredentialProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.mu.Lock()
	provider.calls++
	provider.mu.Unlock()
	return credential.Material{
		APIKey:    []byte("test-api-key"),
		SecretKey: []byte("test-secret-key"),
	}, nil
}

func (provider *streamCredentialProvider) count() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls
}

func TestMarketStreamUsesSelectedRouteAndResubscribesCurrentSet(t *testing.T) {
	t.Parallel()

	first := newTestStreamConnection()
	second := newTestStreamConnection()
	connector := &testStreamConnector{connections: []*testStreamConnection{first, second}}
	client, err := NewStreamClient(StreamClientConfig{
		Connector:              connector,
		DefaultEgressRouteID:   "route-a",
		MarketStreamURL:        "ws://stream.example.test/stream",
		WebSocketAPIURL:        "ws://api.example.test/ws-api/v3",
		AllowInsecureWebSocket: true,
		Backoff:                func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	market, err := client.MarketStream(MarketStreamRequest{
		Streams:  []string{"btcusdt@trade", "btcusdt@bookTicker"},
		TimeUnit: StreamTimeMicroseconds,
	}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("MarketStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var gotEvent AggregateTradeEvent
	go func() {
		done <- market.Run(ctx, func(_ context.Context, message MarketStreamMessage) error {
			if message.Response != nil {
				return nil
			}
			if err := message.Decode(&gotEvent); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()

	initial := waitForStreamWrite(t, first)
	assertMarketCommand(t, initial, "SUBSCRIBE", []string{"btcusdt@bookTicker", "btcusdt@trade"})
	if err := market.Subscribe(context.Background(), "btcusdt@aggTrade"); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	added := waitForStreamWrite(t, first)
	assertMarketCommand(t, added, "SUBSCRIBE", []string{"btcusdt@aggTrade"})
	first.reads <- streamReadResult{err: errors.New("connection lost")}
	reconnected := waitForStreamWrite(t, second)
	assertMarketCommand(t, reconnected, "SUBSCRIBE", []string{
		"btcusdt@aggTrade",
		"btcusdt@bookTicker",
		"btcusdt@trade",
	})
	if err := market.Unsubscribe(context.Background(), "btcusdt@bookTicker"); err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
	removed := waitForStreamWrite(t, second)
	assertMarketCommand(t, removed, "UNSUBSCRIBE", []string{"btcusdt@bookTicker"})
	second.reads <- streamReadResult{message: corestream.Message{Type: corestream.MessageText, Data: []byte(
		`{"stream":"btcusdt@aggTrade","data":{"e":"aggTrade","E":1000,"s":"BTCUSDT","a":7,"p":"64000","q":"0.1","f":11,"l":12,"T":999,"m":true,"M":false}}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("market stream Run() did not finish")
	}
	if gotEvent.Symbol != "BTCUSDT" || gotEvent.AggregateID != 7 || gotEvent.Price != "64000" {
		t.Fatalf("aggregate trade = %+v", gotEvent)
	}
	if market.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", market.Generation())
	}
	if err := market.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) {
		t.Fatalf("routes = %v", routes)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	for _, request := range requests {
		parsed, parseErr := url.Parse(request.Endpoint)
		if parseErr != nil || parsed.Query().Get("timeUnit") != "MICROSECOND" {
			t.Fatalf("stream endpoint = %q, error = %v", request.Endpoint, parseErr)
		}
	}
}

func TestUserDataStreamSignsEveryConnectionAndDecodesExecution(t *testing.T) {
	t.Parallel()

	first := newTestStreamConnection()
	second := newTestStreamConnection()
	connector := &testStreamConnector{connections: []*testStreamConnection{first, second}}
	provider := &streamCredentialProvider{}
	times := []time.Time{time.UnixMilli(1000), time.UnixMilli(2000)}
	var timeMu sync.Mutex
	now := func() time.Time {
		timeMu.Lock()
		defer timeMu.Unlock()
		value := times[0]
		times = times[1:]
		return value
	}
	client := newTestBinanceStreamClient(t, connector, provider, now, []transport.EgressRouteID{"route-a", "route-b"})
	userData, err := client.UserDataStream(trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("UserDataStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var execution ExecutionReportEvent
	go func() {
		done <- userData.Run(ctx, func(_ context.Context, message UserDataStreamMessage) error {
			if message.Response != nil {
				return nil
			}
			if err := message.Decode(&execution); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()

	firstSubscription := waitForStreamWrite(t, first)
	assertUserDataSignature(t, firstSubscription, 1000)
	first.reads <- streamReadResult{err: errors.New("connection lost")}
	secondSubscription := waitForStreamWrite(t, second)
	assertUserDataSignature(t, secondSubscription, 2000)
	second.reads <- streamReadResult{message: corestream.Message{Type: corestream.MessageText, Data: []byte(
		`{"id":"user-data-subscribe","status":200,"result":{"subscriptionId":0}}`,
	)}}
	second.reads <- streamReadResult{message: corestream.Message{Type: corestream.MessageText, Data: []byte(
		`{"subscriptionId":0,"event":{"e":"executionReport","E":2001,"s":"BTCUSDT","c":"strategy-1","S":"BUY","o":"LIMIT","f":"GTC","q":"0.1","p":"64000","x":"TRADE","X":"PARTIALLY_FILLED","i":42,"l":"0.01","z":"0.01","L":"64000","n":"0.1","N":"USDT","T":2000,"t":9}}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("user data stream Run() did not finish")
	}
	if execution.OrderID != 42 || execution.OrderStatus != OrderStatusPartiallyFilled || execution.TradeID != 9 {
		t.Fatalf("execution report = %+v", execution)
	}
	if userData.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", userData.Generation())
	}
	if err := userData.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if provider.count() != 2 {
		t.Fatalf("credential provider calls = %d, want 2", provider.count())
	}
	routes, _ := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) {
		t.Fatalf("routes = %v", routes)
	}
}

func TestUserDataStreamRejectsCredentialRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	connector := &testStreamConnector{}
	provider := &streamCredentialProvider{}
	client := newTestBinanceStreamClient(t, connector, provider, time.Now, []transport.EgressRouteID{"route-a"})
	_, err := client.UserDataStream(trade.WithEgressRoute("route-b"))
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("UserDataStream() error = %v, want authorization error", err)
	}
	if provider.count() != 0 {
		t.Fatalf("credential provider calls = %d, want 0", provider.count())
	}
}

func TestDecodeWebSocketResponsesAndEvents(t *testing.T) {
	t.Parallel()

	marketResponse, err := DecodeMarketStreamMessage(corestream.Message{Data: []byte(`{"result":null,"id":1}`)})
	if err != nil || marketResponse.Response == nil || string(marketResponse.Response.ID) != "1" {
		t.Fatalf("market response = %+v, error = %v", marketResponse, err)
	}
	userResponse, err := DecodeUserDataStreamMessage(corestream.Message{Data: []byte(
		`{"id":"request-1","status":400,"error":{"code":-2015,"msg":"invalid key"}}`,
	)})
	if err != nil || userResponse.Response == nil || userResponse.Response.Error == nil || userResponse.Response.Error.Code != -2015 {
		t.Fatalf("user response = %+v, error = %v", userResponse, err)
	}
	userEvent, err := DecodeUserDataStreamMessage(corestream.Message{Data: []byte(
		`{"subscriptionId":3,"event":{"e":"balanceUpdate","E":10,"a":"BTC","d":"1","T":9}}`,
	)})
	if err != nil || userEvent.SubscriptionID != 3 || userEvent.EventType != "balanceUpdate" {
		t.Fatalf("user event = %+v, error = %v", userEvent, err)
	}
	var balance BalanceUpdateEvent
	if err := userEvent.Decode(&balance); err != nil || balance.Asset != "BTC" || balance.Delta != "1" {
		t.Fatalf("balance update = %+v, error = %v", balance, err)
	}
	if _, err := DecodeMarketStreamMessage(corestream.Message{Data: []byte(`not-json`)}); err == nil {
		t.Fatal("DecodeMarketStreamMessage(invalid) error = nil")
	}
}

func TestMarketStreamNameHelpers(t *testing.T) {
	t.Parallel()

	tradeStream, err := SymbolMarketStream("BTCUSDT", "aggTrade")
	if err != nil || tradeStream != "btcusdt@aggTrade" {
		t.Fatalf("SymbolMarketStream() = %q, %v", tradeStream, err)
	}
	klineStream, err := KlineMarketStream("BTCUSDT", Kline1Minute)
	if err != nil || klineStream != "btcusdt@kline_1m" {
		t.Fatalf("KlineMarketStream() = %q, %v", klineStream, err)
	}
	depthStream, err := PartialDepthMarketStream("BTCUSDT", 20, 100*time.Millisecond)
	if err != nil || depthStream != "btcusdt@depth20@100ms" {
		t.Fatalf("PartialDepthMarketStream() = %q, %v", depthStream, err)
	}
	if _, err := PartialDepthMarketStream("BTCUSDT", 15, 0); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("PartialDepthMarketStream(invalid) error = %v", err)
	}
}

func waitForStreamWrite(t *testing.T, connection *testStreamConnection) []byte {
	t.Helper()
	select {
	case payload := <-connection.written:
		return payload
	case <-time.After(time.Second):
		t.Fatal("WebSocket write was not observed")
		return nil
	}
}

func assertMarketCommand(t *testing.T, payload []byte, method string, streams []string) {
	t.Helper()
	var command marketControlRequest
	if err := json.Unmarshal(payload, &command); err != nil {
		t.Fatalf("decode market command: %v", err)
	}
	if command.Method != method || !slices.Equal(command.Params, streams) || command.ID == 0 {
		t.Fatalf("market command = %+v", command)
	}
}

func assertUserDataSignature(t *testing.T, payload []byte, timestamp int64) {
	t.Helper()
	var request userDataSubscribeRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode user data subscription: %v", err)
	}
	if request.Method != "userDataStream.subscribe.signature" || request.Params.APIKey != "test-api-key" || request.Params.Timestamp != timestamp {
		t.Fatalf("user data subscription = %+v", request)
	}
	values := url.Values{}
	values.Set("apiKey", request.Params.APIKey)
	values.Set("recvWindow", "5000")
	values.Set("timestamp", strconv.FormatInt(timestamp, 10))
	want, err := SignHMACSHA256([]byte("test-secret-key"), []byte(values.Encode()))
	if err != nil {
		t.Fatalf("SignHMACSHA256() error = %v", err)
	}
	if request.Params.Signature != want {
		t.Fatalf("signature = %q, want %q", request.Params.Signature, want)
	}
}

func newTestBinanceStreamClient(
	t *testing.T,
	connector corestream.Connector,
	provider credential.Provider,
	now func() time.Time,
	allowedRoutes []transport.EgressRouteID,
) *StreamClient {
	t.Helper()
	client, err := NewStreamClient(StreamClientConfig{
		Connector: connector,
		Credentials: &credential.Descriptor{
			AccountID:             "binance-main",
			Exchange:              model.ExchangeBinance,
			SecretRef:             "secret/binance",
			Permissions:           []credential.Permission{credential.PermissionRead},
			AllowedEgressRouteIDs: allowedRoutes,
		},
		CredentialProvider:     provider,
		DefaultEgressRouteID:   "route-a",
		MarketStreamURL:        "ws://stream.example.test/stream",
		WebSocketAPIURL:        "ws://api.example.test/ws-api/v3",
		AllowInsecureWebSocket: true,
		Now:                    now,
		Backoff:                func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client
}
