package gateio

import (
	"context"
	"encoding/json"
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
	withdrawIDPattern       = regexp.MustCompile(`^w?[0-9]{1,24}$`)
	withdrawOrderIDPattern  = regexp.MustCompile(`^[0-9A-Za-z_-]{1,32}$`)
	chainNamePattern        = regexp.MustCompile(`^[0-9A-Za-z_-]{1,40}$`)
	clientTransferIDPattern = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)
)

// DepositAddressChain은 체인별 입금 주소와 Tag/Memo 정보다.
type DepositAddressChain struct {
	Chain        string `json:"chain"`
	Address      string `json:"address"`
	PaymentID    string `json:"payment_id"`
	PaymentName  string `json:"payment_name"`
	ObtainFailed int    `json:"obtain_failed"`
	MinConfirms  int    `json:"min_confirms"`
}

// DepositAddress는 통화별 입금 주소다.
type DepositAddress struct {
	Currency            string                `json:"currency"`
	Address             string                `json:"address"`
	MinDepositAmount    string                `json:"min_deposit_amount"`
	MultichainAddresses []DepositAddressChain `json:"multichain_addresses"`
	Raw                 json.RawMessage       `json:"-"`
}

func (address *DepositAddress) setRaw(raw json.RawMessage) { address.Raw = raw }

