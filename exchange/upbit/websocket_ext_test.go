package upbit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

// 구독 요청 전문은 업비트 KR 문서의 Request 예시와 같은 필드 순서다.
const wantInfoSubscription = `[{"ticket":"info-ticket"},` +
	`{"type":"indicator","codes":["KRW-BTC"],"timeframe":"5m","indicators":[{"type":"rsi","params":{"period":14}}]},` +
	`{"type":"indicator_signal","codes":["KRW-BTC"],"timeframe":"5m","indicators":[` +
	`{"type":"volume","params":{"period":20},"trigger":"volume_spike","event_params":{"multiplier":2}}]},` +
	`{"format":"DEFAULT"}]`

const wantAnnouncementSubscription = `[{"ticket":"announcement-ticket"},` +
	`{"type":"announcement","categories":["trade"],"include_body":true},{"format":"DEFAULT"}]`

const upbitRSIIndicatorJSON = `{"type":"indicator","code":"KRW-BTC","timeframe":"5m","indicator":"rsi","params":{"period":14},` +
	`"candle_date_time_utc":"2026-09-03T04:50:00","value":{"rsi":58.75323648},"timestamp":1788411301095,"stream_type":"REALTIME"}`

const upbitVolumeSignalJSON = `{"type":"indicator_signal","code":"KRW-BTC","timeframe":"5m","indicator":"volume","params":{"period":20},` +
	`"trigger":"volume_spike","event_params":{"multiplier":2.0},"candle_date_time_utc":"2026-09-03T04:55:00",` +
	`"value":{"volume":120.5,"avg_volume":40.1,"ratio":3.0},"timestamp":1788411601095,"stream_type":"REALTIME"}`

const upbitAnnouncementJSON = `{"type":"announcement","event_type":"CREATED","uuid":"562593104","title":"[거래] 신규 거래 지원 안내 ...",` +
	`"category":"trade","url":"https://www.upbit.com/service_center/notice?id=562593104&view=share",` +
	`"first_listed_at":"2026-07-14T16:36:34+09:00","listed_at":"2026-07-14T16:36:34+09:00","timestamp":1752566400123,` +
	`"stream_type":"REALTIME","body":"공지사항 본문 ..."}`

const upbitAnnouncementSimpleJSON = `{"ty":"announcement","et":"CREATED","uid":"562593104","tt":"[거래] 신규 거래 지원 안내 ...",` +
	`"cat":"trade","url":"https://www.upbit.com/service_center/notice?id=562593104&view=share",` +
	`"flat":"2026-07-14T16:36:34+09:00","lat":"2026-07-14T16:36:34+09:00","tms":1752566400123,` +
	`"st":"REALTIME","bd":"공지사항 본문 ..."}`

func TestInfoStreamSubscribesIndicatorsAndDecodesEvents(t *testing.T) {
	t.Parallel()

	connection := newWebSocketTestConnection()
	connector := &websocketTestConnector{connections: []*websocketTestConnection{connection}}
	provider := &websocketCredentialProvider{}
	client := newTestUpbitStreamClient(
		t, connector, provider, sequentialSource("nonce-1"), sequentialSource("info-ticket"),
		[]transport.EgressRouteID{"route-a"},
	)
	rsi := IndicatorSpec{Type: IndicatorTypeRSI, Params: IndicatorParams{Period: 14}}
	volumeSignal := IndicatorSpec{
		Type: IndicatorTypeVolume, Params: IndicatorParams{Period: 20},
		Trigger: IndicatorTriggerVolumeSpike, EventParams: IndicatorEventParams{Multiplier: 2},
	}
	info, err := client.InfoStream(StreamRequest{Types: []StreamDataType{
		{Type: StreamTypeIndicator, Codes: []string{"krw-btc"}, Timeframe: IndicatorTimeframe5m, Indicators: []IndicatorSpec{rsi}},
		{Type: StreamTypeIndicatorSignal, Codes: []string{"KRW-BTC"}, Timeframe: IndicatorTimeframe5m, Indicators: []IndicatorSpec{volumeSignal}},
	}})
	if err != nil {
		t.Fatalf("InfoStream() error = %v", err)
	}
	messages, stop := startUpbitStream(t, info.Run)
	defer stop()

	if payload := waitForWebSocketWrite(t, connection); string(payload) != wantInfoSubscription {
		t.Fatalf("subscription = %s\nwant %s", payload, wantInfoSubscription)
	}
	connection.reads <- websocketReadResult{message: corestream.Message{Data: []byte(upbitRSIIndicatorJSON)}}
	connection.reads <- websocketReadResult{message: corestream.Message{Data: []byte(upbitVolumeSignalJSON)}}

	indicatorMessage := waitUpbitStreamMessage(t, messages)
	if indicatorMessage.Type != StreamTypeIndicator || indicatorMessage.Code != "KRW-BTC" {
		t.Fatalf("indicator message = %+v", indicatorMessage)
	}
	var indicator StreamIndicator
	if err := indicatorMessage.Decode(&indicator); err != nil {
		t.Fatalf("Decode(indicator) error = %v", err)
	}
	if indicator.Indicator != IndicatorTypeRSI || indicator.Params.Period != 14 ||
		indicator.Value.RSI != "58.75323648" || indicator.CandleDateTimeUTC != "2026-09-03T04:50:00" {
		t.Fatalf("indicator = %+v", indicator)
	}
	signalMessage := waitUpbitStreamMessage(t, messages)
	if signalMessage.Type != StreamTypeIndicatorSignal {
		t.Fatalf("signal message = %+v", signalMessage)
	}
	var signal StreamIndicatorSignal
	if err := signalMessage.Decode(&signal); err != nil {
		t.Fatalf("Decode(signal) error = %v", err)
	}
	if signal.Trigger != IndicatorTriggerVolumeSpike || signal.EventParams.Multiplier != 2 ||
		signal.Value.Ratio != "3.0" || signal.Params.Period != 20 {
		t.Fatalf("signal = %+v", signal)
	}

	routes, requests := connector.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{"route-a"}) || len(requests) != 1 ||
		requests[0].Endpoint != "ws://info.example.test/websocket/v1/info" {
		t.Fatalf("routes = %v, requests = %+v", routes, requests)
	}
	assertJWTHeader(t, requests[0].Header.Get("Authorization"), "nonce-1")
	if provider.count() != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.count())
	}
}

