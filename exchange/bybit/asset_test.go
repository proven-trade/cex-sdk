package bybit

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

var (
	assetTransferPermissions = []credential.Permission{credential.PermissionRead, credential.PermissionTransfer}
	assetWithdrawPermissions = []credential.Permission{credential.PermissionRead, credential.PermissionWithdraw}
)

func TestAssetReads(t *testing.T) {
	t.Parallel()

	start := time.UnixMilli(1_700_000_000_000)
	end := time.UnixMilli(1_700_000_100_000)
	withdrawType := 2
	side := 1
	tests := []struct {
		name     string
		result   string
		path     string
		query    url.Values
		limitKey string
		call     func(*Client) (any, error)
		want     any
	}{
		{
			name:     "CoinInfo",
			result:   `{"rows":[{"name":"ETH","coin":"ETH","remainAmount":"1020000","chains":[{"chainType":"ETH","confirmation":"10000","withdrawFee":"0.005","depositMin":"0.01","withdrawMin":"0.02","chain":"ETH","chainDeposit":"1","chainWithdraw":"1","minAccuracy":"8","withdrawPercentageFee":"0.022"}]}]}`,
			path:     "/v5/asset/coin/query-info",
			query:    url.Values{"coin": {"ETH"}},
			limitKey: "/v5/asset/coin/query-info",
			call: func(client *Client) (any, error) {
				return client.CoinInfo(context.Background(), CoinInfoRequest{Coin: "ETH"})
			},
			want: CoinInfo{Rows: []CoinInfoRow{{Name: "ETH", Coin: "ETH", RemainAmount: "1020000", Chains: []CoinChainInfo{{
				Chain: "ETH", ChainType: "ETH", Confirmation: "10000", WithdrawFee: "0.005", DepositMin: "0.01",
				WithdrawMin: "0.02", MinAccuracy: "8", ChainDeposit: "1", ChainWithdraw: "1", WithdrawPercentageFee: "0.022",
			}}}}},
		},
		{
			name:   "InterTransferRecords",
			result: `{"list":[{"transferId":"t1","coin":"USDT","amount":"5000","fromAccountType":"SPOT","toAccountType":"UNIFIED","timestamp":"1667283263000","status":"SUCCESS"}],"nextPageCursor":"c2"}`,
			path:   "/v5/asset/transfer/query-inter-transfer-list",
			query: url.Values{
				"coin": {"USDT"}, "status": {"SUCCESS"}, "startTime": {"1700000000000"}, "endTime": {"1700000100000"},
				"limit": {"50"}, "cursor": {"c1"}, "transferId": {"t1"},
			},
			call: func(client *Client) (any, error) {
				return client.InterTransferRecords(context.Background(), TransferRecordsRequest{
					TransferID: "t1", Coin: "USDT", Status: "SUCCESS", StartTime: &start, EndTime: &end, Limit: 50, Cursor: "c1",
				})
			},
			want: InterTransferPage{List: []InterTransferRecord{{
				TransferID: "t1", Coin: "USDT", Amount: "5000", FromAccountType: "SPOT", ToAccountType: "UNIFIED",
				Timestamp: "1667283263000", Status: "SUCCESS",
			}}, NextPageCursor: "c2"},
		},
		{
			name:   "UniversalTransferRecords",
			result: `{"list":[{"transferId":"u1","coin":"USDC","amount":"1000","timestamp":"1672134373000","status":"SUCCESS","fromAccountType":"UNIFIED","toAccountType":"UNIFIED","fromMemberId":"533285","toMemberId":"592324"}],"nextPageCursor":""}`,
			path:   "/v5/asset/transfer/query-universal-transfer-list",
			query:  url.Values{"limit": {"1"}},
			call: func(client *Client) (any, error) {
				return client.UniversalTransferRecords(context.Background(), TransferRecordsRequest{Limit: 1})
			},
			want: UniversalTransferPage{List: []UniversalTransferRecord{{
				TransferID: "u1", Coin: "USDC", Amount: "1000", FromMemberID: "533285", ToMemberID: "592324",
				FromAccountType: "UNIFIED", ToAccountType: "UNIFIED", Timestamp: "1672134373000", Status: "SUCCESS",
			}}},
		},
		{
			name:   "TransferableCoins",
			result: `{"list":["BTC","ETH"]}`,
			path:   "/v5/asset/transfer/query-transfer-coin-list",
			query:  url.Values{"fromAccountType": {"UNIFIED"}, "toAccountType": {"FUND"}},
			call: func(client *Client) (any, error) {
				return client.TransferableCoins(
					context.Background(), TransferableCoinsRequest{FromAccountType: "UNIFIED", ToAccountType: "FUND"},
				)
			},
			want: TransferableCoins{List: []string{"BTC", "ETH"}},
		},
		{
			name:   "AccountCoinBalance",
			result: `{"accountType":"UNIFIED","bizType":1,"accountId":"1631385","memberId":"1631373","balance":{"coin":"USDT","walletBalance":"11999","transferBalance":"11999","bonus":"0","transferSafeAmount":"","ltvTransferSafeAmount":"7602.4861"}}`,
			path:   "/v5/asset/transfer/query-account-coin-balance",
			query: url.Values{
				"accountType": {"UNIFIED"}, "coin": {"USDT"}, "toAccountType": {"FUND"}, "memberId": {"1631373"},
				"withLtvTransferSafeAmount": {"1"}, "withBonus": {"1"},
			},
			call: func(client *Client) (any, error) {
				return client.AccountCoinBalance(context.Background(), AccountCoinBalanceRequest{
					AccountType: "UNIFIED", Coin: "USDT", ToAccountType: "FUND", MemberID: "1631373",
					WithLtvTransferSafeAmount: true, WithBonus: true,
				})
			},
			want: AccountCoinBalance{AccountType: "UNIFIED", BizType: 1, AccountID: "1631385", MemberID: "1631373",
				Balance: AssetCoinBalance{
					Coin: "USDT", WalletBalance: "11999", TransferBalance: "11999", Bonus: "0", LtvTransferSafeAmount: "7602.4861",
				}},
		},
		{
			name:   "AccountCoinsBalance",
			result: `{"memberId":"X","accountType":"FUND","balance":[{"coin":"USDC","transferBalance":"0","walletBalance":"0","bonus":""}]}`,
			path:   "/v5/asset/transfer/query-account-coins-balance",
			query:  url.Values{"accountType": {"FUND"}, "coin": {"USDC,USDT"}},
			call: func(client *Client) (any, error) {
				return client.AccountCoinsBalance(
					context.Background(), AccountCoinsBalanceRequest{AccountType: "FUND", Coins: []string{"USDC", "USDT"}},
				)
			},
			want: AccountCoinsBalance{AccountType: "FUND", MemberID: "X", Balance: []AssetCoinBalance{
				{Coin: "USDC", WalletBalance: "0", TransferBalance: "0"},
			}},
		},
		{
			name:   "DepositRecords",
			result: `{"rows":[{"coin":"USDT","chain":"ETH","amount":"10","txID":"0xabc","status":3,"toAddress":"0x1","tag":"","depositFee":"","successAt":"1672","confirmations":"6","txIndex":"1","blockHash":"0xb","batchReleaseLimit":"-1","depositType":0}],"nextPageCursor":"n"}`,
			path:   "/v5/asset/deposit/query-record",
			query:  url.Values{"coin": {"USDT"}, "limit": {"10"}},
			call: func(client *Client) (any, error) {
				return client.DepositRecords(context.Background(), DepositRecordsRequest{Coin: "USDT", Limit: 10})
			},
			want: DepositRecordPage{Rows: []DepositRecord{{
				Coin: "USDT", Chain: "ETH", Amount: "10", TxID: "0xabc", Status: 3, ToAddress: "0x1", SuccessAt: "1672",
				Confirmations: "6", TxIndex: "1", BlockHash: "0xb", BatchReleaseLimit: "-1",
			}}, NextPageCursor: "n"},
		},
		{
			name:   "SubDepositRecords",
			result: `{"rows":[],"nextPageCursor":""}`,
			path:   "/v5/asset/deposit/query-sub-member-record",
			query:  url.Values{"subMemberId": {"123"}, "coin": {"BTC"}},
			call: func(client *Client) (any, error) {
				return client.SubDepositRecords(context.Background(), SubDepositRecordsRequest{
					SubMemberID: "123", DepositRecordsRequest: DepositRecordsRequest{Coin: "BTC"},
				})
			},
			want: DepositRecordPage{Rows: []DepositRecord{}},
		},
		{
			name:   "InternalDepositRecords",
			result: `{"rows":[{"id":"1","type":1,"coin":"USDT","amount":"5","status":2,"address":"a@b.c","createdTime":"1700","txID":"tx"}],"nextPageCursor":""}`,
			path:   "/v5/asset/deposit/query-internal-record",
			query:  url.Values{"txID": {"tx"}, "coin": {"USDT"}},
			call: func(client *Client) (any, error) {
				return client.InternalDepositRecords(
					context.Background(), InternalDepositRecordsRequest{TxID: "tx", Coin: "USDT"},
				)
			},
			want: InternalDepositRecordPage{Rows: []InternalDepositRecord{{
				ID: "1", Type: 1, Coin: "USDT", Amount: "5", Status: 2, Address: "a@b.c", CreatedTime: "1700", TxID: "tx",
			}}},
		},
		{
			name:   "DepositAddress",
			result: `{"coin":"USDT","chains":[{"chainType":"Ethereum (ERC20)","addressDeposit":"0xd","tagDeposit":"","chain":"ETH","batchReleaseLimit":"-1"}]}`,
			path:   "/v5/asset/deposit/query-address",
			query:  url.Values{"coin": {"USDT"}, "chainType": {"ETH"}},
			call: func(client *Client) (any, error) {
				return client.DepositAddress(context.Background(), DepositAddressRequest{Coin: "USDT", ChainType: "ETH"})
			},
			want: DepositAddress{Coin: "USDT", Chains: []DepositAddressChain{{
				ChainType: "Ethereum (ERC20)", AddressDeposit: "0xd", Chain: "ETH", BatchReleaseLimit: "-1",
			}}},
		},
		{
			name:   "SubDepositAddress",
			result: `{"coin":"USDT","chains":[]}`,
			path:   "/v5/asset/deposit/query-sub-member-address",
			query:  url.Values{"coin": {"USDT"}, "chainType": {"ETH"}, "subMemberId": {"9"}},
			call: func(client *Client) (any, error) {
				return client.SubDepositAddress(
					context.Background(), SubDepositAddressRequest{Coin: "USDT", ChainType: "ETH", SubMemberID: "9"},
				)
			},
			want: DepositAddress{Coin: "USDT", Chains: []DepositAddressChain{}},
		},
		{
			name:   "WithdrawRecords",
			result: `{"rows":[{"coin":"USDT","chain":"ETH","amount":"77","txID":"","status":"SecurityCheck","toAddress":"0x9","tag":"","withdrawFee":"10","createTime":"1670922217000","updateTime":"1670922217000","withdrawId":"9976","withdrawType":0}],"nextPageCursor":"n"}`,
			path:   "/v5/asset/withdraw/query-record",
			query:  url.Values{"withdrawID": {"9976"}, "withdrawType": {"2"}, "coin": {"USDT"}},
			call: func(client *Client) (any, error) {
				return client.WithdrawRecords(context.Background(), WithdrawRecordsRequest{
					WithdrawID: "9976", WithdrawType: &withdrawType, Coin: "USDT",
				})
			},
			want: WithdrawRecordPage{Rows: []WithdrawRecord{{
				WithdrawID: "9976", Coin: "USDT", Chain: "ETH", Amount: "77", WithdrawFee: "10", Status: "SecurityCheck",
				ToAddress: "0x9", CreateTime: "1670922217000", UpdateTime: "1670922217000",
			}}, NextPageCursor: "n"},
		},
		{
			name:   "WithdrawableAmount",
			result: `{"limitAmountUsd":"595051.7","withdrawableAmount":{"FUND":{"coin":"USDT","withdrawableAmount":"155805.847","availableBalance":"155805.847"}}}`,
			path:   "/v5/asset/withdraw/withdrawable-amount",
			query:  url.Values{"coin": {"USDT"}},
			call: func(client *Client) (any, error) {
				return client.WithdrawableAmount(context.Background(), "USDT")
			},
			want: WithdrawableAmount{LimitAmountUSD: "595051.7", Wallets: map[string]WithdrawableWallet{
				"FUND": {Coin: "USDT", WithdrawableAmount: "155805.847", AvailableBalance: "155805.847"},
			}},
		},
		{
			name:   "ConvertCoins",
			result: `{"coins":[{"coin":"BTC","fullName":"Bitcoin","icon":"i","iconNight":"n","accuracyLength":8,"coinType":"crypto","balance":"1","uBalance":"60000","singleFromMinLimit":"0.001","singleFromMaxLimit":"10","disableFrom":false,"disableTo":true}]}`,
			path:   "/v5/asset/exchange/query-coin-list",
			query:  url.Values{"accountType": {"eb_convert_funding"}, "coin": {"BTC"}, "side": {"1"}},
			call: func(client *Client) (any, error) {
				return client.ConvertCoins(context.Background(), ConvertCoinsRequest{
					AccountType: "eb_convert_funding", Coin: "BTC", Side: &side,
				})
			},
			want: ConvertCoins{Coins: []ConvertCoin{{
				Coin: "BTC", FullName: "Bitcoin", Icon: "i", IconNight: "n", AccuracyLength: 8, CoinType: "crypto",
				Balance: "1", UBalance: "60000", SingleFromMinimum: "0.001", SingleFromMaximum: "10", DisableTo: true,
			}}},
		},
		{
			name:   "ConvertStatus",
			result: `{"result":{"accountType":"eb_convert_uta","exchangeTxId":"q1","userId":"1","fromCoin":"USDT","fromCoinType":"crypto","toCoin":"BTC","toCoinType":"crypto","fromAmount":"100","toAmount":"0.001","exchangeStatus":"success","convertRate":"0.00001","createdAt":"1700"}}`,
			path:   "/v5/asset/exchange/convert-result-query",
			query:  url.Values{"quoteTxId": {"q1"}, "accountType": {"eb_convert_uta"}},
			call: func(client *Client) (any, error) {
				return client.ConvertStatus(
					context.Background(), ConvertStatusRequest{QuoteTxID: "q1", AccountType: "eb_convert_uta"},
				)
			},
			want: ConvertRecord{
				AccountType: "eb_convert_uta", ExchangeTxID: "q1", UserID: "1", FromCoin: "USDT", FromCoinType: "crypto",
				ToCoin: "BTC", ToCoinType: "crypto", FromAmount: "100", ToAmount: "0.001", ExchangeStatus: "success",
				ConvertRate: "0.00001", CreatedAt: "1700",
			},
		},
		{
			name:   "ConvertHistory",
			result: `{"list":[{"accountType":"eb_convert_funding","exchangeTxId":"q2","userId":"1","fromCoin":"ETH","toCoin":"USDT","fromAmount":"1","toAmount":"3000","exchangeStatus":"failure","convertRate":"3000","createdAt":"1701"}]}`,
			path:   "/v5/asset/exchange/query-convert-history",
			query:  url.Values{"accountType": {"eb_convert_funding,eb_convert_uta"}, "index": {"2"}, "limit": {"100"}},
			call: func(client *Client) (any, error) {
				return client.ConvertHistory(context.Background(), ConvertHistoryRequest{
					AccountTypes: []string{"eb_convert_funding", "eb_convert_uta"}, Index: 2, Limit: 100,
				})
			},
			want: ConvertHistory{List: []ConvertRecord{{
				AccountType: "eb_convert_funding", ExchangeTxID: "q2", UserID: "1", FromCoin: "ETH", ToCoin: "USDT",
				FromAmount: "1", ToAmount: "3000", ExchangeStatus: "failure", ConvertRate: "3000", CreatedAt: "1701",
			}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, limiter := newExtTestClient(t, extReadPermissions, extResult(test.result))
			got, err := test.call(client)
			if err != nil {
				t.Fatalf("%s() error = %v", test.name, err)
			}
			call := extSingleCall(t, recorder, http.MethodGet, test.path)
			if !reflect.DeepEqual(call.Query, test.query) || call.Body != "" {
				t.Fatalf("query = %v body = %q, want %v and empty body", call.Query, call.Body, test.query)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("%s() = %+v, want %+v", test.name, got, test.want)
			}
			snapshot, err := limiter.Snapshot(rateLimitEndpointKey("account", "bybit-main", test.path))
			if err != nil || snapshot.Used != 1 {
				t.Fatalf("limiter snapshot = %+v, error = %v", snapshot, err)
			}
		})
	}
}

func TestAssetReadValidation(t *testing.T) {
	t.Parallel()

	badType := 3
	badSide := 2
	start := time.UnixMilli(2_000)
	end := time.UnixMilli(1_000)
	tests := []struct {
		name string
		call func(*Client) error
	}{
		{"TransferableCoins from", func(c *Client) error {
			_, err := c.TransferableCoins(context.Background(), TransferableCoinsRequest{ToAccountType: "FUND"})
			return err
		}},
		{"TransferRecords status", func(c *Client) error {
			_, err := c.InterTransferRecords(context.Background(), TransferRecordsRequest{Status: "DONE"})
			return err
		}},
		{"TransferRecords limit", func(c *Client) error {
			_, err := c.UniversalTransferRecords(context.Background(), TransferRecordsRequest{Limit: 51})
			return err
		}},
		{"DepositRecords range", func(c *Client) error {
			_, err := c.DepositRecords(context.Background(), DepositRecordsRequest{StartTime: &start, EndTime: &end})
			return err
		}},
		{"SubDepositRecords uid", func(c *Client) error {
			_, err := c.SubDepositRecords(context.Background(), SubDepositRecordsRequest{})
			return err
		}},
		{"AccountCoinBalance coin", func(c *Client) error {
			_, err := c.AccountCoinBalance(context.Background(), AccountCoinBalanceRequest{AccountType: "FUND"})
			return err
		}},
		{"AccountCoinBalance ltv", func(c *Client) error {
			_, err := c.AccountCoinBalance(context.Background(), AccountCoinBalanceRequest{
				AccountType: "FUND", Coin: "USDT", WithLtvTransferSafeAmount: true,
			})
			return err
		}},
		{"AccountCoinsBalance unified", func(c *Client) error {
			_, err := c.AccountCoinsBalance(context.Background(), AccountCoinsBalanceRequest{AccountType: "UNIFIED"})
			return err
		}},
		{"DepositAddress coin", func(c *Client) error {
			_, err := c.DepositAddress(context.Background(), DepositAddressRequest{})
			return err
		}},
		{"SubDepositAddress chain", func(c *Client) error {
			_, err := c.SubDepositAddress(context.Background(), SubDepositAddressRequest{Coin: "USDT", SubMemberID: "1"})
			return err
		}},
		{"WithdrawRecords type", func(c *Client) error {
			_, err := c.WithdrawRecords(context.Background(), WithdrawRecordsRequest{WithdrawType: &badType})
			return err
		}},
		{"WithdrawableAmount coin", func(c *Client) error {
			_, err := c.WithdrawableAmount(context.Background(), "")
			return err
		}},
		{"ConvertCoins side", func(c *Client) error {
			_, err := c.ConvertCoins(context.Background(), ConvertCoinsRequest{AccountType: "eb_convert_uta", Side: &badSide})
			return err
		}},
		{"ConvertHistory limit", func(c *Client) error {
			_, err := c.ConvertHistory(context.Background(), ConvertHistoryRequest{Limit: 101})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(`{}`))
			if err := test.call(client); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("error = %v, want validation error", err)
			}
			if calls := recorder.snapshot(); len(calls) != 0 {
				t.Fatalf("server received %d requests, want none", len(calls))
			}
		})
	}
}

