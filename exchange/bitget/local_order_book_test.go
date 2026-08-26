package bitget

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

func TestStreamOrderBookDecodesCurrentSequenceFields(t *testing.T) {
	t.Parallel()
	var books []StreamOrderBook
	err := json.Unmarshal([]byte(
		`[{"a":[["101.0","2.0"]],"b":[["100.0","1.0"]],"pseq":"99","seq":100,"maxDepth":50,"ts":"2000"}]`,
	), &books)
	if err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(books) != 1 || books[0].PreviousSequence != 99 || books[0].Sequence != 100 ||
		books[0].MaxDepth != "50" || books[0].Timestamp != "2000" ||
		len(books[0].Asks) != 1 || len(books[0].Bids) != 1 {
		t.Fatalf("decoded books = %+v", books)
	}
}

func TestStreamOrderBookKeepsLegacyDepthCompatibility(t *testing.T) {
	t.Parallel()
	var book StreamOrderBook
	err := json.Unmarshal([]byte(
		`{"asks":[["101","2"]],"bids":[["100","1"]],"checksum":-123,"ts":2000}`,
	), &book)
	if err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(book.Asks) != 1 || len(book.Bids) != 1 || book.Checksum != -123 || book.Timestamp != "2000" {
		t.Fatalf("legacy book = %+v", book)
	}
}

func TestLocalOrderBookPublishesAndRecoversSequenceGaps(t *testing.T) {
	first := newWSTestConnection()
	second := newWSTestConnection()
	third := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{first, second, third}}
	client := newTestBitgetStreamClient(t, connector, nil, time.Now, nil)
	argument, err := PublicStreamArgument(CategorySpot, "books", "btcusdt")
	if err != nil {
		t.Fatalf("PublicStreamArgument() error = %v", err)
	}
	public, err := client.PublicStream(
		StreamRequest{Arguments: []StreamArgument{argument}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Symbol: "btcusdt", EgressRouteID: "route-b", ViewDepth: 2,
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	views := make(chan LocalOrderBookView, 8)
	done := make(chan error, 1)
	go func() {
		done <- book.Run(ctx, public, func(_ context.Context, view LocalOrderBookView) error {
			views <- view
			return nil
		})
	}()

	assertStreamOperation(t, waitForWSWrite(t, first), "subscribe", []StreamArgument{argument})
	sendLocalOrderBookMessage(first,
		`{"arg":{"instType":"spot","topic":"books","symbol":"BTCUSDT"},"action":"snapshot","data":[{"a":[["101.0","1"],["102","2"]],"b":[["100.00","1"],["99","2"]],"pseq":0,"seq":100,"maxDepth":"50","ts":"1000"}],"ts":1001}`,
	)
	view := waitBitgetLocalOrderBookView(t, views)
	if view.Symbol != "BTCUSDT" || view.Generation != 1 || view.SynchronizationID != 1 ||
		view.GapCount != 0 || view.Sequence != 100 || view.Timestamp != 1000 || view.MaxDepth != "50" {
		t.Fatalf("snapshot view = %+v", view)
	}

	sendLocalOrderBookMessage(first,
		`{"arg":{"instType":"spot","topic":"books","symbol":"BTCUSDT"},"action":"update","data":[{"a":[["101.00","3"]],"b":[["100.0","0"],["100.5","4"]],"pseq":99,"seq":101,"ts":"1002"}],"ts":1003}`,
	)
	view = waitBitgetLocalOrderBookView(t, views)
	wantBids := []BookLevel{{Price: "100.5", Quantity: "4"}, {Price: "99", Quantity: "2"}}
	wantAsks := []BookLevel{{Price: "101.00", Quantity: "3"}, {Price: "102", Quantity: "2"}}
	if view.Sequence != 101 || !slices.Equal(view.Bids, wantBids) || !slices.Equal(view.Asks, wantAsks) {
		t.Fatalf("bridged view = %+v, want bids=%v asks=%v", view, wantBids, wantAsks)
	}

	sendLocalOrderBookMessage(first,
		`{"arg":{"instType":"spot","topic":"books","symbol":"BTCUSDT"},"action":"update","data":[{"a":[],"b":[],"pseq":100,"seq":101,"ts":"1004"}],"ts":1004}`,
	)
	assertNoBitgetLocalOrderBookView(t, views)
	sendLocalOrderBookMessage(first,
		`{"arg":{"instType":"spot","topic":"books","symbol":"BTCUSDT"},"action":"update","data":[{"a":[],"b":[],"pseq":105,"seq":106,"ts":"1005"}],"ts":1005}`,
	)
	assertStreamOperation(t, waitForWSWrite(t, second), "subscribe", []StreamArgument{argument})
	sendLocalOrderBookMessage(second,
		`{"arg":{"instType":"spot","topic":"books","symbol":"BTCUSDT"},"action":"snapshot","data":[{"a":[["201","1"]],"b":[["200","1"]],"pseq":0,"seq":200,"maxDepth":"25","ts":"2000"}],"ts":2000}`,
	)
	view = waitBitgetLocalOrderBookView(t, views)
	if view.Generation != 2 || view.SynchronizationID != 2 || view.GapCount != 1 || view.Sequence != 200 {
		t.Fatalf("gap recovery snapshot = %+v", view)
	}
	sendLocalOrderBookMessage(second,
		`{"arg":{"instType":"spot","topic":"books","symbol":"BTCUSDT"},"action":"update","data":[{"a":[],"b":[["200.5","2"]],"pseq":0,"seq":201,"ts":"2001"}],"ts":2001}`,
	)
	view = waitBitgetLocalOrderBookView(t, views)
	if view.Sequence != 201 || view.GapCount != 1 {
		t.Fatalf("reset bridge view = %+v", view)
	}

	sendLocalOrderBookMessage(second,
		`{"arg":{"instType":"spot","topic":"books","symbol":"BTCUSDT"},"action":"update","data":[{"a":[],"b":[],"pseq":0,"seq":1,"ts":"2002"}],"ts":2002}`,
	)
	assertStreamOperation(t, waitForWSWrite(t, third), "subscribe", []StreamArgument{argument})
	sendLocalOrderBookMessage(third,
		`{"arg":{"instType":"spot","topic":"books","symbol":"BTCUSDT"},"action":"snapshot","data":[{"a":[["301","1"]],"b":[["300","1"]],"pseq":0,"seq":300,"maxDepth":"10","ts":"3000"}],"ts":3000}`,
	)
	view = waitBitgetLocalOrderBookView(t, views)
	if view.Generation != 3 || view.SynchronizationID != 3 || view.GapCount != 2 || view.Sequence != 300 {
		t.Fatalf("sequence reset recovery snapshot = %+v", view)
	}

	cancel()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("local order book Run() did not finish")
	}
	if err := public.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	routes, _ := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b", "route-b"}) {
		t.Fatalf("recovery routes = %v", routes)
	}
}

