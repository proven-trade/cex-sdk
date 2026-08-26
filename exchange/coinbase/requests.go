package coinbase

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/model"
)

var (
	positiveDecimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
	pathSegmentPattern     = regexp.MustCompile(`^[0-9A-Za-z._-]+$`)
)

// ProductsRequest는 공개 Spot 상품 목록 조회 조건이다.
type ProductsRequest struct {
	Limit      int
	Offset     int
	ProductIDs []string
}

// OrderBookRequest는 공개 호가 스냅샷 조회 조건이다.
type OrderBookRequest struct {
	ProductID                 string
	Limit                     int
	AggregationPriceIncrement string
}

// MarketTradesRequest는 최근 공개 체결 조회 조건이다.
type MarketTradesRequest struct {
	ProductID string
	Limit     int
	Start     *time.Time
	End       *time.Time
}

// CandlesRequest는 공개 OHLCV 조회 조건이다.
type CandlesRequest struct {
	ProductID   string
	Start       time.Time
	End         time.Time
	Granularity CandleGranularity
	Limit       int
}

// AccountsRequest는 cursor 기반 거래 계정 조회 조건이다.
type AccountsRequest struct {
	Limit             int
	Cursor            string
	RetailPortfolioID string
}

// PlaceOrderRequest는 Spot 시장가 또는 지정가 주문이다.
type PlaceOrderRequest struct {
	ClientOrderID      string             `json:"client_order_id"`
	ProductID          string             `json:"product_id"`
	Side               Side               `json:"side"`
	OrderConfiguration OrderConfiguration `json:"order_configuration"`
}

// CancelOrdersRequest는 최대 100개 주문의 일괄 취소 요청이다.
type CancelOrdersRequest struct {
	OrderIDs []string `json:"order_ids"`
}

// OrdersRequest는 주문 목록 조회 조건이다.
type OrdersRequest struct {
	ProductIDs    []string
	OrderStatuses []string
	Start         *time.Time
	End           *time.Time
	OrderSide     Side
	Limit         int
	Cursor        string
}

// FillsRequest는 계정 체결 목록 조회 조건이다.
type FillsRequest struct {
	OrderIDs   []string
	ProductIDs []string
	Start      *time.Time
	End        *time.Time
	Limit      int
	Cursor     string
}

func (request ProductsRequest) validate() error {
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("product limit must be between 1 and 1000 or zero for default")
	}
	if request.Offset < 0 {
		return validationError("product offset cannot be negative")
	}
	return validateTextList("product ID", request.ProductIDs, 100)
}

func (request ProductsRequest) values() url.Values {
	values := make(url.Values)
	setPositiveInt(values, "limit", request.Limit)
	if request.Offset > 0 {
		values.Set("offset", strconv.Itoa(request.Offset))
	}
	values.Set("product_type", string(ProductTypeSpot))
	addAll(values, "product_ids", request.ProductIDs)
	return values
}

func (request OrderBookRequest) validate() error {
	if err := validateRequiredText("product ID", request.ProductID); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("order book limit must be between 1 and 1000 or zero for default")
	}
	if request.AggregationPriceIncrement != "" && !isPositiveDecimal(request.AggregationPriceIncrement) {
		return validationError("aggregation price increment must be a positive decimal string")
	}
	return nil
}

func (request OrderBookRequest) values() url.Values {
	values := url.Values{"product_id": {request.ProductID}}
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "aggregation_price_increment", request.AggregationPriceIncrement)
	return values
}

func (request MarketTradesRequest) validate() error {
	if err := validateRequiredText("product ID", request.ProductID); err != nil {
		return err
	}
	if request.Limit < 1 || request.Limit > 1000 {
		return validationError("market trade limit must be between 1 and 1000")
	}
	return validateTimeRange(request.Start, request.End)
}

