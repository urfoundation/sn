package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type finalArchiveProbeFixture struct {
	substrate            FinalArchiveProbeResult
	evm                  FinalArchiveProbeResult
	err                  error
	depths               []uint64
	cancelAfterSubstrate context.CancelFunc
	cancelAfterEVM       context.CancelFunc
	evmCalls             int
}

func (f *finalArchiveProbeFixture) Substrate(_ context.Context, endpoint string, earliest ChainHead, futureDepth uint64) (FinalArchiveProbeResult, error) {
	f.depths = append(f.depths, futureDepth)
	if f.err != nil {
		return FinalArchiveProbeResult{}, f.err
	}
	result := f.substrate
	result.Endpoint = endpoint
	result.EarliestRequiredHead = earliest
	if earliest.Number <= futureDepth {
		return FinalArchiveProbeResult{}, errors.New("floor underflow")
	}
	result.HistoricalHead = ChainHead{Number: earliest.Number - futureDepth, Hash: finalTestHex(12)}
	result.RequiredDepthBlocks = result.FinalizedHead.Number - result.HistoricalHead.Number
	if f.cancelAfterSubstrate != nil {
		f.cancelAfterSubstrate()
	}
	return result, nil
}

func (f *finalArchiveProbeFixture) EVM(_ context.Context, endpoint, _ string, earliest, deployment ChainHead, futureDepth uint64) (FinalArchiveProbeResult, error) {
	f.evmCalls++
	f.depths = append(f.depths, futureDepth)
	if f.err != nil {
		return FinalArchiveProbeResult{}, f.err
	}
	result := f.evm
	result.Endpoint = endpoint
	result.EarliestRequiredHead = earliest
	result.DeploymentHead = deployment
	if earliest.Number <= futureDepth {
		return FinalArchiveProbeResult{}, errors.New("floor underflow")
	}
	result.HistoricalHead = ChainHead{Number: earliest.Number - futureDepth, Hash: finalTestHex(13)}
	result.RequiredDepthBlocks = result.FinalizedHead.Number - result.HistoricalHead.Number
	if f.cancelAfterEVM != nil {
		f.cancelAfterEVM()
	}
	return result, nil
}

func testArchivePublicManifest() *PublicDeploymentManifest {
	return &PublicDeploymentManifest{
		Schema: "urnetwork-sim-public-deployment-v1", DeploymentID: "archive-test", SubstrateRPC: "wss://substrate.example.test", EVMRPC: "https://evm.example.test",
		Contracts:     &ContractDeployment{CoordinatorProxy: common.HexToAddress("0x1000000000000000000000000000000000000001"), DeployBlock: 7_000, DeployBlockHash: finalTestHex(9)},
		SetupEvidence: map[string]json.RawMessage{"fleet_1_commitment": json.RawMessage(`{"schema":"urnetwork-fleet-commitment-evidence-v2","manifest_uri":"fleet-1.json","commitment_hash":"` + finalTestHex(8) + `","hotkey":"` + finalTestHex(7) + `","extrinsic_hash":"` + finalTestHex(6) + `","commitment_block":7000,"finalized_block":7000,"finalized_block_hash":"` + finalTestHex(19) + `"}`)},
	}
}

func testArchiveProbeFixture(depth uint64) *finalArchiveProbeFixture {
	return &finalArchiveProbeFixture{
		substrate: FinalArchiveProbeResult{
			FinalizedHead: ChainHead{Number: 10_000, Hash: finalTestHex(1)},
			MetadataHash:  "sha256:" + strings.Repeat("11", 32), EventsHash: "sha256:" + strings.Repeat("22", 32), ExactMetadataHash: "sha256:" + strings.Repeat("33", 32), ExactEventsHash: "sha256:" + strings.Repeat("44", 32),
		},
		evm: FinalArchiveProbeResult{
			FinalizedHead: ChainHead{Number: 10_000, Hash: finalTestHex(3)}, GenericStateHash: finalTestHex(4), ExactStateHash: finalTestHex(5),
			CodeHash: finalTestHex(6), CallResultHash: finalTestHex(7),
		},
	}
}

