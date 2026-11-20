package htx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
)

func extBadRequest(writer http.ResponseWriter) {
	http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
}

func TestClientPublicMarketReferenceExtensions(t *testing.T) {
	t.Parallel()
	updatedSince := time.UnixMilli(1_700_000_000_000)
	authorized := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		query := request.URL.Query()
		if request.Method != http.MethodGet || request.Header.Get("Accept") != "application/json" ||
			request.Header.Get("User-Agent") != "cex-sdk-go/0" || query.Get("Signature") != "" {
			http.Error(writer, `{"status":"error","err-code":"invalid-request"}`, http.StatusBadRequest)
			return
		}
		switch request.URL.Path {
		case "/market/detail":
			if query.Get("symbol") != "btcusdt" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"ch":"market.btcusdt.detail","status":"ok","ts":1629795484817,"tick":{"id":272164011416,"low":48767.0,"high":50500.0,"open":50266.89,"close":49728.71,"vol":6.010379336834868E8,"amount":12110.642402972368,"version":272164011416,"count":420452}}`)
		case "/v2/market-status":
			_, _ = io.WriteString(writer, `{"code":200,"message":"success","data":{"marketStatus":3,"haltStartTime":1630000000000,"haltEndTime":1630003600000,"haltReason":3,"affectedSymbols":"btcusdt,ethusdt"}}`)
		case "/v2/settings/common/symbols":
			if query.Get("ts") != "1700000000000" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"tags":"","state":"online","wr":"1.5","sc":"ethusdt","p":[{"id":9,"name":"Grayscale","weight":91}],"bcdn":"ETH","qcdn":"USDT","elr":null,"tpp":2,"tap":4,"fp":8,"smlr":null,"flr":null,"whe":false,"cd":false,"te":true,"sp":"main","d":null,"bc":"eth","qc":"usdt","toa":1514779200000,"ttp":8,"w":999400000,"lr":5,"dn":"ETH/USDT"}],"ts":"1641870869718","full":1}`)
		case "/v1/settings/common/symbols":
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"symbol":"agldusdt","tags":"","state":"online","bcdn":"AGLD","qcdn":"USDT","elr":null,"tm":"PRO","sn":"AGLD/USDT","ve":true,"dl":false,"te":true,"ce":true,"cd":false,"tet":1630668600000,"we":false,"toa":1630668600000,"tca":1893470400000,"voa":1630666800000,"vca":1893470400000,"bc":"agld","qc":"usdt","sp":"innovation","d":null,"tpp":4,"tap":4,"fp":8,"w":950000000,"ttp":8}],"ts":"1641880066563","full":0}`)
		case "/v2/settings/common/currencies":
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"tags":"","cawt":false,"fc":12,"sc":12,"dma":"1","wma":"10","ft":"eth","whe":false,"cd":false,"qc":true,"sp":"8","wp":6,"fn":"Tether USDT","at":1,"cc":"usdt","v":true,"de":true,"wed":true,"w":10006,"state":"online","dn":"USDT","dd":"deposit notice","svd":null,"swd":null,"sdd":null,"wd":"withdraw notice"}],"ts":"1641869938436","full":1}`)
		case "/v1/settings/common/currencys":
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"tags":"","name":"usdt","state":"online","cawt":false,"fc":12,"sc":12,"sp":"8","iqc":true,"ct":"eth","de":true,"we":true,"cd":false,"oe":1,"v":true,"whe":false,"wet":1609430400000,"det":1609430400000,"cp":"all","vat":1508839200000,"ss":["INSTITUTION","MINEPOOL","OTC"],"fn":"Tether USDT","wp":6,"w":10006,"dma":"1","wma":"10","dn":"USDT","dd":"deposit notice","svd":null,"swd":null,"sdd":null,"wd":"withdraw notice"}],"ts":"1641872721891","full":1}`)
		case "/v1/settings/common/chains":
			if query.Get("currency") != "nft" || query.Get("show-desc") != "1" ||
				query.Get("ts") != "1700000000000" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"chain":"hrc20nft","currency":"nft","code":"hrc20nft","ct":"live","ac":"eth","default":0,"dma":"160298","wma":"160298","de":true,"we":true,"wp":6,"ft":"eth","dn":"HECO","fn":"","awt":false,"adt":false,"ao":false,"fc":10,"sc":20,"v":true,"sda":"","swa":"","deposit-tips-desc":"Minimum deposit amount:160298","withdraw-desc":"Minimum withdrawal amount: 160298 NFT(HECO).","suspend-deposit-desc":"","suspend-withdraw-desc":"","replace-chain-info-desc":""}],"ts":"1641880897191","full":1}`)
		case "/v2/reference/currencies":
			if query.Get("currency") != "usdt" || query.Get("authorizedUser") != "false" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"code":200,"data":[{"chains":[{"chain":"trc20usdt","displayName":"","baseChain":"TRX","baseChainProtocol":"TRC20","isDynamic":false,"depositStatus":"allowed","maxTransactFeeWithdraw":"1.00000000","maxWithdrawAmt":"280000.00000000","minDepositAmt":"100","minTransactFeeWithdraw":"0.10000000","minWithdrawAmt":"0.01","numOfConfirmations":999,"numOfFastConfirmations":999,"withdrawFeeType":"circulated","withdrawPrecision":5,"withdrawQuotaPerDay":"280000.00000000","withdrawQuotaPerYear":"2800000.00000000","withdrawQuotaTotal":"2800000.00000000","withdrawStatus":"allowed"},{"chain":"usdt","displayName":"","baseChain":"BTC","baseChainProtocol":"OMNI","isDynamic":false,"depositStatus":"allowed","maxWithdrawAmt":"19000.00000000","minDepositAmt":"0.0001","minWithdrawAmt":"2","numOfConfirmations":30,"numOfFastConfirmations":15,"transactFeeRateWithdraw":"0.00100000","withdrawFeeType":"ratio","withdrawPrecision":7,"withdrawQuotaPerDay":"90000.00000000","withdrawQuotaPerYear":"111000.00000000","withdrawQuotaTotal":"1110000.00000000","withdrawStatus":"allowed"},{"chain":"usdterc20","displayName":"","baseChain":"ETH","baseChainProtocol":"ERC20","isDynamic":true,"depositStatus":"prohibited","maxWithdrawAmt":"18000.00000000","minDepositAmt":"100","minWithdrawAmt":"1","numOfConfirmations":999,"numOfFastConfirmations":999,"transactFeeWithdraw":"0.10000000","withdrawFeeType":"fixed","withdrawPrecision":6,"withdrawQuotaPerDay":"180000.00000000","withdrawQuotaPerYear":"200000.00000000","withdrawQuotaTotal":"300000.00000000","withdrawStatus":"prohibited"}],"currency":"usdt","instStatus":"normal"}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	client, limiter := newTestClient(t, server.URL, sender)
	ctx := context.Background()

	detail, err := client.MarketDetail(ctx, "btcusdt", trade.WithEgressRoute("route-b"))
	if err != nil || detail.Channel != "market.btcusdt.detail" || detail.ID != "272164011416" ||
		detail.Open != "50266.89" || detail.Low != "48767.0" || detail.BaseVolume != "12110.642402972368" ||
		detail.QuoteVolume != "6.010379336834868E8" || detail.Count != 420452 ||
		detail.Timestamp != 1_629_795_484_817 || len(detail.Raw) == 0 {
		t.Fatalf("MarketDetail() = %+v, error = %v", detail, err)
	}
	status, err := client.MarketStatus(ctx)
	if err != nil || status.Status != MarketStatusCancelOnly || status.HaltStartTime != 1_630_000_000_000 ||
		status.HaltEndTime != 1_630_003_600_000 || status.HaltReason != 3 ||
		status.AffectedSymbols != "btcusdt,ethusdt" || len(status.Raw) == 0 {
		t.Fatalf("MarketStatus() = %+v, error = %v", status, err)
	}
	symbolsV2, err := client.SymbolSettingsV2(ctx, SettingsRequest{UpdatedSince: &updatedSince})
	if err != nil || len(symbolsV2.Symbols) != 1 || symbolsV2.UpdatedAt != "1641870869718" ||
		symbolsV2.Full != 1 || len(symbolsV2.Raw) == 0 {
		t.Fatalf("SymbolSettingsV2() = %+v, error = %v", symbolsV2, err)
	}
	symbolV2 := symbolsV2.Symbols[0]
	if symbolV2.Symbol != "ethusdt" || symbolV2.DisplayName != "ETH/USDT" || !symbolV2.TradeEnabled ||
		symbolV2.TradeTotalPrecision != 8 || symbolV2.TradeAmountPrecision != 4 ||
		symbolV2.TradePricePrecision != 2 || symbolV2.LeverageRatio != "5" ||
		symbolV2.WithdrawRisk != "1.5" || symbolV2.ETPLeverageRatio != "" ||
		symbolV2.Weight != "999400000" || len(symbolV2.Partitions) != 1 ||
		symbolV2.Partitions[0].Name != "Grayscale" || symbolV2.Partitions[0].Weight != "91" ||
		len(symbolV2.Raw) == 0 {
		t.Fatalf("SymbolSettingsV2() item = %+v", symbolV2)
	}
	symbols, err := client.SymbolSettings(ctx, SettingsRequest{})
	if err != nil || len(symbols.Symbols) != 1 || symbols.Full != 0 || symbols.UpdatedAt != "1641880066563" {
		t.Fatalf("SymbolSettings() = %+v, error = %v", symbols, err)
	}
	symbol := symbols.Symbols[0]
	if symbol.Symbol != "agldusdt" || symbol.Name != "AGLD/USDT" || !symbol.CancelEnabled ||
		!symbol.VisibleEnabled || symbol.TradeOpenAt != 1_630_668_600_000 ||
		symbol.TradeCloseAt != 1_893_470_400_000 || symbol.Direction != "" ||
		symbol.Partition != "innovation" || symbol.TradeMarket != "PRO" || len(symbol.Raw) == 0 {
		t.Fatalf("SymbolSettings() item = %+v", symbol)
	}
	currenciesV2, err := client.CurrencySettingsV2(ctx, SettingsRequest{})
	if err != nil || len(currenciesV2.Currencies) != 1 {
		t.Fatalf("CurrencySettingsV2() = %+v, error = %v", currenciesV2, err)
	}
	currencyV2 := currenciesV2.Currencies[0]
	if currencyV2.Currency != "usdt" || currencyV2.AssetType != 1 || !currencyV2.QuoteCurrency ||
		!currencyV2.WithdrawEnabled || currencyV2.DepositMinimumAmount != "1" ||
		currencyV2.WithdrawMinimumAmount != "10" || currencyV2.Weight != "10006" ||
		currencyV2.SuspendDepositDescription != "" || currencyV2.DepositDescription != "deposit notice" ||
		len(currencyV2.Raw) == 0 {
		t.Fatalf("CurrencySettingsV2() item = %+v", currencyV2)
	}
	currencies, err := client.CurrencySettings(ctx, SettingsRequest{})
	if err != nil || len(currencies.Currencies) != 1 {
		t.Fatalf("CurrencySettings() = %+v, error = %v", currencies, err)
	}
	currency := currencies.Currencies[0]
	if currency.Name != "usdt" || !currency.QuoteCurrency || !currency.WithdrawEnabled ||
		currency.OTCEnabled != 1 || currency.CurrencyPartition != "all" ||
		!slices.Equal(currency.SupportSites, []string{"INSTITUTION", "MINEPOOL", "OTC"}) ||
		currency.WithdrawEnableTime != 1_609_430_400_000 || currency.VisibleAssetsTime != 1_508_839_200_000 ||
		len(currency.Raw) == 0 {
		t.Fatalf("CurrencySettings() item = %+v", currency)
	}
	chains, err := client.ChainSettings(ctx, ChainSettingsRequest{
		Currency: "nft", ShowDescription: ChainDescriptionAll, UpdatedSince: &updatedSince,
	})
	if err != nil || len(chains.Chains) != 1 || chains.UpdatedAt != "1641880897191" {
		t.Fatalf("ChainSettings() = %+v, error = %v", chains, err)
	}
	chain := chains.Chains[0]
	if chain.Chain != "hrc20nft" || chain.Currency != "nft" || chain.AddressChain != "eth" ||
		chain.IsDefault != 0 || chain.DepositMinimumAmount != "160298" || !chain.DepositEnabled ||
		!chain.WithdrawEnabled || chain.FastConfirmations != 10 || chain.SafeConfirmations != 20 ||
		chain.DisplayName != "HECO" || chain.DepositTipsDescription == "" || len(chain.Raw) == 0 {
		t.Fatalf("ChainSettings() item = %+v", chain)
	}
	references, err := client.ReferenceCurrencies(ctx, ReferenceCurrenciesRequest{
		Currency: "usdt", AuthorizedUser: &authorized,
	})
	if err != nil || len(references) != 1 || references[0].Currency != "usdt" ||
		references[0].InstrumentStatus != "normal" || len(references[0].Chains) != 3 ||
		len(references[0].Raw) == 0 {
		t.Fatalf("ReferenceCurrencies() = %+v, error = %v", references, err)
	}
	circulated, ratio, fixed := references[0].Chains[0], references[0].Chains[1], references[0].Chains[2]
	if circulated.BaseChainProtocol != "TRC20" || circulated.WithdrawFeeType != "circulated" ||
		circulated.MinTransactFeeWithdraw != "0.10000000" || circulated.MaxTransactFeeWithdraw != "1.00000000" ||
		circulated.Confirmations != 999 || circulated.WithdrawPrecision != 5 ||
		circulated.WithdrawQuotaPerDay != "280000.00000000" || len(circulated.Raw) == 0 {
		t.Fatalf("ReferenceCurrencies() circulated chain = %+v", circulated)
	}
	if ratio.WithdrawFeeType != "ratio" || ratio.TransactFeeRateWithdraw != "0.00100000" ||
		ratio.FastConfirmations != 15 || ratio.MinTransactFeeWithdraw != "" {
		t.Fatalf("ReferenceCurrencies() ratio chain = %+v", ratio)
	}
	if fixed.WithdrawFeeType != "fixed" || fixed.TransactFeeWithdraw != "0.10000000" || !fixed.IsDynamic ||
		fixed.DepositStatus != "prohibited" || fixed.WithdrawStatus != "prohibited" {
		t.Fatalf("ReferenceCurrencies() fixed chain = %+v", fixed)
	}

	routes := sender.snapshot()
	if len(routes) != 8 || routes[0] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	for _, route := range routes[1:] {
		if route != "route-a" {
			t.Fatalf("default sender route = %q, want route-a", route)
		}
	}
	if snapshot, snapshotErr := limiter.Snapshot("htx:route:route-b:public:market-detail:1second"); snapshotErr != nil ||
		snapshot.Used != 1 || snapshot.Rule.Limit != DefaultPublicRequestsPerSecond {
		t.Fatalf("market-detail limiter snapshot = %+v, error = %v", snapshot, snapshotErr)
	}
	if snapshot, snapshotErr := limiter.Snapshot("htx:route:route-a:public:reference-currencies:1second"); snapshotErr != nil ||
		snapshot.Used != 1 {
		t.Fatalf("reference-currencies limiter snapshot = %+v, error = %v", snapshot, snapshotErr)
	}
}

