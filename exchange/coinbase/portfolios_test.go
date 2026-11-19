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

const portfolioBreakdownResponse = `{"breakdown":{` +
	`"portfolio":{"name":"Default","uuid":"pf-1","type":"DEFAULT","deleted":false},` +
	`"portfolio_balances":{"total_balance":{"value":"1500.25","currency":"USD"},` +
	`"total_futures_balance":{"value":"0","currency":"USD"},` +
	`"total_cash_equivalent_balance":{"value":"500","currency":"USD"},` +
	`"total_crypto_balance":{"value":"1000.25","currency":"USD"},` +
	`"futures_unrealized_pnl":{"value":"0","currency":"USD"},"perp_unrealized_pnl":{"value":"0","currency":"USD"}},` +
	`"spot_positions":[{"asset":"BTC","account_uuid":"acct-1","total_balance_fiat":1000.25,"total_balance_crypto":0.0156,` +
	`"available_to_trade_fiat":900.5,"available_to_trade_crypto":0.014,"allocation":0.66,` +
	`"cost_basis":{"value":"900","currency":"USD"},"is_cash":false,"average_entry_price":{"value":"60000","currency":"USD"},` +
	`"asset_uuid":"asset-btc","unrealized_pnl":100.25,"available_to_transfer_fiat":900.5,"available_to_transfer_crypto":0.014,` +
	`"asset_color":"#F7931A","account_type":"ACCOUNT_TYPE_CRYPTO"}],` +
	`"perp_positions":[],"futures_positions":[{"product_id":"BIT-28JUL23-CDE"}],"prediction_markets_positions":[],"equity_positions":[]}}`

func TestPortfoliosFiltersByTypeAndDecodesList(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK,
		`{"portfolios":[{"name":"Default","uuid":"pf-1","type":"DEFAULT","deleted":false},{"name":"Algo","uuid":"pf-2","type":"CONSUMER","deleted":true}]}`,
	)
	portfolios, err := harness.client.Portfolios(
		context.Background(), PortfoliosRequest{PortfolioType: PortfolioTypeDefault},
	)
	if err != nil {
		t.Fatalf("Portfolios() error = %v", err)
	}
	if len(portfolios) != 2 || portfolios[0].UUID != "pf-1" || portfolios[0].Type != PortfolioTypeDefault ||
		portfolios[0].Deleted || portfolios[1].Name != "Algo" || portfolios[1].Type != PortfolioTypeConsumer ||
		!portfolios[1].Deleted || len(portfolios[0].Raw) == 0 || len(portfolios[1].Raw) == 0 {
		t.Fatalf("Portfolios() = %+v", portfolios)
	}
	harness.assertSingle(
		t, http.MethodGet, publicPrefix+"/portfolios", url.Values{"portfolio_type": {"DEFAULT"}}, "",
	)
}

func TestPortfoliosRejectsUnknownTypeAndOmitsEmptyFilter(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK, `{"portfolios":[]}`,
	)
	if _, err := harness.client.Portfolios(
		context.Background(), PortfoliosRequest{PortfolioType: "MARGIN"},
	); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("Portfolios(unknown type) error = %v, want validation", err)
	}
	if count := harness.requestCount(); count != 0 {
		t.Fatalf("request count = %d, want 0", count)
	}
	if _, err := harness.client.Portfolios(context.Background(), PortfoliosRequest{}); err != nil {
		t.Fatalf("Portfolios() error = %v", err)
	}
	harness.assertSingle(t, http.MethodGet, publicPrefix+"/portfolios", nil, "")
}

func TestCreatePortfolioPostsName(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead, credential.PermissionTransfer), http.StatusOK,
		`{"portfolio":{"name":"Algo","uuid":"pf-2","type":"CONSUMER","deleted":false}}`,
	)
	portfolio, err := harness.client.CreatePortfolio(context.Background(), CreatePortfolioRequest{Name: "Algo"})
	if err != nil {
		t.Fatalf("CreatePortfolio() error = %v", err)
	}
	if portfolio.UUID != "pf-2" || portfolio.Name != "Algo" || portfolio.Type != PortfolioTypeConsumer ||
		len(portfolio.Raw) == 0 {
		t.Fatalf("CreatePortfolio() = %+v", portfolio)
	}
	harness.assertSingle(t, http.MethodPost, publicPrefix+"/portfolios", nil, `{"name":"Algo"}`)
}

