package futures

import (
	"context"
	"testing"
	"time"

	"github.com/proven-trade/proven-trade-sdk/ratelimit"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

func TestPrivateRateLimitChargesEndpointCost(t *testing.T) {
	t.Parallel()

	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	charges, err := privateRateLimitCharges(limiter, "account-a", 500, 10*time.Second, 25)
	if err != nil {
		t.Fatalf("privateRateLimitCharges() error = %v", err)
	}
	if err := limiter.Wait(context.Background(), charges...); err != nil {
		t.Fatalf("Limiter.Wait() error = %v", err)
	}
	snapshot, err := limiter.Snapshot("kraken-futures:account:account-a:derivatives:10seconds")
	if err != nil || snapshot.Used != 25 {
		t.Fatalf("limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestPublicRateLimitChargesAreSeparatedByRoute(t *testing.T) {
	t.Parallel()

	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	for _, routeID := range []transport.EgressRouteID{"route-a", "route-b"} {
		charges, chargeErr := publicRateLimitCharges(limiter, routeID, 20)
		if chargeErr != nil {
			t.Fatalf("publicRateLimitCharges(%q) error = %v", routeID, chargeErr)
		}
		if waitErr := limiter.Wait(context.Background(), charges...); waitErr != nil {
			t.Fatalf("Limiter.Wait(%q) error = %v", routeID, waitErr)
		}
		snapshot, snapshotErr := limiter.Snapshot(
			"kraken-futures:route:" + string(routeID) + ":public:1second",
		)
		if snapshotErr != nil || snapshot.Used != 1 {
			t.Fatalf("route %q snapshot = %+v, error = %v", routeID, snapshot, snapshotErr)
		}
	}
}
