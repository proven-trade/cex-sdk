package futures

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// TrailingStopUnit은 trailing stop 최대 이탈 폭의 단위다.
type TrailingStopUnit string

const (
	TrailingStopUnitPercent       TrailingStopUnit = "PERCENT"
	TrailingStopUnitQuoteCurrency TrailingStopUnit = "QUOTE_CURRENCY"
)

// QuantityMode는 주문 수정 시 size를 해석하는 방식이다.
type QuantityMode string

const (
	// QuantityModeAbsolute는 체결분을 포함한 전체 주문 수량을 size로 설정한다.
	QuantityModeAbsolute QuantityMode = "ABSOLUTE"
	// QuantityModeRelative는 현재 미체결 수량을 size로 설정한다(기본값).
	QuantityModeRelative QuantityMode = "RELATIVE"
)

// BatchInstructionKind는 batchorder 지시의 종류다.
type BatchInstructionKind string

const (
	BatchInstructionSend   BatchInstructionKind = "send"
	BatchInstructionEdit   BatchInstructionKind = "edit"
	BatchInstructionCancel BatchInstructionKind = "cancel"
)

// SelfTradeStrategy는 계정 전체 자전거래 매칭 방식이다.
type SelfTradeStrategy string

const (
	SelfTradeRejectTaker      SelfTradeStrategy = "REJECT_TAKER"
	SelfTradeCancelMakerSelf  SelfTradeStrategy = "CANCEL_MAKER_SELF"
	SelfTradeCancelMakerChild SelfTradeStrategy = "CANCEL_MAKER_CHILD"
	SelfTradeCancelMakerAny   SelfTradeStrategy = "CANCEL_MAKER_ANY"
)

// LeverageMarginMode는 최대 레버리지가 적용되는 마진 모드다.
type LeverageMarginMode string

const (
	LeverageMarginIsolated LeverageMarginMode = "isolated"
	LeverageMarginCross    LeverageMarginMode = "cross"
)

// EditOrderRequest는 열린 주문의 수량·가격 수정이다. OrderID와 ClientOrderID 중 하나만 지정한다.
type EditOrderRequest struct {
	OrderID                   string
	ClientOrderID             string
	Size                      string
	LimitPrice                string
	StopPrice                 string
	TrailingStopMaxDeviation  string
	TrailingStopDeviationUnit TrailingStopUnit
	QuantityMode              QuantityMode
	ProcessBefore             *time.Time
}

// EditOrderResult는 주문 수정 접수 결과다.
type EditOrderResult struct {
	OrderID       string          `json:"orderId"`
	ClientOrderID string          `json:"cliOrdId"`
	Status        string          `json:"status"`
	ReceivedTime  string          `json:"receivedTime"`
	OrderEvents   json.RawMessage `json:"orderEvents"`
	Raw           json.RawMessage `json:"-"`
}

// BatchInstruction은 batchorder의 전송·수정·취소 지시 하나다. Kind에 해당하는 필드만 사용한다.
type BatchInstruction struct {
	Kind BatchInstructionKind
	// OrderTag는 send 지시에서 응답과 매핑하기 위한 필수 client 태그다.
	OrderTag      string
	OrderType     OrderType
	Symbol        string
	Side          Side
	Size          string
	LimitPrice    string
	StopPrice     string
	ClientOrderID string
	// TriggerSignal은 stop 주문의 trigger 기준(mark, index, last)이다.
	TriggerSignal             string
	ReduceOnly                bool
	TrailingStopMaxDeviation  string
	TrailingStopDeviationUnit TrailingStopUnit
	// OrderID는 edit/cancel 지시의 대상 주문이다. ClientOrderID로 대체할 수 있다.
	OrderID      string
	QuantityMode QuantityMode
}

// BatchOrderRequest는 최대 500개 지시를 순서대로 처리하는 일괄 주문이다.
type BatchOrderRequest struct {
	Instructions  []BatchInstruction
	ProcessBefore *time.Time
}

