package cryptocom

import (
	"context"
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

type cryptoComWebSocketReadResult struct {
	message corestream.Message
	err     error
}

type cryptoComWebSocketTestConnection struct {
	reads     chan cryptoComWebSocketReadResult
	writes    chan corestream.Message
	closed    chan struct{}
	closeOnce sync.Once
}

func newCryptoComWebSocketTestConnection() *cryptoComWebSocketTestConnection {
	return &cryptoComWebSocketTestConnection{
		reads:  make(chan cryptoComWebSocketReadResult, 16),
		writes: make(chan corestream.Message, 16), closed: make(chan struct{}),
	}
}

func (connection *cryptoComWebSocketTestConnection) Read(
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

func (connection *cryptoComWebSocketTestConnection) Write(
	ctx context.Context,
	message corestream.Message,
) error {
	message.Data = cloneBytes(message.Data)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-connection.closed:
		return corestream.ErrSessionClosed
	case connection.writes <- message:
		return nil
	}
}

func (connection *cryptoComWebSocketTestConnection) Ping(context.Context) error {
	return nil
}

func (connection *cryptoComWebSocketTestConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type cryptoComWebSocketTestConnector struct {
	mu          sync.Mutex
	connections []*cryptoComWebSocketTestConnection
	routes      []transport.EgressRouteID
	requests    []corestream.DialRequest
}

func (connector *cryptoComWebSocketTestConnector) Connect(
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
		return nil, errors.New("no Crypto.com test connection")
	}
	connection := connector.connections[0]
	connector.connections = connector.connections[1:]
	return connection, nil
}

func (connector *cryptoComWebSocketTestConnector) snapshot() (
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

type cryptoComStreamCommand struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params struct {
		Channels             []string `json:"channels"`
		BookSubscriptionType string   `json:"book_subscription_type"`
		BookUpdateFrequency  string   `json:"book_update_frequency"`
	} `json:"params"`
	Nonce string `json:"nonce"`
}

func TestCryptoComPublicStreamRestoresSubscriptionOnSelectedRoute(t *testing.T) {
	t.Parallel()
	first := newCryptoComWebSocketTestConnection()
	second := newCryptoComWebSocketTestConnection()
	connector := &cryptoComWebSocketTestConnector{
		connections: []*cryptoComWebSocketTestConnection{first, second},
	}
	client := newTestCryptoComStreamClient(t, connector)
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelTicker, InstrumentName: "BTC_USDT",
	}}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var tickers []StreamTicker
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Channel != StreamChannelTicker {
				return nil
			}
			if err := message.Decode(&tickers); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()
	firstCommand := decodeCryptoComStreamCommand(t, waitForCryptoComWebSocketWrite(t, first))
	assertCryptoComStreamCommand(t, firstCommand, "subscribe", "ticker.BTC_USDT")
	first.reads <- cryptoComWebSocketReadResult{err: errors.New("connection lost")}
	secondCommand := decodeCryptoComStreamCommand(t, waitForCryptoComWebSocketWrite(t, second))
	assertCryptoComStreamCommand(t, secondCommand, "subscribe", "ticker.BTC_USDT")
	second.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		`{"id":"-1","method":"subscribe","code":"0","result":{"instrument_name":"BTC_USDT","subscription":"ticker.BTC_USDT","channel":"ticker","data":[{"i":"BTC_USDT","a":"64000","b":"63999","k":"64001","t":"1700000000000"}]}}`,
	)}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("public Run() did not finish")
	}
	if len(tickers) != 1 || tickers[0].LatestPrice != "64000" || tickers[0].BestBid != "63999" {
		t.Fatalf("tickers = %+v", tickers)
	}
	if public.Generation() != 2 || public.EgressRouteID() != "route-b" {
		t.Fatalf("generation = %d, route = %q", public.Generation(), public.EgressRouteID())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) ||
		len(requests) != 2 {
		t.Fatalf("routes = %v, requests = %d", routes, len(requests))
	}
	for _, request := range requests {
		if request.Endpoint != "ws://stream.example.test/exchange/v1/market" || len(request.Header) != 0 {
			t.Fatalf("dial request = %+v", request)
		}
	}
	_ = public.Close()
}

