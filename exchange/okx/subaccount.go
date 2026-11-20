package okx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// 서브계정 endpoint는 마스터 계정 API Key로만 호출할 수 있다.

// SubAccountListRequest는 서브계정 목록 조회 조건이다. Enable은 nil이면 전체다.
type SubAccountListRequest struct {
	Enable     *bool
	SubAccount string
	After      *time.Time
	Before     *time.Time
	Limit      int
}

// SubAccount는 서브계정 정보다.
type SubAccount struct {
	Type            string          `json:"type"`
	Enable          bool            `json:"enable"`
	SubAccount      string          `json:"subAcct"`
	UID             string          `json:"uid"`
	Label           string          `json:"label"`
	Mobile          string          `json:"mobile"`
	GoogleAuth      bool            `json:"gAuth"`
	FrozenFunctions []string        `json:"frozenFunc"`
	CanTransferOut  bool            `json:"canTransOut"`
	Timestamp       string          `json:"ts"`
	Level           string          `json:"subAcctLv"`
	FirstLevelAcct  string          `json:"firstLvSubAcct"`
	IsDMA           bool            `json:"ifDma"`
	Raw             json.RawMessage `json:"-"`
}

func (request SubAccountListRequest) validate() error {
	if err := validateOptionalText("subAcct", request.SubAccount); err != nil {
		return err
	}
	if err := extValidatePageTimes("sub-account list", request.After, request.Before); err != nil {
		return err
	}
	return extValidateLimit("sub-account list", request.Limit, 100)
}

func (request SubAccountListRequest) values() url.Values {
	values := make(url.Values)
	if request.Enable != nil {
		if *request.Enable {
			values.Set("enable", "true")
		} else {
			values.Set("enable", "false")
		}
	}
	setIfNotEmpty(values, "subAcct", request.SubAccount)
	setTime(values, "after", request.After)
	setTime(values, "before", request.Before)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

// SubAccounts는 마스터 계정의 서브계정 목록을 조회한다.
func (client *Client) SubAccounts(
	ctx context.Context,
	request SubAccountListRequest,
	options ...trade.RequestOption,
) ([]SubAccount, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/users/subaccount/list", request.values(), nil,
		assetLimit(20, 2*time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *SubAccount, raw []byte) { item.Raw = raw }, options...,
	)
}

// SubAccountAPIKeyModifyRequest는 서브계정 API Key 수정 요청이다.
// Permissions는 read_only, trade, withdraw 중 하나 이상이며 IPs는 쉼표로 연결해 전송한다.
type SubAccountAPIKeyModifyRequest struct {
	SubAccount  string
	APIKey      string
	Label       string
	Permissions []string
	IPs         []string
}

type subAccountAPIKeyModifyBody struct {
	SubAccount  string `json:"subAcct"`
	APIKey      string `json:"apiKey"`
	Label       string `json:"label,omitempty"`
	Permissions string `json:"perm,omitempty"`
	IPs         string `json:"ip,omitempty"`
}

// SubAccountAPIKey는 수정된 서브계정 API Key 정보다.
type SubAccountAPIKey struct {
	SubAccount  string          `json:"subAcct"`
	Label       string          `json:"label"`
	APIKey      string          `json:"apiKey"`
	Permissions string          `json:"perm"`
	IPs         string          `json:"ip"`
	Timestamp   string          `json:"ts"`
	Raw         json.RawMessage `json:"-"`
}

func (request SubAccountAPIKeyModifyRequest) body() (subAccountAPIKeyModifyBody, error) {
	if err := validateRequiredText("subAcct", request.SubAccount); err != nil {
		return subAccountAPIKeyModifyBody{}, err
	}
	if err := validateRequiredText("apiKey", request.APIKey); err != nil {
		return subAccountAPIKeyModifyBody{}, err
	}
	if err := validateOptionalText("label", request.Label); err != nil {
		return subAccountAPIKeyModifyBody{}, err
	}
	permissions, err := extJoinCSV("perm", request.Permissions, 3)
	if err != nil {
		return subAccountAPIKeyModifyBody{}, err
	}
	for _, permission := range request.Permissions {
		if permission != "read_only" && permission != "trade" && permission != "withdraw" {
			return subAccountAPIKeyModifyBody{}, validationError("unsupported API key permission %q", permission)
		}
	}
	ips, err := extJoinCSV("ip", request.IPs, 20)
	if err != nil {
		return subAccountAPIKeyModifyBody{}, err
	}
	return subAccountAPIKeyModifyBody{
		SubAccount: request.SubAccount, APIKey: request.APIKey, Label: request.Label,
		Permissions: permissions, IPs: ips,
	}, nil
}

