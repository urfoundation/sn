package validator

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/urfoundation/sn/protocol"
)

const (
	headEMASchemaV1 = "urnetwork-validator-head-ema-v1"
	headEMASchemaV2 = "urnetwork-validator-head-ema-v2"
)

type FleetScoreKey struct {
	FleetID    [32]byte
	Hotkey     [32]byte
	Generation uint64
	UID        uint16
}

func (k FleetScoreKey) String() string {
	return fmt.Sprintf("%x:%x:%d:%d", k.FleetID, k.Hotkey, k.Generation, k.UID)
}

type headEMAEntry struct {
	Key         FleetScoreKey `json:"key"`
	Numerator   string        `json:"numerator"`
	Denominator string        `json:"denominator"`
}

type headEMAFile struct {
	Schema          string               `json:"schema"`
	UpdatedAt       string               `json:"updated_at"`
	Entries         []headEMAEntry       `json:"entries"`
	LastSubnetEpoch *uint64              `json:"last_subnet_epoch,omitempty"`
	LastAlpha       *protocol.Rational   `json:"last_alpha,omitempty"`
	LastFold        []HeadEMAMeasurement `json:"last_fold"`
}

type HeadEMAStore struct {
	mu              sync.Mutex
	path            string
	values          map[string]headEMAEntry
	lastSubnetEpoch *uint64
	lastAlpha       *protocol.Rational
	lastFold        []HeadEMAMeasurement
}

// HeadEMAMeasurement records every live raw input and every persisted prior
// identity involved in one exact fold. Missing identities decay in the state
// but cannot enter the live head channel.
type HeadEMAMeasurement struct {
	Key      FleetScoreKey `json:"key"`
	HasRaw   bool          `json:"has_raw"`
	Raw      RationalJSON  `json:"raw"`
	HasPrior bool          `json:"has_prior"`
	Prior    RationalJSON  `json:"prior"`
	Next     RationalJSON  `json:"next"`
}

func NewHeadEMAStore(stateDir string) (*HeadEMAStore, error) {
	if !filepath.IsAbs(stateDir) {
		return nil, errors.New("head EMA state directory must be absolute")
	}
	if err := ensurePrivateStateDir(stateDir); err != nil {
		return nil, err
	}
	s := &HeadEMAStore{path: filepath.Join(stateDir, "head-ema.json"), values: map[string]headEMAEntry{}}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var file headEMAFile
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, err
	}
	if file.Schema != headEMASchemaV1 && file.Schema != headEMASchemaV2 {
		return nil, fmt.Errorf("unsupported head EMA schema %q", file.Schema)
	}
	priorKey := ""
	for _, entry := range file.Entries {
		key := entry.Key.String()
		if priorKey != "" && key <= priorKey {
			return nil, errors.New("persisted head EMA entries are not strictly ordered")
		}
		numerator, numeratorOK := new(big.Int).SetString(entry.Numerator, 10)
		denom, denominatorOK := new(big.Int).SetString(entry.Denominator, 10)
		if !numeratorOK || !denominatorOK || numerator.Sign() <= 0 || denom.Sign() <= 0 {
			return nil, errors.New("invalid persisted head EMA denominator")
		}
		value := new(big.Rat).SetFrac(numerator, denom)
		if entry.Numerator != value.Num().String() || entry.Denominator != value.Denom().String() {
			return nil, errors.New("persisted head EMA value is not canonical")
		}
		s.values[key] = entry
		priorKey = key
	}
	if file.Schema == headEMASchemaV2 {
		if (file.LastSubnetEpoch == nil) != (file.LastAlpha == nil) || (file.LastSubnetEpoch != nil && file.LastFold == nil) {
			return nil, errors.New("persisted head EMA fold metadata is partial")
		}
		if file.LastSubnetEpoch != nil {
			if err := verifyHeadEMAFold(file.LastFold, *file.LastAlpha); err != nil {
				return nil, fmt.Errorf("persisted head EMA fold: %w", err)
			}
			lastValues := map[string]RationalJSON{}
			for _, record := range file.LastFold {
				next, decodeErr := decodeRationalJSON(record.Next)
				if decodeErr != nil {
					return nil, decodeErr
				}
				if next.Sign() > 0 {
					lastValues[record.Key.String()] = record.Next
				}
			}
			if len(lastValues) != len(s.values) {
				return nil, errors.New("persisted head EMA entries differ from the last fold")
			}
			for key, entry := range s.values {
				if lastValues[key] != (RationalJSON{Numerator: entry.Numerator, Denominator: entry.Denominator}) {
					return nil, errors.New("persisted head EMA entry differs from the last fold")
				}
			}
			epoch := *file.LastSubnetEpoch
			alpha := *file.LastAlpha
			s.lastSubnetEpoch = &epoch
			s.lastAlpha = &alpha
			s.lastFold = append([]HeadEMAMeasurement(nil), file.LastFold...)
		}
	}
	return s, nil
}

