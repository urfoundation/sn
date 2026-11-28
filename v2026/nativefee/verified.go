// Package nativefee owns invocation of an independently pinned native verifier.
// JSON statements are transport data. Only Invoke constructs a Verified result.
package nativefee

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/crypto/blake2b"
)

const StatementSchema = "urnetwork-owned-native-fee-outcome-v1"

type Reference struct {
	Path   string `json:"path" yaml:"path"`
	Sha256 string `json:"sha256" yaml:"sha256"`
}

// Originals keep the full independently pinned proof inputs available to the
// durable consumer. Their order is part of the statement's canonical grammar.
type Original struct {
	Kind      string    `json:"kind"`
	Reference Reference `json:"reference"`
}

var originalKinds = [...]string{"request", "approval", "archive", "receipt_collection", "checkpoint", "finality_proof", "replay_job"}

func originalLimit(kind string) int64 {
	switch kind {
	case "request":
		return 64 * 1024
	case "approval", "checkpoint":
		return 1024 * 1024
	case "archive":
		return 128 * 1024 * 1024
	case "receipt_collection":
		return 64 * 1024 * 1024
	case "finality_proof":
		return 16 * 1024 * 1024
	case "replay_job":
		return 192 * 1024 * 1024
	default:
		return 0
	}
}

// NativePolicy has the existing admitted-native-fee policy's exact JSON grammar.
// Its key and pins must come from independent authority, never from the request.
type NativePolicy struct {
	ApprovalPublicKey string `json:"approval_ed25519_public_key" yaml:"approval_ed25519_public_key"`
	Genesis           string `json:"genesis_hash" yaml:"genesis_hash"`
	EvmChainId        uint64 `json:"evm_chain_id" yaml:"evm_chain_id"`
	EngineSha256      string `json:"engine_sha256" yaml:"engine_sha256"`
	CheckpointSha256  string `json:"native_checkpoint_sha256" yaml:"native_checkpoint_sha256"`
	ReviewSha256      string `json:"runtime_semantics_review_sha256" yaml:"runtime_semantics_review_sha256"`
	ProfileSha256     string `json:"original_callsite_profile_sha256" yaml:"original_callsite_profile_sha256"`
}

type Authority struct {
	Verifier     Reference    `json:"verifier" yaml:"verifier"`
	NativePolicy NativePolicy `json:"native_policy" yaml:"native_policy"`
}

