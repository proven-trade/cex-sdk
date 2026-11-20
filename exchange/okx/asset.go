package okx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// AssetAccount는 자금 이동의 출발·도착 계정 유형이다.
type AssetAccount string

const (
	// AssetAccountFunding은 펀딩 계정이다.
	AssetAccountFunding AssetAccount = "6"
	// AssetAccountTrading은 거래 계정이다.
	AssetAccountTrading AssetAccount = "18"
)

func (account AssetAccount) valid() bool {
	return account == AssetAccountFunding || account == AssetAccountTrading
}

// TransferType은 /asset/transfer의 이체 유형이다.
type TransferType string

const (
	// TransferTypeWithinAccount는 같은 계정 안의 이체다.
	TransferTypeWithinAccount TransferType = "0"
	// TransferTypeMasterToSub는 마스터 API Key로 마스터에서 서브계정으로 보내는 이체다.
	TransferTypeMasterToSub TransferType = "1"
	// TransferTypeSubToMasterByMaster는 마스터 API Key로 서브계정에서 마스터로 보내는 이체다.
	TransferTypeSubToMasterByMaster TransferType = "2"
	// TransferTypeSubToMasterBySub는 서브계정 API Key로 마스터로 보내는 이체다.
	TransferTypeSubToMasterBySub TransferType = "3"
	// TransferTypeSubToSub는 서브계정 API Key로 같은 마스터의 다른 서브계정으로 보내는 이체다.
	TransferTypeSubToSub TransferType = "4"
)

func (transferType TransferType) valid() bool {
	switch transferType {
	case "", TransferTypeWithinAccount, TransferTypeMasterToSub, TransferTypeSubToMasterByMaster,
		TransferTypeSubToMasterBySub, TransferTypeSubToSub:
		return true
	default:
		return false
	}
}

// 모든 자산 endpoint는 계정(User ID) 단위로 제한한다.
func assetLimit(requests int, window time.Duration, discriminator string) endpointLimit {
	return accountLimit(requests, window, discriminator)
}

func validateCurrencyList(name string, currencies []string, maximum int) (string, error) {
	return extJoinCSV(name, currencies, maximum)
}

func validateClientID(name, value string) error {
	if value != "" && !clientOrderIDPattern.MatchString(value) {
		return validationError("%s has an invalid format", name)
	}
	return nil
}

// ---- 통화·잔고 ----

// AssetCurrency는 통화와 체인별 입출금 설정이다.
type AssetCurrency struct {
	Currency              string          `json:"ccy"`
	Name                  string          `json:"name"`
	LogoLink              string          `json:"logoLink"`
	Chain                 string          `json:"chain"`
	ContractAddress       string          `json:"ctAddr"`
	CanDeposit            bool            `json:"canDep"`
	CanWithdraw           bool            `json:"canWd"`
	CanInternal           bool            `json:"canInternal"`
	DepositEstOpenTime    string          `json:"depEstOpenTime"`
	WithdrawEstOpenTime   string          `json:"wdEstOpenTime"`
	MinimumDeposit        string          `json:"minDep"`
	MinimumWithdraw       string          `json:"minWd"`
	MinimumInternal       string          `json:"minInternal"`
	MaximumWithdraw       string          `json:"maxWd"`
	WithdrawTickSize      string          `json:"wdTickSz"`
	WithdrawQuota         string          `json:"wdQuota"`
	UsedWithdrawQuota     string          `json:"usedWdQuota"`
	Fee                   string          `json:"fee"`
	BurningFeeRate        string          `json:"burningFeeRate"`
	MainNet               bool            `json:"mainNet"`
	NeedTag               bool            `json:"needTag"`
	MinDepositConfirm     string          `json:"minDepArrivalConfirm"`
	MinWithdrawUnlockConf string          `json:"minWdUnlockConfirm"`
	DepositQuotaFixed     string          `json:"depQuotaFixed"`
	UsedDepositQuotaFixed string          `json:"usedDepQuotaFixed"`
	Raw                   json.RawMessage `json:"-"`
}

// AssetCurrencies는 펀딩 계정 기준 통화·체인 정보를 조회한다. Currencies는 최대 20개다.
func (client *Client) AssetCurrencies(
	ctx context.Context,
	currencies []string,
	options ...trade.RequestOption,
) ([]AssetCurrency, error) {
	joined, err := validateCurrencyList("ccy", currencies, 20)
	if err != nil {
		return nil, err
	}
	values := make(url.Values)
	setIfNotEmpty(values, "ccy", joined)
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/asset/currencies", values, nil,
		assetLimit(6, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *AssetCurrency, raw []byte) { item.Raw = raw }, options...,
	)
}

// AssetBalance는 펀딩 계정 통화별 잔고다.
type AssetBalance struct {
	Currency         string          `json:"ccy"`
	Balance          string          `json:"bal"`
	FrozenBalance    string          `json:"frozenBal"`
	AvailableBalance string          `json:"availBal"`
	Raw              json.RawMessage `json:"-"`
}

