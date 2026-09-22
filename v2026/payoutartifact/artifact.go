// Package payoutartifact defines the canonical, signed payout evidence shared
// by operators, validators, and independent release analysis. Storage and HTTP
// transport deliberately live outside this package so every consumer verifies
// the same bytes without depending on the server module.
package payoutartifact

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/protocol"
)

const Schema = "urnetwork-payout-artifact-v1"

// Boundary pins one canonical chain boundary used to build an artifact.
type Boundary struct {
	Number uint64 `json:"number"`
	Hash   string `json:"hash"`
}

// ProviderInput is one canonical provider measurement and eligibility row.
type ProviderInput struct {
	ClientID          [16]byte `json:"client_id"`
	NetworkID         [16]byte `json:"network_id"`
	Coldkey           [32]byte `json:"coldkey"`
	UsageBytes        uint64   `json:"usage_bytes"`
	Assignments       uint64   `json:"assignments"`
	Confirmations     uint64   `json:"confirmations"`
	ReliabilityPPM    uint32   `json:"reliability_ppm"`
	Eligible          bool     `json:"eligible"`
	HeadExcluded      bool     `json:"head_excluded"`
	ExclusionReason   string   `json:"exclusion_reason,omitempty"`
	BindingGeneration uint64   `json:"binding_generation,omitempty"`
}

// Leaf is one payout allocation with its canonical Merkle proof.
type Leaf struct {
	Index    uint64     `json:"index"`
	ClientID [16]byte   `json:"allocation_client_id"`
	Coldkey  [32]byte   `json:"coldkey"`
	ShareBPS uint64     `json:"share_bps"`
	Proof    [][32]byte `json:"proof"`
}

// Artifact is the immutable operator statement validators reconstruct before
// using its prior-epoch usage total to audit the next demand deposit.
type Artifact struct {
	Schema               string          `json:"schema"`
	DeploymentID         string          `json:"deployment_id"`
	ChainID              uint64          `json:"chain_id"`
	GenesisHash          string          `json:"genesis_hash"`
	Netuid               uint16          `json:"netuid"`
	Coordinator          common.Address  `json:"coordinator"`
	SettlementVault      common.Address  `json:"settlement_vault"`
	Epoch                uint64          `json:"epoch"`
	NoID                 uint64          `json:"no_id"`
	PolicyHash           string          `json:"policy_hash"`
	Start                Boundary        `json:"start"`
	End                  Boundary        `json:"end"`
	OperatorSnapshotHash string          `json:"operator_snapshot_hash"`
	FleetSnapshotHash    string          `json:"fleet_snapshot_hash"`
	ProviderSnapshotHash string          `json:"provider_snapshot_hash"`
	ReliabilityAMin      uint64          `json:"reliability_a_min"`
	Providers            []ProviderInput `json:"providers"`
	Leaves               []Leaf          `json:"leaves"`
	PayoutRoot           [32]byte        `json:"payout_root"`
	TotalUsageBytes      uint64          `json:"total_usage_bytes"`
	EligibleUsageBytes   uint64          `json:"eligible_usage_bytes"`
	ExcludedUsageBytes   uint64          `json:"excluded_usage_bytes"`
	SharesTotalBPS       uint64          `json:"shares_total_bps"`
	CreatedAt            string          `json:"created_at"`
	Signer               common.Address  `json:"signer"`
	ContentHash          string          `json:"content_hash"`
	Signature            string          `json:"signature"`
}

// BuildInput is the trusted identity and raw-measurement input to Build.
type BuildInput struct {
	DeploymentID, GenesisHash, PolicyHash   string
	ChainID                                 uint64
	Netuid                                  uint16
	Coordinator, SettlementVault            common.Address
	Epoch, NoID                             uint64
	Start, End                              Boundary
	OperatorSnapshotHash, FleetSnapshotHash string
	Providers                               []ProviderInput
	ReliabilityAMin                         uint64
	CreatedAt                               time.Time
}

