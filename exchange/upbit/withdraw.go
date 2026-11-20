package upbit

import (
	"context"
	"encoding/json"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	// maxWithdrawalLimit은 출금·입금 목록 조회의 limit 상한이다.
	maxWithdrawalLimit = 100
	// maxWithdrawalLookup은 출금 목록 조회가 한 번에 받는 UUID 또는 txid 최대 개수다.
	maxWithdrawalLookup = 100
)

// WithdrawTransactionType은 출금 유형이다.
type WithdrawTransactionType string

const (
	// WithdrawTransactionDefault는 일반 출금이다.
	WithdrawTransactionDefault WithdrawTransactionType = "default"
	// WithdrawTransactionInternal은 바로 출금(업비트 계정 간 출금)이다.
	WithdrawTransactionInternal WithdrawTransactionType = "internal"
)

// TwoFactorType은 원화 입출금에 쓰는 2차 인증 수단이다.
type TwoFactorType string

const (
	TwoFactorKakao TwoFactorType = "kakao"
	TwoFactorNaver TwoFactorType = "naver"
	TwoFactorHana  TwoFactorType = "hana"
)

// WithdrawalState는 출금 처리 상태다. 원화 출금 응답은 소문자로 반환될 수 있다.
type WithdrawalState string

const (
	WithdrawalStateWaiting    WithdrawalState = "WAITING"
	WithdrawalStateProcessing WithdrawalState = "PROCESSING"
	WithdrawalStateDone       WithdrawalState = "DONE"
	WithdrawalStateFailed     WithdrawalState = "FAILED"
	WithdrawalStateCancelled  WithdrawalState = "CANCELLED"
	WithdrawalStateRejected   WithdrawalState = "REJECTED"
)

// WithdrawChanceRequest는 출금 가능 정보를 조회할 통화다.
type WithdrawChanceRequest struct {
	Currency string
	// NetType은 디지털 자산 출금 네트워크 식별자이며 디지털 자산이면 필수다.
	NetType string
}

// WithdrawMemberLevel은 사용자 보안 등급과 출금에 영향을 주는 인증·보호 상태다.
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

// WithdrawCurrency는 통화의 출금 수수료와 입출금 지원 정보다.
// WalletSupport에 deposit, withdraw가 있으면 해당 입출금이 현재 가능하다.
type WithdrawCurrency struct {
	Code          string   `json:"code"`
	WithdrawFee   Decimal  `json:"withdraw_fee"`
	IsCoin        bool     `json:"is_coin"`
	WalletState   string   `json:"wallet_state"`
	WalletSupport []string `json:"wallet_support"`
}

// WithdrawLimit은 출금 제약 조건이다. Onetime, Daily, RemainingDaily, RemainingDailyKRW는 업비트가 더 이상 쓰지 않는 필드다.
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

// WithdrawChance는 통화의 출금 정책과 사용자 잔고다.
type WithdrawChance struct {
	MemberLevel   WithdrawMemberLevel `json:"member_level"`
	Currency      WithdrawCurrency    `json:"currency"`
	Account       Balance             `json:"account"`
	WithdrawLimit WithdrawLimit       `json:"withdraw_limit"`
	Raw           json.RawMessage     `json:"-"`
}

// WithdrawalAddress는 출금 허용 주소로 등록된 주소 한 건이다.
// 수취 지갑이 개인 지갑이면 BeneficiaryName 등 거래소 정보는 비어 있을 수 있다.
type WithdrawalAddress struct {
	Currency               string          `json:"currency"`
	NetType                string          `json:"net_type"`
	NetworkName            string          `json:"network_name"`
	WithdrawAddress        string          `json:"withdraw_address"`
	SecondaryAddress       string          `json:"secondary_address"`
	BeneficiaryName        string          `json:"beneficiary_name"`
	BeneficiaryCompanyName string          `json:"beneficiary_company_name"`
	BeneficiaryType        string          `json:"beneficiary_type"`
	ExchangeName           string          `json:"exchange_name"`
	WalletType             string          `json:"wallet_type"`
	Raw                    json.RawMessage `json:"-"`
}

