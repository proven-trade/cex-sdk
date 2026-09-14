package bithumb

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	commonexchange "github.com/proven-trade/cex-sdk/v2/exchange"
	"github.com/proven-trade/cex-sdk/v2/ratelimit"
	"github.com/proven-trade/cex-sdk/v2/unified"
)

func TestUnifiedBookSkipsEmptyLevelsBeforeApplyingDepth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"market":"KRW-BTC","orderbook_units":[{"bid_price":99,"bid_size":0,"ask_price":101,"ask_size":1},{"bid_price":98,"bid_size":1,"ask_price":102,"ask_size":"0.0000"},{"bid_price":97,"bid_size":2,"ask_price":103,"ask_size":2}]}]`)
	}))
	defer server.Close()
	limiter, _ := ratelimit.New()
	executor, _ := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: &directSender{}, Limiter: limiter})
	client, err := New(Config{Executor: executor, DefaultEgressRouteID: "a", BaseURL: server.URL, AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	spot, _ := NewUnifiedSpot(client)
	book, err := spot.OrderBook(context.Background(), unified.OrderBookRequest{Market: unified.Market{Base: "BTC", Quote: "KRW"}, Limit: 2})
	if err != nil || len(book.Bids) != 2 || len(book.Asks) != 2 || book.Bids[0].Price != "98" || book.Asks[1].Price != "103" {
		t.Fatalf("book=%+v %v", book, err)
	}
}
