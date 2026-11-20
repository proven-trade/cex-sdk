package bybit

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	// transactionLogWindow는 거래 로그 조회의 최대 시간 범위다.
	transactionLogWindow = 7 * 24 * time.Hour
	// borrowHistoryWindow는 차입 이력 조회의 최대 시간 범위다.
	borrowHistoryWindow = 30 * 24 * time.Hour
)

// MarginMode는 통합 계정의 마진 모드다.
type MarginMode string

const (
	// MarginModeIsolated는 격리 마진이다.
	MarginModeIsolated MarginMode = "ISOLATED_MARGIN"
	// MarginModeRegular는 교차 마진이다.
	MarginModeRegular MarginMode = "REGULAR_MARGIN"
	// MarginModePortfolio는 포트폴리오 마진이다.
	MarginModePortfolio MarginMode = "PORTFOLIO_MARGIN"
)

// RepaymentType은 수동 상환 대상 부채 종류다.
type RepaymentType string

const (
	// RepaymentTypeAll은 변동금리 부채를 먼저, 이어서 고정금리 부채를 상환한다.
	RepaymentTypeAll RepaymentType = "ALL"
	// RepaymentTypeFixed는 고정금리 부채만 상환한다.
	RepaymentTypeFixed RepaymentType = "FIXED"
	// RepaymentTypeFlexible은 변동금리 부채만 상환한다. 비우면 Bybit 기본값이다.
	RepaymentTypeFlexible RepaymentType = "FLEXIBLE"
)

// AccountInfo는 통합 계정의 마진 모드와 계정 설정이다.
type AccountInfo struct {
	UnifiedMarginStatus int             `json:"unifiedMarginStatus"`
	MarginMode          MarginMode      `json:"marginMode"`
	DCPStatus           string          `json:"dcpStatus"`
	TimeWindow          int             `json:"timeWindow"`
	SMPGroup            int             `json:"smpGroup"`
	IsMasterTrader      bool            `json:"isMasterTrader"`
	SpotHedgingStatus   string          `json:"spotHedgingStatus"`
	UpdatedTime         string          `json:"updatedTime"`
	Raw                 json.RawMessage `json:"-"`
}

// FeeRatesRequest는 거래 수수료율 조회 조건이다.
type FeeRatesRequest struct {
	Category Category
	Symbol   string
}

// FeeRate는 심볼의 maker·taker 수수료율이다. Spot 응답의 BaseCoin은 비어 있다.
type FeeRate struct {
	Symbol       string          `json:"symbol"`
	BaseCoin     string          `json:"baseCoin"`
	TakerFeeRate string          `json:"takerFeeRate"`
	MakerFeeRate string          `json:"makerFeeRate"`
	Raw          json.RawMessage `json:"-"`
}

// TransactionLogRequest는 통합 계정 거래 로그(원장) 조회 조건이다.
// Category를 비우면 전체 상품을 조회하며 StartTime·EndTime을 모두 지정하면 7일을 넘을 수 없다.
type TransactionLogRequest struct {
	AccountType AccountType
	Category    Category
	Currency    string
	BaseCoin    string
	Type        string
	StartTime   *time.Time
	EndTime     *time.Time
	Limit       int
	Cursor      string
}

// TransactionLogRecord는 거래 로그 한 건이다.
type TransactionLogRecord struct {
	ID              string          `json:"id"`
	Symbol          string          `json:"symbol"`
	Category        Category        `json:"category"`
	Side            string          `json:"side"`
	TransactionTime string          `json:"transactionTime"`
	Type            string          `json:"type"`
	Quantity        string          `json:"qty"`
	Size            string          `json:"size"`
	Currency        string          `json:"currency"`
	TradePrice      string          `json:"tradePrice"`
	Funding         string          `json:"funding"`
	Fee             string          `json:"fee"`
	CashFlow        string          `json:"cashFlow"`
	Change          string          `json:"change"`
	CashBalance     string          `json:"cashBalance"`
	FeeRate         string          `json:"feeRate"`
	BonusChange     string          `json:"bonusChange"`
	TradeID         string          `json:"tradeId"`
	OrderID         string          `json:"orderId"`
	OrderLinkID     string          `json:"orderLinkId"`
	Raw             json.RawMessage `json:"-"`
}

// TransactionLogPage는 거래 로그 조회 한 페이지다.
type TransactionLogPage struct {
	Records        []TransactionLogRecord
	NextPageCursor string
	Raw            json.RawMessage
}

// BorrowHistoryRequest는 차입 이자 이력 조회 조건이다.
// StartTime·EndTime을 모두 지정하면 30일을 넘을 수 없다.
type BorrowHistoryRequest struct {
	Currency  string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Cursor    string
}

