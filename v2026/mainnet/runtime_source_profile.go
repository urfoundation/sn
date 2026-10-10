// A codec source identifies reviewed interpretation, never running-artifact
// authority. Every consumer still authenticates its independently approved full
// runtime tuple; source-to-Wasm provenance is retained in the runtime review.
package main

import "github.com/urfoundation/sn/v2026/crv4"

// Historical plans retain their original source. Current plans name a reviewed
// source without relabeling or rewriting earlier approvals.
func mainnetRuntimeCodecSource(source string) bool {
	return source == frontierMappingSourceCommit || crv4.ReviewedNativeOwnerSource(source)
}

// A version identifies signed bytes; only the reviewed source and authenticated
// metadata can establish the consumed codec. No future version is auto-approved.
func nativeOwnerRuntimeVersion(version crv4.RuntimeVersionIdentity) bool {
	return version.SpecName == "node-subtensor" && version.SpecVersion != 0 && version.TransactionVersion == 1 && version.StateVersion == 1
}

// Planning and replay use the same exact artifact. Metadata capability checks
// remain mandatory at preparation, owner signing and canonical observation.
func nativeOwnerRuntimeProfile(profile rootReceiptProfile) bool {
	return crv4.ReviewedNativeOwnerSource(profile.RuntimeSourceCommit) && nativeOwnerRuntimeVersion(profile.RuntimeVersion) && rootCanonicalHash(profile.RuntimeCodeHash) && rootCanonicalHash(profile.RuntimeMetadataHash)
}
