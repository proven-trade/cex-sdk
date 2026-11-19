package bithumb

import (
	"context"
	"errors"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

const depositListSample = `[{"type":"deposit","uuid":"202620152","currency":"SHIB","net_type":null,"txid":"20240709163030.72335",` +
	`"state":"DEPOSIT_ACCEPTED","created_at":"2024-07-09T16:31:08+09:00","done_at":"2024-07-09T16:31:08+09:00",` +
	`"amount":"100000","fee":"0","transaction_type":null},` +
	`{"type":"deposit","uuid":"202611907","currency":"SANTOS","net_type":null,"txid":"20240701005602.90593",` +
	`"state":"DEPOSIT_ACCEPTED","created_at":"2024-07-01T00:56:23+09:00","done_at":"2024-07-01T00:56:23+09:00",` +
	`"amount":"1000000","fee":"0","transaction_type":null}]`

const krwDepositListSample = `[{"type":"deposit","uuid":"15371593","currency":"KRW","txid":"1596126","state":"ACCEPTED",` +
	`"created_at":"2024-07-06T15:14:37+09:00","done_at":"2024-07-08T15:19:40+09:00","amount":"20000","fee":"0",` +
	`"transaction_type":"default"}]`

const depositInfoSample = `{"type":"deposit","uuid":"200012694","currency":"BTC","net_type":"BTC","txid":"20200313145947.40057",` +
	`"state":"DEPOSIT_ACCEPTED","created_at":"2020-03-13T15:00:35+09:00","done_at":"2020-03-13T15:00:35+09:00",` +
	`"amount":"100","fee":"0","transaction_type":null}`

const depositAddressesSample = `[{"currency":"BTC","net_type":"BTC","deposit_address":"13qdmpBMQVnwF8UGCARw7mfrP6rernoGUW","secondary_address":null},` +
	`{"currency":"ETH","net_type":"ETH","deposit_address":"0x059be8ad9620c1aefd50c30545cb60ab984a3676","secondary_address":null},` +
	`{"currency":"XRP","net_type":"XRP","deposit_address":"rs6qtDrs8qp5qLkUxuzQdxFh5eGrQbL3Ee","secondary_address":"1004775304"}]`

const depositAddressSample = `{"currency":"XRP","net_type":"XRP","deposit_address":"rs6qtDrs8qp5qLkUxuzQdxFh5eGrQbL3Ee",` +
	`"secondary_address":"1004775304"}`

const generatedDepositAddressSample = `{"currency":"KLAY","net_type":"KLAY","deposit_address":"0x147385478caae8d76f8796fdc78e7e2a07b87362",` +
	`"secondary_address":null}`

const depositKRWSample = `{"type":"deposit","uuid":"15373074","currency":"KRW","net_type":null,"txid":"1597453",` +
	`"state":"PROCESSING","created_at":"2024-07-14T17:09:46+09:00","done_at":null,"amount":"5000","fee":"0",` +
	`"transaction_type":"default"}`

func TestDepositListsAndAddresses(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{
		extOK("GET /v1/deposits", depositListSample),
		extOK("GET /v1/deposits/krw", krwDepositListSample),
		extOK("GET /v1/deposit", depositInfoSample),
		extOK("GET /v1/deposits/coin_addresses", depositAddressesSample),
		extOK("GET /v1/deposits/coin_address", depositAddressSample),
	}, extReadOnly)
	ctx := context.Background()

	list, err := fixture.client.Deposits(ctx, DepositsRequest{
		Currency: "SHIB", State: DepositStateDepositAccepted, UUIDs: []string{"202620152"},
		Page: 1, Limit: 20, OrderBy: "desc",
	})
	if err != nil || len(list) != 2 || len(list[0].Raw) == 0 {
		t.Fatalf("Deposits() = %+v, error = %v", list, err)
	}
	first := list[0]
	if first.Type != "deposit" || first.UUID != "202620152" || first.Currency != "SHIB" || first.NetType != "" ||
		first.TxID != "20240709163030.72335" || first.State != DepositStateDepositAccepted ||
		first.CreatedAt != "2024-07-09T16:31:08+09:00" || first.DoneAt != "2024-07-09T16:31:08+09:00" ||
		first.Amount != "100000" || first.Fee != "0" || first.TransactionType != "" {
		t.Fatalf("first deposit = %+v", first)
	}

	krwList, err := fixture.client.KRWDeposits(ctx, KRWDepositsRequest{
		State: DepositStateKRWAccepted, TxIDs: []string{"1596126"},
	})
	if err != nil || len(krwList) != 1 || krwList[0].Currency != "KRW" || krwList[0].State != DepositStateKRWAccepted ||
		krwList[0].Amount != "20000" || krwList[0].TransactionType != "default" || krwList[0].NetType != "" {
		t.Fatalf("KRWDeposits() = %+v, error = %v", krwList, err)
	}

	byUUID, err := fixture.client.DepositInfo(ctx, DepositInfoRequest{Currency: "BTC", UUID: "200012694"})
	if err != nil || byUUID.UUID != "200012694" || byUUID.NetType != "BTC" || byUUID.Amount != "100" ||
		byUUID.State != DepositStateDepositAccepted || len(byUUID.Raw) == 0 {
		t.Fatalf("DepositInfo(uuid) = %+v, error = %v", byUUID, err)
	}
	byTxID, err := fixture.client.DepositInfo(ctx, DepositInfoRequest{Currency: "BTC", TxID: "20200313145947.40057"})
	if err != nil || byTxID.TxID != "20200313145947.40057" {
		t.Fatalf("DepositInfo(txid) = %+v, error = %v", byTxID, err)
	}

	addresses, err := fixture.client.DepositAddresses(ctx)
	if err != nil || len(addresses) != 3 || len(addresses[0].Raw) == 0 {
		t.Fatalf("DepositAddresses() = %+v, error = %v", addresses, err)
	}
	if addresses[0].Currency != "BTC" || addresses[0].NetType != "BTC" ||
		addresses[0].DepositAddress != "13qdmpBMQVnwF8UGCARw7mfrP6rernoGUW" || addresses[0].SecondaryAddress != "" ||
		addresses[2].SecondaryAddress != "1004775304" {
		t.Fatalf("deposit addresses = %+v", addresses)
	}
	address, err := fixture.client.DepositAddressInfo(ctx, DepositAddressRequest{Currency: "XRP", NetType: "XRP"})
	if err != nil || address.DepositAddress != "rs6qtDrs8qp5qLkUxuzQdxFh5eGrQbL3Ee" || address.SecondaryAddress != "1004775304" ||
		address.Currency != "XRP" || address.NetType != "XRP" || len(address.Raw) == 0 {
		t.Fatalf("DepositAddressInfo() = %+v, error = %v", address, err)
	}

	records := extRequests(t, fixture.server, 6)
	listQuery := "currency=SHIB&state=DEPOSIT_ACCEPTED&uuids[]=202620152&page=1&limit=20&order_by=desc"
	assertExtRequest(t, records[0], "GET", "/v1/deposits", listQuery, "", listQuery)
	assertExtRequest(
		t, records[1], "GET", "/v1/deposits/krw", "state=ACCEPTED&txids[]=1596126", "", "state=ACCEPTED&txids[]=1596126",
	)
	assertExtRequest(t, records[2], "GET", "/v1/deposit", "currency=BTC&uuid=200012694", "", "currency=BTC&uuid=200012694")
	assertExtRequest(
		t, records[3], "GET", "/v1/deposit", "currency=BTC&txid=20200313145947.40057", "",
		"currency=BTC&txid=20200313145947.40057",
	)
	assertExtRequest(t, records[4], "GET", "/v1/deposits/coin_addresses", "", "", "")
	assertExtRequest(
		t, records[5], "GET", "/v1/deposits/coin_address", "currency=XRP&net_type=XRP", "", "currency=XRP&net_type=XRP",
	)
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:private-other:1second", 140, 6)
}

