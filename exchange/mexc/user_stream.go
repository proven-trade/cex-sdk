package mexc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
	"github.com/proven-trade/proven-trade-sdk/model"
)

const userDataStreamPath = "/api/v3/userDataStream"

var listenKeyPattern = regexp.MustCompile(`^[0-9A-Za-z]{1,256}$`)

// UserDataListenKey는 MEXC Spot private WebSocket 접속 키와 원본 응답이다.
type UserDataListenKey struct {
	ListenKey string          `json:"listenKey"`
	Raw       json.RawMessage `json:"-"`
}

// UserDataListenKeys는 현재 계정에서 유효한 private WebSocket 접속 키 목록이다.
type UserDataListenKeys struct {
	ListenKeys []string        `json:"listenKey"`
	Raw        json.RawMessage `json:"-"`
}

// StartUserDataStream은 현재 API Key 계정의 새 listenKey를 발급한다.
func (client *Client) StartUserDataStream(
	ctx context.Context,
	options ...trade.RequestOption,
) (UserDataListenKey, error) {
	response, err := client.executeAPIKey(
		ctx, http.MethodPost, userDataStreamPath, nil, commonexchange.OperationMutation,
		options...,
	)
	if err != nil {
		return UserDataListenKey{}, err
	}
	var result UserDataListenKey
	raw, err := client.decodeResponseForOperation(
		response, commonexchange.OperationMutation, &result,
	)
	if err != nil {
		return UserDataListenKey{}, err
	}
	if !listenKeyPattern.MatchString(result.ListenKey) {
		return UserDataListenKey{}, client.decodeBodyErrorForOperation(
			response, commonexchange.OperationMutation,
			errors.New("MEXC listen key response is invalid"),
		)
	}
	result.Raw = raw
	return result, nil
}

// UserDataStreams는 현재 API Key 계정에서 유효한 listenKey를 조회한다.
func (client *Client) UserDataStreams(
	ctx context.Context,
	options ...trade.RequestOption,
) (UserDataListenKeys, error) {
	response, err := client.executeAPIKey(
		ctx, http.MethodGet, userDataStreamPath, nil, commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return UserDataListenKeys{}, err
	}
	var result UserDataListenKeys
	raw, err := client.decodeResponse(response, &result)
	if err != nil {
		return UserDataListenKeys{}, err
	}
	seen := make(map[string]struct{}, len(result.ListenKeys))
	for _, listenKey := range result.ListenKeys {
		if !listenKeyPattern.MatchString(listenKey) {
			return UserDataListenKeys{}, client.decodeBodyError(
				response, errors.New("MEXC listen key list contains an invalid value"),
			)
		}
		if _, exists := seen[listenKey]; exists {
			return UserDataListenKeys{}, client.decodeBodyError(
				response, errors.New("MEXC listen key list contains a duplicate"),
			)
		}
		seen[listenKey] = struct{}{}
	}
	result.Raw = raw
	return result, nil
}

// KeepaliveUserDataStream은 지정한 listenKey의 유효 시간을 다시 60분으로 연장한다.
func (client *Client) KeepaliveUserDataStream(
	ctx context.Context,
	listenKey string,
	options ...trade.RequestOption,
) (UserDataListenKey, error) {
	if !listenKeyPattern.MatchString(listenKey) {
		return UserDataListenKey{}, validationError("invalid MEXC listen key")
	}
	query := url.Values{"listenKey": {listenKey}}
	response, err := client.executeAPIKey(
		ctx, http.MethodPut, userDataStreamPath, query, commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return UserDataListenKey{}, err
	}
	var result UserDataListenKey
	raw, err := client.decodeResponse(response, &result)
	if err != nil {
		return UserDataListenKey{}, err
	}
	if result.ListenKey != listenKey {
		return UserDataListenKey{}, client.decodeBodyError(
			response, errors.New("MEXC keepalive returned a different listen key"),
		)
	}
	result.Raw = raw
	return result, nil
}

// CloseUserDataStream은 지정한 listenKey를 즉시 무효화한다.
func (client *Client) CloseUserDataStream(
	ctx context.Context,
	listenKey string,
	options ...trade.RequestOption,
) (UserDataListenKey, error) {
	if !listenKeyPattern.MatchString(listenKey) {
		return UserDataListenKey{}, validationError("invalid MEXC listen key")
	}
	query := url.Values{"listenKey": {listenKey}}
	response, err := client.executeAPIKey(
		ctx, http.MethodDelete, userDataStreamPath, query, commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return UserDataListenKey{}, err
	}
	var result UserDataListenKey
	raw, err := client.decodeResponse(response, &result)
	if err != nil {
		return UserDataListenKey{}, err
	}
	if result.ListenKey != listenKey {
		return UserDataListenKey{}, client.decodeBodyError(
			response, errors.New("MEXC close returned a different listen key"),
		)
	}
	result.Raw = raw
	return result, nil
}

func (client *Client) executeAPIKey(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	operation commonexchange.OperationKind,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultEgressRouteID, options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	if resolved.Timeout == 0 {
		resolved.Timeout = client.requestTimeout
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeMEXC,
			Cause: errors.New("private MEXC user data stream requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeMEXC,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	if err := client.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeMEXC,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	charges, err := privateRateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, client.credentials.AccountID,
		privateLimit("user-data-stream", 1), client.endpointQuota,
		"private-read", client.privateReadQuota,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}

	baseQuery := cloneValues(query)
	var material credential.Material
	defer material.Destroy()
	return client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeMEXC, AccountID: client.credentials.AccountID,
		EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout,
		Charges: charges, Operation: operation,
		Build: func(buildContext context.Context) (*http.Request, error) {
			resolvedMaterial, resolveErr := client.credentialProvider.Resolve(
				buildContext, client.credentials.SecretRef,
			)
			material = resolvedMaterial
			if resolveErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeMEXC,
					AccountID: client.credentials.AccountID, Cause: resolveErr,
				}
			}
			if len(material.APIKey) == 0 {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeMEXC,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("MEXC API key is required"),
				}
			}
			request, requestErr := client.newRequest(method, path, baseQuery)
			if requestErr != nil {
				return nil, fmt.Errorf("create MEXC user data stream request: %w", requestErr)
			}
			request.Header.Set("X-MEXC-APIKEY", string(material.APIKey))
			return request, nil
		},
	})
}
