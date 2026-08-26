package kraken

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// WebSocketToken은 Spot private WebSocket 구독용 token을 발급한다.
// token은 발급 후 15분 안에 사용해야 하며 연결이 유지되는 동안에는 만료되지 않는다.
func (client *Client) WebSocketToken(
	ctx context.Context,
	options ...trade.RequestOption,
) (WebSocketToken, error) {
	response, err := client.executePrivate(
		ctx, privatePrefix+"GetWebSocketsToken", nil, credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return WebSocketToken{}, err
	}
	defer clearSpotStreamBytes(response.Body)
	result, err := client.decodeResult(response, commonexchange.OperationRead)
	if err != nil {
		return WebSocketToken{}, err
	}
	defer clearSpotStreamBytes(result)
	var value struct {
		Token   string `json:"token"`
		Expires int64  `json:"expires"`
	}
	if err := json.Unmarshal(result, &value); err != nil {
		return WebSocketToken{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	if value.Token == "" || value.Expires <= 0 {
		return WebSocketToken{}, client.decodeBodyError(
			response, commonexchange.OperationRead,
			errors.New("Kraken WebSocket token response is incomplete"),
		)
	}
	return WebSocketToken{Value: value.Token, Expires: time.Duration(value.Expires) * time.Second}, nil
}
