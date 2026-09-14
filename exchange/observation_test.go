package exchange

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	trade "github.com/proven-trade/cex-sdk/v2"
	"github.com/proven-trade/cex-sdk/v2/transport"
)

func TestResponseObservationIncludesHTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		status    int
		operation OperationKind
		category  trade.ErrorCategory
	}{
		{200, OperationRead, ""}, {401, OperationRead, trade.ErrorAuthentication},
		{403, OperationRead, trade.ErrorAuthorization}, {429, OperationRead, trade.ErrorRateLimited},
		{500, OperationRead, trade.ErrorExchangeUnavailable}, {500, OperationMutation, trade.ErrorUnknownExecutionState},
		{400, OperationMutation, trade.ErrorValidation},
	} {
		t.Run(string(tc.category)+http.StatusText(tc.status), func(t *testing.T) {
			var observations []ExecutionObservation
			executor, err := NewExecutor(ExecutorConfig{
				Sender: senderFunc(func(context.Context, transport.EgressRouteID, *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
				}),
				Limiter: emptyLimiter(t), ReadRetryPolicy: &ReadRetryPolicy{MaxAttempts: 1},
				Observer: ExecutionObserverFunc(func(o ExecutionObservation) { observations = append(observations, o) }),
			})
			if err != nil {
				t.Fatal(err)
			}
			response, err := executor.Execute(context.Background(), validExecution(tc.operation))
			if err != nil || response.StatusCode != tc.status {
				t.Fatalf("observation changed result: %+v %v", response, err)
			}
			if len(observations) != 1 || observations[0].ErrorCategory != tc.category {
				t.Fatalf("observations=%+v", observations)
			}
		})
	}
}

func TestLogicalResponseObservationDoesNotRetryOrChangeResponse(t *testing.T) {
	var observed ExecutionObservation
	var sends, classifications int
	executor, err := NewExecutor(ExecutorConfig{
		Sender: senderFunc(func(context.Context, transport.EgressRouteID, *http.Request) (*http.Response, error) {
			sends++
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"retCode":10006}`))}, nil
		}),
		Limiter: emptyLimiter(t), Observer: ExecutionObserverFunc(func(o ExecutionObservation) { observed = o }),
	})
	if err != nil {
		t.Fatal(err)
	}
	execution := validExecution(OperationRead)
	execution.ClassifyResponse = func(r Response, op OperationKind) error {
		classifications++
		if op != OperationRead || string(r.Body) != `{"retCode":10006}` {
			t.Fatal("classifier lost response")
		}
		return &trade.APIError{Category: trade.ErrorRateLimited, Retryable: true}
	}
	response, err := executor.Execute(context.Background(), execution)
	if err != nil || string(response.Body) != `{"retCode":10006}` || sends != 1 || classifications != 1 || observed.ErrorCategory != trade.ErrorRateLimited {
		t.Fatalf("result=%+v %v sends=%d classifications=%d observation=%+v", response, err, sends, classifications, observed)
	}
	executor.observer = nil
	if _, err := executor.Execute(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	if classifications != 1 {
		t.Fatal("classifier ran without observer")
	}
	executor.observer = ExecutionObserverFunc(func(o ExecutionObservation) { observed = o })
	execution.Build = func(context.Context) (*http.Request, error) { return nil, errors.New("build failed") }
	_, _ = executor.Execute(context.Background(), execution)
	if observed.ErrorCategory != trade.ErrorInternal {
		t.Fatalf("unclassified build failure: %+v", observed)
	}
}
