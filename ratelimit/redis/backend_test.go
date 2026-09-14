package redislimit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/proven-trade/cex-sdk/v2/ratelimit"
	"github.com/redis/go-redis/v9"
)

func redisBackends(t *testing.T) (*Backend, *Backend) {
	t.Helper()
	address := os.Getenv("CEX_SDK_REDIS_ADDR")
	if address == "" {
		t.Skip("set CEX_SDK_REDIS_ADDR to run Redis integration tests")
	}
	namespace := fmt.Sprintf("test:%s:%d", t.Name(), time.Now().UnixNano())
	makeBackend := func() *Backend {
		client := redis.NewClient(&redis.Options{Addr: address, ContextTimeoutEnabled: true, MaxRetries: -1})
		t.Cleanup(func() { _ = client.Close() })
		backend, err := New(Config{Client: client, Namespace: namespace, OperationTimeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		return backend
	}
	a, b := makeBackend(), makeBackend()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		iter := a.client.Scan(ctx, 0, a.prefix+"*", 100).Iterator()
		for iter.Next(ctx) {
			if err := a.client.Del(ctx, iter.Val()).Err(); err != nil {
				t.Error(err)
			}
		}
		if err := iter.Err(); err != nil {
			t.Error(err)
		}
	})
	return a, b
}

