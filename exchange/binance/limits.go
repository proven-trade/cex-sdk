package binance

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	rateLimitRequestWeight = "REQUEST_WEIGHT"
	rateLimitRawRequests   = "RAW_REQUESTS"
	rateLimitOrders        = "ORDERS"
)

type limitSpec struct {
	typeName    string
	interval    string
	intervalNum int
	limit       int
	window      time.Duration
}

type limitCatalog struct {
	mu    sync.RWMutex
	specs map[string]limitSpec
}

func newLimitCatalog() *limitCatalog {
	catalog := &limitCatalog{specs: make(map[string]limitSpec)}
	for _, spec := range []limitSpec{
		{typeName: rateLimitRequestWeight, interval: "MINUTE", intervalNum: 1, limit: 6000, window: time.Minute},
		{typeName: rateLimitRawRequests, interval: "MINUTE", intervalNum: 5, limit: 300000, window: 5 * time.Minute},
		{typeName: rateLimitOrders, interval: "SECOND", intervalNum: 10, limit: 100, window: 10 * time.Second},
		{typeName: rateLimitOrders, interval: "DAY", intervalNum: 1, limit: 200000, window: 24 * time.Hour},
	} {
		catalog.specs[spec.identity()] = spec
	}
	return catalog
}

func (catalog *limitCatalog) update(limits []RateLimit) {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	for _, limit := range limits {
		window, ok := rateLimitWindow(limit.Interval, limit.IntervalNum)
		if !ok || limit.Limit <= 0 {
			continue
		}
		spec := limitSpec{
			typeName:    strings.ToUpper(limit.Type),
			interval:    strings.ToUpper(limit.Interval),
			intervalNum: limit.IntervalNum,
			limit:       limit.Limit,
			window:      window,
		}
		if spec.typeName != rateLimitRequestWeight &&
			spec.typeName != rateLimitRawRequests &&
			spec.typeName != rateLimitOrders {
			continue
		}
		catalog.specs[spec.identity()] = spec
	}
}

func (catalog *limitCatalog) charges(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	accountID string,
	requestWeight int,
	orderUnits int,
) ([]ratelimit.Charge, error) {
	catalog.mu.RLock()
	specs := make([]limitSpec, 0, len(catalog.specs))
	for _, spec := range catalog.specs {
		specs = append(specs, spec)
	}
	catalog.mu.RUnlock()

	charges := make([]ratelimit.Charge, 0, len(specs))
	for _, spec := range specs {
		units := 0
		scope := ""
		scopeID := ""
		switch spec.typeName {
		case rateLimitRequestWeight:
			units = requestWeight
			scope = "route"
			scopeID = string(routeID)
		case rateLimitRawRequests:
			units = 1
			scope = "route"
			scopeID = string(routeID)
		case rateLimitOrders:
			units = orderUnits
			scope = "account"
			scopeID = accountID
		}
		if units <= 0 {
			continue
		}
		if strings.TrimSpace(scopeID) == "" {
			return nil, fmt.Errorf("rate limit %s requires %s scope ID", spec.typeName, scope)
		}
		key := spec.key(scope, scopeID)
		if err := limiter.SetRule(ratelimit.Rule{Key: key, Limit: spec.limit, Window: spec.window}); err != nil {
			return nil, err
		}
		charges = append(charges, ratelimit.Charge{Key: key, Units: units})
	}
	return charges, nil
}

func (catalog *limitCatalog) observeHeaders(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	accountID string,
	headers map[string][]string,
) {
	catalog.mu.RLock()
	specs := make([]limitSpec, 0, len(catalog.specs))
	for _, spec := range catalog.specs {
		specs = append(specs, spec)
	}
	catalog.mu.RUnlock()

	for _, spec := range specs {
		var prefix, scope, scopeID string
		switch spec.typeName {
		case rateLimitRequestWeight:
			prefix = "X-MBX-USED-WEIGHT-"
			scope, scopeID = "route", string(routeID)
		case rateLimitOrders:
			prefix = "X-MBX-ORDER-COUNT-"
			scope, scopeID = "account", accountID
		default:
			continue
		}
		if scopeID == "" {
			continue
		}
		headerName := prefix + spec.headerSuffix()
		value := firstHeader(headers, headerName)
		used, ok := parsePositiveInt(value)
		if !ok {
			continue
		}
		_ = limiter.ObserveUsed(spec.key(scope, scopeID), used)
	}
}

func (spec limitSpec) identity() string {
	return fmt.Sprintf("%s:%d%s", spec.typeName, spec.intervalNum, spec.interval)
}

func (spec limitSpec) key(scope, scopeID string) string {
	return fmt.Sprintf(
		"binance:%s:%s:%s:%d%s",
		scope,
		scopeID,
		strings.ToLower(spec.typeName),
		spec.intervalNum,
		strings.ToLower(spec.interval),
	)
}

func (spec limitSpec) headerSuffix() string {
	letter := map[string]string{"SECOND": "S", "MINUTE": "M", "HOUR": "H", "DAY": "D"}[spec.interval]
	return fmt.Sprintf("%d%s", spec.intervalNum, letter)
}

func rateLimitWindow(interval string, count int) (time.Duration, bool) {
	if count <= 0 {
		return 0, false
	}
	var unit time.Duration
	switch strings.ToUpper(interval) {
	case "SECOND":
		unit = time.Second
	case "MINUTE":
		unit = time.Minute
	case "HOUR":
		unit = time.Hour
	case "DAY":
		unit = 24 * time.Hour
	default:
		return 0, false
	}
	return time.Duration(count) * unit, true
}
