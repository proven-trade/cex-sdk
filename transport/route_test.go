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
		LocalSourceIP:    localIP,
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
	if got := route.LocalSourceIP.String(); got != "10.0.10.21" {
		t.Fatalf("LocalSourceIP = %s, want 10.0.10.21", got)
	}
	if route.LocalPrivateIP != nil {
		t.Fatalf("표준 필드 입력에 하위 호환 IP가 설정됨 = %s", route.LocalPrivateIP)
	}
	if got := route.ExpectedPublicIP.String(); got != "203.0.113.10" {
		t.Fatalf("ExpectedPublicIP = %s, want 203.0.113.10", got)
	}
}

func TestNormalizeRouteRejectsIPv6LocalAddress(t *testing.T) {
	t.Parallel()

	_, err := normalizeRoute(EgressRoute{
		ID:            "route-a",
		LocalSourceIP: net.ParseIP("2001:db8::1"),
	})
	if !errors.Is(err, ErrInvalidEgressRoute) {
		t.Fatalf("error = %v, want ErrInvalidEgressRoute", err)
	}
}

func TestNormalizeRouteAcceptsDirectPublicAndLegacySourceIPs(t *testing.T) {
	t.Parallel()

	publicRoute, err := normalizeRoute(EgressRoute{
		ID:               "vultr-a",
		LocalSourceIP:    net.ParseIP("203.0.113.20"),
		ExpectedPublicIP: net.ParseIP("203.0.113.20"),
	})
	if err != nil {
		t.Fatalf("normalizeRoute() 공인 원본 IP 오류 = %v", err)
	}
	if !publicRoute.LocalSourceIP.Equal(publicRoute.ExpectedPublicIP) {
		t.Fatalf("공인 원본 IP = %s, 기대 공인 IP = %s", publicRoute.LocalSourceIP, publicRoute.ExpectedPublicIP)
	}

	legacyRoute, err := normalizeRoute(EgressRoute{
		ID:             "aws-a",
		LocalPrivateIP: net.ParseIP("10.0.10.21"),
	})
	if err != nil {
		t.Fatalf("normalizeRoute() 이전 필드 오류 = %v", err)
	}
	if got := legacyRoute.LocalSourceIP.String(); got != "10.0.10.21" {
		t.Fatalf("LocalSourceIP = %s, want 10.0.10.21", got)
	}
	if !legacyRoute.LocalPrivateIP.Equal(legacyRoute.LocalSourceIP) {
		t.Fatalf("하위 호환 IP = %s, 표준 IP = %s", legacyRoute.LocalPrivateIP, legacyRoute.LocalSourceIP)
	}
}

func TestNormalizeRouteRejectsConflictingSourceAliases(t *testing.T) {
	t.Parallel()

	_, err := normalizeRoute(EgressRoute{
		ID:             "route-a",
		LocalSourceIP:  net.ParseIP("10.0.10.21"),
		LocalPrivateIP: net.ParseIP("10.0.10.22"),
	})
	if !errors.Is(err, ErrInvalidEgressRoute) {
		t.Fatalf("error = %v, want ErrInvalidEgressRoute", err)
	}
}
