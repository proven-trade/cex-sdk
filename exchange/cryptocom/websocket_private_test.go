package cryptocom

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/model"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

type cryptoComUserAuthentication struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	APIKey string `json:"api_key"`
	Sig    string `json:"sig"`
	Nonce  string `json:"nonce"`
}

func TestCryptoComPrivateStreamReauthenticatesAndRestoresSubscriptions(t *testing.T) {
	t.Parallel()
	first := newCryptoComWebSocketTestConnection()
	second := newCryptoComWebSocketTestConnection()
	connector := &cryptoComWebSocketTestConnector{
		connections: []*cryptoComWebSocketTestConnection{first, second},
	}
	provider := &recordingProvider{}
	fixedNow := time.UnixMilli(1_700_000_000_000)
	client := newTestCryptoComPrivateStreamClient(
		t, connector, provider, []transport.EgressRouteID{"route-a", "route-b"},
		[]credential.Permission{credential.PermissionRead}, fixedNow,
	)
	private, err := client.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{
		{Channel: StreamChannelUserOrders, InstrumentName: "BTC_USDT"},
		{Channel: StreamChannelUserBalances},
	}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	if calls, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("provider calls before Run() = %d", calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var orders []Order
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Channel != StreamChannelUserOrders {
				return nil
			}
			if err := message.Decode(&orders); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()

	firstAuth := assertCryptoComUserAuthentication(
		t, waitForCryptoComWebSocketWrite(t, first), fixedNow,
	)
	first.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		`{"id":"` + firstAuth.ID + `","method":"public/auth","code":"0","result":{}}`,
	)}
	assertCryptoComPrivateSubscriptions(t, first)
	first.reads <- cryptoComWebSocketReadResult{err: errors.New("connection lost")}

	secondAuth := assertCryptoComUserAuthentication(
		t, waitForCryptoComWebSocketWrite(t, second), fixedNow,
	)
	second.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		`{"id":"` + secondAuth.ID + `","method":"public/auth","code":"0","result":{}}`,
	)}
	assertCryptoComPrivateSubscriptions(t, second)
	second.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		`{"id":"-1","method":"subscribe","code":"0","result":{"instrument_name":"BTC_USDT","subscription":"user.order.BTC_USDT","channel":"user","data":[{"account_id":"cryptocom-main","order_id":"10001","client_oid":"strategy-1","order_type":"LIMIT","time_in_force":"GOOD_TILL_CANCEL","side":"BUY","quantity":"0.1","limit_price":"64000","cumulative_quantity":"0.01","status":"ACTIVE","instrument_name":"BTC_USDT","create_time":"1700000000000"}]}}`,
	)}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("private Run() did not finish")
	}
	if len(orders) != 1 || orders[0].OrderID != "10001" ||
		orders[0].Status != OrderStatusActive || orders[0].LimitPrice != "64000" {
		t.Fatalf("orders = %+v", orders)
	}
	if private.Generation() != 2 || private.EgressRouteID() != "route-b" {
		t.Fatalf("generation = %d, route = %q", private.Generation(), private.EgressRouteID())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) ||
		len(requests) != 2 {
		t.Fatalf("routes = %v, requests = %d", routes, len(requests))
	}
	for _, request := range requests {
		if request.Endpoint != "ws://stream.example.test/exchange/v1/user" || len(request.Header) != 0 {
			t.Fatalf("dial request = %+v", request)
		}
	}
	calls, apiKey, secret := provider.snapshot()
	if calls != 2 || !allZero(apiKey) || !allZero(secret) {
		t.Fatalf(
			"provider calls = %d, key zero = %v, secret zero = %v",
			calls, allZero(apiKey), allZero(secret),
		)
	}
	_ = private.Close()
}

func TestCryptoComPrivateStreamHeartbeatAndRejectedSubscriptionRollback(t *testing.T) {
	t.Parallel()
	connection := newCryptoComWebSocketTestConnection()
	client := newTestCryptoComPrivateStreamClient(
		t, &cryptoComWebSocketTestConnector{
			connections: []*cryptoComWebSocketTestConnection{connection},
		}, &recordingProvider{}, []transport.EgressRouteID{"route-a"},
		[]credential.Permission{credential.PermissionRead},
		time.UnixMilli(1_700_000_000_000),
	)
	initial := StreamSubscription{Channel: StreamChannelUserBalances}
	private, err := client.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{initial}})
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	heartbeatSeen := make(chan struct{}, 1)
	rejectedSeen := make(chan struct{}, 1)
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Heartbeat {
				heartbeatSeen <- struct{}{}
			}
			if message.Error != nil {
				rejectedSeen <- struct{}{}
			}
			return nil
		})
	}()
	auth := assertCryptoComUserAuthentication(
		t, waitForCryptoComWebSocketWrite(t, connection), time.UnixMilli(1_700_000_000_000),
	)
	connection.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		`{"id":"` + auth.ID + `","method":"public/auth","code":"0","result":{}}`,
	)}
	_ = decodeCryptoComStreamCommand(t, waitForCryptoComWebSocketWrite(t, connection))
	connection.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		`{"id":"99","method":"public/heartbeat","code":"0"}`,
	)}
	select {
	case <-heartbeatSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("private heartbeat was not delivered")
	}
	heartbeat := waitForCryptoComWebSocketWrite(t, connection)
	if string(heartbeat.Data) != `{"id":"99","method":"public/respond-heartbeat"}` {
		t.Fatalf("heartbeat response = %s", heartbeat.Data)
	}
	dynamic := StreamSubscription{
		Channel: StreamChannelUserTrades, InstrumentName: "ETH_USDT",
	}
	if err := private.Subscribe(ctx, dynamic); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	command := decodeCryptoComStreamCommand(t, waitForCryptoComWebSocketWrite(t, connection))
	assertCryptoComStreamCommand(t, command, "subscribe", "user.trade.ETH_USDT")
	connection.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		`{"id":"` + command.ID + `","method":"subscribe","code":"40001","message":"BAD_REQUEST","original":"redacted"}`,
	)}
	select {
	case <-rejectedSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("rejected private subscription response was not delivered")
	}
	if subscriptions := private.managed.snapshotSubscriptions(); len(subscriptions) != 1 || subscriptions[0] != initial {
		t.Fatalf("subscriptions = %+v", subscriptions)
	}
	cancel()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("private Run() did not finish")
	}
}

