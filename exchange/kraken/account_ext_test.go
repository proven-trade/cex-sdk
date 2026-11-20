package kraken

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

func TestTradeBalanceDecodesNumbersAndStrings(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "TradeBalance": `{"error":[],"result":{"eb":"1101.3425","tb":"392.2264","m":"7.0505","n":"-10.0232","c":"21.1063","v":"15.1438","e":"381.8232","mf":"375.1759","ml":5415.28}}`,
	})
	client, limiter := newExtClient(t, server.URL)
	balance, err := client.TradeBalance(context.Background(), TradeBalanceRequest{Asset: "ZEUR"})
	if err != nil {
		t.Fatalf("TradeBalance() error = %v", err)
	}
	if balance.EquivalentBalance != "1101.3425" || balance.Balance != "392.2264" ||
		balance.MarginAmount != "7.0505" || balance.UnrealizedPnL != "-10.0232" ||
		balance.CostBasis != "21.1063" || balance.FloatingValuation != "15.1438" ||
		balance.Equity != "381.8232" || balance.FreeMargin != "375.1759" ||
		balance.MarginLevel != "5415.28" || len(balance.Raw) == 0 {
		t.Fatalf("TradeBalance() = %+v", balance)
	}
	request := server.last(t)
	form := request.form(t)
	if request.Method != http.MethodPost || request.Path != privatePrefix+"TradeBalance" ||
		request.ContentType != formContentType || form.Get("asset") != "ZEUR" || form.Get("nonce") == "" {
		t.Fatalf("TradeBalance request = %+v form = %v", request, form)
	}
	snapshot, err := limiter.Snapshot("kraken:account:kraken-main:private-counter")
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("private limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestQueryTradesReturnsTradesInIDOrder(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "QueryTrades": `{"error":[],"result":{"TTEUX3-HDAAA-RC2RUO":{"ordertxid":"OH76VO-5WZ6A-KQPGZK","postxid":"TKH2SE-M7IF5-CFI7LT","pair":"XXBTZUSD","time":1688667796.8802,"type":"sell","ordertype":"limit","price":"30010.00000","cost":"600.20000","fee":"0.00000","vol":"0.02000000","margin":"0.00000","misc":"","trade_id":40274859},"THVRQM-33VKH-UCI7BS":{"ordertxid":"OQCLML-BW3P3-BUCMWZ","pair":"XXBTZUSD","time":1688666559.8974,"type":"buy","ordertype":"market","price":"30000.00000","cost":"300.00000","fee":"0.00000","vol":"0.01000000","margin":"0.00000","misc":""}}}`,
	})
	client, limiter := newExtClient(t, server.URL)
	trades, err := client.QueryTrades(context.Background(), QueryTradesRequest{
		TransactionIDs: []string{"THVRQM-33VKH-UCI7BS", "TTEUX3-HDAAA-RC2RUO"}, IncludeTrades: true,
	})
	if err != nil {
		t.Fatalf("QueryTrades() error = %v", err)
	}
	if len(trades) != 2 || trades[0].TransactionID != "THVRQM-33VKH-UCI7BS" ||
		trades[0].OrderID != "OQCLML-BW3P3-BUCMWZ" || trades[0].Side != SideBuy ||
		trades[1].TransactionID != "TTEUX3-HDAAA-RC2RUO" || trades[1].TradeID != 40274859 ||
		trades[1].Side != SideSell || trades[1].Price != "30010.00000" || len(trades[1].Raw) == 0 {
		t.Fatalf("QueryTrades() = %+v", trades)
	}
	form := server.last(t).form(t)
	if form.Get("txid") != "THVRQM-33VKH-UCI7BS,TTEUX3-HDAAA-RC2RUO" || form.Get("trades") != "true" ||
		form.Get("ledgers") != "" {
		t.Fatalf("QueryTrades form = %v", form)
	}
	snapshot, err := limiter.Snapshot("kraken:account:kraken-main:private-counter")
	if err != nil || snapshot.Used != 4 {
		t.Fatalf("history limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestOpenPositionsDecodesKeyedAndConsolidatedResults(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "OpenPositions": `{"error":[],"result":{"TF5GVO-T7ZZ2-6NVH4O":{"ordertxid":"OLWNFG-LLH4R-D6SFFP","posstatus":"open","pair":"XXBTZUSD","time":1605280097.8294,"type":"buy","ordertype":"limit","cost":"104610.52842","fee":"289.06565","vol":"8.82412861","vol_closed":"0.20200000","margin":"20922.10568","value":"94600.1","net":"-9880.1","terms":"0.0200% per 4 hours","rollovertm":"1616672637","misc":"","oflags":""}}}`,
	})
	client, _ := newExtClient(t, server.URL)
	positions, err := client.OpenPositions(context.Background(), OpenPositionsRequest{
		TransactionIDs: []string{"TF5GVO-T7ZZ2-6NVH4O"}, IncludeCalculations: true,
	})
	if err != nil {
		t.Fatalf("OpenPositions() error = %v", err)
	}
	if len(positions.Positions) != 1 || len(positions.Consolidated) != 0 ||
		positions.Positions[0].TransactionID != "TF5GVO-T7ZZ2-6NVH4O" ||
		positions.Positions[0].OrderID != "OLWNFG-LLH4R-D6SFFP" || positions.Positions[0].Side != SideBuy ||
		positions.Positions[0].Volume != "8.82412861" || positions.Positions[0].Terms == "" ||
		len(positions.Positions[0].Raw) == 0 || len(positions.Raw) == 0 {
		t.Fatalf("OpenPositions() = %+v", positions)
	}
	form := server.last(t).form(t)
	if form.Get("txid") != "TF5GVO-T7ZZ2-6NVH4O" || form.Get("docalcs") != "true" ||
		form.Get("consolidation") != "" {
		t.Fatalf("OpenPositions form = %v", form)
	}

	consolidatedServer := newExtServer(t, map[string]string{
		privatePrefix + "OpenPositions": `{"error":[],"result":[{"pair":"XXBTZUSD","positions":"2","type":"buy","leverage":"2.00000","cost":"1000.0","fee":"2.5","vol":"0.1","vol_closed":0,"margin":"500.0","value":"1010.0","net":"+10.0"}]}`,
	})
	consolidatedClient, _ := newExtClient(t, consolidatedServer.URL)
	consolidated, err := consolidatedClient.OpenPositions(context.Background(), OpenPositionsRequest{
		ConsolidateByMarket: true,
	})
	if err != nil {
		t.Fatalf("OpenPositions(consolidated) error = %v", err)
	}
	if len(consolidated.Consolidated) != 1 || len(consolidated.Positions) != 0 ||
		consolidated.Consolidated[0].Pair != "XXBTZUSD" || consolidated.Consolidated[0].Positions != "2" ||
		consolidated.Consolidated[0].Leverage != "2.00000" || consolidated.Consolidated[0].ClosedVolume != "0" ||
		consolidated.Consolidated[0].Net != "+10.0" {
		t.Fatalf("OpenPositions(consolidated) = %+v", consolidated)
	}
	if form := consolidatedServer.last(t).form(t); form.Get("consolidation") != "market" ||
		form.Get("txid") != "" {
		t.Fatalf("consolidated form = %v", form)
	}
}

