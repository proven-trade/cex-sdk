package bybit

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

func newTestLocalOrderBook(t *testing.T, depth int, viewDepth int) *LocalOrderBook {
	t.Helper()
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Category: CategorySpot, Symbol: "BTCUSDT", Depth: depth,
		ViewDepth: viewDepth, EgressRouteID: "route-b",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	return book
}

func streamOrderBook(updateID int64, sequence int64) StreamOrderBook {
	return StreamOrderBook{
		Symbol: "BTCUSDT", UpdateID: updateID, Sequence: sequence,
		MatchingTime: 999 + updateID,
	}
}

func TestLocalOrderBookSnapshotAndDelta(t *testing.T) {
	book := newTestLocalOrderBook(t, 50, 2)
	processor := &localOrderBookProcessor{book: book}
	snapshot := streamOrderBook(100, 1000)
	snapshot.Bids = [][]string{{"100.0", "1.0"}, {"99.0", "2.0"}, {"98.0", "3.0"}}
	snapshot.Asks = [][]string{{"101.0", "1.0"}, {"102.0", "2.0"}}

	view, reconnect, err := processor.process(1, "snapshot", 1001, snapshot)
	if err != nil || reconnect || view == nil {
		t.Fatalf("snapshot process = %+v, %v, %v", view, reconnect, err)
	}
	if view.Category != CategorySpot || view.Symbol != "BTCUSDT" || view.Depth != 50 ||
		view.Generation != 1 || view.SynchronizationID != 1 || view.GapCount != 0 ||
		view.UpdateID != 100 || view.Sequence != 1000 || view.Timestamp != 1001 ||
		view.MatchingTime != 1099 {
		t.Fatalf("snapshot metadata = %+v", view)
	}
	wantBids := [][]string{{"100.0", "1.0"}, {"99.0", "2.0"}}
	if !slices.EqualFunc(view.Bids, wantBids, slices.Equal) {
		t.Fatalf("snapshot bids = %v, want %v", view.Bids, wantBids)
	}

	delta := streamOrderBook(101, 1002)
	delta.Bids = [][]string{{"100.00", "0"}, {"100.5", "4.0"}}
	delta.Asks = [][]string{{"101.0", "5.0"}}
	view, reconnect, err = processor.process(1, "delta", 1003, delta)
	if err != nil || reconnect || view == nil {
		t.Fatalf("delta process = %+v, %v, %v", view, reconnect, err)
	}
	wantBids = [][]string{{"100.5", "4.0"}, {"99.0", "2.0"}}
	wantAsks := [][]string{{"101.0", "5.0"}, {"102.0", "2.0"}}
	if view.UpdateID != 101 || view.Sequence != 1002 ||
		!slices.EqualFunc(view.Bids, wantBids, slices.Equal) ||
		!slices.EqualFunc(view.Asks, wantAsks, slices.Equal) {
		t.Fatalf("delta view = %+v", view)
	}

	duplicate, reconnect, err := processor.process(1, "delta", 1004, delta)
	if err != nil || reconnect || duplicate != nil {
		t.Fatalf("duplicate process = %+v, %v, %v", duplicate, reconnect, err)
	}
}

func TestLocalOrderBookSnapshotAlwaysReplacesState(t *testing.T) {
	book := newTestLocalOrderBook(t, 50, 20)
	processor := &localOrderBookProcessor{book: book}
	first := streamOrderBook(100, 1000)
	first.Bids = [][]string{{"100", "1"}}
	first.Asks = [][]string{{"101", "1"}}
	if _, _, err := processor.process(1, "snapshot", 1000, first); err != nil {
		t.Fatalf("first snapshot error = %v", err)
	}

	restart := streamOrderBook(1, 1)
	restart.Bids = [][]string{{"90", "2"}}
	restart.Asks = [][]string{{"91", "3"}}
	view, reconnect, err := processor.process(1, "snapshot", 2000, restart)
	if err != nil || reconnect || view == nil {
		t.Fatalf("restart snapshot process = %+v, %v, %v", view, reconnect, err)
	}
	if view.UpdateID != 1 || view.SynchronizationID != 2 || len(view.Bids) != 1 ||
		view.Bids[0][0] != "90" || len(view.Asks) != 1 || view.Asks[0][0] != "91" {
		t.Fatalf("restart snapshot view = %+v", view)
	}
}

