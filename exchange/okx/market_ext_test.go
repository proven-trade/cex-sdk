package okx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

type publicAssetCase struct {
	name    string
	method  string
	path    string
	query   string
	body    string
	private bool
	reply   string
	call    func(*Client) error
}

// newPermissionedTestClient는 모든 권한을 가진 테스트 클라이언트를 만든다.
func newPermissionedTestClient(
	t *testing.T,
	baseURL string,
	permissions []credential.Permission,
	now func() time.Time,
) *Client {
	t.Helper()
	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: &directSender{}, Limiter: limiter})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	client, err := New(Config{
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "okx-main", Exchange: model.ExchangeOKX, SecretRef: "secret/okx-main",
			Permissions:           permissions,
			AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
		},
		CredentialProvider: &recordingProvider{}, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true, Now: now,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

func runPublicAssetCases(t *testing.T, cases []publicAssetCase) {
	t.Helper()
	fixedNow := time.UnixMilli(1_700_000_000_000)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gotMethod, gotPath, gotQuery, gotBody string
			var signed bool
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				gotMethod, gotPath, gotQuery = request.Method, request.URL.Path, request.URL.RawQuery
				if tc.private {
					signed = verifySignedRequest(t, request, []byte("test-secret"), fixedNow)
				}
				body, _ := io.ReadAll(request.Body)
				gotBody = string(body)
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, tc.reply)
			}))
			defer server.Close()
			client := newPermissionedTestClient(t, server.URL, []credential.Permission{
				credential.PermissionRead, credential.PermissionTransfer, credential.PermissionWithdraw,
			}, func() time.Time { return fixedNow })
			if err := tc.call(client); err != nil {
				t.Fatalf("call error = %v", err)
			}
			if gotMethod != tc.method || gotPath != tc.path {
				t.Fatalf("request = %s %s, want %s %s", gotMethod, gotPath, tc.method, tc.path)
			}
			wantQuery, _ := url.ParseQuery(tc.query)
			if gotValues, _ := url.ParseQuery(gotQuery); gotValues.Encode() != wantQuery.Encode() {
				t.Fatalf("query = %q, want %q", gotQuery, tc.query)
			}
			if gotBody != tc.body {
				t.Fatalf("body = %q, want %q", gotBody, tc.body)
			}
			if tc.private && !signed {
				t.Fatal("private request signature or auth headers are invalid")
			}
		})
	}
}

