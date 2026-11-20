package okx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

func TestAssetAndSubAccountEndpoints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	after := time.UnixMilli(1_654_041_600_000)
	enable := true
	runPublicAssetCases(t, []publicAssetCase{
		{
			name: "currencies", method: http.MethodGet, path: "/api/v5/asset/currencies", query: "ccy=BTC%2CETH", private: true,
			reply: `{"code":"0","msg":"","data":[{"canDep":true,"canInternal":true,"canWd":false,"ccy":"BTC","chain":"BTC-Bitcoin","fee":"0.0005","minWd":"0.001","needTag":false,"mainNet":true}]}`,
			call: func(c *Client) error {
				items, err := c.AssetCurrencies(ctx, []string{"BTC", "ETH"})
				if err == nil && (len(items) != 1 || !items[0].CanDeposit || items[0].CanWithdraw || items[0].Fee != "0.0005") {
					t.Errorf("AssetCurrencies() = %+v", items)
				}
				return err
			},
		},
		{
			name: "balances", method: http.MethodGet, path: "/api/v5/asset/balances", private: true,
			reply: `{"code":"0","msg":"","data":[{"availBal":"37.1","bal":"37.12","ccy":"ETH","frozenBal":"0.02"}]}`,
			call: func(c *Client) error {
				items, err := c.AssetBalances(ctx, nil)
				if err == nil && (len(items) != 1 || items[0].AvailableBalance != "37.1" || items[0].FrozenBalance != "0.02") {
					t.Errorf("AssetBalances() = %+v", items)
				}
				return err
			},
		},
		{
			name: "non tradable", method: http.MethodGet, path: "/api/v5/asset/non-tradable-assets", private: true,
			reply: `{"code":"0","msg":"","data":[{"bal":"989.84","canWd":true,"ccy":"CELT","chain":"CELT-OKTC","fee":"2","feeCcy":"USDT","wdAll":false}]}`,
			call: func(c *Client) error {
				items, err := c.NonTradableAssets(ctx, nil)
				if err == nil && (len(items) != 1 || items[0].FeeCurrency != "USDT" || !items[0].CanWithdraw) {
					t.Errorf("NonTradableAssets() = %+v", items)
				}
				return err
			},
		},
		{
			name: "valuation", method: http.MethodGet, path: "/api/v5/asset/asset-valuation", query: "ccy=USD", private: true,
			reply: `{"code":"0","msg":"","data":[{"details":{"funding":"1","trading":"2","earn":"3"},"totalBal":"6","ts":"1672734730284"}]}`,
			call: func(c *Client) error {
				item, err := c.AssetValuation(ctx, "USD")
				if err == nil && (item.TotalBalance != "6" || item.Details.Trading != "2") {
					t.Errorf("AssetValuation() = %+v", item)
				}
				return err
			},
		},
		{
			name: "transfer", method: http.MethodPost, path: "/api/v5/asset/transfer", private: true,
			body:  `{"type":"1","ccy":"USDT","amt":"1.5","from":"6","to":"18","subAcct":"sub1","clientId":"c1","omitPosRisk":"true"}`,
			reply: `{"code":"0","msg":"","data":[{"transId":"754147","ccy":"USDT","clientId":"c1","from":"6","amt":"1.5","to":"18"}]}`,
			call: func(c *Client) error {
				item, err := c.AssetTransfer(ctx, AssetTransferRequest{
					Type: TransferTypeMasterToSub, Currency: "USDT", Amount: "1.5", From: AssetAccountFunding,
					To: AssetAccountTrading, SubAccount: "sub1", ClientID: "c1", OmitPositionRisk: true,
				})
				if err == nil && (item.TransferID != "754147" || item.ClientID != "c1") {
					t.Errorf("AssetTransfer() = %+v", item)
				}
				return err
			},
		},
		{
			name: "transfer state", method: http.MethodGet, path: "/api/v5/asset/transfer-state", query: "transId=754147&type=1", private: true,
			reply: `{"code":"0","msg":"","data":[{"amt":"1.5","ccy":"USDT","clientId":"","from":"18","instId":"","state":"success","subAcct":"test","to":"6","toInstId":"","transId":"754147","type":"1"}]}`,
			call: func(c *Client) error {
				item, err := c.AssetTransferStates(ctx, AssetTransferStateRequest{TransferID: "754147", Type: TransferTypeMasterToSub})
				if err == nil && (item.State != "success" || item.SubAccount != "test") {
					t.Errorf("AssetTransferStates() = %+v", item)
				}
				return err
			},
		},
		{
			name: "bills", method: http.MethodGet, path: "/api/v5/asset/bills", query: "ccy=USDT&type=1&limit=10", private: true,
			reply: `{"code":"0","msg":"","data":[{"billId":"12344","ccy":"BTC","clientId":"","balChg":"2","bal":"12","type":"1","notes":"","ts":"1597026383085"}]}`,
			call: func(c *Client) error {
				items, err := c.AssetBills(ctx, AssetBillsRequest{Currency: "USDT", Type: "1", Limit: 10})
				if err == nil && (len(items) != 1 || items[0].BalanceChange != "2" || items[0].BillID != "12344") {
					t.Errorf("AssetBills() = %+v", items)
				}
				return err
			},
		},
		{
			name: "bills history", method: http.MethodGet, path: "/api/v5/asset/bills-history", query: "after=123&pagingType=2", private: true,
			reply: `{"code":"0","msg":"","data":[]}`,
			call: func(c *Client) error {
				_, err := c.AssetBillsHistory(ctx, AssetBillsRequest{After: "123", PagingType: "2"})
				return err
			},
		},
		{
			name: "deposit address", method: http.MethodGet, path: "/api/v5/asset/deposit-address", query: "ccy=BTC", private: true,
			reply: `{"code":"0","msg":"","data":[{"addr":"bc1qxx","ccy":"BTC","chain":"BTC-Bitcoin","to":"6","selected":true}]}`,
			call: func(c *Client) error {
				items, err := c.DepositAddresses(ctx, "BTC")
				if err == nil && (len(items) != 1 || items[0].Address != "bc1qxx" || !items[0].Selected) {
					t.Errorf("DepositAddresses() = %+v", items)
				}
				return err
			},
		},
		{
			name: "deposit history", method: http.MethodGet, path: "/api/v5/asset/deposit-history", query: "ccy=BTC&after=1654041600000&limit=5", private: true,
			reply: `{"code":"0","msg":"","data":[{"actualDepBlkConfirm":"2","amt":"0.01","ccy":"BTC","chain":"BTC-Bitcoin","depId":"88165462","from":"","fromWdId":"","state":"2","to":"1BM","ts":"1597026383085","txId":"tx1"}]}`,
			call: func(c *Client) error {
				items, err := c.DepositHistory(ctx, DepositHistoryRequest{Currency: "BTC", After: &after, Limit: 5})
				if err == nil && (len(items) != 1 || items[0].DepositID != "88165462" || items[0].State != "2") {
					t.Errorf("DepositHistory() = %+v", items)
				}
				return err
			},
		},
		{
			name: "withdraw", method: http.MethodPost, path: "/api/v5/asset/withdrawal", private: true,
			body:  `{"ccy":"USDT","amt":"1","dest":"4","toAddr":"TXYZ","chain":"USDT-TRC20","rcvrInfo":{"walletType":"private"},"clientId":"w1"}`,
			reply: `{"code":"0","msg":"","data":[{"amt":"1","wdId":"67485","ccy":"USDT","clientId":"w1","chain":"USDT-TRC20"}]}`,
			call: func(c *Client) error {
				item, err := c.Withdraw(ctx, WithdrawalRequest{
					Currency: "USDT", Amount: "1", Destination: WithdrawalDestinationOnChain, ToAddress: "TXYZ",
					Chain: "USDT-TRC20", Receiver: &ReceiverInfo{WalletType: "private"}, ClientID: "w1",
				})
				if err == nil && (item.Withdraw != "67485" || item.ClientID != "w1") {
					t.Errorf("Withdraw() = %+v", item)
				}
				return err
			},
		},
		{
			name: "cancel withdrawal", method: http.MethodPost, path: "/api/v5/asset/cancel-withdrawal", private: true,
			body:  `{"wdId":"67485"}`,
			reply: `{"code":"0","msg":"","data":[{"wdId":"67485"}]}`,
			call: func(c *Client) error {
				item, err := c.CancelWithdrawal(ctx, "67485")
				if err == nil && item.WithdrawalID != "67485" {
					t.Errorf("CancelWithdrawal() = %+v", item)
				}
				return err
			},
		},
		{
			name: "withdrawal history", method: http.MethodGet, path: "/api/v5/asset/withdrawal-history", query: "wdId=67485&state=2", private: true,
			reply: `{"code":"0","msg":"","data":[{"chain":"ETH-Ethereum","fee":"0.1","feeCcy":"USDT","ccy":"USDT","clientId":"","amt":"0.094","txId":"0x","from":"a","to":"b","state":"2","ts":"1655251200000","wdId":"67485"}]}`,
			call: func(c *Client) error {
				items, err := c.WithdrawalHistory(ctx, WithdrawalHistoryRequest{WithdrawalID: "67485", State: "2"})
				if err == nil && (len(items) != 1 || items[0].FeeCurrency != "USDT" || items[0].State != "2") {
					t.Errorf("WithdrawalHistory() = %+v", items)
				}
				return err
			},
		},
		{
			name: "deposit withdraw status", method: http.MethodGet, path: "/api/v5/asset/deposit-withdraw-status", query: "wdId=200045249", private: true,
			reply: `{"code":"0","msg":"","data":[{"estCompleteTime":"01/09/2023, 8:10:48 PM","state":"Pending withdrawal: Wallet is under maintenance","txId":"","wdId":"200045249"}]}`,
			call: func(c *Client) error {
				items, err := c.DepositWithdrawStatuses(ctx, DepositWithdrawStatusRequest{WithdrawalID: "200045249"})
				if err == nil && (len(items) != 1 || items[0].WithdrawalID != "200045249") {
					t.Errorf("DepositWithdrawStatuses() = %+v", items)
				}
				return err
			},
		},
		{
			name: "convert currencies", method: http.MethodGet, path: "/api/v5/asset/convert/currencies", private: true,
			reply: `{"code":"0","msg":"","data":[{"ccy":"USDT","max":"","min":""}]}`,
			call: func(c *Client) error {
				items, err := c.ConvertCurrencies(ctx)
				if err == nil && (len(items) != 1 || items[0].Currency != "USDT") {
					t.Errorf("ConvertCurrencies() = %+v", items)
				}
				return err
			},
		},
		{
			name: "convert pair", method: http.MethodGet, path: "/api/v5/asset/convert/currency-pair", query: "fromCcy=USDT&toCcy=BTC", private: true,
			reply: `{"code":"0","msg":"","data":[{"instId":"BTC-USDT","baseCcy":"BTC","baseCcyMax":"1","baseCcyMin":"0.001","quoteCcy":"USDT","quoteCcyMax":"10000","quoteCcyMin":"10"}]}`,
			call: func(c *Client) error {
				item, err := c.ConvertCurrencyPairs(ctx, "USDT", "BTC")
				if err == nil && (item.InstrumentID != "BTC-USDT" || item.QuoteMinimum != "10") {
					t.Errorf("ConvertCurrencyPairs() = %+v", item)
				}
				return err
			},
		},
		{
			name: "convert quote", method: http.MethodPost, path: "/api/v5/asset/convert/estimate-quote", private: true,
			body:  `{"baseCcy":"ETH","quoteCcy":"USDT","side":"buy","rfqSz":"30","rfqSzCcy":"USDT","clQReqId":"q1"}`,
			reply: `{"code":"0","msg":"","data":[{"baseCcy":"ETH","baseSz":"0.01","clQReqId":"q1","cnvtPx":"2932.4","origRfqSz":"30","quoteCcy":"USDT","quoteId":"quoterETH-USDT16","quoteSz":"29.3","quoteTime":"1646188","rfqSz":"30","rfqSzCcy":"USDT","side":"buy","ttlMs":"10000"}]}`,
			call: func(c *Client) error {
				item, err := c.ConvertEstimateQuote(ctx, ConvertQuoteRequest{
					BaseCurrency: "ETH", QuoteCurrency: "USDT", Side: SideBuy, RFQSize: "30", RFQSizeCurrency: "USDT",
					ClientQuoteID: "q1",
				})
				if err == nil && (item.QuoteID != "quoterETH-USDT16" || item.TTLMillis != "10000") {
					t.Errorf("ConvertEstimateQuote() = %+v", item)
				}
				return err
			},
		},
		{
			name: "convert trade", method: http.MethodPost, path: "/api/v5/asset/convert/trade", private: true,
			body:  `{"quoteId":"quoterETH-USDT16","baseCcy":"ETH","quoteCcy":"USDT","side":"buy","sz":"30","szCcy":"USDT","clTReqId":"t1"}`,
			reply: `{"code":"0","msg":"","data":[{"instId":"ETH-USDT","side":"buy","fillPx":"2932.4","baseCcy":"ETH","quoteCcy":"USDT","fillBaseSz":"0.01","state":"fullyFilled","tradeId":"trader16","fillQuoteSz":"30","ts":"1646188","clTReqId":"t1","quoteId":"quoterETH-USDT16"}]}`,
			call: func(c *Client) error {
				item, err := c.ConvertTrade(ctx, ConvertTradeRequest{
					QuoteID: "quoterETH-USDT16", BaseCurrency: "ETH", QuoteCurrency: "USDT", Side: SideBuy, Size: "30",
					SizeCurrency: "USDT", ClientTradeID: "t1",
				})
				if err == nil && (item.TradeID != "trader16" || item.State != "fullyFilled") {
					t.Errorf("ConvertTrade() = %+v", item)
				}
				return err
			},
		},
		{
			name: "convert history", method: http.MethodGet, path: "/api/v5/asset/convert/history", query: "clTReqId=t1&limit=2", private: true,
			reply: `{"code":"0","msg":"","data":[{"instId":"ETH-USDT","side":"buy","fillPx":"2932.4","baseCcy":"ETH","quoteCcy":"USDT","fillBaseSz":"0.01","state":"fullyFilled","tradeId":"trader16","fillQuoteSz":"30","ts":"1646188","clTReqId":"t1"}]}`,
			call: func(c *Client) error {
				items, err := c.ConvertHistory(ctx, ConvertHistoryRequest{ClientTradeID: "t1", Limit: 2})
				if err == nil && (len(items) != 1 || items[0].FillPrice != "2932.4") {
					t.Errorf("ConvertHistory() = %+v", items)
				}
				return err
			},
		},
		{
			name: "sub-account list", method: http.MethodGet, path: "/api/v5/users/subaccount/list", query: "enable=true&limit=1", private: true,
			reply: `{"code":"0","msg":"","data":[{"canTransOut":false,"enable":true,"frozenFunc":[],"gAuth":false,"label":"D456DDDLx","mobile":"","subAcct":"D456DDDL","ts":"1659334756000","type":"1","uid":"3400","subAcctLv":"1","firstLvSubAcct":"D456DDDL","ifDma":false}]}`,
			call: func(c *Client) error {
				items, err := c.SubAccounts(ctx, SubAccountListRequest{Enable: &enable, Limit: 1})
				if err == nil && (len(items) != 1 || items[0].SubAccount != "D456DDDL" || !items[0].Enable) {
					t.Errorf("SubAccounts() = %+v", items)
				}
				return err
			},
		},
		{
			name: "modify api key", method: http.MethodPost, path: "/api/v5/users/subaccount/modify-apikey", private: true,
			body:  `{"subAcct":"yongxu","apiKey":"k","label":"v5","perm":"read_only,trade","ip":"1.1.1.1"}`,
			reply: `{"code":"0","msg":"","data":[{"subAcct":"yongxu","label":"v5","apiKey":"******","perm":"read_only,trade","ip":"1.1.1.1","ts":"1597026383085"}]}`,
			call: func(c *Client) error {
				item, err := c.ModifySubAccountAPIKey(ctx, SubAccountAPIKeyModifyRequest{
					SubAccount: "yongxu", APIKey: "k", Label: "v5", Permissions: []string{"read_only", "trade"},
					IPs: []string{"1.1.1.1"},
				})
				if err == nil && item.Permissions != "read_only,trade" {
					t.Errorf("ModifySubAccountAPIKey() = %+v", item)
				}
				return err
			},
		},
		{
			name: "set transfer out", method: http.MethodPost, path: "/api/v5/users/subaccount/set-transfer-out", private: true,
			body:  `{"subAcct":"Test001,Test002","canTransOut":true}`,
			reply: `{"code":"0","msg":"","data":[{"subAcct":"Test001","canTransOut":true},{"subAcct":"Test002","canTransOut":true}]}`,
			call: func(c *Client) error {
				items, err := c.SetSubAccountTransferOut(ctx, []string{"Test001", "Test002"}, true)
				if err == nil && (len(items) != 2 || !items[1].CanTransferOut) {
					t.Errorf("SetSubAccountTransferOut() = %+v", items)
				}
				return err
			},
		},
		{
			name: "entrust list", method: http.MethodGet, path: "/api/v5/users/entrust-subaccount-list", private: true,
			reply: `{"code":"0","msg":"","data":[{"subAcct":"test-1"},{"subAcct":"test-2"}]}`,
			call: func(c *Client) error {
				items, err := c.EntrustSubAccounts(ctx, "")
				if err == nil && (len(items) != 2 || items[1].SubAccount != "test-2") {
					t.Errorf("EntrustSubAccounts() = %+v", items)
				}
				return err
			},
		},
		{
			name: "sub trading balance", method: http.MethodGet, path: "/api/v5/account/subaccount/balances", query: "subAcct=test1", private: true,
			reply: `{"code":"0","msg":"","data":[{"totalEq":"101","uTime":"1700000000000","details":[{"ccy":"USDT","eq":"101.5","availBal":"101.5"}]}]}`,
			call: func(c *Client) error {
				item, err := c.SubAccountBalances(ctx, "test1")
				if err == nil && (item.TotalEquity != "101" || len(item.Details) != 1) {
					t.Errorf("SubAccountBalances() = %+v", item)
				}
				return err
			},
		},
		{
			name: "sub funding balance", method: http.MethodGet, path: "/api/v5/asset/subaccount/balances", query: "subAcct=test1&ccy=ETH", private: true,
			reply: `{"code":"0","msg":"","data":[{"availBal":"37.11","bal":"37.11","ccy":"ETH","frozenBal":"0"}]}`,
			call: func(c *Client) error {
				items, err := c.SubAccountAssetBalances(ctx, "test1", []string{"ETH"})
				if err == nil && (len(items) != 1 || items[0].Balance != "37.11") {
					t.Errorf("SubAccountAssetBalances() = %+v", items)
				}
				return err
			},
		},
		{
			name: "sub max withdrawal", method: http.MethodGet, path: "/api/v5/account/subaccount/max-withdrawal", query: "subAcct=test1", private: true,
			reply: `{"code":"0","msg":"","data":[{"ccy":"BTC","maxWd":"3","maxWdEx":"","spotOffsetMaxWd":"3","spotOffsetMaxWdEx":""}]}`,
			call: func(c *Client) error {
				items, err := c.SubAccountMaxWithdrawals(ctx, "test1", nil)
				if err == nil && (len(items) != 1 || items[0].MaxWithdrawal != "3") {
					t.Errorf("SubAccountMaxWithdrawals() = %+v", items)
				}
				return err
			},
		},
		{
			name: "sub bills", method: http.MethodGet, path: "/api/v5/asset/subaccount/bills", query: "type=0&subAcct=hahatest1", private: true,
			reply: `{"code":"0","msg":"","data":[{"amt":"1.1","billId":"89887685","ccy":"USDT","subAcct":"hahatest1","ts":"1712560959000","type":"0"}]}`,
			call: func(c *Client) error {
				items, err := c.SubAccountBills(ctx, SubAccountBillsRequest{Type: "0", SubAccount: "hahatest1"})
				if err == nil && (len(items) != 1 || items[0].Amount != "1.1") {
					t.Errorf("SubAccountBills() = %+v", items)
				}
				return err
			},
		},
		{
			name: "managed sub bills", method: http.MethodGet, path: "/api/v5/asset/subaccount/managed-subaccount-bills", query: "subUid=77", private: true,
			reply: `{"code":"0","msg":"","data":[{"billId":"12344","type":"1","ccy":"BTC","amt":"2","subAcct":"test-1","subUid":"77","ts":"1597026383085"}]}`,
			call: func(c *Client) error {
				items, err := c.ManagedSubAccountBills(ctx, SubAccountBillsRequest{SubUID: "77"})
				if err == nil && (len(items) != 1 || items[0].SubUID != "77") {
					t.Errorf("ManagedSubAccountBills() = %+v", items)
				}
				return err
			},
		},
		{
			name: "sub transfer", method: http.MethodPost, path: "/api/v5/asset/subaccount/transfer", private: true,
			body:  `{"ccy":"USDT","amt":"1.5","from":"6","to":"6","fromSubAccount":"a","toSubAccount":"b","omitPosRisk":"true"}`,
			reply: `{"code":"0","msg":"","data":[{"transId":"12345"}]}`,
			call: func(c *Client) error {
				item, err := c.TransferBetweenSubAccounts(ctx, SubAccountTransferRequest{
					Currency: "USDT", Amount: "1.5", From: AssetAccountFunding, To: AssetAccountFunding,
					FromSubAccount: "a", ToSubAccount: "b", OmitPositionRisk: true,
				})
				if err == nil && item.TransferID != "12345" {
					t.Errorf("TransferBetweenSubAccounts() = %+v", item)
				}
				return err
			},
		},
	})
}

func TestAssetPermissionsAndValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		called = true
		_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[]}`)
	}))
	defer server.Close()
	client := newPermissionedTestClient(t, server.URL, []credential.Permission{credential.PermissionRead}, time.Now)

	withdrawRequest := WithdrawalRequest{
		Currency: "USDT", Amount: "1", Destination: WithdrawalDestinationOnChain, ToAddress: "TXYZ", Chain: "USDT-TRC20",
	}
	transferRequest := AssetTransferRequest{
		Currency: "USDT", Amount: "1", From: AssetAccountFunding, To: AssetAccountTrading,
	}
	quoteRequest := ConvertQuoteRequest{
		BaseCurrency: "ETH", QuoteCurrency: "USDT", Side: SideBuy, RFQSize: "1", RFQSizeCurrency: "USDT",
	}
	subTransfer := SubAccountTransferRequest{
		Currency: "USDT", Amount: "1", From: AssetAccountFunding, To: AssetAccountFunding,
		FromSubAccount: "a", ToSubAccount: "b",
	}
	permissionCases := []struct {
		name string
		call func() error
	}{
		{"withdraw", func() error { _, err := client.Withdraw(ctx, withdrawRequest); return err }},
		{"cancel withdrawal", func() error { _, err := client.CancelWithdrawal(ctx, "1"); return err }},
		{"transfer", func() error { _, err := client.AssetTransfer(ctx, transferRequest); return err }},
		{"convert quote", func() error { _, err := client.ConvertEstimateQuote(ctx, quoteRequest); return err }},
		{"sub-account transfer", func() error { _, err := client.TransferBetweenSubAccounts(ctx, subTransfer); return err }},
		{"set transfer out", func() error { _, err := client.SetSubAccountTransferOut(ctx, []string{"a"}, true); return err }},
		{"modify api key", func() error {
			_, err := client.ModifySubAccountAPIKey(ctx, SubAccountAPIKeyModifyRequest{SubAccount: "a", APIKey: "k"})
			return err
		}},
	}
	for _, tc := range permissionCases {
		t.Run("permission "+tc.name, func(t *testing.T) {
			var apiError *trade.APIError
			err := tc.call()
			if !errors.As(err, &apiError) || apiError.Category != trade.ErrorAuthorization {
				t.Fatalf("error = %v, want authorization error", err)
			}
		})
	}
	if called {
		t.Fatal("request reached the server without the required permission")
	}

	validationCases := []struct {
		name string
		call func() error
	}{
		{"transfer type", func() error {
			request := transferRequest
			request.Type = "9"
			_, err := client.AssetTransfer(ctx, request)
			return err
		}},
		{"transfer sub-account required", func() error {
			request := transferRequest
			request.Type = TransferTypeMasterToSub
			_, err := client.AssetTransfer(ctx, request)
			return err
		}},
		{"transfer amount", func() error {
			request := transferRequest
			request.Amount = "0"
			_, err := client.AssetTransfer(ctx, request)
			return err
		}},
		{"transfer state identity", func() error {
			_, err := client.AssetTransferStates(ctx, AssetTransferStateRequest{})
			return err
		}},
		{"withdraw destination", func() error {
			request := withdrawRequest
			request.Destination = "5"
			_, err := client.Withdraw(ctx, request)
			return err
		}},
		{"withdraw receiver wallet", func() error {
			request := withdrawRequest
			request.Receiver = &ReceiverInfo{WalletType: "other"}
			_, err := client.Withdraw(ctx, request)
			return err
		}},
		{"currency comma", func() error {
			_, err := client.AssetBalances(ctx, []string{"BTC,ETH"})
			return err
		}},
		{"currency count", func() error {
			currencies := make([]string, 21)
			for index := range currencies {
				currencies[index] = string(rune('A'+index)) + "X"
			}
			_, err := client.AssetCurrencies(ctx, currencies)
			return err
		}},
		{"status exclusive", func() error {
			_, err := client.DepositWithdrawStatuses(ctx, DepositWithdrawStatusRequest{WithdrawalID: "1", TxID: "2"})
			return err
		}},
		{"deposit status fields", func() error {
			_, err := client.DepositWithdrawStatuses(ctx, DepositWithdrawStatusRequest{TxID: "2"})
			return err
		}},
		{"bills paging type", func() error {
			_, err := client.AssetBills(ctx, AssetBillsRequest{PagingType: "1"})
			return err
		}},
		{"convert side", func() error {
			request := quoteRequest
			request.Side = "hold"
			_, err := client.ConvertEstimateQuote(ctx, request)
			return err
		}},
		{"sub-account same", func() error {
			request := subTransfer
			request.ToSubAccount = "a"
			_, err := client.TransferBetweenSubAccounts(ctx, request)
			return err
		}},
		{"api key permission", func() error {
			_, err := client.ModifySubAccountAPIKey(ctx, SubAccountAPIKeyModifyRequest{
				SubAccount: "a", APIKey: "k", Permissions: []string{"admin"},
			})
			return err
		}},
		{"sub bills subUid", func() error {
			_, err := client.SubAccountBills(ctx, SubAccountBillsRequest{SubUID: "1"})
			return err
		}},
	}
	for _, tc := range validationCases {
		t.Run("validation "+tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
		})
	}
}
