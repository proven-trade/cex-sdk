package kucoin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

type extCase struct {
	name     string
	public   bool
	method   string
	path     string
	query    url.Values
	body     string
	response string
	call     func(*Client) (any, error)
	check    func(*testing.T, any)
}

func newExtTestClient(t *testing.T, baseURL string, permissions ...credential.Permission) *Client {
	t.Helper()
	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{
		Sender: &directSender{}, Limiter: limiter,
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	client, err := New(Config{
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "kucoin-main", Exchange: model.ExchangeKuCoin, SecretRef: "secret/kucoin-main",
			Permissions:           permissions,
			AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
		},
		CredentialProvider: &recordingProvider{}, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
		Now: func() time.Time { return time.UnixMilli(1_700_000_000_000) },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

func runExtCases(t *testing.T, cases []extCase) {
	t.Helper()
	allPermissions := []credential.Permission{
		credential.PermissionRead, credential.PermissionTrade,
		credential.PermissionTransfer, credential.PermissionWithdraw,
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			now := time.UnixMilli(1_700_000_000_000)
			var gotMethod, gotPath, gotBody string
			var gotQuery url.Values
			signed := false
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				gotMethod, gotPath, gotQuery = request.Method, request.URL.Path, request.URL.Query()
				if !testCase.public {
					signed = verifySignedRequest(t, request, []byte("test-secret"), now)
				}
				body, _ := io.ReadAll(request.Body)
				gotBody = string(body)
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, testCase.response)
			}))
			defer server.Close()
			client := newExtTestClient(t, server.URL, allPermissions...)
			result, err := testCase.call(client)
			if err != nil {
				t.Fatalf("call error = %v", err)
			}
			if gotMethod != testCase.method || gotPath != testCase.path {
				t.Fatalf("request = %s %s, want %s %s", gotMethod, gotPath, testCase.method, testCase.path)
			}
			if !testCase.public && !signed {
				t.Fatal("private request signature or headers are invalid")
			}
			if testCase.query.Encode() != gotQuery.Encode() {
				t.Fatalf("query = %q, want %q", gotQuery.Encode(), testCase.query.Encode())
			}
			if gotBody != testCase.body {
				t.Fatalf("body = %s, want %s", gotBody, testCase.body)
			}
			testCase.check(t, result)
		})
	}
}

func TestMarketExtEndpoints(t *testing.T) {
	t.Parallel()
	runExtCases(t, []extCase{
		{
			name: "currencies", public: true, method: "GET", path: "/api/v3/currencies",
			response: `{"code":"200000","data":[{"currency":"BTC","name":"BTC","fullName":"Bitcoin","precision":8,"confirms":null,"contractAddress":null,"isMarginEnabled":true,"isDebitEnabled":true,"chains":[{"chainName":"BTC","withdrawalMinSize":"0.00018","depositMinSize":null,"withdrawFeeRate":"0","withdrawalMinFee":"0.00009","isWithdrawEnabled":true,"isDepositEnabled":true,"confirms":3,"preConfirms":1,"contractAddress":"","withdrawPrecision":8,"maxWithdraw":null,"maxDeposit":"0.03","needTag":false,"chainId":"btc"}]}]}`,
			call:     func(client *Client) (any, error) { return client.Currencies(context.Background()) },
			check: func(t *testing.T, value any) {
				items := value.([]Currency)
				if len(items) != 1 || items[0].Chains[0].ChainID != "btc" || items[0].Chains[0].MaxDeposit != "0.03" ||
					items[0].Chains[0].DepositMinSize != "" || !items[0].Chains[0].WithdrawEnabled || len(items[0].Raw) == 0 {
					t.Fatalf("Currencies() = %+v", items)
				}
			},
		},
		{
			name: "currency object with chain", public: true, method: "GET", path: "/api/v3/currencies/USDT",
			query:    url.Values{"chain": {"trx"}},
			response: `{"code":"200000","data":{"currency":"USDT","name":"USDT","fullName":"Tether","precision":8,"chains":[{"chainName":"TRC20","maxWithdraw":1000000,"chainId":"trx"}]}}`,
			call: func(client *Client) (any, error) {
				return client.Currency(context.Background(), CurrencyRequest{Currency: "USDT", Chain: "trx"})
			},
			check: func(t *testing.T, value any) {
				currency := value.(Currency)
				if currency.Currency != "USDT" || currency.Chains[0].MaxWithdraw != "1000000" {
					t.Fatalf("Currency() = %+v", currency)
				}
			},
		},
		{
			name: "currency array", public: true, method: "GET", path: "/api/v3/currencies/BTC",
			response: `{"code":"200000","data":[{"currency":"BTC","fullName":"Bitcoin"}]}`,
			call: func(client *Client) (any, error) {
				return client.Currency(context.Background(), CurrencyRequest{Currency: "BTC"})
			},
			check: func(t *testing.T, value any) {
				if value.(Currency).FullName != "Bitcoin" {
					t.Fatalf("Currency() = %+v", value)
				}
			},
		},
		{
			name: "symbol info", public: true, method: "GET", path: "/api/v2/symbols/BTC-USDT",
			response: `{"code":"200000","data":{"symbol":"BTC-USDT","baseCurrency":"BTC","quoteCurrency":"USDT","minFunds":"0.1","enableTrading":true,"feeCategory":1,"makerFeeCoefficient":"1","takerFeeCoefficient":"1","st":false,"callauctionIsEnabled":false,"callauctionPriceFloor":null,"tradingStartTime":1700000000000}}`,
			call:     func(client *Client) (any, error) { return client.SymbolInfo(context.Background(), "BTC-USDT") },
			check: func(t *testing.T, value any) {
				detail := value.(SymbolDetail)
				if detail.Symbol != "BTC-USDT" || detail.MinimumFunds != "0.1" || !detail.TradingEnabled ||
					detail.TradingStartTime != 1700000000000 || len(detail.Raw) == 0 {
					t.Fatalf("SymbolInfo() = %+v", detail)
				}
			},
		},
		{
			name: "all tickers", public: true, method: "GET", path: "/api/v1/market/allTickers",
			response: `{"code":"200000","data":{"time":1700000000000,"ticker":[{"symbol":"BTC-USDT","symbolName":"BTC-USDT","buy":"64000","bestBidSize":"1","sell":"64001","bestAskSize":"2","changeRate":"0.01","changePrice":"640","high":"65000","low":"63000","vol":"100","volValue":"6400000","last":"64000.5","averagePrice":"64010","takerFeeRate":"0.001","makerFeeRate":"0.001","takerCoefficient":"1","makerCoefficient":"1"}]}}`,
			call:     func(client *Client) (any, error) { return client.AllTickers(context.Background()) },
			check: func(t *testing.T, value any) {
				tickers := value.(AllTickers)
				if tickers.Time != 1700000000000 || len(tickers.Tickers) != 1 ||
					tickers.Tickers[0].Last != "64000.5" || tickers.Tickers[0].BestBid != "64000" || len(tickers.Tickers[0].Raw) == 0 {
					t.Fatalf("AllTickers() = %+v", tickers)
				}
			},
		},
		{
			name: "market stats", public: true, method: "GET", path: "/api/v1/market/stats",
			query:    url.Values{"symbol": {"BTC-USDT"}},
			response: `{"code":"200000","data":{"time":1700000000000,"symbol":"BTC-USDT","buy":"64000","sell":"64001","changeRate":"0.01","changePrice":"640","high":"65000","low":"63000","vol":"100","volValue":"6400000","last":"64000.5","averagePrice":"64010","takerFeeRate":"0.001","makerFeeRate":"0.001","takerCoefficient":"1","makerCoefficient":"1"}}`,
			call:     func(client *Client) (any, error) { return client.MarketStats(context.Background(), "BTC-USDT") },
			check: func(t *testing.T, value any) {
				stats := value.(MarketStats)
				if stats.Volume != "100" || stats.High != "65000" {
					t.Fatalf("MarketStats() = %+v", stats)
				}
			},
		},
		{
			name: "full order book is signed", method: "GET", path: "/api/v3/market/orderbook/level2",
			query:    url.Values{"symbol": {"BTC-USDT"}},
			response: `{"code":"200000","data":{"time":1700000000000,"sequence":"14610502970","bids":[["64000","1"],["63999","2"]],"asks":[["64001","3"]]}}`,
			call:     func(client *Client) (any, error) { return client.FullOrderBook(context.Background(), "BTC-USDT") },
			check: func(t *testing.T, value any) {
				book := value.(OrderBook)
				if book.Sequence != "14610502970" || len(book.Bids) != 2 || book.Asks[0].Size != "3" {
					t.Fatalf("FullOrderBook() = %+v", book)
				}
			},
		},
		{
			name: "server time", public: true, method: "GET", path: "/api/v1/timestamp",
			response: `{"code":"200000","data":1729100692873}`,
			call:     func(client *Client) (any, error) { return client.ServerTime(context.Background()) },
			check: func(t *testing.T, value any) {
				serverTime := value.(ServerTime)
				if serverTime.Millis != 1729100692873 || serverTime.Time.UnixMilli() != 1729100692873 {
					t.Fatalf("ServerTime() = %+v", serverTime)
				}
			},
		},
		{
			name: "service status", public: true, method: "GET", path: "/api/v1/status",
			response: `{"code":"200000","data":{"status":"open","msg":""}}`,
			call:     func(client *Client) (any, error) { return client.ServiceStatus(context.Background()) },
			check: func(t *testing.T, value any) {
				if value.(ServiceStatus).Status != "open" {
					t.Fatalf("ServiceStatus() = %+v", value)
				}
			},
		},
	})
}

