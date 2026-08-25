package smoke

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/transport"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

// RealOrderConfirmation은 실제 주문 실행을 허용하는 정확한 동의 문자열이다.
const RealOrderConfirmation = "실제-주문-생성에-동의합니다"

// ErrTradeSmokeFailed는 주문 smoke 검사 또는 정리가 완전히 성공하지 못했음을 나타낸다.
var ErrTradeSmokeFailed = errors.New("trade smoke failed")

// SpotTradeConfig는 실제 post-only 주문 smoke의 안전 한도와 송신 경로를 정의한다.
type SpotTradeConfig struct {
	Client           unified.SpotClient
	EgressVerifier   EgressVerifier
	Market           unified.Market
	EgressRouteID    transport.EgressRouteID
	PublicIPEndpoint string
	CheckTimeout     time.Duration
	Side             unified.Side
	Price            string
	Quantity         string
	MaxNotional      string
	ClientOrderID    string
	Confirmation     string
}

// TradeReport는 실제 주문 생성·조회·취소 검증 결과와 정리 상태다.
type TradeReport struct {
	Version               int                     `json:"version"`
	Kind                  string                  `json:"kind"`
	Exchange              model.ExchangeID        `json:"exchange"`
	Product               string                  `json:"product"`
	Market                MarketEvidence          `json:"market"`
	EgressRouteID         transport.EgressRouteID `json:"egressRouteId"`
	Side                  unified.Side            `json:"side"`
	StartedAt             time.Time               `json:"startedAt"`
	CompletedAt           time.Time               `json:"completedAt"`
	Passed                bool                    `json:"passed"`
	CleanupAttempted      bool                    `json:"cleanupAttempted"`
	CancellationConfirmed bool                    `json:"cancellationConfirmed"`
	Checks                []CheckResult           `json:"checks"`
}

// SpotTradeRunner는 실제 post-only 주문 한 건의 생성·조회·취소 수명주기를 검증한다.
type SpotTradeRunner struct {
	client           unified.SpotClient
	egressVerifier   EgressVerifier
	market           unified.Market
	routeID          transport.EgressRouteID
	publicIPEndpoint string
	checkTimeout     time.Duration
	side             unified.Side
	price            string
	quantity         string
	clientOrderID    string
}

// NewSpotTradeRunner는 동의 문자열, 주문금액 상한과 post-only 입력을 검증한다.
func NewSpotTradeRunner(config SpotTradeConfig) (*SpotTradeRunner, error) {
	if config.Client == nil {
		return nil, fmt.Errorf("smoke Spot client is required")
	}
	if !config.Client.Exchange().Valid() {
		return nil, fmt.Errorf("smoke Spot client exchange is required")
	}
	if config.EgressVerifier == nil {
		return nil, fmt.Errorf("smoke egress verifier is required")
	}
	if err := config.Market.Validate(); err != nil {
		return nil, err
	}
	config.EgressRouteID = transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if config.EgressRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.Side != unified.SideBuy && config.Side != unified.SideSell {
		return nil, fmt.Errorf("trade smoke side must be buy or sell")
	}
	price, err := decimal(config.Price, false)
	if err != nil {
		return nil, fmt.Errorf("trade smoke price must be a positive decimal")
	}
	quantity, err := decimal(config.Quantity, false)
	if err != nil {
		return nil, fmt.Errorf("trade smoke quantity must be a positive decimal")
	}
	maximum, err := decimal(config.MaxNotional, false)
	if err != nil {
		return nil, fmt.Errorf("trade smoke maximum notional must be a positive decimal")
	}
	notional := new(big.Rat).Mul(price, quantity)
	if notional.Cmp(maximum) > 0 {
		return nil, fmt.Errorf("trade smoke order notional exceeds configured maximum")
	}
	if config.ClientOrderID == "" || strings.TrimSpace(config.ClientOrderID) != config.ClientOrderID {
		return nil, fmt.Errorf("trade smoke client order ID is required without surrounding whitespace")
	}
	if config.Confirmation != RealOrderConfirmation {
		return nil, fmt.Errorf("trade smoke real-order confirmation does not match")
	}
	if config.PublicIPEndpoint == "" {
		config.PublicIPEndpoint = DefaultPublicIPEndpoint
	}
	if config.CheckTimeout == 0 {
		config.CheckTimeout = defaultCheckTimeout
	}
	if config.CheckTimeout < 0 {
		return nil, fmt.Errorf("smoke check timeout must be positive")
	}
	return &SpotTradeRunner{
		client: config.Client, egressVerifier: config.EgressVerifier,
		market: config.Market, routeID: config.EgressRouteID,
		publicIPEndpoint: config.PublicIPEndpoint, checkTimeout: config.CheckTimeout,
		side: config.Side, price: config.Price, quantity: config.Quantity,
		clientOrderID: config.ClientOrderID,
	}, nil
}

