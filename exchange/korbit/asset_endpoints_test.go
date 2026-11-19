package korbit

import (
	"context"
	"errors"
	"net/http"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// assetEndpointCase는 새로 추가한 private endpoint 하나의 계약이다.
// 호출은 항상 검증을 통과하는 최소 요청을 보내므로 SDK 권한·요청 제한 그룹·HTTP 경로만 달라진다.
type assetEndpointCase struct {
	name       string
	method     string
	path       string
	permission credential.Permission
	group      rateGroup
	body       string
	call       func(context.Context, *Client) error
}

func newAssetEndpointCase(
	name, method, path string,
	permission credential.Permission,
	group rateGroup,
	body string,
	call func(context.Context, *Client) error,
) assetEndpointCase {
	return assetEndpointCase{
		name: name, method: method, path: path, permission: permission, group: group, body: body, call: call,
	}
}

// isMutation은 조회 권한이 아닌 SDK 권한을 요구하는 endpoint를 변경 요청으로 취급한다.
func (endpoint assetEndpointCase) isMutation() bool {
	return endpoint.permission != credential.PermissionRead
}

func assetEndpointCases() []assetEndpointCase {
	return []assetEndpointCase{
		newAssetEndpointCase(
			"MyPreventedMatches", http.MethodGet, "/v2/myPreventedMatches",
			credential.PermissionRead, rateGroupPrivate,
			`{"success":true,"data":{"preventedMatches":[],"nextCursor":null}}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.MyPreventedMatches(ctx, PreventedMatchesRequest{Symbol: "btc_krw", AccountSeq: 1})
				return err
			},
		),
		newAssetEndpointCase(
			"TradingFeePolicy", http.MethodGet, "/v2/tradingFeePolicy",
			credential.PermissionRead, rateGroupPrivate,
			`{"success":true,"data":[]}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.TradingFeePolicy(ctx, TradingFeePolicyRequest{})
				return err
			},
		),
		newAssetEndpointCase(
			"CurrentKeyInfo", http.MethodGet, "/v2/currentKeyInfo",
			credential.PermissionRead, rateGroupPrivate,
			`{"success":true,"data":{"apiKey":"key-1","type":"hmac-sha256","permissions":["readOrders"],"whitelist":"","expiration":0,"status":"activated","allowedAccountSeqs":[1],"createdAt":1700000000000}}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.CurrentKeyInfo(ctx)
				return err
			},
		),
		newAssetEndpointCase(
			"Accounts", http.MethodGet, "/v2/accounts",
			credential.PermissionRead, rateGroupPrivate,
			`{"success":true,"data":[{"accountSeq":1}]}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.Accounts(ctx)
				return err
			},
		),
		newAssetEndpointCase(
			"CoinDepositAddresses", http.MethodGet, "/v2/coin/depositAddresses",
			credential.PermissionRead, rateGroupAsset,
			`{"success":true,"data":[]}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.CoinDepositAddresses(ctx, CoinDepositAddressesRequest{})
				return err
			},
		),
		newAssetEndpointCase(
			"CoinDepositAddress", http.MethodGet, "/v2/coin/depositAddress",
			credential.PermissionRead, rateGroupAsset,
			`{"success":true,"data":{"currency":"btc","network":"BTC","address":"addr-1"}}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.CoinDepositAddress(ctx, CoinDepositAddressRequest{Currency: "btc", Network: "BTC"})
				return err
			},
		),
		newAssetEndpointCase(
			"CreateCoinDepositAddress", http.MethodPost, "/v2/coin/depositAddress",
			credential.PermissionTransfer, rateGroupAsset,
			`{"success":true,"data":{"currency":"btc","network":"BTC","address":"addr-1"}}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.CreateCoinDepositAddress(
					ctx, CreateCoinDepositAddressRequest{Currency: "btc", Network: "BTC"},
				)
				return err
			},
		),
		newAssetEndpointCase(
			"RecentCoinDeposits", http.MethodGet, "/v2/coin/recentDeposits",
			credential.PermissionRead, rateGroupAsset,
			`{"success":true,"data":[]}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.RecentCoinDeposits(ctx, RecentCoinDepositsRequest{Currency: "btc"})
				return err
			},
		),
		newAssetEndpointCase(
			"CoinDeposit", http.MethodGet, "/v2/coin/deposit",
			credential.PermissionRead, rateGroupAsset,
			`{"success":true,"data":{"id":1234,"network":"BTC","address":"addr-1","secondaryAddress":null,"status":"done","transactionHash":"hash-1","currency":"btc","quantity":"1.234","createdAt":1700000000000,"filledAt":1700000030000}}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.CoinDeposit(ctx, CoinDepositRequest{Currency: "btc", CoinDepositID: 1234})
				return err
			},
		),
		newAssetEndpointCase(
			"WithdrawableAddresses", http.MethodGet, "/v2/coin/withdrawableAddresses",
			credential.PermissionRead, rateGroupAsset,
			`{"success":true,"data":[]}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.WithdrawableAddresses(ctx, WithdrawableAddressesRequest{})
				return err
			},
		),
		newAssetEndpointCase(
			"WithdrawableAmounts", http.MethodGet, "/v2/coin/withdrawableAmount",
			credential.PermissionRead, rateGroupAsset,
			`{"success":true,"data":[]}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.WithdrawableAmounts(ctx, WithdrawableAmountsRequest{})
				return err
			},
		),
		newAssetEndpointCase(
			"CreateCoinWithdrawal", http.MethodPost, "/v2/coin/withdrawal",
			credential.PermissionWithdraw, rateGroupAsset,
			`{"success":true,"data":{"status":"pending","coinWithdrawalId":1234}}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.CreateCoinWithdrawal(ctx, CreateCoinWithdrawalRequest{
					Currency: "btc", Network: "BTC", Amount: "0.02521236", Address: "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",
				})
				return err
			},
		),
		newAssetEndpointCase(
			"CancelCoinWithdrawal", http.MethodDelete, "/v2/coin/withdrawal",
			credential.PermissionWithdraw, rateGroupAsset,
			`{"success":true}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.CancelCoinWithdrawal(ctx, CancelCoinWithdrawalRequest{CoinWithdrawalID: 1234})
				return err
			},
		),
		newAssetEndpointCase(
			"CoinWithdrawal", http.MethodGet, "/v2/coin/withdrawal",
			credential.PermissionRead, rateGroupAsset,
			`{"success":true,"data":{"id":1234,"quantity":"1.234","fee":"0.0001","currency":"btc","status":"done","network":"BTC","address":"addr-1","secondaryAddress":null,"transactionHash":null,"createdAt":1700000000000}}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.CoinWithdrawal(ctx, CoinWithdrawalRequest{Currency: "btc", CoinWithdrawalID: 1234})
				return err
			},
		),
		newAssetEndpointCase(
			"RecentCoinWithdrawals", http.MethodGet, "/v2/coin/recentWithdrawals",
			credential.PermissionRead, rateGroupAsset,
			`{"success":true,"data":[]}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.RecentCoinWithdrawals(ctx, RecentCoinWithdrawalsRequest{Currency: "btc"})
				return err
			},
		),
		newAssetEndpointCase(
			"SendKRWDepositPush", http.MethodPost, "/v2/krw/sendKrwDepositPush",
			credential.PermissionTransfer, rateGroupAsset,
			`{"success":true}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.SendKRWDepositPush(ctx, KRWPushRequest{Amount: "50000"})
				return err
			},
		),
		newAssetEndpointCase(
			"SendKRWWithdrawalPush", http.MethodPost, "/v2/krw/sendKrwWithdrawalPush",
			credential.PermissionWithdraw, rateGroupAsset,
			`{"success":true}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.SendKRWWithdrawalPush(ctx, KRWPushRequest{Amount: "50000"})
				return err
			},
		),
		newAssetEndpointCase(
			"RecentKRWDeposits", http.MethodGet, "/v2/krw/recentDeposits",
			credential.PermissionRead, rateGroupAsset,
			`{"success":true,"data":[]}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.RecentKRWDeposits(ctx, RecentKRWDepositsRequest{})
				return err
			},
		),
		newAssetEndpointCase(
			"RecentKRWWithdrawals", http.MethodGet, "/v2/krw/recentWithdrawals",
			credential.PermissionRead, rateGroupAsset,
			`{"success":true,"data":[]}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.RecentKRWWithdrawals(ctx, RecentKRWWithdrawalsRequest{})
				return err
			},
		),
		newAssetEndpointCase(
			"CreateAccountTransfer", http.MethodPost, "/v2/accountTransfer",
			credential.PermissionTransfer, rateGroupAsset,
			`{"success":true,"data":{"id":1234,"requestKey":"transfer-1","status":"pending"}}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.CreateAccountTransfer(ctx, CreateAccountTransferRequest{
					AccountSeq: 1, ToAccountSeq: 3, Currency: "btc", Qty: "0.5", RequestKey: "transfer-1",
				})
				return err
			},
		),
		newAssetEndpointCase(
			"AccountTransfer", http.MethodGet, "/v2/accountTransfer",
			credential.PermissionRead, rateGroupAsset,
			`{"success":true,"data":{"id":1234,"requestKey":"transfer-1","status":"done","currency":"btc","fromAccountSeq":1,"toAccountSeq":3,"qty":"0.50000000","source":"api","createdAt":1700000000000,"updatedAt":1700000003000}}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.AccountTransfer(ctx, AccountTransferRequest{AccountSeq: 1, ID: 1234})
				return err
			},
		),
		newAssetEndpointCase(
			"RecentAccountTransfers", http.MethodGet, "/v2/recentAccountTransfers",
			credential.PermissionRead, rateGroupAsset,
			`{"success":true,"data":[]}`,
			func(ctx context.Context, client *Client) error {
				_, err := client.RecentAccountTransfers(ctx, RecentAccountTransfersRequest{
					AccountSeq: 1, Direction: AccountTransferDirectionOut, Limit: 100,
				})
				return err
			},
		),
	}
}

func TestAssetEndpointsRouteAndChargeExpectedRateGroup(t *testing.T) {
	t.Parallel()

	for _, endpoint := range assetEndpointCases() {
		t.Run(endpoint.name, func(t *testing.T) {
			t.Parallel()

			server, recorder := newKorbitStubServer(t, map[string]string{endpoint.method + " " + endpoint.path: endpoint.body})
			client, limiter := newExtTestClient(
				t, server.URL, &directSender{}, &recordingProvider{},
				[]credential.Permission{endpoint.permission}, nil,
			)
			if err := endpoint.call(context.Background(), client); err != nil {
				t.Fatalf("%s error = %v", endpoint.name, err)
			}
			entries := recorder.snapshot()
			if len(entries) != 1 || entries[0].Method != endpoint.method || entries[0].Path != endpoint.path || !entries[0].Signed {
				t.Fatalf("%s requests = %+v", endpoint.name, entries)
			}
			wantLimit := DefaultPrivateRequestsPerSecond
			if endpoint.group == rateGroupAsset {
				wantLimit = DefaultAssetRequestsPerSecond
			}
			snapshot, err := limiter.Snapshot("korbit:account:korbit-account:" + string(endpoint.group) + ":1second")
			if err != nil || snapshot.Used != 1 || snapshot.Rule.Limit != wantLimit {
				t.Fatalf("%s limiter snapshot = %+v, error = %v, want limit %d", endpoint.name, snapshot, err, wantLimit)
			}
		})
	}
}

func TestAssetEndpointsRequireTheirSDKPermission(t *testing.T) {
	t.Parallel()

	for _, endpoint := range assetEndpointCases() {
		t.Run(endpoint.name, func(t *testing.T) {
			t.Parallel()

			server, recorder := newKorbitStubServer(t, map[string]string{endpoint.method + " " + endpoint.path: endpoint.body})
			sender := &directSender{}
			provider := &recordingProvider{}
			client, _ := newExtTestClient(t, server.URL, sender, provider, permissionsExcept(endpoint.permission), nil)
			err := endpoint.call(context.Background(), client)
			if !errors.Is(err, credential.ErrPermissionNotAllowed) || !errors.Is(err, trade.ErrAuthorization) {
				t.Fatalf("%s error = %v, want permission denial for %q", endpoint.name, err, endpoint.permission)
			}
			calls, _, _ := provider.snapshot()
			if calls != 0 || len(sender.snapshot()) != 0 || len(recorder.snapshot()) != 0 {
				t.Fatalf("%s resolved secrets or sent a request without permission", endpoint.name)
			}
		})
	}
}

func TestAssetMutationsNeverRetryAndStayUnknown(t *testing.T) {
	t.Parallel()

	identityMissing := map[string]bool{
		"CreateCoinDepositAddress": true, "CreateCoinWithdrawal": true, "CreateAccountTransfer": true,
	}
	for _, endpoint := range assetEndpointCases() {
		if !endpoint.isMutation() {
			continue
		}
		permissions := []credential.Permission{endpoint.permission}
		t.Run(endpoint.name+" network failure", func(t *testing.T) {
			t.Parallel()

			client, _ := newExtTestClient(t, "http://127.0.0.1", errorSender{}, &recordingProvider{}, permissions, nil)
			if err := endpoint.call(context.Background(), client); !errors.Is(err, trade.ErrUnknownExecutionState) {
				t.Fatalf("%s network failure error = %v, want ErrUnknownExecutionState", endpoint.name, err)
			}
		})
		t.Run(endpoint.name+" server error", func(t *testing.T) {
			t.Parallel()

			server, hits := newFixedResponseServer(
				t, http.StatusServiceUnavailable, `{"success":false,"error":{"code":503,"message":"SERVICE_UNAVAILABLE"}}`,
			)
			client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, permissions, nil)
			err := endpoint.call(context.Background(), client)
			if !errors.Is(err, trade.ErrUnknownExecutionState) || hits.Load() != 1 {
				t.Fatalf("%s server error = %v after %d requests, want one unknown-state request", endpoint.name, err, hits.Load())
			}
		})
		t.Run(endpoint.name+" malformed success", func(t *testing.T) {
			t.Parallel()

			server, hits := newFixedResponseServer(t, http.StatusOK, `not-json`)
			client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, permissions, nil)
			err := endpoint.call(context.Background(), client)
			if !errors.Is(err, trade.ErrUnknownExecutionState) || hits.Load() != 1 {
				t.Fatalf("%s malformed success = %v after %d requests, want one unknown-state request", endpoint.name, err, hits.Load())
			}
		})
		if identityMissing[endpoint.name] {
			t.Run(endpoint.name+" missing identity", func(t *testing.T) {
				t.Parallel()

				server, _ := newFixedResponseServer(t, http.StatusOK, `{"success":true,"data":{}}`)
				client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, permissions, nil)
				if err := endpoint.call(context.Background(), client); !errors.Is(err, trade.ErrUnknownExecutionState) {
					t.Fatalf("%s empty success error = %v, want ErrUnknownExecutionState", endpoint.name, err)
				}
			})
		}
	}
}

func TestAssetErrorClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status    int
		code      string
		operation commonexchange.OperationKind
		category  trade.ErrorCategory
		retryable bool
	}{
		{404, "NOT_FOUND", commonexchange.OperationRead, trade.ErrorOrderNotFound, false},
		{400, "NOT_FOUND", commonexchange.OperationMutation, trade.ErrorOrderNotFound, false},
		{400, "ACCOUNT_SEQ_NOT_ALLOWED", commonexchange.OperationRead, trade.ErrorAuthorization, false},
		{400, "ACCOUNT_SEQ_NOT_FOUND", commonexchange.OperationMutation, trade.ErrorValidation, false},
		{400, "DUPLICATE_REQUEST_KEY", commonexchange.OperationMutation, trade.ErrorValidation, false},
		{400, "UNREGISTERED_WITHDRAWAL_ADDRESS", commonexchange.OperationMutation, trade.ErrorValidation, false},
		{400, "NO_BALANCE", commonexchange.OperationMutation, trade.ErrorInsufficientBalance, false},
		{400, "INVALID_USER_STATUS", commonexchange.OperationMutation, trade.ErrorAuthorization, false},
		{503, "SERVICE_UNAVAILABLE", commonexchange.OperationMutation, trade.ErrorUnknownExecutionState, false},
		{503, "SERVICE_UNAVAILABLE", commonexchange.OperationRead, trade.ErrorExchangeUnavailable, true},
	}
	for _, test := range tests {
		category, retryable := classifyError(test.status, test.code, test.operation)
		if category != test.category || retryable != test.retryable {
			t.Errorf(
				"classifyError(%d, %q, %d) = (%s, %v), want (%s, %v)",
				test.status, test.code, test.operation, category, retryable, test.category, test.retryable,
			)
		}
	}
}

func TestAssetErrorsKeepExchangeCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		code string
		is   error
		call func(context.Context, *Client) error
	}{
		{
			"transfer lookup miss", `{"success":false,"error":{"code":404,"message":"NOT_FOUND"}}`, "NOT_FOUND",
			trade.ErrOrderNotFound,
			func(ctx context.Context, client *Client) error {
				_, err := client.AccountTransfer(ctx, AccountTransferRequest{AccountSeq: 1, RequestKey: "transfer-1"})
				return err
			},
		},
		{
			"duplicate request key", `{"success":false,"error":{"code":400,"message":"DUPLICATE_REQUEST_KEY"}}`, "DUPLICATE_REQUEST_KEY",
			trade.ErrValidation,
			func(ctx context.Context, client *Client) error {
				_, err := client.CreateAccountTransfer(ctx, CreateAccountTransferRequest{
					AccountSeq: 1, ToAccountSeq: 3, Currency: "btc", Qty: "0.5", RequestKey: "transfer-1",
				})
				return err
			},
		},
		{
			"account not allowed", `{"success":false,"error":{"code":403,"message":"ACCOUNT_SEQ_NOT_ALLOWED"}}`, "ACCOUNT_SEQ_NOT_ALLOWED",
			trade.ErrAuthorization,
			func(ctx context.Context, client *Client) error {
				_, err := client.MyPreventedMatches(ctx, PreventedMatchesRequest{Symbol: "btc_krw", AccountSeq: 2})
				return err
			},
		},
		{
			"withdrawal address not registered", `{"success":false,"error":{"code":400,"message":"UNREGISTERED_WITHDRAWAL_ADDRESS"}}`,
			"UNREGISTERED_WITHDRAWAL_ADDRESS", trade.ErrValidation,
			func(ctx context.Context, client *Client) error {
				_, err := client.CreateCoinWithdrawal(ctx, CreateCoinWithdrawalRequest{
					Currency: "btc", Network: "BTC", Amount: "0.02521236", Address: "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",
				})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			status := http.StatusBadRequest
			if test.is == trade.ErrOrderNotFound {
				status = http.StatusNotFound
			}
			if test.is == trade.ErrAuthorization {
				status = http.StatusForbidden
			}
			server, _ := newFixedResponseServer(t, status, test.body)
			client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, allTestPermissions, nil)
			err := test.call(context.Background(), client)
			var apiError *trade.APIError
			if !errors.Is(err, test.is) || !errors.As(err, &apiError) || apiError.ExchangeCode != test.code {
				t.Fatalf("error = %v, want %v with exchange code %q", err, test.is, test.code)
			}
		})
	}
}
