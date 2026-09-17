//go:build linux || darwin

// Historical controls use the real native metadata/registration/stake reader,
// an actual hash-pinned geth client and independently signed payout bytes.
package validator

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Add the actual SubnetEpochIndex metadata/storage entry to the existing
// selective-metagraph fixture. No eligibility or replay verdict is replaced.
func newReleaseDecisionV2NativeTestFixture(t *testing.T, decision *releaseDecisionV2TestFixture) *releaseNativeValidatorTestFixture {
	t.Helper()
	fixture := newReleaseNativeValidatorUIDTestFixture(t, 2, chainBatchHotkey(2))
	fixture.blockNumber = decision.query.boundary.EVMBlock
	original := fixture.chain.API.Client
	var encoded json.RawMessage
	if err := original.CallContext(fixture.ctx, &encoded, "state_getMetadata", fixture.block.Hex()); err != nil {
		t.Fatal(err)
	}
	var metadataHex string
	if err := json.Unmarshal(encoded, &metadataHex); err != nil {
		t.Fatal(err)
	}
	metadata, _, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	metadata.AsMetadataV14.Pallets[0].Storage.Items = append(metadata.AsMetadataV14.Pallets[0].Storage.Items, types.StorageEntryMetadataV14{Name: "SubnetEpochIndex", Modifier: types.StorageFunctionModifierV0{IsDefault: true}, Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Hashers: []types.StorageHasherV10{{IsIdentity: true}}}}})
	metadataHex, err = codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, fixture.expected.MetadataHash, err = crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	key, err := types.CreateStorageKey(metadata, "SubtensorModule", "SubnetEpochIndex", binary.LittleEndian.AppendUint16(nil, decision.query.domain.Netuid))
	if err != nil {
		t.Fatal(err)
	}
	fixture.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		fixture.ctx = ctx
		if method == "state_getMetadata" {
			if len(args) != 1 || args[0] != fixture.block.Hex() {
				return errors.New("decision native metadata hash differs")
			}
			return setValidatorRuntimeIdentityTestResult(result, metadataHex)
		}
		if method == "state_getStorage" && len(args) == 2 && args[0] == key.Hex() {
			if args[1] != fixture.block.Hex() {
				return errors.New("decision native scheduler hash differs")
			}
			return setValidatorRuntimeIdentityTestResult(result, hexutil.Encode(binary.LittleEndian.AppendUint64(nil, 1)))
		}
		return original.CallContext(ctx, result, method, args...)
	}}
	return fixture
}

// An exact native query is independent of the coordinator's EVM hash.
func releaseDecisionV2TestSchedule(fixture *releaseNativeValidatorTestFixture) crv4.ValidatorScheduleQuery {
	return crv4.ValidatorScheduleQuery{GenesisHash: fixture.genesis, BlockHash: fixture.block, BlockNumber: fixture.blockNumber, Netuid: 521, Hotkey: fixture.hotkey, MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs}
}

// Native stake and schedule retain their own hash while matching the EVM UID.
func TestReleaseEvidenceV2DecisionJoinsActualNativeStakeAndPinnedEVM(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	native := newReleaseDecisionV2NativeTestFixture(t, fixture)
	metadata, runtime := native.chain.Meta, native.chain.Runtime
	observed, schedule, err := readReleaseDecisionV2Context(t.Context(), fixture.chain, native.chain, fixture.query, releaseDecisionV2TestSchedule(native), native.expected)
	if err != nil || observed == nil || schedule.SubnetEpochIndex != 1 || schedule.Stake.Identity.UID != 2 || schedule.Stake.TotalStakeRao != native.total || len(observed.hotkeyUIDs) != 3 {
		t.Fatalf("real native/EVM decision join differs: %v", err)
	}
	if native.chain.Meta != metadata || native.chain.Runtime != runtime || schedule.Stake.Identity.BlockHash == types.Hash(common.HexToHash(observed.boundary.EVMBlockHash)) {
		t.Fatal("decision join rewrote signing metadata or substituted the EVM hash")
	}
}

