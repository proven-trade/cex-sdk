package kraken

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"slices"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

func newTestSpotLocalOrderBook(t *testing.T, depth int, viewDepth int) *SpotLocalOrderBook {
	t.Helper()
	book, err := NewSpotLocalOrderBook(SpotLocalOrderBookConfig{
		Symbol: "BTC/USD", Depth: depth, ViewDepth: viewDepth, EgressRouteID: "route-b",
	})
	if err != nil {
		t.Fatalf("NewSpotLocalOrderBook() error = %v", err)
	}
	return book
}

func spotLocalBookLevel(price string, quantity string) SpotStreamBookLevel {
	return SpotStreamBookLevel{Price: SpotStreamDecimal(price), Quantity: SpotStreamDecimal(quantity)}
}

func spotLocalBookEvent(checksum uint32, timestamp string) SpotStreamBook {
	return SpotStreamBook{Symbol: "BTC/USD", Checksum: checksum, Timestamp: timestamp}
}

func checksumText(value string) uint32 {
	return crc32.ChecksumIEEE([]byte(value))
}

func TestSpotLocalOrderBookSnapshotAndUpdates(t *testing.T) {
	book := newTestSpotLocalOrderBook(t, 10, 2)
	processor := &spotLocalOrderBookProcessor{book: book}
	snapshotTime := "2026-08-25T00:00:00Z"
	snapshot := spotLocalBookEvent(checksumText("101110221001992983"), snapshotTime)
	snapshot.Bids = []SpotStreamBookLevel{
		spotLocalBookLevel("100", "1"),
		spotLocalBookLevel("99", "2"),
		spotLocalBookLevel("98", "3"),
	}
	snapshot.Asks = []SpotStreamBookLevel{
		spotLocalBookLevel("101", "1"),
		spotLocalBookLevel("102", "2"),
	}
	view, reconnect, err := processor.process(1, "snapshot", snapshot)
	if err != nil || reconnect || view == nil {
		t.Fatalf("snapshot process = %+v, %v, %v", view, reconnect, err)
	}
	if view.Symbol != "BTC/USD" || view.Depth != 10 || view.ViewDepth != 2 ||
		view.Generation != 1 || view.SynchronizationID != 1 || view.GapCount != 0 ||
		view.Checksum != snapshot.Checksum || view.Timestamp != snapshotTime {
		t.Fatalf("snapshot metadata = %+v", view)
	}
	wantBids := []SpotStreamBookLevel{
		spotLocalBookLevel("100", "1"),
		spotLocalBookLevel("99", "2"),
	}
	if !slices.Equal(view.Bids, wantBids) {
		t.Fatalf("snapshot bids = %v, want %v", view.Bids, wantBids)
	}

	updateTime := "2026-08-25T00:00:01.123456Z"
	update := spotLocalBookEvent(checksumText("1015102210054992983"), updateTime)
	update.Bids = []SpotStreamBookLevel{
		spotLocalBookLevel("100.00", "0"),
		spotLocalBookLevel("100.5", "3"),
		spotLocalBookLevel("100.5", "4"),
	}
	update.Asks = []SpotStreamBookLevel{spotLocalBookLevel("101", "5")}
	view, reconnect, err = processor.process(1, "update", update)
	if err != nil || reconnect || view == nil {
		t.Fatalf("update process = %+v, %v, %v", view, reconnect, err)
	}
	wantBids = []SpotStreamBookLevel{
		spotLocalBookLevel("100.5", "4"),
		spotLocalBookLevel("99", "2"),
	}
	wantAsks := []SpotStreamBookLevel{
		spotLocalBookLevel("101", "5"),
		spotLocalBookLevel("102", "2"),
	}
	if view.Timestamp != updateTime || !slices.Equal(view.Bids, wantBids) ||
		!slices.Equal(view.Asks, wantAsks) {
		t.Fatalf("updated view = %+v", view)
	}
}

