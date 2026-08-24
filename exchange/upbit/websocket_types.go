package upbit

import (
	"encoding/json"
	"fmt"
)

// StreamFormat은 업비트 WebSocket 응답 필드와 묶음 형식이다.
type StreamFormat string

const (
	StreamFormatDefault    StreamFormat = "DEFAULT"
	StreamFormatSimple     StreamFormat = "SIMPLE"
	StreamFormatJSONList   StreamFormat = "JSON_LIST"
	StreamFormatSimpleList StreamFormat = "SIMPLE_LIST"
)

// StreamDataType은 구독할 데이터 종류와 마켓 목록이다.
type StreamDataType struct {
	Type         string   `json:"type"`
	Codes        []string `json:"codes,omitempty"`
	Level        Decimal  `json:"level,omitempty"`
	OnlySnapshot bool     `json:"is_only_snapshot,omitempty"`
	OnlyRealtime bool     `json:"is_only_realtime,omitempty"`
}

// MarshalJSON은 호가 모아보기 level을 정밀도 손실 없는 JSON 숫자로 변환한다.
func (dataType StreamDataType) MarshalJSON() ([]byte, error) {
	var level json.RawMessage
	if dataType.Level != "" {
		level = json.RawMessage(dataType.Level)
	}
	return json.Marshal(struct {
		Type         string          `json:"type"`
		Codes        []string        `json:"codes,omitempty"`
		Level        json.RawMessage `json:"level,omitempty"`
		OnlySnapshot bool            `json:"is_only_snapshot,omitempty"`
		OnlyRealtime bool            `json:"is_only_realtime,omitempty"`
	}{
		Type: dataType.Type, Codes: dataType.Codes, Level: level,
		OnlySnapshot: dataType.OnlySnapshot, OnlyRealtime: dataType.OnlyRealtime,
	})
}

// StreamRequest는 ticket, 데이터 종류와 응답 형식을 정의한다.
type StreamRequest struct {
	Ticket string
	Types  []StreamDataType
	Format StreamFormat
}

// StreamError는 WebSocket 요청 오류 응답이다.
type StreamError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// StreamMessage는 public/private 이벤트, 상태 또는 오류 한 건이다.
type StreamMessage struct {
	Type       string
	Code       string
	StreamType string
	Timestamp  int64
	Status     string
	Error      *StreamError
	Payload    json.RawMessage
	Raw        json.RawMessage
}

// Decode는 이벤트 payload를 지정 타입으로 변환한다.
func (message StreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("Upbit stream decode target is nil")
	}
	if message.Error != nil || message.Status != "" || len(message.Payload) == 0 {
		return fmt.Errorf("Upbit stream message does not contain an event")
	}
	if err := json.Unmarshal(message.Payload, target); err != nil {
		return fmt.Errorf("decode Upbit stream event: %w", err)
	}
	return nil
}

// StreamTicker는 실시간 ticker 이벤트의 주요 필드다.
type StreamTicker struct {
	Type                 string  `json:"type"`
	Code                 string  `json:"code"`
	OpeningPrice         Decimal `json:"opening_price"`
	HighPrice            Decimal `json:"high_price"`
	LowPrice             Decimal `json:"low_price"`
	TradePrice           Decimal `json:"trade_price"`
	PreviousClosingPrice Decimal `json:"prev_closing_price"`
	Change               string  `json:"change"`
	ChangePrice          Decimal `json:"change_price"`
	SignedChangePrice    Decimal `json:"signed_change_price"`
	ChangeRate           Decimal `json:"change_rate"`
	SignedChangeRate     Decimal `json:"signed_change_rate"`
	TradeVolume          Decimal `json:"trade_volume"`
	AccumulatedPrice24h  Decimal `json:"acc_trade_price_24h"`
	AccumulatedVolume24h Decimal `json:"acc_trade_volume_24h"`
	TradeTimestamp       int64   `json:"trade_timestamp"`
	Timestamp            int64   `json:"timestamp"`
	StreamType           string  `json:"stream_type"`
}

// StreamTrade는 실시간 체결 이벤트다.
type StreamTrade struct {
	Type                 string  `json:"type"`
	Code                 string  `json:"code"`
	TradePrice           Decimal `json:"trade_price"`
	TradeVolume          Decimal `json:"trade_volume"`
	AskBid               string  `json:"ask_bid"`
	PreviousClosingPrice Decimal `json:"prev_closing_price"`
	Change               string  `json:"change"`
	ChangePrice          Decimal `json:"change_price"`
	TradeDate            string  `json:"trade_date"`
	TradeTime            string  `json:"trade_time"`
	TradeTimestamp       int64   `json:"trade_timestamp"`
	Timestamp            int64   `json:"timestamp"`
	SequentialID         int64   `json:"sequential_id"`
	StreamType           string  `json:"stream_type"`
}

// StreamOrderBook은 실시간 호가 snapshot 이벤트다.
type StreamOrderBook struct {
	Type         string          `json:"type"`
	Code         string          `json:"code"`
	Timestamp    int64           `json:"timestamp"`
	TotalAskSize Decimal         `json:"total_ask_size"`
	TotalBidSize Decimal         `json:"total_bid_size"`
	OrderBook    []OrderBookUnit `json:"orderbook_units"`
	Level        Decimal         `json:"level"`
	StreamType   string          `json:"stream_type"`
}

