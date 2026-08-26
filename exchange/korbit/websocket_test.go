package korbit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
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

type korbitWebSocketReadResult struct {
	message corestream.Message
	err     error
}

type korbitWebSocketTestConnection struct {
	reads     chan korbitWebSocketReadResult
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newKorbitWebSocketTestConnection() *korbitWebSocketTestConnection {
	return &korbitWebSocketTestConnection{
		reads: make(chan korbitWebSocketReadResult, 16), writes: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (connection *korbitWebSocketTestConnection) Read(ctx context.Context) (corestream.Message, error) {
	select {
	case <-ctx.Done():
		return corestream.Message{}, ctx.Err()
	case <-connection.closed:
		return corestream.Message{}, corestream.ErrSessionClosed
	case result := <-connection.reads:
		return result.message, result.err
	}
}

func (connection *korbitWebSocketTestConnection) Write(
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

func (connection *korbitWebSocketTestConnection) Ping(context.Context) error { return nil }

func (connection *korbitWebSocketTestConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type korbitWebSocketTestConnector struct {
	mu          sync.Mutex
	connections []*korbitWebSocketTestConnection
	routes      []transport.EgressRouteID
	requests    []corestream.DialRequest
}

func (connector *korbitWebSocketTestConnector) Connect(
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
		return nil, errors.New("no Korbit test connection")
	}
	connection := connector.connections[0]
	connector.connections = connector.connections[1:]
	return connection, nil
}

func (connector *korbitWebSocketTestConnector) snapshot() (
	[]transport.EgressRouteID,
	[]corestream.DialRequest,
) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	requests := make([]corestream.DialRequest, len(connector.requests))
	for index, request := range connector.requests {
		requests[index] = corestream.DialRequest{Endpoint: request.Endpoint, Header: request.Header.Clone()}
	}
	return slices.Clone(connector.routes), requests
}

func TestKorbitPublicStreamReconnectsOnSameRouteAndDecodesTicker(t *testing.T) {
	t.Parallel()

	first := newKorbitWebSocketTestConnection()
	second := newKorbitWebSocketTestConnection()
	connector := &korbitWebSocketTestConnector{
		connections: []*korbitWebSocketTestConnection{first, second},
	}
	client := newTestKorbitStreamClient(t, connector, nil, nil)
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{
		{Channel: StreamChannelTicker, Symbols: []string{"btc_krw", "eth_krw"}},
		{Channel: StreamChannelOrderBook, Symbols: []string{"btc_krw"}, Level: "1000"},
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
	assertKorbitSubscriptions(t, waitForKorbitWebSocketWrite(t, first), "subscribe", 2)
	first.reads <- korbitWebSocketReadResult{err: errors.New("connection lost")}
	assertKorbitSubscriptions(t, waitForKorbitWebSocketWrite(t, second), "subscribe", 2)
	second.reads <- korbitWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"type":"ticker","timestamp":1700000002000,"symbol":"btc_krw","snapshot":true,"data":{"open":"63000000","high":"65000000","low":"62000000","close":"64000000","prevClose":"63000000","priceChange":"1000000","priceChangePercent":"1.58","volume":"10.5","quoteVolume":"670000000","bestBidPrice":"64000000","bestAskPrice":"64001000","lastTradedAt":1700000001000}}`),
	}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("public Run() did not finish")
	}
	if ticker.Close != "64000000" || ticker.BestBidPrice != "64000000" || ticker.LastTradedAt != 1700000001000 {
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
	for _, request := range requests {
		if request.Endpoint != "ws://public.example.test/v2/public" || len(request.Header) != 0 {
			t.Fatalf("public dial request = %+v", request)
		}
	}
}

func TestKorbitPrivateStreamRefreshesAuthenticationAndSubscriptions(t *testing.T) {
	t.Parallel()

	first := newKorbitWebSocketTestConnection()
	second := newKorbitWebSocketTestConnection()
	connector := &korbitWebSocketTestConnector{
		connections: []*korbitWebSocketTestConnection{first, second},
	}
	provider := &recordingProvider{}
	client := newTestKorbitStreamClient(
		t, connector, provider, []transport.EgressRouteID{"route-a", "route-b"},
	)
	private, err := client.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{
		{Channel: StreamChannelMyTrade, Symbols: []string{"btc_krw"}, AccountSeqs: []int{2}},
		{Channel: StreamChannelMyAsset, AccountSeqs: []int{2}},
	}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var event MyTradeEvent
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Channel != StreamChannelMyTrade {
				return nil
			}
			if err := message.Decode(&event); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	assertKorbitSubscriptions(t, waitForKorbitWebSocketWrite(t, first), "subscribe", 2)
	first.reads <- korbitWebSocketReadResult{err: errors.New("connection lost")}
	assertKorbitSubscriptions(t, waitForKorbitWebSocketWrite(t, second), "subscribe", 2)
	second.reads <- korbitWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"channelType":"myTrade","timestamp":1700000002000,"symbol":"btc_krw","trade":{"accountSeq":2,"trades":[{"tradeId":52,"orderId":1234,"side":"buy","price":"64000000","qty":"0.01","fee":"320","feeCurrency":"krw","filledAt":1700000001000,"isTaker":true}]}}`),
	}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("private Run() did not finish")
	}
	if event.AccountSeq == nil || *event.AccountSeq != 2 || len(event.Trades) != 1 ||
		event.Trades[0].TradeID != 52 || event.Trades[0].Fee != "320" {
		t.Fatalf("my trade event = %+v", event)
	}
	calls, key, secret := provider.snapshot()
	if calls != 2 || !allZero(key) || !allZero(secret) {
		t.Fatalf("provider calls = %d, key zero = %v, secret zero = %v", calls, allZero(key), allZero(secret))
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) || len(requests) != 2 {
		t.Fatalf("routes = %v, requests = %d", routes, len(requests))
	}
	for index, request := range requests {
		assertKorbitPrivateHandshake(t, request, 1700000000123+int64(index))
	}
	if private.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", private.Generation())
	}
	_ = private.Close()
}

