package cryptocom

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

func TestCryptoComLocalOrderBookAppliesSnapshotAndContinuousDeltas(t *testing.T) {
	t.Parallel()
	processor := newTestCryptoComLocalOrderBookProcessor(t)
	view, reconnect, err := processor.process(1, cryptoComLocalSnapshot(100))
	if err != nil || reconnect || view == nil {
		t.Fatalf("snapshot = view %+v, reconnect %v, error %v", view, reconnect, err)
	}
	if view.InstrumentName != "BTC_USDT" || view.Depth != StreamBookDepth10 ||
		view.UpdateFrequency != StreamBookUpdate100Milliseconds || view.Generation != 1 ||
		view.SynchronizationID != 1 || view.GapCount != 0 || view.Sequence != 100 ||
		view.Timestamp != 1_000 || view.TransactionTime != 1_001 {
		t.Fatalf("snapshot metadata = %+v", view)
	}

	delta := cryptoComLocalDelta(100, 101)
	delta.Update.Bids = []BookLevel{
		{Price: "100.0", Quantity: "0", OrderCount: 0},
		{Price: "98", Quantity: "3", OrderCount: 1},
	}
	delta.Update.Asks = []BookLevel{{Price: "101.00", Quantity: "4", OrderCount: 2}}
	view, reconnect, err = processor.process(1, delta)
	if err != nil || reconnect || view == nil {
		t.Fatalf("delta = view %+v, reconnect %v, error %v", view, reconnect, err)
	}
	wantBids := []BookLevel{
		{Price: "99", Quantity: "2", OrderCount: 1},
		{Price: "98", Quantity: "3", OrderCount: 1},
	}
	wantAsks := []BookLevel{
		{Price: "101.00", Quantity: "4", OrderCount: 2},
		{Price: "102", Quantity: "2", OrderCount: 1},
	}
	if view.Sequence != 101 || view.Timestamp != 1_000 || view.TransactionTime != 2_001 ||
		!slices.Equal(view.Bids, wantBids) || !slices.Equal(view.Asks, wantAsks) {
		t.Fatalf("updated view = %+v", view)
	}

	empty := cryptoComLocalDelta(101, 102)
	view, reconnect, err = processor.process(1, empty)
	if err != nil || reconnect || view == nil || view.Sequence != 102 ||
		view.TransactionTime != 2_002 || !slices.Equal(view.Bids, wantBids) ||
		!slices.Equal(view.Asks, wantAsks) {
		t.Fatalf("empty delta view = %+v, reconnect %v, error %v", view, reconnect, err)
	}
}

func TestCryptoComLocalOrderBookAtomicallyReplacesFullSnapshot(t *testing.T) {
	t.Parallel()
	processor := newTestCryptoComLocalOrderBookProcessor(t)
	if _, _, err := processor.process(1, cryptoComLocalSnapshot(100)); err != nil {
		t.Fatalf("initial snapshot error = %v", err)
	}
	replacement := cryptoComLocalSnapshot(200)
	replacement.Bids = []BookLevel{{Price: "90", Quantity: "5", OrderCount: 2}}
	replacement.Asks = []BookLevel{{Price: "91", Quantity: "6", OrderCount: 3}}
	view, reconnect, err := processor.process(1, replacement)
	if err != nil || reconnect || view == nil || view.Sequence != 200 ||
		view.SynchronizationID != 2 || len(view.Bids) != 1 || len(view.Asks) != 1 ||
		view.Bids[0].Price != "90" || view.Asks[0].Price != "91" {
		t.Fatalf("replacement view = %+v, reconnect %v, error %v", view, reconnect, err)
	}
}

func TestCryptoComLocalOrderBookRecoversFromGapAndReconnect(t *testing.T) {
	t.Parallel()
	processor := newTestCryptoComLocalOrderBookProcessor(t)
	if _, _, err := processor.process(1, cryptoComLocalSnapshot(100)); err != nil {
		t.Fatalf("initial snapshot error = %v", err)
	}
	gap := cryptoComLocalDelta(99, 101)
	view, reconnect, err := processor.process(1, gap)
	if err != nil || !reconnect || view != nil || processor.gapCount != 1 ||
		processor.state != nil {
		t.Fatalf("gap = view %+v, reconnect %v, error %v", view, reconnect, err)
	}
	view, reconnect, err = processor.process(1, cryptoComLocalSnapshot(150))
	if err != nil || reconnect || view != nil {
		t.Fatalf("abandoned generation = view %+v, reconnect %v, error %v", view, reconnect, err)
	}
	view, reconnect, err = processor.process(2, cryptoComLocalSnapshot(200))
	if err != nil || reconnect || view == nil || view.Generation != 2 ||
		view.Sequence != 200 || view.SynchronizationID != 2 || view.GapCount != 1 {
		t.Fatalf("recovered view = %+v, reconnect %v, error %v", view, reconnect, err)
	}
	stale, reconnect, err := processor.process(1, cryptoComLocalSnapshot(300))
	if err != nil || reconnect || stale != nil {
		t.Fatalf("stale generation = view %+v, reconnect %v, error %v", stale, reconnect, err)
	}
}

