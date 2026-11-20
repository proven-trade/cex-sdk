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

func TestAssetsDecodesAndSendsQuery(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		publicPrefix + "Assets": `{"error":[],"result":{"ZUSD":{"aclass":"currency","altname":"USD","decimals":4,"display_decimals":2,"collateral_value":1,"status":"enabled"},"XXBT":{"aclass":"currency","altname":"XBT","decimals":10,"display_decimals":5,"collateral_value":1,"status":"enabled","margin_rate":0.0002}}}`,
	})
	client, _ := newExtClient(t, server.URL)
	assets, err := client.Assets(context.Background(), AssetsRequest{
		Assets: []string{"XBT", "USD"}, AssetClass: AssetClassCurrency, DisplayNames: true,
	})
	if err != nil {
		t.Fatalf("Assets() error = %v", err)
	}
	if len(assets) != 2 || assets[0].ID != "XXBT" || assets[0].AltName != "XBT" ||
		assets[0].Decimals != 10 || assets[0].MarginRate != 0.0002 || assets[1].ID != "ZUSD" ||
		assets[1].Status != "enabled" || len(assets[1].Raw) == 0 {
		t.Fatalf("Assets() = %+v", assets)
	}
	request := server.last(t)
	if request.Method != http.MethodGet || request.Path != publicPrefix+"Assets" ||
		request.Query.Get("asset") != "XBT,USD" || request.Query.Get("aclass") != "currency" ||
		request.Query.Get("assetVersion") != "1" {
		t.Fatalf("Assets request = %+v", request)
	}
}

func TestSystemStatusAndMaintenanceScheduleDecode(t *testing.T) {
	t.Parallel()

	event := `{"event_id":21,"title":"Scheduled Maintenance - Website","expected_start_utc":"2026-05-11T09:00:00Z","expected_end_utc":"2026-05-11T10:00:00Z","time_to_start_s":1740,"phase":"approaching_30m","affected_services":["spot_trading"],"order_submission":"allowed","recommended_action":"reduce_activity","cancel_before_utc":"2026-05-11T08:55:00Z","source_url":"https://status.kraken.com/incidents/b7k2r9wqmn41"}`
	emergency := `{"event_id":4821,"title":"Elevated API error rates","incident_status":"identified","impact":"critical","affected_services":["spot_ws","spot_rest"],"started_at_utc":"2026-05-08T14:23:11Z","next_steps":[{"applies_to":["spot_trading"],"type":"expected_restart","expected_at_utc":"2026-05-08T14:30:00Z"}],"source_url":"https://stspg.io/c378t4f7rh0n"}`
	server := newExtServer(t, map[string]string{
		publicPrefix + "SystemStatus":        `{"error":[],"result":{"status":"cancel_only","timestamp":"2026-05-11T08:30:00Z","upcoming_maintenance":[` + event + `],"emergency":[` + emergency + `]}}`,
		publicPrefix + "MaintenanceSchedule": `{"error":[],"result":{"events":[` + event + `]}}`,
	})
	client, _ := newExtClient(t, server.URL)

	status, err := client.SystemStatus(context.Background())
	if err != nil {
		t.Fatalf("SystemStatus() error = %v", err)
	}
	if status.Status != "cancel_only" || !status.Timestamp.Equal(time.Date(2026, 5, 11, 8, 30, 0, 0, time.UTC)) ||
		len(status.UpcomingMaintenance) != 1 || status.UpcomingMaintenance[0].Phase != "approaching_30m" ||
		status.UpcomingMaintenance[0].TimeToStartSeconds != 1740 ||
		len(status.Emergency) != 1 || status.Emergency[0].Impact != "critical" ||
		len(status.Emergency[0].NextSteps) != 1 || status.Emergency[0].NextSteps[0].Type != "expected_restart" ||
		len(status.Raw) == 0 {
		t.Fatalf("SystemStatus() = %+v", status)
	}
	if request := server.last(t); request.Method != http.MethodGet || request.Path != publicPrefix+"SystemStatus" {
		t.Fatalf("SystemStatus request = %+v", request)
	}

	schedule, err := client.MaintenanceSchedule(context.Background())
	if err != nil {
		t.Fatalf("MaintenanceSchedule() error = %v", err)
	}
	if len(schedule.Events) != 1 || schedule.Events[0].EventID != 21 ||
		schedule.Events[0].OrderSubmission != "allowed" || schedule.Events[0].AffectedServices[0] != "spot_trading" ||
		len(schedule.Raw) == 0 {
		t.Fatalf("MaintenanceSchedule() = %+v", schedule)
	}
}

