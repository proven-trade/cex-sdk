package futures

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// AccountOverview는 결제 통화별 Futures 자산과 증거금 요약을 조회한다.
func (client *Client) AccountOverview(
	ctx context.Context,
	request AccountOverviewRequest,
	options ...trade.RequestOption,
) (AccountOverview, error) {
	if err := request.validate(); err != nil {
		return AccountOverview{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/account-overview",
		url.Values{"currency": {request.Currency}}, nil,
		futuresLimit(5), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return AccountOverview{}, err
	}
	var overview AccountOverview
	data, err := client.decodeData(response, commonexchange.OperationRead, &overview)
	if err != nil {
		return AccountOverview{}, err
	}
	overview.Raw = cloneBytes(data)
	return overview, nil
}

// Positions는 선택한 결제 통화의 열린 Futures 포지션을 조회한다.
func (client *Client) Positions(
	ctx context.Context,
	request PositionsRequest,
	options ...trade.RequestOption,
) ([]Position, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/positions", request.values(), nil,
		futuresLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
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
