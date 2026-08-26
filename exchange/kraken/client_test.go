package kraken

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

type directSender struct {
	mu     sync.Mutex
	routes []transport.EgressRouteID
}

type errorSender struct {
	err error
}

func (sender errorSender) Do(
	context.Context,
	transport.EgressRouteID,
	*http.Request,
) (*http.Response, error) {
	return nil, sender.err
}

func (sender *directSender) Do(
	ctx context.Context,
	routeID transport.EgressRouteID,
	request *http.Request,
) (*http.Response, error) {
	sender.mu.Lock()
	sender.routes = append(sender.routes, routeID)
	sender.mu.Unlock()
	return http.DefaultClient.Do(request.Clone(ctx))
}

func (sender *directSender) snapshot() []transport.EgressRouteID {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	return slices.Clone(sender.routes)
}

type recordingProvider struct {
	mu     sync.Mutex
	calls  int
	issued [][]byte
	secret []byte
}

func (provider *recordingProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.calls++
	material := credential.Material{
		APIKey: []byte("test-api-key"), SecretKey: cloneBytes(provider.secret),
	}
	provider.issued = append(provider.issued, material.APIKey, material.SecretKey)
	return material, nil
}

func (provider *recordingProvider) snapshot() (int, [][]byte) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls, slices.Clone(provider.issued)
}

