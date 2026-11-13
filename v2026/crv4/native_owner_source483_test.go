package crv4

import (
	"strings"
	"testing"
)

// The reviewed upstream commit selects a source capability; neither a nearby
// version nor a malformed spelling can acquire that capability by association.
func TestNativeOwnerSource483BindsReviewedSourceAndSpec(t *testing.T) {
	const reviewed = "d43691767756fabaec697aeb014eb788639cccf9"
	if NativeOwnerSource483 != reviewed || !ReviewedNativeOwnerSource(reviewed) || !ReviewedPostAlphaMigrationSource(reviewed) {
		t.Fatal("runtime483 source lacks its explicitly admitted source capability")
	}
	if spec, ok := ReviewedNativeOwnerSourceSpec(reviewed); !ok || spec != 483 {
		t.Fatal("runtime483 source did not retain its own spec", spec, ok)
	}
	if source, ok := ReviewedNativeOwnerSpecSource(483); !ok || source != reviewed {
		t.Fatal("runtime483 spec did not resolve to its own reviewed source", source, ok)
	}
	for _, source := range []string{"", strings.Repeat("0", 40), strings.ToUpper(reviewed), reviewed[:39], reviewed + "0", " " + reviewed, "d43691767756fabaec697aeb014eb788639cccf8"} {
		if ReviewedNativeOwnerSource(source) || ReviewedPostAlphaMigrationSource(source) {
			t.Fatal("unreviewed spelling acquired runtime483 capability", source)
		}
		if spec, ok := ReviewedNativeOwnerSourceSpec(source); ok || spec != 0 {
			t.Fatal("unreviewed spelling resolved to a spec", source, spec, ok)
		}
	}
	for _, spec := range []uint32{0, 480, 485} {
		if source, ok := ReviewedNativeOwnerSpecSource(spec); ok || source != "" {
			t.Fatal("adjacent unreviewed runtime inherited source483", spec, source, ok)
		}
	}
	if spec, ok := ReviewedNativeOwnerSourceSpec(NativeOwnerSource479); !ok || spec != 479 {
		t.Fatal("runtime483 changed the historical source identity", spec, ok)
	}
	if ReviewedPostAlphaMigrationSource(NativeOwnerSource470) {
		t.Fatal("runtime483 review retroactively completed the historical Alpha migration")
	}
}
