package gateio

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

type extCase struct {
	name      string
	method    string
	path      string
	query     string
	body      string
	response  string
	signed    bool
	call      func(*Client) (any, error)
	checkData func(*testing.T, any)
}

func newExtClient(t *testing.T, baseURL string, permissions ...credential.Permission) *Client {
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
			AccountID: "gateio-main", Exchange: model.ExchangeGateIO, SecretRef: "secret/gateio-main",
			Permissions:           permissions,
			AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
		},
		CredentialProvider: &recordingProvider{}, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
		Now: func() time.Time { return time.Unix(1_700_000_000, 0) },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

var allPermissions = []credential.Permission{
	credential.PermissionRead, credential.PermissionTrade,
	credential.PermissionTransfer, credential.PermissionWithdraw,
}

func runExtCases(t *testing.T, cases []extCase) {
	t.Helper()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var gotMethod, gotPath, gotQuery, gotBody string
			signatureOK := true
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if testCase.signed {
					signatureOK = verifySignedRequest(t, request, []byte("test-secret"))
				}
				body, _ := io.ReadAll(request.Body)
				gotMethod, gotPath, gotQuery, gotBody = request.Method, request.URL.EscapedPath(), request.URL.RawQuery, string(body)
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, testCase.response)
			}))
			defer server.Close()
			client := newExtClient(t, server.URL, allPermissions...)
			data, err := testCase.call(client)
			if err != nil {
				t.Fatalf("call error = %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if gotMethod != testCase.method || gotPath != "/api/v4"+testCase.path {
				t.Fatalf("request = %s %s, want %s /api/v4%s", gotMethod, gotPath, testCase.method, testCase.path)
			}
			if gotQuery != testCase.query {
				t.Fatalf("query = %q, want %q", gotQuery, testCase.query)
			}
			if gotBody != testCase.body {
				t.Fatalf("body = %q, want %q", gotBody, testCase.body)
			}
			if !signatureOK {
				t.Fatal("request signature verification failed")
			}
			if testCase.checkData != nil {
				testCase.checkData(t, data)
			}
		})
	}
}

func wantTrue(t *testing.T, condition bool, format string, args ...any) {
	t.Helper()
	if !condition {
		t.Fatalf(format, args...)
	}
}

const finishedOrderJSON = `{"id":"order-9","text":"t-a","create_time":"1700000000","update_time":"1700000005","create_time_ms":1700000000000,"update_time_ms":1700000005000,"status":"closed","currency_pair":"BTC_USDT","type":"limit","account":"spot","side":"buy","amount":"0.1","price":"64000","time_in_force":"gtc","left":"0","filled_amount":"0.1","filled_total":"6400","avg_deal_price":"64000","fee":"0.0002","fee_currency":"BTC","finish_as":"filled"}`

