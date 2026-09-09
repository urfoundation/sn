package crv4

// Reviewed runtimes 454 and 455 use Utility.batch_all for exactly two calls
// under the original signer: one SHA256 metadata commitment and the actual
// timelock-encrypted CRv4 write. No plaintext weights or recursive source hash
// is published. A commitment authenticates bytes, not measurement truth.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/registry"
	"github.com/centrifuge/go-substrate-rpc-client/v4/registry/parser"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/block"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/extrinsic"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/extrinsic/extensions"
	"github.com/vedhavyas/go-subkey/v2/sr25519"
	"golang.org/x/crypto/blake2b"
)

// PreparedSourceSubmissionSchema distinguishes mandatory atomic source writes
// from historical standalone CRv4 bytes; neither schema silently upgrades.
const PreparedSourceSubmissionSchema = "urnetwork-crv4-prepared-source-submission-v2"

// PreparedSourceCommitment is private write-ahead identity. Its four fields
// are reconstructed against the exact signed call/payload and live pinned
// metadata before preparation or replay. Hash is domain-separated by the
// validator's pre-Prepared measurement protocol, outside this transport layer.
type PreparedSourceCommitment struct {
	Hash               string `json:"hash"`
	GenesisHash        string `json:"genesis_hash"`
	RuntimeSpec        uint32 `json:"runtime_spec"`
	TransactionVersion uint32 `json:"transaction_version"`
}

func canonicalSourceHex(value string, size int) ([]byte, error) {
	if len(value) != 2+2*size {
		return nil, errors.New("crv4: source hex has an invalid length")
	}
	raw, err := codec.HexDecodeString(value)
	if err != nil || codec.HexEncodeToString(raw) != value {
		return nil, errors.New("crv4: source hex is noncanonical")
	}
	return raw, nil
}

// Both reviewed artifacts use the same call and signed-extension layout.
// This is encoding admission, not artifact authority: live release callers
// must authenticate their exact version, code and metadata at the chosen block.
func reviewedSourceEncodingVersion(spec, transaction uint32) bool {
	return (spec == 454 || spec == 455) && transaction == 1
}

// This strict offline encoding is the audited 454/455 shape. The live metadata
// builder below must independently reproduce it; changed call indices,
// argument order or signed extensions fail closed before any broadcast.
func preparedSourceEncoding(prepared *PreparedSubmission) (call, fields, payload []byte, resultErr error) {
	if prepared == nil || prepared.SourceCommitment == nil || prepared.Schema != PreparedSourceSubmissionSchema {
		return nil, nil, nil, errors.New("crv4: source preparation is absent")
	}
	source := prepared.SourceCommitment
	if !reviewedSourceEncodingVersion(source.RuntimeSpec, source.TransactionVersion) || prepared.Netuid == 0 || prepared.CommitRevealVersion != CommitRevealVersion4 || prepared.RevealRound == 0 {
		return nil, nil, nil, errors.New("crv4: source runtime or CRv4 parameters are unsupported")
	}
	if prepared.PreparedAtBlock == 0 {
		return nil, nil, nil, errors.New("crv4: source preparation block is absent")
	}
	if _, err := canonicalSourceHex(prepared.PreparedAtBlockHash, 32); err != nil {
		return nil, nil, nil, err
	}
	hash, err := canonicalSourceHex(source.Hash, 32)
	if err != nil || bytes.Equal(hash, make([]byte, 32)) {
		return nil, nil, nil, errors.New("crv4: source hash is invalid")
	}
	genesis, err := canonicalSourceHex(source.GenesisHash, 32)
	if err != nil || bytes.Equal(genesis, make([]byte, 32)) {
		return nil, nil, nil, errors.New("crv4: source genesis is invalid")
	}
	ciphertext, err := codec.HexDecodeString(prepared.CiphertextHex)
	if err != nil || len(ciphertext) == 0 || len(ciphertext) > MaxCommitSizeBytes || codec.HexEncodeToString(ciphertext) != prepared.CiphertextHex {
		return nil, nil, nil, errors.New("crv4: source ciphertext is invalid")
	}
	// Utility.batch_all(Vec<RuntimeCall>[Commitments.set_commitment, CRv4]).
	call = []byte{11, 2, 8, 18, 0}
	call = append(call, encodeNetuid(prepared.Netuid)...)
	call = append(call, 4, dataSHA256Variant)
	call = append(call, hash...)
	commitIndex := byte(113)
	if prepared.Mecid != nil {
		commitIndex = 118
	}
	call = append(call, 7, commitIndex)
	call = append(call, encodeNetuid(prepared.Netuid)...)
	if prepared.Mecid != nil {
		call = append(call, *prepared.Mecid)
	}
	length, err := codec.Encode(types.NewUCompactFromUInt(uint64(len(ciphertext))))
	if err != nil {
		return nil, nil, nil, err
	}
	call = append(call, length...)
	call = append(call, ciphertext...)
	call = binary.LittleEndian.AppendUint64(call, prepared.RevealRound)
	call = binary.LittleEndian.AppendUint16(call, prepared.CommitRevealVersion)
	nonce, err := codec.Encode(types.NewUCompactFromUInt(uint64(prepared.AccountNonce)))
	if err != nil {
		return nil, nil, nil, err
	}
	fields = append([]byte{0}, nonce...) // immortal era, compact nonce
	fields = append(fields, 0, 0)        // zero compact tip; metadata mode Disabled
	payload = append(append([]byte{}, call...), fields...)
	payload = binary.LittleEndian.AppendUint32(payload, source.RuntimeSpec)
	payload = binary.LittleEndian.AppendUint32(payload, source.TransactionVersion)
	payload = append(payload, genesis...)
	payload = append(payload, genesis...) // immortal era's birth block
	payload = append(payload, 0)          // additional Option<metadata hash> None
	return call, fields, payload, nil
}

