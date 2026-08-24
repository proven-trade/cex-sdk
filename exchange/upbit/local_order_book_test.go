package upbit

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

func TestStreamOrderBookDecodesSimpleListPayload(t *testing.T) {
	t.Parallel()
	message, err := DecodeStreamMessage(corestream.Message{Data: []byte(
		`[{"ty":"orderbook","cd":"KRW-BTC","tms":1000,"tas":3.5,"tbs":2.5,"obu":[{"ap":101,"bp":100,"as":1.5,"bs":0.5}],"lv":10000,"st":"SNAPSHOT"}]`,
	)})
	if err != nil {
		t.Fatalf("DecodeStreamMessage() error = %v", err)
	}
	var event StreamOrderBook
	if err := message.Decode(&event); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if event.Type != "orderbook" || event.Code != "KRW-BTC" || event.Timestamp != 1000 ||
		event.TotalAskSize != "3.5" || event.TotalBidSize != "2.5" || event.Level != "10000" ||
		event.StreamType != "SNAPSHOT" || len(event.OrderBook) != 1 ||
		event.OrderBook[0].AskPrice != "101" || event.OrderBook[0].BidSize != "0.5" {
		t.Fatalf("decoded event = %+v", event)
	}
}

func TestLocalOrderBookPublishesFullSnapshotsAcrossReconnect(t *testing.T) {
	first := newWebSocketTestConnection()
	second := newWebSocketTestConnection()
	connector := &websocketTestConnector{connections: []*websocketTestConnection{first, second}}
	client := newTestUpbitStreamClient(
		t, connector, nil, nil, sequentialSource("ticket-1", "ticket-2"), nil,
	)
	public, err := client.PublicStream(
		StreamRequest{
			Types: []StreamDataType{{
				Type: "orderbook", Codes: []string{"krw-btc.5"}, Level: "10000",
			}},
			Format: StreamFormatSimpleList,
		},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Market: "krw-btc", EgressRouteID: "route-b", ViewDepth: 2,
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	views := make(chan LocalOrderBookView, 4)
	done := make(chan error, 1)
	go func() {
		done <- book.Run(ctx, public, func(_ context.Context, view LocalOrderBookView) error {
			views <- view
			return nil
		})
	}()
	assertSubscription(
		t, waitForWebSocketWrite(t, first), "ticket-1", "orderbook", "KRW-BTC.5",
		StreamFormatSimpleList,
	)
	sendUpbitLocalOrderBookMessage(first,
		`[{"ty":"orderbook","cd":"KRW-BTC","tms":1000,"tas":3.5,"tbs":2.5,"obu":[{"ap":101,"bp":100,"as":1.5,"bs":0.5},{"ap":102,"bp":99,"as":2,"bs":2},{"ap":103,"bp":98,"as":3,"bs":3}],"lv":10000,"st":"SNAPSHOT"}]`,
	)
	view := waitUpbitLocalOrderBookView(t, views)
	if view.Market != "KRW-BTC" || view.Generation != 1 || view.SnapshotID != 1 ||
		view.Timestamp != 1000 || view.StreamType != "SNAPSHOT" || view.Level != "10000" ||
		len(view.Bids) != 2 || len(view.Asks) != 2 || view.Bids[0].Price != "100" ||
		view.Asks[1].Price != "102" {
		t.Fatalf("first snapshot view = %+v", view)
	}

	sendUpbitLocalOrderBookMessage(first,
		`[{"ty":"orderbook","cd":"KRW-BTC","tms":1001,"tas":1,"tbs":1,"obu":[{"ap":100.5,"bp":100.1,"as":1,"bs":1}],"lv":10000,"st":"REALTIME"}]`,
	)
	view = waitUpbitLocalOrderBookView(t, views)
	if view.Generation != 1 || view.SnapshotID != 2 || view.Timestamp != 1001 ||
		view.StreamType != "REALTIME" || len(view.Bids) != 1 {
		t.Fatalf("realtime snapshot view = %+v", view)
	}

	first.reads <- websocketReadResult{err: errors.New("connection lost")}
	assertSubscription(
		t, waitForWebSocketWrite(t, second), "ticket-2", "orderbook", "KRW-BTC.5",
		StreamFormatSimpleList,
	)
	sendUpbitLocalOrderBookMessage(second,
		`[{"ty":"orderbook","cd":"KRW-BTC","tms":2000,"tas":1,"tbs":1,"obu":[{"ap":201,"bp":200,"as":1,"bs":1}],"lv":10000,"st":"SNAPSHOT"}]`,
	)
	view = waitUpbitLocalOrderBookView(t, views)
	if view.Generation != 2 || view.SnapshotID != 3 || view.Timestamp != 2000 || view.Bids[0].Price != "200" {
		t.Fatalf("reconnected snapshot view = %+v", view)
	}

	cancel()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("local order book Run() did not finish")
	}
	if err := public.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	routes, _ := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) {
		t.Fatalf("reconnection routes = %v", routes)
	}
}

