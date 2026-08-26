package gateio

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

func newTestGateIOLocalOrderBook(
	t *testing.T,
	depth StreamOrderBookDepth,
	viewDepth int,
) *LocalOrderBook {
	t.Helper()
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		CurrencyPair: "BTC_USDT", Depth: depth,
		EgressRouteID: "route-b", ViewDepth: viewDepth,
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	return book
}

func gateIOOrderBookV2Event(
	full bool,
	firstUpdateID int64,
	lastUpdateID int64,
) StreamOrderBookV2 {
	return StreamOrderBookV2{
		Timestamp: 1_000 + lastUpdateID, Full: full, StreamName: "ob.BTC_USDT.50",
		FirstUpdateID: firstUpdateID, LastUpdateID: lastUpdateID,
	}
}

func TestLocalOrderBookSnapshotAndContinuousUpdates(t *testing.T) {
	book := newTestGateIOLocalOrderBook(t, StreamOrderBookDepth50, 2)
	processor := &localOrderBookProcessor{book: book}
	snapshot := gateIOOrderBookV2Event(true, 0, 100)
	snapshot.Bids = []BookLevel{{Price: "99", Amount: "2"}, {Price: "100.0", Amount: "1"}, {Price: "98", Amount: "3"}}
	snapshot.Asks = []BookLevel{{Price: "102", Amount: "2"}, {Price: "101.0", Amount: "1"}}

	view, publish, reconnect, err := processor.process(2_100, snapshot, 1)
	if err != nil || !publish || reconnect {
		t.Fatalf("snapshot process = %+v, %v, %v, %v", view, publish, reconnect, err)
	}
	if view.CurrencyPair != "BTC_USDT" || view.Depth != StreamOrderBookDepth50 ||
		view.Generation != 1 || view.SynchronizationID != 1 || view.UpdateID != 100 ||
		view.Timestamp != 1_100 || view.ReceivedAt != 2_100 || view.GapCount != 0 ||
		!slices.Equal(view.Bids, []BookLevel{{Price: "100.0", Amount: "1"}, {Price: "99", Amount: "2"}}) ||
		!slices.Equal(view.Asks, []BookLevel{{Price: "101.0", Amount: "1"}, {Price: "102", Amount: "2"}}) {
		t.Fatalf("snapshot view = %+v", view)
	}

	delta := gateIOOrderBookV2Event(false, 101, 102)
	delta.Bids = []BookLevel{{Price: "100.00", Amount: "3"}, {Price: "99", Amount: "0"}, {Price: "100.5", Amount: "4"}}
	delta.Asks = []BookLevel{{Price: "101", Amount: "0"}, {Price: "100.75", Amount: "5"}}
	view, publish, reconnect, err = processor.process(2_102, delta, 1)
	if err != nil || !publish || reconnect {
		t.Fatalf("delta process = %+v, %v, %v, %v", view, publish, reconnect, err)
	}
	if view.UpdateID != 102 ||
		!slices.Equal(view.Bids, []BookLevel{{Price: "100.5", Amount: "4"}, {Price: "100.00", Amount: "3"}}) ||
		!slices.Equal(view.Asks, []BookLevel{{Price: "100.75", Amount: "5"}, {Price: "102", Amount: "2"}}) {
		t.Fatalf("delta view = %+v", view)
	}

	empty := gateIOOrderBookV2Event(false, 103, 103)
	view, publish, reconnect, err = processor.process(2_103, empty, 1)
	if err != nil || !publish || reconnect || view.UpdateID != 103 ||
		len(view.Bids) != 2 || len(view.Asks) != 2 {
		t.Fatalf("empty delta process = %+v, %v, %v, %v", view, publish, reconnect, err)
	}
}

