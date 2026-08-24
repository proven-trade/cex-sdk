package coinbase

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

func newTestLocalOrderBook(t *testing.T, viewDepth int) *LocalOrderBook {
	t.Helper()
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		ProductID: "BTC-USD", ViewDepth: viewDepth, EgressRouteID: "route-b",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	return book
}

func localBookUpdate(side string, price string, quantity string, eventTime string) StreamLevel2Update {
	return StreamLevel2Update{
		Side: side, PriceLevel: price, NewQuantity: quantity, EventTime: eventTime,
	}
}

func localBookEvent(eventType string, updates ...StreamLevel2Update) StreamLevel2Event {
	return StreamLevel2Event{Type: eventType, ProductID: "BTC-USD", Updates: updates}
}

func TestLocalOrderBookSnapshotAndUpdates(t *testing.T) {
	book := newTestLocalOrderBook(t, 2)
	processor := &localOrderBookProcessor{book: book}
	snapshotTime := "2026-08-25T00:00:00Z"
	snapshot := localBookEvent("snapshot",
		localBookUpdate("bid", "100.0", "1.0", snapshotTime),
		localBookUpdate("bid", "99.0", "2.0", snapshotTime),
		localBookUpdate("bid", "98.0", "3.0", snapshotTime),
		localBookUpdate("offer", "101.0", "1.0", snapshotTime),
		localBookUpdate("offer", "102.0", "2.0", snapshotTime),
	)
	view, reconnect, err := processor.process(1, 0, snapshotTime, []StreamLevel2Event{snapshot})
	if err != nil || reconnect || view == nil {
		t.Fatalf("snapshot process = %+v, %v, %v", view, reconnect, err)
	}
	if view.ProductID != "BTC-USD" || view.ViewDepth != 2 || view.Generation != 1 ||
		view.SynchronizationID != 1 || view.GapCount != 0 || view.SequenceNumber != 0 ||
		view.Timestamp != snapshotTime || view.EventTime != snapshotTime {
		t.Fatalf("snapshot metadata = %+v", view)
	}
	wantBids := []LocalOrderBookLevel{
		{Price: "100.0", Quantity: "1.0"},
		{Price: "99.0", Quantity: "2.0"},
	}
	if !slices.Equal(view.Bids, wantBids) {
		t.Fatalf("snapshot bids = %v, want %v", view.Bids, wantBids)
	}

	updateTime := "2026-08-25T00:00:01.123456Z"
	update := localBookEvent("update",
		localBookUpdate("bid", "100.00", "0", updateTime),
		localBookUpdate("bid", "100.5", "4.0", updateTime),
		localBookUpdate("offer", "101.0", "5.0", updateTime),
	)
	view, reconnect, err = processor.process(1, 1, updateTime, []StreamLevel2Event{update})
	if err != nil || reconnect || view == nil {
		t.Fatalf("update process = %+v, %v, %v", view, reconnect, err)
	}
	wantBids = []LocalOrderBookLevel{
		{Price: "100.5", Quantity: "4.0"},
		{Price: "99.0", Quantity: "2.0"},
	}
	wantAsks := []LocalOrderBookLevel{
		{Price: "101.0", Quantity: "5.0"},
		{Price: "102.0", Quantity: "2.0"},
	}
	if view.SequenceNumber != 1 || view.EventTime != updateTime ||
		!slices.Equal(view.Bids, wantBids) || !slices.Equal(view.Asks, wantAsks) {
		t.Fatalf("updated view = %+v", view)
	}
}

