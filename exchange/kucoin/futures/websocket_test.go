package futures

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

type kucoinWebSocketReadResult struct {
	message corestream.Message
	err     error
}

type kucoinWebSocketTestConnection struct {
	reads     chan kucoinWebSocketReadResult
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newKuCoinWebSocketTestConnection() *kucoinWebSocketTestConnection {
	return &kucoinWebSocketTestConnection{
		reads: make(chan kucoinWebSocketReadResult, 16), writes: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (connection *kucoinWebSocketTestConnection) Read(
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

func (connection *kucoinWebSocketTestConnection) Write(
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

func (connection *kucoinWebSocketTestConnection) Ping(context.Context) error { return nil }

func (connection *kucoinWebSocketTestConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type kucoinWebSocketTestConnector struct {
	mu          sync.Mutex
	connections []*kucoinWebSocketTestConnection
	routes      []transport.EgressRouteID
	requests    []corestream.DialRequest
}

func (connector *kucoinWebSocketTestConnector) Connect(
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
		return nil, errors.New("no KuCoin Futures test connection")
	}
	connection := connector.connections[0]
	connector.connections = connector.connections[1:]
	return connection, nil
}

func (connector *kucoinWebSocketTestConnector) snapshot() (
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

type kucoinStreamCommand struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	Topic          string `json:"topic"`
	PrivateChannel bool   `json:"privateChannel"`
	Response       bool   `json:"response"`
}

func TestWebSocketTokensUseSelectedRoutesAndPrivateAuthentication(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v1/bullet-private" &&
			!verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":"400005","msg":"Invalid signature"}`, http.StatusUnauthorized)
			return
		}
		if request.Method != http.MethodPost ||
			request.URL.Path != "/api/v1/bullet-public" && request.URL.Path != "/api/v1/bullet-private" {
			http.NotFound(writer, request)
			return
		}
		_, _ = io.WriteString(writer, `{"code":"200000","data":{"token":"issued-token","instanceServers":[{"endpoint":"wss://ws-api-futures.kucoin.com/","encrypt":true,"protocol":"websocket","pingInterval":18000,"pingTimeout":10000}]}}`)
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newTestClient(
		t, server.URL, sender, provider, []transport.EgressRouteID{"route-a", "route-b"},
		func() time.Time { return fixedNow },
	)
	publicToken, err := client.PublicWebSocketToken(
		context.Background(), trade.WithEgressRoute("route-b"),
	)
	if err != nil || publicToken.Token != "issued-token" || len(publicToken.Servers) != 1 ||
		len(publicToken.Raw) == 0 {
		t.Fatalf("PublicWebSocketToken() = %+v, error = %v", publicToken, err)
	}
	privateToken, err := client.PrivateWebSocketToken(
		context.Background(), trade.WithEgressRoute("route-b"),
	)
	if err != nil || privateToken.Token != "issued-token" || len(privateToken.Raw) == 0 {
		t.Fatalf("PrivateWebSocketToken() = %+v, error = %v", privateToken, err)
	}
	if routes := sender.snapshot(); !slices.Equal(
		routes, []transport.EgressRouteID{"route-b", "route-b"},
	) {
		t.Fatalf("sender routes = %v", routes)
	}
	calls, apiKey, secret, passphrase := provider.snapshot()
	if calls != 1 || !allZero(apiKey) || !allZero(secret) || !allZero(passphrase) {
		t.Fatalf(
			"provider calls = %d, key zero = %v, secret zero = %v, passphrase zero = %v",
			calls, allZero(apiKey), allZero(secret), allZero(passphrase),
		)
	}
	publicSnapshot, err := limiter.Snapshot("kucoin-futures:route:route-b:public:30seconds")
	if err != nil || publicSnapshot.Used != 10 {
		t.Fatalf("public limiter snapshot = %+v, error = %v", publicSnapshot, err)
	}
	futuresSnapshot, err := limiter.Snapshot("kucoin-futures:account:kucoin-main:futures:30seconds")
	if err != nil || futuresSnapshot.Used != 10 {
		t.Fatalf("Futures limiter snapshot = %+v, error = %v", futuresSnapshot, err)
	}
}

func TestKuCoinFuturesPublicStreamRefreshesTokenAndSubscriptions(t *testing.T) {
	t.Parallel()

	var tokenCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/bullet-public" {
			http.NotFound(writer, request)
			return
		}
		index := tokenCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"code":"200000","data":{"token":"token-%d","instanceServers":[{"endpoint":"ws://stream.example.test/endpoint","encrypt":false,"protocol":"websocket","pingInterval":18000,"pingTimeout":10000}]}}`, index)
	}))
	defer server.Close()

	first := newKuCoinWebSocketTestConnection()
	second := newKuCoinWebSocketTestConnection()
	connector := &kucoinWebSocketTestConnector{
		connections: []*kucoinWebSocketTestConnection{first, second},
	}
	streamClient, sender := newTestKuCoinStreamClient(t, server.URL, connector)
	public, err := streamClient.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{
		{Channel: StreamChannelTicker, Symbol: "XBTUSDTM"},
	}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var ticker StreamTicker
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Channel != StreamChannelTicker {
				return nil
			}
			if err := message.Decode(&ticker); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	firstCommand := decodeKuCoinStreamCommand(t, waitForKuCoinWebSocketWrite(t, first))
	assertKuCoinStreamCommand(
		t, firstCommand, "subscribe", "/contractMarket/tickerV2:XBTUSDTM", false,
	)
	first.reads <- kucoinWebSocketReadResult{err: errors.New("connection lost")}
	secondCommand := decodeKuCoinStreamCommand(t, waitForKuCoinWebSocketWrite(t, second))
	assertKuCoinStreamCommand(
		t, secondCommand, "subscribe", "/contractMarket/tickerV2:XBTUSDTM", false,
	)
	second.reads <- kucoinWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"type":"message","topic":"/contractMarket/tickerV2:XBTUSDTM","subject":"tickerV2","sn":101,"data":{"symbol":"XBTUSDTM","sequence":101,"bestBidSize":3,"bestBidPrice":"63999","bestAskPrice":"64001","bestAskSize":"4","ts":1700000000000000000}}`),
	}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("public Run() did not finish")
	}
	if ticker.BestBid != "63999" || ticker.BestAskSize != "4" ||
		ticker.Timestamp != 1_700_000_000_000_000_000 {
		t.Fatalf("ticker = %+v", ticker)
	}
	if public.Generation() != 2 || tokenCalls.Load() != 2 {
		t.Fatalf("generation = %d, token calls = %d", public.Generation(), tokenCalls.Load())
	}
	_ = public.Close()
	if routes := sender.snapshot(); !slices.Equal(
		routes, []transport.EgressRouteID{"route-b", "route-b"},
	) {
		t.Fatalf("token routes = %v", routes)
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) || len(requests) != 2 {
		t.Fatalf("stream routes = %v, requests = %d", routes, len(requests))
	}
	for index, request := range requests {
		endpoint, parseErr := url.Parse(request.Endpoint)
		if parseErr != nil {
			t.Fatalf("parse dial request: %v", parseErr)
		}
		wantIndex := strconv.Itoa(index + 1)
		if endpoint.Scheme != "ws" || endpoint.Host != "stream.example.test" ||
			endpoint.Path != "/endpoint" || endpoint.Query().Get("token") != "token-"+wantIndex ||
			endpoint.Query().Get("connectId") != "connect-"+wantIndex || len(request.Header) != 0 {
			t.Fatalf("dial request %d = %+v", index, request)
		}
	}
}

