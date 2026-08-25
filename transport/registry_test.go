package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestRegistryBindsEachRouteToItsOwnSourceIP(t *testing.T) {
	t.Parallel()

	nonLoopbackIP := firstNonLoopbackIPv4(t)
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}

	var (
		mu       sync.Mutex
		observed = make(map[string]string)
	)
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		host, _, splitErr := net.SplitHostPort(request.RemoteAddr)
		if splitErr != nil {
			http.Error(writer, splitErr.Error(), http.StatusInternalServerError)
			return
		}
		mu.Lock()
		observed[request.URL.Path] = host
		mu.Unlock()
		writer.WriteHeader(http.StatusNoContent)
	})}
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if shutdownErr := server.Shutdown(ctx); shutdownErr != nil {
			t.Errorf("server.Shutdown() error = %v", shutdownErr)
		}
		if serveErr := <-serverDone; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			t.Errorf("server.Serve() error = %v", serveErr)
		}
	})

	routes := []EgressRoute{
		{ID: "route-a", LocalSourceIP: net.ParseIP("127.0.0.1")},
		{ID: "route-b", LocalSourceIP: nonLoopbackIP},
	}
	registry, err := NewRegistry(
		routes,
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	listenerPort := listener.Addr().(*net.TCPAddr).Port
	destinations := map[EgressRouteID]string{
		"route-a": "127.0.0.1",
		"route-b": nonLoopbackIP.String(),
	}
	for _, route := range routes {
		request, requestErr := http.NewRequest(
			http.MethodGet,
			fmt.Sprintf("http://%s:%d/%s", destinations[route.ID], listenerPort, route.ID),
			nil,
		)
		if requestErr != nil {
			t.Fatalf("http.NewRequest() error = %v", requestErr)
		}
		response, requestErr := registry.Do(context.Background(), route.ID, request)
		if requestErr != nil {
			t.Fatalf("Do(%q) error = %v", route.ID, requestErr)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Fatalf("response.Body.Close() error = %v", closeErr)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if got := observed["/route-a"]; got != "127.0.0.1" {
		t.Fatalf("route-a source IP = %q, want 127.0.0.1", got)
	}
	if got := observed["/route-b"]; got != nonLoopbackIP.String() {
		t.Fatalf("route-b source IP = %q, want %s", got, nonLoopbackIP)
	}
}

func firstNonLoopbackIPv4(t *testing.T) net.IP {
	t.Helper()

	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("net.InterfaceAddrs() error = %v", err)
	}
	for _, address := range addresses {
		ipNet, ok := address.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP.To4()
		if ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			return ip
		}
	}

	t.Skip("source IP 분리 테스트에 사용할 non-loopback IPv4 주소가 없습니다")
	return nil
}

func TestRegistryRejectsDuplicateRouteIDsAndLocalIPs(t *testing.T) {
	t.Parallel()

	verify := WithLocalAddressVerifier(func(net.IP) error { return nil })
	tests := map[string][]EgressRoute{
		"route ID": {
			{ID: "duplicate", LocalSourceIP: net.ParseIP("10.0.0.1")},
			{ID: "duplicate", LocalSourceIP: net.ParseIP("10.0.0.2")},
		},
		"local IP": {
			{ID: "route-a", LocalSourceIP: net.ParseIP("10.0.0.1")},
			{ID: "route-b", LocalSourceIP: net.ParseIP("10.0.0.1")},
		},
	}
	for name, routes := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewRegistry(routes, verify)
			if !errors.Is(err, ErrInvalidEgressRoute) {
				t.Fatalf("NewRegistry() error = %v, want ErrInvalidEgressRoute", err)
			}
		})
	}
}

func TestRegistryRejectsAddressMissingFromHost(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry([]EgressRoute{
		{ID: "route-a", LocalSourceIP: net.ParseIP("10.0.0.21")},
	}, WithLocalAddressVerifier(func(ip net.IP) error {
		return fmt.Errorf("%w: %s", ErrLocalAddressUnavailable, ip)
	}))
	if registry != nil {
		_ = registry.Close()
	}
	if !errors.Is(err, ErrLocalAddressUnavailable) {
		t.Fatalf("NewRegistry() error = %v, want ErrLocalAddressUnavailable", err)
	}
}

func TestRegistryDisablesEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")

	registry, err := NewRegistry(
		[]EgressRoute{{ID: "route-a", LocalPrivateIP: net.ParseIP("127.0.0.1")}},
		WithLocalAddressVerifier(func(net.IP) error { return nil }),
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	defer registry.Close()

	entry, err := registry.lookup("route-a")
	if err != nil {
		t.Fatalf("lookup() error = %v", err)
	}
	if entry.transport.Proxy != nil {
		t.Fatal("transport.Proxy is configured; environment proxy must be disabled")
	}
}

func TestRegistryRejectsRequestsAfterClose(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(
		[]EgressRoute{{ID: "route-a", LocalPrivateIP: net.ParseIP("127.0.0.1")}},
		WithLocalAddressVerifier(func(net.IP) error { return nil }),
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if err := registry.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, "http://127.0.0.1", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	_, err = registry.Do(context.Background(), "route-a", request)
	if !errors.Is(err, ErrRegistryClosed) {
		t.Fatalf("Do() error = %v, want ErrRegistryClosed", err)
	}
}

func TestRegistryHTTPClientUsesBoundRouteAndRejectsRedirect(t *testing.T) {
	t.Parallel()

	var (
		observedMu        sync.Mutex
		destinationCalled bool
		sourceIP          string
	)
	destination := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		observedMu.Lock()
		destinationCalled = true
		observedMu.Unlock()
		writer.WriteHeader(http.StatusNoContent)
	})}
	destinationListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("destination listener error = %v", err)
	}
	go func() { _ = destination.Serve(destinationListener) }()
	t.Cleanup(func() { _ = destination.Close() })

	redirect := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observedMu.Lock()
		sourceIP, _, _ = net.SplitHostPort(request.RemoteAddr)
		observedMu.Unlock()
		http.Redirect(writer, request, "http://"+destinationListener.Addr().String(), http.StatusFound)
	})}
	redirectListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("redirect listener error = %v", err)
	}
	go func() { _ = redirect.Serve(redirectListener) }()
	t.Cleanup(func() { _ = redirect.Close() })

	registry, err := NewRegistry([]EgressRoute{{ID: "route-a", LocalPrivateIP: net.ParseIP("127.0.0.1")}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	client, err := registry.HTTPClient("route-a")
	if err != nil {
		t.Fatalf("HTTPClient() error = %v", err)
	}
	response, err := client.Get("http://" + redirectListener.Addr().String())
	if err != nil {
		t.Fatalf("client.Get() error = %v", err)
	}
	_ = response.Body.Close()
	observedMu.Lock()
	defer observedMu.Unlock()
	if response.StatusCode != http.StatusFound || destinationCalled || sourceIP != "127.0.0.1" {
		t.Fatalf("status = %d, destination called = %v, source IP = %q", response.StatusCode, destinationCalled, sourceIP)
	}
	if _, err := registry.HTTPClient("missing"); !errors.Is(err, ErrUnknownEgressRoute) {
		t.Fatalf("HTTPClient(missing) error = %v", err)
	}
}
