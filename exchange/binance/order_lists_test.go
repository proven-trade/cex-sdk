package binance

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func sampleOCORequest() OCOOrderListRequest {
	request := OCOOrderListRequest{Symbol: "LTCBTC", ListClientOrderID: "oco-list-1", Side: SideSell, Quantity: "5", ResponseType: NewOrderResponseFull}
	request.Above = OrderListLeg{Type: OrderTypeLimitMaker, Price: "3.00000000"}
	request.Below = OrderListLeg{Type: OrderTypeStopLossLimit, ClientOrderID: "below-1", Price: "1.00000000", StopPrice: "1.00000000", TimeInForce: TimeInForceGTC, IcebergQuantity: "1.00000000"}
	return request
}

func sampleOTORequest() OTOOrderListRequest {
	request := OTOOrderListRequest{Symbol: "LTCBTC", WorkingSide: SideBuy, WorkingQuantity: "1", PendingSide: SideSell, PendingQuantity: "1"}
	request.Working = OrderListLeg{Type: OrderTypeLimit, Price: "1.00000000", TimeInForce: TimeInForceGTC}
	request.Pending = OrderListLeg{Type: OrderTypeLimitMaker, Price: "3.00000000"}
	return request
}

func sampleOTOCORequest() OTOCOOrderListRequest {
	request := OTOCOOrderListRequest{Symbol: "LTCBTC", WorkingSide: SideBuy, WorkingQuantity: "1", PendingSide: SideSell, PendingQuantity: "1"}
	request.Working = OrderListLeg{Type: OrderTypeLimit, Price: "1.00000000", TimeInForce: TimeInForceGTC}
	request.PendingAbove = OrderListLeg{Type: OrderTypeLimitMaker, Price: "3.00000000"}
	request.PendingBelow = OrderListLeg{Type: OrderTypeStopLoss, StopPrice: "0.50000000"}
	return request
}

func sampleOPORequest() OPOOrderListRequest {
	request := OPOOrderListRequest{Symbol: "BTCUSDT", WorkingSide: SideBuy, WorkingQuantity: "1", PendingSide: SideSell}
	request.Working = OrderListLeg{Type: OrderTypeLimit, Price: "60000.00", TimeInForce: TimeInForceGTC}
	request.Pending = OrderListLeg{Type: OrderTypeLimitMaker, Price: "70000.00"}
	return request
}

func sampleOPOCORequest() OPOCOOrderListRequest {
	request := OPOCOOrderListRequest{Symbol: "BTCUSDT", WorkingSide: SideBuy, WorkingQuantity: "1", PendingSide: SideSell}
	request.Working = OrderListLeg{Type: OrderTypeLimit, Price: "60000.00", TimeInForce: TimeInForceGTC}
	request.PendingAbove = OrderListLeg{Type: OrderTypeLimitMaker, Price: "70000.00"}
	request.PendingBelow = OrderListLeg{Type: OrderTypeStopLossLimit, Price: "55000.00", StopPrice: "56000.00", TimeInForce: TimeInForceGTC}
	return request
}

const ocoOrderListJSON = `{"orderListId":2,"contingencyType":"OCO","listStatusType":"EXEC_STARTED","listOrderStatus":"EXECUTING","listClientOrderId":"JYVpp3F0f5CAG15DhtrqLp","transactionTime":1710485608839,"symbol":"LTCBTC","orders":[{"symbol":"LTCBTC","orderId":12,"clientOrderId":"Kk7sqHb9J6mJWTMDVW7Vos"},{"symbol":"LTCBTC","orderId":13,"clientOrderId":"xTXKaGYd4bluPVp78IVRvl"}]}`

const otoOrderListJSON = `{"orderListId":0,"contingencyType":"OTO","listStatusType":"EXEC_STARTED","listOrderStatus":"EXECUTING","listClientOrderId":"yl2ERtcar1o25zcWtqVBTC","transactionTime":1712289389158,"symbol":"LTCBTC","orders":[{"symbol":"LTCBTC","orderId":4,"clientOrderId":"Bq17mn9fP6vyCn75Jw1xya"},{"symbol":"LTCBTC","orderId":5,"clientOrderId":"arLFo0zGJVDE69cvGBaU0d"}]}`

