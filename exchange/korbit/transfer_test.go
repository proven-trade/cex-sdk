package korbit

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

const transferRequestKey = "b6f1a9a2-4c3e-4a7d-9d3c-2f8f5e1a7b10"

const accountTransferReceiptSample = `{"success":true,"data":{"id":1234,"requestKey":"` + transferRequestKey + `","status":"pending"}}`

const accountTransferSample = `{"success":true,"data":{"id":1234,"requestKey":"` + transferRequestKey + `","status":"done","currency":"btc","fromAccountSeq":1,"toAccountSeq":3,"qty":"0.50000000","source":"api","createdAt":1700000000000,"updatedAt":1700000003000}}`

const recentAccountTransfersSample = `{"success":true,"data":[` +
	`{"id":1235,"requestKey":null,"status":"pending","currency":"krw","fromAccountSeq":1,"toAccountSeq":2,"qty":"150000","source":"app","createdAt":1700000010000,"updatedAt":0},` +
	`{"id":1234,"requestKey":"` + transferRequestKey + `","status":"done","currency":"btc","fromAccountSeq":1,"toAccountSeq":3,"qty":"0.50000000","source":"api","createdAt":1700000000000,"updatedAt":1700000003000}` +
	`]}`

func TestAccountTransferEndpoints(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{
		"POST /v2/accountTransfer":       accountTransferReceiptSample,
		"GET /v2/accountTransfer":        accountTransferSample,
		"GET /v2/recentAccountTransfers": recentAccountTransfersSample,
	})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead, credential.PermissionTransfer)
	ctx := context.Background()

	receipt, err := client.CreateAccountTransfer(ctx, CreateAccountTransferRequest{
		AccountSeq: 1, ToAccountSeq: 3, Currency: "btc", Qty: "0.5", RequestKey: transferRequestKey,
	})
	if err != nil || receipt.ID != 1234 || receipt.RequestKey != transferRequestKey ||
		receipt.Status != AccountTransferStatusPending || len(receipt.Raw) == 0 {
		t.Fatalf("CreateAccountTransfer() = %+v, error = %v", receipt, err)
	}

	byID, err := client.AccountTransfer(ctx, AccountTransferRequest{AccountSeq: 1, ID: 1234})
	if err != nil {
		t.Fatalf("AccountTransfer(id) error = %v", err)
	}
	if byID.ID != 1234 || byID.RequestKey != transferRequestKey || byID.Status != AccountTransferStatusDone ||
		byID.Currency != "btc" || byID.FromAccountSeq != 1 || byID.ToAccountSeq != 3 || byID.Qty != "0.50000000" ||
		byID.Source != AccountTransferSourceAPI || byID.CreatedAt != 1700000000000 || byID.UpdatedAt != 1700000003000 ||
		len(byID.Raw) == 0 {
		t.Fatalf("AccountTransfer(id) = %+v", byID)
	}
	byKey, err := client.AccountTransfer(ctx, AccountTransferRequest{AccountSeq: 3, RequestKey: transferRequestKey})
	if err != nil || byKey.ID != 1234 {
		t.Fatalf("AccountTransfer(requestKey) = %+v, error = %v", byKey, err)
	}

	recent, err := client.RecentAccountTransfers(ctx, RecentAccountTransfersRequest{
		AccountSeq: 1, Direction: AccountTransferDirectionOut, Limit: 100,
	})
	if err != nil || len(recent) != 2 {
		t.Fatalf("RecentAccountTransfers() = %+v, error = %v", recent, err)
	}
	if recent[0].ID != 1235 || recent[0].RequestKey != "" || recent[0].Status != AccountTransferStatusPending ||
		recent[0].Currency != "krw" || recent[0].Qty != "150000" || recent[0].Source != AccountTransferSourceApp ||
		recent[0].UpdatedAt != 0 || len(recent[0].Raw) == 0 {
		t.Fatalf("pending app transfer = %+v", recent[0])
	}
	if recent[1].RequestKey != transferRequestKey || recent[1].Source != AccountTransferSourceAPI ||
		recent[1].Status != AccountTransferStatusDone {
		t.Fatalf("settled api transfer = %+v", recent[1])
	}

	want := []loggedRequest{
		{
			http.MethodPost, "/v2/accountTransfer",
			"accountSeq=1&currency=btc&qty=0.5&recvWindow=5000&requestKey=" + transferRequestKey + "&timestamp=1700000000123&toAccountSeq=3",
			true,
		},
		{http.MethodGet, "/v2/accountTransfer", "accountSeq=1&id=1234&recvWindow=5000&timestamp=1700000000123", true},
		{
			http.MethodGet, "/v2/accountTransfer",
			"accountSeq=3&recvWindow=5000&requestKey=" + transferRequestKey + "&timestamp=1700000000123",
			true,
		},
		{
			http.MethodGet, "/v2/recentAccountTransfers",
			"accountSeq=1&direction=out&limit=100&recvWindow=5000&timestamp=1700000000123",
			true,
		},
	}
	if entries := recorder.snapshot(); !slices.Equal(entries, want) {
		t.Fatalf("requests = %+v, want %+v", entries, want)
	}
}