func TestCryptoComPrivateStreamReturnsAuthenticationFailure(t *testing.T) {
	t.Parallel()
	connection := newCryptoComWebSocketTestConnection()
	client := newTestCryptoComPrivateStreamClient(
		t, &cryptoComWebSocketTestConnector{
			connections: []*cryptoComWebSocketTestConnection{connection},
		}, &recordingProvider{}, []transport.EgressRouteID{"route-a"},
		[]credential.Permission{credential.PermissionRead},
		time.UnixMilli(1_700_000_000_000),
	)
	private, err := client.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelUserOrders,
	}}})
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- private.Run(context.Background(), func(context.Context, StreamMessage) error {
			return nil
		})
	}()
	auth := assertCryptoComUserAuthentication(
		t, waitForCryptoComWebSocketWrite(t, connection), time.UnixMilli(1_700_000_000_000),
	)
	connection.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		`{"id":"` + auth.ID + `","method":"public/auth","code":"40101","message":"UNAUTHORIZED","original":"redacted"}`,
	)}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, trade.ErrAuthentication) {
			t.Fatalf("Run() error = %v, want authentication", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("private authentication failure was not returned")
	}
}

func TestCryptoComPrivateStreamChecksRouteAndPermissionBeforeSecret(t *testing.T) {
	t.Parallel()
	provider := &recordingProvider{}
	client := newTestCryptoComPrivateStreamClient(
		t, &cryptoComWebSocketTestConnector{}, provider,
		[]transport.EgressRouteID{"route-a"}, []credential.Permission{credential.PermissionTrade},
		time.UnixMilli(1_700_000_000_000),
	)
	request := StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelUserBalances,
	}}}
	if _, err := client.PrivateStream(request, trade.WithEgressRoute("route-b")); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("route error = %v, want authorization", err)
	}
	if _, err := client.PrivateStream(request); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("permission error = %v, want authorization", err)
	}
	if calls, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestCryptoComDecodeStreamMessageSupportsPrivateChannels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		payload    string
		channel    StreamChannel
		instrument string
		target     any
	}{
		{
			name: "orders", channel: StreamChannelUserOrders,
			payload: `{"id":"1","method":"subscribe","code":"0","result":{"subscription":"user.order","channel":"user","data":[{"order_id":"1","status":"ACTIVE","instrument_name":"BTC_USDT"}]}}`,
			target:  &[]Order{},
		},
		{
			name: "trades", channel: StreamChannelUserTrades, instrument: "BTC_USDT",
			payload: `{"id":"-1","method":"subscribe","code":"0","result":{"instrument_name":"BTC_USDT","subscription":"user.trade.BTC_USDT","channel":"user","data":[{"trade_id":"2","order_id":"1","traded_quantity":"0.1","traded_price":"64000","instrument_name":"BTC_USDT"}]}}`,
			target:  &[]AccountTrade{},
		},
		{
			name: "balances", channel: StreamChannelUserBalances,
			payload: `{"id":"-1","method":"subscribe","code":"0","result":{"subscription":"user.balance","channel":"user","data":[{"instrument_name":"USD","total_available_balance":"100","position_balances":[]}]}}`,
			target:  &[]BalanceAccount{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			message, err := DecodeStreamMessage(cryptoComTextMessage(test.payload))
			if err != nil {
				t.Fatalf("DecodeStreamMessage() error = %v", err)
			}
			if !message.Private || message.Channel != test.channel ||
				message.InstrumentName != test.instrument || message.Subscription == "" {
				t.Fatalf("message = %+v", message)
			}
			if err := message.Decode(test.target); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
		})
	}
	auth, err := DecodeStreamMessage(cryptoComTextMessage(
		`{"id":"7","method":"public/auth","code":"0","result":{}}`,
	))
	if err != nil || auth.ID != "7" || auth.Method != "public/auth" || auth.Error != nil {
		t.Fatalf("auth = %+v, error = %v", auth, err)
	}
}

