package validator

// release_measurement_test.go exercises the public transcript at the release
// boundary: 202 candidates compete for 200 slots while pool quality, policy
// clamps, dishonest deposits and EMA lineage are reconstructed from raw input.

import (
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"path/filepath"
	"sort"
	"testing"

	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/protocol"
)

// releaseMeasurementTestID returns stable UUID-shaped provider identities.
func releaseMeasurementTestID(index uint64) connect.Id {
	var clientID connect.Id
	clientID[0] = 1
	binary.BigEndian.PutUint64(clientID[8:], index)
	return clientID
}

// releaseMeasurementTestHash returns a stable nonzero public 32-byte value.
func releaseMeasurementTestHash(index uint64) [32]byte {
	var hash [32]byte
	hash[0] = 1
	binary.BigEndian.PutUint64(hash[24:], index)
	return hash
}

func releaseMeasurementDepositAudit(t *testing.T, policy protocol.Policy, noID uint64) DepositAudit {
	t.Helper()
	conviction := big.NewInt(0)
	usage := uint64(1 << 30)
	required, tier, err := protocol.RequiredDepositRao(usage, conviction, policy.Deposit)
	if err != nil {
		t.Fatal(err)
	}
	artifactHash := releaseMeasurementTestHash(20_000 + noID)
	return DepositAudit{
		NoID: noID, Epoch: 4, SourceEpoch: 3,
		ArtifactHash: fmt.Sprintf("sha256:%x", artifactHash), CommittedArtifactHash: releaseHex32(artifactHash),
		PayoutRoot:       releaseHex32(releaseMeasurementTestHash(21_000 + noID)),
		ArtifactSigner:   "0x0000000000000000000000000000000000000011",
		RootCommitter:    "0x0000000000000000000000000000000000000012",
		RootSigner:       "0x0000000000000000000000000000000000000012",
		SourceStartBlock: 10, SourceStartHash: releaseHex32(releaseMeasurementTestHash(22_000 + noID)),
		SourceEndBlock: 90, SourceEndHash: releaseHex32(releaseMeasurementTestHash(23_000 + noID)),
		RootCommitBlock: 91, ObservedAtBlock: 98, ArtifactDeadlineBlock: 100,
		UsageBytes: usage, ConvictionBeforeRao: conviction.String(),
		RateNumeratorRaoPerGiB: tier.RateNumeratorRaoPerGiB, RateDenominator: tier.RateDenominator,
		RequiredDepositRao: required.String(), ObservedDepositRao: required.String(),
		Status: DepositAuditCompliant, Compliant: true, Disposition: "pool_weight_eligible",
	}
}

// cloneReleaseMeasurementArtifact makes mutations independent between tests.
func cloneReleaseMeasurementArtifact(t *testing.T, artifact *ReleaseMeasurementArtifact) *ReleaseMeasurementArtifact {
	t.Helper()
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	var clone ReleaseMeasurementArtifact
	if err := json.Unmarshal(encoded, &clone); err != nil {
		t.Fatal(err)
	}
	return &clone
}

type releaseMeasurementAttemptToken struct {
	clientID connect.Id
	binding  AttemptBinding
	bucket   uint8
	egress   [32]byte
}

