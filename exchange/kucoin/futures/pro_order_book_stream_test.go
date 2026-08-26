package futures

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

type proOrderBookSubscription struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Channel   string `json:"channel"`
	TradeType string `json:"tradeType"`
	Symbol    string `json:"symbol"`
	Depth     string `json:"depth"`
	RPIFilter int    `json:"rpiFilter"`
}

func TestDecodeProOrderBookMessage(t *testing.T) {
	t.Parallel()
	message, err := DecodeProOrderBookMessage(corestream.Message{Data: []byte(
		`{"T":"obu.FUTURES","dp":"increment@10ms","t":"snapshot","P":1725000000000000000,"d":{"O":100,"C":100,"M":1724999999999999999,"s":"XBTUSDTM","a":[["64001","2.5"]],"b":[["64000",1.25]]}}`,
	)})
	if err != nil {
		t.Fatalf("DecodeProOrderBookMessage() error = %v", err)
	}
	if message.Topic != proOrderBookTopic || message.Depth != proOrderBookDepth ||
		message.UpdateType != "snapshot" || message.PublishTime != 1_725_000_000_000_000_000 ||
		message.Data.SequenceStart != 100 || message.Data.SequenceEnd != 100 ||
		message.Data.Symbol != "XBTUSDTM" || len(message.Data.Bids) != 1 ||
		message.Data.Bids[0] != (BookLevel{Price: "64000", Size: "1.25"}) || len(message.Raw) == 0 {
		t.Fatalf("decoded message = %+v", message)
	}

	controls := []struct {
		name string
		raw  string
		want ProOrderBookMessage
	}{
		{name: "subscription", raw: `{"id":123,"result":true}`, want: ProOrderBookMessage{ID: "123", Result: "true"}},
		{name: "welcome", raw: `{"id":"welcome-1","type":"welcome"}`, want: ProOrderBookMessage{ID: "welcome-1", ControlType: "welcome"}},
		{name: "error", raw: `{"id":"1","type":"error","code":400100,"msg":"invalid symbol","d":"details"}`, want: ProOrderBookMessage{ID: "1", ControlType: "error", ErrorCode: "400100", ErrorMessage: "invalid symbol"}},
	}
	for _, control := range controls {
		t.Run(control.name, func(t *testing.T) {
			decoded, decodeErr := DecodeProOrderBookMessage(corestream.Message{Data: []byte(control.raw)})
			if decodeErr != nil {
				t.Fatalf("DecodeProOrderBookMessage() error = %v", decodeErr)
			}
			if decoded.ID != control.want.ID || decoded.Result != control.want.Result ||
				decoded.ControlType != control.want.ControlType ||
				decoded.ErrorCode != control.want.ErrorCode ||
				decoded.ErrorMessage != control.want.ErrorMessage {
				t.Fatalf("decoded control = %+v, want %+v", decoded, control.want)
			}
		})
	}
}

func TestDecodeProOrderBookMessageRejectsInvalidPayloads(t *testing.T) {
	t.Parallel()
	payloads := []string{
		``,
		`[]`,
		`{"T":"obu.SPOT","dp":"increment@10ms","t":"snapshot","P":1,"d":{"s":"XBTUSDTM"}}`,
		`{"T":"obu.FUTURES","dp":"increment@100ms","t":"snapshot","P":1,"d":{"s":"XBTUSDTM"}}`,
		`{"T":"obu.FUTURES","dp":"increment@10ms","t":"replace","P":1,"d":{"s":"XBTUSDTM"}}`,
		`{"T":"obu.FUTURES","dp":"increment@10ms","t":"snapshot","P":0,"d":{"s":"XBTUSDTM"}}`,
		`{"T":"obu.FUTURES","dp":"increment@10ms","t":"snapshot","P":1,"d":{"s":""}}`,
	}
	for _, payload := range payloads {
		if _, err := DecodeProOrderBookMessage(corestream.Message{Data: []byte(payload)}); err == nil {
			t.Fatalf("DecodeProOrderBookMessage(%q) error = nil", payload)
		}
	}
}

