package korbit

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/proven-trade/cex-sdk/credential"
)

const coinDepositAddressesSample = `{"success":true,"data":[` +
	`{"currency":"btc","network":"BTC","address":"1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"},` +
	`{"currency":"xrp","network":"XRP","address":"rEb8TK3gBgk5auZkwc6sHnwrGVJH8DuaLh","secondaryAddress":"1234"}` +
	`]}`

const coinDepositAddressSample = `{"success":true,"data":{"currency":"btc","network":"BTC","address":"1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"}}`

const recentCoinDepositsSample = `{"success":true,"data":[` +
	`{"id":1234,"network":"BTC","address":"1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa","secondaryAddress":null,"status":"done","transactionHash":"0x000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f","currency":"btc","quantity":"1.234","createdAt":1700000000000,"filledAt":1700000030000},` +
	`{"id":1235,"network":"BTC","address":"1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa","secondaryAddress":null,"status":"pending","transactionHash":"0x000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f","currency":"btc","quantity":"1.234","createdAt":1700000010000,"filledAt":null}` +
	`]}`

const coinDepositSample = `{"success":true,"data":{"id":1234,"network":"BTC","address":"1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa","secondaryAddress":"memo-1","status":"actionRequired","transactionHash":"0xabc","currency":"btc","quantity":"1.234","createdAt":1700000000000,"filledAt":null}}`

func TestCoinDepositEndpoints(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{
		"GET /v2/coin/depositAddresses": coinDepositAddressesSample,
		"GET /v2/coin/depositAddress":   coinDepositAddressSample,
		"POST /v2/coin/depositAddress":  coinDepositAddressSample,
		"GET /v2/coin/recentDeposits":   recentCoinDepositsSample,
		"GET /v2/coin/deposit":          coinDepositSample,
	})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead, credential.PermissionTransfer)
	ctx := context.Background()

	addresses, err := client.CoinDepositAddresses(ctx, CoinDepositAddressesRequest{AccountSeq: 1})
	if err != nil || len(addresses) != 2 {
		t.Fatalf("CoinDepositAddresses() = %+v, error = %v", addresses, err)
	}
	if addresses[0].Currency != "btc" || addresses[0].Network != "BTC" || addresses[0].SecondaryAddress != "" ||
		addresses[0].Address != "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa" || len(addresses[0].Raw) == 0 ||
		addresses[1].SecondaryAddress != "1234" || addresses[1].Network != "XRP" {
		t.Fatalf("deposit addresses = %+v", addresses)
	}

	address, err := client.CoinDepositAddress(ctx, CoinDepositAddressRequest{Currency: "btc", Network: "BTC"})
	if err != nil || address.Address != "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa" || address.Network != "BTC" || len(address.Raw) == 0 {
		t.Fatalf("CoinDepositAddress() = %+v, error = %v", address, err)
	}

	created, err := client.CreateCoinDepositAddress(ctx, CreateCoinDepositAddressRequest{Currency: "btc", Network: "BTC"})
	if err != nil || created.Address != "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa" || created.Currency != "btc" || len(created.Raw) == 0 {
		t.Fatalf("CreateCoinDepositAddress() = %+v, error = %v", created, err)
	}

	deposits, err := client.RecentCoinDeposits(ctx, RecentCoinDepositsRequest{Currency: "btc", Limit: 100})
	if err != nil || len(deposits) != 2 {
		t.Fatalf("RecentCoinDeposits() = %+v, error = %v", deposits, err)
	}
	if deposits[0].ID != 1234 || deposits[0].Status != CoinDepositStatusDone || deposits[0].Quantity != "1.234" ||
		deposits[0].SecondaryAddress != "" || deposits[0].FilledAt == nil || *deposits[0].FilledAt != 1700000030000 ||
		deposits[0].TransactionHash == "" || deposits[0].CreatedAt != 1700000000000 || len(deposits[0].Raw) == 0 {
		t.Fatalf("completed deposit = %+v", deposits[0])
	}
	if deposits[1].Status != CoinDepositStatusPending || deposits[1].FilledAt != nil {
		t.Fatalf("pending deposit = %+v", deposits[1])
	}

	deposit, err := client.CoinDeposit(ctx, CoinDepositRequest{Currency: "btc", CoinDepositID: 1234})
	if err != nil || deposit.ID != 1234 || deposit.Status != CoinDepositStatusActionRequired ||
		deposit.SecondaryAddress != "memo-1" || deposit.FilledAt != nil || len(deposit.Raw) == 0 {
		t.Fatalf("CoinDeposit() = %+v, error = %v", deposit, err)
	}

	want := []loggedRequest{
		{http.MethodGet, "/v2/coin/depositAddresses", "accountSeq=1&recvWindow=5000&timestamp=1700000000123", true},
		{http.MethodGet, "/v2/coin/depositAddress", "currency=btc&network=BTC&recvWindow=5000&timestamp=1700000000123", true},
		{http.MethodPost, "/v2/coin/depositAddress", "currency=btc&network=BTC&recvWindow=5000&timestamp=1700000000123", true},
		{http.MethodGet, "/v2/coin/recentDeposits", "currency=btc&limit=100&recvWindow=5000&timestamp=1700000000123", true},
		{http.MethodGet, "/v2/coin/deposit", "coinDepositId=1234&currency=btc&recvWindow=5000&timestamp=1700000000123", true},
	}
	if entries := recorder.snapshot(); !slices.Equal(entries, want) {
		t.Fatalf("requests = %+v, want %+v", entries, want)
	}
}

