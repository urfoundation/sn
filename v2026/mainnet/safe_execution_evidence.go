// Pure Safe calculations bind published singleton semantics to explicit inputs.
// They have no signer, account selector, chain reader, custody owner or executor.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const maximumSafeExecutionBytes = 64 * 1024
const maximumSafeExecutionSignatures = 32
const maximumSafeExecutionLogs = 128
const maximumSafeExecutionLogBytes = 256 * 1024
const safeExecutionMethod = "execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)"
const safeExecutionDomainTypeHash = "0x47e79534a245952e8b16893a336b85a3d9ea9fa8c573f3d803afb92a79469218"
const safeExecutionTransactionTypeHash = "0xbb8310d486368db6bd6f849402fdd73ad53d316b5a4b2644ad6efe0f941286d8"

// A private profile owns a parsed ABI only after authenticating exact published
// artifact bytes. It is immutable after construction and safe for concurrent reads.
type safeExecutionProfile struct {
	version     string
	variant     string
	artifact    safeReleaseArtifactPin
	contractAbi abi.ABI
}

// These absent facts remain false even when all pure calculations agree.
type safeExecutionScope struct {
	CurrentChainVerified     bool
	CanonicalReceiptVerified bool
	InitializerOwnerVerified bool
	OwnerMembershipVerified  bool
	CurrentThresholdVerified bool
	SafeAuthorityVerified    bool
	SigningAuthorized        bool
	CustodyOwned             bool
	Executable               bool
	EvidenceAnchorVerified   bool
	InstallationComplete     bool
}

// Nonce belongs to the Safe EIP-712 struct; execTransaction calldata reads that
// value from storage instead. No field selects an actual account or signer.
type safeExecutionTransaction struct {
	ChainId        *big.Int
	Safe           common.Address
	To             common.Address
	Value          *big.Int
	Data           []byte
	Operation      uint8
	SafeTxGas      *big.Int
	BaseGas        *big.Int
	GasPrice       *big.Int
	GasToken       common.Address
	RefundReceiver common.Address
	Nonce          *big.Int
}

// Hashes are mathematical results for a declared domain, never account authority.
// Returned bytes are owned by the result rather than aliasing caller inputs.
type safeExecutionDigest struct {
	Version         string
	Variant         string
	ArtifactSha256  string
	DomainSeparator common.Hash
	StructHash      common.Hash
	Hash            common.Hash
	Preimage        []byte
	Scope           safeExecutionScope
}

// Stateful signature kinds name the exact still-required contract or approval
// check. Recovered ECDSA addresses are not authenticated Safe owner membership.
type safeExecutionSignature struct {
	Kind                           string
	Signer                         common.Address
	EcdsaRecoveryVerified          bool
	ContractCall                   []byte
	ContractReturnMagic            [4]byte
	ContractStateRequired          bool
	ApprovedHashOrExecutorRequired bool
}

// RequiredSignatures is an explicit prefix length, not a proved live threshold.
// Safe may ignore trailing bytes after that prefix and contract signature data.
type safeExecutionSignatures struct {
	Digest              common.Hash
	SignaturesSha256    string
	RequiredSignatures  int
	Prefix              []safeExecutionSignature
	EcdsaPrefixVerified bool
	Scope               safeExecutionScope
}

// Receipt claims must be internally consistent, but their inclusion, source and
// current account state remain unauthenticated in this pure layer.
type safeExecutionReceipt struct {
	TransactionHash  common.Hash
	BlockHash        common.Hash
	BlockNumber      uint64
	TransactionIndex uint
	To               common.Address
	Status           uint64
	Logs             []*types.Log
}

// NonceAfterIncrement is this invocation's immediate uint256 increment, not an
// asserted final-block nonce; inner/delegate or later calls can change storage.
type safeExecutionOutcome struct {
	Digest              common.Hash
	Outcome             string
	SafeNonceEffect     string
	NonceAfterIncrement *big.Int
	Payment             *big.Int
	Scope               safeExecutionScope
}

// No unpinned ABI, version or singleton variant can select execution semantics.
func newSafeExecutionProfile(version, variant string, rawArtifact []byte) (*safeExecutionProfile, error) {
	profile, err := loadSafeReleasePin(version, variant)
	if err != nil {
		return nil, err
	}
	for _, pin := range profile.Artifacts {
		if pin.Name != variant {
			continue
		}
		artifact, err := verifySafeReleaseArtifact(rawArtifact, pin)
		if err != nil {
			return nil, err
		}
		contractAbi, err := abi.JSON(bytes.NewReader(artifact.Abi))
		if err != nil {
			return nil, err
		}
		result := &safeExecutionProfile{version: version, variant: variant, artifact: pin, contractAbi: contractAbi}
		if err := result.checkProfile(); err != nil {
			return nil, err
		}
		return result, nil
	}
	return nil, errors.New("Safe singleton artifact is absent")
}

