package mexc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"

	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"google.golang.org/protobuf/encoding/protowire"
)

type protobufField struct {
	number protowire.Number
	typeID protowire.Type
	bytes  []byte
	varint uint64
}

// DecodeStreamMessage는 JSON 제어 frame과 MEXC binary Protobuf 이벤트를 구분해 해석한다.
func DecodeStreamMessage(message corestream.Message) (StreamMessage, error) {
	switch message.Type {
	case corestream.MessageText:
		return decodeStreamControl(message.Data)
	case corestream.MessageBinary:
		return decodeStreamProtobuf(message.Data)
	default:
		return StreamMessage{}, fmt.Errorf("unsupported MEXC WebSocket message type %d", message.Type)
	}
}

func decodeStreamControl(data []byte) (StreamMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || !json.Valid(trimmed) || trimmed[0] != '{' {
		return StreamMessage{}, fmt.Errorf("MEXC WebSocket control message is not valid JSON")
	}
	var wire struct {
		ID      json.RawMessage `json:"id"`
		Code    int             `json:"code"`
		Message string          `json:"msg"`
	}
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return StreamMessage{}, fmt.Errorf("decode MEXC WebSocket control message: %w", err)
	}
	var id uint64
	if len(wire.ID) > 0 && !bytes.Equal(bytes.TrimSpace(wire.ID), []byte("null")) {
		value := string(bytes.TrimSpace(wire.ID))
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return StreamMessage{}, fmt.Errorf("decode MEXC WebSocket control ID: %w", err)
		}
		id = parsed
	}
	raw := append(json.RawMessage(nil), trimmed...)
	return StreamMessage{
		Control: &StreamControl{ID: id, Code: wire.Code, Message: wire.Message, Raw: raw},
		Raw:     append([]byte(nil), trimmed...),
	}, nil
}

func decodeStreamProtobuf(data []byte) (StreamMessage, error) {
	if len(data) == 0 {
		return StreamMessage{}, fmt.Errorf("MEXC WebSocket Protobuf message is empty")
	}
	result := StreamMessage{Raw: append([]byte(nil), data...)}
	err := walkProtobuf(data, func(field protobufField) error {
		switch field.number {
		case 1:
			value, err := protobufString(field, "channel")
			result.Channel = value
			return err
		case 3:
			value, err := protobufString(field, "symbol")
			result.Symbol = value
			return err
		case 4:
			value, err := protobufString(field, "symbol ID")
			result.SymbolID = value
			return err
		case 5:
			value, err := protobufInt64(field, "create time")
			result.CreateTime = value
			return err
		case 6:
			value, err := protobufInt64(field, "send time")
			result.SendTime = value
			return err
		case 303:
			body, err := protobufBytes(field, "partial depth")
			if err != nil {
				return err
			}
			result.clearBody()
			decoded, err := decodeStreamPartialDepth(body)
			result.PartialDepth = decoded
			return err
		case 304:
			body, err := protobufBytes(field, "account order")
			if err != nil {
				return err
			}
			result.clearBody()
			decoded, err := decodeStreamAccountOrder(body)
			result.AccountOrder = decoded
			return err
		case 306:
			body, err := protobufBytes(field, "account deal")
			if err != nil {
				return err
			}
			result.clearBody()
			decoded, err := decodeStreamAccountDeal(body)
			result.AccountDeal = decoded
			return err
		case 307:
			body, err := protobufBytes(field, "account")
			if err != nil {
				return err
			}
			result.clearBody()
			decoded, err := decodeStreamAccount(body)
			result.Account = decoded
			return err
		case 308:
			body, err := protobufBytes(field, "candle")
			if err != nil {
				return err
			}
			result.clearBody()
			decoded, err := decodeStreamCandle(body)
			result.Candle = decoded
			return err
		case 313:
			body, err := protobufBytes(field, "diff depth")
			if err != nil {
				return err
			}
			result.clearBody()
			decoded, err := decodeStreamDiffDepth(body)
			result.DiffDepth = decoded
			return err
		case 314:
			body, err := protobufBytes(field, "aggregate trades")
			if err != nil {
				return err
			}
			result.clearBody()
			decoded, err := decodeStreamAggregateTrades(body)
			result.AggregateTrades = decoded
			return err
		case 315:
			body, err := protobufBytes(field, "book ticker")
			if err != nil {
				return err
			}
			result.clearBody()
			decoded, err := decodeStreamBookTicker(body)
			result.BookTicker = decoded
			return err
		default:
			return nil
		}
	})
	if err != nil {
		return StreamMessage{}, fmt.Errorf("decode MEXC WebSocket Protobuf wrapper: %w", err)
	}
	if result.Channel == "" {
		return StreamMessage{}, fmt.Errorf("MEXC WebSocket Protobuf channel is empty")
	}
	return result, nil
}