func TestLocalOrderBookIgnoresOtherProductsAndOldSequences(t *testing.T) {
	book := newTestLocalOrderBook(t, 20)
	processor := &localOrderBookProcessor{book: book}
	timestamp := "2026-08-25T00:00:00Z"
	snapshot := localBookEvent("snapshot", localBookUpdate("bid", "100", "1", timestamp))
	if _, _, err := processor.process(1, 10, timestamp, []StreamLevel2Event{snapshot}); err != nil {
		t.Fatalf("snapshot error = %v", err)
	}
	other := StreamLevel2Event{
		Type: "update", ProductID: "ETH-USD",
		Updates: []StreamLevel2Update{localBookUpdate("bid", "1", "1", timestamp)},
	}
	view, reconnect, err := processor.process(1, 99, timestamp, []StreamLevel2Event{other})
	if err != nil || reconnect || view != nil || processor.sequenceNumber != 10 {
		t.Fatalf("other product process = %+v, %v, %v, processor=%+v", view, reconnect, err, processor)
	}
	old := localBookEvent("update", localBookUpdate("bid", "100", "9", timestamp))
	view, reconnect, err = processor.process(1, 9, timestamp, []StreamLevel2Event{old})
	if err != nil || reconnect || view != nil || processor.state.bids["100"].quantityText != "1" {
		t.Fatalf("old sequence process = %+v, %v, %v", view, reconnect, err)
	}
}

func TestLocalOrderBookDetectsSequenceGap(t *testing.T) {
	book := newTestLocalOrderBook(t, 20)
	processor := &localOrderBookProcessor{book: book}
	timestamp := "2026-08-25T00:00:00Z"
	snapshot := localBookEvent("snapshot", localBookUpdate("bid", "100", "1", timestamp))
	if _, _, err := processor.process(1, 0, timestamp, []StreamLevel2Event{snapshot}); err != nil {
		t.Fatalf("snapshot error = %v", err)
	}
	update := localBookEvent("update", localBookUpdate("bid", "100", "2", timestamp))
	view, reconnect, err := processor.process(1, 2, timestamp, []StreamLevel2Event{update})
	if err != nil || !reconnect || view != nil || processor.state != nil ||
		processor.sequenceSet || processor.gapCount != 1 {
		t.Fatalf("gap process = %+v, %v, %v, processor=%+v", view, reconnect, err, processor)
	}
}

func TestLocalOrderBookSnapshotAlwaysReplacesState(t *testing.T) {
	book := newTestLocalOrderBook(t, 20)
	processor := &localOrderBookProcessor{book: book}
	timestamp := "2026-08-25T00:00:00Z"
	first := localBookEvent("snapshot",
		localBookUpdate("bid", "100", "1", timestamp),
		localBookUpdate("offer", "101", "1", timestamp),
	)
	if _, _, err := processor.process(1, 0, timestamp, []StreamLevel2Event{first}); err != nil {
		t.Fatalf("first snapshot error = %v", err)
	}
	second := localBookEvent("snapshot",
		localBookUpdate("bid", "90", "2", timestamp),
		localBookUpdate("offer", "91", "3", timestamp),
	)
	view, reconnect, err := processor.process(1, 1, timestamp, []StreamLevel2Event{second})
	if err != nil || reconnect || view == nil || view.SynchronizationID != 2 ||
		len(view.Bids) != 1 || view.Bids[0].Price != "90" ||
		len(view.Asks) != 1 || view.Asks[0].Price != "91" {
		t.Fatalf("replacement snapshot = %+v, %v, %v", view, reconnect, err)
	}
}

func TestLocalOrderBookAppliesMultipleEventsInOneMessage(t *testing.T) {
	book := newTestLocalOrderBook(t, 20)
	processor := &localOrderBookProcessor{book: book}
	timestamp := "2026-08-25T00:00:00Z"
	snapshot := localBookEvent("snapshot",
		localBookUpdate("bid", "100", "1", timestamp),
		localBookUpdate("offer", "101", "1", timestamp),
	)
	update := localBookEvent("update",
		localBookUpdate("bid", "100", "2", timestamp),
		localBookUpdate("offer", "102", "3", timestamp),
	)
	view, reconnect, err := processor.process(
		1, 0, timestamp, []StreamLevel2Event{snapshot, update},
	)
	if err != nil || reconnect || view == nil || view.SynchronizationID != 1 ||
		len(view.Bids) != 1 || view.Bids[0].Quantity != "2" ||
		len(view.Asks) != 2 || view.Asks[1].Price != "102" {
		t.Fatalf("multi-event view = %+v, %v, %v", view, reconnect, err)
	}
}

