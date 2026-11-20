package bybit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

// extCall은 테스트 서버가 받은 서명 검증 통과 요청 한 건이다.
type extCall struct {
	Method string
	Path   string
	Query  url.Values
	Body   string
}

// extRecorder는 서명 검증을 통과한 요청을 순서대로 기록한다.
type extRecorder struct {
	mu    sync.Mutex
	calls []extCall
}

func (recorder *extRecorder) record(call extCall) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.calls = append(recorder.calls, call)
}

func (recorder *extRecorder) snapshot() []extCall {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return slices.Clone(recorder.calls)
}

var (
	extReadPermissions      = []credential.Permission{credential.PermissionRead}
	extReadTradePermissions = []credential.Permission{credential.PermissionRead, credential.PermissionTrade}
)

// extEnvelope는 retCode 0 응답 본문에 result를 넣는다.
func extEnvelope(result string) string {
	return `{"retCode":0,"retMsg":"OK","result":` + result + `,"retExtInfo":{},"time":1700000000000}`
}

// extResult는 항상 같은 result를 HTTP 200으로 돌려주는 응답기다.
func extResult(result string) func(extCall) (int, string) {
	return func(extCall) (int, string) { return http.StatusOK, extEnvelope(result) }
}

// extBody는 항상 같은 전체 응답 본문을 HTTP 200으로 돌려주는 응답기다.
func extBody(body string) func(extCall) (int, string) {
	return func(extCall) (int, string) { return http.StatusOK, body }
}

// extServerError는 항상 HTTP 500을 돌려주는 응답기다.
func extServerError(extCall) (int, string) {
	return http.StatusInternalServerError, `{"retCode":10016,"retMsg":"Server error","result":{},"time":1700000000000}`
}