func TestEditPortfolioPutsNameWithoutUUIDInBody(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionTransfer), http.StatusOK,
		`{"portfolio":{"name":"Algo v2","uuid":"pf-2","type":"CONSUMER","deleted":false}}`,
	)
	portfolio, err := harness.client.EditPortfolio(
		context.Background(), EditPortfolioRequest{PortfolioUUID: "pf-2", Name: "Algo v2"},
	)
	if err != nil {
		t.Fatalf("EditPortfolio() error = %v", err)
	}
	if portfolio.UUID != "pf-2" || portfolio.Name != "Algo v2" {
		t.Fatalf("EditPortfolio() = %+v", portfolio)
	}
	harness.assertSingle(t, http.MethodPut, publicPrefix+"/portfolios/pf-2", nil, `{"name":"Algo v2"}`)
}

func TestDeletePortfolioSendsDeleteWithoutBody(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(t, permissionsOf(credential.PermissionTransfer), http.StatusOK, `{}`)
	if err := harness.client.DeletePortfolio(context.Background(), "pf-2"); err != nil {
		t.Fatalf("DeletePortfolio() error = %v", err)
	}
	harness.assertSingle(t, http.MethodDelete, publicPrefix+"/portfolios/pf-2", nil, "")
}

func TestDeletePortfolioServerErrorIsUnknownExecutionStateAndNotRetried(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionTransfer), http.StatusBadGateway,
		`{"error":"UNAVAILABLE","message":"try later"}`,
	)
	err := harness.client.DeletePortfolio(context.Background(), "pf-2")
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("DeletePortfolio(502) error = %v, want unknown execution state", err)
	}
	if count := harness.requestCount(); count != 1 {
		t.Fatalf("request count = %d, want 1 (mutations must not be retried)", count)
	}
}

func TestPortfolioBreakdownDecodesBalancesAndSpotPositions(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK, portfolioBreakdownResponse,
	)
	breakdown, err := harness.client.PortfolioBreakdown(
		context.Background(), PortfolioBreakdownRequest{PortfolioUUID: "pf-1", Currency: "USD"},
	)
	if err != nil {
		t.Fatalf("PortfolioBreakdown() error = %v", err)
	}
	if breakdown.Portfolio.UUID != "pf-1" || breakdown.Portfolio.Type != PortfolioTypeDefault ||
		breakdown.Balances.TotalBalance.Value != "1500.25" || breakdown.Balances.TotalCashEquivalentBalance.Value != "500" ||
		breakdown.Balances.TotalCryptoBalance.Currency != "USD" || len(breakdown.SpotPositions) != 1 {
		t.Fatalf("PortfolioBreakdown() = %+v", breakdown)
	}
	position := breakdown.SpotPositions[0]
	if position.Asset != "BTC" || position.AccountUUID != "acct-1" || position.TotalBalanceFiat.String() != "1000.25" ||
		position.TotalBalanceCrypto.String() != "0.0156" || position.AvailableToTradeFiat.String() != "900.5" ||
		position.Allocation.String() != "0.66" || position.CostBasis.Value != "900" ||
		position.AverageEntryPrice.Value != "60000" || position.UnrealizedPnL.String() != "100.25" ||
		position.AssetUUID != "asset-btc" || position.AccountType != "ACCOUNT_TYPE_CRYPTO" || position.IsCash {
		t.Fatalf("PortfolioBreakdown() spot position = %+v", position)
	}
	if !strings.Contains(string(breakdown.Raw), "BIT-28JUL23-CDE") {
		t.Fatalf("PortfolioBreakdown() raw = %s", breakdown.Raw)
	}
	harness.assertSingle(
		t, http.MethodGet, publicPrefix+"/portfolios/pf-1", url.Values{"currency": {"USD"}}, "",
	)
}

func TestMovePortfolioFundsPostsAmountAndPortfolios(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead, credential.PermissionTransfer), http.StatusOK,
		`{"source_portfolio_uuid":"pf-1","target_portfolio_uuid":"pf-2"}`,
	)
	transfer, err := harness.client.MovePortfolioFunds(context.Background(), MovePortfolioFundsRequest{
		Funds: Amount{Value: "100.5", Currency: "USD"}, SourcePortfolioUUID: "pf-1", TargetPortfolioUUID: "pf-2",
	})
	if err != nil {
		t.Fatalf("MovePortfolioFunds() error = %v", err)
	}
	if transfer.SourcePortfolioUUID != "pf-1" || transfer.TargetPortfolioUUID != "pf-2" || len(transfer.Raw) == 0 {
		t.Fatalf("MovePortfolioFunds() = %+v", transfer)
	}
	harness.assertSingle(
		t, http.MethodPost, publicPrefix+"/portfolios/move_funds", nil,
		`{"funds":{"value":"100.5","currency":"USD"},"source_portfolio_uuid":"pf-1","target_portfolio_uuid":"pf-2"}`,
	)
}

