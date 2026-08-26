package usdm

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

type scriptedDepthSnapshotLoader struct {
	calls   chan struct{}
	results chan depthSnapshotResult
}

func newScriptedDepthSnapshotLoader() *scriptedDepthSnapshotLoader {
	return &scriptedDepthSnapshotLoader{
		calls:   make(chan struct{}, 8),
		results: make(chan depthSnapshotResult, 8),
	}
}

func (loader *scriptedDepthSnapshotLoader) load(ctx context.Context) (OrderBook, error) {
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

type localOrderBookRun struct {
	cancel context.CancelFunc
	inputs chan depthInput
	views  chan LocalOrderBookView
	done   chan error
}

func startLocalOrderBookRun(t *testing.T, book *LocalOrderBook) *localOrderBookRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	run := &localOrderBookRun{
		cancel: cancel,
		inputs: make(chan depthInput, 16),
		views:  make(chan LocalOrderBookView, 16),
		done:   make(chan error, 1),
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
				t.Errorf("local order book shutdown error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("local order book did not stop")
		}
	})
	return run
}

func newScriptedLocalOrderBook(
	t *testing.T,
	loader *scriptedDepthSnapshotLoader,
	maxBufferedEvents int,
) *LocalOrderBook {
	t.Helper()
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		RESTClient:            &Client{},
		Symbol:                "btcusdt",
		EgressRouteID:         "route-a",
		MaxBufferedEvents:     maxBufferedEvents,
		SnapshotTimeout:       time.Second,
		SnapshotRetryInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	book.loadSnapshot = loader.load
	return book
}

func waitDepthSnapshotCall(t *testing.T, loader *scriptedDepthSnapshotLoader) {
	t.Helper()
	select {
	case <-loader.calls:
	case <-time.After(time.Second):
		t.Fatal("depth snapshot call did not arrive")
	}
}

func waitLocalOrderBookView(t *testing.T, views <-chan LocalOrderBookView) LocalOrderBookView {
	t.Helper()
	select {
	case view := <-views:
		return view
	case <-time.After(time.Second):
		t.Fatal("local order book view did not arrive")
		return LocalOrderBookView{}
	}
}

func depthEvent(
	generation uint64,
	first int64,
	final int64,
	previous int64,
	eventTime int64,
) depthInput {
	return depthInput{
		generation: generation,
		event: StreamDepth{
			EventType: "depthUpdate", EventTime: eventTime,
			TransactionTime: eventTime + 1, Symbol: "BTCUSDT",
			FirstUpdateID: first, FinalUpdateID: final, PreviousUpdate: previous,
		},
	}
}

