package kraken

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	walletTransferSource      = "Spot Wallet"
	walletTransferDestination = "Futures Wallet"
)

// RebaseMultiplier는 xStocks 수량을 표시하는 기준이다.
type RebaseMultiplier string

const (
	// RebaseMultiplierRebased는 기초 주식 기준으로 표시한다.
	RebaseMultiplierRebased RebaseMultiplier = "rebased"
	// RebaseMultiplierBase는 SPV 토큰 기준으로 표시한다.
	RebaseMultiplierBase RebaseMultiplier = "base"
)

// DepositMethodsRequest는 자산의 입금 수단 조회 조건이다.
type DepositMethodsRequest struct {
	Asset            string
	AssetClass       AssetClass
	RebaseMultiplier RebaseMultiplier
}

// DepositMethod는 입금 수단 한 건이다. 수치는 문자열 또는 숫자로 내려와 원문 그대로 보존한다.
// Limit은 한도가 없으면 빈 문자열이다.
type DepositMethod struct {
	Method          string          `json:"method"`
	Limit           DecimalString   `json:"limit"`
	Fee             DecimalString   `json:"fee"`
	FeePercentage   DecimalString   `json:"fee-percentage"`
	AddressSetupFee DecimalString   `json:"address-setup-fee"`
	GenAddress      bool            `json:"gen-address"`
	Minimum         DecimalString   `json:"minimum"`
	Raw             json.RawMessage `json:"-"`
}

// DepositAddressesRequest는 입금 주소 조회 또는 생성 조건이다.
// New가 true이면 새 주소를 생성하므로 전송 권한이 필요한 변경 요청이며 자동 재시도하지 않는다.
type DepositAddressesRequest struct {
	Asset      string
	AssetClass AssetClass
	Method     string
	New        bool
	// Amount는 Bitcoin Lightning 입금에서만 필요하다.
	Amount string
}

// DepositAddress는 입금 주소 한 건이다. Tag는 XRP destination tag, Memo는 STX, XLM, EOS 등의 memo다.
type DepositAddress struct {
	Address    string          `json:"address"`
	ExpireTime string          `json:"expiretm"`
	New        bool            `json:"new"`
	Tag        string          `json:"tag"`
	Memo       string          `json:"memo"`
	Raw        json.RawMessage `json:"-"`
}

// FundingStatusRequest는 최근 입금 또는 출금 상태 조회 조건이다. 최신순으로 반환된다.
type FundingStatusRequest struct {
	Asset      string
	AssetClass AssetClass
	Method     string
	// Start 이전과 End 이후에 생성된 항목은 제외한다.
	Start *time.Time
	End   *time.Time
	// Paginated가 true이면 cursor 방식 페이지네이션을 시작한다. Cursor가 있으면 해당 페이지를 조회한다.
	Paginated bool
	Cursor    string
	// Limit은 페이지당 항목 수다. 생략하면 Kraken 기본값(입금 25, 출금 500)이다.
	Limit            int
	RebaseMultiplier RebaseMultiplier
}

// DepositRecord는 입금 내역 한 건이다. Time은 Unix 초다.
// Status는 Initial, Pending, EarlyConfirmed, Settled, Success, Failure 중 하나이며
// StatusProperty는 return(Kraken 반환 거래), onhold(검토 보류) 같은 추가 상태다.
type DepositRecord struct {
	Method         string          `json:"method"`
	AssetClass     string          `json:"aclass"`
	Asset          string          `json:"asset"`
	ReferenceID    string          `json:"refid"`
	TransactionID  string          `json:"txid"`
	Info           string          `json:"info"`
	Amount         DecimalString   `json:"amount"`
	Fee            DecimalString   `json:"fee"`
	Time           int64           `json:"time"`
	Status         string          `json:"status"`
	StatusProperty string          `json:"status-prop"`
	Originators    []string        `json:"originators"`
	Raw            json.RawMessage `json:"-"`
}

