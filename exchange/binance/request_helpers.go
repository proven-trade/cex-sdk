package binance

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// readPublic은 API Key가 필요 없는 GET 요청을 보내고 성공 응답의 본문을 반환한다.
func (client *Client) readPublic(
	ctx context.Context,
	path string,
	values url.Values,
	requestWeight int,
	options ...trade.RequestOption,
) ([]byte, error) {
	response, _, err := client.executePublic(ctx, http.MethodGet, path, values, requestWeight, options...)
	if err != nil {
		return nil, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationRead); err != nil {
		return nil, err
	}
	return response.Body, nil
}

// readSigned는 read 권한이 필요한 서명 GET 요청을 보내고 성공 응답의 본문을 반환한다.
func (client *Client) readSigned(
	ctx context.Context,
	path string,
	values url.Values,
	requestWeight int,
	options ...trade.RequestOption,
) ([]byte, error) {
	response, _, err := client.executeSigned(
		ctx,
		http.MethodGet,
		path,
		values,
		requestWeight,
		0,
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return nil, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationRead); err != nil {
		return nil, err
	}
	return response.Body, nil
}

// mutateSigned는 trade 권한이 필요한 서명 변경 요청을 보내고 성공 응답의 본문을 반환한다.
// 변경 요청은 자동 재시도하지 않으며 orderUnits는 계정의 미체결 주문 count에 더해진다.
func (client *Client) mutateSigned(
	ctx context.Context,
	method, path string,
	values url.Values,
	requestWeight, orderUnits int,
	options ...trade.RequestOption,
) ([]byte, error) {
	response, _, err := client.executeSigned(
		ctx,
		method,
		path,
		values,
		requestWeight,
		orderUnits,
		credential.PermissionTrade,
		commonexchange.OperationMutation,
		options...,
	)
	if err != nil {
		return nil, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationMutation); err != nil {
		return nil, err
	}
	return response.Body, nil
}

// decodeList는 JSON 배열 응답을 원소별 원본과 함께 디코딩한다.
// setRaw가 원소의 Raw 필드에 원본 JSON을 저장한다.
func decodeList[T any](body []byte, setRaw func(*T, json.RawMessage)) ([]T, error) {
	var rawItems []json.RawMessage
	if err := decodeJSON(body, &rawItems); err != nil {
		return nil, err
	}
	return decodeRawItems(rawItems, setRaw)
}

// decodeObjectOrList는 단일 객체 또는 JSON 배열 응답을 원소 목록으로 디코딩한다.
// Binance는 symbol 하나를 조회하면 객체를, 여러 상품을 조회하면 배열을 반환한다.
func decodeObjectOrList[T any](body []byte, setRaw func(*T, json.RawMessage)) ([]T, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return decodeRawItems([]json.RawMessage{trimmed}, setRaw)
	}
	return decodeList(trimmed, setRaw)
}

func decodeRawItems[T any](rawItems []json.RawMessage, setRaw func(*T, json.RawMessage)) ([]T, error) {
	items := make([]T, len(rawItems))
	for index, rawItem := range rawItems {
		if err := decodeJSON(rawItem, &items[index]); err != nil {
			return nil, err
		}
		setRaw(&items[index], cloneBytes(rawItem))
	}
	return items, nil
}

func validateOptionalLimit(name string, limit, maximum int) error {
	if limit < 0 || limit > maximum {
		return validationError("%s limit must be between 1 and %d or zero for default", name, maximum)
	}
	return nil
}

// validateDayWindow는 startTime과 endTime이 둘 다 있을 때 순서와 24시간 이내 간격을 검사한다.
func validateDayWindow(name string, startTime, endTime *time.Time) error {
	if startTime == nil || endTime == nil {
		return nil
	}
	if startTime.After(*endTime) {
		return validationError("%s startTime cannot be after endTime", name)
	}
	if endTime.Sub(*startTime) > 24*time.Hour {
		return validationError("%s startTime and endTime cannot span more than 24 hours", name)
	}
	return nil
}

func validateOptionalDecimal(name, value string) error {
	if value == "" {
		return nil
	}
	return validatePositiveDecimal(name, value)
}

func validateSide(name string, side Side) error {
	if side != SideBuy && side != SideSell {
		return validationError("%s must be BUY or SELL", name)
	}
	return nil
}

func validatePeg(name, priceType, offsetType string, offsetValue *int) error {
	if (offsetValue == nil) != (offsetType == "") {
		return validationError("%s pegOffsetValue and pegOffsetType must be set together", name)
	}
	if priceType != "" && priceType != "PRIMARY_PEG" && priceType != "MARKET_PEG" {
		return validationError("%s pegPriceType must be PRIMARY_PEG or MARKET_PEG", name)
	}
	if offsetType != "" && offsetType != "PRICE_LEVEL" {
		return validationError("%s pegOffsetType must be PRICE_LEVEL", name)
	}
	if offsetValue != nil && (*offsetValue < 0 || *offsetValue > 100) {
		return validationError("%s pegOffsetValue must be between 0 and 100", name)
	}
	return nil
}

func setLimit(values url.Values, limit int) {
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
}