func requireOTOOrderList(t *testing.T, orderList OrderList) {
	t.Helper()
	if orderList.OrderListID != 0 || orderList.ContingencyType != ContingencyTypeOTO || orderList.ListStatusType != ListStatusTypeExecStarted ||
		orderList.ListOrderStatus != ListOrderStatusExecuting || orderList.ListClientOrderID != "yl2ERtcar1o25zcWtqVBTC" ||
		len(orderList.Orders) != 2 || orderList.Orders[1].OrderID != 5 || len(orderList.Raw) == 0 {
		t.Fatalf("order list = %+v", orderList)
	}
}

func TestNewOCOOrderList(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `{"orderListId":1,"contingencyType":"OCO","listStatusType":"EXEC_STARTED","listOrderStatus":"EXECUTING","listClientOrderId":"lH1YDkuQKWiXVXHPSKYEIp","transactionTime":1710485608839,"symbol":"LTCBTC","orders":[{"symbol":"LTCBTC","orderId":10,"clientOrderId":"44nZvqpemY7sVYgPYbvPih"},{"symbol":"LTCBTC","orderId":11,"clientOrderId":"NuMp0nVYnciDiFmVqfpBqK"}],"orderReports":[{"symbol":"LTCBTC","orderId":10,"orderListId":1,"clientOrderId":"44nZvqpemY7sVYgPYbvPih","transactTime":1710485608839,"price":"1.00000000","origQty":"5.00000000","executedQty":"0.00000000","origQuoteOrderQty":"0.000000","cummulativeQuoteQty":"0.00000000","status":"NEW","timeInForce":"GTC","type":"STOP_LOSS_LIMIT","side":"SELL","stopPrice":"1.00000000","workingTime":-1,"icebergQty":"1.00000000","selfTradePreventionMode":"NONE"},{"symbol":"LTCBTC","orderId":11,"orderListId":1,"clientOrderId":"NuMp0nVYnciDiFmVqfpBqK","transactTime":1710485608839,"price":"3.00000000","origQty":"5.00000000","executedQty":"0.00000000","origQuoteOrderQty":"0.000000","cummulativeQuoteQty":"0.00000000","status":"NEW","timeInForce":"GTC","type":"LIMIT_MAKER","side":"SELL","workingTime":1710485608839,"selfTradePreventionMode":"NONE"}]}`)
	orderList, err := harness.client.NewOCOOrderList(context.Background(), sampleOCORequest())
	if err != nil {
		t.Fatalf("NewOCOOrderList() error = %v", err)
	}
	if orderList.OrderListID != 1 || orderList.ContingencyType != ContingencyTypeOCO || orderList.ListStatusType != ListStatusTypeExecStarted ||
		orderList.ListOrderStatus != ListOrderStatusExecuting || len(orderList.Orders) != 2 || len(orderList.OrderReports) != 2 || len(orderList.Raw) == 0 {
		t.Fatalf("NewOCOOrderList() = %+v", orderList)
	}
	report := orderList.OrderReports[0]
	if report.Type != OrderTypeStopLossLimit || report.StopPrice != "1.00000000" || report.WorkingTime != -1 || report.IcebergQuantity != "1.00000000" || orderList.OrderReports[1].Type != OrderTypeLimitMaker {
		t.Fatalf("NewOCOOrderList() order reports = %+v", orderList.OrderReports)
	}
	harness.expect(
		t,
		http.MethodPost,
		"/api/v3/orderList/oco",
		true,
		1,
		2,
		"symbol=LTCBTC",
		"listClientOrderId=oco-list-1",
		"newOrderRespType=FULL",
		"side=SELL",
		"quantity=5",
		"aboveType=LIMIT_MAKER",
		"abovePrice=3.00000000",
		"belowType=STOP_LOSS_LIMIT",
		"belowClientOrderId=below-1",
		"belowPrice=1.00000000",
		"belowStopPrice=1.00000000",
		"belowTimeInForce=GTC",
		"belowIcebergQty=1.00000000",
	)
}

