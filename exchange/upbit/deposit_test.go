package upbit

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

const depositJSON = `{"type":"deposit","uuid":"94332e99-3a87-4a35-ad98-28b0c969f830","currency":"KRW","net_type":null,"txid":"BKD-2000-12-29-aeked29c05eadac293b4214994","state":"ACCEPTED","created_at":"2025-07-04T15:00:00+09:00","done_at":"2025-07-04T15:00:10+09:00","amount":"100000.0","fee":"0.0","transaction_type":"default"}`

func TestDepositReadEndpoints(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Remaining-Req", "group=default; min=1800; sec=29")
		verifySignedRequest(t, request, []byte("secret-key"), "nonce-fixed")
		switch request.Method + " " + request.URL.RequestURI() {
		case "GET /v1/deposits/chance/coin?currency=BTC&net_type=BTC":
			_, _ = io.WriteString(writer, `{"currency":"BTC","net_type":"BTC","is_deposit_possible":false,"deposit_impossible_reason":"네트워크 업그레이드로 인한 입출금 일시 중단","minimum_deposit_amount":"0","minimum_deposit_confirmations":1,"decimal_precision":8}`)
		case "GET /v1/deposits/coin_address?currency=XRP&net_type=XRP":
			_, _ = io.WriteString(writer, `{"currency":"XRP","net_type":"XRP","deposit_address":"rN9qNpgnBaZwqCg8CvUZRPqCcPPY7wfWep","secondary_address":"3057887915"}`)
		case "GET /v1/deposits/coin_address?currency=BTC&net_type=BTC":
			_, _ = io.WriteString(writer, `{"currency":"BTC","net_type":"BTC","deposit_address":null,"secondary_address":null}`)
		case "GET /v1/deposits/coin_addresses":
			_, _ = io.WriteString(writer, `[{"currency":"BTC","net_type":"BTC","deposit_address":"3EusRwybuZUhVDeHL7gh3HSLmbhLcy7NqD"},{"currency":"ETH","net_type":"ETH","deposit_address":"0x0d73e0a482b8cf568976d2e8688f4a899d29301c"},{"currency":"XRP","net_type":"XRP","deposit_address":"rN9qNpgnBaZwqCg8CvUZRPqCcPPY7wfWep","secondary_address":"3057887915"}]`)
		case "GET /v1/deposit?uuid=94332e99-3a87-4a35-ad98-28b0c969f830&txid=BKD-2000-12-29-aeked29c05eadac293b4214994&currency=KRW":
			_, _ = io.WriteString(writer, depositJSON)
		case "GET /v1/deposit":
			_, _ = io.WriteString(writer, `{"type":"deposit","uuid":"latest-1","currency":"BTC","net_type":"BTC","txid":"5BC9E3CD","state":"TRAVEL_RULE_SUSPECTED","created_at":"2025-07-04T15:00:00+09:00","done_at":null,"amount":"0.5","fee":"0","transaction_type":"internal"}`)
		case "GET /v1/deposits?currency=BTC&state=ACCEPTED&uuids[]=uuid-1&uuids[]=uuid-2&limit=50&page=2&order_by=asc&from=uuid-from&to=uuid-to":
			_, _ = io.WriteString(writer, `[`+depositJSON+`,`+depositJSON+`]`)
		case "GET /v1/deposits?txids[]=tx-1":
			_, _ = io.WriteString(writer, `[`+depositJSON+`]`)
		case "GET /v1/deposits":
			_, _ = io.WriteString(writer, `[]`)
		case "GET /v1/travel_rule/vasps":
			_, _ = io.WriteString(writer, `[{"vasp_name":"업비트","vasp_uuid":"00000000-0000-0000-0000-000000000000","depositable":true,"withdrawable":true},{"vasp_name":"Example","vasp_uuid":"8d4fe968-82b2-42e5-822f-3840a245f802","depositable":true,"withdrawable":false}]`)
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.RequestURI())
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, provider, limiter := newDepositTestClient(t, server.URL, credential.PermissionRead)
	ctx := context.Background()

	chance, err := client.DepositChance(ctx, DepositChanceRequest{Currency: "BTC", NetType: "BTC"})
	if err != nil || chance.Currency != "BTC" || chance.NetType != "BTC" || chance.IsDepositPossible ||
		chance.DepositImpossibleReason != "네트워크 업그레이드로 인한 입출금 일시 중단" || chance.MinimumDepositAmount != "0" ||
		chance.MinimumDepositConfirmations != 1 || chance.DecimalPrecision != 8 || len(chance.Raw) == 0 {
		t.Fatalf("DepositChance() = %+v, error = %v", chance, err)
	}
	address, err := client.DepositAddress(ctx, DepositAddressRequest{Currency: "XRP", NetType: "XRP"})
	if err != nil || address.Address != "rN9qNpgnBaZwqCg8CvUZRPqCcPPY7wfWep" || address.SecondaryAddress != "3057887915" || len(address.Raw) == 0 {
		t.Fatalf("DepositAddress() = %+v, error = %v", address, err)
	}
	ungenerated, err := client.DepositAddress(ctx, DepositAddressRequest{Currency: "BTC", NetType: "BTC"})
	if err != nil || ungenerated.Address != "" || ungenerated.SecondaryAddress != "" || ungenerated.NetType != "BTC" {
		t.Fatalf("DepositAddress() before generation = %+v, error = %v", ungenerated, err)
	}
	addresses, err := client.DepositAddresses(ctx)
	if err != nil || len(addresses) != 3 || addresses[1].Address != "0x0d73e0a482b8cf568976d2e8688f4a899d29301c" ||
		addresses[2].SecondaryAddress != "3057887915" || len(addresses[0].Raw) == 0 {
		t.Fatalf("DepositAddresses() = %+v, error = %v", addresses, err)
	}
	deposit, err := client.DepositInfo(ctx, DepositInfoRequest{
		UUID: "94332e99-3a87-4a35-ad98-28b0c969f830", TxID: "BKD-2000-12-29-aeked29c05eadac293b4214994", Currency: "KRW",
	})
	if err != nil || deposit.UUID != "94332e99-3a87-4a35-ad98-28b0c969f830" || deposit.State != DepositStateAccepted ||
		deposit.NetType != "" || deposit.Amount != "100000.0" || deposit.Fee != "0.0" ||
		deposit.DoneAt != "2025-07-04T15:00:10+09:00" || deposit.TransactionType != DepositTransactionDefault || len(deposit.Raw) == 0 {
		t.Fatalf("DepositInfo() = %+v, error = %v", deposit, err)
	}
	latest, err := client.DepositInfo(ctx, DepositInfoRequest{})
	if err != nil || latest.UUID != "latest-1" || latest.State != DepositStateTravelRuleSuspected ||
		latest.NetType != "BTC" || latest.DoneAt != "" || latest.TransactionType != DepositTransactionInternal {
		t.Fatalf("DepositInfo(latest) = %+v, error = %v", latest, err)
	}
	deposits, err := client.Deposits(ctx, DepositsRequest{
		Currency: "BTC", State: DepositStateAccepted, UUIDs: []string{"uuid-1", "uuid-2"},
		Limit: 50, Page: 2, OrderBy: "asc", From: "uuid-from", To: "uuid-to",
	})
	if err != nil || len(deposits) != 2 || deposits[1].State != DepositStateAccepted || len(deposits[0].Raw) == 0 {
		t.Fatalf("Deposits() = %+v, error = %v", deposits, err)
	}
	byTxID, err := client.Deposits(ctx, DepositsRequest{TxIDs: []string{"tx-1"}})
	if err != nil || len(byTxID) != 1 {
		t.Fatalf("Deposits(txids) = %+v, error = %v", byTxID, err)
	}
	empty, err := client.Deposits(ctx, DepositsRequest{})
	if err != nil || len(empty) != 0 {
		t.Fatalf("Deposits(empty) = %+v, error = %v", empty, err)
	}
	vasps, err := client.TravelRuleVASPs(ctx)
	if err != nil || len(vasps) != 2 || vasps[0].VASPName != "업비트" || !vasps[0].Withdrawable ||
		vasps[1].VASPUUID != "8d4fe968-82b2-42e5-822f-3840a245f802" || vasps[1].Withdrawable || len(vasps[1].Raw) == 0 {
		t.Fatalf("TravelRuleVASPs() = %+v, error = %v", vasps, err)
	}

	providerCalls, key, secret := provider.snapshot()
	if providerCalls != 10 || !allZero(key) || !allZero(secret) {
		t.Fatalf("provider calls = %d, key zero = %v, secret zero = %v", providerCalls, allZero(key), allZero(secret))
	}
	snapshot, err := limiter.Snapshot("upbit:account:upbit-pocket:default:1second")
	if err != nil || snapshot.Rule.Limit != 30 {
		t.Fatalf("default limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestDepositMutationEndpoints(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Remaining-Req", "group=default; min=1800; sec=29")
		body, _ := io.ReadAll(request.Body)
		switch request.Method + " " + request.URL.RequestURI() + " " + string(body) {
		case `POST /v1/deposits/generate_coin_address {"currency":"BTC","net_type":"BTC"}`:
			verifySignedBody(t, request, "currency=BTC&net_type=BTC")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"success":true,"message":"BTC 입금 주소를 생성 중 입니다."}`)
		case `POST /v1/deposits/generate_coin_address {"currency":"ETH","net_type":"ETH"}`:
			verifySignedBody(t, request, "currency=ETH&net_type=ETH")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"currency":"ETH","net_type":"ETH","deposit_address":"0x0d73e0a482b8cf568976d2e8688f4a899d29301c"}`)
		case `POST /v1/deposits/krw {"amount":"10000","two_factor_type":"naver"}`:
			verifySignedBody(t, request, "amount=10000&two_factor_type=naver")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"type":"deposit","uuid":"9f432943-54e0-40b7-825f-b6fec8b42b79","currency":"KRW","txid":"ebe6937b-130e-4066-8ac6-4b0e67f28adc","state":"ACCEPTED","created_at":"2025-07-04T15:00:00+09:00","amount":"10000","fee":"0.0","transaction_type":"default"}`)
		case `POST /v1/travel_rule/deposit/uuid {"deposit_uuid":"5b871d34-fe38-4025-8f5c-9b22028f85d3","vasp_uuid":"8d4fe968-82b2-42e5-822f-3840a245f802"}`:
			verifySignedBody(t, request, "deposit_uuid=5b871d34-fe38-4025-8f5c-9b22028f85d3&vasp_uuid=8d4fe968-82b2-42e5-822f-3840a245f802")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"deposit_uuid":"5b871d34-fe38-4025-8f5c-9b22028f85d3","verification_result":"verified","deposit_state":"PROCESSING"}`)
		case `POST /v1/travel_rule/deposit/txid {"txid":"0xabc123","vasp_uuid":"8d4fe968-82b2-42e5-822f-3840a245f802","currency":"ETH","net_type":"ETH"}`:
			verifySignedBody(t, request, "txid=0xabc123&vasp_uuid=8d4fe968-82b2-42e5-822f-3840a245f802&currency=ETH&net_type=ETH")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"deposit_uuid":"9f432943-54e0-40b7-825f-b6fec8b42b79","verification_result":"verified","deposit_state":"PROCESSING"}`)
		default:
			t.Errorf("unexpected request %s %s %s", request.Method, request.URL.RequestURI(), body)
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, provider, limiter := newDepositTestClient(t, server.URL, credential.PermissionTransfer)
	ctx := context.Background()

	pending, err := client.GenerateDepositAddress(ctx, GenerateDepositAddressRequest{Currency: "BTC", NetType: "BTC"})
	if err != nil || !pending.Pending() || pending.Address != "" || pending.Message != "BTC 입금 주소를 생성 중 입니다." || len(pending.Raw) == 0 {
		t.Fatalf("GenerateDepositAddress(pending) = %+v, error = %v", pending, err)
	}
	ready, err := client.GenerateDepositAddress(ctx, GenerateDepositAddressRequest{Currency: "ETH", NetType: "ETH"})
	if err != nil || ready.Pending() || ready.Address != "0x0d73e0a482b8cf568976d2e8688f4a899d29301c" || ready.NetType != "ETH" || ready.Message != "" {
		t.Fatalf("GenerateDepositAddress(ready) = %+v, error = %v", ready, err)
	}
	krw, err := client.DepositKRW(ctx, KRWDepositRequest{Amount: "10000", TwoFactorType: TwoFactorNaver})
	if err != nil || krw.UUID != "9f432943-54e0-40b7-825f-b6fec8b42b79" || krw.Currency != "KRW" || krw.State != DepositStateAccepted ||
		krw.NetType != "" || krw.DoneAt != "" || krw.Amount != "10000" || len(krw.Raw) == 0 {
		t.Fatalf("DepositKRW() = %+v, error = %v", krw, err)
	}
	byUUID, err := client.VerifyTravelRuleByUUID(ctx, TravelRuleUUIDRequest{
		DepositUUID: "5b871d34-fe38-4025-8f5c-9b22028f85d3", VASPUUID: "8d4fe968-82b2-42e5-822f-3840a245f802",
	})
	if err != nil || byUUID.DepositUUID != "5b871d34-fe38-4025-8f5c-9b22028f85d3" || byUUID.VerificationResult != "verified" ||
		byUUID.DepositState != DepositStateProcessing || len(byUUID.Raw) == 0 {
		t.Fatalf("VerifyTravelRuleByUUID() = %+v, error = %v", byUUID, err)
	}
	byTxID, err := client.VerifyTravelRuleByTxID(ctx, TravelRuleTxIDRequest{
		VASPUUID: "8d4fe968-82b2-42e5-822f-3840a245f802", TxID: "0xabc123", Currency: "ETH", NetType: "ETH",
	})
	if err != nil || byTxID.DepositUUID != "9f432943-54e0-40b7-825f-b6fec8b42b79" || byTxID.VerificationResult != "verified" ||
		byTxID.DepositState != DepositStateProcessing || len(byTxID.Raw) == 0 {
		t.Fatalf("VerifyTravelRuleByTxID() = %+v, error = %v", byTxID, err)
	}

	providerCalls, key, secret := provider.snapshot()
	if providerCalls != 5 || !allZero(key) || !allZero(secret) {
		t.Fatalf("provider calls = %d, key zero = %v, secret zero = %v", providerCalls, allZero(key), allZero(secret))
	}
	snapshot, err := limiter.Snapshot("upbit:account:upbit-pocket:default:1second")
	if err != nil || snapshot.Rule.Limit != 30 {
		t.Fatalf("default limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestDepositPermissionBoundaries(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer server.Close()

	ctx := context.Background()
	readOnly, readProvider, _ := newDepositTestClient(t, server.URL, credential.PermissionRead)
	transferOnly, transferProvider, _ := newDepositTestClient(t, server.URL, credential.PermissionTransfer)
	tests := []struct {
		name string
		call func() error
	}{
		{"GenerateDepositAddress needs transfer", func() error {
			_, err := readOnly.GenerateDepositAddress(ctx, GenerateDepositAddressRequest{Currency: "BTC", NetType: "BTC"})
			return err
		}},
		{"DepositKRW needs transfer", func() error {
			_, err := readOnly.DepositKRW(ctx, KRWDepositRequest{Amount: "10000", TwoFactorType: TwoFactorKakao})
			return err
		}},
		{"VerifyTravelRuleByUUID needs transfer", func() error {
			_, err := readOnly.VerifyTravelRuleByUUID(ctx, TravelRuleUUIDRequest{DepositUUID: "deposit", VASPUUID: "vasp"})
			return err
		}},
		{"VerifyTravelRuleByTxID needs transfer", func() error {
			_, err := readOnly.VerifyTravelRuleByTxID(ctx, TravelRuleTxIDRequest{VASPUUID: "vasp", TxID: "tx", Currency: "ETH", NetType: "ETH"})
			return err
		}},
		{"DepositChance needs read", func() error {
			_, err := transferOnly.DepositChance(ctx, DepositChanceRequest{Currency: "BTC", NetType: "BTC"})
			return err
		}},
		{"DepositAddress needs read", func() error {
			_, err := transferOnly.DepositAddress(ctx, DepositAddressRequest{Currency: "BTC", NetType: "BTC"})
			return err
		}},
		{"DepositAddresses needs read", func() error {
			_, err := transferOnly.DepositAddresses(ctx)
			return err
		}},
		{"DepositInfo needs read", func() error {
			_, err := transferOnly.DepositInfo(ctx, DepositInfoRequest{})
			return err
		}},
		{"Deposits needs read", func() error {
			_, err := transferOnly.Deposits(ctx, DepositsRequest{})
			return err
		}},
		{"TravelRuleVASPs needs read", func() error {
			_, err := transferOnly.TravelRuleVASPs(ctx)
			return err
		}},
	}
	for _, test := range tests {
		if err := test.call(); !errors.Is(err, trade.ErrAuthorization) {
			t.Fatalf("%s: error = %v, want ErrAuthorization", test.name, err)
		}
	}
	readCalls, _, _ := readProvider.snapshot()
	transferCalls, _, _ := transferProvider.snapshot()
	if hits.Load() != 0 || readCalls != 0 || transferCalls != 0 {
		t.Fatalf("server hits = %d, provider calls = %d/%d, want no request or secret resolution", hits.Load(), readCalls, transferCalls)
	}
}

func TestDepositErrorsAndMutationsAreNotRetried(t *testing.T) {
	t.Parallel()

	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/deposit":
			writer.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(writer, `{"error":{"name":"deposit_not_found","message":"입출금 정보를 찾지 못했습니다."}}`)
		case "/v1/travel_rule/deposit/uuid":
			posts.Add(1)
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"error":{"name":"already_have_deposit_transaction","message":"이미 입금 이력이 존재합니다."}}`)
		case "/v1/deposits/generate_coin_address":
			posts.Add(1)
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(writer, `{"error":{"name":"internal_error","message":"temporary"}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, _, _ := newDepositTestClient(t, server.URL, credential.PermissionRead, credential.PermissionTransfer)
	ctx := context.Background()

	var apiError *trade.APIError
	_, err := client.DepositInfo(ctx, DepositInfoRequest{UUID: "missing"})
	if !errors.As(err, &apiError) || apiError.HTTPStatus != http.StatusNotFound || apiError.ExchangeCode != "deposit_not_found" {
		t.Fatalf("DepositInfo() error = %v, want deposit_not_found", err)
	}
	_, err = client.VerifyTravelRuleByUUID(ctx, TravelRuleUUIDRequest{DepositUUID: "deposit", VASPUUID: "vasp"})
	if !errors.Is(err, trade.ErrValidation) || !errors.As(err, &apiError) || apiError.ExchangeCode != "already_have_deposit_transaction" {
		t.Fatalf("VerifyTravelRuleByUUID() error = %v, want already_have_deposit_transaction", err)
	}
	_, err = client.GenerateDepositAddress(ctx, GenerateDepositAddressRequest{Currency: "BTC", NetType: "BTC"})
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("GenerateDepositAddress() error = %v, want ErrUnknownExecutionState", err)
	}
	if posts.Load() != 2 {
		t.Fatalf("POST requests = %d, want 2 without retry", posts.Load())
	}
}

type depositValidationCase struct {
	name string
	err  error
}

func TestDepositRequestValidation(t *testing.T) {
	t.Parallel()

	valid := []depositValidationCase{
		{"chance", DepositChanceRequest{Currency: "BTC", NetType: "BTC"}.validate()},
		{"generate", GenerateDepositAddressRequest{Currency: "XRP", NetType: "XRP"}.validate()},
		{"address", DepositAddressRequest{Currency: "USDT", NetType: "TRX"}.validate()},
		{"krw", KRWDepositRequest{Amount: "10000.5", TwoFactorType: TwoFactorHana}.validate()},
		{"info latest", DepositInfoRequest{}.validate()},
		{"info all", DepositInfoRequest{UUID: "uuid", TxID: "txid", Currency: "KRW"}.validate()},
		{"list default", DepositsRequest{}.validate()},
		{"list full", DepositsRequest{
			Currency: "BTC", State: DepositStateTravelRuleSuspected, UUIDs: []string{"a", "b"}, TxIDs: []string{"c"},
			Limit: 100, Page: 1, OrderBy: "desc", From: "a", To: "b",
		}.validate()},
		{"travel rule uuid", TravelRuleUUIDRequest{DepositUUID: "deposit", VASPUUID: "vasp"}.validate()},
		{"travel rule txid", TravelRuleTxIDRequest{VASPUUID: "vasp", TxID: "tx", Currency: "ETH", NetType: "ETH"}.validate()},
	}
	for _, test := range valid {
		if test.err != nil {
			t.Fatalf("%s: validate() error = %v", test.name, test.err)
		}
	}

	invalid := []depositValidationCase{
		{"chance lowercase currency", DepositChanceRequest{Currency: "btc", NetType: "BTC"}.validate()},
		{"chance missing net type", DepositChanceRequest{Currency: "BTC"}.validate()},
		{"chance whitespace net type", DepositChanceRequest{Currency: "BTC", NetType: "B TC"}.validate()},
		{"generate missing currency", GenerateDepositAddressRequest{NetType: "BTC"}.validate()},
		{"address control character", DepositAddressRequest{Currency: "BTC", NetType: "BTC\n"}.validate()},
		{"krw zero amount", KRWDepositRequest{Amount: "0", TwoFactorType: TwoFactorNaver}.validate()},
		{"krw missing two factor", KRWDepositRequest{Amount: "10000"}.validate()},
		{"krw unsupported two factor", KRWDepositRequest{Amount: "10000", TwoFactorType: "sms"}.validate()},
		{"info whitespace uuid", DepositInfoRequest{UUID: " uuid"}.validate()},
		{"info lowercase currency", DepositInfoRequest{Currency: "krw"}.validate()},
		{"list withdrawal state", DepositsRequest{State: "DONE"}.validate()},
		{"list limit too large", DepositsRequest{Limit: 101}.validate()},
		{"list negative limit", DepositsRequest{Limit: -1}.validate()},
		{"list negative page", DepositsRequest{Page: -1}.validate()},
		{"list unsupported order", DepositsRequest{OrderBy: "up"}.validate()},
		{"list duplicate uuid", DepositsRequest{UUIDs: []string{"a", "a"}}.validate()},
		{"list empty txid", DepositsRequest{TxIDs: []string{"a", ""}}.validate()},
		{"list whitespace cursor", DepositsRequest{From: "uuid cursor"}.validate()},
		{"travel rule uuid missing deposit", TravelRuleUUIDRequest{VASPUUID: "vasp"}.validate()},
		{"travel rule uuid missing vasp", TravelRuleUUIDRequest{DepositUUID: "deposit"}.validate()},
		{"travel rule txid missing net type", TravelRuleTxIDRequest{VASPUUID: "vasp", TxID: "tx", Currency: "ETH"}.validate()},
		{"travel rule txid missing txid", TravelRuleTxIDRequest{VASPUUID: "vasp", Currency: "ETH", NetType: "ETH"}.validate()},
	}
	for _, test := range invalid {
		if !errors.Is(test.err, trade.ErrValidation) {
			t.Fatalf("%s: validate() error = %v, want ErrValidation", test.name, test.err)
		}
	}
}

func newDepositTestClient(
	t *testing.T,
	baseURL string,
	permissions ...credential.Permission,
) (*Client, *recordingProvider, *ratelimit.Limiter) {
	t.Helper()
	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: &directSender{}, Limiter: limiter})
	if err != nil {
		t.Fatalf("exchange.NewExecutor() error = %v", err)
	}
	provider := &recordingProvider{}
	client, err := New(Config{
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "upbit-pocket", Exchange: model.ExchangeUpbit, SecretRef: "secret/upbit",
			Permissions:           permissions,
			AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
		NonceSource: func() (string, error) { return "nonce-fixed", nil },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, provider, limiter
}

// verifySignedBody는 POST 본문 키-값을 순서대로 이은 문자열(wantHashInput)의 SHA-512 쿼리 해시가 JWT에 담겼는지 검증한다.
func verifySignedBody(t *testing.T, request *http.Request, wantHashInput string) {
	t.Helper()
	if got := request.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if request.URL.RawQuery != "" {
		t.Errorf("POST query = %q, want empty", request.URL.RawQuery)
	}
	payload := decodeSignedPayload(t, request, []byte("secret-key"))
	if payload["access_key"] != "access-key" || payload["nonce"] != "nonce-fixed" ||
		payload["query_hash"] != QueryHash(wantHashInput) || payload["query_hash_alg"] != "SHA512" {
		t.Errorf("JWT payload = %v, want query hash of %q", payload, wantHashInput)
	}
}

func decodeSignedPayload(t *testing.T, request *http.Request, secret []byte) map[string]string {
	t.Helper()
	token, found := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(token, ".")
	if !found || len(parts) != 3 {
		t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		return nil
	}
	mac := hmac.New(sha512.New, secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if parts[2] != base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) {
		t.Error("JWT signature is invalid")
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Errorf("decode JWT payload: %v", err)
		return nil
	}
	var payload map[string]string
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Errorf("decode JWT payload JSON: %v", err)
		return nil
	}
	return payload
}
