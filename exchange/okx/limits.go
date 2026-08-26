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
	return []ratelimit.Charge{{Key: key, Units: 1}}, nil
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