// DepositStatusPage는 입금 내역과 다음 페이지 cursor다. 페이지네이션을 쓰지 않으면 NextCursor가 비어 있다.
type DepositStatusPage struct {
	Deposits   []DepositRecord
	NextCursor string
	Raw        json.RawMessage
}

// WithdrawMethodsRequest는 사용자가 이용할 수 있는 출금 수단 조회 조건이다.
type WithdrawMethodsRequest struct {
	Asset            string
	AssetClass       AssetClass
	Network          string
	RebaseMultiplier RebaseMultiplier
}

// WithdrawFee는 출금 수단의 수수료다. FeePercentage가 있으면 정액 수수료 대신 쓰인다.
type WithdrawFee struct {
	AssetClass    string        `json:"aclass"`
	Asset         string        `json:"asset"`
	Fee           DecimalString `json:"fee"`
	FeePercentage DecimalString `json:"fee_percentage"`
}

// WithdrawLimitWindow는 시간 창 하나의 출금 한도 사용량이다.
type WithdrawLimitWindow struct {
	Maximum   DecimalString `json:"maximum"`
	Remaining DecimalString `json:"remaining"`
	Used      DecimalString `json:"used"`
}

// WithdrawLimit는 출금 수단의 한도 규칙이다. Windows의 key는 초 단위 시간 창이다.
type WithdrawLimit struct {
	Description string                         `json:"description"`
	LimitType   string                         `json:"limit_type"`
	Windows     map[string]WithdrawLimitWindow `json:"limits"`
}

// WithdrawMethod는 출금 수단 한 건이다.
type WithdrawMethod struct {
	Asset     string          `json:"asset"`
	Method    string          `json:"method"`
	MethodID  string          `json:"method_id"`
	Network   string          `json:"network"`
	NetworkID string          `json:"network_id"`
	Minimum   DecimalString   `json:"minimum"`
	Fee       WithdrawFee     `json:"fee"`
	Limits    []WithdrawLimit `json:"limits"`
	Raw       json.RawMessage `json:"-"`
}

// WithdrawAddressesRequest는 등록된 출금 주소 조회 조건이다.
type WithdrawAddressesRequest struct {
	Asset      string
	AssetClass AssetClass
	Method     string
	// Key는 계정에 등록한 출금 키 이름이다.
	Key string
	// Verified를 지정하면 이메일 인증 여부로 거른다.
	Verified *bool
}

// WithdrawAddress는 등록된 출금 주소 한 건이다.
type WithdrawAddress struct {
	Address  string          `json:"address"`
	Asset    string          `json:"asset"`
	Method   string          `json:"method"`
	Key      string          `json:"key"`
	Tag      string          `json:"tag"`
	Verified bool            `json:"verified"`
	Raw      json.RawMessage `json:"-"`
}

// WithdrawInfoRequest는 출금 수수료와 한도 조회 조건이다. Key는 계정에 등록한 출금 키 이름이다.
type WithdrawInfoRequest struct {
	Asset  string
	Key    string
	Amount string
}

// WithdrawInfo는 출금 수수료와 한도 정보다. Amount는 수수료를 뺀 실제 송금액이다.
type WithdrawInfo struct {
	Method string          `json:"method"`
	Limit  DecimalString   `json:"limit"`
	Amount DecimalString   `json:"amount"`
	Fee    DecimalString   `json:"fee"`
	Raw    json.RawMessage `json:"-"`
}

// WithdrawRequest는 출금 요청 조건이다. 출금 대상은 계정에 등록한 Key로 지정한다.
type WithdrawRequest struct {
	Asset      string
	AssetClass AssetClass
	Key        string
	// Address를 지정하면 Key의 주소와 일치하는지 확인하며 다르면 Kraken이 거부한다.
	Address string
	Amount  string
	// MaxFee를 지정하면 처리 수수료가 이보다 클 때 출금이 실패한다.
	MaxFee           string
	RebaseMultiplier RebaseMultiplier
}

