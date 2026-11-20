package upbit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"

	corestream "github.com/proven-trade/cex-sdk/stream"
)

// 업비트 WebSocket data type이다. announcement는 private, indicator와 indicator_signal은 info 연결에서 구독한다.
const (
	StreamTypeAnnouncement    = "announcement"
	StreamTypeIndicator       = "indicator"
	StreamTypeIndicatorSignal = "indicator_signal"
)

const (
	listSubscriptionsMethod = "LIST_SUBSCRIPTIONS"
	// maxIndicatorCombinations는 한 요청에서 indicator와 indicator_signal이 함께 쓸 수 있는 (codes 수 × indicators 수)의 합이다.
	maxIndicatorCombinations = 100
)

// AnnouncementCategory는 공지사항 카테고리다.
type AnnouncementCategory string

const (
	AnnouncementCategoryNotice       AnnouncementCategory = "notice"
	AnnouncementCategoryTrade        AnnouncementCategory = "trade"
	AnnouncementCategoryWallet       AnnouncementCategory = "wallet"
	AnnouncementCategoryMaintenance  AnnouncementCategory = "maintenance"
	AnnouncementCategoryDigitalAsset AnnouncementCategory = "digital_asset"
	AnnouncementCategoryNFT          AnnouncementCategory = "nft"
	AnnouncementCategoryService      AnnouncementCategory = "service"
	AnnouncementCategoryEvent        AnnouncementCategory = "event"
)

// IndicatorTimeframe은 지표 계산에 쓰는 캔들 단위다.
type IndicatorTimeframe string

const (
	IndicatorTimeframe5m   IndicatorTimeframe = "5m"
	IndicatorTimeframe15m  IndicatorTimeframe = "15m"
	IndicatorTimeframe60m  IndicatorTimeframe = "60m"
	IndicatorTimeframe240m IndicatorTimeframe = "240m"
	IndicatorTimeframe1d   IndicatorTimeframe = "1d"
)

// IndicatorType은 구독할 기술적 지표 종류다.
type IndicatorType string

const (
	IndicatorTypeVolume         IndicatorType = "volume"
	IndicatorTypeRSI            IndicatorType = "rsi"
	IndicatorTypeMovingAverage  IndicatorType = "moving_average"
	IndicatorTypeMACrossover    IndicatorType = "ma_crossover"
	IndicatorTypeBollingerBands IndicatorType = "bollinger_bands"
	IndicatorTypeMACD           IndicatorType = "macd"
)

// MovingAverageType은 이동평균선 종류다.
type MovingAverageType string

const (
	MovingAverageSMA MovingAverageType = "SMA"
	MovingAverageEMA MovingAverageType = "EMA"
)

// IndicatorTrigger는 indicator_signal 조건 이벤트를 발생시키는 트리거다.
type IndicatorTrigger string

const (
	IndicatorTriggerVolumeSpike     IndicatorTrigger = "volume_spike"
	IndicatorTriggerOverbought      IndicatorTrigger = "overbought"
	IndicatorTriggerOversold        IndicatorTrigger = "oversold"
	IndicatorTriggerPriceCrossAbove IndicatorTrigger = "price_cross_above"
	IndicatorTriggerPriceCrossBelow IndicatorTrigger = "price_cross_below"
	IndicatorTriggerGoldenCross     IndicatorTrigger = "golden_cross"
	IndicatorTriggerDeadCross       IndicatorTrigger = "dead_cross"
	IndicatorTriggerZeroCrossAbove  IndicatorTrigger = "zero_cross_above"
	IndicatorTriggerZeroCrossBelow  IndicatorTrigger = "zero_cross_below"
)

var (
	announcementCategories = []AnnouncementCategory{
		AnnouncementCategoryNotice, AnnouncementCategoryTrade, AnnouncementCategoryWallet,
		AnnouncementCategoryMaintenance, AnnouncementCategoryDigitalAsset, AnnouncementCategoryNFT,
		AnnouncementCategoryService, AnnouncementCategoryEvent,
	}
	indicatorTimeframes = []IndicatorTimeframe{
		IndicatorTimeframe5m, IndicatorTimeframe15m, IndicatorTimeframe60m, IndicatorTimeframe240m, IndicatorTimeframe1d,
	}
)

