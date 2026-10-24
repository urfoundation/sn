// Reviewed source capabilities select native owner interfaces, not a deployed
// runtime or a spec-version whitelist. Every consumer still pins its exact
// artifact, checks consumed metadata and obtains independent authority.
package crv4

const NativeOwnerSource470 = "923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d"
const NativeOwnerSource473 = "f87cada631f81d11683e715a9f059f693992e64a"
const NativeOwnerSource475 = "d1718c99c34cf96abbf2bf09c0e9e48b945c76f5"
const NativeOwnerSource477 = "5c6e83e2f4936f257234f246957f75684c35227c"
const NativeOwnerSource478 = "850f2755bb05a3851f948489f0ab27b0645cebce"
const NativeOwnerSource479 = "e66c7eed760efaa5a3b1c249b5a3369a9bc693cf"
const NativeOwnerSource481 = "6a972693ae9580541225bd37696ac2fee26b8ae9"

// Runtime482 launch admission follows the owner's explicit qualification waiver.
// Exact artifact/configuration approvals remain independently required.
const NativeOwnerSource482 = "8cf229f8ef969389d054a4c161b53ffd164a041f"

// Runtime483 uses the selected local source checkout under the same explicit
// qualification waiver. Artifact identity remains separately pinned.
const NativeOwnerSource483 = "d43691767756fabaec697aeb014eb788639cccf9"

// Runtime484 source review confirms the consumed API and metagraph layout remain unchanged.
const NativeOwnerSource484 = "e4dbe52d3dacb4119dfabbb2a500eb3099f4cd9c"

// Each reviewed source with the spec version its runtime/src/lib.rs declares.
// A newly reviewed source is one more row. The number grants nothing by
// itself: it only lets a consumer refuse a source named for another version.
var reviewedNativeOwnerSources = []struct {
	source string
	spec   uint32
}{
	{NativeOwnerSource470, 470},
	{NativeOwnerSource473, 473},
	{NativeOwnerSource475, 475},
	{NativeOwnerSource477, 477},
	{NativeOwnerSource478, 478},
	{NativeOwnerSource479, 479},
	{NativeOwnerSource481, 481},
	{NativeOwnerSource482, 482},
	{NativeOwnerSource483, 483},
	{NativeOwnerSource484, 484},
}

// The ordinary credit, owner call and passive root interfaces share reviewed
// semantics. The Alpha migration is separate and cannot inherit this capability.
func ReviewedNativeOwnerSource(source string) bool {
	_, reviewed := ReviewedNativeOwnerSourceSpec(source)
	return reviewed
}

// The spec version a reviewed source declares. A consumer that binds the two
// refuses an approval that labels one runtime with another runtime's review.
func ReviewedNativeOwnerSourceSpec(source string) (uint32, bool) {
	for _, reviewed := range reviewedNativeOwnerSources {
		if reviewed.source == source {
			return reviewed.spec, true
		}
	}
	return 0, false
}

// The reviewed source that declares a spec version. A version without one was
// never reviewed, whatever its number.
func ReviewedNativeOwnerSpecSource(spec uint32) (string, bool) {
	for _, reviewed := range reviewedNativeOwnerSources {
		if reviewed.spec == spec {
			return reviewed.source, true
		}
	}
	return "", false
}

// Sources reviewed as running after the Alpha V2 migration. Membership is not a
// completion proof: each approval still proves the marker at its own activation.
func ReviewedPostAlphaMigrationSource(source string) bool {
	switch source {
	case NativeOwnerSource473, NativeOwnerSource475, NativeOwnerSource477, NativeOwnerSource478, NativeOwnerSource479, NativeOwnerSource481, NativeOwnerSource482, NativeOwnerSource483, NativeOwnerSource484:
		return true
	default:
		return false
	}
}