func TestLocalOrderBookRequiresSnapshotAfterReconnect(t *testing.T) {
	book := newTestLocalOrderBook(t, 20)
	processor := &localOrderBookProcessor{book: book}
	timestamp := "2026-08-25T00:00:00Z"
	snapshot := localBookEvent("snapshot", localBookUpdate("bid", "100", "1", timestamp))
	if _, _, err := processor.process(1, 0, timestamp, []StreamLevel2Event{snapshot}); err != nil {
		t.Fatalf("snapshot error = %v", err)
	}
	update := localBookEvent("update", localBookUpdate("bid", "100", "2", timestamp))
	view, reconnect, err := processor.process(2, 0, timestamp, []StreamLevel2Event{update})
	if err != nil || !reconnect || view != nil || processor.gapCount != 1 {
		t.Fatalf("new generation update = %+v, %v, %v", view, reconnect, err)
	}
	view, reconnect, err = processor.process(3, 0, timestamp, []StreamLevel2Event{snapshot})
	if err != nil || reconnect || view == nil || view.Generation != 3 ||
		view.SynchronizationID != 2 || view.GapCount != 1 {
		t.Fatalf("reconnected snapshot = %+v, %v, %v", view, reconnect, err)
	}
}

func TestLocalOrderBookRunReconnectsOnSameRouteAfterGap(t *testing.T) {
	first := newWSTestConnection()
	second := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{first, second}}
	client := newTestCoinbaseStreamClient(t, connector, nil, nil, nil)
	subscription := StreamSubscription{Channel: StreamChannelLevel2, ProductIDs: []string{"BTC-USD"}}
	public, err := client.PublicStream(
		PublicStreamRequest{Subscriptions: []StreamSubscription{subscription}},
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
	assertWSOperation(t, waitForCoinbaseWSWrite(t, first), "subscribe", StreamSubscription{
		Channel: StreamChannelHeartbeats,
	}, false, nil)
	assertWSOperation(t, waitForCoinbaseWSWrite(t, first), "subscribe", subscription, false, nil)
	first.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"channel":"l2_data","timestamp":"2026-08-25T00:00:00Z","sequence_num":0,"events":[{"type":"snapshot","product_id":"BTC-USD","updates":[{"side":"bid","event_time":"2026-08-25T00:00:00Z","price_level":"100","new_quantity":"1"},{"side":"offer","event_time":"2026-08-25T00:00:00Z","price_level":"101","new_quantity":"1"}]}]}`,
	)}}
	initial := waitCoinbaseLocalOrderBookView(t, views)
	if initial.Generation != 1 || initial.SequenceNumber != 0 || initial.SynchronizationID != 1 {
		t.Fatalf("initial view = %+v", initial)
	}
	first.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"channel":"l2_data","timestamp":"2026-08-25T00:00:02Z","sequence_num":2,"events":[{"type":"update","product_id":"BTC-USD","updates":[{"side":"bid","event_time":"2026-08-25T00:00:02Z","price_level":"100","new_quantity":"2"}]}]}`,
	)}}
	assertWSOperation(t, waitForCoinbaseWSWrite(t, second), "subscribe", StreamSubscription{
		Channel: StreamChannelHeartbeats,
	}, false, nil)
	assertWSOperation(t, waitForCoinbaseWSWrite(t, second), "subscribe", subscription, false, nil)
	second.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"channel":"l2_data","timestamp":"2026-08-25T00:01:00Z","sequence_num":0,"events":[{"type":"snapshot","product_id":"BTC-USD","updates":[{"side":"bid","event_time":"2026-08-25T00:01:00Z","price_level":"99","new_quantity":"2"},{"side":"offer","event_time":"2026-08-25T00:01:00Z","price_level":"100","new_quantity":"3"}]}]}`,
	)}}
	recovered := waitCoinbaseLocalOrderBookView(t, views)
	if recovered.Generation != 2 || recovered.SequenceNumber != 0 ||
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
	connector := &wsTestConnector{}
	client := newTestCoinbaseStreamClient(t, connector, nil, nil, nil)
	tests := []struct {
		name         string
		bookRoute    transport.EgressRouteID
		subscription StreamSubscription
		want         string
	}{
		{
			name: "route mismatch", bookRoute: "route-a",
			subscription: StreamSubscription{
				Channel: StreamChannelLevel2, ProductIDs: []string{"BTC-USD"},
			},
			want: "routes must match",
		},
		{
			name: "missing product", bookRoute: "route-b",
			subscription: StreamSubscription{
				Channel: StreamChannelLevel2, ProductIDs: []string{"ETH-USD"},
			},
			want: "required level2 product",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			public, err := client.PublicStream(
				PublicStreamRequest{Subscriptions: []StreamSubscription{test.subscription}},
				trade.WithEgressRoute("route-b"),
			)
			if err != nil {
				t.Fatalf("PublicStream() error = %v", err)
			}
			book, err := NewLocalOrderBook(LocalOrderBookConfig{
				ProductID: "BTC-USD", EgressRouteID: test.bookRoute,
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
	connection := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{connection}}
	client := newTestCoinbaseStreamClient(t, connector, nil, nil, nil)
	subscription := StreamSubscription{Channel: StreamChannelLevel2, ProductIDs: []string{"BTC-USD"}}
	public, err := client.PublicStream(PublicStreamRequest{Subscriptions: []StreamSubscription{subscription}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		ProductID: "BTC-USD", EgressRouteID: "route-a",
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
	_ = waitForCoinbaseWSWrite(t, connection)
	_ = waitForCoinbaseWSWrite(t, connection)
	connection.reads <- wsReadResult{message: corestream.Message{Data: []byte(
		`{"type":"error","message":"invalid product_ids"}`,
	)}}
	select {
	case runErr := <-done:
		if runErr == nil || !strings.Contains(runErr.Error(), "invalid product_ids") {
			t.Fatalf("Run() error = %v, want subscription rejection", runErr)
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
		{name: "missing product", config: LocalOrderBookConfig{EgressRouteID: "route-a"}},
		{name: "invalid product", config: LocalOrderBookConfig{ProductID: "BTC/USD", EgressRouteID: "route-a"}},
		{name: "missing route", config: LocalOrderBookConfig{ProductID: "BTC-USD"}, want: trade.ErrMissingEgressRoute},
		{name: "invalid view", config: LocalOrderBookConfig{ProductID: "BTC-USD", ViewDepth: -1, EgressRouteID: "route-a"}},
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
		ProductID: "BTC-USD", EgressRouteID: "route-a",
	})
	if err != nil || book.viewDepth != defaultLocalOrderBookViewDepth {
		t.Fatalf("NewLocalOrderBook() = %+v, %v", book, err)
	}
}

func TestLocalOrderBookRejectsInvalidMessages(t *testing.T) {
	t.Parallel()
	book := newTestLocalOrderBook(t, 20)
	timestamp := "2026-08-25T00:00:00Z"
	tests := []struct {
		name      string
		sequence  int64
		timestamp string
		event     StreamLevel2Event
	}{
		{name: "invalid sequence", sequence: -1, timestamp: timestamp, event: localBookEvent("snapshot")},
		{name: "invalid timestamp", sequence: 0, timestamp: "now", event: localBookEvent("snapshot")},
		{name: "invalid event type", sequence: 0, timestamp: timestamp, event: localBookEvent("partial")},
		{name: "invalid side", sequence: 0, timestamp: timestamp, event: localBookEvent("snapshot", localBookUpdate("ask", "1", "1", timestamp))},
		{name: "invalid price", sequence: 0, timestamp: timestamp, event: localBookEvent("snapshot", localBookUpdate("bid", "0", "1", timestamp))},
		{name: "invalid quantity", sequence: 0, timestamp: timestamp, event: localBookEvent("snapshot", localBookUpdate("bid", "1", "-1", timestamp))},
		{name: "invalid event time", sequence: 0, timestamp: timestamp, event: localBookEvent("snapshot", localBookUpdate("bid", "1", "1", "now"))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor := &localOrderBookProcessor{book: book}
			if _, _, err := processor.process(
				1, test.sequence, test.timestamp, []StreamLevel2Event{test.event},
			); err == nil {
				t.Fatal("process() error = nil")
			}
		})
	}
}

func waitCoinbaseLocalOrderBookView(
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
