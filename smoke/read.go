// Package smoke는 실제 거래소와 지정 송신 경로를 대상으로 안전한 운영 검증을 수행한다.
package smoke

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net"
	"regexp"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/transport"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

const (
	// DefaultPublicIPEndpoint는 외부에서 송신 IP를 관측할 기본 주소다.
	DefaultPublicIPEndpoint = "https://api.ipify.org"
	defaultCheckTimeout     = 10 * time.Second
	// ReadReportVersion은 읽기 smoke JSON 증적 스키마 버전이다.
	ReadReportVersion = 2
)

var (
	// ErrReadSmokeFailed는 한 개 이상의 읽기 검사가 실패했음을 나타낸다.
	ErrReadSmokeFailed = errors.New("read smoke failed")
	decimalPattern     = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
)

// EgressVerifier는 선택한 route에서 관측되는 실제 공인 IP를 확인한다.
type EgressVerifier interface {
	VerifyPublicIP(context.Context, transport.EgressRouteID, string) (transport.PublicIPCheck, error)
}

// CheckStatus는 개별 운영 검사의 성공 여부다.
type CheckStatus string

const (
	CheckPassed  CheckStatus = "passed"
	CheckFailed  CheckStatus = "failed"
	CheckSkipped CheckStatus = "skipped"
)

// CheckFailure는 민감한 원본 오류 문자열을 제외한 실패 분류다.
type CheckFailure struct {
	Kind         string              `json:"kind"`
	Reason       string              `json:"reason,omitempty"`
	Category     trade.ErrorCategory `json:"category,omitempty"`
	ExchangeCode string              `json:"exchangeCode,omitempty"`
	HTTPStatus   int                 `json:"httpStatus,omitempty"`
	Retryable    bool                `json:"retryable,omitempty"`
}

// CheckEvidence는 비밀값과 거래 데이터 원문을 제외한 검사 증적이다.
type CheckEvidence struct {
	Count            int    `json:"count,omitempty"`
	NativeMarket     string `json:"nativeMarket,omitempty"`
	OrderStatus      string `json:"orderStatus,omitempty"`
	LocalSourceIP    string `json:"localSourceIp,omitempty"`
	ExpectedPublicIP string `json:"expectedPublicIp,omitempty"`
	ObservedPublicIP string `json:"observedPublicIp,omitempty"`
}

// CheckResult는 개별 운영 검사의 시각·상태·증적을 보존한다.
type CheckResult struct {
	Name           string        `json:"name"`
	Status         CheckStatus   `json:"status"`
	StartedAt      time.Time     `json:"startedAt"`
	CompletedAt    time.Time     `json:"completedAt"`
	DurationMillis int64         `json:"durationMillis"`
	Evidence       CheckEvidence `json:"evidence,omitempty"`
	Failure        *CheckFailure `json:"failure,omitempty"`
}

// MarketEvidence는 증적에 기록할 기준·결제 자산이다.
type MarketEvidence struct {
	Base  string `json:"base"`
	Quote string `json:"quote"`
}

// ReadReport는 Spot 읽기 smoke 한 번의 실행 결과다.
type ReadReport struct {
	Version       int                     `json:"version"`
	Kind          string                  `json:"kind"`
	Exchange      model.ExchangeID        `json:"exchange"`
	Product       string                  `json:"product"`
	Market        MarketEvidence          `json:"market"`
	EgressRouteID transport.EgressRouteID `json:"egressRouteId"`
	PrivateRead   bool                    `json:"privateRead"`
	StartedAt     time.Time               `json:"startedAt"`
	CompletedAt   time.Time               `json:"completedAt"`
	Passed        bool                    `json:"passed"`
	Checks        []CheckResult           `json:"checks"`
}

// SpotReadConfig는 Spot 읽기 smoke의 클라이언트·마켓·송신 경로를 정의한다.
type SpotReadConfig struct {
	Client           unified.SpotClient
	EgressVerifier   EgressVerifier
	Market           unified.Market
	EgressRouteID    transport.EgressRouteID
	PublicIPEndpoint string
	CheckTimeout     time.Duration
	IncludeBalances  bool
}

// SpotReadRunner는 한 거래소의 Spot 읽기 검사를 같은 송신 경로에서 순서대로 실행한다.
type SpotReadRunner struct {
	client           unified.SpotClient
	egressVerifier   EgressVerifier
	market           unified.Market
	routeID          transport.EgressRouteID
	publicIPEndpoint string
	checkTimeout     time.Duration
	includeBalances  bool
}