func TestTradeExtEndpoints(t *testing.T) {
	t.Parallel()
	limitOrder := PlaceOrderRequest{
		ClientOrderID: "c1", Symbol: "BTC-USDT", Type: OrderTypeLimit, Side: SideBuy,
		Price: "64000", Size: "0.01",
	}
	const limitOrderBody = `{"clientOid":"c1","symbol":"BTC-USDT","type":"limit","side":"buy","price":"64000","size":"0.01"}`
	start := time.UnixMilli(1_700_000_000_000)
	end := time.UnixMilli(1_700_000_100_000)
	runExtCases(t, []extCase{
		{
			name: "modify order", method: "POST", path: "/api/v1/hf/orders/alter",
			body:     `{"symbol":"BTC-USDT","orderId":"o1","newPrice":"30000","newSize":"0.0001"}`,
			response: `{"code":"200000","data":{"newOrderId":"o2","clientOid":"c1"}}`,
			call: func(client *Client) (any, error) {
				return client.ModifyOrder(context.Background(), ModifyOrderRequest{
					Symbol: "BTC-USDT", OrderID: "o1", NewPrice: "30000", NewSize: "0.0001",
				})
			},
			check: func(t *testing.T, value any) {
				result := value.(ModifyOrderResult)
				if result.NewOrderID != "o2" || result.ClientOrderID != "c1" || len(result.Raw) == 0 {
					t.Fatalf("ModifyOrder() = %+v", result)
				}
			},
		},
		{
			name: "place order sync", method: "POST", path: "/api/v1/hf/orders/sync", body: limitOrderBody,
			response: `{"code":"200000","data":{"orderId":"o1","clientOid":"c1","orderTime":1700000000000,"originSize":"0.01","dealSize":"0","remainSize":"0.01","canceledSize":"0","status":"open","matchTime":1700000000001}}`,
			call:     func(client *Client) (any, error) { return client.PlaceOrderSync(context.Background(), limitOrder) },
			check: func(t *testing.T, value any) {
				result := value.(OrderSyncResult)
				if result.OrderID != "o1" || result.Status != "open" || result.MatchTime != 1700000000001 {
					t.Fatalf("PlaceOrderSync() = %+v", result)
				}
			},
		},
		{
			name: "test order", method: "POST", path: "/api/v1/hf/orders/test", body: limitOrderBody,
			response: `{"code":"200000","data":{"orderId":"o1","clientOid":"c1"}}`,
			call:     func(client *Client) (any, error) { return client.TestOrder(context.Background(), limitOrder) },
			check: func(t *testing.T, value any) {
				if value.(OrderReference).OrderID != "o1" {
					t.Fatalf("TestOrder() = %+v", value)
				}
			},
		},
		{
			name: "batch place orders", method: "POST", path: "/api/v1/hf/orders/multi",
			body:     `{"orderList":[` + limitOrderBody + `]}`,
			response: `{"code":"200000","data":[{"orderId":"o1","clientOid":"c1","success":true},{"clientOid":"c2","success":false,"failMsg":"insufficient"}]}`,
			call: func(client *Client) (any, error) {
				return client.BatchPlaceOrders(context.Background(), BatchOrdersRequest{Orders: []PlaceOrderRequest{limitOrder}})
			},
			check: func(t *testing.T, value any) {
				results := value.([]BatchOrderResult)
				if len(results) != 2 || !results[0].Success || results[1].Success || results[1].FailMessage != "insufficient" {
					t.Fatalf("BatchPlaceOrders() = %+v", results)
				}
			},
		},
		{
			name: "batch place orders sync", method: "POST", path: "/api/v1/hf/orders/multi/sync",
			body:     `{"orderList":[` + limitOrderBody + `]}`,
			response: `{"code":"200000","data":[{"orderId":"o1","clientOid":"c1","orderTime":1700000000000,"status":"open","success":true}]}`,
			call: func(client *Client) (any, error) {
				return client.BatchPlaceOrdersSync(context.Background(), BatchOrdersRequest{Orders: []PlaceOrderRequest{limitOrder}})
			},
			check: func(t *testing.T, value any) {
				results := value.([]BatchOrderResult)
				if len(results) != 1 || results[0].Status != "open" || results[0].OrderTime != 1700000000000 {
					t.Fatalf("BatchPlaceOrdersSync() = %+v", results)
				}
			},
		},
		{
			name: "cancel order sync by order ID", method: "DELETE", path: "/api/v1/hf/orders/sync/o1",
			query:    url.Values{"symbol": {"BTC-USDT"}},
			response: `{"code":"200000","data":{"orderId":"o1","originSize":"0.01","dealSize":"0","remainSize":"0","canceledSize":"0.01","status":"done"}}`,
			call: func(client *Client) (any, error) {
				return client.CancelOrderSync(context.Background(), CancelOrderRequest{OrderID: "o1", Symbol: "BTC-USDT"})
			},
			check: func(t *testing.T, value any) {
				if result := value.(OrderSyncResult); result.CanceledSize != "0.01" || result.Status != "done" {
					t.Fatalf("CancelOrderSync() = %+v", result)
				}
			},
		},
		{
			name: "cancel order sync by client ID", method: "DELETE", path: "/api/v1/hf/orders/sync/client-order/c1",
			query:    url.Values{"symbol": {"BTC-USDT"}},
			response: `{"code":"200000","data":{"clientOid":"c1","originSize":"0.01","dealSize":"0","remainSize":"0","canceledSize":"0.01","status":"done"}}`,
			call: func(client *Client) (any, error) {
				return client.CancelOrderSync(context.Background(), CancelOrderRequest{ClientOrderID: "c1", Symbol: "BTC-USDT"})
			},
			check: func(t *testing.T, value any) {
				if value.(OrderSyncResult).ClientOrderID != "c1" {
					t.Fatalf("CancelOrderSync() = %+v", value)
				}
			},
		},
		{
			name: "cancel partial order", method: "DELETE", path: "/api/v1/hf/orders/cancel/o1",
			query:    url.Values{"symbol": {"BTC-USDT"}, "cancelSize": {"0.00001"}},
			response: `{"code":"200000","data":{"orderId":"o1","cancelSize":"0.00001"}}`,
			call: func(client *Client) (any, error) {
				return client.CancelPartialOrder(context.Background(), CancelPartialOrderRequest{
					OrderID: "o1", Symbol: "BTC-USDT", CancelSize: "0.00001",
				})
			},
			check: func(t *testing.T, value any) {
				if value.(PartialCancelResult).CancelSize != "0.00001" {
					t.Fatalf("CancelPartialOrder() = %+v", value)
				}
			},
		},
		{
			name: "cancel all by symbol", method: "DELETE", path: "/api/v1/hf/orders",
			query:    url.Values{"symbol": {"BTC-USDT"}},
			response: `{"code":"200000","data":"success"}`,
			call: func(client *Client) (any, error) {
				return client.CancelAllOrdersBySymbol(context.Background(), "BTC-USDT")
			},
			check: func(t *testing.T, value any) {
				if string(value.(Acknowledgement).Raw) != `"success"` {
					t.Fatalf("CancelAllOrdersBySymbol() = %+v", value)
				}
			},
		},
		{
			name: "cancel all orders", method: "DELETE", path: "/api/v1/hf/orders/cancelAll",
			response: `{"code":"200000","data":{"succeedSymbols":["BTC-USDT"],"failedSymbols":[{"symbol":"ETH-USDT","error":"busy"}]}}`,
			call:     func(client *Client) (any, error) { return client.CancelAllOrders(context.Background()) },
			check: func(t *testing.T, value any) {
				result := value.(CancelAllResult)
				if len(result.SucceededSymbols) != 1 || result.FailedSymbols[0].Error != "busy" {
					t.Fatalf("CancelAllOrders() = %+v", result)
				}
			},
		},
		{
			name: "dead cancel all", method: "POST", path: "/api/v1/hf/orders/dead-cancel-all",
			body:     `{"symbols":"BTC-USDT,ETH-USDT","timeout":5}`,
			response: `{"code":"200000","data":{"currentTime":1729241305,"triggerTime":1729241308}}`,
			call: func(client *Client) (any, error) {
				return client.DeadCancelAll(context.Background(), DeadCancelRequest{
					Timeout: 5, Symbols: []string{"BTC-USDT", "ETH-USDT"},
				})
			},
			check: func(t *testing.T, value any) {
				setting := value.(DeadCancelSetting)
				if !setting.Configured || setting.TriggerTime != 1729241308 {
					t.Fatalf("DeadCancelAll() = %+v", setting)
				}
			},
		},
		{
			name: "dead cancel query", method: "GET", path: "/api/v1/hf/orders/dead-cancel-all/query",
			response: `{"code":"200000","data":{"timeout":5,"symbols":"BTC-USDT","currentTime":1729241305,"triggerTime":1729241308}}`,
			call:     func(client *Client) (any, error) { return client.DeadCancelSettingQuery(context.Background()) },
			check: func(t *testing.T, value any) {
				setting := value.(DeadCancelSetting)
				if !setting.Configured || setting.Timeout != 5 || setting.Symbols != "BTC-USDT" {
					t.Fatalf("DeadCancelSettingQuery() = %+v", setting)
				}
			},
		},
		{
			name: "dead cancel query unset", method: "GET", path: "/api/v1/hf/orders/dead-cancel-all/query",
			response: `{"code":"200000","data":null}`,
			call:     func(client *Client) (any, error) { return client.DeadCancelSettingQuery(context.Background()) },
			check: func(t *testing.T, value any) {
				if value.(DeadCancelSetting).Configured {
					t.Fatalf("DeadCancelSettingQuery() = %+v", value)
				}
			},
		},
		{
			name: "closed orders", method: "GET", path: "/api/v1/hf/orders/done",
			query: url.Values{
				"symbol": {"BTC-USDT"}, "side": {"buy"}, "type": {"limit"}, "lastId": {"9"}, "limit": {"50"},
				"startAt": {"1700000000000"}, "endAt": {"1700000100000"},
			},
			response: `{"code":"200000","data":{"lastId":19814995255305,"items":[{"id":"o1","symbol":"BTC-USDT","opType":"DEAL","type":"limit","side":"buy","price":"64000","size":"0.01","dealSize":"0.01","dealFunds":"640","fee":"0.64","feeCurrency":"USDT","timeInForce":"GTC","clientOid":"c1","cancelExist":false,"createdAt":1700000000000,"lastUpdatedAt":1700000001000,"tradeType":"TRADE","cancelledSize":"0","remainSize":"0","active":false,"cancelReason":0}]}}`,
			call: func(client *Client) (any, error) {
				return client.ClosedOrders(context.Background(), ClosedOrdersRequest{
					Symbol: "BTC-USDT", Side: SideBuy, Type: OrderTypeLimit, LastID: 9, Limit: 50, Start: &start, End: &end,
				})
			},
			check: func(t *testing.T, value any) {
				page := value.(CursorPage[ClosedOrder])
				if page.LastID != 19814995255305 || len(page.Items) != 1 || page.Items[0].DealFunds != "640" ||
					page.Items[0].Active || len(page.Items[0].Raw) == 0 {
					t.Fatalf("ClosedOrders() = %+v", page)
				}
			},
		},
		{
			name: "fills", method: "GET", path: "/api/v1/hf/fills",
			query:    url.Values{"symbol": {"BTC-USDT"}, "orderId": {"o1"}},
			response: `{"code":"200000","data":{"items":[{"id":2678765568338945,"symbol":"BTC-USDT","tradeId":1,"orderId":"o1","counterOrderId":"o9","side":"buy","liquidity":"taker","forceTaker":false,"price":"64000","size":"0.01","funds":"640","fee":"0.64","feeRate":"0.001","feeCurrency":"USDT","stop":"","tradeType":"TRADE","taxRate":"0","tax":"0","type":"limit","createdAt":1700000000000}],"lastId":2678765568338945}}`,
			call: func(client *Client) (any, error) {
				return client.Fills(context.Background(), FillsRequest{Symbol: "BTC-USDT", OrderID: "o1"})
			},
			check: func(t *testing.T, value any) {
				page := value.(CursorPage[Fill])
				if len(page.Items) != 1 || page.Items[0].Liquidity != "taker" || page.Items[0].ID != 2678765568338945 {
					t.Fatalf("Fills() = %+v", page)
				}
			},
		},
	})
}

