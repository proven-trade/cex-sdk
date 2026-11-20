package usdm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/transport"
)

func TestAccountExtensionReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	start, end := time.UnixMilli(marketStartMS), time.UnixMilli(marketEndMS)
	symbolParams := url.Values{"symbol": {"BTCUSDT"}}
	bracketBody := `{"symbol":"BTCUSDT","notionalCoef":1.5,"brackets":[{"bracket":1,"initialLeverage":125,"notionalCap":50000,"notionalFloor":0,"maintMarginRatio":0.004,"cum":0},{"bracket":2,"initialLeverage":100,"notionalCap":600000,"notionalFloor":50000,"maintMarginRatio":0.005,"cum":50}]}`
	bracketsBody := `[{"symbol":"BTCUSDT","brackets":[{"bracket":1,"initialLeverage":125,"notionalCap":50000,"notionalFloor":0,"maintMarginRatio":0.004,"cum":0}]},{"symbol":"ETHUSDT","brackets":[{"bracket":1,"initialLeverage":100,"notionalCap":10000,"notionalFloor":0,"maintMarginRatio":0.005,"cum":0}]}]`
	indicatorBody := `{"indicators":{"BTCUSDT":[{"isLocked":true,"plannedRecoverTime":1545741270000,"indicator":"UFR","value":0.05,"triggerValue":0.995}],"ACCOUNT":[{"indicator":"TMV","value":10,"triggerValue":1,"plannedRecoverTime":0,"isLocked":false}]},"updateTime":1545741270000}`

	runSignedCases(t, []signedCase{
		{name: "balances", method: http.MethodGet, path: "/fapi/v3/balance", body: `[{"accountAlias":"SgsR","asset":"USDT","balance":"122607.35137903","crossWalletBalance":"23.72469206","crossUnPnl":"0.00000001","availableBalance":"23.72469206","maxWithdrawAmount":"23.72469206","marginAvailable":true,"updateTime":1617939110373}]`, weight: 5, run: func(t *testing.T, client *Client) {
			items, err := client.Balances(ctx)
			if err != nil || len(items) != 1 || items[0].Asset != "USDT" || items[0].WalletBalance != "122607.35137903" || items[0].CrossWalletBalance != "23.72469206" || items[0].CrossUnrealizedProfit != "0.00000001" || items[0].AvailableBalance != "23.72469206" || items[0].MaximumWithdrawAmount != "23.72469206" || !items[0].MarginAvailable || items[0].UpdateTime != 1617939110373 || len(items[0].Raw) == 0 {
				t.Fatalf("Balances() = %+v, error = %v", items, err)
			}
		}},
		{name: "account config", method: http.MethodGet, path: "/fapi/v1/accountConfig", body: `{"feeTier":2,"canTrade":true,"canDeposit":true,"canWithdraw":false,"dualSidePosition":true,"updateTime":0,"multiAssetsMargin":false,"tradeGroupId":-1}`, weight: 5, run: func(t *testing.T, client *Client) {
			config, err := client.AccountConfig(ctx)
			if err != nil || config.FeeTier != 2 || !config.CanTrade || !config.CanDeposit || config.CanWithdraw || !config.DualSidePosition || config.MultiAssetsMargin || config.TradeGroupID != -1 || len(config.Raw) == 0 {
				t.Fatalf("AccountConfig() = %+v, error = %v", config, err)
			}
		}},
		{name: "symbol configs with string boolean", method: http.MethodGet, path: "/fapi/v1/symbolConfig", params: symbolParams, body: `[{"symbol":"BTCUSDT","marginType":"CROSSED","isAutoAddMargin":"true","leverage":21,"maxNotionalValue":"1000000"}]`, weight: 5, run: func(t *testing.T, client *Client) {
			items, err := client.SymbolConfigs(ctx, SymbolConfigsRequest{Symbol: "BTCUSDT"})
			if err != nil || len(items) != 1 || items[0].Symbol != "BTCUSDT" || items[0].MarginType != MarginTypeCrossed || !items[0].IsAutoAddMargin || items[0].Leverage != 21 || items[0].MaxNotionalValue != "1000000" || len(items[0].Raw) == 0 {
				t.Fatalf("SymbolConfigs() = %+v, error = %v", items, err)
			}
		}},
		{name: "symbol configs all with boolean", method: http.MethodGet, path: "/fapi/v1/symbolConfig", body: `[{"symbol":"BTCUSDT","marginType":"ISOLATED","isAutoAddMargin":false,"leverage":10,"maxNotionalValue":"INF"},{"symbol":"ETHUSDT","marginType":"CROSSED","isAutoAddMargin":true,"leverage":20,"maxNotionalValue":"INF"}]`, weight: 5, run: func(t *testing.T, client *Client) {
			items, err := client.SymbolConfigs(ctx, SymbolConfigsRequest{})
			if err != nil || len(items) != 2 || items[0].IsAutoAddMargin || items[0].MarginType != MarginTypeIsolated || items[0].MaxNotionalValue != "INF" || !items[1].IsAutoAddMargin || items[1].Leverage != 20 {
				t.Fatalf("SymbolConfigs() = %+v, error = %v", items, err)
			}
		}},
		{name: "position mode", method: http.MethodGet, path: "/fapi/v1/positionSide/dual", body: `{"dualSidePosition":true}`, weight: 30, run: func(t *testing.T, client *Client) {
			mode, err := client.PositionMode(ctx)
			if err != nil || !mode.DualSidePosition || len(mode.Raw) == 0 {
				t.Fatalf("PositionMode() = %+v, error = %v", mode, err)
			}
		}},
		{name: "multi assets mode", method: http.MethodGet, path: "/fapi/v1/multiAssetsMargin", body: `{"multiAssetsMargin":true}`, weight: 30, run: func(t *testing.T, client *Client) {
			mode, err := client.MultiAssetsMode(ctx)
			if err != nil || !mode.MultiAssetsMargin || len(mode.Raw) == 0 {
				t.Fatalf("MultiAssetsMode() = %+v, error = %v", mode, err)
			}
		}},
		{name: "income history", method: http.MethodGet, path: "/fapi/v1/income", params: url.Values{"symbol": {"BTCUSDT"}, "incomeType": {"FUNDING_FEE"}, "startTime": {"1700000000000"}, "endTime": {"1700001800000"}, "limit": {"100"}}, body: `[{"symbol":"BTCUSDT","incomeType":"FUNDING_FEE","income":"-0.37500000","asset":"USDT","info":"FUNDING_FEE","time":1570608000000,"tranId":9689322392,"tradeId":""},{"symbol":"BTCUSDT","incomeType":"COMMISSION","income":"-0.01000000","asset":"USDT","info":"COMMISSION","time":1570636800000,"tranId":9689322393,"tradeId":"2059192"}]`, weight: 30, run: func(t *testing.T, client *Client) {
			items, err := client.IncomeHistory(ctx, IncomeHistoryRequest{Symbol: "BTCUSDT", IncomeType: "FUNDING_FEE", StartTime: &start, EndTime: &end, Limit: 100})
			if err != nil || len(items) != 2 || items[0].IncomeType != "FUNDING_FEE" || items[0].Income != "-0.37500000" || items[0].Asset != "USDT" || items[0].TranID != 9689322392 || items[0].TradeID != "" || items[1].TradeID != "2059192" || len(items[0].Raw) == 0 {
				t.Fatalf("IncomeHistory() = %+v, error = %v", items, err)
			}
		}},
		{name: "income history all", method: http.MethodGet, path: "/fapi/v1/income", body: `[]`, weight: 30, run: func(t *testing.T, client *Client) {
			items, err := client.IncomeHistory(ctx, IncomeHistoryRequest{})
			if err != nil || items == nil || len(items) != 0 {
				t.Fatalf("IncomeHistory() = %+v, error = %v", items, err)
			}
		}},
		{name: "commission rate", method: http.MethodGet, path: "/fapi/v1/commissionRate", params: symbolParams, body: `{"symbol":"BTCUSDT","makerCommissionRate":"0.0002","takerCommissionRate":"0.0004","rpiCommissionRate":"0.00005"}`, weight: 20, run: func(t *testing.T, client *Client) {
			rate, err := client.CommissionRate(ctx, CommissionRateRequest{Symbol: "BTCUSDT"})
			if err != nil || rate.Symbol != "BTCUSDT" || rate.MakerCommissionRate != "0.0002" || rate.TakerCommissionRate != "0.0004" || rate.RPICommissionRate != "0.00005" || len(rate.Raw) == 0 {
				t.Fatalf("CommissionRate() = %+v, error = %v", rate, err)
			}
		}},
		{name: "leverage brackets for symbol", method: http.MethodGet, path: "/fapi/v1/leverageBracket", params: symbolParams, body: bracketBody, weight: 1, run: func(t *testing.T, client *Client) {
			items, err := client.LeverageBrackets(ctx, LeverageBracketsRequest{Symbol: "BTCUSDT"})
			if err != nil || len(items) != 1 || items[0].NotionalCoef.String() != "1.5" || len(items[0].Brackets) != 2 || len(items[0].Raw) == 0 {
				t.Fatalf("LeverageBrackets() = %+v, error = %v", items, err)
			}
			tier := items[0].Brackets[1]
			if tier.Bracket != 2 || tier.InitialLeverage != 100 || tier.NotionalCap.String() != "600000" || tier.NotionalFloor.String() != "50000" || tier.MaintMarginRatio.String() != "0.005" || tier.Cum.String() != "50" {
				t.Fatalf("LeverageBrackets() tier = %+v", tier)
			}
		}},
		{name: "leverage brackets all", method: http.MethodGet, path: "/fapi/v1/leverageBracket", body: bracketsBody, weight: 1, run: func(t *testing.T, client *Client) {
			items, err := client.LeverageBrackets(ctx, LeverageBracketsRequest{})
			if err != nil || len(items) != 2 || items[1].Symbol != "ETHUSDT" || items[0].NotionalCoef != "" || items[1].Brackets[0].InitialLeverage != 100 {
				t.Fatalf("LeverageBrackets() = %+v, error = %v", items, err)
			}
		}},
		{name: "order rate limits", method: http.MethodGet, path: "/fapi/v1/rateLimit/order", body: `[{"rateLimitType":"ORDERS","interval":"SECOND","intervalNum":10,"limit":10000},{"rateLimitType":"ORDERS","interval":"MINUTE","intervalNum":1,"limit":20000}]`, weight: 1, run: func(t *testing.T, client *Client) {
			limits, err := client.OrderRateLimits(ctx)
			if err != nil || len(limits) != 2 || limits[0].Type != "ORDERS" || limits[0].Interval != "SECOND" || limits[0].IntervalNum != 10 || limits[0].Limit != 10000 || limits[1].Interval != "MINUTE" || limits[1].Limit != 20000 {
				t.Fatalf("OrderRateLimits() = %+v, error = %v", limits, err)
			}
		}},
		{name: "api trading status for symbol", method: http.MethodGet, path: "/fapi/v1/apiTradingStatus", params: symbolParams, body: indicatorBody, weight: 1, run: func(t *testing.T, client *Client) {
			status, err := client.APITradingStatus(ctx, APITradingStatusRequest{Symbol: "BTCUSDT"})
			if err != nil || status.UpdateTime != 1545741270000 || len(status.Indicators["BTCUSDT"]) != 1 || len(status.Indicators["ACCOUNT"]) != 1 || len(status.Raw) == 0 {
				t.Fatalf("APITradingStatus() = %+v, error = %v", status, err)
			}
			indicator := status.Indicators["BTCUSDT"][0]
			if indicator.Indicator != "UFR" || !indicator.IsLocked || indicator.Value.String() != "0.05" || indicator.TriggerValue.String() != "0.995" || indicator.PlannedRecoverTime != 1545741270000 {
				t.Fatalf("APITradingStatus() indicator = %+v", indicator)
			}
		}},
		{name: "api trading status all", method: http.MethodGet, path: "/fapi/v1/apiTradingStatus", body: indicatorBody, weight: 10, run: func(t *testing.T, client *Client) {
			status, err := client.APITradingStatus(ctx, APITradingStatusRequest{})
			if err != nil || status.Indicators["ACCOUNT"][0].Indicator != "TMV" {
				t.Fatalf("APITradingStatus() = %+v, error = %v", status, err)
			}
		}},
		{name: "fee burn", method: http.MethodGet, path: "/fapi/v1/feeBurn", body: `{"feeBurn":true}`, weight: 30, run: func(t *testing.T, client *Client) {
			status, err := client.FeeBurn(ctx)
			if err != nil || !status.FeeBurn || len(status.Raw) == 0 {
				t.Fatalf("FeeBurn() = %+v, error = %v", status, err)
			}
		}},
		{name: "change fee burn", method: http.MethodPost, path: "/fapi/v1/feeBurn", params: url.Values{"feeBurn": {"true"}}, body: `{"code":200,"msg":"success"}`, weight: 1, run: func(t *testing.T, client *Client) {
			result, err := client.ChangeFeeBurn(ctx, ChangeFeeBurnRequest{Enabled: true})
			if err != nil || result.Code != 200 || result.Message != "success" {
				t.Fatalf("ChangeFeeBurn() = %+v, error = %v", result, err)
			}
		}},
		{name: "classic portfolio margin account", method: http.MethodGet, path: "/fapi/v1/pmAccountInfo", params: url.Values{"asset": {"BTC"}}, body: `{"maxWithdrawAmountUSD":"1627523.32459208","asset":"BTC","maxWithdrawAmount":"27.43689636"}`, weight: 5, run: func(t *testing.T, client *Client) {
			info, err := client.ClassicPortfolioMarginAccount(ctx, ClassicPortfolioMarginAccountRequest{Asset: "BTC"})
			if err != nil || info.Asset != "BTC" || info.MaxWithdrawAmount != "27.43689636" || info.MaxWithdrawAmountUSD != "1627523.32459208" || len(info.Raw) == 0 {
				t.Fatalf("ClassicPortfolioMarginAccount() = %+v, error = %v", info, err)
			}
		}},
	})
}

