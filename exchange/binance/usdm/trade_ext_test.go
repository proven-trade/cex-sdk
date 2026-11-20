package usdm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	accountOrdersTenSecondsKey = "binance-usdm:account:futures-main:orders:10seconds"
	accountOrdersMinuteKey     = "binance-usdm:account:futures-main:orders:1minute"
)

// signedCase는 서명 엔드포인트 한 건의 기대 요청과 응답, limiter 사용량이다.
// params는 서명·timestamp·recvWindow를 제외한 기대 파라미터이고 tenSeconds·minute은 주문 count 사용량이다.
// permissions를 비우면 read·trade 권한 클라이언트를 쓴다.
type signedCase struct {
	name        string
	method      string
	path        string
	params      url.Values
	body        string
	weight      int
	tenSeconds  int
	minute      int
	permissions []credential.Permission
	run         func(t *testing.T, client *Client)
}

func newPermissionTestClient(t *testing.T, baseURL string, provider *recordingProvider, now time.Time, permissions ...credential.Permission) (*Client, *ratelimit.Limiter) {
	t.Helper()
	limiter, _ := ratelimit.New()
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: &directSender{}, Limiter: limiter})
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{Executor: executor, Credentials: &credential.Descriptor{AccountID: "futures-main", Exchange: model.ExchangeBinance, SecretRef: "secret/futures", Permissions: permissions, AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"}}, CredentialProvider: provider, DefaultEgressRouteID: "route-a", BaseURL: baseURL, AllowInsecureHTTP: true, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return client, limiter
}

// requireUsage는 limiter 규칙의 사용량을 확인한다. 규칙이 없으면 사용량 0으로 본다.
func requireUsage(t *testing.T, limiter *ratelimit.Limiter, key string, want int) {
	t.Helper()
	snapshot, err := limiter.Snapshot(key)
	switch {
	case errors.Is(err, ratelimit.ErrUnknownRule):
		if want != 0 {
			t.Fatalf("%s rule is missing, want used %d", key, want)
		}
	case err != nil:
		t.Fatalf("Snapshot(%s) error = %v", key, err)
	case snapshot.Used != want:
		t.Fatalf("%s used = %d, want %d", key, snapshot.Used, want)
	}
}

// runSignedCases는 케이스마다 서버를 띄워 method·path·파라미터, HMAC 서명과 API Key 헤더,
// secret 조회 횟수와 limiter 사용량을 검증한다. 응답 해석은 run이 맡는다.
func runSignedCases(t *testing.T, cases []signedCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			now := time.UnixMilli(marketStartMS)
			server, requests := newMarketServer(t, tc.body, nil)
			provider := &recordingProvider{}
			permissions := tc.permissions
			if permissions == nil {
				permissions = []credential.Permission{credential.PermissionRead, credential.PermissionTrade}
			}
			client, limiter := newPermissionTestClient(t, server.URL, provider, now, permissions...)
			tc.run(t, client)

			got := requests()
			if len(got) != 1 {
				t.Fatalf("requests = %+v, want exactly one", got)
			}
			if got[0].method != tc.method || got[0].path != tc.path {
				t.Fatalf("request = %s %s, want %s %s", got[0].method, got[0].path, tc.method, tc.path)
			}
			signed := &http.Request{URL: &url.URL{RawQuery: got[0].query}, Header: got[0].header}
			if !verifySignedRequest(signed, []byte("secret-key"), now) {
				t.Fatalf("signature, timestamp, recvWindow or API key header is invalid: %s", got[0].query)
			}
			values, err := url.ParseQuery(got[0].query)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"signature", "timestamp", "recvWindow"} {
				values.Del(key)
			}
			if values.Encode() != tc.params.Encode() {
				t.Fatalf("params = %s, want %s", values.Encode(), tc.params.Encode())
			}
			if calls, _, _ := provider.snapshot(); calls != 1 {
				t.Fatalf("secret resolutions = %d, want 1", calls)
			}
			requireUsage(t, limiter, marketWeightKey, tc.weight)
			requireUsage(t, limiter, accountOrdersTenSecondsKey, tc.tenSeconds)
			requireUsage(t, limiter, accountOrdersMinuteKey, tc.minute)
		})
	}
}

