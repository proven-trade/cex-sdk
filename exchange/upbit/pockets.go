package upbit

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	// maxPocketTransferIDs는 이전 내역 조회가 한 번에 받는 UUID 또는 identifier 최대 개수다.
	maxPocketTransferIDs = 20
	// maxPocketTransferLimit은 이전 내역 조회의 limit 상한이다.
	maxPocketTransferLimit = 100
	// maxPocketTransferRange는 이전 내역 조회의 start_time~end_time 최대 범위다.
	maxPocketTransferRange = 7 * 24 * time.Hour
)

// PocketType은 포켓 종류다.
type PocketType string

const (
	PocketTypeMain            PocketType = "main"
	PocketTypeUserSpotTrading PocketType = "user_spot_trading"
)

// TransferDirection은 서브포켓 기준 자산 이전 방향이다.
type TransferDirection string

const (
	TransferDirectionIn  TransferDirection = "in"
	TransferDirectionOut TransferDirection = "out"
	TransferDirectionAll TransferDirection = "all"
)

// PocketTransferState는 포켓 간 자산 이전 요청 상태다.
type PocketTransferState string

const (
	PocketTransferSubmitted  PocketTransferState = "submitted"
	PocketTransferProcessing PocketTransferState = "processing"
	PocketTransferDone       PocketTransferState = "done"
	PocketTransferFailed     PocketTransferState = "failed"
)

// Pocket은 메인포켓 또는 사용자가 만든 서브포켓의 UUID와 이름이다.
type Pocket struct {
	UUID string          `json:"uuid"`
	Name string          `json:"name"`
	Type PocketType      `json:"type"`
	Raw  json.RawMessage `json:"-"`
}

// PocketAPIKeysRequest는 포켓별 API Key 목록 조회 조건이다.
type PocketAPIKeysRequest struct {
	// UUIDs를 비우면 모든 포켓이 대상이다.
	UUIDs []string
	// IncludeExpired가 true이면 만료된 API Key도 포함한다.
	IncludeExpired bool
}

// PocketAPIKey는 포켓에 발급된 API Key 한 건이다.
// Permissions는 view_account, view_orders, make_orders, view_withdrawals, withdraw,
// view_deposits, deposit, transfer, manage_pockets 중 부여된 권한이다.
type PocketAPIKey struct {
	AccessKey   string   `json:"access_key"`
	Permissions []string `json:"permissions"`
	AllowedIPs  []string `json:"allowed_ips"`
	CreatedAt   string   `json:"created_at"`
	ExpiredAt   string   `json:"expired_at"`
}

// PocketAPIKeySet은 한 포켓에 발급된 API Key 목록이다.
type PocketAPIKeySet struct {
	UUID string          `json:"uuid"`
	Keys []PocketAPIKey  `json:"keys"`
	Raw  json.RawMessage `json:"-"`
}

// PocketAssetsRequest는 잔고를 조회할 서브포켓이다.
type PocketAssetsRequest struct {
	UUID string
}

// PocketTransfer는 포켓 간 자산 이전 요청 한 건이다. Identifier는 지정하지 않았으면 비어 있다.
type PocketTransfer struct {
	UUID       string              `json:"uuid"`
	Identifier string              `json:"identifier"`
	From       string              `json:"from"`
	To         string              `json:"to"`
	State      PocketTransferState `json:"state"`
	Currency   string              `json:"currency"`
	Amount     Decimal             `json:"amount"`
	CreatedAt  string              `json:"created_at"`
	Raw        json.RawMessage     `json:"-"`
}

// PocketTransferRequest는 서브포켓 API Key로 보내는 자산 이전 요청이다.
type PocketTransferRequest struct {
	// To는 받는 포켓의 UUID다. 비우면 메인포켓으로 이전한다.
	To       string
	Currency string
	Amount   string
	// Identifier는 선택 식별자이며 1~64자의 영문자, 숫자, _, ., -만 허용한다.
	Identifier string
}

// UniversalTransferRequest는 메인포켓 API Key로 보내는 포켓 간 자산 이전 요청이다.
type UniversalTransferRequest struct {
	// From은 보내는 포켓의 UUID다. 비우면 요청에 쓴 API Key가 속한 포켓이다.
	From string
	// To는 받는 포켓의 UUID다.
	To       string
	Currency string
	Amount   string
	// Identifier는 선택 식별자이며 1~64자의 영문자, 숫자, _, ., -만 허용한다.
	Identifier string
}

