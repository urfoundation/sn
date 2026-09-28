// Contract installation consumes exact public release artifacts. The generator
// preserves reviewed bytecode; independent phase approval supplies authority.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/ss58"
	"github.com/urfoundation/sn/stabi"
)

// Semantic immutable names and exact offsets originate in the existing
// generator's source/layout checks, not mutable deployed contract responses.
type contractReleaseArtifact struct {
	Name                string           `json:"name"`
	Abi                 string           `json:"abi"`
	Creation            string           `json:"creation"`
	Runtime             string           `json:"runtime"`
	RuntimeHash         string           `json:"runtime_hash"`
	ArtifactHash        string           `json:"artifact_hash"`
	StorageLayoutHash   string           `json:"storage_layout_hash"`
	ImmutableReferences map[string][]int `json:"immutable_references"`
}

// The wire format contains production artifacts only, never simulator helpers.
type contractReleaseArtifacts struct {
	Schema    string                    `json:"schema"`
	Artifacts []contractReleaseArtifact `json:"artifacts"`
}

// Canonical bytecode is bounded before decoding and never accepts placeholders.
func contractCode(encoded string, maximum int) ([]byte, error) {
	if encoded == "" || len(encoded)%2 != 0 || len(encoded) > maximum*2 || strings.ToLower(encoded) != encoded {
		return nil, errors.New("contract bytecode is empty, noncanonical or oversized")
	}
	raw, err := hex.DecodeString(encoded)
	return raw, err
}

// Exact file identity is checked before any runtime, signer or journal opens.
func loadContractRelease(ctx context.Context, reference planFileReference) (contractReleaseArtifacts, error) {
	var envelope contractReleaseArtifacts
	raw, hash, err := readBootstrapRootFile(ctx, reference.Path, 2*1024*1024)
	if err != nil || !planSha256(reference.Sha256) || hash != reference.Sha256 {
		return envelope, errors.Join(errors.New("contract release differs from its independent content pin"), err)
	}
	if err := decodePlanJson(raw, &envelope); err != nil {
		return envelope, err
	}
	if envelope.Schema != "urnetwork-contract-release-artifacts-v1" || len(envelope.Artifacts) != 5 {
		return envelope, errors.New("contract release must contain the five production artifacts")
	}
	wanted := map[string]bool{"ReserveSink": true, "SettlementVault": true, "Coordinator": true, "ERC1967Proxy": true, "ValidatorEvidence": true}
	for _, artifact := range envelope.Artifacts {
		if !wanted[artifact.Name] {
			return envelope, errors.New("contract release has an unknown or duplicated artifact")
		}
		delete(wanted, artifact.Name)
		if _, err := contractCode(artifact.Creation, 48*1024); err != nil {
			return envelope, err
		}
		runtime, err := contractCode(artifact.Runtime, 24*1024)
		if err != nil || crypto.Keccak256Hash(runtime).Hex() != artifact.RuntimeHash || !rootCanonicalHash(artifact.ArtifactHash) || !planSha256(artifact.StorageLayoutHash) {
			return envelope, errors.Join(errors.New("contract runtime or release identity differs"), err)
		}
		if _, err := abi.JSON(strings.NewReader(artifact.Abi)); err != nil {
			return envelope, err
		}
		used := map[int]bool{}
		for name, offsets := range artifact.ImmutableReferences {
			if name == "" || len(offsets) == 0 || len(offsets) > 128 {
				return envelope, errors.New("contract immutable reference is incomplete")
			}
			for _, offset := range offsets {
				if offset < 0 || offset > len(runtime)-32 {
					return envelope, errors.New("contract immutable offset exceeds runtime")
				}
				for i := offset; i < offset+32; i++ {
					if used[i] {
						return envelope, errors.New("contract immutable references overlap")
					}
					used[i] = true
				}
			}
		}
	}
	return envelope, nil
}

// Word values are copied; caller-owned maps cannot mutate the compiled result.
func (self contractReleaseArtifact) withImmutables(values map[string][]byte) ([]byte, error) {
	runtime, err := contractCode(self.Runtime, 24*1024)
	if err != nil {
		return nil, err
	}
	if len(values) != len(self.ImmutableReferences) {
		return nil, errors.New("contract immutable census differs")
	}
	for name, offsets := range self.ImmutableReferences {
		value, ok := values[name]
		if !ok || len(value) != 32 {
			return nil, fmt.Errorf("contract immutable %s is absent or not one word", name)
		}
		for _, offset := range offsets {
			if offset < 0 || offset > len(runtime)-32 {
				return nil, errors.New("contract immutable offset exceeds runtime")
			}
			copy(runtime[offset:offset+32], value)
		}
	}
	return runtime, nil
}

// Getters are compared byte-for-byte at the canonical inclusion block, never
// against an unpinned latest view or a successful receipt alone.
type contractGetter struct {
	Data     string `json:"data"`
	Expected string `json:"expected"`
}

