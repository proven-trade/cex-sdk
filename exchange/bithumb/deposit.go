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

// depositStatePattern은 입금 상태 필터의 형식이다. 거래소가 입금 상태 목록을 enum으로 고정하지 않아
// 대문자와 밑줄로 된 토큰만 확인하고 값 자체는 거래소가 판단한다.
var depositStatePattern = regexp.MustCompile(`^[A-Z][A-Z_]{0,63}$`)

// DepositState는 입금 처리 상태다. 코인 입금은 입금 신청, 입금, 반환 신청, 반환 출금 단계별 상태를 쓰고
// 원화 입금은 PROCESSING, ACCEPTED, CANCELLED 중 하나를 쓴다.
type DepositState string

const (
	DepositStateRequestedPending        DepositState = "REQUESTED_PENDING"
	DepositStateRequestedSystemRejected DepositState = "REQUESTED_SYSTEM_REJECTED"
	DepositStateRequestedProcessing     DepositState = "REQUESTED_PROCESSING"
	DepositStateRequestedAdminRejected  DepositState = "REQUESTED_ADMIN_REJECTED"
	DepositStateDepositProcessing       DepositState = "DEPOSIT_PROCESSING"
	DepositStateDepositAccepted         DepositState = "DEPOSIT_ACCEPTED"
	DepositStateDepositCancelled        DepositState = "DEPOSIT_CANCELLED"
	DepositStateRefundingPending        DepositState = "REFUNDING_PENDING"
	DepositStateRefundingSystemRejected DepositState = "REFUNDING_SYSTEM_REJECTED"
	DepositStateRefundingProcessing     DepositState = "REFUNDING_PROCESSING"
	DepositStateRefundingAdminRejected  DepositState = "REFUNDING_ADMIN_REJECTED"
	DepositStateRefundingAccepted       DepositState = "REFUNDING_ACCEPTED"
	DepositStateRefundedProcessing      DepositState = "REFUNDED_PROCESSING"
	DepositStateRefundedAccepted        DepositState = "REFUNDED_ACCEPTED"
	DepositStateRefundedCancelled       DepositState = "REFUNDED_CANCELLED"
)

// 원화 입금 목록과 원화 입금 응답에 쓰는 상태다.
const (
	DepositStateKRWProcessing DepositState = "PROCESSING"
	DepositStateKRWAccepted   DepositState = "ACCEPTED"
	DepositStateKRWCancelled  DepositState = "CANCELLED"
)

// DepositsRequest는 코인 입금 목록 조회 조건이다.
// Page는 1부터 시작하며 0이면 거래소 기본값을 쓴다. Limit은 최대 100이다.
type DepositsRequest struct {
	Currency string
	State    DepositState
	UUIDs    []string
	TxIDs    []string
	Page     int
	Limit    int
	OrderBy  string
}

// KRWDepositsRequest는 원화 입금 목록 조회 조건이다.
type KRWDepositsRequest struct {
	State   DepositState
	UUIDs   []string
	TxIDs   []string
	Page    int
	Limit   int
	OrderBy string
}

// DepositInfoRequest는 UUID 또는 TxID로 입금 한 건을 조회하는 조건이다.
// Currency는 필수이며 UUID와 TxID 중 정확히 하나만 지정한다.
type DepositInfoRequest struct {
	Currency string
	UUID     string
	TxID     string
}

// DepositAddressRequest는 통화와 네트워크별 입금 주소 조회 조건이다.
type DepositAddressRequest struct {
	Currency string
	NetType  string
}

// GenerateDepositAddressRequest는 통화와 네트워크별 입금 주소 생성 요청이다.
type GenerateDepositAddressRequest struct {
	Currency string
	NetType  string
}

// DepositKRWRequest는 등록된 계좌에서 원화를 입금하는 요청이다.
type DepositKRWRequest struct {
	Amount        string
	TwoFactorType TwoFactorType
}

