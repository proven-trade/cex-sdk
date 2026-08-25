package cryptocom

import (
	"encoding/json"
	"maps"
	"testing"
)

func TestSignOfficialOrderDetailVector(t *testing.T) {
	t.Parallel()
	signature, err := Sign(
		"private/get-order-detail", "11", []byte("token"),
		map[string]any{"order_id": "53287421324"}, "1587846358253", []byte("secretKey"),
	)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	const want = "02ef0a52c9428e5d3dcc5dd24d534ca39ef73f35acd3f6945f139a2364ef67a9"
	if signature != want {
		t.Fatalf("Sign() = %q, want %q", signature, want)
	}
}

func TestSignNestedOrderListVector(t *testing.T) {
	t.Parallel()
	params := map[string]any{
		"order_list": []any{
			map[string]any{
				"side": "BUY", "quantity": "1.0", "price": "0.24",
				"instrument_name": "ONE_USDT", "type": "LIMIT",
			},
			map[string]any{
				"type": "STOP_LIMIT", "trigger_price": "0.26", "side": "BUY",
				"quantity": "1.0", "instrument_name": "ONE_USDT", "price": "0.27",
			},
		},
		"contingency_type": "LIST",
	}
	parameterString, err := ParamsString(params)
	if err != nil {
		t.Fatalf("ParamsString() error = %v", err)
	}
	const wantParams = "contingency_typeLISTorder_list" +
		"instrument_nameONE_USDTprice0.24quantity1.0sideBUYtypeLIMIT" +
		"instrument_nameONE_USDTprice0.27quantity1.0sideBUYtrigger_price0.26typeSTOP_LIMIT"
	if parameterString != wantParams {
		t.Fatalf("ParamsString() = %q, want %q", parameterString, wantParams)
	}
	signature, err := Sign(
		"private/create-order-list", "14", []byte("API_KEY"), params,
		"1700000000000", []byte("SECRET_KEY"),
	)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	const wantSignature = "b0e801c90469998d677b30874a05a6fb07c6a4df311fd84552893114b31afc9a"
	if signature != wantSignature {
		t.Fatalf("Sign() = %q, want %q", signature, wantSignature)
	}
	if !maps.Equal(params["order_list"].([]any)[0].(map[string]any), map[string]any{
		"side": "BUY", "quantity": "1.0", "price": "0.24",
		"instrument_name": "ONE_USDT", "type": "LIMIT",
	}) {
		t.Fatal("Sign() changed nested parameters")
	}
}

func TestSignerRejectsMalformedInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
	}{
		{name: "numeric parameter", err: func() error {
			_, err := ParamsString(map[string]any{"limit": 10})
			return err
		}()},
		{name: "JSON numeric parameter", err: func() error {
			_, err := ParamsString(map[string]any{"limit": json.Number("10")})
			return err
		}()},
		{name: "negative ID", err: func() error {
			_, err := Sign("private/test", "-1", []byte("key"), nil, "1", []byte("secret"))
			return err
		}()},
		{name: "zero nonce", err: func() error {
			_, err := Sign("private/test", "1", []byte("key"), nil, "0", []byte("secret"))
			return err
		}()},
		{name: "missing secret", err: func() error {
			_, err := Sign("private/test", "1", []byte("key"), nil, "1", nil)
			return err
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.err == nil {
				t.Fatal("signer error = nil")
			}
		})
	}
}