// The real non-self permit gate cannot leak a later EVM-only observation.
func TestReleaseEvidenceV2DecisionNativeRefusalPrecedesEVMRead(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	native := newReleaseDecisionV2NativeTestFixture(t, fixture)
	native.permit = false
	observed, schedule, err := readReleaseDecisionV2Context(t.Context(), fixture.chain, native.chain, fixture.query, releaseDecisionV2TestSchedule(native), native.expected)
	if err == nil || observed != nil || schedule != (crv4.ValidatorScheduleObservation{}) || fixture.count("currentEpoch") != 0 || len(native.calls) <= 1 {
		t.Fatalf("actual native permit refusal leaked an EVM decision: %v", err)
	}
}

// The canonical cross-head lag allowance is checked before either RPC reader.
func TestReleaseEvidenceV2DecisionRejectsNativeLagBeforeRPC(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	native := newReleaseDecisionV2NativeTestFixture(t, fixture)
	before := len(native.calls)
	query := releaseDecisionV2TestSchedule(native)
	query.BlockNumber += uint64(fixture.query.policy.Safety.MaximumFinalizedHeadLagBlocks) + 1
	observed, schedule, err := readReleaseDecisionV2Context(t.Context(), fixture.chain, native.chain, fixture.query, query, native.expected)
	if err == nil || observed != nil || schedule != (crv4.ValidatorScheduleObservation{}) || len(native.calls) != before || fixture.finalizedReads != 0 {
		t.Fatalf("out-of-window native decision reached an actual reader: %v", err)
	}
}

// This helper prepares only the caller-side already-replayed provider census;
// the method under test independently re-reads every native and EVM fact.
func releaseDecisionV2HistoricalTestReference(t *testing.T, fixture *releaseDecisionV2TestFixture, native *releaseNativeValidatorTestFixture) (*releaseEvidenceV2StartupHistory, *SteeringIntent, *ReleaseMeasurementArtifact) {
	t.Helper()
	// Select the complete caller deployment and the real tiny native artifact
	// before obtaining observations or constructing candidate reference fields.
	cfg := validReleaseConfig(t)
	cfg.Policy, cfg.Netuid = fixture.query.policy, fixture.query.domain.Netuid
	cfg.ChainID, cfg.GenesisHash = fixture.query.domain.ChainID, releaseHex32(fixture.query.domain.GenesisHash)
	cfg.Coordinator, cfg.SettlementVault = strings.ToLower(common.Address(fixture.query.domain.Coordinator).Hex()), strings.ToLower(common.Address(fixture.query.domain.SettlementVault).Hex())
	cfg.PolicyHash = releaseHex32(fixture.query.domain.PolicyHash)
	cfg.RuntimeSpec, cfg.TransactionVersion, cfg.StateVersion = native.expected.Version.SpecVersion, native.expected.Version.TransactionVersion, native.expected.Version.StateVersion
	cfg.RuntimeCodeHash, cfg.RuntimeMetadataHash = native.expected.CodeHash, native.expected.MetadataHash
	fixture.query.domain.DeploymentIDHash = sha256.Sum256([]byte(cfg.DeploymentID))
	observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query)
	if err != nil {
		t.Fatal(err)
	}
	history := &releaseEvidenceV2StartupHistory{cfg: cfg, initial: map[uint64]ReleaseEvidenceV2ActivationContext{}, inputByEpoch: map[uint64]map[uint64]*releaseMeasurementInputJournal{1: {}}}
	artifact := &ReleaseMeasurementArtifact{
		DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.GenesisHash, Coordinator: cfg.Coordinator, SettlementVault: cfg.SettlementVault, ValidatorID: cfg.ValidatorID, Netuid: cfg.Netuid, PolicyHash: cfg.PolicyHash,
		SubnetEpoch: 1, NativeSnapshotBlock: native.blockNumber, NativeSnapshotHash: native.block.Hex(), EVMSnapshotBlock: fixture.query.boundary.EVMBlock, EVMSnapshotHash: fixture.query.boundary.EVMBlockHash, SettlementEpoch: fixture.query.boundary.SettlementEpoch, SelfUID: native.uid, Bindings: slices.Clone(observed.bindings), Pools: slices.Clone(observed.pools),
	}
	for _, operator := range fixture.query.operators {
		history.initial[operator.noID] = ReleaseEvidenceV2ActivationContext{InitialCut: AttemptCutV2Context{Activation: AttemptCutV2Activation{Domain: fixture.query.domain, Hotkey: native.hotkey}}}
		history.participants = append(history.participants, AttemptSettlementRuntimeV2Participant{NoID: operator.noID})
		input := ReleaseMeasurementInput{NoID: operator.noID}
		for _, id := range operator.providerIDs {
			input.Stats.Providers = append(input.Stats.Providers, ReleaseProviderMeasurement{ClientID: id.String()})
		}
		history.inputByEpoch[1][operator.noID] = &releaseMeasurementInputJournal{MeasurementInput: input}
	}
	for _, operator := range observed.operators {
		if operator.commitment.CommitBlock == 0 {
			audit, err := history.historicalDepositAudit(t.Context(), observed, operator, DepositAuditUnavailablePending)
			if err != nil {
				t.Fatal(err)
			}
			artifact.DepositAudits = append(artifact.DepositAudits, audit)
		}
	}
	intent := &SteeringIntent{SubnetEpoch: 1, SelfUID: native.uid}
	return history, intent, artifact
}