func TestKorbitStreamDynamicCommandsAndFailedAckRecovery(t *testing.T) {
	t.Parallel()

	first := newKorbitWebSocketTestConnection()
	second := newKorbitWebSocketTestConnection()
	connector := &korbitWebSocketTestConnector{
		connections: []*korbitWebSocketTestConnection{first, second},
	}
	client := newTestKorbitStreamClient(t, connector, nil, nil)
	ticker := StreamSubscription{Channel: StreamChannelTicker, Symbols: []string{"btc_krw"}}
	tradeSubscription := StreamSubscription{Channel: StreamChannelTrade, Symbols: []string{"eth_krw"}}
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{ticker}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	ack := make(chan struct{}, 1)
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Status == "fail" {
				ack <- struct{}{}
			}
			return nil
		})
	}()
	_ = waitForKorbitWebSocketWrite(t, first)
	if err := public.Subscribe(context.Background(), tradeSubscription); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	items := decodeKorbitCommands(t, waitForKorbitWebSocketWrite(t, first))
	if len(items) != 1 || items[0].Method != "subscribe" || items[0].Type != StreamChannelTrade {
		t.Fatalf("dynamic subscribe = %+v", items)
	}
	first.reads <- korbitWebSocketReadResult{message: corestream.Message{Data: []byte(
		`{"requestId":` + jsonNumber(items[0].RequestID) + `,"status":"fail","code":"INVALID_SYMBOL","message":"invalid symbol"}`,
	)}}
	select {
	case <-ack:
	case <-time.After(2 * time.Second):
		t.Fatal("failed acknowledgement was not delivered")
	}
	first.reads <- korbitWebSocketReadResult{err: errors.New("connection lost")}
	recovered := decodeKorbitCommands(t, waitForKorbitWebSocketWrite(t, second))
	if len(recovered) != 1 || recovered[0].Type != StreamChannelTicker {
		t.Fatalf("recovered subscriptions = %+v", recovered)
	}
	if err := public.Unsubscribe(context.Background(), ticker); err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
	unsubscribed := decodeKorbitCommands(t, waitForKorbitWebSocketWrite(t, second))
	if len(unsubscribed) != 1 || unsubscribed[0].Method != "unsubscribe" || unsubscribed[0].Type != StreamChannelTicker {
		t.Fatalf("dynamic unsubscribe = %+v", unsubscribed)
	}
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

