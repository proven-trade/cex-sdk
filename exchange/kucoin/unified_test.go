package kucoin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/conformance"
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
		case "/api/v1/market/orderbook/level1":
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"sequence":"1","price":"64000.10","size":"0.01","time":1700000000000000000}}`)
		case "/api/v1/accounts":
			if request.URL.Query().Get("type") != "trade" ||
				!verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
				http.Error(writer, `{"code":"400005","msg":"Invalid request"}`, http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(writer, `{"code":"200000","data":[{"id":"account-1","currency":"USDT","type":"trade","balance":"11.50","available":"10.25","holds":"1.25"}]}`)
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
		Client: adapter, Exchange: model.ExchangeKuCoin,
		Market: unified.Market{Base: "BTC", Quote: "USDT"},
		Price:  "64000.10", NativeMarket: "BTC-USDT",
		BalanceAsset: "USDT", BalanceAvailable: "10.25",
		Options: []trade.RequestOption{trade.WithEgressRoute("route-b")},
	})
	balances, err := adapter.Balances(context.Background(), trade.WithEgressRoute("route-b"))
	if err != nil || len(balances) != 1 || balances[0].Locked != "1.25" {
		t.Fatalf("Balances() = %+v, error = %v", balances, err)
	}
	if routes := sender.snapshot(); len(routes) != 3 || routes[0] != "route-b" ||
		routes[1] != "route-b" || routes[2] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedKuCoinMarketDataMapping(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v2/symbols":
			_, _ = io.WriteString(writer, `{"code":"200000","data":[{"symbol":"BTC-USDT","baseCurrency":"BTC","quoteCurrency":"USDT","enableTrading":true}]}`)
		case "/api/v1/market/orderbook/level2_20":
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"sequence":"2","time":1700000000000,"bids":[["64000","1"],["63999","2"]],"asks":[["64001","3"],["64002","4"]]}}`)
		case "/api/v1/market/histories":
			_, _ = io.WriteString(writer, `{"code":"200000","data":[{"sequence":"3","price":"64000","size":"0.01","side":"buy","time":1700000000000000000},{"sequence":"2","price":"63999","size":"0.02","side":"sell","time":1699999999000000000}]}`)
		case "/api/v1/market/candles":
			_, _ = io.WriteString(writer, `{"code":"200000","data":[["1700000000","63000","64000","65000","62000","10","640000"],["1699999940","62000","63000","64000","61000","9","560000"]]}`)
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
	if err != nil || len(book.Bids) != 1 || len(book.Asks) != 1 ||
		book.Bids[0].Quantity != "1" || book.Timestamp != 1_700_000_000_000 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := adapter.RecentTrades(context.Background(), unified.RecentTradesRequest{
		Market: market, Limit: 1,
	}, trade.WithEgressRoute("route-b"))
	if err != nil || len(trades) != 1 || trades[0].ID != "3" ||
		trades[0].Side != unified.SideBuy || trades[0].Timestamp != 1_700_000_000_000 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	candles, err := adapter.Candles(context.Background(), unified.CandlesRequest{
		Market: market, Interval: unified.Candle1Minute, Limit: 1,
	}, trade.WithEgressRoute("route-b"))
	if err != nil || len(candles) != 1 || candles[0].StartTime != 1_700_000_000_000 ||
		candles[0].Close != "64000" || candles[0].Volume != "10" {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
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

func TestUnifiedSpotOrderConformance(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/hf/orders" || request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		if !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":"400005","msg":"Invalid signature"}`, http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"clientOid":"client-1","symbol":"BTC-USDT","type":"market","side":"buy","funds":"100"}` {
			http.Error(writer, `{"code":"400100","msg":"bad common mapping"}`, http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"code":"200000","data":{"orderId":"order-1","clientOid":"client-1"}}`)
	}))
	defer server.Close()

	native, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return fixedNow },
	)
	adapter, _ := NewUnifiedSpot(native)
	request := unified.PlaceOrderRequest{
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, Side: unified.SideBuy,
		Type: unified.OrderTypeMarket, QuoteAmount: "100", ClientOrderID: "client-1",
	}
	conformance.RunSpotOrderSuite(t, conformance.SpotOrderScenario{
		Client: adapter, Exchange: model.ExchangeKuCoin, Request: request,
		OrderID: "order-1", ClientOrderID: "client-1", NativeMarket: "BTC-USDT",
		Status: unified.OrderStatusAcknowledged,
	})
}

func TestUnifiedKuCoinOrderByClientIDAndCancel(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/hf/orders/client-order/client-1" ||
			request.URL.Query().Get("symbol") != "BTC-USDT" ||
			!verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":"400005","msg":"Invalid request"}`, http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodGet {
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"id":"order-1","clientOid":"client-1","symbol":"BTC-USDT","type":"limit","side":"buy","price":"64000","size":"0.01","dealSize":"0.005","isActive":true,"cancelExist":false}}`)
			return
		}
		if request.Method == http.MethodDelete {
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"clientOid":"client-1"}}`)
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
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, ClientOrderID: "client-1",
	}
	order, err := adapter.Order(context.Background(), request)
	if err != nil || order.ID != "order-1" || order.Status != unified.OrderStatusPartiallyFilled ||
		order.Quantity != "0.01" || order.ExecutedQuantity != "0.005" {
		t.Fatalf("Order() = %+v, error = %v", order, err)
	}
	canceled, err := adapter.CancelOrder(context.Background(), request)
	if err != nil || canceled.ClientOrderID != "client-1" ||
		canceled.Status != unified.OrderStatusCancelPending || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
}

