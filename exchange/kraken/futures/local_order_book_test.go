package futures

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

func newTestLocalOrderBook(t *testing.T, viewDepth int) *LocalOrderBook {
	t.Helper()
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		ProductID: "PI_XBTUSD", ViewDepth: viewDepth, EgressRouteID: "route-b",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	return book
}

func localBookLevel(price string, quantity string) StreamBookLevel {
	return StreamBookLevel{Price: Decimal(price), Quantity: Decimal(quantity)}
}

func localBookSnapshot(sequence uint64, timestamp int64) StreamBookSnapshot {
	return StreamBookSnapshot{
		Feed: "book_snapshot", ProductID: "PI_XBTUSD",
		Sequence: sequence, Timestamp: timestamp,
	}
}

func localBookUpdate(sequence uint64, timestamp int64, side string, price string, quantity string) StreamBookUpdate {
	return StreamBookUpdate{
		Feed: string(PublicFeedBook), ProductID: "PI_XBTUSD",
		Sequence: sequence, Timestamp: timestamp, Side: side,
		Price: Decimal(price), Quantity: Decimal(quantity),
	}
}

func TestLocalOrderBookSnapshotAndUpdates(t *testing.T) {
	book := newTestLocalOrderBook(t, 2)
	processor := &localOrderBookProcessor{book: book}
	snapshot := localBookSnapshot(10, 1000)
	snapshot.Bids = []StreamBookLevel{
		localBookLevel("100.0", "1.0"),
		localBookLevel("99.0", "2.0"),
		localBookLevel("98.0", "3.0"),
	}
	snapshot.Asks = []StreamBookLevel{
		localBookLevel("101.0", "1.0"),
		localBookLevel("102.0", "2.0"),
	}
	view, reconnect, err := processor.processSnapshot(1, snapshot)
	if err != nil || reconnect || view == nil {
		t.Fatalf("snapshot process = %+v, %v, %v", view, reconnect, err)
	}
	if view.ProductID != "PI_XBTUSD" || view.ViewDepth != 2 || view.Generation != 1 ||
		view.SynchronizationID != 1 || view.GapCount != 0 || view.Sequence != 10 ||
		view.Timestamp != 1000 {
		t.Fatalf("snapshot metadata = %+v", view)
	}
	wantBids := []StreamBookLevel{
		localBookLevel("100.0", "1.0"),
		localBookLevel("99.0", "2.0"),
	}
	if !slices.Equal(view.Bids, wantBids) {
		t.Fatalf("snapshot bids = %v, want %v", view.Bids, wantBids)
	}

	updates := []StreamBookUpdate{
		localBookUpdate(11, 1001, "buy", "100.00", "0"),
		localBookUpdate(12, 1002, "buy", "100.5", "4.0"),
		localBookUpdate(13, 1003, "sell", "101.0", "5.0"),
	}
	for _, update := range updates {
		view, reconnect, err = processor.processUpdate(1, update)
		if err != nil || reconnect || view == nil {
			t.Fatalf("update process = %+v, %v, %v", view, reconnect, err)
		}
	}
	wantBids = []StreamBookLevel{
		localBookLevel("100.5", "4.0"),
		localBookLevel("99.0", "2.0"),
	}
	wantAsks := []StreamBookLevel{
		localBookLevel("101.0", "5.0"),
		localBookLevel("102.0", "2.0"),
	}
	if view.Sequence != 13 || view.Timestamp != 1003 || !slices.Equal(view.Bids, wantBids) ||
		!slices.Equal(view.Asks, wantAsks) {
		t.Fatalf("updated view = %+v", view)
	}

	stale := localBookUpdate(12, 1004, "buy", "100.5", "9")
	view, reconnect, err = processor.processUpdate(1, stale)
	if err != nil || reconnect || view != nil ||
		processor.state.bids["201/2"].quantityText != "4.0" {
		t.Fatalf("stale update = %+v, %v, %v", view, reconnect, err)
	}
}

