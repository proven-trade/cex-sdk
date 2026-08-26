package okx

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

func newTestLocalOrderBook(t *testing.T, channel string, viewDepth int) *LocalOrderBook {
	t.Helper()
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Channel: channel, InstrumentID: "BTC-USDT", ViewDepth: viewDepth,
		EgressRouteID: "route-b",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	return book
}

func localBookLevel(price string, quantity string, count string) BookLevel {
	return BookLevel{
		Price: price, Quantity: quantity, LiquidatedOrderCount: "0", OrderCount: count,
	}
}

func localBookEvent(sequence int64, previous int64, timestamp string) OrderBook {
	return OrderBook{
		SequenceID: sequence, PreviousSequenceID: previous, Timestamp: timestamp,
	}
}

func TestLocalOrderBookSnapshotAndUpdates(t *testing.T) {
	book := newTestLocalOrderBook(t, "books", 2)
	processor := &localOrderBookProcessor{book: book}
	snapshot := localBookEvent(10, -1, "1000")
	snapshot.Checksum = 123
	snapshot.Bids = []BookLevel{
		localBookLevel("100.0", "1.0", "1"),
		localBookLevel("99.0", "2.0", "2"),
		localBookLevel("98.0", "3.0", "3"),
	}
	snapshot.Asks = []BookLevel{
		localBookLevel("101.0", "1.0", "1"),
		localBookLevel("102.0", "2.0", "2"),
	}

	view, reconnect, err := processor.process(1, "snapshot", snapshot)
	if err != nil || reconnect || view == nil {
		t.Fatalf("snapshot process = %+v, %v, %v", view, reconnect, err)
	}
	if view.Channel != "books" || view.InstrumentID != "BTC-USDT" || view.Depth != 400 ||
		view.Generation != 1 || view.SynchronizationID != 1 || view.GapCount != 0 ||
		view.SequenceID != 10 || view.Timestamp != 1000 {
		t.Fatalf("snapshot metadata = %+v", view)
	}
	wantBids := []BookLevel{
		localBookLevel("100.0", "1.0", "1"),
		localBookLevel("99.0", "2.0", "2"),
	}
	if !slices.Equal(view.Bids, wantBids) {
		t.Fatalf("snapshot bids = %v, want %v", view.Bids, wantBids)
	}

	update := localBookEvent(15, 10, "1001")
	update.Bids = []BookLevel{
		localBookLevel("100.00", "0", "0"),
		localBookLevel("100.5", "4.0", "4"),
	}
	update.Asks = []BookLevel{localBookLevel("101.0", "5.0", "5")}
	view, reconnect, err = processor.process(1, "update", update)
	if err != nil || reconnect || view == nil {
		t.Fatalf("update process = %+v, %v, %v", view, reconnect, err)
	}
	wantBids = []BookLevel{
		localBookLevel("100.5", "4.0", "4"),
		localBookLevel("99.0", "2.0", "2"),
	}
	wantAsks := []BookLevel{
		localBookLevel("101.0", "5.0", "5"),
		localBookLevel("102.0", "2.0", "2"),
	}
	if view.SequenceID != 15 || !slices.Equal(view.Bids, wantBids) ||
		!slices.Equal(view.Asks, wantAsks) {
		t.Fatalf("updated view = %+v", view)
	}
}

func TestLocalOrderBookAcceptsHeartbeatAndMaintenanceSequenceReset(t *testing.T) {
	book := newTestLocalOrderBook(t, "books", 20)
	processor := &localOrderBookProcessor{book: book}
	if _, _, err := processor.process(1, "snapshot", localBookEvent(10, -1, "1000")); err != nil {
		t.Fatalf("snapshot error = %v", err)
	}

	heartbeat := localBookEvent(10, 10, "1060")
	view, reconnect, err := processor.process(1, "update", heartbeat)
	if err != nil || reconnect || view != nil || processor.state.sequenceID != 10 {
		t.Fatalf("heartbeat process = %+v, %v, %v", view, reconnect, err)
	}

	reset := localBookEvent(3, 10, "1061")
	reset.Bids = []BookLevel{localBookLevel("100", "1", "1")}
	view, reconnect, err = processor.process(1, "update", reset)
	if err != nil || reconnect || view == nil || view.SequenceID != 3 {
		t.Fatalf("maintenance reset process = %+v, %v, %v", view, reconnect, err)
	}
	next := localBookEvent(5, 3, "1062")
	view, reconnect, err = processor.process(1, "update", next)
	if err != nil || reconnect || view == nil || view.SequenceID != 5 || view.GapCount != 0 {
		t.Fatalf("post-reset process = %+v, %v, %v", view, reconnect, err)
	}
}

