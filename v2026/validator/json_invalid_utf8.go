package validator

import "encoding/json"

// encoding/json writes each invalid UTF-8 byte as one replacement: a literal
// U+FFFD under Go 1.27's default jsonv2 experiment, or a � escape
// without it. Exact JSON writers and counters take it from one probe of the
// linked encoder, so they stay byte-exact in either build.
var encodingJSONInvalidUTF8Replacement = func() string {
	encoded, err := json.Marshal(string([]byte{0xff}))
	if err != nil || len(encoded) < 2 {
		return "\\ufffd"
	}
	return string(encoded[1 : len(encoded)-1])
}()