func TestClientSpotLifecycleAndPerRequestRoute(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	var nonceMu sync.Mutex
	var nonces []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(request.URL.Path, privatePrefix) {
			body, _ := io.ReadAll(request.Body)
			values, parseErr := url.ParseQuery(string(body))
			if parseErr != nil {
				http.Error(writer, `{"error":["EGeneral:Invalid arguments"]}`, http.StatusBadRequest)
				return
			}
			nonce := values.Get("nonce")
			expected, signErr := SignREST(request.URL.Path, nonce, string(body), secret)
			if signErr != nil || request.Header.Get("API-Key") != "test-api-key" ||
				request.Header.Get("API-Sign") != expected {
				http.Error(writer, `{"error":["EAPI:Invalid signature"]}`, http.StatusUnauthorized)
				return
			}
			nonceMu.Lock()
			nonces = append(nonces, nonce)
			nonceMu.Unlock()
		}
		switch request.URL.Path {
		case publicPrefix + "Time":
			_, _ = io.WriteString(writer, `{"error":[],"result":{"unixtime":1700000000,"rfc1123":"Tue, 14 Nov 23 22:13:20 +0000"}}`)
		case publicPrefix + "AssetPairs":
			_, _ = io.WriteString(writer, `{"error":[],"result":{"XXBTZUSD":{"altname":"XBTUSD","wsname":"XBT/USD","base":"XXBT","quote":"ZUSD","lot":"unit","pair_decimals":1,"lot_decimals":8,"ordermin":"0.0001","costmin":"0.5","tick_size":"0.1","status":"online"}}}`)
		case publicPrefix + "Ticker":
			_, _ = io.WriteString(writer, `{"error":[],"result":{"XXBTZUSD":{"a":["64001","1","1"],"b":["63999","1","1"],"c":["64000","0.01"],"v":["10","20"],"p":["63000","63500"],"t":[100,200],"l":["62000","61000"],"h":["65000","66000"],"o":"62500"}}}`)
		case publicPrefix + "Depth":
			_, _ = io.WriteString(writer, `{"error":[],"result":{"XXBTZUSD":{"asks":[["64001","2",1700000000]],"bids":[["63999","1",1700000001]]}}}`)
		case publicPrefix + "Trades":
			_, _ = io.WriteString(writer, `{"error":[],"result":{"XXBTZUSD":[["64000","0.01",1700000000.25,"b","m","",61044952]],"last":"1700000000250000000"}}`)
		case publicPrefix + "OHLC":
			_, _ = io.WriteString(writer, `{"error":[],"result":{"XXBTZUSD":[[1699999980,"63900","64100","63800","64000","63950","10",23]],"last":1700000040}}`)
		case privatePrefix + "Balance":
			_, _ = io.WriteString(writer, `{"error":[],"result":{"ZUSD":"900.5","XXBT":"1.25"}}`)
		case privatePrefix + "AddOrder":
			_, _ = io.WriteString(writer, `{"error":[],"result":{"descr":{"order":"buy 0.01 XBTUSD @ limit 64000"},"txid":["ORDER-1"]}}`)
		case privatePrefix + "CancelOrder":
			_, _ = io.WriteString(writer, `{"error":[],"result":{"count":1}}`)
		case privatePrefix + "QueryOrders":
			_, _ = io.WriteString(writer, `{"error":[],"result":{"ORDER-1":{"refid":"None","status":"open","descr":{"pair":"XBTUSD","type":"buy","ordertype":"limit","price":"64000"},"vol":"0.01","vol_exec":"0"}}}`)
		case privatePrefix + "OpenOrders":
			_, _ = io.WriteString(writer, `{"error":[],"result":{"open":{"ORDER-1":{"status":"open","descr":{"pair":"XBTUSD","type":"buy","ordertype":"limit"},"vol":"0.01"}}}}`)
		case privatePrefix + "ClosedOrders":
			_, _ = io.WriteString(writer, `{"error":[],"result":{"closed":{"ORDER-0":{"status":"closed","descr":{"pair":"XBTUSD","type":"sell","ordertype":"market"},"vol":"0.02","vol_exec":"0.02"}},"count":1}}`)
		case privatePrefix + "TradesHistory":
			_, _ = io.WriteString(writer, `{"error":[],"result":{"trades":{"TRADE-1":{"ordertxid":"ORDER-0","pair":"XBTUSD","time":1700000000.5,"type":"sell","ordertype":"market","price":"64000","cost":"1280","fee":"1","vol":"0.02","trade_id":42}},"count":1}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{secret: secret}
	client, limiter := newTestClient(
		t, server.URL, sender, provider, []transport.EgressRouteID{"route-a", "route-b"}, fixedNow,
	)
	serverTime, err := client.ServerTime(context.Background())
	if err != nil || serverTime.UnixTime != fixedNow.Unix() || len(serverTime.Raw) == 0 {
		t.Fatalf("ServerTime() = %+v, error = %v", serverTime, err)
	}
	pairs, err := client.AssetPairs(
		context.Background(), AssetPairsRequest{Pairs: []string{"XBTUSD"}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || len(pairs) != 1 || pairs[0].ID != "XXBTZUSD" || pairs[0].MinimumCost != "0.5" {
		t.Fatalf("AssetPairs() = %+v, error = %v", pairs, err)
	}
	tickers, err := client.Tickers(context.Background(), TickersRequest{Pairs: []string{"XBTUSD"}})
	if err != nil || len(tickers) != 1 || tickers[0].LastPrice != "64000" || tickers[0].Trades24Hours != 200 {
		t.Fatalf("Tickers() = %+v, error = %v", tickers, err)
	}
	book, err := client.OrderBook(context.Background(), OrderBookRequest{Pair: "XBTUSD", Count: 10})
	if err != nil || len(book.Bids) != 1 || book.Bids[0].Price != "63999" || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := client.RecentTrades(
		context.Background(), RecentTradesRequest{Pair: "XBTUSD", Count: 10},
	)
	if err != nil || len(trades.Trades) != 1 || trades.Trades[0].TradeID != 61044952 || trades.Last == "" {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	candles, err := client.Candles(
		context.Background(), CandlesRequest{Pair: "XBTUSD", Interval: Candle1Minute},
	)
	if err != nil || len(candles.Items) != 1 || candles.Items[0].Close != "64000" || candles.Last != 1700000040 {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	balance, err := client.Balance(context.Background())
	if err != nil || balance.Amounts["ZUSD"] != "900.5" || len(balance.Raw) == 0 {
		t.Fatalf("Balance() = %+v, error = %v", balance, err)
	}
	reference, err := client.PlaceOrder(
		context.Background(),
		PlaceOrderRequest{
			Pair: "XBTUSD", Side: SideBuy, OrderType: OrderTypeLimit,
			Volume: "0.01", Price: "64000", ClientOrderID: "strategy-1",
		},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || len(reference.TransactionIDs) != 1 || reference.TransactionIDs[0] != "ORDER-1" {
		t.Fatalf("PlaceOrder() = %+v, error = %v", reference, err)
	}
	canceled, err := client.CancelOrder(
		context.Background(), CancelOrderRequest{TransactionID: "ORDER-1", Pair: "XBTUSD"},
	)
	if err != nil || canceled.Count != 1 || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	orders, err := client.OrderInfo(
		context.Background(), OrderInfoRequest{TransactionIDs: []string{"ORDER-1"}},
	)
	if err != nil || len(orders) != 1 || orders[0].TransactionID != "ORDER-1" || len(orders[0].Raw) == 0 {
		t.Fatalf("OrderInfo() = %+v, error = %v", orders, err)
	}
	open, err := client.OpenOrders(context.Background(), OpenOrdersRequest{})
	if err != nil || open.Count != 1 || open.Orders[0].Status != "open" {
		t.Fatalf("OpenOrders() = %+v, error = %v", open, err)
	}
	closed, err := client.ClosedOrders(context.Background(), ClosedOrdersRequest{CloseTime: CloseTimeClose})
	if err != nil || closed.Count != 1 || closed.Orders[0].ExecutedVolume != "0.02" {
		t.Fatalf("ClosedOrders() = %+v, error = %v", closed, err)
	}
	history, err := client.TradesHistory(
		context.Background(), TradesHistoryRequest{Type: TradeHistoryAll},
	)
	if err != nil || history.Count != 1 || history.Trades[0].OrderID != "ORDER-0" || history.Trades[0].TradeID != 42 {
		t.Fatalf("TradesHistory() = %+v, error = %v", history, err)
	}

	routes := sender.snapshot()
	if len(routes) != 13 || routes[1] != "route-b" || routes[7] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	calls, issued := provider.snapshot()
	if calls != 7 {
		t.Fatalf("provider calls = %d, want 7", calls)
	}
	for _, secretBytes := range issued {
		if !allZero(secretBytes) {
			t.Fatal("resolved credential byte slices were not overwritten")
		}
	}
	nonceMu.Lock()
	for index := 1; index < len(nonces); index++ {
		if nonces[index] <= nonces[index-1] {
			t.Fatalf("nonces are not increasing: %v", nonces)
		}
	}
	nonceMu.Unlock()
	privateSnapshot, err := limiter.Snapshot("kraken:account:kraken-main:private-counter")
	if err != nil || privateSnapshot.Used != 11 {
		t.Fatalf("private limiter snapshot = %+v, error = %v", privateSnapshot, err)
	}
	tradingSnapshot, err := limiter.Snapshot("kraken:account:kraken-main:trading:XBTUSD:1second")
	if err != nil || tradingSnapshot.Used != 2 {
		t.Fatalf("trading limiter snapshot = %+v, error = %v", tradingSnapshot, err)
	}
}

func TestClientRejectsUnauthorizedRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{secret: []byte("dGVzdA==")}
	client, _ := newTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, time.Now(),
	)
	_, err := client.Balance(context.Background(), trade.WithEgressRoute("route-b"))
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Balance() error = %v, want authorization", err)
	}
	calls, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestClientClassifiesKrakenEnvelopeError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"error":["EOrder:Insufficient funds"],"result":{}}`)
	}))
	defer server.Close()
	provider := &recordingProvider{secret: []byte("dGVzdA==")}
	client, _ := newTestClient(
		t, server.URL, &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, time.Now(),
	)
	_, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		Pair: "XBTUSD", Side: SideBuy, OrderType: OrderTypeMarket, Volume: "1",
	})
	if !errors.Is(err, trade.ErrInsufficientBalance) {
		t.Fatalf("PlaceOrder() error = %v, want insufficient balance", err)
	}
}

