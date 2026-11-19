package bithumb

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// travelRuleConsentRequiredCode는 트래블룰 개인정보 처리 동의가 필요할 때 거래소가 돌려주는 오류 코드다.
const travelRuleConsentRequiredCode = "travel_rule_consent_required"

// twoFactorTypePattern은 원화 입출금 요청의 2차 인증 수단 토큰 형식이다.
var twoFactorTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// TwoFactorType은 원화 입출금 요청에 쓰는 2차 인증 수단이다.
type TwoFactorType string

// TwoFactorKakao는 카카오 인증이다. 공식 문서가 안내하는 수단이다.
const TwoFactorKakao TwoFactorType = "kakao"

// ReceiverType은 출금 수취인 유형이다.
type ReceiverType string

const (
	ReceiverTypePersonal    ReceiverType = "personal"
	ReceiverTypeCorporation ReceiverType = "corporation"
)

// WithdrawalState는 출금 진행 상태다.
type WithdrawalState string

const (
	WithdrawalStateProcessing WithdrawalState = "PROCESSING"
	WithdrawalStateDone       WithdrawalState = "DONE"
	WithdrawalStateCancelled  WithdrawalState = "CANCELLED"
)

// TravelRuleConsentError는 출금 대상 거래소에 대한 트래블룰 개인정보 처리 동의가 필요함을 나타낸다.
// 422 travel_rule_consent_required 응답에서 만들어지며 trade.APIError의 Cause로 전달되므로
// errors.As로 꺼낼 수 있다. 사용자는 ConsentURL에서 동의를 마친 뒤 출금을 다시 요청해야 한다.
// ConsentURL은 로그에 남지 않도록 Error 문자열에 포함하지 않는다.
type TravelRuleConsentError struct {
	ExchangeName string
	ConsentURL   string
}

// Error는 동의 URL을 제외한 안내 문구를 반환한다.
func (consent *TravelRuleConsentError) Error() string {
	if consent == nil || consent.ExchangeName == "" {
		return "Bithumb travel rule consent is required"
	}
	return "Bithumb travel rule consent is required for " + consent.ExchangeName
}

// WithdrawalsRequest는 코인 출금 목록 조회 조건이다.
// Page는 1부터 시작하며 0이면 거래소 기본값을 쓴다. Limit은 최대 100이다.
type WithdrawalsRequest struct {
	Currency string
	State    WithdrawalState
	UUIDs    []string
	TxIDs    []string
	Page     int
	Limit    int
	OrderBy  string
}

// KRWWithdrawalsRequest는 원화 출금 목록 조회 조건이다.
type KRWWithdrawalsRequest struct {
	State   WithdrawalState
	UUIDs   []string
	TxIDs   []string
	Page    int
	Limit   int
	OrderBy string
}

// WithdrawalInfoRequest는 UUID 또는 TxID로 출금 한 건을 조회하는 조건이다.
// Currency는 필수이며 UUID와 TxID 중 정확히 하나만 지정한다.
type WithdrawalInfoRequest struct {
	Currency string
	UUID     string
	TxID     string
}

// WithdrawChanceRequest는 통화와 네트워크별 출금 가능 정보 조회 조건이다.
type WithdrawChanceRequest struct {
	Currency string
	NetType  string
}

// WithdrawCoinRequest는 가상자산 출금 요청이다.
// 출금 주소는 거래소에 등록된 출금 가능 주소여야 하며 출금 유형에 따라 필요한 수취인 정보가 다르다.
// ReceiverType이 corporation이면 ReceiverCorpKoName과 ReceiverCorpEnName이 필수다.
type WithdrawCoinRequest struct {
	Currency           string
	NetType            string
	Amount             string
	Address            string
	SecondaryAddress   string
	ExchangeName       string
	ReceiverType       ReceiverType
	ReceiverKoName     string
	ReceiverEnName     string
	ReceiverCorpKoName string
	ReceiverCorpEnName string
}