func TestCryptoComLocalOrderBookReconnectsOnRewindAndGenerationDelta(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		generation uint64
		event      StreamBookEvent
	}{
		{name: "rewind", generation: 1, event: cryptoComLocalDelta(100, 100)},
		{name: "new generation delta", generation: 2, event: cryptoComLocalDelta(200, 201)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			processor := newTestCryptoComLocalOrderBookProcessor(t)
			if _, _, err := processor.process(1, cryptoComLocalSnapshot(100)); err != nil {
				t.Fatalf("initial snapshot error = %v", err)
			}
			view, reconnect, err := processor.process(test.generation, test.event)
			if err != nil || !reconnect || view != nil || processor.gapCount != 1 {
				t.Fatalf("sequence recovery = view %+v, reconnect %v, error %v", view, reconnect, err)
			}
		})
	}
}

func TestCryptoComLocalOrderBookRejectsMalformedEvents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		event StreamBookEvent
	}{
		{name: "missing sequence", event: StreamBookEvent{Timestamp: "1", TransactionTime: "2"}},
		{name: "missing snapshot timestamp", event: StreamBookEvent{Sequence: "1", TransactionTime: "2"}},
		{
			name: "duplicate canonical price",
			event: StreamBookEvent{
				Sequence: "1", Timestamp: "1", TransactionTime: "2",
				Bids: []BookLevel{
					{Price: "100", Quantity: "1", OrderCount: 1},
					{Price: "100.0", Quantity: "2", OrderCount: 1},
				},
			},
		},
		{
			name: "zero snapshot quantity",
			event: StreamBookEvent{
				Sequence: "1", Timestamp: "1", TransactionTime: "2",
				Bids: []BookLevel{{Price: "100", Quantity: "0", OrderCount: 0}},
			},
		},
		{
			name: "crossed snapshot",
			event: StreamBookEvent{
				Sequence: "1", Timestamp: "1", TransactionTime: "2",
				Bids: []BookLevel{{Price: "101", Quantity: "1", OrderCount: 1}},
				Asks: []BookLevel{{Price: "100", Quantity: "1", OrderCount: 1}},
			},
		},
		{
			name: "delta with snapshot levels",
			event: StreamBookEvent{
				Sequence: "2", PreviousSequence: "1", TransactionTime: "2",
				Bids:   []BookLevel{{Price: "99", Quantity: "1", OrderCount: 1}},
				Update: &StreamBookDelta{},
			},
		},
		{
			name: "positive quantity without orders",
			event: StreamBookEvent{
				Sequence: "1", Timestamp: "1", TransactionTime: "2",
				Bids: []BookLevel{{Price: "99", Quantity: "1", OrderCount: 0}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			processor := newTestCryptoComLocalOrderBookProcessor(t)
			if _, _, err := processor.process(1, test.event); err == nil {
				t.Fatal("process() error = nil")
			}
		})
	}
}

func TestCryptoComLocalOrderBookRunReconnectsOnSameRoute(t *testing.T) {
	t.Parallel()
	first := newCryptoComWebSocketTestConnection()
	second := newCryptoComWebSocketTestConnection()
	connector := &cryptoComWebSocketTestConnector{
		connections: []*cryptoComWebSocketTestConnection{first, second},
	}
	client := newTestCryptoComStreamClient(t, connector)
	subscription := cryptoComLocalBookSubscription()
	public, err := client.PublicStream(
		StreamRequest{Subscriptions: []StreamSubscription{subscription}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book := newTestCryptoComLocalOrderBook(t, "route-b")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	views := make(chan LocalOrderBookView, 2)
	go func() {
		done <- book.Run(ctx, public, func(_ context.Context, view LocalOrderBookView) error {
			views <- view
			if view.Generation == 2 {
				cancel()
			}
			return nil
		})
	}()
	assertCryptoComLocalBookCommand(t, waitForCryptoComWebSocketWrite(t, first))
	first.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		cryptoComLocalSnapshotPayload("-1", 100),
	)}
	firstView := waitCryptoComLocalOrderBookView(t, views)
	if firstView.Generation != 1 || firstView.Sequence != 100 {
		t.Fatalf("first view = %+v", firstView)
	}
	first.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		cryptoComLocalDeltaPayload("-1", 99, 101),
	)}
	assertCryptoComLocalBookCommand(t, waitForCryptoComWebSocketWrite(t, second))
	second.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		cryptoComLocalSnapshotPayload("-1", 200),
	)}
	secondView := waitCryptoComLocalOrderBookView(t, views)
	if secondView.Generation != 2 || secondView.Sequence != 200 ||
		secondView.SynchronizationID != 2 || secondView.GapCount != 1 {
		t.Fatalf("second view = %+v", secondView)
	}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("local order book Run() did not finish")
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) ||
		len(requests) != 2 {
		t.Fatalf("routes = %v, requests = %d", routes, len(requests))
	}
}

