package cryptocom

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	methodGetDepositAddress       = "private/get-deposit-address"
	methodGetDepositHistory       = "private/get-deposit-history"
	methodGetWithdrawalHistory    = "private/get-withdrawal-history"
	methodGetCurrencyNetworks     = "private/get-currency-networks"
	methodGetWithdrawalAddresses  = "private/get-withdrawal-addresses"
	methodCreateWithdrawal        = "private/create-withdrawal"
	maximumWalletPageSize         = 200
	maximumWithdrawalClientIDSize = 64
)

// DepositStatus는 입금 상태 코드다. 0 미도착, 1 도착, 2 실패, 3 대기.
type DepositStatus string

const (
	DepositNotArrived DepositStatus = "0"
	DepositArrived    DepositStatus = "1"
	DepositFailed     DepositStatus = "2"
	DepositPending    DepositStatus = "3"
)

// WithdrawalStatus는 출금 상태 코드다. 0 대기, 1 처리 중, 2 거절, 3 지급 진행, 4 지급 실패, 5 완료, 6 취소.
type WithdrawalStatus string

const (
	WithdrawalPending           WithdrawalStatus = "0"
	WithdrawalProcessing        WithdrawalStatus = "1"
	WithdrawalRejected          WithdrawalStatus = "2"
	WithdrawalPaymentInProgress WithdrawalStatus = "3"
	WithdrawalPaymentFailed     WithdrawalStatus = "4"
	WithdrawalCompleted         WithdrawalStatus = "5"
	WithdrawalCancelled         WithdrawalStatus = "6"
)

// DepositAddressRequest는 입금 주소를 조회할 통화를 지정한다.
type DepositAddressRequest struct {
	Currency string
}

// DepositAddress는 통화·네트워크별 입금 주소 한 건이다. 태그가 있으면 주소 뒤에 붙어 반환된다.
type DepositAddress struct {
	ID         Scalar          `json:"id"`
	Currency   string          `json:"currency"`
	Network    string          `json:"network"`
	Address    string          `json:"address"`
	CreateTime Scalar          `json:"create_time"`
	Status     string          `json:"status"`
	Raw        json.RawMessage `json:"-"`
}

// DepositHistoryRequest는 입금 이력의 통화·상태·시간 범위·page를 지정한다.
type DepositHistoryRequest struct {
	Currency string
	Status   DepositStatus
	Start    *time.Time
	End      *time.Time
	PageSize int
	Page     int
}

// Deposit은 입금 이력 한 건이다.
type Deposit struct {
	ID         Scalar          `json:"id"`
	Currency   string          `json:"currency"`
	Amount     Decimal         `json:"amount"`
	Fee        Decimal         `json:"fee"`
	Address    string          `json:"address"`
	CreateTime Scalar          `json:"create_time"`
	UpdateTime Scalar          `json:"update_time"`
	Status     DepositStatus   `json:"status"`
	Raw        json.RawMessage `json:"-"`
}

// WithdrawalHistoryRequest는 출금 이력의 통화·상태·시간 범위·page를 지정한다.
type WithdrawalHistoryRequest struct {
	Currency string
	Status   WithdrawalStatus
	Start    *time.Time
	End      *time.Time
	PageSize int
	Page     int
}

// Withdrawal은 출금 이력 한 건이다.
type Withdrawal struct {
	ID               Scalar           `json:"id"`
	ClientWithdrawID string           `json:"client_wid"`
	Currency         string           `json:"currency"`
	Amount           Decimal          `json:"amount"`
	Fee              Decimal          `json:"fee"`
	Address          string           `json:"address"`
	CreateTime       Scalar           `json:"create_time"`
	UpdateTime       Scalar           `json:"update_time"`
	Status           WithdrawalStatus `json:"status"`
	TransactionID    string           `json:"txid"`
	NetworkID        string           `json:"network_id"`
	Raw              json.RawMessage  `json:"-"`
}

// CreateWithdrawalRequest는 화이트리스트 주소로의 출금 요청이다.
type CreateWithdrawalRequest struct {
	Currency         string
	Amount           string
	Address          string
	AddressTag       string
	NetworkID        string
	ClientWithdrawID string
}

// WithdrawalReceipt는 출금 접수 결과다. 접수는 최종 지급을 뜻하지 않는다.
type WithdrawalReceipt struct {
	ID               Scalar          `json:"id"`
	ClientWithdrawID string          `json:"client_wid"`
	Currency         string          `json:"currency"`
	Symbol           string          `json:"symbol"`
	Amount           Decimal         `json:"amount"`
	Fee              Decimal         `json:"fee"`
	Address          string          `json:"address"`
	CreateTime       Scalar          `json:"create_time"`
	NetworkID        string          `json:"network_id"`
	Raw              json.RawMessage `json:"-"`
}

