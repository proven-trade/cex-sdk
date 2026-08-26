package okx

import (
	"context"
	"errors"
	"net/http"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// PlaceOrder는 Spot 또는 SWAP 주문을 생성한다.
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
		"/api/v5/trade/order",
		nil,
		body,
		accountLimit(60, 2*time.Second, request.InstrumentID),
		credential.PermissionTrade,
		commonexchange.OperationMutation,
		options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	data, err := client.responseData(response, commonexchange.OperationMutation)
	if err != nil {
		return OrderReference{}, err
	}
	return client.decodeOrderReference(response, data, commonexchange.OperationMutation)
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
		"/api/v5/trade/cancel-order",
		nil,
		body,
		accountLimit(60, 2*time.Second, request.InstrumentID),
		credential.PermissionTrade,
		commonexchange.OperationMutation,
		options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	data, err := client.responseData(response, commonexchange.OperationMutation)
	if err != nil {
		return OrderReference{}, err
	}
	return client.decodeOrderReference(response, data, commonexchange.OperationMutation)
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
		"/api/v5/trade/order",
		request.values(),
		nil,
		accountLimit(60, 2*time.Second, request.InstrumentID),
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return Order{}, err
	}
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return Order{}, err
	}
	items, err := decodeItems(data, func(item *Order, raw []byte) { item.Raw = raw })
	if err != nil {
		return Order{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	if len(items) == 0 {
		return Order{}, client.decodeError(
			response, "51603", "order does not exist", commonexchange.OperationRead, nil,
		)
	}
	return items[0], nil
}

// OpenOrders는 미체결 주문을 pagination 조건으로 조회한다.
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
		"/api/v5/trade/orders-pending",
		request.values(),
		nil,
		accountLimit(60, 2*time.Second, ""),
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return OrderPage{}, err
	}
	return client.decodeOrderPage(response)
}

// OrderHistory는 최근 7일 주문 이력을 pagination 조건으로 조회한다.
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
		"/api/v5/trade/orders-history",
		request.values(),
		nil,
		accountLimit(40, 2*time.Second, ""),
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return OrderPage{}, err
	}
	return client.decodeOrderPage(response)
}

func (client *Client) decodeOrderReference(
	response commonexchange.Response,
	data []byte,
	operation commonexchange.OperationKind,
) (OrderReference, error) {
	items, err := decodeItems(data, func(item *OrderReference, raw []byte) { item.Raw = raw })
	if err != nil || len(items) == 0 {
		if err == nil {
			err = errors.New("OKX order acknowledgement is empty")
		}
		return OrderReference{}, client.decodeBodyError(response, operation, err)
	}
	if items[0].StatusCode == "" {
		return OrderReference{}, client.decodeBodyError(
			response, operation, errors.New("OKX order acknowledgement status code is missing"),
		)
	}
	if items[0].StatusCode != "" && items[0].StatusCode != "0" {
		return OrderReference{}, client.decodeError(
			response, items[0].StatusCode, items[0].StatusMessage, operation, nil,
		)
	}
	if items[0].OrderID == "" && items[0].ClientOrderID == "" {
		return OrderReference{}, client.decodeBodyError(
			response, operation, errors.New("OKX order acknowledgement identity is missing"),
		)
	}
	return items[0], nil
}

func (client *Client) decodeOrderPage(response commonexchange.Response) (OrderPage, error) {
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return OrderPage{}, err
	}
	orders, err := decodeItems(data, func(item *Order, raw []byte) { item.Raw = raw })
	if err != nil {
		return OrderPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return OrderPage{Orders: orders, Raw: cloneBytes(data)}, nil
}
