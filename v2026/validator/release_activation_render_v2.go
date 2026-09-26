//go:build linux || darwin

// One renderer builds the signed activation record and the exact five
// runtime input files (activation payload, both signatures, context, history)
// for one validator/operator pair. The validator binary (`validator
// activate`) and the sim-testnet harness both call it, so neither can fork
// the byte layout the production loader authenticates.
package validator

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// ReleaseActivationDeploymentV2 is the independently authenticated deployment
// identity every activation binds. Nothing here is copied from a candidate.
type ReleaseActivationDeploymentV2 struct {
	DeploymentID    string
	ChainID         uint64
	GenesisHash     [32]byte
	Netuid          uint16
	Coordinator     [20]byte
	SettlementVault [20]byte
	PolicyHash      [32]byte
}

// Domain names the activation domain for one activation epoch.
func (self ReleaseActivationDeploymentV2) Domain(epoch uint64) protocol.ValidatorEvidenceActivationDomain {
	return protocol.ValidatorEvidenceActivationDomain{
		ChainID: self.ChainID, GenesisHash: self.GenesisHash, Netuid: self.Netuid,
		Coordinator: self.Coordinator, SettlementVault: self.SettlementVault,
		DeploymentIDHash: sha256.Sum256([]byte(self.DeploymentID)), PolicyHash: self.PolicyHash, Epoch: epoch,
	}
}

// ReleaseActivationSnapshotV2 is the pair of finalized clocks an activation
// names: the next settlement epoch plus the native and EVM snapshots read
// while preparing it.
type ReleaseActivationSnapshotV2 struct {
	Epoch       uint64
	NativeBlock uint64
	NativeHash  [32]byte
	EVMBlock    uint64
	EVMHash     [32]byte
}

// BuildFreshReleaseActivationV2 builds the activation of a source with no
// retained history: first_sequence 1 and a zero prior root. A migration
// prefix from retained public history is a separate, authenticated input and
// is never invented here.
func BuildFreshReleaseActivationV2(deployment ReleaseActivationDeploymentV2, snapshot ReleaseActivationSnapshotV2, hotkey [32]byte, noID uint64, vpk [32]byte) (protocol.ValidatorEvidenceActivation, error) {
	activation := protocol.ValidatorEvidenceActivation{
		Domain: deployment.Domain(snapshot.Epoch), Hotkey: hotkey, NoID: noID, VPK: vpk, FirstSequence: 1,
		NativeBlock: snapshot.NativeBlock, NativeHash: snapshot.NativeHash, EVMBlock: snapshot.EVMBlock, EVMHash: snapshot.EVMHash,
	}
	if err := activation.Validate(); err != nil {
		return protocol.ValidatorEvidenceActivation{}, err
	}
	return activation, nil
}

// SignReleaseActivationV2 signs one activation with its operator-scoped VPK
// (ed25519) and the validator hotkey (sr25519), and checks both signatures
// before returning them.
func SignReleaseActivationV2(activation protocol.ValidatorEvidenceActivation, hotkey *crv4.Keypair, vpkKey ed25519.PrivateKey) ([]byte, []byte, error) {
	if hotkey == nil || hotkey.PublicKey() != activation.Hotkey {
		return nil, nil, errors.New("activation hotkey differs from the signing hotkey")
	}
	vpkSignature, err := activation.SignVPK(vpkKey)
	if err != nil {
		return nil, nil, err
	}
	digest, err := activation.Digest()
	if err != nil {
		return nil, nil, err
	}
	hotkeySignature, err := hotkey.Sign(digest[:])
	if err != nil {
		return nil, nil, err
	}
	if err := activation.Verify(activation, vpkSignature, hotkeySignature); err != nil {
		return nil, nil, err
	}
	return vpkSignature, hotkeySignature, nil
}

// ReleaseActivationMemberV2 is one signed activation for one operator, with
// the validator UID observed at the activation's native snapshot.
type ReleaseActivationMemberV2 struct {
	NoID            uint64                               `json:"no_id"`
	ValidatorUID    uint16                               `json:"validator_uid"`
	Activation      protocol.ValidatorEvidenceActivation `json:"activation"`
	VPKSignature    []byte                               `json:"vpk_signature"`
	HotkeySignature []byte                               `json:"hotkey_signature"`
}

