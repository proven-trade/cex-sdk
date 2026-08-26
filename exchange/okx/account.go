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

// Balance는 거래 계정의 총자산과 통화별 잔고를 조회한다.
func (client *Client) Balance(
	ctx context.Context,
	request BalanceRequest,
	options ...trade.RequestOption,
) (Balance, error) {
	if err := request.validate(); err != nil {
		return Balance{}, err
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodGet,
		"/api/v5/account/balance",
		request.values(),
		nil,
		accountLimit(10, 2*time.Second, ""),
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return Balance{}, err
	}
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return Balance{}, err
	}
	items, err := decodeItems(data, func(item *Balance, raw []byte) { item.Raw = raw })
	if err != nil || len(items) == 0 {
		if err == nil {
			err = errors.New("OKX balance response is empty")
		}
		return Balance{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return items[0], nil
}

// Positions는 SWAP 포지션을 조회한다.
func (client *Client) Positions(
	ctx context.Context,
	request PositionsRequest,
	options ...trade.RequestOption,
) ([]Position, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodGet,
		"/api/v5/account/positions",
		request.values(),
		nil,
		accountLimit(10, 2*time.Second, ""),
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return nil, err
	}
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems(data, func(item *Position, raw []byte) { item.Raw = raw })
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return items, nil
}
