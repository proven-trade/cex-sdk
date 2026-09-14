package unified

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk/v2"
	"github.com/proven-trade/cex-sdk/v2/model"
	"github.com/proven-trade/cex-sdk/v2/transport"
)

type validationClient struct {
	SpotClient
	markets func(context.Context, ...trade.RequestOption) ([]MarketInfo, error)
	place   func(context.Context, PlaceOrderRequest, ...trade.RequestOption) (Order, error)
}

func (*validationClient) Exchange() model.ExchangeID { return model.ExchangeBinance }
func (c *validationClient) Markets(ctx context.Context, o ...trade.RequestOption) ([]MarketInfo, error) {
	return c.markets(ctx, o...)
}
func (c *validationClient) PlaceOrder(ctx context.Context, r PlaceOrderRequest, o ...trade.RequestOption) (Order, error) {
	return c.place(ctx, r, o...)
}

func validatedRequest() PlaceOrderRequest {
	return PlaceOrderRequest{Market: Market{Base: "BTC", Quote: "USDT"}, Side: SideBuy, Type: OrderTypeLimit, Price: "100", Quantity: "0.2", ClientOrderID: "unique-order"}
}
func validationRules() []MarketInfo {
	return []MarketInfo{{Exchange: model.ExchangeBinance, Market: validatedRequest().Market, PriceIncrement: "0.5", QuantityIncrement: "0.1", MinimumQuoteAmount: "10"}}
}

