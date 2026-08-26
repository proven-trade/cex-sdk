package bybit

import (
	"context"
	"encoding/json"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// PlaceOrder는 Spot 또는 Linear 주문을 생성한다.
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
		"/v5/order/create",
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
	result, _, err := client.responseResult(response, commonexchange.OperationMutation)
	if err != nil {
		return OrderReference{}, err
	}
	return decodeOrderReference(response, result, commonexchange.OperationMutation, client)
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
		"/v5/order/cancel",
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
	result, _, err := client.responseResult(response, commonexchange.OperationMutation)
	if err != nil {
		return OrderReference{}, err
	}
	return decodeOrderReference(response, result, commonexchange.OperationMutation, client)
}

// OrderInfo는 주문 ID 또는 주문 연결 ID로 단건 주문을 조회한다.
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
		"/v5/order/realtime",
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
	result, _, err := client.responseResult(response, commonexchange.OperationRead)
	if err != nil {
		return Order{}, err
	}
	page, err := client.decodeOrderPage(response, result)
	if err != nil {
		return Order{}, err
	}
	if len(page.Orders) == 0 {
		return Order{}, client.decodeError(
			response, "110001", "order not found", commonexchange.OperationRead, nil,
		)
	}
	return page.Orders[0], nil
}

// OpenOrders는 미체결 주문을 cursor 기반으로 조회한다.
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
		"/v5/order/realtime",
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
	result, _, err := client.responseResult(response, commonexchange.OperationRead)
	if err != nil {
		return OrderPage{}, err
	}
	return client.decodeOrderPage(response, result)
}

// OrderHistory는 최근 주문 이력을 cursor 기반으로 조회한다.
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
		"/v5/order/history",
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
	result, _, err := client.responseResult(response, commonexchange.OperationRead)
	if err != nil {
		return OrderPage{}, err
	}
	return client.decodeOrderPage(response, result)
}

func decodeOrderReference(
	response commonexchange.Response,
	result json.RawMessage,
	operation commonexchange.OperationKind,
	client *Client,
) (OrderReference, error) {
	var reference OrderReference
	if err := decodeResult(result, &reference); err != nil {
		return OrderReference{}, client.decodeBodyError(response, operation, err)
	}
	reference.Raw = cloneBytes(result)
	return reference, nil
}

func (client *Client) decodeOrderPage(
	response commonexchange.Response,
	result json.RawMessage,
) (OrderPage, error) {
	var rawPage struct {
		Category       Category          `json:"category"`
		Items          []json.RawMessage `json:"list"`
		NextPageCursor string            `json:"nextPageCursor"`
	}
	if err := decodeResult(result, &rawPage); err != nil {
		return OrderPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	page := OrderPage{
		Category: rawPage.Category, Orders: make([]Order, len(rawPage.Items)),
		NextPageCursor: rawPage.NextPageCursor, Raw: cloneBytes(result),
	}
	for index, item := range rawPage.Items {
		if err := decodeResult(item, &page.Orders[index]); err != nil {
			return OrderPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		page.Orders[index].Raw = cloneBytes(item)
	}
	return page, nil
}