func TestKuCoinFuturesPrivateStreamAuthenticatesAndDecodesOrder(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/bullet-private" {
			http.NotFound(writer, request)
			return
		}
		if !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":"400005","msg":"Invalid signature"}`, http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(writer, `{"code":"200000","data":{"token":"private-token","instanceServers":[{"endpoint":"ws://private.example.test/endpoint","encrypt":false,"protocol":"websocket","pingInterval":18000,"pingTimeout":10000}]}}`)
	}))
	defer server.Close()

	connection := newKuCoinWebSocketTestConnection()
	connector := &kucoinWebSocketTestConnector{
		connections: []*kucoinWebSocketTestConnection{connection},
	}
	sender := &directSender{}
	provider := &recordingProvider{}
	restClient, _ := newTestClient(
		t, server.URL, sender, provider, []transport.EgressRouteID{"route-a", "route-b"},
		func() time.Time { return fixedNow },
	)
	streamClient, err := NewStreamClient(StreamClientConfig{
		Connector: connector, RESTClient: restClient, DefaultEgressRouteID: "route-a",
		AllowInsecureWebSocket: true, SubscriptionInterval: time.Nanosecond,
		ConnectIDSource: func() (string, error) { return "private-connect", nil },
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	private, err := streamClient.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{
		{Channel: StreamChannelOrders, Symbol: "XBTUSDTM"},
		{Channel: StreamChannelBalance},
		{Channel: StreamChannelPositions},
	}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var order StreamOrder
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Channel != StreamChannelOrders {
				return nil
			}
			if !message.Private {
				return errors.New("private Futures order message was classified as public")
			}
			if err := message.Decode(&order); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	commands := []kucoinStreamCommand{
		decodeKuCoinStreamCommand(t, waitForKuCoinWebSocketWrite(t, connection)),
		decodeKuCoinStreamCommand(t, waitForKuCoinWebSocketWrite(t, connection)),
		decodeKuCoinStreamCommand(t, waitForKuCoinWebSocketWrite(t, connection)),
	}
	wantTopics := []string{
		"/contractAccount/wallet", "/contractMarket/tradeOrders:XBTUSDTM", "/contract/positionAll",
	}
	for index, command := range commands {
		if command.Topic != wantTopics[index] || command.Type != "subscribe" ||
			!command.PrivateChannel || !command.Response {
			t.Fatalf("private command %d = %+v", index, command)
		}
	}
	connection.reads <- kucoinWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"type":"message","topic":"/contractMarket/tradeOrders:XBTUSDTM","subject":"symbolOrderChange","userId":"user-1","data":{"symbol":"XBTUSDTM","orderType":"limit","tradeType":"trade","side":"buy","canceledSize":"0","orderId":"order-1","clientOid":"strategy-1","positionSide":"BOTH","liquidity":"maker","marginMode":"ISOLATED","type":"match","orderTime":1700000000000000000,"size":"2","filledSize":"1","price":"64000","matchPrice":"64000","matchSize":"1","remainSize":"1","tradeId":"trade-1","status":"open","ts":1700000000000000001}}`),
	}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("private Run() did not finish")
	}
	if order.OrderID != "order-1" || order.EventType != "match" ||
		order.FilledSize != "1" || order.MarginMode != MarginModeIsolated {
		t.Fatalf("order = %+v", order)
	}
	if routes := sender.snapshot(); !slices.Equal(routes, []transport.EgressRouteID{"route-b"}) {
		t.Fatalf("private token routes = %v", routes)
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b"}) || len(requests) != 1 {
		t.Fatalf("private stream routes = %v, requests = %d", routes, len(requests))
	}
	endpoint, err := url.Parse(requests[0].Endpoint)
	if err != nil || endpoint.Query().Get("token") != "private-token" ||
		endpoint.Query().Get("connectId") != "private-connect" {
		t.Fatalf("private dial request = %+v, error = %v", requests[0], err)
	}
	calls, apiKey, secret, passphrase := provider.snapshot()
	if calls != 1 || !allZero(apiKey) || !allZero(secret) || !allZero(passphrase) {
		t.Fatalf("private provider calls = %d", calls)
	}
	_ = private.Close()
}

func TestKuCoinFuturesStreamDynamicCommandsAndFailedAckRecovery(t *testing.T) {
	t.Parallel()

	server := newKuCoinWebSocketTokenServer(t)
	defer server.Close()
	first := newKuCoinWebSocketTestConnection()
	second := newKuCoinWebSocketTestConnection()
	connector := &kucoinWebSocketTestConnector{
		connections: []*kucoinWebSocketTestConnection{first, second},
	}
	streamClient, _ := newTestKuCoinStreamClient(t, server.URL, connector)
	ticker := StreamSubscription{Channel: StreamChannelTicker, Symbol: "XBTUSDTM"}
	tradeSubscription := StreamSubscription{Channel: StreamChannelTrade, Symbol: "ETHUSDTM"}
	public, err := streamClient.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{ticker}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	failed := make(chan struct{}, 1)
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Type == "error" {
				failed <- struct{}{}
			}
			return nil
		})
	}()
	_ = waitForKuCoinWebSocketWrite(t, first)
	if err := public.Subscribe(context.Background(), tradeSubscription); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	subscribe := decodeKuCoinStreamCommand(t, waitForKuCoinWebSocketWrite(t, first))
	assertKuCoinStreamCommand(
		t, subscribe, "subscribe", "/contractMarket/execution:ETHUSDTM", false,
	)
	first.reads <- kucoinWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"id":"` + subscribe.ID + `","type":"error","code":400100,"msg":"invalid topic"}`),
	}}
	select {
	case <-failed:
	case <-time.After(2 * time.Second):
		t.Fatal("failed acknowledgement was not delivered")
	}
	first.reads <- kucoinWebSocketReadResult{err: errors.New("connection lost")}
	recovered := decodeKuCoinStreamCommand(t, waitForKuCoinWebSocketWrite(t, second))
	assertKuCoinStreamCommand(
		t, recovered, "subscribe", "/contractMarket/tickerV2:XBTUSDTM", false,
	)
	select {
	case unexpected := <-second.writes:
		t.Fatalf("unexpected recovered command = %s", unexpected)
	case <-time.After(25 * time.Millisecond):
	}
	if err := public.Unsubscribe(context.Background(), ticker); err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
	unsubscribe := decodeKuCoinStreamCommand(t, waitForKuCoinWebSocketWrite(t, second))
	assertKuCoinStreamCommand(
		t, unsubscribe, "unsubscribe", "/contractMarket/tickerV2:XBTUSDTM", false,
	)
	cancel()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dynamic stream Run() did not finish")
	}
}