func validatePreparedSourceBytes(prepared *PreparedSubmission, raw []byte) error {
	if codec.HexEncodeToString(raw) != prepared.ExtrinsicHex {
		return errors.New("crv4: source extrinsic hex is noncanonical")
	}
	plain, err := codec.HexDecodeString(prepared.PayloadHex)
	if err != nil || codec.HexEncodeToString(plain) != prepared.PayloadHex {
		return errors.New("crv4: source payload hex is noncanonical")
	}
	call, fields, payload, err := preparedSourceEncoding(prepared)
	if err != nil {
		return err
	}
	hotkey, err := canonicalSourceHex(prepared.HotkeyHex, 32)
	if err != nil {
		return err
	}
	// Derive the length prefix from the complete expected body. No substring
	// search or guessed signature boundary can hide another call or signer.
	bodyLength := 1 + 1 + 32 + 1 + 64 + len(fields) + len(call)
	prefix, err := codec.Encode(types.NewUCompactFromUInt(uint64(bodyLength)))
	if err != nil || len(raw) != len(prefix)+bodyLength || !bytes.Equal(raw[:len(prefix)], prefix) {
		return errors.New("crv4: source extrinsic framing differs")
	}
	body := raw[len(prefix):]
	if body[0] != 0x84 || body[1] != 0 || !bytes.Equal(body[2:34], hotkey) || body[34] != 1 || !bytes.Equal(body[99:99+len(fields)], fields) || !bytes.Equal(body[99+len(fields):], call) {
		return errors.New("crv4: source extrinsic signer, nonce, extensions or atomic call differs")
	}
	public, err := (sr25519.Scheme{}).FromPublicKey(hotkey)
	if err != nil {
		return err
	}
	if len(payload) > 256 {
		hash := blake2b.Sum256(payload)
		payload = hash[:]
	}
	if !public.Verify(payload, body[35:99]) {
		return errors.New("crv4: source extrinsic signature is invalid")
	}
	return nil
}

func (self *Chain) newSourceCommitmentBatchCall(netuid uint16, mecid *uint8, source [32]byte, ciphertext []byte, round uint64, version uint16) (types.Call, error) {
	if self == nil || self.Meta == nil || self.Runtime == nil || !reviewedSourceEncodingVersion(uint32(self.Runtime.SpecVersion), uint32(self.Runtime.TransactionVersion)) {
		return types.Call{}, errors.New("crv4: source metadata is not the reviewed runtime")
	}
	anchor, err := self.NewSetFleetCommitmentCall(netuid, source)
	if err != nil {
		return types.Call{}, err
	}
	var commit types.Call
	if mecid == nil {
		commit, err = types.NewCall(self.Meta, PalletName+"."+CallCommitTimelocked, types.NewU16(netuid), types.Bytes(ciphertext), types.U64(round), types.U16(version))
	} else {
		commit, err = types.NewCall(self.Meta, PalletName+"."+CallCommitTimelockedMech, types.NewU16(netuid), types.U8(*mecid), types.Bytes(ciphertext), types.U64(round), types.U16(version))
	}
	if err != nil {
		return types.Call{}, err
	}
	return types.NewCall(self.Meta, "Utility.batch_all", []types.Call{anchor, commit})
}