// Deposit은 코인 또는 원화 입금 한 건이다. 원화 입금에는 NetType이 비어 있다.
type Deposit struct {
	Type            string          `json:"type"`
	UUID            string          `json:"uuid"`
	Currency        string          `json:"currency"`
	NetType         string          `json:"net_type"`
	TxID            string          `json:"txid"`
	State           DepositState    `json:"state"`
	CreatedAt       string          `json:"created_at"`
	DoneAt          string          `json:"done_at"`
	Amount          Decimal         `json:"amount"`
	Fee             Decimal         `json:"fee"`
	TransactionType string          `json:"transaction_type"`
	Raw             json.RawMessage `json:"-"`
}

// DepositAddress는 통화와 네트워크별 입금 주소다. SecondaryAddress는 destination tag처럼
// 보조 주소가 필요한 자산에만 채워진다.
type DepositAddress struct {
	Currency         string          `json:"currency"`
	NetType          string          `json:"net_type"`
	DepositAddress   string          `json:"deposit_address"`
	SecondaryAddress string          `json:"secondary_address"`
	Raw              json.RawMessage `json:"-"`
}

// Deposits는 코인 입금 목록을 조회한다.
func (client *Client) Deposits(
	ctx context.Context,
	request DepositsRequest,
	options ...trade.RequestOption,
) ([]Deposit, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.listDeposits(ctx, "/v1/deposits", request.parameters(), options...)
}

// KRWDeposits는 원화 입금 목록을 조회한다.
func (client *Client) KRWDeposits(
	ctx context.Context,
	request KRWDepositsRequest,
	options ...trade.RequestOption,
) ([]Deposit, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.listDeposits(ctx, "/v1/deposits/krw", request.parameters(), options...)
}

func (client *Client) listDeposits(
	ctx context.Context,
	path string,
	params parameters,
	options ...trade.RequestOption,
) ([]Deposit, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, path, params, nil, rateLimitPrivateOther,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *Deposit, raw []byte) {
		item.Raw = raw
	})
}