func TestCryptoComLocalOrderBookRunValidatesRouteAndSubscription(t *testing.T) {
	t.Parallel()
	connector := &cryptoComWebSocketTestConnector{}
	client := newTestCryptoComStreamClient(t, connector)
	tests := []struct {
		name         string
		bookRoute    transport.EgressRouteID
		subscription StreamSubscription
		want         string
	}{
		{
			name: "route mismatch", bookRoute: "route-a",
			subscription: cryptoComLocalBookSubscription(), want: "routes must match",
		},
		{
			name: "snapshot only", bookRoute: "route-b",
			subscription: StreamSubscription{
				Channel: StreamChannelBook, InstrumentName: "BTC_USDT",
				BookDepth: StreamBookDepth10, BookSubscriptionType: StreamBookSnapshot,
				BookUpdateFrequency: StreamBookUpdate500Milliseconds,
			},
			want: "exact incremental book",
		},
		{
			name: "missing book", bookRoute: "route-b",
			subscription: StreamSubscription{
				Channel: StreamChannelTicker, InstrumentName: "BTC_USDT",
			},
			want: "exact incremental book",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			public, err := client.PublicStream(
				StreamRequest{Subscriptions: []StreamSubscription{test.subscription}},
				trade.WithEgressRoute("route-b"),
			)
			if err != nil {
				t.Fatalf("PublicStream() error = %v", err)
			}
			book := newTestCryptoComLocalOrderBook(t, test.bookRoute)
			runErr := book.Run(context.Background(), public, func(
				context.Context,
				LocalOrderBookView,
			) error {
				return nil
			})
			if runErr == nil || !strings.Contains(runErr.Error(), test.want) {
				t.Fatalf("Run() error = %v, want text %q", runErr, test.want)
			}
		})
	}
	if routes, requests := connector.snapshot(); len(routes) != 0 || len(requests) != 0 {
		t.Fatalf("connector called during validation: routes=%v requests=%v", routes, requests)
	}
}

func TestCryptoComLocalOrderBookRunStopsOnSubscriptionRejection(t *testing.T) {
	t.Parallel()
	connection := newCryptoComWebSocketTestConnection()
	client := newTestCryptoComStreamClient(t, &cryptoComWebSocketTestConnector{
		connections: []*cryptoComWebSocketTestConnection{connection},
	})
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{
		cryptoComLocalBookSubscription(),
	}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book := newTestCryptoComLocalOrderBook(t, "route-a")
	done := make(chan error, 1)
	go func() {
		done <- book.Run(context.Background(), public, func(
			context.Context,
			LocalOrderBookView,
		) error {
			return nil
		})
	}()
	command := decodeCryptoComStreamCommand(t, waitForCryptoComWebSocketWrite(t, connection))
	connection.reads <- cryptoComWebSocketReadResult{message: cryptoComTextMessage(
		`{"id":"` + command.ID + `","method":"subscribe","code":"40001","message":"BAD_REQUEST"}`,
	)}
	select {
	case runErr := <-done:
		if runErr == nil || !strings.Contains(runErr.Error(), "subscription failed") {
			t.Fatalf("Run() error = %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscription rejection did not stop local order book")
	}
}

func TestCryptoComLocalOrderBookConfigurationValidation(t *testing.T) {
	t.Parallel()
	tests := []LocalOrderBookConfig{
		{Depth: StreamBookDepth10, EgressRouteID: "route-a"},
		{InstrumentName: "BTC_USDT", EgressRouteID: "route-a"},
		{InstrumentName: "BTC_USDT", Depth: StreamBookDepth10},
		{
			InstrumentName: "BTC_USDT", Depth: StreamBookDepth10,
			UpdateFrequency: "250", EgressRouteID: "route-a",
		},
		{
			InstrumentName: "BTC_USDT", Depth: StreamBookDepth10,
			ViewDepth: 11, EgressRouteID: "route-a",
		},
	}
	for index, config := range tests {
		if _, err := NewLocalOrderBook(config); err == nil {
			t.Fatalf("invalid config %d error = nil", index)
		}
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		InstrumentName: "BTC_USDT", Depth: StreamBookDepth10,
		EgressRouteID: "route-a",
	})
	if err != nil || book.updateFrequency != StreamBookUpdate100Milliseconds ||
		book.viewDepth != 10 {
		t.Fatalf("default book = %+v, error = %v", book, err)
	}
}