// NewSpotReadRunner는 설정을 검증하고 재사용 가능한 읽기 smoke 실행기를 만든다.
func NewSpotReadRunner(config SpotReadConfig) (*SpotReadRunner, error) {
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
	if config.PublicIPEndpoint == "" {
		config.PublicIPEndpoint = DefaultPublicIPEndpoint
	}
	if config.CheckTimeout == 0 {
		config.CheckTimeout = defaultCheckTimeout
	}
	if config.CheckTimeout < 0 {
		return nil, fmt.Errorf("smoke check timeout must be positive")
	}
	return &SpotReadRunner{
		client: config.Client, egressVerifier: config.EgressVerifier,
		market: config.Market, routeID: config.EgressRouteID,
		publicIPEndpoint: config.PublicIPEndpoint, checkTimeout: config.CheckTimeout,
		includeBalances: config.IncludeBalances,
	}, nil
}

// Run은 송신 경로, 공개 조회와 선택적 private 잔고 검사를 실행하고 안전한 증적을 반환한다.
func (runner *SpotReadRunner) Run(ctx context.Context) (ReadReport, error) {
	if ctx == nil {
		return ReadReport{}, fmt.Errorf("smoke context cannot be nil")
	}
	startedAt := time.Now().UTC()
	report := ReadReport{
		Version: ReadReportVersion, Kind: "spot_read", Exchange: runner.client.Exchange(),
		Product: "spot", Market: MarketEvidence{
			Base: runner.market.Base, Quote: runner.market.Quote,
		}, EgressRouteID: runner.routeID,
		PrivateRead: runner.includeBalances, StartedAt: startedAt,
	}
	runner.appendCheck(ctx, &report, "egress_ip", runner.checkEgress)
	type namedCheck struct {
		name  string
		check checkFunc
	}
	checks := []namedCheck{
		{name: "markets", check: runner.checkMarkets},
		{name: "ticker", check: runner.checkTicker},
		{name: "order_book", check: runner.checkOrderBook},
		{name: "recent_trades", check: runner.checkRecentTrades},
		{name: "candles", check: runner.checkCandles},
	}
	if runner.includeBalances {
		checks = append(checks, namedCheck{name: "balances", check: runner.checkBalances})
	}
	if report.Checks[0].Status != CheckPassed {
		for _, item := range checks {
			runner.appendSkipped(&report, item.name, "egress_verification_failed")
		}
	} else {
		for _, item := range checks {
			runner.appendCheck(ctx, &report, item.name, item.check)
		}
	}
	report.CompletedAt = time.Now().UTC()
	report.Passed = true
	failed := 0
	for _, check := range report.Checks {
		if check.Status != CheckPassed {
			report.Passed = false
			failed++
		}
	}
	if failed > 0 {
		return report, fmt.Errorf("%w: %d checks did not pass", ErrReadSmokeFailed, failed)
	}
	return report, nil
}

func (runner *SpotReadRunner) appendSkipped(
	report *ReadReport,
	name string,
	reason string,
) {
	report.Checks = append(report.Checks, skippedCheck(name, reason))
}

type checkFunc func(context.Context) (CheckEvidence, error)

func (runner *SpotReadRunner) appendCheck(
	ctx context.Context,
	report *ReadReport,
	name string,
	check checkFunc,
) {
	report.Checks = append(report.Checks, executeCheck(ctx, runner.checkTimeout, name, check))
}

func executeCheck(
	ctx context.Context,
	timeout time.Duration,
	name string,
	check checkFunc,
) CheckResult {
	startedAt := time.Now().UTC()
	checkContext, cancel := context.WithTimeout(ctx, timeout)
	evidence, err := check(checkContext)
	cancel()
	completedAt := time.Now().UTC()
	result := CheckResult{
		Name: name, Status: CheckPassed, StartedAt: startedAt, CompletedAt: completedAt,
		DurationMillis: completedAt.Sub(startedAt).Milliseconds(), Evidence: evidence,
	}
	if err != nil {
		failure := classifyFailure(err)
		result.Status = CheckFailed
		result.Failure = &failure
	}
	return result
}

func skippedCheck(name string, reason string) CheckResult {
	now := time.Now().UTC()
	return CheckResult{
		Name: name, Status: CheckSkipped, StartedAt: now, CompletedAt: now,
		Failure: &CheckFailure{Kind: "prerequisite", Reason: reason},
	}
}

func (runner *SpotReadRunner) requestOptions() []trade.RequestOption {
	return []trade.RequestOption{
		trade.WithEgressRoute(runner.routeID),
		trade.WithTimeout(runner.checkTimeout),
	}
}

func (runner *SpotReadRunner) checkEgress(ctx context.Context) (CheckEvidence, error) {
	return verifyEgress(
		ctx, runner.egressVerifier, runner.routeID, runner.publicIPEndpoint,
	)
}

