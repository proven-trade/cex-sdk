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

// BestBidAskRequest는 여러 상품의 최우선 호가 일괄 조회 조건이다.
// ProductIDs가 비어 있으면 거래 가능한 모든 상품을 조회한다.
type BestBidAskRequest struct {
	ProductIDs []string
}

// BestBidAsk는 상품별 최우선 호가 목록이다.
type BestBidAsk struct {
	PriceBooks []PriceBook
	Raw        json.RawMessage
}

func (request BestBidAskRequest) validate() error {
	return validateTextList("product ID", request.ProductIDs, 100)
}

func (request BestBidAskRequest) values() url.Values {
	values := make(url.Values)
	addAll(values, "product_ids", request.ProductIDs)
	return values
}

// BestBidAsk는 여러 상품의 최우선 매수·매도 호가를 한 번에 조회한다.
// 공개 market 엔드포인트와 달리 서명된 읽기 요청이며 PermissionRead가 필요하다.
func (client *Client) BestBidAsk(
	ctx context.Context,
	request BestBidAskRequest,
	options ...trade.RequestOption,
) (BestBidAsk, error) {
	if err := request.validate(); err != nil {
		return BestBidAsk{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/best_bid_ask", request.values(), nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return BestBidAsk{}, err
	}
	var result struct {
		PriceBooks []PriceBook `json:"pricebooks"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &result); err != nil {
		return BestBidAsk{}, err
	}
	return BestBidAsk{PriceBooks: result.PriceBooks, Raw: cloneBytes(response.Body)}, nil
}
