package gateio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// TransferAccount는 계정 간 이체와 서브계정 이체의 거래 계정 종류다.
type TransferAccount string

const (
	TransferAccountSpot        TransferAccount = "spot"
	TransferAccountMargin      TransferAccount = "margin"
	TransferAccountFutures     TransferAccount = "futures"
	TransferAccountDelivery    TransferAccount = "delivery"
	TransferAccountCrossMargin TransferAccount = "cross_margin"
	TransferAccountOptions     TransferAccount = "options"
	TransferAccountUnified     TransferAccount = "unified"
)

func (account TransferAccount) valid() bool {
	switch account {
	case TransferAccountSpot, TransferAccountMargin, TransferAccountFutures, TransferAccountDelivery,
		TransferAccountCrossMargin, TransferAccountOptions, TransferAccountUnified:
		return true
	default:
		return false
	}
}

// TransferTx는 이체 접수 결과의 거래 번호다.
type TransferTx struct {
	TxID int64           `json:"tx_id"`
	Raw  json.RawMessage `json:"-"`
}

func (tx *TransferTx) setRaw(raw json.RawMessage) { tx.Raw = raw }

func decodeTransferTx(
	client *Client,
	response commonexchange.Response,
) (TransferTx, error) {
	tx, err := decodeObject[TransferTx](client, response, commonexchange.OperationMutation)
	if err != nil {
		return TransferTx{}, err
	}
	if tx.TxID == 0 {
		return TransferTx{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Gate.io transfer acknowledgement is empty"),
		)
	}
	return tx, nil
}

// TransferRequest는 본인 거래 계정 사이 이체 요청이다.
// 마진 계정이 한쪽이면 CurrencyPair, 선물·인도 계정이 한쪽이면 Settle이 필요하다.
type TransferRequest struct {
	Currency     string          `json:"currency"`
	From         TransferAccount `json:"from"`
	To           TransferAccount `json:"to"`
	Amount       string          `json:"amount"`
	CurrencyPair string          `json:"currency_pair,omitempty"`
	Settle       string          `json:"settle,omitempty"`
}

func (request TransferRequest) validate() error {
	if !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid currency %q", request.Currency)
	}
	if !request.From.valid() || !request.To.valid() {
		return validationError("unsupported transfer account")
	}
	if request.From == request.To {
		return validationError("transfer source and destination must differ")
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return err
	}
	involves := func(account TransferAccount) bool { return request.From == account || request.To == account }
	if involves(TransferAccountMargin) {
		if err := validateCurrencyPair(request.CurrencyPair); err != nil {
			return err
		}
	} else if request.CurrencyPair != "" {
		return validationError("currency pair is only valid for margin transfers")
	}
	if involves(TransferAccountFutures) || involves(TransferAccountDelivery) {
		if !currencyPattern.MatchString(strings.ToUpper(request.Settle)) {
			return validationError("settle currency is required for futures or delivery transfers")
		}
	} else if request.Settle != "" {
		return validationError("settle is only valid for futures or delivery transfers")
	}
	return nil
}