// attachReleaseMeasurementAttemptCuts builds cryptographically valid,
// deterministic attempt cuts for a measurement fixture. The public artifact
// verifier is intentionally unable to accept raw counters or prefix hashes
// without the signed records that generated them.
func attachReleaseMeasurementAttemptCuts(t *testing.T, artifact *ReleaseMeasurementArtifact) {
	t.Helper()
	validatorSeed := [32]byte{31: 0x71}
	serverSeed := [32]byte{31: 0x72}
	validatorKey := ed25519.NewKeyFromSeed(validatorSeed[:])
	validatorVPK := validatorKey.Public().(ed25519.PublicKey)
	serverKey := ed25519.NewKeyFromSeed(serverSeed[:])
	serverVPK := serverKey.Public().(ed25519.PublicKey)
	bindingByNO := map[uint64]map[connect.Id]AttemptBinding{}
	for _, binding := range artifact.Bindings {
		clientID, err := connect.ParseId(binding.ClientID)
		if err != nil {
			t.Fatal(err)
		}
		if bindingByNO[binding.NoID] == nil {
			bindingByNO[binding.NoID] = map[connect.Id]AttemptBinding{}
		}
		attemptBinding := AttemptBinding{ClientID: clientID, FleetID: releaseHex32([32]byte{}), Hotkey: releaseHex32([32]byte{})}
		if binding.Active {
			attemptBinding.Active = true
			attemptBinding.FleetID = binding.FleetID
			attemptBinding.Hotkey = binding.Hotkey
			attemptBinding.Generation = binding.Generation
			attemptBinding.UIDFound = binding.LiveUIDFound
			attemptBinding.UID = binding.LiveUID
		}
		bindingByNO[binding.NoID][clientID] = attemptBinding
	}
	rootDir := t.TempDir()
	for inputIndex := range artifact.Inputs {
		input := &artifact.Inputs[inputIndex]
		ledger, err := NewAttemptLedger(filepath.Join(rootDir, fmt.Sprintf("no-%d", input.NoID)), AttemptLedgerIdentity{
			DeploymentID: artifact.DeploymentID, ChainID: artifact.ChainID,
			GenesisHash: artifact.GenesisHash, Netuid: artifact.Netuid,
			ValidatorID: artifact.ValidatorID, ValidatorUID: artifact.SelfUID, NoID: input.NoID,
		}, validatorKey)
		if err != nil {
			t.Fatal(err)
		}
		ledger.appendFn = func(string, []byte) error { return nil }
		boundary := AttemptBoundary{SettlementEpoch: input.SettlementEpoch, EVMBlock: input.CutEVMSnapshotBlock, EVMBlockHash: input.CutEVMSnapshotHash}
		maxConfirmations := uint64(0)
		for _, provider := range input.Stats.Providers {
			if provider.Assignments != provider.Confirmations {
				t.Fatal("release measurement fixture only supports completed attempts")
			}
			if provider.Confirmations > maxConfirmations {
				maxConfirmations = provider.Confirmations
			}
		}
		sequence := uint64(0)
		for round := uint64(0); round < maxConfirmations; round++ {
			tokens := make([]releaseMeasurementAttemptToken, 0, len(input.Stats.Providers))
			for _, provider := range input.Stats.Providers {
				if round >= provider.Confirmations {
					continue
				}
				clientID, parseErr := connect.ParseId(provider.ClientID)
				if parseErr != nil {
					t.Fatal(parseErr)
				}
				bucket := uint8(0)
				remaining := round
				foundBucket := false
				for bucketIndex, count := range provider.LatencyBuckets {
					if remaining < count {
						bucket = uint8(bucketIndex)
						foundBucket = true
						break
					}
					remaining -= count
				}
				if !foundBucket {
					t.Fatalf("provider %s has no latency bucket for confirmation %d", provider.ClientID, round)
				}
				var egress [32]byte
				if len(provider.EgressIPHashHexes) != 0 {
					egress, parseErr = parseReleaseHex32("fixture egress hash", provider.EgressIPHashHexes[round%uint64(len(provider.EgressIPHashHexes))], false)
					if parseErr != nil {
						t.Fatal(parseErr)
					}
				}
				tokens = append(tokens, releaseMeasurementAttemptToken{clientID: clientID, binding: bindingByNO[input.NoID][clientID], bucket: bucket, egress: egress})
			}
			for len(tokens) != 0 {
				groupSize := len(tokens)
				if groupSize > connect.VerifyMMax-1 {
					groupSize = connect.VerifyMMax - 1
					if remainder := len(tokens) - groupSize; remainder > 0 && remainder < connect.VerifyMMin-1 {
						groupSize -= connect.VerifyMMin - 1 - remainder
					}
				}
				if groupSize < connect.VerifyMMin-1 {
					t.Fatalf("fixture round has only %d distinct providers", groupSize)
				}
				group := tokens[:groupSize]
				tokens = tokens[groupSize:]
				sequence++
				trailID := releaseMeasurementTestID(2_000_000 + input.NoID*10_000 + sequence)
				seedID := releaseMeasurementTestID(3_000_000 + input.NoID*10_000 + sequence)
				nonce := make([]byte, connect.VerifyNonceSize)
				binary.BigEndian.PutUint64(nonce[len(nonce)-8:], input.NoID*10_000+sequence)
				m := len(group) + 1
				trail := []connect.Id{seedID}
				hops := []connect.VerifyProofHop{{ClientId: seedID, TimeMs: sequence * 1000}}
				assignments := make([]AttemptAssignment, 0, len(group))
				for tokenIndex, token := range group {
					walked := append(append([]connect.Id(nil), trail...), token.clientID)
					message, buildErr := connect.BuildVerifyAssignMessage(1, trailID, nonce, validatorVPK, byte(m), walked)
					if buildErr != nil {
						t.Fatal(buildErr)
					}
					assignments = append(assignments, AttemptAssignment{
						Trail: append([]connect.Id(nil), trail...), NextHop: token.clientID,
						ServerKeyID: 1, AssignMessage: message, AssignSignature: ed25519.Sign(serverKey, message),
						Confirmed: true, HasLatency: true, LatencyBucket: token.bucket, Binding: token.binding,
					})
					trail = walked
					hops = append(hops, connect.VerifyProofHop{ClientId: token.clientID, TimeMs: sequence*1000 + uint64(tokenIndex+1), EgressIpHash: token.egress})
				}
				finalMessage, buildErr := connect.BuildVerifyFinalMessage(1, trailID, nonce, validatorVPK, byte(m), hops)
				if buildErr != nil {
					t.Fatal(buildErr)
				}
				extendMessage, buildErr := connect.BuildVerifyExtendMessage(trailID, nonce, validatorVPK, byte(m), trail)
				if buildErr != nil {
					t.Fatal(buildErr)
				}
				finalDigest := connect.VerifyFinalDigest(finalMessage)
				pathID := TrailPathId(trailID, validatorVPK, 1)
				proof := &ProofRecord{
					Version: 1, Epoch: input.SettlementEpoch, TrailId: trailID,
					ServerNonce: nonce, Vpk: append([]byte(nil), validatorVPK...), M: m, Hops: hops,
					ServerKeyId: 1, FinalSig: ed25519.Sign(serverKey, finalMessage),
					VerifierSig: ed25519.Sign(validatorKey, extendMessage), FinalDigest: finalDigest[:],
					VpkSig: ed25519.Sign(validatorKey, finalMessage), Coverage: uint64(m - 1),
					PathId: pathID[:], CompleteTimeMs: hops[len(hops)-1].TimeMs,
				}
				for assignmentIndex := range assignments {
					checkpoint := append([]AttemptAssignment(nil), assignments[:assignmentIndex+1]...)
					checkpoint = attemptAssignmentsWithUnconfirmedLast(checkpoint)
					if _, appendErr := ledger.Append(AttemptRecord{
						Boundary: boundary, TrailID: trailID, ServerNonce: nonce, M: m,
						Assignments: checkpoint, Disposition: AttemptDispositionPending,
					}); appendErr != nil {
						t.Fatal(appendErr)
					}
				}
				if _, appendErr := ledger.Append(AttemptRecord{
					Boundary: boundary, TrailID: trailID, ServerNonce: nonce, M: m,
					Assignments: assignments, Disposition: AttemptDispositionComplete, Proof: proof,
				}); appendErr != nil {
					t.Fatal(appendErr)
				}
			}
		}
		cut, err := ledger.BuildCut(boundary, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyAttemptLedgerCut(cut, validatorVPK, map[byte]ed25519.PublicKey{1: serverVPK}); err != nil {
			t.Fatal(err)
		}
		input.Stats.AttemptCut = cut
	}
}

// releaseMeasurementTopFixture builds two isolated pools plus a configurable
// number of one-client head fleets. The first two share one routable prefix and
// each has one unique prefix, giving each an exact raw score of 3/2.
func releaseMeasurementTopFixture(t *testing.T, headCount int) *ReleaseMeasurementArtifact {
	t.Helper()
	policy := exactPolicy(t)
	policyHash, err := policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	statsConfig := ReleaseStatsConfig{
		AMin: policy.Verify.ReliabilityAMin, AlphaNumerator: releasePoolAlphaNumerator,
		AlphaDenominator: releasePoolAlphaDenominator, LatRefMillis: releasePoolLatRefMillis,
	}
	providersByNO := map[uint64][]ReleaseProviderMeasurement{1: {}, 2: {}}
	bindings := make([]ReleaseBindingMeasurement, 0, headCount+2)
	headEMA := make([]HeadEMAMeasurement, 0, headCount)
	zeroHash := releaseHex32([32]byte{})
	for noID := uint64(1); noID <= 2; noID++ {
		clientID := releaseMeasurementTestID(10_000 + noID)
		providersByNO[noID] = append(providersByNO[noID], ReleaseProviderMeasurement{
			ClientID: clientID.String(), LatencyBuckets: make([]uint64, statsLatencyBuckets),
			HasPriorQuality: true, PriorQualityPPM: 500_000,
		})
		bindings = append(bindings, ReleaseBindingMeasurement{
			NoID: noID, ClientID: clientID.String(), FleetID: zeroHash, Hotkey: zeroHash,
			ClientKey: zeroHash, LocalClientKey: zeroHash, CommitmentHash: zeroHash,
		})
	}
	sharedHash := releaseHex32(releaseMeasurementTestHash(900_000))
	for index := 0; index < headCount; index++ {
		noID := uint64(index%2 + 1)
		clientID := releaseMeasurementTestID(uint64(index + 1))
		fleetID := releaseMeasurementTestHash(uint64(1000 + index))
		hotkey := releaseMeasurementTestHash(uint64(2000 + index))
		clientKey := releaseMeasurementTestHash(uint64(3000 + index))
		commitmentHash := releaseMeasurementTestHash(uint64(4000 + index))
		uid := uint16(10 + index)
		egressHashes := []string{releaseHex32(releaseMeasurementTestHash(uint64(5000 + index)))}
		raw := big.NewRat(1, 1)
		if index < 2 {
			egressHashes = append(egressHashes, sharedHash)
			sort.Strings(egressHashes)
			raw = big.NewRat(3, 2)
		}
		latencyBuckets := make([]uint64, statsLatencyBuckets)
		latencyBuckets[latencyBucket(100)] = policy.Verify.ReliabilityAMin
		providersByNO[noID] = append(providersByNO[noID], ReleaseProviderMeasurement{
			ClientID: clientID.String(), Assignments: policy.Verify.ReliabilityAMin,
			Confirmations: policy.Verify.ReliabilityAMin, LatencyBuckets: latencyBuckets,
			EgressIPHashHexes: egressHashes,
		})
		bindings = append(bindings, ReleaseBindingMeasurement{
			NoID: noID, ClientID: clientID.String(), Active: true,
			FleetID: releaseHex32(fleetID), Hotkey: releaseHex32(hotkey),
			ClientKey: releaseHex32(clientKey), LocalClientKey: releaseHex32(clientKey),
			CommitmentHash: releaseHex32(commitmentHash), Generation: 1,
			ValidFromEpoch: 1, ValidToEpoch: 32, RecordUID: uid, LiveUIDFound: true, LiveUID: uid,
		})
		rawJSON, encodeErr := encodeRationalJSON(raw)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		headEMA = append(headEMA, HeadEMAMeasurement{
			Key:    FleetScoreKey{FleetID: fleetID, Hotkey: hotkey, Generation: 1, UID: uid},
			HasRaw: true, Raw: rawJSON, Prior: RationalJSON{Numerator: "0", Denominator: "1"}, Next: rawJSON,
		})
	}
	// A successful verify trail contains at least three distinct server-assigned
	// providers. Small fixtures retain that real protocol shape by adding
	// zero-prefix members to the already-declared fleet; they neither create a
	// candidate nor change its routable-prefix score.
	for noID := uint64(1); noID <= 2; noID++ {
		var base *ReleaseBindingMeasurement
		activeCount := 0
		for bindingIndex := range bindings {
			if bindings[bindingIndex].NoID == noID && bindings[bindingIndex].Active {
				activeCount++
				if base == nil {
					base = &bindings[bindingIndex]
				}
			}
		}
		for paddingIndex := activeCount; base != nil && paddingIndex < connect.VerifyMMin-1; paddingIndex++ {
			clientID := releaseMeasurementTestID(70_000 + noID*100 + uint64(paddingIndex))
			clientKey := releaseMeasurementTestHash(71_000 + noID*100 + uint64(paddingIndex))
			commitmentHash := releaseMeasurementTestHash(72_000 + noID*100 + uint64(paddingIndex))
			latencyBuckets := make([]uint64, statsLatencyBuckets)
			latencyBuckets[latencyBucket(100)] = policy.Verify.ReliabilityAMin
			providersByNO[noID] = append(providersByNO[noID], ReleaseProviderMeasurement{
				ClientID: clientID.String(), Assignments: policy.Verify.ReliabilityAMin,
				Confirmations: policy.Verify.ReliabilityAMin, LatencyBuckets: latencyBuckets,
			})
			bindings = append(bindings, ReleaseBindingMeasurement{
				NoID: noID, ClientID: clientID.String(), Active: true,
				FleetID: base.FleetID, Hotkey: base.Hotkey,
				ClientKey: releaseHex32(clientKey), LocalClientKey: releaseHex32(clientKey),
				CommitmentHash: releaseHex32(commitmentHash), Generation: base.Generation,
				ValidFromEpoch: base.ValidFromEpoch, ValidToEpoch: base.ValidToEpoch,
				RecordUID: base.RecordUID, LiveUIDFound: true, LiveUID: base.LiveUID,
			})
		}
	}
	for noID := uint64(1); noID <= 2; noID++ {
		sort.Slice(providersByNO[noID], func(i, j int) bool { return providersByNO[noID][i].ClientID < providersByNO[noID][j].ClientID })
	}
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].NoID != bindings[j].NoID {
			return bindings[i].NoID < bindings[j].NoID
		}
		return bindings[i].ClientID < bindings[j].ClientID
	})
	sort.Slice(headEMA, func(i, j int) bool { return headEMA[i].Key.String() < headEMA[j].Key.String() })
	audits := []DepositAudit{releaseMeasurementDepositAudit(t, policy, 1), releaseMeasurementDepositAudit(t, policy, 2)}
	artifact := &ReleaseMeasurementArtifact{
		Schema: ReleaseMeasurementSchema, DeploymentID: "measurement-test", ChainID: 945,
		GenesisHash: releaseHex32(releaseMeasurementTestHash(1)), Coordinator: "0x0000000000000000000000000000000000000001",
		SettlementVault: "0x0000000000000000000000000000000000000002", ValidatorID: 1, Netuid: 521,
		SubnetEpoch: 7, NativeSnapshotBlock: 100, NativeSnapshotHash: releaseHex32(releaseMeasurementTestHash(2)),
		EVMSnapshotBlock: 98, EVMSnapshotHash: releaseHex32(releaseMeasurementTestHash(3)), SettlementEpoch: 4,
		PolicyHash: fmt.Sprintf("0x%x", policyHash), Policy: policy,
		ControlledNOIDs: []uint64{},
		Inputs: []ReleaseMeasurementInput{
			{NoID: 1, SettlementEpoch: 4, CutNativeBlock: 90, CutNativeBlockHash: releaseHex32(releaseMeasurementTestHash(4)), CutEVMSnapshotBlock: 90, CutEVMSnapshotHash: releaseHex32(releaseMeasurementTestHash(7)), Stats: ReleaseStatsMeasurement{Config: statsConfig, Providers: providersByNO[1]}},
			{NoID: 2, SettlementEpoch: 4, CutNativeBlock: 90, CutNativeBlockHash: releaseHex32(releaseMeasurementTestHash(4)), CutEVMSnapshotBlock: 90, CutEVMSnapshotHash: releaseHex32(releaseMeasurementTestHash(7)), Stats: ReleaseStatsMeasurement{Config: statsConfig, Providers: providersByNO[2]}},
		},
		Bindings: bindings, HeadEMA: headEMA,
		Pools: []ReleasePoolMeasurement{
			{NoID: 1, UID: 1, PoolHotkey: releaseHex32(releaseMeasurementTestHash(5))},
			{NoID: 2, UID: 2, PoolHotkey: releaseHex32(releaseMeasurementTestHash(6))},
		},
		DepositAudits: audits, SelfUID: 3,
	}
	attachReleaseMeasurementAttemptCuts(t, artifact)
	return artifact
}