func TestTradeOrderExtensions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	orderID, modifyID := int64(42), int64(7)
	start, end := time.UnixMilli(marketStartMS), time.UnixMilli(marketEndMS)
	limitOrder := PlaceOrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit, TimeInForce: TimeInForceGTC, Quantity: "0.01", Price: "64000", ClientOrderID: "b-1"}
	reduceOrder := PlaceOrderRequest{Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeMarket, Quantity: "0.01", ReduceOnly: true}
	modifyByID := ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: &orderID, Side: SideBuy, Quantity: "0.02", Price: "64100", ModifyID: &modifyID}
	modifyByClient := ModifyOrderRequest{Symbol: "BTCUSDT", ClientOrderID: "c-2", Side: SideSell, Quantity: "0.03", PriceMatch: "QUEUE"}
	orderBody := `{"orderId":42,"symbol":"BTCUSDT","status":"NEW","clientOrderId":"b-1","price":"64000","avgPrice":"0.00000","origQty":"0.01","executedQty":"0","cumQuote":"0","timeInForce":"GTC","type":"LIMIT","origType":"LIMIT","reduceOnly":false,"closePosition":false,"side":"BUY","positionSide":"BOTH","stopPrice":"0","workingType":"CONTRACT_PRICE","priceProtect":false,"updateTime":1700000000000}`
	modifiedBody := `{"orderId":42,"symbol":"BTCUSDT","pair":"BTCUSDT","status":"NEW","clientOrderId":"b-1","price":"64100","avgPrice":"0.00000","origQty":"0.02","executedQty":"0","cumQty":"0","cumBase":"0","timeInForce":"GTC","type":"LIMIT","reduceOnly":true,"closePosition":false,"side":"BUY","positionSide":"BOTH","stopPrice":"0","workingType":"CONTRACT_PRICE","priceProtect":false,"origType":"LIMIT","updateTime":1700000000100,"modifyId":7}`
	placeBatch := `[{"newClientOrderId":"b-1","price":"64000","quantity":"0.01","side":"BUY","symbol":"BTCUSDT","timeInForce":"GTC","type":"LIMIT"},{"quantity":"0.01","reduceOnly":"true","side":"SELL","symbol":"BTCUSDT","type":"MARKET"}]`
	modifyBatch := `[{"modifyId":"7","orderId":"42","price":"64100","quantity":"0.02","side":"BUY","symbol":"BTCUSDT"},{"origClientOrderId":"c-2","priceMatch":"QUEUE","quantity":"0.03","side":"SELL","symbol":"BTCUSDT"}]`
	batchBody := `[{"orderId":42,"symbol":"BTCUSDT","status":"NEW","clientOrderId":"b-1","price":"64000","origQty":"0.01","executedQty":"0","timeInForce":"GTC","type":"LIMIT","side":"BUY","positionSide":"BOTH","updateTime":1700000000000},{"code":-2022,"msg":"ReduceOnly Order is rejected."}]`
	modifyBatchBody := `[{"orderId":42,"symbol":"BTCUSDT","status":"NEW","price":"64100","origQty":"0.02","modifyId":7,"updateTime":1700000000100},{"code":-2013,"msg":"Order does not exist."}]`
	cancelBatchBody := `[{"orderId":42,"symbol":"BTCUSDT","status":"CANCELED","clientOrderId":"a"},{"code":-2011,"msg":"Unknown order sent."}]`
	infoParams := url.Values{"symbol": {"BTCUSDT"}, "orderId": {"42"}}
	symbolParams := url.Values{"symbol": {"BTCUSDT"}}

	runSignedCases(t, []signedCase{
		{name: "test order", method: http.MethodPost, path: "/fapi/v1/order/test", params: url.Values{"symbol": {"BTCUSDT"}, "side": {"BUY"}, "type": {"LIMIT"}, "timeInForce": {"GTC"}, "quantity": {"0.01"}, "price": {"64000"}, "newClientOrderId": {"b-1"}}, body: orderBody, run: func(t *testing.T, client *Client) {
			order, err := client.TestOrder(ctx, limitOrder)
			if err != nil || order.OrderID != 42 || order.ClientOrderID != "b-1" || order.Status != "NEW" || len(order.Raw) == 0 {
				t.Fatalf("TestOrder() = %+v, error = %v", order, err)
			}
		}},
		{name: "open order", method: http.MethodGet, path: "/fapi/v1/openOrder", params: infoParams, body: orderBody, weight: 1, run: func(t *testing.T, client *Client) {
			order, err := client.OpenOrder(ctx, OrderInfoRequest{Symbol: "BTCUSDT", OrderID: &orderID})
			if err != nil || order.OrderID != 42 || order.Price != "64000" || len(order.Raw) == 0 {
				t.Fatalf("OpenOrder() = %+v, error = %v", order, err)
			}
		}},
		{name: "modify order", method: http.MethodPut, path: "/fapi/v1/order", params: url.Values{"symbol": {"BTCUSDT"}, "side": {"BUY"}, "quantity": {"0.02"}, "orderId": {"42"}, "price": {"64100"}, "modifyId": {"7"}, "reduceOnly": {"true"}}, body: modifiedBody, tenSeconds: 1, minute: 1, run: func(t *testing.T, client *Client) {
			request := modifyByID
			request.ReduceOnly = true
			order, err := client.ModifyOrder(ctx, request)
			if err != nil || order.OrderID != 42 || order.Price != "64100" || order.OriginalQuantity != "0.02" || order.ModifyID != 7 || !order.ReduceOnly || len(order.Raw) == 0 {
				t.Fatalf("ModifyOrder() = %+v, error = %v", order, err)
			}
		}},
		{name: "place batch orders", method: http.MethodPost, path: "/fapi/v1/batchOrders", params: url.Values{"batchOrders": {placeBatch}}, body: batchBody, weight: 5, tenSeconds: 5, minute: 1, run: func(t *testing.T, client *Client) {
			results, err := client.PlaceBatchOrders(ctx, PlaceBatchOrdersRequest{Orders: []PlaceOrderRequest{limitOrder, reduceOrder}})
			if err != nil || len(results) != 2 || results[0].Failed() || results[0].OrderID != 42 || len(results[0].Raw) == 0 {
				t.Fatalf("PlaceBatchOrders() = %+v, error = %v", results, err)
			}
			if !results[1].Failed() || results[1].Code != -2022 || results[1].Message != "ReduceOnly Order is rejected." || len(results[1].Raw) == 0 {
				t.Fatalf("PlaceBatchOrders() rejected item = %+v", results[1])
			}
		}},
		{name: "modify batch orders", method: http.MethodPut, path: "/fapi/v1/batchOrders", params: url.Values{"batchOrders": {modifyBatch}}, body: modifyBatchBody, weight: 5, tenSeconds: 5, minute: 1, run: func(t *testing.T, client *Client) {
			results, err := client.ModifyBatchOrders(ctx, ModifyBatchOrdersRequest{Orders: []ModifyOrderRequest{modifyByID, modifyByClient}})
			if err != nil || len(results) != 2 || results[0].Failed() || results[0].ModifyID != 7 || results[0].OriginalQuantity != "0.02" || !results[1].Failed() || results[1].Code != -2013 {
				t.Fatalf("ModifyBatchOrders() = %+v, error = %v", results, err)
			}
		}},
		{name: "cancel batch orders by id", method: http.MethodDelete, path: "/fapi/v1/batchOrders", params: url.Values{"symbol": {"BTCUSDT"}, "orderIdList": {"[42,43]"}}, body: cancelBatchBody, weight: 1, run: func(t *testing.T, client *Client) {
			results, err := client.CancelBatchOrders(ctx, CancelBatchOrdersRequest{Symbol: "BTCUSDT", OrderIDs: []int64{42, 43}})
			if err != nil || len(results) != 2 || results[0].Failed() || results[0].Status != "CANCELED" || !results[1].Failed() || results[1].Code != -2011 {
				t.Fatalf("CancelBatchOrders() = %+v, error = %v", results, err)
			}
		}},
		{name: "cancel batch orders by client id", method: http.MethodDelete, path: "/fapi/v1/batchOrders", params: url.Values{"symbol": {"BTCUSDT"}, "origClientOrderIdList": {`["a","b"]`}}, body: cancelBatchBody, weight: 1, run: func(t *testing.T, client *Client) {
			results, err := client.CancelBatchOrders(ctx, CancelBatchOrdersRequest{Symbol: "BTCUSDT", ClientOrderIDs: []string{"a", "b"}})
			if err != nil || len(results) != 2 || results[0].ClientOrderID != "a" {
				t.Fatalf("CancelBatchOrders() = %+v, error = %v", results, err)
			}
		}},
		{name: "cancel all open orders", method: http.MethodDelete, path: "/fapi/v1/allOpenOrders", params: symbolParams, body: `{"code":200,"msg":"The operation of cancel all open order is done."}`, weight: 1, run: func(t *testing.T, client *Client) {
			result, err := client.CancelAllOpenOrders(ctx, CancelAllOpenOrdersRequest{Symbol: "BTCUSDT"})
			if err != nil || result.Code != 200 || result.Message != "The operation of cancel all open order is done." || len(result.Raw) == 0 {
				t.Fatalf("CancelAllOpenOrders() = %+v, error = %v", result, err)
			}
		}},
		{name: "cancel all algo open orders", method: http.MethodDelete, path: "/fapi/v1/algoOpenOrders", params: symbolParams, body: `{"code":"200","msg":"success"}`, weight: 1, run: func(t *testing.T, client *Client) {
			result, err := client.CancelAllAlgoOpenOrders(ctx, CancelAllAlgoOpenOrdersRequest{Symbol: "BTCUSDT"})
			if err != nil || result.Code != 200 || result.Message != "success" {
				t.Fatalf("CancelAllAlgoOpenOrders() = %+v, error = %v", result, err)
			}
		}},
		{name: "countdown cancel all", method: http.MethodPost, path: "/fapi/v1/countdownCancelAll", params: url.Values{"symbol": {"BTCUSDT"}, "countdownTime": {"120000"}}, body: `{"symbol":"BTCUSDT","countdownTime":"120000"}`, weight: 10, run: func(t *testing.T, client *Client) {
			result, err := client.CountdownCancelAll(ctx, CountdownCancelAllRequest{Symbol: "BTCUSDT", Countdown: 2 * time.Minute})
			if err != nil || result.Symbol != "BTCUSDT" || result.CountdownTime.String() != "120000" || len(result.Raw) == 0 {
				t.Fatalf("CountdownCancelAll() = %+v, error = %v", result, err)
			}
		}},
		{name: "order amendments", method: http.MethodGet, path: "/fapi/v1/orderAmendment", params: url.Values{"symbol": {"BTCUSDT"}, "orderId": {"42"}, "startTime": {"1700000000000"}, "endTime": {"1700001800000"}, "limit": {"10"}}, body: `[{"amendmentId":5363,"symbol":"BTCUSDT","pair":"BTCUSDT","orderId":42,"clientOrderId":"b-1","time":1629184560899,"amendment":{"price":{"before":"30004","after":"30003.2"},"origQty":{"before":"1","after":"2"},"count":3,"modifyId":9}}]`, weight: 1, run: func(t *testing.T, client *Client) {
			items, err := client.OrderAmendments(ctx, OrderAmendmentsRequest{Symbol: "BTCUSDT", OrderID: &orderID, StartTime: &start, EndTime: &end, Limit: 10})
			if err != nil || len(items) != 1 || items[0].AmendmentID != 5363 || items[0].OrderID != 42 || items[0].Amendment.Price.After != "30003.2" || items[0].Amendment.OriginalQuantity.After != "2" || items[0].Amendment.Count != 3 || items[0].Amendment.ModifyID != 9 || len(items[0].Raw) == 0 {
				t.Fatalf("OrderAmendments() = %+v, error = %v", items, err)
			}
		}},
	})
}