// ModifySubAccountAPIKey는 서브계정 API Key의 label·권한·허용 IP를 수정한다.
// API Key 관리 작업이므로 PermissionTransfer가 필요하며 자동 재시도하지 않는다.
func (client *Client) ModifySubAccountAPIKey(
	ctx context.Context,
	request SubAccountAPIKeyModifyRequest,
	options ...trade.RequestOption,
) (SubAccountAPIKey, error) {
	payload, err := request.body()
	if err != nil {
		return SubAccountAPIKey{}, err
	}
	body, err := encodeBody(payload)
	if err != nil {
		return SubAccountAPIKey{}, err
	}
	return extSignedOne(
		ctx, client, http.MethodPost, "/api/v5/users/subaccount/modify-apikey", nil, body,
		assetLimit(1, time.Second, ""), credential.PermissionTransfer, commonexchange.OperationMutation,
		func(item *SubAccountAPIKey, raw []byte) { item.Raw = raw }, options...,
	)
}

// SubAccountTransferOutResult는 서브계정 이체 권한 설정 결과다.
type SubAccountTransferOutResult struct {
	SubAccount     string          `json:"subAcct"`
	CanTransferOut bool            `json:"canTransOut"`
	Raw            json.RawMessage `json:"-"`
}

// SetSubAccountTransferOut은 서브계정이 직접 이체할 수 있는 권한을 설정한다. 서브계정은 여러 개를 지정할 수 있다.
func (client *Client) SetSubAccountTransferOut(
	ctx context.Context,
	subAccounts []string,
	canTransferOut bool,
	options ...trade.RequestOption,
) ([]SubAccountTransferOutResult, error) {
	if len(subAccounts) == 0 {
		return nil, validationError("subAcct is required")
	}
	joined, err := extJoinCSV("subAcct", subAccounts, 100)
	if err != nil {
		return nil, err
	}
	body, err := encodeBody(struct {
		SubAccount     string `json:"subAcct"`
		CanTransferOut bool   `json:"canTransOut"`
	}{SubAccount: joined, CanTransferOut: canTransferOut})
	if err != nil {
		return nil, err
	}
	return extSignedList(
		ctx, client, http.MethodPost, "/api/v5/users/subaccount/set-transfer-out", nil, body,
		assetLimit(1, time.Second, ""), credential.PermissionTransfer, commonexchange.OperationMutation,
		func(item *SubAccountTransferOutResult, raw []byte) { item.Raw = raw }, options...,
	)
}

// EntrustSubAccount는 수탁 거래 서브계정이다.
type EntrustSubAccount struct {
	SubAccount string          `json:"subAcct"`
	Raw        json.RawMessage `json:"-"`
}

// EntrustSubAccounts는 거래팀 마스터 계정이 위탁받은 서브계정 목록을 조회한다. subAccount는 선택이다.
func (client *Client) EntrustSubAccounts(
	ctx context.Context,
	subAccount string,
	options ...trade.RequestOption,
) ([]EntrustSubAccount, error) {
	if err := validateOptionalText("subAcct", subAccount); err != nil {
		return nil, err
	}
	values := make(url.Values)
	setIfNotEmpty(values, "subAcct", subAccount)
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/users/entrust-subaccount-list", values, nil,
		assetLimit(1, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *EntrustSubAccount, raw []byte) { item.Raw = raw }, options...,
	)
}

