package gatewayclient

import "encoding/json"

func jsonUnmarshal(data []byte, target any) error {
	return json.Unmarshal(data, target)
}
