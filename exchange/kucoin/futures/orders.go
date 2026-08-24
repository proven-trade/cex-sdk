package futures

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// PlaceOrder는 Classic Futures 지정가 또는 시장가 주문을 생성한다.
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
		return OrderReference{}, validationError("encode KuCoin Futures order: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/api/v1/orders", nil, body,
		futuresLimit(2), credential.PermissionTrade, commonexchange.OperationMutation, options...,
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
			response, commonexchange.OperationMutation,
			errors.New("KuCoin Futures order acknowledgement is empty"),
		)
	}
	reference.Raw = cloneBytes(data)
	return reference, nil
}

// OrderInfo는 거래소 주문 ID로 Classic Futures 주문 한 건을 조회한다.
func (client *Client) OrderInfo(
	ctx context.Context,
	request OrderInfoRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/orders/"+url.PathEscape(request.OrderID), nil, nil,
		futuresLimit(5), credential.PermissionRead, commonexchange.OperationRead, options...,
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

// CancelOrder는 주문 ID 또는 사용자 주문 ID로 Classic Futures 주문 취소를 접수한다.
// 성공 응답은 취소 접수이며 최종 상태는 주문 조회 또는 private stream으로 확인해야 한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (CancelResult, error) {
	if err := request.validate(); err != nil {
		return CancelResult{}, err
	}
	path := "/api/v1/orders/" + url.PathEscape(request.OrderID)
	if request.ClientOrderID != "" {
		path = "/api/v1/orders/client-order/" + url.PathEscape(request.ClientOrderID)
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, path, nil, nil,
		futuresLimit(1), credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelResult{}, err
	}
	var result CancelResult
	data, err := client.decodeData(response, commonexchange.OperationMutation, &result)
	if err != nil {
		return CancelResult{}, err
	}
	if len(result.CancelledOrderIDs) == 0 && result.ClientOrderID == "" {
		return CancelResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation,
			errors.New("KuCoin Futures cancel acknowledgement is empty"),
		)
	}
	result.Raw = cloneBytes(data)
	return result, nil
}

// OpenOrders는 페이지 번호 기반 Classic Futures 미체결 주문을 조회한다.
func (client *Client) OpenOrders(
	ctx context.Context,
	request OpenOrdersRequest,
	options ...trade.RequestOption,
) (OrderPage, error) {
	if err := request.validate(); err != nil {
		return OrderPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/orders", request.values(), nil,
		futuresLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
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

// Fills는 주문 또는 계약으로 필터링한 Futures 체결 이력을 조회한다.
func (client *Client) Fills(
	ctx context.Context,
	request FillsRequest,
	options ...trade.RequestOption,
) (FillPage, error) {
	if err := request.validate(); err != nil {
		return FillPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/fills", request.values(), nil,
		futuresLimit(5), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return FillPage{}, err
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
		return FillPage{}, err
	}
	page := FillPage{
		CurrentPage: rawPage.CurrentPage, PageSize: rawPage.PageSize,
		TotalNumber: rawPage.TotalNumber, TotalPages: rawPage.TotalPages,
		Fills: make([]Fill, len(rawPage.Items)), Raw: cloneBytes(data),
	}
	for index, raw := range rawPage.Items {
		if err := json.Unmarshal(raw, &page.Fills[index]); err != nil {
			return FillPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		page.Fills[index].Raw = cloneBytes(raw)
	}
	return page, nil
}
