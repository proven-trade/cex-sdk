package binance

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

func TestMyTrades(t *testing.T) {
	t.Parallel()

	const body = `[{"symbol":"BNBBTC","id":28457,"orderId":100234,"orderListId":-1,"price":"4.00000100","qty":"12.00000000","quoteQty":"48.000012","commission":"10.10000000","commissionAsset":"BNB","time":1499865549590,"isBuyer":true,"isMaker":false,"isBestMatch":true}]`
	t.Run("symbol only costs 20", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		trades, err := harness.client.MyTrades(context.Background(), MyTradesRequest{Symbol: "BNBBTC"})
		if err != nil {
			t.Fatalf("MyTrades() error = %v", err)
		}
		if len(trades) != 1 {
			t.Fatalf("MyTrades() = %+v, want one trade", trades)
		}
		accountTrade := trades[0]
		if accountTrade.Symbol != "BNBBTC" || accountTrade.ID != 28457 || accountTrade.OrderID != 100234 || accountTrade.OrderListID != -1 ||
			accountTrade.Price != "4.00000100" || accountTrade.Quantity != "12.00000000" || accountTrade.QuoteQuantity != "48.000012" ||
			accountTrade.Commission != "10.10000000" || accountTrade.CommissionAsset != "BNB" || accountTrade.Time != 1499865549590 ||
			!accountTrade.Buyer || accountTrade.Maker || !accountTrade.BestMatch || len(accountTrade.Raw) == 0 {
			t.Fatalf("MyTrades() trade = %+v", accountTrade)
		}
		harness.expect(t, http.MethodGet, "/api/v3/myTrades", true, 20, 0, "symbol=BNBBTC")
	})
	t.Run("order ID costs 5", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		orderID := int64(100234)
		fromID := int64(28457)
		trades, err := harness.client.MyTrades(context.Background(), MyTradesRequest{Symbol: "BNBBTC", OrderID: &orderID, FromID: &fromID, Limit: 10})
		if err != nil {
			t.Fatalf("MyTrades() error = %v", err)
		}
		if len(trades) != 1 {
			t.Fatalf("MyTrades() = %+v, want one trade", trades)
		}
		harness.expect(t, http.MethodGet, "/api/v3/myTrades", true, 5, 0, "symbol=BNBBTC", "orderId=100234", "fromId=28457", "limit=10")
	})
	t.Run("time window costs 20", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, `[]`)
		start := time.UnixMilli(1499865549000)
		end := start.Add(time.Hour)
		trades, err := harness.client.MyTrades(context.Background(), MyTradesRequest{Symbol: "BNBBTC", StartTime: &start, EndTime: &end})
		if err != nil {
			t.Fatalf("MyTrades() error = %v", err)
		}
		if len(trades) != 0 {
			t.Fatalf("MyTrades() = %+v, want empty", trades)
		}
		harness.expect(t, http.MethodGet, "/api/v3/myTrades", true, 20, 0, "symbol=BNBBTC", "startTime=1499865549000", "endTime=1499869149000")
	})
}

func TestMyTradesValidation(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `[]`)
	ctx := context.Background()
	orderID := int64(1)
	zero := int64(0)
	negative := int64(-1)
	start := time.UnixMilli(1499865549000)
	end := start.Add(24*time.Hour + time.Millisecond)
	before := start.Add(-time.Hour)

	_, err := harness.client.MyTrades(ctx, MyTradesRequest{})
	harness.requireValidation(t, "missing symbol", err)
	_, err = harness.client.MyTrades(ctx, MyTradesRequest{Symbol: "BNBBTC", OrderID: &zero})
	harness.requireValidation(t, "zero orderId", err)
	_, err = harness.client.MyTrades(ctx, MyTradesRequest{Symbol: "BNBBTC", FromID: &negative})
	harness.requireValidation(t, "negative fromId", err)
	_, err = harness.client.MyTrades(ctx, MyTradesRequest{Symbol: "BNBBTC", OrderID: &orderID, StartTime: &start})
	harness.requireValidation(t, "orderId with startTime", err)
	_, err = harness.client.MyTrades(ctx, MyTradesRequest{Symbol: "BNBBTC", FromID: &orderID, EndTime: &end})
	harness.requireValidation(t, "fromId with endTime", err)
	_, err = harness.client.MyTrades(ctx, MyTradesRequest{Symbol: "BNBBTC", StartTime: &start, EndTime: &end})
	harness.requireValidation(t, "window over 24 hours", err)
	_, err = harness.client.MyTrades(ctx, MyTradesRequest{Symbol: "BNBBTC", StartTime: &start, EndTime: &before})
	harness.requireValidation(t, "startTime after endTime", err)
	_, err = harness.client.MyTrades(ctx, MyTradesRequest{Symbol: "BNBBTC", Limit: 1001})
	harness.requireValidation(t, "limit above 1000", err)
	_, err = harness.client.MyTrades(ctx, MyTradesRequest{Symbol: "BNBBTC", Limit: -1})
	harness.requireValidation(t, "negative limit", err)
}