// TestReleaseMeasurementReconstructsTop200AndPoolClamp proves the complete
// promotion boundary and the policy's 500k-to-750k pool-quality clamp.
func TestReleaseMeasurementReconstructsTop200AndPoolClamp(t *testing.T) {
	artifact := releaseMeasurementTopFixture(t, 202)
	encoded, _, verified, err := SealReleaseMeasurementArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if len(verified.EligibleHead) != 202 || len(verified.SelectedHead) != 200 || len(verified.RejectedHead) != 2 {
		t.Fatalf("head boundary eligible=%d selected=%d rejected=%d", len(verified.EligibleHead), len(verified.SelectedHead), len(verified.RejectedHead))
	}
	if verified.RejectedHead[0].UID != 210 || verified.RejectedHead[1].UID != 211 {
		t.Fatalf("rejected head UIDs = %d,%d, want 210,211", verified.RejectedHead[0].UID, verified.RejectedHead[1].UID)
	}
	for _, pool := range verified.Pools {
		if pool.QualityPPM != 500_000 || !pool.Eligible {
			t.Fatalf("pool no_id %d quality/eligibility = %d/%v", pool.NoID, pool.QualityPPM, pool.Eligible)
		}
		deposit, ok := new(big.Int).SetString(pool.Audit.ObservedDepositRao, 10)
		if !ok {
			t.Fatal("fixture deposit is invalid")
		}
		want, scoreErr := impliedUsageQuality(deposit, big.NewInt(0), 500_000, artifact.Policy)
		if scoreErr != nil {
			t.Fatal(scoreErr)
		}
		if pool.Score.Cmp(want) != 0 {
			t.Fatalf("pool no_id %d score %s, want clamped score %s", pool.NoID, pool.Score, want)
		}
	}
	finalUIDs := map[uint16]bool{}
	for _, uid := range verified.UIDs {
		finalUIDs[uid] = true
	}
	if !finalUIDs[10] || !finalUIDs[209] || finalUIDs[210] || finalUIDs[211] {
		t.Fatalf("final vector does not enforce selected-positive/rejected-zero: %v", verified.UIDs)
	}
	decoded, replayed, err := DecodeReleaseMeasurementArtifact(encoded)
	if err != nil || decoded.SubnetEpoch != artifact.SubnetEpoch || len(replayed.SelectedHead) != 200 {
		t.Fatalf("canonical replay failed: decoded=%+v selected=%d err=%v", decoded, len(replayed.SelectedHead), err)
	}
}

