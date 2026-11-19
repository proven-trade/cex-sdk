package korbit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// WithdrawableAddressesRequest는 API 출금용으로 등록된 주소 조회 조건이다. 입출금 API는 메인 계정만 지원하므로
// AccountSeq는 0(생략)이나 1만 허용한다.
type WithdrawableAddressesRequest struct {
	AccountSeq int
}

// WithdrawableAmountsRequest는 출금 가능 수량 조회 조건이다. Currency를 비우면 모든 자산을 조회한다.
type WithdrawableAmountsRequest struct {
	AccountSeq int
	Currency   string
}

// CreateCoinWithdrawalRequest는 가상자산 출금 요청이다. Amount는 수수료를 제외한 출금 수량이고 Address는 API 출금용으로
// 미리 등록한 주소여야 한다. 거래소 기본 네트워크는 바뀔 수 있으므로 Network는 필수이며, SecondaryAddress는
// destination tag나 memo가 없으면 비운다.
type CreateCoinWithdrawalRequest struct {
	AccountSeq       int
	Currency         string
	Network          string
	Amount           string
	Address          string
	SecondaryAddress string
}

// CancelCoinWithdrawalRequest는 출금 취소 조건이다. 상태가 actionRequired 또는 reviewing인 출금만 취소할 수 있다.
type CancelCoinWithdrawalRequest struct {
	AccountSeq       int
	CoinWithdrawalID int64
}

// CoinWithdrawalRequest는 출금 ID로 가상자산 출금 한 건을 조회하는 조건이다.
type CoinWithdrawalRequest struct {
	AccountSeq       int
	Currency         string
	CoinWithdrawalID int64
}

// RecentCoinWithdrawalsRequest는 자산별 최근 가상자산 출금 목록 조회 조건이다. Limit은 1~100이고 0이면 거래소 기본값을 쓴다.
type RecentCoinWithdrawalsRequest struct {
	AccountSeq int
	Currency   string
	Limit      int
}

// WithdrawableAddress는 API 출금용으로 등록된 주소다. Currency가 비어 있으면 해당 네트워크의 모든 자산에 등록된 주소이고,
// SecondaryAddress는 destination tag나 memo이며 없으면 빈 문자열이다.
type WithdrawableAddress struct {
	Network          string          `json:"network"`
	Currency         string          `json:"currency"`
	Address          string          `json:"address"`
	SecondaryAddress string          `json:"secondaryAddress"`
	Raw              json.RawMessage `json:"-"`
}

// WithdrawableAmount는 자산별 출금 가능 수량과 출금 진행 중인 수량이다.
type WithdrawableAmount struct {
	Currency        string          `json:"currency"`
	Withdrawable    string          `json:"withdrawableAmount"`
	WithdrawalInUse string          `json:"withdrawalInUseAmount"`
	Raw             json.RawMessage `json:"-"`
}

// CoinWithdrawalStatus는 가상자산 출금 처리 상태다.
type CoinWithdrawalStatus string

const (
	CoinWithdrawalStatusPending        CoinWithdrawalStatus = "pending"
	CoinWithdrawalStatusActionRequired CoinWithdrawalStatus = "actionRequired"
	CoinWithdrawalStatusReviewing      CoinWithdrawalStatus = "reviewing"
	CoinWithdrawalStatusProcessing     CoinWithdrawalStatus = "processing"
	CoinWithdrawalStatusDone           CoinWithdrawalStatus = "done"
	CoinWithdrawalStatusCanceled       CoinWithdrawalStatus = "canceled"
	CoinWithdrawalStatusFailed         CoinWithdrawalStatus = "failed"
)

// CoinWithdrawalReceipt는 출금 요청 접수 결과다. 접수 후에도 이메일 확인이나 심사로 실제 출금이 지연될 수 있으므로
// Status가 pending이어도 완료로 보지 말고 CoinWithdrawal로 상태를 확인한다.
type CoinWithdrawalReceipt struct {
	CoinWithdrawalID int64                `json:"coinWithdrawalId"`
	Status           CoinWithdrawalStatus `json:"status"`
	Raw              json.RawMessage      `json:"-"`
}

