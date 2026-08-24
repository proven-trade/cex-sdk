package bybit

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	"github.com/proven-trade/proven-trade-sdk/model"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type wsReadResult struct {
	message corestream.Message
	err     error
}

type wsTestConnection struct {
	reads     chan wsReadResult
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newWSTestConnection() *wsTestConnection {
	return &wsTestConnection{
		reads: make(chan wsReadResult, 16), writes: make(chan []byte, 16), closed: make(chan struct{}),
	}
}

func (connection *wsTestConnection) Read(ctx context.Context) (corestream.Message, error) {
	select {
	case <-ctx.Done():
		return corestream.Message{}, ctx.Err()
	case <-connection.closed:
		return corestream.Message{}, corestream.ErrSessionClosed
	case result := <-connection.reads:
		return result.message, result.err
	}
}

func (connection *wsTestConnection) Write(ctx context.Context, message corestream.Message) error {
	payload := append([]byte(nil), message.Data...)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-connection.closed:
		return corestream.ErrSessionClosed
	case connection.writes <- payload:
		return nil
	}
}

func (connection *wsTestConnection) Ping(context.Context) error { return nil }

func (connection *wsTestConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type wsTestConnector struct {
	mu          sync.Mutex
	connections []*wsTestConnection
	routes      []transport.EgressRouteID
	requests    []corestream.DialRequest
}

func (connector *wsTestConnector) Connect(
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
		return nil, errors.New("no test connection")
	}
	connection := connector.connections[0]
	connector.connections = connector.connections[1:]
	return connection, nil
}

func (connector *wsTestConnector) snapshot() ([]transport.EgressRouteID, []corestream.DialRequest) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	return slices.Clone(connector.routes), slices.Clone(connector.requests)
}

type wsCredentialProvider struct {
	mu         sync.Mutex
	calls      int
	lastAPIKey []byte
	lastSecret []byte
}

func (provider *wsCredentialProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.calls++
	material := credential.Material{
		APIKey: []byte("test-api-key"), SecretKey: []byte("test-secret-key"),
	}
	provider.lastAPIKey = material.APIKey
	provider.lastSecret = material.SecretKey
	return material, nil
}

func (provider *wsCredentialProvider) snapshot() (int, []byte, []byte) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls, slices.Clone(provider.lastAPIKey), slices.Clone(provider.lastSecret)
}

