//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

type monitorEvmCatalogFixture struct {
	archive *monitorEvmArchiveFixture
	key     ed25519.PrivateKey
	request monitorEvmCatalogRequest
}

func newMonitorEvmCatalogFixture(t *testing.T, peer bool) *monitorEvmCatalogFixture {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x4a}, ed25519.SeedSize))
	archive := newMonitorEvmArchiveFixtureWithPolicy(t, peer, func(policy *monitorEconomicEvmPolicy) {
		policy.HistoryCatalog = &monitorHistoryCatalogPolicy{Schema: monitorHistoryCatalogPolicySchema,
			ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), ReviewSha256: "sha256:" + strings.Repeat("9", 64),
			InitialCapacity: monitorHistoryCapacity{Segments: 128, CatalogBytes: 64 * 1024, HeldReaders: 128}}
	})
	return &monitorEvmCatalogFixture{archive: archive, key: key, request: monitorEvmCatalogRequest{
		Schema: monitorEvmCatalogRequestSchema, Expected: archive.request.Expected, Policy: archive.evm.policy,
		Original: archive.request.Original, FormerWriterFence: archive.request.FormerWriterFence,
		Capacity: monitorHistoryCapacity{Segments: 256, CatalogBytes: 128 * 1024, HeldReaders: 256}, FutureSegments: 2}}
}

func (self *monitorEvmCatalogFixture) document(t *testing.T, name string, value any) planFileReference {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	path := filepath.Join(self.archive.metadata, name)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return planFileReference{Path: path, Sha256: monitorReadDigest(raw)}
}

func (self *monitorEvmCatalogFixture) planArgs(t *testing.T, request monitorEvmCatalogRequest) []string {
	t.Helper()
	reference := self.document(t, "catalog-request.json", request)
	return []string{"monitor-evm-catalog", "plan", "--request", reference.Path, "--request-sha256", reference.Sha256}
}

func (self *monitorEvmCatalogFixture) plan(t *testing.T) monitorEvmCatalogPlan {
	t.Helper()
	before := mainnetNamespaceTest(t, filepath.Dir(self.archive.checkpoint))
	var output, diagnostic bytes.Buffer
	if code := runMainWithMonitorHooks(self.archive.evm.ctx, self.planArgs(t, self.request), &output, &diagnostic, nil, monitorServiceHooks{}); code != 0 {
		t.Fatal("public catalog plan did not reach the reviewed original owner", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(self.archive.checkpoint))) {
		t.Fatal("read-only catalog preview changed original custody")
	}
	var plan monitorEvmCatalogPlan
	if err := decodeMonitorHistoryInput(output.Bytes(), &plan); err != nil || plan.PlanHash != plan.hash() || plan.RestartAuthorized {
		t.Fatal("invalid public catalog plan", err, output.String())
	}
	return plan
}

// The synthetic signer consumes the actual exported message bytes. Test setup
// cannot silently use a different JSON/signature frame than the public producer.
func (self *monitorEvmCatalogFixture) approve(t *testing.T, plan monitorEvmCatalogPlan) monitorHistoryCatalogApproval {
	t.Helper()
	message, err := hex.DecodeString(plan.SigningBytes)
	if err != nil || len(message) == 0 {
		t.Fatal("public signing frame absent", err)
	}
	approval := monitorHistoryCatalogApproval{Schema: monitorHistoryCatalogApprovalSchema, Revision: plan.Revision,
		Signature: hex.EncodeToString(ed25519.Sign(self.key, message))}
	if err := approval.validate(self.archive.evm.policy.HistoryCatalog); err != nil {
		t.Fatal("actual exported frame could not be signed", err)
	}
	return approval
}

func (self *monitorEvmCatalogFixture) applyArgs(t *testing.T, plan monitorEvmCatalogPlan, approval monitorHistoryCatalogApproval) []string {
	t.Helper()
	planReference := self.document(t, "catalog-plan.json", plan)
	approvalReference := self.document(t, "catalog-approval.json", approval)
	return []string{"monitor-evm-catalog", "apply", "--plan", planReference.Path, "--plan-sha256", planReference.Sha256,
		"--approval", approvalReference.Path, "--approval-sha256", approvalReference.Sha256}
}

func (self *monitorEvmCatalogFixture) apply(t *testing.T, plan monitorEvmCatalogPlan, approval monitorHistoryCatalogApproval) {
	t.Helper()
	var output, diagnostic bytes.Buffer
	if code := runMainWithMonitorHooks(self.archive.evm.ctx, self.applyArgs(t, plan, approval), &output, &diagnostic, nil, monitorServiceHooks{}); code != 0 || output.Len() == 0 {
		t.Fatal("public independently signed catalog adoption failed", code, diagnostic.String())
	}
}

