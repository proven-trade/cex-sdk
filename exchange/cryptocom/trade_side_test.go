package cryptocom

import (
	"encoding/json"
	"testing"
)

func TestTradeSideAcceptsRESTAndStreamSpellings(t *testing.T) {
	for _, value := range []string{"buy", "BUY", "sell", "SELL"} {
		var trade Trade
		if err := json.Unmarshal([]byte(`{"s":"`+value+`"}`), &trade); err != nil {
			t.Fatal(err)
		}
		if trade.Side != TradeSideBuy && trade.Side != TradeSideSell {
			t.Fatal(trade.Side)
		}
	}
	var side TradeSide
	for _, raw := range []string{`"unknown"`, `null`, `42`} {
		if err := json.Unmarshal([]byte(raw), &side); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
