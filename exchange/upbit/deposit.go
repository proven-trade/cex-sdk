package upbit

import (
	"context"
	"encoding/json"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// DepositState는 입금 처리 상태다.
// DepositStateTravelRuleSuspected는 트래블룰 추가 인증을 기다리는 상태이며
// VerifyTravelRuleByUUID 또는 VerifyTravelRuleByTxID로 계정주를 확인하면 입금 반영이 재개된다.
type DepositState string

const (
	DepositStateProcessing          DepositState = "PROCESSING"
	DepositStateAccepted            DepositState = "ACCEPTED"
	DepositStateCancelled           DepositState = "CANCELLED"
	DepositStateRejected            DepositState = "REJECTED"
	DepositStateTravelRuleSuspected DepositState = "TRAVEL_RULE_SUSPECTED"
	DepositStateRefunding           DepositState = "REFUNDING"
	DepositStateRefunded            DepositState = "REFUNDED"
)

// DepositTransactionType은 입금 유형이다.
type DepositTransactionType string

const (
	// DepositTransactionDefault는 일반 입금이다.
	DepositTransactionDefault DepositTransactionType = "default"
	// DepositTransactionInternal은 바로 입금(업비트 계정 간 입금)이다.
	DepositTransactionInternal DepositTransactionType = "internal"
)

// DepositChanceRequest는 입금 가능 정보를 조회할 통화와 네트워크다.
type DepositChanceRequest struct {
	Currency string
	// NetType은 입금 네트워크 식별자이며 필수다. 예: BTC, ETH, TRX
	NetType string
}

// DepositChance는 통화와 네트워크의 디지털 자산 입금 가능 정보다.
// IsDepositPossible이 false이면 DepositImpossibleReason에 입금할 수 없는 이유가 담긴다.
// 서비스 상태를 실시간으로 반영하지 않고 수 분 늦게 반영될 수 있으므로 거래 전략이 아닌 참고용으로만 써야 한다.
type DepositChance struct {
	Currency                    string          `json:"currency"`
	NetType                     string          `json:"net_type"`
	IsDepositPossible           bool            `json:"is_deposit_possible"`
	DepositImpossibleReason     string          `json:"deposit_impossible_reason"`
	MinimumDepositAmount        Decimal         `json:"minimum_deposit_amount"`
	MinimumDepositConfirmations int             `json:"minimum_deposit_confirmations"`
	DecimalPrecision            int             `json:"decimal_precision"`
	Raw                         json.RawMessage `json:"-"`
}

// GenerateDepositAddressRequest는 입금 주소를 생성할 통화와 네트워크다.
type GenerateDepositAddressRequest struct {
	Currency string
	// NetType은 입금 네트워크 식별자이며 필수다.
	NetType string
}

// DepositAddressRequest는 입금 주소를 조회할 통화와 네트워크다.
type DepositAddressRequest struct {
	Currency string
	// NetType은 입금 네트워크 식별자이며 필수다.
	NetType string
}

// DepositAddress는 계정에 생성된 디지털 자산 입금 주소 한 건이다.
// SecondaryAddress는 Destination Tag, Memo, Message처럼 2차 주소가 필요한 자산에서만 채워진다.
// 주소 생성이 끝나기 전에 조회하면 deposit_address가 null일 수 있으며 이때 Address는 비어 있다.
type DepositAddress struct {
	Currency         string          `json:"currency"`
	NetType          string          `json:"net_type"`
	Address          string          `json:"deposit_address"`
	SecondaryAddress string          `json:"secondary_address"`
	Raw              json.RawMessage `json:"-"`
}

// GenerateDepositAddressResult는 입금 주소 생성 요청의 응답이다.
// 주소 생성은 비동기라서 완료 전에는 접수 결과(Success, Message)만 반환하고 Address는 비어 있다.
// 이미 생성된 주소가 있으면 통화당 최초 1회 생성된 주소 정보를 그대로 반환한다.
type GenerateDepositAddressResult struct {
	Currency         string          `json:"currency"`
	NetType          string          `json:"net_type"`
	Address          string          `json:"deposit_address"`
	SecondaryAddress string          `json:"secondary_address"`
	Success          bool            `json:"success"`
	Message          string          `json:"message"`
	Raw              json.RawMessage `json:"-"`
}

// Pending은 주소를 아직 생성 중이어서 일정 시간 뒤 DepositAddress로 다시 조회해야 하는지 반환한다.
func (result GenerateDepositAddressResult) Pending() bool {
	return result.Success && result.Address == ""
}

// KRWDepositRequest는 원화 입금 요청이다. 입출금 계좌에서 지정한 금액만큼 입금한다.
type KRWDepositRequest struct {
	Amount        string
	TwoFactorType TwoFactorType
}

// Deposit은 디지털 자산 또는 원화 입금 한 건이다.
// 원화 입금은 NetType이 비어 있고, 완료 전에는 DoneAt도 비어 있다.
type Deposit struct {
	Type            string                 `json:"type"`
	UUID            string                 `json:"uuid"`
	Currency        string                 `json:"currency"`
	NetType         string                 `json:"net_type"`
	TxID            string                 `json:"txid"`
	State           DepositState           `json:"state"`
	CreatedAt       string                 `json:"created_at"`
	DoneAt          string                 `json:"done_at"`
	Amount          Decimal                `json:"amount"`
	Fee             Decimal                `json:"fee"`
	TransactionType DepositTransactionType `json:"transaction_type"`
	Raw             json.RawMessage        `json:"-"`
}

// DepositInfoRequest는 입금 한 건을 찾는 조건이다. 모두 비우면 최신 입금을 반환한다.
type DepositInfoRequest struct {
	UUID     string
	TxID     string
	Currency string
}

// DepositsRequest는 입금 목록 조회 조건이다.
type DepositsRequest struct {
	Currency string
	State    DepositState
	UUIDs    []string
	TxIDs    []string
	// Limit을 비우면 100이며 최대 100이다.
	Limit int
	// Page를 비우면 1이다.
	Page int
	// OrderBy는 asc 또는 desc이며 비우면 desc다.
	OrderBy string
	// From과 To는 응답의 uuid를 쓰는 페이지 이동 커서다.
	From string
	To   string
}

// TravelRuleVASP는 계정주 확인 서비스를 지원하는 거래소(VASP) 한 곳이다.
// Depositable은 해당 거래소에서 업비트로의 입금을 반영할 수 있는지, Withdrawable은 해당 거래소로 출금할 수 있는지 나타낸다.
type TravelRuleVASP struct {
	VASPName     string          `json:"vasp_name"`
	VASPUUID     string          `json:"vasp_uuid"`
	Depositable  bool            `json:"depositable"`
	Withdrawable bool            `json:"withdrawable"`
	Raw          json.RawMessage `json:"-"`
}

// TravelRuleUUIDRequest는 입금 UUID로 트래블룰 검증(입금 계정주 확인)을 요청하는 조건이다.
type TravelRuleUUIDRequest struct {
	DepositUUID string
	// VASPUUID는 자산을 출금한 상대 거래소의 UUID이며 TravelRuleVASPs로 조회한다.
	VASPUUID string
}

// TravelRuleTxIDRequest는 입금 TxID로 트래블룰 검증(입금 계정주 확인)을 요청하는 조건이다.
type TravelRuleTxIDRequest struct {
	// VASPUUID는 자산을 출금한 상대 거래소의 UUID이며 TravelRuleVASPs로 조회한다.
	VASPUUID string
	TxID     string
	Currency string
	// NetType은 입금 네트워크 식별자이며 필수다.
	NetType string
}

// TravelRuleVerification은 트래블룰 검증 요청 결과다. VerificationResult는 검증을 통과하면 verified다.
type TravelRuleVerification struct {
	DepositUUID        string          `json:"deposit_uuid"`
	DepositState       DepositState    `json:"deposit_state"`
	VerificationResult string          `json:"verification_result"`
	Raw                json.RawMessage `json:"-"`
}

// DepositChance는 통화와 네트워크의 입금 가능 정보를 조회한다. 업비트 API Key에 입금조회 권한이 필요하다.
func (client *Client) DepositChance(
	ctx context.Context,
	request DepositChanceRequest,
	options ...trade.RequestOption,
) (DepositChance, error) {
	if err := request.validate(); err != nil {
		return DepositChance{}, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/deposits/chance/coin", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return DepositChance{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead, func(item *DepositChance, raw []byte) {
		item.Raw = raw
	})
}

