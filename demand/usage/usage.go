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
//
// An epoch whose committed header carries an extender component (WHITEPAPER.md
// §8.6.4, from E_x, the first epoch of the extender pool amendment) has a version 2
// statement instead, schema urnetwork-epoch-usage-v2 under the evidence kind
// epoch-usage-v2. It holds every version 1 field and the epoch's extender pool
// (ExtenderPool), and its TotalUsageBytes is the provider rows' usage plus all of
// Z, the extender pool's bytes before the cap, which the deposit prices like any
// usage. Every other epoch keeps its version 1 statement, byte for byte.
package usage

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/ledger"
	"github.com/urfoundation/sn/protocol"
)

const (
	// Schema names the statement format.
	Schema = "urnetwork-epoch-usage-v1"
	// EvidenceKind is the kind it is published under on the operator's /sn/evidence
	// routes, with RunID as its run.
	EvidenceKind = "epoch-usage-v1"
	// SchemaV2 names the format of an epoch with an extender component
	// (WHITEPAPER.md §8.6.4): version 1's fields and its extender pool.
	SchemaV2 = "urnetwork-epoch-usage-v2"
	// EvidenceKindV2 is the kind a version 2 statement is published under, with
	// RunID as its run, as version 1's is.
	EvidenceKindV2 = "epoch-usage-v2"
	// MaxBytes bounds a statement: its chunk lists are the only parts that grow.
	MaxBytes = 1 << 20
)

// KindOf is the evidence kind of an epoch's statement: EvidenceKindV2 when its
// committed header carries an extender component, else EvidenceKind.
func KindOf(extenders bool) string {
	if extenders {
		return EvidenceKindV2
	}
	return EvidenceKind
}

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
	// artifact's total_usage_bytes does. In a version 2 statement it adds all of Z,
	// the extender pool's bytes before the cap (WHITEPAPER.md §8.6.4).
	TotalUsageBytes uint64 `json:"total_usage_bytes"`
	// TotalUsers counts distinct top-level clients with contract usage in the epoch
	// window, as a payout artifact's total_users does.
	TotalUsers uint64 `json:"total_users"`
	// ExtenderPool is a version 2 statement's extender pool, written after
	// total_users, and nil in a version 1 statement, which it leaves byte for byte
	// as it was.
	*ExtenderPool
	Signer    common.Address `json:"signer"`
	Signature string         `json:"signature,omitempty"`
}

// ExtenderPool is what a version 2 statement adds (WHITEPAPER.md §8.6.4): the
// epoch's extender pool, in the order it is written. Z and its parts, and the
// contracts and payer networks behind them, are the operator's attestation (M4).
// The rest is what the committed header's extender component holds: Z, its census
// and share-row chunks, and, from those chunks, N, M and Z'.
type ExtenderPool struct {
	// IntraPlatformBytes and IntraNetworkBytes are Z's parts: the funded shares of
	// the operator's platform endpoints, and those on each contract's origin
	// network.
	IntraPlatformBytes uint64 `json:"intra_platform_bytes"`
	IntraNetworkBytes  uint64 `json:"intra_network_bytes"`
	// ExtenderPoolBytes is Z, their sum, before the cap: the header's
	// extenders.bytes.
	ExtenderPoolBytes uint64 `json:"extender_pool_bytes"`
	// ExtenderPoolWeightBytes is Z', what Z entered the allocation as: min(Z, W_k),
	// and 0 with no eligible extender. The share rows sum to it.
	ExtenderPoolWeightBytes uint64 `json:"extender_pool_weight_bytes"`
	// ExtenderPoolContracts and ExtenderPoolPayers count the contracts behind Z and
	// their distinct payer networks.
	ExtenderPoolContracts uint64 `json:"extender_pool_contracts"`
	ExtenderPoolPayers    uint64 `json:"extender_pool_payers"`
	// ExtenderCensus lists the census chunks, as the header lists them; [] for a
	// census with no rows.
	ExtenderCensus []string `json:"extender_census"`
	// ExtenderUnits is M, the units that hold an eligible extender, and
	// EligibleExtenders is N, the census rows that are eligible.
	ExtenderUnits     uint64 `json:"extender_units"`
	EligibleExtenders uint64 `json:"eligible_extenders"`
	// ExtenderChunks lists the share-row chunks, as the header lists them; []
	// when no extender is eligible or Z is 0.
	ExtenderChunks []string `json:"extender_chunks"`
}

// Kind is the evidence kind the statement is published under.
func (s *Statement) Kind() string {
	return KindOf(s.ExtenderPool != nil)
}

