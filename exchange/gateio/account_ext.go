package gateio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// SpotCurrencyChain은 Spot 통화에 연결된 블록체인별 입출금 상태다.
type SpotCurrencyChain struct {
	Name             string `json:"name"`
	Address          string `json:"addr"`
	WithdrawDisabled bool   `json:"withdraw_disabled"`
	WithdrawDelayed  bool   `json:"withdraw_delayed"`
	DepositDisabled  bool   `json:"deposit_disabled"`
}

// Currency는 Gate.io 통화 정보와 체인별 입출금 상태다.
type Currency struct {
	Currency         string              `json:"currency"`
	Name             string              `json:"name"`
	Delisted         bool                `json:"delisted"`
	WithdrawDisabled bool                `json:"withdraw_disabled"`
	WithdrawDelayed  bool                `json:"withdraw_delayed"`
	DepositDisabled  bool                `json:"deposit_disabled"`
	TradeDisabled    bool                `json:"trade_disabled"`
	FixedRate        string              `json:"fixed_rate"`
	Chain            string              `json:"chain"`
	Chains           []SpotCurrencyChain `json:"chains"`
	TotalSupply      string              `json:"total_supply"`
	MarketCap        string              `json:"market_cap"`
	Category         []string            `json:"category"`
	Raw              json.RawMessage     `json:"-"`
}

func (currency *Currency) setRaw(raw json.RawMessage) { currency.Raw = raw }

