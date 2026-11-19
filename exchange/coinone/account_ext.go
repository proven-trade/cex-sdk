package coinone

import (
	"context"
	"encoding/json"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// BalancesRequest는 지정한 자산의 잔고 조회 조건이다.
type BalancesRequest struct {
	Currencies []string
}

// TradeFeeRequest는 개별 종목의 수수료율 조회 조건이다.
type TradeFeeRequest struct {
	QuoteCurrency  string
	TargetCurrency string
}

// OrderRewardHistoryRequest는 주문 리워드 수령 내역의 조회 기간이다.
// From이 비어 있으면 시작 시점을 제한하지 않고 To가 비어 있으면 현재 시점까지 조회한다.
// 코인원은 기간 안에서 최근일부터 역순으로 최대 30일치만 반환한다.
type OrderRewardHistoryRequest struct {
	From time.Time
	To   time.Time
}

// TradeFee는 종목별 maker와 taker 수수료율이다.
type TradeFee struct {
	QuoteCurrency  string          `json:"quote_currency"`
	TargetCurrency string          `json:"target_currency"`
	Maker          Decimal         `json:"maker"`
	Taker          Decimal         `json:"taker"`
	Raw            json.RawMessage `json:"-"`
}

// OrderRewardProgram은 진행 중인 주문 리워드 종목과 내 기여도다.
// CompetitionLevel은 LOW, MIDDLE, HIGH 중 하나다.
type OrderRewardProgram struct {
	QuoteCurrency        string          `json:"quote_currency"`
	TargetCurrency       string          `json:"target_currency"`
	RewardAmount         Decimal         `json:"reward_amount"`
	ContributionRatio    Decimal         `json:"contribution_ratio"`
	ExpectedRewardAmount Decimal         `json:"expect_reward_amount"`
	Score                Decimal         `json:"score"`
	TotalScore           Decimal         `json:"total_score"`
	IsMaintenance        bool            `json:"is_maintenance"`
	CompetitionLevel     string          `json:"competition_level"`
	Raw                  json.RawMessage `json:"-"`
}

// OrderRewardDetail은 하루치 주문 리워드 중 종목별 지급 내역이다.
type OrderRewardDetail struct {
	QuoteCurrency        string  `json:"quote_currency"`
	TargetCurrency       string  `json:"target_currency"`
	DailyRewardAmount    Decimal `json:"daily_reward_amount"`
	ReceivedRewardAmount Decimal `json:"received_reward_amount"`
	Score                Decimal `json:"score"`
	TotalScore           Decimal `json:"total_score"`
	ContributionRatio    Decimal `json:"contribution_ratio"`
}

// OrderReward는 하루치 주문 리워드 수령 내역이다.
// AppliedAt은 초 단위 Unix 시각이고 DepositStatus는 PENDING 또는 COMPLETE다.
// NetRewardAmount는 세금을 제외한 실제 수령액이다.
type OrderReward struct {
	AppliedAt       int64               `json:"applied_at"`
	RewardAmount    Decimal             `json:"reward_amount"`
	NetRewardAmount Decimal             `json:"net_reward_amount"`
	DepositStatus   string              `json:"deposit_status"`
	Details         []OrderRewardDetail `json:"details"`
	Raw             json.RawMessage     `json:"-"`
}

func (request BalancesRequest) validate() error {
	if len(request.Currencies) == 0 {
		return validationError("balance currencies are required")
	}
	return validateCurrencyList("balance currency", request.Currencies)
}

func (request BalancesRequest) fields() payloadFields {
	values := payloadFields{}
	values.addStrings("currencies", request.Currencies)
	return values
}

func (request TradeFeeRequest) validate() error {
	return validatePair(request.QuoteCurrency, request.TargetCurrency)
}

func (request OrderRewardHistoryRequest) validate() error {
	if !request.From.IsZero() && request.From.Unix() <= 0 {
		return validationError("order reward history from must be after the Unix epoch")
	}
	if !request.To.IsZero() && request.To.Unix() <= 0 {
		return validationError("order reward history to must be after the Unix epoch")
	}
	if !request.From.IsZero() && !request.To.IsZero() && request.From.After(request.To) {
		return validationError("order reward history from cannot be after to")
	}
	return nil
}

func (request OrderRewardHistoryRequest) fields() payloadFields {
	values := payloadFields{}
	if !request.From.IsZero() {
		values.addInt64("from_ts", request.From.Unix())
	}
	if !request.To.IsZero() {
		values.addInt64("to_ts", request.To.Unix())
	}
	return values
}

// Balances는 지정한 자산의 잔고만 조회한다. Read 권한이 필요하다.
func (client *Client) Balances(
	ctx context.Context,
	request BalancesRequest,
	options ...trade.RequestOption,
) ([]Balance, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, "/v2.1/account/balance", request.fields(), false,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Balances []json.RawMessage `json:"balances"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(client, response, envelope.Balances, func(item *Balance, raw []byte) {
		item.Raw = raw
	})
}

// TradeFees는 거래 가능한 모든 종목의 maker와 taker 수수료율을 조회한다. Read 권한이 필요하다.
func (client *Client) TradeFees(ctx context.Context, options ...trade.RequestOption) ([]TradeFee, error) {
	items, _, err := client.fetchTradeFees(ctx, "/v2.1/account/trade_fee", options...)
	return items, err
}

// TradeFee는 개별 종목의 maker와 taker 수수료율을 조회한다. Read 권한이 필요하다.
func (client *Client) TradeFee(
	ctx context.Context,
	request TradeFeeRequest,
	options ...trade.RequestOption,
) (TradeFee, error) {
	if err := request.validate(); err != nil {
		return TradeFee{}, err
	}
	path := pairPath("/v2.1/account/trade_fee", request.QuoteCurrency, request.TargetCurrency)
	items, response, err := client.fetchTradeFees(ctx, path, options...)
	if err != nil {
		return TradeFee{}, err
	}
	return singleItem(client, response, "trade fee", items)
}

// OrderRewardPrograms는 진행 중인 주문 리워드 프로그램과 내 점수를 조회한다. Read 권한이 필요하다.
func (client *Client) OrderRewardPrograms(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]OrderRewardProgram, error) {
	response, err := client.executePrivate(
		ctx, "/v2.1/event/order-reward/programs", nil, false,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Programs []json.RawMessage `json:"programs"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(client, response, envelope.Programs, func(item *OrderRewardProgram, raw []byte) {
		item.Raw = raw
	})
}

// OrderRewardHistory는 수령한 주문 리워드 내역을 조회한다. Read 권한이 필요하다.
func (client *Client) OrderRewardHistory(
	ctx context.Context,
	request OrderRewardHistoryRequest,
	options ...trade.RequestOption,
) ([]OrderReward, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, "/v2.1/event/order-reward/history", request.fields(), false,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Rewards []json.RawMessage `json:"rewards"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(client, response, envelope.Rewards, func(item *OrderReward, raw []byte) {
		item.Raw = raw
	})
}

func (client *Client) fetchTradeFees(
	ctx context.Context,
	path string,
	options ...trade.RequestOption,
) ([]TradeFee, commonexchange.Response, error) {
	response, err := client.executePrivate(
		ctx, path, nil, false,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, commonexchange.Response{}, err
	}
	var envelope struct {
		FeeRates []json.RawMessage `json:"fee_rates"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, commonexchange.Response{}, err
	}
	items, err := decodeRawItems(client, response, envelope.FeeRates, func(item *TradeFee, raw []byte) {
		item.Raw = raw
	})
	return items, response, err
}

func validateCurrencyList(name string, values []string) error {
	for _, value := range values {
		if err := validateCurrency(name, value); err != nil {
			return err
		}
	}
	return nil
}
