package futures

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

type gateIOFuturesWebSocketReadResult struct {
	message corestream.Message
	err     error
}

type gateIOFuturesWebSocketTestConnection struct {
	reads     chan gateIOFuturesWebSocketReadResult
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newGateIOFuturesWebSocketTestConnection() *gateIOFuturesWebSocketTestConnection {
	return &gateIOFuturesWebSocketTestConnection{
		reads: make(chan gateIOFuturesWebSocketReadResult, 16), writes: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (connection *gateIOFuturesWebSocketTestConnection) Read(
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

func (connection *gateIOFuturesWebSocketTestConnection) Write(
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

func (connection *gateIOFuturesWebSocketTestConnection) Ping(context.Context) error { return nil }

func (connection *gateIOFuturesWebSocketTestConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type gateIOFuturesWebSocketTestConnector struct {
	mu          sync.Mutex
	connections []*gateIOFuturesWebSocketTestConnection
	routes      []transport.EgressRouteID
	requests    []corestream.DialRequest
}

func (connector *gateIOFuturesWebSocketTestConnector) Connect(
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
		return nil, errors.New("no Gate.io Futures test connection")
	}
	connection := connector.connections[0]
	connector.connections = connector.connections[1:]
	return connection, nil
}

func (connector *gateIOFuturesWebSocketTestConnector) snapshot() (
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

type gateIOFuturesStreamCommand struct {
	Time    int64                 `json:"time"`
	ID      int64                 `json:"id"`
	Channel string                `json:"channel"`
	Event   string                `json:"event"`
	Payload []string              `json:"payload"`
	Auth    *streamAuthentication `json:"auth"`
}

func TestGateIOFuturesPublicStreamRestoresSubscriptionOnSelectedRoute(t *testing.T) {
	t.Parallel()

	first := newGateIOFuturesWebSocketTestConnection()
	second := newGateIOFuturesWebSocketTestConnection()
	connector := &gateIOFuturesWebSocketTestConnector{
		connections: []*gateIOFuturesWebSocketTestConnection{first, second},
	}
	client := newTestGateIOFuturesStreamClient(t, connector, nil, nil, func() time.Time {
		return time.Unix(1_700_000_000, 0)
	})
	public, err := client.PublicStream(StreamRequest{
		Settlement: SettlementUSD1,
		Subscriptions: []StreamSubscription{{
			Channel: StreamChannelTicker, Contract: "BTC_USD1",
		}},
	}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var tickers []StreamTicker
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Channel != StreamChannelTicker || message.Event != "update" {
				return nil
			}
			if err := message.Decode(&tickers); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	firstCommand := decodeGateIOFuturesStreamCommand(
		t, waitForGateIOFuturesWebSocketWrite(t, first),
	)
	assertGateIOFuturesStreamCommand(
		t, firstCommand, "futures.tickers", "subscribe", []string{"BTC_USD1"}, false,
	)
	first.reads <- gateIOFuturesWebSocketReadResult{err: errors.New("connection lost")}
	secondCommand := decodeGateIOFuturesStreamCommand(
		t, waitForGateIOFuturesWebSocketWrite(t, second),
	)
	assertGateIOFuturesStreamCommand(
		t, secondCommand, "futures.tickers", "subscribe", []string{"BTC_USD1"}, false,
	)
	second.reads <- gateIOFuturesWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"time":1700000000,"time_ms":1700000000001,"channel":"futures.tickers","event":"update","result":[{"contract":"BTC_USD1","last":"64000","mark_price":"64001","index_price":"63999","total_size":"100"}]}`),
	}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("public Run() did not finish")
	}
	if len(tickers) != 1 || tickers[0].Last != "64000" {
		t.Fatalf("tickers = %+v", tickers)
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
		if request.Endpoint != "ws://stream.example.test/v4/ws/usd1" || len(request.Header) != 0 {
			t.Fatalf("dial request = %+v", request)
		}
	}
	_ = public.Close()
}

func TestGateIOFuturesPrivateStreamSignsSubscriptionsAndDecodesOrder(t *testing.T) {
	t.Parallel()

	connection := newGateIOFuturesWebSocketTestConnection()
	connector := &gateIOFuturesWebSocketTestConnector{
		connections: []*gateIOFuturesWebSocketTestConnection{connection},
	}
	provider := &recordingProvider{}
	credentials := testGateIOFuturesStreamCredentials(credential.PermissionRead)
	client := newTestGateIOFuturesStreamClient(
		t, connector, credentials, provider,
		func() time.Time { return time.Unix(1_700_000_000, 0) },
	)
	private, err := client.PrivateStream(StreamRequest{
		Settlement: SettlementUSDT,
		Subscriptions: []StreamSubscription{
			{Channel: StreamChannelOrders, Contract: "!all"},
			{Channel: StreamChannelUserTrades, Contract: "BTC_USDT"},
			{Channel: StreamChannelBalances},
			{Channel: StreamChannelPositions, Contract: "BTC_USDT"},
		},
	}, trade.WithEgressRoute("route-b"))
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
				return errors.New("private Futures order message classification is invalid")
			}
			if err := message.Decode(&orders); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	commands := []gateIOFuturesStreamCommand{
		decodeGateIOFuturesStreamCommand(t, waitForGateIOFuturesWebSocketWrite(t, connection)),
		decodeGateIOFuturesStreamCommand(t, waitForGateIOFuturesWebSocketWrite(t, connection)),
		decodeGateIOFuturesStreamCommand(t, waitForGateIOFuturesWebSocketWrite(t, connection)),
		decodeGateIOFuturesStreamCommand(t, waitForGateIOFuturesWebSocketWrite(t, connection)),
	}
	wantChannels := []string{
		"futures.balances", "futures.orders", "futures.positions", "futures.usertrades",
	}
	for index, command := range commands {
		wantPayload := []string{"1666", "BTC_USDT"}
		switch command.Channel {
		case "futures.balances":
			wantPayload = []string{"1666"}
		case "futures.orders":
			wantPayload = []string{"1666", "!all"}
		}
		assertGateIOFuturesStreamCommand(
			t, command, wantChannels[index], "subscribe", wantPayload, true,
		)
		assertGateIOFuturesStreamSignature(t, command, []byte("test-secret"))
	}
	connection.reads <- gateIOFuturesWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"time":1700000000,"time_ms":1700000000001,"channel":"futures.orders","event":"update","result":[{"id":4872460,"user":"1666","contract":"BTC_USDT","create_time":1628736847,"create_time_ms":1628736847325,"fill_price":40000.4,"finish_as":"filled","finish_time":1628736848,"finish_time_ms":1628736848321,"iceberg":0,"is_close":false,"is_liq":false,"is_reduce_only":false,"left":0,"mkfr":-0.00025,"price":40000.4,"size":1,"status":"finished","text":"t-strategy","tif":"gtc","tkfr":0.0005}]}`),
	}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("private Run() did not finish")
	}
	if len(orders) != 1 || orders[0].ID != "4872460" || orders[0].Size != "1" ||
		orders[0].FillPrice != "40000.4" {
		t.Fatalf("orders = %+v", orders)
	}
	calls, apiKey, secret := provider.snapshot()
	if calls != 4 || !allZero(apiKey) || !allZero(secret) {
		t.Fatalf(
			"provider calls = %d, key zero = %v, secret zero = %v",
			calls, allZero(apiKey), allZero(secret),
		)
	}
	_ = private.Close()
}

func TestGateIOFuturesPrivateStreamRejectsRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{}
	client := newTestGateIOFuturesStreamClient(
		t, &gateIOFuturesWebSocketTestConnector{},
		testGateIOFuturesStreamCredentials(credential.PermissionRead), provider, nil,
	)
	_, err := client.PrivateStream(
		StreamRequest{
			Settlement: SettlementUSDT,
			Subscriptions: []StreamSubscription{{
				Channel: StreamChannelBalances,
			}},
		},
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

func TestGateIOFuturesStreamRollsBackRejectedDynamicSubscription(t *testing.T) {
	t.Parallel()

	connection := newGateIOFuturesWebSocketTestConnection()
	connector := &gateIOFuturesWebSocketTestConnector{
		connections: []*gateIOFuturesWebSocketTestConnection{connection},
	}
	client := newTestGateIOFuturesStreamClient(t, connector, nil, nil, func() time.Time {
		return time.Unix(1_700_000_000, 0)
	})
	initial := StreamSubscription{Channel: StreamChannelTicker, Contract: "BTC_USDT"}
	rejected := StreamSubscription{Channel: StreamChannelBookTicker, Contract: "BTC_USDT"}
	public, err := client.PublicStream(StreamRequest{
		Settlement: SettlementUSDT, Subscriptions: []StreamSubscription{initial},
	})
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
	_ = decodeGateIOFuturesStreamCommand(t, waitForGateIOFuturesWebSocketWrite(t, connection))
	if err := public.Subscribe(context.Background(), rejected); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	command := decodeGateIOFuturesStreamCommand(
		t, waitForGateIOFuturesWebSocketWrite(t, connection),
	)
	connection.reads <- gateIOFuturesWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"time":1700000000,"id":` + strconv.FormatInt(command.ID, 10) +
			`,"channel":"futures.book_ticker","event":"subscribe","error":{"code":2,"message":"invalid argument"},"result":null}`),
	}}
	select {
	case <-ackHandled:
	case <-time.After(2 * time.Second):
		t.Fatal("subscription error acknowledgement was not handled")
	}
	if subscriptions := public.managed.snapshotSubscriptions(); !slices.Equal(subscriptions, []StreamSubscription{initial}) {
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

func TestGateIOFuturesStreamDecodesSupportedEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		check   func(t *testing.T, message StreamMessage)
	}{
		{
			name:    "공개 체결",
			payload: `{"time":1700000000,"channel":"futures.trades","event":"update","result":[{"size":-108,"id":27753479,"create_time":1545136464,"create_time_ms":1545136464123,"price":"96.4","contract":"BTC_USDT","is_internal":true}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var trades []StreamTrade
				if err := message.Decode(&trades); err != nil || len(trades) != 1 ||
					trades[0].Size != "-108" || !trades[0].Internal {
					t.Fatalf("trades = %+v, error = %v", trades, err)
				}
			},
		},
		{
			name:    "캔들",
			payload: `{"time":1700000000,"channel":"futures.candlesticks","event":"update","result":[{"t":1545129300,"v":27525555,"c":"95.4","h":"96.9","l":"89.5","o":"94.3","n":"1m_BTC_USDT","a":"314732.87412"}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var candles []StreamCandle
				if err := message.Decode(&candles); err != nil || candles[0].Volume != "27525555" ||
					candles[0].Open != "94.3" {
					t.Fatalf("candles = %+v, error = %v", candles, err)
				}
			},
		},
		{
			name:    "최우선 호가",
			payload: `{"time":1700000000,"channel":"futures.book_ticker","event":"update","result":{"t":1615366379123,"u":2517661076,"s":"BTC_USDT","b":"54696.6","B":37000,"a":"54696.7","A":"47061"}}`,
			check: func(t *testing.T, message StreamMessage) {
				var ticker StreamBookTicker
				if err := message.Decode(&ticker); err != nil || ticker.BestAskSize != "47061" {
					t.Fatalf("book ticker = %+v, error = %v", ticker, err)
				}
			},
		},
		{
			name:    "증분 호가",
			payload: `{"time":1700000000,"channel":"futures.order_book_update","event":"update","result":{"t":1615366381417,"s":"BTC_USDT","U":2517661101,"u":2517661113,"b":[{"p":"54672.1","s":0},{"p":"54664.5","s":"58794"}],"a":[{"p":"54743.6","s":0}]}}`,
			check: func(t *testing.T, message StreamMessage) {
				var book StreamOrderBookUpdate
				if err := message.Decode(&book); err != nil || book.LastUpdateID != 2517661113 ||
					book.Bids[1].Size != "58794" {
					t.Fatalf("book = %+v, error = %v", book, err)
				}
			},
		},
		{
			name:    "계정 체결",
			payload: `{"time":1700000000,"channel":"futures.usertrades","event":"update","result":[{"id":"3335259","create_time":1628736848,"create_time_ms":1628736848321,"contract":"BTC_USDT","order_id":"4872460","size":1,"price":"40000.4","role":"maker","text":"api","fee":0.0009290592,"point_fee":0}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var trades []StreamUserTrade
				if err := message.Decode(&trades); err != nil || !message.Private ||
					trades[0].Fee != "0.0009290592" {
					t.Fatalf("trades = %+v, error = %v", trades, err)
				}
			},
		},
		{
			name:    "잔고",
			payload: `{"time":1700000000,"channel":"futures.balances","event":"update","result":[{"balance":9.998739899488,"change":-0.000002074115,"text":"BTC_USD:3914424","time":1547199246,"time_ms":1547199246123,"type":"fee","user":"1666","currency":"btc"}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var balances []StreamBalance
				if err := message.Decode(&balances); err != nil ||
					balances[0].Balance != "9.998739899488" {
					t.Fatalf("balances = %+v, error = %v", balances, err)
				}
			},
		},
		{
			name:    "포지션",
			payload: `{"time":1700000000,"channel":"futures.positions","event":"update","result":[{"contract":"BTC_USDT","cross_leverage_limit":0,"entry_price":40000.36666661111,"leverage":0,"leverage_max":100,"liq_price":0.1,"maintenance_rate":0.005,"margin":49.999890611186,"mode":"single","realised_pnl":-1.25e-8,"risk_limit":100,"size":3,"time":1628736848,"time_ms":1628736848321,"user":"1666","update_id":170919}]}`,
			check: func(t *testing.T, message StreamMessage) {
				var positions []StreamPosition
				if err := message.Decode(&positions); err != nil ||
					positions[0].RealisedPNL != "-1.25e-8" || positions[0].UpdateID != 170919 {
					t.Fatalf("positions = %+v, error = %v", positions, err)
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

func TestGateIOFuturesStreamValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		subscription StreamSubscription
		private      bool
	}{
		{subscription: StreamSubscription{Channel: StreamChannelTicker, Contract: "btc_usdt"}},
		{subscription: StreamSubscription{
			Channel: StreamChannelCandles, Contract: "BTC_USDT", CandleInterval: Candle1Week,
		}},
		{subscription: StreamSubscription{
			Channel: StreamChannelOrderBookUpdate, Contract: "BTC_USDT",
			UpdateInterval: StreamUpdate20Millis, OrderBookLevel: 100,
		}},
		{subscription: StreamSubscription{
			Channel: StreamChannelBookTicker, Contract: "BTC_USDT",
			UpdateInterval: StreamUpdate100Millis,
		}},
		{subscription: StreamSubscription{Channel: StreamChannelOrders}, private: true},
		{subscription: StreamSubscription{
			Channel: StreamChannelBalances, Contract: "BTC_USDT",
		}, private: true},
	}
	for index, test := range tests {
		if err := validateStreamSubscription(test.subscription, test.private); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("subscription %d error = %v, want validation", index, err)
		}
	}
	valid := []StreamSubscription{
		{Channel: StreamChannelCandles, Contract: "mark_BTC_USDT", CandleInterval: Candle1Minute},
		{Channel: StreamChannelOrderBookUpdate, Contract: "BTC_USDT", UpdateInterval: StreamUpdate20Millis, OrderBookLevel: 20},
		{Channel: StreamChannelPositions, Contract: "!all"},
	}
	privateValues := []bool{false, false, true}
	for index, subscription := range valid {
		if err := validateStreamSubscription(subscription, privateValues[index]); err != nil {
			t.Fatalf("valid subscription %d error = %v", index, err)
		}
	}
}

func newTestGateIOFuturesStreamClient(
	t *testing.T,
	connector corestream.Connector,
	credentials *credential.Descriptor,
	provider credential.Provider,
	now func() time.Time,
) *StreamClient {
	t.Helper()
	client, err := NewStreamClient(StreamClientConfig{
		Connector: connector, Credentials: credentials, CredentialProvider: provider,
		UserID: "1666", DefaultEgressRouteID: "route-a",
		WebSocketURL: "ws://stream.example.test/v4/ws", AllowInsecureWebSocket: true,
		Now: now, Backoff: func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client
}

func testGateIOFuturesStreamCredentials(
	permissions ...credential.Permission,
) *credential.Descriptor {
	return &credential.Descriptor{
		AccountID: "gateio-futures-main", Exchange: model.ExchangeGateIO,
		SecretRef: "secret/gateio-futures-main", Permissions: permissions,
		AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a", "route-b"},
	}
}

func waitForGateIOFuturesWebSocketWrite(
	t *testing.T,
	connection *gateIOFuturesWebSocketTestConnection,
) []byte {
	t.Helper()
	select {
	case payload := <-connection.writes:
		return payload
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Gate.io Futures WebSocket write")
		return nil
	}
}

func decodeGateIOFuturesStreamCommand(
	t *testing.T,
	payload []byte,
) gateIOFuturesStreamCommand {
	t.Helper()
	var command gateIOFuturesStreamCommand
	if err := json.Unmarshal(payload, &command); err != nil {
		t.Fatalf("decode Gate.io Futures stream command: %v", err)
	}
	return command
}

func assertGateIOFuturesStreamCommand(
	t *testing.T,
	command gateIOFuturesStreamCommand,
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

func assertGateIOFuturesStreamSignature(
	t *testing.T,
	command gateIOFuturesStreamCommand,
	secret []byte,
) {
	t.Helper()
	payload := []byte("channel=" + command.Channel + "&event=" + command.Event +
		"&time=1700000000")
	mac := hmac.New(sha512.New, secret)
	_, _ = mac.Write(payload)
	want := hex.EncodeToString(mac.Sum(nil))
	if command.Auth == nil || !hmac.Equal([]byte(command.Auth.Signature), []byte(want)) {
		t.Fatalf("signature = %q, want %q", command.Auth.Signature, want)
	}
}
