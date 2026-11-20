package upbit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

const (
	withdrawalUUID           = "9f432943-54e0-40b7-825f-b6fec8b42b79"
	withdrawalProcessingJSON = `{"type":"withdraw","uuid":"9f432943-54e0-40b7-825f-b6fec8b42b79","currency":"BTC","net_type":"BTC","txid":"ebe6937b-130e-4066-8ac6-4b0e67f28adc","state":"PROCESSING","created_at":"2018-04-13T11:24:01+09:00","amount":"0.01","fee":"0.0","transaction_type":"default","is_cancelable":false}`
	withdrawChanceJSON       = `{"member_level":{"security_level":4,"fee_level":0,"email_verified":true,"identity_auth_verified":true,"bank_account_verified":true,"two_factor_auth_verified":true,"locked":false,"wallet_locked":false},"currency":{"code":"BTC","withdraw_fee":"0.0008","is_coin":true,"wallet_state":"working","wallet_support":["deposit","withdraw"]},"account":{"currency":"BTC","balance":"0.0","locked":"0.0","avg_buy_price":"145115000","avg_buy_price_modified":false,"unit_currency":"KRW"},"withdraw_limit":{"currency":"BTC","onetime":"50.0","daily":"100.0","remaining_daily":"0.0","remaining_daily_fiat":"5000000000.0","fiat_currency":"KRW","minimum":"0.00001","fixed":8,"withdraw_delayed_fiat":"227.0","can_withdraw":true,"remaining_daily_krw":"4999999000.0"}}`
	withdrawChanceKRWJSON    = `{"member_level":{"security_level":3,"fee_level":0,"email_verified":true,"identity_auth_verified":true,"bank_account_verified":true,"two_factor_auth_verified":false,"locked":false,"wallet_locked":true},"currency":{"code":"KRW","withdraw_fee":"1000.0","is_coin":false,"wallet_state":"working","wallet_support":["deposit","withdraw"]},"account":{"currency":"KRW","balance":"1000000.0","locked":"0.0","avg_buy_price":"0","avg_buy_price_modified":true,"unit_currency":"KRW"},"withdraw_limit":{"currency":"KRW","onetime":null,"daily":null,"remaining_daily":null,"remaining_daily_fiat":null,"fiat_currency":null,"minimum":"5000","fixed":0,"withdraw_delayed_fiat":"0","can_withdraw":false}}`
)

