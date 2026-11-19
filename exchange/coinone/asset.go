package coinone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// maxTransferHistorySpan은 입출금 내역 한 번에 조회할 수 있는 최대 기간이다.
const maxTransferHistorySpan = 90 * 24 * time.Hour

// TransferType은 입출금 내역의 방향이다. 요청에서 빈 값은 입금과 출금을 모두 조회한다.
type TransferType string

const (
	TransferTypeDeposit    TransferType = "DEPOSIT"
	TransferTypeWithdrawal TransferType = "WITHDRAWAL"
)

// DepositAddressesRequest는 입금 주소 조회 조건이다. Currencies가 비어 있으면 전체 자산을 조회한다.
type DepositAddressesRequest struct {
	Currencies []string
}

// KRWTransactionHistoryRequest는 원화 입출금 내역 조회 조건이다.
// ToID는 해당 ID 이전 내역을 조회하는 커서이고 Size는 1부터 100까지이며 비우면 100건이다.
// From이 비어 있으면 현재 시점 90일 전, To가 비어 있으면 현재 시점으로 조회하며
// 기간은 90일을 넘을 수 없고 From은 To보다 앞서야 한다.
type KRWTransactionHistoryRequest struct {
	ToID string
	Type TransferType
	Size int
	From time.Time
	To   time.Time
}

// CoinTransactionHistoryRequest는 가상자산 입출금 내역 조회 조건이다.
// Currency가 비어 있으면 모든 종목을 조회하고 나머지 조건은 KRWTransactionHistoryRequest와 같다.
type CoinTransactionHistoryRequest struct {
	Currency string
	ToID     string
	Type     TransferType
	Size     int
	From     time.Time
	To       time.Time
}

// CoinTransactionDetailRequest는 입출금 거래 식별 ID로 가상자산 입출금 한 건을 조회하는 조건이다.
// 출금 요청 응답의 Withdrawal.ID로 진행 상태를 확인할 때 사용한다.
type CoinTransactionDetailRequest struct {
	ID string
}

// WithdrawalLimitRequest는 가상자산 출금 한도 조회 조건이다.
type WithdrawalLimitRequest struct {
	Currency string
}

// WithdrawalAddressBookRequest는 출금 주소록 조회 조건이다. Currency가 비어 있으면 전체 종목을 조회한다.
type WithdrawalAddressBookRequest struct {
	Currency string
}

// WithdrawRequest는 가상자산 출금 요청이다.
// Address는 코인원 API 출금 주소록에 2차 인증과 수취인 정보 등록을 마친 주소여야 하며
// 대소문자를 구분한다. SecondaryAddress는 Memo 또는 Destination Tag이고 대소문자를 구분한다.
type WithdrawRequest struct {
	Currency         string
	Amount           string
	Address          string
	SecondaryAddress string
}

// DepositAddress는 가상자산 입금 주소다. SecondaryAddress는 Memo 또는 Tag가 없으면 비어 있다.
type DepositAddress struct {
	Currency         string          `json:"currency"`
	Address          string          `json:"address"`
	SecondaryAddress string          `json:"secondary_address"`
	Raw              json.RawMessage `json:"-"`
}

// KRWTransaction은 원화 입출금 내역 한 건이다. CreatedAt은 밀리초 단위 Unix 시각이다.
// Status는 DEPOSIT_COMPLETE, DEPOSIT_WAIT, DEPOSIT_PROCESSING, DEPOSIT_FAILED,
// WITHDRAWAL_COMPLETE, WITHDRAWAL_WAIT, WITHDRAWAL_PROCESSING, WITHDRAWAL_FAILED, REFUND_COMPLETE 중 하나다.
// TransactionType이 DEFAULT이면 은행 입출금이다.
type KRWTransaction struct {
	ID              string          `json:"id"`
	Status          string          `json:"status"`
	Amount          Decimal         `json:"amount"`
	Fee             Decimal         `json:"fee"`
	Type            TransferType    `json:"type"`
	TransactionType string          `json:"transaction_type"`
	CreatedAt       int64           `json:"created_at"`
	Raw             json.RawMessage `json:"-"`
}

