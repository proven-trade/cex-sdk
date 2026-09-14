package unified

import (
	"context"
	"fmt"
	"sync"
	"time"

	trade "github.com/proven-trade/cex-sdk/v2"
	"github.com/proven-trade/cex-sdk/v2/transport"
)

// ValidationConfig configures opt-in rule validation before Spot order submission.
type ValidationConfig struct {
	// DefaultEgressRouteID is used for both rule reads and orders unless overridden.
	DefaultEgressRouteID transport.EgressRouteID
	// CacheTTL defaults to one minute. Expired rules are never used on refresh failure.
	CacheTTL time.Duration
}

// ValidatedSpot validates PlaceOrder against cached Markets metadata. Other methods
// retain the wrapped client's behavior. It never rounds quantities or retries orders.
type ValidatedSpot struct {
	SpotClient
	config ValidationConfig
	mu     sync.Mutex
	rules  map[transport.EgressRouteID]*marketRulesEntry
}

type marketRulesEntry struct {
	ready   chan struct{}
	expires time.Time
	markets map[Market]MarketInfo
	err     error
}

var _ SpotClient = (*ValidatedSpot)(nil)

// NewValidatedSpot enables preflight validation for any Spot adapter. Rules with
// empty metadata fields remain unchecked, as in MarketInfo.ValidateOrder.
func NewValidatedSpot(client SpotClient, config ValidationConfig) (*ValidatedSpot, error) {
	if client == nil || !client.Exchange().Valid() {
		return nil, fmt.Errorf("validated Spot client is required")
	}
	options, err := trade.ResolveRequestOptions(config.DefaultEgressRouteID)
	if err != nil {
		return nil, err
	}
	config.DefaultEgressRouteID = options.EgressRouteID
	if config.CacheTTL < 0 {
		return nil, fmt.Errorf("market rule cache TTL cannot be negative")
	}
	if config.CacheTTL == 0 {
		config.CacheTTL = time.Minute
	}
	return &ValidatedSpot{SpotClient: client, config: config, rules: make(map[transport.EgressRouteID]*marketRulesEntry)}, nil
}

// PlaceOrder refreshes expired metadata on the order's route, rejects invalid
// orders locally, then submits once. WithTimeout covers preflight and submission.
func (client *ValidatedSpot) PlaceOrder(ctx context.Context, request PlaceOrderRequest, options ...trade.RequestOption) (Order, error) {
	if ctx == nil {
		return Order{}, fmt.Errorf("order context cannot be nil")
	}
	if err := request.Validate(); err != nil {
		return Order{}, err
	}
	resolved, err := trade.ResolveRequestOptions(client.config.DefaultEgressRouteID, options...)
	if err != nil {
		return Order{}, err
	}
	forward := []trade.RequestOption{trade.WithEgressRoute(resolved.EgressRouteID)}
	if resolved.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, resolved.Timeout)
		defer cancel()
		forward = append(forward, trade.WithTimeout(resolved.Timeout))
	}
	if err := ctx.Err(); err != nil {
		return Order{}, err
	}
	markets, err := client.marketRules(ctx, resolved.EgressRouteID, forward)
	if err != nil {
		return Order{}, err
	}
	info, found := markets[request.Market]
	if !found {
		return Order{}, validationError("market rules not found for %s", request.Market)
	}
	if err := info.ValidateOrder(request); err != nil {
		return Order{}, err
	}
	if err := ctx.Err(); err != nil {
		return Order{}, err
	}
	return client.SpotClient.PlaceOrder(ctx, request, forward...)
}

// InvalidateMarketRules makes subsequent orders refresh their route's metadata.
// Already-running preflight reads are not canceled.
func (client *ValidatedSpot) InvalidateMarketRules() {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.rules = make(map[transport.EgressRouteID]*marketRulesEntry)
}

func (client *ValidatedSpot) marketRules(ctx context.Context, route transport.EgressRouteID, options []trade.RequestOption) (map[Market]MarketInfo, error) {
	client.mu.Lock()
	entry := client.rules[route]
	if entry != nil && (entry.expires.IsZero() || time.Now().Before(entry.expires)) {
		client.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-entry.ready:
			return entry.markets, entry.err
		}
	}
	entry = &marketRulesEntry{ready: make(chan struct{})}
	client.rules[route] = entry
	client.mu.Unlock()

	items, err := client.SpotClient.Markets(ctx, options...)
	markets := make(map[Market]MarketInfo, len(items))
	if err == nil {
		for _, item := range items {
			if item.Exchange != client.Exchange() || item.Market.Validate() != nil {
				err = fmt.Errorf("invalid market identity in rule metadata")
				break
			}
			if _, exists := markets[item.Market]; exists {
				err = fmt.Errorf("duplicate market rules for %s", item.Market)
				break
			}
			// Cache only value fields; raw response buffers are not needed for validation.
			item.Raw = nil
			markets[item.Market] = item
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	client.mu.Lock()
	entry.markets, entry.err = markets, err
	entry.expires = time.Now().Add(client.config.CacheTTL)
	if err != nil && client.rules[route] == entry {
		delete(client.rules, route)
	}
	close(entry.ready)
	client.mu.Unlock()
	return markets, err
}
