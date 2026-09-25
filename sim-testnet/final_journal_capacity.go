// Journal controls retain their original singleton bytes and hash. Only exact
// producer slots gain this finite owner; ordinary proof capacities are unchanged.
package main

import (
	"errors"
	"strings"
)

const maximumFinalJournalBytes = 128 * 1024 * 1024
const maximumFinalJournalBundleBytes = ((maximumFinalJournalBytes + 2) / 3 * 4) + maximumCampaignFileEnvelopeOverhead
const finalJournalCapturePrefixV2 = "final-inputs/validators/v2/journals/"
const finalHistoricalJournalPath = "final-derived/historical-coordinator/journal.jsonl"

// A dedicated content-addressed namespace cannot enlarge generic V2 sources.
func finalJournalCapturePathV2(name string) bool {
	if len(name) != len(finalJournalCapturePrefixV2)+64+len(".jsonl") || !strings.HasPrefix(name, finalJournalCapturePrefixV2) || !strings.HasSuffix(name, ".jsonl") {
		return false
	}
	digest := strings.TrimSuffix(strings.TrimPrefix(name, finalJournalCapturePrefixV2), ".jsonl")
	return digest == strings.ToLower(digest) && validCanonicalHashHex("0x"+digest)
}

// Historical replay and compact capture are the only standalone producers.
func finalJournalArtifactPath(name string) bool {
	return name == finalHistoricalJournalPath || finalJournalCapturePathV2(name)
}

// A signed carrier still has to prove that enlarged bytes belong to journal
// slots. A hash-only fast path cannot validate an embedded source's capacity.
func finalJournalCarrierRequiresBodyV2(name string) bool {
	if finalJournalArtifactPath(name) || name == "final-derived/fleet-generation-lineage.json" || name == "final-derived/fleet-lifecycle-lineage.json" {
		return true
	}
	const prefix = "final-inputs/bundles/"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") {
		return false
	}
	class := finalPlanBundleClass(strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".json"))
	return class == "launch-foundation" || class == "validator-evidence-companion"
}

// The new namespace carries only the harness journal. Legacy small sources
// retain their old generic path; another proof kind cannot borrow this owner.
func validateFinalJournalCaptureSourceV2(source FinalCollectedValidatorSourceV2) error {
	if finalJournalCapturePathV2(source.Artifact.URI) {
		if source.Source.Kind != "relay-journal" || source.Source.Name != "journal.jsonl" || source.Source.Origin != "" || source.Artifact.ContentHash != "sha256:"+strings.TrimSuffix(strings.TrimPrefix(source.Artifact.URI, finalJournalCapturePrefixV2), ".jsonl") {
			return errors.New("compact journal artifact differs from its exact source identity")
		}
	} else if source.Source.Kind == "relay-journal" && source.Artifact.SizeBytes > maximumCampaignEvidenceRawFileBytes {
		return errors.New("oversized compact journal lacks its dedicated source path")
	}
	return nil
}

// Sizing never authenticates a partial or relabeled journal. Every accepted
// body retains the complete sequence and original record hashes.
func validateFinalJournalArtifactBytes(name string, raw []byte) error {
	if !finalJournalArtifactPath(name) || len(raw) == 0 || len(raw) > maximumFinalJournalBytes {
		return errors.New("journal artifact lacks its exact path, complete records, or finite capacity")
	}
	if finalJournalCapturePathV2(name) && raw[len(raw)-1] != '\n' {
		return errors.New("captured journal does not end at a durable record boundary")
	}
	if finalJournalCapturePathV2(name) && bytesSHA256(raw) != "sha256:"+strings.TrimSuffix(strings.TrimPrefix(name, finalJournalCapturePrefixV2), ".jsonl") {
		return errors.New("journal artifact differs from its content-addressed path")
	}
	_, err := decodeFinalSemanticJournalBytes(raw)
	return err
}

// The legacy bundle wire remains intact. Other paths in the same carrier do
// not inherit journal capacity or bypass their original proof decoder.
func validateFinalJournalBundleEntry(name string, entry FinalCollectedFileBundleEntry) error {
	class := finalPlanBundleClass(name)
	if entry.SizeBytes <= finalCollectedBundleMaximumRawBytes || entry.Path != "journal.jsonl" || class != "launch-foundation" && class != "validator-evidence-companion" {
		return nil
	}
	return validateFinalJournalArtifactBytes(finalHistoricalJournalPath, entry.Data)
}
