package futures

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/transport"
)

type recordedRequest struct {
	method string
	path   string
	query  string
	body   string
}

// newExtServer는 요청을 기록하고 path별 고정 응답을 돌려준다. private 요청은 서명을 검증한다.
func newExtServer(
	t *testing.T,
	private bool,
	responses map[string]string,
) (*httptest.Server, func() recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var last recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if private && !verifySignedRequest(t, request, []byte("test-secret"), time.UnixMilli(1_700_000_000_000)) {
			http.Error(writer, `{"code":"400005","msg":"Invalid signature"}`, http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(request.Body)
		mu.Lock()
		last = recordedRequest{
			method: request.Method, path: request.URL.Path, query: request.URL.RawQuery, body: string(body),
		}
		mu.Unlock()
		response, ok := responses[request.URL.Path]
		if !ok {
			http.NotFound(writer, request)
			return
		}
		_, _ = io.WriteString(writer, response)
	}))
	t.Cleanup(server.Close)
	return server, func() recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return last
	}
}

func newExtClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return time.UnixMilli(1_700_000_000_000) },
	)
	return client
}

func expectRequest(t *testing.T, got recordedRequest, method, path, query, body string) {
	t.Helper()
	if got.method != method || got.path != path || got.query != query || got.body != body {
		t.Fatalf("request = %+v, want %s %s ?%s body=%s", got, method, path, query, body)
	}
}

