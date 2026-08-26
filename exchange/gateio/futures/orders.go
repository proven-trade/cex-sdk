package futures

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// PlaceOrder는 Gate.io 무기한 Futures 지정가 또는 시장가 주문을 생성한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) PlaceOrder(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	canonical := request.canonical()
	body, err := json.Marshal(canonical)
	if err != nil {
		return Order{}, validationError("encode Gate.io Futures order: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, futuresPath(request.Settlement, "/orders"), nil, body,
		orderLimit(), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Order{}, err
	}
	order, err := client.decodeOrder(response, commonexchange.OperationMutation)
	if err != nil {
		return Order{}, err
	}
	if order.ID == "" {
		return Order{}, client.decodeBodyError(
			response, commonexchange.OperationMutation,
			errors.New("Gate.io Futures order acknowledgement is empty"),
		)
	}
	return order, nil
}

// OrderInfo는 거래소 주문 ID 또는 사용자 주문 ID로 무기한 Futures 주문 한 건을 조회한다.
func (client *Client) OrderInfo(
	ctx context.Context,
	request OrderInfoRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet,
		futuresPath(request.Settlement, "/orders/"+url.PathEscape(request.OrderID)), nil, nil,
		privateLimit("order-info"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Order{}, err
	}
	return client.decodeOrder(response, commonexchange.OperationRead)
}

// CancelOrder는 거래소 주문 ID 또는 사용자 주문 ID로 무기한 Futures 주문 취소를 접수한다.
// 성공 응답은 취소 접수이며 최종 상태는 주문 조회 또는 private stream으로 확인해야 한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete,
		futuresPath(request.Settlement, "/orders/"+url.PathEscape(request.OrderID)), nil, nil,
		cancelLimit(), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Order{}, err
	}
	order, err := client.decodeOrder(response, commonexchange.OperationMutation)
	if err != nil {
		return Order{}, err
	}
	if order.ID == "" {
		return Order{}, client.decodeBodyError(
			response, commonexchange.OperationMutation,
			errors.New("Gate.io Futures cancel acknowledgement is empty"),
		)
	}
	return order, nil
}

// Orders는 계약과 상태로 필터링한 Gate.io 무기한 Futures 주문 한 페이지를 조회한다.
func (client *Client) Orders(
	ctx context.Context,
	request OrdersRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, futuresPath(request.Settlement, "/orders"), request.values(), nil,
		privateLimit("orders"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return client.decodeOrders(response)
}

// MyTrades는 계약 또는 주문으로 필터링한 Gate.io 무기한 Futures 계정 체결을 조회한다.
func (client *Client) MyTrades(
	ctx context.Context,
	request MyTradesRequest,
	options ...trade.RequestOption,
) ([]MyTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, futuresPath(request.Settlement, "/my_trades"), request.values(), nil,
		privateLimit("my-trades"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make([]MyTrade, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

func (client *Client) decodeOrder(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) (Order, error) {
	var order Order
	data, err := client.decodeData(response, operation, &order)
	if err != nil {
		return Order{}, err
	}
	order.Raw = cloneBytes(data)
	return order, nil
}

func (client *Client) decodeOrders(response commonexchange.Response) ([]Order, error) {
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make([]Order, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}