func TestStopAndOCOEndpoints(t *testing.T) {
	t.Parallel()
	runExtCases(t, []extCase{
		{
			name: "place stop order", method: "POST", path: "/api/v1/stop-order",
			body:     `{"clientOid":"s1","symbol":"BTC-USDT","type":"limit","side":"sell","price":"50000","size":"0.01","stopPrice":"51000","stop":"entry"}`,
			response: `{"code":"200000","data":{"orderId":"vs1"}}`,
			call: func(client *Client) (any, error) {
				return client.PlaceStopOrder(context.Background(), StopOrderRequest{
					PlaceOrderRequest: PlaceOrderRequest{
						ClientOrderID: "s1", Symbol: "BTC-USDT", Type: OrderTypeLimit, Side: SideSell,
						Price: "50000", Size: "0.01",
					},
					StopPrice: "51000", Stop: StopEntry,
				})
			},
			check: func(t *testing.T, value any) {
				if value.(StopOrderReference).OrderID != "vs1" {
					t.Fatalf("PlaceStopOrder() = %+v", value)
				}
			},
		},
		{
			name: "cancel stop order by ID", method: "DELETE", path: "/api/v1/stop-order/vs1",
			response: `{"code":"200000","data":{"cancelledOrderIds":["vs1"]}}`,
			call: func(client *Client) (any, error) {
				return client.CancelStopOrder(context.Background(), StopOrderIdentity{OrderID: "vs1"})
			},
			check: func(t *testing.T, value any) {
				if ids := value.(StopOrderCancellation).CancelledOrderIDs; len(ids) != 1 || ids[0] != "vs1" {
					t.Fatalf("CancelStopOrder() = %+v", value)
				}
			},
		},
		{
			name: "cancel stop order by client ID", method: "DELETE", path: "/api/v1/stop-order/cancelOrderByClientOid",
			query:    url.Values{"clientOid": {"s1"}, "symbol": {"BTC-USDT"}},
			response: `{"code":"200000","data":{"clientOid":"s1","cancelledOrderId":"vs1"}}`,
			call: func(client *Client) (any, error) {
				return client.CancelStopOrder(context.Background(), StopOrderIdentity{ClientOrderID: "s1", Symbol: "BTC-USDT"})
			},
			check: func(t *testing.T, value any) {
				if value.(StopOrderCancellation).CancelledOrderID != "vs1" {
					t.Fatalf("CancelStopOrder() = %+v", value)
				}
			},
		},
		{
			name: "cancel stop orders", method: "DELETE", path: "/api/v1/stop-order/cancel",
			query:    url.Values{"symbol": {"BTC-USDT"}, "orderIds": {"a,b"}},
			response: `{"code":"200000","data":{"cancelledOrderIds":["a","b"]}}`,
			call: func(client *Client) (any, error) {
				return client.CancelStopOrders(context.Background(), CancelStopOrdersRequest{
					Symbol: "BTC-USDT", OrderIDs: []string{"a", "b"},
				})
			},
			check: func(t *testing.T, value any) {
				if len(value.(CancelledOrderIDs).OrderIDs) != 2 {
					t.Fatalf("CancelStopOrders() = %+v", value)
				}
			},
		},
		{
			name: "stop orders", method: "GET", path: "/api/v1/stop-order",
			query:    url.Values{"symbol": {"BTC-USDT"}, "pageSize": {"10"}, "currentPage": {"2"}},
			response: `{"code":"200000","data":{"currentPage":2,"pageSize":10,"totalNum":11,"totalPage":2,"items":[{"id":"vs1","symbol":"BTC-USDT","status":"NEW","type":"limit","side":"sell","price":"50000","size":"0.01","stop":"entry","stopPrice":"51000","clientOid":"s1","orderTime":1700000000000000000}]}}`,
			call: func(client *Client) (any, error) {
				return client.StopOrders(context.Background(), StopOrdersRequest{Symbol: "BTC-USDT", CurrentPage: 2, PageSize: 10})
			},
			check: func(t *testing.T, value any) {
				page := value.(Page[StopOrder])
				if page.TotalPages != 2 || len(page.Items) != 1 || page.Items[0].StopPrice != "51000" || page.Items[0].Status != "NEW" {
					t.Fatalf("StopOrders() = %+v", page)
				}
			},
		},
		{
			name: "stop order by ID", method: "GET", path: "/api/v1/stop-order/vs1",
			response: `{"code":"200000","data":{"id":"vs1","symbol":"BTC-USDT","stopPrice":"51000"}}`,
			call: func(client *Client) (any, error) {
				return client.StopOrderInfo(context.Background(), StopOrderIdentity{OrderID: "vs1"})
			},
			check: func(t *testing.T, value any) {
				orders := value.([]StopOrder)
				if len(orders) != 1 || orders[0].ID != "vs1" {
					t.Fatalf("StopOrderInfo() = %+v", orders)
				}
			},
		},
		{
			name: "stop order by client ID", method: "GET", path: "/api/v1/stop-order/queryOrderByClientOid",
			query:    url.Values{"clientOid": {"s1"}},
			response: `{"code":"200000","data":[{"id":"vs1","clientOid":"s1","stopPrice":"51000"}]}`,
			call: func(client *Client) (any, error) {
				return client.StopOrderInfo(context.Background(), StopOrderIdentity{ClientOrderID: "s1"})
			},
			check: func(t *testing.T, value any) {
				orders := value.([]StopOrder)
				if len(orders) != 1 || orders[0].ClientOrderID != "s1" {
					t.Fatalf("StopOrderInfo() = %+v", orders)
				}
			},
		},
		{
			name: "place oco order", method: "POST", path: "/api/v3/oco/order",
			body:     `{"clientOid":"oc1","side":"sell","symbol":"BTC-USDT","price":"70000","size":"0.01","stopPrice":"60000","limitPrice":"59000"}`,
			response: `{"code":"200000","data":{"orderId":"oco1"}}`,
			call: func(client *Client) (any, error) {
				return client.PlaceOCOOrder(context.Background(), OCOOrderRequest{
					ClientOrderID: "oc1", Side: SideSell, Symbol: "BTC-USDT",
					Price: "70000", Size: "0.01", StopPrice: "60000", LimitPrice: "59000",
				})
			},
			check: func(t *testing.T, value any) {
				if value.(OCOOrderReference).OrderID != "oco1" {
					t.Fatalf("PlaceOCOOrder() = %+v", value)
				}
			},
		},
		{
			name: "cancel oco order by ID", method: "DELETE", path: "/api/v3/oco/order/oco1",
			response: `{"code":"200000","data":{"cancelledOrderIds":["a","b"]}}`,
			call: func(client *Client) (any, error) {
				return client.CancelOCOOrder(context.Background(), OCOOrderIdentity{OrderID: "oco1"})
			},
			check: func(t *testing.T, value any) {
				if len(value.(CancelledOrderIDs).OrderIDs) != 2 {
					t.Fatalf("CancelOCOOrder() = %+v", value)
				}
			},
		},
		{
			name: "cancel oco order by client ID", method: "DELETE", path: "/api/v3/oco/client-order/oc1",
			response: `{"code":"200000","data":{"cancelledOrderIds":["a","b"]}}`,
			call: func(client *Client) (any, error) {
				return client.CancelOCOOrder(context.Background(), OCOOrderIdentity{ClientOrderID: "oc1"})
			},
			check: func(t *testing.T, value any) {
				if len(value.(CancelledOrderIDs).OrderIDs) != 2 {
					t.Fatalf("CancelOCOOrder() = %+v", value)
				}
			},
		},
		{
			name: "cancel oco orders", method: "DELETE", path: "/api/v3/oco/orders",
			query:    url.Values{"orderIds": {"a,b"}},
			response: `{"code":"200000","data":{"cancelledOrderIds":["a","b"]}}`,
			call: func(client *Client) (any, error) {
				return client.CancelOCOOrders(context.Background(), CancelOCOOrdersRequest{OrderIDs: []string{"a", "b"}})
			},
			check: func(t *testing.T, value any) {
				if len(value.(CancelledOrderIDs).OrderIDs) != 2 {
					t.Fatalf("CancelOCOOrders() = %+v", value)
				}
			},
		},
		{
			name: "oco order info by client ID", method: "GET", path: "/api/v3/oco/client-order/oc1",
			response: `{"code":"200000","data":{"orderId":"oco1","symbol":"BTC-USDT","clientOid":"oc1","orderTime":1700000000000,"status":"NEW"}}`,
			call: func(client *Client) (any, error) {
				return client.OCOOrderInfo(context.Background(), OCOOrderIdentity{ClientOrderID: "oc1"})
			},
			check: func(t *testing.T, value any) {
				if order := value.(OCOOrder); order.OrderID != "oco1" || order.Status != "NEW" {
					t.Fatalf("OCOOrderInfo() = %+v", order)
				}
			},
		},
		{
			name: "oco order detail", method: "GET", path: "/api/v3/oco/order/details/oco1",
			response: `{"code":"200000","data":{"orderId":"oco1","symbol":"BTC-USDT","clientOid":"oc1","orderTime":1700000000000,"status":"NEW","orders":[{"id":"a","symbol":"BTC-USDT","side":"sell","price":"70000","stopPrice":"","size":"0.01","status":"NEW"}]}}`,
			call: func(client *Client) (any, error) {
				return client.OCOOrderDetail(context.Background(), "oco1")
			},
			check: func(t *testing.T, value any) {
				detail := value.(OCOOrderDetail)
				if len(detail.Orders) != 1 || detail.Orders[0].Price != "70000" {
					t.Fatalf("OCOOrderDetail() = %+v", detail)
				}
			},
		},
		{
			name: "oco orders", method: "GET", path: "/api/v3/oco/orders",
			query:    url.Values{"symbol": {"BTC-USDT"}, "pageSize": {"10"}},
			response: `{"code":"200000","data":{"currentPage":1,"pageSize":10,"totalNum":1,"totalPage":1,"items":[{"orderId":"oco1","symbol":"BTC-USDT","clientOid":"oc1","orderTime":1700000000000,"status":"NEW"}]}}`,
			call: func(client *Client) (any, error) {
				return client.OCOOrders(context.Background(), OCOOrdersRequest{Symbol: "BTC-USDT", PageSize: 10})
			},
			check: func(t *testing.T, value any) {
				page := value.(Page[OCOOrder])
				if page.TotalNumber != 1 || page.Items[0].OrderID != "oco1" || len(page.Items[0].Raw) == 0 {
					t.Fatalf("OCOOrders() = %+v", page)
				}
			},
		},
	})
}

