package binance

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
		case "/api/v3/ticker/price":
			_, _ = io.WriteString(writer, `{"symbol":"BTCUSDT","price":"64000.10"}`)
		case "/api/v3/account":
			if !verifySignedRequest(t, request, []byte("test-secret-key")) {
				http.Error(writer, `{"code":-1022,"msg":"Signature invalid."}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"accountType":"SPOT","balances":[{"asset":"USDT","free":"1000.00","locked":"2.00"}]}`)
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
	adapter, err := NewUnifiedSpot(native)
	if err != nil {
		t.Fatalf("NewUnifiedSpot() error = %v", err)
	}
	conformance.RunSpotReadSuite(t, conformance.SpotReadScenario{
		Client: adapter, Exchange: model.ExchangeBinance,
		Market: unified.Market{Base: "BTC", Quote: "USDT"},
		Price:  "64000.10", NativeMarket: "BTCUSDT",
		BalanceAsset: "USDT", BalanceAvailable: "1000.00",
		Options: []trade.RequestOption{trade.WithEgressRoute("route-b")},
	})
	routes, _ := sender.snapshot()
	if len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedBinanceOrderMapping(t *testing.T) {
	t.Parallel()

	market := unified.Market{Base: "BTC", Quote: "USDT"}
	native := Order{
		Symbol: "BTCUSDT", OrderID: 42, ClientOrderID: "client-1",
		Side: SideBuy, Type: OrderTypeLimitMaker, Status: OrderStatusPartiallyFilled,
		Price: "64000", OriginalQuantity: "0.01", ExecutedQuantity: "0.004",
	}
	got := fromBinanceOrder(native, market)
	if got.ID != "42" || got.NativeMarket != "BTCUSDT" || got.Side != unified.SideBuy ||
		got.Type != unified.OrderTypeLimit || got.Status != unified.OrderStatusPartiallyFilled {
		t.Fatalf("fromBinanceOrder() = %+v", got)
	}
}

func TestUnifiedSpotOrderConformance(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v3/order" || request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		if !verifySignedRequest(t, request, []byte("test-secret-key")) {
			http.Error(writer, `{"code":-1022,"msg":"Signature invalid."}`, http.StatusBadRequest)
			return
		}
		query := request.URL.Query()
		if query.Get("symbol") != "BTCUSDT" || query.Get("side") != "BUY" ||
			query.Get("type") != "MARKET" || query.Get("quoteOrderQty") != "100" ||
			query.Get("newClientOrderId") != "client-1" {
			http.Error(writer, `{"code":-1102,"msg":"bad common mapping"}`, http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(writer, `{"symbol":"BTCUSDT","orderId":42,"clientOrderId":"client-1","status":"NEW","type":"MARKET","side":"BUY","origQuoteOrderQty":"100"}`)
	}))
	defer server.Close()

	native, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, nil,
	)
	adapter, _ := NewUnifiedSpot(native)
	request := unified.PlaceOrderRequest{
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, Side: unified.SideBuy,
		Type: unified.OrderTypeMarket, QuoteAmount: "100", ClientOrderID: "client-1",
	}
	conformance.RunSpotOrderSuite(t, conformance.SpotOrderScenario{
		Client: adapter, Exchange: model.ExchangeBinance, Request: request,
		OrderID: "42", ClientOrderID: "client-1", NativeMarket: "BTCUSDT",
	})
}