func TestWithdrawReadEndpoints(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Remaining-Req", "group=default; min=1800; sec=29")
		switch request.Method + " " + request.URL.RequestURI() {
		case "GET /v1/withdraws/chance?currency=BTC&net_type=BTC":
			verifySignedQuery(t, request, "currency=BTC&net_type=BTC")
			_, _ = io.WriteString(writer, withdrawChanceJSON)
		case "GET /v1/withdraws/chance?currency=KRW":
			verifySignedQuery(t, request, "currency=KRW")
			_, _ = io.WriteString(writer, withdrawChanceKRWJSON)
		case "GET /v1/withdraws/coin_addresses":
			verifySignedQuery(t, request, "")
			_, _ = io.WriteString(writer, `[{"currency":"BTC","net_type":"BTC","network_name":"Bitcoin","withdraw_address":"1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa","beneficiary_name":"John","beneficiary_type":"individual","exchange_name":"바이낸스"},{"currency":"ETH","net_type":"ETH","network_name":"Ethereum","withdraw_address":"0x1234615148db0926d76bde31d420abcd5439484fd","secondary_address":null,"beneficiary_name":"John","wallet_type":"메타마스크"}]`)
		case "GET /v1/withdraw?uuid=" + withdrawalUUID + "&txid=ebe6937b-130e-4066-8ac6-4b0e67f28adc&currency=BTC":
			verifySignedQuery(t, request, "uuid="+withdrawalUUID+"&txid=ebe6937b-130e-4066-8ac6-4b0e67f28adc&currency=BTC")
			_, _ = io.WriteString(writer, withdrawalProcessingJSON)
		case "GET /v1/withdraw":
			verifySignedQuery(t, request, "")
			_, _ = io.WriteString(writer, `{"type":"withdraw","uuid":"latest-1","currency":"KRW","net_type":null,"txid":null,"state":"done","created_at":"2025-07-01T15:00:00+09:00","done_at":"2025-07-01T15:00:30+09:00","amount":"10000","fee":"0.0","transaction_type":"default","is_cancelable":null}`)
		case "GET /v1/withdraws?currency=XRP&state=DONE&uuids[]=uuid-1&uuids[]=uuid-2&limit=50&page=2&order_by=asc&from=uuid-from&to=uuid-to":
			verifySignedQuery(t, request, "currency=XRP&state=DONE&uuids[]=uuid-1&uuids[]=uuid-2&limit=50&page=2&order_by=asc&from=uuid-from&to=uuid-to")
			_, _ = io.WriteString(writer, `[{"type":"withdraw","uuid":"uuid-1","currency":"XRP","net_type":"XRP","txid":"98c15999f0bdc4ae0e8a-ed35868bb0c204fe6ec29e4058a3451e-88636d1040f4baddf943274ce37cf9cc","state":"DONE","created_at":"2025-07-04T15:00:00+09:00","done_at":"2025-07-04T15:00:20+09:00","amount":"10.00","fee":"0.0","transaction_type":"default","is_cancelable":false},{"type":"withdraw","uuid":"uuid-2","currency":"XRP","net_type":"XRP","txid":null,"state":"DONE","created_at":"2025-07-03T15:00:00+09:00","done_at":"2025-07-03T15:00:20+09:00","amount":"5","fee":"0.1","transaction_type":"internal","is_cancelable":false}]`)
		case "GET /v1/withdraws?txids[]=tx-1":
			verifySignedQuery(t, request, "txids[]=tx-1")
			_, _ = io.WriteString(writer, `[`+withdrawalProcessingJSON+`]`)
		case "GET /v1/withdraws":
			verifySignedQuery(t, request, "")
			_, _ = io.WriteString(writer, `[]`)
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.RequestURI())
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, provider, limiter := newPrivateTestClient(t, server.URL, credential.PermissionRead)
	ctx := context.Background()

	chance, err := client.WithdrawChance(ctx, WithdrawChanceRequest{Currency: "BTC", NetType: "BTC"})
	if err != nil || chance.MemberLevel.SecurityLevel != 4 || chance.MemberLevel.FeeLevel != 0 || !chance.MemberLevel.EmailVerified ||
		!chance.MemberLevel.IdentityAuthVerified || !chance.MemberLevel.BankAccountVerified || !chance.MemberLevel.TwoFactorAuthVerified ||
		chance.MemberLevel.Locked || chance.MemberLevel.WalletLocked || chance.Currency.Code != "BTC" ||
		chance.Currency.WithdrawFee != "0.0008" || !chance.Currency.IsCoin || chance.Currency.WalletState != "working" ||
		!slices.Equal(chance.Currency.WalletSupport, []string{"deposit", "withdraw"}) || chance.Account.Currency != "BTC" ||
		chance.Account.AverageBuyPrice != "145115000" || chance.Account.UnitCurrency != "KRW" ||
		chance.WithdrawLimit.Currency != "BTC" || chance.WithdrawLimit.Onetime != "50.0" || chance.WithdrawLimit.Daily != "100.0" ||
		chance.WithdrawLimit.RemainingDaily != "0.0" || chance.WithdrawLimit.RemainingDailyFiat != "5000000000.0" ||
		chance.WithdrawLimit.FiatCurrency != "KRW" || chance.WithdrawLimit.Minimum != "0.00001" || chance.WithdrawLimit.Fixed != 8 ||
		chance.WithdrawLimit.WithdrawDelayedFiat != "227.0" || !chance.WithdrawLimit.CanWithdraw ||
		chance.WithdrawLimit.RemainingDailyKRW != "4999999000.0" || len(chance.Raw) == 0 {
		t.Fatalf("WithdrawChance(BTC) = %+v, error = %v", chance, err)
	}
	krwChance, err := client.WithdrawChance(ctx, WithdrawChanceRequest{Currency: "KRW"})
	if err != nil || krwChance.Currency.Code != "KRW" || krwChance.Currency.IsCoin || krwChance.MemberLevel.TwoFactorAuthVerified ||
		!krwChance.MemberLevel.WalletLocked || krwChance.WithdrawLimit.Onetime != "" || krwChance.WithdrawLimit.RemainingDaily != "" ||
		krwChance.WithdrawLimit.FiatCurrency != "" || krwChance.WithdrawLimit.Minimum != "5000" || krwChance.WithdrawLimit.Fixed != 0 ||
		krwChance.WithdrawLimit.CanWithdraw {
		t.Fatalf("WithdrawChance(KRW) = %+v, error = %v", krwChance, err)
	}
	addresses, err := client.WithdrawalAddresses(ctx)
	if err != nil || len(addresses) != 2 || addresses[0].Currency != "BTC" || addresses[0].NetType != "BTC" ||
		addresses[0].NetworkName != "Bitcoin" || addresses[0].WithdrawAddress != "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa" ||
		addresses[0].BeneficiaryName != "John" || addresses[0].BeneficiaryType != "individual" || addresses[0].ExchangeName != "바이낸스" ||
		addresses[0].WalletType != "" || len(addresses[0].Raw) == 0 || addresses[1].WithdrawAddress != "0x1234615148db0926d76bde31d420abcd5439484fd" ||
		addresses[1].SecondaryAddress != "" || addresses[1].WalletType != "메타마스크" {
		t.Fatalf("WithdrawalAddresses() = %+v, error = %v", addresses, err)
	}
	withdrawal, err := client.WithdrawalInfo(ctx, WithdrawalInfoRequest{
		UUID: withdrawalUUID, TxID: "ebe6937b-130e-4066-8ac6-4b0e67f28adc", Currency: "BTC",
	})
	if err != nil || withdrawal.Type != "withdraw" || withdrawal.UUID != withdrawalUUID || withdrawal.Currency != "BTC" ||
		withdrawal.NetType != "BTC" || withdrawal.TxID != "ebe6937b-130e-4066-8ac6-4b0e67f28adc" || withdrawal.State != WithdrawalStateProcessing ||
		withdrawal.CreatedAt != "2018-04-13T11:24:01+09:00" || withdrawal.DoneAt != "" || withdrawal.Amount != "0.01" ||
		withdrawal.Fee != "0.0" || withdrawal.TransactionType != WithdrawTransactionDefault || withdrawal.IsCancelable || len(withdrawal.Raw) == 0 {
		t.Fatalf("WithdrawalInfo() = %+v, error = %v", withdrawal, err)
	}
	latest, err := client.WithdrawalInfo(ctx, WithdrawalInfoRequest{})
	if err != nil || latest.UUID != "latest-1" || latest.Currency != "KRW" || latest.NetType != "" || latest.TxID != "" ||
		latest.State != "done" || latest.DoneAt != "2025-07-01T15:00:30+09:00" || latest.Amount != "10000" || latest.IsCancelable {
		t.Fatalf("WithdrawalInfo(latest) = %+v, error = %v", latest, err)
	}
	withdrawals, err := client.Withdrawals(ctx, WithdrawalsRequest{
		Currency: "XRP", State: WithdrawalStateDone, UUIDs: []string{"uuid-1", "uuid-2"},
		Limit: 50, Page: 2, OrderBy: "asc", From: "uuid-from", To: "uuid-to",
	})
	if err != nil || len(withdrawals) != 2 || withdrawals[0].UUID != "uuid-1" || withdrawals[0].State != WithdrawalStateDone ||
		withdrawals[0].DoneAt != "2025-07-04T15:00:20+09:00" || withdrawals[0].Amount != "10.00" || len(withdrawals[0].Raw) == 0 ||
		withdrawals[1].TxID != "" || withdrawals[1].Fee != "0.1" || withdrawals[1].TransactionType != WithdrawTransactionInternal {
		t.Fatalf("Withdrawals() = %+v, error = %v", withdrawals, err)
	}
	byTxID, err := client.Withdrawals(ctx, WithdrawalsRequest{TxIDs: []string{"tx-1"}})
	if err != nil || len(byTxID) != 1 || byTxID[0].UUID != withdrawalUUID {
		t.Fatalf("Withdrawals(txids) = %+v, error = %v", byTxID, err)
	}
	empty, err := client.Withdrawals(ctx, WithdrawalsRequest{})
	if err != nil || len(empty) != 0 {
		t.Fatalf("Withdrawals(default) = %+v, error = %v", empty, err)
	}
	assertSecretsDestroyed(t, provider, 8)
	assertRateRule(t, limiter, "upbit:account:upbit-pocket:default:1second", 30, time.Second)
}

