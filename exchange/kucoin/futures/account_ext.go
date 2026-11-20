package futures

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// LedgerType은 Futures 계정 원장 항목 종류 필터다.
type LedgerType string

const (
	LedgerTypeRealisedPNL LedgerType = "RealizedPNL"
	LedgerTypeDeposit     LedgerType = "Deposit"
	LedgerTypeWithdrawal  LedgerType = "Withdrawal"
	LedgerTypeTransferIn  LedgerType = "Transferin"
	LedgerTypeTransferOut LedgerType = "TransferOut"
)

// LedgerRequest는 Futures 계정 원장 조회 조건이다.
// Forward를 nil로 두면 거래소 기본값(true)을 사용하고, MaxCount는 기본 50이다.
type LedgerRequest struct {
	Currency string
	Type     LedgerType
	Offset   int64
	Forward  *bool
	MaxCount int
	StartAt  *time.Time
	EndAt    *time.Time
}

// LedgerEntry는 Futures 계정 원장 한 건이다.
type LedgerEntry struct {
	Time          int64           `json:"time"`
	Type          string          `json:"type"`
	Amount        Decimal         `json:"amount"`
	Fee           Decimal         `json:"fee"`
	AccountEquity Decimal         `json:"accountEquity"`
	Status        string          `json:"status"`
	Remark        string          `json:"remark"`
	Offset        int64           `json:"offset"`
	Currency      string          `json:"currency"`
	Raw           json.RawMessage `json:"-"`
}

// LedgerPage는 offset 기반 Futures 계정 원장 한 페이지다. HasMore가 true이면 다음 페이지가 있다.
type LedgerPage struct {
	Entries []LedgerEntry
	HasMore bool
	Raw     json.RawMessage
}

// ActualFee는 계약별 실제 적용 수수료율이다.
type ActualFee struct {
	Symbol       string          `json:"symbol"`
	TakerFeeRate Decimal         `json:"takerFeeRate"`
	MakerFeeRate Decimal         `json:"makerFeeRate"`
	Raw          json.RawMessage `json:"-"`
}

// FundingHistoryRequest는 내 펀딩비 정산 내역 조회 조건이다.
// Reverse와 Forward를 nil로 두면 거래소 기본값을 사용하고, MaxCount는 1~100이며 0이면 기본값을 사용한다.
type FundingHistoryRequest struct {
	Symbol   string
	StartAt  *time.Time
	EndAt    *time.Time
	Reverse  *bool
	Offset   int64
	Forward  *bool
	MaxCount int
}

// FundingHistoryEntry는 펀딩비 정산 한 건이다. Funding이 음수이면 지불한 금액이다.
type FundingHistoryEntry struct {
	ID             int64           `json:"id"`
	Symbol         string          `json:"symbol"`
	TimePoint      int64           `json:"timePoint"`
	FundingRate    Decimal         `json:"fundingRate"`
	MarkPrice      Decimal         `json:"markPrice"`
	PositionQty    int64           `json:"positionQty"`
	PositionCost   Decimal         `json:"positionCost"`
	Funding        Decimal         `json:"funding"`
	SettleCurrency string          `json:"settleCurrency"`
	Context        string          `json:"context"`
	MarginMode     MarginMode      `json:"marginMode"`
	Raw            json.RawMessage `json:"-"`
}

// FundingHistoryPage는 offset 기반 펀딩비 정산 내역 한 페이지다.
type FundingHistoryPage struct {
	Entries []FundingHistoryEntry
	HasMore bool
	Raw     json.RawMessage
}

// SubAccountOverviewRequest는 서브계정 Futures 잔고 목록 조회 조건이다. Currency를 생략하면 거래소 기본 통화(XBT)를 사용한다.
type SubAccountOverviewRequest struct {
	Currency string
}

// SubAccountOverviewSummary는 모든 서브계정을 합산한 Futures 자산 요약이다.
type SubAccountOverviewSummary struct {
	AccountEquityTotal    Decimal `json:"accountEquityTotal"`
	UnrealisedPNLTotal    Decimal `json:"unrealisedPNLTotal"`
	MarginBalanceTotal    Decimal `json:"marginBalanceTotal"`
	PositionMarginTotal   Decimal `json:"positionMarginTotal"`
	OrderMarginTotal      Decimal `json:"orderMarginTotal"`
	FrozenFundsTotal      Decimal `json:"frozenFundsTotal"`
	AvailableBalanceTotal Decimal `json:"availableBalanceTotal"`
	Currency              string  `json:"currency"`
}

// SubAccountOverview는 서브계정 하나의 Futures 자산과 증거금 요약이다.
type SubAccountOverview struct {
	AccountName      string  `json:"accountName"`
	AccountEquity    Decimal `json:"accountEquity"`
	UnrealisedPNL    Decimal `json:"unrealisedPNL"`
	MarginBalance    Decimal `json:"marginBalance"`
	PositionMargin   Decimal `json:"positionMargin"`
	OrderMargin      Decimal `json:"orderMargin"`
	FrozenFunds      Decimal `json:"frozenFunds"`
	AvailableBalance Decimal `json:"availableBalance"`
	Currency         string  `json:"currency"`
}

// SubAccountOverviews는 서브계정별 Futures 잔고 목록과 합계다.
type SubAccountOverviews struct {
	Summary  SubAccountOverviewSummary `json:"summary"`
	Accounts []SubAccountOverview      `json:"accounts"`
	Raw      json.RawMessage           `json:"-"`
}

