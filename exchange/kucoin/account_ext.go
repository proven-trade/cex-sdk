package kucoin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

var alphanumericPattern = regexp.MustCompile(`^[0-9A-Za-z]+$`)

// AccountSummary는 VIP 등급과 서브계정 개수 한도다. V3 응답이다.
type AccountSummary struct {
	Level              int             `json:"level"`
	MaxSharedSubCount  int             `json:"maxSharedSubCount"`
	UsedSharedSubCount int             `json:"usedSharedSubCount"`
	Raw                json.RawMessage `json:"-"`
}

// AccountSummaryV2는 VIP 등급과 서브계정 유형별 개수·한도다.
type AccountSummaryV2 struct {
	Level                 int             `json:"level"`
	SubQuantity           int             `json:"subQuantity"`
	SpotSubQuantity       int             `json:"spotSubQuantity"`
	MarginSubQuantity     int             `json:"marginSubQuantity"`
	FuturesSubQuantity    int             `json:"futuresSubQuantity"`
	OptionSubQuantity     int             `json:"optionSubQuantity"`
	MaxSubQuantity        int             `json:"maxSubQuantity"`
	MaxDefaultSubQuantity int             `json:"maxDefaultSubQuantity"`
	MaxSpotSubQuantity    int             `json:"maxSpotSubQuantity"`
	MaxMarginSubQuantity  int             `json:"maxMarginSubQuantity"`
	MaxFuturesSubQuantity int             `json:"maxFuturesSubQuantity"`
	MaxOptionSubQuantity  int             `json:"maxOptionSubQuantity"`
	Raw                   json.RawMessage `json:"-"`
}

// APIKeyInfo는 현재 API Key의 권한과 IP 허용 목록 정보다.
type APIKeyInfo struct {
	UID           int64           `json:"uid"`
	ParentUID     int64           `json:"parentUid"`
	Region        string          `json:"region"`
	KYCStatus     int             `json:"kycStatus"`
	SubName       string          `json:"subName"`
	Remark        string          `json:"remark"`
	APIKey        string          `json:"apiKey"`
	APIVersion    int             `json:"apiVersion"`
	Permission    string          `json:"permission"`
	IPWhitelist   string          `json:"ipWhitelist"`
	IsMaster      bool            `json:"isMaster"`
	CreatedAt     int64           `json:"createdAt"`
	ExpiredAt     int64           `json:"expiredAt"`
	ThirdPartyApp string          `json:"thirdPartyApp"`
	SiteType      string          `json:"siteType"`
	Raw           json.RawMessage `json:"-"`
}

// AccountDetail은 계정 ID로 조회한 단일 Spot 계정 잔고다.
type AccountDetail struct {
	Currency  string          `json:"currency"`
	Balance   string          `json:"balance"`
	Available string          `json:"available"`
	Holds     string          `json:"holds"`
	Raw       json.RawMessage `json:"-"`
}

// LedgerDirection은 원장 입출금 방향이다.
type LedgerDirection string

const (
	LedgerIn  LedgerDirection = "in"
	LedgerOut LedgerDirection = "out"
)

func (direction LedgerDirection) valid() bool {
	return direction == "" || direction == LedgerIn || direction == LedgerOut
}

func validateLedgerCurrencies(currencies []string) error {
	if len(currencies) > 10 {
		return validationError("ledger currencies cannot exceed 10")
	}
	for _, currency := range currencies {
		if err := validateCurrency(currency); err != nil {
			return err
		}
	}
	return nil
}

// LedgersRequest는 Spot/Margin 원장 조회 조건이다. Currencies는 최대 10개, PageSize는 10~500(기본 50)이다.
type LedgersRequest struct {
	Currencies  []string
	Direction   LedgerDirection
	BizType     string
	Start       *time.Time
	End         *time.Time
	CurrentPage int
	PageSize    int
}

