package kucoin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// CurrencyChain은 통화의 체인별 입출금 규칙이다.
type CurrencyChain struct {
	ChainName         string          `json:"chainName"`
	ChainID           string          `json:"chainId"`
	WithdrawalMinSize DecimalString   `json:"withdrawalMinSize"`
	DepositMinSize    DecimalString   `json:"depositMinSize"`
	WithdrawFeeRate   DecimalString   `json:"withdrawFeeRate"`
	WithdrawalMinFee  DecimalString   `json:"withdrawalMinFee"`
	WithdrawMaxFee    DecimalString   `json:"withdrawMaxFee"`
	MaxWithdraw       DecimalString   `json:"maxWithdraw"`
	MaxDeposit        DecimalString   `json:"maxDeposit"`
	DepositFeeRate    DecimalString   `json:"depositFeeRate"`
	WithdrawEnabled   bool            `json:"isWithdrawEnabled"`
	DepositEnabled    bool            `json:"isDepositEnabled"`
	Confirms          int             `json:"confirms"`
	PreConfirms       int             `json:"preConfirms"`
	ContractAddress   string          `json:"contractAddress"`
	WithdrawPrecision int             `json:"withdrawPrecision"`
	NeedTag           bool            `json:"needTag"`
	Raw               json.RawMessage `json:"-"`
}

// Currency는 통화 상세와 체인별 입출금 규칙이다.
type Currency struct {
	Currency        string          `json:"currency"`
	Name            string          `json:"name"`
	FullName        string          `json:"fullName"`
	Precision       int             `json:"precision"`
	Confirms        int             `json:"confirms"`
	ContractAddress string          `json:"contractAddress"`
	MarginEnabled   bool            `json:"isMarginEnabled"`
	DebitEnabled    bool            `json:"isDebitEnabled"`
	Chains          []CurrencyChain `json:"chains"`
	Raw             json.RawMessage `json:"-"`
}

// CurrencyRequest는 단일 통화 조회 조건이다. Chain은 다중 체인 통화에서만 사용한다.
type CurrencyRequest struct {
	Currency string
	Chain    string
}

func (request CurrencyRequest) validate() error {
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	return validateOptionalChain(request.Chain)
}

func (request CurrencyRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "chain", request.Chain)
	return values
}

// SymbolDetail은 단일 거래쌍의 주문 단위 규칙과 콜옥션 설정이다.
type SymbolDetail struct {
	Symbol                      string          `json:"symbol"`
	Name                        string          `json:"name"`
	BaseCurrency                string          `json:"baseCurrency"`
	QuoteCurrency               string          `json:"quoteCurrency"`
	FeeCurrency                 string          `json:"feeCurrency"`
	Market                      string          `json:"market"`
	BaseMinimumSize             string          `json:"baseMinSize"`
	QuoteMinimumSize            string          `json:"quoteMinSize"`
	BaseMaximumSize             string          `json:"baseMaxSize"`
	QuoteMaximumSize            string          `json:"quoteMaxSize"`
	BaseIncrement               string          `json:"baseIncrement"`
	QuoteIncrement              string          `json:"quoteIncrement"`
	PriceIncrement              string          `json:"priceIncrement"`
	PriceLimitRate              string          `json:"priceLimitRate"`
	MinimumFunds                DecimalString   `json:"minFunds"`
	MarginEnabled               bool            `json:"isMarginEnabled"`
	TradingEnabled              bool            `json:"enableTrading"`
	FeeCategory                 int             `json:"feeCategory"`
	MakerFeeCoefficient         DecimalString   `json:"makerFeeCoefficient"`
	TakerFeeCoefficient         DecimalString   `json:"takerFeeCoefficient"`
	SpecialTreatment            bool            `json:"st"`
	CallAuctionEnabled          bool            `json:"callauctionIsEnabled"`
	CallAuctionPriceFloor       DecimalString   `json:"callauctionPriceFloor"`
	CallAuctionPriceCeiling     DecimalString   `json:"callauctionPriceCeiling"`
	CallAuctionFirstStageStart  int64           `json:"callauctionFirstStageStartTime"`
	CallAuctionSecondStageStart int64           `json:"callauctionSecondStageStartTime"`
	CallAuctionThirdStageStart  int64           `json:"callauctionThirdStageStartTime"`
	TradingStartTime            int64           `json:"tradingStartTime"`
	Raw                         json.RawMessage `json:"-"`
}

