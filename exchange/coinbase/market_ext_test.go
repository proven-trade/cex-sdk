package coinbase

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

const bestBidAskResponse = `{"pricebooks":[` +
	`{"product_id":"BTC-USD","bids":[{"price":"63999.5","size":"0.4"}],"asks":[{"price":"64000.5","size":"0.2"}],"time":"2023-11-14T22:13:20.123Z"},` +
	`{"product_id":"ETH-USD","bids":[{"price":"3199","size":"2"}],"asks":[{"price":"3200","size":"1"}],"time":"2023-11-14T22:13:20.456Z"}]}`

func TestBestBidAskSignsRequestAndDecodesPriceBooks(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(t, permissionsOf(credential.PermissionRead), http.StatusOK, bestBidAskResponse)
	result, err := harness.client.BestBidAsk(
		context.Background(), BestBidAskRequest{ProductIDs: []string{"BTC-USD", "ETH-USD"}},
	)
	if err != nil {
		t.Fatalf("BestBidAsk() error = %v", err)
	}
	if len(result.PriceBooks) != 2 || result.PriceBooks[0].ProductID != "BTC-USD" ||
		result.PriceBooks[0].Bids[0].Price != "63999.5" || result.PriceBooks[0].Asks[0].Size != "0.2" ||
		result.PriceBooks[1].ProductID != "ETH-USD" || result.PriceBooks[1].Time != "2023-11-14T22:13:20.456Z" ||
		len(result.Raw) == 0 {
		t.Fatalf("BestBidAsk() = %+v", result)
	}
	harness.assertSingle(
		t, http.MethodGet, publicPrefix+"/best_bid_ask",
		url.Values{"product_ids": {"BTC-USD", "ETH-USD"}}, "",
	)
}

func TestBestBidAskWithoutProductsQueriesEveryProduct(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(t, permissionsOf(credential.PermissionRead), http.StatusOK, bestBidAskResponse)
	if _, err := harness.client.BestBidAsk(context.Background(), BestBidAskRequest{}); err != nil {
		t.Fatalf("BestBidAsk() error = %v", err)
	}
	harness.assertSingle(t, http.MethodGet, publicPrefix+"/best_bid_ask", nil, "")
}

func TestBestBidAskRejectsDuplicateProductsBeforeSending(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(t, permissionsOf(credential.PermissionRead), http.StatusOK, bestBidAskResponse)
	_, err := harness.client.BestBidAsk(
		context.Background(), BestBidAskRequest{ProductIDs: []string{"BTC-USD", "BTC-USD"}},
	)
	if !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("BestBidAsk(duplicate) error = %v, want validation", err)
	}
	if count := harness.requestCount(); count != 0 {
		t.Fatalf("request count = %d, want 0", count)
	}
}

func TestBestBidAskClassifiesExchangeRejection(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusBadRequest,
		`{"error":"INVALID_ARGUMENT","message":"unknown product"}`,
	)
	_, err := harness.client.BestBidAsk(
		context.Background(), BestBidAskRequest{ProductIDs: []string{"NOPE-USD"}},
	)
	if !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("BestBidAsk(400) error = %v, want validation", err)
	}
}