// indicatorTriggers는 지표별로 indicator_signal이 지원하는 트리거다. 키가 지원하는 지표 종류의 전체 목록이다.
var indicatorTriggers = map[IndicatorType][]IndicatorTrigger{
	IndicatorTypeVolume:         {IndicatorTriggerVolumeSpike},
	IndicatorTypeRSI:            {IndicatorTriggerOverbought, IndicatorTriggerOversold},
	IndicatorTypeMovingAverage:  {IndicatorTriggerPriceCrossAbove, IndicatorTriggerPriceCrossBelow},
	IndicatorTypeMACrossover:    {IndicatorTriggerGoldenCross, IndicatorTriggerDeadCross},
	IndicatorTypeBollingerBands: {IndicatorTriggerPriceCrossAbove, IndicatorTriggerPriceCrossBelow},
	IndicatorTypeMACD:           {IndicatorTriggerGoldenCross, IndicatorTriggerDeadCross, IndicatorTriggerZeroCrossAbove, IndicatorTriggerZeroCrossBelow},
}

// IndicatorParams는 지표 계산 파라미터다. 지표마다 쓰는 필드만 채우고 나머지는 0으로 둔다.
// 기간과 개수는 정수, Multiplier는 JSON 숫자로 전송한다. 지원하는 값의 범위는 업비트가 검증한다.
type IndicatorParams struct {
	Period       int               `json:"period,omitempty"`
	Type         MovingAverageType `json:"type,omitempty"`
	ShortPeriod  int               `json:"short_period,omitempty"`
	LongPeriod   int               `json:"long_period,omitempty"`
	Multiplier   float64           `json:"multiplier,omitempty"`
	Fast         int               `json:"fast,omitempty"`
	Slow         int               `json:"slow,omitempty"`
	SignalPeriod int               `json:"signal_period,omitempty"`
}

// IndicatorEventParams는 indicator_signal 조건 판정 파라미터다.
// 거래량 급증은 Multiplier, RSI는 Threshold를 지정하고 나머지 트리거는 비워 둔다.
type IndicatorEventParams struct {
	Threshold  int     `json:"threshold,omitempty"`
	Multiplier float64 `json:"multiplier,omitempty"`
}

// IndicatorSpec은 구독할 지표 한 건의 종류, 계산 파라미터와 조건이다.
// Trigger와 EventParams는 indicator_signal에서만 쓰며 indicator에서는 비워 둔다.
type IndicatorSpec struct {
	Type        IndicatorType        `json:"type"`
	Params      IndicatorParams      `json:"params"`
	Trigger     IndicatorTrigger     `json:"trigger,omitempty"`
	EventParams IndicatorEventParams `json:"event_params,omitzero"`
}

// StreamAnnouncement는 공지사항 신규 게시와 갱신 이벤트다.
// EventType은 CREATED(신규 게시) 또는 UPDATED(기존 공지 갱신)이고 Body는 include_body를 요청한 경우에만 채워진다.
type StreamAnnouncement struct {
	Type          string               `json:"type"`
	EventType     string               `json:"event_type"`
	UUID          string               `json:"uuid"`
	Title         string               `json:"title"`
	Category      AnnouncementCategory `json:"category"`
	URL           string               `json:"url"`
	FirstListedAt string               `json:"first_listed_at"`
	ListedAt      string               `json:"listed_at"`
	Timestamp     int64                `json:"timestamp"`
	StreamType    string               `json:"stream_type"`
	Body          string               `json:"body,omitempty"`
}

// IndicatorValue는 계산된 지표 값이다. 지표 종류에 해당하는 필드만 채워진다.
type IndicatorValue struct {
	Volume        Decimal `json:"volume"`
	AverageVolume Decimal `json:"avg_volume"`
	Ratio         Decimal `json:"ratio"`
	RSI           Decimal `json:"rsi"`
	Value         Decimal `json:"value"`
	MAShort       Decimal `json:"ma_short"`
	MALong        Decimal `json:"ma_long"`
	BBUpper       Decimal `json:"bb_upper"`
	BBMiddle      Decimal `json:"bb_middle"`
	BBLower       Decimal `json:"bb_lower"`
	MACD          Decimal `json:"macd"`
	MACDSignal    Decimal `json:"macd_signal"`
	Histogram     Decimal `json:"histogram"`
}