// Run은 송신 경로와 비관통 호가를 확인한 뒤 실제 주문을 한 번 생성하고 반드시 취소를 시도한다.
func (runner *SpotTradeRunner) Run(ctx context.Context) (TradeReport, error) {
	if ctx == nil {
		return TradeReport{}, fmt.Errorf("smoke context cannot be nil")
	}
	report := TradeReport{
		Version: ReadReportVersion, Kind: "spot_trade", Exchange: runner.client.Exchange(),
		Product: "spot", Market: MarketEvidence{
			Base: runner.market.Base, Quote: runner.market.Quote,
		}, EgressRouteID: runner.routeID, Side: runner.side, StartedAt: time.Now().UTC(),
	}
	report.Checks = append(report.Checks, executeCheck(
		ctx, runner.checkTimeout, "egress_ip", func(checkContext context.Context) (CheckEvidence, error) {
			return verifyEgress(
				checkContext, runner.egressVerifier, runner.routeID, runner.publicIPEndpoint,
			)
		},
	))
	if report.Checks[0].Status != CheckPassed {
		runner.skipTradeChecks(&report, "egress_verification_failed")
		return runner.finish(report)
	}
	report.Checks = append(report.Checks, executeCheck(
		ctx, runner.checkTimeout, "order_book_safety", runner.checkOrderBookSafety,
	))
	if report.Checks[1].Status != CheckPassed {
		runner.skipMutationChecks(&report, "order_book_safety_failed")
		return runner.finish(report)
	}

	identity := unified.OrderRequest{Market: runner.market, ClientOrderID: runner.clientOrderID}
	placeRequestSucceeded := false
	placeResult := executeCheck(
		ctx, runner.checkTimeout, "place_order", func(checkContext context.Context) (CheckEvidence, error) {
			order, err := runner.client.PlaceOrder(
				checkContext,
				unified.PlaceOrderRequest{
					Market: runner.market, Side: runner.side, Type: unified.OrderTypeLimit,
					TimeInForce: unified.TimeInForcePostOnly, Quantity: runner.quantity,
					Price: runner.price, ClientOrderID: runner.clientOrderID,
				},
				runner.requestOptions()...,
			)
			if err != nil {
				return CheckEvidence{}, err
			}
			placeRequestSucceeded = true
			if err := runner.validateOrderIdentity(order); err != nil {
				return CheckEvidence{OrderStatus: string(order.Status)}, err
			}
			if order.ID != "" {
				identity = unified.OrderRequest{Market: runner.market, OrderID: order.ID}
			}
			return CheckEvidence{OrderStatus: string(order.Status)}, nil
		},
	)
	report.Checks = append(report.Checks, placeResult)
	mayExist := placeRequestSucceeded || placeResult.Status == CheckPassed ||
		(placeResult.Failure != nil && placeResult.Failure.Category == trade.ErrorUnknownExecutionState)
	if !mayExist {
		runner.skipAfterPlaceChecks(&report, "order_not_created")
		return runner.finish(report)
	}

	report.Checks = append(report.Checks, executeCheck(
		ctx, runner.checkTimeout, "query_order", func(checkContext context.Context) (CheckEvidence, error) {
			order, err := runner.client.Order(checkContext, identity, runner.requestOptions()...)
			if err != nil {
				return CheckEvidence{}, err
			}
			if err := runner.validateOrderIdentity(order); err != nil {
				return CheckEvidence{OrderStatus: string(order.Status)}, err
			}
			if order.ID != "" {
				identity = unified.OrderRequest{Market: runner.market, OrderID: order.ID}
			}
			return CheckEvidence{OrderStatus: string(order.Status)}, nil
		},
	))

	cleanupContext := context.WithoutCancel(ctx)
	report.CleanupAttempted = true
	report.Checks = append(report.Checks, executeCheck(
		cleanupContext, runner.checkTimeout, "cancel_order", func(checkContext context.Context) (CheckEvidence, error) {
			order, err := runner.client.CancelOrder(checkContext, identity, runner.requestOptions()...)
			if err != nil {
				return CheckEvidence{}, err
			}
			return CheckEvidence{OrderStatus: string(order.Status)}, nil
		},
	))
	report.Checks = append(report.Checks, executeCheck(
		cleanupContext, runner.checkTimeout, "verify_canceled", func(checkContext context.Context) (CheckEvidence, error) {
			order, err := runner.client.Order(checkContext, identity, runner.requestOptions()...)
			if err != nil {
				return CheckEvidence{}, err
			}
			evidence := CheckEvidence{OrderStatus: string(order.Status)}
			if err := runner.validateOrderIdentity(order); err != nil {
				return evidence, err
			}
			if order.Status != unified.OrderStatusCanceled {
				return evidence, invalidEvidence("order_not_canceled")
			}
			if order.ExecutedQuantity == "" || validateDecimal(order.ExecutedQuantity, true) != nil {
				return evidence, invalidEvidence("invalid_executed_quantity")
			}
			executed, _ := decimal(order.ExecutedQuantity, true)
			if executed.Sign() != 0 {
				return evidence, invalidEvidence("order_executed")
			}
			report.CancellationConfirmed = true
			return evidence, nil
		},
	))
	return runner.finish(report)
}

