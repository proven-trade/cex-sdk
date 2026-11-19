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

const convertTradeResponse = `{"trade":{"id":"trade-1","status":"%STATUS%",` +
	`"user_entered_amount":{"value":"100","currency":"USD"},"amount":{"value":"100","currency":"USD"},` +
	`"subtotal":{"value":"100","currency":"USD"},"total":{"value":"100","currency":"USD"},` +
	`"fees":[{"title":"Convert fee","description":"Waived","amount":{"value":"0","currency":"USD"},"label":"fee"}],` +
	`"total_fee":{"title":"Total fee","amount":{"value":"0","currency":"USD"}},` +
	`"source":{"type":"COINBASE_ACCOUNT","payment_method_id":"pm-1"},` +
	`"source_currency":"USD","target_currency":"USDC","source_id":"usd-account","target_id":"usdc-account",` +
	`"exchange_rate":{"value":"1","currency":"USDC"}}}`

func convertResponse(status string) string {
	return strings.Replace(convertTradeResponse, "%STATUS%", status, 1)
}

func TestConvertQuotePostsBodyAndDecodesTrade(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead, credential.PermissionTransfer), http.StatusOK,
		convertResponse("TRADE_STATUS_CREATED"),
	)
	quote, err := harness.client.ConvertQuote(context.Background(), ConvertQuoteRequest{
		FromAccount: "usd-account", ToAccount: "usdc-account", Amount: "100",
	})
	if err != nil {
		t.Fatalf("ConvertQuote() error = %v", err)
	}
	if quote.ID != "trade-1" || quote.Status != "TRADE_STATUS_CREATED" || quote.Amount.Value != "100" ||
		quote.Total.Currency != "USD" || len(quote.Fees) != 1 || quote.Fees[0].Title != "Convert fee" ||
		quote.TotalFee.Amount.Value != "0" || quote.SourceCurrency != "USD" || quote.TargetCurrency != "USDC" ||
		quote.SourceID != "usd-account" || quote.ExchangeRate.Currency != "USDC" ||
		!strings.Contains(string(quote.Raw), "COINBASE_ACCOUNT") {
		t.Fatalf("ConvertQuote() = %+v", quote)
	}
	harness.assertSingle(
		t, http.MethodPost, publicPrefix+"/convert/quote", nil,
		`{"from_account":"usd-account","to_account":"usdc-account","amount":"100"}`,
	)
}

func TestConvertQuoteSendsTradeIncentiveMetadata(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionTransfer), http.StatusOK, convertResponse("TRADE_STATUS_CREATED"),
	)
	_, err := harness.client.ConvertQuote(context.Background(), ConvertQuoteRequest{
		FromAccount: "usd-account", ToAccount: "usdc-account", Amount: "100",
		TradeIncentiveMetadata: &TradeIncentiveMetadata{CodeVal: "PROMO"},
	})
	if err != nil {
		t.Fatalf("ConvertQuote() error = %v", err)
	}
	harness.assertSingle(
		t, http.MethodPost, publicPrefix+"/convert/quote", nil,
		`{"from_account":"usd-account","to_account":"usdc-account","amount":"100","trade_incentive_metadata":{"code_val":"PROMO"}}`,
	)
}

func TestCommitConvertTradePostsAccountsForTradeID(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionTransfer), http.StatusOK, convertResponse("TRADE_STATUS_COMPLETED"),
	)
	committed, err := harness.client.CommitConvertTrade(context.Background(), ConvertTradeRequest{
		TradeID: "trade-1", FromAccount: "usd-account", ToAccount: "usdc-account",
	})
	if err != nil {
		t.Fatalf("CommitConvertTrade() error = %v", err)
	}
	if committed.ID != "trade-1" || committed.Status != "TRADE_STATUS_COMPLETED" || len(committed.Raw) == 0 {
		t.Fatalf("CommitConvertTrade() = %+v", committed)
	}
	harness.assertSingle(
		t, http.MethodPost, publicPrefix+"/convert/trade/trade-1", nil,
		`{"from_account":"usd-account","to_account":"usdc-account"}`,
	)
}

