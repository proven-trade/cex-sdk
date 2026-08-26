package bithumb

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
		case "/v1/ticker":
			_, _ = io.WriteString(writer, `[{"market":"KRW-BTC","trade_price":64000.10}]`)
		case "/v1/accounts":
			verifySignedRequest(t, request, []byte("secret-key"), "nonce-fixed", 1700000000123)
			_, _ = io.WriteString(writer, `[{"currency":"KRW","balance":"1000000.00","locked":"2.00","unit_currency":"KRW"}]`)
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
		Client: adapter, Exchange: model.ExchangeBithumb,
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
		if request.URL.Path != "/v2/orders" || request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		verifySignedRequest(t, request, []byte("secret-key"), "nonce-fixed", 1700000000123)
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"market":"KRW-BTC","side":"bid","price":"100000","order_type":"price","client_order_id":"client-1"}` {
			http.Error(writer, `{"error":{"name":"validation_error","message":"bad common mapping"}}`, http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, `{"order_id":"order-1","market":"KRW-BTC","side":"bid","order_type":"price","client_order_id":"client-1"}`)
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
		Client: adapter, Exchange: model.ExchangeBithumb, Request: request,
		OrderID: "order-1", ClientOrderID: "client-1", NativeMarket: "KRW-BTC",
	})
}

func TestUnifiedBithumbOrderMapping(t *testing.T) {
	t.Parallel()

	market := unified.Market{Base: "BTC", Quote: "KRW"}
	native := OrderDetail{
		UUID: "order-1", ClientOrderID: "client-1", Market: "KRW-BTC",
		Side: SideBid, OrderType: OrderTypeLimit, State: OrderStateWait,
		Volume: "0.01", ExecutedVolume: "0.004",
	}
	got := fromBithumbOrderDetail(native, market)
	if got.ID != "order-1" || got.NativeMarket != "KRW-BTC" || got.Side != unified.SideBuy ||
		got.Type != unified.OrderTypeLimit || got.Status != unified.OrderStatusPartiallyFilled {
		t.Fatalf("fromBithumbOrderDetail() = %+v", got)
	}
}

func TestUnifiedSpotOpenOrdersFollowsCursorOnSameRoute(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2/orders/pending" || request.Method != http.MethodGet {
			http.NotFound(writer, request)
			return
		}
		verifySignedRequest(t, request, []byte("secret-key"), "nonce-fixed", 1700000000123)
		writer.Header().Set("Content-Type", "application/json")
		switch calls.Add(1) {
		case 1:
			if request.URL.Query().Get("next_key") != "" || request.URL.Query().Get("limit") != "100" {
				t.Errorf("first page query = %q", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{"data":[{"order_id":"order-1","market":"KRW-BTC","side":"bid","order_type":"limit","state":"wait","volume":"0.01","executed_volume":"0"}],"has_next":true,"next_key":"cursor+/="}`)
		case 2:
			if request.URL.Query().Get("next_key") != "cursor+/=" {
				t.Errorf("second page query = %q", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{"data":[{"order_id":"order-2","market":"BTC-ETH","side":"ask","order_type":"market","state":"watch","volume":"2","executed_volume":"0.5"}],"has_next":false,"next_key":""}`)
		default:
			t.Errorf("unexpected page request %d", calls.Load())
		}
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
	if len(orders) != 2 || orders[0].Market != (unified.Market{Base: "BTC", Quote: "KRW"}) ||
		orders[1].Market != (unified.Market{Base: "ETH", Quote: "BTC"}) ||
		orders[1].Status != unified.OrderStatusPartiallyFilled {
		t.Fatalf("OpenOrders() = %+v", orders)
	}
	if routes := sender.snapshot(); len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedSpotOpenOrdersRejectsRepeatedCursor(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"data":[],"has_next":true,"next_key":"same-cursor"}`)
	}))
	defer server.Close()

	native, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"},
	)
	adapter, _ := NewUnifiedSpot(native)
	_, err := adapter.OpenOrders(context.Background(), unified.OpenOrdersRequest{AllMarkets: true})
	if err == nil || !strings.Contains(err.Error(), "repeated cursor") {
		t.Fatalf("OpenOrders() error = %v", err)
	}
}

func TestNewUnifiedSpotRejectsNilClient(t *testing.T) {
	t.Parallel()

	if _, err := NewUnifiedSpot(nil); err == nil {
		t.Fatal("NewUnifiedSpot(nil) error = nil")
	}
}
