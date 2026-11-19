package coinone

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

const testCoinTransactionID = "0fec72eb-1e4d-11e9-9ec7-00e04c3600d7"

func TestDepositAddresses(t *testing.T) {
	t.Parallel()

	addresses := `"deposit_addresses":[{"currency":"BTC","address":"btc_address","secondary_address":null},{"currency":"XRP","address":"xrp_address","secondary_address":"189072"}]`
	server := newExtServer(t, successReply(addresses), successReply(addresses))
	client, limiter := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)
	ctx := context.Background()

	selected, err := client.DepositAddresses(ctx, DepositAddressesRequest{Currencies: []string{"BTC", "XRP"}})
	if err != nil || len(selected) != 2 || selected[0].Currency != "BTC" || selected[0].Address != "btc_address" ||
		selected[0].SecondaryAddress != "" || selected[1].SecondaryAddress != "189072" || len(selected[1].Raw) == 0 {
		t.Fatalf("DepositAddresses() = %+v, error = %v", selected, err)
	}
	if _, err = client.DepositAddresses(ctx, DepositAddressesRequest{}); err != nil {
		t.Fatalf("DepositAddresses() without currencies error = %v", err)
	}
	if _, err = client.DepositAddresses(ctx, DepositAddressesRequest{Currencies: []string{"btc"}}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("DepositAddresses() invalid currency error = %v, want ErrValidation", err)
	}

	requests := server.received()
	if len(requests) != 2 {
		t.Fatalf("requests = %+v, want two", requests)
	}
	assertPrivateRequest(t, requests[0], "/v2.1/account/deposit_address", `,"currencies":["BTC","XRP"]`)
	assertPrivateRequest(t, requests[1], "/v2.1/account/deposit_address", "")
	assertLimiterCharged(t, limiter, extPrivateKey)
	assertLimiterUnknown(t, limiter, extOrderKey)
}

