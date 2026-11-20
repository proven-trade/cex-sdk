package kraken

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

func TestAmendOrderSendsFormAndDecodesAmendID(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "AmendOrder": `{"error":[],"result":{"amend_id":"TEZA4R-DSDGT-IJBOJK"}}`,
	})
	client, limiter := newExtClient(t, server.URL)
	amended, err := client.AmendOrder(context.Background(), AmendOrderRequest{
		TransactionID: "OHYO67-6LP66-HMQ437", Pair: "XBTUSD", Quantity: "1.25",
		LimitPrice: "+50", PostOnly: true, Deadline: "2026-01-02T03:04:05Z",
	})
	if err != nil {
		t.Fatalf("AmendOrder() error = %v", err)
	}
	if amended.AmendID != "TEZA4R-DSDGT-IJBOJK" || len(amended.Raw) == 0 {
		t.Fatalf("AmendOrder() = %+v", amended)
	}
	request := server.last(t)
	form := request.form(t)
	if request.Method != http.MethodPost || request.Path != privatePrefix+"AmendOrder" ||
		request.ContentType != formContentType || form.Get("txid") != "OHYO67-6LP66-HMQ437" ||
		form.Get("order_qty") != "1.25" || form.Get("limit_price") != "+50" ||
		form.Get("post_only") != "true" || form.Get("pair") != "XBTUSD" ||
		form.Get("deadline") != "2026-01-02T03:04:05Z" || form.Get("nonce") == "" ||
		form.Get("cl_ord_id") != "" {
		t.Fatalf("AmendOrder request = %+v form = %v", request, form)
	}
	snapshot, err := limiter.Snapshot("kraken:account:kraken-main:trading:XBTUSD:1second")
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("trading limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestAmendOrderByClientOrderIDRejectsEmptyAcknowledgement(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "AmendOrder": `{"error":[],"result":{}}`,
	})
	client, _ := newExtClient(t, server.URL)
	_, err := client.AmendOrder(context.Background(), AmendOrderRequest{
		ClientOrderID: "strategy-1", Quantity: "1",
	})
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("AmendOrder() error = %v, want unknown execution state", err)
	}
	if form := server.last(t).form(t); form.Get("cl_ord_id") != "strategy-1" || form.Get("txid") != "" {
		t.Fatalf("AmendOrder form = %v", form)
	}
}

func TestEditOrderSendsFormAndDecodesReplacement(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "EditOrder": `{"error":[],"result":{"status":"ok","txid":"OFVXHJ-KPQ3B-VS7ELA","originaltxid":"OHYO67-6LP66-HMQ437","volume":"0.00030000","price":"19500.0","price2":"32500.0","orders_cancelled":1,"descr":{"order":"buy 0.00030000 XXBTZGBP @ limit 19500.0"}}}`,
	})
	client, _ := newExtClient(t, server.URL)
	edited, err := client.EditOrder(context.Background(), EditOrderRequest{
		TransactionID: "OHYO67-6LP66-HMQ437", Pair: "XBTGBP", Volume: "0.0003",
		Price: "19500.0", Price2: "32500.0", PostOnly: true, UserReference: 7,
	})
	if err != nil {
		t.Fatalf("EditOrder() error = %v", err)
	}
	if edited.Status != "ok" || edited.TransactionID != "OFVXHJ-KPQ3B-VS7ELA" ||
		edited.OriginalTransactionID != "OHYO67-6LP66-HMQ437" || edited.OrdersCancelled != 1 ||
		edited.Price2 != "32500.0" || edited.Description.Order == "" || len(edited.Raw) == 0 {
		t.Fatalf("EditOrder() = %+v", edited)
	}
	request := server.last(t)
	form := request.form(t)
	if request.Method != http.MethodPost || request.Path != privatePrefix+"EditOrder" ||
		form.Get("txid") != "OHYO67-6LP66-HMQ437" || form.Get("pair") != "XBTGBP" ||
		form.Get("volume") != "0.0003" || form.Get("price") != "19500.0" ||
		form.Get("price2") != "32500.0" || form.Get("oflags") != "post" || form.Get("userref") != "7" {
		t.Fatalf("EditOrder request = %+v form = %v", request, form)
	}
}

