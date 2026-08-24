package bitget

import (
	"io"
	"net/http"
	"net/http/httptest"
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

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v3/market/tickers":
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","data":[{"category":"SPOT","symbol":"BTCUSDT","lastPrice":"64000.10"}]}`)
		case "/api/v3/account/assets":
			if !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
				http.Error(writer, `{"code":"40009","msg":"signature error"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","data":{"assets":[{"coin":"USDT","available":"1000.00","locked":"2.00"}]}}`)
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
		Client: adapter, Exchange: model.ExchangeBitget,
		Market: unified.Market{Base: "BTC", Quote: "USDT"},
		Price:  "64000.10", NativeMarket: "BTCUSDT",
		BalanceAsset: "USDT", BalanceAvailable: "1000.00",
		Options: []trade.RequestOption{trade.WithEgressRoute("route-b")},
	})
	if routes := sender.snapshot(); len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedBitgetOrderMapping(t *testing.T) {
	t.Parallel()

	market := unified.Market{Base: "BTC", Quote: "USDT"}
	native := Order{
		OrderID: "42", ClientOrderID: "client-1", Symbol: "BTCUSDT",
		Side: SideSell, OrderType: OrderTypeMarket, Status: OrderStatusFilled,
		Quantity: "0.01", ExecutedQuantity: "0.01",
	}
	got := fromBitgetOrder(native, market)
	if got.ID != "42" || got.NativeMarket != "BTCUSDT" || got.Side != unified.SideSell ||
		got.Type != unified.OrderTypeMarket || got.Status != unified.OrderStatusFilled {
		t.Fatalf("fromBitgetOrder() = %+v", got)
	}
}

func TestUnifiedSpotOrderConformance(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v3/trade/place-order" || request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		if !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":"40009","msg":"signature error"}`, http.StatusBadRequest)
			return
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"category":"SPOT","symbol":"BTCUSDT","qty":"100","side":"buy","orderType":"market","clientOid":"client-1"}` {
			http.Error(writer, `{"code":"40017","msg":"bad common mapping"}`, http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","data":{"orderId":"42","clientOid":"client-1"}}`)
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
		Client: adapter, Exchange: model.ExchangeBitget, Request: request,
		OrderID: "42", ClientOrderID: "client-1", NativeMarket: "BTCUSDT",
	})
}
