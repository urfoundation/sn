// One runtime render or verification may traverse the same retained plan many
// times. Reuse its full validation only for identical bytes and authority, while
// every read still acquires the bounded no-follow source and returns owned data.
package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"sync"
)

// A single immutable result bounds memory regardless of plan/config revisions.
// Concurrent callers may authenticate independent cold reads; completed proofs
// are reused without holding the state lock during decoding or authentication.
type runtimePlanReadScope struct {
	stateLock sync.Mutex
	key       string
	planBytes []byte

	authenticate func(*ResolvedConfig, []byte, bool) (*SetupPlan, error)
}

// Bypass the public redacted JSON view: omitted budget/custody fields are
// mandatory cache authority. These bytes are only hashed in memory, never
// persisted or logged as a configuration or evidence artifact.
type runtimePlanReadConfigIdentity ResolvedConfig

// Scope proof reuse to the returned configuration. The caller's configuration
// and unrelated invocations stay cold; value copies within this operation
// share the reader but never share a mutable returned plan.
func runtimePlanReadScopeConfig(cfg *ResolvedConfig) *ResolvedConfig {
	if cfg == nil || cfg.runtimePlanReads != nil {
		return cfg
	}
	resolved := *cfg
	resolved.runtimePlanReads = &runtimePlanReadScope{authenticate: loadPlanIdentityBytes}
	return &resolved
}

// Include all configuration fields consumed by plan admission and its private
// assurance/route flags. Generated evidence paths do not change approved
// provisioning policy; each resolver independently authenticates those files.
func (self *runtimePlanReadScope) load(cfg *ResolvedConfig, stateDir string, raw []byte, retainRelease bool) (*SetupPlan, error) {
	identity := *cfg
	if cfg.Config != nil && cfg.Config.ProvisionValidatorEvidenceV2 {
		config := *cfg.Config
		config.ValidatorEvidenceV2 = runtimeEvidenceTemplateV2(config.ValidatorEvidenceV2)
		identity.Config = &config
	}
	root, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, err
	}
	var record *provisionalResumeRecord
	if cfg.provisionalResume != nil {
		record = cfg.provisionalResume.Record
	}
	key, err := canonicalHashHex(struct {
		Config                  runtimePlanReadConfigIdentity
		Root                    string
		SourceHash              string
		RetainRelease           bool
		ReadOnlyAudit           bool
		RelayCapturePlanHash    string
		OwnedRpcAuthority       string
		ProvisionalRpcAuthority string
		Record                  *provisionalResumeRecord
	}{Config: runtimePlanReadConfigIdentity(identity), Root: root, SourceHash: bytesSHA256(raw), RetainRelease: retainRelease,
		ReadOnlyAudit: cfg.readOnlyAudit, RelayCapturePlanHash: cfg.relayCapturePlanHash,
		OwnedRpcAuthority: cfg.ownedRPCAuthority, ProvisionalRpcAuthority: cfg.provisionalRPCAuthority, Record: record})
	if err != nil {
		return nil, err
	}
	var cached []byte
	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if self.key == key {
			cached = self.planBytes
		}
	}()
	if cached != nil {
		var plan SetupPlan
		if err := json.Unmarshal(cached, &plan); err != nil {
			return nil, err
		}
		return &plan, nil
	}
	authenticate := self.authenticate
	if authenticate == nil {
		authenticate = loadPlanIdentityBytes
	}
	plan, err := authenticate(cfg, raw, retainRelease)
	if err != nil {
		return nil, err
	}
	// Keep the admitted wire, including explicit empty maps/slices. A
	// re-marshal could omit them and change warm callers' decoded ownership.
	encoded := bytes.Clone(raw)
	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.key, self.planBytes = key, encoded
	}()
	return plan, nil
}