// CurrencyNetwork는 통화의 네트워크별 입출금 설정이다.
type CurrencyNetwork struct {
	NetworkID            string          `json:"network_id"`
	WithdrawalFee        Decimal         `json:"withdrawal_fee"`
	WithdrawEnabled      bool            `json:"withdraw_enabled"`
	MinWithdrawalAmount  Decimal         `json:"min_withdrawal_amount"`
	DepositEnabled       bool            `json:"deposit_enabled"`
	ConfirmationRequired Integer         `json:"confirmation_required"`
	Raw                  json.RawMessage `json:"-"`
}

// CurrencyNetworkInfo는 통화 하나의 전체 이름·기본 네트워크·네트워크 목록이다.
type CurrencyNetworkInfo struct {
	FullName       string            `json:"full_name"`
	DefaultNetwork string            `json:"default_network"`
	Networks       []CurrencyNetwork `json:"network_list"`
}

// CurrencyNetworks는 통화 코드별 네트워크 매핑과 갱신 시각이다.
type CurrencyNetworks struct {
	UpdateTime Scalar                         `json:"update_time"`
	Currencies map[string]CurrencyNetworkInfo `json:"currency_map"`
	Raw        json.RawMessage                `json:"-"`
}

// WithdrawalAddressesRequest는 저장된 화이트리스트 출금 주소 필터와 page를 지정한다.
type WithdrawalAddressesRequest struct {
	Currency  string
	NetworkID string
	PageSize  int
	Page      int
}

// WithdrawalAddress는 화이트리스트 출금 주소 한 건이다.
type WithdrawalAddress struct {
	ID               Scalar          `json:"id"`
	Currency         string          `json:"currency"`
	NetworkID        string          `json:"network_id"`
	Address          string          `json:"address"`
	Label            string          `json:"label"`
	CreateTime       Scalar          `json:"create_time"`
	IsOwnerOfAddress bool            `json:"is_owner_of_address"`
	WalletType       string          `json:"wallet_type"`
	Raw              json.RawMessage `json:"-"`
}

func (status DepositStatus) valid() bool {
	switch status {
	case DepositNotArrived, DepositArrived, DepositFailed, DepositPending:
		return true
	default:
		return false
	}
}

func (status WithdrawalStatus) valid() bool {
	switch status {
	case WithdrawalPending, WithdrawalProcessing, WithdrawalRejected, WithdrawalPaymentInProgress,
		WithdrawalPaymentFailed, WithdrawalCompleted, WithdrawalCancelled:
		return true
	default:
		return false
	}
}

func validateCurrency(value string) error {
	if !currencyPattern.MatchString(value) {
		return validationError("invalid Crypto.com currency %q", value)
	}
	return nil
}

func validateOptionalCurrency(value string) error {
	if value == "" {
		return nil
	}
	return validateCurrency(value)
}

func validateNetworkID(value string) error {
	if value != "" && !networkIDPattern.MatchString(value) {
		return validationError("invalid Crypto.com network ID %q", value)
	}
	return nil
}

func validateWalletPage(start, end *time.Time, pageSize, page int) error {
	if err := validateTimeRange(start, end); err != nil {
		return err
	}
	if pageSize < 0 || pageSize > maximumWalletPageSize {
		return validationError("wallet page size must be between 1 and %d or zero", maximumWalletPageSize)
	}
	if page < 0 {
		return validationError("wallet page cannot be negative")
	}
	return nil
}

func (request DepositAddressRequest) validate() error {
	return validateCurrency(request.Currency)
}

func (request DepositAddressRequest) params() map[string]any {
	return map[string]any{"currency": request.Currency}
}

func (request DepositHistoryRequest) validate() error {
	if err := validateOptionalCurrency(request.Currency); err != nil {
		return err
	}
	if request.Status != "" && !request.Status.valid() {
		return validationError("unsupported Crypto.com deposit status %q", request.Status)
	}
	return validateWalletPage(request.Start, request.End, request.PageSize, request.Page)
}

func (request DepositHistoryRequest) params() map[string]any {
	params := make(map[string]any)
	setStringParam(params, "currency", request.Currency)
	setStringParam(params, "status", string(request.Status))
	setPageParams(params, request.Start, request.End, request.PageSize, request.Page)
	return params
}

func (request WithdrawalHistoryRequest) validate() error {
	if err := validateOptionalCurrency(request.Currency); err != nil {
		return err
	}
	if request.Status != "" && !request.Status.valid() {
		return validationError("unsupported Crypto.com withdrawal status %q", request.Status)
	}
	return validateWalletPage(request.Start, request.End, request.PageSize, request.Page)
}

