//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

func setEvidencePolicyGapTestPolicy(t *testing.T, fixture *evidenceRelayRpcFixture, epoch uint64, hash [32]byte) {
	t.Helper()
	parsed, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := parsed.Methods["policyAt"].Outputs.Pack(stabi.STCoordinatorPolicySnapshot{PolicyHash: hash, EffectiveEpoch: 7, EffectiveBlock: 1000, EpochBlocks: 100, EpochDepositCapRao: big.NewInt(13), CampaignDepositCapRao: big.NewInt(29)})
	if err != nil {
		t.Fatal(err)
	}
	key := common.Address(fixture.expected.Activation.Domain.Coordinator).Hex() + ":" + hexutil.Encode(stabi.NewSTCoordinator().PackPolicyAt(new(big.Int).SetUint64(epoch)))
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	fixture.responses[key] = encoded
}

func signedEvidencePolicyGapTestCutoff(t *testing.T, activation protocol.ValidatorEvidenceActivation) evidenceRelayPolicyGapActivation {
	t.Helper()
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x71})
	if err != nil {
		t.Fatal(err)
	}
	result := evidenceRelayPolicyGapActivation{Activation: activation}
	result.VPKSignature, err = activation.SignVPK(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x72}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := activation.Digest()
	if err != nil {
		t.Fatal(err)
	}
	result.HotkeySignature, err = hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// Real signed requests, a real private journal and block-pinned local RPC
// responses feed the production gap path. No verdict callback is injected.
func newEvidencePolicyGapTest(t *testing.T, install bool) (*evidenceRelayRuntime, *evidenceRelayRpcFixture, evidenceRelayPolicyGapActivation) {
	t.Helper()
	fixture := newEvidenceRelayRpcFixture(t, "success")
	activation := fixture.expected.Activation
	activation.Domain.PolicyHash, activation.Domain.Epoch = [32]byte{0x99}, 9
	activation.EVMBlock, activation.EVMHash = fixture.finalizedBlock, [32]byte(fixture.finalizedHash)
	activation.NativeBlock++
	activation.FirstSequence++
	activation.PriorRoot = [32]byte{0x97}
	signed := signedEvidencePolicyGapTestCutoff(t, activation)
	setEvidencePolicyGapTestPolicy(t, fixture, 7, activation.Domain.PolicyHash)
	setEvidencePolicyGapTestPolicy(t, fixture, 9, activation.Domain.PolicyHash)
	cfg := &ResolvedConfig{Config: &HarnessConfig{}, ConfigHash: common.Hash{0x62}.Hex(), WalletMaterial: "deterministic-policy-gap-owner"}
	cfg.Config.Deployment.DeploymentID = fixture.manager.deploymentID
	plan := &SetupPlan{PlanHash: fixture.planHash, ConfigHash: cfg.ConfigHash, DeploymentID: fixture.manager.deploymentID}
	runtime := &evidenceRelayRuntime{ctx: t.Context(), executor: &Executor{cfg: cfg, plan: plan, stateDir: fixture.stateDir, journal: fixture.manager.journal, keeper: fixture.manager},
		chain: fixture.chain, sources: []evidenceRelaySource{{validatorId: 1, stateDir: filepath.Join(fixture.stateDir, "original-source"), activations: []protocol.ValidatorEvidenceActivation{fixture.expected.Activation}, nextEpoch: 7}},
		changed: make(chan struct{}), done: make(chan struct{}), through: map[uint64]uint64{}, completed: map[uint64]bool{1: false}}
	if install {
		if err := runtime.installPolicyGapCutoff(t.Context(), 1, []evidenceRelayPolicyGapActivation{signed}); err != nil {
			t.Fatal(err)
		}
	}
	runtime.horizon = &evidenceRelayHorizon{work: evidenceRelayWork{settlementCadence: 100, nativeCadence: 100}, maximum: 100, anchorBlock: 1000, anchorEpoch: 7, anchorNativeEpoch: 1,
		sourceKVs: map[evidenceRelayHorizonSource]protocol.ValidatorEvidenceActivation{{hotkey: fixture.expected.Activation.Hotkey, noId: fixture.expected.Activation.NoID}: fixture.expected.Activation}, headerKVs: map[[32]byte]protocol.ValidatorEvidenceHeader{}}
	return runtime, fixture, signed
}

func evidencePolicyGapTestRequest(fixture *evidenceRelayRpcFixture) validatorcomponent.ValidatorEvidenceTransactionV2Expected {
	request := fixture.expected
	request.Relayer = common.Address{}
	request.MaxGas, request.MaxFeePerGas = 0, 0
	request.Window.FinalizedBlock = request.Window.EndBlock
	return request
}

func evidencePolicyGapTestPath(t *testing.T, runtime *evidenceRelayRuntime, request validatorcomponent.ValidatorEvidenceTransactionV2Expected) string {
	t.Helper()
	slot, err := request.Evidence.Header.SlotKey()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(runtime.executor.stateDir, "evidence-relay", "policy-gaps-v2", strings.TrimPrefix(runtime.executor.plan.PlanHash, "0x"), fmt.Sprintf("%x.json", slot))
}

