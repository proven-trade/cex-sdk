package bybit

import (
	"context"
	"encoding/json"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// WalletBalance는 통합 계정의 총자산과 코인별 잔고를 조회한다.
func (client *Client) WalletBalance(
	ctx context.Context,
	request WalletBalanceRequest,
	options ...trade.RequestOption,
) ([]WalletAccount, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodGet,
		"/v5/account/wallet-balance",
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
	result, _, err := client.responseResult(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	var page struct {
		Items []json.RawMessage `json:"list"`
	}
	if err := decodeResult(result, &page); err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	accounts := make([]WalletAccount, len(page.Items))
	for index, item := range page.Items {
		if err := decodeResult(item, &accounts[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		accounts[index].Raw = cloneBytes(item)
	}
	return accounts, nil
}

// Positions는 Linear 포지션을 cursor 기반으로 조회한다.
func (client *Client) Positions(
	ctx context.Context,
	request PositionsRequest,
	options ...trade.RequestOption,
) (PositionPage, error) {
	if err := request.validate(); err != nil {
		return PositionPage{}, err
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodGet,
		"/v5/position/list",
		request.values(),
		nil,
		accountLimit(20),
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return PositionPage{}, err
	}
	result, _, err := client.responseResult(response, commonexchange.OperationRead)
	if err != nil {
		return PositionPage{}, err
	}
	var page struct {
		Items          []json.RawMessage `json:"list"`
		NextPageCursor string            `json:"nextPageCursor"`
	}
	if err := decodeResult(result, &page); err != nil {
		return PositionPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	positionPage := PositionPage{
		Category: request.Category, Positions: make([]Position, len(page.Items)),
		NextPageCursor: page.NextPageCursor, Raw: cloneBytes(result),
	}
	for index, item := range page.Items {
		if err := decodeResult(item, &positionPage.Positions[index]); err != nil {
			return PositionPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		positionPage.Positions[index].Raw = cloneBytes(item)
	}
	return positionPage, nil
}