// AssetBalances는 펀딩 계정 잔고를 조회한다. Currencies는 최대 20개다.
func (client *Client) AssetBalances(
	ctx context.Context,
	currencies []string,
	options ...trade.RequestOption,
) ([]AssetBalance, error) {
	joined, err := validateCurrencyList("ccy", currencies, 20)
	if err != nil {
		return nil, err
	}
	values := make(url.Values)
	setIfNotEmpty(values, "ccy", joined)
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/asset/balances", values, nil,
		assetLimit(6, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *AssetBalance, raw []byte) { item.Raw = raw }, options...,
	)
}

// NonTradableAsset은 거래 불가 자산의 출금 정보다.
type NonTradableAsset struct {
	Currency        string          `json:"ccy"`
	Name            string          `json:"name"`
	LogoLink        string          `json:"logoLink"`
	Balance         string          `json:"bal"`
	CanWithdraw     bool            `json:"canWd"`
	Chain           string          `json:"chain"`
	MinimumWithdraw string          `json:"minWd"`
	WithdrawAll     bool            `json:"wdAll"`
	Fee             string          `json:"fee"`
	FeeCurrency     string          `json:"feeCcy"`
	BurningFeeRate  string          `json:"burningFeeRate"`
	ContractAddress string          `json:"ctAddr"`
	TickSize        string          `json:"wdTickSz"`
	NeedTag         bool            `json:"needTag"`
	Raw             json.RawMessage `json:"-"`
}

// NonTradableAssets는 거래 불가 자산 목록을 조회한다.
func (client *Client) NonTradableAssets(
	ctx context.Context,
	currencies []string,
	options ...trade.RequestOption,
) ([]NonTradableAsset, error) {
	joined, err := validateCurrencyList("ccy", currencies, 20)
	if err != nil {
		return nil, err
	}
	values := make(url.Values)
	setIfNotEmpty(values, "ccy", joined)
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/asset/non-tradable-assets", values, nil,
		assetLimit(6, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *NonTradableAsset, raw []byte) { item.Raw = raw }, options...,
	)
}

// AssetValuationDetails는 계정별 평가액이다.
type AssetValuationDetails struct {
	Funding string `json:"funding"`
	Trading string `json:"trading"`
	Earn    string `json:"earn"`
}

// AssetValuation은 계정 총자산 평가액이다.
type AssetValuation struct {
	TotalBalance string                `json:"totalBal"`
	Timestamp    string                `json:"ts"`
	Details      AssetValuationDetails `json:"details"`
	Raw          json.RawMessage       `json:"-"`
}

// AssetValuation은 총자산을 지정한 통화(기본 USD)로 평가해 조회한다.
func (client *Client) AssetValuation(
	ctx context.Context,
	currency string,
	options ...trade.RequestOption,
) (AssetValuation, error) {
	if err := validateOptionalText("ccy", currency); err != nil {
		return AssetValuation{}, err
	}
	values := make(url.Values)
	setIfNotEmpty(values, "ccy", currency)
	return extSignedOne(
		ctx, client, http.MethodGet, "/api/v5/asset/asset-valuation", values, nil,
		assetLimit(1, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *AssetValuation, raw []byte) { item.Raw = raw }, options...,
	)
}

// ---- 이체 ----

// AssetTransferRequest는 /asset/transfer 요청이다.
type AssetTransferRequest struct {
	Type             TransferType `json:"type,omitempty"`
	Currency         string       `json:"ccy"`
	Amount           string       `json:"amt"`
	From             AssetAccount `json:"from"`
	To               AssetAccount `json:"to"`
	SubAccount       string       `json:"subAcct,omitempty"`
	LoanTransfer     bool         `json:"loanTrans,omitempty"`
	OmitPositionRisk bool         `json:"-"`
	ClientID         string       `json:"clientId,omitempty"`
}

// assetTransferBody는 omitPosRisk가 문자열 boolean이어야 하는 OKX 규격에 맞춘 JSON 본문이다.
type assetTransferBody struct {
	AssetTransferRequest
	OmitPosRisk string `json:"omitPosRisk,omitempty"`
}

// AssetTransferResult는 이체 접수 결과다.
type AssetTransferResult struct {
	TransferID string          `json:"transId"`
	ClientID   string          `json:"clientId"`
	Currency   string          `json:"ccy"`
	From       string          `json:"from"`
	Amount     string          `json:"amt"`
	To         string          `json:"to"`
	Raw        json.RawMessage `json:"-"`
}