func TestLocalOrderBookSnapshotAlwaysReplacesState(t *testing.T) {
	book := newTestGateIOLocalOrderBook(t, StreamOrderBookDepth50, 20)
	processor := &localOrderBookProcessor{book: book}
	first := gateIOOrderBookV2Event(true, 0, 100)
	first.Bids = []BookLevel{{Price: "100", Amount: "1"}}
	if _, _, _, err := processor.process(2_100, first, 1); err != nil {
		t.Fatalf("first snapshot error = %v", err)
	}
	replacement := gateIOOrderBookV2Event(true, 0, 10)
	replacement.Bids = []BookLevel{{Price: "90", Amount: "2"}}
	view, publish, reconnect, err := processor.process(3_000, replacement, 1)
	if err != nil || !publish || reconnect || view.UpdateID != 10 ||
		view.SynchronizationID != 2 || len(view.Bids) != 1 || view.Bids[0].Price != "90" {
		t.Fatalf("replacement snapshot = %+v, %v, %v, %v", view, publish, reconnect, err)
	}
}

func TestLocalOrderBookDetectsEveryNonContinuousUpdate(t *testing.T) {
	tests := []struct {
		name  string
		first int64
		last  int64
	}{
		{name: "forward gap", first: 102, last: 102},
		{name: "duplicate", first: 100, last: 100},
		{name: "overlap", first: 100, last: 101},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			book := newTestGateIOLocalOrderBook(t, StreamOrderBookDepth50, 20)
			processor := &localOrderBookProcessor{book: book}
			snapshot := gateIOOrderBookV2Event(true, 0, 100)
			snapshot.Bids = []BookLevel{{Price: "100", Amount: "1"}}
			if _, _, _, err := processor.process(2_100, snapshot, 1); err != nil {
				t.Fatalf("snapshot error = %v", err)
			}
			delta := gateIOOrderBookV2Event(false, test.first, test.last)
			view, publish, reconnect, err := processor.process(2_101, delta, 1)
			if err != nil || publish || !reconnect || view.UpdateID != 0 ||
				processor.state != nil || processor.gapCount != 1 {
				t.Fatalf("gap process = %+v, %v, %v, %v, processor=%+v", view, publish, reconnect, err, processor)
			}
		})
	}
}

func TestLocalOrderBookRequiresSnapshotAfterReconnect(t *testing.T) {
	book := newTestGateIOLocalOrderBook(t, StreamOrderBookDepth50, 20)
	processor := &localOrderBookProcessor{book: book}
	snapshot := gateIOOrderBookV2Event(true, 0, 100)
	snapshot.Bids = []BookLevel{{Price: "100", Amount: "1"}}
	if _, _, _, err := processor.process(2_100, snapshot, 1); err != nil {
		t.Fatalf("snapshot error = %v", err)
	}
	delta := gateIOOrderBookV2Event(false, 101, 101)
	_, publish, reconnect, err := processor.process(2_101, delta, 2)
	if err != nil || publish || !reconnect || processor.gapCount != 1 {
		t.Fatalf("new generation delta = %v, %v, %v, processor=%+v", publish, reconnect, err, processor)
	}
	recovered := gateIOOrderBookV2Event(true, 0, 200)
	recovered.Asks = []BookLevel{{Price: "101", Amount: "2"}}
	view, publish, reconnect, err := processor.process(3_200, recovered, 3)
	if err != nil || !publish || reconnect || view.Generation != 3 ||
		view.SynchronizationID != 2 || view.UpdateID != 200 || view.GapCount != 1 {
		t.Fatalf("recovered snapshot = %+v, %v, %v, %v", view, publish, reconnect, err)
	}
}

