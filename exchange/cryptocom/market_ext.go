package cryptocom

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	trade "github.com/proven-trade/cex-sdk"
)

const (
	methodGetValuations         = "public/get-valuations"
	methodGetDepositInstruments = "public/get-deposit-instruments"
	methodGetRiskParameters     = "public/get-risk-parameters"
)

// ValuationType은 public/get-valuations가 지원하는 평가 데이터 종류다.
type ValuationType string

const (
	ValuationIndexPrice           ValuationType = "index_price"
	ValuationMarkPrice            ValuationType = "mark_price"
	ValuationFundingHistory       ValuationType = "funding_hist"
	ValuationFundingRate          ValuationType = "funding_rate"
	ValuationEstimatedFundingRate ValuationType = "estimated_funding_rate"
)

// ValuationsRequest는 상품과 평가 종류, 개수·시간 범위를 지정한다.
type ValuationsRequest struct {
	InstrumentName string
	ValuationType  ValuationType
	Count          int
	Start          *time.Time
	End            *time.Time
}

// ValuationPoint는 시각별 평가 값 한 건이다.
type ValuationPoint struct {
	Value     Decimal `json:"v"`
	Timestamp Scalar  `json:"t"`
}

// Valuations는 상품의 평가 데이터 시계열이다.
type Valuations struct {
	InstrumentName string           `json:"instrument_name"`
	Data           []ValuationPoint `json:"data"`
	Raw            json.RawMessage  `json:"-"`
}

// DepositInstrument는 입금과 담보로 사용할 수 있는 상품 한 건이다.
type DepositInstrument struct {
	Symbol             string          `json:"symbol"`
	InstrumentType     string          `json:"inst_type"`
	DisplayName        string          `json:"display_name"`
	BaseCurrency       string          `json:"base_ccy"`
	QuoteCurrency      string          `json:"quote_ccy"`
	QuoteDecimals      Integer         `json:"quote_decimals"`
	QuantityDecimals   Integer         `json:"quantity_decimals"`
	Haircut            Decimal         `json:"haircut"`
	CollateralEligible bool            `json:"collateral_eligible"`
	PriceTickSize      Decimal         `json:"price_tick_size"`
	QuantityTickSize   Decimal         `json:"qty_tick_size"`
	MaximumLeverage    Decimal         `json:"max_leverage"`
	Tradable           bool            `json:"tradable"`
	ExpiryTimestamp    Integer         `json:"expiry_timestamp_ms"`
	BetaProduct        bool            `json:"beta_product"`
	ConversionFee      Decimal         `json:"conversion_fee"`
	MarginBuyEnabled   bool            `json:"margin_buy_enabled"`
	MarginSellEnabled  bool            `json:"margin_sell_enabled"`
	InstrumentID       Scalar          `json:"inst_id"`
	Raw                json.RawMessage `json:"-"`
}

// RiskParameters는 Smart Cross Margin 리스크 파라미터 기본값과 통화별 설정이다.
type RiskParameters struct {
	DefaultMaxLeverageSpot           Decimal              `json:"default_max_product_leverage_for_spot"`
	DefaultMaxLeveragePerps          Decimal              `json:"default_max_product_leverage_for_perps"`
	DefaultMaxLeverageFutures        Decimal              `json:"default_max_product_leverage_for_futures"`
	DefaultUMRMultiplierSpot         Decimal              `json:"default_umr_multiplier_for_spot"`
	DefaultUMRMultiplierPerps        Decimal              `json:"default_umr_multiplier_for_perps"`
	DefaultUMRMultiplierFutures      Decimal              `json:"default_umr_multiplier_for_futures"`
	DefaultLongPositionLimitPerps    Decimal              `json:"default_long_pos_limit_perps"`
	DefaultShortPositionLimitPerps   Decimal              `json:"default_short_pos_limit_perps"`
	DefaultLongPositionLimitFutures  Decimal              `json:"default_long_pos_limit_futures"`
	DefaultShortPositionLimitFutures Decimal              `json:"default_short_pos_limit_futures"`
	DefaultUnitMarginRate            Decimal              `json:"default_unit_margin_rate"`
	DefaultCollateralCap             Decimal              `json:"default_collateral_cap"`
	UpdateTimestamp                  Scalar               `json:"update_timestamp_ms"`
	BaseCurrencyConfig               []RiskCurrencyConfig `json:"base_currency_config"`
	Raw                              json.RawMessage      `json:"-"`
}

