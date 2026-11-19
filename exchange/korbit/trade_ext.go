package korbit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// PreventedMatchesRequest는 자전거래 방지(STP)가 중단시킨 체결 조회 조건이다.
// AccountSeq는 생략하면 BAD_REQUEST이므로 메인 계정도 1로 명시해야 한다.
// OrderID가 0이면 거래쌍의 모든 기록을 조회하고, Cursor는 이전 응답의 NextCursor다.
type PreventedMatchesRequest struct {
	Symbol     string
	AccountSeq int
	OrderID    int64
	Cursor     string
	Limit      int
}

// STPMode는 taker 주문에 지정된 자전거래 방지 방식이다.
type STPMode string

const (
	STPModeCancelTaker STPMode = "cancelTaker"
	STPModeCancelMaker STPMode = "cancelMaker"
	STPModeCancelBoth  STPMode = "cancelBoth"
)

// PreventedMatch는 자전거래 방지가 막은 taker·maker 주문 충돌 한 건이다.
// TakerPreventedQty·TakerPreventedAmt는 시장가 매수·매도처럼 값이 정해지지 않으면 빈 문자열이다.
type PreventedMatch struct {
	Symbol            string          `json:"symbol"`
	STP               STPMode         `json:"stp"`
	Price             string          `json:"price"`
	TakerOrderID      int64           `json:"takerOrderId"`
	MakerOrderID      int64           `json:"makerOrderId"`
	TakerAccountSeq   int             `json:"takerAccountSeq"`
	MakerAccountSeq   int             `json:"makerAccountSeq"`
	TakerClosed       bool            `json:"takerClosed"`
	MakerClosed       bool            `json:"makerClosed"`
	TakerPreventedQty string          `json:"takerPreventedQty"`
	MakerPreventedQty string          `json:"makerPreventedQty"`
	TakerPreventedAmt string          `json:"takerPreventedAmt"`
	MakerPreventedAmt string          `json:"makerPreventedAmt"`
	PreventedAt       int64           `json:"preventedAt"`
	Raw               json.RawMessage `json:"-"`
}

// PreventedMatchesPage는 최신순 STP 차단 내역 한 페이지다.
// NextCursor가 있으면 같은 조건에 Cursor로 넘겨 다음 페이지를 읽고, 빈 목록과 빈 NextCursor가 올 때까지 반복한다.
type PreventedMatchesPage struct {
	PreventedMatches []PreventedMatch
	NextCursor       string
	Raw              json.RawMessage
}

func (request PreventedMatchesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateRequiredAccountSeq("accountSeq", request.AccountSeq); err != nil {
		return err
	}
	if request.OrderID < 0 {
		return validationError("order ID cannot be negative")
	}
	return validateOptionalLimit("prevented match", request.Limit, 1000)
}

func (request PreventedMatchesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setRequiredAccountSeq(values, "accountSeq", request.AccountSeq)
	if request.OrderID > 0 {
		values.Set("orderId", strconv.FormatInt(request.OrderID, 10))
	}
	setIfNotEmpty(values, "cursor", request.Cursor)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

// MyPreventedMatches는 자전거래 방지(STP)가 중단시킨 taker·maker 충돌을 최신순으로 조회한다.
// 응답의 NextCursor는 같은 Symbol, OrderID, AccountSeq 조건에서만 유효하다.
func (client *Client) MyPreventedMatches(
	ctx context.Context,
	request PreventedMatchesRequest,
	options ...trade.RequestOption,
) (PreventedMatchesPage, error) {
	if err := request.validate(); err != nil {
		return PreventedMatchesPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/myPreventedMatches", request.values(), rateGroupPrivate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return PreventedMatchesPage{}, err
	}
	var wire struct {
		PreventedMatches []json.RawMessage `json:"preventedMatches"`
		NextCursor       string            `json:"nextCursor"`
	}
	raw, err := client.decodeResponse(response, commonexchange.OperationRead, &wire)
	if err != nil {
		return PreventedMatchesPage{}, err
	}
	matches := make([]PreventedMatch, len(wire.PreventedMatches))
	for index, rawMatch := range wire.PreventedMatches {
		if err := json.Unmarshal(rawMatch, &matches[index]); err != nil {
			return PreventedMatchesPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		matches[index].Raw = cloneBytes(rawMatch)
	}
	return PreventedMatchesPage{PreventedMatches: matches, NextCursor: wire.NextCursor, Raw: raw}, nil
}