func TestLocalOrderBookPrunesToSubscribedDepth(t *testing.T) {
	book := newTestGateIOLocalOrderBook(t, StreamOrderBookDepth50, 50)
	processor := &localOrderBookProcessor{book: book}
	snapshot := gateIOOrderBookV2Event(true, 0, 1)
	for index := 1; index <= 50; index++ {
		snapshot.Bids = append(snapshot.Bids, BookLevel{Price: strconv.Itoa(index), Amount: "1"})
	}
	if _, _, _, err := processor.process(2_001, snapshot, 1); err != nil {
		t.Fatalf("snapshot error = %v", err)
	}
	delta := gateIOOrderBookV2Event(false, 2, 2)
	delta.Bids = []BookLevel{{Price: "51", Amount: "1"}}
	view, publish, reconnect, err := processor.process(2_002, delta, 1)
	if err != nil || !publish || reconnect || len(view.Bids) != 50 {
		t.Fatalf("pruned view = %+v, publish=%v reconnect=%v err=%v", view, publish, reconnect, err)
	}
	if view.Bids[0].Price != "51" || view.Bids[len(view.Bids)-1].Price != "2" {
		t.Fatalf("pruned bids = %+v", view.Bids)
	}
}

func TestLocalOrderBookRunHandlesSnapshotBeforeAckAndReconnectsOnSameRoute(t *testing.T) {
	first := newGateIOWebSocketTestConnection()
	second := newGateIOWebSocketTestConnection()
	connector := &gateIOWebSocketTestConnector{
		connections: []*gateIOWebSocketTestConnection{first, second},
	}
	client := newTestGateIOStreamClient(t, connector, nil, nil, func() time.Time {
		return time.Unix(1_700_000_000, 0)
	})
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelOrderBookV2, CurrencyPair: "BTC_USDT",
		OrderBookDepth: StreamOrderBookDepth50,
	}}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book := newTestGateIOLocalOrderBook(t, StreamOrderBookDepth50, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	views := make(chan LocalOrderBookView, 4)
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

	firstCommand := decodeGateIOStreamCommand(t, waitForGateIOWebSocketWrite(t, first))
	assertGateIOStreamCommand(t, firstCommand, "spot.obu", "subscribe", []string{"ob.BTC_USDT.50"}, false)
	sendGateIOLocalOrderBookMessage(first,
		`{"time":1700000000,"time_ms":2100,"channel":"spot.obu","event":"update","result":{"t":1100,"full":true,"s":"ob.BTC_USDT.50","u":100,"b":[["99","2"],["100","1"]],"a":[["102","2"],["101","1"]]}}`)
	initial := waitGateIOLocalOrderBookView(t, views)
	if initial.Generation != 1 || initial.UpdateID != 100 || initial.SynchronizationID != 1 ||
		len(initial.Bids) != 2 || initial.Bids[0].Price != "100" ||
		len(initial.Asks) != 2 || initial.Asks[0].Price != "101" {
		t.Fatalf("initial view = %+v", initial)
	}
	sendGateIOLocalOrderBookMessage(first,
		`{"time":1700000000,"time_ms":2100,"id":`+strconv.FormatInt(firstCommand.ID, 10)+`,"channel":"spot.obu","event":"subscribe","payload":["ob.BTC_USDT.50"],"result":{"status":"success"}}`)
	sendGateIOLocalOrderBookMessage(first,
		`{"time":1700000000,"time_ms":2102,"channel":"spot.obu","event":"update","result":{"t":1102,"s":"ob.BTC_USDT.50","U":101,"u":102,"b":[["100","3"]],"a":[["101","0"],["100.5","4"]]}}`)
	updated := waitGateIOLocalOrderBookView(t, views)
	if updated.UpdateID != 102 || updated.Bids[0].Amount != "3" || updated.Asks[0].Price != "100.5" {
		t.Fatalf("updated view = %+v", updated)
	}
	sendGateIOLocalOrderBookMessage(first,
		`{"time":1700000000,"time_ms":2104,"channel":"spot.obu","event":"update","result":{"t":1104,"s":"ob.BTC_USDT.50","U":104,"u":104,"b":[],"a":[["103","1"]]}}`)
	secondCommand := decodeGateIOStreamCommand(t, waitForGateIOWebSocketWrite(t, second))
	assertGateIOStreamCommand(t, secondCommand, "spot.obu", "subscribe", []string{"ob.BTC_USDT.50"}, false)
	sendGateIOLocalOrderBookMessage(second,
		`{"time":1700000000,"time_ms":3200,"channel":"spot.obu","event":"update","result":{"t":2200,"full":true,"s":"ob.BTC_USDT.50","u":200,"b":[["90","2"]],"a":[["91","3"]]}}`)
	recovered := waitGateIOLocalOrderBookView(t, views)
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

func TestLocalOrderBookRunValidatesRouteAndSubscription(t *testing.T) {
	t.Parallel()
	connector := &gateIOWebSocketTestConnector{}
	client := newTestGateIOStreamClient(t, connector, nil, nil, time.Now)
	tests := []struct {
		name      string
		bookRoute transport.EgressRouteID
		sub       StreamSubscription
		want      string
	}{
		{
			name: "route mismatch", bookRoute: "route-a", want: "routes must match",
			sub: StreamSubscription{Channel: StreamChannelOrderBookV2, CurrencyPair: "BTC_USDT", OrderBookDepth: StreamOrderBookDepth50},
		},
		{
			name: "wrong pair", bookRoute: "route-b", want: "exact order book V2 subscription",
			sub: StreamSubscription{Channel: StreamChannelOrderBookV2, CurrencyPair: "ETH_USDT", OrderBookDepth: StreamOrderBookDepth50},
		},
		{
			name: "wrong depth", bookRoute: "route-b", want: "exact order book V2 subscription",
			sub: StreamSubscription{Channel: StreamChannelOrderBookV2, CurrencyPair: "BTC_USDT", OrderBookDepth: StreamOrderBookDepth400},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			public, err := client.PublicStream(
				StreamRequest{Subscriptions: []StreamSubscription{test.sub}},
				trade.WithEgressRoute("route-b"),
			)
			if err != nil {
				t.Fatalf("PublicStream() error = %v", err)
			}
			book, err := NewLocalOrderBook(LocalOrderBookConfig{
				CurrencyPair: "BTC_USDT", Depth: StreamOrderBookDepth50,
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
	connection := newGateIOWebSocketTestConnection()
	connector := &gateIOWebSocketTestConnector{
		connections: []*gateIOWebSocketTestConnection{connection},
	}
	client := newTestGateIOStreamClient(t, connector, nil, nil, func() time.Time {
		return time.Unix(1_700_000_000, 0)
	})
	public, err := client.PublicStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelOrderBookV2, CurrencyPair: "BTC_USDT",
		OrderBookDepth: StreamOrderBookDepth50,
	}}}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book := newTestGateIOLocalOrderBook(t, StreamOrderBookDepth50, 20)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- book.Run(ctx, public, func(context.Context, LocalOrderBookView) error { return nil })
	}()
	command := decodeGateIOStreamCommand(t, waitForGateIOWebSocketWrite(t, connection))
	sendGateIOLocalOrderBookMessage(connection,
		`{"time":1700000000,"id":`+strconv.FormatInt(command.ID, 10)+`,"channel":"spot.obu","event":"subscribe","error":{"code":2,"message":"invalid stream"},"result":{"status":"fail"}}`)
	select {
	case runErr := <-done:
		if runErr == nil || !strings.Contains(runErr.Error(), "code 2") ||
			!strings.Contains(runErr.Error(), "invalid stream") {
			t.Fatalf("Run() error = %v, want subscription rejection", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("subscription rejection did not stop local order book")
	}
}

func TestNewLocalOrderBookValidationAndDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config LocalOrderBookConfig
		want   error
	}{
		{name: "invalid pair", config: LocalOrderBookConfig{CurrencyPair: "BTCUSDT", Depth: StreamOrderBookDepth50, EgressRouteID: "route-a"}},
		{name: "invalid depth", config: LocalOrderBookConfig{CurrencyPair: "BTC_USDT", Depth: 100, EgressRouteID: "route-a"}},
		{name: "missing route", config: LocalOrderBookConfig{CurrencyPair: "BTC_USDT", Depth: StreamOrderBookDepth50}, want: trade.ErrMissingEgressRoute},
		{name: "negative view", config: LocalOrderBookConfig{CurrencyPair: "BTC_USDT", Depth: StreamOrderBookDepth50, EgressRouteID: "route-a", ViewDepth: -1}},
		{name: "oversized view", config: LocalOrderBookConfig{CurrencyPair: "BTC_USDT", Depth: StreamOrderBookDepth50, EgressRouteID: "route-a", ViewDepth: 51}},
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
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		CurrencyPair: "btc_usdt", Depth: StreamOrderBookDepth400,
		EgressRouteID: " route-a ",
	})
	if err != nil || book.currencyPair != "BTC_USDT" || book.routeID != "route-a" ||
		book.viewDepth != defaultLocalOrderBookViewDepth || book.streamName != "ob.BTC_USDT.400" {
		t.Fatalf("NewLocalOrderBook() defaults = %+v, %v", book, err)
	}
}

