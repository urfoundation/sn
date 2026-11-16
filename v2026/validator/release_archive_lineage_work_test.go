//go:build linux || darwin

package validator

// Full archive work probes use actual M8 producers, two settlement terminals,
// failed-attempt predecessor links, signed envelopes and the public decoder.
// Chain observations below are synthetic offline inputs, not finality evidence.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urnetwork/connect/v2026"
	"golang.org/x/crypto/blake2b"
)

// Counts the real detached source callback, including bytes returned before a
// later cancellation. It grants no verification result and owns no cache.
type releaseArchiveLineageWork struct {
	mu    sync.Mutex
	reads map[ReleaseEvidenceV2CaptureSource]uint64
	bytes uint64
}

// Counter mutation is independent of the original byte source's behavior.
func (self *releaseArchiveLineageWork) read(reader func(context.Context, ReleaseEvidenceV2CaptureSource) ([]byte, error)) func(context.Context, ReleaseEvidenceV2CaptureSource) ([]byte, error) {
	return func(ctx context.Context, source ReleaseEvidenceV2CaptureSource) ([]byte, error) {
		raw, err := reader(ctx, source)
		self.mu.Lock()
		self.reads[source]++
		self.bytes += uint64(len(raw))
		self.mu.Unlock()
		return raw, err
	}
}

// Returns an owned census so assertions cannot modify the callback's state.
func (self *releaseArchiveLineageWork) snapshot() (map[ReleaseEvidenceV2CaptureSource]uint64, uint64) {
	self.mu.Lock()
	defer self.mu.Unlock()
	result := make(map[ReleaseEvidenceV2CaptureSource]uint64, len(self.reads))
	for source, count := range self.reads {
		result[source] = count
	}
	return result, self.bytes
}

type releaseArchiveLineageFixture struct {
	releaseArchiveV2TestFixture
	file         steeringIntentFile
	observations []ReleaseEvidenceV2DecisionObservation
}