// StreamIndicator는 선택한 캔들이 마감될 때 전송되는 지표 값 이벤트다.
type StreamIndicator struct {
	Type              string             `json:"type"`
	Code              string             `json:"code"`
	Timeframe         IndicatorTimeframe `json:"timeframe"`
	Indicator         IndicatorType      `json:"indicator"`
	Params            IndicatorParams    `json:"params"`
	Value             IndicatorValue     `json:"value"`
	CandleDateTimeUTC string             `json:"candle_date_time_utc"`
	Timestamp         int64              `json:"timestamp"`
	StreamType        string             `json:"stream_type"`
}

// StreamIndicatorSignal은 지표 조건이 미충족에서 충족으로 바뀔 때 전송되는 이벤트다.
// 별도 조건 파라미터가 없는 트리거의 EventParams는 0 값이다.
type StreamIndicatorSignal struct {
	Type              string               `json:"type"`
	Code              string               `json:"code"`
	Timeframe         IndicatorTimeframe   `json:"timeframe"`
	Indicator         IndicatorType        `json:"indicator"`
	Params            IndicatorParams      `json:"params"`
	Trigger           IndicatorTrigger     `json:"trigger"`
	EventParams       IndicatorEventParams `json:"event_params"`
	Value             IndicatorValue       `json:"value"`
	CandleDateTimeUTC string               `json:"candle_date_time_utc"`
	Timestamp         int64                `json:"timestamp"`
	StreamType        string               `json:"stream_type"`
}

// StreamSubscription은 현재 연결에서 구독 중인 스트림 한 건이다.
// Codes, Level, Timeframe, Indicators는 해당 데이터 종류가 쓰는 경우에만 채워진다.
// 문서에 축약형이 없는 SIMPLE 필드는 StreamMessage.Payload에서 직접 읽는다.
type StreamSubscription struct {
	Type       string             `json:"type"`
	Codes      []string           `json:"codes"`
	Level      Decimal            `json:"level"`
	Timeframe  IndicatorTimeframe `json:"timeframe"`
	Indicators []IndicatorSpec    `json:"indicators"`
}

// StreamSubscriptions는 LIST_SUBSCRIPTIONS 응답이다. Ticket은 요청에 쓴 ticket과 같다.
type StreamSubscriptions struct {
	Method string               `json:"method"`
	Result []StreamSubscription `json:"result"`
	Ticket string               `json:"ticket"`
}

// UnmarshalJSON은 DEFAULT와 SIMPLE 계열 공지사항 필드를 같은 타입으로 변환한다.
func (event *StreamAnnouncement) UnmarshalJSON(data []byte) error {
	type plain StreamAnnouncement
	var decoded plain
	if err := decodeExpandedStream(data, announcementKeys, &decoded); err != nil {
		return fmt.Errorf("decode Upbit stream announcement: %w", err)
	}
	*event = StreamAnnouncement(decoded)
	return nil
}

// UnmarshalJSON은 DEFAULT와 SIMPLE 계열 지표 값 필드를 같은 타입으로 변환한다.
func (event *StreamIndicator) UnmarshalJSON(data []byte) error {
	type plain StreamIndicator
	var decoded plain
	if err := decodeExpandedStream(data, indicatorEventKeys, &decoded); err != nil {
		return fmt.Errorf("decode Upbit stream indicator: %w", err)
	}
	*event = StreamIndicator(decoded)
	return nil
}

// UnmarshalJSON은 DEFAULT와 SIMPLE 계열 지표 조건 이벤트 필드를 같은 타입으로 변환한다.
func (event *StreamIndicatorSignal) UnmarshalJSON(data []byte) error {
	type plain StreamIndicatorSignal
	var decoded plain
	if err := decodeExpandedStream(data, indicatorEventKeys, &decoded); err != nil {
		return fmt.Errorf("decode Upbit stream indicator signal: %w", err)
	}
	*event = StreamIndicatorSignal(decoded)
	return nil
}