// TestReleaseMeasurementRejectsUnprovenPositiveHeadScore deterministically
// removes one measured prefix while retaining the declared EMA raw score.
func TestReleaseMeasurementRejectsUnprovenPositiveHeadScore(t *testing.T) {
	artifact := cloneReleaseMeasurementArtifact(t, releaseMeasurementTopFixture(t, 202))
	for inputIndex := range artifact.Inputs {
		for providerIndex := range artifact.Inputs[inputIndex].Stats.Providers {
			provider := &artifact.Inputs[inputIndex].Stats.Providers[providerIndex]
			if provider.ClientID == releaseMeasurementTestID(1).String() {
				provider.EgressIPHashHexes = provider.EgressIPHashHexes[:1]
			}
		}
	}
	if _, err := VerifyReleaseMeasurementArtifact(artifact); err == nil {
		t.Fatal("positive head score survived removal of its measured routable-prefix evidence")
	}
}

// TestReleaseMeasurementRejectsReboundPrefixInheritance proves that prefix
// evidence measured for one fleet generation cannot be joined to the same
// provider IDs after their coordinator binding changes. This is the adjacent
// identity-churn case that a provider-keyed egress map alone cannot detect.
func TestReleaseMeasurementRejectsReboundPrefixInheritance(t *testing.T) {
	artifact := cloneReleaseMeasurementArtifact(t, releaseMeasurementTopFixture(t, 2))
	oldUID := uint16(10)
	newUID := uint16(310)
	newFleetID := releaseMeasurementTestHash(880_001)
	newHotkey := releaseMeasurementTestHash(880_002)
	for bindingIndex := range artifact.Bindings {
		binding := &artifact.Bindings[bindingIndex]
		if binding.NoID != 1 || !binding.Active || binding.LiveUID != oldUID {
			continue
		}
		binding.FleetID = releaseHex32(newFleetID)
		binding.Hotkey = releaseHex32(newHotkey)
		binding.Generation = 2
		binding.RecordUID = newUID
		binding.LiveUID = newUID
	}
	for recordIndex := range artifact.HeadEMA {
		if artifact.HeadEMA[recordIndex].Key.UID == oldUID {
			artifact.HeadEMA[recordIndex].Key = FleetScoreKey{FleetID: newFleetID, Hotkey: newHotkey, Generation: 2, UID: newUID}
		}
	}
	if _, err := VerifyReleaseMeasurementArtifact(artifact); err == nil {
		t.Fatal("new fleet generation inherited prefixes signed for its predecessor")
	}
}

