// Production controls join genuine provider transcripts, independently signed
// approvals, exact native storage, real CRv4 encryption/signing and hotkey seals.
// No test supplies a pre-approved runtime, stake, proof or submission verdict.
package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Every instance owns mutable RPC response bytes and synthetic key material.
type ownerRecycleProductionTestFixture struct {
	test           *testing.T
	operator       *recycleOperatorFixture
	cfg            *ReleaseConfig
	hotkey         *crv4.Keypair
	metadata       *types.Metadata
	validatorUids  []uint16
	permitKVs      map[uint16]bool
	stakeKVs       map[uint16]uint64
	storageNameKVs map[string]string
	epoch          uint64
	head           uint64
	extrinsicsKVs  map[uint64][]string
}

// The complete reviewed producer interface is retained. Only missing owner
// extension fields are added using distinct portable ids and exact wire shapes.
func ownerRecycleProductionTestMetadata(t *testing.T) (*types.Metadata, string, string) {
	t.Helper()
	metadata, _ := provisionalValidatorMetadataTest(t, "../crv4/runtime-profile-v1.scale.gz.base64", crv4.ReviewedRuntimeMetadataHash)
	_, _, owners := recycleAdmissionTestMetadata(t, nil)
	offset := uint64(10000)
	shift := func(id *types.Si1LookupTypeID) { *id = types.NewSi1LookupTypeIDFromUInt(uint64(id.Int64()) + offset) }
	for _, portable := range owners.AsMetadataV14.Lookup.Types {
		shift(&portable.ID)
		definition := &portable.Type.Def
		switch {
		case definition.IsArray:
			shift(&definition.Array.Type)
		case definition.IsSequence:
			shift(&definition.Sequence.Type)
		case definition.IsComposite:
			for index := range definition.Composite.Fields {
				shift(&definition.Composite.Fields[index].Type)
			}
		case definition.IsTuple:
			for index := range definition.Tuple {
				shift(&definition.Tuple[index])
			}
		}
		metadata.AsMetadataV14.Lookup.Types = append(metadata.AsMetadataV14.Lookup.Types, portable)
	}
	for index := range metadata.AsMetadataV14.Pallets {
		pallet := &metadata.AsMetadataV14.Pallets[index]
		if pallet.Name != crv4.PalletName {
			continue
		}
		namesKVs := map[string]bool{}
		for _, entry := range pallet.Storage.Items {
			namesKVs[string(entry.Name)] = true
		}
		for _, entry := range owners.AsMetadataV14.Pallets[0].Storage.Items {
			if namesKVs[string(entry.Name)] {
				continue
			}
			shift(&entry.Type.AsMap.Key)
			shift(&entry.Type.AsMap.Value)
			pallet.Storage.Items = append(pallet.Storage.Items, entry)
		}
	}
	encoded, err := codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	decoded, digest, err := crv4.DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ownerRecycleProductionStorageProfile(decoded); err != nil {
		t.Fatal(err)
	}
	return decoded, encoded, digest
}

// Select the real validator hotkey before any M8 artifact is sealed. The
// production approval is independently signed only after all inputs are fixed.
func newOwnerRecycleProductionTestFixture(t *testing.T) *ownerRecycleProductionTestFixture {
	return newOwnerRecycleProductionTestFixtureWithInputs(t, newRecycleOperatorFixtureWithHotkey, nil)
}