func TestLedgersDecodesCursorPage(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "Ledgers": `{"error":[],"result":{"ledger":{"L4UESK-KG3EQ-UFO4T5":{"refid":"TJKLXQ-WUXMU-TO6LI4","time":1688464484.1787,"type":"trade","subtype":"tradespot","aclass":"currency","asset":"ZGBP","amount":"-24.5000","fee":"0.0490","balance":"459567.9171"},"LMKZCZ-Z3GVL-CXKK4H":{"refid":"TBZIP2-F6QOU-TMB6FY","time":1688444262.8888,"type":"trade","subtype":"","aclass":"currency","asset":"XXBT","amount":"0.0005","fee":"0.0000","balance":"0.0005"}},"count":null,"cursor":{"next":"Q0xPU0VEOjE2ODg0NDQyNjI"}}}`,
	})
	client, limiter := newExtClient(t, server.URL)
	start := time.Unix(1688000000, 0)
	end := time.Unix(1689000000, 0)
	page, err := client.Ledgers(context.Background(), LedgersRequest{
		Assets: []string{"XXBT", "ZGBP"}, Type: LedgerTypeTrade, Start: &start, End: &end,
		WithCursor: true, WithoutCount: true,
	})
	if err != nil {
		t.Fatalf("Ledgers() error = %v", err)
	}
	if len(page.Entries) != 2 || page.Entries[0].ID != "L4UESK-KG3EQ-UFO4T5" ||
		page.Entries[0].ReferenceID != "TJKLXQ-WUXMU-TO6LI4" || page.Entries[0].Amount != "-24.5000" ||
		page.Entries[0].Subtype != "tradespot" || page.Entries[1].ID != "LMKZCZ-Z3GVL-CXKK4H" ||
		page.Count != 0 || page.NextCursor != "Q0xPU0VEOjE2ODg0NDQyNjI" || len(page.Raw) == 0 ||
		len(page.Entries[1].Raw) == 0 {
		t.Fatalf("Ledgers() = %+v", page)
	}
	form := server.last(t).form(t)
	if form.Get("asset") != "XXBT,ZGBP" || form.Get("type") != "trade" || form.Get("start") != "1688000000" ||
		form.Get("end") != "1689000000" || form.Get("with_cursor") != "true" ||
		form.Get("without_count") != "true" || form.Get("cursor") != "" {
		t.Fatalf("Ledgers form = %v", form)
	}
	snapshot, err := limiter.Snapshot("kraken:account:kraken-main:private-counter")
	if err != nil || snapshot.Used != 4 {
		t.Fatalf("history limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestQueryLedgersReturnsEntriesInIDOrder(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "QueryLedgers": `{"error":[],"result":{"L4UESK-KG3EQ-UFO4T5":{"refid":"TJKLXQ-WUXMU-TO6LI4","time":1688464484.1787,"type":"trade","subtype":"","aclass":"currency","asset":"ZGBP","amount":"-24.5000","fee":"0.0490","balance":"459567.9171","amount_token":"0.01"}}}`,
	})
	client, _ := newExtClient(t, server.URL)
	entries, err := client.QueryLedgers(context.Background(), QueryLedgersRequest{
		IDs: []string{"L4UESK-KG3EQ-UFO4T5"},
	})
	if err != nil {
		t.Fatalf("QueryLedgers() error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "L4UESK-KG3EQ-UFO4T5" || entries[0].Balance != "459567.9171" ||
		entries[0].AmountToken != "0.01" || entries[0].Fee != "0.0490" {
		t.Fatalf("QueryLedgers() = %+v", entries)
	}
	if form := server.last(t).form(t); form.Get("id") != "L4UESK-KG3EQ-UFO4T5" {
		t.Fatalf("QueryLedgers form = %v", form)
	}
}

func TestTradeVolumeDecodesFeeTiers(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "TradeVolume": `{"error":[],"result":{"currency":"ZUSD","volume":"200709587.4223","fees":{"XXBTZUSD":{"fee":"0.1000","minfee":"0.1000","maxfee":"0.2600","nextfee":null,"tiervolume":"10000000.0000","nextvolume":null}},"fees_maker":{"XXBTZUSD":{"fee":"0.0000","minfee":"0.0000","maxfee":"0.1600","nextfee":null,"tiervolume":"10000000.0000","nextvolume":null}},"inputs":{"domain_spot_volume_30d":"200709587.4223","domain_futures_volume_30d":"0","domain_assets_on_platform":"1500.5"},"volume_subaccounts":[{"iiban":"AB12 CD34 EF56 GH78","volume":"12.5"}]}}`,
	})
	client, _ := newExtClient(t, server.URL)
	volume, err := client.TradeVolume(context.Background(), TradeVolumeRequest{
		Pairs: []string{"XXBTZUSD"}, IncludeFeeSchedule: true,
	})
	if err != nil {
		t.Fatalf("TradeVolume() error = %v", err)
	}
	taker, maker := volume.Fees["XXBTZUSD"], volume.FeesMaker["XXBTZUSD"]
	if volume.Currency != "ZUSD" || volume.Volume != "200709587.4223" || taker.Fee != "0.1000" ||
		taker.MaxFee != "0.2600" || taker.NextFee != "" || maker.Fee != "0.0000" ||
		volume.Inputs.SpotVolume30Days != "200709587.4223" || volume.Inputs.AssetsOnPlatform != "1500.5" ||
		len(volume.SubaccountVolumes) != 1 || volume.SubaccountVolumes[0].Volume != "12.5" ||
		len(volume.Raw) == 0 {
		t.Fatalf("TradeVolume() = %+v", volume)
	}
	form := server.last(t).form(t)
	if form.Get("pair") != "XXBTZUSD" || form.Get("fee_schedule") != "true" {
		t.Fatalf("TradeVolume form = %v", form)
	}
}

