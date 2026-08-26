package exchange

import (
	"context"
	"errors"
	"io"
	"net/http"
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
