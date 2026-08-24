package bithumb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// PlaceOrder는 빗썸 v2 Spot 주문을 생성한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) PlaceOrder(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	params := request.parameters()
	body, err := encodeOrderBody(params)
	if err != nil {
		return OrderReference{}, validationError("encode order body: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v2/orders", params, body, true,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	var result OrderReference
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &result); err != nil {
		return OrderReference{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// OrderInfo는 UUID 또는 사용자 주문 ID로 v1 주문 상세를 조회한다.
func (client *Client) OrderInfo(
	ctx context.Context,
	request OrderInfoRequest,
	options ...trade.RequestOption,
) (OrderDetail, error) {
	if err := request.validate(); err != nil {
		return OrderDetail{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/order", request.parameters(), nil, true,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return OrderDetail{}, err
	}
	var result OrderDetail
	if err := client.decodeResponse(response, commonexchange.OperationRead, &result); err != nil {
		return OrderDetail{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// CancelOrder는 거래소 주문 ID 또는 사용자 주문 ID로 v2 주문 취소를 요청한다.
// 성공 응답은 취소 접수이며 최종 상태는 다시 조회해야 한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (CancelResult, error) {
	if err := request.validate(); err != nil {
		return CancelResult{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, "/v2/order", request.parameters(), nil, true,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelResult{}, err
	}
	var result CancelResult
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &result); err != nil {
		return CancelResult{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// PendingOrders는 v2 미체결 주문 목록과 다음 페이지 cursor를 조회한다.
func (client *Client) PendingOrders(
	ctx context.Context,
	request PendingOrdersRequest,
	options ...trade.RequestOption,
) (OrderPage, error) {
	if err := request.validate(); err != nil {
		return OrderPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/orders/pending", request.parameters(), nil, true,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return OrderPage{}, err
	}
	return client.decodeOrderPage(response)
}

// OrderHistory는 v2 완료·취소 주문 이력과 다음 페이지 cursor를 조회한다.
func (client *Client) OrderHistory(
	ctx context.Context,
	request OrderHistoryRequest,
	options ...trade.RequestOption,
) (OrderPage, error) {
	if err := request.validate(); err != nil {
		return OrderPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/orders/history", request.parameters(), nil, true,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return OrderPage{}, err
	}
	return client.decodeOrderPage(response)
}

func (client *Client) decodeOrderPage(response commonexchange.Response) (OrderPage, error) {
	var envelope struct {
		Data    []json.RawMessage `json:"data"`
		HasNext bool              `json:"has_next"`
		NextKey string            `json:"next_key"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return OrderPage{}, err
	}
	page := OrderPage{
		Data: make([]OrderSummary, len(envelope.Data)), HasNext: envelope.HasNext,
		NextKey: envelope.NextKey, Raw: cloneBytes(response.Body),
	}
	for index, rawItem := range envelope.Data {
		if err := json.Unmarshal(rawItem, &page.Data[index]); err != nil {
			return OrderPage{}, client.decodeBodyError(
				response, commonexchange.OperationRead,
				fmt.Errorf("decode Bithumb order page item: %w", err),
			)
		}
		page.Data[index].Raw = cloneBytes(rawItem)
	}
	return page, nil
}