// WithdrawReference는 출금 요청 접수 결과다. ReferenceID로 상태 조회와 취소를 한다.
type WithdrawReference struct {
	ReferenceID string          `json:"refid"`
	Raw         json.RawMessage `json:"-"`
}

// WithdrawRecord는 출금 내역 한 건이다. Time은 Unix 초다.
// Status는 Initial, Pending, Settled, Success, Failure 중 하나이며 StatusProperty는
// cancel-pending, canceled, cancel-denied, return, onhold 같은 추가 상태다.
type WithdrawRecord struct {
	Method         string          `json:"method"`
	Network        string          `json:"network"`
	AssetClass     string          `json:"aclass"`
	Asset          string          `json:"asset"`
	ReferenceID    string          `json:"refid"`
	TransactionID  string          `json:"txid"`
	Info           string          `json:"info"`
	Amount         DecimalString   `json:"amount"`
	Fee            DecimalString   `json:"fee"`
	Time           int64           `json:"time"`
	Status         string          `json:"status"`
	StatusProperty string          `json:"status-prop"`
	Key            string          `json:"key"`
	Raw            json.RawMessage `json:"-"`
}

// WithdrawStatusPage는 출금 내역과 다음 페이지 cursor다. 페이지네이션을 쓰지 않으면 NextCursor가 비어 있다.
type WithdrawStatusPage struct {
	Withdrawals []WithdrawRecord
	NextCursor  string
	Raw         json.RawMessage
}

// WithdrawCancelRequest는 아직 처리되지 않은 출금의 취소 요청이다.
type WithdrawCancelRequest struct {
	Asset       string
	ReferenceID string
}

// WithdrawCancelResult는 출금 취소 결과다. Canceled가 false이면 이미 처리되어 취소하지 못한 것이다.
type WithdrawCancelResult struct {
	Canceled bool
	Raw      json.RawMessage
}

// WalletTransferRequest는 Kraken Spot 지갑에서 Futures 지갑으로 보내는 이체 조건이다.
// 반대 방향 이체는 Kraken Futures API로만 요청할 수 있다.
type WalletTransferRequest struct {
	Asset  string
	Amount string
}

// WalletTransferResult는 지갑 이체 접수 결과다.
type WalletTransferResult struct {
	ReferenceID string          `json:"refid"`
	Raw         json.RawMessage `json:"-"`
}

// CreateSubaccountRequest는 거래 서브계정 생성 조건이다. 기관 마스터 계정 API Key로만 호출할 수 있다.
type CreateSubaccountRequest struct {
	Username string
	Email    string
}

// CreateSubaccountResult는 서브계정 생성 결과다.
type CreateSubaccountResult struct {
	Created bool
	Raw     json.RawMessage
}

// AccountTransferRequest는 마스터 계정과 서브계정 사이 이체 조건이다. 마스터 계정 API Key로만 호출할 수 있다.
// From과 To는 "ABCD 1234 EFGH 5678" 형식의 공개 계정 ID다.
type AccountTransferRequest struct {
	Asset      string
	AssetClass AssetClass
	Amount     string
	From       string
	To         string
}

// AccountTransferResult는 계정 간 이체 결과다. Status는 pending 또는 complete다.
type AccountTransferResult struct {
	TransferID string          `json:"transfer_id"`
	Status     string          `json:"status"`
	Raw        json.RawMessage `json:"-"`
}

func (multiplier RebaseMultiplier) validate() error {
	if multiplier != "" && multiplier != RebaseMultiplierRebased && multiplier != RebaseMultiplierBase {
		return validationError("unsupported rebase multiplier %q", multiplier)
	}
	return nil
}

func (request DepositMethodsRequest) validate() error {
	if err := validateAssetName("deposit asset", request.Asset); err != nil {
		return err
	}
	if err := validateAssetClass(request.AssetClass); err != nil {
		return err
	}
	return request.RebaseMultiplier.validate()
}