func TestInfoStreamRequiresCredentialsAndAllowedRoute(t *testing.T) {
	t.Parallel()

	rsi := IndicatorSpec{Type: IndicatorTypeRSI, Params: IndicatorParams{Period: 14}}
	request := StreamRequest{Types: []StreamDataType{
		{Type: StreamTypeIndicator, Codes: []string{"KRW-BTC"}, Timeframe: IndicatorTimeframe5m, Indicators: []IndicatorSpec{rsi}},
	}}
	anonymous := newTestUpbitStreamClient(t, &websocketTestConnector{}, nil, nil, sequentialSource("ticket"), nil)
	if _, err := anonymous.InfoStream(request); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("InfoStream() without credentials error = %v, want authentication error", err)
	}

	provider := &websocketCredentialProvider{}
	restricted := newTestUpbitStreamClient(
		t, &websocketTestConnector{}, provider, sequentialSource("nonce"), sequentialSource("ticket"),
		[]transport.EgressRouteID{"route-a"},
	)
	if _, err := restricted.InfoStream(request, trade.WithEgressRoute("route-b")); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("InfoStream() on route-b error = %v, want authorization error", err)
	}
	if provider.count() != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.count())
	}
}

func TestPrivateStreamSubscribesAnnouncementAndDecodesEvent(t *testing.T) {
	t.Parallel()

	connection := newWebSocketTestConnection()
	connector := &websocketTestConnector{connections: []*websocketTestConnection{connection}}
	client := newTestUpbitStreamClient(
		t, connector, &websocketCredentialProvider{}, sequentialSource("nonce-1"), sequentialSource("announcement-ticket"),
		[]transport.EgressRouteID{"route-a"},
	)
	private, err := client.PrivateStream(StreamRequest{Types: []StreamDataType{
		{Type: StreamTypeAnnouncement, Categories: []AnnouncementCategory{AnnouncementCategoryTrade}, IncludeBody: true},
	}})
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	messages, stop := startUpbitStream(t, private.Run)
	defer stop()

	if payload := waitForWebSocketWrite(t, connection); string(payload) != wantAnnouncementSubscription {
		t.Fatalf("subscription = %s\nwant %s", payload, wantAnnouncementSubscription)
	}
	connection.reads <- websocketReadResult{message: corestream.Message{Data: []byte(upbitAnnouncementJSON)}}
	message := waitUpbitStreamMessage(t, messages)
	if message.Type != StreamTypeAnnouncement || message.StreamType != "REALTIME" {
		t.Fatalf("announcement message = %+v", message)
	}
	var announcement StreamAnnouncement
	if err := message.Decode(&announcement); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if announcement.EventType != "CREATED" || announcement.Category != AnnouncementCategoryTrade || announcement.Body == "" {
		t.Fatalf("announcement = %+v", announcement)
	}
	_, requests := connector.snapshot()
	if len(requests) != 1 || requests[0].Endpoint != "ws://private.example.test/websocket/v1/private" {
		t.Fatalf("requests = %+v", requests)
	}
	assertJWTHeader(t, requests[0].Header.Get("Authorization"), "nonce-1")
}

func TestPublicStreamListSubscriptionsSendsMethodAndDecodesResponse(t *testing.T) {
	t.Parallel()

	connection := newWebSocketTestConnection()
	connector := &websocketTestConnector{connections: []*websocketTestConnection{connection}}
	client := newTestUpbitStreamClient(t, connector, nil, nil, sequentialSource("subscribe-ticket", "list-ticket"), nil)
	public, err := client.PublicStream(StreamRequest{
		Types:  []StreamDataType{{Type: "ticker", Codes: []string{"KRW-BTC", "KRW-ETH"}}},
		Format: StreamFormatSimple,
	})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	messages, stop := startUpbitStream(t, public.Run)
	defer stop()

	_ = waitForWebSocketWrite(t, connection)
	ticket, err := public.ListSubscriptions(context.Background())
	if err != nil || ticket != "list-ticket" {
		t.Fatalf("ListSubscriptions() = %q, %v", ticket, err)
	}
	const wantRequest = `[{"ticket":"list-ticket"},{"method":"LIST_SUBSCRIPTIONS"},{"format":"SIMPLE"}]`
	if payload := waitForWebSocketWrite(t, connection); string(payload) != wantRequest {
		t.Fatalf("list request = %s, want %s", payload, wantRequest)
	}

	connection.reads <- websocketReadResult{message: corestream.Message{Data: []byte(
		`{"mthd":"LIST_SUBSCRIPTIONS","rslt":[{"ty":"ticker","cds":["KRW-BTC","KRW-ETH"]},{"ty":"orderbook","cds":["KRW-BTC"],"lv":0}],"tckt":"list-ticket"}`,
	)}}
	message := waitUpbitStreamMessage(t, messages)
	if message.Method != listSubscriptionsMethod || message.Type != "" {
		t.Fatalf("message = %+v", message)
	}
	var subscriptions StreamSubscriptions
	if err := message.Decode(&subscriptions); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if subscriptions.Method != listSubscriptionsMethod || subscriptions.Ticket != ticket || len(subscriptions.Result) != 2 {
		t.Fatalf("subscriptions = %+v", subscriptions)
	}
	if first := subscriptions.Result[0]; first.Type != "ticker" || !slices.Equal(first.Codes, []string{"KRW-BTC", "KRW-ETH"}) {
		t.Fatalf("first subscription = %+v", first)
	}
	if second := subscriptions.Result[1]; second.Type != "orderbook" || second.Level != "0" {
		t.Fatalf("second subscription = %+v", second)
	}
}

