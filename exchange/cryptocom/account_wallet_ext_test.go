package cryptocom

import (
	"context"
	"errors"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

func TestClientAccountExtensions(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_700_000_000_000)
	start := time.UnixMilli(1_699_900_000_000)
	server, captured := newExtServer(t, now, map[string]string{
		methodGetPositions:          `{"data":[{"account_id":"acc-1","quantity":"-0.1984","cost":"-10159.5735","open_position_pnl":"-497.74","open_pos_cost":"-10159.35","session_pnl":"2.23","update_timestamp_ms":"1613552240770","instrument_name":"BTCUSD-PERP","type":"PERPETUAL_SWAP"},{"account_id":"acc-1","quantity":"1","instrument_name":"BTCUSD-PERP","type":"PERPETUAL_SWAP","isolation_id":"19848526","isolation_type":"ISOLATED_MARGIN"}]}`,
		methodGetTransactions:       `{"data":[{"account_id":"acc-1","event_date":"2021-02-18","journal_type":"TRADING","journal_id":"187078","transaction_qty":"-0.0005","transaction_cost":"-24.5","realized_pnl":"-0.006125","order_id":"72062","trade_id":"71497","trade_match_id":"8625","event_timestamp_ms":"1613640752166","event_timestamp_ns":"1613640752166234567","client_oid":"c-1","taker_side":"MAKER","side":"SELL","instrument_name":"BTCUSD-PERP"}]}`,
		methodGetSubaccountBalances: `{"data":[{"instrument_name":"USD","total_available_balance":"900","total_cash_balance":"1000","position_balances":[{"instrument_name":"USDT","quantity":"1000","reserved_qty":"100"}],"isolated_positions":[]}]}`,
	})
	client := newExtClient(t, server, now, credential.PermissionRead)
	ctx := context.Background()

	positions, err := client.Positions(ctx, PositionsRequest{InstrumentName: "BTCUSD-PERP"})
	if err != nil || len(positions) != 2 || positions[0].Quantity != "-0.1984" ||
		positions[0].OpenPositionPNL != "-497.74" || positions[0].UpdateTime != "1613552240770" ||
		positions[1].IsolationID != "19848526" || len(positions[0].Raw) == 0 ||
		stringParam(captured.envelope(methodGetPositions).Params, "instrument_name") != "BTCUSD-PERP" {
		t.Fatalf("Positions() = %+v, error = %v", positions, err)
	}
	if _, err := client.Positions(ctx, PositionsRequest{InstrumentName: "ETHUSD-PERP"}); err == nil {
		t.Fatal("Positions(inconsistent instrument) error = nil")
	}

	transactions, err := client.Transactions(ctx, TransactionsRequest{
		InstrumentName: "BTCUSD-PERP", JournalType: JournalTrading, Start: &start, Limit: 100,
	})
	params := captured.envelope(methodGetTransactions).Params
	if err != nil || len(transactions) != 1 || transactions[0].JournalID != "187078" ||
		transactions[0].JournalType != JournalTrading || transactions[0].TransactionQty != "-0.0005" ||
		transactions[0].TradeMatchID != "8625" || transactions[0].EventTimestampNS != "1613640752166234567" ||
		stringParam(params, "journal_type") != "TRADING" ||
		stringParam(params, "start_time") != "1699900000000000000" || stringParam(params, "limit") != "100" {
		t.Fatalf("Transactions() = %+v, params = %+v, error = %v", transactions, params, err)
	}

	balances, err := client.SubaccountBalances(ctx)
	if err != nil || len(balances.Accounts) != 1 || balances.Accounts[0].TotalCashBalance != "1000" ||
		len(balances.Accounts[0].PositionBalances) != 1 ||
		balances.Accounts[0].PositionBalances[0].ReservedQuantity != "100" || len(balances.Raw) == 0 {
		t.Fatalf("SubaccountBalances() = %+v, error = %v", balances, err)
	}

	for name, call := range map[string]func() error{
		"bad journal type": func() error {
			_, err := client.Transactions(ctx, TransactionsRequest{JournalType: "NOPE"})
			return err
		},
		"bad isolation id": func() error {
			_, err := client.Transactions(ctx, TransactionsRequest{IsolationID: "x"})
			return err
		},
		"limit too large": func() error {
			_, err := client.Transactions(ctx, TransactionsRequest{Limit: 101})
			return err
		},
		"bad position instrument": func() error {
			_, err := client.Positions(ctx, PositionsRequest{InstrumentName: "btc"})
			return err
		},
	} {
		if err := call(); err == nil {
			t.Fatalf("%s: error = nil", name)
		}
	}
}