func TestFinalArchiveRetentionPreflightWritesExactDepthReceipt(t *testing.T) {
	const depth = uint64(2_100)
	fixture := testArchiveProbeFixture(depth)
	dir := t.TempDir()
	value, locator, err := runFinalArchiveRetentionPreflight(context.Background(), dir, testArchivePublicManifest(), 1_800, 300, func() time.Time {
		return time.Date(2026, 9, 2, 23, 30, 0, 123, time.UTC)
	}, fixture)
	if err != nil {
		t.Fatal(err)
	}
	const wantRequired = uint64(5_100)
	if value.RequiredDepthBlocks != wantRequired || len(fixture.depths) != 2 || fixture.depths[0] != depth || fixture.depths[1] != depth || !value.Passed {
		t.Fatalf("unexpected archive preflight: value=%+v depths=%v", value, fixture.depths)
	}
	wire, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(locator.URI)))
	if err != nil || uint64(len(wire)) != locator.SizeBytes || bytesSHA256(wire) != locator.ContentHash {
		t.Fatalf("archive preflight receipt mismatch: locator=%+v error=%v", locator, err)
	}
}

func TestFinalArchiveRetentionPreflightEnforcesMinimumAndFailsClosed(t *testing.T) {
	fixture := testArchiveProbeFixture(minimumFinalArchiveProbeDepthBlocks)
	if _, _, err := runFinalArchiveRetentionPreflight(context.Background(), t.TempDir(), testArchivePublicManifest(), 10, 20, time.Now, fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.depths[0] != minimumFinalArchiveProbeDepthBlocks {
		t.Fatalf("minimum archive depth=%d", fixture.depths[0])
	}
	failed := testArchiveProbeFixture(minimumFinalArchiveProbeDepthBlocks)
	failed.err = errors.New("state discarded")
	if _, _, err := runFinalArchiveRetentionPreflight(context.Background(), t.TempDir(), testArchivePublicManifest(), 1_000, 1_000, time.Now, failed); err == nil || !strings.Contains(err.Error(), "state discarded") {
		t.Fatalf("pruned archive was not a hard failure: %v", err)
	}
	short := testArchiveProbeFixture(minimumFinalArchiveProbeDepthBlocks - 1)
	short.substrate.FinalizedHead.Number = 6_999
	if _, _, err := runFinalArchiveRetentionPreflight(context.Background(), t.TempDir(), testArchivePublicManifest(), 1_000, 1_000, time.Now, short); err == nil || !strings.Contains(err.Error(), "insufficient") {
		t.Fatalf("short archive was not rejected: %v", err)
	}
}

func TestFinalArchiveRetentionPreflightCanceledContextWritesNoReceipt(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := runFinalArchiveRetentionPreflight(ctx, dir, testArchivePublicManifest(), 1_000, 1_000, time.Now, testArchiveProbeFixture(2_000)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled archive preflight returned %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "receipts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled archive preflight created receipt state: %v", err)
	}
}

func TestFinalArchiveRetentionPreflightCancellationFencesProbeAndWriteBoundaries(t *testing.T) {
	for _, test := range []struct {
		name               string
		configure          func(*finalArchiveProbeFixture, context.CancelFunc)
		wantEVMCalls       int
		wantSubstrateCalls int
	}{
		{
			name: "between-probes",
			configure: func(probe *finalArchiveProbeFixture, cancel context.CancelFunc) {
				probe.cancelAfterSubstrate = cancel
			},
			wantEVMCalls: 0, wantSubstrateCalls: 1,
		},
		{
			name: "before-write",
			configure: func(probe *finalArchiveProbeFixture, cancel context.CancelFunc) {
				probe.cancelAfterEVM = cancel
			},
			wantEVMCalls: 1, wantSubstrateCalls: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			probe := testArchiveProbeFixture(minimumFinalArchiveProbeDepthBlocks)
			test.configure(probe, cancel)
			if _, _, err := runFinalArchiveRetentionPreflight(ctx, stateDir, testArchivePublicManifest(), 1_000, 1_000, time.Now, probe); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled archive preflight returned %v", err)
			}
			if probe.evmCalls != test.wantEVMCalls || len(probe.depths) != test.wantSubstrateCalls+test.wantEVMCalls {
				t.Fatalf("probe calls depths=%v evm=%d", probe.depths, probe.evmCalls)
			}
			if _, err := os.Lstat(filepath.Join(stateDir, "receipts")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("canceled archive preflight created receipt state: %v", err)
			}
		})
	}
}