// BorrowHistoryRecord는 차입 이자 기록 한 건이다. 생성 시각 CreatedTime은 밀리초 정수다.
type BorrowHistoryRecord struct {
	Currency                  string          `json:"currency"`
	CreatedTime               int64           `json:"createdTime"`
	BorrowCost                string          `json:"borrowCost"`
	HourlyBorrowRate          string          `json:"hourlyBorrowRate"`
	InterestBearingBorrowSize string          `json:"InterestBearingBorrowSize"`
	CostExemption             string          `json:"costExemption"`
	BorrowAmount              string          `json:"borrowAmount"`
	UnrealisedLoss            string          `json:"unrealisedLoss"`
	FreeBorrowedAmount        string          `json:"freeBorrowedAmount"`
	Raw                       json.RawMessage `json:"-"`
}

// BorrowHistoryPage는 차입 이력 조회 한 페이지다.
type BorrowHistoryPage struct {
	Records        []BorrowHistoryRecord
	NextPageCursor string
	Raw            json.RawMessage
}

// CollateralInfoRequest는 담보 정보 조회 조건이다. Currency를 비우면 전체 코인을 반환한다.
type CollateralInfoRequest struct {
	Currency string
}

// CollateralInfo는 코인별 담보·차입 정보다.
type CollateralInfo struct {
	Currency           string          `json:"currency"`
	HourlyBorrowRate   string          `json:"hourlyBorrowRate"`
	MaxBorrowingAmount string          `json:"maxBorrowingAmount"`
	FreeBorrowingLimit string          `json:"freeBorrowingLimit"`
	FreeBorrowAmount   string          `json:"freeBorrowAmount"`
	BorrowAmount       string          `json:"borrowAmount"`
	AvailableToBorrow  string          `json:"availableToBorrow"`
	Borrowable         bool            `json:"borrowable"`
	BorrowUsageRate    string          `json:"borrowUsageRate"`
	MarginCollateral   bool            `json:"marginCollateral"`
	CollateralSwitch   bool            `json:"collateralSwitch"`
	CollateralRatio    string          `json:"collateralRatio"`
	Raw                json.RawMessage `json:"-"`
}

// SetCollateralSwitchRequest는 코인 하나의 담보 사용 여부 설정 요청이다.
// USDT와 USDC는 끌 수 없다.
type SetCollateralSwitchRequest struct {
	Coin    string
	Enabled bool
}

// CollateralSwitchItem은 일괄 담보 설정의 코인별 요청·응답 항목이다.
type CollateralSwitchItem struct {
	Coin    string
	Enabled bool
}

// BatchSetCollateralSwitchRequest는 여러 코인의 담보 사용 여부를 한 번에 설정하는 요청이다.
type BatchSetCollateralSwitchRequest struct {
	Items []CollateralSwitchItem
}

// BatchSetCollateralSwitchResult는 일괄 담보 설정으로 반영된 코인별 상태다.
type BatchSetCollateralSwitchResult struct {
	Items []CollateralSwitchItem
	Raw   json.RawMessage
}

// SetMarginModeRequest는 통합 계정 마진 모드 변경 요청이다.
type SetMarginModeRequest struct {
	Mode MarginMode
}

// BorrowRequest는 변동금리 수동 차입 요청이다.
type BorrowRequest struct {
	Coin   string
	Amount string
}

// BorrowResult는 접수된 차입 코인과 수량이다.
type BorrowResult struct {
	Coin   string          `json:"coin"`
	Amount string          `json:"amount"`
	Raw    json.RawMessage `json:"-"`
}

// RepayRequest는 수동 상환 요청이다. 같은 형식을 자산 전환 없는 상환에도 쓴다.
// Coin을 비우면 Amount도 비워야 하고 RepaymentType은 RepaymentTypeAll이어야 하며 전체 부채를 상환한다.
type RepayRequest struct {
	Coin          string
	Amount        string
	RepaymentType RepaymentType
}

// RepayResult는 수동 상환 접수 상태다. P는 처리 중, SU는 성공, FA는 실패다.
// FIXED·ALL은 비동기로 처리되므로 부채 상태는 CollateralInfo 등으로 다시 확인해야 한다.
type RepayResult struct {
	ResultStatus string          `json:"resultStatus"`
	Raw          json.RawMessage `json:"-"`
}

// RepayLiabilityRequest는 통합 계정 부채 일괄 상환 요청이다. Coin을 비우면 모든 코인의 부채를 상환한다.
type RepayLiabilityRequest struct {
	Coin string
}

// RepayLiabilityItem은 부채 상환에 사용한 코인과 상환 수량이다.
type RepayLiabilityItem struct {
	Coin         string          `json:"coin"`
	RepaymentQty string          `json:"repaymentQty"`
	Raw          json.RawMessage `json:"-"`
}