func TestTradePositionExtensions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	start, end := time.UnixMilli(marketStartMS), time.UnixMilli(marketEndMS)
	orderID, fromID := int64(42), int64(698759)
	ackBody := `{"code":200,"msg":"success"}`
	tradeBody := `[{"symbol":"BTCUSDT","id":698759,"orderId":25851813,"side":"BUY","price":"7819.01","qty":"0.002","realizedPnl":"-0.91539999","quoteQty":"15.63802","marginAsset":"USDT","commission":"-0.07819010","commissionAsset":"USDT","time":1569514978020,"positionSide":"SHORT","maker":false,"buyer":true}]`
	forceBody := `[{"orderId":6071832819,"symbol":"BTCUSDT","status":"FILLED","clientOrderId":"autoclose-1596107620040000020","price":"10871.09","avgPrice":"10913.21000","origQty":"0.001","executedQty":"0.001","cumQuote":"10.91321","timeInForce":"IOC","type":"LIMIT","reduceOnly":false,"closePosition":false,"side":"SELL","positionSide":"BOTH","stopPrice":"0","workingType":"CONTRACT_PRICE","origType":"LIMIT","time":1596107620044,"updateTime":1596107620087}]`

	runSignedCases(t, []signedCase{
		{name: "change leverage", method: http.MethodPost, path: "/fapi/v1/leverage", params: url.Values{"symbol": {"BTCUSDT"}, "leverage": {"20"}}, body: `{"leverage":20,"maxNotionalValue":"250000","symbol":"BTCUSDT"}`, weight: 1, run: func(t *testing.T, client *Client) {
			result, err := client.ChangeLeverage(ctx, ChangeLeverageRequest{Symbol: "BTCUSDT", Leverage: 20})
			if err != nil || result.Symbol != "BTCUSDT" || result.Leverage != 20 || result.MaxNotionalValue != "250000" || len(result.Raw) == 0 {
				t.Fatalf("ChangeLeverage() = %+v, error = %v", result, err)
			}
		}},
		{name: "change position mode", method: http.MethodPost, path: "/fapi/v1/positionSide/dual", params: url.Values{"dualSidePosition": {"true"}}, body: ackBody, weight: 1, run: func(t *testing.T, client *Client) {
			result, err := client.ChangePositionMode(ctx, ChangePositionModeRequest{DualSidePosition: true})
			if err != nil || result.Code != 200 || result.Message != "success" {
				t.Fatalf("ChangePositionMode() = %+v, error = %v", result, err)
			}
		}},
		{name: "change multi assets mode off", method: http.MethodPost, path: "/fapi/v1/multiAssetsMargin", params: url.Values{"multiAssetsMargin": {"false"}}, body: ackBody, weight: 1, run: func(t *testing.T, client *Client) {
			result, err := client.ChangeMultiAssetsMode(ctx, ChangeMultiAssetsModeRequest{})
			if err != nil || result.Code != 200 {
				t.Fatalf("ChangeMultiAssetsMode() = %+v, error = %v", result, err)
			}
		}},
		{name: "modify position margin", method: http.MethodPost, path: "/fapi/v1/positionMargin", params: url.Values{"symbol": {"BTCUSDT"}, "positionSide": {"LONG"}, "amount": {"100"}, "type": {"1"}}, body: `{"amount":100.0,"code":200,"msg":"Successfully modify position margin.","type":1}`, weight: 1, run: func(t *testing.T, client *Client) {
			result, err := client.ModifyPositionMargin(ctx, ModifyPositionMarginRequest{Symbol: "BTCUSDT", PositionSide: PositionSideLong, Amount: "100", Type: PositionMarginAdd})
			if err != nil || result.Amount.String() != "100.0" || result.Code != 200 || result.Type != PositionMarginAdd || len(result.Raw) == 0 {
				t.Fatalf("ModifyPositionMargin() = %+v, error = %v", result, err)
			}
		}},
		{name: "position margin history", method: http.MethodGet, path: "/fapi/v1/positionMargin/history", params: url.Values{"symbol": {"BTCUSDT"}, "type": {"2"}, "startTime": {"1700000000000"}, "endTime": {"1700001800000"}, "limit": {"5"}}, body: `[{"symbol":"BTCUSDT","type":1,"deltaType":"USER_ADJUST","amount":"23.36332311","asset":"USDT","time":1578047897183,"positionSide":"BOTH"}]`, weight: 1, run: func(t *testing.T, client *Client) {
			items, err := client.PositionMarginHistory(ctx, PositionMarginHistoryRequest{Symbol: "BTCUSDT", Type: PositionMarginReduce, StartTime: &start, EndTime: &end, Limit: 5})
			if err != nil || len(items) != 1 || items[0].Type != PositionMarginAdd || items[0].DeltaType != "USER_ADJUST" || items[0].Amount != "23.36332311" || items[0].PositionSide != PositionSideBoth || len(items[0].Raw) == 0 {
				t.Fatalf("PositionMarginHistory() = %+v, error = %v", items, err)
			}
		}},
		{name: "user trades by order", method: http.MethodGet, path: "/fapi/v1/userTrades", params: url.Values{"symbol": {"BTCUSDT"}, "orderId": {"42"}, "startTime": {"1700000000000"}, "endTime": {"1700001800000"}, "limit": {"500"}}, body: tradeBody, weight: 5, run: func(t *testing.T, client *Client) {
			items, err := client.UserTrades(ctx, UserTradesRequest{Symbol: "BTCUSDT", OrderID: &orderID, StartTime: &start, EndTime: &end, Limit: 500})
			if err != nil || len(items) != 1 || items[0].ID != 698759 || items[0].OrderID != 25851813 || items[0].Side != SideBuy || items[0].PositionSide != PositionSideShort || items[0].Price != "7819.01" || items[0].Quantity != "0.002" || items[0].RealizedPnl != "-0.91539999" || items[0].Commission != "-0.07819010" || !items[0].Buyer || items[0].Maker || len(items[0].Raw) == 0 {
				t.Fatalf("UserTrades() = %+v, error = %v", items, err)
			}
		}},
		{name: "user trades from id", method: http.MethodGet, path: "/fapi/v1/userTrades", params: url.Values{"symbol": {"BTCUSDT"}, "fromId": {"698759"}}, body: tradeBody, weight: 5, run: func(t *testing.T, client *Client) {
			items, err := client.UserTrades(ctx, UserTradesRequest{Symbol: "BTCUSDT", FromID: &fromID})
			if err != nil || len(items) != 1 || items[0].MarginAsset != "USDT" {
				t.Fatalf("UserTrades() = %+v, error = %v", items, err)
			}
		}},
		{name: "force orders with symbol", method: http.MethodGet, path: "/fapi/v1/forceOrders", params: url.Values{"symbol": {"BTCUSDT"}, "autoCloseType": {"LIQUIDATION"}, "startTime": {"1700000000000"}, "limit": {"2"}}, body: forceBody, weight: 20, run: func(t *testing.T, client *Client) {
			items, err := client.ForceOrders(ctx, ForceOrdersRequest{Symbol: "BTCUSDT", AutoCloseType: AutoCloseLiquidation, StartTime: &start, Limit: 2})
			if err != nil || len(items) != 1 || items[0].OrderID != 6071832819 || items[0].Status != "FILLED" || items[0].TimeInForce != TimeInForceIOC || len(items[0].Raw) == 0 {
				t.Fatalf("ForceOrders() = %+v, error = %v", items, err)
			}
		}},
		{name: "force orders all symbols", method: http.MethodGet, path: "/fapi/v1/forceOrders", body: forceBody, weight: 50, run: func(t *testing.T, client *Client) {
			items, err := client.ForceOrders(ctx, ForceOrdersRequest{})
			if err != nil || len(items) != 1 {
				t.Fatalf("ForceOrders() = %+v, error = %v", items, err)
			}
		}},
		{name: "adl quantiles", method: http.MethodGet, path: "/fapi/v1/adlQuantile", params: url.Values{"symbol": {"BTCUSDT"}}, body: `[{"symbol":"ETHUSDT","adlQuantile":{"LONG":3,"SHORT":3,"BOTH":0}},{"symbol":"BTCUSDT","adlQuantile":{"LONG":1,"SHORT":2,"HEDGE":0}}]`, weight: 5, run: func(t *testing.T, client *Client) {
			items, err := client.ADLQuantiles(ctx, ADLQuantileRequest{Symbol: "BTCUSDT"})
			if err != nil || len(items) != 2 || items[0].Symbol != "ETHUSDT" || items[0].Quantile[PositionSideLong] != 3 || items[0].Quantile[PositionSideBoth] != 0 {
				t.Fatalf("ADLQuantiles() = %+v, error = %v", items, err)
			}
			if hedge, ok := items[1].Quantile["HEDGE"]; !ok || hedge != 0 || items[1].Quantile[PositionSideShort] != 2 || len(items[1].Raw) == 0 {
				t.Fatalf("ADLQuantiles() hedge item = %+v", items[1])
			}
		}},
		{name: "sign tradfi perps contract", method: http.MethodPost, path: "/fapi/v1/stock/contract", body: ackBody, weight: 50, run: func(t *testing.T, client *Client) {
			result, err := client.SignTradFiPerpsContract(ctx)
			if err != nil || result.Code != 200 {
				t.Fatalf("SignTradFiPerpsContract() = %+v, error = %v", result, err)
			}
		}},
	})
}