// GenerateDepositAddress는 입금 주소 생성을 요청한다. 업비트 API Key에 입금하기 권한이 필요하다.
// 주소 생성은 비동기로 진행되므로 결과의 Pending이 true이면 잠시 뒤 DepositAddress로 다시 조회해야 한다.
// 자동 재시도하지 않으며 전송 결과가 불명확하면 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) GenerateDepositAddress(
	ctx context.Context,
	request GenerateDepositAddressRequest,
	options ...trade.RequestOption,
) (GenerateDepositAddressResult, error) {
	if err := request.validate(); err != nil {
		return GenerateDepositAddressResult{}, err
	}
	response, err := client.executeSignedPost(
		ctx, "/v1/deposits/generate_coin_address", request.parameters(), defaultRate,
		credential.PermissionTransfer, options...,
	)
	if err != nil {
		return GenerateDepositAddressResult{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation, func(item *GenerateDepositAddressResult, raw []byte) {
		item.Raw = raw
	})
}

// DepositAddress는 통화와 네트워크의 입금 주소를 조회한다. 업비트 API Key에 입금조회 권한이 필요하다.
func (client *Client) DepositAddress(
	ctx context.Context,
	request DepositAddressRequest,
	options ...trade.RequestOption,
) (DepositAddress, error) {
	if err := request.validate(); err != nil {
		return DepositAddress{}, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/deposits/coin_address", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return DepositAddress{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead, func(item *DepositAddress, raw []byte) {
		item.Raw = raw
	})
}