func TestCreateTransfersAndConvert(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result string
		path   string
		body   string
		call   func(*Client) (any, error)
		want   any
	}{
		{
			name: "CreateInterTransfer", result: `{"transferId":"42c0"}`, path: "/v5/asset/transfer/inter-transfer",
			body: `{"transferId":"42c0","coin":"BTC","amount":"0.05","fromAccountType":"UNIFIED","toAccountType":"FUND"}`,
			call: func(client *Client) (any, error) {
				return client.CreateInterTransfer(context.Background(), InterTransferRequest{
					TransferID: "42c0", Coin: "BTC", Amount: "0.05", FromAccountType: "UNIFIED", ToAccountType: "FUND",
				})
			},
			want: TransferResult{TransferID: "42c0"},
		},
		{
			name: "CreateUniversalTransfer", result: `{"transferId":"be7a"}`, path: "/v5/asset/transfer/universal-transfer",
			body: `{"transferId":"be7a","coin":"ETH","amount":"0.5","fromMemberId":592334,"toMemberId":691355,"fromAccountType":"FUND","toAccountType":"UNIFIED"}`,
			call: func(client *Client) (any, error) {
				return client.CreateUniversalTransfer(context.Background(), UniversalTransferRequest{
					TransferID: "be7a", Coin: "ETH", Amount: "0.5", FromMemberID: 592334, ToMemberID: 691355,
					FromAccountType: "FUND", ToAccountType: "UNIFIED",
				})
			},
			want: TransferResult{TransferID: "be7a"},
		},
		{
			name: "RequestConvertQuote", result: `{"quoteTxId":"q1","exchangeRate":"0.00001","fromCoin":"USDT","fromCoinType":"crypto","toCoin":"BTC","toCoinType":"crypto","fromAmount":"100","toAmount":"0.001","expiredTime":"1700","requestId":"r1","extTaxAndFee":[]}`,
			path: "/v5/asset/exchange/quote-apply",
			body: `{"accountType":"eb_convert_uta","fromCoin":"USDT","toCoin":"BTC","requestCoin":"USDT","requestAmount":"100","requestId":"r1"}`,
			call: func(client *Client) (any, error) {
				return client.RequestConvertQuote(context.Background(), ConvertQuoteRequest{
					AccountType: "eb_convert_uta", FromCoin: "USDT", ToCoin: "BTC", RequestAmount: "100", RequestID: "r1",
				})
			},
			want: ConvertQuote{
				QuoteTxID: "q1", ExchangeRate: "0.00001", FromCoin: "USDT", FromCoinType: "crypto", ToCoin: "BTC",
				ToCoinType: "crypto", FromAmount: "100", ToAmount: "0.001", ExpiredTime: "1700", RequestID: "r1",
			},
		},
		{
			name: "ConfirmConvertQuote", result: `{"quoteTxId":"q1","exchangeStatus":"processing"}`,
			path: "/v5/asset/exchange/convert-execute", body: `{"quoteTxId":"q1"}`,
			call: func(client *Client) (any, error) {
				return client.ConfirmConvertQuote(context.Background(), "q1")
			},
			want: ConvertConfirmResult{QuoteTxID: "q1", ExchangeStatus: "processing"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(t, assetTransferPermissions, extResult(test.result))
			got, err := test.call(client)
			if err != nil {
				t.Fatalf("%s() error = %v", test.name, err)
			}
			call := extSingleCall(t, recorder, http.MethodPost, test.path)
			assertJSONEqual(t, call.Body, test.body)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("%s() = %+v, want %+v", test.name, got, test.want)
			}
		})
	}
}

