package kucoin

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

func newTestKuCoinLocalOrderBook(t *testing.T, viewDepth int) *LocalOrderBook {
	t.Helper()
	book, err := NewLocalOrderBook(LocalOrderBookConfig{
		Symbol: "BTC-USDT", EgressRouteID: "route-b", ViewDepth: viewDepth,
	})
	if err != nil {
		t.Fatalf("NewLocalOrderBook() error = %v", err)
	}
	return book
}

func kuCoinProBookMessage(updateType string, start, end int64) ProOrderBookMessage {
	return ProOrderBookMessage{
		Topic: proOrderBookTopic, Depth: proOrderBookDepth, UpdateType: updateType,
		PublishTime: 2_000 + end,
		Data: ProOrderBookEvent{
			SequenceStart: start, SequenceEnd: end, MatchingTime: 1_000 + end,
			Symbol: "BTC-USDT",
		},
	}
}

func TestLocalOrderBookSnapshotAndOverlappingDelta(t *testing.T) {
	book := newTestKuCoinLocalOrderBook(t, 2)
	processor := &localOrderBookProcessor{book: book}
	snapshot := kuCoinProBookMessage("snapshot", 100, 100)
	snapshot.Data.Bids = []BookLevel{{Price: "99", Size: "2"}, {Price: "100.0", Size: "1"}, {Price: "98", Size: "3"}}
	snapshot.Data.Asks = []BookLevel{{Price: "102", Size: "2"}, {Price: "101.0", Size: "1"}}

	view, publish, reconnect, err := processor.process(snapshot, 1)
	if err != nil || !publish || reconnect {
		t.Fatalf("snapshot process = %+v, %v, %v, %v", view, publish, reconnect, err)
	}
	if view.Symbol != "BTC-USDT" || view.Generation != 1 || view.SynchronizationID != 1 ||
		view.Sequence != 100 || view.MatchingTime != 1_100 || view.PublishTime != 2_100 ||
		view.GapCount != 0 ||
		!slices.Equal(view.Bids, []BookLevel{{Price: "100.0", Size: "1"}, {Price: "99", Size: "2"}}) ||
		!slices.Equal(view.Asks, []BookLevel{{Price: "101.0", Size: "1"}, {Price: "102", Size: "2"}}) {
		t.Fatalf("snapshot view = %+v", view)
	}

	delta := kuCoinProBookMessage("delta", 99, 102)
	delta.Data.Bids = []BookLevel{{Price: "100.00", Size: "3"}, {Price: "99", Size: "0"}, {Price: "100.5", Size: "4"}}
	delta.Data.Asks = []BookLevel{{Price: "101", Size: "0"}, {Price: "100.75", Size: "5"}}
	view, publish, reconnect, err = processor.process(delta, 1)
	if err != nil || !publish || reconnect {
		t.Fatalf("delta process = %+v, %v, %v, %v", view, publish, reconnect, err)
	}
	if view.Sequence != 102 ||
		!slices.Equal(view.Bids, []BookLevel{{Price: "100.5", Size: "4"}, {Price: "100.00", Size: "3"}}) ||
		!slices.Equal(view.Asks, []BookLevel{{Price: "100.75", Size: "5"}, {Price: "102", Size: "2"}}) {
		t.Fatalf("delta view = %+v", view)
	}

	view, publish, reconnect, err = processor.process(delta, 1)
	if err != nil || publish || reconnect || view.Sequence != 0 {
		t.Fatalf("old delta process = %+v, %v, %v, %v", view, publish, reconnect, err)
	}
}