func verifyEgress(
	ctx context.Context,
	verifier EgressVerifier,
	routeID transport.EgressRouteID,
	endpoint string,
) (CheckEvidence, error) {
	check, err := verifier.VerifyPublicIP(ctx, routeID, endpoint)
	localSourceIP := check.LocalSourceIP
	if localSourceIP == nil {
		localSourceIP = check.LocalPrivateIP
	}
	evidence := CheckEvidence{
		LocalSourceIP:    ipString(localSourceIP),
		ExpectedPublicIP: ipString(check.ExpectedPublicIP),
		ObservedPublicIP: ipString(check.ObservedPublicIP),
	}
	if err != nil {
		return evidence, err
	}
	if check.RouteID != routeID {
		return evidence, invalidEvidence("unexpected_route")
	}
	if check.ExpectedPublicIP == nil {
		return evidence, invalidEvidence("expected_public_ip_missing")
	}
	if check.ObservedPublicIP == nil || !check.MatchesExpected ||
		!check.ObservedPublicIP.Equal(check.ExpectedPublicIP) {
		return evidence, invalidEvidence("public_ip_mismatch")
	}
	return evidence, nil
}

func (runner *SpotReadRunner) checkMarkets(ctx context.Context) (CheckEvidence, error) {
	markets, err := runner.client.Markets(ctx, runner.requestOptions()...)
	evidence := CheckEvidence{Count: len(markets)}
	if err != nil {
		return evidence, err
	}
	for _, market := range markets {
		if market.Market != runner.market {
			continue
		}
		evidence.NativeMarket = market.NativeMarket
		if market.Exchange != runner.client.Exchange() {
			return evidence, invalidEvidence("unexpected_exchange")
		}
		if strings.TrimSpace(market.NativeMarket) == "" {
			return evidence, invalidEvidence("native_market_missing")
		}
		return evidence, nil
	}
	return evidence, invalidEvidence("market_not_found")
}

func (runner *SpotReadRunner) checkTicker(ctx context.Context) (CheckEvidence, error) {
	ticker, err := runner.client.Ticker(
		ctx, unified.TickerRequest{Market: runner.market}, runner.requestOptions()...,
	)
	evidence := CheckEvidence{NativeMarket: ticker.NativeMarket}
	if err != nil {
		return evidence, err
	}
	if ticker.Exchange != runner.client.Exchange() || ticker.Market != runner.market {
		return evidence, invalidEvidence("unexpected_ticker_identity")
	}
	if strings.TrimSpace(ticker.NativeMarket) == "" {
		return evidence, invalidEvidence("native_market_missing")
	}
	if err := validateDecimal(ticker.Price, false); err != nil {
		return evidence, invalidEvidence("invalid_ticker_price")
	}
	return evidence, nil
}

func (runner *SpotReadRunner) checkOrderBook(ctx context.Context) (CheckEvidence, error) {
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
	if strings.TrimSpace(book.NativeMarket) == "" {
		return evidence, invalidEvidence("native_market_missing")
	}
	if len(book.Bids) == 0 || len(book.Asks) == 0 {
		return evidence, invalidEvidence("empty_order_book_side")
	}
	for _, level := range append(append([]unified.BookLevel(nil), book.Bids...), book.Asks...) {
		if validateDecimal(level.Price, false) != nil || validateDecimal(level.Quantity, false) != nil {
			return evidence, invalidEvidence("invalid_order_book_level")
		}
	}
	return evidence, nil
}

func (runner *SpotReadRunner) checkRecentTrades(ctx context.Context) (CheckEvidence, error) {
	trades, err := runner.client.RecentTrades(
		ctx, unified.RecentTradesRequest{Market: runner.market, Limit: 5},
		runner.requestOptions()...,
	)
	evidence := CheckEvidence{Count: len(trades)}
	if err != nil {
		return evidence, err
	}
	if len(trades) == 0 {
		return evidence, invalidEvidence("recent_trades_empty")
	}
	for _, item := range trades {
		if validateDecimal(item.Price, false) != nil || validateDecimal(item.Quantity, false) != nil ||
			(item.Side != unified.SideBuy && item.Side != unified.SideSell) {
			return evidence, invalidEvidence("invalid_recent_trade")
		}
	}
	return evidence, nil
}

func (runner *SpotReadRunner) checkCandles(ctx context.Context) (CheckEvidence, error) {
	candles, err := runner.client.Candles(
		ctx, unified.CandlesRequest{
			Market: runner.market, Interval: unified.Candle1Minute, Limit: 5,
		},
		runner.requestOptions()...,
	)
	evidence := CheckEvidence{Count: len(candles)}
	if err != nil {
		return evidence, err
	}
	if len(candles) == 0 {
		return evidence, invalidEvidence("candles_empty")
	}
	for _, candle := range candles {
		if err := validateCandle(candle); err != nil {
			return evidence, err
		}
	}
	return evidence, nil
}