func TestOrderExtensions(t *testing.T) {
	t.Parallel()
	limit := PlaceOrderRequest{
		ClientOrderID: "t-a", CurrencyPair: "BTC_USDT", Type: OrderTypeLimit, Account: "spot",
		Side: SideBuy, Amount: "0.1", Price: "64000", TimeInForce: TimeInForceGTC,
	}
	from := time.Unix(1_699_000_000, 0)
	to := time.Unix(1_700_000_000, 0)
	runExtCases(t, []extCase{
		{
			name: "finished orders", method: http.MethodGet, path: "/spot/orders", signed: true,
			query:    "account=spot&currency_pair=BTC_USDT&from=1699000000&limit=50&page=2&side=buy&status=finished&to=1700000000",
			response: `[` + finishedOrderJSON + `]`,
			call: func(c *Client) (any, error) {
				return c.FinishedOrders(context.Background(), FinishedOrdersRequest{
					CurrencyPair: "BTC_USDT", Side: SideBuy, From: &from, To: &to, Page: 2, Limit: 50,
				})
			},
			checkData: func(t *testing.T, data any) {
				orders := data.([]Order)
				wantTrue(t, len(orders) == 1 && orders[0].FinishAs == "filled" &&
					orders[0].FilledTotal == "6400" && len(orders[0].Raw) > 0, "orders = %+v", orders)
			},
		},
		{
			name: "batch orders", method: http.MethodPost, path: "/spot/batch_orders", signed: true,
			body:     `[{"text":"t-a","currency_pair":"BTC_USDT","type":"limit","account":"spot","side":"buy","amount":"0.1","price":"64000","time_in_force":"gtc"}]`,
			response: `[{"order_id":"order-9","succeeded":true,"text":"t-a","id":"order-9","currency_pair":"BTC_USDT","status":"open"},{"succeeded":false,"label":"BALANCE_NOT_ENOUGH","message":"no","text":"t-b"}]`,
			call: func(c *Client) (any, error) {
				return c.PlaceBatchOrders(context.Background(), PlaceBatchOrdersRequest{Orders: []PlaceOrderRequest{limit}})
			},
			checkData: func(t *testing.T, data any) {
				items := data.([]BatchOrder)
				wantTrue(t, len(items) == 2 && items[0].Succeeded && items[0].ID == "order-9" &&
					!items[1].Succeeded && items[1].Label == "BALANCE_NOT_ENOUGH" && len(items[1].Raw) > 0, "items = %+v", items)
			},
		},
		{
			name: "amend order", method: http.MethodPatch, path: "/spot/orders/order-9", signed: true,
			query: "account=spot&currency_pair=BTC_USDT", body: `{"price":"63000","amend_text":"fix"}`,
			response: finishedOrderJSON,
			call: func(c *Client) (any, error) {
				return c.AmendOrder(context.Background(), AmendOrderRequest{
					OrderID: "order-9", CurrencyPair: "BTC_USDT", Price: "63000", AmendText: "fix",
				})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(Order).ID == "order-9", "order = %+v", data)
			},
		},
		{
			name: "amend batch orders", method: http.MethodPost, path: "/spot/amend_batch_orders", signed: true,
			body:     `[{"order_id":"order-9","currency_pair":"BTC_USDT","amount":"0.2"}]`,
			response: `[{"order_id":"order-9","succeeded":true,"id":"order-9","amend_text":"x"}]`,
			call: func(c *Client) (any, error) {
				return c.AmendBatchOrders(context.Background(), AmendBatchOrdersRequest{
					Items: []AmendBatchItem{{OrderID: "order-9", CurrencyPair: "BTC_USDT", Amount: "0.2"}},
				})
			},
			checkData: func(t *testing.T, data any) {
				items := data.([]BatchOrder)
				wantTrue(t, len(items) == 1 && items[0].Succeeded && items[0].AmendText == "x", "items = %+v", items)
			},
		},
		{
			name: "cancel all orders", method: http.MethodDelete, path: "/spot/orders", signed: true,
			query: "account=spot&currency_pair=BTC_USDT&side=sell", response: `[` + finishedOrderJSON + `]`,
			call: func(c *Client) (any, error) {
				return c.CancelAllOrders(context.Background(), CancelAllOrdersRequest{CurrencyPair: "BTC_USDT", Side: SideSell})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, len(data.([]Order)) == 1, "orders = %+v", data)
			},
		},
		{
			name: "cancel batch orders", method: http.MethodPost, path: "/spot/cancel_batch_orders", signed: true,
			body:     `[{"currency_pair":"BTC_USDT","id":"order-9"}]`,
			response: `[{"currency_pair":"BTC_USDT","id":"order-9","succeeded":false,"label":"ORDER_NOT_FOUND","message":"gone"}]`,
			call: func(c *Client) (any, error) {
				return c.CancelBatchOrders(context.Background(), CancelBatchOrdersRequest{
					Orders: []CancelBatchOrderItem{{CurrencyPair: "BTC_USDT", ID: "order-9"}},
				})
			},
			checkData: func(t *testing.T, data any) {
				items := data.([]CancelOrderResult)
				wantTrue(t, len(items) == 1 && !items[0].Succeeded && items[0].Label == "ORDER_NOT_FOUND", "items = %+v", items)
			},
		},
		{
			name: "countdown cancel all", method: http.MethodPost, path: "/spot/countdown_cancel_all", signed: true,
			body: `{"timeout":30,"currency_pair":"BTC_USDT"}`, response: `{"triggerTime":1700000030000}`,
			call: func(c *Client) (any, error) {
				return c.CountdownCancelAll(context.Background(), CountdownCancelAllRequest{TimeoutSeconds: 30, CurrencyPair: "BTC_USDT"})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(CountdownTrigger).TriggerTimeMilli == 1700000030000, "trigger = %+v", data)
			},
		},
		{
			name: "price orders list", method: http.MethodGet, path: "/spot/price_orders", signed: true,
			query:    "limit=10&market=BTC_USDT&offset=5&status=open",
			response: `[{"trigger":{"price":"60000","rule":"<=","expiration":3600},"put":{"type":"limit","side":"buy","price":"59900","amount":"0.1","account":"normal","time_in_force":"gtc"},"id":12345,"user":7,"market":"BTC_USDT","ctime":1700000000,"ftime":0,"fired_order_id":0,"status":"open","reason":""}]`,
			call: func(c *Client) (any, error) {
				return c.PriceOrders(context.Background(), PriceOrdersRequest{
					Status: PriceOrderOpen, Market: "BTC_USDT", Limit: 10, Offset: 5,
				})
			},
			checkData: func(t *testing.T, data any) {
				orders := data.([]PriceOrder)
				wantTrue(t, len(orders) == 1 && orders[0].ID == 12345 && orders[0].Trigger.Rule == "<=" &&
					orders[0].Put.Amount == "0.1" && len(orders[0].Raw) > 0, "orders = %+v", orders)
			},
		},
		{
			name: "place price order", method: http.MethodPost, path: "/spot/price_orders", signed: true,
			body:     `{"trigger":{"price":"60000","rule":"\u003c=","expiration":3600},"put":{"type":"limit","side":"buy","price":"59900","amount":"0.1","account":"normal","time_in_force":"gtc"},"market":"BTC_USDT"}`,
			response: `{"id":12345,"id_string":"12345"}`,
			call: func(c *Client) (any, error) {
				return c.PlacePriceOrder(context.Background(), PlacePriceOrderRequest{
					Market: "BTC_USDT", Trigger: PriceOrderTrigger{Price: "60000", Rule: "<=", ExpirationSeconds: 3600},
					Type: OrderTypeLimit, Side: SideBuy, Price: "59900", Amount: "0.1", TimeInForce: TimeInForceGTC,
				})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(PriceOrderAck).ID == 12345 && data.(PriceOrderAck).IDString == "12345", "ack = %+v", data)
			},
		},
		{
			name: "cancel all price orders", method: http.MethodDelete, path: "/spot/price_orders", signed: true,
			query: "market=BTC_USDT", response: `[{"id":12345,"market":"BTC_USDT","status":"cancelled"}]`,
			call: func(c *Client) (any, error) {
				return c.CancelAllPriceOrders(context.Background(), CancelAllPriceOrdersRequest{Market: "BTC_USDT"})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.([]PriceOrder)[0].Status == "cancelled", "orders = %+v", data)
			},
		},
		{
			name: "price order info", method: http.MethodGet, path: "/spot/price_orders/12345", signed: true,
			response: `{"id":12345,"market":"BTC_USDT","status":"finish","fired_order_id":99}`,
			call:     func(c *Client) (any, error) { return c.PriceOrder(context.Background(), "12345") },
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(PriceOrder).FiredOrderID == 99, "order = %+v", data)
			},
		},
		{
			name: "cancel price order", method: http.MethodDelete, path: "/spot/price_orders/12345", signed: true,
			response: `{"id":12345,"market":"BTC_USDT","status":"cancelled"}`,
			call:     func(c *Client) (any, error) { return c.CancelPriceOrder(context.Background(), "12345") },
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(PriceOrder).Status == "cancelled", "order = %+v", data)
			},
		},
	})
}