// newExtTestClient는 서명을 검증하고 요청을 기록하는 테스트 서버에 연결된 클라이언트를 만든다.
func newExtTestClient(
	t *testing.T,
	permissions []credential.Permission,
	respond func(extCall) (int, string),
) (*Client, *extRecorder, *ratelimit.Limiter) {
	t.Helper()
	fixedNow := time.UnixMilli(1_700_000_000_000)
	recorder := &extRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"retCode":10004,"retMsg":"error sign","result":{},"time":1700000000000}`, http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(request.Body)
		call := extCall{
			Method: request.Method, Path: request.URL.Path, Query: request.URL.Query(), Body: string(body),
		}
		recorder.record(call)
		status, response := respond(call)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = io.WriteString(writer, response)
	}))
	t.Cleanup(server.Close)

	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: &directSender{}, Limiter: limiter})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	client, err := New(Config{
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "bybit-main", Exchange: model.ExchangeBybit, SecretRef: "secret/bybit-main",
			Permissions:           permissions,
			AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
		},
		CredentialProvider: &recordingProvider{}, DefaultEgressRouteID: "route-a",
		BaseURL: server.URL, AllowInsecureHTTP: true, Now: func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, recorder, limiter
}

// extSingleCall은 요청이 정확히 한 건이고 메서드와 경로가 맞는지 확인한 뒤 그 요청을 반환한다.
func extSingleCall(t *testing.T, recorder *extRecorder, method, path string) extCall {
	t.Helper()
	calls := recorder.snapshot()
	if len(calls) != 1 || calls[0].Method != method || calls[0].Path != path {
		t.Fatalf("calls = %+v, want exactly one %s %s", calls, method, path)
	}
	return calls[0]
}

// assertJSONEqual은 JSON 값을 필드 순서와 무관하게 비교한다.
func assertJSONEqual(t *testing.T, got, want string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
		t.Fatalf("decode request body %q: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("decode expected body %q: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("request body = %s, want %s", got, want)
	}
}

func TestPlaceOrderExtendedParameters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		request  PlaceOrderRequest
		wantBody string
	}{
		{
			"Linear partial limit TP/SL with trigger prices",
			PlaceOrderRequest{
				Category: CategoryLinear, Symbol: "BTCUSDT", Side: SideBuy, OrderType: OrderTypeLimit,
				Quantity: "1", Price: "25000", TimeInForce: TimeInForceGTC, OrderLinkID: "usdt-test-01",
				TakeProfit: "28000", StopLoss: "20000", TPSLMode: TPSLModePartial,
				TPOrderType: OrderTypeLimit, SLOrderType: OrderTypeLimit,
				TPLimitPrice: "27500", SLLimitPrice: "20500",
				TPTriggerBy: TriggerByMarkPrice, SLTriggerBy: TriggerByIndexPrice, SMPType: SMPTypeCancelMaker,
			},
			`{"category":"linear","symbol":"BTCUSDT","side":"Buy","orderType":"Limit","qty":"1","price":"25000",` +
				`"timeInForce":"GTC","orderLinkId":"usdt-test-01","takeProfit":"28000","stopLoss":"20000",` +
				`"tpTriggerBy":"MarkPrice","slTriggerBy":"IndexPrice","tpslMode":"Partial",` +
				`"tpLimitPrice":"27500","slLimitPrice":"20500","tpOrderType":"Limit","slOrderType":"Limit",` +
				`"smpType":"CancelMaker"}`,
		},
		{
			"Linear conditional order",
			PlaceOrderRequest{
				Category: CategoryLinear, Symbol: "BTCUSDT", Side: SideSell, OrderType: OrderTypeMarket,
				Quantity: "0.5", TriggerPrice: "1145", TriggerDirection: 2, TriggerBy: TriggerByMarkPrice,
			},
			`{"category":"linear","symbol":"BTCUSDT","side":"Sell","orderType":"Market","qty":"0.5",` +
				`"triggerDirection":2,"triggerPrice":"1145","triggerBy":"MarkPrice"}`,
		},
		{
			"Linear market TP/SL in Full mode",
			PlaceOrderRequest{
				Category: CategoryLinear, Symbol: "BTCUSDT", Side: SideBuy, OrderType: OrderTypeMarket,
				Quantity: "0.1", TakeProfit: "70000", StopLoss: "60000", TPSLMode: TPSLModeFull,
			},
			`{"category":"linear","symbol":"BTCUSDT","side":"Buy","orderType":"Market","qty":"0.1",` +
				`"takeProfit":"70000","stopLoss":"60000","tpslMode":"Full"}`,
		},
		{
			"Linear close entire position",
			PlaceOrderRequest{
				Category: CategoryLinear, Symbol: "BTCUSDT", Side: SideSell, OrderType: OrderTypeMarket,
				Quantity: "0", ReduceOnly: true, CloseOnTrigger: true,
			},
			`{"category":"linear","symbol":"BTCUSDT","side":"Sell","orderType":"Market","qty":"0",` +
				`"reduceOnly":true,"closeOnTrigger":true}`,
		},
		{
			"Spot limit order with market TP/SL",
			PlaceOrderRequest{
				Category: CategorySpot, Symbol: "BTCUSDT", Side: SideBuy, OrderType: OrderTypeLimit,
				Quantity: "0.01", Price: "28000", TimeInForce: TimeInForcePostOnly,
				TakeProfit: "35000", StopLoss: "27000", TPOrderType: OrderTypeMarket, SLOrderType: OrderTypeMarket,
			},
			`{"category":"spot","symbol":"BTCUSDT","side":"Buy","orderType":"Limit","qty":"0.01","price":"28000",` +
				`"timeInForce":"PostOnly","takeProfit":"35000","stopLoss":"27000",` +
				`"tpOrderType":"Market","slOrderType":"Market"}`,
		},
		{
			"Spot limit order with limit TP/SL",
			PlaceOrderRequest{
				Category: CategorySpot, Symbol: "BTCUSDT", Side: SideBuy, OrderType: OrderTypeLimit,
				Quantity: "0.01", Price: "28000", TimeInForce: TimeInForcePostOnly,
				TakeProfit: "35000", StopLoss: "27000", TPLimitPrice: "36000", SLLimitPrice: "27500",
				TPOrderType: OrderTypeLimit, SLOrderType: OrderTypeLimit,
			},
			`{"category":"spot","symbol":"BTCUSDT","side":"Buy","orderType":"Limit","qty":"0.01","price":"28000",` +
				`"timeInForce":"PostOnly","takeProfit":"35000","stopLoss":"27000",` +
				`"tpLimitPrice":"36000","slLimitPrice":"27500","tpOrderType":"Limit","slOrderType":"Limit"}`,
		},
		{
			"Spot TP/SL order",
			PlaceOrderRequest{
				Category: CategorySpot, Symbol: "BTCUSDT", Side: SideBuy, OrderType: OrderTypeLimit,
				Quantity: "0.1", Price: "15600", TriggerPrice: "15000", OrderLinkID: "spot-test-02",
				OrderFilter: OrderFilterTPSLOrder,
			},
			`{"category":"spot","symbol":"BTCUSDT","side":"Buy","orderType":"Limit","qty":"0.1","price":"15600",` +
				`"orderLinkId":"spot-test-02","triggerPrice":"15000","orderFilter":"tpslOrder"}`,
		},
		{
			"Spot margin order",
			PlaceOrderRequest{
				Category: CategorySpot, Symbol: "BTCUSDT", Side: SideBuy, OrderType: OrderTypeLimit,
				Quantity: "0.1", Price: "15600", IsLeverage: 1, OrderFilter: OrderFilterOrder,
			},
			`{"category":"spot","symbol":"BTCUSDT","side":"Buy","orderType":"Limit","qty":"0.1","price":"15600",` +
				`"isLeverage":1,"orderFilter":"Order"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(
				t, extReadTradePermissions, extResult(`{"orderId":"1321003749386327552","orderLinkId":"x"}`),
			)
			reference, err := client.PlaceOrder(context.Background(), test.request)
			if err != nil || reference.OrderID != "1321003749386327552" || len(reference.Raw) == 0 {
				t.Fatalf("PlaceOrder() = %+v, error = %v", reference, err)
			}
			call := extSingleCall(t, recorder, http.MethodPost, "/v5/order/create")
			assertJSONEqual(t, call.Body, test.wantBody)
		})
	}
}