// DepositInfo는 UUID 또는 TxID로 입금 한 건을 조회한다.
func (client *Client) DepositInfo(
	ctx context.Context,
	request DepositInfoRequest,
	options ...trade.RequestOption,
) (Deposit, error) {
	if err := request.validate(); err != nil {
		return Deposit{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/deposit", request.parameters(), nil, rateLimitPrivateOther,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Deposit{}, err
	}
	var result Deposit
	if err := client.decodeResponse(response, commonexchange.OperationRead, &result); err != nil {
		return Deposit{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// DepositAddresses는 발급된 전체 입금 주소를 조회한다.
func (client *Client) DepositAddresses(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]DepositAddress, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/deposits/coin_addresses", nil, nil, rateLimitPrivateOther,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *DepositAddress, raw []byte) {
		item.Raw = raw
	})
}

// DepositAddressInfo는 통화와 네트워크별 입금 주소 한 건을 조회한다.
func (client *Client) DepositAddressInfo(
	ctx context.Context,
	request DepositAddressRequest,
	options ...trade.RequestOption,
) (DepositAddress, error) {
	if err := request.validate(); err != nil {
		return DepositAddress{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/deposits/coin_address", request.parameters(), nil, rateLimitPrivateOther,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return DepositAddress{}, err
	}
	var result DepositAddress
	if err := client.decodeResponse(response, commonexchange.OperationRead, &result); err != nil {
		return DepositAddress{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// GenerateDepositAddress는 통화와 네트워크별 입금 주소 생성을 요청한다. credential.PermissionTransfer가 있어야 한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환하므로
// DepositAddressInfo로 생성 여부를 확인한 뒤에만 다시 요청해야 한다.
func (client *Client) GenerateDepositAddress(
	ctx context.Context,
	request GenerateDepositAddressRequest,
	options ...trade.RequestOption,
) (DepositAddress, error) {
	if err := request.validate(); err != nil {
		return DepositAddress{}, err
	}
	params := request.parameters()
	body, err := encodeBody(params)
	if err != nil {
		return DepositAddress{}, validationError("encode deposit address body: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v1/deposits/generate_coin_address", params, body, rateLimitPrivateOther,
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return DepositAddress{}, err
	}
	var result DepositAddress
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &result); err != nil {
		return DepositAddress{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// DepositKRW는 등록된 계좌에서 원화 입금을 요청한다. credential.PermissionTransfer가 있어야 한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환하므로
// KRWDeposits로 접수 여부를 확인한 뒤에만 다시 요청해야 한다.
func (client *Client) DepositKRW(
	ctx context.Context,
	request DepositKRWRequest,
	options ...trade.RequestOption,
) (Deposit, error) {
	if err := request.validate(); err != nil {
		return Deposit{}, err
	}
	params := request.parameters()
	body, err := encodeBody(params)
	if err != nil {
		return Deposit{}, validationError("encode KRW deposit body: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v1/deposits/krw", params, body, rateLimitPrivateOther,
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Deposit{}, err
	}
	var result Deposit
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &result); err != nil {
		return Deposit{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

func (request DepositsRequest) validate() error {
	if request.Currency != "" {
		if err := validateIdentifier("currency", request.Currency); err != nil {
			return err
		}
	}
	if err := validateDepositFilters(request.State, request.UUIDs, request.TxIDs); err != nil {
		return err
	}
	return validateListPaging(request.Page, request.Limit, request.OrderBy)
}

func (request DepositsRequest) parameters() parameters {
	return transferListParameters(
		request.Currency, string(request.State), request.UUIDs, request.TxIDs,
		request.Page, request.Limit, request.OrderBy,
	)
}

func (request KRWDepositsRequest) validate() error {
	if err := validateDepositFilters(request.State, request.UUIDs, request.TxIDs); err != nil {
		return err
	}
	return validateListPaging(request.Page, request.Limit, request.OrderBy)
}

func (request KRWDepositsRequest) parameters() parameters {
	return transferListParameters(
		"", string(request.State), request.UUIDs, request.TxIDs,
		request.Page, request.Limit, request.OrderBy,
	)
}

func (request DepositInfoRequest) validate() error {
	if err := validateIdentifier("currency", request.Currency); err != nil {
		return err
	}
	return validateTransferIdentity(request.UUID, request.TxID)
}

func (request DepositInfoRequest) parameters() parameters {
	values := parameters{}
	values.add("currency", request.Currency)
	values.add("uuid", request.UUID)
	values.add("txid", request.TxID)
	return values
}

func (request DepositAddressRequest) validate() error {
	return validateCurrencyNetwork(request.Currency, request.NetType)
}

func (request DepositAddressRequest) parameters() parameters {
	return parameters{
		{key: "currency", value: request.Currency},
		{key: "net_type", value: request.NetType},
	}
}

func (request GenerateDepositAddressRequest) validate() error {
	return validateCurrencyNetwork(request.Currency, request.NetType)
}

func (request GenerateDepositAddressRequest) parameters() parameters {
	return parameters{
		{key: "currency", value: request.Currency},
		{key: "net_type", value: request.NetType},
	}
}

func (request DepositKRWRequest) validate() error {
	return validateKRWTransfer(request.Amount, request.TwoFactorType)
}

func (request DepositKRWRequest) parameters() parameters {
	return parameters{
		{key: "amount", value: request.Amount},
		{key: "two_factor_type", value: string(request.TwoFactorType)},
	}
}

// validateDepositFilters는 입금 목록 조회의 상태와 UUID·TxID 필터를 검증한다.
func validateDepositFilters(state DepositState, uuids, txids []string) error {
	if state != "" && !depositStatePattern.MatchString(string(state)) {
		return validationError("invalid deposit state %q", state)
	}
	if err := validateIDList("uuids", uuids, 0, nil); err != nil {
		return err
	}
	return validateIDList("txids", txids, 0, nil)
}
