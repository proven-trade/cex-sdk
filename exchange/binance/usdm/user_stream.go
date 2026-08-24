package usdm

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
	"github.com/proven-trade/proven-trade-sdk/model"
)

const userDataStreamPath = "/fapi/v1/listenKey"

// UserDataListenKey는 Binance USDⓈ-M Futures private stream 접속 키다.
type UserDataListenKey struct {
	ListenKey string `json:"listenKey"`
}

// StartUserDataStream은 현재 API Key 계정의 listenKey를 생성하거나 갱신한다.
func (client *Client) StartUserDataStream(
	ctx context.Context,
	options ...trade.RequestOption,
) (UserDataListenKey, error) {
	response, err := client.executeAPIKey(
		ctx, http.MethodPost, userDataStreamPath, options...,
	)
	if err != nil {
		return UserDataListenKey{}, err
	}
	var result UserDataListenKey
	if err := client.decode(response, commonexchange.OperationRead, &result); err != nil {
		return UserDataListenKey{}, err
	}
	if result.ListenKey == "" {
		return UserDataListenKey{}, fmt.Errorf("Binance USD-M listen key response is empty")
	}
	return result, nil
}

// KeepaliveUserDataStream은 현재 API Key 계정의 listenKey 수명을 연장한다.
func (client *Client) KeepaliveUserDataStream(
	ctx context.Context,
	options ...trade.RequestOption,
) (UserDataListenKey, error) {
	response, err := client.executeAPIKey(
		ctx, http.MethodPut, userDataStreamPath, options...,
	)
	if err != nil {
		return UserDataListenKey{}, err
	}
	var result UserDataListenKey
	if err := client.decode(response, commonexchange.OperationRead, &result); err != nil {
		return UserDataListenKey{}, err
	}
	return result, nil
}

// CloseUserDataStream은 현재 API Key 계정의 listenKey를 무효화한다.
func (client *Client) CloseUserDataStream(
	ctx context.Context,
	options ...trade.RequestOption,
) error {
	response, err := client.executeAPIKey(
		ctx, http.MethodDelete, userDataStreamPath, options...,
	)
	if err != nil {
		return err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return client.decodeError(response, commonexchange.OperationRead, nil)
	}
	return nil
}

func (client *Client) executeAPIKey(
	ctx context.Context,
	method string,
	path string,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	resolved, err := client.resolveOptions(options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeBinance,
			Cause: errors.New("private Binance USD-M user data stream requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeBinance,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	if err := client.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeBinance,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	charges, err := client.limits.charges(
		client.executor.Limiter(), resolved.EgressRouteID, client.credentials.AccountID, 1, 0,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}
	var material credential.Material
	defer material.Destroy()
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeBinance, AccountID: client.credentials.AccountID,
		EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout,
		Charges: charges, Operation: commonexchange.OperationRead,
		Build: func(buildContext context.Context) (*http.Request, error) {
			resolvedMaterial, resolveErr := client.credentialProvider.Resolve(
				buildContext, client.credentials.SecretRef,
			)
			material = resolvedMaterial
			if resolveErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeBinance,
					AccountID: client.credentials.AccountID, Cause: resolveErr,
				}
			}
			if len(material.APIKey) == 0 {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeBinance,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("Binance USD-M API key is required"),
				}
			}
			request, requestErr := client.newRequest(method, path, nil)
			if requestErr != nil {
				return nil, requestErr
			}
			request.Header.Set("X-MBX-APIKEY", string(material.APIKey))
			return request, nil
		},
	})
	if err == nil {
		observeHeaders(
			client.executor.Limiter(), resolved.EgressRouteID,
			client.credentials.AccountID, response.Header,
		)
	}
	return response, err
}