// Withdrawal은 디지털 자산 또는 원화 출금 한 건이다.
type Withdrawal struct {
	Type            string                  `json:"type"`
	UUID            string                  `json:"uuid"`
	Currency        string                  `json:"currency"`
	NetType         string                  `json:"net_type"`
	TxID            string                  `json:"txid"`
	State           WithdrawalState         `json:"state"`
	CreatedAt       string                  `json:"created_at"`
	DoneAt          string                  `json:"done_at"`
	Amount          Decimal                 `json:"amount"`
	Fee             Decimal                 `json:"fee"`
	TransactionType WithdrawTransactionType `json:"transaction_type"`
	IsCancelable    bool                    `json:"is_cancelable"`
	Raw             json.RawMessage         `json:"-"`
}

// CoinWithdrawRequest는 디지털 자산 출금 요청이다.
// Address는 출금 허용 주소 목록에 등록된 주소여야 하며 NetType은 그 주소의 net_type을 써야 한다.
type CoinWithdrawRequest struct {
	Currency string
	NetType  string
	Amount   string
	Address  string
	// SecondaryAddress는 Destination Tag, Memo처럼 2차 주소가 필요한 자산에서 반드시 지정한다.
	SecondaryAddress string
	// TransactionType을 비우면 일반 출금(default)이다.
	TransactionType WithdrawTransactionType
}

// KRWWithdrawRequest는 원화 출금 요청이다.
type KRWWithdrawRequest struct {
	Amount        string
	TwoFactorType TwoFactorType
}

// WithdrawalInfoRequest는 출금 한 건을 찾는 조건이다. 모두 비우면 최신 출금을 반환한다.
type WithdrawalInfoRequest struct {
	UUID     string
	TxID     string
	Currency string
}

// WithdrawalsRequest는 출금 목록 조회 조건이다. UUIDs와 TxIDs는 함께 쓸 수 없고 각각 최대 100개다.
type WithdrawalsRequest struct {
	Currency string
	State    WithdrawalState
	UUIDs    []string
	TxIDs    []string
	// Limit을 비우면 100이며 최대 100이다.
	Limit int
	// Page를 비우면 1이다.
	Page int
	// OrderBy는 asc 또는 desc이며 비우면 desc다.
	OrderBy string
	// From과 To는 응답의 uuid를 쓰는 페이지 이동 커서다.
	From string
	To   string
}

// CancelWithdrawalRequest는 취소할 디지털 자산 출금의 UUID다.
type CancelWithdrawalRequest struct {
	UUID string
}

// WithdrawChance는 통화의 출금 가능 정보를 조회한다. 업비트 API Key에 출금조회 권한이 필요하다.
func (client *Client) WithdrawChance(
	ctx context.Context,
	request WithdrawChanceRequest,
	options ...trade.RequestOption,
) (WithdrawChance, error) {
	if err := request.validate(); err != nil {
		return WithdrawChance{}, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/withdraws/chance", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return WithdrawChance{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead, func(item *WithdrawChance, raw []byte) {
		item.Raw = raw
	})
}

// WithdrawalAddresses는 등록된 출금 허용 주소 목록을 조회한다. 업비트 API Key에 출금조회 권한이 필요하다.
func (client *Client) WithdrawalAddresses(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]WithdrawalAddress, error) {
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/withdraws/coin_addresses", nil, nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *WithdrawalAddress, raw []byte) {
		item.Raw = raw
	})
}

