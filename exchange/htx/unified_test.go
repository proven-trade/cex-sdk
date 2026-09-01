package htx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/conformance"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/transport"
	"github.com/proven-trade/cex-sdk/unified"
)

func TestUnifiedSpotReadConformance(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/market/detail/merged":
			if request.URL.Query().Get("symbol") != "btcusdt" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","ts":1787627045000,"tick":{"id":1,"version":1,"open":"63000","close":"64000.10","low":"62000","high":"65000","amount":"1","vol":"64000","count":1,"bid":["63999","1"],"ask":["64001","1"]}}`)
		case "/v1/account/accounts":
			if !verifyHTXSignedRequest(t, request, []byte("test-secret"), fixedNow) {
				http.Error(writer, `{"status":"error","err-code":"api-signature-not-valid"}`, http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"id":12345,"type":"spot","state":"working"}]}`)
		case "/v1/account/accounts/12345/balance":
			if !verifyHTXSignedRequest(t, request, []byte("test-secret"), fixedNow) {
				http.Error(writer, `{"status":"error","err-code":"api-signature-not-valid"}`, http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":{"id":12345,"type":"spot","state":"working","list":[{"currency":"usdt","type":"trade","balance":"10.25"},{"currency":"usdt","type":"frozen","balance":"1.25"}]}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newPrivateTestClient(
		t, server.URL, sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	adapter, err := NewUnifiedSpot(native)
	if err != nil {
		t.Fatalf("NewUnifiedSpot() error = %v", err)
	}
	conformance.RunSpotReadSuite(t, conformance.SpotReadScenario{
		Client: adapter, Exchange: model.ExchangeHTX,
		Market: unified.Market{Base: "BTC", Quote: "USDT"},
		Price:  "64000.10", NativeMarket: "btcusdt",
		BalanceAsset: "USDT", BalanceAvailable: "10.25",
		Options: []trade.RequestOption{trade.WithEgressRoute("route-b")},
	})
	if routes := sender.snapshot(); len(routes) != 3 {
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
	fixedNow := time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if !verifyHTXSignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"status":"error","err-code":"api-signature-not-valid"}`, http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/v1/account/accounts":
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"id":12345,"type":"spot","state":"working"}]}`)
		case "/v1/order/orders/place":
			var body struct {
				AccountID     string    `json:"account-id"`
				Symbol        string    `json:"symbol"`
				Type          OrderType `json:"type"`
				ClientOrderID string    `json:"client-order-id"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil ||
				body.AccountID != "12345" || body.Symbol != "btcusdt" ||
				body.Type != OrderTypeBuyLimitMaker || body.ClientOrderID != "strategy-1" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":"10001"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newPrivateTestClient(
		t, server.URL, sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	adapter, _ := NewUnifiedSpot(native)
	conformance.RunSpotOrderSuite(t, conformance.SpotOrderScenario{
		Client: adapter, Exchange: model.ExchangeHTX,
		Request: unified.PlaceOrderRequest{
			Market: unified.Market{Base: "BTC", Quote: "USDT"},
			Side:   unified.SideBuy, Type: unified.OrderTypeLimit,
			TimeInForce: unified.TimeInForcePostOnly,
			Quantity:    "0.1", Price: "64000", ClientOrderID: "strategy-1",
		},
		OrderID: "10001", ClientOrderID: "strategy-1", NativeMarket: "btcusdt",
		Status:  unified.OrderStatusAcknowledged,
		Options: []trade.RequestOption{trade.WithEgressRoute("route-b")},
	})
	if routes := sender.snapshot(); len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedHTXMarketDataMapping(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/settings/common/market-symbols":
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"symbol":"btcusdt","state":"online","bc":"btc","qc":"usdt","at":"enabled"},{"symbol":"ethusdt","state":"offline","bc":"eth","qc":"usdt","at":"disabled"}],"ts":1787627045000,"full":1}`)
		case "/market/depth":
			query := request.URL.Query()
			if query.Get("symbol") != "btcusdt" || query.Get("depth") != "5" || query.Get("type") != "step0" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","tick":{"ts":1787627045000,"version":10,"bids":[["64000","2"],["63999","3"]],"asks":[["64001","4"],["64002","5"]]}}`)
		case "/market/history/trade":
			if request.URL.Query().Get("size") != "1" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"id":1,"ts":1787627045000,"data":[{"id":11,"trade-id":101,"amount":"0.01","price":"64000","ts":1787627045000,"direction":"sell"},{"id":12,"trade-id":102,"amount":"0.02","price":"64001","ts":1787627044000,"direction":"buy"}]}]}`)
		case "/market/history/kline":
			query := request.URL.Query()
			if query.Get("period") == "1min" && query.Get("size") == "3" {
				_, _ = io.WriteString(writer, `{"status":"ok","data":[{"id":360,"open":"7","close":"7.5","low":"6","high":"8","amount":"4","vol":"30","count":1},{"id":300,"open":"5","close":"5.5","low":"4","high":"6","amount":"0.003","vol":"1","count":1},{"id":240,"open":"3","close":"4","low":"2","high":"5","amount":"2.20","vol":"8","count":1}]}`)
				return
			}
			if query.Get("period") != "5min" || query.Get("size") != "1" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"id":1787626800,"open":"63000","close":"64000","low":"62000","high":"65000","amount":"10","vol":"640000","count":10}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	adapter, _ := NewUnifiedSpot(mustPublicHTXClient(t, server.URL, &directSender{}))
	market := unified.Market{Base: "BTC", Quote: "USDT"}
	markets, err := adapter.Markets(context.Background(), trade.WithEgressRoute("route-b"))
	if err != nil || len(markets) != 2 || markets[0].Market != market ||
		markets[0].Status != "trading" || markets[1].Status != "disabled" || len(markets[0].Raw) == 0 {
		t.Fatalf("Markets() = %+v, error = %v", markets, err)
	}
	book, err := adapter.OrderBook(context.Background(), unified.OrderBookRequest{
		Market: market, Limit: 1,
	})
	if err != nil || len(book.Bids) != 1 || len(book.Asks) != 1 ||
		book.Bids[0].Quantity != "2" || book.Timestamp != 1_787_627_045_000 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := adapter.RecentTrades(context.Background(), unified.RecentTradesRequest{
		Market: market, Limit: 1,
	})
	if err != nil || len(trades) != 1 || trades[0].ID != "101" || trades[0].Side != unified.SideSell {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	threeMinute, err := adapter.Candles(context.Background(), unified.CandlesRequest{
		Market: market, Interval: unified.Candle3Minutes, Limit: 1,
	})
	if err != nil || len(threeMinute) != 1 || threeMinute[0].StartTime != 360_000 ||
		threeMinute[0].Open != "7" || threeMinute[0].Volume != "4" {
		t.Fatalf("Candles(3m) = %+v, error = %v", threeMinute, err)
	}
	fiveMinute, err := adapter.Candles(context.Background(), unified.CandlesRequest{
		Market: market, Interval: unified.Candle5Minutes, Limit: 1,
	})
	if err != nil || len(fiveMinute) != 1 || fiveMinute[0].StartTime != 1_787_626_800_000 ||
		fiveMinute[0].Close != "64000" {
		t.Fatalf("Candles(5m) = %+v, error = %v", fiveMinute, err)
	}
}

func TestUnifiedHTXOrderMapping(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path != "/v1/settings/common/market-symbols" &&
			!verifyHTXSignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"status":"error","err-code":"api-signature-not-valid"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case request.URL.Path == "/v1/order/orders/getClientOrder" && request.Method == http.MethodGet:
			if request.URL.Query().Get("clientOrderId") != "strategy-1" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":`+htxOrderJSON(OrderStatePartialFilled, false)+`}`)
		case request.URL.Path == "/v1/order/orders/10001/submitcancel" && request.Method == http.MethodPost:
			if request.URL.Query().Get("symbol") != "btcusdt" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":"10001"}`)
		case request.URL.Path == "/v1/settings/common/market-symbols":
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"symbol":"btcusdt","state":"online","bc":"btc","qc":"usdt","at":"enabled"}],"ts":1787627045000,"full":1}`)
		case request.URL.Path == "/v1/account/accounts":
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"id":12345,"type":"spot","state":"working"}]}`)
		case request.URL.Path == "/v1/order/openOrders":
			if request.URL.Query().Get("account-id") != "12345" ||
				request.URL.Query().Get("size") != "500" || request.URL.Query().Get("symbol") != "" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":[`+htxOrderJSON(OrderStateSubmitted, true)+`]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newPrivateTestClient(
		t, server.URL, sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	adapter, _ := NewUnifiedSpot(native)
	market := unified.Market{Base: "BTC", Quote: "USDT"}
	order, err := adapter.Order(context.Background(), unified.OrderRequest{
		Market: market, ClientOrderID: "strategy-1",
	}, trade.WithEgressRoute("route-b"))
	if err != nil || order.ID != "10001" || order.Side != unified.SideBuy ||
		order.Type != unified.OrderTypeLimit || order.Status != unified.OrderStatusPartiallyFilled {
		t.Fatalf("Order() = %+v, error = %v", order, err)
	}
	canceled, err := adapter.CancelOrder(context.Background(), unified.OrderRequest{
		Market: market, OrderID: "10001",
	}, trade.WithEgressRoute("route-b"))
	if err != nil || canceled.ID != "10001" || canceled.Status != unified.OrderStatusCancelPending ||
		len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	open, err := adapter.OpenOrders(
		context.Background(), unified.OpenOrdersRequest{AllMarkets: true},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || len(open) != 1 || open[0].Market != market ||
		open[0].Status != unified.OrderStatusNew || open[0].ExecutedQuantity != "0.01" {
		t.Fatalf("OpenOrders() = %+v, error = %v", open, err)
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

func TestUnifiedHTXHelpers(t *testing.T) {
	t.Parallel()
	clientOrderID, err := newHTXClientOrderID()
	if err != nil {
		t.Fatalf("newHTXClientOrderID() error = %v", err)
	}
	if !regexp.MustCompile(`^proven-[0-9a-f]{24}$`).MatchString(clientOrderID) {
		t.Fatalf("newHTXClientOrderID() = %q", clientOrderID)
	}
	if status := fromHTXOrderStatus(OrderStateCanceling); status != unified.OrderStatusUnknown {
		t.Fatalf("canceling order status = %q", status)
	}
	if side, orderType, err := fromHTXOrderType(OrderTypeSellStopLimitFOK); err != nil ||
		side != unified.SideSell || orderType != unified.OrderTypeLimit {
		t.Fatalf("sell stop FOK mapping = %q, %q, error = %v", side, orderType, err)
	}
	if _, err := marketFromHTXSymbol(MarketSymbol{
		Symbol: "wrong", BaseCurrency: "btc", QuoteCurrency: "usdt",
	}); err == nil {
		t.Fatal("mismatched native symbol error = nil")
	}
}

func TestNewUnifiedSpotRejectsNilClient(t *testing.T) {
	t.Parallel()
	if _, err := NewUnifiedSpot(nil); err == nil {
		t.Fatal("NewUnifiedSpot(nil) error = nil")
	}
}

func mustPublicHTXClient(t *testing.T, baseURL string, sender *directSender) *Client {
	t.Helper()
	client, _ := newTestClient(t, baseURL, sender)
	return client
}
