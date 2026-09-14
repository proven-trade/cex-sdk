package gateio

import (
	"context"
	commonexchange "github.com/proven-trade/cex-sdk/v2/exchange"
	"github.com/proven-trade/cex-sdk/v2/ratelimit"
	"github.com/proven-trade/cex-sdk/v2/unified"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCatalogueKeepsSupportedMarketsAlongsideOtherInstruments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"老子_USDT","base":"老子","quote":"USDT"},{"id":"BTC_USDT","base":"BTC","quote":"USDT","trade_status":"tradable","precision":2,"amount_precision":6}]`)
	}))
	defer server.Close()
	limiter, _ := ratelimit.New()
	executor, _ := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: &directSender{}, Limiter: limiter})
	client, err := New(Config{Executor: executor, DefaultEgressRouteID: "route-a", BaseURL: server.URL, AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	spot, _ := NewUnifiedSpot(client)
	markets, err := spot.Markets(context.Background())
	if err != nil || len(markets) != 1 || markets[0].Market != (unified.Market{Base: "BTC", Quote: "USDT"}) {
		t.Fatalf("markets=%+v err=%v", markets, err)
	}
}
