package htx

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/transport"
)

func TestHTXLocalOrderBookAlignsOfficialRefreshAndUpdates(t *testing.T) {
	t.Parallel()

	processor := newTestHTXLocalProcessor(t, 8)
	first := htxLocalMBPInput(1, 100, 101)
	first.event.Bids = []BookLevel{
		{Price: "100.0", Quantity: "0"},
		{Price: "98", Quantity: "3"},
	}
	first.event.Asks = []BookLevel{{Price: "101.00", Quantity: "4"}}
	if _, publish, request, err := processor.processUpdate(first); err != nil || publish || !request {
		t.Fatalf("first update = publish %v, request %v, error %v", publish, request, err)
	}
	processor.snapshotID = "refresh-1"
	second := htxLocalMBPInput(1, 101, 102)
	second.timestamp = 2_002
	second.event.Bids = []BookLevel{{Price: "97", Quantity: "5"}}
	second.event.Asks = nil
	if _, publish, request, err := processor.processUpdate(second); err != nil || publish || request {
		t.Fatalf("second update = publish %v, request %v, error %v", publish, request, err)
	}
	view, publish, request, err := processor.processSnapshot(
		1, "refresh-1", 1_999,
		StreamMBPSnapshot{
			Sequence: 100,
			Bids: []BookLevel{
				{Price: "100.00", Quantity: "1"},
				{Price: "99", Quantity: "2"},
			},
			Asks: []BookLevel{
				{Price: "101", Quantity: "1"},
				{Price: "102", Quantity: "2"},
			},
		},
	)
	if err != nil || !publish || request {
		t.Fatalf("snapshot = publish %v, request %v, error %v", publish, request, err)
	}
	if view.Symbol != "btcusdt" || view.Depth != StreamMBPDepth20 ||
		view.Generation != 1 || view.SynchronizationID != 1 || view.GapCount != 0 ||
		view.Sequence != 102 || view.Timestamp != 2_002 {
		t.Fatalf("view metadata = %+v", view)
	}
	wantBids := []BookLevel{
		{Price: "99", Quantity: "2"},
		{Price: "98", Quantity: "3"},
		{Price: "97", Quantity: "5"},
	}
	wantAsks := []BookLevel{
		{Price: "101.00", Quantity: "4"},
		{Price: "102", Quantity: "2"},
	}
	if !slices.Equal(view.Bids, wantBids) || !slices.Equal(view.Asks, wantAsks) {
		t.Fatalf("levels = (%v, %v), want (%v, %v)", view.Bids, view.Asks, wantBids, wantAsks)
	}
}

func TestHTXLocalOrderBookWaitsForAlignmentWhenRefreshIsAheadOfBuffer(t *testing.T) {
	t.Parallel()

	processor := newTestHTXLocalProcessor(t, 8)
	_, _, request, err := processor.processUpdate(htxLocalMBPInput(1, 100, 101))
	if err != nil || !request {
		t.Fatalf("processUpdate() request = %v, error = %v", request, err)
	}
	processor.snapshotID = "refresh-1"
	view, publish, request, err := processor.processSnapshot(
		1, "refresh-1", 2_500,
		StreamMBPSnapshot{
			Sequence: 102,
			Bids:     []BookLevel{{Price: "100", Quantity: "1"}},
			Asks:     []BookLevel{{Price: "101", Quantity: "1"}},
		},
	)
	if err != nil || publish || request || processor.state == nil || processor.aligned {
		t.Fatalf("snapshot view = %+v, publish = %v, request = %v, error = %v", view, publish, request, err)
	}
	aligned := htxLocalMBPInput(1, 102, 103)
	aligned.timestamp = 2_501
	view, publish, request, err = processor.processUpdate(aligned)
	if err != nil || !publish || request || view.Sequence != 103 ||
		view.Timestamp != 2_501 || view.SynchronizationID != 1 {
		t.Fatalf("aligned view = %+v, publish = %v, request = %v, error = %v", view, publish, request, err)
	}
}

func TestHTXLocalOrderBookRecoversFromLiveGap(t *testing.T) {
	t.Parallel()

	processor := synchronizedHTXLocalProcessor(t)
	gap := htxLocalMBPInput(1, 103, 104)
	gap.event.Bids = []BookLevel{{Price: "99", Quantity: "2"}}
	if _, publish, request, err := processor.processUpdate(gap); err != nil || publish || !request {
		t.Fatalf("gap update = publish %v, request %v, error %v", publish, request, err)
	}
	processor.snapshotID = "refresh-2"
	view, publish, request, err := processor.processSnapshot(
		1, "refresh-2", 2_003,
		StreamMBPSnapshot{
			Sequence: 103,
			Bids:     []BookLevel{{Price: "100", Quantity: "1"}},
			Asks:     []BookLevel{{Price: "101", Quantity: "1"}},
		},
	)
	if err != nil || !publish || request || view.Sequence != 104 ||
		view.GapCount != 1 || view.SynchronizationID != 2 ||
		len(view.Bids) != 2 || view.Bids[1].Price != "99" {
		t.Fatalf("recovered view = %+v, publish = %v, request = %v, error = %v", view, publish, request, err)
	}
}

