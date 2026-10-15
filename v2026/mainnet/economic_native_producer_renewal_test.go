// Signed renewals preserve the original completed accounting chain. The cheap
// grammar controls use real Ed25519 approval; public controls require both real
// Rust engines and the immutable contiguous original-Wasm fixture.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urnetwork/server/v2026/strecovery"
	"golang.org/x/crypto/blake2b"
)

func nativeRenewalTestWrite(t *testing.T, path string, value any) planFileReference {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return planFileReference{Path: path, Sha256: monitorReadDigest(raw)}
}

func nativeRenewalTestSign(t *testing.T, renewal *nativeProducerRenewal) {
	t.Helper()
	message, err := renewal.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
	renewal.Signature = hex.EncodeToString(ed25519.Sign(key, message))
}

func nativeRenewalTestInputs(t *testing.T) (context.Context, economicEmissionPolicy, []nativeProducerReviewedAuthority, nativeProducerRenewal, *nativeProducerFiles) {
	t.Helper()
	ctx, execution, files := nativeProducerTestFiles(t, nil)
	source := newEconomicEmissionFixture(t)
	policy := source.policy
	policy.Runtime.RuntimeSourceCommit = frontierMappingSourceCommit
	header := types.Header{ParentHash: types.Hash{1}, Number: 100, StateRoot: types.Hash{2}, ExtrinsicsRoot: types.Hash{3}, Digest: types.Digest{}}
	headerRaw, err := codec.Encode(header)
	if err != nil {
		t.Fatal(err)
	}
	headerHash := blake2b.Sum256(headerRaw)
	policy.From = economicEmissionBoundary{Number: 100, Hash: nativeExecutionTestHex(headerHash[:])}
	policy.Through = economicEmissionBoundary{Number: 101, Hash: nativeExecutionTestHex(bytes.Repeat([]byte{4}, 32))}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x71}, ed25519.SeedSize))
	rule, _ := nativeExecutionTestRecord("native-emission", 1, []nativeExecutionTestField{{name: "netuid", raw: []byte{25, 0}}})
	profile := &historicalReplayObservationProfile{Schema: historicalNativeProfileSchema, RuntimeCodeSha256: historicalReplayDigest{2}, SourceReviewSha256: historicalReplayDigest{3}, Rules: []historicalReplayHookRule{rule}}
	profileRaw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	execution.Schema, execution.ApprovalPublicKey = nativeExecutionPolicySchema, nativeExecutionTestHex(key.Public().(ed25519.PublicKey))
	execution.ReviewSha256, execution.ProfileSha256 = fmt.Sprintf("sha256:%x", profile.SourceReviewSha256), monitorReadDigest(profileRaw)
	execution.Engine = planFileReference{Path: filepath.Join(filepath.Dir(files.path), "replay"), Sha256: monitorReadDigest([]byte("reviewed replay"))}
	execution.Producer.Schema, execution.Producer.MaximumJobs = nativeProducerSchema, 4096
	execution.Producer.CaptureEngine = planFileReference{Path: filepath.Join(filepath.Dir(files.path), "capture"), Sha256: monitorReadDigest([]byte("reviewed capture"))}
	policy.Execution = execution
	original := nativeProducerAuthority{Schema: nativeProducerAuthoritySchema, Network: policy.Network, Netuid: policy.Netuid, Registration: *policy.SubnetRegistrationBlock, Generation: *policy.SubnetGeneration, From: policy.From, Runtime: policy.Runtime, ReviewSha256: execution.ReviewSha256, Profile: profile, CaptureEngine: execution.Producer.CaptureEngine, ReplayEngine: execution.Engine, Directory: execution.Directory, Nodes: execution.Producer.Nodes, MaximumJobs: execution.Producer.MaximumJobs, MaximumBytes: execution.Producer.MaximumBytes, MaximumEntries: execution.Producer.MaximumEntries, Checkpoint: strecovery.NativeFinalityCheckpoint{Schema: strecovery.NativeFinalityCheckpointSchema, CodecProfile: strecovery.NativeFinalityCodecProfile, Genesis: policy.Network.GenesisHash, HeaderScale: nativeExecutionTestHex(headerRaw), SetId: 9, LiveState: "live", Authorities: []strecovery.GrandpaAuthority{{PublicKey: nativeExecutionTestHex(key.Public().(ed25519.PublicKey)), Weight: 1}}}, Providers: []nativeProducerProvider{}}
	message, err := original.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	original.Signature = hex.EncodeToString(ed25519.Sign(key, message))
	execution.Producer.Authority = nativeRenewalTestWrite(t, filepath.Join(filepath.Dir(files.path), "original.json"), original)
	if err := policy.validate(); err != nil {
		t.Fatal("complete original policy failed before renewal", err)
	}
	loaded, err := loadNativeProducerAuthorities(ctx, policy)
	if err != nil || len(loaded) != 1 {
		t.Fatal("original independently signed approval did not load", err)
	}
	renewal := nativeProducerRenewal{Schema: nativeProducerRenewalSchema, Original: execution.Producer.Authority, Previous: execution.Producer.Authority, Ordinal: 1, After: policy.Through, Completed: 1, CompletionChain: monitorReadDigest([]byte("original completed chain")), Next: original}
	renewal.Next.Signature = ""
	renewal.Next.MaximumJobs = 8192
	nativeRenewalTestSign(t, &renewal)
	execution.Producer.Renewals = []planFileReference{nativeRenewalTestWrite(t, filepath.Join(filepath.Dir(files.path), "renewal.json"), renewal)}
	loaded, err = loadNativeProducerAuthorities(ctx, policy)
	if err != nil || len(loaded) != 2 {
		t.Fatal("complete signed renewal baseline failed admission", err)
	}
	return ctx, policy, loaded, renewal, files
}

