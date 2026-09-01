package coinbase

import (
	"bytes"
	"crypto/elliptic"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

	privateKey, secret := newTestECKey(t, elliptic.P256())
	keyName := "organizations/test/apiKeys/main"
	fixedNow := time.Unix(1_700_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case publicPrefix + "/market/products/BTC-USDT":
			if request.Header.Get("Authorization") != "" {
				http.Error(writer, `{"error":"INVALID_ARGUMENT","message":"unexpected public token"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"product_id":"BTC-USDT","price":"64000.10","product_type":"SPOT","base_currency_id":"BTC","quote_currency_id":"USDT"}`)
		case publicPrefix + "/accounts":
			if !verifyRequestJWT(t, request, &privateKey.PublicKey, keyName, fixedNow) ||
				request.URL.Query().Get("limit") != "250" {
				http.Error(writer, `{"error":"UNAUTHENTICATED","message":"bad signed balance mapping"}`, http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(writer, `{"accounts":[{"uuid":"account-1","currency":"USDT","available_balance":{"value":"1000.00","currency":"USDT"},"hold":{"value":"2.00","currency":"USDT"}}],"has_next":false,"cursor":"","size":1}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newTestClient(
		t, server.URL, sender, &recordingProvider{keyName: keyName, secret: secret},
		[]transport.EgressRouteID{"route-a", "route-b"}, fixedNow,
	)
	adapter, err := NewUnifiedSpot(native)
	if err != nil {
		t.Fatalf("NewUnifiedSpot() error = %v", err)
	}
	conformance.RunSpotReadSuite(t, conformance.SpotReadScenario{
		Client: adapter, Exchange: model.ExchangeCoinbase,
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

	privateKey, secret := newTestECKey(t, elliptic.P256())
	keyName := "organizations/test/apiKeys/main"
	fixedNow := time.Unix(1_700_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != publicPrefix+"/orders" || request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		if !verifyRequestJWT(t, request, &privateKey.PublicKey, keyName, fixedNow) {
			http.Error(writer, `{"error":"UNAUTHENTICATED","message":"error sign"}`, http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"client_order_id":"client-1","product_id":"BTC-USDT","side":"BUY","order_configuration":{"market_market_ioc":{"quote_size":"100"}}}` {
			http.Error(writer, `{"error":"INVALID_ARGUMENT","message":"bad common mapping"}`, http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(writer, `{"success":true,"success_response":{"order_id":"42","product_id":"BTC-USDT","side":"BUY","client_order_id":"client-1"}}`)
	}))
	defer server.Close()

	native, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{keyName: keyName, secret: secret},
		[]transport.EgressRouteID{"route-a"}, fixedNow,
	)
	adapter, _ := NewUnifiedSpot(native)
	request := unified.PlaceOrderRequest{
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, Side: unified.SideBuy,
		Type: unified.OrderTypeMarket, QuoteAmount: "100", ClientOrderID: "client-1",
	}
	conformance.RunSpotOrderSuite(t, conformance.SpotOrderScenario{
		Client: adapter, Exchange: model.ExchangeCoinbase, Request: request,
		OrderID: "42", ClientOrderID: "client-1", NativeMarket: "BTC-USDT",
		Status: unified.OrderStatusAcknowledged,
	})
}

func TestUnifiedCoinbaseThreeMinuteCandleAggregation(t *testing.T) {
	t.Parallel()

	native := []Candle{
		{Start: "300", Open: "5", High: "6", Low: "4", Close: "5.5", Volume: "0.003"},
		{Start: "180", Open: "2", High: "4", Low: "1", Close: "3", Volume: "1.1"},
		{Start: "360", Open: "7", High: "8", Low: "6", Close: "7.5", Volume: "4"},
		{Start: "240", Open: "3", High: "5", Low: "2", Close: "4", Volume: "2.20"},
		{Start: "240", Open: "3", High: "5", Low: "2", Close: "4", Volume: "2.20"},
	}
	candles, err := aggregateCoinbaseThreeMinuteCandles(native, 2)
	if err != nil {
		t.Fatalf("aggregateCoinbaseThreeMinuteCandles() error = %v", err)
	}
	if len(candles) != 2 || candles[0].StartTime != 360000 || candles[0].Volume != "4" ||
		candles[1].StartTime != 180000 || candles[1].Open != "2" || candles[1].High != "6" ||
		candles[1].Low != "1" || candles[1].Close != "5.5" || candles[1].Volume != "3.303" {
		t.Fatalf("aggregateCoinbaseThreeMinuteCandles() = %+v", candles)
	}
	if _, err := aggregateCoinbaseThreeMinuteCandles([]Candle{{
		Start: "180", Open: "1", High: "invalid", Low: "1", Close: "1", Volume: "1",
	}}, 1); err == nil {
		t.Fatal("invalid Coinbase candle decimal error = nil")
	}
}

func TestUnifiedCoinbaseThreeMinuteCandlesUseSameRouteAcrossPages(t *testing.T) {
	t.Parallel()

	fixedNow := time.Unix(36_000, 0)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path != publicPrefix+"/market/products/BTC-USDT/candles" ||
			request.URL.Query().Get("granularity") != "ONE_MINUTE" {
			http.Error(writer, `{"error":"INVALID_ARGUMENT","message":"bad candle mapping"}`, http.StatusBadRequest)
			return
		}
		switch calls.Add(1) {
		case 1:
			if request.URL.Query().Get("start") != "0" || request.URL.Query().Get("end") != "21000" ||
				request.URL.Query().Get("limit") != "350" {
				http.Error(writer, `{"error":"INVALID_ARGUMENT","message":"bad first candle page"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"candles":[{"start":"0","low":"1","high":"2","open":"1","close":"2","volume":"3"}]}`)
		case 2:
			if request.URL.Query().Get("start") != "21000" || request.URL.Query().Get("end") != "36000" ||
				request.URL.Query().Get("limit") != "250" {
				http.Error(writer, `{"error":"INVALID_ARGUMENT","message":"bad second candle page"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"candles":[{"start":"35940","low":"4","high":"5","open":"4","close":"5","volume":"6"}]}`)
		default:
			http.Error(writer, `{"error":"INVALID_ARGUMENT","message":"too many candle pages"}`, http.StatusBadRequest)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	native, _ := newTestClient(
		t, server.URL, sender, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"}, fixedNow,
	)
	adapter, _ := NewUnifiedSpot(native)
	candles, err := adapter.Candles(t.Context(), unified.CandlesRequest{
		Market:   unified.Market{Base: "BTC", Quote: "USDT"},
		Interval: unified.Candle3Minutes, Limit: 200,
	}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("Candles() error = %v", err)
	}
	if len(candles) != 2 || candles[0].StartTime != 35_820_000 || candles[1].StartTime != 0 {
		t.Fatalf("Candles() = %+v", candles)
	}
	if routes := sender.snapshot(); len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestUnifiedCoinbaseOrderConfigurationAndStatusMapping(t *testing.T) {
	t.Parallel()

	configuration := OrderConfiguration{
		SORLimitIOC: &SORLimitIOCConfiguration{BaseSize: "0.01", LimitPrice: "64000"},
	}
	if err := configuration.validate(); err != nil {
		t.Fatalf("SORLimitIOC validate() error = %v", err)
	}
	encoded, err := json.Marshal(configuration)
	if err != nil || string(encoded) != `{"sor_limit_ioc":{"base_size":"0.01","limit_price":"64000"}}` {
		t.Fatalf("SORLimitIOC JSON = %s, error = %v", encoded, err)
	}
	configuration = OrderConfiguration{
		LimitLimitFOK: &LimitFOKConfiguration{BaseSize: "0.01", LimitPrice: "64000"},
	}
	if err := configuration.validate(); err != nil {
		t.Fatalf("LimitLimitFOK validate() error = %v", err)
	}
	market := unified.Market{Base: "BTC", Quote: "USDT"}
	native := Order{
		OrderID: "42", ClientOrderID: "client-1", ProductID: "BTC-USDT",
		Side: SideSell, Status: "OPEN", FilledSize: "0.004",
		OrderConfiguration: OrderConfiguration{LimitLimitFOK: &LimitFOKConfiguration{
			BaseSize: "0.01", LimitPrice: "64000",
		}},
	}
	got := fromCoinbaseOrder(native, market)
	if got.Side != unified.SideSell || got.Type != unified.OrderTypeLimit ||
		got.Status != unified.OrderStatusPartiallyFilled || got.Price != "64000" ||
		got.Quantity != "0.01" || got.ExecutedQuantity != "0.004" {
		t.Fatalf("fromCoinbaseOrder() = %+v", got)
	}
}

func TestUnifiedCoinbaseGeneratesMissingClientOrderID(t *testing.T) {
	t.Parallel()

	_, secret := newTestECKey(t, elliptic.P256())
	native, _ := newTestClient(
		t, "http://127.0.0.1", &directSender{},
		&recordingProvider{keyName: "organizations/test/apiKeys/main", secret: secret},
		[]transport.EgressRouteID{"route-a"}, time.Unix(1_700_000_000, 0),
	)
	native.random = bytes.NewReader(make([]byte, 16))
	adapter, _ := NewUnifiedSpot(native)
	got, err := adapter.coinbaseClientOrderID("")
	if err != nil || got != "proven-00000000000000000000000000000000" {
		t.Fatalf("coinbaseClientOrderID() = %q, error = %v", got, err)
	}
}

func TestNewUnifiedSpotRejectsNilClient(t *testing.T) {
	t.Parallel()

	if _, err := NewUnifiedSpot(nil); err == nil {
		t.Fatal("NewUnifiedSpot(nil) error = nil")
	}
}