func (request AssetTransferRequest) validate() error {
	if !request.Type.valid() {
		return validationError("unsupported transfer type %q", request.Type)
	}
	if err := validateRequiredText("ccy", request.Currency); err != nil {
		return err
	}
	if err := validatePositiveDecimal("amt", request.Amount); err != nil {
		return err
	}
	if !request.From.valid() || !request.To.valid() {
		return validationError("transfer from and to must be funding (6) or trading (18)")
	}
	if err := validateOptionalText("subAcct", request.SubAccount); err != nil {
		return err
	}
	switch request.Type {
	case TransferTypeMasterToSub, TransferTypeSubToMasterByMaster, TransferTypeSubToSub:
		if request.SubAccount == "" {
			return validationError("subAcct is required for transfer type %s", request.Type)
		}
	}
	return validateClientID("clientId", request.ClientID)
}

// AssetTransfer는 펀딩·거래 계정 또는 마스터·서브계정 사이로 자금을 이체한다.
// 전송 결과가 불명확하면 자동 재시도하지 않으므로 ClientID로 AssetTransferState를 조회해야 한다.
func (client *Client) AssetTransfer(
	ctx context.Context,
	request AssetTransferRequest,
	options ...trade.RequestOption,
) (AssetTransferResult, error) {
	if err := request.validate(); err != nil {
		return AssetTransferResult{}, err
	}
	payload := assetTransferBody{AssetTransferRequest: request}
	if request.OmitPositionRisk {
		payload.OmitPosRisk = "true"
	}
	body, err := encodeBody(payload)
	if err != nil {
		return AssetTransferResult{}, err
	}
	return extSignedOne(
		ctx, client, http.MethodPost, "/api/v5/asset/transfer", nil, body,
		assetLimit(2, time.Second, request.Currency), credential.PermissionTransfer,
		commonexchange.OperationMutation,
		func(item *AssetTransferResult, raw []byte) { item.Raw = raw }, options...,
	)
}

// AssetTransferStateRequest는 이체 상태 조회 조건이다. TransferID 또는 ClientID가 필요하다.
type AssetTransferStateRequest struct {
	TransferID string
	ClientID   string
	Type       TransferType
}

// AssetTransferState는 이체 상태다. State는 success, pending, failed 중 하나다.
type AssetTransferState struct {
	TransferID string          `json:"transId"`
	ClientID   string          `json:"clientId"`
	Currency   string          `json:"ccy"`
	Amount     string          `json:"amt"`
	Type       TransferType    `json:"type"`
	From       string          `json:"from"`
	To         string          `json:"to"`
	SubAccount string          `json:"subAcct"`
	State      string          `json:"state"`
	Raw        json.RawMessage `json:"-"`
}

func (request AssetTransferStateRequest) validate() error {
	if err := validateOptionalText("transId", request.TransferID); err != nil {
		return err
	}
	if err := validateOptionalText("clientId", request.ClientID); err != nil {
		return err
	}
	if request.TransferID == "" && request.ClientID == "" {
		return validationError("transId or clientId is required")
	}
	if !request.Type.valid() {
		return validationError("unsupported transfer type %q", request.Type)
	}
	return nil
}

// AssetTransferStates는 이체 ID 또는 client ID로 이체 상태를 조회한다.
func (client *Client) AssetTransferStates(
	ctx context.Context,
	request AssetTransferStateRequest,
	options ...trade.RequestOption,
) (AssetTransferState, error) {
	if err := request.validate(); err != nil {
		return AssetTransferState{}, err
	}
	values := make(url.Values)
	setIfNotEmpty(values, "transId", request.TransferID)
	setIfNotEmpty(values, "clientId", request.ClientID)
	setIfNotEmpty(values, "type", string(request.Type))
	return extSignedOne(
		ctx, client, http.MethodGet, "/api/v5/asset/transfer-state", values, nil,
		assetLimit(10, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *AssetTransferState, raw []byte) { item.Raw = raw }, options...,
	)
}

// ---- 펀딩 원장 ----

// AssetBillsRequest는 펀딩 계정 원장 조회 조건이다. After/Before는 ts 또는 History의 PagingType=2일 때 billId다.
type AssetBillsRequest struct {
	Currency       string
	Type           string
	ThirdPartyType string
	ClientID       string
	After          string
	Before         string
	Limit          int
	// PagingType은 AssetBillsHistory에서만 사용한다. 1은 ts, 2는 billId 기준이다.
	PagingType string
}

// AssetBill은 펀딩 계정 원장 한 건이다.
type AssetBill struct {
	BillID        string          `json:"billId"`
	Currency      string          `json:"ccy"`
	ClientID      string          `json:"clientId"`
	BalanceChange string          `json:"balChg"`
	Balance       string          `json:"bal"`
	Type          string          `json:"type"`
	Notes         string          `json:"notes"`
	Timestamp     string          `json:"ts"`
	Raw           json.RawMessage `json:"-"`
}

func (request AssetBillsRequest) validate(history bool) error {
	for name, value := range map[string]string{
		"ccy": request.Currency, "type": request.Type, "thirdPartyType": request.ThirdPartyType,
		"after": request.After, "before": request.Before,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return err
		}
	}
	if err := validateClientID("clientId", request.ClientID); err != nil {
		return err
	}
	if request.PagingType != "" {
		if !history {
			return validationError("pagingType is only supported by AssetBillsHistory")
		}
		if request.PagingType != "1" && request.PagingType != "2" {
			return validationError("pagingType must be 1 or 2")
		}
	}
	return extValidateLimit("asset bill", request.Limit, 100)
}