// SubAccountOverviews는 모든 서브계정의 Futures 잔고 목록을 조회한다. 마스터 계정 API Key가 필요하다.
func (client *Client) SubAccountOverviews(
	ctx context.Context,
	request SubAccountOverviewRequest,
	options ...trade.RequestOption,
) (SubAccountOverviews, error) {
	values := make(url.Values)
	if request.Currency != "" {
		if !currencyPattern.MatchString(request.Currency) {
			return SubAccountOverviews{}, validationError("invalid Futures sub-account currency %q", request.Currency)
		}
		values.Set("currency", request.Currency)
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/account-overview-all", values, nil,
		futuresLimit(6), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return SubAccountOverviews{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead,
		func(value *SubAccountOverviews, raw json.RawMessage) { value.Raw = raw })
}

// Ledgers는 Futures 계정의 입출금·이체·실현손익 원장을 조회한다.
func (client *Client) Ledgers(
	ctx context.Context,
	request LedgerRequest,
	options ...trade.RequestOption,
) (LedgerPage, error) {
	if err := request.validate(); err != nil {
		return LedgerPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/transaction-history", request.values(), nil,
		futuresLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return LedgerPage{}, err
	}
	var rawPage struct {
		DataList []json.RawMessage `json:"dataList"`
		HasMore  bool              `json:"hasMore"`
	}
	data, err := client.decodeData(response, commonexchange.OperationRead, &rawPage)
	if err != nil {
		return LedgerPage{}, err
	}
	entries, err := decodeRawItems(client, response, commonexchange.OperationRead, rawPage.DataList,
		func(value *LedgerEntry, raw json.RawMessage) { value.Raw = raw })
	if err != nil {
		return LedgerPage{}, err
	}
	return LedgerPage{Entries: entries, HasMore: rawPage.HasMore, Raw: cloneBytes(data)}, nil
}

// ActualFee는 계약 하나에 실제 적용되는 maker·taker 수수료율을 조회한다.
func (client *Client) ActualFee(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (ActualFee, error) {
	if err := validateSymbol(symbol); err != nil {
		return ActualFee{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/trade-fees", url.Values{"symbol": {symbol}}, nil,
		futuresLimit(3), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return ActualFee{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead,
		func(value *ActualFee, raw json.RawMessage) { value.Raw = raw })
}

// FundingHistory는 계약 하나에 대한 내 펀딩비 정산 내역을 조회한다.
func (client *Client) FundingHistory(
	ctx context.Context,
	request FundingHistoryRequest,
	options ...trade.RequestOption,
) (FundingHistoryPage, error) {
	if err := request.validate(); err != nil {
		return FundingHistoryPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/funding-history", request.values(), nil,
		futuresLimit(5), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return FundingHistoryPage{}, err
	}
	var rawPage struct {
		DataList []json.RawMessage `json:"dataList"`
		HasMore  bool              `json:"hasMore"`
	}
	data, err := client.decodeData(response, commonexchange.OperationRead, &rawPage)
	if err != nil {
		return FundingHistoryPage{}, err
	}
	entries, err := decodeRawItems(client, response, commonexchange.OperationRead, rawPage.DataList,
		func(value *FundingHistoryEntry, raw json.RawMessage) { value.Raw = raw })
	if err != nil {
		return FundingHistoryPage{}, err
	}
	return FundingHistoryPage{Entries: entries, HasMore: rawPage.HasMore, Raw: cloneBytes(data)}, nil
}

func (request LedgerRequest) validate() error {
	if request.Currency != "" && !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid Futures ledger currency %q", request.Currency)
	}
	switch request.Type {
	case "", LedgerTypeRealisedPNL, LedgerTypeDeposit, LedgerTypeWithdrawal,
		LedgerTypeTransferIn, LedgerTypeTransferOut:
	default:
		return validationError("unsupported ledger type %q", request.Type)
	}
	if request.Offset < 0 {
		return validationError("ledger offset cannot be negative")
	}
	if request.MaxCount < 0 || request.MaxCount > 100 {
		return validationError("ledger max count must be between 1 and 100 or zero for default")
	}
	return validateTimeRange("ledger", request.StartAt, request.EndAt)
}

func (request LedgerRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "currency", request.Currency)
	setIfNotEmpty(values, "type", string(request.Type))
	setPositiveInt64(values, "offset", request.Offset)
	setBool(values, "forward", request.Forward)
	setPositiveInt(values, "maxCount", request.MaxCount)
	setMillis(values, "startAt", request.StartAt)
	setMillis(values, "endAt", request.EndAt)
	return values
}

func (request FundingHistoryRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Offset < 0 {
		return validationError("funding history offset cannot be negative")
	}
	if request.MaxCount < 0 || request.MaxCount > 100 {
		return validationError("funding history max count must be between 1 and 100 or zero for default")
	}
	return validateTimeRange("funding history", request.StartAt, request.EndAt)
}

func (request FundingHistoryRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setMillis(values, "startAt", request.StartAt)
	setMillis(values, "endAt", request.EndAt)
	setBool(values, "reverse", request.Reverse)
	setPositiveInt64(values, "offset", request.Offset)
	setBool(values, "forward", request.Forward)
	setPositiveInt(values, "maxCount", request.MaxCount)
	return values
}
