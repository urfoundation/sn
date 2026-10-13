//go:build linux || darwin

// Genuine signed M8 transcripts are joined to real ABI/RPC responses. Faults
// change the raw coordinator or route facts, never a verification verdict.
package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

// Each test owns its transport, route generation and independently encoded
// current/source operator state. The base fixture owns all shared counters.
type recycleOperatorFixture struct {
	*releaseDecisionV2TestFixture
	measurement       *recycleMeasurementFixture
	routeReads        int
	requests          int
	transientFirst    bool
	retargetChain     bool
	retargetGenesis   bool
	retargetDecision  bool
	beforeRouteReturn func(int)
	startup           *productionStartupEvmTestFixture
}

// Startup adds independent historical journal ABI responses to the original
// exact decision reader. Existing fixtures keep the original dispatch intact.
func (self *recycleOperatorFixture) Call(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	if self.startup != nil {
		if value, found, err := self.startup.view(ctx, call, selector); found || err != nil {
			return value, err
		}
	}
	return self.releaseDecisionV2TestFixture.Call(ctx, call, selector)
}

// The historical code image is served only at the independently pinned
// activation publication block and actual journal address.
func (self *recycleOperatorFixture) GetCode(ctx context.Context, target common.Address, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	if self.startup == nil {
		return nil, errors.New("synthetic operator has no journal code image")
	}
	return self.startup.codeAt(ctx, target, selector)
}

// The first route is selected before approval; late drift happens only after
// the entire real coordinator census and proof replay have returned.
func (self *recycleOperatorFixture) ChainId(ctx context.Context) (*hexutil.Big, error) {
	count := func() int {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.routeReads++
		return self.routeReads
	}()
	value := self.query.domain.ChainID
	if self.retargetChain && count >= 2 {
		value = 945
	}
	if self.beforeRouteReturn != nil {
		self.beforeRouteReturn(count)
	}
	return (*hexutil.Big)(new(big.Int).SetUint64(value)), ctx.Err()
}

// Native genesis is queried through the same actual EVM transport.
func (self *recycleOperatorFixture) GetBlockHash(ctx context.Context, number uint64) (common.Hash, error) {
	if number != 0 {
		return common.Hash{}, errors.New("synthetic route permits only the genesis identity read")
	}
	count := func() int { self.stateLock.Lock(); defer self.stateLock.Unlock(); return self.routeReads }()
	hash := common.Hash(self.query.domain.GenesisHash)
	if self.retargetGenesis && count >= 2 {
		hash[0] ^= 1
	}
	return hash, ctx.Err()
}

// The final exact-height read exposes a same-genesis fork after route checks.
func (self *recycleOperatorFixture) GetBlockByNumber(ctx context.Context, number gethrpc.BlockNumber, full bool) (map[string]any, error) {
	result, err := self.releaseDecisionV2TestFixture.GetBlockByNumber(ctx, number, full)
	if err != nil {
		return nil, err
	}
	count := func() int { self.stateLock.Lock(); defer self.stateLock.Unlock(); return self.routeReads }()
	if self.retargetDecision && count >= 2 && uint64(number) == self.finalized {
		hash := result["hash"].(common.Hash)
		// Keep this a valid fork of the original single-byte fixture identity.
		// Zero would stop at header decoding before the canonical route check.
		hash[len(hash)-1] ^= 1
		result["hash"] = hash
	}
	return result, ctx.Err()
}

// Every identity and unsigned snapshot is declared before signed approval and
// provider sealing. No finished testnet evidence is relabeled as mainnet.
func newRecycleOperatorFixture(t *testing.T) *recycleOperatorFixture {
	return newRecycleOperatorFixtureWithHotkey(t, [32]byte{0x15})
}

// Production tests choose the real signer before constructing the evidence.
func newRecycleOperatorFixtureWithHotkey(t *testing.T, hotkey [32]byte) *recycleOperatorFixture {
	return newRecycleOperatorFixtureWithInputs(t, hotkey, 2, nil, nil)
}

