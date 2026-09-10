//go:build linux || darwin

// Real source readers, retained signature custody and both protected public
// replicas must agree before an audit locator becomes discoverable.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// A compliant genuine payout and an absent-root source remain separate members
// of one complete shared census, not an outcome-selected subset.
func TestValidatorEvidenceDepositAuditV2PublishesActualCompliantSources(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
	audits := fixture.artifact.DepositAudits
	if len(audits) != 2 || audits[0].Status != DepositAuditCompliant || !audits[0].Compliant || audits[0].UsageBytes != fixture.payout.TotalUsageBytes || audits[0].HttpObservationHash == "" || audits[1].HttpObservationHash != "" || audits[1].Compliant {
		t.Fatalf("actual gathered payout/no-root audit differs: %+v", audits)
	}
	if fixture.payoutReads.Load() != 2 {
		t.Fatal("gather did not perform the real history and selected artifact requests")
	}
	parent := filepath.Join(fixture.base.runtime.cfg.StateDir, "evidence-deposit-audits")
	if _, err := os.Lstat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fresh publication fixture precreated a completed-locator namespace: %v", err)
	}
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	if state, err := os.Lstat(parent); err != nil || !state.IsDir() || state.Mode().Perm() != 0o700 {
		t.Fatalf("actual immutable publication did not create its private namespace: %v", err)
	}
	manifest, _ := fixture.retained(t)
	publication, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Kind != protocol.ValidatorEvidenceDepositAudit || manifest.Epoch != fixture.options.Window.Epoch || manifest.Subject != fixture.options.Window.Subject || len(publication.Members) != 2 {
		t.Fatal("actual later epoch/native subject or complete audit census was lost")
	}
	var census ValidatorEvidenceDepositAuditV2Census
	if err := json.Unmarshal(publication.Census, &census); err != nil {
		t.Fatal(err)
	}
	if len(census.Members) != 2 || census.Decision != manifest.Decision || census.Hotkey != fixture.base.hotkey.PublicKey() {
		t.Fatal("shared canonical census lost actual source identity")
	}
	for index, member := range publication.Members {
		payload, err := decodeValidatorEvidenceDepositAuditV2Payload(t.Context(), member.Payload, fixture.options.Bounds.MaxTransitionBytes)
		if err != nil || payload.Audit != audits[index] {
			t.Fatalf("public payload %d differs from actual gather: %v", index, err)
		}
		calldata, err := stabi.PackValidatorEvidenceCommitment(census.Members[index].Domain, fixture.options.Window, member.Evidence.Header, member.Evidence.VPKSignature, member.Evidence.HotkeySignature)
		if err != nil || !bytes.Equal(calldata, member.Calldata) {
			t.Fatalf("public member %d lost real dual consent: %v", index, err)
		}
		if (index == 0) != (len(payload.HttpObservation) > 0) {
			t.Fatal("public source invented or omitted an actual payout request")
		}
	}
	for _, store := range fixture.base.stores {
		counts := store.counts()
		if counts[fixture.sourceNoIds[0]] != 3 || counts[fixture.sourceNoIds[1]] != 2 {
			t.Fatalf("source-owned protected upload census differs: %v", counts)
		}
	}
	if fixture.nativeReads.Load() == 0 || fixture.chain.count("rootCommitments") < 4 || fixture.chain.count("epochDeposits") < 4 || fixture.publicReads.Load() == 0 || fixture.payoutReads.Load() != 2 {
		t.Fatal("publication skipped actual source/public reads or refetched past payout availability")
	}
	manifests, err := DiscoverValidatorEvidenceDepositAuditV2Manifests(t.Context(), fixture.base.runtime.cfg.StateDir, fixture.options.Bounds)
	if err != nil || len(manifests) != 1 || manifests[0].CensusHash != manifest.CensusHash {
		t.Fatalf("completed audit discovery differs: %v", err)
	}
}

// The first real production snapshot has a new cadence/effective boundary,
// while its canonical profile and original source activation remain fixed.
func TestValidatorEvidenceDepositAuditV2ProductionCadenceKeepsPinnedProfile(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "production")
	if fixture.artifact.DepositAudits[0].Status != DepositAuditCompliant {
		t.Fatal("production source did not use the actual compliant payout")
	}
	if fixture.artifact.DepositAudits[0].ArtifactDeadlineBlock != fixture.options.Window.EndBlock+fixture.base.runtime.cfg.Policy.ProductionCadence.RootCommitWindowBlocks {
		t.Fatal("production source reused the accelerated root deadline")
	}
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	manifest, _ := fixture.retained(t)
	publication, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range publication.Members {
		if attemptHex32(member.Evidence.Header.Domain.PolicyHash) != fixture.base.runtime.cfg.PolicyHash || member.Evidence.Header.BoundaryBlock != fixture.artifact.EVMSnapshotBlock {
			t.Fatal("production audit changed its activation/profile authority")
		}
	}
}

