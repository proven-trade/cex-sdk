package kraken

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// AssetsRequest는 자산 정보 조회 조건이다. Assets가 비어 있으면 전체 자산을 조회한다.
type AssetsRequest struct {
	Assets     []string
	AssetClass AssetClass
	// DisplayNames가 true이면 assetVersion=1로 표시용 자산 이름을 key로 받는다.
	DisplayNames bool
}

// Asset은 입금, 출금, 거래, Earn에 쓰이는 자산 정보다.
type Asset struct {
	ID              string          `json:"-"`
	AssetClass      string          `json:"aclass"`
	AltName         string          `json:"altname"`
	Decimals        int             `json:"decimals"`
	DisplayDecimals int             `json:"display_decimals"`
	CollateralValue float64         `json:"collateral_value"`
	MarginRate      float64         `json:"margin_rate"`
	Status          string          `json:"status"`
	Raw             json.RawMessage `json:"-"`
}

// MaintenanceEvent는 예정된 점검 한 건이다.
type MaintenanceEvent struct {
	EventID            int64     `json:"event_id"`
	Title              string    `json:"title"`
	ExpectedStart      time.Time `json:"expected_start_utc"`
	ExpectedEnd        time.Time `json:"expected_end_utc"`
	TimeToStartSeconds int64     `json:"time_to_start_s"`
	Phase              string    `json:"phase"`
	AffectedServices   []string  `json:"affected_services"`
	OrderSubmission    string    `json:"order_submission"`
	RecommendedAction  string    `json:"recommended_action"`
	CancelBefore       time.Time `json:"cancel_before_utc"`
	SourceURL          string    `json:"source_url"`
}

// EmergencyStep은 장애 중 공지된 운영 예정 조치다.
type EmergencyStep struct {
	AppliesTo  []string  `json:"applies_to"`
	Type       string    `json:"type"`
	ExpectedAt time.Time `json:"expected_at_utc"`
}

// EmergencyEvent는 해결되지 않은 장애 공지 한 건이다.
type EmergencyEvent struct {
	EventID          int64           `json:"event_id"`
	Title            string          `json:"title"`
	IncidentStatus   string          `json:"incident_status"`
	Impact           string          `json:"impact"`
	AffectedServices []string        `json:"affected_services"`
	StartedAt        time.Time       `json:"started_at_utc"`
	NextSteps        []EmergencyStep `json:"next_steps"`
	SourceURL        string          `json:"source_url"`
}

// SystemStatus는 거래 엔진 상태와 72시간 이내 점검, 진행 중 장애 공지다.
type SystemStatus struct {
	Status              string             `json:"status"`
	Timestamp           time.Time          `json:"timestamp"`
	UpcomingMaintenance []MaintenanceEvent `json:"upcoming_maintenance"`
	Emergency           []EmergencyEvent   `json:"emergency"`
	Raw                 json.RawMessage    `json:"-"`
}

// MaintenanceSchedule은 향후 7일 안에 예정된 점검 목록이다.
type MaintenanceSchedule struct {
	Events []MaintenanceEvent `json:"events"`
	Raw    json.RawMessage    `json:"-"`
}

// SpreadsRequest는 최근 top-of-book 스프레드 조회 조건이다.
type SpreadsRequest struct {
	Pair       string
	Since      int64
	AssetClass AssetClass
	// DisplayNames가 true이면 assetVersion=1로 표시용 pair 이름을 key로 받는다.
	DisplayNames bool
}

// SpreadEntry는 시각별 최우선 매수·매도 호가다.
type SpreadEntry struct {
	Time int64
	Bid  string
	Ask  string
}

// Spreads는 최근 스프레드와 다음 조회용 cursor다.
type Spreads struct {
	PairID  string
	Entries []SpreadEntry
	Last    int64
	Raw     json.RawMessage
}

// GroupedBookRequest는 가격 구간별 집계 호가 조회 조건이다.
type GroupedBookRequest struct {
	Pair string
	// Depth는 side별 가격 구간 수다. 0이면 서버 기본값(10)을 쓴다.
	Depth int
	// Grouping은 구간 하나에 묶을 tick 수다. 0이면 서버 기본값(1)을 쓴다.
	Grouping int
}

// GroupedBookLevel은 집계된 가격 구간 하나다.
type GroupedBookLevel struct {
	Price    string `json:"price"`
	Quantity string `json:"qty"`
}

// GroupedBook은 가격 구간별로 수량을 합산한 호가다.
type GroupedBook struct {
	Pair     string             `json:"pair"`
	Grouping int                `json:"grouping"`
	Bids     []GroupedBookLevel `json:"bids"`
	Asks     []GroupedBookLevel `json:"asks"`
	Raw      json.RawMessage    `json:"-"`
}