func TestAddOrderBatchSendsSignedJSONAndDecodesOrders(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "AddOrderBatch": `{"error":[],"result":{"orders":[{"txid":"O5OR23-ADFAD-Y2G61C","descr":{"order":"buy 0.80300000 XBTUSD @ limit 28300.0","close":"close position @ stop loss 27000.0 -> limit 26000.0"}},{"txid":"9K6KFS-5H3PL-XBRC7A","descr":{"order":"sell 0.10500000 XBTUSD @ limit 36000.0"}}]}}`,
	})
	client, limiter := newExtClient(t, server.URL)
	reference, err := client.AddOrderBatch(context.Background(), AddOrderBatchRequest{
		Pair: "XBTUSD",
		Orders: []BatchOrder{
			{
				ClientOrderID: "batch-1", OrderType: OrderTypeLimit, Side: SideBuy, Volume: "0.803",
				Price: "28300", TimeInForce: TimeInForceGTC, PostOnly: true,
				Close: &BatchOrderClose{OrderType: OrderTypeStopLossLimit, Price: "27000", Price2: "26000"},
			},
			{
				UserReference: 9, OrderType: OrderTypeLimit, Side: SideSell, Volume: "0.105", Price: "36000",
			},
		},
		Deadline: "2026-01-02T03:04:05Z",
	})
	if err != nil {
		t.Fatalf("AddOrderBatch() error = %v", err)
	}
	if len(reference.Orders) != 2 || reference.Orders[0].TransactionID != "O5OR23-ADFAD-Y2G61C" ||
		reference.Orders[0].Description.Close == "" || reference.Orders[1].TransactionID != "9K6KFS-5H3PL-XBRC7A" ||
		len(reference.Raw) == 0 {
		t.Fatalf("AddOrderBatch() = %+v", reference)
	}
	request := server.last(t)
	if request.Method != http.MethodPost || request.Path != privatePrefix+"AddOrderBatch" ||
		request.ContentType != jsonContentType {
		t.Fatalf("AddOrderBatch request = %+v", request)
	}
	body := request.jsonBody(t)
	orders, ok := body["orders"].([]any)
	if !ok || len(orders) != 2 || body["pair"] != "XBTUSD" || body["deadline"] != "2026-01-02T03:04:05Z" {
		t.Fatalf("AddOrderBatch body = %v", body)
	}
	if _, exists := body["validate"]; exists {
		t.Fatalf("AddOrderBatch body includes validate: %v", body)
	}
	first, ok := orders[0].(map[string]any)
	if !ok || first["cl_ord_id"] != "batch-1" || first["ordertype"] != "limit" || first["type"] != "buy" ||
		first["volume"] != "0.803" || first["price"] != "28300" || first["timeinforce"] != "GTC" ||
		first["oflags"] != "post" {
		t.Fatalf("AddOrderBatch first order = %v", orders[0])
	}
	closeBody, ok := first["close"].(map[string]any)
	if !ok || closeBody["ordertype"] != "stop-loss-limit" || closeBody["price"] != "27000" ||
		closeBody["price2"] != "26000" {
		t.Fatalf("AddOrderBatch close = %v", first["close"])
	}
	second, ok := orders[1].(map[string]any)
	if !ok || fmt.Sprint(second["userref"]) != "9" || second["type"] != "sell" {
		t.Fatalf("AddOrderBatch second order = %v", orders[1])
	}
	snapshot, err := limiter.Snapshot("kraken:account:kraken-main:trading:XBTUSD:1second")
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("trading limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestAddOrderBatchRejectsAcknowledgementCountMismatch(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "AddOrderBatch": `{"error":[],"result":{"orders":[{"txid":"O5OR23-ADFAD-Y2G61C"}]}}`,
	})
	client, _ := newExtClient(t, server.URL)
	_, err := client.AddOrderBatch(context.Background(), AddOrderBatchRequest{
		Pair: "XBTUSD",
		Orders: []BatchOrder{
			{OrderType: OrderTypeLimit, Side: SideBuy, Volume: "1", Price: "1"},
			{OrderType: OrderTypeLimit, Side: SideSell, Volume: "1", Price: "2"},
		},
	})
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("AddOrderBatch() error = %v, want unknown execution state", err)
	}
}