func TestUnifiedKuCoinOpenOrdersMapsAllMarketsAndPages(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":"400005","msg":"Invalid signature"}`, http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/hf/orders/active/symbols":
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"symbols":["BTC-USDT","ETH-USDT"]}}`)
		case "/api/v1/hf/orders/active/page":
			symbol := request.URL.Query().Get("symbol")
			page := request.URL.Query().Get("pageNum")
			if request.URL.Query().Get("pageSize") != "50" {
				http.Error(writer, `{"code":"400100","msg":"bad page size"}`, http.StatusBadRequest)
				return
			}
			switch symbol + ":" + page {
			case "BTC-USDT:1":
				_, _ = io.WriteString(writer, `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":2,"totalPage":2,"items":[{"id":"order-1","clientOid":"client-1","symbol":"BTC-USDT","type":"limit","side":"buy","price":"64000","size":"0.01","dealSize":"0","isActive":true}]}}`)
			case "BTC-USDT:2":
				_, _ = io.WriteString(writer, `{"code":"200000","data":{"currentPage":2,"pageSize":50,"totalNum":2,"totalPage":2,"items":[{"id":"order-2","clientOid":"client-2","symbol":"BTC-USDT","type":"limit","side":"sell","price":"65000","size":"0.02","dealSize":"0.01","isActive":true}]}}`)
			case "ETH-USDT:1":
				_, _ = io.WriteString(writer, `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":0,"totalPage":1,"items":[]}}`)
			default:
				http.Error(writer, `{"code":"400100","msg":"unexpected page"}`, http.StatusBadRequest)
			}
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

func TestUnifiedKuCoinIdentityAndStatusHelpers(t *testing.T) {
	t.Parallel()

	clientOrderID, err := newKuCoinClientOrderID()
	if err != nil {
		t.Fatalf("newKuCoinClientOrderID() error = %v", err)
	}
	if !regexp.MustCompile(`^proven-[0-9a-f]{28}$`).MatchString(clientOrderID) {
		t.Fatalf("newKuCoinClientOrderID() = %q", clientOrderID)
	}
	status, err := toUnifiedKuCoinOrderStatus(Order{
		Size: "0.010", DealSize: "0.01", Active: false,
	})
	if err != nil || status != unified.OrderStatusFilled {
		t.Fatalf("filled status = %q, error = %v", status, err)
	}
	status, err = toUnifiedKuCoinOrderStatus(Order{
		Size: "0.01", DealSize: "0.005", CancelExists: true,
	})
	if err != nil || status != unified.OrderStatusCanceled {
		t.Fatalf("canceled status = %q, error = %v", status, err)
	}
	if _, err := kucoinCandleMilliseconds("invalid"); err == nil {
		t.Fatal("kucoinCandleMilliseconds(invalid) error = nil")
	}
	if _, err := fromKuCoinSymbol("BTC_USDT"); err == nil {
		t.Fatal("fromKuCoinSymbol(BTC_USDT) error = nil")
	}
	if err := validateOrderIdentity("order-1", "client-1"); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("validateOrderIdentity() error = %v", err)
	}
}

func TestNewUnifiedSpotRejectsNilClient(t *testing.T) {
	t.Parallel()

	if _, err := NewUnifiedSpot(nil); err == nil {
		t.Fatal("NewUnifiedSpot(nil) error = nil")
	}
}
