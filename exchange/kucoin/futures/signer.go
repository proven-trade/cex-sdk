package futures

import (
	"strings"

	parentkucoin "github.com/proven-trade/proven-trade-sdk/exchange/kucoin"
)

func signaturePayload(timestamp, method, endpoint string, body []byte) []byte {
	var builder strings.Builder
	builder.Grow(len(timestamp) + len(method) + len(endpoint) + len(body))
	builder.WriteString(timestamp)
	builder.WriteString(strings.ToUpper(method))
	builder.WriteString(endpoint)
	builder.Write(body)
	return []byte(builder.String())
}

func signHMACSHA256(secret, payload []byte) (string, error) {
	return parentkucoin.SignHMACSHA256(secret, payload)
}