// BatchInstructionResult는 batchorder 지시별 결과다. 지시 단위 거절은 Status로 전달되며 호출 오류가 아니다.
type BatchInstructionResult struct {
	Status           string          `json:"status"`
	OrderTag         string          `json:"order_tag"`
	OrderID          string          `json:"order_id"`
	ClientOrderID    string          `json:"cliOrdId"`
	DateTimeReceived string          `json:"dateTimeReceived"`
	OrderEvents      json.RawMessage `json:"orderEvents"`
	Raw              json.RawMessage `json:"-"`
}

// CancelAllOrdersRequest는 전체 또는 단일 심볼의 열린 주문 일괄 취소다.
type CancelAllOrdersRequest struct {
	// Symbol이 비어 있으면 모든 열린 주문을 취소한다.
	Symbol string
}

// CancelledOrder는 일괄 취소된 주문 식별자다.
type CancelledOrder struct {
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"cliOrdId"`
}

// CancelAllOrdersResult는 일괄 취소 접수 결과다.
type CancelAllOrdersResult struct {
	Status          string           `json:"status"`
	CancelOnly      string           `json:"cancelOnly"`
	ReceivedTime    string           `json:"receivedTime"`
	CancelledOrders []CancelledOrder `json:"cancelledOrders"`
	OrderEvents     json.RawMessage  `json:"orderEvents"`
	Raw             json.RawMessage  `json:"-"`
}

// CancelAllOrdersAfterRequest는 dead man's switch 타이머를 설정한다. TimeoutSeconds가 0이면 해제한다.
type CancelAllOrdersAfterRequest struct {
	TimeoutSeconds uint32
}

// CancelAllOrdersAfterStatus는 dead man's switch 상태다.
type CancelAllOrdersAfterStatus struct {
	CurrentTime string          `json:"currentTime"`
	TriggerTime string          `json:"triggerTime"`
	Raw         json.RawMessage `json:"-"`
}

// LeveragePreference는 심볼별 최대 레버리지 설정이다.
type LeveragePreference struct {
	Symbol      string             `json:"symbol"`
	MaxLeverage Decimal            `json:"maxLeverage"`
	MarginMode  LeverageMarginMode `json:"marginMode"`
}

// SetLeveragePreferenceRequest는 심볼의 최대 레버리지와 마진 모드를 설정한다.
type SetLeveragePreferenceRequest struct {
	Symbol string
	// MaxLeverage가 비어 있으면 해당 심볼을 일반 cross 마진으로 되돌린다.
	MaxLeverage string
	// MarginMode가 비어 있으면 서버 기본값(isolated)을 사용한다.
	MarginMode LeverageMarginMode
}

// PnlPreference는 심볼별 PnL 정산 통화 설정이다.
type PnlPreference struct {
	Symbol      string `json:"symbol"`
	PnlCurrency string `json:"pnlCurrency"`
}

// PnlPreferences는 계정 전체 및 심볼별 PnL 정산 통화다.
type PnlPreferences struct {
	AccountWidePnlCurrency string          `json:"accountWidePnlCurrency"`
	Preferences            []PnlPreference `json:"preferences"`
	Raw                    json.RawMessage `json:"-"`
}

// SetPnlPreferenceRequest는 PnL 정산 통화를 설정한다.
type SetPnlPreferenceRequest struct {
	// Symbol이 비어 있으면 계정 전체 설정이다.
	Symbol      string
	PnlCurrency string
}

// DeletePnlPreferenceRequest는 PnL 정산 통화 설정을 제거한다.
type DeletePnlPreferenceRequest struct {
	// Symbol이 비어 있으면 계정 전체 설정을 제거한다.
	Symbol string
}

// EditOrder는 열린 주문의 수량·가격을 수정한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
// Required permission: credential.PermissionTrade.
func (client *Client) EditOrder(
	ctx context.Context,
	request EditOrderRequest,
	options ...trade.RequestOption,
) (EditOrderResult, error) {
	if err := request.validate(); err != nil {
		return EditOrderResult{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, derivativesPrefix+"editorder", request.values(), 10,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return EditOrderResult{}, err
	}
	var envelope struct {
		EditStatus json.RawMessage `json:"editStatus"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationMutation, &envelope); err != nil {
		return EditOrderResult{}, err
	}
	var result EditOrderResult
	if err := json.Unmarshal(envelope.EditStatus, &result); err != nil {
		return EditOrderResult{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	if result.Status != "edited" && result.Status != "filled" {
		return EditOrderResult{}, client.decodeError(
			response, result.Status, result.Status, commonexchange.OperationMutation, nil,
		)
	}
	result.Raw = cloneBytes(envelope.EditStatus)
	return result, nil
}

// BatchOrder는 주문 전송·수정·취소 지시를 한 요청으로 처리한다. 비용은 9 + 지시 수다.
// 호출이 성공해도 지시별 Status를 확인해야 한다.
// Required permission: credential.PermissionTrade.
func (client *Client) BatchOrder(
	ctx context.Context,
	request BatchOrderRequest,
	options ...trade.RequestOption,
) ([]BatchInstructionResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	encoded, err := request.encode()
	if err != nil {
		return nil, err
	}
	values := url.Values{"json": []string{encoded}}
	setProcessBefore(values, request.ProcessBefore)
	response, err := client.executePrivate(
		ctx, http.MethodPost, derivativesPrefix+"batchorder", values, 9+len(request.Instructions),
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		BatchStatus []json.RawMessage `json:"batchStatus"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationMutation, &envelope); err != nil {
		return nil, err
	}
	results, err := decodeRawItems(
		envelope.BatchStatus, func(item *BatchInstructionResult, raw []byte) { item.Raw = raw },
	)
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	return results, nil
}

// CancelAllOrders는 전체 또는 단일 심볼의 열린 주문을 취소한다.
// Required permission: credential.PermissionTrade.
func (client *Client) CancelAllOrders(
	ctx context.Context,
	request CancelAllOrdersRequest,
	options ...trade.RequestOption,
) (CancelAllOrdersResult, error) {
	values := make(url.Values)
	if request.Symbol != "" {
		if err := validateSymbol(request.Symbol); err != nil {
			return CancelAllOrdersResult{}, err
		}
		values.Set("symbol", request.Symbol)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, derivativesPrefix+"cancelallorders", values, 25,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelAllOrdersResult{}, err
	}
	var envelope struct {
		CancelStatus json.RawMessage `json:"cancelStatus"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationMutation, &envelope); err != nil {
		return CancelAllOrdersResult{}, err
	}
	var result CancelAllOrdersResult
	if err := json.Unmarshal(envelope.CancelStatus, &result); err != nil {
		return CancelAllOrdersResult{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	if result.Status != "cancelled" && result.Status != "noOrdersToCancel" {
		return CancelAllOrdersResult{}, client.decodeError(
			response, result.Status, result.Status, commonexchange.OperationMutation, nil,
		)
	}
	result.Raw = cloneBytes(envelope.CancelStatus)
	return result, nil
}

// CancelAllOrdersAfter는 dead man's switch를 설정·갱신·해제한다.
// 권장 사용은 15~20초마다 timeout 60초로 호출하는 것이다.
// Required permission: credential.PermissionTrade.
func (client *Client) CancelAllOrdersAfter(
	ctx context.Context,
	request CancelAllOrdersAfterRequest,
	options ...trade.RequestOption,
) (CancelAllOrdersAfterStatus, error) {
	values := url.Values{"timeout": []string{strconv.FormatUint(uint64(request.TimeoutSeconds), 10)}}
	response, err := client.executePrivate(
		ctx, http.MethodPost, derivativesPrefix+"cancelallordersafter", values, 25,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelAllOrdersAfterStatus{}, err
	}
	var envelope struct {
		Status json.RawMessage `json:"status"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationMutation, &envelope); err != nil {
		return CancelAllOrdersAfterStatus{}, err
	}
	var status CancelAllOrdersAfterStatus
	if err := json.Unmarshal(envelope.Status, &status); err != nil {
		return CancelAllOrdersAfterStatus{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	status.Raw = cloneBytes(envelope.Status)
	return status, nil
}

// SelfTradeStrategy는 계정 전체 자전거래 매칭 방식을 조회한다.
// Required permission: credential.PermissionRead.
func (client *Client) SelfTradeStrategy(
	ctx context.Context,
	options ...trade.RequestOption,
) (SelfTradeStrategy, error) {
	var envelope struct {
		Strategy SelfTradeStrategy `json:"strategy"`
	}
	if _, err := client.privateRead(
		ctx, http.MethodGet, derivativesPrefix+"self-trade-strategy", nil, 2, &envelope, options...,
	); err != nil {
		return "", err
	}
	return envelope.Strategy, nil
}

// SetSelfTradeStrategy는 계정 전체 자전거래 매칭 방식을 변경하고 저장된 값을 반환한다.
// Required permission: credential.PermissionTrade.
func (client *Client) SetSelfTradeStrategy(
	ctx context.Context,
	strategy SelfTradeStrategy,
	options ...trade.RequestOption,
) (SelfTradeStrategy, error) {
	switch strategy {
	case SelfTradeRejectTaker, SelfTradeCancelMakerSelf, SelfTradeCancelMakerChild, SelfTradeCancelMakerAny:
	default:
		return "", validationError("unsupported self trade strategy %q", strategy)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPut, derivativesPrefix+"self-trade-strategy",
		url.Values{"strategy": []string{string(strategy)}}, 2,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return "", err
	}
	var envelope struct {
		Strategy SelfTradeStrategy `json:"strategy"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationMutation, &envelope); err != nil {
		return "", err
	}
	return envelope.Strategy, nil
}

// LeveragePreferences는 설정된 심볼별 최대 레버리지를 조회한다.
// Required permission: credential.PermissionRead.
func (client *Client) LeveragePreferences(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]LeveragePreference, error) {
	var envelope struct {
		LeveragePreferences []LeveragePreference `json:"leveragePreferences"`
	}
	if _, err := client.privateRead(
		ctx, http.MethodGet, derivativesPrefix+"leveragepreferences", nil, 2, &envelope, options...,
	); err != nil {
		return nil, err
	}
	return envelope.LeveragePreferences, nil
}

// SetLeveragePreference는 심볼의 최대 레버리지와 마진 모드를 설정한다.
// Required permission: credential.PermissionTrade.
func (client *Client) SetLeveragePreference(
	ctx context.Context,
	request SetLeveragePreferenceRequest,
	options ...trade.RequestOption,
) (Ack, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return Ack{}, err
	}
	values := url.Values{"symbol": []string{request.Symbol}}
	if request.MaxLeverage != "" {
		if err := validatePositiveDecimal("maxLeverage", request.MaxLeverage); err != nil {
			return Ack{}, err
		}
		values.Set("maxLeverage", request.MaxLeverage)
	}
	switch request.MarginMode {
	case "":
	case LeverageMarginIsolated, LeverageMarginCross:
		values.Set("marginMode", string(request.MarginMode))
	default:
		return Ack{}, validationError("unsupported margin mode %q", request.MarginMode)
	}
	return client.privateAck(
		ctx, http.MethodPut, derivativesPrefix+"leveragepreferences", values, 10,
		credential.PermissionTrade, options...,
	)
}

// PnlPreferences는 계정 전체 및 심볼별 PnL 정산 통화를 조회한다.
// Required permission: credential.PermissionRead.
func (client *Client) PnlPreferences(
	ctx context.Context,
	options ...trade.RequestOption,
) (PnlPreferences, error) {
	var preferences PnlPreferences
	response, err := client.privateRead(
		ctx, http.MethodGet, derivativesPrefix+"pnlpreferences", nil, 2, &preferences, options...,
	)
	if err != nil {
		return PnlPreferences{}, err
	}
	preferences.Raw = cloneBytes(response.Body)
	return preferences, nil
}

// SetPnlPreference는 PnL 정산 통화를 설정한다.
// Required permission: credential.PermissionTrade.
func (client *Client) SetPnlPreference(
	ctx context.Context,
	request SetPnlPreferenceRequest,
	options ...trade.RequestOption,
) (Ack, error) {
	if err := validateIdentifier("pnlPreference", request.PnlCurrency); err != nil {
		return Ack{}, err
	}
	values := url.Values{"pnlPreference": []string{request.PnlCurrency}}
	if request.Symbol != "" {
		if err := validateSymbol(request.Symbol); err != nil {
			return Ack{}, err
		}
		values.Set("symbol", request.Symbol)
	}
	return client.privateAck(
		ctx, http.MethodPut, derivativesPrefix+"pnlpreferences", values, 10,
		credential.PermissionTrade, options...,
	)
}

// DeletePnlPreference는 PnL 정산 통화 설정을 제거한다.
// Required permission: credential.PermissionTrade.
func (client *Client) DeletePnlPreference(
	ctx context.Context,
	request DeletePnlPreferenceRequest,
	options ...trade.RequestOption,
) (Ack, error) {
	values := make(url.Values)
	if request.Symbol != "" {
		if err := validateSymbol(request.Symbol); err != nil {
			return Ack{}, err
		}
		values.Set("symbol", request.Symbol)
	}
	return client.privateAck(
		ctx, http.MethodDelete, derivativesPrefix+"pnlpreferences", values, 10,
		credential.PermissionTrade, options...,
	)
}

func (request EditOrderRequest) validate() error {
	if err := validateOrderIdentity(request.OrderID, request.ClientOrderID); err != nil {
		return err
	}
	if err := validateEditFields(
		request.Size, request.LimitPrice, request.StopPrice,
		request.TrailingStopMaxDeviation, request.TrailingStopDeviationUnit, request.QuantityMode,
	); err != nil {
		return err
	}
	return validateProcessBefore(request.ProcessBefore)
}

func (request EditOrderRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "orderId", request.OrderID)
	setIfNotEmpty(values, "cliOrdId", request.ClientOrderID)
	setIfNotEmpty(values, "size", request.Size)
	setIfNotEmpty(values, "limitPrice", request.LimitPrice)
	setIfNotEmpty(values, "stopPrice", request.StopPrice)
	setIfNotEmpty(values, "trailingStopMaxDeviation", request.TrailingStopMaxDeviation)
	setIfNotEmpty(values, "trailingStopDeviationUnit", string(request.TrailingStopDeviationUnit))
	setIfNotEmpty(values, "qtyMode", string(request.QuantityMode))
	setProcessBefore(values, request.ProcessBefore)
	return values
}