func entryRat(entry headEMAEntry) *big.Rat {
	n, _ := new(big.Int).SetString(entry.Numerator, 10)
	d, _ := new(big.Int).SetString(entry.Denominator, 10)
	return new(big.Rat).SetFrac(n, d)
}

func (s *HeadEMAStore) saveLocked() error {
	keys := make([]string, 0, len(s.values))
	for key := range s.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	file := headEMAFile{Schema: headEMASchemaV2, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if s.lastSubnetEpoch != nil {
		epoch := *s.lastSubnetEpoch
		alpha := *s.lastAlpha
		file.LastSubnetEpoch = &epoch
		file.LastAlpha = &alpha
		file.LastFold = append([]HeadEMAMeasurement{}, s.lastFold...)
	}
	for _, key := range keys {
		file.Entries = append(file.Entries, s.values[key])
	}
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return atomicStateWrite(s.path, append(b, '\n'), 0o600)
}

// verifyHeadEMAFold independently checks exact arithmetic and canonical order.
func verifyHeadEMAFold(records []HeadEMAMeasurement, alpha protocol.Rational) error {
	if err := alpha.Validate("head_score_ema"); err != nil || alpha.Numerator > alpha.Denominator {
		return errors.New("invalid head EMA policy")
	}
	a := new(big.Rat).SetFrac(new(big.Int).SetUint64(alpha.Numerator), new(big.Int).SetUint64(alpha.Denominator))
	oneMinus := new(big.Rat).Sub(big.NewRat(1, 1), a)
	priorKey := ""
	for index, record := range records {
		key := record.Key.String()
		if priorKey != "" && key <= priorKey {
			return errors.New("head EMA records are not strictly ordered")
		}
		if !record.HasRaw && !record.HasPrior {
			return fmt.Errorf("head EMA record %d has neither raw nor prior state", index)
		}
		raw, err := decodeRationalJSON(record.Raw)
		if err != nil || (!record.HasRaw && raw != nil && raw.Sign() != 0) {
			return fmt.Errorf("head EMA record %d raw value is invalid", index)
		}
		prior, err := decodeRationalJSON(record.Prior)
		if err != nil || (!record.HasPrior && prior != nil && prior.Sign() != 0) || (record.HasPrior && prior.Sign() <= 0) {
			return fmt.Errorf("head EMA record %d prior value is invalid", index)
		}
		next, err := decodeRationalJSON(record.Next)
		if err != nil {
			return fmt.Errorf("head EMA record %d next value is invalid", index)
		}
		want := new(big.Rat)
		if !record.HasPrior {
			want.Set(raw)
		} else {
			want.Add(new(big.Rat).Mul(a, raw), new(big.Rat).Mul(oneMinus, prior))
		}
		if next.Cmp(want) != 0 {
			return fmt.Errorf("head EMA record %d next value does not match the exact fold", index)
		}
		priorKey = key
	}
	return nil
}

// headEMAOutput returns only identities present in the raw live input. Missing
// identities remain persisted while decaying but receive no live weight.
func headEMAOutput(records []HeadEMAMeasurement) (map[uint16]*big.Rat, error) {
	out := map[uint16]*big.Rat{}
	for _, record := range records {
		if !record.HasRaw {
			continue
		}
		next, err := decodeRationalJSON(record.Next)
		if err != nil {
			return nil, err
		}
		if next.Sign() == 0 {
			continue
		}
		if out[record.Key.UID] == nil {
			out[record.Key.UID] = new(big.Rat)
		}
		out[record.Key.UID].Add(out[record.Key.UID], next)
	}
	return out, nil
}

// foldWithLock mutates the exact EMA state and returns its public arithmetic
// transcript. The caller must hold the store state lock.
func (s *HeadEMAStore) foldWithLock(raw map[FleetScoreKey]*big.Rat, alpha protocol.Rational) (map[uint16]*big.Rat, []HeadEMAMeasurement, error) {
	if err := alpha.Validate("head_score_ema"); err != nil || alpha.Numerator > alpha.Denominator {
		return nil, nil, errors.New("invalid head EMA policy")
	}
	a := new(big.Rat).SetFrac(new(big.Int).SetUint64(alpha.Numerator), new(big.Int).SetUint64(alpha.Denominator))
	oneMinus := new(big.Rat).Sub(big.NewRat(1, 1), a)
	all := map[string]FleetScoreKey{}
	for key, entry := range s.values {
		all[key] = entry.Key
	}
	for key, score := range raw {
		if score == nil || score.Sign() < 0 {
			return nil, nil, errors.New("head EMA raw score is nil or negative")
		}
		all[key.String()] = key
	}
	keys := make([]string, 0, len(all))
	for key := range all {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	records := make([]HeadEMAMeasurement, 0, len(keys))
	for _, id := range keys {
		key := all[id]
		prior := new(big.Rat)
		priorEntry, hasPrior := s.values[id]
		if hasPrior {
			prior = entryRat(priorEntry)
		}
		current, hasRaw := raw[key]
		if !hasRaw {
			current = new(big.Rat)
		}
		next := new(big.Rat)
		if !hasPrior {
			next.Set(current)
		} else {
			next.Add(new(big.Rat).Mul(a, current), new(big.Rat).Mul(oneMinus, prior))
		}
		rawJSON, _ := encodeRationalJSON(current)
		priorJSON, _ := encodeRationalJSON(prior)
		nextJSON, _ := encodeRationalJSON(next)
		records = append(records, HeadEMAMeasurement{Key: key, HasRaw: hasRaw, Raw: rawJSON, HasPrior: hasPrior, Prior: priorJSON, Next: nextJSON})
		if next.Sign() == 0 {
			delete(s.values, id)
		} else {
			s.values[id] = headEMAEntry{Key: key, Numerator: next.Num().String(), Denominator: next.Denom().String()}
		}
	}
	if err := verifyHeadEMAFold(records, alpha); err != nil {
		return nil, nil, err
	}
	out, err := headEMAOutput(records)
	return out, records, err
}

// Fold applies the policy rational to each fleet identity (fleet, hotkey,
// generation, uid). Missing identities decay; a new generation never inherits
// the score of the prior owner of the same UID.
func (s *HeadEMAStore) Fold(raw map[FleetScoreKey]*big.Rat, alpha protocol.Rational) (map[uint16]*big.Rat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	priorValues := cloneHeadEMAEntries(s.values)
	priorEpoch, priorAlpha := s.lastSubnetEpoch, s.lastAlpha
	priorFold := append([]HeadEMAMeasurement(nil), s.lastFold...)
	out, _, err := s.foldWithLock(raw, alpha)
	if err != nil {
		return nil, err
	}
	s.lastSubnetEpoch = nil
	s.lastAlpha = nil
	s.lastFold = nil
	if err := s.saveLocked(); err != nil {
		s.values = priorValues
		s.lastSubnetEpoch, s.lastAlpha, s.lastFold = priorEpoch, priorAlpha, priorFold
		return nil, err
	}
	return out, nil
}

func cloneHeadEMAEntries(values map[string]headEMAEntry) map[string]headEMAEntry {
	cloned := make(map[string]headEMAEntry, len(values))
	for key, entry := range values {
		cloned[key] = entry
	}
	return cloned
}

func rawHeadEMAInputs(records []HeadEMAMeasurement) (map[FleetScoreKey]*big.Rat, error) {
	raw := make(map[FleetScoreKey]*big.Rat)
	for index, record := range records {
		if !record.HasRaw {
			continue
		}
		value, err := decodeRationalJSON(record.Raw)
		if err != nil {
			return nil, fmt.Errorf("head EMA record %d raw value: %w", index, err)
		}
		if _, exists := raw[record.Key]; exists {
			return nil, fmt.Errorf("head EMA record %d duplicates a raw identity", index)
		}
		raw[record.Key] = value
	}
	return raw, nil
}

func equalHeadEMAFolds(left, right []HeadEMAMeasurement) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// PreviewForEpoch calculates a fold without advancing durable EMA state. The
// caller must commit the returned transcript only after its steering intent is
// durably present, so a downstream RPC or disk failure cannot contaminate the
// following native epoch.
func (s *HeadEMAStore) PreviewForEpoch(subnetEpoch uint64, raw map[FleetScoreKey]*big.Rat, alpha protocol.Rational) (map[uint16]*big.Rat, []HeadEMAMeasurement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastSubnetEpoch != nil {
		if subnetEpoch < *s.lastSubnetEpoch {
			return nil, nil, fmt.Errorf("head EMA epoch regressed from %d to %d", *s.lastSubnetEpoch, subnetEpoch)
		}
		if subnetEpoch == *s.lastSubnetEpoch {
			if s.lastAlpha == nil || *s.lastAlpha != alpha {
				return nil, nil, errors.New("same-epoch head EMA policy changed")
			}
			candidateRaw, err := rawHeadEMAInputs(s.lastFold)
			if err != nil {
				return nil, nil, err
			}
			if len(candidateRaw) != len(raw) {
				return nil, nil, errors.New("same-epoch head EMA raw input cardinality changed")
			}
			for key, want := range candidateRaw {
				got, ok := raw[key]
				if !ok || got == nil || got.Cmp(want) != 0 {
					return nil, nil, errors.New("same-epoch head EMA raw inputs changed")
				}
			}
			out, err := headEMAOutput(s.lastFold)
			return out, append([]HeadEMAMeasurement(nil), s.lastFold...), err
		}
		if *s.lastSubnetEpoch == ^uint64(0) || subnetEpoch != *s.lastSubnetEpoch+1 {
			return nil, nil, fmt.Errorf("head EMA epoch jumped from %d to %d", *s.lastSubnetEpoch, subnetEpoch)
		}
	}
	preview := &HeadEMAStore{values: cloneHeadEMAEntries(s.values)}
	out, records, err := preview.foldWithLock(raw, alpha)
	return out, records, err
}

// CommitForEpoch advances durable EMA state using an already-published exact
// transcript. It is idempotent for restart recovery after IntentStore.Begin.
func (s *HeadEMAStore) CommitForEpoch(subnetEpoch uint64, records []HeadEMAMeasurement, alpha protocol.Rational) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastSubnetEpoch != nil {
		if subnetEpoch < *s.lastSubnetEpoch {
			return fmt.Errorf("head EMA epoch regressed from %d to %d", *s.lastSubnetEpoch, subnetEpoch)
		}
		if subnetEpoch == *s.lastSubnetEpoch {
			if s.lastAlpha == nil || *s.lastAlpha != alpha || !equalHeadEMAFolds(s.lastFold, records) {
				return errors.New("committed same-epoch head EMA transcript changed")
			}
			return nil
		}
		if *s.lastSubnetEpoch == ^uint64(0) || subnetEpoch != *s.lastSubnetEpoch+1 {
			return fmt.Errorf("head EMA epoch jumped from %d to %d", *s.lastSubnetEpoch, subnetEpoch)
		}
	}
	raw, err := rawHeadEMAInputs(records)
	if err != nil {
		return err
	}
	priorValues := cloneHeadEMAEntries(s.values)
	priorEpoch, priorAlpha := s.lastSubnetEpoch, s.lastAlpha
	priorFold := append([]HeadEMAMeasurement(nil), s.lastFold...)
	_, reconstructed, err := s.foldWithLock(raw, alpha)
	if err != nil {
		return err
	}
	if !equalHeadEMAFolds(reconstructed, records) {
		s.values = priorValues
		return errors.New("head EMA transcript does not extend durable prior state")
	}
	epoch := subnetEpoch
	policy := alpha
	s.lastSubnetEpoch, s.lastAlpha = &epoch, &policy
	s.lastFold = append([]HeadEMAMeasurement(nil), records...)
	if err := s.saveLocked(); err != nil {
		s.values = priorValues
		s.lastSubnetEpoch, s.lastAlpha, s.lastFold = priorEpoch, priorAlpha, priorFold
		return err
	}
	return nil
}

// FoldForEpoch is crash-idempotent. A retry in the same native epoch must
// supply byte-equivalent raw inputs and receives the already persisted fold;
// it can never apply alpha twice after a process restart.
func (s *HeadEMAStore) FoldForEpoch(subnetEpoch uint64, raw map[FleetScoreKey]*big.Rat, alpha protocol.Rational) (map[uint16]*big.Rat, []HeadEMAMeasurement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastSubnetEpoch != nil {
		if subnetEpoch < *s.lastSubnetEpoch {
			return nil, nil, fmt.Errorf("head EMA epoch regressed from %d to %d", *s.lastSubnetEpoch, subnetEpoch)
		}
		if subnetEpoch == *s.lastSubnetEpoch {
			if s.lastAlpha == nil || *s.lastAlpha != alpha {
				return nil, nil, errors.New("same-epoch head EMA policy changed")
			}
			rawCount := 0
			for _, record := range s.lastFold {
				if !record.HasRaw {
					continue
				}
				rawCount++
				value, ok := raw[record.Key]
				encoded, encodeErr := encodeRationalJSON(value)
				if !ok || encodeErr != nil || encoded != record.Raw {
					return nil, nil, errors.New("same-epoch head EMA raw inputs changed")
				}
			}
			if rawCount != len(raw) {
				return nil, nil, errors.New("same-epoch head EMA raw input cardinality changed")
			}
			out, err := headEMAOutput(s.lastFold)
			return out, append([]HeadEMAMeasurement(nil), s.lastFold...), err
		}
	}
	priorValues := make(map[string]headEMAEntry, len(s.values))
	for key, entry := range s.values {
		priorValues[key] = entry
	}
	priorEpoch, priorAlpha, priorFold := s.lastSubnetEpoch, s.lastAlpha, s.lastFold
	out, records, err := s.foldWithLock(raw, alpha)
	if err != nil {
		return nil, nil, err
	}
	epoch := subnetEpoch
	policy := alpha
	s.lastSubnetEpoch = &epoch
	s.lastAlpha = &policy
	s.lastFold = append([]HeadEMAMeasurement(nil), records...)
	if err := s.saveLocked(); err != nil {
		s.values = priorValues
		s.lastSubnetEpoch, s.lastAlpha, s.lastFold = priorEpoch, priorAlpha, priorFold
		return nil, nil, err
	}
	return out, records, nil
}
