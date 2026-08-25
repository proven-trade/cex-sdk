package htx

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	"github.com/proven-trade/proven-trade-sdk/model"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type htxPrivateAuthentication struct {
	Action  string `json:"action"`
	Channel string `json:"ch"`
	Params  struct {
		AuthType         string `json:"authType"`
		AccessKey        string `json:"accessKey"`
		SignatureMethod  string `json:"signatureMethod"`
		SignatureVersion string `json:"signatureVersion"`
		Timestamp        string `json:"timestamp"`
		Signature        string `json:"signature"`
	} `json:"params"`
}

type htxPrivateCommand struct {
	Action  string `json:"action"`
	Channel string `json:"ch"`
}

func TestHTXPrivateStreamReauthenticatesAndRestoresSubscriptions(t *testing.T) {
	t.Parallel()

	first := newHTXWebSocketTestConnection()
	second := newHTXWebSocketTestConnection()
	connector := &htxWebSocketTestConnector{
		connections: []*htxWebSocketTestConnection{first, second},
	}
	provider := &recordingProvider{}
	fixedNow := time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC)
	client := newTestHTXPrivateStreamClient(t, connector, provider, fixedNow)
	private, err := client.PrivateStream(
		StreamRequest{Subscriptions: []StreamSubscription{
			{Channel: StreamChannelOrders, Symbol: "btcusdt"},
			{Channel: StreamChannelAccounts, Mode: StreamModeBalanceAndAvailable},
		}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var order StreamOrderEvent
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Channel != StreamChannelOrders || message.Action != "push" {
				return nil
			}
			if err := message.Decode(&order); err != nil {
				return err
			}
			cancel()
			return nil
		})
	}()

	assertHTXPrivateAuthentication(t, waitForHTXWebSocketWrite(t, first), fixedNow)
	first.reads <- htxWebSocketReadResult{message: htxPrivateTextMessage(
		`{"action":"req","code":200,"ch":"auth","data":{}}`,
	)}
	assertHTXPrivateSubscriptions(t, first)
	first.reads <- htxWebSocketReadResult{err: errors.New("connection lost")}

	assertHTXPrivateAuthentication(t, waitForHTXWebSocketWrite(t, second), fixedNow)
	second.reads <- htxWebSocketReadResult{message: htxPrivateTextMessage(
		`{"action":"req","code":200,"ch":"auth","data":{}}`,
	)}
	assertHTXPrivateSubscriptions(t, second)
	second.reads <- htxWebSocketReadResult{message: htxPrivateTextMessage(
		`{"action":"push","ch":"orders#btcusdt","data":{"eventType":"trade","symbol":"btcusdt","accountId":12345,"orderId":10001,"clientOrderId":"strategy-1","orderSource":"spot-api","orderPrice":"64000","orderSize":"0.1","type":"buy-limit","orderSide":"buy","orderStatus":"partial-filled","tradePrice":"64000","tradeVolume":"0.01","tradeId":20001,"tradeTime":1787627045000,"aggressor":false,"remainAmt":"0.09","execAmt":"0.01"}}`,
	)}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("private Run() did not finish")
	}
	if order.OrderID != "10001" || order.TradeID != "20001" ||
		order.TradeVolume == nil || *order.TradeVolume != "0.01" ||
		order.OrderStatus != "partial-filled" {
		t.Fatalf("order = %+v", order)
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
		if request.Endpoint != "ws://stream.example.test/ws/v2" || len(request.Header) != 0 {
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

func TestHTXPrivateStreamAnswersHeartbeat(t *testing.T) {
	t.Parallel()

	connection := newHTXWebSocketTestConnection()
	client := newTestHTXPrivateStreamClient(
		t, &htxWebSocketTestConnector{
			connections: []*htxWebSocketTestConnection{connection},
		},
		&recordingProvider{}, time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC),
	)
	private, err := client.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelAccounts, Mode: StreamModeBalanceOrAvailable,
	}}})
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	seen := make(chan struct{}, 1)
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Ping != nil {
				seen <- struct{}{}
			}
			return nil
		})
	}()
	_ = waitForHTXWebSocketWrite(t, connection)
	connection.reads <- htxWebSocketReadResult{message: htxPrivateTextMessage(
		`{"action":"req","code":200,"ch":"auth","data":{}}`,
	)}
	_ = waitForHTXWebSocketWrite(t, connection)
	connection.reads <- htxWebSocketReadResult{message: htxPrivateTextMessage(
		`{"action":"ping","data":{"ts":1787627045123}}`,
	)}
	select {
	case <-seen:
	case <-time.After(2 * time.Second):
		t.Fatal("private heartbeat was not delivered")
	}
	pong := waitForHTXWebSocketWrite(t, connection)
	if pong.Type != corestream.MessageText ||
		string(pong.Data) != `{"action":"pong","data":{"ts":1787627045123}}` {
		t.Fatalf("pong = type %d, %s", pong.Type, pong.Data)
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

func TestHTXPrivateStreamRollsBackRejectedSubscription(t *testing.T) {
	t.Parallel()

	connection := newHTXWebSocketTestConnection()
	client := newTestHTXPrivateStreamClient(
		t, &htxWebSocketTestConnector{
			connections: []*htxWebSocketTestConnection{connection},
		},
		&recordingProvider{}, time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC),
	)
	initial := StreamSubscription{
		Channel: StreamChannelAccounts, Mode: StreamModeBalanceAndAvailable,
	}
	private, err := client.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{initial}})
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	rejected := make(chan struct{}, 1)
	go func() {
		done <- private.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Error != nil {
				rejected <- struct{}{}
			}
			return nil
		})
	}()
	_ = waitForHTXWebSocketWrite(t, connection)
	connection.reads <- htxWebSocketReadResult{message: htxPrivateTextMessage(
		`{"action":"req","code":200,"ch":"auth","data":{}}`,
	)}
	_ = waitForHTXWebSocketWrite(t, connection)
	dynamic := StreamSubscription{Channel: StreamChannelOrders, Symbol: "ethusdt"}
	if err := private.Subscribe(ctx, dynamic); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	command := decodeHTXPrivateCommand(t, waitForHTXWebSocketWrite(t, connection))
	if command.Action != "sub" || command.Channel != "orders#ethusdt" {
		t.Fatalf("command = %+v", command)
	}
	connection.reads <- htxWebSocketReadResult{message: htxPrivateTextMessage(
		`{"action":"sub","code":2001,"ch":"orders#ethusdt","message":"invalid.symbol"}`,
	)}
	select {
	case <-rejected:
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

func TestHTXPrivateStreamReturnsAuthenticationFailure(t *testing.T) {
	t.Parallel()

	connection := newHTXWebSocketTestConnection()
	client := newTestHTXPrivateStreamClient(
		t, &htxWebSocketTestConnector{
			connections: []*htxWebSocketTestConnection{connection},
		},
		&recordingProvider{}, time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC),
	)
	private, err := client.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelOrders, Symbol: "*",
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
	_ = waitForHTXWebSocketWrite(t, connection)
	connection.reads <- htxWebSocketReadResult{message: htxPrivateTextMessage(
		`{"action":"req","code":2002,"ch":"auth","message":"auth.fail"}`,
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

func TestHTXDecodePrivateStreamMessageSupportsAccountChannels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		channel StreamChannel
		symbol  string
		mode    StreamMode
		target  any
	}{
		{
			name: "orders", channel: StreamChannelOrders, symbol: "btcusdt",
			payload: `{"action":"push","ch":"orders#btcusdt","data":{"eventType":"creation","symbol":"btcusdt","accountId":1,"orderId":2,"orderPrice":"3","orderSize":"4","type":"buy-limit","orderStatus":"submitted"}}`,
			target:  &StreamOrderEvent{},
		},
		{
			name: "clearing", channel: StreamChannelClearing, symbol: "*",
			mode:    StreamModeTradesAndCancellations,
			payload: `{"ch":"trade.clearing#*#1","data":{"eventType":"trade","symbol":"btcusdt","accountId":1,"orderId":2,"orderSide":"buy","orderType":"buy-limit","tradePrice":"3","tradeVolume":"4","tradeId":5,"tradeTime":6,"transactFee":"0.1"}}`,
			target:  &StreamClearingEvent{},
		},
		{
			name: "accounts", channel: StreamChannelAccounts,
			mode:    StreamModeBalanceAndAvailable,
			payload: `{"action":"push","ch":"accounts.update#2","data":{"currency":"btc","accountId":1,"balance":"2","available":"1.5","changeType":"order-match","accountType":"trade","seqNum":3,"changeTime":4}}`,
			target:  &StreamAccountEvent{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			message, err := DecodePrivateStreamMessage(htxPrivateTextMessage(test.payload))
			if err != nil {
				t.Fatalf("DecodePrivateStreamMessage() error = %v", err)
			}
			if !message.Private || message.Channel != test.channel ||
				message.Symbol != test.symbol || message.Mode != test.mode || len(message.Raw) == 0 {
				t.Fatalf("message = %+v", message)
			}
			if err := message.Decode(test.target); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
		})
	}
	if _, err := DecodePrivateStreamMessage(corestream.Message{
		Type: corestream.MessageBinary, Data: []byte(`{}`),
	}); err == nil {
		t.Fatal("DecodePrivateStreamMessage() binary error = nil")
	}
}

func TestHTXPrivateStreamRejectsRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{}
	client := newTestHTXPrivateStreamClient(
		t, &htxWebSocketTestConnector{}, provider,
		time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC),
	)
	_, err := client.PrivateStream(
		StreamRequest{Subscriptions: []StreamSubscription{{
			Channel: StreamChannelAccounts, Mode: StreamModeBalanceOnly,
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

func TestHTXPrivateStreamRejectsPermissionBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{}
	client, err := NewStreamClient(StreamClientConfig{
		Connector: &htxWebSocketTestConnector{},
		Credentials: &credential.Descriptor{
			AccountID: "htx-main", Exchange: model.ExchangeHTX, SecretRef: "secret/htx-main",
			Permissions:           []credential.Permission{credential.PermissionTrade},
			AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	_, err = client.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelOrders, Symbol: "btcusdt",
	}}})
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("PrivateStream() error = %v, want authorization", err)
	}
	calls, _, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestHTXPrivateStreamValidation(t *testing.T) {
	t.Parallel()

	client := newTestHTXPrivateStreamClient(
		t, &htxWebSocketTestConnector{}, &recordingProvider{},
		time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC),
	)
	invalid := []StreamSubscription{
		{Channel: StreamChannelOrders, Symbol: ""},
		{Channel: StreamChannelOrders, Symbol: "btcusdt", Mode: 1},
		{Channel: StreamChannelClearing, Symbol: "btcusdt", Mode: 2},
		{Channel: StreamChannelAccounts, Symbol: "btcusdt"},
		{Channel: StreamChannelAccounts, Mode: 3},
		{Channel: StreamChannelTicker, Symbol: "btcusdt"},
	}
	for _, subscription := range invalid {
		_, err := client.PrivateStream(StreamRequest{Subscriptions: []StreamSubscription{subscription}})
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("PrivateStream(%+v) error = %v, want validation", subscription, err)
		}
	}
}

