package htx

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

type rateLimit struct {
	key    string
	limit  int
	window time.Duration
}

type rateGroup string

const (
	rateGroupAccount      rateGroup = "account"
	rateGroupOrder        rateGroup = "order"
	rateGroupOrderRead    rateGroup = "order-read"
	rateGroupTradeHistory rateGroup = "trade-history"
)

// 아래 그룹은 공식 문서가 endpoint별로 더 낮거나 다른 시간 창의 한도를 명시한 요청에 쓴다.
// 한도는 설정한 계정 quota와 공식 값 중 작은 쪽을 적용한다.
const (
	rateGroupBatchOrder      rateGroup = "order-batch"
	rateGroupAlgoOrder       rateGroup = "algo-order"
	rateGroupAccountHistory  rateGroup = "account-history"
	rateGroupValuation       rateGroup = "valuation"
	rateGroupPoint           rateGroup = "point"
	rateGroupFuturesTransfer rateGroup = "futures-transfer"
)

const (
	batchOrderRequestLimit      = 50
	algoOrderRequestLimit       = 20
	accountHistoryRequestLimit  = 5
	valuationRequestLimit       = 3
	pointRequestLimit           = 2
	futuresTransferRequestLimit = 10
)

func publicRateLimit(
	limiter *ratelimit.Limiter,
	routeID transport.EgressRouteID,
	endpoint string,
	requestsPerSecond int,
) (rateLimit, []ratelimit.Charge, error) {
	if strings.TrimSpace(string(routeID)) == "" || strings.TrimSpace(endpoint) == "" ||
		requestsPerSecond < 1 {
		return rateLimit{}, nil, fmt.Errorf("invalid HTX public rate limit")
	}
	value := rateLimit{
		key:   fmt.Sprintf("htx:route:%s:public:%s:1second", routeID, endpoint),
		limit: requestsPerSecond, window: time.Second,
	}
	if err := limiter.SetRule(ratelimit.Rule{
		Key: value.key, Limit: value.limit, Window: value.window,
	}); err != nil {
		return rateLimit{}, nil, err
	}
	return value, []ratelimit.Charge{{Key: value.key, Units: 1}}, nil
}

func privateRateLimit(
	limiter *ratelimit.Limiter,
	accountID string,
	group rateGroup,
	quota int,
) (rateLimit, []ratelimit.Charge, error) {
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(string(group)) == "" || quota < 1 {
		return rateLimit{}, nil, fmt.Errorf("invalid HTX private rate limit")
	}
	window, suffix := rateGroupWindow(group)
	value := rateLimit{
		key:   fmt.Sprintf("htx:account:%s:%s:%s", accountID, group, suffix),
		limit: quota, window: window,
	}
	if err := limiter.SetRule(ratelimit.Rule{
		Key: value.key, Limit: value.limit, Window: value.window,
	}); err != nil {
		return rateLimit{}, nil, err
	}
	return value, []ratelimit.Charge{{Key: value.key, Units: 1}}, nil
}

// rateGroupWindow는 그룹별 공식 한도의 시간 창과 limiter 키 접미사를 반환한다.
// 별도 창을 명시하지 않은 그룹은 계정 기준 2초 창을 쓴다.
func rateGroupWindow(group rateGroup) (time.Duration, string) {
	switch group {
	case rateGroupValuation, rateGroupPoint:
		return time.Second, "1second"
	case rateGroupFuturesTransfer:
		return time.Minute, "1minute"
	default:
		return 2 * time.Second, "2seconds"
	}
}

func observeRateLimit(
	limiter *ratelimit.Limiter,
	value rateLimit,
	header http.Header,
	now time.Time,
) {
	remaining, err := strconv.Atoi(strings.TrimSpace(
		header.Get("X-HB-RateLimit-Requests-Remain"),
	))
	if err != nil || remaining < 0 || remaining > value.limit {
		return
	}
	_ = limiter.ObserveUsed(value.key, value.limit-remaining)
	if remaining != 0 {
		return
	}
	duration, ok := rateLimitExpireDuration(
		header.Get("X-HB-RateLimit-Requests-Expire"), now,
	)
	if ok {
		_ = limiter.BlockFor([]string{value.key}, duration)
	}
}

func rateLimitExpireDuration(value string, now time.Time) (time.Duration, bool) {
	raw, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || raw <= 0 {
		return 0, false
	}
	if raw >= 1_000_000_000_000 {
		duration := time.UnixMilli(raw).Sub(now)
		if duration <= 0 {
			return 0, false
		}
		return duration, true
	}
	if raw >= 1_000_000_000 {
		duration := time.Unix(raw, 0).Sub(now)
		if duration <= 0 {
			return 0, false
		}
		return duration, true
	}
	return time.Duration(raw) * time.Millisecond, true
}
