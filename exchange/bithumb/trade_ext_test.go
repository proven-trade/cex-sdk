package bithumb

import (
	"context"
	"errors"
	"strings"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/ratelimit"
)

const orderChanceSample = `{"bid_fee":"0.0025","ask_fee":"0.0025","maker_bid_fee":"0.0015","maker_ask_fee":"0.0015",` +
	`"market":{"id":"KRW-BTC","name":"BTC/KRW","order_types":["limit","price","market"],"order_sides":["bid","ask"],` +
	`"bid_types":["limit","price"],"ask_types":["limit","market"],` +
	`"bid":{"currency":"KRW","price_unit":"1000","min_total":"5000"},` +
	`"ask":{"currency":"BTC","price_unit":null,"min_total":"5000"},"max_total":"1000000000","state":"active"},` +
	`"bid_account":{"currency":"KRW","balance":"1000000","locked":"0","avg_buy_price":"0","avg_buy_price_modified":false,"unit_currency":"KRW"},` +
	`"ask_account":{"currency":"BTC","balance":"0.5","locked":"0.1","avg_buy_price":"90000000","avg_buy_price_modified":false,"unit_currency":"KRW"}}`

const searchOrdersSample = `[{"order_id":"o-1","client_order_id":"c-1","side":"bid","order_type":"limit","price":"90000000","state":"wait",` +
	`"market":"KRW-BTC","created_at":"2025-01-01T00:00:00+09:00","volume":"0.01","remaining_volume":"0.01","reserved_fee":"0",` +
	`"remaining_fee":"0","paid_fee":"0","locked":"900000","executed_volume":"0","executed_funds":"0","trades_count":0,` +
	`"stp_type":"cancel_taker","time_in_force":"post_only"},` +
	`{"order_id":"o-2","side":"ask","order_type":"limit","price":"91000000","state":"cancel","market":"KRW-BTC",` +
	`"volume":"0.02","remaining_volume":"0.02","executed_volume":"0","executed_funds":"0","trades_count":0,` +
	`"cancel_type":"stp_cancel","canceling_order_id":"o-9"}]`

const batchOrdersSample = `{"batch_orders_response":[{"client_order_id":"batch-1","order_id":"o-1","market":"KRW-BTC","side":"bid",` +
	`"order_type":"limit","time_in_force":"ioc","created_at":"2025-01-01T00:00:00+09:00","stp_type":"cancel_taker"},` +
	`{"client_order_id":"batch-2","name":"under_min_total_bid","message":"Order amount is below the minimum."}]}`

const batchCancelSample = `{"success":[{"order_id":"o-1","client_order_id":"batch-1","created_at":"2025-01-01T00:00:01+09:00"}],` +
	`"fail":[{"order_id":"o-2","client_order_id":"batch-2","error":{"name":"order_not_found","message":"Order not found."}}]}`

const batchOrdersBody = `{"batch_orders":[{"market":"KRW-BTC","side":"bid","volume":"0.001","price":"84000000",` +
	`"order_type":"limit","time_in_force":"ioc","client_order_id":"batch-1"},` +
	`{"market":"KRW-BTC","side":"bid","volume":"0.001","price":"83000000","order_type":"limit","client_order_id":"batch-2"}]}`

const batchOrdersHash = `batch_orders[0][market]=KRW-BTC&batch_orders[0][side]=bid&batch_orders[0][volume]=0.001&` +
	`batch_orders[0][price]=84000000&batch_orders[0][order_type]=limit&batch_orders[0][time_in_force]=ioc&` +
	`batch_orders[0][client_order_id]=batch-1&batch_orders[1][market]=KRW-BTC&batch_orders[1][side]=bid&` +
	`batch_orders[1][volume]=0.001&batch_orders[1][price]=83000000&batch_orders[1][order_type]=limit&` +
	`batch_orders[1][client_order_id]=batch-2`

func extBatchRequest() BatchOrdersRequest {
	return BatchOrdersRequest{Orders: []PlaceOrderRequest{
		{
			Market: "KRW-BTC", Side: SideBid, Volume: "0.001", Price: "84000000", OrderType: OrderTypeLimit,
			TimeInForce: TimeInForceIOC, ClientOrderID: "batch-1",
		},
		{
			Market: "KRW-BTC", Side: SideBid, Volume: "0.001", Price: "83000000", OrderType: OrderTypeLimit,
			ClientOrderID: "batch-2",
		},
	}}
}