func (request DepositMethodsRequest) values() url.Values {
	values := url.Values{"asset": {request.Asset}}
	setAssetClass(values, "aclass", request.AssetClass)
	setIfNotEmpty(values, "rebase_multiplier", string(request.RebaseMultiplier))
	return values
}

func (request DepositAddressesRequest) validate() error {
	if err := validateAssetName("deposit asset", request.Asset); err != nil {
		return err
	}
	if err := validateAssetClass(request.AssetClass); err != nil {
		return err
	}
	if err := validateRequiredText("deposit method", request.Method); err != nil {
		return err
	}
	return validateOptionalDecimal("deposit amount", request.Amount)
}

func (request DepositAddressesRequest) values() url.Values {
	values := url.Values{"asset": {request.Asset}, "method": {request.Method}}
	setAssetClass(values, "aclass", request.AssetClass)
	setBool(values, "new", request.New)
	setIfNotEmpty(values, "amount", request.Amount)
	return values
}

func (request FundingStatusRequest) validate() error {
	if request.Asset != "" {
		if err := validateAssetName("funding asset", request.Asset); err != nil {
			return err
		}
	}
	if err := validateAssetClass(request.AssetClass); err != nil {
		return err
	}
	if request.Method != "" {
		if err := validateRequiredText("funding method", request.Method); err != nil {
			return err
		}
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	if request.Cursor != "" {
		if err := validateOpaqueToken("funding cursor", request.Cursor); err != nil {
			return err
		}
	}
	if request.Limit < 0 {
		return validationError("funding limit cannot be negative")
	}
	return request.RebaseMultiplier.validate()
}

func (request FundingStatusRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "asset", request.Asset)
	setAssetClass(values, "aclass", request.AssetClass)
	setIfNotEmpty(values, "method", request.Method)
	setTime(values, "start", request.Start)
	setTime(values, "end", request.End)
	if request.Cursor != "" {
		values.Set("cursor", request.Cursor)
	} else if request.Paginated {
		values.Set("cursor", "true")
	}
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "rebase_multiplier", string(request.RebaseMultiplier))
	return values
}

func (request WithdrawMethodsRequest) validate() error {
	if request.Asset != "" {
		if err := validateAssetName("withdraw asset", request.Asset); err != nil {
			return err
		}
	}
	if err := validateAssetClass(request.AssetClass); err != nil {
		return err
	}
	if request.Network != "" {
		if err := validateRequiredText("withdraw network", request.Network); err != nil {
			return err
		}
	}
	return request.RebaseMultiplier.validate()
}

func (request WithdrawMethodsRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "asset", request.Asset)
	setAssetClass(values, "aclass", request.AssetClass)
	setIfNotEmpty(values, "network", request.Network)
	setIfNotEmpty(values, "rebase_multiplier", string(request.RebaseMultiplier))
	return values
}

func (request WithdrawAddressesRequest) validate() error {
	if request.Asset != "" {
		if err := validateAssetName("withdraw asset", request.Asset); err != nil {
			return err
		}
	}
	if err := validateAssetClass(request.AssetClass); err != nil {
		return err
	}
	if request.Method != "" {
		if err := validateRequiredText("withdraw method", request.Method); err != nil {
			return err
		}
	}
	if request.Key != "" {
		return validateRequiredText("withdraw key", request.Key)
	}
	return nil
}

func (request WithdrawAddressesRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "asset", request.Asset)
	setAssetClass(values, "aclass", request.AssetClass)
	setIfNotEmpty(values, "method", request.Method)
	setIfNotEmpty(values, "key", request.Key)
	if request.Verified != nil {
		if *request.Verified {
			values.Set("verified", "true")
		} else {
			values.Set("verified", "false")
		}
	}
	return values
}

