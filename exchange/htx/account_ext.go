package htx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

var (
	userIDPattern            = regexp.MustCompile(`^[0-9]{1,20}$`)
	valuationCurrencyPattern = regexp.MustCompile(`^[A-Z]{2,10}$`)
	accountTypePattern       = regexp.MustCompile(`^[0-9A-Za-z-]{1,32}$`)
	accessKeyPattern         = regexp.MustCompile(`^[0-9A-Za-z_-]{1,128}$`)
)

const (
	maximumAccountHistorySize   = 500
	maximumAccountLedgerLimit   = 500
	maximumAccountHistoryWindow = time.Hour
	maximumAccountLedgerWindow  = 10 * 24 * time.Hour
	maximumPointDecimalPlaces   = 8
)

// AccountValuationRequest는 플랫폼 전체 자산 평가의 계정 유형과 평가 통화 조건이다.
// AccountType은 공식 문서의 계정 유형 코드표 값이며 비우면 전체 계정이다.
// ValuationCurrency를 비우면 BTC이며 현재 BTC만 지원된다.
type AccountValuationRequest struct {
	AccountType       string
	ValuationCurrency string
}

// AccountValuation은 플랫폼 전체 자산의 평가 총액과 당일 손익이다.
type AccountValuation struct {
	TotalBalance    Decimal
	TodayProfit     Decimal
	TodayProfitRate Decimal
	UpdatedToday    bool
	UpdatedAt       int64
	Balances        []AccountValuationBalance
	Raw             json.RawMessage
}

// AccountValuationBalance는 계정 유형별 평가 잔고다. Success가 false이면 잔고는 0이다.
type AccountValuationBalance struct {
	DistributionType Scalar          `json:"distributionType"`
	Balance          Decimal         `json:"balance"`
	AccountBalance   Decimal         `json:"accountBalance"`
	Success          bool            `json:"success"`
	Raw              json.RawMessage `json:"-"`
}

// AssetAccountType은 자산 평가를 조회할 계정 유형이다.
type AssetAccountType string

const (
	AssetAccountSpot        AssetAccountType = "spot"
	AssetAccountMargin      AssetAccountType = "margin"
	AssetAccountOTC         AssetAccountType = "otc"
	AssetAccountSuperMargin AssetAccountType = "super-margin"
)

// AssetValuationRequest는 계정 유형별 자산 평가의 계정 유형·평가 통화·서브유저 조건이다.
// SubUID를 비우면 API Key 소유자의 자산을 평가한다.
type AssetValuationRequest struct {
	AccountType       AssetAccountType
	ValuationCurrency string
	SubUID            string
}

// AssetValuation은 계정 유형별 자산의 평가 금액이다.
type AssetValuation struct {
	Balance   Decimal
	Timestamp int64
	Raw       json.RawMessage
}

// TransactType은 계정 자산 변동 이력의 변동 유형이다.
type TransactType string

const (
	TransactTypeTrade        TransactType = "trade"
	TransactTypeETF          TransactType = "etf"
	TransactTypeTransactFee  TransactType = "transact-fee"
	TransactTypeFeeDeduction TransactType = "fee-deduction"
	TransactTypeTransfer     TransactType = "transfer"
	TransactTypeCredit       TransactType = "credit"
	TransactTypeLiquidation  TransactType = "liquidation"
	TransactTypeInterest     TransactType = "interest"
	TransactTypeDeposit      TransactType = "deposit"
	TransactTypeWithdraw     TransactType = "withdraw"
	TransactTypeWithdrawFee  TransactType = "withdraw-fee"
	TransactTypeExchange     TransactType = "exchange"
	TransactTypeOtherTypes   TransactType = "other-types"
	TransactTypeRebate       TransactType = "rebate"
)

// AccountHistoryRequest는 계정 자산 변동 이력의 필터·시간 창·커서를 지정한다.
// 조회 창은 최대 1시간이며 최근 30일 안에서만 이동할 수 있다.
type AccountHistoryRequest struct {
	AccountID     string
	Currency      string
	TransactTypes []TransactType
	Start         *time.Time
	End           *time.Time
	Sort          SortOrder
	Size          int
	FromID        string
}