func TestStatusCodeAcceptsNumberAndString(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
		want StatusCode
	}{
		{"number", `{"code":200,"msg":"ok"}`, 200},
		{"string", `{"code":"200","msg":"ok"}`, 200},
		{"negative", `{"code":-4046,"msg":"No need to change margin type."}`, -4046},
		{"null", `{"code":null}`, 0},
		{"missing", `{"msg":"ok"}`, 0},
	} {
		var result AckResult
		if err := json.Unmarshal([]byte(tc.body), &result); err != nil || result.Code != tc.want {
			t.Errorf("%s: Unmarshal() = %+v, error = %v, want code %d", tc.name, result, err, tc.want)
		}
	}
	var result AckResult
	if err := json.Unmarshal([]byte(`{"code":"abc"}`), &result); err == nil {
		t.Errorf("Unmarshal(non numeric code) = %+v, want error", result)
	}
}

func TestTradeExtensionValidation(t *testing.T) {
	t.Parallel()
	one, zero, negative := int64(1), int64(0), int64(-1)
	begin := time.UnixMilli(marketStartMS)
	later := begin.Add(time.Hour)
	earlier := begin.Add(-time.Hour)
	sevenDays := begin.Add(7 * 24 * time.Hour)
	eightDays := begin.Add(8 * 24 * time.Hour)
	thirtyDays := begin.Add(30 * 24 * time.Hour)
	thirtyOneDays := begin.Add(31 * 24 * time.Hour)
	limitOrder := PlaceOrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit, TimeInForce: TimeInForceGTC, Quantity: "1", Price: "2"}
	badOrder := PlaceOrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit}
	modify := ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: &one, Side: SideBuy, Quantity: "1", Price: "2"}
	sixOrders := []PlaceOrderRequest{limitOrder, limitOrder, limitOrder, limitOrder, limitOrder, limitOrder}
	sixModifies := []ModifyOrderRequest{modify, modify, modify, modify, modify, modify}
	elevenIDs := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	elevenClientIDs := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"}
	reduceModify := modify
	reduceModify.ReduceOnly = true

	for _, tc := range []validationCase{
		{"modify no symbol", ModifyOrderRequest{OrderID: &one, Side: SideBuy, Quantity: "1", Price: "2"}.validate()},
		{"modify no identity", ModifyOrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Quantity: "1", Price: "2"}.validate()},
		{"modify both identities", ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: &one, ClientOrderID: "a", Side: SideBuy, Quantity: "1", Price: "2"}.validate()},
		{"modify zero order id", ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: &zero, Side: SideBuy, Quantity: "1", Price: "2"}.validate()},
		{"modify padded client id", ModifyOrderRequest{Symbol: "BTCUSDT", ClientOrderID: " a", Side: SideBuy, Quantity: "1", Price: "2"}.validate()},
		{"modify bad side", ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: &one, Side: "HOLD", Quantity: "1", Price: "2"}.validate()},
		{"modify no quantity", ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: &one, Side: SideBuy, Price: "2"}.validate()},
		{"modify zero quantity", ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: &one, Side: SideBuy, Quantity: "0", Price: "2"}.validate()},
		{"modify no price", ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: &one, Side: SideBuy, Quantity: "1"}.validate()},
		{"modify price and priceMatch", ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: &one, Side: SideBuy, Quantity: "1", Price: "2", PriceMatch: "QUEUE"}.validate()},
		{"modify bad priceMatch", ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: &one, Side: SideBuy, Quantity: "1", PriceMatch: "BEST"}.validate()},
		{"modify negative modifyId", ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: &one, Side: SideBuy, Quantity: "1", Price: "2", ModifyID: &negative}.validate()},
		{"place batch empty", PlaceBatchOrdersRequest{}.validate()},
		{"place batch six", PlaceBatchOrdersRequest{Orders: sixOrders}.validate()},
		{"place batch invalid item", PlaceBatchOrdersRequest{Orders: []PlaceOrderRequest{limitOrder, badOrder}}.validate()},
		{"modify batch empty", ModifyBatchOrdersRequest{}.validate()},
		{"modify batch six", ModifyBatchOrdersRequest{Orders: sixModifies}.validate()},
		{"modify batch reduceOnly", ModifyBatchOrdersRequest{Orders: []ModifyOrderRequest{reduceModify}}.validate()},
		{"modify batch invalid item", ModifyBatchOrdersRequest{Orders: []ModifyOrderRequest{modify, {Symbol: "BTCUSDT"}}}.validate()},
		{"cancel batch no symbol", CancelBatchOrdersRequest{OrderIDs: []int64{1}}.validate()},
		{"cancel batch no identity", CancelBatchOrdersRequest{Symbol: "BTCUSDT"}.validate()},
		{"cancel batch both identities", CancelBatchOrdersRequest{Symbol: "BTCUSDT", OrderIDs: []int64{1}, ClientOrderIDs: []string{"a"}}.validate()},
		{"cancel batch eleven ids", CancelBatchOrdersRequest{Symbol: "BTCUSDT", OrderIDs: elevenIDs}.validate()},
		{"cancel batch eleven client ids", CancelBatchOrdersRequest{Symbol: "BTCUSDT", ClientOrderIDs: elevenClientIDs}.validate()},
		{"cancel batch zero id", CancelBatchOrdersRequest{Symbol: "BTCUSDT", OrderIDs: []int64{1, 0}}.validate()},
		{"cancel batch blank client id", CancelBatchOrdersRequest{Symbol: "BTCUSDT", ClientOrderIDs: []string{""}}.validate()},
		{"countdown no symbol", CountdownCancelAllRequest{Countdown: time.Second}.validate()},
		{"countdown negative", CountdownCancelAllRequest{Symbol: "BTCUSDT", Countdown: -time.Second}.validate()},
		{"countdown sub millisecond", CountdownCancelAllRequest{Symbol: "BTCUSDT", Countdown: 500 * time.Microsecond}.validate()},
		{"amendments no identity", OrderAmendmentsRequest{Symbol: "BTCUSDT"}.validate()},
		{"amendments limit", OrderAmendmentsRequest{Symbol: "BTCUSDT", OrderID: &one, Limit: 101}.validate()},
		{"amendments reversed", OrderAmendmentsRequest{Symbol: "BTCUSDT", OrderID: &one, StartTime: &later, EndTime: &begin}.validate()},
		{"leverage no symbol", ChangeLeverageRequest{Leverage: 10}.validate()},
		{"leverage zero", ChangeLeverageRequest{Symbol: "BTCUSDT"}.validate()},
		{"position margin no amount", ModifyPositionMarginRequest{Symbol: "BTCUSDT", Type: PositionMarginAdd}.validate()},
		{"position margin zero amount", ModifyPositionMarginRequest{Symbol: "BTCUSDT", Amount: "0", Type: PositionMarginAdd}.validate()},
		{"position margin bad type", ModifyPositionMarginRequest{Symbol: "BTCUSDT", Amount: "1", Type: 3}.validate()},
		{"position margin bad side", ModifyPositionMarginRequest{Symbol: "BTCUSDT", PositionSide: "UP", Amount: "1", Type: PositionMarginAdd}.validate()},
		{"margin history bad type", PositionMarginHistoryRequest{Symbol: "BTCUSDT", Type: 3}.validate()},
		{"margin history negative limit", PositionMarginHistoryRequest{Symbol: "BTCUSDT", Limit: -1}.validate()},
		{"margin history reversed", PositionMarginHistoryRequest{Symbol: "BTCUSDT", StartTime: &later, EndTime: &begin}.validate()},
		{"margin history range", PositionMarginHistoryRequest{Symbol: "BTCUSDT", StartTime: &begin, EndTime: &thirtyOneDays}.validate()},
		{"userTrades no symbol", UserTradesRequest{}.validate()},
		{"userTrades zero order id", UserTradesRequest{Symbol: "BTCUSDT", OrderID: &zero}.validate()},
		{"userTrades negative from id", UserTradesRequest{Symbol: "BTCUSDT", FromID: &negative}.validate()},
		{"userTrades fromId with startTime", UserTradesRequest{Symbol: "BTCUSDT", FromID: &one, StartTime: &begin}.validate()},
		{"userTrades fromId with endTime", UserTradesRequest{Symbol: "BTCUSDT", FromID: &one, EndTime: &later}.validate()},
		{"userTrades limit", UserTradesRequest{Symbol: "BTCUSDT", Limit: 1001}.validate()},
		{"userTrades reversed", UserTradesRequest{Symbol: "BTCUSDT", StartTime: &later, EndTime: &earlier}.validate()},
		{"userTrades range", UserTradesRequest{Symbol: "BTCUSDT", StartTime: &begin, EndTime: &eightDays}.validate()},
		{"forceOrders padded symbol", ForceOrdersRequest{Symbol: " BTCUSDT"}.validate()},
		{"forceOrders bad type", ForceOrdersRequest{AutoCloseType: "FORCED"}.validate()},
		{"forceOrders limit", ForceOrdersRequest{Limit: 101}.validate()},
		{"forceOrders reversed", ForceOrdersRequest{StartTime: &later, EndTime: &begin}.validate()},
	} {
		if !errors.Is(tc.err, trade.ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", tc.name, tc.err)
		}
	}

	fiveOrders := []PlaceOrderRequest{limitOrder, limitOrder, limitOrder, limitOrder, limitOrder}
	fiveModifies := []ModifyOrderRequest{modify, modify, modify, modify, modify}
	tenIDs := elevenIDs[:10]
	for _, tc := range []validationCase{
		{"modify by order id", modify.validate()},
		{"modify by client id priceMatch", ModifyOrderRequest{Symbol: "BTCUSDT", ClientOrderID: "a", Side: SideSell, Quantity: "1", PriceMatch: "OPPONENT_20"}.validate()},
		{"modify zero modifyId", ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: &one, Side: SideBuy, Quantity: "1", Price: "2", ModifyID: &zero}.validate()},
		{"place batch five", PlaceBatchOrdersRequest{Orders: fiveOrders}.validate()},
		{"modify batch five", ModifyBatchOrdersRequest{Orders: fiveModifies}.validate()},
		{"cancel batch ten ids", CancelBatchOrdersRequest{Symbol: "BTCUSDT", OrderIDs: tenIDs}.validate()},
		{"cancel batch client ids", CancelBatchOrdersRequest{Symbol: "BTCUSDT", ClientOrderIDs: []string{"a"}}.validate()},
		{"countdown stop", CountdownCancelAllRequest{Symbol: "BTCUSDT"}.validate()},
		{"amendments by client id", OrderAmendmentsRequest{Symbol: "BTCUSDT", ClientOrderID: "a", Limit: 100}.validate()},
		{"position margin reduce", ModifyPositionMarginRequest{Symbol: "BTCUSDT", PositionSide: PositionSideShort, Amount: "1.5", Type: PositionMarginReduce}.validate()},
		{"margin history thirty days", PositionMarginHistoryRequest{Symbol: "BTCUSDT", StartTime: &begin, EndTime: &thirtyDays}.validate()},
		{"userTrades fromId", UserTradesRequest{Symbol: "BTCUSDT", FromID: &zero, Limit: 1000}.validate()},
		{"userTrades seven days", UserTradesRequest{Symbol: "BTCUSDT", StartTime: &begin, EndTime: &sevenDays}.validate()},
		{"forceOrders all", ForceOrdersRequest{AutoCloseType: AutoCloseADL, Limit: 100}.validate()},
	} {
		if tc.err != nil {
			t.Errorf("%s: error = %v, want nil", tc.name, tc.err)
		}
	}
}

