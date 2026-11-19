package coinbase

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// TradeIncentiveMetadata는 전환 수수료 면제에 쓰는 인센티브 정보다.
type TradeIncentiveMetadata struct {
	UserIncentiveID string `json:"user_incentive_id,omitempty"`
	CodeVal         string `json:"code_val,omitempty"`
}

// ConvertQuoteRequest는 스테이블코인 전환 견적 요청이다.
// FromAccount와 ToAccount는 전환 출발·도착 계정이며 Amount는 FromAccount 기준 수량이다.
type ConvertQuoteRequest struct {
	FromAccount            string                  `json:"from_account"`
	ToAccount              string                  `json:"to_account"`
	Amount                 string                  `json:"amount"`
	TradeIncentiveMetadata *TradeIncentiveMetadata `json:"trade_incentive_metadata,omitempty"`
}

// ConvertTradeRequest는 견적 ID로 전환 거래를 확정하거나 조회하는 요청이다.
type ConvertTradeRequest struct {
	TradeID     string
	FromAccount string
	ToAccount   string
}

// ConvertFee는 전환 거래의 수수료 항목이다.
type ConvertFee struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Amount      Amount `json:"amount"`
	Label       string `json:"label"`
}

// ConvertTrade는 전환 견적 또는 확정된 전환 거래다.
// source, target, unit_price, user_warnings 같은 중첩 필드는 Raw에서 읽는다.
type ConvertTrade struct {
	ID                string          `json:"id"`
	Status            string          `json:"status"`
	UserEnteredAmount Amount          `json:"user_entered_amount"`
	Amount            Amount          `json:"amount"`
	Subtotal          Amount          `json:"subtotal"`
	Total             Amount          `json:"total"`
	Fees              []ConvertFee    `json:"fees"`
	TotalFee          ConvertFee      `json:"total_fee"`
	SourceCurrency    string          `json:"source_currency"`
	TargetCurrency    string          `json:"target_currency"`
	SourceID          string          `json:"source_id"`
	TargetID          string          `json:"target_id"`
	UserReference     string          `json:"user_reference"`
	ExchangeRate      Amount          `json:"exchange_rate"`
	FiatDenotedTotal  Amount          `json:"fiat_denoted_total"`
	Raw               json.RawMessage `json:"-"`
}

func (request ConvertQuoteRequest) validate() error {
	if err := validateRequiredText("from account", request.FromAccount); err != nil {
		return err
	}
	if err := validateRequiredText("to account", request.ToAccount); err != nil {
		return err
	}
	if !isPositiveDecimal(request.Amount) {
		return validationError("convert amount must be a positive decimal string")
	}
	if metadata := request.TradeIncentiveMetadata; metadata != nil {
		if err := validateOptionalText("user incentive ID", metadata.UserIncentiveID); err != nil {
			return err
		}
		return validateOptionalText("promo code", metadata.CodeVal)
	}
	return nil
}

func (request ConvertTradeRequest) validate() error {
	if _, err := escapePathSegment("trade ID", request.TradeID); err != nil {
		return err
	}
	if err := validateRequiredText("from account", request.FromAccount); err != nil {
		return err
	}
	return validateRequiredText("to account", request.ToAccount)
}

// ConvertQuote는 스테이블코인 전환 견적을 생성한다.
// 견적만으로는 자산이 이동하지 않으며 CommitConvertTrade로 확정해야 체결된다.
// 전환은 계정 변경이므로 PermissionTransfer가 필요하고 자동 재시도하지 않는다.
func (client *Client) ConvertQuote(
	ctx context.Context,
	request ConvertQuoteRequest,
	options ...trade.RequestOption,
) (ConvertTrade, error) {
	if err := request.validate(); err != nil {
		return ConvertTrade{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return ConvertTrade{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, publicPrefix+"/convert/quote", nil, body,
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return ConvertTrade{}, err
	}
	return client.decodeConvertTrade(response, commonexchange.OperationMutation)
}

// CommitConvertTrade는 견적으로 만든 전환 거래를 확정한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환하므로
// ConvertTrade로 거래 상태를 확인해야 한다.
func (client *Client) CommitConvertTrade(
	ctx context.Context,
	request ConvertTradeRequest,
	options ...trade.RequestOption,
) (ConvertTrade, error) {
	if err := request.validate(); err != nil {
		return ConvertTrade{}, err
	}
	segment, err := escapePathSegment("trade ID", request.TradeID)
	if err != nil {
		return ConvertTrade{}, err
	}
	body, err := encodeBody(map[string]string{
		"from_account": request.FromAccount, "to_account": request.ToAccount,
	})
	if err != nil {
		return ConvertTrade{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, publicPrefix+"/convert/trade/"+segment, nil, body,
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return ConvertTrade{}, err
	}
	return client.decodeConvertTrade(response, commonexchange.OperationMutation)
}

// ConvertTrade는 전환 거래 ID로 견적 또는 체결 상태를 조회한다.
func (client *Client) ConvertTrade(
	ctx context.Context,
	request ConvertTradeRequest,
	options ...trade.RequestOption,
) (ConvertTrade, error) {
	if err := request.validate(); err != nil {
		return ConvertTrade{}, err
	}
	segment, err := escapePathSegment("trade ID", request.TradeID)
	if err != nil {
		return ConvertTrade{}, err
	}
	query := url.Values{
		"from_account": {request.FromAccount}, "to_account": {request.ToAccount},
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/convert/trade/"+segment, query, nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return ConvertTrade{}, err
	}
	return client.decodeConvertTrade(response, commonexchange.OperationRead)
}

func (client *Client) decodeConvertTrade(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) (ConvertTrade, error) {
	var envelope struct {
		Trade json.RawMessage `json:"trade"`
	}
	if err := client.decodeSuccess(response, operation, &envelope); err != nil {
		return ConvertTrade{}, err
	}
	var convertTrade ConvertTrade
	if err := json.Unmarshal(envelope.Trade, &convertTrade); err != nil {
		return ConvertTrade{}, client.decodeBodyError(response, operation, err)
	}
	convertTrade.Raw = cloneBytes(envelope.Trade)
	return convertTrade, nil
}