// CoinWithdrawal은 가상자산 출금 한 건이다. Quantity는 수수료를 제외한 출금 수량이고, TransactionHash는 아직
// 블록체인으로 전송되지 않았으면 빈 문자열이다.
type CoinWithdrawal struct {
	ID               int64                `json:"id"`
	Quantity         string               `json:"quantity"`
	Fee              string               `json:"fee"`
	Currency         string               `json:"currency"`
	Status           CoinWithdrawalStatus `json:"status"`
	Network          string               `json:"network"`
	Address          string               `json:"address"`
	SecondaryAddress string               `json:"secondaryAddress"`
	TransactionHash  string               `json:"transactionHash"`
	CreatedAt        int64                `json:"createdAt"`
	Raw              json.RawMessage      `json:"-"`
}

func (request WithdrawableAddressesRequest) validate() error {
	return validateMainAccountSeq(request.AccountSeq)
}

func (request WithdrawableAddressesRequest) values() url.Values {
	values := make(url.Values)
	setAccountSeq(values, request.AccountSeq)
	return values
}

func (request WithdrawableAmountsRequest) validate() error {
	if err := validateMainAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	return validateOptionalCurrency(request.Currency)
}

func (request WithdrawableAmountsRequest) values() url.Values {
	values := make(url.Values)
	setAccountSeq(values, request.AccountSeq)
	setIfNotEmpty(values, "currency", request.Currency)
	return values
}

func (request CreateCoinWithdrawalRequest) validate() error {
	if err := validateMainAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	if err := validateNetwork(request.Network); err != nil {
		return err
	}
	if err := validatePositiveDecimal("withdrawal amount", request.Amount); err != nil {
		return err
	}
	if err := validateWithdrawalAddress(request.Address); err != nil {
		return err
	}
	return validateSecondaryAddress(request.SecondaryAddress)
}

func (request CreateCoinWithdrawalRequest) values() url.Values {
	values := make(url.Values)
	setAccountSeq(values, request.AccountSeq)
	values.Set("currency", request.Currency)
	values.Set("network", request.Network)
	values.Set("amount", request.Amount)
	values.Set("address", request.Address)
	setIfNotEmpty(values, "secondaryAddress", request.SecondaryAddress)
	return values
}

func (request CancelCoinWithdrawalRequest) validate() error {
	if err := validateMainAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	return validatePositiveID("coin withdrawal ID", request.CoinWithdrawalID)
}

func (request CancelCoinWithdrawalRequest) values() url.Values {
	values := url.Values{"coinWithdrawalId": {strconv.FormatInt(request.CoinWithdrawalID, 10)}}
	setAccountSeq(values, request.AccountSeq)
	return values
}

func (request CoinWithdrawalRequest) validate() error {
	if err := validateMainAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	return validatePositiveID("coin withdrawal ID", request.CoinWithdrawalID)
}

func (request CoinWithdrawalRequest) values() url.Values {
	values := url.Values{"currency": {request.Currency}}
	values.Set("coinWithdrawalId", strconv.FormatInt(request.CoinWithdrawalID, 10))
	setAccountSeq(values, request.AccountSeq)
	return values
}

func (request RecentCoinWithdrawalsRequest) validate() error {
	if err := validateMainAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	return validateOptionalLimit("recent coin withdrawal", request.Limit, 100)
}

func (request RecentCoinWithdrawalsRequest) values() url.Values {
	values := url.Values{"currency": {request.Currency}}
	setAccountSeq(values, request.AccountSeq)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

// WithdrawableAddresses는 API 출금용으로 등록된 주소 목록을 조회한다.
func (client *Client) WithdrawableAddresses(
	ctx context.Context,
	request WithdrawableAddressesRequest,
	options ...trade.RequestOption,
) ([]WithdrawableAddress, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/coin/withdrawableAddresses", request.values(), rateGroupAsset,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[WithdrawableAddress](
		client, response, func(item *WithdrawableAddress, raw []byte) { item.Raw = raw },
	)
}

