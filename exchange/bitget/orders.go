package bitget

import (
	"context"
	"encoding/json"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// PlaceOrder는 Spot 또는 USDT-M Futures 주문을 생성한다.
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
	response, _, err := client.executeSigned(
		ctx,
		http.MethodPost,
		"/api/v3/trade/place-order",
		nil,
		body,
		accountLimit(10),
		credential.PermissionTrade,
		commonexchange.OperationMutation,
		options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationMutation)
	if err != nil {
		return OrderReference{}, err
	}
	reference, err := decodeOrderReference(data)
	if err != nil {
		return OrderReference{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	return reference, nil
}

// CancelOrder는 주문 취소를 접수한다.
// 성공 응답은 취소 접수이며 최종 상태는 주문 조회 또는 private stream으로 확인해야 한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return OrderReference{}, err
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodPost,
		"/api/v3/trade/cancel-order",
		nil,
		body,
		accountLimit(10),
		credential.PermissionTrade,
		commonexchange.OperationMutation,
		options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationMutation)
	if err != nil {
		return OrderReference{}, err
	}
	reference, err := decodeOrderReference(data)
	if err != nil {
		return OrderReference{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	return reference, nil
}

// OrderInfo는 주문 ID 또는 client order ID로 단건 주문을 조회한다.
func (client *Client) OrderInfo(
	ctx context.Context,
	request OrderInfoRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodGet,
		"/api/v3/trade/order-info",
		request.values(),
		nil,
		accountLimit(20),
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return Order{}, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return Order{}, err
	}
	var order Order
	if err := decodeData(data, &order); err != nil {
		return Order{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	order.Raw = cloneBytes(data)
	return order, nil
}

// OpenOrders는 미체결 또는 부분 체결 주문을 cursor 기반으로 조회한다.
func (client *Client) OpenOrders(
	ctx context.Context,
	request OpenOrdersRequest,
	options ...trade.RequestOption,
) (OrderPage, error) {
	if err := request.validate(); err != nil {
		return OrderPage{}, err
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodGet,
		"/api/v3/trade/unfilled-orders",
		request.values(),
		nil,
		accountLimit(20),
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return OrderPage{}, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return OrderPage{}, err
	}
	page, err := decodeOrderPage(data)
	if err != nil {
		return OrderPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return page, nil
}

// OrderHistory는 최근 90일 안의 주문 이력을 cursor 기반으로 조회한다.
func (client *Client) OrderHistory(
	ctx context.Context,
	request OrderHistoryRequest,
	options ...trade.RequestOption,
) (OrderPage, error) {
	if err := request.validate(); err != nil {
		return OrderPage{}, err
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodGet,
		"/api/v3/trade/history-orders",
		request.values(),
		nil,
		accountLimit(20),
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return OrderPage{}, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return OrderPage{}, err
	}
	page, err := decodeOrderPage(data)
	if err != nil {
		return OrderPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return page, nil
}

func decodeOrderReference(data json.RawMessage) (OrderReference, error) {
	var reference OrderReference
	if err := decodeData(data, &reference); err != nil {
		return OrderReference{}, err
	}
	reference.Raw = cloneBytes(data)
	return reference, nil
}

func decodeOrderPage(data json.RawMessage) (OrderPage, error) {
	var rawPage struct {
		Orders []json.RawMessage `json:"list"`
		Cursor string            `json:"cursor"`
	}
	if err := decodeData(data, &rawPage); err != nil {
		return OrderPage{}, err
	}
	page := OrderPage{
		Orders: make([]Order, len(rawPage.Orders)),
		Cursor: rawPage.Cursor,
		Raw:    cloneBytes(data),
	}
	for index, rawOrder := range rawPage.Orders {
		if err := decodeData(rawOrder, &page.Orders[index]); err != nil {
			return OrderPage{}, err
		}
		page.Orders[index].Raw = cloneBytes(rawOrder)
	}
	return page, nil
}