// An actual unsuccessful bounded exchange is signed and replayed as observed;
// it is neither an invented success nor a claim about universal availability.
func TestValidatorEvidenceDepositAuditV2PublishesActualUnavailableResponse(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "negative")
	audit := fixture.artifact.DepositAudits[0]
	want := DepositAuditUnavailablePending
	if audit.ObservedAtBlock > audit.ArtifactDeadlineBlock {
		want = DepositAuditUnavailable
	}
	if audit.Status != want || audit.Compliant || audit.HttpObservationHash == "" || fixture.payoutReads.Load() != 1 {
		t.Fatalf("actual unavailable observation differs: %+v", audit)
	}
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	manifest, _ := fixture.retained(t)
	publication, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := decodeValidatorEvidenceDepositAuditV2Payload(t.Context(), publication.Members[0].Payload, fixture.options.Bounds.MaxTransitionBytes)
	if err != nil {
		t.Fatal(err)
	}
	var observation releaseArtifactHttpObservationV2
	if err := json.Unmarshal(payload.HttpObservation, &observation); err != nil {
		t.Fatal(err)
	}
	if payload.Audit != audit || observation.Decision != manifest.Decision || observation.Signature == "" || len(observation.Exchanges) != 1 || observation.Exchanges[0].Status != http.StatusServiceUnavailable || string(observation.Exchanges[0].Body) != `{"actual":"payout unavailable"}` || fixture.payoutReads.Load() != 1 {
		t.Fatal("public audit replaced the exact retained unsuccessful request/response")
	}
}

// An independently read absent root produces a complete source payload without
// manufacturing an http request that the actual gather never made.
func TestValidatorEvidenceDepositAuditV2PublishesNoPayoutWithoutInventedHttp(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "no-payout")
	if fixture.payoutReads.Load() != 0 {
		t.Fatal("no-payout gather contacted an artifact endpoint")
	}
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	manifest, _ := fixture.retained(t)
	publication, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options)
	if err != nil || len(publication.Members) != 2 {
		t.Fatalf("no-payout complete public census differs: %v", err)
	}
	for index, member := range publication.Members {
		payload, err := decodeValidatorEvidenceDepositAuditV2Payload(t.Context(), member.Payload, fixture.options.Bounds.MaxTransitionBytes)
		if err != nil || payload.Audit != fixture.artifact.DepositAudits[index] || payload.Audit.Compliant || payload.Audit.HttpObservationHash != "" || len(payload.HttpObservation) != 0 {
			t.Fatalf("no-root source %d invented positive/request evidence: %v", index, err)
		}
	}
	if fixture.payoutReads.Load() != 0 || fixture.chain.count("rootCommitments") < 4 {
		t.Fatal("no-payout publication skipped actual roots or invented a request")
	}
}