func TestLocalOrderBookDetectsSequenceGapAndRequiresSnapshot(t *testing.T) {
	book := newTestKuCoinLocalOrderBook(t, 20)
	processor := &localOrderBookProcessor{book: book}
	snapshot := kuCoinProBookMessage("snapshot", 100, 100)
	snapshot.Data.Bids = []BookLevel{{Price: "100", Size: "1"}}
	if _, _, _, err := processor.process(snapshot, 1); err != nil {
		t.Fatalf("snapshot process error = %v", err)
	}

	gap := kuCoinProBookMessage("delta", 102, 102)
	gap.Data.Bids = []BookLevel{{Price: "100", Size: "2"}}
	view, publish, reconnect, err := processor.process(gap, 1)
	if err != nil || publish || !reconnect || view.Sequence != 0 ||
		processor.state != nil || processor.gapCount != 1 {
		t.Fatalf("gap process = %+v, %v, %v, %v, processor=%+v", view, publish, reconnect, err, processor)
	}

	nextGeneration := kuCoinProBookMessage("delta", 200, 200)
	nextGeneration.Data.Asks = []BookLevel{{Price: "101", Size: "1"}}
	_, publish, reconnect, err = processor.process(nextGeneration, 2)
	if err != nil || publish || !reconnect || processor.gapCount != 2 {
		t.Fatalf("new generation delta = %v, %v, %v, processor=%+v", publish, reconnect, err, processor)
	}

	recovered := kuCoinProBookMessage("snapshot", 300, 300)
	recovered.Data.Bids = []BookLevel{{Price: "90", Size: "3"}}
	view, publish, reconnect, err = processor.process(recovered, 3)
	if err != nil || !publish || reconnect || view.Generation != 3 ||
		view.SynchronizationID != 2 || view.Sequence != 300 || view.GapCount != 2 {
		t.Fatalf("recovered snapshot = %+v, %v, %v, %v", view, publish, reconnect, err)
	}
}

func TestLocalOrderBookPrunesDeltaToBestFiveHundred(t *testing.T) {
	book := newTestKuCoinLocalOrderBook(t, 500)
	processor := &localOrderBookProcessor{book: book}
	snapshot := kuCoinProBookMessage("snapshot", 1, 1)
	for index := 1; index <= 500; index++ {
		snapshot.Data.Bids = append(snapshot.Data.Bids, BookLevel{Price: strconv.Itoa(index), Size: "1"})
	}
	if _, _, _, err := processor.process(snapshot, 1); err != nil {
		t.Fatalf("snapshot process error = %v", err)
	}
	delta := kuCoinProBookMessage("delta", 2, 2)
	delta.Data.Bids = []BookLevel{{Price: "501", Size: "1"}}
	view, publish, reconnect, err := processor.process(delta, 1)
	if err != nil || !publish || reconnect || len(view.Bids) != 500 ||
		view.Bids[0].Price != "501" || view.Bids[len(view.Bids)-1].Price != "2" {
		t.Fatalf("pruned view size = %d, first = %+v, last = %+v, publish=%v reconnect=%v err=%v",
			len(view.Bids), view.Bids[0], view.Bids[len(view.Bids)-1], publish, reconnect, err)
	}
}