func TestMarketExtEndpoints(t *testing.T) {
	t.Parallel()

	server, last := newExtServer(t, false, map[string]string{
		"/api/v1/allTickers":                    `{"code":"200000","data":[{"sequence":1,"symbol":"XBTUSDTM","side":"sell","size":21,"tradeId":"t1","price":"67153","bestBidPrice":"67153","bestBidSize":2767,"bestAskPrice":"67153.1","bestAskSize":5368,"ts":1729163466659000000}]}`,
		"/api/v1/level2/snapshot":               `{"code":"200000","data":{"sequence":1697895963339,"symbol":"XBTUSDM","bids":[[66968,2]],"asks":[[66968.1,13501]],"ts":1729168101216000000}}`,
		"/api/v1/mark-price/XBTUSDTM/current":   `{"code":"200000","data":{"symbol":"XBTUSDTM","granularity":1000,"timePoint":1729254307000,"value":67687.08,"indexPrice":67683.58}}`,
		"/api/v1/index/query":                   `{"code":"200000","data":{"hasMore":true,"dataList":[{"symbol":".KXBTUSDT","granularity":1000,"timePoint":1730557515000,"value":69202.94,"decomposionList":[{"exchange":"gateio","price":69209.27,"weight":0.0533}]}]}}`,
		"/api/v1/interest/query":                `{"code":"200000","data":{"hasMore":false,"dataList":[{"symbol":".XBTINT","granularity":60000,"timePoint":1728692100000,"value":0.0003}]}}`,
		"/api/v1/premium/query":                 `{"code":"200000","data":{"hasMore":false,"dataList":[{"symbol":".XBTUSDTMPI","granularity":60000,"timePoint":1730557980000,"value":-0.000025}]}}`,
		"/api/v1/trade-statistics":              `{"code":"200000","data":{"turnoverOf24h":1115573341.3273683}}`,
		"/api/v1/timestamp":                     `{"code":"200000","data":1729260030774}`,
		"/api/v1/status":                        `{"code":"200000","data":{"msg":"","status":"open"}}`,
		"/api/v1/funding-rate/XBTUSDTM/current": `{"code":"200000","data":{"symbol":".XBTUSDTMFPI8H","granularity":28800000,"timePoint":1748462400000,"value":0.000061,"predictedValue":0.000109,"fundingRateCap":0.003,"fundingRateFloor":-0.003,"period":0,"fundingTime":1748491200000}}`,
		"/api/v1/contract/funding-rates":        `{"code":"200000","data":[{"symbol":"XBTUSDTM","fundingRate":0.00021,"timepoint":1702296000000}]}`,
		"/api/v1/contracts/risk-limit/XBTUSDTM": `{"code":"200000","data":[{"symbol":"XBTUSDTM","level":1,"maxRiskLimit":100000,"minRiskLimit":0,"maxLeverage":125,"initialMargin":0.008,"maintainMargin":0.004}]}`,
	})
	client := newExtClient(t, server)
	ctx := context.Background()
	start := time.UnixMilli(1_728_663_338_000)
	end := time.UnixMilli(1_728_692_138_000)
	reverse := false

	tickers, err := client.AllTickers(ctx)
	if err != nil || len(tickers) != 1 || tickers[0].BestAsk != "67153.1" || len(tickers[0].Raw) == 0 {
		t.Fatalf("AllTickers() = %+v, %v", tickers, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/allTickers", "", "")

	book, err := client.FullOrderBook(ctx, "XBTUSDM")
	if err != nil || book.Sequence != 1697895963339 || book.Bids[0].Price != "66968" || book.Asks[0].Size != "13501" {
		t.Fatalf("FullOrderBook() = %+v, %v", book, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/level2/snapshot", "symbol=XBTUSDM", "")

	mark, err := client.MarkPrice(ctx, "XBTUSDTM")
	if err != nil || mark.Value != "67687.08" || mark.IndexPrice != "67683.58" {
		t.Fatalf("MarkPrice() = %+v, %v", mark, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/mark-price/XBTUSDTM/current", "", "")

	index, err := client.SpotIndexPrices(ctx, IndexHistoryRequest{
		Symbol: ".KXBTUSDT", StartAt: &start, EndAt: &end, Reverse: &reverse, Offset: 5, MaxCount: 20,
	})
	if err != nil || !index.HasMore || len(index.Points) != 1 ||
		index.Points[0].Components[0].Exchange != "gateio" || index.Points[0].Components[0].Weight != "0.0533" {
		t.Fatalf("SpotIndexPrices() = %+v, %v", index, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/index/query",
		"endAt=1728692138000&maxCount=20&offset=5&reverse=false&startAt=1728663338000&symbol=.KXBTUSDT", "")

	interest, err := client.InterestRates(ctx, IndexHistoryRequest{Symbol: ".XBTINT"})
	if err != nil || len(interest.Points) != 1 || interest.Points[0].Value != "0.0003" {
		t.Fatalf("InterestRates() = %+v, %v", interest, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/interest/query", "symbol=.XBTINT", "")

	premium, err := client.PremiumIndexes(ctx, IndexHistoryRequest{Symbol: ".XBTUSDTMPI"})
	if err != nil || len(premium.Points) != 1 || premium.Points[0].Value != "-0.000025" {
		t.Fatalf("PremiumIndexes() = %+v, %v", premium, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/premium/query", "symbol=.XBTUSDTMPI", "")

	statistics, err := client.TradeStatistics(ctx)
	if err != nil || statistics.Turnover24h != "1115573341.3273683" {
		t.Fatalf("TradeStatistics() = %+v, %v", statistics, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/trade-statistics", "", "")

	serverTime, err := client.ServerTime(ctx)
	if err != nil || serverTime.UnixMilli() != 1729260030774 {
		t.Fatalf("ServerTime() = %v, %v", serverTime, err)
	}

	status, err := client.ServiceStatus(ctx)
	if err != nil || status.Status != "open" {
		t.Fatalf("ServiceStatus() = %+v, %v", status, err)
	}

	rate, err := client.CurrentFundingRate(ctx, "XBTUSDTM")
	if err != nil || rate.Value != "0.000061" || rate.FundingRateFloor != "-0.003" || rate.FundingTime != 1748491200000 {
		t.Fatalf("CurrentFundingRate() = %+v, %v", rate, err)
	}

	history, err := client.FundingRateHistory(ctx, FundingRateHistoryRequest{
		Symbol: "XBTUSDTM", From: time.UnixMilli(1_700_310_700_000), To: time.UnixMilli(1_702_310_700_000),
	})
	if err != nil || len(history) != 1 || history[0].FundingRate != "0.00021" || history[0].TimePoint != 1702296000000 {
		t.Fatalf("FundingRateHistory() = %+v, %v", history, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/contract/funding-rates",
		"from=1700310700000&symbol=XBTUSDTM&to=1702310700000", "")

	levels, err := client.RiskLimitLevels(ctx, "XBTUSDTM")
	if err != nil || len(levels) != 1 || levels[0].MaxLeverage != 125 || levels[0].InitialMargin != "0.008" {
		t.Fatalf("RiskLimitLevels() = %+v, %v", levels, err)
	}
}

func TestMarketExtValidation(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(1_700_000_000_000)
	later := now.Add(time.Hour)
	tests := []struct {
		name string
		err  error
	}{
		{"index symbol", (IndexHistoryRequest{Symbol: "xbt"}).validate()},
		{"index range", (IndexHistoryRequest{Symbol: ".XBTINT", StartAt: &later, EndAt: &now}).validate()},
		{"index max count", (IndexHistoryRequest{Symbol: ".XBTINT", MaxCount: 101}).validate()},
		{"funding range", (FundingRateHistoryRequest{Symbol: "XBTUSDTM", From: later, To: now}).validate()},
		{"funding symbol", (FundingRateHistoryRequest{Symbol: "bad", From: now, To: later}).validate()},
	}
	for _, test := range tests {
		if !errors.Is(test.err, trade.ErrValidation) {
			t.Errorf("%s error = %v, want validation", test.name, test.err)
		}
	}
}

func TestTradeExtEndpoints(t *testing.T) {
	t.Parallel()

	order := `{"id":"order-1","symbol":"XBTUSDTM","type":"limit","side":"buy","price":"64000","size":2,"value":"128","dealValue":"0","dealSize":0,"stop":"down","stopPriceType":"TP","stopTriggered":false,"stopPrice":"63000","timeInForce":"GTC","leverage":"10","clientOid":"strategy-1","isActive":true,"createdAt":1700000000000,"marginMode":"ISOLATED","positionSide":"BOTH","avgDealPrice":"0","filledSize":0,"filledValue":"0","status":"open"}`
	closedOrder := `{"id":"order-2","symbol":"XBTUSDTM","type":"market","side":"buy","price":"0","size":1,"stopPrice":null,"status":"done","isActive":false}`
	server, last := newExtServer(t, true, map[string]string{
		"/api/v1/orders/test":         `{"code":"200000","data":{"orderId":"test-1","clientOid":"strategy-1"}}`,
		"/api/v1/orders/multi":        `{"code":"200000","data":[{"orderId":"o1","clientOid":"c1","symbol":"XBTUSDTM","code":"200000","msg":"success"},{"orderId":"","clientOid":"c2","symbol":"XBTUSDTM","code":"300003","msg":"Balance insufficient"}]}`,
		"/api/v1/st-orders":           `{"code":"200000","data":{"orderId":"st-1","clientOid":"tpsl-1"}}`,
		"/api/v1/orders/multi-cancel": `{"code":"200000","data":[{"orderId":"o1","clientOid":null,"code":"200","msg":"success"}]}`,
		"/api/v3/orders":              `{"code":"200000","data":{"cancelledOrderIds":["o1","o2"]}}`,
		"/api/v1/stopOrders":          `{"code":"200000","data":{"cancelledOrderIds":["s1"]}}`,
		"/api/v1/orders/byClientOid":  `{"code":"200000","data":` + order + `}`,
		"/api/v1/orders":              `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":1,"totalPage":1,"items":[` + closedOrder + `]}}`,
		"/api/v1/recentDoneOrders":    `{"code":"200000","data":[` + closedOrder + `]}`,
		"/api/v1/openOrderStatistics": `{"code":"200000","data":{"openOrderBuySize":1,"openOrderSellSize":0,"openOrderBuyCost":"0.0001","openOrderSellCost":"0","settleCurrency":"USDT"}}`,
		"/api/v1/recentFills":         `{"code":"200000","data":[{"symbol":"XBTUSDTM","tradeId":"1784277229880","orderId":"o1","side":"buy","liquidity":"taker","price":"67430.9","size":1,"value":"67.4309","feeRate":"0.00060","feeCurrency":"USDT","fee":"0.04045854","marginMode":"ISOLATED","positionSide":"BOTH","orderType":"market","tradeType":"trade","tradeTime":1729155616320000000,"createdAt":1729155616493}]}`,
	})
	client := newExtClient(t, server)
	ctx := context.Background()
	place := PlaceOrderRequest{
		ClientOrderID: "strategy-1", Symbol: "XBTUSDTM", MarginMode: MarginModeIsolated, Leverage: 10,
		PositionSide: PositionSideBoth, Side: SideBuy, Type: OrderTypeLimit, Size: 2, Price: "64000",
		TimeInForce: TimeInForceGTC,
	}
	placeJSON := `{"clientOid":"strategy-1","symbol":"XBTUSDTM","marginMode":"ISOLATED","leverage":10,"positionSide":"BOTH","side":"buy","type":"limit","size":2,"price":"64000","timeInForce":"GTC"}`

	reference, err := client.PlaceOrderTest(ctx, place)
	if err != nil || reference.OrderID != "test-1" || len(reference.Raw) == 0 {
		t.Fatalf("PlaceOrderTest() = %+v, %v", reference, err)
	}
	expectRequest(t, last(), http.MethodPost, "/api/v1/orders/test", "", placeJSON)

	batch, err := client.BatchPlaceOrders(ctx, []PlaceOrderRequest{place})
	if err != nil || len(batch) != 2 || batch[0].Code != "200000" || batch[1].Code != "300003" || batch[1].ClientOrderID != "c2" {
		t.Fatalf("BatchPlaceOrders() = %+v, %v", batch, err)
	}
	expectRequest(t, last(), http.MethodPost, "/api/v1/orders/multi", "", "["+placeJSON+"]")

	tpsl, err := client.PlaceTakeProfitStopLossOrder(ctx, TakeProfitStopLossOrderRequest{
		PlaceOrderRequest: place, StopPriceType: StopPriceTypeMark,
		TriggerStopUpPrice: "70000", TriggerStopDownPrice: "60000",
	})
	if err != nil || tpsl.OrderID != "st-1" {
		t.Fatalf("PlaceTakeProfitStopLossOrder() = %+v, %v", tpsl, err)
	}
	expectRequest(t, last(), http.MethodPost, "/api/v1/st-orders", "",
		`{"clientOid":"strategy-1","symbol":"XBTUSDTM","marginMode":"ISOLATED","leverage":10,"positionSide":"BOTH","side":"buy","type":"limit","size":2,"price":"64000","timeInForce":"GTC","stopPriceType":"MP","triggerStopUpPrice":"70000","triggerStopDownPrice":"60000"}`)

	cancelled, err := client.BatchCancelOrders(ctx, BatchCancelOrdersRequest{OrderIDs: []string{"o1"}})
	if err != nil || len(cancelled) != 1 || cancelled[0].OrderID != "o1" || cancelled[0].ClientOrderID != "" {
		t.Fatalf("BatchCancelOrders() = %+v, %v", cancelled, err)
	}
	expectRequest(t, last(), http.MethodDelete, "/api/v1/orders/multi-cancel", "", `{"orderIdsList":["o1"]}`)
	if _, err = client.BatchCancelOrders(ctx, BatchCancelOrdersRequest{
		ClientOrders: []ClientOrderRef{{Symbol: "XBTUSDTM", ClientOrderID: "c1"}},
	}); err != nil {
		t.Fatalf("BatchCancelOrders(client) error = %v", err)
	}
	expectRequest(t, last(), http.MethodDelete, "/api/v1/orders/multi-cancel", "",
		`{"clientOidsList":[{"symbol":"XBTUSDTM","clientOid":"c1"}]}`)

	all, err := client.CancelAllOrders(ctx, CancelAllOrdersRequest{Symbol: "XBTUSDTM"})
	if err != nil || len(all.CancelledOrderIDs) != 2 {
		t.Fatalf("CancelAllOrders() = %+v, %v", all, err)
	}
	expectRequest(t, last(), http.MethodDelete, "/api/v3/orders", "symbol=XBTUSDTM", "")

	stops, err := client.CancelAllStopOrders(ctx, CancelAllStopOrdersRequest{})
	if err != nil || len(stops.CancelledOrderIDs) != 1 {
		t.Fatalf("CancelAllStopOrders() = %+v, %v", stops, err)
	}
	expectRequest(t, last(), http.MethodDelete, "/api/v1/stopOrders", "", "")

	byClient, err := client.OrderByClientOrderID(ctx, OrderByClientOrderIDRequest{ClientOrderID: "strategy-1"})
	if err != nil || byClient.ID != "order-1" || byClient.Stop != "down" || byClient.StopPrice != "63000" {
		t.Fatalf("OrderByClientOrderID() = %+v, %v", byClient, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/orders/byClientOid", "clientOid=strategy-1", "")

	start := time.UnixMilli(1_729_000_000_000)
	closed, err := client.ClosedOrders(ctx, ClosedOrdersRequest{
		Symbol: "XBTUSDTM", Side: SideBuy, Type: "market", StartAt: &start, CurrentPage: 2, PageSize: 500,
	})
	if err != nil || len(closed.Orders) != 1 || closed.Orders[0].Status != "done" || closed.Orders[0].StopPrice != "" {
		t.Fatalf("ClosedOrders() = %+v, %v", closed, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/orders",
		"currentPage=2&pageSize=500&side=buy&startAt=1729000000000&status=done&symbol=XBTUSDTM&type=market", "")

	recent, err := client.RecentClosedOrders(ctx, RecentClosedOrdersRequest{Symbol: "XBTUSDTM"})
	if err != nil || len(recent) != 1 || recent[0].ID != "order-2" {
		t.Fatalf("RecentClosedOrders() = %+v, %v", recent, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/recentDoneOrders", "symbol=XBTUSDTM", "")

	value, err := client.OpenOrderValue(ctx, OpenOrderValueRequest{Symbol: "XBTUSDTM"})
	if err != nil || value.OpenOrderBuySize != 1 || value.OpenOrderBuyCost != "0.0001" || value.SettleCurrency != "USDT" {
		t.Fatalf("OpenOrderValue() = %+v, %v", value, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/openOrderStatistics", "symbol=XBTUSDTM", "")

	fills, err := client.RecentFills(ctx, RecentFillsRequest{})
	if err != nil || len(fills) != 1 || fills[0].Fee != "0.04045854" || fills[0].OrderID != "o1" {
		t.Fatalf("RecentFills() = %+v, %v", fills, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/recentFills", "", "")
}

func TestStopOrdersEndpoint(t *testing.T) {
	t.Parallel()

	server, last := newExtServer(t, true, map[string]string{
		"/api/v1/stopOrders": `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":1,"totalPage":1,"items":[{"id":"stop-1","symbol":"XBTUSDTM","type":"limit","side":"sell","price":"70000","size":1,"stop":"up","stopPriceType":"TP","stopTriggered":false,"stopPrice":"69000","status":"open","isActive":true}]}}`,
	})
	client := newExtClient(t, server)
	page, err := client.StopOrders(context.Background(), StopOrdersRequest{
		Symbol: "XBTUSDTM", Side: SideSell, Type: OrderTypeLimit, PageSize: 10,
	})
	if err != nil || page.TotalNumber != 1 || len(page.Orders) != 1 ||
		page.Orders[0].Stop != "up" || page.Orders[0].StopPrice != "69000" {
		t.Fatalf("StopOrders() = %+v, %v", page, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/stopOrders", "pageSize=10&side=sell&symbol=XBTUSDTM&type=limit", "")
}

func TestTradeExtValidationAndPermissions(t *testing.T) {
	t.Parallel()

	valid := PlaceOrderRequest{
		ClientOrderID: "c1", Symbol: "XBTUSDTM", MarginMode: MarginModeCross, Side: SideBuy,
		Type: OrderTypeMarket, Size: 1,
	}
	tooMany := make([]string, 11)
	for index := range tooMany {
		tooMany[index] = "order"
	}
	tests := []struct {
		name string
		err  error
	}{
		{"tpsl without trigger", (TakeProfitStopLossOrderRequest{PlaceOrderRequest: valid}).validate()},
		{"tpsl bad type", (TakeProfitStopLossOrderRequest{PlaceOrderRequest: valid, StopPriceType: "XX", TriggerStopUpPrice: "1"}).validate()},
		{"tpsl bad price", (TakeProfitStopLossOrderRequest{PlaceOrderRequest: valid, TriggerStopDownPrice: "-1"}).validate()},
		{"batch cancel empty", (BatchCancelOrdersRequest{}).validate()},
		{"batch cancel both", (BatchCancelOrdersRequest{OrderIDs: []string{"a"}, ClientOrders: []ClientOrderRef{{Symbol: "XBTUSDTM", ClientOrderID: "c"}}}).validate()},
		{"batch cancel too many", (BatchCancelOrdersRequest{OrderIDs: tooMany}).validate()},
		{"batch cancel client symbol", (BatchCancelOrdersRequest{ClientOrders: []ClientOrderRef{{Symbol: "x", ClientOrderID: "c"}}}).validate()},
		{"closed type", (ClosedOrdersRequest{Type: "stop"}).validate()},
		{"closed page size", (ClosedOrdersRequest{PageSize: 1001}).validate()},
		{"stop side", (StopOrdersRequest{Side: "hold"}).validate()},
	}
	for _, test := range tests {
		if !errors.Is(test.err, trade.ErrValidation) {
			t.Errorf("%s error = %v, want validation", test.name, test.err)
		}
	}

	server, _ := newExtServer(t, true, map[string]string{})
	client := newExtClient(t, server)
	ctx := context.Background()
	if _, err := client.BatchPlaceOrders(ctx, nil); !errors.Is(err, trade.ErrValidation) {
		t.Errorf("BatchPlaceOrders(nil) error = %v, want validation", err)
	}
	if _, err := client.BatchPlaceOrders(ctx, make([]PlaceOrderRequest, 21)); !errors.Is(err, trade.ErrValidation) {
		t.Errorf("BatchPlaceOrders(21) error = %v, want validation", err)
	}
	if _, err := client.CancelAllOrders(ctx, CancelAllOrdersRequest{}); !errors.Is(err, trade.ErrValidation) {
		t.Errorf("CancelAllOrders() error = %v, want validation", err)
	}
	if _, err := client.OrderByClientOrderID(ctx, OrderByClientOrderIDRequest{}); !errors.Is(err, trade.ErrValidation) {
		t.Errorf("OrderByClientOrderID() error = %v, want validation", err)
	}
	// 서버에 없는 path는 404이며 mutation은 재시도 없이 한 번만 전송된다.
	if _, err := client.PlaceOrderTest(ctx, valid); err == nil || strings.Contains(err.Error(), "retry") {
		t.Errorf("PlaceOrderTest() on 404 error = %v", err)
	}
}

func TestPositionEndpoints(t *testing.T) {
	t.Parallel()

	detail := `{"id":"6200c9b83aecfb000152ddcd","symbol":"XBTUSDTM","maintMarginReq":"0.005","crossMode":false,"currentQty":1,"currentCost":"43.388","isOpen":true,"markPrice":"43536.65","posMargin":"2.19675944","unrealisedPnl":"0.14865","avgEntryPrice":"43388","liquidationPrice":"41440","settleCurrency":"USDT","isInverse":false,"marginMode":"ISOLATED","positionSide":"BOTH","leverage":"10","autoDeposit":false,"riskLimit":500000,"riskLimitLevel":2,"cumulativeFundingFee":"-0.03"}`
	server, last := newExtServer(t, true, map[string]string{
		"/api/v2/position":                            `{"code":"200000","data":[` + detail + `]}`,
		"/api/v1/history-positions":                   `{"code":"200000","data":{"currentPage":1,"pageSize":10,"totalNum":1,"totalPage":1,"items":[{"closeId":"500000000036305465","userId":"633559791e1cbc0001f319bc","symbol":"XBTUSDTM","settleCurrency":"USDT","leverage":"1.0","type":"CLOSE_LONG","pnl":"0.51214413","realisedGrossCost":"-0.5837","withdrawPnl":"0.0","tradeFee":"0.03766066","fundingFee":"-0.03389521","openTime":1735549162120,"closeTime":1735589352069,"openPrice":"93859.8","closePrice":"94443.5","marginMode":"CROSS","positionSide":"BOTH","tax":"0.0","roe":null,"liquidAmount":null,"liquidPrice":null,"side":"LONG"}]}}`,
		"/api/v2/position/getMarginMode":              `{"code":"200000","data":{"symbol":"XBTUSDTM","marginMode":"ISOLATED"}}`,
		"/api/v2/position/changeMarginMode":           `{"code":"200000","data":{"symbol":"XBTUSDTM","marginMode":"CROSS"}}`,
		"/api/v2/position/batchChangeMarginMode":      `{"code":"200000","data":{"marginMode":{"ETHUSDTM":"ISOLATED","XBTUSDTM":"CROSS"},"errors":[{"code":"50002","msg":"exist.order.or.position","symbol":"XBTUSDTM"}]}}`,
		"/api/v2/position/getPositionMode":            `{"code":"200000","data":{"positionMode":1}}`,
		"/api/v2/position/switchPositionMode":         `{"code":"200000","data":{"positionMode":"0"}}`,
		"/api/v2/getMaxOpenSize":                      `{"code":"200000","data":{"symbol":"XBTUSDTM","maxBuyOpenSize":12,"maxSellOpenSize":10}}`,
		"/api/v1/margin/maxWithdrawMargin":            `{"code":"200000","data":"21.1135719252"}`,
		"/api/v2/getCrossUserLeverage":                `{"code":"200000","data":{"symbol":"XBTUSDTM","leverage":"10"}}`,
		"/api/v2/changeCrossUserLeverage":             `{"code":"200000","data":true}`,
		"/api/v1/position/margin/deposit-margin":      `{"code":"200000","data":{"id":"6200c9b83aecfb000152ddcd","symbol":"XBTUSDTM","autoDeposit":false,"maintMarginReq":0.005,"riskLimit":500000,"realLeverage":18.72,"crossMode":false,"currentQty":1,"posMargin":2.19675944,"settleCurrency":"USDT"}}`,
		"/api/v1/margin/withdrawMargin":               `{"code":"200000","data":"0.5"}`,
		"/api/v1/position/margin/auto-deposit-status": `{"code":"200000","data":true}`,
		"/api/v2/batchGetCrossOrderLimit":             `{"code":"200000","data":[{"symbol":"XBTUSDTM","maxOpenSize":12102,"maxOpenValue":"1234549.2240000000","totalMargin":"10000","price":"102012","leverage":"125.00","mmr":"0.00416136","imr":"0.008","currency":"USDT"}]}`,
		"/api/v2/getCrossModeMarginRequirement":       `{"code":"200000","data":{"symbol":"XBTUSDTM","size":9,"positionValue":"1000","price":"105016","imr":"0.008","mmr":"0.00400013"}}`,
		"/api/v1/position/risk-limit-level/change":    `{"code":"200000","data":true}`,
	})
	client := newExtClient(t, server)
	ctx := context.Background()

	details, err := client.PositionDetails(ctx, "XBTUSDTM")
	if err != nil || len(details) != 1 || details[0].RiskLimitLevel != 2 || details[0].CumulativeFundingFee != "-0.03" ||
		details[0].MarginMode != MarginModeIsolated {
		t.Fatalf("PositionDetails() = %+v, %v", details, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v2/position", "symbol=XBTUSDTM", "")

	from := time.UnixMilli(1_735_000_000_000)
	history, err := client.ClosedPositions(ctx, ClosedPositionsRequest{Symbol: "XBTUSDTM", From: &from, Limit: 10, PageID: 1})
	if err != nil || len(history.Positions) != 1 || history.Positions[0].Type != "CLOSE_LONG" ||
		history.Positions[0].FundingFee != "-0.03389521" || history.Positions[0].ROE != "" {
		t.Fatalf("ClosedPositions() = %+v, %v", history, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/history-positions", "from=1735000000000&limit=10&pageId=1&symbol=XBTUSDTM", "")

	mode, err := client.MarginMode(ctx, "XBTUSDTM")
	if err != nil || mode.MarginMode != MarginModeIsolated {
		t.Fatalf("MarginMode() = %+v, %v", mode, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v2/position/getMarginMode", "symbol=XBTUSDTM", "")

	switched, err := client.SwitchMarginMode(ctx, SwitchMarginModeRequest{Symbol: "XBTUSDTM", MarginMode: MarginModeCross})
	if err != nil || switched.MarginMode != MarginModeCross {
		t.Fatalf("SwitchMarginMode() = %+v, %v", switched, err)
	}
	expectRequest(t, last(), http.MethodPost, "/api/v2/position/changeMarginMode", "", `{"symbol":"XBTUSDTM","marginMode":"CROSS"}`)

	batch, err := client.BatchSwitchMarginMode(ctx, BatchSwitchMarginModeRequest{
		MarginMode: MarginModeCross, Symbols: []string{"XBTUSDTM", "ETHUSDTM"},
	})
	if err != nil || batch.MarginModes["XBTUSDTM"] != MarginModeCross || len(batch.Errors) != 1 || batch.Errors[0].Code != "50002" {
		t.Fatalf("BatchSwitchMarginMode() = %+v, %v", batch, err)
	}
	expectRequest(t, last(), http.MethodPost, "/api/v2/position/batchChangeMarginMode", "",
		`{"marginMode":"CROSS","symbols":["XBTUSDTM","ETHUSDTM"]}`)

	positionMode, err := client.GetPositionMode(ctx)
	if err != nil || positionMode.PositionMode != PositionModeHedge {
		t.Fatalf("GetPositionMode() = %+v, %v", positionMode, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v2/position/getPositionMode", "", "")

	switchedMode, err := client.SwitchPositionMode(ctx, PositionModeOneWay)
	if err != nil || switchedMode.PositionMode != PositionModeOneWay {
		t.Fatalf("SwitchPositionMode() = %+v, %v", switchedMode, err)
	}
	expectRequest(t, last(), http.MethodPost, "/api/v2/position/switchPositionMode", "", `{"positionMode":"0"}`)

	maxOpen, err := client.MaxOpenSize(ctx, MaxOpenSizeRequest{Symbol: "XBTUSDTM", Price: "100000", Leverage: 10})
	if err != nil || maxOpen.MaxBuyOpenSize != 12 || maxOpen.MaxSellOpenSize != 10 {
		t.Fatalf("MaxOpenSize() = %+v, %v", maxOpen, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v2/getMaxOpenSize", "leverage=10&price=100000&symbol=XBTUSDTM", "")

	maxWithdraw, err := client.MaxWithdrawMargin(ctx, MaxWithdrawMarginRequest{Symbol: "XBTUSDTM", PositionSide: PositionSideLong})
	if err != nil || maxWithdraw != "21.1135719252" {
		t.Fatalf("MaxWithdrawMargin() = %q, %v", maxWithdraw, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/margin/maxWithdrawMargin", "positionSide=LONG&symbol=XBTUSDTM", "")

	leverage, err := client.CrossLeverage(ctx, "XBTUSDTM")
	if err != nil || leverage.Leverage != "10" {
		t.Fatalf("CrossLeverage() = %+v, %v", leverage, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v2/getCrossUserLeverage", "symbol=XBTUSDTM", "")

	changed, err := client.ChangeCrossLeverage(ctx, ChangeCrossLeverageRequest{Symbol: "XBTUSDTM", Leverage: "20"})
	if err != nil || !changed {
		t.Fatalf("ChangeCrossLeverage() = %v, %v", changed, err)
	}
	expectRequest(t, last(), http.MethodPost, "/api/v2/changeCrossUserLeverage", "", `{"symbol":"XBTUSDTM","leverage":"20"}`)

	added, err := client.AddIsolatedMargin(ctx, AddIsolatedMarginRequest{Symbol: "XBTUSDTM", Margin: "1.5", BizNo: "biz-1"})
	if err != nil || added.RealLeverage != "18.72" || added.PositionMargin != "2.19675944" || added.MaintenanceMarginReq != "0.005" {
		t.Fatalf("AddIsolatedMargin() = %+v, %v", added, err)
	}
	expectRequest(t, last(), http.MethodPost, "/api/v1/position/margin/deposit-margin", "",
		`{"symbol":"XBTUSDTM","margin":1.5,"bizNo":"biz-1"}`)

	removed, err := client.RemoveIsolatedMargin(ctx, RemoveIsolatedMarginRequest{
		Symbol: "XBTUSDTM", WithdrawAmount: "0.5", PositionSide: PositionSideShort,
	})
	if err != nil || removed != "0.5" {
		t.Fatalf("RemoveIsolatedMargin() = %q, %v", removed, err)
	}
	expectRequest(t, last(), http.MethodPost, "/api/v1/margin/withdrawMargin", "",
		`{"symbol":"XBTUSDTM","withdrawAmount":"0.5","positionSide":"SHORT"}`)

	enabled := true
	auto, err := client.SetAutoDeposit(ctx, AutoDepositRequest{Symbol: "XBTUSDTM", Status: &enabled})
	if err != nil || !auto {
		t.Fatalf("SetAutoDeposit() = %v, %v", auto, err)
	}
	expectRequest(t, last(), http.MethodPost, "/api/v1/position/margin/auto-deposit-status", "",
		`{"symbol":"XBTUSDTM","status":true}`)

	limits, err := client.CrossRiskLimits(ctx, CrossRiskLimitRequest{Symbol: "XBTUSDTM", TotalMargin: "10000", Leverage: 125})
	if err != nil || len(limits) != 1 || limits[0].MaxOpenSize != 12102 || limits[0].IMR != "0.008" {
		t.Fatalf("CrossRiskLimits() = %+v, %v", limits, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v2/batchGetCrossOrderLimit", "leverage=125&symbol=XBTUSDTM&totalMargin=10000", "")

	requirements, err := client.CrossMarginRequirements(ctx, CrossMarginRequirementRequest{Symbol: "XBTUSDTM", PositionValue: "1000"})
	if err != nil || len(requirements) != 1 || requirements[0].MMR != "0.00400013" || requirements[0].Size != "9" {
		t.Fatalf("CrossMarginRequirements() = %+v, %v", requirements, err)
	}
	expectRequest(t, last(), http.MethodPost, "/api/v2/getCrossModeMarginRequirement", "", `{"symbol":"XBTUSDTM","positionValue":"1000"}`)

	level, err := client.ChangeRiskLimitLevel(ctx, ChangeRiskLimitLevelRequest{Symbol: "XBTUSDTM", Level: 3})
	if err != nil || !level {
		t.Fatalf("ChangeRiskLimitLevel() = %v, %v", level, err)
	}
	expectRequest(t, last(), http.MethodPost, "/api/v1/position/risk-limit-level/change", "", `{"symbol":"XBTUSDTM","level":3}`)
}

func TestCrossMarginRequirementsAcceptsArray(t *testing.T) {
	t.Parallel()

	server, _ := newExtServer(t, true, map[string]string{
		"/api/v2/getCrossModeMarginRequirement": `{"code":"200000","data":[{"symbol":"XBTUSDTM","imr":"0.00800000","mmr":"0.00400013","size":9,"positionValue":"1000","price":"105016"}]}`,
	})
	client := newExtClient(t, server)
	items, err := client.CrossMarginRequirements(
		context.Background(), CrossMarginRequirementRequest{Symbol: "XBTUSDTM", Leverage: "10", PositionValue: "1000"},
	)
	if err != nil || len(items) != 1 || items[0].IMR != "0.00800000" {
		t.Fatalf("CrossMarginRequirements() = %+v, %v", items, err)
	}
}

func TestPositionValidation(t *testing.T) {
	t.Parallel()

	enabled := true
	server, _ := newExtServer(t, true, map[string]string{})
	client := newExtClient(t, server)
	ctx := context.Background()
	now := time.UnixMilli(1_700_000_000_000)
	later := now.Add(time.Hour)
	calls := []struct {
		name string
		err  error
	}{
		{"closed positions limit", (ClosedPositionsRequest{Limit: 201}).validate()},
		{"closed positions range", (ClosedPositionsRequest{From: &later, To: &now}).validate()},
	}
	add := func(name string, err error) {
		calls = append(calls, struct {
			name string
			err  error
		}{name, err})
	}
	_, err := client.PositionDetails(ctx, "bad")
	add("position details", err)
	_, err = client.SwitchMarginMode(ctx, SwitchMarginModeRequest{Symbol: "XBTUSDTM", MarginMode: "AUTO"})
	add("switch margin mode", err)
	_, err = client.BatchSwitchMarginMode(ctx, BatchSwitchMarginModeRequest{MarginMode: MarginModeCross})
	add("batch margin mode empty", err)
	_, err = client.SwitchPositionMode(ctx, "2")
	add("position mode", err)
	_, err = client.MaxOpenSize(ctx, MaxOpenSizeRequest{Symbol: "XBTUSDTM", Price: "0", Leverage: 10})
	add("max open size price", err)
	_, err = client.MaxOpenSize(ctx, MaxOpenSizeRequest{Symbol: "XBTUSDTM", Price: "1", Leverage: 0})
	add("max open size leverage", err)
	_, err = client.MaxWithdrawMargin(ctx, MaxWithdrawMarginRequest{Symbol: "XBTUSDTM", PositionSide: "UP"})
	add("max withdraw side", err)
	_, err = client.ChangeCrossLeverage(ctx, ChangeCrossLeverageRequest{Symbol: "XBTUSDTM", Leverage: "x"})
	add("cross leverage", err)
	_, err = client.AddIsolatedMargin(ctx, AddIsolatedMarginRequest{Symbol: "XBTUSDTM", Margin: "1"})
	add("add margin biz no", err)
	_, err = client.AddIsolatedMargin(ctx, AddIsolatedMarginRequest{Symbol: "XBTUSDTM", Margin: "0", BizNo: "b"})
	add("add margin amount", err)
	_, err = client.RemoveIsolatedMargin(ctx, RemoveIsolatedMarginRequest{Symbol: "XBTUSDTM", WithdrawAmount: "-1"})
	add("remove margin amount", err)
	_, err = client.SetAutoDeposit(ctx, AutoDepositRequest{Symbol: "XBTUSDTM"})
	add("auto deposit empty", err)
	_, err = client.SetAutoDeposit(ctx, AutoDepositRequest{Symbol: "XBTUSDTM", Status: &enabled, LongStatus: &enabled})
	add("auto deposit mixed", err)
	_, err = client.CrossRiskLimits(ctx, CrossRiskLimitRequest{Symbol: "XBTUSDTM", Leverage: 2000})
	add("cross risk leverage", err)
	_, err = client.CrossMarginRequirements(ctx, CrossMarginRequirementRequest{Symbol: "XBTUSDTM"})
	add("margin requirement value", err)
	_, err = client.ChangeRiskLimitLevel(ctx, ChangeRiskLimitLevelRequest{Symbol: "XBTUSDTM"})
	add("risk limit level", err)
	for _, call := range calls {
		if !errors.Is(call.err, trade.ErrValidation) {
			t.Errorf("%s error = %v, want validation", call.name, call.err)
		}
	}
}

func TestAccountExtEndpoints(t *testing.T) {
	t.Parallel()

	server, last := newExtServer(t, true, map[string]string{
		"/api/v1/transaction-history":  `{"code":"200000","data":{"hasMore":false,"dataList":[{"time":1728665747000,"type":"TransferIn","amount":0.01,"fee":0,"accountEquity":14.02924938,"status":"Completed","remark":"Transferred from High-Frequency Trading Account","offset":51360793,"currency":"USDT"}]}}`,
		"/api/v1/account-overview-all": `{"code":"200000","data":{"summary":{"accountEquityTotal":103.899081508,"unrealisedPNLTotal":38.81075,"marginBalanceTotal":65.336985668,"positionMarginTotal":68.9588320683,"orderMarginTotal":0,"frozenFundsTotal":0,"availableBalanceTotal":67.2492494397,"currency":"USDT"},"accounts":[{"accountName":"main","accountEquity":76.593630228,"unrealisedPNL":16.26175,"marginBalance":60.580534388,"positionMargin":44.8364570708,"orderMargin":0,"frozenFunds":0,"availableBalance":41.5171731572,"currency":"USDT"}]}}`,
		"/api/v1/trade-fees":           `{"code":"200000","data":{"symbol":"XBTUSDTM","takerFeeRate":"0.0006","makerFeeRate":"0.0002"}}`,
		"/api/v1/funding-history":      `{"code":"200000","data":{"hasMore":true,"dataList":[{"id":1472387374042586,"symbol":"XBTUSDTM","timePoint":1731470400000,"fundingRate":0.000641,"markPrice":87139.92,"positionQty":1,"positionCost":87.13992,"funding":-0.05585669,"settleCurrency":"USDT","context":"{\"marginMode\": \"ISOLATED\", \"positionSide\": \"BOTH\"}","marginMode":"ISOLATED"}]}}`,
	})
	client := newExtClient(t, server)
	ctx := context.Background()
	start := time.UnixMilli(1_728_663_338_000)
	forward := false

	ledger, err := client.Ledgers(ctx, LedgerRequest{
		Currency: "USDT", Type: LedgerTypeTransferIn, Offset: 100, Forward: &forward, MaxCount: 50, StartAt: &start,
	})
	if err != nil || ledger.HasMore || len(ledger.Entries) != 1 || ledger.Entries[0].AccountEquity != "14.02924938" ||
		ledger.Entries[0].Offset != 51360793 {
		t.Fatalf("Ledgers() = %+v, %v", ledger, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/transaction-history",
		"currency=USDT&forward=false&maxCount=50&offset=100&startAt=1728663338000&type=Transferin", "")

	subs, err := client.SubAccountOverviews(ctx, SubAccountOverviewRequest{Currency: "USDT"})
	if err != nil || subs.Summary.AccountEquityTotal != "103.899081508" || len(subs.Accounts) != 1 ||
		subs.Accounts[0].AccountName != "main" || subs.Accounts[0].AvailableBalance != "41.5171731572" {
		t.Fatalf("SubAccountOverviews() = %+v, %v", subs, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/account-overview-all", "currency=USDT", "")

	fee, err := client.ActualFee(ctx, "XBTUSDTM")
	if err != nil || fee.TakerFeeRate != "0.0006" || fee.MakerFeeRate != "0.0002" {
		t.Fatalf("ActualFee() = %+v, %v", fee, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/trade-fees", "symbol=XBTUSDTM", "")

	funding, err := client.FundingHistory(ctx, FundingHistoryRequest{Symbol: "XBTUSDTM", MaxCount: 10})
	if err != nil || !funding.HasMore || len(funding.Entries) != 1 || funding.Entries[0].Funding != "-0.05585669" ||
		funding.Entries[0].MarginMode != MarginModeIsolated || funding.Entries[0].ID != 1472387374042586 {
		t.Fatalf("FundingHistory() = %+v, %v", funding, err)
	}
	expectRequest(t, last(), http.MethodGet, "/api/v1/funding-history", "maxCount=10&symbol=XBTUSDTM", "")
}

func TestAccountExtValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{"ledger currency", (LedgerRequest{Currency: "usdt"}).validate()},
		{"ledger type", (LedgerRequest{Type: "Other"}).validate()},
		{"ledger max count", (LedgerRequest{MaxCount: 101}).validate()},
		{"funding symbol", (FundingHistoryRequest{Symbol: "bad"}).validate()},
		{"sub account currency", func() error {
			_, err := (&Client{}).SubAccountOverviews(context.Background(), SubAccountOverviewRequest{Currency: "usdt"})
			return err
		}()},
		{"funding offset", (FundingHistoryRequest{Symbol: "XBTUSDTM", Offset: -1}).validate()},
	}
	for _, test := range tests {
		if !errors.Is(test.err, trade.ErrValidation) {
			t.Errorf("%s error = %v, want validation", test.name, test.err)
		}
	}
}

func TestPositionModeDecodesNumberAndString(t *testing.T) {
	t.Parallel()

	for _, input := range []string{`1`, `"1"`} {
		var mode PositionMode
		if err := mode.UnmarshalJSON([]byte(input)); err != nil || mode != PositionModeHedge {
			t.Errorf("UnmarshalJSON(%s) = %q, %v", input, mode, err)
		}
	}
	var mode PositionMode
	if err := mode.UnmarshalJSON([]byte(`2`)); err == nil {
		t.Error("UnmarshalJSON(2) error = nil, want error")
	}
}

func TestBatchPlaceOrdersRejectsMissingCredentials(t *testing.T) {
	t.Parallel()

	server, _ := newExtServer(t, false, map[string]string{})
	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return time.UnixMilli(1_700_000_000_000) },
	)
	// Trade 권한이 없는 자격증명으로는 서명 전에 거절되어야 한다.
	client.credentials.Permissions = client.credentials.Permissions[:1]
	_, err := client.SwitchPositionMode(context.Background(), PositionModeHedge)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("SwitchPositionMode() without trade permission error = %v, want authorization", err)
	}
}