// The last member's second replica fails after earlier real uploads. Restart
// opens the same durable bundle and reuses every original consent byte.
func TestValidatorEvidenceDepositAuditV2RestartReusesOriginalSignatures(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
	fixture.refuseLast.Store(true)
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err == nil || fixture.refuseLast.Load() {
		t.Fatalf("deterministic final replica failure did not refuse publication: pending=%t posts=%d error=%v", fixture.refuseLast.Load(), fixture.posts.Load(), err)
	}
	manifests, err := DiscoverValidatorEvidenceDepositAuditV2Manifests(t.Context(), fixture.base.runtime.cfg.StateDir, fixture.options.Bounds)
	if err != nil || len(manifests) != 0 || fixture.posts.Load() < 9 {
		t.Fatalf("partial public census became discoverable: %v", err)
	}
	path, err := ValidatorEvidenceDepositAuditV2ManifestPath(fixture.base.runtime.cfg.StateDir, fixture.options.Window.Epoch, fixture.options.Window.Subject)
	if err != nil {
		t.Fatal(err)
	}
	preparedPath := filepath.Join(fixture.base.runtime.cfg.StateDir, "evidence-deposit-audit-prepared", filepath.Base(path))
	original, err := os.ReadFile(preparedPath)
	if err != nil {
		t.Fatal(err)
	}
	var retained releaseDepositAuditPreparedV2
	if err := json.Unmarshal(original, &retained); err != nil {
		t.Fatal(err)
	}
	fixture.outage.Store(true)
	fixture.restart()
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	manifest, actual := fixture.retained(t)
	if !bytes.Equal(actual, original) || fixture.payoutReads.Load() != 2 {
		t.Fatal("restart resigned or replaced the original bounded payout observation")
	}
	publication, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	for index, member := range publication.Members {
		if !bytes.Equal(member.SignedArtifact, retained.Publication.Members[index].SignedArtifact) || !bytes.Equal(member.Payload, retained.Publication.Members[index].Payload) {
			t.Fatal("restart selected fresh signatures or source payloads")
		}
	}
	posts := fixture.posts.Load()
	fixture.restart()
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	if fixture.posts.Load() != posts || fixture.payoutReads.Load() != 2 {
		t.Fatal("completed retry uploaded or queried a new past availability result")
	}
	retained.Publication.Members[0].Evidence.VPKSignature[0] ^= 1
	corrupt, err := marshalAttemptSettlementV2JSON(t.Context(), &retained, fixture.options.Bounds.MaxHistoryBytes, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preparedPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err == nil || fixture.posts.Load() != posts {
		t.Fatal("corrupt original signature custody reached protected upload")
	}
	if err := os.WriteFile(preparedPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(preparedPath, filepath.Join(t.TempDir(), "retained-original.json")); err != nil {
		t.Fatal(err)
	}
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err == nil || fixture.posts.Load() != posts {
		t.Fatal("completed locator authorized replacement signatures after custody loss")
	}
}

// Real native permit/stake and hash-selected Evm metagraph/deposit changes all
// fail before protected upload. Candidate audits are never their own authority.
func TestValidatorEvidenceDepositAuditV2RejectsChangedActualSources(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
	native := fixture.base.startup.nativeFixture
	native.permit = false
	requireDepositAuditPublicationV2Refused(t, fixture)
	native.permit = true
	native.threshold = native.total + 1
	requireDepositAuditPublicationV2Refused(t, fixture)
	native.threshold = 100
	original := fixture.artifact.DepositAudits[0].ObservedDepositRao
	fixture.artifact.DepositAudits[0].ObservedDepositRao = "123456789"
	requireDepositAuditPublicationV2Refused(t, fixture)
	fixture.artifact.DepositAudits[0].ObservedDepositRao = original
	coordinator := fixture.chain.chain.coordinator
	calldata := coordinator.PackEpochDeposits(new(big.Int).SetUint64(fixture.artifact.SettlementEpoch), new(big.Int).SetUint64(fixture.sourceNoIds[0]))
	key := fmt.Sprintf("%x", calldata)
	view := fixture.chain.views[key]
	fixture.chain.set(t, "epochDeposits", calldata, big.NewInt(1))
	requireDepositAuditPublicationV2Refused(t, fixture)
	fixture.chain.views[key] = view
	selector, netuid, uid := evmSelector("getHotkey(uint16,uint16)"), evmUint16Word(fixture.artifact.Netuid), evmUint16Word(fixture.artifact.SelfUID)
	key = fmt.Sprintf("%x", append(append(slices.Clone(selector[:]), netuid[:]...), uid[:]...))
	view = fixture.chain.views[key]
	fixture.chain.views[key] = releaseDecisionV2TestView{method: "getHotkey", data: bytes.Repeat([]byte{0x98}, 32)}
	requireDepositAuditPublicationV2Refused(t, fixture)
	fixture.chain.views[key] = view
	if fixture.nativeReads.Load() == 0 {
		t.Fatal("negative controls bypassed actual native transport")
	}
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatalf("restored real sources failed: %v", err)
	}
}

// The private tiny runtime is an exact fixture identity, not a wildcard for
// newer versions or another artifact. Every mismatch fails before uploads.
func TestValidatorEvidenceDepositAuditV2RejectsChangedNativeArtifactIdentity(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
	expected := fixture.base.startup.nativeFixture.expected
	configured := fixture.base.runtime.cfg
	if actual := releaseRuntimeIdentityV2(&configured); actual != expected {
		t.Fatal("later audit producer lost the actual startup fixture artifact", actual, expected)
	}
	for _, fault := range []string{"version", "code", "metadata"} {
		changed := configured
		switch fault {
		case "version":
			changed.RuntimeSpec++
		case "code":
			changed.RuntimeCodeHash = attemptHex32([32]byte{0x91})
		case "metadata":
			changed.RuntimeMetadataHash = attemptHex32([32]byte{0x92})
		}
		fixture.base.runtime.cfg = changed
		requireDepositAuditPublicationV2Refused(t, fixture)
	}
	fixture.base.runtime.cfg = configured
	if fixture.nativeReads.Load() == 0 || fixture.posts.Load() != 0 {
		t.Fatal("wrong runtime identity did not reach actual native authority before refusing upload")
	}
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal("restored exact native fixture identity did not publish genuine sources", err)
	}
}