func TestFinalArchiveRetentionPreflightIncludesExistingEvidenceAge(t *testing.T) {
	public := testArchivePublicManifest()
	public.Contracts.DeployBlock = 8_000
	public.Contracts.DeployBlockHash = finalTestHex(21)
	public.SetupEvidence = nil
	stateDir := t.TempDir()
	root := filepath.Join(stateDir, "receipts", "postconditions")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	record := ActionPostcondition{
		Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: public.DeploymentID, PlanHash: finalTestHex(22), ActionID: "subnet.verify-owner", IntentHash: finalTestHex(23),
		SubstrateFinalized: ChainHead{Number: 6_000, Hash: finalTestHex(24)}, EVMFinalized: ChainHead{Number: 7_000, Hash: finalTestHex(25)},
	}
	wire, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "floor.json"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := testArchiveProbeFixture(2_500)
	fixture.substrate.FinalizedHead.Number = 10_000
	fixture.evm.FinalizedHead.Number = 10_000
	value, _, err := runFinalArchiveRetentionPreflight(context.Background(), stateDir, public, 2_000, 500, time.Now, fixture)
	if err != nil {
		t.Fatal(err)
	}
	if value.Substrate.EarliestRequiredHead != record.SubstrateFinalized || value.Substrate.HistoricalHead.Number != 3_500 || value.Substrate.RequiredDepthBlocks != 6_500 {
		t.Fatalf("Substrate floor/depth=%+v", value.Substrate)
	}
	if value.EVM.EarliestRequiredHead != record.EVMFinalized || value.EVM.HistoricalHead.Number != 4_500 || value.EVM.RequiredDepthBlocks != 5_500 || value.RequiredDepthBlocks != 6_500 {
		t.Fatalf("EVM/top floor/depth=%+v total=%d", value.EVM, value.RequiredDepthBlocks)
	}
}

func TestFinalArchiveRetentionPreflightRejectsFloorUnderflowAndDepthOverflow(t *testing.T) {
	public := testArchivePublicManifest()
	public.Contracts.DeployBlock = 1_999
	public.Contracts.DeployBlockHash = finalTestHex(31)
	public.SetupEvidence = map[string]json.RawMessage{"fleet_1_commitment": json.RawMessage(`{"schema":"urnetwork-fleet-commitment-evidence-v2","manifest_uri":"fleet-1.json","commitment_hash":"` + finalTestHex(8) + `","hotkey":"` + finalTestHex(7) + `","extrinsic_hash":"` + finalTestHex(6) + `","commitment_block":1999,"finalized_block":1999,"finalized_block_hash":"` + finalTestHex(30) + `"}`)}
	if _, _, err := runFinalArchiveRetentionPreflight(context.Background(), t.TempDir(), public, 1_500, 500, time.Now, testArchiveProbeFixture(2_000)); err == nil || !strings.Contains(err.Error(), "underflow") {
		t.Fatalf("evidence-floor underflow was not rejected: %v", err)
	}
	if _, _, err := runFinalArchiveRetentionPreflight(context.Background(), t.TempDir(), testArchivePublicManifest(), ^uint64(0), 1, time.Now, testArchiveProbeFixture(2_000)); err == nil || !strings.Contains(err.Error(), "overflows") {
		t.Fatalf("depth overflow was not rejected: %v", err)
	}
}