func (request WithdrawInfoRequest) validate() error {
	if err := validateAssetName("withdraw asset", request.Asset); err != nil {
		return err
	}
	if err := validateRequiredText("withdraw key", request.Key); err != nil {
		return err
	}
	return validateDecimalAmount("withdraw amount", request.Amount)
}

func (request WithdrawInfoRequest) values() url.Values {
	return url.Values{"asset": {request.Asset}, "key": {request.Key}, "amount": {request.Amount}}
}

func (request WithdrawRequest) validate() error {
	if err := validateAssetName("withdraw asset", request.Asset); err != nil {
		return err
	}
	if err := validateAssetClass(request.AssetClass); err != nil {
		return err
	}
	if err := validateRequiredText("withdraw key", request.Key); err != nil {
		return err
	}
	if request.Address != "" {
		if err := validateRequiredText("withdraw address", request.Address); err != nil {
			return err
		}
	}
	if err := validateDecimalAmount("withdraw amount", request.Amount); err != nil {
		return err
	}
	if err := validateOptionalDecimal("withdraw max fee", request.MaxFee); err != nil {
		return err
	}
	return request.RebaseMultiplier.validate()
}

func (request WithdrawRequest) values() url.Values {
	values := url.Values{
		"asset": {request.Asset}, "key": {request.Key}, "amount": {request.Amount},
	}
	setAssetClass(values, "aclass", request.AssetClass)
	setIfNotEmpty(values, "address", request.Address)
	setIfNotEmpty(values, "max_fee", request.MaxFee)
	setIfNotEmpty(values, "rebase_multiplier", string(request.RebaseMultiplier))
	return values
}

func (request WithdrawCancelRequest) validate() error {
	if err := validateAssetName("withdraw asset", request.Asset); err != nil {
		return err
	}
	return validateOpaqueToken("withdraw reference ID", request.ReferenceID)
}

func (request WithdrawCancelRequest) values() url.Values {
	return url.Values{"asset": {request.Asset}, "refid": {request.ReferenceID}}
}

func (request WalletTransferRequest) validate() error {
	if err := validateAssetName("transfer asset", request.Asset); err != nil {
		return err
	}
	return validateDecimalAmount("transfer amount", request.Amount)
}

func (request WalletTransferRequest) values() url.Values {
	return url.Values{
		"asset": {request.Asset}, "from": {walletTransferSource}, "to": {walletTransferDestination},
		"amount": {request.Amount},
	}
}

func (request CreateSubaccountRequest) validate() error {
	if err := validateRequiredText("subaccount username", request.Username); err != nil {
		return err
	}
	return validateRequiredText("subaccount email", request.Email)
}

func (request CreateSubaccountRequest) values() url.Values {
	return url.Values{"username": {request.Username}, "email": {request.Email}}
}

func (request AccountTransferRequest) validate() error {
	if err := validateAssetName("transfer asset", request.Asset); err != nil {
		return err
	}
	if err := validateAssetClass(request.AssetClass); err != nil {
		return err
	}
	if err := validateDecimalAmount("transfer amount", request.Amount); err != nil {
		return err
	}
	if err := validateRequiredText("source account ID", request.From); err != nil {
		return err
	}
	if err := validateRequiredText("destination account ID", request.To); err != nil {
		return err
	}
	if request.From == request.To {
		return validationError("source and destination accounts must differ")
	}
	return nil
}

func (request AccountTransferRequest) values() url.Values {
	values := url.Values{
		"asset": {request.Asset}, "amount": {request.Amount}, "from": {request.From}, "to": {request.To},
	}
	setAssetClass(values, "asset_class", request.AssetClass)
	return values
}