// Reviewed namespaced storage is checked only at the authenticated canonical
// inclusion. A zero initializer word is distinct from constructor-disabled state.
type contractStorageWord struct {
	Slot     string `json:"slot"`
	Expected string `json:"expected"`
}

// The reserve constructor is the first executable action of installation.
// Its address-derived custody key is fixed before creation.
func contractReservePayload(artifact contractReleaseArtifact, netuid uint16, hotkey [32]byte, deployer common.Address, nonce uint64) ([]byte, []byte, []contractGetter, error) {
	if artifact.Name != "ReserveSink" || netuid != 25 || hotkey == ([32]byte{}) || deployer == (common.Address{}) {
		return nil, nil, nil, errors.New("reserve deployment domain is incomplete")
	}
	creation, err := contractCode(artifact.Creation, 48*1024)
	if err != nil {
		return nil, nil, nil, err
	}
	address := crypto.CreateAddress(deployer, nonce)
	mirror := ss58.EvmMirrorPubkey(address)
	word := func(address common.Address) []byte {
		result := make([]byte, 32)
		copy(result[12:], address[:])
		return result
	}
	uid := make([]byte, 32)
	uid[30], uid[31] = byte(netuid>>8), byte(netuid)
	runtime, err := artifact.withImmutables(map[string][]byte{"netuid": uid, "reserveHotkey": hotkey[:], "selfColdkey": mirror[:], "bootstrap": word(deployer)})
	if err != nil {
		return nil, nil, nil, err
	}
	contract := stabi.NewSTReserveSink()
	parsed, err := abi.JSON(strings.NewReader(artifact.Abi))
	if err != nil {
		return nil, nil, nil, err
	}
	arguments, err := parsed.Pack("", netuid, hotkey, mirror, deployer)
	if err != nil || !bytes.Equal(arguments, contract.PackConstructor(netuid, hotkey, mirror, deployer)) {
		return nil, nil, nil, errors.Join(errors.New("reserve artifact constructor differs from generated binding"), err)
	}
	getters := []contractGetter{}
	for _, getter := range []struct{ data, value []byte }{{data: contract.PackNetuid(), value: uid}, {data: contract.PackReserveHotkey(), value: hotkey[:]}, {data: contract.PackSelfColdkey(), value: mirror[:]}, {data: contract.PackBootstrap(), value: word(deployer)}, {data: contract.PackRecorder(), value: make([]byte, 32)}} {
		getters = append(getters, contractGetter{Data: "0x" + hex.EncodeToString(getter.data), Expected: "0x" + hex.EncodeToString(getter.value)})
	}
	return append(creation, arguments...), runtime, getters, nil
}

// These review values come from the exact approved constructor calldata. The
// address-derived coldkey, netuid and bootstrap are reconstructed independently.
type contractVaultConstructor struct {
	EscrowHotkey          string `json:"escrow_hotkey"`
	MinimumClaimTtlBlocks uint64 `json:"minimum_claim_ttl_blocks"`
	MinimumTransferTaoRao uint64 `json:"minimum_transfer_tao_rao"`
}

// Six immutable words and the initial unbound/unregistered accounting state
// must agree with genuine constructor execution at the canonical inclusion.
func contractVaultPayload(artifact contractReleaseArtifact, netuid uint16, hotkey [32]byte, minimumClaimTtlBlocks, minimumTransferTaoRao uint64, deployer common.Address, nonce uint64) ([]byte, []byte, []contractGetter, error) {
	if artifact.Name != "SettlementVault" || netuid != 25 || hotkey == ([32]byte{}) || minimumClaimTtlBlocks == 0 || minimumTransferTaoRao == 0 || deployer == (common.Address{}) {
		return nil, nil, nil, errors.New("vault deployment domain is incomplete")
	}
	creation, err := contractCode(artifact.Creation, 48*1024)
	if err != nil {
		return nil, nil, nil, err
	}
	mirror := ss58.EvmMirrorPubkey(crypto.CreateAddress(deployer, nonce))
	word := func(value uint64) []byte {
		result := make([]byte, 32)
		binary.BigEndian.PutUint64(result[24:], value)
		return result
	}
	bootstrap := make([]byte, 32)
	copy(bootstrap[12:], deployer[:])
	uid, ttl, minimum := word(uint64(netuid)), word(minimumClaimTtlBlocks), word(minimumTransferTaoRao)
	runtime, err := artifact.withImmutables(map[string][]byte{"netuid": uid, "escrowHotkey": hotkey[:], "selfColdkey": mirror[:], "minimumClaimTTLBlocks": ttl, "minimumTransferTaoRao": minimum, "bootstrap": bootstrap})
	if err != nil {
		return nil, nil, nil, err
	}
	contract := stabi.NewSTSettlementVault()
	parsed, err := abi.JSON(strings.NewReader(artifact.Abi))
	if err != nil {
		return nil, nil, nil, err
	}
	arguments, err := parsed.Pack("", netuid, hotkey, mirror, minimumClaimTtlBlocks, minimumTransferTaoRao, deployer)
	if err != nil || !bytes.Equal(arguments, contract.PackConstructor(netuid, hotkey, mirror, minimumClaimTtlBlocks, minimumTransferTaoRao, deployer)) {
		return nil, nil, nil, errors.Join(errors.New("vault artifact constructor differs from generated binding"), err)
	}
	getters := []contractGetter{}
	for _, getter := range []struct{ data, value []byte }{
		{data: contract.PackNetuid(), value: uid}, {data: contract.PackEscrowHotkey(), value: hotkey[:]},
		{data: contract.PackSelfColdkey(), value: mirror[:]}, {data: contract.PackMinimumClaimTTLBlocks(), value: ttl},
		{data: contract.PackMinimumTransferTaoRao(), value: minimum}, {data: contract.PackBootstrap(), value: bootstrap},
		{data: contract.PackCoordinator(), value: word(0)}, {data: contract.PackEscrowRegistered(), value: word(0)},
		{data: contract.PackTotalCaptured(), value: word(0)}, {data: contract.PackTotalPaid(), value: word(0)},
		{data: contract.PackPendingFunding(), value: word(0)}, {data: contract.PackOutstandingLiability(), value: word(0)},
		{data: contract.PackEscrowAccounted(), value: word(0)},
	} {
		getters = append(getters, contractGetter{Data: "0x" + hex.EncodeToString(getter.data), Expected: "0x" + hex.EncodeToString(getter.value)})
	}
	return append(creation, arguments...), runtime, getters, nil
}