func TestAccountExtensions(t *testing.T) {
	t.Parallel()
	from := time.Unix(1_699_900_000, 0)
	to := time.Unix(1_700_000_000, 0)
	runExtCases(t, []extCase{
		{
			name: "currencies", method: http.MethodGet, path: "/spot/currencies",
			response: `[{"currency":"BTC","name":"Bitcoin","delisted":false,"trade_disabled":false,"chain":"BTC","chains":[{"name":"BTC","addr":"","withdraw_disabled":false,"withdraw_delayed":false,"deposit_disabled":true}]}]`,
			call:     func(c *Client) (any, error) { return c.Currencies(context.Background()) },
			checkData: func(t *testing.T, data any) {
				items := data.([]Currency)
				wantTrue(t, len(items) == 1 && items[0].Chains[0].DepositDisabled && len(items[0].Raw) > 0, "items = %+v", items)
			},
		},
		{
			name: "currency info", method: http.MethodGet, path: "/spot/currencies/BTC",
			response: `{"currency":"BTC","name":"Bitcoin","chain":"BTC"}`,
			call:     func(c *Client) (any, error) { return c.CurrencyInfo(context.Background(), "BTC") },
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(Currency).Name == "Bitcoin", "currency = %+v", data)
			},
		},
		{
			name: "currency chains", method: http.MethodGet, path: "/wallet/currency_chains", query: "currency=USDT",
			response: `[{"chain":"TRX","name_cn":"波场","name_en":"Tron","contract_address":"TR7","is_disabled":0,"is_deposit_disabled":0,"is_withdraw_disabled":1,"decimal":"6","is_tag":0}]`,
			call:     func(c *Client) (any, error) { return c.CurrencyChains(context.Background(), "USDT") },
			checkData: func(t *testing.T, data any) {
				items := data.([]CurrencyChain)
				wantTrue(t, len(items) == 1 && items[0].IsWithdrawDisabled == 1 && items[0].Decimal == "6", "items = %+v", items)
			},
		},
		{
			name: "spot fee", method: http.MethodGet, path: "/spot/fee", query: "currency_pair=BTC_USDT", signed: true,
			response: `{"user_id":7,"taker_fee":"0.002","maker_fee":"0.002","gt_discount":true,"gt_taker_fee":"0.0015","gt_maker_fee":"0.0015","currency_pair":"BTC_USDT","debit_fee":1}`,
			call: func(c *Client) (any, error) {
				return c.SpotFee(context.Background(), SpotFeeRequest{CurrencyPair: "BTC_USDT"})
			},
			checkData: func(t *testing.T, data any) {
				fee := data.(SpotFee)
				wantTrue(t, fee.TakerFee == "0.002" && fee.GTDiscount && fee.DebitFee == 1, "fee = %+v", fee)
			},
		},
		{
			name: "batch spot fee", method: http.MethodGet, path: "/spot/batch_fee", query: "currency_pairs=BTC_USDT%2CETH_USDT", signed: true,
			response: `{"BTC_USDT":{"taker_fee":"0.002","maker_fee":"0.001"},"ETH_USDT":{"taker_fee":"0.003","maker_fee":"0.002"}}`,
			call: func(c *Client) (any, error) {
				return c.BatchSpotFee(context.Background(), BatchSpotFeeRequest{CurrencyPairs: []string{"BTC_USDT", "ETH_USDT"}})
			},
			checkData: func(t *testing.T, data any) {
				fees := data.(map[string]SpotFee)
				wantTrue(t, len(fees) == 2 && fees["ETH_USDT"].TakerFee == "0.003" && len(fees["BTC_USDT"].Raw) > 0, "fees = %+v", fees)
			},
		},
		{
			name: "account book", method: http.MethodGet, path: "/spot/account_book", signed: true,
			query:    "code=trade&currency=USDT&from=1699900000&limit=20&page=1&to=1700000000",
			response: `[{"id":"1","time":1700000000123,"currency":"USDT","change":"-100","balance":"900","type":"trade","code":"trade","text":""}]`,
			call: func(c *Client) (any, error) {
				return c.AccountBook(context.Background(), AccountBookRequest{
					Currency: "USDT", From: &from, To: &to, Page: 1, Limit: 20, Code: "trade",
				})
			},
			checkData: func(t *testing.T, data any) {
				items := data.([]AccountBookEntry)
				wantTrue(t, len(items) == 1 && items[0].Change == "-100" && items[0].Time == 1700000000123, "items = %+v", items)
			},
		},
		{
			name: "wallet trade fee", method: http.MethodGet, path: "/wallet/fee", query: "currency_pair=BTC_USDT&settle=usdt", signed: true,
			response: `{"user_id":7,"taker_fee":"0.002","futures_taker_fee":"0.0005","delivery_maker_fee":"0.0001"}`,
			call: func(c *Client) (any, error) {
				return c.WalletTradeFee(context.Background(), TradeFeeRequest{CurrencyPair: "BTC_USDT", Settle: "USDT"})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(TradeFee).FuturesTakerFee == "0.0005", "fee = %+v", data)
			},
		},
		{
			name: "total balance", method: http.MethodGet, path: "/wallet/total_balance", query: "currency=BTC", signed: true,
			response: `{"total":{"amount":"1.5","currency":"BTC"},"details":{"spot":{"amount":"1","currency":"BTC"},"futures":{"amount":"0.5","currency":"BTC","unrealised_pnl":"0.01"}}}`,
			call:     func(c *Client) (any, error) { return c.TotalBalance(context.Background(), "BTC") },
			checkData: func(t *testing.T, data any) {
				balance := data.(TotalBalance)
				wantTrue(t, balance.Total.Amount == "1.5" && balance.Details["futures"].UnrealisedPnl == "0.01", "balance = %+v", balance)
			},
		},
		{
			name: "small balances", method: http.MethodGet, path: "/wallet/small_balance", signed: true,
			response: `[{"currency":"DOGE","available_balance":"1","estimated_as_btc":"0.00001","convertible_to_gt":"0.01"}]`,
			call:     func(c *Client) (any, error) { return c.SmallBalances(context.Background()) },
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.([]SmallBalance)[0].ConvertibleToGT == "0.01", "balances = %+v", data)
			},
		},
		{
			name: "convert small balance", method: http.MethodPost, path: "/wallet/small_balance", signed: true,
			body: `{"currency":["DOGE"]}`, response: `{}`,
			call: func(c *Client) (any, error) {
				return nil, c.ConvertSmallBalance(context.Background(), ConvertSmallBalanceRequest{Currencies: []string{"DOGE"}})
			},
		},
		{
			name: "small balance history", method: http.MethodGet, path: "/wallet/small_balance_history", query: "currency=DOGE&limit=10&page=1", signed: true,
			response: `[{"id":"1","currency":"DOGE","amount":"1","gt_amount":"0.01","create_time":1700000000}]`,
			call: func(c *Client) (any, error) {
				return c.SmallBalanceHistory(context.Background(), SmallBalanceHistoryRequest{Currency: "DOGE", Page: 1, Limit: 10})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.([]SmallBalanceHistory)[0].GTAmount == "0.01", "history = %+v", data)
			},
		},
	})
}