// DepositMethods는 자산을 입금할 수 있는 수단을 조회한다.
// Kraken은 이 endpoint를 deprecated로 표시하며 Funding (Beta) API 사용을 권장한다.
func (client *Client) DepositMethods(
	ctx context.Context,
	request DepositMethodsRequest,
	options ...trade.RequestOption,
) ([]DepositMethod, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"DepositMethods", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &items); err != nil {
		return nil, err
	}
	methods := make([]DepositMethod, len(items))
	for index, item := range items {
		if err := client.decodeResultInto(response, commonexchange.OperationRead, item, &methods[index]); err != nil {
			return nil, err
		}
		methods[index].Raw = cloneBytes(item)
	}
	return methods, nil
}

// DepositAddresses는 입금 주소를 조회하거나 New가 true이면 새로 생성한다.
// 주소를 생성하는 요청은 전송 권한이 필요한 변경 요청이며, 전송 결과가 불명확하면 자동 재시도하지 않고
// UNKNOWN_EXECUTION_STATE로 반환한다.
// Kraken은 이 endpoint를 deprecated로 표시하며 Funding (Beta) API 사용을 권장한다.
func (client *Client) DepositAddresses(
	ctx context.Context,
	request DepositAddressesRequest,
	options ...trade.RequestOption,
) ([]DepositAddress, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	permission, operation := credential.PermissionRead, commonexchange.OperationRead
	if request.New {
		permission, operation = credential.PermissionTransfer, commonexchange.OperationMutation
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"DepositAddresses", request.values(), permission, operation, limitPrivate, "",
		options...,
	)
	if err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if err := client.decodeResultInto(response, operation, result, &items); err != nil {
		return nil, err
	}
	addresses := make([]DepositAddress, len(items))
	for index, item := range items {
		if err := client.decodeResultInto(response, operation, item, &addresses[index]); err != nil {
			return nil, err
		}
		addresses[index].Raw = cloneBytes(item)
	}
	return addresses, nil
}

// DepositStatus는 최근 입금 상태를 최신순으로 조회한다.
// Kraken은 이 endpoint를 deprecated로 표시하며 Funding (Beta) API 사용을 권장한다.
func (client *Client) DepositStatus(
	ctx context.Context,
	request FundingStatusRequest,
	options ...trade.RequestOption,
) (DepositStatusPage, error) {
	if err := request.validate(); err != nil {
		return DepositStatusPage{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"DepositStatus", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return DepositStatusPage{}, err
	}
	items, nextCursor, err := decodeFundingPage(result)
	if err != nil {
		return DepositStatusPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	page := DepositStatusPage{
		Deposits: make([]DepositRecord, len(items)), NextCursor: nextCursor, Raw: cloneBytes(result),
	}
	for index, item := range items {
		if err := client.decodeResultInto(
			response, commonexchange.OperationRead, item, &page.Deposits[index],
		); err != nil {
			return DepositStatusPage{}, err
		}
		page.Deposits[index].Raw = cloneBytes(item)
	}
	return page, nil
}

// WithdrawMethods는 사용자가 이용할 수 있는 출금 수단을 조회한다.
// Kraken은 이 endpoint를 deprecated로 표시하며 Funding (Beta) API 사용을 권장한다.
func (client *Client) WithdrawMethods(
	ctx context.Context,
	request WithdrawMethodsRequest,
	options ...trade.RequestOption,
) ([]WithdrawMethod, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"WithdrawMethods", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &items); err != nil {
		return nil, err
	}
	methods := make([]WithdrawMethod, len(items))
	for index, item := range items {
		if err := client.decodeResultInto(response, commonexchange.OperationRead, item, &methods[index]); err != nil {
			return nil, err
		}
		methods[index].Raw = cloneBytes(item)
	}
	return methods, nil
}

// WithdrawAddresses는 계정에 등록된 출금 주소를 조회한다.
// Kraken은 이 endpoint를 deprecated로 표시하며 Funding (Beta) API 사용을 권장한다.
func (client *Client) WithdrawAddresses(
	ctx context.Context,
	request WithdrawAddressesRequest,
	options ...trade.RequestOption,
) ([]WithdrawAddress, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"WithdrawAddresses", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &items); err != nil {
		return nil, err
	}
	addresses := make([]WithdrawAddress, len(items))
	for index, item := range items {
		if err := client.decodeResultInto(response, commonexchange.OperationRead, item, &addresses[index]); err != nil {
			return nil, err
		}
		addresses[index].Raw = cloneBytes(item)
	}
	return addresses, nil
}