// DepositAddresses는 계정에 정상 생성된 모든 입금 주소를 조회한다. 업비트 API Key에 입금조회 권한이 필요하다.
func (client *Client) DepositAddresses(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]DepositAddress, error) {
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/deposits/coin_addresses", nil, nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *DepositAddress, raw []byte) {
		item.Raw = raw
	})
}

// DepositKRW는 입출금 계좌에서 원화를 입금한다. 업비트 API Key에 입금하기 권한이 필요하며 2차 인증 앱 승인이 필요하다.
// 자동 재시도하지 않으며 전송 결과가 불명확하면 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) DepositKRW(
	ctx context.Context,
	request KRWDepositRequest,
	options ...trade.RequestOption,
) (Deposit, error) {
	if err := request.validate(); err != nil {
		return Deposit{}, err
	}
	response, err := client.executeSignedPost(
		ctx, "/v1/deposits/krw", request.parameters(), defaultRate, credential.PermissionTransfer, options...,
	)
	if err != nil {
		return Deposit{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation, func(item *Deposit, raw []byte) {
		item.Raw = raw
	})
}

// DepositInfo는 UUID 또는 txid로 입금 한 건을 조회한다. 업비트 API Key에 입금조회 권한이 필요하다.
// 대상이 없으면 HTTP 404 deposit_not_found 오류가 반환되며 APIError의 ExchangeCode로 구분한다.
func (client *Client) DepositInfo(
	ctx context.Context,
	request DepositInfoRequest,
	options ...trade.RequestOption,
) (Deposit, error) {
	if err := request.validate(); err != nil {
		return Deposit{}, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/deposit", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Deposit{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead, func(item *Deposit, raw []byte) {
		item.Raw = raw
	})
}

// Deposits는 입금 목록을 조회한다. 업비트 API Key에 입금조회 권한이 필요하다.
func (client *Client) Deposits(
	ctx context.Context,
	request DepositsRequest,
	options ...trade.RequestOption,
) ([]Deposit, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/deposits", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *Deposit, raw []byte) {
		item.Raw = raw
	})
}

// TravelRuleVASPs는 계정주 확인 서비스를 지원하는 거래소 목록을 조회한다. 업비트 API Key에 입금조회 권한이 필요하다.
func (client *Client) TravelRuleVASPs(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]TravelRuleVASP, error) {
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/travel_rule/vasps", nil, nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *TravelRuleVASP, raw []byte) {
		item.Raw = raw
	})
}

// VerifyTravelRuleByUUID는 입금 UUID로 트래블룰 검증(입금 계정주 확인)을 요청한다.
// 업비트 API Key에 입금하기 권한이 필요하며 같은 입금 건은 10분에 한 번만 요청할 수 있다.
// 자동 재시도하지 않으며 전송 결과가 불명확하면 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) VerifyTravelRuleByUUID(
	ctx context.Context,
	request TravelRuleUUIDRequest,
	options ...trade.RequestOption,
) (TravelRuleVerification, error) {
	if err := request.validate(); err != nil {
		return TravelRuleVerification{}, err
	}
	return client.verifyTravelRule(ctx, "/v1/travel_rule/deposit/uuid", request.parameters(), options...)
}

// VerifyTravelRuleByTxID는 입금 TxID로 트래블룰 검증(입금 계정주 확인)을 요청한다.
// 업비트 API Key에 입금하기 권한이 필요하며 같은 입금 건은 10분에 한 번만 요청할 수 있다.
// 자동 재시도하지 않으며 전송 결과가 불명확하면 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) VerifyTravelRuleByTxID(
	ctx context.Context,
	request TravelRuleTxIDRequest,
	options ...trade.RequestOption,
) (TravelRuleVerification, error) {
	if err := request.validate(); err != nil {
		return TravelRuleVerification{}, err
	}
	return client.verifyTravelRule(ctx, "/v1/travel_rule/deposit/txid", request.parameters(), options...)
}