func TestInfoStreamListSubscriptions(t *testing.T) {
	t.Parallel()

	connection := newWebSocketTestConnection()
	connector := &websocketTestConnector{connections: []*websocketTestConnection{connection}}
	client := newTestUpbitStreamClient(
		t, connector, &websocketCredentialProvider{}, sequentialSource("nonce"), sequentialSource("subscribe-ticket", "list-ticket"),
		[]transport.EgressRouteID{"route-a"},
	)
	rsi := IndicatorSpec{Type: IndicatorTypeRSI, Params: IndicatorParams{Period: 7}}
	info, err := client.InfoStream(StreamRequest{Types: []StreamDataType{
		{Type: StreamTypeIndicator, Codes: []string{"KRW-DOGE"}, Timeframe: IndicatorTimeframe5m, Indicators: []IndicatorSpec{rsi}},
	}})
	if err != nil {
		t.Fatalf("InfoStream() error = %v", err)
	}
	verifyUpbitListSubscriptions(
		t, connection, info.Run, info.ListSubscriptions,
		`[{"ticket":"list-ticket"},{"method":"LIST_SUBSCRIPTIONS"},{"format":"DEFAULT"}]`,
		`{"method":"LIST_SUBSCRIPTIONS","result":[{"type":"indicator","codes":["KRW-DOGE"],"timeframe":"5m","indicators":[{"type":"rsi","params":{"period":7}}]}],"ticket":"list-ticket"}`,
	)
}

func TestPrivateStreamListSubscriptions(t *testing.T) {
	t.Parallel()

	connection := newWebSocketTestConnection()
	connector := &websocketTestConnector{connections: []*websocketTestConnection{connection}}
	client := newTestUpbitStreamClient(
		t, connector, &websocketCredentialProvider{}, sequentialSource("nonce"), sequentialSource("subscribe-ticket", "list-ticket"),
		[]transport.EgressRouteID{"route-a"},
	)
	private, err := client.PrivateStream(StreamRequest{
		Types:  []StreamDataType{{Type: "myAsset"}},
		Format: StreamFormatJSONList,
	})
	if err != nil {
		t.Fatalf("PrivateStream() error = %v", err)
	}
	verifyUpbitListSubscriptions(
		t, connection, private.Run, private.ListSubscriptions,
		`[{"ticket":"list-ticket"},{"method":"LIST_SUBSCRIPTIONS"},{"format":"JSON_LIST"}]`,
		`[{"method":"LIST_SUBSCRIPTIONS","result":[{"type":"myAsset"}],"ticket":"list-ticket"}]`,
	)
}

func TestStreamListSubscriptionsRequiresLiveConnection(t *testing.T) {
	t.Parallel()

	client := newTestUpbitStreamClient(t, &websocketTestConnector{}, nil, nil, sequentialSource("ticket-1", "ticket-2"), nil)
	public, err := client.PublicStream(StreamRequest{Types: []StreamDataType{{Type: "ticker", Codes: []string{"KRW-BTC"}}}})
	if err != nil {
		t.Fatalf("PublicStream() error = %v", err)
	}
	if _, err := public.ListSubscriptions(context.Background()); !errors.Is(err, corestream.ErrNotConnected) {
		t.Fatalf("ListSubscriptions() before Run error = %v, want ErrNotConnected", err)
	}
	_ = public.Close()
	if _, err := public.ListSubscriptions(context.Background()); !errors.Is(err, corestream.ErrSessionClosed) {
		t.Fatalf("ListSubscriptions() after Close error = %v, want ErrSessionClosed", err)
	}
}

func TestDecodeStreamAnnouncementDefaultAndSimple(t *testing.T) {
	t.Parallel()

	want := StreamAnnouncement{
		Type: "announcement", EventType: "CREATED", UUID: "562593104", Title: "[거래] 신규 거래 지원 안내 ...",
		Category: AnnouncementCategoryTrade, URL: "https://www.upbit.com/service_center/notice?id=562593104&view=share",
		FirstListedAt: "2026-07-14T16:36:34+09:00", ListedAt: "2026-07-14T16:36:34+09:00",
		Timestamp: 1752566400123, StreamType: "REALTIME", Body: "공지사항 본문 ...",
	}
	for name, payload := range map[string]string{"default": upbitAnnouncementJSON, "simple": upbitAnnouncementSimpleJSON} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var got StreamAnnouncement
			decodeUpbitStreamPayload(t, payload, &got)
			if got != want {
				t.Fatalf("announcement = %+v, want %+v", got, want)
			}
		})
	}
}

