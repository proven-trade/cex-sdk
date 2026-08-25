package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxPublicIPResponseBytes = 4096

// PublicIPCheck는 외부에서 관측한 송신 경로의 IP 확인 결과다.
type PublicIPCheck struct {
	RouteID       EgressRouteID `json:"routeId"`
	LocalSourceIP net.IP        `json:"localSourceIp"`
	// LocalPrivateIP은 이전 Go 호출 코드의 호환성을 위한 별칭이며 JSON에는 기록하지 않는다.
	// Deprecated: 새 코드는 공급자 중립적인 LocalSourceIP를 사용해야 한다.
	LocalPrivateIP   net.IP    `json:"-"`
	ExpectedPublicIP net.IP    `json:"expectedPublicIp,omitempty"`
	ObservedPublicIP net.IP    `json:"observedPublicIp"`
	MatchesExpected  bool      `json:"matchesExpected"`
	CheckedAt        time.Time `json:"checkedAt"`
}

// VerifyPublicIP는 routeID를 통해 IP 확인 endpoint를 호출한다.
// ExpectedPublicIP가 설정되어 있으면 관측 결과와 일치하는지 검사한다.
func (registry *Registry) VerifyPublicIP(
	ctx context.Context,
	routeID EgressRouteID,
	endpoint string,
) (PublicIPCheck, error) {
	route, err := registry.Route(routeID)
	if err != nil {
		return PublicIPCheck{}, err
	}

	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil || parsedEndpoint.Host == "" ||
		(parsedEndpoint.Scheme != "http" && parsedEndpoint.Scheme != "https") {
		return PublicIPCheck{}, fmt.Errorf("invalid public IP endpoint %q", endpoint)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedEndpoint.String(), nil)
	if err != nil {
		return PublicIPCheck{}, fmt.Errorf("create public IP request: %w", err)
	}
	request.Header.Set("Accept", "application/json, text/plain")
	request.Header.Set("User-Agent", "proven-trade-sdk-egressdiag/0")

	response, err := registry.Do(ctx, routeID, request)
	if err != nil {
		return PublicIPCheck{}, err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxPublicIPResponseBytes))
		return PublicIPCheck{}, fmt.Errorf("public IP endpoint returned HTTP %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxPublicIPResponseBytes+1))
	if err != nil {
		return PublicIPCheck{}, fmt.Errorf("read public IP response: %w", err)
	}
	if len(body) > maxPublicIPResponseBytes {
		return PublicIPCheck{}, fmt.Errorf("public IP response exceeds %d bytes", maxPublicIPResponseBytes)
	}

	observedIP, err := parsePublicIP(body)
	if err != nil {
		return PublicIPCheck{}, err
	}
	check := PublicIPCheck{
		RouteID:          route.ID,
		LocalSourceIP:    cloneIP(route.LocalSourceIP),
		LocalPrivateIP:   cloneIP(route.LocalPrivateIP),
		ExpectedPublicIP: cloneIP(route.ExpectedPublicIP),
		ObservedPublicIP: cloneIP(observedIP),
		MatchesExpected:  route.ExpectedPublicIP == nil || observedIP.Equal(route.ExpectedPublicIP),
		CheckedAt:        time.Now().UTC(),
	}
	if !check.MatchesExpected {
		return check, fmt.Errorf(
			"%w: route %q expected %s, observed %s",
			ErrPublicIPMismatch,
			route.ID,
			route.ExpectedPublicIP,
			observedIP,
		)
	}
	return check, nil
}

func parsePublicIP(body []byte) (net.IP, error) {
	text := strings.TrimSpace(string(body))
	if parsed := net.ParseIP(text); parsed != nil {
		return parsed, nil
	}

	var payload struct {
		IP     string `json:"ip"`
		Origin string `json:"origin"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		for _, candidate := range []string{payload.IP, payload.Origin} {
			// 일부 IP 확인 서비스는 origin에 쉼표로 구분한 proxy 체인을 반환한다.
			candidate = strings.TrimSpace(strings.Split(candidate, ",")[0])
			if parsed := net.ParseIP(candidate); parsed != nil {
				return parsed, nil
			}
		}
	}

	return nil, fmt.Errorf("public IP endpoint returned an invalid IP address")
}
