package htx

import "testing"

func TestUnifiedDecimalsExpandExponentsExactly(t *testing.T) {
	for input, want := range map[Decimal]string{
		"5.21E-4": "0.000521", "1.8E-4": "0.00018", "1E+3": "1000",
		"1.23e1": "12.3", "-1.2E-2": "-0.012", "9007199254740993e-3": "9007199254740.993",
		"1.200": "1.200", "1e999999": "1e999999", "1einvalid": "1einvalid",
	} {
		if got := plainHTXDecimal(input); got != want {
			t.Errorf("%s = %s, want %s", input, got, want)
		}
	}
}
