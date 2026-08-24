package usdm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type usdmWebSocketReadResult struct {
	message corestream.Message
	err     error
}

type usdmWebSocketTestConnection struct {
	reads     chan usdmWebSocketReadResult
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newUSDMWebSocketTestConnection() *usdmWebSocketTestConnection {
	return &usdmWebSocketTestConnection{
		reads: make(chan usdmWebSocketReadResult, 16), writes: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (connection *usdmWebSocketTestConnection) Read(
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

func (connection *usdmWebSocketTestConnection) Write(
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

func (connection *usdmWebSocketTestConnection) Ping(context.Context) error { return nil }

func (connection *usdmWebSocketTestConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type usdmWebSocketTestConnector struct {
	mu          sync.Mutex
	connections []*usdmWebSocketTestConnection
	routes      []transport.EgressRouteID
	requests    []corestream.DialRequest
	connected   chan struct{}
}

func (connector *usdmWebSocketTestConnector) Connect(
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
		return nil, errors.New("no Binance USD-M test connection")
	}
	connection := connector.connections[0]
	connector.connections = connector.connections[1:]
	if connector.connected != nil {
		connector.connected <- struct{}{}
	}
	return connection, nil
}

func (connector *usdmWebSocketTestConnector) snapshot() (
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

type usdmMarketCommand struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
	ID     uint64   `json:"id"`
}

func TestUserDataListenKeyLifecycleUsesSelectedRoute(t *testing.T) {
	t.Parallel()

	var methods []string
	var methodMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != userDataStreamPath || request.Header.Get("X-MBX-APIKEY") != "api-key" ||
			request.URL.RawQuery != "" {
			http.Error(writer, `{"code":-2015,"msg":"invalid request"}`, http.StatusUnauthorized)
			return
		}
		methodMu.Lock()
		methods = append(methods, request.Method)
		methodMu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case http.MethodPost:
			_, _ = io.WriteString(writer, `{"listenKey":"ListenKey123"}`)
		case http.MethodPut:
			_, _ = io.WriteString(writer, `{"listenKey":"ListenKey123"}`)
		case http.MethodDelete:
			writer.WriteHeader(http.StatusOK)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newTestClient(
		t, server.URL, sender, provider, time.UnixMilli(1_700_000_000_000),
		[]transport.EgressRouteID{"route-a", "route-b"},
	)
	started, err := client.StartUserDataStream(
		context.Background(), trade.WithEgressRoute("route-b"),
	)
	if err != nil || started.ListenKey != "ListenKey123" {
		t.Fatalf("StartUserDataStream() = %+v, error = %v", started, err)
	}
	kept, err := client.KeepaliveUserDataStream(
		context.Background(), trade.WithEgressRoute("route-b"),
	)
	if err != nil || kept.ListenKey != "ListenKey123" {
		t.Fatalf("KeepaliveUserDataStream() = %+v, error = %v", kept, err)
	}
	if err := client.CloseUserDataStream(
		context.Background(), trade.WithEgressRoute("route-b"),
	); err != nil {
		t.Fatalf("CloseUserDataStream() error = %v", err)
	}
	methodMu.Lock()
	gotMethods := slices.Clone(methods)
	methodMu.Unlock()
	if !slices.Equal(gotMethods, []string{http.MethodPost, http.MethodPut, http.MethodDelete}) {
		t.Fatalf("methods = %v", gotMethods)
	}
	if routes := sender.snapshot(); !slices.Equal(
		routes, []transport.EgressRouteID{"route-b", "route-b", "route-b"},
	) {
		t.Fatalf("routes = %v", routes)
	}
	calls, key, secret := provider.snapshot()
	if calls != 3 || !allZero(key) || !allZero(secret) {
		t.Fatalf(
			"provider calls = %d, key zero = %v, secret zero = %v",
			calls, allZero(key), allZero(secret),
		)
	}
	snapshot, err := limiter.Snapshot("binance-usdm:route:route-b:request_weight:1minute")
	if err != nil || snapshot.Used != 3 {
		t.Fatalf("limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestUSDMMarketStreamUsesSplitRouteAndRestoresSubscriptions(t *testing.T) {
	t.Parallel()

	first := newUSDMWebSocketTestConnection()
	second := newUSDMWebSocketTestConnection()
	connector := &usdmWebSocketTestConnector{
		connections: []*usdmWebSocketTestConnection{first, second},
	}
	client := newTestUSDMStreamClient(t, connector, nil, 50*time.Minute)
	aggregate, _ := AggregateTradeStream("BTCUSDT")
	kline, _ := KlineStream("BTCUSDT", Candle1Minute)
	market, err := client.MarketStream(
		MarketStreamRequest{Subscriptions: []StreamSubscription{aggregate, kline}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("MarketStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var event StreamAggregateTrade
	go func() {
		done <- market.Run(ctx, func(_ context.Context, message MarketStreamMessage) error {
			if message.Response != nil {
				return nil
			}
			if err := message.Decode(&event); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	firstCommand := decodeUSDMMarketCommand(t, waitForUSDMWebSocketWrite(t, first))
	assertUSDMMarketCommand(t, firstCommand, "SUBSCRIBE", []string{
		"btcusdt@aggTrade", "btcusdt@kline_1m",
	})
	first.reads <- usdmWebSocketReadResult{err: errors.New("connection lost")}
	secondCommand := decodeUSDMMarketCommand(t, waitForUSDMWebSocketWrite(t, second))
	assertUSDMMarketCommand(t, secondCommand, "SUBSCRIBE", []string{
		"btcusdt@aggTrade", "btcusdt@kline_1m",
	})
	second.reads <- usdmWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"stream":"btcusdt@aggTrade","data":{"e":"aggTrade","E":1700000000001,"s":"BTCUSDT","a":7,"p":"64000","q":"0.1","f":11,"l":12,"T":1700000000000,"m":true,"ps":"BTCUSDT","st":1}}`),
	}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("market stream Run() did not finish")
	}
	if event.Symbol != "BTCUSDT" || event.AggregateID != 7 || event.Price != "64000" {
		t.Fatalf("aggregate trade = %+v", event)
	}
	if market.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", market.Generation())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) {
		t.Fatalf("routes = %v", routes)
	}
	for _, request := range requests {
		if request.Endpoint != "ws://stream.example.test/market/stream" {
			t.Fatalf("endpoint = %q", request.Endpoint)
		}
	}
	_ = market.Close()
}

func TestUSDMMarketStreamRollsBackRejectedSubscription(t *testing.T) {
	t.Parallel()

	connection := newUSDMWebSocketTestConnection()
	connector := &usdmWebSocketTestConnector{
		connections: []*usdmWebSocketTestConnection{connection},
	}
	client := newTestUSDMStreamClient(t, connector, nil, 50*time.Minute)
	aggregate, _ := AggregateTradeStream("BTCUSDT")
	ticker, _ := TickerStream("BTCUSDT")
	market, err := client.MarketStream(
		MarketStreamRequest{Subscriptions: []StreamSubscription{aggregate}},
	)
	if err != nil {
		t.Fatalf("MarketStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	ackHandled := make(chan struct{}, 1)
	go func() {
		done <- market.Run(ctx, func(_ context.Context, message MarketStreamMessage) error {
			if message.Response != nil && message.Response.Error != nil {
				ackHandled <- struct{}{}
			}
			return nil
		})
	}()
	_ = decodeUSDMMarketCommand(t, waitForUSDMWebSocketWrite(t, connection))
	if err := market.Subscribe(context.Background(), ticker); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	command := decodeUSDMMarketCommand(t, waitForUSDMWebSocketWrite(t, connection))
	connection.reads <- usdmWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"code":2,"msg":"Invalid request","id":` +
			strconv.FormatUint(command.ID, 10) + `}`),
	}}
	select {
	case <-ackHandled:
	case <-time.After(2 * time.Second):
		t.Fatal("subscription error response was not handled")
	}
	if subscriptions := market.snapshotSubscriptions(); !slices.Equal(subscriptions, []StreamSubscription{aggregate}) {
		t.Fatalf("subscriptions = %+v", subscriptions)
	}
	_ = market.Close()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, corestream.ErrSessionClosed) {
			t.Fatalf("Run() error = %v, want session closed", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("market stream Run() did not finish")
	}
}

func TestUSDMUserDataStreamRefreshFailureReconnectsWithNewListenKey(t *testing.T) {
	t.Parallel()

	var starts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != userDataStreamPath || request.Header.Get("X-MBX-APIKEY") != "api-key" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case http.MethodPost:
			_, _ = io.WriteString(writer, `{"listenKey":"ListenKey`+
				string(rune('0'+starts.Add(1)))+`"}`)
		case http.MethodPut:
			http.Error(writer, `{"code":-1125,"msg":"listen key does not exist"}`, http.StatusBadRequest)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	restClient, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		time.UnixMilli(1_700_000_000_000), []transport.EgressRouteID{"route-a", "route-b"},
	)
	first := newUSDMWebSocketTestConnection()
	second := newUSDMWebSocketTestConnection()
	connector := &usdmWebSocketTestConnector{
		connections: []*usdmWebSocketTestConnection{first, second}, connected: make(chan struct{}, 2),
	}
	client := newTestUSDMStreamClient(t, connector, restClient, 30*time.Millisecond)
	userData, err := client.UserDataStream(trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("UserDataStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var account StreamAccountUpdate
	go func() {
		done <- userData.Run(ctx, func(_ context.Context, message UserDataStreamMessage) error {
			if message.EventType != "ACCOUNT_UPDATE" {
				return nil
			}
			if err := message.Decode(&account); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	waitForUSDMConnection(t, connector.connected)
	waitForUSDMConnection(t, connector.connected)
	second.reads <- usdmWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"e":"ACCOUNT_UPDATE","E":1700000000001,"T":1700000000000,"a":{"m":"ORDER","B":[{"a":"USDT","wb":"1000","cw":"900","bc":"10"}],"P":[{"s":"BTCUSDT","pa":"0.1","ep":"64000","bep":"64001","cr":"2","up":"1","mt":"cross","iw":"0","ps":"LONG"}]}}`),
	}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("user data Run() did not finish")
	}
	if len(account.Account.Balances) != 1 || account.Account.Balances[0].WalletBalance != "1000" ||
		len(account.Account.Positions) != 1 || account.Account.Positions[0].PositionAmount != "0.1" {
		t.Fatalf("account update = %+v", account)
	}
	if userData.Generation() != 2 || starts.Load() != 2 {
		t.Fatalf("generation = %d, starts = %d", userData.Generation(), starts.Load())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) || len(requests) != 2 {
		t.Fatalf("routes = %v, requests = %d", routes, len(requests))
	}
	if requests[0].Endpoint != "ws://stream.example.test/private/ws/ListenKey1" ||
		requests[1].Endpoint != "ws://stream.example.test/private/ws/ListenKey2" {
		t.Fatalf("requests = %+v", requests)
	}
	_ = userData.Close()
}

func TestUSDMUserDataStreamRejectsRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{}
	restClient, _ := newTestClient(
		t, "http://rest.example.test", &directSender{}, provider,
		time.UnixMilli(1_700_000_000_000), []transport.EgressRouteID{"route-a"},
	)
	client := newTestUSDMStreamClient(
		t, &usdmWebSocketTestConnector{}, restClient, 50*time.Minute,
	)
	_, err := client.UserDataStream(trade.WithEgressRoute("route-b"))
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("UserDataStream() error = %v, want authorization", err)
	}
	calls, _, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestUSDMUserDataStreamRunRejectsNilContext(t *testing.T) {
	t.Parallel()

	connection := newUSDMWebSocketTestConnection()
	connector := &usdmWebSocketTestConnector{
		connections: []*usdmWebSocketTestConnection{connection},
	}
	restClient, _ := newTestClient(
		t, "http://example.test", &directSender{}, &recordingProvider{},
		time.UnixMilli(1_700_000_000_000), []transport.EgressRouteID{"route-a"},
	)
	client := newTestUSDMStreamClient(t, connector, restClient, 50*time.Minute)
	userData, err := client.UserDataStream()
	if err != nil {
		t.Fatalf("UserDataStream() error = %v", err)
	}
	err = userData.Run(nil, func(context.Context, UserDataStreamMessage) error { return nil })
	if err == nil {
		t.Fatal("Run() error = nil, want nil context error")
	}
}

func TestUSDMStreamDecodesPublicAndPrivateEvents(t *testing.T) {
	t.Parallel()

	marketTests := []struct {
		name    string
		payload string
		check   func(t *testing.T, message MarketStreamMessage)
	}{
		{
			name:    "마크가",
			payload: `{"stream":"btcusdt@markPrice@1s","data":{"e":"markPriceUpdate","E":1700000000000,"s":"BTCUSDT","p":"64001","i":"63999","P":"64000","r":"0.0001","T":1700003600000,"ps":"BTCUSDT","st":1}}`,
			check: func(t *testing.T, message MarketStreamMessage) {
				var event StreamMarkPrice
				if err := message.Decode(&event); err != nil || event.MarkPrice != "64001" {
					t.Fatalf("mark price = %+v, error = %v", event, err)
				}
			},
		},
		{
			name:    "캔들",
			payload: `{"stream":"btcusdt@kline_1m","data":{"e":"kline","E":1700000000000,"s":"BTCUSDT","k":{"t":1699999980000,"T":1700000039999,"s":"BTCUSDT","i":"1m","f":1,"L":3,"o":"63000","c":"64000","h":"65000","l":"62000","v":"10","n":3,"x":true,"q":"640000","V":"6","Q":"384000"}}}`,
			check: func(t *testing.T, message MarketStreamMessage) {
				var event StreamKlineEvent
				if err := message.Decode(&event); err != nil || !event.Kline.Closed ||
					event.Kline.Close != "64000" {
					t.Fatalf("kline = %+v, error = %v", event, err)
				}
			},
		},
		{
			name:    "증분 호가",
			payload: `{"e":"depthUpdate","E":1700000000001,"T":1700000000000,"s":"BTCUSDT","U":157,"u":160,"pu":156,"b":[["63999","1"]],"a":[["64001","2"]],"ps":"BTCUSDT","st":1}`,
			check: func(t *testing.T, message MarketStreamMessage) {
				var event StreamDepth
				if err := message.Decode(&event); err != nil || event.PreviousUpdate != 156 ||
					event.Bids[0].Quantity != "1" {
					t.Fatalf("depth = %+v, error = %v", event, err)
				}
			},
		},
	}
	for _, test := range marketTests {
		t.Run(test.name, func(t *testing.T) {
			message, err := DecodeMarketStreamMessage(corestream.Message{
				Type: corestream.MessageText, Data: []byte(test.payload),
			})
			if err != nil {
				t.Fatalf("DecodeMarketStreamMessage() error = %v", err)
			}
			test.check(t, message)
		})
	}
	private, err := DecodeUserDataStreamMessage(corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"e":"ORDER_TRADE_UPDATE","E":1700000000001,"T":1700000000000,"o":{"s":"BTCUSDT","c":"strategy-1","S":"BUY","o":"LIMIT","f":"GTC","q":"0.1","p":"64000","ap":"64000","sp":"0","x":"TRADE","X":"PARTIALLY_FILLED","i":42,"l":"0.01","z":"0.01","L":"64000","N":"USDT","n":"0.1","T":1700000000000,"t":9,"m":true,"R":false,"wt":"CONTRACT_PRICE","ot":"LIMIT","ps":"LONG","rp":"1"}}`),
	})
	if err != nil || private.EventType != "ORDER_TRADE_UPDATE" ||
		private.TransactionTime != 1700000000000 {
		t.Fatalf("private message = %+v, error = %v", private, err)
	}
	var order StreamOrderTradeUpdate
	if err := private.Decode(&order); err != nil || order.Order.OrderID != 42 ||
		order.Order.OrderStatus != OrderStatusPartiallyFilled || order.Order.RealizedProfit != "1" {
		t.Fatalf("order update = %+v, error = %v", order, err)
	}
	if _, err := DecodeMarketStreamMessage(corestream.Message{
		Type: corestream.MessageBinary, Data: []byte{1},
	}); err == nil {
		t.Fatal("DecodeMarketStreamMessage() accepted binary frame")
	}
}

func TestUSDMStreamHelpersAndRouteValidation(t *testing.T) {
	t.Parallel()

	book, err := BookTickerStream("BTCUSDT")
	if err != nil || book.Route != StreamRoutePublic || book.Name != "btcusdt@bookTicker" {
		t.Fatalf("BookTickerStream() = %+v, %v", book, err)
	}
	depth, err := PartialDepthStream("BTCUSDT", 20, 100*time.Millisecond)
	if err != nil || depth.Name != "btcusdt@depth20@100ms" {
		t.Fatalf("PartialDepthStream() = %+v, %v", depth, err)
	}
	mark, err := MarkPriceStream("BTCUSDT", time.Second)
	if err != nil || mark.Route != StreamRouteMarket || mark.Name != "btcusdt@markPrice@1s" {
		t.Fatalf("MarkPriceStream() = %+v, %v", mark, err)
	}
	monthly, err := KlineStream("BTCUSDT", Candle1Month)
	if err != nil || monthly.Name != "btcusdt@kline_1M" {
		t.Fatalf("KlineStream() = %+v, %v", monthly, err)
	}
	if _, _, err := validateMarketSubscriptions(
		[]StreamSubscription{book, mark}, "", true,
	); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("mixed route validation error = %v", err)
	}
	if _, err := PartialDepthStream("BTCUSDT", 15, 0); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("PartialDepthStream(invalid) error = %v", err)
	}
}

func newTestUSDMStreamClient(
	t *testing.T,
	connector corestream.Connector,
	restClient *Client,
	keepaliveInterval time.Duration,
) *StreamClient {
	t.Helper()
	client, err := NewStreamClient(StreamClientConfig{
		Connector: connector, RESTClient: restClient, DefaultEgressRouteID: "route-a",
		PublicStreamURL:        "ws://stream.example.test/public/stream",
		RegularMarketStreamURL: "ws://stream.example.test/market/stream",
		PrivateStreamURL:       "ws://stream.example.test/private/ws",
		AllowInsecureWebSocket: true, Backoff: func(int) time.Duration { return 0 },
		PingInterval: time.Hour, PingTimeout: time.Second,
		SubscriptionInterval:       time.Millisecond,
		ListenKeyKeepaliveInterval: keepaliveInterval,
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client
}

func waitForUSDMWebSocketWrite(
	t *testing.T,
	connection *usdmWebSocketTestConnection,
) []byte {
	t.Helper()
	select {
	case payload := <-connection.writes:
		return payload
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Binance USD-M WebSocket write")
		return nil
	}
}

func waitForUSDMConnection(t *testing.T, connected <-chan struct{}) {
	t.Helper()
	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Binance USD-M WebSocket connection")
	}
}

func decodeUSDMMarketCommand(t *testing.T, payload []byte) usdmMarketCommand {
	t.Helper()
	var command usdmMarketCommand
	if err := json.Unmarshal(payload, &command); err != nil {
		t.Fatalf("decode Binance USD-M market command: %v", err)
	}
	return command
}

func assertUSDMMarketCommand(
	t *testing.T,
	command usdmMarketCommand,
	method string,
	params []string,
) {
	t.Helper()
	if command.Method != method || command.ID == 0 || !slices.Equal(command.Params, params) {
		t.Fatalf("market command = %+v", command)
	}
}
