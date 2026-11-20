package cryptocom

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	methodGetPositions          = "private/get-positions"
	methodGetTransactions       = "private/get-transactions"
	methodGetSubaccountBalances = "private/get-subaccount-balances"
)

// JournalType은 private/get-transactions의 거래·정산 내역 종류다.
type JournalType string

const (
	JournalTrading             JournalType = "TRADING"
	JournalTradeFee            JournalType = "TRADE_FEE"
	JournalOnchainWithdrawal   JournalType = "ONCHAIN_WITHDRAWAL"
	JournalOnchainDeposit      JournalType = "ONCHAIN_DEPOSIT"
	JournalFunding             JournalType = "FUNDING"
	JournalRealizedPNL         JournalType = "REALIZED_PNL"
	JournalInsuranceFund       JournalType = "INSURANCE_FUND"
	JournalSocializedLoss      JournalType = "SOCIALIZED_LOSS"
	JournalLiquidationFee      JournalType = "LIQUIDATION_FEE"
	JournalSessionReset        JournalType = "SESSION_RESET"
	JournalAdjustment          JournalType = "ADJUSTMENT"
	JournalSessionSettle       JournalType = "SESSION_SETTLE"
	JournalUncoveredLoss       JournalType = "UNCOVERED_LOSS"
	JournalAdminAdjustment     JournalType = "ADMIN_ADJUSTMENT"
	JournalDelist              JournalType = "DELIST"
	JournalSettlementFee       JournalType = "SETTLEMENT_FEE"
	JournalAutoConversion      JournalType = "AUTO_CONVERSION"
	JournalManualConversion    JournalType = "MANUAL_CONVERSION"
	JournalSubaccountTx        JournalType = "SUBACCOUNT_TX"
	JournalFiatWithdrawCancel  JournalType = "FIAT_WITHDRAWAL_CANCEL"
	JournalMarginTradeInterest JournalType = "MARGIN_TRADE_INTEREST"
)

// PositionsRequest는 선택적 상품으로 계정 포지션을 필터링한다.
type PositionsRequest struct {
	InstrumentName string
}

// Position은 파생 상품 포지션 한 건이다.
type Position struct {
	AccountID        string          `json:"account_id"`
	InstrumentName   string          `json:"instrument_name"`
	Type             string          `json:"type"`
	Quantity         Decimal         `json:"quantity"`
	Cost             Decimal         `json:"cost"`
	OpenPositionPNL  Decimal         `json:"open_position_pnl"`
	OpenPositionCost Decimal         `json:"open_pos_cost"`
	SessionPNL       Decimal         `json:"session_pnl"`
	UpdateTime       Scalar          `json:"update_timestamp_ms"`
	IsolationID      string          `json:"isolation_id"`
	IsolationType    string          `json:"isolation_type"`
	Raw              json.RawMessage `json:"-"`
}

// TransactionsRequest는 계정 거래·정산 내역의 상품·종류·시간 범위·개수를 지정한다.
type TransactionsRequest struct {
	InstrumentName string
	JournalType    JournalType
	IsolationID    string
	Start          *time.Time
	End            *time.Time
	Limit          int
}

// Transaction은 거래·정산·입출금 등 계정 원장 내역 한 건이다.
type Transaction struct {
	AccountID        string          `json:"account_id"`
	EventDate        string          `json:"event_date"`
	JournalType      JournalType     `json:"journal_type"`
	JournalID        Scalar          `json:"journal_id"`
	TransactionQty   Decimal         `json:"transaction_qty"`
	TransactionCost  Decimal         `json:"transaction_cost"`
	RealizedPNL      Decimal         `json:"realized_pnl"`
	OrderID          Scalar          `json:"order_id"`
	TradeID          Scalar          `json:"trade_id"`
	TradeMatchID     Scalar          `json:"trade_match_id"`
	EventTimestamp   Scalar          `json:"event_timestamp_ms"`
	EventTimestampNS Scalar          `json:"event_timestamp_ns"`
	ClientOrderID    string          `json:"client_oid"`
	TakerSide        string          `json:"taker_side"`
	Side             string          `json:"side"`
	InstrumentName   string          `json:"instrument_name"`
	IsolationID      string          `json:"isolation_id"`
	IsolationType    string          `json:"isolation_type"`
	Raw              json.RawMessage `json:"-"`
}

func (value JournalType) valid() bool {
	switch value {
	case JournalTrading, JournalTradeFee, JournalOnchainWithdrawal, JournalOnchainDeposit,
		JournalFunding, JournalRealizedPNL, JournalInsuranceFund, JournalSocializedLoss,
		JournalLiquidationFee, JournalSessionReset, JournalAdjustment, JournalSessionSettle,
		JournalUncoveredLoss, JournalAdminAdjustment, JournalDelist, JournalSettlementFee,
		JournalAutoConversion, JournalManualConversion, JournalSubaccountTx,
		JournalFiatWithdrawCancel, JournalMarginTradeInterest:
		return true
	default:
		return false
	}
}

