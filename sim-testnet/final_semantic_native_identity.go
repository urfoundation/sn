package main

// This file is the native-chain identity boundary for FINAL evidence. A
// successful dispatch hash is not enough: the exact signed call and the
// operation-specific Subtensor events must agree with the reviewed plan or
// CRv4 write-ahead record.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"

	gsrpcregistry "github.com/centrifuge/go-substrate-rpc-client/v4/registry"
	gsrpcparser "github.com/centrifuge/go-substrate-rpc-client/v4/registry/parser"
	"github.com/centrifuge/go-substrate-rpc-client/v4/scale"
	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	gsrpccodec "github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	gsrpcextrinsic "github.com/centrifuge/go-substrate-rpc-client/v4/types/extrinsic"
	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ss58"
)

const (
	finalNativeCallEvidenceSchema = "urnetwork-final-native-call-v1"

	finalNativeOperationRegistration = "registration"
	finalNativeOperationCommit       = "crv4-commit"
	finalNativeOperationReveal       = "crv4-reveal"
	finalNativeOperationApplication  = "crv4-application"

	// The authenticated runtime-v451/v452/v453 metadata resolves these exact
	// indices. The metadata-driven decoder below also requires the canonical
	// pallet/call name, so a future runtime cannot silently reuse an index.
	finalNativeSubtensorPalletIndex      = byte(7)
	finalNativeRegisterLimitCallIndex    = byte(134)
	finalNativeCommitCallIndex           = byte(113)
	finalNativeCommitMechanismCallIndex  = byte(118)
	finalNativeSignedExtrinsicVersion    = byte(gsrpcextrinsic.Version4 | gsrpcextrinsic.BitSigned)
	finalNativeRegisterLimitCall         = "SubtensorModule.register_limit"
	finalNativeCommitTimelockedCall      = "SubtensorModule.commit_timelocked_weights"
	finalNativeCommitTimelockedMechanism = "SubtensorModule.commit_timelocked_mechanism_weights"
	finalNativeNeuronRegisteredEvent     = "SubtensorModule.NeuronRegistered"
	finalNativeTimelockedCommittedEvent  = "SubtensorModule.TimelockedWeightsCommitted"
	finalNativeWeightsSetEvent           = "SubtensorModule.WeightsSet"
	finalNativeTimelockedRevealedEvent   = "SubtensorModule.TimelockedWeightsRevealed"
)

// Carries the source-derived, self-contained expectation for one native
// mutation. Reveal and application records repeat their exact
// commit lineage so an old weight row cannot be relabeled as a fresh cycle.
// Public replay derives RawCallSHA256 from the exact block bytes after fully
// decoding them with the authenticated historical metadata.
type FinalNativeCallEvidence struct {
	Schema               string `json:"schema"`
	Operation            string `json:"operation"`
	Signer               string `json:"signer_public_key"`
	Nonce                uint32 `json:"nonce"`
	Pallet               string `json:"pallet"`
	Call                 string `json:"call"`
	Netuid               uint16 `json:"netuid"`
	UID                  uint16 `json:"uid"`
	Hotkey               string `json:"hotkey_public_key,omitempty"`
	RegistrationLimitRao uint64 `json:"registration_limit_rao,omitempty"`
	Mecid                *uint8 `json:"mecid,omitempty"`
	CiphertextSHA256     string `json:"ciphertext_sha256,omitempty"`
	CiphertextBlake2     string `json:"ciphertext_blake2_256,omitempty"`
	RevealRound          uint64 `json:"reveal_round,omitempty"`
	CommitRevealVersion  uint16 `json:"commit_reveal_version,omitempty"`
	RawCallSHA256        string `json:"raw_call_sha256"`
	CommitExtrinsicHash  string `json:"commit_extrinsic_hash,omitempty"`
	CommitBlock          uint64 `json:"commit_block,omitempty"`
	RevealBlock          uint64 `json:"reveal_block,omitempty"`
	ApplicationBlock     uint64 `json:"application_block,omitempty"`
}

// Holds the fully decoded signed-v4 envelope and metadata-resolved call.
type decodedFinalNativeExtrinsic struct {
	Version      byte
	Signer       [32]byte
	Signature    [64]byte
	Nonce        uint32
	Tip          uint64
	Immortal     bool
	MetadataMode byte
	CallIndex    gsrpctypes.CallIndex
	CallName     string
	CallFields   gsrpcregistry.DecodedFields
	RawCall      []byte
}

// Flattens a registry field while preserving its variant path.
type finalNativeDecodedLeaf struct {
	path  string
	value any
}

// Decodes either canonical hex or a canonical Bittensor address.
func finalNativeAccountPublicKey(value string) ([32]byte, error) {
	if strings.HasPrefix(value, "0x") {
		raw, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
		if err != nil || len(raw) != 32 || value != strings.ToLower(value) {
			return [32]byte{}, errors.New("native account hex is not canonical")
		}
		var result [32]byte
		copy(result[:], raw)
		return result, nil
	}
	result, prefix, err := ss58.Decode(value)
	if err != nil || prefix != ss58.BittensorPrefix {
		return [32]byte{}, errors.New("native account is not a canonical Bittensor account")
	}
	canonical, err := ss58.Encode(result, prefix)
	if err != nil || canonical != value {
		return [32]byte{}, errors.New("native account SS58 encoding is not canonical")
	}
	return result, nil
}