func TestKorbitDecodePublicPrivateAndControlMessages(t *testing.T) {
	t.Parallel()

	control, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"requestId":7,"status":"success"}`,
	)})
	if err != nil || control.RequestID == nil || *control.RequestID != 7 || control.Status != "success" {
		t.Fatalf("control = %+v, error = %v", control, err)
	}
	failure, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"requestId":8,"status":"fail","code":"INVALID_SYMBOL","message":"bad symbol"}`,
	)})
	if err != nil || failure.Code != "INVALID_SYMBOL" || failure.ErrorMessage != "bad symbol" {
		t.Fatalf("failure = %+v, error = %v", failure, err)
	}
	serverError, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"status":"error","message":"server error"}`,
	)})
	if err != nil || serverError.Status != "error" || serverError.RequestID != nil {
		t.Fatalf("server error = %+v, error = %v", serverError, err)
	}

	tests := []struct {
		name    string
		payload string
		decode  func(StreamMessage) error
	}{
		{
			name:    "order book",
			payload: `{"type":"orderbook","timestamp":2,"symbol":"btc_krw","snapshot":true,"data":{"timestamp":1,"bids":[{"price":"10","qty":"2"}],"asks":[{"price":"11","qty":"3","amt":"33"}]}}`,
			decode: func(message StreamMessage) error {
				var value StreamOrderBook
				if err := message.Decode(&value); err != nil {
					return err
				}
				if value.Bids[0].Price != "10" || value.Asks[0].Amount != "33" {
					return errors.New("unexpected order book")
				}
				return nil
			},
		},
		{
			name:    "public trade",
			payload: `{"type":"trade","timestamp":2,"symbol":"btc_krw","snapshot":false,"data":[{"timestamp":1,"price":"10","qty":"0.1","isBuyerTaker":true,"tradeId":52}]}`,
			decode: func(message StreamMessage) error {
				var value []StreamPublicTrade
				if err := message.Decode(&value); err != nil {
					return err
				}
				if len(value) != 1 || value[0].TradeID != 52 || !value[0].IsBuyerTaker {
					return errors.New("unexpected public trade")
				}
				return nil
			},
		},
		{
			name:    "my order",
			payload: `{"channelType":"myOrder","timestamp":2,"symbol":"btc_krw","order":{"accountSeq":1,"orders":[{"orderId":1234,"status":"partiallyFilled","side":"buy","orderType":"limit","timeInForce":"gtc","price":"10","qty":"2","filledQty":"1","filledAmt":"10","avgPrice":"10","createdAt":1,"lastFilledAt":2,"clientOrderId":"strategy-1"}]}}`,
			decode: func(message StreamMessage) error {
				var value MyOrderEvent
				if err := message.Decode(&value); err != nil {
					return err
				}
				if len(value.Orders) != 1 || value.Orders[0].Status != StreamOrderStatusPartiallyFilled ||
					value.Orders[0].ClientOrderID != "strategy-1" {
					return errors.New("unexpected my order")
				}
				return nil
			},
		},
		{
			name:    "my asset",
			payload: `{"channelType":"myAsset","timestamp":2,"asset":{"accountSeq":1,"assets":[{"currency":"btc","balance":"10","available":"7","tradeInUse":"2","withdrawalInUse":"1","avgPrice":"50000","updatedAt":1}]}}`,
			decode: func(message StreamMessage) error {
				var value MyAssetEvent
				if err := message.Decode(&value); err != nil {
					return err
				}
				if len(value.Assets) != 1 || value.Assets[0].Available != "7" || value.Assets[0].UpdatedAt != 1 {
					return errors.New("unexpected my asset")
				}
				return nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message, decodeErr := DecodeStreamMessage(corestream.Message{Data: []byte(test.payload)})
			if decodeErr != nil {
				t.Fatalf("DecodeStreamMessage() error = %v", decodeErr)
			}
			if len(message.Raw) == 0 || len(message.Data) == 0 {
				t.Fatalf("message = %+v", message)
			}
			if err := test.decode(message); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := DecodeStreamMessage(corestream.Message{Data: []byte(`[]`)}); err == nil {
		t.Fatal("array stream message error = nil")
	}
	if err := control.Decode(&StreamTicker{}); err == nil {
		t.Fatal("control Decode() error = nil")
	}
}

func TestKorbitStreamValidationAndRouteCheckBeforeSecret(t *testing.T) {
	t.Parallel()

	connection := newKorbitWebSocketTestConnection()
	connector := &korbitWebSocketTestConnector{
		connections: []*korbitWebSocketTestConnection{connection},
	}
	publicClient := newTestKorbitStreamClient(t, connector, nil, nil)
	invalidPublic := []StreamRequest{
		{},
		{Subscriptions: []StreamSubscription{{Channel: StreamChannelMyOrder, Symbols: []string{"btc_krw"}}}},
		{Subscriptions: []StreamSubscription{{Channel: StreamChannelOrderBook, Symbols: []string{"btc_krw"}, Level: "0"}}},
		{Subscriptions: []StreamSubscription{{Channel: StreamChannelTicker, Symbols: []string{"btc_krw", "btc_krw"}}}},
	}
	for index, request := range invalidPublic {
		if _, err := publicClient.PublicStream(request); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("public validation %d error = %v", index, err)
		}
	}
	if _, err := publicClient.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelMyAsset,
	}}}); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("private without credentials error = %v", err)
	}

	provider := &recordingProvider{}
	privateClient := newTestKorbitStreamClient(
		t, connector, provider, []transport.EgressRouteID{"route-a"},
	)
	if _, err := privateClient.PrivateStream(
		StreamRequest{Subscriptions: []StreamSubscription{{Channel: StreamChannelMyAsset}}},
		trade.WithEgressRoute("route-b"),
	); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("disallowed route error = %v", err)
	}
	calls, _, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
	if _, err := privateClient.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelTicker, Symbols: []string{"btc_krw"},
	}}}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("private public-channel error = %v", err)
	}
}

func newTestKorbitStreamClient(
	t *testing.T,
	connector corestream.Connector,
	provider *recordingProvider,
	allowedRoutes []transport.EgressRouteID,
) *StreamClient {
	t.Helper()
	var descriptor *credential.Descriptor
	var credentialProvider credential.Provider
	if provider != nil {
		descriptor = &credential.Descriptor{
			AccountID: "korbit-account", Exchange: model.ExchangeKorbit,
			SecretRef: "secret/korbit", Permissions: []credential.Permission{credential.PermissionRead},
			AllowedEgressRouteIDs: allowedRoutes,
		}
		credentialProvider = provider
	}
	var nowMu sync.Mutex
	nowCalls := int64(0)
	client, err := NewStreamClient(StreamClientConfig{
		Connector: connector, Credentials: descriptor, CredentialProvider: credentialProvider,
		DefaultEgressRouteID:   "route-a",
		PublicWebSocketURL:     "ws://public.example.test/v2/public",
		PrivateWebSocketURL:    "ws://private.example.test/v2/private",
		AllowInsecureWebSocket: true,
		Now: func() time.Time {
			nowMu.Lock()
			defer nowMu.Unlock()
			value := time.UnixMilli(1700000000123 + nowCalls)
			nowCalls++
			return value
		},
		Backoff:      func(int) time.Duration { return 0 },
		PingInterval: time.Hour, PingTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client
}

func waitForKorbitWebSocketWrite(
	t *testing.T,
	connection *korbitWebSocketTestConnection,
) []byte {
	t.Helper()
	select {
	case payload := <-connection.writes:
		return payload
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Korbit WebSocket write")
		return nil
	}
}

func decodeKorbitCommands(t *testing.T, payload []byte) []streamCommandItem {
	t.Helper()
	var items []streamCommandItem
	if err := json.Unmarshal(payload, &items); err != nil {
		t.Fatalf("decode Korbit commands %s: %v", payload, err)
	}
	return items
}

func assertKorbitSubscriptions(t *testing.T, payload []byte, method string, count int) {
	t.Helper()
	items := decodeKorbitCommands(t, payload)
	if len(items) != count {
		t.Fatalf("subscription count = %d, want %d: %s", len(items), count, payload)
	}
	seen := make(map[StreamChannel]bool, count)
	for _, item := range items {
		if item.RequestID <= 0 || item.Method != method || !item.Type.valid() {
			t.Fatalf("subscription item = %+v", item)
		}
		seen[item.Type] = true
	}
	if count == 2 && len(seen) != 2 {
		t.Fatalf("subscription channels = %v", seen)
	}
}

func assertKorbitPrivateHandshake(t *testing.T, request corestream.DialRequest, timestamp int64) {
	t.Helper()
	if request.Header.Get("X-KAPI-KEY") != "api-key" {
		t.Fatalf("X-KAPI-KEY = %q", request.Header.Get("X-KAPI-KEY"))
	}
	parsed, err := url.Parse(request.Endpoint)
	if err != nil {
		t.Fatalf("parse private endpoint: %v", err)
	}
	if parsed.Scheme != "ws" || parsed.Host != "private.example.test" || parsed.Path != "/v2/private" {
		t.Fatalf("private endpoint = %q", request.Endpoint)
	}
	marker := "&signature="
	position := strings.LastIndex(parsed.RawQuery, marker)
	if position < 0 {
		t.Fatalf("private query has no final signature: %q", parsed.RawQuery)
	}
	unsigned := parsed.RawQuery[:position]
	if unsigned != "recvWindow=5000&timestamp="+jsonNumber(timestamp) {
		t.Fatalf("unsigned private query = %q", unsigned)
	}
	signature, err := url.QueryUnescape(parsed.RawQuery[position+len(marker):])
	if err != nil {
		t.Fatalf("decode private signature: %v", err)
	}
	mac := hmac.New(sha256.New, []byte("secret-key"))
	_, _ = mac.Write([]byte(unsigned))
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(want)) {
		t.Fatalf("private signature = %q, want %q", signature, want)
	}
}

func jsonNumber(value int64) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func TestKorbitStreamConfigValidation(t *testing.T) {
	t.Parallel()

	connector := &korbitWebSocketTestConnector{}
	tests := []StreamClientConfig{
		{Connector: connector, DefaultEgressRouteID: "route-a", PublicWebSocketURL: "http://invalid"},
		{Connector: connector, DefaultEgressRouteID: "route-a", ReceiveWindow: 60001},
		{Connector: connector, DefaultEgressRouteID: "route-a", SigningMode: "invalid"},
	}
	for index, config := range tests {
		if _, err := NewStreamClient(config); err == nil {
			t.Fatalf("invalid stream config %d error = nil", index)
		}
	}
	if _, err := NewStreamClient(StreamClientConfig{Connector: connector}); !errors.Is(err, trade.ErrMissingEgressRoute) {
		t.Fatalf("missing route error = %v", err)
	}
}

func TestKorbitStreamCommandJSONOmitsUnavailableFields(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal([]streamCommandItem{{
		RequestID: 1, Method: "subscribe", Type: StreamChannelMyAsset,
	}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(payload) != `[{"requestId":1,"method":"subscribe","type":"myAsset"}]` {
		t.Fatalf("command JSON = %s", payload)
	}
	if strings.Contains(string(payload), "symbols") || strings.Contains(string(payload), "accountSeqs") {
		t.Fatalf("command JSON contains empty optional fields: %s", payload)
	}
}

func TestKorbitPrivateHandshakeUsesFormEncoding(t *testing.T) {
	t.Parallel()

	parameters := url.Values{"recvWindow": {"5000"}, "timestamp": {"1700000000123"}}
	unsigned := parameters.Encode()
	signature, err := SignHMACSHA256([]byte("secret-key"), unsigned)
	if err != nil {
		t.Fatalf("SignHMACSHA256() error = %v", err)
	}
	query := unsigned + "&signature=" + url.QueryEscape(signature)
	if !strings.HasPrefix(query, "recvWindow=5000&timestamp=1700000000123&signature=") {
		t.Fatalf("signed query = %q", query)
	}
	if _, err := http.NewRequest(http.MethodGet, "https://example.test?"+query, nil); err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
}