func TestSystemStatusRejectsEmptyStatus(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		publicPrefix + "SystemStatus": `{"error":[],"result":{"timestamp":"2026-05-11T08:30:00Z"}}`,
	})
	client, _ := newExtClient(t, server.URL)
	if _, err := client.SystemStatus(context.Background()); err == nil {
		t.Fatal("SystemStatus() error = nil, want empty status error")
	}
}

func TestSpreadsDecodesCursorAndSendsQuery(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		publicPrefix + "Spread": `{"error":[],"result":{"XXBTZUSD":[[1688671834,"30292.10000","30297.50000"],[1688671834,"30292.70000","30296.70000"]],"last":1688672106}}`,
	})
	client, _ := newExtClient(t, server.URL)
	spreads, err := client.Spreads(context.Background(), SpreadsRequest{Pair: "XBTUSD", Since: 1688671000})
	if err != nil {
		t.Fatalf("Spreads() error = %v", err)
	}
	if spreads.PairID != "XXBTZUSD" || len(spreads.Entries) != 2 || spreads.Last != 1688672106 ||
		spreads.Entries[0].Time != 1688671834 || spreads.Entries[0].Bid != "30292.10000" ||
		spreads.Entries[1].Ask != "30296.70000" || len(spreads.Raw) == 0 {
		t.Fatalf("Spreads() = %+v", spreads)
	}
	request := server.last(t)
	if request.Method != http.MethodGet || request.Path != publicPrefix+"Spread" ||
		request.Query.Get("pair") != "XBTUSD" || request.Query.Get("since") != "1688671000" {
		t.Fatalf("Spreads request = %+v", request)
	}
}

func TestSpreadsRejectsMissingCursor(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		publicPrefix + "Spread": `{"error":[],"result":{"XXBTZUSD":[[1688671834,"1","2"]]}}`,
	})
	client, _ := newExtClient(t, server.URL)
	if _, err := client.Spreads(context.Background(), SpreadsRequest{Pair: "XBTUSD"}); err == nil {
		t.Fatal("Spreads() error = nil, want missing cursor error")
	}
}

func TestGroupedBookDecodesAndSendsQuery(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		publicPrefix + "GroupedBook": `{"error":[],"result":{"pair":"BTC/USD","grouping":1000,"bids":[{"price":"90400.00000","qty":"19.83057746"},{"price":"90300.00000","qty":"45.35073006"}],"asks":[{"price":"90500.00000","qty":"38.96185061"}]}}`,
	})
	client, _ := newExtClient(t, server.URL)
	book, err := client.GroupedBook(context.Background(), GroupedBookRequest{
		Pair: "BTC/USD", Depth: 10, Grouping: 1000,
	})
	if err != nil {
		t.Fatalf("GroupedBook() error = %v", err)
	}
	if book.Pair != "BTC/USD" || book.Grouping != 1000 || len(book.Bids) != 2 || len(book.Asks) != 1 ||
		book.Bids[0].Price != "90400.00000" || book.Asks[0].Quantity != "38.96185061" || len(book.Raw) == 0 {
		t.Fatalf("GroupedBook() = %+v", book)
	}
	request := server.last(t)
	if request.Method != http.MethodGet || request.Path != publicPrefix+"GroupedBook" ||
		request.Query.Get("pair") != "BTC/USD" || request.Query.Get("depth") != "10" ||
		request.Query.Get("grouping") != "1000" {
		t.Fatalf("GroupedBook request = %+v", request)
	}
}

