// Exact-block native stake observations use the reviewed runtime's own
// parent/child and TAO-weight calculation, not a second floating-point model.
// They establish stake/permit prerequisites only, not activation or inclusion.
package crv4

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

const validatorStakeRuntimeMethod = "SubnetInfoRuntimeApi_get_selective_metagraph"

var errValidatorStakeCompact = errors.New("validator stake compact integer is noncanonical or truncated")

// The identity, weighted stake, threshold and registered subnet-owner UID all
// refer to the same independently selected finalized native block. Owner here
// means SubnetOwnerHotkey, not the coldkey Owner of the candidate hotkey.
type ValidatorStakeObservation struct {
	Identity              ValidatorIdentityObservation
	TotalStakeRao         uint64
	StakeThresholdRao     uint64
	SubnetOwnerHotkey     [32]byte
	SubnetOwnerPresent    bool
	SubnetOwnerUID        uint16
	SubnetOwnerRegistered bool
}

// Mirrors runtime454 check_weights_min_stake and the non-self branch of
// check_validator_permit. Weight version, timing, payload, commit/reveal and
// transaction admission remain separate; this is not a submission guarantee.
func (self ValidatorStakeObservation) MeetsNonSelfStakeAndPermit() bool {
	if self.SubnetOwnerPresent && self.SubnetOwnerRegistered && self.SubnetOwnerUID == self.Identity.UID {
		return true
	}
	return self.Identity.ValidatorPermit && self.TotalStakeRao >= self.StakeThresholdRao
}

// Executes only caller-cancellable reads. Runtime454's frozen selective
// metagraph layout bb7420226d39c0eb is decoded as a complete bounded census.
// Its integer floor preserves comparison with the integer StakeThreshold:
// floor(nonnegative fixed stake) >= threshold iff fixed stake >= threshold.
// The API's Validators field is deliberately unused: it applies strict >
// and omits the registered subnet-owner exception used by actual submission.
func ReadValidatorStakeAtContext(ctx context.Context, chain *Chain, query ValidatorIdentityQuery, allowed ...RuntimeArtifactIdentity) (ValidatorStakeObservation, error) {
	empty := ValidatorStakeObservation{}
	identity, err := ReadValidatorIdentityAtContext(ctx, chain, query, allowed...)
	if err != nil {
		return empty, err
	}
	if identity.Runtime.Version != (RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 454, TransactionVersion: 1, StateVersion: 1}) {
		return empty, errors.New("validator stake runtime layout has not been reviewed")
	}
	artifact, err := AuthenticateRuntimeArtifactAtContext(ctx, chain, query.BlockHash, allowed...)
	if err != nil {
		return empty, err
	}
	if (RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}) != identity.Runtime {
		return empty, errors.New("validator stake runtime changed after identity observation")
	}
	read := func(name string, maximum int, args ...[]byte) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key, err := types.CreateStorageKey(artifact.Metadata, PalletName, name, args...)
		if err != nil {
			return nil, fmt.Errorf("validator stake %s key: %w", name, err)
		}
		// GSRPC preserves the destination for JSON null. A fresh null token
		// retains only this field's explicit default; omitted results and
		// transport errors still return before decoding any observation.
		raw := json.RawMessage("null")
		if err := chain.API.Client.CallContext(ctx, &raw, "state_getStorage", key.Hex(), query.BlockHash.Hex()); err != nil {
			return nil, fmt.Errorf("validator stake %s read: %w", name, err)
		}
		value, err := decodeValidatorIdentityHexResult(raw, maximum, true)
		if err != nil {
			return nil, fmt.Errorf("validator stake %s: %w", name, err)
		}
		return value, nil
	}
	observation := ValidatorStakeObservation{Identity: identity}
	threshold, err := read("StakeThreshold", 8)
	if err != nil {
		return empty, err
	}
	// StakeThreshold is ValueQuery with an exact zero default in runtime454.
	if threshold != nil {
		if len(threshold) != 8 {
			return empty, errors.New("validator stake threshold is not an exact u64")
		}
		observation.StakeThresholdRao = binary.LittleEndian.Uint64(threshold)
	}
	owner, err := read("SubnetOwnerHotkey", 32, encodeNetuid(query.Netuid))
	if err != nil {
		return empty, err
	}
	// get_owner_uid uses try_get, so a missing owner is not its default key.
	if owner != nil {
		if len(owner) != 32 {
			return empty, errors.New("validator stake subnet owner is not an exact AccountId32")
		}
		observation.SubnetOwnerPresent = true
		copy(observation.SubnetOwnerHotkey[:], owner)
		ownerUID, err := read("Uids", 2, encodeNetuid(query.Netuid), owner)
		if err != nil {
			return empty, err
		}
		if ownerUID != nil {
			if len(ownerUID) != 2 || binary.LittleEndian.Uint16(ownerUID) >= identity.SubnetUIDs {
				return empty, errors.New("validator stake subnet owner UID is malformed or outside the census")
			}
			observation.SubnetOwnerRegistered = true
			observation.SubnetOwnerUID = binary.LittleEndian.Uint16(ownerUID)
			if (observation.SubnetOwnerUID == identity.UID) != (observation.SubnetOwnerHotkey == identity.Hotkey) {
				return empty, errors.New("validator stake subnet owner registration contradicts the selected hotkey")
			}
		} else if observation.SubnetOwnerHotkey == identity.Hotkey {
			return empty, errors.New("validator stake selected subnet owner lost its reverse registration")
		}
	}
	// Argument layout is NetUid(u16) followed by Vec<u16>. Select no identity,
	// commitment, or other unbounded field; every unselected option must be None.
	input := encodeNetuid(query.Netuid)
	input = appendCompact(input, 5)
	for _, index := range []uint16{0, 30, 52, 57, 69} {
		input = binary.LittleEndian.AppendUint16(input, index)
	}
	var raw json.RawMessage
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := chain.API.Client.CallContext(ctx, &raw, "state_call", validatorStakeRuntimeMethod, "0x"+hex.EncodeToString(input), query.BlockHash.Hex()); err != nil {
		return empty, fmt.Errorf("validator stake runtime call: %w", err)
	}
	observation.TotalStakeRao, err = decodeValidatorStakeMetagraph(raw, identity)
	if err != nil {
		return empty, err
	}
	canonical, err := validatorIdentityBlockHashAtContext(ctx, chain, query.BlockNumber)
	if err != nil {
		return empty, err
	}
	if canonical != query.BlockHash {
		return empty, errors.New("validator stake canonical block changed during observation")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return observation, nil
}

