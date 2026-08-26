// Package trade는 거래소 공통 SDK 설정과 요청 옵션을 제공한다.
package trade

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/proven-trade/cex-sdk/transport"
)

var (
	// ErrMissingEgressRoute는 요청과 클라이언트 어디에도 송신 경로가 지정되지 않았음을 나타낸다.
	ErrMissingEgressRoute = errors.New("missing egress route")
	// ErrInvalidRequestOption은 잘못된 함수형 요청 옵션을 나타낸다.
	ErrInvalidRequestOption = errors.New("invalid request option")
)

// RequestOptions는 거래소 요청 한 건의 동작을 제어한다.
type RequestOptions struct {
	EgressRouteID transport.EgressRouteID
	Timeout       time.Duration
}

// RequestOption은 요청 한 건의 동작을 변경한다.
type RequestOption func(*RequestOptions) error

// WithEgressRoute는 거래소 클라이언트의 기본 송신 경로를 재정의한다.
func WithEgressRoute(routeID transport.EgressRouteID) RequestOption {
	return func(options *RequestOptions) error {
		cleanRouteID := strings.TrimSpace(string(routeID))
		if cleanRouteID == "" {
			return fmt.Errorf("%w: egress route ID is empty", ErrInvalidRequestOption)
		}
		options.EgressRouteID = transport.EgressRouteID(cleanRouteID)
		return nil
	}
}

// WithTimeout은 요청 한 건의 전체 제한 시간을 설정한다.
func WithTimeout(timeout time.Duration) RequestOption {
	return func(options *RequestOptions) error {
		if timeout <= 0 {
			return fmt.Errorf("%w: timeout must be positive", ErrInvalidRequestOption)
		}
		options.Timeout = timeout
		return nil
	}
}

// ResolveRequestOptions는 클라이언트 기본 경로에 요청별 옵션을 적용한다.
func ResolveRequestOptions(
	defaultRouteID transport.EgressRouteID,
	requestOptions ...RequestOption,
) (RequestOptions, error) {
	resolved := RequestOptions{
		EgressRouteID: transport.EgressRouteID(strings.TrimSpace(string(defaultRouteID))),
	}
	for index, apply := range requestOptions {
		if apply == nil {
			return RequestOptions{}, fmt.Errorf("%w: option %d is nil", ErrInvalidRequestOption, index)
		}
		if err := apply(&resolved); err != nil {
			return RequestOptions{}, err
		}
	}
	if strings.TrimSpace(string(resolved.EgressRouteID)) == "" {
		return RequestOptions{}, ErrMissingEgressRoute
	}
	return resolved, nil
}