// CancelWithdrawCoinRequest는 처리 중인 가상자산 출금의 취소 조건이다.
type CancelWithdrawCoinRequest struct {
	WithdrawalID string
}

// WithdrawKRWRequest는 등록된 출금 계좌로의 원화 출금 요청이다.
type WithdrawKRWRequest struct {
	Amount        string
	TwoFactorType TwoFactorType
}

// Withdrawal은 코인 또는 원화 출금 한 건이다. 새 출금 요청 응답에는 KRWAmount가 포함될 수 있다.
type Withdrawal struct {
	Type            string          `json:"type"`
	UUID            string          `json:"uuid"`
	Currency        string          `json:"currency"`
	NetType         string          `json:"net_type"`
	TxID            string          `json:"txid"`
	State           WithdrawalState `json:"state"`
	CreatedAt       string          `json:"created_at"`
	DoneAt          string          `json:"done_at"`
	Amount          Decimal         `json:"amount"`
	Fee             Decimal         `json:"fee"`
	KRWAmount       Decimal         `json:"krw_amount"`
	TransactionType string          `json:"transaction_type"`
	Raw             json.RawMessage `json:"-"`
}

// WithdrawalCancellation은 가상자산 출금 취소 응답이다.
type WithdrawalCancellation struct {
	Type         string          `json:"type"`
	WithdrawalID string          `json:"withdrawal_id"`
	Currency     string          `json:"currency"`
	NetType      string          `json:"net_type"`
	State        WithdrawalState `json:"state"`
	CreatedAt    string          `json:"created_at"`
	Amount       Decimal         `json:"amount"`
	Fee          Decimal         `json:"fee"`
	KRWAmount    Decimal         `json:"krw_amount"`
	Raw          json.RawMessage `json:"-"`
}

// WithdrawMemberLevel은 출금 가능 정보에 포함된 회원의 보안·인증 상태다.
// 거래소가 값을 주지 않으면 0 또는 false이며 원본은 WithdrawChance.Raw에서 확인한다.
type WithdrawMemberLevel struct {
	SecurityLevel         int  `json:"security_level"`
	FeeLevel              int  `json:"fee_level"`
	EmailVerified         bool `json:"email_verified"`
	IdentityAuthVerified  bool `json:"identity_auth_verified"`
	BankAccountVerified   bool `json:"bank_account_verified"`
	TwoFactorAuthVerified bool `json:"two_factor_auth_verified"`
	Locked                bool `json:"locked"`
	WalletLocked          bool `json:"wallet_locked"`
}

// WithdrawCurrency는 출금 가능 정보에 포함된 화폐의 출금 수수료와 지갑 상태다.
// 수수료는 금액 기준(WithdrawFee)이거나 정률 기준(WithdrawRate, 선택적으로 최소·최대 한도)이다.
type WithdrawCurrency struct {
	Code           string   `json:"code"`
	WithdrawFee    Decimal  `json:"withdraw_fee"`
	WithdrawRate   Decimal  `json:"withdraw_rate"`
	WithdrawFeeMax Decimal  `json:"withdraw_fee_max"`
	WithdrawFeeMin Decimal  `json:"withdraw_fee_min"`
	IsCoin         bool     `json:"is_coin"`
	WalletState    string   `json:"wallet_state"`
	WalletSupport  []string `json:"wallet_support"`
}

// WithdrawLimit은 출금 가능 정보에 포함된 출금 한도와 가능 여부다.
type WithdrawLimit struct {
	Currency            string  `json:"currency"`
	Onetime             Decimal `json:"onetime"`
	Daily               Decimal `json:"daily"`
	RemainingDaily      Decimal `json:"remaining_daily"`
	RemainingDailyFiat  Decimal `json:"remaining_daily_fiat"`
	FiatCurrency        string  `json:"fiat_currency"`
	Minimum             Decimal `json:"minimum"`
	Fixed               int     `json:"fixed"`
	WithdrawDelayedFiat Decimal `json:"withdraw_delayed_fiat"`
	CanWithdraw         bool    `json:"can_withdraw"`
	RemainingDailyKRW   Decimal `json:"remaining_daily_krw"`
}

