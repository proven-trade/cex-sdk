package mexc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/conformance"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/transport"
	"github.com/proven-trade/cex-sdk/unified"
)

func TestUnifiedSpotReadConformance(t *testing.T) {
	t.Parallel()
	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v3/ticker/price":
			if request.URL.Query().Get("symbol") != "BTCUSDT" {
				http.Error(writer, `{"code":33333,"msg":"bad market"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"symbol":"BTCUSDT","price":"64000.10"}`)
		case "/api/v3/account":
			if !verifyMEXCSignedRequest(t, request, []byte("test-secret"), fixedNow) {
				http.Error(writer, `{"code":700002,"msg":"bad signature"}`, http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(writer, `{"accountType":"SPOT","balances":[{"asset":"USDT","free":"10.25","locked":"1.25"}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newPrivateTestClient(
		t, server.URL, sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	adapter, err := NewUnifiedSpot(native)
	if err != nil {
		t.Fatalf("NewUnifiedSpot() error = %v", err)
	}
	conformance.RunSpotReadSuite(t, conformance.SpotReadScenario{
		Client: adapter, Exchange: model.ExchangeMEXC,
		Market: unified.Market{Base: "BTC", Quote: "USDT"},
		Price:  "64000.10", NativeMarket: "BTCUSDT",
		BalanceAsset: "USDT", BalanceAvailable: "10.25",
		Options: []trade.RequestOption{trade.WithEgressRoute("route-b")},
	})
	if routes := sender.snapshot(); len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedMEXCMarketDataMapping(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v3/exchangeInfo":
			_, _ = io.WriteString(writer, `{"symbols":[{"symbol":"BTCUSDT","status":"1","baseAsset":"BTC","quoteAsset":"USDT","isSpotTradingAllowed":true,"tradeSideType":1},{"symbol":"ETHUSDT","status":"1","baseAsset":"ETH","quoteAsset":"USDT","isSpotTradingAllowed":true,"tradeSideType":2}]}`)
		case "/api/v3/depth":
			if request.URL.Query().Get("symbol") != "BTCUSDT" || request.URL.Query().Get("limit") != "1" {
				http.Error(writer, `{"code":33333,"msg":"bad book mapping"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"lastUpdateId":10,"bids":[["64000","2"]],"asks":[["64001","3"]]}`)
		case "/api/v3/trades":
			if request.URL.Query().Get("limit") != "1" {
				http.Error(writer, `{"code":33333,"msg":"bad trade mapping"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[{"id":"trade-1","price":"64000","qty":"0.01","time":1700000000000,"isBuyerMaker":true}]`)
		case "/api/v3/klines":
			query := request.URL.Query()
			if query.Get("interval") == "1m" && query.Get("limit") == "3" {
				_, _ = io.WriteString(writer, `[[1699999920000,"62500","63500","62000","63000","1",1699999979999,"63000"],[1699999980000,"63000","64000","62800","63800","2",1700000039999,"126000"],[1700000040000,"63800","64500","63500","64000","3",1700000099999,"192000"]]`)
				return
			}
			if query.Get("interval") != "5m" || query.Get("limit") != "1" {
				http.Error(writer, `{"code":33333,"msg":"bad candle mapping"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[[1700000000000,"63000","65000","62000","64000","10",1700000299999,"640000"]]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	adapter, _ := NewUnifiedSpot(mustPublicMEXCClient(t, server.URL, &directSender{}))
	market := unified.Market{Base: "BTC", Quote: "USDT"}
	markets, err := adapter.Markets(context.Background(), trade.WithEgressRoute("route-b"))
	if err != nil || len(markets) != 2 || markets[0].Market != market ||
		markets[0].Status != "trading" || markets[1].Status != "buy_only" || len(markets[0].Raw) == 0 {
		t.Fatalf("Markets() = %+v, error = %v", markets, err)
	}
	book, err := adapter.OrderBook(context.Background(), unified.OrderBookRequest{
		Market: market, Limit: 1,
	}, trade.WithEgressRoute("route-b"))
	if err != nil || len(book.Bids) != 1 || book.Bids[0].Quantity != "2" ||
		book.NativeMarket != "BTCUSDT" || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := adapter.RecentTrades(context.Background(), unified.RecentTradesRequest{
		Market: market, Limit: 1,
	}, trade.WithEgressRoute("route-b"))
	if err != nil || len(trades) != 1 || trades[0].ID != "trade-1" ||
		trades[0].Side != unified.SideSell || trades[0].Timestamp != 1_700_000_000_000 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	candles, err := adapter.Candles(context.Background(), unified.CandlesRequest{
		Market: market, Interval: unified.Candle5Minutes, Limit: 1,
	}, trade.WithEgressRoute("route-b"))
	if err != nil || len(candles) != 1 || candles[0].StartTime != 1_700_000_000_000 ||
		candles[0].Close != "64000" || candles[0].Volume != "10" {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	aggregated, err := adapter.Candles(context.Background(), unified.CandlesRequest{
		Market: market, Interval: unified.Candle3Minutes, Limit: 1,
	}, trade.WithEgressRoute("route-b"))
	if err != nil || len(aggregated) != 1 || aggregated[0].StartTime != 1_699_999_920_000 ||
		aggregated[0].Open != "62500" || aggregated[0].High != "64500" ||
		aggregated[0].Low != "62000" || aggregated[0].Close != "64000" ||
		aggregated[0].Volume != "6" {
		t.Fatalf("three-minute Candles() = %+v, error = %v", aggregated, err)
	}
}

func TestUnifiedSpotOrderConformance(t *testing.T) {
	t.Parallel()
	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v3/order" || request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		if !verifyMEXCSignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":700002,"msg":"bad signature"}`, http.StatusUnauthorized)
			return
		}
		query := request.URL.Query()
		if query.Get("symbol") != "BTCUSDT" || query.Get("side") != "BUY" ||
			query.Get("type") != "MARKET" || query.Get("quoteOrderQty") != "100" ||
			query.Get("newClientOrderId") != "strategy-1" {
			http.Error(writer, `{"code":33333,"msg":"bad common mapping"}`, http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"symbol":"BTCUSDT","orderId":"order-1","orderListId":-1,"type":"MARKET","side":"BUY","transactTime":1700000000000}`)
	}))
	defer server.Close()

	native, _ := newPrivateTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	adapter, _ := NewUnifiedSpot(native)
	request := unified.PlaceOrderRequest{
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, Side: unified.SideBuy,
		Type: unified.OrderTypeMarket, QuoteAmount: "100", ClientOrderID: "strategy-1",
	}
	conformance.RunSpotOrderSuite(t, conformance.SpotOrderScenario{
		Client: adapter, Exchange: model.ExchangeMEXC, Request: request,
		OrderID: "order-1", ClientOrderID: "strategy-1", NativeMarket: "BTCUSDT",
	})
}

func TestUnifiedMEXCOrderAndCancelMapping(t *testing.T) {
	t.Parallel()
	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v3/order" ||
			!verifyMEXCSignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":700002,"msg":"bad request"}`, http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodGet {
			_, _ = io.WriteString(writer, orderJSON("PARTIALLY_FILLED"))
			return
		}
		if request.Method == http.MethodDelete {
			_, _ = io.WriteString(writer, orderJSON("CANCELED"))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	native, _ := newPrivateTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	adapter, _ := NewUnifiedSpot(native)
	request := unified.OrderRequest{
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, ClientOrderID: "strategy-1",
	}
	order, err := adapter.Order(context.Background(), request)
	if err != nil || order.ID != "order-1" || order.Status != unified.OrderStatusPartiallyFilled ||
		order.Quantity != "0.1" || order.ExecutedQuantity != "0" || len(order.Raw) == 0 {
		t.Fatalf("Order() = %+v, error = %v", order, err)
	}
	canceled, err := adapter.CancelOrder(context.Background(), request)
	if err != nil || canceled.ClientOrderID != "strategy-1" ||
		canceled.Status != unified.OrderStatusCanceled || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
}

func TestUnifiedMEXCOpenOrdersBatchesAllowedMarkets(t *testing.T) {
	t.Parallel()
	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v3/selfSymbols":
			if !verifyMEXCSignedRequest(t, request, []byte("test-secret"), fixedNow) {
				http.Error(writer, `{"code":700002,"msg":"bad signature"}`, http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(writer, `{"code":200,"data":["BTCUSDT","ETHUSDT","SOLUSDT","XRPUSDT","ADAUSDT","DOGEUSDT"],"msg":null}`)
		case "/api/v3/exchangeInfo":
			_, _ = io.WriteString(writer, `{"symbols":[{"symbol":"BTCUSDT","status":"1","baseAsset":"BTC","quoteAsset":"USDT","isSpotTradingAllowed":true,"tradeSideType":1},{"symbol":"ETHUSDT","status":"1","baseAsset":"ETH","quoteAsset":"USDT","isSpotTradingAllowed":true,"tradeSideType":1},{"symbol":"SOLUSDT","status":"1","baseAsset":"SOL","quoteAsset":"USDT","isSpotTradingAllowed":true,"tradeSideType":1},{"symbol":"XRPUSDT","status":"1","baseAsset":"XRP","quoteAsset":"USDT","isSpotTradingAllowed":true,"tradeSideType":1},{"symbol":"ADAUSDT","status":"1","baseAsset":"ADA","quoteAsset":"USDT","isSpotTradingAllowed":true,"tradeSideType":1},{"symbol":"DOGEUSDT","status":"1","baseAsset":"DOGE","quoteAsset":"USDT","isSpotTradingAllowed":true,"tradeSideType":1}]}`)
		case "/api/v3/openOrders":
			if !verifyMEXCSignedRequest(t, request, []byte("test-secret"), fixedNow) {
				http.Error(writer, `{"code":700002,"msg":"bad signature"}`, http.StatusUnauthorized)
				return
			}
			switch request.URL.Query().Get("symbol") {
			case "BTCUSDT,ETHUSDT,SOLUSDT,XRPUSDT,ADAUSDT":
				_, _ = io.WriteString(writer, `[`+unifiedMEXCOrderJSON("BTCUSDT", "order-1")+`]`)
			case "DOGEUSDT":
				_, _ = io.WriteString(writer, `[`+unifiedMEXCOrderJSON("DOGEUSDT", "order-2")+`]`)
			default:
				http.Error(writer, `{"code":33333,"msg":"bad symbol batch"}`, http.StatusBadRequest)
			}
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newPrivateTestClient(
		t, server.URL, sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	adapter, _ := NewUnifiedSpot(native)
	orders, err := adapter.OpenOrders(
		context.Background(), unified.OpenOrdersRequest{AllMarkets: true},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || len(orders) != 2 || orders[0].ID != "order-1" ||
		orders[0].Market != (unified.Market{Base: "BTC", Quote: "USDT"}) ||
		orders[1].Market != (unified.Market{Base: "DOGE", Quote: "USDT"}) {
		t.Fatalf("OpenOrders() = %+v, error = %v", orders, err)
	}
	if routes := sender.snapshot(); len(routes) != 4 {
		t.Fatalf("sender routes = %v", routes)
	} else {
		for _, route := range routes {
			if route != "route-b" {
				t.Fatalf("sender routes = %v", routes)
			}
		}
	}
}

func TestUnifiedMEXCHelpers(t *testing.T) {
	t.Parallel()
	clientOrderID, err := newMEXCClientOrderID()
	if err != nil {
		t.Fatalf("newMEXCClientOrderID() error = %v", err)
	}
	if !regexp.MustCompile(`^proven-[0-9a-f]{24}$`).MatchString(clientOrderID) {
		t.Fatalf("newMEXCClientOrderID() = %q", clientOrderID)
	}
	if got := fromMEXCMarketStatus(Symbol{
		Status: "1", SpotTradingAllowed: true, TradeSideType: "3",
	}); got != "sell_only" {
		t.Fatalf("sell-only market status = %q", got)
	}
	if status, err := fromMEXCOrderStatus(OrderStatusPartiallyCanceled); err != nil ||
		status != unified.OrderStatusCanceled {
		t.Fatalf("partially canceled status = %q, error = %v", status, err)
	}
	if status, err := fromMEXCOrderStatus("UNKNOWN_NATIVE_STATUS"); err != nil ||
		status != unified.OrderStatusUnknown {
		t.Fatalf("unknown native order status = %q, error = %v", status, err)
	}
	if _, err := marketFromMEXCSymbol(Symbol{
		Symbol: "WRONG", BaseAsset: "BTC", QuoteAsset: "USDT",
	}); err == nil {
		t.Fatal("mismatched native symbol error = nil")
	}
}

func TestNewUnifiedSpotRejectsNilClient(t *testing.T) {
	t.Parallel()
	if _, err := NewUnifiedSpot(nil); err == nil {
		t.Fatal("NewUnifiedSpot(nil) error = nil")
	}
}

func mustPublicMEXCClient(t *testing.T, baseURL string, sender *directSender) *Client {
	t.Helper()
	client, _ := newTestClient(t, baseURL, sender)
	return client
}

func unifiedMEXCOrderJSON(symbol, orderID string) string {
	return `{"symbol":"` + symbol + `","origClientOrderId":"strategy-1","orderId":"` + orderID + `","clientOrderId":"strategy-1","price":"64000","origQty":"0.1","executedQty":"0","cummulativeQuoteQty":"0","status":"NEW","timeInForce":"GTC","type":"LIMIT","side":"BUY","time":1700000000000,"updateTime":1700000000000,"isWorking":true}`
}