func TestCryptoComPublicStreamHeartbeatAndRejectedSubscriptionRollback(t *testing.T) {
	t.Parallel()
	connection := newCryptoComWebSocketTestConnection()
	client := newTestCryptoComStreamClient(t, &cryptoComWebSocketTestConnector{
		connections: []*cryptoComWebSocketTestConnection{connection},
	})
	initial := StreamSubscription{Channel: StreamChannelTrades, InstrumentName: "BTC_USDT"}
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{initial}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	heartbeatSeen := make(chan struct{}, 1)
	rejectedSeen := make(chan struct{}, 1)
	go func() {
		done <- public.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Heartbeat {
				heartbeatSeen <- struct{}{}
			}
			if message.Error != nil {
				rejectedSeen <- struct{}{}
			}
			return nil
		})
	}()
	initialCommand := decodeCryptoComStreamCommand(t, waitForCryptoComWebSocketWrite(t, connection))
	connection.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		`{"id":"` + initialCommand.ID + `","method":"public/heartbeat","code":"0"}`,
	)}
	select {
	case <-heartbeatSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat was not delivered")
	}
	heartbeat := waitForCryptoComWebSocketWrite(t, connection)
	if string(heartbeat.Data) != `{"id":"`+initialCommand.ID+`","method":"public/respond-heartbeat"}` {
		t.Fatalf("heartbeat response = %s", heartbeat.Data)
	}
	connection.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		`{"id":"` + initialCommand.ID + `","method":"public/respond-heartbeat","code":"0"}`,
	)}
	dynamic := StreamSubscription{
		Channel: StreamChannelCandles, InstrumentName: "ETH_USDT", CandleTimeframe: Candle5Minutes,
	}
	if err := public.Subscribe(ctx, dynamic); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	command := decodeCryptoComStreamCommand(t, waitForCryptoComWebSocketWrite(t, connection))
	assertCryptoComStreamCommand(t, command, "subscribe", "candlestick.5m.ETH_USDT")
	connection.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		`{"id":"` + command.ID + `","method":"subscribe","code":"40001","message":"BAD_REQUEST","original":"redacted"}`,
	)}
	select {
	case <-rejectedSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("rejected subscription response was not delivered")
	}
	if subscriptions := public.managed.snapshotSubscriptions(); len(subscriptions) != 1 || subscriptions[0] != initial {
		t.Fatalf("subscriptions = %+v", subscriptions)
	}
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

func TestCryptoComDecodeStreamMessageSupportsPublicChannels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		payload    string
		channel    StreamChannel
		instrument string
		depth      int
		target     any
	}{
		{
			name: "ticker", channel: StreamChannelTicker, instrument: "BTC_USDT",
			payload: `{"id":"-1","method":"subscribe","code":"0","result":{"instrument_name":"BTC_USDT","subscription":"ticker.BTC_USDT","channel":"ticker","data":[{"i":"BTC_USDT","h":"2","l":"1","a":"1.5","v":"10","t":"1"}]}}`,
			target:  &[]StreamTicker{},
		},
		{
			name: "trades", channel: StreamChannelTrades, instrument: "BTC_USDT",
			payload: `{"id":"-1","method":"subscribe","code":"0","result":{"instrument_name":"BTC_USDT","subscription":"trade.BTC_USDT","channel":"trade","data":[{"d":"1","m":"2","t":"3","p":"4","q":"5","s":"BUY","i":"BTC_USDT"}]}}`,
			target:  &[]StreamTrade{},
		},
		{
			name: "candles", channel: StreamChannelCandles, instrument: "ETH_USDT",
			payload: `{"id":"-1","method":"subscribe","code":"0","result":{"instrument_name":"ETH_USDT","subscription":"candlestick.1m.ETH_USDT","channel":"candlestick","data":[{"o":"1","h":"3","l":"0.5","c":"2","v":"4","t":"5"}]}}`,
			target:  &[]StreamCandle{},
		},
		{
			name: "book snapshot", channel: StreamChannelBook, instrument: "BTC_USDT", depth: 10,
			payload: `{"id":"-1","method":"subscribe","code":"0","result":{"instrument_name":"BTC_USDT","subscription":"book.BTC_USDT.10","channel":"book","depth":"10","data":[{"bids":[["1","2",1]],"asks":[["3","4",1]],"t":"5","tt":"6","u":"7"}]}}`,
			target:  &[]StreamBookEvent{},
		},
		{
			name: "book update", channel: StreamChannelBook, instrument: "BTC_USDT", depth: 50,
			payload: `{"id":"-1","method":"subscribe","code":"0","result":{"instrument_name":"BTC_USDT","subscription":"book.BTC_USDT.50","channel":"book","depth":50,"data":[{"update":{"bids":[["1","0",0]],"asks":[]},"tt":"6","u":"8","pu":"7"}]}}`,
			target:  &[]StreamBookEvent{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			message, err := DecodeStreamMessage(cryptoComTextMessage(test.payload))
			if err != nil {
				t.Fatalf("DecodeStreamMessage() error = %v", err)
			}
			if message.Channel != test.channel || message.InstrumentName != test.instrument ||
				message.Depth != test.depth || message.Subscription == "" || len(message.Raw) == 0 {
				t.Fatalf("message = %+v", message)
			}
			if err := message.Decode(test.target); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
		})
	}
}

