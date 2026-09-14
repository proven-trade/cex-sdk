package cryptocom

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
		_, _ = io.WriteString(w, `{"method":"public/get-instruments","code":0,"result":{"data":[{"symbol":"SHIB_BTC@OEX_HK","inst_type":"CCY_PAIR","base_ccy":"SHIB","quote_ccy":"BTC"},{"symbol":"BTC_USDT","inst_type":"CCY_PAIR","base_ccy":"BTC","quote_ccy":"USDT","tradable":true}]}}`)
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
