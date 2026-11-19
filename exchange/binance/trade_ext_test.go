package binance

import (
	"context"
	"errors"
	"net/http"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
)

func sampleLimitOrder() NewOrderRequest {
	return NewOrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit, TimeInForce: TimeInForceGTC, Quantity: "0.001", Price: "64000.00"}
}

func sampleCancelReplaceRequest() CancelReplaceOrderRequest {
	cancelID := int64(9)
	return CancelReplaceOrderRequest{Mode: CancelReplaceModeStopOnFailure, CancelOrderID: &cancelID, NewOrder: sampleLimitOrder()}
}

var sampleLimitOrderParams = []string{
	"symbol=BTCUSDT",
	"side=BUY",
	"type=LIMIT",
	"timeInForce=GTC",
	"quantity=0.001",
	"price=64000.00",
}

func TestTestOrderWithoutCommissionRates(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `{}`)
	result, err := harness.client.TestOrder(context.Background(), TestOrderRequest{Order: sampleLimitOrder()})
	if err != nil {
		t.Fatalf("TestOrder() error = %v", err)
	}
	if result.StandardCommissionForOrder != nil || result.Discount != nil || string(result.Raw) != `{}` {
		t.Fatalf("TestOrder() = %+v, want an empty result", result)
	}
	harness.expect(t, http.MethodPost, "/api/v3/order/test", true, 1, 0, sampleLimitOrderParams...)
}

func TestTestOrderWithCommissionRates(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `{"standardCommissionForOrder":{"maker":"0.00000112","taker":"0.00000114"},"specialCommissionForOrder":{"maker":"0.05000000","taker":"0.06000000"},"taxCommissionForOrder":{"maker":"0.00000112","taker":"0.00000114"},"discount":{"enabledForAccount":true,"enabledForSymbol":true,"discountAsset":"BNB","discount":"0.25000000"}}`)
	result, err := harness.client.TestOrder(context.Background(), TestOrderRequest{Order: sampleLimitOrder(), ComputeCommissionRates: true})
	if err != nil {
		t.Fatalf("TestOrder() error = %v", err)
	}
	if result.StandardCommissionForOrder == nil || result.StandardCommissionForOrder.Maker != "0.00000112" ||
		result.SpecialCommissionForOrder == nil || result.SpecialCommissionForOrder.Taker != "0.06000000" ||
		result.TaxCommissionForOrder == nil || result.Discount == nil || result.Discount.DiscountAsset != "BNB" || !result.Discount.EnabledForAccount {
		t.Fatalf("TestOrder() = %+v", result)
	}
	params := append([]string{"computeCommissionRates=true"}, sampleLimitOrderParams...)
	harness.expect(t, http.MethodPost, "/api/v3/order/test", true, 20, 0, params...)
}

func TestTestOrderRejectsInvalidOrder(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `{}`)
	_, err := harness.client.TestOrder(context.Background(), TestOrderRequest{Order: NewOrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit}})
	harness.requireValidation(t, "TestOrder", err)
}