func (request LedgersRequest) validate() error {
	if err := validateLedgerCurrencies(request.Currencies); err != nil {
		return err
	}
	if !request.Direction.valid() {
		return validationError("ledger direction must be in, out, or empty")
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	return validatePage(request.CurrentPage, request.PageSize, 10, 500)
}

func (request LedgersRequest) values() url.Values {
	values := make(url.Values)
	if len(request.Currencies) > 0 {
		values.Set("currency", joinList(request.Currencies))
	}
	setIfNotEmpty(values, "direction", string(request.Direction))
	setIfNotEmpty(values, "bizType", request.BizType)
	setMillis(values, "startAt", request.Start)
	setMillis(values, "endAt", request.End)
	setPositiveInt(values, "currentPage", request.CurrentPage)
	setPositiveInt(values, "pageSize", request.PageSize)
	return values
}

// Ledger는 Spot/Margin 원장 항목 한 건이다.
type Ledger struct {
	ID          string          `json:"id"`
	Currency    string          `json:"currency"`
	Amount      string          `json:"amount"`
	Fee         string          `json:"fee"`
	Balance     string          `json:"balance"`
	AccountType string          `json:"accountType"`
	BizType     string          `json:"bizType"`
	Direction   string          `json:"direction"`
	CreatedAt   int64           `json:"createdAt"`
	Context     string          `json:"context"`
	Raw         json.RawMessage `json:"-"`
}

// HFLedgersRequest는 trade_hf 원장 조회 조건이다. Limit은 1~200(기본 100)이다.
type HFLedgersRequest struct {
	Currencies []string
	Direction  LedgerDirection
	BizType    string
	LastID     int64
	Limit      int
	Start      *time.Time
	End        *time.Time
}

func (request HFLedgersRequest) validate() error {
	if err := validateLedgerCurrencies(request.Currencies); err != nil {
		return err
	}
	if !request.Direction.valid() {
		return validationError("ledger direction must be in, out, or empty")
	}
	if request.LastID < 0 {
		return validationError("lastId cannot be negative")
	}
	if err := validatePageSize("limit", request.Limit, 1, 200); err != nil {
		return err
	}
	return validateTimeRange(request.Start, request.End)
}

func (request HFLedgersRequest) values() url.Values {
	values := make(url.Values)
	if len(request.Currencies) > 0 {
		values.Set("currency", joinList(request.Currencies))
	}
	setIfNotEmpty(values, "direction", string(request.Direction))
	setIfNotEmpty(values, "bizType", request.BizType)
	setPositiveInt64(values, "lastId", request.LastID)
	setPositiveInt(values, "limit", request.Limit)
	setMillis(values, "startAt", request.Start)
	setMillis(values, "endAt", request.End)
	return values
}

// HFLedger는 trade_hf 원장 항목 한 건이다. CreatedAt은 문자열·숫자 모두 원문으로 보존한다.
type HFLedger struct {
	ID          string          `json:"id"`
	Currency    string          `json:"currency"`
	Amount      string          `json:"amount"`
	Fee         string          `json:"fee"`
	Tax         string          `json:"tax"`
	Balance     string          `json:"balance"`
	AccountType string          `json:"accountType"`
	BizType     string          `json:"bizType"`
	Direction   string          `json:"direction"`
	CreatedAt   DecimalString   `json:"createdAt"`
	Context     string          `json:"context"`
	Raw         json.RawMessage `json:"-"`
}

// FeeRate는 기본 또는 거래쌍별 수수료율이다. Symbol은 기본 수수료율 응답에서 비어 있다.
type FeeRate struct {
	Symbol       string          `json:"symbol"`
	TakerFeeRate string          `json:"takerFeeRate"`
	MakerFeeRate string          `json:"makerFeeRate"`
	SellTaxRate  string          `json:"sellTaxRate"`
	BuyTaxRate   string          `json:"buyTaxRate"`
	Raw          json.RawMessage `json:"-"`
}

// CurrencyType은 기본 수수료율의 통화 구분이다.
type CurrencyType int

const (
	CurrencyTypeCrypto CurrencyType = 0
	CurrencyTypeFiat   CurrencyType = 1
)

// DepositAddressRequest는 입금 주소 조회 조건이다. Chain은 다중 체인 통화에서 사용한다.
type DepositAddressRequest struct {
	Currency string
	Chain    string
}

func (request DepositAddressRequest) validate() error {
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	return validateOptionalChain(request.Chain)
}

func (request DepositAddressRequest) values() url.Values {
	values := url.Values{"currency": {request.Currency}}
	setIfNotEmpty(values, "chain", request.Chain)
	return values
}

// DepositAddress는 입금 주소와 메모·체인 정보다.
type DepositAddress struct {
	Address         string          `json:"address"`
	Memo            string          `json:"memo"`
	ChainID         string          `json:"chainId"`
	To              string          `json:"to"`
	Currency        string          `json:"currency"`
	ContractAddress string          `json:"contractAddress"`
	ChainName       string          `json:"chainName"`
	Remark          string          `json:"remark"`
	ExpirationDate  int64           `json:"expirationDate"`
	Raw             json.RawMessage `json:"-"`
}

// CreateDepositAddressRequest는 입금 주소 생성 조건이다. To는 main(기본) 또는 trade다.
type CreateDepositAddressRequest struct {
	Currency string `json:"currency"`
	Chain    string `json:"chain"`
	To       string `json:"to,omitempty"`
	Amount   string `json:"amount,omitempty"`
}

func (request CreateDepositAddressRequest) validate() error {
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	if !chainPattern.MatchString(request.Chain) {
		return validationError("deposit address chain is required")
	}
	if request.To != "" && request.To != "main" && request.To != "trade" {
		return validationError("deposit address destination must be main, trade, or empty")
	}
	if request.Amount != "" {
		return validatePositiveDecimal("amount", request.Amount)
	}
	return nil
}

// DepositsRequest는 입금 내역 조회 조건이다. Status는 PROCESSING, SUCCESS, FAILURE,
// WAIT_TRM_MGT, TRM_MGT_REJECTED 중 하나이며 PageSize는 10~500(기본 50)이다.
type DepositsRequest struct {
	Currency    string
	Status      string
	Start       *time.Time
	End         *time.Time
	CurrentPage int
	PageSize    int
}

func (request DepositsRequest) validate() error {
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	switch request.Status {
	case "", "PROCESSING", "SUCCESS", "FAILURE", "WAIT_TRM_MGT", "TRM_MGT_REJECTED":
	default:
		return validationError("unsupported deposit status %q", request.Status)
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	return validatePage(request.CurrentPage, request.PageSize, 10, 500)
}

func (request DepositsRequest) values() url.Values {
	return historyPageValues(request.Currency, request.Status, request.Start, request.End, request.CurrentPage, request.PageSize)
}

func historyPageValues(currency, status string, start, end *time.Time, currentPage, pageSize int) url.Values {
	values := url.Values{"currency": {currency}}
	setIfNotEmpty(values, "status", status)
	setMillis(values, "startAt", start)
	setMillis(values, "endAt", end)
	setPositiveInt(values, "currentPage", currentPage)
	setPositiveInt(values, "pageSize", pageSize)
	return values
}

// Deposit은 입금 내역 한 건이다.
type Deposit struct {
	ID            string          `json:"id"`
	Currency      string          `json:"currency"`
	Chain         string          `json:"chain"`
	Status        string          `json:"status"`
	SubStatus     string          `json:"subStatus"`
	Address       string          `json:"address"`
	Memo          string          `json:"memo"`
	IsInner       bool            `json:"isInner"`
	Amount        string          `json:"amount"`
	Fee           string          `json:"fee"`
	WalletTxID    string          `json:"walletTxId"`
	CreatedAt     int64           `json:"createdAt"`
	UpdatedAt     int64           `json:"updatedAt"`
	Remark        string          `json:"remark"`
	Arrears       bool            `json:"arrears"`
	URL           string          `json:"url"`
	NeedSubmitWht string          `json:"needSubmitWht"`
	Raw           json.RawMessage `json:"-"`
}

// WithdrawalQuotasRequest는 출금 한도·수수료 조회 조건이다.
type WithdrawalQuotasRequest struct {
	Currency string
	Chain    string
}

func (request WithdrawalQuotasRequest) validate() error {
	return DepositAddressRequest(request).validate()
}

func (request WithdrawalQuotasRequest) values() url.Values {
	return DepositAddressRequest(request).values()
}

// WithdrawalQuotas는 출금 한도, 잔여 한도, 최소 수수료와 출금 가능 여부다.
type WithdrawalQuotas struct {
	Currency                 string          `json:"currency"`
	LimitBTCAmount           string          `json:"limitBTCAmount"`
	UsedBTCAmount            string          `json:"usedBTCAmount"`
	QuotaCurrency            string          `json:"quotaCurrency"`
	LimitQuotaCurrencyAmount string          `json:"limitQuotaCurrencyAmount"`
	UsedQuotaCurrencyAmount  string          `json:"usedQuotaCurrencyAmount"`
	RemainAmount             string          `json:"remainAmount"`
	AvailableAmount          string          `json:"availableAmount"`
	WithdrawMinFee           string          `json:"withdrawMinFee"`
	InnerWithdrawMinFee      string          `json:"innerWithdrawMinFee"`
	WithdrawMinSize          string          `json:"withdrawMinSize"`
	WithdrawEnabled          bool            `json:"isWithdrawEnabled"`
	Precision                int             `json:"precision"`
	Chain                    string          `json:"chain"`
	Reason                   string          `json:"reason"`
	LockedAmount             string          `json:"lockedAmount"`
	Raw                      json.RawMessage `json:"-"`
}

// WithdrawType은 출금 대상 지정 방식이다.
type WithdrawType string

const (
	WithdrawTypeAddress WithdrawType = "ADDRESS"
	WithdrawTypeUID     WithdrawType = "UID"
	WithdrawTypeMail    WithdrawType = "MAIL"
	WithdrawTypePhone   WithdrawType = "PHONE"
)

// FeeDeductType은 출금 수수료 차감 방식이다.
type FeeDeductType string

const (
	FeeDeductInternal FeeDeductType = "INTERNAL"
	FeeDeductExternal FeeDeductType = "EXTERNAL"
)

// WithdrawRequest는 출금 요청이다. 자금이 거래소 밖으로 나가므로 호출자는 중복 요청을 직접 방지해야 한다.
type WithdrawRequest struct {
	Currency      string        `json:"currency"`
	ToAddress     string        `json:"toAddress"`
	Amount        string        `json:"amount"`
	WithdrawType  WithdrawType  `json:"withdrawType"`
	Chain         string        `json:"chain,omitempty"`
	Memo          string        `json:"memo,omitempty"`
	IsInner       bool          `json:"isInner,omitempty"`
	Remark        string        `json:"remark,omitempty"`
	FeeDeductType FeeDeductType `json:"feeDeductType,omitempty"`
}

func (request WithdrawRequest) validate() error {
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	if strings.TrimSpace(request.ToAddress) == "" || len(request.ToAddress) > 512 {
		return validationError("withdrawal destination is required")
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return err
	}
	switch request.WithdrawType {
	case WithdrawTypeAddress, WithdrawTypeUID, WithdrawTypeMail, WithdrawTypePhone:
	default:
		return validationError("withdraw type must be ADDRESS, UID, MAIL, or PHONE")
	}
	if err := validateOptionalChain(request.Chain); err != nil {
		return err
	}
	if len(request.Memo) > 256 || len(request.Remark) > 256 {
		return validationError("withdrawal memo and remark are too long")
	}
	if request.FeeDeductType != "" && request.FeeDeductType != FeeDeductInternal && request.FeeDeductType != FeeDeductExternal {
		return validationError("fee deduct type must be INTERNAL, EXTERNAL, or empty")
	}
	return nil
}

// WithdrawalReference는 출금 요청 접수 결과다.
type WithdrawalReference struct {
	WithdrawalID string          `json:"withdrawalId"`
	Raw          json.RawMessage `json:"-"`
}

// WithdrawalsRequest는 출금 내역 조회 조건이다. Status는 PROCESSING, REVIEW, WALLET_PROCESSING,
// SUCCESS, FAILURE 중 하나이며 PageSize는 10~500이다.
type WithdrawalsRequest struct {
	Currency    string
	Status      string
	Start       *time.Time
	End         *time.Time
	CurrentPage int
	PageSize    int
}

func (request WithdrawalsRequest) validate() error {
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	switch request.Status {
	case "", "PROCESSING", "REVIEW", "WALLET_PROCESSING", "SUCCESS", "FAILURE":
	default:
		return validationError("unsupported withdrawal status %q", request.Status)
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	return validatePage(request.CurrentPage, request.PageSize, 10, 500)
}

func (request WithdrawalsRequest) values() url.Values {
	return historyPageValues(request.Currency, request.Status, request.Start, request.End, request.CurrentPage, request.PageSize)
}

// Withdrawal은 출금 내역 한 건이다.
type Withdrawal struct {
	ID               string          `json:"id"`
	Currency         string          `json:"currency"`
	Chain            string          `json:"chain"`
	Status           string          `json:"status"`
	Address          string          `json:"address"`
	Memo             string          `json:"memo"`
	IsInner          bool            `json:"isInner"`
	Amount           string          `json:"amount"`
	Fee              string          `json:"fee"`
	WalletTxID       string          `json:"walletTxId"`
	CreatedAt        int64           `json:"createdAt"`
	UpdatedAt        int64           `json:"updatedAt"`
	Remark           string          `json:"remark"`
	FailureReason    string          `json:"failureReason"`
	FailureReasonMsg string          `json:"failureReasonMsg"`
	Raw              json.RawMessage `json:"-"`
}

// WithdrawalDetail은 출금 ID로 조회한 출금 상세다.
type WithdrawalDetail struct {
	ID               string            `json:"id"`
	UID              int64             `json:"uid"`
	Currency         string            `json:"currency"`
	ChainID          string            `json:"chainId"`
	ChainName        string            `json:"chainName"`
	CurrencyName     string            `json:"currencyName"`
	Status           string            `json:"status"`
	FailureReason    string            `json:"failureReason"`
	FailureReasonMsg string            `json:"failureReasonMsg"`
	Address          string            `json:"address"`
	Memo             string            `json:"memo"`
	IsInner          bool              `json:"isInner"`
	Amount           string            `json:"amount"`
	Fee              string            `json:"fee"`
	WalletTxID       string            `json:"walletTxId"`
	AddressRemark    string            `json:"addressRemark"`
	Remark           string            `json:"remark"`
	CreatedAt        int64             `json:"createdAt"`
	CancelType       string            `json:"cancelType"`
	Taxes            []json.RawMessage `json:"taxes"`
	TaxDescription   string            `json:"taxDescription"`
	ReturnStatus     string            `json:"returnStatus"`
	ReturnAmount     string            `json:"returnAmount"`
	ReturnCurrency   string            `json:"returnCurrency"`
	TxID             string            `json:"txId"`
	Raw              json.RawMessage   `json:"-"`
}

// TransferableRequest는 이체 가능 수량 조회 조건이다.
// Type은 MAIN, TRADE, MARGIN, ISOLATED, MARGIN_V2, ISOLATED_V2이며 ISOLATED 계열은 Tag에 거래쌍이 필요하다.
type TransferableRequest struct {
	Currency string
	Type     string
	Tag      string
}

func (request TransferableRequest) validate() error {
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	switch request.Type {
	case "MAIN", "TRADE", "MARGIN", "MARGIN_V2":
		if request.Tag != "" {
			return validationError("tag is only accepted for ISOLATED account types")
		}
	case "ISOLATED", "ISOLATED_V2":
		return validateSymbol(request.Tag)
	default:
		return validationError("unsupported account type %q", request.Type)
	}
	return nil
}

func (request TransferableRequest) values() url.Values {
	values := url.Values{"currency": {request.Currency}, "type": {request.Type}}
	setIfNotEmpty(values, "tag", request.Tag)
	return values
}

// Transferable은 계정의 이체 가능 수량이다.
type Transferable struct {
	Currency     string          `json:"currency"`
	Balance      string          `json:"balance"`
	Available    string          `json:"available"`
	Holds        string          `json:"holds"`
	Transferable string          `json:"transferable"`
	Raw          json.RawMessage `json:"-"`
}

// FlexTransferType은 Flex 이체 방향이다.
type FlexTransferType string

const (
	FlexTransferInternal    FlexTransferType = "INTERNAL"
	FlexTransferParentToSub FlexTransferType = "PARENT_TO_SUB"
	FlexTransferSubToParent FlexTransferType = "SUB_TO_PARENT"
	FlexTransferSubToSub    FlexTransferType = "SUB_TO_SUB"
)

// FlexTransferRequest는 계정·서브계정 간 Flex 이체 조건이다.
// 계정 유형은 MAIN, TRADE, CONTRACT, MARGIN, ISOLATED, MARGIN_V2, ISOLATED_V2, UNIFIED다.
type FlexTransferRequest struct {
	ClientOrderID   string           `json:"clientOid"`
	Currency        string           `json:"currency"`
	Amount          string           `json:"amount"`
	Type            FlexTransferType `json:"type"`
	FromUserID      string           `json:"fromUserId,omitempty"`
	FromAccountType string           `json:"fromAccountType"`
	FromAccountTag  string           `json:"fromAccountTag,omitempty"`
	ToUserID        string           `json:"toUserId,omitempty"`
	ToAccountType   string           `json:"toAccountType"`
	ToAccountTag    string           `json:"toAccountTag,omitempty"`
}

func validFlexAccountType(value string) bool {
	switch value {
	case "MAIN", "TRADE", "CONTRACT", "MARGIN", "ISOLATED", "MARGIN_V2", "ISOLATED_V2", "UNIFIED":
		return true
	}
	return false
}

func validateFlexTag(accountType, tag string) error {
	if accountType == "ISOLATED" || accountType == "ISOLATED_V2" {
		return validateSymbol(tag)
	}
	if tag != "" {
		return validationError("account tag is only accepted for ISOLATED account types")
	}
	return nil
}

func (request FlexTransferRequest) validate() error {
	if !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("transfer client ID is required and must match [0-9A-Za-z_-]{1,40}")
	}
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return err
	}
	if !validFlexAccountType(request.FromAccountType) || !validFlexAccountType(request.ToAccountType) {
		return validationError("unsupported transfer account type")
	}
	if err := validateFlexTag(request.FromAccountType, request.FromAccountTag); err != nil {
		return err
	}
	if err := validateFlexTag(request.ToAccountType, request.ToAccountTag); err != nil {
		return err
	}
	switch request.Type {
	case FlexTransferInternal:
	case FlexTransferParentToSub:
		if request.ToUserID == "" {
			return validationError("toUserId is required for PARENT_TO_SUB")
		}
	case FlexTransferSubToParent:
		if request.FromUserID == "" {
			return validationError("fromUserId is required for SUB_TO_PARENT")
		}
	case FlexTransferSubToSub:
		if request.FromUserID == "" || request.ToUserID == "" {
			return validationError("fromUserId and toUserId are required for SUB_TO_SUB")
		}
	default:
		return validationError("unsupported transfer type %q", request.Type)
	}
	return nil
}

// TransferReference는 이체 접수 결과다.
type TransferReference struct {
	OrderID string          `json:"orderId"`
	Raw     json.RawMessage `json:"-"`
}

// CreateSubAccountRequest는 서브계정 생성 조건이다.
// Access는 Spot, Futures, Margin을 쉼표로 조합하고 Mode는 CLASSIC 또는 UNIFIED(선택)다.
type CreateSubAccountRequest struct {
	Password string `json:"password"`
	Remarks  string `json:"remarks,omitempty"`
	SubName  string `json:"subName"`
	Access   string `json:"access"`
	Mode     string `json:"mode,omitempty"`
}

func (request CreateSubAccountRequest) validate() error {
	if n := len(request.Password); n < 7 || n > 24 || !alphanumericPattern.MatchString(request.Password) ||
		strings.IndexFunc(request.Password, unicode.IsLetter) < 0 ||
		strings.IndexFunc(request.Password, unicode.IsDigit) < 0 {
		return validationError("sub-account password must be 7-24 alphanumeric characters with letters and numbers")
	}
	if n := len(request.SubName); n < 7 || n > 32 || !alphanumericPattern.MatchString(request.SubName) {
		return validationError("sub-account name must be 7-32 alphanumeric characters")
	}
	if len(request.Remarks) > 24 {
		return validationError("sub-account remarks cannot exceed 24 characters")
	}
	if request.Access == "" {
		return validationError("sub-account access is required")
	}
	for _, item := range strings.Split(request.Access, ",") {
		if item != "Spot" && item != "Futures" && item != "Margin" {
			return validationError("sub-account access must combine Spot, Futures, and Margin")
		}
	}
	if request.Mode != "" && request.Mode != "CLASSIC" && request.Mode != "UNIFIED" {
		return validationError("sub-account mode must be CLASSIC, UNIFIED, or empty")
	}
	return nil
}

// SubAccount는 서브계정 생성 결과다.
type SubAccount struct {
	UID     int64           `json:"uid"`
	SubName string          `json:"subName"`
	Remarks string          `json:"remarks"`
	Access  string          `json:"access"`
	Raw     json.RawMessage `json:"-"`
}

// SubAccountPermissionRequest는 서브계정 마진·선물 권한 부여 대상이다.
type SubAccountPermissionRequest struct {
	UID string `json:"uid"`
}

func (request SubAccountPermissionRequest) validate() error {
	return validateID("sub-account UID", request.UID)
}

// SubAccountsRequest는 서브계정 목록 조회 조건이다. PageSize는 요약 1~100, 잔고 10~100이다.
type SubAccountsRequest struct {
	CurrentPage int
	PageSize    int
}

func (request SubAccountsRequest) values() url.Values {
	values := make(url.Values)
	setPositiveInt(values, "currentPage", request.CurrentPage)
	setPositiveInt(values, "pageSize", request.PageSize)
	return values
}

// SubAccountSummary는 서브계정 요약 한 건이다.
type SubAccountSummary struct {
	UserID           string          `json:"userId"`
	UID              int64           `json:"uid"`
	SubName          string          `json:"subName"`
	Status           int             `json:"status"`
	Type             int             `json:"type"`
	Access           string          `json:"access"`
	CreatedAt        int64           `json:"createdAt"`
	Remarks          string          `json:"remarks"`
	TradeTypes       []string        `json:"tradeTypes"`
	OpenedTradeTypes []string        `json:"openedTradeTypes"`
	HostedStatus     string          `json:"hostedStatus"`
	Raw              json.RawMessage `json:"-"`
}

// SubAccountCurrencyBalance는 서브계정의 계정 유형별 통화 잔고다.
type SubAccountCurrencyBalance struct {
	Currency          string `json:"currency"`
	Balance           string `json:"balance"`
	Available         string `json:"available"`
	Holds             string `json:"holds"`
	BaseCurrency      string `json:"baseCurrency"`
	BaseCurrencyPrice string `json:"baseCurrencyPrice"`
	BaseAmount        string `json:"baseAmount"`
	Tag               string `json:"tag"`
}

// SubAccountBalances는 서브계정 한 개의 Spot 잔고다.
type SubAccountBalances struct {
	SubUserID       string                      `json:"subUserId"`
	SubName         string                      `json:"subName"`
	MainAccounts    []SubAccountCurrencyBalance `json:"mainAccounts"`
	TradeAccounts   []SubAccountCurrencyBalance `json:"tradeAccounts"`
	MarginAccounts  []SubAccountCurrencyBalance `json:"marginAccounts"`
	TradeHFAccounts []SubAccountCurrencyBalance `json:"tradeHFAccounts"`
	Raw             json.RawMessage             `json:"-"`
}

// SubAccountBalanceRequest는 서브계정 잔고 상세 조회 조건이다.
type SubAccountBalanceRequest struct {
	SubUserID         string
	IncludeBaseAmount bool
	BaseCurrency      string
	BaseAmount        string
}

func (request SubAccountBalanceRequest) validate() error {
	if err := validateID("sub-account user ID", request.SubUserID); err != nil {
		return err
	}
	if err := validateOptionalCurrency(request.BaseCurrency); err != nil {
		return err
	}
	if request.BaseAmount != "" {
		return validatePositiveDecimal("baseAmount", request.BaseAmount)
	}
	return nil
}

func (request SubAccountBalanceRequest) values() url.Values {
	values := make(url.Values)
	if request.IncludeBaseAmount {
		values.Set("includeBaseAmount", "true")
	}
	setIfNotEmpty(values, "baseCurrency", request.BaseCurrency)
	setIfNotEmpty(values, "baseAmount", request.BaseAmount)
	return values
}

// SubAPIKeysRequest는 서브계정 API Key 목록 조회 조건이다.
type SubAPIKeysRequest struct {
	SubName string
	APIKey  string
}

func (request SubAPIKeysRequest) validate() error {
	if !alphanumericPattern.MatchString(request.SubName) {
		return validationError("sub-account name is required")
	}
	if request.APIKey != "" {
		return validateID("API key", request.APIKey)
	}
	return nil
}

func (request SubAPIKeysRequest) values() url.Values {
	values := url.Values{"subName": {request.SubName}}
	setIfNotEmpty(values, "apiKey", request.APIKey)
	return values
}

// SubAPIKey는 서브계정 API Key 한 건이다. 비밀값은 포함하지 않는다.
type SubAPIKey struct {
	SubName     string          `json:"subName"`
	Remark      string          `json:"remark"`
	APIKey      string          `json:"apiKey"`
	APIVersion  int             `json:"apiVersion"`
	Permission  string          `json:"permission"`
	IPWhitelist string          `json:"ipWhitelist"`
	CreatedAt   int64           `json:"createdAt"`
	UID         int64           `json:"uid"`
	IsMaster    bool            `json:"isMaster"`
	Raw         json.RawMessage `json:"-"`
}

// CreateSubAPIKeyRequest는 서브계정 API Key 생성 조건이다.
// Permission은 General, Spot, Futures, Margin, Unified, InnerTransfer를 쉼표로 조합하고,
// Expire는 -1(무기한), 30, 90, 180, 360 중 하나다.
type CreateSubAPIKeyRequest struct {
	SubName     string `json:"subName"`
	Passphrase  string `json:"passphrase"`
	Remark      string `json:"remark"`
	Permission  string `json:"permission,omitempty"`
	IPWhitelist string `json:"ipWhitelist,omitempty"`
	Expire      string `json:"expire,omitempty"`
}

func (request CreateSubAPIKeyRequest) validate() error {
	if !alphanumericPattern.MatchString(request.SubName) {
		return validationError("sub-account name is required")
	}
	if err := validateSubAPIPassphrase(request.Passphrase); err != nil {
		return err
	}
	if n := len(request.Remark); n < 1 || n > 24 {
		return validationError("sub-account API key remark must be 1-24 characters")
	}
	return validateSubAPIOptions(request.Permission, request.IPWhitelist, request.Expire)
}

func validateSubAPIPassphrase(passphrase string) error {
	if n := len(passphrase); n < 7 || n > 32 {
		return validationError("sub-account API key passphrase must be 7-32 characters")
	}
	return nil
}

func validateSubAPIOptions(permission, ipWhitelist, expire string) error {
	if permission != "" {
		for _, item := range strings.Split(permission, ",") {
			switch item {
			case "General", "Spot", "Futures", "Margin", "Unified", "InnerTransfer":
			default:
				return validationError("unsupported API key permission %q", item)
			}
		}
	}
	if ipWhitelist != "" && len(strings.Split(ipWhitelist, ",")) > 20 {
		return validationError("IP whitelist cannot exceed 20 addresses")
	}
	switch expire {
	case "", "-1", "30", "90", "180", "360":
	default:
		return validationError("API key expire must be -1, 30, 90, 180, or 360")
	}
	return nil
}

// CreatedSubAPIKey는 생성된 서브계정 API Key다. APISecret은 이 응답에서만 제공되므로 호출자가 보관해야 한다.
type CreatedSubAPIKey struct {
	SubName     string          `json:"subName"`
	Remark      string          `json:"remark"`
	APIKey      string          `json:"apiKey"`
	APISecret   string          `json:"apiSecret"`
	APIVersion  int             `json:"apiVersion"`
	Passphrase  string          `json:"passphrase"`
	Permission  string          `json:"permission"`
	IPWhitelist string          `json:"ipWhitelist"`
	CreatedAt   int64           `json:"createdAt"`
	Raw         json.RawMessage `json:"-"`
}

// ModifySubAPIKeyRequest는 서브계정 API Key 권한·IP·만료 수정 조건이다.
type ModifySubAPIKeyRequest struct {
	SubName     string `json:"subName"`
	APIKey      string `json:"apiKey"`
	Passphrase  string `json:"passphrase"`
	Permission  string `json:"permission,omitempty"`
	IPWhitelist string `json:"ipWhitelist,omitempty"`
	Expire      string `json:"expire,omitempty"`
}

func (request ModifySubAPIKeyRequest) validate() error {
	if !alphanumericPattern.MatchString(request.SubName) {
		return validationError("sub-account name is required")
	}
	if err := validateID("API key", request.APIKey); err != nil {
		return err
	}
	if err := validateSubAPIPassphrase(request.Passphrase); err != nil {
		return err
	}
	return validateSubAPIOptions(request.Permission, request.IPWhitelist, request.Expire)
}

// ModifiedSubAPIKey는 수정된 서브계정 API Key 설정이다.
type ModifiedSubAPIKey struct {
	SubName     string          `json:"subName"`
	APIKey      string          `json:"apiKey"`
	Permission  string          `json:"permission"`
	IPWhitelist string          `json:"ipWhitelist"`
	Raw         json.RawMessage `json:"-"`
}

// DeleteSubAPIKeyRequest는 서브계정 API Key 삭제 조건이다.
type DeleteSubAPIKeyRequest struct {
	SubName    string
	APIKey     string
	Passphrase string
}

func (request DeleteSubAPIKeyRequest) validate() error {
	if !alphanumericPattern.MatchString(request.SubName) {
		return validationError("sub-account name is required")
	}
	if err := validateID("API key", request.APIKey); err != nil {
		return err
	}
	return validateSubAPIPassphrase(request.Passphrase)
}

// DeletedSubAPIKey는 삭제된 서브계정 API Key 식별자다.
type DeletedSubAPIKey struct {
	SubName string          `json:"subName"`
	APIKey  string          `json:"apiKey"`
	Raw     json.RawMessage `json:"-"`
}

// readObject는 단일 객체 data를 조회해 디코딩하고 원본을 setRaw로 전달한다.
func readObject[T any](
	ctx context.Context,
	client *Client,
	path string,
	query url.Values,
	limit endpointLimit,
	setRaw func(*T, json.RawMessage),
	options ...trade.RequestOption,
) (T, error) {
	var value T
	data, err := client.readPrivate(ctx, path, query, limit, &value, options...)
	if err != nil {
		var zero T
		return zero, err
	}
	setRaw(&value, cloneBytes(data))
	return value, nil
}

func readList[T any](
	ctx context.Context,
	client *Client,
	path string,
	query url.Values,
	limit endpointLimit,
	setRaw func(*T, json.RawMessage),
	options ...trade.RequestOption,
) ([]T, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, path, query, nil, limit,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, setRaw)
}