// Level3Request는 L3 개별 주문 호가 조회 조건이다.
type Level3Request struct {
	Pair string
	// Depth는 side별 가격 구간 수다. 0이면 서버 기본값(100)을 쓴다.
	Depth int
	// FullBook이 true이면 depth 0으로 전체 호가를 요청한다. Depth와 함께 쓸 수 없다.
	FullBook bool
}

// Level3Order는 L3 호가의 개별 주문 한 건이다. Timestamp는 Unix 나노초다.
type Level3Order struct {
	Price     string `json:"price"`
	Quantity  string `json:"qty"`
	OrderID   string `json:"order_id"`
	Timestamp int64  `json:"timestamp"`
}

// Level3Book은 개별 주문 단위의 호가 스냅샷이다.
type Level3Book struct {
	Pair string          `json:"pair"`
	Bids []Level3Order   `json:"bids"`
	Asks []Level3Order   `json:"asks"`
	Raw  json.RawMessage `json:"-"`
}

// PreTradeRequest는 거래 전 투명성 호가 조회 조건이다.
type PreTradeRequest struct {
	Symbol string
}

// PreTradeLevel은 거래 전 투명성 호가의 가격 구간 하나다.
type PreTradeLevel struct {
	Side            string `json:"side"`
	Price           string `json:"price"`
	Quantity        string `json:"qty"`
	Count           int    `json:"count"`
	SubmissionTime  string `json:"submission_ts"`
	PublicationTime string `json:"publication_ts"`
}

// PreTrade는 상위 10호가의 거래 전 투명성 데이터다.
type PreTrade struct {
	Symbol            string          `json:"symbol"`
	Description       string          `json:"description"`
	BaseAsset         string          `json:"base_asset"`
	BaseDTICode       string          `json:"base_dti_code"`
	BaseDTIShortName  string          `json:"base_dti_short_name"`
	QuoteDTICode      string          `json:"quote_dti_code"`
	QuoteDTIShortName string          `json:"quote_dti_short_name"`
	BaseNotation      string          `json:"base_notation"`
	QuoteAsset        string          `json:"quote_asset"`
	QuoteNotation     string          `json:"quote_notation"`
	Venue             string          `json:"venue"`
	System            string          `json:"system"`
	Bids              []PreTradeLevel `json:"bids"`
	Asks              []PreTradeLevel `json:"asks"`
	Raw               json.RawMessage `json:"-"`
}

// PostTradeRequest는 거래 후 투명성 체결 조회 조건이다. 모든 필드가 선택이다.
type PostTradeRequest struct {
	Symbol string
	From   *time.Time
	To     *time.Time
	Count  int
}

// PostTradeEntry는 거래 후 투명성 체결 한 건이다. 시각은 ISO 8601 원문을 보존한다.
type PostTradeEntry struct {
	TradeID          string `json:"trade_id"`
	Price            string `json:"price"`
	Quantity         string `json:"quantity"`
	Symbol           string `json:"symbol"`
	Description      string `json:"description"`
	BaseAsset        string `json:"base_asset"`
	BaseNotation     string `json:"base_notation"`
	QuoteAsset       string `json:"quote_asset"`
	QuoteNotation    string `json:"quote_notation"`
	TradeVenue       string `json:"trade_venue"`
	TradeTime        string `json:"trade_ts"`
	PublicationVenue string `json:"publication_venue"`
	PublicationTime  string `json:"publication_ts"`
}

// PostTrade는 거래 후 투명성 체결 목록과 다음 조회용 시각이다.
type PostTrade struct {
	LastTime string           `json:"last_ts"`
	Count    int              `json:"count"`
	Trades   []PostTradeEntry `json:"trades"`
	Raw      json.RawMessage  `json:"-"`
}

func (request AssetsRequest) validate() error {
	if err := validateAssetNames("asset", request.Assets, false); err != nil {
		return err
	}
	return validateAssetClass(request.AssetClass)
}

func (request AssetsRequest) values() url.Values {
	values := make(url.Values)
	if len(request.Assets) > 0 {
		values.Set("asset", strings.Join(request.Assets, ","))
	}
	setAssetClass(values, "aclass", request.AssetClass)
	if request.DisplayNames {
		values.Set("assetVersion", "1")
	}
	return values
}

func (request SpreadsRequest) validate() error {
	if err := validatePair(request.Pair); err != nil {
		return err
	}
	if request.Since < 0 {
		return validationError("spread since cannot be negative")
	}
	if request.AssetClass != "" && request.AssetClass != AssetClassTokenizedAsset {
		return validationError("spread asset class must be tokenized_asset or empty")
	}
	return nil
}

