package bithumb

import (
	"context"
	"errors"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

const twapReferenceSample = `{"algo_order_id":"TWAP-A01B02C03D04E05F06"}`

const twapListSample = `{"has_next":true,"next_key":"NDMyMjM2fEdCRVQtS1JXfDYyNDc3YjYxLWEwZjItNDY1OC04ZGVhLTFkMjQyYjIxZGFmZQ==","orders":[` +
	`{"uuid":"TWAP-001-PROGRESS-BID","side":"bid","price":"92500000","state":"progress","market":"KRW-BTC",` +
	`"created_at":"2025-12-04T10:00:00+09:00","volume":"1.0","total_order_count":60,"total_trades_count":10,` +
	`"progress_count":25,"total_executed_amount":"2312500000","total_executed_volume":"0.25",` +
	`"avg_trade_price":"92500000.000","wallet_id":"0000000000-00-0000"},` +
	`{"uuid":"TWAP-002-CANCEL-ASK","side":"ask","price":"5000","state":"cancel","market":"KRW-XRP",` +
	`"created_at":"2025-12-03T09:00:00+09:00","volume":"1000","total_order_count":120,"total_trades_count":5,` +
	`"progress_count":15,"total_executed_amount":"25000000","total_executed_volume":"5000","avg_trade_price":"5000.0",` +
	`"canceled_at":"2025-12-03T09:15:00+09:00","cancel_type":"user"}]}`

func TestPlaceTWAPOrder(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{extOK("POST /v1/twap", twapReferenceSample)}, extTrader)
	ctx := context.Background()

	bid, err := fixture.client.PlaceTWAPOrder(ctx, PlaceTWAPOrderRequest{
		Market: "KRW-BTC", Side: SideBid, Price: "1000000", Duration: time.Hour, Frequency: 30 * time.Second,
	})
	if err != nil || bid.AlgoOrderID != "TWAP-A01B02C03D04E05F06" || len(bid.Raw) == 0 {
		t.Fatalf("PlaceTWAPOrder(bid) = %+v, error = %v", bid, err)
	}
	ask, err := fixture.client.PlaceTWAPOrder(ctx, PlaceTWAPOrderRequest{
		Market: "KRW-BTC", Side: SideAsk, Volume: "0.5", Duration: 300 * time.Second, Frequency: 2 * time.Minute,
	})
	if err != nil || ask.AlgoOrderID != "TWAP-A01B02C03D04E05F06" {
		t.Fatalf("PlaceTWAPOrder(ask) = %+v, error = %v", ask, err)
	}

	records := extRequests(t, fixture.server, 2)
	assertExtRequest(
		t, records[0], "POST", "/v1/twap", "",
		`{"market":"KRW-BTC","side":"bid","price":"1000000","duration":"3600","frequency":"30"}`,
		"market=KRW-BTC&side=bid&price=1000000&duration=3600&frequency=30",
	)
	assertExtRequest(
		t, records[1], "POST", "/v1/twap", "",
		`{"market":"KRW-BTC","side":"ask","volume":"0.5","duration":"300","frequency":"120"}`,
		"market=KRW-BTC&side=ask&volume=0.5&duration=300&frequency=120",
	)
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:order-create:1second", 140, 2)
}

func TestTWAPOrders(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{extOK("GET /v1/twap", twapListSample)}, extReadOnly)
	ctx := context.Background()

	page, err := fixture.client.TWAPOrders(ctx, TWAPOrdersRequest{
		Market: "KRW-BTC", State: TWAPStateProgress, UUIDs: []string{"TWAP-A", "TWAP-B"},
		Limit: 50, OrderBy: "desc", NextKey: "abc+/=",
	})
	if err != nil || !page.HasNext || len(page.Orders) != 2 || len(page.Raw) == 0 ||
		page.NextKey != "NDMyMjM2fEdCRVQtS1JXfDYyNDc3YjYxLWEwZjItNDY1OC04ZGVhLTFkMjQyYjIxZGFmZQ==" {
		t.Fatalf("TWAPOrders() = %+v, error = %v", page, err)
	}
	progress, canceled := page.Orders[0], page.Orders[1]
	if progress.UUID != "TWAP-001-PROGRESS-BID" || progress.Side != SideBid || progress.Price != "92500000" ||
		progress.State != TWAPStateProgress || progress.Market != "KRW-BTC" || progress.Volume != "1.0" ||
		progress.TotalOrderCount != 60 || progress.TotalTradesCount != 10 || progress.ProgressCount != 25 ||
		progress.TotalExecutedAmount != "2312500000" || progress.TotalExecutedVolume != "0.25" ||
		progress.AvgTradePrice != "92500000.000" || progress.WalletID != "0000000000-00-0000" ||
		progress.CanceledAt != "" || progress.CancelType != "" || len(progress.Raw) == 0 {
		t.Fatalf("progress TWAP order = %+v", progress)
	}
	if canceled.UUID != "TWAP-002-CANCEL-ASK" || canceled.Side != SideAsk || canceled.State != TWAPStateCancel ||
		canceled.CanceledAt != "2025-12-03T09:15:00+09:00" || canceled.CancelType != "user" ||
		canceled.WalletID != "" || len(canceled.Raw) == 0 {
		t.Fatalf("canceled TWAP order = %+v", canceled)
	}

	if _, err := fixture.client.TWAPOrders(ctx, TWAPOrdersRequest{}); err != nil {
		t.Fatalf("TWAPOrders(empty) error = %v", err)
	}

	records := extRequests(t, fixture.server, 2)
	assertExtRequest(
		t, records[0], "GET", "/v1/twap",
		"market=KRW-BTC&state=progress&uuids[]=TWAP-A&uuids[]=TWAP-B&limit=50&order_by=desc&next_key=abc%2B%2F%3D", "",
		"market=KRW-BTC&state=progress&uuids[]=TWAP-A&uuids[]=TWAP-B&limit=50&order_by=desc&next_key=abc+/=",
	)
	assertExtRequest(t, records[1], "GET", "/v1/twap", "", "", "")
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:private-other:1second", 140, 2)
}