// Refuse absent profiles or mismatched release identity; the constructor owns
// and authenticates the private ABI before exposing these immutable methods.
func (self *safeExecutionProfile) checkProfile() error {
	if self == nil {
		return errors.New("Safe execution profile is absent")
	}
	profile, err := loadSafeReleasePin(self.version, self.variant)
	if err != nil {
		return err
	}
	matched := false
	for _, pin := range profile.Artifacts {
		if pin.Name == self.variant && pin == self.artifact {
			matched = true
		}
	}
	method := self.contractAbi.Methods["execTransaction"]
	success, successOk := self.contractAbi.Events["ExecutionSuccess"]
	failure, failureOk := self.contractAbi.Events["ExecutionFailure"]
	if !matched || method.Sig != safeExecutionMethod || len(method.Inputs) != 10 ||
		!successOk || !failureOk || len(success.Inputs) != 2 || len(failure.Inputs) != 2 ||
		!success.Inputs[0].Indexed || success.Inputs[1].Indexed || !failure.Inputs[0].Indexed || failure.Inputs[1].Indexed {
		return errors.New("Safe execution ABI/profile differs")
	}
	return nil
}

// Both supported releases sign the same 66-byte EIP-712 preimage. All integer
// widths and dynamic work are bounded before encoding; inputs are never changed.
func (self *safeExecutionProfile) transactionDigest(transaction safeExecutionTransaction) (safeExecutionDigest, error) {
	if err := self.checkProfile(); err != nil {
		return safeExecutionDigest{}, err
	}
	values := []struct {
		name  string
		value *big.Int
	}{
		{name: "chain ID", value: transaction.ChainId}, {name: "value", value: transaction.Value},
		{name: "Safe gas", value: transaction.SafeTxGas}, {name: "base gas", value: transaction.BaseGas},
		{name: "gas price", value: transaction.GasPrice}, {name: "nonce", value: transaction.Nonce},
	}
	for _, value := range values {
		if value.value == nil || value.value.Sign() < 0 || value.value.BitLen() > 256 {
			return safeExecutionDigest{}, fmt.Errorf("Safe %s is not uint256", value.name)
		}
	}
	if transaction.Operation > 1 || len(transaction.Data) > maximumSafeExecutionBytes {
		return safeExecutionDigest{}, errors.New("Safe operation or calldata exceeds the pure profile")
	}
	domain := make([]byte, 96)
	domainType := common.HexToHash(safeExecutionDomainTypeHash)
	copy(domain, domainType[:])
	transaction.ChainId.FillBytes(domain[32:64])
	copy(domain[76:], transaction.Safe[:])
	domainHash := crypto.Keccak256Hash(domain)
	encoded := make([]byte, 352)
	typeHash := common.HexToHash(safeExecutionTransactionTypeHash)
	copy(encoded, typeHash[:])
	copy(encoded[44:64], transaction.To[:])
	transaction.Value.FillBytes(encoded[64:96])
	dataHash := crypto.Keccak256Hash(transaction.Data)
	copy(encoded[96:128], dataHash[:])
	encoded[159] = transaction.Operation
	transaction.SafeTxGas.FillBytes(encoded[160:192])
	transaction.BaseGas.FillBytes(encoded[192:224])
	transaction.GasPrice.FillBytes(encoded[224:256])
	copy(encoded[268:288], transaction.GasToken[:])
	copy(encoded[300:320], transaction.RefundReceiver[:])
	transaction.Nonce.FillBytes(encoded[320:352])
	structHash := crypto.Keccak256Hash(encoded)
	preimage := make([]byte, 66)
	preimage[0], preimage[1] = 0x19, 0x01
	copy(preimage[2:34], domainHash[:])
	copy(preimage[34:], structHash[:])
	return safeExecutionDigest{Version: self.version, Variant: self.variant, ArtifactSha256: self.artifact.ArtifactSha256,
		DomainSeparator: domainHash, StructHash: structHash, Hash: crypto.Keccak256Hash(preimage), Preimage: preimage}, nil
}