func TestPortfolioMutationsRequireTransferPermission(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead, credential.PermissionTrade), http.StatusOK, `{}`,
	)
	ctx := context.Background()
	_, createErr := harness.client.CreatePortfolio(ctx, CreatePortfolioRequest{Name: "Algo"})
	_, editErr := harness.client.EditPortfolio(ctx, EditPortfolioRequest{PortfolioUUID: "pf-2", Name: "Algo"})
	deleteErr := harness.client.DeletePortfolio(ctx, "pf-2")
	_, moveErr := harness.client.MovePortfolioFunds(ctx, MovePortfolioFundsRequest{
		Funds: Amount{Value: "1", Currency: "USD"}, SourcePortfolioUUID: "pf-1", TargetPortfolioUUID: "pf-2",
	})
	for name, err := range map[string]error{
		"CreatePortfolio": createErr, "EditPortfolio": editErr,
		"DeletePortfolio": deleteErr, "MovePortfolioFunds": moveErr,
	} {
		if !errors.Is(err, trade.ErrAuthorization) {
			t.Fatalf("%s(no transfer) error = %v, want authorization", name, err)
		}
	}
	if count := harness.requestCount(); count != 0 {
		t.Fatalf("request count = %d, want 0", count)
	}
}

func TestPortfolioRequestValidation(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead, credential.PermissionTransfer), http.StatusOK, `{}`,
	)
	ctx := context.Background()
	usd := Amount{Value: "1", Currency: "USD"}
	_, createEmpty := harness.client.CreatePortfolio(ctx, CreatePortfolioRequest{})
	_, editNoUUID := harness.client.EditPortfolio(ctx, EditPortfolioRequest{Name: "Algo"})
	_, editNoName := harness.client.EditPortfolio(ctx, EditPortfolioRequest{PortfolioUUID: "pf-2"})
	_, editBadUUID := harness.client.EditPortfolio(ctx, EditPortfolioRequest{PortfolioUUID: "pf/2", Name: "Algo"})
	deleteEmpty := harness.client.DeletePortfolio(ctx, "")
	deleteBadUUID := harness.client.DeletePortfolio(ctx, "pf/2")
	_, breakdownNoUUID := harness.client.PortfolioBreakdown(ctx, PortfolioBreakdownRequest{})
	_, breakdownBadCurrency := harness.client.PortfolioBreakdown(
		ctx, PortfolioBreakdownRequest{PortfolioUUID: "pf-1", Currency: " USD"},
	)
	_, moveZero := harness.client.MovePortfolioFunds(ctx, MovePortfolioFundsRequest{
		Funds: Amount{Value: "0", Currency: "USD"}, SourcePortfolioUUID: "pf-1", TargetPortfolioUUID: "pf-2",
	})
	_, moveNoCurrency := harness.client.MovePortfolioFunds(ctx, MovePortfolioFundsRequest{
		Funds: Amount{Value: "1"}, SourcePortfolioUUID: "pf-1", TargetPortfolioUUID: "pf-2",
	})
	_, moveNoSource := harness.client.MovePortfolioFunds(ctx, MovePortfolioFundsRequest{
		Funds: usd, TargetPortfolioUUID: "pf-2",
	})
	_, moveSame := harness.client.MovePortfolioFunds(ctx, MovePortfolioFundsRequest{
		Funds: usd, SourcePortfolioUUID: "pf-1", TargetPortfolioUUID: "pf-1",
	})
	for name, err := range map[string]error{
		"create empty name": createEmpty, "edit missing UUID": editNoUUID, "edit missing name": editNoName,
		"edit unsafe UUID": editBadUUID, "delete empty UUID": deleteEmpty, "delete unsafe UUID": deleteBadUUID,
		"breakdown missing UUID": breakdownNoUUID, "breakdown bad currency": breakdownBadCurrency,
		"move zero value": moveZero, "move missing currency": moveNoCurrency,
		"move missing source": moveNoSource, "move same portfolio": moveSame,
	} {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s error = %v, want validation", name, err)
		}
	}
	if count := harness.requestCount(); count != 0 {
		t.Fatalf("request count = %d, want 0", count)
	}
}