func TestKRWTransactionHistory(t *testing.T) {
	t.Parallel()

	history := `"transactions":[{"id":"` + testCoinTransactionID + `","status":"DEPOSIT_COMPLETE","amount":"10000","fee":"0","type":"DEPOSIT","transaction_type":"DEFAULT","created_at":1662108602000}]`
	server := newExtServer(t, successReply(history), successReply(`"transactions":[]`))
	client, limiter := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)
	ctx := context.Background()

	transactions, err := client.KRWTransactionHistory(ctx, KRWTransactionHistoryRequest{
		ToID: testCoinTransactionID, Type: TransferTypeDeposit, Size: 50,
		From: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil || len(transactions) != 1 {
		t.Fatalf("KRWTransactionHistory() = %+v, error = %v", transactions, err)
	}
	transaction := transactions[0]
	if transaction.ID != testCoinTransactionID || transaction.Status != "DEPOSIT_COMPLETE" || transaction.Amount != "10000" ||
		transaction.Fee != "0" || transaction.Type != TransferTypeDeposit || transaction.TransactionType != "DEFAULT" ||
		transaction.CreatedAt != 1662108602000 || len(transaction.Raw) == 0 {
		t.Fatalf("KRWTransactionHistory()[0] = %+v", transaction)
	}
	empty, err := client.KRWTransactionHistory(ctx, KRWTransactionHistoryRequest{})
	if err != nil || len(empty) != 0 {
		t.Fatalf("KRWTransactionHistory() without conditions = %+v, error = %v", empty, err)
	}

	requests := server.received()
	if len(requests) != 2 {
		t.Fatalf("requests = %+v, want two", requests)
	}
	assertPrivateRequest(
		t, requests[0], "/v2.1/transaction/krw/history",
		`,"to_id":"`+testCoinTransactionID+`","is_deposit":true,"size":50,"from_ts":1785542400000,"to_ts":1787529600000`,
	)
	assertPrivateRequest(t, requests[1], "/v2.1/transaction/krw/history", "")
	assertLimiterCharged(t, limiter, extPrivateKey)
	assertLimiterUnknown(t, limiter, extOrderKey)
}

func TestCoinTransactionHistory(t *testing.T) {
	t.Parallel()

	history := `"transactions":[{"id":"` + testCoinTransactionID + `","currency":"BTC","txid":"bb1d723751cc4d312c38adc13d9a45b9a16608328d0b9a10f5e3ebc647d64506","type":"WITHDRAWAL","from_address":"muQoJGAySUJsn1c9iaj9GQitdVLJhnQVnL","from_secondary_address":"153214622","to_address":"n4G1hT3egiBQ6uSU5pLkGjiKJ6XGjS5k1P","to_secondary_address":"","confirmations":3,"amount":"0.121","fee":"0.0001","status":"WITHDRAWAL_SUCCESS","created_at":1662108602000}]`
	server := newExtServer(t, successReply(history), successReply(`"transactions":[]`))
	client, limiter := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)
	ctx := context.Background()

	transactions, err := client.CoinTransactionHistory(ctx, CoinTransactionHistoryRequest{
		Currency: "BTC", Type: TransferTypeWithdrawal, Size: 100,
		From: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil || len(transactions) != 1 {
		t.Fatalf("CoinTransactionHistory() = %+v, error = %v", transactions, err)
	}
	transaction := transactions[0]
	if transaction.ID != testCoinTransactionID || transaction.Currency != "BTC" ||
		transaction.TxID != "bb1d723751cc4d312c38adc13d9a45b9a16608328d0b9a10f5e3ebc647d64506" ||
		transaction.Type != TransferTypeWithdrawal || transaction.FromAddress != "muQoJGAySUJsn1c9iaj9GQitdVLJhnQVnL" ||
		transaction.FromSecondaryAddress != "153214622" || transaction.ToAddress != "n4G1hT3egiBQ6uSU5pLkGjiKJ6XGjS5k1P" ||
		transaction.ToSecondaryAddress != "" || transaction.Confirmations != 3 || transaction.Amount != "0.121" ||
		transaction.Fee != "0.0001" || transaction.Status != "WITHDRAWAL_SUCCESS" || transaction.CreatedAt != 1662108602000 ||
		len(transaction.Raw) == 0 {
		t.Fatalf("CoinTransactionHistory()[0] = %+v", transaction)
	}
	if _, err = client.CoinTransactionHistory(ctx, CoinTransactionHistoryRequest{}); err != nil {
		t.Fatalf("CoinTransactionHistory() without conditions error = %v", err)
	}

	requests := server.received()
	if len(requests) != 2 {
		t.Fatalf("requests = %+v, want two", requests)
	}
	assertPrivateRequest(
		t, requests[0], "/v2.1/transaction/coin/history",
		`,"currency":"BTC","is_deposit":false,"size":100,"from_ts":1782864000000,"to_ts":1785542400000`,
	)
	assertPrivateRequest(t, requests[1], "/v2.1/transaction/coin/history", "")
	assertLimiterCharged(t, limiter, extPrivateKey)
	assertLimiterUnknown(t, limiter, extOrderKey)
}

func TestTransferHistoryRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	server := newExtServer(t)
	client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)
	ctx := context.Background()
	day := 24 * time.Hour

	tests := []struct {
		name    string
		request KRWTransactionHistoryRequest
	}{
		{"unknown type", KRWTransactionHistoryRequest{Type: "BOTH"}},
		{"negative size", KRWTransactionHistoryRequest{Size: -1}},
		{"oversized page", KRWTransactionHistoryRequest{Size: 101}},
		{"cursor with spaces", KRWTransactionHistoryRequest{ToID: " cursor"}},
		{"from in the future", KRWTransactionHistoryRequest{From: extTestNow.Add(time.Hour)}},
		{"to in the future", KRWTransactionHistoryRequest{To: extTestNow.Add(time.Hour)}},
		{"from equals to", KRWTransactionHistoryRequest{From: extTestNow.Add(-day), To: extTestNow.Add(-day)}},
		{"from after to", KRWTransactionHistoryRequest{From: extTestNow.Add(-day), To: extTestNow.Add(-2 * day)}},
		{"range over 90 days", KRWTransactionHistoryRequest{From: extTestNow.Add(-92 * day), To: extTestNow.Add(-day)}},
		{"open range over 90 days", KRWTransactionHistoryRequest{From: extTestNow.Add(-91 * day)}},
		{"to before default from", KRWTransactionHistoryRequest{To: extTestNow.Add(-91 * day)}},
		{"from at the Unix epoch", KRWTransactionHistoryRequest{From: time.Unix(0, 0)}},
	}
	for _, test := range tests {
		_, krwErr := client.KRWTransactionHistory(ctx, test.request)
		if !errors.Is(krwErr, trade.ErrValidation) {
			t.Fatalf("%s: KRWTransactionHistory() error = %v, want ErrValidation", test.name, krwErr)
		}
		_, coinErr := client.CoinTransactionHistory(ctx, CoinTransactionHistoryRequest{
			ToID: test.request.ToID, Type: test.request.Type, Size: test.request.Size,
			From: test.request.From, To: test.request.To,
		})
		if !errors.Is(coinErr, trade.ErrValidation) {
			t.Fatalf("%s: CoinTransactionHistory() error = %v, want ErrValidation", test.name, coinErr)
		}
	}
	_, currencyErr := client.CoinTransactionHistory(ctx, CoinTransactionHistoryRequest{Currency: "btc"})
	if !errors.Is(currencyErr, trade.ErrValidation) {
		t.Fatalf("CoinTransactionHistory() invalid currency error = %v, want ErrValidation", currencyErr)
	}
	if requests := server.received(); len(requests) != 0 {
		t.Fatalf("requests = %+v, want none", requests)
	}

	valid := []KRWTransactionHistoryRequest{
		{},
		{Size: 100, Type: TransferTypeWithdrawal},
		{From: extTestNow.Add(-90 * day)},
		{From: extTestNow.Add(-90 * day), To: extTestNow},
		{To: extTestNow.Add(-day)},
	}
	for index, request := range valid {
		if err := request.validate(extTestNow); err != nil {
			t.Fatalf("valid request %d error = %v", index, err)
		}
	}
}

