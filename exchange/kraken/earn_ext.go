package kraken

import (
	"context"
	"encoding/json"
	"errors"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// EarnLockType은 Earn 전략의 잠금 방식이다.
type EarnLockType string

const (
	// EarnLockFlex는 현물 잔고에 자동 적용되는 Kraken rewards로, 직접 배분할 수 없다.
	EarnLockFlex EarnLockType = "flex"
	// EarnLockBonded는 본딩과 언본딩 기간이 있는 전략이다.
	EarnLockBonded EarnLockType = "bonded"
	EarnLockTimed  EarnLockType = "timed"
	// EarnLockInstant는 언본딩 기간 없이 해제할 수 있는 전략이다.
	EarnLockInstant EarnLockType = "instant"
)

// EarnStrategiesRequest는 Earn 전략 목록 조회 조건이다. 현재 Kraken은 한 페이지에 모든 전략을 반환한다.
type EarnStrategiesRequest struct {
	Asset     string
	LockTypes []EarnLockType
	Ascending bool
	Limit     int
	Cursor    string
}

// EarnLock은 전략의 잠금 방식과 기간이다. 기간은 초 단위다.
type EarnLock struct {
	Type                    EarnLockType `json:"type"`
	PayoutFrequency         int64        `json:"payout_frequency"`
	BondingPeriod           int64        `json:"bonding_period"`
	BondingPeriodVariable   bool         `json:"bonding_period_variable"`
	BondingRewards          bool         `json:"bonding_rewards"`
	UnbondingPeriod         int64        `json:"unbonding_period"`
	UnbondingPeriodVariable bool         `json:"unbonding_period_variable"`
	UnbondingRewards        bool         `json:"unbonding_rewards"`
	ExitQueuePeriod         int64        `json:"exit_queue_period"`
}

// EarnAPREstimate는 과거 수익을 기준으로 한 연 수익률 추정 범위(퍼센트)다.
type EarnAPREstimate struct {
	Low  string `json:"low"`
	High string `json:"high"`
}

// EarnAutoCompound는 전략의 자동 복리 정책이다. Type은 disabled, enabled, optional 중 하나다.
type EarnAutoCompound struct {
	Type    string `json:"type"`
	Default bool   `json:"default"`
}

// EarnYieldSource는 수익 발생 방식이다. Type은 staking 또는 off_chain이다.
type EarnYieldSource struct {
	Type string `json:"type"`
}

// EarnStrategy는 Earn 전략 한 건이다. 수수료는 문자열 또는 숫자로 내려와 원문 그대로 보존한다.
type EarnStrategy struct {
	ID                        string           `json:"id"`
	Asset                     string           `json:"asset"`
	LockType                  EarnLock         `json:"lock_type"`
	APREstimate               *EarnAPREstimate `json:"apr_estimate"`
	UserMinAllocation         string           `json:"user_min_allocation"`
	UserCap                   string           `json:"user_cap"`
	AllocationFee             DecimalString    `json:"allocation_fee"`
	DeallocationFee           DecimalString    `json:"deallocation_fee"`
	AutoCompound              EarnAutoCompound `json:"auto_compound"`
	YieldSource               EarnYieldSource  `json:"yield_source"`
	CanAllocate               bool             `json:"can_allocate"`
	CanDeallocate             bool             `json:"can_deallocate"`
	AllocationRestrictionInfo []string         `json:"allocation_restriction_info"`
	Raw                       json.RawMessage  `json:"-"`
}

// EarnStrategyPage는 Earn 전략 목록과 다음 페이지 cursor다.
type EarnStrategyPage struct {
	Strategies []EarnStrategy
	NextCursor string
	Raw        json.RawMessage
}

// EarnAllocationsRequest는 Earn 배분 목록 조회 조건이다.
type EarnAllocationsRequest struct {
	// ConvertedAsset은 금액을 환산할 보조 자산이다. 생략하면 USD다.
	ConvertedAsset string
	// HideZeroAllocations가 true이면 과거에 쓰였지만 현재 잔고가 0인 전략을 제외한다.
	HideZeroAllocations bool
	Ascending           bool
}

// EarnAllocationEntry는 상태가 바뀌는 시각이 있는 배분 한 건이다. 시각은 RFC3339 문자열이다.
type EarnAllocationEntry struct {
	CreatedAt string `json:"created_at"`
	Expires   string `json:"expires"`
	Native    string `json:"native"`
	Converted string `json:"converted"`
}

// EarnAllocationState는 한 상태(bonding, unbonding 등)에 있는 배분 합계다.
type EarnAllocationState struct {
	Native          string                `json:"native"`
	Converted       string                `json:"converted"`
	AllocationCount int                   `json:"allocation_count"`
	Allocations     []EarnAllocationEntry `json:"allocations"`
}

// EarnAmountAllocated는 전략에 배분된 금액의 상태별 내역이다. 해당 상태의 배분이 없으면 nil이다.
// Pending은 진행 중인 (해제) 배분 금액이며 해제 중이면 음수다.
type EarnAmountAllocated struct {
	Bonding   *EarnAllocationState `json:"bonding"`
	ExitQueue *EarnAllocationState `json:"exit_queue"`
	Pending   *EarnAllocationState `json:"pending"`
	Unbonding *EarnAllocationState `json:"unbonding"`
	Total     *EarnAllocationState `json:"total"`
}

// EarnAmount는 자산 원래 단위와 환산 단위의 금액이다.
type EarnAmount struct {
	Native    string `json:"native"`
	Converted string `json:"converted"`
}

// EarnPayout은 현재 보상 지급 기간 정보다. 시각은 RFC3339 문자열이다.
type EarnPayout struct {
	AccumulatedReward EarnAmount `json:"accumulated_reward"`
	EstimatedReward   EarnAmount `json:"estimated_reward"`
	PeriodStart       string     `json:"period_start"`
	PeriodEnd         string     `json:"period_end"`
}

// EarnAllocation은 전략 한 건에 대한 배분 현황이다. Payout은 현재 지급 기간이 없으면 nil이다.
type EarnAllocation struct {
	StrategyID      string              `json:"strategy_id"`
	NativeAsset     string              `json:"native_asset"`
	AmountAllocated EarnAmountAllocated `json:"amount_allocated"`
	Payout          *EarnPayout         `json:"payout"`
	TotalRewarded   EarnAmount          `json:"total_rewarded"`
	Raw             json.RawMessage     `json:"-"`
}

// EarnAllocationPage는 Earn 배분 목록과 전체 합계다. 합계는 ConvertedAsset 단위다.
type EarnAllocationPage struct {
	ConvertedAsset string
	TotalAllocated string
	TotalRewarded  string
	Allocations    []EarnAllocation
	NextCursor     string
	Raw            json.RawMessage
}

// EarnAllocateRequest는 Earn 전략에 자금을 배분하거나 배분을 해제하는 조건이다.
// Amount는 전략 자산 단위의 양수 decimal 문자열이다.
type EarnAllocateRequest struct {
	StrategyID string
	Amount     string
}

// EarnOperationStatusRequest는 마지막 배분 또는 해제 요청의 처리 상태 조회 조건이다.
type EarnOperationStatusRequest struct {
	StrategyID string
}

// EarnOperationAck는 배분 또는 해제 요청 접수 결과다. 처리는 비동기이므로 상태 조회로 완료를 확인한다.
type EarnOperationAck struct {
	Accepted bool
	Raw      json.RawMessage
}

// EarnOperationStatus는 마지막 배분 또는 해제 요청의 처리 상태다.
// Pending이 true이면 같은 전략에 대한 요청이 아직 진행 중이다. 요청이 실패했다면 상태 조회가 오류로 반환된다.
type EarnOperationStatus struct {
	Pending bool            `json:"pending"`
	Raw     json.RawMessage `json:"-"`
}

func (request EarnStrategiesRequest) validate() error {
	if request.Asset != "" {
		if err := validateAssetName("earn asset", request.Asset); err != nil {
			return err
		}
	}
	for _, lockType := range request.LockTypes {
		switch lockType {
		case EarnLockFlex, EarnLockBonded, EarnLockTimed, EarnLockInstant:
		default:
			return validationError("unsupported earn lock type %q", lockType)
		}
	}
	if request.Limit < 0 {
		return validationError("earn strategy limit cannot be negative")
	}
	if request.Cursor != "" {
		return validateOpaqueToken("earn strategy cursor", request.Cursor)
	}
	return nil
}

func (request EarnStrategiesRequest) body() map[string]any {
	body := make(map[string]any)
	setBodyString(body, "asset", request.Asset)
	setBodyString(body, "cursor", request.Cursor)
	if len(request.LockTypes) > 0 {
		lockTypes := make([]string, len(request.LockTypes))
		for index, lockType := range request.LockTypes {
			lockTypes[index] = string(lockType)
		}
		body["lock_type"] = lockTypes
	}
	if request.Ascending {
		body["ascending"] = true
	}
	if request.Limit > 0 {
		body["limit"] = request.Limit
	}
	return body
}

func (request EarnAllocationsRequest) validate() error {
	if request.ConvertedAsset != "" {
		return validateAssetName("earn converted asset", request.ConvertedAsset)
	}
	return nil
}

func (request EarnAllocationsRequest) body() map[string]any {
	body := make(map[string]any)
	setBodyString(body, "converted_asset", request.ConvertedAsset)
	if request.HideZeroAllocations {
		body["hide_zero_allocations"] = true
	}
	if request.Ascending {
		body["ascending"] = true
	}
	return body
}

func (request EarnAllocateRequest) validate() error {
	if err := validateEarnStrategyID(request.StrategyID); err != nil {
		return err
	}
	return validateDecimalAmount("earn amount", request.Amount)
}

func (request EarnAllocateRequest) body() map[string]any {
	return map[string]any{"strategy_id": request.StrategyID, "amount": request.Amount}
}

func (request EarnOperationStatusRequest) validate() error {
	return validateEarnStrategyID(request.StrategyID)
}

func (request EarnOperationStatusRequest) body() map[string]any {
	return map[string]any{"strategy_id": request.StrategyID}
}

func validateEarnStrategyID(id string) error {
	if !transactionIDPattern.MatchString(id) {
		return validationError("earn strategy ID is required and contains unsupported characters")
	}
	return nil
}

// EarnStrategies는 사용자가 이용할 수 있는 Earn 전략과 조건을 조회한다. 특별한 API Key 권한이 필요 없다.
func (client *Client) EarnStrategies(
	ctx context.Context,
	request EarnStrategiesRequest,
	options ...trade.RequestOption,
) (EarnStrategyPage, error) {
	if err := request.validate(); err != nil {
		return EarnStrategyPage{}, err
	}
	response, err := client.executePrivateJSON(
		ctx, privatePrefix+"Earn/Strategies", request.body(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return EarnStrategyPage{}, err
	}
	result, err := client.decodeNullableResult(response, commonexchange.OperationRead)
	if err != nil {
		return EarnStrategyPage{}, err
	}
	if result == nil {
		return EarnStrategyPage{}, nil
	}
	var payload struct {
		Items      []json.RawMessage `json:"items"`
		NextCursor string            `json:"next_cursor"`
	}
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &payload); err != nil {
		return EarnStrategyPage{}, err
	}
	page := EarnStrategyPage{
		Strategies: make([]EarnStrategy, len(payload.Items)), NextCursor: payload.NextCursor, Raw: cloneBytes(result),
	}
	for index, item := range payload.Items {
		if err := client.decodeResultInto(
			response, commonexchange.OperationRead, item, &page.Strategies[index],
		); err != nil {
			return EarnStrategyPage{}, err
		}
		page.Strategies[index].Raw = cloneBytes(item)
	}
	return page, nil
}

// EarnAllocations는 사용자의 Earn 배분 현황을 조회한다. 상태별 금액과 다음 보상 지급 정보를 포함한다.
func (client *Client) EarnAllocations(
	ctx context.Context,
	request EarnAllocationsRequest,
	options ...trade.RequestOption,
) (EarnAllocationPage, error) {
	if err := request.validate(); err != nil {
		return EarnAllocationPage{}, err
	}
	response, err := client.executePrivateJSON(
		ctx, privatePrefix+"Earn/Allocations", request.body(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return EarnAllocationPage{}, err
	}
	result, err := client.decodeNullableResult(response, commonexchange.OperationRead)
	if err != nil {
		return EarnAllocationPage{}, err
	}
	if result == nil {
		return EarnAllocationPage{}, nil
	}
	var payload struct {
		ConvertedAsset string            `json:"converted_asset"`
		TotalAllocated string            `json:"total_allocated"`
		TotalRewarded  string            `json:"total_rewarded"`
		NextCursor     string            `json:"next_cursor"`
		Items          []json.RawMessage `json:"items"`
	}
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &payload); err != nil {
		return EarnAllocationPage{}, err
	}
	page := EarnAllocationPage{
		ConvertedAsset: payload.ConvertedAsset, TotalAllocated: payload.TotalAllocated,
		TotalRewarded: payload.TotalRewarded, NextCursor: payload.NextCursor,
		Allocations: make([]EarnAllocation, len(payload.Items)), Raw: cloneBytes(result),
	}
	for index, item := range payload.Items {
		if err := client.decodeResultInto(
			response, commonexchange.OperationRead, item, &page.Allocations[index],
		); err != nil {
			return EarnAllocationPage{}, err
		}
		page.Allocations[index].Raw = cloneBytes(item)
	}
	return page, nil
}

// EarnAllocate는 Earn 전략에 자금을 배분한다. 요청은 비동기로 처리되므로 EarnAllocateStatus로 완료를 확인한다.
// 같은 사용자와 전략에는 진행 중인 (해제) 배분 요청이 하나만 있을 수 있다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) EarnAllocate(
	ctx context.Context,
	request EarnAllocateRequest,
	options ...trade.RequestOption,
) (EarnOperationAck, error) {
	return client.earnMutation(ctx, "Earn/Allocate", request, options...)
}

