package cryptocom

import (
	"context"
	"errors"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

func advancedOrderJSON() string {
	return `{"account_id":"account-1","order_id":"18342311","client_oid":"sl-1","order_type":"STOP_LOSS","time_in_force":"GOOD_TILL_CANCEL","side":"SELL","quantity":"0.1","limit_price":"0","status":"ACTIVE","instrument_name":"BTC_USDT","create_time":"1700000000000","list_id":"6498090546073120100","contingency_type":"OCO","leg_id":"1","ref_price":"45000.0","ref_price_type":"MARK_PRICE","exchange_order_id":"77"}`
}

func stopLoss(clientOrderID string) AdvancedOrder {
	return AdvancedOrder{
		InstrumentName: "BTC_USDT", Side: OrderSideSell, Type: AdvancedOrderStopLoss,
		Quantity: "0.1", RefPrice: "45000", ClientOrderID: clientOrderID,
	}
}

func takeProfit(clientOrderID string) AdvancedOrder {
	return AdvancedOrder{
		InstrumentName: "BTC_USDT", Side: OrderSideSell, Type: AdvancedOrderTakeProfitLimit,
		Quantity: "0.1", Price: "56000", RefPrice: "55000", ClientOrderID: clientOrderID,
	}
}

func primaryLimit(clientOrderID string) AdvancedOrder {
	return AdvancedOrder{
		InstrumentName: "BTC_USDT", Side: OrderSideBuy, Type: AdvancedOrderLimit,
		Quantity: "0.1", Price: "50000", ClientOrderID: clientOrderID,
	}
}

func TestClientAdvancedOrderMutations(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_700_000_000_000)
	server, captured := newExtServer(t, now, map[string]string{
		methodAdvancedCreateOrder:     `{"client_oid":"sl-1","order_id":"18342311"}`,
		methodAdvancedAmendOrder:      `{"client_oid":"sl-1","order_id":"18342311"}`,
		methodAdvancedCreateOCO:       `{"list_id":"6498090546073120100"}`,
		methodAdvancedCancelOCO:       "",
		methodAdvancedCreateOTO:       `{"list_id":"6498090546073120101"}`,
		methodAdvancedCancelOTO:       "",
		methodAdvancedCreateOTOCO:     `{"list_id":"6498090546073120102"}`,
		methodAdvancedCancelOTOCO:     "",
		methodAdvancedCancelOrder:     `{"client_oid":"sl-1","order_id":"18342311"}`,
		methodAdvancedCancelAllOrders: "",
		methodAdvancedCreateOrderList: `[{"index":0,"code":0,"order_id":"289991951579667814","client_oid":"sl-1"},{"index":1,"code":40001,"message":"BAD_REQUEST"}]`,
		methodAdvancedCancelOrderList: `[{"index":0,"code":0}]`,
	})
	client := newExtClient(t, server, now, credential.PermissionTrade)
	ctx := context.Background()

	created, err := client.CreateAdvancedOrder(ctx, CreateAdvancedOrderRequest{Order: stopLoss("sl-1")})
	params := captured.envelope(methodAdvancedCreateOrder).Params
	if err != nil || created.OrderID != "18342311" || stringParam(params, "type") != "STOP_LOSS" ||
		stringParam(params, "ref_price") != "45000" || stringParam(params, "quantity") != "0.1" ||
		stringParam(params, "price") != "" || stringParam(params, "client_oid") != "sl-1" {
		t.Fatalf("CreateAdvancedOrder() = %+v, params = %+v, error = %v", created, params, err)
	}

	amended, err := client.AmendAdvancedOrder(ctx, AmendAdvancedOrderRequest{
		OrderID: "18342311", NewRefPrice: "46000", NewQuantity: "0.15", NewPrice: "0",
	})
	params = captured.envelope(methodAdvancedAmendOrder).Params
	if err != nil || amended.OrderID != "18342311" || stringParam(params, "new_ref_price") != "46000" ||
		stringParam(params, "new_quantity") != "0.15" || stringParam(params, "new_price") != "0" {
		t.Fatalf("AmendAdvancedOrder() = %+v, params = %+v, error = %v", amended, params, err)
	}

	oco, err := client.CreateOCO(ctx, CreateOCORequest{AdvancedListRequest{
		Orders: []AdvancedOrder{primaryLimit("tp"), stopLoss("sl")},
	}})
	orders := listParam(t, captured.envelope(methodAdvancedCreateOCO).Params, "order_list")
	if err != nil || oco.ListID != "6498090546073120100" || len(orders) != 2 ||
		stringParam(orders[0], "type") != "LIMIT" || stringParam(orders[1], "type") != "STOP_LOSS" {
		t.Fatalf("CreateOCO() = %+v, orders = %+v, error = %v", oco, orders, err)
	}
	oto, err := client.CreateOTO(ctx, CreateOTORequest{AdvancedListRequest{
		Orders: []AdvancedOrder{primaryLimit("entry"), stopLoss("sl")},
	}})
	if err != nil || oto.ListID != "6498090546073120101" {
		t.Fatalf("CreateOTO() = %+v, error = %v", oto, err)
	}
	otoco, err := client.CreateOTOCO(ctx, CreateOTOCORequest{AdvancedListRequest{
		Orders: []AdvancedOrder{primaryLimit("entry"), stopLoss("sl"), takeProfit("tp")},
	}})
	orders = listParam(t, captured.envelope(methodAdvancedCreateOTOCO).Params, "order_list")
	if err != nil || otoco.ListID != "6498090546073120102" || len(orders) != 3 ||
		stringParam(orders[2], "type") != "TAKE_PROFIT_LIMIT" || stringParam(orders[2], "price") != "56000" {
		t.Fatalf("CreateOTOCO() = %+v, orders = %+v, error = %v", otoco, orders, err)
	}

	for method, call := range map[string]func(CancelAdvancedListRequest, ...trade.RequestOption) (Acknowledgement, error){
		methodAdvancedCancelOCO: func(request CancelAdvancedListRequest, options ...trade.RequestOption) (Acknowledgement, error) {
			return client.CancelOCO(ctx, request, options...)
		},
		methodAdvancedCancelOTO: func(request CancelAdvancedListRequest, options ...trade.RequestOption) (Acknowledgement, error) {
			return client.CancelOTO(ctx, request, options...)
		},
		methodAdvancedCancelOTOCO: func(request CancelAdvancedListRequest, options ...trade.RequestOption) (Acknowledgement, error) {
			return client.CancelOTOCO(ctx, request, options...)
		},
	} {
		if _, err := call(CancelAdvancedListRequest{ListID: "6498090546073120100"}); err != nil ||
			stringParam(captured.envelope(method).Params, "list_id") != "6498090546073120100" {
			t.Fatalf("%s error = %v, params = %+v", method, err, captured.envelope(method).Params)
		}
	}

	canceled, err := client.CancelAdvancedOrder(ctx, CancelAdvancedOrderRequest{ClientOrderID: "sl-1"})
	if err != nil || canceled.OrderID != "18342311" || canceled.ClientOrderID != "sl-1" ||
		stringParam(captured.envelope(methodAdvancedCancelOrder).Params, "client_oid") != "sl-1" {
		t.Fatalf("CancelAdvancedOrder() = %+v, error = %v", canceled, err)
	}

	if _, err := client.CancelAllAdvancedOrders(ctx, CancelAllAdvancedOrdersRequest{
		InstrumentName: "BTC_USDT", Type: CancelAllTrigger,
	}); err != nil || stringParam(captured.envelope(methodAdvancedCancelAllOrders).Params, "type") != "TRIGGER" {
		t.Fatalf("CancelAllAdvancedOrders() error = %v", err)
	}

	list, err := client.CreateAdvancedOrderList(ctx, AdvancedListRequest{
		Orders: []AdvancedOrder{stopLoss("sl-1"), takeProfit("tp-1")},
	})
	if err != nil || len(list) != 2 || list[0].OrderID != "289991951579667814" || list[1].Code != 40001 {
		t.Fatalf("CreateAdvancedOrderList() = %+v, error = %v", list, err)
	}
	cancelList, err := client.CancelAdvancedOrderList(ctx, CancelAdvancedOrderListRequest{
		Orders: []CancelAdvancedOrderRequest{{OrderID: "18342311"}},
	})
	if err != nil || len(cancelList) != 1 ||
		stringParam(listParam(t, captured.envelope(methodAdvancedCancelOrderList).Params, "order_list")[0], "order_id") != "18342311" {
		t.Fatalf("CancelAdvancedOrderList() = %+v, error = %v", cancelList, err)
	}
}

