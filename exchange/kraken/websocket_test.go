package kraken

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type spotWSReadResult struct {
	message corestream.Message
	err     error
}

type spotWSTestConnection struct {
	reads     chan spotWSReadResult
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newSpotWSTestConnection() *spotWSTestConnection {
	return &spotWSTestConnection{
		reads: make(chan spotWSReadResult, 16), writes: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (connection *spotWSTestConnection) Read(ctx context.Context) (corestream.Message, error) {
	select {
	case <-ctx.Done():
		return corestream.Message{}, ctx.Err()
	case <-connection.closed:
		return corestream.Message{}, corestream.ErrSessionClosed
	case result := <-connection.reads:
		return result.message, result.err
	}
}

func (connection *spotWSTestConnection) Write(ctx context.Context, message corestream.Message) error {
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

func (connection *spotWSTestConnection) Ping(context.Context) error { return nil }

func (connection *spotWSTestConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type spotWSTestConnector struct {
	mu          sync.Mutex
	connections []*spotWSTestConnection
	routes      []transport.EgressRouteID
	requests    []corestream.DialRequest
}

func (connector *spotWSTestConnector) Connect(
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
		return nil, errors.New("no Kraken Spot test connection")
	}
	connection := connector.connections[0]
	connector.connections = connector.connections[1:]
	return connection, nil
}

func (connector *spotWSTestConnector) snapshot() (
	[]transport.EgressRouteID,
	[]corestream.DialRequest,
) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	return slices.Clone(connector.routes), slices.Clone(connector.requests)
}

func TestSpotPublicStreamKeepsRouteAndSubscriptionsAcrossReconnect(t *testing.T) {
	t.Parallel()

	first := newSpotWSTestConnection()
	second := newSpotWSTestConnection()
	connector := &spotWSTestConnector{connections: []*spotWSTestConnection{first, second}}
	client := newTestKrakenSpotStreamClient(t, connector, nil)
	ticker := SpotPublicSubscription{
		Channel: SpotChannelTicker, Symbols: []string{"BTC/USD"}, EventTrigger: SpotTickerOnBBO,
	}
	book := SpotPublicSubscription{
		Channel: SpotChannelBook, Symbols: []string{"BTC/USD"}, Depth: 25,
	}
	public, err := client.PublicStream(
		SpotPublicStreamRequest{Subscriptions: []SpotPublicSubscription{ticker}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var got []SpotStreamTicker
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message SpotStreamMessage) error {
			if message.Channel != string(SpotChannelTicker) {
				return nil
			}
			if err := message.DecodeData(&got); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	assertSpotStreamOperation(t, waitForSpotWSWrite(t, first), "subscribe", ticker, "")
	if err := public.Subscribe(context.Background(), book); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	assertSpotStreamOperation(t, waitForSpotWSWrite(t, first), "subscribe", book, "")
	first.reads <- spotWSReadResult{err: errors.New("connection lost")}
	assertSpotStreamOperation(t, waitForSpotWSWrite(t, second), "subscribe", book, "")
	assertSpotStreamOperation(t, waitForSpotWSWrite(t, second), "subscribe", ticker, "")
	second.reads <- spotWSReadResult{message: corestream.Message{Data: []byte(
		`{"channel":"ticker","type":"update","data":[{"symbol":"BTC/USD","ask":64001.25,"ask_qty":2,"bid":63999.75,"bid_qty":1,"change":100,"change_pct":0.15,"high":65000,"last":64000.5,"low":62000,"volume":12.25,"vwap":63500,"timestamp":"2026-08-25T00:00:00Z"}]}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("public Run() did not finish")
	}
	if len(got) != 1 || got[0].Last != "64000.5" || got[0].Ask != "64001.25" {
		t.Fatalf("ticker data = %+v", got)
	}
	if public.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", public.Generation())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) {
		t.Fatalf("routes = %v", routes)
	}
	if len(requests) != 2 || requests[0].Endpoint != "ws://public.example.test/v2" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestSpotPrivateStreamRefreshesTokenOnSameRouteAfterReconnect(t *testing.T) {
	t.Parallel()

	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	var tokenCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != privatePrefix+"GetWebSocketsToken" {
			http.NotFound(writer, request)
			return
		}
		body, _ := io.ReadAll(request.Body)
		values, parseErr := url.ParseQuery(string(body))
		nonce := values.Get("nonce")
		expected, signErr := SignREST(request.URL.Path, nonce, string(body), secret)
		if parseErr != nil || signErr != nil || request.Header.Get("API-Key") != "test-api-key" ||
			request.Header.Get("API-Sign") != expected {
			http.Error(writer, `{"error":["EAPI:Invalid signature"]}`, http.StatusUnauthorized)
			return
		}
		call := tokenCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"error":[],"result":{"token":"token-`+
			strconv.FormatInt(call, 10)+`","expires":900}}`)
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{secret: secret}
	restClient, _ := newTestClient(
		t, server.URL, sender, provider,
		[]transport.EgressRouteID{"route-a", "route-b"}, time.UnixMilli(1_700_000_000_000),
	)
	first := newSpotWSTestConnection()
	second := newSpotWSTestConnection()
	connector := &spotWSTestConnector{connections: []*spotWSTestConnection{first, second}}
	client := newTestKrakenSpotStreamClient(t, connector, restClient)
	executions := SpotPrivateSubscription{
		Channel: SpotChannelExecutions, SnapOrders: boolPointer(true), SnapTrades: boolPointer(true),
	}
	balances := SpotPrivateSubscription{Channel: SpotChannelBalances, Snapshot: boolPointer(true)}
	private, err := client.PrivateStream(
		SpotPrivateStreamRequest{Subscriptions: []SpotPrivateSubscription{executions, balances}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var got []SpotStreamExecution
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message SpotStreamMessage) error {
			if message.Channel != string(SpotChannelExecutions) {
				return nil
			}
			if err := message.DecodeData(&got); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	assertSpotPrivateOperation(t, waitForSpotWSWrite(t, first), executions, "token-1")
	assertSpotPrivateOperation(t, waitForSpotWSWrite(t, first), balances, "token-1")
	first.reads <- spotWSReadResult{err: errors.New("connection lost")}
	assertSpotPrivateOperation(t, waitForSpotWSWrite(t, second), executions, "token-2")
	assertSpotPrivateOperation(t, waitForSpotWSWrite(t, second), balances, "token-2")
	second.reads <- spotWSReadResult{message: corestream.Message{Data: []byte(
		`{"channel":"executions","type":"update","data":[{"exec_type":"trade","order_id":"ORDER-1","trade_id":42,"symbol":"BTC/USD","side":"buy","order_type":"limit","order_status":"filled","order_qty":0.01,"cum_qty":"0.01","last_qty":0.01,"limit_price":64000,"avg_price":64000.25,"last_price":64000.25,"cost":640.0025,"fees":[{"asset":"USD","qty":1.6}],"timestamp":"2026-08-25T00:00:00Z","sequence":7}]}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("private Run() did not finish")
	}
	if len(got) != 1 || got[0].OrderID != "ORDER-1" || got[0].AveragePrice != "64000.25" {
		t.Fatalf("execution data = %+v", got)
	}
	if tokenCalls.Load() != 2 {
		t.Fatalf("token calls = %d, want 2", tokenCalls.Load())
	}
	if routes := sender.snapshot(); !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) {
		t.Fatalf("REST routes = %v", routes)
	}
	calls, issued := provider.snapshot()
	if calls != 2 {
		t.Fatalf("provider calls = %d, want 2", calls)
	}
	for _, value := range issued {
		if !allZero(value) {
			t.Fatal("resolved token credential byte slices were not overwritten")
		}
	}
	wsRoutes, requests := connector.snapshot()
	if !slices.Equal(wsRoutes, []transport.EgressRouteID{"route-b", "route-b"}) ||
		len(requests) != 2 || requests[0].Endpoint != "ws://private.example.test/v2" {
		t.Fatalf("WebSocket routes = %v, requests = %+v", wsRoutes, requests)
	}
}

func TestSpotPrivateStreamRejectsRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{secret: []byte("dGVzdA==")}
	restClient, _ := newTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, time.Now(),
	)
	connector := &spotWSTestConnector{}
	client := newTestKrakenSpotStreamClient(t, connector, restClient)
	_, err := client.PrivateStream(
		SpotPrivateStreamRequest{Subscriptions: []SpotPrivateSubscription{{Channel: SpotChannelBalances}}},
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

func TestSpotStreamRejectedOperationStopsHandler(t *testing.T) {
	t.Parallel()

	connection := newSpotWSTestConnection()
	connector := &spotWSTestConnector{connections: []*spotWSTestConnection{connection}}
	client := newTestKrakenSpotStreamClient(t, connector, nil)
	public, err := client.PublicStream(SpotPublicStreamRequest{Subscriptions: []SpotPublicSubscription{{
		Channel: SpotChannelTrade, Symbols: []string{"BTC/USD"},
	}}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- public.Run(context.Background(), func(context.Context, SpotStreamMessage) error {
			t.Error("handler must not receive a rejected operation")
			return nil
		})
	}()
	_ = waitForSpotWSWrite(t, connection)
	connection.reads <- spotWSReadResult{message: corestream.Message{Data: []byte(
		`{"method":"subscribe","req_id":1,"success":false,"error":"Unsupported field"}`,
	)}}
	select {
	case runErr := <-done:
		var requestError *SpotStreamRequestError
		if !errors.As(runErr, &requestError) || requestError.Method != "subscribe" ||
			requestError.RequestID != 1 || requestError.Message != "Unsupported field" {
			t.Fatalf("Run() error = %v, want SpotStreamRequestError", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("rejected operation did not stop")
	}
}

func TestDecodeSpotTypedStreamData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		check   func(t *testing.T, message SpotStreamMessage)
	}{
		{
			name:    "orderbook",
			payload: `{"channel":"book","type":"snapshot","data":[{"symbol":"BTC/USD","bids":[{"price":64000.125,"qty":"1.5"}],"asks":[{"price":"64001","qty":2}],"checksum":7,"timestamp":"2026-08-25T00:00:00Z"}]}`,
			check: func(t *testing.T, message SpotStreamMessage) {
				var value []SpotStreamBook
				if err := message.DecodeData(&value); err != nil || len(value) != 1 ||
					value[0].Bids[0].Price != "64000.125" || value[0].Checksum != 7 {
					t.Fatalf("orderbook = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "trade",
			payload: `{"channel":"trade","type":"update","data":[{"symbol":"BTC/USD","side":"buy","qty":0.01,"price":"64000","ord_type":"market","trade_id":42,"timestamp":"2026-08-25T00:00:00Z"}]}`,
			check: func(t *testing.T, message SpotStreamMessage) {
				var value []SpotStreamTrade
				if err := message.DecodeData(&value); err != nil || len(value) != 1 ||
					value[0].TradeID != 42 || value[0].Quantity != "0.01" {
					t.Fatalf("trade = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "ohlc",
			payload: `{"channel":"ohlc","type":"update","data":[{"symbol":"BTC/USD","open":63000,"high":65000,"low":62000,"close":64000,"vwap":63500,"trades":20,"volume":10.5,"interval_begin":"2026-08-25T00:00:00Z","interval":1}]}`,
			check: func(t *testing.T, message SpotStreamMessage) {
				var value []SpotStreamOHLC
				if err := message.DecodeData(&value); err != nil || len(value) != 1 ||
					value[0].Close != "64000" || value[0].Trades != 20 {
					t.Fatalf("OHLC = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "instrument",
			payload: `{"channel":"instrument","type":"snapshot","data":{"assets":[{"id":"USD","class":"currency","status":"enabled","precision":4,"precision_display":2,"borrowable":true,"collateral_value":1,"margin_rate":"0.01","multiplier":1}],"pairs":[{"symbol":"BTC/USD","base":"BTC","quote":"USD","status":"online","cost_min":0.5,"cost_precision":5,"qty_min":"0.0001","qty_precision":8,"price_increment":0.1,"qty_increment":"0.00000001"}]}}`,
			check: func(t *testing.T, message SpotStreamMessage) {
				var value SpotStreamInstrument
				if err := message.DecodeData(&value); err != nil || len(value.Assets) != 1 ||
					len(value.Pairs) != 1 || value.Pairs[0].QuantityMinimum != "0.0001" {
					t.Fatalf("instrument = %+v, error = %v", value, err)
				}
			},
		},
		{
			name:    "balance",
			payload: `{"channel":"balances","type":"snapshot","data":[{"asset":"USD","balance":900.5,"wallets":[{"balance":"900.5","type":"spot","id":"main"}],"sequence":3}]}`,
			check: func(t *testing.T, message SpotStreamMessage) {
				var value []SpotStreamBalance
				if err := message.DecodeData(&value); err != nil || len(value) != 1 ||
					value[0].Balance != "900.5" || value[0].Wallets[0].Type != "spot" {
					t.Fatalf("balance = %+v, error = %v", value, err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message, err := DecodeSpotStreamMessage(corestream.Message{Data: []byte(test.payload)})
			if err != nil || len(message.Raw) == 0 {
				t.Fatalf("DecodeSpotStreamMessage() = %+v, %v", message, err)
			}
			test.check(t, message)
		})
	}
}

func TestSpotStreamSubscriptionValidation(t *testing.T) {
	t.Parallel()

	invalidPublic := []SpotPublicSubscription{
		{Channel: SpotChannelBook, Symbols: []string{"BTC/USD"}, Depth: 50},
		{Channel: SpotChannelOHLC, Symbols: []string{"BTC/USD"}, Interval: 7},
		{Channel: SpotChannelTicker, Symbols: []string{"btc/usd"}},
		{Channel: SpotChannelInstrument, Symbols: []string{"BTC/USD"}},
		{Channel: SpotChannelTrade, Symbols: []string{"BTC/USD", "BTC/USD"}},
	}
	for _, subscription := range invalidPublic {
		if _, err := validateSpotPublicSubscriptions([]SpotPublicSubscription{subscription}); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("public validation error = %v, want validation", err)
		}
	}
	invalidPrivate := []SpotPrivateSubscription{
		{Channel: SpotPrivateChannel("orders")},
		{Channel: SpotChannelBalances, SnapOrders: boolPointer(true)},
		{Channel: SpotChannelExecutions, Snapshot: boolPointer(true)},
	}
	for _, subscription := range invalidPrivate {
		if _, err := validateSpotPrivateSubscriptions([]SpotPrivateSubscription{subscription}); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("private validation error = %v, want validation", err)
		}
	}
}

func waitForSpotWSWrite(t *testing.T, connection *spotWSTestConnection) []byte {
	t.Helper()
	select {
	case payload := <-connection.writes:
		return payload
	case <-time.After(time.Second):
		t.Fatal("Kraken Spot WebSocket write was not observed")
		return nil
	}
}

func assertSpotStreamOperation(
	t *testing.T,
	payload []byte,
	method string,
	subscription SpotPublicSubscription,
	token string,
) {
	t.Helper()
	var request spotStreamOperation
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode Spot stream operation: %v", err)
	}
	if request.RequestID < 1 || request.Method != method || request.Params.Channel != string(subscription.Channel) ||
		!slices.Equal(request.Params.Symbol, subscription.Symbols) || request.Params.Depth != subscription.Depth ||
		request.Params.Interval != subscription.Interval ||
		request.Params.EventTrigger != string(subscription.EventTrigger) || request.Params.Token != token {
		t.Fatalf("Spot stream operation = %+v", request)
	}
}

func assertSpotPrivateOperation(
	t *testing.T,
	payload []byte,
	subscription SpotPrivateSubscription,
	token string,
) {
	t.Helper()
	var request spotStreamOperation
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode Spot private operation: %v", err)
	}
	if request.RequestID < 1 || request.Method != "subscribe" ||
		request.Params.Channel != string(subscription.Channel) || request.Params.Token != token ||
		!sameOptionalBool(request.Params.Snapshot, subscription.Snapshot) ||
		!sameOptionalBool(request.Params.SnapOrders, subscription.SnapOrders) ||
		!sameOptionalBool(request.Params.SnapTrades, subscription.SnapTrades) ||
		!sameOptionalBool(request.Params.OrderStatus, subscription.OrderStatus) {
		t.Fatalf("Spot private operation = %+v", request)
	}
}

func sameOptionalBool(left, right *bool) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func boolPointer(value bool) *bool { return &value }

func newTestKrakenSpotStreamClient(
	t *testing.T,
	connector corestream.Connector,
	restClient *Client,
) *SpotStreamClient {
	t.Helper()
	client, err := NewSpotStreamClient(SpotStreamClientConfig{
		Connector: connector, RESTClient: restClient, DefaultEgressRouteID: "route-a",
		PublicStreamURL: "ws://public.example.test/v2", PrivateStreamURL: "ws://private.example.test/v2",
		AllowInsecureWebSocket: true, Backoff: func(int) time.Duration { return 0 },
		PingInterval: time.Hour, PingTimeout: time.Second, SubscriptionInterval: time.Nanosecond,
	})
	if err != nil {
		t.Fatalf("NewSpotStreamClient() error = %v", err)
	}
	return client
}

func TestSpotWebSocketTokenRejectsIncompleteResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"error":[],"result":{"token":"","expires":0}}`)
	}))
	defer server.Close()
	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{
			secret: []byte(base64.StdEncoding.EncodeToString([]byte("test-secret"))),
		}, []transport.EgressRouteID{"route-a"}, time.Now(),
	)
	_, err := client.WebSocketToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "token response is incomplete") {
		t.Fatalf("WebSocketToken() error = %v", err)
	}
}