func TestLevel3SendsSignedJSONBody(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "Level3": `{"error":[],"result":{"pair":"YFI/EUR","bids":[{"price":"3062.00000","qty":"0.29665800","order_id":"O5KJU4-IEQTM-NDMS6W","timestamp":1765622008594292000}],"asks":[{"price":"3066.00000","qty":"0.00278335","order_id":"ORAWGV-N5L4J-LBA3WH","timestamp":1765622008499456000}]}}`,
	})
	client, _ := newExtClient(t, server.URL, credential.PermissionRead)
	book, err := client.Level3(context.Background(), Level3Request{Pair: "YFI/EUR", Depth: 10})
	if err != nil {
		t.Fatalf("Level3() error = %v", err)
	}
	if book.Pair != "YFI/EUR" || len(book.Bids) != 1 || len(book.Asks) != 1 ||
		book.Bids[0].OrderID != "O5KJU4-IEQTM-NDMS6W" || book.Bids[0].Timestamp != 1765622008594292000 ||
		book.Asks[0].Quantity != "0.00278335" || len(book.Raw) == 0 {
		t.Fatalf("Level3() = %+v", book)
	}
	request := server.last(t)
	if request.Method != http.MethodPost || request.Path != privatePrefix+"Level3" ||
		request.ContentType != jsonContentType {
		t.Fatalf("Level3 request = %+v", request)
	}
	body := request.jsonBody(t)
	if body["pair"] != "YFI/EUR" || fmt.Sprint(body["depth"]) != "10" || body["nonce"] == nil {
		t.Fatalf("Level3 body = %v", body)
	}
}

func TestLevel3FullBookSendsZeroDepth(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "Level3": `{"error":[],"result":{"pair":"BTC/USD","bids":[],"asks":[]}}`,
	})
	client, _ := newExtClient(t, server.URL, credential.PermissionRead)
	if _, err := client.Level3(context.Background(), Level3Request{Pair: "BTC/USD", FullBook: true}); err != nil {
		t.Fatalf("Level3() error = %v", err)
	}
	body := server.last(t).jsonBody(t)
	depth, exists := body["depth"]
	if !exists || fmt.Sprint(depth) != "0" {
		t.Fatalf("Level3 full book body = %v", body)
	}
}

