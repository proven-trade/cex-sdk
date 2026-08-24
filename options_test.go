package trade

import (
	"errors"
	"testing"
	"time"

	"github.com/proven-trade/proven-trade-sdk/transport"
)

func TestResolveRequestOptions(t *testing.T) {
	t.Parallel()

	resolved, err := ResolveRequestOptions(
		transport.EgressRouteID("default"),
		WithEgressRoute("override"),
		WithTimeout(2*time.Second),
	)
	if err != nil {
		t.Fatalf("ResolveRequestOptions() error = %v", err)
	}
	if resolved.EgressRouteID != "override" {
		t.Fatalf("EgressRouteID = %q, want override", resolved.EgressRouteID)
	}
	if resolved.Timeout != 2*time.Second {
		t.Fatalf("Timeout = %s, want 2s", resolved.Timeout)
	}
}

func TestResolveRequestOptionsRequiresRoute(t *testing.T) {
	t.Parallel()

	_, err := ResolveRequestOptions("")
	if !errors.Is(err, ErrMissingEgressRoute) {
		t.Fatalf("error = %v, want ErrMissingEgressRoute", err)
	}
}

func TestWithTimeoutRejectsNonPositiveValue(t *testing.T) {
	t.Parallel()

	_, err := ResolveRequestOptions("route-a", WithTimeout(0))
	if !errors.Is(err, ErrInvalidRequestOption) {
		t.Fatalf("error = %v, want ErrInvalidRequestOption", err)
	}
}
