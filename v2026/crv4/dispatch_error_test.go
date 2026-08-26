// Dispatch error formatting regressions preserve actionable module detail in
// transaction failures and journals.
package crv4

import (
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/registry"
)

func TestFormatDecodedEventFieldsIncludesNestedModuleError(t *testing.T) {
	fields := registry.DecodedFields{
		&registry.DecodedField{Name: "dispatch_error", Value: registry.DecodedFields{
			&registry.DecodedField{Name: "index", Value: uint8(7)},
			&registry.DecodedField{Name: "error", Value: []any{uint8(94), uint8(0), uint8(0), uint8(0)}},
		}},
	}
	got := formatDecodedEventFields(fields)
	for _, want := range []string{`"dispatch_error"`, `"index"`, `"Value":7`, `"error"`, `"Value":[94,0,0,0]`} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted fields %q do not contain %q", got, want)
		}
	}
	if strings.Contains(got, "0x") {
		t.Fatalf("formatted fields still contain pointer output: %s", got)
	}
}