func TestClientMarketStatusAPIError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"code":500,"message":"error"}`)
	}))
	defer server.Close()
	client, _ := newTestClient(t, server.URL, &directSender{})
	if _, err := client.MarketStatus(context.Background()); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("MarketStatus() error = %v, want exchange unavailable", err)
	}
	var apiError *trade.APIError
	_, err := client.MarketStatus(context.Background())
	if !errors.As(err, &apiError) || apiError.ExchangeCode != "500" || apiError.ExchangeMessage != "error" {
		t.Fatalf("MarketStatus() API error = %+v", apiError)
	}
}

func TestMarketReferenceExtensionValidation(t *testing.T) {
	t.Parallel()
	beforeEpoch := time.UnixMilli(0)
	tests := []struct {
		name string
		err  error
	}{
		{name: "settings time", err: (SettingsRequest{UpdatedSince: &beforeEpoch}).validate()},
		{name: "chain currency", err: (ChainSettingsRequest{Currency: "BTC"}).validate()},
		{name: "chain description", err: (ChainSettingsRequest{ShowDescription: "3"}).validate()},
		{name: "chain time", err: (ChainSettingsRequest{UpdatedSince: &beforeEpoch}).validate()},
		{name: "reference currency", err: (ReferenceCurrenciesRequest{Currency: "usdt/trc20"}).validate()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, trade.ErrValidation) {
				t.Fatalf("validation error = %v", test.err)
			}
		})
	}
	client, _ := newTestClient(t, "http://127.0.0.1", &directSender{})
	if _, err := client.MarketDetail(context.Background(), "BTC-USDT"); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("MarketDetail() symbol error = %v, want validation", err)
	}
}
