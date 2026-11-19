package binance

import (
	"context"
	"encoding/json"
	"net/url"

	trade "github.com/proven-trade/cex-sdk"
)

// ExecutionRules는 상품별 실행 규칙(PRICE_RANGE)을 조회한다.
// Symbol, Symbols, SymbolStatus는 함께 쓸 수 없고 모두 비우면 전체 상품을 조회한다.
func (client *Client) ExecutionRules(
	ctx context.Context,
	request ExecutionRulesRequest,
	options ...trade.RequestOption,
) (ExecutionRules, error) {
	if err := request.validate(); err != nil {
		return ExecutionRules{}, err
	}
	body, err := client.readPublic(ctx, "/api/v3/executionRules", request.values(), request.weight(), options...)
	if err != nil {
		return ExecutionRules{}, err
	}
	var rules ExecutionRules
	if err := decodeJSON(body, &rules); err != nil {
		return ExecutionRules{}, err
	}
	rules.Raw = cloneBytes(body)
	return rules, nil
}

// HistoricalTrades는 지정한 상품의 과거 공개 체결을 조회한다. API Key가 필요 없다.
func (client *Client) HistoricalTrades(
	ctx context.Context,
	request HistoricalTradesRequest,
	options ...trade.RequestOption,
) ([]PublicTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := client.readPublic(ctx, "/api/v3/historicalTrades", request.values(), 25, options...)
	if err != nil {
		return nil, err
	}
	var trades []PublicTrade
	if err := decodeJSON(body, &trades); err != nil {
		return nil, err
	}
	return trades, nil
}

// HistoricalBlockTrades는 지정한 상품의 블록 체결 이력을 FromID부터 조회한다.
func (client *Client) HistoricalBlockTrades(
	ctx context.Context,
	request HistoricalBlockTradesRequest,
	options ...trade.RequestOption,
) ([]BlockTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := client.readPublic(ctx, "/api/v3/historicalBlockTrades", request.values(), 25, options...)
	if err != nil {
		return nil, err
	}
	var trades []BlockTrade
	if err := decodeJSON(body, &trades); err != nil {
		return nil, err
	}
	return trades, nil
}

// AggregateTrades는 같은 taker 주문과 가격으로 합쳐진 집계 체결을 조회한다.
func (client *Client) AggregateTrades(
	ctx context.Context,
	request AggregateTradesRequest,
	options ...trade.RequestOption,
) ([]AggregateTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := client.readPublic(ctx, "/api/v3/aggTrades", request.values(), 4, options...)
	if err != nil {
		return nil, err
	}
	var trades []AggregateTrade
	if err := decodeJSON(body, &trades); err != nil {
		return nil, err
	}
	return trades, nil
}

// UIKlines는 차트 표시에 맞게 보정된 OHLCV 캔들을 조회한다. 요청과 응답 형식은 Klines와 같다.
func (client *Client) UIKlines(
	ctx context.Context,
	request KlinesRequest,
	options ...trade.RequestOption,
) ([]Kline, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := client.readPublic(ctx, "/api/v3/uiKlines", request.values(), 2, options...)
	if err != nil {
		return nil, err
	}
	var klines []Kline
	if err := decodeJSON(body, &klines); err != nil {
		return nil, err
	}
	return klines, nil
}

// AveragePrice는 지정한 상품의 현재 평균가를 조회한다.
func (client *Client) AveragePrice(
	ctx context.Context,
	request AveragePriceRequest,
	options ...trade.RequestOption,
) (AveragePrice, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return AveragePrice{}, err
	}
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	body, err := client.readPublic(ctx, "/api/v3/avgPrice", values, 2, options...)
	if err != nil {
		return AveragePrice{}, err
	}
	var price AveragePrice
	if err := decodeJSON(body, &price); err != nil {
		return AveragePrice{}, err
	}
	price.Raw = cloneBytes(body)
	return price, nil
}

// Ticker24Hour는 24시간 rolling window 가격 변동 통계를 조회한다.
// 상품 하나를 조회해도 원소가 하나인 목록으로 반환한다.
func (client *Client) Ticker24Hour(
	ctx context.Context,
	request Ticker24HourRequest,
	options ...trade.RequestOption,
) ([]TickerStatistics, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := client.readPublic(ctx, "/api/v3/ticker/24hr", request.values(), request.weight(), options...)
	if err != nil {
		return nil, err
	}
	return decodeTickerStatistics(body)
}