func TestClientWalletExtensions(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_700_000_000_000)
	start := time.UnixMilli(1_699_900_000_000)
	server, captured := newExtServer(t, now, map[string]string{
		methodGetDepositAddress:      `{"deposit_address_list":[{"currency":"CRO","create_time":"1615886328000","id":"12345","address":"cro-main-address","status":"1","network":"CRO"},{"currency":"CRO","create_time":1615886332000,"id":"12346","address":"0xerc20","status":"1","network":"ETH"}]}`,
		methodGetDepositHistory:      `{"deposit_list":[{"currency":"XRP","fee":"1.0","create_time":"1607063412000","id":"2220","update_time":"1607063460000","amount":"100","address":"rAddr?1234567890","status":"1"}]}`,
		methodGetWithdrawalHistory:   `{"withdrawal_list":[{"currency":"BTC","client_wid":"w-1","fee":"0.0004","create_time":"1607063412000","id":"2220","update_time":"1607063460000","amount":"1","address":"2NBq","status":"5","txid":"abc","network_id":null}]}`,
		methodGetWithdrawalAddresses: `{"withdrawal_address_list":[{"id":"9","currency":"USDC","network_id":"ETH","address":"0xabc","label":"cold","create_time":"1700000000000","is_owner_of_address":true,"wallet_type":"Others"}]}`,
		methodGetCurrencyNetworks:    `{"update_time":"1641151604000","currency_map":{"AGLD":{"full_name":"Adventure Gold","default_network":null,"network_list":[{"network_id":"ETH","withdrawal_fee":null,"withdraw_enabled":true,"min_withdrawal_amount":"10.0","deposit_enabled":true,"confirmation_required":"12"}]},"MATIC":{"full_name":"Polygon","default_network":"ETH","network_list":[{"network_id":"BNB","withdrawal_fee":"0.80000000","withdraw_enabled":true,"min_withdrawal_amount":"1.6","deposit_enabled":false,"confirmation_required":"0"}]}}}`,
		methodCreateWithdrawal:       `{"id":"2220","amount":"1","fee":"0.0004","symbol":"BTC","address":"2NBq","client_wid":"w-1","create_time":"1607063412000","network_id":null}`,
	})
	client := newExtClient(
		t, server, now, credential.PermissionRead, credential.PermissionWithdraw,
	)
	ctx := context.Background()

	addresses, err := client.DepositAddresses(ctx, DepositAddressRequest{Currency: "CRO"})
	if err != nil || len(addresses) != 2 || addresses[0].Network != "CRO" || addresses[1].Network != "ETH" ||
		addresses[1].CreateTime != "1615886332000" || addresses[0].Address != "cro-main-address" ||
		stringParam(captured.envelope(methodGetDepositAddress).Params, "currency") != "CRO" {
		t.Fatalf("DepositAddresses() = %+v, error = %v", addresses, err)
	}

	deposits, err := client.DepositHistory(ctx, DepositHistoryRequest{
		Currency: "XRP", Status: DepositArrived, Start: &start, PageSize: 50, Page: 1,
	})
	params := captured.envelope(methodGetDepositHistory).Params
	if err != nil || len(deposits) != 1 || deposits[0].Amount != "100" || deposits[0].Status != DepositArrived ||
		stringParam(params, "status") != "1" || stringParam(params, "start_ts") != "1699900000000" ||
		stringParam(params, "page_size") != "50" || stringParam(params, "page") != "1" {
		t.Fatalf("DepositHistory() = %+v, params = %+v, error = %v", deposits, params, err)
	}

	withdrawals, err := client.WithdrawalHistory(ctx, WithdrawalHistoryRequest{Status: WithdrawalCompleted})
	if err != nil || len(withdrawals) != 1 || withdrawals[0].TransactionID != "abc" ||
		withdrawals[0].Status != WithdrawalCompleted || withdrawals[0].ClientWithdrawID != "w-1" ||
		withdrawals[0].NetworkID != "" {
		t.Fatalf("WithdrawalHistory() = %+v, error = %v", withdrawals, err)
	}

	saved, err := client.WithdrawalAddresses(ctx, WithdrawalAddressesRequest{Currency: "USDC", NetworkID: "ETH"})
	if err != nil || len(saved) != 1 || !saved[0].IsOwnerOfAddress || saved[0].Label != "cold" ||
		stringParam(captured.envelope(methodGetWithdrawalAddresses).Params, "network_id") != "ETH" {
		t.Fatalf("WithdrawalAddresses() = %+v, error = %v", saved, err)
	}

	networks, err := client.CurrencyNetworks(ctx)
	matic := networks.Currencies["MATIC"]
	agld := networks.Currencies["AGLD"]
	if err != nil || networks.UpdateTime != "1641151604000" || len(networks.Currencies) != 2 ||
		matic.DefaultNetwork != "ETH" || len(matic.Networks) != 1 || matic.Networks[0].DepositEnabled ||
		matic.Networks[0].WithdrawalFee != "0.80000000" || agld.DefaultNetwork != "" ||
		agld.Networks[0].WithdrawalFee != "" || agld.Networks[0].ConfirmationRequired != 12 {
		t.Fatalf("CurrencyNetworks() = %+v, error = %v", networks, err)
	}

	receipt, err := client.CreateWithdrawal(ctx, CreateWithdrawalRequest{
		Currency: "BTC", Amount: "1", Address: "2NBq", ClientWithdrawID: "w-1",
	})
	params = captured.envelope(methodCreateWithdrawal).Params
	if err != nil || receipt.ID != "2220" || receipt.Fee != "0.0004" || receipt.ClientWithdrawID != "w-1" ||
		stringParam(params, "currency") != "BTC" || stringParam(params, "amount") != "1" ||
		stringParam(params, "address") != "2NBq" || stringParam(params, "client_wid") != "w-1" {
		t.Fatalf("CreateWithdrawal() = %+v, params = %+v, error = %v", receipt, params, err)
	}
}