// A failed submission may be retried in the same native epoch. Two distinct
// signed measurements therefore exercise a real predecessor edge without
// inventing a second native cut or advancing HeadEMA twice. Active providers
// carry nonempty HeadEMA; one unbound provider preserves positive pool work.
func newReleaseArchiveLineageFixture(t *testing.T) *releaseArchiveLineageFixture {
	t.Helper()
	f := &releaseArchiveLineageFixture{releaseArchiveV2TestFixture: newReleaseArchiveV2TestFixture(t)}
	archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	}()
	cfg := f.options.Config
	first := archive.history.initial[archive.history.participants[0].NoID].InitialCut
	a := &ReleaseMeasurementArtifact{
		Schema: ReleaseMeasurementSchemaV2, DeploymentID: cfg.DeploymentID,
		ChainID: cfg.ChainID, GenesisHash: strings.ToLower(cfg.GenesisHash),
		Coordinator: strings.ToLower(cfg.Coordinator), SettlementVault: strings.ToLower(cfg.SettlementVault),
		ValidatorID: cfg.ValidatorID, Netuid: cfg.Netuid, SubnetEpoch: 7,
		PolicyHash: strings.ToLower(cfg.PolicyHash), Policy: cfg.Policy,
		SelfUID: first.Identity.ValidatorUID, ControlledNOIDs: slices.Clone(cfg.ControlledNOIDs),
		Inputs: []ReleaseMeasurementInput{}, Bindings: []ReleaseBindingMeasurement{},
		HeadEMA: []HeadEMAMeasurement{}, Pools: []ReleasePoolMeasurement{}, DepositAudits: []DepositAudit{},
	}
	for _, participant := range archive.history.participants {
		journal := archive.history.inputByEpoch[a.SubnetEpoch][participant.NoID]
		if journal == nil {
			t.Fatal("full lineage fixture has no original native journal")
		}
		input := journal.MeasurementInput
		a.Inputs = append(a.Inputs, input)
		a.NativeSnapshotBlock, a.NativeSnapshotHash = input.CutNativeBlock, input.CutNativeBlockHash
		a.EVMSnapshotBlock, a.EVMSnapshotHash, a.SettlementEpoch = input.CutEVMSnapshotBlock, input.CutEVMSnapshotHash, input.SettlementEpoch
		if len(input.Stats.Providers) == 0 {
			t.Fatal("original M8 input has no provider census")
		}
		unbound := input.Stats.Providers[0]
		for _, provider := range input.Stats.Providers {
			if provider.Assignments > unbound.Assignments {
				unbound = provider
			}
		}
		for _, provider := range input.Stats.Providers {
			clientId, err := connect.ParseId(provider.ClientID)
			if err != nil {
				t.Fatal(err)
			}
			binding := attemptLedgerTestBinding(clientId, 1)
			observed := ReleaseBindingMeasurement{NoID: input.NoID, ClientID: provider.ClientID, Active: true,
				FleetID: binding.FleetID, Hotkey: binding.Hotkey, Generation: binding.Generation,
				ClientKey: releaseHex32([32]byte{0x31}), LocalClientKey: releaseHex32([32]byte{0x31}),
				CommitmentHash: releaseHex32([32]byte{0x32}), ValidFromEpoch: a.SettlementEpoch, ValidToEpoch: a.SettlementEpoch + 1,
				RecordUID: binding.UID, LiveUIDFound: true, LiveUID: binding.UID}
			if provider.ClientID == unbound.ClientID {
				observed.Active, observed.LiveUIDFound, observed.LiveUID = false, false, 0
				observed.LocalClientKey = releaseHex32([32]byte{})
			}
			a.Bindings = append(a.Bindings, observed)
		}
		a.Pools = append(a.Pools, ReleasePoolMeasurement{NoID: input.NoID, UID: uint16(100 + input.NoID), PoolHotkey: releaseHex32([32]byte{0x61, byte(input.NoID)})})
		audit := releaseMeasurementDepositAudit(t, a.Policy, input.NoID)
		if a.Policy.IsZeroPrice() {
			deadline := audit.ArtifactDeadlineBlock
			audit = ZeroPriceDepositAudit(a.SettlementEpoch, a.SettlementEpoch-a.Policy.Deposit.UsageLagEpochs, input.NoID, new(big.Int), new(big.Int))
			audit.ArtifactDeadlineBlock = deadline
		}
		audit.Epoch, audit.SourceEpoch, audit.ObservedAtBlock = a.SettlementEpoch, a.SettlementEpoch-a.Policy.Deposit.UsageLagEpochs, a.EVMSnapshotBlock
		a.DepositAudits = append(a.DepositAudits, audit)
	}
	slices.SortFunc(a.Bindings, func(left, right ReleaseBindingMeasurement) int {
		return strings.Compare(fmt.Sprintf("%020d:%s", left.NoID, left.ClientID), fmt.Sprintf("%020d:%s", right.NoID, right.ClientID))
	})
	f.populateOriginalHead(t, a)
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x31})
	if err != nil || hotkey == nil || hotkey.PublicKey() != f.options.Hotkey {
		t.Fatalf("fixture original hotkey: %v", err)
	}
	f.file = steeringIntentFile{Schema: steeringIntentSchema, History: []SteeringIntent{}}
	for index := range 2 {
		observation := ReleaseEvidenceV2DecisionObservation{Decision: releaseMeasurementV2Decision(a), Bindings: slices.Clone(a.Bindings), Pools: slices.Clone(a.Pools), DepositAudits: slices.Clone(a.DepositAudits)}
		basis := &SteeringIntent{SubnetEpoch: a.SubnetEpoch}
		options, err := archive.decisionOptions(t.Context(), basis, a, observation)
		if err != nil {
			t.Fatal(err)
		}
		raw, hash, err := SealReleaseMeasurementArtifactV2(t.Context(), a, options)
		if err != nil {
			t.Fatalf("seal original lineage measurement %d: %v", index, err)
		}
		options, err = archive.decisionOptions(t.Context(), basis, a, observation)
		if err != nil {
			t.Fatal(err)
		}
		verified, err := VerifyReleaseMeasurementArtifactV2(t.Context(), a, options)
		if err != nil || verified.Decision == nil || len(verified.Decision.UIDs) == 0 {
			t.Fatalf("actual signed-cut decision fixture %d: %v", index, err)
		}
		projection := &releaseMeasurementV2TestFixture{artifact: a, want: verified.Decision}
		intent := projection.intent(t, raw)
		intent.Schema, intent.Status = steeringIntentSchema, "pending"
		signedAt := time.Date(2026, time.September, 30, 0, 0, index, 0, time.UTC)
		intent.CreatedAt, intent.UpdatedAt = signedAt.Format(time.RFC3339Nano), signedAt.Format(time.RFC3339Nano)
		intent.Prepared = releaseArchiveLineagePrepared(t, hotkey, a, verified.Decision.UIDs, uint32(index))
		intent.MeasurementArtifactPath, intent.MeasurementArtifactSize, err = persistReleaseMeasurementArtifact(cfg.StateDir, raw, hash)
		if err != nil {
			t.Fatal(err)
		}
		options, err = archive.decisionOptions(t.Context(), intent, a, observation)
		if err != nil {
			t.Fatal(err)
		}
		envelopeRaw, envelopeHash, _, err := SealReleaseMeasurementEnvelopeV2(t.Context(), raw, a.SelfUID, hotkey, intent.Prepared.ExtrinsicHash, signedAt, options)
		if err != nil {
			t.Fatalf("seal original lineage envelope %d: %v", index, err)
		}
		intent.MeasurementEnvelopeHash = envelopeHash
		intent.MeasurementEnvelopePath, intent.MeasurementEnvelopeSize, err = persistReleaseMeasurementEnvelope(cfg.StateDir, envelopeRaw, envelopeHash)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			intent.Status, intent.Error = "failed", "synthetic pre-inclusion refusal; original attempt retained"
		}
		intent.VectorHash, err = intent.ReconstructedVectorHash()
		if err != nil {
			t.Fatal(err)
		}
		if err := validateSteeringIntentLifecycle(intent, index == 0); err != nil {
			t.Fatal(err)
		}
		f.files[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: intent.MeasurementArtifactPath}] = raw
		f.files[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: intent.MeasurementEnvelopePath}] = envelopeRaw
		observation.MeasurementHash = hash
		f.observations = append(f.observations, observation)
		if index == 0 {
			f.file.History = append(f.file.History, *intent)
		} else {
			f.file.Current = intent
		}
		a = cloneReleaseMeasurementArtifact(t, a)
		a.PreviousArtifactHash = hash
	}
	f.repinIntents(t)
	return f
}

