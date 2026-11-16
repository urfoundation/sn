package crv4

import (
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/extrinsic"
)

func TestSignedExtrinsicImmortalityInspection(t *testing.T) {
	immortal := types.ExtrinsicEra{IsImmortalEra: true}
	mortal := types.ExtrinsicEra{IsMortalEra: true, AsMortalEra: types.MortalEra{First: 1, Second: 1}}
	makeExtrinsic := func(fields ...*extrinsic.SignedField) *extrinsic.Extrinsic {
		return &extrinsic.Extrinsic{Signature: &extrinsic.Signature{SignedFields: fields}}
	}
	eraField := func(value any) *extrinsic.SignedField {
		return &extrinsic.SignedField{Name: extrinsic.EraSignedField, Value: value, Mutated: true}
	}

	if !SignedExtrinsicUsesImmortalEra(makeExtrinsic(eraField(immortal))) {
		t.Fatal("explicit immortal era was rejected")
	}
	if !SignedExtrinsicUsesImmortalEra(makeExtrinsic(eraField(&immortal))) {
		t.Fatal("pointer-form immortal era was rejected")
	}
	for name, candidate := range map[string]*extrinsic.Extrinsic{
		"nil":             nil,
		"unsigned":        {},
		"missing-era":     makeExtrinsic(&extrinsic.SignedField{Name: extrinsic.NonceSignedField, Value: types.U32(1)}),
		"mortal":          makeExtrinsic(eraField(mortal)),
		"nil-era":         makeExtrinsic(eraField((*types.ExtrinsicEra)(nil))),
		"wrong-type":      makeExtrinsic(eraField("immortal")),
		"duplicate-era":   makeExtrinsic(eraField(immortal), eraField(immortal)),
		"mixed-duplicate": makeExtrinsic(eraField(immortal), eraField(mortal)),
	} {
		if SignedExtrinsicUsesImmortalEra(candidate) {
			t.Fatalf("%s extrinsic was accepted as exactly immortal", name)
		}
	}
}