// Normalizes a public native account to lowercase AccountId32 hex.
func finalNativeAccountHex(value string) (string, error) {
	key, err := finalNativeAccountPublicKey(value)
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(key[:]), nil
}

// Hashes an exact wire object with SHA-256 for source manifests.
func finalNativeHashSHA256(value []byte) string {
	digest := sha256.Sum256(value)
	return "0x" + hex.EncodeToString(digest[:])
}

// Hashes an exact wire object with the chain's BLAKE2b-256 digest.
func finalNativeHashBlake2(value []byte) string {
	digest := blake2b.Sum256(value)
	return "0x" + hex.EncodeToString(digest[:])
}

// Encodes one exact metadata-independent call index and argument sequence.
func finalNativeEncodeCall(index gsrpctypes.CallIndex, args ...any) ([]byte, error) {
	var encoded bytes.Buffer
	encoder := scale.NewEncoder(&encoded)
	if err := encoder.Encode(index); err != nil {
		return nil, err
	}
	for _, arg := range args {
		if err := encoder.Encode(arg); err != nil {
			return nil, err
		}
	}
	return encoded.Bytes(), nil
}

// Derives a registration expectation from approved plan and journal fields.
func finalNativeRegistrationCallEvidence(signer string, nonce uint32, netuid uint16, hotkey string, uid uint16, registrationLimitRao uint64) (FinalNativeCallEvidence, error) {
	signerHex, err := finalNativeAccountHex(signer)
	if err != nil {
		return FinalNativeCallEvidence{}, fmt.Errorf("registration signer: %w", err)
	}
	hotkeyHex, err := finalNativeAccountHex(hotkey)
	if err != nil {
		return FinalNativeCallEvidence{}, fmt.Errorf("registration hotkey: %w", err)
	}
	if netuid == 0 || registrationLimitRao == 0 {
		return FinalNativeCallEvidence{}, errors.New("registration netuid or burn limit is zero")
	}
	hotkeyKey, _ := finalNativeAccountPublicKey(hotkeyHex)
	account := gsrpctypes.AccountID(hotkeyKey)
	rawCall, err := finalNativeEncodeCall(
		gsrpctypes.CallIndex{SectionIndex: finalNativeSubtensorPalletIndex, MethodIndex: finalNativeRegisterLimitCallIndex},
		gsrpctypes.NewU16(netuid), account, gsrpctypes.NewU64(registrationLimitRao),
	)
	if err != nil {
		return FinalNativeCallEvidence{}, err
	}
	return FinalNativeCallEvidence{
		Schema: finalNativeCallEvidenceSchema, Operation: finalNativeOperationRegistration,
		Signer: signerHex, Nonce: nonce, Pallet: crv4.PalletName, Call: "register_limit",
		Netuid: netuid, UID: uid, Hotkey: hotkeyHex, RegistrationLimitRao: registrationLimitRao,
		RawCallSHA256: finalNativeHashSHA256(rawCall),
	}, nil
}

// Derives a commit expectation from the exact prepared signed submission.
func finalNativeCommitCallEvidence(prepared *crv4.PreparedSubmission, uid uint16, commitBlock uint64) (FinalNativeCallEvidence, error) {
	if prepared == nil || commitBlock == 0 {
		return FinalNativeCallEvidence{}, errors.New("prepared commit or commit block is missing")
	}
	extrinsic, err := prepared.Validate()
	if err != nil {
		return FinalNativeCallEvidence{}, fmt.Errorf("prepared commit: %w", err)
	}
	signerHex, err := finalNativeAccountHex(prepared.HotkeyHex)
	if err != nil {
		return FinalNativeCallEvidence{}, fmt.Errorf("commit signer: %w", err)
	}
	ciphertext, err := gsrpccodec.HexDecodeString(prepared.CiphertextHex)
	if err != nil || len(ciphertext) == 0 {
		return FinalNativeCallEvidence{}, errors.New("prepared commit ciphertext is invalid")
	}
	callIndex := finalNativeCommitCallIndex
	callName := crv4.CallCommitTimelocked
	args := []any{gsrpctypes.NewU16(prepared.Netuid)}
	if prepared.Mecid != nil {
		callIndex = finalNativeCommitMechanismCallIndex
		callName = crv4.CallCommitTimelockedMech
		args = append(args, gsrpctypes.NewU8(*prepared.Mecid))
	}
	args = append(args, gsrpctypes.NewBytes(ciphertext), gsrpctypes.NewU64(prepared.RevealRound), gsrpctypes.NewU16(prepared.CommitRevealVersion))
	rawCall, err := finalNativeEncodeCall(gsrpctypes.CallIndex{SectionIndex: finalNativeSubtensorPalletIndex, MethodIndex: callIndex}, args...)
	if err != nil {
		return FinalNativeCallEvidence{}, err
	}
	if !bytes.HasSuffix(extrinsic, rawCall) {
		return FinalNativeCallEvidence{}, errors.New("prepared signed extrinsic does not end in its exact CRv4 call")
	}
	if got := finalNativeHashSHA256(ciphertext); got != strings.ToLower(prepared.CiphertextSHA256) {
		return FinalNativeCallEvidence{}, errors.New("prepared ciphertext hash differs from exact bytes")
	}
	return FinalNativeCallEvidence{
		Schema: finalNativeCallEvidenceSchema, Operation: finalNativeOperationCommit,
		Signer: signerHex, Nonce: prepared.AccountNonce, Pallet: crv4.PalletName, Call: callName,
		Netuid: prepared.Netuid, UID: uid, Mecid: prepared.Mecid,
		CiphertextSHA256: strings.ToLower(prepared.CiphertextSHA256), CiphertextBlake2: finalNativeHashBlake2(ciphertext),
		RevealRound: prepared.RevealRound, CommitRevealVersion: prepared.CommitRevealVersion,
		RawCallSHA256: finalNativeHashSHA256(rawCall), CommitExtrinsicHash: strings.ToLower(prepared.ExtrinsicHash), CommitBlock: commitBlock,
		RevealBlock: prepared.RevealBlock,
	}, nil
}

