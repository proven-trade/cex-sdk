package kraken

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	maxQueryStrategyIDs     = 20
	maxClosedStrategiesPage = 100
)

var strategyIDPattern = regexp.MustCompile(`^AS[A-Z0-9]{5}-[A-Z0-9]{5}-[A-Z0-9]{6}$`)

// StrategyType은 알고리즘 전략 종류다. 현재 twap만 지원된다.
type StrategyType string

const StrategyTypeTWAP StrategyType = "twap"

// StrategyStatus는 알고리즘 전략의 진행 상태다.
type StrategyStatus string

const (
	// StrategyPending은 접수되어 starttm을 기다리는 상태다.
	StrategyPending StrategyStatus = "pending"
	// StrategyOpen은 실행 중인 상태다.
	StrategyOpen StrategyStatus = "open"
	// StrategyClosed는 expiretm 전에 전량 체결된 상태다.
	StrategyClosed StrategyStatus = "closed"
	// StrategyExpired는 전량 체결되기 전에 expiretm이 도래한 상태다.
	StrategyExpired StrategyStatus = "expired"
	// StrategyCancelled는 사용자가 취소한 상태다.
	StrategyCancelled StrategyStatus = "cancelled"
)

// StrategyAssetClass는 전략 상품의 자산 분류다.
type StrategyAssetClass string

const (
	StrategyAssetClassForex          StrategyAssetClass = "forex"
	StrategyAssetClassTokenizedAsset StrategyAssetClass = "tokenized_asset"
	StrategyAssetClassSyntheticPair  StrategyAssetClass = "synthetic_pair"
)

// StrategyQueryTimeMode는 종료 전략 조회에서 start와 end 경계가 적용되는 시각 기준이다.
type StrategyQueryTimeMode string

const (
	// StrategyQueryTimeOpen은 두 경계를 전략 개시 시각에 적용한다.
	StrategyQueryTimeOpen StrategyQueryTimeMode = "open"
	// StrategyQueryTimeClose는 두 경계를 전략 종료 시각에 적용한다.
	StrategyQueryTimeClose StrategyQueryTimeMode = "close"
	// StrategyQueryTimeBoth는 start를 개시 시각에, end를 종료 시각에 적용한다.
	StrategyQueryTimeBoth StrategyQueryTimeMode = "both"
)

// AddStrategyRequest는 TWAP 알고리즘 전략 생성 조건이다.
// 전략은 Volume을 StartTime부터 ExpireTime까지 균등하게 market 또는 limit 하위 주문으로 나눠 실행한다.
type AddStrategyRequest struct {
	// StrategyType을 생략하면 twap이다.
	StrategyType StrategyType
	Pair         string
	Side         Side
	// Volume은 기준 통화 단위의 총 수량이다.
	Volume string
	// OrderType은 market 또는 limit이다. limit이면 Price가 필요하고 market이면 Price를 생략해야 한다.
	OrderType OrderType
	Price     string
	// StartTime은 "0"(즉시), "+초"(지금부터 상대), Unix 초 중 하나이며 생략하면 즉시 시작한다.
	StartTime string
	// ExpireTime은 "+초" 또는 Unix 초다. 시작 후 60초 이상, 현재로부터 30일 이내여야 한다.
	ExpireTime string
	// ValidateOnly가 true이면 전략을 만들지 않고 요청만 검증하며 ID는 비어 있다.
	ValidateOnly bool
}

// StrategyReference는 전략 생성 또는 취소 결과다. ValidateOnly 생성은 ID가 비어 있다.
type StrategyReference struct {
	ID  string          `json:"strategy_id"`
	Raw json.RawMessage `json:"-"`
}

// CancelStrategyRequest는 pending 또는 open 전략 취소 조건이다.
type CancelStrategyRequest struct {
	StrategyID string
}

// OpenStrategiesRequest는 진행 중(pending, open) 전략 조회 조건이다.
type OpenStrategiesRequest struct {
	// StrategyType을 생략하면 모든 종류를 반환한다.
	StrategyType StrategyType
}

// QueryStrategiesRequest는 전략 ID로 최대 20건을 조회한다.
type QueryStrategiesRequest struct {
	StrategyIDs []string
}