func readPage[T any](
	ctx context.Context,
	client *Client,
	path string,
	query url.Values,
	limit endpointLimit,
	setRaw func(*T, json.RawMessage),
	options ...trade.RequestOption,
) (Page[T], error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, path, query, nil, limit,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Page[T]{}, err
	}
	return decodePage(client, response, commonexchange.OperationRead, setRaw)
}

// AccountSummary는 VIP 등급과 서브계정 개수 한도를 조회한다(V3).
func (client *Client) AccountSummary(
	ctx context.Context,
	options ...trade.RequestOption,
) (AccountSummary, error) {
	return readObject(ctx, client, "/api/v3/user-info", nil, managementLimit(20),
		func(item *AccountSummary, raw json.RawMessage) { item.Raw = raw }, options...)
}

// AccountSummaryV2는 VIP 등급과 서브계정 유형별 개수·한도를 조회한다(V2).
func (client *Client) AccountSummaryV2(
	ctx context.Context,
	options ...trade.RequestOption,
) (AccountSummaryV2, error) {
	return readObject(ctx, client, "/api/v2/user-info", nil, managementLimit(20),
		func(item *AccountSummaryV2, raw json.RawMessage) { item.Raw = raw }, options...)
}

// APIKeyInfo는 현재 API Key의 권한과 IP 허용 목록을 조회한다.
func (client *Client) APIKeyInfo(
	ctx context.Context,
	options ...trade.RequestOption,
) (APIKeyInfo, error) {
	return readObject(ctx, client, "/api/v1/user/api-key", nil, managementLimit(20),
		func(item *APIKeyInfo, raw json.RawMessage) { item.Raw = raw }, options...)
}

