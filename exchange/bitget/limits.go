package bitget

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/proven-trade/proven-trade-sdk/ratelimit"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const globalRequestsPerMinute = 6000

type endpointLimit struct {
	perSecond int
	account   bool
}

func publicLimit(perSecond int) endpointLimit {
	return endpointLimit{perSecond: perSecond}
}

func accountLimit(perSecond int) endpointLimit {
	return endpointLimit{perSecond: perSecond, account: true}
}

func rateLimitCharges(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	accountID, path string,
	limit endpointLimit,
) ([]ratelimit.Charge, error) {
	globalKey := fmt.Sprintf("bitget:route:%s:global:1minute", routeID)
	if err := limiter.SetRule(ratelimit.Rule{
		Key: globalKey, Limit: globalRequestsPerMinute, Window: time.Minute,
	}); err != nil {
		return nil, err
	}

	scope, scopeID := "route", string(routeID)
	if limit.account {
		scope, scopeID = "account", accountID
	}
	if strings.TrimSpace(scopeID) == "" {
		return nil, fmt.Errorf("Bitget endpoint rate limit requires %s scope ID", scope)
	}
	endpointKey := rateLimitEndpointKey(scope, scopeID, path)
	if err := limiter.SetRule(ratelimit.Rule{
		Key: endpointKey, Limit: limit.perSecond, Window: time.Second,
	}); err != nil {
		return nil, err
	}
	return []ratelimit.Charge{
		{Key: globalKey, Units: 1},
		{Key: endpointKey, Units: 1},
	}, nil
}

func observeRemaining(
	limiter *ratelimit.Limiter,
	scope, scopeID, path string,
	limit endpointLimit,
	header http.Header,
) {
	remaining, err := strconv.Atoi(strings.TrimSpace(header.Get("x-mbx-used-remain-limit")))
	if err != nil || remaining < 0 || remaining > limit.perSecond {
		return
	}
	_ = limiter.ObserveUsed(
		rateLimitEndpointKey(scope, scopeID, path),
		limit.perSecond-remaining,
	)
}

func rateLimitEndpointKey(scope, scopeID, path string) string {
	return fmt.Sprintf("bitget:%s:%s:endpoint:%s:1second", scope, scopeID, strings.TrimPrefix(path, "/"))
}
