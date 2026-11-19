package coinone

import (
	"context"
	"errors"
	"net/http"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

func TestCancelAllOrdersUsesOrderBucketAndTradePermission(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, successReply(`"canceled_count":15,"total_order_count":15`), successReply(`"canceled_count":3,"total_order_count":5`))
	provider := &recordingProvider{}
	client, limiter := newExtTestClient(
		t, server.URL, &directSender{}, provider, credential.PermissionRead, credential.PermissionTrade,
	)
	request := CancelAllOrdersRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC"}

	result, err := client.CancelAllOrders(context.Background(), request)
	if err != nil {
		t.Fatalf("CancelAllOrders() error = %v", err)
	}
	if result.CanceledCount != 15 || result.TotalOrderCount != 15 || len(result.Raw) == 0 {
		t.Fatalf("CancelAllOrders() = %+v", result)
	}
	partial, err := client.CancelAllOrders(context.Background(), request)
	if err != nil || partial.CanceledCount != 3 || partial.TotalOrderCount != 5 {
		t.Fatalf("CancelAllOrders() partial = %+v, error = %v", partial, err)
	}

	requests := server.received()
	if len(requests) != 2 {
		t.Fatalf("requests = %+v, want two", requests)
	}
	for _, got := range requests {
		assertPrivateRequest(t, got, "/v2.1/order/cancel/all", `,"quote_currency":"KRW","target_currency":"BTC"`)
	}
	calls, key, secret := provider.snapshot()
	if calls != 2 || !allZero(key) || !allZero(secret) {
		t.Fatalf("provider calls = %d, key zero = %v, secret zero = %v", calls, allZero(key), allZero(secret))
	}
	assertLimiterCharged(t, limiter, extOrderKey)
	assertLimiterUnknown(t, limiter, extPrivateKey)
}

func TestCancelAllOrdersRequiresTradePermission(t *testing.T) {
	t.Parallel()

	server := newExtServer(t)
	sender := &directSender{}
	provider := &recordingProvider{}
	client, _ := newExtTestClient(t, server.URL, sender, provider, credential.PermissionRead)

	_, err := client.CancelAllOrders(context.Background(), CancelAllOrdersRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC"})
	if !errors.Is(err, trade.ErrAuthorization) || !errors.Is(err, credential.ErrPermissionNotAllowed) {
		t.Fatalf("CancelAllOrders() error = %v, want ErrAuthorization", err)
	}
	if calls, _, _ := provider.snapshot(); calls != 0 || len(sender.snapshot()) != 0 || len(server.received()) != 0 {
		t.Fatalf("provider calls = %d, routes = %v, requests = %v", calls, sender.snapshot(), server.received())
	}
}

func TestCancelAllOrdersRejectsInvalidPair(t *testing.T) {
	t.Parallel()

	server := newExtServer(t)
	client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionTrade)

	_, missingErr := client.CancelAllOrders(context.Background(), CancelAllOrdersRequest{QuoteCurrency: "KRW"})
	_, samePairErr := client.CancelAllOrders(context.Background(), CancelAllOrdersRequest{QuoteCurrency: "BTC", TargetCurrency: "BTC"})
	for index, err := range []error{missingErr, samePairErr} {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v, want ErrValidation", index, err)
		}
	}
	if requests := server.received(); len(requests) != 0 {
		t.Fatalf("requests = %+v, want none", requests)
	}
}

func TestCancelAllOrdersNeverRetriesAndReportsUnknownState(t *testing.T) {
	t.Parallel()

	request := CancelAllOrdersRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC"}
	networkClient, _ := newExtTestClient(
		t, "http://127.0.0.1", errorSender{}, &recordingProvider{}, credential.PermissionTrade,
	)
	if _, err := networkClient.CancelAllOrders(context.Background(), request); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CancelAllOrders() network error = %v, want ErrUnknownExecutionState", err)
	}

	tests := []struct {
		name  string
		reply extReply
	}{
		{"server error", errorReply(http.StatusInternalServerError, "500", "internal error")},
		{"missing canceled count", successReply(`"total_order_count":4`)},
		{"missing total count", successReply(`"canceled_count":4`)},
		{"empty result", successReply("")},
	}
	for _, test := range tests {
		server := newExtServer(t, test.reply)
		client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionTrade)
		_, err := client.CancelAllOrders(context.Background(), request)
		if !errors.Is(err, trade.ErrUnknownExecutionState) {
			t.Fatalf("%s: CancelAllOrders() error = %v, want ErrUnknownExecutionState", test.name, err)
		}
		server.single(t)
	}
}

func TestCancelAllOrdersKeepsExchangeRejection(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, errorReply(http.StatusOK, "4", "Blocked by rate limit"))
	client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionTrade)

	_, err := client.CancelAllOrders(context.Background(), CancelAllOrdersRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC"})
	if !errors.Is(err, trade.ErrRateLimited) {
		t.Fatalf("CancelAllOrders() error = %v, want ErrRateLimited", err)
	}
	assertExchangeError(t, err, trade.ErrorRateLimited, "4")
	server.single(t)
}
