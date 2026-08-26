package coinone

import (
	"bytes"
	"encoding/json"
	"fmt"

	corestream "github.com/proven-trade/cex-sdk/stream"
)

type streamWireMessage struct {
	ResponseType      string          `json:"response_type"`
	ShortResponseType string          `json:"r"`
	Channel           StreamChannel   `json:"channel"`
	ShortChannel      StreamChannel   `json:"c"`
	ErrorCode         int             `json:"error_code"`
	Message           string          `json:"message"`
	Data              json.RawMessage `json:"data"`
	ShortData         json.RawMessage `json:"d"`
}

// DecodeStreamMessage는 DEFAULT 또는 SHORT WebSocket frame을 분류한다.
func DecodeStreamMessage(message corestream.Message) (StreamMessage, error) {
	trimmed := bytes.TrimSpace(message.Data)
	if len(trimmed) == 0 || !json.Valid(trimmed) || trimmed[0] == '[' {
		return StreamMessage{}, fmt.Errorf("invalid Coinone stream JSON object")
	}
	var wire streamWireMessage
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return StreamMessage{}, fmt.Errorf("decode Coinone stream envelope: %w", err)
	}
	short := wire.ResponseType == "" && wire.ShortResponseType != ""
	responseType := wire.ResponseType
	channel := wire.Channel
	data := wire.Data
	if short {
		responseType = wire.ShortResponseType
		channel = wire.ShortChannel
		data = wire.ShortData
	}
	if responseType == "" {
		return StreamMessage{}, fmt.Errorf("Coinone stream message has no response type")
	}
	return StreamMessage{
		ResponseType: responseType, Channel: channel,
		ErrorCode: wire.ErrorCode, ErrorMessage: wire.Message,
		Data: cloneBytes(data), Short: short, Raw: cloneBytes(trimmed),
	}, nil
}

func decodeShortStreamData(channel StreamChannel, data []byte, target any) error {
	var err error
	switch value := target.(type) {
	case *StreamOrderBook:
		var wire shortStreamOrderBook
		err = json.Unmarshal(data, &wire)
		if err == nil {
			*value = wire.normalized()
		}
	case *StreamTicker:
		var wire shortStreamTicker
		err = json.Unmarshal(data, &wire)
		if err == nil {
			*value = wire.normalized()
		}
	case *StreamTrade:
		var wire shortStreamTrade
		err = json.Unmarshal(data, &wire)
		if err == nil {
			*value = wire.normalized()
		}
	case *StreamCandle:
		var wire shortStreamCandle
		err = json.Unmarshal(data, &wire)
		if err == nil {
			*value = wire.normalized()
		}
	case *MyOrderEvent:
		var wire shortMyOrderEvent
		err = json.Unmarshal(data, &wire)
		if err == nil {
			*value = wire.normalized()
		}
	case *MyAssetEvent:
		var wire shortMyAssetEvent
		err = json.Unmarshal(data, &wire)
		if err == nil {
			*value = wire.normalized()
		}
	default:
		err = json.Unmarshal(data, target)
	}
	if err != nil {
		return fmt.Errorf("decode Coinone SHORT %s event: %w", channel, err)
	}
	return nil
}

type shortOrderBookLevel struct {
	Price    Decimal `json:"p"`
	Quantity Decimal `json:"q"`
}

type shortStreamOrderBook struct {
	QuoteCurrency  string                `json:"qc"`
	TargetCurrency string                `json:"tc"`
	Timestamp      int64                 `json:"t"`
	ID             string                `json:"i"`
	Asks           []shortOrderBookLevel `json:"a"`
	Bids           []shortOrderBookLevel `json:"b"`
}

func (wire shortStreamOrderBook) normalized() StreamOrderBook {
	return StreamOrderBook{
		QuoteCurrency: wire.QuoteCurrency, TargetCurrency: wire.TargetCurrency,
		Timestamp: wire.Timestamp, ID: wire.ID,
		Asks: normalizeShortLevels(wire.Asks), Bids: normalizeShortLevels(wire.Bids),
	}
}