// A complete locator and first origin cannot hide missing or corrupt bytes on
// the other actual public origin. All workers join before mutation resumes.
func TestValidatorEvidenceDepositAuditV2RequiresBothCompletePublicReplicas(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	manifest, _ := fixture.retained(t)
	publication, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	store := fixture.base.stores[1]
	for _, hash := range [][32]byte{publication.CensusHash, publication.Members[1].SignedArtifactHash, publication.Members[1].Evidence.Header.PayloadHash} {
		key := "metadata/" + attemptHex32(hash)
		var original []byte
		func() {
			store.stateLock.Lock()
			defer store.stateLock.Unlock()
			original = bytes.Clone(store.objects[key])
			delete(store.objects, key)
		}()
		if len(original) == 0 {
			t.Fatal("required actual public object was never uploaded")
		}
		if got, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options); err == nil || got != nil {
			t.Fatal("one missing last-origin object yielded relay calldata")
		}
		func() {
			store.stateLock.Lock()
			defer store.stateLock.Unlock()
			store.objects[key] = append(bytes.Clone(original), ' ')
		}()
		if got, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options); err == nil || got != nil {
			t.Fatal("corrupt last-origin bytes yielded relay calldata")
		}
		func() { store.stateLock.Lock(); defer store.stateLock.Unlock(); store.objects[key] = original }()
	}
	if _, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options); err != nil {
		t.Fatal(err)
	}
}

// Discovery is bounded exact private custody. Public reads admit supplied
// coordinates, participants and cancellation before contacting either origin.
func TestValidatorEvidenceDepositAuditV2RejectsLocatorAuthorityAndCustodyDrift(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	manifest, _ := fixture.retained(t)
	before := fixture.publicReads.Load()
	for _, fault := range []string{"subject", "census", "policy", "aggregate"} {
		candidate, options := *manifest, fixture.options
		switch fault {
		case "subject":
			options.Window.Subject.NativeEpoch++
		case "census":
			candidate.Members = slices.Clone(manifest.Members[:1])
		case "policy":
			candidate.Decision.PolicyHash = "0x" + strings.Repeat("ab", 32)
		case "aggregate":
			options.Bounds.MaxControlBytes = 1
		}
		if got, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), &candidate, options); err == nil || got != nil {
			t.Fatalf("%s escaped public authority admission", fault)
		}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := ReadValidatorEvidenceDepositAuditV2(cancelled, manifest, fixture.options); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("cancelled public owner returned a result")
	}
	if fixture.publicReads.Load() != before {
		t.Fatal("invalid supplied authority contacted a public origin")
	}
	bounds := fixture.options.Bounds
	bounds.MaxHistoryBytes = 1
	if got, err := DiscoverValidatorEvidenceDepositAuditV2Manifests(t.Context(), fixture.base.runtime.cfg.StateDir, bounds); err == nil || got != nil {
		t.Fatal("discovery exceeded its finite aggregate custody")
	}
	path, err := ValidatorEvidenceDepositAuditV2ManifestPath(fixture.base.runtime.cfg.StateDir, manifest.Epoch, manifest.Subject)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(bytes.Clone(raw), ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadValidatorEvidenceDepositAuditV2Manifest(t.Context(), path, bounds.MaxClosureBytes, bounds.MaxParticipants); err == nil || got != nil {
		t.Fatal("noncanonical locator accepted")
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(filepath.Dir(path), "alias.json")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if got, err := DiscoverValidatorEvidenceDepositAuditV2Manifests(t.Context(), fixture.base.runtime.cfg.StateDir, fixture.options.Bounds); err == nil || got != nil {
		t.Fatal("discovery followed a foreign locator alias")
	}
}

// A prelaunch absence is an empty read, not permission to create mutable state.
func TestValidatorEvidenceDepositAuditV2DiscoveryDoesNotCreateMissingState(t *testing.T) {
	cfg := validReleaseConfig(t)
	if got, err := DiscoverValidatorEvidenceDepositAuditV2Manifests(t.Context(), cfg.StateDir, cfg.EvidenceV2.Bounds); err != nil || len(got) != 0 {
		t.Fatalf("initial empty discovery differs: %v", err)
	}
	if _, err := os.Stat(cfg.StateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only discovery created a state owner")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := DiscoverValidatorEvidenceDepositAuditV2Manifests(ctx, cfg.StateDir, cfg.EvidenceV2.Bounds); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("cancelled missing discovery returned success state")
	}
}
