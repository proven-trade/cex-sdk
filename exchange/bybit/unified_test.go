package bybit

import (
	"io"
	"net/http"
	"net/http/httptest"
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
		case "/v5/market/tickers":
			if request.URL.Query().Get("category") != "spot" || request.URL.Query().Get("symbol") != "BTCUSDT" {
				http.Error(writer, `{"retCode":10001,"retMsg":"bad ticker mapping"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"category":"spot","list":[{"symbol":"BTCUSDT","lastPrice":"64000.10"}]},"time":1700000000000}`)
		case "/v5/account/wallet-balance":
			if !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) ||
				request.URL.Query().Get("accountType") != "UNIFIED" {
				http.Error(writer, `{"retCode":10004,"retMsg":"bad signed balance mapping"}`, http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"list":[{"accountType":"UNIFIED","coin":[{"coin":"USDT","walletBalance":"1002.00","spotBorrow":"0","availableToWithdraw":"","locked":"2.00"}]}]},"time":1700000000000}`)
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
		Client: adapter, Exchange: model.ExchangeBybit,
		Market: unified.Market{Base: "BTC", Quote: "USDT"},
		Price:  "64000.10", NativeMarket: "BTCUSDT",
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
		if request.URL.Path != "/v5/order/create" || request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		if !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"retCode":10004,"retMsg":"error sign"}`, http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"category":"spot","symbol":"BTCUSDT","side":"Buy","orderType":"Market","qty":"100","orderLinkId":"client-1","marketUnit":"quoteCoin"}` {
			http.Error(writer, `{"retCode":10001,"retMsg":"bad common mapping"}`, http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"orderId":"42","orderLinkId":"client-1"},"time":1700000000000}`)
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
		Client: adapter, Exchange: model.ExchangeBybit, Request: request,
		OrderID: "42", ClientOrderID: "client-1", NativeMarket: "BTCUSDT",
	})
}

func TestUnifiedBybitMapping(t *testing.T) {
	t.Parallel()

	market := unified.Market{Base: "BTC", Quote: "USDT"}
	native := Order{
		OrderID: "42", OrderLinkID: "client-1", Symbol: "BTCUSDT",
		Side: SideSell, OrderType: OrderTypeMarket, OrderStatus: "PartiallyFilledCanceled",
		Quantity: "0.01", CumulativeExecutedQuantity: "0.004",
	}
	got := fromBybitOrder(native, market)
	if got.ID != "42" || got.NativeMarket != "BTCUSDT" || got.Side != unified.SideSell ||
		got.Type != unified.OrderTypeMarket || got.Status != unified.OrderStatusCanceled ||
		got.ExecutedQuantity != "0.004" {
		t.Fatalf("fromBybitOrder() = %+v", got)
	}
	levels, err := fromBybitBookLevels([][]string{{"64000", "1.5", "ignored"}})
	if err != nil || len(levels) != 1 || levels[0].Quantity != "1.5" {
		t.Fatalf("fromBybitBookLevels() = %+v, error = %v", levels, err)
	}
	if _, err := fromBybitBookLevels([][]string{{"64000"}}); err == nil {
		t.Fatal("short Bybit book level error = nil")
	}
}

func TestUnifiedBybitAvailableBalanceUsesCurrentUTAFields(t *testing.T) {
	t.Parallel()

	available, err := bybitSpotAvailable(WalletCoin{
		Coin: "USDT", WalletBalance: "1002.50", SpotBorrow: "0.25", Locked: "2.00",
	})
	if err != nil || available != "1000.25" {
		t.Fatalf("bybitSpotAvailable() = %q, error = %v", available, err)
	}
	available, err = bybitSpotAvailable(WalletCoin{
		Coin: "BTC", WalletBalance: "1", Locked: "0.00000001",
	})
	if err != nil || available != "0.99999999" {
		t.Fatalf("bybitSpotAvailable() precision = %q, error = %v", available, err)
	}
	if _, err := bybitSpotAvailable(WalletCoin{
		Coin: "ETH", WalletBalance: "1", Locked: "2",
	}); err == nil {
		t.Fatal("negative available balance error = nil")
	}
}

func TestNewUnifiedSpotRejectsNilClient(t *testing.T) {
	t.Parallel()

	if _, err := NewUnifiedSpot(nil); err == nil {
		t.Fatal("NewUnifiedSpot(nil) error = nil")
	}
}
