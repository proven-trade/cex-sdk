package coinone

import (
	"context"
	"errors"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

func TestBalancesSendsSelectedCurrencies(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, successReply(`"balances":[{"available":"998999692485","limit":"0","average_price":"100000000","currency":"BTC"},{"available":"232706745677","limit":"1000","average_price":"0","currency":"KRW"}]`))
	provider := &recordingProvider{}
	client, limiter := newExtTestClient(t, server.URL, &directSender{}, provider, credential.PermissionRead)

	balances, err := client.Balances(context.Background(), BalancesRequest{Currencies: []string{"BTC", "KRW"}})
	if err != nil {
		t.Fatalf("Balances() error = %v", err)
	}
	assertPrivateRequest(t, server.single(t), "/v2.1/account/balance", `,"currencies":["BTC","KRW"]`)
	if len(balances) != 2 || balances[0].Currency != "BTC" || balances[0].Available != "998999692485" ||
		balances[0].AveragePrice != "100000000" || balances[1].Limit != "1000" || len(balances[1].Raw) == 0 {
		t.Fatalf("Balances() = %+v", balances)
	}
	calls, key, secret := provider.snapshot()
	if calls != 1 || !allZero(key) || !allZero(secret) {
		t.Fatalf("provider calls = %d, key zero = %v, secret zero = %v", calls, allZero(key), allZero(secret))
	}
	assertLimiterUsed(t, limiter, extPrivateKey, 1)
	assertLimiterUnknown(t, limiter, extOrderKey)
}

func TestBalancesRejectsInvalidCurrencies(t *testing.T) {
	t.Parallel()

	server := newExtServer(t)
	client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)

	_, emptyErr := client.Balances(context.Background(), BalancesRequest{})
	_, lowerErr := client.Balances(context.Background(), BalancesRequest{Currencies: []string{"BTC", "krw"}})
	for index, err := range []error{emptyErr, lowerErr} {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v, want ErrValidation", index, err)
		}
	}
	if requests := server.received(); len(requests) != 0 {
		t.Fatalf("requests = %+v, want none", requests)
	}
}

func TestTradeFeesAndTradeFee(t *testing.T) {
	t.Parallel()

	server := newExtServer(
		t,
		successReply(`"fee_rates":[{"quote_currency":"KRW","target_currency":"BTC","maker":"0.001","taker":"0.001"},{"quote_currency":"KRW","target_currency":"XRP","maker":"0.0005","taker":"0.001"}]`),
		successReply(`"fee_rates":[{"quote_currency":"KRW","target_currency":"BTC","maker":"0.001","taker":"0.002"}]`),
		successReply(`"fee_rates":[]`),
	)
	client, limiter := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)
	ctx := context.Background()

	fees, err := client.TradeFees(ctx)
	if err != nil || len(fees) != 2 || fees[1].TargetCurrency != "XRP" || fees[1].Maker != "0.0005" || fees[1].Taker != "0.001" || len(fees[0].Raw) == 0 {
		t.Fatalf("TradeFees() = %+v, error = %v", fees, err)
	}
	fee, err := client.TradeFee(ctx, TradeFeeRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC"})
	if err != nil || fee.QuoteCurrency != "KRW" || fee.TargetCurrency != "BTC" || fee.Maker != "0.001" || fee.Taker != "0.002" || len(fee.Raw) == 0 {
		t.Fatalf("TradeFee() = %+v, error = %v", fee, err)
	}
	if _, err = client.TradeFee(ctx, TradeFeeRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC"}); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("TradeFee() with no item error = %v, want ErrExchangeUnavailable", err)
	}

	requests := server.received()
	if len(requests) != 3 {
		t.Fatalf("requests = %+v, want three", requests)
	}
	assertPrivateRequest(t, requests[0], "/v2.1/account/trade_fee", "")
	assertPrivateRequest(t, requests[1], "/v2.1/account/trade_fee/KRW/BTC", "")
	assertPrivateRequest(t, requests[2], "/v2.1/account/trade_fee/KRW/BTC", "")
	assertLimiterCharged(t, limiter, extPrivateKey)
	assertLimiterUnknown(t, limiter, extOrderKey)
}

func TestTradeFeeRejectsSamePair(t *testing.T) {
	t.Parallel()

	server := newExtServer(t)
	client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)

	_, err := client.TradeFee(context.Background(), TradeFeeRequest{QuoteCurrency: "KRW", TargetCurrency: "KRW"})
	if !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("TradeFee() error = %v, want ErrValidation", err)
	}
	if requests := server.received(); len(requests) != 0 {
		t.Fatalf("requests = %+v, want none", requests)
	}
}