func TestCoinTransactionDetail(t *testing.T) {
	t.Parallel()

	detail := `"transactions":[{"id":"` + testCoinTransactionID + `","currency":"XRP","txid":"txid-1","type":"WITHDRAWAL","from_address":"rFrom","from_secondary_address":"","to_address":"rTo","to_secondary_address":"189072","confirmations":0,"amount":"10.5","fee":"1","status":"WITHDRAWAL_WAIT","created_at":1662108602000}]`
	server := newExtServer(t, successReply(detail), successReply(`"transactions":[]`))
	client, limiter := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)
	ctx := context.Background()

	transaction, err := client.CoinTransactionDetail(ctx, CoinTransactionDetailRequest{ID: testCoinTransactionID})
	if err != nil {
		t.Fatalf("CoinTransactionDetail() error = %v", err)
	}
	if transaction.ID != testCoinTransactionID || transaction.Currency != "XRP" || transaction.Status != "WITHDRAWAL_WAIT" ||
		transaction.ToSecondaryAddress != "189072" || transaction.Amount != "10.5" || transaction.Fee != "1" ||
		transaction.Type != TransferTypeWithdrawal || len(transaction.Raw) == 0 {
		t.Fatalf("CoinTransactionDetail() = %+v", transaction)
	}
	if _, err = client.CoinTransactionDetail(ctx, CoinTransactionDetailRequest{ID: testCoinTransactionID}); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("CoinTransactionDetail() with no item error = %v, want ErrExchangeUnavailable", err)
	}
	_, missingErr := client.CoinTransactionDetail(ctx, CoinTransactionDetailRequest{})
	_, paddedErr := client.CoinTransactionDetail(ctx, CoinTransactionDetailRequest{ID: " " + testCoinTransactionID})
	for index, err := range []error{missingErr, paddedErr} {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v, want ErrValidation", index, err)
		}
	}

	requests := server.received()
	if len(requests) != 2 {
		t.Fatalf("requests = %+v, want two", requests)
	}
	for _, got := range requests {
		assertPrivateRequest(t, got, "/v2.1/transaction/coin/history/detail", `,"id":"`+testCoinTransactionID+`"`)
	}
	assertLimiterCharged(t, limiter, extPrivateKey)
}

func TestWithdrawalLimit(t *testing.T) {
	t.Parallel()

	server := newExtServer(
		t,
		successReply(`"limit":{"total":"0.07142857","available":"0.062143"}`),
		successReply(`"limit":null`),
		successReply(""),
	)
	client, limiter := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)
	ctx := context.Background()

	limit, err := client.WithdrawalLimit(ctx, WithdrawalLimitRequest{Currency: "BTC"})
	if err != nil || limit.Total != "0.07142857" || limit.Available != "0.062143" || len(limit.Raw) == 0 {
		t.Fatalf("WithdrawalLimit() = %+v, error = %v", limit, err)
	}
	if _, err = client.WithdrawalLimit(ctx, WithdrawalLimitRequest{Currency: "BTC"}); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("WithdrawalLimit() with null limit error = %v, want ErrExchangeUnavailable", err)
	}
	if _, err = client.WithdrawalLimit(ctx, WithdrawalLimitRequest{Currency: "BTC"}); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("WithdrawalLimit() without limit error = %v, want ErrExchangeUnavailable", err)
	}
	_, missingErr := client.WithdrawalLimit(ctx, WithdrawalLimitRequest{})
	_, lowerErr := client.WithdrawalLimit(ctx, WithdrawalLimitRequest{Currency: "btc"})
	for index, err := range []error{missingErr, lowerErr} {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v, want ErrValidation", index, err)
		}
	}

	requests := server.received()
	if len(requests) != 3 {
		t.Fatalf("requests = %+v, want three", requests)
	}
	for _, got := range requests {
		assertPrivateRequest(t, got, "/v2.1/transaction/coin/withdrawal/limit", `,"currency":"BTC"`)
	}
	assertLimiterCharged(t, limiter, extPrivateKey)
}