func TestGenerateDepositAddressAndDepositKRW(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{
		extReplies("POST /v1/deposits/generate_coin_address", extStatus(201, generatedDepositAddressSample)),
		extReplies("POST /v1/deposits/krw", extStatus(201, depositKRWSample)),
	}, extTransfer)
	ctx := context.Background()

	address, err := fixture.client.GenerateDepositAddress(ctx, GenerateDepositAddressRequest{Currency: "KLAY", NetType: "KLAY"})
	if err != nil || address.Currency != "KLAY" || address.NetType != "KLAY" ||
		address.DepositAddress != "0x147385478caae8d76f8796fdc78e7e2a07b87362" || address.SecondaryAddress != "" ||
		len(address.Raw) == 0 {
		t.Fatalf("GenerateDepositAddress() = %+v, error = %v", address, err)
	}
	deposit, err := fixture.client.DepositKRW(ctx, DepositKRWRequest{Amount: "5000", TwoFactorType: TwoFactorKakao})
	if err != nil || deposit.UUID != "15373074" || deposit.Currency != "KRW" || deposit.NetType != "" ||
		deposit.TxID != "1597453" || deposit.State != DepositStateKRWProcessing || deposit.DoneAt != "" ||
		deposit.Amount != "5000" || deposit.Fee != "0" || deposit.TransactionType != "default" || len(deposit.Raw) == 0 {
		t.Fatalf("DepositKRW() = %+v, error = %v", deposit, err)
	}

	records := extRequests(t, fixture.server, 2)
	assertExtRequest(
		t, records[0], "POST", "/v1/deposits/generate_coin_address", "",
		`{"currency":"KLAY","net_type":"KLAY"}`, "currency=KLAY&net_type=KLAY",
	)
	assertExtRequest(
		t, records[1], "POST", "/v1/deposits/krw", "",
		`{"amount":"5000","two_factor_type":"kakao"}`, "amount=5000&two_factor_type=kakao",
	)
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:private-other:1second", 140, 2)
}

