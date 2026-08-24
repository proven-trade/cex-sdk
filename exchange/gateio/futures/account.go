package futures

import (
	"context"
	"encoding/json"
	"net/http"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// Account는 결제 통화별 Gate.io 무기한 Futures 자산과 증거금 요약을 조회한다.
func (client *Client) Account(
	ctx context.Context,
	request AccountRequest,
	options ...trade.RequestOption,
) (Account, error) {
	if err := request.validate(); err != nil {
		return Account{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, futuresPath(request.Settlement, "/accounts"), nil, nil,
		privateLimit("accounts"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Account{}, err
	}
	var account Account
	data, err := client.decodeData(response, commonexchange.OperationRead, &account)
	if err != nil {
		return Account{}, err
	}
	account.Raw = cloneBytes(data)
	return account, nil
}

// Positions는 결제 통화별 Gate.io 무기한 Futures 포지션 한 페이지를 조회한다.
func (client *Client) Positions(
	ctx context.Context,
	request PositionsRequest,
	options ...trade.RequestOption,
) ([]Position, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, futuresPath(request.Settlement, "/positions"), request.values(), nil,
		privateLimit("positions"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make([]Position, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}
