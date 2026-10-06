//go:build linux || darwin

// A separately signed production config and actual hash-pinned coordinator
// bytes must agree on the first mainnet epoch without an accelerated prelude.
package validator

import (
	"math/big"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Both semantic readers consume a real Evm snapshot. Production runtime
// admission still uses the existing independently signed exact-artifact gate;
// this fixture grants neither a signer nor a live network identity.
func newReleaseMainnetCadenceTestFixture(t *testing.T) (*productionRuntimeTestFixture, *releaseDecisionV2TestFixture) {
	t.Helper()
	authority := newProductionRuntimeTestFixture(t, false)
	policy := &authority.cfg.Policy
	policy.EffectiveEpoch = 0
	policy.ProductionCadence = protocol.CadenceSnapshot{AfterAcceleratedEpochs: 0, EpochBlocks: 50_400, RootCommitWindowBlocks: 1_200, FinalizeOffsetBlocks: 14_400, CloseGraceBlocks: 120}
	policy.Settlement.EpochBlocks, policy.Settlement.RootCommitWindowBlocks = 50_400, 1_200
	policy.Settlement.FinalizeOffsetBlocks, policy.Settlement.CloseGraceBlocks = 14_400, 120
	var err error
	authority.cfg.PolicyHash, err = policy.HashHex()
	if err != nil {
		t.Fatalf("steady mainnet policy cannot enter the actual production loader: %v", err)
	}
	authority.approval.Proposal.ParentPolicyHash, err = policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	authority.resignAndLoad(t)
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), authority.rpc.native, authority.cfg, mainnetRuntimeTestBlock(150)); err != nil {
		t.Fatal(err)
	}

	fixture := newReleaseDecisionV2TestFixture(t)
	fixture.query.policy = authority.cfg.Policy
	fixture.query.domain.ChainID, fixture.query.domain.Netuid = authority.cfg.ChainID, authority.cfg.Netuid
	fixture.query.domain.PolicyHash = authority.approval.Proposal.ParentPolicyHash
	fixture.query.domain.GenesisHash = [32]byte(authority.rpc.genesis)
	fixture.chain.chainId = new(big.Int).SetUint64(authority.cfg.ChainID)
	fixture.finalized = 150
	fixture.blocks = map[uint64][32]byte{10: {0x10}, 149: {0x71}, 150: {0x72}}
	fixture.query.boundary = AttemptBoundary{SettlementEpoch: 0, EVMBlock: 150, EVMBlockHash: releaseHex32(fixture.blocks[150])}
	coordinator := fixture.chain.coordinator
	fixture.set(t, "currentEpoch", coordinator.PackCurrentEpoch(), big.NewInt(0))
	fixture.set(t, "netuid", coordinator.PackNetuid(), authority.cfg.Netuid)
	fixture.set(t, "policyAt", coordinator.PackPolicyAt(big.NewInt(0)), releaseRateAmendmentTestSnapshot(t, authority.cfg.Policy, 0, 10))
	return authority, fixture
}

// This crosses the public signed config parser, current producer capability,
// actual finalized HTTP snapshot, and both unchanged cadence consumers.
func TestReleaseMainnetSteadyCadenceAdmitsActualEpochZeroSnapshot(t *testing.T) {
	authority, fixture := newReleaseMainnetCadenceTestFixture(t)
	snapshot, err := fixture.chain.ReleaseSnapshotContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	steerer := &ReleaseSteerer{cfg: authority.cfg, chain: fixture.chain, native: authority.rpc.native}
	if err := steerer.validatePinnedChains(t.Context(), snapshot, &crv4.EpochScheduleState{CurrentBlock: 150}, mainnetRuntimeTestBlock(150)); err != nil {
		t.Fatalf("actual epoch-zero mainnet snapshot cannot enter current steering: %v", err)
	}
	if err := validateReleaseDecisionChainV2Policy(fixture.query, snapshot.Policy); err != nil {
		t.Fatalf("actual epoch-zero mainnet snapshot cannot enter historical decision replay: %v", err)
	}
	if snapshot.Epoch.Sign() != 0 || snapshot.Policy.EffectiveEpoch != 0 || snapshot.Policy.EpochBlocks != 50_400 || fixture.count("currentEpoch") != 1 || fixture.count("policyAt") != 1 || fixture.count("netuid") != 1 {
		t.Fatal("epoch-zero admission bypassed the real finalized coordinator readers")
	}
	if authority.cfg.Policy.ProductionCadence.AfterAcceleratedEpochs != 0 || authority.cfg.SchemaVersion != ReleaseMainnetProductionSchemaVersion {
		t.Fatal("steady cadence was inferred or downgraded to another approval schema")
	}
}