func TestNativeProducerRenewalAuthenticatesOriginalAndMonotonicCapacity(t *testing.T) {
	_, policy, loaded, renewal, _ := nativeRenewalTestInputs(t)
	if loaded[0].value.MaximumJobs != 4096 || loaded[1].value.MaximumJobs != 8192 || loaded[1].reference != policy.Execution.Producer.Renewals[0] || loaded[1].renewal.CompletionChain != renewal.CompletionChain || loaded[0].value.Signature == "" || loaded[1].value.Signature != "" {
		t.Fatal("renewal reset original approval or completion capacity", loaded)
	}
	monitor := monitorEconomicNativePolicy{Role: "native", Observation: policy}
	original := monitor
	original.Observation = nativeProducerOriginalPolicy(original.Observation)
	if monitor.identityHash() != original.identityHash() || len(policy.Execution.Producer.Renewals) != 1 {
		t.Fatal("renewal changed immutable monitor identity or mutated the caller")
	}
}

func TestNativeProducerRenewalRefusesDomainHistoryCapacityAndSignerDrift(t *testing.T) {
	ctx, policy, _, baseline, files := nativeRenewalTestInputs(t)
	original, err := os.ReadFile(policy.Execution.Producer.Authority.Path)
	if err != nil {
		t.Fatal(err)
	}
	before := mainnetNamespaceTest(t, files.path)
	for _, fault := range []string{"domain", "provider", "directory", "anchor", "previous", "original", "cursor", "completion", "jobs", "bytes", "entries", "descendants", "profile", "review", "runtime", "signer", "unchanged"} {
		raw, err := json.Marshal(baseline)
		if err != nil {
			t.Fatal(err)
		}
		var value nativeProducerRenewal
		if err := decodePlanJson(raw, &value); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "domain":
			value.Next.Netuid++
		case "provider":
			value.Next.Providers = []nativeProducerProvider{{Hotkey: nativeExecutionTestHex(bytes.Repeat([]byte{7}, 32)), Coldkey: nativeExecutionTestHex(bytes.Repeat([]byte{8}, 32))}}
		case "directory":
			value.Next.Directory += "-replacement"
		case "anchor":
			value.Next.Checkpoint.SetId++
		case "previous":
			value.Previous.Sha256 = monitorReadDigest([]byte("different predecessor"))
		case "original":
			value.Original.Sha256 = monitorReadDigest([]byte("different origin"))
		case "cursor":
			value.After.Number--
		case "completion":
			value.CompletionChain = ""
		case "jobs":
			value.Next.MaximumJobs = 4095
		case "bytes":
			value.Next.MaximumBytes--
		case "entries":
			value.Next.MaximumEntries--
		case "descendants":
			value.Next.MaximumDescendantHeaders = 4095
		case "profile":
			value.Next.Profile.Rules[0].OffsetEnd = value.Next.Profile.Rules[0].OffsetStart
		case "review":
			value.Next.ReviewSha256 = monitorReadDigest([]byte("a different callsite semantic review"))
		case "runtime":
			value.Next.Runtime.RuntimeCodeHash = "unverified"
		case "unchanged":
			value.Next.MaximumJobs = 4096
		}
		nativeRenewalTestSign(t, &value)
		if fault == "signer" {
			value.Signature = strings.Repeat("00", ed25519.SignatureSize)
		}
		reference := nativeRenewalTestWrite(t, filepath.Join(filepath.Dir(files.path), "fault-"+fault+".json"), value)
		policy.Execution.Producer.Renewals = []planFileReference{reference}
		if _, err := loadNativeProducerAuthorities(ctx, policy); !errors.Is(err, errRpcIntegrity) {
			t.Fatal("invalid signed renewal reached publication or a different boundary", fault, err)
		}
		if after := mainnetNamespaceTest(t, files.path); !reflect.DeepEqual(before, after) {
			t.Fatal("refused renewal wrote original artifact namespace", fault)
		}
	}
	retained, err := os.ReadFile(policy.Execution.Producer.Authority.Path)
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatal("renewal rewrote original signed approval", err)
	}
}

