package coinbase

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

const transactionSummaryResponse = `{"total_volume":1000.5,"total_fees":25.25,` +
	`"fee_tier":{"pricing_tier":"Advanced 1","taker_fee_rate":"0.006","maker_fee_rate":"0.004","aop_from":"0","aop_to":"10000",` +
	`"volume_types_and_range":[{"volume_types":["VOLUME_TYPE_SPOT","VOLUME_TYPE_US_DERIVATIVES"],"vol_from":"0","vol_to":"50000"}]},` +
	`"margin_rate":{"value":"0.5"},"goods_and_services_tax":{"rate":"0.1","type":"INCLUSIVE"},` +
	`"advanced_trade_only_volume":1000,"advanced_trade_only_fees":25,"coinbase_pro_volume":0,"coinbase_pro_fees":0,` +
	`"total_balance":"1500","volume_breakdown":[{"volume_type":"VOLUME_TYPE_SPOT","volume":1000.5}],` +
	`"has_cost_plus_commission":false,` +
	`"fee_tier_without_promotion":{"pricing_tier":"Advanced 1","qualification_type":"FEE_TIER_QUALIFICATION_TYPE_SPOT_VOLUME",` +
	`"current_value":"1000.5","next_tier_threshold":"10000",` +
	`"current_tier":{"pricing_tier":"Advanced 1","taker_fee_rate":"0.006","maker_fee_rate":"0.004"},` +
	`"next_tier":{"pricing_tier":"Advanced 2","taker_fee_rate":"0.0025","maker_fee_rate":"0.0015"},"current_tier_threshold":"0"},` +
	`"deribit_venue_data":{"perps":{"total_volume":5,"total_fees":1}}}`

func TestTransactionSummaryQueriesAndDecodesFeeTier(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK, transactionSummaryResponse,
	)
	summary, err := harness.client.TransactionSummary(
		context.Background(), TransactionSummaryRequest{ProductType: ProductTypeSpot},
	)
	if err != nil {
		t.Fatalf("TransactionSummary() error = %v", err)
	}
	if summary.TotalVolume.String() != "1000.5" || summary.TotalFees.String() != "25.25" ||
		summary.FeeTier.PricingTier != "Advanced 1" || summary.FeeTier.TakerFeeRate != "0.006" ||
		summary.FeeTier.MakerFeeRate != "0.004" || len(summary.FeeTier.VolumeTypesAndRange) != 1 ||
		summary.FeeTier.VolumeTypesAndRange[0].VolumeTypes[1] != "VOLUME_TYPE_US_DERIVATIVES" ||
		summary.FeeTier.VolumeTypesAndRange[0].VolTo != "50000" ||
		summary.MarginRate.Value != "0.5" || summary.GoodsAndServicesTax.Type != "INCLUSIVE" ||
		summary.AdvancedTradeOnlyVolume.String() != "1000" || summary.CoinbaseProFees.String() != "0" ||
		summary.TotalBalance != "1500" || len(summary.VolumeBreakdown) != 1 ||
		summary.VolumeBreakdown[0].Volume.String() != "1000.5" || summary.HasCostPlusCommission ||
		summary.FeeTierWithoutPromotion.QualificationType != "FEE_TIER_QUALIFICATION_TYPE_SPOT_VOLUME" ||
		summary.FeeTierWithoutPromotion.NextTier.PricingTier != "Advanced 2" ||
		summary.FeeTierWithoutPromotion.CurrentTier.MakerFeeRate != "0.004" ||
		!strings.Contains(string(summary.Raw), "deribit_venue_data") {
		t.Fatalf("TransactionSummary() = %+v", summary)
	}
	harness.assertSingle(
		t, http.MethodGet, publicPrefix+"/transaction_summary",
		url.Values{"product_type": {"SPOT"}}, "",
	)
}

func TestTransactionSummarySendsFuturesFilters(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK, transactionSummaryResponse,
	)
	_, err := harness.client.TransactionSummary(context.Background(), TransactionSummaryRequest{
		ProductType: ProductTypeFuture, ContractExpiryType: ContractExpiryPerpetual, ProductVenue: ProductVenueFCM,
	})
	if err != nil {
		t.Fatalf("TransactionSummary() error = %v", err)
	}
	harness.assertSingle(
		t, http.MethodGet, publicPrefix+"/transaction_summary",
		url.Values{"product_type": {"FUTURE"}, "contract_expiry_type": {"PERPETUAL"}, "product_venue": {"FCM"}}, "",
	)
}

func TestTransactionSummaryWithoutFiltersSendsNoQuery(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK, transactionSummaryResponse,
	)
	if _, err := harness.client.TransactionSummary(context.Background(), TransactionSummaryRequest{}); err != nil {
		t.Fatalf("TransactionSummary() error = %v", err)
	}
	harness.assertSingle(t, http.MethodGet, publicPrefix+"/transaction_summary", nil, "")
}

