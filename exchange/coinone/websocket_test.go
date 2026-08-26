package coinone

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
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

type coinoneWebSocketReadResult struct {
	message corestream.Message
	err     error
}

type coinoneWebSocketTestConnection struct {
	reads     chan coinoneWebSocketReadResult
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newCoinoneWebSocketTestConnection() *coinoneWebSocketTestConnection {
	return &coinoneWebSocketTestConnection{
		reads: make(chan coinoneWebSocketReadResult, 16), writes: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (connection *coinoneWebSocketTestConnection) Read(ctx context.Context) (corestream.Message, error) {
	select {
	case <-ctx.Done():
		return corestream.Message{}, ctx.Err()
	case <-connection.closed:
		return corestream.Message{}, corestream.ErrSessionClosed
	case result := <-connection.reads:
		return result.message, result.err
	}
}

func (connection *coinoneWebSocketTestConnection) Write(
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

func (connection *coinoneWebSocketTestConnection) Ping(context.Context) error { return nil }

func (connection *coinoneWebSocketTestConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type coinoneWebSocketTestConnector struct {
	mu          sync.Mutex
	connections []*coinoneWebSocketTestConnection
	routes      []transport.EgressRouteID
	requests    []corestream.DialRequest
}

func (connector *coinoneWebSocketTestConnector) Connect(
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
		return nil, errors.New("no Coinone test connection")
	}
	connection := connector.connections[0]
	connector.connections = connector.connections[1:]
	return connection, nil
}

func (connector *coinoneWebSocketTestConnector) snapshot() (
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

func TestCoinonePublicStreamReconnectsOnSameRouteAndDecodesShort(t *testing.T) {
	t.Parallel()

	first := newCoinoneWebSocketTestConnection()
	second := newCoinoneWebSocketTestConnection()
	connector := &coinoneWebSocketTestConnector{
		connections: []*coinoneWebSocketTestConnection{first, second},
	}
	client := newTestCoinoneStreamClient(t, connector, nil, nil, nil)
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelTicker,
		Topics:  []StreamTopic{{QuoteCurrency: "KRW", TargetCurrency: "BTC"}},
		Format:  StreamFormatShort,
	}}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var ticker StreamTicker
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.ResponseType != "DATA" {
				return nil
			}
			if err := message.Decode(&ticker); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	assertCoinoneStreamCommand(
		t, waitForCoinoneWebSocketWrite(t, first), "SUBSCRIBE", StreamChannelTicker,
		StreamFormatShort, false, 1,
	)
	first.reads <- coinoneWebSocketReadResult{err: errors.New("connection lost")}
	assertCoinoneStreamCommand(
		t, waitForCoinoneWebSocketWrite(t, second), "SUBSCRIBE", StreamChannelTicker,
		StreamFormatShort, false, 1,
	)
	second.reads <- coinoneWebSocketReadResult{message: corestream.Message{Data: []byte(
		`{"r":"DATA","c":"TICKER","d":{"qc":"KRW","tc":"BTC","t":1700000000000,"qv":"1000.5","tv":"2.5","hi":"65000000","lo":"62000000","fi":"63000000","la":"64000000","vp":"110","abp":"64001000","abq":"0.2","bbp":"64000000","bbq":"0.1","i":"ticker-1","yla":"63000000"}}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("public Run() did not finish")
	}
	if ticker.QuoteCurrency != "KRW" || ticker.Last != "64000000" || ticker.YesterdayLast != "63000000" {
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
		if request.Endpoint != "ws://public.example.test" || len(request.Header) != 0 {
			t.Fatalf("public dial request = %+v", request)
		}
	}
}

func TestCoinonePrivateStreamRefreshesAuthenticationAndSubscriptions(t *testing.T) {
	t.Parallel()

	first := newCoinoneWebSocketTestConnection()
	second := newCoinoneWebSocketTestConnection()
	connector := &coinoneWebSocketTestConnector{
		connections: []*coinoneWebSocketTestConnection{first, second},
	}
	provider := &recordingProvider{}
	nonces := sequentialCoinoneNonce("nonce-1", "nonce-2")
	client := newTestCoinoneStreamClient(
		t, connector, provider, nonces, []transport.EgressRouteID{"route-a", "route-b"},
	)
	private, err := client.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{
		{
			Channel: StreamChannelMyOrder,
			Topics: []StreamTopic{
				{QuoteCurrency: "KRW", TargetCurrency: "BTC"},
				{QuoteCurrency: "KRW", TargetCurrency: "ETH"},
			},
		},
		{Channel: StreamChannelMyAsset, Format: StreamFormatShort},
	}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var order MyOrderEvent
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.ResponseType != "DATA" || message.Channel != StreamChannelMyOrder {
				return nil
			}
			if err := message.Decode(&order); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	firstCommands := waitForCoinoneWebSocketWrites(t, first, 2)
	assertCoinonePrivateCommands(t, firstCommands)
	first.reads <- coinoneWebSocketReadResult{err: errors.New("connection lost")}
	secondCommands := waitForCoinoneWebSocketWrites(t, second, 2)
	assertCoinonePrivateCommands(t, secondCommands)
	second.reads <- coinoneWebSocketReadResult{message: corestream.Message{Data: []byte(
		`{"response_type":"DATA","channel":"MYORDER","data":{"quote_currency":"KRW","target_currency":"BTC","order_id":"order-1","type":"LIMIT","status":"trade","side":"BID","order_price":null,"order_qty":null,"order_amount":null,"trade_id":"trade-1","is_maker":false,"executed_price":"64000000","executed_qty":"0.01","executed_fee":"320","remain_qty":"0.09","remain_amount":null,"user_order_id":"strategy-1","prevented_qty":null,"executed_timestamp":1700000000,"order_timestamp":1700000000,"timestamp":1700000001}}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("private Run() did not finish")
	}
	if order.OrderID != "order-1" || order.Side != StreamOrderSideBid ||
		order.ExecutedPrice != "64000000" || order.RemainingQuantity != "0.09" || order.IsMaker == nil {
		t.Fatalf("order = %+v", order)
	}
	calls, key, secret := provider.snapshot()
	if calls != 2 || !allZero(key) || !allZero(secret) {
		t.Fatalf("provider calls = %d, key zero = %v, secret zero = %v", calls, allZero(key), allZero(secret))
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) || len(requests) != 2 {
		t.Fatalf("routes = %v, requests = %d", routes, len(requests))
	}
	assertCoinonePrivateHeader(t, requests[0], "nonce-1")
	assertCoinonePrivateHeader(t, requests[1], "nonce-2")
	if private.Generation() != 2 {
		t.Fatalf("Generation() = %d, want 2", private.Generation())
	}
	_ = private.Close()
}

func TestCoinoneStreamDynamicSubscribeAndUnsubscribe(t *testing.T) {
	t.Parallel()

	connection := newCoinoneWebSocketTestConnection()
	connector := &coinoneWebSocketTestConnector{
		connections: []*coinoneWebSocketTestConnection{connection},
	}
	client := newTestCoinoneStreamClient(t, connector, nil, nil, nil)
	ticker := StreamSubscription{
		Channel: StreamChannelTicker,
		Topics:  []StreamTopic{{QuoteCurrency: "KRW", TargetCurrency: "BTC"}},
	}
	tradeSubscription := StreamSubscription{
		Channel: StreamChannelTrade,
		Topics:  []StreamTopic{{QuoteCurrency: "KRW", TargetCurrency: "ETH"}},
	}
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{ticker}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- public.Run(ctx, func(context.Context, StreamMessage) error { return nil })
	}()
	_ = waitForCoinoneWebSocketWrite(t, connection)
	if err := public.Subscribe(context.Background(), tradeSubscription); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	assertCoinoneStreamCommand(
		t, waitForCoinoneWebSocketWrite(t, connection), "SUBSCRIBE", StreamChannelTrade,
		StreamFormatDefault, false, 1,
	)
	if err := public.Unsubscribe(context.Background(), ticker); err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
	assertCoinoneStreamCommand(
		t, waitForCoinoneWebSocketWrite(t, connection), "UNSUBSCRIBE", StreamChannelTicker,
		StreamFormatDefault, false, 1,
	)
	cancel()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("dynamic stream Run() did not finish")
	}
}

func TestCoinoneApplicationPingWaitsForPong(t *testing.T) {
	t.Parallel()

	next := newCoinoneWebSocketTestConnection()
	connection := &coinoneConnection{next: next, pong: make(chan struct{}, 1)}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- connection.Ping(ctx) }()
	if payload := waitForCoinoneWebSocketWrite(t, next); string(payload) != `{"request_type":"PING"}` {
		t.Fatalf("PING payload = %s", payload)
	}
	next.reads <- coinoneWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText, Data: []byte(`{"response_type":"PONG"}`),
	}}
	message, err := connection.Read(ctx)
	if err != nil || !isCoinonePong(message.Data) {
		t.Fatalf("Read() = %s, error = %v", message.Data, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func TestCoinoneDecodeDefaultShortStatusAndErrorMessages(t *testing.T) {
	t.Parallel()

	connected, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"response_type":"CONNECTED","data":{"session_id":"session-1"}}`,
	)})
	if err != nil || connected.ResponseType != "CONNECTED" {
		t.Fatalf("connected = %+v, error = %v", connected, err)
	}
	var session StreamSession
	if err := json.Unmarshal(connected.Data, &session); err != nil || session.SessionID != "session-1" {
		t.Fatalf("session = %+v, error = %v", session, err)
	}
	failure, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`{"response_type":"ERROR","error_code":160012,"message":"Invalid Topic"}`,
	)})
	if err != nil || failure.ErrorCode != 160012 || failure.ErrorMessage != "Invalid Topic" {
		t.Fatalf("failure = %+v, error = %v", failure, err)
	}

	tests := []struct {
		name    string
		payload string
		decode  func(StreamMessage) error
	}{
		{
			"order book", `{"r":"DATA","c":"ORDERBOOK","d":{"qc":"KRW","tc":"BTC","t":1,"i":"book-1","a":[{"p":"11","q":"2"}],"b":[{"p":"10","q":"3"}]}}`,
			func(message StreamMessage) error {
				var value StreamOrderBook
				if err := message.Decode(&value); err != nil {
					return err
				}
				if value.Asks[0].Price != "11" || value.Bids[0].Quantity != "3" {
					return errors.New("unexpected order book")
				}
				return nil
			},
		},
		{
			"trade", `{"r":"DATA","c":"TRADE","d":{"qc":"KRW","tc":"BTC","i":"trade-1","t":1,"p":"10","q":"0.1","sm":true}}`,
			func(message StreamMessage) error {
				var value StreamTrade
				if err := message.Decode(&value); err != nil {
					return err
				}
				if value.Price != "10" || !value.IsSellerMaker {
					return errors.New("unexpected trade")
				}
				return nil
			},
		},
		{
			"chart", `{"r":"DATA","c":"CHART","d":{"qc":"KRW","tc":"BTC","iv":"1m","t":2,"i":"chart-1","ct":1,"fi":"10","lo":"9","hi":"12","la":"11","qv":"100","tv":"10"}}`,
			func(message StreamMessage) error {
				var value StreamCandle
				if err := message.Decode(&value); err != nil {
					return err
				}
				if value.Interval != Candle1Minute || value.Last != "11" {
					return errors.New("unexpected chart")
				}
				return nil
			},
		},
		{
			"my order", `{"r":"DATA","c":"MYORDER","d":{"qc":"KRW","tc":"BTC","oi":"order-1","t":"LIMIT","st":"trade","s":"BID","ep":"10","eq":"0.1","im":false,"rq":"0.9","ui":"strategy-1","et":1,"ot":1,"ts":2}}`,
			func(message StreamMessage) error {
				var value MyOrderEvent
				if err := message.Decode(&value); err != nil {
					return err
				}
				if value.OrderID != "order-1" || value.IsMaker == nil || *value.IsMaker {
					return errors.New("unexpected my order")
				}
				return nil
			},
		},
		{
			"my asset", `{"r":"DATA","c":"MYASSET","d":{"as":[{"c":"KRW","a":"1000","l":"10"}],"t":"trade","ts":2}}`,
			func(message StreamMessage) error {
				var value MyAssetEvent
				if err := message.Decode(&value); err != nil {
					return err
				}
				if len(value.Assets) != 1 || value.Assets[0].Available != "1000" || value.Type != "trade" {
					return errors.New("unexpected my asset")
				}
				return nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message, err := DecodeStreamMessage(corestream.Message{Data: []byte(test.payload)})
			if err != nil {
				t.Fatalf("DecodeStreamMessage() error = %v", err)
			}
			if err := test.decode(message); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := DecodeStreamMessage(corestream.Message{Data: []byte(`[{}]`)}); err == nil {
		t.Fatal("DecodeStreamMessage() accepted an array")
	}
}

func TestCoinonePrivateStreamRejectsRouteBeforeResolvingSecret(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{}
	client := newTestCoinoneStreamClient(
		t, &coinoneWebSocketTestConnector{}, provider,
		sequentialCoinoneNonce("nonce"), []transport.EgressRouteID{"route-a"},
	)
	_, err := client.PrivateStream(
		StreamRequest{Subscriptions: []StreamSubscription{{Channel: StreamChannelMyAsset}}},
		trade.WithEgressRoute("route-b"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("PrivateStream() error = %v, want ErrAuthorization", err)
	}
	calls, _, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestCoinoneStreamSubscriptionValidation(t *testing.T) {
	t.Parallel()

	invalidPublic := [][]StreamSubscription{
		nil,
		{{Channel: StreamChannelTicker}},
		{{Channel: StreamChannelMyAsset, Topics: []StreamTopic{{QuoteCurrency: "KRW", TargetCurrency: "BTC"}}}},
		{{Channel: StreamChannelChart, Topics: []StreamTopic{{QuoteCurrency: "KRW", TargetCurrency: "BTC", Interval: Candle10Minutes}}}},
		{{Channel: StreamChannelTrade, Topics: []StreamTopic{{QuoteCurrency: "KRW", TargetCurrency: "BTC", Interval: Candle1Minute}}}},
	}
	for index, subscriptions := range invalidPublic {
		if _, err := validateStreamSubscriptions(subscriptions, false, true); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("public validation %d error = %v", index, err)
		}
	}
	invalidPrivate := [][]StreamSubscription{
		nil,
		{{Channel: StreamChannelTicker, Topics: []StreamTopic{{QuoteCurrency: "KRW", TargetCurrency: "BTC"}}}},
		{{Channel: StreamChannelMyAsset, Topics: []StreamTopic{{QuoteCurrency: "KRW", TargetCurrency: "BTC"}}}},
		{{Channel: StreamChannelMyOrder, Topics: []StreamTopic{{QuoteCurrency: "krw", TargetCurrency: "BTC"}}}},
	}
	for index, subscriptions := range invalidPrivate {
		if _, err := validateStreamSubscriptions(subscriptions, true, true); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("private validation %d error = %v", index, err)
		}
	}
	valid, err := validateStreamSubscriptions(
		[]StreamSubscription{{Channel: StreamChannelMyOrder}}, true, true,
	)
	if err != nil || valid[0].Format != StreamFormatDefault {
		t.Fatalf("valid private subscription = %+v, error = %v", valid, err)
	}
	payload, err := encodeStreamCommand("SUBSCRIBE", valid[0], true)
	if err != nil || string(payload) != `{"request_type":"SUBSCRIBE","channel":"MYORDER"}` {
		t.Fatalf("all-market MYORDER payload = %s, error = %v", payload, err)
	}
}

func newTestCoinoneStreamClient(
	t *testing.T,
	connector corestream.Connector,
	provider *recordingProvider,
	nonceSource NonceSource,
	allowedRoutes []transport.EgressRouteID,
) *StreamClient {
	t.Helper()
	config := StreamClientConfig{
		Connector: connector, DefaultEgressRouteID: "route-a",
		PublicWebSocketURL:     "ws://public.example.test",
		PrivateWebSocketURL:    "ws://private.example.test/v1/private",
		AllowInsecureWebSocket: true,
		NonceSource:            nonceSource,
		Now:                    func() time.Time { return time.UnixMilli(1700000000123) },
		Backoff:                func(int) time.Duration { return 0 },
	}
	if provider != nil {
		config.Credentials = &credential.Descriptor{
			AccountID: "coinone-portfolio", Exchange: model.ExchangeCoinone,
			SecretRef:             "secret/coinone",
			Permissions:           []credential.Permission{credential.PermissionRead, credential.PermissionTrade},
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

func sequentialCoinoneNonce(values ...string) NonceSource {
	var mu sync.Mutex
	index := 0
	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if index >= len(values) {
			return "", errors.New("no Coinone nonce remains")
		}
		value := values[index]
		index++
		return value, nil
	}
}

func waitForCoinoneWebSocketWrite(t *testing.T, connection *coinoneWebSocketTestConnection) []byte {
	t.Helper()
	select {
	case payload := <-connection.writes:
		return payload
	case <-time.After(time.Second):
		t.Fatal("Coinone WebSocket write was not observed")
		return nil
	}
}

func waitForCoinoneWebSocketWrites(
	t *testing.T,
	connection *coinoneWebSocketTestConnection,
	count int,
) [][]byte {
	t.Helper()
	result := make([][]byte, count)
	for index := range result {
		result[index] = waitForCoinoneWebSocketWrite(t, connection)
	}
	return result
}

func assertCoinoneStreamCommand(
	t *testing.T,
	payload []byte,
	requestType string,
	channel StreamChannel,
	format StreamFormat,
	private bool,
	topicCount int,
) {
	t.Helper()
	var envelope struct {
		RequestType string          `json:"request_type"`
		Channel     StreamChannel   `json:"channel"`
		Topic       json.RawMessage `json:"topic"`
		Format      StreamFormat    `json:"format"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode stream command: %v", err)
	}
	if envelope.Format == "" {
		envelope.Format = StreamFormatDefault
	}
	if envelope.RequestType != requestType || envelope.Channel != channel || envelope.Format != format {
		t.Fatalf("stream command = %s", payload)
	}
	if topicCount == 0 && len(envelope.Topic) != 0 {
		t.Fatalf("unexpected stream topic = %s", envelope.Topic)
	}
	if topicCount == 0 {
		return
	}
	if private {
		var topics []StreamTopic
		if err := json.Unmarshal(envelope.Topic, &topics); err != nil || len(topics) != topicCount {
			t.Fatalf("private topics = %s, error = %v", envelope.Topic, err)
		}
		return
	}
	var topic StreamTopic
	if err := json.Unmarshal(envelope.Topic, &topic); err != nil || topicCount != 1 {
		t.Fatalf("public topic = %s, error = %v", envelope.Topic, err)
	}
}

func assertCoinonePrivateCommands(t *testing.T, payloads [][]byte) {
	t.Helper()
	channels := make(map[StreamChannel]bool, len(payloads))
	for _, payload := range payloads {
		var envelope struct {
			Channel StreamChannel `json:"channel"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatalf("decode private command: %v", err)
		}
		channels[envelope.Channel] = true
		switch envelope.Channel {
		case StreamChannelMyOrder:
			assertCoinoneStreamCommand(
				t, payload, "SUBSCRIBE", StreamChannelMyOrder, StreamFormatDefault, true, 2,
			)
		case StreamChannelMyAsset:
			assertCoinoneStreamCommand(
				t, payload, "SUBSCRIBE", StreamChannelMyAsset, StreamFormatShort, true, 0,
			)
		default:
			t.Fatalf("unexpected private channel %q", envelope.Channel)
		}
	}
	if !channels[StreamChannelMyOrder] || !channels[StreamChannelMyAsset] {
		t.Fatalf("private channels = %v", channels)
	}
}

func assertCoinonePrivateHeader(t *testing.T, request corestream.DialRequest, nonce string) {
	t.Helper()
	if request.Endpoint != "ws://private.example.test/v1/private" {
		t.Fatalf("private endpoint = %q", request.Endpoint)
	}
	payload := request.Header.Get("X-COINONE-PAYLOAD")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode private payload: %v", err)
	}
	wantBody := `{"access_token":"access-key","nonce":"` + nonce + `","timestamp":1700000000123}`
	if string(decoded) != wantBody {
		t.Fatalf("private payload = %s, want %s", decoded, wantBody)
	}
	mac := hmac.New(sha512.New, []byte("secret-key"))
	_, _ = mac.Write([]byte(payload))
	wantSignature := hex.EncodeToString(mac.Sum(nil))
	if request.Header.Get("X-COINONE-SIGNATURE") != wantSignature {
		t.Fatalf("private signature is invalid")
	}
}