// Canonical calldata carries supplied signature bytes but does not create them
// or authorize the transaction. Safe nonce and domain remain bound by the digest.
func (self *safeExecutionProfile) encodeTransaction(transaction safeExecutionTransaction, signatures []byte) ([]byte, error) {
	if _, err := self.transactionDigest(transaction); err != nil {
		return nil, err
	}
	if len(signatures) > maximumSafeExecutionBytes {
		return nil, errors.New("Safe signature bytes exceed the pure profile")
	}
	return self.contractAbi.Pack("execTransaction", transaction.To, transaction.Value, transaction.Data,
		transaction.Operation, transaction.SafeTxGas, transaction.BaseGas, transaction.GasPrice,
		transaction.GasToken, transaction.RefundReceiver, signatures)
}

// Recovery follows Safe's ecrecover rules, including high-s and eth_sign. Owner
// membership, threshold and both stateful signature kinds stay unverified.
func (self *safeExecutionProfile) inspectSignatures(transaction safeExecutionTransaction, signatures []byte, required int) (safeExecutionSignatures, error) {
	digest, err := self.transactionDigest(transaction)
	if err != nil {
		return safeExecutionSignatures{}, err
	}
	if required < 1 || required > maximumSafeExecutionSignatures || len(signatures) > maximumSafeExecutionBytes || len(signatures) < required*65 {
		return safeExecutionSignatures{}, errors.New("Safe signature count or byte bounds differ")
	}
	result := safeExecutionSignatures{Digest: digest.Hash, SignaturesSha256: safeReleaseHash(signatures), RequiredSignatures: required, EcdsaPrefixVerified: true}
	lastSigner := common.Address{}
	for i := 0; i < required; i++ {
		raw := signatures[i*65 : (i+1)*65]
		entry := safeExecutionSignature{}
		switch raw[64] {
		case 0:
			entry.Kind, entry.Signer, entry.ContractStateRequired = "contract", common.BytesToAddress(raw[:32]), true
			offset := new(big.Int).SetBytes(raw[32:64])
			if !offset.IsUint64() || offset.Uint64() < uint64(required*65) || offset.Uint64() > uint64(len(signatures)-32) {
				return safeExecutionSignatures{}, errors.New("Safe contract signature offset is outside its dynamic region")
			}
			start := int(offset.Uint64())
			length := new(big.Int).SetBytes(signatures[start : start+32])
			if !length.IsUint64() || length.Uint64() > uint64(len(signatures)-start-32) {
				return safeExecutionSignatures{}, errors.New("Safe contract signature length is outside its dynamic region")
			}
			contractSignature := signatures[start+32 : start+32+int(length.Uint64())]
			bytesType, _ := abi.NewType("bytes", "", nil)
			arguments := abi.Arguments{{Type: bytesType}, {Type: bytesType}}
			function := "isValidSignature(bytes,bytes)"
			var signedValue any = digest.Preimage
			if self.version == "1.5.0" {
				hashType, _ := abi.NewType("bytes32", "", nil)
				arguments[0].Type, function, signedValue = hashType, "isValidSignature(bytes32,bytes)", digest.Hash
			}
			encoded, err := arguments.Pack(signedValue, contractSignature)
			if err != nil {
				return safeExecutionSignatures{}, err
			}
			selector := crypto.Keccak256([]byte(function))[:4]
			entry.ContractCall = append(slices.Clone(selector), encoded...)
			copy(entry.ContractReturnMagic[:], selector)
			result.EcdsaPrefixVerified = false
		case 1:
			entry.Kind, entry.Signer, entry.ApprovedHashOrExecutorRequired = "approved-hash", common.BytesToAddress(raw[:32]), true
			result.EcdsaPrefixVerified = false
		default:
			signingHash := digest.Hash
			v := raw[64]
			entry.Kind = "eip712-ecdsa"
			if v > 30 {
				v -= 4
				entry.Kind = "eth-sign-ecdsa"
				signingHash = crypto.Keccak256Hash([]byte("\x19Ethereum Signed Message:\n32"), digest.Hash[:])
			}
			r, s := new(big.Int).SetBytes(raw[:32]), new(big.Int).SetBytes(raw[32:64])
			if v != 27 && v != 28 || !crypto.ValidateSignatureValues(v-27, r, s, false) {
				return safeExecutionSignatures{}, errors.New("Safe ECDSA recovery values differ")
			}
			wire := slices.Clone(raw)
			wire[64] = v - 27
			publicKey, err := crypto.SigToPub(signingHash[:], wire)
			if err != nil {
				return safeExecutionSignatures{}, err
			}
			entry.Signer, entry.EcdsaRecoveryVerified = crypto.PubkeyToAddress(*publicKey), true
		}
		if bytes.Compare(entry.Signer[:], lastSigner[:]) <= 0 || entry.Signer == common.BytesToAddress([]byte{1}) {
			return safeExecutionSignatures{}, errors.New("Safe signer ordering, duplicate or sentinel differs")
		}
		lastSigner = entry.Signer
		result.Prefix = append(result.Prefix, entry)
	}
	return result, nil
}

