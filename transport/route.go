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
	// ErrLocalAddressUnavailable은 설정한 private IP가 로컬 네트워크 인터페이스에
	// 할당되지 않았음을 나타낸다.
	ErrLocalAddressUnavailable = errors.New("local address unavailable")
	// ErrRegistryClosed는 종료된 경로 레지스트리를 사용했음을 나타낸다.
	ErrRegistryClosed = errors.New("egress registry closed")
	// ErrPublicIPMismatch는 실제 송신 IP가 경로에 설정한 EIP와 다름을 나타낸다.
	ErrPublicIPMismatch = errors.New("observed public IP does not match expected EIP")
)

// EgressRouteID는 선택 가능한 송신 경로의 논리 식별자다.
type EgressRouteID string

// EgressRoute는 애플리케이션 경로와 EIP에 연결된 private IPv4 주소를 대응시킨다.
// 소켓은 EIP가 아니라 항상 LocalPrivateIP에 바인딩한다.
type EgressRoute struct {
	ID               EgressRouteID
	LocalPrivateIP   net.IP
	ExpectedPublicIP net.IP
}

func normalizeRoute(route EgressRoute) (EgressRoute, error) {
	route.ID = EgressRouteID(strings.TrimSpace(string(route.ID)))
	if route.ID == "" {
		return EgressRoute{}, fmt.Errorf("%w: route ID is required", ErrInvalidEgressRoute)
	}

	localIP := route.LocalPrivateIP.To4()
	if localIP == nil || localIP.IsUnspecified() || localIP.IsMulticast() {
		return EgressRoute{}, fmt.Errorf(
			"%w: route %q local private IP must be a bindable IPv4 address",
			ErrInvalidEgressRoute,
			route.ID,
		)
	}

	route.LocalPrivateIP = cloneIP(localIP)
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

func cloneRoute(route EgressRoute) EgressRoute {
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
