package validator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urfoundation/sn/crv4"
)

const steeringIntentSchema = "urnetwork-validator-steering-intent-v4"

var (
	ErrSteeringIntentPending = errors.New("a steering intent for this subnet epoch is already pending")
	ErrSteeringAlreadyFinal  = errors.New("weights for this subnet epoch are already finalized")
)

type RationalJSON struct {
	Numerator   string `json:"numerator"`
	Denominator string `json:"denominator"`
}

type StaleHeadBinding struct {
	NoID      uint64 `json:"no_id"`
	ClientID  string `json:"client_id"`
	RecordUID uint16 `json:"record_uid"`
	LiveUID   uint16 `json:"live_uid"`
	Found     bool   `json:"found"`
}

type SteeringIntent struct {
	Schema               string                   `json:"schema"`
	ValidatorID          uint64                   `json:"validator_id"`
	Netuid               uint16                   `json:"netuid"`
	SubnetEpoch          uint64                   `json:"subnet_epoch"`
	NativeSnapshotBlock  uint64                   `json:"native_snapshot_block"`
	NativeSnapshotHash   string                   `json:"native_snapshot_hash"`
	EVMSnapshotBlock     uint64                   `json:"evm_snapshot_block"`
	EVMSnapshotHash      string                   `json:"evm_snapshot_hash"`
	SettlementEpoch      uint64                   `json:"settlement_epoch"`
	PolicyHash           string                   `json:"policy_hash"`
	SelfUID              uint16                   `json:"self_uid"`
	MaskedUIDs           []uint16                 `json:"masked_uids"`
	EligibleHeadUIDs     []uint16                 `json:"eligible_head_uids"`
	SelectedHeadUIDs     []uint16                 `json:"selected_head_uids"`
	RejectedHeadUIDs     []uint16                 `json:"rejected_head_uids"`
	StaleHeadBindings    []StaleHeadBinding       `json:"stale_head_bindings"`
	DepositAudits        []DepositAudit           `json:"deposit_audits"`
	UIDs                 []uint16                 `json:"uids"`
	Scores               []RationalJSON           `json:"scores"`
	Prepared             *crv4.PreparedSubmission `json:"prepared"`
	Values               []uint16                 `json:"values,omitempty"`
	VectorHash           string                   `json:"vector_hash"`
	Status               string                   `json:"status"`
	CreatedAt            string                   `json:"created_at"`
	UpdatedAt            string                   `json:"updated_at"`
	ExtrinsicHash        string                   `json:"extrinsic_hash,omitempty"`
	FinalizedBlock       uint64                   `json:"finalized_block,omitempty"`
	FinalizedBlockHash   string                   `json:"finalized_block_hash,omitempty"`
	RevealBlock          uint64                   `json:"reveal_block,omitempty"`
	ApplicationBlock     uint64                   `json:"application_block,omitempty"`
	ApplicationBlockHash string                   `json:"application_block_hash,omitempty"`
	Error                string                   `json:"error,omitempty"`
}

type steeringIntentFile struct {
	Schema  string           `json:"schema"`
	Current *SteeringIntent  `json:"current,omitempty"`
	History []SteeringIntent `json:"history"`
}

type IntentStore struct {
	mu   sync.Mutex
	path string
}

func NewIntentStore(stateDir string) (*IntentStore, error) {
	if !filepath.IsAbs(stateDir) {
		return nil, errors.New("intent state directory must be absolute")
	}
	if err := ensurePrivateStateDir(stateDir); err != nil {
		return nil, err
	}
	return &IntentStore{path: filepath.Join(stateDir, "steering-intents.json")}, nil
}

func rationalJSON(scores []*big.Rat) ([]RationalJSON, error) {
	out := make([]RationalJSON, len(scores))
	for i, score := range scores {
		if score == nil || score.Sign() < 0 {
			return nil, fmt.Errorf("score %d is nil or negative", i)
		}
		out[i] = RationalJSON{Numerator: score.Num().String(), Denominator: score.Denom().String()}
	}
	return out, nil
}

func (i *SteeringIntent) computeVectorHash() (string, error) {
	clone := *i
	clone.VectorHash = ""
	clone.Status = ""
	clone.CreatedAt = ""
	clone.UpdatedAt = ""
	clone.ExtrinsicHash = ""
	clone.Values = nil
	clone.FinalizedBlock = 0
	clone.FinalizedBlockHash = ""
	clone.RevealBlock = 0
	clone.ApplicationBlock = 0
	clone.ApplicationBlockHash = ""
	clone.Error = ""
	b, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return "0x" + hex.EncodeToString(h[:]), nil
}

// ReconstructedVectorHash returns the canonical immutable intent hash without
// trusting the stored VectorHash field. It is useful to independent evidence
// tooling and deterministic cross-package fixtures.
func (i *SteeringIntent) ReconstructedVectorHash() (string, error) {
	if i == nil {
		return "", errors.New("steering intent is nil")
	}
	return i.computeVectorHash()
}