func TestAccountTransferKeepsUnknownStatusAsNotFinal(t *testing.T) {
	t.Parallel()

	server, _ := newKorbitStubServer(t, map[string]string{
		"GET /v2/accountTransfer": `{"success":true,"data":{"id":9,"requestKey":null,"status":"reversing","currency":"krw","fromAccountSeq":2,"toAccountSeq":1,"qty":"1000","source":"app","createdAt":1700000000000,"updatedAt":0}}`,
	})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)
	transfer, err := client.AccountTransfer(context.Background(), AccountTransferRequest{AccountSeq: 2, ID: 9})
	if err != nil || transfer.Status != "reversing" || transfer.RequestKey != "" || transfer.UpdatedAt != 0 {
		t.Fatalf("AccountTransfer() = %+v, error = %v, want the unrecognized status preserved", transfer, err)
	}
}

func TestCreateAccountTransferReportsExchangeRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		is   error
	}{
		{"insufficient balance", "NO_BALANCE", trade.ErrInsufficientBalance},
		{"unknown destination account", "ACCOUNT_SEQ_NOT_FOUND", trade.ErrValidation},
		{"source account not allowed for the key", "ACCOUNT_SEQ_NOT_ALLOWED", trade.ErrAuthorization},
		{"invalid currency", "INVALID_CURRENCY", trade.ErrValidation},
		{"request key already claimed", "DUPLICATE_REQUEST_KEY", trade.ErrValidation},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server, hits := newFixedResponseServer(
				t, http.StatusBadRequest, `{"success":false,"error":{"code":400,"message":"`+test.code+`"}}`,
			)
			client := newAssetTestClient(t, server.URL, credential.PermissionTransfer)
			receipt, err := client.CreateAccountTransfer(context.Background(), CreateAccountTransferRequest{
				AccountSeq: 1, ToAccountSeq: 3, Currency: "btc", Qty: "0.5", RequestKey: transferRequestKey,
			})
			var apiError *trade.APIError
			if !errors.Is(err, test.is) || !errors.As(err, &apiError) || apiError.ExchangeCode != test.code ||
				receipt.ID != 0 || hits.Load() != 1 {
				t.Fatalf(
					"CreateAccountTransfer() = %+v, error = %v after %d requests, want %v with exchange code %q",
					receipt, err, hits.Load(), test.is, test.code,
				)
			}
		})
	}
}

