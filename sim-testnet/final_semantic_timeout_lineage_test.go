package main

// Pins same-call lineage authentication work while preserving every complete
// fixture edge, standalone authority boundary and caller-owned byte graph.

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Stores only a complete immutable sealed fixture wire. Each test receives a
// new graph, and no successful lineage verification is retained here.
var finalLineageWorkFixtureCache struct {
	stateLock sync.Mutex
	wire      []byte
}

// Seals the full 1,000-miner, 202/200, two-validator/operator fixture once and
// detaches all maps, raw messages, receipts and members for each caller.
func finalLineageWorkFixture(t *testing.T) *FinalSemanticEvidence {
	t.Helper()
	var wire []byte
	func() {
		finalLineageWorkFixtureCache.stateLock.Lock()
		defer finalLineageWorkFixtureCache.stateLock.Unlock()
		if len(finalLineageWorkFixtureCache.wire) == 0 {
			source, _ := finalSemanticFixture(t)
			draft, err := BuildFinalSemanticEvidence(source)
			if err != nil {
				t.Fatal(err)
			}
			sealed, err := SealFinalSemanticEvidenceOnChain(context.Background(), draft, &finalTestChainReader{evidence: draft})
			if err != nil {
				t.Fatal(err)
			}
			if sealed.ExpectedMiners != 1000 || sealed.ExpectedCandidates != 202 || sealed.ExpectedHeadSlots != 200 || sealed.ExpectedValidators != 2 || sealed.ExpectedOperators != 2 || sealed.FleetGeneration == nil || len(sealed.FleetGeneration.SetupFleets) != 200 || len(sealed.FleetGeneration.Batches) != 40 || len(sealed.FleetGeneration.ChallengerFleets) != 2 || sealed.PublicVerification == nil {
				t.Fatal("lineage work fixture lost the complete release census")
			}
			finalLineageWorkFixtureCache.wire, err = json.Marshal(sealed)
			if err != nil {
				t.Fatal(err)
			}
		}
		wire = finalLineageWorkFixtureCache.wire
	}()
	var evidence FinalSemanticEvidence
	if err := json.Unmarshal(wire, &evidence); err != nil {
		t.Fatal(err)
	}
	return &evidence
}

// Selects one real installed/refresh receipt without relying on the initial
// partition being all installed rather than partly carried from history.
func finalLineageWorkBatch(t *testing.T, evidence *FinalSemanticEvidence) *FinalFleetGenerationBatchEvidence {
	t.Helper()
	for index := range evidence.FleetGeneration.Batches {
		batch := &evidence.FleetGeneration.Batches[index]
		if batch.BatchWrite != nil && len(batch.BatchWrite.Events) != 0 {
			return batch
		}
	}
	t.Fatal("complete lineage has no real batch receipt")
	return nil
}

// Produces a different canonical 32-byte hex value deterministically.
func finalLineageWorkChangedHash(t *testing.T, value string) string {
	t.Helper()
	if err := requireFinalHex32("lineage mutation input", value); err != nil {
		t.Fatal(err)
	}
	changed := []byte(value)
	if changed[2] == '0' {
		changed[2] = '1'
	} else {
		changed[2] = '0'
	}
	return string(changed)
}

// The later public audit projection belongs to this same read-only operation
// and must not repeat complete lineage authentication already performed here.
func TestFinalSemanticVerifierAuthenticatesFleetLineageOnce(t *testing.T) {
	t.Parallel()
	evidence := finalLineageWorkFixture(t)
	lineageCalls := 0
	err := verifyFinalSemanticEvidenceWithLineageVerifier(evidence, true, func(candidate *FinalSemanticEvidence, lineage *FinalFleetGenerationLineageEvidence) error {
		if candidate != evidence || lineage != evidence.FleetGeneration {
			t.Fatal("lineage authentication no longer checks the exact enclosing evidence")
		}
		lineageCalls++
		return verifyFinalFleetGenerationLineage(candidate, lineage)
	})
	if err != nil {
		t.Fatal(err)
	}
	if lineageCalls != 1 {
		t.Fatalf("authenticated the same complete fleet lineage %d times, want exactly 1 within one semantic verification", lineageCalls)
	}
}

// Independent audit calls still authenticate every receipt before returning
// their projection, including after the same caller edits and restores bytes.
func TestFinalFleetLineageAuditStandaloneAuthenticatesEveryCall(t *testing.T) {
	t.Parallel()
	evidence := finalLineageWorkFixture(t)
	batch := finalLineageWorkBatch(t, evidence)
	originalHash := batch.CalldataHash
	for _, invalid := range []bool{false, true, false} {
		batch.CalldataHash = originalHash
		if invalid {
			batch.CalldataHash = finalLineageWorkChangedHash(t, originalHash)
		}
		batch.BatchWrite.CalldataHash = batch.CalldataHash
		lineageCalls := 0
		audit, err := finalPublicFleetGenerationAuditForEvidenceWithLineageVerifier(evidence, func(candidate *FinalSemanticEvidence, lineage *FinalFleetGenerationLineageEvidence) error {
			lineageCalls++
			return verifyFinalFleetGenerationLineage(candidate, lineage)
		})
		if lineageCalls != 1 || (err != nil) != invalid || invalid && !strings.Contains(err.Error(), "write calldata hash differs") || !invalid && audit != evidence.PublicVerification.FleetGenerationAudit {
			t.Fatalf("invalid=%t standalone lineage calls=%d error=%v", invalid, lineageCalls, err)
		}
	}
}