func (request AssetBillsRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "ccy", request.Currency)
	setIfNotEmpty(values, "type", request.Type)
	setIfNotEmpty(values, "thirdPartyType", request.ThirdPartyType)
	setIfNotEmpty(values, "clientId", request.ClientID)
	setIfNotEmpty(values, "after", request.After)
	setIfNotEmpty(values, "before", request.Before)
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "pagingType", request.PagingType)
	return values
}

// AssetBills는 최근 펀딩 계정 원장을 조회한다.
func (client *Client) AssetBills(
	ctx context.Context,
	request AssetBillsRequest,
	options ...trade.RequestOption,
) ([]AssetBill, error) {
	if err := request.validate(false); err != nil {
		return nil, err
	}
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/asset/bills", request.values(), nil,
		assetLimit(6, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *AssetBill, raw []byte) { item.Raw = raw }, options...,
	)
}

// AssetBillsHistory는 2021년 2월 이후 펀딩 계정 원장을 조회한다. 데이터는 30초 간격으로 갱신된다.
func (client *Client) AssetBillsHistory(
	ctx context.Context,
	request AssetBillsRequest,
	options ...trade.RequestOption,
) ([]AssetBill, error) {
	if err := request.validate(true); err != nil {
		return nil, err
	}
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/asset/bills-history", request.values(), nil,
		assetLimit(1, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *AssetBill, raw []byte) { item.Raw = raw }, options...,
	)
}

// ---- 입금 ----

// DepositAddress는 통화·체인별 입금 주소다.
type DepositAddress struct {
	Address         string          `json:"addr"`
	Tag             string          `json:"tag"`
	Memo            string          `json:"memo"`
	PaymentID       string          `json:"pmtId"`
	Currency        string          `json:"ccy"`
	Chain           string          `json:"chain"`
	To              string          `json:"to"`
	VerifiedName    string          `json:"verifiedName"`
	Selected        bool            `json:"selected"`
	ContractAddress string          `json:"ctAddr"`
	Raw             json.RawMessage `json:"-"`
}

// DepositAddresses는 통화의 입금 주소 목록을 조회한다. 주소 생성 endpoint는 아니다.
func (client *Client) DepositAddresses(
	ctx context.Context,
	currency string,
	options ...trade.RequestOption,
) ([]DepositAddress, error) {
	if err := validateRequiredText("ccy", currency); err != nil {
		return nil, err
	}
	values := make(url.Values)
	values.Set("ccy", currency)
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/asset/deposit-address", values, nil,
		assetLimit(6, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *DepositAddress, raw []byte) { item.Raw = raw }, options...,
	)
}

// DepositHistoryRequest는 입금 내역 조회 조건이다.
type DepositHistoryRequest struct {
	Currency         string
	DepositID        string
	FromWithdrawalID string
	TxID             string
	Type             string
	State            string
	After            *time.Time
	Before           *time.Time
	Limit            int
}

// DepositRecord는 입금 내역 한 건이다. State 0 대기, 1 입금 반영, 2 입금 완료다.
type DepositRecord struct {
	Currency         string          `json:"ccy"`
	Chain            string          `json:"chain"`
	Amount           string          `json:"amt"`
	From             string          `json:"from"`
	To               string          `json:"to"`
	TxID             string          `json:"txId"`
	Timestamp        string          `json:"ts"`
	State            string          `json:"state"`
	DepositID        string          `json:"depId"`
	FromWithdrawalID string          `json:"fromWdId"`
	ActualConfirms   string          `json:"actualDepBlkConfirm"`
	Raw              json.RawMessage `json:"-"`
}

func (request DepositHistoryRequest) validate() error {
	for name, value := range map[string]string{
		"ccy": request.Currency, "depId": request.DepositID, "fromWdId": request.FromWithdrawalID,
		"txId": request.TxID, "type": request.Type, "state": request.State,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return err
		}
	}
	if err := extValidatePageTimes("deposit history", request.After, request.Before); err != nil {
		return err
	}
	return extValidateLimit("deposit history", request.Limit, 100)
}

func (request DepositHistoryRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "ccy", request.Currency)
	setIfNotEmpty(values, "depId", request.DepositID)
	setIfNotEmpty(values, "fromWdId", request.FromWithdrawalID)
	setIfNotEmpty(values, "txId", request.TxID)
	setIfNotEmpty(values, "type", request.Type)
	setIfNotEmpty(values, "state", request.State)
	setTime(values, "after", request.After)
	setTime(values, "before", request.Before)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

// DepositHistory는 입금 내역을 조회한다.
func (client *Client) DepositHistory(
	ctx context.Context,
	request DepositHistoryRequest,
	options ...trade.RequestOption,
) ([]DepositRecord, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/asset/deposit-history", request.values(), nil,
		assetLimit(6, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *DepositRecord, raw []byte) { item.Raw = raw }, options...,
	)
}