func TestLocalOrderBookRunValidatesRouteAndSubscription(t *testing.T) {
	t.Parallel()
	connector := &websocketTestConnector{}
	client := newTestUpbitStreamClient(t, connector, nil, nil, sequentialSource("ticket"), nil)
	tests := []struct {
		name      string
		bookRoute transport.EgressRouteID
		dataType  StreamDataType
		want      string
	}{
		{name: "route mismatch", bookRoute: "route-b", dataType: StreamDataType{Type: "orderbook", Codes: []string{"KRW-BTC"}}, want: "routes must match"},
		{name: "missing orderbook", bookRoute: "route-a", dataType: StreamDataType{Type: "ticker", Codes: []string{"KRW-BTC"}}, want: "required orderbook"},
		{name: "different market", bookRoute: "route-a", dataType: StreamDataType{Type: "orderbook", Codes: []string{"KRW-ETH"}}, want: "required orderbook"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			public, publicErr := client.PublicStream(StreamRequest{Types: []StreamDataType{test.dataType}})
			if publicErr != nil {
				t.Fatalf("PublicStream() error = %v", publicErr)
			}
			book, bookErr := NewLocalOrderBook(LocalOrderBookConfig{
				Market: "KRW-BTC", EgressRouteID: test.bookRoute,
			})
			if bookErr != nil {
				t.Fatalf("NewLocalOrderBook() error = %v", bookErr)
			}
			runErr := book.Run(context.Background(), public, func(context.Context, LocalOrderBookView) error {
				return nil
			})
			if runErr == nil || !strings.Contains(runErr.Error(), test.want) {
				t.Fatalf("Run() error = %v, want text %q", runErr, test.want)
			}
		})
	}
	routes, requests := connector.snapshot()
	if len(routes) != 0 || len(requests) != 0 {
		t.Fatalf("connector was called during validation: routes=%v requests=%v", routes, requests)
	}
}

func TestValidateLocalOrderBookSnapshotRejectsInvalidState(t *testing.T) {
	t.Parallel()
	valid := StreamOrderBook{
		Type: "orderbook", Code: "KRW-BTC", Timestamp: 1000,
		TotalAskSize: "2", TotalBidSize: "2", StreamType: "REALTIME",
		OrderBook: []OrderBookUnit{
			{AskPrice: "101", BidPrice: "100", AskSize: "1", BidSize: "1"},
			{AskPrice: "102", BidPrice: "99", AskSize: "1", BidSize: "1"},
		},
	}
	tests := []struct {
		name   string
		mutate func(*StreamOrderBook)
	}{
		{name: "invalid type", mutate: func(event *StreamOrderBook) { event.Type = "ticker" }},
		{name: "invalid timestamp", mutate: func(event *StreamOrderBook) { event.Timestamp = 0 }},
		{name: "invalid stream type", mutate: func(event *StreamOrderBook) { event.StreamType = "" }},
		{name: "missing units", mutate: func(event *StreamOrderBook) { event.OrderBook = nil }},
		{name: "negative total", mutate: func(event *StreamOrderBook) { event.TotalAskSize = "-1" }},
		{name: "zero price", mutate: func(event *StreamOrderBook) { event.OrderBook[0].AskPrice = "0" }},
		{name: "negative size", mutate: func(event *StreamOrderBook) { event.OrderBook[0].BidSize = "-1" }},
		{name: "unsorted asks", mutate: func(event *StreamOrderBook) { event.OrderBook[1].AskPrice = "100" }},
		{name: "unsorted bids", mutate: func(event *StreamOrderBook) { event.OrderBook[1].BidPrice = "101" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := valid
			event.OrderBook = slices.Clone(valid.OrderBook)
			test.mutate(&event)
			if err := validateLocalOrderBookSnapshot("KRW-BTC", event); err == nil {
				t.Fatal("validateLocalOrderBookSnapshot() error = nil")
			}
		})
	}
}