// PocketTransferQuery는 자산 이전 내역 조회의 공통 필터다.
// StartTime과 EndTime은 함께 지정하면 최대 7일 범위여야 하며 EndTime을 비우면 현재 시각이다.
type PocketTransferQuery struct {
	States      []PocketTransferState
	UUIDs       []string
	Identifiers []string
	StartTime   *time.Time
	EndTime     *time.Time
	Currency    string
	// Limit을 비우면 20이며 최대 100이다.
	Limit int
	// OrderBy는 asc 또는 desc이며 비우면 desc다.
	OrderBy string
}

// PocketTransfersRequest는 서브포켓 API Key로 조회하는 자산 이전 내역 조건이다.
type PocketTransfersRequest struct {
	Direction TransferDirection
	PocketTransferQuery
}

// UniversalTransfersRequest는 메인포켓 API Key로 조회하는 자산 이전 내역 조건이다.
type UniversalTransfersRequest struct {
	From string
	To   string
	PocketTransferQuery
}

// Pockets는 메인포켓과 사용자가 만든 서브포켓의 UUID와 이름을 조회한다.
// 메인포켓 API Key와 업비트 포켓관리 권한이 필요하다.
func (client *Client) Pockets(ctx context.Context, options ...trade.RequestOption) ([]Pocket, error) {
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/pockets", nil, nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *Pocket, raw []byte) {
		item.Raw = raw
	})
}

// PocketAPIKeys는 메인포켓 권한으로 포켓별 API Key 목록을 조회한다.
// 메인포켓 API Key와 업비트 포켓관리 권한이 필요하다.
func (client *Client) PocketAPIKeys(
	ctx context.Context,
	request PocketAPIKeysRequest,
	options ...trade.RequestOption,
) ([]PocketAPIKeySet, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/pockets/api_keys", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *PocketAPIKeySet, raw []byte) {
		item.Raw = raw
	})
}

// PocketAssets는 메인포켓 권한으로 서브포켓 하나의 잔고를 조회한다.
// 메인포켓 API Key와 업비트 포켓관리 권한이 필요하다. 없는 포켓이면 HTTP 404가 반환된다.
func (client *Client) PocketAssets(
	ctx context.Context,
	request PocketAssetsRequest,
	options ...trade.RequestOption,
) ([]Balance, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/pockets/assets", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[Balance](client, response, commonexchange.OperationRead, nil)
}

// CreatePocketTransfer는 서브포켓 API Key로 포켓 간 자산 이전을 요청한다.
// 업비트 자산이전 권한이 필요하며, 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) CreatePocketTransfer(
	ctx context.Context,
	request PocketTransferRequest,
	options ...trade.RequestOption,
) (PocketTransfer, error) {
	if err := request.validate(); err != nil {
		return PocketTransfer{}, err
	}
	response, err := client.executeSignedPost(
		ctx, "/v1/pockets/transfers", request.parameters(), defaultRate, credential.PermissionTransfer, options...,
	)
	if err != nil {
		return PocketTransfer{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation, func(item *PocketTransfer, raw []byte) {
		item.Raw = raw
	})
}

// PocketTransfers는 서브포켓 API Key로 해당 서브포켓의 자산 이전 내역을 조회한다.
// 업비트 자산이전 권한이 필요하다.
func (client *Client) PocketTransfers(
	ctx context.Context,
	request PocketTransfersRequest,
	options ...trade.RequestOption,
) ([]PocketTransfer, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/pockets/transfers", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *PocketTransfer, raw []byte) {
		item.Raw = raw
	})
}

// CreateUniversalTransfer는 메인포켓 API Key로 메인포켓과 서브포켓 사이 자산 이전을 요청한다.
// 업비트 포켓관리 권한이 필요하며, 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) CreateUniversalTransfer(
	ctx context.Context,
	request UniversalTransferRequest,
	options ...trade.RequestOption,
) (PocketTransfer, error) {
	if err := request.validate(); err != nil {
		return PocketTransfer{}, err
	}
	response, err := client.executeSignedPost(
		ctx, "/v1/pockets/universal_transfers", request.parameters(), defaultRate,
		credential.PermissionTransfer, options...,
	)
	if err != nil {
		return PocketTransfer{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation, func(item *PocketTransfer, raw []byte) {
		item.Raw = raw
	})
}

// UniversalTransfers는 메인포켓 API Key로 포켓 간 자산 이전 내역을 조회한다.
// 업비트 포켓관리 권한이 필요하다.
func (client *Client) UniversalTransfers(
	ctx context.Context,
	request UniversalTransfersRequest,
	options ...trade.RequestOption,
) ([]PocketTransfer, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/pockets/universal_transfers", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *PocketTransfer, raw []byte) {
		item.Raw = raw
	})
}

func (request PocketAPIKeysRequest) validate() error {
	return validateOptionalValueList("pocket UUIDs", request.UUIDs, 0)
}

func (request PocketAPIKeysRequest) parameters() parameters {
	values := parameters{}
	values.addAll("uuids[]", request.UUIDs)
	values.addBool("include_expired", request.IncludeExpired)
	return values
}

