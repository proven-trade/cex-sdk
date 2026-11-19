package coinbase

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// MarginProfileType은 CFM 계정의 증거금 프로필이다.
type MarginProfileType string

const (
	MarginProfileUnspecified     MarginProfileType = "MARGIN_PROFILE_TYPE_UNSPECIFIED"
	MarginProfileRetailRegular   MarginProfileType = "MARGIN_PROFILE_TYPE_RETAIL_REGULAR"
	MarginProfileRetailIntraday1 MarginProfileType = "MARGIN_PROFILE_TYPE_RETAIL_INTRADAY_MARGIN_1"
)

// IntradayMarginSetting은 평일 장중 증거금 상향 적용 여부다.
type IntradayMarginSetting string

const (
	IntradayMarginSettingStandard IntradayMarginSetting = "INTRADAY_MARGIN_SETTING_STANDARD"
	IntradayMarginSettingIntraday IntradayMarginSetting = "INTRADAY_MARGIN_SETTING_INTRADAY"
)

// CFMMarginWindowMeasure는 증거금 구간별 증거금과 청산 완충액이다.
type CFMMarginWindowMeasure struct {
	MarginWindowType   string `json:"margin_window_type"`
	MarginLevel        string `json:"margin_level"`
	InitialMargin      string `json:"initial_margin"`
	MaintenanceMargin  string `json:"maintenance_margin"`
	LiquidationBuffer  string `json:"liquidation_buffer"`
	TotalHold          string `json:"total_hold"`
	FuturesBuyingPower string `json:"futures_buying_power"`
}

// CFMBalanceSummary는 미국 선물(CFM) 계정의 잔고와 증거금 요약이다.
type CFMBalanceSummary struct {
	FuturesBuyingPower           Amount                 `json:"futures_buying_power"`
	TotalUSDBalance              Amount                 `json:"total_usd_balance"`
	CBIUSDBalance                Amount                 `json:"cbi_usd_balance"`
	CFMUSDBalance                Amount                 `json:"cfm_usd_balance"`
	TotalOpenOrdersHoldAmount    Amount                 `json:"total_open_orders_hold_amount"`
	UnrealizedPnL                Amount                 `json:"unrealized_pnl"`
	DailyRealizedPnL             Amount                 `json:"daily_realized_pnl"`
	InitialMargin                Amount                 `json:"initial_margin"`
	AvailableMargin              Amount                 `json:"available_margin"`
	LiquidationThreshold         Amount                 `json:"liquidation_threshold"`
	LiquidationBufferAmount      Amount                 `json:"liquidation_buffer_amount"`
	LiquidationBufferPercentage  string                 `json:"liquidation_buffer_percentage"`
	IntradayMarginWindowMeasure  CFMMarginWindowMeasure `json:"intraday_margin_window_measure"`
	OvernightMarginWindowMeasure CFMMarginWindowMeasure `json:"overnight_margin_window_measure"`
	TotalPendingTransfersAmount  Amount                 `json:"total_pending_transfers_amount"`
	FundingPnL                   Amount                 `json:"funding_pnl"`
	Raw                          json.RawMessage        `json:"-"`
}

// CFMPosition은 CFM 상품의 포지션 한 건이다.
type CFMPosition struct {
	ProductID         string          `json:"product_id"`
	ExpirationTime    string          `json:"expiration_time"`
	Side              string          `json:"side"`
	NumberOfContracts string          `json:"number_of_contracts"`
	CurrentPrice      string          `json:"current_price"`
	AverageEntryPrice string          `json:"avg_entry_price"`
	UnrealizedPnL     string          `json:"unrealized_pnl"`
	DailyRealizedPnL  string          `json:"daily_realized_pnl"`
	Raw               json.RawMessage `json:"-"`
}

// CFMCurrentMarginWindowRequest는 현재 증거금 구간 조회 조건이다.
// MarginProfileType이 비어 있으면 거래소 기본 프로필을 사용한다.
type CFMCurrentMarginWindowRequest struct {
	MarginProfileType MarginProfileType
}

// CFMMarginWindow는 적용 중인 증거금 구간과 종료 시각이다.
type CFMMarginWindow struct {
	MarginWindowType string `json:"margin_window_type"`
	EndTime          string `json:"end_time"`
}