// Continuation fixtures supply independently selected inputs and finite disk
// capacities before the complete production configuration is signed.
func newOwnerRecycleProductionTestFixtureWithInputs(t *testing.T, create func(*testing.T, [32]byte) *recycleOperatorFixture, setup func(*ownerRecycleProductionTestFixture)) *ownerRecycleProductionTestFixture {
	t.Helper()
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x6a, 0x41})
	if err != nil {
		t.Fatal(err)
	}
	operator := create(t, hotkey.PublicKey())
	measurement := operator.measurement
	admission, artifact := measurement.admission, measurement.provider.artifact
	metadata, metadataHex, metadataHash := ownerRecycleProductionTestMetadata(t)
	cfg := admission.cfg
	cfg.SchemaVersion = ReleaseMainnetProductionSchemaVersion
	if err := os.Chmod(cfg.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeMetadataHash = metadataHash
	cfg.EvidenceV2.Bounds.MaxControlBytes = 1024 * 1024
	cfg.EvidenceV2.Bounds.MaxArtifactBytes = 1024 * 1024
	cfg.EvidenceV2.Bounds.MaxOperators = uint64(len(artifact.Inputs))
	admission.metadata = metadataHex
	admission.approval.Schema = ownerRecycleProductionApprovalSchema
	admission.approval.Proposal.Runtime.MetadataHash, _ = parseHash32("synthetic metadata", metadataHash)
	admission.approval.ValidFromNativeBlock = 100
	admission.approval.ValidThroughNativeBlock = 200
	self := &ownerRecycleProductionTestFixture{test: t, operator: operator, cfg: cfg, hotkey: hotkey, metadata: metadata,
		validatorUids: []uint16{artifact.SelfUID, 6}, permitKVs: map[uint16]bool{}, stakeKVs: map[uint16]uint64{}, storageNameKVs: map[string]string{}, epoch: artifact.SubnetEpoch, head: 100}
	registrations := measurement.authority.observation.Snapshot.Registrations
	hotkeys := [][32]byte{hotkey.PublicKey(), registrations[6].Hotkey}
	slices.SortFunc(hotkeys, func(a, b [32]byte) int { return bytes.Compare(a[:], b[:]) })
	admission.approval.Production = &OwnerRecycleProductionApproval{Schema: ownerRecycleProductionScope,
		RuntimeCapability: crv4.ValidatorProducerRuntimeProfile, ValidatorHotkeys: hotkeys, MaximumLastUpdateAge: 50,
		ValidThroughNativeEpoch: artifact.SubnetEpoch + 10, ActivationNativeHash: [32]byte(admission.finalized)}
	netuid := binary.LittleEndian.AppendUint16(nil, cfg.Netuid)
	put := func(pallet, name string, value []byte, args ...[]byte) {
		key, err := types.CreateStorageKey(metadata, pallet, name, args...)
		if err != nil {
			t.Fatal(err)
		}
		self.storageNameKVs[name] = key.Hex()
		if value == nil {
			admission.storage[key.Hex()] = nil
		} else {
			admission.storage[key.Hex()] = codec.HexEncodeToString(value)
		}
	}
	for index, uid := range self.validatorUids {
		self.permitKVs[uid], self.stakeKVs[uid] = true, 100
		hotkey := registrations[uid].Hotkey
		coldkey := [32]byte{0xd0, byte(index + 1)}
		put(crv4.PalletName, "Owner", coldkey[:], hotkey[:])
		put(crv4.PalletName, "TotalHotkeyAlpha", binary.LittleEndian.AppendUint64(nil, 100), hotkey[:], netuid)
	}
	put(crv4.PalletName, "StakeThreshold", binary.LittleEndian.AppendUint64(nil, 10))
	permits := releaseNativeValidatorTestCompact(t, uint64(len(registrations)))
	updates := releaseNativeValidatorTestCompact(t, uint64(len(registrations)))
	for _, registration := range registrations {
		permit := byte(0)
		if self.permitKVs[registration.Uid] {
			permit = 1
		}
		permits = append(permits, permit)
		updates = binary.LittleEndian.AppendUint64(updates, 90)
	}
	put(crv4.PalletName, "ValidatorPermit", permits, netuid)
	put(crv4.PalletName, "LastUpdate", updates, netuid)
	put(crv4.PalletName, "PendingServerEmission", make([]byte, 8), netuid)
	for _, field := range []struct {
		name  string
		value uint64
	}{
		{name: "LastEpochBlock", value: 98}, {name: "PendingEpochAt"}, {name: "BlocksSinceLastStep", value: 2}, {name: "RevealPeriodEpochs", value: 1},
	} {
		put(crv4.PalletName, field.name, binary.LittleEndian.AppendUint64(nil, field.value), netuid)
	}
	put(crv4.PalletName, "Tempo", binary.LittleEndian.AppendUint16(nil, 12), netuid)
	put(crv4.PalletName, "CommitRevealWeightsEnabled", []byte{1}, netuid)
	put(crv4.PalletName, "CommitRevealWeightsVersion", binary.LittleEndian.AppendUint16(nil, 4))
	put("Commitments", "MaxSpace", binary.LittleEndian.AppendUint32(nil, 1000))
	public := hotkey.PublicKey()
	put("Commitments", "UsedSpaceOf", nil, netuid, public[:])
	base := admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	original := base.callContext
	base.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		assign := func(value any) error {
			encoded, err := json.Marshal(value)
			if err != nil {
				return err
			}
			return json.Unmarshal(encoded, target)
		}
		if method == "system_accountNextIndex" {
			return assign(uint32(0))
		}
		if method == "state_getRuntimeVersion" {
			return assign(map[string]any{"specName": admission.version.SpecName, "specVersion": admission.version.SpecVersion,
				"transactionVersion": 1, "stateVersion": 1, "apis": []any{[]any{"0x8375104b299b74c5", 2}}})
		}
		if method == "chain_getFinalizedHead" {
			return assign(self.block(self.head))
		}
		if method == "chain_getBlockHash" && len(args) == 1 && args[0] != uint64(0) {
			number, ok := args[0].(uint64)
			if !ok || number < 100 || number > self.head {
				return errors.New("synthetic production height escaped its bounded chain")
			}
			return assign(self.block(number))
		}
		if method == "chain_getHeader" {
			for number := uint64(100); number <= self.head; number++ {
				if len(args) == 1 && args[0] == self.block(number).Hex() {
					return assign(releaseReceiptTestHeaderWire(self.header(number)))
				}
			}
			return errors.New("synthetic production header escaped its bounded chain")
		}
		if method == "state_call" {
			if len(args) != 3 || args[0] != "SubnetInfoRuntimeApi_get_selective_metagraph" {
				return errors.New("unexpected synthetic production runtime API")
			}
			data := append([]byte{1}, releaseNativeValidatorTestCompact(t, uint64(cfg.Netuid))...)
			for index := 1; index <= 76; index++ {
				if index != 30 && index != 52 && index != 57 && index != 69 {
					data = append(data, 0)
					continue
				}
				data = append(data, 1)
				data = append(data, releaseNativeValidatorTestCompact(t, uint64(len(registrations)))...)
				for _, registration := range registrations {
					switch index {
					case 52:
						data = append(data, registration.Hotkey[:]...)
					case 57:
						permit := byte(0)
						if self.permitKVs[registration.Uid] {
							permit = 1
						}
						data = append(data, permit)
					case 69:
						data = append(data, releaseNativeValidatorTestCompact(t, self.stakeKVs[registration.Uid])...)
					}
				}
			}
			return assign(codec.HexEncodeToString(data))
		}
		if method == "state_getStorage" && len(args) == 2 {
			key := args[0].(string)
			if key == admission.keys["epoch"] && args[1] != admission.finalized.Hex() {
				return assign(codec.HexEncodeToString(binary.LittleEndian.AppendUint64(nil, self.epoch)))
			}
		}
		// The selected synthetic state is immutable except the separately
		// supplied later epoch counter. Metadata and all other rows are shared.
		if (method == "state_getStorage" || method == "state_getStorageHash") && len(args) == 2 {
			args = append([]any(nil), args...)
			args[1] = admission.finalized.Hex()
		}
		if method == "state_getMetadata" && len(args) == 1 {
			args = []any{admission.finalized.Hex()}
		}
		return original(ctx, target, method, args...)
	}
	if setup != nil {
		setup(self)
	}
	admission.approval.ConfigHash, _ = OwnerRecycleConfigHash(cfg)
	admission.sign(t)
	if err := loadOwnerRecycleProductionConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionRuntimeHistory(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := RetainOwnerRecycleApproval(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	return self
}

// Stable exact block identities let a later finality witness advance without
// changing the original approval, state or provider decision.
func (self *ownerRecycleProductionTestFixture) block(number uint64) types.Hash {
	_, hash := self.receiptBlock(number)
	return hash
}

// Empty canonical bodies exist before a later test installs a real receipt.
func (self *ownerRecycleProductionTestFixture) header(number uint64) types.Header {
	header, _ := self.receiptBlock(number)
	return header
}

// Hashes are fixed before approvals and artifacts select their native blocks.
func (self *ownerRecycleProductionTestFixture) receiptBlock(number uint64) (types.Header, types.Hash) {
	if number < 100 || number > 1000 {
		self.test.Fatal("synthetic production header height is outside its fixture")
	}
	parent := types.Hash{2}
	var header types.Header
	for current := uint64(100); current <= number; current++ {
		header, parent = releaseReceiptTestHeader(self.test, parent, current, self.extrinsicsKVs[current]...)
	}
	return header, parent
}

// Fresh proof scratch is supplied to every genuine replay, as in production.
func (self *ownerRecycleProductionTestFixture) stage(t *testing.T) (*ownerRecycleProductionStage, *VerifiedReleaseMeasurement) {
	t.Helper()
	measurement := self.operator.measurement
	_, provider, err := DecodeReleaseMeasurementArtifactV2(t.Context(), measurement.encoded, measurement.provider.options(t))
	if err != nil {
		t.Fatal(err)
	}
	stage, err := prepareOwnerRecycleProductionDecision(t.Context(), self.cfg, measurement.admission.chain, self.operator.chain,
		measurement.encoded, measurement.provider.artifact, provider.Decision, measurement.provider.options(t))
	if err != nil {
		t.Fatal(err)
	}
	return stage, provider.Decision
}

// This calls the real cryptographic preparation path and stores its exact
// signed atomic source/weights batch. The RPC fixture has no send implementation.
func (self *ownerRecycleProductionTestFixture) intent(t *testing.T, stage *ownerRecycleProductionStage, provider *VerifiedReleaseMeasurement) *SteeringIntent {
	t.Helper()
	intent, _ := self.intentAndEnvelope(t, stage, provider)
	return intent
}

// Content addressing retains the original randomized envelope signature;
// signing the same fields again does not reproduce the same envelope bytes.
func (self *ownerRecycleProductionTestFixture) intentAndEnvelope(t *testing.T, stage *ownerRecycleProductionStage, provider *VerifiedReleaseMeasurement) (*SteeringIntent, []byte) {
	t.Helper()
	measurement := self.operator.measurement
	native := measurement.admission.chain
	selected, err := types.NewHashFromHexString(stage.proof.Decision.NativeSnapshotHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := authenticatePinnedNativeRuntimeAtContext(t.Context(), native, self.cfg, selected); err != nil {
		t.Fatal(err)
	}
	row, err := ownerRecycleProductionRowDecision(provider, stage.proof.Row)
	if err != nil {
		t.Fatal(err)
	}
	options := releaseSubmitOptions(self.cfg)
	options.SourceHash = stage.sourceHash
	options.Now = func() time.Time { return time.Unix(2_000_000_000, 0) }
	prepared, err := crv4.PrepareWeightsCRv4ExactAtContext(t.Context(), native, self.hotkey, self.cfg.Netuid, row.UIDs, row.Scores, options, selected)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Validate(); err != nil {
		t.Fatal(err)
	}
	envelope, envelopeHash, _, err := SealReleaseMeasurementEnvelopeV2(t.Context(), measurement.encoded, measurement.provider.artifact.SelfUID, self.hotkey, prepared.ExtrinsicHash, time.Unix(2_000_000_000, 0), measurement.provider.options(t))
	if err != nil {
		t.Fatal(err)
	}
	sidecar, err := sealOwnerRecycleProductionIntent(t.Context(), stage, self.hotkey, prepared, envelopeHash)
	if err != nil {
		t.Fatal(err)
	}
	artifact := measurement.provider.artifact
	scores, _ := rationalJSON(row.Scores)
	intent := &SteeringIntent{ValidatorID: artifact.ValidatorID, Netuid: artifact.Netuid, SubnetEpoch: artifact.SubnetEpoch,
		NativeSnapshotBlock: artifact.NativeSnapshotBlock, NativeSnapshotHash: artifact.NativeSnapshotHash, EVMSnapshotBlock: artifact.EVMSnapshotBlock, EVMSnapshotHash: artifact.EVMSnapshotHash,
		SettlementEpoch: artifact.SettlementEpoch, PolicyHash: artifact.PolicyHash, SelfUID: artifact.SelfUID,
		MeasurementArtifactHash: ReleaseMeasurementContentHash(measurement.encoded), MeasurementEnvelopeHash: envelopeHash, OwnerRecycle: sidecar,
		Prepared: prepared, UIDs: row.UIDs, Scores: scores, MaskedUIDs: provider.MaskedUIDs, EligibleHeadUIDs: headSelectionUIDs(provider.EligibleHead),
		SelectedHeadUIDs: headSelectionUIDs(provider.SelectedHead), RejectedHeadUIDs: headSelectionUIDs(provider.RejectedHead), StaleHeadBindings: provider.StaleBindings, DepositAudits: artifact.DepositAudits}
	for _, head := range provider.EligibleHead {
		encoded, _ := rationalJSON([]*big.Rat{head.Score})
		intent.EligibleHeadScores = append(intent.EligibleHeadScores, encoded[0])
	}
	return intent, envelope
}

// Real first-decision preparation now reaches a signed 10/90 row; signatures
// never assert that final Yuma or native allocation has already happened.
func TestOwnerRecycleProductionPreparesSignedMeasuredSuccessor(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	measurement := fixture.operator.measurement
	verified, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, stage, intent, measurement.encoded, measurement.provider.artifact, provider)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyReleaseMeasurementIntent(intent, measurement.provider.artifact, verified); err != nil {
		t.Fatal(err)
	}
	if slices.Equal(verified.UIDs, provider.UIDs) || intent.Prepared.SourceCommitment.Hash == releaseHex32(releaseNativeSourceHashV2(measurement.encoded)) {
		t.Fatal("production retained the unapproved parent row/source")
	}
	if err := validateOwnerRecyclePreparedAuthorization(fixture.cfg, intent.Prepared); err == nil {
		t.Fatal("signed config alone acquired a durable-intent grant")
	}
	if requireOwnerRecycleProductionFirstIntent(fixture.cfg, nil, intent.SubnetEpoch+1) == nil {
		t.Fatal("an ongoing epoch window allowed first activation after the signed boundary")
	}
	if err := requireOwnerRecycleProductionFirstIntent(fixture.cfg, nil, intent.SubnetEpoch); err != nil {
		t.Fatal(err)
	}
	if err := requireOwnerRecycleProductionFirstIntent(fixture.cfg, intent, intent.SubnetEpoch+1); err != nil {
		t.Fatal(err)
	}
}

// A fresh process re-observes exactly the old decision after finality advances;
// the live-head witness and lost performance caches cannot rewrite its proof.
func TestOwnerRecycleProductionHistoricalReplaySurvivesHeadAndCacheChanges(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	fixture.head = 105
	admission := fixture.operator.measurement.admission
	if err := os.Remove(fixture.cfg.OwnerRecycleApproval.Approval.Path); err != nil {
		t.Fatal(err)
	}
	admission.chain = &crv4.Chain{API: admission.chain.API, GenesisHash: admission.chain.GenesisHash}
	recovered, recoveredProvider := fixture.stage(t)
	if !bytes.Equal(stage.encoded, recovered.encoded) {
		t.Fatal("advancing finality or a cold runtime cache changed immutable decision facts")
	}
	measurement := fixture.operator.measurement
	if _, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, recovered, intent, measurement.encoded, measurement.provider.artifact, recoveredProvider); err != nil {
		t.Fatal(err)
	}
	if ownerRecycleProductionBoundary(fixture.cfg) != nil {
		t.Fatal("valid signed production still selected the old blanket refusal")
	}
}