func TestHTXLocalOrderBookRequestsAnotherRefreshForBufferedGap(t *testing.T) {
	t.Parallel()

	processor := newTestHTXLocalProcessor(t, 8)
	_, _, request, err := processor.processUpdate(htxLocalMBPInput(1, 100, 101))
	if err != nil || !request {
		t.Fatalf("first update request = %v, error = %v", request, err)
	}
	processor.snapshotID = "refresh-1"
	_, _, request, err = processor.processUpdate(htxLocalMBPInput(1, 102, 103))
	if err != nil || request {
		t.Fatalf("second update request = %v, error = %v", request, err)
	}
	_, publish, request, err := processor.processSnapshot(
		1, "refresh-1", 2_000,
		StreamMBPSnapshot{
			Sequence: 100,
			Bids:     []BookLevel{{Price: "100", Quantity: "1"}},
			Asks:     []BookLevel{{Price: "101", Quantity: "1"}},
		},
	)
	if err != nil || publish || !request || processor.gapCount != 1 ||
		len(processor.buffer) != 1 || processor.buffer[0].event.Sequence != 103 {
		t.Fatalf(
			"buffered gap = publish %v, request %v, gaps %d, buffer %v, error %v",
			publish, request, processor.gapCount, processor.buffer, err,
		)
	}
}

func TestHTXLocalOrderBookResynchronizesAfterReconnect(t *testing.T) {
	t.Parallel()

	processor := synchronizedHTXLocalProcessor(t)
	_, publish, request, err := processor.processUpdate(htxLocalMBPInput(2, 200, 201))
	if err != nil || publish || !request {
		t.Fatalf("reconnect update = publish %v, request %v, error %v", publish, request, err)
	}
	processor.snapshotID = "refresh-2"
	view, publish, request, err := processor.processSnapshot(
		2, "refresh-2", 3_000,
		StreamMBPSnapshot{
			Sequence: 200,
			Bids:     []BookLevel{{Price: "100", Quantity: "1"}},
			Asks:     []BookLevel{{Price: "101", Quantity: "1"}},
		},
	)
	if err != nil || !publish || request || view.Generation != 2 ||
		view.Sequence != 201 || view.SynchronizationID != 2 || view.GapCount != 0 {
		t.Fatalf("reconnected view = %+v, publish = %v, request = %v, error = %v", view, publish, request, err)
	}
}

func TestHTXLocalOrderBookStopsOnBufferOverflow(t *testing.T) {
	t.Parallel()

	processor := newTestHTXLocalProcessor(t, 1)
	_, _, request, err := processor.processUpdate(htxLocalMBPInput(1, 100, 101))
	if err != nil || !request {
		t.Fatalf("first update request = %v, error = %v", request, err)
	}
	processor.snapshotID = "refresh-1"
	_, _, _, err = processor.processUpdate(htxLocalMBPInput(1, 101, 102))
	if !errors.Is(err, ErrMBPBufferOverflow) {
		t.Fatalf("processUpdate() error = %v, want buffer overflow", err)
	}
}