// AccountHistoryRecord는 계정 자산 변동 한 건이다. 수입은 양수, 지출은 음수다.
type AccountHistoryRecord struct {
	AccountID        Scalar          `json:"account-id"`
	Currency         string          `json:"currency"`
	RecordID         Scalar          `json:"record-id"`
	TransactAmount   Decimal         `json:"transact-amt"`
	TransactType     string          `json:"transact-type"`
	AvailableBalance Decimal         `json:"avail-balance"`
	AccountBalance   Decimal         `json:"acct-balance"`
	TransactTime     int64           `json:"transact-time"`
	Raw              json.RawMessage `json:"-"`
}

// AccountHistory는 계정 자산 변동 이력 한 쪽과 다음 쪽 커서다.
// NextID는 결과가 size를 넘을 때만 있으며 같은 조건의 다음 요청 FromID로 쓴다.
type AccountHistory struct {
	Records []AccountHistoryRecord
	NextID  Scalar
	Raw     json.RawMessage
}

// AccountLedgerRequest는 계정 원장(이체) 이력의 필터·시간 창·커서를 지정한다.
// 현재 공식 API는 이체(transfer) 변동만 지원하므로 변동 유형은 보내지 않는다.
// 조회 창은 최대 10일이며 최근 180일 안에서만 이동할 수 있다.
type AccountLedgerRequest struct {
	AccountID string
	Currency  string
	Start     *time.Time
	End       *time.Time
	Sort      SortOrder
	Limit     int
	FromID    string
}

// AccountLedgerRecord는 계정 원장의 이체 한 건이다. 수입은 양수, 지출은 음수다.
type AccountLedgerRecord struct {
	AccountID      Scalar          `json:"accountId"`
	Currency       string          `json:"currency"`
	TransactAmount Decimal         `json:"transactAmt"`
	TransactType   string          `json:"transactType"`
	TransferType   string          `json:"transferType"`
	TransactID     Scalar          `json:"transactId"`
	TransactTime   int64           `json:"transactTime"`
	Transferer     Scalar          `json:"transferer"`
	Transferee     Scalar          `json:"transferee"`
	Raw            json.RawMessage `json:"-"`
}

// AccountLedger는 계정 원장 한 쪽과 다음 쪽 커서다.
type AccountLedger struct {
	Records []AccountLedgerRecord
	NextID  Scalar
	Raw     json.RawMessage
}

// TransferAccountType은 계정 간 이체에서 쓰는 계정 유형이다.
type TransferAccountType string

const (
	TransferAccountSpot   TransferAccountType = "spot"
	TransferAccountMargin TransferAccountType = "margin"
)

// AccountTransferRequest는 부모·서브 사용자의 현물·마진 계정 사이 자산 이체 조건이다.
// 사용자 UID와 계정 ID는 모두 숫자 문자열이며 Amount는 양수 십진 문자열이다.
type AccountTransferRequest struct {
	FromUserID      string
	FromAccountType TransferAccountType
	FromAccountID   string
	ToUserID        string
	ToAccountType   TransferAccountType
	ToAccountID     string
	Currency        string
	Amount          string
}

// AccountTransferResult는 계정 간 이체 접수 결과다.
type AccountTransferResult struct {
	TransactID   Scalar          `json:"transact-id"`
	TransactTime int64           `json:"transact-time"`
	Raw          json.RawMessage `json:"-"`
}

// FuturesTransferType은 현물과 선물 계정 사이 이체 방향이다.
type FuturesTransferType string

const (
	// FuturesTransferToFutures는 현물 계정에서 선물 계정으로 옮긴다.
	FuturesTransferToFutures FuturesTransferType = "pro-to-futures"
	// FuturesTransferToSpot은 선물 계정에서 현물 계정으로 옮긴다.
	FuturesTransferToSpot FuturesTransferType = "futures-to-pro"
)

// FuturesTransferRequest는 현물과 선물 계약 계정 사이 자산 이체 조건이다.
type FuturesTransferRequest struct {
	Currency string
	Amount   string
	Type     FuturesTransferType
}

// FuturesTransferResult는 현물·선물 이체 접수 결과다.
type FuturesTransferResult struct {
	TransferID Scalar
	Raw        json.RawMessage
}

// PointAccountRequest는 포인트 잔고를 조회할 대상이다.
// SubUID는 부모 사용자가 서브 사용자의 포인트를 조회할 때만 지정한다.
type PointAccountRequest struct {
	SubUID string
}