func (request MarketTradesRequest) values() url.Values {
	values := make(url.Values)
	setPositiveInt(values, "limit", request.Limit)
	setUnixTime(values, "start", request.Start)
	setUnixTime(values, "end", request.End)
	return values
}

func (request CandlesRequest) validate() error {
	if err := validateRequiredText("product ID", request.ProductID); err != nil {
		return err
	}
	if request.Start.IsZero() || request.End.IsZero() || !request.Start.Before(request.End) {
		return validationError("candle start and end must define an increasing time range")
	}
	if !request.Granularity.valid() {
		return validationError("unsupported candle granularity %q", request.Granularity)
	}
	if request.Limit < 0 || request.Limit > 350 {
		return validationError("candle limit must be between 1 and 350 or zero for default")
	}
	return nil
}

func (request CandlesRequest) values() url.Values {
	values := url.Values{
		"start":       {strconv.FormatInt(request.Start.Unix(), 10)},
		"end":         {strconv.FormatInt(request.End.Unix(), 10)},
		"granularity": {string(request.Granularity)},
	}
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request AccountsRequest) validate() error {
	if request.Limit < 0 || request.Limit > 250 {
		return validationError("account limit must be between 1 and 250 or zero for default")
	}
	if err := validateOptionalText("cursor", request.Cursor); err != nil {
		return err
	}
	return validateOptionalText("retail portfolio ID", request.RetailPortfolioID)
}

func (request AccountsRequest) values() url.Values {
	values := make(url.Values)
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "cursor", request.Cursor)
	setIfNotEmpty(values, "retail_portfolio_id", request.RetailPortfolioID)
	return values
}

func (request PlaceOrderRequest) validate() error {
	if err := validateRequiredText("client order ID", request.ClientOrderID); err != nil {
		return err
	}
	if len(request.ClientOrderID) > 64 {
		return validationError("client order ID cannot exceed 64 bytes")
	}
	if err := validateRequiredText("product ID", request.ProductID); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("order side must be BUY or SELL")
	}
	return request.OrderConfiguration.validate()
}

func (configuration OrderConfiguration) validate() error {
	count := 0
	if configuration.MarketMarketIOC != nil {
		count++
		market := configuration.MarketMarketIOC
		if (market.QuoteSize == "") == (market.BaseSize == "") {
			return validationError("market order must set exactly one of quote size or base size")
		}
		if (market.QuoteSize != "" && !isPositiveDecimal(market.QuoteSize)) ||
			(market.BaseSize != "" && !isPositiveDecimal(market.BaseSize)) {
			return validationError("market order size must be a positive decimal string")
		}
	}
	if configuration.LimitLimitGTC != nil {
		count++
		limit := configuration.LimitLimitGTC
		if !isPositiveDecimal(limit.BaseSize) || !isPositiveDecimal(limit.LimitPrice) {
			return validationError("limit order size and price must be positive decimal strings")
		}
	}
	if configuration.SORLimitIOC != nil {
		count++
		limit := configuration.SORLimitIOC
		if !isPositiveDecimal(limit.BaseSize) || !isPositiveDecimal(limit.LimitPrice) {
			return validationError("SOR IOC order size and price must be positive decimal strings")
		}
	}
	if configuration.LimitLimitFOK != nil {
		count++
		limit := configuration.LimitLimitFOK
		if !isPositiveDecimal(limit.BaseSize) || !isPositiveDecimal(limit.LimitPrice) {
			return validationError("FOK order size and price must be positive decimal strings")
		}
	}
	if count != 1 {
		return validationError("exactly one supported order configuration is required")
	}
	return nil
}

func (request CancelOrdersRequest) validate() error {
	if len(request.OrderIDs) == 0 || len(request.OrderIDs) > 100 {
		return validationError("cancel request requires between 1 and 100 order IDs")
	}
	return validateTextList("order ID", request.OrderIDs, 100)
}