func TestLocalOrderBookRunReconnectsOnSameRouteAfterGap(t *testing.T) {
	first := newKuCoinWebSocketTestConnection()
	second := newKuCoinWebSocketTestConnection()
	connector := &kucoinWebSocketTestConnector{
		connections: []*kucoinWebSocketTestConnection{first, second},
	}
	tokenServer := newKuCoinWebSocketTokenServer(t)
	defer tokenServer.Close()
	client, sender := newTestKuCoinStreamClient(t, tokenServer.URL, connector)
	stream, err := client.ProOrderBookStream("BTC-USDT", trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("ProOrderBookStream() error = %v", err)
	}
	book := newTestKuCoinLocalOrderBook(t, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	views := make(chan LocalOrderBookView, 4)
	done := make(chan error, 1)
	go func() {
		done <- book.Run(ctx, stream, func(_ context.Context, view LocalOrderBookView) error {
			views <- view
			if view.SynchronizationID == 2 {
				cancel()
			}
			return nil
		})
	}()

	assertProOrderBookSubscription(t, waitForKuCoinWebSocketWrite(t, first), "BTC-USDT")
	sendKuCoinProBookMessage(first,
		`{"T":"obu.SPOT","dp":"increment@10ms","t":"snapshot","P":1000,"d":{"O":100,"C":100,"M":999,"s":"BTC-USDT","a":[["102","2"],["101","1"]],"b":[["99","2"],["100","1"]]}}`)
	initial := waitKuCoinLocalOrderBookView(t, views)
	if initial.Generation != 1 || initial.Sequence != 100 || initial.SynchronizationID != 1 ||
		initial.Bids[0].Price != "100" || initial.Asks[0].Price != "101" {
		t.Fatalf("initial view = %+v", initial)
	}

	sendKuCoinProBookMessage(first,
		`{"T":"obu.SPOT","dp":"increment@10ms","t":"delta","P":1002,"d":{"O":101,"C":102,"M":1001,"s":"BTC-USDT","a":[["101","0"],["100.5","4"]],"b":[["100","3"]]}}`)
	updated := waitKuCoinLocalOrderBookView(t, views)
	if updated.Sequence != 102 || updated.Bids[0].Size != "3" || updated.Asks[0].Price != "100.5" {
		t.Fatalf("updated view = %+v", updated)
	}

	sendKuCoinProBookMessage(first,
		`{"T":"obu.SPOT","dp":"increment@10ms","t":"delta","P":1005,"d":{"O":105,"C":105,"M":1004,"s":"BTC-USDT","a":[["103","1"]],"b":[]}}`)
	assertProOrderBookSubscription(t, waitForKuCoinWebSocketWrite(t, second), "BTC-USDT")
	sendKuCoinProBookMessage(second,
		`{"T":"obu.SPOT","dp":"increment@10ms","t":"snapshot","P":2000,"d":{"O":200,"C":200,"M":1999,"s":"BTC-USDT","a":[["91","3"]],"b":[["90","2"]]}}`)
	recovered := waitKuCoinLocalOrderBookView(t, views)
	if recovered.Generation != 2 || recovered.Sequence != 200 ||
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
		len(requests) != 2 || requests[0].Endpoint != "ws://pro.example.test" ||
		requests[1].Endpoint != requests[0].Endpoint {
		t.Fatalf("local order book reconnect routes = %v, requests = %+v", routes, requests)
	}
	if senderRoutes := sender.snapshot(); len(senderRoutes) != 0 {
		t.Fatalf("token REST sender routes = %v, want none", senderRoutes)
	}
}

func TestLocalOrderBookRunValidatesRouteAndSymbol(t *testing.T) {
	t.Parallel()
	connector := &kucoinWebSocketTestConnector{}
	client, _ := newTestKuCoinStreamClient(t, "http://rest.example.test", connector)
	stream, err := client.ProOrderBookStream("BTC-USDT", trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("ProOrderBookStream() error = %v", err)
	}
	tests := []struct {
		name      string
		symbol    string
		route     transport.EgressRouteID
		wantError string
	}{
		{name: "route mismatch", symbol: "BTC-USDT", route: "route-a", wantError: "routes must match"},
		{name: "symbol mismatch", symbol: "ETH-USDT", route: "route-b", wantError: "symbols must match"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			book, bookErr := NewLocalOrderBook(LocalOrderBookConfig{
				Symbol: test.symbol, EgressRouteID: test.route,
			})
			if bookErr != nil {
				t.Fatalf("NewLocalOrderBook() error = %v", bookErr)
			}
			runErr := book.Run(context.Background(), stream, func(context.Context, LocalOrderBookView) error {
				return nil
			})
			if runErr == nil || !strings.Contains(runErr.Error(), test.wantError) {
				t.Fatalf("Run() error = %v, want text %q", runErr, test.wantError)
			}
		})
	}
	if routes, requests := connector.snapshot(); len(routes) != 0 || len(requests) != 0 {
		t.Fatalf("connector was called during validation: routes=%v requests=%v", routes, requests)
	}
}

func TestLocalOrderBookRunStopsOnSubscriptionError(t *testing.T) {
	connection := newKuCoinWebSocketTestConnection()
	connector := &kucoinWebSocketTestConnector{
		connections: []*kucoinWebSocketTestConnection{connection},
	}
	client, _ := newTestKuCoinStreamClient(t, "http://rest.example.test", connector)
	stream, err := client.ProOrderBookStream("BTC-USDT", trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("ProOrderBookStream() error = %v", err)
	}
	book := newTestKuCoinLocalOrderBook(t, 20)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- book.Run(ctx, stream, func(context.Context, LocalOrderBookView) error { return nil })
	}()
	_ = waitForKuCoinWebSocketWrite(t, connection)
	connection.reads <- kucoinWebSocketReadResult{message: corestream.Message{Data: []byte(
		`{"id":"1","type":"error","code":400100,"msg":"invalid symbol"}`,
	)}}
	select {
	case runErr := <-done:
		if runErr == nil || !strings.Contains(runErr.Error(), "400100") ||
			!strings.Contains(runErr.Error(), "invalid symbol") {
			t.Fatalf("Run() error = %v, want subscription error", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("subscription error did not stop local order book")
	}
}

func TestNewLocalOrderBookValidationAndDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config LocalOrderBookConfig
		want   error
	}{
		{name: "invalid symbol", config: LocalOrderBookConfig{Symbol: "BTCBTC", EgressRouteID: "route-a"}},
		{name: "missing route", config: LocalOrderBookConfig{Symbol: "BTC-USDT"}, want: trade.ErrMissingEgressRoute},
		{name: "negative view", config: LocalOrderBookConfig{Symbol: "BTC-USDT", EgressRouteID: "route-a", ViewDepth: -1}},
		{name: "oversized view", config: LocalOrderBookConfig{Symbol: "BTC-USDT", EgressRouteID: "route-a", ViewDepth: 501}},
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
		Symbol: "btc-usdt", EgressRouteID: " route-a ",
	})
	if err != nil || book.symbol != "BTC-USDT" || book.routeID != "route-a" ||
		book.viewDepth != defaultLocalOrderBookViewDepth {
		t.Fatalf("NewLocalOrderBook() defaults = %+v, %v", book, err)
	}
}