// TestReleaseMeasurementRequiresSignedAttemptCuts covers missing and
// cross-operator cut identities before any derived score is accepted.
func TestReleaseMeasurementRequiresSignedAttemptCuts(t *testing.T) {
	missing := cloneReleaseMeasurementArtifact(t, releaseMeasurementTopFixture(t, 2))
	missing.Inputs[0].Stats.AttemptCut = nil
	if _, err := VerifyReleaseMeasurementArtifact(missing); err == nil {
		t.Fatal("release measurement accepted raw statistics without a signed attempt cut")
	}

	wrongOperator := cloneReleaseMeasurementArtifact(t, releaseMeasurementTopFixture(t, 2))
	wrongOperator.Inputs[0].Stats.AttemptCut.Identity.NoID = 2
	if _, err := VerifyReleaseMeasurementArtifact(wrongOperator); err == nil {
		t.Fatal("release measurement accepted an attempt cut from another operator")
	}
}

// TestReleaseMeasurementRejectsMalformedQualityInputs covers adjacent counter,
// histogram and integer-overflow attacks on pool-quality reconstruction.
func TestReleaseMeasurementRejectsMalformedQualityInputs(t *testing.T) {
	cases := []func(*ReleaseMeasurementArtifact){
		func(artifact *ReleaseMeasurementArtifact) {
			artifact.Inputs[0].Stats.Providers[0].Confirmations = 1
		},
		func(artifact *ReleaseMeasurementArtifact) {
			artifact.Inputs[0].Stats.Providers[0].LatencyBuckets = artifact.Inputs[0].Stats.Providers[0].LatencyBuckets[:statsLatencyBuckets-1]
		},
		func(artifact *ReleaseMeasurementArtifact) {
			artifact.Inputs[0].Stats.Config.AlphaDenominator = ^uint64(0)
		},
	}
	for index, mutate := range cases {
		artifact := cloneReleaseMeasurementArtifact(t, releaseMeasurementTopFixture(t, 2))
		mutate(artifact)
		if _, err := VerifyReleaseMeasurementArtifact(artifact); err == nil {
			t.Fatalf("malformed quality input %d was accepted", index)
		}
	}
}

