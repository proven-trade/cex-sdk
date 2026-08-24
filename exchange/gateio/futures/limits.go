package futures

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/proven-trade/proven-trade-sdk/ratelimit"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type ratePool string

const (
	ratePoolPublic  ratePool = "public"
	ratePoolPrivate ratePool = "private"
	ratePoolOrder   ratePool = "futures-order"
	ratePoolCancel  ratePool = "futures-cancel"
)

type endpointLimit struct {
	pool     ratePool
	endpoint string
}

type rateLimit struct {
	key   string
	limit int
}

func publicLimit(endpoint string) endpointLimit {
	return endpointLimit{pool: ratePoolPublic, endpoint: endpoint}
}

func privateLimit(endpoint string) endpointLimit {
	return endpointLimit{pool: ratePoolPrivate, endpoint: endpoint}
}

func orderLimit() endpointLimit {
	return endpointLimit{pool: ratePoolOrder}
}

func cancelLimit() endpointLimit {
	return endpointLimit{pool: ratePoolCancel}
}

func rateLimitCharges(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	accountID string,
	limit endpointLimit,
	publicQuota int,
	privateQuota int,
	orderQuota int,
	cancelQuota int,
) (rateLimit, []ratelimit.Charge, error) {
	var value rateLimit
	var window time.Duration
	switch limit.pool {
	case ratePoolPublic:
		if strings.TrimSpace(string(routeID)) == "" || strings.TrimSpace(limit.endpoint) == "" {
			return rateLimit{}, nil, fmt.Errorf("Gate.io Futures public rate limit requires route and endpoint")
		}
		value = rateLimit{
			key: fmt.Sprintf(
				"gateio:route:%s:futures-public:%s:10seconds", routeID, limit.endpoint,
			),
			limit: publicQuota,
		}
		window = 10 * time.Second
	case ratePoolPrivate:
		if strings.TrimSpace(accountID) == "" || strings.TrimSpace(limit.endpoint) == "" {
			return rateLimit{}, nil, fmt.Errorf("Gate.io Futures private rate limit requires account and endpoint")
		}
		value = rateLimit{
			key: fmt.Sprintf(
				"gateio:account:%s:futures-private:%s:10seconds", accountID, limit.endpoint,
			),
			limit: privateQuota,
		}
		window = 10 * time.Second
	case ratePoolOrder:
		if strings.TrimSpace(accountID) == "" {
			return rateLimit{}, nil, fmt.Errorf("Gate.io Futures order rate limit requires account")
		}
		value = rateLimit{
			key:   fmt.Sprintf("gateio:account:%s:futures-order:1second", accountID),
			limit: orderQuota,
		}
		window = time.Second
	case ratePoolCancel:
		if strings.TrimSpace(accountID) == "" {
			return rateLimit{}, nil, fmt.Errorf("Gate.io Futures cancel rate limit requires account")
		}
		value = rateLimit{
			key:   fmt.Sprintf("gateio:account:%s:futures-cancel:1second", accountID),
			limit: cancelQuota,
		}
		window = time.Second
	default:
		return rateLimit{}, nil, fmt.Errorf("unsupported Gate.io Futures rate limit pool %q", limit.pool)
	}
	if err := limiter.SetRule(ratelimit.Rule{Key: value.key, Limit: value.limit, Window: window}); err != nil {
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
	reportedLimit, limitErr := strconv.Atoi(strings.TrimSpace(
		header.Get("X-Gate-RateLimit-Limit"),
	))
	remaining, remainingErr := strconv.Atoi(strings.TrimSpace(
		header.Get("X-Gate-RateLimit-Requests-Remain"),
	))
	if limitErr != nil || remainingErr != nil || reportedLimit != value.limit ||
		remaining < 0 || remaining > value.limit {
		return
	}
	_ = limiter.ObserveUsed(value.key, value.limit-remaining)
	if remaining != 0 {
		return
	}
	resetSeconds, err := strconv.ParseFloat(strings.TrimSpace(
		header.Get("X-Gate-RateLimit-Reset-Timestamp"),
	), 64)
	if err != nil || math.IsNaN(resetSeconds) || math.IsInf(resetSeconds, 0) {
		return
	}
	resetAt := time.Unix(0, int64(resetSeconds*float64(time.Second)))
	if resetAt.After(now) {
		_ = limiter.BlockFor([]string{value.key}, resetAt.Sub(now))
	}
}
