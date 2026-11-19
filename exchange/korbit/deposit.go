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

// CoinDepositAddressesRequest는 전체 가상자산 입금 주소 조회 조건이다.
// 입출금 API는 메인 계정만 지원하므로 AccountSeq는 0(생략)이나 1만 허용한다.
type CoinDepositAddressesRequest struct {
	AccountSeq int
}

// CoinDepositAddressRequest는 자산·네트워크별 입금 주소 한 건의 조회 조건이다.
// Network를 비우면 거래소 기본 네트워크를 쓰지만 기본값은 바뀔 수 있으므로 항상 지정하는 것을 권장한다.
type CoinDepositAddressRequest struct {
	AccountSeq int
	Currency   string
	Network    string
}

// CreateCoinDepositAddressRequest는 입금 주소 생성 조건이다. 이미 주소가 있으면 기존 주소를 반환한다.
// Network를 비우면 거래소 기본 네트워크를 쓰지만 기본값은 바뀔 수 있으므로 항상 지정하는 것을 권장한다.
type CreateCoinDepositAddressRequest struct {
	AccountSeq int
	Currency   string
	Network    string
}

// RecentCoinDepositsRequest는 자산별 최근 가상자산 입금 목록 조회 조건이다. Limit은 1~100이고 0이면 거래소 기본값을 쓴다.
type RecentCoinDepositsRequest struct {
	AccountSeq int
	Currency   string
	Limit      int
}

// CoinDepositRequest는 입금 ID로 가상자산 입금 한 건을 조회하는 조건이다.
type CoinDepositRequest struct {
	AccountSeq    int
	Currency      string
	CoinDepositID int64
}

// CoinDepositAddress는 자산·네트워크별 입금 주소다. SecondaryAddress는 destination tag나 memo이며 없으면 빈 문자열이다.
type CoinDepositAddress struct {
	Currency         string          `json:"currency"`
	Network          string          `json:"network"`
	Address          string          `json:"address"`
	SecondaryAddress string          `json:"secondaryAddress"`
	Raw              json.RawMessage `json:"-"`
}

// CoinDepositStatus는 가상자산 입금 처리 상태다.
type CoinDepositStatus string

const (
	CoinDepositStatusPending        CoinDepositStatus = "pending"
	CoinDepositStatusActionRequired CoinDepositStatus = "actionRequired"
	CoinDepositStatusReviewing      CoinDepositStatus = "reviewing"
	CoinDepositStatusDone           CoinDepositStatus = "done"
	CoinDepositStatusRefunded       CoinDepositStatus = "refunded"
	CoinDepositStatusFailed         CoinDepositStatus = "failed"
)

// CoinDeposit은 가상자산 입금 한 건이다. FilledAt은 잔고에 반영된 시각이며 진행 중이면 nil이다.
type CoinDeposit struct {
	ID               int64             `json:"id"`
	Network          string            `json:"network"`
	Address          string            `json:"address"`
	SecondaryAddress string            `json:"secondaryAddress"`
	Status           CoinDepositStatus `json:"status"`
	TransactionHash  string            `json:"transactionHash"`
	Currency         string            `json:"currency"`
	Quantity         string            `json:"quantity"`
	CreatedAt        int64             `json:"createdAt"`
	FilledAt         *int64            `json:"filledAt"`
	Raw              json.RawMessage   `json:"-"`
}

func (request CoinDepositAddressesRequest) validate() error {
	return validateMainAccountSeq(request.AccountSeq)
}

func (request CoinDepositAddressesRequest) values() url.Values {
	values := make(url.Values)
	setAccountSeq(values, request.AccountSeq)
	return values
}

func (request CoinDepositAddressRequest) validate() error {
	return validateCurrencyNetwork(request.AccountSeq, request.Currency, request.Network)
}

func (request CoinDepositAddressRequest) values() url.Values {
	return currencyNetworkValues(request.AccountSeq, request.Currency, request.Network)
}

func (request CreateCoinDepositAddressRequest) validate() error {
	return validateCurrencyNetwork(request.AccountSeq, request.Currency, request.Network)
}

func (request CreateCoinDepositAddressRequest) values() url.Values {
	return currencyNetworkValues(request.AccountSeq, request.Currency, request.Network)
}

func (request RecentCoinDepositsRequest) validate() error {
	if err := validateMainAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	return validateOptionalLimit("recent coin deposit", request.Limit, 100)
}

func (request RecentCoinDepositsRequest) values() url.Values {
	values := url.Values{"currency": {request.Currency}}
	setAccountSeq(values, request.AccountSeq)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request CoinDepositRequest) validate() error {
	if err := validateMainAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	return validatePositiveID("coin deposit ID", request.CoinDepositID)
}