func TestDepositMutationsRequireTransferPermission(t *testing.T) {
	t.Parallel()

	for name, permissions := range map[string][]credential.Permission{
		"read":     extReadOnly,
		"trade":    extTrader,
		"withdraw": extWithdraw,
	} {
		fixture := newExtFixture(t, []extRoute{
			extReplies("POST /v1/deposits/generate_coin_address", extStatus(201, generatedDepositAddressSample)),
			extReplies("POST /v1/deposits/krw", extStatus(201, depositKRWSample)),
		}, permissions)
		ctx := context.Background()
		_, addressErr := fixture.client.GenerateDepositAddress(ctx, GenerateDepositAddressRequest{Currency: "KLAY", NetType: "KLAY"})
		_, depositErr := fixture.client.DepositKRW(ctx, DepositKRWRequest{Amount: "5000", TwoFactorType: TwoFactorKakao})
		for operation, err := range map[string]error{"address": addressErr, "krw": depositErr} {
			if !errors.Is(err, trade.ErrAuthorization) || !errors.Is(err, credential.ErrPermissionNotAllowed) {
				t.Errorf("%s permission, %s: error = %v, want ErrAuthorization from missing transfer permission", name, operation, err)
			}
		}
		if calls, _, _ := fixture.provider.snapshot(); calls != 0 {
			t.Errorf("%s permission: credential provider calls = %d, want 0", name, calls)
		}
		extRequests(t, fixture.server, 0)
	}
}