func (request SpreadsRequest) values() url.Values {
	values := url.Values{"pair": {request.Pair}}
	if request.Since > 0 {
		values.Set("since", strconv.FormatInt(request.Since, 10))
	}
	setAssetClass(values, "asset_class", request.AssetClass)
	if request.DisplayNames {
		values.Set("assetVersion", "1")
	}
	return values
}

func (request GroupedBookRequest) validate() error {
	if err := validatePair(request.Pair); err != nil {
		return err
	}
	switch request.Depth {
	case 0, 10, 25, 100, 250, 1000:
	default:
		return validationError("grouped book depth must be one of 10, 25, 100, 250, 1000")
	}
	switch request.Grouping {
	case 0, 1, 5, 10, 25, 50, 100, 250, 500, 1000:
	default:
		return validationError("grouped book grouping must be one of 1, 5, 10, 25, 50, 100, 250, 500, 1000")
	}
	return nil
}

func (request GroupedBookRequest) values() url.Values {
	values := url.Values{"pair": {request.Pair}}
	setPositiveInt(values, "depth", request.Depth)
	setPositiveInt(values, "grouping", request.Grouping)
	return values
}

func (request Level3Request) validate() error {
	if err := validatePair(request.Pair); err != nil {
		return err
	}
	if request.FullBook && request.Depth != 0 {
		return validationError("level3 full book cannot be combined with depth")
	}
	switch request.Depth {
	case 0, 10, 25, 100, 250, 1000:
	default:
		return validationError("level3 depth must be one of 10, 25, 100, 250, 1000")
	}
	return nil
}

func (request Level3Request) body() map[string]any {
	body := map[string]any{"pair": request.Pair}
	if request.FullBook {
		body["depth"] = 0
	} else if request.Depth > 0 {
		body["depth"] = request.Depth
	}
	return body
}

func (request PreTradeRequest) validate() error {
	if len(request.Symbol) < 3 || len(request.Symbol) > 32 {
		return validationError("pre-trade symbol must be 3 to 32 characters")
	}
	return validatePair(request.Symbol)
}

func (request PreTradeRequest) values() url.Values {
	return url.Values{"symbol": {request.Symbol}}
}

func (request PostTradeRequest) validate() error {
	if request.Symbol != "" {
		if err := validatePair(request.Symbol); err != nil {
			return err
		}
	}
	if err := validateTimeRange(request.From, request.To); err != nil {
		return err
	}
	if request.Count < 0 || request.Count > 1000 {
		return validationError("post-trade count must be between 1 and 1000 or zero for default")
	}
	return nil
}

func (request PostTradeRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "symbol", request.Symbol)
	setRFC3339Nano(values, "from_ts", request.From)
	setRFC3339Nano(values, "to_ts", request.To)
	setPositiveInt(values, "count", request.Count)
	return values
}

// Assets는 입금, 출금, 거래, Earn에 쓰이는 자산 정보를 조회한다.
func (client *Client) Assets(
	ctx context.Context,
	request AssetsRequest,
	options ...trade.RequestOption,
) ([]Asset, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.publicResult(ctx, publicPrefix+"Assets", request.values(), options...)
	if err != nil {
		return nil, err
	}
	entries, err := decodeKeyedEntries[Asset](result)
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	assets := make([]Asset, len(entries))
	for index, entry := range entries {
		assets[index] = entry.Value
		assets[index].ID = entry.Key
		assets[index].Raw = entry.Raw
	}
	return assets, nil
}

// SystemStatus는 거래 엔진 상태와 72시간 이내 점검, 진행 중 장애 공지를 조회한다.
func (client *Client) SystemStatus(
	ctx context.Context,
	options ...trade.RequestOption,
) (SystemStatus, error) {
	response, result, err := client.publicResult(ctx, publicPrefix+"SystemStatus", nil, options...)
	if err != nil {
		return SystemStatus{}, err
	}
	var status SystemStatus
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &status); err != nil {
		return SystemStatus{}, err
	}
	if status.Status == "" {
		return SystemStatus{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Kraken system status is empty"),
		)
	}
	status.Raw = cloneBytes(result)
	return status, nil
}

// MaintenanceSchedule은 향후 7일 안에 예정된 점검 목록을 조회한다.
func (client *Client) MaintenanceSchedule(
	ctx context.Context,
	options ...trade.RequestOption,
) (MaintenanceSchedule, error) {
	response, result, err := client.publicResult(ctx, publicPrefix+"MaintenanceSchedule", nil, options...)
	if err != nil {
		return MaintenanceSchedule{}, err
	}
	var schedule MaintenanceSchedule
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &schedule); err != nil {
		return MaintenanceSchedule{}, err
	}
	schedule.Raw = cloneBytes(result)
	return schedule, nil
}

