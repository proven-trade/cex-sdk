package kraken

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case publicPrefix + "Ticker":
			if request.URL.Query().Get("pair") != "XBTUSDT" || request.Header.Get("API-Key") != "" {
				http.Error(writer, `{"error":["EGeneral:Invalid arguments"]}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"error":[],"result":{"XXBTUSDT":{"a":["64001","1","1"],"b":["63999","1","1"],"c":["64000.10","0.01"],"v":["10","20"],"p":["63000","63500"],"t":[100,200],"l":["62000","61000"],"h":["65000","66000"],"o":"62500"}}}`)
		case privatePrefix + "BalanceEx":
			if !verifyUnifiedKrakenRequest(t, request, secret) {
				http.Error(writer, `{"error":["EAPI:Invalid signature"]}`, http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(writer, `{"error":[],"result":{"USDT":{"balance":"1002.00","credit":1,"credit_used":"1","hold_trade":"2.00"}}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newTestClient(
		t, server.URL, sender, &recordingProvider{secret: secret},
		[]transport.EgressRouteID{"route-a", "route-b"}, fixedNow,
	)
	adapter, err := NewUnifiedSpot(native)
	if err != nil {
		t.Fatalf("NewUnifiedSpot() error = %v", err)
	}
	conformance.RunSpotReadSuite(t, conformance.SpotReadScenario{
		Client: adapter, Exchange: model.ExchangeKraken,
		Market: unified.Market{Base: "BTC", Quote: "USDT"},
		Price:  "64000.10", NativeMarket: "XXBTUSDT",
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
	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != privatePrefix+"AddOrder" || request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		values, valid := unifiedKrakenRequestValues(t, request, secret)
		if !valid || values.Get("pair") != "XBTUSDT" || values.Get("type") != "buy" ||
			values.Get("ordertype") != "market" || values.Get("volume") != "100" ||
			values.Get("oflags") != "viqc" || values.Get("cl_ord_id") != "client-1" {
			http.Error(writer, `{"error":["EGeneral:Invalid arguments"]}`, http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(writer, `{"error":[],"result":{"descr":{"order":"buy USDT worth of XBTUSDT @ market"},"txid":["ORDER-42"]}}`)
	}))
	defer server.Close()

	native, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{secret: secret},
		[]transport.EgressRouteID{"route-a"}, fixedNow,
	)
	adapter, _ := NewUnifiedSpot(native)
	request := unified.PlaceOrderRequest{
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, Side: unified.SideBuy,
		Type: unified.OrderTypeMarket, QuoteAmount: "100", ClientOrderID: "client-1",
	}
	conformance.RunSpotOrderSuite(t, conformance.SpotOrderScenario{
		Client: adapter, Exchange: model.ExchangeKraken, Request: request,
		OrderID: "ORDER-42", ClientOrderID: "client-1", NativeMarket: "XBTUSDT",
		Status: unified.OrderStatusAcknowledged,
	})
}

func TestUnifiedKrakenExtendedBalanceAndOrderMappings(t *testing.T) {
	t.Parallel()

	var detail ExtendedBalanceDetail
	if err := json.Unmarshal([]byte(`{"balance":10.25,"credit":"1.00","credit_used":0.25,"hold_trade":"2.5"}`), &detail); err != nil {
		t.Fatalf("ExtendedBalanceDetail.UnmarshalJSON() error = %v", err)
	}
	available, err := krakenAvailableBalance(detail)
	if err != nil || available != "8.50" {
		t.Fatalf("krakenAvailableBalance() = %q, error = %v", available, err)
	}
	if _, err := krakenAvailableBalance(ExtendedBalanceDetail{
		Balance: "1", HoldTrade: "2",
	}); err == nil {
		t.Fatal("negative Kraken available balance error = nil")
	}
	request := PlaceOrderRequest{
		Pair: "XBTUSD", Side: SideBuy, OrderType: OrderTypeLimit,
		TimeInForce: TimeInForceFOK, Volume: "0.01", Price: "64000",
	}
	if err := request.validate(); err != nil || request.values().Get("timeinforce") != "FOK" {
		t.Fatalf("FOK request values = %v, error = %v", request.values(), err)
	}
	request = PlaceOrderRequest{
		Pair: "XBTUSD", Side: SideBuy, OrderType: OrderTypeMarket,
		Volume: "100", VolumeInQuote: true,
	}
	if err := request.validate(); err != nil || request.values().Get("oflags") != "viqc" {
		t.Fatalf("quote volume request values = %v, error = %v", request.values(), err)
	}
	market, err := fromKrakenWebSocketPair("XBT/USD")
	if err != nil || market != (unified.Market{Base: "BTC", Quote: "USD"}) {
		t.Fatalf("fromKrakenWebSocketPair() = %+v, error = %v", market, err)
	}
	if normalizeKrakenAsset("XXDG.F") != "DOGE.F" {
		t.Fatalf("normalizeKrakenAsset() = %q", normalizeKrakenAsset("XXDG.F"))
	}
	order := fromKrakenOrder(Order{
		TransactionID: "ORDER-1", Status: "open", ExecutedVolume: "0.004", Volume: "0.01",
		Description: OrderDescription{Pair: "XBTUSD", Side: SideSell, OrderType: OrderTypeLimit, Price: "64000"},
	}, unified.Market{Base: "BTC", Quote: "USD"})
	if order.Status != unified.OrderStatusPartiallyFilled || order.Side != unified.SideSell ||
		order.Type != unified.OrderTypeLimit || order.ExecutedQuantity != "0.004" {
		t.Fatalf("fromKrakenOrder() = %+v", order)
	}
}

func TestUnifiedKrakenOpenOrdersMapsAllMarketsOnSameRoute(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case publicPrefix + "AssetPairs":
			_, _ = io.WriteString(writer, `{"error":[],"result":{"XXBTZUSD":{"altname":"XBTUSD","wsname":"XBT/USD","status":"online"}}}`)
		case privatePrefix + "OpenOrders":
			if !verifyUnifiedKrakenRequest(t, request, secret) {
				http.Error(writer, `{"error":["EAPI:Invalid signature"]}`, http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(writer, `{"error":[],"result":{"open":{"ORDER-1":{"cl_ord_id":"client-1","status":"open","descr":{"pair":"XXBTZUSD","type":"buy","ordertype":"limit","price":"64000"},"vol":"0.01","vol_exec":"0"}}}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newTestClient(
		t, server.URL, sender, &recordingProvider{secret: secret},
		[]transport.EgressRouteID{"route-a", "route-b"}, fixedNow,
	)
	adapter, _ := NewUnifiedSpot(native)
	orders, err := adapter.OpenOrders(
		t.Context(), unified.OpenOrdersRequest{AllMarkets: true}, trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("OpenOrders() error = %v", err)
	}
	if len(orders) != 1 || orders[0].Market != (unified.Market{Base: "BTC", Quote: "USD"}) ||
		orders[0].NativeMarket != "XXBTZUSD" || orders[0].Status != unified.OrderStatusNew {
		t.Fatalf("OpenOrders() = %+v", orders)
	}
	if routes := sender.snapshot(); len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedKrakenTimestampMapping(t *testing.T) {
	t.Parallel()

	timestamp, err := krakenSecondsToMillis("1700000000.2509")
	if err != nil || timestamp != 1_700_000_000_250 {
		t.Fatalf("krakenSecondsToMillis() = %d, error = %v", timestamp, err)
	}
	if _, err := krakenSecondsToMillis("invalid"); err == nil {
		t.Fatal("invalid Kraken timestamp error = nil")
	}
}

func TestNewUnifiedSpotRejectsNilClient(t *testing.T) {
	t.Parallel()

	if _, err := NewUnifiedSpot(nil); err == nil {
		t.Fatal("NewUnifiedSpot(nil) error = nil")
	}
}

func verifyUnifiedKrakenRequest(t *testing.T, request *http.Request, secret []byte) bool {
	t.Helper()
	_, valid := unifiedKrakenRequestValues(t, request, secret)
	return valid
}

func unifiedKrakenRequestValues(
	t *testing.T,
	request *http.Request,
	secret []byte,
) (url.Values, bool) {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read Kraken request body: %v", err)
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("parse Kraken request body: %v", err)
	}
	nonce := values.Get("nonce")
	expected, err := SignREST(request.URL.Path, nonce, string(body), secret)
	if err != nil {
		t.Fatalf("sign expected Kraken request: %v", err)
	}
	valid := request.Header.Get("API-Key") == "test-api-key" &&
		request.Header.Get("API-Sign") == expected && nonce != ""
	return values, valid
}

func TestTradeHistoryPagesBuildCandlesBeyondOHLCWindow(t *testing.T) {
	t.Parallel()

	pages := map[string]string{
		"1388534400000000000": `{"error":[],"result":{"XXBTZUSD":[["730.10","0.5",1388534460.123,"b","l","",1],["731.00","0.25",1388534461.5,"s","m","",2]],"last":"1388534461500000000"}}`,
		"1388534461500000000": `{"error":[],"result":{"XXBTZUSD":[["729.5","1",1388534519.999,"s","m","",3],["732","2",1388534520,"b","l","",4]],"last":"1388534520000000000"}}`,
		"1388534520000000000": `{"error":[],"result":{"XXBTZUSD":[],"last":"1388534520000000000"}}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		page, exists := pages[request.URL.Query().Get("since")]
		if request.URL.Path != publicPrefix+"Trades" || !exists {
			http.Error(writer, `{"error":["EGeneral:Invalid arguments"]}`, http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(writer, page)
	}))
	defer server.Close()

	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, time.Unix(1_700_000_000, 0),
	)
	builder, err := unified.NewTradeCandleBuilder(time.Minute, unified.TradeOrderAscending)
	if err != nil {
		t.Fatalf("NewTradeCandleBuilder() error = %v", err)
	}
	var candles []unified.TradeCandle
	since := "1388534400000000000"
	for {
		page, pageErr := client.RecentTrades(
			context.Background(), RecentTradesRequest{Pair: "XBTUSD", Since: since, Count: 1000},
		)
		if pageErr != nil {
			t.Fatalf("RecentTrades(since=%s) error = %v", since, pageErr)
		}
		if len(page.Trades) == 0 {
			break
		}
		for _, item := range page.Trades {
			tick, convertErr := item.Unified()
			if convertErr != nil {
				t.Fatalf("Unified() error = %v", convertErr)
			}
			candle, done, addErr := builder.Add(tick)
			if addErr != nil {
				t.Fatalf("Add() error = %v", addErr)
			}
			if done {
				candles = append(candles, candle)
			}
		}
		since = page.Last
	}
	if last, ok := builder.Flush(); ok {
		candles = append(candles, last)
	}
	if len(candles) != 2 {
		t.Fatalf("candles = %+v, want two", candles)
	}
	first, second := candles[0], candles[1]
	if first.StartTime != 1_388_534_460_000 || first.Open != "730.10" || first.High != "731.00" ||
		first.Low != "729.5" || first.Close != "729.5" || first.Volume != "1.75" || first.TradeCount != 3 {
		t.Fatalf("first candle = %+v", first)
	}
	if second.StartTime != 1_388_534_520_000 || second.Open != "732" || second.Volume != "2" ||
		second.QuoteVolume != "1464" {
		t.Fatalf("second candle = %+v", second)
	}
}