func TestSpotLocalOrderBookPrunesToSubscribedDepth(t *testing.T) {
	book := newTestSpotLocalOrderBook(t, 10, 10)
	processor := &spotLocalOrderBookProcessor{book: book}
	timestamp := "2026-08-25T00:00:00Z"
	snapshot := spotLocalBookEvent(0, timestamp)
	for index := 0; index < 10; index++ {
		snapshot.Bids = append(snapshot.Bids, spotLocalBookLevel(
			fmt.Sprintf("%d", 100-index), "1",
		))
		snapshot.Asks = append(snapshot.Asks, spotLocalBookLevel(
			fmt.Sprintf("%d", 101+index), "1",
		))
	}
	state := &spotLocalOrderBookState{
		bids: make(map[string]spotLocalOrderBookLevel),
		asks: make(map[string]spotLocalOrderBookLevel),
	}
	if err := state.apply(snapshot, 10); err != nil {
		t.Fatalf("snapshot apply error = %v", err)
	}
	snapshot.Checksum = state.checksum
	if _, _, err := processor.process(1, "snapshot", snapshot); err != nil {
		t.Fatalf("snapshot process error = %v", err)
	}
	update := spotLocalBookEvent(0, timestamp)
	update.Bids = []SpotStreamBookLevel{spotLocalBookLevel("100.5", "2")}
	update.Asks = []SpotStreamBookLevel{spotLocalBookLevel("100.8", "3")}
	if err := state.apply(update, 10); err != nil {
		t.Fatalf("update apply error = %v", err)
	}
	update.Checksum = state.checksum
	view, reconnect, err := processor.process(1, "update", update)
	if err != nil || reconnect || view == nil || len(view.Bids) != 10 || len(view.Asks) != 10 ||
		view.Bids[9].Price != "92" || view.Asks[9].Price != "109" {
		t.Fatalf("pruned view = %+v, %v, %v", view, reconnect, err)
	}
}

func TestSpotLocalOrderBookMatchesOfficialChecksumVector(t *testing.T) {
	t.Parallel()
	timestamp := "2026-08-25T00:00:00Z"
	event := spotLocalBookEvent(3310070434, timestamp)
	event.Bids = []SpotStreamBookLevel{
		spotLocalBookLevel("45283.5", "0.10000000"),
		spotLocalBookLevel("45283.4", "1.54582015"),
		spotLocalBookLevel("45282.1", "0.10000000"),
		spotLocalBookLevel("45281.0", "0.10000000"),
		spotLocalBookLevel("45280.3", "1.54592586"),
		spotLocalBookLevel("45279.0", "0.07990000"),
		spotLocalBookLevel("45277.6", "0.03310103"),
		spotLocalBookLevel("45277.5", "0.30000000"),
		spotLocalBookLevel("45277.3", "1.54602737"),
		spotLocalBookLevel("45276.6", "0.15445238"),
	}
	event.Asks = []SpotStreamBookLevel{
		spotLocalBookLevel("45285.2", "0.00100000"),
		spotLocalBookLevel("45286.4", "1.54571953"),
		spotLocalBookLevel("45286.6", "1.54571109"),
		spotLocalBookLevel("45289.6", "1.54560911"),
		spotLocalBookLevel("45290.2", "0.15890660"),
		spotLocalBookLevel("45291.8", "1.54553491"),
		spotLocalBookLevel("45294.7", "0.04454749"),
		spotLocalBookLevel("45296.1", "0.35380000"),
		spotLocalBookLevel("45297.5", "0.09945542"),
		spotLocalBookLevel("45299.5", "0.18772827"),
	}
	state := &spotLocalOrderBookState{
		bids: make(map[string]spotLocalOrderBookLevel),
		asks: make(map[string]spotLocalOrderBookLevel),
	}
	if err := state.apply(event, 10); err != nil || state.checksum != event.Checksum {
		t.Fatalf("official checksum = %d, want %d, error = %v", state.checksum, event.Checksum, err)
	}
}

