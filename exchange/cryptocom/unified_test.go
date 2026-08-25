package cryptocom

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/conformance"
	"github.com/proven-trade/proven-trade-sdk/credential"
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
		case "/exchange/v1/public/get-tickers":
			if request.URL.Query().Get("instrument_name") != "BTC_USDT" {
				http.Error(writer, `{"code":"40001"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"id":"1","method":"public/get-tickers","code":"0","result":{"data":[{"i":"BTC_USDT","a":"64000.10","t":"1700000000000"}]}}`)
		case "/exchange/v1/private/user-balance":
			envelope, ok := verifyPrivateRequest(t, request, fixedNow)
			if !ok {
				http.Error(writer, `{"code":"40101"}`, http.StatusUnauthorized)
				return
			}
			writePrivateSuccess(writer, envelope, `{"data":[{"instrument_name":"USD","position_balances":[{"instrument_name":"USDT","quantity":"11.50","max_withdrawal_balance":"10.25","reserved_qty":"1.25"}]}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newPrivateTestClient(
		t, server.URL+"/exchange/v1", sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	adapter, err := NewUnifiedSpot(native)
	if err != nil {
		t.Fatalf("NewUnifiedSpot() error = %v", err)
	}
	conformance.RunSpotReadSuite(t, conformance.SpotReadScenario{
		Client: adapter, Exchange: model.ExchangeCryptoCom,
		Market: unified.Market{Base: "BTC", Quote: "USDT"},
		Price:  "64000.10", NativeMarket: "BTC_USDT",
		BalanceAsset: "USDT", BalanceAvailable: "10.25",
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
		envelope, ok := verifyPrivateRequest(t, request, fixedNow)
		if !ok || envelope.Method != methodCreateOrder ||
			stringParam(envelope.Params, "instrument_name") != "BTC_USDT" ||
			stringParam(envelope.Params, "side") != "BUY" ||
			stringParam(envelope.Params, "type") != "MARKET" ||
			stringParam(envelope.Params, "notional") != "100" ||
			stringParam(envelope.Params, "quantity") != "" ||
			stringParam(envelope.Params, "client_oid") != "strategy-1" {
			http.Error(writer, `{"code":"40001"}`, http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writePrivateSuccess(writer, envelope, `{"client_oid":"strategy-1","order_id":"18342311"}`)
	}))
	defer server.Close()

	native, _ := newPrivateTestClient(
		t, server.URL+"/exchange/v1", &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	adapter, _ := NewUnifiedSpot(native)
	request := unified.PlaceOrderRequest{
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, Side: unified.SideBuy,
		Type: unified.OrderTypeMarket, QuoteAmount: "100", ClientOrderID: "strategy-1",
	}
	conformance.RunSpotOrderSuite(t, conformance.SpotOrderScenario{
		Client: adapter, Exchange: model.ExchangeCryptoCom, Request: request,
		OrderID: "18342311", ClientOrderID: "strategy-1", NativeMarket: "BTC_USDT",
	})
}

func TestUnifiedCryptoComMarketDataMapping(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/exchange/v1/public/get-instruments":
			_, _ = io.WriteString(writer, `{"id":"1","method":"public/get-instruments","code":"0","result":{"data":[{"symbol":"BTC_USDT","inst_type":"CCY_PAIR","base_ccy":"BTC","quote_ccy":"USDT","tradable":true},{"symbol":"ETH_USDT","inst_type":"CCY_PAIR","base_ccy":"ETH","quote_ccy":"USDT","tradable":false},{"symbol":"BTCUSD-PERP","inst_type":"PERPETUAL_SWAP","base_ccy":"BTC","quote_ccy":"USD","tradable":true}]}}`)
		case "/exchange/v1/public/get-book":
			if request.URL.Query().Get("instrument_name") != "BTC_USDT" ||
				request.URL.Query().Get("depth") != "1" {
				http.Error(writer, `{"code":"40001"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"id":"2","method":"public/get-book","code":"0","result":{"instrument_name":"BTC_USDT","depth":"1","data":[{"bids":[["64000","2",1]],"asks":[["64001","3",1]],"t":"1700000000000"}]}}`)
		case "/exchange/v1/public/get-trades":
			if request.URL.Query().Get("instrument_name") != "BTC_USDT" ||
				request.URL.Query().Get("count") != "1" {
				http.Error(writer, `{"code":"40001"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"id":"3","method":"public/get-trades","code":"0","result":{"data":[{"s":"SELL","p":"64000","q":"0.01","t":"1700000000000","tn":"1700000000000000000","d":"trade-1","i":"BTC_USDT"}]}}`)
		case "/exchange/v1/public/get-candlestick":
			query := request.URL.Query()
			if query.Get("timeframe") == "1m" && query.Get("count") == "3" {
				_, _ = io.WriteString(writer, `{"id":"4","method":"public/get-candlestick","code":"0","result":{"instrument_name":"BTC_USDT","interval":"1m","data":[{"o":"62500","h":"63500","l":"62000","c":"63000","v":"1","t":"1699999920000"},{"o":"63000","h":"64000","l":"62800","c":"63800","v":"2","t":"1699999980000"},{"o":"63800","h":"64500","l":"63500","c":"64000","v":"3","t":"1700000040000"}]}}`)
				return
			}
			if query.Get("timeframe") != "5m" || query.Get("count") != "1" {
				http.Error(writer, `{"code":"40001"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"id":"5","method":"public/get-candlestick","code":"0","result":{"instrument_name":"BTC_USDT","interval":"5m","data":[{"o":"63000","h":"65000","l":"62000","c":"64000","v":"10","t":"1700000000000"}]}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	adapter, _ := NewUnifiedSpot(mustPublicCryptoComClient(t, server.URL+"/exchange/v1"))
	market := unified.Market{Base: "BTC", Quote: "USDT"}
	markets, err := adapter.Markets(context.Background(), trade.WithEgressRoute("route-b"))
	if err != nil || len(markets) != 2 || markets[0].Market != market ||
		markets[0].Status != "trading" || markets[1].Status != "disabled" || len(markets[0].Raw) == 0 {
		t.Fatalf("Markets() = %+v, error = %v", markets, err)
	}
	book, err := adapter.OrderBook(context.Background(), unified.OrderBookRequest{
		Market: market, Limit: 1,
	})
	if err != nil || len(book.Bids) != 1 || book.Bids[0].Quantity != "2" ||
		book.Timestamp != 1_700_000_000_000 || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := adapter.RecentTrades(context.Background(), unified.RecentTradesRequest{
		Market: market, Limit: 1,
	})
	if err != nil || len(trades) != 1 || trades[0].ID != "trade-1" ||
		trades[0].Side != unified.SideSell || trades[0].Timestamp != 1_700_000_000_000 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	fiveMinute, err := adapter.Candles(context.Background(), unified.CandlesRequest{
		Market: market, Interval: unified.Candle5Minutes, Limit: 1,
	})
	if err != nil || len(fiveMinute) != 1 || fiveMinute[0].StartTime != 1_700_000_000_000 ||
		fiveMinute[0].Close != "64000" || fiveMinute[0].Volume != "10" {
		t.Fatalf("Candles(5m) = %+v, error = %v", fiveMinute, err)
	}
	threeMinute, err := adapter.Candles(context.Background(), unified.CandlesRequest{
		Market: market, Interval: unified.Candle3Minutes, Limit: 1,
	})
	if err != nil || len(threeMinute) != 1 || threeMinute[0].StartTime != 1_699_999_920_000 ||
		threeMinute[0].Open != "62500" || threeMinute[0].High != "64500" ||
		threeMinute[0].Low != "62000" || threeMinute[0].Close != "64000" ||
		threeMinute[0].Volume != "6" {
		t.Fatalf("Candles(3m) = %+v, error = %v", threeMinute, err)
	}
}

func TestUnifiedCryptoComOrderCancelAndOpenOrderMapping(t *testing.T) {
	t.Parallel()
	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		envelope, ok := verifyPrivateRequest(t, request, fixedNow)
		if !ok {
			http.Error(writer, `{"code":"40101"}`, http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		switch envelope.Method {
		case methodGetOrderDetail:
			writePrivateSuccess(writer, envelope, cryptoComUnifiedOrderJSON("ACTIVE", "0.01"))
		case methodCancelOrder:
			writePrivateSuccess(writer, envelope, `{"client_oid":"strategy-1","order_id":"18342311"}`)
		case methodGetOpenOrders:
			writePrivateSuccess(writer, envelope, `{"data":[`+cryptoComUnifiedOrderJSON("ACTIVE", "0")+`]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newPrivateTestClient(
		t, server.URL+"/exchange/v1", sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	adapter, _ := NewUnifiedSpot(native)
	market := unified.Market{Base: "BTC", Quote: "USDT"}
	order, err := adapter.Order(context.Background(), unified.OrderRequest{
		Market: market, ClientOrderID: "strategy-1",
	}, trade.WithEgressRoute("route-b"))
	if err != nil || order.ID != "18342311" || order.Side != unified.SideBuy ||
		order.Type != unified.OrderTypeLimit || order.Status != unified.OrderStatusPartiallyFilled ||
		order.ExecutedQuantity != "0.01" || len(order.Raw) == 0 {
		t.Fatalf("Order() = %+v, error = %v", order, err)
	}
	canceled, err := adapter.CancelOrder(context.Background(), unified.OrderRequest{
		Market: market, OrderID: "18342311",
	}, trade.WithEgressRoute("route-b"))
	if err != nil || canceled.ID != "18342311" || canceled.ClientOrderID != "strategy-1" ||
		canceled.Status != unified.OrderStatusUnknown || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	open, err := adapter.OpenOrders(context.Background(), unified.OpenOrdersRequest{
		Market: &market,
	}, trade.WithEgressRoute("route-b"))
	if err != nil || len(open) != 1 || open[0].Market != market ||
		open[0].Status != unified.OrderStatusNew {
		t.Fatalf("OpenOrders() = %+v, error = %v", open, err)
	}
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

func TestUnifiedCryptoComHelpers(t *testing.T) {
	t.Parallel()
	clientOrderID, err := newCryptoComClientOrderID()
	if err != nil {
		t.Fatalf("newCryptoComClientOrderID() error = %v", err)
	}
	if !regexp.MustCompile(`^proven-[0-9a-f]{24}$`).MatchString(clientOrderID) {
		t.Fatalf("newCryptoComClientOrderID() = %q", clientOrderID)
	}
	if status, err := fromCryptoComOrderStatus(OrderStatusActive, "0.0001"); err != nil ||
		status != unified.OrderStatusPartiallyFilled {
		t.Fatalf("active order status = %q, error = %v", status, err)
	}
	if status, err := fromCryptoComOrderStatus("FUTURE_STATUS", "0"); err != nil ||
		status != unified.OrderStatusUnknown {
		t.Fatalf("future order status = %q, error = %v", status, err)
	}
	if timeInForce, postOnly := toCryptoComTimeInForce(unified.TimeInForcePostOnly); timeInForce != TimeInForceGoodTillCancel || !postOnly {
		t.Fatalf("post-only mapping = %q, %t", timeInForce, postOnly)
	}
	if timeInForce, postOnly := toCryptoComTimeInForce(unified.TimeInForceIOC); timeInForce != TimeInForceImmediateOrCancel || postOnly {
		t.Fatalf("IOC mapping = %q, %t", timeInForce, postOnly)
	}
	if _, err := marketFromCryptoComInstrument(Instrument{
		Symbol: "WRONG", BaseCurrency: "BTC", QuoteCurrency: "USDT",
	}); err == nil {
		t.Fatal("mismatched native instrument error = nil")
	}
	if _, err := marketFromCryptoComInstrumentName("BTC_USD_C"); err == nil {
		t.Fatal("ambiguous native instrument error = nil")
	}
}

func TestNewUnifiedSpotRejectsNilClient(t *testing.T) {
	t.Parallel()
	if _, err := NewUnifiedSpot(nil); err == nil {
		t.Fatal("NewUnifiedSpot(nil) error = nil")
	}
}

func mustPublicCryptoComClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, _ := newTestClient(t, baseURL, &directSender{})
	return client
}

func cryptoComUnifiedOrderJSON(status string, cumulativeQuantity string) string {
	return `{"account_id":"account-1","order_id":"18342311","client_oid":"strategy-1","order_type":"LIMIT","time_in_force":"GOOD_TILL_CANCEL","side":"BUY","quantity":"0.1","limit_price":"64000","cumulative_quantity":"` + cumulativeQuantity + `","status":"` + status + `","instrument_name":"BTC_USDT"}`
}