// Carries a commit identity forward to an automatic reveal or application.
func finalNativeAutomaticCallEvidence(commit FinalNativeCallEvidence, operation string, block uint64) (FinalNativeCallEvidence, error) {
	if err := verifyFinalNativeCallEvidenceShape(commit, finalNativeOperationCommit); err != nil {
		return FinalNativeCallEvidence{}, err
	}
	switch operation {
	case finalNativeOperationReveal:
		if block != commit.RevealBlock {
			return FinalNativeCallEvidence{}, errors.New("automatic reveal block differs from prepared CRv4 lineage")
		}
	case finalNativeOperationApplication:
		if block < commit.RevealBlock {
			return FinalNativeCallEvidence{}, errors.New("automatic application predates prepared CRv4 reveal")
		}
		commit.ApplicationBlock = block
	default:
		return FinalNativeCallEvidence{}, fmt.Errorf("unsupported automatic native operation %q", operation)
	}
	commit.Operation = operation
	return commit, nil
}

// Rejects noncanonical, incomplete, or cross-operation evidence fields.
func verifyFinalNativeCallEvidenceShape(value FinalNativeCallEvidence, operation string) error {
	if value.Schema != finalNativeCallEvidenceSchema || value.Operation != operation || value.Pallet != crv4.PalletName || value.Netuid == 0 {
		return errors.New("native call evidence identity is incomplete")
	}
	if _, err := finalNativeAccountPublicKey(value.Signer); err != nil {
		return fmt.Errorf("native call signer: %w", err)
	}
	if err := requireFinalHex32("native raw call SHA-256", value.RawCallSHA256); err != nil {
		return err
	}
	switch operation {
	case finalNativeOperationRegistration:
		if value.Call != "register_limit" || value.Hotkey == "" || value.RegistrationLimitRao == 0 || value.Mecid != nil || value.CiphertextSHA256 != "" || value.CiphertextBlake2 != "" || value.RevealRound != 0 || value.CommitRevealVersion != 0 || value.CommitExtrinsicHash != "" || value.CommitBlock != 0 || value.RevealBlock != 0 || value.ApplicationBlock != 0 {
			return errors.New("native registration call evidence has invalid fields")
		}
		if _, err := finalNativeAccountPublicKey(value.Hotkey); err != nil {
			return fmt.Errorf("native registration hotkey: %w", err)
		}
		hotkey, _ := finalNativeAccountPublicKey(value.Hotkey)
		raw, err := finalNativeEncodeCall(gsrpctypes.CallIndex{SectionIndex: finalNativeSubtensorPalletIndex, MethodIndex: finalNativeRegisterLimitCallIndex}, gsrpctypes.NewU16(value.Netuid), gsrpctypes.AccountID(hotkey), gsrpctypes.NewU64(value.RegistrationLimitRao))
		if err != nil || finalNativeHashSHA256(raw) != value.RawCallSHA256 {
			return stateMismatchError(err, "native registration raw call hash is not derived from its exact fields")
		}
	case finalNativeOperationCommit, finalNativeOperationReveal, finalNativeOperationApplication:
		if value.Call != crv4.CallCommitTimelocked && value.Call != crv4.CallCommitTimelockedMech {
			return errors.New("native CRv4 call name is invalid")
		}
		if value.Call == crv4.CallCommitTimelocked && value.Mecid != nil || value.Call == crv4.CallCommitTimelockedMech && value.Mecid == nil {
			return errors.New("native CRv4 mechanism fields are inconsistent")
		}
		if requireFinalHex32("native ciphertext SHA-256", value.CiphertextSHA256) != nil || requireFinalHex32("native ciphertext BLAKE2b-256", value.CiphertextBlake2) != nil || requireFinalHex32("native commit extrinsic", value.CommitExtrinsicHash) != nil || value.RevealRound == 0 || value.CommitRevealVersion == 0 || value.CommitBlock == 0 || value.RevealBlock <= value.CommitBlock {
			return errors.New("native CRv4 cryptographic lineage is incomplete")
		}
		if operation == finalNativeOperationApplication && value.ApplicationBlock < value.RevealBlock || operation != finalNativeOperationApplication && value.ApplicationBlock != 0 {
			return errors.New("native CRv4 application lineage is inconsistent")
		}
	default:
		return fmt.Errorf("unsupported native operation %q", operation)
	}
	return nil
}

