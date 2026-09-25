// The canonical producer's final base64 field has a bounded structural proof:
// exact typed metadata plus a freshly decoded alphabet/count/hash. Historical
// spellings still use the original whole-Json verifier on each invocation.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
)

// This is not cached authentication. The exact metadata prefix is encoded
// from the independently supplied manifest identity, never decoded and trusted
// from the carrier. A plain base64 value has no Json whitespace/escaping work.
// The returned borrowed span lives only through this synchronous verifier.
func canonicalFinalPriorFilePayloadV2(limits campaignEvidenceLimits, runId, scope string, entry campaignEvidenceFileEntry, encoded []byte) ([]byte, error) {
	if campaignMetadataSourcePathV2(entry.Path) || finalJournalCarrierRequiresBodyV2(entry.Path) {
		// These hash-addressed controls additionally require the original
		// decoded source schema, so retain their existing admission.
		return nil, nil
	}
	expected := campaignEvidenceFilePayload{
		Schema: campaignEvidenceFileSchema, RunID: runId, Scope: scope,
		Path: entry.Path, ContentHash: entry.ContentHash, Size: entry.Size, Data: []byte{},
	}
	framing, err := json.Marshal(expected)
	if err != nil {
		return nil, err
	}
	if !bytes.HasSuffix(framing, []byte("\"}")) {
		return nil, errors.New("canonical prior file framing is incomplete")
	}
	prefix := framing[:len(framing)-2]
	if len(encoded) < len(prefix)+2 || !bytes.HasPrefix(encoded, prefix) || !bytes.HasSuffix(encoded, []byte("\"}")) {
		return nil, nil
	}
	data := encoded[len(prefix) : len(encoded)-2]
	// StdEncoding accepts raw CR/LF while Json strings do not. Escaped
	// variants retain historical admission through the general decoder.
	if bytes.IndexByte(data, '\r') >= 0 || bytes.IndexByte(data, '\n') >= 0 || bytes.IndexByte(data, '\\') >= 0 || bytes.IndexByte(data, '"') >= 0 {
		return nil, nil
	}
	// Every remaining byte must belong to the base64 grammar. The existing
	// bounded decoder also checks padding placement, original byte count,
	// source hash and raw-size policy before this canonical span is usable.
	if err := verifyFinalPriorCarrierFilePayloadV2(limits, runId, scope, entry, encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

// The payload was proved canonical from exact standard-encoded metadata and
// freshly checked plain base64, so compare its bytes directly, not by rescanning
// every byte through a general Json canonicalizer. Outer framing remains exact.
func verifyFinalCanonicalPriorWireV2(envelope *ReleaseEvidenceEnvelope, payload, wire []byte) error {
	prefix, suffix, err := evidenceWireFraming(envelope)
	if err != nil {
		return err
	}
	comparator := &evidenceCanonicalWireComparator{remaining: wire}
	for _, part := range [][]byte{prefix, payload, suffix} {
		if _, err := comparator.Write(part); err != nil {
			return err
		}
	}
	if len(comparator.remaining) != 0 {
		return errors.New("canonical prior evidence wire has trailing bytes")
	}
	return nil
}