// UnmarshalJSON은 DEFAULT와 SIMPLE 계열 호가 필드를 같은 타입으로 변환한다.
func (book *StreamOrderBook) UnmarshalJSON(data []byte) error {
	var wire struct {
		Type               string          `json:"type"`
		SimpleType         string          `json:"ty"`
		Code               string          `json:"code"`
		SimpleCode         string          `json:"cd"`
		Timestamp          int64           `json:"timestamp"`
		SimpleTimestamp    int64           `json:"tms"`
		TotalAskSize       Decimal         `json:"total_ask_size"`
		SimpleTotalAskSize Decimal         `json:"tas"`
		TotalBidSize       Decimal         `json:"total_bid_size"`
		SimpleTotalBidSize Decimal         `json:"tbs"`
		OrderBook          []OrderBookUnit `json:"orderbook_units"`
		SimpleOrderBook    []struct {
			AskPrice Decimal `json:"ap"`
			BidPrice Decimal `json:"bp"`
			AskSize  Decimal `json:"as"`
			BidSize  Decimal `json:"bs"`
		} `json:"obu"`
		Level            Decimal `json:"level"`
		SimpleLevel      Decimal `json:"lv"`
		StreamType       string  `json:"stream_type"`
		SimpleStreamType string  `json:"st"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("decode Upbit stream order book: %w", err)
	}
	book.Type = wire.Type
	if book.Type == "" {
		book.Type = wire.SimpleType
	}
	book.Code = wire.Code
	if book.Code == "" {
		book.Code = wire.SimpleCode
	}
	book.Timestamp = wire.Timestamp
	if book.Timestamp == 0 {
		book.Timestamp = wire.SimpleTimestamp
	}
	book.TotalAskSize = wire.TotalAskSize
	if book.TotalAskSize == "" {
		book.TotalAskSize = wire.SimpleTotalAskSize
	}
	book.TotalBidSize = wire.TotalBidSize
	if book.TotalBidSize == "" {
		book.TotalBidSize = wire.SimpleTotalBidSize
	}
	book.OrderBook = wire.OrderBook
	if book.OrderBook == nil && wire.SimpleOrderBook != nil {
		book.OrderBook = make([]OrderBookUnit, len(wire.SimpleOrderBook))
		for index, unit := range wire.SimpleOrderBook {
			book.OrderBook[index] = OrderBookUnit{
				AskPrice: unit.AskPrice, BidPrice: unit.BidPrice,
				AskSize: unit.AskSize, BidSize: unit.BidSize,
			}
		}
	}
	book.Level = wire.Level
	if book.Level == "" {
		book.Level = wire.SimpleLevel
	}
	book.StreamType = wire.StreamType
	if book.StreamType == "" {
		book.StreamType = wire.SimpleStreamType
	}
	return nil
}

// StreamCandle은 실시간 캔들 이벤트다.
type StreamCandle struct {
	Type                    string  `json:"type"`
	Code                    string  `json:"code"`
	CandleDateTimeUTC       string  `json:"candle_date_time_utc"`
	CandleDateTimeKST       string  `json:"candle_date_time_kst"`
	OpeningPrice            Decimal `json:"opening_price"`
	HighPrice               Decimal `json:"high_price"`
	LowPrice                Decimal `json:"low_price"`
	TradePrice              Decimal `json:"trade_price"`
	CandleAccumulatedPrice  Decimal `json:"candle_acc_trade_price"`
	CandleAccumulatedVolume Decimal `json:"candle_acc_trade_volume"`
	Timestamp               int64   `json:"timestamp"`
	StreamType              string  `json:"stream_type"`
}

// MyOrderEvent는 내 주문과 체결 변경 이벤트다.
type MyOrderEvent struct {
	Type            string      `json:"type"`
	Code            string      `json:"code"`
	UUID            string      `json:"uuid"`
	AskBid          string      `json:"ask_bid"`
	OrderType       OrderType   `json:"order_type"`
	State           OrderState  `json:"state"`
	TradeUUID       string      `json:"trade_uuid"`
	Price           Decimal     `json:"price"`
	AveragePrice    Decimal     `json:"avg_price"`
	Volume          Decimal     `json:"volume"`
	RemainingVolume Decimal     `json:"remaining_volume"`
	ExecutedVolume  Decimal     `json:"executed_volume"`
	ExecutedFunds   Decimal     `json:"executed_funds"`
	ReservedFee     Decimal     `json:"reserved_fee"`
	RemainingFee    Decimal     `json:"remaining_fee"`
	PaidFee         Decimal     `json:"paid_fee"`
	Locked          Decimal     `json:"locked"`
	TradesCount     int         `json:"trades_count"`
	TimeInForce     TimeInForce `json:"time_in_force"`
	Identifier      string      `json:"identifier"`
	Timestamp       int64       `json:"timestamp"`
	StreamType      string      `json:"stream_type"`
}

// MyAssetEvent는 내 자산 변경 이벤트다.
type MyAssetEvent struct {
	Type       string  `json:"type"`
	AssetUUID  string  `json:"asset_uuid"`
	Currency   string  `json:"currency"`
	Balance    Decimal `json:"balance"`
	Locked     Decimal `json:"locked"`
	Timestamp  int64   `json:"timestamp"`
	StreamType string  `json:"stream_type"`
}
