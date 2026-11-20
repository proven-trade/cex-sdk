package htx

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

var currencyPattern = regexp.MustCompile(`^[a-z0-9]{1,40}$`)

// MarketDetail은 단일 Spot 거래쌍의 최근 24시간 시장 요약이다.
// 공식 문서의 amount·vol 설명은 서로 바뀌어 있어 amount는 base 수량, vol은 quote 거래대금으로 해석한다.
type MarketDetail struct {
	Channel     string
	ID          Scalar
	Version     Scalar
	Open        Decimal
	Close       Decimal
	Low         Decimal
	High        Decimal
	BaseVolume  Decimal
	QuoteVolume Decimal
	Count       int64
	Timestamp   int64
	Raw         json.RawMessage
}

// MarketStatusCode는 HTX 시장 운영 상태다.
type MarketStatusCode int

const (
	// MarketStatusNormal은 주문 접수와 취소가 모두 가능한 상태다.
	MarketStatusNormal MarketStatusCode = 1
	// MarketStatusHalted는 주문 접수와 취소가 모두 금지된 상태다.
	MarketStatusHalted MarketStatusCode = 2
	// MarketStatusCancelOnly는 주문 접수는 금지되고 취소만 가능한 상태다.
	MarketStatusCancelOnly MarketStatusCode = 3
)

// MarketStatus는 시장 운영 상태와 중단 구간·사유·영향 거래쌍이다.
// 중단 필드는 Status가 halted 또는 cancel-only일 때만 의미가 있고,
// HaltEndTime이 0이면 거래소가 종료 시각을 아직 추정하지 못한 것이다.
type MarketStatus struct {
	Status          MarketStatusCode
	HaltStartTime   int64
	HaltEndTime     int64
	HaltReason      int
	AffectedSymbols string
	Raw             json.RawMessage
}

// SettingsRequest는 증분 기준 시각만 받는 HTX 공통 설정 조회 조건이다.
type SettingsRequest struct {
	UpdatedSince *time.Time
}

// SymbolSettings는 `/v1/settings/common/symbols`의 거래쌍 설정과 증분 메타데이터다.
type SymbolSettings struct {
	Symbols   []SymbolSetting
	UpdatedAt Scalar
	Full      int
	Raw       json.RawMessage
}

// SymbolSetting은 HTX Spot 거래쌍의 공개·거래·취소 가능 상태와 정밀도다.
type SymbolSetting struct {
	Symbol                   string          `json:"symbol"`
	Name                     string          `json:"sn"`
	BaseCurrency             string          `json:"bc"`
	QuoteCurrency            string          `json:"qc"`
	BaseCurrencyDisplayName  string          `json:"bcdn"`
	QuoteCurrencyDisplayName string          `json:"qcdn"`
	State                    string          `json:"state"`
	TradeMarket              string          `json:"tm"`
	Partition                string          `json:"sp"`
	Tags                     string          `json:"tags"`
	VisibleEnabled           bool            `json:"ve"`
	WhiteEnabled             bool            `json:"we"`
	Delisted                 bool            `json:"dl"`
	CountryDisabled          bool            `json:"cd"`
	TradeEnabled             bool            `json:"te"`
	CancelEnabled            bool            `json:"ce"`
	TradeEnableTime          int64           `json:"tet"`
	TradeOpenAt              int64           `json:"toa"`
	TradeCloseAt             int64           `json:"tca"`
	VisibleOpenAt            int64           `json:"voa"`
	VisibleCloseAt           int64           `json:"vca"`
	Weight                   Scalar          `json:"w"`
	TradeTotalPrecision      int             `json:"ttp"`
	TradeAmountPrecision     int             `json:"tap"`
	TradePricePrecision      int             `json:"tpp"`
	FeePrecision             int             `json:"fp"`
	Direction                Scalar          `json:"d"`
	ETPLeverageRatio         Scalar          `json:"elr"`
	CallAuctionState         string          `json:"castate"`
	CallAuction1OpenAt       int64           `json:"ca1oa"`
	CallAuction1CloseAt      int64           `json:"ca1ca"`
	CallAuction2OpenAt       int64           `json:"ca2oa"`
	CallAuction2CloseAt      int64           `json:"ca2ca"`
	Raw                      json.RawMessage `json:"-"`
}