// TestReleaseMeasurementRejectsFabricatedDepositAudits covers status, lag,
// formula, cap, commitment, deadline and canonical-decimal equivocation.
func TestReleaseMeasurementRejectsFabricatedDepositAudits(t *testing.T) {
	mutations := []func(*DepositAudit, *ReleaseMeasurementArtifact){
		func(audit *DepositAudit, _ *ReleaseMeasurementArtifact) {
			audit.Status, audit.Compliant, audit.Disposition = DepositAuditMismatch, false, "zero_pool_weight"
			audit.Error = "dishonest relabel"
		},
		func(audit *DepositAudit, _ *ReleaseMeasurementArtifact) { audit.ObservedDepositRao = "0" },
		func(audit *DepositAudit, _ *ReleaseMeasurementArtifact) { audit.SourceEpoch++ },
		func(audit *DepositAudit, _ *ReleaseMeasurementArtifact) { audit.RateNumeratorRaoPerGiB++ },
		func(audit *DepositAudit, artifact *ReleaseMeasurementArtifact) {
			aboveCap := new(big.Int).Add(new(big.Int).SetUint64(artifact.Policy.Deposit.EpochCapRaoPerOperator), big.NewInt(1)).String()
			audit.RequiredDepositRao, audit.ObservedDepositRao = aboveCap, aboveCap
		},
		func(audit *DepositAudit, _ *ReleaseMeasurementArtifact) {
			audit.ArtifactHash = "sha256:" + fmt.Sprintf("%064x", 9)
		},
		func(audit *DepositAudit, _ *ReleaseMeasurementArtifact) {
			audit.Status, audit.Compliant, audit.Disposition = DepositAuditUnavailablePending, false, "zero_pool_weight"
			audit.Error, audit.ArtifactDeadlineBlock = "missing", audit.ObservedAtBlock-1
		},
		func(audit *DepositAudit, _ *ReleaseMeasurementArtifact) { audit.ObservedDepositRao = "+1000000" },
		func(audit *DepositAudit, _ *ReleaseMeasurementArtifact) { audit.ConvictionBeforeRao = "00" },
	}
	for index, mutate := range mutations {
		artifact := cloneReleaseMeasurementArtifact(t, releaseMeasurementTopFixture(t, 2))
		mutate(&artifact.DepositAudits[0], artifact)
		if _, err := VerifyReleaseMeasurementArtifact(artifact); err == nil {
			t.Fatalf("fabricated deposit audit mutation %d was accepted", index)
		}
	}
}

