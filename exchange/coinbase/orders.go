package coinbase

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
	"github.com/proven-trade/proven-trade-sdk/model"
)

// PlaceOrder는 Spot 시장가 또는 GTC 지정가 주문을 생성한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) PlaceOrder(
	ctx context.Context,
	request PlaceOrderRequest,
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
		ctx, http.MethodPost, publicPrefix+"/orders", nil, body,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
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

// CancelOrders는 최대 100개 주문의 취소를 일괄 접수한다.
// 각 항목의 Success를 확인하고 최종 주문 상태는 별도로 조회해야 한다.
func (client *Client) CancelOrders(
	ctx context.Context,
	request CancelOrdersRequest,
	options ...trade.RequestOption,
) ([]CancelResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, publicPrefix+"/orders/batch_cancel", nil, body,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return nil, err
	}
	var result struct {
		Results []CancelResult `json:"results"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationMutation, &result); err != nil {
		return nil, err
	}
	return result.Results, nil
}

// OrderInfo는 주문 ID로 단일 주문을 조회한다.
func (client *Client) OrderInfo(
	ctx context.Context,
	orderID string,
	options ...trade.RequestOption,
) (Order, error) {
	segment, err := escapePathSegment("order ID", orderID)
	if err != nil {
		return Order{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/orders/historical/"+segment, nil, nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Order{}, err
	}
	var envelope struct {
		Order json.RawMessage `json:"order"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return Order{}, err
	}
	var order Order
	if err := json.Unmarshal(envelope.Order, &order); err != nil {
		return Order{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	order.Raw = cloneBytes(envelope.Order)
	return order, nil
}

// Orders는 cursor 기반 Spot 주문 목록을 조회한다.
func (client *Client) Orders(
	ctx context.Context,
	request OrdersRequest,
	options ...trade.RequestOption,
) (OrderPage, error) {
	if err := request.validate(); err != nil {
		return OrderPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/orders/historical/batch", request.values(), nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return OrderPage{}, err
	}
	var rawPage struct {
		Orders   []json.RawMessage `json:"orders"`
		Sequence string            `json:"sequence"`
		HasNext  bool              `json:"has_next"`
		Cursor   string            `json:"cursor"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &rawPage); err != nil {
		return OrderPage{}, err
	}
	page := OrderPage{
		Orders: make([]Order, len(rawPage.Orders)), Sequence: rawPage.Sequence,
		HasNext: rawPage.HasNext, Cursor: rawPage.Cursor, Raw: cloneBytes(response.Body),
	}
	for index, raw := range rawPage.Orders {
		if err := json.Unmarshal(raw, &page.Orders[index]); err != nil {
			return OrderPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		page.Orders[index].Raw = cloneBytes(raw)
	}
	return page, nil
}

// Fills는 cursor 기반 Spot 체결 목록을 조회한다.
func (client *Client) Fills(
	ctx context.Context,
	request FillsRequest,
	options ...trade.RequestOption,
) (FillPage, error) {
	if err := request.validate(); err != nil {
		return FillPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/orders/historical/fills", request.values(), nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return FillPage{}, err
	}
	var rawPage struct {
		Fills  []json.RawMessage `json:"fills"`
		Cursor string            `json:"cursor"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &rawPage); err != nil {
		return FillPage{}, err
	}
	page := FillPage{
		Fills: make([]Fill, len(rawPage.Fills)), Cursor: rawPage.Cursor, Raw: cloneBytes(response.Body),
	}
	for index, raw := range rawPage.Fills {
		if err := json.Unmarshal(raw, &page.Fills[index]); err != nil {
			return FillPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		page.Fills[index].Raw = cloneBytes(raw)
	}
	return page, nil
}

func (client *Client) apiError(
	response commonexchange.Response,
	category trade.ErrorCategory,
	retryable bool,
	code, message string,
	cause error,
) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeCoinbase, AccountID: accountID,
		RequestID: firstNonEmpty(response.Header.Get("X-Request-ID"), response.Header.Get("Trace-ID")),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		ExchangeCode: code, ExchangeMessage: message, Cause: cause,
	}
}
