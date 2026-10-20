//go:build linux || darwin

// The settlement v2 audit as `validator run` wires it: a release
// configuration carrying the settlement_v2 block reaches the shared resolver
// and builds the ledger audit, which starts only once the release runtime has
// confirmed the contracts.
package validator

import (
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// config is a release configuration carrying the settlement_v2 block for the
// fixture's chain, operator endpoint and state directory.
func (self *settlementV2AuditTestFixture) config() *ReleaseConfig {
	return &ReleaseConfig{ChainID: settlementV2TestChainID, RPC: []string{self.chain.url}, DeployBlock: 1, StateDir: self.stateDir,
		Operators:    []OperatorConfig{{NoID: settlementV2AuditTestNoID, APIURL: self.objects.url}},
		SettlementV2: &ReleaseSettlementV2Config{Settlement: strings.ToLower(settlementV2TestSettlement.Hex()), Vault: strings.ToLower(settlementV2TestVault.Hex())}}
}

// wired builds the audit `validator run` builds from cfg and its auditor key,
// confirmed by runtime.
func (self *settlementV2AuditTestFixture) wired(t *testing.T, cfg *ReleaseConfig, runtime *releaseRuntimeV2) *settlementV2Audit {
	t.Helper()
	ctx := WithSettlementV2RuntimeOptions(t.Context(), SettlementV2RuntimeOptions{AuditorKeyFile: self.keyFile, LedgerMaxBytes: DefaultLedgerMaxBytes})
	audit, err := newReleaseSettlementV2Audit(ctx, cfg, self.client, runtime.settlementV2Confirmed)
	if err != nil || audit == nil {
		t.Fatalf("the configured audit: %v", err)
	}
	audit.random = settlementV2AuditTestRandom()
	return audit
}

// confirmSettlementV2AuditTest has the runtime pass its settlement_v2
// identity check, as its first advance does, and the audit take that
// confirmation.
func confirmSettlementV2AuditTest(t *testing.T, runtime *releaseRuntimeV2, audit *settlementV2Audit) {
	t.Helper()
	release, err := runtime.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	runtime.settlementV2Verified = true
	release()
	if err := audit.confirmed(t.Context()); err != nil || audit.confirm != nil {
		t.Fatalf("a confirmed runtime: %v", err)
	}
}

// VA-1: the settlement_v2 block of a release configuration reaches live
// steering and replay, and with replay the production row, through
// releaseSettlementV2Addresses. From the pool's first v2 epoch both resolve
// vault2's immutable pool hotkey and agree; without the block both keep
// operatorAt and read no v2 contract.
func TestSettlementV2ConfiguredBlockReachesSteeringAndReplay(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2ResolverTestFixture(t)
	fixture.registerPool(1, 1)
	cfg := validReleaseConfig(t)
	cfg.SettlementV2 = &ReleaseSettlementV2Config{Settlement: strings.ToLower(fixture.addresses.Settlement.Hex()), Vault: strings.ToLower(fixture.addresses.Vault.Hex())}
	resolve := func(cfg *ReleaseConfig) ([]ReleasePoolMeasurement, []ReleasePoolMeasurement) {
		t.Helper()
		live, err := fixture.steer(t, releaseSettlementV2Addresses(cfg))
		if err != nil {
			t.Fatal(err)
		}
		replayed, err := fixture.replay(t, releaseSettlementV2Addresses(cfg))
		if err != nil {
			t.Fatal(err)
		}
		return live, replayed
	}
	want := settlementV2TestV1Pools()
	want[0] = ReleasePoolMeasurement{NoID: 1, UID: 3, PoolHotkey: releaseHex32(settlementV2TestPoolHotkey)}
	if live, replayed := resolve(&cfg); !slices.Equal(live, want) || !slices.Equal(replayed, live) || len(fixture.v2Reads("settlementAt")) != 2 {
		t.Fatalf("with the block: live %+v, replayed %+v", live, replayed)
	}
	reads := len(fixture.v2Reads("pools"))
	cfg.SettlementV2 = nil
	if live, replayed := resolve(&cfg); !slices.Equal(live, settlementV2TestV1Pools()) || !slices.Equal(replayed, live) || len(fixture.v2Reads("pools")) != reads {
		t.Fatalf("without the block: live %+v, replayed %+v", live, replayed)
	}
}

// VA-3: a validator whose configuration carries the settlement_v2 block
// builds its ledger audit from it, with `validator run`'s auditor key and
// each operator's api_url, and keeps custody and send records under
// state_dir. The audit runs no pass until the runtime's first advance has
// confirmed the contracts, and waiting for the runtime ends with its context;
// then it approves exactly the pending root before its deadline.
func TestSettlementV2AuditFromConfigApprovesOnceConfirmed(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	f := settlementV2AuditTestF(t)
	p := settlementV2AuditTestP(t, f)
	fixture.useScenario(t, f, p.Commit, p.Objects, true, 610)
	runtime := &releaseRuntimeV2{gate: make(chan struct{}, 1)}
	audit := fixture.wired(t, fixture.config(), runtime)
	if err := audit.confirmed(t.Context()); !errors.Is(err, errSettlementV2Unconfirmed) || audit.confirm == nil || fixture.chain.count("pools") != 0 {
		t.Fatalf("an unconfirmed runtime: %v", err)
	}
	release, err := runtime.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	if err := audit.confirmed(ended); !errors.Is(err, context.Canceled) || audit.confirm == nil {
		t.Fatalf("waiting for a busy runtime: %v", err)
	}
	release()
	confirmSettlementV2AuditTest(t, runtime, audit)
	if err := audit.audit(t.Context(), audit.pools[0]); err != nil {
		t.Fatal(err)
	}
	fixture.requireApproved(t, fixture.sentCalls(t), 2, p.Commit)
	if _, err := os.Stat(filepath.Join(fixture.stateDir, "settlement-v2-ledger", "audits", "1-2.json")); err != nil {
		t.Fatal("no audit record under state_dir", err)
	}
	if entries, err := os.ReadDir(filepath.Join(fixture.stateDir, "settlement-v2-sends")); err != nil || len(entries) != 1 {
		t.Fatal("no send record under state_dir", err)
	}
}

// VA-8: through the configured audit, an account another demander already
// demanded against the protected root counts as done, keyed by root index:
// with the latest root's objects withheld the audit protects root 0, sends
// its other demands naming root 0, and still demands the account whose only
// earlier demand named root 1.
func TestSettlementV2AuditFromConfigDemandsByRootIndex(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	r, _, _ := fixture.useOlderScenario(t, true, nil)
	id := big.NewInt(settlementV2AuditTestNoID)
	for _, demand := range []settlementV2TestDemand{{noID: settlementV2AuditTestNoID, seq: 3, rootIndex: 0, index: 2}, {noID: settlementV2AuditTestNoID, seq: 3, rootIndex: 1, index: 1}} {
		fixture.chain.addLog(t, settlementV2VaultABI, settlementV2TestVault, "PathDemanded", 710, id, demand.seq, new(big.Int).SetUint64(demand.rootIndex), new(big.Int).SetUint64(demand.index), common.Address{0xd2})
		fixture.chain.mutate(func(state *settlementV2TestState) {
			state.demanded[demand] = settlementV2TestDemandOpen
		})
	}
	fixture.chain.advanceTo(730)
	runtime := &releaseRuntimeV2{gate: make(chan struct{}, 1)}
	audit := fixture.wired(t, fixture.config(), runtime)
	confirmSettlementV2AuditTest(t, runtime, audit)
	if err := audit.audit(t.Context(), audit.pools[0]); err != nil {
		t.Fatal(err)
	}
	record := fixture.record(t, 3)
	want := settlementV2AuditTestWhole(t, r.Accounts)
	if record.Decision != settlementV2DecisionDemand || record.RootIndex != 0 || !record.Done || record.Outcome != "sent" || !slices.Equal(record.Demands, want) {
		t.Fatalf("record %+v", record)
	}
	calls := fixture.sentCalls(t)
	if len(calls) != 3 {
		t.Fatalf("%d demands sent, want 3", len(calls))
	}
	for k, index := range slices.DeleteFunc(want, func(index uint64) bool { return index == 2 }) {
		requireSettlementV2AuditOpening(t, calls[k], r, 0, index)
	}
}

// VA-2: the configured audit is not built when custody cannot live under
// state_dir, the auditor key cannot be read, or an operator's api_url does
// not parse; a configuration without the block builds none.
func TestSettlementV2AuditFromConfigRefusals(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2AuditTestFixture(t)
	runtime := &releaseRuntimeV2{gate: make(chan struct{}, 1)}
	options := SettlementV2RuntimeOptions{AuditorKeyFile: fixture.keyFile, LedgerMaxBytes: DefaultLedgerMaxBytes}
	blocked := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		change func(*ReleaseConfig, *SettlementV2RuntimeOptions)
		want   string
	}{
		"state_dir":    {func(cfg *ReleaseConfig, _ *SettlementV2RuntimeOptions) { cfg.StateDir = blocked }, "not a directory"},
		"auditor key":  {func(_ *ReleaseConfig, options *SettlementV2RuntimeOptions) { options.AuditorKeyFile = blocked + ".key" }, "--ledger_auditor_key_file"},
		"api_url":      {func(cfg *ReleaseConfig, _ *SettlementV2RuntimeOptions) { cfg.Operators[0].APIURL = "http://[::1" }, "settlement v2 object endpoint"},
		"no v2 config": {func(cfg *ReleaseConfig, _ *SettlementV2RuntimeOptions) { cfg.SettlementV2 = nil }, ""},
	} {
		cfg, options := fixture.config(), options
		c.change(cfg, &options)
		audit, err := newReleaseSettlementV2Audit(WithSettlementV2RuntimeOptions(t.Context(), options), cfg, fixture.client, runtime.settlementV2Confirmed)
		if audit != nil || c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Fatalf("%s: audit %v, %v", name, audit != nil, err)
		}
	}
	// Without an auditor key the audit decides and sends nothing.
	cfg := fixture.config()
	audit, err := newReleaseSettlementV2Audit(t.Context(), cfg, fixture.client, runtime.settlementV2Confirmed)
	if err != nil || audit == nil || audit.sender != nil || audit.maxBytes != DefaultLedgerMaxBytes || len(audit.pools) != 1 || audit.pools[0].noID != settlementV2AuditTestNoID ||
		audit.pools[0].objects.(*settlementV2Objects).endpoint.String() != fixture.objects.url+"/sn/ledger/object" {
		t.Fatalf("an audit without an auditor key: %v", err)
	}
}

