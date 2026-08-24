package kraken

import "testing"

func TestSignRESTMatchesOfficialVector(t *testing.T) {
	t.Parallel()

	signature, err := SignREST(
		"/0/private/AddOrder",
		"1616492376594",
		"nonce=1616492376594&ordertype=limit&pair=XBTUSD&price=37500&type=buy&volume=1.25",
		[]byte("kQH5HW/8p1uGOVjbgWA7FunAmGO8lsSUXNsu3eow76sz84Q18fWxnyRzBHCd3pd5nE9qa99HAZtuZuj6F1huXg=="),
	)
	if err != nil {
		t.Fatalf("SignREST() error = %v", err)
	}
	const expected = "4/dpxb3iT4tp/ZCVEwSnEsLxx0bqyhLpdfOpc6fn7OR8+UClSV5n9E6aSS8MPtnRfp32bAb0nmbRn6H8ndwLUQ=="
	if signature != expected {
		t.Fatalf("SignREST() = %q, want %q", signature, expected)
	}
}

func TestSignRESTRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		path   string
		nonce  string
		secret []byte
	}{
		{path: "/public/Time", nonce: "1", secret: []byte("dGVzdA==")},
		{path: "/0/private/Balance", secret: []byte("dGVzdA==")},
		{path: "/0/private/Balance", nonce: "1", secret: []byte("invalid")},
	} {
		if _, err := SignREST(test.path, test.nonce, "nonce="+test.nonce, test.secret); err == nil {
			t.Fatalf("SignREST(%q) unexpectedly succeeded", test.path)
		}
	}
}