func TestDecodeStreamIndicatorDefaultAndSimple(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		want    StreamIndicator
	}{
		{
			name:    "default rsi",
			payload: upbitRSIIndicatorJSON,
			want:    StreamIndicator{Type: "indicator", Code: "KRW-BTC", Timeframe: IndicatorTimeframe5m, Indicator: IndicatorTypeRSI, Params: IndicatorParams{Period: 14}, Value: IndicatorValue{RSI: "58.75323648"}, CandleDateTimeUTC: "2026-09-03T04:50:00", Timestamp: 1788411301095, StreamType: "REALTIME"},
		},
		{
			name:    "default moving average keeps the value field",
			payload: `{"type":"indicator","code":"KRW-BTC","timeframe":"5m","indicator":"moving_average","params":{"type":"SMA","period":20},"candle_date_time_utc":"2026-09-03T04:50:00","value":{"value":106791950.0},"timestamp":1788411301095,"stream_type":"REALTIME"}`,
			want:    StreamIndicator{Type: "indicator", Code: "KRW-BTC", Timeframe: IndicatorTimeframe5m, Indicator: IndicatorTypeMovingAverage, Params: IndicatorParams{Type: MovingAverageSMA, Period: 20}, Value: IndicatorValue{Value: "106791950.0"}, CandleDateTimeUTC: "2026-09-03T04:50:00", Timestamp: 1788411301095, StreamType: "REALTIME"},
		},
		{
			name:    "simple moving average keeps the value field",
			payload: `{"ty":"indicator","cd":"KRW-BTC","tf":"5m","ind":"moving_average","p":{"t":"SMA","p":20},"cdttmu":"2026-09-03T04:50:00","v":{"value":106791950.0},"tms":1788411301095,"st":"REALTIME"}`,
			want:    StreamIndicator{Type: "indicator", Code: "KRW-BTC", Timeframe: IndicatorTimeframe5m, Indicator: IndicatorTypeMovingAverage, Params: IndicatorParams{Type: MovingAverageSMA, Period: 20}, Value: IndicatorValue{Value: "106791950.0"}, CandleDateTimeUTC: "2026-09-03T04:50:00", Timestamp: 1788411301095, StreamType: "REALTIME"},
		},
		{
			name:    "simple macd",
			payload: `{"ty":"indicator","cd":"KRW-BTC","tf":"5m","ind":"macd","p":{"f":12,"s":26,"sgp":9},"cdttmu":"2026-09-03T04:50:00","v":{"macd":33554.85769535,"macds":55795.38214693,"hist":-22240.52445158},"tms":1788411301096,"st":"REALTIME"}`,
			want:    StreamIndicator{Type: "indicator", Code: "KRW-BTC", Timeframe: IndicatorTimeframe5m, Indicator: IndicatorTypeMACD, Params: IndicatorParams{Fast: 12, Slow: 26, SignalPeriod: 9}, Value: IndicatorValue{MACD: "33554.85769535", MACDSignal: "55795.38214693", Histogram: "-22240.52445158"}, CandleDateTimeUTC: "2026-09-03T04:50:00", Timestamp: 1788411301096, StreamType: "REALTIME"},
		},
		{
			name:    "simple bollinger bands",
			payload: `{"ty":"indicator","cd":"KRW-ETH","tf":"15m","ind":"bollinger_bands","p":{"p":20,"m":2.0},"cdttmu":"2026-09-03T05:00:00","v":{"bbu":3300000.5,"bbm":3200000.0,"bbl":3100000.25},"tms":1788411900000,"st":"REALTIME"}`,
			want:    StreamIndicator{Type: "indicator", Code: "KRW-ETH", Timeframe: IndicatorTimeframe15m, Indicator: IndicatorTypeBollingerBands, Params: IndicatorParams{Period: 20, Multiplier: 2}, Value: IndicatorValue{BBUpper: "3300000.5", BBMiddle: "3200000.0", BBLower: "3100000.25"}, CandleDateTimeUTC: "2026-09-03T05:00:00", Timestamp: 1788411900000, StreamType: "REALTIME"},
		},
		{
			name:    "simple moving average crossover",
			payload: `{"ty":"indicator","cd":"KRW-BTC","tf":"60m","ind":"ma_crossover","p":{"t":"EMA","shp":5,"lop":20},"cdttmu":"2026-09-03T04:00:00","v":{"mas":106800000.5,"mal":106700000.25},"tms":1788411300000,"st":"REALTIME"}`,
			want:    StreamIndicator{Type: "indicator", Code: "KRW-BTC", Timeframe: IndicatorTimeframe60m, Indicator: IndicatorTypeMACrossover, Params: IndicatorParams{Type: MovingAverageEMA, ShortPeriod: 5, LongPeriod: 20}, Value: IndicatorValue{MAShort: "106800000.5", MALong: "106700000.25"}, CandleDateTimeUTC: "2026-09-03T04:00:00", Timestamp: 1788411300000, StreamType: "REALTIME"},
		},
		{
			name:    "simple volume",
			payload: `{"ty":"indicator","cd":"KRW-BTC","tf":"1d","ind":"volume","p":{"p":20},"cdttmu":"2026-09-03T00:00:00","v":{"vol":120.5,"av":40.1,"rto":3.0},"tms":1788411300000,"st":"REALTIME"}`,
			want:    StreamIndicator{Type: "indicator", Code: "KRW-BTC", Timeframe: IndicatorTimeframe1d, Indicator: IndicatorTypeVolume, Params: IndicatorParams{Period: 20}, Value: IndicatorValue{Volume: "120.5", AverageVolume: "40.1", Ratio: "3.0"}, CandleDateTimeUTC: "2026-09-03T00:00:00", Timestamp: 1788411300000, StreamType: "REALTIME"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var got StreamIndicator
			decodeUpbitStreamPayload(t, test.payload, &got)
			if got != test.want {
				t.Fatalf("indicator = %+v\nwant %+v", got, test.want)
			}
		})
	}
}