// VA-9: under the adopted configuration the ledger audit takes the
// confirmation the runtime's first advance made, reading no contract
// identity itself, and its pass over pools vault2 has not registered keeps
// its custody beside the evidence state. The runtime then restarts on that
// disk, and CLOSED_CENSUS still reads both closed epochs back under the
// original activations.
func TestSettlementV2AuditBesideClosedCensusAfterAdoption(t *testing.T) {
	fixture := newReleaseRuntimeV2TestFixture(t)
	closed := map[uint64]uint64{7: 1501, 8: 2001}
	advance := func(epoch uint64) {
		t.Helper()
		block := closed[epoch]
		if err := fixture.runtime.advance(t.Context(), &ReleaseSnapshot{Epoch: new(big.Int).SetUint64(epoch + 1), BlockNumber: block, BlockHash: fixture.startup.blocks[block]}); err != nil {
			t.Fatal(err)
		}
	}
	advance(7)
	fixture.startup.reopen(t)
	block := settlementV2TestBlock
	fixture.startup.cfg.SettlementV2 = &block
	answers := settlementV2TestAnswers(fixture.startup.cfg.Coordinator)
	unregistered, err := settlementV2VaultABI.Methods["pools"].Outputs.Pack(stabi.ISTSettlementVault2Pool{})
	if err != nil {
		t.Fatal(err)
	}
	for _, operator := range fixture.startup.cfg.Operators {
		answers[block.Vault+"/"+common.Bytes2Hex(settlementV2Vault.PackPools(new(big.Int).SetUint64(operator.NoID)))] = unregistered
	}
	fixture.evm.serveSettlementV2(answers)
	fixture.start(t)
	advance(8)
	audit, err := newReleaseSettlementV2Audit(t.Context(), &fixture.startup.cfg, fixture.startup.chain, fixture.runtime.settlementV2Confirmed)
	if err != nil || audit == nil {
		t.Fatal("the adopted configuration builds no audit", err)
	}
	if err := audit.confirmed(t.Context()); err != nil || fixture.evm.settlementV2ReadCount() != 3 {
		t.Fatal("the audit did not take the runtime's confirmation", err, fixture.evm.settlementV2ReadCount())
	}
	for _, pool := range audit.pools {
		if err := audit.audit(t.Context(), pool); err != nil {
			t.Fatal(err)
		}
	}
	if fixture.evm.settlementV2ReadCount() != 3+len(audit.pools) {
		t.Fatal("the audit did not read each pool once", fixture.evm.settlementV2ReadCount())
	}
	if info, err := os.Stat(filepath.Join(fixture.startup.cfg.StateDir, "settlement-v2-ledger")); err != nil || !info.IsDir() {
		t.Fatal("no custody under state_dir", err)
	}
	fixture.startup.reopen(t)
	fixture.start(t)
	for epoch, finalized := range closed {
		path, err := ValidatorEvidencePublicationV2ManifestPath(fixture.startup.cfg.StateDir, epoch)
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := ReadValidatorEvidencePublicationV2Manifest(t.Context(), path, fixture.startup.cfg.EvidenceV2.Bounds.MaxClosureBytes, 2)
		if err != nil {
			t.Fatal(err)
		}
		options := ValidatorEvidencePublicationV2ReadOptions{Origins: fixture.origins, Bounds: fixture.startup.cfg.EvidenceV2.Bounds, Window: protocol.ValidatorEvidenceWindow{Epoch: epoch, StartBlock: 1001 + 500*(epoch-7), EndBlock: 1501 + 500*(epoch-7), FinalizedBlock: finalized}}
		for _, input := range fixture.startup.inputs {
			options.Activations = append(options.Activations, input.Context.Activation)
		}
		if publication, err := ReadValidatorEvidencePublicationV2(t.Context(), manifest, options); err != nil || len(publication.Members) != 2 {
			t.Fatalf("epoch %d closed census beside the audit: %v", epoch, err)
		}
	}
}