func TestDepositMutationsAreNeverRetried(t *testing.T) {
	t.Parallel()

	serverError := `{"error":{"name":"internal_error","message":"boom"}}`
	fixture := newExtFixture(t, []extRoute{
		extReplies("POST /v1/deposits/generate_coin_address", extStatus(500, serverError)),
		extReplies("POST /v1/deposits/krw", extStatus(503, serverError)),
	}, extTransfer)
	ctx := context.Background()
	if _, err := fixture.client.GenerateDepositAddress(ctx, GenerateDepositAddressRequest{Currency: "KLAY", NetType: "KLAY"}); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("GenerateDepositAddress() 5xx error = %v, want ErrUnknownExecutionState", err)
	}
	if _, err := fixture.client.DepositKRW(ctx, DepositKRWRequest{Amount: "5000", TwoFactorType: TwoFactorKakao}); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("DepositKRW() 5xx error = %v, want ErrUnknownExecutionState", err)
	}
	extRequests(t, fixture.server, 2)

	networkClient, _, _ := newExtClient(t, "http://127.0.0.1", errorSender{}, extTransfer)
	if _, err := networkClient.DepositKRW(ctx, DepositKRWRequest{Amount: "5000", TwoFactorType: TwoFactorKakao}); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("DepositKRW() network error = %v, want ErrUnknownExecutionState", err)
	}
}

func TestDepositReadsRetryTransientFailures(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{
		extReplies(
			"GET /v1/deposits/coin_addresses",
			extStatus(503, `{"error":{"name":"service_unavailable","message":"retry later"}}`),
			extStatus(200, depositAddressesSample),
		),
	}, extReadOnly)
	addresses, err := fixture.client.DepositAddresses(context.Background())
	if err != nil || len(addresses) != 3 {
		t.Fatalf("DepositAddresses() = %+v, error = %v", addresses, err)
	}
	for _, record := range extRequests(t, fixture.server, 2) {
		assertExtRequest(t, record, "GET", "/v1/deposits/coin_addresses", "", "", "")
	}
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:private-other:1second", 140, 2)
}

