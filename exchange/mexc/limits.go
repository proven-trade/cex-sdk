package mexc

import (
	"fmt"
	"strings"
	"time"

	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

type endpointLimit struct {
	name   string
	weight int
}

func publicLimit(name string, weight int) endpointLimit {
	return endpointLimit{name: name, weight: weight}
}

// privateLimit은 상충하는 공식 제한표 중 더 보수적인 weight를 선택한다.
func privateLimit(name string, endpointWeight int) endpointLimit {
	const currentPrivateWeight = 10
	if endpointWeight < currentPrivateWeight {
		endpointWeight = currentPrivateWeight
	}
	return endpointLimit{name: name, weight: endpointWeight}
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

func privateRateLimitCharges(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	accountID string,
	limit endpointLimit,
	endpointQuota int,
	frequencyName string,
	frequencyQuota int,
) ([]ratelimit.Charge, error) {
	if strings.TrimSpace(string(routeID)) == "" || strings.TrimSpace(accountID) == "" ||
		strings.TrimSpace(limit.name) == "" || limit.weight < 1 ||
		strings.TrimSpace(frequencyName) == "" || frequencyQuota < 1 {
		return nil, fmt.Errorf("invalid MEXC private rate limit")
	}
	routeKey := fmt.Sprintf("mexc:route:%s:private:%s:10seconds", routeID, limit.name)
	accountKey := fmt.Sprintf("mexc:account:%s:private:%s:10seconds", accountID, limit.name)
	frequencyKey := fmt.Sprintf("mexc:account:%s:%s:1second", accountID, frequencyName)
	rules := []ratelimit.Rule{
		{Key: routeKey, Limit: endpointQuota, Window: 10 * time.Second},
		{Key: accountKey, Limit: endpointQuota, Window: 10 * time.Second},
		{Key: frequencyKey, Limit: frequencyQuota, Window: time.Second},
	}
	for _, rule := range rules {
		if err := limiter.SetRule(rule); err != nil {
			return nil, err
		}
	}
	return []ratelimit.Charge{
		{Key: routeKey, Units: limit.weight},
		{Key: accountKey, Units: limit.weight},
		{Key: frequencyKey, Units: 1},
	}, nil
}