func TestPublicMarketExtensions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	after := time.UnixMilli(1_700_000_100_000)
	runPublicAssetCases(t, []publicAssetCase{
		{
			name: "mark price", method: http.MethodGet, path: "/api/v5/public/mark-price",
			query: "instType=SWAP&instId=BTC-USDT-SWAP",
			reply: `{"code":"0","msg":"","data":[{"instType":"SWAP","instId":"BTC-USDT-SWAP","markPx":"200","ts":"1597026383085"}]}`,
			call: func(c *Client) error {
				items, err := c.MarkPrices(ctx, MarkPriceRequest{InstrumentType: InstrumentTypeSwap, InstrumentID: "BTC-USDT-SWAP"})
				if err == nil && (len(items) != 1 || items[0].MarkPrice != "200" || len(items[0].Raw) == 0) {
					t.Errorf("MarkPrices() = %+v", items)
				}
				return err
			},
		},
		{
			name: "funding rate", method: http.MethodGet, path: "/api/v5/public/funding-rate", query: "instId=BTC-USDT-SWAP",
			reply: `{"code":"0","msg":"","data":[{"formulaType":"noRate","fundingRate":"0.0000182221218054","fundingTime":"1743609600000","instId":"BTC-USDT-SWAP","instType":"SWAP","maxFundingRate":"0.00375","method":"current_period","minFundingRate":"-0.00375","nextFundingRate":"","nextFundingTime":"1743638400000","premium":"0.00009","settFundingRate":"0.0000145","settState":"settled","ts":"1743588686291"}]}`,
			call: func(c *Client) error {
				items, err := c.FundingRates(ctx, FundingRateRequest{InstrumentID: "BTC-USDT-SWAP"})
				if err == nil && (len(items) != 1 || items[0].FundingRate != "0.0000182221218054" ||
					items[0].MinimumFundingRate != "-0.00375" || items[0].SettlementState != "settled") {
					t.Errorf("FundingRates() = %+v", items)
				}
				return err
			},
		},
		{
			name: "funding rate history", method: http.MethodGet, path: "/api/v5/public/funding-rate-history",
			query: "instId=BTC-USD-SWAP&after=1700000100000&limit=2",
			reply: `{"code":"0","msg":"","data":[{"formulaType":"noRate","fundingRate":"0.00007","fundingTime":"1703059200000","instId":"BTC-USD-SWAP","instType":"SWAP","method":"next_period","realizedRate":"0.00008"}]}`,
			call: func(c *Client) error {
				items, err := c.FundingRateHistory(ctx, FundingRateHistoryRequest{InstrumentID: "BTC-USD-SWAP", After: &after, Limit: 2})
				if err == nil && (len(items) != 1 || items[0].RealizedRate != "0.00008" || items[0].FundingTime != "1703059200000") {
					t.Errorf("FundingRateHistory() = %+v", items)
				}
				return err
			},
		},
		{
			name: "open interest", method: http.MethodGet, path: "/api/v5/public/open-interest", query: "instType=SWAP",
			reply: `{"code":"0","msg":"","data":[{"instType":"SWAP","instId":"BTC-USDT-SWAP","oi":"5000","oiCcy":"555.55","oiUsd":"50000","ts":"1597026383085"}]}`,
			call: func(c *Client) error {
				items, err := c.OpenInterests(ctx, OpenInterestRequest{InstrumentType: InstrumentTypeSwap})
				if err == nil && (len(items) != 1 || items[0].OpenInterestCoin != "555.55" || items[0].OpenInterestUSD != "50000") {
					t.Errorf("OpenInterests() = %+v", items)
				}
				return err
			},
		},
		{
			name: "price limit", method: http.MethodGet, path: "/api/v5/public/price-limit", query: "instId=BTC-USDT-SWAP",
			reply: `{"code":"0","msg":"","data":[{"instId":"BTC-USDT-SWAP","instType":"SWAP","buyLmt":"17057.9","sellLmt":"16388.9","ts":"1597026383085","enabled":true}]}`,
			call: func(c *Client) error {
				item, err := c.PriceLimits(ctx, "BTC-USDT-SWAP")
				if err == nil && (item.BuyLimit != "17057.9" || !item.Enabled) {
					t.Errorf("PriceLimits() = %+v", item)
				}
				return err
			},
		},
		{
			name: "books full", method: http.MethodGet, path: "/api/v5/market/books-full", query: "instId=BTC-USDT&sz=5000",
			reply: `{"code":"0","msg":"","data":[{"asks":[["411.8","10","0","4"]],"bids":[["410.5","3","0","1"]],"ts":"1597026383085"}]}`,
			call: func(c *Client) error {
				book, err := c.OrderBookFull(ctx, OrderBookFullRequest{InstrumentID: "BTC-USDT", Size: 5000})
				if err == nil && (len(book.Asks) != 1 || book.Asks[0].OrderCount != "4" || book.Bids[0].Price != "410.5") {
					t.Errorf("OrderBookFull() = %+v", book)
				}
				return err
			},
		},
		{
			name: "index tickers", method: http.MethodGet, path: "/api/v5/market/index-tickers", query: "quoteCcy=USDT",
			reply: `{"code":"0","msg":"","data":[{"instId":"BTC-USDT","idxPx":"43350","high24h":"43649.7","sodUtc0":"43444.1","open24h":"43640.8","low24h":"43261.9","sodUtc8":"43328.7","ts":"1649419644492"}]}`,
			call: func(c *Client) error {
				items, err := c.IndexTickers(ctx, IndexTickersRequest{QuoteCurrency: "USDT"})
				if err == nil && (len(items) != 1 || items[0].IndexPrice != "43350" || items[0].StartUTC8 != "43328.7") {
					t.Errorf("IndexTickers() = %+v", items)
				}
				return err
			},
		},
		{
			name: "index candles", method: http.MethodGet, path: "/api/v5/market/index-candles", query: "instId=BTC-USD&bar=1H&limit=100",
			reply: `{"code":"0","msg":"","data":[["1597026383085","3.721","3.743","3.677","3.708","0"],["1597026383084","3.731","3.799","3.494","3.72","1"]]}`,
			call: func(c *Client) error {
				items, err := c.IndexCandles(ctx, PriceCandlesRequest{InstrumentID: "BTC-USD", Interval: Candle1Hour, Limit: 100})
				if err == nil && (len(items) != 2 || items[0].Confirmed || !items[1].Confirmed || items[1].High != "3.799") {
					t.Errorf("IndexCandles() = %+v", items)
				}
				return err
			},
		},
		{
			name: "history index candles", method: http.MethodGet, path: "/api/v5/market/history-index-candles", query: "instId=BTC-USD",
			reply: `{"code":"0","msg":"","data":[["1597026383085","3.721","3.743","3.677","3.708","1"]]}`,
			call: func(c *Client) error {
				_, err := c.HistoryIndexCandles(ctx, PriceCandlesRequest{InstrumentID: "BTC-USD"})
				return err
			},
		},
		{
			name: "mark price candles", method: http.MethodGet, path: "/api/v5/market/mark-price-candles", query: "instId=BTC-USD-SWAP&before=1700000100000",
			reply: `{"code":"0","msg":"","data":[["1597026383085","3.721","3.743","3.677","3.708","1"]]}`,
			call: func(c *Client) error {
				_, err := c.MarkPriceCandles(ctx, PriceCandlesRequest{InstrumentID: "BTC-USD-SWAP", Before: &after})
				return err
			},
		},
		{
			name: "history mark price candles", method: http.MethodGet, path: "/api/v5/market/history-mark-price-candles", query: "instId=BTC-USD-SWAP",
			reply: `{"code":"0","msg":"","data":[["1597026383085","3.721","3.743","3.677","3.708","1"]]}`,
			call: func(c *Client) error {
				_, err := c.HistoryMarkPriceCandles(ctx, PriceCandlesRequest{InstrumentID: "BTC-USD-SWAP"})
				return err
			},
		},
		{
			name: "premium history", method: http.MethodGet, path: "/api/v5/public/premium-history", query: "instId=BTC-USDT-SWAP&limit=1",
			reply: `{"code":"0","msg":"","data":[{"instId":"BTC-USDT-SWAP","premium":"0.0000578896878167","ts":"1713925924000"}]}`,
			call: func(c *Client) error {
				items, err := c.PremiumHistory(ctx, PremiumHistoryRequest{InstrumentID: "BTC-USDT-SWAP", Limit: 1})
				if err == nil && (len(items) != 1 || items[0].Premium != "0.0000578896878167") {
					t.Errorf("PremiumHistory() = %+v", items)
				}
				return err
			},
		},
		{
			name: "estimated price", method: http.MethodGet, path: "/api/v5/public/estimated-price", query: "instId=BTC-USDT-201227",
			reply: `{"code":"0","msg":"","data":[{"instType":"FUTURES","instId":"BTC-USDT-201227","settlePx":"200","ts":"1597026383085"}]}`,
			call: func(c *Client) error {
				item, err := c.EstimatedPrices(ctx, "BTC-USDT-201227")
				if err == nil && item.SettlePrice != "200" {
					t.Errorf("EstimatedPrices() = %+v", item)
				}
				return err
			},
		},
		{
			name: "discount rate", method: http.MethodGet, path: "/api/v5/public/discount-rate-interest-free-quota", query: "ccy=BTC",
			reply: `{"code":"0","msg":"","data":[{"amt":"0","ccy":"BTC","colRes":"0","details":[{"discountRate":"0.98","liqPenaltyRate":"0.02","maxAmt":"20","minAmt":"0","tier":"1","disCcyEq":"1000"}],"minDiscountRate":"0"}]}`,
			call: func(c *Client) error {
				items, err := c.DiscountRateInterestFreeQuota(ctx, "BTC")
				if err == nil && (len(items) != 1 || len(items[0].Details) != 1 || items[0].Details[0].DiscountRate != "0.98") {
					t.Errorf("DiscountRateInterestFreeQuota() = %+v", items)
				}
				return err
			},
		},
		{
			name: "interest rate loan quota", method: http.MethodGet, path: "/api/v5/public/interest-rate-loan-quota",
			reply: `{"code":"0","msg":"","data":[{"basic":[{"ccy":"USDT","quota":"500000","rate":"0.00043728"}],"vip":[{"irDiscount":"","loanQuotaCoef":"6","level":"VIP1"}],"regular":[{"irDiscount":"","loanQuotaCoef":"1","level":"Lv1"}]}]}`,
			call: func(c *Client) error {
				item, err := c.InterestRateLoanQuotas(ctx)
				if err == nil && (len(item.Basic) != 1 || item.Basic[0].Rate != "0.00043728" || item.VIP[0].LoanQuotaCoefficient != "6") {
					t.Errorf("InterestRateLoanQuotas() = %+v", item)
				}
				return err
			},
		},
		{
			name: "position tiers", method: http.MethodGet, path: "/api/v5/public/position-tiers",
			query: "instType=SWAP&tdMode=cross&instFamily=BTC-USDT",
			reply: `{"code":"0","msg":"","data":[{"baseMaxLoan":"","imr":"0.01","instId":"","instFamily":"BTC-USDT","maxLever":"100","maxSz":"50","minSz":"0","mmr":"0.004","optMgnFactor":"0","quoteMaxLoan":"","tier":"1","uly":"BTC-USDT"}]}`,
			call: func(c *Client) error {
				items, err := c.PositionTiers(ctx, PositionTiersRequest{
					InstrumentType: InstrumentTypeSwap, TradeMode: TradeModeCross, InstrumentFamily: "BTC-USDT",
				})
				if err == nil && (len(items) != 1 || items[0].MaximumLeverage != "100" || items[0].MaintenanceRate != "0.004") {
					t.Errorf("PositionTiers() = %+v", items)
				}
				return err
			},
		},
		{
			name: "insurance fund", method: http.MethodGet, path: "/api/v5/public/insurance-fund",
			query: "instType=SWAP&instFamily=BTC-USDT&limit=1",
			reply: `{"code":"0","msg":"","data":[{"details":[{"adlType":"","amt":"1343.1308","balance":"1369179138.7489","ccy":"ETH","maxBal":"","maxBalTs":"","ts":"1704883083000","type":"liquidation_balance_deposit"}],"instFamily":"BTC-USDT","instType":"SWAP","total":"1369179138.7489"}]}`,
			call: func(c *Client) error {
				items, err := c.InsuranceFunds(ctx, InsuranceFundRequest{
					InstrumentType: InstrumentTypeSwap, InstrumentFamily: "BTC-USDT", Limit: 1,
				})
				if err == nil && (len(items) != 1 || len(items[0].Details) != 1 || items[0].Details[0].Type != "liquidation_balance_deposit") {
					t.Errorf("InsuranceFunds() = %+v", items)
				}
				return err
			},
		},
		{
			name: "system status", method: http.MethodGet, path: "/api/v5/system/status", query: "state=ongoing",
			reply: `{"code":"0","msg":"","data":[{"title":"Spot trading maintenance","state":"ongoing","begin":"1672823400000","end":"1672823520000","href":"","serviceType":"1","system":"classic","scheDesc":"","maintType":"1","env":"1"}]}`,
			call: func(c *Client) error {
				items, err := c.SystemStatus(ctx, SystemStatusRequest{State: "ongoing"})
				if err == nil && (len(items) != 1 || items[0].State != "ongoing" || items[0].MaintType != "1") {
					t.Errorf("SystemStatus() = %+v", items)
				}
				return err
			},
		},
		{
			name: "announcements", method: http.MethodGet, path: "/api/v5/support/announcements", query: "annType=announcements-latest-announcements&page=2",
			reply: `{"code":"0","msg":"","data":[{"details":[{"annType":"announcements-latest-announcements","pTime":"1672823400000","businessPTime":"1672823400001","title":"Listing","url":"https://www.okx.com/help/x"}],"totalPage":"5"}]}`,
			call: func(c *Client) error {
				page, err := c.Announcements(ctx, AnnouncementsRequest{AnnouncementType: "announcements-latest-announcements", Page: 2})
				if err == nil && (page.TotalPage != "5" || len(page.Details) != 1 || page.Details[0].Title != "Listing") {
					t.Errorf("Announcements() = %+v", page)
				}
				return err
			},
		},
		{
			name: "announcement types", method: http.MethodGet, path: "/api/v5/support/announcement-types",
			reply: `{"code":"0","msg":"","data":[{"annType":"announcements-new-listings","annTypeDesc":"New listings"}]}`,
			call: func(c *Client) error {
				items, err := c.AnnouncementTypes(ctx)
				if err == nil && (len(items) != 1 || items[0].Type != "announcements-new-listings") {
					t.Errorf("AnnouncementTypes() = %+v", items)
				}
				return err
			},
		},
	})
}

func TestMarketExtensionValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := newPermissionedTestClient(t, "http://127.0.0.1:1", []credential.Permission{credential.PermissionRead},
		time.Now)
	before, after := time.UnixMilli(2000), time.UnixMilli(1000)
	cases := []struct {
		name string
		call func() error
	}{
		{"mark price spot", func() error {
			_, err := client.MarkPrices(ctx, MarkPriceRequest{InstrumentType: InstrumentTypeSpot})
			return err
		}},
		{"funding rate empty", func() error {
			_, err := client.FundingRates(ctx, FundingRateRequest{})
			return err
		}},
		{"funding history limit", func() error {
			_, err := client.FundingRateHistory(ctx, FundingRateHistoryRequest{InstrumentID: "X", Limit: 401})
			return err
		}},
		{"open interest option family", func() error {
			_, err := client.OpenInterests(ctx, OpenInterestRequest{InstrumentType: InstrumentTypeOption})
			return err
		}},
		{"books full depth", func() error {
			_, err := client.OrderBookFull(ctx, OrderBookFullRequest{InstrumentID: "BTC-USDT", Size: 5001})
			return err
		}},
		{"index tickers empty", func() error {
			_, err := client.IndexTickers(ctx, IndexTickersRequest{})
			return err
		}},
		{"price candles order", func() error {
			_, err := client.IndexCandles(ctx, PriceCandlesRequest{InstrumentID: "BTC-USD", After: &after, Before: &before})
			return err
		}},
		{"price candles limit", func() error {
			_, err := client.MarkPriceCandles(ctx, PriceCandlesRequest{InstrumentID: "BTC-USD", Limit: 101})
			return err
		}},
		{"position tiers mode", func() error {
			_, err := client.PositionTiers(ctx, PositionTiersRequest{InstrumentType: InstrumentTypeSwap, InstrumentFamily: "BTC-USDT"})
			return err
		}},
		{"position tiers family", func() error {
			_, err := client.PositionTiers(ctx, PositionTiersRequest{InstrumentType: InstrumentTypeSwap, TradeMode: TradeModeCross})
			return err
		}},
		{"insurance fund family", func() error {
			_, err := client.InsuranceFunds(ctx, InsuranceFundRequest{InstrumentType: InstrumentTypeSwap})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.call(); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
		})
	}
}