// Requires all three CRv4 checkpoints to share one exact commitment.
func verifyFinalNativeCRv4Lineage(commit, reveal, application FinalNativeCallEvidence) error {
	if err := verifyFinalNativeCallEvidenceShape(commit, finalNativeOperationCommit); err != nil {
		return fmt.Errorf("native CRv4 commit lineage: %w", err)
	}
	if err := verifyFinalNativeCallEvidenceShape(reveal, finalNativeOperationReveal); err != nil {
		return fmt.Errorf("native CRv4 reveal lineage: %w", err)
	}
	if err := verifyFinalNativeCallEvidenceShape(application, finalNativeOperationApplication); err != nil {
		return fmt.Errorf("native CRv4 application lineage: %w", err)
	}
	normalize := func(value FinalNativeCallEvidence) FinalNativeCallEvidence {
		value.Operation = finalNativeOperationCommit
		value.ApplicationBlock = 0
		return value
	}
	if !finalJSONEqual(normalize(commit), normalize(reveal)) || !finalJSONEqual(normalize(commit), normalize(application)) {
		return errors.New("native CRv4 commit, reveal, and application do not share one exact lineage")
	}
	return nil
}

// Binds a receipt checkpoint and transaction presence to its operation.
func verifyFinalNativeReceiptCall(receipt FinalNativeReceipt, operation string) error {
	if receipt.Call == nil {
		return errors.New("native receipt has no exact call evidence")
	}
	if err := verifyFinalNativeCallEvidenceShape(*receipt.Call, operation); err != nil {
		return err
	}
	switch operation {
	case finalNativeOperationRegistration:
		if receipt.ExtrinsicHash == "" {
			return errors.New("native registration receipt has no extrinsic")
		}
	case finalNativeOperationCommit:
		if !strings.EqualFold(receipt.ExtrinsicHash, receipt.Call.CommitExtrinsicHash) || receipt.Block.Number != receipt.Call.CommitBlock {
			return errors.New("native commit receipt differs from its exact call lineage")
		}
	case finalNativeOperationReveal:
		if receipt.ExtrinsicHash != "" || receipt.Block.Number != receipt.Call.RevealBlock {
			return errors.New("native reveal receipt differs from its exact automatic-call lineage")
		}
	case finalNativeOperationApplication:
		if receipt.ExtrinsicHash != "" || receipt.Block.Number != receipt.Call.ApplicationBlock {
			return errors.New("native application receipt differs from its exact state lineage")
		}
	default:
		return fmt.Errorf("unsupported native receipt operation %q", operation)
	}
	return nil
}

// Binds a registration call to the projected UID, hotkey, and coldkey.
func verifyFinalNativeRegistrationProjection(receipt FinalNativeReceipt, netuid, uid uint16, hotkey, coldkey string) error {
	if err := verifyFinalNativeReceiptCall(receipt, finalNativeOperationRegistration); err != nil {
		return err
	}
	wantHotkey, err := finalNativeAccountHex(hotkey)
	if err != nil {
		return fmt.Errorf("native registration projected hotkey: %w", err)
	}
	wantColdkey, err := finalNativeAccountHex(coldkey)
	if err != nil {
		return fmt.Errorf("native registration projected coldkey: %w", err)
	}
	if receipt.Call.Netuid != netuid || receipt.Call.UID != uid || receipt.Call.Hotkey != wantHotkey || receipt.Call.Signer != wantColdkey {
		return fmt.Errorf("native registration call differs from its projected UID ownership: netuid=%d/%d uid=%d/%d hotkey=%t coldkey=%t", receipt.Call.Netuid, netuid, receipt.Call.UID, uid, receipt.Call.Hotkey == wantHotkey, receipt.Call.Signer == wantColdkey)
	}
	return nil
}

// Rejects byte-identical commitments relabeled as separate cycle writes.
func verifyFinalNativeUniqueCRv4Cycles(applications []FinalNativeCallEvidence) error {
	seen := map[string]string{}
	for index, application := range applications {
		if err := verifyFinalNativeCallEvidenceShape(application, finalNativeOperationApplication); err != nil {
			return fmt.Errorf("native CRv4 application %d: %w", index, err)
		}
		identities := []struct {
			name  string
			value string
		}{
			{name: "commit extrinsic", value: application.CommitExtrinsicHash},
			{name: "raw call", value: application.RawCallSHA256},
			{name: "ciphertext SHA", value: application.CiphertextSHA256},
			{name: "ciphertext BLAKE2", value: application.CiphertextBlake2},
		}
		for _, identity := range identities {
			name, value := identity.name, identity.value
			key := name + "/" + value
			if prior, duplicate := seen[key]; duplicate {
				return fmt.Errorf("native CRv4 application %d reuses %s from application %s", index, name, prior)
			}
			seen[key] = fmt.Sprint(index)
		}
	}
	return nil
}

