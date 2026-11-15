//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Only transport observations are synthetic; records, cut, closure, signatures
// and public replay all use the ordinary production implementation.
type consentRecoveryChainFixture struct {
	occupied bool
	calls    int
}

func (self *consentRecoveryChainFixture) authenticate(ctx context.Context, _ ValidatorEvidenceConsentRecoveryV2Options, adopted bool) (uint64, [32]byte, error) {
	self.calls++
	if self.occupied && !adopted {
		return 0, [32]byte{}, errors.New("immutable evidence slot already occupied")
	}
	return 1000, [32]byte{9}, ctx.Err()
}

type consentRecoveryFixture struct {
	census   *evidenceCensusV2TestFixture
	options  ValidatorEvidenceConsentRecoveryV2Options
	original *ValidatorEvidenceCensusV2Publication
	oldRaw   []byte
	chain    *consentRecoveryChainFixture
}

// A real activation record supplies its derived hash before the fixture's
// original cut/transition are signed. No production identity is used here.
func newConsentRecoveryFixture(t *testing.T) *consentRecoveryFixture {
	t.Helper()
	f := newEvidenceCensusV2TestFixtureForCensus(t, 1, 0, []uint64{9})
	operator := f.operators[0]
	domain := operator.seal.expected.Activation.Domain
	vpk, err := canonicalAttemptHex32("synthetic recovery vpk", operator.seal.expected.Identity.ValidatorVPK, false)
	if err != nil {
		t.Fatal(err)
	}
	activation := protocol.ValidatorEvidenceActivation{Domain: protocol.ValidatorEvidenceActivationDomain{ChainID: domain.ChainID, GenesisHash: domain.GenesisHash, Netuid: domain.Netuid, Coordinator: domain.Coordinator, SettlementVault: domain.SettlementVault, DeploymentIDHash: domain.DeploymentIDHash, PolicyHash: domain.PolicyHash, Epoch: domain.ActivationEpoch}, Hotkey: f.hotkey.PublicKey(), NoID: 9, VPK: vpk, FirstSequence: 1, NativeBlock: 1, NativeHash: [32]byte{1}, EVMBlock: 1, EVMHash: [32]byte{2}}
	actualDomain, err := activation.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	operator.seal.expected.Activation.Domain = actualDomain
	cut, err := SealReplicatedAttemptCutV2(t.Context(), operator.seal.ledger, operator.seal.expected, operator.seal.policy, operator.seal.key, operator.seal.bounds, AttemptCutV2ReplicaOptions{ReplayBounds: operator.seal.replay, ScratchDirectory: filepath.Join(t.TempDir(), "recovery-fixture-cut"), ServerKeys: operator.seal.server.serverPublicKeys(), Replicas: f.replicas})
	if err != nil {
		t.Fatal(err)
	}
	operator.cut = *cut.Cut
	f.closure = sealAttemptSettlementV2Test(t, f.operators...)
	publication, err := PublishValidatorEvidenceClosedCensusV2(t.Context(), f.closure, f.options(t))
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	manifest, err := WriteValidatorEvidencePublicationV2Manifest(t.Context(), stateDir, publication, []uint64{9}, f.options(t).Settlement.MaxClosureBytes, 1)
	if err != nil {
		t.Fatal(err)
	}
	path, err := ValidatorEvidencePublicationV2ManifestPath(stateDir, manifest.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	oldRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	operatorStateDir := operator.seal.ledger.directory.path
	if err := operator.seal.ledger.Close(); err != nil {
		t.Fatal(err)
	}
	f.stores[0].stateLock.Lock()
	delete(f.stores[0].objects, "metadata/"+attemptHex32(publication.Members[0].SignedArtifactHash))
	f.stores[0].stateLock.Unlock()
	return &consentRecoveryFixture{census: f, original: publication, oldRaw: oldRaw, chain: &consentRecoveryChainFixture{}, options: ValidatorEvidenceConsentRecoveryV2Options{StateDir: stateDir, OperatorStateDir: operatorStateDir, ExpectedManifestHash: sha256.Sum256(oldRaw), ExpectedManifestBytes: uint64(len(oldRaw)), OriginalConsentUnavailable: true, ExpectedHeader: publication.Members[0].Evidence.Header, ActivationContext: ReleaseEvidenceV2ActivationContext{Activation: activation, Journal: [20]byte{3}, RuntimeHash: [32]byte{4}}, Census: f.options(t)}}
}

func (self *consentRecoveryFixture) freshOptions(t *testing.T) ValidatorEvidenceConsentRecoveryV2Options {
	t.Helper()
	options := self.options
	options.Census = self.census.options(t)
	return options
}

func (self *consentRecoveryFixture) locator(t *testing.T) []byte {
	t.Helper()
	path, err := ValidatorEvidencePublicationV2ManifestPath(self.options.StateDir, self.options.ExpectedHeader.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// This is the original migration failure: every replayable history object
// survives, but deleting one randomized public consent blocks retained resume.
func TestEvidenceConsentRecoveryMissingEnvelope(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	reader, err := NewHTTPAttemptStreamV2Reader(f.original.Origins[0], f.census.operators[0].seal.bounds)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadMetadata(t.Context(), attemptHex32(f.original.Members[0].SignedArtifactHash), uint64(len(f.original.Members[0].SignedArtifact))); err == nil {
		t.Fatal("synthetic original envelope was not lost")
	}
	result, err := repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{})
	if err != nil || result == nil || !result.Completed {
		t.Fatalf("explicit identical-header consent recovery: %v", err)
	}
	raw, err := reader.ReadMetadata(t.Context(), attemptHex32(result.SignedArtifactHash), uint64(len(f.original.Members[0].SignedArtifact)))
	if err != nil {
		t.Fatal(err)
	}
	var signed ValidatorEvidenceSignedV2
	if err := json.Unmarshal(raw, &signed); err != nil {
		t.Fatal(err)
	}
	if signed.Header != f.original.Members[0].Evidence.Header || !signed.Header.VerifyVPK(signed.VPKSignature) || !signed.Header.VerifyHotkey(signed.HotkeySignature) {
		t.Fatal("recovered envelope changed history header or consent owners")
	}
	var prepared evidenceConsentRecoveryPrepared
	preparedRaw, err := os.ReadFile(result.PreparedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(preparedRaw, &prepared); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(prepared.OldManifest, f.oldRaw) || !bytes.Equal(f.locator(t), prepared.NewManifest) {
		t.Fatal("recovery lost old locator preimage or new CAS result")
	}

}

func TestEvidenceConsentRecoveryChangedHeaderPreservesLocator(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	options := f.freshOptions(t)
	options.ExpectedHeader.PayloadHash[0] ^= 1
	result, err := repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, options, f.chain, evidenceConsentRecoveryHooks{})
	if err == nil || result != nil || !bytes.Equal(f.locator(t), f.oldRaw) || f.chain.calls != 0 {
		t.Fatalf("changed header crossed recovery: %v", err)
	}
}

func TestEvidenceConsentRecoveryReplayFailurePreservesLocator(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	f.census.stores[0].stateLock.Lock()
	for key := range f.census.stores[0].objects {
		if len(key) > 8 && key[:8] == "records/" {
			delete(f.census.stores[0].objects, key)
			break
		}
	}
	f.census.stores[0].stateLock.Unlock()
	result, err := repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{})
	if err == nil || result != nil || !bytes.Equal(f.locator(t), f.oldRaw) || f.chain.calls != 0 {
		t.Fatalf("missing public replay crossed recovery: %v", err)
	}
}

func TestEvidenceConsentRecoveryOccupiedSlotPreservesLocator(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	f.chain.occupied = true
	result, err := repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{})
	if err == nil || result != nil || !bytes.Equal(f.locator(t), f.oldRaw) {
		t.Fatalf("occupied immutable slot crossed recovery: %v", err)
	}
}

