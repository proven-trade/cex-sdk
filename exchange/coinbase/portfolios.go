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

// PortfolioType은 포트폴리오 분류다.
type PortfolioType string

const (
	PortfolioTypeUndefined PortfolioType = "UNDEFINED"
	PortfolioTypeDefault   PortfolioType = "DEFAULT"
	PortfolioTypeConsumer  PortfolioType = "CONSUMER"
	PortfolioTypeINTX      PortfolioType = "INTX"
)

// Portfolio는 계정을 묶는 포트폴리오의 식별 정보다.
type Portfolio struct {
	Name    string          `json:"name"`
	UUID    string          `json:"uuid"`
	Type    PortfolioType   `json:"type"`
	Deleted bool            `json:"deleted"`
	Raw     json.RawMessage `json:"-"`
}

// PortfoliosRequest는 포트폴리오 목록 조회 조건이다.
// PortfolioType이 비어 있으면 모든 유형을 조회한다.
type PortfoliosRequest struct {
	PortfolioType PortfolioType
}

// CreatePortfolioRequest는 포트폴리오 생성 요청이다.
type CreatePortfolioRequest struct {
	Name string `json:"name"`
}

// EditPortfolioRequest는 포트폴리오 이름 수정 요청이다.
type EditPortfolioRequest struct {
	PortfolioUUID string `json:"-"`
	Name          string `json:"name"`
}

// PortfolioBreakdownRequest는 포트폴리오 구성 조회 조건이다.
// Currency가 비어 있으면 거래소 기본 통화로 환산한다.
type PortfolioBreakdownRequest struct {
	PortfolioUUID string
	Currency      string
}

// PortfolioBalances는 포트폴리오 잔고 요약이다.
type PortfolioBalances struct {
	TotalBalance               Amount `json:"total_balance"`
	TotalFuturesBalance        Amount `json:"total_futures_balance"`
	TotalCashEquivalentBalance Amount `json:"total_cash_equivalent_balance"`
	TotalCryptoBalance         Amount `json:"total_crypto_balance"`
	FuturesUnrealizedPnL       Amount `json:"futures_unrealized_pnl"`
	PerpUnrealizedPnL          Amount `json:"perp_unrealized_pnl"`
	TotalEquitiesBalance       Amount `json:"total_equities_balance"`
}

// PortfolioPosition은 포트폴리오의 Spot 자산 한 건이다.
// 수치 필드는 JSON 숫자 원문을 보존한다.
type PortfolioPosition struct {
	Asset                     string      `json:"asset"`
	AccountUUID               string      `json:"account_uuid"`
	TotalBalanceFiat          json.Number `json:"total_balance_fiat"`
	TotalBalanceCrypto        json.Number `json:"total_balance_crypto"`
	AvailableToTradeFiat      json.Number `json:"available_to_trade_fiat"`
	AvailableToTradeCrypto    json.Number `json:"available_to_trade_crypto"`
	AvailableToTransferFiat   json.Number `json:"available_to_transfer_fiat"`
	AvailableToTransferCrypto json.Number `json:"available_to_transfer_crypto"`
	Allocation                json.Number `json:"allocation"`
	UnrealizedPnL             json.Number `json:"unrealized_pnl"`
	CostBasis                 Amount      `json:"cost_basis"`
	AverageEntryPrice         Amount      `json:"average_entry_price"`
	AssetUUID                 string      `json:"asset_uuid"`
	AccountType               string      `json:"account_type"`
	IsCash                    bool        `json:"is_cash"`
}

// PortfolioBreakdown은 포트폴리오의 잔고 요약과 Spot 자산 목록이다.
// perp_positions, futures_positions, equity_positions 등은 Raw의 breakdown 객체에서 읽는다.
type PortfolioBreakdown struct {
	Portfolio     Portfolio
	Balances      PortfolioBalances
	SpotPositions []PortfolioPosition
	Raw           json.RawMessage
}

