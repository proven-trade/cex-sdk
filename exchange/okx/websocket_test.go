package okx

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
	mu             sync.Mutex
	calls          int
	lastAPIKey     []byte
	lastSecret     []byte
	lastPassphrase []byte
}

func (provider *wsCredentialProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.calls++
	material := credential.Material{
		APIKey: []byte("test-api-key"), SecretKey: []byte("test-secret-key"),
		Passphrase: []byte("test-passphrase"),
	}
	provider.lastAPIKey = material.APIKey
	provider.lastSecret = material.SecretKey
	provider.lastPassphrase = material.Passphrase
	return material, nil
}

func (provider *wsCredentialProvider) snapshot() (int, []byte, []byte, []byte) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls, slices.Clone(provider.lastAPIKey), slices.Clone(provider.lastSecret),
		slices.Clone(provider.lastPassphrase)
}

func TestPublicStreamKeepsRouteEndpointAndChannelsAcrossReconnect(t *testing.T) {
	t.Parallel()

	first := newWSTestConnection()
	second := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{first, second}}
	client := newTestOKXStreamClient(t, connector, nil, time.Now, nil)
	ticker, err := PublicStreamArgument("tickers", "BTC-USDT")
	if err != nil {
		t.Fatalf("PublicStreamArgument() error = %v", err)
	}
	trades, err := PublicStreamArgument("trades", "BTC-USDT")
	if err != nil {
		t.Fatalf("PublicStreamArgument() error = %v", err)
	}
	public, err := client.PublicStream(
		PublicStreamRequest{Endpoint: StreamEndpointPublic, Arguments: []StreamArgument{ticker}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var got []Ticker
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Event != "" || message.Pong {
				return nil
			}
			if err := message.DecodeData(&got); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	assertStreamOperation(t, waitForWSWrite(t, first), "subscribe", []StreamArgument{ticker})
	if err := public.Subscribe(context.Background(), trades); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	assertStreamOperation(t, waitForWSWrite(t, first), "subscribe", []StreamArgument{trades})
	first.reads <- wsReadResult{err: errors.New("connection lost")}
	assertStreamOperation(t, waitForWSWrite(t, second), "subscribe", []StreamArgument{ticker, trades})
	second.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"arg":{"channel":"tickers","instId":"BTC-USDT"},"data":[{"instType":"SPOT","instId":"BTC-USDT","last":"64000","bidPx":"63999","askPx":"64001","ts":"2000"}]}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("public Run() did not finish")
	}
	if len(got) != 1 || got[0].LastPrice != "64000" {
		t.Fatalf("ticker data = %+v", got)
	}
	if public.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", public.Generation())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) {
		t.Fatalf("routes = %v", routes)
	}
	if len(requests) != 2 || requests[0].Endpoint != "ws://public.example.test/ws/v5/public" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestBusinessStreamUsesBusinessEndpoint(t *testing.T) {
	t.Parallel()

	connection := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{connection}}
	client := newTestOKXStreamClient(t, connector, nil, time.Now, nil)
	candle, err := CandleStreamArgument("BTC-USDT", Candle1Minute)
	if err != nil {
		t.Fatalf("CandleStreamArgument() error = %v", err)
	}
	stream, err := client.PublicStream(PublicStreamRequest{
		Endpoint: StreamEndpointBusiness, Arguments: []StreamArgument{candle},
	})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- stream.Run(ctx, func(context.Context, StreamMessage) error { return nil })
	}()
	assertStreamOperation(t, waitForWSWrite(t, connection), "subscribe", []StreamArgument{candle})
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("business Run() did not finish")
	}
	_, requests := connector.snapshot()
	if len(requests) != 1 || requests[0].Endpoint != "ws://business.example.test/ws/v5/business" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestPrivateStreamRelogsAndResubscribesAfterReconnect(t *testing.T) {
	t.Parallel()

	first := newWSTestConnection()
	second := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{first, second}}
	provider := &wsCredentialProvider{}
	times := []time.Time{time.Unix(1000, 0), time.Unix(2000, 0)}
	var timeMu sync.Mutex
	now := func() time.Time {
		timeMu.Lock()
		defer timeMu.Unlock()
		value := times[0]
		times = times[1:]
		return value
	}
	client := newTestOKXStreamClient(
		t, connector, provider, now, []transport.EgressRouteID{"route-a", "route-b"},
	)
	orders, err := OrderStreamArgument(InstrumentTypeSwap, "BTC-USDT-SWAP")
	if err != nil {
		t.Fatalf("OrderStreamArgument() error = %v", err)
	}
	private, err := client.PrivateStream(
		PrivateStreamRequest{Arguments: []StreamArgument{orders}}, trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var got []Order
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Event != "" || message.Pong {
				return nil
			}
			if err := message.DecodeData(&got); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	assertLoginRequest(t, waitForWSWrite(t, first), "1000")
	first.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"event":"login","code":"0","msg":"","connId":"first"}`,
	)}}
	assertStreamOperation(t, waitForWSWrite(t, first), "subscribe", []StreamArgument{orders})
	first.reads <- wsReadResult{err: errors.New("connection lost")}
	assertLoginRequest(t, waitForWSWrite(t, second), "2000")
	second.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"event":"login","code":"0","msg":"","connId":"second"}`,
	)}}
	assertStreamOperation(t, waitForWSWrite(t, second), "subscribe", []StreamArgument{orders})
	second.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"arg":{"channel":"orders","instType":"SWAP","instId":"BTC-USDT-SWAP"},"data":[{"instType":"SWAP","instId":"BTC-USDT-SWAP","ordId":"42","state":"live","reduceOnly":"false"}]}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("private Run() did not finish")
	}
	if len(got) != 1 || got[0].OrderID != "42" || got[0].ReduceOnly != "false" {
		t.Fatalf("order data = %+v", got)
	}
	calls, apiKey, secret, passphrase := provider.snapshot()
	if calls != 2 {
		t.Fatalf("provider calls = %d, want 2", calls)
	}
	if !allZero(apiKey) || !allZero(secret) || !allZero(passphrase) {
		t.Fatal("resolved WebSocket credential byte slices were not overwritten")
	}
	if private.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", private.Generation())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) ||
		len(requests) != 2 || requests[0].Endpoint != "ws://private.example.test/ws/v5/private" {
		t.Fatalf("routes = %v, requests = %+v", routes, requests)
	}
}

func TestPrivateStreamRejectsRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &wsCredentialProvider{}
	client := newTestOKXStreamClient(
		t, &wsTestConnector{}, provider, time.Now, []transport.EgressRouteID{"route-a"},
	)
	_, err := client.PrivateStream(
		PrivateStreamRequest{Arguments: []StreamArgument{AccountStreamArgument()}},
		trade.WithEgressRoute("route-b"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("PrivateStream() error = %v, want authorization error", err)
	}
	calls, _, _, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestPrivateStreamDoesNotReconnectRejectedLogin(t *testing.T) {
	t.Parallel()

	connection := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{connection}}
	client := newTestOKXStreamClient(
		t, connector, &wsCredentialProvider{}, time.Now, []transport.EgressRouteID{"route-a"},
	)
	private, err := client.PrivateStream(
		PrivateStreamRequest{Arguments: []StreamArgument{AccountStreamArgument()}},
	)
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- private.Run(context.Background(), func(context.Context, StreamMessage) error { return nil })
	}()
	_ = waitForWSWrite(t, connection)
	connection.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"event":"error","code":"60009","msg":"Login failed","connId":"rejected"}`,
	)}}
	select {
	case runErr := <-done:
		var loginError *StreamLoginError
		if !errors.As(runErr, &loginError) || loginError.Code != "60009" {
			t.Fatalf("Run() error = %v, want StreamLoginError", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("rejected login did not stop")
	}
	if routes, _ := connector.snapshot(); len(routes) != 1 {
		t.Fatalf("connect attempts = %d, want 1", len(routes))
	}
}

func TestOKXApplicationHeartbeatWaitsForPong(t *testing.T) {
	t.Parallel()

	underlying := newWSTestConnection()
	connection := &okxConnection{next: underlying, pong: make(chan struct{}, 1)}
	done := make(chan error, 1)
	go func() { done <- connection.Ping(context.Background()) }()
	if payload := waitForWSWrite(t, underlying); string(payload) != "ping" {
		t.Fatalf("heartbeat payload = %q", payload)
	}
	underlying.reads <- wsReadResult{message: corestream.Message{
		Type: corestream.MessageText, Data: []byte("pong"),
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

func TestDecodeTypedStreamData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		check   func(t *testing.T, message StreamMessage)
	}{
		{
			name:    "orderbook",
			payload: `{"id":"request-1","arg":{"channel":"books","instId":"BTC-USDT"},"action":"snapshot","data":[{"asks":[["64001","2","0","3"]],"bids":[["64000","1","0","2"]],"ts":"1000","checksum":7,"seqId":9}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var value []OrderBook
				if err := message.DecodeData(&value); err != nil || message.RequestID != "request-1" ||
					len(value) != 1 || value[0].SequenceID != 9 {
					t.Fatalf("orderbook = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "trade",
			payload: `{"arg":{"channel":"trades","instId":"BTC-USDT"},"data":[{"instId":"BTC-USDT","tradeId":"10","px":"64000","sz":"0.01","side":"buy","ts":"1000","seqId":11}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var value []StreamTrade
				if err := message.DecodeData(&value); err != nil || len(value) != 1 || value[0].SequenceID != 11 {
					t.Fatalf("trade = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "candle",
			payload: `{"arg":{"channel":"candle1m","instId":"BTC-USDT"},"data":[["1000","1","2","0.5","1.5","10","15","16","1"]]}`,
			check: func(t *testing.T, message StreamMessage) {
				var value []Candle
				if err := message.DecodeData(&value); err != nil || len(value) != 1 || !value[0].Confirmed {
					t.Fatalf("candle = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "balance and position",
			payload: `{"arg":{"channel":"balance_and_position"},"data":[{"pTime":"1000","eventType":"snapshot","balData":[{"ccy":"USDT","cashBal":"900","uTime":"999"}],"posData":[{"posId":"20","instId":"BTC-USDT-SWAP","instType":"SWAP","posSide":"long","pos":"1"}]}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var value []StreamBalanceAndPosition
				if err := message.DecodeData(&value); err != nil || len(value) != 1 ||
					value[0].Balances[0].CashBalance != "900" || value[0].Positions[0].Position != "1" {
					t.Fatalf("balance and position = %+v, error = %v", value, err)
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

func TestStreamArgumentValidation(t *testing.T) {
	t.Parallel()

	candle, err := CandleStreamArgument("BTC-USDT", Candle1Hour)
	if err != nil || candle.Channel != "candle1H" {
		t.Fatalf("CandleStreamArgument() = %+v, %v", candle, err)
	}
	position, err := PositionStreamArgument("BTC-USDT-SWAP")
	if err != nil || position.InstrumentType != InstrumentTypeSwap {
		t.Fatalf("PositionStreamArgument() = %+v, %v", position, err)
	}
	invalid := []error{}
	_, err = PublicStreamArgument("candle1m", "BTC-USDT")
	invalid = append(invalid, err)
	_, err = CandleStreamArgument("BTC-USDT", CandleInterval("7m"))
	invalid = append(invalid, err)
	_, err = OrderStreamArgument(InstrumentType("FUTURES"), "BTC-USDT-260327")
	invalid = append(invalid, err)
	for _, item := range invalid {
		if !errors.Is(item, trade.ErrValidation) {
			t.Fatalf("stream validation error = %v", item)
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

func assertStreamOperation(
	t *testing.T,
	payload []byte,
	operation string,
	arguments []StreamArgument,
) {
	t.Helper()
	var request streamOperation
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode stream operation: %v", err)
	}
	if request.ID == "" || request.Operation != operation || !slices.Equal(request.Arguments, arguments) {
		t.Fatalf("stream operation = %+v, want %s %+v", request, operation, arguments)
	}
}

func assertLoginRequest(t *testing.T, payload []byte, timestamp string) {
	t.Helper()
	var request struct {
		Operation string                `json:"op"`
		Arguments []streamLoginArgument `json:"args"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode login request: %v", err)
	}
	if request.Operation != "login" || len(request.Arguments) != 1 {
		t.Fatalf("login request = %+v", request)
	}
	argument := request.Arguments[0]
	if argument.APIKey != "test-api-key" || argument.Passphrase != "test-passphrase" ||
		argument.Timestamp != timestamp {
		t.Fatalf("login argument = %+v", argument)
	}
	want, err := SignHMACSHA256(
		[]byte("test-secret-key"), []byte(timestamp+"GET/users/self/verify"),
	)
	if err != nil {
		t.Fatalf("SignHMACSHA256() error = %v", err)
	}
	if argument.Signature != want {
		t.Fatalf("signature = %q, want %q", argument.Signature, want)
	}
	if _, err := strconv.ParseInt(argument.Timestamp, 10, 64); err != nil {
		t.Fatalf("timestamp = %q", argument.Timestamp)
	}
}

func newTestOKXStreamClient(
	t *testing.T,
	connector corestream.Connector,
	provider credential.Provider,
	now func() time.Time,
	allowedRoutes []transport.EgressRouteID,
) *StreamClient {
	t.Helper()
	config := StreamClientConfig{
		Connector: connector, DefaultEgressRouteID: "route-a",
		PublicStreamURL:        "ws://public.example.test/ws/v5/public",
		PrivateStreamURL:       "ws://private.example.test/ws/v5/private",
		BusinessStreamURL:      "ws://business.example.test/ws/v5/business",
		AllowInsecureWebSocket: true, Now: now,
		Backoff:      func(int) time.Duration { return 0 },
		PingInterval: time.Hour, PingTimeout: time.Second,
		LoginTimeout: time.Second, SubscriptionInterval: time.Nanosecond,
	}
	if provider != nil {
		config.Credentials = &credential.Descriptor{
			AccountID: "okx-main", Exchange: model.ExchangeOKX, SecretRef: "secret/okx",
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