func TestEvidenceConsentRecoveryPreparedCrashReusesSignatures(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	result, err := repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{afterPrepared: func() error { return errors.New("synthetic crash after prepared fsync") }})
	if err == nil || result != nil || !bytes.Equal(f.locator(t), f.oldRaw) {
		t.Fatalf("prepared crash changed active discovery: %v", err)
	}
	audit := filepath.Join(f.options.StateDir, "evidence-consent-recoveries")
	entries, err := os.ReadDir(audit)
	if err != nil || len(entries) != 1 {
		t.Fatalf("prepared public bytes not retained: %v", err)
	}
	preparedRaw, err := os.ReadFile(filepath.Join(audit, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var prepared evidenceConsentRecoveryPrepared
	if err := json.Unmarshal(preparedRaw, &prepared); err != nil {
		t.Fatal(err)
	}
	result, err = repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{})
	if err != nil || result == nil || result.SignedArtifactHash != sha256.Sum256(prepared.SignedArtifact) || !bytes.Equal(f.locator(t), prepared.NewManifest) {
		t.Fatalf("retry did not reuse durable original-header signatures: %v", err)
	}
}

func TestEvidenceConsentRecoveryExchangeCrashRetainsBothImages(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	result, err := repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{afterExchange: func() error { return errors.New("synthetic crash after exchange fsync") }})
	if err == nil || result != nil || bytes.Equal(f.locator(t), f.oldRaw) {
		t.Fatalf("exchange crash was falsely completed: %v", err)
	}
	f.chain.occupied = true
	result, err = repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{})
	if err != nil || result == nil || !result.Completed {
		t.Fatalf("post-adoption matching slot could not finish durable completion: %v", err)
	}
}

