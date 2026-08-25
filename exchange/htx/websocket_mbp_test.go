package htx

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type htxMBPRequest struct {
	Request string `json:"req"`
	ID      string `json:"id"`
}

func TestHTXMBPStreamReconnectsAndRequestsRefreshOnSelectedRoute(t *testing.T) {
	t.Parallel()

	first := newHTXWebSocketTestConnection()
	second := newHTXWebSocketTestConnection()
	connector := &htxWebSocketTestConnector{
		connections: []*htxWebSocketTestConnection{first, second},
	}
	client := newTestHTXMBPStreamClient(t, connector)
	subscription := StreamSubscription{
		Channel: StreamChannelMBP, Symbol: "btcusdt", MBPDepth: StreamMBPDepth20,
	}
	mbp, err := client.MBPStream(
		StreamRequest{Subscriptions: []StreamSubscription{subscription}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("MBPStream() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	updates := make(chan StreamMBPUpdate, 1)
	var snapshot StreamMBPSnapshot
	go func() {
		done <- mbp.Run(ctx, func(_ context.Context, message StreamMessage) error {
			if message.Channel != StreamChannelMBP {
				return nil
			}
			if message.Reply != "" {
				if err := message.Decode(&snapshot); err != nil {
					return err
				}
				cancel()
				return nil
			}
			if len(message.Tick) != 0 {
				var update StreamMBPUpdate
				if err := message.Decode(&update); err != nil {
					return err
				}
				updates <- update
			}
			return nil
		})
	}()
	firstCommand := decodeHTXStreamCommand(t, waitForHTXWebSocketWrite(t, first))
	assertHTXStreamCommand(t, firstCommand, "subscribe", "market.btcusdt.mbp.20")
	first.reads <- htxWebSocketReadResult{err: errors.New("connection lost")}
	secondCommand := decodeHTXStreamCommand(t, waitForHTXWebSocketWrite(t, second))
	assertHTXStreamCommand(t, secondCommand, "subscribe", "market.btcusdt.mbp.20")
	second.reads <- htxWebSocketReadResult{message: gzipHTXStreamMessage(t,
		`{"ch":"market.btcusdt.mbp.20","ts":1787627045000,"tick":{"seqNum":101,"prevSeqNum":100,"bids":[[64000,0.1]],"asks":[[64001,0.2]]}}`,
	)}
	select {
	case update := <-updates:
		if update.Sequence != 101 || update.PreviousSequence != 100 {
			t.Fatalf("update = %+v", update)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("MBP update was not delivered")
	}
	refreshID, err := mbp.RequestSnapshot(ctx, subscription)
	if err != nil {
		t.Fatalf("RequestSnapshot() error = %v", err)
	}
	request := decodeHTXMBPRequest(t, waitForHTXWebSocketWrite(t, second))
	if request.ID != refreshID || request.Request != "market.btcusdt.mbp.20" {
		t.Fatalf("refresh request = %+v, returned ID = %q", request, refreshID)
	}
	rateContext, rateCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer rateCancel()
	if _, err := mbp.RequestSnapshot(rateContext, subscription); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("rate-limited RequestSnapshot() error = %v, want deadline exceeded", err)
	}
	second.reads <- htxWebSocketReadResult{message: gzipHTXStreamMessage(t,
		`{"id":"`+refreshID+`","rep":"market.btcusdt.mbp.20","status":"ok","data":{"seqNum":101,"bids":[[64000,0.1]],"asks":[[64001,0.2]]}}`,
	)}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("MBP Run() did not finish")
	}
	if snapshot.Sequence != 101 || len(snapshot.Bids) != 1 || snapshot.Bids[0].Price != "64000" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if mbp.Generation() != 2 || mbp.EgressRouteID() != "route-b" {
		t.Fatalf("generation = %d, route = %q", mbp.Generation(), mbp.EgressRouteID())
	}
	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-b", "route-b"}) ||
		len(requests) != 2 {
		t.Fatalf("routes = %v, requests = %d", routes, len(requests))
	}
	for _, dialRequest := range requests {
		if dialRequest.Endpoint != "ws://stream.example.test/feed" {
			t.Fatalf("dial request = %+v", dialRequest)
		}
	}
	_ = mbp.Close()
}

func TestHTXDecodeMBPStreamMessage(t *testing.T) {
	t.Parallel()

	update, err := DecodeStreamMessage(gzipHTXStreamMessage(t,
		`{"ch":"market.btcusdt.mbp.150","ts":1787627045000,"tick":{"seqNum":101,"prevSeqNum":100,"bids":[],"asks":[]}}`,
	))
	if err != nil {
		t.Fatalf("DecodeStreamMessage(update) error = %v", err)
	}
	var updateEvent StreamMBPUpdate
	if update.Channel != StreamChannelMBP || update.MBPDepth != StreamMBPDepth150 ||
		update.Symbol != "btcusdt" || update.Decode(&updateEvent) != nil ||
		updateEvent.Sequence != 101 || updateEvent.PreviousSequence != 100 {
		t.Fatalf("update = %+v, event = %+v", update, updateEvent)
	}
	snapshot, err := DecodeStreamMessage(gzipHTXStreamMessage(t,
		`{"id":"refresh-1","rep":"market.btcusdt.mbp.20","status":"ok","data":{"seqNum":100,"bids":[[99,2]],"asks":[[102,2]]}}`,
	))
	if err != nil {
		t.Fatalf("DecodeStreamMessage(snapshot) error = %v", err)
	}
	var snapshotEvent StreamMBPSnapshot
	if snapshot.Reply == "" || snapshot.MBPDepth != StreamMBPDepth20 ||
		snapshot.Decode(&snapshotEvent) != nil || snapshotEvent.Sequence != 100 {
		t.Fatalf("snapshot = %+v, event = %+v", snapshot, snapshotEvent)
	}
	invalid := []string{
		`{"id":"refresh-1","rep":"market.btcusdt.mbp.20","status":"ok"}`,
		`{"id":"refresh-1","rep":"market.btcusdt.ticker","status":"ok","data":{}}`,
		`{"ch":"market.btcusdt.mbp.10","ts":1,"tick":{}}`,
	}
	for _, payload := range invalid {
		if _, err := DecodeStreamMessage(gzipHTXStreamMessage(t, payload)); err == nil {
			t.Fatalf("DecodeStreamMessage(%s) error = nil", payload)
		}
	}
}

func TestHTXMBPStreamValidation(t *testing.T) {
	t.Parallel()

	client := newTestHTXMBPStreamClient(t, &htxWebSocketTestConnector{})
	invalid := []StreamSubscription{
		{Channel: StreamChannelMBP, Symbol: "BTCUSDT", MBPDepth: StreamMBPDepth20},
		{Channel: StreamChannelMBP, Symbol: "btcusdt", MBPDepth: 10},
		{Channel: StreamChannelTicker, Symbol: "btcusdt", MBPDepth: StreamMBPDepth20},
		{Channel: StreamChannelMBP, Symbol: "btcusdt", MBPDepth: StreamMBPDepth20, Mode: 1},
	}
	for _, subscription := range invalid {
		_, err := client.MBPStream(StreamRequest{Subscriptions: []StreamSubscription{subscription}})
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("MBPStream(%+v) error = %v, want validation", subscription, err)
		}
	}
	mbp, err := client.MBPStream(StreamRequest{Subscriptions: []StreamSubscription{{
		Channel: StreamChannelMBP, Symbol: "btcusdt", MBPDepth: StreamMBPDepth400,
	}}})
	if err != nil {
		t.Fatalf("MBPStream() error = %v", err)
	}
	_, err = mbp.RequestSnapshot(context.Background(), StreamSubscription{
		Channel: StreamChannelMBP, Symbol: "btcusdt", MBPDepth: StreamMBPDepth400,
	})
	if !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("RequestSnapshot() error = %v, want validation", err)
	}
}

func newTestHTXMBPStreamClient(
	t *testing.T,
	connector corestream.Connector,
) *StreamClient {
	t.Helper()
	client, err := NewStreamClient(StreamClientConfig{
		Connector: connector, DefaultEgressRouteID: "route-a",
		MBPWebSocketURL: "ws://stream.example.test/feed", AllowInsecureWebSocket: true,
		Backoff: func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("NewStreamClient() error = %v", err)
	}
	return client
}

func decodeHTXMBPRequest(t *testing.T, message corestream.Message) htxMBPRequest {
	t.Helper()
	if message.Type != corestream.MessageText {
		t.Fatalf("refresh frame type = %d, want text", message.Type)
	}
	var request htxMBPRequest
	if err := json.Unmarshal(message.Data, &request); err != nil {
		t.Fatalf("decode refresh request: %v", err)
	}
	if request.ID == "" {
		t.Fatal("refresh request ID is empty")
	}
	return request
}