func TestLocalOrderBookRejectsInvalidEvents(t *testing.T) {
	t.Parallel()
	book := newTestGateIOLocalOrderBook(t, StreamOrderBookDepth50, 20)
	base := gateIOOrderBookV2Event(true, 0, 1)
	base.Bids = []BookLevel{{Price: "100", Amount: "1"}}
	tests := []struct {
		name       string
		receivedAt int64
		event      StreamOrderBookV2
	}{
		{name: "invalid received time", event: base},
		{name: "invalid event time", receivedAt: 1, event: func() StreamOrderBookV2 { value := base; value.Timestamp = 0; return value }()},
		{name: "invalid update ID", receivedAt: 1, event: func() StreamOrderBookV2 { value := base; value.LastUpdateID = 0; return value }()},
		{name: "wrong stream", receivedAt: 1, event: func() StreamOrderBookV2 { value := base; value.StreamName = "ob.ETH_USDT.50"; return value }()},
		{name: "snapshot first ID", receivedAt: 1, event: func() StreamOrderBookV2 { value := base; value.FirstUpdateID = 1; return value }()},
		{name: "empty snapshot", receivedAt: 1, event: gateIOOrderBookV2Event(true, 0, 1)},
		{name: "zero snapshot amount", receivedAt: 1, event: func() StreamOrderBookV2 {
			value := base
			value.Bids = []BookLevel{{Price: "100", Amount: "0"}}
			return value
		}()},
		{name: "negative amount", receivedAt: 1, event: func() StreamOrderBookV2 {
			value := base
			value.Bids = []BookLevel{{Price: "100", Amount: "-1"}}
			return value
		}()},
		{name: "invalid price", receivedAt: 1, event: func() StreamOrderBookV2 {
			value := base
			value.Bids = []BookLevel{{Price: "zero", Amount: "1"}}
			return value
		}()},
		{name: "duplicate canonical price", receivedAt: 1, event: func() StreamOrderBookV2 {
			value := base
			value.Bids = []BookLevel{{Price: "100", Amount: "1"}, {Price: "100.0", Amount: "2"}}
			return value
		}()},
		{name: "increment missing first ID", receivedAt: 1, event: gateIOOrderBookV2Event(false, 0, 2)},
		{name: "increment reversed range", receivedAt: 1, event: gateIOOrderBookV2Event(false, 3, 2)},
	}
	tooMany := base
	tooMany.Bids = make([]BookLevel, 51)
	for index := range tooMany.Bids {
		tooMany.Bids[index] = BookLevel{Price: strconv.Itoa(index + 1), Amount: "1"}
	}
	tests = append(tests, struct {
		name       string
		receivedAt int64
		event      StreamOrderBookV2
	}{name: "too many levels", receivedAt: 1, event: tooMany})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor := &localOrderBookProcessor{book: book}
			if _, _, _, err := processor.process(test.receivedAt, test.event, 1); err == nil {
				t.Fatal("process() error = nil")
			}
		})
	}
}

func sendGateIOLocalOrderBookMessage(
	connection *gateIOWebSocketTestConnection,
	payload string,
) {
	connection.reads <- gateIOWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText, Data: []byte(payload),
	}}
}

func waitGateIOLocalOrderBookView(
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
