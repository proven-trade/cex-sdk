package mexc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

type scriptedMEXCSnapshotLoader struct {
	calls   chan struct{}
	results chan localSnapshotResult
}

func newScriptedMEXCSnapshotLoader() *scriptedMEXCSnapshotLoader {
	return &scriptedMEXCSnapshotLoader{
		calls: make(chan struct{}, 16), results: make(chan localSnapshotResult, 16),
	}
}

func (loader *scriptedMEXCSnapshotLoader) load(ctx context.Context) (OrderBook, error) {
	select {
	case loader.calls <- struct{}{}:
	case <-ctx.Done():
		return OrderBook{}, ctx.Err()
	}
	select {
	case result := <-loader.results:
		return result.snapshot, result.err
	case <-ctx.Done():
		return OrderBook{}, ctx.Err()
	}
}

type mexcLocalOrderBookRun struct {
	cancel context.CancelFunc
	inputs chan localDepthInput
	views  chan LocalOrderBookView
	done   chan error
}

func startMEXCLocalOrderBookRun(
	t *testing.T,
	book *LocalOrderBook,
) *mexcLocalOrderBookRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	run := &mexcLocalOrderBookRun{
		cancel: cancel, inputs: make(chan localDepthInput, 32),
		views: make(chan LocalOrderBookView, 32), done: make(chan error, 1),
	}
	go func() {
		run.done <- book.runInputs(ctx, run.inputs, nil, func(
			_ context.Context,
			view LocalOrderBookView,
		) error {
			run.views <- view
			return nil
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-run.done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("MEXC local order book shutdown error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("MEXC local order book did not stop")
		}
	})
	return run
}

func newScriptedMEXCLocalOrderBook(
	t *testing.T,
	loader *scriptedMEXCSnapshotLoader,
	maxBufferedEvents int,
) *LocalOrderBook {
	t.Helper()
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: "route-a",
		UpdateInterval: StreamUpdate100Millis, MaxBufferedEvents: maxBufferedEvents,
		SnapshotTimeout: time.Second, SnapshotRetryInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	book.loadSnapshot = loader.load
	return book
}

func mexcDepthInput(generation uint64, from, to string) localDepthInput {
	return localDepthInput{
		generation: generation,
		channel:    "spot@public.aggre.depth.v3.api.pb@100ms@BTCUSDT",
		symbol:     "BTCUSDT", createTime: 2_000, sendTime: 2_001,
		event: StreamDiffDepth{
			EventType: "push.depth", FromVersion: from, ToVersion: to,
			LastOrderCreateTime: 1_999,
		},
	}
}

func waitMEXCSnapshotCall(t *testing.T, loader *scriptedMEXCSnapshotLoader) {
	t.Helper()
	select {
	case <-loader.calls:
	case <-time.After(time.Second):
		t.Fatal("MEXC depth snapshot call did not arrive")
	}
}

func waitMEXCLocalView(
	t *testing.T,
	views <-chan LocalOrderBookView,
) LocalOrderBookView {
	t.Helper()
	select {
	case view := <-views:
		return view
	case <-time.After(time.Second):
		t.Fatal("MEXC local order book view did not arrive")
		return LocalOrderBookView{}
	}
}