func (runner *SpotReadRunner) checkBalances(ctx context.Context) (CheckEvidence, error) {
	balances, err := runner.client.Balances(ctx, runner.requestOptions()...)
	evidence := CheckEvidence{Count: len(balances)}
	if err != nil {
		return evidence, err
	}
	for _, balance := range balances {
		if strings.TrimSpace(balance.Asset) == "" ||
			validateDecimal(balance.Available, true) != nil ||
			validateDecimal(balance.Locked, true) != nil {
			return evidence, invalidEvidence("invalid_balance")
		}
	}
	return evidence, nil
}

func validateCandle(candle unified.Candle) error {
	if candle.StartTime <= 0 {
		return invalidEvidence("invalid_candle_time")
	}
	open, err := decimal(candle.Open, false)
	if err != nil {
		return invalidEvidence("invalid_candle_price")
	}
	high, err := decimal(candle.High, false)
	if err != nil {
		return invalidEvidence("invalid_candle_price")
	}
	low, err := decimal(candle.Low, false)
	if err != nil {
		return invalidEvidence("invalid_candle_price")
	}
	closeValue, err := decimal(candle.Close, false)
	if err != nil {
		return invalidEvidence("invalid_candle_price")
	}
	if validateDecimal(candle.Volume, true) != nil {
		return invalidEvidence("invalid_candle_volume")
	}
	if high.Cmp(low) < 0 || high.Cmp(open) < 0 || high.Cmp(closeValue) < 0 ||
		low.Cmp(open) > 0 || low.Cmp(closeValue) > 0 {
		return invalidEvidence("invalid_candle_range")
	}
	return nil
}

func validateDecimal(value string, allowZero bool) error {
	_, err := decimal(value, allowZero)
	return err
}

func decimal(value string, allowZero bool) (*big.Rat, error) {
	if !decimalPattern.MatchString(value) {
		return nil, fmt.Errorf("invalid decimal")
	}
	parsed, ok := new(big.Rat).SetString(value)
	if !ok || parsed.Sign() < 0 || (!allowZero && parsed.Sign() == 0) {
		return nil, fmt.Errorf("invalid decimal")
	}
	return parsed, nil
}

type evidenceError struct {
	reason string
}

func (err *evidenceError) Error() string { return err.reason }

func invalidEvidence(reason string) error {
	return &evidenceError{reason: reason}
}

func classifyFailure(err error) CheckFailure {
	var evidence *evidenceError
	if errors.As(err, &evidence) {
		return CheckFailure{Kind: "evidence", Reason: evidence.reason}
	}
	var apiError *trade.APIError
	if errors.As(err, &apiError) {
		return CheckFailure{
			Kind: "api", Category: apiError.Category,
			ExchangeCode: apiError.ExchangeCode, HTTPStatus: apiError.HTTPStatus,
			Retryable: apiError.Retryable,
		}
	}
	if errors.Is(err, transport.ErrPublicIPMismatch) {
		return CheckFailure{Kind: "egress", Reason: "public_ip_mismatch"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CheckFailure{Kind: "timeout", Category: trade.ErrorTimeout}
	}
	if errors.Is(err, context.Canceled) {
		return CheckFailure{Kind: "canceled"}
	}
	return CheckFailure{Kind: "error", Category: categoryForError(err)}
}

func categoryForError(err error) trade.ErrorCategory {
	categories := []struct {
		target   error
		category trade.ErrorCategory
	}{
		{trade.ErrAuthentication, trade.ErrorAuthentication},
		{trade.ErrAuthorization, trade.ErrorAuthorization},
		{trade.ErrValidation, trade.ErrorValidation},
		{trade.ErrInsufficientBalance, trade.ErrorInsufficientBalance},
		{trade.ErrOrderNotFound, trade.ErrorOrderNotFound},
		{trade.ErrRateLimited, trade.ErrorRateLimited},
		{trade.ErrNetwork, trade.ErrorNetwork},
		{trade.ErrTimeout, trade.ErrorTimeout},
		{trade.ErrUnknownExecutionState, trade.ErrorUnknownExecutionState},
		{trade.ErrExchangeUnavailable, trade.ErrorExchangeUnavailable},
		{trade.ErrUnsupportedCapability, trade.ErrorUnsupportedCapability},
		{trade.ErrInternal, trade.ErrorInternal},
	}
	for _, item := range categories {
		if errors.Is(err, item.target) {
			return item.category
		}
	}
	return trade.ErrorInternal
}

func ipString(value net.IP) string {
	if value == nil {
		return ""
	}
	return value.String()
}