func TestLocalOrderBookSnapshotAlwaysReplacesState(t *testing.T) {
	book := newTestLocalOrderBook(t, 20)
	processor := &localOrderBookProcessor{book: book}
	first := localBookSnapshot(10, 1000)
	first.Bids = []StreamBookLevel{localBookLevel("100", "1")}
	first.Asks = []StreamBookLevel{localBookLevel("101", "1")}
	if _, _, err := processor.processSnapshot(1, first); err != nil {
		t.Fatalf("first snapshot error = %v", err)
	}
	second := localBookSnapshot(3, 2000)
	second.Bids = []StreamBookLevel{localBookLevel("90", "2")}
	second.Asks = []StreamBookLevel{localBookLevel("91", "3")}
	view, reconnect, err := processor.processSnapshot(1, second)
	if err != nil || reconnect || view == nil || view.Sequence != 3 ||
		view.SynchronizationID != 2 || len(view.Bids) != 1 || view.Bids[0].Price != "90" ||
		len(view.Asks) != 1 || view.Asks[0].Price != "91" {
		t.Fatalf("replacement snapshot = %+v, %v, %v", view, reconnect, err)
	}
}

func TestLocalOrderBookDetectsSequenceGap(t *testing.T) {
	book := newTestLocalOrderBook(t, 20)
	processor := &localOrderBookProcessor{book: book}
	if _, _, err := processor.processSnapshot(1, localBookSnapshot(10, 1000)); err != nil {
		t.Fatalf("snapshot error = %v", err)
	}
	view, reconnect, err := processor.processUpdate(
		1, localBookUpdate(12, 1001, "buy", "100", "1"),
	)
	if err != nil || !reconnect || view != nil || processor.state != nil ||
		processor.gapCount != 1 {
		t.Fatalf("gap process = %+v, %v, %v, processor=%+v", view, reconnect, err, processor)
	}
}

func TestLocalOrderBookRequiresSnapshotAfterReconnect(t *testing.T) {
	book := newTestLocalOrderBook(t, 20)
	processor := &localOrderBookProcessor{book: book}
	if _, _, err := processor.processSnapshot(1, localBookSnapshot(10, 1000)); err != nil {
		t.Fatalf("snapshot error = %v", err)
	}
	view, reconnect, err := processor.processUpdate(
		2, localBookUpdate(11, 1001, "buy", "100", "1"),
	)
	if err != nil || !reconnect || view != nil || processor.gapCount != 1 {
		t.Fatalf("new generation update = %+v, %v, %v", view, reconnect, err)
	}
	view, reconnect, err = processor.processSnapshot(3, localBookSnapshot(20, 2000))
	if err != nil || reconnect || view == nil || view.Generation != 3 ||
		view.SynchronizationID != 2 || view.GapCount != 1 {
		t.Fatalf("reconnected snapshot = %+v, %v, %v", view, reconnect, err)
	}
}

