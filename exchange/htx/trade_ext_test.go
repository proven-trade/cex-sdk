package htx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

func extReadJSONBody(request *http.Request) (map[string]any, bool) {
	var body map[string]any
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		return nil, false
	}
	return body, true
}

func assertHTXWindowLimit(
	t *testing.T,
	limiter *ratelimit.Limiter,
	key string,
	wantUsed int,
	wantLimit int,
	wantWindow time.Duration,
) {
	t.Helper()
	snapshot, err := limiter.Snapshot(key)
	if err != nil || snapshot.Used != wantUsed || snapshot.Rule.Limit != wantLimit ||
		snapshot.Rule.Window != wantWindow {
		t.Fatalf("limiter snapshot %q = %+v, error = %v", key, snapshot, err)
	}
}

func validAlgoLimitOrder() PlaceAlgoOrderRequest {
	return PlaceAlgoOrderRequest{
		AccountID: "13496526", Symbol: "adausdt", Side: SideBuy, Type: AlgoOrderTypeLimit,
		ClientOrderID: "a001", StopPrice: "0.1", Price: "0.1", Size: "100",
		TimeInForce: AlgoTimeInForceGTC,
	}
}

func TestClientPrivateTradeExtensions(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC)
	start := fixedNow.Add(-24 * time.Hour)
	end := fixedNow
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if !verifyHTXSignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"status":"error","err-code":"api-signature-not-valid"}`, http.StatusUnauthorized)
			return
		}
		query := request.URL.Query()
		switch {
		case request.URL.Path == "/v1/order/batch-orders" && request.Method == http.MethodPost:
			var body []map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || len(body) != 2 ||
				body[0]["account-id"] != "12345" || body[0]["client-order-id"] != "strategy-1" ||
				body[0]["symbol"] != "btcusdt" || body[0]["type"] != "buy-limit" ||
				body[0]["amount"] != "0.1" || body[0]["price"] != "64000" ||
				body[0]["source"] != "spot-api" || body[0]["self-match-prevent"] != float64(1) ||
				body[1]["client-order-id"] != "strategy-2" || body[1]["amount"] != "0.2" ||
				body[1]["self-match-prevent"] != nil {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"order-id":361560582529749,"client-order-id":"strategy-1"},{"client-order-id":"strategy-2","err-code":"order-value-min-error","err-msg":"Order total cannot be lower than: 5 USDT"}]}`)
		case request.URL.Path == "/v1/order/orders/batchCancelOpenOrders" && request.Method == http.MethodPost:
			body, ok := extReadJSONBody(request)
			if !ok {
				extBadRequest(writer)
				return
			}
			if _, scoped := body["symbol"]; scoped {
				if body["account-id"] != "12345" || body["symbol"] != "btcusdt,ethusdt" ||
					body["types"] != "buy-limit,sell-limit" || body["side"] != "buy" ||
					body["size"] != float64(5) {
					extBadRequest(writer)
					return
				}
				_, _ = io.WriteString(writer, `{"status":"ok","data":{"success-count":2,"failed-count":0,"next-id":5454600}}`)
				return
			}
			if len(body) != 0 {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":{"success-count":3,"failed-count":1,"next-id":-1}}`)
		case request.URL.Path == "/v1/order/orders/batchcancel" && request.Method == http.MethodPost:
			var body struct {
				OrderIDs       []string `json:"order-ids"`
				ClientOrderIDs []string `json:"client-order-ids"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || len(body.OrderIDs) != 0 ||
				!slices.Equal(body.ClientOrderIDs, []string{"12345", "123456"}) {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":{"success":["12345"],"failed":[{"err-msg":"Incorrect order state","order-state":7,"order-id":"357631450723117","err-code":"order-orderstate-error","client-order-id":"123456"}]}}`)
		case request.URL.Path == "/v2/algo-orders/cancel-all-after" && request.Method == http.MethodPost:
			body, ok := extReadJSONBody(request)
			if !ok || len(body) != 1 {
				extBadRequest(writer)
				return
			}
			switch body["timeout"] {
			case float64(10):
				_, _ = io.WriteString(writer, `{"code":200,"message":"success","data":{"currentTime":1630491627230,"triggerTime":1630491637230}}`)
			case float64(0):
				_, _ = io.WriteString(writer, `{"code":200,"message":"success","data":{"currentTime":1630491780445,"triggerTime":0}}`)
			default:
				extBadRequest(writer)
			}
		case request.URL.Path == "/v1/order/history" && request.Method == http.MethodGet:
			if query.Get("symbol") != "adausdt" || query.Get("start-time") != "1787540645000" ||
				query.Get("end-time") != "1787627045000" || query.Get("direct") != "prev" ||
				query.Get("size") != "10" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"id":357632718898331,"symbol":"adausdt","account-id":13496526,"client-order-id":"23456","amount":"5.000000000000000000","price":"1.000000000000000000","created-at":1630649406687,"type":"buy-limit-maker","field-amount":"0.0","field-cash-amount":"0.0","field-fees":"0.0","finished-at":0,"source":"spot-api","state":"submitted","canceled-at":0},{"id":357632718898330,"symbol":"adausdt","account-id":13496526,"client-order-id":"2345","amount":"5.000000000000000000","price":"1.000000000000000000","created-at":1630649406687,"type":"buy-limit-maker","field-amount":"5.0","field-cash-amount":"5.0","field-fees":"0.005","finished-at":1630649500000,"source":"spot-api","state":"filled","canceled-at":0}],"next-time":1630649406000}`)
		case request.URL.Path == "/v2/reference/transact-fee-rate" && request.Method == http.MethodGet:
			if query.Get("symbols") != "btcusdt,apnusdt" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"code":200,"data":[{"symbol":"btcusdt","actualMakerRate":"0.002","actualTakerRate":"0.002","takerFeeRate":"0.002","makerFeeRate":"0.002"},{"symbol":"apnusdt","actualMakerRate":"-0.0001","actualTakerRate":"0.0015","takerFeeRate":"0.002","makerFeeRate":"-0.0001"}],"success":true}`)
		case request.URL.Path == "/v2/algo-orders" && request.Method == http.MethodPost:
			body, ok := extReadJSONBody(request)
			if !ok {
				extBadRequest(writer)
				return
			}
			switch body["clientOrderId"] {
			case "a001":
				if body["accountId"] != float64(13496526) || body["symbol"] != "adausdt" ||
					body["orderPrice"] != "0.1" || body["orderSide"] != "buy" ||
					body["orderSize"] != "100" || body["timeInForce"] != "gtc" ||
					body["orderType"] != "limit" || body["stopPrice"] != "0.1" ||
					body["orderValue"] != nil || body["trailingRate"] != nil {
					extBadRequest(writer)
					return
				}
			case "a002":
				if body["orderType"] != "market" || body["orderSide"] != "sell" ||
					body["orderSize"] != "100" || body["stopPrice"] != "0.2" ||
					body["trailingRate"] != "0.01" || body["orderPrice"] != nil ||
					body["timeInForce"] != nil {
					extBadRequest(writer)
					return
				}
			default:
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"code":200,"data":{"clientOrderId":"`+body["clientOrderId"].(string)+`"}}`)
		case request.URL.Path == "/v2/algo-orders/cancellation" && request.Method == http.MethodPost:
			var body struct {
				ClientOrderIDs []string `json:"clientOrderIds"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil ||
				!slices.Equal(body.ClientOrderIDs, []string{"zy0002", "zy0003"}) {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"code":200,"data":{"accepted":["zy0002"],"rejected":["zy0003"]}}`)
		case request.URL.Path == "/v2/algo-orders/opening" && request.Method == http.MethodGet:
			if query.Get("accountId") != "13496526" || query.Get("symbol") != "adausdt" ||
				query.Get("orderSide") != "buy" || query.Get("orderType") != "limit" ||
				query.Get("sort") != "desc" || query.Get("limit") != "100" ||
				query.Get("fromId") != "9585084" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"code":200,"data":[{"lastActTime":1630657250326,"orderOrigTime":1630657250238,"symbol":"adausdt","source":"api","clientOrderId":"123","orderSide":"buy","orderType":"limit","orderPrice":"0.1","orderSize":"100","accountId":13496526,"timeInForce":"gtc","stopPrice":"0.1","orderStatus":"created"},{"lastActTime":1630657243576,"orderOrigTime":1630657243534,"symbol":"adausdt","source":"api","clientOrderId":"12","orderSide":"buy","orderType":"market","orderValue":"10","accountId":13496526,"timeInForce":"ioc","stopPrice":"0.1","trailingRate":"0.01","orderStatus":"created"}],"nextId":9585000}`)
		case request.URL.Path == "/v2/algo-orders/history" && request.Method == http.MethodGet:
			if query.Get("accountId") != "13496526" || query.Get("symbol") != "adausdt" ||
				query.Get("orderSide") != "sell" || query.Get("orderType") != "market" ||
				query.Get("orderStatus") != "triggered" || query.Get("startTime") != "1787540645000" ||
				query.Get("endTime") != "1787627045000" || query.Get("sort") != "asc" ||
				query.Get("limit") != "50" || query.Get("fromId") != "9585084" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"code":200,"data":[{"orderOrigTime":1630656758442,"lastActTime":1630656880512,"symbol":"adausdt","source":"api","clientOrderId":"a002","orderId":"357632718898331","orderSide":"sell","orderType":"market","orderSize":"100","accountId":13496526,"timeInForce":"ioc","stopPrice":"0.2","trailingRate":"0.01","orderCreateTime":1630656880500,"orderStatus":"triggered"},{"orderOrigTime":1630656758000,"lastActTime":1630656880000,"symbol":"adausdt","source":"api","clientOrderId":"a003","orderSide":"sell","orderType":"market","orderSize":"100","accountId":13496526,"timeInForce":"ioc","stopPrice":"0.2","orderStatus":"rejected","errCode":2002,"errMessage":"insufficient balance"}],"nextId":9585084}`)
		case request.URL.Path == "/v2/algo-orders/specific" && request.Method == http.MethodGet:
			if query.Get("clientOrderId") != "1234567" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"code":200,"data":{"lastActTime":1630656880512,"orderOrigTime":1630656758442,"symbol":"adausdt","source":"api","orderStatus":"canceled","clientOrderId":"1234567","orderSide":"buy","orderType":"limit","orderPrice":"0.1","orderSize":"100","accountId":13496526,"timeInForce":"gtc","stopPrice":"0.1"}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newPrivateTestClient(
		t, server.URL, sender, provider,
		[]transport.EgressRouteID{"route-a", "route-b"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	ctx := context.Background()

	second := validHTXLimitOrder()
	second.ClientOrderID = "strategy-2"
	second.Amount = "0.2"
	second.SelfMatchPrevent = false
	batch, err := client.PlaceBatchOrders(
		ctx, PlaceBatchOrdersRequest{Orders: []PlaceOrderRequest{validHTXLimitOrder(), second}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || len(batch) != 2 || !batch[0].Accepted() || batch[0].OrderID != "361560582529749" ||
		batch[0].ClientOrderID != "strategy-1" || len(batch[0].Raw) == 0 ||
		batch[1].Accepted() || batch[1].OrderID != "" || batch[1].ClientOrderID != "strategy-2" ||
		batch[1].ErrorCode != "order-value-min-error" ||
		batch[1].ErrorMessage != "Order total cannot be lower than: 5 USDT" {
		t.Fatalf("PlaceBatchOrders() = %+v, error = %v", batch, err)
	}
	scoped, err := client.CancelOpenOrders(ctx, CancelOpenOrdersRequest{
		AccountID: "12345", Symbols: []string{"btcusdt", "ethusdt"},
		Types: []OrderType{OrderTypeBuyLimit, OrderTypeSellLimit}, Side: SideBuy, Size: 5,
	})
	if err != nil || scoped.SuccessCount != 2 || scoped.FailedCount != 0 ||
		scoped.NextID != "5454600" || len(scoped.Raw) == 0 {
		t.Fatalf("CancelOpenOrders() scoped = %+v, error = %v", scoped, err)
	}
	everything, err := client.CancelOpenOrders(ctx, CancelOpenOrdersRequest{AllSymbols: true})
	if err != nil || everything.SuccessCount != 3 || everything.FailedCount != 1 ||
		everything.NextID != "-1" {
		t.Fatalf("CancelOpenOrders() all = %+v, error = %v", everything, err)
	}
	cancelled, err := client.CancelOrdersByIDs(
		ctx, CancelOrdersByIDsRequest{ClientOrderIDs: []string{"12345", "123456"}},
	)
	if err != nil || !slices.Equal(cancelled.Succeeded, []Scalar{"12345"}) ||
		len(cancelled.Failed) != 1 || cancelled.Failed[0].OrderID != "357631450723117" ||
		cancelled.Failed[0].ClientOrderID != "123456" || cancelled.Failed[0].OrderState != "7" ||
		cancelled.Failed[0].ErrorCode != "order-orderstate-error" ||
		cancelled.Failed[0].ErrorMessage != "Incorrect order state" ||
		len(cancelled.Failed[0].Raw) == 0 || len(cancelled.Raw) == 0 {
		t.Fatalf("CancelOrdersByIDs() = %+v, error = %v", cancelled, err)
	}
	switchOn, err := client.CancelAllAfter(ctx, CancelAllAfterRequest{TimeoutSeconds: 10})
	if err != nil || switchOn.CurrentTime != 1_630_491_627_230 ||
		switchOn.TriggerTime != 1_630_491_637_230 || len(switchOn.Raw) == 0 {
		t.Fatalf("CancelAllAfter() on = %+v, error = %v", switchOn, err)
	}
	switchOff, err := client.CancelAllAfter(ctx, CancelAllAfterRequest{})
	if err != nil || switchOff.CurrentTime != 1_630_491_780_445 || switchOff.TriggerTime != 0 {
		t.Fatalf("CancelAllAfter() off = %+v, error = %v", switchOff, err)
	}
	recent, err := client.RecentOrderHistory(ctx, RecentOrderHistoryRequest{
		Symbol: "adausdt", Start: &start, End: &end, Direction: QueryDirectionPrev, Size: 10,
	})
	if err != nil || len(recent.Orders) != 2 || recent.NextTime != "1630649406000" ||
		recent.Orders[0].ID != "357632718898331" || recent.Orders[0].State != OrderStateSubmitted ||
		recent.Orders[0].Type != OrderTypeBuyLimitMaker || recent.Orders[0].Amount != "5.000000000000000000" ||
		recent.Orders[1].State != OrderStateFilled || recent.Orders[1].FilledAmount != "5.0" ||
		recent.Orders[1].FilledFees != "0.005" || recent.Orders[1].FinishedAt != 1_630_649_500_000 ||
		len(recent.Orders[0].Raw) == 0 || len(recent.Raw) == 0 {
		t.Fatalf("RecentOrderHistory() = %+v, error = %v", recent, err)
	}
	rates, err := client.TransactFeeRates(
		ctx, TransactFeeRateRequest{Symbols: []string{"btcusdt", "apnusdt"}},
	)
	if err != nil || len(rates) != 2 || rates[0].Symbol != "btcusdt" || rates[0].MakerFeeRate != "0.002" ||
		rates[0].ActualTakerRate != "0.002" || rates[1].MakerFeeRate != "-0.0001" ||
		rates[1].ActualMakerRate != "-0.0001" || rates[1].ActualTakerRate != "0.0015" ||
		len(rates[1].Raw) == 0 {
		t.Fatalf("TransactFeeRates() = %+v, error = %v", rates, err)
	}
	limitAlgo, err := client.PlaceAlgoOrder(ctx, validAlgoLimitOrder())
	if err != nil || limitAlgo.ClientOrderID != "a001" || len(limitAlgo.Raw) == 0 {
		t.Fatalf("PlaceAlgoOrder() limit = %+v, error = %v", limitAlgo, err)
	}
	trailing, err := client.PlaceAlgoOrder(ctx, PlaceAlgoOrderRequest{
		AccountID: "13496526", Symbol: "adausdt", Side: SideSell, Type: AlgoOrderTypeMarket,
		ClientOrderID: "a002", StopPrice: "0.2", Size: "100", TrailingRate: "0.01",
	})
	if err != nil || trailing.ClientOrderID != "a002" {
		t.Fatalf("PlaceAlgoOrder() trailing = %+v, error = %v", trailing, err)
	}
	algoCancel, err := client.CancelAlgoOrders(
		ctx, CancelAlgoOrdersRequest{ClientOrderIDs: []string{"zy0002", "zy0003"}},
	)
	if err != nil || !slices.Equal(algoCancel.Accepted, []string{"zy0002"}) ||
		!slices.Equal(algoCancel.Rejected, []string{"zy0003"}) || len(algoCancel.Raw) == 0 {
		t.Fatalf("CancelAlgoOrders() = %+v, error = %v", algoCancel, err)
	}
	opening, err := client.OpenAlgoOrders(ctx, OpenAlgoOrdersRequest{
		AccountID: "13496526", Symbol: "adausdt", Side: SideBuy, Type: AlgoOrderTypeLimit,
		Sort: SortDescending, Limit: 100, FromID: "9585084",
	})
	if err != nil || len(opening.Orders) != 2 || opening.NextID != "9585000" ||
		opening.Orders[0].ClientOrderID != "123" || opening.Orders[0].Status != AlgoOrderStatusCreated ||
		opening.Orders[0].AccountID != "13496526" || opening.Orders[0].OrderPrice != "0.1" ||
		opening.Orders[0].OrderSize != "100" || opening.Orders[0].StopPrice != "0.1" ||
		opening.Orders[0].TimeInForce != AlgoTimeInForceGTC || opening.Orders[0].LastActTime != 1_630_657_250_326 ||
		opening.Orders[0].OrderOrigTime != 1_630_657_250_238 || len(opening.Orders[0].Raw) == 0 ||
		opening.Orders[1].OrderType != AlgoOrderTypeMarket || opening.Orders[1].OrderValue != "10" ||
		opening.Orders[1].OrderPrice != "" || opening.Orders[1].TrailingRate != "0.01" || len(opening.Raw) == 0 {
		t.Fatalf("OpenAlgoOrders() = %+v, error = %v", opening, err)
	}
	history, err := client.AlgoOrderHistory(ctx, AlgoOrderHistoryRequest{
		AccountID: "13496526", Symbol: "adausdt", Side: SideSell, Type: AlgoOrderTypeMarket,
		Status: AlgoOrderStatusTriggered, Start: &start, End: &end,
		Sort: SortAscending, Limit: 50, FromID: "9585084",
	})
	if err != nil || len(history.Orders) != 2 || history.NextID != "9585084" ||
		history.Orders[0].Status != AlgoOrderStatusTriggered || history.Orders[0].OrderID != "357632718898331" ||
		history.Orders[0].OrderCreateTime != 1_630_656_880_500 || history.Orders[0].TrailingRate != "0.01" ||
		history.Orders[1].Status != AlgoOrderStatusRejected || history.Orders[1].ErrorCode != "2002" ||
		history.Orders[1].ErrorMessage != "insufficient balance" {
		t.Fatalf("AlgoOrderHistory() = %+v, error = %v", history, err)
	}
	specific, err := client.AlgoOrder(ctx, "1234567")
	if err != nil || specific.ClientOrderID != "1234567" || specific.Status != AlgoOrderStatusCanceled ||
		specific.OrderSide != SideBuy || specific.Source != "api" || specific.LastActTime != 1_630_656_880_512 ||
		len(specific.Raw) == 0 {
		t.Fatalf("AlgoOrder() = %+v, error = %v", specific, err)
	}

	routes := sender.snapshot()
	if len(routes) != 14 || routes[0] != "route-b" {
		t.Fatalf("private sender routes = %v", routes)
	}
	for _, route := range routes[1:] {
		if route != "route-a" {
			t.Fatalf("default sender route = %q, want route-a", route)
		}
	}
	if calls, apiKey, secret := provider.snapshot(); calls != 14 || !allZero(apiKey) || !allZero(secret) {
		t.Fatalf("provider state calls=%d apiKey=%v secret=%v", calls, apiKey, secret)
	}
	assertHTXWindowLimit(t, limiter, "htx:account:htx-main:order-batch:2seconds", 4, batchOrderRequestLimit, 2*time.Second)
	assertHTXWindowLimit(t, limiter, "htx:account:htx-main:order:2seconds", 2, DefaultOrderQuota, 2*time.Second)
	assertHTXWindowLimit(t, limiter, "htx:account:htx-main:trade-history:2seconds", 1, DefaultTradeHistoryQuota, 2*time.Second)
	assertHTXWindowLimit(t, limiter, "htx:account:htx-main:account:2seconds", 1, DefaultAccountQuota, 2*time.Second)
	assertHTXWindowLimit(t, limiter, "htx:account:htx-main:algo-order:2seconds", 6, algoOrderRequestLimit, 2*time.Second)
}

func TestTradeExtensionValidation(t *testing.T) {
	t.Parallel()
	start := time.UnixMilli(1_787_540_645_000)
	end := time.UnixMilli(1_787_627_045_000)
	orderIDs := make([]string, 51)
	for index := range orderIDs {
		orderIDs[index] = strconv.Itoa(index + 1)
	}
	symbols := []string{
		"btcusdt", "ethusdt", "xrpusdt", "adausdt", "dogeusdt", "ltcusdt",
		"eosusdt", "trxusdt", "linkusdt", "dotusdt", "uniusdt",
	}
	algo := func(change func(*PlaceAlgoOrderRequest)) error {
		request := validAlgoLimitOrder()
		change(&request)
		return request.validate()
	}
	marketBuy := func(change func(*PlaceAlgoOrderRequest)) error {
		request := PlaceAlgoOrderRequest{
			AccountID: "13496526", Symbol: "adausdt", Side: SideBuy, Type: AlgoOrderTypeMarket,
			ClientOrderID: "a010", StopPrice: "0.1", Value: "10",
		}
		change(&request)
		return request.validate()
	}
	valid := []struct {
		name string
		err  error
	}{
		{name: "limit algo order", err: validAlgoLimitOrder().validate()},
		{name: "market buy algo order", err: marketBuy(func(*PlaceAlgoOrderRequest) {})},
		{name: "market sell trailing", err: (PlaceAlgoOrderRequest{
			AccountID: "13496526", Symbol: "adausdt", Side: SideSell, Type: AlgoOrderTypeMarket,
			ClientOrderID: "a011", StopPrice: "0.1", Size: "5", TrailingRate: "0.05",
			TimeInForce: AlgoTimeInForceIOC,
		}).validate()},
		{name: "dead man off", err: (CancelAllAfterRequest{}).validate()},
		{name: "dead man minimum", err: (CancelAllAfterRequest{TimeoutSeconds: 5}).validate()},
		{name: "batch cancel by order IDs", err: (CancelOrdersByIDsRequest{OrderIDs: orderIDs[:50]}).validate()},
		{name: "batch cancel ten symbols", err: (CancelOpenOrdersRequest{Symbols: symbols[:10]}).validate()},
		{name: "history bounds", err: (RecentOrderHistoryRequest{Size: 1000}).validate()},
		{name: "algo history", err: (AlgoOrderHistoryRequest{
			Symbol: "adausdt", Status: AlgoOrderStatusRejected, Start: &start, End: &end, Limit: 500,
		}).validate()},
	}
	for _, test := range valid {
		t.Run("valid "+test.name, func(t *testing.T) {
			if test.err != nil {
				t.Fatalf("validation error = %v, want nil", test.err)
			}
		})
	}
	invalid := []struct {
		name string
		err  error
	}{
		{name: "batch empty", err: (PlaceBatchOrdersRequest{}).validate()},
		{name: "batch too many", err: (PlaceBatchOrdersRequest{
			Orders: slices.Repeat([]PlaceOrderRequest{validHTXLimitOrder()}, 11),
		}).validate()},
		{name: "batch duplicate client ID", err: (PlaceBatchOrdersRequest{
			Orders: []PlaceOrderRequest{validHTXLimitOrder(), validHTXLimitOrder()},
		}).validate()},
		{name: "batch invalid order", err: (PlaceBatchOrdersRequest{
			Orders: []PlaceOrderRequest{{}},
		}).validate()},
		{name: "cancel open no scope", err: (CancelOpenOrdersRequest{}).validate()},
		{name: "cancel open ambiguous scope", err: (CancelOpenOrdersRequest{
			Symbols: []string{"btcusdt"}, AllSymbols: true,
		}).validate()},
		{name: "cancel open too many symbols", err: (CancelOpenOrdersRequest{Symbols: symbols}).validate()},
		{name: "cancel open duplicate symbol", err: (CancelOpenOrdersRequest{
			Symbols: []string{"btcusdt", "btcusdt"},
		}).validate()},
		{name: "cancel open symbol", err: (CancelOpenOrdersRequest{Symbols: []string{"BTC-USDT"}}).validate()},
		{name: "cancel open account", err: (CancelOpenOrdersRequest{AllSymbols: true, AccountID: "abc"}).validate()},
		{name: "cancel open type", err: (CancelOpenOrdersRequest{AllSymbols: true, Types: []OrderType{"bad"}}).validate()},
		{name: "cancel open side", err: (CancelOpenOrdersRequest{AllSymbols: true, Side: "hold"}).validate()},
		{name: "cancel open size", err: (CancelOpenOrdersRequest{AllSymbols: true, Size: 101}).validate()},
		{name: "cancel open negative size", err: (CancelOpenOrdersRequest{AllSymbols: true, Size: -1}).validate()},
		{name: "cancel IDs none", err: (CancelOrdersByIDsRequest{}).validate()},
		{name: "cancel IDs both", err: (CancelOrdersByIDsRequest{
			OrderIDs: []string{"1"}, ClientOrderIDs: []string{"a"},
		}).validate()},
		{name: "cancel IDs too many", err: (CancelOrdersByIDsRequest{OrderIDs: orderIDs}).validate()},
		{name: "cancel IDs invalid order ID", err: (CancelOrdersByIDsRequest{OrderIDs: []string{"abc"}}).validate()},
		{name: "cancel IDs duplicate order ID", err: (CancelOrdersByIDsRequest{OrderIDs: []string{"1", "1"}}).validate()},
		{name: "cancel IDs invalid client ID", err: (CancelOrdersByIDsRequest{ClientOrderIDs: []string{"bad id"}}).validate()},
		{name: "cancel IDs duplicate client ID", err: (CancelOrdersByIDsRequest{ClientOrderIDs: []string{"a", "a"}}).validate()},
		{name: "dead man too short", err: (CancelAllAfterRequest{TimeoutSeconds: 4}).validate()},
		{name: "dead man negative", err: (CancelAllAfterRequest{TimeoutSeconds: -1}).validate()},
		{name: "history symbol", err: (RecentOrderHistoryRequest{Symbol: "BTC"}).validate()},
		{name: "history size low", err: (RecentOrderHistoryRequest{Size: 9}).validate()},
		{name: "history size high", err: (RecentOrderHistoryRequest{Size: 1001}).validate()},
		{name: "history direction", err: (RecentOrderHistoryRequest{Direction: "sideways"}).validate()},
		{name: "fee rate empty", err: (TransactFeeRateRequest{}).validate()},
		{name: "fee rate duplicate", err: (TransactFeeRateRequest{Symbols: []string{"btcusdt", "btcusdt"}}).validate()},
		{name: "fee rate symbol", err: (TransactFeeRateRequest{Symbols: []string{"BTC"}}).validate()},
		{name: "algo account", err: algo(func(request *PlaceAlgoOrderRequest) { request.AccountID = "x" })},
		{name: "algo symbol", err: algo(func(request *PlaceAlgoOrderRequest) { request.Symbol = "BTC" })},
		{name: "algo side", err: algo(func(request *PlaceAlgoOrderRequest) { request.Side = "hold" })},
		{name: "algo type", err: algo(func(request *PlaceAlgoOrderRequest) { request.Type = "stop" })},
		{name: "algo client ID", err: algo(func(request *PlaceAlgoOrderRequest) { request.ClientOrderID = "" })},
		{name: "algo stop price", err: algo(func(request *PlaceAlgoOrderRequest) { request.StopPrice = "0" })},
		{name: "algo time in force", err: algo(func(request *PlaceAlgoOrderRequest) { request.TimeInForce = "day" })},
		{name: "algo limit value", err: algo(func(request *PlaceAlgoOrderRequest) { request.Value = "10" })},
		{name: "algo limit price", err: algo(func(request *PlaceAlgoOrderRequest) { request.Price = "" })},
		{name: "algo limit size", err: algo(func(request *PlaceAlgoOrderRequest) { request.Size = "" })},
		{name: "algo limit trailing", err: algo(func(request *PlaceAlgoOrderRequest) { request.TrailingRate = "0.01" })},
		{name: "algo market price", err: marketBuy(func(request *PlaceAlgoOrderRequest) { request.Price = "1" })},
		{name: "algo market gtc", err: marketBuy(func(request *PlaceAlgoOrderRequest) { request.TimeInForce = AlgoTimeInForceGTC })},
		{name: "algo market buy size", err: marketBuy(func(request *PlaceAlgoOrderRequest) { request.Size = "1" })},
		{name: "algo market buy no value", err: marketBuy(func(request *PlaceAlgoOrderRequest) { request.Value = "" })},
		{name: "algo market sell value", err: marketBuy(func(request *PlaceAlgoOrderRequest) {
			request.Side = SideSell
			request.Size = "1"
		})},
		{name: "algo market sell no size", err: marketBuy(func(request *PlaceAlgoOrderRequest) {
			request.Side = SideSell
			request.Value = ""
		})},
		{name: "algo trailing high", err: marketBuy(func(request *PlaceAlgoOrderRequest) { request.TrailingRate = "0.06" })},
		{name: "algo trailing low", err: marketBuy(func(request *PlaceAlgoOrderRequest) { request.TrailingRate = "0.0005" })},
		{name: "algo cancel empty", err: (CancelAlgoOrdersRequest{}).validate()},
		{name: "algo cancel too many", err: (CancelAlgoOrdersRequest{ClientOrderIDs: orderIDs}).validate()},
		{name: "algo cancel duplicate", err: (CancelAlgoOrdersRequest{ClientOrderIDs: []string{"a", "a"}}).validate()},
		{name: "algo cancel invalid", err: (CancelAlgoOrdersRequest{ClientOrderIDs: []string{"bad id"}}).validate()},
		{name: "algo open account", err: (OpenAlgoOrdersRequest{AccountID: "x"}).validate()},
		{name: "algo open symbol", err: (OpenAlgoOrdersRequest{Symbol: "BTC"}).validate()},
		{name: "algo open side", err: (OpenAlgoOrdersRequest{Side: "hold"}).validate()},
		{name: "algo open type", err: (OpenAlgoOrdersRequest{Type: "stop"}).validate()},
		{name: "algo open sort", err: (OpenAlgoOrdersRequest{Sort: "up"}).validate()},
		{name: "algo open limit", err: (OpenAlgoOrdersRequest{Limit: 501}).validate()},
		{name: "algo open negative limit", err: (OpenAlgoOrdersRequest{Limit: -1}).validate()},
		{name: "algo open cursor", err: (OpenAlgoOrdersRequest{FromID: "abc"}).validate()},
		{name: "algo history symbol", err: (AlgoOrderHistoryRequest{Status: AlgoOrderStatusCanceled}).validate()},
		{name: "algo history status", err: (AlgoOrderHistoryRequest{Symbol: "adausdt"}).validate()},
		{name: "algo history open status", err: (AlgoOrderHistoryRequest{
			Symbol: "adausdt", Status: AlgoOrderStatusCreated,
		}).validate()},
		{name: "algo history window", err: (AlgoOrderHistoryRequest{
			Symbol: "adausdt", Status: AlgoOrderStatusCanceled, Start: &end, End: &start,
		}).validate()},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, trade.ErrValidation) {
				t.Fatalf("validation error = %v", test.err)
			}
		})
	}
	client, _ := newTestClient(t, "http://127.0.0.1", &directSender{})
	if _, err := client.AlgoOrder(context.Background(), "bad id"); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("AlgoOrder() client order ID error = %v, want validation", err)
	}
}
