package upbit

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

var (
	currencyCodePattern       = regexp.MustCompile(`^[A-Z0-9]{1,20}$`)
	transferIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
)

// addAll은 같은 키를 목록 순서대로 반복해 추가한다. 예: uuids[]=a&uuids[]=b
func (values *parameters) addAll(key string, items []string) {
	for _, item := range items {
		values.add(key, item)
	}
}

// decodeObject는 단일 JSON 객체 응답을 decode하고 원문을 setRaw로 보존한다.
func decodeObject[T any](
	client *Client,
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	setRaw func(*T, []byte),
) (T, error) {
	var value T
	if err := client.decodeResponse(response, operation, &value); err != nil {
		var zero T
		return zero, err
	}
	if setRaw != nil {
		setRaw(&value, cloneBytes(response.Body))
	}
	return value, nil
}

// executeSignedPost는 파라미터 순서를 유지한 JSON 본문으로 서명된 POST를 보낸다.
// POST는 모두 변경 요청이므로 자동 재시도하지 않고, 결과가 불명확하면 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) executeSignedPost(
	ctx context.Context,
	path string,
	params parameters,
	group rateGroup,
	permission credential.Permission,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	body, err := encodeOrderBody(params)
	if err != nil {
		return commonexchange.Response{}, validationError("encode request body: %v", err)
	}
	return client.executeSigned(
		ctx, http.MethodPost, path, params, body, group,
		permission, commonexchange.OperationMutation, options...,
	)
}

// validateCurrencyCode는 KRW, BTC, USDT 같은 대문자 통화 코드를 검사한다.
func validateCurrencyCode(name, value string) error {
	if !currencyCodePattern.MatchString(value) {
		return validationError("%s must be an uppercase currency code", name)
	}
	return nil
}

// validateRequestValue는 UUID, txid, 주소처럼 그대로 전송하는 값의 공백과 제어 문자를 거부한다.
// 쿼리 해시는 디코드된 문자열로 계산하므로 공백이 섞이면 서버가 계산한 해시와 달라진다.
func validateRequestValue(name, value string) error {
	if value == "" || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return validationError("%s must not be empty or contain whitespace or control characters", name)
	}
	return nil
}

// validateOptionalRequestValue는 비어 있지 않은 선택 값만 검사한다.
func validateOptionalRequestValue(name, value string) error {
	if value == "" {
		return nil
	}
	return validateRequestValue(name, value)
}

// validateValueList는 반복 파라미터로 보낼 값 목록의 개수, 공백, 중복을 검사한다.
// limit이 0이면 개수 상한을 두지 않는다.
func validateValueList(name string, values []string, limit int) error {
	if len(values) == 0 {
		return validationError("%s must contain at least one item", name)
	}
	if limit > 0 && len(values) > limit {
		return validationError("%s must contain at most %d items", name, limit)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateRequestValue(name, value); err != nil {
			return err
		}
		if _, exists := seen[value]; exists {
			return validationError("duplicate %s %q", name, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

// validateOptionalValueList는 목록이 비어 있으면 통과시키고 값이 있으면 validateValueList로 검사한다.
func validateOptionalValueList(name string, values []string, limit int) error {
	if len(values) == 0 {
		return nil
	}
	return validateValueList(name, values, limit)
}

// validateTransferIdentifier는 자산 이전 요청 식별자를 검사한다. 비어 있으면 거래소가 null로 처리한다.
func validateTransferIdentifier(identifier string) error {
	if identifier != "" && !transferIdentifierPattern.MatchString(identifier) {
		return validationError("identifier must be 1-64 characters of letters, digits, underscore, dot, or hyphen")
	}
	return nil
}

// validatePageLimit는 limit이 0(기본값)이거나 1~maximum 범위인지 검사한다.
func validatePageLimit(name string, limit, maximum int) error {
	if limit < 0 || limit > maximum {
		return validationError("%s limit must be between 1 and %d or zero for default", name, maximum)
	}
	return nil
}

// validateCurrencyList는 쉼표로 묶어 보낼 통화 코드 목록의 형식과 중복을 검사한다.
func validateCurrencyList(name string, currencies []string) error {
	if len(currencies) == 0 {
		return validationError("%s list must contain at least one item", name)
	}
	seen := make(map[string]struct{}, len(currencies))
	for _, currency := range currencies {
		if err := validateCurrencyCode(name, currency); err != nil {
			return err
		}
		if _, exists := seen[currency]; exists {
			return validationError("duplicate %s %q", name, currency)
		}
		seen[currency] = struct{}{}
	}
	return nil
}
