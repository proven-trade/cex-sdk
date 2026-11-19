package korbit

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/proven-trade/cex-sdk/credential"
)

const withdrawableAddressesSample = `{"success":true,"data":[` +
	`{"network":"BTC","currency":"btc","address":"1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"},` +
	`{"network":"XRP","address":"rEb8TK3gBgk5auZkwc6sHnwrGVJH8DuaLh","secondaryAddress":"1234"}` +
	`]}`

const withdrawableAmountsSample = `{"success":true,"data":[` +
	`{"currency":"btc","withdrawableAmount":"1.52","withdrawalInUseAmount":"0.005"},` +
	`{"currency":"eth","withdrawableAmount":"10.52","withdrawalInUseAmount":"2.5"}` +
	`]}`

const coinWithdrawalSample = `{"success":true,"data":{"id":1234,"quantity":"1.234","fee":"0.0001","currency":"btc","status":"done","network":"BTC","address":"1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa","secondaryAddress":null,"transactionHash":"0x000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f","createdAt":1700000000000}}`

const recentCoinWithdrawalsSample = `{"success":true,"data":[` +
	`{"id":1234,"quantity":"1.234","fee":"0.0001","currency":"btc","status":"done","network":"BTC","address":"1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa","secondaryAddress":null,"transactionHash":"0x000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f","createdAt":1700000000000},` +
	`{"id":1235,"quantity":"20","fee":"1","currency":"xrp","status":"reviewing","network":"XRP","address":"rEb8TK3gBgk5auZkwc6sHnwrGVJH8DuaLh","secondaryAddress":"1234","transactionHash":null,"createdAt":1700000020000}` +
	`]}`

func TestCoinWithdrawalReadEndpoints(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{
		"GET /v2/coin/withdrawableAddresses": withdrawableAddressesSample,
		"GET /v2/coin/withdrawableAmount":    withdrawableAmountsSample,
		"GET /v2/coin/recentWithdrawals":     recentCoinWithdrawalsSample,
		"GET /v2/coin/withdrawal":            coinWithdrawalSample,
	})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)
	ctx := context.Background()

	addresses, err := client.WithdrawableAddresses(ctx, WithdrawableAddressesRequest{})
	if err != nil || len(addresses) != 2 {
		t.Fatalf("WithdrawableAddresses() = %+v, error = %v", addresses, err)
	}
	if addresses[0].Network != "BTC" || addresses[0].Currency != "btc" || addresses[0].SecondaryAddress != "" ||
		addresses[0].Address != "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa" || len(addresses[0].Raw) == 0 ||
		addresses[1].Currency != "" || addresses[1].SecondaryAddress != "1234" {
		t.Fatalf("withdrawable addresses = %+v", addresses)
	}

	amounts, err := client.WithdrawableAmounts(ctx, WithdrawableAmountsRequest{Currency: "btc"})
	if err != nil || len(amounts) != 2 {
		t.Fatalf("WithdrawableAmounts() = %+v, error = %v", amounts, err)
	}
	if amounts[0].Currency != "btc" || amounts[0].Withdrawable != "1.52" || amounts[0].WithdrawalInUse != "0.005" ||
		len(amounts[0].Raw) == 0 || amounts[1].Withdrawable != "10.52" {
		t.Fatalf("withdrawable amounts = %+v", amounts)
	}

	recent, err := client.RecentCoinWithdrawals(ctx, RecentCoinWithdrawalsRequest{Currency: "btc", Limit: 100})
	if err != nil || len(recent) != 2 {
		t.Fatalf("RecentCoinWithdrawals() = %+v, error = %v", recent, err)
	}
	if recent[0].ID != 1234 || recent[0].Status != CoinWithdrawalStatusDone || recent[0].Quantity != "1.234" ||
		recent[0].Fee != "0.0001" || recent[0].SecondaryAddress != "" || recent[0].TransactionHash == "" ||
		recent[0].CreatedAt != 1700000000000 || len(recent[0].Raw) == 0 {
		t.Fatalf("completed withdrawal = %+v", recent[0])
	}
	if recent[1].Status != CoinWithdrawalStatusReviewing || recent[1].TransactionHash != "" ||
		recent[1].SecondaryAddress != "1234" || recent[1].Network != "XRP" {
		t.Fatalf("withdrawal under review = %+v", recent[1])
	}

	withdrawal, err := client.CoinWithdrawal(ctx, CoinWithdrawalRequest{Currency: "btc", CoinWithdrawalID: 1234})
	if err != nil || withdrawal.ID != 1234 || withdrawal.Status != CoinWithdrawalStatusDone ||
		withdrawal.Currency != "btc" || withdrawal.Network != "BTC" || len(withdrawal.Raw) == 0 {
		t.Fatalf("CoinWithdrawal() = %+v, error = %v", withdrawal, err)
	}

	want := []loggedRequest{
		{http.MethodGet, "/v2/coin/withdrawableAddresses", "recvWindow=5000&timestamp=1700000000123", true},
		{http.MethodGet, "/v2/coin/withdrawableAmount", "currency=btc&recvWindow=5000&timestamp=1700000000123", true},
		{http.MethodGet, "/v2/coin/recentWithdrawals", "currency=btc&limit=100&recvWindow=5000&timestamp=1700000000123", true},
		{http.MethodGet, "/v2/coin/withdrawal", "coinWithdrawalId=1234&currency=btc&recvWindow=5000&timestamp=1700000000123", true},
	}
	if entries := recorder.snapshot(); !slices.Equal(entries, want) {
		t.Fatalf("requests = %+v, want %+v", entries, want)
	}
}

