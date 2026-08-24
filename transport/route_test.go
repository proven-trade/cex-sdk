package transport

import (
	"errors"
	"net"
	"testing"
)

func TestNormalizeRouteDefensivelyCopiesIPs(t *testing.T) {
	t.Parallel()

	localIP := net.ParseIP("10.0.10.21")
	expectedIP := net.ParseIP("203.0.113.10")
	route, err := normalizeRoute(EgressRoute{
		ID:               " route-a ",
		LocalPrivateIP:   localIP,
		ExpectedPublicIP: expectedIP,
	})
	if err != nil {
		t.Fatalf("normalizeRoute() error = %v", err)
	}

	for index := range localIP {
		localIP[index] = 0
	}
	for index := range expectedIP {
		expectedIP[index] = 0
	}

	if route.ID != "route-a" {
		t.Fatalf("ID = %q, want route-a", route.ID)
	}
	if got := route.LocalPrivateIP.String(); got != "10.0.10.21" {
		t.Fatalf("LocalPrivateIP = %s, want 10.0.10.21", got)
	}
	if got := route.ExpectedPublicIP.String(); got != "203.0.113.10" {
		t.Fatalf("ExpectedPublicIP = %s, want 203.0.113.10", got)
	}
}

func TestNormalizeRouteRejectsIPv6LocalAddress(t *testing.T) {
	t.Parallel()

	_, err := normalizeRoute(EgressRoute{
		ID:             "route-a",
		LocalPrivateIP: net.ParseIP("2001:db8::1"),
	})
	if !errors.Is(err, ErrInvalidEgressRoute) {
		t.Fatalf("error = %v, want ErrInvalidEgressRoute", err)
	}
}