// Spreads는 단일 Spot 상품의 최근 top-of-book 스프레드를 조회한다.
func (client *Client) Spreads(
	ctx context.Context,
	request SpreadsRequest,
	options ...trade.RequestOption,
) (Spreads, error) {
	if err := request.validate(); err != nil {
		return Spreads{}, err
	}
	response, result, err := client.publicResult(ctx, publicPrefix+"Spread", request.values(), options...)
	if err != nil {
		return Spreads{}, err
	}
	var entries map[string]json.RawMessage
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &entries); err != nil {
		return Spreads{}, err
	}
	lastRaw, exists := entries["last"]
	if !exists {
		return Spreads{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Kraken spread cursor is missing"),
		)
	}
	delete(entries, "last")
	pairID, spreadsRaw, err := singleMapEntry(entries)
	if err != nil {
		return Spreads{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	var tuples [][]json.RawMessage
	if err := client.decodeResultInto(response, commonexchange.OperationRead, spreadsRaw, &tuples); err != nil {
		return Spreads{}, err
	}
	items := make([]SpreadEntry, len(tuples))
	for index, tuple := range tuples {
		item, parseErr := parseSpreadEntry(tuple)
		if parseErr != nil {
			return Spreads{}, client.decodeBodyError(response, commonexchange.OperationRead, parseErr)
		}
		items[index] = item
	}
	last, err := scalarInt64(lastRaw)
	if err != nil {
		return Spreads{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return Spreads{PairID: pairID, Entries: items, Last: last, Raw: cloneBytes(result)}, nil
}

// GroupedBook은 가격 구간별로 수량을 합산한 호가를 조회한다.
func (client *Client) GroupedBook(
	ctx context.Context,
	request GroupedBookRequest,
	options ...trade.RequestOption,
) (GroupedBook, error) {
	if err := request.validate(); err != nil {
		return GroupedBook{}, err
	}
	response, result, err := client.publicResult(
		ctx, publicPrefix+"GroupedBook", request.values(), options...,
	)
	if err != nil {
		return GroupedBook{}, err
	}
	var book GroupedBook
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &book); err != nil {
		return GroupedBook{}, err
	}
	book.Raw = cloneBytes(result)
	return book, nil
}

// Level3는 개별 주문 단위의 L3 호가를 조회한다. API Key에 주문·체결 조회 권한이 필요하다.
func (client *Client) Level3(
	ctx context.Context,
	request Level3Request,
	options ...trade.RequestOption,
) (Level3Book, error) {
	if err := request.validate(); err != nil {
		return Level3Book{}, err
	}
	response, result, err := client.privateJSONResult(
		ctx, privatePrefix+"Level3", request.body(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return Level3Book{}, err
	}
	var book Level3Book
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &book); err != nil {
		return Level3Book{}, err
	}
	book.Raw = cloneBytes(result)
	return book, nil
}

// PreTrade는 상위 10호가의 거래 전 투명성 데이터를 조회한다.
func (client *Client) PreTrade(
	ctx context.Context,
	request PreTradeRequest,
	options ...trade.RequestOption,
) (PreTrade, error) {
	if err := request.validate(); err != nil {
		return PreTrade{}, err
	}
	response, result, err := client.publicResult(ctx, publicPrefix+"PreTrade", request.values(), options...)
	if err != nil {
		return PreTrade{}, err
	}
	var preTrade PreTrade
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &preTrade); err != nil {
		return PreTrade{}, err
	}
	preTrade.Raw = cloneBytes(result)
	return preTrade, nil
}

// PostTrade는 거래 후 투명성 체결 데이터를 조회한다.
func (client *Client) PostTrade(
	ctx context.Context,
	request PostTradeRequest,
	options ...trade.RequestOption,
) (PostTrade, error) {
	if err := request.validate(); err != nil {
		return PostTrade{}, err
	}
	response, result, err := client.publicResult(ctx, publicPrefix+"PostTrade", request.values(), options...)
	if err != nil {
		return PostTrade{}, err
	}
	var postTrade PostTrade
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &postTrade); err != nil {
		return PostTrade{}, err
	}
	postTrade.Raw = cloneBytes(result)
	return postTrade, nil
}

func parseSpreadEntry(tuple []json.RawMessage) (SpreadEntry, error) {
	if len(tuple) < 3 {
		return SpreadEntry{}, errors.New("Kraken spread tuple is too short")
	}
	timestamp, err := scalarInt64(tuple[0])
	if err != nil {
		return SpreadEntry{}, err
	}
	bid, err := scalarString(tuple[1])
	if err != nil {
		return SpreadEntry{}, err
	}
	ask, err := scalarString(tuple[2])
	if err != nil {
		return SpreadEntry{}, err
	}
	return SpreadEntry{Time: timestamp, Bid: bid, Ask: ask}, nil
}
