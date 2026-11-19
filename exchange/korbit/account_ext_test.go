package korbit

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/proven-trade/cex-sdk/credential"
)

const tradingFeePolicySample = `{"success":true,"data":[` +
	`{"symbol":"btc_krw","buyFeeCurrency":"btc","sellFeeCurrency":"krw","maxFeeRate":"0.002","takerFeeRate":"0.0015","makerFeeRate":"0"},` +
	`{"symbol":"etc_krw","buyFeeCurrency":"krw","sellFeeCurrency":"krw","maxFeeRate":"0.002","takerFeeRate":"0.0015","makerFeeRate":"0"}` +
	`]}`

const currentKeyInfoSample = `{"success":true,"data":{"apiKey":"FFSoRME97Sr7WBCMZJ_NO5Bj8MZ03EyArRzqyr1NKIA","userUuid":"f81d4fae-7dec-11d0-a765-00a0c91e6bf6","type":"ed25519","publicKey":"-----BEGIN PUBLIC KEY-----\nMCowBQYDK2VwAyEAk+Yp3C31eFwoky+zyRNB6rAv/lgULTeghxTQpqwQHzM=\n-----END PUBLIC KEY-----\n","permissions":["readBalances","readOrders","writeAccountTransfers"],"whitelist":"1.2.3.4,5.6.7.8","allowedAccountSeqs":[1,2],"expiration":1700000000000,"status":"activated","label":"test key","createdAt":1699990000000}}`

const accountsSample = `{"success":true,"data":[{"accountSeq":1},{"accountSeq":2,"name":"Short-term trading"}]}`

func TestAccountExtensionEndpoints(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{
		"GET /v2/tradingFeePolicy": tradingFeePolicySample,
		"GET /v2/currentKeyInfo":   currentKeyInfoSample,
		"GET /v2/accounts":         accountsSample,
	})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)
	ctx := context.Background()

	policies, err := client.TradingFeePolicy(ctx, TradingFeePolicyRequest{
		AccountSeq: 2, Symbols: []string{"btc_krw", "etc_krw"},
	})
	if err != nil || len(policies) != 2 {
		t.Fatalf("TradingFeePolicy() = %+v, error = %v", policies, err)
	}
	if policies[0].Symbol != "btc_krw" || policies[0].BuyFeeCurrency != "btc" || policies[0].SellFeeCurrency != "krw" ||
		policies[0].MaxFeeRate != "0.002" || policies[0].TakerFeeRate != "0.0015" || policies[0].MakerFeeRate != "0" ||
		len(policies[0].Raw) == 0 || policies[1].BuyFeeCurrency != "krw" {
		t.Fatalf("trading fee policies = %+v", policies)
	}

	info, err := client.CurrentKeyInfo(ctx)
	if err != nil {
		t.Fatalf("CurrentKeyInfo() error = %v", err)
	}
	wantPermissions := []APIKeyPermission{
		APIKeyPermissionReadBalances, APIKeyPermissionReadOrders, APIKeyPermissionWriteAccountTransfers,
	}
	if info.APIKey != "FFSoRME97Sr7WBCMZJ_NO5Bj8MZ03EyArRzqyr1NKIA" || info.UserUUID != "f81d4fae-7dec-11d0-a765-00a0c91e6bf6" ||
		info.Type != APIKeyTypeED25519 || !strings.HasPrefix(info.PublicKey, "-----BEGIN PUBLIC KEY-----\n") ||
		!slices.Equal(info.Permissions, wantPermissions) || info.Whitelist != "1.2.3.4,5.6.7.8" ||
		!slices.Equal(info.AllowedAccountSeqs, []int{1, 2}) || info.Expiration != 1700000000000 ||
		info.Status != APIKeyStatusActivated || info.Label != "test key" || info.CreatedAt != 1699990000000 ||
		len(info.Raw) == 0 {
		t.Fatalf("API key info = %+v", info)
	}

	accounts, err := client.Accounts(ctx)
	if err != nil || len(accounts) != 2 {
		t.Fatalf("Accounts() = %+v, error = %v", accounts, err)
	}
	if accounts[0].AccountSeq != 1 || accounts[0].Name != "" || accounts[1].AccountSeq != 2 ||
		accounts[1].Name != "Short-term trading" || len(accounts[1].Raw) == 0 {
		t.Fatalf("accounts = %+v", accounts)
	}

	entries := recorder.snapshot()
	want := []loggedRequest{
		{http.MethodGet, "/v2/tradingFeePolicy", "accountSeq=2&recvWindow=5000&symbol=btc_krw%2Cetc_krw&timestamp=1700000000123", true},
		{http.MethodGet, "/v2/currentKeyInfo", "recvWindow=5000&timestamp=1700000000123", true},
		{http.MethodGet, "/v2/accounts", "recvWindow=5000&timestamp=1700000000123", true},
	}
	if !slices.Equal(entries, want) {
		t.Fatalf("requests = %+v, want %+v", entries, want)
	}
}

func TestTradingFeePolicyOmitsEmptyFilters(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{"GET /v2/tradingFeePolicy": `{"success":true,"data":[]}`})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)
	policies, err := client.TradingFeePolicy(context.Background(), TradingFeePolicyRequest{})
	if err != nil || policies == nil || len(policies) != 0 {
		t.Fatalf("TradingFeePolicy() = %#v, error = %v, want an empty non-nil list", policies, err)
	}
	entries := recorder.snapshot()
	if len(entries) != 1 || entries[0].Params != "recvWindow=5000&timestamp=1700000000123" {
		t.Fatalf("requests = %+v, want no accountSeq or symbol", entries)
	}
}

func TestTradingFeePolicyRejectsInvalidRequestBeforeSending(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{"GET /v2/tradingFeePolicy": `{"success":true,"data":[]}`})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)
	tooMany := make([]string, 101)
	for index := range tooMany {
		tooMany[index] = "btc_krw"
	}
	requests := map[string]TradingFeePolicyRequest{
		"negative accountSeq": {AccountSeq: -1},
		"invalid symbol":      {Symbols: []string{"btckrw"}},
		"duplicate symbol":    {Symbols: []string{"btc_krw", "btc_krw"}},
		"too many symbols":    {Symbols: tooMany},
	}
	calls := make([]namedCall, 0, len(requests))
	for name, request := range requests {
		calls = append(calls, namedCall{name, func() error {
			_, err := client.TradingFeePolicy(context.Background(), request)
			return err
		}})
	}
	requireRejectedBeforeSend(t, recorder, calls)
}

func TestCurrentKeyInfoRequiresAPIKey(t *testing.T) {
	t.Parallel()

	server, _ := newKorbitStubServer(t, map[string]string{
		"GET /v2/currentKeyInfo": `{"success":true,"data":{"type":"hmac-sha256","permissions":[],"allowedAccountSeqs":[1]}}`,
	})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)
	if _, err := client.CurrentKeyInfo(context.Background()); err == nil {
		t.Fatal("CurrentKeyInfo() accepted a response without apiKey")
	}
}
