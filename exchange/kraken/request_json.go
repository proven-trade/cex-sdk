package kraken

import (
	"context"
	"encoding/json"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	formContentType = "application/x-www-form-urlencoded"
	jsonContentType = "application/json"
)

// payloadEncoder는 요청 nonce로 서명 대상 본문과 Content-Type을 만든다.
// 서명은 nonce와 실제 전송하는 본문 문자열 전체를 대상으로 하므로 본문 생성과 서명이 같은 nonce를 쓴다.
type payloadEncoder func(nonce string) (payload string, contentType string, err error)

// executePrivateJSON은 중첩 배열이 필요한 private 요청을 JSON 본문으로 서명해 전송한다.
// body에 nonce를 넣지 않아도 되며, 전송 직전에 숫자 nonce가 추가된다.
func (client *Client) executePrivateJSON(
	ctx context.Context,
	path string,
	body map[string]any,
	permission credential.Permission,
	operation commonexchange.OperationKind,
	limit limitKind,
	pair string,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	return client.executePrivatePayload(
		ctx, path, func(nonce string) (string, string, error) {
			payload := make(map[string]any, len(body)+1)
			for key, value := range body {
				payload[key] = value
			}
			payload["nonce"] = json.Number(nonce)
			encoded, err := json.Marshal(payload)
			if err != nil {
				return "", "", validationError("encode Kraken JSON request: %v", err)
			}
			return string(encoded), jsonContentType, nil
		}, permission, operation, limit, pair, options...,
	)
}