// Currencies는 Gate.io의 모든 통화 정보를 조회한다. 공개 API이며 자격증명이 필요 없다.
// GET /spot/currencies
func (client *Client) Currencies(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]Currency, error) {
	response, err := client.executePublic(
		ctx, http.MethodGet, "/spot/currencies", nil,
		publicLimit("currencies"), options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[Currency](client, response, commonexchange.OperationRead)
}

// CurrencyInfo는 단일 통화 정보를 조회한다. 공개 API이며 자격증명이 필요 없다.
// GET /spot/currencies/{currency}
func (client *Client) CurrencyInfo(
	ctx context.Context,
	currency string,
	options ...trade.RequestOption,
) (Currency, error) {
	if !currencyPattern.MatchString(currency) {
		return Currency{}, validationError("invalid currency %q", currency)
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/spot/currencies/"+url.PathEscape(currency), nil,
		publicLimit("currency"), options...,
	)
	if err != nil {
		return Currency{}, err
	}
	return decodeObject[Currency](client, response, commonexchange.OperationRead)
}

// CurrencyChain은 통화별 지원 체인과 입출금 비활성 여부다. 값이 0이면 활성이다.
type CurrencyChain struct {
	Chain              string          `json:"chain"`
	NameCN             string          `json:"name_cn"`
	NameEN             string          `json:"name_en"`
	ContractAddress    string          `json:"contract_address"`
	IsDisabled         int             `json:"is_disabled"`
	IsDepositDisabled  int             `json:"is_deposit_disabled"`
	IsWithdrawDisabled int             `json:"is_withdraw_disabled"`
	Decimal            string          `json:"decimal"`
	IsTag              int             `json:"is_tag"`
	Raw                json.RawMessage `json:"-"`
}

func (chain *CurrencyChain) setRaw(raw json.RawMessage) { chain.Raw = raw }

// CurrencyChains는 통화별 지원 체인을 조회한다. 공개 API이며 자격증명이 필요 없다.
// GET /wallet/currency_chains
func (client *Client) CurrencyChains(
	ctx context.Context,
	currency string,
	options ...trade.RequestOption,
) ([]CurrencyChain, error) {
	if !currencyPattern.MatchString(currency) {
		return nil, validationError("invalid currency %q", currency)
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/wallet/currency_chains",
		url.Values{"currency": {currency}}, publicLimit("currency-chains"), options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[CurrencyChain](client, response, commonexchange.OperationRead)
}

// SpotFee는 Spot 수수료율이다. 값은 문자열 소수로 보존한다.
type SpotFee struct {
	UserID       int64           `json:"user_id"`
	TakerFee     string          `json:"taker_fee"`
	MakerFee     string          `json:"maker_fee"`
	RPIMakerFee  string          `json:"rpi_maker_fee"`
	GTDiscount   bool            `json:"gt_discount"`
	GTTakerFee   string          `json:"gt_taker_fee"`
	GTMakerFee   string          `json:"gt_maker_fee"`
	LoanFee      string          `json:"loan_fee"`
	PointType    string          `json:"point_type"`
	CurrencyPair string          `json:"currency_pair"`
	DebitFee     int             `json:"debit_fee"`
	RPIMM        int             `json:"rpi_mm"`
	Raw          json.RawMessage `json:"-"`
}

func (fee *SpotFee) setRaw(raw json.RawMessage) { fee.Raw = raw }

// SpotFeeRequest는 Spot 수수료율 조회 조건이다. CurrencyPair를 비우면 계정 기본 수수료를 조회한다.
type SpotFeeRequest struct{ CurrencyPair string }

func (request SpotFeeRequest) validate() error {
	if request.CurrencyPair != "" {
		return validateCurrencyPair(request.CurrencyPair)
	}
	return nil
}

// SpotFee는 계정의 Spot 수수료율을 조회한다. 권한은 Read다.
// GET /spot/fee
func (client *Client) SpotFee(
	ctx context.Context,
	request SpotFeeRequest,
	options ...trade.RequestOption,
) (SpotFee, error) {
	if err := request.validate(); err != nil {
		return SpotFee{}, err
	}
	query := make(url.Values)
	if request.CurrencyPair != "" {
		query.Set("currency_pair", request.CurrencyPair)
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/spot/fee", query, nil,
		privateLimit("spot-fee"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return SpotFee{}, err
	}
	return decodeObject[SpotFee](client, response, commonexchange.OperationRead)
}

// BatchSpotFeeRequest는 거래쌍별 Spot 수수료율 일괄 조회 조건이다. 거래쌍은 최대 50개다.
type BatchSpotFeeRequest struct{ CurrencyPairs []string }

func (request BatchSpotFeeRequest) validate() error {
	if len(request.CurrencyPairs) == 0 || len(request.CurrencyPairs) > 50 {
		return validationError("batch fee requires 1 to 50 currency pairs")
	}
	for _, pair := range request.CurrencyPairs {
		if err := validateCurrencyPair(pair); err != nil {
			return err
		}
	}
	return nil
}

// BatchSpotFee는 거래쌍별 Spot 수수료율을 일괄 조회한다. 키는 거래쌍이다. 권한은 Read다.
// GET /spot/batch_fee
func (client *Client) BatchSpotFee(
	ctx context.Context,
	request BatchSpotFeeRequest,
	options ...trade.RequestOption,
) (map[string]SpotFee, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/spot/batch_fee",
		url.Values{"currency_pairs": {strings.Join(request.CurrencyPairs, ",")}}, nil,
		privateLimit("batch-fee"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems map[string]json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make(map[string]SpotFee, len(rawItems))
	for pair, raw := range rawItems {
		var fee SpotFee
		if err := json.Unmarshal(raw, &fee); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		fee.Raw = cloneBytes(raw)
		items[pair] = fee
	}
	return items, nil
}

// AccountBookEntry는 Spot 계정 원장 한 건이다. Time은 밀리초다.
type AccountBookEntry struct {
	ID       string          `json:"id"`
	Time     int64           `json:"time"`
	Currency string          `json:"currency"`
	Change   string          `json:"change"`
	Balance  string          `json:"balance"`
	Type     string          `json:"type"`
	Code     string          `json:"code"`
	Text     string          `json:"text"`
	Raw      json.RawMessage `json:"-"`
}

func (entry *AccountBookEntry) setRaw(raw json.RawMessage) { entry.Raw = raw }

// AccountBookRequest는 Spot 계정 원장 조회 조건이다. 조회 구간은 30일을 넘을 수 없다.
type AccountBookRequest struct {
	Currency string
	From     *time.Time
	To       *time.Time
	Page     int
	Limit    int
	Type     string
	Code     string
}

func (request AccountBookRequest) validate() error {
	if err := validateOptionalCurrency(request.Currency); err != nil {
		return err
	}
	if err := validateTimeRange(request.From, request.To); err != nil {
		return err
	}
	if request.From != nil && request.To != nil && request.To.Sub(*request.From) > 30*24*time.Hour {
		return validationError("account book range cannot exceed 30 days")
	}
	return validatePage(request.Page, request.Limit)
}

func (request AccountBookRequest) values() url.Values {
	values := make(url.Values)
	if request.Currency != "" {
		values.Set("currency", request.Currency)
	}
	setUnixTime(values, "from", request.From)
	setUnixTime(values, "to", request.To)
	setPositiveInt(values, "page", request.Page)
	setPositiveInt(values, "limit", request.Limit)
	if request.Type != "" {
		values.Set("type", request.Type)
	}
	if request.Code != "" {
		values.Set("code", request.Code)
	}
	return values
}

// AccountBook은 Spot 계정 원장(잔고 변동) 이력을 조회한다. 권한은 Read다.
// GET /spot/account_book
func (client *Client) AccountBook(
	ctx context.Context,
	request AccountBookRequest,
	options ...trade.RequestOption,
) ([]AccountBookEntry, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/spot/account_book", request.values(), nil,
		privateLimit("account-book"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[AccountBookEntry](client, response, commonexchange.OperationRead)
}

// TradeFee는 Spot·선물·인도형 계약을 포함한 개인 거래 수수료율이다.
type TradeFee struct {
	UserID             int64           `json:"user_id"`
	TakerFee           string          `json:"taker_fee"`
	MakerFee           string          `json:"maker_fee"`
	RPIMakerFee        string          `json:"rpi_maker_fee"`
	GTDiscount         bool            `json:"gt_discount"`
	GTTakerFee         string          `json:"gt_taker_fee"`
	GTMakerFee         string          `json:"gt_maker_fee"`
	LoanFee            string          `json:"loan_fee"`
	PointType          string          `json:"point_type"`
	FuturesTakerFee    string          `json:"futures_taker_fee"`
	FuturesMakerFee    string          `json:"futures_maker_fee"`
	FuturesRPIMakerFee string          `json:"futures_rpi_maker_fee"`
	DeliveryTakerFee   string          `json:"delivery_taker_fee"`
	DeliveryMakerFee   string          `json:"delivery_maker_fee"`
	DebitFee           int             `json:"debit_fee"`
	RPIMM              int             `json:"rpi_mm"`
	Raw                json.RawMessage `json:"-"`
}

func (fee *TradeFee) setRaw(raw json.RawMessage) { fee.Raw = raw }

// TradeFeeRequest는 개인 거래 수수료율 조회 조건이다.
type TradeFeeRequest struct {
	CurrencyPair string
	Settle       string
}

func (request TradeFeeRequest) validate() error {
	if request.CurrencyPair != "" {
		if err := validateCurrencyPair(request.CurrencyPair); err != nil {
			return err
		}
	}
	return validateOptionalCurrency(request.Settle)
}

// WalletTradeFee는 개인 거래 수수료율을 조회한다. 권한은 Read다.
// GET /wallet/fee
func (client *Client) WalletTradeFee(
	ctx context.Context,
	request TradeFeeRequest,
	options ...trade.RequestOption,
) (TradeFee, error) {
	if err := request.validate(); err != nil {
		return TradeFee{}, err
	}
	query := make(url.Values)
	if request.CurrencyPair != "" {
		query.Set("currency_pair", request.CurrencyPair)
	}
	if request.Settle != "" {
		query.Set("settle", strings.ToLower(request.Settle))
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/wallet/fee", query, nil,
		privateLimit("wallet-fee"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return TradeFee{}, err
	}
	return decodeObject[TradeFee](client, response, commonexchange.OperationRead)
}

// AccountBalance는 계정 종류별 환산 잔고다.
type AccountBalance struct {
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	UnrealisedPnl string `json:"unrealised_pnl"`
	Borrowed      string `json:"borrowed"`
}

// TotalBalance는 모든 계정의 환산 합계 잔고다.
type TotalBalance struct {
	Total   AccountBalance            `json:"total"`
	Details map[string]AccountBalance `json:"details"`
	Raw     json.RawMessage           `json:"-"`
}

func (balance *TotalBalance) setRaw(raw json.RawMessage) { balance.Raw = raw }

// TotalBalance는 모든 계정의 잔고를 지정 통화(BTC, CNY, USD, USDT)로 환산해 조회한다.
// Currency를 비우면 USDT다. 권한은 Read다.
// GET /wallet/total_balance
func (client *Client) TotalBalance(
	ctx context.Context,
	currency string,
	options ...trade.RequestOption,
) (TotalBalance, error) {
	if err := validateOptionalCurrency(currency); err != nil {
		return TotalBalance{}, err
	}
	query := make(url.Values)
	if currency != "" {
		query.Set("currency", currency)
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/wallet/total_balance", query, nil,
		privateLimit("total-balance"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return TotalBalance{}, err
	}
	return decodeObject[TotalBalance](client, response, commonexchange.OperationRead)
}

// SmallBalance는 소액 전환 가능한 잔고다.
type SmallBalance struct {
	Currency         string          `json:"currency"`
	AvailableBalance string          `json:"available_balance"`
	EstimatedAsBTC   string          `json:"estimated_as_btc"`
	ConvertibleToGT  string          `json:"convertible_to_gt"`
	Raw              json.RawMessage `json:"-"`
}

func (balance *SmallBalance) setRaw(raw json.RawMessage) { balance.Raw = raw }

// SmallBalances는 GT로 전환 가능한 소액 잔고를 조회한다. 권한은 Read다.
// GET /wallet/small_balance
func (client *Client) SmallBalances(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]SmallBalance, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/wallet/small_balance", nil, nil,
		privateLimit("small-balance"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[SmallBalance](client, response, commonexchange.OperationRead)
}

// ConvertSmallBalanceRequest는 소액 잔고의 GT 전환 조건이다.
// All이 true이면 전체를 전환하고 아니면 Currencies가 필요하다.
type ConvertSmallBalanceRequest struct {
	Currencies []string `json:"currency,omitempty"`
	All        bool     `json:"is_all,omitempty"`
}

func (request ConvertSmallBalanceRequest) validate() error {
	if request.All == (len(request.Currencies) > 0) {
		return validationError("specify either all or currencies for small balance conversion")
	}
	for _, currency := range request.Currencies {
		if !currencyPattern.MatchString(currency) {
			return validationError("invalid currency %q", currency)
		}
	}
	return nil
}

// ConvertSmallBalance는 소액 잔고를 GT로 전환한다. 권한은 Transfer이며 자동 재시도하지 않는다.
// 성공 응답은 본문이 없다.
// POST /wallet/small_balance
func (client *Client) ConvertSmallBalance(
	ctx context.Context,
	request ConvertSmallBalanceRequest,
	options ...trade.RequestOption,
) error {
	if err := request.validate(); err != nil {
		return err
	}
	body, err := encodeBody(request)
	if err != nil {
		return err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/wallet/small_balance", nil, body,
		privateLimit("convert-small-balance"), credential.PermissionTransfer,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return err
	}
	return client.decodeEmpty(response, commonexchange.OperationMutation)
}

// SmallBalanceHistory는 소액 전환 이력 한 건이다. CreateTime은 초다.
type SmallBalanceHistory struct {
	ID         string          `json:"id"`
	Currency   string          `json:"currency"`
	Amount     string          `json:"amount"`
	GTAmount   string          `json:"gt_amount"`
	CreateTime int64           `json:"create_time"`
	Raw        json.RawMessage `json:"-"`
}

func (history *SmallBalanceHistory) setRaw(raw json.RawMessage) { history.Raw = raw }

// SmallBalanceHistoryRequest는 소액 전환 이력 조회 조건이다. Limit은 최대 100이다.
type SmallBalanceHistoryRequest struct {
	Currency string
	Page     int
	Limit    int
}

func (request SmallBalanceHistoryRequest) validate() error {
	if err := validateOptionalCurrency(request.Currency); err != nil {
		return err
	}
	if request.Page < 0 {
		return validationError("page cannot be negative")
	}
	if request.Limit < 0 || request.Limit > 100 {
		return validationError("limit must be between 1 and 100 or zero")
	}
	return nil
}

// SmallBalanceHistory는 소액 잔고 전환 이력을 조회한다. 권한은 Read다.
// GET /wallet/small_balance_history
func (client *Client) SmallBalanceHistory(
	ctx context.Context,
	request SmallBalanceHistoryRequest,
	options ...trade.RequestOption,
) ([]SmallBalanceHistory, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	query := make(url.Values)
	if request.Currency != "" {
		query.Set("currency", request.Currency)
	}
	setPositiveInt(query, "page", request.Page)
	setPositiveInt(query, "limit", request.Limit)
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/wallet/small_balance_history", query, nil,
		privateLimit("small-balance-history"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[SmallBalanceHistory](client, response, commonexchange.OperationRead)
}