func TestClientClassifiesOrderTransportFailureAsUnknownState(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{secret: []byte("dGVzdA==")}
	client, _ := newTestClient(
		t, "http://kraken.example.test", errorSender{err: errors.New("connection reset")}, provider,
		[]transport.EgressRouteID{"route-a"}, time.Now(),
	)
	_, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		Pair: "XBTUSD", Side: SideBuy, OrderType: OrderTypeMarket, Volume: "1",
	})
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() error = %v, want unknown execution state", err)
	}
}

func TestClientNonceIsUniqueAcrossConcurrentRequests(t *testing.T) {
	t.Parallel()

	client := &Client{now: func() time.Time { return time.UnixMilli(1_700_000_000_000) }}
	const count = 200
	values := make(chan uint64, count)
	var wait sync.WaitGroup
	for range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			value, err := strconv.ParseUint(client.nextNonce(), 10, 64)
			if err != nil {
				t.Errorf("parse nonce: %v", err)
				return
			}
			values <- value
		}()
	}
	wait.Wait()
	close(values)
	got := make([]uint64, 0, count)
	for value := range values {
		got = append(got, value)
	}
	if len(got) != count {
		t.Fatalf("nonce count = %d, want %d", len(got), count)
	}
	sort.Slice(got, func(left, right int) bool { return got[left] < got[right] })
	for index := 1; index < len(got); index++ {
		if got[index] != got[index-1]+1 {
			t.Fatalf("nonces are not contiguous near %d: %v", index, got[index-1:index+1])
		}
	}
}