func (request WithdrawalHistoryRequest) params() map[string]any {
	params := make(map[string]any)
	setStringParam(params, "currency", request.Currency)
	setStringParam(params, "status", string(request.Status))
	setPageParams(params, request.Start, request.End, request.PageSize, request.Page)
	return params
}

func (request CreateWithdrawalRequest) validate() error {
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	if err := validatePositiveDecimal("withdrawal amount", request.Amount); err != nil {
		return err
	}
	if err := validateOptionalText("withdrawal address", request.Address, 512); err != nil || request.Address == "" {
		if err != nil {
			return err
		}
		return validationError("Crypto.com withdrawal address is required")
	}
	if err := validateOptionalText("address tag", request.AddressTag, 128); err != nil {
		return err
	}
	if err := validateNetworkID(request.NetworkID); err != nil {
		return err
	}
	return validateOptionalText("client withdrawal ID", request.ClientWithdrawID, maximumWithdrawalClientIDSize)
}

func (request CreateWithdrawalRequest) params() map[string]any {
	params := map[string]any{
		"currency": request.Currency,
		"amount":   request.Amount,
		"address":  request.Address,
	}
	setStringParam(params, "address_tag", request.AddressTag)
	setStringParam(params, "network_id", request.NetworkID)
	setStringParam(params, "client_wid", request.ClientWithdrawID)
	return params
}

func (request WithdrawalAddressesRequest) validate() error {
	if err := validateOptionalCurrency(request.Currency); err != nil {
		return err
	}
	if err := validateNetworkID(request.NetworkID); err != nil {
		return err
	}
	return validateWalletPage(nil, nil, request.PageSize, request.Page)
}

func (request WithdrawalAddressesRequest) params() map[string]any {
	params := make(map[string]any)
	setStringParam(params, "currency", request.Currency)
	setStringParam(params, "network_id", request.NetworkID)
	setPageParams(params, nil, nil, request.PageSize, request.Page)
	return params
}

// walletListCall은 result의 listKey 배열을 읽는 read 요청을 실행한다.
func (client *Client) walletListCall(
	ctx context.Context,
	method string,
	params map[string]any,
	listKey string,
	options ...trade.RequestOption,
) (commonexchange.Response, string, []json.RawMessage, error) {
	response, requestID, resultRaw, err := client.callPrivate(
		ctx, method, params, credential.PermissionRead,
		commonexchange.OperationRead, true, options...,
	)
	if err != nil {
		return commonexchange.Response{}, "", nil, err
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		return commonexchange.Response{}, "", nil, client.decodePrivateBodyError(
			response, commonexchange.OperationRead, requestID, err,
		)
	}
	listRaw, ok := result[listKey]
	if !ok {
		// 결과가 없으면 거래소가 키를 생략할 수 있으므로 빈 목록으로 본다.
		return response, requestID, nil, nil
	}
	items, err := client.decodePrivateArray(response, requestID, commonexchange.OperationRead, listRaw)
	if err != nil {
		return commonexchange.Response{}, "", nil, err
	}
	return response, requestID, items, nil
}

// DepositAddresses는 통화의 네트워크별 입금 주소를 조회한다. 권한은 read다.
func (client *Client) DepositAddresses(
	ctx context.Context,
	request DepositAddressRequest,
	options ...trade.RequestOption,
) ([]DepositAddress, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, requestID, itemsRaw, err := client.walletListCall(
		ctx, methodGetDepositAddress, request.params(), "deposit_address_list", options...,
	)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems(itemsRaw, func(item *DepositAddress, raw json.RawMessage) { item.Raw = raw })
	if err != nil {
		return nil, client.decodePrivateBodyError(response, commonexchange.OperationRead, requestID, err)
	}
	for _, item := range items {
		if item.Address == "" || item.Currency != request.Currency {
			return nil, client.decodePrivateBodyError(
				response, commonexchange.OperationRead, requestID,
				errors.New("Crypto.com deposit address or currency is invalid"),
			)
		}
	}
	return items, nil
}

// DepositHistory는 입금 이력을 조회한다. 권한은 read다.
func (client *Client) DepositHistory(
	ctx context.Context,
	request DepositHistoryRequest,
	options ...trade.RequestOption,
) ([]Deposit, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, requestID, itemsRaw, err := client.walletListCall(
		ctx, methodGetDepositHistory, request.params(), "deposit_list", options...,
	)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems(itemsRaw, func(item *Deposit, raw json.RawMessage) { item.Raw = raw })
	if err != nil {
		return nil, client.decodePrivateBodyError(response, commonexchange.OperationRead, requestID, err)
	}
	for _, item := range items {
		if item.ID == "" || item.Currency == "" {
			return nil, client.decodePrivateBodyError(
				response, commonexchange.OperationRead, requestID,
				errors.New("Crypto.com deposit identity is invalid"),
			)
		}
	}
	return items, nil
}

