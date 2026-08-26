package kucoin

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

type ratePool string

const (
	ratePoolPublic     ratePool = "public"
	ratePoolSpot       ratePool = "spot"
	ratePoolManagement ratePool = "management"
)

type endpointLimit struct {
	pool   ratePool
	weight int
}

type rateLimit struct {
	key    string
	limit  int
	window time.Duration
}

func publicLimit(weight int) endpointLimit {
	return endpointLimit{pool: ratePoolPublic, weight: weight}
}

func spotLimit(weight int) endpointLimit {
	return endpointLimit{pool: ratePoolSpot, weight: weight}
}

func managementLimit(weight int) endpointLimit {
	return endpointLimit{pool: ratePoolManagement, weight: weight}
}

func rateLimitCharges(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	accountID string,
	limit endpointLimit,
	publicQuota, spotQuota, managementQuota int,
) (rateLimit, []ratelimit.Charge, error) {
	scope, scopeID, quota := "route", string(routeID), publicQuota
	switch limit.pool {
	case ratePoolSpot:
		scope, scopeID, quota = "account", accountID, spotQuota
	case ratePoolManagement:
		scope, scopeID, quota = "account", accountID, managementQuota
	case ratePoolPublic:
	default:
		return rateLimit{}, nil, fmt.Errorf("unsupported KuCoin rate limit pool %q", limit.pool)
	}
	if strings.TrimSpace(scopeID) == "" {
		return rateLimit{}, nil, fmt.Errorf("KuCoin %s rate limit requires scope ID", limit.pool)
	}
	value := rateLimit{
		key:   fmt.Sprintf("kucoin:%s:%s:%s:30seconds", scope, scopeID, limit.pool),
		limit: quota, window: 30 * time.Second,
	}
	if err := limiter.SetRule(ratelimit.Rule{
		Key: value.key, Limit: value.limit, Window: value.window,
	}); err != nil {
		return rateLimit{}, nil, err
	}
	return value, []ratelimit.Charge{{Key: value.key, Units: limit.weight}}, nil
}

func observeRateLimit(limiter *ratelimit.Limiter, value rateLimit, header http.Header) {
	reportedLimit, limitErr := strconv.Atoi(strings.TrimSpace(header.Get("gw-ratelimit-limit")))
	remaining, remainingErr := strconv.Atoi(strings.TrimSpace(header.Get("gw-ratelimit-remaining")))
	if limitErr == nil && remainingErr == nil && reportedLimit == value.limit &&
		remaining >= 0 && remaining <= value.limit {
		_ = limiter.ObserveUsed(value.key, value.limit-remaining)
	}
	if remaining != 0 {
		return
	}
	resetMillis, err := strconv.ParseInt(strings.TrimSpace(header.Get("gw-ratelimit-reset")), 10, 64)
	if err == nil && resetMillis > 0 {
		_ = limiter.BlockFor([]string{value.key}, time.Duration(resetMillis)*time.Millisecond)
	}
}