// Production's independently signed interval permits the next native epoch;
// it keeps the original drained block and does not promote an old observer.
func TestOwnerRecycleProductionContinuesApprovedEpochWindow(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	first, _ := fixture.stage(t)
	measurement := fixture.operator.measurement
	artifact := measurement.provider.artifact
	oldHash := ReleaseMeasurementContentHash(measurement.encoded)
	fixture.head, fixture.epoch = 101, artifact.SubnetEpoch+1
	artifact.SubnetEpoch, artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash = fixture.epoch, 101, fixture.block(101).Hex()
	artifact.PreviousArtifactHash = oldHash
	for index := range artifact.Inputs {
		artifact.Inputs[index].CutNativeBlock, artifact.Inputs[index].CutNativeBlockHash = 101, fixture.block(101).Hex()
	}
	var err error
	measurement.encoded, _, err = SealReleaseMeasurementArtifactV2(t.Context(), artifact, measurement.provider.options(t))
	if err != nil {
		t.Fatal(err)
	}
	next, _ := fixture.stage(t)
	if next.proof.Decision.SubnetEpoch != first.proof.Decision.SubnetEpoch+1 || next.proof.Eligibility.ActivationHash != first.proof.Eligibility.ActivationHash || next.proof.Eligibility.ActivationNativeEpoch != first.proof.Decision.SubnetEpoch {
		t.Fatal("successor lost its original activation and native epoch window")
	}
	approval := fixture.operator.measurement.admission.approval
	approval.Production = nil
	if ownerRecycleDecisionEpochApproved(&approval, next.proof.Decision.SubnetEpoch) {
		t.Fatal("old observer approval gained an ongoing production window")
	}
}

