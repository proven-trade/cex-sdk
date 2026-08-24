package upbit

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/conformance"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/transport"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

func TestUnifiedSpotReadConformance(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/ticker":
			_, _ = io.WriteString(writer, `[{"market":"KRW-BTC","trade_price":64000.10}]`)
		case "/v1/accounts":
			verifySignedRequest(t, request, []byte("secret-key"), "nonce-fixed")
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
		Client: adapter, Exchange: model.ExchangeUpbit,
		Market: unified.Market{Base: "BTC", Quote: "KRW"},
		Price:  "64000.10", NativeMarket: "KRW-BTC",
		BalanceAsset: "KRW", BalanceAvailable: "1000000.00",
		Options: []trade.RequestOption{trade.WithEgressRoute("route-b")},
	})
	if routes := sender.snapshot(); len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedUpbitOrderMapping(t *testing.T) {
	t.Parallel()

	market := unified.Market{Base: "BTC", Quote: "KRW"}
	native := Order{
		UUID: "order-1", Identifier: "client-1", Market: "KRW-BTC",
		Side: SideBid, OrderType: OrderTypeLimit, State: OrderStateWait,
		Volume: "0.01", ExecutedVolume: "0.004",
	}
	got := fromUpbitOrder(native, market)
	if got.ID != "order-1" || got.NativeMarket != "KRW-BTC" || got.Side != unified.SideBuy ||
		got.Type != unified.OrderTypeLimit || got.Status != unified.OrderStatusPartiallyFilled {
		t.Fatalf("fromUpbitOrder() = %+v", got)
	}
}

func TestUnifiedSpotOrderConformance(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/orders" || request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		verifySignedRequest(t, request, []byte("secret-key"), "nonce-fixed")
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"market":"KRW-BTC","side":"bid","price":"100000","ord_type":"price","identifier":"client-1"}` {
			http.Error(writer, `{"error":{"name":"validation_error","message":"bad common mapping"}}`, http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(writer, `{"uuid":"order-1","side":"bid","ord_type":"price","price":"100000","state":"wait","market":"KRW-BTC","identifier":"client-1"}`)
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
		Client: adapter, Exchange: model.ExchangeUpbit, Request: request,
		OrderID: "order-1", ClientOrderID: "client-1", NativeMarket: "KRW-BTC",
	})
}
