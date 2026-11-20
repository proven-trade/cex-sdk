package futures

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// MarkPrice는 Futures 계약의 현재 마크가격과 지수가격이다.
type MarkPrice struct {
	Symbol      string          `json:"symbol"`
	Granularity int64           `json:"granularity"`
	TimePoint   int64           `json:"timePoint"`
	Value       Decimal         `json:"value"`
	IndexPrice  Decimal         `json:"indexPrice"`
	Raw         json.RawMessage `json:"-"`
}

// IndexComponent는 현물 지수가격을 구성하는 거래소별 가격과 가중치다.
type IndexComponent struct {
	Exchange string  `json:"exchange"`
	Price    Decimal `json:"price"`
	Weight   Decimal `json:"weight"`
}

// IndexPoint는 지수·이자율·프리미엄 지수 이력의 한 시점이다.
// Components는 현물 지수가격 응답에만 채워진다.
type IndexPoint struct {
	Symbol      string           `json:"symbol"`
	Granularity int64            `json:"granularity"`
	TimePoint   int64            `json:"timePoint"`
	Value       Decimal          `json:"value"`
	Components  []IndexComponent `json:"decomposionList"`
	Raw         json.RawMessage  `json:"-"`
}

// IndexPointPage는 offset 기반 지수 이력 한 페이지다.
type IndexPointPage struct {
	Points  []IndexPoint
	HasMore bool
	Raw     json.RawMessage
}

// IndexHistoryRequest는 현물 지수·이자율·프리미엄 지수 이력 조회 조건이다.
// Symbol은 `.KXBTUSDT`, `.XBTINT`, `.XBTUSDTMPI`처럼 계약 정보의 지수 symbol이다.
// Reverse와 Forward를 nil로 두면 거래소 기본값(true)을 사용하고, MaxCount는 1~100이며 0이면 기본값 10을 사용한다.
type IndexHistoryRequest struct {
	Symbol   string
	StartAt  *time.Time
	EndAt    *time.Time
	Reverse  *bool
	Forward  *bool
	Offset   int64
	MaxCount int
}

// TradeStatistics는 Futures 24시간 거래 통계다.
type TradeStatistics struct {
	Turnover24h Decimal         `json:"turnoverOf24h"`
	Raw         json.RawMessage `json:"-"`
}

// ServiceStatus는 Futures 서비스 상태다. Status는 open, close, cancelonly 중 하나다.
type ServiceStatus struct {
	Status  string          `json:"status"`
	Message string          `json:"msg"`
	Raw     json.RawMessage `json:"-"`
}

// CurrentFundingRate는 계약의 현재 펀딩비율과 예상 펀딩비율이다.
type CurrentFundingRate struct {
	Symbol           string          `json:"symbol"`
	Granularity      int64           `json:"granularity"`
	TimePoint        int64           `json:"timePoint"`
	Value            Decimal         `json:"value"`
	PredictedValue   Decimal         `json:"predictedValue"`
	FundingRateCap   Decimal         `json:"fundingRateCap"`
	FundingRateFloor Decimal         `json:"fundingRateFloor"`
	Period           int64           `json:"period"`
	FundingTime      int64           `json:"fundingTime"`
	Raw              json.RawMessage `json:"-"`
}

// FundingRatePoint는 공개 펀딩비율 이력의 한 시점이다.
type FundingRatePoint struct {
	Symbol      string          `json:"symbol"`
	FundingRate Decimal         `json:"fundingRate"`
	TimePoint   int64           `json:"timepoint"`
	Raw         json.RawMessage `json:"-"`
}

// FundingRateHistoryRequest는 공개 펀딩비율 이력 조회 구간이다. From과 To는 모두 필수다.
type FundingRateHistoryRequest struct {
	Symbol string
	From   time.Time
	To     time.Time
}

// AllTickers는 모든 Futures 계약의 최근 체결과 최우선 호가를 조회한다.
func (client *Client) AllTickers(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]Ticker, error) {
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/allTickers", nil, publicLimit(5), options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList(client, response, commonexchange.OperationRead,
		func(value *Ticker, raw json.RawMessage) { value.Raw = raw })
}

