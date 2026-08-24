package kucoin

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
		return nil, errors.New("no KuCoin test connection")
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
		_, _ = io.WriteString(writer, `{"code":"200000","data":{"token":"issued-token","instanceServers":[{"endpoint":"wss://ws-api-spot.kucoin.com/","encrypt":true,"protocol":"websocket","pingInterval":18000,"pingTimeout":10000}]}}`)
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
	publicSnapshot, err := limiter.Snapshot("kucoin:route:route-b:public:30seconds")
	if err != nil || publicSnapshot.Used != 10 {
		t.Fatalf("public limiter snapshot = %+v, error = %v", publicSnapshot, err)
	}
	spotSnapshot, err := limiter.Snapshot("kucoin:account:kucoin-main:spot:30seconds")
	if err != nil || spotSnapshot.Used != 10 {
		t.Fatalf("spot limiter snapshot = %+v, error = %v", spotSnapshot, err)
	}
}

func TestKuCoinPublicStreamRefreshesTokenAndSubscriptions(t *testing.T) {
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
		{Channel: StreamChannelTicker, Symbol: "BTC-USDT"},
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
	assertKuCoinStreamCommand(t, firstCommand, "subscribe", "/market/ticker:BTC-USDT", false)
	first.reads <- kucoinWebSocketReadResult{err: errors.New("connection lost")}
	secondCommand := decodeKuCoinStreamCommand(t, waitForKuCoinWebSocketWrite(t, second))
	assertKuCoinStreamCommand(t, secondCommand, "subscribe", "/market/ticker:BTC-USDT", false)
	second.reads <- kucoinWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"type":"message","topic":"/market/ticker:BTC-USDT","subject":"trade.ticker","data":{"sequence":"1","price":"64000","size":"0.01","bestAsk":"64001","bestAskSize":"2","bestBid":"64000","bestBidSize":"1","time":1700000000000000000}}`),
	}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("public Run() did not finish")
	}
	if ticker.Price != "64000" || ticker.BestAsk != "64001" || ticker.Time != 1_700_000_000_000_000_000 {
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

func TestKuCoinPrivateStreamAuthenticatesAndDecodesOrder(t *testing.T) {
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
		{Channel: StreamChannelOrders}, {Channel: StreamChannelBalance},
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
				return errors.New("private order message was classified as public")
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
	}
	if commands[0].Topic != "/account/balance" || commands[1].Topic != "/spotMarket/tradeOrdersV2" {
		t.Fatalf("private commands = %+v", commands)
	}
	for _, command := range commands {
		if command.Type != "subscribe" || !command.PrivateChannel || !command.Response {
			t.Fatalf("private command = %+v", command)
		}
	}
	connection.reads <- kucoinWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"type":"message","topic":"/spotMarket/tradeOrdersV2","subject":"orderChange","channelType":"private","userId":"user-1","data":{"orderId":"order-1","clientOid":"strategy-1","symbol":"BTC-USDT","type":"match","orderType":"limit","side":"buy","price":"64000","size":"0.01","filledSize":"0.005","remainSize":"0.005","tradeType":"TRADE","status":"open","ts":1700000000000000000}}`),
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
		order.FilledSize != "0.005" || order.TradeType != "TRADE" {
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

func TestKuCoinStreamDynamicCommandsAndFailedAckRecovery(t *testing.T) {
	t.Parallel()

	server := newKuCoinWebSocketTokenServer(t)
	defer server.Close()
	first := newKuCoinWebSocketTestConnection()
	second := newKuCoinWebSocketTestConnection()
	connector := &kucoinWebSocketTestConnector{
		connections: []*kucoinWebSocketTestConnection{first, second},
	}
	streamClient, _ := newTestKuCoinStreamClient(t, server.URL, connector)
	ticker := StreamSubscription{Channel: StreamChannelTicker, Symbol: "BTC-USDT"}
	tradeSubscription := StreamSubscription{Channel: StreamChannelTrade, Symbol: "ETH-USDT"}
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
	assertKuCoinStreamCommand(t, subscribe, "subscribe", "/market/match:ETH-USDT", false)
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
	assertKuCoinStreamCommand(t, recovered, "subscribe", "/market/ticker:BTC-USDT", false)
	select {
	case unexpected := <-second.writes:
		t.Fatalf("unexpected recovered command = %s", unexpected)
	case <-time.After(25 * time.Millisecond):
	}
	if err := public.Unsubscribe(context.Background(), ticker); err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
	unsubscribe := decodeKuCoinStreamCommand(t, waitForKuCoinWebSocketWrite(t, second))
	assertKuCoinStreamCommand(t, unsubscribe, "unsubscribe", "/market/ticker:BTC-USDT", false)
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

func TestKuCoinConnectionUsesApplicationPing(t *testing.T) {
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

func TestDecodeKuCoinStreamDataAndControlMessages(t *testing.T) {
	t.Parallel()

	levelMessage, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"type":"message","topic":"/market/level2:BTC-USDT","subject":"trade.l2update","data":{"changes":{"asks":[["64001","2","101"]],"bids":[["64000","1","100"]]},"sequenceStart":100,"sequenceEnd":101,"symbol":"BTC-USDT","time":1700000000000}}`,
	)})
	if err != nil || levelMessage.Channel != StreamChannelLevel2 {
		t.Fatalf("DecodeStreamMessage() = %+v, error = %v", levelMessage, err)
	}
	var changes StreamLevel2Changes
	if err := levelMessage.Decode(&changes); err != nil || changes.SequenceEnd != 101 ||
		len(changes.Changes.Asks) != 1 || changes.Changes.Asks[0].Sequence != "101" {
		t.Fatalf("level2 changes = %+v, error = %v", changes, err)
	}
	tradeMessage, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"type":"message","topic":"/market/match:BTC-USDT","subject":"trade.l3match","data":{"makerOrderId":"maker","price":"64000","sequence":"102","side":"buy","size":"0.01","symbol":"BTC-USDT","takerOrderId":"taker","time":1700000000000000000,"tradeId":"trade-1","type":"match"}}`,
	)})
	var matched StreamTrade
	if err != nil || tradeMessage.Decode(&matched) != nil || matched.Time != 1_700_000_000_000_000_000 ||
		matched.TradeID != "trade-1" {
		t.Fatalf("trade = %+v, envelope error = %v", matched, err)
	}
	control, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"id":42,"type":"error","code":400100,"msg":"invalid topic"}`,
	)})
	if err != nil || control.ID != "42" || control.ErrorCode != "400100" ||
		control.ErrorMessage != "invalid topic" {
		t.Fatalf("control = %+v, error = %v", control, err)
	}
}

func TestKuCoinPrivateStreamRejectsUnauthorizedRouteBeforeSecretResolution(t *testing.T) {
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
		t.Fatal("KuCoin WebSocket write timed out")
		return nil
	}
}

func decodeKuCoinStreamCommand(t *testing.T, payload []byte) kucoinStreamCommand {
	t.Helper()
	var command kucoinStreamCommand
	if err := json.Unmarshal(payload, &command); err != nil {
		t.Fatalf("decode KuCoin stream command: %v", err)
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