func TestWalletExtensions(t *testing.T) {
	t.Parallel()
	from := time.Unix(1_699_900_000, 0)
	to := time.Unix(1_700_000_000, 0)
	runExtCases(t, []extCase{
		{
			name: "deposit address", method: http.MethodGet, path: "/wallet/deposit_address", query: "currency=USDT", signed: true,
			response: `{"currency":"USDT","address":"0xabc","multichain_addresses":[{"chain":"TRX","address":"T1","payment_id":"","payment_name":"","obtain_failed":0,"min_confirms":20}]}`,
			call:     func(c *Client) (any, error) { return c.DepositAddress(context.Background(), "USDT") },
			checkData: func(t *testing.T, data any) {
				address := data.(DepositAddress)
				wantTrue(t, address.Address == "0xabc" && address.MultichainAddresses[0].MinConfirms == 20, "address = %+v", address)
			},
		},
		{
			name: "deposits", method: http.MethodGet, path: "/wallet/deposits", signed: true,
			query:    "currency=USDT&from=1699900000&limit=100&offset=10&to=1700000000",
			response: `[{"id":"d1","txid":"0x1","timestamp":"1700000000","amount":"10","currency":"USDT","status":"DONE","chain":"TRX"}]`,
			call: func(c *Client) (any, error) {
				return c.Deposits(context.Background(), TransferHistoryRequest{
					Currency: "USDT", From: &from, To: &to, Limit: 100, Offset: 10,
				})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.([]DepositRecord)[0].Status == "DONE", "deposits = %+v", data)
			},
		},
		{
			name: "withdrawals", method: http.MethodGet, path: "/wallet/withdrawals", signed: true,
			query:    "currency=USDT&withdraw_order_id=my-1",
			response: `[{"id":"w1","txid":"0x2","withdraw_order_id":"my-1","amount":"5","fee":"1","currency":"USDT","address":"T1","status":"DONE","chain":"TRX"}]`,
			call: func(c *Client) (any, error) {
				return c.Withdrawals(context.Background(), WithdrawalsRequest{
					TransferHistoryRequest: TransferHistoryRequest{Currency: "USDT"}, WithdrawOrderID: "my-1",
				})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.([]WithdrawalRecord)[0].Fee == "1", "withdrawals = %+v", data)
			},
		},
		{
			name: "withdraw", method: http.MethodPost, path: "/withdrawals", signed: true,
			body:     `{"withdraw_order_id":"my-1","currency":"USDT","chain":"TRX","amount":"5","address":"T1"}`,
			response: `{"id":"w1","withdraw_order_id":"my-1","amount":"5","currency":"USDT","address":"T1","status":"REQUEST","chain":"TRX"}`,
			call: func(c *Client) (any, error) {
				return c.Withdraw(context.Background(), WithdrawRequest{
					WithdrawOrderID: "my-1", Currency: "USDT", Chain: "TRX", Amount: "5", Address: "T1",
				})
			},
			checkData: func(t *testing.T, data any) {
				ack := data.(WithdrawalAck)
				wantTrue(t, ack.ID == "w1" && ack.Status == "REQUEST" && len(ack.Raw) > 0, "ack = %+v", ack)
			},
		},
		{
			name: "cancel withdrawal", method: http.MethodDelete, path: "/withdrawals/w1", signed: true,
			response: `{"id":"w1","amount":"5","currency":"USDT","status":"CANCEL","chain":"TRX"}`,
			call:     func(c *Client) (any, error) { return c.CancelWithdrawal(context.Background(), "w1") },
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(WithdrawalAck).Status == "CANCEL", "ack = %+v", data)
			},
		},
		{
			name: "withdraw status", method: http.MethodGet, path: "/wallet/withdraw_status", query: "currency=USDT", signed: true,
			response: `[{"currency":"USDT","name":"Tether","withdraw_fix":"1","withdraw_fix_on_chains":{"TRX":"1","ETH":"4"}}]`,
			call:     func(c *Client) (any, error) { return c.WithdrawStatuses(context.Background(), "USDT") },
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.([]WithdrawStatus)[0].WithdrawFixOnChains["ETH"] == "4", "status = %+v", data)
			},
		},
		{
			name: "saved addresses", method: http.MethodGet, path: "/wallet/saved_address", query: "chain=TRX&currency=USDT&limit=50&page=1", signed: true,
			response: `[{"currency":"USDT","chain":"TRX","address":"T1","name":"cold","tag":"","verified":"1"}]`,
			call: func(c *Client) (any, error) {
				return c.SavedAddresses(context.Background(), SavedAddressesRequest{Currency: "USDT", Chain: "TRX", Page: 1, Limit: 50})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.([]SavedAddress)[0].Verified == "1", "addresses = %+v", data)
			},
		},
		{
			name: "push to uid", method: http.MethodPost, path: "/withdrawals/push", signed: true,
			body: `{"receive_uid":99,"currency":"USDT","amount":"5"}`, response: `{"id":"501"}`,
			call: func(c *Client) (any, error) {
				return c.PushToUID(context.Background(), UIDPushRequest{ReceiveUID: 99, Currency: "USDT", Amount: "5"})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(UIDPushAck).ID == "501", "ack = %+v", data)
			},
		},
		{
			name: "uid push orders", method: http.MethodGet, path: "/wallet/push", query: "limit=10&transaction_type=deposit", signed: true,
			response: `[{"id":501,"push_uid":7,"receive_uid":99,"currency":"USDT","amount":"5","create_time":1700000000,"status":"DONE","transaction_type":"deposit"}]`,
			call: func(c *Client) (any, error) {
				return c.UIDPushOrders(context.Background(), UIDPushOrdersRequest{Limit: 10, Type: PushOrderDeposit})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.([]UIDPushOrder)[0].ReceiveUID == 99, "orders = %+v", data)
			},
		},
		{
			name: "transfer", method: http.MethodPost, path: "/wallet/transfers", signed: true,
			body:     `{"currency":"USDT","from":"spot","to":"futures","amount":"10","settle":"usdt"}`,
			response: `{"tx_id":1234}`,
			call: func(c *Client) (any, error) {
				return c.Transfer(context.Background(), TransferRequest{
					Currency: "USDT", From: TransferAccountSpot, To: TransferAccountFutures, Amount: "10", Settle: "usdt",
				})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(TransferTx).TxID == 1234, "tx = %+v", data)
			},
		},
		{
			name: "transfer info", method: http.MethodGet, path: "/wallet/transfers", query: "tx_id=1234", signed: true,
			response: `{"tx_id":"1234","status":"success","currency":"USDT","amount":"10","from_account":"spot","to_account":"futures","settle":"usdt","currency_pair":null}`,
			call:     func(c *Client) (any, error) { return c.TransferInfo(context.Background(), "1234") },
			checkData: func(t *testing.T, data any) {
				detail := data.(TransferDetail)
				wantTrue(t, detail.Status == "success" && *detail.Settle == "usdt" && detail.CurrencyPair == nil, "detail = %+v", detail)
			},
		},
		{
			name: "sub account transfer", method: http.MethodPost, path: "/wallet/sub_account_transfers", signed: true,
			body:     `{"sub_account":"1001","sub_account_type":"spot","currency":"USDT","amount":"10","direction":"to","client_order_id":"cid-1"}`,
			response: `{"tx_id":77}`,
			call: func(c *Client) (any, error) {
				return c.TransferWithSubAccount(context.Background(), SubAccountTransferRequest{
					SubAccount: "1001", SubAccountType: "spot", Currency: "USDT", Amount: "10", Direction: "to", ClientOrderID: "cid-1",
				})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(TransferTx).TxID == 77, "tx = %+v", data)
			},
		},
		{
			name: "sub account to sub account", method: http.MethodPost, path: "/wallet/sub_account_to_sub_account", signed: true,
			body:     `{"currency":"USDT","sub_account_from":"1001","sub_account_from_type":"spot","sub_account_to":"1002","sub_account_to_type":"futures","amount":"3"}`,
			response: `{"tx_id":78}`,
			call: func(c *Client) (any, error) {
				return c.SubAccountToSubAccount(context.Background(), SubAccountToSubAccountRequest{
					Currency: "USDT", SubAccountFrom: "1001", SubAccountFromType: "spot",
					SubAccountTo: "1002", SubAccountToType: "futures", Amount: "3",
				})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(TransferTx).TxID == 78, "tx = %+v", data)
			},
		},
		{
			name: "sub account transfers", method: http.MethodGet, path: "/wallet/sub_account_transfers", signed: true,
			query:    "limit=10&offset=2&sub_uid=1001%2C1002",
			response: `[{"timest":"1700000000","uid":"7","sub_account":"1001","sub_account_type":"spot","currency":"USDT","amount":"10","direction":"to","source":"web","client_order_id":"cid-1","status":"success"}]`,
			call: func(c *Client) (any, error) {
				return c.SubAccountTransfers(context.Background(), SubAccountTransfersRequest{
					SubUIDs: []string{"1001", "1002"}, Limit: 10, Offset: 2,
				})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.([]SubAccountTransferRecord)[0].ClientOrderID == "cid-1", "records = %+v", data)
			},
		},
		{
			name: "transfer order status", method: http.MethodGet, path: "/wallet/order_status", query: "client_order_id=cid-1", signed: true,
			response: `{"tx_id":"77","status":"SUCCESS"}`,
			call: func(c *Client) (any, error) {
				return c.TransferOrderStatus(context.Background(), TransferOrderStatusRequest{ClientOrderID: "cid-1"})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(TransferOrderStatus).Status == "SUCCESS", "status = %+v", data)
			},
		},
	})
}

