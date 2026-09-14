package redislimit

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk/v2"
	"github.com/proven-trade/cex-sdk/v2/credential"
	"github.com/proven-trade/cex-sdk/v2/exchange"
	"github.com/proven-trade/cex-sdk/v2/exchange/binance"
	"github.com/proven-trade/cex-sdk/v2/model"
	"github.com/proven-trade/cex-sdk/v2/ratelimit"
	"github.com/proven-trade/cex-sdk/v2/transport"
	"github.com/redis/go-redis/v9"
)

// Intercept Redis commands to stall only this client's registration, without
// pausing the shared integration server or depending on a network timeout.
type processHook redis.ProcessHook

func (hook processHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (hook processHook) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return redis.ProcessHook(hook)
}
func (hook processHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

type requestSender func(context.Context, transport.EgressRouteID, *http.Request) (*http.Response, error)

func (send requestSender) Do(ctx context.Context, route transport.EgressRouteID, request *http.Request) (*http.Response, error) {
	return send(ctx, route, request)
}

type unusedProvider struct{ t *testing.T }

func (provider unusedProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.t.Error("resolved credentials after registration timed out")
	return credential.Material{}, errors.New("unexpected credential resolution")
}

func deadlineClient(t *testing.T, timeout time.Duration, hook processHook, sender requestSender) *binance.Client {
	t.Helper()
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", ContextTimeoutEnabled: true, MaxRetries: -1})
	redisClient.AddHook(hook)
	t.Cleanup(func() { _ = redisClient.Close() })
	backend, err := New(Config{Client: redisClient, Namespace: t.Name(), OperationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := ratelimit.NewWithBackend(backend)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := exchange.NewExecutor(exchange.ExecutorConfig{Sender: sender, Limiter: limiter})
	if err != nil {
		t.Fatal(err)
	}
	client, err := binance.New(binance.Config{
		Executor: executor, DefaultEgressRouteID: "route-a", RequestTimeout: timeout,
		Credentials: &credential.Descriptor{
			Exchange: model.ExchangeBinance, AccountID: "account-a", SecretRef: "unused",
			Permissions:           []credential.Permission{credential.PermissionRead, credential.PermissionTrade},
			AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
		},
		CredentialProvider: unusedProvider{t},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestNativeRequestDeadlineIncludesRedisRegistration(t *testing.T) {
	for _, operation := range []string{"public", "mutation"} {
		for _, budget := range []string{"default", "option", "parent", "cancel"} {
			t.Run(operation+"/"+budget, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				requestTimeout := time.Second
				var options []trade.RequestOption
				wantErr := context.DeadlineExceeded
				switch budget {
				case "default":
					requestTimeout = 50 * time.Millisecond
				case "option":
					options = []trade.RequestOption{trade.WithTimeout(50 * time.Millisecond)}
				case "parent":
					var cancelDeadline context.CancelFunc
					ctx, cancelDeadline = context.WithTimeout(ctx, 50*time.Millisecond)
					defer cancelDeadline()
				case "cancel":
					wantErr = context.Canceled
				}
				registrations, submissions := 0, 0
				client := deadlineClient(t, requestTimeout, func(redisCtx context.Context, cmd redis.Cmder) error {
					registrations++
					if cmd.Name() != "evalsha" || cmd.Args()[5] != "set" {
						t.Fatalf("expected rule registration, got %v", cmd.Args())
					}
					if budget == "cancel" {
						cancel()
					}
					<-redisCtx.Done()
					return redisCtx.Err()
				}, func(context.Context, transport.EgressRouteID, *http.Request) (*http.Response, error) {
					submissions++
					return nil, errors.New("unexpected HTTP request")
				})
				started := time.Now()
				var err error
				if operation == "public" {
					_, err = client.OrderBook(ctx, binance.OrderBookRequest{Symbol: "BTCUSDT"}, options...)
				} else {
					_, err = client.NewOrder(ctx, binance.NewOrderRequest{
						Symbol: "BTCUSDT", Side: binance.SideBuy, Type: binance.OrderTypeLimit,
						TimeInForce: binance.TimeInForceGTC, Quantity: "0.1", Price: "100", ClientOrderID: "deadline-order",
					}, options...)
				}
				if !errors.Is(err, wantErr) {
					t.Fatalf("error=%v, want %v", err, wantErr)
				}
				if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
					t.Fatalf("registration exceeded request budget: %v", elapsed)
				}
				if registrations != 1 || submissions != 0 {
					t.Fatalf("registrations=%d submissions=%d", registrations, submissions)
				}
			})
		}
	}
}

func TestRedisRegistrationAndHTTPShareDeadline(t *testing.T) {
	var registrationDeadline time.Time
	var submissions int
	client := deadlineClient(t, time.Second, func(ctx context.Context, cmd redis.Cmder) error {
		if registrationDeadline.IsZero() {
			registrationDeadline, _ = ctx.Deadline()
			// Consume part of the budget during registration.
			timer := time.NewTimer(20 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
		cmd.(*redis.Cmd).SetVal([]any{int64(0)})
		return nil
	}, func(ctx context.Context, _ transport.EgressRouteID, _ *http.Request) (*http.Response, error) {
		submissions++
		deadline, ok := ctx.Deadline()
		if !ok || !deadline.Equal(registrationDeadline) {
			t.Errorf("HTTP deadline=%v, registration deadline=%v", deadline, registrationDeadline)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	_, err := client.OrderBook(context.Background(), binance.OrderBookRequest{Symbol: "BTCUSDT"}, trade.WithTimeout(100*time.Millisecond))
	if !errors.Is(err, context.DeadlineExceeded) || submissions != 1 {
		t.Fatalf("error=%v submissions=%d", err, submissions)
	}
}