func TestTransactionSummaryRejectsUnsupportedFilters(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK, transactionSummaryResponse,
	)
	requests := []TransactionSummaryRequest{
		{ProductType: "SWAP"},
		{ContractExpiryType: "WEEKLY"},
		{ProductVenue: "NYSE"},
	}
	for _, request := range requests {
		if _, err := harness.client.TransactionSummary(context.Background(), request); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("TransactionSummary(%+v) error = %v, want validation", request, err)
		}
	}
	if count := harness.requestCount(); count != 0 {
		t.Fatalf("request count = %d, want 0", count)
	}
}

func TestKeyPermissionsDecodesFlagsAndPortfolio(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK,
		`{"can_view":true,"can_trade":true,"can_transfer":false,"portfolio_uuid":"portfolio-1","portfolio_type":"DEFAULT"}`,
	)
	permissions, err := harness.client.KeyPermissions(context.Background())
	if err != nil {
		t.Fatalf("KeyPermissions() error = %v", err)
	}
	if !permissions.CanView || !permissions.CanTrade || permissions.CanTransfer ||
		permissions.PortfolioUUID != "portfolio-1" || permissions.PortfolioType != PortfolioTypeDefault ||
		len(permissions.Raw) == 0 {
		t.Fatalf("KeyPermissions() = %+v", permissions)
	}
	harness.assertSingle(t, http.MethodGet, publicPrefix+"/key_permissions", nil, "")
}

func TestKeyPermissionsClassifiesAuthenticationFailure(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusUnauthorized,
		`{"error":"UNAUTHENTICATED","message":"invalid token"}`,
	)
	if _, err := harness.client.KeyPermissions(context.Background()); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("KeyPermissions(401) error = %v, want authentication", err)
	}
}

const paymentMethodJSON = `{"id":"pm-1","type":"ACH","name":"ALLY BANK ******1234","currency":"USD","verified":true,` +
	`"allow_buy":true,"allow_sell":false,"allow_deposit":true,"allow_withdraw":false,` +
	`"created_at":"2021-05-31T09:59:59Z","updated_at":"2021-06-01T09:59:59Z"}`

func TestPaymentMethodsDecodesList(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK,
		`{"payment_methods":[`+paymentMethodJSON+`,{"id":"pm-2","type":"FIAT_ACCOUNT","name":"USD Wallet","currency":"USD","verified":true,"allow_buy":true,"allow_sell":true,"allow_deposit":false,"allow_withdraw":false}]}`,
	)
	methods, err := harness.client.PaymentMethods(context.Background())
	if err != nil {
		t.Fatalf("PaymentMethods() error = %v", err)
	}
	if len(methods) != 2 || methods[0].ID != "pm-1" || methods[0].Type != "ACH" || !methods[0].Verified ||
		!methods[0].AllowBuy || methods[0].AllowSell || !methods[0].AllowDeposit || methods[0].AllowWithdraw ||
		methods[0].CreatedAt != "2021-05-31T09:59:59Z" || methods[1].ID != "pm-2" || !methods[1].AllowSell ||
		len(methods[0].Raw) == 0 || len(methods[1].Raw) == 0 {
		t.Fatalf("PaymentMethods() = %+v", methods)
	}
	harness.assertSingle(t, http.MethodGet, publicPrefix+"/payment_methods", nil, "")
}

func TestPaymentMethodDecodesSingleMethod(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK,
		`{"payment_method":`+paymentMethodJSON+`}`,
	)
	method, err := harness.client.PaymentMethod(context.Background(), "pm-1")
	if err != nil {
		t.Fatalf("PaymentMethod() error = %v", err)
	}
	if method.ID != "pm-1" || method.Name != "ALLY BANK ******1234" || method.Currency != "USD" ||
		method.UpdatedAt != "2021-06-01T09:59:59Z" || len(method.Raw) == 0 {
		t.Fatalf("PaymentMethod() = %+v", method)
	}
	harness.assertSingle(t, http.MethodGet, publicPrefix+"/payment_methods/pm-1", nil, "")
}

func TestPaymentMethodRejectsUnsafeIDs(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(t, permissionsOf(credential.PermissionRead), http.StatusOK, `{}`)
	for _, paymentMethodID := range []string{"", " pm-1", "pm/1", "pm-1?x=1"} {
		if _, err := harness.client.PaymentMethod(context.Background(), paymentMethodID); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("PaymentMethod(%q) error = %v, want validation", paymentMethodID, err)
		}
	}
	if count := harness.requestCount(); count != 0 {
		t.Fatalf("request count = %d, want 0", count)
	}
}