func TestPlaceOrderExtensionValidation(t *testing.T) {
	t.Parallel()

	base := PlaceOrderRequest{
		Category: CategoryLinear, Symbol: "BTCUSDT", Side: SideBuy, OrderType: OrderTypeLimit,
		Quantity: "1", Price: "25000",
	}
	if err := base.validate(); err != nil {
		t.Fatalf("base request validation error = %v", err)
	}
	closeAll := PlaceOrderRequest{
		Category: CategoryLinear, Symbol: "BTCUSDT", Side: SideSell, OrderType: OrderTypeMarket,
		Quantity: "0", ReduceOnly: true, CloseOnTrigger: true,
	}
	if err := closeAll.validate(); err != nil {
		t.Fatalf("close-all request validation error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*PlaceOrderRequest)
	}{
		{"Spot tpslMode", func(r *PlaceOrderRequest) { r.Category = CategorySpot; r.TPSLMode = TPSLModePartial }},
		{"Spot triggerBy", func(r *PlaceOrderRequest) { r.Category = CategorySpot; r.TriggerBy = TriggerByMarkPrice }},
		{"Spot isLeverage out of range", func(r *PlaceOrderRequest) { r.Category = CategorySpot; r.IsLeverage = 2 }},
		{"Spot create does not accept OCO filter", func(r *PlaceOrderRequest) {
			r.Category = CategorySpot
			r.OrderFilter = OrderFilterOcoOrder
		}},
		{"Linear isLeverage", func(r *PlaceOrderRequest) { r.IsLeverage = 1 }},
		{"Linear orderFilter", func(r *PlaceOrderRequest) { r.OrderFilter = OrderFilterTPSLOrder }},
		{"trigger direction without trigger price", func(r *PlaceOrderRequest) { r.TriggerDirection = 1 }},
		{"trigger direction out of range", func(r *PlaceOrderRequest) { r.TriggerPrice = "1"; r.TriggerDirection = 3 }},
		{"unknown triggerBy", func(r *PlaceOrderRequest) { r.TriggerPrice = "1"; r.TriggerBy = "Last" }},
		{"zero takeProfit", func(r *PlaceOrderRequest) { r.TakeProfit = "0" }},
		{"reduceOnly with takeProfit", func(r *PlaceOrderRequest) { r.ReduceOnly = true; r.TakeProfit = "30000" }},
		{"limit takeProfit without Partial", func(r *PlaceOrderRequest) {
			r.TakeProfit = "30000"
			r.TPOrderType = OrderTypeLimit
			r.TPLimitPrice = "29900"
		}},
		{"limit price without limit order type", func(r *PlaceOrderRequest) {
			r.TPSLMode = TPSLModePartial
			r.TPLimitPrice = "29900"
		}},
		{"Spot limit stopLoss without limit price", func(r *PlaceOrderRequest) {
			r.Category = CategorySpot
			r.StopLoss = "20000"
			r.SLOrderType = OrderTypeLimit
		}},
		{"unknown smpType", func(r *PlaceOrderRequest) { r.SMPType = "Bad" }},
		{"zero qty without close-all", func(r *PlaceOrderRequest) { r.Quantity = "0" }},
		{"zero qty reduceOnly without closeOnTrigger", func(r *PlaceOrderRequest) {
			r.Quantity = "0"
			r.ReduceOnly = true
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := base
			test.mutate(&request)
			if err := request.validate(); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("validate() error = %v, want validation error", err)
			}
		})
	}
}