func validateEditFields(
	size, limitPrice, stopPrice, deviation string,
	unit TrailingStopUnit,
	mode QuantityMode,
) error {
	if size == "" && limitPrice == "" && stopPrice == "" && deviation == "" {
		return validationError("edit requires size, limitPrice, stopPrice or trailingStopMaxDeviation")
	}
	for _, field := range []struct{ name, value string }{
		{"size", size}, {"limitPrice", limitPrice}, {"stopPrice", stopPrice},
		{"trailingStopMaxDeviation", deviation},
	} {
		if field.value == "" {
			continue
		}
		if err := validatePositiveDecimal(field.name, field.value); err != nil {
			return err
		}
	}
	switch unit {
	case "", TrailingStopUnitPercent, TrailingStopUnitQuoteCurrency:
	default:
		return validationError("unsupported trailing stop deviation unit %q", unit)
	}
	switch mode {
	case "", QuantityModeAbsolute, QuantityModeRelative:
	default:
		return validationError("unsupported quantity mode %q", mode)
	}
	return nil
}

func (request BatchOrderRequest) validate() error {
	if len(request.Instructions) == 0 || len(request.Instructions) > 500 {
		return validationError("batchorder requires between 1 and 500 instructions")
	}
	for index, instruction := range request.Instructions {
		if err := instruction.validate(); err != nil {
			var cause error = err
			var apiError *trade.APIError
			if errors.As(err, &apiError) && apiError.Cause != nil {
				cause = apiError.Cause
			}
			return validationError("batch instruction %d: %v", index, cause)
		}
	}
	return validateProcessBefore(request.ProcessBefore)
}

