package coinone

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/conformance"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/transport"
	"github.com/proven-trade/cex-sdk/unified"
)

func TestUnifiedSpotReadConformance(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/public/v2/ticker_new/KRW/BTC":
			// Coinone의 실응답은 이 endpoint에서 통화 코드를 소문자로 반환할 수 있다.
			_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","tickers":[{"quote_currency":"krw","target_currency":"btc","last":"64000.10"}]}`)
		case "/v2.1/account/balance/all":
			verifySignedRequest(t, request, []byte("secret-key"), "123e4567-e89b-42d3-a456-426614174000")
			_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","balances":[{"currency":"KRW","available":"1000000.00","limit":"2.00"}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newTestClient(
		t, server.URL, sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"},
	)
	adapter, err := NewUnifiedSpot(native)
	if err != nil {
		t.Fatalf("NewUnifiedSpot() error = %v", err)
	}
	conformance.RunSpotReadSuite(t, conformance.SpotReadScenario{
		Client: adapter, Exchange: model.ExchangeCoinone,
		Market: unified.Market{Base: "BTC", Quote: "KRW"},
		Price:  "64000.10", NativeMarket: "KRW-BTC",
		BalanceAsset: "KRW", BalanceAvailable: "1000000.00",
		Options: []trade.RequestOption{trade.WithEgressRoute("route-b")},
	})
	if routes := sender.snapshot(); len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedSpotOrderConformance(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2.1/order" || request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		verifySignedRequest(t, request, []byte("secret-key"), "123e4567-e89b-42d3-a456-426614174000")
		body := readRequestBody(t, request)
		want := `{"access_token":"access-key","nonce":"123e4567-e89b-42d3-a456-426614174000","side":"BUY","quote_currency":"KRW","target_currency":"BTC","type":"MARKET","amount":"100000","user_order_id":"client-1"}`
		if string(body) != want {
			http.Error(writer, `{"result":"error","error_code":"101","error_msg":"bad common mapping"}`, http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","order_id":"order-1"}`)
	}))
	defer server.Close()

	native, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"},
	)
	adapter, _ := NewUnifiedSpot(native)
	request := unified.PlaceOrderRequest{
		Market: unified.Market{Base: "BTC", Quote: "KRW"}, Side: unified.SideBuy,
		Type: unified.OrderTypeMarket, QuoteAmount: "100000", ClientOrderID: "client-1",
	}
	conformance.RunSpotOrderSuite(t, conformance.SpotOrderScenario{
		Client: adapter, Exchange: model.ExchangeCoinone, Request: request,
		OrderID: "order-1", ClientOrderID: "client-1", NativeMarket: "KRW-BTC",
	})
}

func TestUnifiedCoinoneOrderAndMarketMappings(t *testing.T) {
	t.Parallel()

	market := unified.Market{Base: "BTC", Quote: "KRW"}
	native := OrderDetail{
		OrderID: "order-1", UserOrderID: "client-1", Type: OrderTypeMarket,
		QuoteCurrency: "KRW", TargetCurrency: "BTC", Status: "PARTIALLY_CANCELED",
		Side: SideBuy, OriginalAmount: "100000", ExecutedQuantity: "0.001",
	}
	got := fromCoinoneOrderDetail(native, market)
	if got.ID != "order-1" || got.NativeMarket != "KRW-BTC" || got.Side != unified.SideBuy ||
		got.Type != unified.OrderTypeMarket || got.Status != unified.OrderStatusCanceled ||
		got.Quantity != "100000" || got.ExecutedQuantity != "0.001" {
		t.Fatalf("fromCoinoneOrderDetail() = %+v", got)
	}
	if got := coinoneMarketStatus(Market{TradeState: 2}); got != "sell_only" {
		t.Fatalf("coinoneMarketStatus() = %q", got)
	}
	if got := coinoneMarketStatus(Market{MaintenanceState: 1, TradeState: 1}); got != "maintenance" {
		t.Fatalf("coinoneMarketStatus() = %q", got)
	}
}

func TestUnifiedCoinoneDepthMappings(t *testing.T) {
	t.Parallel()

	orderBookCases := map[int]int{0: 16, 1: 5, 6: 10, 11: 15, 16: 16, 30: 16}
	for input, want := range orderBookCases {
		if got := coinoneOrderBookSize(input); got != want {
			t.Fatalf("coinoneOrderBookSize(%d) = %d, want %d", input, got, want)
		}
	}
	recentTradeCases := map[int]int{0: 100, 1: 10, 11: 50, 51: 100, 100: 100}
	for input, want := range recentTradeCases {
		if got := coinoneRecentTradesSize(input); got != want {
			t.Fatalf("coinoneRecentTradesSize(%d) = %d, want %d", input, got, want)
		}
	}
}

func TestUnifiedSpotOpenOrdersMapsAllMarketsOnSelectedRoute(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2.1/order/active_orders" {
			http.NotFound(writer, request)
			return
		}
		verifySignedRequest(t, request, []byte("secret-key"), "123e4567-e89b-42d3-a456-426614174000")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","active_orders":[{"order_id":"order-1","user_order_id":"client-1","type":"STOP_LIMIT","side":"SELL","quote_currency":"KRW","target_currency":"ETH","price":"5000000","original_qty":"2","executed_qty":"0.5","remain_qty":"1.5"}]}`)
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newTestClient(
		t, server.URL, sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"},
	)
	adapter, _ := NewUnifiedSpot(native)
	orders, err := adapter.OpenOrders(
		context.Background(), unified.OpenOrdersRequest{AllMarkets: true},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("OpenOrders() error = %v", err)
	}
	if len(orders) != 1 || orders[0].Market != (unified.Market{Base: "ETH", Quote: "KRW"}) ||
		orders[0].NativeMarket != "KRW-ETH" || orders[0].Type != unified.OrderTypeLimit ||
		orders[0].Status != unified.OrderStatusPartiallyFilled {
		t.Fatalf("OpenOrders() = %+v", orders)
	}
	if routes := sender.snapshot(); len(routes) != 1 || routes[0] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedSpotRejectsUnsupportedLimitTimeInForce(t *testing.T) {
	t.Parallel()

	adapter := &UnifiedSpot{}
	_, err := adapter.PlaceOrder(context.Background(), unified.PlaceOrderRequest{
		Market: unified.Market{Base: "BTC", Quote: "KRW"}, Side: unified.SideBuy,
		Type: unified.OrderTypeLimit, TimeInForce: unified.TimeInForceIOC,
		Quantity: "0.01", Price: "64000000",
	})
	if !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("PlaceOrder() error = %v, want ErrValidation", err)
	}
}

func TestNewUnifiedSpotRejectsNilClient(t *testing.T) {
	t.Parallel()

	if _, err := NewUnifiedSpot(nil); err == nil {
		t.Fatal("NewUnifiedSpot(nil) error = nil")
	}
}
