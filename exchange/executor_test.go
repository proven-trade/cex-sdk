package exchange

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

type senderFunc func(context.Context, transport.EgressRouteID, *http.Request) (*http.Response, error)

func (sender senderFunc) Do(
	ctx context.Context,
	routeID transport.EgressRouteID,
	request *http.Request,
) (*http.Response, error) {
	return sender(ctx, routeID, request)
}

func TestExecuteBuildsRequestAfterLimiterWait(t *testing.T) {
	t.Parallel()

	limiter, err := ratelimit.New(ratelimit.Rule{Key: "full", Limit: 1, Window: time.Second})
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	if err := limiter.Wait(context.Background(), ratelimit.Charge{Key: "full", Units: 1}); err != nil {
		t.Fatalf("initial limiter.Wait() error = %v", err)
	}
	executor := mustExecutor(t, senderFunc(func(
		context.Context,
		transport.EgressRouteID,
		*http.Request,
	) (*http.Response, error) {
		t.Fatal("sender was called")
		return nil, nil
	}), limiter)

	built := false
	_, err = executor.Execute(context.Background(), Execution{
		Exchange:      model.ExchangeBinance,
		EgressRouteID: "route-a",
		Timeout:       10 * time.Millisecond,
		Charges:       []ratelimit.Charge{{Key: "full", Units: 1}},
		Operation:     OperationMutation,
		Build: func(context.Context) (*http.Request, error) {
			built = true
			return http.NewRequest(http.MethodPost, "https://example.com", nil)
		},
	})
	if !errors.Is(err, trade.ErrTimeout) {
		t.Fatalf("Execute() error = %v, want ErrTimeout", err)
	}
	if built {
		t.Fatal("request was built before rate limit capacity became available")
	}
}

func TestExecuteClassifiesMutationNetworkFailureAsUnknown(t *testing.T) {
	t.Parallel()

	networkError := errors.New("connection reset")
	executor := mustExecutor(t, senderFunc(func(
		context.Context,
		transport.EgressRouteID,
		*http.Request,
	) (*http.Response, error) {
		return nil, networkError
	}), emptyLimiter(t))

	_, err := executor.Execute(context.Background(), validExecution(OperationMutation))
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("Execute() error = %v, want ErrUnknownExecutionState", err)
	}
	if errors.Is(err, trade.ErrNetwork) {
		t.Fatal("mutation error must not be exposed as an automatically retryable network error")
	}
}

func TestExecuteClassifiesReadNetworkFailureAsRetryable(t *testing.T) {
	t.Parallel()

	executor := mustExecutor(t, senderFunc(func(
		context.Context,
		transport.EgressRouteID,
		*http.Request,
	) (*http.Response, error) {
		return nil, errors.New("connection reset")
	}), emptyLimiter(t))

	_, err := executor.Execute(context.Background(), validExecution(OperationRead))
	if !errors.Is(err, trade.ErrNetwork) {
		t.Fatalf("Execute() error = %v, want ErrNetwork", err)
	}
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || !apiError.Retryable {
		t.Fatalf("APIError = %+v, want retryable", apiError)
	}
}

func TestExecuteReturnsBodyAndClonedHeaders(t *testing.T) {
	t.Parallel()

	header := make(http.Header)
	header.Set("X-Test", "value")
	executor := mustExecutor(t, senderFunc(func(
		context.Context,
		transport.EgressRouteID,
		*http.Request,
	) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}, nil
	}), emptyLimiter(t))

	response, err := executor.Execute(context.Background(), validExecution(OperationRead))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	header.Set("X-Test", "changed")
	if got := response.Header.Get("X-Test"); got != "value" {
		t.Fatalf("response header = %q, want value", got)
	}
	if got := string(response.Body); got != `{"ok":true}` {
		t.Fatalf("response body = %q", got)
	}
}