func newTestHTXPrivateStreamClient(
	t *testing.T,
	connector corestream.Connector,
	provider credential.Provider,
	now time.Time,
) *StreamClient {
	t.Helper()
	client, err := NewStreamClient(StreamClientConfig{
		Connector: connector,
		Credentials: &credential.Descriptor{
			AccountID: "htx-main", Exchange: model.ExchangeHTX, SecretRef: "secret/htx-main",
			Permissions:           []credential.Permission{credential.PermissionRead},
			AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a", "route-b"},
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		PrivateWebSocketURL:    "ws://stream.example.test/ws/v2",
		AllowInsecureWebSocket: true, Now: func() time.Time { return now },
		Backoff: func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client
}

func assertHTXPrivateAuthentication(
	t *testing.T,
	message corestream.Message,
	now time.Time,
) {
	t.Helper()
	if message.Type != corestream.MessageText {
		t.Fatalf("authentication frame type = %d, want text", message.Type)
	}
	var authentication htxPrivateAuthentication
	if err := json.Unmarshal(message.Data, &authentication); err != nil {
		t.Fatalf("decode authentication: %v", err)
	}
	if authentication.Action != "req" || authentication.Channel != "auth" ||
		authentication.Params.AuthType != "api" ||
		authentication.Params.AccessKey != "test-access" ||
		authentication.Params.SignatureMethod != "HmacSHA256" ||
		authentication.Params.SignatureVersion != "2.1" ||
		authentication.Params.Timestamp != now.UTC().Format("2006-01-02T15:04:05") {
		t.Fatalf("authentication = %+v", authentication)
	}
	canonical := "GET\nstream.example.test\n/ws/v2\n" +
		"accessKey=test-access&signatureMethod=HmacSHA256&signatureVersion=2.1&" +
		"timestamp=2026-08-25T03%3A04%3A05"
	mac := hmac.New(sha256.New, []byte("test-secret"))
	_, _ = mac.Write([]byte(canonical))
	wantSignature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if authentication.Params.Signature != wantSignature {
		t.Fatalf(
			"signature = %q, want %q", authentication.Params.Signature, wantSignature,
		)
	}
}

func assertHTXPrivateSubscriptions(
	t *testing.T,
	connection *htxWebSocketTestConnection,
) {
	t.Helper()
	first := decodeHTXPrivateCommand(t, waitForHTXWebSocketWrite(t, connection))
	second := decodeHTXPrivateCommand(t, waitForHTXWebSocketWrite(t, connection))
	if first.Action != "sub" || first.Channel != "accounts.update#2" ||
		second.Action != "sub" || second.Channel != "orders#btcusdt" {
		t.Fatalf("subscription commands = %+v, %+v", first, second)
	}
}

func decodeHTXPrivateCommand(t *testing.T, message corestream.Message) htxPrivateCommand {
	t.Helper()
	if message.Type != corestream.MessageText {
		t.Fatalf("command frame type = %d, want text", message.Type)
	}
	var command htxPrivateCommand
	if err := json.Unmarshal(message.Data, &command); err != nil {
		t.Fatalf("decode command: %v", err)
	}
	return command
}

func htxPrivateTextMessage(payload string) corestream.Message {
	return corestream.Message{Type: corestream.MessageText, Data: []byte(payload)}
}