func TestAccountExtensionDownloads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rangeRequest := DownloadRangeRequest{StartTime: time.UnixMilli(marketStartMS), EndTime: time.UnixMilli(marketEndMS)}
	rangeParams := url.Values{"startTime": {"1700000000000"}, "endTime": {"1700001800000"}}
	linkParams := url.Values{"downloadId": {"545923594199212032"}}
	ticketBody := `{"avgCostTimestampOfLast30d":7241837,"downloadId":"546975389218332672"}`
	linkBody := `{"downloadId":"545923594199212032","status":"completed","url":"https://download.example/file.csv","notified":true,"expirationTimestamp":1645009771000,"isExpired":null}`
	checkTicket := func(t *testing.T, name string, ticket DownloadTicket, err error) {
		t.Helper()
		if err != nil || ticket.DownloadID != "546975389218332672" || ticket.AverageCostMillis != 7241837 || len(ticket.Raw) == 0 {
			t.Fatalf("%s() = %+v, error = %v", name, ticket, err)
		}
	}
	checkLink := func(t *testing.T, name string, link DownloadLink, err error) {
		t.Helper()
		if err != nil || link.DownloadID != "545923594199212032" || link.Status != "completed" || link.URL != "https://download.example/file.csv" || !link.Notified || link.ExpirationTimestamp != 1645009771000 || len(link.Raw) == 0 {
			t.Fatalf("%s() = %+v, error = %v", name, link, err)
		}
	}

	runSignedCases(t, []signedCase{
		{name: "order history download", method: http.MethodGet, path: "/fapi/v1/order/asyn", params: rangeParams, body: ticketBody, weight: 1000, run: func(t *testing.T, client *Client) {
			ticket, err := client.RequestOrderHistoryDownload(ctx, rangeRequest)
			checkTicket(t, "RequestOrderHistoryDownload", ticket, err)
		}},
		{name: "order history link", method: http.MethodGet, path: "/fapi/v1/order/asyn/id", params: linkParams, body: linkBody, weight: 10, run: func(t *testing.T, client *Client) {
			link, err := client.OrderHistoryDownloadLink(ctx, DownloadLinkRequest{DownloadID: "545923594199212032"})
			checkLink(t, "OrderHistoryDownloadLink", link, err)
		}},
		{name: "trade history download", method: http.MethodGet, path: "/fapi/v1/trade/asyn", params: rangeParams, body: ticketBody, weight: 1000, run: func(t *testing.T, client *Client) {
			ticket, err := client.RequestTradeHistoryDownload(ctx, rangeRequest)
			checkTicket(t, "RequestTradeHistoryDownload", ticket, err)
		}},
		{name: "trade history link", method: http.MethodGet, path: "/fapi/v1/trade/asyn/id", params: linkParams, body: linkBody, weight: 10, run: func(t *testing.T, client *Client) {
			link, err := client.TradeHistoryDownloadLink(ctx, DownloadLinkRequest{DownloadID: "545923594199212032"})
			checkLink(t, "TradeHistoryDownloadLink", link, err)
		}},
		{name: "income history download", method: http.MethodGet, path: "/fapi/v1/income/asyn", params: rangeParams, body: ticketBody, weight: 1000, run: func(t *testing.T, client *Client) {
			ticket, err := client.RequestIncomeHistoryDownload(ctx, rangeRequest)
			checkTicket(t, "RequestIncomeHistoryDownload", ticket, err)
		}},
		{name: "income history link", method: http.MethodGet, path: "/fapi/v1/income/asyn/id", params: linkParams, body: `{"downloadId":"545923594199212032","status":"processing","url":"","notified":false,"expirationTimestamp":-1}`, weight: 10, run: func(t *testing.T, client *Client) {
			link, err := client.IncomeHistoryDownloadLink(ctx, DownloadLinkRequest{DownloadID: "545923594199212032"})
			if err != nil || link.Status != "processing" || link.URL != "" || link.Notified || link.ExpirationTimestamp != -1 {
				t.Fatalf("IncomeHistoryDownloadLink() = %+v, error = %v", link, err)
			}
		}},
	})
}