// ---- 출금 ----

// WithdrawalDestination은 출금 방식이다.
type WithdrawalDestination string

const (
	// WithdrawalDestinationInternal은 OKX 내부 이체다.
	WithdrawalDestinationInternal WithdrawalDestination = "3"
	// WithdrawalDestinationOnChain은 온체인 출금이다.
	WithdrawalDestinationOnChain WithdrawalDestination = "4"
)

// ReceiverInfo는 일부 규제 지역 사용자의 온체인 출금에 필요한 수취인 정보다.
type ReceiverInfo struct {
	WalletType    string `json:"walletType"`
	ExchangeID    string `json:"exchId,omitempty"`
	FirstName     string `json:"rcvrFirstName,omitempty"`
	LastName      string `json:"rcvrLastName,omitempty"`
	Country       string `json:"rcvrCountry,omitempty"`
	CountrySubDiv string `json:"rcvrCountrySubDivision,omitempty"`
	TownName      string `json:"rcvrTownName,omitempty"`
	StreetName    string `json:"rcvrStreetName,omitempty"`
}

// WithdrawalRequest는 /asset/withdrawal 요청이다. 수수료는 Amount에 포함되지 않는다.
type WithdrawalRequest struct {
	Currency      string                `json:"ccy"`
	Amount        string                `json:"amt"`
	Destination   WithdrawalDestination `json:"dest"`
	ToAddress     string                `json:"toAddr"`
	ToAddressType string                `json:"toAddrType,omitempty"`
	Chain         string                `json:"chain,omitempty"`
	AreaCode      string                `json:"areaCode,omitempty"`
	Receiver      *ReceiverInfo         `json:"rcvrInfo,omitempty"`
	ClientID      string                `json:"clientId,omitempty"`
}

// WithdrawalResult는 출금 접수 결과다.
type WithdrawalResult struct {
	Currency string          `json:"ccy"`
	Chain    string          `json:"chain"`
	Amount   string          `json:"amt"`
	Withdraw string          `json:"wdId"`
	ClientID string          `json:"clientId"`
	Raw      json.RawMessage `json:"-"`
}

func (request WithdrawalRequest) validate() error {
	if err := validateRequiredText("ccy", request.Currency); err != nil {
		return err
	}
	if err := validatePositiveDecimal("amt", request.Amount); err != nil {
		return err
	}
	if request.Destination != WithdrawalDestinationInternal && request.Destination != WithdrawalDestinationOnChain {
		return validationError("withdrawal destination must be 3 (internal) or 4 (on-chain)")
	}
	if err := validateRequiredText("toAddr", request.ToAddress); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"toAddrType": request.ToAddressType, "chain": request.Chain, "areaCode": request.AreaCode,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return err
		}
	}
	if request.Receiver != nil {
		if request.Receiver.WalletType != "exchange" && request.Receiver.WalletType != "private" {
			return validationError("rcvrInfo walletType must be exchange or private")
		}
	}
	return validateClientID("clientId", request.ClientID)
}

