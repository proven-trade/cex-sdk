// Package exchange는 거래소 어댑터가 공유하는 HTTP 실행 파이프라인을 제공한다.
package exchange

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

const defaultMaxResponseBodyBytes int64 = 32 << 20

// Sender는 선택한 송신 경로로 HTTP 요청을 보낼 수 있는 전송 계층이다.
type Sender interface {
	Do(context.Context, transport.EgressRouteID, *http.Request) (*http.Response, error)
}

// OperationKind는 네트워크 결과가 불명확할 때 mutation 여부를 판단하는 값이다.
type OperationKind uint8

const (
	OperationRead OperationKind = iota
	OperationMutation
)

// BuildRequest는 limiter 대기가 끝난 뒤 최종 직렬화와 서명을 수행한다.
type BuildRequest func(context.Context) (*http.Request, error)

// Execution은 HTTP 요청 한 건의 공통 실행 정보를 담는다.
type Execution struct {
	Exchange      model.ExchangeID
	AccountID     string
	EgressRouteID transport.EgressRouteID
	Timeout       time.Duration
	Charges       []ratelimit.Charge
	Operation     OperationKind
	Build         BuildRequest
}

// Response는 거래소 어댑터가 해석할 원본 HTTP 응답이다.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// Limiter는 실행기가 사용하는 요청 제한기를 반환한다.
// 여러 거래소 클라이언트가 같은 limiter를 공유할 때 정책을 갱신하는 데 사용한다.
func (executor *Executor) Limiter() *ratelimit.Limiter {
	return executor.limiter
}

// ExecutorConfig는 공통 실행 파이프라인의 의존성을 정의한다.
type ExecutorConfig struct {
	Sender               Sender
	Limiter              *ratelimit.Limiter
	MaxResponseBodyBytes int64
}

// Executor는 timeout, limiter, 전송, 응답 크기 제한을 공통 처리한다.
type Executor struct {
	sender               Sender
	limiter              *ratelimit.Limiter
	maxResponseBodyBytes int64
}

// NewExecutor는 검증된 공통 실행 파이프라인을 생성한다.
func NewExecutor(config ExecutorConfig) (*Executor, error) {
	if config.Sender == nil {
		return nil, fmt.Errorf("exchange executor sender is required")
	}
	if config.Limiter == nil {
		return nil, fmt.Errorf("exchange executor limiter is required")
	}
	if config.MaxResponseBodyBytes == 0 {
		config.MaxResponseBodyBytes = defaultMaxResponseBodyBytes
	}
	if config.MaxResponseBodyBytes < 0 {
		return nil, fmt.Errorf("maximum response body size cannot be negative")
	}
	return &Executor{
		sender:               config.Sender,
		limiter:              config.Limiter,
		maxResponseBodyBytes: config.MaxResponseBodyBytes,
	}, nil
}

// Execute는 limiter 확보 이후 요청을 만들고 지정한 송신 경로로 전송한다.
func (executor *Executor) Execute(ctx context.Context, execution Execution) (Response, error) {
	if ctx == nil {
		return Response{}, fmt.Errorf("execution context cannot be nil")
	}
	if !execution.Exchange.Valid() {
		return Response{}, fmt.Errorf("execution exchange is required")
	}
	if strings.TrimSpace(string(execution.EgressRouteID)) == "" {
		return Response{}, trade.ErrMissingEgressRoute
	}
	if execution.Build == nil {
		return Response{}, fmt.Errorf("request builder is required")
	}
	if execution.Operation != OperationRead && execution.Operation != OperationMutation {
		return Response{}, fmt.Errorf("unsupported operation kind %d", execution.Operation)
	}

	requestCtx := ctx
	cancel := func() {}
	if execution.Timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, execution.Timeout)
	}
	defer cancel()

	if err := executor.limiter.Wait(requestCtx, execution.Charges...); err != nil {
		category := trade.ErrorInternal
		retryable := false
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			category = trade.ErrorTimeout
			retryable = true
		}
		return Response{}, &trade.APIError{
			Category:  category,
			Exchange:  execution.Exchange,
			AccountID: execution.AccountID,
			Retryable: retryable,
			Cause:     err,
		}
	}

	request, err := execution.Build(requestCtx)
	if err != nil {
		return Response{}, err
	}
	if request == nil {
		return Response{}, fmt.Errorf("request builder returned nil request")
	}

	httpResponse, err := executor.sender.Do(requestCtx, execution.EgressRouteID, request)
	if err != nil {
		return Response{}, execution.transportError(err)
	}
	defer httpResponse.Body.Close()

	body, err := io.ReadAll(io.LimitReader(httpResponse.Body, executor.maxResponseBodyBytes+1))
	if err != nil {
		return Response{}, execution.responseError(fmt.Errorf("read response body: %w", err))
	}
	if int64(len(body)) > executor.maxResponseBodyBytes {
		return Response{}, execution.responseError(fmt.Errorf(
			"response body exceeds %d bytes",
			executor.maxResponseBodyBytes,
		))
	}

	response := Response{
		StatusCode: httpResponse.StatusCode,
		Header:     httpResponse.Header.Clone(),
		Body:       body,
	}
	if httpResponse.StatusCode == http.StatusTooManyRequests || httpResponse.StatusCode == http.StatusTeapot {
		executor.applyRetryAfter(response.Header, execution.Charges)
	}
	return response, nil
}

func (execution Execution) transportError(err error) error {
	if errors.Is(err, transport.ErrUnknownEgressRoute) ||
		errors.Is(err, transport.ErrRegistryClosed) ||
		errors.Is(err, transport.ErrLocalAddressUnavailable) {
		return &trade.APIError{
			Category:  trade.ErrorValidation,
			Exchange:  execution.Exchange,
			AccountID: execution.AccountID,
			Cause:     err,
		}
	}
	category := trade.ErrorNetwork
	retryable := execution.Operation == OperationRead
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		category = trade.ErrorTimeout
	}
	if execution.Operation == OperationMutation {
		category = trade.ErrorUnknownExecutionState
		retryable = false
	}
	return &trade.APIError{
		Category:  category,
		Exchange:  execution.Exchange,
		AccountID: execution.AccountID,
		Retryable: retryable,
		Cause:     err,
	}
}

func (execution Execution) responseError(err error) error {
	category := trade.ErrorExchangeUnavailable
	retryable := execution.Operation == OperationRead
	if execution.Operation == OperationMutation {
		category = trade.ErrorUnknownExecutionState
		retryable = false
	}
	return &trade.APIError{
		Category:  category,
		Exchange:  execution.Exchange,
		AccountID: execution.AccountID,
		Retryable: retryable,
		Cause:     err,
	}
}

func (executor *Executor) applyRetryAfter(headers http.Header, charges []ratelimit.Charge) {
	duration := retryAfterDuration(headers.Get("Retry-After"), time.Now())
	keys := make([]string, 0, len(charges))
	seen := make(map[string]struct{}, len(charges))
	for _, charge := range charges {
		if _, exists := seen[charge.Key]; exists {
			continue
		}
		seen[charge.Key] = struct{}{}
		keys = append(keys, charge.Key)
	}
	if len(keys) > 0 {
		_ = executor.limiter.BlockFor(keys, duration)
	}
}

func retryAfterDuration(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		return retryAt.Sub(now)
	}
	return time.Second
}