// Optional independent fixture inputs are fixed before provider sealing and
// approval. The callback assembles real journals; it cannot admit a verdict.
func newRecycleOperatorFixtureWithInputs(t *testing.T, hotkey [32]byte, completed int, selectedPolicy *protocol.Policy, setup func(*recycleAdmissionFixture, *releaseMeasurementV2TestFixture)) *recycleOperatorFixture {
	return newRecycleOperatorFixtureWithActivation(t, hotkey, completed, selectedPolicy, setup, nil)
}

// Full startup can provide real activation bytes without rewriting any signed
// provider evidence after its compact stream has been sealed.
func newRecycleOperatorFixtureWithActivation(t *testing.T, hotkey [32]byte, completed int, selectedPolicy *protocol.Policy, setup func(*recycleAdmissionFixture, *releaseMeasurementV2TestFixture), anchor func(*recycleAdmissionFixture, *attemptCutV2SealTestFixture)) *recycleOperatorFixture {
	t.Helper()
	fixture := &recycleOperatorFixture{releaseDecisionV2TestFixture: newReleaseDecisionV2TestFixture(t)}
	server := gethrpc.NewServer()
	if err := errors.Join(server.RegisterName("eth", fixture), server.RegisterName("chain", fixture)); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if fixture.startup != nil && !fixture.startup.allowHttp(writer, request) {
			return
		}
		count := func() int {
			fixture.stateLock.Lock()
			defer fixture.stateLock.Unlock()
			fixture.requests++
			return fixture.requests
		}()
		if fixture.transientFirst && count == 1 {
			http.Error(writer, "synthetic temporary transport outage", http.StatusServiceUnavailable)
			return
		}
		server.ServeHTTP(writer, request)
	}))
	client, err := ethclient.Dial(httpServer.URL)
	if err != nil {
		httpServer.Close()
		server.Stop()
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); httpServer.Close(); server.Stop() })
	fixture.measurement = newRecycleMeasurementFixtureWithActivation(t, completed, func(admission *recycleAdmissionFixture, provider *releaseMeasurementV2TestFixture) {
		admission.cfg.RPC = []string{httpServer.URL}
		artifact := provider.artifact
		for index := range artifact.DepositAudits {
			audit := &artifact.DepositAudits[index]
			if !artifact.Policy.IsZeroPrice() {
				audit.SourceStartHash = releaseHex32([32]byte{0x25})
				audit.SourceEndHash = releaseHex32([32]byte{0x26})
			}
			audit.ArtifactDeadlineBlock = 90 + artifact.Policy.Settlement.RootCommitWindowBlocks
		}
		if setup != nil {
			setup(admission, provider)
		}
		provider.rebuildLegacy(t)
	}, hotkey, selectedPolicy, anchor)
	measurement := fixture.measurement
	artifact := measurement.provider.artifact
	fixture.views = map[string]releaseDecisionV2TestView{}
	fixture.query.domain = measurement.provider.operators[9].seal.expected.Activation.Domain
	fixture.query.policy = artifact.Policy
	fixture.query.boundary = AttemptBoundary{SettlementEpoch: artifact.SettlementEpoch, EVMBlock: artifact.EVMSnapshotBlock, EVMBlockHash: artifact.EVMSnapshotHash}
	fixture.chain = &ChainClient{client: client, rpcUrl: httpServer.URL, chainId: new(big.Int).SetUint64(964), coordinator: stabi.NewSTCoordinator(), contractAddr: common.Address(fixture.query.domain.Coordinator), release: true}
	fixture.finalized = artifact.EVMSnapshotBlock
	decisionHash, _ := parseReleaseHex32("synthetic decision", artifact.EVMSnapshotHash, false)
	fixture.blocks = map[uint64][32]byte{10: {0x25}, 90: {0x26}, fixture.finalized: decisionHash}
	coordinator, epoch := fixture.chain.coordinator, new(big.Int).SetUint64(artifact.SettlementEpoch)
	sourceEpoch := new(big.Int).SetUint64(artifact.SettlementEpoch - artifact.Policy.Deposit.UsageLagEpochs)
	policy := artifact.Policy
	fixture.set(t, "netuid", coordinator.PackNetuid(), artifact.Netuid)
	fixture.set(t, "currentEpoch", coordinator.PackCurrentEpoch(), epoch)
	fixture.set(t, "policyAt", coordinator.PackPolicyAt(epoch), stabi.STCoordinatorPolicySnapshot{PolicyHash: fixture.query.domain.PolicyHash, EffectiveEpoch: artifact.SettlementEpoch, EffectiveBlock: 90, EpochBlocks: policy.Settlement.EpochBlocks, RootCommitWindowBlocks: policy.Settlement.RootCommitWindowBlocks, FinalizeOffsetBlocks: policy.Settlement.FinalizeOffsetBlocks, CloseGraceBlocks: policy.Settlement.CloseGraceBlocks, ClaimTTLEpochs: policy.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: policy.Settlement.ClaimGraceEpochs, MaximumBindingValidityEpochs: policy.Binding.MaximumValidityEpochs, CommitmentMaxAgeBlocks: 10, EpochDepositCapRao: new(big.Int).SetUint64(policy.Deposit.EpochCapRaoPerOperator), CampaignDepositCapRao: new(big.Int).SetUint64(policy.Deposit.TotalTestCampaignCapRao)})
	fixture.set(t, "operatorCount", coordinator.PackOperatorCount(), big.NewInt(2))
	fixture.set(t, "epochStartBlock", coordinator.PackEpochStartBlock(epoch), big.NewInt(90))
	fixture.set(t, "epochStartBlock", coordinator.PackEpochStartBlock(sourceEpoch), big.NewInt(10))
	fixture.set(t, "epochEndBlock", coordinator.PackEpochEndBlock(sourceEpoch), big.NewInt(90))
	fixture.versions, fixture.commitments = nil, nil
	for index, pool := range artifact.Pools {
		id := new(big.Int).SetUint64(pool.NoID)
		audit := artifact.DepositAudits[index]
		poolHotkey, _ := parseReleaseHex32("synthetic pool", pool.PoolHotkey, false)
		version := stabi.STCoordinatorOperatorVersion{Coldkey: [32]byte{byte(0x40 + index)}, PoolHotkey: poolHotkey, DepositHotkey: [32]byte{byte(0x50 + index)}, DepositSigner: common.Address{byte(0x60 + index)}, RootSigner: common.HexToAddress(audit.RootSigner), Active: true}
		if policy.IsZeroPrice() {
			version.RootSigner = common.Address{0x70, byte(index + 1)}
		}
		fixture.versions = append(fixture.versions, version)
		fixture.set(t, "operatorIdAt", coordinator.PackOperatorIdAt(big.NewInt(int64(index))), id)
		fixture.set(t, "operatorAt", coordinator.PackOperatorAt(id, epoch), version)
		fixture.set(t, "operatorAt", coordinator.PackOperatorAt(id, sourceEpoch), version)
		deposit, _ := new(big.Int).SetString(audit.ObservedDepositRao, 10)
		conviction, _ := new(big.Int).SetString(audit.ConvictionBeforeRao, 10)
		fixture.set(t, "epochDeposits", coordinator.PackEpochDeposits(epoch, id), deposit)
		fixture.set(t, "epochConvictionAdded", coordinator.PackEpochConvictionAdded(epoch, id), big.NewInt(7))
		fixture.set(t, "cumulativeConviction", coordinator.PackCumulativeConviction(id), new(big.Int).Add(conviction, new(big.Int).Add(deposit, big.NewInt(7))))
		root, _ := parseReleaseHex32("synthetic root", audit.PayoutRoot, false)
		artifactHash, _ := parseReleaseHex32("synthetic artifact", audit.CommittedArtifactHash, false)
		commitment := stabi.RootCommitmentsOutput{PayoutRoot: root, ArtifactHash: artifactHash, Committer: version.RootSigner, CommitBlock: audit.RootCommitBlock}
		if policy.IsZeroPrice() {
			// An independently registered root signer does not occupy the
			// source slot. The complete absent ABI tuple is zero on chain.
			commitment = stabi.RootCommitmentsOutput{}
		}
		fixture.commitments = append(fixture.commitments, commitment)
		fixture.set(t, "rootCommitments", coordinator.PackRootCommitments(sourceEpoch, id), commitment.PayoutRoot, commitment.ArtifactHash, commitment.Committer, commitment.CommitBlock)
	}
	for _, binding := range artifact.Bindings {
		clientId, _ := connect.ParseId(binding.ClientID)
		fleet, _ := parseReleaseHex32("synthetic fleet", binding.FleetID, false)
		hotkey, _ := parseReleaseHex32("synthetic binding", binding.Hotkey, false)
		clientKey, _ := parseReleaseHex32("synthetic client key", binding.ClientKey, false)
		commitment, _ := parseReleaseHex32("synthetic commitment", binding.CommitmentHash, false)
		record := stabi.STCoordinatorBindingRecord{FleetId: fleet, Hotkey: hotkey, ClientKey: clientKey, CommitmentHash: commitment, Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, Uid: binding.RecordUID}
		fixture.set(t, "bindingAt", coordinator.PackBindingAt([16]byte(clientId), epoch), binding.Active, record)
	}
	selector, netuid := evmSelector("getUidCount(uint16)"), evmUint16Word(artifact.Netuid)
	registrations := measurement.authority.observation.Snapshot.Registrations
	fixture.views[hex.EncodeToString(append(selector[:], netuid[:]...))] = releaseDecisionV2TestView{method: "getUidCount", data: new(big.Int).SetUint64(uint64(len(registrations))).FillBytes(make([]byte, 32))}
	for _, registration := range registrations {
		selector, uid := evmSelector("getHotkey(uint16,uint16)"), evmUint16Word(registration.Uid)
		data := append(append(slices.Clone(selector[:]), netuid[:]...), uid[:]...)
		fixture.views[hex.EncodeToString(data)] = releaseDecisionV2TestView{method: "getHotkey", data: slices.Clone(registration.Hotkey[:])}
	}
	return fixture
}