func TestMyAllocations(t *testing.T) {
	t.Parallel()

	const body = `[{"symbol":"BTCUSDT","allocationId":0,"allocationType":"SOR","orderId":1,"orderListId":-1,"price":"1.00000000","qty":"5.00000000","quoteQty":"5.00000000","commission":"0.00000000","commissionAsset":"BTC","time":1687506878118,"isBuyer":true,"isMaker":false,"isAllocator":false}]`
	t.Run("order ID and allocation ID", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		orderID := int64(1)
		fromAllocationID := int64(0)
		allocations, err := harness.client.MyAllocations(context.Background(), MyAllocationsRequest{Symbol: "BTCUSDT", OrderID: &orderID, FromAllocationID: &fromAllocationID, Limit: 100})
		if err != nil {
			t.Fatalf("MyAllocations() error = %v", err)
		}
		if len(allocations) != 1 {
			t.Fatalf("MyAllocations() = %+v, want one allocation", allocations)
		}
		allocation := allocations[0]
		if allocation.Symbol != "BTCUSDT" || allocation.AllocationID != 0 || allocation.AllocationType != "SOR" || allocation.OrderID != 1 || allocation.OrderListID != -1 ||
			allocation.Price != "1.00000000" || allocation.Quantity != "5.00000000" || allocation.QuoteQuantity != "5.00000000" ||
			allocation.CommissionAsset != "BTC" || allocation.Time != 1687506878118 || !allocation.Buyer || allocation.Maker || allocation.Allocator || len(allocation.Raw) == 0 {
			t.Fatalf("MyAllocations() allocation = %+v", allocation)
		}
		harness.expect(t, http.MethodGet, "/api/v3/myAllocations", true, 20, 0, "symbol=BTCUSDT", "orderId=1", "fromAllocationId=0", "limit=100")
	})
	t.Run("time window", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, `[]`)
		start := time.UnixMilli(1687506878000)
		end := start.Add(24 * time.Hour)
		allocations, err := harness.client.MyAllocations(context.Background(), MyAllocationsRequest{Symbol: "BTCUSDT", StartTime: &start, EndTime: &end})
		if err != nil {
			t.Fatalf("MyAllocations() error = %v", err)
		}
		if len(allocations) != 0 {
			t.Fatalf("MyAllocations() = %+v, want empty", allocations)
		}
		harness.expect(t, http.MethodGet, "/api/v3/myAllocations", true, 20, 0, "symbol=BTCUSDT", "startTime=1687506878000", "endTime=1687593278000")
	})
}

