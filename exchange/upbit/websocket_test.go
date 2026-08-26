package upbit

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/model"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

type websocketReadResult struct {
	message corestream.Message
	err     error
}

type websocketTestConnection struct {
	reads     chan websocketReadResult
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newWebSocketTestConnection() *websocketTestConnection {
	return &websocketTestConnection{
		reads:  make(chan websocketReadResult, 8),
		writes: make(chan []byte, 8),
		closed: make(chan struct{}),
	}
}

func (connection *websocketTestConnection) Read(ctx context.Context) (corestream.Message, error) {
	select {
	case <-ctx.Done():
		return corestream.Message{}, ctx.Err()
	case <-connection.closed:
		return corestream.Message{}, corestream.ErrSessionClosed
	case result := <-connection.reads:
		return result.message, result.err
	}
}

func (connection *websocketTestConnection) Write(ctx context.Context, message corestream.Message) error {
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

func (connection *websocketTestConnection) Ping(context.Context) error { return nil }

func (connection *websocketTestConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type websocketTestConnector struct {
	mu          sync.Mutex
	connections []*websocketTestConnection
	routes      []transport.EgressRouteID
	requests    []corestream.DialRequest
}

func (connector *websocketTestConnector) Connect(
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

func (connector *websocketTestConnector) snapshot() ([]transport.EgressRouteID, []corestream.DialRequest) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	requests := make([]corestream.DialRequest, len(connector.requests))
	for index, request := range connector.requests {
		requests[index] = corestream.DialRequest{Endpoint: request.Endpoint, Header: request.Header.Clone()}
	}
	return slices.Clone(connector.routes), requests
}

type websocketCredentialProvider struct {
	mu    sync.Mutex
	calls int
}

func (provider *websocketCredentialProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.mu.Lock()
	provider.calls++
	provider.mu.Unlock()
	return credential.Material{APIKey: []byte("access-key"), SecretKey: []byte("secret-key")}, nil
}

func (provider *websocketCredentialProvider) count() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls
}

func TestPublicStreamReconnectsOnSameRouteWithNewTicket(t *testing.T) {
	t.Parallel()

	first := newWebSocketTestConnection()
	second := newWebSocketTestConnection()
	connector := &websocketTestConnector{connections: []*websocketTestConnection{first, second}}
	tickets := sequentialSource("ticket-1", "ticket-2")
	client := newTestUpbitStreamClient(t, connector, nil, nil, tickets, nil)
	public, err := client.PublicStream(StreamRequest{
		Types:  []StreamDataType{{Type: "ticker", Codes: []string{"KRW-BTC"}}},
		Format: StreamFormatDefault,
	}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var ticker StreamTicker
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if err := message.Decode(&ticker); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	assertSubscription(t, waitForWebSocketWrite(t, first), "ticket-1", "ticker", "KRW-BTC", StreamFormatDefault)
	first.reads <- websocketReadResult{err: errors.New("connection lost")}
	assertSubscription(t, waitForWebSocketWrite(t, second), "ticket-2", "ticker", "KRW-BTC", StreamFormatDefault)
	second.reads <- websocketReadResult{message: corestream.Message{Data: []byte(
		`{"type":"ticker","code":"KRW-BTC","trade_price":64000000,"timestamp":2000,"stream_type":"REALTIME"}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("public Run() did not finish")
	}
	if ticker.Code != "KRW-BTC" || ticker.TradePrice != "64000000" {
		t.Fatalf("ticker = %+v", ticker)
	}
	if public.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", public.Generation())
	}
	_ = public.Close()
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) || len(requests) != 2 {
		t.Fatalf("routes = %v, requests = %d", routes, len(requests))
	}
}

func TestPrivateStreamRefreshesJWTAndTicketOnReconnect(t *testing.T) {
	t.Parallel()

	first := newWebSocketTestConnection()
	second := newWebSocketTestConnection()
	connector := &websocketTestConnector{connections: []*websocketTestConnection{first, second}}
	provider := &websocketCredentialProvider{}
	nonces := sequentialSource("nonce-1", "nonce-2")
	tickets := sequentialSource("private-ticket-1", "private-ticket-2")
	client := newTestUpbitStreamClient(t, connector, provider, nonces, tickets, []transport.EgressRouteID{"route-a", "route-b"})
	private, err := client.PrivateStream(StreamRequest{
		Types: []StreamDataType{{Type: "myOrder", Codes: []string{"KRW-BTC"}}, {Type: "myAsset"}},
	}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var order MyOrderEvent
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Type != "myOrder" {
				return nil
			}
			if err := message.Decode(&order); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	assertSubscriptionTicket(t, waitForWebSocketWrite(t, first), "private-ticket-1")
	first.reads <- websocketReadResult{err: errors.New("connection lost")}
	assertSubscriptionTicket(t, waitForWebSocketWrite(t, second), "private-ticket-2")
	second.reads <- websocketReadResult{message: corestream.Message{Data: []byte(
		`{"type":"myOrder","code":"KRW-BTC","uuid":"order-1","ask_bid":"BID","order_type":"limit","state":"trade","trade_uuid":"trade-1","price":64000000,"volume":0.1,"executed_volume":0.01,"timestamp":2000,"stream_type":"REALTIME"}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("private Run() did not finish")
	}
	if order.UUID != "order-1" || order.Price != "64000000" || order.ExecutedVolume != "0.01" {
		t.Fatalf("myOrder = %+v", order)
	}
	if provider.count() != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.count())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) || len(requests) != 2 {
		t.Fatalf("routes = %v, requests = %d", routes, len(requests))
	}
	assertJWTHeader(t, requests[0].Header.Get("Authorization"), "nonce-1")
	assertJWTHeader(t, requests[1].Header.Get("Authorization"), "nonce-2")
	if private.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", private.Generation())
	}
	_ = private.Close()
}

func TestPrivateStreamRejectsRouteBeforeResolvingSecret(t *testing.T) {
	t.Parallel()

	provider := &websocketCredentialProvider{}
	client := newTestUpbitStreamClient(t, &websocketTestConnector{}, provider, sequentialSource("nonce"), sequentialSource("ticket"), []transport.EgressRouteID{"route-a"})
	_, err := client.PrivateStream(
		StreamRequest{Types: []StreamDataType{{Type: "myAsset"}}},
		trade.WithEgressRoute("route-b"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("PrivateStream() error = %v, want authorization error", err)
	}
	if provider.count() != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.count())
	}
}

func TestDecodeStreamMessageSupportsJSONListSimpleStatusAndError(t *testing.T) {
	t.Parallel()

	listed, err := DecodeStreamMessage(corestream.Message{Data: []byte(`[{"type":"trade","code":"KRW-BTC","trade_price":1}]`)})
	if err != nil || listed.Type != "trade" || listed.Code != "KRW-BTC" {
		t.Fatalf("JSON_LIST = %+v, %v", listed, err)
	}
	simple, err := DecodeStreamMessage(corestream.Message{Data: []byte(`{"ty":"ticker","cd":"KRW-BTC","tms":10,"st":"REALTIME"}`)})
	if err != nil || simple.Type != "ticker" || simple.Timestamp != 10 || simple.StreamType != "REALTIME" {
		t.Fatalf("SIMPLE = %+v, %v", simple, err)
	}
	status, err := DecodeStreamMessage(corestream.Message{Data: []byte(`{"status":"UP"}`)})
	if err != nil || status.Status != "UP" {
		t.Fatalf("status = %+v, %v", status, err)
	}
	failure, err := DecodeStreamMessage(corestream.Message{Data: []byte(`{"error":{"name":"INVALID_AUTH","message":"invalid token"}}`)})
	if err != nil || failure.Error == nil || failure.Error.Name != "INVALID_AUTH" {
		t.Fatalf("error = %+v, %v", failure, err)
	}
}

func waitForWebSocketWrite(t *testing.T, connection *websocketTestConnection) []byte {
	t.Helper()
	select {
	case payload := <-connection.writes:
		return payload
	case <-time.After(time.Second):
		t.Fatal("WebSocket write was not observed")
		return nil
	}
}

func assertSubscription(t *testing.T, payload []byte, ticket, streamType, code string, format StreamFormat) {
	t.Helper()
	var items []json.RawMessage
	if err := json.Unmarshal(payload, &items); err != nil {
		t.Fatalf("decode subscription: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("subscription item count = %d", len(items))
	}
	var ticketValue struct {
		Ticket string `json:"ticket"`
	}
	var dataType StreamDataType
	var formatValue struct {
		Format StreamFormat `json:"format"`
	}
	_ = json.Unmarshal(items[0], &ticketValue)
	_ = json.Unmarshal(items[1], &dataType)
	_ = json.Unmarshal(items[2], &formatValue)
	if ticketValue.Ticket != ticket || dataType.Type != streamType || !slices.Equal(dataType.Codes, []string{code}) || formatValue.Format != format {
		t.Fatalf("subscription = %s", payload)
	}
}

func assertSubscriptionTicket(t *testing.T, payload []byte, ticket string) {
	t.Helper()
	var items []json.RawMessage
	if err := json.Unmarshal(payload, &items); err != nil || len(items) != 4 {
		t.Fatalf("subscription = %s, error = %v", payload, err)
	}
	var value struct {
		Ticket string `json:"ticket"`
	}
	_ = json.Unmarshal(items[0], &value)
	if value.Ticket != ticket {
		t.Fatalf("ticket = %q, want %q", value.Ticket, ticket)
	}
}

func assertJWTHeader(t *testing.T, authorization, nonce string) {
	t.Helper()
	if !strings.HasPrefix(authorization, "Bearer ") {
		t.Fatalf("Authorization = %q", authorization)
	}
	token := strings.TrimPrefix(authorization, "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT parts = %d", len(parts))
	}
	mac := hmac.New(sha512.New, []byte("secret-key"))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	wantSignature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if parts[2] != wantSignature {
		t.Fatal("JWT signature is invalid")
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("decode JWT JSON: %v", err)
	}
	if payload["access_key"] != "access-key" || payload["nonce"] != nonce {
		t.Fatalf("JWT payload = %v", payload)
	}
}

func sequentialSource(values ...string) NonceSource {
	var mu sync.Mutex
	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if len(values) == 0 {
			return "", errors.New("source exhausted")
		}
		value := values[0]
		values = values[1:]
		return value, nil
	}
}

func newTestUpbitStreamClient(
	t *testing.T,
	connector corestream.Connector,
	provider credential.Provider,
	nonceSource NonceSource,
	ticketSource NonceSource,
	allowedRoutes []transport.EgressRouteID,
) *StreamClient {
	t.Helper()
	config := StreamClientConfig{
		Connector:              connector,
		DefaultEgressRouteID:   "route-a",
		PublicWebSocketURL:     "ws://public.example.test/websocket/v1",
		PrivateWebSocketURL:    "ws://private.example.test/websocket/v1/private",
		AllowInsecureWebSocket: true,
		NonceSource:            nonceSource,
		TicketSource:           ticketSource,
		Backoff:                func(int) time.Duration { return 0 },
		PingInterval:           time.Hour,
		PingTimeout:            time.Second,
	}
	if provider != nil {
		config.Credentials = &credential.Descriptor{
			AccountID:             "upbit-pocket",
			Exchange:              model.ExchangeUpbit,
			SecretRef:             "secret/upbit",
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