func TestValidatedSpotRejectsBeforeSubmission(t *testing.T) {
	var reads, orders int
	native := &validationClient{
		markets: func(context.Context, ...trade.RequestOption) ([]MarketInfo, error) {
			reads++
			return validationRules(), nil
		},
		place: func(context.Context, PlaceOrderRequest, ...trade.RequestOption) (Order, error) {
			orders++
			return Order{}, nil
		},
	}
	client, err := NewValidatedSpot(native, ValidationConfig{DefaultEgressRouteID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*PlaceOrderRequest){
		func(r *PlaceOrderRequest) { r.Price = "100.1" },
		func(r *PlaceOrderRequest) { r.Quantity = "0.01" },
		func(r *PlaceOrderRequest) { r.Price = "10" },
		func(r *PlaceOrderRequest) { r.Market.Base = "ETH" },
		func(r *PlaceOrderRequest) { r.ClientOrderID = "" },
	} {
		r := validatedRequest()
		mutate(&r)
		if _, err := client.PlaceOrder(context.Background(), r); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if reads != 1 || orders != 0 {
		t.Fatalf("reads=%d orders=%d", reads, orders)
	}
	if _, err := client.PlaceOrder(context.Background(), validatedRequest()); err != nil {
		t.Fatal(err)
	}
	if orders != 1 {
		t.Fatalf("orders=%d", orders)
	}
}

func TestValidatedSpotCachesByRouteAndRefreshesWithoutStaleFallback(t *testing.T) {
	var mu sync.Mutex
	reads := map[transport.EgressRouteID]int{}
	var fail bool
	native := &validationClient{
		markets: func(_ context.Context, options ...trade.RequestOption) ([]MarketInfo, error) {
			resolved, err := trade.ResolveRequestOptions("", options...)
			if err != nil {
				return nil, err
			}
			mu.Lock()
			defer mu.Unlock()
			reads[resolved.EgressRouteID]++
			if fail {
				return nil, errors.New("metadata unavailable")
			}
			return validationRules(), nil
		},
		place: func(_ context.Context, _ PlaceOrderRequest, options ...trade.RequestOption) (Order, error) {
			resolved, err := trade.ResolveRequestOptions("", options...)
			if err != nil {
				return Order{}, err
			}
			if resolved.EgressRouteID != "a" && resolved.EgressRouteID != "b" {
				t.Errorf("lost route")
			}
			return Order{}, nil
		},
	}
	client, _ := NewValidatedSpot(native, ValidationConfig{DefaultEgressRouteID: "a"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.PlaceOrder(context.Background(), validatedRequest()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if _, err := client.PlaceOrder(context.Background(), validatedRequest(), trade.WithEgressRoute("b")); err != nil {
		t.Fatal(err)
	}
	if reads["a"] != 1 || reads["b"] != 1 {
		t.Fatalf("reads=%v", reads)
	}
	// Expire without wall-clock sleeps, then verify a failed refresh denies submission.
	client.mu.Lock()
	client.rules["a"].expires = time.Now().Add(-time.Second)
	client.mu.Unlock()
	fail = true
	if _, err := client.PlaceOrder(context.Background(), validatedRequest()); err == nil {
		t.Fatal("used stale rules")
	}
	fail = false
	if _, err := client.PlaceOrder(context.Background(), validatedRequest()); err != nil {
		t.Fatal(err)
	}
	client.InvalidateMarketRules()
	if _, err := client.PlaceOrder(context.Background(), validatedRequest()); err != nil {
		t.Fatal(err)
	}
	if reads["a"] != 4 {
		t.Fatalf("refresh reads=%d", reads["a"])
	}
}

func TestValidatedSpotTimeoutAndCanceledWaiter(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var submissions atomic.Int32
	native := &validationClient{
		markets: func(ctx context.Context, _ ...trade.RequestOption) ([]MarketInfo, error) {
			close(started)
			select {
			case <-release:
				return validationRules(), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		place: func(context.Context, PlaceOrderRequest, ...trade.RequestOption) (Order, error) {
			submissions.Add(1)
			return Order{}, nil
		},
	}
	client, _ := NewValidatedSpot(native, ValidationConfig{DefaultEgressRouteID: "a"})
	done := make(chan error, 1)
	go func() {
		_, err := client.PlaceOrder(context.Background(), validatedRequest(), trade.WithTimeout(150*time.Millisecond))
		done <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := client.PlaceOrder(ctx, validatedRequest()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter error=%v", err)
	}
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("preflight timeout=%v", err)
	}
	close(release)
	if submissions.Load() != 0 {
		t.Fatal("submitted after preflight timeout")
	}
}

func TestValidatedSpotRejectsInvalidMetadataAndConfiguration(t *testing.T) {
	native := &validationClient{markets: func(context.Context, ...trade.RequestOption) ([]MarketInfo, error) {
		return append(validationRules(), validationRules()...), nil
	}}
	if _, err := NewValidatedSpot(nil, ValidationConfig{}); err == nil {
		t.Fatal("accepted nil")
	}
	if _, err := NewValidatedSpot(native, ValidationConfig{}); err == nil {
		t.Fatal("accepted missing route")
	}
	if _, err := NewValidatedSpot(native, ValidationConfig{DefaultEgressRouteID: "a", CacheTTL: -1}); err == nil {
		t.Fatal("accepted TTL")
	}
	client, _ := NewValidatedSpot(native, ValidationConfig{DefaultEgressRouteID: "a"})
	if _, err := client.PlaceOrder(nil, validatedRequest()); err == nil {
		t.Fatal("accepted nil context")
	}
	if _, err := client.PlaceOrder(context.Background(), validatedRequest(), nil); err == nil {
		t.Fatal("accepted invalid option")
	}
	if _, err := client.PlaceOrder(context.Background(), validatedRequest()); err == nil {
		t.Fatal("accepted duplicate rules")
	}
	native.markets = func(context.Context, ...trade.RequestOption) ([]MarketInfo, error) {
		items := validationRules()
		items[0].Exchange = model.ExchangeUpbit
		return items, nil
	}
	if _, err := client.PlaceOrder(context.Background(), validatedRequest()); err == nil {
		t.Fatal("accepted wrong exchange")
	}
}

// Signals when the waiter reaches the select on a shared metadata refresh.
type metadataWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (ctx *metadataWaitContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.waiting) })
	return ctx.Context.Done()
}

func TestValidatedSpotRetriesRefreshOwnedByCanceledCaller(t *testing.T) {
	for _, cache := range []string{"uncached", "expired"} {
		for _, cancellation := range []string{"cancel", "timeout"} {
			t.Run(cache+"/"+cancellation, func(t *testing.T) {
				started := make(chan struct{})
				var reads, submissions atomic.Int32
				// A submission error must be returned after exactly one attempt.
				orderErr := errors.New("order result unavailable")
				native := &validationClient{
					markets: func(ctx context.Context, options ...trade.RequestOption) ([]MarketInfo, error) {
						resolved, err := trade.ResolveRequestOptions("", options...)
						if err != nil || resolved.EgressRouteID != "a" {
							t.Errorf("refresh route=%v error=%v", resolved.EgressRouteID, err)
						}
						if reads.Add(1) == 1 {
							close(started)
							<-ctx.Done()
							// Native clients can wrap cancellation in APIError.
							return nil, &trade.APIError{Category: trade.ErrorTimeout, Cause: ctx.Err()}
						}
						return validationRules(), nil
					},
					place: func(context.Context, PlaceOrderRequest, ...trade.RequestOption) (Order, error) {
						submissions.Add(1)
						return Order{}, orderErr
					},
				}
				client, err := NewValidatedSpot(native, ValidationConfig{DefaultEgressRouteID: "a"})
				if err != nil {
					t.Fatal(err)
				}
				if cache == "expired" {
					ready := make(chan struct{})
					close(ready)
					client.rules["a"] = &marketRulesEntry{ready: ready, expires: time.Now().Add(-time.Second)}
				}
				ownerCtx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var ownerOptions []trade.RequestOption
				wantErr := context.Canceled
				if cancellation == "timeout" {
					ownerOptions = []trade.RequestOption{trade.WithTimeout(100 * time.Millisecond)}
					wantErr = context.DeadlineExceeded
				}
				ownerDone := make(chan error, 1)
				go func() {
					_, err := client.PlaceOrder(ownerCtx, validatedRequest(), ownerOptions...)
					ownerDone <- err
				}()
				<-started
				const waiters = 8
				waiterDone := make(chan error, waiters)
				for i := 0; i < waiters; i++ {
					ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
					defer stop()
					waiterCtx := &metadataWaitContext{Context: ctx, waiting: make(chan struct{})}
					go func() {
						_, err := client.PlaceOrder(waiterCtx, validatedRequest())
						waiterDone <- err
					}()
					<-waiterCtx.waiting
				}
				if cancellation == "cancel" {
					cancel()
				}
				if err := <-ownerDone; !errors.Is(err, wantErr) {
					t.Fatalf("owner error=%v", err)
				}
				for i := 0; i < waiters; i++ {
					if err := <-waiterDone; !errors.Is(err, orderErr) {
						t.Errorf("live waiter error=%v", err)
					}
				}
				if reads.Load() != 2 || submissions.Load() != waiters {
					t.Fatalf("reads=%d submissions=%d", reads.Load(), submissions.Load())
				}
			})
		}
	}
}

func TestValidatedSpotDoesNotRetryUnrelatedRefreshError(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var reads atomic.Int32
	native := &validationClient{markets: func(context.Context, ...trade.RequestOption) ([]MarketInfo, error) {
		if reads.Add(1) == 1 {
			close(started)
		}
		<-release
		// An upstream timeout with a live caller is not caller cancellation.
		return nil, context.DeadlineExceeded
	}}
	client, err := NewValidatedSpot(native, ValidationConfig{DefaultEgressRouteID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	go func() {
		_, err := client.PlaceOrder(context.Background(), validatedRequest())
		done <- err
	}()
	<-started
	waiterCtx := &metadataWaitContext{Context: context.Background(), waiting: make(chan struct{})}
	go func() {
		_, err := client.PlaceOrder(waiterCtx, validatedRequest())
		done <- err
	}()
	<-waiterCtx.waiting
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("refresh error=%v", err)
		}
	}
	if reads.Load() != 1 {
		t.Fatalf("retried upstream timeout %d times", reads.Load())
	}
}