func TestEvidencePolicyGapDurableRefusalAdvancesDiscoveryWithoutCompletion(t *testing.T) {
	runtime, fixture, _ := newEvidencePolicyGapTest(t, true)
	request := evidencePolicyGapTestRequest(fixture)
	before, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &validatorcomponent.ValidatorEvidencePublicationV2Manifest{Epoch: 7}
	runtime.startupCache = &evidenceRelayStartupSession{}
	if err := runtime.advanceClosedPublication(&runtime.sources[0], manifest, []validatorcomponent.ValidatorEvidenceTransactionV2Expected{request}, 1200); err != nil {
		t.Fatal(err)
	}
	if runtime.sources[0].nextEpoch != 8 || runtime.completed[1] || runtime.through[1] != 0 || len(runtime.policyGaps) != 1 || runtime.startupCache != nil || !runtime.startupProgress {
		t.Fatal("historical gap entered completed progress or failed to advance discovery")
	}
	if fixture.requestCount("eth_sendRawTransaction") != 0 || fixture.requestCount("pending-nonce") != 0 || len(fixture.manager.journal.Entries()) != 0 {
		t.Fatal("historical gap acquired transaction custody or a budget debit")
	}
	after, _ := json.Marshal(request)
	if !bytes.Equal(before, after) {
		t.Fatal("gap treatment rewrote old signed header or request")
	}
	raw, err := os.ReadFile(evidencePolicyGapTestPath(t, runtime, request))
	if err != nil {
		t.Fatal(err)
	}
	var envelope evidenceRelayPolicyGapEnvelope
	if err := decodeStrictJSONBytes(raw, &envelope); err != nil || envelope.Record.CompletedEvidence || envelope.Record.FinalAcceptance || envelope.Record.Mismatch.FinalizedBlock != 1200 || envelope.Record.Mismatch.FinalizedHash != ([32]byte{0xa1}) || envelope.Record.Mismatch.Policy.PolicyHash != ([32]byte{0x99}) || !reflect.DeepEqual(envelope.Record.Request, request) {
		t.Fatal("durable gap lost exact signed source or finalized mismatch", err)
	}
	// A later completed publication cannot turn the omitted historical epoch
	// into acceptance. Range waits permit only the post-rollover interval.
	runtime.through[1], runtime.completed[1] = 12, true
	if err := runtime.WaitThrough(t.Context(), 12); err == nil || !strings.Contains(err.Error(), "policy gap") {
		t.Fatal("later progress concealed the historical gap", err)
	}
	if err := runtime.WaitRange(t.Context(), 7, 12); err == nil {
		t.Fatal("accepted range counted an intersecting gap")
	}
	if err := runtime.WaitRange(t.Context(), 9, 12); err != nil {
		t.Fatal("old excluded gap blocked complete post-rollover evidence", err)
	}
	if err := runtime.WaitRange(t.Context(), 8, 12); err == nil {
		t.Fatal("acceptance preceded its authenticated cutoff")
	}
}