// RepayLiabilityResult는 부채 일괄 상환 결과다.
type RepayLiabilityResult struct {
	Items []RepayLiabilityItem
	Raw   json.RawMessage
}

// PayInfoRequest는 상환 담보 정보 조회 조건이다.
// Coin은 미상환 부채가 있는 코인이어야 하며 비우면 전체 부채 합계를 반환한다.
type PayInfoRequest struct {
	Coin string
}

// PayInfoCollateral은 상환에 쓸 수 있는 코인별 담보다.
type PayInfoCollateral struct {
	Coin            string `json:"coin"`
	AvailableSize   string `json:"availableSize"`
	AvailableValue  string `json:"availableValue"`
	CoinScale       int    `json:"coinScale"`
	BorrowSize      string `json:"borrowSize"`
	SpotHedgeAmount string `json:"spotHedgeAmount"`
	AssetFrozen     string `json:"assetFrozen"`
}

// PayInfoBorrow는 조회한 코인의 차입 현황이다. Coin은 요청에 coin을 지정했을 때만 채워진다.
type PayInfoBorrow struct {
	Coin             string `json:"coin"`
	BorrowSize       string `json:"borrowSize"`
	BorrowValue      string `json:"borrowValue"`
	AssetFrozen      string `json:"assetFrozen"`
	AvailableBalance string `json:"availableBalance"`
}

// PayInfo는 상환 전에 확인하는 담보와 차입 현황이다.
type PayInfo struct {
	Collateral []PayInfoCollateral
	Borrow     PayInfoBorrow
	Raw        json.RawMessage
}

// DCPInfo는 상품별 연결 끊김 보호(DCP) 설정이다.
type DCPInfo struct {
	Product    string          `json:"product"`
	DCPStatus  string          `json:"dcpStatus"`
	TimeWindow string          `json:"timeWindow"`
	Raw        json.RawMessage `json:"-"`
}

// SMPGroup은 자전거래 방지(SMP) 그룹 ID다. 그룹이 없으면 0이다.
type SMPGroup struct {
	ID  int
	Raw json.RawMessage
}

// UserSettingConfig는 주문 가격 제한 초과 시 동작과 델타 중립·수수료 공제 설정이다.
type UserSettingConfig struct {
	LimitPriceActionSpot bool            `json:"lpaSpot"`
	LimitPriceActionPerp bool            `json:"lpaPerp"`
	DeltaEnabled         bool            `json:"deltaEnable"`
	SpotMNTFeeDeduction  bool            `json:"smsef"`
	FuturesMNTFeeDeduct  bool            `json:"fmsef"`
	SMPType              int             `json:"smpType"`
	Raw                  json.RawMessage `json:"-"`
}

// SetDeltaModeRequest는 델타 중립 모드 설정 요청이다.
type SetDeltaModeRequest struct {
	Enabled bool
}

// SpotTradeAnalysisRequest는 Spot 심볼의 체결 집계 조회 조건이다.
type SpotTradeAnalysisRequest struct {
	Symbol    string
	StartTime *time.Time
	EndTime   *time.Time
}

// SpotTradeAnalysisDay는 일별 체결 금액 집계다.
type SpotTradeAnalysisDay struct {
	Day              string `json:"day"`
	SumBuyExecValue  string `json:"sumBuyExecValue"`
	SumSellExecValue string `json:"sumSellExecValue"`
	SumExecValue     string `json:"sumExecValue"`
}

// SpotTradeAnalysis는 Spot 심볼의 체결 집계다.
type SpotTradeAnalysis struct {
	SymbolRealisedPnL string                 `json:"symbolRnl"`
	NetExecQuantity   string                 `json:"netExecQty"`
	SumExecValue      string                 `json:"sumExecValue"`
	SumExecQuantity   string                 `json:"sumExecQty"`
	AvgBuyExecPrice   string                 `json:"avgBuyExecPrice"`
	SumBuyExecValue   string                 `json:"sumBuyExecValue"`
	SumBuyExecQty     string                 `json:"sumBuyExecQty"`
	SumBuyExecFee     string                 `json:"sumBuyExecFee"`
	SumBuyOrderQty    string                 `json:"sumBuyOrderQty"`
	AvgSellExecPrice  string                 `json:"avgSellExecPrice"`
	SumSellExecValue  string                 `json:"sumSellExecValue"`
	SumSellExecQty    string                 `json:"sumSellExecQty"`
	SumSellExecFee    string                 `json:"sumSellExecFee"`
	SumSellOrderQty   string                 `json:"sumSellOrderQty"`
	MaxMarginVersion  int                    `json:"maxMarginVersion"`
	BaseCoin          string                 `json:"baseCoin"`
	SettleCoin        string                 `json:"settleCoin"`
	DailyPrices       []SpotTradeAnalysisDay `json:"sumPriceList"`
	Raw               json.RawMessage        `json:"-"`
}

