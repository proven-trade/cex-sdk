package bithumb

import (
	"testing"

	"github.com/proven-trade/cex-sdk/ratelimit"
)

func TestRateLimitsUseRouteAndAPICategoryScopes(t *testing.T) {
	t.Parallel()

	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	charges, err := rateLimitCharges(limiter, "seoul-b", rateLimitOrderCreate, 140)
	if err != nil {
		t.Fatalf("rateLimitCharges() error = %v", err)
	}
	if len(charges) != 1 || charges[0].Key != "bithumb:route:seoul-b:order-create:1second" {
		t.Fatalf("charges = %+v", charges)
	}
	snapshot, err := limiter.Snapshot(charges[0].Key)
	if err != nil || snapshot.Rule.Limit != 140 {
		t.Fatalf("snapshot = %+v, error = %v", snapshot, err)
	}

	public, err := rateLimitCharges(limiter, "seoul-b", rateLimitPublicTicker, 150)
	if err != nil || public[0].Key != "bithumb:route:seoul-b:public-ticker:1second" {
		t.Fatalf("public charges = %+v, error = %v", public, err)
	}
}
