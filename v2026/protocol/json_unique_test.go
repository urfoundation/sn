// JSON admission tests use synthetic documents only.
package protocol

import (
	"strings"
	"testing"
)

// TestJsonAdmissionRejectsAmbiguousDocuments covers decoded-key equivalence,
// nested objects, trailing values and the fixed recursion bound.
func TestJsonAdmissionRejectsAmbiguousDocuments(t *testing.T) {
	for _, test := range []struct {
		name string
		wire string
		want string
	}{
		{"direct duplicate", `{"id":1,"id":2}`, "duplicate"},
		{"escaped duplicate", `{"id":1,"\u0069d":2}`, "duplicate"},
		{"case-folded duplicate", `{"id":1,"Id":2}`, "duplicate"},
		{"unicode-folded duplicate", `{"result":1,"reſult":2}`, "duplicate"},
		{"nested duplicate", `{"result":{"value":1,"value":2}}`, "duplicate"},
		{"array duplicate", `[{"x":1},{"x":2,"x":3}]`, "duplicate"},
		{"trailing value", `{"result":1} {"result":2}`, "multiple"},
		{"trailing garbage", `{"result":1} garbage`, "trailing"},
		{"truncated", `{"result":[1,2}`, "invalid"},
		{"too deep", strings.Repeat("[", maximumJsonAdmissionDepth+2) + "0" + strings.Repeat("]", maximumJsonAdmissionDepth+2), "nesting"},
	} {
		if err := ValidateUniqueJsonKeys([]byte(test.wire)); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: got %v, want %q", test.name, err, test.want)
		}
	}
}

// TestJsonAdmissionAllowsDistinctScopes permits repeated names in separate
// objects while still admitting the complete document exactly once.
func TestJsonAdmissionAllowsDistinctScopes(t *testing.T) {
	for _, wire := range []string{
		`{"jsonrpc":"2.0","id":1,"result":{"id":2}}`,
		`[{"id":1,"result":null},{"id":2,"result":[true,false,null]}]`,
		`{"escaped":"a","\u0069d":"b"}`,
	} {
		if err := ValidateUniqueJsonKeys([]byte(wire)); err != nil {
			t.Errorf("valid document %s: %v", wire, err)
		}
	}
}