// A signed candidate's local key cannot fill missing immutable API history.
func TestReleaseEvidenceV2DecisionHistoricalKeysAreNotBorrowedFromSignedClaims(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	native := newReleaseDecisionV2NativeTestFixture(t, fixture)
	history, intent, artifact := releaseDecisionV2HistoricalTestReference(t, fixture, native)
	for index := range artifact.Bindings {
		artifact.Bindings[index].LocalClientKey = artifact.Bindings[index].ClientKey
		// Name a canonical but absent original object. A missing digest would
		// correctly fail wire admission before reaching the custody boundary.
		artifact.Bindings[index].ClientKeyObservationHash = ReleaseMeasurementContentHash([]byte("absent original client-key capture"))
	}
	if err := history.authenticateIntentChainReference(t.Context(), fixture.chain, native.chain, native.expected, intent, artifact); !errors.Is(err, errReleaseHistoricalClientKeyV2) {
		t.Fatalf("historical chain key was promoted into missing API-key authority: %v", err)
	}
	artifact.Bindings[0].ClientKeyObservationHash = ""
	if err := history.authenticateIntentChainReference(t.Context(), fixture.chain, native.chain, native.expected, intent, artifact); err == nil || !strings.Contains(err.Error(), "content hash is not canonical") {
		t.Fatalf("local key claim bypassed missing observation-digest admission: %v", err)
	}
}

// Real pinned root absence is reproducible; altered deposit claims are not.
func TestReleaseEvidenceV2DecisionHistoricalRootAbsenceIsActuallyReconstructable(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	for key, view := range fixture.views {
		if view.method == "bindingAt" {
			view.data = slices.Clone(view.data)
			clear(view.data[:32])
			fixture.views[key] = view
		}
	}
	for _, operator := range fixture.query.operators {
		fixture.set(t, "rootCommitments", fixture.chain.coordinator.PackRootCommitments(big.NewInt(0), new(big.Int).SetUint64(operator.noID)), [32]byte{}, [32]byte{}, common.Address{}, uint64(0))
	}
	native := newReleaseDecisionV2NativeTestFixture(t, fixture)
	history, intent, artifact := releaseDecisionV2HistoricalTestReference(t, fixture, native)
	if err := history.authenticateIntentChainReference(t.Context(), fixture.chain, native.chain, native.expected, intent, artifact); err != nil {
		t.Fatalf("actual pinned missing-root decision was refused: %v", err)
	}
	artifact.DepositAudits[1].ObservedDepositRao = "999"
	if err := history.authenticateIntentChainReference(t.Context(), fixture.chain, native.chain, native.expected, intent, artifact); err == nil || !strings.Contains(err.Error(), "deposit audit differs") {
		t.Fatalf("historical signed deposit claim replaced actual observed amount: %v", err)
	}
}

