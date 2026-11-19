package bithumb

import (
	"context"
	"errors"
	"strings"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

const withdrawalListSample = `[{"type":"withdraw","uuid":"200347674","currency":"TRX","net_type":null,"txid":"20240425231724.50893",` +
	`"state":"DONE","created_at":"2024-04-25T23:19:28+09:00","done_at":"2024-04-25T23:19:28+09:00",` +
	`"amount":"99988545780.9056","fee":"0","transaction_type":null},` +
	`{"type":"withdraw","uuid":"200347279","currency":"TRX","net_type":null,"txid":"20240425182026.6693405",` +
	`"state":"DONE","created_at":"2024-04-25T18:22:11+09:00","done_at":"2024-04-25T18:22:11+09:00",` +
	`"amount":"10000000","fee":"0","transaction_type":null}]`

const krwWithdrawalListSample = `[{"type":"withdraw","uuid":"12703781","currency":"KRW","net_type":null,"txid":"1596146",` +
	`"state":"DONE","created_at":"2024-07-06T17:36:22+09:00","done_at":"2024-07-06T17:36:39+09:00",` +
	`"amount":"6000","fee":"1000","transaction_type":"default"}]`

const withdrawalInfoSample = `{"type":"withdraw","uuid":"200359853","currency":"MTL","net_type":"MTL_ETH",` +
	`"txid":"0x28d331ddca9fb3b5a413737b3062289db4995dfeaddb96c4d82abd591fe17a52","state":"DONE",` +
	`"created_at":"2024-06-28T15:13:10+09:00","done_at":"2024-06-28T15:17:17+09:00",` +
	`"amount":"0.6113","fee":"0.1","transaction_type":null}`

const withdrawChanceSample = `{"member_level":{"security_level":3,"fee_level":1,"email_verified":true,"identity_auth_verified":true,` +
	`"bank_account_verified":true,"two_factor_auth_verified":true,"locked":false,"wallet_locked":false},` +
	`"currency":{"code":"BTC","withdraw_fee":"0.000108","is_coin":true,"wallet_state":"working",` +
	`"wallet_support":["deposit","withdraw"],"withdraw_rate":null,"withdraw_fee_min":null,"withdraw_fee_max":null},` +
	`"account":{"currency":"BTC","balance":"124.45282908","locked":"0","avg_buy_price":"36341011",` +
	`"avg_buy_price_modified":false,"unit_currency":"KRW"},` +
	`"withdraw_limit":{"currency":"BTC","onetime":"6.01","daily":"160","remaining_daily":"160.00000000",` +
	`"remaining_daily_fiat":null,"fiat_currency":null,"minimum":"0.0001","fixed":8,"withdraw_delayed_fiat":null,` +
	`"can_withdraw":true,"remaining_daily_krw":null}}`

const withdrawAddressesSample = `[{"currency":"ETH","net_type":"ETH","network_name":"Ethereum",` +
	`"withdraw_address":"0x569ece3d6cd807a31b1a2d85ebfee79f89fe0b87","secondary_address":null,"exchange_name":"vv",` +
	`"owner_type":"personal","owner_ko_name":"홍길동","owner_en_name":null,"owner_corp_ko_name":null,"owner_corp_en_name":null},` +
	`{"currency":"XRP","net_type":"XRP","network_name":"Ripple",` +
	`"withdraw_address":"rs6qtDrs8qp5qLkUxuzQdxFh5eGrQbL3Ee","secondary_address":"1004775304","exchange_name":"Binance",` +
	`"owner_type":"corporation","owner_ko_name":"김대표","owner_en_name":"KIM DAE PYO",` +
	`"owner_corp_ko_name":"주식회사 예시","owner_corp_en_name":"EXAMPLE INC"}]`

const withdrawCoinSample = `{"type":"withdraw","uuid":"200377211","currency":"BTC","net_type":"BTC","state":"processing",` +
	`"created_at":"2024-07-14T14:54:24+09:00","done_at":null,"amount":"0.00010000","fee":"0","krw_amount":"8400",` +
	`"transaction_type":null,"txid":null}`

const withdrawCancelSample = `{"type":"withdraw","withdrawal_id":"200377211","currency":"BTC","net_type":"BTC","state":"PROCESSING",` +
	`"created_at":"2024-07-14T14:54:24+09:00","amount":"0.00010000","fee":"0","krw_amount":"8400"}`

const withdrawKRWSample = `{"type":"withdraw","uuid":"12704033","currency":"KRW","net_type":null,"txid":"1597452",` +
	`"state":"PROCESSING","created_at":"2024-07-14T15:05:20+09:00","done_at":null,"amount":"6000","fee":"1000",` +
	`"transaction_type":"default"}`

const travelRuleConsentSample = `{"error":{"name":"travel_rule_consent_required",` +
	`"message":"Consent required before withdrawal to Binance.",` +
	`"consent_url":"https://consent.example.com/travel-rule?token=secret-token","exchange_name":"Binance"}}`

func extWithdrawCoinRequest() WithdrawCoinRequest {
	return WithdrawCoinRequest{
		Currency: "BTC", NetType: "BTC", Amount: "0.0001", Address: "bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh",
		ExchangeName: "Binance", ReceiverType: ReceiverTypePersonal, ReceiverKoName: "홍길동", ReceiverEnName: "GIL DONG HONG",
	}
}

func TestWithdrawalListsAndLookups(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{
		extOK("GET /v1/withdraws", withdrawalListSample),
		extOK("GET /v1/withdraws/krw", krwWithdrawalListSample),
		extOK("GET /v1/withdraw", withdrawalInfoSample),
		extOK("GET /v1/withdraws/chance", withdrawChanceSample),
		extOK("GET /v1/withdraws/coin_addresses", withdrawAddressesSample),
	}, extReadOnly)
	ctx := context.Background()

	list, err := fixture.client.Withdrawals(ctx, WithdrawalsRequest{
		Currency: "TRX", State: WithdrawalStateDone, UUIDs: []string{"200347674", "200347279"},
		TxIDs: []string{"20240425231724.50893"}, Page: 2, Limit: 50, OrderBy: "asc",
	})
	if err != nil || len(list) != 2 || len(list[0].Raw) == 0 {
		t.Fatalf("Withdrawals() = %+v, error = %v", list, err)
	}
	first := list[0]
	if first.Type != "withdraw" || first.UUID != "200347674" || first.Currency != "TRX" || first.NetType != "" ||
		first.TxID != "20240425231724.50893" || first.State != WithdrawalStateDone ||
		first.CreatedAt != "2024-04-25T23:19:28+09:00" || first.DoneAt != "2024-04-25T23:19:28+09:00" ||
		first.Amount != "99988545780.9056" || first.Fee != "0" || first.TransactionType != "" || first.KRWAmount != "" {
		t.Fatalf("first withdrawal = %+v", first)
	}
	if _, err := fixture.client.Withdrawals(ctx, WithdrawalsRequest{}); err != nil {
		t.Fatalf("Withdrawals(empty) error = %v", err)
	}

	krwList, err := fixture.client.KRWWithdrawals(ctx, KRWWithdrawalsRequest{State: WithdrawalStateProcessing, Limit: 10})
	if err != nil || len(krwList) != 1 || krwList[0].Currency != "KRW" || krwList[0].Fee != "1000" ||
		krwList[0].TransactionType != "default" || krwList[0].UUID != "12703781" {
		t.Fatalf("KRWWithdrawals() = %+v, error = %v", krwList, err)
	}

	byUUID, err := fixture.client.WithdrawalInfo(ctx, WithdrawalInfoRequest{Currency: "MTL", UUID: "200359853"})
	if err != nil || byUUID.UUID != "200359853" || byUUID.NetType != "MTL_ETH" || byUUID.Amount != "0.6113" ||
		byUUID.Fee != "0.1" || byUUID.State != WithdrawalStateDone || len(byUUID.Raw) == 0 {
		t.Fatalf("WithdrawalInfo(uuid) = %+v, error = %v", byUUID, err)
	}
	txID := "0x28d331ddca9fb3b5a413737b3062289db4995dfeaddb96c4d82abd591fe17a52"
	byTxID, err := fixture.client.WithdrawalInfo(ctx, WithdrawalInfoRequest{Currency: "MTL", TxID: txID})
	if err != nil || byTxID.TxID != txID {
		t.Fatalf("WithdrawalInfo(txid) = %+v, error = %v", byTxID, err)
	}

	chance, err := fixture.client.WithdrawChance(ctx, WithdrawChanceRequest{Currency: "BTC", NetType: "BTC"})
	if err != nil || len(chance.Raw) == 0 {
		t.Fatalf("WithdrawChance() = %+v, error = %v", chance, err)
	}
	if chance.MemberLevel.SecurityLevel != 3 || chance.MemberLevel.FeeLevel != 1 || !chance.MemberLevel.EmailVerified ||
		!chance.MemberLevel.TwoFactorAuthVerified || chance.MemberLevel.Locked || chance.MemberLevel.WalletLocked {
		t.Fatalf("WithdrawChance() member level = %+v", chance.MemberLevel)
	}
	if chance.Currency.Code != "BTC" || chance.Currency.WithdrawFee != "0.000108" || chance.Currency.WithdrawRate != "" ||
		!chance.Currency.IsCoin || chance.Currency.WalletState != "working" || len(chance.Currency.WalletSupport) != 2 ||
		chance.Currency.WalletSupport[1] != "withdraw" {
		t.Fatalf("WithdrawChance() currency = %+v", chance.Currency)
	}
	if chance.Account.Balance != "124.45282908" || chance.Account.UnitCurrency != "KRW" ||
		chance.Limit.Onetime != "6.01" || chance.Limit.Daily != "160" || chance.Limit.RemainingDaily != "160.00000000" ||
		chance.Limit.RemainingDailyFiat != "" || chance.Limit.Minimum != "0.0001" || chance.Limit.Fixed != 8 ||
		!chance.Limit.CanWithdraw {
		t.Fatalf("WithdrawChance() account = %+v, limit = %+v", chance.Account, chance.Limit)
	}

	addresses, err := fixture.client.WithdrawAddresses(ctx)
	if err != nil || len(addresses) != 2 || len(addresses[0].Raw) == 0 {
		t.Fatalf("WithdrawAddresses() = %+v, error = %v", addresses, err)
	}
	personal, corporation := addresses[0], addresses[1]
	if personal.Currency != "ETH" || personal.NetType != "ETH" || personal.NetworkName != "Ethereum" ||
		personal.Address != "0x569ece3d6cd807a31b1a2d85ebfee79f89fe0b87" || personal.SecondaryAddress != "" ||
		personal.ExchangeName != "vv" || personal.OwnerType != "personal" || personal.OwnerKoName != "홍길동" ||
		personal.OwnerEnName != "" || personal.OwnerCorpKoName != "" {
		t.Fatalf("personal withdraw address = %+v", personal)
	}
	if corporation.SecondaryAddress != "1004775304" || corporation.OwnerType != "corporation" ||
		corporation.OwnerCorpKoName != "주식회사 예시" || corporation.OwnerCorpEnName != "EXAMPLE INC" {
		t.Fatalf("corporation withdraw address = %+v", corporation)
	}

	records := extRequests(t, fixture.server, 7)
	filtered := "currency=TRX&state=DONE&uuids[]=200347674&uuids[]=200347279&txids[]=20240425231724.50893&page=2&limit=50&order_by=asc"
	assertExtRequest(t, records[0], "GET", "/v1/withdraws", filtered, "", filtered)
	assertExtRequest(t, records[1], "GET", "/v1/withdraws", "", "", "")
	assertExtRequest(t, records[2], "GET", "/v1/withdraws/krw", "state=PROCESSING&limit=10", "", "state=PROCESSING&limit=10")
	assertExtRequest(t, records[3], "GET", "/v1/withdraw", "currency=MTL&uuid=200359853", "", "currency=MTL&uuid=200359853")
	assertExtRequest(t, records[4], "GET", "/v1/withdraw", "currency=MTL&txid="+txID, "", "currency=MTL&txid="+txID)
	assertExtRequest(
		t, records[5], "GET", "/v1/withdraws/chance", "currency=BTC&net_type=BTC", "", "currency=BTC&net_type=BTC",
	)
	assertExtRequest(t, records[6], "GET", "/v1/withdraws/coin_addresses", "", "", "")
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:private-other:1second", 140, 7)
}