func TestNativeProducerRenewalRequiresAuthenticatedAncestorBeforeEffects(t *testing.T) {
	_, policy, authorities, renewal, files := nativeRenewalTestInputs(t)
	state := nativeExecutionProducerState{AuthorityHash: policy.Execution.Producer.Authority.Sha256, Cursor: renewal.After, Completed: renewal.Completed, CompletionChain: renewal.CompletionChain}
	session := &nativeProducerSession{files: files, authority: &authorities[0].value, authorities: authorities, originalPolicy: policy, policy: policy, state: state}
	block := economicEmissionBlock{Boundary: economicEmissionBoundary{Number: renewal.After.Number + 1, Hash: nativeExecutionTestHex(bytes.Repeat([]byte{9}, 32))}}
	before := mainnetNamespaceTest(t, files.path)
	session.state.CompletionChain = monitorReadDigest([]byte("other acknowledged chain"))
	if err := session.admitRenewal(block, policy.Runtime); !errors.Is(err, errRpcIntegrity) || session.state.AuthorityHash != state.AuthorityHash || len(session.state.AuthorityRevisions) != 0 {
		t.Fatal("unmatched completed chain gained authority", err)
	}
	session.state = state
	if err := session.admitRenewal(block, policy.Runtime); err != nil || session.state.AuthorityHash != authorities[1].reference.Sha256 || len(session.state.AuthorityRevisions) != 1 || session.state.Cursor != state.Cursor || session.state.Completed != state.Completed || session.state.CompletionChain != state.CompletionChain || session.policy.Execution.Producer.MaximumJobs != 8192 {
		t.Fatal("exact reviewed renewal failed or reset original accounting", err)
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, files.path)) {
		t.Fatal("in-memory proposal acknowledged itself before a completed block")
	}
}