// A candidate cannot replace any independently configured deployment field,
// even when the real native and Evm observations remain otherwise unchanged.
func TestReleaseEvidenceV2DecisionHistoricalDeploymentScopeIsIndependent(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2EligibilityTestFixture(t, nil)
	history, native, decision := fixture.history, fixture.native, fixture.decision
	if err := history.authenticateIntentChainReference(t.Context(), decision.chain, native.chain, native.expected, fixture.intent, fixture.artifact); err != nil {
		t.Fatalf("complete independently scoped reference failed real reads: %v", err)
	}
	for _, change := range []struct {
		name   string
		mutate func(*ReleaseMeasurementArtifact)
	}{
		{name: "deployment", mutate: func(value *ReleaseMeasurementArtifact) { value.DeploymentID += "-other" }},
		{name: "chain", mutate: func(value *ReleaseMeasurementArtifact) { value.ChainID++ }},
		{name: "genesis", mutate: func(value *ReleaseMeasurementArtifact) { value.GenesisHash = releaseHex32([32]byte{0xe1}) }},
		{name: "coordinator", mutate: func(value *ReleaseMeasurementArtifact) {
			value.Coordinator = strings.ToLower((common.Address{0xe2}).Hex())
		}},
		{name: "vault", mutate: func(value *ReleaseMeasurementArtifact) {
			value.SettlementVault = strings.ToLower((common.Address{0xe3}).Hex())
		}},
		{name: "validator", mutate: func(value *ReleaseMeasurementArtifact) { value.ValidatorID++ }},
		{name: "netuid", mutate: func(value *ReleaseMeasurementArtifact) { value.Netuid++ }},
		{name: "policy", mutate: func(value *ReleaseMeasurementArtifact) { value.PolicyHash = releaseHex32([32]byte{0xe4}) }},
	} {
		candidate := cloneReleaseMeasurementArtifact(t, fixture.artifact)
		change.mutate(candidate)
		before := decision.count("currentEpoch")
		err := history.authenticateIntentChainReference(t.Context(), decision.chain, native.chain, native.expected, fixture.intent, candidate)
		if err == nil || !strings.Contains(err.Error(), "client-key capture differs from the admitted validator deployment") || decision.count("currentEpoch") <= before {
			t.Fatalf("%s candidate replaced independent historical scope or skipped real reads: %v", change.name, err)
		}
	}
}

// Today's content availability is not a receipt for a historical failure.
func TestReleaseEvidenceV2DecisionHistoricalNegativeHTTPClaimsRemainRefused(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query)
	if err != nil {
		t.Fatal(err)
	}
	history := &releaseEvidenceV2StartupHistory{cfg: validReleaseConfig(t)}
	for _, status := range []string{DepositAuditUnavailablePending, DepositAuditUnavailable, DepositAuditEquivocation, DepositAuditInvalid} {
		if audit, err := history.historicalDepositAudit(t.Context(), observed, observed.operators[0], status); !errors.Is(err, errReleaseHistoricalArtifactV2) || audit != (DepositAudit{}) {
			t.Fatalf("%s fabricated past HTTP/history authority: %v", status, err)
		}
	}
}