func TestPreTradeAndPostTradeDecode(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		publicPrefix + "PreTrade":  `{"error":[],"result":{"symbol":"BTC/USD","description":"Bitcoin / US Dollars","base_asset":"BTC","quote_asset":"USD","base_notation":"UNIT","quote_notation":"MONE","venue":"PGSL","system":"CLOB","bids":[{"side":"BUY","price":"102002.1","qty":"1.5","count":3,"submission_ts":"2024-05-30T12:34:56.123456Z","publication_ts":"2024-05-30T12:34:56.223456Z"}],"asks":[{"side":"SELL","price":"102003.1","qty":"2","count":1,"submission_ts":"2024-05-30T12:34:57.123456Z","publication_ts":"2024-05-30T12:34:57.223456Z"}]}}`,
		publicPrefix + "PostTrade": `{"error":[],"result":{"last_ts":"2024-05-30T12:34:56.123456789Z","count":1,"trades":[{"trade_id":"TGBB7L-HT5LX-J3BZ4A","price":"102002.1","quantity":"1.24","symbol":"BTC/USD","description":"Bitcoin / US Dollars","base_asset":"BTC","base_notation":"UNIT","quote_asset":"USD","quote_notation":"MONE","trade_venue":"PGSL","trade_ts":"2024-05-30T12:34:56.123456789Z","publication_venue":"PGSL","publication_ts":"2024-05-30T12:34:56.123456789Z"}]}}`,
	})
	client, _ := newExtClient(t, server.URL)

	preTrade, err := client.PreTrade(context.Background(), PreTradeRequest{Symbol: "BTC/USD"})
	if err != nil {
		t.Fatalf("PreTrade() error = %v", err)
	}
	if preTrade.Symbol != "BTC/USD" || preTrade.Venue != "PGSL" || preTrade.System != "CLOB" ||
		len(preTrade.Bids) != 1 || preTrade.Bids[0].Side != "BUY" || preTrade.Bids[0].Count != 3 ||
		preTrade.Asks[0].Quantity != "2" || len(preTrade.Raw) == 0 {
		t.Fatalf("PreTrade() = %+v", preTrade)
	}
	if request := server.last(t); request.Method != http.MethodGet || request.Path != publicPrefix+"PreTrade" ||
		request.Query.Get("symbol") != "BTC/USD" {
		t.Fatalf("PreTrade request = %+v", request)
	}

	from := time.Date(2024, 5, 30, 12, 0, 0, 123456789, time.UTC)
	to := time.Date(2024, 5, 30, 13, 0, 0, 0, time.UTC)
	postTrade, err := client.PostTrade(context.Background(), PostTradeRequest{
		Symbol: "BTC/USD", From: &from, To: &to, Count: 100,
	})
	if err != nil {
		t.Fatalf("PostTrade() error = %v", err)
	}
	if postTrade.Count != 1 || postTrade.LastTime != "2024-05-30T12:34:56.123456789Z" ||
		len(postTrade.Trades) != 1 || postTrade.Trades[0].TradeID != "TGBB7L-HT5LX-J3BZ4A" ||
		postTrade.Trades[0].Quantity != "1.24" || postTrade.Trades[0].TradeVenue != "PGSL" ||
		len(postTrade.Raw) == 0 {
		t.Fatalf("PostTrade() = %+v", postTrade)
	}
	request := server.last(t)
	if request.Method != http.MethodGet || request.Path != publicPrefix+"PostTrade" ||
		request.Query.Get("symbol") != "BTC/USD" || request.Query.Get("count") != "100" ||
		request.Query.Get("from_ts") != "2024-05-30T12:00:00.123456789Z" ||
		request.Query.Get("to_ts") != "2024-05-30T13:00:00Z" {
		t.Fatalf("PostTrade request = %+v", request)
	}
}

func TestMarketExtensionRequestValidation(t *testing.T) {
	t.Parallel()

	later := time.Date(2024, 5, 30, 13, 0, 0, 0, time.UTC)
	earlier := later.Add(-time.Hour)
	invalid := []error{
		(AssetsRequest{Assets: []string{"bad asset"}}).validate(),
		(AssetsRequest{Assets: []string{"XBT", "XBT"}}).validate(),
		(AssetsRequest{AssetClass: "equity"}).validate(),
		(SpreadsRequest{}).validate(),
		(SpreadsRequest{Pair: "XBTUSD", Since: -1}).validate(),
		(SpreadsRequest{Pair: "XBTUSD", AssetClass: AssetClassCurrency}).validate(),
		(GroupedBookRequest{Pair: "BTC/USD", Depth: 7}).validate(),
		(GroupedBookRequest{Pair: "BTC/USD", Grouping: 3}).validate(),
		(Level3Request{}).validate(),
		(Level3Request{Pair: "BTC/USD", Depth: 10, FullBook: true}).validate(),
		(Level3Request{Pair: "BTC/USD", Depth: 11}).validate(),
		(PreTradeRequest{Symbol: "BT"}).validate(),
		(PostTradeRequest{Count: 1001}).validate(),
		(PostTradeRequest{From: &later, To: &earlier}).validate(),
	}
	for index, err := range invalid {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v, want validation", index, err)
		}
	}
}

func TestLevel3RequiresReadPermission(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{})
	client, _ := newExtClient(t, server.URL, credential.PermissionTrade)
	_, err := client.Level3(context.Background(), Level3Request{Pair: "BTC/USD"})
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Level3() error = %v, want authorization", err)
	}
	if server.count() != 0 {
		t.Fatalf("server requests = %d, want 0", server.count())
	}
}