func TestDecodeStreamIndicatorSignalDefaultAndSimple(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		want    StreamIndicatorSignal
	}{
		{
			name:    "default moving average has empty event params",
			payload: `{"type":"indicator_signal","code":"KRW-ARB","timeframe":"5m","indicator":"moving_average","params":{"type":"SMA","period":5},"trigger":"price_cross_above","event_params":{},"candle_date_time_utc":"2026-09-10T07:25:00","value":{"value":202.8},"timestamp":1789025407115,"stream_type":"REALTIME"}`,
			want:    StreamIndicatorSignal{Type: "indicator_signal", Code: "KRW-ARB", Timeframe: IndicatorTimeframe5m, Indicator: IndicatorTypeMovingAverage, Params: IndicatorParams{Type: MovingAverageSMA, Period: 5}, Trigger: IndicatorTriggerPriceCrossAbove, Value: IndicatorValue{Value: "202.8"}, CandleDateTimeUTC: "2026-09-10T07:25:00", Timestamp: 1789025407115, StreamType: "REALTIME"},
		},
		{
			name:    "simple rsi threshold",
			payload: `{"ty":"indicator_signal","cd":"KRW-BTC","tf":"5m","ind":"rsi","p":{"p":14},"tr":"overbought","ep":{"th":70},"cdttmu":"2026-09-03T04:50:00","v":{"rsi":71.5},"tms":1788411301096,"st":"REALTIME"}`,
			want:    StreamIndicatorSignal{Type: "indicator_signal", Code: "KRW-BTC", Timeframe: IndicatorTimeframe5m, Indicator: IndicatorTypeRSI, Params: IndicatorParams{Period: 14}, Trigger: IndicatorTriggerOverbought, EventParams: IndicatorEventParams{Threshold: 70}, Value: IndicatorValue{RSI: "71.5"}, CandleDateTimeUTC: "2026-09-03T04:50:00", Timestamp: 1788411301096, StreamType: "REALTIME"},
		},
		{
			name:    "simple volume spike multiplier",
			payload: `{"ty":"indicator_signal","cd":"KRW-BTC","tf":"5m","ind":"volume","p":{"p":20},"tr":"volume_spike","ep":{"mul":2.0},"cdttmu":"2026-09-03T04:55:00","v":{"vol":120.5,"av":40.1,"rto":3.0},"tms":1788411601095,"st":"REALTIME"}`,
			want:    StreamIndicatorSignal{Type: "indicator_signal", Code: "KRW-BTC", Timeframe: IndicatorTimeframe5m, Indicator: IndicatorTypeVolume, Params: IndicatorParams{Period: 20}, Trigger: IndicatorTriggerVolumeSpike, EventParams: IndicatorEventParams{Multiplier: 2}, Value: IndicatorValue{Volume: "120.5", AverageVolume: "40.1", Ratio: "3.0"}, CandleDateTimeUTC: "2026-09-03T04:55:00", Timestamp: 1788411601095, StreamType: "REALTIME"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var got StreamIndicatorSignal
			decodeUpbitStreamPayload(t, test.payload, &got)
			if got != test.want {
				t.Fatalf("signal = %+v\nwant %+v", got, test.want)
			}
		})
	}
}

func TestDecodeStreamSubscriptionsDefaultAndSimple(t *testing.T) {
	t.Parallel()

	var defaultFormat StreamSubscriptions
	decodeUpbitStreamPayload(t, `{"method":"LIST_SUBSCRIPTIONS","result":[{"type":"indicator_signal","codes":["KRW-DOGE"],"timeframe":"5m","indicators":[{"type":"rsi","params":{"period":7},"trigger":"overbought","event_params":{"threshold":70}}]}],"ticket":"indicator-subscriptions"}`, &defaultFormat)
	if defaultFormat.Method != listSubscriptionsMethod || defaultFormat.Ticket != "indicator-subscriptions" || len(defaultFormat.Result) != 1 {
		t.Fatalf("subscriptions = %+v", defaultFormat)
	}
	wantIndicators := []IndicatorSpec{{
		Type: IndicatorTypeRSI, Params: IndicatorParams{Period: 7},
		Trigger: IndicatorTriggerOverbought, EventParams: IndicatorEventParams{Threshold: 70},
	}}
	result := defaultFormat.Result[0]
	if result.Type != StreamTypeIndicatorSignal || !slices.Equal(result.Codes, []string{"KRW-DOGE"}) ||
		result.Timeframe != IndicatorTimeframe5m || !slices.Equal(result.Indicators, wantIndicators) {
		t.Fatalf("result = %+v", result)
	}

	var simpleFormat StreamSubscriptions
	decodeUpbitStreamPayload(t, `{"mthd":"LIST_SUBSCRIPTIONS","rslt":[{"ty":"ticker","cds":["KRW-BTC","KRW-ETH"]},{"ty":"orderbook","cds":["KRW-BTC"],"lv":0},{"ty":"myAsset"}],"tckt":"unique uuid"}`, &simpleFormat)
	if simpleFormat.Method != listSubscriptionsMethod || simpleFormat.Ticket != "unique uuid" || len(simpleFormat.Result) != 3 {
		t.Fatalf("subscriptions = %+v", simpleFormat)
	}
	if simpleFormat.Result[0].Type != "ticker" || !slices.Equal(simpleFormat.Result[0].Codes, []string{"KRW-BTC", "KRW-ETH"}) ||
		simpleFormat.Result[1].Level != "0" || simpleFormat.Result[2].Type != "myAsset" || len(simpleFormat.Result[2].Codes) != 0 {
		t.Fatalf("results = %+v", simpleFormat.Result)
	}
}

func TestEncodeSubscriptionIndicatorExtensions(t *testing.T) {
	t.Parallel()

	bollinger := IndicatorSpec{Type: IndicatorTypeBollingerBands, Params: IndicatorParams{Period: 20, Multiplier: 2.5}}
	crossover := IndicatorSpec{Type: IndicatorTypeMACrossover, Params: IndicatorParams{Type: MovingAverageEMA, ShortPeriod: 5, LongPeriod: 20}}
	macd := IndicatorSpec{Type: IndicatorTypeMACD, Params: IndicatorParams{Fast: 12, Slow: 26, SignalPeriod: 9}}
	payload, err := encodeSubscription("ticket", []StreamDataType{
		{Type: StreamTypeAnnouncement},
		{Type: StreamTypeIndicator, Codes: []string{"KRW-BTC"}, Timeframe: IndicatorTimeframe1d, Indicators: []IndicatorSpec{bollinger, crossover, macd}},
	}, StreamFormatSimple)
	if err != nil {
		t.Fatalf("encodeSubscription() error = %v", err)
	}
	const want = `[{"ticket":"ticket"},{"type":"announcement"},` +
		`{"type":"indicator","codes":["KRW-BTC"],"timeframe":"1d","indicators":[` +
		`{"type":"bollinger_bands","params":{"period":20,"multiplier":2.5}},` +
		`{"type":"ma_crossover","params":{"type":"EMA","short_period":5,"long_period":20}},` +
		`{"type":"macd","params":{"fast":12,"slow":26,"signal_period":9}}]},{"format":"SIMPLE"}]`
	if string(payload) != want {
		t.Fatalf("subscription = %s\nwant %s", payload, want)
	}
}