// Bounds JSON before hex allocation and validates all77 frozen fields, every
// vector length and every compact stake. The selected UID cannot hide malformed
// peers. Only its stake survives; no per-UID slice is allocated from the wire.
func decodeValidatorStakeMetagraph(raw json.RawMessage, identity ValidatorIdentityObservation) (uint64, error) {
	if identity.Netuid == 0 || identity.SubnetUIDs == 0 || identity.UID >= identity.SubnetUIDs {
		return 0, errors.New("validator stake metagraph identity census is invalid")
	}
	// Outer option + compact netuid +76 options + compact census +3 compact
	// vector counts + AccountId32/bool/maximum9-byte compact stake per UID.
	maximum := 1 + 4 + 76 + 4 + 3*4 + int(identity.SubnetUIDs)*(32+1+9)
	if len(raw) < 4 || len(raw) > maximum*2+4 || raw[0] != '"' || raw[len(raw)-1] != '"' || raw[1] != '0' || raw[2] != 'x' {
		return 0, errors.New("validator stake metagraph JSON exceeds its census bound or is malformed")
	}
	encoded := raw[3 : len(raw)-1]
	if len(encoded)%2 != 0 {
		return 0, errors.New("validator stake metagraph hex length is odd")
	}
	for _, value := range encoded {
		if !(value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F') {
			return 0, errors.New("validator stake metagraph contains non-hex bytes")
		}
	}
	data := make([]byte, len(encoded)/2)
	if _, err := hex.Decode(data, encoded); err != nil {
		return 0, err
	}
	if len(data) == 0 || data[0] != 1 {
		return 0, errors.New("validator stake metagraph is missing or has an invalid outer option")
	}
	offset := 1
	readCompact := func() (uint64, error) {
		value, width, err := decodeValidatorStakeCompact(data[offset:])
		if err == nil {
			offset += width
		}
		return value, err
	}
	netuid, err := readCompact()
	if err != nil || netuid != uint64(identity.Netuid) {
		return 0, errors.New("validator stake metagraph netuid differs from the selected subnet")
	}
	var stake uint64
	for index := 1; index <= 76; index++ {
		if offset >= len(data) {
			return 0, errors.New("validator stake metagraph options are truncated")
		}
		tag := data[offset]
		offset++
		selected := index == 30 || index == 52 || index == 57 || index == 69
		if !selected {
			if tag != 0 {
				return 0, fmt.Errorf("validator stake metagraph unselected field %d is present", index)
			}
			continue
		}
		if tag != 1 {
			return 0, fmt.Errorf("validator stake metagraph selected field %d is missing", index)
		}
		count, err := readCompact()
		if err != nil || count != uint64(identity.SubnetUIDs) {
			return 0, fmt.Errorf("validator stake metagraph field %d has a different census", index)
		}
		switch index {
		case 30:
			// This field is the census scalar, not a vector.
		case 52:
			width := int(identity.SubnetUIDs) * 32
			if width > len(data)-offset {
				return 0, errors.New("validator stake metagraph hotkeys are truncated")
			}
			selectedOffset := offset + int(identity.UID)*32
			if !bytes.Equal(data[selectedOffset:selectedOffset+32], identity.Hotkey[:]) {
				return 0, errors.New("validator stake metagraph selected hotkey differs from storage")
			}
			offset += width
		case 57:
			width := int(identity.SubnetUIDs)
			if width > len(data)-offset {
				return 0, errors.New("validator stake metagraph permits are truncated")
			}
			for _, permit := range data[offset : offset+width] {
				if permit > 1 {
					return 0, errors.New("validator stake metagraph contains a non-boolean permit")
				}
			}
			if (data[offset+int(identity.UID)] == 1) != identity.ValidatorPermit {
				return 0, errors.New("validator stake metagraph selected permit differs from storage")
			}
			offset += width
		case 69:
			for uid := uint16(0); uid < identity.SubnetUIDs; uid++ {
				value, err := readCompact()
				if err != nil || value > math.MaxInt64 {
					return 0, errors.New("validator stake metagraph contains a malformed or out-of-range I64F64 stake")
				}
				if uid == identity.UID {
					stake = value
				}
			}
		}
	}
	if offset != len(data) {
		return 0, errors.New("validator stake metagraph has trailing bytes")
	}
	return stake, nil
}