func TestWithdrawCoin(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{
		extReplies("POST /v1/withdraws/coin", extStatus(201, withdrawCoinSample)),
	}, extWithdraw)
	withdrawal, err := fixture.client.WithdrawCoin(context.Background(), extWithdrawCoinRequest())
	if err != nil || withdrawal.UUID != "200377211" || withdrawal.Type != "withdraw" || withdrawal.Currency != "BTC" ||
		withdrawal.NetType != "BTC" || withdrawal.State != "processing" || withdrawal.DoneAt != "" ||
		withdrawal.Amount != "0.00010000" || withdrawal.Fee != "0" || withdrawal.KRWAmount != "8400" ||
		withdrawal.TxID != "" || len(withdrawal.Raw) == 0 {
		t.Fatalf("WithdrawCoin() = %+v, error = %v", withdrawal, err)
	}
	records := extRequests(t, fixture.server, 1)
	assertExtRequest(
		t, records[0], "POST", "/v1/withdraws/coin", "",
		`{"currency":"BTC","net_type":"BTC","amount":"0.0001","address":"bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh",`+
			`"exchange_name":"Binance","receiver_type":"personal","receiver_ko_name":"홍길동","receiver_en_name":"GIL DONG HONG"}`,
		"currency=BTC&net_type=BTC&amount=0.0001&address=bc1qxy2kgdygjrsqtzq2n0yrf2493p83kkfjhx0wlh&"+
			"exchange_name=Binance&receiver_type=personal&receiver_ko_name=홍길동&receiver_en_name=GIL DONG HONG",
	)
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:private-other:1second", 140, 1)
}