// The original provider bytes, source commitment, final row, native preparation
// and both hotkey signatures must all agree. No one field selects authority.
func TestOwnerRecycleProductionRejectsSidecarAndPreparedSubstitution(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	valid := fixture.intent(t, stage, provider)
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	measurement := fixture.operator.measurement
	for _, fault := range []string{"absent", "schema", "approval", "census", "operators", "row", "validator", "signature", "envelope", "parent-source", "prepared-row"} {
		var candidate SteeringIntent
		if err := json.Unmarshal(raw, &candidate); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "absent":
			candidate.OwnerRecycle = nil
		case "schema":
			candidate.OwnerRecycle.Proof.Schema = ownerRecycleOperatorMeasurementSchema
		case "approval":
			candidate.OwnerRecycle.Proof.Approval[0] ^= 1
		case "census":
			candidate.OwnerRecycle.Proof.Census[0] ^= 1
		case "operators":
			candidate.OwnerRecycle.Proof.OperatorEvidence[0] ^= 1
		case "row":
			candidate.OwnerRecycle.Proof.Row.Scores[0].Numerator = "999"
		case "validator":
			candidate.OwnerRecycle.Hotkey = releaseHex32([32]byte{0x44})
		case "signature":
			candidate.OwnerRecycle.Signature = "0x" + strings.Repeat("00", 64)
		case "envelope":
			candidate.MeasurementEnvelopeHash = ReleaseMeasurementContentHash([]byte("other synthetic envelope"))
		case "parent-source":
			candidate.Prepared.SourceCommitment.Hash = releaseHex32(releaseNativeSourceHashV2(measurement.encoded))
		case "prepared-row":
			candidate.Prepared.Values[0]++
		}
		if _, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, stage, &candidate, measurement.encoded, measurement.provider.artifact, provider); err == nil {
			t.Errorf("%s sidecar substitution acquired authority", fault)
		}
	}
	legacy := *fixture.cfg
	legacy.SchemaVersion, legacy.ownerRecycleProduction, legacy.productionRuntimeHistory = ReleaseValidatorSchemaVersion, nil, nil
	legacy.ProductionRuntimeApprovals = nil
	if _, err := verifyOwnerRecycleProductionIntent(t.Context(), &legacy, nil, valid, measurement.encoded, measurement.provider.artifact, provider); err == nil {
		t.Fatal("sidecar selected production for legacy configuration")
	}
}