// Transfer는 본인 거래 계정 사이에서 자산을 이체한다. 권한은 Transfer이며 자동 재시도하지 않는다.
// POST /wallet/transfers
func (client *Client) Transfer(
	ctx context.Context,
	request TransferRequest,
	options ...trade.RequestOption,
) (TransferTx, error) {
	if err := request.validate(); err != nil {
		return TransferTx{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return TransferTx{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/wallet/transfers", nil, body,
		privateLimit("transfer"), credential.PermissionTransfer,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return TransferTx{}, err
	}
	return decodeTransferTx(client, response)
}

// TransferDetail은 계정 간 이체 상세다.
type TransferDetail struct {
	TxID         string          `json:"tx_id"`
	Status       string          `json:"status"`
	Currency     string          `json:"currency"`
	Amount       string          `json:"amount"`
	FromAccount  string          `json:"from_account"`
	ToAccount    string          `json:"to_account"`
	Settle       *string         `json:"settle"`
	CurrencyPair *string         `json:"currency_pair"`
	Raw          json.RawMessage `json:"-"`
}

func (detail *TransferDetail) setRaw(raw json.RawMessage) { detail.Raw = raw }

// TransferInfo는 Transfer가 반환한 tx_id로 계정 간 이체 상세를 조회한다. 권한은 Read다.
// GET /wallet/transfers
func (client *Client) TransferInfo(
	ctx context.Context,
	txID string,
	options ...trade.RequestOption,
) (TransferDetail, error) {
	if !numericIDPattern.MatchString(txID) {
		return TransferDetail{}, validationError("invalid transfer tx ID")
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/wallet/transfers", url.Values{"tx_id": {txID}}, nil,
		privateLimit("transfer-info"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return TransferDetail{}, err
	}
	return decodeObject[TransferDetail](client, response, commonexchange.OperationRead)
}

// SubAccountTransferRequest는 메인계정과 서브계정 사이 이체 요청이다.
// Direction은 "to"(서브계정으로) 또는 "from"(서브계정에서)이다.
type SubAccountTransferRequest struct {
	SubAccount     string `json:"sub_account"`
	SubAccountType string `json:"sub_account_type,omitempty"`
	Currency       string `json:"currency"`
	Amount         string `json:"amount"`
	Direction      string `json:"direction"`
	ClientOrderID  string `json:"client_order_id,omitempty"`
}

func validSubAccountType(value string) bool {
	switch value {
	case "spot", "futures", "delivery", "cross_margin":
		return true
	default:
		return false
	}
}

func (request SubAccountTransferRequest) validate() error {
	if !numericIDPattern.MatchString(request.SubAccount) {
		return validationError("invalid sub-account user ID")
	}
	if request.SubAccountType != "" && !validSubAccountType(request.SubAccountType) {
		return validationError("unsupported sub-account type %q", request.SubAccountType)
	}
	if !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid currency %q", request.Currency)
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return err
	}
	if request.Direction != "to" && request.Direction != "from" {
		return validationError("transfer direction must be to or from")
	}
	if request.ClientOrderID != "" && !clientTransferIDPattern.MatchString(request.ClientOrderID) {
		return validationError("invalid client order ID")
	}
	return nil
}

// TransferWithSubAccount는 메인계정과 서브계정 사이에서 자산을 이체한다.
// 권한은 Transfer이며 자동 재시도하지 않는다. 불명확한 결과는 ClientOrderID로 TransferOrderStatus를 조회한다.
// POST /wallet/sub_account_transfers
func (client *Client) TransferWithSubAccount(
	ctx context.Context,
	request SubAccountTransferRequest,
	options ...trade.RequestOption,
) (TransferTx, error) {
	if err := request.validate(); err != nil {
		return TransferTx{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return TransferTx{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/wallet/sub_account_transfers", nil, body,
		privateLimit("sub-account-transfer"), credential.PermissionTransfer,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return TransferTx{}, err
	}
	return decodeTransferTx(client, response)
}

// SubAccountToSubAccountRequest는 같은 메인계정의 서브계정 사이 이체 요청이다.
type SubAccountToSubAccountRequest struct {
	Currency           string `json:"currency"`
	SubAccountFrom     string `json:"sub_account_from"`
	SubAccountFromType string `json:"sub_account_from_type"`
	SubAccountTo       string `json:"sub_account_to"`
	SubAccountToType   string `json:"sub_account_to_type"`
	Amount             string `json:"amount"`
}

func (request SubAccountToSubAccountRequest) validate() error {
	if !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid currency %q", request.Currency)
	}
	if !numericIDPattern.MatchString(request.SubAccountFrom) ||
		!numericIDPattern.MatchString(request.SubAccountTo) {
		return validationError("invalid sub-account user ID")
	}
	if !validSubAccountType(request.SubAccountFromType) || !validSubAccountType(request.SubAccountToType) {
		return validationError("unsupported sub-account type")
	}
	return validatePositiveDecimal("amount", request.Amount)
}

// SubAccountToSubAccount는 같은 메인계정의 두 서브계정 사이에서 자산을 이체한다.
// 권한은 Transfer이며 자동 재시도하지 않는다.
// POST /wallet/sub_account_to_sub_account
func (client *Client) SubAccountToSubAccount(
	ctx context.Context,
	request SubAccountToSubAccountRequest,
	options ...trade.RequestOption,
) (TransferTx, error) {
	if err := request.validate(); err != nil {
		return TransferTx{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return TransferTx{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/wallet/sub_account_to_sub_account", nil, body,
		privateLimit("sub-account-to-sub-account"), credential.PermissionTransfer,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return TransferTx{}, err
	}
	return decodeTransferTx(client, response)
}

// SubAccountTransferRecord는 메인·서브 이체 이력 한 건이다.
type SubAccountTransferRecord struct {
	Timestamp      string          `json:"timest"`
	UID            string          `json:"uid"`
	SubAccount     string          `json:"sub_account"`
	SubAccountType string          `json:"sub_account_type"`
	Currency       string          `json:"currency"`
	Amount         string          `json:"amount"`
	Direction      string          `json:"direction"`
	Source         string          `json:"source"`
	ClientOrderID  string          `json:"client_order_id"`
	Status         string          `json:"status"`
	Raw            json.RawMessage `json:"-"`
}

func (record *SubAccountTransferRecord) setRaw(raw json.RawMessage) { record.Raw = raw }

// SubAccountTransfersRequest는 메인·서브 이체 이력 조회 조건이다. SubUIDs를 비우면 전체 서브계정이며
// From을 생략하면 최근 7일이다.
type SubAccountTransfersRequest struct {
	SubUIDs []string
	From    *time.Time
	To      *time.Time
	Limit   int
	Offset  int
}

func (request SubAccountTransfersRequest) validate() error {
	for _, uid := range request.SubUIDs {
		if !numericIDPattern.MatchString(uid) {
			return validationError("invalid sub-account user ID")
		}
	}
	if err := validateTimeRange(request.From, request.To); err != nil {
		return err
	}
	return validateOffsetLimit(request.Offset, request.Limit, 1000)
}

// SubAccountTransfers는 메인계정과 서브계정 사이 이체 이력을 조회한다. 권한은 Read다.
// GET /wallet/sub_account_transfers
func (client *Client) SubAccountTransfers(
	ctx context.Context,
	request SubAccountTransfersRequest,
	options ...trade.RequestOption,
) ([]SubAccountTransferRecord, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	query := make(url.Values)
	if len(request.SubUIDs) > 0 {
		query.Set("sub_uid", strings.Join(request.SubUIDs, ","))
	}
	setUnixTime(query, "from", request.From)
	setUnixTime(query, "to", request.To)
	setPositiveInt(query, "limit", request.Limit)
	if request.Offset > 0 {
		query.Set("offset", strconv.Itoa(request.Offset))
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/wallet/sub_account_transfers", query, nil,
		privateLimit("sub-account-transfers"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[SubAccountTransferRecord](client, response, commonexchange.OperationRead)
}

// TransferOrderStatusRequest는 메인·서브 이체 상태 조회 조건이다. ClientOrderID와 TxID 중 하나가 필요하다.
type TransferOrderStatusRequest struct {
	ClientOrderID string
	TxID          string
}

func (request TransferOrderStatusRequest) validate() error {
	if request.ClientOrderID == "" && request.TxID == "" {
		return validationError("client order ID or tx ID is required")
	}
	if request.ClientOrderID != "" && !clientTransferIDPattern.MatchString(request.ClientOrderID) {
		return validationError("invalid client order ID")
	}
	if request.TxID != "" && !numericIDPattern.MatchString(request.TxID) {
		return validationError("invalid transfer tx ID")
	}
	return nil
}

// TransferOrderStatus는 메인·서브 이체의 처리 상태다.
type TransferOrderStatus struct {
	TxID   string          `json:"tx_id"`
	Status string          `json:"status"`
	Raw    json.RawMessage `json:"-"`
}

func (status *TransferOrderStatus) setRaw(raw json.RawMessage) { status.Raw = raw }

// TransferOrderStatus는 메인·서브 이체 상태를 조회한다. 권한은 Read다.
// GET /wallet/order_status
func (client *Client) TransferOrderStatus(
	ctx context.Context,
	request TransferOrderStatusRequest,
	options ...trade.RequestOption,
) (TransferOrderStatus, error) {
	if err := request.validate(); err != nil {
		return TransferOrderStatus{}, err
	}
	query := make(url.Values)
	if request.ClientOrderID != "" {
		query.Set("client_order_id", request.ClientOrderID)
	}
	if request.TxID != "" {
		query.Set("tx_id", request.TxID)
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/wallet/order_status", query, nil,
		privateLimit("transfer-order-status"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return TransferOrderStatus{}, err
	}
	return decodeObject[TransferOrderStatus](client, response, commonexchange.OperationRead)
}