// MarketTicker는 allTickers가 반환하는 거래쌍별 24시간 시세다.
type MarketTicker struct {
	Symbol           string          `json:"symbol"`
	SymbolName       string          `json:"symbolName"`
	BestBid          DecimalString   `json:"buy"`
	BestBidSize      DecimalString   `json:"bestBidSize"`
	BestAsk          DecimalString   `json:"sell"`
	BestAskSize      DecimalString   `json:"bestAskSize"`
	ChangeRate       DecimalString   `json:"changeRate"`
	ChangePrice      DecimalString   `json:"changePrice"`
	High             DecimalString   `json:"high"`
	Low              DecimalString   `json:"low"`
	Volume           DecimalString   `json:"vol"`
	VolumeValue      DecimalString   `json:"volValue"`
	Last             DecimalString   `json:"last"`
	AveragePrice     DecimalString   `json:"averagePrice"`
	TakerFeeRate     DecimalString   `json:"takerFeeRate"`
	MakerFeeRate     DecimalString   `json:"makerFeeRate"`
	TakerCoefficient DecimalString   `json:"takerCoefficient"`
	MakerCoefficient DecimalString   `json:"makerCoefficient"`
	Raw              json.RawMessage `json:"-"`
}

// AllTickers는 전체 거래쌍 24시간 시세 스냅샷이다.
type AllTickers struct {
	Time    int64           `json:"time"`
	Tickers []MarketTicker  `json:"-"`
	Raw     json.RawMessage `json:"-"`
}

// MarketStats는 단일 거래쌍의 24시간 통계다.
type MarketStats struct {
	Time             int64           `json:"time"`
	Symbol           string          `json:"symbol"`
	BestBid          DecimalString   `json:"buy"`
	BestAsk          DecimalString   `json:"sell"`
	ChangeRate       DecimalString   `json:"changeRate"`
	ChangePrice      DecimalString   `json:"changePrice"`
	High             DecimalString   `json:"high"`
	Low              DecimalString   `json:"low"`
	Volume           DecimalString   `json:"vol"`
	VolumeValue      DecimalString   `json:"volValue"`
	Last             DecimalString   `json:"last"`
	AveragePrice     DecimalString   `json:"averagePrice"`
	TakerFeeRate     DecimalString   `json:"takerFeeRate"`
	MakerFeeRate     DecimalString   `json:"makerFeeRate"`
	TakerCoefficient DecimalString   `json:"takerCoefficient"`
	MakerCoefficient DecimalString   `json:"makerCoefficient"`
	Raw              json.RawMessage `json:"-"`
}

// ServerTime은 KuCoin 서버 시각이다.
type ServerTime struct {
	Millis int64
	Time   time.Time
	Raw    json.RawMessage
}

// ServiceStatus는 KuCoin 서비스 상태다. Status는 open, close, cancelonly 중 하나다.
type ServiceStatus struct {
	Status  string          `json:"status"`
	Message string          `json:"msg"`
	Raw     json.RawMessage `json:"-"`
}

// Currencies는 모든 통화와 체인별 입출금 규칙을 조회한다.
func (client *Client) Currencies(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]Currency, error) {
	response, err := client.executePublic(ctx, http.MethodGet, "/api/v3/currencies", nil, publicLimit(3), options...)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *Currency, raw json.RawMessage) {
		item.Raw = raw
	})
}