// The existing legacy scoring oracle reconstructs original head inputs from
// the exact signed record prefix. No compact verifier result supplies raw head
// scores or prior EMA; the current public archive must reproduce both itself.
func (self *releaseArchiveLineageFixture) populateOriginalHead(t *testing.T, artifact *ReleaseMeasurementArtifact) {
	t.Helper()
	legacy := cloneReleaseMeasurementArtifact(t, artifact)
	legacy.Schema = ReleaseMeasurementSchema
	stats := map[uint64]VerifiedReleaseStats{}
	for index := range legacy.Inputs {
		input := &legacy.Inputs[index]
		cut := input.AttemptCutV2
		participant := self.startup.disk.participants[index]
		if participant.NoID != input.NoID || cut == nil || cut.Context.FirstSequence != 1 || cut.Context.EgressFirstSequence != 1 {
			t.Fatal("lineage head oracle requires the original complete first prefix")
		}
		var records []AttemptRecord
		if err := participant.Ledger.Walk(t.Context(), 1, cut.LastSequence, func(record AttemptRecord) error {
			copyRecord, err := cloneAttemptRecord(record)
			if err == nil {
				records = append(records, copyRecord)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		ledger := &AttemptLedger{identity: cut.Context.Identity, vsk: self.startup.inputs[index].PrivateKey, records: records}
		var err error
		input.Stats.AttemptCut, err = ledger.BuildCut(cut.Context.Boundary, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyAttemptLedgerCut(input.Stats.AttemptCut, self.startup.inputs[index].PrivateKey.Public().(ed25519.PublicKey), self.startup.keys[input.NoID]); err != nil {
			t.Fatal(err)
		}
		input.AttemptCutV2 = nil
		stats[input.NoID], err = VerifyReleaseStatsMeasurement(input.Stats)
		if err != nil {
			t.Fatalf("original signed head scoring prefix: %v", err)
		}
	}
	fleets, _, _, _, err := releaseMeasurementBindings(legacy, stats)
	if err != nil {
		t.Fatal(err)
	}
	head, err := NewHeadEMAStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, artifact.HeadEMA, err = head.PreviewForEpoch(artifact.SubnetEpoch, releaseRawHeadScores(fleets), artifact.Policy.Steering.HeadScoreEMA)
	if err != nil || len(artifact.HeadEMA) == 0 {
		t.Fatalf("original M8 lineage lacks nonempty HeadEMA: %v", err)
	}
	positive := 0
	for _, record := range artifact.HeadEMA {
		if record.HasPrior {
			t.Fatal("original full-lineage fixture invented a prior head fold")
		}
		if record.HasRaw && record.Next.Numerator != "0" {
			positive++
		}
	}
	if positive == 0 {
		t.Fatal("original full-lineage fixture lacks actual positive first head fold")
	}
}

// Lifecycle replay needs exact prepared wire bytes, not an on-chain receipt.
// This uses the existing structural CRv4 fixture grammar; no native submission
// or independent signature/inclusion authority is claimed for the placeholder.
func releaseArchiveLineagePrepared(t *testing.T, hotkey *crv4.Keypair, artifact *ReleaseMeasurementArtifact, uids []uint16, nonce uint32) *crv4.PreparedSubmission {
	t.Helper()
	values := make([]uint16, len(uids))
	for index := range values {
		values[index] = 65535
	}
	payload, err := (&crv4.Payload{Hotkey: hotkey.PublicKey(), Uids: uids, Values: values, VersionKey: 1}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := []byte{0xde, 0xad, 0xbe, 0xef, byte(nonce)}
	body := append([]byte{0x84, 0xaa}, ciphertext...)
	raw := append([]byte{byte(len(body) << 2)}, body...)
	cipherHash, transactionHash := sha256.Sum256(ciphertext), blake2b.Sum256(raw)
	prepared := &crv4.PreparedSubmission{Schema: crv4.PreparedSubmissionSchema, Netuid: artifact.Netuid, HotkeyHex: releaseHex32(hotkey.PublicKey()), VersionKey: 1, CommitRevealVersion: 4, AccountNonce: nonce, PreparedAtBlock: artifact.NativeSnapshotBlock, PreparedAtBlockHash: artifact.NativeSnapshotHash, SubnetEpoch: artifact.SubnetEpoch, RevealRound: 8, RevealBlock: artifact.NativeSnapshotBlock + 20, UIDs: slices.Clone(uids), Values: values, PayloadHex: "0x" + hex.EncodeToString(payload), CiphertextHex: "0x" + hex.EncodeToString(ciphertext), CiphertextSHA256: releaseHex32(cipherHash), ExtrinsicHex: "0x" + hex.EncodeToString(raw), ExtrinsicHash: releaseHex32(transactionHash)}
	if _, err := prepared.Validate(); err != nil {
		t.Fatal(err)
	}
	return prepared
}

// Re-encoding a deliberate changed census never changes unrelated source bytes.
func (self *releaseArchiveLineageFixture) repinIntents(t *testing.T) {
	t.Helper()
	raw, err := marshalAttemptSettlementV2JSON(t.Context(), &self.file, self.options.Config.EvidenceV2.Bounds.IntentFileLimit(), true, true)
	if err != nil {
		t.Fatal(err)
	}
	self.files[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: "steering-intents.json"}] = raw
	self.repin()
}

// Each new owner receives fresh scratch and counts real reads. No predecessor
// or mathematical acceptance is injected into the public archive under test.
func (self *releaseArchiveLineageFixture) measuredOptions(t *testing.T) (ReleaseEvidenceV2ArchiveOptions, *releaseArchiveLineageWork) {
	t.Helper()
	options := self.options
	options.ScratchRoot = newAttemptSettlementRuntimeV2TestStateDir(t)
	work := &releaseArchiveLineageWork{reads: map[ReleaseEvidenceV2CaptureSource]uint64{}}
	options.ReadSource = work.read(self.options.ReadSource)
	return options, work
}

// Repeated lookups use the verified owner's byte copies. A fresh owner must
// reconstruct the full history again; an old success cannot waive that work.
func TestReleaseArchiveFullLineageOwnedReadsDoNotReplayHistory(t *testing.T) {
	f := newReleaseArchiveLineageFixture(t)
	options, work := f.measuredOptions(t)
	archive, err := OpenReleaseEvidenceV2Archive(t.Context(), options)
	if err != nil || archive == nil {
		t.Fatalf("actual complete lineage admission: %v", err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	}()
	if len(archive.intents) != 2 || len(archive.history.terminals) != 2 {
		t.Fatal("work probe skipped native predecessor or settlement terminal history")
	}
	for _, intent := range []*SteeringIntent{&f.file.History[0], f.file.Current} {
		reads, _ := work.snapshot()
		if reads[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: intent.MeasurementArtifactPath}] != 1 || reads[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: intent.MeasurementEnvelopePath}] != 1 {
			t.Fatal("original measurement or envelope was skipped or redundantly read during admission")
		}
	}
	beforeReplay, beforeBytes := work.snapshot()
	if err := archive.ReplayDecisions(t.Context(), f.observations); err != nil {
		t.Fatal(err)
	}
	afterReplay, afterBytes := work.snapshot()
	if reflect.DeepEqual(beforeReplay, afterReplay) || afterBytes <= beforeBytes {
		t.Fatal("complete mathematical replay skipped original signed stream bytes")
	}
	for range 8 {
		for _, observation := range f.observations {
			artifact, decision, err := archive.Measurement(observation.MeasurementHash)
			if err != nil || artifact == nil || decision == nil || len(decision.UIDs) == 0 || len(decision.StatsByNO) != 2 || len(artifact.Inputs[0].Stats.Providers) == 0 {
				t.Fatalf("owned measurement read: %v", err)
			}
			original, err := canonicalReleaseMeasurementBytes(artifact)
			if err != nil || ReleaseMeasurementContentHash(original) != observation.MeasurementHash {
				t.Fatalf("returned measurement changed its admitted content: %v", err)
			}
			artifact.Inputs[0].Stats.Providers = nil
			clear(decision.StatsByNO)
		}
		terminal, err := archive.TerminalClosure(f.last.Epoch)
		if err != nil || terminal == nil || len(terminal.Transitions) != 2 {
			t.Fatalf("owned terminal read: %v", err)
		}
		terminal.Transitions = nil
	}
	reads, bytesRead := work.snapshot()
	if !reflect.DeepEqual(reads, afterReplay) || bytesRead != afterBytes {
		t.Fatal("repeated verified lookup replayed complete source history")
	}
	if err := archive.ReplayDecisions(t.Context(), f.observations); err == nil {
		t.Fatal("completed owner accepted a second mathematical decision census")
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := archive.Measurement(f.file.Current.MeasurementArtifactHash); err == nil {
		t.Fatal("closed owner retained lookup authority")
	}
	options, work = f.measuredOptions(t)
	reopened, err := OpenReleaseEvidenceV2Archive(t.Context(), options)
	if err != nil || reopened == nil {
		t.Fatalf("fresh original history owner: %v", err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	reopenedReads, reopenedBytes := work.snapshot()
	if !reflect.DeepEqual(reopenedReads, beforeReplay) || reopenedBytes != beforeBytes {
		t.Fatal("fresh owner inherited partial or cached predecessor acceptance")
	}
	if err := reopened.ReplayDecisions(t.Context(), f.observations); err != nil {
		t.Fatal(err)
	}
}

// A well-formed current record still names its predecessor. Replacing the
// canonical census with only that record cannot turn it into a first intent.
func TestReleaseArchiveFullLineageRejectsRemovedPredecessor(t *testing.T) {
	f := newReleaseArchiveLineageFixture(t)
	f.file.History = nil
	f.repinIntents(t)
	options, work := f.measuredOptions(t)
	archive, err := OpenReleaseEvidenceV2Archive(t.Context(), options)
	if err == nil || archive != nil || !strings.Contains(err.Error(), "omits the first signed measurement predecessor") {
		t.Fatalf("removed original predecessor was accepted: %v", err)
	}
	reads, _ := work.snapshot()
	if reads[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: f.file.Current.MeasurementArtifactPath}] != 1 {
		t.Fatal("predecessor refusal did not reach the actual current signed artifact")
	}
}

// Standalone mathematical validity and a fresh real signature cannot authorize
// folding the failed predecessor's nonempty HeadEMA again in the same epoch.
// The full public archive must reject that edge after reading both originals.
func TestReleaseArchiveFullLineageCountsNonemptyHeadAndRefusesDoubleFold(t *testing.T) {
	f := newReleaseArchiveLineageFixture(t)
	options, work := f.measuredOptions(t)
	archive, err := OpenReleaseEvidenceV2Archive(t.Context(), options)
	if err != nil || archive == nil {
		t.Fatalf("original nonempty-head lineage: %v", err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := archive.ReplayDecisions(t.Context(), f.observations); err != nil {
		t.Fatal(err)
	}
	before, beforeBytes := work.snapshot()
	previous, _, err := archive.Measurement(f.file.History[0].MeasurementArtifactHash)
	if err != nil || previous == nil || len(previous.HeadEMA) == 0 {
		t.Fatalf("owned original nonempty head: %v", err)
	}
	current, _, err := archive.Measurement(f.file.Current.MeasurementArtifactHash)
	if err != nil || current == nil || !reflect.DeepEqual(current.HeadEMA, previous.HeadEMA) {
		t.Fatalf("same-native retry did not retain original head base: %v", err)
	}
	for range 16 {
		value, _, err := archive.Measurement(f.file.Current.MeasurementArtifactHash)
		if err != nil || value == nil || !reflect.DeepEqual(value.HeadEMA, previous.HeadEMA) {
			t.Fatalf("owned nonempty-head repeat lookup: %v", err)
		}
		value.HeadEMA[0].Next = RationalJSON{Numerator: "999", Denominator: "1"}
	}
	after, afterBytes := work.snapshot()
	if !reflect.DeepEqual(before, after) || beforeBytes != afterBytes {
		t.Fatal("owned nonempty-head lookups repeated full historical source work")
	}
	head, err := NewHeadEMAStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := head.CommitForEpoch(previous.SubnetEpoch, previous.HeadEMA, current.Policy.Steering.HeadScoreEMA); err != nil {
		t.Fatal(err)
	}
	rawHead := map[FleetScoreKey]*big.Rat{}
	for _, record := range current.HeadEMA {
		if record.HasRaw {
			value, err := decodeRationalJSON(record.Raw)
			if err != nil {
				t.Fatal(err)
			}
			rawHead[record.Key] = value
		}
	}
	_, current.HeadEMA, err = head.PreviewForEpoch(previous.SubnetEpoch+1, rawHead, current.Policy.Steering.HeadScoreEMA)
	if err != nil {
		t.Fatal(err)
	}
	priorCount := 0
	for _, record := range current.HeadEMA {
		if record.HasPrior {
			priorCount++
		}
	}
	if priorCount == 0 {
		t.Fatal("double-fold control lacks actual positive prior head")
	}
	intent := f.file.Current
	observation := f.observations[1]
	verifyOptions, err := archive.decisionOptions(t.Context(), intent, current, observation)
	if err != nil {
		t.Fatal(err)
	}
	raw, hash, err := SealReleaseMeasurementArtifactV2(t.Context(), current, verifyOptions)
	if err != nil {
		t.Fatalf("double fold did not reach lineage with valid standalone mathematics: %v", err)
	}
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x31})
	if err != nil {
		t.Fatal(err)
	}
	verifyOptions, err = archive.decisionOptions(t.Context(), intent, current, observation)
	if err != nil {
		t.Fatal(err)
	}
	signedAt, err := time.Parse(time.RFC3339Nano, intent.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	envelope, envelopeHash, _, err := SealReleaseMeasurementEnvelopeV2(t.Context(), raw, current.SelfUID, hotkey, intent.Prepared.ExtrinsicHash, signedAt, verifyOptions)
	if err != nil {
		t.Fatalf("genuine double-fold envelope: %v", err)
	}
	intent.MeasurementArtifactHash, intent.MeasurementArtifactSize = hash, uint64(len(raw))
	intent.MeasurementArtifactPath = "measurements/" + strings.TrimPrefix(hash, "sha256:") + ".json"
	intent.MeasurementEnvelopeHash, intent.MeasurementEnvelopeSize = envelopeHash, uint64(len(envelope))
	intent.MeasurementEnvelopePath = "measurements/envelopes/" + strings.TrimPrefix(envelopeHash, "sha256:") + ".json"
	intent.VectorHash, err = intent.ReconstructedVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	f.files[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: intent.MeasurementArtifactPath}] = raw
	f.files[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: intent.MeasurementEnvelopePath}] = envelope
	f.repinIntents(t)
	changedOptions, changedWork := f.measuredOptions(t)
	changed, err := OpenReleaseEvidenceV2Archive(t.Context(), changedOptions)
	if err == nil || changed != nil || !strings.Contains(err.Error(), "head EMA prior state changed") {
		t.Fatalf("signed double fold bypassed original complete archive lineage: %v", err)
	}
	changedReads, changedBytes := changedWork.snapshot()
	for _, original := range []*SteeringIntent{&f.file.History[0], f.file.Current} {
		if changedReads[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: original.MeasurementArtifactPath}] != 1 || changedReads[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: original.MeasurementEnvelopePath}] != 1 {
			t.Fatal("double-fold refusal skipped an original measurement or envelope")
		}
	}
	t.Logf("admitted source identities=%d bytes=%d owned measurement calls=18 head entries=%d; changed-source identities=%d bytes=%d", len(before), beforeBytes, len(previous.HeadEMA), len(changedReads), changedBytes)
}

// The second signature fails after the first predecessor's actual admission.
// The public constructor must not publish that partially populated owner.
func TestReleaseArchiveFullLineageRejectsLateEnvelopeWithoutPartialOwner(t *testing.T) {
	f := newReleaseArchiveLineageFixture(t)
	intent := f.file.Current
	source := ReleaseEvidenceV2CaptureSource{Kind: "private", Name: intent.MeasurementEnvelopePath}
	var envelope ReleaseMeasurementEnvelope
	if err := json.Unmarshal(f.files[source], &envelope); err != nil {
		t.Fatal(err)
	}
	signature, err := hex.DecodeString(strings.TrimPrefix(envelope.Signature, "0x"))
	if err != nil || len(signature) == 0 {
		t.Fatalf("original envelope signature: %v", err)
	}
	signature[0] ^= 1
	envelope.Signature = "0x" + hex.EncodeToString(signature)
	raw, err := canonicalReleaseMeasurementEnvelopeBytes(&envelope)
	if err != nil {
		t.Fatal(err)
	}
	intent.MeasurementEnvelopeHash = ReleaseMeasurementEnvelopeContentHash(raw)
	intent.MeasurementEnvelopePath = "measurements/envelopes/" + strings.TrimPrefix(intent.MeasurementEnvelopeHash, "sha256:") + ".json"
	intent.MeasurementEnvelopeSize = uint64(len(raw))
	intent.VectorHash, err = intent.ReconstructedVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	f.files[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: intent.MeasurementEnvelopePath}] = raw
	f.repinIntents(t)
	options, work := f.measuredOptions(t)
	archive, err := OpenReleaseEvidenceV2Archive(t.Context(), options)
	if err == nil || archive != nil || !strings.Contains(strings.ToLower(err.Error()), "signature") {
		t.Fatalf("changed original envelope was accepted: %v", err)
	}
	reads, _ := work.snapshot()
	if reads[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: f.file.History[0].MeasurementEnvelopePath}] != 1 || reads[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: intent.MeasurementEnvelopePath}] != 1 {
		t.Fatal("signature refusal did not read both actual original envelopes")
	}
}

