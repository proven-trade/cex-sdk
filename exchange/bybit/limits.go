package bybit

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

const globalRequestsPerFiveSeconds = 600

type endpointLimit struct {
	perSecond int
	account   bool
	// units는 요청 한 건이 엔드포인트 한도에서 소비하는 양이다. 0이면 1로 계산한다.
	units int
}

func publicLimit(perSecond int) endpointLimit {
	return endpointLimit{perSecond: perSecond}
}

func accountLimit(perSecond int) endpointLimit {
	return endpointLimit{perSecond: perSecond, account: true}
}

// batchAccountLimit은 일괄 주문 엔드포인트의 계정 한도다.
// Bybit는 일괄 요청의 한도를 요청 수가 아니라 요청에 담긴 주문 수만큼 소비한다.
func batchAccountLimit(perSecond, orders int) endpointLimit {
	return endpointLimit{perSecond: perSecond, account: true, units: orders}
}

func rateLimitCharges(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	accountID, path string,
	limit endpointLimit,
) ([]ratelimit.Charge, error) {
	globalKey := fmt.Sprintf("bybit:route:%s:global:5seconds", routeID)
	if err := limiter.SetRule(ratelimit.Rule{
		Key: globalKey, Limit: globalRequestsPerFiveSeconds, Window: 5 * time.Second,
	}); err != nil {
		return nil, err
	}

	scope, scopeID := "route", string(routeID)
	if limit.account {
		scope, scopeID = "account", accountID
	}
	if strings.TrimSpace(scopeID) == "" {
		return nil, fmt.Errorf("Bybit endpoint rate limit requires %s scope ID", scope)
	}
	endpointKey := rateLimitEndpointKey(scope, scopeID, path)
	if err := limiter.SetRule(ratelimit.Rule{
		Key: endpointKey, Limit: limit.perSecond, Window: time.Second,
	}); err != nil {
		return nil, err
	}
	endpointUnits := 1
	if limit.units > 1 {
		endpointUnits = limit.units
	}
	return []ratelimit.Charge{
		{Key: globalKey, Units: 1},
		{Key: endpointKey, Units: endpointUnits},
	}, nil
}

func observeRemaining(
	limiter *ratelimit.Limiter,
	scope, scopeID, path string,
	limit endpointLimit,
	header http.Header,
) {
	reportedLimit, limitErr := strconv.Atoi(strings.TrimSpace(header.Get("X-Bapi-Limit")))
	remaining, remainingErr := strconv.Atoi(strings.TrimSpace(header.Get("X-Bapi-Limit-Status")))
	if limitErr != nil || remainingErr != nil || reportedLimit != limit.perSecond ||
		remaining < 0 || remaining > reportedLimit {
		return
	}
	_ = limiter.ObserveUsed(
		rateLimitEndpointKey(scope, scopeID, path),
		reportedLimit-remaining,
	)
}

func rateLimitEndpointKey(scope, scopeID, path string) string {
	return fmt.Sprintf("bybit:%s:%s:endpoint:%s:1second", scope, scopeID, strings.TrimPrefix(path, "/"))
}