func TestLocalOrderBookResynchronizesOnNetworkReconnect(t *testing.T) {
	first := newWSTestConnection()
	second := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{first, second}}
	client := newTestBitgetStreamClient(t, connector, nil, time.Now, nil)
	argument, _ := PublicStreamArgument(CategorySpot, "books", "BTCUSDT")
	public, err := client.PublicStream(StreamRequest{Arguments: []StreamArgument{argument}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Symbol: "BTCUSDT", EgressRouteID: "route-a",
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	views := make(chan LocalOrderBookView, 4)
	done := make(chan error, 1)
	go func() {
		done <- book.Run(ctx, public, func(_ context.Context, view LocalOrderBookView) error {
			views <- view
			return nil
		})
	}()
	_ = waitForWSWrite(t, first)
	sendLocalOrderBookMessage(first,
		`{"arg":{"instType":"spot","topic":"books","symbol":"BTCUSDT"},"action":"snapshot","data":[{"a":[],"b":[],"pseq":0,"seq":100,"ts":"1000"}],"ts":1000}`,
	)
	_ = waitBitgetLocalOrderBookView(t, views)
	first.reads <- wsReadResult{err: errors.New("connection lost")}
	_ = waitForWSWrite(t, second)
	sendLocalOrderBookMessage(second,
		`{"arg":{"instType":"spot","topic":"books","symbol":"BTCUSDT"},"action":"snapshot","data":[{"a":[],"b":[],"pseq":0,"seq":200,"ts":"2000"}],"ts":2000}`,
	)
	view := waitBitgetLocalOrderBookView(t, views)
	if view.Generation != 2 || view.SynchronizationID != 2 || view.GapCount != 0 {
		t.Fatalf("network recovery view = %+v", view)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("local order book Run() did not finish")
	}
}

func TestLocalOrderBookRunValidatesRouteAndSubscription(t *testing.T) {
	t.Parallel()
	connector := &wsTestConnector{}
	client := newTestBitgetStreamClient(t, connector, nil, time.Now, nil)
	tests := []struct {
		name      string
		bookRoute transport.EgressRouteID
		topic     string
		want      string
	}{
		{name: "route mismatch", bookRoute: "route-b", topic: "books", want: "routes must match"},
		{name: "missing full depth", bookRoute: "route-a", topic: "books5", want: "required Spot books"},
		{name: "snapshot depth rejected", bookRoute: "route-a", topic: "books50", want: "required Spot books"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			argument, argumentErr := PublicStreamArgument(CategorySpot, test.topic, "BTCUSDT")
			if argumentErr != nil {
				t.Fatalf("PublicStreamArgument() error = %v", argumentErr)
			}
			public, publicErr := client.PublicStream(StreamRequest{Arguments: []StreamArgument{argument}})
			if publicErr != nil {
				t.Fatalf("PublicStream() error = %v", publicErr)
			}
			book, bookErr := NewLocalOrderBook(LocalOrderBookConfig{
				Symbol: "BTCUSDT", EgressRouteID: test.bookRoute,
			})
			if bookErr != nil {
				t.Fatalf("NewLocalOrderBook() error = %v", bookErr)
			}
			runErr := book.Run(context.Background(), public, func(context.Context, LocalOrderBookView) error {
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

func TestLocalOrderBookStopsOnSubscriptionRejection(t *testing.T) {
	connection := newWSTestConnection()
	connector := &wsTestConnector{connections: []*wsTestConnection{connection}}
	client := newTestBitgetStreamClient(t, connector, nil, time.Now, nil)
	argument, _ := PublicStreamArgument(CategorySpot, "books", "BTCUSDT")
	public, err := client.PublicStream(StreamRequest{Arguments: []StreamArgument{argument}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Symbol: "BTCUSDT", EgressRouteID: "route-a",
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
	sendLocalOrderBookMessage(connection, `{"event":"error","code":"30016","msg":"invalid channel"}`)
	select {
	case runErr := <-done:
		if runErr == nil || !strings.Contains(runErr.Error(), "30016: invalid channel") {
			t.Fatalf("Run() error = %v, want subscription rejection", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("subscription rejection did not stop local order book")
	}
}

func TestBitgetCurrentDepthTopicAndLocalOrderBookValidation(t *testing.T) {
	t.Parallel()
	argument, err := PublicStreamArgument(CategorySpot, "books50", "btcusdt")
	if err != nil {
		t.Fatalf("PublicStreamArgument(books50) error = %v", err)
	}
	if argument.Symbol != "BTCUSDT" {
		t.Fatalf("normalized symbol = %q, want BTCUSDT", argument.Symbol)
	}
	if _, err := PublicStreamArgument(CategorySpot, "books15", "BTCUSDT"); err == nil {
		t.Fatal("PublicStreamArgument(books15) error = nil")
	}
	if _, err := NewLocalOrderBook(LocalOrderBookConfig{Symbol: "BTCUSDT"}); !errors.Is(err, trade.ErrMissingEgressRoute) {
		t.Fatalf("NewLocalOrderBook() error = %v, want missing route", err)
	}
	if _, err := NewLocalOrderBook(LocalOrderBookConfig{
		Symbol: "BTCUSDT", EgressRouteID: "route-a", ViewDepth: 1001,
	}); err == nil {
		t.Fatal("NewLocalOrderBook(invalid depth) error = nil")
	}
}

func sendLocalOrderBookMessage(connection *wsTestConnection, payload string) {
	connection.reads <- wsReadResult{message: corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(payload),
	}}
}

func waitBitgetLocalOrderBookView(t *testing.T, views <-chan LocalOrderBookView) LocalOrderBookView {
	t.Helper()
	select {
	case view := <-views:
		return view
	case <-time.After(time.Second):
		t.Fatal("Bitget local order book view did not arrive")
		return LocalOrderBookView{}
	}
}

func assertNoBitgetLocalOrderBookView(t *testing.T, views <-chan LocalOrderBookView) {
	t.Helper()
	select {
	case view := <-views:
		t.Fatalf("unexpected Bitget local order book view = %+v", view)
	case <-time.After(20 * time.Millisecond):
	}
}