// validate checks the relations a version 2 statement's extender pool holds by
// construction: Z is the sum of its parts, the weight is at most Z and is 0 with no
// eligible extender, every eligible extender is in one of at least one unit, the
// share rows exist exactly when the weight is positive, the contracts and payers
// behind Z exist exactly when Z is positive, and the total holds Z.
func (p *ExtenderPool) validate(total uint64) error {
	switch {
	case p.IntraPlatformBytes > math.MaxUint64-p.IntraNetworkBytes || p.ExtenderPoolBytes != p.IntraPlatformBytes+p.IntraNetworkBytes:
		return errors.New("usage: extender_pool_bytes is not intra_platform_bytes plus intra_network_bytes")
	case p.ExtenderPoolWeightBytes > p.ExtenderPoolBytes:
		return errors.New("usage: extender_pool_weight_bytes exceeds extender_pool_bytes")
	case p.ExtenderCensus == nil || p.ExtenderChunks == nil:
		return errors.New("usage: extender_census and extender_chunks are lists, [] when empty")
	case p.ExtenderUnits > p.EligibleExtenders || (p.EligibleExtenders != 0) != (p.ExtenderUnits != 0):
		return errors.New("usage: extender_units does not fit eligible_extenders")
	case p.EligibleExtenders == 0 && p.ExtenderPoolWeightBytes != 0:
		return errors.New("usage: extender_pool_weight_bytes is positive with no eligible extender")
	case (len(p.ExtenderChunks) != 0) != (p.ExtenderPoolWeightBytes != 0):
		return errors.New("usage: extender_chunks are listed exactly when extender_pool_weight_bytes is positive")
	case (p.ExtenderPoolContracts != 0) != (p.ExtenderPoolBytes != 0) || p.ExtenderPoolPayers > p.ExtenderPoolContracts || (p.ExtenderPoolPayers != 0) != (p.ExtenderPoolContracts != 0):
		return errors.New("usage: extender_pool_contracts and extender_pool_payers do not fit extender_pool_bytes")
	case total < p.ExtenderPoolBytes:
		return errors.New("usage: total_usage_bytes does not hold extender_pool_bytes")
	}
	for _, refs := range [][]string{p.ExtenderCensus, p.ExtenderChunks} {
		for _, text := range refs {
			if _, err := ledger.ParseRef(text); err != nil {
				return fmt.Errorf("usage: extender chunk %q is not a reference", text)
			}
		}
	}
	return nil
}

// schema is the schema the statement's fields call for.
func (s *Statement) schema() string {
	if s.ExtenderPool != nil {
		return SchemaV2
	}
	return Schema
}

func (s *Statement) validate() error {
	if s == nil || s.Schema != s.schema() || s.DeploymentID == "" || s.ChainID == 0 || s.Netuid == 0 || s.NoID == 0 {
		return errors.New("usage: incomplete statement identity, or a schema its fields do not call for")
	}
	if s.Coordinator == (common.Address{}) || s.VaultV2 == (common.Address{}) || s.Signer == (common.Address{}) {
		return errors.New("usage: incomplete statement addresses")
	}
	if s.GenesisHash == "" || s.PolicyHash == "" || s.Start.Hash == "" || s.End.Hash == "" || s.End.Number <= s.Start.Number {
		return errors.New("usage: incomplete statement boundaries")
	}
	if s.ExtenderPool != nil {
		return s.ExtenderPool.validate(s.TotalUsageBytes)
	}
	return nil
}

// signingHash covers every field but the signature, in the struct's stable order:
// a version 2 statement's extender pool between total_users and signer.
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