func TestLocalOrderBookRejectsInvalidEvents(t *testing.T) {
	t.Parallel()
	book := newTestKuCoinLocalOrderBook(t, 20)
	base := kuCoinProBookMessage("snapshot", 1, 1)
	base.Data.Bids = []BookLevel{{Price: "100", Size: "1"}}
	tests := []struct {
		name    string
		message ProOrderBookMessage
	}{
		{name: "wrong topic", message: func() ProOrderBookMessage { value := base; value.Topic = "obu.FUTURES"; return value }()},
		{name: "wrong depth", message: func() ProOrderBookMessage { value := base; value.Depth = "increment@100ms"; return value }()},
		{name: "wrong symbol", message: func() ProOrderBookMessage { value := base; value.Data.Symbol = "ETH-USDT"; return value }()},
		{name: "invalid publish time", message: func() ProOrderBookMessage { value := base; value.PublishTime = 0; return value }()},
		{name: "invalid matching time", message: func() ProOrderBookMessage { value := base; value.Data.MatchingTime = 0; return value }()},
		{name: "invalid sequence", message: func() ProOrderBookMessage { value := base; value.Data.SequenceStart = 0; return value }()},
		{name: "snapshot range", message: kuCoinProBookMessage("snapshot", 1, 2)},
		{name: "empty snapshot", message: kuCoinProBookMessage("snapshot", 1, 1)},
		{name: "zero snapshot quantity", message: func() ProOrderBookMessage {
			value := base
			value.Data.Bids = []BookLevel{{Price: "100", Size: "0"}}
			return value
		}()},
		{name: "negative quantity", message: func() ProOrderBookMessage {
			value := base
			value.Data.Bids = []BookLevel{{Price: "100", Size: "-1"}}
			return value
		}()},
		{name: "invalid price", message: func() ProOrderBookMessage {
			value := base
			value.Data.Bids = []BookLevel{{Price: "zero", Size: "1"}}
			return value
		}()},
		{name: "duplicate canonical price", message: func() ProOrderBookMessage {
			value := base
			value.Data.Bids = []BookLevel{{Price: "100", Size: "1"}, {Price: "100.0", Size: "2"}}
			return value
		}()},
		{name: "empty delta", message: kuCoinProBookMessage("delta", 2, 2)},
		{name: "invalid update type", message: func() ProOrderBookMessage { value := base; value.UpdateType = "replace"; return value }()},
	}
	tooMany := base
	tooMany.Data.Bids = make([]BookLevel, 501)
	for index := range tooMany.Data.Bids {
		tooMany.Data.Bids[index] = BookLevel{Price: strconv.Itoa(index + 1), Size: "1"}
	}
	tests = append(tests, struct {
		name    string
		message ProOrderBookMessage
	}{name: "too many levels", message: tooMany})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor := &localOrderBookProcessor{book: book}
			if _, _, _, err := processor.process(test.message, 1); err == nil {
				t.Fatal("process() error = nil")
			}
		})
	}
}

func sendKuCoinProBookMessage(connection *kucoinWebSocketTestConnection, payload string) {
	connection.reads <- kucoinWebSocketReadResult{message: corestream.Message{
		Type: corestream.MessageText, Data: []byte(payload),
	}}
}

func waitKuCoinLocalOrderBookView(t *testing.T, views <-chan LocalOrderBookView) LocalOrderBookView {
	t.Helper()
	select {
	case view := <-views:
		return view
	case <-time.After(time.Second):
		t.Fatal("local order book view was not observed")
		return LocalOrderBookView{}
	}
}