func TestAmendOrder(t *testing.T) {
	t.Parallel()

	client, recorder, limiter := newExtTestClient(
		t, extReadTradePermissions,
		extResult(`{"orderId":"c6f055d9-7f21-4079-913d-e6523a9cfffa","orderLinkId":"linear-004"}`),
	)
	reference, err := client.AmendOrder(context.Background(), AmendOrderRequest{
		Category: CategoryLinear, Symbol: "ETHPERP", OrderLinkID: "linear-004",
		TriggerPrice: "1145", Quantity: "0.15", Price: "1050", TakeProfit: "0", StopLoss: "0",
	})
	if err != nil || reference.OrderID != "c6f055d9-7f21-4079-913d-e6523a9cfffa" ||
		reference.OrderLinkID != "linear-004" || len(reference.Raw) == 0 {
		t.Fatalf("AmendOrder() = %+v, error = %v", reference, err)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/order/amend")
	assertJSONEqual(t, call.Body, `{"category":"linear","symbol":"ETHPERP","orderLinkId":"linear-004",`+
		`"triggerPrice":"1145","qty":"0.15","price":"1050","takeProfit":"0","stopLoss":"0"}`)
	snapshot, err := limiter.Snapshot(rateLimitEndpointKey("account", "bybit-main", "/v5/order/amend"))
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("amend limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestAmendOrderValidation(t *testing.T) {
	t.Parallel()

	valid := AmendOrderRequest{Category: CategorySpot, Symbol: "BTCUSDT", OrderID: "42", Price: "64000"}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid amend request error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*AmendOrderRequest)
	}{
		{"missing identity", func(r *AmendOrderRequest) { r.OrderID = "" }},
		{"missing symbol", func(r *AmendOrderRequest) { r.Symbol = "" }},
		{"nothing to modify", func(r *AmendOrderRequest) { r.Price = "" }},
		{"zero price", func(r *AmendOrderRequest) { r.Price = "0" }},
		{"negative takeProfit", func(r *AmendOrderRequest) { r.TakeProfit = "-1" }},
		{"unknown triggerBy", func(r *AmendOrderRequest) { r.TriggerBy = "Last" }},
		{"unknown tpslMode", func(r *AmendOrderRequest) { r.Category = CategoryLinear; r.TPSLMode = "Half" }},
		{"Spot tpslMode", func(r *AmendOrderRequest) { r.TPSLMode = TPSLModePartial }},
		{"unsupported category", func(r *AmendOrderRequest) { r.Category = "option" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			if err := request.validate(); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("validate() error = %v, want validation error", err)
			}
		})
	}
}

func TestCancelAllOrders(t *testing.T) {
	t.Parallel()

	listResult := `{"list":[{"orderId":"1616024329462743808","orderLinkId":"1616024329462743809"},` +
		`{"orderId":"1616024287544869632","orderLinkId":"1616024287544869633"}],"success":"1"}`
	t.Run("Linear settle coin", func(t *testing.T) {
		t.Parallel()
		client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(listResult))
		result, err := client.CancelAllOrders(context.Background(), CancelAllOrdersRequest{
			Category: CategoryLinear, SettleCoin: "USDT", OrderFilter: OrderFilterStopOrder,
			StopOrderType: StopOrderTypeStop,
		})
		if err != nil || len(result.Orders) != 2 || result.Success != "1" || len(result.Raw) == 0 ||
			result.Orders[1].OrderID != "1616024287544869632" || len(result.Orders[1].Raw) == 0 {
			t.Fatalf("CancelAllOrders() = %+v, error = %v", result, err)
		}
		call := extSingleCall(t, recorder, http.MethodPost, "/v5/order/cancel-all")
		assertJSONEqual(t, call.Body,
			`{"category":"linear","settleCoin":"USDT","orderFilter":"StopOrder","stopOrderType":"Stop"}`)
	})
	t.Run("Spot all orders with numeric success", func(t *testing.T) {
		t.Parallel()
		client, recorder, _ := newExtTestClient(
			t, extReadTradePermissions, extResult(`{"list":[{"orderId":"7","orderLinkId":""}],"success":1}`),
		)
		result, err := client.CancelAllOrders(context.Background(), CancelAllOrdersRequest{Category: CategorySpot})
		if err != nil || len(result.Orders) != 1 || result.Success != "1" {
			t.Fatalf("CancelAllOrders() = %+v, error = %v", result, err)
		}
		call := extSingleCall(t, recorder, http.MethodPost, "/v5/order/cancel-all")
		assertJSONEqual(t, call.Body, `{"category":"spot"}`)
	})
}

func TestCancelAllOrdersValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request CancelAllOrdersRequest
	}{
		{"Linear without filter", CancelAllOrdersRequest{Category: CategoryLinear}},
		{"Spot baseCoin", CancelAllOrdersRequest{Category: CategorySpot, BaseCoin: "BTC"}},
		{"Spot settleCoin", CancelAllOrdersRequest{Category: CategorySpot, SettleCoin: "USDT"}},
		{"Linear tpslOrder filter", CancelAllOrdersRequest{
			Category: CategoryLinear, Symbol: "BTCUSDT", OrderFilter: OrderFilterTPSLOrder,
		}},
		{"unknown filter", CancelAllOrdersRequest{Category: CategorySpot, OrderFilter: "Other"}},
		{"stopOrderType without StopOrder", CancelAllOrdersRequest{
			Category: CategoryLinear, Symbol: "BTCUSDT", StopOrderType: StopOrderTypeStop,
		}},
		{"stopOrderType on Spot", CancelAllOrdersRequest{
			Category: CategorySpot, OrderFilter: OrderFilterStopOrder, StopOrderType: StopOrderTypeStop,
		}},
		{"surrounding whitespace", CancelAllOrdersRequest{Category: CategoryLinear, Symbol: " BTCUSDT"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.request.validate(); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("validate() error = %v, want validation error", err)
			}
		})
	}
	for _, request := range []CancelAllOrdersRequest{
		{Category: CategorySpot, OrderFilter: OrderFilterBidirectionalTPSLOrder},
		{Category: CategoryLinear, BaseCoin: "BTC", OrderFilter: OrderFilterOrder},
	} {
		if err := request.validate(); err != nil {
			t.Fatalf("validate(%+v) error = %v", request, err)
		}
	}
}

