package main

// A successful provisional receipt reconciliation can be reused only while its
// exact journal and every immutable receipt/source file remain unchanged. The
// cache is deliberately unavailable to strict acceptance.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"slices"
	"strings"
)

const (
	provisionalPreparationPersistentCacheSchema = "urnetwork-sim-provisional-preparation-cache-v1"
	provisionalPreparationPersistentCacheDir    = "provisional-preparation-cache-v1"
)

type provisionalPreparationPersistentWitness struct {
	Path             string      `json:"path"`
	Size             int64       `json:"size"`
	Mode             os.FileMode `json:"mode"`
	ModifiedUnixNano int64       `json:"modified_unix_nano"`
}

type provisionalPreparationPersistentEntry struct {
	ActionID          string                                  `json:"action_id"`
	IntentHash        string                                  `json:"intent_hash"`
	EntryHash         string                                  `json:"entry_hash"`
	PostconditionHash string                                  `json:"postcondition_hash"`
	Postcondition     provisionalPreparationPersistentWitness `json:"postcondition"`
}

type provisionalPreparationPersistentProof struct {
	Schema          string                                    `json:"schema"`
	PlanHash        string                                    `json:"plan_hash"`
	ConfigHash      string                                    `json:"config_hash"`
	ReleaseLockHash string                                    `json:"release_lock_hash"`
	JournalRoot     string                                    `json:"journal_root"`
	Entries         []provisionalPreparationPersistentEntry   `json:"entries"`
	Sources         []provisionalPreparationPersistentWitness `json:"sources"`
}

type provisionalPreparationPersistentEnvelope struct {
	Proof provisionalPreparationPersistentProof `json:"proof"`
	MAC   string                                `json:"hmac_sha256"`
}

func provisionalPreparationPersistentCachePath(cfg *ResolvedConfig, stateDir string) (string, [32]byte, error) {
	var key [32]byte
	if !provisionalResumeEnabled(cfg) || cfg == nil || cfg.provisionalResume.Record == nil || cfg.Config == nil || stateDir == "" {
		return "", key, errors.New("provisional preparation cache identity is unavailable")
	}
	key = derive32(cfg, "provisional-preparation-cache/v1")
	nameHash, err := canonicalHashHex(struct{ PlanHash, ConfigHash, ReleaseLockHash string }{cfg.provisionalResume.Record.PlanHash, cfg.ConfigHash, cfg.provisionalResume.Record.ReleaseLockHash})
	if err != nil {
		return "", key, err
	}
	return filepath.Join(stateDir, provisionalPreparationPersistentCacheDir, strings.TrimPrefix(nameHash, "0x")+".json"), key, nil
}

func provisionalPreparationPersistentWitnessFor(stateDir, path string) (provisionalPreparationPersistentWitness, error) {
	var out provisionalPreparationPersistentWitness
	relative, err := filepath.Rel(stateDir, path)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return out, errors.New("persistent preparation witness escapes state")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return out, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 {
		return out, errors.New("persistent preparation witness file is unsafe")
	}
	return provisionalPreparationPersistentWitness{Path: filepath.ToSlash(relative), Size: info.Size(), Mode: info.Mode().Perm(), ModifiedUnixNano: info.ModTime().UnixNano()}, nil
}

func provisionalPreparationPersistentWitnessMatches(stateDir string, want provisionalPreparationPersistentWitness) bool {
	if want.Path == "" || filepath.IsAbs(want.Path) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(want.Path))) != want.Path {
		return false
	}
	got, err := provisionalPreparationPersistentWitnessFor(stateDir, filepath.Join(stateDir, filepath.FromSlash(want.Path)))
	return err == nil && got == want
}