// RiskCurrencyConfig는 기준 통화별 리스크 파라미터다. 해당 없는 값은 빈 문자열이다.
type RiskCurrencyConfig struct {
	InstrumentName            string          `json:"instrument_name"`
	CollateralCapNotional     Decimal         `json:"collateral_cap_notional"`
	MinimumHaircut            Decimal         `json:"minimum_haircut"`
	MaxLeverageSpot           Decimal         `json:"max_product_leverage_for_spot"`
	MaxLeveragePerps          Decimal         `json:"max_product_leverage_for_perps"`
	MaxLeverageFutures        Decimal         `json:"max_product_leverage_for_futures"`
	UnitMarginRate            Decimal         `json:"unit_margin_rate"`
	MaxShortSellLimit         Decimal         `json:"max_short_sell_limit"`
	DailyNotionalLimit        Decimal         `json:"daily_notional_limit"`
	OrderLimit                Decimal         `json:"order_limit"`
	MaxOrderNotionalUSD       Decimal         `json:"max_order_notional_usd"`
	MinOrderNotionalUSD       Decimal         `json:"min_order_notional_usd"`
	LongPositionLimitPerps    Decimal         `json:"long_pos_limit_perps"`
	ShortPositionLimitPerps   Decimal         `json:"short_pos_limit_perps"`
	LongPositionLimitFutures  Decimal         `json:"long_pos_limit_futures"`
	ShortPositionLimitFutures Decimal         `json:"short_pos_limit_futures"`
	Raw                       json.RawMessage `json:"-"`
}

func (valuationType ValuationType) valid() bool {
	switch valuationType {
	case ValuationIndexPrice, ValuationMarkPrice, ValuationFundingHistory,
		ValuationFundingRate, ValuationEstimatedFundingRate:
		return true
	default:
		return false
	}
}

func (request ValuationsRequest) validate() error {
	if err := validateSymbol(request.InstrumentName); err != nil {
		return err
	}
	if !request.ValuationType.valid() {
		return validationError("unsupported Crypto.com valuation type %q", request.ValuationType)
	}
	if request.Count < 0 {
		return validationError("valuation count must be positive or zero")
	}
	return validateTimeRange(request.Start, request.End)
}

func (request ValuationsRequest) values() url.Values {
	values := url.Values{
		"instrument_name": {request.InstrumentName},
		"valuation_type":  {string(request.ValuationType)},
	}
	setPositiveInt(values, "count", request.Count)
	setTimeRange(values, request.Start, request.End)
	return values
}

// GetValuations는 지수·마크가·펀딩 등 상품 평가 시계열을 조회한다.
func (client *Client) GetValuations(
	ctx context.Context,
	request ValuationsRequest,
	options ...trade.RequestOption,
) (Valuations, error) {
	if err := request.validate(); err != nil {
		return Valuations{}, err
	}
	response, err := client.executePublic(ctx, methodGetValuations, request.values(), options...)
	if err != nil {
		return Valuations{}, err
	}
	resultRaw, err := client.decodeResult(response, methodGetValuations)
	if err != nil {
		return Valuations{}, err
	}
	var value Valuations
	if err := json.Unmarshal(resultRaw, &value); err != nil {
		return Valuations{}, client.decodeBodyError(response, err)
	}
	if value.InstrumentName != request.InstrumentName {
		return Valuations{}, client.decodeBodyError(
			response, errors.New("Crypto.com valuation instrument is inconsistent"),
		)
	}
	value.Raw = cloneBytes(resultRaw)
	return value, nil
}

// DepositInstruments는 입금과 담보로 사용할 수 있는 상품 목록을 조회한다.
func (client *Client) DepositInstruments(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]DepositInstrument, error) {
	response, err := client.executePublic(ctx, methodGetDepositInstruments, nil, options...)
	if err != nil {
		return nil, err
	}
	resultRaw, err := client.decodeResult(response, methodGetDepositInstruments)
	if err != nil {
		return nil, err
	}
	itemsRaw, err := client.decodePublicItems(response, resultRaw)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems(itemsRaw, func(item *DepositInstrument, raw json.RawMessage) {
		item.Raw = raw
	})
	if err != nil {
		return nil, client.decodeBodyError(response, err)
	}
	for _, item := range items {
		if item.Symbol == "" {
			return nil, client.decodeBodyError(
				response, errors.New("Crypto.com deposit instrument symbol is missing"),
			)
		}
	}
	return items, nil
}

// GetRiskParameters는 Smart Cross Margin 리스크 파라미터를 조회한다.
func (client *Client) GetRiskParameters(
	ctx context.Context,
	options ...trade.RequestOption,
) (RiskParameters, error) {
	response, err := client.executePublic(ctx, methodGetRiskParameters, nil, options...)
	if err != nil {
		return RiskParameters{}, err
	}
	resultRaw, err := client.decodeResult(response, methodGetRiskParameters)
	if err != nil {
		return RiskParameters{}, err
	}
	var value RiskParameters
	if err := json.Unmarshal(resultRaw, &value); err != nil {
		return RiskParameters{}, client.decodeBodyError(response, err)
	}
	var configs struct {
		Items []json.RawMessage `json:"base_currency_config"`
	}
	if err := json.Unmarshal(resultRaw, &configs); err != nil {
		return RiskParameters{}, client.decodeBodyError(response, err)
	}
	for index, itemRaw := range configs.Items {
		if index >= len(value.BaseCurrencyConfig) {
			break
		}
		if value.BaseCurrencyConfig[index].InstrumentName == "" {
			return RiskParameters{}, client.decodeBodyError(
				response, errors.New("Crypto.com risk parameter instrument is missing"),
			)
		}
		value.BaseCurrencyConfig[index].Raw = cloneBytes(itemRaw)
	}
	value.Raw = cloneBytes(resultRaw)
	return value, nil
}