// HFAccountOpened는 HF(trade_hf) 계정 개설 여부를 조회한다.
func (client *Client) HFAccountOpened(
	ctx context.Context,
	options ...trade.RequestOption,
) (bool, error) {
	var opened bool
	if _, err := client.readPrivate(ctx, "/api/v1/hf/accounts/opened", nil, spotLimit(30), &opened, options...); err != nil {
		return false, err
	}
	return opened, nil
}

// AccountDetail은 계정 ID로 단일 Spot 계정 잔고를 조회한다.
func (client *Client) AccountDetail(
	ctx context.Context,
	accountID string,
	options ...trade.RequestOption,
) (AccountDetail, error) {
	if err := validateID("account ID", accountID); err != nil {
		return AccountDetail{}, err
	}
	return readObject(ctx, client, "/api/v1/accounts/"+url.PathEscape(accountID), nil, managementLimit(5),
		func(item *AccountDetail, raw json.RawMessage) { item.Raw = raw }, options...)
}

// Ledgers는 Spot/Margin 계정 원장을 조회한다.
func (client *Client) Ledgers(
	ctx context.Context,
	request LedgersRequest,
	options ...trade.RequestOption,
) (Page[Ledger], error) {
	if err := request.validate(); err != nil {
		return Page[Ledger]{}, err
	}
	return readPage(ctx, client, "/api/v1/accounts/ledgers", request.values(), managementLimit(2),
		func(item *Ledger, raw json.RawMessage) { item.Raw = raw }, options...)
}