// WithdrawChance는 통화와 네트워크별 출금 가능 정보다.
type WithdrawChance struct {
	MemberLevel WithdrawMemberLevel `json:"member_level"`
	Currency    WithdrawCurrency    `json:"currency"`
	Account     Balance             `json:"account"`
	Limit       WithdrawLimit       `json:"withdraw_limit"`
	Raw         json.RawMessage     `json:"-"`
}

// WithdrawAddress는 출금 허용 주소로 등록된 주소 한 건이다.
// OwnerType은 personal 또는 corporation이다.
type WithdrawAddress struct {
	Currency         string          `json:"currency"`
	NetType          string          `json:"net_type"`
	NetworkName      string          `json:"network_name"`
	Address          string          `json:"withdraw_address"`
	SecondaryAddress string          `json:"secondary_address"`
	ExchangeName     string          `json:"exchange_name"`
	OwnerType        string          `json:"owner_type"`
	OwnerKoName      string          `json:"owner_ko_name"`
	OwnerEnName      string          `json:"owner_en_name"`
	OwnerCorpKoName  string          `json:"owner_corp_ko_name"`
	OwnerCorpEnName  string          `json:"owner_corp_en_name"`
	Raw              json.RawMessage `json:"-"`
}

// Withdrawals는 코인 출금 목록을 조회한다.
func (client *Client) Withdrawals(
	ctx context.Context,
	request WithdrawalsRequest,
	options ...trade.RequestOption,
) ([]Withdrawal, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.listWithdrawals(ctx, "/v1/withdraws", request.parameters(), options...)
}

// KRWWithdrawals는 원화 출금 목록을 조회한다.
func (client *Client) KRWWithdrawals(
	ctx context.Context,
	request KRWWithdrawalsRequest,
	options ...trade.RequestOption,
) ([]Withdrawal, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.listWithdrawals(ctx, "/v1/withdraws/krw", request.parameters(), options...)
}

func (client *Client) listWithdrawals(
	ctx context.Context,
	path string,
	params parameters,
	options ...trade.RequestOption,
) ([]Withdrawal, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, path, params, nil, rateLimitPrivateOther,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *Withdrawal, raw []byte) {
		item.Raw = raw
	})
}