func TestEvidencePolicyGapRestartReauthenticatesExactImmutableObservation(t *testing.T) {
	runtime, fixture, _ := newEvidencePolicyGapTest(t, true)
	request := evidencePolicyGapTestRequest(fixture)
	manifestHash := common.Hash{0x51}.Hex()
	if gap, err := runtime.retainHistoricalPolicyGap(t.Context(), &runtime.sources[0], manifestHash, request); err != nil || !gap {
		t.Fatal(gap, err)
	}
	path := evidencePolicyGapTestPath(t, runtime, request)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.reopen(t)
	runtime.executor.journal = fixture.manager.journal
	runtime.policyGaps = nil
	fixture.stateLock.Lock()
	fixture.finalizedBlock, fixture.finalizedHash, fixture.historicalPolicyGapReads = 1210, common.Hash{0xa2}, true
	fixture.stateLock.Unlock()
	if gap, err := runtime.retainHistoricalPolicyGap(t.Context(), &runtime.sources[0], manifestHash, request); err != nil || !gap {
		t.Fatal("restart lost the originally finalized mismatch", gap, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || len(runtime.policyGaps) != 1 || runtime.completed[1] {
		t.Fatal("later head rewrote immutable gap or created completion", err)
	}
}

func TestEvidencePolicyGapFailsClosedWithoutCutoffOrAcrossItsBoundary(t *testing.T) {
	for _, fault := range []string{"no-cutoff", "at-cutoff", "after-cutoff", "audit-observation", "late-boundary", "missing-consent", "foreign-source", "already-owned", "transport", "cancelled", "persistence"} {
		t.Run(fault, func(t *testing.T) {
			runtime, fixture, _ := newEvidencePolicyGapTest(t, fault != "no-cutoff")
			request := evidencePolicyGapTestRequest(fixture)
			ctx := t.Context()
			switch fault {
			case "at-cutoff":
				request.Evidence.Header.Epoch = 9
			case "after-cutoff":
				request.Evidence.Header.Epoch = 10
			case "audit-observation":
				request.Evidence.Header.Kind = protocol.ValidatorEvidenceDepositAudit
				request.Evidence.Header.Subject.ObservationEpoch = 9
			case "late-boundary":
				request.Window.EndBlock = 1201
			case "missing-consent":
				request.Evidence.VPKSignature = nil
			case "foreign-source":
				request.Activation.NoID++
			case "already-owned":
				fixture.retain(t, fixture.transaction, fixture.expected.Relayer.Hex(), "4", StageFailed)
			case "transport":
				fixture.stateLock.Lock()
				fixture.requestError = func(request evidenceRelayRpcRequest) error {
					if request.Method == "eth_call" {
						return errors.New("deterministic policy transport failure")
					}
					return nil
				}
				fixture.stateLock.Unlock()
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			case "persistence":
				if err := os.WriteFile(filepath.Join(fixture.stateDir, "evidence-relay"), []byte("foreign non-directory"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			gap, err := runtime.retainHistoricalPolicyGap(ctx, &runtime.sources[0], common.Hash{0x51}.Hex(), request)
			ineligible := fault == "no-cutoff" || fault == "at-cutoff" || fault == "after-cutoff" || fault == "audit-observation"
			if gap || !ineligible && err == nil || len(runtime.policyGaps) != 0 || runtime.sources[0].nextEpoch != 7 || runtime.completed[1] || fixture.requestCount("eth_sendRawTransaction") != 0 {
				t.Fatal("unverified or in-window subject became a historical gap", gap, err)
			}
		})
	}
}

func TestEvidencePolicyGapRejectsTamperingAndChangedCanonicalAuthority(t *testing.T) {
	for _, fault := range []string{"mac", "source", "manifest", "policy", "canonical-block", "canonical-policy", "wallet"} {
		t.Run(fault, func(t *testing.T) {
			runtime, fixture, _ := newEvidencePolicyGapTest(t, true)
			request := evidencePolicyGapTestRequest(fixture)
			manifestHash := common.Hash{0x51}.Hex()
			if gap, err := runtime.retainHistoricalPolicyGap(t.Context(), &runtime.sources[0], manifestHash, request); err != nil || !gap {
				t.Fatal(gap, err)
			}
			path := evidencePolicyGapTestPath(t, runtime, request)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var envelope evidenceRelayPolicyGapEnvelope
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "mac":
				envelope.MAC = strings.Repeat("00", 32)
				raw, err = json.Marshal(envelope)
			case "source":
				envelope.Record.Request.Evidence.Header.PayloadBytes++
				raw, err = encodeEvidenceRelayPolicyGap(envelope.Record, derive32(runtime.executor.cfg, "evidence-policy-gap/v2"))
			case "manifest":
				manifestHash = common.Hash{0x52}.Hex()
			case "policy":
				envelope.Record.Mismatch.Policy.EpochBlocks++
				raw, err = encodeEvidenceRelayPolicyGap(envelope.Record, derive32(runtime.executor.cfg, "evidence-policy-gap/v2"))
			case "canonical-block":
				envelope.Record.Mismatch.FinalizedHash = [32]byte{0xa9}
				raw, err = encodeEvidenceRelayPolicyGap(envelope.Record, derive32(runtime.executor.cfg, "evidence-policy-gap/v2"))
			case "canonical-policy":
				setEvidencePolicyGapTestPolicy(t, fixture, 7, [32]byte{0x98})
			case "wallet":
				runtime.executor.cfg.WalletMaterial += "foreign"
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			runtime.policyGaps = nil
			if gap, err := runtime.retainHistoricalPolicyGap(t.Context(), &runtime.sources[0], manifestHash, request); gap || err == nil || len(runtime.policyGaps) != 0 {
				t.Fatal("changed durable gap became progress", gap, err)
			}
		})
	}
}

func TestEvidencePolicyGapCutoffRequiresCompleteUnchangedSignedCensus(t *testing.T) {
	for _, fault := range []string{"missing", "consent", "operator", "same-policy", "snapshot", "started"} {
		t.Run(fault, func(t *testing.T) {
			runtime, _, signed := newEvidencePolicyGapTest(t, false)
			runtime.horizon = nil
			supplied := []evidenceRelayPolicyGapActivation{signed}
			switch fault {
			case "missing":
				supplied = nil
			case "consent":
				supplied[0].HotkeySignature[0] ^= 1
			case "operator":
				supplied[0].Activation.NoID++
			case "same-policy":
				supplied[0].Activation.Domain.PolicyHash = runtime.sources[0].activations[0].Domain.PolicyHash
			case "snapshot":
				activation := supplied[0].Activation
				activation.EVMHash = [32]byte{0x98}
				supplied[0] = signedEvidencePolicyGapTestCutoff(t, activation)
			case "started":
				runtime.workerStarted = true
			}
			if err := runtime.installPolicyGapCutoff(t.Context(), 1, supplied); err == nil || len(runtime.policyGapCutoffs) != 0 {
				t.Fatal("unauthenticated cutoff authorized a gap", err)
			}
		})
	}
}