func TestLocalOrderBookInitialSynchronizationAndUpdates(t *testing.T) {
	loader := newScriptedDepthSnapshotLoader()
	run := startLocalOrderBookRun(t, newScriptedLocalOrderBook(t, loader, 8))

	run.inputs <- depthEvent(1, 90, 99, 89, 1000)
	waitDepthSnapshotCall(t, loader)
	bridge := depthEvent(1, 99, 101, 98, 1002)
	bridge.event.Bids = []BookLevel{{Price: "100.0", Quantity: "0.000"}, {Price: "98.0", Quantity: "3.0"}}
	bridge.event.Asks = []BookLevel{{Price: "101.00", Quantity: "4.0"}}
	run.inputs <- bridge
	loader.results <- depthSnapshotResult{snapshot: OrderBook{
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

	view := waitLocalOrderBookView(t, run.views)
	if view.Symbol != "BTCUSDT" || view.Generation != 1 || view.SynchronizationID != 1 ||
		view.GapCount != 0 || view.LastUpdateID != 101 || view.EventTime != 1002 ||
		view.TransactionTime != 1003 {
		t.Fatalf("initial view metadata = %+v", view)
	}
	wantBids := []BookLevel{{Price: "99.0", Quantity: "2.0"}, {Price: "98.0", Quantity: "3.0"}}
	wantAsks := []BookLevel{{Price: "101.00", Quantity: "4.0"}, {Price: "102.0", Quantity: "2.0"}}
	if !slices.Equal(view.Bids, wantBids) || !slices.Equal(view.Asks, wantAsks) {
		t.Fatalf("initial levels = (%v, %v), want (%v, %v)", view.Bids, view.Asks, wantBids, wantAsks)
	}

	run.inputs <- depthEvent(1, 100, 101, 100, 1003)
	select {
	case duplicate := <-run.views:
		t.Fatalf("duplicate event published view = %+v", duplicate)
	case <-time.After(20 * time.Millisecond):
	}
	next := depthEvent(1, 102, 102, 101, 1004)
	next.event.Bids = []BookLevel{{Price: "100.5", Quantity: "1.25"}}
	run.inputs <- next
	view = waitLocalOrderBookView(t, run.views)
	if view.LastUpdateID != 102 || view.EventTime != 1004 || view.Bids[0].Price != "100.5" {
		t.Fatalf("updated view = %+v", view)
	}
}

func TestLocalOrderBookRetriesSnapshotThatCannotBridge(t *testing.T) {
	loader := newScriptedDepthSnapshotLoader()
	run := startLocalOrderBookRun(t, newScriptedLocalOrderBook(t, loader, 8))
	run.inputs <- depthEvent(1, 105, 106, 104, 1006)

	waitDepthSnapshotCall(t, loader)
	loader.results <- depthSnapshotResult{snapshot: OrderBook{LastUpdateID: 100}}
	waitDepthSnapshotCall(t, loader)
	loader.results <- depthSnapshotResult{snapshot: OrderBook{LastUpdateID: 105}}

	view := waitLocalOrderBookView(t, run.views)
	if view.LastUpdateID != 106 || view.SynchronizationID != 1 || view.GapCount != 0 {
		t.Fatalf("retried snapshot view = %+v", view)
	}
}

func TestLocalOrderBookRetriesSnapshotLoadError(t *testing.T) {
	loader := newScriptedDepthSnapshotLoader()
	run := startLocalOrderBookRun(t, newScriptedLocalOrderBook(t, loader, 8))
	run.inputs <- depthEvent(1, 100, 101, 99, 1001)

	waitDepthSnapshotCall(t, loader)
	loader.results <- depthSnapshotResult{err: errors.New("temporary snapshot failure")}
	waitDepthSnapshotCall(t, loader)
	loader.results <- depthSnapshotResult{snapshot: OrderBook{LastUpdateID: 100}}

	view := waitLocalOrderBookView(t, run.views)
	if view.LastUpdateID != 101 || view.SynchronizationID != 1 {
		t.Fatalf("snapshot retry view = %+v", view)
	}
}

func TestLocalOrderBookRecoversFromPreviousUpdateGap(t *testing.T) {
	loader := newScriptedDepthSnapshotLoader()
	run := startLocalOrderBookRun(t, newScriptedLocalOrderBook(t, loader, 8))
	run.inputs <- depthEvent(1, 100, 101, 99, 1001)
	waitDepthSnapshotCall(t, loader)
	loader.results <- depthSnapshotResult{snapshot: OrderBook{LastUpdateID: 100}}
	first := waitLocalOrderBookView(t, run.views)
	if first.LastUpdateID != 101 || first.SynchronizationID != 1 {
		t.Fatalf("first synchronized view = %+v", first)
	}

	run.inputs <- depthEvent(1, 102, 103, 102, 1003)
	waitDepthSnapshotCall(t, loader)
	loader.results <- depthSnapshotResult{snapshot: OrderBook{LastUpdateID: 102}}
	recovered := waitLocalOrderBookView(t, run.views)
	if recovered.LastUpdateID != 103 || recovered.SynchronizationID != 2 || recovered.GapCount != 1 {
		t.Fatalf("gap recovery view = %+v", recovered)
	}
}

func TestLocalOrderBookResynchronizesAfterReconnect(t *testing.T) {
	loader := newScriptedDepthSnapshotLoader()
	run := startLocalOrderBookRun(t, newScriptedLocalOrderBook(t, loader, 8))
	run.inputs <- depthEvent(1, 100, 101, 99, 1001)
	waitDepthSnapshotCall(t, loader)
	loader.results <- depthSnapshotResult{snapshot: OrderBook{LastUpdateID: 100}}
	_ = waitLocalOrderBookView(t, run.views)

	run.inputs <- depthEvent(2, 200, 201, 199, 2001)
	waitDepthSnapshotCall(t, loader)
	loader.results <- depthSnapshotResult{snapshot: OrderBook{LastUpdateID: 200}}
	reconnected := waitLocalOrderBookView(t, run.views)
	if reconnected.Generation != 2 || reconnected.LastUpdateID != 201 ||
		reconnected.SynchronizationID != 2 || reconnected.GapCount != 0 {
		t.Fatalf("reconnected view = %+v", reconnected)
	}

	run.inputs <- depthEvent(1, 201, 202, 201, 2002)
	select {
	case stale := <-run.views:
		t.Fatalf("stale generation published view = %+v", stale)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestLocalOrderBookStopsOnBufferOverflow(t *testing.T) {
	loader := newScriptedDepthSnapshotLoader()
	book := newScriptedLocalOrderBook(t, loader, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inputs := make(chan depthInput, 3)
	inputs <- depthEvent(1, 100, 101, 99, 1001)
	inputs <- depthEvent(1, 101, 102, 101, 1002)
	inputs <- depthEvent(1, 102, 103, 102, 1003)

	err := book.runInputs(ctx, inputs, nil, func(context.Context, LocalOrderBookView) error {
		return nil
	})
	if !errors.Is(err, ErrDepthBufferOverflow) {
		t.Fatalf("runInputs() error = %v, want %v", err, ErrDepthBufferOverflow)
	}
}

func TestLocalOrderBookSnapshotUsesConfiguredRoute(t *testing.T) {
	t.Parallel()
	sender := &directSender{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/fapi/v1/depth" || request.URL.Query().Get("symbol") != "BTCUSDT" ||
			request.URL.Query().Get("limit") != "1000" {
			http.Error(writer, "unexpected depth request", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"lastUpdateId":100,"bids":[],"asks":[]}`)
	}))
	defer server.Close()
	client, _ := newTestClient(
		t, server.URL, sender, &recordingProvider{}, time.Now(),
		[]transport.EgressRouteID{"route-a", "route-b"},
	)
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		RESTClient: client, Symbol: "btcusdt", EgressRouteID: "route-b",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	snapshot, err := book.loadSnapshot(context.Background())
	if err != nil {
		t.Fatalf("loadSnapshot() error = %v", err)
	}
	if snapshot.LastUpdateID != 100 {
		t.Fatalf("snapshot LastUpdateID = %d, want 100", snapshot.LastUpdateID)
	}
	if routes := sender.snapshot(); !slices.Equal(routes, []transport.EgressRouteID{"route-b"}) {
		t.Fatalf("snapshot routes = %v, want [route-b]", routes)
	}
}

func TestLocalOrderBookRunValidatesRouteAndDiffDepthSubscription(t *testing.T) {
	t.Parallel()
	connector := &usdmWebSocketTestConnector{}
	streamClient := newTestUSDMStreamClient(t, connector, nil, time.Minute)

	tests := []struct {
		name      string
		bookRoute transport.EgressRouteID
		stream    StreamSubscription
		want      string
	}{
		{name: "route mismatch", bookRoute: "route-b", stream: StreamSubscription{Route: StreamRoutePublic, Name: "btcusdt@depth"}, want: "routes must match"},
		{name: "missing diff depth", bookRoute: "route-a", stream: StreamSubscription{Route: StreamRouteMarket, Name: "btcusdt@aggTrade"}, want: "required diff depth"},
		{name: "partial depth rejected", bookRoute: "route-a", stream: StreamSubscription{Route: StreamRoutePublic, Name: "btcusdt@depth20"}, want: "required diff depth"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			market, marketErr := streamClient.MarketStream(MarketStreamRequest{
				Subscriptions: []StreamSubscription{test.stream},
			})
			if marketErr != nil {
				t.Fatalf("MarketStream() error = %v", marketErr)
			}
			book, bookErr := NewLocalOrderBook(LocalOrderBookConfig{
				RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: test.bookRoute,
			})
			if bookErr != nil {
				t.Fatalf("NewLocalOrderBook() error = %v", bookErr)
			}
			runErr := book.Run(context.Background(), market, func(context.Context, LocalOrderBookView) error {
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

func TestLocalOrderBookRunStopsOnSubscriptionRejection(t *testing.T) {
	connection := newUSDMWebSocketTestConnection()
	connection.reads <- usdmWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"id":1,"code":-1100,"msg":"invalid stream"}`),
	}}
	connector := &usdmWebSocketTestConnector{
		connections: []*usdmWebSocketTestConnection{connection}, connected: make(chan struct{}, 1),
	}
	streamClient := newTestUSDMStreamClient(t, connector, nil, time.Minute)
	depth, err := DiffDepthStream("BTCUSDT", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("DiffDepthStream() error = %v", err)
	}
	market, err := streamClient.MarketStream(MarketStreamRequest{
		Subscriptions: []StreamSubscription{depth},
	})
	if err != nil {
		t.Fatalf("MarketStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: "route-a",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	book.loadSnapshot = func(context.Context) (OrderBook, error) {
		t.Error("snapshot loader was called before a depth event")
		return OrderBook{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = book.Run(ctx, market, func(context.Context, LocalOrderBookView) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "code -1100: invalid stream") {
		t.Fatalf("Run() error = %v, want subscription rejection", err)
	}
}

func TestNewLocalOrderBookValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config LocalOrderBookConfig
		want   error
	}{
		{name: "missing REST client", config: LocalOrderBookConfig{}, want: nil},
		{name: "missing route", config: LocalOrderBookConfig{RESTClient: &Client{}, Symbol: "BTCUSDT"}, want: trade.ErrMissingEgressRoute},
		{name: "invalid snapshot limit", config: LocalOrderBookConfig{RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: "route-a", SnapshotLimit: 200}, want: nil},
		{name: "invalid buffer", config: LocalOrderBookConfig{RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: "route-a", MaxBufferedEvents: -1}, want: nil},
		{name: "invalid view", config: LocalOrderBookConfig{RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: "route-a", SnapshotLimit: 5, ViewDepth: 6}, want: nil},
		{name: "invalid duration", config: LocalOrderBookConfig{RESTClient: &Client{}, Symbol: "BTCUSDT", EgressRouteID: "route-a", SnapshotTimeout: -1}, want: nil},
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

func TestValidateDepthSnapshot(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		snapshot OrderBook
	}{
		{name: "zero update ID", snapshot: OrderBook{}},
		{name: "invalid bid", snapshot: OrderBook{LastUpdateID: 1, Bids: []BookLevel{{Price: "0", Quantity: "1"}}}},
		{name: "invalid ask", snapshot: OrderBook{LastUpdateID: 1, Asks: []BookLevel{{Price: "1", Quantity: "-1"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateDepthSnapshot(test.snapshot); err == nil {
				t.Fatal("validateDepthSnapshot() error = nil")
			}
		})
	}
}
