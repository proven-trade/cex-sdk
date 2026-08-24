package futures

import (
	"encoding/base64"
	"testing"
)

func TestSignAuthentKnownVector(t *testing.T) {
	t.Parallel()

	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	signature, err := SignAuthent(
		"limitPrice=40000&orderType=lmt&side=buy&size=1&symbol=PI_XBTUSD",
		"1700",
		"/api/v3/sendorder",
		secret,
	)
	if err != nil {
		t.Fatalf("SignAuthent() error = %v", err)
	}
	const expected = "ZIQeXbAPJt/PhxcVVgznvq9DHyHB1JttG9iMYo8qMb2Co0y8zDoBwXTOGEAng6K6nrKWeP6exofWlXeEfwXlPQ=="
	if signature != expected {
		t.Fatalf("SignAuthent() = %q, want %q", signature, expected)
	}
}

func TestSignAuthentRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		nonce  string
		path   string
		secret []byte
	}{
		{name: "nonce 없음", path: "/api/v3/accounts", secret: []byte("dGVzdA==")},
		{name: "경로 형식 오류", nonce: "1", path: "/derivatives/api/v3/accounts", secret: []byte("dGVzdA==")},
		{name: "secret 형식 오류", nonce: "1", path: "/api/v3/accounts", secret: []byte("%%")},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if _, err := SignAuthent("", testCase.nonce, testCase.path, testCase.secret); err == nil {
				t.Fatal("SignAuthent() error = nil")
			}
		})
	}
}