func TestProOrderBookStreamReconnectsOnSelectedRouteWithoutToken(t *testing.T) {
	first := newKuCoinWebSocketTestConnection()
	second := newKuCoinWebSocketTestConnection()
	connector := &kucoinWebSocketTestConnector{
		connections: []*kucoinWebSocketTestConnection{first, second},
	}
	tokenServer := newKuCoinWebSocketTokenServer(t)
	defer tokenServer.Close()
	client, sender := newTestKuCoinStreamClient(t, tokenServer.URL, connector)
	stream, err := client.ProOrderBookStream("xbtusdtm", trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("ProOrderBookStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- stream.Run(ctx, func(_ context.Context, message ProOrderBookMessage) error {
			if message.UpdateType == "snapshot" {
				cancel()
			}
			return nil
		})
	}()

	assertProOrderBookSubscription(t, waitForKuCoinWebSocketWrite(t, first), "XBTUSDTM")
	first.reads <- kucoinWebSocketReadResult{err: errors.New("connection lost")}
	assertProOrderBookSubscription(t, waitForKuCoinWebSocketWrite(t, second), "XBTUSDTM")
	second.reads <- kucoinWebSocketReadResult{message: corestream.Message{Data: []byte(
		`{"T":"obu.FUTURES","dp":"increment@10ms","t":"snapshot","P":2000,"d":{"O":200,"C":200,"M":1999,"s":"XBTUSDTM","a":[["101","1"]],"b":[["100","1"]]}}`,
	)}}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Pro Futures order book stream did not finish")
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) ||
		len(requests) != 2 || requests[0].Endpoint != "ws://pro-futures.example.test" ||
		requests[1].Endpoint != requests[0].Endpoint {
		t.Fatalf("Pro reconnect routes = %v, requests = %+v", routes, requests)
	}
	if senderRoutes := sender.snapshot(); len(senderRoutes) != 0 {
		t.Fatalf("token REST sender routes = %v, want none", senderRoutes)
	}
}

func TestProOrderBookStreamValidation(t *testing.T) {
	t.Parallel()
	client, _ := newTestKuCoinStreamClient(t, "http://rest.example.test", &kucoinWebSocketTestConnector{})
	if _, err := client.ProOrderBookStream("XBT-USDT"); err == nil {
		t.Fatal("ProOrderBookStream() invalid symbol error = nil")
	}
	if _, err := client.ProOrderBookStream("XBTUSDTM", trade.WithTimeout(time.Second)); err == nil || !strings.Contains(err.Error(), "Run context") {
		t.Fatalf("ProOrderBookStream() timeout error = %v", err)
	}
	stream, err := client.ProOrderBookStream("XBTUSDTM")
	if err != nil {
		t.Fatalf("ProOrderBookStream() error = %v", err)
	}
	if err := stream.Run(context.Background(), nil); err == nil ||
		!strings.Contains(err.Error(), "handler") {
		t.Fatalf("Run() handler error = %v", err)
	}
}

func TestValidateProPublicWebSocketURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw           string
		allowInsecure bool
		want          string
	}{
		{raw: "wss://x-push-futures.kucoin.com/", want: "wss://x-push-futures.kucoin.com"},
		{raw: "ws://localhost:9000/", allowInsecure: true, want: "ws://localhost:9000"},
	}
	for _, test := range tests {
		got, err := validateProPublicWebSocketURL(test.raw, test.allowInsecure)
		if err != nil || got != test.want {
			t.Fatalf("validateProPublicWebSocketURL(%q) = %q, %v, want %q", test.raw, got, err, test.want)
		}
	}
	for _, raw := range []string{
		"http://x-push-futures.kucoin.com",
		"ws://x-push-futures.kucoin.com",
		"wss://user@x-push-futures.kucoin.com",
		"wss://x-push-futures.kucoin.com?token=secret",
		"wss://x-push-futures.kucoin.com/#fragment",
	} {
		if _, err := validateProPublicWebSocketURL(raw, false); err == nil {
			t.Fatalf("validateProPublicWebSocketURL(%q) error = nil", raw)
		}
	}
}

func assertProOrderBookSubscription(t *testing.T, payload []byte, symbol string) {
	t.Helper()
	var subscription proOrderBookSubscription
	if err := json.Unmarshal(payload, &subscription); err != nil {
		t.Fatalf("decode Pro Futures order book subscription: %v", err)
	}
	if subscription.ID == "" || subscription.Action != "SUBSCRIBE" ||
		subscription.Channel != "obu" || subscription.TradeType != "FUTURES" ||
		subscription.Symbol != symbol || subscription.Depth != proOrderBookDepth ||
		subscription.RPIFilter != 0 {
		t.Fatalf("Pro Futures order book subscription = %+v", subscription)
	}
}