func TestSubAccountExtensions(t *testing.T) {
	t.Parallel()
	runExtCases(t, []extCase{
		{
			name: "sub accounts", method: http.MethodGet, path: "/sub_accounts", query: "type=0", signed: true,
			response: `[{"remark":"bot","login_name":"bot1","email":"a@b.c","state":1,"type":3,"user_id":1001,"create_time":1700000000}]`,
			call: func(c *Client) (any, error) {
				return c.SubAccounts(context.Background(), SubAccountsRequest{IncludeCrossMargin: true})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.([]SubAccount)[0].UserID == 1001, "accounts = %+v", data)
			},
		},
		{
			name: "create sub account", method: http.MethodPost, path: "/sub_accounts", signed: true,
			body:     `{"login_name":"bot1","remark":"bot"}`,
			response: `{"remark":"bot","login_name":"bot1","state":1,"type":1,"user_id":1001}`,
			call: func(c *Client) (any, error) {
				return c.CreateSubAccount(context.Background(), CreateSubAccountRequest{LoginName: "bot1", Remark: "bot"})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(SubAccount).LoginName == "bot1", "account = %+v", data)
			},
		},
		{
			name: "sub account info", method: http.MethodGet, path: "/sub_accounts/1001", signed: true,
			response: `{"login_name":"bot1","user_id":1001}`,
			call:     func(c *Client) (any, error) { return c.SubAccountInfo(context.Background(), "1001") },
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(SubAccount).UserID == 1001, "account = %+v", data)
			},
		},
		{
			name: "sub account keys", method: http.MethodGet, path: "/sub_accounts/1001/keys", signed: true,
			response: `[{"user_id":1001,"mode":1,"name":"k","perms":[{"name":"spot","read_only":false}],"ip_whitelist":["1.1.1.1"],"key":"abcd1234","state":1}]`,
			call:     func(c *Client) (any, error) { return c.SubAccountKeys(context.Background(), "1001") },
			checkData: func(t *testing.T, data any) {
				keys := data.([]SubAccountKey)
				wantTrue(t, len(keys) == 1 && keys[0].Perms[0].Name == "spot" && keys[0].IPWhitelist[0] == "1.1.1.1", "keys = %+v", keys)
			},
		},
		{
			name: "create sub account key", method: http.MethodPost, path: "/sub_accounts/1001/keys", signed: true,
			body:     `{"name":"k","perms":[{"name":"spot","read_only":false}],"ip_whitelist":["1.1.1.1"]}`,
			response: `{"user_id":1001,"name":"k","key":"abcd1234","secret":"s3cret","state":1}`,
			call: func(c *Client) (any, error) {
				return c.CreateSubAccountKey(context.Background(), "1001", SubAccountKeyRequest{
					Name: "k", Perms: []SubAccountKeyPermission{{Name: "spot"}}, IPWhitelist: []string{"1.1.1.1"},
				})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(SubAccountKey).Secret == "s3cret", "key = %+v", data)
			},
		},
		{
			name: "sub account key info", method: http.MethodGet, path: "/sub_accounts/1001/keys/abcd1234", signed: true,
			response: `{"user_id":1001,"key":"abcd1234","state":1}`,
			call:     func(c *Client) (any, error) { return c.SubAccountKeyInfo(context.Background(), "1001", "abcd1234") },
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.(SubAccountKey).Key == "abcd1234", "key = %+v", data)
			},
		},
		{
			name: "update sub account key", method: http.MethodPut, path: "/sub_accounts/1001/keys/abcd1234", signed: true,
			body: `{"name":"k2"}`, response: ``,
			call: func(c *Client) (any, error) {
				return nil, c.UpdateSubAccountKey(context.Background(), "1001", "abcd1234", SubAccountKeyRequest{Name: "k2"})
			},
		},
		{
			name: "delete sub account key", method: http.MethodDelete, path: "/sub_accounts/1001/keys/abcd1234", signed: true,
			response: ``,
			call: func(c *Client) (any, error) {
				return nil, c.DeleteSubAccountKey(context.Background(), "1001", "abcd1234")
			},
		},
		{
			name: "lock sub account", method: http.MethodPost, path: "/sub_accounts/1001/lock", signed: true,
			response: ``,
			call:     func(c *Client) (any, error) { return nil, c.LockSubAccount(context.Background(), "1001") },
		},
		{
			name: "unlock sub account", method: http.MethodPost, path: "/sub_accounts/1001/unlock", signed: true,
			response: ``,
			call:     func(c *Client) (any, error) { return nil, c.UnlockSubAccount(context.Background(), "1001") },
		},
		{
			name: "sub account modes", method: http.MethodGet, path: "/sub_accounts/unified_mode", signed: true,
			response: `[{"user_id":1001,"is_unified":true,"mode":"portfolio"}]`,
			call:     func(c *Client) (any, error) { return c.SubAccountModes(context.Background()) },
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.([]SubUserMode)[0].Mode == "portfolio", "modes = %+v", data)
			},
		},
		{
			name: "sub account balances", method: http.MethodGet, path: "/wallet/sub_account_balances", query: "sub_uid=1001", signed: true,
			response: `[{"uid":"1001","available":{"USDT":"10"},"locking":{"USDT":"1"}}]`,
			call: func(c *Client) (any, error) {
				return c.SubAccountBalances(context.Background(), SubAccountBalancesRequest{SubUIDs: []string{"1001"}})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.([]SubAccountBalance)[0].Available["USDT"] == "10", "balances = %+v", data)
			},
		},
		{
			name: "sub account margin balances", method: http.MethodGet, path: "/wallet/sub_account_margin_balances", signed: true,
			response: `[{"uid":"1001","available":[{"currency_pair":"BTC_USDT"}]}]`,
			call: func(c *Client) (any, error) {
				return c.SubAccountMarginBalances(context.Background(), SubAccountBalancesRequest{})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, len(data.([]SubAccountScopedBalance)[0].Available) > 0, "balances = %+v", data)
			},
		},
		{
			name: "sub account futures balances", method: http.MethodGet, path: "/wallet/sub_account_futures_balances", query: "settle=usdt", signed: true,
			response: `[{"uid":"1001","available":{"usdt":{"total":"1"}}}]`,
			call: func(c *Client) (any, error) {
				return c.SubAccountFuturesBalances(context.Background(), SubAccountBalancesRequest{}, "USDT")
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, data.([]SubAccountScopedBalance)[0].UID == "1001", "balances = %+v", data)
			},
		},
		{
			name: "sub account cross margin balances", method: http.MethodGet, path: "/wallet/sub_account_cross_margin_balances", signed: true,
			response: `[{"uid":"1001","available":{"user_id":1001,"total":"5"}}]`,
			call: func(c *Client) (any, error) {
				return c.SubAccountCrossMarginBalances(context.Background(), SubAccountBalancesRequest{})
			},
			checkData: func(t *testing.T, data any) {
				wantTrue(t, len(data.([]SubAccountScopedBalance)) == 1, "balances = %+v", data)
			},
		},
	})
}