// The last independent observation disagrees only after the first complete
// mathematical replay. A failed census cannot expose its earlier decisions.
func TestReleaseArchiveFullLineageLateObservationPublishesNoCache(t *testing.T) {
	f := newReleaseArchiveLineageFixture(t)
	options, work := f.measuredOptions(t)
	archive, err := OpenReleaseEvidenceV2Archive(t.Context(), options)
	if err != nil || archive == nil {
		t.Fatalf("actual complete lineage admission: %v", err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	}()
	before, beforeBytes := work.snapshot()
	observations := slices.Clone(f.observations)
	observations[1].Decision.NativeSnapshotHash = releaseHex32([32]byte{0xda})
	if err := archive.ReplayDecisions(t.Context(), observations); err == nil {
		t.Fatal("changed independent decision boundary was accepted")
	}
	after, afterBytes := work.snapshot()
	if reflect.DeepEqual(before, after) || afterBytes <= beforeBytes {
		t.Fatal("late observation control skipped the first complete mathematical replay")
	}
	for _, observation := range f.observations {
		if _, _, err := archive.Measurement(observation.MeasurementHash); err == nil {
			t.Fatal("late observation refusal published partial decision cache")
		}
	}
}

// Cancellation after the actual second measurement read cannot publish a
// partial owner. Retry reads complete original history using fresh scratch.
func TestReleaseArchiveFullLineageCancellationRetainsFreshReplay(t *testing.T) {
	f := newReleaseArchiveLineageFixture(t)
	options, work := f.measuredOptions(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := options.ReadSource
	afterRead := false
	options.ReadSource = func(ctx context.Context, source ReleaseEvidenceV2CaptureSource) ([]byte, error) {
		raw, err := reader(ctx, source)
		if err == nil && source.Kind == "private" && source.Name == f.file.Current.MeasurementArtifactPath {
			afterRead = true
			cancel()
		}
		return raw, err
	}
	archive, err := OpenReleaseEvidenceV2Archive(ctx, options)
	if !afterRead || !errors.Is(err, context.Canceled) || archive != nil {
		t.Fatalf("canceled complete-history owner published acceptance: %v", err)
	}
	reads, _ := work.snapshot()
	if reads[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: f.file.History[0].MeasurementArtifactPath}] != 1 {
		t.Fatal("cancellation control did not follow real predecessor admission")
	}
	options, work = f.measuredOptions(t)
	archive, err = OpenReleaseEvidenceV2Archive(t.Context(), options)
	if err != nil || archive == nil {
		t.Fatalf("uncanceled retry could not reopen original history: %v", err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := archive.ReplayDecisions(t.Context(), f.observations); err != nil {
		t.Fatal(err)
	}
	reads, _ = work.snapshot()
	if reads[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: f.file.History[0].MeasurementArtifactPath}] != 1 {
		t.Fatal("retry reused the canceled owner's predecessor acceptance")
	}
	root := archive.owner.root
	if err := os.Rename(root, root+"-retained"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(root); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error(err)
		}
		if err := os.Rename(root+"-retained", root); err != nil {
			t.Error(err)
		}
	}()
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	_, _, custodyErr := archive.Measurement(f.file.Current.MeasurementArtifactHash)
	if custodyErr == nil || !strings.Contains(custodyErr.Error(), "changed ownership") {
		t.Fatalf("cached decision ignored lost original scratch custody: %v", custodyErr)
	}
	if resolved, err := filepath.EvalSymlinks(root); err != nil || resolved != root {
		t.Fatalf("replacement fixture has an unintended path alias: %v", err)
	}
}

// An independently owned background replay may wait on its own source. That
// does not serialize the already admitted foreground owner's cached reads.
// Channel barriers establish overlap; the timer only bounds fixture failure.
func TestReleaseArchiveFullLineageBlockedReplayAllowsOwnedForegroundRead(t *testing.T) {
	f := newReleaseArchiveLineageFixture(t)
	options, work := f.measuredOptions(t)
	foreground, err := OpenReleaseEvidenceV2Archive(t.Context(), options)
	if err != nil || foreground == nil {
		t.Fatalf("foreground history: %v", err)
	}
	defer func() {
		if err := foreground.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := foreground.ReplayDecisions(t.Context(), f.observations); err != nil {
		t.Fatal(err)
	}
	before, beforeBytes := work.snapshot()
	background, _ := f.measuredOptions(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	reader := background.ReadSource
	var once sync.Once
	background.ReadSource = func(ctx context.Context, source ReleaseEvidenceV2CaptureSource) ([]byte, error) {
		raw, err := reader(ctx, source)
		if err == nil && source.Kind == "private" && source.Name == f.file.History[0].MeasurementArtifactPath {
			once.Do(func() { close(entered) })
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return raw, err
	}
	joined := make(chan error, 1)
	go func() {
		owner, err := OpenReleaseEvidenceV2Archive(ctx, background)
		if owner != nil {
			err = errors.Join(err, owner.Close(), errors.New("blocked background unexpectedly admitted"))
		}
		joined <- err
	}()
	defer func() {
		cancel()
		select {
		case err := <-joined:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("background replay did not join cancellation: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Error("background source did not join after cancellation")
		}
	}()
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("background never reached its actual source-read barrier")
	}
	artifact, decision, err := foreground.Measurement(f.file.Current.MeasurementArtifactHash)
	if err != nil || artifact == nil || decision == nil {
		t.Fatalf("foreground blocked behind independent history: %v", err)
	}
	after, afterBytes := work.snapshot()
	if !reflect.DeepEqual(before, after) || beforeBytes != afterBytes {
		t.Fatal("foreground cached read repeated original source work")
	}
	if !bytes.Equal(mustArchiveV2JSONTest(t, artifact.Inputs), mustArchiveV2JSONTest(t, f.startupInputCopies(t))) {
		t.Fatal("foreground lost original native input census")
	}
}

// Read the original fixture artifact rather than a second verification result.
func (self *releaseArchiveLineageFixture) startupInputCopies(t *testing.T) []ReleaseMeasurementInput {
	t.Helper()
	var artifact ReleaseMeasurementArtifact
	if err := json.Unmarshal(self.files[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: self.file.Current.MeasurementArtifactPath}], &artifact); err != nil {
		t.Fatal(err)
	}
	return artifact.Inputs
}