// SubAccountBalances는 서브계정 거래 계정의 상세 잔고를 조회한다.
func (client *Client) SubAccountBalances(
	ctx context.Context,
	subAccount string,
	options ...trade.RequestOption,
) (Balance, error) {
	if err := validateRequiredText("subAcct", subAccount); err != nil {
		return Balance{}, err
	}
	values := make(url.Values)
	values.Set("subAcct", subAccount)
	return extSignedOne(
		ctx, client, http.MethodGet, "/api/v5/account/subaccount/balances", values, nil,
		assetLimit(6, 2*time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *Balance, raw []byte) { item.Raw = raw }, options...,
	)
}

// SubAccountAssetBalances는 서브계정 펀딩 계정 잔고를 조회한다. Currencies는 최대 20개다.
func (client *Client) SubAccountAssetBalances(
	ctx context.Context,
	subAccount string,
	currencies []string,
	options ...trade.RequestOption,
) ([]AssetBalance, error) {
	if err := validateRequiredText("subAcct", subAccount); err != nil {
		return nil, err
	}
	joined, err := validateCurrencyList("ccy", currencies, 20)
	if err != nil {
		return nil, err
	}
	values := make(url.Values)
	values.Set("subAcct", subAccount)
	setIfNotEmpty(values, "ccy", joined)
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/asset/subaccount/balances", values, nil,
		assetLimit(6, 2*time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *AssetBalance, raw []byte) { item.Raw = raw }, options...,
	)
}

// SubAccountMaxWithdrawal은 서브계정 통화별 최대 출금 가능액이다.
type SubAccountMaxWithdrawal struct {
	Currency        string          `json:"ccy"`
	MaxWithdrawal   string          `json:"maxWd"`
	MaxWithdrawalEx string          `json:"maxWdEx"`
	SpotOffsetMax   string          `json:"spotOffsetMaxWd"`
	SpotOffsetMaxEx string          `json:"spotOffsetMaxWdEx"`
	Raw             json.RawMessage `json:"-"`
}

// SubAccountMaxWithdrawals는 서브계정의 최대 출금 가능액을 조회한다. Currencies는 최대 20개다.
func (client *Client) SubAccountMaxWithdrawals(
	ctx context.Context,
	subAccount string,
	currencies []string,
	options ...trade.RequestOption,
) ([]SubAccountMaxWithdrawal, error) {
	if err := validateRequiredText("subAcct", subAccount); err != nil {
		return nil, err
	}
	joined, err := validateCurrencyList("ccy", currencies, 20)
	if err != nil {
		return nil, err
	}
	values := make(url.Values)
	values.Set("subAcct", subAccount)
	setIfNotEmpty(values, "ccy", joined)
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/account/subaccount/max-withdrawal", values, nil,
		assetLimit(20, 2*time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *SubAccountMaxWithdrawal, raw []byte) { item.Raw = raw }, options...,
	)
}

// SubAccountBillsRequest는 서브계정 이체 내역 조회 조건이다.
// Type은 0(마스터→서브), 1(서브→마스터)이고 SubUID는 위탁 서브계정 조회에서만 사용한다.
type SubAccountBillsRequest struct {
	Currency   string
	Type       string
	SubAccount string
	SubUID     string
	After      string
	Before     string
	Limit      int
}

// SubAccountBill은 서브계정 이체 내역 한 건이다.
type SubAccountBill struct {
	BillID     string          `json:"billId"`
	Type       string          `json:"type"`
	Currency   string          `json:"ccy"`
	Amount     string          `json:"amt"`
	SubAccount string          `json:"subAcct"`
	SubUID     string          `json:"subUid"`
	Timestamp  string          `json:"ts"`
	Raw        json.RawMessage `json:"-"`
}

func (request SubAccountBillsRequest) validate(managed bool) error {
	for name, value := range map[string]string{
		"ccy": request.Currency, "subAcct": request.SubAccount, "after": request.After, "before": request.Before,
		"subUid": request.SubUID,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return err
		}
	}
	if request.SubUID != "" && !managed {
		return validationError("subUid is only supported by ManagedSubAccountBills")
	}
	if request.Type != "" && request.Type != "0" && request.Type != "1" {
		return validationError("sub-account bill type must be 0 or 1")
	}
	return extValidateLimit("sub-account bill", request.Limit, 100)
}