// TestReleaseMeasurementRepresentsDishonestDepositAndRecovery proves that a
// mismatched operator stays visible but receives zero, then regains positive
// pool weight only after a later compliant audit in consecutive lineage.
func TestReleaseMeasurementRepresentsDishonestDepositAndRecovery(t *testing.T) {
	dishonest := releaseMeasurementTopFixture(t, 2)
	dishonest.DepositAudits[1].Status = DepositAuditMismatch
	dishonest.DepositAudits[1].Compliant = false
	dishonest.DepositAudits[1].Disposition = "zero_pool_weight"
	dishonest.DepositAudits[1].ObservedDepositRao = "0"
	dishonest.DepositAudits[1].Error = "observed deposit does not equal the signed-usage requirement"
	dishonestBytes, dishonestHash, dishonestVerified, err := SealReleaseMeasurementArtifact(dishonest)
	if err != nil {
		t.Fatal(err)
	}
	if dishonestVerified.Pools[1].Eligible || dishonestVerified.Pools[1].Score.Sign() != 0 {
		t.Fatal("dishonest operator received a positive pool decision")
	}
	for _, uid := range dishonestVerified.UIDs {
		if uid == 2 {
			t.Fatal("dishonest operator pool UID remained in the final vector")
		}
	}

	recovery := cloneReleaseMeasurementArtifact(t, releaseMeasurementTopFixture(t, 2))
	recovery.PreviousArtifactHash = dishonestHash
	recovery.SubnetEpoch = dishonest.SubnetEpoch + 1
	recovery.NativeSnapshotBlock = 110
	recovery.NativeSnapshotHash = releaseHex32(releaseMeasurementTestHash(20))
	recovery.EVMSnapshotBlock = 108
	recovery.EVMSnapshotHash = releaseHex32(releaseMeasurementTestHash(21))
	for auditIndex := range recovery.DepositAudits {
		recovery.DepositAudits[auditIndex].ObservedAtBlock = recovery.EVMSnapshotBlock
		recovery.DepositAudits[auditIndex].ArtifactDeadlineBlock = 110
	}
	// A retry in the same settlement epoch must retain the exact signed attempt
	// boundary. The later native/EVM decision snapshot may observe the corrected
	// deposit, but it cannot rewrite the already-cut attempt prefix.
	attachReleaseMeasurementAttemptCuts(t, recovery)
	priorByKey := map[string]RationalJSON{}
	for _, record := range dishonest.HeadEMA {
		priorByKey[record.Key.String()] = record.Next
	}
	alpha := new(big.Rat).SetFrac(new(big.Int).SetUint64(recovery.Policy.Steering.HeadScoreEMA.Numerator), new(big.Int).SetUint64(recovery.Policy.Steering.HeadScoreEMA.Denominator))
	oneMinus := new(big.Rat).Sub(big.NewRat(1, 1), alpha)
	for index := range recovery.HeadEMA {
		record := &recovery.HeadEMA[index]
		record.HasPrior = true
		record.Prior = priorByKey[record.Key.String()]
		raw, decodeErr := decodeRationalJSON(record.Raw)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		prior, decodeErr := decodeRationalJSON(record.Prior)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		next := new(big.Rat).Add(new(big.Rat).Mul(alpha, raw), new(big.Rat).Mul(oneMinus, prior))
		record.Next, err = encodeRationalJSON(next)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, _, recovered, err := SealReleaseMeasurementArtifact(recovery)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyReleaseMeasurementLineage(dishonestBytes, recovery); err != nil {
		t.Fatal(err)
	}
	if !recovered.Pools[1].Eligible || recovered.Pools[1].Score.Sign() <= 0 {
		t.Fatal("corrected operator did not recover positive pool eligibility")
	}
	foundPool := false
	for _, uid := range recovered.UIDs {
		foundPool = foundPool || uid == 2
	}
	if !foundPool {
		t.Fatal("corrected operator pool UID was absent from the recovered vector")
	}
}

// TestReleaseMeasurementLineageRejectsResetEMA proves that a validator cannot
// erase prior score state and reseed the same fleet in a consecutive cycle.
func TestReleaseMeasurementLineageRejectsResetEMA(t *testing.T) {
	previous := releaseMeasurementTopFixture(t, 2)
	previousBytes, previousHash, _, err := SealReleaseMeasurementArtifact(previous)
	if err != nil {
		t.Fatal(err)
	}
	current := cloneReleaseMeasurementArtifact(t, previous)
	current.SubnetEpoch++
	current.PreviousArtifactHash = previousHash
	if err := VerifyReleaseMeasurementLineage(previousBytes, current); err == nil {
		t.Fatal("consecutive measurement lineage accepted a reset head EMA")
	}
}

// A failed transaction can be rebuilt before the native epoch advances. The
// retry recomputes from the same pre-epoch EMA base; treating the failed
// artifact's output as prior state would apply alpha twice.
func TestReleaseMeasurementLineageSameEpochRetainsPreEpochBase(t *testing.T) {
	previous := releaseMeasurementTopFixture(t, 2)
	previousBytes, previousHash, _, err := SealReleaseMeasurementArtifact(previous)
	if err != nil {
		t.Fatal(err)
	}
	retry := cloneReleaseMeasurementArtifact(t, previous)
	retry.PreviousArtifactHash = previousHash
	if err := VerifyReleaseMeasurementLineage(previousBytes, retry); err != nil {
		t.Fatalf("same-epoch retry from the same EMA base: %v", err)
	}
	retry.HeadEMA[0].HasPrior = true
	retry.HeadEMA[0].Prior = previous.HeadEMA[0].Next
	if err := VerifyReleaseMeasurementLineage(previousBytes, retry); err == nil {
		t.Fatal("same-epoch retry double-folded the failed artifact output")
	}
}