// UnmarshalJSON은 DEFAULT와 SIMPLE 계열 구독 목록 필드를 같은 타입으로 변환한다.
func (subscriptions *StreamSubscriptions) UnmarshalJSON(data []byte) error {
	type plain StreamSubscriptions
	var decoded plain
	if err := decodeExpandedStream(data, subscriptionsKeys, &decoded); err != nil {
		return fmt.Errorf("decode Upbit stream subscriptions: %w", err)
	}
	*subscriptions = StreamSubscriptions(decoded)
	return nil
}

// streamObjectKeys는 SIMPLE 형식의 축약 필드명을 DEFAULT 필드명으로 되돌리는 규칙이다.
// nested는 변환한 필드명을 기준으로 하위 객체나 객체 배열에 적용할 규칙이다.
type streamObjectKeys struct {
	aliases map[string]string
	nested  map[string]streamObjectKeys
}

var (
	announcementKeys = streamObjectKeys{
		aliases: map[string]string{
			"ty": "type", "et": "event_type", "uid": "uuid", "tt": "title", "cat": "category",
			"flat": "first_listed_at", "lat": "listed_at", "tms": "timestamp", "st": "stream_type", "bd": "body",
		},
	}
	indicatorParamKeys = streamObjectKeys{
		aliases: map[string]string{
			"p": "period", "t": "type", "shp": "short_period", "lop": "long_period",
			"m": "multiplier", "f": "fast", "s": "slow", "sgp": "signal_period",
		},
	}
	indicatorEventParamKeys = streamObjectKeys{
		aliases: map[string]string{"th": "threshold", "mul": "multiplier"},
	}
	indicatorValueKeys = streamObjectKeys{
		aliases: map[string]string{
			"vol": "volume", "av": "avg_volume", "rto": "ratio", "mas": "ma_short", "mal": "ma_long",
			"bbu": "bb_upper", "bbm": "bb_middle", "bbl": "bb_lower", "macds": "macd_signal", "hist": "histogram",
		},
	}
	// indicatorEventKeys는 indicator와 indicator_signal 이벤트가 함께 쓴다. tr과 ep는 indicator_signal에만 있다.
	indicatorEventKeys = streamObjectKeys{
		aliases: map[string]string{
			"ty": "type", "cd": "code", "tf": "timeframe", "ind": "indicator", "p": "params", "tr": "trigger",
			"ep": "event_params", "v": "value", "cdttmu": "candle_date_time_utc", "tms": "timestamp", "st": "stream_type",
		},
		nested: map[string]streamObjectKeys{
			"params": indicatorParamKeys, "event_params": indicatorEventParamKeys, "value": indicatorValueKeys,
		},
	}
	subscriptionsKeys = streamObjectKeys{
		aliases: map[string]string{"mthd": "method", "rslt": "result", "tckt": "ticket"},
		nested: map[string]streamObjectKeys{
			"result": {aliases: map[string]string{"ty": "type", "cds": "codes", "lv": "level"}},
		},
	}
)

// expand는 JSON 객체와 객체 배열의 축약 필드명을 DEFAULT 필드명으로 바꾼다.
// 이미 DEFAULT 이름인 필드와 객체가 아닌 값은 그대로 둔다.
func (keys streamObjectKeys) expand(data json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	switch {
	case bytes.HasPrefix(trimmed, []byte("[")):
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, err
		}
		for index, item := range items {
			expanded, err := keys.expand(item)
			if err != nil {
				return nil, err
			}
			items[index] = expanded
		}
		return json.Marshal(items)
	case bytes.HasPrefix(trimmed, []byte("{")):
		var object map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &object); err != nil {
			return nil, err
		}
		expanded := make(map[string]json.RawMessage, len(object))
		for key, value := range object {
			if name, ok := keys.aliases[key]; ok {
				key = name
			}
			if child, ok := keys.nested[key]; ok {
				resolved, err := child.expand(value)
				if err != nil {
					return nil, err
				}
				value = resolved
			}
			expanded[key] = value
		}
		return json.Marshal(expanded)
	default:
		return data, nil
	}
}