func TestPlaceBatchOrders(t *testing.T) {
	t.Parallel()

	body := `{"retCode":0,"retMsg":"OK","result":{"list":[` +
		`{"category":"linear","symbol":"BTCUSDT","orderId":"b551f227-7059-4fb5-a6a6-699c04dbd2f2","orderLinkId":"batch-001","createAt":"1672222047683"},` +
		`{"category":"linear","symbol":"ETHUSDT","orderId":"","orderLinkId":"batch-002","createAt":""}]},` +
		`"retExtInfo":{"list":[{"code":0,"msg":"OK"},{"code":10001,"msg":"The number of contracts exceeds minimum limit allowed"}]},` +
		`"time":1672222047684}`
	client, recorder, limiter := newExtTestClient(t, extReadTradePermissions, extBody(body))
	orders := []PlaceOrderRequest{
		{
			Symbol: "BTCUSDT", Side: SideBuy, OrderType: OrderTypeLimit,
			Quantity: "0.01", Price: "25000", OrderLinkID: "batch-001",
		},
		{
			Category: CategoryLinear, Symbol: "ETHUSDT", Side: SideSell, OrderType: OrderTypeMarket,
			Quantity: "0.001", OrderLinkID: "batch-002", TakeProfit: "1000", TPSLMode: TPSLModeFull,
		},
	}
	result, err := client.PlaceBatchOrders(
		context.Background(), BatchPlaceOrdersRequest{Category: CategoryLinear, Orders: orders},
	)
	if err != nil || len(result.Items) != 2 || len(result.Raw) == 0 {
		t.Fatalf("PlaceBatchOrders() = %+v, error = %v", result, err)
	}
	first, second := result.Items[0], result.Items[1]
	if !first.Succeeded() || first.OrderID != "b551f227-7059-4fb5-a6a6-699c04dbd2f2" ||
		first.CreatedTime != "1672222047683" || first.Message != "OK" || len(first.Raw) == 0 {
		t.Fatalf("first item = %+v", first)
	}
	if second.Succeeded() || second.Code != 10001 || second.OrderLinkID != "batch-002" ||
		second.Message != "The number of contracts exceeds minimum limit allowed" {
		t.Fatalf("second item = %+v", second)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/order/create-batch")
	assertJSONEqual(t, call.Body, `{"category":"linear","request":[`+
		`{"symbol":"BTCUSDT","side":"Buy","orderType":"Limit","qty":"0.01","price":"25000","orderLinkId":"batch-001"},`+
		`{"symbol":"ETHUSDT","side":"Sell","orderType":"Market","qty":"0.001","orderLinkId":"batch-002",`+
		`"takeProfit":"1000","tpslMode":"Full"}]}`)
	if orders[0].Category != "" || orders[1].Category != CategoryLinear {
		t.Fatalf("caller orders were modified: %q, %q", orders[0].Category, orders[1].Category)
	}
	snapshot, err := limiter.Snapshot(rateLimitEndpointKey("account", "bybit-main", "/v5/order/create-batch"))
	if err != nil || snapshot.Used != 2 {
		t.Fatalf("create-batch limiter snapshot = %+v, error = %v; want 2 units for 2 orders", snapshot, err)
	}
}

func TestAmendBatchOrders(t *testing.T) {
	t.Parallel()

	body := `{"retCode":0,"retMsg":"OK","result":{"list":[` +
		`{"category":"spot","symbol":"BTCUSDT","orderId":"1","orderLinkId":""},` +
		`{"category":"spot","symbol":"ETHUSDT","orderId":"2","orderLinkId":"amend-2"}]},` +
		`"retExtInfo":{"list":[{"code":0,"msg":"OK"},{"code":0,"msg":"OK"}]},"time":1672222935987}`
	client, recorder, limiter := newExtTestClient(t, extReadTradePermissions, extBody(body))
	result, err := client.AmendBatchOrders(context.Background(), BatchAmendOrdersRequest{
		Category: CategorySpot,
		Orders: []AmendOrderRequest{
			{Symbol: "BTCUSDT", OrderID: "1", Price: "64000"},
			{Category: CategorySpot, Symbol: "ETHUSDT", OrderLinkID: "amend-2", Quantity: "0.5", TakeProfit: "0"},
		},
	})
	if err != nil || len(result.Items) != 2 || !result.Items[0].Succeeded() || !result.Items[1].Succeeded() ||
		result.Items[1].OrderLinkID != "amend-2" {
		t.Fatalf("AmendBatchOrders() = %+v, error = %v", result, err)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/order/amend-batch")
	assertJSONEqual(t, call.Body, `{"category":"spot","request":[`+
		`{"symbol":"BTCUSDT","orderId":"1","price":"64000"},`+
		`{"symbol":"ETHUSDT","orderLinkId":"amend-2","qty":"0.5","takeProfit":"0"}]}`)
	snapshot, err := limiter.Snapshot(rateLimitEndpointKey("account", "bybit-main", "/v5/order/amend-batch"))
	if err != nil || snapshot.Used != 2 {
		t.Fatalf("amend-batch limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestCancelBatchOrders(t *testing.T) {
	t.Parallel()

	body := `{"retCode":0,"retMsg":"OK","result":{"list":[` +
		`{"category":"linear","symbol":"BTCUSDT","orderId":"1","orderLinkId":""},` +
		`{"category":"linear","symbol":"ETHUSDT","orderId":"","orderLinkId":"cancel-2"}]},` +
		`"retExtInfo":{"list":[{"code":0,"msg":"OK"},{"code":110001,"msg":"Order does not exist"}]},"time":1672223356634}`
	client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extBody(body))
	result, err := client.CancelBatchOrders(context.Background(), BatchCancelOrdersRequest{
		Category: CategoryLinear,
		Orders: []CancelOrderRequest{
			{Symbol: "BTCUSDT", OrderID: "1"},
			{Category: CategoryLinear, Symbol: "ETHUSDT", OrderLinkID: "cancel-2"},
		},
	})
	if err != nil || len(result.Items) != 2 || !result.Items[0].Succeeded() ||
		result.Items[1].Succeeded() || result.Items[1].Code != 110001 {
		t.Fatalf("CancelBatchOrders() = %+v, error = %v", result, err)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/order/cancel-batch")
	assertJSONEqual(t, call.Body, `{"category":"linear","request":[`+
		`{"symbol":"BTCUSDT","orderId":"1"},{"symbol":"ETHUSDT","orderLinkId":"cancel-2"}]}`)
}

func TestBatchOrdersRejectMismatchedExtInfo(t *testing.T) {
	t.Parallel()

	body := `{"retCode":0,"retMsg":"OK","result":{"list":[{"orderId":"1"},{"orderId":"2"}]},` +
		`"retExtInfo":{"list":[{"code":0,"msg":"OK"}]},"time":1672223356634}`
	client, _, _ := newExtTestClient(t, extReadTradePermissions, extBody(body))
	_, err := client.CancelBatchOrders(context.Background(), BatchCancelOrdersRequest{
		Category: CategorySpot,
		Orders: []CancelOrderRequest{
			{Symbol: "BTCUSDT", OrderID: "1"}, {Symbol: "BTCUSDT", OrderID: "2"},
		},
	})
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CancelBatchOrders() error = %v, want unknown execution state", err)
	}
}

func TestBatchOrdersValidation(t *testing.T) {
	t.Parallel()

	order := PlaceOrderRequest{
		Symbol: "BTCUSDT", Side: SideBuy, OrderType: OrderTypeLimit, Quantity: "0.01", Price: "25000",
	}
	eleven := slices.Repeat([]PlaceOrderRequest{order}, 11)
	ten := slices.Repeat([]PlaceOrderRequest{order}, 10)
	if err := (BatchPlaceOrdersRequest{Category: CategoryLinear, Orders: ten}).validate(); err != nil {
		t.Fatalf("ten-order batch validation error = %v", err)
	}
	mismatched := slices.Clone(ten)
	mismatched[3].Category = CategorySpot
	invalidItem := slices.Clone(ten)
	invalidItem[7].Symbol = ""

	tests := []struct {
		name string
		err  error
	}{
		{"empty place batch", (BatchPlaceOrdersRequest{Category: CategoryLinear}).validate()},
		{"eleven place orders", (BatchPlaceOrdersRequest{Category: CategoryLinear, Orders: eleven}).validate()},
		{"unsupported batch category", (BatchPlaceOrdersRequest{Category: "option", Orders: ten}).validate()},
		{"item category mismatch", (BatchPlaceOrdersRequest{Category: CategoryLinear, Orders: mismatched}).validate()},
		{"invalid item", (BatchPlaceOrdersRequest{Category: CategoryLinear, Orders: invalidItem}).validate()},
		{"empty amend batch", (BatchAmendOrdersRequest{Category: CategorySpot}).validate()},
		{"invalid amend item", (BatchAmendOrdersRequest{
			Category: CategorySpot, Orders: []AmendOrderRequest{{Symbol: "BTCUSDT", OrderID: "1"}},
		}).validate()},
		{"empty cancel batch", (BatchCancelOrdersRequest{Category: CategorySpot}).validate()},
		{"cancel item without identity", (BatchCancelOrdersRequest{
			Category: CategorySpot, Orders: []CancelOrderRequest{{Symbol: "BTCUSDT"}},
		}).validate()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, trade.ErrValidation) {
				t.Fatalf("validation error = %v, want validation error", test.err)
			}
		})
	}
}

func TestSetDisconnectCancelAll(t *testing.T) {
	t.Parallel()

	client, recorder, limiter := newExtTestClient(
		t, extReadTradePermissions, extBody(`{"retCode":0,"retMsg":"success"}`),
	)
	ack, err := client.SetDisconnectCancelAll(context.Background(), DisconnectCancelAllRequest{TimeWindow: 40})
	if err != nil || len(ack.Raw) != 0 {
		t.Fatalf("SetDisconnectCancelAll() = %+v, error = %v", ack, err)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/order/disconnected-cancel-all")
	assertJSONEqual(t, call.Body, `{"timeWindow":40}`)
	snapshot, err := limiter.Snapshot(rateLimitEndpointKey("account", "bybit-main", "/v5/order/disconnected-cancel-all"))
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("dcp limiter snapshot = %+v, error = %v", snapshot, err)
	}
	for _, window := range []int{2, 301} {
		if err := (DisconnectCancelAllRequest{TimeWindow: window}).validate(); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validate(%d) error = %v, want validation error", window, err)
		}
	}
	for _, window := range []int{3, 300} {
		if err := (DisconnectCancelAllRequest{TimeWindow: window}).validate(); err != nil {
			t.Fatalf("validate(%d) error = %v", window, err)
		}
	}
}