// MovePortfolioFundsRequest는 포트폴리오 사이 자금 이동 요청이다.
type MovePortfolioFundsRequest struct {
	Funds               Amount `json:"funds"`
	SourcePortfolioUUID string `json:"source_portfolio_uuid"`
	TargetPortfolioUUID string `json:"target_portfolio_uuid"`
}

// PortfolioFundsTransfer는 자금 이동이 접수된 출발·도착 포트폴리오다.
type PortfolioFundsTransfer struct {
	SourcePortfolioUUID string          `json:"source_portfolio_uuid"`
	TargetPortfolioUUID string          `json:"target_portfolio_uuid"`
	Raw                 json.RawMessage `json:"-"`
}

func (request PortfoliosRequest) validate() error {
	switch request.PortfolioType {
	case "", PortfolioTypeUndefined, PortfolioTypeDefault, PortfolioTypeConsumer, PortfolioTypeINTX:
		return nil
	default:
		return validationError("unsupported portfolio type %q", request.PortfolioType)
	}
}

func (request PortfoliosRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "portfolio_type", string(request.PortfolioType))
	return values
}

func (request CreatePortfolioRequest) validate() error {
	return validateRequiredText("portfolio name", request.Name)
}

func (request EditPortfolioRequest) validate() error {
	if _, err := escapePathSegment("portfolio UUID", request.PortfolioUUID); err != nil {
		return err
	}
	return validateRequiredText("portfolio name", request.Name)
}

func (request PortfolioBreakdownRequest) validate() error {
	if _, err := escapePathSegment("portfolio UUID", request.PortfolioUUID); err != nil {
		return err
	}
	return validateOptionalText("currency", request.Currency)
}

func (request PortfolioBreakdownRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "currency", request.Currency)
	return values
}

func (request MovePortfolioFundsRequest) validate() error {
	if !isPositiveDecimal(request.Funds.Value) {
		return validationError("portfolio funds value must be a positive decimal string")
	}
	if err := validateRequiredText("funds currency", request.Funds.Currency); err != nil {
		return err
	}
	if err := validateRequiredText("source portfolio UUID", request.SourcePortfolioUUID); err != nil {
		return err
	}
	if err := validateRequiredText("target portfolio UUID", request.TargetPortfolioUUID); err != nil {
		return err
	}
	if request.SourcePortfolioUUID == request.TargetPortfolioUUID {
		return validationError("source and target portfolio must differ")
	}
	return nil
}