func TestFinalArchiveEvidenceFloorsKeepNativeAndEVMHashDomainsSeparate(t *testing.T) {
	public := testArchivePublicManifest()
	public.Contracts.DeployBlock = 8_000
	public.Contracts.DeployBlockHash = finalTestHex(41)
	public.SetupEvidence = map[string]json.RawMessage{
		"fleet_1_commitment": json.RawMessage(`{"schema":"urnetwork-fleet-commitment-evidence-v2","manifest_uri":"fleet-1.json","commitment_hash":"` + finalTestHex(42) + `","hotkey":"` + finalTestHex(43) + `","extrinsic_hash":"` + finalTestHex(44) + `","commitment_block":6000,"finalized_block":6000,"finalized_block_hash":"` + finalTestHex(45) + `"}`),
		"fleet_1_binding_1":  json.RawMessage(`{"schema":"urnetwork-fleet-binding-evidence-v1","client_id":"0x01","client_key":"0x02","fleet_id":"0x03","hotkey":"0x04","generation":1,"valid_from_epoch":1,"valid_to_epoch":2,"commitment_hash":"` + finalTestHex(42) + `","binding_digest":"` + finalTestHex(46) + `","client_signature":"0x05","hotkey_signature":"0x06","transaction_hash":"` + finalTestHex(47) + `","block_number":6000,"block_hash":"` + finalTestHex(48) + `","uid":1}`),
	}
	native, evm, err := finalArchiveEvidenceFloors(t.TempDir(), public)
	if err != nil {
		t.Fatal(err)
	}
	if native != (ChainHead{Number: 6_000, Hash: finalTestHex(45)}) {
		t.Fatalf("native floor=%+v", native)
	}
	if evm != (ChainHead{Number: 6_000, Hash: finalTestHex(48)}) {
		t.Fatalf("EVM floor=%+v", evm)
	}
}

func TestFinalArchiveRetentionPreflightVerifierBindsHashAndCanonicalTime(t *testing.T) {
	value, _, err := runFinalArchiveRetentionPreflight(context.Background(), t.TempDir(), testArchivePublicManifest(), 1_800, 300, func() time.Time {
		return time.Date(2026, 9, 2, 23, 30, 0, 123, time.UTC)
	}, testArchiveProbeFixture(2_100))
	if err != nil {
		t.Fatal(err)
	}
	tampered := *value
	tampered.EvidenceHash = finalTestHex(49)
	if err := verifyFinalArchiveRetentionPreflight(&tampered); err == nil || !strings.Contains(err.Error(), "evidence hash differs") {
		t.Fatalf("tampered receipt hash was accepted: %v", err)
	}
	tampered = *value
	tampered.GeneratedAt = "2026-09-03T00:30:00.000000123+01:00"
	tampered.EvidenceHash, err = finalArchiveRetentionPreflightHash(&tampered)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalArchiveRetentionPreflight(&tampered); err == nil || !strings.Contains(err.Error(), "timestamp") {
		t.Fatalf("non-canonical receipt timestamp was accepted: %v", err)
	}
}

func TestFinalCompositeArchiveSpanCoversBothPhasesAndDiscardedBoundaries(t *testing.T) {
	cfg := testResolvedConfig(t)
	span, err := FinalCompositeArchiveSpan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	const want = uint64(3_570)
	if span != want {
		t.Fatalf("composite archive span=%d, want %d", span, want)
	}
	fixture := testArchiveProbeFixture(want + 500)
	if _, _, err := runFinalArchiveRetentionPreflight(context.Background(), t.TempDir(), testArchivePublicManifest(), span, 500, time.Now, fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.depths) != 2 || fixture.depths[0] != want+500 || fixture.depths[1] != want+500 {
		t.Fatalf("composite probe depths=%v", fixture.depths)
	}
}
