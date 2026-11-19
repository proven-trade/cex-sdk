package coinbase

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// MarginType은 선물 주문의 증거금 방식이다.
type MarginType string

const (
	MarginTypeCross    MarginType = "CROSS"
	MarginTypeIsolated MarginType = "ISOLATED"
)

// EditOrderRequest는 미체결 주문의 가격·수량 정정 요청이다.
// Price와 Size는 정정 후 값이며 공식 API가 둘 다 요구한다.
// StopPrice는 stop 계열 주문, CancelAttachedOrder는 TP/SL 부착 주문에만 적용된다.
type EditOrderRequest struct {
	OrderID             string `json:"order_id"`
	Price               string `json:"price"`
	Size                string `json:"size"`
	StopPrice           string `json:"stop_price,omitempty"`
	AverageEntryPrice   string `json:"average_entry_price,omitempty"`
	CancelAttachedOrder bool   `json:"cancel_attached_order,omitempty"`
}

// EditOrderError는 정정 요청이 거부된 사유 한 건이다.
type EditOrderError struct {
	EditFailureReason    string `json:"edit_failure_reason"`
	PreviewFailureReason string `json:"preview_failure_reason"`
}

// EditOrderResult는 주문 정정 접수 결과다.
type EditOrderResult struct {
	Success bool
	Errors  []EditOrderError
	Raw     json.RawMessage
}

// CommissionDetailTotal은 주문 수수료의 항목별 내역이다.
type CommissionDetailTotal struct {
	TotalCommission       string `json:"total_commission"`
	GSTCommission         string `json:"gst_commission"`
	WithholdingCommission string `json:"withholding_commission"`
	ClientCommission      string `json:"client_commission"`
	VenueCommission       string `json:"venue_commission"`
	RegulatoryCommission  string `json:"regulatory_commission"`
	ClearingCommission    string `json:"clearing_commission"`
}

// EditOrderPreview는 정정을 반영했을 때의 예상 비용이다.
// pnl_configuration 같은 중첩 필드는 Raw에서 읽는다.
type EditOrderPreview struct {
	Errors             []EditOrderError      `json:"errors"`
	Slippage           string                `json:"slippage"`
	OrderTotal         string                `json:"order_total"`
	CommissionTotal    string                `json:"commission_total"`
	QuoteSize          string                `json:"quote_size"`
	BaseSize           string                `json:"base_size"`
	BestBid            string                `json:"best_bid"`
	BestAsk            string                `json:"best_ask"`
	AverageFilledPrice string                `json:"average_filled_price"`
	OrderMarginTotal   string                `json:"order_margin_total"`
	CommissionDetail   CommissionDetailTotal `json:"commission_detail_total"`
	Raw                json.RawMessage       `json:"-"`
}

// PreviewOrderRequest는 주문 접수 전 예상 비용 조회 요청이다.
// Leverage와 MarginType은 선물 상품에만 적용된다.
type PreviewOrderRequest struct {
	ProductID          string             `json:"product_id"`
	Side               Side               `json:"side"`
	OrderConfiguration OrderConfiguration `json:"order_configuration"`
	Leverage           string             `json:"leverage,omitempty"`
	MarginType         MarginType         `json:"margin_type,omitempty"`
}

// MarginRatioData는 주문 전후의 증거금 비율이다.
type MarginRatioData struct {
	CurrentMarginRatio   string `json:"current_margin_ratio"`
	ProjectedMarginRatio string `json:"projected_margin_ratio"`
}