func TestWithdraw(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, assetWithdrawPermissions, extResult(`{"id":"10195"}`))
	result, err := client.CreateWithdraw(context.Background(), WithdrawRequest{
		Coin: "USDT", Chain: "ETH", Address: "0xabc", Tag: "t", Amount: "10.5", ForceChain: 1,
		AccountType: "FUND", FeeType: 1, RequestID: "req01",
	})
	if err != nil || result.ID != "10195" {
		t.Fatalf("CreateWithdraw() = %+v, error = %v", result, err)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/asset/withdraw/create")
	assertJSONEqual(t, call.Body,
		`{"coin":"USDT","chain":"ETH","address":"0xabc","tag":"t","amount":"10.5","timestamp":1700000000000,`+
			`"forceChain":1,"accountType":"FUND","feeType":1,"requestId":"req01"}`)

	client, recorder, _ = newExtTestClient(t, assetWithdrawPermissions, extResult(`{"status":1}`))
	cancelled, err := client.CancelWithdraw(context.Background(), "10195")
	if err != nil || !cancelled.Succeeded() {
		t.Fatalf("CancelWithdraw() = %+v, error = %v", cancelled, err)
	}
	call = extSingleCall(t, recorder, http.MethodPost, "/v5/asset/withdraw/cancel")
	assertJSONEqual(t, call.Body, `{"id":"10195"}`)

	invalid := []WithdrawRequest{
		{Address: "a", Chain: "ETH", Amount: "1"},
		{Coin: "USDT", Chain: "ETH", Amount: "1"},
		{Coin: "USDT", Address: "a", Amount: "1"},
		{Coin: "USDT", Address: "a", Chain: "ETH", Amount: "0"},
		{Coin: "USDT", Address: "a", Chain: "ETH", Amount: "1", ForceChain: 3},
		{Coin: "USDT", Address: "a", Chain: "ETH", Amount: "1", AccountType: "UNIFIED"},
		{Coin: "USDT", Address: "a", Chain: "ETH", Amount: "1", FeeType: 2},
		{Coin: "USDT", Address: "a", Chain: "ETH", Amount: "1", RequestID: "has-dash"},
	}
	for _, request := range invalid {
		if err := request.validate(); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validate(%+v) error = %v, want validation error", request, err)
		}
	}
	if err := (WithdrawRequest{Coin: "USDT", Address: "1234", Amount: "1", ForceChain: 2}).validate(); err != nil {
		t.Fatalf("UID withdraw without chain error = %v", err)
	}
}