func TestWithdrawCoinForCorporationReceiver(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{
		extReplies("POST /v1/withdraws/coin", extStatus(201, withdrawCoinSample)),
	}, extWithdraw)
	_, err := fixture.client.WithdrawCoin(context.Background(), WithdrawCoinRequest{
		Currency: "XRP", NetType: "XRP", Amount: "10", Address: "rs6qtDrs8qp5qLkUxuzQdxFh5eGrQbL3Ee",
		SecondaryAddress: "1004775304", ReceiverType: ReceiverTypeCorporation, ReceiverKoName: "김대표",
		ReceiverEnName: "KIM DAE PYO", ReceiverCorpKoName: "주식회사 예시", ReceiverCorpEnName: "EXAMPLE INC",
	})
	if err != nil {
		t.Fatalf("WithdrawCoin() error = %v", err)
	}
	records := extRequests(t, fixture.server, 1)
	assertExtRequest(
		t, records[0], "POST", "/v1/withdraws/coin", "",
		`{"currency":"XRP","net_type":"XRP","amount":"10","address":"rs6qtDrs8qp5qLkUxuzQdxFh5eGrQbL3Ee",`+
			`"secondary_address":"1004775304","receiver_type":"corporation","receiver_ko_name":"김대표",`+
			`"receiver_en_name":"KIM DAE PYO","receiver_corp_ko_name":"주식회사 예시","receiver_corp_en_name":"EXAMPLE INC"}`,
		"currency=XRP&net_type=XRP&amount=10&address=rs6qtDrs8qp5qLkUxuzQdxFh5eGrQbL3Ee&secondary_address=1004775304&"+
			"receiver_type=corporation&receiver_ko_name=김대표&receiver_en_name=KIM DAE PYO&"+
			"receiver_corp_ko_name=주식회사 예시&receiver_corp_en_name=EXAMPLE INC",
	)
}