func TestMyAllocationsValidation(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `[]`)
	ctx := context.Background()
	orderID := int64(1)
	zero := int64(0)
	negative := int64(-1)
	start := time.UnixMilli(1687506878000)
	end := start.Add(25 * time.Hour)

	_, err := harness.client.MyAllocations(ctx, MyAllocationsRequest{})
	harness.requireValidation(t, "missing symbol", err)
	_, err = harness.client.MyAllocations(ctx, MyAllocationsRequest{Symbol: "BTCUSDT", OrderID: &zero})
	harness.requireValidation(t, "zero orderId", err)
	_, err = harness.client.MyAllocations(ctx, MyAllocationsRequest{Symbol: "BTCUSDT", FromAllocationID: &negative})
	harness.requireValidation(t, "negative fromAllocationId", err)
	_, err = harness.client.MyAllocations(ctx, MyAllocationsRequest{Symbol: "BTCUSDT", OrderID: &orderID, StartTime: &start})
	harness.requireValidation(t, "orderId with startTime", err)
	_, err = harness.client.MyAllocations(ctx, MyAllocationsRequest{Symbol: "BTCUSDT", FromAllocationID: &zero, EndTime: &start})
	harness.requireValidation(t, "fromAllocationId with endTime", err)
	_, err = harness.client.MyAllocations(ctx, MyAllocationsRequest{Symbol: "BTCUSDT", StartTime: &start, EndTime: &end})
	harness.requireValidation(t, "window over 24 hours", err)
	_, err = harness.client.MyAllocations(ctx, MyAllocationsRequest{Symbol: "BTCUSDT", Limit: 1001})
	harness.requireValidation(t, "limit above 1000", err)
}

func TestMyPreventedMatches(t *testing.T) {
	t.Parallel()

	const body = `[{"symbol":"BTCUSDT","preventedMatchId":1,"takerOrderId":5,"makerSymbol":"BTCUSDT","makerOrderId":3,"tradeGroupId":1,"selfTradePreventionMode":"EXPIRE_MAKER","price":"1.100000","makerPreventedQuantity":"1.300000","transactTime":1669101687094}]`
	t.Run("by prevented match ID costs 2", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		preventedMatchID := int64(1)
		matches, err := harness.client.MyPreventedMatches(context.Background(), MyPreventedMatchesRequest{Symbol: "BTCUSDT", PreventedMatchID: &preventedMatchID})
		if err != nil {
			t.Fatalf("MyPreventedMatches() error = %v", err)
		}
		if len(matches) != 1 {
			t.Fatalf("MyPreventedMatches() = %+v, want one match", matches)
		}
		match := matches[0]
		if match.Symbol != "BTCUSDT" || match.PreventedMatchID != 1 || match.TakerOrderID != 5 || match.MakerSymbol != "BTCUSDT" || match.MakerOrderID != 3 ||
			match.TradeGroupID != 1 || match.SelfTradePreventionMode != "EXPIRE_MAKER" || match.Price != "1.100000" ||
			match.MakerPreventedQuantity != "1.300000" || match.TransactionTime != 1669101687094 || len(match.Raw) == 0 {
			t.Fatalf("MyPreventedMatches() match = %+v", match)
		}
		harness.expect(t, http.MethodGet, "/api/v3/myPreventedMatches", true, 2, 0, "symbol=BTCUSDT", "preventedMatchId=1")
	})
	t.Run("by order ID costs 20", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		orderID := int64(3)
		fromPreventedMatchID := int64(1)
		matches, err := harness.client.MyPreventedMatches(context.Background(), MyPreventedMatchesRequest{Symbol: "BTCUSDT", OrderID: &orderID, FromPreventedMatchID: &fromPreventedMatchID, Limit: 50})
		if err != nil {
			t.Fatalf("MyPreventedMatches() error = %v", err)
		}
		if len(matches) != 1 {
			t.Fatalf("MyPreventedMatches() = %+v, want one match", matches)
		}
		harness.expect(t, http.MethodGet, "/api/v3/myPreventedMatches", true, 20, 0, "symbol=BTCUSDT", "orderId=3", "fromPreventedMatchId=1", "limit=50")
	})
}

