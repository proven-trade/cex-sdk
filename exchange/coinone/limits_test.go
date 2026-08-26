package coinone

import (
	"net/http"
	"testing"

	"github.com/proven-trade/cex-sdk/ratelimit"
)

func TestRateLimitScopesAndHeaders(t *testing.T) {
	t.Parallel()

	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	public, publicCharges, err := publicRateLimit(limiter, "route-a", 1200)
	if err != nil {
		t.Fatalf("publicRateLimit() error = %v", err)
	}
	private, privateCharges, err := privateRateLimit(limiter, "portfolio-a", false, 80, 40)
	if err != nil {
		t.Fatalf("privateRateLimit() error = %v", err)
	}
	order, orderCharges, err := privateRateLimit(limiter, "portfolio-a", true, 80, 40)
	if err != nil {
		t.Fatalf("privateRateLimit() error = %v", err)
	}
	if public.key != "coinone:route:route-a:public:1minute" || len(publicCharges) != 1 {
		t.Fatalf("public limit = %+v, charges = %+v", public, publicCharges)
	}
	if private.key != "coinone:account:portfolio-a:private:1second" || len(privateCharges) != 1 {
		t.Fatalf("private limit = %+v, charges = %+v", private, privateCharges)
	}
	if order.key != "coinone:account:portfolio-a:order:1second" || len(orderCharges) != 1 {
		t.Fatalf("order limit = %+v, charges = %+v", order, orderCharges)
	}

	header := make(http.Header)
	header.Set("Public-Ratelimit-Remaining", "1175")
	header.Set("Private-Ratelimit-Remaining", "70")
	header.Set("Private-Order-Ratelimit-Remaining", "35")
	observeRateLimit(limiter, public, header)
	observeRateLimit(limiter, private, header)
	observeRateLimit(limiter, order, header)
	publicSnapshot, _ := limiter.Snapshot(public.key)
	privateSnapshot, _ := limiter.Snapshot(private.key)
	orderSnapshot, _ := limiter.Snapshot(order.key)
	if publicSnapshot.Used != 25 || privateSnapshot.Used != 10 || orderSnapshot.Used != 5 {
		t.Fatalf("observed usage = public %d, private %d, order %d", publicSnapshot.Used, privateSnapshot.Used, orderSnapshot.Used)
	}
}