func (request OrdersRequest) validate() error {
	if err := validateTextList("product ID", request.ProductIDs, 100); err != nil {
		return err
	}
	if err := validateTextList("order status", request.OrderStatuses, 20); err != nil {
		return err
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	if request.OrderSide != "" && request.OrderSide != SideBuy && request.OrderSide != SideSell {
		return validationError("order side must be BUY, SELL, or empty")
	}
	return validatePage(request.Limit, 1000, request.Cursor)
}

func (request OrdersRequest) values() url.Values {
	values := make(url.Values)
	addAll(values, "product_ids", request.ProductIDs)
	addAll(values, "order_status", request.OrderStatuses)
	setTime(values, "start_date", request.Start)
	setTime(values, "end_date", request.End)
	setIfNotEmpty(values, "order_side", string(request.OrderSide))
	values.Set("product_type", string(ProductTypeSpot))
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "cursor", request.Cursor)
	return values
}

func (request FillsRequest) validate() error {
	if err := validateTextList("order ID", request.OrderIDs, 100); err != nil {
		return err
	}
	if err := validateTextList("product ID", request.ProductIDs, 100); err != nil {
		return err
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	return validatePage(request.Limit, 1000, request.Cursor)
}

func (request FillsRequest) values() url.Values {
	values := make(url.Values)
	addAll(values, "order_ids", request.OrderIDs)
	addAll(values, "product_ids", request.ProductIDs)
	setTime(values, "start_sequence_timestamp", request.Start)
	setTime(values, "end_sequence_timestamp", request.End)
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "cursor", request.Cursor)
	return values
}

func (granularity CandleGranularity) valid() bool {
	switch granularity {
	case Candle1Minute, Candle5Minutes, Candle15Minutes, Candle30Minutes,
		Candle1Hour, Candle2Hours, Candle4Hours, Candle6Hours, Candle1Day:
		return true
	default:
		return false
	}
}

func validatePage(limit, maximum int, cursor string) error {
	if limit < 0 || limit > maximum {
		return validationError("page limit must be between 1 and %d or zero for default", maximum)
	}
	return validateOptionalText("cursor", cursor)
}

func validateTimeRange(start, end *time.Time) error {
	if start != nil && end != nil && start.After(*end) {
		return validationError("start time cannot be after end time")
	}
	return nil
}

func validateTextList(name string, values []string, maximum int) error {
	if len(values) > maximum {
		return validationError("%s list cannot exceed %d items", name, maximum)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateRequiredText(name, value); err != nil {
			return err
		}
		if _, exists := seen[value]; exists {
			return validationError("duplicate %s %q", name, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateRequiredText(name, value string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\x00") {
		return validationError("%s is required without surrounding whitespace or control characters", name)
	}
	return nil
}

func validateOptionalText(name, value string) error {
	if value == "" {
		return nil
	}
	return validateRequiredText(name, value)
}

func addAll(values url.Values, key string, items []string) {
	for _, item := range items {
		values.Add(key, item)
	}
}

func setPositiveInt(values url.Values, key string, value int) {
	if value > 0 {
		values.Set(key, strconv.Itoa(value))
	}
}

func setIfNotEmpty(values url.Values, key, value string) {
	if value != "" {
		values.Set(key, value)
	}
}

func setTime(values url.Values, key string, value *time.Time) {
	if value != nil {
		values.Set(key, value.UTC().Format(time.RFC3339))
	}
}

func setUnixTime(values url.Values, key string, value *time.Time) {
	if value != nil {
		values.Set(key, strconv.FormatInt(value.Unix(), 10))
	}
}

func isPositiveDecimal(value string) bool {
	return positiveDecimalPattern.MatchString(value) && strings.Trim(value, "0.") != ""
}

func validationError(format string, arguments ...any) error {
	return &trade.APIError{
		Category: trade.ErrorValidation, Exchange: model.ExchangeCoinbase,
		Cause: fmt.Errorf(format, arguments...),
	}
}
