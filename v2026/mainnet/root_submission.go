// An independently approved owned route submits one original root extrinsic.
// Durable attempt identity precedes every write; only canonical reconciliation
// can settle a lost acknowledgement, dispatch outcome or mortal expiry.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"
)

const rootSubmissionApprovalSchema = "urnetwork-mainnet-root-submission-approval-v1"
const rootSubmissionConfigSchema = "urnetwork-mainnet-root-submission-config-v1"
const rootSubmissionStateSchema = "urnetwork-mainnet-root-submission-state-v1"

var errRootSubmissionUncertain = errors.New("root submission attempt may have reached the node; reconcile original bytes before another numbered attempt")

// Separate approval binds the complete service config, exact owned route and
// transport/journal limits. It uses the independently provisioned approval key,
// with a different signature domain from the native-action approval.
type rootSubmissionApproval struct {
	Schema             string `json:"schema"`
	ServiceConfigHash  string `json:"service_config_hash"`
	PacketHash         string `json:"packet_hash"`
	RpcUrl             string `json:"rpc_url"`
	TlsSpkiHash        string `json:"tls_spki_sha256,omitempty"`
	StatePath          string `json:"submission_state_path"`
	ReadRetrySeconds   uint32 `json:"read_retry_seconds"`
	SendTimeoutSeconds uint32 `json:"send_timeout_seconds"`
	Signature          string `json:"approval_signature_ed25519"`
}

// Native signing approval cannot be replayed as route/submission approval.
func (self rootSubmissionApproval) signingBytes() ([]byte, error) {
	self.Signature = ""
	raw, err := json.Marshal(self)
	return append([]byte(rootSubmissionApprovalSchema+"\x00"), raw...), err
}

// Callers provision trust independently; imported state cannot supply it.
type rootSubmissionConfig struct {
	Schema   string                 `json:"schema"`
	Service  rootServiceConfig      `json:"service"`
	Approval rootSubmissionApproval `json:"submission_approval"`
}

// Three journals must have distinct canonical paths and ownership markers.
func (self rootSubmissionConfig) validate() error {
	if err := self.Service.validate(); err != nil {
		return err
	}
	approval := self.Approval
	if self.Schema != rootSubmissionConfigSchema || approval.Schema != rootSubmissionApprovalSchema ||
		approval.ServiceConfigHash != rootObjectHash(self.Service) || approval.PacketHash != self.Service.Packet.ContentHash ||
		approval.ReadRetrySeconds < 60 || approval.ReadRetrySeconds > 900 || approval.SendTimeoutSeconds == 0 || approval.SendTimeoutSeconds > 60 ||
		!filepath.IsAbs(approval.StatePath) || filepath.Clean(approval.StatePath) != approval.StatePath || approval.StatePath == "/" {
		return errors.New("root submission lacks exact approval, private path or bounded transport limits")
	}
	for _, path := range []string{self.Service.Packet.Action.Scope.StatePath, self.Service.CustodyTrust.StatePath} {
		if approval.StatePath == path || approval.StatePath+".lock" == path || path+".lock" == approval.StatePath {
			return errors.New("root submission journal overlaps action or custody ownership")
		}
	}
	if err := rootSubmissionRoute(approval); err != nil {
		return err
	}
	key, _ := hex.DecodeString(self.Service.CustodyTrust.ApprovalPublicKey[2:])
	signature, err := rootOfflineSignatureBytes(approval.Signature)
	message, messageErr := approval.signingBytes()
	if err != nil || messageErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.New("root submission route/action approval signature is invalid")
	}
	return nil
}

// No caller-owned action vector remains mutable inside a submitter or store.
func copyRootSubmissionConfig(config rootSubmissionConfig) rootSubmissionConfig {
	config.Service = copyRootServiceConfig(config.Service)
	return config
}

// Uncertain is persisted before the transport sees any bytes. Acknowledgement
// proves only that the node returned the expected hash, never finality.
type rootSubmissionAttempt struct {
	Number       uint8  `json:"number"`
	Phase        string `json:"phase"`
	ReturnedHash string `json:"returned_hash,omitempty"`
	Detail       string `json:"detail,omitempty"`
}

