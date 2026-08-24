package korbit

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

func TestLocalOrderBookPublishesFullSnapshotsAcrossReconnect(t *testing.T) {
	first := newKorbitWebSocketTestConnection()
	second := newKorbitWebSocketTestConnection()
	connector := &korbitWebSocketTestConnector{
		connections: []*korbitWebSocketTestConnection{first, second},
	}
	client := newTestKorbitStreamClient(t, connector, nil, nil)
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelOrderBook, Symbols: []string{"btc_krw"}, Level: "1000",
	}}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Symbol: "BTC_KRW", Level: "1000", EgressRouteID: "route-b", ViewDepth: 2,
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
	assertKorbitSubscriptions(t, waitForKorbitWebSocketWrite(t, first), "subscribe", 1)
	sendKorbitLocalOrderBookMessage(first,
		`{"type":"orderbook","timestamp":1010,"symbol":"btc_krw","snapshot":true,"data":{"timestamp":1000,"asks":[{"price":"101","qty":"1","amt":"101"},{"price":"102","qty":"2","amt":"204"},{"price":"103","qty":"3","amt":"309"}],"bids":[{"price":"100","qty":"1","amt":"100"},{"price":"99","qty":"2","amt":"198"},{"price":"98","qty":"3","amt":"294"}]}}`,
	)
	view := waitKorbitLocalOrderBookView(t, views)
	if view.Symbol != "btc_krw" || view.Level != "1000" || view.Generation != 1 ||
		view.SnapshotID != 1 || view.EnvelopeTimestamp != 1010 || view.Timestamp != 1000 ||
		!view.Snapshot || len(view.Bids) != 2 || len(view.Asks) != 2 ||
		view.Bids[0].Price != "100" || view.Asks[1].Price != "102" {
		t.Fatalf("first snapshot view = %+v", view)
	}

	sendKorbitLocalOrderBookMessage(first,
		`{"type":"orderbook","timestamp":1011,"symbol":"btc_krw","snapshot":false,"data":{"timestamp":1001,"asks":[{"price":"100.5","qty":"1"}],"bids":[{"price":"100.1","qty":"1"}]}}`,
	)
	view = waitKorbitLocalOrderBookView(t, views)
	if view.Generation != 1 || view.SnapshotID != 2 || view.Timestamp != 1001 ||
		view.Snapshot || len(view.Bids) != 1 || view.Bids[0].Price != "100.1" {
		t.Fatalf("realtime snapshot view = %+v", view)
	}
	sendKorbitLocalOrderBookMessage(first,
		`{"type":"orderbook","timestamp":1012,"symbol":"btc_krw","snapshot":false,"data":{"timestamp":999,"asks":[{"price":"91","qty":"1"}],"bids":[{"price":"90","qty":"1"}]}}`,
	)
	assertNoKorbitLocalOrderBookView(t, views)

	first.reads <- korbitWebSocketReadResult{err: errors.New("connection lost")}
	assertKorbitSubscriptions(t, waitForKorbitWebSocketWrite(t, second), "subscribe", 1)
	sendKorbitLocalOrderBookMessage(second,
		`{"type":"orderbook","timestamp":2010,"symbol":"btc_krw","snapshot":true,"data":{"timestamp":500,"asks":[{"price":"201","qty":"1"}],"bids":[{"price":"200","qty":"1"}]}}`,
	)
	view = waitKorbitLocalOrderBookView(t, views)
	if view.Generation != 2 || view.SnapshotID != 3 || view.Timestamp != 500 ||
		view.Bids[0].Price != "200" {
		t.Fatalf("reconnected snapshot view = %+v", view)
	}

	cancel()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
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
	connector := &korbitWebSocketTestConnector{}
	client := newTestKorbitStreamClient(t, connector, nil, nil)
	tests := []struct {
		name          string
		bookRoute     transport.EgressRouteID
		bookLevel     string
		subscriptions []StreamSubscription
		want          string
	}{
		{name: "route mismatch", bookRoute: "route-b", subscriptions: []StreamSubscription{{Channel: StreamChannelOrderBook, Symbols: []string{"btc_krw"}}}, want: "routes must match"},
		{name: "missing order book", bookRoute: "route-a", subscriptions: []StreamSubscription{{Channel: StreamChannelTicker, Symbols: []string{"btc_krw"}}}, want: "exactly one"},
		{name: "different symbol", bookRoute: "route-a", subscriptions: []StreamSubscription{{Channel: StreamChannelOrderBook, Symbols: []string{"eth_krw"}}}, want: "exactly one"},
		{name: "different level", bookRoute: "route-a", bookLevel: "1000", subscriptions: []StreamSubscription{{Channel: StreamChannelOrderBook, Symbols: []string{"btc_krw"}, Level: "10000"}}, want: "exactly one"},
		{name: "ambiguous levels", bookRoute: "route-a", subscriptions: []StreamSubscription{{Channel: StreamChannelOrderBook, Symbols: []string{"btc_krw"}}, {Channel: StreamChannelOrderBook, Symbols: []string{"btc_krw"}, Level: "1000"}}, want: "exactly one"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			public, publicErr := client.PublicStream(StreamRequest{Subscriptions: test.subscriptions})
			if publicErr != nil {
				t.Fatalf("PublicStream() error = %v", publicErr)
			}
			book, bookErr := NewLocalOrderBook(LocalOrderBookConfig{
				Symbol: "btc_krw", Level: test.bookLevel, EgressRouteID: test.bookRoute,
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
		Timestamp: 1000,
		Asks:      []OrderBookLevel{{Price: "101", Qty: "1"}, {Price: "102", Qty: "2"}},
		Bids:      []OrderBookLevel{{Price: "100", Qty: "1"}, {Price: "99", Qty: "2"}},
	}
	tests := []struct {
		name              string
		envelopeTimestamp int64
		mutate            func(*StreamOrderBook)
	}{
		{name: "invalid envelope timestamp", envelopeTimestamp: 0, mutate: func(*StreamOrderBook) {}},
		{name: "invalid data timestamp", envelopeTimestamp: 1001, mutate: func(event *StreamOrderBook) { event.Timestamp = 0 }},
		{name: "empty book", envelopeTimestamp: 1001, mutate: func(event *StreamOrderBook) { event.Asks = nil; event.Bids = nil }},
		{name: "too many asks", envelopeTimestamp: 1001, mutate: func(event *StreamOrderBook) { event.Asks = make([]OrderBookLevel, 31) }},
		{name: "zero price", envelopeTimestamp: 1001, mutate: func(event *StreamOrderBook) { event.Asks[0].Price = "0" }},
		{name: "negative quantity", envelopeTimestamp: 1001, mutate: func(event *StreamOrderBook) { event.Bids[0].Qty = "-1" }},
		{name: "invalid amount", envelopeTimestamp: 1001, mutate: func(event *StreamOrderBook) { event.Asks[0].Amount = "invalid" }},
		{name: "unsorted asks", envelopeTimestamp: 1001, mutate: func(event *StreamOrderBook) { event.Asks[1].Price = "100" }},
		{name: "unsorted bids", envelopeTimestamp: 1001, mutate: func(event *StreamOrderBook) { event.Bids[1].Price = "101" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := valid
			event.Asks = slices.Clone(valid.Asks)
			event.Bids = slices.Clone(valid.Bids)
			test.mutate(&event)
			if err := validateLocalOrderBookSnapshot(test.envelopeTimestamp, event); err == nil {
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
		{name: "invalid symbol", config: LocalOrderBookConfig{Symbol: "btc", EgressRouteID: "route-a"}},
		{name: "invalid level", config: LocalOrderBookConfig{Symbol: "btc_krw", Level: "0", EgressRouteID: "route-a"}},
		{name: "missing route", config: LocalOrderBookConfig{Symbol: "btc_krw"}, want: trade.ErrMissingEgressRoute},
		{name: "invalid depth", config: LocalOrderBookConfig{Symbol: "btc_krw", EgressRouteID: "route-a", ViewDepth: 31}},
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
	connection := newKorbitWebSocketTestConnection()
	connector := &korbitWebSocketTestConnector{
		connections: []*korbitWebSocketTestConnection{connection},
	}
	client := newTestKorbitStreamClient(t, connector, nil, nil)
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelOrderBook, Symbols: []string{"btc_krw"},
	}}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Symbol: "btc_krw", EgressRouteID: "route-a",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- book.Run(ctx, public, func(context.Context, LocalOrderBookView) error { return nil })
	}()
	_ = waitForKorbitWebSocketWrite(t, connection)
	sendKorbitLocalOrderBookMessage(
		connection,
		`{"requestId":1,"status":"fail","code":"INVALID_SYMBOL","message":"bad symbol"}`,
	)
	select {
	case runErr := <-done:
		if runErr == nil || !strings.Contains(runErr.Error(), "INVALID_SYMBOL: bad symbol") {
			t.Fatalf("Run() error = %v, want subscription error", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscription error did not stop local order book")
	}
}

func sendKorbitLocalOrderBookMessage(
	connection *korbitWebSocketTestConnection,
	payload string,
) {
	connection.reads <- korbitWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(payload),
	}}
}

func waitKorbitLocalOrderBookView(
	t *testing.T,
	views <-chan LocalOrderBookView,
) LocalOrderBookView {
	t.Helper()
	select {
	case view := <-views:
		return view
	case <-time.After(2 * time.Second):
		t.Fatal("Korbit local order book view did not arrive")
		return LocalOrderBookView{}
	}
}

func assertNoKorbitLocalOrderBookView(t *testing.T, views <-chan LocalOrderBookView) {
	t.Helper()
	select {
	case view := <-views:
		t.Fatalf("unexpected Korbit local order book view = %+v", view)
	case <-time.After(50 * time.Millisecond):
	}
}
