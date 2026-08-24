package coinone

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

func TestLocalOrderBookPublishesNewestSnapshotsAcrossReconnect(t *testing.T) {
	first := newCoinoneWebSocketTestConnection()
	second := newCoinoneWebSocketTestConnection()
	connector := &coinoneWebSocketTestConnector{
		connections: []*coinoneWebSocketTestConnection{first, second},
	}
	client := newTestCoinoneStreamClient(t, connector, nil, nil, nil)
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelOrderBook,
		Topics:  []StreamTopic{{QuoteCurrency: "KRW", TargetCurrency: "BTC"}},
		Format:  StreamFormatShort,
	}}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		QuoteCurrency: "krw", TargetCurrency: "btc", EgressRouteID: "route-b", ViewDepth: 2,
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
	assertCoinoneStreamCommand(
		t, waitForCoinoneWebSocketWrite(t, first), "SUBSCRIBE", StreamChannelOrderBook,
		StreamFormatShort, false, 1,
	)
	sendCoinoneLocalOrderBookMessage(first,
		`{"r":"DATA","c":"ORDERBOOK","d":{"qc":"KRW","tc":"BTC","t":1000,"i":"1000","a":[{"p":"103","q":"3"},{"p":"102","q":"2"},{"p":"101","q":"1"}],"b":[{"p":"100","q":"1"},{"p":"99","q":"2"},{"p":"98","q":"3"}]}}`,
	)
	view := waitCoinoneLocalOrderBookView(t, views)
	if view.QuoteCurrency != "KRW" || view.TargetCurrency != "BTC" ||
		view.Generation != 1 || view.SnapshotID != 1 || view.Timestamp != 1000 ||
		view.SourceID != "1000" || len(view.Bids) != 2 || len(view.Asks) != 2 ||
		view.Bids[0].Price != "100" || view.Asks[0].Price != "101" || view.Asks[1].Price != "102" {
		t.Fatalf("first snapshot view = %+v", view)
	}

	sendCoinoneLocalOrderBookMessage(first,
		`{"r":"DATA","c":"ORDERBOOK","d":{"qc":"KRW","tc":"BTC","t":1002,"i":"1002","a":[{"p":"101.5","q":"1"}],"b":[{"p":"100.5","q":"1"}]}}`,
	)
	view = waitCoinoneLocalOrderBookView(t, views)
	if view.Generation != 1 || view.SnapshotID != 2 || view.SourceID != "1002" ||
		len(view.Bids) != 1 || view.Bids[0].Price != "100.5" {
		t.Fatalf("newer snapshot view = %+v", view)
	}
	sendCoinoneLocalOrderBookMessage(first,
		`{"r":"DATA","c":"ORDERBOOK","d":{"qc":"KRW","tc":"BTC","t":1001,"i":"1001","a":[{"p":"91","q":"1"}],"b":[{"p":"90","q":"1"}]}}`,
	)
	assertNoCoinoneLocalOrderBookView(t, views)

	first.reads <- coinoneWebSocketReadResult{err: errors.New("connection lost")}
	assertCoinoneStreamCommand(
		t, waitForCoinoneWebSocketWrite(t, second), "SUBSCRIBE", StreamChannelOrderBook,
		StreamFormatShort, false, 1,
	)
	sendCoinoneLocalOrderBookMessage(second,
		`{"r":"DATA","c":"ORDERBOOK","d":{"qc":"KRW","tc":"BTC","t":2000,"i":"5","a":[{"p":"201","q":"1"}],"b":[{"p":"200","q":"1"}]}}`,
	)
	view = waitCoinoneLocalOrderBookView(t, views)
	if view.Generation != 2 || view.SnapshotID != 3 || view.SourceID != "5" ||
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