// ReleaseActivationBoundaryV2 is the common finalized initial boundary every
// rendered context pins: at or after the activation epoch's start and after
// every member's publication.
type ReleaseActivationBoundaryV2 struct {
	Block uint64
	Hash  [32]byte
}

// ReleaseActivationBoundaryBlockV2 selects that boundary from the epoch
// geometry and the members' publication heights. It refuses a boundary at or
// past the epoch end, past finality, or not after the EVM snapshot.
func ReleaseActivationBoundaryBlockV2(start, end, evmSnapshot, finalized uint64, published []uint64) (uint64, error) {
	if start == 0 || end <= start {
		return 0, errors.New("activation epoch geometry is invalid")
	}
	boundary := start
	for _, block := range published {
		if block == 0 {
			return 0, errors.New("activation publication height is zero")
		}
		boundary = max(boundary, block)
	}
	if boundary >= end || boundary > finalized || boundary <= evmSnapshot {
		return 0, errors.New("all activations must be finalized before their common initial epoch boundary")
	}
	return boundary, nil
}

// ReleaseEvidenceV2ReferenceLimit is each role's independent byte bound, in
// the Files() order: activation payload, VPK signature, hotkey signature,
// context, history. Role widths are protocol constants; variable inputs keep
// the caller's bounds.
func ReleaseEvidenceV2ReferenceLimit(bounds ReleaseEvidenceV2Bounds, index int) uint64 {
	switch index {
	case 0:
		return uint64(protocol.ValidatorEvidenceActivationPayloadSize)
	case 1, 2:
		return 64
	case 3:
		return bounds.Cut.MaxHeaderBytes
	case 4:
		return bounds.MaxHistoryBytes
	default:
		return 0
	}
}

// ReleaseEvidenceV2OperatorPaths names one operator's five input files (in
// Files() order) and its two scratch roots.
type ReleaseEvidenceV2OperatorPaths struct {
	Files             [5]string
	ReplayScratchRoot string
	SealScratchRoot   string
}

// ReleaseEvidenceV2FileNames are the fixed role file names.
var ReleaseEvidenceV2FileNames = [5]string{"activation.payload", "vpk.signature", "hotkey.signature", "context.json", "history.json"}

// DefaultReleaseEvidenceV2OperatorPaths lays one operator's inputs out under
// root as evidence-v2/no-<id>/<role> and scratch-v2/no-<id>/{replay,seal}, the
// same shape the harness renders. Root must be outside the validator's own
// state directory and every operator state directory.
func DefaultReleaseEvidenceV2OperatorPaths(root string, noID uint64) ReleaseEvidenceV2OperatorPaths {
	var paths ReleaseEvidenceV2OperatorPaths
	input := filepath.Join(root, "evidence-v2", fmt.Sprintf("no-%d", noID))
	for index, name := range ReleaseEvidenceV2FileNames {
		paths.Files[index] = filepath.Join(input, name)
	}
	scratch := filepath.Join(root, "scratch-v2", fmt.Sprintf("no-%d", noID))
	paths.ReplayScratchRoot = filepath.Join(scratch, "replay")
	paths.SealScratchRoot = filepath.Join(scratch, "seal")
	return paths
}

// ReleaseActivationLedgerV2 names the attempt-ledger identity the rendered
// context carries beyond the activation itself.
type ReleaseActivationLedgerV2 struct {
	DeploymentID string
	ChainID      uint64
	GenesisHash  string
	Netuid       uint16
	ValidatorID  uint64
}

