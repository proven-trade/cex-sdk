package cryptocom

import (
	"fmt"
	"strings"
	"time"

	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

func publicRateLimit(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	method string,
	requestsPerSecond int,
) ([]ratelimit.Charge, error) {
	if strings.TrimSpace(string(routeID)) == "" || strings.TrimSpace(method) == "" ||
		requestsPerSecond < 1 || requestsPerSecond > DefaultPublicRequestsPerSecond {
		return nil, fmt.Errorf("invalid Crypto.com public rate limit")
	}
	key := fmt.Sprintf("cryptocom:route:%s:public:%s:1second", routeID, method)
	if err := limiter.SetRule(ratelimit.Rule{
		Key: key, Limit: requestsPerSecond, Window: time.Second,
	}); err != nil {
		return nil, err
	}
	return []ratelimit.Charge{{Key: key, Units: 1}}, nil
}

func privateRateLimit(
	limiter *ratelimit.Limiter,
	accountID string,
	method string,
	orderQuota int,
	detailQuota int,
	historyQuota int,
	otherQuota int,
) ([]ratelimit.Charge, error) {
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(method) == "" {
		return nil, fmt.Errorf("invalid Crypto.com private rate limit")
	}
	limit := otherQuota
	window := 100 * time.Millisecond
	switch method {
	case "create-order", "cancel-order", "cancel-all-orders":
		limit = orderQuota
	case "get-order-detail":
		limit = detailQuota
	case "get-trades", "get-order-history":
		limit = historyQuota
		window = time.Second
	}
	if limit < 1 {
		return nil, fmt.Errorf("invalid Crypto.com private rate limit quota")
	}
	windowName := "100milliseconds"
	if window == time.Second {
		windowName = "1second"
	}
	key := fmt.Sprintf("cryptocom:account:%s:private:%s:%s", accountID, method, windowName)
	if err := limiter.SetRule(ratelimit.Rule{Key: key, Limit: limit, Window: window}); err != nil {
		return nil, err
	}
	return []ratelimit.Charge{{Key: key, Units: 1}}, nil
}