func provisionalPreparationPersistentMAC(key [32]byte, proof provisionalPreparationPersistentProof) (string, error) {
	raw, err := json.Marshal(proof)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func (self *Executor) loadProvisionalPreparationPersistentCache(entries []JournalEntry) bool {
	if self == nil || self.cfg == nil || self.plan == nil || !provisionalResumeEnabled(self.cfg) {
		return false
	}
	path, key, err := provisionalPreparationPersistentCachePath(self.cfg, self.stateDir)
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var envelope provisionalPreparationPersistentEnvelope
	if json.Unmarshal(raw, &envelope) != nil {
		return false
	}
	wantMAC, err := provisionalPreparationPersistentMAC(key, envelope.Proof)
	if err != nil || !hmac.Equal([]byte(wantMAC), []byte(envelope.MAC)) {
		return false
	}
	root, err := canonicalHashHex(entries)
	if err != nil || envelope.Proof.Schema != provisionalPreparationPersistentCacheSchema || envelope.Proof.PlanHash != self.plan.PlanHash || envelope.Proof.ConfigHash != self.cfg.ConfigHash || envelope.Proof.ReleaseLockHash != self.cfg.provisionalResume.Record.ReleaseLockHash || envelope.Proof.JournalRoot != root {
		return false
	}
	verified := newCarriedPreparationIndex(self.plan, entries)
	if len(envelope.Proof.Entries) == 0 {
		return false
	}
	for _, cached := range envelope.Proof.Entries {
		action := Action{ID: cached.ActionID, IntentHash: cached.IntentHash}
		entry, ok := verified.find(action, true)
		if !ok || entry.Stage != StageVerified || entry.EntryHash != cached.EntryHash || entry.PostconditionHash != cached.PostconditionHash || !provisionalPreparationPersistentWitnessMatches(self.stateDir, cached.Postcondition) {
			return false
		}
	}
	for _, source := range envelope.Proof.Sources {
		if !provisionalPreparationPersistentWitnessMatches(self.stateDir, source) {
			return false
		}
	}
	return true
}

func (self *Executor) saveProvisionalPreparationPersistentCache(entries []JournalEntry, verifiedEntries []JournalEntry) {
	if self == nil || self.cfg == nil || self.plan == nil || !provisionalResumeEnabled(self.cfg) || len(verifiedEntries) == 0 {
		return
	}
	path, key, err := provisionalPreparationPersistentCachePath(self.cfg, self.stateDir)
	if err != nil {
		return
	}
	root, err := canonicalHashHex(entries)
	if err != nil {
		return
	}
	proof := provisionalPreparationPersistentProof{Schema: provisionalPreparationPersistentCacheSchema, PlanHash: self.plan.PlanHash, ConfigHash: self.cfg.ConfigHash, ReleaseLockHash: self.cfg.provisionalResume.Record.ReleaseLockHash, JournalRoot: root}
	sourceKVs := map[string]provisionalPreparationPersistentWitness{}
	for _, entry := range verifiedEntries {
		post, err := provisionalPreparationPersistentWitnessFor(self.stateDir, filepath.Join(self.stateDir, filepath.FromSlash(entry.PostconditionPath)))
		if err != nil {
			return
		}
		proof.Entries = append(proof.Entries, provisionalPreparationPersistentEntry{ActionID: entry.ActionID, IntentHash: entry.IntentHash, EntryHash: entry.EntryHash, PostconditionHash: entry.PostconditionHash, Postcondition: post})
		sourcePath := filepath.Join(self.stateDir, "plans", strings.TrimPrefix(entry.PlanHash, "0x")+".json")
		source, err := provisionalPreparationPersistentWitnessFor(self.stateDir, sourcePath)
		if err != nil {
			return
		}
		sourceKVs[source.Path] = source
	}
	sort.Slice(proof.Entries, func(i, j int) bool { return proof.Entries[i].ActionID < proof.Entries[j].ActionID })
	for _, source := range sourceKVs {
		proof.Sources = append(proof.Sources, source)
	}
	sort.Slice(proof.Sources, func(i, j int) bool { return proof.Sources[i].Path < proof.Sources[j].Path })
	mac, err := provisionalPreparationPersistentMAC(key, proof)
	if err != nil {
		return
	}
	wire, err := json.MarshalIndent(provisionalPreparationPersistentEnvelope{Proof: proof, MAC: mac}, "", "  ")
	if err != nil {
		return
	}
	if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
		return
	}
	_ = atomicWrite(path, append(wire, '\n'), 0o600)
}

func (self *Executor) reuseProvisionalPreparationPersistentCache(ctx context.Context, entries []JournalEntry) bool {
	if ctx == nil || ctx.Err() != nil || !self.loadProvisionalPreparationPersistentCache(entries) {
		return false
	}
	if !slices.Equal(entries, self.journal.Entries()) {
		return false
	}
	return true
}
