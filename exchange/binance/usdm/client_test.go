package usdm

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/ratelimit"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type directSender struct {
	mu     sync.Mutex
	routes []transport.EgressRouteID
}

func (sender *directSender) Do(ctx context.Context, routeID transport.EgressRouteID, request *http.Request) (*http.Response, error) {
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
	key    []byte
	secret []byte
}

func (provider *recordingProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.calls++
	material := credential.Material{APIKey: []byte("api-key"), SecretKey: []byte("secret-key")}
	provider.key = material.APIKey
	provider.secret = material.SecretKey
	return material, nil
}

func (provider *recordingProvider) snapshot() (int, []byte, []byte) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls, slices.Clone(provider.key), slices.Clone(provider.secret)
}

func TestClientMarketAccountPositionAndOrderLifecycle(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("X-MBX-USED-WEIGHT-1M", "20")
		private := request.URL.Path == "/fapi/v3/account" || request.URL.Path == "/fapi/v3/positionRisk" || request.URL.Path == "/fapi/v1/order" || request.URL.Path == "/fapi/v1/openOrders" || request.URL.Path == "/fapi/v1/allOrders"
		if private && !verifySignedRequest(request, []byte("secret-key"), fixedNow) {
			http.Error(writer, `{"code":-1022,"msg":"Signature invalid."}`, http.StatusBadRequest)
			return
		}
		switch request.URL.Path {
		case "/fapi/v1/exchangeInfo":
			_, _ = io.WriteString(writer, `{"timezone":"UTC","serverTime":1700000000000,"rateLimits":[{"rateLimitType":"REQUEST_WEIGHT","interval":"MINUTE","intervalNum":1,"limit":2400}],"symbols":[{"symbol":"BTCUSDT","pair":"BTCUSDT","contractType":"PERPETUAL","status":"TRADING","baseAsset":"BTC","quoteAsset":"USDT","marginAsset":"USDT","pricePrecision":2,"quantityPrecision":3}]}`)
		case "/fapi/v1/ticker/price":
			_, _ = io.WriteString(writer, `{"symbol":"BTCUSDT","price":"64000.10","time":1700000000000}`)
		case "/fapi/v1/depth":
			_, _ = io.WriteString(writer, `{"lastUpdateId":1,"E":1700000000000,"T":1700000000000,"bids":[["64000","1"]],"asks":[["64001","2"]]}`)
		case "/fapi/v1/trades":
			_, _ = io.WriteString(writer, `[{"id":9,"price":"64000","qty":"0.01","quoteQty":"640","time":1700000000000,"isBuyerMaker":false}]`)
		case "/fapi/v1/klines":
			_, _ = io.WriteString(writer, `[[1700000000000,"1","2","0.5","1.5","10",1700000059999,"15",3,"0","0","0"]]`)
		case "/fapi/v3/account":
			_, _ = io.WriteString(writer, `{"canTrade":true,"totalWalletBalance":"1000","availableBalance":"900","assets":[{"asset":"USDT","walletBalance":"1000","availableBalance":"900"}],"positions":[]}`)
		case "/fapi/v3/positionRisk":
			_, _ = io.WriteString(writer, `[{"symbol":"BTCUSDT","positionSide":"LONG","positionAmt":"0.01","entryPrice":"64000","markPrice":"64100","unRealizedProfit":"1","notional":"641"}]`)
		case "/fapi/v1/order":
			if request.Method == http.MethodPost {
				query := request.URL.Query()
				if query.Get("positionSide") != "LONG" || query.Get("quantity") != "0.01" || query.Get("price") != "64000" {
					http.Error(writer, `{"code":-1102,"msg":"bad order"}`, 400)
					return
				}
				writer.Header().Set("X-MBX-ORDER-COUNT-10S", "1")
				writer.Header().Set("X-MBX-ORDER-COUNT-1M", "1")
			}
			status := "NEW"
			if request.Method == http.MethodDelete {
				status = "CANCELED"
			}
			_, _ = io.WriteString(writer, `{"orderId":42,"symbol":"BTCUSDT","status":"`+status+`","clientOrderId":"strategy-1","price":"64000","origQty":"0.01","executedQty":"0","type":"LIMIT","side":"BUY","positionSide":"LONG"}`)
		case "/fapi/v1/openOrders":
			_, _ = io.WriteString(writer, `[{"orderId":42,"symbol":"BTCUSDT","status":"NEW","clientOrderId":"strategy-1"}]`)
		case "/fapi/v1/allOrders":
			_, _ = io.WriteString(writer, `[{"orderId":41,"symbol":"BTCUSDT","status":"FILLED","clientOrderId":"done-1"}]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newTestClient(t, server.URL, sender, provider, fixedNow, []transport.EgressRouteID{"route-a", "route-b"})
	info, err := client.ExchangeInfo(context.Background(), trade.WithEgressRoute("route-b"))
	if err != nil || len(info.Symbols) != 1 || len(info.Raw) == 0 {
		t.Fatalf("ExchangeInfo() = %+v, error = %v", info, err)
	}
	ticker, err := client.TickerPrice(context.Background(), TickerPriceRequest{Symbol: "BTCUSDT"}, trade.WithEgressRoute("route-b"))
	if err != nil || ticker.Price != "64000.10" {
		t.Fatalf("TickerPrice() = %+v, error = %v", ticker, err)
	}
	book, err := client.OrderBook(context.Background(), OrderBookRequest{Symbol: "BTCUSDT", Limit: 5})
	if err != nil || book.Bids[0].Price != "64000" {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := client.RecentTrades(context.Background(), RecentTradesRequest{Symbol: "BTCUSDT", Limit: 1})
	if err != nil || trades[0].ID != 9 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	candles, err := client.Candles(context.Background(), CandlesRequest{Symbol: "BTCUSDT", Interval: Candle1Minute, Limit: 1})
	if err != nil || candles[0].Close != "1.5" {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	account, err := client.Account(context.Background())
	if err != nil || account.TotalWalletBalance != "1000" || len(account.Raw) == 0 {
		t.Fatalf("Account() = %+v, error = %v", account, err)
	}
	positions, err := client.Positions(context.Background(), PositionsRequest{Symbol: "BTCUSDT"})
	if err != nil || positions[0].PositionAmount != "0.01" || len(positions[0].Raw) == 0 {
		t.Fatalf("Positions() = %+v, error = %v", positions, err)
	}
	order, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{Symbol: "BTCUSDT", Side: SideBuy, PositionSide: PositionSideLong, Type: OrderTypeLimit, TimeInForce: TimeInForceGTC, Quantity: "0.01", Price: "64000", ClientOrderID: "strategy-1"}, trade.WithEgressRoute("route-b"))
	if err != nil || order.OrderID != 42 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", order, err)
	}
	id := int64(42)
	queried, err := client.OrderInfo(context.Background(), OrderInfoRequest{Symbol: "BTCUSDT", OrderID: &id})
	if err != nil || queried.Status != OrderStatusNew {
		t.Fatalf("OrderInfo() = %+v, error = %v", queried, err)
	}
	canceled, err := client.CancelOrder(context.Background(), OrderInfoRequest{Symbol: "BTCUSDT", ClientOrderID: "strategy-1"})
	if err != nil || canceled.Status != OrderStatusCanceled {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	open, err := client.OpenOrders(context.Background(), OpenOrdersRequest{Symbol: "BTCUSDT"})
	if err != nil || len(open) != 1 || len(open[0].Raw) == 0 {
		t.Fatalf("OpenOrders() = %+v, error = %v", open, err)
	}
	history, err := client.OrderHistory(context.Background(), OrderHistoryRequest{Symbol: "BTCUSDT", Limit: 10})
	if err != nil || history[0].Status != OrderStatusFilled {
		t.Fatalf("OrderHistory() = %+v, error = %v", history, err)
	}
	if routes := sender.snapshot(); len(routes) != 12 || routes[0] != "route-b" || routes[1] != "route-b" || routes[7] != "route-b" {
		t.Fatalf("routes = %v", routes)
	}
	calls, key, secret := provider.snapshot()
	if calls != 7 || !allZero(key) || !allZero(secret) {
		t.Fatalf("provider calls = %d, secrets zero = %v/%v", calls, allZero(key), allZero(secret))
	}
	snapshot, err := limiter.Snapshot("binance-usdm:account:futures-main:orders:10seconds")
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("order limiter = %+v, error = %v", snapshot, err)
	}
}

func TestCredentialRouteRejectedBeforeSecretResolution(t *testing.T) {
	t.Parallel()
	sender := &directSender{}
	provider := &recordingProvider{}
	client, _ := newTestClient(t, "http://127.0.0.1", sender, provider, time.Now(), []transport.EgressRouteID{"route-a"})
	_, err := client.Account(context.Background(), trade.WithEgressRoute("route-b"))
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Account() error = %v", err)
	}
	calls, _, _ := provider.snapshot()
	if calls != 0 || len(sender.snapshot()) != 0 {
		t.Fatalf("calls = %d, routes = %v", calls, sender.snapshot())
	}
}

func TestFutures503Classification(t *testing.T) {
	t.Parallel()
	category, retryable := classifyError(503, 0, "Unknown error, please check your request or try again later.", commonexchange.OperationMutation)
	if category != trade.ErrorUnknownExecutionState || retryable {
		t.Fatalf("unknown variant = %s/%v", category, retryable)
	}
	category, retryable = classifyError(503, 0, "Service Unavailable.", commonexchange.OperationMutation)
	if category != trade.ErrorExchangeUnavailable || !retryable {
		t.Fatalf("unavailable variant = %s/%v", category, retryable)
	}
	category, retryable = classifyError(503, -1008, "Request throttled by system-level protection.", commonexchange.OperationMutation)
	if category != trade.ErrorExchangeUnavailable || !retryable {
		t.Fatalf("throttle variant = %s/%v", category, retryable)
	}
}

func newTestClient(t *testing.T, baseURL string, sender *directSender, provider *recordingProvider, now time.Time, routes []transport.EgressRouteID) (*Client, *ratelimit.Limiter) {
	t.Helper()
	limiter, _ := ratelimit.New()
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: sender, Limiter: limiter})
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{Executor: executor, Credentials: &credential.Descriptor{AccountID: "futures-main", Exchange: model.ExchangeBinance, SecretRef: "secret/futures", Permissions: []credential.Permission{credential.PermissionRead, credential.PermissionTrade}, AllowedEgressRouteIDs: routes}, CredentialProvider: provider, DefaultEgressRouteID: "route-a", BaseURL: baseURL, AllowInsecureHTTP: true, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return client, limiter
}

func verifySignedRequest(request *http.Request, secret []byte, now time.Time) bool {
	values, _ := url.ParseQuery(request.URL.RawQuery)
	signature := values.Get("signature")
	values.Del("signature")
	if values.Get("timestamp") != strconv.FormatInt(now.UnixMilli(), 10) || values.Get("recvWindow") != "5000" || request.Header.Get("X-MBX-APIKEY") != "api-key" {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(values.Encode()))
	return hmac.Equal([]byte(signature), []byte(hex.EncodeToString(mac.Sum(nil))))
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}