func TestMEXCLocalOrderBookOfficialBridgeAndContinuousUpdates(t *testing.T) {
	loader := newScriptedMEXCSnapshotLoader()
	run := startMEXCLocalOrderBookRun(
		t, newScriptedMEXCLocalOrderBook(t, loader, 8),
	)

	run.inputs <- mexcDepthInput(1, "90", "99")
	waitMEXCSnapshotCall(t, loader)
	bridge := mexcDepthInput(1, "100", "102")
	bridge.createTime, bridge.sendTime = 2_102, 2_103
	bridge.event.LastOrderCreateTime = 2_101
	bridge.event.Bids = []BookLevel{
		{Price: "100.0", Quantity: "0.000"},
		{Price: "98.0", Quantity: "3.0"},
	}
	bridge.event.Asks = []BookLevel{{Price: "101.00", Quantity: "4.0"}}
	run.inputs <- bridge
	loader.results <- localSnapshotResult{snapshot: OrderBook{
		LastUpdateID: 100,
		Bids: []BookLevel{
			{Price: "100.00", Quantity: "1.0"},
			{Price: "99.0", Quantity: "2.0"},
		},
		Asks: []BookLevel{
			{Price: "101.0", Quantity: "1.0"},
			{Price: "102.0", Quantity: "2.0"},
		},
	}}

	view := waitMEXCLocalView(t, run.views)
	if view.Symbol != "BTCUSDT" || view.Generation != 1 ||
		view.SynchronizationID != 1 || view.GapCount != 0 ||
		view.LastVersion != "102" || view.CreateTime != 2_102 ||
		view.SendTime != 2_103 || view.LastOrderCreateTime != 2_101 {
		t.Fatalf("initial view metadata = %+v", view)
	}
	wantBids := []BookLevel{
		{Price: "99.0", Quantity: "2.0"},
		{Price: "98.0", Quantity: "3.0"},
	}
	wantAsks := []BookLevel{
		{Price: "101.00", Quantity: "4.0"},
		{Price: "102.0", Quantity: "2.0"},
	}
	if !slices.Equal(view.Bids, wantBids) || !slices.Equal(view.Asks, wantAsks) {
		t.Fatalf("initial levels = (%v, %v), want (%v, %v)", view.Bids, view.Asks, wantBids, wantAsks)
	}

	next := mexcDepthInput(1, "103", "103")
	next.createTime, next.sendTime = 2_104, 2_105
	next.event.LastOrderCreateTime = 2_103
	next.event.Bids = []BookLevel{{Price: "100.5", Quantity: "1.25"}}
	run.inputs <- next
	view = waitMEXCLocalView(t, run.views)
	if view.LastVersion != "103" || view.CreateTime != 2_104 ||
		view.Bids[0].Price != "100.5" {
		t.Fatalf("updated view = %+v", view)
	}
}

func TestMEXCLocalOrderBookRetriesSnapshotThatCannotBridge(t *testing.T) {
	loader := newScriptedMEXCSnapshotLoader()
	run := startMEXCLocalOrderBookRun(
		t, newScriptedMEXCLocalOrderBook(t, loader, 8),
	)
	run.inputs <- mexcDepthInput(1, "105", "106")

	waitMEXCSnapshotCall(t, loader)
	loader.results <- localSnapshotResult{snapshot: OrderBook{LastUpdateID: 100}}
	waitMEXCSnapshotCall(t, loader)
	loader.results <- localSnapshotResult{snapshot: OrderBook{LastUpdateID: 105}}

	view := waitMEXCLocalView(t, run.views)
	if view.LastVersion != "106" || view.SynchronizationID != 1 || view.GapCount != 0 {
		t.Fatalf("retried snapshot view = %+v", view)
	}
}

func TestMEXCLocalOrderBookRetriesSnapshotLoadError(t *testing.T) {
	loader := newScriptedMEXCSnapshotLoader()
	run := startMEXCLocalOrderBookRun(
		t, newScriptedMEXCLocalOrderBook(t, loader, 8),
	)
	run.inputs <- mexcDepthInput(1, "100", "101")

	waitMEXCSnapshotCall(t, loader)
	loader.results <- localSnapshotResult{err: errors.New("temporary snapshot failure")}
	waitMEXCSnapshotCall(t, loader)
	loader.results <- localSnapshotResult{snapshot: OrderBook{LastUpdateID: 100}}

	view := waitMEXCLocalView(t, run.views)
	if view.LastVersion != "101" || view.SynchronizationID != 1 {
		t.Fatalf("snapshot retry view = %+v", view)
	}
}

func TestMEXCLocalOrderBookRecoversFromBufferedGap(t *testing.T) {
	loader := newScriptedMEXCSnapshotLoader()
	run := startMEXCLocalOrderBookRun(
		t, newScriptedMEXCLocalOrderBook(t, loader, 8),
	)
	run.inputs <- mexcDepthInput(1, "100", "101")
	waitMEXCSnapshotCall(t, loader)
	run.inputs <- mexcDepthInput(1, "103", "103")
	loader.results <- localSnapshotResult{snapshot: OrderBook{LastUpdateID: 100}}

	waitMEXCSnapshotCall(t, loader)
	loader.results <- localSnapshotResult{snapshot: OrderBook{LastUpdateID: 103}}
	recovered := waitMEXCLocalView(t, run.views)
	if recovered.LastVersion != "103" || recovered.SynchronizationID != 1 ||
		recovered.GapCount != 1 {
		t.Fatalf("buffered gap recovery view = %+v", recovered)
	}
}