// Each observation receives new actual replay scratch, preserving retry scope.
func (self *recycleOperatorFixture) observe(t *testing.T) (*OwnerRecycleMeasurementAuthority, error) {
	t.Helper()
	return ObserveOwnerRecycleMeasurementOperators(t.Context(), self.measurement.authority, self.chain, self.measurement.encoded, self.measurement.provider.options(t))
}

// The new capsule contains exact source commitment/amount facts, yet remains
// unsigned and blocked. The prior owner's bytes and v1 output stay unchanged.
func TestOwnerRecycleOperatorsObserveSealReplayAndRetainV1(t *testing.T) {
	fixture := newRecycleOperatorFixture(t)
	legacy, legacyIntent := fixture.measurement.seal(t)
	owner, err := fixture.observe(t)
	if err != nil || owner == nil {
		t.Fatalf("real decision observation: %v", err)
	}
	if len(fixture.measurement.authority.operatorEvidence) != 0 || owner == fixture.measurement.authority {
		t.Fatal("operator observation mutated its prior immutable owner")
	}
	var evidence OwnerRecycleOperatorEvidence
	if err := json.Unmarshal(owner.operatorEvidence, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Schema != ownerRecycleOperatorEvidenceSchema || evidence.Decision != owner.expected || len(evidence.Operators) != 2 || evidence.SourceStart != 10 || evidence.SourceEnd != 90 || evidence.Operators[0].SourceCommitment != fixture.commitments[0] || evidence.Operators[0].DepositRao != fixture.measurement.provider.artifact.DepositAudits[0].ObservedDepositRao {
		t.Fatal("retained coordinator facts differ from the independent RPC fixture")
	}
	encoded, intent, err := SealOwnerRecycleMeasurement(t.Context(), owner, fixture.measurement.encoded, fixture.measurement.provider.options(t))
	if err != nil || intent == nil {
		t.Fatalf("operator capsule seal: %v", err)
	}
	if intent.Schema != ownerRecycleOperatorIntentSchema || intent.Status != OwnerRecycleDecisionBlocked || intent.ActivationReady || intent.NativeOutcomeVerified || intent.OperatorEvidenceHash != ReleaseMeasurementContentHash(owner.operatorEvidence) || !reflect.DeepEqual(intent.Row, legacyIntent.Row) || !strings.Contains(intent.Blockers[1], "API health") {
		t.Fatal("observed capsule acquired launch authority or changed measured economics")
	}
	if err := VerifyOwnerRecycleDecisionIntent(t.Context(), owner, encoded, fixture.measurement.provider.options(t), intent); err != nil {
		t.Fatal(err)
	}
	unchanged, unchangedIntent := fixture.measurement.seal(t)
	if !bytes.Equal(legacy, unchanged) || !reflect.DeepEqual(legacyIntent, unchangedIntent) || bytes.Contains(legacy, []byte("operator_evidence")) {
		t.Fatal("v2 observation rewrote historical v1 capsule bytes")
	}
}

// Actual pinned state, not the self-consistent measurement assertions, decides
// operator/pool membership and positive audit eligibility.
func TestOwnerRecycleOperatorsRejectChangedDecisionFacts(t *testing.T) {
	for _, fault := range []string{"inactive", "pool", "deposit", "conviction", "binding", "root", "source", "registry"} {
		fixture := newRecycleOperatorFixture(t)
		artifact, coordinator := fixture.measurement.provider.artifact, fixture.chain.coordinator
		epoch, id := new(big.Int).SetUint64(artifact.SettlementEpoch), big.NewInt(9)
		switch fault {
		case "inactive", "pool":
			version := fixture.versions[0]
			if fault == "inactive" {
				version.Active = false
			} else {
				version.PoolHotkey = fixture.measurement.authority.observation.Snapshot.Registrations[1].Hotkey
			}
			fixture.set(t, "operatorAt", coordinator.PackOperatorAt(id, epoch), version)
		case "deposit":
			fixture.set(t, "epochDeposits", coordinator.PackEpochDeposits(epoch, id), big.NewInt(1))
		case "conviction":
			fixture.set(t, "cumulativeConviction", coordinator.PackCumulativeConviction(id), new(big.Int).Lsh(big.NewInt(1), 200))
		case "binding":
			binding := artifact.Bindings[0]
			clientId, _ := connect.ParseId(binding.ClientID)
			data := coordinator.PackBindingAt([16]byte(clientId), epoch)
			actual, err := coordinator.UnpackBindingAt(fixture.views[hex.EncodeToString(data)].data)
			if err != nil {
				t.Fatal(err)
			}
			actual.Record.Generation++
			fixture.set(t, "bindingAt", data, actual.Active, actual.Record)
		case "root":
			commitment := fixture.commitments[0]
			commitment.PayoutRoot[0] ^= 1
			source := new(big.Int).SetUint64(artifact.DepositAudits[0].SourceEpoch)
			fixture.set(t, "rootCommitments", coordinator.PackRootCommitments(source, id), commitment.PayoutRoot, commitment.ArtifactHash, commitment.Committer, commitment.CommitBlock)
		case "source":
			fixture.retargetSource = true
		case "registry":
			fixture.set(t, "operatorIdAt", coordinator.PackOperatorIdAt(big.NewInt(1)), id)
		}
		if owner, err := fixture.observe(t); err == nil || owner != nil {
			t.Fatalf("%s raw-state fault published partial authority: %v", fault, err)
		}
		if len(fixture.measurement.authority.operatorEvidence) != 0 {
			t.Fatalf("%s raw-state fault mutated retained approval owner", fault)
		}
	}
}

// Neither a cached dial chain id nor a same-genesis proxy fork may survive the
// final uncached checks after the expensive real proof/census work.
func TestOwnerRecycleOperatorsRejectLateRouteRetarget(t *testing.T) {
	for _, fault := range []string{"chain", "genesis", "decision"} {
		fixture := newRecycleOperatorFixture(t)
		fixture.retargetChain = fault == "chain"
		fixture.retargetGenesis = fault == "genesis"
		fixture.retargetDecision = fault == "decision"
		owner, err := fixture.observe(t)
		if err == nil || owner != nil || RetryableEvidenceTransportError(err) {
			t.Fatalf("%s late route replacement admitted: %v", fault, err)
		}
		if fault == "decision" && !strings.Contains(err.Error(), "operator canonical decision changed before retention") {
			t.Fatal("late decision fork did not reach the canonical route check", err)
		}
		if fixture.count("rootCommitments") != 2 {
			t.Fatalf("%s test did not reach complete source census", fault)
		}
	}
}

// One genuine HTTP 503 retries the same route, while canceled ownership never
// publishes a partial success after a completely successful coordinator read.
func TestOwnerRecycleOperatorsRetryTransportAndHonorLateCancellation(t *testing.T) {
	fixture := newRecycleOperatorFixture(t)
	fixture.transientFirst = true
	waits := 0
	fixture.chain.readRetryHooks.wait = func(ctx context.Context, delay time.Duration) error { waits++; return ctx.Err() }
	if owner, err := fixture.observe(t); err != nil || owner == nil || waits != 1 {
		t.Fatalf("actual temporary HTTP failure did not recover once: waits=%d err=%v", waits, err)
	}
	fixture = newRecycleOperatorFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.beforeRouteReturn = func(count int) {
		if count == 2 {
			cancel()
		}
	}
	owner, err := ObserveOwnerRecycleMeasurementOperators(ctx, fixture.measurement.authority, fixture.chain, fixture.measurement.encoded, fixture.measurement.provider.options(t))
	if owner != nil || !errors.Is(err, context.Canceled) || fixture.count("rootCommitments") != 2 {
		t.Fatalf("late cancellation lost its owner after completed reads: %v", err)
	}
}

// Candidate bytes can be rehashed freely; they still cannot replace observed
// operator facts, downgrade a v2 owner or borrow a v1 owner's weaker scope.
func TestOwnerRecycleOperatorsRejectTamperedOrDowngradedCapsule(t *testing.T) {
	fixture := newRecycleOperatorFixture(t)
	owner, err := fixture.observe(t)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := SealOwnerRecycleMeasurement(t.Context(), owner, fixture.measurement.encoded, fixture.measurement.provider.options(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"evidence", "downgrade", "provider", "old-owner"} {
		var candidate OwnerRecycleMeasurement
		if err := json.Unmarshal(encoded, &candidate); err != nil {
			t.Fatal(err)
		}
		selected := owner
		switch fault {
		case "evidence":
			var evidence OwnerRecycleOperatorEvidence
			if err := json.Unmarshal(candidate.OperatorEvidence, &evidence); err != nil {
				t.Fatal(err)
			}
			evidence.Operators[0].DepositRao = "0"
			candidate.OperatorEvidence, _ = json.Marshal(evidence)
			candidate.OperatorEvidence = append(candidate.OperatorEvidence, '\n')
		case "downgrade":
			candidate.Schema, candidate.OperatorEvidence = ownerRecycleMeasurementSchema, nil
		case "provider":
			candidate.ProviderMeasurement = append(candidate.ProviderMeasurement, ' ')
		case "old-owner":
			selected = fixture.measurement.authority
		}
		raw, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if intent, err := ReplayOwnerRecycleMeasurement(t.Context(), selected, append(raw, '\n'), fixture.measurement.provider.options(t)); err == nil || intent != nil {
			t.Fatalf("%s candidate replacement survived independent authority: %v", fault, err)
		}
	}
}

// The route whitelist and finite existing budget fail before any RPC, even
// when the caller has real approved native authority and complete proofs.
func TestOwnerRecycleOperatorsRejectUnapprovedRouteOrBudgetBeforeIo(t *testing.T) {
	fixture := newRecycleOperatorFixture(t)
	for _, fault := range []string{"route", "budget", "missing-chain"} {
		options := fixture.measurement.provider.options(t)
		chain := ChainClient{client: fixture.chain.client, rpcUrl: fixture.chain.rpcUrl, chainId: new(big.Int).Set(fixture.chain.chainId), coordinator: fixture.chain.coordinator, contractAddr: fixture.chain.contractAddr, release: true}
		selected := &chain
		switch fault {
		case "route":
			chain.rpcUrl = "https://unapproved.example"
		case "budget":
			options.MaxArtifactBytes = 1
		case "missing-chain":
			selected = nil
		}
		owner, err := ObserveOwnerRecycleMeasurementOperators(t.Context(), fixture.measurement.authority, selected, fixture.measurement.encoded, options)
		if err == nil || owner != nil {
			t.Fatalf("%s invalid owner admitted: %v", fault, err)
		}
	}
	requests := func() int { fixture.stateLock.Lock(); defer fixture.stateLock.Unlock(); return fixture.requests }()
	if requests != 0 {
		t.Fatalf("invalid admission performed %d actual RPC requests", requests)
	}
}