func TestAccountExtensionDownloadRequestsAreNotRetried(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempts.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(writer, `{"code":-1007,"msg":"Timeout waiting for response from backend server. Send status unknown; execution status unknown."}`)
	}))
	t.Cleanup(server.Close)
	client, _ := newPermissionTestClient(t, server.URL, &recordingProvider{}, time.UnixMilli(marketStartMS), credential.PermissionRead)
	_, err := client.RequestOrderHistoryDownload(context.Background(), DownloadRangeRequest{StartTime: time.UnixMilli(marketStartMS), EndTime: time.UnixMilli(marketEndMS)})
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.Category != trade.ErrorUnknownExecutionState || apiError.Retryable {
		t.Fatalf("RequestOrderHistoryDownload() error = %v, want unknown execution state", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want exactly one because each request consumes a monthly quota", attempts.Load())
	}
}

func TestAccountExtensionMalformedResponses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []malformedCase{
		{"not json", `not-json`},
		{"object instead of list", `{"symbol":"BTCUSDT"}`},
		{"unexpected autoAddMargin", `[{"symbol":"BTCUSDT","isAutoAddMargin":"maybe"}]`},
		{"numeric autoAddMargin", `[{"symbol":"BTCUSDT","isAutoAddMargin":5}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server, _ := newMarketServer(t, tc.body, nil)
			client, _ := newTestClient(t, server.URL, &directSender{}, &recordingProvider{}, time.UnixMilli(marketStartMS), []transport.EgressRouteID{"route-a"})
			if items, err := client.SymbolConfigs(ctx, SymbolConfigsRequest{}); err == nil {
				t.Errorf("SymbolConfigs() = %+v, error = nil", items)
			}
		})
	}

	server, _ := newMarketServer(t, `{"code":"abc","msg":"bad"}`, nil)
	client, _ := newTestClient(t, server.URL, &directSender{}, &recordingProvider{}, time.UnixMilli(marketStartMS), []transport.EgressRouteID{"route-a"})
	if result, err := client.ChangePositionMode(ctx, ChangePositionModeRequest{}); err == nil {
		t.Errorf("ChangePositionMode() = %+v, error = nil for a non numeric code", result)
	}
}

func TestAccountExtensionValidation(t *testing.T) {
	t.Parallel()
	begin := time.UnixMilli(marketStartMS)
	later := begin.Add(time.Hour)
	oneYear := begin.AddDate(1, 0, 0)
	overYear := oneYear.Add(time.Millisecond)

	for _, tc := range []validationCase{
		{"income padded symbol", IncomeHistoryRequest{Symbol: "BTCUSDT "}.validate()},
		{"income padded type", IncomeHistoryRequest{IncomeType: " FUNDING_FEE"}.validate()},
		{"income limit", IncomeHistoryRequest{Limit: 1001}.validate()},
		{"income negative limit", IncomeHistoryRequest{Limit: -1}.validate()},
		{"income reversed", IncomeHistoryRequest{StartTime: &later, EndTime: &begin}.validate()},
		{"download zero start", DownloadRangeRequest{EndTime: later}.validate()},
		{"download zero end", DownloadRangeRequest{StartTime: begin}.validate()},
		{"download reversed", DownloadRangeRequest{StartTime: later, EndTime: begin}.validate()},
		{"download over one year", DownloadRangeRequest{StartTime: begin, EndTime: overYear}.validate()},
		{"download link empty", DownloadLinkRequest{}.validate()},
		{"download link padded", DownloadLinkRequest{DownloadID: "1 "}.validate()},
	} {
		if !errors.Is(tc.err, trade.ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", tc.name, tc.err)
		}
	}
	for _, tc := range []validationCase{
		{"income empty", IncomeHistoryRequest{}.validate()},
		{"income max limit", IncomeHistoryRequest{Symbol: "BTCUSDT", IncomeType: "COMMISSION", Limit: 1000, StartTime: &begin, EndTime: &later}.validate()},
		{"download one year", DownloadRangeRequest{StartTime: begin, EndTime: oneYear}.validate()},
		{"download link", DownloadLinkRequest{DownloadID: "545923594199212032"}.validate()},
	} {
		if tc.err != nil {
			t.Errorf("%s: error = %v, want nil", tc.name, tc.err)
		}
	}

	ctx := context.Background()
	sender := &directSender{}
	provider := &recordingProvider{}
	client, _ := newTestClient(t, "http://127.0.0.1", sender, provider, begin, []transport.EgressRouteID{"route-a"})
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"symbol configs padded", func() error {
			_, err := client.SymbolConfigs(ctx, SymbolConfigsRequest{Symbol: " BTCUSDT"})
			return err
		}},
		{"commission rate empty", func() error {
			_, err := client.CommissionRate(ctx, CommissionRateRequest{})
			return err
		}},
		{"leverage brackets padded", func() error {
			_, err := client.LeverageBrackets(ctx, LeverageBracketsRequest{Symbol: "BTCUSDT "})
			return err
		}},
		{"api trading status padded", func() error {
			_, err := client.APITradingStatus(ctx, APITradingStatusRequest{Symbol: " BTCUSDT"})
			return err
		}},
		{"classic portfolio margin asset empty", func() error {
			_, err := client.ClassicPortfolioMarginAccount(ctx, ClassicPortfolioMarginAccountRequest{})
			return err
		}},
		{"income history range", func() error {
			_, err := client.IncomeHistory(ctx, IncomeHistoryRequest{StartTime: &later, EndTime: &begin})
			return err
		}},
		{"order history download empty range", func() error {
			_, err := client.RequestOrderHistoryDownload(ctx, DownloadRangeRequest{})
			return err
		}},
		{"trade history link empty", func() error {
			_, err := client.TradeHistoryDownloadLink(ctx, DownloadLinkRequest{})
			return err
		}},
	} {
		if err := tc.call(); !errors.Is(err, trade.ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", tc.name, err)
		}
	}
	if calls, _, _ := provider.snapshot(); calls != 0 || len(sender.snapshot()) != 0 {
		t.Fatalf("secret resolutions = %d, routes = %v, want none", calls, sender.snapshot())
	}
}