func TestValidateStreamRequestExtensions(t *testing.T) {
	t.Parallel()

	rsi := IndicatorSpec{Type: IndicatorTypeRSI, Params: IndicatorParams{Period: 14}}
	rsiSignal := IndicatorSpec{Type: IndicatorTypeRSI, Params: IndicatorParams{Period: 14}, Trigger: IndicatorTriggerOverbought, EventParams: IndicatorEventParams{Threshold: 70}}
	volumeSignal := IndicatorSpec{Type: IndicatorTypeVolume, Params: IndicatorParams{Period: 20}, Trigger: IndicatorTriggerVolumeSpike, EventParams: IndicatorEventParams{Multiplier: 2}}
	macdSignal := IndicatorSpec{Type: IndicatorTypeMACD, Params: IndicatorParams{Fast: 12, Slow: 26, SignalPeriod: 9}, Trigger: IndicatorTriggerZeroCrossAbove}
	crossoverSignal := IndicatorSpec{Type: IndicatorTypeMACrossover, Params: IndicatorParams{Type: MovingAverageEMA, ShortPeriod: 5, LongPeriod: 20}, Trigger: IndicatorTriggerGoldenCross}
	bollinger := IndicatorSpec{Type: IndicatorTypeBollingerBands, Params: IndicatorParams{Period: 20, Multiplier: 2}}
	announcement := StreamDataType{Type: StreamTypeAnnouncement, IncludeBody: true, Categories: []AnnouncementCategory{AnnouncementCategoryTrade, AnnouncementCategoryWallet}}

	tests := []struct {
		name    string
		kind    streamKind
		types   []StreamDataType
		wantErr bool
	}{
		{name: "info indicator and signal", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorData(rsi), upbitSignalData(rsiSignal, volumeSignal, macdSignal)}},
		{name: "info bollinger bands", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorData(bollinger)}},
		{name: "info moving average crossover signal", kind: streamKindInfo, types: []StreamDataType{upbitSignalData(crossoverSignal)}},
		{name: "info exactly 100 combinations", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorLoad(StreamTypeIndicator, 2, 50)}},
		{name: "info 100 combinations across both types", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorLoad(StreamTypeIndicator, 2, 30), upbitIndicatorLoad(StreamTypeIndicatorSignal, 2, 20)}},
		{name: "info 101 combinations", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorLoad(StreamTypeIndicator, 101, 1)}, wantErr: true},
		{name: "info 102 combinations across both types", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorLoad(StreamTypeIndicator, 2, 30), upbitIndicatorLoad(StreamTypeIndicatorSignal, 2, 21)}, wantErr: true},
		{name: "info ticker type", kind: streamKindInfo, types: []StreamDataType{{Type: "ticker", Codes: []string{"KRW-BTC"}}}, wantErr: true},
		{name: "info without codes", kind: streamKindInfo, types: []StreamDataType{{Type: StreamTypeIndicator, Timeframe: IndicatorTimeframe5m, Indicators: []IndicatorSpec{rsi}}}, wantErr: true},
		{name: "info unsupported timeframe", kind: streamKindInfo, types: []StreamDataType{{Type: StreamTypeIndicator, Codes: []string{"KRW-BTC"}, Timeframe: "1h", Indicators: []IndicatorSpec{rsi}}}, wantErr: true},
		{name: "info missing timeframe", kind: streamKindInfo, types: []StreamDataType{{Type: StreamTypeIndicator, Codes: []string{"KRW-BTC"}, Indicators: []IndicatorSpec{rsi}}}, wantErr: true},
		{name: "info without indicators", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorData()}, wantErr: true},
		{name: "info unsupported indicator", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorData(IndicatorSpec{Type: "stochastic", Params: IndicatorParams{Period: 14}})}, wantErr: true},
		{name: "info indicator with trigger", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorData(IndicatorSpec{Type: IndicatorTypeRSI, Params: IndicatorParams{Period: 14}, Trigger: IndicatorTriggerOverbought})}, wantErr: true},
		{name: "info indicator with event params", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorData(IndicatorSpec{Type: IndicatorTypeRSI, Params: IndicatorParams{Period: 14}, EventParams: IndicatorEventParams{Threshold: 70}})}, wantErr: true},
		{name: "info signal without trigger", kind: streamKindInfo, types: []StreamDataType{upbitSignalData(IndicatorSpec{Type: IndicatorTypeRSI, Params: IndicatorParams{Period: 14}, EventParams: IndicatorEventParams{Threshold: 70}})}, wantErr: true},
		{name: "info signal with trigger of another indicator", kind: streamKindInfo, types: []StreamDataType{upbitSignalData(IndicatorSpec{Type: IndicatorTypeRSI, Params: IndicatorParams{Period: 14}, Trigger: IndicatorTriggerGoldenCross, EventParams: IndicatorEventParams{Threshold: 70}})}, wantErr: true},
		{name: "info rsi signal without threshold", kind: streamKindInfo, types: []StreamDataType{upbitSignalData(IndicatorSpec{Type: IndicatorTypeRSI, Params: IndicatorParams{Period: 14}, Trigger: IndicatorTriggerOverbought})}, wantErr: true},
		{name: "info volume signal without multiplier", kind: streamKindInfo, types: []StreamDataType{upbitSignalData(IndicatorSpec{Type: IndicatorTypeVolume, Params: IndicatorParams{Period: 20}, Trigger: IndicatorTriggerVolumeSpike})}, wantErr: true},
		{name: "info volume signal with NaN multiplier", kind: streamKindInfo, types: []StreamDataType{upbitSignalData(IndicatorSpec{Type: IndicatorTypeVolume, Params: IndicatorParams{Period: 20}, Trigger: IndicatorTriggerVolumeSpike, EventParams: IndicatorEventParams{Multiplier: math.NaN()}})}, wantErr: true},
		{name: "info macd signal with event params", kind: streamKindInfo, types: []StreamDataType{upbitSignalData(IndicatorSpec{Type: IndicatorTypeMACD, Params: IndicatorParams{Fast: 12, Slow: 26, SignalPeriod: 9}, Trigger: IndicatorTriggerGoldenCross, EventParams: IndicatorEventParams{Threshold: 1}})}, wantErr: true},
		{name: "info rsi with macd params", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorData(IndicatorSpec{Type: IndicatorTypeRSI, Params: IndicatorParams{Period: 14, Fast: 12}})}, wantErr: true},
		{name: "info rsi without period", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorData(IndicatorSpec{Type: IndicatorTypeRSI})}, wantErr: true},
		{name: "info unsupported moving average type", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorData(IndicatorSpec{Type: IndicatorTypeMovingAverage, Params: IndicatorParams{Type: "WMA", Period: 20}})}, wantErr: true},
		{name: "info crossover with inverted periods", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorData(IndicatorSpec{Type: IndicatorTypeMACrossover, Params: IndicatorParams{Type: MovingAverageSMA, ShortPeriod: 20, LongPeriod: 5}})}, wantErr: true},
		{name: "info bollinger bands without multiplier", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorData(IndicatorSpec{Type: IndicatorTypeBollingerBands, Params: IndicatorParams{Period: 20}})}, wantErr: true},
		{name: "info macd without signal period", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorData(IndicatorSpec{Type: IndicatorTypeMACD, Params: IndicatorParams{Fast: 12, Slow: 26}})}, wantErr: true},
		{name: "info duplicate subscription", kind: streamKindInfo, types: []StreamDataType{upbitIndicatorData(rsi, rsi)}, wantErr: true},
		{name: "info snapshot flag", kind: streamKindInfo, types: []StreamDataType{{Type: StreamTypeIndicator, Codes: []string{"KRW-BTC"}, Timeframe: IndicatorTimeframe5m, Indicators: []IndicatorSpec{rsi}, OnlySnapshot: true}}, wantErr: true},
		{name: "info announcement categories", kind: streamKindInfo, types: []StreamDataType{{Type: StreamTypeIndicator, Codes: []string{"KRW-BTC"}, Timeframe: IndicatorTimeframe5m, Indicators: []IndicatorSpec{rsi}, Categories: []AnnouncementCategory{AnnouncementCategoryTrade}}}, wantErr: true},
		{name: "info level", kind: streamKindInfo, types: []StreamDataType{{Type: StreamTypeIndicator, Codes: []string{"KRW-BTC"}, Timeframe: IndicatorTimeframe5m, Indicators: []IndicatorSpec{rsi}, Level: "10000"}}, wantErr: true},
		{name: "private announcement", kind: streamKindPrivate, types: []StreamDataType{announcement}},
		{name: "private announcement without options", kind: streamKindPrivate, types: []StreamDataType{{Type: StreamTypeAnnouncement}, {Type: "myAsset"}}},
		{name: "private announcement with codes", kind: streamKindPrivate, types: []StreamDataType{{Type: StreamTypeAnnouncement, Codes: []string{"KRW-BTC"}}}, wantErr: true},
		{name: "private announcement unsupported category", kind: streamKindPrivate, types: []StreamDataType{{Type: StreamTypeAnnouncement, Categories: []AnnouncementCategory{"trading"}}}, wantErr: true},
		{name: "private announcement duplicate category", kind: streamKindPrivate, types: []StreamDataType{{Type: StreamTypeAnnouncement, Categories: []AnnouncementCategory{AnnouncementCategoryTrade, AnnouncementCategoryTrade}}}, wantErr: true},
		{name: "private announcement with timeframe", kind: streamKindPrivate, types: []StreamDataType{{Type: StreamTypeAnnouncement, Timeframe: IndicatorTimeframe5m}}, wantErr: true},
		{name: "private announcement with realtime flag", kind: streamKindPrivate, types: []StreamDataType{{Type: StreamTypeAnnouncement, OnlyRealtime: true}}, wantErr: true},
		{name: "private indicator type", kind: streamKindPrivate, types: []StreamDataType{upbitIndicatorData(rsi)}, wantErr: true},
		{name: "public ticker with categories", kind: streamKindPublic, types: []StreamDataType{{Type: "ticker", Codes: []string{"KRW-BTC"}, Categories: []AnnouncementCategory{AnnouncementCategoryTrade}}}, wantErr: true},
		{name: "public ticker with include body", kind: streamKindPublic, types: []StreamDataType{{Type: "ticker", Codes: []string{"KRW-BTC"}, IncludeBody: true}}, wantErr: true},
		{name: "public ticker with timeframe", kind: streamKindPublic, types: []StreamDataType{{Type: "ticker", Codes: []string{"KRW-BTC"}, Timeframe: IndicatorTimeframe5m}}, wantErr: true},
		{name: "public announcement type", kind: streamKindPublic, types: []StreamDataType{{Type: StreamTypeAnnouncement}}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateStreamRequest(StreamRequest{Types: test.types}, test.kind)
			if test.wantErr {
				if !errors.Is(err, trade.ErrValidation) {
					t.Fatalf("validateStreamRequest() error = %v, want validation error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateStreamRequest() error = %v", err)
			}
		})
	}
}