func TestWithdrawalAddressBook(t *testing.T) {
	t.Parallel()

	book := `"withdrawal_addresses":[{"currency":"BTC","nickname":"nick1","address":"muQoJGAySUJsn1c9iaj9GQitdVLJhnQVnL","secondary_address":"memo","created_at":1662108602000},{"currency":"BTC","nickname":null,"address":"muQoJGAySUJsn1c9iaj9GQitdVLJhnQVcL","secondary_address":null,"created_at":1662108603000}]`
	server := newExtServer(t, successReply(book), successReply(book))
	client, limiter := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)
	ctx := context.Background()

	addresses, err := client.WithdrawalAddressBook(ctx, WithdrawalAddressBookRequest{Currency: "BTC"})
	if err != nil || len(addresses) != 2 {
		t.Fatalf("WithdrawalAddressBook() = %+v, error = %v", addresses, err)
	}
	first, second := addresses[0], addresses[1]
	if first.Currency != "BTC" || first.Nickname != "nick1" || first.Address != "muQoJGAySUJsn1c9iaj9GQitdVLJhnQVnL" ||
		first.SecondaryAddress != "memo" || first.CreatedAt != 1662108602000 || len(first.Raw) == 0 {
		t.Fatalf("WithdrawalAddressBook()[0] = %+v", first)
	}
	if second.Nickname != "" || second.SecondaryAddress != "" || second.CreatedAt != 1662108603000 {
		t.Fatalf("WithdrawalAddressBook()[1] = %+v", second)
	}
	if _, err = client.WithdrawalAddressBook(ctx, WithdrawalAddressBookRequest{}); err != nil {
		t.Fatalf("WithdrawalAddressBook() without currency error = %v", err)
	}
	if _, err = client.WithdrawalAddressBook(ctx, WithdrawalAddressBookRequest{Currency: "btc"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("WithdrawalAddressBook() invalid currency error = %v, want ErrValidation", err)
	}

	requests := server.received()
	if len(requests) != 2 {
		t.Fatalf("requests = %+v, want two", requests)
	}
	assertPrivateRequest(t, requests[0], "/v2.1/transaction/coin/withdrawal/address_book", `,"currency":"BTC"`)
	assertPrivateRequest(t, requests[1], "/v2.1/transaction/coin/withdrawal/address_book", "")
	assertLimiterCharged(t, limiter, extPrivateKey)
}

func TestPortfolioUnsupportedAPIsReportAuthorization(t *testing.T) {
	t.Parallel()

	reply := errorReply(http.StatusOK, "315", "The API does not support a portfolio.")
	server := newExtServer(t, reply, reply, reply, reply, reply, reply)
	client, _ := newExtTestClient(
		t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead, credential.PermissionWithdraw,
	)
	ctx := context.Background()

	_, krwErr := client.KRWTransactionHistory(ctx, KRWTransactionHistoryRequest{})
	_, coinErr := client.CoinTransactionHistory(ctx, CoinTransactionHistoryRequest{})
	_, detailErr := client.CoinTransactionDetail(ctx, CoinTransactionDetailRequest{ID: testCoinTransactionID})
	_, limitErr := client.WithdrawalLimit(ctx, WithdrawalLimitRequest{Currency: "BTC"})
	_, bookErr := client.WithdrawalAddressBook(ctx, WithdrawalAddressBookRequest{})
	_, withdrawErr := client.Withdraw(ctx, WithdrawRequest{Currency: "XRP", Amount: "10", Address: "rAddress"})
	for _, err := range []error{krwErr, coinErr, detailErr, limitErr, bookErr, withdrawErr} {
		assertExchangeError(t, err, trade.ErrorAuthorization, "315")
		if !errors.Is(err, trade.ErrAuthorization) {
			t.Fatalf("error = %v, want ErrAuthorization", err)
		}
	}
	if requests := server.received(); len(requests) != 6 {
		t.Fatalf("requests = %+v, want six", requests)
	}
}

