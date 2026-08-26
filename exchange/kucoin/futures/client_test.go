package futures

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
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
	mu             sync.Mutex
	calls          int
	lastAPIKey     []byte
	lastSecret     []byte
	lastPassphrase []byte
}

func (provider *recordingProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.calls++
	material := credential.Material{
		APIKey: []byte("test-api-key"), SecretKey: []byte("test-secret"),
		Passphrase: []byte("test-passphrase"),
	}
	provider.lastAPIKey = material.APIKey
	provider.lastSecret = material.SecretKey
	provider.lastPassphrase = material.Passphrase
	return material, nil
}

func (provider *recordingProvider) snapshot() (int, []byte, []byte, []byte) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls, slices.Clone(provider.lastAPIKey), slices.Clone(provider.lastSecret),
		slices.Clone(provider.lastPassphrase)
}

func TestClientFuturesLifecycle(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v1/contracts/active" {
			writer.Header().Set("gw-ratelimit-limit", "2000")
			writer.Header().Set("gw-ratelimit-remaining", "1997")
		}
		if isPrivatePath(request.URL.Path) && !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":"400005","msg":"Invalid signature"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case request.URL.Path == "/api/v1/contracts/active":
			_, _ = io.WriteString(writer, `{"code":"200000","data":[{"symbol":"XBTUSDTM","displaySymbol":"BTC/USDT","rootSymbol":"USDT","type":"FFWCSX","baseCurrency":"XBT","quoteCurrency":"USDT","settleCurrency":"USDT","maxOrderQty":1000000,"marketMaxOrderQty":1000000,"maxPrice":1000000,"lotSize":1,"tickSize":0.1,"indexPriceTickSize":0.01,"multiplier":0.001,"initialMargin":0.01,"maintainMargin":0.005,"makerFeeRate":0.0002,"takerFeeRate":0.0006,"isInverse":false,"isQuanto":true,"status":"Open","fundingFeeRate":0.0001,"fundingRateGranularity":28800000,"currentFundingRateGranularity":28800000,"openInterest":100,"markPrice":64000,"indexPrice":63999,"lastTradePrice":64001,"maxLeverage":100}]}`)
		case request.URL.Path == "/api/v1/contracts/XBTUSDTM":
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"symbol":"XBTUSDTM","displaySymbol":"BTC/USDT","rootSymbol":"USDT","type":"FFWCSX","baseCurrency":"XBT","quoteCurrency":"USDT","settleCurrency":"USDT","maxOrderQty":"1000000","marketMaxOrderQty":"1000000","maxPrice":"1000000","lotSize":"1","tickSize":"0.1","indexPriceTickSize":"0.01","multiplier":"0.001","initialMargin":"0.01","maintainMargin":"0.005","makerFeeRate":"0.0002","takerFeeRate":"0.0006","isInverse":false,"isQuanto":true,"status":"Open","fundingFeeRate":"0.0001","fundingRateGranularity":28800000,"currentFundingRateGranularity":28800000,"openInterest":"100","markPrice":"64000","indexPrice":"63999","lastTradePrice":"64001","maxLeverage":100}}`)
		case request.URL.Path == "/api/v1/ticker":
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"sequence":1,"symbol":"XBTUSDTM","side":"buy","size":2,"tradeId":"trade-1","price":"64000","bestBidPrice":"63999","bestBidSize":3,"bestAskPrice":"64001","bestAskSize":4,"ts":1700000000000000000}}`)
		case request.URL.Path == "/api/v1/level2/depth20":
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"sequence":2,"symbol":"XBTUSDTM","bids":[["63999",3]],"asks":[["64001","4"]],"ts":1700000000000000000}}`)
		case request.URL.Path == "/api/v1/trade/history":
			_, _ = io.WriteString(writer, `{"code":"200000","data":[{"sequence":3,"contractId":1,"tradeId":"trade-1","makerOrderId":"maker-1","takerOrderId":"taker-1","ts":1700000000000000000,"size":2,"price":"64000","side":"buy"}]}`)
		case request.URL.Path == "/api/v1/kline/query":
			_, _ = io.WriteString(writer, `{"code":"200000","data":[[1700000000000,"63000",65000,"62000","64000",10,"640000"]]}`)
		case request.URL.Path == "/api/v1/account-overview":
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"accountEquity":"1000","unrealisedPNL":"10","marginBalance":"1010","positionMargin":"100","orderMargin":"20","frozenFunds":"5","availableBalance":"885","availableMargin":"890","currency":"USDT","riskRatio":"0.1","maxWithdrawAmount":"880"}}`)
		case request.URL.Path == "/api/v1/positions":
			_, _ = io.WriteString(writer, `{"code":"200000","data":[{"id":"position-1","symbol":"XBTUSDTM","autoDeposit":false,"crossMode":false,"currentQty":2,"openingTimestamp":1700000000000,"currentTimestamp":1700000001000,"markPrice":"64000","posMargin":"100","maintMargin":"5","realisedPnl":"1","unrealisedPnl":"10","avgEntryPrice":"63000","liquidationPrice":"50000","bankruptPrice":"49000","settleCurrency":"USDT","isInverse":false,"isOpen":true,"marginMode":"ISOLATED","positionSide":"BOTH","leverage":"10"}]}`)
		case request.URL.Path == "/api/v1/orders" && request.Method == http.MethodPost:
			body, _ := io.ReadAll(request.Body)
			if string(body) != `{"clientOid":"strategy-1","symbol":"XBTUSDTM","marginMode":"ISOLATED","leverage":10,"positionSide":"BOTH","side":"buy","type":"limit","size":2,"price":"64000","timeInForce":"GTC"}` {
				http.Error(writer, `{"code":"400100","msg":"unexpected body"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"orderId":"order-1","clientOid":"strategy-1"}}`)
		case request.URL.Path == "/api/v1/orders/order-1" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"id":"order-1","symbol":"XBTUSDTM","type":"limit","side":"buy","price":"64000","size":2,"value":"128","dealValue":"64","dealSize":1,"timeInForce":"GTC","postOnly":false,"leverage":"10","closeOrder":false,"clientOid":"strategy-1","isActive":true,"cancelExist":false,"createdAt":1700000000000,"updatedAt":1700000001000,"orderTime":1700000000000000000,"settleCurrency":"USDT","marginMode":"ISOLATED","positionSide":"BOTH","avgDealPrice":"64000","filledSize":1,"filledValue":"64","status":"open","reduceOnly":false}}`)
		case request.URL.Path == "/api/v1/orders/order-1" && request.Method == http.MethodDelete:
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"cancelledOrderIds":["order-1"]}}`)
		case request.URL.Path == "/api/v1/orders" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":1,"totalPage":1,"items":[{"id":"order-1","symbol":"XBTUSDTM","type":"limit","side":"buy","price":"64000","size":2,"value":"128","dealValue":"0","dealSize":0,"timeInForce":"GTC","leverage":"10","clientOid":"strategy-1","isActive":true,"createdAt":1700000000000,"marginMode":"ISOLATED","positionSide":"BOTH","avgDealPrice":"0","filledSize":0,"filledValue":"0","status":"open"}]}}`)
		case request.URL.Path == "/api/v1/fills":
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":1,"totalPage":1,"items":[{"symbol":"XBTUSDTM","tradeId":"trade-1","orderId":"order-1","side":"buy","liquidity":"taker","price":"64000","size":1,"value":"64","fee":"0.0384","feeRate":"0.0006","feeCurrency":"USDT","marginMode":"ISOLATED","positionSide":"BOTH","orderType":"limit","tradeType":"trade","tradeTime":1700000000000000000,"createdAt":1700000000000}]}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newTestClient(
		t, server.URL, sender, provider, []transport.EgressRouteID{"route-a", "route-b"},
		func() time.Time { return fixedNow },
	)

	contracts, err := client.Contracts(context.Background(), trade.WithEgressRoute("route-b"))
	if err != nil || len(contracts) != 1 || contracts[0].TickSize != "0.1" || len(contracts[0].Raw) == 0 {
		t.Fatalf("Contracts() = %+v, error = %v", contracts, err)
	}
	contract, err := client.Contract(context.Background(), "XBTUSDTM")
	if err != nil || contract.MarkPrice != "64000" || len(contract.Raw) == 0 {
		t.Fatalf("Contract() = %+v, error = %v", contract, err)
	}
	ticker, err := client.Ticker(context.Background(), "XBTUSDTM")
	if err != nil || ticker.Price != "64000" || ticker.BestAskSize != 4 || len(ticker.Raw) == 0 {
		t.Fatalf("Ticker() = %+v, error = %v", ticker, err)
	}
	book, err := client.OrderBook(
		context.Background(), OrderBookRequest{Symbol: "XBTUSDTM", Size: OrderBook20},
	)
	if err != nil || len(book.Bids) != 1 || book.Bids[0].Size != "3" || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := client.RecentTrades(
		context.Background(), RecentTradesRequest{Symbol: "XBTUSDTM"},
	)
	if err != nil || len(trades) != 1 || trades[0].ContractID != 1 || len(trades[0].Raw) == 0 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	start, end := time.UnixMilli(1_699_999_000_000), time.UnixMilli(1_700_000_000_000)
	candles, err := client.Candles(
		context.Background(), CandlesRequest{
			Symbol: "XBTUSDTM", Granularity: Candle1Hour, From: &start, To: &end,
		},
	)
	if err != nil || len(candles) != 1 || candles[0].Close != "64000" || len(candles[0].Raw) == 0 {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	overview, err := client.AccountOverview(
		context.Background(), AccountOverviewRequest{Currency: "USDT"},
	)
	if err != nil || overview.AvailableBalance != "885" || len(overview.Raw) == 0 {
		t.Fatalf("AccountOverview() = %+v, error = %v", overview, err)
	}
	positions, err := client.Positions(
		context.Background(), PositionsRequest{Currency: "USDT"},
	)
	if err != nil || len(positions) != 1 || positions[0].CurrentQuantity != 2 || len(positions[0].Raw) == 0 {
		t.Fatalf("Positions() = %+v, error = %v", positions, err)
	}
	reference, err := client.PlaceOrder(
		context.Background(), PlaceOrderRequest{
			ClientOrderID: "strategy-1", Symbol: "XBTUSDTM", MarginMode: MarginModeIsolated,
			Leverage: 10, PositionSide: PositionSideBoth, Side: SideBuy, Type: OrderTypeLimit,
			Size: 2, Price: "64000", TimeInForce: TimeInForceGTC,
		}, trade.WithEgressRoute("route-b"),
	)
	if err != nil || reference.OrderID != "order-1" || len(reference.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", reference, err)
	}
	order, err := client.OrderInfo(context.Background(), OrderInfoRequest{OrderID: "order-1"})
	if err != nil || !order.Active || order.DealSize != 1 || len(order.Raw) == 0 {
		t.Fatalf("OrderInfo() = %+v, error = %v", order, err)
	}
	canceled, err := client.CancelOrder(
		context.Background(), CancelOrderRequest{OrderID: "order-1"},
	)
	if err != nil || !slices.Equal(canceled.CancelledOrderIDs, []string{"order-1"}) {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	orders, err := client.OpenOrders(
		context.Background(), OpenOrdersRequest{Symbol: "XBTUSDTM", CurrentPage: 1, PageSize: 50},
	)
	if err != nil || len(orders.Orders) != 1 || orders.TotalPages != 1 ||
		len(orders.Raw) == 0 || len(orders.Orders[0].Raw) == 0 {
		t.Fatalf("OpenOrders() = %+v, error = %v", orders, err)
	}
	fills, err := client.Fills(
		context.Background(), FillsRequest{
			OrderID: "order-1", Symbol: "XBTUSDTM", CurrentPage: 1, PageSize: 50,
		},
	)
	if err != nil || len(fills.Fills) != 1 || fills.Fills[0].Fee != "0.0384" ||
		len(fills.Raw) == 0 || len(fills.Fills[0].Raw) == 0 {
		t.Fatalf("Fills() = %+v, error = %v", fills, err)
	}

	routes := sender.snapshot()
	if len(routes) != 13 || routes[0] != "route-b" || routes[8] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	calls, apiKey, secret, passphrase := provider.snapshot()
	if calls != 7 {
		t.Fatalf("provider calls = %d, want 7", calls)
	}
	if !allZero(apiKey) || !allZero(secret) || !allZero(passphrase) {
		t.Fatal("resolved credential byte slices were not overwritten")
	}
	publicRouteA, err := limiter.Snapshot("kucoin-futures:route:route-a:public:30seconds")
	if err != nil || publicRouteA.Used != 18 {
		t.Fatalf("route-a public limiter snapshot = %+v, error = %v", publicRouteA, err)
	}
	publicRouteB, err := limiter.Snapshot("kucoin-futures:route:route-b:public:30seconds")
	if err != nil || publicRouteB.Used != 3 {
		t.Fatalf("route-b public limiter snapshot = %+v, error = %v", publicRouteB, err)
	}
	futuresSnapshot, err := limiter.Snapshot("kucoin-futures:account:kucoin-main:futures:30seconds")
	if err != nil || futuresSnapshot.Used != 22 {
		t.Fatalf("Futures limiter snapshot = %+v, error = %v", futuresSnapshot, err)
	}
}

func TestClientRejectsUnauthorizedRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{}
	client, _ := newTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, nil,
	)
	_, err := client.AccountOverview(
		context.Background(), AccountOverviewRequest{Currency: "USDT"},
		trade.WithEgressRoute("route-b"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("AccountOverview() error = %v, want authorization", err)
	}
	calls, _, _, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestClientClassifiesLogicalErrorsAndUnknownMutationState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/ticker":
			_, _ = io.WriteString(writer, `{"code":"429000","msg":"Too Many Requests"}`)
		case "/api/v1/account-overview":
			_, _ = io.WriteString(writer, `{"code":"300003","msg":"Balance insufficient"}`)
		default:
			_, _ = io.WriteString(writer, `{"code":"500000","msg":"Internal Server Error"}`)
		}
	}))
	defer server.Close()

	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return time.UnixMilli(1_700_000_000_000) },
	)
	_, err := client.Ticker(context.Background(), "XBTUSDTM")
	if !errors.Is(err, trade.ErrRateLimited) {
		t.Fatalf("Ticker() error = %v, want rate limited", err)
	}
	_, err = client.AccountOverview(
		context.Background(), AccountOverviewRequest{Currency: "USDT"},
	)
	if !errors.Is(err, trade.ErrInsufficientBalance) {
		t.Fatalf("AccountOverview() error = %v, want insufficient balance", err)
	}
	_, err = client.PlaceOrder(
		context.Background(), PlaceOrderRequest{
			ClientOrderID: "strategy-1", Symbol: "XBTUSDTM", MarginMode: MarginModeCross,
			Side: SideBuy, Type: OrderTypeMarket, Size: 1,
		},
	)
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() error = %v, want unknown execution state", err)
	}
}