// OrderPreview는 신규 주문 미리보기 결과다.
// Errs는 주문을 그대로 접수했을 때 발생할 수 있는 거부 사유이며 HTTP 오류가 아니다.
type OrderPreview struct {
	OrderTotal                     string                `json:"order_total"`
	CommissionTotal                string                `json:"commission_total"`
	Errs                           []string              `json:"errs"`
	Warning                        []string              `json:"warning"`
	QuoteSize                      string                `json:"quote_size"`
	BaseSize                       string                `json:"base_size"`
	BestBid                        string                `json:"best_bid"`
	BestAsk                        string                `json:"best_ask"`
	IsMax                          bool                  `json:"is_max"`
	OrderMarginTotal               string                `json:"order_margin_total"`
	Leverage                       string                `json:"leverage"`
	LongLeverage                   string                `json:"long_leverage"`
	ShortLeverage                  string                `json:"short_leverage"`
	Slippage                       string                `json:"slippage"`
	PreviewID                      string                `json:"preview_id"`
	CurrentLiquidationBuffer       string                `json:"current_liquidation_buffer"`
	ProjectedLiquidationBuffer     string                `json:"projected_liquidation_buffer"`
	MaxLeverage                    string                `json:"max_leverage"`
	PositionNotionalLimit          string                `json:"position_notional_limit"`
	MaxNotionalAtRequestedLeverage string                `json:"max_notional_at_requested_leverage"`
	MarginRatioData                MarginRatioData       `json:"margin_ratio_data"`
	CommissionDetail               CommissionDetailTotal `json:"commission_detail_total"`
	EstimatedAverageFilledPrice    string                `json:"est_average_filled_price"`
	PredictedLiquidationPrice      string                `json:"predicted_liquidation_price"`
	AvailableBuyingPower           string                `json:"available_buying_power"`
	MaxLoss                        string                `json:"max_loss"`
	Raw                            json.RawMessage       `json:"-"`
}

// ClosePositionRequest는 상품의 미결제 포지션을 청산하는 주문 요청이다.
// Size가 비어 있으면 거래소가 해당 상품의 전체 포지션을 청산한다.
type ClosePositionRequest struct {
	ClientOrderID string `json:"client_order_id"`
	ProductID     string `json:"product_id"`
	Size          string `json:"size,omitempty"`
}

func (request EditOrderRequest) validate() error {
	if err := validateRequiredText("order ID", request.OrderID); err != nil {
		return err
	}
	if !isPositiveDecimal(request.Price) || !isPositiveDecimal(request.Size) {
		return validationError("edit order price and size must be positive decimal strings")
	}
	if (request.StopPrice != "" && !isPositiveDecimal(request.StopPrice)) ||
		(request.AverageEntryPrice != "" && !isPositiveDecimal(request.AverageEntryPrice)) {
		return validationError("edit order stop price and average entry price must be positive decimal strings")
	}
	return nil
}

func (request PreviewOrderRequest) validate() error {
	if err := validateRequiredText("product ID", request.ProductID); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("order side must be BUY or SELL")
	}
	if request.Leverage != "" && !isPositiveDecimal(request.Leverage) {
		return validationError("leverage must be a positive decimal string")
	}
	if request.MarginType != "" && request.MarginType != MarginTypeCross &&
		request.MarginType != MarginTypeIsolated {
		return validationError("margin type must be CROSS, ISOLATED, or empty")
	}
	return request.OrderConfiguration.validate()
}

func (request ClosePositionRequest) validate() error {
	if err := validateRequiredText("client order ID", request.ClientOrderID); err != nil {
		return err
	}
	if len(request.ClientOrderID) > 64 {
		return validationError("client order ID cannot exceed 64 bytes")
	}
	if err := validateRequiredText("product ID", request.ProductID); err != nil {
		return err
	}
	if request.Size != "" && !isPositiveDecimal(request.Size) {
		return validationError("close position size must be a positive decimal string")
	}
	return nil
}