// Canonical SCALE Compact<u64>, with a fixed maximum9 bytes and no allocation.
// Rejects wide encodings of smaller numbers, overflow and truncation.
func decodeValidatorStakeCompact(data []byte) (uint64, int, error) {
	if len(data) == 0 {
		return 0, 0, errValidatorStakeCompact
	}
	switch data[0] & 3 {
	case 0:
		return uint64(data[0] >> 2), 1, nil
	case 1:
		if len(data) < 2 {
			return 0, 0, errValidatorStakeCompact
		}
		value := uint64(binary.LittleEndian.Uint16(data[:2]) >> 2)
		if value < 64 {
			return 0, 0, errValidatorStakeCompact
		}
		return value, 2, nil
	case 2:
		if len(data) < 4 {
			return 0, 0, errValidatorStakeCompact
		}
		value := uint64(binary.LittleEndian.Uint32(data[:4]) >> 2)
		if value < 16*1024 {
			return 0, 0, errValidatorStakeCompact
		}
		return value, 4, nil
	default:
		width := 4 + int(data[0]>>2)
		if width > 8 || len(data) < width+1 || data[width] == 0 {
			return 0, 0, errValidatorStakeCompact
		}
		var value uint64
		for index := 0; index < width; index++ {
			value |= uint64(data[index+1]) << (8 * index)
		}
		if value < 1024*1024*1024 {
			return 0, 0, errValidatorStakeCompact
		}
		return value, width + 1, nil
	}
}
