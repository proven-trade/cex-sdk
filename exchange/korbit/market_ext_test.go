package korbit

import (
	"context"
	"errors"
	"net/http"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
)

const currenciesSample = `{"success":true,"data":[` +
	`{"name":"krw","fullName":"Won","withdrawalMaxAmountPerRequest":"5000000000","depositStatus":"launched","withdrawalStatus":"launched","withdrawalTxFee":"1000","withdrawalMinAmount":"1000"},` +
	`{"name":"btc","fullName":"Bitcoin","withdrawalMaxAmountPerRequest":"120","defaultNetwork":"BTC","networkList":[{"name":"BTC","fullName":"Bitcoin","depositStatus":"launched","withdrawalStatus":"stopped","confirmationCount":3,"withdrawalTxFee":"0.0008","withdrawalMinAmount":"0.0001","withdrawalPrecision":8,"hasSecondaryAddr":false,"addressExplorerUrl":"https://www.blockchain.com/ko/btc/address/"}],"depositStatus":"launched","withdrawalStatus":"launched","confirmationCount":"3","withdrawalTxFee":"0.0008","withdrawalMinAmount":"0.0001"},` +
	`{"name":"xrp","fullName":"XRP","withdrawalMaxAmountPerRequest":"1000000","defaultNetwork":"XRP","networkList":[{"name":"XRP","fullName":"XRP Ledger","depositStatus":"launched","withdrawalStatus":"launched","confirmationCount":1,"withdrawalTxFee":"1","withdrawalMinAmount":"20","withdrawalPrecision":6,"hasSecondaryAddr":true,"contractAddress":null,"addressExplorerUrl":null}],"confirmationCount":null}` +
	`]}`

const noticesSample = `{"success":true,"data":[` +
	`{"title":"공지사항 제목 샘플","createdAt":1700000000000,"updatedAt":1700000000500,"url":"https://digitalx.miraeasset.com/notice/detail/?noticeId=4Oy9q6ALiM7jABzMt32ul5"},` +
	`{"title":"Open API 점검 안내","createdAt":1699990000000,"url":"https://digitalx.miraeasset.com/notice/detail/?noticeId=abc"}` +
	`]}`

const marketAlertsSample = `{"success":true,"data":[{"symbol":"btc_krw","alerts":[` +
	`{"type":"price","typeKorean":"가격 급등락","level":"danger","levelKorean":"투자위험","startAt":1700000000000,"endAt":1700000100000,"value":"250.5"},` +
	`{"type":"limit_buy_range","typeKorean":"주문가격 제한","startAt":1700000000000,"endAt":null,"value":"145000000"}` +
	`]}]}`