func TestCancelTWAPOrder(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{extOK("DELETE /v1/twap", twapReferenceSample)}, extTrader)
	reference, err := fixture.client.CancelTWAPOrder(context.Background(), CancelTWAPOrderRequest{
		AlgoOrderID: "TWAP-A01B02C03D04E05F06",
	})
	if err != nil || reference.AlgoOrderID != "TWAP-A01B02C03D04E05F06" || len(reference.Raw) == 0 {
		t.Fatalf("CancelTWAPOrder() = %+v, error = %v", reference, err)
	}
	records := extRequests(t, fixture.server, 1)
	assertExtRequest(
		t, records[0], "DELETE", "/v1/twap", "algo_order_id=TWAP-A01B02C03D04E05F06", "",
		"algo_order_id=TWAP-A01B02C03D04E05F06",
	)
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:order-cancel:1second", 140, 1)
}

func TestTWAPPermissionsAndUnknownExecutionState(t *testing.T) {
	t.Parallel()

	placeRequest := PlaceTWAPOrderRequest{
		Market: "KRW-BTC", Side: SideAsk, Volume: "1", Duration: 10 * time.Minute, Frequency: time.Minute,
	}
	cancelRequest := CancelTWAPOrderRequest{AlgoOrderID: "TWAP-A01B02C03D04E05F06"}
	ctx := context.Background()

	readOnly := newExtFixture(t, []extRoute{
		extOK("POST /v1/twap", twapReferenceSample),
		extOK("DELETE /v1/twap", twapReferenceSample),
		extOK("GET /v1/twap", twapListSample),
	}, extReadOnly)
	_, placeErr := readOnly.client.PlaceTWAPOrder(ctx, placeRequest)
	_, cancelErr := readOnly.client.CancelTWAPOrder(ctx, cancelRequest)
	for name, err := range map[string]error{"place": placeErr, "cancel": cancelErr} {
		if !errors.Is(err, trade.ErrAuthorization) || !errors.Is(err, credential.ErrPermissionNotAllowed) {
			t.Errorf("%s error = %v, want ErrAuthorization from missing trade permission", name, err)
		}
	}
	if _, err := readOnly.client.TWAPOrders(ctx, TWAPOrdersRequest{}); err != nil {
		t.Fatalf("TWAPOrders() with read permission error = %v", err)
	}
	extRequests(t, readOnly.server, 1)

	serverError := `{"error":{"name":"internal_error","message":"boom"}}`
	failing := newExtFixture(t, []extRoute{
		extReplies("POST /v1/twap", extStatus(500, serverError)),
		extReplies("DELETE /v1/twap", extStatus(500, serverError)),
	}, extTrader)
	if _, err := failing.client.PlaceTWAPOrder(ctx, placeRequest); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceTWAPOrder() 5xx error = %v, want ErrUnknownExecutionState", err)
	}
	if _, err := failing.client.CancelTWAPOrder(ctx, cancelRequest); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CancelTWAPOrder() 5xx error = %v, want ErrUnknownExecutionState", err)
	}
	extRequests(t, failing.server, 2)
}