func TestSpotBorrowCheck(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(
		`{"symbol":"BTCUSDT","maxTradeQty":"6.6065","side":"Buy","spotMaxTradeAmount":"9004.75628594",`+
			`"maxTradeAmount":"218014.01330797","borrowCoin":"USDT","spotMaxTradeQty":"0.2728"}`,
	))
	quota, err := client.SpotBorrowCheck(
		context.Background(), SpotBorrowCheckRequest{Symbol: "BTCUSDT", Side: SideBuy},
	)
	if err != nil || quota.MaxTradeQuantity != "6.6065" || quota.MaxTradeAmount != "218014.01330797" ||
		quota.SpotMaxTradeQuantity != "0.2728" || quota.SpotMaxTradeAmount != "9004.75628594" ||
		quota.BorrowCoin != "USDT" || quota.Side != SideBuy || len(quota.Raw) == 0 {
		t.Fatalf("SpotBorrowCheck() = %+v, error = %v", quota, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/order/spot-borrow-check")
	want := url.Values{"category": {"spot"}, "symbol": {"BTCUSDT"}, "side": {"Buy"}}
	if !reflect.DeepEqual(call.Query, want) || call.Body != "" {
		t.Fatalf("query = %v body = %q, want %v and empty body", call.Query, call.Body, want)
	}
	for _, request := range []SpotBorrowCheckRequest{
		{Side: SideBuy}, {Symbol: "BTCUSDT"}, {Symbol: "BTCUSDT", Side: "Hold"},
	} {
		if err := request.validate(); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validate(%+v) error = %v, want validation error", request, err)
		}
	}
}