func (request PositionsRequest) validate() error {
	return validateOptionalSymbol(request.InstrumentName)
}

func (request PositionsRequest) params() map[string]any {
	params := make(map[string]any)
	setStringParam(params, "instrument_name", request.InstrumentName)
	return params
}

func (request TransactionsRequest) validate() error {
	if err := validateOptionalSymbol(request.InstrumentName); err != nil {
		return err
	}
	if request.JournalType != "" && !request.JournalType.valid() {
		return validationError("unsupported Crypto.com journal type %q", request.JournalType)
	}
	if request.IsolationID != "" {
		if err := validateNumericID("isolation ID", request.IsolationID); err != nil {
			return err
		}
	}
	return validatePrivateHistory(request.Start, request.End, request.Limit)
}

func (request TransactionsRequest) params() map[string]any {
	params := privateHistoryParams(request.InstrumentName, request.Start, request.End, request.Limit)
	setStringParam(params, "journal_type", string(request.JournalType))
	setStringParam(params, "isolation_id", request.IsolationID)
	return params
}

// Positions는 계정의 파생 상품 포지션을 조회한다. 권한은 read다.
func (client *Client) Positions(
	ctx context.Context,
	request PositionsRequest,
	options ...trade.RequestOption,
) ([]Position, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, requestID, resultRaw, err := client.callPrivate(
		ctx, methodGetPositions, request.params(), credential.PermissionRead,
		commonexchange.OperationRead, true, options...,
	)
	if err != nil {
		return nil, err
	}
	itemsRaw, err := client.decodePrivateDataItems(
		response, resultRaw, commonexchange.OperationRead, requestID,
	)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems(itemsRaw, func(item *Position, raw json.RawMessage) { item.Raw = raw })
	if err != nil {
		return nil, client.decodePrivateBodyError(response, commonexchange.OperationRead, requestID, err)
	}
	for _, item := range items {
		if item.InstrumentName == "" ||
			request.InstrumentName != "" && item.InstrumentName != request.InstrumentName {
			return nil, client.decodePrivateBodyError(
				response, commonexchange.OperationRead, requestID,
				errors.New("Crypto.com position instrument is missing or inconsistent"),
			)
		}
	}
	return items, nil
}

// Transactions는 계정 거래·정산·입출금 원장 내역을 조회한다. 권한은 read다.
func (client *Client) Transactions(
	ctx context.Context,
	request TransactionsRequest,
	options ...trade.RequestOption,
) ([]Transaction, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, requestID, resultRaw, err := client.callPrivate(
		ctx, methodGetTransactions, request.params(), credential.PermissionRead,
		commonexchange.OperationRead, true, options...,
	)
	if err != nil {
		return nil, err
	}
	itemsRaw, err := client.decodePrivateDataItems(
		response, resultRaw, commonexchange.OperationRead, requestID,
	)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems(itemsRaw, func(item *Transaction, raw json.RawMessage) { item.Raw = raw })
	if err != nil {
		return nil, client.decodePrivateBodyError(response, commonexchange.OperationRead, requestID, err)
	}
	for _, item := range items {
		if item.JournalType == "" {
			return nil, client.decodePrivateBodyError(
				response, commonexchange.OperationRead, requestID,
				errors.New("Crypto.com transaction journal type is missing"),
			)
		}
	}
	return items, nil
}

// SubaccountBalances는 모든 서브계정의 통화별 잔고와 위험 요약을 조회한다. 권한은 read다.
func (client *Client) SubaccountBalances(
	ctx context.Context,
	options ...trade.RequestOption,
) (UserBalance, error) {
	response, requestID, resultRaw, err := client.callPrivate(
		ctx, methodGetSubaccountBalances, nil, credential.PermissionRead,
		commonexchange.OperationRead, true, options...,
	)
	if err != nil {
		return UserBalance{}, err
	}
	itemsRaw, err := client.decodePrivateDataItems(
		response, resultRaw, commonexchange.OperationRead, requestID,
	)
	if err != nil {
		return UserBalance{}, err
	}
	value := UserBalance{
		Accounts: make([]BalanceAccount, len(itemsRaw)), Raw: cloneBytes(resultRaw),
	}
	for index, itemRaw := range itemsRaw {
		account, err := client.decodeBalanceAccount(response, requestID, itemRaw)
		if err != nil {
			return UserBalance{}, err
		}
		value.Accounts[index] = account
	}
	return value, nil
}
