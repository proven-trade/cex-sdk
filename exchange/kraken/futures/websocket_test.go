package futures

import (
	"context"
	"encoding/base64"
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

type futuresWSReadResult struct {
	message corestream.Message
	err     error
}

type futuresWSTestConnection struct {
	reads     chan futuresWSReadResult
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newFuturesWSTestConnection() *futuresWSTestConnection {
	return &futuresWSTestConnection{
		reads: make(chan futuresWSReadResult, 16), writes: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (connection *futuresWSTestConnection) Read(ctx context.Context) (corestream.Message, error) {
	select {
	case <-ctx.Done():
		return corestream.Message{}, ctx.Err()
	case <-connection.closed:
		return corestream.Message{}, corestream.ErrSessionClosed
	case result := <-connection.reads:
		return result.message, result.err
	}
}

func (connection *futuresWSTestConnection) Write(
	ctx context.Context,
	message corestream.Message,
) error {
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

func (connection *futuresWSTestConnection) Ping(context.Context) error { return nil }

func (connection *futuresWSTestConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type futuresWSTestConnector struct {
	mu          sync.Mutex
	connections []*futuresWSTestConnection
	routes      []transport.EgressRouteID
	requests    []corestream.DialRequest
}

func (connector *futuresWSTestConnector) Connect(
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
		return nil, errors.New("no Kraken Futures test connection")
	}
	connection := connector.connections[0]
	connector.connections = connector.connections[1:]
	return connection, nil
}

func (connector *futuresWSTestConnector) snapshot() (
	[]transport.EgressRouteID,
	[]corestream.DialRequest,
) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	return slices.Clone(connector.routes), slices.Clone(connector.requests)
}

func TestPublicStreamKeepsRouteAndSubscriptionsAcrossReconnect(t *testing.T) {
	t.Parallel()

	first := newFuturesWSTestConnection()
	second := newFuturesWSTestConnection()
	connector := &futuresWSTestConnector{
		connections: []*futuresWSTestConnection{first, second},
	}
	client := newTestFuturesStreamClient(t, connector, nil)
	ticker := PublicStreamSubscription{
		Feed: PublicFeedTicker, ProductIDs: []string{"PI_XBTUSD"},
	}
	book := PublicStreamSubscription{
		Feed: PublicFeedBook, ProductIDs: []string{"PI_XBTUSD"},
	}
	public, err := client.PublicStream(
		PublicStreamRequest{Subscriptions: []PublicStreamSubscription{ticker}},
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
			if message.Feed != string(PublicFeedTicker) {
				return nil
			}
			if err := message.Decode(&got); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	assertPublicStreamOperation(t, waitForFuturesWSWrite(t, first), "subscribe", ticker)
	if err := public.Subscribe(context.Background(), book); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	assertPublicStreamOperation(t, waitForFuturesWSWrite(t, first), "subscribe", book)
	first.reads <- futuresWSReadResult{err: errors.New("connection lost")}
	assertPublicStreamOperation(t, waitForFuturesWSWrite(t, second), "subscribe", book)
	assertPublicStreamOperation(t, waitForFuturesWSWrite(t, second), "subscribe", ticker)
	second.reads <- futuresWSReadResult{message: corestream.Message{Data: []byte(
		`{"feed":"ticker","time":1700000000000,"product_id":"PI_XBTUSD","bid":64000.125,"ask":"64000.5","bid_size":2,"ask_size":"1.5","volume":100,"index":63990.25,"last":64000.25,"change":1.25,"pair":"XBT:USD","openInterest":500,"markPrice":64001.25,"post_only":false}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("public Run() did not finish")
	}
	if got.ProductID != "PI_XBTUSD" || got.Bid != "64000.125" || got.MarkPrice != "64001.25" {
		t.Fatalf("ticker data = %+v", got)
	}
	if public.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", public.Generation())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) {
		t.Fatalf("routes = %v", routes)
	}
	if len(requests) != 2 || requests[0].Endpoint != "ws://futures.example.test/ws/v1" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestPrivateStreamRenewsChallengeAndResubscribesAfterReconnect(t *testing.T) {
	t.Parallel()

	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	provider := &recordingProvider{secret: secret}
	sender := &directSender{}
	restClient, _ := newTestClient(
		t, "http://127.0.0.1", sender, provider,
		[]transport.EgressRouteID{"route-a", "route-b"}, time.Now(),
	)
	first := newFuturesWSTestConnection()
	second := newFuturesWSTestConnection()
	connector := &futuresWSTestConnector{
		connections: []*futuresWSTestConnection{first, second},
	}
	client := newTestFuturesStreamClient(t, connector, restClient)
	fills := PrivateStreamSubscription{
		Feed: PrivateFeedFills, ProductIDs: []string{"PI_XBTUSD"},
	}
	balances := PrivateStreamSubscription{Feed: PrivateFeedBalances}
	private, err := client.PrivateStream(
		PrivateStreamRequest{Subscriptions: []PrivateStreamSubscription{fills, balances}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var got StreamFills
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Feed != "fills" {
				return nil
			}
			if err := message.Decode(&got); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	assertChallengeOperation(t, waitForFuturesWSWrite(t, first))
	first.reads <- futuresWSReadResult{message: corestream.Message{Data: []byte(
		`{"event":"info","version":1}`,
	)}}
	first.reads <- futuresWSReadResult{message: corestream.Message{Data: []byte(
		`{"event":"challenge","message":"challenge-1"}`,
	)}}
	assertPrivateStreamOperation(t, waitForFuturesWSWrite(t, first), fills, "challenge-1", secret)
	assertPrivateStreamOperation(t, waitForFuturesWSWrite(t, first), balances, "challenge-1", secret)
	first.reads <- futuresWSReadResult{err: errors.New("connection lost")}
	assertChallengeOperation(t, waitForFuturesWSWrite(t, second))
	second.reads <- futuresWSReadResult{message: corestream.Message{Data: []byte(
		`{"event":"challenge","message":"challenge-2"}`,
	)}}
	assertPrivateStreamOperation(t, waitForFuturesWSWrite(t, second), fills, "challenge-2", secret)
	assertPrivateStreamOperation(t, waitForFuturesWSWrite(t, second), balances, "challenge-2", secret)
	second.reads <- futuresWSReadResult{message: corestream.Message{Data: []byte(
		`{"feed":"fills","account":"kraken-main","fills":[{"instrument":"PI_XBTUSD","time":1700000000000,"price":64000.25,"seq":7,"buy":true,"qty":"1.5","remaining_order_qty":0,"order_id":"ORDER-1","cli_ord_id":"CLIENT-1","fill_id":"FILL-1","fill_type":"maker","fee_paid":1.25,"fee_currency":"USD","order_type":"lmt"}]}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("private Run() did not finish")
	}
	if len(got.Fills) != 1 || got.Fills[0].FillID != "FILL-1" || got.Fills[0].Price != "64000.25" {
		t.Fatalf("fills data = %+v", got)
	}
	if routes := sender.snapshot(); len(routes) != 0 {
		t.Fatalf("unexpected REST routes = %v", routes)
	}
	calls, issued := provider.snapshot()
	if calls != 2 {
		t.Fatalf("provider calls = %d, want 2", calls)
	}
	for _, value := range issued {
		if !allZero(value) {
			t.Fatal("resolved WebSocket credential byte slices were not overwritten")
		}
	}
	if private.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", private.Generation())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) ||
		len(requests) != 2 || requests[0].Endpoint != "ws://futures.example.test/ws/v1" {
		t.Fatalf("routes = %v, requests = %+v", routes, requests)
	}
}

func TestPrivateStreamRejectsRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{secret: []byte("dGVzdA==")}
	restClient, _ := newTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, time.Now(),
	)
	connector := &futuresWSTestConnector{}
	client := newTestFuturesStreamClient(t, connector, restClient)
	_, err := client.PrivateStream(
		PrivateStreamRequest{Subscriptions: []PrivateStreamSubscription{{Feed: PrivateFeedBalances}}},
		trade.WithEgressRoute("route-b"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("PrivateStream() error = %v, want authorization error", err)
	}
	calls, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
	if routes, _ := connector.snapshot(); len(routes) != 0 {
		t.Fatalf("connect attempts = %d, want 0", len(routes))
	}
}

func TestPrivateStreamDoesNotReconnectRejectedChallenge(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{secret: []byte("dGVzdA==")}
	restClient, _ := newTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, time.Now(),
	)
	connection := newFuturesWSTestConnection()
	connector := &futuresWSTestConnector{connections: []*futuresWSTestConnection{connection}}
	client := newTestFuturesStreamClient(t, connector, restClient)
	private, err := client.PrivateStream(PrivateStreamRequest{
		Subscriptions: []PrivateStreamSubscription{{Feed: PrivateFeedOpenOrders}},
	})
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- private.Run(context.Background(), func(context.Context, StreamMessage) error {
			return nil
		})
	}()
	assertChallengeOperation(t, waitForFuturesWSWrite(t, connection))
	connection.reads <- futuresWSReadResult{message: corestream.Message{Data: []byte(
		`{"event":"error","message":"Invalid API key"}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, trade.ErrAuthentication) {
			t.Fatalf("Run() error = %v, want authentication error", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("rejected challenge did not stop")
	}
	if routes, _ := connector.snapshot(); len(routes) != 1 {
		t.Fatalf("connect attempts = %d, want 1", len(routes))
	}
	calls, issued := provider.snapshot()
	if calls != 1 {
		t.Fatalf("provider calls = %d, want 1", calls)
	}
	for _, value := range issued {
		if !allZero(value) {
			t.Fatal("rejected challenge credential byte slices were not overwritten")
		}
	}
}

func TestChallengeTimeoutRemainsReconnectable(t *testing.T) {
	t.Parallel()

	client := newTestFuturesStreamClient(t, &futuresWSTestConnector{}, nil)
	err := normalizeChallengeError(context.Background(), context.DeadlineExceeded)
	if errors.Is(err, context.DeadlineExceeded) || !client.privateReconnectPolicy(err) {
		t.Fatalf("normalized challenge error = %v, want reconnectable error", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	err = normalizeChallengeError(canceled, context.Canceled)
	if !errors.Is(err, context.Canceled) || client.privateReconnectPolicy(err) {
		t.Fatalf("canceled challenge error = %v, want permanent cancellation", err)
	}
}

func TestStreamRejectedSubscriptionStopsHandler(t *testing.T) {
	t.Parallel()

	connection := newFuturesWSTestConnection()
	connector := &futuresWSTestConnector{connections: []*futuresWSTestConnection{connection}}
	client := newTestFuturesStreamClient(t, connector, nil)
	public, err := client.PublicStream(PublicStreamRequest{
		Subscriptions: []PublicStreamSubscription{{
			Feed: PublicFeedTrade, ProductIDs: []string{"PI_XBTUSD"},
		}},
	})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- public.Run(context.Background(), func(context.Context, StreamMessage) error {
			t.Error("handler must not receive a rejected subscription")
			return nil
		})
	}()
	_ = waitForFuturesWSWrite(t, connection)
	connection.reads <- futuresWSReadResult{message: corestream.Message{Data: []byte(
		`{"event":"subscribed_failed","feed":"trade","message":"Invalid product id"}`,
	)}}
	select {
	case runErr := <-done:
		var requestError *StreamRequestError
		if !errors.As(runErr, &requestError) || requestError.Feed != "trade" ||
			requestError.Message != "Invalid product id" {
			t.Fatalf("Run() error = %v, want StreamRequestError", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("rejected subscription did not stop")
	}
}

func TestDecodeTypedStreamFeeds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		check   func(t *testing.T, message StreamMessage)
	}{
		{
			name:    "orderbook snapshot",
			payload: `{"feed":"book_snapshot","product_id":"PI_XBTUSD","timestamp":1700000000000,"seq":7,"tickSize":null,"bids":[{"price":64000.125,"qty":"1.5"}],"asks":[{"price":"64001","qty":2}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var value StreamBookSnapshot
				if err := message.Decode(&value); err != nil || value.Sequence != 7 ||
					value.Bids[0].Price != "64000.125" || value.TickSize != "" {
					t.Fatalf("orderbook = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "trade snapshot",
			payload: `{"feed":"trade_snapshot","product_id":"PI_XBTUSD","trades":[{"feed":"trade","product_id":"PI_XBTUSD","uid":"TRADE-1","side":"sell","type":"fill","seq":8,"time":1700000000000,"qty":2.5,"price":"64000"}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var value StreamTradeSnapshot
				if err := message.Decode(&value); err != nil || len(value.Trades) != 1 ||
					value.Trades[0].TradeID != "TRADE-1" || value.Trades[0].Quantity != "2.5" {
					t.Fatalf("trades = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "balances",
			payload: `{"feed":"balances","account":"main","timestamp":1700000000000,"seq":9,"holding":{"USD":900.5},"futures":{"f-xbt:usd":{"name":"f-xbt:usd","pair":"XBT:USD","unit":"XBT","portfolio_value":2.5,"balance":"2","maintenance_margin":0.1,"initial_margin":0.2,"available":1.8,"unrealized_funding":0.01,"pnl":0.5}},"flex_futures":{"balance_value":1000,"portfolio_value":"1010","collateral_value":990,"currencies":{"USD":{"quantity":900,"value":900,"collateral_value":900,"available":800,"haircut":0,"conversion_spread":0.001}}}}`,
			check: func(t *testing.T, message StreamMessage) {
				var value StreamBalances
				if err := message.Decode(&value); err != nil || value.Holding["USD"] != "900.5" ||
					value.Futures["f-xbt:usd"].PortfolioValue != "2.5" ||
					value.FlexFutures.Currencies["USD"].Available != "800" {
					t.Fatalf("balances = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "open orders",
			payload: `{"feed":"open_orders","order":{"instrument":"PI_XBTUSD","time":1700000000000,"last_update_time":1700000000100,"qty":2,"filled":"0.5","limit_price":64000,"stop_price":0,"type":"limit","order_id":"ORDER-1","direction":0,"reduce_only":true},"is_cancel":false,"reason":"partial_fill"}`,
			check: func(t *testing.T, message StreamMessage) {
				var value StreamOpenOrders
				if err := message.Decode(&value); err != nil || value.Order == nil ||
					value.Order.Filled != "0.5" || value.Reason != "partial_fill" {
					t.Fatalf("orders = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "open positions",
			payload: `{"feed":"open_positions","account":"main","seq":10,"timestamp":1700000000000,"positions":[{"instrument":"PI_XBTUSD","balance":2,"entry_price":63000,"mark_price":"64000.25","index_price":63990,"pnl":2000.5,"liquidation_threshold":50000,"return_on_equity":0.2,"unrealized_funding":1,"effective_leverage":2,"initial_margin":1000,"maintenance_margin":500,"pnl_currency":"USD","max_fixed_leverage":50,"fill_time":"2026-08-25T00:00:00Z"}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var value StreamOpenPositions
				if err := message.Decode(&value); err != nil || len(value.Positions) != 1 ||
					value.Positions[0].MarkPrice != "64000.25" || value.Sequence != 10 {
					t.Fatalf("positions = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "account log",
			payload: `{"feed":"account_log","logs":[{"id":11,"date":"2026-08-25T00:00:00Z","asset":"xbt","contract":"PI_XBTUSD","info":"trade","booking_uid":"BOOK-1","margin_account":"f-xbt:usd","old_balance":1,"new_balance":2.5,"trade_price":64000,"realized_pnl":10.25,"fee":1}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var value StreamAccountLog
				if err := message.Decode(&value); err != nil || len(value.Logs) != 1 ||
					value.Logs[0].BookingID != "BOOK-1" || value.Logs[0].NewBalance != "2.5" {
					t.Fatalf("account log = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "notifications",
			payload: `{"feed":"notifications_auth","notifications":[{"id":12,"type":"maintenance","priority":"high","note":"scheduled","effective_time":1700000000000,"expected_downtime_minutes":5}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var value StreamNotifications
				if err := message.Decode(&value); err != nil || len(value.Notifications) != 1 ||
					value.Notifications[0].Priority != "high" {
					t.Fatalf("notifications = %+v, error = %v", value, err)
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

func TestStreamSubscriptionValidation(t *testing.T) {
	t.Parallel()

	invalidPublic := []PublicStreamSubscription{
		{Feed: PublicFeedTicker},
		{Feed: PublicFeedHeartbeat, ProductIDs: []string{"PI_XBTUSD"}},
		{Feed: PublicFeedTrade, ProductIDs: []string{"pi_xbtusd"}},
		{Feed: PublicFeedBook, ProductIDs: []string{"PI_XBTUSD", "PI_XBTUSD"}},
	}
	for _, subscription := range invalidPublic {
		if _, err := validatePublicStreamSubscriptions([]PublicStreamSubscription{subscription}); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("public validation error = %v, want validation", err)
		}
	}
	invalidPrivate := []PrivateStreamSubscription{
		{Feed: PrivateStreamFeed("orders")},
		{Feed: PrivateFeedBalances, ProductIDs: []string{"PI_XBTUSD"}},
		{Feed: PrivateFeedFills, ProductIDs: []string{"bad product"}},
	}
	for _, subscription := range invalidPrivate {
		if _, err := validatePrivateStreamSubscriptions([]PrivateStreamSubscription{subscription}); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("private validation error = %v, want validation", err)
		}
	}
}

func TestDecodeStreamMessageRedactsAuthentication(t *testing.T) {
	t.Parallel()

	message, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"event":"subscribed","feed":"fills","api_key":"key","original_challenge":"challenge","signed_challenge":"signature"}`,
	)})
	if err != nil {
		t.Fatalf("DecodeStreamMessage() error = %v", err)
	}
	if string(message.Raw) == "" || json.Valid(message.Raw) == false {
		t.Fatalf("redacted raw = %q", message.Raw)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(message.Raw, &fields); err != nil {
		t.Fatalf("decode redacted raw: %v", err)
	}
	for _, key := range []string{"api_key", "original_challenge", "signed_challenge"} {
		if _, exists := fields[key]; exists {
			t.Fatalf("redacted raw contains %s", key)
		}
	}
}

func waitForFuturesWSWrite(t *testing.T, connection *futuresWSTestConnection) []byte {
	t.Helper()
	select {
	case payload := <-connection.writes:
		return payload
	case <-time.After(time.Second):
		t.Fatal("Kraken Futures WebSocket write was not observed")
		return nil
	}
}

func assertPublicStreamOperation(
	t *testing.T,
	payload []byte,
	event string,
	subscription PublicStreamSubscription,
) {
	t.Helper()
	var operation streamOperation
	if err := json.Unmarshal(payload, &operation); err != nil {
		t.Fatalf("decode public stream operation: %v", err)
	}
	if operation.Event != event || operation.Feed != string(subscription.Feed) ||
		!slices.Equal(operation.ProductIDs, subscription.ProductIDs) || operation.APIKey != "" {
		t.Fatalf("public stream operation = %+v", operation)
	}
}

func assertChallengeOperation(t *testing.T, payload []byte) {
	t.Helper()
	var operation streamOperation
	if err := json.Unmarshal(payload, &operation); err != nil {
		t.Fatalf("decode challenge operation: %v", err)
	}
	if operation.Event != "challenge" || operation.APIKey != "test-api-key" || operation.Feed != "" ||
		operation.OriginalChallenge != "" || operation.SignedChallenge != "" {
		t.Fatalf("challenge operation = %+v", operation)
	}
}

func assertPrivateStreamOperation(
	t *testing.T,
	payload []byte,
	subscription PrivateStreamSubscription,
	challenge string,
	secret []byte,
) {
	t.Helper()
	var operation streamOperation
	if err := json.Unmarshal(payload, &operation); err != nil {
		t.Fatalf("decode private stream operation: %v", err)
	}
	wantSignature, err := SignChallenge(challenge, secret)
	if err != nil {
		t.Fatalf("SignChallenge() error = %v", err)
	}
	if operation.Event != "subscribe" || operation.Feed != string(subscription.Feed) ||
		!slices.Equal(operation.ProductIDs, subscription.ProductIDs) ||
		operation.APIKey != "test-api-key" || operation.OriginalChallenge != challenge ||
		operation.SignedChallenge != wantSignature {
		t.Fatalf("private stream operation = %+v", operation)
	}
}

func newTestFuturesStreamClient(
	t *testing.T,
	connector corestream.Connector,
	restClient *Client,
) *StreamClient {
	t.Helper()
	client, err := NewStreamClient(StreamClientConfig{
		Connector: connector, RESTClient: restClient, DefaultEgressRouteID: "route-a",
		StreamURL: "ws://futures.example.test/ws/v1", AllowInsecureWebSocket: true,
		Backoff: func(int) time.Duration { return 0 }, PingInterval: time.Hour,
		PingTimeout: time.Second, ChallengeTimeout: time.Second,
		SubscriptionInterval: time.Nanosecond,
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client
}
