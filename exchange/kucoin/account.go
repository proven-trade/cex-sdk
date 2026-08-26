package kucoin

import (
	"context"
	"encoding/json"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// Accounts는 통화와 계정 유형별 Classic 계정 잔고를 조회한다.
func (client *Client) Accounts(
	ctx context.Context,
	request AccountsRequest,
	options ...trade.RequestOption,
) ([]Account, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/accounts", request.values(), nil,
		managementLimit(5), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make([]Account, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}