func TestTradeExtensionRejectsInvalidInputBeforeNetwork(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sender := &directSender{}
	provider := &recordingProvider{}
	client, _ := newTestClient(t, "http://127.0.0.1", sender, provider, time.UnixMilli(marketStartMS), []transport.EgressRouteID{"route-a"})
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"cancel all open orders", func() error {
			_, err := client.CancelAllOpenOrders(ctx, CancelAllOpenOrdersRequest{})
			return err
		}},
		{"cancel all algo open orders", func() error {
			_, err := client.CancelAllAlgoOpenOrders(ctx, CancelAllAlgoOpenOrdersRequest{Symbol: " BTCUSDT"})
			return err
		}},
		{"adl quantiles", func() error {
			_, err := client.ADLQuantiles(ctx, ADLQuantileRequest{Symbol: "BTCUSDT "})
			return err
		}},
		{"modify order", func() error {
			_, err := client.ModifyOrder(ctx, ModifyOrderRequest{Symbol: "BTCUSDT"})
			return err
		}},
		{"place batch orders", func() error {
			_, err := client.PlaceBatchOrders(ctx, PlaceBatchOrdersRequest{})
			return err
		}},
		{"test order", func() error {
			_, err := client.TestOrder(ctx, PlaceOrderRequest{Symbol: "BTCUSDT"})
			return err
		}},
	} {
		if err := tc.call(); !errors.Is(err, trade.ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", tc.name, err)
		}
	}
	if calls, _, _ := provider.snapshot(); calls != 0 || len(sender.snapshot()) != 0 {
		t.Fatalf("secret resolutions = %d, routes = %v, want none", calls, sender.snapshot())
	}
}

