package gateio

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	"github.com/proven-trade/proven-trade-sdk/model"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type gateIOWebSocketReadResult struct {
	message corestream.Message
	err     error
}

type gateIOWebSocketTestConnection struct {
	reads     chan gateIOWebSocketReadResult
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newGateIOWebSocketTestConnection() *gateIOWebSocketTestConnection {
	return &gateIOWebSocketTestConnection{
		reads: make(chan gateIOWebSocketReadResult, 16), writes: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (connection *gateIOWebSocketTestConnection) Read(
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

func (connection *gateIOWebSocketTestConnection) Write(
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

func (connection *gateIOWebSocketTestConnection) Ping(context.Context) error { return nil }

func (connection *gateIOWebSocketTestConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type gateIOWebSocketTestConnector struct {
	mu          sync.Mutex
	connections []*gateIOWebSocketTestConnection
	routes      []transport.EgressRouteID
	requests    []corestream.DialRequest
}

func (connector *gateIOWebSocketTestConnector) Connect(
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
		return nil, errors.New("no Gate.io test connection")
	}
	connection := connector.connections[0]
	connector.connections = connector.connections[1:]
	return connection, nil
}

func (connector *gateIOWebSocketTestConnector) snapshot() (
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

type gateIOStreamCommand struct {
	Time    int64                 `json:"time"`
	ID      int64                 `json:"id"`
	Channel string                `json:"channel"`
	Event   string                `json:"event"`
	Payload []string              `json:"payload"`
	Auth    *streamAuthentication `json:"auth"`
}

func TestGateIOPublicStreamRestoresSubscriptionOnSelectedRoute(t *testing.T) {
	t.Parallel()

	first := newGateIOWebSocketTestConnection()
	second := newGateIOWebSocketTestConnection()
	connector := &gateIOWebSocketTestConnector{
		connections: []*gateIOWebSocketTestConnection{first, second},
	}
	client := newTestGateIOStreamClient(t, connector, nil, nil, func() time.Time {
		return time.Unix(1_700_000_000, 0)
	})
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelTicker, CurrencyPair: "BTC_USDT",
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
			if message.Channel != StreamChannelTicker || message.Event != "update" {
				return nil
			}
			if err := message.Decode(&ticker); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	firstCommand := decodeGateIOStreamCommand(t, waitForGateIOWebSocketWrite(t, first))
	assertGateIOStreamCommand(
		t, firstCommand, "spot.tickers", "subscribe", []string{"BTC_USDT"}, false,
	)
	first.reads <- gateIOWebSocketReadResult{err: errors.New("connection lost")}
	secondCommand := decodeGateIOStreamCommand(t, waitForGateIOWebSocketWrite(t, second))
	assertGateIOStreamCommand(
		t, secondCommand, "spot.tickers", "subscribe", []string{"BTC_USDT"}, false,
	)
	second.reads <- gateIOWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"time":1700000000,"time_ms":1700000000001,"channel":"spot.tickers","event":"update","result":{"currency_pair":"BTC_USDT","last":"64000","lowest_ask":"64001","highest_bid":"63999","change_percentage":"1.2","base_volume":"10","quote_volume":"640000","high_24h":"65000","low_24h":"62000"}}`),
	}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("public Run() did not finish")
	}
	if ticker.Last != "64000" || ticker.HighestBid != "63999" {
		t.Fatalf("ticker = %+v", ticker)
	}
	if public.Generation() != 2 {
		t.Fatalf("generation = %d, want 2", public.Generation())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) ||
		len(requests) != 2 {
		t.Fatalf("routes = %v, requests = %d", routes, len(requests))
	}
	for _, request := range requests {
		if request.Endpoint != "ws://stream.example.test/ws/v4/" || len(request.Header) != 0 {
			t.Fatalf("dial request = %+v", request)
		}
	}
	_ = public.Close()
}

func TestGateIOPrivateStreamSignsSubscriptionsAndDecodesOrder(t *testing.T) {
	t.Parallel()

	connection := newGateIOWebSocketTestConnection()
	connector := &gateIOWebSocketTestConnector{
		connections: []*gateIOWebSocketTestConnection{connection},
	}
	provider := &recordingProvider{}
	credentials := testGateIOStreamCredentials(
		credential.PermissionRead, credential.PermissionTrade,
	)
	client := newTestGateIOStreamClient(
		t, connector, credentials, provider,
		func() time.Time { return time.Unix(1_700_000_000, 0) },
	)
	private, err := client.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{
		{Channel: StreamChannelOrders, CurrencyPair: "BTC_USDT"},
		{Channel: StreamChannelUserTrades, CurrencyPair: "BTC_USDT"},
		{Channel: StreamChannelBalances},
	}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var orders []StreamOrder
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Channel != StreamChannelOrders || message.Event != "update" {
				return nil
			}
			if !message.Private || len(message.Raw) == 0 {
				return errors.New("private order message classification is invalid")
			}
			if err := message.Decode(&orders); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	commands := []gateIOStreamCommand{
		decodeGateIOStreamCommand(t, waitForGateIOWebSocketWrite(t, connection)),
		decodeGateIOStreamCommand(t, waitForGateIOWebSocketWrite(t, connection)),
		decodeGateIOStreamCommand(t, waitForGateIOWebSocketWrite(t, connection)),
	}
	wantChannels := []string{"spot.balances", "spot.orders", "spot.usertrades"}
	for index, command := range commands {
		wantPayload := []string{"BTC_USDT"}
		if command.Channel == "spot.balances" {
			wantPayload = nil
		}
		assertGateIOStreamCommand(
			t, command, wantChannels[index], "subscribe", wantPayload, true,
		)
		assertGateIOStreamSignature(t, command, []byte("test-secret"))
	}
	connection.reads <- gateIOWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"time":1700000000,"time_ms":1700000000001,"channel":"spot.orders","event":"update","result":[{"id":"order-1","user":1001,"text":"t-strategy","create_time":"1700000000","create_time_ms":"1700000000000","update_time":"1700000001","update_time_ms":"1700000001000","event":"put","status":"open","currency_pair":"BTC_USDT","type":"limit","account":"spot","side":"buy","amount":"0.1","price":"64000","time_in_force":"gtc","left":"0.1","filled_amount":"0","filled_total":"0","avg_deal_price":"0","fee":"0","fee_currency":"USDT","finish_as":"open"}]}`),
	}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("private Run() did not finish")
	}
	if len(orders) != 1 || orders[0].ID != "order-1" || orders[0].Event != "put" {
		t.Fatalf("orders = %+v", orders)
	}
	calls, apiKey, secret := provider.snapshot()
	if calls != 3 || !allZero(apiKey) || !allZero(secret) {
		t.Fatalf(
			"provider calls = %d, key zero = %v, secret zero = %v",
			calls, allZero(apiKey), allZero(secret),
		)
	}
	routes, _ := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b"}) {
		t.Fatalf("routes = %v", routes)
	}
	_ = private.Close()
}

func TestGateIOPrivateStreamRejectsRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{}
	client := newTestGateIOStreamClient(
		t, &gateIOWebSocketTestConnector{},
		testGateIOStreamCredentials(credential.PermissionRead), provider, nil,
	)
	_, err := client.PrivateStream(
		StreamRequest{Subscriptions: []StreamSubscription{{
			Channel: StreamChannelBalances,
		}}},
		trade.WithEgressRoute("route-c"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("PrivateStream() error = %v, want authorization", err)
	}
	calls, _, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestGateIOStreamRollsBackRejectedDynamicSubscription(t *testing.T) {
	t.Parallel()

	connection := newGateIOWebSocketTestConnection()
	connector := &gateIOWebSocketTestConnector{
		connections: []*gateIOWebSocketTestConnection{connection},
	}
	client := newTestGateIOStreamClient(t, connector, nil, nil, func() time.Time {
		return time.Unix(1_700_000_000, 0)
	})
	initial := StreamSubscription{Channel: StreamChannelTicker, CurrencyPair: "BTC_USDT"}
	rejected := StreamSubscription{Channel: StreamChannelBookTicker, CurrencyPair: "BTC_USDT"}
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{initial}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	ackHandled := make(chan struct{}, 1)
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Error != nil {
				ackHandled <- struct{}{}
			}
			return nil
		})
	}()
	_ = decodeGateIOStreamCommand(t, waitForGateIOWebSocketWrite(t, connection))
	if err := public.Subscribe(context.Background(), rejected); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	command := decodeGateIOStreamCommand(t, waitForGateIOWebSocketWrite(t, connection))
	connection.reads <- gateIOWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"time":1700000000,"id":` + strconv.FormatInt(command.ID, 10) +
			`,"channel":"spot.book_ticker","event":"subscribe","error":{"code":2,"message":"invalid argument"},"result":null}`),
	}}
	select {
	case <-ackHandled:
	case <-time.After(2 * time.Second):
		t.Fatal("subscription error acknowledgement was not handled")
	}
	subscriptions := public.managed.snapshotSubscriptions()
	if !slices.Equal(subscriptions, []StreamSubscription{initial}) {
		t.Fatalf("subscriptions = %+v", subscriptions)
	}
	_ = public.Close()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, corestream.ErrSessionClosed) {
			t.Fatalf("Run() error = %v, want session closed", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("public Run() did not finish")
	}
}

func TestGateIOStreamDecodesSupportedEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		check   func(t *testing.T, message StreamMessage)
	}{
		{
			name:    "public trade",
			payload: `{"time":1700000000,"channel":"spot.trades","event":"update","result":{"id":10,"id_market":9,"create_time":1700000000,"create_time_ms":"1700000000000.1","side":"sell","currency_pair":"BTC_USDT","amount":"0.1","price":"64000"}}`,
			check: func(t *testing.T, message StreamMessage) {
				var tradeEvent StreamTrade
				if err := message.Decode(&tradeEvent); err != nil || tradeEvent.MarketID != 9 {
					t.Fatalf("trade = %+v, error = %v", tradeEvent, err)
				}
			},
		},
		{
			name:    "candle",
			payload: `{"time":1700000000,"channel":"spot.candlesticks","event":"update","result":{"t":"1700000000","v":"640000","c":"64000","h":"65000","l":"62000","o":"63000","n":"1m_BTC_USDT","a":"10","w":true}}`,
			check: func(t *testing.T, message StreamMessage) {
				var candle StreamCandle
				if err := message.Decode(&candle); err != nil || !candle.Closed || candle.Open != "63000" {
					t.Fatalf("candle = %+v, error = %v", candle, err)
				}
			},
		},
		{
			name:    "book ticker",
			payload: `{"time":1700000000,"channel":"spot.book_ticker","event":"update","result":{"t":1700000000000,"u":11,"s":"BTC_USDT","b":"63999","B":"0.3","a":"64001","A":"0.2"}}`,
			check: func(t *testing.T, message StreamMessage) {
				var ticker StreamBookTicker
				if err := message.Decode(&ticker); err != nil || ticker.BestAskAmount != "0.2" {
					t.Fatalf("book ticker = %+v, error = %v", ticker, err)
				}
			},
		},
		{
			name:    "order book update",
			payload: `{"time":1700000000,"channel":"spot.order_book_update","event":"update","result":{"t":1700000000000,"full":true,"l":"100","e":"depthUpdate","E":1700000000,"s":"BTC_USDT","U":10,"u":12,"b":[["63999","0.3"]],"a":[["64001","0.2"]]}}`,
			check: func(t *testing.T, message StreamMessage) {
				var book StreamOrderBookUpdate
				if err := message.Decode(&book); err != nil || !book.Full ||
					book.LastUpdateID != 12 || book.Bids[0].Amount != "0.3" {
					t.Fatalf("book = %+v, error = %v", book, err)
				}
			},
		},
		{
			name:    "user trade",
			payload: `{"time":1700000000,"channel":"spot.usertrades","event":"update","result":[{"id":10,"id_market":9,"user_id":1001,"order_id":"order-1","create_time":1700000000,"create_time_ms":"1700000000000.1","side":"buy","currency_pair":"BTC_USDT","amount":"0.1","price":"64000","role":"taker","fee":"0.064","fee_currency":"USDT","text":"t-strategy"}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var trades []StreamTrade
				if err := message.Decode(&trades); err != nil || !message.Private ||
					len(trades) != 1 || trades[0].Fee != "0.064" {
					t.Fatalf("trades = %+v, error = %v", trades, err)
				}
			},
		},
		{
			name:    "balance",
			payload: `{"time":1700000000,"channel":"spot.balances","event":"update","result":[{"timestamp":"1700000000","timestamp_ms":"1700000000000","user":"1001","currency":"USDT","change":"1","total":"1000","available":"900","freeze":"100","freeze_change":"0","change_type":"order-match"}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var balances []StreamBalance
				if err := message.Decode(&balances); err != nil || len(balances) != 1 ||
					balances[0].Available != "900" {
					t.Fatalf("balances = %+v, error = %v", balances, err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message, err := DecodeStreamMessage(corestream.Message{
				Type: corestream.MessageText, Data: []byte(test.payload),
			})
			if err != nil {
				t.Fatalf("DecodeStreamMessage() error = %v", err)
			}
			if len(message.Raw) == 0 || message.Channel == "" {
				t.Fatalf("message = %+v", message)
			}
			test.check(t, message)
		})
	}
	if _, err := DecodeStreamMessage(corestream.Message{
		Type: corestream.MessageBinary, Data: []byte{1},
	}); err == nil {
		t.Fatal("DecodeStreamMessage() accepted a binary frame")
	}
}

func TestGateIOStreamValidation(t *testing.T) {
	t.Parallel()

	tests := []StreamSubscription{
		{Channel: StreamChannelTicker, CurrencyPair: "btc_usdt"},
		{Channel: StreamChannelCandles, CurrencyPair: "BTC_USDT", CandleInterval: Candle1Second},
		{Channel: StreamChannelOrderBookUpdate, CurrencyPair: "BTC_USDT"},
		{Channel: StreamChannelOrderBookV2, CurrencyPair: "BTC_USDT"},
		{Channel: StreamChannelBookTicker, CurrencyPair: "BTC_USDT", UpdateInterval: StreamUpdate20Millis},
		{Channel: StreamChannelTicker, CurrencyPair: "BTC_USDT", OrderBookDepth: StreamOrderBookDepth50},
		{Channel: StreamChannelOrders},
		{Channel: StreamChannelBalances, CurrencyPair: "BTC_USDT"},
	}
	privateValues := []bool{false, false, false, false, false, false, true, true}
	for index, subscription := range tests {
		if err := validateStreamSubscription(subscription, privateValues[index]); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("subscription %d error = %v, want validation", index, err)
		}
	}
}

func newTestGateIOStreamClient(
	t *testing.T,
	connector corestream.Connector,
	credentials *credential.Descriptor,
	provider credential.Provider,
	now func() time.Time,
) *StreamClient {
	t.Helper()
	client, err := NewStreamClient(StreamClientConfig{
		Connector: connector, Credentials: credentials, CredentialProvider: provider,
		DefaultEgressRouteID: "route-a", WebSocketURL: "ws://stream.example.test/ws/v4/",
		AllowInsecureWebSocket: true, Now: now,
		Backoff: func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client
}

func testGateIOStreamCredentials(
	permissions ...credential.Permission,
) *credential.Descriptor {
	return &credential.Descriptor{
		AccountID: "gateio-main", Exchange: model.ExchangeGateIO,
		SecretRef: "secret/gateio-main", Permissions: permissions,
		AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a", "route-b"},
	}
}

func waitForGateIOWebSocketWrite(
	t *testing.T,
	connection *gateIOWebSocketTestConnection,
) []byte {
	t.Helper()
	select {
	case payload := <-connection.writes:
		return payload
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Gate.io WebSocket write")
		return nil
	}
}

func decodeGateIOStreamCommand(t *testing.T, payload []byte) gateIOStreamCommand {
	t.Helper()
	var command gateIOStreamCommand
	if err := json.Unmarshal(payload, &command); err != nil {
		t.Fatalf("decode Gate.io stream command: %v", err)
	}
	return command
}

func assertGateIOStreamCommand(
	t *testing.T,
	command gateIOStreamCommand,
	channel string,
	event string,
	payload []string,
	private bool,
) {
	t.Helper()
	if command.Time != 1_700_000_000 || command.ID <= 0 || command.Channel != channel ||
		command.Event != event || !slices.Equal(command.Payload, payload) ||
		(command.Auth != nil) != private {
		t.Fatalf("stream command = %+v", command)
	}
	if private && (command.Auth.Method != "api_key" || command.Auth.APIKey != "test-api-key") {
		t.Fatalf("stream authentication = %+v", command.Auth)
	}
}

func assertGateIOStreamSignature(
	t *testing.T,
	command gateIOStreamCommand,
	secret []byte,
) {
	t.Helper()
	payload := []byte("channel=" + command.Channel + "&event=" + command.Event +
		"&time=" + "1700000000")
	mac := hmac.New(sha512.New, secret)
	_, _ = mac.Write(payload)
	want := hex.EncodeToString(mac.Sum(nil))
	if command.Auth == nil || !hmac.Equal(
		[]byte(command.Auth.Signature), []byte(want),
	) {
		t.Fatalf("signature = %q, want %q", command.Auth.Signature, want)
	}
}