func TestCoinDepositAddressDefaultsToExchangeNetworkWhenOmitted(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{"GET /v2/coin/depositAddress": coinDepositAddressSample})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)
	if _, err := client.CoinDepositAddress(context.Background(), CoinDepositAddressRequest{Currency: "btc"}); err != nil {
		t.Fatalf("CoinDepositAddress() error = %v", err)
	}
	entries := recorder.snapshot()
	if len(entries) != 1 || entries[0].Params != "currency=btc&recvWindow=5000&timestamp=1700000000123" {
		t.Fatalf("requests = %+v, want no network parameter", entries)
	}
}

func TestCoinDepositResponsesNeedIdentity(t *testing.T) {
	t.Parallel()

	server, _ := newKorbitStubServer(t, map[string]string{
		"GET /v2/coin/depositAddress": `{"success":true,"data":{"currency":"btc"}}`,
		"GET /v2/coin/deposit":        `{"success":true,"data":{"currency":"btc"}}`,
	})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)
	ctx := context.Background()
	if _, err := client.CoinDepositAddress(ctx, CoinDepositAddressRequest{Currency: "btc"}); err == nil {
		t.Error("CoinDepositAddress() accepted a response without address")
	}
	if _, err := client.CoinDeposit(ctx, CoinDepositRequest{Currency: "btc", CoinDepositID: 1}); err == nil {
		t.Error("CoinDeposit() accepted a response without id")
	}
}

func TestCoinDepositRejectsInvalidRequestBeforeSending(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{"GET /v2/coin/deposit": coinDepositSample})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead, credential.PermissionTransfer)
	ctx := context.Background()
	requireRejectedBeforeSend(t, recorder, []namedCall{
		{"deposit addresses on a sub account", func() error {
			_, err := client.CoinDepositAddresses(ctx, CoinDepositAddressesRequest{AccountSeq: 2})
			return err
		}},
		{"deposit address without currency", func() error {
			_, err := client.CoinDepositAddress(ctx, CoinDepositAddressRequest{Network: "BTC"})
			return err
		}},
		{"deposit address with invalid network", func() error {
			_, err := client.CoinDepositAddress(ctx, CoinDepositAddressRequest{Currency: "btc", Network: "B T C"})
			return err
		}},
		{"create address on a sub account", func() error {
			_, err := client.CreateCoinDepositAddress(ctx, CreateCoinDepositAddressRequest{AccountSeq: 3, Currency: "btc"})
			return err
		}},
		{"create address with upper case currency", func() error {
			_, err := client.CreateCoinDepositAddress(ctx, CreateCoinDepositAddressRequest{Currency: "BTC"})
			return err
		}},
		{"recent deposits without currency", func() error {
			_, err := client.RecentCoinDeposits(ctx, RecentCoinDepositsRequest{})
			return err
		}},
		{"recent deposits over the limit", func() error {
			_, err := client.RecentCoinDeposits(ctx, RecentCoinDepositsRequest{Currency: "btc", Limit: 101})
			return err
		}},
		{"deposit without ID", func() error {
			_, err := client.CoinDeposit(ctx, CoinDepositRequest{Currency: "btc"})
			return err
		}},
		{"deposit with negative ID", func() error {
			_, err := client.CoinDeposit(ctx, CoinDepositRequest{Currency: "btc", CoinDepositID: -5})
			return err
		}},
	})
}