func TestWithdrawableAmountsWithoutCurrencyQueriesEveryAsset(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{"GET /v2/coin/withdrawableAmount": withdrawableAmountsSample})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)
	if _, err := client.WithdrawableAmounts(context.Background(), WithdrawableAmountsRequest{AccountSeq: 1}); err != nil {
		t.Fatalf("WithdrawableAmounts() error = %v", err)
	}
	entries := recorder.snapshot()
	if len(entries) != 1 || entries[0].Params != "accountSeq=1&recvWindow=5000&timestamp=1700000000123" {
		t.Fatalf("requests = %+v, want no currency parameter", entries)
	}
}

func TestCreateAndCancelCoinWithdrawal(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{
		"POST /v2/coin/withdrawal":   `{"success":true,"data":{"status":"actionRequired","coinWithdrawalId":1234}}`,
		"DELETE /v2/coin/withdrawal": `{"success":true}`,
	})
	client := newAssetTestClient(t, server.URL, credential.PermissionWithdraw)
	ctx := context.Background()

	receipt, err := client.CreateCoinWithdrawal(ctx, CreateCoinWithdrawalRequest{
		Currency: "xrp", Network: "XRP", Amount: "20.5", Address: "rEb8TK3gBgk5auZkwc6sHnwrGVJH8DuaLh",
		SecondaryAddress: "1234",
	})
	if err != nil || receipt.CoinWithdrawalID != 1234 || receipt.Status != CoinWithdrawalStatusActionRequired ||
		len(receipt.Raw) == 0 {
		t.Fatalf("CreateCoinWithdrawal() = %+v, error = %v", receipt, err)
	}
	canceled, err := client.CancelCoinWithdrawal(ctx, CancelCoinWithdrawalRequest{AccountSeq: 1, CoinWithdrawalID: 1234})
	if err != nil || !canceled.Accepted || string(canceled.Raw) != `{"success":true}` {
		t.Fatalf("CancelCoinWithdrawal() = %+v, error = %v", canceled, err)
	}

	want := []loggedRequest{
		{
			http.MethodPost, "/v2/coin/withdrawal",
			"address=rEb8TK3gBgk5auZkwc6sHnwrGVJH8DuaLh&amount=20.5&currency=xrp&network=XRP&recvWindow=5000&secondaryAddress=1234&timestamp=1700000000123",
			true,
		},
		{
			http.MethodDelete, "/v2/coin/withdrawal",
			"accountSeq=1&coinWithdrawalId=1234&recvWindow=5000&timestamp=1700000000123",
			true,
		},
	}
	if entries := recorder.snapshot(); !slices.Equal(entries, want) {
		t.Fatalf("requests = %+v, want %+v", entries, want)
	}
}

func TestCreateCoinWithdrawalOmitsEmptySecondaryAddress(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{
		"POST /v2/coin/withdrawal": `{"success":true,"data":{"status":"pending","coinWithdrawalId":77}}`,
	})
	client := newAssetTestClient(t, server.URL, credential.PermissionWithdraw)
	receipt, err := client.CreateCoinWithdrawal(context.Background(), CreateCoinWithdrawalRequest{
		AccountSeq: 1, Currency: "btc", Network: "BTC", Amount: "0.02521236", Address: "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",
	})
	if err != nil || receipt.CoinWithdrawalID != 77 || receipt.Status != CoinWithdrawalStatusPending {
		t.Fatalf("CreateCoinWithdrawal() = %+v, error = %v", receipt, err)
	}
	wantParams := "accountSeq=1&address=1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa&amount=0.02521236&currency=btc&network=BTC&recvWindow=5000&timestamp=1700000000123"
	if entries := recorder.snapshot(); len(entries) != 1 || entries[0].Params != wantParams {
		t.Fatalf("requests = %+v, want %q", entries, wantParams)
	}
}