func TestCancelOrderBatchSendsIDsAsJSON(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "CancelOrderBatch": `{"error":[],"result":{"count":3}}`,
	})
	client, _ := newExtClient(t, server.URL)
	canceled, err := client.CancelOrderBatch(context.Background(), CancelOrderBatchRequest{
		TransactionIDs: []string{"OP5V2Y-RYKVL-ET3V3B", "OP5V2Y-7YKVL-ET3V3B"},
		ClientOrderIDs: []string{"strategy-1"},
	})
	if err != nil {
		t.Fatalf("CancelOrderBatch() error = %v", err)
	}
	if canceled.Count != 3 || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrderBatch() = %+v", canceled)
	}
	request := server.last(t)
	body := request.jsonBody(t)
	orders, ordersOK := body["orders"].([]any)
	clientIDs, clientIDsOK := body["cl_ord_ids"].([]any)
	if request.Method != http.MethodPost || request.Path != privatePrefix+"CancelOrderBatch" ||
		request.ContentType != jsonContentType || !ordersOK || len(orders) != 2 ||
		orders[1] != "OP5V2Y-7YKVL-ET3V3B" || !clientIDsOK || len(clientIDs) != 1 ||
		clientIDs[0] != "strategy-1" {
		t.Fatalf("CancelOrderBatch request = %+v body = %v", request, body)
	}
}

func TestCancelAllAndCancelAllOrdersAfter(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "CancelAll":            `{"error":[],"result":{"count":4}}`,
		privatePrefix + "CancelAllOrdersAfter": `{"error":[],"result":{"currentTime":"2023-03-24T17:41:56Z","triggerTime":"2023-03-24T17:42:56Z"}}`,
	})
	client, limiter := newExtClient(t, server.URL)

	canceled, err := client.CancelAll(context.Background())
	if err != nil {
		t.Fatalf("CancelAll() error = %v", err)
	}
	if canceled.Count != 4 || len(canceled.Raw) == 0 {
		t.Fatalf("CancelAll() = %+v", canceled)
	}
	request := server.last(t)
	if request.Method != http.MethodPost || request.Path != privatePrefix+"CancelAll" ||
		request.form(t).Get("nonce") == "" {
		t.Fatalf("CancelAll request = %+v", request)
	}

	deadMan, err := client.CancelAllOrdersAfter(
		context.Background(), CancelAllOrdersAfterRequest{Timeout: 60 * time.Second},
	)
	if err != nil {
		t.Fatalf("CancelAllOrdersAfter() error = %v", err)
	}
	if deadMan.CurrentTime != "2023-03-24T17:41:56Z" || deadMan.TriggerTime != "2023-03-24T17:42:56Z" ||
		len(deadMan.Raw) == 0 {
		t.Fatalf("CancelAllOrdersAfter() = %+v", deadMan)
	}
	request = server.last(t)
	if request.Path != privatePrefix+"CancelAllOrdersAfter" || request.form(t).Get("timeout") != "60" {
		t.Fatalf("CancelAllOrdersAfter request = %+v", request)
	}
	snapshot, err := limiter.Snapshot("kraken:account:kraken-main:private-counter")
	if err != nil || snapshot.Used != 2 {
		t.Fatalf("private limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestTradingExtensionsRequireTradePermission(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{})
	client, _ := newExtClient(t, server.URL, credential.PermissionRead)
	if _, err := client.AmendOrder(context.Background(), AmendOrderRequest{
		TransactionID: "OHYO67-6LP66-HMQ437", Quantity: "1",
	}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("AmendOrder() error = %v, want authorization", err)
	}
	if _, err := client.CancelAll(context.Background()); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("CancelAll() error = %v, want authorization", err)
	}
	if _, err := client.CancelAllOrdersAfter(
		context.Background(), CancelAllOrdersAfterRequest{Timeout: time.Minute},
	); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("CancelAllOrdersAfter() error = %v, want authorization", err)
	}
	if server.count() != 0 {
		t.Fatalf("server requests = %d, want 0", server.count())
	}
}