func TestTWAPRequestsRejectInvalidInput(t *testing.T) {
	t.Parallel()

	ask := PlaceTWAPOrderRequest{Market: "KRW-BTC", Side: SideAsk, Volume: "1", Duration: 10 * time.Minute, Frequency: time.Minute}
	bid := PlaceTWAPOrderRequest{Market: "KRW-BTC", Side: SideBid, Price: "1000", Duration: 10 * time.Minute, Frequency: time.Minute}
	change := func(base PlaceTWAPOrderRequest, mutate func(*PlaceTWAPOrderRequest)) PlaceTWAPOrderRequest {
		mutate(&base)
		return base
	}

	invalid := []struct {
		name string
		err  error
	}{
		{"lowercase market", change(ask, func(r *PlaceTWAPOrderRequest) { r.Market = "krw-btc" }).validate()},
		{"unknown side", change(ask, func(r *PlaceTWAPOrderRequest) { r.Side = "buy" }).validate()},
		{"ask without volume", change(ask, func(r *PlaceTWAPOrderRequest) { r.Volume = "" }).validate()},
		{"bid without price", change(bid, func(r *PlaceTWAPOrderRequest) { r.Price = "" }).validate()},
		{"zero volume", change(ask, func(r *PlaceTWAPOrderRequest) { r.Volume = "0" }).validate()},
		{"negative price", change(bid, func(r *PlaceTWAPOrderRequest) { r.Price = "-1" }).validate()},
		{"bid with invalid optional volume", change(bid, func(r *PlaceTWAPOrderRequest) { r.Volume = "abc" }).validate()},
		{"ask with invalid optional price", change(ask, func(r *PlaceTWAPOrderRequest) { r.Price = "0.0" }).validate()},
		{"zero duration", change(ask, func(r *PlaceTWAPOrderRequest) { r.Duration = 0 }).validate()},
		{"duration below minimum", change(ask, func(r *PlaceTWAPOrderRequest) { r.Duration = 299 * time.Second }).validate()},
		{"duration above maximum", change(ask, func(r *PlaceTWAPOrderRequest) { r.Duration = 43201 * time.Second }).validate()},
		{"fractional duration", change(ask, func(r *PlaceTWAPOrderRequest) { r.Duration = 301*time.Second + 500*time.Millisecond }).validate()},
		{"zero frequency", change(ask, func(r *PlaceTWAPOrderRequest) { r.Frequency = 0 }).validate()},
		{"unsupported frequency", change(ask, func(r *PlaceTWAPOrderRequest) { r.Frequency = 45 * time.Second }).validate()},
		{"fractional frequency", change(ask, func(r *PlaceTWAPOrderRequest) { r.Frequency = 15*time.Second + time.Millisecond }).validate()},
		{"orders invalid market", (TWAPOrdersRequest{Market: "krw"}).validate()},
		{"orders unknown state", (TWAPOrdersRequest{State: "running"}).validate()},
		{"orders limit above maximum", (TWAPOrdersRequest{Limit: 101}).validate()},
		{"orders negative limit", (TWAPOrdersRequest{Limit: -1}).validate()},
		{"orders invalid order by", (TWAPOrdersRequest{OrderBy: "up"}).validate()},
		{"orders next key with ampersand", (TWAPOrdersRequest{NextKey: "a&b"}).validate()},
		{"orders UUID with ampersand", (TWAPOrdersRequest{UUIDs: []string{"a&b"}}).validate()},
		{"orders empty UUID", (TWAPOrdersRequest{UUIDs: []string{""}}).validate()},
		{"cancel without ID", (CancelTWAPOrderRequest{}).validate()},
		{"cancel ID with ampersand", (CancelTWAPOrderRequest{AlgoOrderID: "a&b=1"}).validate()},
		{"cancel ID with padding", (CancelTWAPOrderRequest{AlgoOrderID: " TWAP-1"}).validate()},
	}
	for _, test := range invalid {
		if !errors.Is(test.err, trade.ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", test.name, test.err)
		}
	}

	accepted := []error{
		ask.validate(),
		bid.validate(),
		change(ask, func(r *PlaceTWAPOrderRequest) { r.Duration = 300 * time.Second }).validate(),
		change(ask, func(r *PlaceTWAPOrderRequest) { r.Duration = 43200 * time.Second }).validate(),
		change(ask, func(r *PlaceTWAPOrderRequest) { r.Frequency = 15 * time.Second }).validate(),
		change(ask, func(r *PlaceTWAPOrderRequest) { r.Frequency = 20 * time.Second }).validate(),
		change(ask, func(r *PlaceTWAPOrderRequest) { r.Frequency = 30 * time.Second }).validate(),
		change(ask, func(r *PlaceTWAPOrderRequest) { r.Frequency = 2 * time.Minute }).validate(),
		(TWAPOrdersRequest{}).validate(),
		(TWAPOrdersRequest{Market: "KRW-BTC", State: TWAPStateDone, Limit: 100, OrderBy: "asc"}).validate(),
		(CancelTWAPOrderRequest{AlgoOrderID: "TWAP-A01B02C03D04E05F06"}).validate(),
	}
	for index, err := range accepted {
		if err != nil {
			t.Errorf("accepted TWAP request %d error = %v", index, err)
		}
	}
}

func TestTWAPRequestsRejectInvalidInputBeforeSending(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, nil, extTrader)
	ctx := context.Background()
	if _, err := fixture.client.PlaceTWAPOrder(ctx, PlaceTWAPOrderRequest{Market: "KRW-BTC", Side: SideAsk}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("PlaceTWAPOrder() error = %v, want ErrValidation", err)
	}
	if _, err := fixture.client.TWAPOrders(ctx, TWAPOrdersRequest{Limit: 500}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("TWAPOrders() error = %v, want ErrValidation", err)
	}
	if _, err := fixture.client.CancelTWAPOrder(ctx, CancelTWAPOrderRequest{}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("CancelTWAPOrder() error = %v, want ErrValidation", err)
	}
	extRequests(t, fixture.server, 0)
	if calls, _, _ := fixture.provider.snapshot(); calls != 0 {
		t.Fatalf("credential provider calls = %d, want 0", calls)
	}
}