func TestMyPreventedMatchesValidation(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `[]`)
	ctx := context.Background()
	id := int64(1)
	zero := int64(0)
	negative := int64(-1)

	_, err := harness.client.MyPreventedMatches(ctx, MyPreventedMatchesRequest{OrderID: &id})
	harness.requireValidation(t, "missing symbol", err)
	_, err = harness.client.MyPreventedMatches(ctx, MyPreventedMatchesRequest{Symbol: "BTCUSDT"})
	harness.requireValidation(t, "no identifier", err)
	_, err = harness.client.MyPreventedMatches(ctx, MyPreventedMatchesRequest{Symbol: "BTCUSDT", PreventedMatchID: &id, OrderID: &id})
	harness.requireValidation(t, "both identifiers", err)
	_, err = harness.client.MyPreventedMatches(ctx, MyPreventedMatchesRequest{Symbol: "BTCUSDT", PreventedMatchID: &zero})
	harness.requireValidation(t, "zero preventedMatchId", err)
	_, err = harness.client.MyPreventedMatches(ctx, MyPreventedMatchesRequest{Symbol: "BTCUSDT", OrderID: &zero})
	harness.requireValidation(t, "zero orderId", err)
	_, err = harness.client.MyPreventedMatches(ctx, MyPreventedMatchesRequest{Symbol: "BTCUSDT", OrderID: &id, FromPreventedMatchID: &negative})
	harness.requireValidation(t, "negative fromPreventedMatchId", err)
	_, err = harness.client.MyPreventedMatches(ctx, MyPreventedMatchesRequest{Symbol: "BTCUSDT", PreventedMatchID: &id, FromPreventedMatchID: &id})
	harness.requireValidation(t, "fromPreventedMatchId with preventedMatchId", err)
	_, err = harness.client.MyPreventedMatches(ctx, MyPreventedMatchesRequest{Symbol: "BTCUSDT", PreventedMatchID: &id, Limit: 10})
	harness.requireValidation(t, "limit with preventedMatchId", err)
	_, err = harness.client.MyPreventedMatches(ctx, MyPreventedMatchesRequest{Symbol: "BTCUSDT", OrderID: &id, Limit: 1001})
	harness.requireValidation(t, "limit above 1000", err)
}

func TestAccountCommission(t *testing.T) {
	t.Parallel()

	const body = `{"symbol":"BTCUSDT","standardCommission":{"maker":"0.00000010","taker":"0.00000020","buyer":"0.00000030","seller":"0.00000040"},"specialCommission":{"maker":"0.01000000","taker":"0.02000000","buyer":"0.03000000","seller":"0.04000000"},"taxCommission":{"maker":"0.00000112","taker":"0.00000114","buyer":"0.00000118","seller":"0.00000116"},"discount":{"enabledForAccount":true,"enabledForSymbol":true,"discountAsset":"BNB","discount":"0.75000000"}}`
	t.Run("decodes rates and discount", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		commission, err := harness.client.AccountCommission(context.Background(), AccountCommissionRequest{Symbol: "BTCUSDT"})
		if err != nil {
			t.Fatalf("AccountCommission() error = %v", err)
		}
		if commission.Symbol != "BTCUSDT" || commission.StandardCommission.Maker != "0.00000010" || commission.StandardCommission.Seller != "0.00000040" ||
			commission.SpecialCommission.Taker != "0.02000000" || commission.TaxCommission.Buyer != "0.00000118" ||
			!commission.Discount.EnabledForAccount || !commission.Discount.EnabledForSymbol || commission.Discount.DiscountAsset != "BNB" ||
			commission.Discount.Discount != "0.75000000" || string(commission.Raw) != body {
			t.Fatalf("AccountCommission() = %+v", commission)
		}
		harness.expect(t, http.MethodGet, "/api/v3/account/commission", true, 20, 0, "symbol=BTCUSDT")
	})
	t.Run("requires symbol", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		_, err := harness.client.AccountCommission(context.Background(), AccountCommissionRequest{})
		harness.requireValidation(t, "missing symbol", err)
	})
}

func TestOrderRateLimits(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `[{"rateLimitType":"ORDERS","interval":"SECOND","intervalNum":10,"limit":50,"count":0},{"rateLimitType":"ORDERS","interval":"DAY","intervalNum":1,"limit":160000,"count":3}]`)
	limits, err := harness.client.OrderRateLimits(context.Background())
	if err != nil {
		t.Fatalf("OrderRateLimits() error = %v", err)
	}
	if len(limits) != 2 || limits[0].Type != "ORDERS" || limits[0].Interval != "SECOND" || limits[0].IntervalNum != 10 || limits[0].Limit != 50 || limits[0].Count != 0 ||
		limits[1].Interval != "DAY" || limits[1].IntervalNum != 1 || limits[1].Limit != 160000 || limits[1].Count != 3 {
		t.Fatalf("OrderRateLimits() = %+v", limits)
	}
	harness.expect(t, http.MethodGet, "/api/v3/rateLimit/order", true, 40, 0)
}