// WithdrawInfo는 자산, 출금 키, 금액에 대한 출금 수수료와 한도를 조회한다.
// Kraken은 이 endpoint를 deprecated로 표시하며 Funding (Beta) API 사용을 권장한다.
func (client *Client) WithdrawInfo(
	ctx context.Context,
	request WithdrawInfoRequest,
	options ...trade.RequestOption,
) (WithdrawInfo, error) {
	if err := request.validate(); err != nil {
		return WithdrawInfo{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"WithdrawInfo", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return WithdrawInfo{}, err
	}
	var info WithdrawInfo
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &info); err != nil {
		return WithdrawInfo{}, err
	}
	info.Raw = cloneBytes(result)
	return info, nil
}

// Withdraw는 출금을 요청한다. 접수된 출금은 WithdrawStatus로 추적하고 WithdrawCancel로 취소할 수 있다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
// Kraken은 이 endpoint를 deprecated로 표시하며 Funding (Beta) API 사용을 권장한다.
func (client *Client) Withdraw(
	ctx context.Context,
	request WithdrawRequest,
	options ...trade.RequestOption,
) (WithdrawReference, error) {
	if err := request.validate(); err != nil {
		return WithdrawReference{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"Withdraw", request.values(), credential.PermissionWithdraw,
		commonexchange.OperationMutation, limitPrivate, "", options...,
	)
	if err != nil {
		return WithdrawReference{}, err
	}
	var reference WithdrawReference
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &reference); err != nil {
		return WithdrawReference{}, err
	}
	if reference.ReferenceID == "" {
		return WithdrawReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Kraken withdrawal acknowledgement is empty"),
		)
	}
	reference.Raw = cloneBytes(result)
	return reference, nil
}

