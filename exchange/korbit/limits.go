package korbit

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/proven-trade/proven-trade-sdk/ratelimit"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type rateGroup string

const (
	rateGroupPublic      rateGroup = "public"
	rateGroupPrivate     rateGroup = "private"
	rateGroupOrderPlace  rateGroup = "order-place"
	rateGroupOrderCancel rateGroup = "order-cancel"
)

type rateLimit struct {
	key    string
	limit  int
	window time.Duration
}

func publicRateLimit(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	requestsPerSecond int,
) (rateLimit, []ratelimit.Charge, error) {
	value := rateLimit{
		key:   fmt.Sprintf("korbit:route:%s:public:1second", routeID),
		limit: requestsPerSecond, window: time.Second,
	}
	return registerRateLimit(limiter, value)
}

func privateRateLimit(
	limiter *ratelimit.Limiter,
	accountID string,
	group rateGroup,
	privateRequestsPerSecond, orderRequestsPerSecond int,
) (rateLimit, []ratelimit.Charge, error) {
	if strings.TrimSpace(accountID) == "" {
		return rateLimit{}, nil, fmt.Errorf("Korbit private rate limit requires account ID")
	}
	limit := privateRequestsPerSecond
	if group == rateGroupOrderPlace || group == rateGroupOrderCancel {
		limit = orderRequestsPerSecond
	}
	value := rateLimit{
		key:   fmt.Sprintf("korbit:account:%s:%s:1second", accountID, group),
		limit: limit, window: time.Second,
	}
	return registerRateLimit(limiter, value)
}

func registerRateLimit(
	limiter *ratelimit.Limiter,
	value rateLimit,
) (rateLimit, []ratelimit.Charge, error) {
	if err := limiter.SetRule(ratelimit.Rule{Key: value.key, Limit: value.limit, Window: value.window}); err != nil {
		return rateLimit{}, nil, err
	}
	return value, []ratelimit.Charge{{Key: value.key, Units: 1}}, nil
}

func observeRateLimit(limiter *ratelimit.Limiter, value rateLimit, header http.Header) {
	remaining, ok := parseRateLimitRemaining(header.Get("Ratelimit"))
	if !ok || remaining < 0 || remaining > value.limit {
		return
	}
	_ = limiter.ObserveUsed(value.key, value.limit-remaining)
}

func parseRateLimitRemaining(header string) (int, bool) {
	for _, part := range strings.Split(header, ",") {
		key, rawValue, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found || !strings.EqualFold(strings.TrimSpace(key), "remaining") {
			continue
		}
		value, err := strconv.Atoi(strings.TrimSpace(rawValue))
		return value, err == nil
	}
	return 0, false
}
