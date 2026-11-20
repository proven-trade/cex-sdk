package bybit

import (
	"context"
	"encoding/json"
	"math/big"
	"net/url"
	"regexp"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	maxMovePositionLegs = 25
	// positionHistoryWindow는 체결·청산 손익·포지션 이전 이력 조회의 최대 시간 범위다.
	positionHistoryWindow = 7 * 24 * time.Hour
)

var (
	// marginAmountPattern은 소수점 이하 4자리까지 허용하는 부호 있는 증거금 금액이다.
	marginAmountPattern = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]{1,4})?$`)
	positionUIDPattern  = regexp.MustCompile(`^[0-9]+$`)
)

// PositionMode는 Linear 포지션 모드다.
type PositionMode int

const (
	// PositionModeOneWay는 단방향(Merged Single) 모드다.
	PositionModeOneWay PositionMode = 0
	// PositionModeHedge는 양방향(Both Sides) 헤지 모드다.
	PositionModeHedge PositionMode = 3
)

// SetLeverageRequest는 Linear 심볼의 레버리지 설정 요청이다.
// 단방향 모드에서는 BuyLeverage와 SellLeverage가 같아야 하며, 값이 현재와 같으면 Bybit가 오류를 반환한다.
type SetLeverageRequest struct {
	Category     Category `json:"category"`
	Symbol       string   `json:"symbol"`
	BuyLeverage  string   `json:"buyLeverage"`
	SellLeverage string   `json:"sellLeverage"`
}

// SwitchPositionModeRequest는 Linear 포지션 모드 전환 요청이다.
// Symbol이 Coin보다 우선하며, Coin(정산 코인)으로 요청하면 포지션과 미체결 주문이 없는 심볼이 일괄 전환된다.
type SwitchPositionModeRequest struct {
	Category Category     `json:"category"`
	Symbol   string       `json:"symbol,omitempty"`
	Coin     string       `json:"coin,omitempty"`
	Mode     PositionMode `json:"mode"`
}

// TradingStopRequest는 Linear 포지션의 TP/SL과 트레일링 스톱 설정 요청이다.
// TakeProfit, StopLoss, TrailingStop을 "0"으로 보내면 해당 설정을 취소한다.
// Partial 모드는 새 부분 TP/SL만 추가할 수 있으며 TPSize와 SLSize는 같아야 한다.
type TradingStopRequest struct {
	Category      Category  `json:"category"`
	Symbol        string    `json:"symbol"`
	TPSLMode      TPSLMode  `json:"tpslMode"`
	PositionIndex int       `json:"positionIdx"`
	TakeProfit    string    `json:"takeProfit,omitempty"`
	StopLoss      string    `json:"stopLoss,omitempty"`
	TrailingStop  string    `json:"trailingStop,omitempty"`
	TPTriggerBy   TriggerBy `json:"tpTriggerBy,omitempty"`
	SLTriggerBy   TriggerBy `json:"slTriggerBy,omitempty"`
	ActivePrice   string    `json:"activePrice,omitempty"`
	TPSize        string    `json:"tpSize,omitempty"`
	SLSize        string    `json:"slSize,omitempty"`
	TPLimitPrice  string    `json:"tpLimitPrice,omitempty"`
	SLLimitPrice  string    `json:"slLimitPrice,omitempty"`
	TPOrderType   OrderType `json:"tpOrderType,omitempty"`
	SLOrderType   OrderType `json:"slOrderType,omitempty"`
}

// SetAutoAddMarginRequest는 격리 마진 포지션의 자동 증거금 추가 설정 요청이다.
// 헤지 모드 포지션은 PositionIndex(1 또는 2)가 필수다.
type SetAutoAddMarginRequest struct {
	Category      Category
	Symbol        string
	Enabled       bool
	PositionIndex int
}

// AddMarginRequest는 격리 마진 포지션의 증거금 수동 조정 요청이다.
// Margin이 양수면 추가, 음수면 감소이며 소수점 이하 4자리까지 허용한다.
// 헤지 모드 포지션은 PositionIndex(1 또는 2)가 필수다.
type AddMarginRequest struct {
	Category      Category `json:"category"`
	Symbol        string   `json:"symbol"`
	Margin        string   `json:"margin"`
	PositionIndex int      `json:"positionIdx,omitempty"`
}

// AddMarginResult는 증거금 조정 후 포지션 상태다.
type AddMarginResult struct {
	Category              Category        `json:"category"`
	Symbol                string          `json:"symbol"`
	PositionIndex         int             `json:"positionIdx"`
	RiskID                int             `json:"riskId"`
	RiskLimitValue        string          `json:"riskLimitValue"`
	Size                  string          `json:"size"`
	AveragePrice          string          `json:"avgPrice"`
	LiquidationPrice      string          `json:"liqPrice"`
	BustPrice             string          `json:"bustPrice"`
	MarkPrice             string          `json:"markPrice"`
	PositionValue         string          `json:"positionValue"`
	Leverage              string          `json:"leverage"`
	AutoAddMargin         int             `json:"autoAddMargin"`
	PositionStatus        string          `json:"positionStatus"`
	PositionIM            string          `json:"positionIM"`
	PositionMM            string          `json:"positionMM"`
	TakeProfit            string          `json:"takeProfit"`
	StopLoss              string          `json:"stopLoss"`
	TrailingStop          string          `json:"trailingStop"`
	UnrealisedPnL         string          `json:"unrealisedPnl"`
	CumulativeRealisedPnL string          `json:"cumRealisedPnl"`
	CreatedTime           string          `json:"createdTime"`
	UpdatedTime           string          `json:"updatedTime"`
	Raw                   json.RawMessage `json:"-"`
}

// ConfirmPendingMMRRequest는 reduce-only로 표시된 Linear 포지션의 새 리스크 한도 확인 요청이다.
type ConfirmPendingMMRRequest struct {
	Category Category `json:"category"`
	Symbol   string   `json:"symbol"`
}

// SymbolLeverageRequest는 Linear 심볼별 격리 레버리지 설정 조회 조건이다.
type SymbolLeverageRequest struct {
	Category Category
	Symbol   string
}

// SymbolLeverage는 포지션이 없어도 조회되는 심볼별 레버리지 설정이다.
type SymbolLeverage struct {
	Symbol        string          `json:"symbol"`
	Leverage      string          `json:"leverage"`
	Side          string          `json:"side"`
	PositionIndex int             `json:"positionIdx"`
	Raw           json.RawMessage `json:"-"`
}

// ClosedPnLRequest는 Linear 청산 손익 조회 조건이다.
// 시간 범위를 지정하지 않으면 최근 7일이며, 지정하면 범위는 7일을 넘을 수 없다.
type ClosedPnLRequest struct {
	Category  Category
	Symbol    string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Cursor    string
}

// ClosedPnLRecord는 청산된 포지션 손익 한 건이다.
type ClosedPnLRecord struct {
	Symbol               string          `json:"symbol"`
	OrderID              string          `json:"orderId"`
	Side                 Side            `json:"side"`
	Quantity             string          `json:"qty"`
	OrderPrice           string          `json:"orderPrice"`
	OrderType            OrderType       `json:"orderType"`
	ExecutionType        string          `json:"execType"`
	ClosedSize           string          `json:"closedSize"`
	CumulativeEntryValue string          `json:"cumEntryValue"`
	AverageEntryPrice    string          `json:"avgEntryPrice"`
	CumulativeExitValue  string          `json:"cumExitValue"`
	AverageExitPrice     string          `json:"avgExitPrice"`
	ClosedPnL            string          `json:"closedPnl"`
	FillCount            string          `json:"fillCount"`
	Leverage             string          `json:"leverage"`
	OpenFee              string          `json:"openFee"`
	CloseFee             string          `json:"closeFee"`
	CreatedTime          string          `json:"createdTime"`
	UpdatedTime          string          `json:"updatedTime"`
	Raw                  json.RawMessage `json:"-"`
}

// ClosedPnLPage는 cursor 기반 청산 손익 목록이다.
type ClosedPnLPage struct {
	Category       Category
	Records        []ClosedPnLRecord
	NextPageCursor string
	Raw            json.RawMessage
}

// ExecutionsRequest는 내 체결 내역 조회 조건이다.
// 시간 범위를 지정하지 않으면 최근 7일이며, 지정하면 범위는 7일을 넘을 수 없다.
// orderId, orderLinkId, symbol, baseCoin을 함께 지정하면 Bybit는 이 순서로 하나만 처리한다.
type ExecutionsRequest struct {
	Category      Category
	Symbol        string
	OrderID       string
	OrderLinkID   string
	BaseCoin      string
	StartTime     *time.Time
	EndTime       *time.Time
	ExecutionType string
	Limit         int
	Cursor        string
}

// Execution은 내 체결 한 건이다.
type Execution struct {
	Symbol         string          `json:"symbol"`
	OrderID        string          `json:"orderId"`
	OrderLinkID    string          `json:"orderLinkId"`
	Side           Side            `json:"side"`
	OrderPrice     string          `json:"orderPrice"`
	OrderQuantity  string          `json:"orderQty"`
	LeavesQuantity string          `json:"leavesQty"`
	CreateType     string          `json:"createType"`
	OrderType      OrderType       `json:"orderType"`
	StopOrderType  string          `json:"stopOrderType"`
	ExecutionFee   string          `json:"execFee"`
	ExecutionID    string          `json:"execId"`
	ExecutionPrice string          `json:"execPrice"`
	ExecutionQty   string          `json:"execQty"`
	ExecutionType  string          `json:"execType"`
	ExecutionValue string          `json:"execValue"`
	ExecutionTime  string          `json:"execTime"`
	FeeCurrency    string          `json:"feeCurrency"`
	Maker          bool            `json:"isMaker"`
	FeeRate        string          `json:"feeRate"`
	MarkPrice      string          `json:"markPrice"`
	BlockTradeID   string          `json:"blockTradeId"`
	ClosedSize     string          `json:"closedSize"`
	Sequence       int64           `json:"seq"`
	Raw            json.RawMessage `json:"-"`
}

// ExecutionPage는 cursor 기반 체결 목록이다.
type ExecutionPage struct {
	Category       Category
	Executions     []Execution
	NextPageCursor string
	Raw            json.RawMessage
}

// MovePositionLeg는 계정 간 포지션 이전의 개별 거래 다리다.
// Side는 FromUID 기준 방향이며, Linear 가격은 마크 가격의 95%~105% 안이어야 한다.
type MovePositionLeg struct {
	Category Category `json:"category"`
	Symbol   string   `json:"symbol"`
	Price    string   `json:"price"`
	Side     Side     `json:"side"`
	Quantity string   `json:"qty"`
}

// MovePositionsRequest는 마스터-서브 또는 서브-서브 UID 사이 포지션 이전 요청이다.
// 마스터 UID의 API Key로만 호출할 수 있고 한 요청에 최대 25개 다리를 담을 수 있다.
type MovePositionsRequest struct {
	FromUID string            `json:"fromUid"`
	ToUID   string            `json:"toUid"`
	Legs    []MovePositionLeg `json:"list"`
}

// MovePositionsResult는 포지션 이전 접수 결과다.
// RejectParty가 비어 있으면 초기 검증을 통과한 것이며 최종 상태는 MovePositionHistory로 확인한다.
type MovePositionsResult struct {
	BlockTradeID string          `json:"blockTradeId"`
	Status       string          `json:"status"`
	RejectParty  string          `json:"rejectParty"`
	Raw          json.RawMessage `json:"-"`
}

// MovePositionHistoryRequest는 포지션 이전 이력 조회 조건이다.
// 시간 범위는 7일을 넘을 수 없다. Status는 Processing, Filled, Rejected 중 하나다.
type MovePositionHistoryRequest struct {
	Category     Category
	Symbol       string
	StartTime    *time.Time
	EndTime      *time.Time
	Status       string
	BlockTradeID string
	Limit        int
	Cursor       string
}

// MovePositionRecord는 포지션 이전 이력 한 건이다.
type MovePositionRecord struct {
	BlockTradeID  string          `json:"blockTradeId"`
	Category      Category        `json:"category"`
	OrderID       string          `json:"orderId"`
	UserID        int64           `json:"userId"`
	Symbol        string          `json:"symbol"`
	Side          Side            `json:"side"`
	Price         string          `json:"price"`
	Quantity      string          `json:"qty"`
	ExecutionFee  string          `json:"execFee"`
	Status        string          `json:"status"`
	ExecutionID   string          `json:"execId"`
	ResultCode    int             `json:"resultCode"`
	ResultMessage string          `json:"resultMessage"`
	CreatedAt     int64           `json:"createdAt"`
	UpdatedAt     int64           `json:"updatedAt"`
	RejectParty   string          `json:"rejectParty"`
	Raw           json.RawMessage `json:"-"`
}

// MovePositionPage는 cursor 기반 포지션 이전 이력 목록이다.
type MovePositionPage struct {
	Records        []MovePositionRecord
	NextPageCursor string
	Raw            json.RawMessage
}

// SetLeverage는 Linear 심볼의 레버리지를 설정한다.
func (client *Client) SetLeverage(
	ctx context.Context,
	request SetLeverageRequest,
	options ...trade.RequestOption,
) (MutationAck, error) {
	if err := request.validate(); err != nil {
		return MutationAck{}, err
	}
	_, result, err := client.executeTradeMutation(
		ctx, "/v5/position/set-leverage", request, accountLimit(10), credential.PermissionTrade, options...,
	)
	if err != nil {
		return MutationAck{}, err
	}
	return MutationAck{Raw: cloneBytes(result)}, nil
}

// SwitchPositionMode는 Linear 심볼 또는 정산 코인 단위로 단방향·헤지 포지션 모드를 전환한다.
func (client *Client) SwitchPositionMode(
	ctx context.Context,
	request SwitchPositionModeRequest,
	options ...trade.RequestOption,
) (MutationAck, error) {
	if err := request.validate(); err != nil {
		return MutationAck{}, err
	}
	_, result, err := client.executeTradeMutation(
		ctx, "/v5/position/switch-mode", request, accountLimit(10), credential.PermissionTrade, options...,
	)
	if err != nil {
		return MutationAck{}, err
	}
	return MutationAck{Raw: cloneBytes(result)}, nil
}

// SetTradingStop은 Linear 포지션의 TP/SL과 트레일링 스톱을 설정한다.
// Bybit가 내부적으로 조건부 주문을 만들며 포지션이 닫히면 해당 주문도 취소된다.
func (client *Client) SetTradingStop(
	ctx context.Context,
	request TradingStopRequest,
	options ...trade.RequestOption,
) (MutationAck, error) {
	if err := request.validate(); err != nil {
		return MutationAck{}, err
	}
	_, result, err := client.executeTradeMutation(
		ctx, "/v5/position/trading-stop", request, accountLimit(10), credential.PermissionTrade, options...,
	)
	if err != nil {
		return MutationAck{}, err
	}
	return MutationAck{Raw: cloneBytes(result)}, nil
}

// SetAutoAddMargin은 격리 마진 Linear 포지션의 자동 증거금 추가를 켜거나 끈다.
func (client *Client) SetAutoAddMargin(
	ctx context.Context,
	request SetAutoAddMarginRequest,
	options ...trade.RequestOption,
) (MutationAck, error) {
	if err := request.validate(); err != nil {
		return MutationAck{}, err
	}
	_, result, err := client.executeTradeMutation(
		ctx, "/v5/position/set-auto-add-margin", request.payload(),
		accountLimit(10), credential.PermissionTrade, options...,
	)
	if err != nil {
		return MutationAck{}, err
	}
	return MutationAck{Raw: cloneBytes(result)}, nil
}

// AddMargin은 격리 마진 Linear 포지션의 증거금을 수동으로 추가하거나 줄이고 조정 후 포지션을 반환한다.
func (client *Client) AddMargin(
	ctx context.Context,
	request AddMarginRequest,
	options ...trade.RequestOption,
) (AddMarginResult, error) {
	if err := request.validate(); err != nil {
		return AddMarginResult{}, err
	}
	response, result, err := client.executeTradeMutation(
		ctx, "/v5/position/add-margin", request, accountLimit(10), credential.PermissionTrade, options...,
	)
	if err != nil {
		return AddMarginResult{}, err
	}
	var margin AddMarginResult
	if err := decodeResult(result, &margin); err != nil {
		return AddMarginResult{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	margin.Raw = cloneBytes(result)
	return margin, nil
}

// ConfirmPendingMMR는 reduce-only로 표시된 Linear 포지션의 새 리스크 한도를 확인해 표시를 해제한다.
func (client *Client) ConfirmPendingMMR(
	ctx context.Context,
	request ConfirmPendingMMRRequest,
	options ...trade.RequestOption,
) (MutationAck, error) {
	if err := request.validate(); err != nil {
		return MutationAck{}, err
	}
	_, result, err := client.executeTradeMutation(
		ctx, "/v5/position/confirm-pending-mmr", request, accountLimit(10), credential.PermissionTrade, options...,
	)
	if err != nil {
		return MutationAck{}, err
	}
	return MutationAck{Raw: cloneBytes(result)}, nil
}

// SymbolLeverages는 포지션이 없어도 Linear 심볼별 격리 레버리지 설정을 조회한다.
func (client *Client) SymbolLeverages(
	ctx context.Context,
	request SymbolLeverageRequest,
	options ...trade.RequestOption,
) ([]SymbolLeverage, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.executeTradeRead(
		ctx, "/v5/position/symbol-info", request.values(), accountLimit(10), options...,
	)
	if err != nil {
		return nil, err
	}
	_, items, _, err := decodeListPage(client, response, result, func(item *SymbolLeverage, raw json.RawMessage) {
		item.Raw = raw
	})
	return items, err
}

// ClosedPnL은 Linear 청산 손익을 cursor 기반으로 조회한다.
func (client *Client) ClosedPnL(
	ctx context.Context,
	request ClosedPnLRequest,
	options ...trade.RequestOption,
) (ClosedPnLPage, error) {
	if err := request.validate(); err != nil {
		return ClosedPnLPage{}, err
	}
	response, result, err := client.executeTradeRead(
		ctx, "/v5/position/closed-pnl", request.values(), accountLimit(20), options...,
	)
	if err != nil {
		return ClosedPnLPage{}, err
	}
	category, records, cursor, err := decodeListPage(
		client, response, result, func(item *ClosedPnLRecord, raw json.RawMessage) { item.Raw = raw },
	)
	if err != nil {
		return ClosedPnLPage{}, err
	}
	return ClosedPnLPage{Category: category, Records: records, NextPageCursor: cursor, Raw: cloneBytes(result)}, nil
}

// Executions는 내 체결 내역을 cursor 기반으로 조회한다.
func (client *Client) Executions(
	ctx context.Context,
	request ExecutionsRequest,
	options ...trade.RequestOption,
) (ExecutionPage, error) {
	if err := request.validate(); err != nil {
		return ExecutionPage{}, err
	}
	response, result, err := client.executeTradeRead(
		ctx, "/v5/execution/list", request.values(), accountLimit(20), options...,
	)
	if err != nil {
		return ExecutionPage{}, err
	}
	category, executions, cursor, err := decodeListPage(
		client, response, result, func(item *Execution, raw json.RawMessage) { item.Raw = raw },
	)
	if err != nil {
		return ExecutionPage{}, err
	}
	return ExecutionPage{
		Category: category, Executions: executions, NextPageCursor: cursor, Raw: cloneBytes(result),
	}, nil
}

// MovePositions는 마스터-서브 또는 서브-서브 UID 사이에서 포지션을 이전한다.
// 계정 간 이전이므로 PermissionTransfer가 필요하고 마스터 UID의 API Key로만 호출할 수 있다.
// 성공 응답은 접수이며 최종 상태는 MovePositionHistory로 확인해야 한다. 자동 재시도하지 않는다.
func (client *Client) MovePositions(
	ctx context.Context,
	request MovePositionsRequest,
	options ...trade.RequestOption,
) (MovePositionsResult, error) {
	if err := request.validate(); err != nil {
		return MovePositionsResult{}, err
	}
	response, result, err := client.executeTradeMutation(
		ctx, "/v5/position/move-positions", request, accountLimit(10), credential.PermissionTransfer, options...,
	)
	if err != nil {
		return MovePositionsResult{}, err
	}
	var moved MovePositionsResult
	if err := decodeResult(result, &moved); err != nil {
		return MovePositionsResult{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	moved.Raw = cloneBytes(result)
	return moved, nil
}

// MovePositionHistory는 포지션 이전 이력을 cursor 기반으로 조회한다.
func (client *Client) MovePositionHistory(
	ctx context.Context,
	request MovePositionHistoryRequest,
	options ...trade.RequestOption,
) (MovePositionPage, error) {
	if err := request.validate(); err != nil {
		return MovePositionPage{}, err
	}
	response, result, err := client.executeTradeRead(
		ctx, "/v5/position/move-history", request.values(), accountLimit(10), options...,
	)
	if err != nil {
		return MovePositionPage{}, err
	}
	_, records, cursor, err := decodeListPage(
		client, response, result, func(item *MovePositionRecord, raw json.RawMessage) { item.Raw = raw },
	)
	if err != nil {
		return MovePositionPage{}, err
	}
	return MovePositionPage{Records: records, NextPageCursor: cursor, Raw: cloneBytes(result)}, nil
}

// decodeListPage는 result의 list와 nextPageCursor를 항목별 Raw와 함께 디코드한다.
func decodeListPage[T any](
	client *Client,
	response commonexchange.Response,
	result json.RawMessage,
	setRaw func(item *T, raw json.RawMessage),
) (Category, []T, string, error) {
	var rawPage struct {
		Category       Category          `json:"category"`
		Items          []json.RawMessage `json:"list"`
		NextPageCursor string            `json:"nextPageCursor"`
	}
	if err := decodeResult(result, &rawPage); err != nil {
		return "", nil, "", client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	items := make([]T, len(rawPage.Items))
	for index, raw := range rawPage.Items {
		if err := decodeResult(raw, &items[index]); err != nil {
			return "", nil, "", client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		setRaw(&items[index], cloneBytes(raw))
	}
	return rawPage.Category, items, rawPage.NextPageCursor, nil
}

// autoAddMarginPayload는 bool 설정을 Bybit의 0/1 정수로 보내기 위한 JSON 본문이다.
type autoAddMarginPayload struct {
	Category      Category `json:"category"`
	Symbol        string   `json:"symbol"`
	AutoAddMargin int      `json:"autoAddMargin"`
	PositionIndex int      `json:"positionIdx,omitempty"`
}

func (request SetAutoAddMarginRequest) payload() autoAddMarginPayload {
	payload := autoAddMarginPayload{
		Category: request.Category, Symbol: request.Symbol, PositionIndex: request.PositionIndex,
	}
	if request.Enabled {
		payload.AutoAddMargin = 1
	}
	return payload
}

func (request SetLeverageRequest) validate() error {
	if err := validateLinearPositionCategory("set leverage", request.Category); err != nil {
		return err
	}
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	if err := validatePositiveDecimal("buyLeverage", request.BuyLeverage); err != nil {
		return err
	}
	return validatePositiveDecimal("sellLeverage", request.SellLeverage)
}

func (request SwitchPositionModeRequest) validate() error {
	if err := validateLinearPositionCategory("switch position mode", request.Category); err != nil {
		return err
	}
	if err := validateOptionalText("symbol", request.Symbol); err != nil {
		return err
	}
	if err := validateOptionalText("coin", request.Coin); err != nil {
		return err
	}
	if request.Symbol == "" && request.Coin == "" {
		return validationError("symbol or coin is required")
	}
	if request.Mode != PositionModeOneWay && request.Mode != PositionModeHedge {
		return validationError("position mode must be 0 (one-way) or 3 (hedge)")
	}
	return nil
}

func (request TradingStopRequest) validate() error {
	if err := validateLinearPositionCategory("trading stop", request.Category); err != nil {
		return err
	}
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	if !request.TPSLMode.valid() {
		return validationError("tpslMode must be Full or Partial")
	}
	if request.PositionIndex < 0 || request.PositionIndex > 2 {
		return validationError("position index must be 0, 1, or 2")
	}
	if err := validateTradeDecimals(
		true,
		tradeDecimalField{"takeProfit", request.TakeProfit},
		tradeDecimalField{"stopLoss", request.StopLoss},
		tradeDecimalField{"trailingStop", request.TrailingStop},
		tradeDecimalField{"activePrice", request.ActivePrice},
	); err != nil {
		return err
	}
	if err := validateTradeDecimals(
		false,
		tradeDecimalField{"tpSize", request.TPSize},
		tradeDecimalField{"slSize", request.SLSize},
		tradeDecimalField{"tpLimitPrice", request.TPLimitPrice},
		tradeDecimalField{"slLimitPrice", request.SLLimitPrice},
	); err != nil {
		return err
	}
	if err := validateTradeTriggers(
		tradeTriggerField{"tpTriggerBy", request.TPTriggerBy},
		tradeTriggerField{"slTriggerBy", request.SLTriggerBy},
	); err != nil {
		return err
	}
	for _, orderType := range []struct {
		name  string
		value OrderType
	}{{"tpOrderType", request.TPOrderType}, {"slOrderType", request.SLOrderType}} {
		if orderType.value != "" && orderType.value != OrderTypeMarket && orderType.value != OrderTypeLimit {
			return validationError("%s must be Market or Limit", orderType.name)
		}
	}
	if request.TPSLMode == TPSLModeFull {
		if request.TPSize != "" || request.SLSize != "" || request.TPLimitPrice != "" || request.SLLimitPrice != "" {
			return validationError("Full tpslMode does not accept tpSize, slSize, tpLimitPrice, or slLimitPrice")
		}
		if request.TPOrderType == OrderTypeLimit || request.SLOrderType == OrderTypeLimit {
			return validationError("Full tpslMode supports only Market tpOrderType and slOrderType")
		}
	}
	if request.TPLimitPrice != "" && request.TPOrderType != OrderTypeLimit {
		return validationError("tpLimitPrice requires tpOrderType Limit")
	}
	if request.SLLimitPrice != "" && request.SLOrderType != OrderTypeLimit {
		return validationError("slLimitPrice requires slOrderType Limit")
	}
	if request.TPSize != "" && request.SLSize != "" && !equalDecimalStrings(request.TPSize, request.SLSize) {
		return validationError("tpSize and slSize must be equal")
	}
	return nil
}

func (request SetAutoAddMarginRequest) validate() error {
	if err := validateLinearPositionCategory("set auto add margin", request.Category); err != nil {
		return err
	}
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	if request.PositionIndex < 0 || request.PositionIndex > 2 {
		return validationError("position index must be 0, 1, or 2")
	}
	return nil
}

func (request AddMarginRequest) validate() error {
	if err := validateLinearPositionCategory("add margin", request.Category); err != nil {
		return err
	}
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	if !marginAmountPattern.MatchString(request.Margin) ||
		strings.Trim(strings.TrimPrefix(request.Margin, "-"), "0.") == "" {
		return validationError("margin must be a non-zero signed decimal with at most 4 decimal places")
	}
	if request.PositionIndex < 0 || request.PositionIndex > 2 {
		return validationError("position index must be 0, 1, or 2")
	}
	return nil
}

func (request ConfirmPendingMMRRequest) validate() error {
	if err := validateLinearPositionCategory("confirm pending MMR", request.Category); err != nil {
		return err
	}
	return validateRequiredText("symbol", request.Symbol)
}

func (request SymbolLeverageRequest) validate() error {
	if err := validateLinearPositionCategory("symbol leverage", request.Category); err != nil {
		return err
	}
	return validateOptionalText("symbol", request.Symbol)
}

func (request SymbolLeverageRequest) values() url.Values {
	values := categoryValues(request.Category)
	setIfNotEmpty(values, "symbol", request.Symbol)
	return values
}

func (request ClosedPnLRequest) validate() error {
	if err := validateLinearPositionCategory("closed PnL", request.Category); err != nil {
		return err
	}
	if err := validateOptionalText("symbol", request.Symbol); err != nil {
		return err
	}
	if err := validatePositionTimeRange("closed PnL", request.StartTime, request.EndTime); err != nil {
		return err
	}
	return validatePage(request.Limit, 100, request.Cursor)
}

func (request ClosedPnLRequest) values() url.Values {
	values := categoryValues(request.Category)
	setIfNotEmpty(values, "symbol", request.Symbol)
	setTime(values, "startTime", request.StartTime)
	setTime(values, "endTime", request.EndTime)
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "cursor", request.Cursor)
	return values
}

func (request ExecutionsRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateOptionalText("symbol", request.Symbol); err != nil {
		return err
	}
	if err := validateOptionalText("orderId", request.OrderID); err != nil {
		return err
	}
	if err := validateOptionalText("orderLinkId", request.OrderLinkID); err != nil {
		return err
	}
	if request.OrderLinkID != "" && !orderLinkIDPattern.MatchString(request.OrderLinkID) {
		return validationError("orderLinkId has an invalid format")
	}
	if err := validateOptionalText("baseCoin", request.BaseCoin); err != nil {
		return err
	}
	if err := validateOptionalText("execType", request.ExecutionType); err != nil {
		return err
	}
	if err := validatePositionTimeRange("execution", request.StartTime, request.EndTime); err != nil {
		return err
	}
	return validatePage(request.Limit, 100, request.Cursor)
}

func (request ExecutionsRequest) values() url.Values {
	values := categoryValues(request.Category)
	setIfNotEmpty(values, "symbol", request.Symbol)
	setIfNotEmpty(values, "orderId", request.OrderID)
	setIfNotEmpty(values, "orderLinkId", request.OrderLinkID)
	setIfNotEmpty(values, "baseCoin", request.BaseCoin)
	setTime(values, "startTime", request.StartTime)
	setTime(values, "endTime", request.EndTime)
	setIfNotEmpty(values, "execType", request.ExecutionType)
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "cursor", request.Cursor)
	return values
}

func (request MovePositionsRequest) validate() error {
	if !positionUIDPattern.MatchString(request.FromUID) {
		return validationError("fromUid must be a numeric UID")
	}
	if !positionUIDPattern.MatchString(request.ToUID) {
		return validationError("toUid must be a numeric UID")
	}
	if request.FromUID == request.ToUID {
		return validationError("fromUid and toUid must differ")
	}
	if len(request.Legs) < 1 || len(request.Legs) > maxMovePositionLegs {
		return validationError("move positions requires between 1 and %d legs", maxMovePositionLegs)
	}
	for _, leg := range request.Legs {
		if err := leg.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (leg MovePositionLeg) validate() error {
	if err := validateCategory(leg.Category); err != nil {
		return err
	}
	if err := validateRequiredText("symbol", leg.Symbol); err != nil {
		return err
	}
	if err := validatePositiveDecimal("price", leg.Price); err != nil {
		return err
	}
	if leg.Side != SideBuy && leg.Side != SideSell {
		return validationError("side must be Buy or Sell")
	}
	return validatePositiveDecimal("qty", leg.Quantity)
}

func (request MovePositionHistoryRequest) validate() error {
	if request.Category != "" {
		if err := validateCategory(request.Category); err != nil {
			return err
		}
	}
	if err := validateOptionalText("symbol", request.Symbol); err != nil {
		return err
	}
	if err := validatePositionTimeRange("move position history", request.StartTime, request.EndTime); err != nil {
		return err
	}
	switch request.Status {
	case "", "Processing", "Filled", "Rejected":
	default:
		return validationError("status must be Processing, Filled, or Rejected")
	}
	if err := validateOptionalText("blockTradeId", request.BlockTradeID); err != nil {
		return err
	}
	return validatePage(request.Limit, 200, request.Cursor)
}

func (request MovePositionHistoryRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "category", string(request.Category))
	setIfNotEmpty(values, "symbol", request.Symbol)
	setTime(values, "startTime", request.StartTime)
	setTime(values, "endTime", request.EndTime)
	setIfNotEmpty(values, "status", request.Status)
	setIfNotEmpty(values, "blockTradeId", request.BlockTradeID)
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "cursor", request.Cursor)
	return values
}

func validateLinearPositionCategory(operation string, category Category) error {
	if category != CategoryLinear {
		return validationError("%s supports only linear category", operation)
	}
	return nil
}

func validatePositionTimeRange(name string, start, end *time.Time) error {
	if start == nil || end == nil {
		return nil
	}
	if start.After(*end) {
		return validationError("%s start time cannot be after end time", name)
	}
	if end.Sub(*start) > positionHistoryWindow {
		return validationError("%s time range cannot exceed 7 days", name)
	}
	return nil
}

func equalDecimalStrings(left, right string) bool {
	leftValue, leftOK := new(big.Rat).SetString(left)
	rightValue, rightOK := new(big.Rat).SetString(right)
	return leftOK && rightOK && leftValue.Cmp(rightValue) == 0
}