// These are actual immutable owned files and hash links; the amount/Rust path
// is exercised separately by the public delayed-review control below.
func TestNativeProducerRenewalDelayedAncestorRefusesBrokenOriginalLinks(t *testing.T) {
	_, policy, authorities, revision, files := nativeRenewalTestInputs(t)
	state := nativeExecutionProducerState{AuthorityHash: revision.Previous.Sha256, Cursor: revision.After, Completed: revision.Completed, CompletionChain: revision.CompletionChain}
	var last nativeProducerCompletion
	for number := uint64(102); number <= 103; number++ {
		child := economicEmissionBoundary{Number: number, Hash: nativeExecutionTestHex(bytes.Repeat([]byte{byte(number)}, 32))}
		last = nativeProducerCompletion{Schema: nativeProducerCompletionSchema, AuthorityHash: state.AuthorityHash, Previous: state.CompletionChain, Sequence: state.Completed + 1, Admission: nativeExecutionAdmission{Parent: state.Cursor, Child: child}}
		raw, err := json.Marshal(last)
		if err != nil {
			t.Fatal(err)
		}
		ref, err := files.publish(filepath.Join(fmt.Sprintf("b%010d-%s", number, strings.TrimPrefix(child.Hash, "0x")), "complete.json"), append(raw, '\n'), nativeProducerCompletionLimit)
		if err != nil {
			t.Fatal(err)
		}
		state.Cursor, state.Completed, state.CompletionChain, state.Completion = child, last.Sequence, rootObjectHash(last), &ref
	}
	session := &nativeProducerSession{files: files, authority: &authorities[0].value, authorities: authorities, originalPolicy: policy, policy: policy, state: state}
	if err := session.verifyRenewalAncestor(&revision); err != nil {
		t.Fatal("delayed signed ancestor could not follow intact original completions", err)
	}
	original, err := os.ReadFile(state.Completion.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"bytes", "sequence", "parent", "authority", "link", "reviewed-chain", "reviewed-boundary", "missing", "cancel"} {
		value, review := last, revision
		session.state = state
		if fault == "sequence" {
			value.Sequence--
		}
		if fault == "parent" {
			value.Admission.Parent.Number--
		}
		if fault == "authority" {
			value.AuthorityHash = monitorReadDigest([]byte("another approval"))
		}
		if fault == "link" {
			value.Previous = monitorReadDigest([]byte("another chain"))
		}
		if fault == "reviewed-chain" {
			review.CompletionChain = monitorReadDigest([]byte("unretained signed ancestor"))
		}
		if fault == "reviewed-boundary" {
			review.After.Hash = nativeExecutionTestHex(bytes.Repeat([]byte{7}, 32))
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if fault == "bytes" {
			raw = append(raw, 'x')
		} else {
			session.state.CompletionChain = rootObjectHash(value)
		}
		if err := os.WriteFile(state.Completion.Path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		oldContext := files.ctx
		if fault == "missing" {
			if err := os.Remove(state.Completion.Path); err != nil {
				t.Fatal(err)
			}
		}
		if fault == "cancel" {
			ctx, cancel := context.WithCancel(files.ctx)
			cancel()
			files.ctx = ctx
		}
		before := mainnetNamespaceTest(t, files.path)
		err = session.verifyRenewalAncestor(&review)
		files.ctx = oldContext
		if fault == "missing" {
			if !errors.Is(err, os.ErrNotExist) || errors.Is(err, errRpcIntegrity) {
				t.Fatal("missing original history was not unavailable", err)
			}
		} else if fault == "cancel" {
			if !errors.Is(err, context.Canceled) || errors.Is(err, errRpcIntegrity) {
				t.Fatal("canceled ancestor read became an integrity conflict", err)
			}
		} else if !errors.Is(err, errRpcIntegrity) {
			t.Fatal("changed original completion gained delayed authority", fault, err)
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, files.path)) {
			t.Fatal("refused ancestor read published custody", fault)
		}
		if err := os.WriteFile(state.Completion.Path, original, 0600); err != nil {
			t.Fatal(err)
		}
	}
	session.state = state
	block := economicEmissionBlock{Boundary: economicEmissionBoundary{Number: 104, Hash: nativeExecutionTestHex(bytes.Repeat([]byte{104}, 32))}}
	if err := session.admitRenewal(block, policy.Runtime); err != nil {
		t.Fatal("ordinary completed progress permanently staled the review", err)
	}
	ack := session.state.AuthorityRevisions[0]
	if ack.After != revision.After || ack.Completed != revision.Completed || ack.CompletionChain != revision.CompletionChain || ack.AdoptedAfter != state.Cursor || ack.AdoptedCompleted != state.Completed || ack.AdoptedChain != state.CompletionChain || ack.FirstCompletion != nil {
		t.Fatal("adoption lost reviewed or actual original predecessor", ack)
	}
}

func TestNativeProducerRenewalPreservesPendingOriginalEngine(t *testing.T) {
	_, policy, authorities, renewal, files := nativeRenewalTestInputs(t)
	state := nativeExecutionProducerState{AuthorityHash: policy.Execution.Producer.Authority.Sha256, Cursor: renewal.After, Completed: renewal.Completed, CompletionChain: renewal.CompletionChain}
	session := &nativeProducerSession{files: files, authority: &authorities[0].value, authorities: authorities, originalPolicy: policy, policy: policy, state: state}
	block := economicEmissionBlock{Boundary: economicEmissionBoundary{Number: renewal.After.Number + 1, Hash: nativeExecutionTestHex(bytes.Repeat([]byte{9}, 32))}}
	intent := nativeProducerIntent{AuthorityHash: state.AuthorityHash, Parent: state.Cursor, Child: block.Boundary, Runtime: policy.Runtime}
	raw, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := files.publish("intents/0000000102.json", append(raw, '\n'), 16*1024); err != nil {
		t.Fatal(err)
	}
	before := mainnetNamespaceTest(t, files.path)
	if err := session.admitRenewal(block, policy.Runtime); err != nil || !reflect.DeepEqual(session.state, state) || session.authority.ReplayEngine != authorities[0].value.ReplayEngine || session.policy.Execution.Producer.MaximumJobs != 4096 {
		t.Fatal("configured renewal reinterpreted original pending execution", err)
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, files.path)) {
		t.Fatal("renewal replaced a retained pending intent")
	}
}

