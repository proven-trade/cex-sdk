package binance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// OpenOrdersRequest는 미체결 주문 조회 범위를 명시한다.
// 전체 상품 조회는 높은 weight를 사용하므로 AllSymbols를 명시해야 한다.
type OpenOrdersRequest struct {
	Symbol     string
	AllSymbols bool
}

func (request OpenOrdersRequest) validate() error {
	if request.AllSymbols && request.Symbol != "" {
		return validationError("open orders cannot set symbol and AllSymbols together")
	}
	if !request.AllSymbols {
		return validateSymbol(request.Symbol)
	}
	return nil
}

// AllOrdersRequest는 활성·취소·체결 주문 이력의 조회 범위다.
type AllOrdersRequest struct {
	Symbol    string
	OrderID   *int64
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

func (request AllOrdersRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.OrderID != nil && *request.OrderID <= 0 {
		return validationError("orderId must be positive")
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("all orders limit must be between 1 and 1000 or zero for default")
	}
	if request.StartTime != nil && request.EndTime != nil {
		if request.StartTime.After(*request.EndTime) {
			return validationError("startTime cannot be after endTime")
		}
		if request.EndTime.Sub(*request.StartTime) > 24*time.Hour {
			return validationError("startTime and endTime cannot span more than 24 hours")
		}
	}
	return nil
}

func (request AllOrdersRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	setInt64(values, "orderId", request.OrderID)
	if request.StartTime != nil {
		values.Set("startTime", strconv.FormatInt(request.StartTime.UnixMilli(), 10))
	}
	if request.EndTime != nil {
		values.Set("endTime", strconv.FormatInt(request.EndTime.UnixMilli(), 10))
	}
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	return values
}

// OpenOrders는 미체결 주문을 조회한다.
func (client *Client) OpenOrders(
	ctx context.Context,
	request OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	values := make(url.Values)
	weight := 80
	if !request.AllSymbols {
		values.Set("symbol", request.Symbol)
		weight = 6
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodGet,
		"/api/v3/openOrders",
		values,
		weight,
		0,
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return nil, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationRead); err != nil {
		return nil, err
	}
	return decodeOrders(response.Body)
}

// AllOrders는 상품의 활성·취소·체결 주문 이력을 조회한다.
func (client *Client) AllOrders(
	ctx context.Context,
	request AllOrdersRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodGet,
		"/api/v3/allOrders",
		request.values(),
		20,
		0,
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return nil, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationRead); err != nil {
		return nil, err
	}
	return decodeOrders(response.Body)
}

func decodeOrders(body []byte) ([]Order, error) {
	var rawOrders []json.RawMessage
	if err := decodeJSON(body, &rawOrders); err != nil {
		return nil, err
	}
	orders := make([]Order, len(rawOrders))
	for index, rawOrder := range rawOrders {
		if err := decodeJSON(rawOrder, &orders[index]); err != nil {
			return nil, err
		}
		orders[index].Raw = cloneBytes(rawOrder)
	}
	return orders, nil
}
