package bithumb

import (
	"fmt"
	"time"

	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
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
	if err := limiter.SetRule(ratelimit.Rule{Key: key, Limit: requestsPerSecond, Window: time.Second}); err != nil {
		return nil, err
	}
	return []ratelimit.Charge{{Key: key, Units: 1}}, nil
}