func TestCryptoComPrivateStreamValidation(t *testing.T) {
	t.Parallel()
	valid := StreamSubscription{
		Channel: StreamChannelUserOrders, InstrumentName: "BTC_USDT",
	}
	invalid := [][]StreamSubscription{
		nil,
		{{Channel: StreamChannelTicker, InstrumentName: "BTC_USDT"}},
		{{Channel: StreamChannelUserBalances, InstrumentName: "BTC_USDT"}},
		{{Channel: StreamChannelUserTrades, InstrumentName: "bad"}},
		{{Channel: StreamChannelUserOrders, CandleTimeframe: Candle1Minute}},
		{valid, valid},
	}
	for index, subscriptions := range invalid {
		if _, err := validateCryptoComPrivateStreamSubscriptions(subscriptions, true); err == nil {
			t.Fatalf("invalid subscriptions %d error = nil", index)
		}
	}
}

func TestCryptoComPrivateStreamClientConfigurationValidation(t *testing.T) {
	t.Parallel()
	connector := &cryptoComWebSocketTestConnector{}
	descriptor := credential.Descriptor{
		AccountID: "cryptocom-main", Exchange: model.ExchangeCryptoCom,
		SecretRef: "secret/cryptocom-main", Permissions: []credential.Permission{credential.PermissionRead},
		AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
	}
	tests := []StreamClientConfig{
		{
			Connector: connector, DefaultEgressRouteID: "route-a",
			UserWebSocketURL: "https://example.test",
		},
		{
			Connector: connector, DefaultEgressRouteID: "route-a",
			UserRequestsPerSecond: 151,
		},
		{
			Connector: connector, DefaultEgressRouteID: "route-a",
			Credentials: &descriptor,
		},
		{
			Connector: connector, DefaultEgressRouteID: "route-a",
			CredentialProvider: &recordingProvider{},
		},
	}
	for index, config := range tests {
		if _, err := NewStreamClient(config); err == nil {
			t.Fatalf("invalid private stream config %d error = nil", index)
		}
	}
}

func assertCryptoComUserAuthentication(
	t *testing.T,
	message corestream.Message,
	now time.Time,
) cryptoComUserAuthentication {
	t.Helper()
	if message.Type != corestream.MessageText {
		t.Fatalf("authentication message type = %d", message.Type)
	}
	var authentication cryptoComUserAuthentication
	if err := json.Unmarshal(message.Data, &authentication); err != nil {
		t.Fatalf("decode private stream authentication: %v", err)
	}
	nonce := strconv.FormatInt(now.UTC().UnixMilli(), 10)
	wantSignature, err := Sign(
		"public/auth", authentication.ID, []byte("test-api-key"), map[string]any{},
		nonce, []byte("test-secret"),
	)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if authentication.ID == "" || authentication.Method != "public/auth" ||
		authentication.APIKey != "test-api-key" || authentication.Nonce != nonce ||
		authentication.Sig != wantSignature {
		t.Fatalf("authentication = %+v", authentication)
	}
	return authentication
}

func assertCryptoComPrivateSubscriptions(
	t *testing.T,
	connection *cryptoComWebSocketTestConnection,
) {
	t.Helper()
	commands := []cryptoComStreamCommand{
		decodeCryptoComStreamCommand(t, waitForCryptoComWebSocketWrite(t, connection)),
		decodeCryptoComStreamCommand(t, waitForCryptoComWebSocketWrite(t, connection)),
	}
	for _, command := range commands {
		if len(command.Params.Channels) != 1 {
			t.Fatalf("subscription command = %+v", command)
		}
	}
	channels := []string{commands[0].Params.Channels[0], commands[1].Params.Channels[0]}
	if !slices.Equal(channels, []string{"user.balance", "user.order.BTC_USDT"}) {
		t.Fatalf("subscription channels = %v", channels)
	}
	for _, command := range commands {
		if command.ID == "" || command.Method != "subscribe" ||
			command.Nonce != "1700000000000" {
			t.Fatalf("subscription command = %+v", command)
		}
	}
}

func newTestCryptoComPrivateStreamClient(
	t *testing.T,
	connector *cryptoComWebSocketTestConnector,
	provider credential.Provider,
	routes []transport.EgressRouteID,
	permissions []credential.Permission,
	now time.Time,
) *StreamClient {
	t.Helper()
	client, err := NewStreamClient(StreamClientConfig{
		Connector: connector,
		Credentials: &credential.Descriptor{
			AccountID: "cryptocom-main", Exchange: model.ExchangeCryptoCom,
			SecretRef: "secret/cryptocom-main", Permissions: permissions,
			AllowedEgressRouteIDs: routes,
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		MarketWebSocketURL:     "ws://stream.example.test/exchange/v1/market",
		UserWebSocketURL:       "ws://stream.example.test/exchange/v1/user",
		AllowInsecureWebSocket: true, ConnectionReadyDelay: time.Nanosecond,
		Now: func() time.Time { return now }, Backoff: func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client
}
