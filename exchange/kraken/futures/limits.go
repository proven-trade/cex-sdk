package futures

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
	key := fmt.Sprintf("kraken-futures:route:%s:public:1second", routeID)
	if err := limiter.SetRule(ratelimit.Rule{
		Key: key, Limit: requestsPerSecond, Window: time.Second,
	}); err != nil {
		return nil, err
	}
	return []ratelimit.Charge{{Key: key, Units: 1}}, nil
}

func privateRateLimitCharges(
	limiter *ratelimit.Limiter,
	accountID string,
	pointLimit int,
	window time.Duration,
	cost int,
) ([]ratelimit.Charge, error) {
	if accountID == "" {
		return nil, fmt.Errorf("Kraken Futures private rate limit requires account ID")
	}
	if cost < 1 || cost > pointLimit {
		return nil, fmt.Errorf("Kraken Futures endpoint cost must be between 1 and %d", pointLimit)
	}
	key := fmt.Sprintf("kraken-futures:account:%s:derivatives:%s", accountID, durationKey(window))
	if err := limiter.SetRule(ratelimit.Rule{Key: key, Limit: pointLimit, Window: window}); err != nil {
		return nil, err
	}
	return []ratelimit.Charge{{Key: key, Units: cost}}, nil
}

const (
	historyPointLimit = 100
	historyWindow     = 10 * time.Minute
)

// historyRateLimitCharges는 /api/history/v3 계정 이력 endpoint의 별도 토큰 풀(10분당 100)에서 비용을 차감한다.
func historyRateLimitCharges(
	limiter *ratelimit.Limiter,
	accountID string,
	cost int,
) ([]ratelimit.Charge, error) {
	if accountID == "" {
		return nil, fmt.Errorf("Kraken Futures history rate limit requires account ID")
	}
	if cost < 1 || cost > historyPointLimit {
		return nil, fmt.Errorf("Kraken Futures history endpoint cost must be between 1 and %d", historyPointLimit)
	}
	key := fmt.Sprintf("kraken-futures:account:%s:history:10minutes", accountID)
	if err := limiter.SetRule(ratelimit.Rule{Key: key, Limit: historyPointLimit, Window: historyWindow}); err != nil {
		return nil, err
	}
	return []ratelimit.Charge{{Key: key, Units: cost}}, nil
}

func durationKey(window time.Duration) string {
	if window%time.Second == 0 {
		return fmt.Sprintf("%dseconds", int(window/time.Second))
	}
	return window.String()
}