func TestUpbitCurrentOrderBookSubscriptionOptions(t *testing.T) {
	t.Parallel()
	validated, err := validateStreamRequest(StreamRequest{
		Types: []StreamDataType{{
			Type: "orderbook", Codes: []string{"krw-btc.5"}, Level: "10000",
		}},
		Format: StreamFormatSimpleList,
	}, false)
	if err != nil {
		t.Fatalf("validateStreamRequest() error = %v", err)
	}
	if validated.Format != StreamFormatSimpleList ||
		!slices.Equal(validated.Types[0].Codes, []string{"KRW-BTC.5"}) ||
		validated.Types[0].Level != "10000" {
		t.Fatalf("validated request = %+v", validated)
	}
	if _, err := validateStreamRequest(StreamRequest{
		Types: []StreamDataType{{Type: "orderbook", Codes: []string{"KRW-BTC.3"}}},
	}, false); err == nil {
		t.Fatal("unsupported order book unit error = nil")
	}
	if _, err := validateStreamRequest(StreamRequest{
		Types: []StreamDataType{{Type: "ticker", Codes: []string{"KRW-BTC"}, Level: "10000"}},
	}, false); err == nil {
		t.Fatal("ticker level error = nil")
	}
	if _, err := NewLocalOrderBook(LocalOrderBookConfig{Market: "KRW-BTC"}); !errors.Is(err, trade.ErrMissingEgressRoute) {
		t.Fatalf("NewLocalOrderBook() error = %v, want missing route", err)
	}
}

func TestLocalOrderBookStopsOnSubscriptionError(t *testing.T) {
	connection := newWebSocketTestConnection()
	connector := &websocketTestConnector{connections: []*websocketTestConnection{connection}}
	client := newTestUpbitStreamClient(t, connector, nil, nil, sequentialSource("ticket"), nil)
	public, err := client.PublicStream(StreamRequest{
		Types: []StreamDataType{{Type: "orderbook", Codes: []string{"KRW-BTC"}}},
	})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Market: "KRW-BTC", EgressRouteID: "route-a",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- book.Run(ctx, public, func(context.Context, LocalOrderBookView) error { return nil })
	}()
	_ = waitForWebSocketWrite(t, connection)
	sendUpbitLocalOrderBookMessage(
		connection,
		`{"error":{"name":"INVALID_PARAM","message":"invalid orderbook subscription"}}`,
	)
	select {
	case runErr := <-done:
		if runErr == nil || !strings.Contains(runErr.Error(), "INVALID_PARAM: invalid orderbook subscription") {
			t.Fatalf("Run() error = %v, want subscription error", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("subscription error did not stop local order book")
	}
}

func sendUpbitLocalOrderBookMessage(connection *websocketTestConnection, payload string) {
	connection.reads <- websocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(payload),
	}}
}

func waitUpbitLocalOrderBookView(t *testing.T, views <-chan LocalOrderBookView) LocalOrderBookView {
	t.Helper()
	select {
	case view := <-views:
		return view
	case <-time.After(time.Second):
		t.Fatal("Upbit local order book view did not arrive")
		return LocalOrderBookView{}
	}
}

func TestStreamDataTypeLevelJSON(t *testing.T) {
	t.Parallel()
	payload, err := json.Marshal(StreamDataType{
		Type: "orderbook", Codes: []string{"KRW-BTC"}, Level: "10000",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !strings.Contains(string(payload), `"level":10000`) {
		t.Fatalf("StreamDataType JSON = %s", payload)
	}
}