func (request PocketAssetsRequest) validate() error {
	return validateRequestValue("pocket UUID", request.UUID)
}

func (request PocketAssetsRequest) parameters() parameters {
	return parameters{{key: "uuid", value: request.UUID}}
}

func (request PocketTransferRequest) validate() error {
	if err := validateOptionalRequestValue("destination pocket UUID", request.To); err != nil {
		return err
	}
	return validateTransferFields(request.Currency, request.Amount, request.Identifier)
}

func (request PocketTransferRequest) parameters() parameters {
	values := parameters{}
	values.add("to", request.To)
	values.add("currency", request.Currency)
	values.add("amount", request.Amount)
	values.add("identifier", request.Identifier)
	return values
}

func (request UniversalTransferRequest) validate() error {
	if err := validateOptionalRequestValue("source pocket UUID", request.From); err != nil {
		return err
	}
	if err := validateRequestValue("destination pocket UUID", request.To); err != nil {
		return err
	}
	return validateTransferFields(request.Currency, request.Amount, request.Identifier)
}

func (request UniversalTransferRequest) parameters() parameters {
	values := parameters{}
	values.add("from", request.From)
	values.add("to", request.To)
	values.add("currency", request.Currency)
	values.add("amount", request.Amount)
	values.add("identifier", request.Identifier)
	return values
}

func (query PocketTransferQuery) validate() error {
	for _, state := range query.States {
		if !state.valid() {
			return validationError("unsupported pocket transfer state %q", state)
		}
	}
	if err := validateOptionalValueList("transfer UUIDs", query.UUIDs, maxPocketTransferIDs); err != nil {
		return err
	}
	if err := validateOptionalValueList("transfer identifiers", query.Identifiers, maxPocketTransferIDs); err != nil {
		return err
	}
	if query.StartTime != nil && query.EndTime != nil {
		if query.StartTime.After(*query.EndTime) {
			return validationError("startTime cannot be after endTime")
		}
		if query.EndTime.Sub(*query.StartTime) > maxPocketTransferRange {
			return validationError("pocket transfer time range cannot exceed seven days")
		}
	}
	if query.Currency != "" {
		if err := validateCurrencyCode("currency", query.Currency); err != nil {
			return err
		}
	}
	if err := validatePageLimit("pocket transfer", query.Limit, maxPocketTransferLimit); err != nil {
		return err
	}
	return validateOrderBy(query.OrderBy)
}

func (query PocketTransferQuery) parameters() parameters {
	values := parameters{}
	for _, state := range query.States {
		values.add("states[]", string(state))
	}
	values.addAll("uuids[]", query.UUIDs)
	values.addAll("identifiers[]", query.Identifiers)
	if query.StartTime != nil {
		values.add("start_time", query.StartTime.Format(time.RFC3339))
	}
	if query.EndTime != nil {
		values.add("end_time", query.EndTime.Format(time.RFC3339))
	}
	values.add("currency", query.Currency)
	values.addInt("limit", query.Limit)
	values.add("order_by", query.OrderBy)
	return values
}

func (request PocketTransfersRequest) validate() error {
	switch request.Direction {
	case "", TransferDirectionIn, TransferDirectionOut, TransferDirectionAll:
	default:
		return validationError("transfer direction must be in, out, or all")
	}
	return request.PocketTransferQuery.validate()
}

func (request PocketTransfersRequest) parameters() parameters {
	values := parameters{}
	values.add("direction", string(request.Direction))
	return append(values, request.PocketTransferQuery.parameters()...)
}

func (request UniversalTransfersRequest) validate() error {
	if err := validateOptionalRequestValue("source pocket UUID", request.From); err != nil {
		return err
	}
	if err := validateOptionalRequestValue("destination pocket UUID", request.To); err != nil {
		return err
	}
	return request.PocketTransferQuery.validate()
}

func (request UniversalTransfersRequest) parameters() parameters {
	values := parameters{}
	values.add("from", request.From)
	values.add("to", request.To)
	return append(values, request.PocketTransferQuery.parameters()...)
}

func (state PocketTransferState) valid() bool {
	switch state {
	case PocketTransferSubmitted, PocketTransferProcessing, PocketTransferDone, PocketTransferFailed:
		return true
	default:
		return false
	}
}

// validateTransferFields는 자산 이전 요청의 통화, 수량과 식별자를 검사한다.
func validateTransferFields(currency, amount, identifier string) error {
	if err := validateCurrencyCode("currency", currency); err != nil {
		return err
	}
	if err := validatePositiveDecimal("amount", amount); err != nil {
		return err
	}
	return validateTransferIdentifier(identifier)
}
