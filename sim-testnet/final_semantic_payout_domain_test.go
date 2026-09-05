// Payout-domain regressions prove that independently re-signed operator
// artifacts cannot substitute provider identities, policy, or source bounds.
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/payoutartifact"
	"github.com/urfoundation/sn/ss58"
	"github.com/urnetwork/connect"
)

// Holds a compact, fully signed operator artifact and its independent
// topology/audit trust roots for adversarial unit vectors.
type finalPayoutArtifactFixture struct {
	evidence       *FinalSemanticEvidence
	pool           *FinalPoolUIDEvidence
	expected       *finalPayoutArtifactExpectation
	assignments    map[connect.Id]finalMinerAssignment
	key            *ecdsa.PrivateKey
	providers      []payoutartifact.ProviderInput
	crossOperator  payoutartifact.ProviderInput
	extraProvider  payoutartifact.ProviderInput
	reliabilityMin uint64
}

// Produces a stable nonzero logical client id for compact payout vectors.
func finalPayoutArtifactTestClient(value byte) connect.Id {
	var result connect.Id
	result[0], result[15] = 1, value
	return result
}

// Produces a stable, unique payout key for one logical provider.
func finalPayoutArtifactTestColdkey(value byte) [32]byte {
	var result [32]byte
	result[0], result[31] = 2, value
	return result
}