// WithdrawCoin은 디지털 자산 출금을 요청한다. 업비트 API Key에 출금하기 권한이 필요하다.
// 자산이 거래소 밖으로 나가므로 자동 재시도하지 않으며 전송 결과가 불명확하면 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) WithdrawCoin(
	ctx context.Context,
	request CoinWithdrawRequest,
	options ...trade.RequestOption,
) (Withdrawal, error) {
	if err := request.validate(); err != nil {
		return Withdrawal{}, err
	}
	response, err := client.executeSignedPost(
		ctx, "/v1/withdraws/coin", request.parameters(), defaultRate, credential.PermissionWithdraw, options...,
	)
	if err != nil {
		return Withdrawal{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation, func(item *Withdrawal, raw []byte) {
		item.Raw = raw
	})
}

// WithdrawKRW는 원화 출금을 요청한다. 업비트 API Key에 출금하기 권한이 필요하며 2차 인증 앱 승인이 필요하다.
// 자동 재시도하지 않으며 전송 결과가 불명확하면 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) WithdrawKRW(
	ctx context.Context,
	request KRWWithdrawRequest,
	options ...trade.RequestOption,
) (Withdrawal, error) {
	if err := request.validate(); err != nil {
		return Withdrawal{}, err
	}
	response, err := client.executeSignedPost(
		ctx, "/v1/withdraws/krw", request.parameters(), defaultRate, credential.PermissionWithdraw, options...,
	)
	if err != nil {
		return Withdrawal{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation, func(item *Withdrawal, raw []byte) {
		item.Raw = raw
	})
}

// WithdrawalInfo는 UUID 또는 txid로 출금 한 건을 조회한다. 업비트 API Key에 출금조회 권한이 필요하다.
func (client *Client) WithdrawalInfo(
	ctx context.Context,
	request WithdrawalInfoRequest,
	options ...trade.RequestOption,
) (Withdrawal, error) {
	if err := request.validate(); err != nil {
		return Withdrawal{}, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/withdraw", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Withdrawal{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead, func(item *Withdrawal, raw []byte) {
		item.Raw = raw
	})
}

// Withdrawals는 출금 목록을 조회한다. 업비트 API Key에 출금조회 권한이 필요하다.
func (client *Client) Withdrawals(
	ctx context.Context,
	request WithdrawalsRequest,
	options ...trade.RequestOption,
) ([]Withdrawal, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/withdraws", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *Withdrawal, raw []byte) {
		item.Raw = raw
	})
}

