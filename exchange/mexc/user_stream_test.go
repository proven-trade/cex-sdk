package mexc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const testMEXCListenKey = "ListenKey1234567890"

func TestMEXCUserDataStreamRESTLifecycleUsesSelectedRoute(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path != userDataStreamPath ||
			request.Header.Get("X-MEXC-APIKEY") != "test-api-key" ||
			request.URL.Query().Get("signature") != "" ||
			request.URL.Query().Get("timestamp") != "" {
			http.Error(writer, `{"code":400,"msg":"invalid request"}`, http.StatusBadRequest)
			return
		}
		switch request.Method {
		case http.MethodPost:
			if request.URL.RawQuery != "" {
				http.Error(writer, `{"code":400,"msg":"unexpected query"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"listenKey":"`+testMEXCListenKey+`"}`)
		case http.MethodGet:
			_, _ = io.WriteString(writer, `{"listenKey":["`+testMEXCListenKey+`"]}`)
		case http.MethodPut, http.MethodDelete:
			if request.URL.Query().Get("listenKey") != testMEXCListenKey {
				http.Error(writer, `{"code":400,"msg":"invalid listen key"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"listenKey":"`+testMEXCListenKey+`"}`)
		default:
			http.Error(writer, `{"code":400,"msg":"invalid method"}`, http.StatusBadRequest)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newPrivateTestClient(
		t, server.URL, sender, provider,
		[]transport.EgressRouteID{"route-a", "route-b"},
		[]credential.Permission{credential.PermissionRead}, time.Now(),
	)
	started, err := client.StartUserDataStream(
		context.Background(), trade.WithEgressRoute("route-b"),
	)
	if err != nil || started.ListenKey != testMEXCListenKey || len(started.Raw) == 0 {
		t.Fatalf("StartUserDataStream() = %+v, error = %v", started, err)
	}
	listed, err := client.UserDataStreams(
		context.Background(), trade.WithEgressRoute("route-b"),
	)
	if err != nil || !slices.Equal(listed.ListenKeys, []string{testMEXCListenKey}) ||
		len(listed.Raw) == 0 {
		t.Fatalf("UserDataStreams() = %+v, error = %v", listed, err)
	}
	kept, err := client.KeepaliveUserDataStream(
		context.Background(), testMEXCListenKey, trade.WithEgressRoute("route-b"),
	)
	if err != nil || kept.ListenKey != testMEXCListenKey || len(kept.Raw) == 0 {
		t.Fatalf("KeepaliveUserDataStream() = %+v, error = %v", kept, err)
	}
	closed, err := client.CloseUserDataStream(
		context.Background(), testMEXCListenKey, trade.WithEgressRoute("route-b"),
	)
	if err != nil || closed.ListenKey != testMEXCListenKey || len(closed.Raw) == 0 {
		t.Fatalf("CloseUserDataStream() = %+v, error = %v", closed, err)
	}
	if routes := sender.snapshot(); !slices.Equal(routes, []transport.EgressRouteID{
		"route-b", "route-b", "route-b", "route-b",
	}) {
		t.Fatalf("sender routes = %v", routes)
	}
	calls, apiKey, secret := provider.snapshot()
	if calls != 4 || !allZero(apiKey) || !allZero(secret) {
		t.Fatalf(
			"provider calls = %d, key zero = %v, secret zero = %v",
			calls, allZero(apiKey), allZero(secret),
		)
	}
	assertLimiterUsed(
		t, limiter, "mexc:route:route-b:private:user-data-stream:10seconds", 40,
	)
	assertPrivateFrequency(
		t, limiter, "mexc:account:mexc-main:private-read:1second", 4, DefaultPrivateReadQuota,
	)
}

func TestMEXCUserDataStreamRESTRejectsRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()
	provider := &recordingProvider{}
	client, _ := newPrivateTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"},
		[]credential.Permission{credential.PermissionRead}, time.Now(),
	)
	_, err := client.StartUserDataStream(
		context.Background(), trade.WithEgressRoute("route-b"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("StartUserDataStream() error = %v, want authorization", err)
	}
	if calls, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
	if _, err := client.KeepaliveUserDataStream(
		context.Background(), "bad-key!",
	); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("KeepaliveUserDataStream() error = %v, want validation", err)
	}
}

func TestMEXCStartUserDataStreamClassifiesMalformedSuccessAsUnknown(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer server.Close()
	client, _ := newPrivateTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"},
		[]credential.Permission{credential.PermissionRead}, time.Now(),
	)
	_, err := client.StartUserDataStream(context.Background())
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("StartUserDataStream() error = %v, want unknown execution state", err)
	}
}