func TestCoinWithdrawalResponsesNeedIdentity(t *testing.T) {
	t.Parallel()

	server, _ := newKorbitStubServer(t, map[string]string{
		"GET /v2/coin/withdrawal": `{"success":true,"data":{"currency":"btc"}}`,
	})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)
	if _, err := client.CoinWithdrawal(context.Background(), CoinWithdrawalRequest{Currency: "btc", CoinWithdrawalID: 1}); err == nil {
		t.Fatal("CoinWithdrawal() accepted a response without id")
	}
}

func TestCoinWithdrawalRejectsInvalidRequestBeforeSending(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{"POST /v2/coin/withdrawal": coinWithdrawalSample})
	client := newAssetTestClient(t, server.URL, allTestPermissions...)
	ctx := context.Background()
	valid := CreateCoinWithdrawalRequest{
		Currency: "btc", Network: "BTC", Amount: "0.02521236", Address: "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",
	}
	create := func(change func(*CreateCoinWithdrawalRequest)) func() error {
		return func() error {
			request := valid
			change(&request)
			_, err := client.CreateCoinWithdrawal(ctx, request)
			return err
		}
	}
	requireRejectedBeforeSend(t, recorder, []namedCall{
		{"withdrawal on a sub account", create(func(request *CreateCoinWithdrawalRequest) { request.AccountSeq = 2 })},
		{"withdrawal without currency", create(func(request *CreateCoinWithdrawalRequest) { request.Currency = "" })},
		{"withdrawal without network", create(func(request *CreateCoinWithdrawalRequest) { request.Network = "" })},
		{"withdrawal with invalid network", create(func(request *CreateCoinWithdrawalRequest) { request.Network = "B/C" })},
		{"withdrawal without amount", create(func(request *CreateCoinWithdrawalRequest) { request.Amount = "" })},
		{"withdrawal of zero", create(func(request *CreateCoinWithdrawalRequest) { request.Amount = "0" })},
		{"withdrawal with exponent amount", create(func(request *CreateCoinWithdrawalRequest) { request.Amount = "1e-3" })},
		{"withdrawal without address", create(func(request *CreateCoinWithdrawalRequest) { request.Address = "" })},
		{"withdrawal with spaced address", create(func(request *CreateCoinWithdrawalRequest) { request.Address = "1A1zP1 eP5" })},
		{"withdrawal with control character tag", create(func(request *CreateCoinWithdrawalRequest) { request.SecondaryAddress = "12\n34" })},
		{"cancel without ID", func() error {
			_, err := client.CancelCoinWithdrawal(ctx, CancelCoinWithdrawalRequest{})
			return err
		}},
		{"cancel on a sub account", func() error {
			_, err := client.CancelCoinWithdrawal(ctx, CancelCoinWithdrawalRequest{AccountSeq: 2, CoinWithdrawalID: 1})
			return err
		}},
		{"withdrawal lookup without ID", func() error {
			_, err := client.CoinWithdrawal(ctx, CoinWithdrawalRequest{Currency: "btc"})
			return err
		}},
		{"withdrawal lookup without currency", func() error {
			_, err := client.CoinWithdrawal(ctx, CoinWithdrawalRequest{CoinWithdrawalID: 1})
			return err
		}},
		{"recent withdrawals over the limit", func() error {
			_, err := client.RecentCoinWithdrawals(ctx, RecentCoinWithdrawalsRequest{Currency: "btc", Limit: 101})
			return err
		}},
		{"recent withdrawals without currency", func() error {
			_, err := client.RecentCoinWithdrawals(ctx, RecentCoinWithdrawalsRequest{})
			return err
		}},
		{"withdrawable addresses on a sub account", func() error {
			_, err := client.WithdrawableAddresses(ctx, WithdrawableAddressesRequest{AccountSeq: 2})
			return err
		}},
		{"withdrawable amounts with upper case currency", func() error {
			_, err := client.WithdrawableAmounts(ctx, WithdrawableAmountsRequest{Currency: "BTC"})
			return err
		}},
	})
}