func TestLocalOrderBookDetectsSequenceGaps(t *testing.T) {
	tests := []struct {
		name  string
		event OrderBook
	}{
		{name: "previous mismatch", event: localBookEvent(12, 9, "1001")},
		{name: "nonempty duplicate", event: OrderBook{
			SequenceID: 10, PreviousSequenceID: 10, Timestamp: "1001",
			Bids: []BookLevel{localBookLevel("100", "1", "1")},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			book := newTestLocalOrderBook(t, "books", 20)
			processor := &localOrderBookProcessor{book: book}
			if _, _, err := processor.process(
				1, "snapshot", localBookEvent(10, -1, "1000"),
			); err != nil {
				t.Fatalf("snapshot error = %v", err)
			}
			view, reconnect, err := processor.process(1, "update", test.event)
			if err != nil || !reconnect || view != nil || processor.state != nil ||
				processor.gapCount != 1 {
				t.Fatalf("gap process = %+v, %v, %v, processor=%+v", view, reconnect, err, processor)
			}
		})
	}
}

func TestLocalOrderBookSnapshotChannelsReplaceEveryMessage(t *testing.T) {
	tests := []struct {
		channel string
		depth   int
	}{
		{channel: "books5", depth: 5},
		{channel: "bbo-tbt", depth: 1},
	}
	for _, test := range tests {
		t.Run(test.channel, func(t *testing.T) {
			book := newTestLocalOrderBook(t, test.channel, 0)
			processor := &localOrderBookProcessor{book: book}
			first := localBookEvent(10, 0, "1000")
			first.Bids = []BookLevel{localBookLevel("100", "1", "1")}
			view, reconnect, err := processor.process(1, "", first)
			if err != nil || reconnect || view == nil || view.Depth != test.depth {
				t.Fatalf("first snapshot = %+v, %v, %v", view, reconnect, err)
			}
			second := localBookEvent(10, 0, "1001")
			second.Bids = []BookLevel{localBookLevel("99", "2", "2")}
			view, reconnect, err = processor.process(1, "snapshot", second)
			if err != nil || reconnect || view == nil || view.SynchronizationID != 2 ||
				len(view.Bids) != 1 || view.Bids[0].Price != "99" {
				t.Fatalf("replacement snapshot = %+v, %v, %v", view, reconnect, err)
			}
		})
	}
}

func TestLocalOrderBookRequiresSnapshotAfterReconnect(t *testing.T) {
	book := newTestLocalOrderBook(t, "books", 20)
	processor := &localOrderBookProcessor{book: book}
	if _, _, err := processor.process(1, "snapshot", localBookEvent(10, -1, "1000")); err != nil {
		t.Fatalf("snapshot error = %v", err)
	}
	view, reconnect, err := processor.process(2, "update", localBookEvent(12, 10, "1001"))
	if err != nil || !reconnect || view != nil || processor.gapCount != 1 {
		t.Fatalf("new generation update = %+v, %v, %v", view, reconnect, err)
	}
	view, reconnect, err = processor.process(3, "snapshot", localBookEvent(20, -1, "2000"))
	if err != nil || reconnect || view == nil || view.Generation != 3 ||
		view.SynchronizationID != 2 || view.GapCount != 1 {
		t.Fatalf("reconnected snapshot = %+v, %v, %v", view, reconnect, err)
	}
}