// TradingDayTicker는 거래일 기준 가격 변동 통계를 조회한다.
// 상품 하나를 조회해도 원소가 하나인 목록으로 반환한다.
func (client *Client) TradingDayTicker(
	ctx context.Context,
	request TradingDayTickerRequest,
	options ...trade.RequestOption,
) ([]TickerStatistics, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := client.readPublic(ctx, "/api/v3/ticker/tradingDay", request.values(), request.weight(), options...)
	if err != nil {
		return nil, err
	}
	return decodeTickerStatistics(body)
}

// RollingTicker는 지정한 windowSize의 rolling window 가격 변동 통계를 조회한다.
// 상품 하나를 조회해도 원소가 하나인 목록으로 반환한다.
func (client *Client) RollingTicker(
	ctx context.Context,
	request RollingTickerRequest,
	options ...trade.RequestOption,
) ([]TickerStatistics, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := client.readPublic(ctx, "/api/v3/ticker", request.values(), request.weight(), options...)
	if err != nil {
		return nil, err
	}
	return decodeTickerStatistics(body)
}

// TickerPrices는 하나, 여러 개 또는 전체 상품의 최신 가격을 조회한다.
// 상품 하나를 조회해도 원소가 하나인 목록으로 반환한다.
func (client *Client) TickerPrices(
	ctx context.Context,
	request TickerPricesRequest,
	options ...trade.RequestOption,
) ([]TickerPrice, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := client.readPublic(ctx, "/api/v3/ticker/price", request.values(), request.weight(), options...)
	if err != nil {
		return nil, err
	}
	return decodeObjectOrList(body, func(ticker *TickerPrice, raw json.RawMessage) { ticker.Raw = raw })
}

// BookTickers는 하나, 여러 개 또는 전체 상품의 최우선 호가를 조회한다.
// 상품 하나를 조회해도 원소가 하나인 목록으로 반환한다.
func (client *Client) BookTickers(
	ctx context.Context,
	request BookTickersRequest,
	options ...trade.RequestOption,
) ([]BookTicker, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := client.readPublic(ctx, "/api/v3/ticker/bookTicker", request.values(), request.weight(), options...)
	if err != nil {
		return nil, err
	}
	return decodeObjectOrList(body, func(ticker *BookTicker, raw json.RawMessage) { ticker.Raw = raw })
}

// ReferencePrice는 지정한 상품의 기준가격을 조회한다.
// 기준가격이 설정되지 않은 상태면 Price가 nil이고, 한 번도 설정된 적이 없으면 거래소가 -2043 오류로 거부한다.
func (client *Client) ReferencePrice(
	ctx context.Context,
	request ReferencePriceRequest,
	options ...trade.RequestOption,
) (ReferencePrice, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return ReferencePrice{}, err
	}
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	body, err := client.readPublic(ctx, "/api/v3/referencePrice", values, 2, options...)
	if err != nil {
		return ReferencePrice{}, err
	}
	var price ReferencePrice
	if err := decodeJSON(body, &price); err != nil {
		return ReferencePrice{}, err
	}
	price.Raw = cloneBytes(body)
	return price, nil
}

// ReferencePriceCalculation은 지정한 상품의 기준가격 산출 방식을 조회한다.
func (client *Client) ReferencePriceCalculation(
	ctx context.Context,
	request ReferencePriceCalculationRequest,
	options ...trade.RequestOption,
) (ReferencePriceCalculation, error) {
	if err := request.validate(); err != nil {
		return ReferencePriceCalculation{}, err
	}
	body, err := client.readPublic(ctx, "/api/v3/referencePrice/calculation", request.values(), 2, options...)
	if err != nil {
		return ReferencePriceCalculation{}, err
	}
	var calculation ReferencePriceCalculation
	if err := decodeJSON(body, &calculation); err != nil {
		return ReferencePriceCalculation{}, err
	}
	calculation.Raw = cloneBytes(body)
	return calculation, nil
}

func decodeTickerStatistics(body []byte) ([]TickerStatistics, error) {
	return decodeObjectOrList(body, func(ticker *TickerStatistics, raw json.RawMessage) { ticker.Raw = raw })
}
