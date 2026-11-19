package korbit

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

const recentKRWDepositsSample = `{"success":true,"data":[` +
	`{"id":1234,"status":"done","quantity":"50000","createdAt":1700000000000,"filledAt":1700000030000},` +
	`{"id":1235,"status":"processing","quantity":"50000","createdAt":1700000010000,"filledAt":null}` +
	`]}`

const recentKRWDepositsAllSample = `{"success":true,"data":[` +
	`{"id":1236,"type":"reward","status":"done","quantity":"1000","createdAt":1700000040000,"filledAt":1700000040000},` +
	`{"id":1237,"type":"general","status":"canceling","quantity":"3000","createdAt":1700000050000},` +
	`{"id":1238,"type":null,"status":"pending","quantity":"7000","createdAt":1700000060000}` +
	`]}`

const recentKRWWithdrawalsSample = `{"success":true,"data":[` +
	`{"id":1234,"quantity":"50000","fee":"1000","status":"done","createdAt":1700000000000},` +
	`{"id":1233,"quantity":"20000","fee":"1000","status":"reviewing","createdAt":1699990000000}` +
	`]}`

func TestKRWPushRequestsAreSignedFormPosts(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{
		"POST /v2/krw/sendKrwDepositPush":    `{"success":true}`,
		"POST /v2/krw/sendKrwWithdrawalPush": `{"success":true}`,
	})
	client := newAssetTestClient(t, server.URL, credential.PermissionTransfer, credential.PermissionWithdraw)
	ctx := context.Background()

	deposit, err := client.SendKRWDepositPush(ctx, KRWPushRequest{Amount: "50000"})
	if err != nil || !deposit.Accepted || string(deposit.Raw) != `{"success":true}` {
		t.Fatalf("SendKRWDepositPush() = %+v, error = %v", deposit, err)
	}
	withdrawal, err := client.SendKRWWithdrawalPush(ctx, KRWPushRequest{AccountSeq: 1, Amount: "12345.5"})
	if err != nil || !withdrawal.Accepted || string(withdrawal.Raw) != `{"success":true}` {
		t.Fatalf("SendKRWWithdrawalPush() = %+v, error = %v", withdrawal, err)
	}

	want := []loggedRequest{
		{http.MethodPost, "/v2/krw/sendKrwDepositPush", "amount=50000&recvWindow=5000&timestamp=1700000000123", true},
		{http.MethodPost, "/v2/krw/sendKrwWithdrawalPush", "accountSeq=1&amount=12345.5&recvWindow=5000&timestamp=1700000000123", true},
	}
	if entries := recorder.snapshot(); !slices.Equal(entries, want) {
		t.Fatalf("requests = %+v, want %+v", entries, want)
	}
}

func TestKRWPushRejectionIsNotAccepted(t *testing.T) {
	t.Parallel()

	server, hits := newFixedResponseServer(
		t, http.StatusBadRequest, `{"success":false,"error":{"code":400,"message":"BAD_REQUEST"}}`,
	)
	client := newAssetTestClient(t, server.URL, credential.PermissionTransfer, credential.PermissionWithdraw)
	ctx := context.Background()

	deposit, err := client.SendKRWDepositPush(ctx, KRWPushRequest{Amount: "50000"})
	if !errors.Is(err, trade.ErrValidation) || deposit.Accepted || len(deposit.Raw) != 0 {
		t.Fatalf("SendKRWDepositPush() = %+v, error = %v, want an unaccepted validation failure", deposit, err)
	}
	withdrawal, err := client.SendKRWWithdrawalPush(ctx, KRWPushRequest{Amount: "50000"})
	if !errors.Is(err, trade.ErrValidation) || withdrawal.Accepted || len(withdrawal.Raw) != 0 {
		t.Fatalf("SendKRWWithdrawalPush() = %+v, error = %v, want an unaccepted validation failure", withdrawal, err)
	}
	if hits.Load() != 2 {
		t.Fatalf("requests = %d, want one per call without retry", hits.Load())
	}
}

func TestRecentKRWDeposits(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{"GET /v2/krw/recentDeposits": recentKRWDepositsSample})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)

	deposits, err := client.RecentKRWDeposits(context.Background(), RecentKRWDepositsRequest{AccountSeq: 1, Limit: 100})
	if err != nil || len(deposits) != 2 {
		t.Fatalf("RecentKRWDeposits() = %+v, error = %v", deposits, err)
	}
	if deposits[0].ID != 1234 || deposits[0].Type != "" || deposits[0].Status != KRWDepositStatusDone ||
		deposits[0].Quantity != "50000" || deposits[0].CreatedAt != 1700000000000 ||
		deposits[0].FilledAt == nil || *deposits[0].FilledAt != 1700000030000 || len(deposits[0].Raw) == 0 {
		t.Fatalf("completed KRW deposit = %+v", deposits[0])
	}
	if deposits[1].Status != KRWDepositStatusProcessing || deposits[1].FilledAt != nil {
		t.Fatalf("processing KRW deposit = %+v", deposits[1])
	}
	want := []loggedRequest{
		{http.MethodGet, "/v2/krw/recentDeposits", "accountSeq=1&limit=100&recvWindow=5000&timestamp=1700000000123", true},
	}
	if entries := recorder.snapshot(); !slices.Equal(entries, want) {
		t.Fatalf("requests = %+v, want %+v", entries, want)
	}
}