// Bootstrap has no prior-window artifact. The actual zero on-chain deposit,
// not missing HTTP bytes or a candidate audit flag, determines this result.
func TestReleaseEvidenceV2DecisionHistoricalBootstrapUsesPinnedZeroDeposit(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	coordinator := fixture.chain.coordinator
	fixture.finalized = 15
	fixture.blocks = map[uint64][32]byte{10: {0x10}, 15: {0x30}, 14: {0x31}}
	fixture.query.boundary = AttemptBoundary{SettlementEpoch: 0, EVMBlock: 15, EVMBlockHash: releaseHex32(fixture.blocks[15])}
	fixture.set(t, "currentEpoch", coordinator.PackCurrentEpoch(), big.NewInt(0))
	fixture.views[fmt.Sprintf("%x", coordinator.PackPolicyAt(big.NewInt(0)))] = fixture.views[fmt.Sprintf("%x", coordinator.PackPolicyAt(big.NewInt(1)))]
	for _, operator := range fixture.query.operators {
		id := new(big.Int).SetUint64(operator.noID)
		fixture.set(t, "epochDeposits", coordinator.PackEpochDeposits(big.NewInt(0), id), big.NewInt(0))
		fixture.set(t, "epochConvictionAdded", coordinator.PackEpochConvictionAdded(big.NewInt(0), id), big.NewInt(0))
		for _, provider := range operator.providerIDs {
			fixture.views[fmt.Sprintf("%x", coordinator.PackBindingAt([16]byte(provider), big.NewInt(0)))] = fixture.views[fmt.Sprintf("%x", coordinator.PackBindingAt([16]byte(provider), big.NewInt(1)))]
		}
	}
	observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query)
	if err != nil {
		t.Fatal(err)
	}
	history := &releaseEvidenceV2StartupHistory{cfg: validReleaseConfig(t)}
	audit, err := history.historicalDepositAudit(t.Context(), observed, observed.operators[0], DepositAuditBootstrap)
	if err != nil || audit.Status != DepositAuditBootstrap || !audit.Compliant || audit.ObservedDepositRao != "0" || audit.Disposition != "zero_pool_weight_bootstrap" || fixture.count("rootCommitments") != 0 || observed.sourceStart != 0 || observed.sourceEnd != 0 {
		t.Fatalf("bootstrap manufactured prior-window source authority: %+v %v", audit, err)
	}
}

