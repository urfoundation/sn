// Earning selection is an independently admitted deployment policy. It never
// shortens the original epoch clock or removes pre-cutoff physical work.
package payoutartifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// Field order is the existing immutable Server earning-identity grammar.
// Readiness and the full mutable configuration digest are deliberately absent.
type WholeWorkEarningIdentity struct {
	Schema      string `json:"schema"`
	CutoffUtc   string `json:"cutoff_utc"`
	Attribution string `json:"attribution"`
	LegacyUsdc  string `json:"legacy_usdc"`
	Profile     string `json:"profile"`
	ChainId     uint64 `json:"chain_id"`
	GenesisHash string `json:"genesis_hash"`
	Netuid      uint16 `json:"netuid"`
}

// A policy owner passes the independently read immutable deployment identity.
// This constructor reproduces its commitment; it does not admit authority.
func NewWholeWorkEarningSelection(identity WholeWorkEarningIdentity) (*WholeWorkEarningSelection, error) {
	start, err := time.Parse(time.RFC3339Nano, identity.CutoffUtc)
	if err != nil || start.IsZero() || identity.CutoffUtc != start.UTC().Format(time.RFC3339Nano) || identity.ChainId == 0 || identity.Netuid == 0 {
		return nil, ErrClosedWorkIntegrity
	}
	for _, text := range []string{identity.Schema, identity.Attribution, identity.LegacyUsdc, identity.Profile} {
		if len(text) == 0 || len(text) > 128 {
			return nil, ErrClosedWorkIntegrity
		}
	}
	identity.GenesisHash = strings.ToLower(identity.GenesisHash)
	if !IsDigest(identity.GenesisHash, "0x") {
		return nil, ErrClosedWorkIntegrity
	}
	raw, err := json.Marshal(identity)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	return &WholeWorkEarningSelection{StartTime: start, PolicyHash: "sha256:" + hex.EncodeToString(digest[:])}, nil
}

// An optional declaration cannot authorize itself. Missing original policy is
// unknown, while contradictory present policy and declaration are integrity.
func verifyWholeWorkEarningSelection(census *ClosedWorkCensus, selection *WholeWorkEarningSelection) (time.Time, error) {
	if census == nil {
		return time.Time{}, ErrClosedWorkUnavailable
	}
	if census.EarningStart == "" {
		if census.EarningSelectionHash != "" {
			return time.Time{}, ErrClosedWorkIntegrity
		}
		if selection != nil {
			return time.Time{}, ErrClosedWorkUnavailable
		}
		return time.Time{}, nil
	}
	start, err := time.Parse(time.RFC3339Nano, census.EarningStart)
	if err != nil || start.IsZero() || census.EarningStart != start.UTC().Format(time.RFC3339Nano) || !canonicalClosedWorkDigest(census.EarningSelectionHash) {
		return time.Time{}, ErrClosedWorkIntegrity
	}
	if selection == nil {
		return time.Time{}, ErrClosedWorkUnavailable
	}
	if selection.StartTime.IsZero() || !selection.StartTime.Equal(start) || !canonicalClosedWorkDigest(selection.PolicyHash) || selection.PolicyHash != census.EarningSelectionHash {
		return time.Time{}, ErrClosedWorkIntegrity
	}
	return start, nil
}