func TestRequestValidation(t *testing.T) {
	t.Parallel()

	end, start := time.UnixMilli(1_700_000_000_000), time.UnixMilli(1_700_001_000_000)
	tests := []struct {
		name string
		err  error
	}{
		{name: "symbol", err: validateSymbol("xbtusdtm")},
		{name: "book size", err: (OrderBookRequest{Symbol: "XBTUSDTM", Size: 50}).validate()},
		{name: "candle range", err: (CandlesRequest{Symbol: "XBTUSDTM", Granularity: Candle1Minute, From: &start, To: &end}).validate()},
		{name: "account currency", err: (AccountOverviewRequest{Currency: "usdt"}).validate()},
		{name: "market options", err: (PlaceOrderRequest{ClientOrderID: "one", Symbol: "XBTUSDTM", MarginMode: MarginModeCross, Type: OrderTypeMarket, Side: SideBuy, Size: 1, Price: "1"}).validate()},
		{name: "post only IOC", err: (PlaceOrderRequest{ClientOrderID: "one", Symbol: "XBTUSDTM", MarginMode: MarginModeCross, Type: OrderTypeLimit, Side: SideBuy, Size: 1, Price: "1", TimeInForce: TimeInForceIOC, PostOnly: true}).validate()},
		{name: "cancel identity", err: (CancelOrderRequest{OrderID: "one", ClientOrderID: "two"}).validate()},
		{name: "page size", err: (OpenOrdersRequest{Symbol: "XBTUSDTM", PageSize: 51}).validate()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, trade.ErrValidation) {
				t.Fatalf("validation error = %v", test.err)
			}
		})
	}
}

