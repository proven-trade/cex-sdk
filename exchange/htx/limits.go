package htx

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/proven-trade/proven-trade-sdk/ratelimit"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type rateLimit struct {
	key    string
	limit  int
	window time.Duration
}

type rateGroup string

const (
	rateGroupAccount      rateGroup = "account"
	rateGroupOrder        rateGroup = "order"
	rateGroupOrderRead    rateGroup = "order-read"
	rateGroupTradeHistory rateGroup = "trade-history"
)

func publicRateLimit(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	endpoint string,
	requestsPerSecond int,
) (rateLimit, []ratelimit.Charge, error) {
	if strings.TrimSpace(string(routeID)) == "" || strings.TrimSpace(endpoint) == "" ||
		requestsPerSecond < 1 {
		return rateLimit{}, nil, fmt.Errorf("invalid HTX public rate limit")
	}
	value := rateLimit{
		key:   fmt.Sprintf("htx:route:%s:public:%s:1second", routeID, endpoint),
		limit: requestsPerSecond, window: time.Second,
	}
	if err := limiter.SetRule(ratelimit.Rule{
		Key: value.key, Limit: value.limit, Window: value.window,
	}); err != nil {
		return rateLimit{}, nil, err
	}
	return value, []ratelimit.Charge{{Key: value.key, Units: 1}}, nil
}

func privateRateLimit(
	limiter *ratelimit.Limiter,
	accountID string,
	group rateGroup,
	quota int,
) (rateLimit, []ratelimit.Charge, error) {
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(string(group)) == "" || quota < 1 {
		return rateLimit{}, nil, fmt.Errorf("invalid HTX private rate limit")
	}
	value := rateLimit{
		key:   fmt.Sprintf("htx:account:%s:%s:2seconds", accountID, group),
		limit: quota, window: 2 * time.Second,
	}
	if err := limiter.SetRule(ratelimit.Rule{
		Key: value.key, Limit: value.limit, Window: value.window,
	}); err != nil {
		return rateLimit{}, nil, err
	}
	return value, []ratelimit.Charge{{Key: value.key, Units: 1}}, nil
}

func observeRateLimit(
	limiter *ratelimit.Limiter,
	value rateLimit,
	header http.Header,
	now time.Time,
) {
	remaining, err := strconv.Atoi(strings.TrimSpace(
		header.Get("X-HB-RateLimit-Requests-Remain"),
	))
	if err != nil || remaining < 0 || remaining > value.limit {
		return
	}
	_ = limiter.ObserveUsed(value.key, value.limit-remaining)
	if remaining != 0 {
		return
	}
	duration, ok := rateLimitExpireDuration(
		header.Get("X-HB-RateLimit-Requests-Expire"), now,
	)
	if ok {
		_ = limiter.BlockFor([]string{value.key}, duration)
	}
}

func rateLimitExpireDuration(value string, now time.Time) (time.Duration, bool) {
	raw, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || raw <= 0 {
		return 0, false
	}
	if raw >= 1_000_000_000_000 {
		duration := time.UnixMilli(raw).Sub(now)
		if duration <= 0 {
			return 0, false
		}
		return duration, true
	}
	if raw >= 1_000_000_000 {
		duration := time.Unix(raw, 0).Sub(now)
		if duration <= 0 {
			return 0, false
		}
		return duration, true
	}
	return time.Duration(raw) * time.Millisecond, true
}