func TestWithdrawCoinReportsTravelRuleConsent(t *testing.T) {
	t.Parallel()

	otherUnprocessable := `{"error":{"name":"invalid_parameter","message":"amount is invalid"}}`
	fixture := newExtFixture(t, []extRoute{
		extReplies(
			"POST /v1/withdraws/coin",
			extStatus(422, travelRuleConsentSample),
			extStatus(422, strings.ReplaceAll(travelRuleConsentSample, "travel_rule_consent_required", "TRAVEL_RULE_CONSENT_REQUIRED")),
			extStatus(422, otherUnprocessable),
		),
	}, extWithdraw)
	ctx := context.Background()

	for _, attempt := range []string{"lowercase code", "uppercase code"} {
		_, err := fixture.client.WithdrawCoin(ctx, extWithdrawCoinRequest())
		var apiError *trade.APIError
		var consent *TravelRuleConsentError
		if !errors.Is(err, trade.ErrValidation) || !errors.As(err, &apiError) || !errors.As(err, &consent) {
			t.Fatalf("%s: WithdrawCoin() error = %v, want ErrValidation carrying TravelRuleConsentError", attempt, err)
		}
		if apiError.HTTPStatus != 422 || apiError.Retryable || !strings.EqualFold(apiError.ExchangeCode, "travel_rule_consent_required") {
			t.Fatalf("%s: APIError = %+v", attempt, apiError)
		}
		if consent.ExchangeName != "Binance" || consent.ConsentURL != "https://consent.example.com/travel-rule?token=secret-token" {
			t.Fatalf("%s: TravelRuleConsentError = %+v", attempt, consent)
		}
		if strings.Contains(err.Error(), "secret-token") || strings.Contains(consent.Error(), "secret-token") ||
			strings.Contains(consent.Error(), "consent.example.com") || !strings.Contains(consent.Error(), "Binance") {
			t.Fatalf("%s: error text leaks the consent URL or omits the exchange: %q / %q", attempt, err.Error(), consent.Error())
		}
	}

	_, err := fixture.client.WithdrawCoin(ctx, extWithdrawCoinRequest())
	var consent *TravelRuleConsentError
	if !errors.Is(err, trade.ErrValidation) || errors.As(err, &consent) {
		t.Fatalf("WithdrawCoin() other 422 error = %v, want plain ErrValidation without TravelRuleConsentError", err)
	}
	extRequests(t, fixture.server, 3)

	if message := (&TravelRuleConsentError{}).Error(); !strings.Contains(message, "travel rule") {
		t.Fatalf("TravelRuleConsentError without exchange = %q", message)
	}
}

