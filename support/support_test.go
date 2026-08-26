package support

import (
	"testing"

	"github.com/proven-trade/cex-sdk/model"
)

func TestCatalogLookupAndCopyIsolation(t *testing.T) {
	t.Parallel()

	entry, ok := Lookup(model.ExchangeKorbit, ProductSpot)
	if !ok || !entry.REST.Implemented() || !entry.WebSocketPublic.Implemented() ||
		entry.OperationallyVerified() || len(entry.Docs) != 1 {
		t.Fatalf("Korbit Spot support = %+v, found = %v", entry, ok)
	}
	entry.Docs[0] = "changed"
	again, ok := Lookup(model.ExchangeKorbit, ProductSpot)
	if !ok || again.Docs[0] != "docs/exchanges/KORBIT.md" {
		t.Fatalf("catalog mutated through Lookup() = %+v", again)
	}
	all := All()
	all[0].Docs[0] = "changed"
	fresh := All()
	if fresh[0].Docs[0] == "changed" {
		t.Fatal("catalog mutated through All()")
	}
}

func TestCatalogCoversBuiltInExchangesAndHasUniqueProducts(t *testing.T) {
	t.Parallel()

	builtIn := []model.ExchangeID{
		model.ExchangeBinance, model.ExchangeBitget, model.ExchangeUpbit,
		model.ExchangeBybit, model.ExchangeOKX, model.ExchangeCoinbase,
		model.ExchangeKraken, model.ExchangeBithumb, model.ExchangeCoinone,
		model.ExchangeKorbit,
	}
	entries := All()
	seen := make(map[string]struct{}, len(entries))
	covered := make(map[model.ExchangeID]bool, len(builtIn))
	for _, entry := range entries {
		key := string(entry.Exchange) + "/" + string(entry.Product)
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate catalog entry %s", key)
		}
		seen[key] = struct{}{}
		if entry.REST.Implemented() {
			covered[entry.Exchange] = true
		}
	}
	for _, exchange := range builtIn {
		if !covered[exchange] {
			t.Fatalf("built-in exchange %s has no implemented REST product", exchange)
		}
	}
}

func TestStatusImplemented(t *testing.T) {
	t.Parallel()

	if !StatusImplemented.Implemented() {
		t.Fatal("implemented status returned false")
	}
	for _, status := range []Status{StatusPlanned, StatusPending, StatusNotApplicable} {
		if status.Implemented() {
			t.Fatalf("status %q returned true", status)
		}
	}
}