func TestTradingExtensionRequestValidation(t *testing.T) {
	t.Parallel()

	validOrder := BatchOrder{OrderType: OrderTypeLimit, Side: SideBuy, Volume: "1", Price: "10"}
	otherOrder := BatchOrder{OrderType: OrderTypeLimit, Side: SideSell, Volume: "1", Price: "11"}
	manyOrders := make([]BatchOrder, maxBatchOrders+1)
	for index := range manyOrders {
		manyOrders[index] = validOrder
	}
	manyIDs := make([]string, maxBatchCancelIDs+1)
	for index := range manyIDs {
		manyIDs[index] = fmt.Sprintf("ORDER-%d", index)
	}
	invalidOrder := func(change func(order *BatchOrder)) error {
		order := validOrder
		change(&order)
		return (AddOrderBatchRequest{Pair: "XBTUSD", Orders: []BatchOrder{order, otherOrder}}).validate()
	}
	invalid := []error{
		(AmendOrderRequest{Quantity: "1"}).validate(),
		(AmendOrderRequest{TransactionID: "ORDER-1", ClientOrderID: "strategy-1", Quantity: "1"}).validate(),
		(AmendOrderRequest{TransactionID: "ORDER-1"}).validate(),
		(AmendOrderRequest{TransactionID: "ORDER-1", Quantity: "abc"}).validate(),
		(AmendOrderRequest{TransactionID: "ORDER-1", Quantity: "1", PostOnly: true}).validate(),
		(AmendOrderRequest{TransactionID: "ORDER-1", Quantity: "1", Deadline: "tomorrow"}).validate(),
		(EditOrderRequest{Pair: "XBTUSD", Volume: "1"}).validate(),
		(EditOrderRequest{TransactionID: "ORDER-1", Volume: "1"}).validate(),
		(EditOrderRequest{TransactionID: "ORDER-1", Pair: "XBTUSD"}).validate(),
		(EditOrderRequest{TransactionID: "ORDER-1", Pair: "XBTUSD", Price: "1e5"}).validate(),
		(AddOrderBatchRequest{Pair: "XBTUSD", Orders: []BatchOrder{validOrder}}).validate(),
		(AddOrderBatchRequest{Pair: "XBTUSD", Orders: manyOrders}).validate(),
		(AddOrderBatchRequest{Orders: []BatchOrder{validOrder, otherOrder}}).validate(),
		invalidOrder(func(order *BatchOrder) { order.Price = "" }),
		invalidOrder(func(order *BatchOrder) { order.OrderType = OrderTypeStopLossLimit }),
		invalidOrder(func(order *BatchOrder) { order.OrderType = OrderTypeMarket }),
		invalidOrder(func(order *BatchOrder) { order.TimeInForce = TimeInForceGTD }),
		invalidOrder(func(order *BatchOrder) { order.UserReference = 1; order.ClientOrderID = "strategy-1" }),
		invalidOrder(func(order *BatchOrder) { order.Side = "hold" }),
		invalidOrder(func(order *BatchOrder) { order.Close = &BatchOrderClose{OrderType: OrderTypeMarket, Price: "1"} }),
		(AddOrderBatchRequest{Pair: "XBTUSD", Orders: []BatchOrder{
			{ClientOrderID: "same", OrderType: OrderTypeLimit, Side: SideBuy, Volume: "1", Price: "1"},
			{ClientOrderID: "same", OrderType: OrderTypeLimit, Side: SideBuy, Volume: "1", Price: "2"},
		}}).validate(),
		(CancelOrderBatchRequest{}).validate(),
		(CancelOrderBatchRequest{TransactionIDs: manyIDs}).validate(),
		(CancelOrderBatchRequest{TransactionIDs: []string{"ORDER-1", "ORDER-1"}}).validate(),
		(CancelOrderBatchRequest{TransactionIDs: []string{"bad id"}}).validate(),
		(CancelOrderBatchRequest{ClientOrderIDs: []string{""}}).validate(),
		(CancelAllOrdersAfterRequest{Timeout: -time.Second}).validate(),
		(CancelAllOrdersAfterRequest{Timeout: 86400 * time.Second}).validate(),
		(CancelAllOrdersAfterRequest{Timeout: 1500 * time.Millisecond}).validate(),
	}
	for index, err := range invalid {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v, want validation", index, err)
		}
	}
	valid := []error{
		(AmendOrderRequest{ClientOrderID: "strategy-1", LimitPrice: "-5%", PostOnly: true}).validate(),
		(EditOrderRequest{TransactionID: "ORDER-1", Pair: "XBTUSD", PostOnly: true}).validate(),
		(AddOrderBatchRequest{Pair: "XBTUSD", Orders: []BatchOrder{validOrder, otherOrder}}).validate(),
		(CancelOrderBatchRequest{ClientOrderIDs: []string{"strategy-1"}}).validate(),
		(CancelAllOrdersAfterRequest{}).validate(),
	}
	for index, err := range valid {
		if err != nil {
			t.Fatalf("valid request %d error = %v", index, err)
		}
	}
	if values := (CancelAllOrdersAfterRequest{}).values(); strings.TrimSpace(values.Get("timeout")) != "0" {
		t.Fatalf("disable timer values = %v", values)
	}
}