func TestCancelWithdrawCoin(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{extOK("DELETE /v1/withdraws/coin", withdrawCancelSample)}, extWithdraw)
	cancellation, err := fixture.client.CancelWithdrawCoin(context.Background(), CancelWithdrawCoinRequest{
		WithdrawalID: "200377211",
	})
	if err != nil || cancellation.WithdrawalID != "200377211" || cancellation.Type != "withdraw" ||
		cancellation.Currency != "BTC" || cancellation.NetType != "BTC" || cancellation.State != WithdrawalStateProcessing ||
		cancellation.Amount != "0.00010000" || cancellation.Fee != "0" || cancellation.KRWAmount != "8400" ||
		cancellation.CreatedAt != "2024-07-14T14:54:24+09:00" || len(cancellation.Raw) == 0 {
		t.Fatalf("CancelWithdrawCoin() = %+v, error = %v", cancellation, err)
	}
	records := extRequests(t, fixture.server, 1)
	assertExtRequest(
		t, records[0], "DELETE", "/v1/withdraws/coin", "withdrawal_id=200377211", "", "withdrawal_id=200377211",
	)
}

func TestWithdrawKRW(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{
		extReplies("POST /v1/withdraws/krw", extStatus(201, withdrawKRWSample)),
	}, extWithdraw)
	withdrawal, err := fixture.client.WithdrawKRW(context.Background(), WithdrawKRWRequest{
		Amount: "6000", TwoFactorType: TwoFactorKakao,
	})
	if err != nil || withdrawal.UUID != "12704033" || withdrawal.Currency != "KRW" || withdrawal.NetType != "" ||
		withdrawal.TxID != "1597452" || withdrawal.State != WithdrawalStateProcessing || withdrawal.DoneAt != "" ||
		withdrawal.Amount != "6000" || withdrawal.Fee != "1000" || withdrawal.TransactionType != "default" ||
		len(withdrawal.Raw) == 0 {
		t.Fatalf("WithdrawKRW() = %+v, error = %v", withdrawal, err)
	}
	records := extRequests(t, fixture.server, 1)
	assertExtRequest(
		t, records[0], "POST", "/v1/withdraws/krw", "",
		`{"amount":"6000","two_factor_type":"kakao"}`, "amount=6000&two_factor_type=kakao",
	)
}