func newTestCryptoComLocalOrderBookProcessor(t *testing.T) *localOrderBookProcessor {
	t.Helper()
	return &localOrderBookProcessor{book: newTestCryptoComLocalOrderBook(t, "route-a")}
}

func newTestCryptoComLocalOrderBook(
	t *testing.T,
	routeID transport.EgressRouteID,
) *LocalOrderBook {
	t.Helper()
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		InstrumentName: "BTC_USDT", Depth: StreamBookDepth10,
		UpdateFrequency: StreamBookUpdate100Milliseconds,
		ViewDepth:       10, EgressRouteID: routeID,
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	return book
}

func cryptoComLocalBookSubscription() StreamSubscription {
	return StreamSubscription{
		Channel: StreamChannelBook, InstrumentName: "BTC_USDT",
		BookDepth: StreamBookDepth10, BookSubscriptionType: StreamBookSnapshotAndUpdate,
		BookUpdateFrequency: StreamBookUpdate100Milliseconds,
	}
}

func cryptoComLocalSnapshot(sequence uint64) StreamBookEvent {
	return StreamBookEvent{
		Bids: []BookLevel{
			{Price: "100.00", Quantity: "1", OrderCount: 1},
			{Price: "99", Quantity: "2", OrderCount: 1},
		},
		Asks: []BookLevel{
			{Price: "101", Quantity: "1", OrderCount: 1},
			{Price: "102", Quantity: "2", OrderCount: 1},
		},
		Timestamp: "1000", TransactionTime: "1001",
		Sequence: Scalar(strconvFormatUint(sequence)),
	}
}

func cryptoComLocalDelta(previous, sequence uint64) StreamBookEvent {
	return StreamBookEvent{
		Update: &StreamBookDelta{}, TransactionTime: Scalar(strconvFormatUint(1_900 + sequence)),
		Sequence:         Scalar(strconvFormatUint(sequence)),
		PreviousSequence: Scalar(strconvFormatUint(previous)),
	}
}

func cryptoComLocalSnapshotPayload(id string, sequence uint64) string {
	return `{"id":"` + id + `","method":"subscribe","code":"0","result":{"instrument_name":"BTC_USDT","subscription":"book.BTC_USDT.10","channel":"book","depth":"10","data":[{"bids":[["100","1",1],["99","2",1]],"asks":[["101","1",1],["102","2",1]],"t":"1000","tt":"1001","u":"` + strconvFormatUint(sequence) + `"}]}}`
}

func cryptoComLocalDeltaPayload(id string, previous, sequence uint64) string {
	return `{"id":"` + id + `","method":"subscribe","code":"0","result":{"instrument_name":"BTC_USDT","subscription":"book.BTC_USDT.10","channel":"book","depth":"10","data":[{"update":{"bids":[],"asks":[]},"tt":"2000","u":"` + strconvFormatUint(sequence) + `","pu":"` + strconvFormatUint(previous) + `"}]}}`
}

func assertCryptoComLocalBookCommand(t *testing.T, message corestream.Message) {
	t.Helper()
	command := decodeCryptoComStreamCommand(t, message)
	assertCryptoComStreamCommand(t, command, "subscribe", "book.BTC_USDT.10")
	if command.Params.BookSubscriptionType != "SNAPSHOT_AND_UPDATE" ||
		command.Params.BookUpdateFrequency != "100" {
		t.Fatalf("book command = %+v", command)
	}
}

func waitCryptoComLocalOrderBookView(
	t *testing.T,
	views <-chan LocalOrderBookView,
) LocalOrderBookView {
	t.Helper()
	select {
	case view := <-views:
		return view
	case <-time.After(2 * time.Second):
		t.Fatal("Crypto.com local order book view timeout")
		return LocalOrderBookView{}
	}
}

func strconvFormatUint(value uint64) string {
	return strconv.FormatUint(value, 10)
}