// WithdrawalInfo는 UUID 또는 TxID로 출금 한 건을 조회한다.
func (client *Client) WithdrawalInfo(
	ctx context.Context,
	request WithdrawalInfoRequest,
	options ...trade.RequestOption,
) (Withdrawal, error) {
	if err := request.validate(); err != nil {
		return Withdrawal{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/withdraw", request.parameters(), nil, rateLimitPrivateOther,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Withdrawal{}, err
	}
	var result Withdrawal
	if err := client.decodeResponse(response, commonexchange.OperationRead, &result); err != nil {
		return Withdrawal{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// WithdrawChance는 통화와 네트워크별 출금 수수료, 한도, 지갑 상태, 계좌 잔고를 조회한다.
func (client *Client) WithdrawChance(
	ctx context.Context,
	request WithdrawChanceRequest,
	options ...trade.RequestOption,
) (WithdrawChance, error) {
	if err := request.validate(); err != nil {
		return WithdrawChance{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/withdraws/chance", request.parameters(), nil, rateLimitPrivateOther,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return WithdrawChance{}, err
	}
	var result WithdrawChance
	if err := client.decodeResponse(response, commonexchange.OperationRead, &result); err != nil {
		return WithdrawChance{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// WithdrawAddresses는 출금 허용 주소로 등록된 주소 목록을 조회한다.
func (client *Client) WithdrawAddresses(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]WithdrawAddress, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/withdraws/coin_addresses", nil, nil, rateLimitPrivateOther,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *WithdrawAddress, raw []byte) {
		item.Raw = raw
	})
}

// WithdrawCoin은 가상자산 출금을 요청한다. credential.PermissionWithdraw가 있어야 한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환하므로
// 중복 출금을 막으려면 WithdrawalInfo나 Withdrawals로 접수 여부를 확인한 뒤에만 다시 요청해야 한다.
// 트래블룰 동의가 필요하면 오류의 Cause로 *TravelRuleConsentError가 전달된다.
func (client *Client) WithdrawCoin(
	ctx context.Context,
	request WithdrawCoinRequest,
	options ...trade.RequestOption,
) (Withdrawal, error) {
	if err := request.validate(); err != nil {
		return Withdrawal{}, err
	}
	params := request.parameters()
	body, err := encodeBody(params)
	if err != nil {
		return Withdrawal{}, validationError("encode coin withdrawal body: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v1/withdraws/coin", params, body, rateLimitPrivateOther,
		credential.PermissionWithdraw, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Withdrawal{}, err
	}
	var result Withdrawal
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &result); err != nil {
		return Withdrawal{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// CancelWithdrawCoin은 처리 중(PROCESSING)인 가상자산 출금을 취소한다. credential.PermissionWithdraw가 있어야 한다.
// 이미 완료되거나 취소된 출금은 취소할 수 없다. 자동 재시도하지 않는다.
func (client *Client) CancelWithdrawCoin(
	ctx context.Context,
	request CancelWithdrawCoinRequest,
	options ...trade.RequestOption,
) (WithdrawalCancellation, error) {
	if err := request.validate(); err != nil {
		return WithdrawalCancellation{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, "/v1/withdraws/coin", request.parameters(), nil, rateLimitPrivateOther,
		credential.PermissionWithdraw, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return WithdrawalCancellation{}, err
	}
	var result WithdrawalCancellation
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &result); err != nil {
		return WithdrawalCancellation{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// WithdrawKRW는 등록된 출금 계좌로 원화 출금을 요청한다. credential.PermissionWithdraw가 있어야 한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) WithdrawKRW(
	ctx context.Context,
	request WithdrawKRWRequest,
	options ...trade.RequestOption,
) (Withdrawal, error) {
	if err := request.validate(); err != nil {
		return Withdrawal{}, err
	}
	params := request.parameters()
	body, err := encodeBody(params)
	if err != nil {
		return Withdrawal{}, validationError("encode KRW withdrawal body: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v1/withdraws/krw", params, body, rateLimitPrivateOther,
		credential.PermissionWithdraw, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Withdrawal{}, err
	}
	var result Withdrawal
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &result); err != nil {
		return Withdrawal{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

func (request WithdrawalsRequest) validate() error {
	if request.Currency != "" {
		if err := validateIdentifier("currency", request.Currency); err != nil {
			return err
		}
	}
	if err := validateWithdrawalFilters(request.State, request.UUIDs, request.TxIDs); err != nil {
		return err
	}
	return validateListPaging(request.Page, request.Limit, request.OrderBy)
}

func (request WithdrawalsRequest) parameters() parameters {
	return transferListParameters(
		request.Currency, string(request.State), request.UUIDs, request.TxIDs,
		request.Page, request.Limit, request.OrderBy,
	)
}

func (request KRWWithdrawalsRequest) validate() error {
	if err := validateWithdrawalFilters(request.State, request.UUIDs, request.TxIDs); err != nil {
		return err
	}
	return validateListPaging(request.Page, request.Limit, request.OrderBy)
}

func (request KRWWithdrawalsRequest) parameters() parameters {
	return transferListParameters(
		"", string(request.State), request.UUIDs, request.TxIDs,
		request.Page, request.Limit, request.OrderBy,
	)
}

func (request WithdrawalInfoRequest) validate() error {
	if err := validateIdentifier("currency", request.Currency); err != nil {
		return err
	}
	return validateTransferIdentity(request.UUID, request.TxID)
}

func (request WithdrawalInfoRequest) parameters() parameters {
	values := parameters{}
	values.add("currency", request.Currency)
	values.add("uuid", request.UUID)
	values.add("txid", request.TxID)
	return values
}

func (request WithdrawChanceRequest) validate() error {
	return validateCurrencyNetwork(request.Currency, request.NetType)
}

func (request WithdrawChanceRequest) parameters() parameters {
	return parameters{
		{key: "currency", value: request.Currency},
		{key: "net_type", value: request.NetType},
	}
}

func (request WithdrawCoinRequest) validate() error {
	if err := validateCurrencyNetwork(request.Currency, request.NetType); err != nil {
		return err
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return err
	}
	for _, err := range []error{
		validateText("address", request.Address, true),
		validateText("secondaryAddress", request.SecondaryAddress, false),
		validateText("exchangeName", request.ExchangeName, false),
		validateText("receiverKoName", request.ReceiverKoName, false),
		validateText("receiverEnName", request.ReceiverEnName, false),
		validateText("receiverCorpKoName", request.ReceiverCorpKoName, false),
		validateText("receiverCorpEnName", request.ReceiverCorpEnName, false),
	} {
		if err != nil {
			return err
		}
	}
	switch request.ReceiverType {
	case "", ReceiverTypePersonal:
		return nil
	case ReceiverTypeCorporation:
		if request.ReceiverCorpKoName == "" || request.ReceiverCorpEnName == "" {
			return validationError("corporation receiver requires receiverCorpKoName and receiverCorpEnName")
		}
		return nil
	default:
		return validationError("receiverType must be personal or corporation")
	}
}

func (request WithdrawCoinRequest) parameters() parameters {
	values := parameters{}
	values.add("currency", request.Currency)
	values.add("net_type", request.NetType)
	values.add("amount", request.Amount)
	values.add("address", request.Address)
	values.add("secondary_address", request.SecondaryAddress)
	values.add("exchange_name", request.ExchangeName)
	values.add("receiver_type", string(request.ReceiverType))
	values.add("receiver_ko_name", request.ReceiverKoName)
	values.add("receiver_en_name", request.ReceiverEnName)
	values.add("receiver_corp_ko_name", request.ReceiverCorpKoName)
	values.add("receiver_corp_en_name", request.ReceiverCorpEnName)
	return values
}

func (request CancelWithdrawCoinRequest) validate() error {
	return validateIdentifier("withdrawalID", request.WithdrawalID)
}

func (request CancelWithdrawCoinRequest) parameters() parameters {
	return parameters{{key: "withdrawal_id", value: request.WithdrawalID}}
}

func (request WithdrawKRWRequest) validate() error {
	return validateKRWTransfer(request.Amount, request.TwoFactorType)
}

func (request WithdrawKRWRequest) parameters() parameters {
	return parameters{
		{key: "amount", value: request.Amount},
		{key: "two_factor_type", value: string(request.TwoFactorType)},
	}
}

// validateWithdrawalFilters는 출금 목록 조회의 상태와 UUID·TxID 필터를 검증한다.
func validateWithdrawalFilters(state WithdrawalState, uuids, txids []string) error {
	switch state {
	case "", WithdrawalStateProcessing, WithdrawalStateDone, WithdrawalStateCancelled:
	default:
		return validationError("withdrawal state must be PROCESSING, DONE, or CANCELLED")
	}
	if err := validateIDList("uuids", uuids, 0, nil); err != nil {
		return err
	}
	return validateIDList("txids", txids, 0, nil)
}

// validateKRWTransfer는 원화 출금·입금 요청의 금액과 2차 인증 수단을 검증한다.
func validateKRWTransfer(amount string, twoFactorType TwoFactorType) error {
	if err := validatePositiveDecimal("amount", amount); err != nil {
		return err
	}
	if !twoFactorTypePattern.MatchString(string(twoFactorType)) {
		return validationError("invalid twoFactorType")
	}
	return nil
}