func TestAccountTransferRejectsInvalidRequestBeforeSending(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{"POST /v2/accountTransfer": accountTransferReceiptSample})
	client := newAssetTestClient(t, server.URL, allTestPermissions...)
	ctx := context.Background()

	valid := CreateAccountTransferRequest{
		AccountSeq: 1, ToAccountSeq: 3, Currency: "btc", Qty: "0.5", RequestKey: transferRequestKey,
	}
	create := func(change func(*CreateAccountTransferRequest)) func() error {
		return func() error {
			request := valid
			change(&request)
			_, err := client.CreateAccountTransfer(ctx, request)
			return err
		}
	}
	lookup := func(request AccountTransferRequest) func() error {
		return func() error {
			_, err := client.AccountTransfer(ctx, request)
			return err
		}
	}
	recent := func(request RecentAccountTransfersRequest) func() error {
		return func() error {
			_, err := client.RecentAccountTransfers(ctx, request)
			return err
		}
	}
	requireRejectedBeforeSend(t, recorder, []namedCall{
		{"create without source account", create(func(request *CreateAccountTransferRequest) { request.AccountSeq = 0 })},
		{"create with oversized source account", create(func(request *CreateAccountTransferRequest) { request.AccountSeq = 10000 })},
		{"create without destination account", create(func(request *CreateAccountTransferRequest) { request.ToAccountSeq = 0 })},
		{"create to the same account", create(func(request *CreateAccountTransferRequest) { request.ToAccountSeq = 1 })},
		{"create with upper case currency", create(func(request *CreateAccountTransferRequest) { request.Currency = "BTC" })},
		{"create without currency", create(func(request *CreateAccountTransferRequest) { request.Currency = "" })},
		{"create without quantity", create(func(request *CreateAccountTransferRequest) { request.Qty = "" })},
		{"create of zero", create(func(request *CreateAccountTransferRequest) { request.Qty = "0.00000000" })},
		{"create with negative quantity", create(func(request *CreateAccountTransferRequest) { request.Qty = "-1" })},
		{"create with exponent quantity", create(func(request *CreateAccountTransferRequest) { request.Qty = "1e5" })},
		{"create with nine decimals", create(func(request *CreateAccountTransferRequest) { request.Qty = "0.123456789" })},
		{"create without request key", create(func(request *CreateAccountTransferRequest) { request.RequestKey = "" })},
		{"create with spaced request key", create(func(request *CreateAccountTransferRequest) { request.RequestKey = "a b" })},
		{"create with long request key", create(func(request *CreateAccountTransferRequest) {
			request.RequestKey = strings.Repeat("k", 37)
		})},
		{"lookup without source account", lookup(AccountTransferRequest{ID: 1})},
		{"lookup with neither identity", lookup(AccountTransferRequest{AccountSeq: 1})},
		{"lookup with both identities", lookup(AccountTransferRequest{AccountSeq: 1, ID: 1, RequestKey: transferRequestKey})},
		{"lookup with negative ID", lookup(AccountTransferRequest{AccountSeq: 1, ID: -1})},
		{"lookup with invalid request key", lookup(AccountTransferRequest{AccountSeq: 1, RequestKey: "bad key!"})},
		{"recent without account", recent(RecentAccountTransfersRequest{Direction: AccountTransferDirectionIn, Limit: 10})},
		{"recent without direction", recent(RecentAccountTransfersRequest{AccountSeq: 1, Limit: 10})},
		{"recent with unknown direction", recent(RecentAccountTransfersRequest{AccountSeq: 1, Direction: "both", Limit: 10})},
		{"recent without limit", recent(RecentAccountTransfersRequest{AccountSeq: 1, Direction: AccountTransferDirectionOut})},
		{"recent over the limit", recent(RecentAccountTransfersRequest{AccountSeq: 1, Direction: AccountTransferDirectionOut, Limit: 101})},
	})
}

func TestAccountTransferResponsesNeedIdentity(t *testing.T) {
	t.Parallel()

	server, _ := newKorbitStubServer(t, map[string]string{
		"GET /v2/accountTransfer": `{"success":true,"data":{"status":"done","currency":"btc"}}`,
	})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)
	if _, err := client.AccountTransfer(context.Background(), AccountTransferRequest{AccountSeq: 1, ID: 1}); err == nil {
		t.Fatal("AccountTransfer() accepted a response without id")
	}
}