func TestNewOCOOrderListAllowsLimitMakerBelowForBuySide(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, ocoOrderListJSON)
	request := OCOOrderListRequest{Symbol: "LTCBTC", Side: SideBuy, Quantity: "5"}
	request.Above = OrderListLeg{Type: OrderTypeStopLossLimit, Price: "3.00000000", StopPrice: "3.10000000", TimeInForce: TimeInForceGTC}
	request.Below = OrderListLeg{Type: OrderTypeLimitMaker, Price: "1.00000000"}
	orderList, err := harness.client.NewOCOOrderList(context.Background(), request)
	if err != nil {
		t.Fatalf("NewOCOOrderList() error = %v", err)
	}
	if orderList.OrderListID != 2 || orderList.ContingencyType != ContingencyTypeOCO || len(orderList.Orders) != 2 {
		t.Fatalf("NewOCOOrderList() = %+v", orderList)
	}
	harness.expect(
		t,
		http.MethodPost,
		"/api/v3/orderList/oco",
		true,
		1,
		2,
		"symbol=LTCBTC",
		"side=BUY",
		"quantity=5",
		"aboveType=STOP_LOSS_LIMIT",
		"abovePrice=3.00000000",
		"aboveStopPrice=3.10000000",
		"aboveTimeInForce=GTC",
		"belowType=LIMIT_MAKER",
		"belowPrice=1.00000000",
	)
}

func TestNewOTOOrderList(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, otoOrderListJSON)
	orderList, err := harness.client.NewOTOOrderList(context.Background(), sampleOTORequest())
	if err != nil {
		t.Fatalf("NewOTOOrderList() error = %v", err)
	}
	requireOTOOrderList(t, orderList)
	harness.expect(
		t,
		http.MethodPost,
		"/api/v3/orderList/oto",
		true,
		1,
		2,
		"symbol=LTCBTC",
		"workingType=LIMIT",
		"workingSide=BUY",
		"workingPrice=1.00000000",
		"workingQuantity=1",
		"workingTimeInForce=GTC",
		"pendingType=LIMIT_MAKER",
		"pendingSide=SELL",
		"pendingPrice=3.00000000",
		"pendingQuantity=1",
	)
}

func TestNewOTOCOOrderList(t *testing.T) {
	t.Parallel()

	t.Run("pending above and below", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, otoOrderListJSON)
		orderList, err := harness.client.NewOTOCOOrderList(context.Background(), sampleOTOCORequest())
		if err != nil {
			t.Fatalf("NewOTOCOOrderList() error = %v", err)
		}
		requireOTOOrderList(t, orderList)
		harness.expect(
			t,
			http.MethodPost,
			"/api/v3/orderList/otoco",
			true,
			1,
			3,
			"symbol=LTCBTC",
			"workingType=LIMIT",
			"workingSide=BUY",
			"workingPrice=1.00000000",
			"workingQuantity=1",
			"workingTimeInForce=GTC",
			"pendingSide=SELL",
			"pendingQuantity=1",
			"pendingAboveType=LIMIT_MAKER",
			"pendingAbovePrice=3.00000000",
			"pendingBelowType=STOP_LOSS",
			"pendingBelowStopPrice=0.50000000",
		)
	})
	t.Run("pending below is optional", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, otoOrderListJSON)
		request := sampleOTOCORequest()
		request.PendingBelow = OrderListLeg{}
		if _, err := harness.client.NewOTOCOOrderList(context.Background(), request); err != nil {
			t.Fatalf("NewOTOCOOrderList() error = %v", err)
		}
		harness.expect(
			t,
			http.MethodPost,
			"/api/v3/orderList/otoco",
			true,
			1,
			3,
			"symbol=LTCBTC",
			"workingType=LIMIT",
			"workingSide=BUY",
			"workingPrice=1.00000000",
			"workingQuantity=1",
			"workingTimeInForce=GTC",
			"pendingSide=SELL",
			"pendingQuantity=1",
			"pendingAboveType=LIMIT_MAKER",
			"pendingAbovePrice=3.00000000",
		)
	})
}

func TestNewOPOOrderList(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, otoOrderListJSON)
	request := sampleOPORequest()
	request.Working.QuoteNotionalLimit = "100.00"
	orderList, err := harness.client.NewOPOOrderList(context.Background(), request)
	if err != nil {
		t.Fatalf("NewOPOOrderList() error = %v", err)
	}
	requireOTOOrderList(t, orderList)
	harness.expect(
		t,
		http.MethodPost,
		"/api/v3/orderList/opo",
		true,
		1,
		2,
		"symbol=BTCUSDT",
		"workingType=LIMIT",
		"workingSide=BUY",
		"workingPrice=60000.00",
		"workingQuantity=1",
		"workingTimeInForce=GTC",
		"workingQuoteNotionalLimit=100.00",
		"pendingType=LIMIT_MAKER",
		"pendingSide=SELL",
		"pendingPrice=70000.00",
	)
}

