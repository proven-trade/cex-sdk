package usdm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// Account는 USDⓈ-M Futures 계정 V3 자산과 포지션을 조회한다.
func (client *Client) Account(ctx context.Context, options ...trade.RequestOption) (Account, error) {
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v3/account", nil, 5, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return Account{}, err
	}
	var account Account
	if err := client.decode(response, commonexchange.OperationRead, &account); err != nil {
		return Account{}, err
	}
	account.Raw = cloneBytes(response.Body)
	return account, nil
}

// Positions는 전체 또는 단일 계약의 포지션 위험 V3 정보를 조회한다.
func (client *Client) Positions(ctx context.Context, request PositionsRequest, options ...trade.RequestOption) ([]Position, error) {
	if request.Symbol != "" {
		if err := validateSymbol(request.Symbol); err != nil {
			return nil, err
		}
	}
	values := make(url.Values)
	set(values, "symbol", request.Symbol)
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v3/positionRisk", values, 5, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if err := client.decode(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	positions := make([]Position, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &positions[index]); err != nil {
			return nil, client.decodeError(response, commonexchange.OperationRead, err)
		}
		positions[index].Raw = cloneBytes(raw)
	}
	return positions, nil
}
