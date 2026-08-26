package cryptocom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
)

type privateRequestEnvelope struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	APIKey string         `json:"api_key"`
	Params map[string]any `json:"params"`
	Nonce  string         `json:"nonce"`
	Sig    string         `json:"sig"`
}

func (client *Client) executePrivate(
	ctx context.Context,
	method string,
	params map[string]any,
	permission credential.Permission,
	operation commonexchange.OperationKind,
	options ...trade.RequestOption,
) (commonexchange.Response, string, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultEgressRouteID, options...)
	if err != nil {
		return commonexchange.Response{}, "", err
	}
	if resolved.Timeout == 0 {
		resolved.Timeout = client.requestTimeout
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return commonexchange.Response{}, "", &trade.APIError{
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeCryptoCom,
			Cause: errors.New("private Crypto.com request requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, "", &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeCryptoCom,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	if err := client.credentials.RequirePermission(permission); err != nil {
		return commonexchange.Response{}, "", &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeCryptoCom,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	limitMethod := strings.TrimPrefix(method, "private/")
	charges, err := privateRateLimit(
		client.executor.Limiter(), client.credentials.AccountID, limitMethod,
		client.orderRequestsPer100Milliseconds,
		client.orderDetailRequestsPer100Milliseconds,
		client.historyRequestsPerSecond,
		client.otherPrivateRequestsPer100Milliseconds,
	)
	if err != nil {
		return commonexchange.Response{}, "", err
	}
	paramsCopy, err := clonePrivateParams(params)
	if err != nil {
		return commonexchange.Response{}, "", &trade.APIError{
			Category: trade.ErrorInternal, Exchange: model.ExchangeCryptoCom,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	requestIDValue := client.requestID.Add(1)
	if requestIDValue <= 0 {
		return commonexchange.Response{}, "", &trade.APIError{
			Category: trade.ErrorInternal, Exchange: model.ExchangeCryptoCom,
			AccountID: client.credentials.AccountID,
			Cause:     errors.New("Crypto.com request ID space is exhausted"),
		}
	}
	requestID := strconv.FormatInt(requestIDValue, 10)

	var material credential.Material
	var requestBody []byte
	defer material.Destroy()
	defer func() { zeroBytes(requestBody) }()
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeCryptoCom, AccountID: client.credentials.AccountID,
		EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout,
		Charges: charges, Operation: operation,
		Build: func(buildContext context.Context) (*http.Request, error) {
			resolvedMaterial, resolveErr := client.credentialProvider.Resolve(
				buildContext, client.credentials.SecretRef,
			)
			material = resolvedMaterial
			if resolveErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeCryptoCom,
					AccountID: client.credentials.AccountID, Cause: resolveErr,
				}
			}
			if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeCryptoCom,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("Crypto.com API key and HMAC secret are required"),
				}
			}
			nonceValue := client.now().UTC().UnixMilli()
			if nonceValue <= 0 {
				return nil, validationError("Crypto.com nonce must be after the Unix epoch")
			}
			nonce := strconv.FormatInt(nonceValue, 10)
			signature, signErr := Sign(
				method, requestID, material.APIKey, paramsCopy, nonce, material.SecretKey,
			)
			if signErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorInternal, Exchange: model.ExchangeCryptoCom,
					AccountID: client.credentials.AccountID, Cause: signErr,
				}
			}
			body, marshalErr := json.Marshal(privateRequestEnvelope{
				ID: requestID, Method: method, APIKey: string(material.APIKey),
				Params: paramsCopy, Nonce: nonce, Sig: signature,
			})
			if marshalErr != nil {
				return nil, fmt.Errorf("encode Crypto.com private request: %w", marshalErr)
			}
			requestBody = body
			return client.newPrivateRequest(method, requestBody)
		},
	})
	return response, requestID, err
}

func (client *Client) newPrivateRequest(method string, body []byte) (*http.Request, error) {
	requestURL := *client.baseURL
	requestURL.Path = strings.TrimRight(client.baseURL.Path, "/") + "/" + strings.TrimLeft(method, "/")
	request, err := http.NewRequest(http.MethodPost, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Crypto.com private request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "cex-sdk-go/0")
	return request, nil
}

