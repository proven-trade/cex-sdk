package okx

import (
	"fmt"
	"strings"
	"time"

	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

type endpointLimit struct {
	requests      int
	window        time.Duration
	account       bool
	discriminator string
	// units는 요청 한 건이 소모하는 제한 단위 수다. 0이면 1로 계산한다.
	// 일괄 주문처럼 주문 수 기준으로 제한되는 endpoint에서 사용한다.
	units int
}

// withUnits는 요청 한 건이 소모하는 제한 단위 수를 지정한다.
func (limit endpointLimit) withUnits(units int) endpointLimit {
	limit.units = units
	return limit
}

func publicLimit(requests int, window time.Duration, discriminator string) endpointLimit {
	return endpointLimit{requests: requests, window: window, discriminator: discriminator}
}

func accountLimit(requests int, window time.Duration, discriminator string) endpointLimit {
	return endpointLimit{
		requests: requests, window: window, account: true, discriminator: discriminator,
	}
}

func rateLimitCharges(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	accountID, method, path string,
	limit endpointLimit,
) ([]ratelimit.Charge, error) {
	scope, scopeID := "route", string(routeID)
	if limit.account {
		scope, scopeID = "account", accountID
	}
	if strings.TrimSpace(scopeID) == "" {
		return nil, fmt.Errorf("OKX endpoint rate limit requires %s scope ID", scope)
	}
	key := rateLimitEndpointKey(scope, scopeID, method, path, limit.discriminator, limit.window)
	if err := limiter.SetRule(ratelimit.Rule{Key: key, Limit: limit.requests, Window: limit.window}); err != nil {
		return nil, err
	}
	units := limit.units
	if units < 1 {
		units = 1
	}
	return []ratelimit.Charge{{Key: key, Units: units}}, nil
}

func rateLimitEndpointKey(scope, scopeID, method, path, discriminator string, window time.Duration) string {
	key := fmt.Sprintf(
		"okx:%s:%s:endpoint:%s:%s",
		scope,
		scopeID,
		strings.ToUpper(method),
		strings.TrimPrefix(path, "/"),
	)
	if discriminator != "" {
		key += ":" + discriminator
	}
	return fmt.Sprintf("%s:%s", key, durationKey(window))
}

func durationKey(window time.Duration) string {
	if window%time.Second == 0 {
		return fmt.Sprintf("%dseconds", int(window/time.Second))
	}
	return window.String()
}
