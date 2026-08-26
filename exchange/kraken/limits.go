package kraken

import (
	"fmt"
	"time"

	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

type limitKind uint8

const (
	limitPublic limitKind = iota
	limitPrivate
	limitPrivateHistory
	limitTrading
)

func rateLimitCharges(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	accountID, pair string,
	kind limitKind,
	publicRequestsPerSecond, privateCounterLimit int,
	privateCounterWindow time.Duration,
	tradingRequestsPerSecond int,
) ([]ratelimit.Charge, error) {
	if kind == limitPublic {
		key := fmt.Sprintf("kraken:route:%s:public:1second", routeID)
		if err := limiter.SetRule(ratelimit.Rule{
			Key: key, Limit: publicRequestsPerSecond, Window: time.Second,
		}); err != nil {
			return nil, err
		}
		return []ratelimit.Charge{{Key: key, Units: 1}}, nil
	}
	if accountID == "" {
		return nil, fmt.Errorf("Kraken private rate limit requires account ID")
	}
	if kind == limitTrading {
		if pair == "" {
			pair = "unknown"
		}
		key := fmt.Sprintf("kraken:account:%s:trading:%s:1second", accountID, pair)
		if err := limiter.SetRule(ratelimit.Rule{
			Key: key, Limit: tradingRequestsPerSecond, Window: time.Second,
		}); err != nil {
			return nil, err
		}
		return []ratelimit.Charge{{Key: key, Units: 1}}, nil
	}
	key := fmt.Sprintf("kraken:account:%s:private-counter", accountID)
	if err := limiter.SetRule(ratelimit.Rule{
		Key: key, Limit: privateCounterLimit, Window: privateCounterWindow,
	}); err != nil {
		return nil, err
	}
	units := 1
	if kind == limitPrivateHistory {
		units = 4
	}
	return []ratelimit.Charge{{Key: key, Units: units}}, nil
}
