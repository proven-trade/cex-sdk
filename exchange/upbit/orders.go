package upbit

import (
	"context"
	"net/http"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// PlaceOrder는 업비트 Spot 주문을 생성한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) PlaceOrder(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	params := request.parameters()
	body, err := encodeOrderBody(params)
	if err != nil {
		return Order{}, validationError("encode order body: %v", err)
	}
	response, err := client.executeSigned(
		ctx, http.MethodPost, "/v1/orders", params, body, orderRate,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Order{}, err
	}
	return client.decodeOrder(response, commonexchange.OperationMutation)
}

// OrderInfo는 UUID 또는 사용자 식별자로 주문 한 건을 조회한다.
func (client *Client) OrderInfo(
	ctx context.Context,
	request OrderInfoRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/order", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Order{}, err
	}
	return client.decodeOrder(response, commonexchange.OperationRead)
}

// CancelOrder는 UUID 또는 사용자 식별자로 주문 취소를 요청한다.
// 성공 응답은 취소 접수이며 최종 상태는 다시 조회해야 한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodDelete, "/v1/order", request.parameters(), nil, defaultRate,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Order{}, err
	}
	return client.decodeOrder(response, commonexchange.OperationMutation)
}

// OpenOrders는 미체결 주문 목록을 조회한다.
func (client *Client) OpenOrders(
	ctx context.Context,
	request OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/orders/open", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(order *Order, raw []byte) {
		order.Raw = raw
	})
}

// ClosedOrders는 완료 또는 취소된 주문 목록을 조회한다.
func (client *Client) ClosedOrders(
	ctx context.Context,
	request ClosedOrdersRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/orders/closed", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(order *Order, raw []byte) {
		order.Raw = raw
	})
}

func (client *Client) decodeOrder(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) (Order, error) {
	var order Order
	if err := client.decodeResponse(response, operation, &order); err != nil {
		return Order{}, err
	}
	order.Raw = cloneBytes(response.Body)
	return order, nil
}
