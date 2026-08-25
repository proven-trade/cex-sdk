// Package transport는 송신 네트워크 경로와 HTTP 연결 풀을 관리한다.
package transport

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

var (
	// ErrInvalidEgressRoute는 송신 경로 설정이 누락되거나 잘못되었음을 나타낸다.
	ErrInvalidEgressRoute = errors.New("invalid egress route")
	// ErrUnknownEgressRoute는 송신 경로 ID가 등록되지 않았음을 나타낸다.
	ErrUnknownEgressRoute = errors.New("unknown egress route")
	// ErrLocalAddressUnavailable은 설정한 송신 원본 IP가 로컬 네트워크 인터페이스에
	// 할당되지 않았음을 나타낸다.
	ErrLocalAddressUnavailable = errors.New("local address unavailable")
	// ErrRegistryClosed는 종료된 경로 레지스트리를 사용했음을 나타낸다.
	ErrRegistryClosed = errors.New("egress registry closed")
	// ErrPublicIPMismatch는 실제 송신 IP가 경로에 설정한 기대 공인 IP와 다름을 나타낸다.
	ErrPublicIPMismatch = errors.New("observed public IP does not match expected public IP")
)

// EgressRouteID는 선택 가능한 송신 경로의 논리 식별자다.
type EgressRouteID string

// EgressRoute는 논리 송신 경로와 호스트에 할당된 원본 IPv4 주소를 대응시킨다.
// LocalSourceIP는 사설 주소를 공인 IP로 변환하는 환경과 공인 IP를 직접 할당하는 환경을 모두 지원한다.
type EgressRoute struct {
	ID            EgressRouteID
	LocalSourceIP net.IP
	// LocalPrivateIP은 이전 설정과 소스 코드의 호환성을 위한 별칭이다.
	// Deprecated: 새 코드는 공급자 중립적인 LocalSourceIP를 사용해야 한다.
	LocalPrivateIP   net.IP
	ExpectedPublicIP net.IP
}

func normalizeRoute(route EgressRoute) (EgressRoute, error) {
	route.ID = EgressRouteID(strings.TrimSpace(string(route.ID)))
	if route.ID == "" {
		return EgressRoute{}, fmt.Errorf("%w: route ID is required", ErrInvalidEgressRoute)
	}

	localIP, err := resolveLocalSourceIP(route)
	if err != nil {
		return EgressRoute{}, err
	}
	localIP = localIP.To4()
	if localIP == nil || localIP.IsUnspecified() || localIP.IsMulticast() {
		return EgressRoute{}, fmt.Errorf(
			"%w: route %q local source IP must be a bindable IPv4 address",
			ErrInvalidEgressRoute,
			route.ID,
		)
	}

	route.LocalSourceIP = cloneIP(localIP)
	if route.LocalPrivateIP != nil {
		route.LocalPrivateIP = cloneIP(localIP)
	}
	if route.ExpectedPublicIP != nil {
		expectedIP := route.ExpectedPublicIP.To4()
		if expectedIP == nil || expectedIP.IsUnspecified() || expectedIP.IsMulticast() {
			return EgressRoute{}, fmt.Errorf(
				"%w: route %q expected public IP must be an IPv4 address",
				ErrInvalidEgressRoute,
				route.ID,
			)
		}
		route.ExpectedPublicIP = cloneIP(expectedIP)
	}

	return route, nil
}

func resolveLocalSourceIP(route EgressRoute) (net.IP, error) {
	hasSource := route.LocalSourceIP != nil
	hasLegacy := route.LocalPrivateIP != nil
	if !hasSource && !hasLegacy {
		return nil, fmt.Errorf(
			"%w: route %q local source IP is required",
			ErrInvalidEgressRoute,
			route.ID,
		)
	}
	if hasSource && hasLegacy && !route.LocalSourceIP.Equal(route.LocalPrivateIP) {
		return nil, fmt.Errorf(
			"%w: route %q local source IP conflicts with legacy local private IP",
			ErrInvalidEgressRoute,
			route.ID,
		)
	}
	if hasSource {
		return route.LocalSourceIP, nil
	}
	return route.LocalPrivateIP, nil
}

func cloneRoute(route EgressRoute) EgressRoute {
	route.LocalSourceIP = cloneIP(route.LocalSourceIP)
	route.LocalPrivateIP = cloneIP(route.LocalPrivateIP)
	route.ExpectedPublicIP = cloneIP(route.ExpectedPublicIP)
	return route
}

func cloneIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	return append(net.IP(nil), ip...)
}