func TestOrderChance(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{extOK("GET /v1/orders/chance", orderChanceSample)}, extReadOnly)
	ctx := context.Background()
	chance, err := fixture.client.OrderChance(ctx, OrderChanceRequest{Market: "KRW-BTC"})
	if err != nil || chance.BidFee != "0.0025" || chance.MakerBidFee != "0.0015" || chance.Market.ID != "KRW-BTC" ||
		len(chance.Market.OrderTypes) != 3 || chance.Market.OrderTypes[2] != OrderTypeMarket ||
		len(chance.Market.OrderSides) != 2 || chance.Market.Bid.Currency != "KRW" || chance.Market.Bid.MinTotal != "5000" ||
		chance.Market.Ask.PriceUnit != "" || chance.Market.MaxTotal != "1000000000" || chance.Market.State != "active" ||
		chance.BidAccount.Balance != "1000000" || chance.AskAccount.Locked != "0.1" || len(chance.Raw) == 0 {
		t.Fatalf("OrderChance() = %+v, error = %v", chance, err)
	}
	records := extRequests(t, fixture.server, 1)
	assertExtRequest(t, records[0], "GET", "/v1/orders/chance", "market=KRW-BTC", "", "market=KRW-BTC")
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:private-other:1second", 140, 1)

	if _, err := fixture.client.OrderChance(ctx, OrderChanceRequest{Market: "KRWBTC"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("OrderChance() invalid market error = %v, want ErrValidation", err)
	}
	extRequests(t, fixture.server, 1)
}

func TestSearchOrders(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{extOK("POST /v2/orders/search", searchOrdersSample)}, extReadOnly)
	ctx := context.Background()
	orders, err := fixture.client.SearchOrders(ctx, SearchOrdersRequest{
		Market: "KRW-BTC", OrderIDs: []string{"o-1", "o-2"}, OrderBy: "desc",
	})
	if err != nil || len(orders) != 2 || orders[0].OrderID != "o-1" || orders[0].ClientOrderID != "c-1" ||
		orders[0].State != OrderStateWait || orders[0].TimeInForce != TimeInForcePostOnly ||
		orders[0].STPType != "cancel_taker" || orders[1].State != OrderStateCancel ||
		orders[1].CancelType != "stp_cancel" || orders[1].CancelingOrderID != "o-9" || len(orders[1].Raw) == 0 {
		t.Fatalf("SearchOrders() = %+v, error = %v", orders, err)
	}
	byClientID, err := fixture.client.SearchOrders(ctx, SearchOrdersRequest{ClientOrderIDs: []string{"c-1"}})
	if err != nil || len(byClientID) != 2 {
		t.Fatalf("SearchOrders(client IDs) = %+v, error = %v", byClientID, err)
	}

	records := extRequests(t, fixture.server, 2)
	assertExtRequest(
		t, records[0], "POST", "/v2/orders/search", "",
		`{"market":"KRW-BTC","order_ids":["o-1","o-2"],"order_by":"desc"}`,
		"market=KRW-BTC&order_ids[]=o-1&order_ids[]=o-2&order_by=desc",
	)
	assertExtRequest(
		t, records[1], "POST", "/v2/orders/search", "",
		`{"client_order_ids":["c-1"]}`, "client_order_ids[]=c-1",
	)
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:private-other:1second", 140, 2)
}

func TestSearchOrdersRetriesTransientReadFailures(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{
		extReplies(
			"POST /v2/orders/search",
			extStatus(500, `{"error":{"name":"internal_error","message":"temporary"}}`),
			extStatus(200, `[]`),
		),
	}, extReadOnly)
	orders, err := fixture.client.SearchOrders(context.Background(), SearchOrdersRequest{ClientOrderIDs: []string{"c-1"}})
	if err != nil || len(orders) != 0 {
		t.Fatalf("SearchOrders() = %+v, error = %v", orders, err)
	}
	for _, record := range extRequests(t, fixture.server, 2) {
		assertExtRequest(
			t, record, "POST", "/v2/orders/search", "",
			`{"client_order_ids":["c-1"]}`, "client_order_ids[]=c-1",
		)
	}
}

func TestPlaceBatchOrders(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{extOK("POST /v2/orders/batch", batchOrdersSample)}, extTrader)
	result, err := fixture.client.PlaceBatchOrders(context.Background(), extBatchRequest())
	if err != nil || len(result.Orders) != 2 || len(result.Raw) == 0 {
		t.Fatalf("PlaceBatchOrders() = %+v, error = %v", result, err)
	}
	accepted, rejected := result.Orders[0], result.Orders[1]
	if !accepted.Succeeded() || accepted.OrderID != "o-1" || accepted.ClientOrderID != "batch-1" ||
		accepted.Market != "KRW-BTC" || accepted.Side != SideBid || accepted.OrderType != OrderTypeLimit ||
		accepted.TimeInForce != TimeInForceIOC || accepted.STPType != "cancel_taker" || len(accepted.Raw) == 0 {
		t.Fatalf("accepted batch order = %+v", accepted)
	}
	if rejected.Succeeded() || rejected.OrderID != "" || rejected.ClientOrderID != "batch-2" ||
		rejected.ErrorName != "under_min_total_bid" || rejected.ErrorMessage == "" || len(rejected.Raw) == 0 {
		t.Fatalf("rejected batch order = %+v", rejected)
	}

	records := extRequests(t, fixture.server, 1)
	assertExtRequest(t, records[0], "POST", "/v2/orders/batch", "", batchOrdersBody, batchOrdersHash)
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:order-batch:1second", 20, 1)
	if _, err := fixture.limiter.Snapshot("bithumb:route:route-a:order-create:1second"); !errors.Is(err, ratelimit.ErrUnknownRule) {
		t.Fatalf("single order bucket error = %v, want ErrUnknownRule because batch orders use their own bucket", err)
	}
}