func TestOrderAmendsDecodesHistory(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "OrderAmends": `{"error":[],"result":{"count":2,"amends":[{"amend_id":"TJ6NWM-XEFQG-U4OUKG","amend_type":"original","order_qty":"1.5","display_qty":"0.5","remaining_qty":"1.5","limit_price":"29000.0","post_only":false,"timestamp":1688464484000},{"amend_id":"T4C5AV-S4LBP-HOGBPR","amend_type":"user","order_qty":"1.2","remaining_qty":"1.2","limit_price":"29100.0","trigger_price":"28900.0","reason":"user amend","post_only":true,"timestamp":1688464499000}]}}`,
	})
	client, _ := newExtClient(t, server.URL)
	amends, err := client.OrderAmends(context.Background(), OrderAmendsRequest{OrderID: "OHYO67-6LP66-HMQ437"})
	if err != nil {
		t.Fatalf("OrderAmends() error = %v", err)
	}
	if amends.Count != 2 || len(amends.Amends) != 2 || amends.Amends[0].AmendType != "original" ||
		amends.Amends[1].LimitPrice != "29100.0" || !amends.Amends[1].PostOnly ||
		amends.Amends[1].Timestamp != 1688464499000 || amends.Amends[1].TriggerPrice != "28900.0" ||
		len(amends.Raw) == 0 {
		t.Fatalf("OrderAmends() = %+v", amends)
	}
	if form := server.last(t).form(t); form.Get("order_id") != "OHYO67-6LP66-HMQ437" {
		t.Fatalf("OrderAmends form = %v", form)
	}
}