// Currency는 통화 한 개의 상세와 체인별 입출금 규칙을 조회한다.
func (client *Client) Currency(
	ctx context.Context,
	request CurrencyRequest,
	options ...trade.RequestOption,
) (Currency, error) {
	if err := request.validate(); err != nil {
		return Currency{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v3/currencies/"+url.PathEscape(request.Currency), request.values(),
		publicLimit(3), options...,
	)
	if err != nil {
		return Currency{}, err
	}
	// 공식 스키마는 배열, 예시는 객체이므로 두 형태를 모두 받는다.
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return Currency{}, err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil || len(items) != 1 {
			if err == nil {
				err = fmt.Errorf("KuCoin currency response has %d items, want 1", len(items))
			}
			return Currency{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		trimmed = items[0]
	}
	var currency Currency
	if len(trimmed) == 0 || json.Unmarshal(trimmed, &currency) != nil {
		return Currency{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("invalid KuCoin currency response"),
		)
	}
	currency.Raw = cloneBytes(trimmed)
	return currency, nil
}

// SymbolInfo는 거래쌍 한 개의 주문 단위 규칙을 조회한다.
func (client *Client) SymbolInfo(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (SymbolDetail, error) {
	if err := validateSymbol(symbol); err != nil {
		return SymbolDetail{}, err
	}
	var detail SymbolDetail
	data, err := client.readPublic(
		ctx, "/api/v2/symbols/"+url.PathEscape(symbol), nil, publicLimit(4), &detail, options...,
	)
	if err != nil {
		return SymbolDetail{}, err
	}
	detail.Raw = cloneBytes(data)
	return detail, nil
}

// AllTickers는 전체 거래쌍의 24시간 시세를 한 번에 조회한다.
func (client *Client) AllTickers(
	ctx context.Context,
	options ...trade.RequestOption,
) (AllTickers, error) {
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/market/allTickers", nil, publicLimit(15), options...,
	)
	if err != nil {
		return AllTickers{}, err
	}
	var wire struct {
		Time    int64             `json:"time"`
		Tickers []json.RawMessage `json:"ticker"`
	}
	data, err := client.decodeData(response, commonexchange.OperationRead, &wire)
	if err != nil {
		return AllTickers{}, err
	}
	result := AllTickers{Time: wire.Time, Tickers: make([]MarketTicker, len(wire.Tickers)), Raw: cloneBytes(data)}
	for index, raw := range wire.Tickers {
		if err := json.Unmarshal(raw, &result.Tickers[index]); err != nil {
			return AllTickers{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		result.Tickers[index].Raw = cloneBytes(raw)
	}
	return result, nil
}

// MarketStats는 거래쌍 한 개의 24시간 통계를 조회한다.
func (client *Client) MarketStats(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (MarketStats, error) {
	if err := validateSymbol(symbol); err != nil {
		return MarketStats{}, err
	}
	var stats MarketStats
	data, err := client.readPublic(
		ctx, "/api/v1/market/stats", url.Values{"symbol": {symbol}}, publicLimit(15), &stats, options...,
	)
	if err != nil {
		return MarketStats{}, err
	}
	stats.Raw = cloneBytes(data)
	return stats, nil
}

// FullOrderBook은 전체 깊이의 합산 호가를 조회한다. 인증이 필요하다.
func (client *Client) FullOrderBook(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := validateSymbol(symbol); err != nil {
		return OrderBook{}, err
	}
	var book OrderBook
	data, err := client.readPrivate(
		ctx, "/api/v3/market/orderbook/level2", url.Values{"symbol": {symbol}}, spotLimit(3), &book, options...,
	)
	if err != nil {
		return OrderBook{}, err
	}
	book.Raw = cloneBytes(data)
	return book, nil
}

// ServerTime은 KuCoin 서버 시각을 밀리초로 조회한다.
func (client *Client) ServerTime(
	ctx context.Context,
	options ...trade.RequestOption,
) (ServerTime, error) {
	var millis int64
	data, err := client.readPublic(ctx, "/api/v1/timestamp", nil, publicLimit(3), &millis, options...)
	if err != nil {
		return ServerTime{}, err
	}
	return ServerTime{Millis: millis, Time: time.UnixMilli(millis), Raw: cloneBytes(data)}, nil
}

// ServiceStatus는 KuCoin 서비스 상태를 조회한다.
func (client *Client) ServiceStatus(
	ctx context.Context,
	options ...trade.RequestOption,
) (ServiceStatus, error) {
	var status ServiceStatus
	data, err := client.readPublic(ctx, "/api/v1/status", nil, publicLimit(3), &status, options...)
	if err != nil {
		return ServiceStatus{}, err
	}
	status.Raw = cloneBytes(data)
	return status, nil
}