func TestWithdrawalMutationsRequireWithdrawPermission(t *testing.T) {
	t.Parallel()

	for name, permissions := range map[string][]credential.Permission{
		"read":     extReadOnly,
		"trade":    extTrader,
		"transfer": extTransfer,
	} {
		fixture := newExtFixture(t, []extRoute{
			extReplies("POST /v1/withdraws/coin", extStatus(201, withdrawCoinSample)),
			extOK("DELETE /v1/withdraws/coin", withdrawCancelSample),
			extReplies("POST /v1/withdraws/krw", extStatus(201, withdrawKRWSample)),
		}, permissions)
		ctx := context.Background()
		_, coinErr := fixture.client.WithdrawCoin(ctx, extWithdrawCoinRequest())
		_, cancelErr := fixture.client.CancelWithdrawCoin(ctx, CancelWithdrawCoinRequest{WithdrawalID: "200377211"})
		_, krwErr := fixture.client.WithdrawKRW(ctx, WithdrawKRWRequest{Amount: "6000", TwoFactorType: TwoFactorKakao})
		for operation, err := range map[string]error{"coin": coinErr, "cancel": cancelErr, "krw": krwErr} {
			if !errors.Is(err, trade.ErrAuthorization) || !errors.Is(err, credential.ErrPermissionNotAllowed) {
				t.Errorf("%s permission, %s: error = %v, want ErrAuthorization from missing withdraw permission", name, operation, err)
			}
		}
		if calls, _, _ := fixture.provider.snapshot(); calls != 0 {
			t.Errorf("%s permission: credential provider calls = %d, want 0", name, calls)
		}
		extRequests(t, fixture.server, 0)
	}
}

func TestWithdrawalMutationsAreNeverRetried(t *testing.T) {
	t.Parallel()

	serverError := `{"error":{"name":"internal_error","message":"boom"}}`
	fixture := newExtFixture(t, []extRoute{
		extReplies("POST /v1/withdraws/coin", extStatus(500, serverError)),
		extReplies("DELETE /v1/withdraws/coin", extStatus(503, serverError)),
		extReplies("POST /v1/withdraws/krw", extStatus(502, serverError)),
	}, extWithdraw)
	ctx := context.Background()
	if _, err := fixture.client.WithdrawCoin(ctx, extWithdrawCoinRequest()); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("WithdrawCoin() 5xx error = %v, want ErrUnknownExecutionState", err)
	}
	if _, err := fixture.client.CancelWithdrawCoin(ctx, CancelWithdrawCoinRequest{WithdrawalID: "200377211"}); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CancelWithdrawCoin() 5xx error = %v, want ErrUnknownExecutionState", err)
	}
	if _, err := fixture.client.WithdrawKRW(ctx, WithdrawKRWRequest{Amount: "6000", TwoFactorType: TwoFactorKakao}); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("WithdrawKRW() 5xx error = %v, want ErrUnknownExecutionState", err)
	}
	extRequests(t, fixture.server, 3)

	networkClient, _, _ := newExtClient(t, "http://127.0.0.1", errorSender{}, extWithdraw)
	if _, err := networkClient.WithdrawCoin(ctx, extWithdrawCoinRequest()); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("WithdrawCoin() network error = %v, want ErrUnknownExecutionState", err)
	}
	malformed := newExtFixture(t, []extRoute{extReplies("POST /v1/withdraws/coin", extStatus(201, `{invalid`))}, extWithdraw)
	if _, err := malformed.client.WithdrawCoin(ctx, extWithdrawCoinRequest()); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("WithdrawCoin() malformed body error = %v, want ErrUnknownExecutionState", err)
	}
	extRequests(t, malformed.server, 1)
}