// WithdrawalHistory는 출금 이력을 조회한다. 권한은 read다.
func (client *Client) WithdrawalHistory(
	ctx context.Context,
	request WithdrawalHistoryRequest,
	options ...trade.RequestOption,
) ([]Withdrawal, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, requestID, itemsRaw, err := client.walletListCall(
		ctx, methodGetWithdrawalHistory, request.params(), "withdrawal_list", options...,
	)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems(itemsRaw, func(item *Withdrawal, raw json.RawMessage) { item.Raw = raw })
	if err != nil {
		return nil, client.decodePrivateBodyError(response, commonexchange.OperationRead, requestID, err)
	}
	for _, item := range items {
		if item.ID == "" || item.Currency == "" {
			return nil, client.decodePrivateBodyError(
				response, commonexchange.OperationRead, requestID,
				errors.New("Crypto.com withdrawal identity is invalid"),
			)
		}
	}
	return items, nil
}

// WithdrawalAddresses는 저장된 화이트리스트 출금 주소를 조회한다. 권한은 read다.
func (client *Client) WithdrawalAddresses(
	ctx context.Context,
	request WithdrawalAddressesRequest,
	options ...trade.RequestOption,
) ([]WithdrawalAddress, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, requestID, itemsRaw, err := client.walletListCall(
		ctx, methodGetWithdrawalAddresses, request.params(), "withdrawal_address_list", options...,
	)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems(itemsRaw, func(item *WithdrawalAddress, raw json.RawMessage) { item.Raw = raw })
	if err != nil {
		return nil, client.decodePrivateBodyError(response, commonexchange.OperationRead, requestID, err)
	}
	for _, item := range items {
		if item.Address == "" {
			return nil, client.decodePrivateBodyError(
				response, commonexchange.OperationRead, requestID,
				errors.New("Crypto.com withdrawal address is missing"),
			)
		}
	}
	return items, nil
}

// CurrencyNetworks는 통화별 네트워크 매핑과 입출금 가능 여부·수수료를 조회한다. 권한은 read다.
func (client *Client) CurrencyNetworks(
	ctx context.Context,
	options ...trade.RequestOption,
) (CurrencyNetworks, error) {
	response, requestID, resultRaw, err := client.callPrivate(
		ctx, methodGetCurrencyNetworks, nil, credential.PermissionRead,
		commonexchange.OperationRead, true, options...,
	)
	if err != nil {
		return CurrencyNetworks{}, err
	}
	var value CurrencyNetworks
	if err := json.Unmarshal(resultRaw, &value); err != nil {
		return CurrencyNetworks{}, client.decodePrivateBodyError(
			response, commonexchange.OperationRead, requestID, err,
		)
	}
	value.Raw = cloneBytes(resultRaw)
	return value, nil
}

// CreateWithdrawal은 화이트리스트 주소로 출금을 요청한다. 권한은 withdraw이며 자동 재시도하지 않는다.
// 전송 결과가 불명확하면 UNKNOWN_EXECUTION_STATE로 반환하므로 WithdrawalHistory로 ClientWithdrawID를 확인해야 한다.
func (client *Client) CreateWithdrawal(
	ctx context.Context,
	request CreateWithdrawalRequest,
	options ...trade.RequestOption,
) (WithdrawalReceipt, error) {
	if err := request.validate(); err != nil {
		return WithdrawalReceipt{}, err
	}
	response, requestID, resultRaw, err := client.callPrivate(
		ctx, methodCreateWithdrawal, request.params(), credential.PermissionWithdraw,
		commonexchange.OperationMutation, true, options...,
	)
	if err != nil {
		return WithdrawalReceipt{}, err
	}
	var value WithdrawalReceipt
	if err := json.Unmarshal(resultRaw, &value); err != nil {
		return WithdrawalReceipt{}, client.decodePrivateBodyError(
			response, commonexchange.OperationMutation, requestID, err,
		)
	}
	if value.ID == "" ||
		request.ClientWithdrawID != "" && value.ClientWithdrawID != request.ClientWithdrawID {
		return WithdrawalReceipt{}, client.decodePrivateBodyError(
			response, commonexchange.OperationMutation, requestID,
			errors.New("Crypto.com withdrawal receipt identity is inconsistent"),
		)
	}
	value.Raw = cloneBytes(resultRaw)
	return value, nil
}