// Build computes reliability, exact largest-remainder shares, the Merkle root,
// every proof, and all checked summaries. Provider order is canonicalized by
// client id.
func Build(in BuildInput) (*Artifact, error) {
	if in.DeploymentID == "" || in.ChainID == 0 || in.Netuid == 0 || in.NoID == 0 || in.Coordinator == (common.Address{}) || in.SettlementVault == (common.Address{}) || !IsDigest(in.GenesisHash, "0x") || !IsDigest(in.PolicyHash, "0x") || !IsDigest(in.Start.Hash, "0x") || !IsDigest(in.End.Hash, "0x") || !IsDigest(in.OperatorSnapshotHash, "sha256:") || !IsDigest(in.FleetSnapshotHash, "sha256:") || in.End.Number < in.Start.Number || in.ReliabilityAMin == 0 {
		return nil, errors.New("incomplete payout artifact identity/boundary")
	}
	providers := append([]ProviderInput(nil), in.Providers...)
	sort.SliceStable(providers, func(i, j int) bool {
		return bytes.Compare(providers[i].ClientID[:], providers[j].ClientID[:]) < 0
	})
	allocations := make([]protocol.ProviderAllocation, 0, len(providers))
	var totalUsage, eligibleUsage uint64
	var priorClientID [16]byte
	for i := range providers {
		provider := &providers[i]
		if provider.ClientID == ([16]byte{}) || (i > 0 && bytes.Equal(priorClientID[:], provider.ClientID[:])) {
			return nil, errors.New("provider client ids must be nonzero and unique")
		}
		priorClientID = provider.ClientID
		var err error
		if totalUsage, err = addUint64(totalUsage, provider.UsageBytes); err != nil {
			return nil, err
		}
		provider.ReliabilityPPM = protocol.ReliabilityPPM(provider.Confirmations, provider.Assignments, in.ReliabilityAMin)
		if provider.Eligible && !provider.HeadExcluded && provider.Coldkey != ([32]byte{}) {
			if eligibleUsage, err = addUint64(eligibleUsage, provider.UsageBytes); err != nil {
				return nil, err
			}
		}
		allocations = append(allocations, protocol.ProviderAllocation{ClientID: provider.ClientID, Coldkey: provider.Coldkey, UsageBytes: provider.UsageBytes, ReliabilityPPM: provider.ReliabilityPPM, Eligible: provider.Eligible, HeadExcluded: provider.HeadExcluded})
	}
	shares, err := protocol.AllocateShares(allocations)
	if err != nil {
		if !errors.Is(err, protocol.ErrNoEligibleProviders) {
			return nil, err
		}
		shares = nil
	}
	merkleLeaves := make([]merkle.Leaf, len(shares))
	for i, share := range shares {
		merkleLeaves[i] = merkle.PayoutLeaf(share.Coldkey, new(big.Int).SetUint64(share.ShareBPS))
	}
	var tree *merkle.Tree
	if len(merkleLeaves) != 0 {
		tree, err = merkle.NewTree(merkleLeaves)
		if err != nil {
			return nil, err
		}
	}
	leaves := make([]Leaf, len(shares))
	for i, share := range shares {
		proof, proofErr := tree.Proof(merkleLeaves[i])
		if proofErr != nil {
			return nil, proofErr
		}
		leaves[i] = Leaf{Index: uint64(i), ClientID: share.ClientID, Coldkey: share.Coldkey, ShareBPS: share.ShareBPS, Proof: proof}
	}
	createdAt := in.CreatedAt.UTC()
	if createdAt.IsZero() {
		return nil, errors.New("created_at is required")
	}
	root, sharesTotal := [32]byte{}, uint64(0)
	if tree != nil {
		root, sharesTotal = tree.Root(), 10_000
	}
	return &Artifact{
		Schema: Schema, DeploymentID: in.DeploymentID, ChainID: in.ChainID,
		GenesisHash: strings.ToLower(in.GenesisHash), Netuid: in.Netuid,
		Coordinator: in.Coordinator, SettlementVault: in.SettlementVault,
		Epoch: in.Epoch, NoID: in.NoID, PolicyHash: strings.ToLower(in.PolicyHash),
		Start:                Boundary{Number: in.Start.Number, Hash: strings.ToLower(in.Start.Hash)},
		End:                  Boundary{Number: in.End.Number, Hash: strings.ToLower(in.End.Hash)},
		OperatorSnapshotHash: strings.ToLower(in.OperatorSnapshotHash),
		FleetSnapshotHash:    strings.ToLower(in.FleetSnapshotHash),
		ProviderSnapshotHash: SnapshotHash(providers), ReliabilityAMin: in.ReliabilityAMin,
		Providers: providers, Leaves: leaves, PayoutRoot: root,
		TotalUsageBytes: totalUsage, EligibleUsageBytes: eligibleUsage,
		ExcludedUsageBytes: totalUsage - eligibleUsage, SharesTotalBPS: sharesTotal,
		CreatedAt: createdAt.Format(time.RFC3339Nano),
	}, nil
}

func addUint64(left, right uint64) (uint64, error) {
	if right > ^uint64(0)-left {
		return 0, errors.New("payout artifact usage overflows uint64")
	}
	return left + right, nil
}

// IsDigest reports whether value is a canonical-sized hex digest with prefix.
func IsDigest(value, prefix string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil && len(raw) == sha256.Size
}

// SnapshotHash returns the canonical JSON sha256 identity of value.
func SnapshotHash(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	hash := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(hash[:])
}

func unsignedBytes(artifact *Artifact) ([]byte, error) {
	copy := *artifact
	copy.ContentHash = ""
	copy.Signature = ""
	copy.Signer = common.Address{}
	return json.Marshal(copy)
}