func TestWithdrawRequestsRejectInvalidInput(t *testing.T) {
	t.Parallel()

	coin := extWithdrawCoinRequest()
	changeCoin := func(mutate func(*WithdrawCoinRequest)) WithdrawCoinRequest {
		request := coin
		mutate(&request)
		return request
	}
	corporation := func(mutate func(*WithdrawCoinRequest)) WithdrawCoinRequest {
		return changeCoin(func(request *WithdrawCoinRequest) {
			request.ReceiverType = ReceiverTypeCorporation
			request.ReceiverCorpKoName = "주식회사 예시"
			request.ReceiverCorpEnName = "EXAMPLE INC"
			mutate(request)
		})
	}

	invalid := []struct {
		name string
		err  error
	}{
		{"coin missing currency", changeCoin(func(r *WithdrawCoinRequest) { r.Currency = "" }).validate()},
		{"coin missing net type", changeCoin(func(r *WithdrawCoinRequest) { r.NetType = "" }).validate()},
		{"coin currency with ampersand", changeCoin(func(r *WithdrawCoinRequest) { r.Currency = "BTC&x=1" }).validate()},
		{"coin zero amount", changeCoin(func(r *WithdrawCoinRequest) { r.Amount = "0" }).validate()},
		{"coin negative amount", changeCoin(func(r *WithdrawCoinRequest) { r.Amount = "-1" }).validate()},
		{"coin non numeric amount", changeCoin(func(r *WithdrawCoinRequest) { r.Amount = "abc" }).validate()},
		{"coin missing address", changeCoin(func(r *WithdrawCoinRequest) { r.Address = "" }).validate()},
		{"coin padded address", changeCoin(func(r *WithdrawCoinRequest) { r.Address = " bc1qxy2 " }).validate()},
		{"coin address with ampersand", changeCoin(func(r *WithdrawCoinRequest) { r.Address = "bc1q&amount=9" }).validate()},
		{"coin secondary address with ampersand", changeCoin(func(r *WithdrawCoinRequest) { r.SecondaryAddress = "1&2" }).validate()},
		{"coin padded exchange name", changeCoin(func(r *WithdrawCoinRequest) { r.ExchangeName = "Binance " }).validate()},
		{"coin padded receiver name", changeCoin(func(r *WithdrawCoinRequest) { r.ReceiverKoName = " 홍길동" }).validate()},
		{"coin unknown receiver type", changeCoin(func(r *WithdrawCoinRequest) { r.ReceiverType = "company" }).validate()},
		{"corporation without corporate names", changeCoin(func(r *WithdrawCoinRequest) { r.ReceiverType = ReceiverTypeCorporation }).validate()},
		{"corporation without English corporate name", corporation(func(r *WithdrawCoinRequest) { r.ReceiverCorpEnName = "" }).validate()},
		{"corporation without Korean corporate name", corporation(func(r *WithdrawCoinRequest) { r.ReceiverCorpKoName = "" }).validate()},
		{"withdrawals lowercase state", (WithdrawalsRequest{State: "done"}).validate()},
		{"withdrawals padded currency", (WithdrawalsRequest{Currency: " BTC"}).validate()},
		{"withdrawals UUID with ampersand", (WithdrawalsRequest{UUIDs: []string{"1&2"}}).validate()},
		{"withdrawals empty TxID", (WithdrawalsRequest{TxIDs: []string{""}}).validate()},
		{"withdrawals negative page", (WithdrawalsRequest{Page: -1}).validate()},
		{"withdrawals limit above maximum", (WithdrawalsRequest{Limit: 101}).validate()},
		{"withdrawals invalid order by", (WithdrawalsRequest{OrderBy: "up"}).validate()},
		{"KRW withdrawals unknown state", (KRWWithdrawalsRequest{State: "FAILED"}).validate()},
		{"KRW withdrawals negative limit", (KRWWithdrawalsRequest{Limit: -1}).validate()},
		{"info without currency", (WithdrawalInfoRequest{UUID: "1"}).validate()},
		{"info without identity", (WithdrawalInfoRequest{Currency: "BTC"}).validate()},
		{"info with both identities", (WithdrawalInfoRequest{Currency: "BTC", UUID: "1", TxID: "2"}).validate()},
		{"info UUID with ampersand", (WithdrawalInfoRequest{Currency: "BTC", UUID: "1&2"}).validate()},
		{"chance without currency", (WithdrawChanceRequest{NetType: "BTC"}).validate()},
		{"chance without net type", (WithdrawChanceRequest{Currency: "BTC"}).validate()},
		{"cancel without ID", (CancelWithdrawCoinRequest{}).validate()},
		{"cancel ID with ampersand", (CancelWithdrawCoinRequest{WithdrawalID: "1&2"}).validate()},
		{"KRW withdraw zero amount", (WithdrawKRWRequest{Amount: "0", TwoFactorType: TwoFactorKakao}).validate()},
		{"KRW withdraw missing two factor", (WithdrawKRWRequest{Amount: "6000"}).validate()},
		{"KRW withdraw uppercase two factor", (WithdrawKRWRequest{Amount: "6000", TwoFactorType: "Kakao"}).validate()},
		{"KRW withdraw punctuated two factor", (WithdrawKRWRequest{Amount: "6000", TwoFactorType: "kakao&x=1"}).validate()},
	}
	for _, test := range invalid {
		if !errors.Is(test.err, trade.ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", test.name, test.err)
		}
	}

	minimal := WithdrawCoinRequest{Currency: "BTC", NetType: "BTC", Amount: "1", Address: "addr"}
	accepted := []error{
		coin.validate(),
		minimal.validate(),
		corporation(func(r *WithdrawCoinRequest) {}).validate(),
		(WithdrawalsRequest{}).validate(),
		(WithdrawalsRequest{Currency: "BTC", State: WithdrawalStateCancelled, Page: 1, Limit: 100, OrderBy: "desc"}).validate(),
		(KRWWithdrawalsRequest{State: WithdrawalStateProcessing, UUIDs: []string{"1"}, TxIDs: []string{"2"}}).validate(),
		(WithdrawalInfoRequest{Currency: "BTC", TxID: "0xabc"}).validate(),
		(WithdrawChanceRequest{Currency: "BTC", NetType: "BTC"}).validate(),
		(CancelWithdrawCoinRequest{WithdrawalID: "200377211"}).validate(),
		(WithdrawKRWRequest{Amount: "6000.5", TwoFactorType: TwoFactorKakao}).validate(),
	}
	for index, err := range accepted {
		if err != nil {
			t.Errorf("accepted withdraw request %d error = %v", index, err)
		}
	}
}