// The implementation constructor only disables initialization. Its UUPS self
// word binds the predicted CREATE address; proxy initialization is a later action.
func contractCoordinatorPayload(artifact contractReleaseArtifact, deployer common.Address, nonce uint64) ([]byte, []byte, []contractGetter, []contractStorageWord, error) {
	if artifact.Name != "Coordinator" || deployer == (common.Address{}) {
		return nil, nil, nil, nil, errors.New("coordinator implementation deployment domain is incomplete")
	}
	creation, err := contractCode(artifact.Creation, 48*1024)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	parsed, err := abi.JSON(strings.NewReader(artifact.Abi))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	binding, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	arguments, err := parsed.Pack("")
	expected, bindingErr := binding.Pack("")
	if err != nil || bindingErr != nil || len(parsed.Constructor.Inputs) != 0 || len(arguments) != 0 || !bytes.Equal(arguments, expected) {
		return nil, nil, nil, nil, errors.Join(errors.New("coordinator implementation constructor must match the empty generated binding"), err, bindingErr)
	}
	address := crypto.CreateAddress(deployer, nonce)
	selfWord := make([]byte, 32)
	copy(selfWord[12:], address[:])
	runtime, err := artifact.withImmutables(map[string][]byte{"__self": selfWord})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	contract := stabi.NewSTCoordinator()
	getters := []contractGetter{}
	for _, data := range [][]byte{contract.PackNetuid(), contract.PackSelfColdkey(), contract.PackSettlementVault(), contract.PackReserveSink(), contract.PackGuardian(), contract.PackPendingGuardian(), contract.PackPendingGuardianEpoch(), contract.PackPaused(), contract.PackCampaignReserved(), contract.PackCommitmentOracle(), contract.PackPendingCommitmentOracle(), contract.PackPendingCommitmentOracleEpoch(), contract.PackValidatorEvidence(), contract.PackOwner(), contract.PackPolicyCount(), contract.PackOperatorCount()} {
		getters = append(getters, contractGetter{Data: "0x" + hex.EncodeToString(data), Expected: "0x" + strings.Repeat("00", 32)})
	}
	// These constants are fixed by the reviewed OpenZeppelin source imported by
	// STCoordinator. The independent release file still pins that exact bytecode.
	implementationSlot := "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc"
	version, err := binding.Methods["UPGRADE_INTERFACE_VERSION"].Outputs.Pack("5.0.0")
	if err != nil {
		return nil, nil, nil, nil, err
	}
	getters = append(getters, contractGetter{Data: "0x" + hex.EncodeToString(contract.PackProxiableUUID()), Expected: implementationSlot}, contractGetter{Data: "0x" + hex.EncodeToString(contract.PackUPGRADEINTERFACEVERSION()), Expected: "0x" + hex.EncodeToString(version)})
	storage := []contractStorageWord{{Slot: "0xf0c57e16840df040f15088dc2f81fe391c3923bec73e23a9662efc9c229c6a00", Expected: "0x" + strings.Repeat("00", 24) + strings.Repeat("ff", 8)}, {Slot: implementationSlot, Expected: "0x" + strings.Repeat("00", 32)}}
	return creation, runtime, getters, storage, nil
}
