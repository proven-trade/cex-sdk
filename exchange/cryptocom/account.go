package cryptocom

import (
	"context"
	"encoding/json"
	"errors"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

const methodUserBalance = "private/user-balance"

// Balance는 Crypto.com 계정 위험 요약과 통화별 가용·예약 잔고를 조회한다.
func (client *Client) Balance(
	ctx context.Context,
	options ...trade.RequestOption,
) (UserBalance, error) {
	response, requestID, err := client.executePrivate(
		ctx, methodUserBalance, nil, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return UserBalance{}, err
	}
	resultRaw, err := client.decodePrivateResult(
		response, methodUserBalance, requestID, commonexchange.OperationRead, true,
	)
	if err != nil {
		return UserBalance{}, err
	}
	itemsRaw, err := client.decodePrivateDataItems(
		response, resultRaw, commonexchange.OperationRead, requestID,
	)
	if err != nil {
		return UserBalance{}, err
	}
	value := UserBalance{
		Accounts: make([]BalanceAccount, len(itemsRaw)), Raw: cloneBytes(resultRaw),
	}
	for index, itemRaw := range itemsRaw {
		account, err := client.decodeBalanceAccount(response, requestID, itemRaw)
		if err != nil {
			return UserBalance{}, err
		}
		value.Accounts[index] = account
	}
	return value, nil
}

func (client *Client) decodeBalanceAccount(
	response commonexchange.Response,
	requestID string,
	raw json.RawMessage,
) (BalanceAccount, error) {
	var value BalanceAccount
	if err := json.Unmarshal(raw, &value); err != nil {
		return BalanceAccount{}, client.decodePrivateBodyError(
			response, commonexchange.OperationRead, requestID, err,
		)
	}
	if value.InstrumentName == "" {
		return BalanceAccount{}, client.decodePrivateBodyError(
			response, commonexchange.OperationRead, requestID,
			errors.New("Crypto.com balance account instrument is missing"),
		)
	}
	var collections struct {
		PositionBalances  []json.RawMessage `json:"position_balances"`
		IsolatedPositions []json.RawMessage `json:"isolated_positions"`
	}
	if err := json.Unmarshal(raw, &collections); err != nil {
		return BalanceAccount{}, client.decodePrivateBodyError(
			response, commonexchange.OperationRead, requestID, err,
		)
	}
	for index, itemRaw := range collections.PositionBalances {
		if index >= len(value.PositionBalances) {
			break
		}
		if value.PositionBalances[index].InstrumentName == "" {
			return BalanceAccount{}, client.decodePrivateBodyError(
				response, commonexchange.OperationRead, requestID,
				errors.New("Crypto.com position balance instrument is missing"),
			)
		}
		value.PositionBalances[index].Raw = cloneBytes(itemRaw)
	}
	for index, itemRaw := range collections.IsolatedPositions {
		if index >= len(value.IsolatedPositions) {
			break
		}
		value.IsolatedPositions[index].Raw = cloneBytes(itemRaw)
	}
	value.Raw = cloneBytes(raw)
	return value, nil
}
