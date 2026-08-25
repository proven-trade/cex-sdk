package cryptocom

import (
	"fmt"
	"strings"
	"time"

	"github.com/proven-trade/proven-trade-sdk/ratelimit"
	"github.com/proven-trade/proven-trade-sdk/transport"
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