// A successful semantic call never authorizes a subsequent changed receipt.
// This control requires reauthentication but leaves the causal work count to
// the separate exact-once root, so it passes on the unoptimized verifier too.
func TestFinalFleetLineageVerifierRechecksAcrossCalls(t *testing.T) {
	t.Parallel()
	evidence := finalLineageWorkFixture(t)
	batch := finalLineageWorkBatch(t, evidence)
	originalHash := batch.CalldataHash
	for _, invalid := range []bool{false, true, false} {
		batch.CalldataHash = originalHash
		if invalid {
			batch.CalldataHash = finalLineageWorkChangedHash(t, originalHash)
		}
		batch.BatchWrite.CalldataHash = batch.CalldataHash
		lineageCalls := 0
		err := verifyFinalSemanticEvidenceWithLineageVerifier(evidence, true, func(candidate *FinalSemanticEvidence, lineage *FinalFleetGenerationLineageEvidence) error {
			lineageCalls++
			return verifyFinalFleetGenerationLineage(candidate, lineage)
		})
		if lineageCalls < 1 || (err != nil) != invalid || invalid && !strings.Contains(err.Error(), "write calldata hash differs") {
			t.Fatalf("invalid=%t independent semantic lineage calls=%d error=%v", invalid, lineageCalls, err)
		}
	}
}

// Identical lineage bytes are insufficient under a changed enclosing terminal
// head. Standalone verification resolves that context on every independent use.
func TestFinalFleetLineageAuditRechecksTerminalContext(t *testing.T) {
	t.Parallel()
	evidence := finalLineageWorkFixture(t)
	originalHead := evidence.NativeTerminalHead
	for _, invalid := range []bool{false, true, false} {
		evidence.NativeTerminalHead = originalHead
		if invalid {
			evidence.NativeTerminalHead.Number = 1
		}
		lineageCalls := 0
		err := verifyFinalPublicFleetGenerationAuditWithLineageVerifier(evidence, evidence.PublicVerification.FleetGenerationAudit, func(candidate *FinalSemanticEvidence, lineage *FinalFleetGenerationLineageEvidence) error {
			lineageCalls++
			return verifyFinalFleetGenerationLineage(candidate, lineage)
		})
		if lineageCalls != 1 || (err != nil) != invalid || invalid && !strings.Contains(err.Error(), "write native head follows terminal evidence") {
			t.Fatalf("invalid=%t terminal-context lineage calls=%d error=%v", invalid, lineageCalls, err)
		}
	}
}

// Self-consistently changing the unsigned event projection cannot replace the
// raw ABI event or the per-member topology join after lineage authentication.
func TestFinalSemanticVerifierRetainsRawEventProjectionChecks(t *testing.T) {
	t.Parallel()
	evidence := finalLineageWorkFixture(t)
	batch := finalLineageWorkBatch(t, evidence)
	batch.BatchWrite.Events[0].Hotkey = finalLineageWorkChangedHash(t, batch.BatchWrite.Events[0].Hotkey)
	var err error
	batch.EventHash, err = canonicalHashHex(batch.BatchWrite.Events)
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchWrite.EventHash = batch.EventHash
	lineageCalls := 0
	err = verifyFinalSemanticEvidenceWithLineageVerifier(evidence, true, func(candidate *FinalSemanticEvidence, lineage *FinalFleetGenerationLineageEvidence) error {
		lineageCalls++
		return verifyFinalFleetGenerationLineage(candidate, lineage)
	})
	if lineageCalls != 1 || err == nil || !strings.Contains(err.Error(), "event fields differ from its raw log") {
		t.Fatalf("rebound event projection bypassed raw/member checks: lineage calls=%d error=%v", lineageCalls, err)
	}
}

// Same-call reuse must not write canonicalized members, events, raw buffers or
// hash fields into the caller's full semantic object on success or rejection.
func TestFinalSemanticVerifierDoesNotMutateFleetLineageInputs(t *testing.T) {
	t.Parallel()
	evidence := finalLineageWorkFixture(t)
	for _, invalid := range []bool{false, true} {
		if invalid {
			batch := finalLineageWorkBatch(t, evidence)
			batch.CalldataHash = finalLineageWorkChangedHash(t, batch.CalldataHash)
			batch.BatchWrite.CalldataHash = batch.CalldataHash
		}
		before, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		beforeExchanges := append([]FinalRPCExchange(nil), evidence.PublicVerification.Exchanges...)
		for index := range beforeExchanges {
			beforeExchanges[index].Params = bytes.Clone(beforeExchanges[index].Params)
			beforeExchanges[index].Result = bytes.Clone(beforeExchanges[index].Result)
		}
		err = VerifyFinalSemanticEvidence(evidence)
		if (err != nil) != invalid || invalid && !strings.Contains(err.Error(), "write calldata hash differs") {
			t.Fatalf("invalid=%t semantic verification error=%v", invalid, err)
		}
		after, err := json.Marshal(evidence)
		if err != nil || !bytes.Equal(before, after) || !reflect.DeepEqual(beforeExchanges, evidence.PublicVerification.Exchanges) {
			t.Fatalf("invalid=%t semantic verification changed the caller's complete lineage: %v", invalid, err)
		}
	}
}
