package main

// Keeps source-namespace decoding observable at its actual execution boundary.
// Private replay owners may retain one fully decoded, exact-input namespace;
// standalone calls keep their original complete decoding boundary.

import "bytes"

// Contains only immutable source identity, complete byte length and digest.
// Its stage identifies the real caller, not a planned or suppressed operation.
type finalSemanticNamespaceDecode struct {
	stage        string
	deploymentID string
	planHash     string
	inputBytes   int
	contentHash  string
}

// Observers are immutable and cannot mutate the synchronous input graph.
// Only the two enclosing replay owners create reuse state, never a standalone
// wrapper. The state is not concurrent and must not survive that invocation.
type finalSemanticNamespaceWork struct {
	decoded      func(finalSemanticNamespaceDecode)
	decodedBytes func(int)
	reuse        *finalSemanticNamespaceInputs
}

// Retains at most one successful namespace, not a history of prior verdicts.
// Both its input bytes and decoded files are private copies; every consumer
// receives another owned map so it cannot modify a later replay's inputs.
type finalSemanticNamespaceInputs struct {
	identity      finalSemanticNamespaceIdentity
	data          []byte
	pathFileBytes map[string][]byte
}

// Binds all namespace authority supplied by the enclosing owner. Exact plan
// spelling and the complete locator are compared even where a standalone
// decoder permits equivalent spelling; a mismatch always decodes afresh.
type finalSemanticNamespaceIdentity struct {
	deploymentID string
	planHash     string
	chainID      uint64
	netuid       uint16
	hasLineage   bool
	lineage      FinalArtifactLocator
}

// Copies only namespace context, never an evidence pointer or mutable graph.
func finalSemanticNamespaceIdentityFor(evidence *FinalSemanticEvidence) finalSemanticNamespaceIdentity {
	identity := finalSemanticNamespaceIdentity{deploymentID: evidence.DeploymentID, planHash: evidence.PlanHash, chainID: evidence.ChainID, netuid: evidence.Netuid}
	if evidence.FleetGeneration != nil {
		identity.hasLineage = true
		identity.lineage = evidence.FleetGeneration.Artifact
	}
	return identity
}

// Starts a fresh owner even if a private caller supplies an older work value.
// Observation is carried forward, but no previously decoded input is trusted.
func (self finalSemanticNamespaceWork) forInvocation() finalSemanticNamespaceWork {
	return finalSemanticNamespaceWork{decoded: self.decoded, decodedBytes: self.decodedBytes, reuse: &finalSemanticNamespaceInputs{}}
}

// Reuses only exact current bytes and context. The caller still runs every
// downstream plan, journal, receipt, action and signature comparison.
func (self *finalSemanticNamespaceInputs) files(identity finalSemanticNamespaceIdentity, data []byte) (map[string][]byte, bool) {
	if self == nil || self.pathFileBytes == nil || self.identity != identity || !bytes.Equal(self.data, data) {
		return nil, false
	}
	return cloneFinalSemanticNamespaceFiles(self.pathFileBytes), true
}

// No retained namespace map or underlying file slice is exposed to a replay.
func cloneFinalSemanticNamespaceFiles(files map[string][]byte) map[string][]byte {
	owned := make(map[string][]byte, len(files))
	for name, data := range files {
		owned[name] = bytes.Clone(data)
	}
	return owned
}

// Hashing is observation-only and occurs only with a nonnil observer; the
// existing decoder still performs all original per-file content checks.
func (self finalSemanticNamespaceWork) observe(stage string, evidence *FinalSemanticEvidence, data []byte) {
	if self.decodedBytes != nil {
		self.decodedBytes(len(data))
	}
	if self.decoded != nil {
		self.decoded(finalSemanticNamespaceDecode{stage: stage, deploymentID: evidence.DeploymentID, planHash: evidence.PlanHash, inputBytes: len(data), contentHash: bytesSHA256(data)})
	}
}