func TestCancelOpenOrdersSplitsOrdersAndOrderLists(t *testing.T) {
	t.Parallel()

	const body = `[` +
		`{"symbol":"BTCUSDT","origClientOrderId":"E6APeyTJvkMvLMYMqu1KQ4","orderId":11,"orderListId":-1,"clientOrderId":"pXLV6Hz6mprAcVYpVMTGgx","transactTime":1684804350068,"price":"0.089853","origQty":"0.178622","executedQty":"0.000000","origQuoteOrderQty":"0.000000","cummulativeQuoteQty":"0.000000","status":"CANCELED","timeInForce":"GTC","type":"LIMIT","side":"BUY","selfTradePreventionMode":"NONE"},` +
		`{"symbol":"BTCUSDT","origClientOrderId":"A3EF2HCwxgZPFMrfwbgrhv","orderId":13,"orderListId":-1,"clientOrderId":"pXLV6Hz6mprAcVYpVMTGgx","transactTime":1684804350069,"price":"0.090430","origQty":"0.178622","executedQty":"0.000000","origQuoteOrderQty":"0.000000","cummulativeQuoteQty":"0.000000","status":"CANCELED","timeInForce":"GTC","type":"LIMIT","side":"BUY","selfTradePreventionMode":"NONE"},` +
		`{"orderListId":1929,"contingencyType":"OCO","listStatusType":"ALL_DONE","listOrderStatus":"ALL_DONE","listClientOrderId":"2inzWQdDvZLHbbAmAozX2N","transactionTime":1585230948299,"symbol":"BTCUSDT","orders":[{"symbol":"BTCUSDT","orderId":20,"clientOrderId":"CwOOIPHSmYywx6jZX77TdL"},{"symbol":"BTCUSDT","orderId":21,"clientOrderId":"461cPg51vQjV3zIMOXNz39"}],"orderReports":[` +
		`{"symbol":"BTCUSDT","origClientOrderId":"CwOOIPHSmYywx6jZX77TdL","orderId":20,"orderListId":1929,"clientOrderId":"pXLV6Hz6mprAcVYpVMTGgx","transactTime":1688005070874,"price":"0.668611","origQty":"0.690354","executedQty":"0.000000","status":"CANCELED","timeInForce":"GTC","type":"STOP_LOSS_LIMIT","side":"BUY","stopPrice":"0.378131","icebergQty":"0.017083","selfTradePreventionMode":"NONE"},` +
		`{"symbol":"BTCUSDT","origClientOrderId":"461cPg51vQjV3zIMOXNz39","orderId":21,"orderListId":1929,"clientOrderId":"pXLV6Hz6mprAcVYpVMTGgx","transactTime":1688005070874,"price":"0.008791","origQty":"0.690354","executedQty":"0.000000","status":"CANCELED","timeInForce":"GTC","type":"LIMIT_MAKER","side":"BUY","icebergQty":"0.639962","selfTradePreventionMode":"NONE"}]}]`
	harness := newExtensionHarness(t, http.StatusOK, body)
	result, err := harness.client.CancelOpenOrders(context.Background(), CancelOpenOrdersRequest{Symbol: "BTCUSDT"})
	if err != nil {
		t.Fatalf("CancelOpenOrders() error = %v", err)
	}
	if len(result.Orders) != 2 || result.Orders[1].OrderID != 13 || result.Orders[0].Status != OrderStatusCanceled || len(result.Orders[0].Raw) == 0 {
		t.Fatalf("CancelOpenOrders() orders = %+v", result.Orders)
	}
	if len(result.OrderLists) != 1 {
		t.Fatalf("CancelOpenOrders() order lists = %+v", result.OrderLists)
	}
	orderList := result.OrderLists[0]
	if orderList.OrderListID != 1929 || orderList.ContingencyType != ContingencyTypeOCO || orderList.ListOrderStatus != ListOrderStatusAllDone ||
		len(orderList.Orders) != 2 || len(orderList.OrderReports) != 2 || orderList.OrderReports[0].Type != OrderTypeStopLossLimit || len(orderList.Raw) == 0 {
		t.Fatalf("CancelOpenOrders() order list = %+v", orderList)
	}
	if string(result.Raw) != body {
		t.Fatal("CancelOpenOrders() did not preserve the raw response")
	}
	harness.expect(t, http.MethodDelete, "/api/v3/openOrders", true, 1, 0, "symbol=BTCUSDT")
}

func TestCancelOpenOrdersRequiresSymbol(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `[]`)
	_, err := harness.client.CancelOpenOrders(context.Background(), CancelOpenOrdersRequest{})
	harness.requireValidation(t, "CancelOpenOrders", err)
}