func (runner *SpotTradeRunner) requestOptions() []trade.RequestOption {
	return []trade.RequestOption{
		trade.WithEgressRoute(runner.routeID), trade.WithTimeout(runner.checkTimeout),
	}
}

func (runner *SpotTradeRunner) checkOrderBookSafety(
	ctx context.Context,
) (CheckEvidence, error) {
	book, err := runner.client.OrderBook(
		ctx, unified.OrderBookRequest{Market: runner.market, Limit: 5},
		runner.requestOptions()...,
	)
	evidence := CheckEvidence{Count: len(book.Bids) + len(book.Asks), NativeMarket: book.NativeMarket}
	if err != nil {
		return evidence, err
	}
	if book.Exchange != runner.client.Exchange() || book.Market != runner.market {
		return evidence, invalidEvidence("unexpected_order_book_identity")
	}
	bestBid, bestAsk, err := bestPrices(book)
	if err != nil {
		return evidence, err
	}
	if bestBid.Cmp(bestAsk) >= 0 {
		return evidence, invalidEvidence("crossed_order_book")
	}
	orderPrice, _ := decimal(runner.price, false)
	if runner.side == unified.SideBuy && orderPrice.Cmp(bestAsk) >= 0 {
		return evidence, invalidEvidence("buy_price_crosses_best_ask")
	}
	if runner.side == unified.SideSell && orderPrice.Cmp(bestBid) <= 0 {
		return evidence, invalidEvidence("sell_price_crosses_best_bid")
	}
	return evidence, nil
}

func bestPrices(book unified.OrderBook) (*big.Rat, *big.Rat, error) {
	if len(book.Bids) == 0 || len(book.Asks) == 0 {
		return nil, nil, invalidEvidence("empty_order_book_side")
	}
	var bestBid *big.Rat
	for _, level := range book.Bids {
		price, err := decimal(level.Price, false)
		if err != nil || validateDecimal(level.Quantity, false) != nil {
			return nil, nil, invalidEvidence("invalid_order_book_level")
		}
		if bestBid == nil || price.Cmp(bestBid) > 0 {
			bestBid = price
		}
	}
	var bestAsk *big.Rat
	for _, level := range book.Asks {
		price, err := decimal(level.Price, false)
		if err != nil || validateDecimal(level.Quantity, false) != nil {
			return nil, nil, invalidEvidence("invalid_order_book_level")
		}
		if bestAsk == nil || price.Cmp(bestAsk) < 0 {
			bestAsk = price
		}
	}
	return bestBid, bestAsk, nil
}

func (runner *SpotTradeRunner) validateOrderIdentity(order unified.Order) error {
	if order.Exchange != runner.client.Exchange() || order.Market != runner.market {
		return invalidEvidence("unexpected_order_identity")
	}
	if order.ID == "" && order.ClientOrderID == "" {
		return invalidEvidence("order_identity_missing")
	}
	if order.ClientOrderID != "" && order.ClientOrderID != runner.clientOrderID {
		return invalidEvidence("unexpected_client_order_id")
	}
	return nil
}

func (runner *SpotTradeRunner) skipTradeChecks(report *TradeReport, reason string) {
	report.Checks = append(report.Checks,
		skippedCheck("order_book_safety", reason),
		skippedCheck("place_order", reason),
		skippedCheck("query_order", reason),
		skippedCheck("cancel_order", reason),
		skippedCheck("verify_canceled", reason),
	)
}

func (runner *SpotTradeRunner) skipMutationChecks(report *TradeReport, reason string) {
	report.Checks = append(report.Checks,
		skippedCheck("place_order", reason),
		skippedCheck("query_order", reason),
		skippedCheck("cancel_order", reason),
		skippedCheck("verify_canceled", reason),
	)
}

func (runner *SpotTradeRunner) skipAfterPlaceChecks(report *TradeReport, reason string) {
	report.Checks = append(report.Checks,
		skippedCheck("query_order", reason),
		skippedCheck("cancel_order", reason),
		skippedCheck("verify_canceled", reason),
	)
}

func (runner *SpotTradeRunner) finish(report TradeReport) (TradeReport, error) {
	report.CompletedAt = time.Now().UTC()
	report.Passed = true
	notPassed := 0
	for _, check := range report.Checks {
		if check.Status != CheckPassed {
			report.Passed = false
			notPassed++
		}
	}
	if !report.CancellationConfirmed {
		report.Passed = false
	}
	if !report.Passed {
		return report, fmt.Errorf("%w: %d checks did not pass", ErrTradeSmokeFailed, notPassed)
	}
	return report, nil
}