func TestWithdrawSendsSignedWithdrawalRequest(t *testing.T) {
	t.Parallel()

	transaction := `"transaction":{"id":"` + testCoinTransactionID + `","currency":"XRP","address":"rN7n7otQDd6FczFgLdSqtcsAUxDUwphRuF","secondary_address":"189072","amount":"10.5","status":"WITHDRAWAL_REGISTER"}`
	server := newExtServer(
		t,
		successReply(transaction),
		successReply(`"transaction":{"id":"`+testCoinTransactionID+`","currency":"BTC","address":"muQoJGAySUJsn1c9iaj9GQitdVLJhnQVnL","amount":"0.01","status":"WITHDRAWAL_REGISTER"}`),
	)
	provider := &recordingProvider{}
	client, limiter := newExtTestClient(
		t, server.URL, &directSender{}, provider, credential.PermissionRead, credential.PermissionWithdraw,
	)
	ctx := context.Background()

	withdrawal, err := client.Withdraw(ctx, WithdrawRequest{
		Currency: "XRP", Amount: "10.5",
		Address: "rN7n7otQDd6FczFgLdSqtcsAUxDUwphRuF", SecondaryAddress: "189072",
	})
	if err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
	if withdrawal.ID != testCoinTransactionID || withdrawal.Currency != "XRP" || withdrawal.Address != "rN7n7otQDd6FczFgLdSqtcsAUxDUwphRuF" ||
		withdrawal.SecondaryAddress != "189072" || withdrawal.Amount != "10.5" || withdrawal.Status != "WITHDRAWAL_REGISTER" ||
		len(withdrawal.Raw) == 0 {
		t.Fatalf("Withdraw() = %+v", withdrawal)
	}
	plain, err := client.Withdraw(ctx, WithdrawRequest{
		Currency: "BTC", Amount: "0.01", Address: "muQoJGAySUJsn1c9iaj9GQitdVLJhnQVnL",
	})
	if err != nil || plain.SecondaryAddress != "" || plain.Currency != "BTC" {
		t.Fatalf("Withdraw() without secondary address = %+v, error = %v", plain, err)
	}

	requests := server.received()
	if len(requests) != 2 {
		t.Fatalf("requests = %+v, want two", requests)
	}
	assertPrivateRequest(
		t, requests[0], "/v2.1/transaction/coin/withdrawal",
		`,"currency":"XRP","amount":"10.5","address":"rN7n7otQDd6FczFgLdSqtcsAUxDUwphRuF","secondary_address":"189072"`,
	)
	assertPrivateRequest(
		t, requests[1], "/v2.1/transaction/coin/withdrawal",
		`,"currency":"BTC","amount":"0.01","address":"muQoJGAySUJsn1c9iaj9GQitdVLJhnQVnL"`,
	)
	calls, key, secret := provider.snapshot()
	if calls != 2 || !allZero(key) || !allZero(secret) {
		t.Fatalf("provider calls = %d, key zero = %v, secret zero = %v", calls, allZero(key), allZero(secret))
	}
	assertLimiterCharged(t, limiter, extPrivateKey)
	assertLimiterUnknown(t, limiter, extOrderKey)
}

func TestWithdrawRequiresWithdrawPermission(t *testing.T) {
	t.Parallel()

	server := newExtServer(t)
	sender := &directSender{}
	provider := &recordingProvider{}
	noWithdraw, _ := newExtTestClient(
		t, server.URL, sender, provider,
		credential.PermissionRead, credential.PermissionTrade, credential.PermissionTransfer,
	)
	withdrawOnly, _ := newExtTestClient(t, server.URL, sender, provider, credential.PermissionWithdraw)
	ctx := context.Background()

	_, withdrawErr := noWithdraw.Withdraw(ctx, WithdrawRequest{Currency: "XRP", Amount: "10", Address: "rAddress"})
	_, limitErr := withdrawOnly.WithdrawalLimit(ctx, WithdrawalLimitRequest{Currency: "XRP"})
	for index, err := range []error{withdrawErr, limitErr} {
		if !errors.Is(err, trade.ErrAuthorization) || !errors.Is(err, credential.ErrPermissionNotAllowed) {
			t.Fatalf("permission error %d = %v, want ErrAuthorization", index, err)
		}
	}
	if calls, _, _ := provider.snapshot(); calls != 0 || len(sender.snapshot()) != 0 || len(server.received()) != 0 {
		t.Fatalf("provider calls = %d, routes = %v, requests = %v", calls, sender.snapshot(), server.received())
	}
}

