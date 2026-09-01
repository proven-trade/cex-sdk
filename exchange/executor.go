// Package exchange는 거래소 어댑터가 공유하는 HTTP 실행 파이프라인을 제공한다.
package exchange

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
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

var defaultReadRetryPolicy = ReadRetryPolicy{
	MaxAttempts: 2,
	BaseDelay:   50 * time.Millisecond,
	MaxDelay:    500 * time.Millisecond,
}

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
	EndpointID    string
	AccountID     string
	EgressRouteID transport.EgressRouteID
	Timeout       time.Duration
	Charges       []ratelimit.Charge
	Operation     OperationKind
	Build         BuildRequest
}

// ReadRetryPolicy는 읽기 전용 REST 요청에만 적용되는 제한적 재시도 정책이다.
// mutation은 이 정책과 무관하게 자동 재시도하지 않는다.
type ReadRetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// ExecutionObservation은 credential이나 query를 포함하지 않는 실행 관측값이다.
type ExecutionObservation struct {
	Exchange      model.ExchangeID
	EndpointID    string
	EgressRouteID transport.EgressRouteID
	Operation     OperationKind
	Attempt       int
	Duration      time.Duration
	StatusCode    int
	ErrorCategory trade.ErrorCategory
}

// ExecutionObserver는 REST attempt별 latency, status와 공통 오류 분류를 수집한다.
type ExecutionObserver interface {
	ObserveExecution(ExecutionObservation)
}

// ExecutionObserverFunc는 함수를 ExecutionObserver로 사용하게 한다.
type ExecutionObserverFunc func(ExecutionObservation)

func (observe ExecutionObserverFunc) ObserveExecution(value ExecutionObservation) {
	observe(value)
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
	ReadRetryPolicy      *ReadRetryPolicy
	Observer             ExecutionObserver
}

// Executor는 timeout, limiter, 전송, 응답 크기 제한을 공통 처리한다.
type Executor struct {
	sender               Sender
	limiter              *ratelimit.Limiter
	maxResponseBodyBytes int64
	readRetryPolicy      ReadRetryPolicy
	observer             ExecutionObserver
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
	retryPolicy := defaultReadRetryPolicy
	if config.ReadRetryPolicy != nil {
		retryPolicy = *config.ReadRetryPolicy
	}
	if err := retryPolicy.validate(); err != nil {
		return nil, err
	}
	return &Executor{
		sender:               config.Sender,
		limiter:              config.Limiter,
		maxResponseBodyBytes: config.MaxResponseBodyBytes,
		readRetryPolicy:      retryPolicy,
		observer:             config.Observer,
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

	maxAttempts := 1
	if execution.Operation == OperationRead {
		maxAttempts = executor.readRetryPolicy.MaxAttempts
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		response, retry, err := executor.executeAttempt(requestCtx, execution, attempt)
		if err == nil && !retry {
			return response, nil
		}
		if !retry || attempt == maxAttempts {
			return response, err
		}
		if waitErr := executor.waitBeforeRetry(requestCtx, attempt); waitErr != nil {
			return Response{}, execution.transportError(waitErr)
		}
	}
	return Response{}, fmt.Errorf("execution retry loop exhausted")
}

func (executor *Executor) executeAttempt(
	requestCtx context.Context,
	execution Execution,
	attempt int,
) (Response, bool, error) {
	started := time.Now()
	endpointID := execution.EndpointID
	if err := executor.limiter.Wait(requestCtx, execution.Charges...); err != nil {
		category := trade.ErrorInternal
		retryable := false
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			category = trade.ErrorTimeout
			retryable = true
		}
		apiError := &trade.APIError{
			Category:  category,
			Exchange:  execution.Exchange,
			AccountID: execution.AccountID,
			Retryable: retryable,
			Cause:     err,
		}
		executor.observe(execution, endpointID, attempt, started, 0, apiError)
		return Response{}, false, apiError
	}

	request, err := execution.Build(requestCtx)
	if err != nil {
		executor.observe(execution, endpointID, attempt, started, 0, err)
		return Response{}, false, err
	}
	if request == nil {
		err = fmt.Errorf("request builder returned nil request")
		executor.observe(execution, endpointID, attempt, started, 0, err)
		return Response{}, false, err
	}
	if endpointID == "" && request.URL != nil {
		endpointID = request.Method + " " + request.URL.Path
	}

	httpResponse, err := executor.sender.Do(requestCtx, execution.EgressRouteID, request)
	if err != nil {
		apiError := execution.transportError(err)
		retry := execution.Operation == OperationRead && retryableAPIError(apiError) && requestCtx.Err() == nil
		executor.observe(execution, endpointID, attempt, started, 0, apiError)
		return Response{}, retry, apiError
	}

	body, err := io.ReadAll(io.LimitReader(httpResponse.Body, executor.maxResponseBodyBytes+1))
	_ = httpResponse.Body.Close()
	if err != nil {
		apiError := execution.responseError(fmt.Errorf("read response body: %w", err))
		retry := execution.Operation == OperationRead && requestCtx.Err() == nil
		executor.observe(execution, endpointID, attempt, started, httpResponse.StatusCode, apiError)
		return Response{}, retry, apiError
	}
	if int64(len(body)) > executor.maxResponseBodyBytes {
		apiError := execution.responseError(fmt.Errorf(
			"response body exceeds %d bytes",
			executor.maxResponseBodyBytes,
		))
		executor.observe(execution, endpointID, attempt, started, httpResponse.StatusCode, apiError)
		return Response{}, false, apiError
	}

	response := Response{
		StatusCode: httpResponse.StatusCode,
		Header:     httpResponse.Header.Clone(),
		Body:       body,
	}
	if httpResponse.StatusCode == http.StatusTooManyRequests || httpResponse.StatusCode == http.StatusTeapot {
		executor.applyRetryAfter(response.Header, execution.Charges)
	}
	retry := execution.Operation == OperationRead && retryableReadStatus(response.StatusCode)
	executor.observe(execution, endpointID, attempt, started, response.StatusCode, nil)
	return response, retry, nil
}