// SymbolSettingsV2는 `/v2/settings/common/symbols`의 거래쌍 설정과 증분 메타데이터다.
type SymbolSettingsV2 struct {
	Symbols   []SymbolSettingV2
	UpdatedAt Scalar
	Full      int
	Raw       json.RawMessage
}

// SymbolPartition은 거래쌍이 속한 시장 분류 한 개다.
type SymbolPartition struct {
	ID     Scalar `json:"id"`
	Name   string `json:"name"`
	Weight Scalar `json:"weight"`
}

// SymbolSettingV2는 V2 거래쌍 설정 한 건이다. 레버리지 비율처럼 지원하지 않으면
// null인 값은 빈 문자열로 보존한다.
type SymbolSettingV2 struct {
	Symbol                   string            `json:"sc"`
	DisplayName              string            `json:"dn"`
	IsolatedMarginState      string            `json:"si"`
	CrossMarginState         string            `json:"scr"`
	BaseCurrency             string            `json:"bc"`
	BaseCurrencyDisplayName  string            `json:"bcdn"`
	QuoteCurrency            string            `json:"qc"`
	QuoteCurrencyDisplayName string            `json:"qcdn"`
	State                    string            `json:"state"`
	WhiteEnabled             bool              `json:"whe"`
	CountryDisabled          bool              `json:"cd"`
	TradeEnabled             bool              `json:"te"`
	TradeOpenAt              int64             `json:"toa"`
	Partition                string            `json:"sp"`
	Weight                   Scalar            `json:"w"`
	TradeTotalPrecision      int               `json:"ttp"`
	TradeAmountPrecision     int               `json:"tap"`
	TradePricePrecision      int               `json:"tpp"`
	FeePrecision             int               `json:"fp"`
	SuspendDescription       string            `json:"suspend_desc"`
	TransferBoardDescription string            `json:"transfer_board_desc"`
	Tags                     string            `json:"tags"`
	LeverageRatio            Scalar            `json:"lr"`
	SuperMarginLeverageRatio Scalar            `json:"smlr"`
	FundingLeverageRatio     Scalar            `json:"flr"`
	WithdrawRisk             Scalar            `json:"wr"`
	Direction                Scalar            `json:"d"`
	ETPLeverageRatio         Scalar            `json:"elr"`
	Partitions               []SymbolPartition `json:"p"`
	CallAuctionState         string            `json:"castate"`
	CallAuction1OpenAt       int64             `json:"ca1oa"`
	CallAuction2OpenAt       int64             `json:"ca2oa"`
	Raw                      json.RawMessage   `json:"-"`
}

// CurrencySettings는 `/v1/settings/common/currencys`의 통화 설정과 증분 메타데이터다.
type CurrencySettings struct {
	Currencies []CurrencySetting
	UpdatedAt  Scalar
	Full       int
	Raw        json.RawMessage
}

// CurrencySetting은 V1 통화 설정 한 건이다.
type CurrencySetting struct {
	Name                       string          `json:"name"`
	DisplayName                string          `json:"dn"`
	FullName                   string          `json:"fn"`
	State                      string          `json:"state"`
	CurrencyType               string          `json:"ct"`
	CurrencyPartition          string          `json:"cp"`
	SupportSites               []string        `json:"ss"`
	OTCEnabled                 int             `json:"oe"`
	WithdrawPrecision          int             `json:"wp"`
	DepositMinimumAmount       Scalar          `json:"dma"`
	WithdrawMinimumAmount      Scalar          `json:"wma"`
	ShowPrecision              Scalar          `json:"sp"`
	Weight                     Scalar          `json:"w"`
	QuoteCurrency              bool            `json:"iqc"`
	Visible                    bool            `json:"v"`
	WhiteEnabled               bool            `json:"whe"`
	CountryDisabled            bool            `json:"cd"`
	DepositEnabled             bool            `json:"de"`
	WithdrawEnabled            bool            `json:"we"`
	AddressWithTag             bool            `json:"cawt"`
	AddressOneOff              bool            `json:"cao"`
	FastConfirmations          int             `json:"fc"`
	SafeConfirmations          int             `json:"sc"`
	VisibleAssetsTime          int64           `json:"vat"`
	DepositEnableTime          int64           `json:"det"`
	WithdrawEnableTime         int64           `json:"wet"`
	DepositDescription         string          `json:"dd"`
	WithdrawDescription        string          `json:"wd"`
	SuspendDepositDescription  string          `json:"sdd"`
	SuspendWithdrawDescription string          `json:"swd"`
	SuspendVisibleDescription  string          `json:"svd"`
	Tags                       string          `json:"tags"`
	Raw                        json.RawMessage `json:"-"`
}