// A newly signed complete config is valid authority for itself; it does not
// retroactively authorize a sidecar from the previous complete config.
func TestOwnerRecycleProductionRejectsDifferentApprovedConfig(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	measurement := fixture.operator.measurement
	replacement := *fixture.cfg
	reference := *fixture.cfg.OwnerRecycleApproval
	replacement.OwnerRecycleApproval = &reference
	replacement.StateDir = t.TempDir()
	if err := os.Chmod(replacement.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	replacement.OwnerRecycleApproval.Approval.Path = filepath.Join(replacement.StateDir, "new-approved-config.json")
	approver := *measurement.admission
	approver.cfg = &replacement
	var err error
	approver.approval.ConfigHash, err = OwnerRecycleConfigHash(&replacement)
	if err != nil {
		t.Fatal(err)
	}
	approver.sign(t)
	if err := loadOwnerRecycleProductionConfig(&replacement); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionRuntimeHistory(&replacement); err != nil {
		t.Fatal(err)
	}
	if err := validateOwnerRecycleProductionConfig(&replacement); err != nil {
		t.Fatalf("independent replacement must be valid in its own scope: %v", err)
	}
	if _, err := verifyOwnerRecycleProductionIntent(t.Context(), &replacement, stage, intent, measurement.encoded, measurement.provider.artifact, provider); err == nil || !strings.Contains(err.Error(), "cannot be reinterpreted") {
		t.Fatalf("different independently approved config reinterpreted old authority: %v", err)
	}
	if _, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, stage, intent, measurement.encoded, measurement.provider.artifact, provider); err != nil {
		t.Fatalf("original retained authority was damaged by replacement: %v", err)
	}
}