// PointAccount는 포인트 계정 상태와 잔고, 유효 기간별 잔여 포인트다.
type PointAccount struct {
	AccountID     Scalar
	AccountStatus string
	Balance       Decimal
	Groups        []PointGroup
	Raw           json.RawMessage
}

// PointGroup은 유효 기간이 있는 포인트 묶음이다.
// GroupID 0은 기한 없는 포인트이며 이때 ExpiryDate는 0이다.
type PointGroup struct {
	GroupID         Scalar          `json:"groupId"`
	ExpiryDate      int64           `json:"expiryDate"`
	RemainingAmount Decimal         `json:"remainAmt"`
	Raw             json.RawMessage `json:"-"`
}

// PointTransferRequest는 부모와 서브 사용자 사이 포인트 이체 조건이다.
// GroupID 0은 기한 없는 포인트이며, 기한 있는 포인트를 회수하려면 먼저 서브 사용자의
// 포인트 잔고를 조회해 groupId를 얻어야 한다. Amount는 소수점 8자리까지 허용한다.
type PointTransferRequest struct {
	FromUID string
	ToUID   string
	GroupID int64
	Amount  string
}

// PointTransferResult는 포인트 이체 접수 결과다.
type PointTransferResult struct {
	TransactID   Scalar          `json:"transactId"`
	TransactTime int64           `json:"transactTime"`
	Raw          json.RawMessage `json:"-"`
}

// UserUID는 API Key 소유 사용자의 UID다.
type UserUID struct {
	UID Scalar
	Raw json.RawMessage
}

// APIKeysRequest는 UID가 소유한 API Key 정보 조회 조건이다.
// AccessKey를 비우면 UID의 모든 API Key를 반환한다.
type APIKeysRequest struct {
	UID       string
	AccessKey string
}

// APIKeyInfo는 API Key의 상태·권한·허용 IP·유효 기간이다. Secret Key는 포함하지 않는다.
// ValidDays가 -1이면 영구 유효다.
type APIKeyInfo struct {
	AccessKey   string          `json:"accessKey"`
	Status      string          `json:"status"`
	Note        string          `json:"note"`
	Permission  string          `json:"permission"`
	IPAddresses string          `json:"ipAddresses"`
	ValidDays   int             `json:"validDays"`
	CreateTime  int64           `json:"createTime"`
	UpdateTime  int64           `json:"updateTime"`
	Raw         json.RawMessage `json:"-"`
}

