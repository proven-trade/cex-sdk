package bithumb

import (
	"context"
	"errors"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
)

const warningSample = `[{"market":"KRW-BTC","warning_type":"PRICE_SUDDEN_FLUCTUATION","warning_step":"CAUTION","end_date":"2026-01-15 18:00:00"},` +
	`{"market":"KRW-ETH","warning_type":"TRADING_VOLUME_SUDDEN_FLUCTUATION","warning_step":"WARNING","end_date":"2026-01-16 12:30:00"}]`

const noticeSample = `[{"categories":["이벤트","안내"],"title":"마이쉘(SHELL) 원화 마켓 추가 기념 에어드랍 이벤트",` +
	`"pc_url":"https://feed.bithumb.com/notice/1647203","published_at":"2025-02-28 15:00:00","modified_at":"2025-02-28 14:29:56"}]`

const feeSample = `[{"name":"비트코인","currency":"BTC","networks":[` +
	`{"net_name":"Bitcoin","deposit_fee_quantity":"0","deposit_minimum_quantity":"0.0001","withdraw_fee_quantity":"0.0005",` +
	`"withdraw_rate":null,"withdraw_fee_max":null,"withdraw_fee_min":null,"withdraw_minimum_quantity":"0.001"},` +
	`{"net_name":"Lightning","deposit_fee_quantity":"0","deposit_minimum_quantity":"0","withdraw_fee_quantity":null,` +
	`"withdraw_rate":"0.01","withdraw_fee_max":"100","withdraw_fee_min":"1","withdraw_minimum_quantity":"0.0001"}]}]`

func TestVirtualAssetWarningsNoticesAndInOutFees(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{
		extOK("GET /v1/market/virtual_asset_warning", warningSample),
		extOK("GET /v1/notices", noticeSample),
		extOK("GET /v2/fee/inout/ALL", feeSample),
		extOK("GET /v2/fee/inout/BTC", feeSample),
	}, extReadOnly)
	ctx := context.Background()

	warnings, err := fixture.client.VirtualAssetWarnings(ctx)
	if err != nil || len(warnings) != 2 || warnings[0].Market != "KRW-BTC" ||
		warnings[0].WarningType != WarningTypePriceSuddenFluctuation || warnings[0].WarningStep != WarningStepCaution ||
		warnings[0].EndDate != "2026-01-15 18:00:00" || warnings[1].WarningStep != WarningStepWarning ||
		warnings[1].WarningType != WarningTypeTradingVolumeSuddenFluctuation || len(warnings[1].Raw) == 0 {
		t.Fatalf("VirtualAssetWarnings() = %+v, error = %v", warnings, err)
	}
	notices, err := fixture.client.Notices(ctx, NoticesRequest{Count: 1})
	if err != nil || len(notices) != 1 || len(notices[0].Categories) != 2 || notices[0].Categories[1] != "안내" ||
		notices[0].PCURL != "https://feed.bithumb.com/notice/1647203" ||
		notices[0].PublishedAt != "2025-02-28 15:00:00" || notices[0].ModifiedAt != "2025-02-28 14:29:56" ||
		len(notices[0].Raw) == 0 {
		t.Fatalf("Notices() = %+v, error = %v", notices, err)
	}
	allFees, err := fixture.client.InOutFees(ctx, InOutFeesRequest{Currency: FeeCurrencyAll})
	if err != nil || len(allFees) != 1 || allFees[0].Currency != "BTC" || len(allFees[0].Networks) != 2 || len(allFees[0].Raw) == 0 {
		t.Fatalf("InOutFees(ALL) = %+v, error = %v", allFees, err)
	}
	amountFee, rateFee := allFees[0].Networks[0], allFees[0].Networks[1]
	if amountFee.NetName != "Bitcoin" || amountFee.WithdrawFeeQuantity != "0.0005" || amountFee.WithdrawRate != "" ||
		amountFee.WithdrawFeeMin != "" || amountFee.DepositMinimumQuantity != "0.0001" ||
		rateFee.WithdrawFeeQuantity != "" || rateFee.WithdrawRate != "0.01" ||
		rateFee.WithdrawFeeMin != "1" || rateFee.WithdrawFeeMax != "100" {
		t.Fatalf("InOutFees(ALL) networks = %+v", allFees[0].Networks)
	}
	if _, err := fixture.client.InOutFees(ctx, InOutFeesRequest{Currency: "BTC"}); err != nil {
		t.Fatalf("InOutFees(BTC) error = %v", err)
	}

	records := extRequests(t, fixture.server, 4)
	assertExtPublicRequest(t, records[0], "/v1/market/virtual_asset_warning", "")
	assertExtPublicRequest(t, records[1], "/v1/notices", "count=1")
	assertExtPublicRequest(t, records[2], "/v2/fee/inout/ALL", "")
	assertExtPublicRequest(t, records[3], "/v2/fee/inout/BTC", "")
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:public-other:1second", 150, 3)
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:public-notice:1second", 1, 1)
	if calls, _, _ := fixture.provider.snapshot(); calls != 0 {
		t.Fatalf("credential provider calls = %d, want 0 for public market information", calls)
	}
}

func TestInOutFeesMapsNumericErrorName(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{
		extReplies(
			"GET /v2/fee/inout/BTC",
			extStatus(400, `{"error":{"name":400,"message":"Type mismatch error. Check the parameters type!"}}`),
		),
	}, extReadOnly)
	_, err := fixture.client.InOutFees(context.Background(), InOutFeesRequest{Currency: "BTC"})
	var apiError *trade.APIError
	if !errors.Is(err, trade.ErrValidation) || !errors.As(err, &apiError) || apiError.ExchangeCode != "400" ||
		apiError.HTTPStatus != 400 || apiError.Retryable {
		t.Fatalf("InOutFees() 400 error = %v, want non-retryable validation with exchange code 400", err)
	}
	extRequests(t, fixture.server, 1)
}

func TestMarketInformationRejectsInvalidInputBeforeSending(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, nil, extReadOnly)
	ctx := context.Background()
	for _, currency := range []string{"", "BTC/ETH", "../v1/accounts", "BTC?x=1", "btc eth", "BTC#1"} {
		if _, err := fixture.client.InOutFees(ctx, InOutFeesRequest{Currency: currency}); !errors.Is(err, trade.ErrValidation) {
			t.Errorf("InOutFees(%q) error = %v, want ErrValidation", currency, err)
		}
	}
	for _, count := range []int{-1, 21} {
		if _, err := fixture.client.Notices(ctx, NoticesRequest{Count: count}); !errors.Is(err, trade.ErrValidation) {
			t.Errorf("Notices(count=%d) error = %v, want ErrValidation", count, err)
		}
	}
	if records := fixture.server.requests(); len(records) != 0 {
		t.Fatalf("invalid market information requests reached the server: %+v", records)
	}
}