// CFMCurrentMarginWindow는 현재 증거금 구간과 장중 증거금 킬스위치 상태다.
type CFMCurrentMarginWindow struct {
	MarginWindow                                CFMMarginWindow `json:"margin_window"`
	IsIntradayMarginKillswitchEnabled           bool            `json:"is_intraday_margin_killswitch_enabled"`
	IsIntradayMarginEnrollmentKillswitchEnabled bool            `json:"is_intraday_margin_enrollment_killswitch_enabled"`
	Raw                                         json.RawMessage `json:"-"`
}

// SetCFMIntradayMarginSettingRequest는 장중 증거금 설정 변경 요청이다.
type SetCFMIntradayMarginSettingRequest struct {
	Setting IntradayMarginSetting `json:"setting"`
}

// CFMSweep은 CFM 계정에서 현물 USD 지갑으로 이체 중인 sweep 한 건이다.
// PENDING은 취소할 수 있고 PROCESSING은 취소할 수 없다.
type CFMSweep struct {
	ID              string          `json:"id"`
	RequestedAmount Amount          `json:"requested_amount"`
	ShouldSweepAll  bool            `json:"should_sweep_all"`
	Status          string          `json:"status"`
	ScheduledTime   string          `json:"scheduled_time"`
	Raw             json.RawMessage `json:"-"`
}

// ScheduleCFMSweepRequest는 CFM에서 현물 USD 지갑으로의 sweep 예약 요청이다.
// USDAmount가 비어 있으면 거래소가 이체 가능한 잔여 자금 전체를 sweep한다.
type ScheduleCFMSweepRequest struct {
	USDAmount string `json:"usd_amount,omitempty"`
}

// CFMSweepResult는 sweep 예약 또는 취소 요청의 처리 결과다.
// Success가 false이면 거래소가 요청을 적용하지 않은 것이다.
type CFMSweepResult struct {
	Success bool            `json:"success"`
	Raw     json.RawMessage `json:"-"`
}

func (request CFMCurrentMarginWindowRequest) validate() error {
	switch request.MarginProfileType {
	case "", MarginProfileUnspecified, MarginProfileRetailRegular, MarginProfileRetailIntraday1:
		return nil
	default:
		return validationError("unsupported margin profile type %q", request.MarginProfileType)
	}
}

func (request CFMCurrentMarginWindowRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "margin_profile_type", string(request.MarginProfileType))
	return values
}

func (request SetCFMIntradayMarginSettingRequest) validate() error {
	switch request.Setting {
	case IntradayMarginSettingStandard, IntradayMarginSettingIntraday:
		return nil
	default:
		return validationError("intraday margin setting must be STANDARD or INTRADAY")
	}
}

func (request ScheduleCFMSweepRequest) validate() error {
	if request.USDAmount != "" && !isPositiveDecimal(request.USDAmount) {
		return validationError("sweep USD amount must be a positive decimal string")
	}
	return nil
}