func TestMonitorEvmCatalogPublicSignedGrowthKeepsOriginalPendingRange(t *testing.T) {
	f := newMonitorEvmCatalogFixture(t, false)
	original := f.archive.evm.record(t)
	plan := f.plan(t)
	approval := f.approve(t, plan)
	f.apply(t, plan, approval)
	record := f.archive.evm.record(t)
	if record.State.Catalog == nil || len(record.State.Catalog.Revisions) != 1 || record.State.Catalog.Revisions[0] != approval || record.PolicyHash != original.PolicyHash {
		t.Fatal("signed original catalog authority was not retained")
	}
	record.State.Catalog = nil
	if !reflect.DeepEqual(record.State, original.State) || !reflect.DeepEqual(record.ResourceHistory, original.ResourceHistory) || !reflect.DeepEqual(record.Resources, original.Resources) || record.ResourceReviewSha256 != original.ResourceReviewSha256 {
		t.Fatal("resource revision changed original economics, pending range or runtime authority")
	}
	f.archive.resetRequest(t)
	_, archiveArgs := f.archive.plan(t)
	f.archive.apply(t, archiveArgs)
	run := f.archive.evm.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Cursor.Number != 13 || event.State.PendingThrough != nil || event.State.BatchCount != 2 || event.State.ObservedFeeCostWei == nil || *event.State.ObservedFeeCostWei != "84000" || event.State.ContractState == nil || event.State.ContractState.Counters["totalPaid"] != "15" {
		t.Fatal("signed growth restarted, lost or duplicated original pending economics", event)
	}
	run.stop(t)
	retained := f.archive.evm.record(t)
	if retained.State.Catalog == nil || retained.State.Catalog.Revisions[0] != approval {
		t.Fatal("ordinary runtime publication discarded original resource approval")
	}
}

func TestMonitorEvmCatalogPublicLostAcknowledgmentReusesExactApproval(t *testing.T) {
	f := newMonitorEvmCatalogFixture(t, false)
	plan := f.plan(t)
	approval := f.approve(t, plan)
	args := f.applyArgs(t, plan, approval)
	syncs := 0
	hooks := monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		if err := file.Sync(); err != nil {
			return err
		}
		if role == f.archive.evm.policy.Role && kind == "catalog-checkpoint" {
			syncs++
			return errors.New("injected catalog acknowledgment loss after real directory sync")
		}
		return nil
	}}
	var output, diagnostic bytes.Buffer
	if code := runMainWithMonitorHooks(f.archive.evm.ctx, args, &output, &diagnostic, nil, hooks); code == 0 || syncs != 1 || output.Len() != 0 {
		t.Fatal("publication uncertainty did not reach the actual directory sync", code, syncs, diagnostic.String())
	}
	f.apply(t, plan, approval)
	before := mainnetNamespaceTest(t, filepath.Dir(f.archive.checkpoint))
	f.apply(t, plan, approval)
	diagnostic.Reset()
	if code := runMainWithMonitorHooks(f.archive.evm.ctx, args, storagePreparationShortOutput{}, &diagnostic, nil, monitorServiceHooks{}); code == 0 || !strings.Contains(diagnostic.String(), "report not delivered") {
		t.Fatal("valid completed import did not reach the short-output boundary", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.archive.checkpoint))) {
		t.Fatal("completed capacity replay rewrote original owner or signed approval")
	}
	record := f.archive.evm.record(t)
	if record.State.Catalog == nil || len(record.State.Catalog.Revisions) != 1 || record.State.PendingThrough == nil || record.State.PendingThrough.Number != 13 {
		t.Fatal("lost acknowledgment consumed a second revision or lost pending progress")
	}
}