func TestExecuteRetriesReadsAndRebuildsAfterEachCharge(t *testing.T) {
	t.Parallel()
	var builds, sends int
	observations := make([]ExecutionObservation, 0, 2)
	executor, err := NewExecutor(ExecutorConfig{
		Sender: senderFunc(func(context.Context, transport.EgressRouteID, *http.Request) (*http.Response, error) {
			sends++
			if sends == 1 {
				return nil, errors.New("temporary disconnect")
			}
			return &http.Response{
				StatusCode: http.StatusOK, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		}),
		Limiter:         emptyLimiter(t),
		ReadRetryPolicy: &ReadRetryPolicy{MaxAttempts: 2},
		Observer: ExecutionObserverFunc(func(value ExecutionObservation) {
			observations = append(observations, value)
		}),
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	execution := validExecution(OperationRead)
	execution.Build = func(context.Context) (*http.Request, error) {
		builds++
		return http.NewRequest(http.MethodGet, "https://example.com/ticker?signature=secret", nil)
	}
	response, err := executor.Execute(context.Background(), execution)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("Execute() = %+v, %v", response, err)
	}
	if builds != 2 || sends != 2 {
		t.Fatalf("builds = %d, sends = %d, want 2, 2", builds, sends)
	}
	if len(observations) != 2 || observations[0].ErrorCategory != trade.ErrorNetwork ||
		observations[1].StatusCode != http.StatusOK || observations[1].EndpointID != "GET /ticker" {
		t.Fatalf("observations = %+v", observations)
	}
}

func TestExecuteRetriesSelectedReadStatusesButNotMutations(t *testing.T) {
	t.Parallel()
	for _, operation := range []OperationKind{OperationRead, OperationMutation} {
		operation := operation
		t.Run(strconv.Itoa(int(operation)), func(t *testing.T) {
			calls := 0
			executor, err := NewExecutor(ExecutorConfig{
				Sender: senderFunc(func(context.Context, transport.EgressRouteID, *http.Request) (*http.Response, error) {
					calls++
					return &http.Response{
						StatusCode: http.StatusServiceUnavailable, Header: make(http.Header),
						Body: io.NopCloser(strings.NewReader(`{"error":"busy"}`)),
					}, nil
				}),
				Limiter: emptyLimiter(t), ReadRetryPolicy: &ReadRetryPolicy{MaxAttempts: 2},
			})
			if err != nil {
				t.Fatalf("NewExecutor() error = %v", err)
			}
			response, err := executor.Execute(context.Background(), validExecution(operation))
			if err != nil || response.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("Execute() = %+v, %v", response, err)
			}
			wantCalls := 2
			if operation == OperationMutation {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("calls = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func mustExecutor(t *testing.T, sender Sender, limiter *ratelimit.Limiter) *Executor {
	t.Helper()
	executor, err := NewExecutor(ExecutorConfig{Sender: sender, Limiter: limiter})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	return executor
}

func emptyLimiter(t *testing.T) *ratelimit.Limiter {
	t.Helper()
	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	return limiter
}

func validExecution(operation OperationKind) Execution {
	return Execution{
		Exchange:      model.ExchangeBinance,
		EgressRouteID: "route-a",
		Operation:     operation,
		Build: func(context.Context) (*http.Request, error) {
			return http.NewRequest(http.MethodGet, "https://example.com", nil)
		},
	}
}

func TestStreamReturnsUnbufferedBodyBeyondResponseLimit(t *testing.T) {
	t.Parallel()

	limiter, err := ratelimit.New(ratelimit.Rule{Key: "archive", Limit: 10, Window: time.Second})
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	var requestCtx context.Context
	payload := strings.Repeat("x", 64)
	executor, err := NewExecutor(ExecutorConfig{
		Sender: senderFunc(func(ctx context.Context, routeID transport.EgressRouteID, _ *http.Request) (*http.Response, error) {
			requestCtx = ctx
			if routeID != "route-a" {
				t.Fatalf("route = %s", routeID)
			}
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"X-Test": {"value"}},
				Body: io.NopCloser(strings.NewReader(payload)),
			}, nil
		}),
		Limiter:              limiter,
		MaxResponseBodyBytes: 8,
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	execution := validExecution(OperationRead)
	execution.Timeout = time.Second
	execution.Charges = []ratelimit.Charge{{Key: "archive", Units: 1}}
	response, err := executor.Stream(context.Background(), execution)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != payload || response.Header.Get("X-Test") != "value" {
		t.Fatalf("Stream() body = %q, header = %v, error = %v", body, response.Header, err)
	}
	if requestCtx.Err() != nil {
		t.Fatal("request context was canceled before Close")
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if requestCtx.Err() == nil {
		t.Fatal("Close() did not release the request context")
	}
	snapshot, err := limiter.Snapshot("archive")
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestStreamAppliesTimeoutOnlyUntilHeaders(t *testing.T) {
	t.Parallel()

	headerWait := mustExecutor(t, senderFunc(func(ctx context.Context, _ transport.EgressRouteID, _ *http.Request) (*http.Response, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}), emptyLimiter(t))
	execution := validExecution(OperationRead)
	execution.Timeout = 10 * time.Millisecond
	if _, err := headerWait.Stream(context.Background(), execution); !errors.Is(err, trade.ErrTimeout) {
		t.Fatalf("Stream() header wait error = %v, want ErrTimeout", err)
	}

	slowBody := mustExecutor(t, senderFunc(func(ctx context.Context, _ transport.EgressRouteID, _ *http.Request) (*http.Response, error) {
		reader, writer := io.Pipe()
		go func() {
			time.Sleep(30 * time.Millisecond)
			if ctx.Err() != nil {
				_ = writer.CloseWithError(ctx.Err())
				return
			}
			_, _ = writer.Write([]byte("late body"))
			_ = writer.Close()
		}()
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: reader}, nil
	}), emptyLimiter(t))
	response, err := slowBody.Stream(context.Background(), execution)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "late body" {
		t.Fatalf("slow body = %q, error = %v", body, err)
	}
}

func TestStreamRejectsMutations(t *testing.T) {
	t.Parallel()

	executor := mustExecutor(t, senderFunc(func(context.Context, transport.EgressRouteID, *http.Request) (*http.Response, error) {
		t.Fatal("sender was called")
		return nil, nil
	}), emptyLimiter(t))
	if _, err := executor.Stream(context.Background(), validExecution(OperationMutation)); err == nil {
		t.Fatal("Stream() mutation error = nil")
	}
}