func TestAssetMutationsRequireDedicatedPermission(t *testing.T) {
	t.Parallel()

	transfer := func(client *Client) error {
		_, err := client.CreateInterTransfer(context.Background(), InterTransferRequest{
			TransferID: "1", Coin: "BTC", Amount: "1", FromAccountType: "UNIFIED", ToAccountType: "FUND",
		})
		return err
	}
	universal := func(client *Client) error {
		_, err := client.CreateUniversalTransfer(context.Background(), UniversalTransferRequest{
			TransferID: "1", Coin: "BTC", Amount: "1", FromMemberID: 1, ToMemberID: 2,
			FromAccountType: "UNIFIED", ToAccountType: "FUND",
		})
		return err
	}
	quote := func(client *Client) error {
		_, err := client.RequestConvertQuote(context.Background(), ConvertQuoteRequest{
			AccountType: "eb_convert_uta", FromCoin: "USDT", ToCoin: "BTC", RequestAmount: "1",
		})
		return err
	}
	confirm := func(client *Client) error {
		_, err := client.ConfirmConvertQuote(context.Background(), "q1")
		return err
	}
	withdraw := func(client *Client) error {
		_, err := client.CreateWithdraw(context.Background(), WithdrawRequest{
			Coin: "USDT", Chain: "ETH", Address: "a", Amount: "1",
		})
		return err
	}
	cancelWithdraw := func(client *Client) error {
		_, err := client.CancelWithdraw(context.Background(), "1")
		return err
	}
	tests := []struct {
		name       string
		call       func(*Client) error
		permission credential.Permission
	}{
		{"CreateInterTransfer", transfer, credential.PermissionTransfer},
		{"CreateUniversalTransfer", universal, credential.PermissionTransfer},
		{"RequestConvertQuote", quote, credential.PermissionTransfer},
		{"ConfirmConvertQuote", confirm, credential.PermissionTransfer},
		{"CreateWithdraw", withdraw, credential.PermissionWithdraw},
		{"CancelWithdraw", cancelWithdraw, credential.PermissionWithdraw},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Trade와 반대 권한(출금↔이체)은 다른 권한이므로 허용되지 않아야 한다.
			other := credential.PermissionTrade
			client, recorder, _ := newExtTestClient(
				t, []credential.Permission{credential.PermissionRead, other}, extResult(`{}`),
			)
			if err := test.call(client); !errors.Is(err, trade.ErrAuthorization) {
				t.Fatalf("error = %v, want authorization", err)
			}
			if calls := recorder.snapshot(); len(calls) != 0 {
				t.Fatalf("server received %d requests, want none without %s permission", len(calls), test.permission)
			}

			// 이체 권한만 있는 키로는 출금 계열을, 출금 권한만 있는 키로는 이체 계열을 호출할 수 없다.
			wrong := credential.PermissionWithdraw
			if test.permission == credential.PermissionWithdraw {
				wrong = credential.PermissionTransfer
			}
			client, recorder, _ = newExtTestClient(
				t, []credential.Permission{credential.PermissionRead, wrong}, extResult(`{}`),
			)
			if err := test.call(client); !errors.Is(err, trade.ErrAuthorization) {
				t.Fatalf("error = %v, want authorization with %s only", err, wrong)
			}
			if calls := recorder.snapshot(); len(calls) != 0 {
				t.Fatalf("server received %d requests, want none", len(calls))
			}
		})
	}
}