func TestValidateStreamRequestCopiesIndicatorsAndNormalizesCodes(t *testing.T) {
	t.Parallel()

	specs := []IndicatorSpec{{Type: IndicatorTypeRSI, Params: IndicatorParams{Period: 14}}}
	categories := []AnnouncementCategory{AnnouncementCategoryNotice}
	indicatorRequest, err := validateStreamRequest(StreamRequest{Types: []StreamDataType{
		{Type: StreamTypeIndicator, Codes: []string{"krw-btc"}, Timeframe: IndicatorTimeframe5m, Indicators: specs},
	}}, streamKindInfo)
	if err != nil {
		t.Fatalf("validateStreamRequest(info) error = %v", err)
	}
	announcementRequest, err := validateStreamRequest(StreamRequest{Types: []StreamDataType{
		{Type: StreamTypeAnnouncement, Categories: categories},
	}}, streamKindPrivate)
	if err != nil {
		t.Fatalf("validateStreamRequest(private) error = %v", err)
	}
	specs[0].Params.Period = 99
	categories[0] = AnnouncementCategoryEvent

	validated := indicatorRequest.Types[0]
	if validated.Indicators[0].Params.Period != 14 || !slices.Equal(validated.Codes, []string{"KRW-BTC"}) {
		t.Fatalf("validated indicator type = %+v", validated)
	}
	if announcementRequest.Types[0].Categories[0] != AnnouncementCategoryNotice {
		t.Fatalf("validated announcement type = %+v", announcementRequest.Types[0])
	}
	if indicatorRequest.Format != StreamFormatDefault {
		t.Fatalf("Format = %q, want %q", indicatorRequest.Format, StreamFormatDefault)
	}
}

