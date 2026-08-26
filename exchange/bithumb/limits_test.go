package bithumb

import (
	"testing"

	"github.com/proven-trade/cex-sdk/ratelimit"
)

func TestPrivateOrderRateLimitUsesAccountScopes(t *testing.T) {
	t.Parallel()

	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	charges, err := privateRateLimitCharges(limiter, "main", true, 140, 10)
	if err != nil {
		t.Fatalf("privateRateLimitCharges() error = %v", err)
	}
	if len(charges) != 2 || charges[0].Key != "bithumb:account:main:private:1second" ||
		charges[1].Key != "bithumb:account:main:order:1second" {
		t.Fatalf("charges = %+v", charges)
	}
	privateSnapshot, err := limiter.Snapshot(charges[0].Key)
	if err != nil || privateSnapshot.Rule.Limit != 140 {
		t.Fatalf("private snapshot = %+v, error = %v", privateSnapshot, err)
	}
	orderSnapshot, err := limiter.Snapshot(charges[1].Key)
	if err != nil || orderSnapshot.Rule.Limit != 10 {
		t.Fatalf("order snapshot = %+v, error = %v", orderSnapshot, err)
	}
}

func TestPublicRateLimitUsesRouteScope(t *testing.T) {
	t.Parallel()

	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	charges, err := publicRateLimitCharges(limiter, "seoul-b", 150)
	if err != nil {
		t.Fatalf("publicRateLimitCharges() error = %v", err)
	}
	if len(charges) != 1 || charges[0].Key != "bithumb:route:seoul-b:public:1second" {
		t.Fatalf("charges = %+v", charges)
	}
	snapshot, err := limiter.Snapshot(charges[0].Key)
	if err != nil || snapshot.Rule.Limit != 150 {
		t.Fatalf("snapshot = %+v, error = %v", snapshot, err)
	}
}