func TestKuCoinFuturesConnectionUsesApplicationPing(t *testing.T) {
	t.Parallel()

	next := newKuCoinWebSocketTestConnection()
	var nextID atomic.Int64
	connection := &kucoinConnection{next: next, nextID: &nextID, pong: make(chan struct{}, 1)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- connection.Ping(ctx) }()
	ping := decodeKuCoinStreamCommand(t, waitForKuCoinWebSocketWrite(t, next))
	if ping.ID != "1" || ping.Type != "ping" {
		t.Fatalf("ping = %+v", ping)
	}
	next.reads <- kucoinWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText, Data: []byte(`{"id":"stale","type":"pong"}`),
	}}
	message, err := connection.Read(ctx)
	if err != nil || !isKuCoinPong(message.Data) {
		t.Fatalf("Read() message = %s, error = %v", message.Data, err)
	}
	select {
	case pingErr := <-done:
		t.Fatalf("Ping() accepted mismatched pong: %v", pingErr)
	case <-time.After(10 * time.Millisecond):
	}
	next.reads <- kucoinWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText, Data: []byte(`{"id":"1","type":"pong"}`),
	}}
	message, err = connection.Read(ctx)
	if err != nil || !isKuCoinPong(message.Data) {
		t.Fatalf("Read() message = %s, error = %v", message.Data, err)
	}
	select {
	case pingErr := <-done:
		if pingErr != nil {
			t.Fatalf("Ping() error = %v", pingErr)
		}
	case <-ctx.Done():
		t.Fatal("Ping() did not receive pong")
	}
}

