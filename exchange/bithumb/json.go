package bithumb

import "encoding/json"

type jsonRawMessage = json.RawMessage

func decodeJSON(data []byte, target any) error {
	return json.Unmarshal(data, target)
}