func (message *StreamMessage) clearBody() {
	message.AggregateTrades = nil
	message.DiffDepth = nil
	message.PartialDepth = nil
	message.BookTicker = nil
	message.Candle = nil
	message.Account = nil
	message.AccountDeal = nil
	message.AccountOrder = nil
}

func decodeStreamAggregateTrades(data []byte) (*StreamAggregateTrades, error) {
	result := &StreamAggregateTrades{Raw: append([]byte(nil), data...)}
	err := walkProtobuf(data, func(field protobufField) error {
		switch field.number {
		case 1:
			body, err := protobufBytes(field, "aggregate trade")
			if err != nil {
				return err
			}
			deal, err := decodeStreamAggregateTrade(body)
			if err == nil {
				result.Deals = append(result.Deals, deal)
			}
			return err
		case 2:
			value, err := protobufString(field, "aggregate trade event type")
			result.EventType = value
			return err
		default:
			return nil
		}
	})
	return result, err
}

func decodeStreamAggregateTrade(data []byte) (StreamAggregateTrade, error) {
	result := StreamAggregateTrade{Raw: append([]byte(nil), data...)}
	err := walkProtobuf(data, func(field protobufField) error {
		var err error
		switch field.number {
		case 1:
			result.Price, err = protobufString(field, "trade price")
		case 2:
			result.Quantity, err = protobufString(field, "trade quantity")
		case 3:
			var value int32
			value, err = protobufInt32(field, "trade type")
			result.TradeType = StreamTradeType(value)
		case 4:
			result.Time, err = protobufInt64(field, "trade time")
		case 5:
			result.TradeID, err = protobufString(field, "trade ID")
		}
		return err
	})
	return result, err
}

func decodeStreamDiffDepth(data []byte) (*StreamDiffDepth, error) {
	result := &StreamDiffDepth{Raw: append([]byte(nil), data...)}
	err := walkProtobuf(data, func(field protobufField) error {
		var err error
		switch field.number {
		case 1, 2:
			body, bodyErr := protobufBytes(field, "depth level")
			if bodyErr != nil {
				return bodyErr
			}
			level, levelErr := decodeStreamBookLevel(body)
			if levelErr != nil {
				return levelErr
			}
			if field.number == 1 {
				result.Asks = append(result.Asks, level)
			} else {
				result.Bids = append(result.Bids, level)
			}
		case 3:
			result.EventType, err = protobufString(field, "depth event type")
		case 4:
			result.FromVersion, err = protobufString(field, "depth from version")
		case 5:
			result.ToVersion, err = protobufString(field, "depth to version")
		case 6:
			result.LastOrderCreateTime, err = protobufInt64(field, "last order create time")
		}
		return err
	})
	return result, err
}

func decodeStreamPartialDepth(data []byte) (*StreamPartialDepth, error) {
	result := &StreamPartialDepth{Raw: append([]byte(nil), data...)}
	err := walkProtobuf(data, func(field protobufField) error {
		var err error
		switch field.number {
		case 1, 2:
			body, bodyErr := protobufBytes(field, "partial depth level")
			if bodyErr != nil {
				return bodyErr
			}
			level, levelErr := decodeStreamBookLevel(body)
			if levelErr != nil {
				return levelErr
			}
			if field.number == 1 {
				result.Asks = append(result.Asks, level)
			} else {
				result.Bids = append(result.Bids, level)
			}
		case 3:
			result.EventType, err = protobufString(field, "partial depth event type")
		case 4:
			result.Version, err = protobufString(field, "partial depth version")
		case 5:
			result.LastOrderCreateTime, err = protobufInt64(field, "last order create time")
		}
		return err
	})
	return result, err
}

func decodeStreamBookLevel(data []byte) (BookLevel, error) {
	var result BookLevel
	err := walkProtobuf(data, func(field protobufField) error {
		var err error
		switch field.number {
		case 1:
			result.Price, err = protobufString(field, "book price")
		case 2:
			result.Quantity, err = protobufString(field, "book quantity")
		}
		return err
	})
	return result, err
}