// ClosedStrategiesRequest는 종료된 전략(closed, expired, cancelled) 조회 조건이다.
// cursor는 정렬 위치만 담으므로 다음 페이지에서도 같은 필터를 반복해야 한다.
type ClosedStrategiesRequest struct {
	AssetClass   StrategyAssetClass
	StrategyType StrategyType
	Status       StrategyStatus
	// Start와 End는 QueryTimeMode가 정한 시각에 적용되는 Unix 초 포함 경계다.
	Start         *time.Time
	End           *time.Time
	QueryTimeMode StrategyQueryTimeMode
	// Limit을 생략하면 100이며 최대 100이다.
	Limit  int
	Cursor string
}

// StrategyDetails는 TWAP 전략의 설정과 실행 진행 상황이다. 시각은 Unix 초 문자열이다.
type StrategyDetails struct {
	StrategyType StrategyType       `json:"strategy_type"`
	Side         Side               `json:"side"`
	Volume       string             `json:"volume"`
	FilledVolume string             `json:"filled_volume"`
	AveragePrice string             `json:"avg_price"`
	TotalCost    string             `json:"total_cost"`
	TotalFee     string             `json:"total_fee"`
	StartTime    string             `json:"starttm"`
	ExpireTime   string             `json:"expiretm"`
	Pair         string             `json:"symbol"`
	AssetClass   StrategyAssetClass `json:"asset_class"`
	OrderType    OrderType          `json:"ordertype"`
	Price        string             `json:"price"`
}

// Strategy는 알고리즘 전략 한 건이다. Reason은 insufficientFunds, remainingBelowMinimum, userRequested 등 상태 사유다.
type Strategy struct {
	ID      string          `json:"strategy_id"`
	Status  StrategyStatus  `json:"status"`
	Reason  string          `json:"reason"`
	Details StrategyDetails `json:"details"`
	Raw     json.RawMessage `json:"-"`
}

// ClosedStrategyPage는 종료 전략 목록과 다음 페이지 cursor다. 마지막 페이지는 NextCursor가 비어 있다.
type ClosedStrategyPage struct {
	Strategies []Strategy
	NextCursor string
	Raw        json.RawMessage
}

func (request AddStrategyRequest) validate() error {
	if request.StrategyType != "" && request.StrategyType != StrategyTypeTWAP {
		return validationError("unsupported strategy type %q", request.StrategyType)
	}
	if err := validatePair(request.Pair); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("strategy side must be buy or sell")
	}
	if err := validateDecimalAmount("strategy volume", request.Volume); err != nil {
		return err
	}
	switch request.OrderType {
	case OrderTypeMarket:
		if request.Price != "" {
			return validationError("market strategy must omit price")
		}
	case OrderTypeLimit:
		if err := validateDecimalAmount("strategy price", request.Price); err != nil {
			return err
		}
	default:
		return validationError("strategy order type must be market or limit")
	}
	if request.StartTime != "" && !scheduleTimePattern.MatchString(request.StartTime) {
		return validationError("strategy start time must be 0, +seconds or a Unix timestamp")
	}
	if !scheduleTimePattern.MatchString(request.ExpireTime) {
		return validationError("strategy expire time must be +seconds or a Unix timestamp")
	}
	return nil
}

func (request AddStrategyRequest) body() map[string]any {
	strategyType := request.StrategyType
	if strategyType == "" {
		strategyType = StrategyTypeTWAP
	}
	body := map[string]any{
		"strategy_type": string(strategyType),
		"symbol":        request.Pair,
		"asset_class":   string(StrategyAssetClassForex),
		"side":          string(request.Side),
		"volume":        request.Volume,
		"ordertype":     string(request.OrderType),
		"expiretm":      request.ExpireTime,
	}
	setBodyString(body, "price", request.Price)
	setBodyString(body, "starttm", request.StartTime)
	if request.ValidateOnly {
		body["validate"] = true
	}
	return body
}

func (request CancelStrategyRequest) validate() error {
	return validateStrategyID(request.StrategyID)
}

func (request CancelStrategyRequest) body() map[string]any {
	return map[string]any{"strategy_id": request.StrategyID}
}

