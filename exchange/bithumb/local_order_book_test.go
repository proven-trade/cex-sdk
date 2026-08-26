package bithumb

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

func TestLocalOrderBookPublishesFullSnapshotsAcrossReconnect(t *testing.T) {
	first := newWebSocketTestConnection()
	second := newWebSocketTestConnection()
	connector := &websocketTestConnector{connections: []*websocketTestConnection{first, second}}
	client := newTestBithumbStreamClient(
		t, connector, nil, nil, sequentialStreamSource("ticket-1", "ticket-2"), nil,
	)
	public, err := client.PublicStream(
		StreamRequest{
			Types: []StreamDataType{{
				Type: "orderbook", Codes: []string{"KRW-BTC"}, Level: "1000",
			}},
			Format: StreamFormatDefault,
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
	assertBithumbSubscription(
		t, waitForBithumbWebSocketWrite(t, first), "ticket-1", "orderbook", "KRW-BTC",
		StreamFormatDefault,
	)
	sendBithumbLocalOrderBookMessage(first,
		`{"type":"orderbook","code":"KRW-BTC","total_ask_size":6.5,"total_bid_size":3.5,"orderbook_units":[{"ask_price":101,"bid_price":100,"ask_size":1.5,"bid_size":0.5},{"ask_price":102,"bid_price":99,"ask_size":2,"bid_size":1},{"ask_price":103,"bid_price":98,"ask_size":3,"bid_size":2}],"timestamp":1000000,"level":1000,"stream_type":"SNAPSHOT"}`,
	)
	view := waitBithumbLocalOrderBookView(t, views)
	if view.Market != "KRW-BTC" || view.Generation != 1 || view.SnapshotID != 1 ||
		view.Timestamp != 1000000 || view.StreamType != "SNAPSHOT" || view.Level != "1000" ||
		view.TotalAskSize != "6.5" || len(view.Bids) != 2 || len(view.Asks) != 2 ||
		view.Bids[0].Price != "100" || view.Asks[1].Price != "102" {
		t.Fatalf("first snapshot view = %+v", view)
	}

	sendBithumbLocalOrderBookMessage(first,
		`{"type":"orderbook","code":"KRW-BTC","total_ask_size":1,"total_bid_size":1,"orderbook_units":[{"ask_price":100.5,"bid_price":100.1,"ask_size":1,"bid_size":1}],"timestamp":1000001,"level":1000,"stream_type":"REALTIME"}`,
	)
	view = waitBithumbLocalOrderBookView(t, views)
	if view.Generation != 1 || view.SnapshotID != 2 || view.Timestamp != 1000001 ||
		view.StreamType != "REALTIME" || len(view.Bids) != 1 || view.Bids[0].Price != "100.1" {
		t.Fatalf("realtime snapshot view = %+v", view)
	}

	first.reads <- websocketReadResult{err: errors.New("connection lost")}
	assertBithumbSubscription(
		t, waitForBithumbWebSocketWrite(t, second), "ticket-2", "orderbook", "KRW-BTC",
		StreamFormatDefault,
	)
	sendBithumbLocalOrderBookMessage(second,
		`{"type":"orderbook","code":"KRW-BTC","total_ask_size":1,"total_bid_size":1,"orderbook_units":[{"ask_price":201,"bid_price":200,"ask_size":1,"bid_size":1}],"timestamp":2000000,"level":1000,"stream_type":"SNAPSHOT"}`,
	)
	view = waitBithumbLocalOrderBookView(t, views)
	if view.Generation != 2 || view.SnapshotID != 3 || view.Timestamp != 2000000 ||
		view.Bids[0].Price != "200" {
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

func TestLocalOrderBookRunValidatesRouteFormatAndSubscription(t *testing.T) {
	t.Parallel()
	connector := &websocketTestConnector{}
	client := newTestBithumbStreamClient(
		t, connector, nil, nil, sequentialStreamSource("ticket"), nil,
	)
	tests := []struct {
		name      string
		bookRoute transport.EgressRouteID
		dataType  StreamDataType
		format    StreamFormat
		want      string
	}{
		{name: "route mismatch", bookRoute: "route-b", dataType: StreamDataType{Type: "orderbook", Codes: []string{"KRW-BTC"}}, want: "routes must match"},
		{name: "simple format", bookRoute: "route-a", dataType: StreamDataType{Type: "orderbook", Codes: []string{"KRW-BTC"}}, format: StreamFormatSimple, want: "DEFAULT"},
		{name: "missing orderbook", bookRoute: "route-a", dataType: StreamDataType{Type: "ticker", Codes: []string{"KRW-BTC"}}, want: "required orderbook"},
		{name: "different market", bookRoute: "route-a", dataType: StreamDataType{Type: "orderbook", Codes: []string{"KRW-ETH"}}, want: "required orderbook"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			public, publicErr := client.PublicStream(StreamRequest{
				Types: []StreamDataType{test.dataType}, Format: test.format,
			})
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
		Type: "orderbook", Code: "KRW-BTC", Timestamp: 1000000,
		TotalAskSize: "2", TotalBidSize: "2", Level: "1", StreamType: "REALTIME",
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
		{name: "different market", mutate: func(event *StreamOrderBook) { event.Code = "KRW-ETH" }},
		{name: "invalid timestamp", mutate: func(event *StreamOrderBook) { event.Timestamp = 0 }},
		{name: "invalid stream type", mutate: func(event *StreamOrderBook) { event.StreamType = "" }},
		{name: "missing units", mutate: func(event *StreamOrderBook) { event.OrderBook = nil }},
		{name: "too many units", mutate: func(event *StreamOrderBook) { event.OrderBook = make([]OrderBookUnit, 16) }},
		{name: "negative total", mutate: func(event *StreamOrderBook) { event.TotalAskSize = "-1" }},
		{name: "zero level", mutate: func(event *StreamOrderBook) { event.Level = "0" }},
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

func TestNewLocalOrderBookValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config LocalOrderBookConfig
		want   error
	}{
		{name: "invalid market", config: LocalOrderBookConfig{Market: "btckrw", EgressRouteID: "route-a"}},
		{name: "missing route", config: LocalOrderBookConfig{Market: "KRW-BTC"}, want: trade.ErrMissingEgressRoute},
		{name: "invalid depth", config: LocalOrderBookConfig{Market: "KRW-BTC", EgressRouteID: "route-a", ViewDepth: 16}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewLocalOrderBook(test.config)
			if err == nil {
				t.Fatal("NewLocalOrderBook() error = nil")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("NewLocalOrderBook() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestLocalOrderBookStopsOnSubscriptionError(t *testing.T) {
	connection := newWebSocketTestConnection()
	connector := &websocketTestConnector{connections: []*websocketTestConnection{connection}}
	client := newTestBithumbStreamClient(
		t, connector, nil, nil, sequentialStreamSource("ticket"), nil,
	)
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
	_ = waitForBithumbWebSocketWrite(t, connection)
	sendBithumbLocalOrderBookMessage(
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

func sendBithumbLocalOrderBookMessage(connection *websocketTestConnection, payload string) {
	connection.reads <- websocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(payload),
	}}
}

func waitBithumbLocalOrderBookView(
	t *testing.T,
	views <-chan LocalOrderBookView,
) LocalOrderBookView {
	t.Helper()
	select {
	case view := <-views:
		return view
	case <-time.After(time.Second):
		t.Fatal("Bithumb local order book view did not arrive")
		return LocalOrderBookView{}
	}
}