func TestPublicMarketExtensionEndpoints(t *testing.T) {
	t.Parallel()

	server, recorder := newKorbitStubServer(t, map[string]string{
		"GET /v2/currencies":   currenciesSample,
		"GET /v2/notices":      noticesSample,
		"GET /v2/marketAlerts": marketAlertsSample,
	})
	client, limiter := newPublicTestClient(t, server.URL)
	ctx := context.Background()

	currencies, err := client.Currencies(ctx)
	if err != nil || len(currencies) != 3 {
		t.Fatalf("Currencies() = %+v, error = %v", currencies, err)
	}
	krw, btc, xrp := currencies[0], currencies[1], currencies[2]
	if krw.Name != "krw" || krw.FullName != "Won" || krw.NetworkList != nil || krw.WithdrawalMinAmount != "1000" ||
		krw.WithdrawalMaxAmountPerRequest != "5000000000" || krw.DepositStatus != CurrencyStatusLaunched ||
		krw.WithdrawalTxFee != "1000" || len(krw.Raw) == 0 {
		t.Fatalf("krw currency = %+v", krw)
	}
	if btc.Name != "btc" || btc.DefaultNetwork != "BTC" || btc.ConfirmationCount != "3" ||
		btc.WithdrawalStatus != CurrencyStatusLaunched || len(btc.NetworkList) != 1 || len(btc.Raw) == 0 {
		t.Fatalf("btc currency = %+v", btc)
	}
	network := btc.NetworkList[0]
	if network.Name != "BTC" || network.FullName != "Bitcoin" || network.DepositStatus != CurrencyStatusLaunched ||
		network.WithdrawalStatus != CurrencyStatusStopped || network.ConfirmationCount != 3 ||
		network.WithdrawalTxFee != "0.0008" || network.WithdrawalMinAmount != "0.0001" ||
		network.WithdrawalPrecision != 8 || network.HasSecondaryAddress ||
		network.AddressExplorerURL != "https://www.blockchain.com/ko/btc/address/" {
		t.Fatalf("btc network = %+v", network)
	}
	if len(xrp.NetworkList) != 1 || !xrp.NetworkList[0].HasSecondaryAddress ||
		xrp.NetworkList[0].ContractAddress != "" || xrp.NetworkList[0].AddressExplorerURL != "" ||
		xrp.ConfirmationCount != "" {
		t.Fatalf("xrp currency with null optional fields = %+v", xrp)
	}

	notices, err := client.Notices(ctx)
	if err != nil || len(notices) != 2 {
		t.Fatalf("Notices() = %+v, error = %v", notices, err)
	}
	if notices[0].Title != "공지사항 제목 샘플" || notices[0].CreatedAt != 1700000000000 ||
		notices[0].UpdatedAt == nil || *notices[0].UpdatedAt != 1700000000500 ||
		notices[0].URL != "https://digitalx.miraeasset.com/notice/detail/?noticeId=4Oy9q6ALiM7jABzMt32ul5" ||
		len(notices[0].Raw) == 0 {
		t.Fatalf("first notice = %+v", notices[0])
	}
	if notices[1].UpdatedAt != nil || notices[1].CreatedAt != 1699990000000 {
		t.Fatalf("notice without updatedAt = %+v", notices[1])
	}

	alerts, err := client.MarketAlerts(ctx)
	if err != nil || len(alerts) != 1 || alerts[0].Symbol != "btc_krw" || len(alerts[0].Alerts) != 2 || len(alerts[0].Raw) == 0 {
		t.Fatalf("MarketAlerts() = %+v, error = %v", alerts, err)
	}
	priceAlert, limitAlert := alerts[0].Alerts[0], alerts[0].Alerts[1]
	if priceAlert.Type != "price" || priceAlert.TypeKorean != "가격 급등락" || priceAlert.Level != "danger" ||
		priceAlert.LevelKorean != "투자위험" || priceAlert.StartAt == nil || *priceAlert.StartAt != 1700000000000 ||
		priceAlert.EndAt == nil || *priceAlert.EndAt != 1700000100000 || priceAlert.Value != "250.5" {
		t.Fatalf("price alert = %+v", priceAlert)
	}
	if limitAlert.Type != "limit_buy_range" || limitAlert.Level != "" || limitAlert.EndAt != nil ||
		limitAlert.StartAt == nil || limitAlert.Value != "145000000" {
		t.Fatalf("order price limit alert = %+v", limitAlert)
	}

	entries := recorder.snapshot()
	wantPaths := []string{"/v2/currencies", "/v2/notices", "/v2/marketAlerts"}
	if len(entries) != len(wantPaths) {
		t.Fatalf("requests = %+v", entries)
	}
	for index, entry := range entries {
		if entry.Method != http.MethodGet || entry.Path != wantPaths[index] || entry.Signed || entry.Params != "" {
			t.Fatalf("public request %d = %+v, want unsigned GET %s without parameters", index, entry, wantPaths[index])
		}
	}
	snapshot, err := limiter.Snapshot("korbit:route:route-a:public:1second")
	if err != nil || snapshot.Used != 3 || snapshot.Rule.Limit != DefaultPublicRequestsPerSecond {
		t.Fatalf("public limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestMarketAlertsEmptyListIsNotAnError(t *testing.T) {
	t.Parallel()

	server, _ := newKorbitStubServer(t, map[string]string{"GET /v2/marketAlerts": `{"success":true,"data":[]}`})
	client, _ := newPublicTestClient(t, server.URL)
	alerts, err := client.MarketAlerts(context.Background())
	if err != nil || alerts == nil || len(alerts) != 0 {
		t.Fatalf("MarketAlerts() = %#v, error = %v, want an empty non-nil list", alerts, err)
	}
}

func TestPublicMarketExtensionsRejectMalformedData(t *testing.T) {
	t.Parallel()

	server, _ := newKorbitStubServer(t, map[string]string{
		"GET /v2/currencies":   `{"success":true,"data":{"name":"btc"}}`,
		"GET /v2/notices":      `{"success":true,"data":[{"title":"x","createdAt":"not-a-number"}]}`,
		"GET /v2/marketAlerts": `{"success":true}`,
	})
	client, _ := newPublicTestClient(t, server.URL)
	ctx := context.Background()
	if _, err := client.Currencies(ctx); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Errorf("Currencies() error = %v, want ErrExchangeUnavailable", err)
	}
	if _, err := client.Notices(ctx); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Errorf("Notices() error = %v, want ErrExchangeUnavailable", err)
	}
	if _, err := client.MarketAlerts(ctx); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Errorf("MarketAlerts() error = %v, want ErrExchangeUnavailable", err)
	}
}