func TestWithdrawRequestsRejectInvalidInputBeforeSending(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, nil, extWithdraw)
	ctx := context.Background()
	if _, err := fixture.client.WithdrawCoin(ctx, WithdrawCoinRequest{Currency: "BTC", NetType: "BTC", Amount: "1"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("WithdrawCoin() error = %v, want ErrValidation", err)
	}
	if _, err := fixture.client.WithdrawKRW(ctx, WithdrawKRWRequest{Amount: "6000"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("WithdrawKRW() error = %v, want ErrValidation", err)
	}
	if _, err := fixture.client.CancelWithdrawCoin(ctx, CancelWithdrawCoinRequest{}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("CancelWithdrawCoin() error = %v, want ErrValidation", err)
	}
	if _, err := fixture.client.Withdrawals(ctx, WithdrawalsRequest{Limit: 1000}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("Withdrawals() error = %v, want ErrValidation", err)
	}
	if _, err := fixture.client.WithdrawalInfo(ctx, WithdrawalInfoRequest{Currency: "BTC"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("WithdrawalInfo() error = %v, want ErrValidation", err)
	}
	extRequests(t, fixture.server, 0)
	if calls, _, _ := fixture.provider.snapshot(); calls != 0 {
		t.Fatalf("credential provider calls = %d, want 0", calls)
	}
}