// Withdraw는 온체인 또는 내부 출금을 요청한다. 자금이 OKX 밖으로 나가므로 PermissionWithdraw가 필요하다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 ClientID로 WithdrawalHistory를 조회해야 한다.
func (client *Client) Withdraw(
	ctx context.Context,
	request WithdrawalRequest,
	options ...trade.RequestOption,
) (WithdrawalResult, error) {
	if err := request.validate(); err != nil {
		return WithdrawalResult{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return WithdrawalResult{}, err
	}
	return extSignedOne(
		ctx, client, http.MethodPost, "/api/v5/asset/withdrawal", nil, body,
		assetLimit(6, time.Second, ""), credential.PermissionWithdraw, commonexchange.OperationMutation,
		func(item *WithdrawalResult, raw []byte) { item.Raw = raw }, options...,
	)
}

// WithdrawalCancelResult는 출금 취소 접수 결과다.
type WithdrawalCancelResult struct {
	WithdrawalID string          `json:"wdId"`
	Raw          json.RawMessage `json:"-"`
}

// CancelWithdrawal은 취소 가능한 일반 출금 요청을 취소한다.
func (client *Client) CancelWithdrawal(
	ctx context.Context,
	withdrawalID string,
	options ...trade.RequestOption,
) (WithdrawalCancelResult, error) {
	if err := validateRequiredText("wdId", withdrawalID); err != nil {
		return WithdrawalCancelResult{}, err
	}
	body, err := encodeBody(struct {
		WithdrawalID string `json:"wdId"`
	}{WithdrawalID: withdrawalID})
	if err != nil {
		return WithdrawalCancelResult{}, err
	}
	return extSignedOne(
		ctx, client, http.MethodPost, "/api/v5/asset/cancel-withdrawal", nil, body,
		assetLimit(6, time.Second, ""), credential.PermissionWithdraw, commonexchange.OperationMutation,
		func(item *WithdrawalCancelResult, raw []byte) { item.Raw = raw }, options...,
	)
}

// WithdrawalHistoryRequest는 출금 내역 조회 조건이다.
type WithdrawalHistoryRequest struct {
	Currency     string
	WithdrawalID string
	ClientID     string
	TxID         string
	Type         string
	State        string
	After        *time.Time
	Before       *time.Time
	Limit        int
}

// WithdrawalRecord는 출금 내역 한 건이다.
type WithdrawalRecord struct {
	Currency     string          `json:"ccy"`
	Chain        string          `json:"chain"`
	Amount       string          `json:"amt"`
	Timestamp    string          `json:"ts"`
	From         string          `json:"from"`
	To           string          `json:"to"`
	ToAddrType   string          `json:"toAddrType"`
	Tag          string          `json:"tag"`
	PaymentID    string          `json:"pmtId"`
	Memo         string          `json:"memo"`
	TxID         string          `json:"txId"`
	Fee          string          `json:"fee"`
	FeeCurrency  string          `json:"feeCcy"`
	State        string          `json:"state"`
	WithdrawalID string          `json:"wdId"`
	ClientID     string          `json:"clientId"`
	Note         string          `json:"note"`
	Raw          json.RawMessage `json:"-"`
}

func (request WithdrawalHistoryRequest) validate() error {
	for name, value := range map[string]string{
		"ccy": request.Currency, "wdId": request.WithdrawalID, "txId": request.TxID,
		"type": request.Type, "state": request.State,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return err
		}
	}
	if err := validateClientID("clientId", request.ClientID); err != nil {
		return err
	}
	if err := extValidatePageTimes("withdrawal history", request.After, request.Before); err != nil {
		return err
	}
	return extValidateLimit("withdrawal history", request.Limit, 100)
}

func (request WithdrawalHistoryRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "ccy", request.Currency)
	setIfNotEmpty(values, "wdId", request.WithdrawalID)
	setIfNotEmpty(values, "clientId", request.ClientID)
	setIfNotEmpty(values, "txId", request.TxID)
	setIfNotEmpty(values, "type", request.Type)
	setIfNotEmpty(values, "state", request.State)
	setTime(values, "after", request.After)
	setTime(values, "before", request.Before)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

// WithdrawalHistory는 출금 내역을 조회한다.
func (client *Client) WithdrawalHistory(
	ctx context.Context,
	request WithdrawalHistoryRequest,
	options ...trade.RequestOption,
) ([]WithdrawalRecord, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/asset/withdrawal-history", request.values(), nil,
		assetLimit(6, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *WithdrawalRecord, raw []byte) { item.Raw = raw }, options...,
	)
}

// DepositWithdrawStatusRequest는 입출금 진행 상태 조회 조건이다.
// 출금은 WithdrawalID만, 입금은 TxID와 Currency, ToAddress, Chain을 지정한다.
type DepositWithdrawStatusRequest struct {
	WithdrawalID string
	TxID         string
	Currency     string
	ToAddress    string
	Chain        string
}

// DepositWithdrawStatus는 입출금 상세 진행 상태다.
type DepositWithdrawStatus struct {
	EstimatedCompleteTime string          `json:"estCompleteTime"`
	State                 string          `json:"state"`
	TxID                  string          `json:"txId"`
	WithdrawalID          string          `json:"wdId"`
	Raw                   json.RawMessage `json:"-"`
}

func (request DepositWithdrawStatusRequest) validate() error {
	for name, value := range map[string]string{
		"wdId": request.WithdrawalID, "txId": request.TxID, "ccy": request.Currency,
		"to": request.ToAddress, "chain": request.Chain,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return err
		}
	}
	if (request.WithdrawalID == "") == (request.TxID == "") {
		return validationError("exactly one of wdId and txId is required")
	}
	if request.TxID != "" && (request.Currency == "" || request.ToAddress == "" || request.Chain == "") {
		return validationError("deposit status requires ccy, to, and chain with txId")
	}
	return nil
}

// DepositWithdrawStatuses는 입출금 진행 상태를 조회한다.
func (client *Client) DepositWithdrawStatuses(
	ctx context.Context,
	request DepositWithdrawStatusRequest,
	options ...trade.RequestOption,
) ([]DepositWithdrawStatus, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	values := make(url.Values)
	setIfNotEmpty(values, "wdId", request.WithdrawalID)
	setIfNotEmpty(values, "txId", request.TxID)
	setIfNotEmpty(values, "ccy", request.Currency)
	setIfNotEmpty(values, "to", request.ToAddress)
	setIfNotEmpty(values, "chain", request.Chain)
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/asset/deposit-withdraw-status", values, nil,
		assetLimit(1, 2*time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *DepositWithdrawStatus, raw []byte) { item.Raw = raw }, options...,
	)
}

// ---- 견적 기반 코인 전환 ----