// CurrencySettingsV2는 `/v2/settings/common/currencies`의 통화 설정과 증분 메타데이터다.
type CurrencySettingsV2 struct {
	Currencies []CurrencySettingV2
	UpdatedAt  Scalar
	Full       int
	Raw        json.RawMessage
}

// CurrencySettingV2는 V2 통화 설정 한 건이다.
type CurrencySettingV2 struct {
	Currency                   string          `json:"cc"`
	DisplayName                string          `json:"dn"`
	FullName                   string          `json:"fn"`
	State                      string          `json:"state"`
	FeeType                    string          `json:"ft"`
	AssetType                  int             `json:"at"`
	WithdrawPrecision          int             `json:"wp"`
	DepositMinimumAmount       Scalar          `json:"dma"`
	WithdrawMinimumAmount      Scalar          `json:"wma"`
	ShowPrecision              Scalar          `json:"sp"`
	Weight                     Scalar          `json:"w"`
	QuoteCurrency              bool            `json:"qc"`
	Visible                    bool            `json:"v"`
	WhiteEnabled               bool            `json:"whe"`
	CountryDisabled            bool            `json:"cd"`
	DepositEnabled             bool            `json:"de"`
	WithdrawEnabled            bool            `json:"wed"`
	AddressWithTag             bool            `json:"cawt"`
	FastConfirmations          int             `json:"fc"`
	SafeConfirmations          int             `json:"sc"`
	DepositDescription         string          `json:"dd"`
	WithdrawDescription        string          `json:"wd"`
	SuspendDepositDescription  string          `json:"sdd"`
	SuspendWithdrawDescription string          `json:"swd"`
	SuspendVisibleDescription  string          `json:"svd"`
	Tags                       string          `json:"tags"`
	Raw                        json.RawMessage `json:"-"`
}

// ChainDescriptionMode는 체인 설정 응답에 포함할 안내 문구의 범위다.
type ChainDescriptionMode string

const (
	// ChainDescriptionNone은 안내 문구를 제외한다.
	ChainDescriptionNone ChainDescriptionMode = "0"
	// ChainDescriptionAll은 모든 안내 문구를 포함한다.
	ChainDescriptionAll ChainDescriptionMode = "1"
	// ChainDescriptionSuspended는 입출금·체인 교환 중단 안내만 포함한다.
	ChainDescriptionSuspended ChainDescriptionMode = "2"
)

// ChainSettingsRequest는 체인 설정의 통화 필터·안내 문구 범위·증분 기준을 지정한다.
type ChainSettingsRequest struct {
	Currency        string
	ShowDescription ChainDescriptionMode
	UpdatedSince    *time.Time
}

// ChainSettings는 `/v1/settings/common/chains`의 체인 설정과 증분 메타데이터다.
type ChainSettings struct {
	Chains    []ChainSetting
	UpdatedAt Scalar
	Full      int
	Raw       json.RawMessage
}