// VerifyVectorHash proves that the immutable decision evidence still matches
// the hash submitted with this intent. Release evidence readers use it so a
// changed audit, mask, score, snapshot, or prepared transaction cannot remain
// hidden behind an old vector hash.
func (i *SteeringIntent) VerifyVectorHash() error {
	if i == nil || i.VectorHash == "" {
		return errors.New("steering intent has no vector hash")
	}
	want, err := i.ReconstructedVectorHash()
	if err != nil {
		return err
	}
	if !strings.EqualFold(i.VectorHash, want) {
		return fmt.Errorf("steering intent vector hash %s, reconstructed %s", i.VectorHash, want)
	}
	return nil
}

func (s *IntentStore) readLocked() (*steeringIntentFile, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return &steeringIntentFile{Schema: steeringIntentSchema}, nil
	}
	if err != nil {
		return nil, err
	}
	var f steeringIntentFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("decode %s: %w", s.path, err)
	}
	if f.Schema != steeringIntentSchema {
		return nil, fmt.Errorf("unsupported intent store schema %q", f.Schema)
	}
	return &f, nil
}

func (s *IntentStore) writeLocked(f *steeringIntentFile) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicStateWrite(s.path, append(b, '\n'), 0o600)
}

func (s *IntentStore) Begin(intent SteeringIntent) (*SteeringIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	if intent.Prepared == nil {
		return nil, errors.New("steering intent has no exact prepared submission")
	}
	if _, err := intent.Prepared.Validate(); err != nil {
		return nil, fmt.Errorf("steering intent prepared submission: %w", err)
	}
	if intent.Prepared.Netuid != intent.Netuid || intent.Prepared.SubnetEpoch != intent.SubnetEpoch || !slices.Equal(intent.Prepared.UIDs, intent.UIDs) {
		return nil, errors.New("steering intent does not match prepared submission")
	}
	if len(intent.DepositAudits) == 0 {
		return nil, errors.New("steering intent has no operator deposit audits")
	}
	for index, audit := range intent.DepositAudits {
		if audit.NoID == 0 || audit.Epoch != intent.SettlementEpoch || audit.Status == "" || audit.Disposition == "" || (index > 0 && audit.NoID <= intent.DepositAudits[index-1].NoID) {
			return nil, errors.New("steering intent deposit audits are incomplete or non-canonical")
		}
	}
	if f.Current != nil && f.Current.SubnetEpoch == intent.SubnetEpoch {
		switch f.Current.Status {
		case "pending":
			return nil, ErrSteeringIntentPending
		case "finalized", "applied":
			return nil, ErrSteeringAlreadyFinal
		}
	}
	if f.Current != nil {
		f.History = append(f.History, *f.Current)
		if len(f.History) > 256 {
			f.History = append([]SteeringIntent(nil), f.History[len(f.History)-256:]...)
		}
	}
	intent.Schema = steeringIntentSchema
	intent.Status = "pending"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	intent.CreatedAt, intent.UpdatedAt = now, now
	intent.VectorHash, err = intent.computeVectorHash()
	if err != nil {
		return nil, err
	}
	f.Current = &intent
	if err := s.writeLocked(f); err != nil {
		return nil, err
	}
	copy := intent
	return &copy, nil
}

func (s *IntentStore) update(vectorHash, status string, mutate func(*SteeringIntent) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readLocked()
	if err != nil {
		return err
	}
	if f.Current == nil || f.Current.VectorHash != vectorHash {
		return errors.New("steering intent does not match current vector")
	}
	if mutate != nil {
		if err := mutate(f.Current); err != nil {
			return err
		}
	}
	f.Current.Status = status
	f.Current.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return s.writeLocked(f)
}

func (s *IntentStore) MarkFinalized(vectorHash, extrinsicHash string, finalizedBlock uint64, finalizedHash string, revealBlock uint64, values []uint16) error {
	return s.update(vectorHash, "finalized", func(i *SteeringIntent) error {
		if i.Prepared == nil || !strings.EqualFold(extrinsicHash, i.Prepared.ExtrinsicHash) || finalizedBlock == 0 || finalizedHash == "" || revealBlock != i.Prepared.RevealBlock || !slices.Equal(values, i.Prepared.Values) {
			return errors.New("finalized receipt does not match exact prepared submission")
		}
		i.ExtrinsicHash = extrinsicHash
		i.FinalizedBlock = finalizedBlock
		i.FinalizedBlockHash = finalizedHash
		i.RevealBlock = revealBlock
		i.Values = append([]uint16(nil), values...)
		i.Error = ""
		return nil
	})
}

func (s *IntentStore) MarkApplied(vectorHash string, block uint64, blockHash string) error {
	return s.update(vectorHash, "applied", func(i *SteeringIntent) error {
		if i.Status != "finalized" || block < i.RevealBlock || blockHash == "" {
			return errors.New("application receipt precedes finalized reveal")
		}
		i.ApplicationBlock = block
		i.ApplicationBlockHash = blockHash
		return nil
	})
}

func (s *IntentStore) MarkFailed(vectorHash string, failure error) error {
	return s.update(vectorHash, "failed", func(i *SteeringIntent) error {
		if failure != nil {
			i.Error = failure.Error()
		}
		return nil
	})
}

func (s *IntentStore) Current() (*SteeringIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readLocked()
	if err != nil || f.Current == nil {
		return nil, err
	}
	copy := *f.Current
	return &copy, nil
}