func TestExtensionValidationAndPermissions(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Errorf("unexpected request %s %s", request.Method, request.URL)
		http.Error(writer, `{}`, http.StatusInternalServerError)
	}))
	defer server.Close()
	ctx := context.Background()
	readOnly := newExtClient(t, server.URL, credential.PermissionRead)
	full := newExtClient(t, server.URL, allPermissions...)
	order := PlaceOrderRequest{
		ClientOrderID: "t-a", CurrencyPair: "BTC_USDT", Type: OrderTypeLimit, Account: "spot",
		Side: SideBuy, Amount: "1", Price: "1", TimeInForce: TimeInForceGTC,
	}
	tooManyOrders := make([]PlaceOrderRequest, 11)
	for index := range tooManyOrders {
		tooManyOrders[index] = order
	}
	fivePairs := make([]PlaceOrderRequest, 5)
	for index := range fivePairs {
		fivePairs[index] = order
		fivePairs[index].CurrencyPair = string(rune('A'+index)) + "BC_USDT"
	}

	validationCases := []struct {
		name string
		call func() error
	}{
		{"empty batch", func() error {
			_, err := full.PlaceBatchOrders(ctx, PlaceBatchOrdersRequest{})
			return err
		}},
		{"too many orders per pair", func() error {
			_, err := full.PlaceBatchOrders(ctx, PlaceBatchOrdersRequest{Orders: tooManyOrders})
			return err
		}},
		{"too many pairs", func() error {
			_, err := full.PlaceBatchOrders(ctx, PlaceBatchOrdersRequest{Orders: fivePairs})
			return err
		}},
		{"amend without fields", func() error {
			_, err := full.AmendOrder(ctx, AmendOrderRequest{OrderID: "1", CurrencyPair: "BTC_USDT"})
			return err
		}},
		{"countdown below minimum", func() error {
			_, err := full.CountdownCancelAll(ctx, CountdownCancelAllRequest{TimeoutSeconds: 3})
			return err
		}},
		{"cancel batch over limit", func() error {
			items := make([]CancelBatchOrderItem, 21)
			for index := range items {
				items[index] = CancelBatchOrderItem{CurrencyPair: "BTC_USDT", ID: "1"}
			}
			_, err := full.CancelBatchOrders(ctx, CancelBatchOrdersRequest{Orders: items})
			return err
		}},
		{"price order bad rule", func() error {
			_, err := full.PlacePriceOrder(ctx, PlacePriceOrderRequest{
				Market: "BTC_USDT", Trigger: PriceOrderTrigger{Price: "1", Rule: "=="}, Type: OrderTypeLimit,
				Side: SideBuy, Price: "1", Amount: "1", TimeInForce: TimeInForceGTC,
			})
			return err
		}},
		{"price orders missing status", func() error {
			_, err := full.PriceOrders(ctx, PriceOrdersRequest{})
			return err
		}},
		{"transfer margin without pair", func() error {
			_, err := full.Transfer(ctx, TransferRequest{
				Currency: "USDT", From: TransferAccountSpot, To: TransferAccountMargin, Amount: "1",
			})
			return err
		}},
		{"transfer futures without settle", func() error {
			_, err := full.Transfer(ctx, TransferRequest{
				Currency: "USDT", From: TransferAccountSpot, To: TransferAccountFutures, Amount: "1",
			})
			return err
		}},
		{"withdraw missing order id", func() error {
			_, err := full.Withdraw(ctx, WithdrawRequest{Currency: "USDT", Chain: "TRX", Amount: "1", Address: "T"})
			return err
		}},
		{"sub transfer bad direction", func() error {
			_, err := full.TransferWithSubAccount(ctx, SubAccountTransferRequest{
				SubAccount: "1", Currency: "USDT", Amount: "1", Direction: "up",
			})
			return err
		}},
		{"convert small balance ambiguous", func() error {
			return full.ConvertSmallBalance(ctx, ConvertSmallBalanceRequest{All: true, Currencies: []string{"DOGE"}})
		}},
		{"account book range over 30 days", func() error {
			from, to := time.Unix(1_000_000, 0), time.Unix(1_000_000+31*86400, 0)
			_, err := full.AccountBook(ctx, AccountBookRequest{From: &from, To: &to})
			return err
		}},
	}
	for _, testCase := range validationCases {
		t.Run(testCase.name, func(t *testing.T) {
			var apiError *trade.APIError
			if err := testCase.call(); !errors.As(err, &apiError) || apiError.Category != trade.ErrorValidation {
				t.Fatalf("error = %v, want validation error", err)
			}
		})
	}

	permissionCases := []struct {
		name string
		call func() error
	}{
		{"batch orders need trade", func() error {
			_, err := readOnly.PlaceBatchOrders(ctx, PlaceBatchOrdersRequest{Orders: []PlaceOrderRequest{order}})
			return err
		}},
		{"cancel all needs trade", func() error {
			_, err := readOnly.CancelAllOrders(ctx, CancelAllOrdersRequest{})
			return err
		}},
		{"transfer needs transfer", func() error {
			_, err := readOnly.Transfer(ctx, TransferRequest{
				Currency: "USDT", From: TransferAccountSpot, To: TransferAccountUnified, Amount: "1",
			})
			return err
		}},
		{"withdraw needs withdraw", func() error {
			_, err := newExtClient(t, server.URL, credential.PermissionRead, credential.PermissionTransfer).Withdraw(
				ctx, WithdrawRequest{WithdrawOrderID: "w-1", Currency: "USDT", Chain: "TRX", Amount: "1", Address: "T"},
			)
			return err
		}},
		{"create sub account needs transfer", func() error {
			_, err := readOnly.CreateSubAccount(ctx, CreateSubAccountRequest{LoginName: "bot"})
			return err
		}},
		{"fee needs credentials permission read", func() error {
			_, err := newExtClient(t, server.URL, credential.PermissionTrade).SpotFee(ctx, SpotFeeRequest{})
			return err
		}},
	}
	for _, testCase := range permissionCases {
		t.Run(testCase.name, func(t *testing.T) {
			var apiError *trade.APIError
			if err := testCase.call(); !errors.As(err, &apiError) || apiError.Category != trade.ErrorAuthorization {
				t.Fatalf("error = %v, want authorization error", err)
			}
		})
	}
}

func TestExtensionAPIErrorMapping(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, `{"label":"BALANCE_NOT_ENOUGH","message":"Not enough balance"}`)
	}))
	defer server.Close()
	client := newExtClient(t, server.URL, allPermissions...)
	_, err := client.Withdraw(context.Background(), WithdrawRequest{
		WithdrawOrderID: "w-1", Currency: "USDT", Chain: "TRX", Amount: "1", Address: "T",
	})
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.Category != trade.ErrorInsufficientBalance ||
		apiError.ExchangeCode != "BALANCE_NOT_ENOUGH" {
		t.Fatalf("Withdraw() error = %v", err)
	}
}