func TestTradeExtensionPermissionsAreCheckedBeforeSecretResolution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	orderID := int64(42)
	limitOrder := PlaceOrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit, TimeInForce: TimeInForceGTC, Quantity: "1", Price: "2"}
	modify := ModifyOrderRequest{Symbol: "BTCUSDT", OrderID: &orderID, Side: SideBuy, Quantity: "1", Price: "2"}
	server, requests := newMarketServer(t, `{}`, nil)
	provider := &recordingProvider{}
	client, _ := newPermissionTestClient(t, server.URL, provider, time.UnixMilli(marketStartMS), credential.PermissionRead)
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"test order", func() error {
			_, err := client.TestOrder(ctx, limitOrder)
			return err
		}},
		{"modify order", func() error {
			_, err := client.ModifyOrder(ctx, modify)
			return err
		}},
		{"place batch orders", func() error {
			_, err := client.PlaceBatchOrders(ctx, PlaceBatchOrdersRequest{Orders: []PlaceOrderRequest{limitOrder}})
			return err
		}},
		{"modify batch orders", func() error {
			_, err := client.ModifyBatchOrders(ctx, ModifyBatchOrdersRequest{Orders: []ModifyOrderRequest{modify}})
			return err
		}},
		{"cancel batch orders", func() error {
			_, err := client.CancelBatchOrders(ctx, CancelBatchOrdersRequest{Symbol: "BTCUSDT", OrderIDs: []int64{1}})
			return err
		}},
		{"cancel all open orders", func() error {
			_, err := client.CancelAllOpenOrders(ctx, CancelAllOpenOrdersRequest{Symbol: "BTCUSDT"})
			return err
		}},
		{"cancel all algo open orders", func() error {
			_, err := client.CancelAllAlgoOpenOrders(ctx, CancelAllAlgoOpenOrdersRequest{Symbol: "BTCUSDT"})
			return err
		}},
		{"countdown cancel all", func() error {
			_, err := client.CountdownCancelAll(ctx, CountdownCancelAllRequest{Symbol: "BTCUSDT", Countdown: time.Second})
			return err
		}},
		{"change leverage", func() error {
			_, err := client.ChangeLeverage(ctx, ChangeLeverageRequest{Symbol: "BTCUSDT", Leverage: 5})
			return err
		}},
		{"change position mode", func() error {
			_, err := client.ChangePositionMode(ctx, ChangePositionModeRequest{})
			return err
		}},
		{"change multi assets mode", func() error {
			_, err := client.ChangeMultiAssetsMode(ctx, ChangeMultiAssetsModeRequest{})
			return err
		}},
		{"modify position margin", func() error {
			_, err := client.ModifyPositionMargin(ctx, ModifyPositionMarginRequest{Symbol: "BTCUSDT", Amount: "1", Type: PositionMarginAdd})
			return err
		}},
		{"change fee burn", func() error {
			_, err := client.ChangeFeeBurn(ctx, ChangeFeeBurnRequest{Enabled: true})
			return err
		}},
		{"sign tradfi perps contract", func() error {
			_, err := client.SignTradFiPerpsContract(ctx)
			return err
		}},
	} {
		if err := tc.call(); !errors.Is(err, trade.ErrAuthorization) {
			t.Errorf("%s: error = %v, want ErrAuthorization", tc.name, err)
		}
	}
	if calls, _, _ := provider.snapshot(); calls != 0 || len(requests()) != 0 {
		t.Fatalf("secret resolutions = %d, requests = %+v, want none", calls, requests())
	}
}

func TestBatchOrdersAreNotRetriedAfterUnknownExecution(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempts.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(writer, `{"code":-1007,"msg":"Timeout waiting for response from backend server. Send status unknown; execution status unknown."}`)
	}))
	t.Cleanup(server.Close)
	client, _ := newPermissionTestClient(t, server.URL, &recordingProvider{}, time.UnixMilli(marketStartMS), credential.PermissionRead, credential.PermissionTrade)
	limitOrder := PlaceOrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit, TimeInForce: TimeInForceGTC, Quantity: "1", Price: "2"}
	_, err := client.PlaceBatchOrders(context.Background(), PlaceBatchOrdersRequest{Orders: []PlaceOrderRequest{limitOrder}})
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.Category != trade.ErrorUnknownExecutionState || apiError.Retryable || apiError.ExchangeCode != "-1007" {
		t.Fatalf("PlaceBatchOrders() error = %v, want unknown execution state", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want exactly one", attempts.Load())
	}
}
