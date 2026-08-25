package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouteFlagsSet(t *testing.T) {
	t.Parallel()

	var routes routeFlags
	if err := routes.Set("seoul-a,127.0.0.1,203.0.113.10"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("len(routes) = %d, want 1", len(routes))
	}
	if routes[0].ID != "seoul-a" {
		t.Fatalf("ID = %q, want seoul-a", routes[0].ID)
	}
	if !routes[0].LocalSourceIP.Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("LocalSourceIP = %s, want 127.0.0.1", routes[0].LocalSourceIP)
	}
}

func TestRunReportsMatchingRoute(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("127.0.0.1\n"))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(
		context.Background(),
		[]string{
			"-endpoint", server.URL,
			"-route", "local,127.0.0.1,127.0.0.1",
		},
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatalf("run() error = %v, stderr = %s", err, stderr.String())
	}

	var results []routeResult
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, output = %s", err, stdout.String())
	}
	if len(results) != 1 || !results[0].MatchesExpected {
		t.Fatalf("results = %+v, want one matching route", results)
	}
	if !results[0].LocalSourceIP.Equal(net.ParseIP("127.0.0.1")) ||
		bytes.Contains(stdout.Bytes(), []byte("localPrivateIp")) {
		t.Fatalf("공급자 중립 진단 JSON = %s", stdout.String())
	}
}

func TestRunRequiresRoute(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(context.Background(), nil, &stdout, &stderr); err == nil {
		t.Fatal("run() error = nil, want an error")
	}
}