func TestLocalOrderBookDetectsUpdateAndCrossSequenceGaps(t *testing.T) {
	tests := []struct {
		name  string
		event StreamOrderBook
	}{
		{name: "update ID jump", event: streamOrderBook(102, 1002)},
		{name: "update ID restart as delta", event: streamOrderBook(1, 1002)},
		{name: "cross sequence regression", event: streamOrderBook(101, 1000)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			book := newTestLocalOrderBook(t, 50, 20)
			processor := &localOrderBookProcessor{book: book}
			snapshot := streamOrderBook(100, 1000)
			if _, _, err := processor.process(1, "snapshot", 1000, snapshot); err != nil {
				t.Fatalf("snapshot error = %v", err)
			}
			view, reconnect, err := processor.process(1, "delta", 1001, test.event)
			if err != nil || !reconnect || view != nil || processor.state != nil ||
				processor.gapCount != 1 {
				t.Fatalf("gap process = %+v, %v, %v, processor=%+v", view, reconnect, err, processor)
			}
		})
	}
}

func TestLocalOrderBookRequiresSnapshotAfterReconnect(t *testing.T) {
	book := newTestLocalOrderBook(t, 50, 20)
	processor := &localOrderBookProcessor{book: book}
	first := streamOrderBook(100, 1000)
	if _, _, err := processor.process(1, "snapshot", 1000, first); err != nil {
		t.Fatalf("snapshot error = %v", err)
	}

	delta := streamOrderBook(101, 1001)
	view, reconnect, err := processor.process(2, "delta", 1001, delta)
	if err != nil || !reconnect || view != nil || processor.gapCount != 1 {
		t.Fatalf("new generation delta = %+v, %v, %v", view, reconnect, err)
	}
	second := streamOrderBook(200, 2000)
	second.Bids = [][]string{{"100", "2"}}
	view, reconnect, err = processor.process(3, "snapshot", 2000, second)
	if err != nil || reconnect || view == nil || view.Generation != 3 ||
		view.SynchronizationID != 2 || view.GapCount != 1 {
		t.Fatalf("reconnected snapshot = %+v, %v, %v", view, reconnect, err)
	}
}

