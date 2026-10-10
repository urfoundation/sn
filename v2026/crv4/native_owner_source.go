// Reviewed source capabilities select native owner interfaces, not a deployed
// runtime or a spec-version whitelist. Every consumer still pins its exact
// artifact, checks consumed metadata and obtains independent authority.
package crv4

const NativeOwnerSource470 = "923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d"
const NativeOwnerSource473 = "f87cada631f81d11683e715a9f059f693992e64a"
const NativeOwnerSource475 = "d1718c99c34cf96abbf2bf09c0e9e48b945c76f5"

// The ordinary credit, owner call and passive root interfaces share reviewed
// semantics. The Alpha migration is separate and cannot inherit this capability.
func ReviewedNativeOwnerSource(source string) bool {
	switch source {
	case NativeOwnerSource470, NativeOwnerSource473, NativeOwnerSource475:
		return true
	default:
		return false
	}
}

// Sources reviewed as running after the Alpha V2 migration. Membership is not a
// completion proof: each approval still proves the marker at its own activation.
func ReviewedPostAlphaMigrationSource(source string) bool {
	switch source {
	case NativeOwnerSource473, NativeOwnerSource475:
		return true
	default:
		return false
	}
}
