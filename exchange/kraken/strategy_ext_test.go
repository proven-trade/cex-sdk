package kraken

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

const (
	testStrategyID      = "ASMBA64-R23M2-J7JRJV"
	testOtherStrategyID = "ASPBVCI-6MKBA-QE4VF6"
)

func TestAddStrategySendsJSONAndDecodesID(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "AddStrategy": `{"error":[],"result":{"strategy_id":"` + testStrategyID + `"}}`,
	})
	client, limiter := newExtClient(t, server.URL, credential.PermissionTrade)
	reference, err := client.AddStrategy(context.Background(), AddStrategyRequest{
		Pair: "BTC/USD", Side: SideBuy, Volume: "1.5", OrderType: OrderTypeLimit, Price: "67000.0",
		ExpireTime: "+3600",
	})
	if err != nil {
		t.Fatalf("AddStrategy() error = %v", err)
	}
	if reference.ID != testStrategyID || len(reference.Raw) == 0 {
		t.Fatalf("AddStrategy() = %+v", reference)
	}
	request := server.last(t)
	body := request.jsonBody(t)
	if request.Method != http.MethodPost || request.Path != privatePrefix+"AddStrategy" ||
		request.ContentType != jsonContentType || body["strategy_type"] != "twap" ||
		body["symbol"] != "BTC/USD" || body["asset_class"] != "forex" || body["side"] != "buy" ||
		body["volume"] != "1.5" || body["ordertype"] != "limit" || body["price"] != "67000.0" ||
		body["expiretm"] != "+3600" || fmt.Sprint(body["nonce"]) == "" || fmt.Sprint(body["nonce"]) == "<nil>" {
		t.Fatalf("AddStrategy request = %+v body = %v", request, body)
	}
	for _, key := range []string{"validate", "starttm"} {
		if _, exists := body[key]; exists {
			t.Fatalf("AddStrategy body includes %s: %v", key, body)
		}
	}
	snapshot, err := limiter.Snapshot("kraken:account:kraken-main:private-counter")
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("private limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestAddStrategyValidateOnlyAllowsEmptyID(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "AddStrategy": `{"error":[],"result":{"strategy_id":""}}`,
	})
	client, _ := newExtClient(t, server.URL)
	request := AddStrategyRequest{
		StrategyType: StrategyTypeTWAP, Pair: "ETH/USD", Side: SideSell, Volume: "10",
		OrderType: OrderTypeMarket, StartTime: "+300", ExpireTime: "+7500", ValidateOnly: true,
	}
	reference, err := client.AddStrategy(context.Background(), request)
	if err != nil {
		t.Fatalf("AddStrategy(validate) error = %v", err)
	}
	if reference.ID != "" || len(reference.Raw) == 0 {
		t.Fatalf("AddStrategy(validate) = %+v", reference)
	}
	body := server.last(t).jsonBody(t)
	if body["validate"] != true || body["starttm"] != "+300" || body["ordertype"] != "market" {
		t.Fatalf("AddStrategy(validate) body = %v", body)
	}
	if _, exists := body["price"]; exists {
		t.Fatalf("market strategy body includes price: %v", body)
	}

	request.ValidateOnly = false
	if _, err := client.AddStrategy(context.Background(), request); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("AddStrategy(empty ID) error = %v, want unknown execution state", err)
	}
}

func TestCancelStrategySendsJSONAndDecodesID(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "CancelStrategy": `{"error":[],"result":{"strategy_id":"` + testStrategyID + `"}}`,
	})
	client, _ := newExtClient(t, server.URL)
	reference, err := client.CancelStrategy(context.Background(), CancelStrategyRequest{StrategyID: testStrategyID})
	if err != nil {
		t.Fatalf("CancelStrategy() error = %v", err)
	}
	if reference.ID != testStrategyID {
		t.Fatalf("CancelStrategy() = %+v", reference)
	}
	request := server.last(t)
	if request.Path != privatePrefix+"CancelStrategy" || request.ContentType != jsonContentType ||
		request.jsonBody(t)["strategy_id"] != testStrategyID {
		t.Fatalf("CancelStrategy request = %+v", request)
	}

	emptyServer := newExtServer(t, map[string]string{
		privatePrefix + "CancelStrategy": `{"error":[],"result":{}}`,
	})
	emptyClient, _ := newExtClient(t, emptyServer.URL)
	_, err = emptyClient.CancelStrategy(context.Background(), CancelStrategyRequest{StrategyID: testStrategyID})
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CancelStrategy(empty) error = %v, want unknown execution state", err)
	}
}

