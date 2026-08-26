package binance

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/transport"
)

func TestClientMarketDataEndpoints(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v3/depth":
			if request.URL.Query().Get("symbol") != "BTCUSDT" || request.URL.Query().Get("limit") != "1001" {
				http.Error(writer, `{"code":-1102,"msg":"Bad parameters."}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"lastUpdateId":7,"bids":[["64000.00","1.5"]],"asks":[["64001.00","2.0"]]}`)
		case "/api/v3/trades":
			_, _ = io.WriteString(writer, `[{"id":9,"price":"64000.00","qty":"0.1","quoteQty":"6400.00","time":1700000000000,"isBuyerMaker":true,"isBestMatch":true}]`)
		case "/api/v3/ticker/bookTicker":
			_, _ = io.WriteString(writer, `{"symbol":"BTCUSDT","bidPrice":"64000.00","bidQty":"1.5","askPrice":"64001.00","askQty":"2.0"}`)
		case "/api/v3/klines":
			if request.URL.Query().Get("interval") != "1m" || request.URL.Query().Get("timeZone") != "+09:00" {
				http.Error(writer, `{"code":-1102,"msg":"Bad parameters."}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[[1700000000000,"1.0","2.0","0.5","1.5","10.0",1700000059999,"15.0",8,"6.0","9.0","0"]]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, limiter := newTestClient(
		t,
		server.URL,
		&directSender{},
		&recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"},
		nil,
	)

	book, err := client.OrderBook(
		context.Background(),
		OrderBookRequest{Symbol: "BTCUSDT", Limit: 1001},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("OrderBook() error = %v", err)
	}
	if book.LastUpdateID != 7 || len(book.Bids) != 1 || book.Bids[0].Price != "64000.00" || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v", book)
	}

	trades, err := client.RecentTrades(context.Background(), RecentTradesRequest{Symbol: "BTCUSDT", Limit: 1}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("RecentTrades() error = %v", err)
	}
	if len(trades) != 1 || trades[0].ID != 9 || !trades[0].BuyerMaker {
		t.Fatalf("RecentTrades() = %+v", trades)
	}

	ticker, err := client.BookTicker(context.Background(), BookTickerRequest{Symbol: "BTCUSDT"}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("BookTicker() error = %v", err)
	}
	if ticker.AskPrice != "64001.00" || len(ticker.Raw) == 0 {
		t.Fatalf("BookTicker() = %+v", ticker)
	}

	start := time.UnixMilli(1700000000000)
	klines, err := client.Klines(context.Background(), KlinesRequest{
		Symbol:    "BTCUSDT",
		Interval:  Kline1Minute,
		StartTime: &start,
		TimeZone:  "+09:00",
		Limit:     1,
	}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("Klines() error = %v", err)
	}
	if len(klines) != 1 || klines[0].Open != "1.0" || klines[0].TradeCount != 8 {
		t.Fatalf("Klines() = %+v", klines)
	}

	snapshot, err := limiter.Snapshot("binance:route:route-b:request_weight:1minute")
	if err != nil {
		t.Fatalf("limiter.Snapshot() error = %v", err)
	}
	if snapshot.Used != 279 {
		t.Fatalf("request weight = %d, want 279", snapshot.Used)
	}
}

func TestClientOrderListEndpoints(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret-key")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if !verifySignedRequest(t, request, secret) {
			http.Error(writer, `{"code":-1022,"msg":"Signature invalid."}`, http.StatusBadRequest)
			return
		}
		switch request.URL.Path {
		case "/api/v3/openOrders":
			if request.URL.Query().Get("symbol") != "" {
				http.Error(writer, `{"code":-1102,"msg":"Unexpected symbol."}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[{"symbol":"BTCUSDT","orderId":11,"clientOrderId":"open-11","status":"NEW","type":"LIMIT","side":"BUY"}]`)
		case "/api/v3/allOrders":
			if request.URL.Query().Get("symbol") != "BTCUSDT" || request.URL.Query().Get("limit") != "10" {
				http.Error(writer, `{"code":-1102,"msg":"Bad parameters."}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[{"symbol":"BTCUSDT","orderId":12,"clientOrderId":"done-12","status":"FILLED","type":"LIMIT","side":"SELL"}]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, limiter := newTestClient(
		t,
		server.URL,
		&directSender{},
		&recordingProvider{},
		[]transport.EgressRouteID{"route-a"},
		nil,
	)
	openOrders, err := client.OpenOrders(context.Background(), OpenOrdersRequest{AllSymbols: true})
	if err != nil {
		t.Fatalf("OpenOrders() error = %v", err)
	}
	if len(openOrders) != 1 || openOrders[0].OrderID != 11 || len(openOrders[0].Raw) == 0 {
		t.Fatalf("OpenOrders() = %+v", openOrders)
	}

	allOrders, err := client.AllOrders(context.Background(), AllOrdersRequest{Symbol: "BTCUSDT", Limit: 10})
	if err != nil {
		t.Fatalf("AllOrders() error = %v", err)
	}
	if len(allOrders) != 1 || allOrders[0].Status != OrderStatusFilled || len(allOrders[0].Raw) == 0 {
		t.Fatalf("AllOrders() = %+v", allOrders)
	}

	snapshot, err := limiter.Snapshot("binance:route:route-a:request_weight:1minute")
	if err != nil {
		t.Fatalf("limiter.Snapshot() error = %v", err)
	}
	if snapshot.Used != 100 {
		t.Fatalf("request weight = %d, want 100", snapshot.Used)
	}
}

func TestOpenOrdersRequiresExplicitAllSymbols(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(
		t,
		"http://127.0.0.1",
		&directSender{},
		&recordingProvider{},
		[]transport.EgressRouteID{"route-a"},
		nil,
	)
	if _, err := client.OpenOrders(context.Background(), OpenOrdersRequest{}); err == nil {
		t.Fatal("OpenOrders() error = nil, want a validation error")
	}
}