func TestKrakenRequestValidation(t *testing.T) {
	t.Parallel()

	invalid := []error{
		(OrderBookRequest{}).validate(),
		(RecentTradesRequest{Pair: "XBTUSD", Count: 1001}).validate(),
		(CandlesRequest{Pair: "XBTUSD", Interval: 2}).validate(),
		(PlaceOrderRequest{Pair: "XBTUSD", Side: SideBuy, OrderType: OrderTypeLimit, Volume: "1"}).validate(),
		(CancelOrderRequest{TransactionID: "ORDER-1"}).validate(),
		(CancelOrderRequest{
			TransactionID: "ORDER-1", ClientOrderID: "strategy-1", Pair: "XBTUSD",
		}).validate(),
		(OrderInfoRequest{TransactionIDs: []string{"bad id"}}).validate(),
		(ClosedOrdersRequest{Offset: -1}).validate(),
	}
	for index, err := range invalid {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v, want validation", index, err)
		}
	}
	cancelValues := (CancelOrderRequest{
		ClientOrderID: "strategy-1", Pair: "XBTUSD",
	}).values()
	if cancelValues.Get("cl_ord_id") != "strategy-1" || cancelValues.Get("txid") != "" {
		t.Fatalf("client order cancel values = %v", cancelValues)
	}
}

func newTestClient(
	t *testing.T,
	baseURL string,
	sender commonexchange.Sender,
	provider credential.Provider,
	allowedRoutes []transport.EgressRouteID,
	now time.Time,
) (*Client, *ratelimit.Limiter) {
	t.Helper()
	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: sender, Limiter: limiter})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	client, err := New(Config{
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "kraken-main", Exchange: model.ExchangeKraken,
			SecretRef:             "secret/kraken/main",
			Permissions:           []credential.Permission{credential.PermissionRead, credential.PermissionTrade},
			AllowedEgressRouteIDs: allowedRoutes,
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
		PublicRequestsPerSecond: 100, PrivateCounterLimit: 100,
		PrivateCounterWindow: time.Second, TradingRequestsPerSecond: 100,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}