// CFMBalanceSummary는 미국 선물(CFM) 계정의 잔고와 증거금 요약을 조회한다.
func (client *Client) CFMBalanceSummary(
	ctx context.Context,
	options ...trade.RequestOption,
) (CFMBalanceSummary, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/cfm/balance_summary", nil, nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return CFMBalanceSummary{}, err
	}
	var envelope struct {
		BalanceSummary json.RawMessage `json:"balance_summary"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return CFMBalanceSummary{}, err
	}
	var summary CFMBalanceSummary
	if err := json.Unmarshal(envelope.BalanceSummary, &summary); err != nil {
		return CFMBalanceSummary{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	summary.Raw = cloneBytes(envelope.BalanceSummary)
	return summary, nil
}

// CFMPositions는 CFM 상품의 포지션 목록을 조회한다.
func (client *Client) CFMPositions(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]CFMPosition, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/cfm/positions", nil, nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var page struct {
		Positions []json.RawMessage `json:"positions"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &page); err != nil {
		return nil, err
	}
	positions := make([]CFMPosition, len(page.Positions))
	for index, raw := range page.Positions {
		if err := json.Unmarshal(raw, &positions[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		positions[index].Raw = cloneBytes(raw)
	}
	return positions, nil
}

// CFMPosition은 상품 ID로 CFM 포지션 한 건을 조회한다.
func (client *Client) CFMPosition(
	ctx context.Context,
	productID string,
	options ...trade.RequestOption,
) (CFMPosition, error) {
	segment, err := escapePathSegment("product ID", productID)
	if err != nil {
		return CFMPosition{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/cfm/positions/"+segment, nil, nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return CFMPosition{}, err
	}
	var envelope struct {
		Position json.RawMessage `json:"position"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return CFMPosition{}, err
	}
	var position CFMPosition
	if err := json.Unmarshal(envelope.Position, &position); err != nil {
		return CFMPosition{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	position.Raw = cloneBytes(envelope.Position)
	return position, nil
}

// CFMCurrentMarginWindow는 현재 적용 중인 증거금 구간을 조회한다.
func (client *Client) CFMCurrentMarginWindow(
	ctx context.Context,
	request CFMCurrentMarginWindowRequest,
	options ...trade.RequestOption,
) (CFMCurrentMarginWindow, error) {
	if err := request.validate(); err != nil {
		return CFMCurrentMarginWindow{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/cfm/intraday/current_margin_window", request.values(), nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return CFMCurrentMarginWindow{}, err
	}
	var window CFMCurrentMarginWindow
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &window); err != nil {
		return CFMCurrentMarginWindow{}, err
	}
	window.Raw = cloneBytes(response.Body)
	return window, nil
}

// CFMIntradayMarginSetting은 현재 장중 증거금 설정을 조회한다.
func (client *Client) CFMIntradayMarginSetting(
	ctx context.Context,
	options ...trade.RequestOption,
) (IntradayMarginSetting, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/cfm/intraday/margin_setting", nil, nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return "", err
	}
	var result struct {
		Setting IntradayMarginSetting `json:"setting"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &result); err != nil {
		return "", err
	}
	return result.Setting, nil
}

// SetCFMIntradayMarginSetting은 장중 증거금 설정을 변경한다.
// 증거금 설정 변경은 PermissionTrade가 필요하고 자동 재시도하지 않는다.
func (client *Client) SetCFMIntradayMarginSetting(
	ctx context.Context,
	request SetCFMIntradayMarginSettingRequest,
	options ...trade.RequestOption,
) error {
	if err := request.validate(); err != nil {
		return err
	}
	body, err := encodeBody(request)
	if err != nil {
		return err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, publicPrefix+"/cfm/intraday/margin_setting", nil, body,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return err
	}
	var result struct{}
	return client.decodeSuccess(response, commonexchange.OperationMutation, &result)
}

// CFMSweeps는 CFM에서 현물 USD 지갑으로 이체 대기 또는 처리 중인 sweep 목록을 조회한다.
func (client *Client) CFMSweeps(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]CFMSweep, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/cfm/sweeps", nil, nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var page struct {
		Sweeps []json.RawMessage `json:"sweeps"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &page); err != nil {
		return nil, err
	}
	sweeps := make([]CFMSweep, len(page.Sweeps))
	for index, raw := range page.Sweeps {
		if err := json.Unmarshal(raw, &sweeps[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		sweeps[index].Raw = cloneBytes(raw)
	}
	return sweeps, nil
}

// ScheduleCFMSweep는 CFM 계정의 USD를 현물 USD 지갑으로 옮기는 sweep을 예약한다.
// 계정 사이 이체이므로 PermissionTransfer가 필요하고 자동 재시도하지 않는다.
// 반환된 Success가 false이면 예약되지 않은 것이다.
func (client *Client) ScheduleCFMSweep(
	ctx context.Context,
	request ScheduleCFMSweepRequest,
	options ...trade.RequestOption,
) (CFMSweepResult, error) {
	if err := request.validate(); err != nil {
		return CFMSweepResult{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return CFMSweepResult{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, publicPrefix+"/cfm/sweeps/schedule", nil, body,
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CFMSweepResult{}, err
	}
	return client.decodeSweepResult(response)
}

// CancelCFMSweep는 대기 중(PENDING)인 sweep을 취소한다. 처리 중인 sweep은 취소할 수 없다.
// 계정 사이 이체 취소이므로 PermissionTransfer가 필요하다.
// 반환된 Success가 false이면 취소되지 않은 것이다.
func (client *Client) CancelCFMSweep(
	ctx context.Context,
	options ...trade.RequestOption,
) (CFMSweepResult, error) {
	response, err := client.executePrivate(
		ctx, http.MethodDelete, publicPrefix+"/cfm/sweeps", nil, nil,
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CFMSweepResult{}, err
	}
	return client.decodeSweepResult(response)
}

func (client *Client) decodeSweepResult(response commonexchange.Response) (CFMSweepResult, error) {
	var result CFMSweepResult
	if err := client.decodeSuccess(response, commonexchange.OperationMutation, &result); err != nil {
		return CFMSweepResult{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}
