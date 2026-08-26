package okx

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
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
		case "/api/v5/market/tickers":
			if request.URL.Query().Get("instType") != "SPOT" {
				http.Error(writer, `{"code":"51000","msg":"bad ticker mapping","data":[]}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"instType":"SPOT","instId":"ETH-USDT","last":"3200"},{"instType":"SPOT","instId":"BTC-USDT","last":"64000.10","ts":"1700000000000"}]}`)
		case "/api/v5/account/balance":
			if !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
				http.Error(writer, `{"code":"50113","msg":"bad signed balance mapping","data":[]}`, http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"totalEq":"1002","details":[{"ccy":"USDT","availBal":"1000.00","frozenBal":"2.00"}]}]}`)
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
		Client: adapter, Exchange: model.ExchangeOKX,
		Market: unified.Market{Base: "BTC", Quote: "USDT"},
		Price:  "64000.10", NativeMarket: "BTC-USDT",
		BalanceAsset: "USDT", BalanceAvailable: "1000.00",
		Options: []trade.RequestOption{trade.WithEgressRoute("route-b")},
	})
	if routes := sender.snapshot(); len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedSpotOrderConformance(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v5/trade/order" || request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		if !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":"50113","msg":"error sign","data":[]}`, http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"instId":"BTC-USDT","tdMode":"cash","clOrdId":"client1","side":"buy","ordType":"market","sz":"100","tgtCcy":"quote_ccy"}` {
			http.Error(writer, `{"code":"51000","msg":"bad common mapping","data":[]}`, http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"ordId":"42","clOrdId":"client1","sCode":"0","sMsg":""}]}`)
	}))
	defer server.Close()

	native, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return fixedNow },
	)
	adapter, _ := NewUnifiedSpot(native)
	request := unified.PlaceOrderRequest{
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, Side: unified.SideBuy,
		Type: unified.OrderTypeMarket, QuoteAmount: "100", ClientOrderID: "client1",
	}
	conformance.RunSpotOrderSuite(t, conformance.SpotOrderScenario{
		Client: adapter, Exchange: model.ExchangeOKX, Request: request,
		OrderID: "42", ClientOrderID: "client1", NativeMarket: "BTC-USDT",
	})
}

func TestUnifiedOKXMapping(t *testing.T) {
	t.Parallel()

	market := unified.Market{Base: "BTC", Quote: "USDT"}
	native := Order{
		OrderID: "42", ClientOrderID: "client1", InstrumentID: "BTC-USDT",
		Side: SideSell, OrderType: OrderTypeIOC, State: "partially_filled",
		Price: "64000", Quantity: "0.01", ExecutedQuantity: "0.004",
	}
	got := fromOKXOrder(native, market)
	if got.ID != "42" || got.NativeMarket != "BTC-USDT" || got.Side != unified.SideSell ||
		got.Type != unified.OrderTypeLimit || got.Status != unified.OrderStatusPartiallyFilled ||
		got.ExecutedQuantity != "0.004" {
		t.Fatalf("fromOKXOrder() = %+v", got)
	}
	if toOKXLimitOrderType(unified.TimeInForceGTC) != OrderTypeLimit ||
		toOKXLimitOrderType(unified.TimeInForceIOC) != OrderTypeIOC ||
		toOKXLimitOrderType(unified.TimeInForceFOK) != OrderTypeFOK ||
		toOKXLimitOrderType(unified.TimeInForcePostOnly) != OrderTypePostOnly {
		t.Fatal("toOKXLimitOrderType() mapping is invalid")
	}
	parsed, err := fromOKXSpotInstrumentID("BTC-USDT")
	if err != nil || parsed != market {
		t.Fatalf("fromOKXSpotInstrumentID() = %+v, error = %v", parsed, err)
	}
	if _, err := fromOKXSpotInstrumentID("BTC-USDT-SWAP"); err == nil {
		t.Fatal("invalid OKX Spot instrument ID error = nil")
	}
	if _, err := parseOKXMilliseconds("trade", "invalid"); err == nil {
		t.Fatal("invalid OKX timestamp error = nil")
	}
}

func TestUnifiedOKXOpenOrdersUsesAfterCursor(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path != "/api/v5/trade/orders-pending" ||
			!verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":"50113","msg":"bad request","data":[]}`, http.StatusUnauthorized)
			return
		}
		call := calls.Add(1)
		if request.URL.Query().Get("instType") != "SPOT" || request.URL.Query().Get("limit") != "100" {
			http.Error(writer, `{"code":"51000","msg":"bad page mapping","data":[]}`, http.StatusBadRequest)
			return
		}
		items := make([]map[string]string, 0, 100)
		switch call {
		case 1:
			if request.URL.Query().Get("after") != "" {
				http.Error(writer, `{"code":"51000","msg":"unexpected first cursor","data":[]}`, http.StatusBadRequest)
				return
			}
			for value := 100; value >= 1; value-- {
				items = append(items, map[string]string{
					"instType": "SPOT", "instId": "BTC-USDT", "ordId": strconv.Itoa(value),
					"side": "buy", "ordType": "limit", "state": "live", "sz": "0.01",
				})
			}
		case 2:
			if request.URL.Query().Get("after") != "1" {
				http.Error(writer, `{"code":"51000","msg":"bad after cursor","data":[]}`, http.StatusBadRequest)
				return
			}
			items = append(items, map[string]string{
				"instType": "SPOT", "instId": "ETH-USDT", "ordId": "0",
				"side": "sell", "ordType": "market", "state": "partially_filled", "sz": "1",
			})
		default:
			http.Error(writer, `{"code":"51000","msg":"too many pages","data":[]}`, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"code": "0", "msg": "", "data": items})
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newTestClient(
		t, server.URL, sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"}, func() time.Time { return fixedNow },
	)
	adapter, _ := NewUnifiedSpot(native)
	orders, err := adapter.OpenOrders(
		t.Context(), unified.OpenOrdersRequest{AllMarkets: true}, trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("OpenOrders() error = %v", err)
	}
	if len(orders) != 101 || orders[100].Market != (unified.Market{Base: "ETH", Quote: "USDT"}) ||
		orders[100].Status != unified.OrderStatusPartiallyFilled {
		t.Fatalf("OpenOrders() final page = %+v, count = %d", orders[len(orders)-1], len(orders))
	}
	if routes := sender.snapshot(); len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestNewUnifiedSpotRejectsNilClient(t *testing.T) {
	t.Parallel()

	if _, err := NewUnifiedSpot(nil); err == nil {
		t.Fatal("NewUnifiedSpot(nil) error = nil")
	}
}
