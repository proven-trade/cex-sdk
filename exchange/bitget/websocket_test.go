package bitget

import (
	"context"
	"encoding/json"
	"errors"
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
		reads:  make(chan wsReadResult, 12),
		writes: make(chan []byte, 12),
		closed: make(chan struct{}),
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
	connector.requests = append(connector.requests, corestream.DialRequest{Endpoint: request.Endpoint, Header: request.Header.Clone()})
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
	mu    sync.Mutex
	calls int
}

func (provider *wsCredentialProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.mu.Lock()
	provider.calls++
	provider.mu.Unlock()
	return credential.Material{
		APIKey:     []byte("test-api-key"),
		SecretKey:  []byte("test-secret-key"),
		Passphrase: []byte("test-passphrase"),
	}, nil
}

func (provider *wsCredentialProvider) count() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls
}

func TestPublicStreamKeepsRouteAndCurrentSubscriptionsAcrossReconnect(t *testing.T) {
	t.Parallel()

	first := newWSTestConnection()
	second := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{first, second}}
	client := newTestBitgetStreamClient(t, connector, nil, time.Now, nil)
	ticker, err := PublicStreamArgument(CategorySpot, "ticker", "BTCUSDT")
	if err != nil {
		t.Fatalf("PublicStreamArgument() error = %v", err)
	}
	trades, err := PublicStreamArgument(CategorySpot, "publicTrade", "BTCUSDT")
	if err != nil {
		t.Fatalf("PublicStreamArgument() error = %v", err)
	}
	public, err := client.PublicStream(StreamRequest{Arguments: []StreamArgument{ticker}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var got []StreamTicker
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
	assertStreamOperation(t, waitForWSWrite(t, second), "subscribe", []StreamArgument{trades, ticker})
	second.reads <- wsReadResult{message: corestream.Message{Type: corestream.MessageText, Data: []byte(
		`{"arg":{"instType":"spot","topic":"ticker","symbol":"BTCUSDT"},"action":"snapshot","data":[{"lastPrice":"64000","bid1Price":"63999","ask1Price":"64001"}],"ts":2000}`,
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
	if err := public.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) {
		t.Fatalf("routes = %v", routes)
	}
	if len(requests) != 2 || requests[0].Endpoint != "ws://public.example.test/v3/ws/public" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestPrivateStreamRelogsAndResubscribesAfterReconnect(t *testing.T) {
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
	client := newTestBitgetStreamClient(t, connector, provider, now, []transport.EgressRouteID{"route-a", "route-b"})
	account, err := PrivateStreamArgument("account")
	if err != nil {
		t.Fatalf("PrivateStreamArgument() error = %v", err)
	}
	private, err := client.PrivateStream(StreamRequest{Arguments: []StreamArgument{account}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var got []StreamAccount
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
	first.reads <- wsReadResult{message: corestream.Message{Data: []byte(`{"event":"login","code":"0","msg":""}`)}}
	assertStreamOperation(t, waitForWSWrite(t, first), "subscribe", []StreamArgument{account})
	first.reads <- wsReadResult{err: errors.New("connection lost")}
	assertLoginRequest(t, waitForWSWrite(t, second), "2000")
	second.reads <- wsReadResult{message: corestream.Message{Data: []byte(`{"event":"login","code":"0","msg":""}`)}}
	assertStreamOperation(t, waitForWSWrite(t, second), "subscribe", []StreamArgument{account})
	second.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"arg":{"instType":"UTA","topic":"account"},"action":"snapshot","data":[{"accountEquity":"1000","usdtEquity":"900","assets":[{"coin":"USDT","equity":"900"}]}],"ts":2001}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("private Run() did not finish")
	}
	if len(got) != 1 || got[0].AccountEquity != "1000" || len(got[0].Assets) != 1 {
		t.Fatalf("account data = %+v", got)
	}
	if provider.count() != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.count())
	}
	if private.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", private.Generation())
	}
	if err := private.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	routes, _ := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) {
		t.Fatalf("routes = %v", routes)
	}
}

func TestPrivateStreamRejectsRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &wsCredentialProvider{}
	client := newTestBitgetStreamClient(t, &wsTestConnector{}, provider, time.Now, []transport.EgressRouteID{"route-a"})
	account, _ := PrivateStreamArgument("account")
	_, err := client.PrivateStream(StreamRequest{Arguments: []StreamArgument{account}}, trade.WithEgressRoute("route-b"))
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("PrivateStream() error = %v, want authorization error", err)
	}
	if provider.count() != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.count())
	}
}

func TestPrivateStreamDoesNotReconnectRejectedLogin(t *testing.T) {
	t.Parallel()

	connection := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{connection}}
	provider := &wsCredentialProvider{}
	client := newTestBitgetStreamClient(t, connector, provider, time.Now, []transport.EgressRouteID{"route-a"})
	account, _ := PrivateStreamArgument("account")
	private, err := client.PrivateStream(StreamRequest{Arguments: []StreamArgument{account}})
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- private.Run(context.Background(), func(context.Context, StreamMessage) error { return nil })
	}()
	_ = waitForWSWrite(t, connection)
	connection.reads <- wsReadResult{message: corestream.Message{Data: []byte(`{"event":"error","code":"30005","msg":"invalid key"}`)}}
	select {
	case runErr := <-done:
		var loginError *LoginError
		if !errors.As(runErr, &loginError) || loginError.Code != "30005" {
			t.Fatalf("Run() error = %v, want LoginError", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("rejected login did not stop")
	}
	if routes, _ := connector.snapshot(); len(routes) != 1 {
		t.Fatalf("connect attempts = %d, want 1", len(routes))
	}
}

func TestBitgetApplicationHeartbeatWaitsForPong(t *testing.T) {
	t.Parallel()

	underlying := newWSTestConnection()
	connection := &bitgetConnection{next: underlying, pong: make(chan struct{}, 1)}
	done := make(chan error, 1)
	go func() { done <- connection.Ping(context.Background()) }()
	if payload := waitForWSWrite(t, underlying); string(payload) != "ping" {
		t.Fatalf("heartbeat payload = %q", payload)
	}
	underlying.reads <- wsReadResult{message: corestream.Message{Type: corestream.MessageText, Data: []byte("pong")}}
	message, err := connection.Read(context.Background())
	if err != nil || string(message.Data) != "pong" {
		t.Fatalf("Read() = %q, %v", message.Data, err)
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

func assertStreamOperation(t *testing.T, payload []byte, operation string, arguments []StreamArgument) {
	t.Helper()
	var request streamOperation
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode stream operation: %v", err)
	}
	if request.Operation != operation || !slices.Equal(request.Arguments, arguments) {
		t.Fatalf("stream operation = %+v, want %s %+v", request, operation, arguments)
	}
}

func assertLoginRequest(t *testing.T, payload []byte, timestamp string) {
	t.Helper()
	var request streamLoginRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode login request: %v", err)
	}
	if request.Operation != "login" || len(request.Arguments) != 1 {
		t.Fatalf("login request = %+v", request)
	}
	argument := request.Arguments[0]
	if argument.APIKey != "test-api-key" || argument.Passphrase != "test-passphrase" || argument.Timestamp != timestamp {
		t.Fatalf("login argument = %+v", argument)
	}
	want, err := SignHMACSHA256([]byte("test-secret-key"), []byte(timestamp+"GET/user/verify"))
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

func newTestBitgetStreamClient(
	t *testing.T,
	connector corestream.Connector,
	provider credential.Provider,
	now func() time.Time,
	allowedRoutes []transport.EgressRouteID,
) *StreamClient {
	t.Helper()
	config := StreamClientConfig{
		Connector:              connector,
		DefaultEgressRouteID:   "route-a",
		PublicStreamURL:        "ws://public.example.test/v3/ws/public",
		PrivateStreamURL:       "ws://private.example.test/v3/ws/private",
		AllowInsecureWebSocket: true,
		Now:                    now,
		Backoff:                func(int) time.Duration { return 0 },
		HeartbeatInterval:      time.Hour,
		HeartbeatTimeout:       time.Second,
		LoginTimeout:           time.Second,
		SubscriptionInterval:   time.Nanosecond,
	}
	if provider != nil {
		config.Credentials = &credential.Descriptor{
			AccountID:             "bitget-main",
			Exchange:              model.ExchangeBitget,
			SecretRef:             "secret/bitget",
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