func TestCommitConvertTradeServerErrorIsUnknownExecutionStateAndNotRetried(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionTransfer), http.StatusInternalServerError,
		`{"error":"INTERNAL","message":"try later"}`,
	)
	_, err := harness.client.CommitConvertTrade(context.Background(), ConvertTradeRequest{
		TradeID: "trade-1", FromAccount: "usd-account", ToAccount: "usdc-account",
	})
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CommitConvertTrade(500) error = %v, want unknown execution state", err)
	}
	if count := harness.requestCount(); count != 1 {
		t.Fatalf("request count = %d, want 1 (mutations must not be retried)", count)
	}
}

func TestConvertTradeQueriesTradeWithReadPermission(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK, convertResponse("TRADE_STATUS_STARTED"),
	)
	status, err := harness.client.ConvertTrade(context.Background(), ConvertTradeRequest{
		TradeID: "trade-1", FromAccount: "usd-account", ToAccount: "usdc-account",
	})
	if err != nil {
		t.Fatalf("ConvertTrade() error = %v", err)
	}
	if status.ID != "trade-1" || status.Status != "TRADE_STATUS_STARTED" || status.TargetID != "usdc-account" {
		t.Fatalf("ConvertTrade() = %+v", status)
	}
	harness.assertSingle(
		t, http.MethodGet, publicPrefix+"/convert/trade/trade-1",
		url.Values{"from_account": {"usd-account"}, "to_account": {"usdc-account"}}, "",
	)
}

func TestConvertMutationsRequireTransferPermission(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead, credential.PermissionTrade), http.StatusOK,
		convertResponse("TRADE_STATUS_CREATED"),
	)
	_, err := harness.client.ConvertQuote(context.Background(), ConvertQuoteRequest{
		FromAccount: "usd-account", ToAccount: "usdc-account", Amount: "100",
	})
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("ConvertQuote(no transfer) error = %v, want authorization", err)
	}
	_, err = harness.client.CommitConvertTrade(context.Background(), ConvertTradeRequest{
		TradeID: "trade-1", FromAccount: "usd-account", ToAccount: "usdc-account",
	})
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("CommitConvertTrade(no transfer) error = %v, want authorization", err)
	}
	if count := harness.requestCount(); count != 0 {
		t.Fatalf("request count = %d, want 0", count)
	}
}

func TestConvertRequestValidation(t *testing.T) {
	t.Parallel()

	quotes := []struct {
		name    string
		request ConvertQuoteRequest
	}{
		{name: "missing from account", request: ConvertQuoteRequest{ToAccount: "b", Amount: "1"}},
		{name: "missing to account", request: ConvertQuoteRequest{FromAccount: "a", Amount: "1"}},
		{name: "missing amount", request: ConvertQuoteRequest{FromAccount: "a", ToAccount: "b"}},
		{name: "zero amount", request: ConvertQuoteRequest{FromAccount: "a", ToAccount: "b", Amount: "0"}},
		{name: "bad promo code", request: ConvertQuoteRequest{
			FromAccount: "a", ToAccount: "b", Amount: "1",
			TradeIncentiveMetadata: &TradeIncentiveMetadata{CodeVal: " PROMO"},
		}},
	}
	trades := []struct {
		name    string
		request ConvertTradeRequest
	}{
		{name: "missing trade ID", request: ConvertTradeRequest{FromAccount: "a", ToAccount: "b"}},
		{name: "trade ID with slash", request: ConvertTradeRequest{TradeID: "trade/1", FromAccount: "a", ToAccount: "b"}},
		{name: "missing from account", request: ConvertTradeRequest{TradeID: "t", ToAccount: "b"}},
		{name: "missing to account", request: ConvertTradeRequest{TradeID: "t", FromAccount: "a"}},
	}
	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead, credential.PermissionTransfer), http.StatusOK,
		convertResponse("TRADE_STATUS_CREATED"),
	)
	for _, testCase := range quotes {
		if _, err := harness.client.ConvertQuote(context.Background(), testCase.request); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("ConvertQuote(%s) error = %v, want validation", testCase.name, err)
		}
	}
	for _, testCase := range trades {
		if _, err := harness.client.CommitConvertTrade(context.Background(), testCase.request); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("CommitConvertTrade(%s) error = %v, want validation", testCase.name, err)
		}
		if _, err := harness.client.ConvertTrade(context.Background(), testCase.request); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("ConvertTrade(%s) error = %v, want validation", testCase.name, err)
		}
	}
	if count := harness.requestCount(); count != 0 {
		t.Fatalf("request count = %d, want 0", count)
	}
}