// Converts one trusted assignment into the same canonical Bittensor identity
// format captured by the release topology manifest.
func finalPayoutArtifactTestAssignment(t *testing.T, noID uint64, tier string, coldkey [32]byte) finalMinerAssignment {
	t.Helper()
	providerID, err := ss58.Encode(coldkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	return finalMinerAssignment{
		NoID: noID, Tier: tier, ProviderID: providerID, PayoutColdkey: coldkey,
	}
}

// Builds and signs canonical bytes after imposing the same provider order the
// production server now uses before its fleet-snapshot commitment.
func finalPayoutArtifactTestBuild(t *testing.T, fixture *finalPayoutArtifactFixture, noID uint64, providers []payoutartifact.ProviderInput, start, end payoutartifact.Boundary, reliabilityMin uint64) (*payoutartifact.Artifact, []byte) {
	t.Helper()
	ordered := append([]payoutartifact.ProviderInput(nil), providers...)
	sort.Slice(ordered, func(left, right int) bool {
		return bytes.Compare(ordered[left].ClientID[:], ordered[right].ClientID[:]) < 0
	})
	artifact, err := payoutartifact.Build(payoutartifact.BuildInput{
		DeploymentID:         fixture.evidence.DeploymentID,
		GenesisHash:          fixture.evidence.GenesisHash,
		PolicyHash:           fixture.evidence.PolicyHash,
		ChainID:              fixture.evidence.ChainID,
		Netuid:               fixture.evidence.Netuid,
		Coordinator:          common.HexToAddress(fixture.evidence.Deployment.CoordinatorProxy),
		SettlementVault:      common.HexToAddress(fixture.evidence.Deployment.SettlementVault),
		Epoch:                fixture.expected.Epoch,
		NoID:                 noID,
		Start:                start,
		End:                  end,
		OperatorSnapshotHash: finalPayoutOperatorSnapshotHash(noID, fixture.expected.Epoch, fixture.evidence.PolicyHash),
		FleetSnapshotHash:    finalPayoutFleetSnapshotHash(ordered),
		Providers:            ordered,
		ReliabilityAMin:      reliabilityMin,
		CreatedAt:            time.Unix(1_700_000_000, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(artifact, fixture.key); err != nil {
		t.Fatal(err)
	}
	data, err := payoutartifact.Bytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return artifact, data
}

// Projects the exact signed audit identity used to authenticate one decoded
// artifact while keeping provider rows outside the trusted expectation.
func finalPayoutArtifactTestExpectation(artifact *payoutartifact.Artifact) *finalPayoutArtifactExpectation {
	return &finalPayoutArtifactExpectation{
		NoID:             artifact.NoID,
		Epoch:            artifact.Epoch,
		UsageBytes:       artifact.TotalUsageBytes,
		PayoutRoot:       "0x" + hex.EncodeToString(artifact.PayoutRoot[:]),
		ArtifactHash:     "0x" + strings.TrimPrefix(artifact.ContentHash, "sha256:"),
		SourceStartBlock: artifact.Start.Number,
		SourceStartHash:  artifact.Start.Hash,
		SourceEndBlock:   artifact.End.Number,
		SourceEndHash:    artifact.End.Hash,
		Claims:           nil,
	}
}

// Creates two valid local providers plus independent cross-operator and extra
// identities so each substitution can bypass shallow content checks.
func finalPayoutArtifactTestFixture(t *testing.T) (*finalPayoutArtifactFixture, []byte) {
	t.Helper()
	key, err := crypto.ToECDSA(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	fixture := &finalPayoutArtifactFixture{
		evidence: &FinalSemanticEvidence{
			DeploymentID: "payout-domain-test",
			ChainID:      945,
			GenesisHash:  "0x" + strings.Repeat("11", 32),
			Netuid:       521,
			PolicyHash:   "0x" + strings.Repeat("22", 32),
			Deployment: FinalContractDeploymentEvidence{
				CoordinatorProxy: "0x1111111111111111111111111111111111111111",
				SettlementVault:  "0x3333333333333333333333333333333333333333",
			},
		},
		pool: &FinalPoolUIDEvidence{
			NoID: 1, PayoutRootSigner: strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex()),
		},
		assignments:    make(map[connect.Id]finalMinerAssignment, 4),
		key:            key,
		reliabilityMin: 8,
	}
	for index := byte(1); index <= 4; index++ {
		clientID := finalPayoutArtifactTestClient(index)
		coldkey := finalPayoutArtifactTestColdkey(index)
		noID := uint64(1)
		if index == 3 {
			noID = 2
		}
		fixture.assignments[clientID] = finalPayoutArtifactTestAssignment(t, noID, "pool-tail", coldkey)
		provider := payoutartifact.ProviderInput{
			ClientID: [16]byte(clientID), NetworkID: [16]byte{9, index}, Coldkey: coldkey,
			UsageBytes: 100 + uint64(index), Assignments: 8, Confirmations: 8, Eligible: true,
		}
		switch index {
		case 1, 2:
			fixture.providers = append(fixture.providers, provider)
		case 3:
			fixture.crossOperator = provider
		case 4:
			fixture.extraProvider = provider
		}
	}
	fixture.expected = &finalPayoutArtifactExpectation{
		NoID: 1, Epoch: 9,
	}
	start := payoutartifact.Boundary{Number: 100, Hash: "0x" + strings.Repeat("33", 32)}
	end := payoutartifact.Boundary{Number: 200, Hash: "0x" + strings.Repeat("44", 32)}
	artifact, data := finalPayoutArtifactTestBuild(t, fixture, 1, fixture.providers, start, end, fixture.reliabilityMin)
	fixture.expected = finalPayoutArtifactTestExpectation(artifact)
	return fixture, data
}

// Requires a mutation to reach and fail the intended semantic guard instead
// of succeeding or failing only a shallow fixture precondition.
func finalPayoutArtifactTestRequireRejection(t *testing.T, label, want string, fixture *finalPayoutArtifactFixture, expected *finalPayoutArtifactExpectation, assignments map[connect.Id]finalMinerAssignment, data []byte) {
	t.Helper()
	err := verifyFinalPayoutArtifact(fixture.evidence, fixture.pool, expected, assignments, fixture.reliabilityMin, data)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %v, want substring %q", label, err, want)
	}
}

// Accepts the complete canonical provider snapshot when every independent
// operator, payout-identity, policy, boundary, and leaf join agrees.
func TestFinalPayoutArtifactAcceptsAuthenticatedProviderDomain(t *testing.T) {
	fixture, data := finalPayoutArtifactTestFixture(t)
	if err := verifyFinalPayoutArtifact(fixture.evidence, fixture.pool, fixture.expected, fixture.assignments, fixture.reliabilityMin, data); err != nil {
		t.Fatal(err)
	}
}

// Rejects a validly rebuilt and operator-signed row belonging to another NO
// even when all content-address and Merkle expectations are substituted too.
func TestFinalPayoutArtifactRejectsResignedCrossOperatorProvider(t *testing.T) {
	fixture, _ := finalPayoutArtifactTestFixture(t)
	providers := append([]payoutartifact.ProviderInput(nil), fixture.providers...)
	providers[0] = fixture.crossOperator
	artifact, data := finalPayoutArtifactTestBuild(t, fixture, 1, providers, payoutartifact.Boundary{Number: fixture.expected.SourceStartBlock, Hash: fixture.expected.SourceStartHash}, payoutartifact.Boundary{Number: fixture.expected.SourceEndBlock, Hash: fixture.expected.SourceEndHash}, fixture.reliabilityMin)
	finalPayoutArtifactTestRequireRejection(t, "cross-operator provider", "belongs to operator 2", fixture, finalPayoutArtifactTestExpectation(artifact), fixture.assignments, data)
}

// Rejects swapping two legitimate payout identities between client rows after
// rebuilding the tree, updating its commitments, and re-signing it.
func TestFinalPayoutArtifactRejectsPayeeSwap(t *testing.T) {
	fixture, _ := finalPayoutArtifactTestFixture(t)
	providers := append([]payoutartifact.ProviderInput(nil), fixture.providers...)
	providers[0].Coldkey, providers[1].Coldkey = providers[1].Coldkey, providers[0].Coldkey
	artifact, data := finalPayoutArtifactTestBuild(t, fixture, 1, providers, payoutartifact.Boundary{Number: fixture.expected.SourceStartBlock, Hash: fixture.expected.SourceStartHash}, payoutartifact.Boundary{Number: fixture.expected.SourceEndBlock, Hash: fixture.expected.SourceEndHash}, fixture.reliabilityMin)
	finalPayoutArtifactTestRequireRejection(t, "payee swap", "coldkey differs from canonical identity", fixture, finalPayoutArtifactTestExpectation(artifact), fixture.assignments, data)
}

// Rejects an otherwise canonical operator statement rebuilt under a scoring
// floor different from the policy authenticated by validator measurements.
func TestFinalPayoutArtifactRejectsReliabilityAMinSwap(t *testing.T) {
	fixture, _ := finalPayoutArtifactTestFixture(t)
	artifact, data := finalPayoutArtifactTestBuild(t, fixture, 1, fixture.providers, payoutartifact.Boundary{Number: fixture.expected.SourceStartBlock, Hash: fixture.expected.SourceStartHash}, payoutartifact.Boundary{Number: fixture.expected.SourceEndBlock, Hash: fixture.expected.SourceEndHash}, fixture.reliabilityMin+1)
	finalPayoutArtifactTestRequireRejection(t, "a_min swap", "want signed policy value", fixture, finalPayoutArtifactTestExpectation(artifact), fixture.assignments, data)
}

// Rejects re-signed source hashes exchanged across otherwise valid start/end
// heights while retaining the exact validator-signed audit expectation.
func TestFinalPayoutArtifactRejectsSignedBoundarySwap(t *testing.T) {
	fixture, _ := finalPayoutArtifactTestFixture(t)
	start := payoutartifact.Boundary{Number: fixture.expected.SourceStartBlock, Hash: fixture.expected.SourceEndHash}
	end := payoutartifact.Boundary{Number: fixture.expected.SourceEndBlock, Hash: fixture.expected.SourceStartHash}
	artifact, data := finalPayoutArtifactTestBuild(t, fixture, 1, fixture.providers, start, end, fixture.reliabilityMin)
	expected := finalPayoutArtifactTestExpectation(artifact)
	expected.SourceStartHash = fixture.expected.SourceStartHash
	expected.SourceEndHash = fixture.expected.SourceEndHash
	finalPayoutArtifactTestRequireRejection(t, "boundary swap", "validator-signed deposit audit", fixture, expected, fixture.assignments, data)
}

// Rejects two logical clients sharing one payout identity even though the
// generic share builder can aggregate them into one otherwise valid leaf.
func TestFinalPayoutArtifactRejectsDuplicatePayoutColdkey(t *testing.T) {
	fixture, _ := finalPayoutArtifactTestFixture(t)
	providers := append([]payoutartifact.ProviderInput(nil), fixture.providers...)
	providers[1].Coldkey = providers[0].Coldkey
	assignments := make(map[connect.Id]finalMinerAssignment, len(fixture.assignments))
	for clientID, assignment := range fixture.assignments {
		assignments[clientID] = assignment
	}
	secondID := connect.Id(providers[1].ClientID)
	assignments[secondID] = finalPayoutArtifactTestAssignment(t, 1, "pool-tail", providers[0].Coldkey)
	artifact, data := finalPayoutArtifactTestBuild(t, fixture, 1, providers, payoutartifact.Boundary{Number: fixture.expected.SourceStartBlock, Hash: fixture.expected.SourceStartHash}, payoutartifact.Boundary{Number: fixture.expected.SourceEndBlock, Hash: fixture.expected.SourceEndHash}, fixture.reliabilityMin)
	finalPayoutArtifactTestRequireRejection(t, "duplicate coldkey", "duplicate one payout coldkey", fixture, finalPayoutArtifactTestExpectation(artifact), assignments, data)
}

// Rejects a freshly signed valid subset whose provider snapshot omits one row
// from the content identity already authenticated by the validator audit.
func TestFinalPayoutArtifactRejectsAuthenticatedProviderOmission(t *testing.T) {
	fixture, _ := finalPayoutArtifactTestFixture(t)
	_, data := finalPayoutArtifactTestBuild(t, fixture, 1, fixture.providers[:1], payoutartifact.Boundary{Number: fixture.expected.SourceStartBlock, Hash: fixture.expected.SourceStartHash}, payoutartifact.Boundary{Number: fixture.expected.SourceEndBlock, Hash: fixture.expected.SourceEndHash}, fixture.reliabilityMin)
	finalPayoutArtifactTestRequireRejection(t, "provider omission", "not the committed artifact hash", fixture, fixture.expected, fixture.assignments, data)
}

// Proves a freshly operator-signed provider subset cannot replace the exact
// content identity retained by both validator-signed deposit measurements.
func TestFinalPayoutArtifactRejectsResignedProviderOmissionAgainstSignedMeasurement(t *testing.T) {
	t.Parallel()
	source, artifacts := finalSemanticFixture(t)
	draft, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := SealFinalSemanticEvidenceOnChain(context.Background(), draft, &finalTestChainReader{evidence: draft})
	if err != nil {
		t.Fatal(err)
	}
	evidence := *sealed
	if len(evidence.Validators) != 2 || len(evidence.Validators[0].Cycles) == 0 || len(evidence.Validators[0].Cycles[0].Pools) == 0 {
		t.Fatal("release fixture does not contain two validator payout measurements")
	}
	target := evidence.Validators[0].Cycles[0].Pools[0]
	originalData, ok := artifacts[target.PayoutArtifact.URI]
	if !ok {
		t.Fatalf("fixture payout artifact %s is absent", target.PayoutArtifact.URI)
	}
	original, err := payoutartifact.Decode(originalData)
	if err != nil {
		t.Fatal(err)
	}
	if len(original.Providers) < 2 || original.NoID == 0 || original.NoID > 2 {
		t.Fatalf("fixture payout domain has %d providers for operator %d", len(original.Providers), original.NoID)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, original.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	providers := append([]payoutartifact.ProviderInput(nil), original.Providers[:len(original.Providers)-1]...)
	subset, err := payoutartifact.Build(payoutartifact.BuildInput{
		DeploymentID: original.DeploymentID, GenesisHash: original.GenesisHash, PolicyHash: original.PolicyHash,
		ChainID: original.ChainID, Netuid: original.Netuid, Coordinator: original.Coordinator, SettlementVault: original.SettlementVault,
		Epoch: original.Epoch, NoID: original.NoID, Start: original.Start, End: original.End,
		OperatorSnapshotHash: original.OperatorSnapshotHash, FleetSnapshotHash: finalPayoutFleetSnapshotHash(providers),
		Providers: providers, ReliabilityAMin: original.ReliabilityAMin, CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.ToECDSA(bytes.Repeat([]byte{byte(original.NoID)}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(subset, key); err != nil {
		t.Fatal(err)
	}
	subsetData, err := payoutartifact.Bytes(subset)
	if err != nil {
		t.Fatal(err)
	}
	subsetLocator := FinalArtifactLocator{
		Kind: target.PayoutArtifact.Kind, URI: target.PayoutArtifact.URI + ".omitted",
		ContentHash: bytesSHA256(subsetData), SizeBytes: uint64(len(subsetData)),
	}
	replaced := 0
	for validatorIndex := range evidence.Validators {
		for cycleIndex := range evidence.Validators[validatorIndex].Cycles {
			cycle := &evidence.Validators[validatorIndex].Cycles[cycleIndex]
			for poolIndex := range cycle.Pools {
				pool := &cycle.Pools[poolIndex]
				if pool.PayoutArtifact.URI == target.PayoutArtifact.URI {
					pool.PayoutArtifact = subsetLocator
					replaced++
				}
			}
		}
	}
	if replaced != len(evidence.Validators) {
		t.Fatalf("replaced %d validator payout locators, want %d", replaced, len(evidence.Validators))
	}
	artifacts[subsetLocator.URI] = subsetData
	resignFinalSemantic(t, &evidence)
	load := func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		data, exists := artifacts[locator.URI]
		if !exists {
			return nil, fmt.Errorf("missing fixture artifact %s", locator.URI)
		}
		return append([]byte(nil), data...), nil
	}
	err = VerifyFinalSemanticArtifacts(context.Background(), &evidence, load)
	if err == nil || !strings.Contains(err.Error(), "not the committed artifact hash") {
		t.Fatalf("re-signed provider omission against signed measurement error = %v", err)
	}
}

// Rejects an opaque snapshot digest re-signed under a different operator
// domain even when its new content identity is substituted into the caller.
func TestFinalPayoutArtifactRejectsOperatorSnapshotDomainSwap(t *testing.T) {
	fixture, data := finalPayoutArtifactTestFixture(t)
	artifact, err := payoutartifact.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	artifact.OperatorSnapshotHash = "sha256:" + strings.Repeat("77", 32)
	if err := payoutartifact.Sign(artifact, fixture.key); err != nil {
		t.Fatal(err)
	}
	data, err = payoutartifact.Bytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	finalPayoutArtifactTestRequireRejection(t, "operator snapshot", "operator/epoch/policy domain", fixture, finalPayoutArtifactTestExpectation(artifact), fixture.assignments, data)
}

// Rejects a re-signed fleet digest that does not commit the exact ordered
// provider rows carried by the canonical payout artifact.
func TestFinalPayoutArtifactRejectsProviderCensusCommitmentSwap(t *testing.T) {
	fixture, data := finalPayoutArtifactTestFixture(t)
	artifact, err := payoutartifact.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	artifact.FleetSnapshotHash = "sha256:" + strings.Repeat("88", 32)
	if err := payoutartifact.Sign(artifact, fixture.key); err != nil {
		t.Fatal(err)
	}
	data, err = payoutartifact.Bytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	finalPayoutArtifactTestRequireRejection(t, "provider census", "exact provider census", fixture, finalPayoutArtifactTestExpectation(artifact), fixture.assignments, data)
}

// Rejects extra provider/leaf rows and a stale provider-snapshot commitment,
// including operator-signed encodings that bypass shallow signature checks.
func TestFinalPayoutArtifactRejectsMalformedAndExtraRows(t *testing.T) {
	fixture, baseData := finalPayoutArtifactTestFixture(t)
	baseArtifact, err := payoutartifact.Decode(baseData)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		want     string
		artifact *payoutartifact.Artifact
		data     []byte
		expected *finalPayoutArtifactExpectation
	}{
		{name: "extra-provider", want: "not the committed artifact hash"},
		{name: "extra-leaf", want: "summary, providers, leaves, or proofs do not reconstruct"},
		{name: "stale-provider-snapshot", want: "provider snapshot hash mismatch"},
	}
	extraProviders := append([]payoutartifact.ProviderInput(nil), fixture.providers...)
	extraProviders = append(extraProviders, fixture.extraProvider)
	tests[0].artifact, tests[0].data = finalPayoutArtifactTestBuild(t, fixture, 1, extraProviders, baseArtifact.Start, baseArtifact.End, fixture.reliabilityMin)
	tests[0].expected = fixture.expected

	extraLeafArtifact := *baseArtifact
	extraLeafArtifact.Providers = append([]payoutartifact.ProviderInput(nil), baseArtifact.Providers...)
	extraLeafArtifact.Leaves = append([]payoutartifact.Leaf(nil), baseArtifact.Leaves...)
	extraLeafArtifact.Leaves = append(extraLeafArtifact.Leaves, payoutartifact.Leaf{
		Index: uint64(len(extraLeafArtifact.Leaves)), ClientID: fixture.extraProvider.ClientID,
		Coldkey: fixture.extraProvider.Coldkey, ShareBPS: 1, Proof: nil,
	})
	if err := payoutartifact.Sign(&extraLeafArtifact, fixture.key); err != nil {
		t.Fatal(err)
	}
	tests[1].artifact = &extraLeafArtifact
	tests[1].data, err = json.Marshal(&extraLeafArtifact)
	if err != nil {
		t.Fatal(err)
	}
	tests[1].expected = finalPayoutArtifactTestExpectation(&extraLeafArtifact)

	staleSnapshotArtifact := *baseArtifact
	staleSnapshotArtifact.Providers = append([]payoutartifact.ProviderInput(nil), baseArtifact.Providers...)
	staleSnapshotArtifact.Leaves = append([]payoutartifact.Leaf(nil), baseArtifact.Leaves...)
	staleSnapshotArtifact.ProviderSnapshotHash = "sha256:" + strings.Repeat("99", 32)
	if err := payoutartifact.Sign(&staleSnapshotArtifact, fixture.key); err != nil {
		t.Fatal(err)
	}
	tests[2].artifact = &staleSnapshotArtifact
	tests[2].data, err = json.Marshal(&staleSnapshotArtifact)
	if err != nil {
		t.Fatal(err)
	}
	tests[2].expected = finalPayoutArtifactTestExpectation(&staleSnapshotArtifact)

	for _, test := range tests {
		finalPayoutArtifactTestRequireRejection(t, test.name, test.want, fixture, test.expected, fixture.assignments, test.data)
	}
}

// Rejects malformed, noncanonical, or duplicated payout identities before an
// artifact can use them as an apparently valid operator assignment.
func TestFinalPayoutArtifactRejectsInvalidTopologyPayoutIdentities(t *testing.T) {
	firstID := finalPayoutArtifactTestClient(1)
	secondID := finalPayoutArtifactTestClient(2)
	coldkey := finalPayoutArtifactTestColdkey(1)
	providerID, err := ss58.Encode(coldkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		bindings []FinalFleetMemberBindingEvidence
	}{
		{name: "malformed", bindings: []FinalFleetMemberBindingEvidence{{MinerID: 1, NoID: 1, ClientID: firstID.String(), ProviderID: "not-ss58", Tier: "pool-tail"}}},
		{name: "duplicate-coldkey", bindings: []FinalFleetMemberBindingEvidence{
			{MinerID: 1, NoID: 1, ClientID: firstID.String(), ProviderID: providerID, Tier: "pool-tail"},
			{MinerID: 2, NoID: 1, ClientID: secondID.String(), ProviderID: providerID, Tier: "pool-tail"},
		}},
	}
	for _, test := range tests {
		data, marshalErr := json.Marshal(test.bindings)
		if marshalErr != nil {
			t.Fatalf("%s: %v", test.name, marshalErr)
		}
		if assignments, decodeErr := finalMinerTierByClient(data); decodeErr == nil {
			t.Fatalf("%s unexpectedly produced %+v", test.name, assignments)
		}
	}
}

// Allows one coldkey to receive independently authorized shares from distinct
// operator pools while retaining per-pool uniqueness.
func TestFinalPayoutArtifactAllowsPayoutColdkeyReuseAcrossOperators(t *testing.T) {
	firstID := finalPayoutArtifactTestClient(1)
	secondID := finalPayoutArtifactTestClient(2)
	coldkey := finalPayoutArtifactTestColdkey(1)
	providerID, err := ss58.Encode(coldkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal([]FinalFleetMemberBindingEvidence{
		{MinerID: 1, NoID: 1, ClientID: firstID.String(), ProviderID: providerID, Tier: "pool-tail"},
		{MinerID: 2, NoID: 2, ClientID: secondID.String(), ProviderID: providerID, Tier: "pool-tail"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assignments, err := finalMinerTierByClient(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 2 || assignments[firstID].NoID != 1 || assignments[secondID].NoID != 2 {
		t.Fatalf("cross-operator assignments = %+v, want exact independent operator domains", assignments)
	}
}

// Pins the synthetic identities used by the test fixture so diagnostic output
// cannot silently drift from the raw provider bytes under review.
func TestFinalPayoutArtifactFixtureUsesCanonicalPayoutIdentities(t *testing.T) {
	fixture, _ := finalPayoutArtifactTestFixture(t)
	for clientID, assignment := range fixture.assignments {
		decoded, prefix, err := ss58.Decode(assignment.ProviderID)
		if err != nil || prefix != ss58.BittensorPrefix || decoded != assignment.PayoutColdkey {
			t.Fatalf("client %s payout identity %s is not canonical: %v", clientID.String(), assignment.ProviderID, err)
		}
		if assignment.ProviderID == fmt.Sprintf("%x", assignment.PayoutColdkey) {
			t.Fatalf("client %s payout identity is raw hex rather than SS58", clientID.String())
		}
	}
}
