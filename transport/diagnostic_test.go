package transport

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyPublicIPAcceptsPlainTextAndJSON(t *testing.T) {
	t.Parallel()

	responses := map[string]string{
		"/text": "127.0.0.1\n",
		"/json": `{"ip":"127.0.0.1"}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(responses[request.URL.Path]))
	}))
	defer server.Close()

	registry, err := NewRegistry(
		[]EgressRoute{{
			ID:               "route-a",
			LocalSourceIP:    net.ParseIP("127.0.0.1"),
			ExpectedPublicIP: net.ParseIP("127.0.0.1"),
		}},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	defer registry.Close()

	for path := range responses {
		check, checkErr := registry.VerifyPublicIP(context.Background(), "route-a", server.URL+path)
		if checkErr != nil {
			t.Fatalf("VerifyPublicIP(%q) error = %v", path, checkErr)
		}
		if !check.MatchesExpected {
			t.Fatalf("VerifyPublicIP(%q) MatchesExpected = false", path)
		}
		if !check.LocalSourceIP.Equal(net.ParseIP("127.0.0.1")) || check.LocalPrivateIP != nil {
			t.Fatalf("VerifyPublicIP(%q) source IP = %s", path, check.LocalSourceIP)
		}
	}
}

func TestVerifyPublicIPReportsMismatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("127.0.0.1"))
	}))
	defer server.Close()

	registry, err := NewRegistry(
		[]EgressRoute{{
			ID:               "route-a",
			LocalSourceIP:    net.ParseIP("127.0.0.1"),
			ExpectedPublicIP: net.ParseIP("127.0.0.2"),
		}},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	defer registry.Close()

	check, err := registry.VerifyPublicIP(context.Background(), "route-a", server.URL)
	if !errors.Is(err, ErrPublicIPMismatch) {
		t.Fatalf("VerifyPublicIP() error = %v, want ErrPublicIPMismatch", err)
	}
	if check.MatchesExpected {
		t.Fatal("MatchesExpected = true, want false")
	}
}