// ValidatePreparedSource qualifies the strict persisted signature/call against
// the caller's independently authenticated runtime metadata. This method never
// trusts runtime versions contained only in the candidate write-ahead record.
func (self *Chain) ValidatePreparedSource(prepared *PreparedSubmission) error {
	if self == nil || self.Meta == nil || self.Meta.Version != 14 || self.Runtime == nil || prepared == nil || prepared.SourceCommitment == nil {
		return errors.New("crv4: source signing authority is unavailable")
	}
	if _, err := prepared.Validate(); err != nil {
		return err
	}
	source := prepared.SourceCommitment
	if source.GenesisHash != self.GenesisHash.Hex() || source.RuntimeSpec != uint32(self.Runtime.SpecVersion) || source.TransactionVersion != uint32(self.Runtime.TransactionVersion) {
		return errors.New("crv4: prepared source chain or runtime differs from independent signing authority")
	}
	hashRaw, _ := canonicalSourceHex(source.Hash, 32)
	var hash [32]byte
	copy(hash[:], hashRaw)
	ciphertext, _ := codec.HexDecodeString(prepared.CiphertextHex)
	call, err := self.newSourceCommitmentBatchCall(prepared.Netuid, prepared.Mecid, hash, ciphertext, prepared.RevealRound, prepared.CommitRevealVersion)
	if err != nil {
		return err
	}
	encoded, err := codec.Encode(call)
	if err != nil {
		return err
	}
	wantCall, _, wantPayload, err := preparedSourceEncoding(prepared)
	if err != nil || !bytes.Equal(encoded, wantCall) {
		return errors.Join(errors.New("crv4: runtime metadata changes the atomic source call encoding"), err)
	}
	payload := &extrinsic.Payload{EncodedCall: encoded}
	for _, extension := range self.Meta.AsMetadataV14.Extrinsic.SignedExtensions {
		lookup, exists := self.Meta.AsMetadataV14.EfficientLookup[extension.Type.Int64()]
		if !exists || lookup == nil || len(lookup.Path) == 0 {
			return errors.New("crv4: source signed extension metadata is incomplete")
		}
		mutator := extrinsic.PayloadMutatorFns[extensions.SignedExtensionName(lookup.Path[len(lookup.Path)-1])]
		if mutator == nil {
			return errors.New("crv4: source signed extension is unsupported")
		}
		mutator(payload)
	}
	values := extrinsic.SignedFieldValues{}
	for _, option := range []extrinsic.SigningOption{
		extrinsic.WithEra(types.ExtrinsicEra{IsImmortalEra: true}, self.GenesisHash), extrinsic.WithNonce(types.NewUCompactFromUInt(uint64(prepared.AccountNonce))), extrinsic.WithTip(types.NewUCompactFromUInt(0)), extrinsic.WithSpecVersion(self.Runtime.SpecVersion), extrinsic.WithTransactionVersion(self.Runtime.TransactionVersion), extrinsic.WithGenesisHash(self.GenesisHash), extrinsic.WithMetadataMode(extensions.CheckMetadataModeDisabled, extensions.CheckMetadataHash{Hash: types.NewEmptyOption[types.H256]()}),
	} {
		option(values)
	}
	if err := payload.MutateSignedFields(values); err != nil {
		return err
	}
	actualPayload, err := codec.Encode(payload)
	if err != nil || !bytes.Equal(actualPayload, wantPayload) {
		return errors.Join(errors.New("crv4: runtime metadata changes source signature payload"), err)
	}
	return nil
}

