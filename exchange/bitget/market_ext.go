package bitget

import (
	"context"
	"encoding/json"
	"net/url"

	trade "github.com/proven-trade/cex-sdk"
)

// 이 파일의 시장 데이터 메서드는 인증 없는 공개 endpoint이며 응답의 data 필드를
// json.RawMessage로 반환한다. 공식 문서가 JS 렌더링이라 응답 필드를 SDK 계약으로
// 고정하지 않고, 호출자가 필요한 필드만 decode하도록 한다.

// ServerTime은 Bitget 서버 시간을 조회한다. (GET /api/v3/market/time)
func (client *Client) ServerTime(ctx context.Context, options ...trade.RequestOption) (json.RawMessage, error) {
	return client.publicData(ctx, "/api/v3/market/time", nil, options...)
}

// RPIOrderBook은 RPI(Retail Price Improvement) 주문을 포함한 호가를 조회한다.
// (GET /api/v3/market/rpi-orderbook)
func (client *Client) RPIOrderBook(
	ctx context.Context,
	request OrderBookRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.publicData(ctx, "/api/v3/market/rpi-orderbook", request.values(), options...)
}

// OpenInterestRequest는 미결제약정 조회 범위다. Symbol은 선택이다.
type OpenInterestRequest struct {
	Category Category
	Symbol   string
}

// OpenInterest는 미결제약정을 조회한다. (GET /api/v3/market/open-interest)
func (client *Client) OpenInterest(
	ctx context.Context,
	request OpenInterestRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(request.Category); err != nil {
		return nil, err
	}
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return nil, err
	}
	return client.publicData(
		ctx, "/api/v3/market/open-interest", categorySymbolValues(request.Category, request.Symbol), options...,
	)
}

// CurrentFundingRate는 USDT-M 상품의 현재 펀딩비를 조회한다.
// (GET /api/v3/market/current-fund-rate)
func (client *Client) CurrentFundingRate(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateSymbol(symbol); err != nil {
		return nil, err
	}
	return client.publicData(ctx, "/api/v3/market/current-fund-rate", queryValues("symbol", symbol), options...)
}

// FundingRateHistoryRequest는 펀딩비 이력 조회 조건이다.
type FundingRateHistoryRequest struct {
	Category Category
	Symbol   string
	Cursor   string
	Limit    int
}

// FundingRateHistory는 펀딩비 이력을 cursor 기반으로 조회한다.
// (GET /api/v3/market/history-fund-rate)
func (client *Client) FundingRateHistory(
	ctx context.Context,
	request FundingRateHistoryRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(request.Category); err != nil {
		return nil, err
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return nil, err
	}
	if err := validatePage(nil, nil, noRangeLimit, request.Limit, request.Cursor); err != nil {
		return nil, err
	}
	values := categorySymbolValues(request.Category, request.Symbol)
	setIfNotEmpty(values, "cursor", request.Cursor)
	setPositiveInt(values, "limit", request.Limit)
	return client.publicData(ctx, "/api/v3/market/history-fund-rate", values, options...)
}

// PositionTierRequest는 포지션 티어(리스크 한도) 조회 조건이다. Symbol과 Coin은 선택이다.
type PositionTierRequest struct {
	Category Category
	Symbol   string
	Coin     string
}

// PositionTier는 포지션 티어와 리스크 한도를 조회한다. (GET /api/v3/market/position-tier)
func (client *Client) PositionTier(
	ctx context.Context,
	request PositionTierRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(request.Category); err != nil {
		return nil, err
	}
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return nil, err
	}
	if err := validateOptionalText("coin", request.Coin); err != nil {
		return nil, err
	}
	values := categorySymbolValues(request.Category, request.Symbol)
	setIfNotEmpty(values, "coin", request.Coin)
	return client.publicData(ctx, "/api/v3/market/position-tier", values, options...)
}

// OpenInterestLimit는 상품별 미결제약정 한도를 조회한다. (GET /api/v3/market/oi-limit)
func (client *Client) OpenInterestLimit(
	ctx context.Context,
	request OpenInterestRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(request.Category); err != nil {
		return nil, err
	}
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return nil, err
	}
	return client.publicData(
		ctx, "/api/v3/market/oi-limit", categorySymbolValues(request.Category, request.Symbol), options...,
	)
}

// RiskReserveRequest는 리스크 준비금(보험기금) 조회 조건이다. MarginCoin은 선택이다.
type RiskReserveRequest struct {
	Category   Category
	Symbol     string
	MarginCoin string
}

func (request RiskReserveRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return err
	}
	return validateOptionalText("marginCoin", request.MarginCoin)
}

func (request RiskReserveRequest) values() url.Values {
	values := categorySymbolValues(request.Category, request.Symbol)
	setIfNotEmpty(values, "marginCoin", request.MarginCoin)
	return values
}

// RiskReserve는 리스크 준비금을 조회한다. (GET /api/v3/market/risk-reserve)
func (client *Client) RiskReserve(
	ctx context.Context,
	request RiskReserveRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	return client.riskReserve(ctx, "/api/v3/market/risk-reserve", request, options...)
}

// RiskReserveAll은 전체 리스크 준비금을 조회한다. (GET /api/v3/market/risk-reserve-all)
func (client *Client) RiskReserveAll(
	ctx context.Context,
	request RiskReserveRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	return client.riskReserve(ctx, "/api/v3/market/risk-reserve-all", request, options...)
}

// RiskReserveHourly는 시간 단위 리스크 준비금을 조회한다. (GET /api/v3/market/risk-reserve-hour)
func (client *Client) RiskReserveHourly(
	ctx context.Context,
	request RiskReserveRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	return client.riskReserve(ctx, "/api/v3/market/risk-reserve-hour", request, options...)
}

func (client *Client) riskReserve(
	ctx context.Context,
	path string,
	request RiskReserveRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.publicData(ctx, path, request.values(), options...)
}

// DiscountRate는 담보 코인 할인율을 조회한다. (GET /api/v3/market/discount-rate)
func (client *Client) DiscountRate(ctx context.Context, options ...trade.RequestOption) (json.RawMessage, error) {
	return client.publicData(ctx, "/api/v3/market/discount-rate", nil, options...)
}

// IndexComponents는 지수 가격 구성 거래소를 조회한다. (GET /api/v3/market/index-components)
func (client *Client) IndexComponents(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateSymbol(symbol); err != nil {
		return nil, err
	}
	return client.publicData(ctx, "/api/v3/market/index-components", queryValues("symbol", symbol), options...)
}

// ProofOfReserves는 준비금 증명을 조회한다. (GET /api/v3/market/proof-of-reserves)
func (client *Client) ProofOfReserves(ctx context.Context, options ...trade.RequestOption) (json.RawMessage, error) {
	return client.publicData(ctx, "/api/v3/market/proof-of-reserves", nil, options...)
}

// ProofOfReservesSummary는 준비금 증명 요약(POR)을 조회한다. (GET /api/v3/market/por)
func (client *Client) ProofOfReservesSummary(
	ctx context.Context,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	return client.publicData(ctx, "/api/v3/market/por", nil, options...)
}