// Gaps in attempt numbers are retained: the action owner may have reserved an
// attempt before crashing without ever reaching this independently owned port.
type rootSubmissionRecord struct {
	Schema            string                    `json:"schema"`
	Config            rootSubmissionConfig      `json:"config"`
	RawExtrinsic      string                    `json:"raw_extrinsic,omitempty"`
	ExtrinsicHash     string                    `json:"extrinsic_hash,omitempty"`
	Attempts          []rootSubmissionAttempt   `json:"attempts"`
	LastFinalized     uint64                    `json:"last_finalized"`
	LastFinalizedHash string                    `json:"last_finalized_hash,omitempty"`
	Reconciliation    *rootActionReconciliation `json:"reconciliation,omitempty"`
	ContentHash       string                    `json:"content_hash"`
}

// Every open independently authenticates approval, signature, attempt bounds
// and any canonical evidence; a rehashed journal cannot approve another action.
func (self rootSubmissionRecord) validate(config rootSubmissionConfig) error {
	if err := config.validate(); err != nil {
		return err
	}
	claimed := self.ContentHash
	self.ContentHash = ""
	if self.Schema != rootSubmissionStateSchema || rootObjectHash(self.Config) != rootObjectHash(config) || claimed != rootObjectHash(self) {
		return errors.New("root submission state differs from its independent configuration")
	}
	action := config.Service.Packet.Action
	var raw []byte
	if self.RawExtrinsic == "" {
		if self.ExtrinsicHash != "" || len(self.Attempts) != 0 {
			return errors.New("root submission attempts lack original signed bytes")
		}
	} else {
		var err error
		raw, err = rootReceiptHex(self.RawExtrinsic, 64*1024)
		if err != nil || len(raw) == 0 || rootReceiptSignedAction(action, raw) != nil || self.ExtrinsicHash != rootExtrinsicHash(raw) {
			return errors.New("root submission original signed bytes or native hash differ")
		}
	}
	previous := uint8(0)
	for _, attempt := range self.Attempts {
		if attempt.Number <= previous || attempt.Number > action.Scope.MaxBroadcasts || len(attempt.Detail) > 1024 ||
			attempt.Phase == "acknowledged" && attempt.ReturnedHash != self.ExtrinsicHash ||
			attempt.Phase == "uncertain" && attempt.ReturnedHash != "" || attempt.Phase != "acknowledged" && attempt.Phase != "uncertain" {
			return errors.New("root submission attempt history exceeds its original allowance or has invalid outcome")
		}
		previous = attempt.Number
	}
	if self.LastFinalized == 0 && self.LastFinalizedHash != "" || self.LastFinalized != 0 && (self.LastFinalized < action.BirthBlock || !rootCanonicalHash(self.LastFinalizedHash)) {
		return errors.New("root submission finalized position is invalid")
	}
	if self.Reconciliation != nil {
		if err := self.Reconciliation.validate(action, raw); err != nil {
			return err
		}
		if self.LastFinalized != self.Reconciliation.Observation.FinalizedNumber || self.LastFinalizedHash != self.Reconciliation.Observation.FinalizedHash {
			return errors.New("root submission evidence differs from retained finalized position")
		}
	}
	return nil
}

// Stores sync the complete record before returning. Closing follows joined use.
type rootSubmissionStorage interface {
	load() (rootSubmissionRecord, error)
	save(rootSubmissionRecord) error
}

// The production constructor owns its route and canonical reader. No caller can
// substitute a transport or silently attach a fallback node. Operations serialize
// with cancellation-aware ownership; no background worker is detached.
type rootOwnedSubmission struct {
	config    rootSubmissionConfig
	store     rootSubmissionStorage
	chain     *rootCanonicalChain
	authority rootActionAuthority
	ownerCh   chan struct{}
	poisoned  bool
	failure   error
}