func TestCryptoComDecodeStreamMessageClassifiesControlAndRejectsMalformedFrames(t *testing.T) {
	t.Parallel()
	ack, err := DecodeStreamMessage(cryptoComTextMessage(
		`{"id":"1","method":"subscribe","code":"0","result":{}}`,
	))
	if err != nil || ack.ID != "1" || ack.Method != "subscribe" || ack.Error != nil ||
		ack.Subscription != "" {
		t.Fatalf("ack = %+v, error = %v", ack, err)
	}
	rejected, err := DecodeStreamMessage(cryptoComTextMessage(
		`{"id":"2","method":"subscribe","code":"40107","message":"EXCEED_MAX_SUBSCRIPTIONS","original":"redacted"}`,
	))
	if err != nil || rejected.Error == nil || rejected.Error.Code != "40107" ||
		rejected.Error.Original != "redacted" {
		t.Fatalf("rejected = %+v, error = %v", rejected, err)
	}
	invalid := []corestream.Message{
		{Type: corestream.MessageBinary, Data: []byte(`{}`)},
		cryptoComTextMessage(`[]`),
		cryptoComTextMessage(`{"method":"subscribe","code":"0"}`),
		cryptoComTextMessage(`{"id":"1","method":"public/heartbeat","code":"0","result":{}}`),
		cryptoComTextMessage(`{"id":"-1","method":"subscribe","code":"0","result":{"instrument_name":"BTC_USDT","subscription":"unknown.BTC_USDT","channel":"unknown","data":[]}}`),
		cryptoComTextMessage(`{"id":"-1","method":"subscribe","code":"0","result":{"instrument_name":"ETH_USDT","subscription":"ticker.BTC_USDT","channel":"ticker","data":[]}}`),
		cryptoComTextMessage(`{"id":"-1","method":"subscribe","code":"0","result":{"instrument_name":"BTC_USDT","subscription":"ticker.BTC_USDT","channel":"ticker"}}`),
	}
	for index, message := range invalid {
		if _, err := DecodeStreamMessage(message); err == nil {
			t.Fatalf("invalid message %d error = nil", index)
		}
	}
	if err := ack.Decode(&[]StreamTicker{}); err == nil {
		t.Fatal("control Decode() error = nil")
	}
}

