package okx

import (
	"errors"
	trade "github.com/proven-trade/cex-sdk/v2"
	commonexchange "github.com/proven-trade/cex-sdk/v2/exchange"
	"testing"
)

func TestNestedMutationErrorObservation(t *testing.T) {
	client := &Client{}
	err := client.classifyResponse(commonexchange.Response{StatusCode: 200, Body: []byte(`{"code":"0","data":[{"sCode":"51008","sMsg":"insufficient balance"}]}`)}, commonexchange.OperationMutation)
	var apiErr *trade.APIError
	if !errors.As(err, &apiErr) || apiErr.Category != trade.ErrorInsufficientBalance {
		t.Fatalf("classification=%v", err)
	}
}
