package crv4

import (
	"strings"
	"testing"
)

// The reviewed upstream commit selects a source capability; neither a nearby
// version nor a malformed spelling can acquire that capability by association.
func TestNativeOwnerSource481BindsReviewedSourceAndSpec(t *testing.T) {
	const reviewed = "6a972693ae9580541225bd37696ac2fee26b8ae9"
	if NativeOwnerSource481 != reviewed || !ReviewedNativeOwnerSource(reviewed) || !ReviewedPostAlphaMigrationSource(reviewed) {
		t.Fatal("runtime481 source lacks its independently reviewed capability")
	}
	if spec, ok := ReviewedNativeOwnerSourceSpec(reviewed); !ok || spec != 481 {
		t.Fatal("runtime481 source did not retain its own spec", spec, ok)
	}
	if source, ok := ReviewedNativeOwnerSpecSource(481); !ok || source != reviewed {
		t.Fatal("runtime481 spec did not resolve to its own reviewed source", source, ok)
	}
	for _, source := range []string{"", strings.Repeat("0", 40), strings.ToUpper(reviewed), reviewed[:39], reviewed + "0", " " + reviewed, "6a972693ae9580541225bd37696ac2fee26b8ae8"} {
		if ReviewedNativeOwnerSource(source) || ReviewedPostAlphaMigrationSource(source) {
			t.Fatal("unreviewed spelling acquired runtime481 capability", source)
		}
		if spec, ok := ReviewedNativeOwnerSourceSpec(source); ok || spec != 0 {
			t.Fatal("unreviewed spelling resolved to a spec", source, spec, ok)
		}
	}
	for _, spec := range []uint32{0, 480, 485} {
		if source, ok := ReviewedNativeOwnerSpecSource(spec); ok || source != "" {
			t.Fatal("adjacent unreviewed runtime inherited source481", spec, source, ok)
		}
	}
	if spec, ok := ReviewedNativeOwnerSourceSpec(NativeOwnerSource479); !ok || spec != 479 {
		t.Fatal("runtime481 changed the historical source identity", spec, ok)
	}
	if ReviewedPostAlphaMigrationSource(NativeOwnerSource470) {
		t.Fatal("runtime481 review retroactively completed the historical Alpha migration")
	}
}