// CoinTransaction은 가상자산 입출금 내역 한 건이다. CreatedAt은 밀리초 단위 Unix 시각이다.
// Status는 DEPOSIT_WAIT, DEPOSIT_SUCCESS, DEPOSIT_FAIL, DEPOSIT_REFUND, DEPOSIT_REJECT,
// WITHDRAWAL_REGISTER, WITHDRAWAL_WAIT, WITHDRAWAL_SUCCESS, WITHDRAWAL_FAIL,
// WITHDRAWAL_REFUND, WITHDRAWAL_REFUND_FAIL 중 하나다.
type CoinTransaction struct {
	ID                   string          `json:"id"`
	Currency             string          `json:"currency"`
	TxID                 string          `json:"txid"`
	Type                 TransferType    `json:"type"`
	FromAddress          string          `json:"from_address"`
	FromSecondaryAddress string          `json:"from_secondary_address"`
	ToAddress            string          `json:"to_address"`
	ToSecondaryAddress   string          `json:"to_secondary_address"`
	Confirmations        int             `json:"confirmations"`
	Amount               Decimal         `json:"amount"`
	Fee                  Decimal         `json:"fee"`
	Status               string          `json:"status"`
	CreatedAt            int64           `json:"created_at"`
	Raw                  json.RawMessage `json:"-"`
}

// WithdrawalLimit은 전일 종가 기준 가상자산 출금 한도다.
// Total은 출금 가능한 전체 수량이고 Available은 이미 출금한 수량을 뺀 남은 수량이다.
type WithdrawalLimit struct {
	Total     Decimal         `json:"total"`
	Available Decimal         `json:"available"`
	Raw       json.RawMessage `json:"-"`
}

// WithdrawalAddress는 API 출금이 허용된 출금 주소록 항목이다.
// SecondaryAddress와 Nickname은 없으면 비어 있고 CreatedAt은 주소 등록 시각의 Unix timestamp다.
type WithdrawalAddress struct {
	Currency         string          `json:"currency"`
	Address          string          `json:"address"`
	SecondaryAddress string          `json:"secondary_address"`
	Nickname         string          `json:"nickname"`
	CreatedAt        int64           `json:"created_at"`
	Raw              json.RawMessage `json:"-"`
}

// Withdrawal은 출금 요청 접수 결과다. ID로 CoinTransactionDetail을 조회해 진행 상태를 확인한다.
type Withdrawal struct {
	ID               string          `json:"id"`
	Currency         string          `json:"currency"`
	Address          string          `json:"address"`
	SecondaryAddress string          `json:"secondary_address"`
	Amount           Decimal         `json:"amount"`
	Status           string          `json:"status"`
	Raw              json.RawMessage `json:"-"`
}

func (request DepositAddressesRequest) validate() error {
	return validateCurrencyList("deposit address currency", request.Currencies)
}

func (request DepositAddressesRequest) fields() payloadFields {
	values := payloadFields{}
	values.addStrings("currencies", request.Currencies)
	return values
}

func (request KRWTransactionHistoryRequest) validate(now time.Time) error {
	return validateTransferHistory(now, request.ToID, request.Type, request.Size, request.From, request.To)
}

func (request KRWTransactionHistoryRequest) fields() payloadFields {
	return transferHistoryFields(request.ToID, request.Type, request.Size, request.From, request.To)
}

func (request CoinTransactionHistoryRequest) validate(now time.Time) error {
	if request.Currency != "" {
		if err := validateCurrency("currency", request.Currency); err != nil {
			return err
		}
	}
	return validateTransferHistory(now, request.ToID, request.Type, request.Size, request.From, request.To)
}

func (request CoinTransactionHistoryRequest) fields() payloadFields {
	values := payloadFields{}
	values.addString("currency", request.Currency)
	return append(values, transferHistoryFields(request.ToID, request.Type, request.Size, request.From, request.To)...)
}

func (request CoinTransactionDetailRequest) validate() error {
	if request.ID == "" || strings.TrimSpace(request.ID) != request.ID {
		return validationError("invalid transaction ID")
	}
	return nil
}

func (request CoinTransactionDetailRequest) fields() payloadFields {
	values := payloadFields{}
	values.addString("id", request.ID)
	return values
}

func (request WithdrawalLimitRequest) validate() error {
	return validateCurrency("currency", request.Currency)
}

func (request WithdrawalLimitRequest) fields() payloadFields {
	values := payloadFields{}
	values.addString("currency", request.Currency)
	return values
}

func (request WithdrawalAddressBookRequest) validate() error {
	if request.Currency == "" {
		return nil
	}
	return validateCurrency("currency", request.Currency)
}

func (request WithdrawalAddressBookRequest) fields() payloadFields {
	values := payloadFields{}
	values.addString("currency", request.Currency)
	return values
}