// HFLedgers는 trade_hf 계정 원장을 조회한다.
func (client *Client) HFLedgers(
	ctx context.Context,
	request HFLedgersRequest,
	options ...trade.RequestOption,
) ([]HFLedger, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return readList(ctx, client, "/api/v1/hf/accounts/ledgers", request.values(), spotLimit(2),
		func(item *HFLedger, raw json.RawMessage) { item.Raw = raw }, options...)
}

// BaseFee는 계정 등급의 기본 수수료율을 조회한다.
func (client *Client) BaseFee(
	ctx context.Context,
	currencyType CurrencyType,
	options ...trade.RequestOption,
) (FeeRate, error) {
	if currencyType != CurrencyTypeCrypto && currencyType != CurrencyTypeFiat {
		return FeeRate{}, validationError("currency type must be 0 or 1")
	}
	query := url.Values{"currencyType": {strconv.Itoa(int(currencyType))}}
	return readObject(ctx, client, "/api/v1/base-fee", query, spotLimit(3),
		func(item *FeeRate, raw json.RawMessage) { item.Raw = raw }, options...)
}

// TradeFees는 거래쌍별 실제 수수료율을 조회한다. 한 번에 최대 10개 거래쌍이다.
func (client *Client) TradeFees(
	ctx context.Context,
	symbols []string,
	options ...trade.RequestOption,
) ([]FeeRate, error) {
	if len(symbols) == 0 || len(symbols) > 10 {
		return nil, validationError("trade fee symbols must contain 1 to 10 items")
	}
	for _, symbol := range symbols {
		if err := validateSymbol(symbol); err != nil {
			return nil, err
		}
	}
	return readList(ctx, client, "/api/v1/trade-fees", url.Values{"symbols": {joinList(symbols)}}, spotLimit(3),
		func(item *FeeRate, raw json.RawMessage) { item.Raw = raw }, options...)
}

