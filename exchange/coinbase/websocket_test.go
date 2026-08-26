package coinbase

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/json"
	"errors"
	"slices"
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

func TestPublicStreamKeepsRouteAndCurrentSubscriptionsAcrossReconnect(t *testing.T) {
	t.Parallel()

	first := newWSTestConnection()
	second := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{first, second}}
	client := newTestCoinbaseStreamClient(t, connector, nil, nil, nil)
	ticker := StreamSubscription{Channel: StreamChannelTicker, ProductIDs: []string{"BTC-USD"}}
	trades := StreamSubscription{Channel: StreamChannelMarketTrades, ProductIDs: []string{"BTC-USD"}}
	public, err := client.PublicStream(
		PublicStreamRequest{Subscriptions: []StreamSubscription{ticker}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var got []StreamMarketTradeEvent
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Channel != StreamChannelMarketTrades {
				return nil
			}
			if err := message.DecodeEvents(&got); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	assertWSOperation(t, waitForCoinbaseWSWrite(t, first), "subscribe", StreamSubscription{
		Channel: StreamChannelHeartbeats,
	}, false, nil)
	assertWSOperation(t, waitForCoinbaseWSWrite(t, first), "subscribe", ticker, false, nil)
	if err := public.Subscribe(context.Background(), trades); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	assertWSOperation(t, waitForCoinbaseWSWrite(t, first), "subscribe", trades, false, nil)
	if err := public.Unsubscribe(context.Background(), ticker); err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
	assertWSOperation(t, waitForCoinbaseWSWrite(t, first), "unsubscribe", ticker, false, nil)
	first.reads <- wsReadResult{err: errors.New("connection lost")}
	assertWSOperation(t, waitForCoinbaseWSWrite(t, second), "subscribe", StreamSubscription{
		Channel: StreamChannelHeartbeats,
	}, false, nil)
	assertWSOperation(t, waitForCoinbaseWSWrite(t, second), "subscribe", trades, false, nil)
	second.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"channel":"market_trades","timestamp":"2026-08-24T00:00:00Z","sequence_num":7,"events":[{"type":"update","trades":[{"trade_id":"trade-1","product_id":"BTC-USD","price":"64000","size":"0.01","side":"BUY","time":"2026-08-24T00:00:00Z"}]}]}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("public Run() did not finish")
	}
	if len(got) != 1 || len(got[0].Trades) != 1 || got[0].Trades[0].TradeID != "trade-1" {
		t.Fatalf("market trade events = %+v", got)
	}
	if public.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", public.Generation())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) {
		t.Fatalf("routes = %v", routes)
	}
	if len(requests) != 2 || requests[0].Endpoint != "ws://market.example.test" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestPublicStreamKeepsOnlyRemainingProductsAcrossReconnect(t *testing.T) {
	t.Parallel()

	first := newWSTestConnection()
	second := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{first, second}}
	client := newTestCoinbaseStreamClient(t, connector, nil, nil, nil)
	initial := StreamSubscription{
		Channel: StreamChannelTicker, ProductIDs: []string{"ETH-USD", "BTC-USD"},
	}
	public, err := client.PublicStream(PublicStreamRequest{Subscriptions: []StreamSubscription{initial}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- public.Run(ctx, func(context.Context, StreamMessage) error { return nil })
	}()
	assertWSOperation(t, waitForCoinbaseWSWrite(t, first), "subscribe", StreamSubscription{
		Channel: StreamChannelHeartbeats,
	}, false, nil)
	assertWSOperation(t, waitForCoinbaseWSWrite(t, first), "subscribe", StreamSubscription{
		Channel: StreamChannelTicker, ProductIDs: []string{"BTC-USD", "ETH-USD"},
	}, false, nil)
	removed := StreamSubscription{Channel: StreamChannelTicker, ProductIDs: []string{"BTC-USD"}}
	if err := public.Unsubscribe(context.Background(), removed); err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
	assertWSOperation(t, waitForCoinbaseWSWrite(t, first), "unsubscribe", removed, false, nil)
	first.reads <- wsReadResult{err: errors.New("connection lost")}
	assertWSOperation(t, waitForCoinbaseWSWrite(t, second), "subscribe", StreamSubscription{
		Channel: StreamChannelHeartbeats,
	}, false, nil)
	assertWSOperation(t, waitForCoinbaseWSWrite(t, second), "subscribe", StreamSubscription{
		Channel: StreamChannelTicker, ProductIDs: []string{"ETH-USD"},
	}, false, nil)
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

func TestUserStreamRefreshesJWTAndSubscriptionsAfterReconnect(t *testing.T) {
	t.Parallel()

	first := newWSTestConnection()
	second := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{first, second}}
	privateKey, secret := newTestECKey(t, elliptic.P256())
	keyName := "organizations/test/apiKeys/main"
	provider := &recordingProvider{keyName: keyName, secret: secret}
	fixedNow := time.Unix(1_700_000_000, 0)
	client := newTestCoinbaseStreamClient(t, connector, provider, func() time.Time { return fixedNow }, nil)
	user, err := client.UserStream(
		UserStreamRequest{ProductIDs: []string{"ETH-USD", "BTC-USD"}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("UserStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var got []StreamUserEvent
	go func() {
		done <- user.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Channel != StreamChannelUser {
				return nil
			}
			if err := message.DecodeEvents(&got); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	firstHeartbeatToken := assertWSOperation(
		t, waitForCoinbaseWSWrite(t, first), "subscribe",
		StreamSubscription{Channel: StreamChannelHeartbeats}, true, &privateKey.PublicKey,
	)
	firstUserToken := assertWSOperation(
		t, waitForCoinbaseWSWrite(t, first), "subscribe",
		StreamSubscription{Channel: StreamChannelUser, ProductIDs: []string{"BTC-USD", "ETH-USD"}},
		true, &privateKey.PublicKey,
	)
	if firstHeartbeatToken == firstUserToken {
		t.Fatal("authenticated WebSocket messages reused a JWT")
	}
	first.reads <- wsReadResult{err: errors.New("connection lost")}
	secondHeartbeatToken := assertWSOperation(
		t, waitForCoinbaseWSWrite(t, second), "subscribe",
		StreamSubscription{Channel: StreamChannelHeartbeats}, true, &privateKey.PublicKey,
	)
	secondUserToken := assertWSOperation(
		t, waitForCoinbaseWSWrite(t, second), "subscribe",
		StreamSubscription{Channel: StreamChannelUser, ProductIDs: []string{"BTC-USD", "ETH-USD"}},
		true, &privateKey.PublicKey,
	)
	if slices.Contains([]string{firstHeartbeatToken, firstUserToken}, secondHeartbeatToken) ||
		slices.Contains([]string{firstHeartbeatToken, firstUserToken}, secondUserToken) {
		t.Fatal("reconnected WebSocket reused a previous JWT")
	}
	second.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"channel":"user","timestamp":"2026-08-24T00:00:00Z","sequence_num":8,"events":[{"type":"update","orders":[{"order_id":"order-1","client_order_id":"strategy-1","product_id":"BTC-USD","order_side":"BUY","order_type":"LIMIT","status":"OPEN","limit_price":"64000","leaves_quantity":"0.01","total_fees":"0"}]}]}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("user Run() did not finish")
	}
	if len(got) != 1 || len(got[0].Orders) != 1 || got[0].Orders[0].OrderID != "order-1" {
		t.Fatalf("user events = %+v", got)
	}
	calls, issued := provider.snapshot()
	if calls != 2 {
		t.Fatalf("provider calls = %d, want 2", calls)
	}
	for _, secretBytes := range issued {
		if !allZero(secretBytes) {
			t.Fatal("resolved WebSocket credential byte slices were not overwritten")
		}
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) ||
		len(requests) != 2 || requests[0].Endpoint != "ws://user.example.test" {
		t.Fatalf("routes = %v, requests = %+v", routes, requests)
	}
}

func TestUserStreamRejectsRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	_, secret := newTestECKey(t, elliptic.P256())
	provider := &recordingProvider{keyName: "organizations/test/apiKeys/main", secret: secret}
	client := newTestCoinbaseStreamClient(
		t, &wsTestConnector{}, provider, nil, []transport.EgressRouteID{"route-a"},
	)
	_, err := client.UserStream(UserStreamRequest{}, trade.WithEgressRoute("route-b"))
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("UserStream() error = %v, want authorization", err)
	}
	calls, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestUserStreamStopsAfterExplicitAuthenticationError(t *testing.T) {
	t.Parallel()

	connection := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{connection}}
	_, secret := newTestECKey(t, elliptic.P256())
	provider := &recordingProvider{keyName: "organizations/test/apiKeys/main", secret: secret}
	client := newTestCoinbaseStreamClient(t, connector, provider, nil, nil)
	user, err := client.UserStream(UserStreamRequest{})
	if err != nil {
		t.Fatalf("UserStream() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- user.Run(context.Background(), func(context.Context, StreamMessage) error { return nil })
	}()
	_ = waitForCoinbaseWSWrite(t, connection)
	_ = waitForCoinbaseWSWrite(t, connection)
	connection.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"type":"error","message":"authentication failure: invalid JWT"}`,
	)}}
	select {
	case runErr := <-done:
		var authError *StreamAuthenticationError
		if !errors.As(runErr, &authError) {
			t.Fatalf("Run() error = %v, want StreamAuthenticationError", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("authentication error did not stop user stream")
	}
	if routes, _ := connector.snapshot(); len(routes) != 1 {
		t.Fatalf("connect attempts = %d, want 1", len(routes))
	}
}

func TestDecodeTypedCoinbaseStreamEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		check   func(t *testing.T, message StreamMessage)
	}{
		{
			name:    "ticker",
			payload: `{"channel":"ticker","sequence_num":1,"events":[{"type":"snapshot","tickers":[{"product_id":"BTC-USD","price":"64000","best_bid":"63999","best_ask":"64001"}]}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var events []StreamTickerEvent
				if err := message.DecodeEvents(&events); err != nil || events[0].Tickers[0].BestAsk != "64001" {
					t.Fatalf("ticker events = %+v, error = %v", events, err)
				}
			},
		},
		{
			name:    "level2",
			payload: `{"channel":"l2_data","sequence_num":2,"events":[{"type":"update","product_id":"BTC-USD","updates":[{"side":"bid","event_time":"2026-08-24T00:00:00Z","price_level":"63999","new_quantity":"1"}]}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var events []StreamLevel2Event
				if err := message.DecodeEvents(&events); err != nil || events[0].Updates[0].NewQuantity != "1" {
					t.Fatalf("level2 events = %+v, error = %v", events, err)
				}
			},
		},
		{
			name:    "candle",
			payload: `{"channel":"candles","sequence_num":3,"events":[{"type":"snapshot","candles":[{"start":"1700000000","high":"2","low":"1","open":"1","close":"2","volume":"10","product_id":"BTC-USD"}]}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var events []StreamCandleEvent
				if err := message.DecodeEvents(&events); err != nil || events[0].Candles[0].Close != "2" {
					t.Fatalf("candle events = %+v, error = %v", events, err)
				}
			},
		},
		{
			name:    "heartbeat",
			payload: `{"channel":"heartbeats","sequence_num":4,"events":[{"current_time":"now","heartbeat_counter":12}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var events []StreamHeartbeatEvent
				if err := message.DecodeEvents(&events); err != nil || events[0].HeartbeatCounter != 12 {
					t.Fatalf("heartbeat events = %+v, error = %v", events, err)
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

func TestCoinbaseStreamSubscriptionValidation(t *testing.T) {
	t.Parallel()

	invalid := [][]StreamSubscription{
		{{Channel: StreamChannelUser, ProductIDs: []string{"BTC-USD"}}},
		{{Channel: StreamChannelTicker}},
		{{Channel: StreamChannelHeartbeats, ProductIDs: []string{"BTC-USD"}}},
		{{Channel: StreamChannelTicker, ProductIDs: []string{"BTC/USD"}}},
	}
	for _, subscriptions := range invalid {
		if _, err := normalizePublicSubscriptions(subscriptions, true); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("normalizePublicSubscriptions(%+v) error = %v", subscriptions, err)
		}
	}
}

func waitForCoinbaseWSWrite(t *testing.T, connection *wsTestConnection) []byte {
	t.Helper()
	select {
	case payload := <-connection.writes:
		return payload
	case <-time.After(2 * time.Second):
		t.Fatal("Coinbase WebSocket write was not observed")
		return nil
	}
}

func assertWSOperation(
	t *testing.T,
	payload []byte,
	operation string,
	subscription StreamSubscription,
	authenticated bool,
	publicKey *ecdsa.PublicKey,
) string {
	t.Helper()
	var request streamOperation
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode Coinbase stream operation: %v", err)
	}
	if request.Type != operation || request.Channel != subscription.Channel ||
		!slices.Equal(request.ProductIDs, subscription.ProductIDs) {
		t.Fatalf("stream operation = %+v, want %s %+v", request, operation, subscription)
	}
	if !authenticated {
		if request.JWT != "" {
			t.Fatal("public WebSocket operation unexpectedly contains JWT")
		}
		return ""
	}
	if request.JWT == "" || publicKey == nil {
		t.Fatal("authenticated WebSocket operation does not contain JWT")
	}
	_, claims := verifyTestJWT(t, request.JWT, publicKey)
	if claims.URI != "" || claims.Issuer != "cdp" || claims.ExpiresAt-claims.NotBefore != 120 {
		t.Fatalf("WebSocket JWT claims = %+v", claims)
	}
	return request.JWT
}

func newTestCoinbaseStreamClient(
	t *testing.T,
	connector corestream.Connector,
	provider credential.Provider,
	now func() time.Time,
	allowedRoutes []transport.EgressRouteID,
) *StreamClient {
	t.Helper()
	if now == nil {
		now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	}
	var descriptor *credential.Descriptor
	if provider != nil {
		if len(allowedRoutes) == 0 {
			allowedRoutes = []transport.EgressRouteID{"route-a", "route-b"}
		}
		descriptor = &credential.Descriptor{
			AccountID: "coinbase-main", Exchange: model.ExchangeCoinbase,
			SecretRef: "secret/coinbase/main", Permissions: []credential.Permission{credential.PermissionRead},
			AllowedEgressRouteIDs: allowedRoutes,
		}
	}
	client, err := NewStreamClient(StreamClientConfig{
		Connector: connector, Credentials: descriptor, CredentialProvider: provider,
		DefaultEgressRouteID: "route-a", MarketDataStreamURL: "ws://market.example.test",
		UserDataStreamURL: "ws://user.example.test", AllowInsecureWebSocket: true,
		Now: now, SubscriptionInterval: time.Nanosecond,
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client
}