// CancelWithdrawal은 디지털 자산 출금 취소를 요청한다. 업비트 API Key에 출금하기 권한이 필요하다.
// 취소 가능 여부는 출금의 IsCancelable로 확인한다.
func (client *Client) CancelWithdrawal(
	ctx context.Context,
	request CancelWithdrawalRequest,
	options ...trade.RequestOption,
) (Withdrawal, error) {
	if err := request.validate(); err != nil {
		return Withdrawal{}, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodDelete, "/v1/withdraws/coin", request.parameters(), nil, defaultRate,
		credential.PermissionWithdraw, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Withdrawal{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation, func(item *Withdrawal, raw []byte) {
		item.Raw = raw
	})
}

func (request WithdrawChanceRequest) validate() error {
	if err := validateCurrencyCode("currency", request.Currency); err != nil {
		return err
	}
	return validateOptionalRequestValue("net type", request.NetType)
}

func (request WithdrawChanceRequest) parameters() parameters {
	values := parameters{{key: "currency", value: request.Currency}}
	values.add("net_type", request.NetType)
	return values
}

func (request CoinWithdrawRequest) validate() error {
	if err := validateCurrencyCode("currency", request.Currency); err != nil {
		return err
	}
	if err := validateRequestValue("net type", request.NetType); err != nil {
		return err
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return err
	}
	if err := validateRequestValue("address", request.Address); err != nil {
		return err
	}
	if err := validateOptionalRequestValue("secondary address", request.SecondaryAddress); err != nil {
		return err
	}
	switch request.TransactionType {
	case "", WithdrawTransactionDefault, WithdrawTransactionInternal:
		return nil
	default:
		return validationError("transaction type must be default or internal")
	}
}

func (request CoinWithdrawRequest) parameters() parameters {
	values := parameters{}
	values.add("currency", request.Currency)
	values.add("net_type", request.NetType)
	values.add("amount", request.Amount)
	values.add("address", request.Address)
	values.add("secondary_address", request.SecondaryAddress)
	values.add("transaction_type", string(request.TransactionType))
	return values
}

func (request KRWWithdrawRequest) validate() error {
	return validateKRWTransfer(request.Amount, request.TwoFactorType)
}

func (request KRWWithdrawRequest) parameters() parameters {
	return parameters{
		{key: "amount", value: request.Amount},
		{key: "two_factor_type", value: string(request.TwoFactorType)},
	}
}

func (request WithdrawalInfoRequest) validate() error {
	if err := validateOptionalRequestValue("withdrawal UUID", request.UUID); err != nil {
		return err
	}
	if err := validateOptionalRequestValue("withdrawal txid", request.TxID); err != nil {
		return err
	}
	if request.Currency != "" {
		return validateCurrencyCode("currency", request.Currency)
	}
	return nil
}

func (request WithdrawalInfoRequest) parameters() parameters {
	values := parameters{}
	values.add("uuid", request.UUID)
	values.add("txid", request.TxID)
	values.add("currency", request.Currency)
	return values
}

func (request WithdrawalsRequest) validate() error {
	if request.Currency != "" {
		if err := validateCurrencyCode("currency", request.Currency); err != nil {
			return err
		}
	}
	switch request.State {
	case "", WithdrawalStateWaiting, WithdrawalStateProcessing, WithdrawalStateDone,
		WithdrawalStateFailed, WithdrawalStateCancelled, WithdrawalStateRejected:
	default:
		return validationError("unsupported withdrawal state %q", request.State)
	}
	if len(request.UUIDs) > 0 && len(request.TxIDs) > 0 {
		return validationError("withdrawal UUIDs and txids cannot be used together")
	}
	if err := validateOptionalValueList("withdrawal UUIDs", request.UUIDs, maxWithdrawalLookup); err != nil {
		return err
	}
	if err := validateOptionalValueList("withdrawal txids", request.TxIDs, maxWithdrawalLookup); err != nil {
		return err
	}
	return validateListPaging("withdrawal", request.Limit, request.Page, request.OrderBy, request.From, request.To)
}

func (request WithdrawalsRequest) parameters() parameters {
	values := parameters{}
	values.add("currency", request.Currency)
	values.add("state", string(request.State))
	values.addAll("uuids[]", request.UUIDs)
	values.addAll("txids[]", request.TxIDs)
	values.addInt("limit", request.Limit)
	values.addInt("page", request.Page)
	values.add("order_by", request.OrderBy)
	values.add("from", request.From)
	values.add("to", request.To)
	return values
}

func (request CancelWithdrawalRequest) validate() error {
	return validateRequestValue("withdrawal UUID", request.UUID)
}

func (request CancelWithdrawalRequest) parameters() parameters {
	return parameters{{key: "uuid", value: request.UUID}}
}

// validateKRWTransfer는 원화 입금·출금 요청의 금액과 2차 인증 수단을 검사한다.
func validateKRWTransfer(amount string, twoFactorType TwoFactorType) error {
	if err := validatePositiveDecimal("amount", amount); err != nil {
		return err
	}
	switch twoFactorType {
	case TwoFactorKakao, TwoFactorNaver, TwoFactorHana:
		return nil
	default:
		return validationError("two factor type must be kakao, naver, or hana")
	}
}

// validateListPaging은 입금·출금 목록 조회의 limit, page, 정렬과 페이지 이동 커서를 검사한다.
func validateListPaging(name string, limit, page int, orderBy, from, to string) error {
	if err := validatePageLimit(name, limit, maxWithdrawalLimit); err != nil {
		return err
	}
	if page < 0 {
		return validationError("%s page cannot be negative", name)
	}
	if err := validateOrderBy(orderBy); err != nil {
		return err
	}
	if err := validateOptionalRequestValue(name+" from cursor", from); err != nil {
		return err
	}
	return validateOptionalRequestValue(name+" to cursor", to)
}