func TestSharedQuotaAndAtomicDimensions(t *testing.T) {
	a, b := redisBackends(t)
	for _, r := range []ratelimit.Rule{{Key: "account", Limit: 1, Window: 10 * time.Second}, {Key: "route", Limit: 5, Window: 10 * time.Second}} {
		if err := a.SetRule(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Wait(context.Background(), ratelimit.Charge{Key: "account", Units: 1}, ratelimit.Charge{Key: "route", Units: 1}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := b.Wait(ctx, ratelimit.Charge{Key: "account", Units: 1}, ratelimit.Charge{Key: "route", Units: 1}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second process error=%v", err)
	}
	snapshot, err := b.Snapshot("route")
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("partial charge: %+v %v", snapshot, err)
	}
	if err := b.Wait(context.Background(), ratelimit.Charge{Key: "route", Units: 1}, ratelimit.Charge{Key: "missing", Units: 1}); !errors.Is(err, ratelimit.ErrUnknownRule) {
		t.Fatal(err)
	}
	snapshot, err = b.Snapshot("route")
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("unknown rule consumed quota: %+v %v", snapshot, err)
	}
	if err := b.Wait(context.Background(), ratelimit.Charge{Key: "account", Units: 2}); !errors.Is(err, ratelimit.ErrInvalidRule) {
		t.Fatal(err)
	}
}

func TestConcurrentClientsCannotExceedQuota(t *testing.T) {
	a, b := redisBackends(t)
	if err := a.SetRule(ratelimit.Rule{Key: "uid", Limit: 7, Window: 10 * time.Second}); err != nil {
		t.Fatal(err)
	}
	var passed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		backend := a
		if i%2 != 0 {
			backend = b
		}
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			err := backend.Wait(ctx, ratelimit.Charge{Key: "uid", Units: 1})
			if err == nil {
				passed.Add(1)
			} else if !errors.Is(err, context.DeadlineExceeded) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if passed.Load() != 7 {
		t.Fatalf("granted=%d, want 7", passed.Load())
	}
	state, err := b.Snapshot("uid")
	if err != nil || state.Used != 7 {
		t.Fatalf("snapshot=%+v %v", state, err)
	}
}

func TestRollingExpirationObservationsAndCooldown(t *testing.T) {
	a, b := redisBackends(t)
	rule := ratelimit.Rule{Key: "uid", Limit: 5, Window: 150 * time.Millisecond}
	if err := a.SetRule(rule); err != nil {
		t.Fatal(err)
	}
	if err := a.ObserveUsed("uid", 5); err != nil {
		t.Fatal(err)
	}
	if err := b.ObserveUsed("uid", 1); err != nil {
		t.Fatal(err)
	}
	if state, err := b.Snapshot("uid"); err != nil || state.Used != 5 || state.WindowStart.IsZero() {
		t.Fatalf("snapshot=%+v %v", state, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := b.Wait(ctx, ratelimit.Charge{Key: "uid", Units: 2}); err != nil {
		t.Fatal(err)
	}
	if state, err := a.Snapshot("uid"); err != nil || state.Used != 2 {
		t.Fatalf("expired usage=%+v %v", state, err)
	}
	if err := a.BlockFor([]string{"uid"}, time.Second); err != nil {
		t.Fatal(err)
	}
	before, _ := b.Snapshot("uid")
	if err := b.BlockFor([]string{"uid"}, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := b.SetRule(rule); err != nil {
		t.Fatal(err)
	}
	rule.Limit = 10
	if err := b.SetRule(rule); err != nil {
		t.Fatal(err)
	}
	after, err := a.Snapshot("uid")
	if err != nil || after.BlockedUntil.Before(before.BlockedUntil) || after.Used != 2 {
		t.Fatalf("rule update erased state: %+v %v", after, err)
	}
	blocked, cancelBlocked := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelBlocked()
	if err := b.Wait(blocked, ratelimit.Charge{Key: "uid", Units: 1}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	rule.Window = 2 * time.Second
	if err := b.SetRule(rule); err != nil {
		t.Fatal(err)
	}
	after, err = a.Snapshot("uid")
	if err != nil || !after.BlockedUntil.After(before.BlockedUntil) {
		t.Fatalf("window update=%+v %v", after, err)
	}
}

func TestRedisValidationIsolationAndUnavailableStore(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", ContextTimeoutEnabled: true, MaxRetries: -1})
	defer client.Close()
	if _, err := New(Config{}); err == nil {
		t.Fatal("nil client")
	}
	if _, err := New(Config{Client: client}); err == nil {
		t.Fatal("empty namespace")
	}
	if _, err := New(Config{Client: client, Namespace: "x", OperationTimeout: -1}); err == nil {
		t.Fatal("negative timeout")
	}
	backend, err := New(Config{Client: client, Namespace: "x", OperationTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := New(Config{Client: client, Namespace: "y"})
	if backend.keys([]string{"a"})[0] == other.keys([]string{"a"})[0] {
		t.Fatal("namespace collision")
	}
	keys := backend.keys([]string{"a", "b"})
	tag := strings.Split(strings.Split(keys[0], "{")[1], "}")[0]
	for _, key := range keys {
		if !strings.Contains(key, "{"+tag+"}") {
			t.Fatal("cross-slot keys")
		}
	}
	for _, r := range []ratelimit.Rule{{Key: "", Limit: 1, Window: 1}, {Key: "x", Limit: 0, Window: 1}, {Key: "x", Limit: 1, Window: 0}} {
		if err := backend.SetRule(r); !errors.Is(err, ratelimit.ErrInvalidRule) {
			t.Fatal(err)
		}
	}
	if err := backend.Wait(nil); err == nil {
		t.Fatal("nil context")
	}
	if err := backend.Wait(context.Background(), ratelimit.Charge{Key: "a", Units: 0}); !errors.Is(err, ratelimit.ErrInvalidRule) {
		t.Fatal(err)
	}
	if err := backend.ObserveUsed("a", -1); !errors.Is(err, ratelimit.ErrInvalidRule) {
		t.Fatal(err)
	}
	if err := backend.BlockFor(nil, 0); !errors.Is(err, ratelimit.ErrInvalidRule) {
		t.Fatal(err)
	}
	if err := backend.BlockFor(nil, time.Second); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := backend.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := backend.Wait(context.Background(), ratelimit.Charge{Key: "a", Units: 1}); err == nil {
		t.Fatal("storage outage granted quota")
	}
}