func decodeExpandedStream(data []byte, keys streamObjectKeys, target any) error {
	expanded, err := keys.expand(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(expanded, target)
}

// listSubscriptions는 현재 연결에 LIST_SUBSCRIPTIONS를 보내고 요청 ticket을 반환한다.
// format은 기존 스트림과 같은 값을 보낸다. 다른 값을 보내면 실시간 스트림의 응답 형식도 바뀐다.
func (managed *managedStream) listSubscriptions(ctx context.Context) (string, error) {
	ticket, err := managed.ticketSource()
	if err != nil {
		return "", fmt.Errorf("create Upbit stream ticket: %w", err)
	}
	payload, err := json.Marshal([]map[string]string{
		{"ticket": ticket},
		{"method": listSubscriptionsMethod},
		{"format": string(managed.request.Format)},
	})
	if err != nil {
		return "", fmt.Errorf("encode Upbit stream list subscriptions: %w", err)
	}
	if err := managed.session.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload}); err != nil {
		return "", err
	}
	return ticket, nil
}

// validateStreamExtensionFields는 announcement와 indicator 계열 구독 필드를 검사하고 정규화한 복사본을 반환한다.
// 해당 데이터 종류에 속하지 않는 필드는 거부한다.
func validateStreamExtensionFields(dataType StreamDataType) (StreamDataType, error) {
	announcement := dataType.Type == StreamTypeAnnouncement
	indicator := dataType.Type == StreamTypeIndicator || dataType.Type == StreamTypeIndicatorSignal
	if !announcement && (len(dataType.Categories) > 0 || dataType.IncludeBody) {
		return StreamDataType{}, validationError("only announcement accepts categories and include_body")
	}
	if !indicator && (dataType.Timeframe != "" || len(dataType.Indicators) > 0) {
		return StreamDataType{}, validationError("only indicator and indicator_signal accept timeframe and indicators")
	}
	if (announcement || indicator) && (dataType.OnlySnapshot || dataType.OnlyRealtime) {
		return StreamDataType{}, validationError("%s does not accept snapshot or realtime flags", dataType.Type)
	}
	switch {
	case announcement:
		categories, err := validateAnnouncementCategories(dataType.Categories)
		if err != nil {
			return StreamDataType{}, err
		}
		dataType.Categories = categories
	case indicator:
		indicators, err := validateIndicatorSpecs(dataType)
		if err != nil {
			return StreamDataType{}, err
		}
		dataType.Indicators = indicators
	}
	return dataType, nil
}

func validateAnnouncementCategories(categories []AnnouncementCategory) ([]AnnouncementCategory, error) {
	seen := make(map[AnnouncementCategory]struct{}, len(categories))
	for _, category := range categories {
		if !slices.Contains(announcementCategories, category) {
			return nil, validationError("unsupported announcement category %q", category)
		}
		if _, exists := seen[category]; exists {
			return nil, validationError("duplicate announcement category %q", category)
		}
		seen[category] = struct{}{}
	}
	return slices.Clone(categories), nil
}

func validateIndicatorSpecs(dataType StreamDataType) ([]IndicatorSpec, error) {
	if !slices.Contains(indicatorTimeframes, dataType.Timeframe) {
		return nil, validationError("unsupported indicator timeframe %q", dataType.Timeframe)
	}
	if len(dataType.Indicators) == 0 {
		return nil, validationError("%s requires at least one indicator", dataType.Type)
	}
	signal := dataType.Type == StreamTypeIndicatorSignal
	for _, spec := range dataType.Indicators {
		if err := validateIndicatorSpec(spec, signal); err != nil {
			return nil, err
		}
	}
	return slices.Clone(dataType.Indicators), nil
}

func validateIndicatorSpec(spec IndicatorSpec, signal bool) error {
	triggers, ok := indicatorTriggers[spec.Type]
	if !ok {
		return validationError("unsupported indicator type %q", spec.Type)
	}
	if err := validateIndicatorParams(spec.Type, spec.Params); err != nil {
		return err
	}
	if !signal {
		if spec.Trigger != "" || spec.EventParams != (IndicatorEventParams{}) {
			return validationError("indicator %s does not accept trigger or event_params", spec.Type)
		}
		return nil
	}
	if !slices.Contains(triggers, spec.Trigger) {
		return validationError("unsupported %s trigger %q", spec.Type, spec.Trigger)
	}
	return validateIndicatorEventParams(spec)
}