func TestSpotLocalOrderBookDetectsChecksumMismatch(t *testing.T) {
	tests := []struct {
		name       string
		initialize bool
		message    string
	}{
		{name: "snapshot", message: "snapshot"},
		{name: "update", initialize: true, message: "update"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			book := newTestSpotLocalOrderBook(t, 10, 10)
			processor := &spotLocalOrderBookProcessor{book: book}
			timestamp := "2026-08-25T00:00:00Z"
			if test.initialize {
				snapshot := spotLocalBookEvent(checksumText("1001"), timestamp)
				snapshot.Bids = []SpotStreamBookLevel{spotLocalBookLevel("100", "1")}
				if _, _, err := processor.process(1, "snapshot", snapshot); err != nil {
					t.Fatalf("snapshot error = %v", err)
				}
			}
			event := spotLocalBookEvent(1, timestamp)
			event.Bids = []SpotStreamBookLevel{spotLocalBookLevel("100", "2")}
			view, reconnect, err := processor.process(1, test.message, event)
			if err != nil || !reconnect || view != nil || processor.state != nil ||
				processor.gapCount != 1 {
				t.Fatalf("mismatch process = %+v, %v, %v, processor=%+v", view, reconnect, err, processor)
			}
		})
	}
}

func TestSpotLocalOrderBookRequiresSnapshotAfterReconnect(t *testing.T) {
	book := newTestSpotLocalOrderBook(t, 10, 10)
	processor := &spotLocalOrderBookProcessor{book: book}
	timestamp := "2026-08-25T00:00:00Z"
	snapshot := spotLocalBookEvent(checksumText("1001"), timestamp)
	snapshot.Bids = []SpotStreamBookLevel{spotLocalBookLevel("100", "1")}
	if _, _, err := processor.process(1, "snapshot", snapshot); err != nil {
		t.Fatalf("snapshot error = %v", err)
	}
	update := spotLocalBookEvent(checksumText("1002"), timestamp)
	update.Bids = []SpotStreamBookLevel{spotLocalBookLevel("100", "2")}
	view, reconnect, err := processor.process(2, "update", update)
	if err != nil || !reconnect || view != nil || processor.gapCount != 1 {
		t.Fatalf("new generation update = %+v, %v, %v", view, reconnect, err)
	}
	view, reconnect, err = processor.process(3, "snapshot", snapshot)
	if err != nil || reconnect || view == nil || view.Generation != 3 ||
		view.SynchronizationID != 2 || view.GapCount != 1 {
		t.Fatalf("reconnected snapshot = %+v, %v, %v", view, reconnect, err)
	}
}

