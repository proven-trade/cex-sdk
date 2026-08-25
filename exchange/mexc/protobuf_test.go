package mexc

import (
	"testing"

	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestMEXCDecodeStreamProtobufEvents(t *testing.T) {
	t.Parallel()
	tradeItem := protobufTestMessage(
		protobufTestString(1, "64000"), protobufTestString(2, "0.1"),
		protobufTestVarint(3, 2), protobufTestVarint(4, 1_700_000_000_000),
		protobufTestString(5, "trade-1"),
	)
	bookLevel := protobufTestMessage(
		protobufTestString(1, "64001"), protobufTestString(2, "0.2"),
	)
	tests := []struct {
		name      string
		bodyField protowire.Number
		channel   string
		body      []byte
		check     func(*testing.T, StreamMessage)
	}{
		{
			name: "aggregate trades", bodyField: 314,
			channel: "spot@public.aggre.deals.v3.api.pb@100ms@BTCUSDT",
			body: protobufTestMessage(
				protobufTestBytes(1, tradeItem), protobufTestString(2, "push.deal"),
			),
			check: func(t *testing.T, message StreamMessage) {
				t.Helper()
				if message.AggregateTrades == nil || len(message.AggregateTrades.Deals) != 1 ||
					message.AggregateTrades.Deals[0].TradeType != StreamTradeSell ||
					message.AggregateTrades.Deals[0].TradeID != "trade-1" ||
					len(message.AggregateTrades.Raw) == 0 ||
					len(message.AggregateTrades.Deals[0].Raw) == 0 {
					t.Fatalf("aggregate trades = %+v", message.AggregateTrades)
				}
			},
		},
		{
			name: "diff depth", bodyField: 313,
			channel: "spot@public.aggre.depth.v3.api.pb@10ms@BTCUSDT",
			body: protobufTestMessage(
				protobufTestBytes(1, bookLevel), protobufTestBytes(2, bookLevel),
				protobufTestString(3, "push.depth"), protobufTestString(4, "100"),
				protobufTestString(5, "102"), protobufTestVarint(6, 1_700_000_000_001),
			),
			check: func(t *testing.T, message StreamMessage) {
				t.Helper()
				if message.DiffDepth == nil || message.DiffDepth.FromVersion != "100" ||
					message.DiffDepth.ToVersion != "102" || len(message.DiffDepth.Asks) != 1 ||
					message.DiffDepth.Bids[0].Quantity != "0.2" {
					t.Fatalf("diff depth = %+v", message.DiffDepth)
				}
			},
		},
		{
			name: "partial depth", bodyField: 303,
			channel: "spot@public.limit.depth.v3.api.pb@BTCUSDT@5",
			body: protobufTestMessage(
				protobufTestBytes(1, bookLevel), protobufTestBytes(2, bookLevel),
				protobufTestString(3, "push.depth"), protobufTestString(4, "102"),
				protobufTestVarint(5, 1_700_000_000_001),
			),
			check: func(t *testing.T, message StreamMessage) {
				t.Helper()
				if message.PartialDepth == nil || message.PartialDepth.Version != "102" ||
					message.PartialDepth.Asks[0].Price != "64001" {
					t.Fatalf("partial depth = %+v", message.PartialDepth)
				}
			},
		},
		{
			name: "book ticker", bodyField: 315,
			channel: "spot@public.aggre.bookTicker.v3.api.pb@10ms@BTCUSDT",
			body: protobufTestMessage(
				protobufTestString(1, "63999"), protobufTestString(2, "0.3"),
				protobufTestString(3, "64001"), protobufTestString(4, "0.2"),
				protobufTestString(5, "103"), protobufTestVarint(6, 1_700_000_000_001),
			),
			check: func(t *testing.T, message StreamMessage) {
				t.Helper()
				if message.BookTicker == nil || message.BookTicker.BidPrice != "63999" ||
					message.BookTicker.AskQuantity != "0.2" || message.BookTicker.Version != "103" {
					t.Fatalf("book ticker = %+v", message.BookTicker)
				}
			},
		},
		{
			name: "candle", bodyField: 308,
			channel: "spot@public.kline.v3.api.pb@BTCUSDT@Min1",
			body: protobufTestMessage(
				protobufTestString(1, "Min1"), protobufTestVarint(2, 1_700_000_000),
				protobufTestString(3, "63000"), protobufTestString(4, "64000"),
				protobufTestString(5, "65000"), protobufTestString(6, "62000"),
				protobufTestString(7, "10"), protobufTestString(8, "640000"),
				protobufTestVarint(9, 1_700_000_059),
			),
			check: func(t *testing.T, message StreamMessage) {
				t.Helper()
				if message.Candle == nil || message.Candle.Interval != "Min1" ||
					message.Candle.Open != "63000" || message.Candle.Amount != "640000" {
					t.Fatalf("candle = %+v", message.Candle)
				}
			},
		},
		{
			name: "private account", bodyField: 307,
			channel: "spot@private.account.v3.api.pb",
			body: protobufTestMessage(
				protobufTestString(1, "USDT"), protobufTestString(2, "coin-1"),
				protobufTestString(3, "900"), protobufTestString(4, "-100"),
				protobufTestString(5, "100"), protobufTestString(6, "100"),
				protobufTestString(7, "ORDER"), protobufTestVarint(8, 1_700_000_000_001),
			),
			check: func(t *testing.T, message StreamMessage) {
				t.Helper()
				if message.Account == nil || message.Account.Asset != "USDT" ||
					message.Account.Frozen != "100" || message.Account.ChangeType != "ORDER" {
					t.Fatalf("account = %+v", message.Account)
				}
			},
		},
		{
			name: "private deal", bodyField: 306,
			channel: "spot@private.deals.v3.api.pb",
			body: protobufTestMessage(
				protobufTestString(1, "64000"), protobufTestString(2, "0.1"),
				protobufTestString(3, "6400"), protobufTestVarint(4, 1),
				protobufTestVarint(5, 1), protobufTestVarint(6, 0),
				protobufTestString(7, "trade-1"), protobufTestString(8, "strategy-1"),
				protobufTestString(9, "order-1"), protobufTestString(10, "0.064"),
				protobufTestString(11, "USDT"), protobufTestVarint(12, 1_700_000_000_001),
			),
			check: func(t *testing.T, message StreamMessage) {
				t.Helper()
				if message.AccountDeal == nil || message.AccountDeal.TradeType != StreamTradeBuy ||
					!message.AccountDeal.Maker || message.AccountDeal.FeeAmount != "0.064" {
					t.Fatalf("account deal = %+v", message.AccountDeal)
				}
			},
		},
		{
			name: "private order", bodyField: 304,
			channel: "spot@private.orders.v3.api.pb",
			body: protobufTestMessage(
				protobufTestString(1, "order-1"), protobufTestString(2, "strategy-1"),
				protobufTestString(3, "64000"), protobufTestString(4, "0.1"),
				protobufTestString(5, "6400"), protobufTestString(6, "64000"),
				protobufTestVarint(7, 1), protobufTestVarint(8, 1),
				protobufTestVarint(9, 0), protobufTestString(10, "0"),
				protobufTestString(11, "0"), protobufTestString(12, "0.1"),
				protobufTestString(13, "0.1"), protobufTestString(14, "6400"),
				protobufTestVarint(15, 2), protobufTestVarint(16, 1_700_000_000_001),
				protobufTestString(17, "BTCUSDT"), protobufTestString(23, "symbol-1"),
			),
			check: func(t *testing.T, message StreamMessage) {
				t.Helper()
				if message.AccountOrder == nil || message.AccountOrder.ID != "order-1" ||
					message.AccountOrder.OrderType != StreamOrderLimit ||
					message.AccountOrder.Status != StreamOrderFilled ||
					message.AccountOrder.CumulativeAmount != "6400" {
					t.Fatalf("account order = %+v", message.AccountOrder)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wrapper := protobufTestMessage(
				protobufTestString(1, test.channel),
				protobufTestBytes(test.bodyField, test.body),
				protobufTestString(3, "BTCUSDT"), protobufTestString(4, "symbol-1"),
				protobufTestVarint(5, 1_700_000_000_002),
				protobufTestVarint(6, 1_700_000_000_003),
			)
			message, err := DecodeStreamMessage(corestream.Message{
				Type: corestream.MessageBinary, Data: wrapper,
			})
			if err != nil {
				t.Fatalf("DecodeStreamMessage() error = %v", err)
			}
			if message.Channel != test.channel || message.Symbol != "BTCUSDT" ||
				message.SymbolID != "symbol-1" || message.CreateTime != 1_700_000_000_002 ||
				message.SendTime != 1_700_000_000_003 || len(message.Raw) == 0 {
				t.Fatalf("message metadata = %+v", message)
			}
			test.check(t, message)
		})
	}
}

func TestMEXCDecodeStreamControlAndMalformedFrames(t *testing.T) {
	t.Parallel()
	message, err := DecodeStreamMessage(corestream.Message{
		Type: corestream.MessageText,
		Data: []byte(`{"id":0,"code":1,"msg":"bad channel"}`),
	})
	if err != nil || message.Control == nil || message.Control.ID != 0 ||
		message.Control.Code != 1 || message.Control.Message != "bad channel" ||
		len(message.Control.Raw) == 0 {
		t.Fatalf("control = %+v, error = %v", message.Control, err)
	}
	tests := []corestream.Message{
		{Type: corestream.MessageText, Data: []byte(`{"id":-1,"code":0,"msg":"PONG"}`)},
		{Type: corestream.MessageText, Data: []byte(`{`)},
		{Type: corestream.MessageBinary, Data: nil},
		{Type: corestream.MessageBinary, Data: protobufTestString(3, "BTCUSDT")},
		{Type: corestream.MessageBinary, Data: []byte{0xff}},
	}
	for index, input := range tests {
		if _, decodeErr := DecodeStreamMessage(input); decodeErr == nil {
			t.Fatalf("malformed frame %d was accepted", index)
		}
	}
	if _, err := decodeStreamAccountDeal(protobufTestVarint(5, 2)); err == nil {
		t.Fatal("invalid Protobuf boolean was accepted")
	}
	if _, err := decodeStreamCandle(protobufTestVarint(1, 1)); err == nil {
		t.Fatal("invalid Protobuf wire type was accepted")
	}
	if _, err := decodeStreamCandle(protobufTestBytes(1, []byte{0xff})); err == nil {
		t.Fatal("invalid Protobuf UTF-8 was accepted")
	}
}

func protobufTestMessage(fields ...[]byte) []byte {
	var result []byte
	for _, field := range fields {
		result = append(result, field...)
	}
	return result
}

func protobufTestString(number protowire.Number, value string) []byte {
	return protobufTestBytes(number, []byte(value))
}

func protobufTestBytes(number protowire.Number, value []byte) []byte {
	result := protowire.AppendTag(nil, number, protowire.BytesType)
	return protowire.AppendBytes(result, value)
}

func protobufTestVarint(number protowire.Number, value uint64) []byte {
	result := protowire.AppendTag(nil, number, protowire.VarintType)
	return protowire.AppendVarint(result, value)
}