func TestDepositRequestsRejectInvalidInput(t *testing.T) {
	t.Parallel()

	invalid := []struct {
		name string
		err  error
	}{
		{"deposits lowercase state", (DepositsRequest{State: "deposit_accepted"}).validate()},
		{"deposits state with ampersand", (DepositsRequest{State: "ACCEPTED&x=1"}).validate()},
		{"deposits padded currency", (DepositsRequest{Currency: "BTC "}).validate()},
		{"deposits UUID with equals", (DepositsRequest{UUIDs: []string{"a=b"}}).validate()},
		{"deposits empty TxID", (DepositsRequest{TxIDs: []string{""}}).validate()},
		{"deposits negative page", (DepositsRequest{Page: -1}).validate()},
		{"deposits limit above maximum", (DepositsRequest{Limit: 101}).validate()},
		{"deposits invalid order by", (DepositsRequest{OrderBy: "newest"}).validate()},
		{"KRW deposits state with space", (KRWDepositsRequest{State: "NOT ACCEPTED"}).validate()},
		{"KRW deposits negative limit", (KRWDepositsRequest{Limit: -1}).validate()},
		{"info without currency", (DepositInfoRequest{UUID: "1"}).validate()},
		{"info without identity", (DepositInfoRequest{Currency: "BTC"}).validate()},
		{"info with both identities", (DepositInfoRequest{Currency: "BTC", UUID: "1", TxID: "2"}).validate()},
		{"info TxID with ampersand", (DepositInfoRequest{Currency: "BTC", TxID: "1&2"}).validate()},
		{"address without currency", (DepositAddressRequest{NetType: "BTC"}).validate()},
		{"address without net type", (DepositAddressRequest{Currency: "BTC"}).validate()},
		{"generate without currency", (GenerateDepositAddressRequest{NetType: "BTC"}).validate()},
		{"generate net type with ampersand", (GenerateDepositAddressRequest{Currency: "BTC", NetType: "BTC&x=1"}).validate()},
		{"KRW deposit zero amount", (DepositKRWRequest{Amount: "0", TwoFactorType: TwoFactorKakao}).validate()},
		{"KRW deposit non numeric amount", (DepositKRWRequest{Amount: "5,000", TwoFactorType: TwoFactorKakao}).validate()},
		{"KRW deposit missing two factor", (DepositKRWRequest{Amount: "5000"}).validate()},
		{"KRW deposit uppercase two factor", (DepositKRWRequest{Amount: "5000", TwoFactorType: "KAKAO"}).validate()},
	}
	for _, test := range invalid {
		if !errors.Is(test.err, trade.ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", test.name, test.err)
		}
	}

	states := []DepositState{
		DepositStateRequestedPending, DepositStateRequestedSystemRejected, DepositStateRequestedProcessing,
		DepositStateRequestedAdminRejected, DepositStateDepositProcessing, DepositStateDepositAccepted,
		DepositStateDepositCancelled, DepositStateRefundingPending, DepositStateRefundingSystemRejected,
		DepositStateRefundingProcessing, DepositStateRefundingAdminRejected, DepositStateRefundingAccepted,
		DepositStateRefundedProcessing, DepositStateRefundedAccepted, DepositStateRefundedCancelled,
		DepositStateKRWProcessing, DepositStateKRWAccepted, DepositStateKRWCancelled,
	}
	for _, state := range states {
		if err := (DepositsRequest{State: state}).validate(); err != nil {
			t.Errorf("DepositsRequest state %q error = %v", state, err)
		}
		if err := (KRWDepositsRequest{State: state}).validate(); err != nil {
			t.Errorf("KRWDepositsRequest state %q error = %v", state, err)
		}
	}

	accepted := []error{
		(DepositsRequest{}).validate(),
		(DepositsRequest{Currency: "BTC", Page: 1, Limit: 100, OrderBy: "asc", UUIDs: []string{"1", "2"}}).validate(),
		(DepositInfoRequest{Currency: "BTC", UUID: "200012694"}).validate(),
		(DepositInfoRequest{Currency: "BTC", TxID: "0xabc"}).validate(),
		(DepositAddressRequest{Currency: "XRP", NetType: "XRP"}).validate(),
		(GenerateDepositAddressRequest{Currency: "KLAY", NetType: "KLAY"}).validate(),
		(DepositKRWRequest{Amount: "5000.5", TwoFactorType: TwoFactorKakao}).validate(),
	}
	for index, err := range accepted {
		if err != nil {
			t.Errorf("accepted deposit request %d error = %v", index, err)
		}
	}
}

func TestDepositRequestsRejectInvalidInputBeforeSending(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, nil, extTransfer)
	ctx := context.Background()
	if _, err := fixture.client.Deposits(ctx, DepositsRequest{Limit: 500}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("Deposits() error = %v, want ErrValidation", err)
	}
	if _, err := fixture.client.DepositInfo(ctx, DepositInfoRequest{Currency: "BTC"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("DepositInfo() error = %v, want ErrValidation", err)
	}
	if _, err := fixture.client.DepositAddressInfo(ctx, DepositAddressRequest{Currency: "BTC"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("DepositAddressInfo() error = %v, want ErrValidation", err)
	}
	if _, err := fixture.client.GenerateDepositAddress(ctx, GenerateDepositAddressRequest{NetType: "BTC"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("GenerateDepositAddress() error = %v, want ErrValidation", err)
	}
	if _, err := fixture.client.DepositKRW(ctx, DepositKRWRequest{Amount: "5000"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("DepositKRW() error = %v, want ErrValidation", err)
	}
	extRequests(t, fixture.server, 0)
	if calls, _, _ := fixture.provider.snapshot(); calls != 0 {
		t.Fatalf("credential provider calls = %d, want 0", calls)
	}
}