// DepositAddresses는 통화의 입금 주소 목록을 조회한다. 비어 있으면 먼저 주소를 생성해야 한다.
func (client *Client) DepositAddresses(
	ctx context.Context,
	request DepositAddressRequest,
	options ...trade.RequestOption,
) ([]DepositAddress, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return readList(ctx, client, "/api/v3/deposit-addresses", request.values(), managementLimit(5),
		func(item *DepositAddress, raw json.RawMessage) { item.Raw = raw }, options...)
}

// CreateDepositAddress는 입금 주소를 생성한다.
func (client *Client) CreateDepositAddress(
	ctx context.Context,
	request CreateDepositAddressRequest,
	options ...trade.RequestOption,
) (DepositAddress, error) {
	if err := request.validate(); err != nil {
		return DepositAddress{}, err
	}
	var address DepositAddress
	_, data, err := client.mutate(
		ctx, http.MethodPost, "/api/v3/deposit-address/create", nil, request,
		managementLimit(20), credential.PermissionTransfer, &address, options...,
	)
	if err != nil {
		return DepositAddress{}, err
	}
	address.Raw = cloneBytes(data)
	return address, nil
}

// Deposits는 입금 내역을 조회한다.
func (client *Client) Deposits(
	ctx context.Context,
	request DepositsRequest,
	options ...trade.RequestOption,
) (Page[Deposit], error) {
	if err := request.validate(); err != nil {
		return Page[Deposit]{}, err
	}
	return readPage(ctx, client, "/api/v1/deposits", request.values(), managementLimit(5),
		func(item *Deposit, raw json.RawMessage) { item.Raw = raw }, options...)
}