func TestAccountExtEndpoints(t *testing.T) {
	t.Parallel()
	start := time.UnixMilli(1_700_000_000_000)
	runExtCases(t, []extCase{
		{
			name: "account summary v3", method: "GET", path: "/api/v3/user-info",
			response: `{"code":"200000","data":{"level":2,"maxSharedSubCount":5,"usedSharedSubCount":1}}`,
			call:     func(client *Client) (any, error) { return client.AccountSummary(context.Background()) },
			check: func(t *testing.T, value any) {
				if summary := value.(AccountSummary); summary.Level != 2 || summary.MaxSharedSubCount != 5 {
					t.Fatalf("AccountSummary() = %+v", summary)
				}
			},
		},
		{
			name: "account summary v2", method: "GET", path: "/api/v2/user-info",
			response: `{"code":"200000","data":{"level":0,"subQuantity":3,"maxSubQuantity":5}}`,
			call:     func(client *Client) (any, error) { return client.AccountSummaryV2(context.Background()) },
			check: func(t *testing.T, value any) {
				if value.(AccountSummaryV2).SubQuantity != 3 {
					t.Fatalf("AccountSummaryV2() = %+v", value)
				}
			},
		},
		{
			name: "api key info", method: "GET", path: "/api/v1/user/api-key",
			response: `{"code":"200000","data":{"uid":165111215,"region":"EU","kycStatus":3,"apiKey":"k","apiVersion":3,"permission":"General,Spot","ipWhitelist":"1.1.1.1","isMaster":true,"createdAt":1700000000000,"siteType":"global"}}`,
			call:     func(client *Client) (any, error) { return client.APIKeyInfo(context.Background()) },
			check: func(t *testing.T, value any) {
				info := value.(APIKeyInfo)
				if info.UID != 165111215 || info.Permission != "General,Spot" || !info.IsMaster {
					t.Fatalf("APIKeyInfo() = %+v", info)
				}
			},
		},
		{
			name: "hf account opened", method: "GET", path: "/api/v1/hf/accounts/opened",
			response: `{"code":"200000","data":true}`,
			call:     func(client *Client) (any, error) { return client.HFAccountOpened(context.Background()) },
			check: func(t *testing.T, value any) {
				if !value.(bool) {
					t.Fatal("HFAccountOpened() = false")
				}
			},
		},
		{
			name: "account detail", method: "GET", path: "/api/v1/accounts/548674591753",
			response: `{"code":"200000","data":{"currency":"USDT","balance":"26.66759503","available":"26.66759503","holds":"0"}}`,
			call: func(client *Client) (any, error) {
				return client.AccountDetail(context.Background(), "548674591753")
			},
			check: func(t *testing.T, value any) {
				if value.(AccountDetail).Balance != "26.66759503" {
					t.Fatalf("AccountDetail() = %+v", value)
				}
			},
		},
		{
			name: "ledgers", method: "GET", path: "/api/v1/accounts/ledgers",
			query:    url.Values{"currency": {"BTC,USDT"}, "direction": {"in"}, "startAt": {"1700000000000"}, "pageSize": {"10"}},
			response: `{"code":"200000","data":{"currentPage":1,"pageSize":10,"totalNum":1,"totalPage":1,"items":[{"id":"l1","currency":"BTC","amount":"1","fee":"0","balance":"2","accountType":"TRADE","bizType":"Transfer","direction":"in","createdAt":1700000000000,"context":"{}"}]}}`,
			call: func(client *Client) (any, error) {
				return client.Ledgers(context.Background(), LedgersRequest{
					Currencies: []string{"BTC", "USDT"}, Direction: LedgerIn, Start: &start, PageSize: 10,
				})
			},
			check: func(t *testing.T, value any) {
				page := value.(Page[Ledger])
				if len(page.Items) != 1 || page.Items[0].Balance != "2" || page.Items[0].Direction != "in" {
					t.Fatalf("Ledgers() = %+v", page)
				}
			},
		},
		{
			name: "hf ledgers", method: "GET", path: "/api/v1/hf/accounts/ledgers",
			query:    url.Values{"currency": {"BTC"}, "limit": {"200"}, "lastId": {"7"}},
			response: `{"code":"200000","data":[{"id":"1","currency":"BTC","amount":"1","fee":"0","tax":"0","balance":"2","accountType":"TRADE_HF","bizType":"TRADE_EXCHANGE","direction":"out","createdAt":"1700000000000","context":"{}"}]}`,
			call: func(client *Client) (any, error) {
				return client.HFLedgers(context.Background(), HFLedgersRequest{Currencies: []string{"BTC"}, Limit: 200, LastID: 7})
			},
			check: func(t *testing.T, value any) {
				items := value.([]HFLedger)
				if len(items) != 1 || items[0].CreatedAt != "1700000000000" || items[0].BizType != "TRADE_EXCHANGE" {
					t.Fatalf("HFLedgers() = %+v", items)
				}
			},
		},
		{
			name: "base fee", method: "GET", path: "/api/v1/base-fee", query: url.Values{"currencyType": {"1"}},
			response: `{"code":"200000","data":{"takerFeeRate":"0.001","makerFeeRate":"0.001"}}`,
			call:     func(client *Client) (any, error) { return client.BaseFee(context.Background(), CurrencyTypeFiat) },
			check: func(t *testing.T, value any) {
				if value.(FeeRate).TakerFeeRate != "0.001" {
					t.Fatalf("BaseFee() = %+v", value)
				}
			},
		},
		{
			name: "trade fees", method: "GET", path: "/api/v1/trade-fees",
			query:    url.Values{"symbols": {"BTC-USDT,ETH-USDT"}},
			response: `{"code":"200000","data":[{"symbol":"BTC-USDT","takerFeeRate":"0.001","makerFeeRate":"0.001"},{"symbol":"ETH-USDT","takerFeeRate":"0.002","makerFeeRate":"0.001","sellTaxRate":"0"}]}`,
			call: func(client *Client) (any, error) {
				return client.TradeFees(context.Background(), []string{"BTC-USDT", "ETH-USDT"})
			},
			check: func(t *testing.T, value any) {
				fees := value.([]FeeRate)
				if len(fees) != 2 || fees[1].TakerFeeRate != "0.002" {
					t.Fatalf("TradeFees() = %+v", fees)
				}
			},
		},
		{
			name: "deposit addresses", method: "GET", path: "/api/v3/deposit-addresses",
			query:    url.Values{"currency": {"USDT"}, "chain": {"trx"}},
			response: `{"code":"200000","data":[{"address":"T123","memo":"","chainId":"trx","to":"MAIN","currency":"USDT","contractAddress":"c","chainName":"TRC20","remark":"","expirationDate":0}]}`,
			call: func(client *Client) (any, error) {
				return client.DepositAddresses(context.Background(), DepositAddressRequest{Currency: "USDT", Chain: "trx"})
			},
			check: func(t *testing.T, value any) {
				if addresses := value.([]DepositAddress); len(addresses) != 1 || addresses[0].Address != "T123" {
					t.Fatalf("DepositAddresses() = %+v", addresses)
				}
			},
		},
		{
			name: "create deposit address", method: "POST", path: "/api/v3/deposit-address/create",
			body:     `{"currency":"USDT","chain":"trx","to":"trade"}`,
			response: `{"code":"200000","data":{"address":"T123","memo":"","chainId":"trx","to":"TRADE","currency":"USDT","chainName":"TRC20","expirationDate":0}}`,
			call: func(client *Client) (any, error) {
				return client.CreateDepositAddress(context.Background(), CreateDepositAddressRequest{
					Currency: "USDT", Chain: "trx", To: "trade",
				})
			},
			check: func(t *testing.T, value any) {
				if value.(DepositAddress).To != "TRADE" {
					t.Fatalf("CreateDepositAddress() = %+v", value)
				}
			},
		},
		{
			name: "deposits", method: "GET", path: "/api/v1/deposits",
			query:    url.Values{"currency": {"BTC"}, "status": {"SUCCESS"}},
			response: `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":1,"totalPage":1,"items":[{"id":"d1","currency":"BTC","chain":"btc","status":"SUCCESS","address":"a","isInner":false,"amount":"0.1","fee":"0","walletTxId":"tx","createdAt":1700000000000,"updatedAt":1700000001000}]}}`,
			call: func(client *Client) (any, error) {
				return client.Deposits(context.Background(), DepositsRequest{Currency: "BTC", Status: "SUCCESS"})
			},
			check: func(t *testing.T, value any) {
				page := value.(Page[Deposit])
				if len(page.Items) != 1 || page.Items[0].WalletTxID != "tx" || page.Items[0].Amount != "0.1" {
					t.Fatalf("Deposits() = %+v", page)
				}
			},
		},
		{
			name: "withdrawal quotas", method: "GET", path: "/api/v1/withdrawals/quotas",
			query:    url.Values{"currency": {"BTC"}},
			response: `{"code":"200000","data":{"currency":"BTC","limitBTCAmount":"15.79","usedBTCAmount":"0","remainAmount":"15.79","availableAmount":"0.1","withdrawMinFee":"0.0005","isWithdrawEnabled":true,"precision":8,"chain":"btc","lockedAmount":"0"}}`,
			call: func(client *Client) (any, error) {
				return client.WithdrawalQuotas(context.Background(), WithdrawalQuotasRequest{Currency: "BTC"})
			},
			check: func(t *testing.T, value any) {
				if quotas := value.(WithdrawalQuotas); !quotas.WithdrawEnabled || quotas.WithdrawMinFee != "0.0005" {
					t.Fatalf("WithdrawalQuotas() = %+v", quotas)
				}
			},
		},
		{
			name: "withdraw", method: "POST", path: "/api/v3/withdrawals",
			body:     `{"currency":"USDT","toAddress":"T123","amount":"3","withdrawType":"ADDRESS","chain":"trx"}`,
			response: `{"code":"200000","data":{"withdrawalId":"w1"}}`,
			call: func(client *Client) (any, error) {
				return client.Withdraw(context.Background(), WithdrawRequest{
					Currency: "USDT", ToAddress: "T123", Amount: "3", WithdrawType: WithdrawTypeAddress, Chain: "trx",
				})
			},
			check: func(t *testing.T, value any) {
				if value.(WithdrawalReference).WithdrawalID != "w1" {
					t.Fatalf("Withdraw() = %+v", value)
				}
			},
		},
		{
			name: "cancel withdrawal", method: "DELETE", path: "/api/v1/withdrawals/w1",
			response: `{"code":"200000","data":null}`,
			call:     func(client *Client) (any, error) { return client.CancelWithdrawal(context.Background(), "w1") },
			check: func(t *testing.T, value any) {
				if len(value.(Acknowledgement).Raw) != 0 {
					t.Fatalf("CancelWithdrawal() = %+v", value)
				}
			},
		},
		{
			name: "withdrawals", method: "GET", path: "/api/v1/withdrawals",
			query:    url.Values{"currency": {"BTC"}, "pageSize": {"10"}},
			response: `{"code":"200000","data":{"currentPage":1,"pageSize":10,"totalNum":1,"totalPage":1,"items":[{"id":"w1","currency":"BTC","chain":"btc","status":"SUCCESS","address":"a","amount":"0.1","fee":"0.0005","createdAt":1700000000000}]}}`,
			call: func(client *Client) (any, error) {
				return client.Withdrawals(context.Background(), WithdrawalsRequest{Currency: "BTC", PageSize: 10})
			},
			check: func(t *testing.T, value any) {
				if page := value.(Page[Withdrawal]); len(page.Items) != 1 || page.Items[0].Fee != "0.0005" {
					t.Fatalf("Withdrawals() = %+v", page)
				}
			},
		},
		{
			name: "withdrawal by ID", method: "GET", path: "/api/v1/withdrawals/w1",
			response: `{"code":"200000","data":{"id":"w1","uid":1,"currency":"BTC","chainId":"btc","status":"SUCCESS","amount":"0.1","fee":"0.0005","taxes":[],"txId":"tx"}}`,
			call:     func(client *Client) (any, error) { return client.WithdrawalByID(context.Background(), "w1") },
			check: func(t *testing.T, value any) {
				if detail := value.(WithdrawalDetail); detail.TxID != "tx" || detail.ChainID != "btc" {
					t.Fatalf("WithdrawalByID() = %+v", detail)
				}
			},
		},
		{
			name: "transferable", method: "GET", path: "/api/v1/accounts/transferable",
			query:    url.Values{"currency": {"USDT"}, "type": {"ISOLATED"}, "tag": {"BTC-USDT"}},
			response: `{"code":"200000","data":{"currency":"USDT","balance":"10","available":"8","holds":"2","transferable":"8"}}`,
			call: func(client *Client) (any, error) {
				return client.Transferable(context.Background(), TransferableRequest{Currency: "USDT", Type: "ISOLATED", Tag: "BTC-USDT"})
			},
			check: func(t *testing.T, value any) {
				if value.(Transferable).Transferable != "8" {
					t.Fatalf("Transferable() = %+v", value)
				}
			},
		},
		{
			name: "flex transfer", method: "POST", path: "/api/v3/accounts/universal-transfer",
			body:     `{"clientOid":"t1","currency":"USDT","amount":"1","type":"PARENT_TO_SUB","fromAccountType":"MAIN","toUserId":"u1","toAccountType":"TRADE"}`,
			response: `{"code":"200000","data":{"orderId":"tr1"}}`,
			call: func(client *Client) (any, error) {
				return client.FlexTransfer(context.Background(), FlexTransferRequest{
					ClientOrderID: "t1", Currency: "USDT", Amount: "1", Type: FlexTransferParentToSub,
					FromAccountType: "MAIN", ToUserID: "u1", ToAccountType: "TRADE",
				})
			},
			check: func(t *testing.T, value any) {
				if value.(TransferReference).OrderID != "tr1" {
					t.Fatalf("FlexTransfer() = %+v", value)
				}
			},
		},
		{
			name: "create sub account", method: "POST", path: "/api/v2/sub/user/created",
			body:     `{"password":"Passw0rd1","remarks":"r","subName":"subaccount1","access":"Spot,Margin"}`,
			response: `{"code":"200000","data":{"uid":9969082973,"subName":"subaccount1","remarks":"r","access":"Spot,Margin"}}`,
			call: func(client *Client) (any, error) {
				return client.CreateSubAccount(context.Background(), CreateSubAccountRequest{
					Password: "Passw0rd1", Remarks: "r", SubName: "subaccount1", Access: "Spot,Margin",
				})
			},
			check: func(t *testing.T, value any) {
				if value.(SubAccount).UID != 9969082973 {
					t.Fatalf("CreateSubAccount() = %+v", value)
				}
			},
		},
		{
			name: "enable sub account margin", method: "POST", path: "/api/v3/sub/user/margin/enable",
			body:     `{"uid":"u1"}`,
			response: `{"code":"200000","data":null}`,
			call: func(client *Client) (any, error) {
				return client.EnableSubAccountMargin(context.Background(), SubAccountPermissionRequest{UID: "u1"})
			},
			check: func(t *testing.T, _ any) {},
		},
		{
			name: "enable sub account futures", method: "POST", path: "/api/v3/sub/user/futures/enable",
			body:     `{"uid":"u1"}`,
			response: `{"code":"200000","data":true}`,
			call: func(client *Client) (any, error) {
				return client.EnableSubAccountFutures(context.Background(), SubAccountPermissionRequest{UID: "u1"})
			},
			check: func(t *testing.T, value any) {
				if string(value.(Acknowledgement).Raw) != "true" {
					t.Fatalf("EnableSubAccountFutures() = %+v", value)
				}
			},
		},
		{
			name: "sub accounts", method: "GET", path: "/api/v2/sub/user",
			query:    url.Values{"pageSize": {"10"}},
			response: `{"code":"200000","data":{"currentPage":1,"pageSize":10,"totalNum":1,"totalPage":1,"items":[{"userId":"u1","uid":1,"subName":"subaccount1","status":2,"type":0,"access":"Spot","createdAt":1700000000000,"remarks":"r","tradeTypes":["Spot"],"openedTradeTypes":["Spot"],"hostedStatus":""}]}}`,
			call: func(client *Client) (any, error) {
				return client.SubAccounts(context.Background(), SubAccountsRequest{PageSize: 10})
			},
			check: func(t *testing.T, value any) {
				page := value.(Page[SubAccountSummary])
				if len(page.Items) != 1 || page.Items[0].UserID != "u1" || page.Items[0].TradeTypes[0] != "Spot" {
					t.Fatalf("SubAccounts() = %+v", page)
				}
			},
		},
		{
			name: "sub account balance detail", method: "GET", path: "/api/v1/sub-accounts/u1",
			query:    url.Values{"includeBaseAmount": {"true"}, "baseCurrency": {"USDT"}},
			response: `{"code":"200000","data":{"subUserId":"u1","subName":"subaccount1","mainAccounts":[{"currency":"USDT","balance":"1","available":"1","holds":"0","baseCurrency":"USDT","baseCurrencyPrice":"1","baseAmount":"1","tag":"DEFAULT"}],"tradeAccounts":[],"marginAccounts":[],"tradeHFAccounts":[]}}`,
			call: func(client *Client) (any, error) {
				return client.SubAccountBalanceDetail(context.Background(), SubAccountBalanceRequest{
					SubUserID: "u1", IncludeBaseAmount: true, BaseCurrency: "USDT",
				})
			},
			check: func(t *testing.T, value any) {
				if balances := value.(SubAccountBalances); len(balances.MainAccounts) != 1 || balances.MainAccounts[0].Tag != "DEFAULT" {
					t.Fatalf("SubAccountBalanceDetail() = %+v", balances)
				}
			},
		},
		{
			name: "sub account spot balances", method: "GET", path: "/api/v2/sub-accounts",
			query:    url.Values{"currentPage": {"1"}, "pageSize": {"10"}},
			response: `{"code":"200000","data":{"currentPage":1,"pageSize":10,"totalNum":1,"totalPage":1,"items":[{"subUserId":"u1","subName":"subaccount1","mainAccounts":[],"tradeAccounts":[],"marginAccounts":[],"tradeHFAccounts":[]}]}}`,
			call: func(client *Client) (any, error) {
				return client.SubAccountSpotBalances(context.Background(), SubAccountsRequest{CurrentPage: 1, PageSize: 10})
			},
			check: func(t *testing.T, value any) {
				if page := value.(Page[SubAccountBalances]); len(page.Items) != 1 || page.Items[0].SubUserID != "u1" {
					t.Fatalf("SubAccountSpotBalances() = %+v", page)
				}
			},
		},
		{
			name: "sub api keys", method: "GET", path: "/api/v1/sub/api-key",
			query:    url.Values{"subName": {"subaccount1"}},
			response: `{"code":"200000","data":[{"subName":"subaccount1","remark":"r","apiKey":"k","apiVersion":3,"permission":"General","createdAt":1700000000000,"uid":1,"isMaster":false}]}`,
			call: func(client *Client) (any, error) {
				return client.SubAPIKeys(context.Background(), SubAPIKeysRequest{SubName: "subaccount1"})
			},
			check: func(t *testing.T, value any) {
				if keys := value.([]SubAPIKey); len(keys) != 1 || keys[0].APIKey != "k" {
					t.Fatalf("SubAPIKeys() = %+v", keys)
				}
			},
		},
		{
			name: "create sub api key", method: "POST", path: "/api/v1/sub/api-key",
			body:     `{"subName":"subaccount1","passphrase":"passphrase1","remark":"r","permission":"General,Spot","expire":"30"}`,
			response: `{"code":"200000","data":{"subName":"subaccount1","remark":"r","apiKey":"k","apiSecret":"s","apiVersion":3,"passphrase":"passphrase1","permission":"General,Spot","createdAt":1700000000000}}`,
			call: func(client *Client) (any, error) {
				return client.CreateSubAPIKey(context.Background(), CreateSubAPIKeyRequest{
					SubName: "subaccount1", Passphrase: "passphrase1", Remark: "r", Permission: "General,Spot", Expire: "30",
				})
			},
			check: func(t *testing.T, value any) {
				if key := value.(CreatedSubAPIKey); key.APISecret != "s" || key.APIKey != "k" {
					t.Fatalf("CreateSubAPIKey() = %+v", key)
				}
			},
		},
		{
			name: "modify sub api key", method: "POST", path: "/api/v1/sub/api-key/update",
			body:     `{"subName":"subaccount1","apiKey":"k","passphrase":"passphrase1","permission":"General"}`,
			response: `{"code":"200000","data":{"subName":"subaccount1","apiKey":"k","permission":"General"}}`,
			call: func(client *Client) (any, error) {
				return client.ModifySubAPIKey(context.Background(), ModifySubAPIKeyRequest{
					SubName: "subaccount1", APIKey: "k", Passphrase: "passphrase1", Permission: "General",
				})
			},
			check: func(t *testing.T, value any) {
				if value.(ModifiedSubAPIKey).Permission != "General" {
					t.Fatalf("ModifySubAPIKey() = %+v", value)
				}
			},
		},
		{
			name: "delete sub api key", method: "DELETE", path: "/api/v1/sub/api-key",
			query:    url.Values{"subName": {"subaccount1"}, "apiKey": {"k"}, "passphrase": {"passphrase1"}},
			response: `{"code":"200000","data":{"subName":"subaccount1","apiKey":"k"}}`,
			call: func(client *Client) (any, error) {
				return client.DeleteSubAPIKey(context.Background(), DeleteSubAPIKeyRequest{
					SubName: "subaccount1", APIKey: "k", Passphrase: "passphrase1",
				})
			},
			check: func(t *testing.T, value any) {
				if value.(DeletedSubAPIKey).APIKey != "k" {
					t.Fatalf("DeleteSubAPIKey() = %+v", value)
				}
			},
		},
	})
}