func TestNativeProducerRenewalAcknowledgementRequiresOriginalFirstCompletion(t *testing.T) {
	_, policy, authorities, revision, files := nativeRenewalTestInputs(t)
	state := nativeExecutionProducerState{AuthorityHash: revision.Previous.Sha256, Cursor: revision.After, Completed: revision.Completed, CompletionChain: revision.CompletionChain}
	ack := revision.acknowledgement(authorities[1].reference, state)
	child := economicEmissionBoundary{Number: revision.After.Number + 1, Hash: nativeExecutionTestHex(bytes.Repeat([]byte{7}, 32))}
	completion := nativeProducerCompletion{Schema: nativeProducerCompletionSchema, AuthorityHash: ack.Revision.Sha256, Previous: ack.AdoptedChain, Sequence: ack.AdoptedCompleted + 1, Admission: nativeExecutionAdmission{Parent: ack.AdoptedAfter, Child: child, Runtime: revision.Next.Runtime}, Renewal: &ack}
	raw, err := json.Marshal(completion)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := files.publish(filepath.Join(fmt.Sprintf("b%010d-%s", child.Number, strings.TrimPrefix(child.Hash, "0x")), "complete.json"), append(raw, '\n'), nativeProducerCompletionLimit)
	if err != nil {
		t.Fatal(err)
	}
	ack.FirstCompletion = &ref
	session := &nativeProducerSession{files: files, authority: &authorities[1].value, authorities: authorities, originalPolicy: policy, policy: policy, state: state}
	if err := session.verifyRenewalAcknowledgement(ack, authorities[1]); err != nil {
		t.Fatal("exact first renewed completion was not reusable", err)
	}
	before := mainnetNamespaceTest(t, files.path)
	for _, fault := range []string{"review", "actual", "count", "first", "capacity"} {
		changed := ack
		switch fault {
		case "review":
			changed.CompletionChain = monitorReadDigest([]byte("other reviewed chain"))
		case "actual":
			changed.AdoptedChain = monitorReadDigest([]byte("other actual predecessor"))
		case "count":
			changed.AdoptedCompleted++
		case "first":
			changed.FirstCompletion = nil
		case "capacity":
			changed.Capacity.Jobs++
		}
		if err := session.verifyRenewalAcknowledgement(changed, authorities[1]); !errors.Is(err, errRpcIntegrity) {
			t.Fatal("changed first completion adoption was admitted", fault, err)
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, files.path)) {
			t.Fatal("acknowledgement check rewrote original files", fault)
		}
	}
}

func TestNativeProducerRenewalRuntimeApprovalRetainsPriorEngineAndProfile(t *testing.T) {
	ctx, policy, authorities, value, files := nativeRenewalTestInputs(t)
	originalBytes, err := os.ReadFile(policy.Execution.Producer.Authority.Path)
	if err != nil {
		t.Fatal(err)
	}
	value.Next.Runtime.RuntimeVersion.SpecVersion++
	value.Next.Runtime.RuntimeCodeHash = nativeExecutionTestHex(bytes.Repeat([]byte{8}, 32))
	value.Next.Runtime.RuntimeMetadataHash = nativeExecutionTestHex(bytes.Repeat([]byte{9}, 32))
	value.Next.Profile.RuntimeCodeSha256 = historicalReplayDigest{5}
	value.Next.CaptureEngine = planFileReference{Path: filepath.Join(filepath.Dir(files.path), "capture-next"), Sha256: monitorReadDigest([]byte("next reviewed capture"))}
	value.Next.ReplayEngine = planFileReference{Path: filepath.Join(filepath.Dir(files.path), "replay-next"), Sha256: monitorReadDigest([]byte("next reviewed replay"))}
	nativeRenewalTestSign(t, &value)
	policy.Execution.Producer.Renewals = []planFileReference{nativeRenewalTestWrite(t, filepath.Join(filepath.Dir(files.path), "next-runtime.json"), value)}
	loaded, err := loadNativeProducerAuthorities(ctx, policy)
	if err != nil || len(loaded) != 2 || loaded[0].value.ReplayEngine != authorities[0].value.ReplayEngine || loaded[0].value.Runtime != policy.Runtime || loaded[1].value.ReplayEngine != value.Next.ReplayEngine {
		t.Fatal("runtime review rewrote original authority or rejected an independently signed successor", err)
	}
	state := nativeExecutionProducerState{AuthorityHash: policy.Execution.Producer.Authority.Sha256, Cursor: value.After, Completed: value.Completed, CompletionChain: value.CompletionChain}
	session := &nativeProducerSession{files: files, authority: &loaded[0].value, authorities: loaded, originalPolicy: policy, policy: policy, state: state}
	block := economicEmissionBlock{Boundary: economicEmissionBoundary{Number: value.After.Number + 1, Hash: nativeExecutionTestHex(bytes.Repeat([]byte{9}, 32))}}
	if err := session.admitRenewal(block, policy.Runtime); err != nil || !reflect.DeepEqual(session.state, state) || session.policy.Execution.Engine != authorities[0].value.ReplayEngine {
		t.Fatal("early runtime approval stalled or changed healthy original-runtime work", err)
	}
	if err := session.admitRenewal(block, value.Next.Runtime); err != nil || session.policy.Runtime != value.Next.Runtime || session.policy.Execution.Engine != value.Next.ReplayEngine {
		t.Fatal("runtime adoption ignored exact next engine", err)
	}
	if len(session.runtimeCatalog()) != 2 {
		t.Fatal("runtime renewal discarded original replay layout")
	}
	retained, err := os.ReadFile(policy.Execution.Producer.Authority.Path)
	if err != nil || !bytes.Equal(originalBytes, retained) {
		t.Fatal("runtime renewal rewrote original signed bytes", err)
	}
}