func TestWithdrawMutationEndpoints(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Remaining-Req", "group=default; min=1800; sec=29")
		body, _ := io.ReadAll(request.Body)
		switch request.Method + " " + request.URL.RequestURI() + " " + string(body) {
		case `POST /v1/withdraws/coin {"currency":"XRP","net_type":"XRP","amount":"10.00","address":"rN9qNpgnBaZwqCg8CvUZRPqCcPPY7wfWep","secondary_address":"3057887915","transaction_type":"default"}`:
			verifySignedBody(t, request, "currency=XRP&net_type=XRP&amount=10.00&address=rN9qNpgnBaZwqCg8CvUZRPqCcPPY7wfWep&secondary_address=3057887915&transaction_type=default")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"type":"withdraw","uuid":"`+withdrawalUUID+`","currency":"XRP","net_type":"XRP","txid":null,"state":"WAITING","created_at":"2025-07-04T15:00:00+09:00","done_at":null,"amount":"10.00","fee":"0.0","transaction_type":"default","is_cancelable":true}`)
		case `POST /v1/withdraws/coin {"currency":"BTC","net_type":"BTC","amount":"0.01","address":"1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"}`:
			verifySignedBody(t, request, "currency=BTC&net_type=BTC&amount=0.01&address=1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, withdrawalProcessingJSON)
		case `POST /v1/withdraws/coin {"currency":"ETH","net_type":"ETH","amount":"1","address":"0x1234615148db0926d76bde31d420abcd5439484fd","transaction_type":"internal"}`:
			verifySignedBody(t, request, "currency=ETH&net_type=ETH&amount=1&address=0x1234615148db0926d76bde31d420abcd5439484fd&transaction_type=internal")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"type":"withdraw","uuid":"internal-1","currency":"ETH","net_type":"ETH","txid":"internal-tx","state":"DONE","created_at":"2025-07-04T15:00:00+09:00","done_at":"2025-07-04T15:00:01+09:00","amount":"1","fee":"0","transaction_type":"internal","is_cancelable":false}`)
		case `POST /v1/withdraws/krw {"amount":"10000","two_factor_type":"kakao"}`:
			verifySignedBody(t, request, "amount=10000&two_factor_type=kakao")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"type":"withdraw","uuid":"`+withdrawalUUID+`","currency":"KRW","txid":"ebe6937b-130e-4066-8ac6-4b0e67f28adc","state":"processing","created_at":"2025-07-01T15:00:00+09:00","amount":"10000","fee":"0.0","transaction_type":"default","is_cancelable":false}`)
		case `POST /v1/withdraws/krw {"amount":"50000.5","two_factor_type":"hana"}`:
			verifySignedBody(t, request, "amount=50000.5&two_factor_type=hana")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"type":"withdraw","uuid":"krw-2","currency":"KRW","txid":null,"state":"waiting","created_at":"2025-07-01T15:05:00+09:00","amount":"50000.5","fee":"0","transaction_type":"default","is_cancelable":true}`)
		// DELETE에는 본문이 없으므로 요청 키 끝에 빈 본문이 붙는다.
		case "DELETE /v1/withdraws/coin?uuid=" + withdrawalUUID + " ":
			verifySignedQuery(t, request, "uuid="+withdrawalUUID)
			_, _ = io.WriteString(writer, `{"type":"withdraw","uuid":"`+withdrawalUUID+`","currency":"BTC","net_type":"BTC","state":"CANCELLED","created_at":"2025-07-01T15:00:00+09:00","amount":"0.01","fee":"0.0","transaction_type":"default","is_cancelable":true}`)
		default:
			t.Errorf("unexpected request %s %s %s", request.Method, request.URL.RequestURI(), body)
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, provider, limiter := newPrivateTestClient(t, server.URL, credential.PermissionWithdraw)
	ctx := context.Background()

	ripple, err := client.WithdrawCoin(ctx, CoinWithdrawRequest{
		Currency: "XRP", NetType: "XRP", Amount: "10.00", Address: "rN9qNpgnBaZwqCg8CvUZRPqCcPPY7wfWep",
		SecondaryAddress: "3057887915", TransactionType: WithdrawTransactionDefault,
	})
	if err != nil || ripple.UUID != withdrawalUUID || ripple.Currency != "XRP" || ripple.NetType != "XRP" || ripple.TxID != "" ||
		ripple.State != WithdrawalStateWaiting || ripple.DoneAt != "" || ripple.Amount != "10.00" || !ripple.IsCancelable || len(ripple.Raw) == 0 {
		t.Fatalf("WithdrawCoin(XRP) = %+v, error = %v", ripple, err)
	}
	bitcoin, err := client.WithdrawCoin(ctx, CoinWithdrawRequest{
		Currency: "BTC", NetType: "BTC", Amount: "0.01", Address: "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",
	})
	if err != nil || bitcoin.UUID != withdrawalUUID || bitcoin.State != WithdrawalStateProcessing || bitcoin.TxID != "ebe6937b-130e-4066-8ac6-4b0e67f28adc" {
		t.Fatalf("WithdrawCoin(BTC) = %+v, error = %v", bitcoin, err)
	}
	internal, err := client.WithdrawCoin(ctx, CoinWithdrawRequest{
		Currency: "ETH", NetType: "ETH", Amount: "1", Address: "0x1234615148db0926d76bde31d420abcd5439484fd",
		TransactionType: WithdrawTransactionInternal,
	})
	if err != nil || internal.UUID != "internal-1" || internal.State != WithdrawalStateDone || internal.TransactionType != WithdrawTransactionInternal {
		t.Fatalf("WithdrawCoin(internal) = %+v, error = %v", internal, err)
	}
	krw, err := client.WithdrawKRW(ctx, KRWWithdrawRequest{Amount: "10000", TwoFactorType: TwoFactorKakao})
	if err != nil || krw.UUID != withdrawalUUID || krw.Currency != "KRW" || krw.NetType != "" || krw.State != "processing" ||
		krw.Amount != "10000" || len(krw.Raw) == 0 {
		t.Fatalf("WithdrawKRW(kakao) = %+v, error = %v", krw, err)
	}
	krwHana, err := client.WithdrawKRW(ctx, KRWWithdrawRequest{Amount: "50000.5", TwoFactorType: TwoFactorHana})
	if err != nil || krwHana.UUID != "krw-2" || krwHana.TxID != "" || krwHana.State != "waiting" || !krwHana.IsCancelable {
		t.Fatalf("WithdrawKRW(hana) = %+v, error = %v", krwHana, err)
	}
	canceled, err := client.CancelWithdrawal(ctx, CancelWithdrawalRequest{UUID: withdrawalUUID})
	if err != nil || canceled.UUID != withdrawalUUID || canceled.State != WithdrawalStateCancelled || !canceled.IsCancelable ||
		canceled.Currency != "BTC" || len(canceled.Raw) == 0 {
		t.Fatalf("CancelWithdrawal() = %+v, error = %v", canceled, err)
	}
	assertSecretsDestroyed(t, provider, 6)
	assertRateRule(t, limiter, "upbit:account:upbit-pocket:default:1second", 30, time.Second)
}