// ValidatePreparedSourceWeightsContext re-runs the actual exact-rational
// normalization under the prepared block's real subnet controls, without
// allocating a nonce, encrypting again or creating replacement signed bytes.
func (self *Chain) ValidatePreparedSourceWeightsContext(ctx context.Context, prepared *PreparedSubmission, uids []uint16, scores []*big.Rat, options SubmitOptions) error {
	if err := self.ValidatePreparedSource(prepared); err != nil {
		return err
	}
	hash, err := types.NewHashFromHexString(prepared.PreparedAtBlockHash)
	if err != nil {
		return err
	}
	state, err := self.EpochScheduleStateAtContext(ctx, prepared.Netuid, hash)
	if err != nil || state.CurrentBlock != prepared.PreparedAtBlock || state.SubnetEpochIndex != prepared.SubnetEpoch || prepared.VersionKey != options.VersionKey {
		return errors.Join(errors.New("crv4: source preparation differs from the actual schedule/version"), err)
	}
	version, maximum, err := resolveSubmitParametersAtContext(ctx, self, prepared.Netuid, hash, options)
	if err != nil || prepared.CommitRevealVersion != version {
		return errors.Join(errors.New("crv4: source commit/reveal version differs from actual controls"), err)
	}
	capped, err := ApplyMaxWeightLimitRational(scores, maximum)
	if err != nil {
		return err
	}
	actualUids, actualValues, err := NormalizeRationalToU16(uids, capped)
	if err != nil {
		return err
	}
	if err := repairMaxWeightLimitU16(actualUids, actualValues, maximum); err != nil {
		return err
	}
	if !slices.Equal(actualUids, prepared.UIDs) || !slices.Equal(actualValues, prepared.Values) {
		return errors.New("crv4: actual encrypted vector differs from exact replayed measurement scores")
	}
	return ctx.Err()
}

// CheckSourceCommitmentCapacityAtContext reads actual quota and exact usage at
// the prepared block. An absent MaxSpace uses only the pinned metadata's real
// fallback, never a configuration constant. One SHA256 costs exactly 100 units.
func (self *Chain) CheckSourceCommitmentCapacityAtContext(ctx context.Context, netuid uint16, hotkey [32]byte, hash types.Hash, epoch uint64) error {
	if ctx == nil || self == nil || self.Meta == nil || self.Meta.Version != 14 || hash == (types.Hash{}) {
		return errors.New("crv4: source capacity context is unavailable")
	}
	read := func(name string, args ...[]byte) ([]byte, error) {
		key, err := types.CreateStorageKey(self.Meta, CommitmentsPalletName, name, args...)
		if err != nil {
			return nil, err
		}
		raw, err := self.storageRawAtContext(ctx, key, hash)
		if err != nil || raw == nil {
			return nil, err
		}
		return []byte(*raw), nil
	}
	maxRaw, err := read("MaxSpace")
	if err != nil {
		return err
	}
	if maxRaw == nil {
		for _, pallet := range self.Meta.AsMetadataV14.Pallets {
			if string(pallet.Name) == CommitmentsPalletName && pallet.HasStorage {
				for _, item := range pallet.Storage.Items {
					if string(item.Name) == "MaxSpace" {
						maxRaw = append([]byte{}, item.Fallback...)
					}
				}
			}
		}
	}
	if len(maxRaw) != 4 {
		return errors.New("crv4: source MaxSpace has no exact u32 storage/default")
	}
	maximum := uint64(binary.LittleEndian.Uint32(maxRaw))
	usage, err := read("UsedSpaceOf", encodeNetuid(netuid), hotkey[:])
	if err != nil {
		return err
	}
	used := uint64(0)
	if usage != nil {
		if len(usage) != 16 {
			return errors.New("crv4: source UsedSpaceOf is not the pinned u64/u64 shape")
		}
		last := binary.LittleEndian.Uint64(usage)
		if last > epoch {
			return errors.New("crv4: source usage epoch is ahead of the prepared schedule")
		}
		if last == epoch {
			used = binary.LittleEndian.Uint64(usage[8:])
		}
	}
	if maximum < 100 || used > maximum-100 {
		return errors.New("crv4: source commitment has insufficient actual epoch capacity")
	}
	return ctx.Err()
}

