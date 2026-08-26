package kucoin

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

// PlaceOrder는 Classic Spot 지정가 또는 시장가 주문을 생성한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) PlaceOrder(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return OrderReference{}, validationError("encode KuCoin order: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/api/v1/hf/orders", nil, body,
		spotLimit(1), credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	var reference OrderReference
	data, err := client.decodeData(response, commonexchange.OperationMutation, &reference)
	if err != nil {
		return OrderReference{}, err
	}
	if reference.OrderID == "" {
		return OrderReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("KuCoin order acknowledgement is empty"),
		)
	}
	reference.Raw = cloneBytes(data)
	return reference, nil
}

// OrderInfo는 주문 ID 또는 사용자 주문 ID로 Classic Spot 주문 한 건을 조회한다.
func (client *Client) OrderInfo(
	ctx context.Context,
	request OrderInfoRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	path := orderIdentityPath(request.OrderID, request.ClientOrderID)
	response, err := client.executePrivate(
		ctx, http.MethodGet, path, request.values(), nil,
		spotLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Order{}, err
	}
	var order Order
	data, err := client.decodeData(response, commonexchange.OperationRead, &order)
	if err != nil {
		return Order{}, err
	}
	order.Raw = cloneBytes(data)
	return order, nil
}

// CancelOrder는 주문 ID 또는 사용자 주문 ID로 Classic Spot 주문 취소를 접수한다.
// 성공 응답은 취소 접수이며 최종 상태는 주문 조회 또는 private stream으로 확인해야 한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	path := orderIdentityPath(request.OrderID, request.ClientOrderID)
	response, err := client.executePrivate(
		ctx, http.MethodDelete, path, request.values(), nil,
		spotLimit(1), credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	var reference OrderReference
	data, err := client.decodeData(response, commonexchange.OperationMutation, &reference)
	if err != nil {
		return OrderReference{}, err
	}
	if reference.OrderID == "" && reference.ClientOrderID == "" {
		return OrderReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("KuCoin cancel acknowledgement is empty"),
		)
	}
	reference.Raw = cloneBytes(data)
	return reference, nil
}

// OpenOrderSymbols는 현재 미체결 주문이 존재하는 거래쌍을 조회한다.
func (client *Client) OpenOrderSymbols(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]string, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/hf/orders/active/symbols", nil, nil,
		spotLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var result struct {
		Symbols []string `json:"symbols"`
	}
	if _, err := client.decodeData(response, commonexchange.OperationRead, &result); err != nil {
		return nil, err
	}
	for _, symbol := range result.Symbols {
		if err := validateSymbol(symbol); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
	}
	seen := make(map[string]struct{}, len(result.Symbols))
	for _, symbol := range result.Symbols {
		if _, exists := seen[symbol]; exists {
			return nil, client.decodeBodyError(
				response, commonexchange.OperationRead,
				errors.New("KuCoin open order symbols response contains a duplicate"),
			)
		}
		seen[symbol] = struct{}{}
	}
	return append([]string(nil), result.Symbols...), nil
}

// OpenOrders는 폐기된 active 목록 대신 페이지 기반 Classic Spot 미체결 주문을 조회한다.
func (client *Client) OpenOrders(
	ctx context.Context,
	request OpenOrdersRequest,
	options ...trade.RequestOption,
) (OrderPage, error) {
	if err := request.validate(); err != nil {
		return OrderPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/hf/orders/active/page", request.values(), nil,
		spotLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return OrderPage{}, err
	}
	var rawPage struct {
		CurrentPage int               `json:"currentPage"`
		PageSize    int               `json:"pageSize"`
		TotalNumber int               `json:"totalNum"`
		TotalPages  int               `json:"totalPage"`
		Items       []json.RawMessage `json:"items"`
	}
	data, err := client.decodeData(response, commonexchange.OperationRead, &rawPage)
	if err != nil {
		return OrderPage{}, err
	}
	page := OrderPage{
		CurrentPage: rawPage.CurrentPage, PageSize: rawPage.PageSize,
		TotalNumber: rawPage.TotalNumber, TotalPages: rawPage.TotalPages,
		Orders: make([]Order, len(rawPage.Items)), Raw: cloneBytes(data),
	}
	for index, raw := range rawPage.Items {
		if err := json.Unmarshal(raw, &page.Orders[index]); err != nil {
			return OrderPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		page.Orders[index].Raw = cloneBytes(raw)
	}
	return page, nil
}

func orderIdentityPath(orderID, clientOrderID string) string {
	if orderID != "" {
		return "/api/v1/hf/orders/" + url.PathEscape(orderID)
	}
	return "/api/v1/hf/orders/client-order/" + url.PathEscape(clientOrderID)
}
