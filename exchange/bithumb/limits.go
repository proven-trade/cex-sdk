package bithumb

import (
	"fmt"
	"time"

	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

func publicRateLimitCharges(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	requestsPerSecond int,
) ([]ratelimit.Charge, error) {
	key := fmt.Sprintf("bithumb:route:%s:public:1second", routeID)
	if err := limiter.SetRule(ratelimit.Rule{Key: key, Limit: requestsPerSecond, Window: time.Second}); err != nil {
		return nil, err
	}
	return []ratelimit.Charge{{Key: key, Units: 1}}, nil
}

func privateRateLimitCharges(
	limiter *ratelimit.Limiter,
	accountID string,
	order bool,
	privateRequestsPerSecond, orderRequestsPerSecond int,
) ([]ratelimit.Charge, error) {
	if accountID == "" {
		return nil, fmt.Errorf("Bithumb private rate limit requires account ID")
	}
	privateKey := fmt.Sprintf("bithumb:account:%s:private:1second", accountID)
	if err := limiter.SetRule(ratelimit.Rule{
		Key: privateKey, Limit: privateRequestsPerSecond, Window: time.Second,
	}); err != nil {
		return nil, err
	}
	charges := []ratelimit.Charge{{Key: privateKey, Units: 1}}
	if !order {
		return charges, nil
	}
	orderKey := fmt.Sprintf("bithumb:account:%s:order:1second", accountID)
	if err := limiter.SetRule(ratelimit.Rule{
		Key: orderKey, Limit: orderRequestsPerSecond, Window: time.Second,
	}); err != nil {
		return nil, err
	}
	return append(charges, ratelimit.Charge{Key: orderKey, Units: 1}), nil
}