func TestLocalOrderBookRunReconnectsOnSameRouteAfterGap(t *testing.T) {
	first := newWSTestConnection()
	second := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{first, second}}
	client := newTestBybitStreamClient(t, connector, nil, time.Now, nil)
	topic, err := OrderBookStreamTopic(CategorySpot, "BTCUSDT", 50)
	if err != nil {
		t.Fatalf("OrderBookStreamTopic() error = %v", err)
	}
	public, err := client.PublicStream(
		PublicStreamRequest{Category: CategorySpot, Topics: []string{topic}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book := newTestLocalOrderBook(t, 50, 20)
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
	assertStreamOperation(t, waitForWSWrite(t, first), "subscribe", []string{topic})
	first.reads <- wsReadResult{message: corestream.Message{Type: corestream.MessageText, Data: []byte(
		`{"topic":"orderbook.50.BTCUSDT","type":"snapshot","ts":1000,"data":{"s":"BTCUSDT","b":[["100","1"]],"a":[["101","1"]],"u":100,"seq":1000,"cts":999}}`,
	)}}
	initial := waitBybitLocalOrderBookView(t, views)
	if initial.Generation != 1 || initial.UpdateID != 100 || initial.SynchronizationID != 1 {
		t.Fatalf("initial view = %+v", initial)
	}
	first.reads <- wsReadResult{message: corestream.Message{Type: corestream.MessageText, Data: []byte(
		`{"topic":"orderbook.50.BTCUSDT","type":"delta","ts":1002,"data":{"s":"BTCUSDT","b":[],"a":[],"u":102,"seq":1002,"cts":1001}}`,
	)}}
	assertStreamOperation(t, waitForWSWrite(t, second), "subscribe", []string{topic})
	second.reads <- wsReadResult{message: corestream.Message{Type: corestream.MessageText, Data: []byte(
		`{"topic":"orderbook.50.BTCUSDT","type":"snapshot","ts":2000,"data":{"s":"BTCUSDT","b":[["99","2"]],"a":[["100","3"]],"u":200,"seq":2000,"cts":1999}}`,
	)}}
	recovered := waitBybitLocalOrderBookView(t, views)
	if recovered.Generation != 2 || recovered.UpdateID != 200 ||
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

func TestLocalOrderBookRunValidatesRouteAndTopic(t *testing.T) {
	t.Parallel()
	connector := &wsTestConnector{}
	client := newTestBybitStreamClient(t, connector, nil, time.Now, nil)
	bookTopic, _ := OrderBookStreamTopic(CategorySpot, "BTCUSDT", 50)
	tickerTopic, _ := TickerStreamTopic(CategorySpot, "BTCUSDT")
	tests := []struct {
		name      string
		bookRoute transport.EgressRouteID
		topics    []string
		want      string
	}{
		{name: "route mismatch", bookRoute: "route-a", topics: []string{bookTopic}, want: "routes must match"},
		{name: "missing topic", bookRoute: "route-b", topics: []string{tickerTopic}, want: "required order book topic"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			public, err := client.PublicStream(
				PublicStreamRequest{Category: CategorySpot, Topics: test.topics},
				trade.WithEgressRoute("route-b"),
			)
			if err != nil {
				t.Fatalf("PublicStream() error = %v", err)
			}
			book, err := NewLocalOrderBook(LocalOrderBookConfig{
				Category: CategorySpot, Symbol: "BTCUSDT", Depth: 50,
				EgressRouteID: test.bookRoute,
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
	client := newTestBybitStreamClient(t, connector, nil, time.Now, nil)
	topic, _ := OrderBookStreamTopic(CategoryLinear, "BTCUSDT", 50)
	public, err := client.PublicStream(PublicStreamRequest{
		Category: CategoryLinear, Topics: []string{topic},
	})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Category: CategoryLinear, Symbol: "BTCUSDT", Depth: 50,
		EgressRouteID: "route-a",
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
	_ = waitForWSWrite(t, connection)
	connection.reads <- wsReadResult{message: corestream.Message{Type: corestream.MessageText, Data: []byte(
		`{"success":false,"ret_msg":"invalid topic","op":"subscribe","req_id":"1"}`,
	)}}
	select {
	case runErr := <-done:
		if runErr == nil || !strings.Contains(runErr.Error(), "invalid topic") {
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
		{name: "invalid category", config: LocalOrderBookConfig{Category: "inverse"}},
		{name: "invalid symbol", config: LocalOrderBookConfig{Category: CategorySpot, Symbol: "btcusdt", Depth: 50}},
		{name: "invalid depth", config: LocalOrderBookConfig{Category: CategorySpot, Symbol: "BTCUSDT", Depth: 500}},
		{name: "missing route", config: LocalOrderBookConfig{Category: CategorySpot, Symbol: "BTCUSDT", Depth: 50}, want: trade.ErrMissingEgressRoute},
		{name: "invalid view", config: LocalOrderBookConfig{Category: CategorySpot, Symbol: "BTCUSDT", Depth: 50, ViewDepth: 51, EgressRouteID: "route-a"}},
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

func TestLocalOrderBookRejectsInvalidEvents(t *testing.T) {
	t.Parallel()
	book := newTestLocalOrderBook(t, 1, 1)
	tests := []struct {
		name        string
		messageType string
		event       StreamOrderBook
	}{
		{name: "invalid type", messageType: "unknown", event: streamOrderBook(1, 1)},
		{name: "wrong symbol", messageType: "snapshot", event: StreamOrderBook{Symbol: "ETHUSDT", UpdateID: 1, Sequence: 1}},
		{name: "too many levels", messageType: "snapshot", event: StreamOrderBook{Symbol: "BTCUSDT", UpdateID: 1, Sequence: 1, Bids: [][]string{{"1", "1"}, {"0.9", "1"}}}},
		{name: "level 1 delta", messageType: "delta", event: streamOrderBook(2, 2)},
		{name: "invalid level", messageType: "snapshot", event: StreamOrderBook{Symbol: "BTCUSDT", UpdateID: 1, Sequence: 1, Asks: [][]string{{"1", "-1"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor := &localOrderBookProcessor{book: book}
			if _, _, err := processor.process(1, test.messageType, 1, test.event); err == nil {
				t.Fatal("process() error = nil")
			}
		})
	}
}

func waitBybitLocalOrderBookView(
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
