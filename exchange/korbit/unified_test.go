package korbit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
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
		case "/v2/tickers":
			_, _ = io.WriteString(writer, `{"success":true,"data":[{"symbol":"btc_krw","close":"64000.10"}]}`)
		case "/v2/balance":
			verifyKorbitSignedRequest(t, request, []byte("secret-key"))
			_, _ = io.WriteString(writer, `{"success":true,"data":[{"currency":"krw","available":"1000000.00","tradeInUse":"1.10","withdrawalInUse":"0.20"}]}`)
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
		Client: adapter, Exchange: model.ExchangeKorbit,
		Market: unified.Market{Base: "BTC", Quote: "KRW"},
		Price:  "64000.10", NativeMarket: "btc_krw",
		BalanceAsset: "KRW", BalanceAvailable: "1000000.00",
		Options: []trade.RequestOption{trade.WithEgressRoute("route-b")},
	})
	balances, err := adapter.Balances(context.Background(), trade.WithEgressRoute("route-b"))
	if err != nil || len(balances) != 1 || balances[0].Locked != "1.30" {
		t.Fatalf("Balances() = %+v, error = %v", balances, err)
	}
	if routes := sender.snapshot(); len(routes) != 3 || routes[0] != "route-b" ||
		routes[1] != "route-b" || routes[2] != "route-b" {
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
		verifyKorbitSignedRequest(t, request, []byte("secret-key"))
		unsigned := unsignedKorbitParameters(t, request)
		want := "amt=100000&clientOrderId=client-1&orderType=market&recvWindow=5000&side=buy&symbol=btc_krw&timestamp=1700000000123"
		if unsigned != want {
			http.Error(writer, `{"success":false,"error":{"message":"bad common mapping"}}`, http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"success":true,"data":{"orderId":1234}}`)
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
		Client: adapter, Exchange: model.ExchangeKorbit, Request: request,
		OrderID: "1234", ClientOrderID: "client-1", NativeMarket: "btc_krw",
	})
}

func TestUnifiedKorbitOrderMapping(t *testing.T) {
	t.Parallel()

	market := unified.Market{Base: "BTC", Quote: "KRW"}
	native := Order{
		OrderID: 1234, ClientOrderID: "client-1", Symbol: "btc_krw",
		OrderType: OrderTypeBest, Side: SideBuy, Amount: "100000",
		FilledQty: "0.001", Status: OrderStatusPartiallyFilledCanceled,
	}
	got := fromKorbitOrder(native, market)
	if got.ID != "1234" || got.NativeMarket != "btc_krw" || got.Side != unified.SideBuy ||
		got.Type != unified.OrderTypeMarket || got.Status != unified.OrderStatusCanceled ||
		got.Quantity != "100000" || got.ExecutedQuantity != "0.001" {
		t.Fatalf("fromKorbitOrder() = %+v", got)
	}
}

func TestUnifiedKorbitThreeMinuteCandlesUseSameRouteAcrossPages(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2/candles" {
			http.NotFound(writer, request)
			return
		}
		call := calls.Add(1)
		if request.URL.Query().Get("interval") != "1" || request.URL.Query().Get("limit") != "200" {
			t.Errorf("candle query = %q", request.URL.RawQuery)
		}
		start, startErr := strconv.ParseInt(request.URL.Query().Get("start"), 10, 64)
		end, endErr := strconv.ParseInt(request.URL.Query().Get("end"), 10, 64)
		if startErr != nil || endErr != nil || end-start != 200*60_000 {
			t.Errorf("candle range = %d..%d, errors = (%v, %v)", start, end, startErr, endErr)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"success":true,"data":[`)
		for index := int64(0); index < 200; index++ {
			if index > 0 {
				_, _ = io.WriteString(writer, ",")
			}
			_, _ = fmt.Fprintf(
				writer,
				`{"timestamp":%d,"open":"1","high":"2","low":"0.5","close":"1.5","volume":"0.1"}`,
				start+index*60_000,
			)
		}
		_, _ = io.WriteString(writer, `]}`)
		if call > 3 {
			t.Errorf("unexpected candle page %d", call)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newTestClient(
		t, server.URL, sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"},
	)
	adapter, _ := NewUnifiedSpot(native)
	candles, err := adapter.Candles(context.Background(), unified.CandlesRequest{
		Market:   unified.Market{Base: "BTC", Quote: "KRW"},
		Interval: unified.Candle3Minutes, Limit: 200,
	}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("Candles() error = %v", err)
	}
	if len(candles) != 200 || calls.Load() != 3 {
		t.Fatalf("Candles() length = %d, calls = %d", len(candles), calls.Load())
	}
	if routes := sender.snapshot(); len(routes) != 3 || routes[0] != "route-b" ||
		routes[1] != "route-b" || routes[2] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedSpotOpenOrdersMapsAllMarketsOnSelectedRoute(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v2/currencyPairs":
			_, _ = io.WriteString(writer, `{"success":true,"data":[{"symbol":"btc_krw","status":"launched","baseCurrency":"btc","quoteCurrency":"krw"},{"symbol":"eth_krw","status":"launched","baseCurrency":"eth","quoteCurrency":"krw"}]}`)
		case "/v2/openOrders":
			verifyKorbitSignedRequest(t, request, []byte("secret-key"))
			if request.URL.Query().Get("symbol") == "eth_krw" {
				_, _ = io.WriteString(writer, `{"success":true,"data":[{"orderId":1234,"clientOrderId":"client-1","symbol":"eth_krw","orderType":"limit","side":"sell","price":"5000000","qty":"2","filledQty":"0.5","status":"partiallyFilled"}]}`)
			} else {
				_, _ = io.WriteString(writer, `{"success":true,"data":[]}`)
			}
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
	adapter, _ := NewUnifiedSpot(native)
	orders, err := adapter.OpenOrders(
		context.Background(), unified.OpenOrdersRequest{AllMarkets: true},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("OpenOrders() error = %v", err)
	}
	if len(orders) != 1 || orders[0].Market != (unified.Market{Base: "ETH", Quote: "KRW"}) ||
		orders[0].Status != unified.OrderStatusPartiallyFilled {
		t.Fatalf("OpenOrders() = %+v", orders)
	}
	if routes := sender.snapshot(); len(routes) != 3 || routes[0] != "route-b" ||
		routes[1] != "route-b" || routes[2] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedKorbitIdentityHelpers(t *testing.T) {
	t.Parallel()

	clientOrderID, err := newKorbitClientOrderID()
	if err != nil {
		t.Fatalf("newKorbitClientOrderID() error = %v", err)
	}
	if !regexp.MustCompile(`^proven-[0-9a-f]{28}$`).MatchString(clientOrderID) {
		t.Fatalf("newKorbitClientOrderID() = %q", clientOrderID)
	}
	if _, err := korbitOrderID("not-a-number"); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("korbitOrderID() error = %v, want ErrValidation", err)
	}
}

func TestNewUnifiedSpotRejectsNilClient(t *testing.T) {
	t.Parallel()

	if _, err := NewUnifiedSpot(nil); err == nil {
		t.Fatal("NewUnifiedSpot(nil) error = nil")
	}
}