func (request CoinDepositRequest) values() url.Values {
	values := url.Values{
		"currency":      {request.Currency},
		"coinDepositId": {strconv.FormatInt(request.CoinDepositID, 10)},
	}
	setAccountSeq(values, request.AccountSeq)
	return values
}

func validateCurrencyNetwork(accountSeq int, currency, network string) error {
	if err := validateMainAccountSeq(accountSeq); err != nil {
		return err
	}
	if err := validateCurrency(currency); err != nil {
		return err
	}
	return validateOptionalNetwork(network)
}

func currencyNetworkValues(accountSeq int, currency, network string) url.Values {
	values := url.Values{"currency": {currency}}
	setAccountSeq(values, accountSeq)
	setIfNotEmpty(values, "network", network)
	return values
}

// CoinDepositAddresses는 메인 계정에 발급된 모든 가상자산 입금 주소를 조회한다.
func (client *Client) CoinDepositAddresses(
	ctx context.Context,
	request CoinDepositAddressesRequest,
	options ...trade.RequestOption,
) ([]CoinDepositAddress, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/coin/depositAddresses", request.values(), rateGroupAsset,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[CoinDepositAddress](
		client, response, func(item *CoinDepositAddress, raw []byte) { item.Raw = raw },
	)
}

// CoinDepositAddress는 자산·네트워크별 입금 주소 한 건을 조회한다.
func (client *Client) CoinDepositAddress(
	ctx context.Context,
	request CoinDepositAddressRequest,
	options ...trade.RequestOption,
) (CoinDepositAddress, error) {
	if err := request.validate(); err != nil {
		return CoinDepositAddress{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/coin/depositAddress", request.values(), rateGroupAsset,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return CoinDepositAddress{}, err
	}
	var result CoinDepositAddress
	raw, err := client.decodeResponse(response, commonexchange.OperationRead, &result)
	if err != nil {
		return CoinDepositAddress{}, err
	}
	if result.Address == "" {
		return CoinDepositAddress{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Korbit deposit address response has no address"),
		)
	}
	result.Raw = raw
	return result, nil
}

// CreateCoinDepositAddress는 입금 주소를 생성한다. 이미 주소가 있으면 기존 주소를 반환한다.
// 변경 요청이므로 자동 재시도하지 않고, 전송 결과가 불명확하면 UNKNOWN_EXECUTION_STATE로 반환한다.
// 이 호출은 credential.PermissionTransfer 권한을 요구한다.
func (client *Client) CreateCoinDepositAddress(
	ctx context.Context,
	request CreateCoinDepositAddressRequest,
	options ...trade.RequestOption,
) (CoinDepositAddress, error) {
	if err := request.validate(); err != nil {
		return CoinDepositAddress{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v2/coin/depositAddress", request.values(), rateGroupAsset,
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CoinDepositAddress{}, err
	}
	var result CoinDepositAddress
	raw, err := client.decodeResponse(response, commonexchange.OperationMutation, &result)
	if err != nil {
		return CoinDepositAddress{}, err
	}
	if result.Address == "" {
		return CoinDepositAddress{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Korbit deposit address response has no address"),
		)
	}
	result.Raw = raw
	return result, nil
}

// RecentCoinDeposits는 자산별 최근 가상자산 입금 목록을 조회한다.
func (client *Client) RecentCoinDeposits(
	ctx context.Context,
	request RecentCoinDepositsRequest,
	options ...trade.RequestOption,
) ([]CoinDeposit, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/coin/recentDeposits", request.values(), rateGroupAsset,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[CoinDeposit](client, response, func(item *CoinDeposit, raw []byte) { item.Raw = raw })
}

// CoinDeposit은 입금 ID로 가상자산 입금 한 건의 상태를 조회한다.
func (client *Client) CoinDeposit(
	ctx context.Context,
	request CoinDepositRequest,
	options ...trade.RequestOption,
) (CoinDeposit, error) {
	if err := request.validate(); err != nil {
		return CoinDeposit{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/coin/deposit", request.values(), rateGroupAsset,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return CoinDeposit{}, err
	}
	var result CoinDeposit
	raw, err := client.decodeResponse(response, commonexchange.OperationRead, &result)
	if err != nil {
		return CoinDeposit{}, err
	}
	if result.ID <= 0 {
		return CoinDeposit{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Korbit coin deposit has no ID"),
		)
	}
	result.Raw = raw
	return result, nil
}
