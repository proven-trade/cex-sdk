package upbit

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

type rateGroup struct {
	name      string
	perSecond int
	account   bool
}

var (
	marketRate    = rateGroup{name: "market", perSecond: 10}
	tickerRate    = rateGroup{name: "ticker", perSecond: 10}
	orderBookRate = rateGroup{name: "orderbook", perSecond: 10}
	tradeRate     = rateGroup{name: "trade", perSecond: 10}
	candleRate    = rateGroup{name: "candle", perSecond: 10}
	defaultRate   = rateGroup{name: "default", perSecond: 30, account: true}
	orderRate     = rateGroup{name: "order", perSecond: 12, account: true}
)

func rateLimitCharges(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	accountID string,
	group rateGroup,
) ([]ratelimit.Charge, string, error) {
	scope, scopeID := "route", string(routeID)
	if group.account {
		scope, scopeID = "account", accountID
	}
	if strings.TrimSpace(scopeID) == "" {
		return nil, "", fmt.Errorf("Upbit rate group %q requires %s scope ID", group.name, scope)
	}
	key := rateLimitKey(scope, scopeID, group.name)
	if err := limiter.SetRule(ratelimit.Rule{
		Key: key, Limit: group.perSecond, Window: time.Second,
	}); err != nil {
		return nil, "", err
	}
	return []ratelimit.Charge{{Key: key, Units: 1}}, key, nil
}

func observeRemaining(limiter *ratelimit.Limiter, key string, group rateGroup, header http.Header) {
	name, remaining, ok := parseRemainingRequest(header.Get("Remaining-Req"))
	if !ok || name != group.name || remaining > group.perSecond {
		return
	}
	_ = limiter.ObserveUsed(key, group.perSecond-remaining)
}

func parseRemainingRequest(value string) (string, int, bool) {
	var group string
	remaining := -1
	for _, field := range strings.Split(value, ";") {
		key, item, found := strings.Cut(strings.TrimSpace(field), "=")
		if !found {
			continue
		}
		switch key {
		case "group":
			group = item
		case "sec":
			parsed, err := strconv.Atoi(item)
			if err == nil && parsed >= 0 {
				remaining = parsed
			}
		}
	}
	return group, remaining, group != "" && remaining >= 0
}

func rateLimitKey(scope, scopeID, group string) string {
	return fmt.Sprintf("upbit:%s:%s:%s:1second", scope, scopeID, group)
}