// Seal sets the schema its fields call for and the signer, and signs with the
// operator's artifact key.
func (s *Statement) Seal(key *ecdsa.PrivateKey) error {
	if key == nil {
		return errors.New("usage: no signing key")
	}
	s.Schema = s.schema()
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

// TotalUsage is an epoch's total_usage_bytes: its provider rows' usage, plus Z, the
// extender pool's bytes before the cap, for an epoch with an extender component.
func TotalUsage(providerBytes, extenderPoolBytes uint64) (uint64, error) {
	if extenderPoolBytes > math.MaxUint64-providerBytes {
		return 0, errors.New("usage: provider rows with the extender pool overflow")
	}
	return providerBytes + extenderPoolBytes, nil
}

// CheckRows requires the statement's bytes and row count to be what the ledger's
// rows really hold: the provider rows' usage, plus Z for a version 2 statement.
func (s *Statement) CheckRows(rows []protocol.LedgerUsageRow) error {
	total, count, err := SumRows(rows)
	if err != nil {
		return err
	}
	return s.CheckSums(total, count)
}

// CheckSums is CheckRows over sums already taken: the provider rows' usage and
// their count.
func (s *Statement) CheckSums(providerBytes, count uint64) error {
	var pool uint64
	if s.ExtenderPool != nil {
		pool = s.ExtenderPoolBytes
	}
	total, err := TotalUsage(providerBytes, pool)
	if err != nil {
		return err
	}
	if total != s.TotalUsageBytes || count != s.ProviderRows {
		return fmt.Errorf("usage: the statement claims %d bytes in %d rows; the ledger holds %d in %d, with %d extender pool bytes", s.TotalUsageBytes, s.ProviderRows, providerBytes, count, pool)
	}
	return nil
}

// Extenders is an epoch's committed extender component as its statement must carry
// it: what the header lists, Z and its chunk lists, and what a verifier reads from
// those chunks, N and M from the census rows and Z' from the share rows.
type Extenders struct {
	Bytes    uint64
	Census   []string
	Chunks   []string
	Eligible uint64
	Units    uint64
	Weight   uint64
}

// CommittedExtenders reads a header's extender component with the rows of its
// census and share-row chunks. census must have passed ledger.CheckCensusRows, and
// shares are the rows its share-row chunks decode to.
func CommittedExtenders(header *ledger.HeaderExtenders, census []ledger.CensusRow, shares []protocol.LedgerUsageRow) (*Extenders, error) {
	if header == nil {
		return nil, errors.New("usage: no extender component")
	}
	committed := &Extenders{Bytes: header.Bytes, Census: refStrings(header.Census), Chunks: refStrings(header.Rows)}
	units := map[[32]byte]bool{}
	for i := range census {
		if census[i].Eligible() {
			committed.Eligible++
			units[census[i].Unit] = true
		}
	}
	committed.Units = uint64(len(units))
	weight, _, err := SumRows(shares)
	if err != nil {
		return nil, err
	}
	committed.Weight = weight
	return committed, nil
}

func refStrings(refs []ledger.Ref) []string {
	out := make([]string, len(refs))
	for i, ref := range refs {
		out[i] = ref.String()
	}
	return out
}

// NewExtenderPool is a version 2 statement's extender pool: Z's parts and the
// contracts and payers behind them, the operator's attestation, with what the
// committed component holds. Z's parts must sum to the component's Z.
func NewExtenderPool(intraPlatform, intraNetwork, contracts, payers uint64, committed *Extenders) (*ExtenderPool, error) {
	if committed == nil {
		return nil, errors.New("usage: no committed extender component")
	}
	pool := &ExtenderPool{
		IntraPlatformBytes: intraPlatform, IntraNetworkBytes: intraNetwork, ExtenderPoolBytes: committed.Bytes,
		ExtenderPoolWeightBytes: committed.Weight, ExtenderPoolContracts: contracts, ExtenderPoolPayers: payers,
		ExtenderCensus: append([]string{}, committed.Census...), ExtenderUnits: committed.Units, EligibleExtenders: committed.Eligible,
		ExtenderChunks: append([]string{}, committed.Chunks...),
	}
	if err := pool.validate(math.MaxUint64); err != nil {
		return nil, err
	}
	return pool, nil
}

// CheckExtenders requires the statement to be the version its epoch's committed
// header calls for, and a version 2 statement's extender pool to carry exactly what
// the committed component holds: Z, the census and share-row chunks in order, N, M
// and Z'. committed is nil for an epoch without an extender component, whose
// statement is version 1. Z's parts, contracts and payers are the operator's
// attestation, which validate holds to Z.
func (s *Statement) CheckExtenders(committed *Extenders) error {
	switch {
	case committed == nil && s.ExtenderPool == nil:
		return nil
	case committed == nil:
		return errors.New("usage: a version 2 statement for an epoch without an extender component")
	case s.ExtenderPool == nil:
		return errors.New("usage: a version 1 statement for an epoch with an extender component")
	}
	pool := s.ExtenderPool
	switch {
	case pool.ExtenderPoolBytes != committed.Bytes:
		return fmt.Errorf("usage: the statement's extender_pool_bytes %d is not the header's %d", pool.ExtenderPoolBytes, committed.Bytes)
	case !slices.Equal(pool.ExtenderCensus, committed.Census):
		return errors.New("usage: the statement's extender_census is not the header's census chunks")
	case !slices.Equal(pool.ExtenderChunks, committed.Chunks):
		return errors.New("usage: the statement's extender_chunks are not the header's share-row chunks")
	case pool.EligibleExtenders != committed.Eligible || pool.ExtenderUnits != committed.Units:
		return fmt.Errorf("usage: the statement counts %d eligible extenders in %d units; the census holds %d in %d", pool.EligibleExtenders, pool.ExtenderUnits, committed.Eligible, committed.Units)
	case pool.ExtenderPoolWeightBytes != committed.Weight:
		return fmt.Errorf("usage: the statement's extender_pool_weight_bytes %d is not the share rows' %d", pool.ExtenderPoolWeightBytes, committed.Weight)
	}
	return nil
}