// ConvertCurrency는 전환 가능한 통화다.
type ConvertCurrency struct {
	Currency string          `json:"ccy"`
	Raw      json.RawMessage `json:"-"`
}

// ConvertCurrencies는 견적 전환이 가능한 통화를 조회한다.
func (client *Client) ConvertCurrencies(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]ConvertCurrency, error) {
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/asset/convert/currencies", nil, nil,
		assetLimit(6, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *ConvertCurrency, raw []byte) { item.Raw = raw }, options...,
	)
}

// ConvertCurrencyPair는 전환 통화쌍과 수량 한도다.
type ConvertCurrencyPair struct {
	InstrumentID  string          `json:"instId"`
	BaseCurrency  string          `json:"baseCcy"`
	BaseMaximum   string          `json:"baseCcyMax"`
	BaseMinimum   string          `json:"baseCcyMin"`
	QuoteCurrency string          `json:"quoteCcy"`
	QuoteMaximum  string          `json:"quoteCcyMax"`
	QuoteMinimum  string          `json:"quoteCcyMin"`
	Raw           json.RawMessage `json:"-"`
}

// ConvertCurrencyPairs는 from/to 통화의 전환 통화쌍 정보를 조회한다.
func (client *Client) ConvertCurrencyPairs(
	ctx context.Context,
	fromCurrency, toCurrency string,
	options ...trade.RequestOption,
) (ConvertCurrencyPair, error) {
	if err := validateRequiredText("fromCcy", fromCurrency); err != nil {
		return ConvertCurrencyPair{}, err
	}
	if err := validateRequiredText("toCcy", toCurrency); err != nil {
		return ConvertCurrencyPair{}, err
	}
	values := make(url.Values)
	values.Set("fromCcy", fromCurrency)
	values.Set("toCcy", toCurrency)
	return extSignedOne(
		ctx, client, http.MethodGet, "/api/v5/asset/convert/currency-pair", values, nil,
		assetLimit(6, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *ConvertCurrencyPair, raw []byte) { item.Raw = raw }, options...,
	)
}

// ConvertQuoteRequest는 전환 견적 요청이다. RFQSize 통화는 RFQSizeCurrency로 지정한다.
type ConvertQuoteRequest struct {
	BaseCurrency    string `json:"baseCcy"`
	QuoteCurrency   string `json:"quoteCcy"`
	Side            Side   `json:"side"`
	RFQSize         string `json:"rfqSz"`
	RFQSizeCurrency string `json:"rfqSzCcy"`
	ClientQuoteID   string `json:"clQReqId,omitempty"`
	Tag             string `json:"tag,omitempty"`
	ConvertMode     string `json:"convertMode,omitempty"`
}

// ConvertQuote는 전환 견적이다. QuoteID는 TTLMillis 안에만 유효하다.
type ConvertQuote struct {
	QuoteTime       string          `json:"quoteTime"`
	TTLMillis       string          `json:"ttlMs"`
	ClientQuoteID   string          `json:"clQReqId"`
	QuoteID         string          `json:"quoteId"`
	BaseCurrency    string          `json:"baseCcy"`
	QuoteCurrency   string          `json:"quoteCcy"`
	Side            Side            `json:"side"`
	OriginalRFQSize string          `json:"origRfqSz"`
	RFQSize         string          `json:"rfqSz"`
	RFQSizeCurrency string          `json:"rfqSzCcy"`
	ConvertPrice    string          `json:"cnvtPx"`
	BaseSize        string          `json:"baseSz"`
	QuoteSize       string          `json:"quoteSz"`
	Raw             json.RawMessage `json:"-"`
}

func validateConvertSide(side Side) error {
	if side != SideBuy && side != SideSell {
		return validationError("side must be buy or sell")
	}
	return nil
}

func (request ConvertQuoteRequest) validate() error {
	for name, value := range map[string]string{
		"baseCcy": request.BaseCurrency, "quoteCcy": request.QuoteCurrency, "rfqSzCcy": request.RFQSizeCurrency,
	} {
		if err := validateRequiredText(name, value); err != nil {
			return err
		}
	}
	if err := validateConvertSide(request.Side); err != nil {
		return err
	}
	if err := validatePositiveDecimal("rfqSz", request.RFQSize); err != nil {
		return err
	}
	if err := validateClientID("clQReqId", request.ClientQuoteID); err != nil {
		return err
	}
	if err := validateOptionalText("tag", request.Tag); err != nil {
		return err
	}
	return validateOptionalText("convertMode", request.ConvertMode)
}

