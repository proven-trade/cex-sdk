package bithumb

import (
	"context"
	"fmt"
	"time"

	"github.com/proven-trade/cex-sdk/v2/ratelimit"
	"github.com/proven-trade/cex-sdk/v2/transport"
)

type rateLimitGroup string

const (
	rateLimitPublicOther     rateLimitGroup = "public-other"
	rateLimitPublicTicker    rateLimitGroup = "public-ticker"
	rateLimitPublicOrderBook rateLimitGroup = "public-orderbook"
	rateLimitPublicTrade     rateLimitGroup = "public-trade"
	rateLimitPublicCandle    rateLimitGroup = "public-candle"
	rateLimitPrivateOther    rateLimitGroup = "private-other"
	rateLimitOrderCreate     rateLimitGroup = "order-create"
	rateLimitOrderCancel     rateLimitGroup = "order-cancel"
)

func rateLimitCharges(
	ctx context.Context,
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	group rateLimitGroup,
	requestsPerSecond int,
) ([]ratelimit.Charge, error) {
	if routeID == "" {
		return nil, fmt.Errorf("Bithumb rate limit requires egress route ID")
	}
	if group == "" {
		return nil, fmt.Errorf("Bithumb rate limit group is required")
	}
	key := fmt.Sprintf("bithumb:route:%s:%s:1second", routeID, group)
	if err := limiter.SetRuleContext(ctx, ratelimit.Rule{Key: key, Limit: requestsPerSecond, Window: time.Second}); err != nil {
		return nil, err
	}
	return []ratelimit.Charge{{Key: key, Units: 1}}, nil
}