// Each fault changes a real SCALE row or selective API response. Baseline and
// reset use the same genuine independent observers, never a cached verdict.
func TestOwnerRecycleProductionEligibilityRejectsNativeFaults(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, _ := fixture.stage(t)
	admission := fixture.operator.measurement.admission
	originalStorage := maps.Clone(admission.storage)
	for _, fault := range []string{"stake", "stale", "future", "owner", "pending", "drain-epoch"} {
		admission.storage = maps.Clone(originalStorage)
		fixture.stakeKVs[fixture.validatorUids[0]] = 100
		switch fault {
		case "stake":
			fixture.stakeKVs[fixture.validatorUids[0]] = 9
		case "stale", "future":
			value := uint64(1)
			if fault == "future" {
				value = 101
			}
			updates := releaseNativeValidatorTestCompact(t, uint64(len(stage.authority.observation.Snapshot.Registrations)))
			for range stage.authority.observation.Snapshot.Registrations {
				updates = binary.LittleEndian.AppendUint64(updates, value)
			}
			admission.storage[fixture.storageNameKVs["LastUpdate"]] = codec.HexEncodeToString(updates)
		case "owner":
			for _, uid := range fixture.validatorUids {
				hotkey := stage.authority.observation.Snapshot.Registrations[uid].Hotkey
				key, err := types.CreateStorageKey(fixture.metadata, crv4.PalletName, "Owner", hotkey[:])
				if err != nil {
					t.Fatal(err)
				}
				admission.storage[key.Hex()] = releaseHex32([32]byte{0xd3})
			}
		case "pending":
			admission.storage[fixture.storageNameKVs["PendingServerEmission"]] = codec.HexEncodeToString(binary.LittleEndian.AppendUint64(nil, 1))
		case "drain-epoch":
			admission.storage[admission.keys["epoch"]] = codec.HexEncodeToString(binary.LittleEndian.AppendUint64(nil, fixture.epoch-1))
		}
		if _, err := observeOwnerRecycleProductionEligibility(t.Context(), fixture.cfg, admission.chain, stage.authority); err == nil {
			t.Errorf("%s native failure acquired production eligibility", fault)
		}
	}
	admission.storage = originalStorage
	fixture.stakeKVs[fixture.validatorUids[0]] = 100
	if _, err := observeOwnerRecycleProductionEligibility(t.Context(), fixture.cfg, admission.chain, stage.authority); err != nil {
		t.Fatalf("restored actual state stayed sticky: %v", err)
	}
}