func TestOpenAndQueryStrategiesDecodeMapsInIDOrder(t *testing.T) {
	t.Parallel()

	twap := func(id, status string) string {
		return `"` + id + `":{"strategy_id":"` + id + `","status":"` + status + `","details":{"strategy_type":"twap","side":"buy","volume":"0.01000000","filled_volume":"0","avg_price":"","total_cost":"0","total_fee":"0","starttm":"1781169958","expiretm":"1781173558","symbol":"BTC/USD","asset_class":"forex","ordertype":"market"}}`
	}
	server := newExtServer(t, map[string]string{
		privatePrefix + "GetOpenStrategies": `{"error":[],"result":{"open":{` + twap(testStrategyID, "open") + `,` + twap(testOtherStrategyID, "pending") + `}}}`,
		privatePrefix + "QueryStrategies":   `{"error":[],"result":{"strategies":{` + twap(testStrategyID, "open") + `}}}`,
	})
	client, _ := newExtClient(t, server.URL)

	open, err := client.OpenStrategies(context.Background(), OpenStrategiesRequest{StrategyType: StrategyTypeTWAP})
	if err != nil {
		t.Fatalf("OpenStrategies() error = %v", err)
	}
	if len(open) != 2 || open[0].ID != testStrategyID || open[1].ID != testOtherStrategyID ||
		open[0].Status != StrategyOpen || open[1].Status != StrategyPending ||
		open[0].Details.Pair != "BTC/USD" || open[0].Details.Side != SideBuy ||
		open[0].Details.OrderType != OrderTypeMarket || open[0].Details.AssetClass != StrategyAssetClassForex ||
		open[0].Details.StartTime != "1781169958" || len(open[0].Raw) == 0 {
		t.Fatalf("OpenStrategies() = %+v", open)
	}
	request := server.last(t)
	if request.Path != privatePrefix+"GetOpenStrategies" || request.ContentType != jsonContentType ||
		request.jsonBody(t)["strategy_type"] != "twap" {
		t.Fatalf("OpenStrategies request = %+v", request)
	}

	queried, err := client.QueryStrategies(context.Background(), QueryStrategiesRequest{
		StrategyIDs: []string{testStrategyID, testOtherStrategyID},
	})
	if err != nil {
		t.Fatalf("QueryStrategies() error = %v", err)
	}
	if len(queried) != 1 || queried[0].ID != testStrategyID {
		t.Fatalf("QueryStrategies() = %+v", queried)
	}
	if body := server.last(t).jsonBody(t); body["strategy_ids"] != testStrategyID+","+testOtherStrategyID {
		t.Fatalf("QueryStrategies body = %v", body)
	}
}

func TestOpenStrategiesAcceptsEmptyMap(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "GetOpenStrategies": `{"error":[],"result":{"open":{}}}`,
	})
	client, _ := newExtClient(t, server.URL)
	open, err := client.OpenStrategies(context.Background(), OpenStrategiesRequest{})
	if err != nil {
		t.Fatalf("OpenStrategies() error = %v", err)
	}
	if open == nil || len(open) != 0 {
		t.Fatalf("OpenStrategies() = %#v, want empty non-nil slice", open)
	}
	if body := server.last(t).jsonBody(t); body["strategy_type"] != nil {
		t.Fatalf("OpenStrategies body = %v", body)
	}
}

func TestClosedStrategiesDecodesPageAndSendsFilters(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "GetClosedStrategies": `{"error":[],"result":{"closed":[{"strategy_id":"` + testOtherStrategyID + `","status":"cancelled","reason":"userRequested","details":{"strategy_type":"twap","side":"buy","volume":"1.50000000","filled_volume":"0.80000000","avg_price":"67098.42","total_cost":"53678.74","total_fee":"85.89","starttm":"1781169958","expiretm":"1781173558","symbol":"BTC/USD","asset_class":"forex","ordertype":"limit","price":"67000.0"}},{"strategy_id":"` + testStrategyID + `","status":"closed","details":{"strategy_type":"twap","side":"sell","volume":"2.0","filled_volume":"2.0","symbol":"BTC/USD","asset_class":"forex","ordertype":"market"}}],"next_cursor":"Q0xPU0VEOjE3ODExNzAwMDA6QVNNQkE2NA"}}`,
	})
	client, limiter := newExtClient(t, server.URL)
	start := time.Unix(1781100000, 0)
	end := time.Unix(1781200000, 0)
	page, err := client.ClosedStrategies(context.Background(), ClosedStrategiesRequest{
		AssetClass: StrategyAssetClassForex, StrategyType: StrategyTypeTWAP, Status: StrategyCancelled,
		Start: &start, End: &end, QueryTimeMode: StrategyQueryTimeClose, Limit: 50, Cursor: "abc_DEF-1=",
	})
	if err != nil {
		t.Fatalf("ClosedStrategies() error = %v", err)
	}
	if len(page.Strategies) != 2 || page.Strategies[0].ID != testOtherStrategyID ||
		page.Strategies[0].Status != StrategyCancelled || page.Strategies[0].Reason != "userRequested" ||
		page.Strategies[0].Details.FilledVolume != "0.80000000" || page.Strategies[0].Details.Price != "67000.0" ||
		page.Strategies[1].Status != StrategyClosed || page.NextCursor != "Q0xPU0VEOjE3ODExNzAwMDA6QVNNQkE2NA" ||
		len(page.Raw) == 0 || len(page.Strategies[1].Raw) == 0 {
		t.Fatalf("ClosedStrategies() = %+v", page)
	}
	request := server.last(t)
	body := request.jsonBody(t)
	if request.Path != privatePrefix+"GetClosedStrategies" || body["asset_class"] != "forex" ||
		body["strategy_type"] != "twap" || body["status"] != "cancelled" ||
		fmt.Sprint(body["start"]) != "1781100000" || fmt.Sprint(body["end"]) != "1781200000" ||
		body["query_time_mode"] != "close" || fmt.Sprint(body["limit"]) != "50" || body["cursor"] != "abc_DEF-1=" {
		t.Fatalf("ClosedStrategies request = %+v body = %v", request, body)
	}
	snapshot, err := limiter.Snapshot("kraken:account:kraken-main:private-counter")
	if err != nil || snapshot.Used != 4 {
		t.Fatalf("history limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestStrategyExtensionsRequirePermissions(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{})
	readClient, _ := newExtClient(t, server.URL, credential.PermissionRead)
	if _, err := readClient.AddStrategy(context.Background(), AddStrategyRequest{
		Pair: "BTC/USD", Side: SideBuy, Volume: "1", OrderType: OrderTypeMarket, ExpireTime: "+600",
	}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("AddStrategy() error = %v, want authorization", err)
	}
	if _, err := readClient.CancelStrategy(
		context.Background(), CancelStrategyRequest{StrategyID: testStrategyID},
	); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("CancelStrategy() error = %v, want authorization", err)
	}
	tradeClient, _ := newExtClient(t, server.URL, credential.PermissionTrade)
	if _, err := tradeClient.OpenStrategies(context.Background(), OpenStrategiesRequest{}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("OpenStrategies() error = %v, want authorization", err)
	}
	if server.count() != 0 {
		t.Fatalf("server requests = %d, want 0", server.count())
	}
}