func TestMEXCLocalOrderBookRecoversFromLiveOverlapAndGap(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
	}{
		{name: "overlap", from: "101", to: "102"},
		{name: "forward gap", from: "103", to: "103"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loader := newScriptedMEXCSnapshotLoader()
			run := startMEXCLocalOrderBookRun(
				t, newScriptedMEXCLocalOrderBook(t, loader, 8),
			)
			run.inputs <- mexcDepthInput(1, "100", "101")
			waitMEXCSnapshotCall(t, loader)
			loader.results <- localSnapshotResult{snapshot: OrderBook{LastUpdateID: 100}}
			_ = waitMEXCLocalView(t, run.views)

			run.inputs <- mexcDepthInput(1, test.from, test.to)
			waitMEXCSnapshotCall(t, loader)
			version := int64(102)
			if test.to == "103" {
				version = 103
			}
			loader.results <- localSnapshotResult{snapshot: OrderBook{LastUpdateID: version}}
			recovered := waitMEXCLocalView(t, run.views)
			if recovered.LastVersion != test.to || recovered.SynchronizationID != 2 ||
				recovered.GapCount != 1 {
				t.Fatalf("recovered view = %+v", recovered)
			}
		})
	}
}

func TestMEXCLocalOrderBookResynchronizesAfterReconnect(t *testing.T) {
	loader := newScriptedMEXCSnapshotLoader()
	run := startMEXCLocalOrderBookRun(
		t, newScriptedMEXCLocalOrderBook(t, loader, 8),
	)
	run.inputs <- mexcDepthInput(1, "100", "101")
	waitMEXCSnapshotCall(t, loader)
	loader.results <- localSnapshotResult{snapshot: OrderBook{LastUpdateID: 100}}
	_ = waitMEXCLocalView(t, run.views)

	run.inputs <- mexcDepthInput(2, "200", "201")
	waitMEXCSnapshotCall(t, loader)
	loader.results <- localSnapshotResult{snapshot: OrderBook{LastUpdateID: 200}}
	reconnected := waitMEXCLocalView(t, run.views)
	if reconnected.Generation != 2 || reconnected.LastVersion != "201" ||
		reconnected.SynchronizationID != 2 || reconnected.GapCount != 0 {
		t.Fatalf("reconnected view = %+v", reconnected)
	}

	run.inputs <- mexcDepthInput(1, "202", "202")
	select {
	case stale := <-run.views:
		t.Fatalf("stale generation published view = %+v", stale)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestMEXCLocalOrderBookStopsOnBufferOverflow(t *testing.T) {
	loader := newScriptedMEXCSnapshotLoader()
	book := newScriptedMEXCLocalOrderBook(t, loader, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inputs := make(chan localDepthInput, 3)
	inputs <- mexcDepthInput(1, "100", "101")
	inputs <- mexcDepthInput(1, "102", "102")
	inputs <- mexcDepthInput(1, "103", "103")

	err := book.runInputs(ctx, inputs, nil, func(context.Context, LocalOrderBookView) error {
		return nil
	})
	if !errors.Is(err, ErrDepthBufferOverflow) {
		t.Fatalf("runInputs() error = %v, want %v", err, ErrDepthBufferOverflow)
	}
}

func TestMEXCLocalOrderBookRunUsesSameRouteForStreamAndSnapshot(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v3/depth" ||
			request.URL.Query().Get("symbol") != "BTCUSDT" ||
			request.URL.Query().Get("limit") != "5000" {
			http.Error(writer, "unexpected depth request", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"lastUpdateId":100,"bids":[["99","2"]],"asks":[["101","1"]]}`)
	}))
	defer server.Close()
	sender := &directSender{}
	restClient, _ := newTestClient(t, server.URL, sender)
	connection := newMEXCWebSocketTestConnection()
	connector := &mexcWebSocketTestConnector{
		connections: []*mexcWebSocketTestConnection{connection},
	}
	streamClient := newTestMEXCStreamClient(t, connector, nil, 0, 0)
	subscription, _ := DiffDepthStream("BTCUSDT", StreamUpdate100Millis)
	public, err := streamClient.PublicStream(
		StreamRequest{Subscriptions: []StreamSubscription{subscription}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		RESTClient: restClient, Symbol: "BTCUSDT", EgressRouteID: "route-b",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	views := make(chan LocalOrderBookView, 1)
	go func() {
		done <- book.Run(ctx, public, func(_ context.Context, view LocalOrderBookView) error {
			views <- view
			cancel()
			return nil
		})
	}()
	assertMEXCStreamCommand(
		t, waitForMEXCWebSocketWrite(t, connection),
		"SUBSCRIPTION", streamSubscriptionName(subscription),
	)
	depthBody := protobufTestMessage(
		protobufTestBytes(2, protobufTestMessage(
			protobufTestString(1, "100"), protobufTestString(2, "3"),
		)),
		protobufTestString(3, "push.depth"), protobufTestString(4, "100"),
		protobufTestString(5, "101"), protobufTestVarint(6, 1_999),
	)
	connection.reads <- mexcWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageBinary,
		Data: protobufTestMessage(
			protobufTestString(1, streamSubscriptionName(subscription)),
			protobufTestBytes(313, depthBody), protobufTestString(3, "BTCUSDT"),
			protobufTestVarint(5, 2_000), protobufTestVarint(6, 2_001),
		),
	}}
	view := waitMEXCLocalView(t, views)
	if view.LastVersion != "101" || len(view.Bids) != 2 || view.Bids[0].Price != "100" {
		t.Fatalf("integrated local view = %+v", view)
	}
	waitForMEXCStreamRun(t, done, context.Canceled)
	if routes, _ := connector.snapshot(); !slices.Equal(routes, []transport.EgressRouteID{"route-b"}) {
		t.Fatalf("WebSocket routes = %v", routes)
	}
	if routes := sender.snapshot(); !slices.Equal(routes, []transport.EgressRouteID{"route-b"}) {
		t.Fatalf("snapshot routes = %v", routes)
	}
}

func TestMEXCLocalOrderBookRunValidatesRouteAndSubscription(t *testing.T) {
	t.Parallel()
	connector := &mexcWebSocketTestConnector{}
	client := newTestMEXCStreamClient(t, connector, nil, 0, 0)
	tests := []struct {
		name      string
		bookRoute transport.EgressRouteID
		interval  StreamUpdateInterval
		want      string
	}{
		{name: "route mismatch", bookRoute: "route-a", interval: StreamUpdate100Millis, want: "routes must match"},
		{name: "wrong interval", bookRoute: "route-b", interval: StreamUpdate10Millis, want: "exact diff depth"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			subscription, _ := DiffDepthStream("BTCUSDT", test.interval)
			public, err := client.PublicStream(
				StreamRequest{Subscriptions: []StreamSubscription{subscription}},
				trade.WithEgressRoute("route-b"),
			)
			if err != nil {
				t.Fatalf("PublicStream() error = %v", err)
			}
			book, err := NewLocalOrderBook(LocalOrderBookConfig{
				RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: test.bookRoute,
			})
			if err != nil {
				t.Fatalf("NewLocalOrderBook() error = %v", err)
			}
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

func TestMEXCLocalOrderBookRunStopsOnSubscriptionRejection(t *testing.T) {
	connection := newMEXCWebSocketTestConnection()
	connector := &mexcWebSocketTestConnector{
		connections: []*mexcWebSocketTestConnection{connection},
	}
	streamClient := newTestMEXCStreamClient(t, connector, nil, 0, 0)
	subscription, _ := DiffDepthStream("BTCUSDT", StreamUpdate100Millis)
	public, err := streamClient.PublicStream(
		StreamRequest{Subscriptions: []StreamSubscription{subscription}},
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: "route-a",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	book.loadSnapshot = func(context.Context) (OrderBook, error) {
		t.Error("snapshot loader called before a depth event")
		return OrderBook{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- book.Run(ctx, public, func(context.Context, LocalOrderBookView) error {
			return nil
		})
	}()
	_ = waitForMEXCWebSocketWrite(t, connection)
	connection.reads <- mexcWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"id":0,"code":1,"msg":"invalid depth channel"}`),
	}}
	select {
	case runErr := <-done:
		if runErr == nil || !strings.Contains(runErr.Error(), "code 1") ||
			!strings.Contains(runErr.Error(), "invalid depth channel") {
			t.Fatalf("Run() error = %v, want subscription rejection", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("subscription rejection did not stop local order book")
	}
}

func TestNewMEXCLocalOrderBookValidationAndDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config LocalOrderBookConfig
		want   error
	}{
		{name: "missing client", config: LocalOrderBookConfig{}},
		{name: "invalid symbol", config: LocalOrderBookConfig{RESTClient: &Client{}, Symbol: "btcusdt", EgressRouteID: "route-a"}},
		{name: "missing route", config: LocalOrderBookConfig{RESTClient: &Client{}, Symbol: "BTCUSDT"}, want: trade.ErrMissingEgressRoute},
		{name: "invalid interval", config: LocalOrderBookConfig{RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: "route-a", UpdateInterval: "20ms"}},
		{name: "snapshot limit", config: LocalOrderBookConfig{RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: "route-a", SnapshotLimit: 5001}},
		{name: "buffer", config: LocalOrderBookConfig{RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: "route-a", MaxBufferedEvents: -1}},
		{name: "view", config: LocalOrderBookConfig{RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: "route-a", SnapshotLimit: 5, ViewDepth: 6}},
		{name: "duration", config: LocalOrderBookConfig{RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: "route-a", SnapshotTimeout: -1}},
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
		RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: " route-a ",
	})
	if err != nil || book.routeID != "route-a" ||
		book.channel != "spot@public.aggre.depth.v3.api.pb@100ms@BTCUSDT" ||
		book.maxBufferedEvents != defaultLocalBufferSize ||
		book.viewDepth != defaultLocalViewDepth {
		t.Fatalf("NewLocalOrderBook() defaults = %+v, error = %v", book, err)
	}
}