// This classifies internally consistent receipt claims, not canonical inclusion.
// Persisted inner failure consumes this Safe nonce; an outer revert rolls it back.
func (self *safeExecutionProfile) classifyReceipt(transaction safeExecutionTransaction, expectedTransactionHash common.Hash, receipt safeExecutionReceipt) (safeExecutionOutcome, error) {
	digest, err := self.transactionDigest(transaction)
	if err != nil {
		return safeExecutionOutcome{}, err
	}
	if expectedTransactionHash == (common.Hash{}) || receipt.TransactionHash != expectedTransactionHash || receipt.BlockHash == (common.Hash{}) ||
		receipt.To != transaction.Safe || receipt.Status > 1 || len(receipt.Logs) > maximumSafeExecutionLogs {
		return safeExecutionOutcome{}, errors.New("Safe receipt identity, status or bound differs")
	}
	result := safeExecutionOutcome{Digest: digest.Hash}
	if receipt.Status == types.ReceiptStatusFailed {
		if len(receipt.Logs) != 0 {
			return safeExecutionOutcome{}, errors.New("reverted outer transaction retained logs")
		}
		result.Outcome, result.SafeNonceEffect = "outer-reverted", "rolled-back"
		return result, nil
	}
	success := self.contractAbi.Events["ExecutionSuccess"]
	failure := self.contractAbi.Events["ExecutionFailure"]
	matching, totalBytes := 0, 0
	var previousIndex uint
	for i, log := range receipt.Logs {
		if log == nil || log.Removed || len(log.Topics) > 4 || len(log.Data) > maximumSafeExecutionBytes ||
			log.TxHash != receipt.TransactionHash || log.BlockHash != receipt.BlockHash || log.BlockNumber != receipt.BlockNumber ||
			log.TxIndex != receipt.TransactionIndex || i > 0 && log.Index <= previousIndex {
			return safeExecutionOutcome{}, errors.New("Safe receipt log identity, order or bounds differ")
		}
		previousIndex = log.Index
		totalBytes += len(log.Data)
		if totalBytes > maximumSafeExecutionLogBytes {
			return safeExecutionOutcome{}, errors.New("Safe receipt total log data exceeds the pure profile")
		}
		if log.Address != transaction.Safe || len(log.Topics) == 0 || log.Topics[0] != success.ID && log.Topics[0] != failure.ID {
			continue
		}
		if len(log.Topics) != 2 || len(log.Data) != 32 {
			return safeExecutionOutcome{}, errors.New("Safe execution event shape differs")
		}
		if log.Topics[1] != digest.Hash {
			continue
		}
		matching++
		if matching != 1 {
			return safeExecutionOutcome{}, errors.New("Safe receipt contains duplicate or conflicting outcomes")
		}
		result.Payment = new(big.Int).SetBytes(log.Data)
		if transaction.GasPrice.Sign() == 0 && result.Payment.Sign() != 0 {
			return safeExecutionOutcome{}, errors.New("Safe zero gas price produced a payment")
		}
		if log.Topics[0] == success.ID {
			result.Outcome = "safe-inner-success"
		} else {
			if transaction.SafeTxGas.Sign() == 0 && transaction.GasPrice.Sign() == 0 {
				return safeExecutionOutcome{}, errors.New("Safe committed failure conflicts with its zero-gas success requirement")
			}
			result.Outcome = "safe-inner-failure"
		}
	}
	if matching != 1 {
		return safeExecutionOutcome{}, errors.New("matching Safe execution outcome is absent")
	}
	result.SafeNonceEffect = "committed-increment"
	result.NonceAfterIncrement = new(big.Int).Add(transaction.Nonce, big.NewInt(1))
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	result.NonceAfterIncrement.And(result.NonceAfterIncrement, mask)
	return result, nil
}
