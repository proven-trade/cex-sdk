package binance

import "testing"

func TestSignHMACSHA256OfficialVector(t *testing.T) {
	t.Parallel()

	secret := []byte("NhqPtmdSJYdKjVHjA7PZj4Mge3R5YNiP1e3UZjInClVN65XAbvqqM6A7H5fATj0j")
	payload := []byte("symbol=LTCBTC&side=BUY&type=LIMIT&timeInForce=GTC&quantity=1&price=0.1&recvWindow=5000&timestamp=1499827319559")
	want := "c8db56825ae71d6d79447849e617115f4a920fa2acdcab2b053c4b2838bd6b71"

	got, err := SignHMACSHA256(secret, payload)
	if err != nil {
		t.Fatalf("SignHMACSHA256() error = %v", err)
	}
	if got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func TestSignHMACSHA256RejectsEmptySecret(t *testing.T) {
	t.Parallel()

	if _, err := SignHMACSHA256(nil, []byte("payload")); err == nil {
		t.Fatal("SignHMACSHA256() error = nil, want an error")
	}
}
