// A codec source identifies reviewed interpretation, never running-artifact
// authority. Every consumer still authenticates its independently approved full
// runtime tuple; source-to-Wasm provenance is retained in the runtime review.
package main

// Historical plans retain the original codec source. Current v470 plans can
// name their actual source without relabeling or rewriting earlier approvals.
func mainnetRuntimeCodecSource(source string) bool {
	return source == frontierMappingSourceCommit || source == rootPassiveSource
}