func TestNativeProducerRenewalForecastWarnsBeforeExhaustion(t *testing.T) {
	_, policy, _, _, _ := nativeRenewalTestInputs(t)
	role := monitorEconomicNativePolicy{Role: "native", Observation: policy, BatchBlocks: 8}
	state := &monitorEconomicNativeState{ExecutionProducer: &nativeExecutionProducerState{AuthorityHash: policy.Execution.Producer.Authority.Sha256, Completed: 4090, ResourceForecast: &nativeProducerResourceForecast{BytesUpperBound: nativeProducerBoundaryReserve, EntriesUpperBound: nativeProducerBoundaryEntries}}}
	before := rootObjectHash(state)
	summary := state.producerCapacity(role)
	if summary == nil || summary.JobsRemaining != 6 || !summary.CapacityWarning || summary.AcknowledgedRenewals != 0 || summary.ConfiguredRenewals != 1 || summary.Capacity.Jobs != 4096 {
		t.Fatal("unacknowledged review hid original capacity warning", summary)
	}
	state.ExecutionProducer.Completed = 1
	state.ExecutionProducer.ResourceForecast.BytesUpperBound = policy.Execution.Producer.MaximumBytes - nativeProducerBoundaryReserve
	if summary = state.producerCapacity(role); !summary.CapacityWarning {
		t.Fatal("two-job byte forecast was not visible before exhaustion")
	}
	state.ExecutionProducer.Completed = 4090
	state.ExecutionProducer.ResourceForecast.BytesUpperBound = nativeProducerBoundaryReserve
	if rootObjectHash(state) != before {
		t.Fatal("capacity diagnostics modified original accounting")
	}
}

func TestNativeProducerRenewalRequiresTwoCompleteRetainedNamespaceReserves(t *testing.T) {
	_, _, files := nativeProducerTestFiles(t, nil)
	files.policy.MaximumBytes = 2 * nativeProducerBoundaryReserve
	files.policy.MaximumEntries = 4 * nativeProducerBoundaryEntries
	if err := files.admitMargin(2); err != nil {
		t.Fatal("empty namespace could not admit its exact two-job profile", err)
	}
	if _, err := files.publish("retained.json", []byte("retained\n"), 128); err != nil {
		t.Fatal(err)
	}
	before := mainnetNamespaceTest(t, files.path)
	for _, dimension := range []string{"bytes", "entries"} {
		if dimension == "entries" {
			files.policy.MaximumBytes = 4 * nativeProducerBoundaryReserve
			files.policy.MaximumEntries = 2 * nativeProducerBoundaryEntries
		}
		if err := files.admitMargin(1); err != nil {
			t.Fatal("one-job positive margin did not reach resource boundary", dimension, err)
		}
		if err := files.admitMargin(2); !errors.Is(err, errMonitorEconomicCapacity) {
			t.Fatal("renewal borrowed space from retained members or lost two-job margin", dimension, err)
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, files.path)) {
			t.Fatal("refused margin mutated original namespace", dimension)
		}
	}
}

func nativeRenewalTestPublicReview(t *testing.T, f *nativeProducerPublicFixture, state *nativeExecutionProducerState) nativeProducerRenewal {
	t.Helper()
	value := nativeProducerRenewal{Schema: nativeProducerRenewalSchema, Original: f.source.policy.Execution.Producer.Authority, Previous: f.source.policy.Execution.Producer.Authority, Ordinal: 1, After: state.Cursor, Completed: state.Completed, CompletionChain: state.CompletionChain, Next: f.authority}
	value.Next.Signature = ""
	value.Next.MaximumJobs += 4096
	nativeRenewalTestSign(t, &value)
	reference := nativeRenewalTestWrite(t, filepath.Join(filepath.Dir(f.policy), "producer-renewal.json"), value)
	f.source.policy.Execution.Producer.Renewals = []planFileReference{reference}
	return value
}

func nativeRenewalTestPublicNext(t *testing.T, f *nativeProducerPublicFixture, base context.Context, retained *nativeExecutionProducerState, number uint64) {
	t.Helper()
	f.ctx = context.WithValue(base, nativeProducerStateKey{}, retained)
	f.source.policy.From = retained.Cursor
	f.source.policy.Through = economicEmissionBoundary{Number: number, Hash: f.source.chain.byHeight[number]}
	nativeRenewalTestWrite(t, f.policy, f.source.policy)
}