func (request OpenStrategiesRequest) validate() error {
	if request.StrategyType != "" && request.StrategyType != StrategyTypeTWAP {
		return validationError("unsupported strategy type %q", request.StrategyType)
	}
	return nil
}

func (request OpenStrategiesRequest) body() map[string]any {
	body := make(map[string]any)
	setBodyString(body, "strategy_type", string(request.StrategyType))
	return body
}

func (request QueryStrategiesRequest) validate() error {
	if len(request.StrategyIDs) < 1 || len(request.StrategyIDs) > maxQueryStrategyIDs {
		return validationError("strategy ID count must be between 1 and %d", maxQueryStrategyIDs)
	}
	seen := make(map[string]struct{}, len(request.StrategyIDs))
	for _, id := range request.StrategyIDs {
		if err := validateStrategyID(id); err != nil {
			return err
		}
		if _, exists := seen[id]; exists {
			return validationError("duplicate strategy ID %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (request QueryStrategiesRequest) body() map[string]any {
	return map[string]any{"strategy_ids": strings.Join(request.StrategyIDs, ",")}
}

func (request ClosedStrategiesRequest) validate() error {
	switch request.AssetClass {
	case "", StrategyAssetClassForex, StrategyAssetClassTokenizedAsset, StrategyAssetClassSyntheticPair:
	default:
		return validationError("unsupported strategy asset class %q", request.AssetClass)
	}
	if request.StrategyType != "" && request.StrategyType != StrategyTypeTWAP {
		return validationError("unsupported strategy type %q", request.StrategyType)
	}
	switch request.Status {
	case "", StrategyClosed, StrategyExpired, StrategyCancelled:
	default:
		return validationError("closed strategy status must be closed, expired or cancelled")
	}
	switch request.QueryTimeMode {
	case "", StrategyQueryTimeOpen, StrategyQueryTimeClose, StrategyQueryTimeBoth:
	default:
		return validationError("unsupported strategy query time mode %q", request.QueryTimeMode)
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > maxClosedStrategiesPage {
		return validationError("closed strategy limit must be between 1 and %d", maxClosedStrategiesPage)
	}
	if request.Cursor != "" {
		return validateOpaqueToken("closed strategy cursor", request.Cursor)
	}
	return nil
}

func (request ClosedStrategiesRequest) body() map[string]any {
	body := make(map[string]any)
	setBodyString(body, "asset_class", string(request.AssetClass))
	setBodyString(body, "strategy_type", string(request.StrategyType))
	setBodyString(body, "status", string(request.Status))
	setBodyString(body, "query_time_mode", string(request.QueryTimeMode))
	setBodyString(body, "cursor", request.Cursor)
	if request.Start != nil {
		body["start"] = request.Start.Unix()
	}
	if request.End != nil {
		body["end"] = request.End.Unix()
	}
	if request.Limit > 0 {
		body["limit"] = request.Limit
	}
	return body
}

func validateStrategyID(id string) error {
	if !strategyIDPattern.MatchString(id) {
		return validationError("strategy ID must look like ASXXXXX-XXXXX-XXXXXX")
	}
	return nil
}

// AddStrategy는 TWAP 알고리즘 전략을 만든다. 전략 ID는 CancelStrategy와 QueryStrategies에 쓴다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) AddStrategy(
	ctx context.Context,
	request AddStrategyRequest,
	options ...trade.RequestOption,
) (StrategyReference, error) {
	if err := request.validate(); err != nil {
		return StrategyReference{}, err
	}
	response, result, err := client.privateJSONResult(
		ctx, privatePrefix+"AddStrategy", request.body(), credential.PermissionTrade,
		commonexchange.OperationMutation, limitPrivate, "", options...,
	)
	if err != nil {
		return StrategyReference{}, err
	}
	var reference StrategyReference
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &reference); err != nil {
		return StrategyReference{}, err
	}
	if reference.ID == "" && !request.ValidateOnly {
		return StrategyReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Kraken strategy acknowledgement is empty"),
		)
	}
	reference.Raw = cloneBytes(result)
	return reference, nil
}

// CancelStrategy는 pending 또는 open 전략을 취소한다. 이미 제출된 하위 주문은 취소 완료 전에 체결될 수 있다.
// 최종 상태와 체결 합계는 QueryStrategies로 확인한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) CancelStrategy(
	ctx context.Context,
	request CancelStrategyRequest,
	options ...trade.RequestOption,
) (StrategyReference, error) {
	if err := request.validate(); err != nil {
		return StrategyReference{}, err
	}
	response, result, err := client.privateJSONResult(
		ctx, privatePrefix+"CancelStrategy", request.body(), credential.PermissionTrade,
		commonexchange.OperationMutation, limitPrivate, "", options...,
	)
	if err != nil {
		return StrategyReference{}, err
	}
	var reference StrategyReference
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &reference); err != nil {
		return StrategyReference{}, err
	}
	if reference.ID == "" {
		return StrategyReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Kraken strategy cancellation acknowledgement is empty"),
		)
	}
	reference.Raw = cloneBytes(result)
	return reference, nil
}

