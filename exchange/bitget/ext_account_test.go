package bitget

import (
	"context"
	"net/http"
	"testing"

	"github.com/proven-trade/cex-sdk/credential"
)

func TestExtAccountAndAssetEndpoints(t *testing.T) {
	t.Parallel()
	client, seen := newExtClient(t, credential.PermissionRead, credential.PermissionTrade, credential.PermissionTransfer)
	ctx := context.Background()

	cases := []struct {
		name   string
		call   func() error
		method string
		path   string
		query  string
		body   string
	}{
		{"set margin", func() error {
			_, err := client.SetMargin(ctx, SetMarginRequest{
				Category: CategoryUSDTFutures, Symbol: "BTCUSDT", Operation: "add", Amount: "10",
			})
			return err
		}, http.MethodPost, "/api/v3/account/set-margin", "",
			`{"category":"USDT-FUTURES","symbol":"BTCUSDT","operation":"add","amount":"10"}`},
		{"open interest limit", func() error {
			_, err := client.AccountOpenInterestLimit(ctx, OpenInterestRequest{Category: CategoryUSDTFutures, Symbol: "BTCUSDT"})
			return err
		}, http.MethodGet, "/api/v3/account/open-interest-limit", "category=USDT-FUTURES&symbol=BTCUSDT", ""},
		{"delta info", func() error { _, err := client.DeltaInfo(ctx); return err },
			http.MethodGet, "/api/v3/account/delta-info", "", ""},
		{"deduct info", func() error { _, err := client.DeductInfo(ctx); return err },
			http.MethodGet, "/api/v3/account/deduct-info", "", ""},
		{"switch status", func() error { _, err := client.SwitchStatus(ctx); return err },
			http.MethodGet, "/api/v3/account/switch-status", "", ""},
		{"switch account", func() error { _, err := client.SwitchAccount(ctx); return err },
			http.MethodPost, "/api/v3/account/switch", "", ""},
		{"convert records", func() error {
			_, err := client.ConvertRecords(ctx, ConvertRecordsRequest{FromCoin: "USDT", ToCoin: "BTC"})
			return err
		}, http.MethodGet, "/api/v3/account/convert-records", "fromCoin=USDT&toCoin=BTC", ""},
		{"repayable coins", func() error { _, err := client.RepayableCoins(ctx); return err },
			http.MethodGet, "/api/v3/account/repayable-coins", "", ""},
		{"payment coins", func() error { _, err := client.PaymentCoins(ctx); return err },
			http.MethodGet, "/api/v3/account/payment-coins", "", ""},
		{"max transferable", func() error { _, err := client.MaxTransferable(ctx, "USDT"); return err },
			http.MethodGet, "/api/v3/account/max-transferable", "coin=USDT", ""},
		{"max withdrawal", func() error { _, err := client.MaxWithdrawal(ctx, "USDT"); return err },
			http.MethodGet, "/api/v3/account/max-withdrawal", "coin=USDT", ""},
		{"sub transfer records", func() error {
			_, err := client.SubTransferRecords(ctx, SubTransferRecordsRequest{SubUID: "9", Coin: "USDT"})
			return err
		}, http.MethodGet, "/api/v3/account/sub-transfer-record", "coin=USDT&subUid=9", ""},
		{"sub unified assets", func() error { _, err := client.SubUnifiedAssets(ctx, "9", Page{Limit: 10}); return err },
			http.MethodGet, "/api/v3/account/sub-unified-assets", "limit=10&subUid=9", ""},
		{"sub deposit address", func() error { _, err := client.SubDepositAddress(ctx, "9", "USDT", "trc20", ""); return err },
			http.MethodGet, "/api/v3/account/sub-deposit-address", "chain=trc20&coin=USDT&subUid=9", ""},
		{"sub deposit records", func() error {
			_, err := client.SubDepositRecords(ctx, SubDepositRecordsRequest{SubUID: "9", Coin: "USDT"})
			return err
		}, http.MethodGet, "/api/v3/account/sub-deposit-records", "coin=USDT&subUid=9", ""},
		{"withdraw addresses", func() error {
			_, err := client.WithdrawAddresses(ctx, WithdrawAddressesRequest{Coin: "USDT"})
			return err
		}, http.MethodGet, "/api/v3/account/withdraw-address", "coin=USDT", ""},
		{"modify sub", func() error {
			_, err := client.ModifySubAccount(ctx, "9", map[string]string{"note": "n"})
			return err
		}, http.MethodPost, "/api/v3/user/modify-sub", "", `{"note":"n","subUid":"9"}`},
		{"update sub api", func() error {
			_, err := client.UpdateSubAPIKey(ctx, UpdateSubAPIKeyRequest{APIKey: "key-1", Passphrase: "pass"})
			return err
		}, http.MethodPost, "/api/v3/user/update-sub-api", "", `{"apikey":"key-1","passphrase":"pass"}`},
	}
	for index, tc := range cases {
		if err := tc.call(); err != nil {
			t.Fatalf("%s error = %v", tc.name, err)
		}
		got := (*seen)[index]
		want := seenRequest{tc.method, tc.path, tc.query, tc.body}
		if got != want {
			t.Errorf("%s request = %+v, want %+v", tc.name, got, want)
		}
	}
}

func TestExtAccountMutationsRequirePermission(t *testing.T) {
	t.Parallel()
	client, seen := newExtClient(t, credential.PermissionRead)
	ctx := context.Background()
	if _, err := client.SwitchAccount(ctx); !isAuthorizationError(err) {
		t.Fatalf("SwitchAccount() error = %v, want authorization error", err)
	}
	if _, err := client.ModifySubAccount(ctx, "9", map[string]string{"note": "n"}); !isAuthorizationError(err) {
		t.Fatalf("ModifySubAccount() error = %v, want authorization error", err)
	}
	if _, err := client.SetMargin(ctx, SetMarginRequest{
		Category: CategoryUSDTFutures, Symbol: "BTCUSDT", Operation: "add", Amount: "1",
	}); !isAuthorizationError(err) {
		t.Fatalf("SetMargin() error = %v, want authorization error", err)
	}
	if len(*seen) != 0 {
		t.Fatalf("unauthorized mutations sent %d requests", len(*seen))
	}
	if _, err := client.MaxTransferable(ctx, ""); err == nil {
		t.Fatalf("MaxTransferable(empty) error = nil, want validation error")
	}
}