// WithdrawalQuotas는 통화·체인별 출금 한도와 수수료를 조회한다.
func (client *Client) WithdrawalQuotas(
	ctx context.Context,
	request WithdrawalQuotasRequest,
	options ...trade.RequestOption,
) (WithdrawalQuotas, error) {
	if err := request.validate(); err != nil {
		return WithdrawalQuotas{}, err
	}
	return readObject(ctx, client, "/api/v1/withdrawals/quotas", request.values(), managementLimit(20),
		func(item *WithdrawalQuotas, raw json.RawMessage) { item.Raw = raw }, options...)
}

// Withdraw는 출금을 요청한다. 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) Withdraw(
	ctx context.Context,
	request WithdrawRequest,
	options ...trade.RequestOption,
) (WithdrawalReference, error) {
	if err := request.validate(); err != nil {
		return WithdrawalReference{}, err
	}
	var reference WithdrawalReference
	response, data, err := client.mutate(
		ctx, http.MethodPost, "/api/v3/withdrawals", nil, request,
		managementLimit(5), credential.PermissionWithdraw, &reference, options...,
	)
	if err != nil {
		return WithdrawalReference{}, err
	}
	if reference.WithdrawalID == "" {
		return WithdrawalReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errEmptyAcknowledgement,
		)
	}
	reference.Raw = cloneBytes(data)
	return reference, nil
}

// CancelWithdrawal은 처리 전 출금을 취소한다.
func (client *Client) CancelWithdrawal(
	ctx context.Context,
	withdrawalID string,
	options ...trade.RequestOption,
) (Acknowledgement, error) {
	if err := validateID("withdrawal ID", withdrawalID); err != nil {
		return Acknowledgement{}, err
	}
	return client.acknowledge(
		ctx, http.MethodDelete, "/api/v1/withdrawals/"+url.PathEscape(withdrawalID), nil, nil,
		managementLimit(20), credential.PermissionWithdraw, options...,
	)
}

// Withdrawals는 출금 내역을 조회한다.
func (client *Client) Withdrawals(
	ctx context.Context,
	request WithdrawalsRequest,
	options ...trade.RequestOption,
) (Page[Withdrawal], error) {
	if err := request.validate(); err != nil {
		return Page[Withdrawal]{}, err
	}
	return readPage(ctx, client, "/api/v1/withdrawals", request.values(), managementLimit(20),
		func(item *Withdrawal, raw json.RawMessage) { item.Raw = raw }, options...)
}

// WithdrawalByID는 출금 ID로 출금 상세를 조회한다.
func (client *Client) WithdrawalByID(
	ctx context.Context,
	withdrawalID string,
	options ...trade.RequestOption,
) (WithdrawalDetail, error) {
	if err := validateID("withdrawal ID", withdrawalID); err != nil {
		return WithdrawalDetail{}, err
	}
	return readObject(ctx, client, "/api/v1/withdrawals/"+url.PathEscape(withdrawalID), nil, managementLimit(20),
		func(item *WithdrawalDetail, raw json.RawMessage) { item.Raw = raw }, options...)
}

// Transferable은 계정 유형별 이체 가능 수량을 조회한다.
func (client *Client) Transferable(
	ctx context.Context,
	request TransferableRequest,
	options ...trade.RequestOption,
) (Transferable, error) {
	if err := request.validate(); err != nil {
		return Transferable{}, err
	}
	return readObject(ctx, client, "/api/v1/accounts/transferable", request.values(), managementLimit(20),
		func(item *Transferable, raw json.RawMessage) { item.Raw = raw }, options...)
}

// FlexTransfer는 계정·서브계정 간 Flex 이체를 요청한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) FlexTransfer(
	ctx context.Context,
	request FlexTransferRequest,
	options ...trade.RequestOption,
) (TransferReference, error) {
	if err := request.validate(); err != nil {
		return TransferReference{}, err
	}
	var reference TransferReference
	response, data, err := client.mutate(
		ctx, http.MethodPost, "/api/v3/accounts/universal-transfer", nil, request,
		managementLimit(4), credential.PermissionTransfer, &reference, options...,
	)
	if err != nil {
		return TransferReference{}, err
	}
	if reference.OrderID == "" {
		return TransferReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errEmptyAcknowledgement,
		)
	}
	reference.Raw = cloneBytes(data)
	return reference, nil
}