func TestNativeProducerRenewalPublicKeepsOldCompletionsAndDescendantWindow(t *testing.T) {
	f := nativeProducerPublicFixtureFrom(t, true)
	base := f.ctx
	first, code, issue := f.command(t)
	if code != 0 || first.ExecutionProducer == nil {
		t.Fatal("original public producer baseline failed", code, issue)
	}
	retained := first.ExecutionProducer
	original, err := os.ReadFile(retained.Completion.Path)
	if err != nil {
		t.Fatal(err)
	}
	window, chain, completed := *retained.Window, retained.CompletionChain, retained.Completed
	review := nativeRenewalTestPublicReview(t, f, retained)
	for number := uint64(102); number <= 103; number++ {
		nativeRenewalTestPublicNext(t, f, base, retained, number)
		observation, code, issue := f.command(t)
		if code != 0 || observation.ExecutionProducer == nil || observation.ExecutionWindow == nil {
			t.Fatal("renewed public producer failed original continuation", number, code, issue)
		}
		retained = observation.ExecutionProducer
		references := f.source.policy.Execution.Producer.Renewals
		if retained.Completed != completed+number-101 || len(retained.AuthorityRevisions) != int(number-101) || retained.AuthorityHash != references[len(references)-1].Sha256 || *retained.Window != window || retained.Anchor.SetId != 9 || retained.Certified.Number != 104 || observation.ExecutionWindow.MinerAllocation != "0" {
			t.Fatal("renewal reset authority window, original count or amounts", retained)
		}
		raw, err := os.ReadFile(retained.Completion.Path)
		if err != nil {
			t.Fatal(err)
		}
		var completion nativeProducerCompletion
		if err := decodePlanJson(raw, &completion); err != nil || completion.Previous != chain {
			t.Fatal("renewal broke original completion chain", err)
		}
		chain = retained.CompletionChain
		if number == 102 {
			next := nativeProducerRenewal{Schema: nativeProducerRenewalSchema, Original: review.Original, Previous: references[0], Ordinal: 2, After: retained.Cursor, Completed: retained.Completed, CompletionChain: retained.CompletionChain, Next: review.Next}
			next.Next.MaximumJobs += 4096
			nativeRenewalTestSign(t, &next)
			ref := nativeRenewalTestWrite(t, filepath.Join(filepath.Dir(f.policy), "producer-renewal-2.json"), next)
			f.source.policy.Execution.Producer.Renewals = append(append([]planFileReference(nil), references...), ref)
		}
	}
	after, err := os.ReadFile(first.ExecutionProducer.Completion.Path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("renewal recaptured or rewrote original completion", err)
	}
	f.source.policy.Execution.Producer.Renewals = nil
	nativeRenewalTestPublicNext(t, f, base, retained, 104)
	before := mainnetNamespaceTest(t, f.source.policy.Execution.Directory)
	observation, code, issue := f.command(t)
	if code == 0 || observation.Complete || !strings.Contains(issue, "removed acknowledged") || !reflect.DeepEqual(before, mainnetNamespaceTest(t, f.source.policy.Execution.Directory)) {
		t.Fatal("removed signed renewal reused a writer or original accounting", code, issue)
	}
}

// The hook is on the real closing RPC, after immutable completion publication.
// It does not replace capture, proof verification or either Rust executable.
func nativeRenewalTestClosingFailure(t *testing.T, f *nativeProducerPublicFixture, number uint64) func() {
	t.Helper()
	originalUrl := f.source.client.url
	ctx, cancel := context.WithCancel(f.ctx)
	f.ctx = ctx
	var fired atomic.Bool
	client := &http.Client{Timeout: 300 * time.Second}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(request.Body, 1024*1024))
		if err != nil {
			t.Error(err)
			return
		}
		var call struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			t.Error(err)
			return
		}
		path := filepath.Join(f.source.policy.Execution.Directory, fmt.Sprintf("b%010d-%s", number, strings.TrimPrefix(f.source.chain.byHeight[number], "0x")), "complete.json")
		if call.Method == "chain_getFinalizedHead" {
			if _, err := os.Stat(path); err == nil && fired.CompareAndSwap(false, true) {
				cancel()
				http.Error(writer, "synthetic lost outer acknowledgement", http.StatusServiceUnavailable)
				return
			}
		}
		forward, err := http.NewRequestWithContext(request.Context(), http.MethodPost, originalUrl, bytes.NewReader(raw))
		if err != nil {
			t.Error(err)
			return
		}
		forward.Header.Set("Content-Type", "application/json")
		response, err := client.Do(forward)
		if err != nil {
			if request.Context().Err() == nil {
				t.Error(err)
			}
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(response.StatusCode)
		_, copyErr := io.Copy(writer, response.Body)
		if err := errors.Join(copyErr, response.Body.Close()); err != nil && request.Context().Err() == nil {
			t.Error(err)
		}
	}))
	f.source.client.url = server.URL
	stop := func() { cancel(); server.Close(); client.CloseIdleConnections(); f.source.client.url = originalUrl }
	t.Cleanup(stop)
	return func() {
		stop()
		if !fired.Load() {
			t.Fatal("closing acknowledgement barrier was never reached")
		}
	}
}

