// Package redislimit implements a shared rolling-window limiter using Redis Lua.
package redislimit

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/proven-trade/cex-sdk/v2/ratelimit"
	"github.com/redis/go-redis/v9"
)

//go:embed backend.lua
var scriptSource string

var script = redis.NewScript(scriptSource)

const (
	maxUnits    = 1<<31 - 1
	maxDuration = time.Duration(1<<63-1) / time.Microsecond * time.Microsecond
)

// Config defines the Redis connection and the namespace shared by SDK processes.
type Config struct {
	// Client is owned by the caller. ContextTimeoutEnabled must be true.
	Client redis.UniversalClient
	// Namespace must match across processes sharing a quota. Use stable account
	// and route IDs in SDK configurations as well.
	Namespace string
	// OperationTimeout bounds each Redis call, including methods without a context.
	// It defaults to two seconds.
	OperationTimeout time.Duration
}

// Backend keeps all keys for a namespace in a single Redis Cluster hash slot.
// Redis server time is authoritative. Storage errors never grant quota.
type Backend struct {
	client  redis.UniversalClient
	prefix  string
	timeout time.Duration
}

var _ ratelimit.Backend = (*Backend)(nil)

// New creates a backend without connecting to Redis. The caller owns Client.Close.
func New(config Config) (*Backend, error) {
	if config.Client == nil {
		return nil, fmt.Errorf("Redis client is required")
	}
	var respectsContext bool
	switch client := config.Client.(type) {
	case *redis.Client:
		respectsContext = client != nil && client.Options().ContextTimeoutEnabled
	case *redis.ClusterClient:
		respectsContext = client != nil && client.Options().ContextTimeoutEnabled
	case *redis.Ring:
		respectsContext = client != nil && client.Options().ContextTimeoutEnabled
	default:
		return nil, fmt.Errorf("Redis limiter requires a standard Redis client, cluster, or ring")
	}
	if !respectsContext {
		return nil, fmt.Errorf("Redis limiter requires ContextTimeoutEnabled")
	}
	config.Namespace = strings.TrimSpace(config.Namespace)
	if config.Namespace == "" {
		return nil, fmt.Errorf("Redis limiter namespace is required")
	}
	if config.OperationTimeout < 0 {
		return nil, fmt.Errorf("Redis operation timeout cannot be negative")
	}
	if config.OperationTimeout == 0 {
		config.OperationTimeout = 2 * time.Second
	}
	return &Backend{client: config.Client, prefix: fmt.Sprintf("cex-sdk:ratelimit:{%x}:", sha256.Sum256([]byte(config.Namespace))), timeout: config.OperationTimeout}, nil
}

func (backend *Backend) keys(names []string) []string {
	keys := make([]string, 0, len(names)*2)
	for _, name := range names {
		base := backend.prefix + fmt.Sprintf("%x", sha256.Sum256([]byte(name)))
		keys = append(keys, base+":rule", base+":events")
	}
	return keys
}

func (backend *Backend) call(ctx context.Context, names []string, args ...any) ([]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, backend.timeout)
	defer cancel()
	values, err := script.Run(ctx, backend.client, backend.keys(names), args...).Int64Slice()
	if err != nil {
		// Preserve context identity without rendering connection credentials or URLs.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("Redis limiter operation failed: %w", err)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("Redis limiter returned an empty response")
	}
	switch values[0] {
	case -1:
		return nil, ratelimit.ErrUnknownRule
	case -2:
		return nil, ratelimit.ErrInvalidRule
	}
	return values, nil
}

// SetRule preserves usage and cooldown when a rule is updated. A window change
// blocks for a full new window because previously expired history is unavailable.
func (backend *Backend) SetRule(rule ratelimit.Rule) error {
	return backend.SetRuleContext(context.Background(), rule)
}

// SetRuleContext registers a rule within the caller's deadline. OperationTimeout
// is an additional upper bound; it never extends the request budget.
func (backend *Backend) SetRuleContext(ctx context.Context, rule ratelimit.Rule) error {
	if ctx == nil {
		return fmt.Errorf("limiter context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rule.Key = strings.TrimSpace(rule.Key)
	if rule.Key == "" || rule.Limit <= 0 || rule.Limit > maxUnits || rule.Window <= 0 || rule.Window > maxDuration {
		return ratelimit.ErrInvalidRule
	}
	_, err := backend.call(ctx, []string{rule.Key}, "set", rule.Limit, durationMicros(rule.Window))
	return err
}

// Wait checks and charges every dimension atomically, waiting with cancellation.
func (backend *Backend) Wait(ctx context.Context, charges ...ratelimit.Charge) error {
	if ctx == nil {
		return fmt.Errorf("limiter context cannot be nil")
	}
	combined := make(map[string]int)
	for _, charge := range charges {
		key := strings.TrimSpace(charge.Key)
		if key == "" || charge.Units <= 0 || charge.Units > maxUnits-combined[key] {
			return ratelimit.ErrInvalidRule
		}
		combined[key] += charge.Units
	}
	names := make([]string, 0, len(combined))
	for name := range combined {
		names = append(names, name)
	}
	sort.Strings(names)
	args := []any{"wait"}
	for _, name := range names {
		args = append(args, combined[name])
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(names) == 0 {
			return nil
		}
		values, err := backend.call(ctx, names, args...)
		if err != nil {
			return err
		}
		if values[0] == 0 {
			return nil
		}
		timer := time.NewTimer(time.Duration(values[0]) * time.Microsecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// ObserveUsed conservatively raises shared usage to the server-observed value.
func (backend *Backend) ObserveUsed(key string, used int) error {
	if used < 0 || used > maxUnits {
		return ratelimit.ErrInvalidRule
	}
	_, err := backend.call(context.Background(), []string{key}, "observe", used)
	return err
}

// BlockFor extends shared cooldowns without shortening an existing block.
func (backend *Backend) BlockFor(keys []string, duration time.Duration) error {
	if duration <= 0 || duration > maxDuration {
		return ratelimit.ErrInvalidRule
	}
	if len(keys) == 0 {
		return nil
	}
	_, err := backend.call(context.Background(), keys, "block", durationMicros(duration))
	return err
}

// Snapshot returns state using the same server clock as acquisition.
func (backend *Backend) Snapshot(key string) (ratelimit.Snapshot, error) {
	values, err := backend.call(context.Background(), []string{key}, "snapshot")
	if err != nil {
		return ratelimit.Snapshot{}, err
	}
	if len(values) != 6 {
		return ratelimit.Snapshot{}, errors.New("invalid Redis limiter snapshot")
	}
	result := ratelimit.Snapshot{Rule: ratelimit.Rule{Key: key, Limit: int(values[1]), Window: time.Duration(values[2]) * time.Microsecond}, Used: int(values[3])}
	if values[4] > 0 {
		result.WindowStart = time.UnixMicro(values[4])
	}
	if values[5] > 0 {
		result.BlockedUntil = time.UnixMicro(values[5])
	}
	return result, nil
}

func durationMicros(value time.Duration) int64 {
	micros := value.Microseconds()
	if value%time.Microsecond != 0 {
		micros++
	}
	return micros
}