func TestCreditLinesDecodesDetailsAndEmptyResult(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "CreditLines": `{"error":[],"result":{"asset_details":{"XBT":{"balance":"5.0","hold_trade":0,"credit":"2.5","credit_used":"1.0","collateral_value":"3.25","rollover_fees":"0.01","reserve_fees":"0"}},"limits_monitor":{"total_credit_usd":"100000.50","total_credit_used_usd":"50000","total_collateral_value_usd":"80000","equity_usd":"90000","ongoing_balance":"10000","debt_to_equity":0.55}}}`,
	})
	client, _ := newExtClient(t, server.URL)
	lines, err := client.CreditLines(context.Background())
	if err != nil {
		t.Fatalf("CreditLines() error = %v", err)
	}
	asset, exists := lines.AssetDetails["XBT"]
	if !exists || asset.Balance != "5.0" || asset.HoldTrade != "0" || asset.Credit != "2.5" ||
		asset.CollateralValue != "3.25" || lines.LimitsMonitor.TotalCreditUSD != "100000.50" ||
		lines.LimitsMonitor.DebtToEquity != "0.55" || len(lines.Raw) == 0 {
		t.Fatalf("CreditLines() = %+v", lines)
	}
	if request := server.last(t); request.Path != privatePrefix+"CreditLines" || request.form(t).Get("nonce") == "" {
		t.Fatalf("CreditLines request = %+v", request)
	}

	emptyServer := newExtServer(t, map[string]string{
		privatePrefix + "CreditLines": `{"error":[],"result":null}`,
	})
	emptyClient, _ := newExtClient(t, emptyServer.URL)
	empty, err := emptyClient.CreditLines(context.Background())
	if err != nil {
		t.Fatalf("CreditLines(null result) error = %v", err)
	}
	if len(empty.AssetDetails) != 0 || len(empty.Raw) != 0 {
		t.Fatalf("CreditLines(null result) = %+v", empty)
	}
}

func TestAPIKeyInfoAcceptsSnakeAndCamelCase(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "GetApiKeyInfo": `{"error":[],"result":{"api_key_name":"trading-bot","api_key":"abcDEF","nonce":"incrementing","nonce_window":5000,"permissions":["query-funds","create-modify-orders"],"valid_until":"0","ip_allowlist":["203.0.113.4"],"last_used":1688464484}}`,
	})
	client, _ := newExtClient(t, server.URL)
	info, err := client.APIKeyInfo(context.Background(), APIKeyInfoRequest{OTP: "123456"})
	if err != nil {
		t.Fatalf("APIKeyInfo() error = %v", err)
	}
	if info.Name != "trading-bot" || info.Key != "abcDEF" || info.Nonce != "incrementing" ||
		info.NonceWindow != "5000" || len(info.Permissions) != 2 || info.ValidUntil != "0" ||
		len(info.IPAllowlist) != 1 || info.IPAllowlist[0] != "203.0.113.4" || info.LastUsed != "1688464484" ||
		len(info.Raw) == 0 {
		t.Fatalf("APIKeyInfo() = %+v", info)
	}
	if form := server.last(t).form(t); form.Get("otp") != "123456" {
		t.Fatalf("APIKeyInfo form = %v", form)
	}

	var camel APIKeyInfo
	if err := camel.UnmarshalJSON([]byte(`{"apiKeyName":"bot","nonceWindow":"7","validUntil":0,"ipAllowlist":["198.51.100.1"],"lastUsed":"12"}`)); err != nil {
		t.Fatalf("UnmarshalJSON(camelCase) error = %v", err)
	}
	if camel.Name != "bot" || camel.NonceWindow != "7" || camel.ValidUntil != "0" ||
		len(camel.IPAllowlist) != 1 || camel.LastUsed != "12" {
		t.Fatalf("camelCase APIKeyInfo = %+v", camel)
	}
}