func canonicalHex(value, prefix string, size int) bool {
	if !strings.HasPrefix(value, prefix) || value != strings.ToLower(value) {
		return false
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil && len(raw) == size && !slices.Equal(raw, make([]byte, size))
}

func (self Reference) Validate() error {
	if len(self.Path) > 4096 || !filepath.IsAbs(self.Path) || filepath.Clean(self.Path) != self.Path || strings.ContainsRune(self.Path, 0) || !canonicalHex(self.Sha256, "sha256:", 32) {
		return errors.New("native fee input requires an absolute clean path and exact SHA256")
	}
	return nil
}

func (self NativePolicy) Validate() error {
	if !canonicalHex(self.ApprovalPublicKey, "0x", 32) || !canonicalHex(self.Genesis, "0x", 32) || self.EvmChainId == 0 {
		return errors.New("native fee policy lacks independent key or network")
	}
	for _, value := range []string{self.EngineSha256, self.CheckpointSha256, self.ReviewSha256, self.ProfileSha256} {
		if !canonicalHex(value, "sha256:", 32) {
			return errors.New("native fee policy lacks exact original source pins")
		}
	}
	return nil
}

func (self Authority) Validate() error {
	return errors.Join(self.Verifier.Validate(), self.NativePolicy.Validate())
}

func (self NativePolicy) Hash() string {
	raw, _ := json.Marshal(self)
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// Statement is the bounded wire output of the pinned verifier. It is not an
// admission API: decoding or constructing one cannot create a Verified result.
type Statement struct {
	Schema                string     `json:"schema"`
	RequestSha256         string     `json:"request_sha256"`
	RequestHash           string     `json:"request_hash"`
	PolicyHash            string     `json:"policy_hash"`
	ApprovalHash          string     `json:"approval_hash"`
	ProofHash             string     `json:"proof_hash"`
	Genesis               string     `json:"genesis_hash"`
	EvmChainId            uint64     `json:"evm_chain_id"`
	RuntimeCodeSha256     string     `json:"runtime_code_sha256"`
	EngineSha256          string     `json:"engine_sha256"`
	ProfileSha256         string     `json:"original_callsite_profile_sha256"`
	PayerProfile          string     `json:"payer_profile"`
	PayerRuntimeSource    string     `json:"payer_runtime_source"`
	NativeBlockNumber     uint64     `json:"native_block_number"`
	NativeBlockHash       string     `json:"native_block_hash"`
	NativeParentNumber    uint64     `json:"native_parent_number"`
	NativeParentHash      string     `json:"native_parent_hash"`
	NativeStateRoot       string     `json:"native_state_root"`
	NativeParentStateRoot string     `json:"native_parent_state_root"`
	NativeFinalizedNumber uint64     `json:"native_finalized_number"`
	NativeFinalizedHash   string     `json:"native_finalized_hash"`
	TransactionHash       string     `json:"transaction_hash"`
	Sender                string     `json:"sender"`
	Nonce                 uint64     `json:"nonce"`
	RawTransaction        []byte     `json:"raw_transaction"`
	ReceiptStatus         uint64     `json:"receipt_status"`
	ReceiptBytesHash      string     `json:"receipt_bytes_hash"`
	EvmBlockNumber        uint64     `json:"evm_block_number"`
	EvmBlockHash          string     `json:"evm_block_hash"`
	TransactionIndex      uint64     `json:"transaction_index"`
	ExtrinsicIndex        uint32     `json:"extrinsic_index"`
	Payer                 string     `json:"payer"`
	WithdrawalRao         string     `json:"withdrawal_rao"`
	RefundRao             string     `json:"refund_rao"`
	DebitRao              string     `json:"debit_rao"`
	Originals             []Original `json:"originals"`
}

func amount(value string) (*big.Int, error) {
	if value == "" || len(value) > 20 || len(value) > 1 && value[0] == '0' {
		return nil, errors.New("noncanonical native fee amount")
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return nil, errors.New("noncanonical native fee amount")
		}
	}
	n, ok := new(big.Int).SetString(value, 10)
	if !ok || !n.IsUint64() {
		return nil, errors.New("native fee amount exceeds original u64 Rao")
	}
	return n, nil
}

// Validate checks the protocol join, not original proof authority. Invoke also
// requires this statement to come from its completed pinned child process.
func (self Statement) Validate(policy NativePolicy, request Reference, transactionHash string) error {
	if err := errors.Join(policy.Validate(), request.Validate()); err != nil {
		return err
	}
	if len(self.Originals) != len(originalKinds) {
		return errors.New("native fee outcome omits original proof custody")
	}
	paths, hashes := map[string]bool{}, map[string]bool{}
	for index, original := range self.Originals {
		if original.Kind != originalKinds[index] || original.Reference.Validate() != nil || paths[original.Reference.Path] || hashes[original.Reference.Sha256] || index == 0 && original.Reference != request {
			return errors.New("native fee outcome original proof roles or pins differ")
		}
		paths[original.Reference.Path], hashes[original.Reference.Sha256] = true, true
	}
	if self.Schema != StatementSchema || self.RequestSha256 != request.Sha256 || self.PolicyHash != policy.Hash() || self.Genesis != policy.Genesis || self.EvmChainId != policy.EvmChainId || self.EngineSha256 != policy.EngineSha256 || self.ProfileSha256 != policy.ProfileSha256 || self.TransactionHash != transactionHash || self.ReceiptStatus > 1 || self.NativeBlockNumber == 0 || self.NativeParentNumber != self.NativeBlockNumber-1 || self.NativeFinalizedNumber < self.NativeBlockNumber || self.NativeFinalizedNumber == self.NativeBlockNumber && self.NativeFinalizedHash != self.NativeBlockHash || self.PayerProfile != "subtensor-fron-parent-context-hashed-h160-v1" || self.PayerRuntimeSource != "67dcf7f791dc495064c293f080a0702cb433e51e" {
		return errors.New("native fee outcome differs from independently selected proof context")
	}
	for _, value := range []string{self.RequestHash, self.ApprovalHash, self.ProofHash, self.RuntimeCodeSha256, self.ReceiptBytesHash} {
		if !canonicalHex(value, "sha256:", 32) {
			return errors.New("native fee outcome lacks original evidence identity")
		}
	}
	for _, value := range []string{self.NativeBlockHash, self.NativeParentHash, self.NativeStateRoot, self.NativeParentStateRoot, self.NativeFinalizedHash, self.TransactionHash, self.EvmBlockHash, self.Payer} {
		if !canonicalHex(value, "0x", 32) {
			return errors.New("native fee outcome lacks canonical block or account identity")
		}
	}
	if !canonicalHex(self.Sender, "0x", 20) || len(self.RawTransaction) == 0 || len(self.RawTransaction) > 1024*1024 {
		return errors.New("native fee outcome lacks bounded original signature")
	}
	var transaction types.Transaction
	if err := transaction.UnmarshalBinary(self.RawTransaction); err != nil {
		return err
	}
	sender, err := types.Sender(types.LatestSignerForChainID(new(big.Int).SetUint64(self.EvmChainId)), &transaction)
	if err != nil || !transaction.Protected() || !transaction.ChainId().IsUint64() || transaction.ChainId().Uint64() != self.EvmChainId || strings.ToLower(sender.Hex()) != self.Sender || transaction.Nonce() != self.Nonce || strings.ToLower(transaction.Hash().Hex()) != self.TransactionHash {
		return errors.New("native fee outcome does not bind its original signature")
	}
	account := blake2b.Sum256(append([]byte("evm:"), sender.Bytes()...))
	if self.Payer != "0x"+hex.EncodeToString(account[:]) {
		return errors.New("native fee outcome contradicts approved payer mapping")
	}
	w, e1 := amount(self.WithdrawalRao)
	r, e2 := amount(self.RefundRao)
	d, e3 := amount(self.DebitRao)
	if err := errors.Join(e1, e2, e3); err != nil {
		return err
	}
	if r.Cmp(w) > 0 || new(big.Int).Sub(w, r).Cmp(d) != 0 {
		return errors.New("native fee withdrawal and refund do not conserve debit")
	}
	return nil
}

// Verified has no exported fields, decoder or constructor. A zero value and a
// JSON round trip are unusable. Completed evidence survives worker/file release;
// Check binds it to the consuming owner's exact independently retained authority.
type Verified struct {
	statement Statement
	authority Authority
	completed bool
}

func (self *Verified) Check(ctx context.Context, authority Authority) error {
	if ctx == nil {
		return errors.New("native fee owner context is absent")
	}
	if err := errors.Join(ctx.Err(), authority.Validate()); err != nil {
		return err
	}
	if self == nil || !self.completed || self.authority != authority {
		return errors.New("native fee result was not produced by this owned verifier authority")
	}
	return nil
}

func (self *Verified) Facts() Statement {
	if self == nil || !self.completed {
		return Statement{}
	}
	result := self.statement
	result.RawTransaction = slices.Clone(result.RawTransaction)
	result.Originals = slices.Clone(result.Originals)
	return result
}