func TestAssetMutationsAreNotRetried(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		permissions []credential.Permission
		call        func(*Client) error
	}{
		{"CreateInterTransfer", assetTransferPermissions, func(c *Client) error {
			_, err := c.CreateInterTransfer(context.Background(), InterTransferRequest{
				TransferID: "1", Coin: "BTC", Amount: "1", FromAccountType: "UNIFIED", ToAccountType: "FUND",
			})
			return err
		}},
		{"CreateWithdraw", assetWithdrawPermissions, func(c *Client) error {
			_, err := c.CreateWithdraw(context.Background(), WithdrawRequest{
				Coin: "USDT", Chain: "ETH", Address: "a", Amount: "1",
			})
			return err
		}},
		{"ConfirmConvertQuote", assetTransferPermissions, func(c *Client) error {
			_, err := c.ConfirmConvertQuote(context.Background(), "q1")
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(t, test.permissions, extServerError)
			if err := test.call(client); !errors.Is(err, trade.ErrUnknownExecutionState) {
				t.Fatalf("error = %v, want unknown execution state", err)
			}
			if calls := recorder.snapshot(); len(calls) != 1 {
				t.Fatalf("server received %d requests, want exactly one without retry", len(calls))
			}
		})
	}
}

func TestAssetMutationValidation(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, assetTransferPermissions, extResult(`{}`))
	for name, call := range map[string]func() error{
		"transfer amount": func() error {
			_, err := client.CreateInterTransfer(context.Background(), InterTransferRequest{
				TransferID: "1", Coin: "BTC", Amount: "-1", FromAccountType: "UNIFIED", ToAccountType: "FUND",
			})
			return err
		},
		"universal same uid": func() error {
			_, err := client.CreateUniversalTransfer(context.Background(), UniversalTransferRequest{
				TransferID: "1", Coin: "BTC", Amount: "1", FromMemberID: 5, ToMemberID: 5,
				FromAccountType: "UNIFIED", ToAccountType: "FUND",
			})
			return err
		},
		"universal missing uid": func() error {
			_, err := client.CreateUniversalTransfer(context.Background(), UniversalTransferRequest{
				TransferID: "1", Coin: "BTC", Amount: "1", FromAccountType: "UNIFIED", ToAccountType: "FUND",
			})
			return err
		},
		"quote amount": func() error {
			_, err := client.RequestConvertQuote(context.Background(), ConvertQuoteRequest{
				AccountType: "eb_convert_uta", FromCoin: "USDT", ToCoin: "BTC",
			})
			return err
		},
		"confirm id": func() error {
			_, err := client.ConfirmConvertQuote(context.Background(), "")
			return err
		},
		"cancel withdraw id": func() error {
			_, err := client.CancelWithdraw(context.Background(), "")
			return err
		},
	} {
		if err := call(); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s error = %v, want validation error", name, err)
		}
	}
	if calls := recorder.snapshot(); len(calls) != 0 {
		t.Fatalf("server received %d requests, want none", len(calls))
	}
}