func TestListWalletAccountsDecodesPage(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "ListWalletAccounts": `{"error":[],"result":{"accounts":[{"account_id":"WX6V-JUKW-KKPB-QE36","flags":{"user_defined":false,"active":true},"status":"open","type":"spot","name":"Spot Wallet"},{"account_id":"AB12-CD34-EF56-GH78","flags":{"user_defined":true,"active":false},"status":"closed","type":"funding","name":"Savings"}],"cursor":{"next":"abc123"}}}`,
	})
	client, _ := newExtClient(t, server.URL)
	page, err := client.ListWalletAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListWalletAccounts() error = %v", err)
	}
	if len(page.Accounts) != 2 || page.Accounts[0].AccountID != "WX6V-JUKW-KKPB-QE36" ||
		!page.Accounts[0].Flags.Active || page.Accounts[0].Flags.UserDefined ||
		page.Accounts[1].Name != "Savings" || !page.Accounts[1].Flags.UserDefined ||
		page.NextCursor != "abc123" || len(page.Raw) == 0 {
		t.Fatalf("ListWalletAccounts() = %+v", page)
	}
}

func TestAccountExtensionsRequireReadPermissionAndValidateRequests(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{})
	client, _ := newExtClient(t, server.URL, credential.PermissionTrade)
	if _, err := client.TradeBalance(context.Background(), TradeBalanceRequest{}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("TradeBalance() error = %v, want authorization", err)
	}
	if _, err := client.Ledgers(context.Background(), LedgersRequest{}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Ledgers() error = %v, want authorization", err)
	}
	if _, err := client.ListWalletAccounts(context.Background()); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("ListWalletAccounts() error = %v, want authorization", err)
	}
	if server.count() != 0 {
		t.Fatalf("server requests = %d, want 0", server.count())
	}

	manyIDs := make([]string, maxQueryTradeIDs+1)
	for index := range manyIDs {
		manyIDs[index] = fmt.Sprintf("TRADE-%d", index)
	}
	end := time.Unix(1000, 0)
	start := time.Unix(2000, 0)
	invalid := []error{
		(TradeBalanceRequest{Asset: "bad asset"}).validate(),
		(QueryTradesRequest{}).validate(),
		(QueryTradesRequest{TransactionIDs: manyIDs}).validate(),
		(QueryTradesRequest{TransactionIDs: []string{"A", "A"}}).validate(),
		(QueryTradesRequest{TransactionIDs: []string{"bad id"}}).validate(),
		(OpenPositionsRequest{TransactionIDs: []string{"bad id"}}).validate(),
		(LedgersRequest{Type: "unknown"}).validate(),
		(LedgersRequest{Assets: []string{"XXBT", "XXBT"}}).validate(),
		(LedgersRequest{Start: &start, End: &end}).validate(),
		(LedgersRequest{Cursor: "abc"}).validate(),
		(LedgersRequest{WithCursor: true, Cursor: "bad cursor"}).validate(),
		(QueryLedgersRequest{}).validate(),
		(QueryLedgersRequest{IDs: []string{"bad id"}}).validate(),
		(TradeVolumeRequest{Pairs: []string{"XBT USD"}}).validate(),
		(OrderAmendsRequest{}).validate(),
		(APIKeyInfoRequest{OTP: "12\n34"}).validate(),
	}
	for index, err := range invalid {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v, want validation", index, err)
		}
	}
	valid := []error{
		(TradeBalanceRequest{}).validate(),
		(OpenPositionsRequest{}).validate(),
		(LedgersRequest{Assets: []string{"XXBT"}, Type: LedgerTypeAll, WithCursor: true, Cursor: "abc"}).validate(),
		(TradeVolumeRequest{}).validate(),
		(APIKeyInfoRequest{}).validate(),
	}
	for index, err := range valid {
		if err != nil {
			t.Fatalf("valid request %d error = %v", index, err)
		}
	}
	if values := (QueryLedgersRequest{IDs: []string{"A", "B"}}).values(); strings.TrimSpace(values.Get("id")) != "A,B" {
		t.Fatalf("QueryLedgers values = %v", values)
	}
}