func (client *Client) verifyTravelRule(
	ctx context.Context,
	path string,
	params parameters,
	options ...trade.RequestOption,
) (TravelRuleVerification, error) {
	response, err := client.executeSignedPost(
		ctx, path, params, defaultRate, credential.PermissionTransfer, options...,
	)
	if err != nil {
		return TravelRuleVerification{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation, func(item *TravelRuleVerification, raw []byte) {
		item.Raw = raw
	})
}

func (request DepositChanceRequest) validate() error {
	return validateCurrencyNetwork(request.Currency, request.NetType)
}

func (request DepositChanceRequest) parameters() parameters {
	return currencyNetworkParameters(request.Currency, request.NetType)
}

func (request GenerateDepositAddressRequest) validate() error {
	return validateCurrencyNetwork(request.Currency, request.NetType)
}

func (request GenerateDepositAddressRequest) parameters() parameters {
	return currencyNetworkParameters(request.Currency, request.NetType)
}

func (request DepositAddressRequest) validate() error {
	return validateCurrencyNetwork(request.Currency, request.NetType)
}

func (request DepositAddressRequest) parameters() parameters {
	return currencyNetworkParameters(request.Currency, request.NetType)
}

func (request KRWDepositRequest) validate() error {
	return validateKRWTransfer(request.Amount, request.TwoFactorType)
}

func (request KRWDepositRequest) parameters() parameters {
	return parameters{
		{key: "amount", value: request.Amount},
		{key: "two_factor_type", value: string(request.TwoFactorType)},
	}
}

func (request DepositInfoRequest) validate() error {
	if err := validateOptionalRequestValue("deposit UUID", request.UUID); err != nil {
		return err
	}
	if err := validateOptionalRequestValue("deposit txid", request.TxID); err != nil {
		return err
	}
	if request.Currency != "" {
		return validateCurrencyCode("currency", request.Currency)
	}
	return nil
}

func (request DepositInfoRequest) parameters() parameters {
	values := parameters{}
	values.add("uuid", request.UUID)
	values.add("txid", request.TxID)
	values.add("currency", request.Currency)
	return values
}

func (request DepositsRequest) validate() error {
	if request.Currency != "" {
		if err := validateCurrencyCode("currency", request.Currency); err != nil {
			return err
		}
	}
	if request.State != "" && !request.State.valid() {
		return validationError("unsupported deposit state %q", request.State)
	}
	if err := validateOptionalValueList("deposit UUIDs", request.UUIDs, 0); err != nil {
		return err
	}
	if err := validateOptionalValueList("deposit txids", request.TxIDs, 0); err != nil {
		return err
	}
	return validateListPaging("deposit", request.Limit, request.Page, request.OrderBy, request.From, request.To)
}

func (request DepositsRequest) parameters() parameters {
	values := parameters{}
	values.add("currency", request.Currency)
	values.add("state", string(request.State))
	values.addAll("uuids[]", request.UUIDs)
	values.addAll("txids[]", request.TxIDs)
	values.addInt("limit", request.Limit)
	values.addInt("page", request.Page)
	values.add("order_by", request.OrderBy)
	values.add("from", request.From)
	values.add("to", request.To)
	return values
}

func (request TravelRuleUUIDRequest) validate() error {
	if err := validateRequestValue("deposit UUID", request.DepositUUID); err != nil {
		return err
	}
	return validateRequestValue("VASP UUID", request.VASPUUID)
}

func (request TravelRuleUUIDRequest) parameters() parameters {
	return parameters{
		{key: "deposit_uuid", value: request.DepositUUID},
		{key: "vasp_uuid", value: request.VASPUUID},
	}
}

func (request TravelRuleTxIDRequest) validate() error {
	if err := validateRequestValue("VASP UUID", request.VASPUUID); err != nil {
		return err
	}
	if err := validateRequestValue("deposit txid", request.TxID); err != nil {
		return err
	}
	return validateCurrencyNetwork(request.Currency, request.NetType)
}

func (request TravelRuleTxIDRequest) parameters() parameters {
	return parameters{
		{key: "txid", value: request.TxID},
		{key: "vasp_uuid", value: request.VASPUUID},
		{key: "currency", value: request.Currency},
		{key: "net_type", value: request.NetType},
	}
}

func (state DepositState) valid() bool {
	switch state {
	case DepositStateProcessing, DepositStateAccepted, DepositStateCancelled, DepositStateRejected,
		DepositStateTravelRuleSuspected, DepositStateRefunding, DepositStateRefunded:
		return true
	default:
		return false
	}
}

// validateCurrencyNetwork는 통화 코드와 네트워크 식별자(net_type)를 함께 쓰는 요청의 필수 값을 검사한다.
func validateCurrencyNetwork(currency, netType string) error {
	if err := validateCurrencyCode("currency", currency); err != nil {
		return err
	}
	return validateRequestValue("net type", netType)
}

// currencyNetworkParameters는 통화 코드와 네트워크 식별자를 문서 순서대로 담은 파라미터를 만든다.
func currencyNetworkParameters(currency, netType string) parameters {
	return parameters{
		{key: "currency", value: currency},
		{key: "net_type", value: netType},
	}
}