func TestNewOPOCOOrderList(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, otoOrderListJSON)
	orderList, err := harness.client.NewOPOCOOrderList(context.Background(), sampleOPOCORequest())
	if err != nil {
		t.Fatalf("NewOPOCOOrderList() error = %v", err)
	}
	requireOTOOrderList(t, orderList)
	harness.expect(
		t,
		http.MethodPost,
		"/api/v3/orderList/opoco",
		true,
		1,
		3,
		"symbol=BTCUSDT",
		"workingType=LIMIT",
		"workingSide=BUY",
		"workingPrice=60000.00",
		"workingQuantity=1",
		"workingTimeInForce=GTC",
		"pendingSide=SELL",
		"pendingAboveType=LIMIT_MAKER",
		"pendingAbovePrice=70000.00",
		"pendingBelowType=STOP_LOSS_LIMIT",
		"pendingBelowPrice=55000.00",
		"pendingBelowStopPrice=56000.00",
		"pendingBelowTimeInForce=GTC",
	)
}

func TestOrderListLegPegAndTrailingParameters(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, otoOrderListJSON)
	trailingDelta := int64(100)
	strategyID := int64(7)
	strategyType := int64(1000001)
	offset := 3
	request := sampleOTORequest()
	request.Working = OrderListLeg{Type: OrderTypeLimitMaker, PegPriceType: "PRIMARY_PEG", PegOffsetType: "PRICE_LEVEL", PegOffsetValue: &offset, StrategyID: &strategyID, StrategyType: &strategyType}
	request.Pending = OrderListLeg{Type: OrderTypeTakeProfit, TrailingDelta: &trailingDelta}
	if _, err := harness.client.NewOTOOrderList(context.Background(), request); err != nil {
		t.Fatalf("NewOTOOrderList() error = %v", err)
	}
	harness.expect(
		t,
		http.MethodPost,
		"/api/v3/orderList/oto",
		true,
		1,
		2,
		"symbol=LTCBTC",
		"workingType=LIMIT_MAKER",
		"workingSide=BUY",
		"workingQuantity=1",
		"workingPegPriceType=PRIMARY_PEG",
		"workingPegOffsetType=PRICE_LEVEL",
		"workingPegOffsetValue=3",
		"workingStrategyId=7",
		"workingStrategyType=1000001",
		"pendingType=TAKE_PROFIT",
		"pendingSide=SELL",
		"pendingQuantity=1",
		"pendingTrailingDelta=100",
	)
}

func TestCancelOrderList(t *testing.T) {
	t.Parallel()

	const body = `{"orderListId":0,"contingencyType":"OCO","listStatusType":"ALL_DONE","listOrderStatus":"ALL_DONE","listClientOrderId":"C3wyj4WVEktd7u9aVBRXcN","transactionTime":1574040868128,"symbol":"LTCBTC","orders":[{"symbol":"LTCBTC","orderId":2,"clientOrderId":"pO9ufTiFGg3nw2fOdgeOXa"},{"symbol":"LTCBTC","orderId":3,"clientOrderId":"TXOvglzXuaubXAaENpaRCB"}],"orderReports":[{"symbol":"LTCBTC","origClientOrderId":"pO9ufTiFGg3nw2fOdgeOXa","orderId":2,"orderListId":0,"clientOrderId":"unfWT8ig8i0uj6lPuYLez6","transactTime":1688005070874,"price":"1.00000000","origQty":"10.00000000","executedQty":"0.00000000","status":"CANCELED","timeInForce":"GTC","type":"STOP_LOSS_LIMIT","side":"SELL","stopPrice":"1.00000000","selfTradePreventionMode":"NONE"}]}`
	t.Run("by order list ID zero", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		orderListID := int64(0)
		orderList, err := harness.client.CancelOrderList(context.Background(), CancelOrderListRequest{Symbol: "LTCBTC", OrderListID: &orderListID, NewClientOrderID: "cancel-list-1"})
		if err != nil {
			t.Fatalf("CancelOrderList() error = %v", err)
		}
		if orderList.ListStatusType != ListStatusTypeAllDone || len(orderList.OrderReports) != 1 || orderList.OrderReports[0].Status != OrderStatusCanceled || len(orderList.Raw) == 0 {
			t.Fatalf("CancelOrderList() = %+v", orderList)
		}
		harness.expect(t, http.MethodDelete, "/api/v3/orderList", true, 1, 0, "symbol=LTCBTC", "orderListId=0", "newClientOrderId=cancel-list-1")
	})
	t.Run("by list client order ID", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		if _, err := harness.client.CancelOrderList(context.Background(), CancelOrderListRequest{Symbol: "LTCBTC", ListClientOrderID: "C3wyj4WVEktd7u9aVBRXcN"}); err != nil {
			t.Fatalf("CancelOrderList() error = %v", err)
		}
		harness.expect(t, http.MethodDelete, "/api/v3/orderList", true, 1, 0, "symbol=LTCBTC", "listClientOrderId=C3wyj4WVEktd7u9aVBRXcN")
	})
}

