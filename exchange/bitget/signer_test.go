package bitget

import "testing"

func TestSignHMACSHA256OfficialPreHashExample(t *testing.T) {
	t.Parallel()

	preHash := signaturePayload(
		"16273667805456",
		"GET",
		"/api/v3/account/fee-rate",
		"category=SPOT&symbol=BTCUSDT",
		nil,
	)
	wantPreHash := "16273667805456GET/api/v3/account/fee-rate?category=SPOT&symbol=BTCUSDT"
	if string(preHash) != wantPreHash {
		t.Fatalf("pre-hash = %q, want %q", preHash, wantPreHash)
	}
	signature, err := SignHMACSHA256([]byte("test-secret"), preHash)
	if err != nil {
		t.Fatalf("SignHMACSHA256() error = %v", err)
	}
	if signature != "TdABQQ9ijAseDaXVnIRMSNNiP8a2N3JfNvKnXjSB3EE=" {
		t.Fatalf("signature = %q", signature)
	}
}

func TestSignHMACSHA256RejectsEmptySecret(t *testing.T) {
	t.Parallel()

	if _, err := SignHMACSHA256(nil, []byte("payload")); err == nil {
		t.Fatal("SignHMACSHA256() error = nil, want an error")
	}
}