// AccountValuation은 플랫폼 계정 전체 자산을 BTC 기준으로 평가한 총액과 당일 손익을 조회한다.
func (client *Client) AccountValuation(
	ctx context.Context,
	request AccountValuationRequest,
	options ...trade.RequestOption,
) (AccountValuation, error) {
	if err := request.validate(); err != nil {
		return AccountValuation{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/account/valuation", request.values(), nil,
		rateGroupValuation, min(client.accountQuota, valuationRequestLimit),
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return AccountValuation{}, err
	}
	var wire struct {
		TotalBalance    Decimal `json:"totalBalance"`
		TodayProfit     Decimal `json:"todayProfit"`
		TodayProfitRate Decimal `json:"todayProfitRate"`
		Updated         struct {
			Success bool  `json:"success"`
			Time    int64 `json:"time"`
		} `json:"updated"`
		Balances []json.RawMessage `json:"profitAccountBalanceList"`
	}
	raw, err := client.decodeDataForOperation(response, commonexchange.OperationRead, &wire)
	if err != nil {
		return AccountValuation{}, err
	}
	value := AccountValuation{
		TotalBalance: wire.TotalBalance, TodayProfit: wire.TodayProfit,
		TodayProfitRate: wire.TodayProfitRate, UpdatedToday: wire.Updated.Success,
		UpdatedAt: wire.Updated.Time, Balances: make([]AccountValuationBalance, len(wire.Balances)),
		Raw: raw,
	}
	for index, balanceRaw := range wire.Balances {
		if err := json.Unmarshal(balanceRaw, &value.Balances[index]); err != nil {
			return AccountValuation{}, client.decodeBodyError(response, err)
		}
		value.Balances[index].Raw = cloneBytes(balanceRaw)
	}
	return value, nil
}

// AssetValuation은 지정한 계정 유형의 총자산을 BTC 또는 법정화폐로 평가한다.
func (client *Client) AssetValuation(
	ctx context.Context,
	request AssetValuationRequest,
	options ...trade.RequestOption,
) (AssetValuation, error) {
	if err := request.validate(); err != nil {
		return AssetValuation{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/account/asset-valuation", request.values(), nil,
		rateGroupAccount, client.accountQuota, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return AssetValuation{}, err
	}
	var wire struct {
		Balance   Decimal `json:"balance"`
		Timestamp int64   `json:"timestamp"`
	}
	raw, err := client.decodeDataForOperation(response, commonexchange.OperationRead, &wire)
	if err != nil {
		return AssetValuation{}, err
	}
	return AssetValuation{Balance: wire.Balance, Timestamp: wire.Timestamp, Raw: raw}, nil
}

// AccountHistory는 계정의 자산 변동 이력을 최대 1시간 조회 창으로 조회한다.
func (client *Client) AccountHistory(
	ctx context.Context,
	request AccountHistoryRequest,
	options ...trade.RequestOption,
) (AccountHistory, error) {
	if err := request.validate(); err != nil {
		return AccountHistory{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/account/history", request.values(), nil,
		rateGroupAccountHistory, min(client.accountQuota, accountHistoryRequestLimit),
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return AccountHistory{}, err
	}
	var envelope struct {
		Data   []json.RawMessage `json:"data"`
		NextID Scalar            `json:"next-id"`
	}
	raw, err := client.decodeResponse(response, &envelope)
	if err != nil {
		return AccountHistory{}, err
	}
	value := AccountHistory{
		Records: make([]AccountHistoryRecord, len(envelope.Data)), NextID: envelope.NextID, Raw: raw,
	}
	for index, itemRaw := range envelope.Data {
		if err := json.Unmarshal(itemRaw, &value.Records[index]); err != nil {
			return AccountHistory{}, client.decodeBodyError(response, err)
		}
		value.Records[index].Raw = cloneBytes(itemRaw)
	}
	return value, nil
}

// AccountLedger는 계정 원장의 이체 이력을 최대 10일 조회 창으로 조회한다.
func (client *Client) AccountLedger(
	ctx context.Context,
	request AccountLedgerRequest,
	options ...trade.RequestOption,
) (AccountLedger, error) {
	if err := request.validate(); err != nil {
		return AccountLedger{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/account/ledger", request.values(), nil,
		rateGroupAccount, client.accountQuota, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return AccountLedger{}, err
	}
	var envelope struct {
		Data   []json.RawMessage `json:"data"`
		NextID Scalar            `json:"nextId"`
	}
	raw, err := client.decodeResponse(response, &envelope)
	if err != nil {
		return AccountLedger{}, err
	}
	value := AccountLedger{
		Records: make([]AccountLedgerRecord, len(envelope.Data)), NextID: envelope.NextID, Raw: raw,
	}
	for index, itemRaw := range envelope.Data {
		if err := json.Unmarshal(itemRaw, &value.Records[index]); err != nil {
			return AccountLedger{}, client.decodeBodyError(response, err)
		}
		value.Records[index].Raw = cloneBytes(itemRaw)
	}
	return value, nil
}

// AccountTransfer는 부모·서브 사용자의 현물·마진 계정 사이에서 자산을 이체한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환하므로
// 계정 원장 또는 잔고로 이체 여부를 확인해야 한다.
func (client *Client) AccountTransfer(
	ctx context.Context,
	request AccountTransferRequest,
	options ...trade.RequestOption,
) (AccountTransferResult, error) {
	wire, err := request.wire()
	if err != nil {
		return AccountTransferResult{}, err
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return AccountTransferResult{}, validationError("encode HTX account transfer: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v1/account/transfer", nil, body,
		rateGroupAccount, client.accountQuota, credential.PermissionTransfer,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return AccountTransferResult{}, err
	}
	var value AccountTransferResult
	raw, err := client.decodeDataForOperation(
		response, commonexchange.OperationMutation, &value,
	)
	if err != nil {
		return AccountTransferResult{}, err
	}
	if value.TransactID == "" {
		return AccountTransferResult{}, client.decodeBodyErrorForOperation(
			response, commonexchange.OperationMutation,
			errors.New("HTX account transfer acknowledgement is missing a transfer ID"),
		)
	}
	value.Raw = raw
	return value, nil
}

// FuturesTransfer는 현물 계정과 선물 계약 계정 사이에서 자산을 이체한다.
// 공식 한도는 분당 10회이며 전송 결과가 불명확하면 자동 재시도하지 않는다.
func (client *Client) FuturesTransfer(
	ctx context.Context,
	request FuturesTransferRequest,
	options ...trade.RequestOption,
) (FuturesTransferResult, error) {
	if err := request.validate(); err != nil {
		return FuturesTransferResult{}, err
	}
	body, err := json.Marshal(struct {
		Currency string              `json:"currency"`
		Amount   json.Number         `json:"amount"`
		Type     FuturesTransferType `json:"type"`
	}{Currency: request.Currency, Amount: json.Number(request.Amount), Type: request.Type})
	if err != nil {
		return FuturesTransferResult{}, validationError("encode HTX futures transfer: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v1/futures/transfer", nil, body,
		rateGroupFuturesTransfer, min(client.accountQuota, futuresTransferRequestLimit),
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return FuturesTransferResult{}, err
	}
	var transferID Scalar
	raw, err := client.decodeDataForOperation(
		response, commonexchange.OperationMutation, &transferID,
	)
	if err != nil {
		return FuturesTransferResult{}, err
	}
	if !orderIDPattern.MatchString(string(transferID)) {
		return FuturesTransferResult{}, client.decodeBodyErrorForOperation(
			response, commonexchange.OperationMutation,
			errors.New("HTX futures transfer acknowledgement has an invalid transfer ID"),
		)
	}
	return FuturesTransferResult{TransferID: transferID, Raw: raw}, nil
}

// PointAccount는 포인트 계정의 잔고와 유효 기간별 잔여 포인트를 조회한다.
func (client *Client) PointAccount(
	ctx context.Context,
	request PointAccountRequest,
	options ...trade.RequestOption,
) (PointAccount, error) {
	if err := request.validate(); err != nil {
		return PointAccount{}, err
	}
	query := make(url.Values)
	setIfNotEmpty(query, "subUid", request.SubUID)
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/point/account", query, nil,
		rateGroupPoint, min(client.accountQuota, pointRequestLimit),
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return PointAccount{}, err
	}
	var wire struct {
		AccountID     Scalar            `json:"accountId"`
		AccountStatus string            `json:"accountStatus"`
		Balance       Decimal           `json:"acctBalance"`
		Groups        []json.RawMessage `json:"groupIds"`
	}
	raw, err := client.decodeDataForOperation(response, commonexchange.OperationRead, &wire)
	if err != nil {
		return PointAccount{}, err
	}
	if wire.AccountID == "" {
		return PointAccount{}, client.decodeBodyError(
			response, errors.New("HTX point account is missing an account ID"),
		)
	}
	value := PointAccount{
		AccountID: wire.AccountID, AccountStatus: wire.AccountStatus, Balance: wire.Balance,
		Groups: make([]PointGroup, len(wire.Groups)), Raw: raw,
	}
	for index, groupRaw := range wire.Groups {
		if err := json.Unmarshal(groupRaw, &value.Groups[index]); err != nil {
			return PointAccount{}, client.decodeBodyError(response, err)
		}
		value.Groups[index].Raw = cloneBytes(groupRaw)
	}
	return value, nil
}

// PointTransfer는 부모와 서브 사용자 사이에서 포인트를 이체한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) PointTransfer(
	ctx context.Context,
	request PointTransferRequest,
	options ...trade.RequestOption,
) (PointTransferResult, error) {
	if err := request.validate(); err != nil {
		return PointTransferResult{}, err
	}
	body, err := json.Marshal(struct {
		FromUID string `json:"fromUid"`
		ToUID   string `json:"toUid"`
		GroupID int64  `json:"groupId"`
		Amount  string `json:"amount"`
	}{FromUID: request.FromUID, ToUID: request.ToUID, GroupID: request.GroupID, Amount: request.Amount})
	if err != nil {
		return PointTransferResult{}, validationError("encode HTX point transfer: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v2/point/transfer", nil, body,
		rateGroupPoint, min(client.accountQuota, pointRequestLimit),
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return PointTransferResult{}, err
	}
	var value PointTransferResult
	raw, err := client.decodeDataForOperation(
		response, commonexchange.OperationMutation, &value,
	)
	if err != nil {
		return PointTransferResult{}, err
	}
	if value.TransactID == "" {
		return PointTransferResult{}, client.decodeBodyErrorForOperation(
			response, commonexchange.OperationMutation,
			errors.New("HTX point transfer acknowledgement is missing a transaction ID"),
		)
	}
	value.Raw = raw
	return value, nil
}

// UID는 현재 API Key 소유 사용자의 UID를 조회한다.
func (client *Client) UID(
	ctx context.Context,
	options ...trade.RequestOption,
) (UserUID, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/user/uid", nil, nil,
		rateGroupAccount, client.accountQuota, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return UserUID{}, err
	}
	var uid Scalar
	raw, err := client.decodeDataForOperation(response, commonexchange.OperationRead, &uid)
	if err != nil {
		return UserUID{}, err
	}
	if !userIDPattern.MatchString(string(uid)) {
		return UserUID{}, client.decodeBodyError(
			response, errors.New("HTX user ID is invalid"),
		)
	}
	return UserUID{UID: uid, Raw: raw}, nil
}

// APIKeys는 부모 사용자 자신 또는 그 서브 사용자의 API Key 정보를 조회한다.
func (client *Client) APIKeys(
	ctx context.Context,
	request APIKeysRequest,
	options ...trade.RequestOption,
) ([]APIKeyInfo, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	query := url.Values{"uid": {request.UID}}
	setIfNotEmpty(query, "accessKey", request.AccessKey)
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/user/api-key", query, nil,
		rateGroupAccount, client.accountQuota, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeDataForOperation(
		response, commonexchange.OperationRead, &rawItems,
	); err != nil {
		return nil, err
	}
	items := make([]APIKeyInfo, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		if items[index].AccessKey == "" {
			return nil, client.decodeBodyError(
				response, errors.New("HTX API key item is missing an access key"),
			)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

func (request AccountValuationRequest) validate() error {
	if request.AccountType != "" && !accountTypePattern.MatchString(request.AccountType) {
		return validationError("invalid HTX valuation account type %q", request.AccountType)
	}
	if request.ValuationCurrency != "" &&
		!valuationCurrencyPattern.MatchString(request.ValuationCurrency) {
		return validationError("invalid HTX valuation currency %q", request.ValuationCurrency)
	}
	return nil
}

func (request AccountValuationRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "accountType", request.AccountType)
	setIfNotEmpty(values, "valuationCurrency", request.ValuationCurrency)
	return values
}

func (value AssetAccountType) valid() bool {
	switch value {
	case AssetAccountSpot, AssetAccountMargin, AssetAccountOTC, AssetAccountSuperMargin:
		return true
	default:
		return false
	}
}

func (request AssetValuationRequest) validate() error {
	if !request.AccountType.valid() {
		return validationError("unsupported HTX asset valuation account type %q", request.AccountType)
	}
	if request.ValuationCurrency != "" &&
		!valuationCurrencyPattern.MatchString(request.ValuationCurrency) {
		return validationError("invalid HTX valuation currency %q", request.ValuationCurrency)
	}
	if request.SubUID != "" && !userIDPattern.MatchString(request.SubUID) {
		return validationError("invalid HTX sub user ID %q", request.SubUID)
	}
	return nil
}

func (request AssetValuationRequest) values() url.Values {
	values := url.Values{"accountType": {string(request.AccountType)}}
	setIfNotEmpty(values, "valuationCurrency", request.ValuationCurrency)
	setIfNotEmpty(values, "subUid", request.SubUID)
	return values
}

func (value TransactType) valid() bool {
	switch value {
	case TransactTypeTrade, TransactTypeETF, TransactTypeTransactFee, TransactTypeFeeDeduction,
		TransactTypeTransfer, TransactTypeCredit, TransactTypeLiquidation, TransactTypeInterest,
		TransactTypeDeposit, TransactTypeWithdraw, TransactTypeWithdrawFee, TransactTypeExchange,
		TransactTypeOtherTypes, TransactTypeRebate:
		return true
	default:
		return false
	}
}

func (request AccountHistoryRequest) validate() error {
	if !accountIDPattern.MatchString(request.AccountID) {
		return validationError("invalid HTX account ID %q", request.AccountID)
	}
	if request.Currency != "" && !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid HTX currency %q", request.Currency)
	}
	seen := make(map[TransactType]struct{}, len(request.TransactTypes))
	for _, value := range request.TransactTypes {
		if !value.valid() {
			return validationError("unsupported HTX transact type %q", value)
		}
		if _, exists := seen[value]; exists {
			return validationError("HTX transact types contain a duplicate")
		}
		seen[value] = struct{}{}
	}
	if err := validateQueryWindow(
		request.Start, request.End, maximumAccountHistoryWindow, "account history",
	); err != nil {
		return err
	}
	if request.Sort != "" && !request.Sort.valid() {
		return validationError("sort order must be asc or desc")
	}
	if request.Size < 0 || request.Size > maximumAccountHistorySize {
		return validationError(
			"account history size must be between 1 and %d or zero", maximumAccountHistorySize,
		)
	}
	if request.FromID != "" && !orderIDPattern.MatchString(request.FromID) {
		return validationError("invalid HTX cursor %q", request.FromID)
	}
	return nil
}

func (request AccountHistoryRequest) values() url.Values {
	values := url.Values{"account-id": {request.AccountID}}
	setIfNotEmpty(values, "currency", request.Currency)
	if len(request.TransactTypes) > 0 {
		items := make([]string, len(request.TransactTypes))
		for index, value := range request.TransactTypes {
			items[index] = string(value)
		}
		values.Set("transact-types", strings.Join(items, ","))
	}
	setTimeRange(values, request.Start, request.End)
	setIfNotEmpty(values, "sort", string(request.Sort))
	setPositiveInt(values, "size", request.Size)
	setIfNotEmpty(values, "from-id", request.FromID)
	return values
}

func (request AccountLedgerRequest) validate() error {
	if !accountIDPattern.MatchString(request.AccountID) {
		return validationError("invalid HTX account ID %q", request.AccountID)
	}
	if request.Currency != "" && !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid HTX currency %q", request.Currency)
	}
	if err := validateQueryWindow(
		request.Start, request.End, maximumAccountLedgerWindow, "account ledger",
	); err != nil {
		return err
	}
	if request.Sort != "" && !request.Sort.valid() {
		return validationError("sort order must be asc or desc")
	}
	if request.Limit < 0 || request.Limit > maximumAccountLedgerLimit {
		return validationError(
			"account ledger limit must be between 1 and %d or zero", maximumAccountLedgerLimit,
		)
	}
	if request.FromID != "" && !orderIDPattern.MatchString(request.FromID) {
		return validationError("invalid HTX cursor %q", request.FromID)
	}
	return nil
}

func (request AccountLedgerRequest) values() url.Values {
	values := url.Values{"accountId": {request.AccountID}}
	setIfNotEmpty(values, "currency", request.Currency)
	if request.Start != nil {
		values.Set("startTime", strconv.FormatInt(request.Start.UnixMilli(), 10))
	}
	if request.End != nil {
		values.Set("endTime", strconv.FormatInt(request.End.UnixMilli(), 10))
	}
	setIfNotEmpty(values, "sort", string(request.Sort))
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "fromId", request.FromID)
	return values
}

func (value TransferAccountType) valid() bool {
	return value == TransferAccountSpot || value == TransferAccountMargin
}

// accountTransferWire는 계정 간 이체 JSON body이며 사용자·계정 ID는 숫자로 보낸다.
type accountTransferWire struct {
	FromUser        int64               `json:"from-user"`
	FromAccountType TransferAccountType `json:"from-account-type"`
	FromAccount     int64               `json:"from-account"`
	ToUser          int64               `json:"to-user"`
	ToAccountType   TransferAccountType `json:"to-account-type"`
	ToAccount       int64               `json:"to-account"`
	Currency        string              `json:"currency"`
	Amount          string              `json:"amount"`
}

// wire는 이체 조건을 검증하고 HTX JSON body 구조로 변환한다.
func (request AccountTransferRequest) wire() (accountTransferWire, error) {
	var wire accountTransferWire
	var err error
	if wire.FromUser, err = parseUserID("from user ID", request.FromUserID); err != nil {
		return accountTransferWire{}, err
	}
	if wire.ToUser, err = parseUserID("to user ID", request.ToUserID); err != nil {
		return accountTransferWire{}, err
	}
	if wire.FromAccount, err = parseAccountID("from account ID", request.FromAccountID); err != nil {
		return accountTransferWire{}, err
	}
	if wire.ToAccount, err = parseAccountID("to account ID", request.ToAccountID); err != nil {
		return accountTransferWire{}, err
	}
	if !request.FromAccountType.valid() || !request.ToAccountType.valid() {
		return accountTransferWire{}, validationError("transfer account type must be spot or margin")
	}
	if wire.FromUser == wire.ToUser && wire.FromAccount == wire.ToAccount {
		return accountTransferWire{}, validationError("transfer source and destination are the same account")
	}
	if !currencyPattern.MatchString(request.Currency) {
		return accountTransferWire{}, validationError("invalid HTX currency %q", request.Currency)
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return accountTransferWire{}, err
	}
	wire.FromAccountType = request.FromAccountType
	wire.ToAccountType = request.ToAccountType
	wire.Currency = request.Currency
	wire.Amount = request.Amount
	return wire, nil
}

func (value FuturesTransferType) valid() bool {
	return value == FuturesTransferToFutures || value == FuturesTransferToSpot
}

func (request FuturesTransferRequest) validate() error {
	if !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid HTX currency %q", request.Currency)
	}
	if !request.Type.valid() {
		return validationError("unsupported HTX futures transfer type %q", request.Type)
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return err
	}
	// 금액은 JSON 숫자로 보내므로 선행 0처럼 숫자 리터럴로 유효하지 않은 표기를 거부한다.
	if !json.Valid([]byte(request.Amount)) {
		return validationError("amount must be a canonical decimal number")
	}
	return nil
}

func (request PointAccountRequest) validate() error {
	if request.SubUID != "" && !userIDPattern.MatchString(request.SubUID) {
		return validationError("invalid HTX sub user ID %q", request.SubUID)
	}
	return nil
}

func (request PointTransferRequest) validate() error {
	if !userIDPattern.MatchString(request.FromUID) || !userIDPattern.MatchString(request.ToUID) {
		return validationError("invalid HTX point transfer user ID")
	}
	if request.FromUID == request.ToUID {
		return validationError("point transfer source and destination are the same user")
	}
	if request.GroupID < 0 {
		return validationError("point group ID cannot be negative")
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return err
	}
	if _, fraction, found := strings.Cut(request.Amount, "."); found &&
		len(fraction) > maximumPointDecimalPlaces {
		return validationError(
			"point amount supports at most %d decimal places", maximumPointDecimalPlaces,
		)
	}
	return nil
}

func (request APIKeysRequest) validate() error {
	if !userIDPattern.MatchString(request.UID) {
		return validationError("invalid HTX user ID %q", request.UID)
	}
	if request.AccessKey != "" && !accessKeyPattern.MatchString(request.AccessKey) {
		return validationError("invalid HTX access key")
	}
	return nil
}

// validateQueryWindow는 시작·종료 시각의 순서와 최대 조회 창 길이를 검증한다.
func validateQueryWindow(start, end *time.Time, maximum time.Duration, name string) error {
	if start != nil && start.UnixMilli() <= 0 || end != nil && end.UnixMilli() <= 0 {
		return validationError("query timestamps must be after the Unix epoch")
	}
	if start != nil && end != nil {
		if !start.Before(*end) {
			return validationError("query start must be before end")
		}
		if end.Sub(*start) > maximum {
			return validationError("%s query window cannot exceed %s", name, maximum)
		}
	}
	return nil
}

func parseUserID(name, value string) (int64, error) {
	if !userIDPattern.MatchString(value) {
		return 0, validationError("invalid HTX %s %q", name, value)
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, validationError("invalid HTX %s %q", name, value)
	}
	return number, nil
}

func parseAccountID(name, value string) (int64, error) {
	if !accountIDPattern.MatchString(value) {
		return 0, validationError("invalid HTX %s %q", name, value)
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, validationError("invalid HTX %s %q", name, value)
	}
	return number, nil
}