func TestMonitorEvmCatalogPublicRefusesForeignAndRewrittenAuthority(t *testing.T) {
	f := newMonitorEvmCatalogFixture(t, false)
	plan := f.plan(t)
	approval := f.approve(t, plan)
	// A complete positive baseline reaches the real importer before any fault.
	f.apply(t, plan, approval)
	before := mainnetNamespaceTest(t, filepath.Dir(f.archive.checkpoint))
	for _, fault := range []string{"signature", "key", "predecessor", "role", "original", "forecast", "signing-frame"} {
		candidate, imported := plan, approval
		switch fault {
		case "signature":
			imported.Signature = strings.Repeat("0", 128)
		case "key":
			policy := *candidate.Request.Policy.HistoryCatalog
			other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x5b}, ed25519.SeedSize))
			policy.ApprovalPublicKey = "0x" + hex.EncodeToString(other.Public().(ed25519.PublicKey))
			candidate.Request.Policy.HistoryCatalog = &policy
		case "predecessor":
			candidate.Revision.Previous = "sha256:" + strings.Repeat("1", 64)
		case "role":
			candidate.Revision.Role = "foreign-role"
		case "original":
			candidate.Revision.ProgressHash = "sha256:" + strings.Repeat("2", 64)
		case "forecast":
			candidate.Revision.RequiredBytes++
		case "signing-frame":
			candidate.SigningBytes = "00"
		}
		candidate.PlanHash = candidate.hash()
		var output, diagnostic bytes.Buffer
		code := runMainWithMonitorHooks(f.archive.evm.ctx, f.applyArgs(t, candidate, imported), &output, &diagnostic, nil, monitorServiceHooks{})
		if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "catalog") {
			t.Errorf("%s: foreign authority was admitted or failed elsewhere: %d %s", fault, code, diagnostic.String())
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.archive.checkpoint))) {
			t.Fatal("refused approval changed original physical custody", fault)
		}
	}
}

func TestMonitorEvmCatalogPublicRefusesLegacyEnrollmentAndPolicySwap(t *testing.T) {
	f := newMonitorEvmCatalogFixture(t, false)
	original := mainnetNamespaceTest(t, filepath.Dir(f.archive.checkpoint))
	request := f.request
	request.Policy.HistoryCatalog = nil
	var output, diagnostic bytes.Buffer
	if code := runMainWithMonitorHooks(f.archive.evm.ctx, f.planArgs(t, request), &output, &diagnostic, nil, monitorServiceHooks{}); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "opted-in authority") {
		t.Fatal("legacy policy acquired a new catalog key", code, diagnostic.String())
	}
	request = f.request
	policy := *request.Policy.HistoryCatalog
	policy.ReviewSha256 = "sha256:" + strings.Repeat("3", 64)
	request.Policy.HistoryCatalog = &policy
	output.Reset()
	diagnostic.Reset()
	if code := runMainWithMonitorHooks(f.archive.evm.ctx, f.planArgs(t, request), &output, &diagnostic, nil, monitorServiceHooks{}); code == 0 || output.Len() != 0 {
		t.Fatal("original independently reviewed policy was replaced", code, diagnostic.String())
	}
	if !reflect.DeepEqual(original, mainnetNamespaceTest(t, filepath.Dir(f.archive.checkpoint))) {
		t.Fatal("refused policy enrollment changed original custody")
	}
}

func TestMonitorEvmCatalogPublicBoundsAllForecastDimensions(t *testing.T) {
	f := newMonitorEvmCatalogFixture(t, false)
	_ = f.plan(t)
	before := mainnetNamespaceTest(t, filepath.Dir(f.archive.checkpoint))
	for _, fault := range []string{"segments", "readers", "metadata", "shrink", "zero-future", "future-metadata"} {
		request := f.request
		switch fault {
		case "segments":
			request.Capacity.Segments = maximumReviewedMonitorHistorySegments + 1
		case "readers":
			request.Capacity.HeldReaders = 128
		case "metadata":
			request.Capacity.CatalogBytes = maximumMonitorHistoryCatalogBytes + 1
		case "shrink":
			request.Capacity = request.Policy.HistoryCatalog.InitialCapacity
		case "zero-future":
			request.FutureSegments = 0
		case "future-metadata":
			request.FutureSegments = 64
		}
		var output, diagnostic bytes.Buffer
		code := runMainWithMonitorHooks(f.archive.evm.ctx, f.planArgs(t, request), &output, &diagnostic, nil, monitorServiceHooks{})
		if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "catalog") && !strings.Contains(diagnostic.String(), "history") {
			t.Errorf("%s: invalid capacity exported a signing proposal: %d %s", fault, code, diagnostic.String())
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.archive.checkpoint))) {
			t.Fatal("capacity refusal mutated original owner", fault)
		}
	}
}