func TestOrderAmendments(t *testing.T) {
	t.Parallel()

	const body = `[{"symbol":"BTCUSDT","orderId":9,"executionId":22,"origClientOrderId":"W0fJ9fiLKHOJutovPK3oJp","newClientOrderId":"UQ1Np3bmQ71jJzsSDW9Vpi","origQty":"5.00000000","newQty":"4.00000000","time":1741669661670},{"symbol":"BTCUSDT","orderId":9,"executionId":25,"origClientOrderId":"UQ1Np3bmQ71jJzsSDW9Vpi","newClientOrderId":"5uS0r35ohuQyDlCzZuYXq2","origQty":"4.00000000","newQty":"3.00000000","time":1741672924895}]`
	harness := newExtensionHarness(t, http.StatusOK, body)
	fromExecutionID := int64(22)
	amendments, err := harness.client.OrderAmendments(context.Background(), OrderAmendmentsRequest{Symbol: "BTCUSDT", OrderID: 9, FromExecutionID: &fromExecutionID, Limit: 2})
	if err != nil {
		t.Fatalf("OrderAmendments() error = %v", err)
	}
	if len(amendments) != 2 {
		t.Fatalf("OrderAmendments() = %+v, want two amendments", amendments)
	}
	first, second := amendments[0], amendments[1]
	if first.Symbol != "BTCUSDT" || first.OrderID != 9 || first.ExecutionID != 22 || first.OriginalClientOrderID != "W0fJ9fiLKHOJutovPK3oJp" ||
		first.NewClientOrderID != "UQ1Np3bmQ71jJzsSDW9Vpi" || first.OriginalQuantity != "5.00000000" || first.NewQuantity != "4.00000000" || first.Time != 1741669661670 {
		t.Fatalf("OrderAmendments() first = %+v", first)
	}
	if second.ExecutionID != 25 || second.OriginalQuantity != "4.00000000" || second.NewQuantity != "3.00000000" || len(second.Raw) == 0 {
		t.Fatalf("OrderAmendments() second = %+v", second)
	}
	harness.expect(t, http.MethodGet, "/api/v3/order/amendments", true, 4, 0, "symbol=BTCUSDT", "orderId=9", "fromExecutionId=22", "limit=2")
}

func TestOrderAmendmentsValidation(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `[]`)
	ctx := context.Background()
	negative := int64(-1)

	_, err := harness.client.OrderAmendments(ctx, OrderAmendmentsRequest{OrderID: 9})
	harness.requireValidation(t, "missing symbol", err)
	_, err = harness.client.OrderAmendments(ctx, OrderAmendmentsRequest{Symbol: "BTCUSDT"})
	harness.requireValidation(t, "missing orderId", err)
	_, err = harness.client.OrderAmendments(ctx, OrderAmendmentsRequest{Symbol: "BTCUSDT", OrderID: 9, FromExecutionID: &negative})
	harness.requireValidation(t, "negative fromExecutionId", err)
	_, err = harness.client.OrderAmendments(ctx, OrderAmendmentsRequest{Symbol: "BTCUSDT", OrderID: 9, Limit: 1001})
	harness.requireValidation(t, "limit above 1000", err)
}