func TestWithdrawNeverRetriesAndReportsUnknownState(t *testing.T) {
	t.Parallel()

	request := WithdrawRequest{Currency: "XRP", Amount: "10", Address: "rAddress"}
	networkClient, _ := newExtTestClient(
		t, "http://127.0.0.1", errorSender{}, &recordingProvider{}, credential.PermissionWithdraw,
	)
	if _, err := networkClient.Withdraw(context.Background(), request); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("Withdraw() network error = %v, want ErrUnknownExecutionState", err)
	}

	tests := []struct {
		name  string
		reply extReply
	}{
		{"server error", errorReply(http.StatusInternalServerError, "500", "internal error")},
		{"bad gateway page", extReply{status: http.StatusBadGateway, body: "<html>bad gateway</html>"}},
		{"missing transaction", successReply("")},
		{"null transaction", successReply(`"transaction":null`)},
		{"missing transaction id", successReply(`"transaction":{"currency":"XRP","status":"WITHDRAWAL_REGISTER"}`)},
	}
	for _, test := range tests {
		server := newExtServer(t, test.reply)
		client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionWithdraw)
		_, err := client.Withdraw(context.Background(), request)
		if !errors.Is(err, trade.ErrUnknownExecutionState) {
			t.Fatalf("%s: Withdraw() error = %v, want ErrUnknownExecutionState", test.name, err)
		}
		server.single(t)
	}
}

func TestWithdrawKeepsExchangeRejection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		code     string
		message  string
		category trade.ErrorCategory
	}{
		{"daily limit", "3003", "Exceed daily withdrawal limit", trade.ErrorValidation},
		{"address not registered", "154", "The address is not registered as an API withdrawal address", trade.ErrorValidation},
		{"balance", "157", "Insufficient balance", trade.ErrorInsufficientBalance},
	}
	for _, test := range tests {
		server := newExtServer(t, errorReply(http.StatusOK, test.code, test.message))
		client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionWithdraw)
		_, err := client.Withdraw(context.Background(), WithdrawRequest{Currency: "XRP", Amount: "10", Address: "rAddress"})
		assertExchangeError(t, err, test.category, test.code)
		var apiError *trade.APIError
		if !errors.As(err, &apiError) || apiError.ExchangeMessage != test.message || apiError.Retryable {
			t.Fatalf("%s: Withdraw() error = %#v", test.name, err)
		}
		server.single(t)
	}
}

func TestWithdrawRejectsInvalidRequestsBeforeSending(t *testing.T) {
	t.Parallel()

	server := newExtServer(t)
	provider := &recordingProvider{}
	client, _ := newExtTestClient(t, server.URL, &directSender{}, provider, credential.PermissionWithdraw)

	tests := []struct {
		name    string
		request WithdrawRequest
	}{
		{"missing currency", WithdrawRequest{Amount: "1", Address: "address"}},
		{"lowercase currency", WithdrawRequest{Currency: "xrp", Amount: "1", Address: "address"}},
		{"missing amount", WithdrawRequest{Currency: "XRP", Address: "address"}},
		{"zero amount", WithdrawRequest{Currency: "XRP", Amount: "0.0", Address: "address"}},
		{"negative amount", WithdrawRequest{Currency: "XRP", Amount: "-1", Address: "address"}},
		{"scientific amount", WithdrawRequest{Currency: "XRP", Amount: "1e3", Address: "address"}},
		{"missing address", WithdrawRequest{Currency: "XRP", Amount: "1"}},
		{"padded address", WithdrawRequest{Currency: "XRP", Amount: "1", Address: " address "}},
		{"padded secondary address", WithdrawRequest{Currency: "XRP", Amount: "1", Address: "address", SecondaryAddress: "123 "}},
	}
	for _, test := range tests {
		if _, err := client.Withdraw(context.Background(), test.request); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s: Withdraw() error = %v, want ErrValidation", test.name, err)
		}
	}
	if calls, _, _ := provider.snapshot(); calls != 0 || len(server.received()) != 0 {
		t.Fatalf("provider calls = %d, requests = %v", calls, server.received())
	}
}