// CreateSubAccount는 서브계정을 생성한다.
func (client *Client) CreateSubAccount(
	ctx context.Context,
	request CreateSubAccountRequest,
	options ...trade.RequestOption,
) (SubAccount, error) {
	if err := request.validate(); err != nil {
		return SubAccount{}, err
	}
	var account SubAccount
	_, data, err := client.mutate(
		ctx, http.MethodPost, "/api/v2/sub/user/created", nil, request,
		managementLimit(15), credential.PermissionTransfer, &account, options...,
	)
	if err != nil {
		return SubAccount{}, err
	}
	account.Raw = cloneBytes(data)
	return account, nil
}

// EnableSubAccountMargin은 서브계정에 마진 권한을 부여한다.
func (client *Client) EnableSubAccountMargin(
	ctx context.Context,
	request SubAccountPermissionRequest,
	options ...trade.RequestOption,
) (Acknowledgement, error) {
	if err := request.validate(); err != nil {
		return Acknowledgement{}, err
	}
	return client.acknowledge(
		ctx, http.MethodPost, "/api/v3/sub/user/margin/enable", nil, request,
		managementLimit(15), credential.PermissionTransfer, options...,
	)
}

// EnableSubAccountFutures는 서브계정에 선물 권한을 부여한다.
func (client *Client) EnableSubAccountFutures(
	ctx context.Context,
	request SubAccountPermissionRequest,
	options ...trade.RequestOption,
) (Acknowledgement, error) {
	if err := request.validate(); err != nil {
		return Acknowledgement{}, err
	}
	return client.acknowledge(
		ctx, http.MethodPost, "/api/v3/sub/user/futures/enable", nil, request,
		managementLimit(15), credential.PermissionTransfer, options...,
	)
}

// SubAccounts는 서브계정 요약 목록을 조회한다. PageSize는 1~100이다.
func (client *Client) SubAccounts(
	ctx context.Context,
	request SubAccountsRequest,
	options ...trade.RequestOption,
) (Page[SubAccountSummary], error) {
	if err := validatePage(request.CurrentPage, request.PageSize, 1, 100); err != nil {
		return Page[SubAccountSummary]{}, err
	}
	return readPage(ctx, client, "/api/v2/sub/user", request.values(), managementLimit(20),
		func(item *SubAccountSummary, raw json.RawMessage) { item.Raw = raw }, options...)
}

// SubAccountBalanceDetail은 서브계정 한 개의 Spot 잔고 상세를 조회한다.
func (client *Client) SubAccountBalanceDetail(
	ctx context.Context,
	request SubAccountBalanceRequest,
	options ...trade.RequestOption,
) (SubAccountBalances, error) {
	if err := request.validate(); err != nil {
		return SubAccountBalances{}, err
	}
	return readObject(ctx, client, "/api/v1/sub-accounts/"+url.PathEscape(request.SubUserID), request.values(),
		managementLimit(15), func(item *SubAccountBalances, raw json.RawMessage) { item.Raw = raw }, options...)
}

// SubAccountSpotBalances는 모든 서브계정의 Spot 잔고 목록을 조회한다. PageSize는 10~100이다.
func (client *Client) SubAccountSpotBalances(
	ctx context.Context,
	request SubAccountsRequest,
	options ...trade.RequestOption,
) (Page[SubAccountBalances], error) {
	if err := validatePage(request.CurrentPage, request.PageSize, 10, 100); err != nil {
		return Page[SubAccountBalances]{}, err
	}
	return readPage(ctx, client, "/api/v2/sub-accounts", request.values(), managementLimit(20),
		func(item *SubAccountBalances, raw json.RawMessage) { item.Raw = raw }, options...)
}

// SubAPIKeys는 서브계정 API Key 목록을 조회한다.
func (client *Client) SubAPIKeys(
	ctx context.Context,
	request SubAPIKeysRequest,
	options ...trade.RequestOption,
) ([]SubAPIKey, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return readList(ctx, client, "/api/v1/sub/api-key", request.values(), managementLimit(20),
		func(item *SubAPIKey, raw json.RawMessage) { item.Raw = raw }, options...)
}

// CreateSubAPIKey는 서브계정 API Key를 생성한다.
func (client *Client) CreateSubAPIKey(
	ctx context.Context,
	request CreateSubAPIKeyRequest,
	options ...trade.RequestOption,
) (CreatedSubAPIKey, error) {
	if err := request.validate(); err != nil {
		return CreatedSubAPIKey{}, err
	}
	var key CreatedSubAPIKey
	_, data, err := client.mutate(
		ctx, http.MethodPost, "/api/v1/sub/api-key", nil, request,
		managementLimit(20), credential.PermissionTransfer, &key, options...,
	)
	if err != nil {
		return CreatedSubAPIKey{}, err
	}
	key.Raw = cloneBytes(data)
	return key, nil
}

// ModifySubAPIKey는 서브계정 API Key의 권한·IP 허용 목록·만료를 수정한다.
func (client *Client) ModifySubAPIKey(
	ctx context.Context,
	request ModifySubAPIKeyRequest,
	options ...trade.RequestOption,
) (ModifiedSubAPIKey, error) {
	if err := request.validate(); err != nil {
		return ModifiedSubAPIKey{}, err
	}
	var key ModifiedSubAPIKey
	_, data, err := client.mutate(
		ctx, http.MethodPost, "/api/v1/sub/api-key/update", nil, request,
		managementLimit(30), credential.PermissionTransfer, &key, options...,
	)
	if err != nil {
		return ModifiedSubAPIKey{}, err
	}
	key.Raw = cloneBytes(data)
	return key, nil
}

// DeleteSubAPIKey는 서브계정 API Key를 삭제한다.
func (client *Client) DeleteSubAPIKey(
	ctx context.Context,
	request DeleteSubAPIKeyRequest,
	options ...trade.RequestOption,
) (DeletedSubAPIKey, error) {
	if err := request.validate(); err != nil {
		return DeletedSubAPIKey{}, err
	}
	query := url.Values{
		"subName": {request.SubName}, "apiKey": {request.APIKey}, "passphrase": {request.Passphrase},
	}
	var key DeletedSubAPIKey
	_, data, err := client.mutate(
		ctx, http.MethodDelete, "/api/v1/sub/api-key", query, nil,
		managementLimit(30), credential.PermissionTransfer, &key, options...,
	)
	if err != nil {
		return DeletedSubAPIKey{}, err
	}
	key.Raw = cloneBytes(data)
	return key, nil
}