// Exact grants follow the fully replayed provider/sidecar and real encrypted
// prepared validation; changing any durable prepared field revokes that grant.
func TestOwnerRecycleProductionPreparedGrantIsExactAndPrivate(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	measurement := fixture.operator.measurement
	verified, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, stage, intent, measurement.encoded, measurement.provider.artifact, provider)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyReleaseMeasurementIntent(intent, measurement.provider.artifact, verified); err != nil {
		t.Fatal(err)
	}
	if err := measurement.admission.chain.ValidatePreparedSourceWeightsContext(t.Context(), intent.Prepared, verified.UIDs, verified.Scores, releaseSubmitOptions(fixture.cfg)); err != nil {
		t.Fatal(err)
	}
	store := &IntentStore{v2: &releaseIntentV2Owner{runtime: &releaseRuntimeV2{cfg: *fixture.cfg}}}
	if err := store.retainOwnerRecyclePreparedAuthorization(intent); err != nil {
		t.Fatal(err)
	}
	granted, err := store.ownerRecyclePreparedConfig(t.Context(), fixture.cfg, intent.Prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOwnerRecyclePreparedAuthorization(granted, intent.Prepared); err != nil {
		t.Fatal(err)
	}
	if fixture.cfg.ownerRecycleProduction.prepared != nil {
		t.Fatal("transaction grant mutated shared configuration")
	}
	changed := *intent.Prepared
	changed.RevealBlock++
	if validateOwnerRecyclePreparedAuthorization(granted, &changed) == nil {
		t.Fatal("changed prepared lifetime reused a prior grant")
	}
	if reflect.DeepEqual(granted, fixture.cfg) {
		t.Fatal("production did not create a distinct transaction-scoped capability")
	}
}