func TestMonitorEvmCatalogPublicRefusesActiveOwnerAndCanceledImport(t *testing.T) {
	f := newMonitorEvmCatalogFixture(t, false)
	plan := f.plan(t)
	approval := f.approve(t, plan)
	owner, err := openMonitorHistorySnapshot(f.archive.evm.ctx, f.archive.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planMonitorEvmCatalog(f.archive.evm.ctx, f.request); !errors.Is(err, durablevolume.ErrBusy) {
		t.Fatal("active original writer admitted an offline revision", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	before := mainnetNamespaceTest(t, filepath.Dir(f.archive.checkpoint))
	canceled, cancel := context.WithCancel(f.archive.evm.ctx)
	cancel()
	var output, diagnostic bytes.Buffer
	if code := runMainWithMonitorHooks(canceled, f.applyArgs(t, plan, approval), &output, &diagnostic, nil, monitorServiceHooks{}); code == 0 || output.Len() != 0 {
		t.Fatal("canceled import changed original capacity", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.archive.checkpoint))) {
		t.Fatal("cancellation changed original snapshot or lease")
	}
	f.apply(t, plan, approval)
}

func TestMonitorEvmCatalogSignedPrefixSurvivesLaterCapacityAndRefusesDrop(t *testing.T) {
	f := newMonitorEvmCatalogFixture(t, false)
	first := f.plan(t)
	approval := f.approve(t, first)
	f.apply(t, first, approval)
	f.archive.resetRequest(t)
	f.request.Original, f.request.FormerWriterFence = f.archive.request.Original, f.archive.request.FormerWriterFence
	f.request.Capacity.Segments, f.request.Capacity.HeldReaders = 512, 512
	second := f.plan(t)
	secondApproval := f.approve(t, second)
	f.apply(t, second, secondApproval)
	record := f.archive.evm.record(t)
	if record.State.Catalog == nil || len(record.State.Catalog.Revisions) != 2 || record.State.Catalog.Revisions[0] != approval || record.State.Catalog.Revisions[1] != secondApproval || second.Revision.Previous != rootObjectHash(approval) {
		t.Fatal("monotonic growth discarded the original independently signed revision")
	}
	for _, fault := range []string{"drop", "reorder", "signature", "path"} {
		candidate := record
		candidate.State.Catalog = &monitorHistoryCatalogState{Revisions: append([]monitorHistoryCatalogApproval(nil), record.State.Catalog.Revisions...)}
		switch fault {
		case "drop":
			candidate.State.Catalog.Revisions = candidate.State.Catalog.Revisions[1:]
		case "reorder":
			candidate.State.Catalog.Revisions[0], candidate.State.Catalog.Revisions[1] = candidate.State.Catalog.Revisions[1], candidate.State.Catalog.Revisions[0]
		case "signature":
			candidate.State.Catalog.Revisions[0].Signature = strings.Repeat("0", 128)
		case "path":
			candidate.State.Catalog.Revisions[0].Revision.Original.Path += ".foreign"
		}
		raw, err := encodeMonitorEvmCheckpoint(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeMonitorEconomicEvmCheckpoint(raw, f.archive.evm.policy); err == nil {
			t.Errorf("%s: checksum alone admitted a changed signed prefix", fault)
		}
	}
	var output, diagnostic bytes.Buffer
	if code := runMainWithMonitorHooks(f.archive.evm.ctx, f.applyArgs(t, first, approval), &output, &diagnostic, nil, monitorServiceHooks{}); code == 0 || output.Len() != 0 {
		t.Fatal("an older capacity receipt reset a later approved generation", code, diagnostic.String())
	}
	if actual := f.archive.evm.record(t); !reflect.DeepEqual(actual, record) {
		t.Fatal("old receipt changed acknowledged capacity", diagnostic.String())
	}
}

func TestMonitorEvmCatalogLegacyCheckpointCannotEnrollAuthority(t *testing.T) {
	legacy := newMonitorEvmArchiveFixture(t, false)
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x4a}, ed25519.SeedSize))
	policy := legacy.evm.policy
	policy.HistoryCatalog = &monitorHistoryCatalogPolicy{Schema: monitorHistoryCatalogPolicySchema,
		ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)), ReviewSha256: "sha256:" + strings.Repeat("9", 64),
		InitialCapacity: monitorHistoryCapacity{Segments: 128, CatalogBytes: 64 * 1024, HeldReaders: 128}}
	f := &monitorEvmCatalogFixture{archive: legacy, key: key}
	fence := monitorHistoryWriterFence{Schema: monitorHistoryWriterFenceSchema, Original: legacy.request.Original, PolicyHash: policy.identityHash(), StoppedAndJoined: true}
	request := monitorEvmCatalogRequest{Schema: monitorEvmCatalogRequestSchema, Expected: legacy.request.Expected, Policy: policy,
		Original: legacy.request.Original, FormerWriterFence: f.document(t, "new-policy-fence.json", fence),
		Capacity: monitorHistoryCapacity{Segments: 256, CatalogBytes: 128 * 1024, HeldReaders: 256}, FutureSegments: 2}
	before := mainnetNamespaceTest(t, filepath.Dir(legacy.checkpoint))
	var output, diagnostic bytes.Buffer
	if code := runMainWithMonitorHooks(legacy.evm.ctx, f.planArgs(t, request), &output, &diagnostic, nil, monitorServiceHooks{}); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "checkpoint differs from its retained policy") {
		t.Fatal("existing legacy checkpoint admitted a newly asserted approver", code, diagnostic.String())
	}
	if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(legacy.checkpoint))) {
		t.Fatal("legacy enrollment refusal changed original custody")
	}
}

