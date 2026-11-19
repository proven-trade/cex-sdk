package korbit

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// maxAccountSeq는 공식 문서가 정의한 계정 번호의 최댓값이다.
const maxAccountSeq = 9999

var (
	networkPattern    = regexp.MustCompile(`^[0-9A-Za-z._-]{1,32}$`)
	requestKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,36}$`)
)

// validateMainAccountSeq는 입출금 API가 메인 계정(accountSeq 1)만 허용하므로
// 0(파라미터 생략)과 1만 허용한다.
func validateMainAccountSeq(accountSeq int) error {
	if accountSeq != 0 && accountSeq != 1 {
		return validationError("deposit and withdrawal APIs only support the main account (accountSeq 1)")
	}
	return nil
}

// validateRequiredAccountSeq는 생략하면 BAD_REQUEST가 되는 계정 번호를 1~9999로 검증한다.
func validateRequiredAccountSeq(name string, accountSeq int) error {
	if accountSeq < 1 || accountSeq > maxAccountSeq {
		return validationError("%s must be between 1 and %d", name, maxAccountSeq)
	}
	return nil
}

func validateCurrency(currency string) error {
	if !currencyPattern.MatchString(currency) {
		return validationError("invalid currency %q", currency)
	}
	return nil
}

func validateOptionalCurrency(currency string) error {
	if currency == "" {
		return nil
	}
	return validateCurrency(currency)
}

func validateNetwork(network string) error {
	if !networkPattern.MatchString(network) {
		return validationError("invalid network %q", network)
	}
	return nil
}

func validateOptionalNetwork(network string) error {
	if network == "" {
		return nil
	}
	return validateNetwork(network)
}

// validateOptionalLimit는 0(거래소 기본값)과 1~maximum만 허용한다.
func validateOptionalLimit(name string, limit, maximum int) error {
	if limit < 0 || limit > maximum {
		return validationError("%s limit must be between 1 and %d or zero for default", name, maximum)
	}
	return nil
}

// validateRequiredLimit는 생략할 수 없는 limit을 1~maximum으로 검증한다.
func validateRequiredLimit(name string, limit, maximum int) error {
	if limit < 1 || limit > maximum {
		return validationError("%s limit must be between 1 and %d", name, maximum)
	}
	return nil
}

func validatePositiveID(name string, id int64) error {
	if id <= 0 {
		return validationError("%s must be positive", name)
	}
	return nil
}

func validateRequestKey(requestKey string) error {
	if !requestKeyPattern.MatchString(requestKey) {
		return validationError("request key is required and must match [A-Za-z0-9._-]{1,36}")
	}
	return nil
}

// validateWithdrawalAddress는 체인별 주소 형식을 추측하지 않고 비어 있거나 공백·제어 문자가 있는 값만 거부한다.
func validateWithdrawalAddress(address string) error {
	rejected := func(character rune) bool { return unicode.IsSpace(character) || unicode.IsControl(character) }
	if address == "" || strings.IndexFunc(address, rejected) >= 0 {
		return validationError("withdrawal address is required and cannot contain whitespace or control characters")
	}
	return nil
}

// validateSecondaryAddress는 destination tag·memo에서 제어 문자만 거부한다.
func validateSecondaryAddress(secondaryAddress string) error {
	if strings.IndexFunc(secondaryAddress, unicode.IsControl) >= 0 {
		return validationError("secondary address cannot contain control characters")
	}
	return nil
}

// validateTransferQty는 소수점 이하 8자리 이하의 양수 십진수만 허용한다.
// 지수 표기는 validatePositiveDecimal이 거부한다.
func validateTransferQty(qty string) error {
	if err := validatePositiveDecimal("transfer quantity", qty); err != nil {
		return err
	}
	if _, fraction, found := strings.Cut(qty, "."); found && len(fraction) > 8 {
		return validationError("transfer quantity cannot have more than 8 decimal places")
	}
	return nil
}

// setRequiredAccountSeq는 0이어도 생략하지 않고 accountSeq를 전송한다.
func setRequiredAccountSeq(values url.Values, key string, accountSeq int) {
	values.Set(key, strconv.Itoa(accountSeq))
}
