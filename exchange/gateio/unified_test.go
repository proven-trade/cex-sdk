package gateio

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/conformance"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/transport"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

func TestUnifiedSpotReadConformance(t *testing.T) {
	t.Parallel()

	fixedNow := time.Unix(1_700_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v4/spot/tickers":
			if request.URL.Query().Get("currency_pair") != "BTC_USDT" {
				http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"bad market"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[{"currency_pair":"BTC_USDT","last":"64000.10"}]`)
		case "/api/v4/spot/accounts":
			if !verifySignedRequest(t, request, []byte("test-secret")) {
				http.Error(writer, `{"label":"INVALID_SIGNATURE","message":"bad signature"}`, http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(writer, `[{"currency":"USDT","available":"10.25","locked":"1.25"}]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newTestClient(
		t, server.URL, sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"}, func() time.Time { return fixedNow },
	)
	adapter, err := NewUnifiedSpot(native)
	if err != nil {
		t.Fatalf("NewUnifiedSpot() error = %v", err)
	}
	conformance.RunSpotReadSuite(t, conformance.SpotReadScenario{
		Client: adapter, Exchange: model.ExchangeGateIO,
		Market: unified.Market{Base: "BTC", Quote: "USDT"},
		Price:  "64000.10", NativeMarket: "BTC_USDT",
		BalanceAsset: "USDT", BalanceAvailable: "10.25",
		Options: []trade.RequestOption{trade.WithEgressRoute("route-b")},
	})
	if routes := sender.snapshot(); len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedGateIOMarketDataMapping(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v4/spot/currency_pairs":
			_, _ = io.WriteString(writer, `[{"id":"BTC_USDT","base":"BTC","quote":"USDT","trade_status":"tradable"}]`)
		case "/api/v4/spot/order_book":
			if request.URL.Query().Get("currency_pair") != "BTC_USDT" ||
				request.URL.Query().Get("limit") != "1" || request.URL.Query().Get("with_id") != "true" {
				http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"bad book mapping"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"id":10,"current":1700000000000,"asks":[["64001","3"]],"bids":[["64000","2"]]}`)
		case "/api/v4/spot/trades":
			if request.URL.Query().Get("limit") != "1" {
				http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"bad trade mapping"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[{"id":"trade-1","create_time":"1700000000","create_time_ms":"1700000000000.9","currency_pair":"BTC_USDT","side":"sell","amount":"0.01","price":"64000"}]`)
		case "/api/v4/spot/candlesticks":
			if request.URL.Query().Get("interval") == "1m" && request.URL.Query().Get("limit") == "3" {
				_, _ = io.WriteString(writer, `[["1700000040","192000","64000","64500","63500","63800","3","true"],["1699999980","126000","63800","64000","62800","63000","2","true"],["1699999920","63000","63000","63500","62000","62500","1","true"]]`)
				return
			}
			if request.URL.Query().Get("interval") != "5m" || request.URL.Query().Get("limit") != "1" {
				http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"bad candle mapping"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[["1700000000","640000","64000","65000","62000","63000","10","true"]]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newTestClient(
		t, server.URL, sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"}, nil,
	)
	adapter, _ := NewUnifiedSpot(native)
	market := unified.Market{Base: "BTC", Quote: "USDT"}
	markets, err := adapter.Markets(context.Background(), trade.WithEgressRoute("route-b"))
	if err != nil || len(markets) != 1 || markets[0].Market != market ||
		markets[0].Status != "trading" || len(markets[0].Raw) == 0 {
		t.Fatalf("Markets() = %+v, error = %v", markets, err)
	}
	book, err := adapter.OrderBook(context.Background(), unified.OrderBookRequest{
		Market: market, Limit: 1,
	}, trade.WithEgressRoute("route-b"))
	if err != nil || len(book.Bids) != 1 || book.Bids[0].Quantity != "2" ||
		book.Timestamp != 1_700_000_000_000 || len(book.Raw) == 0 {
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
	if routes := sender.snapshot(); len(routes) != 5 {
		t.Fatalf("sender routes = %v", routes)
	} else {
		for _, route := range routes {
			if route != "route-b" {
				t.Fatalf("sender routes = %v", routes)
			}
		}
	}
}

func TestUnifiedSpotOrderConformance(t *testing.T) {
	t.Parallel()

	fixedNow := time.Unix(1_700_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v4/spot/orders" || request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		if !verifySignedRequest(t, request, []byte("test-secret")) {
			http.Error(writer, `{"label":"INVALID_SIGNATURE","message":"bad signature"}`, http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"text":"t-client-1","currency_pair":"BTC_USDT","type":"market","account":"spot","side":"buy","amount":"100","time_in_force":"ioc"}` {
			http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"bad common mapping"}`, http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"id":"order-1","text":"t-client-1","status":"open","currency_pair":"BTC_USDT","type":"market","account":"spot","side":"buy","amount":"100","filled_amount":"0"}`)
	}))
	defer server.Close()

	native, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return fixedNow },
	)
	adapter, _ := NewUnifiedSpot(native)
	request := unified.PlaceOrderRequest{
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, Side: unified.SideBuy,
		Type: unified.OrderTypeMarket, QuoteAmount: "100", ClientOrderID: "t-client-1",
	}
	conformance.RunSpotOrderSuite(t, conformance.SpotOrderScenario{
		Client: adapter, Exchange: model.ExchangeGateIO, Request: request,
		OrderID: "order-1", ClientOrderID: "t-client-1", NativeMarket: "BTC_USDT",
	})
}

func TestUnifiedGateIOOrderByClientIDAndCancel(t *testing.T) {
	t.Parallel()

	fixedNow := time.Unix(1_700_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v4/spot/orders/t-client-1" ||
			request.URL.Query().Get("currency_pair") != "BTC_USDT" ||
			!verifySignedRequest(t, request, []byte("test-secret")) {
			http.Error(writer, `{"label":"INVALID_SIGNATURE","message":"bad request"}`, http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodGet {
			_, _ = io.WriteString(writer, `{"id":"order-1","text":"t-client-1","status":"open","currency_pair":"BTC_USDT","type":"limit","account":"spot","side":"buy","amount":"0.01","price":"64000","left":"0.005","filled_amount":"0.005"}`)
			return
		}
		if request.Method == http.MethodDelete {
			_, _ = io.WriteString(writer, `{"id":"order-1","text":"t-client-1","status":"cancelled","currency_pair":"BTC_USDT","type":"limit","account":"spot","side":"buy","amount":"0.01","price":"64000","left":"0.005","filled_amount":"0.005"}`)
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	native, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return fixedNow },
	)
	adapter, _ := NewUnifiedSpot(native)
	request := unified.OrderRequest{
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, ClientOrderID: "t-client-1",
	}
	order, err := adapter.Order(context.Background(), request)
	if err != nil || order.ID != "order-1" || order.Status != unified.OrderStatusPartiallyFilled ||
		order.Quantity != "0.01" || order.ExecutedQuantity != "0.005" {
		t.Fatalf("Order() = %+v, error = %v", order, err)
	}
	canceled, err := adapter.CancelOrder(context.Background(), request)
	if err != nil || canceled.ClientOrderID != "t-client-1" ||
		canceled.Status != unified.OrderStatusCanceled || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
}

func TestUnifiedGateIOOpenOrdersMapsAllMarketsAndPages(t *testing.T) {
	t.Parallel()

	fixedNow := time.Unix(1_700_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v4/spot/open_orders" ||
			request.URL.Query().Get("limit") != "1000" ||
			!verifySignedRequest(t, request, []byte("test-secret")) {
			http.Error(writer, `{"label":"INVALID_SIGNATURE","message":"bad request"}`, http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(writer, `[{"currency_pair":"BTC_USDT","total":2,"orders":[{"id":"order-1","text":"t-client-1","status":"open","currency_pair":"BTC_USDT","type":"limit","side":"buy","amount":"0.01","price":"64000","filled_amount":"0"}]},{"currency_pair":"ETH_USDT","total":0,"orders":[]}]`)
		case "2":
			_, _ = io.WriteString(writer, `[{"currency_pair":"BTC_USDT","total":2,"orders":[{"id":"order-2","text":"t-client-2","status":"open","currency_pair":"BTC_USDT","type":"limit","side":"sell","amount":"0.02","price":"65000","filled_amount":"0.01"}]}]`)
		default:
			http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"unexpected page"}`, http.StatusBadRequest)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newTestClient(
		t, server.URL, sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"}, func() time.Time { return fixedNow },
	)
	adapter, _ := NewUnifiedSpot(native)
	orders, err := adapter.OpenOrders(
		context.Background(), unified.OpenOrdersRequest{AllMarkets: true},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("OpenOrders() error = %v", err)
	}
	if len(orders) != 2 || orders[0].Status != unified.OrderStatusNew ||
		orders[1].Status != unified.OrderStatusPartiallyFilled ||
		orders[1].Market != (unified.Market{Base: "BTC", Quote: "USDT"}) {
		t.Fatalf("OpenOrders() = %+v", orders)
	}
	if routes := sender.snapshot(); len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedGateIOHelpers(t *testing.T) {
	t.Parallel()

	clientOrderID, err := newGateIOClientOrderID()
	if err != nil {
		t.Fatalf("newGateIOClientOrderID() error = %v", err)
	}
	if !regexp.MustCompile(`^t-proven-[0-9a-f]{20}$`).MatchString(clientOrderID) {
		t.Fatalf("newGateIOClientOrderID() = %q", clientOrderID)
	}
	if timestamp, err := gateIOTradeMilliseconds("", "1700000000.125"); err != nil || timestamp != 1_700_000_000_125 {
		t.Fatalf("gateIOTradeMilliseconds() = %d, error = %v", timestamp, err)
	}
	if _, err := gateIOTradeMilliseconds("invalid", ""); err == nil {
		t.Fatal("invalid timestamp error = nil")
	}
	if _, err := fromGateIOSymbol("BTC-USDT"); err == nil {
		t.Fatal("fromGateIOSymbol(BTC-USDT) error = nil")
	}
	if status, err := toUnifiedGateIOOrderStatus(Order{Status: "closed"}); err != nil || status != unified.OrderStatusFilled {
		t.Fatalf("closed status = %q, error = %v", status, err)
	}
}

func TestNewUnifiedSpotRejectsNilClient(t *testing.T) {
	t.Parallel()

	if _, err := NewUnifiedSpot(nil); err == nil {
		t.Fatal("NewUnifiedSpot(nil) error = nil")
	}
}