func TestNativeProducerRenewalPublicUnacknowledgedAdoptionReusesExactJob(t *testing.T) {
	f := nativeProducerPublicFixtureFrom(t, true)
	base := f.ctx
	first, code, issue := f.command(t)
	if code != 0 || first.ExecutionProducer == nil {
		t.Fatal("original producer failed before renewal", code, issue)
	}
	retained := first.ExecutionProducer
	nativeRenewalTestPublicReview(t, f, retained)
	nativeRenewalTestPublicNext(t, f, base, retained, 102)
	stop := nativeRenewalTestClosingFailure(t, f, 102)
	partial, code, issue := f.command(t)
	if code == 0 || partial.Complete || partial.ExecutionProducer != nil || len(partial.Blocks) != 1 || partial.Blocks[0].ExecutionOutcome == nil {
		t.Fatal("renewed completion did not reach the outer lost-ack barrier", code, issue)
	}
	stop()
	proofs, blocks := f.proofs.Load(), f.blocks.Load()
	path := filepath.Join(f.source.policy.Execution.Directory, fmt.Sprintf("b%010d-%s", 102, strings.TrimPrefix(f.source.chain.byHeight[102], "0x")), "complete.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	nativeRenewalTestPublicNext(t, f, base, retained, 102)
	completed, code, issue := f.command(t)
	if code != 0 || completed.ExecutionProducer == nil || completed.ExecutionProducer.Completed != 2 || len(completed.ExecutionProducer.AuthorityRevisions) != 1 || completed.ExecutionProducer.AuthorityHash != f.source.policy.Execution.Producer.Renewals[0].Sha256 || len(completed.Blocks) != 1 || !reflect.DeepEqual(partial.Blocks[0].ExecutionOutcome, completed.Blocks[0].ExecutionOutcome) {
		t.Fatal("lost renewal acknowledgement reset or changed original job", code, issue)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) || f.proofs.Load() != proofs || f.blocks.Load()-blocks != 1 {
		t.Fatal("renewal restart recaptured proof or rewrote original completion", err, f.proofs.Load()-proofs, f.blocks.Load()-blocks)
	}
}

func TestNativeProducerRenewalPublicDelayedReviewPreservesPendingOriginalOutcome(t *testing.T) {
	f := nativeProducerPublicFixtureFrom(t, true)
	base := f.ctx
	first, code, issue := f.command(t)
	if code != 0 || first.ExecutionProducer == nil {
		t.Fatal("original producer failed before delayed review", code, issue)
	}
	reviewed := first.ExecutionProducer
	nativeRenewalTestPublicNext(t, f, base, reviewed, 102)
	stop := nativeRenewalTestClosingFailure(t, f, 102)
	pending, code, issue := f.command(t)
	if code == 0 || pending.ExecutionProducer != nil || len(pending.Blocks) != 1 || pending.Blocks[0].ExecutionOutcome == nil {
		t.Fatal("old-scope job did not reach its unacknowledged completion", code, issue)
	}
	stop()
	path := filepath.Join(f.source.policy.Execution.Directory, fmt.Sprintf("b%010d-%s", 102, strings.TrimPrefix(f.source.chain.byHeight[102], "0x")), "complete.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	proofs := f.proofs.Load()
	revision := nativeRenewalTestPublicReview(t, f, reviewed)
	nativeRenewalTestPublicNext(t, f, base, reviewed, 102)
	recovered, code, issue := f.command(t)
	if code != 0 || recovered.ExecutionProducer == nil || len(recovered.ExecutionProducer.AuthorityRevisions) != 0 || recovered.ExecutionProducer.AuthorityHash != revision.Previous.Sha256 || len(recovered.Blocks) != 1 || !reflect.DeepEqual(pending.Blocks[0].ExecutionOutcome, recovered.Blocks[0].ExecutionOutcome) || f.proofs.Load() != proofs {
		t.Fatal("new review reinterpreted or recaptured pending old authority", code, issue)
	}
	for number := uint64(103); number <= 104; number++ {
		predecessor := recovered.ExecutionProducer
		nativeRenewalTestPublicNext(t, f, base, predecessor, number)
		recovered, code, issue = f.command(t)
		if code != 0 || recovered.ExecutionProducer == nil || len(recovered.ExecutionProducer.AuthorityRevisions) != 1 || recovered.ExecutionProducer.Completed != number-100 {
			t.Fatal("delayed review required a new signature after ordinary old-scope progress", number, code, issue)
		}
		ack := recovered.ExecutionProducer.AuthorityRevisions[0]
		if ack.After != revision.After || ack.Completed != 1 || ack.CompletionChain != revision.CompletionChain || ack.AdoptedAfter.Number != 102 || ack.AdoptedCompleted != 2 || ack.FirstCompletion == nil || recovered.ExecutionProducer.AuthorityHash != f.source.policy.Execution.Producer.Renewals[0].Sha256 {
			t.Fatal("delayed adoption changed reviewed ancestry or cumulative count", ack)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("delayed adoption rewrote original old-authority job", err)
	}
}