// Each fault changes independently encoded on-chain bytes, while keeping the
// signed local policy fixed. Equal initial/production fields cannot bypass
// either consumer's policy identity, windows, caps or retention rules.
func TestReleaseMainnetSteadyCadenceRejectsChangedActualSnapshot(t *testing.T) {
	authority, fixture := newReleaseMainnetCadenceTestFixture(t)
	steerer := &ReleaseSteerer{cfg: authority.cfg, chain: fixture.chain, native: authority.rpc.native}
	for _, testCase := range []struct {
		name   string
		change func(*stabi.STCoordinatorPolicySnapshot)
	}{
		{name: "policy-hash", change: func(policy *stabi.STCoordinatorPolicySnapshot) { policy.PolicyHash[0] ^= 1 }},
		{name: "epoch", change: func(policy *stabi.STCoordinatorPolicySnapshot) { policy.EpochBlocks-- }},
		{name: "root", change: func(policy *stabi.STCoordinatorPolicySnapshot) { policy.RootCommitWindowBlocks-- }},
		{name: "finalize", change: func(policy *stabi.STCoordinatorPolicySnapshot) { policy.FinalizeOffsetBlocks-- }},
		{name: "close", change: func(policy *stabi.STCoordinatorPolicySnapshot) { policy.CloseGraceBlocks-- }},
		{name: "claim-ttl", change: func(policy *stabi.STCoordinatorPolicySnapshot) { policy.ClaimTTLEpochs++ }},
		{name: "claim-grace", change: func(policy *stabi.STCoordinatorPolicySnapshot) { policy.ClaimGraceEpochs++ }},
		{name: "binding-horizon", change: func(policy *stabi.STCoordinatorPolicySnapshot) { policy.MaximumBindingValidityEpochs++ }},
		{name: "epoch-cap", change: func(policy *stabi.STCoordinatorPolicySnapshot) {
			policy.EpochDepositCapRao.Add(policy.EpochDepositCapRao, big.NewInt(1))
		}},
		{name: "campaign-cap", change: func(policy *stabi.STCoordinatorPolicySnapshot) {
			policy.CampaignDepositCapRao.Add(policy.CampaignDepositCapRao, big.NewInt(1))
		}},
	} {
		changed := releaseRateAmendmentTestSnapshot(t, authority.cfg.Policy, 0, 10)
		testCase.change(&changed)
		fixture.set(t, "policyAt", fixture.chain.coordinator.PackPolicyAt(big.NewInt(0)), changed)
		snapshot, err := fixture.chain.ReleaseSnapshotContext(t.Context())
		if err != nil {
			t.Fatalf("%s failed before the actual snapshot was decoded: %v", testCase.name, err)
		}
		if err := steerer.validatePinnedChains(t.Context(), snapshot, &crv4.EpochScheduleState{CurrentBlock: 150}, mainnetRuntimeTestBlock(150)); err == nil {
			t.Fatalf("%s changed actual policy acquired current steering authority", testCase.name)
		}
		if err := validateReleaseDecisionChainV2Policy(fixture.query, snapshot.Policy); err == nil {
			t.Fatalf("%s changed actual policy acquired historical decision authority", testCase.name)
		}
	}
	if fixture.count("policyAt") != 10 {
		t.Fatal("snapshot refusals bypassed the actual HTTP policy reader")
	}
}

// Recomputing the canonical policy hash cannot silently replace the external
// production approval, even when the new four-window profile is well formed.
func TestReleaseMainnetSteadyCadenceKeepsIndependentConfigApproval(t *testing.T) {
	authority, _ := newReleaseMainnetCadenceTestFixture(t)
	original := authority.path
	changed := *authority.cfg
	changed.Policy.Settlement.RootCommitWindowBlocks++
	changed.Policy.ProductionCadence.RootCommitWindowBlocks++
	var err error
	changed.PolicyHash, err = changed.Policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadReleaseConfig(writeReleaseConfig(t, changed)); err == nil || loaded != nil {
		t.Fatal("steady mainnet profile replaced its independently signed complete configuration")
	}
	if _, err := LoadReleaseConfig(original); err != nil {
		t.Fatalf("rejected cadence change invalidated original approved config: %v", err)
	}
}
