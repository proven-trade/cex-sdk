package korbit

import (
	"context"
	"net/http"
	"testing"

	"github.com/proven-trade/cex-sdk/credential"
)

const preventedMatchesSample = `{"success":true,"data":{"preventedMatches":[` +
	`{"symbol":"btc_krw","stp":"cancelTaker","price":"99000000","takerOrderId":382312,"makerOrderId":382290,"takerAccountSeq":1,"makerAccountSeq":2,"takerClosed":true,"makerClosed":false,"takerPreventedQty":"0.3","makerPreventedQty":"0","takerPreventedAmt":"29730000","makerPreventedAmt":"0","preventedAt":1700000003000},` +
	`{"symbol":"btc_krw","stp":"cancelMaker","price":"98900000","takerOrderId":382265,"makerOrderId":382240,"takerAccountSeq":2,"makerAccountSeq":1,"takerClosed":false,"makerClosed":true,"takerPreventedQty":"0","makerPreventedQty":"0.05","makerPreventedAmt":"4945000","preventedAt":1700000001000}` +
	`],"nextCursor":"OuIpO-mMkKHzCXM8dntSAfIZF0Ekpg5RVdn762kniDYRZrSOUx"}}`

func TestMyPreventedMatchesSendsSignedQueryAndDecodesPage(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{"GET /v2/myPreventedMatches": preventedMatchesSample})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)

	page, err := client.MyPreventedMatches(context.Background(), PreventedMatchesRequest{
		Symbol: "btc_krw", AccountSeq: 1, OrderID: 382312, Cursor: "cursor-1", Limit: 2,
	})
	if err != nil {
		t.Fatalf("MyPreventedMatches() error = %v", err)
	}
	entries := recorder.snapshot()
	wantParams := "accountSeq=1&cursor=cursor-1&limit=2&orderId=382312&recvWindow=5000&symbol=btc_krw&timestamp=1700000000123"
	if len(entries) != 1 || entries[0].Method != http.MethodGet || entries[0].Path != "/v2/myPreventedMatches" ||
		!entries[0].Signed || entries[0].Params != wantParams {
		t.Fatalf("requests = %+v, want one signed GET with %q", entries, wantParams)
	}
	if page.NextCursor != "OuIpO-mMkKHzCXM8dntSAfIZF0Ekpg5RVdn762kniDYRZrSOUx" || len(page.Raw) == 0 ||
		len(page.PreventedMatches) != 2 {
		t.Fatalf("page = %+v", page)
	}
	first, second := page.PreventedMatches[0], page.PreventedMatches[1]
	if first.Symbol != "btc_krw" || first.STP != STPModeCancelTaker || first.Price != "99000000" ||
		first.TakerOrderID != 382312 || first.MakerOrderID != 382290 ||
		first.TakerAccountSeq != 1 || first.MakerAccountSeq != 2 || !first.TakerClosed || first.MakerClosed ||
		first.TakerPreventedQty != "0.3" || first.MakerPreventedQty != "0" ||
		first.TakerPreventedAmt != "29730000" || first.MakerPreventedAmt != "0" ||
		first.PreventedAt != 1700000003000 || len(first.Raw) == 0 {
		t.Fatalf("first prevented match = %+v", first)
	}
	if second.STP != STPModeCancelMaker || second.TakerPreventedAmt != "" || second.MakerPreventedQty != "0.05" ||
		second.MakerPreventedAmt != "4945000" || second.TakerClosed || !second.MakerClosed {
		t.Fatalf("second prevented match = %+v", second)
	}
}

func TestMyPreventedMatchesLastPageHasNoCursor(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{
		"GET /v2/myPreventedMatches": `{"success":true,"data":{"preventedMatches":[]}}`,
	})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)
	page, err := client.MyPreventedMatches(context.Background(), PreventedMatchesRequest{Symbol: "btc_krw", AccountSeq: 3})
	if err != nil || page.PreventedMatches == nil || len(page.PreventedMatches) != 0 || page.NextCursor != "" {
		t.Fatalf("MyPreventedMatches() = %#v, error = %v, want an empty page without cursor", page, err)
	}
	entries := recorder.snapshot()
	if len(entries) != 1 || entries[0].Params != "accountSeq=3&recvWindow=5000&symbol=btc_krw&timestamp=1700000000123" {
		t.Fatalf("requests = %+v, want optional parameters omitted", entries)
	}
}

func TestMyPreventedMatchesRejectsInvalidRequestBeforeSending(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{"GET /v2/myPreventedMatches": preventedMatchesSample})
	client := newAssetTestClient(t, server.URL, credential.PermissionRead)
	invalid := map[string]PreventedMatchesRequest{
		"accountSeq omitted":       {Symbol: "btc_krw"},
		"accountSeq too large":     {Symbol: "btc_krw", AccountSeq: 10000},
		"symbol without separator": {Symbol: "btckrw", AccountSeq: 1},
		"negative order ID":        {Symbol: "btc_krw", AccountSeq: 1, OrderID: -1},
		"limit above maximum":      {Symbol: "btc_krw", AccountSeq: 1, Limit: 1001},
		"negative limit":           {Symbol: "btc_krw", AccountSeq: 1, Limit: -1},
	}
	calls := make([]namedCall, 0, len(invalid))
	for name, request := range invalid {
		calls = append(calls, namedCall{name, func() error {
			_, err := client.MyPreventedMatches(context.Background(), request)
			return err
		}})
	}
	requireRejectedBeforeSend(t, recorder, calls)
	if err := (PreventedMatchesRequest{Symbol: "btc_krw", AccountSeq: 1, Limit: 1000}).validate(); err != nil {
		t.Fatalf("maximum limit validation error = %v", err)
	}
}