func TestExtValidationAndPermissions(t *testing.T) {
	t.Parallel()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	client := newExtTestClient(t, server.URL, credential.PermissionRead, credential.PermissionTrade)
	ctx := context.Background()

	validationCases := []struct {
		name string
		call func() error
	}{
		{"modify without new value", func() error {
			_, err := client.ModifyOrder(ctx, ModifyOrderRequest{Symbol: "BTC-USDT", OrderID: "o1"})
			return err
		}},
		{"modify with both identities", func() error {
			_, err := client.ModifyOrder(ctx, ModifyOrderRequest{
				Symbol: "BTC-USDT", OrderID: "o1", ClientOrderID: "c1", NewSize: "1",
			})
			return err
		}},
		{"batch empty", func() error {
			_, err := client.BatchPlaceOrders(ctx, BatchOrdersRequest{})
			return err
		}},
		{"batch over limit", func() error {
			orders := make([]PlaceOrderRequest, 21)
			for index := range orders {
				orders[index] = PlaceOrderRequest{
					ClientOrderID: "c" + string(rune('a'+index)), Symbol: "BTC-USDT", Type: OrderTypeLimit,
					Side: SideBuy, Price: "1", Size: "1",
				}
			}
			_, err := client.BatchPlaceOrders(ctx, BatchOrdersRequest{Orders: orders})
			return err
		}},
		{"batch market order", func() error {
			_, err := client.BatchPlaceOrders(ctx, BatchOrdersRequest{Orders: []PlaceOrderRequest{{
				ClientOrderID: "c1", Symbol: "BTC-USDT", Type: OrderTypeMarket, Side: SideSell, Size: "1",
			}}})
			return err
		}},
		{"batch duplicate client ID", func() error {
			order := PlaceOrderRequest{
				ClientOrderID: "c1", Symbol: "BTC-USDT", Type: OrderTypeLimit, Side: SideBuy, Price: "1", Size: "1",
			}
			_, err := client.BatchPlaceOrders(ctx, BatchOrdersRequest{Orders: []PlaceOrderRequest{order, order}})
			return err
		}},
		{"dead cancel timeout", func() error {
			_, err := client.DeadCancelAll(ctx, DeadCancelRequest{Timeout: 4})
			return err
		}},
		{"closed orders limit", func() error {
			_, err := client.ClosedOrders(ctx, ClosedOrdersRequest{Symbol: "BTC-USDT", Limit: 101})
			return err
		}},
		{"stop order bad direction", func() error {
			_, err := client.PlaceStopOrder(ctx, StopOrderRequest{
				PlaceOrderRequest: PlaceOrderRequest{
					ClientOrderID: "s1", Symbol: "BTC-USDT", Type: OrderTypeMarket, Side: SideSell, Size: "1",
				},
				StopPrice: "1", Stop: "up",
			})
			return err
		}},
		{"oco missing limit price", func() error {
			_, err := client.PlaceOCOOrder(ctx, OCOOrderRequest{
				ClientOrderID: "o1", Side: SideSell, Symbol: "BTC-USDT", Price: "1", Size: "1", StopPrice: "1",
			})
			return err
		}},
		{"trade fees over limit", func() error {
			symbols := make([]string, 11)
			for index := range symbols {
				symbols[index] = "BTC-USDT"
			}
			_, err := client.TradeFees(ctx, symbols)
			return err
		}},
		{"sub account weak password", func() error {
			_, err := client.CreateSubAccount(ctx, CreateSubAccountRequest{
				Password: "abcdefgh", SubName: "subaccount1", Access: "Spot",
			})
			return err
		}},
		{"flex transfer missing toUserId", func() error {
			_, err := client.FlexTransfer(ctx, FlexTransferRequest{
				ClientOrderID: "t1", Currency: "USDT", Amount: "1", Type: FlexTransferParentToSub,
				FromAccountType: "MAIN", ToAccountType: "TRADE",
			})
			return err
		}},
		{"transferable isolated without tag", func() error {
			_, err := client.Transferable(ctx, TransferableRequest{Currency: "USDT", Type: "ISOLATED"})
			return err
		}},
		{"ledgers page size", func() error {
			_, err := client.Ledgers(ctx, LedgersRequest{PageSize: 5})
			return err
		}},
	}
	for _, testCase := range validationCases {
		err := testCase.call()
		var apiError *trade.APIError
		if !errors.As(err, &apiError) || apiError.Category != trade.ErrorValidation {
			t.Errorf("%s: error = %v, want validation error", testCase.name, err)
		}
	}

	permissionCases := []struct {
		name string
		call func() error
	}{
		{"withdraw", func() error {
			_, err := client.Withdraw(ctx, WithdrawRequest{
				Currency: "USDT", ToAddress: "T123", Amount: "3", WithdrawType: WithdrawTypeAddress,
			})
			return err
		}},
		{"cancel withdrawal", func() error {
			_, err := client.CancelWithdrawal(ctx, "w1")
			return err
		}},
		{"flex transfer", func() error {
			_, err := client.FlexTransfer(ctx, FlexTransferRequest{
				ClientOrderID: "t1", Currency: "USDT", Amount: "1", Type: FlexTransferInternal,
				FromAccountType: "MAIN", ToAccountType: "TRADE",
			})
			return err
		}},
		{"create deposit address", func() error {
			_, err := client.CreateDepositAddress(ctx, CreateDepositAddressRequest{Currency: "USDT", Chain: "trx"})
			return err
		}},
		{"delete sub api key", func() error {
			_, err := client.DeleteSubAPIKey(ctx, DeleteSubAPIKeyRequest{
				SubName: "subaccount1", APIKey: "k", Passphrase: "passphrase1",
			})
			return err
		}},
	}
	for _, testCase := range permissionCases {
		err := testCase.call()
		var apiError *trade.APIError
		if !errors.As(err, &apiError) || apiError.Category != trade.ErrorAuthorization {
			t.Errorf("%s: error = %v, want authorization error", testCase.name, err)
		}
	}
	if calls != 0 {
		t.Fatalf("server received %d requests for rejected calls", calls)
	}
}

func TestExtMutationUnknownStateIsNotRetried(t *testing.T) {
	t.Parallel()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(writer, `{"code":"500000","msg":"internal"}`, http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newExtTestClient(t, server.URL, credential.PermissionWithdraw)
	_, err := client.Withdraw(context.Background(), WithdrawRequest{
		Currency: "USDT", ToAddress: "T123", Amount: "3", WithdrawType: WithdrawTypeAddress,
	})
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.Category != trade.ErrorUnknownExecutionState || apiError.Retryable {
		t.Fatalf("Withdraw() error = %v, want unknown execution state", err)
	}
	if calls != 1 {
		t.Fatalf("server calls = %d, want 1", calls)
	}
}
