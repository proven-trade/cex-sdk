package coinone

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
	header string
}

func publicRateLimit(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	requestsPerMinute int,
) (rateLimit, []ratelimit.Charge, error) {
	value := rateLimit{
		key:   fmt.Sprintf("coinone:route:%s:public:1minute", routeID),
		limit: requestsPerMinute, window: time.Minute, header: "Public-Ratelimit-Remaining",
	}
	if err := limiter.SetRule(ratelimit.Rule{Key: value.key, Limit: value.limit, Window: value.window}); err != nil {
		return rateLimit{}, nil, err
	}
	return value, []ratelimit.Charge{{Key: value.key, Units: 1}}, nil
}

func privateRateLimit(
	limiter *ratelimit.Limiter,
	accountID string,
	order bool,
	privateRequestsPerSecond, orderRequestsPerSecond int,
) (rateLimit, []ratelimit.Charge, error) {
	if strings.TrimSpace(accountID) == "" {
		return rateLimit{}, nil, fmt.Errorf("Coinone private rate limit requires portfolio account ID")
	}
	group, limit, header := "private", privateRequestsPerSecond, "Private-Ratelimit-Remaining"
	if order {
		group, limit, header = "order", orderRequestsPerSecond, "Private-Order-Ratelimit-Remaining"
	}
	value := rateLimit{
		key:   fmt.Sprintf("coinone:account:%s:%s:1second", accountID, group),
		limit: limit, window: time.Second, header: header,
	}
	if err := limiter.SetRule(ratelimit.Rule{Key: value.key, Limit: value.limit, Window: value.window}); err != nil {
		return rateLimit{}, nil, err
	}
	return value, []ratelimit.Charge{{Key: value.key, Units: 1}}, nil
}

func observeRateLimit(limiter *ratelimit.Limiter, value rateLimit, header http.Header) {
	remaining, err := strconv.Atoi(strings.TrimSpace(header.Get(value.header)))
	if err != nil || remaining < 0 || remaining > value.limit {
		return
	}
	_ = limiter.ObserveUsed(value.key, value.limit-remaining)
}
