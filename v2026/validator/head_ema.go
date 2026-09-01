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

	"github.com/urfoundation/sn/v2026/protocol"
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
	Schema    string         `json:"schema"`
	UpdatedAt string         `json:"updated_at"`
	Entries   []headEMAEntry `json:"entries"`
}

type HeadEMAStore struct {
	mu     sync.Mutex
	path   string
	values map[string]headEMAEntry
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
	if file.Schema != "urnetwork-validator-head-ema-v1" {
		return nil, fmt.Errorf("unsupported head EMA schema %q", file.Schema)
	}
	for _, entry := range file.Entries {
		if _, ok := new(big.Int).SetString(entry.Numerator, 10); !ok {
			return nil, errors.New("invalid persisted head EMA numerator")
		}
		denom, ok := new(big.Int).SetString(entry.Denominator, 10)
		if !ok || denom.Sign() <= 0 {
			return nil, errors.New("invalid persisted head EMA denominator")
		}
		s.values[entry.Key.String()] = entry
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
	file := headEMAFile{Schema: "urnetwork-validator-head-ema-v1", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	for _, key := range keys {
		file.Entries = append(file.Entries, s.values[key])
	}
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return atomicStateWrite(s.path, append(b, '\n'), 0o600)
}

// Fold applies the policy rational to each fleet identity (fleet, hotkey,
// generation, uid). Missing identities decay; a new generation never inherits
// the score of the prior owner of the same UID.
func (s *HeadEMAStore) Fold(raw map[FleetScoreKey]*big.Rat, alpha protocol.Rational) (map[uint16]*big.Rat, error) {
	if err := alpha.Validate("head_score_ema"); err != nil || alpha.Numerator > alpha.Denominator {
		return nil, errors.New("invalid head EMA policy")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a := new(big.Rat).SetFrac(new(big.Int).SetUint64(alpha.Numerator), new(big.Int).SetUint64(alpha.Denominator))
	oneMinus := new(big.Rat).Sub(big.NewRat(1, 1), a)
	all := map[string]FleetScoreKey{}
	for key, entry := range s.values {
		all[key] = entry.Key
	}
	for key := range raw {
		all[key.String()] = key
	}
	for id, key := range all {
		prior := new(big.Rat)
		if entry, ok := s.values[id]; ok {
			prior = entryRat(entry)
		}
		current := raw[key]
		if current == nil {
			current = new(big.Rat)
		}
		var next *big.Rat
		if _, existed := s.values[id]; !existed {
			next = new(big.Rat).Set(current)
		} else {
			next = new(big.Rat).Add(new(big.Rat).Mul(a, current), new(big.Rat).Mul(oneMinus, prior))
		}
		if next.Sign() == 0 {
			delete(s.values, id)
			continue
		}
		s.values[id] = headEMAEntry{Key: key, Numerator: next.Num().String(), Denominator: next.Denom().String()}
	}
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	// Persist missing identities while their EMA decays, but never return them
	// to the live weight channel. Otherwise a pruned UID can keep receiving
	// weight solely because its historical score is still nonzero.
	out := map[uint16]*big.Rat{}
	for key := range raw {
		entry, ok := s.values[key.String()]
		if !ok {
			continue
		}
		if out[entry.Key.UID] == nil {
			out[entry.Key.UID] = new(big.Rat)
		}
		out[entry.Key.UID].Add(out[entry.Key.UID], entryRat(entry))
	}
	return out, nil
}