func TestQueryOrderList(t *testing.T) {
	t.Parallel()

	const body = `{"orderListId":27,"contingencyType":"OCO","listStatusType":"EXEC_STARTED","listOrderStatus":"EXECUTING","listClientOrderId":"h2USkA5YQpaXHPIrkd96xE","transactionTime":1565245656253,"symbol":"LTCBTC","orders":[{"symbol":"LTCBTC","orderId":4,"clientOrderId":"qD1gy3kc3Gx0rihm9Y3xwS"},{"symbol":"LTCBTC","orderId":5,"clientOrderId":"ARzZ9I00CPM8i3NhmU9Ega"}]}`
	t.Run("by order list ID", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		orderListID := int64(27)
		orderList, err := harness.client.QueryOrderList(context.Background(), QueryOrderListRequest{OrderListID: &orderListID})
		if err != nil {
			t.Fatalf("QueryOrderList() error = %v", err)
		}
		if orderList.OrderListID != 27 || orderList.ListClientOrderID != "h2USkA5YQpaXHPIrkd96xE" || len(orderList.Orders) != 2 || len(orderList.OrderReports) != 0 {
			t.Fatalf("QueryOrderList() = %+v", orderList)
		}
		harness.expect(t, http.MethodGet, "/api/v3/orderList", true, 4, 0, "orderListId=27")
	})
	t.Run("by list client order ID uses origClientOrderId", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		if _, err := harness.client.QueryOrderList(context.Background(), QueryOrderListRequest{ListClientOrderID: "h2USkA5YQpaXHPIrkd96xE"}); err != nil {
			t.Fatalf("QueryOrderList() error = %v", err)
		}
		harness.expect(t, http.MethodGet, "/api/v3/orderList", true, 4, 0, "origClientOrderId=h2USkA5YQpaXHPIrkd96xE")
	})
}

func TestAllOrderLists(t *testing.T) {
	t.Parallel()

	const body = `[{"orderListId":29,"contingencyType":"OCO","listStatusType":"EXEC_STARTED","listOrderStatus":"EXECUTING","listClientOrderId":"amEEAXryFzFwYF1FeRpUoZ","transactionTime":1565245913483,"symbol":"LTCBTC","orders":[{"symbol":"LTCBTC","orderId":4,"clientOrderId":"oD7aesZqjEGlZrbtRpy5zB"},{"symbol":"LTCBTC","orderId":5,"clientOrderId":"Jr1h6xirOxgeJOUuYQS7V3"}]},{"orderListId":28,"contingencyType":"OCO","listStatusType":"EXEC_STARTED","listOrderStatus":"EXECUTING","listClientOrderId":"hG7hFNxJV6cZy3Ze4AUT4d","transactionTime":1565245913407,"symbol":"LTCBTC","orders":[{"symbol":"LTCBTC","orderId":2,"clientOrderId":"j6lFOfbmFMRjTYA7rRJ0LP"},{"symbol":"LTCBTC","orderId":3,"clientOrderId":"z0KCjOdditiLS5ekAFtK81"}]}]`
	t.Run("from ID", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		fromID := int64(28)
		orderLists, err := harness.client.AllOrderLists(context.Background(), AllOrderListsRequest{FromID: &fromID, Limit: 2})
		if err != nil {
			t.Fatalf("AllOrderLists() error = %v", err)
		}
		if len(orderLists) != 2 || orderLists[0].OrderListID != 29 || orderLists[1].ListClientOrderID != "hG7hFNxJV6cZy3Ze4AUT4d" || len(orderLists[1].Raw) == 0 {
			t.Fatalf("AllOrderLists() = %+v", orderLists)
		}
		harness.expect(t, http.MethodGet, "/api/v3/allOrderList", true, 20, 0, "fromId=28", "limit=2")
	})
	t.Run("time window", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, `[]`)
		start := time.UnixMilli(1565245913000)
		end := start.Add(24 * time.Hour)
		orderLists, err := harness.client.AllOrderLists(context.Background(), AllOrderListsRequest{StartTime: &start, EndTime: &end})
		if err != nil {
			t.Fatalf("AllOrderLists() error = %v", err)
		}
		if len(orderLists) != 0 {
			t.Fatalf("AllOrderLists() = %+v, want empty", orderLists)
		}
		harness.expect(t, http.MethodGet, "/api/v3/allOrderList", true, 20, 0, "startTime=1565245913000", "endTime=1565332313000")
	})
}