func TestSpotLocalOrderBookRunReconnectsOnSameRouteAfterMismatch(t *testing.T) {
	first := newSpotWSTestConnection()
	second := newSpotWSTestConnection()
	connector := &spotWSTestConnector{connections: []*spotWSTestConnection{first, second}}
	client := newTestKrakenSpotStreamClient(t, connector, nil)
	subscription := SpotPublicSubscription{
		Channel: SpotChannelBook, Symbols: []string{"BTC/USD"}, Depth: 25,
		Snapshot: boolPointer(true),
	}
	public, err := client.PublicStream(
		SpotPublicStreamRequest{Subscriptions: []SpotPublicSubscription{subscription}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book := newTestSpotLocalOrderBook(t, 25, 20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	views := make(chan SpotLocalOrderBookView, 2)
	done := make(chan error, 1)
	go func() {
		done <- book.Run(ctx, public, func(_ context.Context, view SpotLocalOrderBookView) error {
			views <- view
			if view.SynchronizationID == 2 {
				cancel()
			}
			return nil
		})
	}()
	assertSpotStreamOperation(t, waitForSpotWSWrite(t, first), "subscribe", subscription, "")
	firstChecksum := checksumText("10111001")
	first.reads <- spotWSReadResult{message: corestream.Message{Data: []byte(fmt.Sprintf(
		`{"channel":"book","type":"snapshot","data":[{"symbol":"BTC/USD","bids":[{"price":100,"qty":1}],"asks":[{"price":101,"qty":1}],"checksum":%d,"timestamp":"2026-08-25T00:00:00Z"}]}`,
		firstChecksum,
	))}}
	initial := waitKrakenSpotLocalOrderBookView(t, views)
	if initial.Generation != 1 || initial.SynchronizationID != 1 || initial.Checksum != firstChecksum {
		t.Fatalf("initial view = %+v", initial)
	}
	first.reads <- spotWSReadResult{message: corestream.Message{Data: []byte(
		`{"channel":"book","type":"update","data":[{"symbol":"BTC/USD","bids":[{"price":100,"qty":2}],"asks":[],"checksum":1,"timestamp":"2026-08-25T00:00:01Z"}]}`,
	)}}
	assertSpotStreamOperation(t, waitForSpotWSWrite(t, second), "subscribe", subscription, "")
	secondChecksum := checksumText("1003992")
	second.reads <- spotWSReadResult{message: corestream.Message{Data: []byte(fmt.Sprintf(
		`{"channel":"book","type":"snapshot","data":[{"symbol":"BTC/USD","bids":[{"price":99,"qty":2}],"asks":[{"price":100,"qty":3}],"checksum":%d,"timestamp":"2026-08-25T00:01:00Z"}]}`,
		secondChecksum,
	))}}
	recovered := waitKrakenSpotLocalOrderBookView(t, views)
	if recovered.Generation != 2 || recovered.SynchronizationID != 2 ||
		recovered.GapCount != 1 || recovered.Checksum != secondChecksum {
		t.Fatalf("recovered view = %+v", recovered)
	}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Spot local order book Run() did not finish")
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) ||
		len(requests) != 2 || requests[0].Endpoint != requests[1].Endpoint {
		t.Fatalf("reconnect routes = %v, requests = %+v", routes, requests)
	}
}

func TestSpotLocalOrderBookRunValidatesRouteAndSubscription(t *testing.T) {
	t.Parallel()
	connector := &spotWSTestConnector{}
	client := newTestKrakenSpotStreamClient(t, connector, nil)
	tests := []struct {
		name         string
		bookRoute    transport.EgressRouteID
		subscription SpotPublicSubscription
		want         string
	}{
		{
			name: "route mismatch", bookRoute: "route-a",
			subscription: SpotPublicSubscription{
				Channel: SpotChannelBook, Symbols: []string{"BTC/USD"}, Depth: 10,
			},
			want: "routes must match",
		},
		{
			name: "missing channel", bookRoute: "route-b",
			subscription: SpotPublicSubscription{
				Channel: SpotChannelTicker, Symbols: []string{"BTC/USD"},
			},
			want: "required Spot book subscription",
		},
		{
			name: "snapshot disabled", bookRoute: "route-b",
			subscription: SpotPublicSubscription{
				Channel: SpotChannelBook, Symbols: []string{"BTC/USD"}, Depth: 10,
				Snapshot: boolPointer(false),
			},
			want: "required Spot book subscription",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			public, err := client.PublicStream(
				SpotPublicStreamRequest{Subscriptions: []SpotPublicSubscription{test.subscription}},
				trade.WithEgressRoute("route-b"),
			)
			if err != nil {
				t.Fatalf("PublicStream() error = %v", err)
			}
			book, err := NewSpotLocalOrderBook(SpotLocalOrderBookConfig{
				Symbol: "BTC/USD", Depth: 10, EgressRouteID: test.bookRoute,
			})
			if err != nil {
				t.Fatalf("NewSpotLocalOrderBook() error = %v", err)
			}
			runErr := book.Run(context.Background(), public, func(context.Context, SpotLocalOrderBookView) error {
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

func TestSpotLocalOrderBookRunStopsOnSubscriptionRejection(t *testing.T) {
	connection := newSpotWSTestConnection()
	connector := &spotWSTestConnector{connections: []*spotWSTestConnection{connection}}
	client := newTestKrakenSpotStreamClient(t, connector, nil)
	subscription := SpotPublicSubscription{
		Channel: SpotChannelBook, Symbols: []string{"BTC/USD"}, Depth: 10,
	}
	public, err := client.PublicStream(
		SpotPublicStreamRequest{Subscriptions: []SpotPublicSubscription{subscription}},
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewSpotLocalOrderBook(SpotLocalOrderBookConfig{
		Symbol: "BTC/USD", Depth: 10, EgressRouteID: "route-a",
	})
	if err != nil {
		t.Fatalf("NewSpotLocalOrderBook() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- book.Run(ctx, public, func(context.Context, SpotLocalOrderBookView) error { return nil })
	}()
	_ = waitForSpotWSWrite(t, connection)
	connection.reads <- spotWSReadResult{message: corestream.Message{Data: []byte(
		`{"method":"subscribe","req_id":1,"success":false,"error":"Unsupported field"}`,
	)}}
	select {
	case runErr := <-done:
		var requestError *SpotStreamRequestError
		if !errors.As(runErr, &requestError) || requestError.Message != "Unsupported field" {
			t.Fatalf("Run() error = %v, want SpotStreamRequestError", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("subscription rejection did not stop Spot local order book")
	}
}

func TestNewSpotLocalOrderBookValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config SpotLocalOrderBookConfig
		want   error
	}{
		{name: "invalid symbol", config: SpotLocalOrderBookConfig{Symbol: "btc/usd", Depth: 10, EgressRouteID: "route-a"}},
		{name: "invalid depth", config: SpotLocalOrderBookConfig{Symbol: "BTC/USD", Depth: 50, EgressRouteID: "route-a"}},
		{name: "missing route", config: SpotLocalOrderBookConfig{Symbol: "BTC/USD", Depth: 10}, want: trade.ErrMissingEgressRoute},
		{name: "invalid view", config: SpotLocalOrderBookConfig{Symbol: "BTC/USD", Depth: 10, ViewDepth: 11, EgressRouteID: "route-a"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewSpotLocalOrderBook(test.config)
			if err == nil {
				t.Fatal("NewSpotLocalOrderBook() error = nil")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("NewSpotLocalOrderBook() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestNewSpotLocalOrderBookUsesDefaults(t *testing.T) {
	t.Parallel()
	book, err := NewSpotLocalOrderBook(SpotLocalOrderBookConfig{
		Symbol: "BTC/USD", EgressRouteID: "route-a",
	})
	if err != nil || book.depth != 10 || book.viewDepth != 10 {
		t.Fatalf("NewSpotLocalOrderBook() = %+v, %v", book, err)
	}
}

func TestSpotLocalOrderBookRejectsInvalidEvents(t *testing.T) {
	t.Parallel()
	book := newTestSpotLocalOrderBook(t, 10, 10)
	timestamp := "2026-08-25T00:00:00Z"
	tests := []struct {
		name        string
		messageType string
		event       SpotStreamBook
	}{
		{name: "invalid type", messageType: "partial", event: spotLocalBookEvent(0, timestamp)},
		{name: "wrong symbol", messageType: "snapshot", event: SpotStreamBook{Symbol: "ETH/USD", Timestamp: timestamp}},
		{name: "invalid timestamp", messageType: "snapshot", event: spotLocalBookEvent(0, "now")},
		{name: "too many levels", messageType: "snapshot", event: SpotStreamBook{
			Symbol: "BTC/USD", Timestamp: timestamp,
			Bids: make([]SpotStreamBookLevel, 11),
		}},
		{name: "invalid price", messageType: "snapshot", event: SpotStreamBook{
			Symbol: "BTC/USD", Timestamp: timestamp,
			Bids: []SpotStreamBookLevel{spotLocalBookLevel("0", "1")},
		}},
		{name: "invalid quantity", messageType: "snapshot", event: SpotStreamBook{
			Symbol: "BTC/USD", Timestamp: timestamp,
			Asks: []SpotStreamBookLevel{spotLocalBookLevel("1", "-1")},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor := &spotLocalOrderBookProcessor{book: book}
			if _, _, err := processor.process(1, test.messageType, test.event); err == nil {
				t.Fatal("process() error = nil")
			}
		})
	}
}

func waitKrakenSpotLocalOrderBookView(
	t *testing.T,
	views <-chan SpotLocalOrderBookView,
) SpotLocalOrderBookView {
	t.Helper()
	select {
	case view := <-views:
		return view
	case <-time.After(time.Second):
		t.Fatal("Spot local order book view was not observed")
		return SpotLocalOrderBookView{}
	}
}