// Portfolios는 사용자의 포트폴리오 목록을 조회한다.
func (client *Client) Portfolios(
	ctx context.Context,
	request PortfoliosRequest,
	options ...trade.RequestOption,
) ([]Portfolio, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/portfolios", request.values(), nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var page struct {
		Portfolios []json.RawMessage `json:"portfolios"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &page); err != nil {
		return nil, err
	}
	portfolios := make([]Portfolio, len(page.Portfolios))
	for index, raw := range page.Portfolios {
		if err := json.Unmarshal(raw, &portfolios[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		portfolios[index].Raw = cloneBytes(raw)
	}
	return portfolios, nil
}

// CreatePortfolio는 새 포트폴리오를 생성한다.
// 포트폴리오 관리는 SDK 권한 모델에서 서브계정 관리에 해당하므로 PermissionTransfer가 필요하다.
func (client *Client) CreatePortfolio(
	ctx context.Context,
	request CreatePortfolioRequest,
	options ...trade.RequestOption,
) (Portfolio, error) {
	if err := request.validate(); err != nil {
		return Portfolio{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return Portfolio{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, publicPrefix+"/portfolios", nil, body,
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Portfolio{}, err
	}
	return client.decodePortfolioEnvelope(response)
}

// EditPortfolio는 포트폴리오 이름을 수정한다.
// 포트폴리오 관리는 SDK 권한 모델에서 서브계정 관리에 해당하므로 PermissionTransfer가 필요하다.
func (client *Client) EditPortfolio(
	ctx context.Context,
	request EditPortfolioRequest,
	options ...trade.RequestOption,
) (Portfolio, error) {
	if err := request.validate(); err != nil {
		return Portfolio{}, err
	}
	segment, err := escapePathSegment("portfolio UUID", request.PortfolioUUID)
	if err != nil {
		return Portfolio{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return Portfolio{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPut, publicPrefix+"/portfolios/"+segment, nil, body,
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Portfolio{}, err
	}
	return client.decodePortfolioEnvelope(response)
}

// DeletePortfolio는 포트폴리오를 삭제한다.
// 포트폴리오 관리는 SDK 권한 모델에서 서브계정 관리에 해당하므로 PermissionTransfer가 필요하다.
func (client *Client) DeletePortfolio(
	ctx context.Context,
	portfolioUUID string,
	options ...trade.RequestOption,
) error {
	segment, err := escapePathSegment("portfolio UUID", portfolioUUID)
	if err != nil {
		return err
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, publicPrefix+"/portfolios/"+segment, nil, nil,
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return err
	}
	var result struct{}
	return client.decodeSuccess(response, commonexchange.OperationMutation, &result)
}

// PortfolioBreakdown은 포트폴리오의 잔고 요약과 Spot 자산 구성을 조회한다.
func (client *Client) PortfolioBreakdown(
	ctx context.Context,
	request PortfolioBreakdownRequest,
	options ...trade.RequestOption,
) (PortfolioBreakdown, error) {
	if err := request.validate(); err != nil {
		return PortfolioBreakdown{}, err
	}
	segment, err := escapePathSegment("portfolio UUID", request.PortfolioUUID)
	if err != nil {
		return PortfolioBreakdown{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/portfolios/"+segment, request.values(), nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return PortfolioBreakdown{}, err
	}
	var envelope struct {
		Breakdown json.RawMessage `json:"breakdown"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return PortfolioBreakdown{}, err
	}
	var decoded struct {
		Portfolio     Portfolio           `json:"portfolio"`
		Balances      PortfolioBalances   `json:"portfolio_balances"`
		SpotPositions []PortfolioPosition `json:"spot_positions"`
	}
	if err := json.Unmarshal(envelope.Breakdown, &decoded); err != nil {
		return PortfolioBreakdown{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return PortfolioBreakdown{
		Portfolio: decoded.Portfolio, Balances: decoded.Balances,
		SpotPositions: decoded.SpotPositions, Raw: cloneBytes(envelope.Breakdown),
	}, nil
}

// MovePortfolioFunds는 한 포트폴리오에서 다른 포트폴리오로 자금을 이동한다.
// 계정 사이 이체이므로 PermissionTransfer가 필요하고 자동 재시도하지 않는다.
func (client *Client) MovePortfolioFunds(
	ctx context.Context,
	request MovePortfolioFundsRequest,
	options ...trade.RequestOption,
) (PortfolioFundsTransfer, error) {
	if err := request.validate(); err != nil {
		return PortfolioFundsTransfer{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return PortfolioFundsTransfer{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, publicPrefix+"/portfolios/move_funds", nil, body,
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return PortfolioFundsTransfer{}, err
	}
	var transfer PortfolioFundsTransfer
	if err := client.decodeSuccess(response, commonexchange.OperationMutation, &transfer); err != nil {
		return PortfolioFundsTransfer{}, err
	}
	transfer.Raw = cloneBytes(response.Body)
	return transfer, nil
}

func (client *Client) decodePortfolioEnvelope(response commonexchange.Response) (Portfolio, error) {
	var envelope struct {
		Portfolio json.RawMessage `json:"portfolio"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationMutation, &envelope); err != nil {
		return Portfolio{}, err
	}
	var portfolio Portfolio
	if err := json.Unmarshal(envelope.Portfolio, &portfolio); err != nil {
		return Portfolio{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	portfolio.Raw = cloneBytes(envelope.Portfolio)
	return portfolio, nil
}
