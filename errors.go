package trade

import (
	"errors"
	"fmt"

	"github.com/proven-trade/cex-sdk/model"
)

// ErrorCategory는 여러 거래소의 오류를 공통 의미로 분류한다.
type ErrorCategory string

const (
	ErrorAuthentication        ErrorCategory = "AUTHENTICATION"
	ErrorAuthorization         ErrorCategory = "AUTHORIZATION"
	ErrorValidation            ErrorCategory = "VALIDATION"
	ErrorInsufficientBalance   ErrorCategory = "INSUFFICIENT_BALANCE"
	ErrorOrderNotFound         ErrorCategory = "ORDER_NOT_FOUND"
	ErrorRateLimited           ErrorCategory = "RATE_LIMITED"
	ErrorNetwork               ErrorCategory = "NETWORK"
	ErrorTimeout               ErrorCategory = "TIMEOUT"
	ErrorUnknownExecutionState ErrorCategory = "UNKNOWN_EXECUTION_STATE"
	ErrorExchangeUnavailable   ErrorCategory = "EXCHANGE_UNAVAILABLE"
	ErrorUnsupportedCapability ErrorCategory = "UNSUPPORTED_CAPABILITY"
	ErrorInternal              ErrorCategory = "INTERNAL"
)

var (
	ErrAuthentication        = errors.New("authentication failed")
	ErrAuthorization         = errors.New("authorization failed")
	ErrValidation            = errors.New("validation failed")
	ErrInsufficientBalance   = errors.New("insufficient balance")
	ErrOrderNotFound         = errors.New("order not found")
	ErrRateLimited           = errors.New("rate limited")
	ErrNetwork               = errors.New("network error")
	ErrTimeout               = errors.New("request timeout")
	ErrUnknownExecutionState = errors.New("execution state is unknown")
	ErrExchangeUnavailable   = errors.New("exchange unavailable")
	ErrUnsupportedCapability = errors.New("unsupported capability")
	ErrInternal              = errors.New("internal error")
)

// APIError는 공통 분류와 거래소 원본 오류 정보를 함께 보존한다.
// 민감한 요청 헤더나 서명 원문은 이 구조체에 저장하면 안 된다.
type APIError struct {
	Category        ErrorCategory
	Exchange        model.ExchangeID
	AccountID       string
	RequestID       string
	Retryable       bool
	HTTPStatus      int
	ExchangeCode    string
	ExchangeMessage string
	Cause           error
}

// Error는 Secret을 포함하지 않는 범위에서 오류를 문자열로 표현한다.
func (apiError *APIError) Error() string {
	if apiError == nil {
		return "<nil>"
	}
	message := fmt.Sprintf("%s request failed", apiError.Exchange)
	if apiError.ExchangeCode != "" {
		message += fmt.Sprintf(" with code %s", apiError.ExchangeCode)
	}
	if apiError.ExchangeMessage != "" {
		message += ": " + apiError.ExchangeMessage
	}
	if apiError.ExchangeMessage == "" && apiError.Cause != nil && apiError.safeToRenderCause() {
		message += ": " + apiError.Cause.Error()
	} else if apiError.ExchangeMessage == "" && apiError.Category != "" {
		// 인증·전송·불명확한 실행 오류의 Cause는 credential provider나 net/http에서
		// 왔을 수 있다. 문자열에는 allowlist된 공통 분류만 사용하고 Cause는
		// errors.Is/errors.As 용도로만 보존한다.
		message += ": " + string(apiError.Category)
	}
	return message
}

func (apiError *APIError) safeToRenderCause() bool {
	switch apiError.Category {
	case ErrorValidation, ErrorUnsupportedCapability:
		return true
	default:
		return false
	}
}

// Unwrap은 공통 분류 오류와 원본 원인을 errors.Is/errors.As에 노출한다.
func (apiError *APIError) Unwrap() []error {
	if apiError == nil {
		return nil
	}
	unwrapped := make([]error, 0, 2)
	if categoryError := sentinelForCategory(apiError.Category); categoryError != nil {
		unwrapped = append(unwrapped, categoryError)
	}
	if apiError.Cause != nil {
		unwrapped = append(unwrapped, apiError.Cause)
	}
	return unwrapped
}

func sentinelForCategory(category ErrorCategory) error {
	switch category {
	case ErrorAuthentication:
		return ErrAuthentication
	case ErrorAuthorization:
		return ErrAuthorization
	case ErrorValidation:
		return ErrValidation
	case ErrorInsufficientBalance:
		return ErrInsufficientBalance
	case ErrorOrderNotFound:
		return ErrOrderNotFound
	case ErrorRateLimited:
		return ErrRateLimited
	case ErrorNetwork:
		return ErrNetwork
	case ErrorTimeout:
		return ErrTimeout
	case ErrorUnknownExecutionState:
		return ErrUnknownExecutionState
	case ErrorExchangeUnavailable:
		return ErrExchangeUnavailable
	case ErrorUnsupportedCapability:
		return ErrUnsupportedCapability
	case ErrorInternal:
		return ErrInternal
	default:
		return nil
	}
}
