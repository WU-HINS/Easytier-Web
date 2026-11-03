package httpapi_test

import "encoding/json"

func decodeInto(body []byte, target any) error { return json.Unmarshal(body, target) }