func (instruction BatchInstruction) validate() error {
	switch instruction.Kind {
	case BatchInstructionSend:
		return instruction.validateSend()
	case BatchInstructionEdit:
		if err := validateOrderIdentity(instruction.OrderID, instruction.ClientOrderID); err != nil {
			return err
		}
		return validateEditFields(
			instruction.Size, instruction.LimitPrice, instruction.StopPrice,
			instruction.TrailingStopMaxDeviation, instruction.TrailingStopDeviationUnit, instruction.QuantityMode,
		)
	case BatchInstructionCancel:
		return validateOrderIdentity(instruction.OrderID, instruction.ClientOrderID)
	default:
		return validationError("unsupported batch instruction kind %q", instruction.Kind)
	}
}

func (instruction BatchInstruction) validateSend() error {
	if err := validateIdentifier("order_tag", instruction.OrderTag); err != nil {
		return err
	}
	if !instruction.OrderType.valid() && instruction.OrderType != OrderTypeStop {
		return validationError("unsupported order type %q", instruction.OrderType)
	}
	if err := validateSymbol(instruction.Symbol); err != nil {
		return err
	}
	if instruction.Side != SideBuy && instruction.Side != SideSell {
		return validationError("side must be buy or sell")
	}
	if err := validatePositiveDecimal("size", instruction.Size); err != nil {
		return err
	}
	switch instruction.OrderType {
	case OrderTypeMarket:
		if instruction.LimitPrice != "" {
			return validationError("market order does not accept limitPrice")
		}
	case OrderTypeStop:
		if err := validatePositiveDecimal("stopPrice", instruction.StopPrice); err != nil {
			return err
		}
		if instruction.LimitPrice != "" {
			if err := validatePositiveDecimal("limitPrice", instruction.LimitPrice); err != nil {
				return err
			}
		}
	default:
		if err := validatePositiveDecimal("limitPrice", instruction.LimitPrice); err != nil {
			return err
		}
	}
	if instruction.OrderType != OrderTypeStop && instruction.StopPrice != "" {
		return validationError("stopPrice is only valid for stp orders")
	}
	if instruction.ClientOrderID != "" {
		if err := validateIdentifier("client order ID", instruction.ClientOrderID); err != nil {
			return err
		}
	}
	switch instruction.TriggerSignal {
	case "", "mark", "index", "last":
	default:
		return validationError("unsupported trigger signal %q", instruction.TriggerSignal)
	}
	return nil
}