func TestCancelBatchOrders(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{extOK("POST /v2/orders/cancel", batchCancelSample)}, extTrader)
	ctx := context.Background()
	result, err := fixture.client.CancelBatchOrders(ctx, CancelBatchOrdersRequest{OrderIDs: []string{"o-1", "o-2"}})
	if err != nil || len(result.Success) != 1 || len(result.Fail) != 1 || len(result.Raw) == 0 {
		t.Fatalf("CancelBatchOrders() = %+v, error = %v", result, err)
	}
	success, failure := result.Success[0], result.Fail[0]
	if success.OrderID != "o-1" || success.ClientOrderID != "batch-1" || success.CreatedAt != "2025-01-01T00:00:01+09:00" ||
		len(success.Raw) == 0 {
		t.Fatalf("cancel success = %+v", success)
	}
	if failure.OrderID != "o-2" || failure.ClientOrderID != "batch-2" || failure.Reason.Name != "order_not_found" ||
		failure.Reason.Message != "Order not found." || len(failure.Raw) == 0 {
		t.Fatalf("cancel failure = %+v", failure)
	}
	if _, err := fixture.client.CancelBatchOrders(ctx, CancelBatchOrdersRequest{ClientOrderIDs: []string{"batch-1"}}); err != nil {
		t.Fatalf("CancelBatchOrders(client IDs) error = %v", err)
	}

	records := extRequests(t, fixture.server, 2)
	assertExtRequest(
		t, records[0], "POST", "/v2/orders/cancel", "",
		`{"order_ids":["o-1","o-2"]}`, "order_ids[]=o-1&order_ids[]=o-2",
	)
	assertExtRequest(
		t, records[1], "POST", "/v2/orders/cancel", "",
		`{"client_order_ids":["batch-1"]}`, "client_order_ids[]=batch-1",
	)
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:order-batch:1second", 20, 2)
}

func TestBatchMutationsAreNeverRetriedAndReportUnknownState(t *testing.T) {
	t.Parallel()

	serverError := `{"error":{"name":"internal_error","message":"boom"}}`
	fixture := newExtFixture(t, []extRoute{
		extReplies("POST /v2/orders/batch", extStatus(500, serverError)),
		extReplies("POST /v2/orders/cancel", extStatus(500, serverError)),
	}, extTrader)
	ctx := context.Background()
	if _, err := fixture.client.PlaceBatchOrders(ctx, extBatchRequest()); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceBatchOrders() 5xx error = %v, want ErrUnknownExecutionState", err)
	}
	if _, err := fixture.client.CancelBatchOrders(ctx, CancelBatchOrdersRequest{OrderIDs: []string{"o-1"}}); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CancelBatchOrders() 5xx error = %v, want ErrUnknownExecutionState", err)
	}
	extRequests(t, fixture.server, 2)

	malformed := newExtFixture(t, []extRoute{extOK("POST /v2/orders/batch", `{invalid`)}, extTrader)
	if _, err := malformed.client.PlaceBatchOrders(ctx, extBatchRequest()); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceBatchOrders() malformed body error = %v, want ErrUnknownExecutionState", err)
	}
	extRequests(t, malformed.server, 1)

	networkClient, _, _ := newExtClient(t, "http://127.0.0.1", errorSender{}, extTrader)
	if _, err := networkClient.PlaceBatchOrders(ctx, extBatchRequest()); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceBatchOrders() network error = %v, want ErrUnknownExecutionState", err)
	}
	if _, err := networkClient.CancelBatchOrders(ctx, CancelBatchOrdersRequest{OrderIDs: []string{"o-1"}}); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CancelBatchOrders() network error = %v, want ErrUnknownExecutionState", err)
	}
}