func TestCancelReplaceOrderSuccess(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `{"cancelResult":"SUCCESS","newOrderResult":"SUCCESS","cancelResponse":{"symbol":"BTCUSDT","origClientOrderId":"DnLo3vTAQcjha43lAZhZ0y","orderId":9,"orderListId":-1,"clientOrderId":"cancel-1","transactTime":1684804350068,"price":"0.01000000","origQty":"0.000100","executedQty":"0.00000000","status":"CANCELED","timeInForce":"GTC","type":"LIMIT","side":"SELL","selfTradePreventionMode":"NONE"},"newOrderResponse":{"symbol":"BTCUSDT","orderId":10,"orderListId":-1,"clientOrderId":"wOceeeOzNORyLiQfw7jd8S","transactTime":1652928801803,"price":"64000.00","origQty":"0.001","executedQty":"0.00000000","status":"NEW","timeInForce":"GTC","type":"LIMIT","side":"BUY","workingTime":1669277163808,"fills":[],"selfTradePreventionMode":"NONE"}}`)
	request := sampleCancelReplaceRequest()
	request.RateLimitExceededMode = OrderRateLimitExceededModeCancelOnly
	request.CancelNewClientOrderID = "cancel-1"
	request.CancelRestriction = CancelOnlyNew
	result, err := harness.client.CancelReplaceOrder(context.Background(), request)
	if err != nil {
		t.Fatalf("CancelReplaceOrder() error = %v", err)
	}
	if result.CancelResult != CancelReplaceOutcomeSuccess || result.NewOrderResult != CancelReplaceOutcomeSuccess {
		t.Fatalf("CancelReplaceOrder() outcomes = %s/%s", result.CancelResult, result.NewOrderResult)
	}
	if result.CancelResponse.Order == nil || result.CancelResponse.Order.OrderID != 9 || result.CancelResponse.Order.Status != OrderStatusCanceled ||
		result.NewOrderResponse.Order == nil || result.NewOrderResponse.Order.OrderID != 10 || result.NewOrderResponse.Order.Status != OrderStatusNew ||
		len(result.NewOrderResponse.Raw) == 0 || len(result.Raw) == 0 {
		t.Fatalf("CancelReplaceOrder() = %+v", result)
	}
	params := append([]string{
		"cancelReplaceMode=STOP_ON_FAILURE",
		"orderRateLimitExceededMode=CANCEL_ONLY",
		"cancelOrderId=9",
		"cancelNewClientOrderId=cancel-1",
		"cancelRestrictions=ONLY_NEW",
	}, sampleLimitOrderParams...)
	harness.expect(t, http.MethodPost, "/api/v3/order/cancelReplace", true, 1, 1, params...)
}

func TestCancelReplaceOrderPartialFailureReturnsResultWithError(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusConflict, `{"code":-2021,"msg":"Order cancel-replace partially failed.","data":{"cancelResult":"SUCCESS","newOrderResult":"FAILURE","cancelResponse":{"symbol":"BTCUSDT","origClientOrderId":"86M8erehfExV8z2RC8Zo8k","orderId":3,"orderListId":-1,"clientOrderId":"G1kLo6aDv2KGNTFcjfTSFq","transactTime":1684804350068,"price":"0.006123","origQty":"10000.000000","executedQty":"0.000000","status":"CANCELED","timeInForce":"GTC","type":"LIMIT_MAKER","side":"SELL","selfTradePreventionMode":"NONE"},"newOrderResponse":{"code":-2010,"msg":"Order would immediately match and take."}}}`)
	result, err := harness.client.CancelReplaceOrder(context.Background(), sampleCancelReplaceRequest())
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.ExchangeCode != "-2021" || apiError.HTTPStatus != http.StatusConflict {
		t.Fatalf("CancelReplaceOrder() error = %v, want APIError -2021 with HTTP 409", err)
	}
	if result.CancelResult != CancelReplaceOutcomeSuccess || result.NewOrderResult != CancelReplaceOutcomeFailure {
		t.Fatalf("CancelReplaceOrder() outcomes = %s/%s", result.CancelResult, result.NewOrderResult)
	}
	if result.CancelResponse.Order == nil || result.CancelResponse.Order.OrderID != 3 {
		t.Fatalf("CancelReplaceOrder() cancel response = %+v", result.CancelResponse)
	}
	if result.NewOrderResponse.Order != nil || result.NewOrderResponse.Code != -2010 || result.NewOrderResponse.Message != "Order would immediately match and take." {
		t.Fatalf("CancelReplaceOrder() new order response = %+v", result.NewOrderResponse)
	}
	if count := harness.requestCount(); count != 1 {
		t.Fatalf("CancelReplaceOrder() sent %d requests, want 1 without a retry", count)
	}
}

