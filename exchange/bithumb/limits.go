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

	// 공지사항 조회는 공식 안내에서 IP별 초당 1회로 따로 제한한다.
	rateLimitPublicNotice rateLimitGroup = "public-notice"

	// 다건 주문 생성·취소는 단건 주문과 분리된 초당 20회 한도를 공유한다.
	rateLimitOrderBatch rateLimitGroup = "order-batch"
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