func decodeStreamBookTicker(data []byte) (*StreamBookTicker, error) {
	result := &StreamBookTicker{Raw: append([]byte(nil), data...)}
	err := walkProtobuf(data, func(field protobufField) error {
		var err error
		switch field.number {
		case 1:
			result.BidPrice, err = protobufString(field, "best bid price")
		case 2:
			result.BidQuantity, err = protobufString(field, "best bid quantity")
		case 3:
			result.AskPrice, err = protobufString(field, "best ask price")
		case 4:
			result.AskQuantity, err = protobufString(field, "best ask quantity")
		case 5:
			result.Version, err = protobufString(field, "book ticker version")
		case 6:
			result.LastOrderCreateTime, err = protobufInt64(field, "last order create time")
		}
		return err
	})
	return result, err
}

func decodeStreamCandle(data []byte) (*StreamCandle, error) {
	result := &StreamCandle{Raw: append([]byte(nil), data...)}
	err := walkProtobuf(data, func(field protobufField) error {
		var err error
		switch field.number {
		case 1:
			var value string
			value, err = protobufString(field, "candle interval")
			result.Interval = StreamCandleInterval(value)
		case 2:
			result.WindowStart, err = protobufInt64(field, "candle window start")
		case 3:
			result.Open, err = protobufString(field, "candle open")
		case 4:
			result.Close, err = protobufString(field, "candle close")
		case 5:
			result.High, err = protobufString(field, "candle high")
		case 6:
			result.Low, err = protobufString(field, "candle low")
		case 7:
			result.Volume, err = protobufString(field, "candle volume")
		case 8:
			result.Amount, err = protobufString(field, "candle amount")
		case 9:
			result.WindowEnd, err = protobufInt64(field, "candle window end")
		}
		return err
	})
	return result, err
}

func decodeStreamAccount(data []byte) (*StreamAccount, error) {
	result := &StreamAccount{Raw: append([]byte(nil), data...)}
	err := walkProtobuf(data, func(field protobufField) error {
		var err error
		switch field.number {
		case 1:
			result.Asset, err = protobufString(field, "account asset")
		case 2:
			result.CoinID, err = protobufString(field, "account coin ID")
		case 3:
			result.Balance, err = protobufString(field, "account balance")
		case 4:
			result.BalanceChange, err = protobufString(field, "account balance change")
		case 5:
			result.Frozen, err = protobufString(field, "account frozen balance")
		case 6:
			result.FrozenChange, err = protobufString(field, "account frozen change")
		case 7:
			result.ChangeType, err = protobufString(field, "account change type")
		case 8:
			result.Time, err = protobufInt64(field, "account time")
		}
		return err
	})
	return result, err
}

func decodeStreamAccountDeal(data []byte) (*StreamAccountDeal, error) {
	result := &StreamAccountDeal{Raw: append([]byte(nil), data...)}
	err := walkProtobuf(data, func(field protobufField) error {
		var err error
		switch field.number {
		case 1:
			result.Price, err = protobufString(field, "account deal price")
		case 2:
			result.Quantity, err = protobufString(field, "account deal quantity")
		case 3:
			result.Amount, err = protobufString(field, "account deal amount")
		case 4:
			var value int32
			value, err = protobufInt32(field, "account deal trade type")
			result.TradeType = StreamTradeType(value)
		case 5:
			result.Maker, err = protobufBool(field, "account deal maker")
		case 6:
			result.SelfTrade, err = protobufBool(field, "account deal self trade")
		case 7:
			result.TradeID, err = protobufString(field, "account deal trade ID")
		case 8:
			result.ClientOrderID, err = protobufString(field, "account deal client order ID")
		case 9:
			result.OrderID, err = protobufString(field, "account deal order ID")
		case 10:
			result.FeeAmount, err = protobufString(field, "account deal fee amount")
		case 11:
			result.FeeCurrency, err = protobufString(field, "account deal fee currency")
		case 12:
			result.Time, err = protobufInt64(field, "account deal time")
		}
		return err
	})
	return result, err
}