func TestBatchMutationsRequireTradePermission(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{
		extOK("POST /v2/orders/batch", batchOrdersSample),
		extOK("POST /v2/orders/cancel", batchCancelSample),
	}, extReadOnly)
	ctx := context.Background()
	_, placeErr := fixture.client.PlaceBatchOrders(ctx, extBatchRequest())
	_, cancelErr := fixture.client.CancelBatchOrders(ctx, CancelBatchOrdersRequest{OrderIDs: []string{"o-1"}})
	for name, err := range map[string]error{"place": placeErr, "cancel": cancelErr} {
		if !errors.Is(err, trade.ErrAuthorization) || !errors.Is(err, credential.ErrPermissionNotAllowed) {
			t.Errorf("%s error = %v, want ErrAuthorization from missing trade permission", name, err)
		}
	}
	if calls, _, _ := fixture.provider.snapshot(); calls != 0 {
		t.Fatalf("credential provider calls = %d, want 0", calls)
	}
	extRequests(t, fixture.server, 0)
}

func TestBatchAndSearchRequestsRejectInvalidInput(t *testing.T) {
	t.Parallel()

	valid := PlaceOrderRequest{
		Market: "KRW-BTC", Side: SideBid, Volume: "0.001", Price: "84000000", OrderType: OrderTypeLimit,
	}
	withID := func(id string) PlaceOrderRequest {
		order := valid
		order.ClientOrderID = id
		return order
	}
	repeat := func(count int, id string) []string {
		ids := make([]string, count)
		for index := range ids {
			ids[index] = id + "-" + strings.Repeat("x", index%3)
		}
		return ids
	}
	orders := func(count int) []PlaceOrderRequest {
		list := make([]PlaceOrderRequest, count)
		for index := range list {
			list[index] = valid
		}
		return list
	}

	invalid := []struct {
		name string
		err  error
	}{
		{"empty batch", (BatchOrdersRequest{}).validate()},
		{"31 batch orders", (BatchOrdersRequest{Orders: orders(31)}).validate()},
		{"invalid batch item", (BatchOrdersRequest{Orders: []PlaceOrderRequest{valid, {Market: "bad"}}}).validate()},
		{"duplicate client order ID", (BatchOrdersRequest{Orders: []PlaceOrderRequest{withID("dup"), withID("dup")}}).validate()},
		{"search without IDs", (SearchOrdersRequest{}).validate()},
		{"search with both ID lists", (SearchOrdersRequest{OrderIDs: []string{"a"}, ClientOrderIDs: []string{"b"}}).validate()},
		{"search 101 order IDs", (SearchOrdersRequest{OrderIDs: repeat(101, "o")}).validate()},
		{"search invalid market", (SearchOrdersRequest{Market: "krw", OrderIDs: []string{"a"}}).validate()},
		{"search invalid client order ID", (SearchOrdersRequest{ClientOrderIDs: []string{"bad id!"}}).validate()},
		{"search order ID with ampersand", (SearchOrdersRequest{OrderIDs: []string{"a&b"}}).validate()},
		{"search order ID with padding", (SearchOrdersRequest{OrderIDs: []string{" a"}}).validate()},
		{"search empty order ID", (SearchOrdersRequest{OrderIDs: []string{""}}).validate()},
		{"search invalid order by", (SearchOrdersRequest{OrderIDs: []string{"a"}, OrderBy: "up"}).validate()},
		{"cancel without IDs", (CancelBatchOrdersRequest{}).validate()},
		{"cancel with both ID lists", (CancelBatchOrdersRequest{OrderIDs: []string{"a"}, ClientOrderIDs: []string{"b"}}).validate()},
		{"cancel 31 order IDs", (CancelBatchOrdersRequest{OrderIDs: repeat(31, "o")}).validate()},
		{"cancel invalid client order ID", (CancelBatchOrdersRequest{ClientOrderIDs: []string{"a b"}}).validate()},
		{"order chance invalid market", (OrderChanceRequest{Market: "KRW"}).validate()},
	}
	for _, test := range invalid {
		if !errors.Is(test.err, trade.ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", test.name, test.err)
		}
	}
	itemErr := (BatchOrdersRequest{Orders: []PlaceOrderRequest{valid, {Market: "bad"}}}).validate()
	if itemErr == nil || !strings.Contains(itemErr.Error(), "batch order 1") {
		t.Errorf("invalid batch item error = %v, want item index 1", itemErr)
	}

	accepted := []error{
		(BatchOrdersRequest{Orders: orders(30)}).validate(),
		(SearchOrdersRequest{OrderIDs: repeat(100, "o")}).validate(),
		(SearchOrdersRequest{Market: "KRW-BTC", ClientOrderIDs: []string{"client_1"}, OrderBy: "asc"}).validate(),
		(CancelBatchOrdersRequest{OrderIDs: repeat(30, "o")}).validate(),
		(CancelBatchOrdersRequest{ClientOrderIDs: []string{"client-1"}}).validate(),
	}
	for index, err := range accepted {
		if err != nil {
			t.Errorf("accepted request %d error = %v", index, err)
		}
	}
}