// EarnDeallocate는 Earn 전략에서 자금 배분을 해제한다. 요청은 비동기로 처리되므로 EarnDeallocateStatus로 완료를 확인한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) EarnDeallocate(
	ctx context.Context,
	request EarnAllocateRequest,
	options ...trade.RequestOption,
) (EarnOperationAck, error) {
	return client.earnMutation(ctx, "Earn/Deallocate", request, options...)
}

// EarnAllocateStatus는 마지막 배분 요청의 처리 상태를 조회한다.
func (client *Client) EarnAllocateStatus(
	ctx context.Context,
	request EarnOperationStatusRequest,
	options ...trade.RequestOption,
) (EarnOperationStatus, error) {
	return client.earnStatus(ctx, "Earn/AllocateStatus", request, options...)
}

// EarnDeallocateStatus는 마지막 해제 요청의 처리 상태를 조회한다.
func (client *Client) EarnDeallocateStatus(
	ctx context.Context,
	request EarnOperationStatusRequest,
	options ...trade.RequestOption,
) (EarnOperationStatus, error) {
	return client.earnStatus(ctx, "Earn/DeallocateStatus", request, options...)
}

func (client *Client) earnMutation(
	ctx context.Context,
	endpoint string,
	request EarnAllocateRequest,
	options ...trade.RequestOption,
) (EarnOperationAck, error) {
	if err := request.validate(); err != nil {
		return EarnOperationAck{}, err
	}
	response, result, err := client.privateJSONResult(
		ctx, privatePrefix+endpoint, request.body(), credential.PermissionTransfer,
		commonexchange.OperationMutation, limitPrivate, "", options...,
	)
	if err != nil {
		return EarnOperationAck{}, err
	}
	var accepted bool
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &accepted); err != nil {
		return EarnOperationAck{}, err
	}
	if !accepted {
		return EarnOperationAck{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Kraken earn request was not accepted"),
		)
	}
	return EarnOperationAck{Accepted: true, Raw: cloneBytes(result)}, nil
}

func (client *Client) earnStatus(
	ctx context.Context,
	endpoint string,
	request EarnOperationStatusRequest,
	options ...trade.RequestOption,
) (EarnOperationStatus, error) {
	if err := request.validate(); err != nil {
		return EarnOperationStatus{}, err
	}
	response, err := client.executePrivateJSON(
		ctx, privatePrefix+endpoint, request.body(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return EarnOperationStatus{}, err
	}
	result, err := client.decodeNullableResult(response, commonexchange.OperationRead)
	if err != nil {
		return EarnOperationStatus{}, err
	}
	if result == nil {
		return EarnOperationStatus{}, nil
	}
	var status EarnOperationStatus
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &status); err != nil {
		return EarnOperationStatus{}, err
	}
	status.Raw = cloneBytes(result)
	return status, nil
}