func TestHTXLocalOrderBookRunsRefreshOnSameRoute(t *testing.T) {
	t.Parallel()

	connection := newHTXWebSocketTestConnection()
	connector := &htxWebSocketTestConnector{
		connections: []*htxWebSocketTestConnection{connection},
	}
	client := newTestHTXMBPStreamClient(t, connector)
	subscription := StreamSubscription{
		Channel: StreamChannelMBP, Symbol: "btcusdt", MBPDepth: StreamMBPDepth20,
	}
	stream, err := client.MBPStream(
		StreamRequest{Subscriptions: []StreamSubscription{subscription}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("MBPStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Symbol: "btcusdt", Depth: StreamMBPDepth20, EgressRouteID: "route-b",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	views := make(chan LocalOrderBookView, 1)
	go func() {
		done <- book.Run(ctx, stream, func(_ context.Context, view LocalOrderBookView) error {
			views <- view
			cancel()
			return nil
		})
	}()
	command := decodeHTXStreamCommand(t, waitForHTXWebSocketWrite(t, connection))
	assertHTXStreamCommand(t, command, "subscribe", "market.btcusdt.mbp.20")
	connection.reads <- htxWebSocketReadResult{message: gzipHTXStreamMessage(t,
		`{"ch":"market.btcusdt.mbp.20","ts":2001,"tick":{"seqNum":101,"prevSeqNum":100,"bids":[[100,1]],"asks":[[101,1]]}}`,
	)}
	request := decodeHTXMBPRequest(t, waitForHTXWebSocketWrite(t, connection))
	connection.reads <- htxWebSocketReadResult{message: gzipHTXStreamMessage(t,
		`{"id":"`+request.ID+`","rep":"market.btcusdt.mbp.20","status":"ok","data":{"seqNum":100,"bids":[[99,2]],"asks":[[102,2]]}}`,
	)}
	select {
	case view := <-views:
		if view.Generation != 1 || view.Sequence != 101 ||
			view.Bids[0].Price != "100" || view.Asks[0].Price != "101" {
			t.Fatalf("view = %+v", view)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("local order book view was not delivered")
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
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b"}) ||
		len(requests) != 1 || requests[0].Endpoint != "ws://stream.example.test/feed" {
		t.Fatalf("routes = %v, requests = %+v", routes, requests)
	}
}

func TestHTXLocalOrderBookValidation(t *testing.T) {
	t.Parallel()

	invalid := []LocalOrderBookConfig{
		{Symbol: "BTCUSDT", Depth: StreamMBPDepth20, EgressRouteID: "route-a"},
		{Symbol: "btcusdt", Depth: StreamMBPDepth400, EgressRouteID: "route-a"},
		{Symbol: "btcusdt", Depth: StreamMBPDepth20},
		{Symbol: "btcusdt", Depth: StreamMBPDepth20, EgressRouteID: "route-a", ViewDepth: 21},
		{Symbol: "btcusdt", Depth: StreamMBPDepth20, EgressRouteID: "route-a", MaxBufferedUpdates: -1},
	}
	for _, config := range invalid {
		if _, err := NewLocalOrderBook(config); err == nil {
			t.Fatalf("NewLocalOrderBook(%+v) error = nil", config)
		}
	}
}

func TestHTXLocalOrderBookRejectsMismatchedRouteBeforeConnecting(t *testing.T) {
	t.Parallel()

	connector := &htxWebSocketTestConnector{}
	client := newTestHTXMBPStreamClient(t, connector)
	stream, err := client.MBPStream(
		StreamRequest{Subscriptions: []StreamSubscription{{
			Channel: StreamChannelMBP, Symbol: "btcusdt", MBPDepth: StreamMBPDepth20,
		}}},
		trade.WithEgressRoute("route-a"),
	)
	if err != nil {
		t.Fatalf("MBPStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Symbol: "btcusdt", Depth: StreamMBPDepth20, EgressRouteID: "route-b",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	err = book.Run(context.Background(), stream, func(context.Context, LocalOrderBookView) error {
		return nil
	})
	if !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("Run() error = %v, want validation", err)
	}
	routes, requests := connector.snapshot()
	if len(routes) != 0 || len(requests) != 0 {
		t.Fatalf("routes = %v, requests = %+v", routes, requests)
	}
}

func newTestHTXLocalProcessor(t *testing.T, bufferSize int) *localMBPProcessor {
	t.Helper()
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Symbol: "btcusdt", Depth: StreamMBPDepth20, EgressRouteID: "route-a",
		MaxBufferedUpdates: bufferSize,
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	return &localMBPProcessor{book: book}
}

func synchronizedHTXLocalProcessor(t *testing.T) *localMBPProcessor {
	t.Helper()
	processor := newTestHTXLocalProcessor(t, 8)
	_, _, request, err := processor.processUpdate(htxLocalMBPInput(1, 100, 101))
	if err != nil || !request {
		t.Fatalf("initial update request = %v, error = %v", request, err)
	}
	processor.snapshotID = "refresh-1"
	_, publish, request, err := processor.processSnapshot(
		1, "refresh-1", 2_000,
		StreamMBPSnapshot{
			Sequence: 100,
			Bids:     []BookLevel{{Price: "100", Quantity: "1"}},
			Asks:     []BookLevel{{Price: "101", Quantity: "1"}},
		},
	)
	if err != nil || !publish || request {
		t.Fatalf("initial snapshot = publish %v, request %v, error %v", publish, request, err)
	}
	return processor
}

func htxLocalMBPInput(generation, previous, sequence uint64) localMBPInput {
	return localMBPInput{
		generation: generation, timestamp: 2_001,
		event: StreamMBPUpdate{
			PreviousSequence: previous, Sequence: sequence,
			Bids: []BookLevel{{Price: "100", Quantity: "1"}},
			Asks: []BookLevel{{Price: "101", Quantity: "1"}},
		},
	}
}