func (request WithdrawRequest) validate() error {
	if err := validateCurrency("currency", request.Currency); err != nil {
		return err
	}
	if err := validatePositiveDecimal("withdrawal amount", request.Amount); err != nil {
		return err
	}
	if request.Address == "" || strings.TrimSpace(request.Address) != request.Address {
		return validationError("invalid withdrawal address")
	}
	if strings.TrimSpace(request.SecondaryAddress) != request.SecondaryAddress {
		return validationError("invalid withdrawal secondary address")
	}
	return nil
}

func (request WithdrawRequest) fields() payloadFields {
	values := payloadFields{}
	values.addString("currency", request.Currency)
	values.addString("amount", request.Amount)
	values.addString("address", request.Address)
	values.addString("secondary_address", request.SecondaryAddress)
	return values
}

// DepositAddresses는 입금 가능한 가상자산의 입금 주소를 조회한다. Read 권한이 필요하다.
// 이 API는 이미 발급된 주소를 조회할 뿐 주소를 새로 만들지 않으며 입금이 불가능한 자산은 응답에서 제외된다.
func (client *Client) DepositAddresses(
	ctx context.Context,
	request DepositAddressesRequest,
	options ...trade.RequestOption,
) ([]DepositAddress, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, "/v2.1/account/deposit_address", request.fields(), false,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		DepositAddresses []json.RawMessage `json:"deposit_addresses"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(client, response, envelope.DepositAddresses, func(item *DepositAddress, raw []byte) {
		item.Raw = raw
	})
}

// KRWTransactionHistory는 원화 입출금 내역을 조회한다. Read 권한이 필요하다.
// 코인원은 포트폴리오 API Key를 지원하지 않으며 error code 315는 AUTHORIZATION으로 분류된다.
func (client *Client) KRWTransactionHistory(
	ctx context.Context,
	request KRWTransactionHistoryRequest,
	options ...trade.RequestOption,
) ([]KRWTransaction, error) {
	if err := request.validate(client.now()); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, "/v2.1/transaction/krw/history", request.fields(), false,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Transactions []json.RawMessage `json:"transactions"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(client, response, envelope.Transactions, func(item *KRWTransaction, raw []byte) {
		item.Raw = raw
	})
}

// CoinTransactionHistory는 가상자산 입출금 내역을 조회한다. Read 권한이 필요하다.
// 코인원은 포트폴리오 API Key를 지원하지 않으며 error code 315는 AUTHORIZATION으로 분류된다.
func (client *Client) CoinTransactionHistory(
	ctx context.Context,
	request CoinTransactionHistoryRequest,
	options ...trade.RequestOption,
) ([]CoinTransaction, error) {
	if err := request.validate(client.now()); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, "/v2.1/transaction/coin/history", request.fields(), false,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Transactions []json.RawMessage `json:"transactions"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(client, response, envelope.Transactions, func(item *CoinTransaction, raw []byte) {
		item.Raw = raw
	})
}

// CoinTransactionDetail은 입출금 거래 식별 ID로 가상자산 입출금 한 건의 진행 상태를 조회한다.
// Read 권한이 필요하며 포트폴리오 API Key는 지원되지 않는다.
func (client *Client) CoinTransactionDetail(
	ctx context.Context,
	request CoinTransactionDetailRequest,
	options ...trade.RequestOption,
) (CoinTransaction, error) {
	if err := request.validate(); err != nil {
		return CoinTransaction{}, err
	}
	response, err := client.executePrivate(
		ctx, "/v2.1/transaction/coin/history/detail", request.fields(), false,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return CoinTransaction{}, err
	}
	var envelope struct {
		Transactions []json.RawMessage `json:"transactions"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return CoinTransaction{}, err
	}
	items, err := decodeRawItems(client, response, envelope.Transactions, func(item *CoinTransaction, raw []byte) {
		item.Raw = raw
	})
	if err != nil {
		return CoinTransaction{}, err
	}
	return singleItem(client, response, "coin transaction", items)
}

