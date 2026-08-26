package kucoin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// WebSocketInstanceServer는 KuCoin이 발급한 WebSocket 서버와 heartbeat 규칙이다.
type WebSocketInstanceServer struct {
	Endpoint     string `json:"endpoint"`
	Encrypt      bool   `json:"encrypt"`
	Protocol     string `json:"protocol"`
	PingInterval int64  `json:"pingInterval"`
	PingTimeout  int64  `json:"pingTimeout"`
}

// WebSocketToken은 연결 token과 사용 가능한 WebSocket 서버 목록이다.
type WebSocketToken struct {
	Token   string
	Servers []WebSocketInstanceServer
	Raw     json.RawMessage
}

// PublicWebSocketToken은 Classic Spot public WebSocket 연결 token을 발급한다.
func (client *Client) PublicWebSocketToken(
	ctx context.Context,
	options ...trade.RequestOption,
) (WebSocketToken, error) {
	response, err := client.executePublic(
		ctx, http.MethodPost, "/api/v1/bullet-public", nil, publicLimit(10), options...,
	)
	if err != nil {
		return WebSocketToken{}, err
	}
	return client.decodeWebSocketToken(response)
}

// PrivateWebSocketToken은 Classic Spot private WebSocket 연결 token을 발급한다.
func (client *Client) PrivateWebSocketToken(
	ctx context.Context,
	options ...trade.RequestOption,
) (WebSocketToken, error) {
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/api/v1/bullet-private", nil, nil,
		spotLimit(10), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return WebSocketToken{}, err
	}
	return client.decodeWebSocketToken(response)
}

func (client *Client) decodeWebSocketToken(
	response commonexchange.Response,
) (WebSocketToken, error) {
	var wire struct {
		Token   string                    `json:"token"`
		Servers []WebSocketInstanceServer `json:"instanceServers"`
	}
	data, err := client.decodeData(response, commonexchange.OperationRead, &wire)
	if err != nil {
		return WebSocketToken{}, err
	}
	if wire.Token == "" || len(wire.Servers) == 0 {
		return WebSocketToken{}, client.decodeBodyError(
			response, commonexchange.OperationRead,
			errors.New("KuCoin WebSocket token response is incomplete"),
		)
	}
	return WebSocketToken{
		Token: wire.Token, Servers: append([]WebSocketInstanceServer(nil), wire.Servers...),
		Raw: cloneBytes(data),
	}, nil
}