// extMutationCase는 Trade 권한이 필요한 변경 메서드 하나를 유효한 요청으로 호출한다.
type extMutationCase struct {
	name string
	call func(*Client) error
}

func extMutationCases() []extMutationCase {
	amend := AmendOrderRequest{Category: CategoryLinear, Symbol: "BTCUSDT", OrderID: "1", Price: "64000"}
	order := PlaceOrderRequest{
		Symbol: "BTCUSDT", Side: SideBuy, OrderType: OrderTypeLimit, Quantity: "0.01", Price: "25000",
	}
	cancel := CancelOrderRequest{Symbol: "BTCUSDT", OrderID: "1"}
	return []extMutationCase{
		{"AmendOrder", func(client *Client) error {
			_, err := client.AmendOrder(context.Background(), amend)
			return err
		}},
		{"CancelAllOrders", func(client *Client) error {
			_, err := client.CancelAllOrders(
				context.Background(), CancelAllOrdersRequest{Category: CategoryLinear, Symbol: "BTCUSDT"},
			)
			return err
		}},
		{"PlaceBatchOrders", func(client *Client) error {
			_, err := client.PlaceBatchOrders(
				context.Background(), BatchPlaceOrdersRequest{Category: CategoryLinear, Orders: []PlaceOrderRequest{order}},
			)
			return err
		}},
		{"AmendBatchOrders", func(client *Client) error {
			_, err := client.AmendBatchOrders(
				context.Background(), BatchAmendOrdersRequest{Category: CategoryLinear, Orders: []AmendOrderRequest{amend}},
			)
			return err
		}},
		{"CancelBatchOrders", func(client *Client) error {
			_, err := client.CancelBatchOrders(
				context.Background(), BatchCancelOrdersRequest{Category: CategoryLinear, Orders: []CancelOrderRequest{cancel}},
			)
			return err
		}},
		{"SetDisconnectCancelAll", func(client *Client) error {
			_, err := client.SetDisconnectCancelAll(context.Background(), DisconnectCancelAllRequest{TimeWindow: 10})
			return err
		}},
	}
}

func TestTradeExtMutationsAreNotRetried(t *testing.T) {
	t.Parallel()

	for _, test := range extMutationCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extServerError)
			if err := test.call(client); !errors.Is(err, trade.ErrUnknownExecutionState) {
				t.Fatalf("error = %v, want unknown execution state", err)
			}
			if calls := recorder.snapshot(); len(calls) != 1 {
				t.Fatalf("server received %d requests, want exactly one without retry", len(calls))
			}
		})
	}
}

func TestTradeExtMutationsRequireTradePermission(t *testing.T) {
	t.Parallel()

	for _, test := range extMutationCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(
				t, extReadPermissions, extResult(`{"orderId":"1","orderLinkId":""}`),
			)
			if err := test.call(client); !errors.Is(err, trade.ErrAuthorization) {
				t.Fatalf("error = %v, want authorization", err)
			}
			if calls := recorder.snapshot(); len(calls) != 0 {
				t.Fatalf("server received %d requests, want none without Trade permission", len(calls))
			}
		})
	}
}