func TestWithdrawErrorsAndMutationsAreNotRetried(t *testing.T) {
	t.Parallel()

	var mutations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.Method + " " + request.URL.Path {
		case "POST /v1/withdraws/coin":
			mutations.Add(1)
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(writer, `{"error":{"name":"internal_server_error","message":"temporary"}}`)
		case "POST /v1/withdraws/krw":
			mutations.Add(1)
			writer.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(writer, `{"error":{"name":"out_of_scope","message":"권한이 부족합니다."}}`)
		case "DELETE /v1/withdraws/coin":
			mutations.Add(1)
			writer.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(writer, `{"error":{"name":"withdraw_not_found","message":"출금 정보를 찾지 못했습니다."}}`)
		case "GET /v1/withdraws":
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(writer, `{"error":{"name":"invalid_query_payload","message":"Jwt의 query를 검증하는데 실패하였습니다."}}`)
		case "GET /v1/withdraw":
			writer.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(writer, `{"error":{"name":"withdraw_not_found","message":"출금 정보를 찾지 못했습니다."}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, _, _ := newPrivateTestClient(t, server.URL, credential.PermissionRead, credential.PermissionWithdraw)
	ctx := context.Background()

	_, err := client.WithdrawCoin(ctx, CoinWithdrawRequest{Currency: "BTC", NetType: "BTC", Amount: "0.01", Address: "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"})
	requireAPIError(t, err, http.StatusInternalServerError, "internal_server_error")
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("WithdrawCoin() error = %v, want ErrUnknownExecutionState", err)
	}
	_, err = client.WithdrawKRW(ctx, KRWWithdrawRequest{Amount: "10000", TwoFactorType: TwoFactorNaver})
	requireAPIError(t, err, http.StatusForbidden, "out_of_scope")
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("WithdrawKRW() error = %v, want ErrAuthorization from out_of_scope", err)
	}
	_, err = client.CancelWithdrawal(ctx, CancelWithdrawalRequest{UUID: withdrawalUUID})
	requireAPIError(t, err, http.StatusNotFound, "withdraw_not_found")
	if got := mutations.Load(); got != 3 {
		t.Fatalf("mutation requests = %d, want 3 without retry", got)
	}
	_, err = client.Withdrawals(ctx, WithdrawalsRequest{})
	requireAPIError(t, err, http.StatusUnauthorized, "invalid_query_payload")
	if !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("Withdrawals() error = %v, want ErrAuthentication", err)
	}
	_, err = client.WithdrawalInfo(ctx, WithdrawalInfoRequest{UUID: "missing"})
	requireAPIError(t, err, http.StatusNotFound, "withdraw_not_found")
}

func TestWithdrawMutationMalformedResponseIsUnknownExecutionState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, `<html>gateway</html>`)
	}))
	defer server.Close()

	client, _, _ := newPrivateTestClient(t, server.URL, credential.PermissionWithdraw)
	_, err := client.WithdrawCoin(context.Background(), CoinWithdrawRequest{
		Currency: "BTC", NetType: "BTC", Amount: "0.01", Address: "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",
	})
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("WithdrawCoin() error = %v, want ErrUnknownExecutionState for an unreadable acceptance", err)
	}
}

func TestWithdrawPermissionBoundaries(t *testing.T) {
	t.Parallel()

	reads := []callCase{
		{"WithdrawChance needs read", func(ctx context.Context, client *Client) error {
			_, err := client.WithdrawChance(ctx, WithdrawChanceRequest{Currency: "BTC", NetType: "BTC"})
			return err
		}},
		{"WithdrawalAddresses needs read", func(ctx context.Context, client *Client) error {
			_, err := client.WithdrawalAddresses(ctx)
			return err
		}},
		{"WithdrawalInfo needs read", func(ctx context.Context, client *Client) error {
			_, err := client.WithdrawalInfo(ctx, WithdrawalInfoRequest{})
			return err
		}},
		{"Withdrawals needs read", func(ctx context.Context, client *Client) error {
			_, err := client.Withdrawals(ctx, WithdrawalsRequest{})
			return err
		}},
	}
	assertRejectedBeforeSend(
		t, trade.ErrAuthorization, reads,
		credential.PermissionTrade, credential.PermissionTransfer, credential.PermissionWithdraw,
	)

	// 출금은 Trade와 Transfer 권한으로 대체할 수 없고 PermissionWithdraw를 명시해야 한다.
	mutations := []callCase{
		{"WithdrawCoin needs withdraw", func(ctx context.Context, client *Client) error {
			_, err := client.WithdrawCoin(ctx, CoinWithdrawRequest{
				Currency: "BTC", NetType: "BTC", Amount: "0.01", Address: "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",
			})
			return err
		}},
		{"WithdrawKRW needs withdraw", func(ctx context.Context, client *Client) error {
			_, err := client.WithdrawKRW(ctx, KRWWithdrawRequest{Amount: "10000", TwoFactorType: TwoFactorKakao})
			return err
		}},
		{"CancelWithdrawal needs withdraw", func(ctx context.Context, client *Client) error {
			_, err := client.CancelWithdrawal(ctx, CancelWithdrawalRequest{UUID: withdrawalUUID})
			return err
		}},
	}
	assertRejectedBeforeSend(
		t, trade.ErrAuthorization, mutations,
		credential.PermissionRead, credential.PermissionTrade, credential.PermissionTransfer,
	)
}

func TestWithdrawRequestsAreValidatedBeforeSend(t *testing.T) {
	t.Parallel()

	cases := []callCase{
		{"WithdrawChance lowercase currency", func(ctx context.Context, client *Client) error {
			_, err := client.WithdrawChance(ctx, WithdrawChanceRequest{Currency: "btc"})
			return err
		}},
		{"WithdrawCoin missing net type", func(ctx context.Context, client *Client) error {
			_, err := client.WithdrawCoin(ctx, CoinWithdrawRequest{Currency: "BTC", Amount: "0.01", Address: "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"})
			return err
		}},
		{"WithdrawCoin zero amount", func(ctx context.Context, client *Client) error {
			_, err := client.WithdrawCoin(ctx, CoinWithdrawRequest{Currency: "BTC", NetType: "BTC", Amount: "0", Address: "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"})
			return err
		}},
		{"WithdrawCoin address with whitespace", func(ctx context.Context, client *Client) error {
			_, err := client.WithdrawCoin(ctx, CoinWithdrawRequest{Currency: "BTC", NetType: "BTC", Amount: "0.01", Address: "1A1z P1eP"})
			return err
		}},
		{"WithdrawKRW unsupported two factor", func(ctx context.Context, client *Client) error {
			_, err := client.WithdrawKRW(ctx, KRWWithdrawRequest{Amount: "10000", TwoFactorType: "sms"})
			return err
		}},
		{"WithdrawalInfo whitespace uuid", func(ctx context.Context, client *Client) error {
			_, err := client.WithdrawalInfo(ctx, WithdrawalInfoRequest{UUID: "a b"})
			return err
		}},
		{"Withdrawals uuids and txids", func(ctx context.Context, client *Client) error {
			_, err := client.Withdrawals(ctx, WithdrawalsRequest{UUIDs: []string{"a"}, TxIDs: []string{"b"}})
			return err
		}},
		{"CancelWithdrawal missing uuid", func(ctx context.Context, client *Client) error {
			_, err := client.CancelWithdrawal(ctx, CancelWithdrawalRequest{})
			return err
		}},
	}
	assertRejectedBeforeSend(
		t, trade.ErrValidation, cases,
		credential.PermissionRead, credential.PermissionTrade, credential.PermissionTransfer, credential.PermissionWithdraw,
	)
}

func TestWithdrawRequestValidation(t *testing.T) {
	t.Parallel()

	address := "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"
	valid := []depositValidationCase{
		{"chance coin", WithdrawChanceRequest{Currency: "BTC", NetType: "BTC"}.validate()},
		{"chance krw", WithdrawChanceRequest{Currency: "KRW"}.validate()},
		{"coin", CoinWithdrawRequest{Currency: "BTC", NetType: "BTC", Amount: "0.01", Address: address}.validate()},
		{"coin with secondary address", CoinWithdrawRequest{Currency: "XRP", NetType: "XRP", Amount: "10", Address: "r", SecondaryAddress: "3057887915", TransactionType: WithdrawTransactionDefault}.validate()},
		{"coin internal", CoinWithdrawRequest{Currency: "ETH", NetType: "ETH", Amount: "1.5", Address: "0xabc", TransactionType: WithdrawTransactionInternal}.validate()},
		{"krw kakao", KRWWithdrawRequest{Amount: "10000", TwoFactorType: TwoFactorKakao}.validate()},
		{"krw naver", KRWWithdrawRequest{Amount: "10000.5", TwoFactorType: TwoFactorNaver}.validate()},
		{"krw hana", KRWWithdrawRequest{Amount: "5000", TwoFactorType: TwoFactorHana}.validate()},
		{"info latest", WithdrawalInfoRequest{}.validate()},
		{"info all", WithdrawalInfoRequest{UUID: "uuid", TxID: "txid", Currency: "BTC"}.validate()},
		{"list default", WithdrawalsRequest{}.validate()},
		{"list uuids maximum", WithdrawalsRequest{UUIDs: numberedValues("uuid-", 100), Limit: 100, Page: 1, OrderBy: "desc"}.validate()},
		{"list txids maximum", WithdrawalsRequest{Currency: "BTC", State: WithdrawalStateRejected, TxIDs: numberedValues("tx-", 100), From: "a", To: "b"}.validate()},
		{"cancel", CancelWithdrawalRequest{UUID: withdrawalUUID}.validate()},
	}
	invalid := []depositValidationCase{
		{"chance lowercase currency", WithdrawChanceRequest{Currency: "btc", NetType: "BTC"}.validate()},
		{"chance missing currency", WithdrawChanceRequest{NetType: "BTC"}.validate()},
		{"chance whitespace net type", WithdrawChanceRequest{Currency: "BTC", NetType: "B TC"}.validate()},
		{"coin lowercase currency", CoinWithdrawRequest{Currency: "btc", NetType: "BTC", Amount: "0.01", Address: address}.validate()},
		{"coin missing net type", CoinWithdrawRequest{Currency: "BTC", Amount: "0.01", Address: address}.validate()},
		{"coin zero amount", CoinWithdrawRequest{Currency: "BTC", NetType: "BTC", Amount: "0", Address: address}.validate()},
		{"coin scientific amount", CoinWithdrawRequest{Currency: "BTC", NetType: "BTC", Amount: "1e-3", Address: address}.validate()},
		{"coin missing address", CoinWithdrawRequest{Currency: "BTC", NetType: "BTC", Amount: "0.01"}.validate()},
		{"coin whitespace address", CoinWithdrawRequest{Currency: "BTC", NetType: "BTC", Amount: "0.01", Address: " " + address}.validate()},
		{"coin whitespace secondary address", CoinWithdrawRequest{Currency: "XRP", NetType: "XRP", Amount: "1", Address: "r", SecondaryAddress: "1 2"}.validate()},
		{"coin unsupported transaction type", CoinWithdrawRequest{Currency: "BTC", NetType: "BTC", Amount: "0.01", Address: address, TransactionType: "express"}.validate()},
		{"krw zero amount", KRWWithdrawRequest{Amount: "0", TwoFactorType: TwoFactorKakao}.validate()},
		{"krw grouped amount", KRWWithdrawRequest{Amount: "10,000", TwoFactorType: TwoFactorKakao}.validate()},
		{"krw missing two factor", KRWWithdrawRequest{Amount: "10000"}.validate()},
		{"krw unsupported two factor", KRWWithdrawRequest{Amount: "10000", TwoFactorType: "sms"}.validate()},
		{"info whitespace uuid", WithdrawalInfoRequest{UUID: " uuid"}.validate()},
		{"info whitespace txid", WithdrawalInfoRequest{TxID: "tx id"}.validate()},
		{"info lowercase currency", WithdrawalInfoRequest{Currency: "btc"}.validate()},
		{"list lowercase currency", WithdrawalsRequest{Currency: "btc"}.validate()},
		{"list lowercase state", WithdrawalsRequest{State: "done"}.validate()},
		{"list unsupported state", WithdrawalsRequest{State: "ACCEPTED"}.validate()},
		{"list uuids and txids", WithdrawalsRequest{UUIDs: []string{"a"}, TxIDs: []string{"b"}}.validate()},
		{"list uuids above 100", WithdrawalsRequest{UUIDs: numberedValues("uuid-", 101)}.validate()},
		{"list txids above 100", WithdrawalsRequest{TxIDs: numberedValues("tx-", 101)}.validate()},
		{"list duplicate uuid", WithdrawalsRequest{UUIDs: []string{"a", "a"}}.validate()},
		{"list limit above 100", WithdrawalsRequest{Limit: 101}.validate()},
		{"list negative limit", WithdrawalsRequest{Limit: -1}.validate()},
		{"list negative page", WithdrawalsRequest{Page: -1}.validate()},
		{"list unsupported order", WithdrawalsRequest{OrderBy: "up"}.validate()},
		{"list whitespace from cursor", WithdrawalsRequest{From: "uuid cursor"}.validate()},
		{"list whitespace to cursor", WithdrawalsRequest{To: "uuid\tcursor"}.validate()},
		{"cancel missing uuid", CancelWithdrawalRequest{}.validate()},
		{"cancel whitespace uuid", CancelWithdrawalRequest{UUID: strings.Repeat(" ", 2) + withdrawalUUID}.validate()},
	}
	assertValidationCases(t, valid, invalid)
}