func TestLocalOrderBookRunReconnectsOnSameRouteAfterGap(t *testing.T) {
	first := newWSTestConnection()
	second := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{first, second}}
	client := newTestOKXStreamClient(t, connector, nil, time.Now, nil)
	argument, err := PublicStreamArgument("books", "BTC-USDT")
	if err != nil {
		t.Fatalf("PublicStreamArgument() error = %v", err)
	}
	public, err := client.PublicStream(
		PublicStreamRequest{Endpoint: StreamEndpointPublic, Arguments: []StreamArgument{argument}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book := newTestLocalOrderBook(t, "books", 20)
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
	assertStreamOperation(t, waitForWSWrite(t, first), "subscribe", []StreamArgument{argument})
	first.reads <- wsReadResult{message: corestream.Message{Type: corestream.MessageText, Data: []byte(
		`{"arg":{"channel":"books","instId":"BTC-USDT"},"action":"snapshot","data":[{"asks":[["101","1","0","1"]],"bids":[["100","1","0","1"]],"ts":"1000","checksum":0,"prevSeqId":-1,"seqId":10}]}`,
	)}}
	initial := waitOKXLocalOrderBookView(t, views)
	if initial.Generation != 1 || initial.SequenceID != 10 || initial.SynchronizationID != 1 {
		t.Fatalf("initial view = %+v", initial)
	}
	first.reads <- wsReadResult{message: corestream.Message{Type: corestream.MessageText, Data: []byte(
		`{"arg":{"channel":"books","instId":"BTC-USDT"},"action":"update","data":[{"asks":[],"bids":[],"ts":"1001","checksum":0,"prevSeqId":9,"seqId":12}]}`,
	)}}
	assertStreamOperation(t, waitForWSWrite(t, second), "subscribe", []StreamArgument{argument})
	second.reads <- wsReadResult{message: corestream.Message{Type: corestream.MessageText, Data: []byte(
		`{"arg":{"channel":"books","instId":"BTC-USDT"},"action":"snapshot","data":[{"asks":[["100","3","0","1"]],"bids":[["99","2","0","1"]],"ts":"2000","checksum":0,"prevSeqId":-1,"seqId":20}]}`,
	)}}
	recovered := waitOKXLocalOrderBookView(t, views)
	if recovered.Generation != 2 || recovered.SequenceID != 20 ||
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

func TestLocalOrderBookRunValidatesRouteAndChannel(t *testing.T) {
	t.Parallel()
	connector := &wsTestConnector{}
	client := newTestOKXStreamClient(t, connector, nil, time.Now, nil)
	books, _ := PublicStreamArgument("books", "BTC-USDT")
	ticker, _ := PublicStreamArgument("tickers", "BTC-USDT")
	tests := []struct {
		name      string
		bookRoute transport.EgressRouteID
		argument  StreamArgument
		want      string
	}{
		{name: "route mismatch", bookRoute: "route-a", argument: books, want: "routes must match"},
		{name: "missing channel", bookRoute: "route-b", argument: ticker, want: "required order book channel"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			public, err := client.PublicStream(PublicStreamRequest{
				Endpoint: StreamEndpointPublic, Arguments: []StreamArgument{test.argument},
			}, trade.WithEgressRoute("route-b"))
			if err != nil {
				t.Fatalf("PublicStream() error = %v", err)
			}
			book, err := NewLocalOrderBook(LocalOrderBookConfig{
				Channel: "books", InstrumentID: "BTC-USDT", EgressRouteID: test.bookRoute,
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
	client := newTestOKXStreamClient(t, connector, nil, time.Now, nil)
	argument, _ := PublicStreamArgument("books", "BTC-USDT")
	public, err := client.PublicStream(PublicStreamRequest{
		Endpoint: StreamEndpointPublic, Arguments: []StreamArgument{argument},
	})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Channel: "books", InstrumentID: "BTC-USDT", EgressRouteID: "route-a",
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
		`{"id":"1","event":"error","code":"60012","msg":"invalid channel","connId":"test"}`,
	)}}
	select {
	case runErr := <-done:
		if runErr == nil || !strings.Contains(runErr.Error(), "60012: invalid channel") {
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
		{name: "invalid channel", config: LocalOrderBookConfig{Channel: "books50-l2-tbt", InstrumentID: "BTC-USDT", EgressRouteID: "route-a"}},
		{name: "missing instrument", config: LocalOrderBookConfig{Channel: "books", EgressRouteID: "route-a"}},
		{name: "missing route", config: LocalOrderBookConfig{Channel: "books", InstrumentID: "BTC-USDT"}, want: trade.ErrMissingEgressRoute},
		{name: "invalid view", config: LocalOrderBookConfig{Channel: "books5", InstrumentID: "BTC-USDT", ViewDepth: 6, EgressRouteID: "route-a"}},
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
	book := newTestLocalOrderBook(t, "books", 20)
	tests := []struct {
		name   string
		action string
		event  OrderBook
	}{
		{name: "invalid action", action: "partial", event: localBookEvent(1, -1, "1000")},
		{name: "invalid snapshot previous", action: "snapshot", event: localBookEvent(1, 0, "1000")},
		{name: "invalid sequence", action: "snapshot", event: localBookEvent(-1, -1, "1000")},
		{name: "invalid timestamp", action: "snapshot", event: localBookEvent(1, -1, "0")},
		{name: "invalid price", action: "snapshot", event: OrderBook{
			SequenceID: 1, PreviousSequenceID: -1, Timestamp: "1000",
			Bids: []BookLevel{localBookLevel("0", "1", "1")},
		}},
		{name: "invalid auxiliary count", action: "snapshot", event: OrderBook{
			SequenceID: 1, PreviousSequenceID: -1, Timestamp: "1000",
			Bids: []BookLevel{{Price: "1", Quantity: "1", LiquidatedOrderCount: "1", OrderCount: "1"}},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor := &localOrderBookProcessor{book: book}
			if _, _, err := processor.process(1, test.action, test.event); err == nil {
				t.Fatal("process() error = nil")
			}
		})
	}
}

func waitOKXLocalOrderBookView(
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
