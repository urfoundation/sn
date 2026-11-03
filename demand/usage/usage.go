// Package usage is an operator's signed statement of one epoch's usage under
// settlement v2, whose ledger has no signed artifact and no count of users. A
// deposit for an epoch prices the previous epoch's statement (bytes and users), and
// validators audit the deposit against the same statement.
//
// Bytes are checkable: the statement names the epoch's provider chunks, which must
// be those the committed ledger header lists, and their usage rows must sum to
// TotalUsageBytes. Users are the operator's attestation, as they are in a payout
// artifact: the deposit is what backs them. The statement adds to the ledger and
// changes none of its bytes, so a dispute over users can never hold up settlement.
package usage

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/protocol"
)

const (
	// Schema names the statement format.
	Schema = "urnetwork-epoch-usage-v1"
	// EvidenceKind is the kind it is published under on the operator's /sn/evidence
	// routes, with RunID as its run.
	EvidenceKind = "epoch-usage-v1"
	// MaxBytes bounds a statement: its chunk list is the only part that grows.
	MaxBytes = 1 << 20
)

// RunID is the evidence run of one operator's epoch.
func RunID(noID, epoch uint64) string { return fmt.Sprintf("%d-%d", noID, epoch) }

// Boundary is a finalized block.
type Boundary struct {
	Number uint64 `json:"number"`
	Hash   string `json:"hash"`
}

// Statement is one operator's usage for one epoch.
type Statement struct {
	Schema         string         `json:"schema"`
	DeploymentID   string         `json:"deployment_id"`
	ChainID        uint64         `json:"chain_id"`
	GenesisHash    string         `json:"genesis_hash"`
	Netuid         uint16         `json:"netuid"`
	Coordinator    common.Address `json:"coordinator"`
	VaultV2        common.Address `json:"vault_v2"`
	PolicyHash     string         `json:"policy_hash"`
	NoID           uint64         `json:"no_id"`
	Epoch          uint64         `json:"epoch"`
	Start          Boundary       `json:"start"`
	End            Boundary       `json:"end"`
	ProviderChunks []string       `json:"provider_chunks"`
	ProviderRows   uint64         `json:"provider_rows"`
	// TotalUsageBytes sums every provider row, eligible or not, as a payout
	// artifact's total_usage_bytes does.
	TotalUsageBytes uint64 `json:"total_usage_bytes"`
	// TotalUsers counts distinct top-level clients with contract usage in the epoch
	// window, as a payout artifact's total_users does.
	TotalUsers uint64         `json:"total_users"`
	Signer     common.Address `json:"signer"`
	Signature  string         `json:"signature,omitempty"`
}

func (s *Statement) validate() error {
	if s == nil || s.Schema != Schema || s.DeploymentID == "" || s.ChainID == 0 || s.Netuid == 0 || s.NoID == 0 {
		return errors.New("usage: incomplete statement identity")
	}
	if s.Coordinator == (common.Address{}) || s.VaultV2 == (common.Address{}) || s.Signer == (common.Address{}) {
		return errors.New("usage: incomplete statement addresses")
	}
	if s.GenesisHash == "" || s.PolicyHash == "" || s.Start.Hash == "" || s.End.Hash == "" || s.End.Number <= s.Start.Number {
		return errors.New("usage: incomplete statement boundaries")
	}
	return nil
}

// signingHash covers every field but the signature, in the struct's stable order.
func (s *Statement) signingHash() ([]byte, error) {
	unsigned := *s
	unsigned.Signature = ""
	encoded, err := json.Marshal(&unsigned)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	return digest[:], nil
}

// Seal sets the signer and signs with the operator's artifact key.
func (s *Statement) Seal(key *ecdsa.PrivateKey) error {
	if key == nil {
		return errors.New("usage: no signing key")
	}
	s.Schema = Schema
	s.Signer = crypto.PubkeyToAddress(key.PublicKey)
	if err := s.validate(); err != nil {
		return err
	}
	digest, err := s.signingHash()
	if err != nil {
		return err
	}
	signature, err := crypto.Sign(digest, key)
	if err != nil {
		return err
	}
	s.Signature = "0x" + hex.EncodeToString(signature)
	return nil
}

// Verify checks the statement is complete and signed by signer.
func (s *Statement) Verify(signer common.Address) error {
	if err := s.validate(); err != nil {
		return err
	}
	if s.Signer != signer {
		return fmt.Errorf("usage: signed by %s, not %s", s.Signer, signer)
	}
	signature, err := hex.DecodeString(strings.TrimPrefix(s.Signature, "0x"))
	if err != nil || len(signature) != crypto.SignatureLength {
		return errors.New("usage: bad signature")
	}
	digest, err := s.signingHash()
	if err != nil {
		return err
	}
	public, err := crypto.SigToPub(digest, signature)
	if err != nil || crypto.PubkeyToAddress(*public) != signer {
		return errors.New("usage: signature does not match the signer")
	}
	return nil
}

// Bytes is the statement's published encoding.
func (s *Statement) Bytes() ([]byte, error) { return json.Marshal(s) }

// Decode reads a published statement, refusing anything oversized or malformed. It
// does not check the signature; Verify does.
func Decode(raw []byte) (*Statement, error) {
	if len(raw) > MaxBytes {
		return nil, fmt.Errorf("usage: %d bytes exceeds %d", len(raw), MaxBytes)
	}
	statement := &Statement{}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(statement); err != nil {
		return nil, fmt.Errorf("usage: decode: %w", err)
	}
	if decoder.More() {
		return nil, errors.New("usage: trailing data")
	}
	if err := statement.validate(); err != nil {
		return nil, err
	}
	return statement, nil
}

// SumRows totals a ledger's provider rows and counts them. Every row counts,
// eligible or not.
func SumRows(rows []protocol.LedgerUsageRow) (totalBytes uint64, count uint64, err error) {
	for _, row := range rows {
		if row.UsageBytes > math.MaxUint64-totalBytes {
			return 0, 0, errors.New("usage: provider rows overflow")
		}
		totalBytes += row.UsageBytes
		count++
	}
	return totalBytes, count, nil
}

// CheckRows requires the statement's bytes and row count to be what the ledger's
// rows really hold.
func (s *Statement) CheckRows(rows []protocol.LedgerUsageRow) error {
	total, count, err := SumRows(rows)
	if err != nil {
		return err
	}
	if total != s.TotalUsageBytes || count != s.ProviderRows {
		return fmt.Errorf("usage: the statement claims %d bytes in %d rows; the ledger holds %d in %d", s.TotalUsageBytes, s.ProviderRows, total, count)
	}
	return nil
}
