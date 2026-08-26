package htx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// PlaceOrder는 고유한 사용자 주문 ID를 가진 HTX Spot 주문을 생성한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) PlaceOrder(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	wire := struct {
		AccountID        string    `json:"account-id"`
		Amount           string    `json:"amount"`
		Price            string    `json:"price,omitempty"`
		Source           string    `json:"source"`
		Symbol           string    `json:"symbol"`
		Type             OrderType `json:"type"`
		ClientOrderID    string    `json:"client-order-id"`
		SelfMatchPrevent int       `json:"self-match-prevent,omitempty"`
	}{
		AccountID: request.AccountID, Amount: request.Amount, Price: request.Price,
		Source: "spot-api", Symbol: request.Symbol, Type: request.orderType(),
		ClientOrderID: request.ClientOrderID,
	}
	if request.SelfMatchPrevent {
		wire.SelfMatchPrevent = 1
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return OrderReference{}, validationError("encode HTX order: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v1/order/orders/place", nil, body,
		rateGroupOrder, client.orderQuota, credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	var data json.RawMessage
	if _, err := client.decodeDataForOperation(
		response, commonexchange.OperationMutation, &data,
	); err != nil {
		return OrderReference{}, err
	}
	orderID, err := optionalScalarText(data)
	if err != nil || !orderIDPattern.MatchString(orderID) {
		if err == nil {
			err = errors.New("HTX order acknowledgement has an invalid order ID")
		}
		return OrderReference{}, client.decodeBodyErrorForOperation(
			response, commonexchange.OperationMutation, err,
		)
	}
	return OrderReference{
		OrderID: Scalar(orderID), ClientOrderID: request.ClientOrderID, Raw: cloneBytes(data),
	}, nil
}

// OrderInfo는 거래소 주문 ID 또는 사용자 주문 ID로 HTX Spot 주문을 조회한다.
func (client *Client) OrderInfo(
	ctx context.Context,
	request OrderInfoRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	path := "/v1/order/orders/" + request.OrderID
	query := make(url.Values)
	if request.ClientOrderID != "" {
		path = "/v1/order/orders/getClientOrder"
		query.Set("clientOrderId", request.ClientOrderID)
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, path, query, nil,
		rateGroupOrderRead, client.orderReadQuota, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Order{}, err
	}
	return client.decodeOrder(response)
}

// CancelOrder는 거래소 주문 ID 또는 사용자 주문 ID로 HTX Spot 주문 취소를 접수한다.
// 성공 응답은 취소 접수이며 최종 상태는 주문 조회 또는 private stream으로 확인해야 한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (CancelResult, error) {
	if err := request.validate(); err != nil {
		return CancelResult{}, err
	}
	path := "/v1/order/orders/" + request.OrderID + "/submitcancel"
	query := make(url.Values)
	var body []byte
	if request.ClientOrderID != "" {
		path = "/v1/order/orders/submitCancelClientOrder"
		encoded, err := json.Marshal(struct {
			ClientOrderID string `json:"client-order-id"`
		}{ClientOrderID: request.ClientOrderID})
		if err != nil {
			return CancelResult{}, validationError("encode HTX cancel request: %v", err)
		}
		body = encoded
	} else {
		setIfNotEmpty(query, "symbol", request.Symbol)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, path, query, body,
		rateGroupOrder, client.orderQuota, credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelResult{}, err
	}
	var data json.RawMessage
	if _, err := client.decodeDataForOperation(
		response, commonexchange.OperationMutation, &data,
	); err != nil {
		return CancelResult{}, err
	}
	value, err := optionalScalarText(data)
	if err != nil || value == "" {
		if err == nil {
			err = errors.New("HTX cancel acknowledgement is empty")
		}
		return CancelResult{}, client.decodeBodyErrorForOperation(
			response, commonexchange.OperationMutation, err,
		)
	}
	result := CancelResult{
		ClientOrderID: request.ClientOrderID, Raw: cloneBytes(data),
	}
	if request.OrderID != "" {
		if !orderIDPattern.MatchString(value) {
			return CancelResult{}, client.decodeBodyErrorForOperation(
				response, commonexchange.OperationMutation,
				errors.New("HTX cancel acknowledgement has an invalid order ID"),
			)
		}
		result.OrderID = Scalar(value)
		return result, nil
	}
	statusCode, parseErr := strconv.Atoi(value)
	if parseErr != nil {
		return CancelResult{}, client.decodeBodyErrorForOperation(
			response, commonexchange.OperationMutation,
			fmt.Errorf("decode HTX cancel status code: %w", parseErr),
		)
	}
	result.StatusCode = &statusCode
	return result, nil
}

// OpenOrders는 필터와 커서를 적용해 현재 HTX Spot 미체결 주문을 조회한다.
func (client *Client) OpenOrders(
	ctx context.Context,
	request OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/order/openOrders", request.values(), nil,
		rateGroupOrderRead, client.orderReadQuota, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return client.decodeOrders(response)
}

// OrderHistory는 최대 48시간 범위의 종료된 HTX Spot 주문 이력을 조회한다.
func (client *Client) OrderHistory(
	ctx context.Context,
	request OrderHistoryRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/order/orders", request.values(), nil,
		rateGroupOrderRead, client.orderReadQuota, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return client.decodeOrders(response)
}

// MatchResults는 최대 48시간 범위의 계정 HTX Spot 체결 이력을 조회한다.
func (client *Client) MatchResults(
	ctx context.Context,
	request MatchResultsRequest,
	options ...trade.RequestOption,
) ([]MatchResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/order/matchresults", request.values(), nil,
		rateGroupTradeHistory, client.tradeHistoryQuota, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return client.decodeMatchResults(response)
}

// OrderMatches는 거래소 주문 ID에 속한 HTX Spot 체결 목록을 조회한다.
func (client *Client) OrderMatches(
	ctx context.Context,
	orderID string,
	options ...trade.RequestOption,
) ([]MatchResult, error) {
	if !orderIDPattern.MatchString(orderID) {
		return nil, validationError("invalid HTX order ID %q", orderID)
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/order/orders/"+orderID+"/matchresults", nil, nil,
		rateGroupOrderRead, client.orderReadQuota, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return client.decodeMatchResults(response)
}

func (client *Client) decodeOrder(response commonexchange.Response) (Order, error) {
	var value Order
	raw, err := client.decodeDataForOperation(response, commonexchange.OperationRead, &value)
	if err != nil {
		return Order{}, err
	}
	if value.ID == "" || value.Symbol == "" {
		return Order{}, client.decodeBodyError(
			response, errors.New("HTX order response is missing an order ID or symbol"),
		)
	}
	value.Raw = raw
	return value, nil
}

func (client *Client) decodeOrders(response commonexchange.Response) ([]Order, error) {
	var rawItems []json.RawMessage
	if _, err := client.decodeDataForOperation(
		response, commonexchange.OperationRead, &rawItems,
	); err != nil {
		return nil, err
	}
	items := make([]Order, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		if items[index].ID == "" || items[index].Symbol == "" {
			return nil, client.decodeBodyError(
				response, errors.New("HTX order list item is missing an order ID or symbol"),
			)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

func (client *Client) decodeMatchResults(
	response commonexchange.Response,
) ([]MatchResult, error) {
	var rawItems []json.RawMessage
	if _, err := client.decodeDataForOperation(
		response, commonexchange.OperationRead, &rawItems,
	); err != nil {
		return nil, err
	}
	items := make([]MatchResult, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		if items[index].ID == "" || items[index].OrderID == "" {
			return nil, client.decodeBodyError(
				response, errors.New("HTX match result is missing an ID or order ID"),
			)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}
