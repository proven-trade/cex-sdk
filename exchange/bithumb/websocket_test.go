package bithumb

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
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
		reads: make(chan websocketReadResult, 8), writes: make(chan []byte, 8),
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
	mu        sync.Mutex
	calls     int
	materials []credential.Material
}

func (provider *websocketCredentialProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.calls++
	material := credential.Material{APIKey: []byte("access-key"), SecretKey: []byte("secret-key")}
	provider.materials = append(provider.materials, material)
	return material, nil
}

func (provider *websocketCredentialProvider) snapshot() (int, []credential.Material) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls, slices.Clone(provider.materials)
}

func TestBithumbPublicStreamReconnectsOnSameRouteWithNewTicket(t *testing.T) {
	t.Parallel()

	first := newWebSocketTestConnection()
	second := newWebSocketTestConnection()
	connector := &websocketTestConnector{connections: []*websocketTestConnection{first, second}}
	tickets := sequentialStreamSource("ticket-1", "ticket-2")
	client := newTestBithumbStreamClient(t, connector, nil, nil, tickets, nil)
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
	assertBithumbSubscription(
		t, waitForBithumbWebSocketWrite(t, first), "ticket-1", "ticker", "KRW-BTC", StreamFormatDefault,
	)
	first.reads <- websocketReadResult{err: errors.New("connection lost")}
	assertBithumbSubscription(
		t, waitForBithumbWebSocketWrite(t, second), "ticket-2", "ticker", "KRW-BTC", StreamFormatDefault,
	)
	second.reads <- websocketReadResult{message: corestream.Message{Data: []byte(
		`{"type":"ticker","code":"KRW-BTC","trade_price":64000000.10,"acc_trade_price":1000.5,"timestamp":2000,"stream_type":"REALTIME"}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("public Run() did not finish")
	}
	if ticker.Code != "KRW-BTC" || ticker.TradePrice != "64000000.10" || ticker.AccumulatedPrice != "1000.5" {
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
	if requests[0].Endpoint != "ws://public.example.test/websocket/v1" || len(requests[0].Header) != 0 {
		t.Fatalf("public dial request = %+v", requests[0])
	}
}

func TestBithumbPrivateStreamRefreshesJWTAndTicketOnReconnect(t *testing.T) {
	t.Parallel()

	first := newWebSocketTestConnection()
	second := newWebSocketTestConnection()
	connector := &websocketTestConnector{connections: []*websocketTestConnection{first, second}}
	provider := &websocketCredentialProvider{}
	nonces := sequentialStreamSource("nonce-1", "nonce-2")
	tickets := sequentialStreamSource("private-ticket-1", "private-ticket-2")
	client := newTestBithumbStreamClient(
		t, connector, provider, nonces, tickets, []transport.EgressRouteID{"route-a", "route-b"},
	)
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
	assertBithumbSubscriptionTicket(t, waitForBithumbWebSocketWrite(t, first), "private-ticket-1", 4)
	first.reads <- websocketReadResult{err: errors.New("connection lost")}
	assertBithumbSubscriptionTicket(t, waitForBithumbWebSocketWrite(t, second), "private-ticket-2", 4)
	second.reads <- websocketReadResult{message: corestream.Message{Data: []byte(
		`{"type":"myOrder","stream_type":"REALTIME","code":"KRW-BTC","order_id":"order-1","client_order_id":"strategy-1","side":"buy","order_type":"limit","state":"trade","time_in_force":"ioc","order_price":64000000,"order_quantity":0.1,"order_amount":6400000,"trade_id":"trade-1","trade_price":64000000,"trade_quantity":0.01,"trade_amount":640000,"executed_quantity":0.01,"remaining_quantity":0.09,"paid_fee":320,"timestamp":2000}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("private Run() did not finish")
	}
	if order.OrderID != "order-1" || order.Side != StreamOrderSideBuy ||
		order.TradePrice != "64000000" || order.RemainingQuantity != "0.09" {
		t.Fatalf("myOrder = %+v", order)
	}
	calls, materials := provider.snapshot()
	if calls != 2 || len(materials) != 2 {
		t.Fatalf("provider calls = %d, materials = %d", calls, len(materials))
	}
	for index, material := range materials {
		if !allZero(material.APIKey) || !allZero(material.SecretKey) {
			t.Fatalf("material %d was not destroyed", index)
		}
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) || len(requests) != 2 {
		t.Fatalf("routes = %v, requests = %d", routes, len(requests))
	}
	if requests[0].Endpoint != "ws://private.example.test/websocket/v2/private" {
		t.Fatalf("private endpoint = %q", requests[0].Endpoint)
	}
	assertBithumbJWTHeader(t, requests[0].Header.Get("Authorization"), "nonce-1", 1700000000123)
	assertBithumbJWTHeader(t, requests[1].Header.Get("Authorization"), "nonce-2", 1700000000123)
	if private.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", private.Generation())
	}
	_ = private.Close()
}

func TestBithumbPrivateStreamRejectsRouteBeforeResolvingSecret(t *testing.T) {
	t.Parallel()

	provider := &websocketCredentialProvider{}
	client := newTestBithumbStreamClient(
		t, &websocketTestConnector{}, provider, sequentialStreamSource("nonce"),
		sequentialStreamSource("ticket"), []transport.EgressRouteID{"route-a"},
	)
	_, err := client.PrivateStream(
		StreamRequest{Types: []StreamDataType{{Type: "myAsset"}}},
		trade.WithEgressRoute("route-b"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("PrivateStream() error = %v, want authorization error", err)
	}
	calls, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestBithumbDecodeStreamMessageSupportsSimpleStatusErrorAndPrivateAsset(t *testing.T) {
	t.Parallel()

	simple, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"ty":"ticker","cd":"KRW-BTC","tms":10,"st":"REALTIME"}`,
	)})
	if err != nil || simple.Type != "ticker" || simple.Timestamp != 10 || simple.StreamType != "REALTIME" {
		t.Fatalf("SIMPLE = %+v, %v", simple, err)
	}
	status, err := DecodeStreamMessage(corestream.Message{Data: []byte(`{"status":"UP"}`)})
	if err != nil || status.Status != "UP" {
		t.Fatalf("status = %+v, %v", status, err)
	}
	failure, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"error":{"name":"WRONG_FORMAT","message":"invalid format"}}`,
	)})
	if err != nil || failure.Error == nil || failure.Error.Name != "WRONG_FORMAT" {
		t.Fatalf("error = %+v, %v", failure, err)
	}
	assetMessage, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"type":"myAsset","stream_type":"REALTIME","assets":[{"currency":"KRW","balance":"1000.5","locked":"2.5"}],"asset_timestamp":100,"timestamp":101}`,
	)})
	if err != nil {
		t.Fatalf("asset message error = %v", err)
	}
	var asset MyAssetEvent
	if err := assetMessage.Decode(&asset); err != nil || len(asset.Assets) != 1 || asset.Assets[0].Balance != "1000.5" {
		t.Fatalf("asset = %+v, error = %v", asset, err)
	}
	if _, err := DecodeStreamMessage(corestream.Message{Data: []byte(`[{"type":"ticker"}]`)}); err == nil {
		t.Fatal("DecodeStreamMessage() accepted array response")
	}
}

func TestBithumbStreamSubscriptionPreservesOrderBookOptions(t *testing.T) {
	t.Parallel()

	payload, err := encodeSubscription("ticket-1", []StreamDataType{{
		Type: "orderbook", Codes: []string{"KRW-BTC"}, Level: "1000", OnlyRealtime: true,
	}}, StreamFormatSimple)
	if err != nil {
		t.Fatalf("encodeSubscription() error = %v", err)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(payload, &items); err != nil || len(items) != 3 {
		t.Fatalf("subscription = %s, error = %v", payload, err)
	}
	var dataType StreamDataType
	if err := json.Unmarshal(items[1], &dataType); err != nil || dataType.Level != "1000" || !dataType.OnlyRealtime {
		t.Fatalf("data type = %+v, error = %v", dataType, err)
	}
}

func TestBithumbStreamRequestValidation(t *testing.T) {
	t.Parallel()

	invalid := []StreamRequest{
		{},
		{Types: []StreamDataType{{Type: "ticker"}}},
		{Types: []StreamDataType{{Type: "candle.1m", Codes: []string{"KRW-BTC"}}}},
		{Types: []StreamDataType{{Type: "trade", Codes: []string{"krw-btc"}}}},
		{Types: []StreamDataType{{Type: "ticker", Codes: []string{"KRW-BTC"}, Level: "1000"}}},
		{Types: []StreamDataType{{Type: "orderbook", Codes: []string{"KRW-BTC"}, Level: "0"}}},
		{Types: []StreamDataType{{Type: "myAsset", Codes: []string{"KRW-BTC"}}}},
		{Types: []StreamDataType{{Type: "myOrder", OnlyRealtime: true}}},
		{Types: []StreamDataType{{Type: "myOrder"}}, Format: "JSON_LIST"},
	}
	for index, request := range invalid[:6] {
		if _, err := validateStreamRequest(request, false); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("public validation %d error = %v", index, err)
		}
	}
	for index, request := range invalid[6:] {
		if _, err := validateStreamRequest(request, true); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("private validation %d error = %v", index, err)
		}
	}
	valid, err := validateStreamRequest(StreamRequest{
		Types: []StreamDataType{{Type: "myOrder"}, {Type: "myAsset"}},
	}, true)
	if err != nil || valid.Format != StreamFormatDefault {
		t.Fatalf("valid private request = %+v, error = %v", valid, err)
	}
}

func waitForBithumbWebSocketWrite(t *testing.T, connection *websocketTestConnection) []byte {
	t.Helper()
	select {
	case payload := <-connection.writes:
		return payload
	case <-time.After(time.Second):
		t.Fatal("WebSocket write was not observed")
		return nil
	}
}

func assertBithumbSubscription(
	t *testing.T,
	payload []byte,
	ticket, streamType, code string,
	format StreamFormat,
) {
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
	if ticketValue.Ticket != ticket || dataType.Type != streamType ||
		!slices.Equal(dataType.Codes, []string{code}) || formatValue.Format != format {
		t.Fatalf("subscription = %s", payload)
	}
}

func assertBithumbSubscriptionTicket(t *testing.T, payload []byte, ticket string, itemCount int) {
	t.Helper()
	var items []json.RawMessage
	if err := json.Unmarshal(payload, &items); err != nil || len(items) != itemCount {
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

func assertBithumbJWTHeader(t *testing.T, authorization, nonce string, timestamp int64) {
	t.Helper()
	if !strings.HasPrefix(authorization, "Bearer ") {
		t.Fatalf("Authorization = %q", authorization)
	}
	token := strings.TrimPrefix(authorization, "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT parts = %d", len(parts))
	}
	mac := hmac.New(sha256.New, []byte("secret-key"))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	wantSignature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if parts[2] != wantSignature {
		t.Fatal("JWT signature is invalid")
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}
	var payload struct {
		AccessKey string `json:"access_key"`
		Nonce     string `json:"nonce"`
		Timestamp int64  `json:"timestamp"`
		QueryHash string `json:"query_hash"`
	}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("decode JWT JSON: %v", err)
	}
	if payload.AccessKey != "access-key" || payload.Nonce != nonce ||
		payload.Timestamp != timestamp || payload.QueryHash != "" {
		t.Fatalf("JWT payload = %+v", payload)
	}
}

func sequentialStreamSource(values ...string) NonceSource {
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

func newTestBithumbStreamClient(
	t *testing.T,
	connector corestream.Connector,
	provider credential.Provider,
	nonceSource NonceSource,
	ticketSource NonceSource,
	allowedRoutes []transport.EgressRouteID,
) *StreamClient {
	t.Helper()
	config := StreamClientConfig{
		Connector: connector, DefaultEgressRouteID: "route-a",
		PublicWebSocketURL:     "ws://public.example.test/websocket/v1",
		PrivateWebSocketURL:    "ws://private.example.test/websocket/v2/private",
		AllowInsecureWebSocket: true,
		NonceSource:            nonceSource, TicketSource: ticketSource,
		Now:          func() time.Time { return time.UnixMilli(1700000000123) },
		Backoff:      func(int) time.Duration { return 0 },
		PingInterval: time.Hour, PingTimeout: time.Second,
	}
	if provider != nil {
		config.Credentials = &credential.Descriptor{
			AccountID: "bithumb-pocket", Exchange: model.ExchangeBithumb,
			SecretRef: "secret/bithumb", Permissions: []credential.Permission{credential.PermissionRead},
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
