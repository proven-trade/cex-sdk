package coinbase

import (
	"context"
	"fmt"
	"time"

	"github.com/proven-trade/cex-sdk/v2/ratelimit"
	"github.com/proven-trade/cex-sdk/v2/transport"
)

func rateLimitCharges(
	ctx context.Context,
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	accountID string,
	private bool,
	publicRequestsPerSecond, privateRequestsPerSecond int,
) ([]ratelimit.Charge, error) {
	routeKey := fmt.Sprintf("coinbase:route:%s:all:1second", routeID)
	if err := limiter.SetRuleContext(ctx, ratelimit.Rule{
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
	if err := limiter.SetRuleContext(ctx, ratelimit.Rule{
		Key: accountKey, Limit: privateRequestsPerSecond, Window: time.Second,
	}); err != nil {
		return nil, err
	}
	return append(charges, ratelimit.Charge{Key: accountKey, Units: 1}), nil
}
