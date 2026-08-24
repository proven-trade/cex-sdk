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

func TestSignChallengeOfficialVector(t *testing.T) {
	t.Parallel()

	const challenge = "c100b894-1729-464d-ace1-52dbce11db42"
	secret := []byte("7zxMEF5p/Z8l2p2U7Ghv6x14Af+Fx+92tPgUdVQ748FOIrEoT9bgT+bTRfXc5pz8na+hL/QdrCVG7bh9KpT0eMTm")
	signature, err := SignChallenge(challenge, secret)
	if err != nil {
		t.Fatalf("SignChallenge() error = %v", err)
	}
	const expected = "4JEpF3ix66GA2B+ooK128Ift4XQVtc137N9yeg4Kqsn9PI0Kpzbysl9M1IeCEdjg0zl00wkVqcsnG4bmnlMb3A=="
	if signature != expected {
		t.Fatalf("SignChallenge() = %q, want %q", signature, expected)
	}
}

func TestSignChallengeRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	if _, err := SignChallenge(" bad\n", []byte("dGVzdA==")); err == nil {
		t.Fatal("SignChallenge() challenge error = nil")
	}
	if _, err := SignChallenge("challenge", []byte("%%")); err == nil {
		t.Fatal("SignChallenge() secret error = nil")
	}
}