func TestClientAdvancedOrderQueries(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_700_000_000_000)
	start := time.UnixMilli(1_699_900_000_000)
	end := time.UnixMilli(1_699_986_400_000)
	server, captured := newExtServer(t, now, map[string]string{
		methodAdvancedGetOpenOrders:   `{"data":[` + advancedOrderJSON() + `]}`,
		methodAdvancedGetOrderDetail:  advancedOrderJSON(),
		methodAdvancedGetOrderHistory: `{"data":[` + advancedOrderJSON() + `]}`,
		methodAdvancedGetOrderListDet: `{"data":[` + advancedOrderJSON() + `]}`,
	})
	client := newExtClient(t, server, now, credential.PermissionRead)
	ctx := context.Background()

	open, err := client.AdvancedOpenOrders(ctx, AdvancedOpenOrdersRequest{InstrumentName: "BTC_USDT"})
	if err != nil || len(open) != 1 || open[0].ListID != "6498090546073120100" ||
		open[0].ContingencyType != "OCO" || open[0].RefPrice != "45000.0" ||
		open[0].Status != OrderStatusActive || open[0].Type != "STOP_LOSS" || len(open[0].Raw) == 0 ||
		stringParam(captured.envelope(methodAdvancedGetOpenOrders).Params, "instrument_name") != "BTC_USDT" {
		t.Fatalf("AdvancedOpenOrders() = %+v, error = %v", open, err)
	}

	detail, err := client.AdvancedOrderInfo(ctx, AdvancedOrderInfoRequest{OrderID: "18342311"})
	if err != nil || detail.ClientOrderID != "sl-1" || detail.ExchangeOrderID != "77" {
		t.Fatalf("AdvancedOrderInfo() = %+v, error = %v", detail, err)
	}
	if _, err := client.AdvancedOrderInfo(ctx, AdvancedOrderInfoRequest{OrderID: "999"}); err == nil {
		t.Fatal("AdvancedOrderInfo(mismatched order) error = nil")
	}

	history, err := client.AdvancedOrderHistory(ctx, AdvancedOrderHistoryRequest{
		InstrumentName: "BTC_USDT", Start: &start, End: &end, Limit: 50,
	})
	params := captured.envelope(methodAdvancedGetOrderHistory).Params
	if err != nil || len(history) != 1 || stringParam(params, "start_time") != "1699900000000000000" ||
		stringParam(params, "end_time") != "1699986400000000000" || stringParam(params, "limit") != "50" {
		t.Fatalf("AdvancedOrderHistory() = %+v, params = %+v, error = %v", history, params, err)
	}

	byList, err := client.AdvancedOrderListDetail(ctx, AdvancedOrderListDetailRequest{
		ListIDs: []string{"6498090546073120100"},
	})
	listIDs, _ := captured.envelope(methodAdvancedGetOrderListDet).Params["list_id_list"].([]any)
	if err != nil || len(byList) != 1 || len(listIDs) != 1 || listIDs[0] != "6498090546073120100" {
		t.Fatalf("AdvancedOrderListDetail() = %+v, list ids = %+v, error = %v", byList, listIDs, err)
	}
}