// AccountInfo는 통합 계정의 마진 모드와 계정 상태를 조회한다.
func (client *Client) AccountInfo(ctx context.Context, options ...trade.RequestOption) (AccountInfo, error) {
	response, result, err := client.executeTradeRead(ctx, "/v5/account/info", nil, accountLimit(50), options...)
	if err != nil {
		return AccountInfo{}, err
	}
	var info AccountInfo
	if err := decodeResult(result, &info); err != nil {
		return AccountInfo{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	info.Raw = cloneBytes(result)
	return info, nil
}

// FeeRates는 Spot 또는 Linear 심볼의 maker·taker 수수료율을 조회한다.
func (client *Client) FeeRates(
	ctx context.Context,
	request FeeRatesRequest,
	options ...trade.RequestOption,
) ([]FeeRate, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.executeTradeRead(
		ctx, "/v5/account/fee-rate", request.values(), accountLimit(5), options...,
	)
	if err != nil {
		return nil, err
	}
	_, rates, _, err := decodeListPage(client, response, result, func(item *FeeRate, raw json.RawMessage) {
		item.Raw = raw
	})
	return rates, err
}

// TransactionLog는 통합 계정의 거래 로그(원장)를 cursor 기반으로 조회한다.
func (client *Client) TransactionLog(
	ctx context.Context,
	request TransactionLogRequest,
	options ...trade.RequestOption,
) (TransactionLogPage, error) {
	if err := request.validate(); err != nil {
		return TransactionLogPage{}, err
	}
	response, result, err := client.executeTradeRead(
		ctx, "/v5/account/transaction-log", request.values(), accountLimit(25), options...,
	)
	if err != nil {
		return TransactionLogPage{}, err
	}
	_, records, cursor, err := decodeListPage(
		client, response, result, func(item *TransactionLogRecord, raw json.RawMessage) { item.Raw = raw },
	)
	if err != nil {
		return TransactionLogPage{}, err
	}
	return TransactionLogPage{Records: records, NextPageCursor: cursor, Raw: cloneBytes(result)}, nil
}

// BorrowHistory는 통합 계정의 차입 이자 이력을 cursor 기반으로 조회한다.
func (client *Client) BorrowHistory(
	ctx context.Context,
	request BorrowHistoryRequest,
	options ...trade.RequestOption,
) (BorrowHistoryPage, error) {
	if err := request.validate(); err != nil {
		return BorrowHistoryPage{}, err
	}
	response, result, err := client.executeTradeRead(
		ctx, "/v5/account/borrow-history", request.values(), accountLimit(50), options...,
	)
	if err != nil {
		return BorrowHistoryPage{}, err
	}
	_, records, cursor, err := decodeListPage(
		client, response, result, func(item *BorrowHistoryRecord, raw json.RawMessage) { item.Raw = raw },
	)
	if err != nil {
		return BorrowHistoryPage{}, err
	}
	return BorrowHistoryPage{Records: records, NextPageCursor: cursor, Raw: cloneBytes(result)}, nil
}

// CollateralInfos는 통합 계정의 코인별 담보·차입 정보를 조회한다.
func (client *Client) CollateralInfos(
	ctx context.Context,
	request CollateralInfoRequest,
	options ...trade.RequestOption,
) ([]CollateralInfo, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.executeTradeRead(
		ctx, "/v5/account/collateral-info", request.values(), accountLimit(50), options...,
	)
	if err != nil {
		return nil, err
	}
	_, infos, _, err := decodeListPage(client, response, result, func(item *CollateralInfo, raw json.RawMessage) {
		item.Raw = raw
	})
	return infos, err
}

// SetCollateralSwitch는 코인 하나를 담보로 쓸지 설정한다.
func (client *Client) SetCollateralSwitch(
	ctx context.Context,
	request SetCollateralSwitchRequest,
	options ...trade.RequestOption,
) (MutationAck, error) {
	if err := request.validate(); err != nil {
		return MutationAck{}, err
	}
	_, result, err := client.executeTradeMutation(
		ctx, "/v5/account/set-collateral-switch", request.payload(),
		accountLimit(5), credential.PermissionTrade, options...,
	)
	if err != nil {
		return MutationAck{}, err
	}
	return MutationAck{Raw: cloneBytes(result)}, nil
}

// BatchSetCollateralSwitch는 여러 코인의 담보 사용 여부를 한 번에 설정한다.
func (client *Client) BatchSetCollateralSwitch(
	ctx context.Context,
	request BatchSetCollateralSwitchRequest,
	options ...trade.RequestOption,
) (BatchSetCollateralSwitchResult, error) {
	if err := request.validate(); err != nil {
		return BatchSetCollateralSwitchResult{}, err
	}
	response, result, err := client.executeTradeMutation(
		ctx, "/v5/account/set-collateral-switch-batch", request.payload(),
		accountLimit(5), credential.PermissionTrade, options...,
	)
	if err != nil {
		return BatchSetCollateralSwitchResult{}, err
	}
	var decoded struct {
		List []struct {
			Coin             string `json:"coin"`
			CollateralSwitch string `json:"collateralSwitch"`
		} `json:"list"`
	}
	if err := decodeResult(result, &decoded); err != nil {
		return BatchSetCollateralSwitchResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, err,
		)
	}
	items := make([]CollateralSwitchItem, len(decoded.List))
	for index, item := range decoded.List {
		items[index] = CollateralSwitchItem{Coin: item.Coin, Enabled: item.CollateralSwitch == collateralSwitchOn}
	}
	return BatchSetCollateralSwitchResult{Items: items, Raw: cloneBytes(result)}, nil
}

// SetMarginMode는 통합 계정의 마진 모드를 변경한다.
// 조건을 충족하지 못해 실패하면 Bybit 오류 응답의 result.reasons에 사유가 담긴다.
func (client *Client) SetMarginMode(
	ctx context.Context,
	request SetMarginModeRequest,
	options ...trade.RequestOption,
) (MutationAck, error) {
	if err := request.validate(); err != nil {
		return MutationAck{}, err
	}
	_, result, err := client.executeTradeMutation(
		ctx, "/v5/account/set-margin-mode", request.payload(), accountLimit(5), credential.PermissionTrade, options...,
	)
	if err != nil {
		return MutationAck{}, err
	}
	return MutationAck{Raw: cloneBytes(result)}, nil
}

// Borrow는 변동금리로 수동 차입한다. 차입은 부채를 만드는 변경 요청이다.
func (client *Client) Borrow(
	ctx context.Context,
	request BorrowRequest,
	options ...trade.RequestOption,
) (BorrowResult, error) {
	if err := request.validate(); err != nil {
		return BorrowResult{}, err
	}
	response, result, err := client.executeTradeMutation(
		ctx, "/v5/account/borrow", request.payload(), accountLimit(1), credential.PermissionTrade, options...,
	)
	if err != nil {
		return BorrowResult{}, err
	}
	var borrow BorrowResult
	if err := decodeResult(result, &borrow); err != nil {
		return BorrowResult{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	borrow.Raw = cloneBytes(result)
	return borrow, nil
}

// Repay는 부채를 수동 상환한다. 현물 잔고가 부족하면 청산 순서에 따라 다른 자산을 전환해 상환한다.
// 자산 전환 없이 현물 잔고로만 상환하려면 RepayWithoutConversion을 사용한다.
func (client *Client) Repay(
	ctx context.Context,
	request RepayRequest,
	options ...trade.RequestOption,
) (RepayResult, error) {
	return client.repay(ctx, "/v5/account/repay", request, options...)
}

// RepayWithoutConversion은 부채 코인의 현물 가용 잔고로만 수동 상환하며 다른 자산을 전환하지 않는다.
// Coin이 필수다.
func (client *Client) RepayWithoutConversion(
	ctx context.Context,
	request RepayRequest,
	options ...trade.RequestOption,
) (RepayResult, error) {
	if request.Coin == "" {
		return RepayResult{}, validationError("coin is required for repay without conversion")
	}
	return client.repay(ctx, "/v5/account/no-convert-repay", request, options...)
}

func (client *Client) repay(
	ctx context.Context,
	path string,
	request RepayRequest,
	options ...trade.RequestOption,
) (RepayResult, error) {
	if err := request.validate(); err != nil {
		return RepayResult{}, err
	}
	response, result, err := client.executeTradeMutation(
		ctx, path, request.payload(), accountLimit(1), credential.PermissionTrade, options...,
	)
	if err != nil {
		return RepayResult{}, err
	}
	var repay RepayResult
	if err := decodeResult(result, &repay); err != nil {
		return RepayResult{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	repay.Raw = cloneBytes(result)
	return repay, nil
}

// RepayLiability는 통합 계정의 부채를 일괄 상환한다.
func (client *Client) RepayLiability(
	ctx context.Context,
	request RepayLiabilityRequest,
	options ...trade.RequestOption,
) (RepayLiabilityResult, error) {
	if err := request.validate(); err != nil {
		return RepayLiabilityResult{}, err
	}
	response, result, err := client.executeTradeMutation(
		ctx, "/v5/account/quick-repayment", request.payload(), accountLimit(1), credential.PermissionTrade, options...,
	)
	if err != nil {
		return RepayLiabilityResult{}, err
	}
	_, items, _, err := decodeListPageAs(
		client, response, commonexchange.OperationMutation, result,
		func(item *RepayLiabilityItem, raw json.RawMessage) { item.Raw = raw },
	)
	if err != nil {
		return RepayLiabilityResult{}, err
	}
	return RepayLiabilityResult{Items: items, Raw: cloneBytes(result)}, nil
}

// PayInfo는 상환 전에 사용할 수 있는 담보와 차입 현황을 조회한다.
func (client *Client) PayInfo(
	ctx context.Context,
	request PayInfoRequest,
	options ...trade.RequestOption,
) (PayInfo, error) {
	if err := request.validate(); err != nil {
		return PayInfo{}, err
	}
	response, result, err := client.executeTradeRead(
		ctx, "/v5/account/pay-info", request.values(), accountLimit(50), options...,
	)
	if err != nil {
		return PayInfo{}, err
	}
	var decoded struct {
		CollateralInfo struct {
			CollateralList []PayInfoCollateral `json:"collateralList"`
		} `json:"collateralInfo"`
		BorrowInfo PayInfoBorrow `json:"borrowInfo"`
	}
	if err := decodeResult(result, &decoded); err != nil {
		return PayInfo{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return PayInfo{
		Collateral: decoded.CollateralInfo.CollateralList, Borrow: decoded.BorrowInfo, Raw: cloneBytes(result),
	}, nil
}

// DCPInfos는 상품별 연결 끊김 보호(DCP) 설정을 조회한다.
// DCP가 설정된 계정만 값을 받으며 그렇지 않으면 빈 목록이다.
func (client *Client) DCPInfos(ctx context.Context, options ...trade.RequestOption) ([]DCPInfo, error) {
	response, result, err := client.executeTradeRead(ctx, "/v5/account/query-dcp-info", nil, accountLimit(5), options...)
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Items []json.RawMessage `json:"dcpInfos"`
	}
	if err := decodeResult(result, &decoded); err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	infos := make([]DCPInfo, len(decoded.Items))
	for index, raw := range decoded.Items {
		if err := decodeResult(raw, &infos[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		infos[index].Raw = cloneBytes(raw)
	}
	return infos, nil
}

// SMPGroup은 자전거래 방지(SMP) 그룹 ID를 조회한다.
func (client *Client) SMPGroup(ctx context.Context, options ...trade.RequestOption) (SMPGroup, error) {
	response, result, err := client.executeTradeRead(ctx, "/v5/account/smp-group", nil, accountLimit(5), options...)
	if err != nil {
		return SMPGroup{}, err
	}
	var decoded struct {
		SMPGroup int `json:"smpGroup"`
	}
	if err := decodeResult(result, &decoded); err != nil {
		return SMPGroup{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return SMPGroup{ID: decoded.SMPGroup, Raw: cloneBytes(result)}, nil
}

// UserSettingConfig는 가격 제한 초과 시 동작, 델타 중립, 수수료 공제 설정을 조회한다.
func (client *Client) UserSettingConfig(
	ctx context.Context,
	options ...trade.RequestOption,
) (UserSettingConfig, error) {
	response, result, err := client.executeTradeRead(
		ctx, "/v5/account/user-setting-config", nil, accountLimit(50), options...,
	)
	if err != nil {
		return UserSettingConfig{}, err
	}
	var config UserSettingConfig
	if err := decodeResult(result, &config); err != nil {
		return UserSettingConfig{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	config.Raw = cloneBytes(result)
	return config, nil
}

// SetDeltaMode는 델타 중립 모드를 켜거나 끈다. 현재 상태는 UserSettingConfig의 DeltaEnabled로 확인한다.
func (client *Client) SetDeltaMode(
	ctx context.Context,
	request SetDeltaModeRequest,
	options ...trade.RequestOption,
) (MutationAck, error) {
	_, result, err := client.executeTradeMutation(
		ctx, "/v5/account/set-delta-mode", request.payload(), accountLimit(5), credential.PermissionTrade, options...,
	)
	if err != nil {
		return MutationAck{}, err
	}
	return MutationAck{Raw: cloneBytes(result)}, nil
}

// SpotTradeAnalysis는 Spot 심볼의 체결 집계와 일별 내역을 조회한다.
func (client *Client) SpotTradeAnalysis(
	ctx context.Context,
	request SpotTradeAnalysisRequest,
	options ...trade.RequestOption,
) (SpotTradeAnalysis, error) {
	if err := request.validate(); err != nil {
		return SpotTradeAnalysis{}, err
	}
	response, result, err := client.executeTradeRead(
		ctx, "/v5/account/trade-info-for-analysis", request.values(), accountLimit(50), options...,
	)
	if err != nil {
		return SpotTradeAnalysis{}, err
	}
	var analysis SpotTradeAnalysis
	if err := decodeResult(result, &analysis); err != nil {
		return SpotTradeAnalysis{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	analysis.Raw = cloneBytes(result)
	return analysis, nil
}

const (
	collateralSwitchOn  = "ON"
	collateralSwitchOff = "OFF"
)

// decodeListPageAs는 result.list를 항목별로 해석하며 오류 분류에 쓸 작업 종류를 호출자가 정한다.
func decodeListPageAs[T any](
	client *Client,
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	result json.RawMessage,
	setRaw func(item *T, raw json.RawMessage),
) (Category, []T, string, error) {
	var rawPage struct {
		Category       Category          `json:"category"`
		Items          []json.RawMessage `json:"list"`
		NextPageCursor string            `json:"nextPageCursor"`
	}
	if err := decodeResult(result, &rawPage); err != nil {
		return "", nil, "", client.decodeBodyError(response, operation, err)
	}
	items := make([]T, len(rawPage.Items))
	for index, raw := range rawPage.Items {
		if err := decodeResult(raw, &items[index]); err != nil {
			return "", nil, "", client.decodeBodyError(response, operation, err)
		}
		setRaw(&items[index], cloneBytes(raw))
	}
	return rawPage.Category, items, rawPage.NextPageCursor, nil
}

type collateralSwitchPayload struct {
	Coin             string `json:"coin"`
	CollateralSwitch string `json:"collateralSwitch"`
}

func collateralSwitchValue(enabled bool) string {
	if enabled {
		return collateralSwitchOn
	}
	return collateralSwitchOff
}

func (request SetCollateralSwitchRequest) payload() collateralSwitchPayload {
	return collateralSwitchPayload{Coin: request.Coin, CollateralSwitch: collateralSwitchValue(request.Enabled)}
}

func (request BatchSetCollateralSwitchRequest) payload() struct {
	Request []collateralSwitchPayload `json:"request"`
} {
	items := make([]collateralSwitchPayload, len(request.Items))
	for index, item := range request.Items {
		items[index] = collateralSwitchPayload{Coin: item.Coin, CollateralSwitch: collateralSwitchValue(item.Enabled)}
	}
	return struct {
		Request []collateralSwitchPayload `json:"request"`
	}{Request: items}
}

func (request SetMarginModeRequest) payload() struct {
	SetMarginMode MarginMode `json:"setMarginMode"`
} {
	return struct {
		SetMarginMode MarginMode `json:"setMarginMode"`
	}{SetMarginMode: request.Mode}
}

func (request BorrowRequest) payload() struct {
	Coin   string `json:"coin"`
	Amount string `json:"amount"`
} {
	return struct {
		Coin   string `json:"coin"`
		Amount string `json:"amount"`
	}{Coin: request.Coin, Amount: request.Amount}
}

func (request RepayRequest) payload() struct {
	Coin          string        `json:"coin,omitempty"`
	Amount        string        `json:"amount,omitempty"`
	RepaymentType RepaymentType `json:"repaymentType,omitempty"`
} {
	return struct {
		Coin          string        `json:"coin,omitempty"`
		Amount        string        `json:"amount,omitempty"`
		RepaymentType RepaymentType `json:"repaymentType,omitempty"`
	}{Coin: request.Coin, Amount: request.Amount, RepaymentType: request.RepaymentType}
}

func (request RepayLiabilityRequest) payload() struct {
	Coin string `json:"coin,omitempty"`
} {
	return struct {
		Coin string `json:"coin,omitempty"`
	}{Coin: request.Coin}
}

func (request SetDeltaModeRequest) payload() struct {
	DeltaEnable string `json:"deltaEnable"`
} {
	value := "0"
	if request.Enabled {
		value = "1"
	}
	return struct {
		DeltaEnable string `json:"deltaEnable"`
	}{DeltaEnable: value}
}

func (request FeeRatesRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	return validateOptionalText("symbol", request.Symbol)
}

func (request FeeRatesRequest) values() url.Values {
	values := categoryValues(request.Category)
	setIfNotEmpty(values, "symbol", request.Symbol)
	return values
}

func (request TransactionLogRequest) validate() error {
	if request.AccountType != "" && request.AccountType != AccountTypeUnified {
		return validationError("account type must be UNIFIED or empty for default")
	}
	if request.Category != "" {
		if err := validateCategory(request.Category); err != nil {
			return err
		}
	}
	if err := validateOptionalText("currency", request.Currency); err != nil {
		return err
	}
	if err := validateOptionalText("baseCoin", request.BaseCoin); err != nil {
		return err
	}
	if err := validateOptionalText("type", request.Type); err != nil {
		return err
	}
	if err := validateWindow("transaction log", request.StartTime, request.EndTime, transactionLogWindow); err != nil {
		return err
	}
	return validatePage(request.Limit, 50, request.Cursor)
}

func (request TransactionLogRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "accountType", string(request.AccountType))
	setIfNotEmpty(values, "category", string(request.Category))
	setIfNotEmpty(values, "currency", request.Currency)
	setIfNotEmpty(values, "baseCoin", request.BaseCoin)
	setIfNotEmpty(values, "type", request.Type)
	setTime(values, "startTime", request.StartTime)
	setTime(values, "endTime", request.EndTime)
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "cursor", request.Cursor)
	return values
}

func (request BorrowHistoryRequest) validate() error {
	if err := validateOptionalText("currency", request.Currency); err != nil {
		return err
	}
	if err := validateWindow("borrow history", request.StartTime, request.EndTime, borrowHistoryWindow); err != nil {
		return err
	}
	return validatePage(request.Limit, 50, request.Cursor)
}

func (request BorrowHistoryRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "currency", request.Currency)
	setTime(values, "startTime", request.StartTime)
	setTime(values, "endTime", request.EndTime)
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "cursor", request.Cursor)
	return values
}

func (request CollateralInfoRequest) validate() error {
	return validateOptionalText("currency", request.Currency)
}

func (request CollateralInfoRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "currency", request.Currency)
	return values
}

func (request SetCollateralSwitchRequest) validate() error {
	if err := validateRequiredText("coin", request.Coin); err != nil {
		return err
	}
	if !request.Enabled && (request.Coin == "USDT" || request.Coin == "USDC") {
		return validationError("%s collateral cannot be switched off", request.Coin)
	}
	return nil
}

func (request BatchSetCollateralSwitchRequest) validate() error {
	if len(request.Items) == 0 {
		return validationError("batch collateral switch requires at least one coin")
	}
	seen := make(map[string]struct{}, len(request.Items))
	for _, item := range request.Items {
		if err := (SetCollateralSwitchRequest{Coin: item.Coin, Enabled: item.Enabled}).validate(); err != nil {
			return err
		}
		if _, exists := seen[item.Coin]; exists {
			return validationError("duplicate coin %q", item.Coin)
		}
		seen[item.Coin] = struct{}{}
	}
	return nil
}

func (request SetMarginModeRequest) validate() error {
	switch request.Mode {
	case MarginModeIsolated, MarginModeRegular, MarginModePortfolio:
		return nil
	default:
		return validationError("margin mode must be ISOLATED_MARGIN, REGULAR_MARGIN, or PORTFOLIO_MARGIN")
	}
}

func (request BorrowRequest) validate() error {
	if err := validateRequiredText("coin", request.Coin); err != nil {
		return err
	}
	return validatePositiveDecimal("amount", request.Amount)
}

func (request RepayRequest) validate() error {
	if err := validateOptionalText("coin", request.Coin); err != nil {
		return err
	}
	switch request.RepaymentType {
	case "", RepaymentTypeAll, RepaymentTypeFixed, RepaymentTypeFlexible:
	default:
		return validationError("repayment type must be ALL, FIXED, or FLEXIBLE")
	}
	if request.Coin == "" {
		if request.Amount != "" {
			return validationError("amount requires coin")
		}
		if request.RepaymentType != RepaymentTypeAll {
			return validationError("repaying all liabilities without coin requires repayment type ALL")
		}
		return nil
	}
	if request.Amount != "" {
		return validatePositiveDecimal("amount", request.Amount)
	}
	return nil
}

func (request RepayLiabilityRequest) validate() error {
	return validateOptionalText("coin", request.Coin)
}

func (request PayInfoRequest) validate() error {
	return validateOptionalText("coin", request.Coin)
}

func (request PayInfoRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "coin", request.Coin)
	return values
}

func (request SpotTradeAnalysisRequest) validate() error {
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	return validateTimeOrder("spot trade analysis", request.StartTime, request.EndTime)
}

func (request SpotTradeAnalysisRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	setTime(values, "startTime", request.StartTime)
	setTime(values, "endTime", request.EndTime)
	return values
}

// validateWindow는 시작·종료 시각의 순서와, 둘 다 지정했을 때의 최대 범위를 확인한다.
func validateWindow(name string, start, end *time.Time, maximum time.Duration) error {
	if err := validateTimeOrder(name, start, end); err != nil {
		return err
	}
	if start != nil && end != nil && end.Sub(*start) > maximum {
		return validationError("%s time range cannot exceed %d days", name, int(maximum/(24*time.Hour)))
	}
	return nil
}
