package upbit

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

func TestWalletStatusesAndAPIKeysEndpoints(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Remaining-Req", "group=default; min=1800; sec=29")
		switch request.Method + " " + request.URL.RequestURI() {
		case "GET /v1/status/wallet":
			verifySignedQuery(t, request, "")
			_, _ = io.WriteString(writer, `[{"currency":"BTC","wallet_state":"working","block_state":"normal","block_height":903942,"block_updated_at":"2025-07-04T08:02:05.526+00:00","block_elapsed_minutes":6,"net_type":"BTC","network_name":"Bitcoin"},{"currency":"ETH","wallet_state":"withdraw_only","block_state":"delayed","block_height":22844550,"block_updated_at":"2025-07-04T08:06:44.375+00:00","block_elapsed_minutes":42,"net_type":"ETH","network_name":"Ethereum"},{"currency":"XRP","wallet_state":"paused","block_elapsed_minutes":0,"net_type":"XRP","network_name":"XRP Ledger"}]`)
		case "GET /v1/api_keys":
			verifySignedQuery(t, request, "")
			_, _ = io.WriteString(writer, `[{"access_key":"8f3a2c1d9b7e6a5c4d3f2e1a0b9c8d7e6f5a4b3c","expire_at":"2026-06-25T11:22:54+09:00"},{"access_key":"0123456789abcdef0123456789abcdef01234567","expire_at":"2027-01-01T00:00:00+09:00"}]`)
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.RequestURI())
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, provider, limiter := newPrivateTestClient(t, server.URL, credential.PermissionRead)
	ctx := context.Background()

	statuses, err := client.WalletStatuses(ctx)
	if err != nil || len(statuses) != 3 || statuses[0].Currency != "BTC" || statuses[0].WalletState != WalletStateWorking ||
		statuses[0].BlockState != "normal" || statuses[0].BlockHeight != 903942 ||
		statuses[0].BlockUpdatedAt != "2025-07-04T08:02:05.526+00:00" || statuses[0].BlockElapsedMinutes != 6 ||
		statuses[0].NetType != "BTC" || statuses[0].NetworkName != "Bitcoin" || len(statuses[0].Raw) == 0 ||
		statuses[1].WalletState != WalletStateWithdrawOnly || statuses[1].BlockState != "delayed" ||
		statuses[1].BlockHeight != 22844550 || statuses[1].BlockElapsedMinutes != 42 ||
		statuses[2].WalletState != WalletStatePaused || statuses[2].BlockState != "" || statuses[2].BlockHeight != 0 ||
		statuses[2].NetworkName != "XRP Ledger" {
		t.Fatalf("WalletStatuses() = %+v, error = %v", statuses, err)
	}
	keys, err := client.APIKeys(ctx)
	if err != nil || len(keys) != 2 || keys[0].AccessKey != "8f3a2c1d9b7e6a5c4d3f2e1a0b9c8d7e6f5a4b3c" ||
		keys[0].ExpireAt != "2026-06-25T11:22:54+09:00" || len(keys[0].Raw) == 0 ||
		keys[1].AccessKey != "0123456789abcdef0123456789abcdef01234567" || keys[1].ExpireAt != "2027-01-01T00:00:00+09:00" {
		t.Fatalf("APIKeys() = %+v, error = %v", keys, err)
	}
	assertSecretsDestroyed(t, provider, 2)
	assertRateRule(t, limiter, "upbit:account:upbit-pocket:default:1second", 30, time.Second)
}

func TestAccountExtErrorResponses(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/status/wallet":
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(writer, `{"error":{"name":"jwt_verification","message":"JWT 헤더의 형식이 올바르지 않습니다."}}`)
		case "/v1/api_keys":
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(writer, `{"error":{"name":"out_of_scope","message":"허용되지 않은 기능입니다."}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, provider, _ := newPrivateTestClient(t, server.URL, credential.PermissionRead)
	ctx := context.Background()

	_, err := client.WalletStatuses(ctx)
	requireAPIError(t, err, http.StatusUnauthorized, "jwt_verification")
	if !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("WalletStatuses() error = %v, want ErrAuthentication", err)
	}
	_, err = client.APIKeys(ctx)
	requireAPIError(t, err, http.StatusUnauthorized, "out_of_scope")
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("APIKeys() error = %v, want ErrAuthorization from out_of_scope", err)
	}
	assertSecretsDestroyed(t, provider, 2)
}

func TestAccountExtMalformedBodyIsExchangeUnavailable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"unexpected":"object"}`)
	}))
	defer server.Close()

	client, _, _ := newPrivateTestClient(t, server.URL, credential.PermissionRead)
	if _, err := client.APIKeys(context.Background()); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("APIKeys() error = %v, want ErrExchangeUnavailable for a non-list body", err)
	}
}

func TestAccountExtPermissionBoundaries(t *testing.T) {
	t.Parallel()

	reads := []callCase{
		{"WalletStatuses needs read", func(ctx context.Context, client *Client) error {
			_, err := client.WalletStatuses(ctx)
			return err
		}},
		{"APIKeys needs read", func(ctx context.Context, client *Client) error {
			_, err := client.APIKeys(ctx)
			return err
		}},
	}
	assertRejectedBeforeSend(
		t, trade.ErrAuthorization, reads,
		credential.PermissionTrade, credential.PermissionTransfer, credential.PermissionWithdraw,
	)
}
