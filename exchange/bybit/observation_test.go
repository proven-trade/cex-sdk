package bybit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	trade "github.com/proven-trade/cex-sdk/v2"
	commonexchange "github.com/proven-trade/cex-sdk/v2/exchange"
	"github.com/proven-trade/cex-sdk/v2/ratelimit"
)

func TestNativeHTTP200ErrorReachesObserver(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"retCode":10006,"retMsg":"too many visits","result":{}}`)
	}))
	defer server.Close()
	limiter, _ := ratelimit.New()
	var observations []commonexchange.ExecutionObservation
	executor, _ := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: &directSender{}, Limiter: limiter, Observer: commonexchange.ExecutionObserverFunc(func(o commonexchange.ExecutionObservation) { observations = append(observations, o) })})
	client, err := New(Config{Executor: executor, DefaultEgressRouteID: "route-a", BaseURL: server.URL, AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Instruments(context.Background(), InstrumentsRequest{Category: CategorySpot, Symbol: "BTCUSDT"})
	if !errors.Is(err, trade.ErrRateLimited) {
		t.Fatalf("error=%v", err)
	}
	if len(observations) != 1 || observations[0].StatusCode != 200 || observations[0].ErrorCategory != trade.ErrorRateLimited || observations[0].EgressRouteID != "route-a" {
		t.Fatalf("observations=%+v", observations)
	}
}