func (request SubAccountBillsRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "ccy", request.Currency)
	setIfNotEmpty(values, "type", request.Type)
	setIfNotEmpty(values, "subAcct", request.SubAccount)
	setIfNotEmpty(values, "subUid", request.SubUID)
	setIfNotEmpty(values, "after", request.After)
	setIfNotEmpty(values, "before", request.Before)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

// SubAccountBills는 마스터 계정이 서브계정 이체 내역을 조회한다. 2022-09-28 이후 기록만 제공된다.
func (client *Client) SubAccountBills(
	ctx context.Context,
	request SubAccountBillsRequest,
	options ...trade.RequestOption,
) ([]SubAccountBill, error) {
	if err := request.validate(false); err != nil {
		return nil, err
	}
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/asset/subaccount/bills", request.values(), nil,
		assetLimit(6, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *SubAccountBill, raw []byte) { item.Raw = raw }, options...,
	)
}

// ManagedSubAccountBills는 거래팀 마스터 계정이 위탁받은 서브계정의 이체 내역을 조회한다.
func (client *Client) ManagedSubAccountBills(
	ctx context.Context,
	request SubAccountBillsRequest,
	options ...trade.RequestOption,
) ([]SubAccountBill, error) {
	if err := request.validate(true); err != nil {
		return nil, err
	}
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/asset/subaccount/managed-subaccount-bills", request.values(), nil,
		assetLimit(6, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *SubAccountBill, raw []byte) { item.Raw = raw }, options...,
	)
}

// SubAccountTransferRequest는 마스터 API Key로 서브계정 사이 자금을 옮기는 요청이다.
type SubAccountTransferRequest struct {
	Currency         string       `json:"ccy"`
	Amount           string       `json:"amt"`
	From             AssetAccount `json:"from"`
	To               AssetAccount `json:"to"`
	FromSubAccount   string       `json:"fromSubAccount"`
	ToSubAccount     string       `json:"toSubAccount"`
	LoanTransfer     bool         `json:"loanTrans,omitempty"`
	OmitPositionRisk bool         `json:"-"`
}

type subAccountTransferBody struct {
	SubAccountTransferRequest
	OmitPosRisk string `json:"omitPosRisk,omitempty"`
}

// SubAccountTransferResult는 서브계정 간 이체 접수 결과다.
type SubAccountTransferResult struct {
	TransferID string          `json:"transId"`
	Raw        json.RawMessage `json:"-"`
}

func (request SubAccountTransferRequest) validate() error {
	if err := validateRequiredText("ccy", request.Currency); err != nil {
		return err
	}
	if err := validatePositiveDecimal("amt", request.Amount); err != nil {
		return err
	}
	if !request.From.valid() || !request.To.valid() {
		return validationError("transfer from and to must be funding (6) or trading (18)")
	}
	if err := validateRequiredText("fromSubAccount", request.FromSubAccount); err != nil {
		return err
	}
	if err := validateRequiredText("toSubAccount", request.ToSubAccount); err != nil {
		return err
	}
	if strings.EqualFold(request.FromSubAccount, request.ToSubAccount) {
		return validationError("fromSubAccount and toSubAccount must differ")
	}
	return nil
}

// TransferBetweenSubAccounts는 마스터 계정 API Key로 서브계정 사이에서 자금을 이체한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 SubAccountBills로 반영 여부를 확인해야 한다.
func (client *Client) TransferBetweenSubAccounts(
	ctx context.Context,
	request SubAccountTransferRequest,
	options ...trade.RequestOption,
) (SubAccountTransferResult, error) {
	if err := request.validate(); err != nil {
		return SubAccountTransferResult{}, err
	}
	payload := subAccountTransferBody{SubAccountTransferRequest: request}
	if request.OmitPositionRisk {
		payload.OmitPosRisk = "true"
	}
	body, err := encodeBody(payload)
	if err != nil {
		return SubAccountTransferResult{}, err
	}
	return extSignedOne(
		ctx, client, http.MethodPost, "/api/v5/asset/subaccount/transfer", nil, body,
		assetLimit(1, time.Second, ""), credential.PermissionTransfer, commonexchange.OperationMutation,
		func(item *SubAccountTransferResult, raw []byte) { item.Raw = raw }, options...,
	)
}