func normalizeShortLevels(values []shortOrderBookLevel) []OrderBookLevel {
	result := make([]OrderBookLevel, len(values))
	for index, value := range values {
		result[index] = OrderBookLevel{Price: value.Price, Quantity: value.Quantity}
	}
	return result
}

type shortStreamTicker struct {
	QuoteCurrency         string  `json:"qc"`
	TargetCurrency        string  `json:"tc"`
	Timestamp             int64   `json:"t"`
	QuoteVolume           Decimal `json:"qv"`
	TargetVolume          Decimal `json:"tv"`
	High                  Decimal `json:"hi"`
	Low                   Decimal `json:"lo"`
	First                 Decimal `json:"fi"`
	Last                  Decimal `json:"la"`
	VolumePower           Decimal `json:"vp"`
	AskBestPrice          Decimal `json:"abp"`
	AskBestQuantity       Decimal `json:"abq"`
	BidBestPrice          Decimal `json:"bbp"`
	BidBestQuantity       Decimal `json:"bbq"`
	ID                    string  `json:"i"`
	YesterdayHigh         Decimal `json:"yhi"`
	YesterdayLow          Decimal `json:"ylo"`
	YesterdayFirst        Decimal `json:"yfi"`
	YesterdayLast         Decimal `json:"yla"`
	YesterdayQuoteVolume  Decimal `json:"yqv"`
	YesterdayTargetVolume Decimal `json:"ytv"`
}

func (wire shortStreamTicker) normalized() StreamTicker {
	return StreamTicker{
		QuoteCurrency: wire.QuoteCurrency, TargetCurrency: wire.TargetCurrency,
		Timestamp: wire.Timestamp, QuoteVolume: wire.QuoteVolume, TargetVolume: wire.TargetVolume,
		High: wire.High, Low: wire.Low, First: wire.First, Last: wire.Last, VolumePower: wire.VolumePower,
		AskBestPrice: wire.AskBestPrice, AskBestQuantity: wire.AskBestQuantity,
		BidBestPrice: wire.BidBestPrice, BidBestQuantity: wire.BidBestQuantity, ID: wire.ID,
		YesterdayHigh: wire.YesterdayHigh, YesterdayLow: wire.YesterdayLow,
		YesterdayFirst: wire.YesterdayFirst, YesterdayLast: wire.YesterdayLast,
		YesterdayQuoteVolume:  wire.YesterdayQuoteVolume,
		YesterdayTargetVolume: wire.YesterdayTargetVolume,
	}
}

type shortStreamTrade struct {
	QuoteCurrency  string  `json:"qc"`
	TargetCurrency string  `json:"tc"`
	ID             string  `json:"i"`
	Timestamp      int64   `json:"t"`
	Price          Decimal `json:"p"`
	Quantity       Decimal `json:"q"`
	IsSellerMaker  bool    `json:"sm"`
}

func (wire shortStreamTrade) normalized() StreamTrade {
	return StreamTrade{
		QuoteCurrency: wire.QuoteCurrency, TargetCurrency: wire.TargetCurrency,
		ID: wire.ID, Timestamp: wire.Timestamp, Price: wire.Price,
		Quantity: wire.Quantity, IsSellerMaker: wire.IsSellerMaker,
	}
}

type shortStreamCandle struct {
	QuoteCurrency   string         `json:"qc"`
	TargetCurrency  string         `json:"tc"`
	Interval        CandleInterval `json:"iv"`
	Timestamp       int64          `json:"t"`
	ID              string         `json:"i"`
	CandleTimestamp int64          `json:"ct"`
	High            Decimal        `json:"hi"`
	Low             Decimal        `json:"lo"`
	First           Decimal        `json:"fi"`
	Last            Decimal        `json:"la"`
	QuoteVolume     Decimal        `json:"qv"`
	TargetVolume    Decimal        `json:"tv"`
}