// Deduplicates legitimate artifact aliases before checking global uniqueness.
func verifyFinalNativeEvidenceCycleUniqueness(evidence *FinalSemanticEvidence) error {
	if evidence == nil {
		return errors.New("native CRv4 evidence is nil")
	}
	byCycle := map[string]FinalNativeCallEvidence{}
	add := func(validatorID, settlementEpoch, subnetEpoch uint64, receipt FinalNativeReceipt) error {
		if receipt.Call == nil {
			return fmt.Errorf("native CRv4 cycle %d/%d/%d has no exact application identity", validatorID, settlementEpoch, subnetEpoch)
		}
		key := fmt.Sprintf("%d/%d/%d", validatorID, settlementEpoch, subnetEpoch)
		if prior, exists := byCycle[key]; exists {
			if !finalJSONEqual(prior, *receipt.Call) {
				return fmt.Errorf("native CRv4 cycle %s has conflicting application identities", key)
			}
			return nil
		}
		byCycle[key] = *receipt.Call
		return nil
	}
	for _, validator := range evidence.Validators {
		for _, cycle := range validator.Cycles {
			if err := add(validator.ValidatorID, cycle.SettlementEpoch, cycle.SubnetEpoch, cycle.Application); err != nil {
				return err
			}
		}
	}
	if evidence.DishonestDeposit != nil {
		for _, decision := range append(append([]FinalDishonestDepositDecision(nil), evidence.DishonestDeposit.Penalties...), evidence.DishonestDeposit.Recoveries...) {
			if err := add(decision.ValidatorID, decision.Cycle.SettlementEpoch, decision.Cycle.SubnetEpoch, decision.Cycle.Application); err != nil {
				return err
			}
		}
	}
	if evidence.FleetLifecycle != nil {
		for _, decision := range evidence.FleetLifecycle.AppliedDecisions {
			receipt := FinalNativeReceipt{Call: &decision.ApplicationCall}
			if err := add(decision.ValidatorID, decision.SettlementEpoch, decision.SubnetEpoch, receipt); err != nil {
				return err
			}
		}
	}
	keys := make([]string, 0, len(byCycle))
	for key := range byCycle {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	applications := make([]FinalNativeCallEvidence, 0, len(keys))
	for _, key := range keys {
		applications = append(applications, byCycle[key])
	}
	return verifyFinalNativeUniqueCRv4Cycles(applications)
}

// Requires the exact fresh row and current UID owner at application.
func verifyFinalNativeApplicationState(got FinalNativeWeightState, expected FinalNativeCallEvidence, application ChainHead, validatorUID uint16, validatorHotkey string, uids, values []uint16) error {
	if err := verifyFinalNativeCallEvidenceShape(expected, finalNativeOperationApplication); err != nil {
		return err
	}
	wantHotkey, err := finalNativeAccountHex(validatorHotkey)
	if err != nil {
		return fmt.Errorf("native application validator hotkey: %w", err)
	}
	if expected.UID != validatorUID || expected.Signer != wantHotkey || expected.ApplicationBlock != application.Number || got.ValidatorUID != validatorUID || got.Block != application || got.ValidatorHotkey != wantHotkey || got.LastUpdate != expected.CommitBlock {
		return errors.New("native application UID, owner, checkpoint, or fresh LastUpdate lineage differs")
	}
	if len(got.UIDs) != len(got.Values) || len(uids) != len(values) || !slices.Equal(got.UIDs, uids) || !slices.Equal(got.Values, values) {
		return errors.New("native applied vector differs from its exact signed vector")
	}
	return nil
}

// Flattens nested registry values into deterministic typed leaves.
func finalNativeDecodedLeaves(value any, path string, output *[]finalNativeDecodedLeaf) error {
	switch typed := value.(type) {
	case gsrpcregistry.DecodedFields:
		for _, field := range typed {
			if field == nil {
				return errors.New("decoded native field is nil")
			}
			child := field.Name
			if path != "" {
				child = path + "." + child
			}
			if err := finalNativeDecodedLeaves(field.Value, child, output); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range typed {
			if err := finalNativeDecodedLeaves(item, path, output); err != nil {
				return err
			}
		}
	case nil:
		return nil
	default:
		*output = append(*output, finalNativeDecodedLeaf{path: path, value: value})
	}
	return nil
}

// Decodes one exact fixed-size byte sequence from registry leaves.
func finalNativeDecodedBytes(value any, size int) ([]byte, error) {
	var leaves []finalNativeDecodedLeaf
	if err := finalNativeDecodedLeaves(value, "", &leaves); err != nil {
		return nil, err
	}
	if len(leaves) != size {
		return nil, fmt.Errorf("decoded native byte sequence has %d leaves, want %d", len(leaves), size)
	}
	result := make([]byte, size)
	for index, leaf := range leaves {
		value, ok := leaf.value.(gsrpctypes.U8)
		if !ok {
			return nil, fmt.Errorf("decoded native byte %d has type %T", index, leaf.value)
		}
		result[index] = byte(value)
	}
	return result, nil
}

// Decodes a registry unsigned integer and enforces its declared width.
func finalNativeUnsigned(value any, bits int) (uint64, error) {
	var leaves []finalNativeDecodedLeaf
	if err := finalNativeDecodedLeaves(value, "", &leaves); err != nil || len(leaves) != 1 {
		return 0, stateMismatchError(err, "decoded native integer has %d leaves, want one", len(leaves))
	}
	var result uint64
	switch typed := leaves[0].value.(type) {
	case gsrpctypes.U8:
		result = uint64(typed)
	case gsrpctypes.U16:
		result = uint64(typed)
	case gsrpctypes.U32:
		result = uint64(typed)
	case gsrpctypes.U64:
		result = uint64(typed)
	case gsrpctypes.UCompact:
		integer := big.Int(typed)
		if integer.Sign() < 0 || !integer.IsUint64() {
			return 0, errors.New("decoded compact native integer exceeds uint64")
		}
		result = integer.Uint64()
	default:
		return 0, fmt.Errorf("decoded native integer has type %T", leaves[0].value)
	}
	if bits < 64 && result >= uint64(1)<<bits {
		return 0, fmt.Errorf("decoded native integer %d exceeds u%d", result, bits)
	}
	return result, nil
}

// Selects one signed-extrinsic decoder field by its metadata name.
func finalNativeExtrinsicField(decoder *gsrpcregistry.ExtrinsicDecoder, name string) (*gsrpcregistry.Field, error) {
	if decoder == nil {
		return nil, errors.New("native extrinsic decoder is nil")
	}
	for _, field := range decoder.Fields {
		if field != nil && field.Name == name {
			return field, nil
		}
	}
	return nil, fmt.Errorf("native extrinsic has no %s decoder", name)
}

// Fully decodes one length-prefixed signed-v4 extrinsic with no trailing bytes.
func finalDecodeNativeExtrinsic(metadata *gsrpctypes.Metadata, raw []byte) (*decodedFinalNativeExtrinsic, error) {
	if metadata == nil || len(raw) == 0 {
		return nil, errors.New("native extrinsic metadata or bytes are missing")
	}
	reader := bytes.NewReader(raw)
	decoder := scale.NewDecoder(reader)
	declared, err := decoder.DecodeUintCompact()
	if err != nil || declared.Sign() < 0 || !declared.IsUint64() {
		return nil, stateMismatchError(err, "decode native extrinsic compact length")
	}
	compactSize := len(raw) - reader.Len()
	canonicalCompact, err := gsrpccodec.Encode(gsrpctypes.NewUCompactFromUInt(declared.Uint64()))
	if err != nil || compactSize != len(canonicalCompact) || !bytes.Equal(raw[:compactSize], canonicalCompact) || declared.Uint64() != uint64(reader.Len()) {
		return nil, stateMismatchError(err, "native extrinsic length prefix is noncanonical or incorrect")
	}
	result := &decodedFinalNativeExtrinsic{}
	if err := decoder.Decode(&result.Version); err != nil || result.Version != finalNativeSignedExtrinsicVersion {
		return nil, stateMismatchError(err, "native extrinsic version %#x, want signed v4", result.Version)
	}
	factory := gsrpcregistry.NewFactory()
	extrinsicDecoder, err := factory.CreateExtrinsicDecoder(metadata)
	if err != nil {
		return nil, fmt.Errorf("initialize native extrinsic decoder: %w", err)
	}
	decodedPrefix := make(map[string]*gsrpcregistry.DecodedField, 3)
	for _, name := range []string{gsrpcregistry.ExtrinsicAddressName, gsrpcregistry.ExtrinsicSignatureName, gsrpcregistry.ExtrinsicExtraName} {
		field, fieldErr := finalNativeExtrinsicField(extrinsicDecoder, name)
		if fieldErr != nil {
			return nil, fieldErr
		}
		decodedPrefix[name], fieldErr = field.Decode(decoder)
		if fieldErr != nil {
			return nil, fmt.Errorf("decode native extrinsic %s: %w", name, fieldErr)
		}
	}
	address, err := finalNativeDecodedBytes(decodedPrefix[gsrpcregistry.ExtrinsicAddressName].Value, 32)
	if err != nil {
		return nil, fmt.Errorf("decode native extrinsic AccountId32: %w", err)
	}
	copy(result.Signer[:], address)
	var signatureLeaves []finalNativeDecodedLeaf
	if err := finalNativeDecodedLeaves(decodedPrefix[gsrpcregistry.ExtrinsicSignatureName].Value, "", &signatureLeaves); err != nil || len(signatureLeaves) != 64 || !strings.Contains(signatureLeaves[0].path, "sr25519::Signature") {
		return nil, stateMismatchError(err, "native extrinsic signature is not exact sr25519")
	}
	signature, err := finalNativeDecodedBytes(decodedPrefix[gsrpcregistry.ExtrinsicSignatureName].Value, 64)
	if err != nil {
		return nil, err
	}
	copy(result.Signature[:], signature)
	var extra []finalNativeDecodedLeaf
	if err := finalNativeDecodedLeaves(decodedPrefix[gsrpcregistry.ExtrinsicExtraName].Value, "", &extra); err != nil || len(extra) != 4 {
		return nil, stateMismatchError(err, "native signed extensions have %d encoded leaves, want four", len(extra))
	}
	era, eraOK := extra[0].value.(uint8)
	if !eraOK || era != 0 || !strings.Contains(extra[0].path, "Era") {
		return nil, errors.New("native signed extension era is not immortal")
	}
	nonce, err := finalNativeUnsigned(extra[1].value, 32)
	if err != nil || !strings.Contains(extra[1].path, "Nonce") {
		return nil, stateMismatchError(err, "native signed extension nonce is invalid")
	}
	tip, err := finalNativeUnsigned(extra[2].value, 64)
	if err != nil || tip != 0 {
		return nil, stateMismatchError(err, "native signed extension tip=%d, want zero", tip)
	}
	metadataMode, metadataModeOK := extra[3].value.(uint8)
	if !metadataModeOK || metadataMode != 0 || !strings.Contains(extra[3].path, "Mode") {
		return nil, errors.New("native metadata-hash signed extension is not disabled mode")
	}
	result.Nonce, result.Tip, result.Immortal, result.MetadataMode = uint32(nonce), tip, true, metadataMode
	callOffset := len(raw) - reader.Len()
	if err := decoder.Decode(&result.CallIndex); err != nil {
		return nil, fmt.Errorf("decode native call index: %w", err)
	}
	callRegistry, err := gsrpcregistry.NewFactory().CreateCallRegistry(metadata)
	if err != nil {
		return nil, fmt.Errorf("initialize native call decoder: %w", err)
	}
	callDecoder, ok := callRegistry[result.CallIndex]
	if !ok || callDecoder == nil {
		return nil, fmt.Errorf("native call index %v is absent from authenticated metadata", result.CallIndex)
	}
	result.CallFields, err = callDecoder.Decode(decoder)
	if err != nil {
		return nil, fmt.Errorf("decode native call %s: %w", callDecoder.Name, err)
	}
	if reader.Len() != 0 {
		return nil, fmt.Errorf("native extrinsic has %d trailing bytes", reader.Len())
	}
	result.CallName = callDecoder.Name
	result.RawCall = append([]byte(nil), raw[callOffset:]...)
	return result, nil
}

// Compares the decoded signer, extensions, call, and arguments to source truth.
func verifyFinalNativeExtrinsicIdentity(decoded *decodedFinalNativeExtrinsic, expected FinalNativeCallEvidence) error {
	if decoded == nil {
		return errors.New("decoded native extrinsic is missing")
	}
	if err := verifyFinalNativeCallEvidenceShape(expected, expected.Operation); err != nil {
		return err
	}
	signer, _ := finalNativeAccountPublicKey(expected.Signer)
	if decoded.Version != finalNativeSignedExtrinsicVersion || decoded.Signer != signer || !decoded.Immortal || decoded.Nonce != expected.Nonce || decoded.Tip != 0 || decoded.MetadataMode != 0 || decoded.CallName != expected.Pallet+"."+expected.Call || finalNativeHashSHA256(decoded.RawCall) != expected.RawCallSHA256 {
		return errors.New("native signed extrinsic identity or extensions differ from source evidence")
	}
	switch expected.Operation {
	case finalNativeOperationRegistration:
		if decoded.CallIndex != (gsrpctypes.CallIndex{SectionIndex: finalNativeSubtensorPalletIndex, MethodIndex: finalNativeRegisterLimitCallIndex}) || len(decoded.CallFields) != 3 {
			return errors.New("native registration call index or argument count is invalid")
		}
		netuid, netuidErr := finalNativeUnsigned(decoded.CallFields[0].Value, 16)
		hotkey, hotkeyErr := finalNativeDecodedBytes(decoded.CallFields[1].Value, 32)
		limit, limitErr := finalNativeUnsigned(decoded.CallFields[2].Value, 64)
		expectedHotkey, _ := finalNativeAccountPublicKey(expected.Hotkey)
		if netuidErr != nil || hotkeyErr != nil || limitErr != nil || uint16(netuid) != expected.Netuid || !bytes.Equal(hotkey, expectedHotkey[:]) || limit != expected.RegistrationLimitRao {
			return errors.New("native registration call arguments differ from source evidence")
		}
	case finalNativeOperationCommit:
		wantIndex := gsrpctypes.CallIndex{SectionIndex: finalNativeSubtensorPalletIndex, MethodIndex: finalNativeCommitCallIndex}
		wantArgs := 4
		if expected.Mecid != nil {
			wantIndex.MethodIndex = finalNativeCommitMechanismCallIndex
			wantArgs = 5
		}
		if decoded.CallIndex != wantIndex || len(decoded.CallFields) != wantArgs {
			return errors.New("native CRv4 call index or argument count is invalid")
		}
		field := 0
		netuid, netuidErr := finalNativeUnsigned(decoded.CallFields[field].Value, 16)
		field++
		if netuidErr != nil || uint16(netuid) != expected.Netuid {
			return errors.New("native CRv4 netuid differs from source evidence")
		}
		if expected.Mecid != nil {
			mecid, mecidErr := finalNativeUnsigned(decoded.CallFields[field].Value, 8)
			field++
			if mecidErr != nil || uint8(mecid) != *expected.Mecid {
				return errors.New("native CRv4 mechanism differs from source evidence")
			}
		}
		ciphertext, ciphertextErr := finalNativeDecodedByteSlice(decoded.CallFields[field].Value)
		field++
		revealRound, revealErr := finalNativeUnsigned(decoded.CallFields[field].Value, 64)
		field++
		version, versionErr := finalNativeUnsigned(decoded.CallFields[field].Value, 16)
		if ciphertextErr != nil || revealErr != nil || versionErr != nil || finalNativeHashSHA256(ciphertext) != expected.CiphertextSHA256 || finalNativeHashBlake2(ciphertext) != expected.CiphertextBlake2 || revealRound != expected.RevealRound || uint16(version) != expected.CommitRevealVersion {
			return errors.New("native CRv4 call arguments differ from source evidence")
		}
	default:
		return fmt.Errorf("native operation %q has no signed extrinsic", expected.Operation)
	}
	return nil
}

// Decodes a variable-length byte vector from metadata registry leaves.
func finalNativeDecodedByteSlice(value any) ([]byte, error) {
	var leaves []finalNativeDecodedLeaf
	if err := finalNativeDecodedLeaves(value, "", &leaves); err != nil {
		return nil, err
	}
	if len(leaves) == 0 {
		return nil, errors.New("decoded native byte vector is empty")
	}
	result := make([]byte, len(leaves))
	for index, leaf := range leaves {
		item, ok := leaf.value.(gsrpctypes.U8)
		if !ok {
			return nil, fmt.Errorf("decoded native vector item %d has type %T", index, leaf.value)
		}
		result[index] = byte(item)
	}
	return result, nil
}

// Normalizes parser event names which omit their pallet prefix.
func finalNativeEventName(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	return crv4.PalletName + "." + name
}

// Selects either one applied extrinsic phase or runtime initialization.
func finalNativeEventMatchesPhase(event *gsrpcparser.Event, extrinsicIndex *uint32, initialization bool) bool {
	if event == nil || event.Phase == nil {
		return false
	}
	if initialization {
		return event.Phase.IsInitialization
	}
	return extrinsicIndex != nil && event.Phase.IsApplyExtrinsic && event.Phase.AsApplyExtrinsic == *extrinsicIndex
}

// Requires the unique operation-specific runtime events and their exact fields.
func verifyFinalNativeOperationEvents(events []*gsrpcparser.Event, extrinsicIndex *uint32, expected FinalNativeCallEvidence) error {
	if err := verifyFinalNativeCallEvidenceShape(expected, expected.Operation); err != nil {
		return err
	}
	switch expected.Operation {
	case finalNativeOperationRegistration:
		matches := 0
		for _, event := range events {
			if !finalNativeEventMatchesPhase(event, extrinsicIndex, false) || finalNativeEventName(event.Name) != finalNativeNeuronRegisteredEvent || len(event.Fields) != 3 {
				continue
			}
			netuid, netuidErr := finalNativeUnsigned(event.Fields[0].Value, 16)
			uid, uidErr := finalNativeUnsigned(event.Fields[1].Value, 16)
			hotkey, hotkeyErr := finalNativeDecodedBytes(event.Fields[2].Value, 32)
			wantHotkey, _ := finalNativeAccountPublicKey(expected.Hotkey)
			if netuidErr == nil && uidErr == nil && hotkeyErr == nil && uint16(netuid) == expected.Netuid && uint16(uid) == expected.UID && bytes.Equal(hotkey, wantHotkey[:]) {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("native registration has %d exact NeuronRegistered events, want one", matches)
		}
	case finalNativeOperationCommit:
		matches := 0
		for _, event := range events {
			if !finalNativeEventMatchesPhase(event, extrinsicIndex, false) || finalNativeEventName(event.Name) != finalNativeTimelockedCommittedEvent || len(event.Fields) != 4 {
				continue
			}
			who, whoErr := finalNativeDecodedBytes(event.Fields[0].Value, 32)
			netuid, netuidErr := finalNativeUnsigned(event.Fields[1].Value, 16)
			commitHash, commitErr := finalNativeDecodedBytes(event.Fields[2].Value, 32)
			round, roundErr := finalNativeUnsigned(event.Fields[3].Value, 64)
			wantWho, _ := finalNativeAccountPublicKey(expected.Signer)
			wantHash, _ := hex.DecodeString(strings.TrimPrefix(expected.CiphertextBlake2, "0x"))
			if whoErr == nil && netuidErr == nil && commitErr == nil && roundErr == nil && bytes.Equal(who, wantWho[:]) && uint16(netuid) == expected.Netuid && bytes.Equal(commitHash, wantHash) && round == expected.RevealRound {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("native CRv4 commit has %d exact TimelockedWeightsCommitted events, want one", matches)
		}
	case finalNativeOperationReveal:
		weightsIndex, revealIndex := -1, -1
		weightsMatches, revealMatches := 0, 0
		wantSigner, _ := finalNativeAccountPublicKey(expected.Signer)
		for index, event := range events {
			if !finalNativeEventMatchesPhase(event, nil, true) {
				continue
			}
			switch finalNativeEventName(event.Name) {
			case finalNativeWeightsSetEvent:
				if len(event.Fields) != 2 {
					continue
				}
				netuid, netuidErr := finalNativeUnsigned(event.Fields[0].Value, 16)
				uid, uidErr := finalNativeUnsigned(event.Fields[1].Value, 16)
				if netuidErr == nil && uidErr == nil && uint16(netuid) == expected.Netuid && uint16(uid) == expected.UID {
					weightsMatches++
					weightsIndex = index
				}
			case finalNativeTimelockedRevealedEvent:
				if len(event.Fields) != 2 {
					continue
				}
				netuid, netuidErr := finalNativeUnsigned(event.Fields[0].Value, 16)
				who, whoErr := finalNativeDecodedBytes(event.Fields[1].Value, 32)
				if netuidErr == nil && whoErr == nil && uint16(netuid) == expected.Netuid && bytes.Equal(who, wantSigner[:]) {
					revealMatches++
					revealIndex = index
				}
			}
		}
		if weightsMatches != 1 || revealMatches != 1 || weightsIndex >= revealIndex {
			return fmt.Errorf("native CRv4 reveal/application event lineage weights=%d reveal=%d order=%d/%d", weightsMatches, revealMatches, weightsIndex, revealIndex)
		}
	case finalNativeOperationApplication:
		// Application is a pinned storage-state proof. Its unique WeightsSet and
		// TimelockedWeightsRevealed events are verified by the paired reveal.
		return nil
	default:
		return fmt.Errorf("unsupported native operation %q", expected.Operation)
	}
	return nil
}