func TestSignHMACSHA256(t *testing.T) {
	t.Parallel()

	payload := []byte("1547015186532POST/api/v1/orders{\"symbol\":\"XBTUSDTM\"}")
	got, err := signHMACSHA256([]byte("test-secret"), payload)
	if err != nil {
		t.Fatalf("signHMACSHA256() error = %v", err)
	}
	mac := hmac.New(sha256.New, []byte("test-secret"))
	_, _ = mac.Write(payload)
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func isPrivatePath(path string) bool {
	return path == "/api/v1/account-overview" || path == "/api/v1/positions" ||
		path == "/api/v1/orders" || path == "/api/v1/orders/order-1" ||
		path == "/api/v1/fills"
}

func verifySignedRequest(t *testing.T, request *http.Request, secret []byte, now time.Time) bool {
	t.Helper()
	timestamp := request.Header.Get("KC-API-TIMESTAMP")
	if timestamp != "1700000000000" || request.Header.Get("KC-API-KEY") != "test-api-key" ||
		request.Header.Get("KC-API-KEY-VERSION") != "2" {
		return false
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	endpoint := request.URL.EscapedPath()
	if request.URL.RawQuery != "" {
		endpoint += "?" + request.URL.RawQuery
	}
	wantSignature, err := signHMACSHA256(
		secret, signaturePayload(timestamp, request.Method, endpoint, body),
	)
	if err != nil {
		t.Fatalf("sign expected request: %v", err)
	}
	wantPassphrase, err := signHMACSHA256(secret, []byte("test-passphrase"))
	if err != nil {
		t.Fatalf("sign expected passphrase: %v", err)
	}
	return hmac.Equal([]byte(request.Header.Get("KC-API-SIGN")), []byte(wantSignature)) &&
		hmac.Equal([]byte(request.Header.Get("KC-API-PASSPHRASE")), []byte(wantPassphrase)) &&
		now.UnixMilli() == 1_700_000_000_000
}

func newTestClient(
	t *testing.T,
	baseURL string,
	sender commonexchange.Sender,
	provider credential.Provider,
	allowedRoutes []transport.EgressRouteID,
	now func() time.Time,
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
			AccountID: "kucoin-main", Exchange: model.ExchangeKuCoin, SecretRef: "secret/kucoin-main",
			Permissions: []credential.Permission{
				credential.PermissionRead, credential.PermissionTrade,
			},
			AllowedEgressRouteIDs: allowedRoutes,
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true, Now: now,
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