// SourceCommitmentSlotAtContext distinguishes a genuinely unused metadata
// slot from malformed, partial or inaccessible storage. No error string is
// interpreted as absence by the production role-separation check.
func (self *Chain) SourceCommitmentSlotAtContext(ctx context.Context, netuid uint16, hotkey [32]byte, hash types.Hash) (*FinalizedCommitment, error) {
	if ctx == nil || self == nil || self.Meta == nil || netuid == 0 || hotkey == ([32]byte{}) || hash == (types.Hash{}) {
		return nil, errors.New("crv4: source metadata slot identity is incomplete")
	}
	key, err := types.CreateStorageKey(self.Meta, CommitmentsPalletName, "CommitmentOf", encodeNetuid(netuid), hotkey[:])
	if err != nil {
		return nil, err
	}
	raw, err := self.storageRawAtContext(ctx, key, hash)
	if err != nil {
		return nil, err
	}
	if raw != nil {
		return self.FleetCommitmentAtContext(ctx, netuid, hotkey, hash)
	}
	lastKey, err := types.CreateStorageKey(self.Meta, CommitmentsPalletName, "LastCommitment", encodeNetuid(netuid), hotkey[:])
	if err != nil {
		return nil, err
	}
	last, err := self.storageRawAtContext(ctx, lastKey, hash)
	if err != nil || last != nil {
		return nil, errors.Join(errors.New("crv4: absent source slot has occupied or inaccessible LastCommitment"), err)
	}
	return nil, ctx.Err()
}

func sourceEventValue(value any) any {
	for depth := 0; depth < 8; depth++ {
		fields, ok := value.(registry.DecodedFields)
		if !ok || len(fields) != 1 {
			return value
		}
		if fields[0] == nil {
			return nil
		}
		value = fields[0].Value
	}
	return nil
}

func sourceEventBytes(value any, size int) ([]byte, bool) {
	value = sourceEventValue(value)
	ref := reflect.ValueOf(value)
	if !ref.IsValid() || ref.Kind() != reflect.Array && ref.Kind() != reflect.Slice || ref.Len() != size {
		return nil, false
	}
	result := make([]byte, size)
	for index := range result {
		element := ref.Index(index)
		if !element.CanInterface() {
			return nil, false
		}
		number, ok := sourceEventUint(element.Interface())
		if !ok || number > 255 {
			return nil, false
		}
		result[index] = byte(number)
	}
	return result, true
}

func sourceEventUint(value any) (uint64, bool) {
	ref := reflect.ValueOf(sourceEventValue(value))
	if !ref.IsValid() {
		return 0, false
	}
	switch ref.Kind() {
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return ref.Uint(), true
	}
	return 0, false
}

// The exact extrinsic phase must contain both operation events in call order,
// two Utility.ItemCompleted events, BatchCompleted and final dispatch success.
// No other extrinsic in the same block may supply a missing postcondition.
func verifySourceCommitmentEvents(prepared *PreparedSubmission, index uint32, records []*parser.Event) error {
	hotkey, _ := codec.HexDecodeString(prepared.HotkeyHex)
	ciphertext, _ := codec.HexDecodeString(prepared.CiphertextHex)
	cipherHash := blake2b.Sum256(ciphertext)
	want := []string{"Commitments.Commitment", "Utility.ItemCompleted", "SubtensorModule.TimelockedWeightsCommitted", "Utility.ItemCompleted", "Utility.BatchCompleted", "System.ExtrinsicSuccess"}
	step := 0
	for _, event := range records {
		if event == nil || event.Phase == nil || !event.Phase.IsApplyExtrinsic || event.Phase.AsApplyExtrinsic != index {
			continue
		}
		switch event.Name {
		case "Commitments.Commitment", "Utility.ItemCompleted", "SubtensorModule.TimelockedWeightsCommitted", "Utility.BatchCompleted", "System.ExtrinsicSuccess":
			if step >= len(want) || event.Name != want[step] {
				return errors.New("crv4: source atomic event order or count differs")
			}
			for _, field := range event.Fields {
				if field == nil {
					return errors.New("crv4: source event has a nil decoded field")
				}
			}
			if event.Name == "Commitments.Commitment" {
				if len(event.Fields) != 2 {
					return errors.New("crv4: source commitment event shape differs")
				}
				netuid, ok := sourceEventUint(event.Fields[0].Value)
				who, found := sourceEventBytes(event.Fields[1].Value, 32)
				if !ok || netuid != uint64(prepared.Netuid) || !found || !bytes.Equal(who, hotkey) {
					return errors.New("crv4: source commitment event identity differs")
				}
			}
			if event.Name == "SubtensorModule.TimelockedWeightsCommitted" {
				if len(event.Fields) != 4 {
					return errors.New("crv4: source encrypted commit event shape differs")
				}
				who, found := sourceEventBytes(event.Fields[0].Value, 32)
				netuidIndex, indexOk := sourceEventUint(event.Fields[1].Value)
				digest, ok := sourceEventBytes(event.Fields[2].Value, 32)
				round, roundOk := sourceEventUint(event.Fields[3].Value)
				wantIndex := uint64(prepared.Netuid)
				if prepared.Mecid != nil {
					wantIndex = min(uint64(65535), wantIndex+min(uint64(65535), uint64(*prepared.Mecid)*4096))
				}
				if !found || !bytes.Equal(who, hotkey) || !indexOk || netuidIndex != wantIndex || !ok || !bytes.Equal(digest, cipherHash[:]) || !roundOk || round != prepared.RevealRound {
					return errors.New("crv4: source encrypted commit event differs from actual ciphertext")
				}
			}
			step++
		case "System.ExtrinsicFailed", "Utility.BatchInterrupted", "Utility.ItemFailed", "Utility.BatchCompletedWithErrors":
			return errors.New("crv4: source atomic dispatch failed")
		}
	}
	if step != len(want) {
		return errors.New("crv4: source atomic operation events are incomplete")
	}
	return nil
}