func TestLocalOrderBookRunValidatesRouteAndSubscription(t *testing.T) {
	t.Parallel()
	connector := &coinoneWebSocketTestConnector{}
	client := newTestCoinoneStreamClient(t, connector, nil, nil, nil)
	tests := []struct {
		name      string
		bookRoute transport.EgressRouteID
		channel   StreamChannel
		topic     StreamTopic
		want      string
	}{
		{name: "route mismatch", bookRoute: "route-b", channel: StreamChannelOrderBook, topic: StreamTopic{QuoteCurrency: "KRW", TargetCurrency: "BTC"}, want: "routes must match"},
		{name: "missing order book", bookRoute: "route-a", channel: StreamChannelTicker, topic: StreamTopic{QuoteCurrency: "KRW", TargetCurrency: "BTC"}, want: "required ORDERBOOK"},
		{name: "different quote", bookRoute: "route-a", channel: StreamChannelOrderBook, topic: StreamTopic{QuoteCurrency: "USDT", TargetCurrency: "BTC"}, want: "required ORDERBOOK"},
		{name: "different target", bookRoute: "route-a", channel: StreamChannelOrderBook, topic: StreamTopic{QuoteCurrency: "KRW", TargetCurrency: "ETH"}, want: "required ORDERBOOK"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			public, publicErr := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{{
				Channel: test.channel, Topics: []StreamTopic{test.topic},
			}}})
			if publicErr != nil {
				t.Fatalf("PublicStream() error = %v", publicErr)
			}
			book, bookErr := NewLocalOrderBook(LocalOrderBookConfig{
				QuoteCurrency: "KRW", TargetCurrency: "BTC", EgressRouteID: test.bookRoute,
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
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		QuoteCurrency: "KRW", TargetCurrency: "BTC", EgressRouteID: "route-a",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	valid := StreamOrderBook{
		QuoteCurrency: "KRW", TargetCurrency: "BTC", Timestamp: 1000, ID: "1000001",
		Asks: []OrderBookLevel{{Price: "102", Quantity: "2"}, {Price: "101", Quantity: "1"}},
		Bids: []OrderBookLevel{{Price: "100", Quantity: "1"}, {Price: "99", Quantity: "2"}},
	}
	tests := []struct {
		name   string
		mutate func(*StreamOrderBook)
	}{
		{name: "different quote", mutate: func(event *StreamOrderBook) { event.QuoteCurrency = "USDT" }},
		{name: "different target", mutate: func(event *StreamOrderBook) { event.TargetCurrency = "ETH" }},
		{name: "invalid timestamp", mutate: func(event *StreamOrderBook) { event.Timestamp = 0 }},
		{name: "invalid source ID", mutate: func(event *StreamOrderBook) { event.ID = "book-1" }},
		{name: "empty book", mutate: func(event *StreamOrderBook) { event.Asks = nil; event.Bids = nil }},
		{name: "too many asks", mutate: func(event *StreamOrderBook) { event.Asks = make([]OrderBookLevel, 17) }},
		{name: "zero price", mutate: func(event *StreamOrderBook) { event.Asks[0].Price = "0" }},
		{name: "negative quantity", mutate: func(event *StreamOrderBook) { event.Bids[0].Quantity = "-1" }},
		{name: "unsorted asks", mutate: func(event *StreamOrderBook) { event.Asks[1].Price = "103" }},
		{name: "unsorted bids", mutate: func(event *StreamOrderBook) { event.Bids[1].Price = "101" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := valid
			event.Asks = slices.Clone(valid.Asks)
			event.Bids = slices.Clone(valid.Bids)
			test.mutate(&event)
			if _, err := validateLocalOrderBookSnapshot(book, event); err == nil {
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
		{name: "invalid pair", config: LocalOrderBookConfig{QuoteCurrency: "KRW", TargetCurrency: "KRW", EgressRouteID: "route-a"}},
		{name: "missing route", config: LocalOrderBookConfig{QuoteCurrency: "KRW", TargetCurrency: "BTC"}, want: trade.ErrMissingEgressRoute},
		{name: "invalid depth", config: LocalOrderBookConfig{QuoteCurrency: "KRW", TargetCurrency: "BTC", EgressRouteID: "route-a", ViewDepth: 17}},
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
	connection := newCoinoneWebSocketTestConnection()
	connector := &coinoneWebSocketTestConnector{
		connections: []*coinoneWebSocketTestConnection{connection},
	}
	client := newTestCoinoneStreamClient(t, connector, nil, nil, nil)
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelOrderBook,
		Topics:  []StreamTopic{{QuoteCurrency: "KRW", TargetCurrency: "BTC"}},
	}}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		QuoteCurrency: "KRW", TargetCurrency: "BTC", EgressRouteID: "route-a",
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
	_ = waitForCoinoneWebSocketWrite(t, connection)
	sendCoinoneLocalOrderBookMessage(
		connection,
		`{"response_type":"ERROR","error_code":160012,"message":"Invalid Topic"}`,
	)
	select {
	case runErr := <-done:
		if runErr == nil || !strings.Contains(runErr.Error(), "160012: Invalid Topic") {
			t.Fatalf("Run() error = %v, want subscription error", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("subscription error did not stop local order book")
	}
}

func sendCoinoneLocalOrderBookMessage(
	connection *coinoneWebSocketTestConnection,
	payload string,
) {
	connection.reads <- coinoneWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(payload),
	}}
}

func waitCoinoneLocalOrderBookView(
	t *testing.T,
	views <-chan LocalOrderBookView,
) LocalOrderBookView {
	t.Helper()
	select {
	case view := <-views:
		return view
	case <-time.After(time.Second):
		t.Fatal("Coinone local order book view did not arrive")
		return LocalOrderBookView{}
	}
}

func assertNoCoinoneLocalOrderBookView(t *testing.T, views <-chan LocalOrderBookView) {
	t.Helper()
	select {
	case view := <-views:
		t.Fatalf("unexpected Coinone local order book view = %+v", view)
	case <-time.After(50 * time.Millisecond):
	}
}