// WithdrawableAmounts는 자산별 출금 가능 수량을 조회한다. Currency를 비우면 모든 자산을 조회한다.
func (client *Client) WithdrawableAmounts(
	ctx context.Context,
	request WithdrawableAmountsRequest,
	options ...trade.RequestOption,
) ([]WithdrawableAmount, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/coin/withdrawableAmount", request.values(), rateGroupAsset,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[WithdrawableAmount](
		client, response, func(item *WithdrawableAmount, raw []byte) { item.Raw = raw },
	)
}

// CreateCoinWithdrawal은 API 출금용으로 등록된 주소로 가상자산 출금을 요청한다.
// 이 호출은 credential.PermissionWithdraw 권한을 요구하고 자동 재시도하지 않는다. 출금 API에는 멱등 키가 없으므로
// UNKNOWN_EXECUTION_STATE를 받으면 같은 요청을 다시 보내지 말고 RecentCoinWithdrawals로 접수 여부를 먼저 확인한다.
func (client *Client) CreateCoinWithdrawal(
	ctx context.Context,
	request CreateCoinWithdrawalRequest,
	options ...trade.RequestOption,
) (CoinWithdrawalReceipt, error) {
	if err := request.validate(); err != nil {
		return CoinWithdrawalReceipt{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v2/coin/withdrawal", request.values(), rateGroupAsset,
		credential.PermissionWithdraw, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CoinWithdrawalReceipt{}, err
	}
	var result CoinWithdrawalReceipt
	raw, err := client.decodeResponse(response, commonexchange.OperationMutation, &result)
	if err != nil {
		return CoinWithdrawalReceipt{}, err
	}
	if result.CoinWithdrawalID <= 0 {
		return CoinWithdrawalReceipt{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Korbit withdrawal response has no withdrawal ID"),
		)
	}
	result.Raw = raw
	return result, nil
}

// CancelCoinWithdrawal은 actionRequired 또는 reviewing 상태인 가상자산 출금의 취소를 요청한다.
// 이 호출은 credential.PermissionWithdraw 권한을 요구하고 자동 재시도하지 않는다.
func (client *Client) CancelCoinWithdrawal(
	ctx context.Context,
	request CancelCoinWithdrawalRequest,
	options ...trade.RequestOption,
) (CancelResult, error) {
	if err := request.validate(); err != nil {
		return CancelResult{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, "/v2/coin/withdrawal", request.values(), rateGroupAsset,
		credential.PermissionWithdraw, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelResult{}, err
	}
	if _, err := client.decodeResponse(response, commonexchange.OperationMutation, nil); err != nil {
		return CancelResult{}, err
	}
	return CancelResult{Accepted: true, Raw: cloneBytes(response.Body)}, nil
}

// CoinWithdrawal은 출금 ID로 가상자산 출금 한 건의 상태를 조회한다.
func (client *Client) CoinWithdrawal(
	ctx context.Context,
	request CoinWithdrawalRequest,
	options ...trade.RequestOption,
) (CoinWithdrawal, error) {
	if err := request.validate(); err != nil {
		return CoinWithdrawal{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/coin/withdrawal", request.values(), rateGroupAsset,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return CoinWithdrawal{}, err
	}
	var result CoinWithdrawal
	raw, err := client.decodeResponse(response, commonexchange.OperationRead, &result)
	if err != nil {
		return CoinWithdrawal{}, err
	}
	if result.ID <= 0 {
		return CoinWithdrawal{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Korbit coin withdrawal has no ID"),
		)
	}
	result.Raw = raw
	return result, nil
}

// RecentCoinWithdrawals는 자산별 최근 가상자산 출금 목록을 조회한다.
func (client *Client) RecentCoinWithdrawals(
	ctx context.Context,
	request RecentCoinWithdrawalsRequest,
	options ...trade.RequestOption,
) ([]CoinWithdrawal, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/coin/recentWithdrawals", request.values(), rateGroupAsset,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[CoinWithdrawal](client, response, func(item *CoinWithdrawal, raw []byte) { item.Raw = raw })
}
