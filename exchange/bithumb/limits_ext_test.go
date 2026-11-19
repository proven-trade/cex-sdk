package bithumb

import (
	"context"
	"testing"

	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/ratelimit"
)

func TestNoticeAndBatchRateLimitsUseOwnBuckets(t *testing.T) {
	t.Parallel()

	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	notice, err := rateLimitCharges(limiter, "seoul-b", rateLimitPublicNotice, DefaultNoticeRequestsPerSecond)
	if err != nil || len(notice) != 1 || notice[0].Key != "bithumb:route:seoul-b:public-notice:1second" {
		t.Fatalf("notice charges = %+v, error = %v", notice, err)
	}
	batch, err := rateLimitCharges(limiter, "seoul-b", rateLimitOrderBatch, DefaultBatchOrderRequestsPerSecond)
	if err != nil || len(batch) != 1 || batch[0].Key != "bithumb:route:seoul-b:order-batch:1second" {
		t.Fatalf("batch charges = %+v, error = %v", batch, err)
	}
	assertExtLimiter(t, limiter, notice[0].Key, 1, 0)
	assertExtLimiter(t, limiter, batch[0].Key, 20, 0)
}

func TestNewValidatesNoticeAndBatchOrderLimits(t *testing.T) {
	t.Parallel()

	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: &directSender{}, Limiter: limiter})
	if err != nil {
		t.Fatalf("exchange.NewExecutor() error = %v", err)
	}
	base := Config{Executor: executor, DefaultEgressRouteID: "route-a"}

	defaults, err := New(base)
	if err != nil {
		t.Fatalf("New() with default limits error = %v", err)
	}
	if defaults.noticeRequestsPerSecond != DefaultNoticeRequestsPerSecond ||
		defaults.batchOrderRequestsPerSecond != DefaultBatchOrderRequestsPerSecond {
		t.Fatalf(
			"default limits = notice %d, batch %d",
			defaults.noticeRequestsPerSecond, defaults.batchOrderRequestsPerSecond,
		)
	}

	negativeNotice := base
	negativeNotice.NoticeRequestsPerSecond = -1
	if _, err := New(negativeNotice); err == nil {
		t.Error("New() accepted a negative notice request limit")
	}
	negativeBatch := base
	negativeBatch.BatchOrderRequestsPerSecond = -1
	if _, err := New(negativeBatch); err == nil {
		t.Error("New() accepted a negative batch order request limit")
	}
}

func TestCustomNoticeAndBatchOrderLimitsApplyToTheirBuckets(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{
		extOK("GET /v1/notices", `[]`),
		extOK("POST /v2/orders/batch", `{"batch_orders_response":[]}`),
		extOK("POST /v2/orders/cancel", `{"success":[],"fail":[]}`),
	}, extTrader, func(config *Config) {
		config.NoticeRequestsPerSecond = 3
		config.BatchOrderRequestsPerSecond = 7
	})
	ctx := context.Background()
	if _, err := fixture.client.Notices(ctx, NoticesRequest{}); err != nil {
		t.Fatalf("Notices() error = %v", err)
	}
	if _, err := fixture.client.PlaceBatchOrders(ctx, extBatchRequest()); err != nil {
		t.Fatalf("PlaceBatchOrders() error = %v", err)
	}
	if _, err := fixture.client.CancelBatchOrders(ctx, CancelBatchOrdersRequest{OrderIDs: []string{"o-1"}}); err != nil {
		t.Fatalf("CancelBatchOrders() error = %v", err)
	}
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:public-notice:1second", 3, 1)
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:order-batch:1second", 7, 2)
}