func TestDecodeKuCoinFuturesStreamMessages(t *testing.T) {
	t.Parallel()

	levelMessage, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"type":"message","topic":"/contractMarket/level2:XBTUSDTM","subject":"level2","sn":101,"data":{"sequence":101,"change":"64000,buy,2","timestamp":1700000000000}}`,
	)})
	if err != nil || levelMessage.Channel != StreamChannelLevel2 || levelMessage.Sequence != 101 {
		t.Fatalf("DecodeStreamMessage() = %+v, error = %v", levelMessage, err)
	}
	var level StreamLevel2
	if err := levelMessage.Decode(&level); err != nil || level.Sequence != 101 ||
		level.Change.Price != "64000" || level.Change.Side != SideBuy || level.Change.Size != "2" {
		t.Fatalf("level2 change = %+v, error = %v", level, err)
	}
	candleMessage, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"type":"message","topic":"/contractMarket/limitCandle:XBTUSDTM_1min","subject":"candle.stick","data":{"symbol":"XBTUSDTM","candles":["1700000000","63000","64000","65000","62000","10.5","672000"],"time":1700000000123}}`,
	)})
	var candle StreamCandle
	if err != nil || candleMessage.Decode(&candle) != nil || candle.Candles.Timestamp != 1_700_000_000 ||
		candle.Candles.Close != "64000" || candle.Candles.Volume != "10.5" {
		t.Fatalf("candle = %+v, envelope error = %v", candle, err)
	}
	tradeMessage, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"type":"message","topic":"/contractMarket/execution:XBTUSDTM","subject":"match","sn":102,"data":{"symbol":"XBTUSDTM","sequence":102,"side":"sell","size":2,"price":"64000","takerOrderId":"taker","makerOrderId":"maker","tradeId":"trade-1","ts":1700000000000000000}}`,
	)})
	var matched StreamTrade
	if err != nil || tradeMessage.Decode(&matched) != nil ||
		matched.Timestamp != 1_700_000_000_000_000_000 || matched.Size != "2" {
		t.Fatalf("trade = %+v, envelope error = %v", matched, err)
	}
	balanceMessage, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"type":"message","topic":"/contractAccount/wallet","subject":"walletBalance.change","data":{"equity":"1000","availableBalance":"900","walletBalance":"990","currency":"USDT","timestamp":"1700000000000"}}`,
	)})
	var balance StreamBalance
	if err != nil || !balanceMessage.Private || balanceMessage.Decode(&balance) != nil ||
		balance.AvailableBalance != "900" || balance.Timestamp != "1700000000000" {
		t.Fatalf("balance = %+v, message = %+v, error = %v", balance, balanceMessage, err)
	}
	positionMessage, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"type":"message","topic":"/contract/positionAll","subject":"position.change","data":{"symbol":"XBTUSDTM","currentQty":-2,"markPrice":64000,"posMargin":"100","avgEntryPrice":"65000","liquidationPrice":"70000","bankruptPrice":"71000","settleCurrency":"USDT","realisedPnl":"1","unrealisedPnl":"2","leverage":10,"marginMode":"CROSS","positionSide":"BOTH"}}`,
	)})
	var position StreamPosition
	if err != nil || !positionMessage.Private || positionMessage.Decode(&position) != nil ||
		position.CurrentQuantity != -2 || position.MarkPrice != "64000" ||
		position.MarginMode != MarginModeCross {
		t.Fatalf("position = %+v, message = %+v, error = %v", position, positionMessage, err)
	}
	control, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"id":42,"type":"error","code":400100,"msg":"invalid topic"}`,
	)})
	if err != nil || control.ID != "42" || control.ErrorCode != "400100" ||
		control.ErrorMessage != "invalid topic" {
		t.Fatalf("control = %+v, error = %v", control, err)
	}
}

