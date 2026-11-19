package bithumb

import (
	"context"
	"errors"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
)

const walletStatusSample = `[{"currency":"BTC","wallet_state":"working","block_state":"normal","block_height":852086,` +
	`"block_updated_at":"2024-07-14T13:43:57+09:00","block_elapsed_minutes":2,"net_type":"BTC","network_name":"Bitcoin"},` +
	`{"currency":"ETH","wallet_state":"withdraw_only","block_state":"delayed","block_height":20302440,` +
	`"block_updated_at":"2024-07-14T13:45:27+09:00","block_elapsed_minutes":35,"net_type":"ETH","network_name":"Ethereum"}]`

const apiKeysSample = `[{"access_key":"59683c90185742d69fd8fa1bc0cf27785c392afaa56ece","expire_at":"2025-06-11T09:00:00+09:00"},` +
	`{"access_key":"3e97926e9b75a6aeb637d2c172a292588502daccfb5cab","expire_at":"2025-06-12T09:00:00+09:00"}]`

func TestWalletStatusesAndAPIKeys(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{
		extOK("GET /v1/status/wallet", walletStatusSample),
		extOK("GET /v1/api_keys", apiKeysSample),
	}, extReadOnly)
	ctx := context.Background()

	statuses, err := fixture.client.WalletStatuses(ctx)
	if err != nil || len(statuses) != 2 || len(statuses[0].Raw) == 0 {
		t.Fatalf("WalletStatuses() = %+v, error = %v", statuses, err)
	}
	working, delayed := statuses[0], statuses[1]
	if working.Currency != "BTC" || working.WalletState != "working" || working.BlockState != "normal" ||
		working.BlockHeight != 852086 || working.BlockUpdatedAt != "2024-07-14T13:43:57+09:00" ||
		working.BlockElapsedMinutes != 2 || working.NetType != "BTC" || working.NetworkName != "Bitcoin" {
		t.Fatalf("working wallet status = %+v", working)
	}
	if delayed.Currency != "ETH" || delayed.WalletState != "withdraw_only" || delayed.BlockState != "delayed" ||
		delayed.BlockHeight != 20302440 || delayed.BlockElapsedMinutes != 35 {
		t.Fatalf("delayed wallet status = %+v", delayed)
	}

	keys, err := fixture.client.APIKeys(ctx)
	if err != nil || len(keys) != 2 || keys[0].AccessKey != "59683c90185742d69fd8fa1bc0cf27785c392afaa56ece" ||
		keys[0].ExpireAt != "2025-06-11T09:00:00+09:00" || keys[1].ExpireAt != "2025-06-12T09:00:00+09:00" ||
		len(keys[1].Raw) == 0 {
		t.Fatalf("APIKeys() = %+v, error = %v", keys, err)
	}

	records := extRequests(t, fixture.server, 2)
	assertExtRequest(t, records[0], "GET", "/v1/status/wallet", "", "", "")
	assertExtRequest(t, records[1], "GET", "/v1/api_keys", "", "", "")
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:private-other:1second", 140, 2)
	if calls, key, secret := fixture.provider.snapshot(); calls != 2 || !allZero(key) || !allZero(secret) {
		t.Fatalf("credential provider calls = %d, key zeroed = %t, secret zeroed = %t", calls, allZero(key), allZero(secret))
	}
}

func TestWalletStatusesAndAPIKeysRequireCredentialsAndMapErrors(t *testing.T) {
	t.Parallel()

	publicClient, _, _ := newExtClient(t, "http://127.0.0.1", errorSender{}, extReadOnly, func(config *Config) {
		config.Credentials = nil
		config.CredentialProvider = nil
	})
	if _, err := publicClient.WalletStatuses(context.Background()); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("WalletStatuses() without credentials error = %v, want ErrAuthentication", err)
	}
	if _, err := publicClient.APIKeys(context.Background()); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("APIKeys() without credentials error = %v, want ErrAuthentication", err)
	}

	fixture := newExtFixture(t, []extRoute{
		extReplies("GET /v1/status/wallet", extStatus(401, `{"error":{"name":"jwt_verification","message":"Jwt verification failed."}}`)),
		extReplies("GET /v1/api_keys", extStatus(403, `{"error":{"name":"out_of_scope","message":"This API key cannot call this API."}}`)),
	}, extReadOnly)
	ctx := context.Background()
	if _, err := fixture.client.WalletStatuses(ctx); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("WalletStatuses() 401 error = %v, want ErrAuthentication", err)
	}
	_, err := fixture.client.APIKeys(ctx)
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.Category != trade.ErrorAuthorization || apiError.ExchangeCode != "out_of_scope" ||
		apiError.HTTPStatus != 403 || apiError.Retryable {
		t.Fatalf("APIKeys() 403 error = %+v (%v), want non-retryable authorization out_of_scope", apiError, err)
	}
	extRequests(t, fixture.server, 2)
}
