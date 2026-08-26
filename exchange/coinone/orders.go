package coinone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// PlaceOrder는 코인원 Spot 주문을 생성한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) PlaceOrder(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	response, err := client.executePrivate(
		ctx, "/v2.1/order", request.fields(), true,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	var envelope struct {
		OrderID string `json:"order_id"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &envelope); err != nil {
		return OrderReference{}, err
	}
	if envelope.OrderID == "" {
		return OrderReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Coinone order response has no order ID"),
		)
	}
	return OrderReference{OrderID: envelope.OrderID, Raw: cloneBytes(response.Body)}, nil
}

// OrderInfo는 거래소 주문 ID 또는 사용자 주문 ID로 주문 상세를 조회한다.
func (client *Client) OrderInfo(
	ctx context.Context,
	request OrderInfoRequest,
	options ...trade.RequestOption,
) (OrderDetail, error) {
	if err := request.validate(); err != nil {
		return OrderDetail{}, err
	}
	response, err := client.executePrivate(
		ctx, "/v2.1/order/detail", request.fields(), true,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return OrderDetail{}, err
	}
	var envelope struct {
		Order json.RawMessage `json:"order"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return OrderDetail{}, err
	}
	rawOrder := response.Body
	if len(envelope.Order) > 0 && string(envelope.Order) != "null" {
		rawOrder = envelope.Order
	}
	var result OrderDetail
	if err := json.Unmarshal(rawOrder, &result); err != nil {
		return OrderDetail{}, client.decodeBodyError(
			response, commonexchange.OperationRead, fmt.Errorf("decode Coinone order detail: %w", err),
		)
	}
	if result.OrderID == "" {
		return OrderDetail{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Coinone order detail has no order ID"),
		)
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// CancelOrder는 거래소 주문 ID 또는 사용자 주문 ID로 주문을 취소한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (CancelResult, error) {
	if err := request.validate(); err != nil {
		return CancelResult{}, err
	}
	response, err := client.executePrivate(
		ctx, "/v2.1/order/cancel", request.fields(), true,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelResult{}, err
	}
	var result CancelResult
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &result); err != nil {
		return CancelResult{}, err
	}
	if result.OrderID == "" {
		return CancelResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Coinone cancel response has no order ID"),
		)
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// ActiveOrders는 특정 마켓 또는 전체 마켓의 미종료 주문을 조회한다.
func (client *Client) ActiveOrders(
	ctx context.Context,
	request ActiveOrdersRequest,
	options ...trade.RequestOption,
) ([]ActiveOrder, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, "/v2.1/order/active_orders", request.fields(), true,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		ActiveOrders []json.RawMessage `json:"active_orders"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(client, response, envelope.ActiveOrders, func(item *ActiveOrder, raw []byte) {
		item.Raw = raw
	})
}

// CompletedOrders는 특정 마켓 또는 전체 마켓의 종료 주문 체결을 조회한다.
func (client *Client) CompletedOrders(
	ctx context.Context,
	request CompletedOrdersRequest,
	options ...trade.RequestOption,
) ([]CompletedTrade, error) {
	if err := request.validate(client.now()); err != nil {
		return nil, err
	}
	path := "/v2.1/order/completed_orders"
	if request.AllMarkets {
		path += "/all"
	}
	response, err := client.executePrivate(
		ctx, path, request.fields(), true,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		CompletedOrders []json.RawMessage `json:"completed_orders"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(client, response, envelope.CompletedOrders, func(item *CompletedTrade, raw []byte) {
		item.Raw = raw
	})
}
