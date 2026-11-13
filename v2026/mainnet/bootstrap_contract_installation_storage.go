// Installation proves the complete initial storage of all five contracts. The
// finite profile rejects prior service activity and hidden mapping entries.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/blake2b"
)

// Every word comes from approved constructors, exact original inclusion and
// the one permitted evidence binding. Runtime code includes all immutables.
type bootstrapContractInstallationAccount struct {
	Address     common.Address           `json:"address"`
	RuntimeHash common.Hash              `json:"runtime_hash"`
	Words       []safeCurrentStorageWord `json:"words"`
}

// The proof root is an owned-node finalized assertion, not independent
// consensus. Complete storage makes no claim about past internal execution.
type bootstrapContractInstallationStorage struct {
	NativeHash      string                                 `json:"native_hash"`
	NativeNumber    uint64                                 `json:"native_number"`
	StateRoot       string                                 `json:"state_root"`
	RuntimeCodeHash string                                 `json:"runtime_code_hash"`
	Accounts        []bootstrapContractInstallationAccount `json:"accounts"`
	CompleteStorage bool                                   `json:"complete_storage"`
}

// These exact reviewed layouts delimit this initial-installation profile. A
// later release with different storage requires a new explicit interpretation.
func bootstrapContractInstallationAccounts(ctx context.Context, plans []evmCreatePlan, records []evmActionRecord) ([]bootstrapContractInstallationAccount, error) {
	if len(plans) != 8 || len(records) != 8 || plans[4].ProxyConstructor == nil || records[4].Receipt == nil || records[4].Receipt.BlockNumber == 0 {
		return nil, errors.New("installation storage lacks the complete original graph")
	}
	release, err := loadContractRelease(ctx, plans[0].Config.Plan.Artifacts)
	if err != nil {
		return nil, err
	}
	layouts := map[string]string{
		"Coordinator":       "sha256:35292b7d34fe63ac674a23cad3be32639abe86752b7213e281d446b5dadfe1dc",
		"ERC1967Proxy":      "sha256:913ff69d6bb92c2fcb8def2a2070fae7ea4bb6d000f71396953ecda144261fb9",
		"ReserveSink":       "sha256:8905295fa1ad48bfa4bbe781a1bcf3cbede80fd7e59e9ce10bd9845d2dee4841",
		"SettlementVault":   "sha256:44d35144a609e764787a13ace11a1f3efaf0482d28a861c0c50200ba1013910a",
		"ValidatorEvidence": "sha256:c7b1d4c69d489d880dad1eb8f79de12f6af04f9abfb0bdf7f7f4b60ad045204e",
	}
	for _, artifact := range release.Artifacts {
		if layouts[artifact.Name] != artifact.StorageLayoutHash {
			return nil, errors.New("installation storage requires the exact reviewed five-contract layouts")
		}
	}
	accounts := make([]bootstrapContractInstallationAccount, 0, 5)
	for _, index := range []int{2, 4, 5, 6, 7} {
		plan := plans[index]
		values := map[common.Hash]common.Hash{}
		for _, word := range plan.Storage {
			if !validHash(word.Slot) || common.HexToHash(word.Slot).Hex() != word.Slot || !validHash(word.Expected) || common.HexToHash(word.Expected).Hex() != word.Expected {
				return nil, errors.New("installation storage projection contains a malformed word")
			}
			values[common.HexToHash(word.Slot)] = common.HexToHash(word.Expected)
		}
		if index == 4 {
			constructor := plan.ProxyConstructor
			policy := constructor.ApprovedPolicy.binding()
			word := func(value uint64) common.Hash { return common.BigToHash(new(big.Int).SetUint64(value)) }
			for slot, value := range map[uint64]common.Hash{
				0: word(uint64(plan.Config.Plan.Netuid)), 1: common.HexToHash(constructor.SelfColdkey),
				2: common.BytesToHash(plans[6].Address[:]), 3: common.BytesToHash(plans[5].Address[:]),
				4: common.BytesToHash(constructor.Guardian[:]), 6: word(1),
				16: common.BytesToHash(constructor.CommitmentOracle[:]), 23: common.BytesToHash(plans[7].Address[:]),
			} {
				values[word(slot)] = value
			}
			pack := func(numbers ...uint64) common.Hash {
				var result common.Hash
				for i, number := range numbers {
					binary.BigEndian.PutUint64(result[24-i*8:32-i*8], number)
				}
				return result
			}
			base := new(big.Int).SetBytes(crypto.Keccak256(common.LeftPadBytes([]byte{6}, 32)))
			for offset, value := range []common.Hash{policy.PolicyHash,
				pack(0, records[4].Receipt.BlockNumber, policy.EpochBlocks, policy.RootCommitWindowBlocks),
				pack(policy.FinalizeOffsetBlocks, policy.CloseGraceBlocks, policy.ClaimTTLEpochs, policy.ClaimGraceEpochs),
				pack(policy.MaximumBindingValidityEpochs, policy.CommitmentMaxAgeBlocks),
				common.BigToHash(policy.EpochDepositCapRao), common.BigToHash(policy.CampaignDepositCapRao)} {
				values[common.BigToHash(new(big.Int).Add(base, big.NewInt(int64(offset))))] = value
			}
		}
		account := bootstrapContractInstallationAccount{Address: plan.Address, RuntimeHash: crypto.Keccak256Hash(plan.Runtime), Words: []safeCurrentStorageWord{}}
		for slot, value := range values {
			if value != (common.Hash{}) {
				account.Words = append(account.Words, safeCurrentStorageWord{Slot: slot, Value: value})
			}
		}
		slices.SortFunc(account.Words, func(a, b safeCurrentStorageWord) int { return bytes.Compare(a.Slot[:], b.Slot[:]) })
		accounts = append(accounts, account)
	}
	return accounts, ctx.Err()
}