// FullOrderBook은 지정한 Futures 계약의 전체 호가 snapshot을 조회한다.
func (client *Client) FullOrderBook(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := validateSymbol(symbol); err != nil {
		return OrderBook{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/level2/snapshot", url.Values{"symbol": {symbol}},
		publicLimit(3), options...,
	)
	if err != nil {
		return OrderBook{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead,
		func(value *OrderBook, raw json.RawMessage) { value.Raw = raw })
}

// MarkPrice는 지정한 Futures 계약의 현재 마크가격과 지수가격을 조회한다.
func (client *Client) MarkPrice(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (MarkPrice, error) {
	if err := validateSymbol(symbol); err != nil {
		return MarkPrice{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/mark-price/"+url.PathEscape(symbol)+"/current", nil,
		publicLimit(3), options...,
	)
	if err != nil {
		return MarkPrice{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead,
		func(value *MarkPrice, raw json.RawMessage) { value.Raw = raw })
}

// SpotIndexPrices는 현물 지수가격 이력을 구성 거래소별 가격과 함께 조회한다.
func (client *Client) SpotIndexPrices(
	ctx context.Context,
	request IndexHistoryRequest,
	options ...trade.RequestOption,
) (IndexPointPage, error) {
	return client.indexHistory(ctx, "/api/v1/index/query", 2, request, options...)
}

// InterestRates는 이자율 지수 이력을 조회한다.
func (client *Client) InterestRates(
	ctx context.Context,
	request IndexHistoryRequest,
	options ...trade.RequestOption,
) (IndexPointPage, error) {
	return client.indexHistory(ctx, "/api/v1/interest/query", 5, request, options...)
}

// PremiumIndexes는 프리미엄 지수 이력을 조회한다.
func (client *Client) PremiumIndexes(
	ctx context.Context,
	request IndexHistoryRequest,
	options ...trade.RequestOption,
) (IndexPointPage, error) {
	return client.indexHistory(ctx, "/api/v1/premium/query", 3, request, options...)
}

func (client *Client) indexHistory(
	ctx context.Context,
	path string,
	weight int,
	request IndexHistoryRequest,
	options ...trade.RequestOption,
) (IndexPointPage, error) {
	if err := request.validate(); err != nil {
		return IndexPointPage{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, path, request.values(), publicLimit(weight), options...,
	)
	if err != nil {
		return IndexPointPage{}, err
	}
	var rawPage struct {
		DataList []json.RawMessage `json:"dataList"`
		HasMore  bool              `json:"hasMore"`
	}
	data, err := client.decodeData(response, commonexchange.OperationRead, &rawPage)
	if err != nil {
		return IndexPointPage{}, err
	}
	points, err := decodeRawItems(client, response, commonexchange.OperationRead, rawPage.DataList,
		func(value *IndexPoint, raw json.RawMessage) { value.Raw = raw })
	if err != nil {
		return IndexPointPage{}, err
	}
	return IndexPointPage{Points: points, HasMore: rawPage.HasMore, Raw: cloneBytes(data)}, nil
}

// TradeStatistics는 Futures 24시간 거래 통계를 조회한다.
// 공식 문서는 Futures pool weight 3을 표기하지만 인증 없는 public 요청이므로 route별 Public pool에서 차감한다.
func (client *Client) TradeStatistics(
	ctx context.Context,
	options ...trade.RequestOption,
) (TradeStatistics, error) {
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/trade-statistics", nil, publicLimit(3), options...,
	)
	if err != nil {
		return TradeStatistics{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead,
		func(value *TradeStatistics, raw json.RawMessage) { value.Raw = raw })
}

// ServerTime은 Futures 서버 시각을 조회한다.
func (client *Client) ServerTime(
	ctx context.Context,
	options ...trade.RequestOption,
) (time.Time, error) {
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/timestamp", nil, publicLimit(2), options...,
	)
	if err != nil {
		return time.Time{}, err
	}
	var millis int64
	if _, err := client.decodeData(response, commonexchange.OperationRead, &millis); err != nil {
		return time.Time{}, err
	}
	if millis <= 0 {
		return time.Time{}, client.decodeBodyError(
			response, commonexchange.OperationRead, validationError("KuCoin Futures server time is not positive"),
		)
	}
	return time.UnixMilli(millis), nil
}

// ServiceStatus는 Futures 서비스 상태를 조회한다.
func (client *Client) ServiceStatus(
	ctx context.Context,
	options ...trade.RequestOption,
) (ServiceStatus, error) {
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/status", nil, publicLimit(4), options...,
	)
	if err != nil {
		return ServiceStatus{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead,
		func(value *ServiceStatus, raw json.RawMessage) { value.Raw = raw })
}

// CurrentFundingRate는 지정한 계약의 현재 펀딩비율을 조회한다.
func (client *Client) CurrentFundingRate(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (CurrentFundingRate, error) {
	if err := validateSymbol(symbol); err != nil {
		return CurrentFundingRate{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/funding-rate/"+url.PathEscape(symbol)+"/current", nil,
		publicLimit(2), options...,
	)
	if err != nil {
		return CurrentFundingRate{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead,
		func(value *CurrentFundingRate, raw json.RawMessage) { value.Raw = raw })
}

// FundingRateHistory는 지정한 구간의 공개 펀딩비율 이력을 조회한다.
func (client *Client) FundingRateHistory(
	ctx context.Context,
	request FundingRateHistoryRequest,
	options ...trade.RequestOption,
) ([]FundingRatePoint, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/contract/funding-rates", request.values(),
		publicLimit(5), options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList(client, response, commonexchange.OperationRead,
		func(value *FundingRatePoint, raw json.RawMessage) { value.Raw = raw })
}

func (request IndexHistoryRequest) validate() error {
	if err := validateIndexSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateTimeRange("index history", request.StartAt, request.EndAt); err != nil {
		return err
	}
	if request.Offset < 0 {
		return validationError("index history offset cannot be negative")
	}
	if request.MaxCount < 0 || request.MaxCount > 100 {
		return validationError("index history max count must be between 1 and 100 or zero for default")
	}
	return nil
}

func (request IndexHistoryRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setMillis(values, "startAt", request.StartAt)
	setMillis(values, "endAt", request.EndAt)
	setBool(values, "reverse", request.Reverse)
	setBool(values, "forward", request.Forward)
	setPositiveInt64(values, "offset", request.Offset)
	setPositiveInt(values, "maxCount", request.MaxCount)
	return values
}

func (request FundingRateHistoryRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.From.UnixMilli() <= 0 || request.To.UnixMilli() <= 0 || !request.From.Before(request.To) {
		return validationError("funding rate history range must be after the Unix epoch with from before to")
	}
	return nil
}

func (request FundingRateHistoryRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setMillis(values, "from", &request.From)
	setMillis(values, "to", &request.To)
	return values
}