// Both the source digest and deposit amount are independently served by the
// real coordinator fixture. A genuine signed artifact reconstructs the same
// exact formula; the candidate contributes neither usage nor conviction.
func TestReleaseEvidenceV2DecisionHistoricalCommittedAuditReconstructsExactDeposit(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	query := fixture.query
	cfg := validReleaseConfig(t)
	cfg.Policy, cfg.Netuid, cfg.GenesisHash = query.policy, query.domain.Netuid, releaseHex32(query.domain.GenesisHash)
	start, end := uint64(10), fixture.finalized-5
	artifact, err := payoutartifact.Build(payoutartifact.BuildInput{DeploymentID: cfg.DeploymentID, GenesisHash: cfg.GenesisHash, PolicyHash: cfg.PolicyHash, ChainID: cfg.ChainID, Netuid: cfg.Netuid, Coordinator: common.HexToAddress(cfg.Coordinator), SettlementVault: common.HexToAddress(cfg.SettlementVault), Epoch: 0, NoID: 1, Start: payoutartifact.Boundary{Number: start, Hash: releaseHex32(fixture.blocks[start])}, End: payoutartifact.Boundary{Number: end, Hash: releaseHex32(fixture.blocks[end])}, OperatorSnapshotHash: "sha256:" + strings.Repeat("10", 32), FleetSnapshotHash: "sha256:" + strings.Repeat("20", 32), Providers: []payoutartifact.ProviderInput{{ClientID: [16]byte{1}, Coldkey: [32]byte{1}, UsageBytes: 3 * 1024 * 1024 * 1024, Assignments: 8, Confirmations: 8, Eligible: true}}, ReliabilityAMin: cfg.Policy.Verify.ReliabilityAMin, CreatedAt: time.Unix(1_700_000_000, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(artifact, key); err != nil {
		t.Fatal(err)
	}
	encoded, err := payoutartifact.Bytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := parseReleaseContentHash(artifact.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	required, _, err := protocol.RequiredDepositRao(artifact.TotalUsageBytes, fixture.bigConviction, cfg.Policy.Deposit)
	if err != nil || required.Sign() <= 0 {
		t.Fatalf("genuine artifact requires no positive deposit: %v", err)
	}
	coordinator := fixture.chain.coordinator
	fixture.set(t, "epochDeposits", coordinator.PackEpochDeposits(big.NewInt(1), big.NewInt(1)), required)
	fixture.set(t, "cumulativeConviction", coordinator.PackCumulativeConviction(big.NewInt(1)), new(big.Int).Add(new(big.Int).Add(new(big.Int).Set(fixture.bigConviction), required), big.NewInt(7)))
	fixture.set(t, "rootCommitments", coordinator.PackRootCommitments(big.NewInt(0), big.NewInt(1)), artifact.PayoutRoot, hash, fixture.versions[0].RootSigner, end+1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/sn/artifact" || request.URL.Query().Get("hash") != artifact.ContentHash {
			http.Error(writer, "committed artifact route differs", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(encoded)
	}))
	t.Cleanup(server.Close)
	cfg.Operators[0].APIURL, cfg.Operators[0].ArtifactSigner = server.URL, artifact.Signer.Hex()
	observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	history := &releaseEvidenceV2StartupHistory{cfg: cfg}
	audit, err := history.historicalDepositAudit(t.Context(), observed, observed.operators[0], DepositAuditCompliant)
	if err != nil || !audit.Compliant || audit.Status != DepositAuditCompliant || audit.ObservedDepositRao != required.String() || audit.ConvictionBeforeRao != fixture.bigConviction.String() || audit.ArtifactHash != artifact.ContentHash || audit.UsageBytes != artifact.TotalUsageBytes {
		t.Fatalf("actual signed source and pinned deposit did not reconstruct the exact audit: %+v %v", audit, err)
	}
}

// A real server is reached only through the committed content endpoint.
func TestReleaseEvidenceV2DecisionCommittedArtifactUsesExactHashAndBound(t *testing.T) {
	t.Parallel()
	artifact, encoded := validatorTestArtifact(t)
	hash, err := parseReleaseContentHash(artifact.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/sn/artifact" || request.URL.Query().Get("hash") != artifact.ContentHash {
			http.Error(writer, "mutable history is not a committed content source", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(encoded)
	}))
	t.Cleanup(server.Close)
	reader, err := NewHTTPArtifactReader(server.URL, artifact.DeploymentID, artifact.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := reader.readCommittedReleaseDecisionV2Artifact(t.Context(), hash, uint64(len(encoded)))
	if err != nil || actual == nil || actual.ContentHash != artifact.ContentHash || actual.TotalUsageBytes != artifact.TotalUsageBytes {
		t.Fatalf("exact genuine committed content was not preserved: %v", err)
	}
	if actual, err := reader.readCommittedReleaseDecisionV2Artifact(t.Context(), hash, uint64(len(encoded)-1)); err == nil || actual != nil {
		t.Fatalf("committed artifact exceeded its exact original byte bound: %v", err)
	}
}

// The underlying actual response closes first. Only then is caller cancellation
// delivered, exposing a lost late-cause bug without faking a proof verdict.
type releaseDecisionV2TestCloseCancel struct {
	io.ReadCloser
	cancel context.CancelFunc
	closed *bool
}

// Close the genuine body before delivering the deterministic late cause.
func (self *releaseDecisionV2TestCloseCancel) Close() error {
	err := self.ReadCloser.Close()
	*self.closed = true
	self.cancel()
	return err
}

// Successful decoding cannot hide a failure delivered by actual body closure.
func TestReleaseEvidenceV2DecisionCommittedArtifactJoinsActualCloseCancellation(t *testing.T) {
	t.Parallel()
	artifact, encoded := validatorTestArtifact(t)
	hash, err := parseReleaseContentHash(artifact.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(encoded)
	}))
	t.Cleanup(server.Close)
	reader, err := NewHTTPArtifactReader(server.URL, artifact.DeploymentID, artifact.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closed := false
	reader.client.Transport = releaseStartupV2TestRoundTripper(func(request *http.Request) (*http.Response, error) {
		response, err := http.DefaultTransport.RoundTrip(request)
		if err == nil {
			response.Body = &releaseDecisionV2TestCloseCancel{ReadCloser: response.Body, cancel: cancel, closed: &closed}
		}
		return response, err
	})
	if result, err := reader.readCommittedReleaseDecisionV2Artifact(ctx, hash, uint64(len(encoded))); !errors.Is(err, context.Canceled) || result != nil || !closed {
		t.Fatalf("actual committed-content Close cancellation was hidden: %v", err)
	}
}