func TestStrategyRequestValidation(t *testing.T) {
	t.Parallel()

	valid := AddStrategyRequest{
		Pair: "BTC/USD", Side: SideBuy, Volume: "1", OrderType: OrderTypeLimit, Price: "10", ExpireTime: "+600",
	}
	change := func(modify func(request *AddStrategyRequest)) error {
		request := valid
		modify(&request)
		return request.validate()
	}
	manyIDs := make([]string, maxQueryStrategyIDs+1)
	for index := range manyIDs {
		manyIDs[index] = fmt.Sprintf("ASAAAA%d-AAAAA-AAAAAA", index%10)
	}
	end := time.Unix(100, 0)
	start := time.Unix(200, 0)
	invalid := []error{
		change(func(request *AddStrategyRequest) { request.StrategyType = "vwap" }),
		change(func(request *AddStrategyRequest) { request.Pair = "" }),
		change(func(request *AddStrategyRequest) { request.Side = "hold" }),
		change(func(request *AddStrategyRequest) { request.Volume = "0" }),
		change(func(request *AddStrategyRequest) { request.Price = "" }),
		change(func(request *AddStrategyRequest) { request.OrderType = OrderTypeMarket }),
		change(func(request *AddStrategyRequest) { request.OrderType = OrderTypeStopLoss }),
		change(func(request *AddStrategyRequest) { request.StartTime = "soon" }),
		change(func(request *AddStrategyRequest) { request.ExpireTime = "" }),
		change(func(request *AddStrategyRequest) { request.ExpireTime = "-5" }),
		(CancelStrategyRequest{StrategyID: "ORDER-1"}).validate(),
		(OpenStrategiesRequest{StrategyType: "vwap"}).validate(),
		(QueryStrategiesRequest{}).validate(),
		(QueryStrategiesRequest{StrategyIDs: manyIDs}).validate(),
		(QueryStrategiesRequest{StrategyIDs: []string{testStrategyID, testStrategyID}}).validate(),
		(QueryStrategiesRequest{StrategyIDs: []string{"not-an-id"}}).validate(),
		(ClosedStrategiesRequest{AssetClass: "equity"}).validate(),
		(ClosedStrategiesRequest{Status: StrategyOpen}).validate(),
		(ClosedStrategiesRequest{QueryTimeMode: "later"}).validate(),
		(ClosedStrategiesRequest{Start: &start, End: &end}).validate(),
		(ClosedStrategiesRequest{Limit: maxClosedStrategiesPage + 1}).validate(),
		(ClosedStrategiesRequest{Limit: -1}).validate(),
		(ClosedStrategiesRequest{Cursor: "bad cursor"}).validate(),
	}
	for index, err := range invalid {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v, want validation", index, err)
		}
	}
	validResults := []error{
		valid.validate(),
		change(func(request *AddStrategyRequest) { request.OrderType = OrderTypeMarket; request.Price = "" }),
		change(func(request *AddStrategyRequest) { request.StartTime = "0" }),
		change(func(request *AddStrategyRequest) { request.ExpireTime = "1781173558" }),
		(CancelStrategyRequest{StrategyID: testStrategyID}).validate(),
		(OpenStrategiesRequest{}).validate(),
		(QueryStrategiesRequest{StrategyIDs: []string{testStrategyID, testOtherStrategyID}}).validate(),
		(ClosedStrategiesRequest{}).validate(),
	}
	for index, err := range validResults {
		if err != nil {
			t.Fatalf("valid request %d error = %v", index, err)
		}
	}
}
