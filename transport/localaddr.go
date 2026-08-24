package transport

import (
	"fmt"
	"net"
)

// LocalAddressVerifier는 IP를 소켓의 로컬 주소로 사용할 준비가 되었는지 확인한다.
// 운영 환경에서는 일반적으로 VerifySystemLocalAddress를 사용한다.
type LocalAddressVerifier func(net.IP) error

// VerifySystemLocalAddress는 현재 호스트에 할당된 주소인지 확인한다.
func VerifySystemLocalAddress(want net.IP) error {
	want = want.To4()
	if want == nil {
		return fmt.Errorf("%w: address is not IPv4", ErrLocalAddressUnavailable)
	}

	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return fmt.Errorf("%w: list network addresses: %v", ErrLocalAddressUnavailable, err)
	}

	for _, address := range addresses {
		var candidate net.IP
		switch value := address.(type) {
		case *net.IPNet:
			candidate = value.IP
		case *net.IPAddr:
			candidate = value.IP
		default:
			continue
		}

		if candidate.Equal(want) {
			return nil
		}
	}

	return fmt.Errorf("%w: %s is not assigned to this host", ErrLocalAddressUnavailable, want)
}