type batchWireInstruction struct {
	Order                     string           `json:"order"`
	OrderTag                  string           `json:"order_tag,omitempty"`
	OrderType                 OrderType        `json:"orderType,omitempty"`
	Symbol                    string           `json:"symbol,omitempty"`
	Side                      Side             `json:"side,omitempty"`
	OrderID                   string           `json:"order_id,omitempty"`
	ClientOrderID             string           `json:"cliOrdId,omitempty"`
	Size                      json.Number      `json:"size,omitempty"`
	LimitPrice                json.Number      `json:"limitPrice,omitempty"`
	StopPrice                 json.Number      `json:"stopPrice,omitempty"`
	TriggerSignal             string           `json:"triggerSignal,omitempty"`
	ReduceOnly                bool             `json:"reduceOnly,omitempty"`
	TrailingStopMaxDeviation  json.Number      `json:"trailingStopMaxDeviation,omitempty"`
	TrailingStopDeviationUnit TrailingStopUnit `json:"trailingStopDeviationUnit,omitempty"`
	QuantityMode              QuantityMode     `json:"qtyMode,omitempty"`
}

func (request BatchOrderRequest) encode() (string, error) {
	wire := make([]batchWireInstruction, len(request.Instructions))
	for index, instruction := range request.Instructions {
		item := batchWireInstruction{
			Order: string(instruction.Kind), OrderID: instruction.OrderID,
			ClientOrderID: instruction.ClientOrderID,
		}
		if instruction.Kind != BatchInstructionCancel {
			item.Size = json.Number(instruction.Size)
			item.LimitPrice = json.Number(instruction.LimitPrice)
			item.StopPrice = json.Number(instruction.StopPrice)
			item.TrailingStopMaxDeviation = json.Number(instruction.TrailingStopMaxDeviation)
			item.TrailingStopDeviationUnit = instruction.TrailingStopDeviationUnit
		}
		switch instruction.Kind {
		case BatchInstructionSend:
			item.OrderTag, item.OrderType = instruction.OrderTag, instruction.OrderType
			item.Symbol, item.Side = instruction.Symbol, instruction.Side
			item.TriggerSignal, item.ReduceOnly = instruction.TriggerSignal, instruction.ReduceOnly
		case BatchInstructionEdit:
			item.QuantityMode = instruction.QuantityMode
		}
		wire[index] = item
	}
	encoded, err := json.Marshal(struct {
		BatchOrder []batchWireInstruction `json:"batchOrder"`
	}{BatchOrder: wire})
	if err != nil {
		return "", validationError("encode batchorder: %v", err)
	}
	return string(encoded), nil
}
