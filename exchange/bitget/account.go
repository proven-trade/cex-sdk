package bitget

import (
	"context"
	"encoding/json"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// AccountAssets는 통합 계정의 총자산과 코인별 잔고를 조회한다.
func (client *Client) AccountAssets(
	ctx context.Context,
	options ...trade.RequestOption,
) (AccountAssets, error) {
	response, _, err := client.executeSigned(
		ctx,
		http.MethodGet,
		"/api/v3/account/assets",
		nil,
		nil,
		accountLimit(20),
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return AccountAssets{}, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return AccountAssets{}, err
	}
	var assets AccountAssets
	if err := decodeData(data, &assets); err != nil {
		return AccountAssets{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	assets.Raw = cloneBytes(data)
	return assets, nil
}

// Positions는 USDT-M Futures 실시간 포지션을 조회한다.
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
		"/api/v3/position/current-position",
		request.values(),
		nil,
		accountLimit(20),
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return nil, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	var rawPage struct {
		Positions []json.RawMessage `json:"list"`
	}
	if err := decodeData(data, &rawPage); err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	positions := make([]Position, len(rawPage.Positions))
	for index, rawPosition := range rawPage.Positions {
		if err := decodeData(rawPosition, &positions[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		positions[index].Raw = cloneBytes(rawPosition)
	}
	return positions, nil
}
