// Dispatch error formatting regressions preserve actionable module detail in
// transaction failures and journals.
package crv4

import (
	"errors"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/registry"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

func TestFormatDecodedEventFieldsIncludesNestedModuleError(t *testing.T) {
	fields := registry.DecodedFields{
		&registry.DecodedField{Name: "dispatch_error", Value: registry.DecodedFields{
			&registry.DecodedField{Name: "index", Value: uint8(7)},
			&registry.DecodedField{Name: "error", Value: []any{uint8(94), uint8(0), uint8(0), uint8(0)}},
		}},
	}
	got := formatDecodedEventFields(nil, fields)
	for _, want := range []string{`"dispatch_error"`, `"index"`, `"Value":7`, `"error"`, `"Value":[94,0,0,0]`} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted fields %q do not contain %q", got, want)
		}
	}
	if strings.Contains(got, "0x") {
		t.Fatalf("formatted fields still contain pointer output: %s", got)
	}
}

func TestFinalizedDispatchErrorIsMachineClassifiableAndPreservesDetail(t *testing.T) {
	wantHash := types.Hash{1}
	wantBlock := types.Hash{2}
	err := error(&FinalizedDispatchError{ExtrinsicHash: wantHash, BlockHash: wantBlock, Detail: "SubtensorModule.NotEnoughBalanceToStake"})
	var dispatchError *FinalizedDispatchError
	if !errors.As(err, &dispatchError) || dispatchError.ExtrinsicHash != wantHash || dispatchError.BlockHash != wantBlock || !strings.Contains(err.Error(), "NotEnoughBalanceToStake") {
		t.Fatalf("typed dispatch error was not preserved: %#v / %v", dispatchError, err)
	}
}

func TestDecodedModuleErrorResolvesExactMetadataRegistryID(t *testing.T) {
	fields := registry.DecodedFields{
		&registry.DecodedField{Name: "sp_runtime.DispatchError.dispatch_error", Value: registry.DecodedFields{
			&registry.DecodedField{Name: "sp_runtime.ModuleError.ModuleError", Value: registry.DecodedFields{
				&registry.DecodedField{Name: "index", Value: uint8(7)},
				&registry.DecodedField{Name: "error", Value: []any{uint8(12), uint8(0), uint8(0), uint8(0)}},
			}},
		}},
	}
	errorID, ok := decodedModuleErrorID(fields)
	if !ok || errorID.ModuleIndex != 7 || errorID.ErrorIndex != [4]types.U8{12, 0, 0, 0} {
		t.Fatalf("module error = %+v, %t", errorID, ok)
	}
	errorRegistry := registry.ErrorRegistry{
		errorID: &registry.TypeDecoder{Name: "SubtensorModule.NotEnoughBalanceToStake"},
	}
	if got := errorRegistry[errorID].Name; got != "SubtensorModule.NotEnoughBalanceToStake" {
		t.Fatalf("resolved error = %q", got)
	}
}

func TestDecodedModuleErrorRejectsAdjacentMalformedShapes(t *testing.T) {
	tests := []struct {
		name   string
		fields registry.DecodedFields
	}{
		{name: "unrelated index and error", fields: registry.DecodedFields{
			&registry.DecodedField{Name: "other", Value: registry.DecodedFields{
				&registry.DecodedField{Name: "index", Value: uint8(7)},
				&registry.DecodedField{Name: "error", Value: []uint8{12, 0, 0, 0}},
			}},
		}},
		{name: "short error bytes", fields: registry.DecodedFields{
			&registry.DecodedField{Name: "sp_runtime.ModuleError.ModuleError", Value: registry.DecodedFields{
				&registry.DecodedField{Name: "index", Value: uint8(7)},
				&registry.DecodedField{Name: "error", Value: []uint8{12}},
			}},
		}},
		{name: "wide module index", fields: registry.DecodedFields{
			&registry.DecodedField{Name: "sp_runtime.ModuleError.ModuleError", Value: registry.DecodedFields{
				&registry.DecodedField{Name: "index", Value: uint16(256)},
				&registry.DecodedField{Name: "error", Value: []uint8{12, 0, 0, 0}},
			}},
		}},
	}
	for _, test := range tests {
		if errorID, ok := decodedModuleErrorID(test.fields); ok {
			t.Errorf("%s: malformed shape resolved as %+v", test.name, errorID)
		}
	}
}