func TestMEXCLocalOrderBookRejectsInvalidInputsAndSnapshots(t *testing.T) {
	t.Parallel()
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: "route-a",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	base := mexcDepthInput(1, "100", "101")
	invalidInputs := []localDepthInput{
		func() localDepthInput { value := base; value.generation = 0; return value }(),
		func() localDepthInput { value := base; value.symbol = "ETHUSDT"; return value }(),
		func() localDepthInput { value := base; value.sendTime = 0; return value }(),
		func() localDepthInput { value := base; value.event.FromVersion = "0"; return value }(),
		func() localDepthInput { value := base; value.event.FromVersion = "+100"; return value }(),
		func() localDepthInput { value := base; value.event.ToVersion = "99"; return value }(),
		func() localDepthInput {
			value := base
			value.event.Bids = []BookLevel{{Price: "zero", Quantity: "1"}}
			return value
		}(),
		func() localDepthInput {
			value := base
			value.event.Asks = []BookLevel{
				{Price: "100", Quantity: "1"}, {Price: "100.0", Quantity: "2"},
			}
			return value
		}(),
	}
	for index, input := range invalidInputs {
		if err := book.validateInput(input); err == nil {
			t.Fatalf("invalid input %d was accepted", index)
		}
	}
	invalidSnapshots := []OrderBook{
		{},
		{LastUpdateID: 1, Bids: []BookLevel{{Price: "0", Quantity: "1"}}},
		{LastUpdateID: 1, Asks: []BookLevel{{Price: "1", Quantity: "0"}}},
		{LastUpdateID: 1, Bids: []BookLevel{
			{Price: "100", Quantity: "1"}, {Price: "100.0", Quantity: "2"},
		}},
	}
	for index, snapshot := range invalidSnapshots {
		if err := validateLocalSnapshot(snapshot); err == nil {
			t.Fatalf("invalid snapshot %d was accepted", index)
		}
	}
}