// DepositAddress는 통화의 입금 주소를 조회한다. 권한은 Read다.
// GET /wallet/deposit_address
func (client *Client) DepositAddress(
	ctx context.Context,
	currency string,
	options ...trade.RequestOption,
) (DepositAddress, error) {
	if !currencyPattern.MatchString(currency) {
		return DepositAddress{}, validationError("invalid currency %q", currency)
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/wallet/deposit_address",
		url.Values{"currency": {currency}}, nil,
		privateLimit("deposit-address"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return DepositAddress{}, err
	}
	return decodeObject[DepositAddress](client, response, commonexchange.OperationRead)
}

// TransferHistoryRequest는 입금·출금 이력 조회 조건이다. 시간 범위는 30일을 넘을 수 없고
// From을 생략하면 최근 7일이다.
type TransferHistoryRequest struct {
	Currency string
	From     *time.Time
	To       *time.Time
	Limit    int
	Offset   int
}

func (request TransferHistoryRequest) validate(maxLimit int) error {
	if err := validateOptionalCurrency(request.Currency); err != nil {
		return err
	}
	if err := validateTimeRange(request.From, request.To); err != nil {
		return err
	}
	if request.From != nil && request.To != nil && request.To.Sub(*request.From) > 30*24*time.Hour {
		return validationError("history range cannot exceed 30 days")
	}
	return validateOffsetLimit(request.Offset, request.Limit, maxLimit)
}

func (request TransferHistoryRequest) values() url.Values {
	values := make(url.Values)
	if request.Currency != "" {
		values.Set("currency", request.Currency)
	}
	setUnixTime(values, "from", request.From)
	setUnixTime(values, "to", request.To)
	setPositiveInt(values, "limit", request.Limit)
	if request.Offset > 0 {
		values.Set("offset", strconv.Itoa(request.Offset))
	}
	return values
}

// DepositRecord는 입금 이력 한 건이다.
type DepositRecord struct {
	ID           string          `json:"id"`
	TxID         string          `json:"txid"`
	Timestamp    string          `json:"timestamp"`
	Amount       string          `json:"amount"`
	Currency     string          `json:"currency"`
	Address      string          `json:"address"`
	Memo         string          `json:"memo"`
	Status       string          `json:"status"`
	RefundStatus string          `json:"refund_status"`
	Chain        string          `json:"chain"`
	Raw          json.RawMessage `json:"-"`
}

func (record *DepositRecord) setRaw(raw json.RawMessage) { record.Raw = raw }

// Deposits는 입금 이력을 조회한다. Limit은 최대 500이다. 권한은 Read다.
// GET /wallet/deposits
func (client *Client) Deposits(
	ctx context.Context,
	request TransferHistoryRequest,
	options ...trade.RequestOption,
) ([]DepositRecord, error) {
	if err := request.validate(500); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/wallet/deposits", request.values(), nil,
		privateLimit("deposits"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[DepositRecord](client, response, commonexchange.OperationRead)
}

// WithdrawalRecord는 출금 이력 한 건이다.
type WithdrawalRecord struct {
	ID              string          `json:"id"`
	TxID            string          `json:"txid"`
	BlockNumber     string          `json:"block_number"`
	WithdrawOrderID string          `json:"withdraw_order_id"`
	Timestamp       string          `json:"timestamp"`
	Amount          string          `json:"amount"`
	Fee             string          `json:"fee"`
	Currency        string          `json:"currency"`
	Address         string          `json:"address"`
	Type            string          `json:"type"`
	FailReason      string          `json:"fail_reason"`
	FinalTimestamp  string          `json:"timestamp2"`
	Memo            string          `json:"memo"`
	Status          string          `json:"status"`
	Chain           string          `json:"chain"`
	Raw             json.RawMessage `json:"-"`
}

func (record *WithdrawalRecord) setRaw(raw json.RawMessage) { record.Raw = raw }

// WithdrawalsRequest는 출금 이력 조회 조건이다. WithdrawID나 WithdrawOrderID를 지정하면
// 해당 기록만 조회한다.
type WithdrawalsRequest struct {
	TransferHistoryRequest
	WithdrawID      string
	WithdrawOrderID string
}

func (request WithdrawalsRequest) validate() error {
	if request.WithdrawID != "" && !withdrawIDPattern.MatchString(request.WithdrawID) {
		return validationError("invalid withdraw ID")
	}
	if request.WithdrawOrderID != "" && !withdrawOrderIDPattern.MatchString(request.WithdrawOrderID) {
		return validationError("invalid withdraw order ID")
	}
	return request.TransferHistoryRequest.validate(1000)
}

// Withdrawals는 출금 이력을 조회한다. 권한은 Read다.
// GET /wallet/withdrawals
func (client *Client) Withdrawals(
	ctx context.Context,
	request WithdrawalsRequest,
	options ...trade.RequestOption,
) ([]WithdrawalRecord, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	query := request.values()
	if request.WithdrawID != "" {
		query.Set("withdraw_id", request.WithdrawID)
	}
	if request.WithdrawOrderID != "" {
		query.Set("withdraw_order_id", request.WithdrawOrderID)
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/wallet/withdrawals", query, nil,
		privateLimit("withdrawals"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[WithdrawalRecord](client, response, commonexchange.OperationRead)
}

// WithdrawRequest는 온체인 출금 요청이다. 같은 WithdrawOrderID는 중복 출금 방지 키로 쓰이므로
// 호출자가 항상 지정해야 한다.
type WithdrawRequest struct {
	WithdrawOrderID string `json:"withdraw_order_id"`
	Currency        string `json:"currency"`
	Chain           string `json:"chain"`
	Amount          string `json:"amount"`
	Address         string `json:"address"`
	Memo            string `json:"memo,omitempty"`
}

func (request WithdrawRequest) validate() error {
	if !withdrawOrderIDPattern.MatchString(request.WithdrawOrderID) {
		return validationError("withdraw order ID must be 1 to 32 characters of letters, digits, _ or -")
	}
	if !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid currency %q", request.Currency)
	}
	if !chainNamePattern.MatchString(request.Chain) {
		return validationError("invalid chain %q", request.Chain)
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return err
	}
	if strings.TrimSpace(request.Address) == "" {
		return validationError("withdraw address is required")
	}
	return nil
}

// WithdrawalAck는 출금 요청·취소 응답이다. 요청 응답(LedgerRecord)과 취소 응답(WithdrawalsDel)의
// 필드를 합쳐 해석하므로 응답에 없는 필드는 빈 값이다.
type WithdrawalAck struct {
	ID              string          `json:"id"`
	TxID            string          `json:"txid"`
	WithdrawOrderID string          `json:"withdraw_order_id"`
	WithdrawID      string          `json:"withdraw_id"`
	AssetClass      string          `json:"asset_class"`
	Timestamp       string          `json:"timestamp"`
	Amount          string          `json:"amount"`
	Currency        string          `json:"currency"`
	Address         string          `json:"address"`
	Memo            string          `json:"memo"`
	BlockNumber     string          `json:"block_number"`
	Status          string          `json:"status"`
	Chain           string          `json:"chain"`
	Raw             json.RawMessage `json:"-"`
}

func (ack *WithdrawalAck) setRaw(raw json.RawMessage) { ack.Raw = raw }

// Withdraw는 온체인 출금을 요청한다. 권한은 Withdraw이며 자동 재시도하지 않는다.
// 전송 결과가 불명확하면 같은 WithdrawOrderID로 Withdrawals를 조회해 확인해야 한다.
// POST /withdrawals
func (client *Client) Withdraw(
	ctx context.Context,
	request WithdrawRequest,
	options ...trade.RequestOption,
) (WithdrawalAck, error) {
	if err := request.validate(); err != nil {
		return WithdrawalAck{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return WithdrawalAck{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/withdrawals", nil, body,
		privateLimit("withdraw"), credential.PermissionWithdraw,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return WithdrawalAck{}, err
	}
	return decodeObject[WithdrawalAck](client, response, commonexchange.OperationMutation)
}

// CancelWithdrawal은 처리 전 출금을 취소한다. 권한은 Withdraw이며 자동 재시도하지 않는다.
// DELETE /withdrawals/{withdrawal_id}
func (client *Client) CancelWithdrawal(
	ctx context.Context,
	withdrawalID string,
	options ...trade.RequestOption,
) (WithdrawalAck, error) {
	if !withdrawIDPattern.MatchString(withdrawalID) {
		return WithdrawalAck{}, validationError("invalid withdrawal ID")
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, "/withdrawals/"+url.PathEscape(withdrawalID), nil, nil,
		privateLimit("cancel-withdrawal"), credential.PermissionWithdraw,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return WithdrawalAck{}, err
	}
	return decodeObject[WithdrawalAck](client, response, commonexchange.OperationMutation)
}

// WithdrawStatus는 통화별 출금 수수료와 한도다.
type WithdrawStatus struct {
	Currency                string            `json:"currency"`
	Name                    string            `json:"name"`
	NameCN                  string            `json:"name_cn"`
	Deposit                 string            `json:"deposit"`
	WithdrawPercent         string            `json:"withdraw_percent"`
	WithdrawFix             string            `json:"withdraw_fix"`
	WithdrawDayLimit        string            `json:"withdraw_day_limit"`
	WithdrawAmountMini      string            `json:"withdraw_amount_mini"`
	WithdrawDayLimitRemain  string            `json:"withdraw_day_limit_remain"`
	WithdrawEachtimeLimit   string            `json:"withdraw_eachtime_limit"`
	WithdrawFixOnChains     map[string]string `json:"withdraw_fix_on_chains"`
	WithdrawPercentOnChains map[string]string `json:"withdraw_percent_on_chains"`
	Raw                     json.RawMessage   `json:"-"`
}

func (status *WithdrawStatus) setRaw(raw json.RawMessage) { status.Raw = raw }

// WithdrawStatuses는 통화별 출금 수수료·한도를 조회한다. Currency를 비우면 전체다. 권한은 Read다.
// GET /wallet/withdraw_status
func (client *Client) WithdrawStatuses(
	ctx context.Context,
	currency string,
	options ...trade.RequestOption,
) ([]WithdrawStatus, error) {
	if err := validateOptionalCurrency(currency); err != nil {
		return nil, err
	}
	query := make(url.Values)
	if currency != "" {
		query.Set("currency", currency)
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/wallet/withdraw_status", query, nil,
		privateLimit("withdraw-status"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[WithdrawStatus](client, response, commonexchange.OperationRead)
}

// SavedAddress는 저장된 출금 주소다.
type SavedAddress struct {
	Currency string          `json:"currency"`
	Chain    string          `json:"chain"`
	Address  string          `json:"address"`
	Name     string          `json:"name"`
	Tag      string          `json:"tag"`
	Verified string          `json:"verified"`
	Raw      json.RawMessage `json:"-"`
}

func (address *SavedAddress) setRaw(raw json.RawMessage) { address.Raw = raw }

// SavedAddressesRequest는 저장된 출금 주소 조회 조건이다. Currency는 필수이고 Limit은 최대 100이다.
type SavedAddressesRequest struct {
	Currency string
	Chain    string
	Page     int
	Limit    int
}

func (request SavedAddressesRequest) validate() error {
	if !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid currency %q", request.Currency)
	}
	if request.Chain != "" && !chainNamePattern.MatchString(request.Chain) {
		return validationError("invalid chain %q", request.Chain)
	}
	if request.Page < 0 {
		return validationError("page cannot be negative")
	}
	if request.Limit < 0 || request.Limit > 100 {
		return validationError("limit must be between 1 and 100 or zero")
	}
	return nil
}

// SavedAddresses는 저장된 출금 주소를 조회한다. 권한은 Read다.
// GET /wallet/saved_address
func (client *Client) SavedAddresses(
	ctx context.Context,
	request SavedAddressesRequest,
	options ...trade.RequestOption,
) ([]SavedAddress, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	query := url.Values{"currency": {request.Currency}}
	if request.Chain != "" {
		query.Set("chain", request.Chain)
	}
	setPositiveInt(query, "page", request.Page)
	setPositiveInt(query, "limit", request.Limit)
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/wallet/saved_address", query, nil,
		privateLimit("saved-address"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[SavedAddress](client, response, commonexchange.OperationRead)
}

// PushOrderType은 UID 내부이체 이력의 방향이다.
type PushOrderType string

const (
	PushOrderWithdraw PushOrderType = "withdraw"
	PushOrderDeposit  PushOrderType = "deposit"
)

// UIDPushRequest는 Gate UID 사이 내부이체(수수료 없음) 요청이다.
type UIDPushRequest struct {
	ReceiveUID int64  `json:"receive_uid"`
	Currency   string `json:"currency"`
	Amount     string `json:"amount"`
}

func (request UIDPushRequest) validate() error {
	if request.ReceiveUID <= 0 {
		return validationError("receive UID must be positive")
	}
	if !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid currency %q", request.Currency)
	}
	return validatePositiveDecimal("amount", request.Amount)
}

// UIDPushAck는 UID 내부이체 요청 응답이다.
type UIDPushAck struct {
	ID  string          `json:"id"`
	Raw json.RawMessage `json:"-"`
}

func (ack *UIDPushAck) setRaw(raw json.RawMessage) { ack.Raw = raw }

// PushToUID는 다른 Gate UID로 내부이체한다. 권한은 Withdraw이며 자동 재시도하지 않는다.
// POST /withdrawals/push
func (client *Client) PushToUID(
	ctx context.Context,
	request UIDPushRequest,
	options ...trade.RequestOption,
) (UIDPushAck, error) {
	if err := request.validate(); err != nil {
		return UIDPushAck{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return UIDPushAck{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/withdrawals/push", nil, body,
		privateLimit("withdraw-push"), credential.PermissionWithdraw,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return UIDPushAck{}, err
	}
	return decodeObject[UIDPushAck](client, response, commonexchange.OperationMutation)
}

// UIDPushOrder는 UID 내부이체 이력 한 건이다.
type UIDPushOrder struct {
	ID              int64           `json:"id"`
	PushUID         int64           `json:"push_uid"`
	ReceiveUID      int64           `json:"receive_uid"`
	Currency        string          `json:"currency"`
	Amount          string          `json:"amount"`
	CreateTime      int64           `json:"create_time"`
	Status          string          `json:"status"`
	Message         string          `json:"message"`
	TransactionType string          `json:"transaction_type"`
	Raw             json.RawMessage `json:"-"`
}

func (order *UIDPushOrder) setRaw(raw json.RawMessage) { order.Raw = raw }

// UIDPushOrdersRequest는 UID 내부이체 이력 조회 조건이다. Type을 비우면 withdraw다.
type UIDPushOrdersRequest struct {
	ID     int
	From   *time.Time
	To     *time.Time
	Limit  int
	Offset int
	Type   PushOrderType
}

func (request UIDPushOrdersRequest) validate() error {
	if request.ID < 0 {
		return validationError("push order ID cannot be negative")
	}
	if request.Type != "" && request.Type != PushOrderWithdraw && request.Type != PushOrderDeposit {
		return validationError("push order type must be withdraw or deposit")
	}
	if err := validateTimeRange(request.From, request.To); err != nil {
		return err
	}
	return validateOffsetLimit(request.Offset, request.Limit, 1000)
}

// UIDPushOrders는 UID 내부이체 이력을 조회한다. 권한은 Read다.
// GET /wallet/push
func (client *Client) UIDPushOrders(
	ctx context.Context,
	request UIDPushOrdersRequest,
	options ...trade.RequestOption,
) ([]UIDPushOrder, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	query := make(url.Values)
	setPositiveInt(query, "id", request.ID)
	setUnixTime(query, "from", request.From)
	setUnixTime(query, "to", request.To)
	setPositiveInt(query, "limit", request.Limit)
	if request.Offset > 0 {
		query.Set("offset", strconv.Itoa(request.Offset))
	}
	if request.Type != "" {
		query.Set("transaction_type", string(request.Type))
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/wallet/push", query, nil,
		privateLimit("push-orders"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[UIDPushOrder](client, response, commonexchange.OperationRead)
}