func TestCancelReplaceOrderCancelFailureLeavesNewOrderEmpty(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusBadRequest, `{"code":-2022,"msg":"Order cancel-replace failed.","data":{"cancelResult":"FAILURE","newOrderResult":"NOT_ATTEMPTED","cancelResponse":{"code":-2011,"msg":"Unknown order sent."},"newOrderResponse":null}}`)
	result, err := harness.client.CancelReplaceOrder(context.Background(), sampleCancelReplaceRequest())
	if !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("CancelReplaceOrder() error = %v, want ErrValidation", err)
	}
	if result.CancelResult != CancelReplaceOutcomeFailure || result.NewOrderResult != CancelReplaceOutcomeNotAttempted {
		t.Fatalf("CancelReplaceOrder() outcomes = %s/%s", result.CancelResult, result.NewOrderResult)
	}
	if result.CancelResponse.Order != nil || result.CancelResponse.Code != -2011 || result.NewOrderResponse.Order != nil || result.NewOrderResponse.Code != 0 {
		t.Fatalf("CancelReplaceOrder() = %+v", result)
	}
}

func TestCancelReplaceOrderErrorWithoutStageResult(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusBadRequest, `{"code":-1015,"msg":"Too many new orders; current limit is 50 orders per 10 SECOND."}`)
	result, err := harness.client.CancelReplaceOrder(context.Background(), sampleCancelReplaceRequest())
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.ExchangeCode != "-1015" {
		t.Fatalf("CancelReplaceOrder() error = %v, want APIError -1015", err)
	}
	if result.CancelResult != "" || result.NewOrderResult != "" || len(result.Raw) != 0 {
		t.Fatalf("CancelReplaceOrder() = %+v, want an empty result", result)
	}
}

func TestCancelReplaceOrderValidation(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `{}`)
	ctx := context.Background()
	request := sampleCancelReplaceRequest()
	request.Mode = ""
	_, err := harness.client.CancelReplaceOrder(ctx, request)
	harness.requireValidation(t, "missing mode", err)

	request = sampleCancelReplaceRequest()
	request.CancelOrderID = nil
	_, err = harness.client.CancelReplaceOrder(ctx, request)
	harness.requireValidation(t, "missing cancel identity", err)

	request = sampleCancelReplaceRequest()
	request.RateLimitExceededMode = "IGNORE"
	_, err = harness.client.CancelReplaceOrder(ctx, request)
	harness.requireValidation(t, "unsupported rate limit mode", err)

	request = sampleCancelReplaceRequest()
	request.CancelRestriction = "ONLY_FILLED"
	_, err = harness.client.CancelReplaceOrder(ctx, request)
	harness.requireValidation(t, "unsupported restriction", err)

	request = sampleCancelReplaceRequest()
	request.CancelNewClientOrderID = "has a space"
	_, err = harness.client.CancelReplaceOrder(ctx, request)
	harness.requireValidation(t, "invalid cancel client ID", err)

	request = sampleCancelReplaceRequest()
	request.NewOrder.Price = ""
	_, err = harness.client.CancelReplaceOrder(ctx, request)
	harness.requireValidation(t, "invalid new order", err)
}

