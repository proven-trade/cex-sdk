package usdm

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/proven-trade/proven-trade-sdk/ratelimit"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	requestWeightPerMinute = 2400
	ordersPerTenSeconds    = 300
	ordersPerMinute        = 1200
)

type limitCatalog struct {
	mu            sync.RWMutex
	requestWeight int
	ordersTenSecs int
	ordersOneMin  int
}

func newLimitCatalog() *limitCatalog {
	return &limitCatalog{
		requestWeight: requestWeightPerMinute,
		ordersTenSecs: ordersPerTenSeconds,
		ordersOneMin:  ordersPerMinute,
	}
}

func (catalog *limitCatalog) update(rules []RateLimit) {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	for _, rule := range rules {
		if rule.Limit <= 0 {
			continue
		}
		typeName, interval := strings.ToUpper(rule.Type), strings.ToUpper(rule.Interval)
		switch {
		case typeName == "REQUEST_WEIGHT" && interval == "MINUTE" && rule.IntervalNum == 1:
			catalog.requestWeight = rule.Limit
		case typeName == "ORDERS" && interval == "SECOND" && rule.IntervalNum == 10:
			catalog.ordersTenSecs = rule.Limit
		case typeName == "ORDERS" && interval == "MINUTE" && rule.IntervalNum == 1:
			catalog.ordersOneMin = rule.Limit
		}
	}
}

func (catalog *limitCatalog) charges(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	accountID string,
	requestWeight, orderUnits int,
) ([]ratelimit.Charge, error) {
	catalog.mu.RLock()
	requestLimit := catalog.requestWeight
	tenSecondLimit := catalog.ordersTenSecs
	minuteLimit := catalog.ordersOneMin
	catalog.mu.RUnlock()
	rules := []struct {
		key    string
		limit  int
		window time.Duration
		units  int
	}{
		{rateLimitKey("route", string(routeID), "request_weight", "1minute"), requestLimit, time.Minute, requestWeight},
		{rateLimitKey("account", accountID, "orders", "10seconds"), tenSecondLimit, 10 * time.Second, orderUnits},
		{rateLimitKey("account", accountID, "orders", "1minute"), minuteLimit, time.Minute, orderUnits},
	}
	charges := make([]ratelimit.Charge, 0, len(rules))
	for _, rule := range rules {
		if rule.units <= 0 {
			continue
		}
		if strings.Contains(rule.key, "::") {
			return nil, fmt.Errorf("Binance USD-M rate limit scope ID is required")
		}
		if err := limiter.SetRule(ratelimit.Rule{Key: rule.key, Limit: rule.limit, Window: rule.window}); err != nil {
			return nil, err
		}
		charges = append(charges, ratelimit.Charge{Key: rule.key, Units: rule.units})
	}
	return charges, nil
}

func observeHeaders(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	accountID string,
	header http.Header,
) {
	observations := []struct {
		key    string
		header string
	}{
		{rateLimitKey("route", string(routeID), "request_weight", "1minute"), "X-MBX-USED-WEIGHT-1M"},
		{rateLimitKey("account", accountID, "orders", "10seconds"), "X-MBX-ORDER-COUNT-10S"},
		{rateLimitKey("account", accountID, "orders", "1minute"), "X-MBX-ORDER-COUNT-1M"},
	}
	for _, observation := range observations {
		if strings.Contains(observation.key, "::") {
			continue
		}
		used, err := strconv.Atoi(strings.TrimSpace(header.Get(observation.header)))
		if err == nil && used >= 0 {
			_ = limiter.ObserveUsed(observation.key, used)
		}
	}
}

func rateLimitKey(scope, scopeID, kind, window string) string {
	return fmt.Sprintf("binance-usdm:%s:%s:%s:%s", scope, scopeID, kind, window)
}

func depthWeight(limit int) int {
	switch {
	case limit <= 50:
		return 2
	case limit <= 100:
		return 5
	case limit <= 500:
		return 10
	default:
		return 20
	}
}

func candleWeight(limit int) int {
	switch {
	case limit == 0 || limit < 100:
		return 1
	case limit < 500:
		return 2
	case limit <= 1000:
		return 5
	default:
		return 10
	}
}