func TestWalletValidationAndPermissions(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_700_000_000_000)
	server, captured := newExtServer(t, now, map[string]string{})
	ctx := context.Background()

	// withdraw 권한이 없으면 서버에 도달하기 전에 거부한다.
	traderOnly := newExtClient(t, server, now, credential.PermissionTrade, credential.PermissionRead)
	_, err := traderOnly.CreateWithdrawal(ctx, CreateWithdrawalRequest{
		Currency: "BTC", Amount: "1", Address: "addr",
	})
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.Category != trade.ErrorAuthorization {
		t.Fatalf("CreateWithdrawal(no withdraw permission) error = %v, want authorization", err)
	}

	withdrawer := newExtClient(t, server, now, credential.PermissionWithdraw)
	for name, call := range map[string]func() error{
		"zero amount": func() error {
			_, err := withdrawer.CreateWithdrawal(ctx, CreateWithdrawalRequest{Currency: "BTC", Amount: "0", Address: "a"})
			return err
		},
		"missing address": func() error {
			_, err := withdrawer.CreateWithdrawal(ctx, CreateWithdrawalRequest{Currency: "BTC", Amount: "1"})
			return err
		},
		"bad currency": func() error {
			_, err := withdrawer.CreateWithdrawal(ctx, CreateWithdrawalRequest{Currency: "btc", Amount: "1", Address: "a"})
			return err
		},
		"bad network": func() error {
			_, err := withdrawer.CreateWithdrawal(ctx, CreateWithdrawalRequest{
				Currency: "BTC", Amount: "1", Address: "a", NetworkID: "x y",
			})
			return err
		},
		"bad deposit status": func() error {
			_, err := withdrawer.DepositHistory(ctx, DepositHistoryRequest{Status: "9"})
			return err
		},
		"page size too large": func() error {
			_, err := withdrawer.WithdrawalHistory(ctx, WithdrawalHistoryRequest{PageSize: 201})
			return err
		},
		"deposit address without currency": func() error {
			_, err := withdrawer.DepositAddresses(ctx, DepositAddressRequest{})
			return err
		},
	} {
		if err := call(); err == nil {
			t.Fatalf("%s: error = nil", name)
		}
	}
	if len(captured.envelopes) != 0 {
		t.Fatalf("invalid requests reached the server: %+v", captured.envelopes)
	}
}

func TestCreateWithdrawalIdentityMismatchIsUnknownExecutionState(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_700_000_000_000)
	server, captured := newExtServer(t, now, map[string]string{
		methodCreateWithdrawal: `{"id":"1","client_wid":"other","symbol":"BTC","amount":"1","fee":"0"}`,
	})
	client := newExtClient(t, server, now, credential.PermissionWithdraw)
	_, err := client.CreateWithdrawal(context.Background(), CreateWithdrawalRequest{
		Currency: "BTC", Amount: "1", Address: "addr", ClientWithdrawID: "w-1",
	})
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.Category != trade.ErrorUnknownExecutionState {
		t.Fatalf("CreateWithdrawal() error = %v, want unknown execution state", err)
	}
	if captured.callCount(methodCreateWithdrawal) != 1 {
		t.Fatalf("withdrawal sent %d times, want exactly once", captured.callCount(methodCreateWithdrawal))
	}
}