func TestOpenOrderLists(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `[{"orderListId":31,"contingencyType":"OCO","listStatusType":"EXEC_STARTED","listOrderStatus":"EXECUTING","listClientOrderId":"wuB13fmulKj3YjdqWEcsnp","transactionTime":1565246080644,"symbol":"LTCBTC","orders":[{"symbol":"LTCBTC","orderId":4,"clientOrderId":"r3EH2N76dHfLoSZWIUw1bT"},{"symbol":"LTCBTC","orderId":5,"clientOrderId":"Cv1SnyPD3qhqpbjpYEHbd2"}]}]`)
	orderLists, err := harness.client.OpenOrderLists(context.Background())
	if err != nil {
		t.Fatalf("OpenOrderLists() error = %v", err)
	}
	if len(orderLists) != 1 || orderLists[0].OrderListID != 31 || orderLists[0].Orders[1].ClientOrderID != "Cv1SnyPD3qhqpbjpYEHbd2" {
		t.Fatalf("OpenOrderLists() = %+v", orderLists)
	}
	harness.expect(t, http.MethodGet, "/api/v3/openOrderList", true, 6, 0)
}

func TestOrderListValidation(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `{}`)
	ctx := context.Background()

	oco := sampleOCORequest()
	oco.Above.Type = OrderTypeMarket
	_, err := harness.client.NewOCOOrderList(ctx, oco)
	harness.requireValidation(t, "OCO above MARKET", err)

	oco = sampleOCORequest()
	oco.Above.Price = ""
	_, err = harness.client.NewOCOOrderList(ctx, oco)
	harness.requireValidation(t, "OCO above LIMIT_MAKER without price", err)

	oco = sampleOCORequest()
	oco.Below.TimeInForce = ""
	oco.Below.IcebergQuantity = ""
	_, err = harness.client.NewOCOOrderList(ctx, oco)
	harness.requireValidation(t, "OCO below STOP_LOSS_LIMIT without timeInForce", err)

	oco = sampleOCORequest()
	oco.Below = OrderListLeg{Type: OrderTypeStopLoss}
	_, err = harness.client.NewOCOOrderList(ctx, oco)
	harness.requireValidation(t, "OCO below STOP_LOSS without trigger", err)

	oco = sampleOCORequest()
	oco.Quantity = "0"
	_, err = harness.client.NewOCOOrderList(ctx, oco)
	harness.requireValidation(t, "OCO zero quantity", err)

	oco = sampleOCORequest()
	oco.Below.IcebergQuantity = "1"
	oco.Below.TimeInForce = TimeInForceIOC
	_, err = harness.client.NewOCOOrderList(ctx, oco)
	harness.requireValidation(t, "OCO iceberg without GTC", err)

	oco = sampleOCORequest()
	strategyType := int64(999999)
	oco.Above.StrategyType = &strategyType
	_, err = harness.client.NewOCOOrderList(ctx, oco)
	harness.requireValidation(t, "OCO reserved strategyType", err)

	oco = sampleOCORequest()
	oco.Above.PegPriceType = "OTHER_PEG"
	_, err = harness.client.NewOCOOrderList(ctx, oco)
	harness.requireValidation(t, "OCO unsupported pegPriceType", err)

	oco = sampleOCORequest()
	oco.ResponseType = "VERBOSE"
	_, err = harness.client.NewOCOOrderList(ctx, oco)
	harness.requireValidation(t, "OCO response type", err)

	oto := sampleOTORequest()
	oto.Working.Type = OrderTypeStopLoss
	_, err = harness.client.NewOTOOrderList(ctx, oto)
	harness.requireValidation(t, "OTO working STOP_LOSS", err)

	oto = sampleOTORequest()
	oto.Working.TimeInForce = ""
	_, err = harness.client.NewOTOOrderList(ctx, oto)
	harness.requireValidation(t, "OTO working LIMIT without timeInForce", err)

	oto = sampleOTORequest()
	oto.PendingQuantity = ""
	_, err = harness.client.NewOTOOrderList(ctx, oto)
	harness.requireValidation(t, "OTO without pendingQuantity", err)

	oto = sampleOTORequest()
	oto.Pending = OrderListLeg{Type: OrderTypeMarket, Price: "1"}
	_, err = harness.client.NewOTOOrderList(ctx, oto)
	harness.requireValidation(t, "OTO pending MARKET with price", err)

	oto = sampleOTORequest()
	oto.WorkingSide = ""
	_, err = harness.client.NewOTOOrderList(ctx, oto)
	harness.requireValidation(t, "OTO without workingSide", err)

	otoco := sampleOTOCORequest()
	otoco.PendingBelow = OrderListLeg{Price: "0.5"}
	_, err = harness.client.NewOTOCOOrderList(ctx, otoco)
	harness.requireValidation(t, "OTOCO pending below without type", err)

	otoco = sampleOTOCORequest()
	otoco.PendingAbove = OrderListLeg{}
	_, err = harness.client.NewOTOCOOrderList(ctx, otoco)
	harness.requireValidation(t, "OTOCO without pending above", err)

	opo := sampleOPORequest()
	opo.Pending.QuoteNotionalLimit = "100"
	_, err = harness.client.NewOPOOrderList(ctx, opo)
	harness.requireValidation(t, "OPO pending quoteNotionalLimit", err)

	opoco := sampleOPOCORequest()
	opoco.PendingAbove.QuoteNotionalLimit = "100"
	_, err = harness.client.NewOPOCOOrderList(ctx, opoco)
	harness.requireValidation(t, "OPOCO pending quoteNotionalLimit", err)

	opoco = sampleOPOCORequest()
	opoco.PendingSide = ""
	_, err = harness.client.NewOPOCOOrderList(ctx, opoco)
	harness.requireValidation(t, "OPOCO without pendingSide", err)

	_, err = harness.client.CancelOrderList(ctx, CancelOrderListRequest{Symbol: "LTCBTC"})
	harness.requireValidation(t, "CancelOrderList without identity", err)
	negative := int64(-1)
	_, err = harness.client.CancelOrderList(ctx, CancelOrderListRequest{Symbol: "LTCBTC", OrderListID: &negative})
	harness.requireValidation(t, "CancelOrderList negative ID", err)
	_, err = harness.client.QueryOrderList(ctx, QueryOrderListRequest{})
	harness.requireValidation(t, "QueryOrderList without identity", err)
	_, err = harness.client.QueryOrderList(ctx, QueryOrderListRequest{ListClientOrderID: "has a space"})
	harness.requireValidation(t, "QueryOrderList invalid client ID", err)

	fromID := int64(1)
	start := time.UnixMilli(1565245913000)
	end := start.Add(25 * time.Hour)
	_, err = harness.client.AllOrderLists(ctx, AllOrderListsRequest{FromID: &fromID, StartTime: &start})
	harness.requireValidation(t, "AllOrderLists fromId with startTime", err)
	_, err = harness.client.AllOrderLists(ctx, AllOrderListsRequest{StartTime: &start, EndTime: &end})
	harness.requireValidation(t, "AllOrderLists window over 24 hours", err)
	_, err = harness.client.AllOrderLists(ctx, AllOrderListsRequest{Limit: 1001})
	harness.requireValidation(t, "AllOrderLists limit", err)
}