// WithdrawStatus는 최근 출금 상태를 최신순으로 조회한다.
// Kraken은 이 endpoint를 deprecated로 표시하며 Funding (Beta) API 사용을 권장한다.
func (client *Client) WithdrawStatus(
	ctx context.Context,
	request FundingStatusRequest,
	options ...trade.RequestOption,
) (WithdrawStatusPage, error) {
	if err := request.validate(); err != nil {
		return WithdrawStatusPage{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"WithdrawStatus", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return WithdrawStatusPage{}, err
	}
	items, nextCursor, err := decodeFundingPage(result)
	if err != nil {
		return WithdrawStatusPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	page := WithdrawStatusPage{
		Withdrawals: make([]WithdrawRecord, len(items)), NextCursor: nextCursor, Raw: cloneBytes(result),
	}
	for index, item := range items {
		if err := client.decodeResultInto(
			response, commonexchange.OperationRead, item, &page.Withdrawals[index],
		); err != nil {
			return WithdrawStatusPage{}, err
		}
		page.Withdrawals[index].Raw = cloneBytes(item)
	}
	return page, nil
}

// WithdrawCancel은 아직 처리되지 않은 출금의 취소를 요청한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
// Kraken은 이 endpoint를 deprecated로 표시하며 Funding (Beta) API 사용을 권장한다.
func (client *Client) WithdrawCancel(
	ctx context.Context,
	request WithdrawCancelRequest,
	options ...trade.RequestOption,
) (WithdrawCancelResult, error) {
	if err := request.validate(); err != nil {
		return WithdrawCancelResult{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"WithdrawCancel", request.values(), credential.PermissionWithdraw,
		commonexchange.OperationMutation, limitPrivate, "", options...,
	)
	if err != nil {
		return WithdrawCancelResult{}, err
	}
	var canceled bool
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &canceled); err != nil {
		return WithdrawCancelResult{}, err
	}
	return WithdrawCancelResult{Canceled: canceled, Raw: cloneBytes(result)}, nil
}

// WalletTransfer는 Kraken Spot 지갑의 자산을 Futures 지갑으로 이체한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
// Kraken은 이 endpoint를 deprecated로 표시하며 Funding (Beta) API 사용을 권장한다.
func (client *Client) WalletTransfer(
	ctx context.Context,
	request WalletTransferRequest,
	options ...trade.RequestOption,
) (WalletTransferResult, error) {
	if err := request.validate(); err != nil {
		return WalletTransferResult{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"WalletTransfer", request.values(), credential.PermissionTransfer,
		commonexchange.OperationMutation, limitPrivate, "", options...,
	)
	if err != nil {
		return WalletTransferResult{}, err
	}
	var transfer WalletTransferResult
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &transfer); err != nil {
		return WalletTransferResult{}, err
	}
	if transfer.ReferenceID == "" {
		return WalletTransferResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Kraken wallet transfer acknowledgement is empty"),
		)
	}
	transfer.Raw = cloneBytes(result)
	return transfer, nil
}

// CreateSubaccount는 거래 서브계정을 만든다. 기관 마스터 계정의 API Key로만 호출할 수 있다.
// Kraken은 이 호출에 Funds permissions - Withdraw 권한을 요구한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) CreateSubaccount(
	ctx context.Context,
	request CreateSubaccountRequest,
	options ...trade.RequestOption,
) (CreateSubaccountResult, error) {
	if err := request.validate(); err != nil {
		return CreateSubaccountResult{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"CreateSubaccount", request.values(), credential.PermissionTransfer,
		commonexchange.OperationMutation, limitPrivate, "", options...,
	)
	if err != nil {
		return CreateSubaccountResult{}, err
	}
	var created bool
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &created); err != nil {
		return CreateSubaccountResult{}, err
	}
	if !created {
		return CreateSubaccountResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Kraken subaccount creation was not acknowledged"),
		)
	}
	return CreateSubaccountResult{Created: true, Raw: cloneBytes(result)}, nil
}

// AccountTransfer는 마스터 계정과 서브계정 사이에서 자산을 이체한다. 마스터 계정의 API Key로만 호출할 수 있다.
// Kraken은 이 호출에 Funds permissions - Withdraw 권한을 요구한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) AccountTransfer(
	ctx context.Context,
	request AccountTransferRequest,
	options ...trade.RequestOption,
) (AccountTransferResult, error) {
	if err := request.validate(); err != nil {
		return AccountTransferResult{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"AccountTransfer", request.values(), credential.PermissionTransfer,
		commonexchange.OperationMutation, limitPrivate, "", options...,
	)
	if err != nil {
		return AccountTransferResult{}, err
	}
	var transfer AccountTransferResult
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &transfer); err != nil {
		return AccountTransferResult{}, err
	}
	if transfer.TransferID == "" {
		return AccountTransferResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Kraken account transfer acknowledgement is empty"),
		)
	}
	transfer.Raw = cloneBytes(result)
	return transfer, nil
}

// decodeFundingPage는 배열 또는 {항목 배열, next_cursor} 객체 형태의 입출금 내역 result를 분해한다.
func decodeFundingPage(result json.RawMessage) ([]json.RawMessage, string, error) {
	trimmed := bytes.TrimSpace(result)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, "", err
		}
		return items, "", nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, "", err
	}
	var nextCursor string
	if raw, exists := fields["next_cursor"]; exists && string(raw) != "null" {
		if err := json.Unmarshal(raw, &nextCursor); err != nil {
			return nil, "", err
		}
	}
	for _, key := range sortedKeys(fields) {
		raw := bytes.TrimSpace(fields[key])
		if len(raw) == 0 || raw[0] != '[' {
			continue
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, "", err
		}
		return items, nextCursor, nil
	}
	return nil, nextCursor, nil
}