func TestAmendOrderKeepPriority(t *testing.T) {
	t.Parallel()

	const single = `{"transactTime":1741926410255,"executionId":75,"amendedOrder":{"symbol":"BTCUSDT","orderId":33,"orderListId":-1,"origClientOrderId":"5xrgbMyg6z36NzBn2pbT8H","clientOrderId":"PFaq6hIHxqFENGfdtn4J6Q","price":"6.00000000","qty":"5.00000000","executedQty":"0.00000000","preventedQty":"0.00000000","quoteOrderQty":"0.00000000","cumulativeQuoteQty":"0.00000000","status":"NEW","timeInForce":"GTC","type":"LIMIT","side":"SELL","workingTime":1741926410242,"selfTradePreventionMode":"NONE"}}`
	t.Run("single order", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, single)
		orderID := int64(33)
		result, err := harness.client.AmendOrderKeepPriority(context.Background(), AmendOrderRequest{Symbol: "BTCUSDT", OrderID: &orderID, NewClientOrderID: "amend-1", NewQuantity: "5"})
		if err != nil {
			t.Fatalf("AmendOrderKeepPriority() error = %v", err)
		}
		amended := result.AmendedOrder
		if result.ExecutionID != 75 || result.TransactionTime != 1741926410255 || result.ListStatus != nil || len(result.Raw) == 0 {
			t.Fatalf("AmendOrderKeepPriority() = %+v", result)
		}
		if amended.OrderID != 33 || amended.Quantity != "5.00000000" || amended.PreventedQuantity != "0.00000000" || amended.Status != OrderStatusNew || amended.WorkingTime != 1741926410242 {
			t.Fatalf("AmendOrderKeepPriority() amended order = %+v", amended)
		}
		harness.expect(t, http.MethodPut, "/api/v3/order/amend/keepPriority", true, 4, 0, "symbol=BTCUSDT", "orderId=33", "newClientOrderId=amend-1", "newQty=5")
	})
	t.Run("order in an order list", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, single[:len(single)-1]+`,"listStatus":{"orderListId":1,"contingencyType":"OTO","listOrderStatus":"EXECUTING","listClientOrderId":"AT7FTxZXylVSwRoZs52mt3","symbol":"BTCUSDT","orders":[{"symbol":"BTCUSDT","orderId":8,"clientOrderId":"GkwwHZUUbFtZOoH1YsZk9Q"},{"symbol":"BTCUSDT","orderId":9,"clientOrderId":"UQ1Np3bmQ71jJzsSDW9Vpi"}]}}`)
		result, err := harness.client.AmendOrderKeepPriority(context.Background(), AmendOrderRequest{Symbol: "BTCUSDT", OriginalClientOrderID: "5xrgbMyg6z36NzBn2pbT8H", NewQuantity: "4"})
		if err != nil {
			t.Fatalf("AmendOrderKeepPriority() error = %v", err)
		}
		if result.ListStatus == nil || result.ListStatus.ContingencyType != ContingencyTypeOTO || result.ListStatus.ListOrderStatus != ListOrderStatusExecuting || len(result.ListStatus.Orders) != 2 {
			t.Fatalf("AmendOrderKeepPriority() list status = %+v", result.ListStatus)
		}
		harness.expect(t, http.MethodPut, "/api/v3/order/amend/keepPriority", true, 4, 0, "symbol=BTCUSDT", "origClientOrderId=5xrgbMyg6z36NzBn2pbT8H", "newQty=4")
	})
}

func TestAmendOrderValidation(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `{}`)
	ctx := context.Background()
	orderID := int64(33)
	_, err := harness.client.AmendOrderKeepPriority(ctx, AmendOrderRequest{Symbol: "BTCUSDT", OrderID: &orderID})
	harness.requireValidation(t, "missing newQty", err)
	_, err = harness.client.AmendOrderKeepPriority(ctx, AmendOrderRequest{Symbol: "BTCUSDT", OrderID: &orderID, NewQuantity: "0"})
	harness.requireValidation(t, "zero newQty", err)
	_, err = harness.client.AmendOrderKeepPriority(ctx, AmendOrderRequest{Symbol: "BTCUSDT", NewQuantity: "1"})
	harness.requireValidation(t, "missing order identity", err)
	_, err = harness.client.AmendOrderKeepPriority(ctx, AmendOrderRequest{Symbol: "BTCUSDT", OrderID: &orderID, NewClientOrderID: "has a space", NewQuantity: "1"})
	harness.requireValidation(t, "invalid new client ID", err)
}