// Sign attaches the artifact signer's recoverable signature and content hash.
func Sign(artifact *Artifact, key *ecdsa.PrivateKey) error {
	if artifact == nil || key == nil {
		return errors.New("artifact or signer is nil")
	}
	b, err := unsignedBytes(artifact)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(b)
	signature, err := crypto.Sign(hash[:], key)
	if err != nil {
		return err
	}
	artifact.Signer = crypto.PubkeyToAddress(key.PublicKey)
	artifact.ContentHash = "sha256:" + hex.EncodeToString(hash[:])
	artifact.Signature = "0x" + hex.EncodeToString(signature)
	return nil
}

// Verify reconstructs every derived field and verifies the content identity
// and recoverable signature. Re-signing a false summary is therefore rejected.
func Verify(artifact *Artifact) error {
	if artifact == nil || artifact.Schema != Schema || artifact.DeploymentID == "" || artifact.ChainID == 0 || artifact.Netuid == 0 || artifact.Coordinator == (common.Address{}) || artifact.SettlementVault == (common.Address{}) || artifact.NoID == 0 || artifact.ReliabilityAMin == 0 || artifact.End.Number < artifact.Start.Number || !IsDigest(artifact.GenesisHash, "0x") || !IsDigest(artifact.PolicyHash, "0x") || !IsDigest(artifact.Start.Hash, "0x") || !IsDigest(artifact.End.Hash, "0x") || !IsDigest(artifact.OperatorSnapshotHash, "sha256:") || !IsDigest(artifact.FleetSnapshotHash, "sha256:") || !IsDigest(artifact.ProviderSnapshotHash, "sha256:") || !IsDigest(artifact.ContentHash, "sha256:") {
		return errors.New("invalid artifact schema/hash")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, artifact.CreatedAt)
	if err != nil || createdAt.Location() != time.UTC || createdAt.Format(time.RFC3339Nano) != artifact.CreatedAt {
		return errors.New("artifact created_at is not canonical UTC RFC3339Nano")
	}
	b, err := unsignedBytes(artifact)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(b)
	if artifact.ContentHash != "sha256:"+hex.EncodeToString(hash[:]) {
		return errors.New("artifact content hash mismatch")
	}
	signature, err := hex.DecodeString(strings.TrimPrefix(artifact.Signature, "0x"))
	if err != nil || len(signature) != crypto.SignatureLength {
		return errors.New("invalid artifact signature")
	}
	publicKey, err := crypto.SigToPub(hash[:], signature)
	if err != nil || crypto.PubkeyToAddress(*publicKey) != artifact.Signer {
		return errors.New("artifact signer mismatch")
	}
	if SnapshotHash(artifact.Providers) != strings.ToLower(artifact.ProviderSnapshotHash) {
		return errors.New("artifact provider snapshot hash mismatch")
	}
	rebuilt, err := Build(BuildInput{
		DeploymentID: artifact.DeploymentID, GenesisHash: artifact.GenesisHash,
		PolicyHash: artifact.PolicyHash, ChainID: artifact.ChainID, Netuid: artifact.Netuid,
		Coordinator: artifact.Coordinator, SettlementVault: artifact.SettlementVault,
		Epoch: artifact.Epoch, NoID: artifact.NoID, Start: artifact.Start, End: artifact.End,
		OperatorSnapshotHash: artifact.OperatorSnapshotHash, FleetSnapshotHash: artifact.FleetSnapshotHash,
		Providers: artifact.Providers, ReliabilityAMin: artifact.ReliabilityAMin, CreatedAt: createdAt,
	})
	if err != nil {
		return fmt.Errorf("rebuild artifact: %w", err)
	}
	if rebuilt.ProviderSnapshotHash != strings.ToLower(artifact.ProviderSnapshotHash) || rebuilt.PayoutRoot != artifact.PayoutRoot || rebuilt.TotalUsageBytes != artifact.TotalUsageBytes || rebuilt.EligibleUsageBytes != artifact.EligibleUsageBytes || rebuilt.ExcludedUsageBytes != artifact.ExcludedUsageBytes || rebuilt.SharesTotalBPS != artifact.SharesTotalBPS || !reflect.DeepEqual(rebuilt.Providers, artifact.Providers) || !reflect.DeepEqual(rebuilt.Leaves, artifact.Leaves) {
		return errors.New("artifact summary, providers, leaves, or proofs do not reconstruct")
	}
	return nil
}

// Bytes verifies and returns the canonical JSON representation.
func Bytes(artifact *Artifact) ([]byte, error) {
	if err := Verify(artifact); err != nil {
		return nil, err
	}
	return json.Marshal(artifact)
}

// Decode accepts only the exact canonical JSON emitted by Bytes. Rejecting
// unknown fields, trailing values, and alternate whitespace prevents a blob
// from having more than one byte representation for one content identity.
func Decode(value []byte) (*Artifact, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var artifact Artifact
	if err := decoder.Decode(&artifact); err != nil {
		return nil, fmt.Errorf("decode payout artifact: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode payout artifact: trailing JSON value")
		}
		return nil, fmt.Errorf("decode payout artifact trailer: %w", err)
	}
	canonical, err := Bytes(&artifact)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, value) {
		return nil, errors.New("payout artifact is not canonical JSON")
	}
	return &artifact, nil
}
