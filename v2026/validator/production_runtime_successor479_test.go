// The real runtime 479 upgrade crosses the production successor gate that a
// signed opt-in installs, through the validator's own consumed-storage checks.
package validator

import (
	"testing"
)

// Runtime 479, from crv4/testdata/runtime479-metadata.md.
const (
	productionSuccessorTest479Code     = "0x44e952fd1cebe9bd15c0c55cee6e1d7c6c97b44d941b79127343118dbbd342bc"
	productionSuccessorTest479Metadata = "0x3ef17424a54b53d3630c2fde92ebff918364a2f4730c9c23e22ebdd7c21575d1"
)

// A production validator whose signed config opts in keeps a fresh producer
// signing view when its approved runtime is replaced by 479, and halts on it
// without the opt-in. The approval and its exact anchor are unchanged.
func TestProductionRuntimeSuccessorCrossesRuntime479(t *testing.T) {
	for _, optIn := range []bool{true, false} {
		fixture := newProductionSuccessorTestFixture(t, optIn)
		_, wire479 := provisionalValidatorMetadataTest(t, "../crv4/testdata/runtime479-metadata.scale.gz.base64", productionSuccessorTest479Metadata)
		fixture.successorWire, fixture.successorCode = wire479, productionSuccessorTest479Code
		fixture.successorVersion["specVersion"] = 479
		native, cfg := fixture.rpc.native, fixture.cfg
		if optIn {
			if err := enableReleaseRuntimeSuccession(native, cfg); err != nil {
				t.Fatal(err)
			}
		}
		if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, cfg, mainnetRuntimeTestBlock(150)); err != nil {
			t.Fatal(err)
		}
		err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, cfg, mainnetRuntimeTestBlock(170))
		if !optIn {
			if err == nil {
				t.Fatal("runtime479 was admitted without the signed opt-in")
			}
			continue
		}
		if err != nil {
			t.Fatalf("compatible 479 successor halted production: %v", err)
		}
		if uint32(native.Runtime.SpecVersion) != 479 || validateReleaseNativeSigningRuntime(native, cfg) != nil {
			t.Fatal("signing view did not bind runtime 479 or failed the signing boundary")
		}
		if native.ValidateValidatorProducerRuntime(releaseNativeRuntimeIdentity(cfg)) == nil {
			t.Fatal("runtime479 successor view satisfied the exact approved pin itself")
		}
	}
}