func upbitIndicatorData(specs ...IndicatorSpec) StreamDataType {
	return StreamDataType{Type: StreamTypeIndicator, Codes: []string{"KRW-BTC"}, Timeframe: IndicatorTimeframe5m, Indicators: specs}
}

func upbitSignalData(specs ...IndicatorSpec) StreamDataType {
	return StreamDataType{Type: StreamTypeIndicatorSignal, Codes: []string{"KRW-BTC"}, Timeframe: IndicatorTimeframe5m, Indicators: specs}
}

// upbitIndicatorLoad는 서로 다른 RSI 기간 count개를 codes개 마켓에 구독하는 데이터 종류를 만든다.
func upbitIndicatorLoad(streamType string, codes, count int) StreamDataType {
	dataType := StreamDataType{Type: streamType, Timeframe: IndicatorTimeframe5m}
	for index := range codes {
		dataType.Codes = append(dataType.Codes, fmt.Sprintf("KRW-C%03d", index))
	}
	for index := range count {
		spec := IndicatorSpec{Type: IndicatorTypeRSI, Params: IndicatorParams{Period: index + 1}}
		if streamType == StreamTypeIndicatorSignal {
			spec.Trigger = IndicatorTriggerOverbought
			spec.EventParams = IndicatorEventParams{Threshold: 70}
		}
		dataType.Indicators = append(dataType.Indicators, spec)
	}
	return dataType
}

func decodeUpbitStreamPayload(t *testing.T, payload string, target any) {
	t.Helper()
	message, err := DecodeStreamMessage(corestream.Message{Data: []byte(payload)})
	if err != nil {
		t.Fatalf("DecodeStreamMessage() error = %v", err)
	}
	if err := message.Decode(target); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
}

// verifyUpbitListSubscriptions는 구독 직후 LIST_SUBSCRIPTIONS 요청 전문을 확인하고,
// 같은 연결로 받은 응답이 요청 ticket과 함께 StreamSubscriptions로 decode되는지 확인한다.
func verifyUpbitListSubscriptions(
	t *testing.T,
	connection *websocketTestConnection,
	run func(context.Context, func(context.Context, StreamMessage) error) error,
	list func(context.Context) (string, error),
	wantRequest string,
	response string,
) {
	t.Helper()
	messages, stop := startUpbitStream(t, run)
	defer stop()

	_ = waitForWebSocketWrite(t, connection)
	ticket, err := list(context.Background())
	if err != nil || ticket != "list-ticket" {
		t.Fatalf("ListSubscriptions() = %q, %v", ticket, err)
	}
	if payload := waitForWebSocketWrite(t, connection); string(payload) != wantRequest {
		t.Fatalf("list request = %s, want %s", payload, wantRequest)
	}
	connection.reads <- websocketReadResult{message: corestream.Message{Data: []byte(response)}}
	message := waitUpbitStreamMessage(t, messages)
	if message.Method != listSubscriptionsMethod {
		t.Fatalf("message = %+v", message)
	}
	var subscriptions StreamSubscriptions
	if err := message.Decode(&subscriptions); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if subscriptions.Ticket != ticket || len(subscriptions.Result) != 1 {
		t.Fatalf("subscriptions = %+v", subscriptions)
	}
}

// startUpbitStream은 stream을 고루틴에서 실행하고 handler가 받은 메시지를 채널로 전달한다.
// 반환하는 stop은 실행을 취소하고 Run이 context.Canceled로 끝나는지 확인한다.
func startUpbitStream(
	t *testing.T,
	run func(context.Context, func(context.Context, StreamMessage) error) error,
) (<-chan StreamMessage, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan StreamMessage, 8)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, func(_ context.Context, message StreamMessage) error {
			messages <- message
			return nil
		})
	}()
	return messages, func() {
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("Run() error = %v, want context.Canceled", err)
			}
		case <-time.After(time.Second):
			t.Error("Run() did not finish")
		}
	}
}

func waitUpbitStreamMessage(t *testing.T, messages <-chan StreamMessage) StreamMessage {
	t.Helper()
	select {
	case message := <-messages:
		return message
	case <-time.After(time.Second):
		t.Fatal("stream message was not delivered")
		return StreamMessage{}
	}
}
