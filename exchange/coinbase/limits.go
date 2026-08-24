package coinbase

import (
	"fmt"
	"time"

	"github.com/proven-trade/proven-trade-sdk/ratelimit"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

func rateLimitCharges(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	accountID string,
	private bool,
	publicRequestsPerSecond, privateRequestsPerSecond int,
) ([]ratelimit.Charge, error) {
	routeKey := fmt.Sprintf("coinbase:route:%s:all:1second", routeID)
	if err := limiter.SetRule(ratelimit.Rule{
		Key: routeKey, Limit: publicRequestsPerSecond, Window: time.Second,
	}); err != nil {
		return nil, err
	}
	charges := []ratelimit.Charge{{Key: routeKey, Units: 1}}
	if !private {
		return charges, nil
	}
	if accountID == "" {
		return nil, fmt.Errorf("Coinbase private rate limit requires account ID")
	}
	accountKey := fmt.Sprintf("coinbase:account:%s:private:1second", accountID)
	if err := limiter.SetRule(ratelimit.Rule{
		Key: accountKey, Limit: privateRequestsPerSecond, Window: time.Second,
	}); err != nil {
		return nil, err
	}
	return append(charges, ratelimit.Charge{Key: accountKey, Units: 1}), nil
}