// VerifyFinalizedSourceContext joins inclusion, both exact operation events
// and the single-slot commitment at that exact finalized write block. Recovery
// reads this historical slot, never the latest overwritten metadata commitment.
func (self *Chain) VerifyFinalizedSourceContext(ctx context.Context, prepared *PreparedSubmission, receipt *FinalizedExtrinsic) error {
	if ctx == nil || receipt == nil || receipt.BlockNumber == 0 || receipt.BlockHash == (types.Hash{}) || prepared == nil || receipt.ExtrinsicHash.Hex() != prepared.ExtrinsicHash {
		return errors.New("crv4: source finality identity is incomplete")
	}
	if err := self.ValidatePreparedSource(prepared); err != nil {
		return err
	}
	finalized, err := FinalizedHeadContext(ctx, self)
	if err != nil {
		return err
	}
	header, err := self.HeaderAtContext(ctx, finalized)
	if err != nil || header == nil || uint64(header.Number) < receipt.BlockNumber {
		return errors.Join(errors.New("crv4: source receipt is not finalized"), err)
	}
	var canonical types.Hash
	if err := self.API.Client.CallContext(ctx, &canonical, "chain_getBlockHash", receipt.BlockNumber); err != nil || canonical != receipt.BlockHash {
		return errors.Join(errors.New("crv4: source receipt is not the canonical native block"), err)
	}
	if err := self.VerifyFinalizedExtrinsicContext(ctx, receipt.BlockHash, receipt.ExtrinsicHash); err != nil {
		return err
	}
	var signed block.SignedBlock
	if err := self.API.Client.CallContext(ctx, &signed, "chain_getBlock", receipt.BlockHash.Hex()); err != nil {
		return err
	}
	if uint64(signed.Block.Header.Number) != receipt.BlockNumber {
		return errors.New("crv4: source receipt block number differs from actual body")
	}
	index, found, err := extrinsicIndex(signed.Block.Extrinsics, receipt.ExtrinsicHash)
	if err != nil || !found {
		return errors.Join(errors.New("crv4: source exact transaction is absent"), err)
	}
	key, err := types.CreateStorageKey(self.Meta, "System", "Events")
	if err != nil {
		return err
	}
	raw, err := self.storageRawAtContext(ctx, key, receipt.BlockHash)
	if err != nil || raw == nil || len(*raw) > 16*1024*1024 {
		return errors.Join(errors.New("crv4: source events are unavailable or exceed the finite block bound"), err)
	}
	registered, err := registry.NewFactory().CreateEventRegistry(self.Meta)
	if err != nil {
		return err
	}
	records, err := parser.NewEventParser().ParseEvents(registered, raw)
	if err != nil {
		return err
	}
	if err := verifySourceCommitmentEvents(prepared, index, records); err != nil {
		return err
	}
	public, _ := codec.HexDecodeString(prepared.HotkeyHex)
	var hotkey [32]byte
	copy(hotkey[:], public)
	hashRaw, _ := codec.HexDecodeString(prepared.SourceCommitment.Hash)
	var hash [32]byte
	copy(hash[:], hashRaw)
	observed, err := self.FleetCommitmentAtContext(ctx, prepared.Netuid, hotkey, receipt.BlockHash)
	if err != nil {
		return err
	}
	if err := ValidateFleetCommitmentWrite(hash, receipt.BlockNumber, observed); err != nil {
		return fmt.Errorf("crv4: source metadata readback: %w", err)
	}
	return ctx.Err()
}