func (client *Client) decodePrivateResult(
	response commonexchange.Response,
	expectedMethod string,
	expectedID string,
	operation commonexchange.OperationKind,
	resultRequired bool,
) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(response.Body)
	if len(trimmed) == 0 || !json.Valid(trimmed) || trimmed[0] != '{' {
		return nil, client.decodePrivateBodyError(
			response, operation, "", errors.New("Crypto.com response is not a JSON object"),
		)
	}
	var envelope struct {
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Code    json.RawMessage `json:"code"`
		Result  json.RawMessage `json:"result"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, client.decodePrivateBodyError(response, operation, "", err)
	}
	code, err := optionalScalarText(envelope.Code)
	if err != nil || len(bytes.TrimSpace(envelope.Code)) == 0 || code == "" {
		if err == nil {
			err = errors.New("Crypto.com response code is missing")
		}
		return nil, client.decodePrivateBodyError(response, operation, "", err)
	}
	requestID, err := optionalScalarText(envelope.ID)
	if err != nil {
		return nil, client.decodePrivateBodyError(response, operation, "", err)
	}
	httpSuccess := response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices
	if !httpSuccess || code != "0" {
		return nil, client.privateAPIError(
			response, expectedMethod, code, envelope.Message, requestID, operation, nil,
		)
	}
	if envelope.Method != expectedMethod || requestID != expectedID {
		return nil, client.decodePrivateBodyError(
			response, operation, requestID,
			fmt.Errorf("unexpected Crypto.com private response method %q or ID %q", envelope.Method, requestID),
		)
	}
	resultRaw := bytes.TrimSpace(envelope.Result)
	if len(resultRaw) == 0 || bytes.Equal(resultRaw, []byte("null")) {
		if !resultRequired {
			return nil, nil
		}
		return nil, client.decodePrivateBodyError(
			response, operation, requestID, errors.New("Crypto.com response result is missing"),
		)
	}
	return cloneBytes(resultRaw), nil
}

func (client *Client) privateAPIError(
	response commonexchange.Response,
	method string,
	code string,
	message string,
	envelopeID string,
	operation commonexchange.OperationKind,
	cause error,
) error {
	category, retryable := classifyPrivateError(response.StatusCode, method, code, operation)
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeCryptoCom,
		AccountID: client.credentials.AccountID,
		RequestID: firstNonEmpty(
			response.Header.Get("X-Request-ID"), response.Header.Get("X-Request-Id"), envelopeID,
		),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		ExchangeCode: code, ExchangeMessage: message, Cause: cause,
	}
}

func (client *Client) decodePrivateBodyError(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	envelopeID string,
	cause error,
) error {
	category := trade.ErrorExchangeUnavailable
	retryable := true
	if operation == commonexchange.OperationMutation {
		category = trade.ErrorUnknownExecutionState
		retryable = false
	}
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeCryptoCom, AccountID: accountID,
		RequestID: firstNonEmpty(
			response.Header.Get("X-Request-ID"), response.Header.Get("X-Request-Id"), envelopeID,
		),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		Cause: fmt.Errorf("decode Crypto.com private JSON response: %w", cause),
	}
}

func classifyPrivateError(
	status int,
	method string,
	code string,
	operation commonexchange.OperationKind,
) (trade.ErrorCategory, bool) {
	code = strings.TrimSpace(code)
	switch code {
	case "306":
		return trade.ErrorInsufficientBalance, false
	case "212", "30004", "40401":
		if method == "private/get-order-detail" || method == "private/cancel-order" {
			return trade.ErrorOrderNotFound, false
		}
	case "40101", "40102", "10002":
		return trade.ErrorAuthentication, false
	case "40301", "10003":
		return trade.ErrorAuthorization, false
	case "42901":
		return trade.ErrorRateLimited, true
	case "40801", "50001":
		if operation == commonexchange.OperationMutation {
			return trade.ErrorUnknownExecutionState, false
		}
		return trade.ErrorExchangeUnavailable, true
	}
	if status == http.StatusTooManyRequests {
		return trade.ErrorRateLimited, true
	}
	if status == http.StatusUnauthorized {
		return trade.ErrorAuthentication, false
	}
	if status == http.StatusForbidden {
		return trade.ErrorAuthorization, false
	}
	if code != "" && code != "0" {
		return trade.ErrorValidation, false
	}
	if status >= http.StatusInternalServerError || status == http.StatusRequestTimeout {
		if operation == commonexchange.OperationMutation {
			return trade.ErrorUnknownExecutionState, false
		}
		return trade.ErrorExchangeUnavailable, true
	}
	if status >= http.StatusBadRequest {
		return trade.ErrorValidation, false
	}
	return trade.ErrorInternal, false
}

func (client *Client) decodePrivateDataItems(
	response commonexchange.Response,
	resultRaw json.RawMessage,
	operation commonexchange.OperationKind,
	requestID string,
) ([]json.RawMessage, error) {
	var result struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		return nil, client.decodePrivateBodyError(response, operation, requestID, err)
	}
	dataRaw := bytes.TrimSpace(result.Data)
	if len(dataRaw) == 0 || dataRaw[0] != '[' {
		return nil, client.decodePrivateBodyError(
			response, operation, requestID,
			errors.New("Crypto.com private response data is not an array"),
		)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(dataRaw, &items); err != nil {
		return nil, client.decodePrivateBodyError(response, operation, requestID, err)
	}
	return items, nil
}

func clonePrivateParams(params map[string]any) (map[string]any, error) {
	if params == nil {
		return map[string]any{}, nil
	}
	value, err := clonePrivateParameterValue(params, 0)
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}

func clonePrivateParameterValue(value any, depth int) (any, error) {
	if depth > maximumParameterDepth {
		return nil, fmt.Errorf("Crypto.com private parameters exceed maximum nesting depth")
	}
	switch typed := value.(type) {
	case nil, string, bool:
		return typed, nil
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			cloned, err := clonePrivateParameterValue(item, depth+1)
			if err != nil {
				return nil, err
			}
			result[index] = cloned
		}
		return result, nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned, err := clonePrivateParameterValue(item, depth+1)
			if err != nil {
				return nil, err
			}
			result[key] = cloned
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported Crypto.com private parameter type %T", value)
	}
}