func TestOrderRewardPrograms(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, successReply(`"programs":[{"quote_currency":"KRW","target_currency":"BTC","reward_amount":"200000","contribution_ratio":"0.05","expect_reward_amount":"5000","score":"10.0","total_score":"200.0","is_maintenance":false,"competition_level":"MIDDLE"},{"quote_currency":"KRW","target_currency":"ETH","reward_amount":"100000","contribution_ratio":"0","expect_reward_amount":"0","score":"0","total_score":"80.0","is_maintenance":true,"competition_level":"HIGH"}]`))
	client, limiter := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)

	programs, err := client.OrderRewardPrograms(context.Background())
	if err != nil {
		t.Fatalf("OrderRewardPrograms() error = %v", err)
	}
	assertPrivateRequest(t, server.single(t), "/v2.1/event/order-reward/programs", "")
	if len(programs) != 2 {
		t.Fatalf("OrderRewardPrograms() = %+v", programs)
	}
	first, second := programs[0], programs[1]
	if first.TargetCurrency != "BTC" || first.RewardAmount != "200000" || first.ContributionRatio != "0.05" ||
		first.ExpectedRewardAmount != "5000" || first.Score != "10.0" || first.TotalScore != "200.0" ||
		first.IsMaintenance || first.CompetitionLevel != "MIDDLE" || len(first.Raw) == 0 {
		t.Fatalf("OrderRewardPrograms()[0] = %+v", first)
	}
	if !second.IsMaintenance || second.CompetitionLevel != "HIGH" {
		t.Fatalf("OrderRewardPrograms()[1] = %+v", second)
	}
	assertLimiterUsed(t, limiter, extPrivateKey, 1)
	assertLimiterUnknown(t, limiter, extOrderKey)
}

func TestOrderRewardHistorySendsSecondTimestamps(t *testing.T) {
	t.Parallel()

	history := `"rewards":[{"applied_at":1625097600,"reward_amount":"100","net_reward_amount":"95","deposit_status":"COMPLETE","details":[{"quote_currency":"KRW","target_currency":"BTC","daily_reward_amount":"100","received_reward_amount":"95","score":"10.0","total_score":"200.0","contribution_ratio":"0.05"}]}]`
	server := newExtServer(t, successReply(history), successReply(`"rewards":[]`), successReply(`"rewards":[]`))
	client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)
	ctx := context.Background()

	rewards, err := client.OrderRewardHistory(ctx, OrderRewardHistoryRequest{
		From: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil || len(rewards) != 1 {
		t.Fatalf("OrderRewardHistory() = %+v, error = %v", rewards, err)
	}
	reward := rewards[0]
	if reward.AppliedAt != 1625097600 || reward.RewardAmount != "100" || reward.NetRewardAmount != "95" ||
		reward.DepositStatus != "COMPLETE" || len(reward.Details) != 1 || len(reward.Raw) == 0 {
		t.Fatalf("OrderRewardHistory()[0] = %+v", reward)
	}
	detail := reward.Details[0]
	if detail.QuoteCurrency != "KRW" || detail.TargetCurrency != "BTC" || detail.DailyRewardAmount != "100" ||
		detail.ReceivedRewardAmount != "95" || detail.Score != "10.0" || detail.TotalScore != "200.0" || detail.ContributionRatio != "0.05" {
		t.Fatalf("OrderRewardHistory()[0].Details[0] = %+v", detail)
	}
	if _, err = client.OrderRewardHistory(ctx, OrderRewardHistoryRequest{}); err != nil {
		t.Fatalf("OrderRewardHistory() without period error = %v", err)
	}
	if _, err = client.OrderRewardHistory(ctx, OrderRewardHistoryRequest{To: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("OrderRewardHistory() with only To error = %v", err)
	}

	requests := server.received()
	if len(requests) != 3 {
		t.Fatalf("requests = %+v, want three", requests)
	}
	assertPrivateRequest(t, requests[0], "/v2.1/event/order-reward/history", `,"from_ts":1782864000,"to_ts":1785542400`)
	assertPrivateRequest(t, requests[1], "/v2.1/event/order-reward/history", "")
	assertPrivateRequest(t, requests[2], "/v2.1/event/order-reward/history", `,"to_ts":1785542400`)
}

func TestOrderRewardHistoryRejectsInvalidPeriod(t *testing.T) {
	t.Parallel()

	server := newExtServer(t)
	client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)
	ctx := context.Background()

	_, reversedErr := client.OrderRewardHistory(ctx, OrderRewardHistoryRequest{
		From: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	_, fromEpochErr := client.OrderRewardHistory(ctx, OrderRewardHistoryRequest{From: time.Unix(0, 0)})
	_, toEpochErr := client.OrderRewardHistory(ctx, OrderRewardHistoryRequest{To: time.Unix(-1, 0)})
	for index, err := range []error{reversedErr, fromEpochErr, toEpochErr} {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v, want ErrValidation", index, err)
		}
	}
	if requests := server.received(); len(requests) != 0 {
		t.Fatalf("requests = %+v, want none", requests)
	}
}

func TestAccountExtensionsRequireReadPermission(t *testing.T) {
	t.Parallel()

	server := newExtServer(t)
	sender := &directSender{}
	provider := &recordingProvider{}
	client, _ := newExtTestClient(t, server.URL, sender, provider, credential.PermissionTrade)
	ctx := context.Background()

	_, balancesErr := client.Balances(ctx, BalancesRequest{Currencies: []string{"BTC"}})
	_, feesErr := client.TradeFees(ctx)
	_, programsErr := client.OrderRewardPrograms(ctx)
	_, historyErr := client.OrderRewardHistory(ctx, OrderRewardHistoryRequest{})
	for index, err := range []error{balancesErr, feesErr, programsErr, historyErr} {
		if !errors.Is(err, trade.ErrAuthorization) || !errors.Is(err, credential.ErrPermissionNotAllowed) {
			t.Fatalf("permission error %d = %v, want ErrAuthorization", index, err)
		}
	}
	if calls, _, _ := provider.snapshot(); calls != 0 || len(sender.snapshot()) != 0 || len(server.received()) != 0 {
		t.Fatalf("provider calls = %d, routes = %v, requests = %v", calls, sender.snapshot(), server.received())
	}
}