func TestKuCoinFuturesStreamSubscriptionValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		subscription StreamSubscription
		private      bool
	}{
		{name: "public symbol", subscription: StreamSubscription{Channel: StreamChannelTicker, Symbol: "xbtusdtm"}},
		{name: "candle interval", subscription: StreamSubscription{Channel: StreamChannelCandles, Symbol: "XBTUSDTM", Interval: "6min"}},
		{name: "public private channel", subscription: StreamSubscription{Channel: StreamChannelOrders}, private: false},
		{name: "private public channel", subscription: StreamSubscription{Channel: StreamChannelTicker, Symbol: "XBTUSDTM"}, private: true},
		{name: "balance symbol", subscription: StreamSubscription{Channel: StreamChannelBalance, Symbol: "XBTUSDTM"}, private: true},
		{name: "private interval", subscription: StreamSubscription{Channel: StreamChannelPositions, Interval: StreamCandle1Minute}, private: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateStreamSubscription(test.subscription, test.private); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("validation error = %v", err)
			}
		})
	}
}

func TestKuCoinFuturesPrivateStreamRejectsUnauthorizedRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{}
	restClient, _ := newTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, nil,
	)
	streamClient, err := NewStreamClient(StreamClientConfig{
		Connector: &kucoinWebSocketTestConnector{}, RESTClient: restClient,
		DefaultEgressRouteID: "route-a",
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	_, err = streamClient.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{
		{Channel: StreamChannelOrders},
	}}, trade.WithEgressRoute("route-b"))
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("PrivateStream() error = %v, want authorization", err)
	}
	calls, _, _, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func newTestKuCoinStreamClient(
	t *testing.T,
	baseURL string,
	connector corestream.Connector,
) (*StreamClient, *directSender) {
	t.Helper()
	sender := &directSender{}
	restClient, _ := newTestClient(
		t, baseURL, sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"}, nil,
	)
	var connectID atomic.Int64
	client, err := NewStreamClient(StreamClientConfig{
		Connector: connector, RESTClient: restClient, DefaultEgressRouteID: "route-a",
		ProPublicWebSocketURL:  "ws://pro-futures.example.test",
		AllowInsecureWebSocket: true, SubscriptionInterval: time.Nanosecond,
		Backoff: func(int) time.Duration { return 0 },
		ConnectIDSource: func() (string, error) {
			return "connect-" + strconv.FormatInt(connectID.Add(1), 10), nil
		},
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client, sender
}

func newKuCoinWebSocketTokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/bullet-public" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"code":"200000","data":{"token":"issued-token","instanceServers":[{"endpoint":"ws://stream.example.test/endpoint","encrypt":false,"protocol":"websocket","pingInterval":18000,"pingTimeout":10000}]}}`)
	}))
}

func waitForKuCoinWebSocketWrite(
	t *testing.T,
	connection *kucoinWebSocketTestConnection,
) []byte {
	t.Helper()
	select {
	case payload := <-connection.writes:
		return payload
	case <-time.After(2 * time.Second):
		t.Fatal("KuCoin Futures WebSocket write timed out")
		return nil
	}
}

func decodeKuCoinStreamCommand(t *testing.T, payload []byte) kucoinStreamCommand {
	t.Helper()
	var command kucoinStreamCommand
	if err := json.Unmarshal(payload, &command); err != nil {
		t.Fatalf("decode KuCoin Futures stream command: %v", err)
	}
	return command
}

func assertKuCoinStreamCommand(
	t *testing.T,
	command kucoinStreamCommand,
	wantType string,
	wantTopic string,
	wantPrivate bool,
) {
	t.Helper()
	if command.ID == "" || command.Type != wantType || command.Topic != wantTopic ||
		command.PrivateChannel != wantPrivate || !command.Response {
		t.Fatalf("stream command = %+v", command)
	}
}