func TestMonitorEvmCatalogRequiresBothPhysicalForecastReserves(t *testing.T) {
	f := newMonitorEvmCatalogFixture(t, false)
	_ = f.plan(t)
	reference, _ := durablevolume.ReferenceFromContext(f.archive.evm.ctx)
	config, err := durablevolume.Load(reference)
	if err != nil {
		t.Fatal(err)
	}
	before := mainnetNamespaceTest(t, filepath.Dir(f.archive.checkpoint))
	for _, field := range []string{"bytes", "inodes"} {
		candidate := config
		candidate.Volumes = append([]durablevolume.VolumeSpec(nil), config.Volumes...)
		for index := range candidate.Volumes {
			if field == "bytes" {
				candidate.Volumes[index].MinAvailableBytes = 1
			} else {
				candidate.Volumes[index].MinAvailableInodes = 1
			}
		}
		input := f.document(t, "catalog-low-"+field+".json", candidate)
		ctx := durablevolume.WithReference(f.archive.evm.ctx, durablevolume.Reference{Path: input.Path, Sha256: input.Sha256})
		var output, diagnostic bytes.Buffer
		if code := runMainWithMonitorHooks(ctx, f.planArgs(t, f.request), &output, &diagnostic, nil, monitorServiceHooks{}); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "forecast floors") {
			t.Errorf("%s: insufficient physical margin was signed or failed elsewhere: %d %s", field, code, diagnostic.String())
		}
		if !reflect.DeepEqual(before, mainnetNamespaceTest(t, filepath.Dir(f.archive.checkpoint))) {
			t.Fatal("physical forecast refusal changed original custody", field)
		}
	}
}

// A valid revision near its payload bound must still be importable after the
// signature envelope is added. The protected long path is synthetic and local.
func TestMonitorEvmCatalogPublicImportsCompleteBoundedApprovalFrame(t *testing.T) {
	f := newMonitorEvmCatalogFixture(t, false)
	baseline := f.plan(t)
	var path string
	for count := 1; count <= 3000; count++ {
		parts := []string{f.archive.metadata}
		for remaining := count; remaining > 0; remaining -= min(remaining, 100) {
			parts = append(parts, strings.Repeat("\x01", min(remaining, 100)))
		}
		parts = append(parts, "writer-fence.json")
		candidate := baseline.Revision
		candidate.FormerWriterFence.Path = filepath.Join(parts...)
		revision, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := json.Marshal(monitorHistoryCatalogApproval{Schema: monitorHistoryCatalogApprovalSchema, Revision: candidate, Signature: strings.Repeat("0", 128)})
		if err != nil {
			t.Fatal(err)
		}
		if len(revision) <= maximumMonitorHistoryRevisionBytes && len(envelope)+1 > maximumMonitorHistoryRevisionBytes {
			path = candidate.FormerWriterFence.Path
			break
		}
	}
	if path == "" || len(path) >= 4096 {
		t.Fatal("synthetic signature-envelope boundary was not representable")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	fence, err := os.ReadFile(f.request.FormerWriterFence.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, fence, 0600); err != nil {
		t.Fatal(err)
	}
	f.request.FormerWriterFence = planFileReference{Path: path, Sha256: monitorReadDigest(fence)}
	plan := f.plan(t)
	approval := f.approve(t, plan)
	raw, err := json.Marshal(approval)
	if err != nil || len(raw)+1 <= maximumMonitorHistoryRevisionBytes || len(raw)+1 > 17*1024 {
		t.Fatal("actual emitted signature frame did not cross only the old import boundary", len(raw), err)
	}
	f.apply(t, plan, approval)
	retained := f.archive.evm.record(t)
	if retained.State.Catalog == nil || retained.State.Catalog.Revisions[0] != approval || retained.State.PendingThrough == nil || retained.State.PendingThrough.Number != 13 {
		t.Fatal("complete approval import lost original signature or pending range")
	}
}