// ChainSetting은 통화별 체인의 입출금 가능 여부·최소 수량·확인 수다.
type ChainSetting struct {
	Chain                      string          `json:"chain"`
	Currency                   string          `json:"currency"`
	ChainType                  string          `json:"ct"`
	AddressChain               string          `json:"ac"`
	IsDefault                  int             `json:"default"`
	DepositMinimumAmount       Scalar          `json:"dma"`
	WithdrawMinimumAmount      Scalar          `json:"wma"`
	DepositEnabled             bool            `json:"de"`
	WithdrawEnabled            bool            `json:"we"`
	WithdrawPrecision          int             `json:"wp"`
	FeeType                    string          `json:"ft"`
	DisplayName                string          `json:"dn"`
	FullName                   string          `json:"fn"`
	AddressWithTag             bool            `json:"awt"`
	AddressDepositTag          bool            `json:"adt"`
	AddressOneOff              bool            `json:"ao"`
	FastConfirmations          int             `json:"fc"`
	SafeConfirmations          int             `json:"sc"`
	Visible                    bool            `json:"v"`
	DepositDescription         string          `json:"deposit-desc"`
	DepositTipsDescription     string          `json:"deposit-tips-desc"`
	WithdrawDescription        string          `json:"withdraw-desc"`
	WithdrawTipsDescription    string          `json:"withdraw-tips-desc"`
	SuspendDepositDescription  string          `json:"suspend-deposit-desc"`
	SuspendWithdrawDescription string          `json:"suspend-withdraw-desc"`
	SuspendVisibleDescription  string          `json:"suspend-visible-desc"`
	Raw                        json.RawMessage `json:"-"`
}

// ReferenceCurrenciesRequest는 통화·체인 입출금 참조 정보의 통화 필터와 사용자 범위다.
// AuthorizedUser를 nil로 두면 거래소 기본값(true)을 쓴다.
type ReferenceCurrenciesRequest struct {
	Currency       string
	AuthorizedUser *bool
}

// ReferenceCurrency는 통화 하나와 그 통화가 지원하는 체인의 입출금 참조 정보다.
type ReferenceCurrency struct {
	Currency         string
	InstrumentStatus string
	Chains           []ReferenceChain
	Raw              json.RawMessage
}

// ReferenceChain은 체인별 입출금 상태·한도·출금 수수료 정책이다.
// 출금 수수료는 WithdrawFeeType에 따라 고정액·구간액·비율 중 해당하는 값만 채워진다.
type ReferenceChain struct {
	Chain                   string          `json:"chain"`
	DisplayName             string          `json:"displayName"`
	BaseChain               string          `json:"baseChain"`
	BaseChainProtocol       string          `json:"baseChainProtocol"`
	IsDynamic               bool            `json:"isDynamic"`
	DepositStatus           string          `json:"depositStatus"`
	WithdrawStatus          string          `json:"withdrawStatus"`
	Confirmations           int             `json:"numOfConfirmations"`
	FastConfirmations       int             `json:"numOfFastConfirmations"`
	MinDepositAmount        Decimal         `json:"minDepositAmt"`
	MinWithdrawAmount       Decimal         `json:"minWithdrawAmt"`
	MaxWithdrawAmount       Decimal         `json:"maxWithdrawAmt"`
	WithdrawQuotaPerDay     Decimal         `json:"withdrawQuotaPerDay"`
	WithdrawQuotaPerYear    Decimal         `json:"withdrawQuotaPerYear"`
	WithdrawQuotaTotal      Decimal         `json:"withdrawQuotaTotal"`
	WithdrawPrecision       int             `json:"withdrawPrecision"`
	WithdrawFeeType         string          `json:"withdrawFeeType"`
	TransactFeeWithdraw     Scalar          `json:"transactFeeWithdraw"`
	MinTransactFeeWithdraw  Scalar          `json:"minTransactFeeWithdraw"`
	MaxTransactFeeWithdraw  Scalar          `json:"maxTransactFeeWithdraw"`
	TransactFeeRateWithdraw Scalar          `json:"transactFeeRateWithdraw"`
	Raw                     json.RawMessage `json:"-"`
}

// settingsEnvelope는 ts·full 증분 메타데이터를 가진 HTX 설정 목록 응답이다.
type settingsEnvelope struct {
	Data []json.RawMessage `json:"data"`
	TS   Scalar            `json:"ts"`
	Full int               `json:"full"`
}