// WithdrawalLimit은 전일 종가 기준 가상자산 출금 한도를 조회한다. Read 권한이 필요하다.
// 코인원은 포트폴리오 API Key를 지원하지 않으며 error code 315는 AUTHORIZATION으로 분류된다.
func (client *Client) WithdrawalLimit(
	ctx context.Context,
	request WithdrawalLimitRequest,
	options ...trade.RequestOption,
) (WithdrawalLimit, error) {
	if err := request.validate(); err != nil {
		return WithdrawalLimit{}, err
	}
	response, err := client.executePrivate(
		ctx, "/v2.1/transaction/coin/withdrawal/limit", request.fields(), false,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return WithdrawalLimit{}, err
	}
	var envelope struct {
		Limit *WithdrawalLimit `json:"limit"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return WithdrawalLimit{}, err
	}
	if envelope.Limit == nil {
		return WithdrawalLimit{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Coinone withdrawal limit response has no limit"),
		)
	}
	result := *envelope.Limit
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// WithdrawalAddressBook은 API 출금이 허용된 출금 주소록을 조회한다. Read 권한이 필요하다.
// 코인원은 포트폴리오 API Key를 지원하지 않으며 error code 315는 AUTHORIZATION으로 분류된다.
func (client *Client) WithdrawalAddressBook(
	ctx context.Context,
	request WithdrawalAddressBookRequest,
	options ...trade.RequestOption,
) ([]WithdrawalAddress, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, "/v2.1/transaction/coin/withdrawal/address_book", request.fields(), false,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		WithdrawalAddresses []json.RawMessage `json:"withdrawal_addresses"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(client, response, envelope.WithdrawalAddresses, func(item *WithdrawalAddress, raw []byte) {
		item.Raw = raw
	})
}

// Withdraw는 API 출금 주소록에 등록된 주소로 가상자산 출금을 요청한다. Withdraw 권한이 필요하다.
// 코인원은 포트폴리오 API Key를 지원하지 않으며 error code 315는 AUTHORIZATION으로 분류된다.
// 자동 재시도하지 않으며 전송 결과가 불명확하면 UNKNOWN_EXECUTION_STATE로 반환하므로
// 출금 내역(CoinTransactionHistory)으로 접수 여부를 확인하기 전에는 다시 요청하지 않아야 한다.
func (client *Client) Withdraw(
	ctx context.Context,
	request WithdrawRequest,
	options ...trade.RequestOption,
) (Withdrawal, error) {
	if err := request.validate(); err != nil {
		return Withdrawal{}, err
	}
	response, err := client.executePrivate(
		ctx, "/v2.1/transaction/coin/withdrawal", request.fields(), false,
		credential.PermissionWithdraw, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Withdrawal{}, err
	}
	var envelope struct {
		Transaction json.RawMessage `json:"transaction"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &envelope); err != nil {
		return Withdrawal{}, err
	}
	var result Withdrawal
	if err := json.Unmarshal(envelope.Transaction, &result); err != nil {
		return Withdrawal{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, fmt.Errorf("decode Coinone withdrawal: %w", err),
		)
	}
	if result.ID == "" {
		return Withdrawal{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Coinone withdrawal response has no transaction ID"),
		)
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

func validateTransferHistory(
	now time.Time,
	toID string,
	transferType TransferType,
	size int,
	from, to time.Time,
) error {
	if strings.TrimSpace(toID) != toID {
		return validationError("invalid transfer history cursor")
	}
	if transferType != "" && transferType != TransferTypeDeposit && transferType != TransferTypeWithdrawal {
		return validationError("transfer type must be DEPOSIT or WITHDRAWAL, or empty for both")
	}
	if size < 0 || size > 100 {
		return validationError("transfer history size must be between 1 and 100 or zero for default")
	}
	if !from.IsZero() && from.UnixMilli() <= 0 || !to.IsZero() && to.UnixMilli() <= 0 {
		return validationError("transfer history timestamps must be after the Unix epoch")
	}
	if from.After(now) || to.After(now) {
		return validationError("transfer history range cannot be in the future")
	}
	effectiveFrom, effectiveTo := now.Add(-maxTransferHistorySpan), now
	if !from.IsZero() {
		effectiveFrom = from
	}
	if !to.IsZero() {
		effectiveTo = to
	}
	if !effectiveFrom.Before(effectiveTo) {
		return validationError("transfer history from must be before to")
	}
	if effectiveTo.Sub(effectiveFrom) > maxTransferHistorySpan {
		return validationError("transfer history range cannot exceed 90 days")
	}
	return nil
}

func transferHistoryFields(
	toID string,
	transferType TransferType,
	size int,
	from, to time.Time,
) payloadFields {
	values := payloadFields{}
	values.addString("to_id", toID)
	if transferType != "" {
		values.addBool("is_deposit", transferType == TransferTypeDeposit)
	}
	values.addInt("size", size)
	if !from.IsZero() {
		values.addInt64("from_ts", from.UnixMilli())
	}
	if !to.IsZero() {
		values.addInt64("to_ts", to.UnixMilli())
	}
	return values
}