// OpenStrategies는 pending 또는 open 상태의 전략을 조회한다. 결과는 전략 ID 순서다.
// Kraken endpoint 이름은 GetOpenStrategies다.
func (client *Client) OpenStrategies(
	ctx context.Context,
	request OpenStrategiesRequest,
	options ...trade.RequestOption,
) ([]Strategy, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.privateJSONResult(
		ctx, privatePrefix+"GetOpenStrategies", request.body(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Open json.RawMessage `json:"open"`
	}
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &payload); err != nil {
		return nil, err
	}
	strategies, err := decodeStrategyMap(payload.Open)
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return strategies, nil
}

// QueryStrategies는 전략 ID로 최대 20건을 조회한다. 일치하지 않는 ID는 결과에서 빠진다. 결과는 전략 ID 순서다.
func (client *Client) QueryStrategies(
	ctx context.Context,
	request QueryStrategiesRequest,
	options ...trade.RequestOption,
) ([]Strategy, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.privateJSONResult(
		ctx, privatePrefix+"QueryStrategies", request.body(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Strategies json.RawMessage `json:"strategies"`
	}
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &payload); err != nil {
		return nil, err
	}
	strategies, err := decodeStrategyMap(payload.Strategies)
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return strategies, nil
}

// ClosedStrategies는 종료된 전략을 최신순으로 조회한다. Kraken endpoint 이름은 GetClosedStrategies다.
// 다음 페이지는 NextCursor와 같은 필터로 다시 호출한다.
func (client *Client) ClosedStrategies(
	ctx context.Context,
	request ClosedStrategiesRequest,
	options ...trade.RequestOption,
) (ClosedStrategyPage, error) {
	if err := request.validate(); err != nil {
		return ClosedStrategyPage{}, err
	}
	response, result, err := client.privateJSONResult(
		ctx, privatePrefix+"GetClosedStrategies", request.body(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivateHistory, "", options...,
	)
	if err != nil {
		return ClosedStrategyPage{}, err
	}
	var payload struct {
		Closed     []json.RawMessage `json:"closed"`
		NextCursor string            `json:"next_cursor"`
	}
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &payload); err != nil {
		return ClosedStrategyPage{}, err
	}
	page := ClosedStrategyPage{
		Strategies: make([]Strategy, len(payload.Closed)), NextCursor: payload.NextCursor, Raw: cloneBytes(result),
	}
	for index, item := range payload.Closed {
		if err := client.decodeResultInto(
			response, commonexchange.OperationRead, item, &page.Strategies[index],
		); err != nil {
			return ClosedStrategyPage{}, err
		}
		page.Strategies[index].Raw = cloneBytes(item)
	}
	return page, nil
}

// decodeStrategyMap은 전략 ID를 key로 하는 객체를 ID 순서의 목록으로 decode한다.
func decodeStrategyMap(raw json.RawMessage) ([]Strategy, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []Strategy{}, nil
	}
	entries, err := decodeKeyedEntries[Strategy](raw)
	if err != nil {
		return nil, err
	}
	strategies := make([]Strategy, len(entries))
	for index, entry := range entries {
		strategies[index] = entry.Value
		if strategies[index].ID == "" {
			strategies[index].ID = entry.Key
		}
		strategies[index].Raw = entry.Raw
	}
	return strategies, nil
}