func (wire shortStreamCandle) normalized() StreamCandle {
	return StreamCandle{
		QuoteCurrency: wire.QuoteCurrency, TargetCurrency: wire.TargetCurrency,
		Interval: wire.Interval, Timestamp: wire.Timestamp, ID: wire.ID,
		CandleTimestamp: wire.CandleTimestamp, High: wire.High, Low: wire.Low,
		First: wire.First, Last: wire.Last, QuoteVolume: wire.QuoteVolume,
		TargetVolume: wire.TargetVolume,
	}
}

type shortMyOrderEvent struct {
	QuoteCurrency     string            `json:"qc"`
	TargetCurrency    string            `json:"tc"`
	OrderID           string            `json:"oi"`
	Type              OrderType         `json:"t"`
	Status            StreamOrderStatus `json:"st"`
	Side              StreamOrderSide   `json:"s"`
	OrderPrice        Decimal           `json:"op"`
	OrderQuantity     Decimal           `json:"oq"`
	OrderAmount       Decimal           `json:"oa"`
	TradeID           string            `json:"ti"`
	IsMaker           *bool             `json:"im"`
	ExecutedPrice     Decimal           `json:"ep"`
	ExecutedQuantity  Decimal           `json:"eq"`
	ExecutedFee       Decimal           `json:"ef"`
	RemainingQuantity Decimal           `json:"rq"`
	RemainingAmount   Decimal           `json:"ra"`
	UserOrderID       string            `json:"ui"`
	PreventedQuantity Decimal           `json:"pq"`
	ExecutedTimestamp *int64            `json:"et"`
	OrderTimestamp    *int64            `json:"ot"`
	Timestamp         int64             `json:"ts"`
}

func (wire shortMyOrderEvent) normalized() MyOrderEvent {
	return MyOrderEvent{
		QuoteCurrency: wire.QuoteCurrency, TargetCurrency: wire.TargetCurrency,
		OrderID: wire.OrderID, Type: wire.Type, Status: wire.Status, Side: wire.Side,
		OrderPrice: wire.OrderPrice, OrderQuantity: wire.OrderQuantity, OrderAmount: wire.OrderAmount,
		TradeID: wire.TradeID, IsMaker: wire.IsMaker, ExecutedPrice: wire.ExecutedPrice,
		ExecutedQuantity: wire.ExecutedQuantity, ExecutedFee: wire.ExecutedFee,
		RemainingQuantity: wire.RemainingQuantity, RemainingAmount: wire.RemainingAmount,
		UserOrderID: wire.UserOrderID, PreventedQuantity: wire.PreventedQuantity,
		ExecutedTimestamp: wire.ExecutedTimestamp, OrderTimestamp: wire.OrderTimestamp,
		Timestamp: wire.Timestamp,
	}
}

type shortStreamAsset struct {
	Currency  string  `json:"c"`
	Available Decimal `json:"a"`
	Limit     Decimal `json:"l"`
}

type shortMyAssetEvent struct {
	OrderID     string             `json:"oi"`
	UserOrderID string             `json:"ui"`
	TradeID     string             `json:"ti"`
	Assets      []shortStreamAsset `json:"as"`
	Type        string             `json:"t"`
	Timestamp   int64              `json:"ts"`
}

func (wire shortMyAssetEvent) normalized() MyAssetEvent {
	assets := make([]StreamAsset, len(wire.Assets))
	for index, asset := range wire.Assets {
		assets[index] = StreamAsset{Currency: asset.Currency, Available: asset.Available, Limit: asset.Limit}
	}
	return MyAssetEvent{
		OrderID: wire.OrderID, UserOrderID: wire.UserOrderID, TradeID: wire.TradeID,
		Assets: assets, Type: wire.Type, Timestamp: wire.Timestamp,
	}
}
