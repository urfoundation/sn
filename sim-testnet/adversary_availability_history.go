// Historical failed campaigns retain exact RPC/HTTP recovery observations.
// These wire types preserve the original result commitment; they do not grant
// current sampling or final-acceptance credit without a recovery verifier.
package main

import (
	"encoding/json"
	"errors"

	"github.com/urnetwork/connect/v2026"
)

type AdversaryRpcRecoveryCheckpoint struct {
	Endpoint      string `json:"endpoint"`
	FinalizedHash string `json:"finalized_hash,omitempty"`
	Finalized     uint64 `json:"finalized_number,omitempty"`
	BestHash      string `json:"best_hash,omitempty"`
	Best          uint64 `json:"best_number,omitempty"`
}

type AdversaryRpcRecoveryRead struct {
	RequestHash  string `json:"request_hash"`
	ResponseHash string `json:"response_hash"`
}

type AdversaryRpcRecoveryAttempt struct {
	Sequence       uint64                     `json:"sequence"`
	StartedAt      string                     `json:"started_at"`
	CompletedAt    string                     `json:"completed_at"`
	DurationMillis int64                      `json:"duration_milliseconds"`
	Requests       uint64                     `json:"requests"`
	Detail         string                     `json:"detail"`
	Outcome        string                     `json:"outcome"`
	ReadSetHash    string                     `json:"read_set_hash"`
	ObservedReads  []AdversaryRpcRecoveryRead `json:"observed_reads"`
}

type AdversaryRpcRecoveryEvidence struct {
	AuthorityHash    string                           `json:"authority_hash"`
	Schema           string                           `json:"schema"`
	Id               string                           `json:"id"`
	CreditedSequence uint64                           `json:"credited_sequence,omitempty"`
	OriginalSequence uint64                           `json:"original_sequence"`
	Phase            adversarySamplePhase             `json:"phase"`
	StartedAt        string                           `json:"started_at"`
	Status           string                           `json:"status"`
	Checkpoints      []AdversaryRpcRecoveryCheckpoint `json:"checkpoints,omitempty"`
	Reads            []AdversaryRpcRecoveryRead       `json:"reads"`
	Attempts         []AdversaryRpcRecoveryAttempt    `json:"attempts"`
}

type AdversaryHttpRecoveryRequest struct {
	Kind         string `json:"kind"`
	Endpoint     string `json:"endpoint"`
	MaximumBytes int64  `json:"maximum_bytes"`
	RequestHash  string `json:"request_hash"`
}

type AdversaryHttpRecoveryRead struct {
	AdversaryHttpRecoveryRequest
	ResponseHash string `json:"response_hash"`
	Status       int    `json:"status"`
}

type AdversaryHttpRecoveryAttempt struct {
	AdversaryRpcRecoveryAttempt
	FailedRequest *AdversaryHttpRecoveryRequest `json:"failed_request,omitempty"`
	SnapshotReads uint64                        `json:"snapshot_reads"`
	FreshReads    []string                      `json:"fresh_reads,omitempty"`
}

type AdversaryHttpRecoveryEvidence struct {
	Schema           string                          `json:"schema"`
	Id               string                          `json:"id"`
	AuthorityHash    string                          `json:"authority_hash"`
	OriginalSequence uint64                          `json:"original_sequence"`
	CreditedSequence uint64                          `json:"credited_sequence,omitempty"`
	Phase            adversarySamplePhase            `json:"phase"`
	StartedAt        string                          `json:"started_at"`
	Status           string                          `json:"status"`
	Operator         int                             `json:"operator"`
	Operators        int                             `json:"operators"`
	DeploymentId     string                          `json:"deployment_id"`
	Netuid           uint16                          `json:"netuid"`
	ArtifactHash     string                          `json:"artifact_hash,omitempty"`
	HistoryHash      string                          `json:"history_hash,omitempty"`
	HistoryObjects   int                             `json:"history_objects,omitempty"`
	Reads            []AdversaryHttpRecoveryRead     `json:"reads"`
	Attempts         []AdversaryHttpRecoveryAttempt  `json:"attempts"`
	Verify           *AdversaryVerifyHttpCheckpoint  `json:"verify_checkpoint,omitempty"`
	VerifyHash       string                          `json:"verify_checkpoint_hash,omitempty"`
	Custody          *AdversaryCustodyHttpCheckpoint `json:"custody_checkpoint,omitempty"`
	CustodyHash      string                          `json:"custody_checkpoint_hash,omitempty"`
	Stages           []AdversaryHttpRecoveryStage    `json:"get_stages,omitempty"`
}

type AdversaryVerifyHttpCheckpoint struct {
	Kind              string          `json:"kind"`
	ValidatorId       connect.Id      `json:"validator_id"`
	ValidatorPublic   []byte          `json:"validator_public"`
	Keys              json.RawMessage `json:"keys,omitempty"`
	WalkStartedAt     string          `json:"walk_started_at,omitempty"`
	WalkCompletedAt   string          `json:"walk_completed_at,omitempty"`
	Trail             []connect.Id    `json:"trail,omitempty"`
	ServerNonce       []byte          `json:"server_nonce,omitempty"`
	VerifierSignature []byte          `json:"verifier_signature,omitempty"`
	Final             json.RawMessage `json:"final,omitempty"`
	Replay            bool            `json:"replay,omitempty"`
	BusyResponses     uint64          `json:"busy_responses,omitempty"`
	SkippedTarget     string          `json:"skipped_target,omitempty"`
	SkippedProvider   connect.Id      `json:"skipped_provider"`
	SkippedStage      string          `json:"skipped_stage,omitempty"`
}

type AdversaryCustodyHttpArtifact struct {
	Epoch        uint64 `json:"epoch"`
	ResponseHash string `json:"response_hash"`
}

type AdversaryCustodyHttpCheckpoint struct {
	PriorPassed    []int                                   `json:"prior_passed"`
	DeploymentHash string                                  `json:"deployment_hash"`
	Artifacts      map[string]AdversaryCustodyHttpArtifact `json:"artifacts"`
}

type AdversaryHttpRecoveryStage struct {
	RequestHash string `json:"request_hash"`
	StartedAt   string `json:"started_at"`
	DeadlineAt  string `json:"deadline_at"`
	CompletedAt string `json:"completed_at,omitempty"`
}

// The current actor implementation produces no availability continuations.
// Only failed historical recovery readers may retain these known wire fields;
// final consumers must not infer success merely from a retained status string.
func validateCurrentAdversaryAvailability(actor AdversaryActorEvidence) error {
	if len(actor.RpcAvailability) != 0 || len(actor.HttpAvailability) != 0 || actor.AvailabilityAttempts != 0 {
		return errors.New("historical adversary availability evidence requires its original recovery verifier")
	}
	return nil
}