func TestEvidenceConsentRecoveryLiveWriterRefused(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	backend, err := os.Open(filepath.Join(f.options.OperatorStateDir, attemptLedgerStoreName))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if err := attemptStoreLockFile(backend); err != nil {
		t.Fatal(err)
	}
	result, err := repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{})
	if err == nil || result != nil || f.chain.calls != 0 || !bytes.Equal(f.locator(t), f.oldRaw) {
		t.Fatalf("live writer fence bypassed: %v", err)
	}
}

func TestEvidenceConsentRecoveryLocatorRacePreservesRacer(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	path, _ := ValidatorEvidencePublicationV2ManifestPath(f.options.StateDir, f.options.ExpectedHeader.Epoch)
	var third ValidatorEvidencePublicationV2Manifest
	if err := json.Unmarshal(f.oldRaw, &third); err != nil {
		t.Fatal(err)
	}
	third.Members[0].SignedArtifactHash[0] ^= 1
	thirdRaw, err := marshalAttemptSettlementV2JSON(t.Context(), third, f.options.Census.Settlement.MaxClosureBytes, true, true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{beforeExchange: func() error {
		if err := os.WriteFile(path+".race", thirdRaw, 0o600); err != nil {
			return err
		}
		return os.Rename(path+".race", path)
	}})
	if err == nil || result != nil || !bytes.Equal(f.locator(t), thirdRaw) {
		t.Fatalf("locator CAS replaced raced discovery: %v", err)
	}
}

func TestEvidenceConsentRecoveryProtectedWriteFailureRetriesPreparedBytes(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	f.census.stores[0].beforePut = func(_ context.Context, kind, hash string, raw []byte) error {
		if kind == "metadata" && bytes.Contains(raw, []byte(ValidatorEvidenceSignedV2Schema)) {
			return errors.New("synthetic protected upload refusal")
		}
		return nil
	}
	result, err := repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{})
	if err == nil || result != nil || !bytes.Equal(f.locator(t), f.oldRaw) {
		t.Fatalf("protected upload refusal adopted locator: %v", err)
	}
	projectionOptions := f.options
	projectionOptions.Census.Hotkey = nil
	projectionOptions.Census.PrivateKeys = nil
	projectionOptions.Chain = nil
	raw, err := ReadPreparedValidatorEvidenceConsentRecoveryV2(t.Context(), projectionOptions)
	if err != nil {
		t.Fatalf("signer-free public prepared projection: %v", err)
	}
	f.census.stores[0].beforePut = nil
	result, err = repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{})
	if err != nil || result == nil || result.SignedArtifactHash != sha256.Sum256(raw) {
		t.Fatalf("protected upload retry re-signed instead of reusing prepared bytes: %v", err)
	}
}