// Request permitted nonzero words and each complete storage prefix. An omitted
// branch remains an error in the proof walker, never evidence of empty storage.
func bootstrapContractInstallationStorageKeys(accounts []bootstrapContractInstallationAccount) []string {
	keys := []string{runtimeCodeStorageKey}
	for _, account := range accounts {
		for _, item := range []string{"AccountCodes", "AccountCodesMetadata", "AccountStorages"} {
			keys = append(keys, "0x"+hex.EncodeToString(safeCurrentNativeAccountKey(item, account.Address)))
		}
		for _, word := range account.Words {
			keys = append(keys, "0x"+hex.EncodeToString(safeCurrentNativeStorageKey(account.Address, word.Slot)))
		}
	}
	return keys
}

// Verify exact code metadata and the entire account prefix, including hashed
// entries unreachable through Solidity getters. Explicit zero entries refuse.
func verifyBootstrapContractInstallationStorage(ctx context.Context, accounts []bootstrapContractInstallationAccount, runtime rootReceiptProfile, head chainIdentity, witness safeCurrentStorageWitness) (*bootstrapContractInstallationStorage, error) {
	if ctx == nil || len(accounts) != 5 || witness.At != head.FinalizedHash || head.runtimeVersion != runtime.RuntimeVersion {
		return nil, errors.New("installation storage proof changed its selected scope")
	}
	height, err := witness.Header.authenticate(head.FinalizedHash)
	if err != nil || height != head.FinalizedNumber {
		return nil, errors.Join(errors.New("installation storage native header differs"), err)
	}
	trie, err := newSafeCurrentStorageTrie(ctx, witness.Nodes)
	if err != nil {
		return nil, err
	}
	root := [32]byte(common.HexToHash(witness.Header.StateRoot))
	code, present, err := trie.read(ctx, root, []byte(":code"))
	if err != nil || !present || len(code) == 0 || len(code) > maximumRuntimeSnapshotCodeBytes || common.Hash(blake2b.Sum256(code)).Hex() != runtime.RuntimeCodeHash {
		return nil, errors.Join(errors.New("installation storage runtime artifact differs"), err)
	}
	seen := map[common.Address]bool{}
	for _, account := range accounts {
		if account.Address == (common.Address{}) || seen[account.Address] || account.RuntimeHash == (common.Hash{}) {
			return nil, errors.New("installation storage repeats or omits an account")
		}
		seen[account.Address] = true
		raw, present, err := trie.read(ctx, root, safeCurrentNativeAccountKey("AccountCodes", account.Address))
		if err != nil || !present {
			return nil, errors.Join(errors.New("installation storage account code is absent"), err)
		}
		reader := &rootScaleReader{data: raw}
		length, err := reader.compact()
		if err != nil || length == 0 || length > 64*1024 || int(length) != len(raw)-reader.offset || crypto.Keccak256Hash(raw[reader.offset:]) != account.RuntimeHash {
			return nil, errors.New("installation storage account runtime differs")
		}
		metadata, present, err := trie.read(ctx, root, safeCurrentNativeAccountKey("AccountCodesMetadata", account.Address))
		if err != nil || !present || len(metadata) != 40 || binary.LittleEndian.Uint64(metadata[:8]) != length || !bytes.Equal(metadata[8:], account.RuntimeHash[:]) {
			return nil, errors.Join(errors.New("installation storage account metadata differs"), err)
		}
		entries, err := trie.prefix(ctx, root, safeCurrentNativeAccountKey("AccountStorages", account.Address))
		if err != nil || len(entries) != len(account.Words) {
			return nil, errors.Join(errors.New("installation storage contains missing or unapproved account words"), err)
		}
		for i, word := range account.Words {
			if word.Value == (common.Hash{}) || i > 0 && bytes.Compare(account.Words[i-1].Slot[:], word.Slot[:]) >= 0 || !bytes.Equal(entries[string(safeCurrentNativeStorageKey(account.Address, word.Slot))], word.Value[:]) {
				return nil, errors.New("installation storage account word differs from exact initial authority")
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &bootstrapContractInstallationStorage{NativeHash: head.FinalizedHash, NativeNumber: head.FinalizedNumber, StateRoot: witness.Header.StateRoot,
		RuntimeCodeHash: runtime.RuntimeCodeHash, Accounts: accounts, CompleteStorage: true}, nil
}