// EditOrder는 미체결 주문의 가격과 수량을 정정한다.
// 거래소가 정정을 거부하면 거부 사유를 담은 오류를 반환하며 자동 재시도하지 않는다.
// 전송 결과가 불명확하면 UNKNOWN_EXECUTION_STATE로 반환하므로 OrderInfo로 상태를 확인해야 한다.
func (client *Client) EditOrder(
	ctx context.Context,
	request EditOrderRequest,
	options ...trade.RequestOption,
) (EditOrderResult, error) {
	if err := request.validate(); err != nil {
		return EditOrderResult{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return EditOrderResult{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, publicPrefix+"/orders/edit", nil, body,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return EditOrderResult{}, err
	}
	var result struct {
		Success bool             `json:"success"`
		Errors  []EditOrderError `json:"errors"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationMutation, &result); err != nil {
		return EditOrderResult{}, err
	}
	if !result.Success {
		reasons := make([]string, 0, len(result.Errors))
		for _, item := range result.Errors {
			if reason := firstNonEmpty(item.EditFailureReason, item.PreviewFailureReason); reason != "" {
				reasons = append(reasons, reason)
			}
		}
		code := ""
		var cause error
		if len(reasons) > 0 {
			code = reasons[0]
		} else {
			cause = errors.New("Coinbase order edit was rejected without a failure reason")
		}
		message := strings.Join(reasons, ",")
		category, retryable := classifyError(response.StatusCode, code, message, commonexchange.OperationMutation)
		return EditOrderResult{}, client.apiError(response, category, retryable, code, message, cause)
	}
	return EditOrderResult{Success: true, Errors: result.Errors, Raw: cloneBytes(response.Body)}, nil
}

// PreviewEditOrder는 주문을 정정하지 않고 정정 반영 시의 예상 비용을 조회한다.
// 주문 상태를 바꾸지 않으므로 읽기 요청으로 처리하며 PermissionRead가 필요하다.
func (client *Client) PreviewEditOrder(
	ctx context.Context,
	request EditOrderRequest,
	options ...trade.RequestOption,
) (EditOrderPreview, error) {
	if err := request.validate(); err != nil {
		return EditOrderPreview{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return EditOrderPreview{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, publicPrefix+"/orders/edit_preview", nil, body,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return EditOrderPreview{}, err
	}
	var preview EditOrderPreview
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &preview); err != nil {
		return EditOrderPreview{}, err
	}
	preview.Raw = cloneBytes(response.Body)
	return preview, nil
}

// PreviewOrder는 주문을 접수하지 않고 예상 비용과 거부 사유를 조회한다.
// 주문 상태를 바꾸지 않으므로 읽기 요청으로 처리하며 PermissionRead가 필요하다.
func (client *Client) PreviewOrder(
	ctx context.Context,
	request PreviewOrderRequest,
	options ...trade.RequestOption,
) (OrderPreview, error) {
	if err := request.validate(); err != nil {
		return OrderPreview{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return OrderPreview{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, publicPrefix+"/orders/preview", nil, body,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return OrderPreview{}, err
	}
	var preview OrderPreview
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &preview); err != nil {
		return OrderPreview{}, err
	}
	preview.Raw = cloneBytes(response.Body)
	return preview, nil
}

// ClosePosition은 상품의 미결제 포지션을 청산하는 주문을 접수한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) ClosePosition(
	ctx context.Context,
	request ClosePositionRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return OrderReference{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, publicPrefix+"/orders/close_position", nil, body,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	return client.decodeOrderAcknowledgement(response)
}

// decodeOrderAcknowledgement는 success_response에서 주문 참조를 꺼내고 거부 응답을 오류로 바꾼다.
func (client *Client) decodeOrderAcknowledgement(response commonexchange.Response) (OrderReference, error) {
	var envelope struct {
		Success         bool            `json:"success"`
		SuccessResponse json.RawMessage `json:"success_response"`
		ErrorResponse   struct {
			Error                 string `json:"error"`
			Message               string `json:"message"`
			ErrorDetails          string `json:"error_details"`
			NewOrderFailureReason string `json:"new_order_failure_reason"`
			PreviewFailureReason  string `json:"preview_failure_reason"`
		} `json:"error_response"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationMutation, &envelope); err != nil {
		return OrderReference{}, err
	}
	if !envelope.Success {
		code := firstNonEmpty(
			envelope.ErrorResponse.NewOrderFailureReason,
			envelope.ErrorResponse.PreviewFailureReason,
			envelope.ErrorResponse.Error,
		)
		message := firstNonEmpty(envelope.ErrorResponse.Message, envelope.ErrorResponse.ErrorDetails)
		category, retryable := classifyError(response.StatusCode, code, message, commonexchange.OperationMutation)
		return OrderReference{}, client.apiError(response, category, retryable, code, message, nil)
	}
	var reference OrderReference
	if err := json.Unmarshal(envelope.SuccessResponse, &reference); err != nil {
		return OrderReference{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	if reference.OrderID == "" {
		return OrderReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Coinbase order acknowledgement is empty"),
		)
	}
	reference.Raw = cloneBytes(envelope.SuccessResponse)
	return reference, nil
}