// validateIndicatorParams는 지표가 요구하는 파라미터가 모두 있고 다른 파라미터가 없는지 검사한다.
// 기간 같은 값의 지원 범위는 업비트가 바꿀 수 있으므로 양수인지만 확인한다.
func validateIndicatorParams(indicator IndicatorType, params IndicatorParams) error {
	rest := params
	switch indicator {
	case IndicatorTypeVolume, IndicatorTypeRSI:
		if params.Period <= 0 {
			return validationError("%s indicator requires params.period", indicator)
		}
		rest.Period = 0
	case IndicatorTypeMovingAverage:
		if err := validateMovingAverageType(params.Type); err != nil {
			return err
		}
		if params.Period <= 0 {
			return validationError("moving_average indicator requires params.period")
		}
		rest.Type, rest.Period = "", 0
	case IndicatorTypeMACrossover:
		if err := validateMovingAverageType(params.Type); err != nil {
			return err
		}
		if params.ShortPeriod <= 0 || params.LongPeriod <= params.ShortPeriod {
			return validationError("ma_crossover indicator requires 0 < params.short_period < params.long_period")
		}
		rest.Type, rest.ShortPeriod, rest.LongPeriod = "", 0, 0
	case IndicatorTypeBollingerBands:
		if params.Period <= 0 {
			return validationError("bollinger_bands indicator requires params.period")
		}
		if err := validateIndicatorMultiplier("params.multiplier", params.Multiplier); err != nil {
			return err
		}
		rest.Period, rest.Multiplier = 0, 0
	case IndicatorTypeMACD:
		if params.Fast <= 0 || params.Slow <= params.Fast || params.SignalPeriod <= 0 {
			return validationError("macd indicator requires 0 < params.fast < params.slow and params.signal_period > 0")
		}
		rest.Fast, rest.Slow, rest.SignalPeriod = 0, 0, 0
	}
	if rest != (IndicatorParams{}) {
		return validationError("%s indicator does not accept the given params", indicator)
	}
	return nil
}

func validateIndicatorEventParams(spec IndicatorSpec) error {
	rest := spec.EventParams
	switch spec.Type {
	case IndicatorTypeVolume:
		if err := validateIndicatorMultiplier("event_params.multiplier", rest.Multiplier); err != nil {
			return err
		}
		rest.Multiplier = 0
	case IndicatorTypeRSI:
		if rest.Threshold <= 0 {
			return validationError("rsi trigger requires event_params.threshold")
		}
		rest.Threshold = 0
	}
	if rest != (IndicatorEventParams{}) {
		return validationError("%s trigger %q does not accept the given event_params", spec.Type, spec.Trigger)
	}
	return nil
}

func validateMovingAverageType(value MovingAverageType) error {
	if value != MovingAverageSMA && value != MovingAverageEMA {
		return validationError("moving average type must be SMA or EMA")
	}
	return nil
}

func validateIndicatorMultiplier(name string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return validationError("%s must be a positive finite number", name)
	}
	return nil
}

// validateIndicatorCombinations는 한 요청의 지표 구독 조합 수 상한과 중복을 검사한다.
// 조합 수는 indicator와 indicator_signal을 합쳐 각 데이터 종류의 (codes 수 × indicators 수)의 합이다.
func validateIndicatorCombinations(types []StreamDataType) error {
	total := 0
	seen := make(map[string]struct{})
	for _, dataType := range types {
		if dataType.Type != StreamTypeIndicator && dataType.Type != StreamTypeIndicatorSignal {
			continue
		}
		total += len(dataType.Codes) * len(dataType.Indicators)
		for _, spec := range dataType.Indicators {
			specJSON, err := json.Marshal(spec)
			if err != nil {
				return validationError("encode indicator spec: %v", err)
			}
			for _, code := range dataType.Codes {
				key := dataType.Type + "|" + code + "|" + string(dataType.Timeframe) + "|" + string(specJSON)
				if _, exists := seen[key]; exists {
					return validationError("duplicate %s subscription %s %s %s", dataType.Type, code, dataType.Timeframe, spec.Type)
				}
				seen[key] = struct{}{}
			}
		}
	}
	if total > maxIndicatorCombinations {
		return validationError("indicator subscription combinations %d exceed %d", total, maxIndicatorCombinations)
	}
	return nil
}