// MarketDetail은 지정한 Spot 거래쌍의 최근 24시간 시장 요약을 조회한다.
func (client *Client) MarketDetail(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (MarketDetail, error) {
	if err := validateSymbol(symbol); err != nil {
		return MarketDetail{}, err
	}
	response, err := client.executePublic(
		ctx, "/market/detail", url.Values{"symbol": {symbol}}, "market-detail", options...,
	)
	if err != nil {
		return MarketDetail{}, err
	}
	var envelope struct {
		Channel   string          `json:"ch"`
		Timestamp int64           `json:"ts"`
		Tick      json.RawMessage `json:"tick"`
	}
	if _, err := client.decodeResponse(response, &envelope); err != nil {
		return MarketDetail{}, err
	}
	var wire struct {
		ID          Scalar  `json:"id"`
		Version     Scalar  `json:"version"`
		Open        Decimal `json:"open"`
		Close       Decimal `json:"close"`
		Low         Decimal `json:"low"`
		High        Decimal `json:"high"`
		BaseVolume  Decimal `json:"amount"`
		QuoteVolume Decimal `json:"vol"`
		Count       int64   `json:"count"`
	}
	if err := json.Unmarshal(envelope.Tick, &wire); err != nil {
		return MarketDetail{}, client.decodeBodyError(response, err)
	}
	return MarketDetail{
		Channel: envelope.Channel, ID: wire.ID, Version: wire.Version,
		Open: wire.Open, Close: wire.Close, Low: wire.Low, High: wire.High,
		BaseVolume: wire.BaseVolume, QuoteVolume: wire.QuoteVolume, Count: wire.Count,
		Timestamp: envelope.Timestamp, Raw: cloneBytes(envelope.Tick),
	}, nil
}

// MarketStatus는 HTX Spot 시장의 운영 상태와 중단 정보를 조회한다.
func (client *Client) MarketStatus(
	ctx context.Context,
	options ...trade.RequestOption,
) (MarketStatus, error) {
	response, err := client.executePublic(
		ctx, "/v2/market-status", nil, "market-status", options...,
	)
	if err != nil {
		return MarketStatus{}, err
	}
	var wire struct {
		Status          MarketStatusCode `json:"marketStatus"`
		HaltStartTime   int64            `json:"haltStartTime"`
		HaltEndTime     int64            `json:"haltEndTime"`
		HaltReason      int              `json:"haltReason"`
		AffectedSymbols string           `json:"affectedSymbols"`
	}
	raw, err := client.decodeDataForOperation(response, commonexchange.OperationRead, &wire)
	if err != nil {
		return MarketStatus{}, err
	}
	if wire.Status == 0 {
		return MarketStatus{}, client.decodeBodyError(
			response, errors.New("HTX market status is missing"),
		)
	}
	return MarketStatus{
		Status: wire.Status, HaltStartTime: wire.HaltStartTime, HaltEndTime: wire.HaltEndTime,
		HaltReason: wire.HaltReason, AffectedSymbols: wire.AffectedSymbols, Raw: raw,
	}, nil
}

// SymbolSettings는 `/v1/settings/common/symbols`로 거래쌍의 공개·거래·취소 설정을 조회한다.
func (client *Client) SymbolSettings(
	ctx context.Context,
	request SettingsRequest,
	options ...trade.RequestOption,
) (SymbolSettings, error) {
	if err := request.validate(); err != nil {
		return SymbolSettings{}, err
	}
	response, envelope, raw, err := client.executeSettings(
		ctx, "/v1/settings/common/symbols", request.values(), "symbol-settings", options...,
	)
	if err != nil {
		return SymbolSettings{}, err
	}
	value := SymbolSettings{
		Symbols:   make([]SymbolSetting, len(envelope.Data)),
		UpdatedAt: envelope.TS, Full: envelope.Full, Raw: raw,
	}
	for index, itemRaw := range envelope.Data {
		if err := json.Unmarshal(itemRaw, &value.Symbols[index]); err != nil {
			return SymbolSettings{}, client.decodeBodyError(response, err)
		}
		value.Symbols[index].Raw = cloneBytes(itemRaw)
	}
	return value, nil
}

// SymbolSettingsV2는 `/v2/settings/common/symbols`로 V2 거래쌍 설정을 조회한다.
func (client *Client) SymbolSettingsV2(
	ctx context.Context,
	request SettingsRequest,
	options ...trade.RequestOption,
) (SymbolSettingsV2, error) {
	if err := request.validate(); err != nil {
		return SymbolSettingsV2{}, err
	}
	response, envelope, raw, err := client.executeSettings(
		ctx, "/v2/settings/common/symbols", request.values(), "symbol-settings-v2", options...,
	)
	if err != nil {
		return SymbolSettingsV2{}, err
	}
	value := SymbolSettingsV2{
		Symbols:   make([]SymbolSettingV2, len(envelope.Data)),
		UpdatedAt: envelope.TS, Full: envelope.Full, Raw: raw,
	}
	for index, itemRaw := range envelope.Data {
		if err := json.Unmarshal(itemRaw, &value.Symbols[index]); err != nil {
			return SymbolSettingsV2{}, client.decodeBodyError(response, err)
		}
		value.Symbols[index].Raw = cloneBytes(itemRaw)
	}
	return value, nil
}

// CurrencySettings는 `/v1/settings/common/currencys`로 통화 설정을 조회한다.
func (client *Client) CurrencySettings(
	ctx context.Context,
	request SettingsRequest,
	options ...trade.RequestOption,
) (CurrencySettings, error) {
	if err := request.validate(); err != nil {
		return CurrencySettings{}, err
	}
	response, envelope, raw, err := client.executeSettings(
		ctx, "/v1/settings/common/currencys", request.values(), "currency-settings", options...,
	)
	if err != nil {
		return CurrencySettings{}, err
	}
	value := CurrencySettings{
		Currencies: make([]CurrencySetting, len(envelope.Data)),
		UpdatedAt:  envelope.TS, Full: envelope.Full, Raw: raw,
	}
	for index, itemRaw := range envelope.Data {
		if err := json.Unmarshal(itemRaw, &value.Currencies[index]); err != nil {
			return CurrencySettings{}, client.decodeBodyError(response, err)
		}
		value.Currencies[index].Raw = cloneBytes(itemRaw)
	}
	return value, nil
}

// CurrencySettingsV2는 `/v2/settings/common/currencies`로 V2 통화 설정을 조회한다.
func (client *Client) CurrencySettingsV2(
	ctx context.Context,
	request SettingsRequest,
	options ...trade.RequestOption,
) (CurrencySettingsV2, error) {
	if err := request.validate(); err != nil {
		return CurrencySettingsV2{}, err
	}
	response, envelope, raw, err := client.executeSettings(
		ctx, "/v2/settings/common/currencies", request.values(), "currency-settings-v2", options...,
	)
	if err != nil {
		return CurrencySettingsV2{}, err
	}
	value := CurrencySettingsV2{
		Currencies: make([]CurrencySettingV2, len(envelope.Data)),
		UpdatedAt:  envelope.TS, Full: envelope.Full, Raw: raw,
	}
	for index, itemRaw := range envelope.Data {
		if err := json.Unmarshal(itemRaw, &value.Currencies[index]); err != nil {
			return CurrencySettingsV2{}, client.decodeBodyError(response, err)
		}
		value.Currencies[index].Raw = cloneBytes(itemRaw)
	}
	return value, nil
}

// ChainSettings는 `/v1/settings/common/chains`로 통화별 체인 설정을 조회한다.
func (client *Client) ChainSettings(
	ctx context.Context,
	request ChainSettingsRequest,
	options ...trade.RequestOption,
) (ChainSettings, error) {
	if err := request.validate(); err != nil {
		return ChainSettings{}, err
	}
	response, envelope, raw, err := client.executeSettings(
		ctx, "/v1/settings/common/chains", request.values(), "chain-settings", options...,
	)
	if err != nil {
		return ChainSettings{}, err
	}
	value := ChainSettings{
		Chains:    make([]ChainSetting, len(envelope.Data)),
		UpdatedAt: envelope.TS, Full: envelope.Full, Raw: raw,
	}
	for index, itemRaw := range envelope.Data {
		if err := json.Unmarshal(itemRaw, &value.Chains[index]); err != nil {
			return ChainSettings{}, client.decodeBodyError(response, err)
		}
		value.Chains[index].Raw = cloneBytes(itemRaw)
	}
	return value, nil
}

// ReferenceCurrencies는 `/v2/reference/currencies`로 통화·체인별 입출금 참조 정보를 조회한다.
func (client *Client) ReferenceCurrencies(
	ctx context.Context,
	request ReferenceCurrenciesRequest,
	options ...trade.RequestOption,
) ([]ReferenceCurrency, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, "/v2/reference/currencies", request.values(), "reference-currencies", options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeDataForOperation(
		response, commonexchange.OperationRead, &rawItems,
	); err != nil {
		return nil, err
	}
	items := make([]ReferenceCurrency, len(rawItems))
	for index, itemRaw := range rawItems {
		var wire struct {
			Currency         string            `json:"currency"`
			InstrumentStatus string            `json:"instStatus"`
			Chains           []json.RawMessage `json:"chains"`
		}
		if err := json.Unmarshal(itemRaw, &wire); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		if wire.Currency == "" {
			return nil, client.decodeBodyError(
				response, errors.New("HTX reference currency is missing a currency"),
			)
		}
		item := ReferenceCurrency{
			Currency: wire.Currency, InstrumentStatus: wire.InstrumentStatus,
			Chains: make([]ReferenceChain, len(wire.Chains)), Raw: cloneBytes(itemRaw),
		}
		for chainIndex, chainRaw := range wire.Chains {
			if err := json.Unmarshal(chainRaw, &item.Chains[chainIndex]); err != nil {
				return nil, client.decodeBodyError(response, err)
			}
			item.Chains[chainIndex].Raw = cloneBytes(chainRaw)
		}
		items[index] = item
	}
	return items, nil
}

// executeSettings는 증분 메타데이터를 가진 공개 설정 조회를 실행하고 항목 원본을 분해한다.
func (client *Client) executeSettings(
	ctx context.Context,
	path string,
	query url.Values,
	endpoint string,
	options ...trade.RequestOption,
) (commonexchange.Response, settingsEnvelope, json.RawMessage, error) {
	response, err := client.executePublic(ctx, path, query, endpoint, options...)
	if err != nil {
		return commonexchange.Response{}, settingsEnvelope{}, nil, err
	}
	var envelope settingsEnvelope
	raw, err := client.decodeResponse(response, &envelope)
	if err != nil {
		return commonexchange.Response{}, settingsEnvelope{}, nil, err
	}
	return response, envelope, raw, nil
}

func (request SettingsRequest) validate() error {
	return validateUpdatedSince(request.UpdatedSince)
}

func (request SettingsRequest) values() url.Values {
	values := make(url.Values)
	setUpdatedSince(values, request.UpdatedSince)
	return values
}

func (mode ChainDescriptionMode) valid() bool {
	switch mode {
	case ChainDescriptionNone, ChainDescriptionAll, ChainDescriptionSuspended:
		return true
	default:
		return false
	}
}

func (request ChainSettingsRequest) validate() error {
	if request.Currency != "" && !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid HTX currency %q", request.Currency)
	}
	if request.ShowDescription != "" && !request.ShowDescription.valid() {
		return validationError("unsupported chain description mode %q", request.ShowDescription)
	}
	return validateUpdatedSince(request.UpdatedSince)
}

func (request ChainSettingsRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "currency", request.Currency)
	setIfNotEmpty(values, "show-desc", string(request.ShowDescription))
	setUpdatedSince(values, request.UpdatedSince)
	return values
}

func (request ReferenceCurrenciesRequest) validate() error {
	if request.Currency != "" && !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid HTX currency %q", request.Currency)
	}
	return nil
}

func (request ReferenceCurrenciesRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "currency", request.Currency)
	if request.AuthorizedUser != nil {
		values.Set("authorizedUser", strconv.FormatBool(*request.AuthorizedUser))
	}
	return values
}

func validateUpdatedSince(updatedSince *time.Time) error {
	if updatedSince != nil && updatedSince.UnixMilli() <= 0 {
		return validationError("settings update time must be after the Unix epoch")
	}
	return nil
}

func setUpdatedSince(values url.Values, updatedSince *time.Time) {
	if updatedSince != nil {
		values.Set("ts", strconv.FormatInt(updatedSince.UnixMilli(), 10))
	}
}