func (policy ReadRetryPolicy) validate() error {
	if policy.MaxAttempts < 1 || policy.MaxAttempts > 5 {
		return fmt.Errorf("read retry max attempts must be between 1 and 5")
	}
	if policy.BaseDelay < 0 || policy.MaxDelay < 0 || policy.MaxDelay < policy.BaseDelay {
		return fmt.Errorf("read retry delays must be non-negative and max delay must be at least base delay")
	}
	return nil
}

func (executor *Executor) waitBeforeRetry(ctx context.Context, attempt int) error {
	delay := executor.readRetryPolicy.BaseDelay
	for retry := 1; retry < attempt && delay < executor.readRetryPolicy.MaxDelay; retry++ {
		if delay > executor.readRetryPolicy.MaxDelay/2 {
			delay = executor.readRetryPolicy.MaxDelay
			break
		}
		delay *= 2
	}
	if delay > executor.readRetryPolicy.MaxDelay {
		delay = executor.readRetryPolicy.MaxDelay
	}
	if delay > 0 {
		delay = time.Duration(rand.Int64N(int64(delay) + 1))
	}
	if delay == 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryableReadStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryableAPIError(err error) bool {
	var apiError *trade.APIError
	return errors.As(err, &apiError) && apiError.Retryable
}

func (executor *Executor) observe(
	execution Execution,
	endpointID string,
	attempt int,
	started time.Time,
	statusCode int,
	err error,
) {
	if executor.observer == nil {
		return
	}
	category := trade.ErrorCategory("")
	var apiError *trade.APIError
	if errors.As(err, &apiError) {
		category = apiError.Category
	}
	executor.observer.ObserveExecution(ExecutionObservation{
		Exchange: execution.Exchange, EndpointID: endpointID,
		EgressRouteID: execution.EgressRouteID, Operation: execution.Operation,
		Attempt: attempt, Duration: time.Since(started), StatusCode: statusCode,
		ErrorCategory: category,
	})
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