func TestPublicStreamKeepsRouteCategoryAndTopicsAcrossReconnect(t *testing.T) {
	t.Parallel()

	first := newWSTestConnection()
	second := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{first, second}}
	client := newTestBybitStreamClient(t, connector, nil, time.Now, nil)
	ticker, err := TickerStreamTopic(CategorySpot, "BTCUSDT")
	if err != nil {
		t.Fatalf("TickerStreamTopic() error = %v", err)
	}
	trades, err := PublicTradeStreamTopic(CategorySpot, "BTCUSDT")
	if err != nil {
		t.Fatalf("PublicTradeStreamTopic() error = %v", err)
	}
	public, err := client.PublicStream(
		PublicStreamRequest{Category: CategorySpot, Topics: []string{ticker}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var got StreamTicker
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Operation != "" || message.Pong {
				return nil
			}
			if err := message.DecodeData(&got); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	assertStreamOperation(t, waitForWSWrite(t, first), "subscribe", []string{ticker})
	if err := public.Subscribe(context.Background(), trades); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	assertStreamOperation(t, waitForWSWrite(t, first), "subscribe", []string{trades})
	first.reads <- wsReadResult{err: errors.New("connection lost")}
	assertStreamOperation(t, waitForWSWrite(t, second), "subscribe", []string{trades, ticker})
	second.reads <- wsReadResult{message: corestream.Message{Type: corestream.MessageText, Data: []byte(
		`{"topic":"tickers.BTCUSDT","type":"snapshot","ts":2000,"data":{"symbol":"BTCUSDT","lastPrice":"64000","bid1Price":"63999","ask1Price":"64001"}}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("public Run() did not finish")
	}
	if got.LastPrice != "64000" || got.BidPrice != "63999" {
		t.Fatalf("ticker data = %+v", got)
	}
	if public.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", public.Generation())
	}
	if err := public.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) {
		t.Fatalf("routes = %v", routes)
	}
	if len(requests) != 2 || requests[0].Endpoint != "ws://public.example.test/v5/public/spot" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestPrivateStreamReauthenticatesAndResubscribesAfterReconnect(t *testing.T) {
	t.Parallel()

	first := newWSTestConnection()
	second := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{first, second}}
	provider := &wsCredentialProvider{}
	times := []time.Time{time.UnixMilli(1000), time.UnixMilli(2000)}
	var timeMu sync.Mutex
	now := func() time.Time {
		timeMu.Lock()
		defer timeMu.Unlock()
		value := times[0]
		times = times[1:]
		return value
	}
	client := newTestBybitStreamClient(
		t, connector, provider, now, []transport.EgressRouteID{"route-a", "route-b"},
	)
	private, err := client.PrivateStream(
		PrivateStreamRequest{Topics: []string{"order.linear"}}, trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var got []StreamOrder
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Operation != "" || message.Pong {
				return nil
			}
			if err := message.DecodeData(&got); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	assertAuthenticationRequest(t, waitForWSWrite(t, first), 11000)
	first.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"success":true,"ret_msg":"","op":"auth","conn_id":"first"}`,
	)}}
	assertStreamOperation(t, waitForWSWrite(t, first), "subscribe", []string{"order.linear"})
	first.reads <- wsReadResult{err: errors.New("connection lost")}
	assertAuthenticationRequest(t, waitForWSWrite(t, second), 12000)
	second.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"success":true,"ret_msg":"","op":"auth","conn_id":"second"}`,
	)}}
	assertStreamOperation(t, waitForWSWrite(t, second), "subscribe", []string{"order.linear"})
	second.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"topic":"order.linear","creationTime":2001,"data":[{"category":"linear","orderId":"42","symbol":"BTCUSDT","orderStatus":"New","qty":"0.01"}]}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("private Run() did not finish")
	}
	if len(got) != 1 || got[0].OrderID != "42" || got[0].Category != CategoryLinear {
		t.Fatalf("order data = %+v", got)
	}
	calls, apiKey, secret := provider.snapshot()
	if calls != 2 {
		t.Fatalf("provider calls = %d, want 2", calls)
	}
	if !allZero(apiKey) || !allZero(secret) {
		t.Fatal("resolved WebSocket credential byte slices were not overwritten")
	}
	if private.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", private.Generation())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) ||
		len(requests) != 2 || requests[0].Endpoint != "ws://private.example.test/v5/private" {
		t.Fatalf("routes = %v, requests = %+v", routes, requests)
	}
}

func TestPrivateStreamRejectsRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &wsCredentialProvider{}
	client := newTestBybitStreamClient(
		t, &wsTestConnector{}, provider, time.Now, []transport.EgressRouteID{"route-a"},
	)
	_, err := client.PrivateStream(
		PrivateStreamRequest{Topics: []string{"wallet"}}, trade.WithEgressRoute("route-b"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("PrivateStream() error = %v, want authorization error", err)
	}
	calls, _, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestPrivateStreamDoesNotReconnectRejectedAuthentication(t *testing.T) {
	t.Parallel()

	connection := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{connection}}
	client := newTestBybitStreamClient(
		t, connector, &wsCredentialProvider{}, time.Now, []transport.EgressRouteID{"route-a"},
	)
	private, err := client.PrivateStream(PrivateStreamRequest{Topics: []string{"wallet"}})
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- private.Run(context.Background(), func(context.Context, StreamMessage) error { return nil })
	}()
	_ = waitForWSWrite(t, connection)
	connection.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"success":false,"ret_msg":"invalid key","op":"auth","conn_id":"rejected"}`,
	)}}
	select {
	case runErr := <-done:
		var authError *StreamAuthError
		if !errors.As(runErr, &authError) || authError.Message != "invalid key" {
			t.Fatalf("Run() error = %v, want StreamAuthError", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("rejected authentication did not stop")
	}
	if routes, _ := connector.snapshot(); len(routes) != 1 {
		t.Fatalf("connect attempts = %d, want 1", len(routes))
	}
}

func TestBybitApplicationHeartbeatWaitsForPong(t *testing.T) {
	t.Parallel()

	underlying := newWSTestConnection()
	connection := &bybitConnection{next: underlying, pong: make(chan struct{}, 1)}
	done := make(chan error, 1)
	go func() { done <- connection.Ping(context.Background()) }()
	if payload := waitForWSWrite(t, underlying); string(payload) != `{"op":"ping"}` {
		t.Fatalf("heartbeat payload = %q", payload)
	}
	underlying.reads <- wsReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"success":true,"ret_msg":"pong","conn_id":"heartbeat","op":"ping"}`),
	}}
	message, err := connection.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	select {
	case pingErr := <-done:
		if pingErr != nil {
			t.Fatalf("Ping() error = %v", pingErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Ping() did not observe pong")
	}
	decoded, err := DecodeStreamMessage(message)
	if err != nil || !decoded.Pong {
		t.Fatalf("DecodeStreamMessage(pong) = %+v, %v", decoded, err)
	}
}

func TestSpotSubscriptionsAreSplitIntoTenTopicBatches(t *testing.T) {
	t.Parallel()

	topics := make([]string, 11)
	for index := range topics {
		topics[index] = "tickers.SYMBOL" + strconv.Itoa(index)
	}
	validated, err := validatePublicTopics(CategorySpot, topics)
	if err != nil {
		t.Fatalf("validatePublicTopics() error = %v", err)
	}
	connection := newWSTestConnection()
	managed := newManagedStream(CategorySpot, validated, false, time.Nanosecond)
	if err := managed.resubscribe(context.Background(), connection); err != nil {
		t.Fatalf("resubscribe() error = %v", err)
	}
	first := waitForWSWrite(t, connection)
	second := waitForWSWrite(t, connection)
	var firstRequest, secondRequest streamOperation
	if err := json.Unmarshal(first, &firstRequest); err != nil {
		t.Fatalf("decode first subscription: %v", err)
	}
	if err := json.Unmarshal(second, &secondRequest); err != nil {
		t.Fatalf("decode second subscription: %v", err)
	}
	if len(firstRequest.Arguments) != 10 || len(secondRequest.Arguments) != 1 {
		t.Fatalf(
			"subscription batch sizes = %d, %d, want 10, 1",
			len(firstRequest.Arguments),
			len(secondRequest.Arguments),
		)
	}
}

func TestDecodeTypedStreamData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		check   func(t *testing.T, message StreamMessage)
	}{
		{
			name:    "orderbook",
			payload: `{"topic":"orderbook.50.BTCUSDT","type":"delta","ts":1000,"data":{"s":"BTCUSDT","b":[["64000","1"]],"a":[["64001","2"]],"u":2,"seq":3,"cts":999}}`,
			check: func(t *testing.T, message StreamMessage) {
				var value StreamOrderBook
				if err := message.DecodeData(&value); err != nil || value.Sequence != 3 || value.Bids[0][1] != "1" {
					t.Fatalf("orderbook = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "public trade",
			payload: `{"topic":"publicTrade.BTCUSDT","type":"snapshot","ts":1000,"data":[{"T":999,"s":"BTCUSDT","S":"Buy","v":"0.1","p":"64000","i":"exec-1"}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var value []StreamPublicTrade
				if err := message.DecodeData(&value); err != nil || len(value) != 1 || value[0].ExecutionID != "exec-1" {
					t.Fatalf("trades = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "kline",
			payload: `{"topic":"kline.1.BTCUSDT","type":"snapshot","ts":1000,"data":[{"start":900,"end":959,"interval":"1","open":"1","close":"2","high":"3","low":"0.5","volume":"10","turnover":"20","confirm":true,"timestamp":999}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var value []StreamKline
				if err := message.DecodeData(&value); err != nil || len(value) != 1 || !value[0].Confirmed || value[0].Close != "2" {
					t.Fatalf("kline = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "execution",
			payload: `{"topic":"execution.linear","creationTime":1000,"data":[{"category":"linear","symbol":"BTCUSDT","orderId":"42","execId":"exec-1","execPrice":"64000","execQty":"0.01","isMaker":true,"seq":9}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var value []StreamExecution
				if err := message.DecodeData(&value); err != nil || len(value) != 1 || !value[0].Maker || value[0].Sequence != 9 {
					t.Fatalf("execution = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "position",
			payload: `{"topic":"position.linear","creationTime":1000,"data":[{"category":"linear","symbol":"BTCUSDT","side":"Buy","size":"0.01","entryPrice":"64000","seq":10}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var value []StreamPosition
				if err := message.DecodeData(&value); err != nil || len(value) != 1 || value[0].AveragePrice != "64000" {
					t.Fatalf("position = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "wallet",
			payload: `{"topic":"wallet","creationTime":1000,"data":[{"accountType":"UNIFIED","totalEquity":"1000","coin":[{"coin":"USDT","walletBalance":"900"}]}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var value []StreamWallet
				if err := message.DecodeData(&value); err != nil || len(value) != 1 || value[0].Coins[0].WalletBalance != "900" {
					t.Fatalf("wallet = %+v, error = %v", value, err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message, err := DecodeStreamMessage(corestream.Message{Data: []byte(test.payload)})
			if err != nil || len(message.Raw) == 0 {
				t.Fatalf("DecodeStreamMessage() = %+v, %v", message, err)
			}
			test.check(t, message)
		})
	}
}

func TestStreamTopicValidation(t *testing.T) {
	t.Parallel()

	book, err := OrderBookStreamTopic(CategoryLinear, "BTCUSDT", 1000)
	if err != nil || book != "orderbook.1000.BTCUSDT" {
		t.Fatalf("OrderBookStreamTopic() = %q, %v", book, err)
	}
	kline, err := KlineStreamTopic(CategorySpot, "BTCUSDT", Candle15Minutes)
	if err != nil || kline != "kline.15.BTCUSDT" {
		t.Fatalf("KlineStreamTopic() = %q, %v", kline, err)
	}
	invalid := []error{}
	_, err = OrderBookStreamTopic(CategorySpot, "BTCUSDT", 500)
	invalid = append(invalid, err)
	_, err = TickerStreamTopic(CategorySpot, "btcusdt")
	invalid = append(invalid, err)
	_, err = validatePrivateTopics([]string{"order", "order.linear"})
	invalid = append(invalid, err)
	for _, item := range invalid {
		if !errors.Is(item, trade.ErrValidation) {
			t.Fatalf("topic validation error = %v", item)
		}
	}
}

func waitForWSWrite(t *testing.T, connection *wsTestConnection) []byte {
	t.Helper()
	select {
	case payload := <-connection.writes:
		return payload
	case <-time.After(time.Second):
		t.Fatal("WebSocket write was not observed")
		return nil
	}
}

func assertStreamOperation(t *testing.T, payload []byte, operation string, topics []string) {
	t.Helper()
	var request streamOperation
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode stream operation: %v", err)
	}
	if request.RequestID == "" || request.Operation != operation || !slices.Equal(request.Arguments, topics) {
		t.Fatalf("stream operation = %+v, want %s %v", request, operation, topics)
	}
}

func assertAuthenticationRequest(t *testing.T, payload []byte, expires int64) {
	t.Helper()
	var request struct {
		Operation string            `json:"op"`
		Arguments []json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode authentication request: %v", err)
	}
	if request.Operation != "auth" || len(request.Arguments) != 3 {
		t.Fatalf("authentication request = %+v", request)
	}
	var apiKey, signature string
	var gotExpires int64
	if err := json.Unmarshal(request.Arguments[0], &apiKey); err != nil {
		t.Fatalf("decode API key: %v", err)
	}
	if err := json.Unmarshal(request.Arguments[1], &gotExpires); err != nil {
		t.Fatalf("decode expires: %v", err)
	}
	if err := json.Unmarshal(request.Arguments[2], &signature); err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if apiKey != "test-api-key" || gotExpires != expires {
		t.Fatalf("authentication arguments = %s, %d", apiKey, gotExpires)
	}
	want, err := SignHMACSHA256(
		[]byte("test-secret-key"), []byte("GET/realtime"+strconv.FormatInt(expires, 10)),
	)
	if err != nil {
		t.Fatalf("SignHMACSHA256() error = %v", err)
	}
	if signature != want {
		t.Fatalf("signature = %q, want %q", signature, want)
	}
}

func newTestBybitStreamClient(
	t *testing.T,
	connector corestream.Connector,
	provider credential.Provider,
	now func() time.Time,
	allowedRoutes []transport.EgressRouteID,
) *StreamClient {
	t.Helper()
	config := StreamClientConfig{
		Connector: connector, DefaultEgressRouteID: "route-a",
		SpotPublicStreamURL:    "ws://public.example.test/v5/public/spot",
		LinearPublicStreamURL:  "ws://public.example.test/v5/public/linear",
		PrivateStreamURL:       "ws://private.example.test/v5/private",
		AllowInsecureWebSocket: true, Now: now,
		Backoff:      func(int) time.Duration { return 0 },
		PingInterval: time.Hour, PingTimeout: time.Second,
		AuthenticationTimeout: time.Second, AuthenticationWindow: 10 * time.Second,
		SubscriptionInterval: time.Nanosecond,
	}
	if provider != nil {
		config.Credentials = &credential.Descriptor{
			AccountID: "bybit-main", Exchange: model.ExchangeBybit, SecretRef: "secret/bybit",
			Permissions:           []credential.Permission{credential.PermissionRead},
			AllowedEgressRouteIDs: allowedRoutes,
		}
		config.CredentialProvider = provider
	}
	client, err := NewStreamClient(config)
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client
}
