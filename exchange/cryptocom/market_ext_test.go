package cryptocom

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientPublicExtendedMarketData(t *testing.T) {
	t.Parallel()
	start := time.UnixMilli(1_700_000_000_000)
	end := time.UnixMilli(1_700_003_600_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method != http.MethodGet {
			http.Error(writer, `{"id":-1,"method":"","code":40001}`, http.StatusBadRequest)
			return
		}
		switch request.URL.Path {
		case "/exchange/v1/public/get-valuations":
			query := request.URL.Query()
			if query.Get("instrument_name") != "BTCUSD-INDEX" || query.Get("valuation_type") != "index_price" ||
				query.Get("count") != "2" || query.Get("start_ts") != "1700000000000" ||
				query.Get("end_ts") != "1700003600000" {
				http.Error(writer, `{"id":1,"method":"public/get-valuations","code":40001}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"id":-1,"method":"public/get-valuations","code":0,"result":{"instrument_name":"BTCUSD-INDEX","data":[{"v":"64000.5","t":1700000060000},{"v":64001,"t":"1700000120000"}]}}`)
		case "/exchange/v1/public/get-deposit-instruments":
			_, _ = io.WriteString(writer, `{"id":-1,"method":"public/get-deposit-instruments","code":"0","result":[{"symbol":"USD","inst_type":"CCY","display_name":"USD","base_ccy":"USD","quote_ccy":"USD","quote_decimals":8,"quantity_decimals":8,"haircut":"1.0","collateral_eligible":false,"price_tick_size":"0.00000001","qty_tick_size":"0.00000001","max_leverage":"1","tradable":false,"expiry_timestamp_ms":0,"beta_product":false,"conversion_fee":"0.40","margin_buy_enabled":false,"margin_sell_enabled":false,"inst_id":12345}]}`)
		case "/exchange/v1/public/get-risk-parameters":
			_, _ = io.WriteString(writer, `{"id":-1,"method":"public/get-risk-parameters","code":"0","result":{"default_max_product_leverage_for_spot":"1.0","default_max_product_leverage_for_perps":"20.0","default_long_pos_limit_perps":"-1.0","default_unit_margin_rate":"0.05","default_collateral_cap":"0.0","update_timestamp_ms":"1763005542745","base_currency_config":[{"instrument_name":"1INCH","minimum_haircut":"0","unit_margin_rate":"0.00060","order_limit":"100000.0"},{"instrument_name":"BTC","collateral_cap_notional":"25000000","minimum_haircut":"0.0625","max_product_leverage_for_perps":"50.0","long_pos_limit_perps":"8800.0"}]}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, _ := newTestClient(t, server.URL+"/exchange/v1", &directSender{})
	ctx := context.Background()

	valuations, err := client.GetValuations(ctx, ValuationsRequest{
		InstrumentName: "BTCUSD-INDEX", ValuationType: ValuationIndexPrice,
		Count: 2, Start: &start, End: &end,
	})
	if err != nil || valuations.InstrumentName != "BTCUSD-INDEX" || len(valuations.Data) != 2 ||
		valuations.Data[0].Value != "64000.5" || valuations.Data[0].Timestamp != "1700000060000" ||
		valuations.Data[1].Value != "64001" || valuations.Data[1].Timestamp != "1700000120000" ||
		len(valuations.Raw) == 0 {
		t.Fatalf("GetValuations() = %+v, error = %v", valuations, err)
	}

	instruments, err := client.DepositInstruments(ctx)
	if err != nil || len(instruments) != 1 || instruments[0].Symbol != "USD" ||
		instruments[0].Haircut != "1.0" || instruments[0].ConversionFee != "0.40" ||
		instruments[0].QuoteDecimals != 8 || instruments[0].InstrumentID != "12345" ||
		instruments[0].CollateralEligible || len(instruments[0].Raw) == 0 {
		t.Fatalf("DepositInstruments() = %+v, error = %v", instruments, err)
	}

	risk, err := client.GetRiskParameters(ctx)
	if err != nil || risk.DefaultMaxLeveragePerps != "20.0" || risk.DefaultLongPositionLimitPerps != "-1.0" ||
		risk.UpdateTimestamp != "1763005542745" || len(risk.BaseCurrencyConfig) != 2 ||
		risk.BaseCurrencyConfig[1].InstrumentName != "BTC" ||
		risk.BaseCurrencyConfig[1].CollateralCapNotional != "25000000" ||
		risk.BaseCurrencyConfig[1].MaxLeveragePerps != "50.0" ||
		risk.BaseCurrencyConfig[0].CollateralCapNotional != "" ||
		len(risk.BaseCurrencyConfig[1].Raw) == 0 || len(risk.Raw) == 0 {
		t.Fatalf("GetRiskParameters() = %+v, error = %v", risk, err)
	}
}

func TestValuationsRequestValidation(t *testing.T) {
	t.Parallel()
	later := time.UnixMilli(1_700_000_000_000)
	earlier := later.Add(-time.Hour)
	tests := []struct {
		name    string
		request ValuationsRequest
	}{
		{"bad instrument", ValuationsRequest{InstrumentName: "btc usd", ValuationType: ValuationMarkPrice}},
		{"bad type", ValuationsRequest{InstrumentName: "BTCUSD-INDEX", ValuationType: "spot"}},
		{"negative count", ValuationsRequest{InstrumentName: "BTCUSD-INDEX", ValuationType: ValuationMarkPrice, Count: -1}},
		{"reversed range", ValuationsRequest{InstrumentName: "BTCUSD-INDEX", ValuationType: ValuationMarkPrice, Start: &later, End: &earlier}},
	}
	client, _ := newTestClient(t, "http://127.0.0.1/exchange/v1", &directSender{})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := client.GetValuations(context.Background(), test.request); err == nil {
				t.Fatal("GetValuations() error = nil")
			}
		})
	}
}