// Cancellation is forced at the physical last drain read, after successful
// eligibility work, so partial observations never become a retained stage.
func TestOwnerRecycleProductionCancellationPreservesNoPartialAuthority(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, _ := fixture.stage(t)
	admission := fixture.operator.measurement.admission
	client := admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	original := client.callContext
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client.callContext = func(callCtx context.Context, target any, method string, args ...any) error {
		err := original(callCtx, target, method, args...)
		if method == "state_getStorage" && len(args) == 2 && args[0] == fixture.storageNameKVs["PendingServerEmission"] {
			cancel()
		}
		return err
	}
	result, err := observeOwnerRecycleProductionEligibility(ctx, fixture.cfg, admission.chain, stage.authority)
	if !errors.Is(err, context.Canceled) || result != nil {
		t.Fatalf("late cancellation retained a production grant: %v", err)
	}
}

// Wire lengths are checked before allocating a native census. Storage profile
// mutations remain incompatible even if an external signer approves their hash.
func TestOwnerRecycleProductionNativeBoundsAndConsumedShape(t *testing.T) {
	oversized := OwnerRecycleApproval{Schema: ownerRecycleProductionApprovalSchema, Production: &OwnerRecycleProductionApproval{ValidatorHotkeys: make([][32]byte, maximumOwnerRecycleApprovedHotkeys+1)}}
	if _, err := oversized.SigningMessage(); err == nil {
		t.Fatal("external signing admitted an unbounded production validator census")
	}
	metadata, _, _ := ownerRecycleProductionTestMetadata(t)
	for _, raw := range [][]byte{nil, {3}, {5, 0}, {10, 0, 0, 0}, {4, 1}, append([]byte{8}, make([]byte, 8)...)} {
		if _, err := ownerRecycleLastUpdates(raw, 1); err == nil {
			t.Errorf("invalid last-update census %x accepted", raw)
		}
	}
	for index := range metadata.AsMetadataV14.Pallets {
		pallet := &metadata.AsMetadataV14.Pallets[index]
		for fieldIndex := range pallet.Storage.Items {
			entry := &pallet.Storage.Items[fieldIndex]
			if entry.Name == "PendingServerEmission" {
				entry.Fallback = []byte{1, 0, 0, 0, 0, 0, 0, 0}
			}
		}
	}
	if _, err := ownerRecycleProductionStorageProfile(metadata); err == nil {
		t.Fatal("changed pending-emission default retained production capability")
	}
}