// RenderReleaseActivationInputsV2 builds the exact five files for one source
// without touching the filesystem, and the operator config entry that pins
// them. Every byte string is bounded by its role limit; the caller writes the
// returned bytes with WriteReleaseEvidenceV2File.
func RenderReleaseActivationInputsV2(ledger ReleaseActivationLedgerV2, member ReleaseActivationMemberV2, journal [20]byte, runtimeHash [32]byte, boundary ReleaseActivationBoundaryV2, bounds ReleaseEvidenceV2Bounds, paths ReleaseEvidenceV2OperatorPaths) (ReleaseEvidenceV2OperatorConfig, map[string][]byte, error) {
	domain, err := member.Activation.EvidenceDomain()
	if err != nil {
		return ReleaseEvidenceV2OperatorConfig{}, nil, err
	}
	value := ReleaseEvidenceV2ActivationContext{Schema: ReleaseEvidenceV2ActivationContextSchema, Activation: member.Activation,
		InitialCut: AttemptCutV2Context{Identity: AttemptLedgerIdentity{DeploymentID: ledger.DeploymentID, ChainID: ledger.ChainID, GenesisHash: ledger.GenesisHash, Netuid: ledger.Netuid, ValidatorID: ledger.ValidatorID, ValidatorUID: member.ValidatorUID, NoID: member.NoID, ValidatorVPK: attemptHex32(member.Activation.VPK)},
			Activation:    AttemptCutV2Activation{Domain: domain, Hotkey: member.Activation.Hotkey, FirstSequence: member.Activation.FirstSequence, PriorRoot: attemptHex32(member.Activation.PriorRoot)},
			Boundary:      AttemptBoundary{SettlementEpoch: member.Activation.Domain.Epoch, EVMBlock: boundary.Block, EVMBlockHash: common.Hash(boundary.Hash).Hex()},
			FirstSequence: member.Activation.FirstSequence, EgressFirstSequence: member.Activation.FirstSequence, EgressGeneration: 1, PriorRoot: attemptHex32(member.Activation.PriorRoot)},
		ValidatorUID: member.ValidatorUID, Journal: journal, RuntimeHash: runtimeHash, ObservedEVMBlock: boundary.Block, ObservedEVMHash: boundary.Hash}
	contextBytes, err := value.CanonicalJSON(bounds.Cut.MaxHeaderBytes)
	if err != nil {
		return ReleaseEvidenceV2OperatorConfig{}, nil, err
	}
	historyBytes, err := (ReleaseEvidenceV2ActivationHistory{Schema: ReleaseEvidenceV2ActivationHistorySchema, LegacyClosures: [][]byte{}}).CanonicalJSON(bounds.MaxHistoryBytes)
	if err != nil {
		return ReleaseEvidenceV2OperatorConfig{}, nil, err
	}
	activationBytes, err := member.Activation.Payload()
	if err != nil {
		return ReleaseEvidenceV2OperatorConfig{}, nil, err
	}
	if err := member.Activation.Verify(member.Activation, member.VPKSignature, member.HotkeySignature); err != nil {
		return ReleaseEvidenceV2OperatorConfig{}, nil, err
	}
	inputs := map[string][]byte{}
	var files [5]ReleaseEvidenceV2File
	for index, data := range [][]byte{activationBytes, member.VPKSignature, member.HotkeySignature, contextBytes, historyBytes} {
		if paths.Files[index] == "" {
			return ReleaseEvidenceV2OperatorConfig{}, nil, errors.New("evidence input path is empty")
		}
		if uint64(len(data)) > ReleaseEvidenceV2ReferenceLimit(bounds, index) {
			return ReleaseEvidenceV2OperatorConfig{}, nil, errors.New("evidence setup fixed input exceeds its explicit bound")
		}
		inputs[paths.Files[index]] = slices.Clone(data)
		files[index] = ReleaseEvidenceV2File{Path: paths.Files[index], Bytes: uint64(len(data)), SHA256: attemptHex32(sha256.Sum256(data))}
	}
	return ReleaseEvidenceV2OperatorConfig{NoID: member.NoID, Activation: files[0], VPKSignature: files[1], HotkeySignature: files[2], Context: files[3], History: files[4], ReplayScratchRoot: paths.ReplayScratchRoot, SealScratchRoot: paths.SealScratchRoot}, inputs, nil
}
