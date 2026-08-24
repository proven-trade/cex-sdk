package kraken

import (
	"context"
	"encoding/json"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// Balance는 계정의 자산별 총 Spot 잔고를 조회한다.
func (client *Client) Balance(
	ctx context.Context,
	options ...trade.RequestOption,
) (Balance, error) {
	response, err := client.executePrivate(
		ctx, privatePrefix+"Balance", nil, credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return Balance{}, err
	}
	result, err := client.decodeResult(response, commonexchange.OperationRead)
	if err != nil {
		return Balance{}, err
	}
	var amounts map[string]string
	if err := json.Unmarshal(result, &amounts); err != nil {
		return Balance{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return Balance{Amounts: amounts, Raw: cloneBytes(result)}, nil
}