func TestRecentKRWDepositsIncludeAllAddsTypes(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{"GET /v2/krw/recentDeposits": recentKRWDepositsAllSample})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)

	deposits, err := client.RecentKRWDeposits(context.Background(), RecentKRWDepositsRequest{IncludeAll: true})
	if err != nil || len(deposits) != 3 {
		t.Fatalf("RecentKRWDeposits() = %+v, error = %v", deposits, err)
	}
	if deposits[0].Type != KRWDepositTypeReward || deposits[0].Status != KRWDepositStatusDone {
		t.Fatalf("reward deposit = %+v", deposits[0])
	}
	if deposits[1].Type != KRWDepositTypeGeneral || deposits[1].Status != KRWDepositStatusCanceling || deposits[1].FilledAt != nil {
		t.Fatalf("canceling deposit = %+v", deposits[1])
	}
	if deposits[2].Type != "" || deposits[2].Status != KRWDepositStatusPending {
		t.Fatalf("deposit with null type = %+v", deposits[2])
	}
	entries := recorder.snapshot()
	if len(entries) != 1 || entries[0].Params != "includeAll=true&recvWindow=5000&timestamp=1700000000123" {
		t.Fatalf("requests = %+v, want includeAll=true without default parameters", entries)
	}
}

func TestRecentKRWWithdrawals(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{"GET /v2/krw/recentWithdrawals": recentKRWWithdrawalsSample})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)

	withdrawals, err := client.RecentKRWWithdrawals(context.Background(), RecentKRWWithdrawalsRequest{Limit: 50})
	if err != nil || len(withdrawals) != 2 {
		t.Fatalf("RecentKRWWithdrawals() = %+v, error = %v", withdrawals, err)
	}
	if withdrawals[0].ID != 1234 || withdrawals[0].Quantity != "50000" || withdrawals[0].Fee != "1000" ||
		withdrawals[0].Status != KRWWithdrawalStatusDone || withdrawals[0].CreatedAt != 1700000000000 ||
		len(withdrawals[0].Raw) == 0 || withdrawals[1].Status != KRWWithdrawalStatusReviewing {
		t.Fatalf("KRW withdrawals = %+v", withdrawals)
	}
	want := []loggedRequest{
		{http.MethodGet, "/v2/krw/recentWithdrawals", "limit=50&recvWindow=5000&timestamp=1700000000123", true},
	}
	if entries := recorder.snapshot(); !slices.Equal(entries, want) {
		t.Fatalf("requests = %+v, want %+v", entries, want)
	}
}

func TestKRWRejectsInvalidRequestBeforeSending(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{"GET /v2/krw/recentDeposits": recentKRWDepositsSample})
	client := newAssetTestClient(t, server.URL, allTestPermissions...)
	ctx := context.Background()

	pushes := map[string]KRWPushRequest{
		"push without amount":         {},
		"push of zero":                {Amount: "0"},
		"push of negative amount":     {Amount: "-1000"},
		"push with exponent amount":   {Amount: "5e4"},
		"push with spaced amount":     {Amount: "50 000"},
		"push from a sub account":     {AccountSeq: 2, Amount: "50000"},
		"push from a negative number": {AccountSeq: -1, Amount: "50000"},
	}
	calls := make([]namedCall, 0, len(pushes)*2+5)
	for name, request := range pushes {
		calls = append(calls,
			namedCall{"deposit " + name, func() error {
				_, err := client.SendKRWDepositPush(ctx, request)
				return err
			}},
			namedCall{"withdrawal " + name, func() error {
				_, err := client.SendKRWWithdrawalPush(ctx, request)
				return err
			}},
		)
	}
	calls = append(calls,
		namedCall{"recent deposits on a sub account", func() error {
			_, err := client.RecentKRWDeposits(ctx, RecentKRWDepositsRequest{AccountSeq: 2})
			return err
		}},
		namedCall{"recent deposits over the limit", func() error {
			_, err := client.RecentKRWDeposits(ctx, RecentKRWDepositsRequest{Limit: 101})
			return err
		}},
		namedCall{"recent deposits with negative limit", func() error {
			_, err := client.RecentKRWDeposits(ctx, RecentKRWDepositsRequest{Limit: -1})
			return err
		}},
		namedCall{"recent withdrawals on a sub account", func() error {
			_, err := client.RecentKRWWithdrawals(ctx, RecentKRWWithdrawalsRequest{AccountSeq: 3})
			return err
		}},
		namedCall{"recent withdrawals over the limit", func() error {
			_, err := client.RecentKRWWithdrawals(ctx, RecentKRWWithdrawalsRequest{Limit: 101})
			return err
		}},
	)
	requireRejectedBeforeSend(t, recorder, calls)
}