func TestCryptoComStreamValidation(t *testing.T) {
	t.Parallel()
	validBook := StreamSubscription{
		Channel: StreamChannelBook, InstrumentName: "BTC_USDT", BookDepth: StreamBookDepth10,
		BookSubscriptionType: StreamBookSnapshotAndUpdate,
		BookUpdateFrequency:  StreamBookUpdate100Milliseconds,
	}
	invalid := [][]StreamSubscription{
		nil,
		{{Channel: StreamChannelTicker, InstrumentName: "bad"}},
		{{Channel: StreamChannelCandles, InstrumentName: "BTC_USDT"}},
		{{Channel: StreamChannelBook, InstrumentName: "BTC_USDT"}},
		{{Channel: StreamChannelBook, InstrumentName: "BTC_USDT", BookDepth: StreamBookDepth10, BookSubscriptionType: StreamBookSnapshot, BookUpdateFrequency: StreamBookUpdate100Milliseconds}},
		{{Channel: StreamChannelTicker, InstrumentName: "BTC_USDT", CandleTimeframe: Candle1Minute}},
		{validBook, validBook},
	}
	for index, subscriptions := range invalid {
		if _, err := validateCryptoComStreamSubscriptions(subscriptions, true); err == nil {
			t.Fatalf("invalid subscriptions %d error = nil", index)
		}
	}
	client := newTestCryptoComStreamClient(t, &cryptoComWebSocketTestConnector{})
	payload, _, err := client.encodeStreamCommand("subscribe", validBook)
	if err != nil {
		t.Fatalf("encodeStreamCommand(subscribe) error = %v", err)
	}
	bookCommand := decodeCryptoComStreamCommand(
		t, corestream.Message{Type: corestream.MessageText, Data: payload},
	)
	if bookCommand.Params.BookSubscriptionType != "SNAPSHOT_AND_UPDATE" ||
		bookCommand.Params.BookUpdateFrequency != "100" {
		t.Fatalf("book subscribe command = %+v", bookCommand)
	}
	payload, _, err = client.encodeStreamCommand("unsubscribe", validBook)
	if err != nil {
		t.Fatalf("encodeStreamCommand(unsubscribe) error = %v", err)
	}
	bookCommand = decodeCryptoComStreamCommand(
		t, corestream.Message{Type: corestream.MessageText, Data: payload},
	)
	if bookCommand.Params.BookSubscriptionType != "" ||
		bookCommand.Params.BookUpdateFrequency != "" {
		t.Fatalf("book unsubscribe command = %+v", bookCommand)
	}
	if _, err := client.PublicStream(
		StreamRequest{Subscriptions: []StreamSubscription{validBook}},
		trade.WithTimeout(time.Second),
	); err == nil {
		t.Fatal("PublicStream() timeout error = nil")
	}
}

func TestCryptoComStreamClientConfigurationValidation(t *testing.T) {
	t.Parallel()
	connector := &cryptoComWebSocketTestConnector{}
	tests := []StreamClientConfig{
		{DefaultEgressRouteID: "route-a"},
		{Connector: connector},
		{Connector: connector, DefaultEgressRouteID: "route-a", MarketWebSocketURL: "https://example.test"},
		{Connector: connector, DefaultEgressRouteID: "route-a", MarketRequestsPerSecond: 101},
		{Connector: connector, DefaultEgressRouteID: "route-a", ConnectionReadyDelay: -1},
		{Connector: connector, DefaultEgressRouteID: "route-a", MaxReconnectAttempts: -1},
	}
	for index, config := range tests {
		if _, err := NewStreamClient(config); err == nil {
			t.Fatalf("invalid config %d error = nil", index)
		}
	}
}

func newTestCryptoComStreamClient(
	t *testing.T,
	connector *cryptoComWebSocketTestConnector,
) *StreamClient {
	t.Helper()
	client, err := NewStreamClient(StreamClientConfig{
		Connector: connector, DefaultEgressRouteID: "route-a",
		MarketWebSocketURL:     "ws://stream.example.test/exchange/v1/market",
		AllowInsecureWebSocket: true, ConnectionReadyDelay: time.Nanosecond,
		Now:     func() time.Time { return time.UnixMilli(1_700_000_000_000) },
		Backoff: func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client
}

func waitForCryptoComWebSocketWrite(
	t *testing.T,
	connection *cryptoComWebSocketTestConnection,
) corestream.Message {
	t.Helper()
	select {
	case message := <-connection.writes:
		return message
	case <-time.After(2 * time.Second):
		t.Fatal("Crypto.com WebSocket write timeout")
		return corestream.Message{}
	}
}

func decodeCryptoComStreamCommand(
	t *testing.T,
	message corestream.Message,
) cryptoComStreamCommand {
	t.Helper()
	if message.Type != corestream.MessageText {
		t.Fatalf("command message type = %d", message.Type)
	}
	var command cryptoComStreamCommand
	if err := json.Unmarshal(message.Data, &command); err != nil {
		t.Fatalf("decode stream command: %v", err)
	}
	return command
}

func assertCryptoComStreamCommand(
	t *testing.T,
	command cryptoComStreamCommand,
	method string,
	channel string,
) {
	t.Helper()
	if command.ID == "" || command.Method != method || command.Nonce != "1700000000000" ||
		!slices.Equal(command.Params.Channels, []string{channel}) {
		t.Fatalf("command = %+v", command)
	}
}

func cryptoComTextMessage(payload string) corestream.Message {
	return corestream.Message{Type: corestream.MessageText, Data: []byte(payload)}
}
