package korbit

import (
	"net/http"
	"testing"

	"github.com/proven-trade/proven-trade-sdk/ratelimit"
)

func TestRateLimitScopesGroupsAndHeader(t *testing.T) {
	t.Parallel()

	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	public, _, err := publicRateLimit(limiter, "route-a", 50)
	if err != nil {
		t.Fatalf("publicRateLimit() error = %v", err)
	}
	private, _, err := privateRateLimit(limiter, "account-a", rateGroupPrivate, 50, 30)
	if err != nil {
		t.Fatalf("privateRateLimit() error = %v", err)
	}
	place, _, err := privateRateLimit(limiter, "account-a", rateGroupOrderPlace, 50, 30)
	if err != nil {
		t.Fatalf("privateRateLimit(place) error = %v", err)
	}
	cancel, _, err := privateRateLimit(limiter, "account-a", rateGroupOrderCancel, 50, 30)
	if err != nil {
		t.Fatalf("privateRateLimit(cancel) error = %v", err)
	}
	if public.key != "korbit:route:route-a:public:1second" || public.limit != 50 ||
		private.key != "korbit:account:account-a:private:1second" || private.limit != 50 ||
		place.key != "korbit:account:account-a:order-place:1second" || place.limit != 30 ||
		cancel.key != "korbit:account:account-a:order-cancel:1second" || cancel.limit != 30 {
		t.Fatalf("limits = public %+v, private %+v, place %+v, cancel %+v", public, private, place, cancel)
	}
	header := make(http.Header)
	header.Set("Ratelimit", "limit=50, remaining=42, reset=1")
	observeRateLimit(limiter, public, header)
	snapshot, err := limiter.Snapshot(public.key)
	if err != nil || snapshot.Used != 8 {
		t.Fatalf("public snapshot = %+v, error = %v", snapshot, err)
	}
	if remaining, ok := parseRateLimitRemaining("limit=30, remaining=17, reset=1"); !ok || remaining != 17 {
		t.Fatalf("remaining = %d, ok = %v", remaining, ok)
	}
}