func TestAdvancedOrderValidation(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_700_000_000_000)
	server, captured := newExtServer(t, now, map[string]string{})
	client := newExtClient(t, server, now, credential.PermissionTrade, credential.PermissionRead)
	ctx := context.Background()

	marketTrigger := stopLoss("a")
	marketTrigger.Price = "1"
	noRef := stopLoss("a")
	noRef.RefPrice = ""
	limitWithoutPrice := takeProfit("a")
	limitWithoutPrice.Price = ""
	limitOrder := primaryLimit("a")
	oco := []AdvancedOrder{primaryLimit("a"), stopLoss("b")}
	ocoWrongRefType := stopLoss("b")
	ocoWrongRefType.RefPriceType = RefPriceLast

	tests := []struct {
		name string
		call func() error
	}{
		{"create non-trigger", func() error {
			_, err := client.CreateAdvancedOrder(ctx, CreateAdvancedOrderRequest{Order: limitOrder})
			return err
		}},
		{"stop loss with price", func() error {
			_, err := client.CreateAdvancedOrder(ctx, CreateAdvancedOrderRequest{Order: marketTrigger})
			return err
		}},
		{"trigger without ref price", func() error {
			_, err := client.CreateAdvancedOrder(ctx, CreateAdvancedOrderRequest{Order: noRef})
			return err
		}},
		{"stop limit without price", func() error {
			_, err := client.CreateAdvancedOrder(ctx, CreateAdvancedOrderRequest{Order: limitWithoutPrice})
			return err
		}},
		{"oco two limits", func() error {
			_, err := client.CreateOCO(ctx, CreateOCORequest{AdvancedListRequest{
				Orders: []AdvancedOrder{primaryLimit("a"), primaryLimit("b")},
			}})
			return err
		}},
		{"oco last price", func() error {
			_, err := client.CreateOCO(ctx, CreateOCORequest{AdvancedListRequest{
				Orders: []AdvancedOrder{oco[0], ocoWrongRefType},
			}})
			return err
		}},
		{"oto wrong count", func() error {
			_, err := client.CreateOTO(ctx, CreateOTORequest{AdvancedListRequest{Orders: oco[:1]}})
			return err
		}},
		{"otoco two stops", func() error {
			_, err := client.CreateOTOCO(ctx, CreateOTOCORequest{AdvancedListRequest{
				Orders: []AdvancedOrder{primaryLimit("a"), stopLoss("b"), stopLoss("c")},
			}})
			return err
		}},
		{"cancel list bad id", func() error {
			_, err := client.CancelOCO(ctx, CancelAdvancedListRequest{ListID: "x"})
			return err
		}},
		{"cancel all position type with limit", func() error {
			_, err := client.CancelAllAdvancedOrders(ctx, CancelAllAdvancedOrdersRequest{
				InstrumentName: "BTCUSD-PERP", Type: CancelAllLimit, PositionType: "CROSS_MARGIN",
			})
			return err
		}},
		{"amend negative price", func() error {
			_, err := client.AmendAdvancedOrder(ctx, AmendAdvancedOrderRequest{
				OrderID: "1", NewQuantity: "1", NewPrice: "-1",
			})
			return err
		}},
		{"order list detail empty", func() error {
			_, err := client.AdvancedOrderListDetail(ctx, AdvancedOrderListDetailRequest{})
			return err
		}},
		{"advanced list with non-trigger", func() error {
			_, err := client.CreateAdvancedOrderList(ctx, AdvancedListRequest{Orders: []AdvancedOrder{limitOrder}})
			return err
		}},
	}
	for _, test := range tests {
		if err := test.call(); err == nil {
			t.Fatalf("%s: error = nil", test.name)
		}
	}
	if len(captured.envelopes) != 0 {
		t.Fatalf("invalid requests reached the server: %+v", captured.envelopes)
	}

	readOnly := newExtClient(t, server, now, credential.PermissionRead)
	_, err := readOnly.CreateAdvancedOrder(ctx, CreateAdvancedOrderRequest{Order: stopLoss("a")})
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.Category != trade.ErrorAuthorization {
		t.Fatalf("CreateAdvancedOrder(read-only) error = %v, want authorization", err)
	}
}
