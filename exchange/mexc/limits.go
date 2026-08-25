package mexc

import (
	"fmt"
	"strings"
	"time"

	"github.com/proven-trade/proven-trade-sdk/ratelimit"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type endpointLimit struct {
	name   string
	weight int
}

func publicLimit(name string, weight int) endpointLimit {
	return endpointLimit{name: name, weight: weight}
}

func publicRateLimitCharges(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	limit endpointLimit,
	quota int,
) ([]ratelimit.Charge, error) {
	if strings.TrimSpace(string(routeID)) == "" || strings.TrimSpace(limit.name) == "" || limit.weight < 1 {
		return nil, fmt.Errorf("invalid MEXC public rate limit")
	}
	key := fmt.Sprintf("mexc:route:%s:public:%s:10seconds", routeID, limit.name)
	if err := limiter.SetRule(ratelimit.Rule{Key: key, Limit: quota, Window: 10 * time.Second}); err != nil {
		return nil, err
	}
	return []ratelimit.Charge{{Key: key, Units: limit.weight}}, nil
}