// ConvertEstimateQuote는 전환 견적을 요청한다. 견적은 거래를 실행하지 않으므로 별도로 ConvertTrade를 호출해야 한다.
func (client *Client) ConvertEstimateQuote(
	ctx context.Context,
	request ConvertQuoteRequest,
	options ...trade.RequestOption,
) (ConvertQuote, error) {
	if err := request.validate(); err != nil {
		return ConvertQuote{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return ConvertQuote{}, err
	}
	return extSignedOne(
		ctx, client, http.MethodPost, "/api/v5/asset/convert/estimate-quote", nil, body,
		assetLimit(1, 5*time.Second, request.BaseCurrency+"-"+request.QuoteCurrency),
		credential.PermissionTransfer, commonexchange.OperationMutation,
		func(item *ConvertQuote, raw []byte) { item.Raw = raw }, options...,
	)
}

// ConvertTradeRequest는 견적으로 전환을 체결하는 요청이다. Size는 견적 수량을 넘을 수 없다.
type ConvertTradeRequest struct {
	QuoteID       string `json:"quoteId"`
	BaseCurrency  string `json:"baseCcy"`
	QuoteCurrency string `json:"quoteCcy"`
	Side          Side   `json:"side"`
	Size          string `json:"sz"`
	SizeCurrency  string `json:"szCcy"`
	ClientTradeID string `json:"clTReqId,omitempty"`
	Tag           string `json:"tag,omitempty"`
	ConvertMode   string `json:"convertMode,omitempty"`
}

// ConvertTradeResult는 전환 체결 결과다. State fullyFilled는 성공, rejected는 실패다.
type ConvertTradeResult struct {
	TradeID       string          `json:"tradeId"`
	QuoteID       string          `json:"quoteId"`
	ClientTradeID string          `json:"clTReqId"`
	State         string          `json:"state"`
	InstrumentID  string          `json:"instId"`
	BaseCurrency  string          `json:"baseCcy"`
	QuoteCurrency string          `json:"quoteCcy"`
	Side          Side            `json:"side"`
	FillPrice     string          `json:"fillPx"`
	FillBaseSize  string          `json:"fillBaseSz"`
	FillQuoteSize string          `json:"fillQuoteSz"`
	Timestamp     string          `json:"ts"`
	Raw           json.RawMessage `json:"-"`
}

func (request ConvertTradeRequest) validate() error {
	for name, value := range map[string]string{
		"quoteId": request.QuoteID, "baseCcy": request.BaseCurrency,
		"quoteCcy": request.QuoteCurrency, "szCcy": request.SizeCurrency,
	} {
		if err := validateRequiredText(name, value); err != nil {
			return err
		}
	}
	if err := validateConvertSide(request.Side); err != nil {
		return err
	}
	if err := validatePositiveDecimal("sz", request.Size); err != nil {
		return err
	}
	if err := validateClientID("clTReqId", request.ClientTradeID); err != nil {
		return err
	}
	if err := validateOptionalText("tag", request.Tag); err != nil {
		return err
	}
	return validateOptionalText("convertMode", request.ConvertMode)
}

// ConvertTrade는 견적으로 코인 전환을 체결한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 ClientTradeID로 ConvertHistory를 조회해야 한다.
func (client *Client) ConvertTrade(
	ctx context.Context,
	request ConvertTradeRequest,
	options ...trade.RequestOption,
) (ConvertTradeResult, error) {
	if err := request.validate(); err != nil {
		return ConvertTradeResult{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return ConvertTradeResult{}, err
	}
	return extSignedOne(
		ctx, client, http.MethodPost, "/api/v5/asset/convert/trade", nil, body,
		assetLimit(10, time.Second, ""), credential.PermissionTransfer, commonexchange.OperationMutation,
		func(item *ConvertTradeResult, raw []byte) { item.Raw = raw }, options...,
	)
}

// ConvertHistoryRequest는 전환 내역 조회 조건이다.
type ConvertHistoryRequest struct {
	ClientTradeID string
	After         *time.Time
	Before        *time.Time
	Limit         int
	Tag           string
}

// ConvertHistory는 코인 전환 내역을 조회한다.
func (client *Client) ConvertHistory(
	ctx context.Context,
	request ConvertHistoryRequest,
	options ...trade.RequestOption,
) ([]ConvertTradeResult, error) {
	if err := validateClientID("clTReqId", request.ClientTradeID); err != nil {
		return nil, err
	}
	if err := validateOptionalText("tag", request.Tag); err != nil {
		return nil, err
	}
	if err := extValidatePageTimes("convert history", request.After, request.Before); err != nil {
		return nil, err
	}
	if err := extValidateLimit("convert history", request.Limit, 100); err != nil {
		return nil, err
	}
	values := make(url.Values)
	setIfNotEmpty(values, "clTReqId", request.ClientTradeID)
	setIfNotEmpty(values, "tag", request.Tag)
	setTime(values, "after", request.After)
	setTime(values, "before", request.Before)
	setPositiveInt(values, "limit", request.Limit)
	return extSignedList(
		ctx, client, http.MethodGet, "/api/v5/asset/convert/history", values, nil,
		assetLimit(6, time.Second, ""), credential.PermissionRead, commonexchange.OperationRead,
		func(item *ConvertTradeResult, raw []byte) { item.Raw = raw }, options...,
	)
}