func TestEvidenceConsentRecoveryRequiresExplicitUnavailableConfirmation(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	options := f.freshOptions(t)
	options.OriginalConsentUnavailable = false
	result, err := repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, options, f.chain, evidenceConsentRecoveryHooks{})
	if err == nil || result != nil || f.chain.calls != 0 || !bytes.Equal(f.locator(t), f.oldRaw) {
		t.Fatalf("ordinary failed read authorized recovery: %v", err)
	}
}

func TestEvidenceConsentRecoveryReadableOriginalRefused(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	f.census.stores[0].stateLock.Lock()
	f.census.stores[0].objects["metadata/"+attemptHex32(f.original.Members[0].SignedArtifactHash)] = bytes.Clone(f.original.Members[0].SignedArtifact)
	f.census.stores[0].stateLock.Unlock()
	result, err := repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{})
	if err == nil || result != nil || f.chain.calls != 0 || !bytes.Equal(f.locator(t), f.oldRaw) {
		t.Fatalf("readable original consent was replaced: %v", err)
	}
}

func TestEvidenceConsentRecoveryMissingProjectionDoesNotCreateAudit(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	options := f.options
	options.Census.Hotkey, options.Census.PrivateKeys, options.Chain = nil, nil, nil
	raw, err := ReadPreparedValidatorEvidenceConsentRecoveryV2(t.Context(), options)
	if err == nil || raw != nil {
		t.Fatal("missing prepared consent produced public bytes")
	}
	if _, err := os.Stat(filepath.Join(options.StateDir, "evidence-consent-recoveries")); !os.IsNotExist(err) {
		t.Fatalf("read-only projection created audit state: %v", err)
	}
}

func TestEvidenceConsentRecoveryProjectionRejectsDifferentHeader(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	_, err := repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{afterPrepared: func() error { return errors.New("synthetic prepared crash") }})
	if err == nil {
		t.Fatal("prepared fault was not observed")
	}
	options := f.options
	options.ExpectedHeader.PayloadHash[0] ^= 1
	options.Census.Hotkey, options.Census.PrivateKeys, options.Chain = nil, nil, nil
	raw, err := ReadPreparedValidatorEvidenceConsentRecoveryV2(t.Context(), options)
	if err == nil || raw != nil || !bytes.Equal(f.locator(t), f.oldRaw) {
		t.Fatal("public projection accepted a different independently pinned header")
	}
}

func TestEvidenceConsentRecoveryExchangeRetryRejectsChangedPreimage(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	_, err := repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{afterExchange: func() error { return errors.New("synthetic exchange crash") }})
	if err == nil {
		t.Fatal("exchange fault was not observed")
	}
	displaced := filepath.Join(f.options.StateDir, "evidence-consent-recoveries", fmt.Sprintf("epoch-%020d-%x.displaced.json", f.options.ExpectedHeader.Epoch, f.options.ExpectedManifestHash))
	if err := os.WriteFile(displaced, append(bytes.Clone(f.oldRaw), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	f.chain.calls = 0
	result, err := repairValidatorEvidenceConsentV2(t.Context(), f.census.closure, f.freshOptions(t), f.chain, evidenceConsentRecoveryHooks{})
	if err == nil || result != nil || f.chain.calls != 0 {
		t.Fatal("crashed exchange was blessed without its exact original displaced preimage")
	}
}

func TestEvidenceConsentRecoveryChainInspectionRejectsMissingClient(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	block, hash, err := InspectValidatorEvidenceConsentRecoveryChainV2(t.Context(), f.options)
	if err == nil || block != 0 || hash != ([32]byte{}) || !strings.Contains(err.Error(), "release chain is unavailable") {
		t.Fatalf("missing production chain client granted eligibility: %v", err)
	}
}

func TestEvidenceConsentRecoveryChainInspectionRejectsChangedActivation(t *testing.T) {
	f := newConsentRecoveryFixture(t)
	options := f.options
	options.ActivationContext.Activation.NoID++
	block, hash, err := InspectValidatorEvidenceConsentRecoveryChainV2(t.Context(), options)
	if err == nil || block != 0 || hash != ([32]byte{}) || !strings.Contains(err.Error(), "original activation differs") {
		t.Fatalf("different original activation reached chain inspection: %v", err)
	}
}