// Construction performs no network call. Nil authority permits old receipt
// recovery but cannot authorize a new transport attempt.
func newRootOwnedSubmission(config rootSubmissionConfig, store rootSubmissionStorage, authority rootActionAuthority) (*rootOwnedSubmission, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("root submission journal is absent")
	}
	client, err := newRootSubmissionClient(config.Approval)
	if err != nil {
		return nil, err
	}
	scope := config.Service.Packet.Action.Scope
	profile := rootReceiptProfile{RuntimeSourceCommit: scope.RuntimeSourceCommit, RuntimeVersion: scope.RuntimeVersion, RuntimeCodeHash: scope.RuntimeCodeHash, RuntimeMetadataHash: scope.RuntimeMetadataHash}
	chain, err := newRootCanonicalChain(client, identityExpectation{NativeChain: scope.NativeChain, GenesisHash: scope.GenesisHash, EvmChainId: scope.EvmChainId}, []rootReceiptProfile{profile})
	if err != nil {
		return nil, err
	}
	self := &rootOwnedSubmission{config: copyRootSubmissionConfig(config), store: store, chain: chain, authority: authority, ownerCh: make(chan struct{}, 1)}
	if _, err := self.load(); err != nil {
		return nil, err
	}
	return self, nil
}

// Canceled waiters cannot consume an attempt or read another operation's state.
func (self *rootOwnedSubmission) acquire(ctx context.Context) error {
	if ctx == nil {
		return errors.New("root submission requires a context")
	}
	select {
	case self.ownerCh <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-self.ownerCh
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Internal access requires the owner channel or construction. Missing/corrupt
// state poisons this instance instead of replenishing an allowance.
func (self *rootOwnedSubmission) load() (rootSubmissionRecord, error) {
	if self.poisoned {
		return rootSubmissionRecord{}, errors.Join(errors.New("root submission owner must reopen after an integrity or durability failure"), self.failure)
	}
	record, err := self.store.load()
	if err == nil {
		err = record.validate(self.config)
	}
	if err != nil && !mainnetDurableAdmissionPending(err) {
		self.poisoned, self.failure = true, err
	}
	return record, err
}

// A post-rename error may already be durable; no transport call follows it.
func (self *rootOwnedSubmission) persist(record rootSubmissionRecord) error {
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(self.config); err != nil {
		return err
	}
	if err := self.store.save(record); err != nil {
		if !mainnetDurableAdmissionPending(err) {
			self.poisoned, self.failure = true, err
		}
		return err
	}
	return nil
}

// Pin the original signature even before transport. A valid alternative
// signature of the same payload cannot replace the first retained bytes.
func (self *rootOwnedSubmission) bind(record *rootSubmissionRecord, raw []byte) error {
	if len(raw) > 64*1024 || rootReceiptSignedAction(self.config.Service.Packet.Action, raw) != nil {
		return errors.New("root submission signed call differs from approved native action")
	}
	encoded := ""
	if len(raw) != 0 {
		encoded = "0x" + hex.EncodeToString(raw)
	}
	if record.RawExtrinsic != "" && record.RawExtrinsic != encoded {
		return errors.New("root submission cannot replace or forget original signed bytes")
	}
	if encoded != "" && record.RawExtrinsic == "" {
		record.RawExtrinsic, record.ExtrinsicHash = encoded, rootExtrinsicHash(raw)
		record.Reconciliation = nil
		return self.persist(*record)
	}
	return nil
}

// The same owned canonical reader supports the service's reconciliation port.
// A current-authority failure never erases an earlier signed receipt.
func (self *rootOwnedSubmission) reconcile(ctx context.Context, action rootAction, raw []byte) (rootActionReconciliation, error) {
	if err := self.acquire(ctx); err != nil {
		return rootActionReconciliation{}, err
	}
	defer func() { <-self.ownerCh }()
	if err := action.validate(); err != nil || action.RequestHash != self.config.Service.Packet.Action.RequestHash {
		return rootActionReconciliation{}, errors.Join(errors.New("root submission reconciliation names another request"), err)
	}
	record, err := self.load()
	if err != nil {
		return rootActionReconciliation{}, err
	}
	if err := self.bind(&record, raw); err != nil {
		return rootActionReconciliation{}, err
	}
	return self.reconcileOwned(ctx, &record, raw)
}

// The owner channel is held across canonical reads. Stored terminal evidence
// is immutable and remains readable after route/authority loss.
func (self *rootOwnedSubmission) reconcileOwned(ctx context.Context, record *rootSubmissionRecord, raw []byte) (rootActionReconciliation, error) {
	action := self.config.Service.Packet.Action
	if record.Reconciliation != nil && len(raw) != 0 && rootTerminalPhase(action, *record.Reconciliation) != "" {
		return *record.Reconciliation, ctx.Err()
	}
	result, err := self.chain.reconcile(ctx, copyRootAction(action), raw)
	if err != nil {
		return rootActionReconciliation{}, err
	}
	if err := result.validate(action, raw); err != nil {
		return rootActionReconciliation{}, err
	}
	position := result.Observation
	if position.FinalizedNumber < record.LastFinalized || position.FinalizedNumber == record.LastFinalized && record.LastFinalizedHash != "" && position.FinalizedHash != record.LastFinalizedHash || position.FinalizedNumber > record.LastFinalized && position.FinalizedHash == record.LastFinalizedHash {
		return rootActionReconciliation{}, errors.New("root submission finalized continuity changed")
	}
	record.LastFinalized, record.LastFinalizedHash, record.Reconciliation = position.FinalizedNumber, position.FinalizedHash, &result
	if err := self.persist(*record); err != nil {
		return rootActionReconciliation{}, err
	}
	if err := ctx.Err(); err != nil {
		return rootActionReconciliation{}, err
	}
	return result, nil
}

// There is exactly one possible write per numbered attempt. Duplicate calls
// only reconcile/return the original outcome, including after uncertain sends.
// A higher attempt requires fresh independent authority and the same bytes.
func (self *rootOwnedSubmission) submitRoot(ctx context.Context, intent rootServiceSubmission) error {
	if err := self.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-self.ownerCh }()
	operationCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if err := intent.Packet.validate(self.config.Service.CustodyTrust); err != nil {
		return err
	}
	action := self.config.Service.Packet.Action
	if rootObjectHash(intent.Packet) != rootObjectHash(self.config.Service.Packet) || intent.ConfigHash != rootObjectHash(self.config.Service) || intent.Attempt == 0 || intent.Attempt > action.Scope.MaxBroadcasts {
		return errors.New("root submission packet, service or attempt differs from independent approval")
	}
	raw, err := rootReceiptHex(intent.RawExtrinsic, 64*1024)
	if err != nil || len(raw) == 0 || intent.ExtrinsicHash != rootExtrinsicHash(raw) {
		return errors.New("root submission requires exact original native bytes and hash")
	}
	record, err := self.load()
	if err != nil {
		return err
	}
	if err := self.bind(&record, raw); err != nil {
		return err
	}
	result, err := self.reconcileOwned(operationCtx, &record, raw)
	if err != nil {
		return err
	}
	if rootTerminalPhase(action, result) != "" {
		return nil
	}
	for _, previous := range record.Attempts {
		if previous.Number == intent.Attempt {
			if previous.Phase == "acknowledged" {
				return nil
			}
			return errRootSubmissionUncertain
		}
		if previous.Number > intent.Attempt {
			return errors.New("root submission cannot refill a previously skipped attempt")
		}
	}
	if err := result.Observation.matches(action, true); err != nil {
		return err
	}
	if self.authority == nil {
		return errors.New("root submission current eligibility/custody/exposure authority is absent")
	}
	if err := self.authority.authorize(operationCtx, copyRootAction(action), result.Observation); err != nil {
		return err
	}
	if err := self.chain.network(operationCtx); err != nil {
		return err
	}
	if err := operationCtx.Err(); err != nil {
		return err
	}
	record.Attempts = append(record.Attempts, rootSubmissionAttempt{Number: intent.Attempt, Phase: "uncertain"})
	if err := self.persist(record); err != nil {
		return err
	}
	if err := operationCtx.Err(); err != nil {
		return err
	}
	returned, sendErr := self.sendOnce(operationCtx, intent.RawExtrinsic, intent.ExtrinsicHash)
	attempt := &record.Attempts[len(record.Attempts)-1]
	if sendErr == nil {
		attempt.Phase, attempt.ReturnedHash = "acknowledged", returned
	} else {
		attempt.Detail = sendErr.Error()
		if len(attempt.Detail) > 1024 {
			attempt.Detail = attempt.Detail[:1024]
		}
	}
	if err := self.persist(record); err != nil {
		return errors.Join(sendErr, err)
	}
	return errors.Join(sendErr, operationCtx.Err())
}