func decodeStreamAccountOrder(data []byte) (*StreamAccountOrder, error) {
	result := &StreamAccountOrder{Raw: append([]byte(nil), data...)}
	err := walkProtobuf(data, func(field protobufField) error {
		var err error
		switch field.number {
		case 1:
			result.ID, err = protobufString(field, "account order ID")
		case 2:
			result.ClientOrderID, err = protobufString(field, "account order client ID")
		case 3:
			result.Price, err = protobufString(field, "account order price")
		case 4:
			result.Quantity, err = protobufString(field, "account order quantity")
		case 5:
			result.Amount, err = protobufString(field, "account order amount")
		case 6:
			result.AveragePrice, err = protobufString(field, "account order average price")
		case 7:
			var value int32
			value, err = protobufInt32(field, "account order type")
			result.OrderType = StreamOrderType(value)
		case 8:
			var value int32
			value, err = protobufInt32(field, "account order trade type")
			result.TradeType = StreamTradeType(value)
		case 9:
			result.Maker, err = protobufBool(field, "account order maker")
		case 10:
			result.RemainingAmount, err = protobufString(field, "account order remaining amount")
		case 11:
			result.RemainingQuantity, err = protobufString(field, "account order remaining quantity")
		case 12:
			result.LastDealQuantity, err = protobufString(field, "account order last deal quantity")
		case 13:
			result.CumulativeQuantity, err = protobufString(field, "account order cumulative quantity")
		case 14:
			result.CumulativeAmount, err = protobufString(field, "account order cumulative amount")
		case 15:
			var value int32
			value, err = protobufInt32(field, "account order status")
			result.Status = StreamOrderStatus(value)
		case 16:
			result.CreatedAt, err = protobufInt64(field, "account order create time")
		case 17:
			result.Market, err = protobufString(field, "account order market")
		case 18:
			result.TriggerType, err = protobufInt32(field, "account order trigger type")
		case 19:
			result.TriggerPrice, err = protobufString(field, "account order trigger price")
		case 20:
			result.State, err = protobufInt32(field, "account order state")
		case 21:
			result.OCOID, err = protobufString(field, "account order OCO ID")
		case 22:
			result.RouteFactor, err = protobufString(field, "account order route factor")
		case 23:
			result.SymbolID, err = protobufString(field, "account order symbol ID")
		case 24:
			result.MarketID, err = protobufString(field, "account order market ID")
		case 25:
			result.MarketCurrencyID, err = protobufString(field, "account order market currency ID")
		case 26:
			result.CurrencyID, err = protobufString(field, "account order currency ID")
		}
		return err
	})
	return result, err
}

func walkProtobuf(data []byte, visit func(protobufField) error) error {
	for len(data) > 0 {
		number, typeID, tagLength := protowire.ConsumeTag(data)
		if tagLength < 0 {
			return protowire.ParseError(tagLength)
		}
		data = data[tagLength:]
		field := protobufField{number: number, typeID: typeID}
		var valueLength int
		switch typeID {
		case protowire.VarintType:
			field.varint, valueLength = protowire.ConsumeVarint(data)
		case protowire.BytesType:
			field.bytes, valueLength = protowire.ConsumeBytes(data)
		default:
			valueLength = protowire.ConsumeFieldValue(number, typeID, data)
		}
		if valueLength < 0 {
			return protowire.ParseError(valueLength)
		}
		if err := visit(field); err != nil {
			return err
		}
		data = data[valueLength:]
	}
	return nil
}

func protobufBytes(field protobufField, name string) ([]byte, error) {
	if field.typeID != protowire.BytesType {
		return nil, fmt.Errorf("MEXC Protobuf %s has wire type %d", name, field.typeID)
	}
	return field.bytes, nil
}

func protobufString(field protobufField, name string) (string, error) {
	value, err := protobufBytes(field, name)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(value) {
		return "", fmt.Errorf("MEXC Protobuf %s is not valid UTF-8", name)
	}
	return string(value), nil
}

func protobufInt64(field protobufField, name string) (int64, error) {
	if field.typeID != protowire.VarintType {
		return 0, fmt.Errorf("MEXC Protobuf %s has wire type %d", name, field.typeID)
	}
	return int64(field.varint), nil
}

func protobufInt32(field protobufField, name string) (int32, error) {
	value, err := protobufInt64(field, name)
	return int32(value), err
}

func protobufBool(field protobufField, name string) (bool, error) {
	value, err := protobufInt64(field, name)
	if err != nil {
		return false, err
	}
	if value != 0 && value != 1 {
		return false, fmt.Errorf("MEXC Protobuf %s is not boolean", name)
	}
	return value == 1, nil
}
