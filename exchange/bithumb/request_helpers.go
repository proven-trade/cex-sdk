package bithumb

import (
	"regexp"
	"strings"
)

// feeCurrencyPattern은 URL 경로 한 구간으로 쓰는 화폐 심볼이다. 경로 구분자와 쿼리 문자를 허용하지 않는다.
var feeCurrencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)

// validateIdentifier는 해시 입력과 쿼리를 모호하게 만들 수 있는 문자가 없는 비어 있지 않은 식별자를 요구한다.
func validateIdentifier(name, value string) error {
	if value == "" {
		return validationError("%s is required", name)
	}
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "&=") {
		return validationError("invalid %s", name)
	}
	return nil
}

// validateCurrencyNetwork는 통화와 네트워크를 함께 요구하는 입출금 요청의 두 식별자를 검증한다.
func validateCurrencyNetwork(currency, netType string) error {
	if err := validateIdentifier("currency", currency); err != nil {
		return err
	}
	return validateIdentifier("netType", netType)
}

// validateText는 주소나 이름처럼 자유 형식인 값의 앞뒤 공백과 `&`를 거부한다.
// required가 false이면 빈 값을 허용한다.
func validateText(name, value string, required bool) error {
	if value == "" {
		if required {
			return validationError("%s is required", name)
		}
		return nil
	}
	if strings.TrimSpace(value) != value {
		return validationError("%s cannot have surrounding whitespace", name)
	}
	return validateParameterValue(name, value)
}

// validateIDList는 식별자 목록의 개수와 각 항목을 검증한다.
// limit이 0 이하이면 개수 제한을 두지 않고, pattern이 nil이 아니면 모든 항목이 일치해야 한다.
func validateIDList(name string, ids []string, limit int, pattern *regexp.Regexp) error {
	if limit > 0 && len(ids) > limit {
		return validationError("%s cannot contain more than %d items", name, limit)
	}
	for _, id := range ids {
		if err := validateIdentifier(name, id); err != nil {
			return err
		}
		if pattern != nil && !pattern.MatchString(id) {
			return validationError("invalid %s item %q", name, id)
		}
	}
	return nil
}

// validateListPaging은 입출금 목록 조회의 page, limit, order_by를 검증한다.
func validateListPaging(page, limit int, orderBy string) error {
	if page < 0 {
		return validationError("page cannot be negative")
	}
	if limit < 0 || limit > 100 {
		return validationError("limit must be between 1 and 100 or zero for default")
	}
	return validateOrderBy(orderBy)
}

// validateTransferIdentity는 개별 입출금 조회에서 UUID와 TxID 중 정확히 하나를 요구한다.
func validateTransferIdentity(uuid, txID string) error {
	if (uuid == "") == (txID == "") {
		return validationError("exactly one of UUID or TxID is required")
	}
	if uuid != "" {
		return validateIdentifier("UUID", uuid)
	}
	return validateIdentifier("TxID", txID)
}

// transferListParameters는 입출금 목록 조회의 공통 쿼리를 문서 순서대로 만든다.
func transferListParameters(
	currency, state string,
	uuids, txids []string,
	page, limit int,
	orderBy string,
) parameters {
	values := parameters{}
	values.add("currency", currency)
	values.add("state", state)
	values.addAll("uuids[]", uuids)
	values.addAll("txids[]", txids)
	values.addInt("page", page)
	values.addInt("limit", limit)
	values.add("order_by", orderBy)
	return values
}