func TestLocalOrderBookRunReconnectsOnSameRouteAfterGap(t *testing.T) {
	first := newFuturesWSTestConnection()
	second := newFuturesWSTestConnection()
	connector := &futuresWSTestConnector{connections: []*futuresWSTestConnection{first, second}}
	client := newTestFuturesStreamClient(t, connector, nil)
	subscription := PublicStreamSubscription{
		Feed: PublicFeedBook, ProductIDs: []string{"PI_XBTUSD"},
	}
	public, err := client.PublicStream(
		PublicStreamRequest{Subscriptions: []PublicStreamSubscription{subscription}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book := newTestLocalOrderBook(t, 20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	views := make(chan LocalOrderBookView, 2)
	done := make(chan error, 1)
	go func() {
		done <- book.Run(ctx, public, func(_ context.Context, view LocalOrderBookView) error {
			views <- view
			if view.SynchronizationID == 2 {
				cancel()
			}
			return nil
		})
	}()
	assertPublicStreamOperation(t, waitForFuturesWSWrite(t, first), "subscribe", subscription)
	first.reads <- futuresWSReadResult{message: corestream.Message{Data: []byte(
		`{"feed":"book_snapshot","product_id":"PI_XBTUSD","timestamp":1000,"seq":10,"tickSize":null,"bids":[{"price":100,"qty":1}],"asks":[{"price":101,"qty":1}]}`,
	)}}
	initial := waitFuturesLocalOrderBookView(t, views)
	if initial.Generation != 1 || initial.Sequence != 10 || initial.SynchronizationID != 1 {
		t.Fatalf("initial view = %+v", initial)
	}
	first.reads <- futuresWSReadResult{message: corestream.Message{Data: []byte(
		`{"feed":"book","product_id":"PI_XBTUSD","side":"buy","seq":12,"price":100,"qty":2,"timestamp":1001}`,
	)}}
	assertPublicStreamOperation(t, waitForFuturesWSWrite(t, second), "subscribe", subscription)
	second.reads <- futuresWSReadResult{message: corestream.Message{Data: []byte(
		`{"feed":"book_snapshot","product_id":"PI_XBTUSD","timestamp":2000,"seq":20,"tickSize":null,"bids":[{"price":99,"qty":2}],"asks":[{"price":100,"qty":3}]}`,
	)}}
	recovered := waitFuturesLocalOrderBookView(t, views)
	if recovered.Generation != 2 || recovered.Sequence != 20 ||
		recovered.SynchronizationID != 2 || recovered.GapCount != 1 {
		t.Fatalf("recovered view = %+v", recovered)
	}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("local order book Run() did not finish")
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) ||
		len(requests) != 2 || requests[0].Endpoint != requests[1].Endpoint {
		t.Fatalf("reconnect routes = %v, requests = %+v", routes, requests)
	}
}

func TestLocalOrderBookRunValidatesRouteAndSubscription(t *testing.T) {
	t.Parallel()
	connector := &futuresWSTestConnector{}
	client := newTestFuturesStreamClient(t, connector, nil)
	tests := []struct {
		name         string
		bookRoute    transport.EgressRouteID
		subscription PublicStreamSubscription
		want         string
	}{
		{
			name: "route mismatch", bookRoute: "route-a",
			subscription: PublicStreamSubscription{
				Feed: PublicFeedBook, ProductIDs: []string{"PI_XBTUSD"},
			},
			want: "routes must match",
		},
		{
			name: "missing feed", bookRoute: "route-b",
			subscription: PublicStreamSubscription{
				Feed: PublicFeedTicker, ProductIDs: []string{"PI_XBTUSD"},
			},
			want: "required Futures book product",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			public, err := client.PublicStream(
				PublicStreamRequest{Subscriptions: []PublicStreamSubscription{test.subscription}},
				trade.WithEgressRoute("route-b"),
			)
			if err != nil {
				t.Fatalf("PublicStream() error = %v", err)
			}
			book, err := NewLocalOrderBook(LocalOrderBookConfig{
				ProductID: "PI_XBTUSD", EgressRouteID: test.bookRoute,
			})
			if err != nil {
				t.Fatalf("NewLocalOrderBook() error = %v", err)
			}
			runErr := book.Run(context.Background(), public, func(context.Context, LocalOrderBookView) error {
				return nil
			})
			if runErr == nil || !strings.Contains(runErr.Error(), test.want) {
				t.Fatalf("Run() error = %v, want text %q", runErr, test.want)
			}
		})
	}
	if routes, requests := connector.snapshot(); len(routes) != 0 || len(requests) != 0 {
		t.Fatalf("connector was called during validation: routes=%v requests=%v", routes, requests)
	}
}

func TestLocalOrderBookRunStopsOnSubscriptionRejection(t *testing.T) {
	connection := newFuturesWSTestConnection()
	connector := &futuresWSTestConnector{connections: []*futuresWSTestConnection{connection}}
	client := newTestFuturesStreamClient(t, connector, nil)
	subscription := PublicStreamSubscription{
		Feed: PublicFeedBook, ProductIDs: []string{"PI_XBTUSD"},
	}
	public, err := client.PublicStream(PublicStreamRequest{
		Subscriptions: []PublicStreamSubscription{subscription},
	})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		ProductID: "PI_XBTUSD", EgressRouteID: "route-a",
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
	_ = waitForFuturesWSWrite(t, connection)
	connection.reads <- futuresWSReadResult{message: corestream.Message{Data: []byte(
		`{"event":"subscribed_failed","feed":"book","message":"Invalid product id"}`,
	)}}
	select {
	case runErr := <-done:
		var requestError *StreamRequestError
		if !errors.As(runErr, &requestError) || requestError.Message != "Invalid product id" {
			t.Fatalf("Run() error = %v, want StreamRequestError", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("subscription rejection did not stop local order book")
	}
}

func TestNewLocalOrderBookValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config LocalOrderBookConfig
		want   error
	}{
		{name: "invalid product", config: LocalOrderBookConfig{ProductID: "pi_xbtusd", EgressRouteID: "route-a"}},
		{name: "missing route", config: LocalOrderBookConfig{ProductID: "PI_XBTUSD"}, want: trade.ErrMissingEgressRoute},
		{name: "invalid view", config: LocalOrderBookConfig{ProductID: "PI_XBTUSD", ViewDepth: -1, EgressRouteID: "route-a"}},
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

func TestNewLocalOrderBookUsesDefaultViewDepth(t *testing.T) {
	t.Parallel()
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		ProductID: "PI_XBTUSD", EgressRouteID: "route-a",
	})
	if err != nil || book.viewDepth != defaultLocalOrderBookViewDepth {
		t.Fatalf("NewLocalOrderBook() = %+v, %v", book, err)
	}
}

func TestLocalOrderBookRejectsInvalidEvents(t *testing.T) {
	t.Parallel()
	book := newTestLocalOrderBook(t, 20)
	snapshots := []StreamBookSnapshot{
		{Feed: "book", ProductID: "PI_XBTUSD", Sequence: 1, Timestamp: 1},
		{Feed: "book_snapshot", ProductID: "PI_ETHUSD", Sequence: 1, Timestamp: 1},
		{Feed: "book_snapshot", ProductID: "PI_XBTUSD", Timestamp: 1},
		{Feed: "book_snapshot", ProductID: "PI_XBTUSD", Sequence: 1},
		{
			Feed: "book_snapshot", ProductID: "PI_XBTUSD", Sequence: 1, Timestamp: 1,
			Bids: []StreamBookLevel{localBookLevel("0", "1")},
		},
		{
			Feed: "book_snapshot", ProductID: "PI_XBTUSD", Sequence: 1, Timestamp: 1,
			Asks: []StreamBookLevel{localBookLevel("1", "0")},
		},
	}
	for _, snapshot := range snapshots {
		processor := &localOrderBookProcessor{book: book}
		if _, _, err := processor.processSnapshot(1, snapshot); err == nil {
			t.Fatalf("processSnapshot(%+v) error = nil", snapshot)
		}
	}
	updates := []StreamBookUpdate{
		{Feed: "ticker", ProductID: "PI_XBTUSD", Sequence: 1, Timestamp: 1, Side: "buy", Price: "1", Quantity: "1"},
		{Feed: "book", ProductID: "PI_ETHUSD", Sequence: 1, Timestamp: 1, Side: "buy", Price: "1", Quantity: "1"},
		{Feed: "book", ProductID: "PI_XBTUSD", Timestamp: 1, Side: "buy", Price: "1", Quantity: "1"},
		{Feed: "book", ProductID: "PI_XBTUSD", Sequence: 1, Side: "buy", Price: "1", Quantity: "1"},
		{Feed: "book", ProductID: "PI_XBTUSD", Sequence: 1, Timestamp: 1, Side: "bid", Price: "1", Quantity: "1"},
		{Feed: "book", ProductID: "PI_XBTUSD", Sequence: 1, Timestamp: 1, Side: "buy", Price: "0", Quantity: "1"},
		{Feed: "book", ProductID: "PI_XBTUSD", Sequence: 1, Timestamp: 1, Side: "sell", Price: "1", Quantity: "-1"},
	}
	for _, update := range updates {
		processor := &localOrderBookProcessor{book: book}
		if _, _, err := processor.processUpdate(1, update); err == nil {
			t.Fatalf("processUpdate(%+v) error = nil", update)
		}
	}
}

func waitFuturesLocalOrderBookView(
	t *testing.T,
	views <-chan LocalOrderBookView,
) LocalOrderBookView {
	t.Helper()
	select {
	case view := <-views:
		return view
	case <-time.After(time.Second):
		t.Fatal("local order book view was not observed")
		return LocalOrderBookView{}
	}
}