func TestMyFilters(t *testing.T) {
	t.Parallel()

	const body = `{"exchangeFilters":[{"filterType":"EXCHANGE_MAX_NUM_ORDERS","maxNumOrders":1000}],"symbolFilters":[{"filterType":"PRICE_FILTER","minPrice":"0.00000100","maxPrice":"100000.00000000","tickSize":"0.00000100"}],"assetFilters":[{"filterType":"MAX_ASSET","limit":"100.00000000","asset":"BNB"}],"rateLimits":[{"rateLimitType":"REQUEST_WEIGHT","interval":"MINUTE","intervalNum":1,"limit":6000,"count":321}]}`
	t.Run("decodes asset filters and keeps raw filters", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		filters, err := harness.client.MyFilters(context.Background(), MyFiltersRequest{Symbol: "BTCUSDT"})
		if err != nil {
			t.Fatalf("MyFilters() error = %v", err)
		}
		if len(filters.ExchangeFilters) != 1 || len(filters.SymbolFilters) != 1 || len(filters.AssetFilters) != 1 || len(filters.RateLimits) != 1 {
			t.Fatalf("MyFilters() = %+v", filters)
		}
		assetFilter := filters.AssetFilters[0]
		if assetFilter.FilterType != "MAX_ASSET" || assetFilter.Limit != "100.00000000" || assetFilter.Asset != "BNB" {
			t.Fatalf("MyFilters() asset filter = %+v", assetFilter)
		}
		if filters.RateLimits[0].Type != "REQUEST_WEIGHT" || filters.RateLimits[0].Limit != 6000 || filters.RateLimits[0].Count != 321 {
			t.Fatalf("MyFilters() rate limit = %+v", filters.RateLimits[0])
		}
		if string(filters.SymbolFilters[0]) != `{"filterType":"PRICE_FILTER","minPrice":"0.00000100","maxPrice":"100000.00000000","tickSize":"0.00000100"}` || string(filters.Raw) != body {
			t.Fatalf("MyFilters() did not preserve the raw filters: %s", filters.SymbolFilters[0])
		}
		harness.expect(t, http.MethodGet, "/api/v3/myFilters", true, 40, 0, "symbol=BTCUSDT")
	})
	t.Run("requires symbol", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		_, err := harness.client.MyFilters(context.Background(), MyFiltersRequest{})
		harness.requireValidation(t, "missing symbol", err)
	})
}

// TestAccountReadMethodsRequireReadPermission은 read 권한이 없는 자격증명으로 서명 조회 메서드가 요청 전에 거부되는지 확인한다.
func TestAccountReadMethodsRequireReadPermission(t *testing.T) {
	t.Parallel()

	harness := newPermissionHarness(t, http.StatusOK, `[]`, credential.PermissionTrade)
	ctx := context.Background()
	orderID := int64(9)
	requireAuthorization := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, trade.ErrAuthorization) {
			t.Errorf("%s error = %v, want ErrAuthorization", name, err)
		}
	}

	_, err := harness.client.MyTrades(ctx, MyTradesRequest{Symbol: "BNBBTC"})
	requireAuthorization("MyTrades", err)
	_, err = harness.client.MyAllocations(ctx, MyAllocationsRequest{Symbol: "BTCUSDT"})
	requireAuthorization("MyAllocations", err)
	_, err = harness.client.MyPreventedMatches(ctx, MyPreventedMatchesRequest{Symbol: "BTCUSDT", OrderID: &orderID})
	requireAuthorization("MyPreventedMatches", err)
	_, err = harness.client.AccountCommission(ctx, AccountCommissionRequest{Symbol: "BTCUSDT"})
	requireAuthorization("AccountCommission", err)
	_, err = harness.client.OrderRateLimits(ctx)
	requireAuthorization("OrderRateLimits", err)
	_, err = harness.client.OrderAmendments(ctx, OrderAmendmentsRequest{Symbol: "BTCUSDT", OrderID: orderID})
	requireAuthorization("OrderAmendments", err)
	_, err = harness.client.MyFilters(ctx, MyFiltersRequest{Symbol: "BTCUSDT"})
	requireAuthorization("MyFilters", err)
	_, err = harness.client.QueryOrderList(ctx, QueryOrderListRequest{OrderListID: &orderID})
	requireAuthorization("QueryOrderList", err)
	_, err = harness.client.AllOrderLists(ctx, AllOrderListsRequest{})
	requireAuthorization("AllOrderLists", err)
	_, err = harness.client.OpenOrderLists(ctx)
	requireAuthorization("OpenOrderLists", err)

	providerCalls, _, _ := harness.provider.snapshot()
	_, senderCalls := harness.sender.snapshot()
	if providerCalls != 0 || senderCalls != 0 || harness.requestCount() != 0 {
		t.Fatalf("provider calls = %d, sender calls = %d, requests = %d, want all zero", providerCalls, senderCalls, harness.requestCount())
	}
}
