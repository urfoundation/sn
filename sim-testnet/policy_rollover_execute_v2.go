//go:build linux || darwin

package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

type policyRolloverPublicationV2 struct {
	Schema         string `json:"schema"`
	PlanHash       string `json:"plan_hash"`
	ValidatorID    uint64 `json:"validator_id"`
	NoID           uint64 `json:"no_id"`
	ActivationHash string `json:"activation_hash"`
	PublishedBlock uint64 `json:"published_block"`
}

// Production supplies real finalized contract reads and the existing durable
// EVM keeper. These narrow I/O boundaries make crash/error sequencing testable.
type policyRolloverPublicationIOV2 struct {
	Observe func(context.Context, runtimeEvidenceActivationMemberV2) (uint64, error)
	Send    func(context.Context, Action, runtimeEvidenceActivationMemberV2) error
}

func validatePolicyRolloverJournalV2(p *policyRolloverPlanV2, entries []JournalEntry) error {
	if p == nil {
		return errors.New("rollover plan is absent")
	}
	checkpoint := -1
	for index, entry := range entries {
		if entry.EntryHash == p.SourceJournalHash {
			checkpoint = index
			break
		}
	}
	if checkpoint < 0 {
		return errors.New("rollover immutable source journal checkpoint is absent")
	}
	actions := map[string]string{}
	for _, action := range p.Actions {
		actions[action.ID] = action.IntentHash
	}
	handoff, err := policyRolloverHandoffActionV2(p)
	if err != nil {
		return err
	}
	actions[handoff.ID] = handoff.IntentHash
	for _, entry := range entries[checkpoint+1:] {
		if entry.PlanHash != p.PlanHash || entry.DeploymentID != p.DeploymentID || actions[entry.ActionID] != entry.IntentHash {
			return errors.New("deployment journal advanced outside the reviewed rollover checkpoint")
		}
	}
	return nil
}

func policyRolloverHandoffActionV2(p *policyRolloverPlanV2) (Action, error) {
	a := Action{ID: "evidence.policy-rollover-handoff", Kind: "local", Target: p.DeploymentID, Description: "activate the complete fresh evidence generation after four finalized dual consents", Parameters: map[string]string{"rollover_plan_hash": p.PlanHash}}
	var err error
	a.IntentHash, err = actionIntentHash(a)
	return a, err
}

func policyRolloverPriorV2(p *policyRolloverPlanV2, action Action, entries []JournalEntry) (attempts uint64, verified *JournalEntry) {
	for _, entry := range entries {
		if entry.PlanHash != p.PlanHash || entry.ActionID != action.ID || entry.IntentHash != action.IntentHash {
			continue
		}
		if entry.Stage == StageIntent {
			attempts++
		}
		if entry.Stage == StageVerified {
			copy := entry
			verified = &copy
		}
	}
	return
}

func persistPolicyRolloverPostconditionV2(ctx context.Context, p *policyRolloverPlanV2, journal *Journal, action Action, value any, limit uint64) error {
	path, err := postconditionRelativePath(p.PlanHash, action.ID)
	if err != nil {
		return err
	}
	encoded, err := writeRuntimeEvidenceSetupV2(ctx, filepath.Join(p.StateDir, path), value, limit)
	if err != nil {
		return err
	}
	hash := fmt.Sprintf("0x%x", sha256.Sum256(encoded))
	_, prior := policyRolloverPriorV2(p, action, journal.Entries())
	if prior != nil {
		if prior.PostconditionHash != hash || prior.PostconditionPath != path {
			return errors.New("rollover retained postcondition differs from finalized publication")
		}
		return nil
	}
	return journal.Append(JournalEntry{DeploymentID: p.DeploymentID, PlanHash: p.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionHash: hash, PostconditionPath: path})
}

func policyRolloverObserveV2(ctx context.Context, p *policyRolloverPlanV2, io policyRolloverPublicationIOV2, member runtimeEvidenceActivationMemberV2) (uint64, error) {
	var lastErr error
	// Read failures cannot reserve a nonce or consume the durable send budget.
	// Four independently bounded 90-second production attempts tolerate a
	// transient RPC outage for up to 360 seconds without changing the plan.
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		operation, cancel := context.WithTimeout(ctx, time.Duration(p.AttemptTimeoutSeconds)*time.Second)
		block, err := io.Observe(operation, member)
		cancel()
		if err == nil && block != 0 {
			return block, nil
		}
		if errors.Is(err, validatorcomponent.ErrValidatorEvidenceAbsent) {
			return 0, err
		}
		lastErr = err
		if lastErr == nil {
			lastErr = errors.New("rollover publication has no block")
		}
	}
	return 0, lastErr
}

// The retry count is durable across invocations. Every retry first reconciles
// finalized state; absence is the only observation authorizing Send. A lost
// receipt or timeout is success when a subsequent exact public read succeeds.
// Send reuses the keeper's retained signed transaction and nonce on restart.
func publishPolicyRolloverV2(ctx context.Context, p *policyRolloverPlanV2, journal *Journal, limit uint64, io policyRolloverPublicationIOV2) ([]policyRolloverPublicationV2, error) {
	if ctx == nil || p == nil || journal == nil || io.Observe == nil || io.Send == nil || len(p.Members) != 4 || len(p.Actions) != 4 || p.MaximumAttempts == 0 || p.MaximumAttempts > 8 || p.AttemptTimeoutSeconds == 0 || p.AttemptTimeoutSeconds > 300 {
		return nil, errors.New("rollover publication ownership, census or bounded retry policy is incomplete")
	}
	if err := validatePolicyRolloverJournalV2(p, journal.Entries()); err != nil {
		return nil, err
	}
	publications := make([]policyRolloverPublicationV2, 0, 4)
	for index, member := range p.Members {
		action := p.Actions[index]
		attempts, verified := policyRolloverPriorV2(p, action, journal.Entries())
		var published uint64
		var lastErr error
		for {
			published, lastErr = policyRolloverObserveV2(ctx, p, io, member)
			if lastErr == nil && published != 0 {
				break
			}
			// Only authenticated absence can proceed to a send. An exhausted
			// budget still gets the read above, so late finality can finish resume.
			if !errors.Is(lastErr, validatorcomponent.ErrValidatorEvidenceAbsent) || verified != nil || attempts >= p.MaximumAttempts {
				break
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := journal.Append(JournalEntry{DeploymentID: p.DeploymentID, PlanHash: p.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent}); err != nil {
				return nil, err
			}
			attempts++
			operation, cancel := context.WithTimeout(ctx, time.Duration(p.AttemptTimeoutSeconds)*time.Second)
			sendErr := io.Send(operation, action, member)
			cancel()
			published, lastErr = policyRolloverObserveV2(ctx, p, io, member)
			if lastErr == nil && published != 0 {
				break
			}
			lastErr = errors.Join(sendErr, lastErr)
			if err := journal.Append(JournalEntry{DeploymentID: p.DeploymentID, PlanHash: p.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFailed, Error: lastErr.Error()}); err != nil {
				return nil, err
			}
			// Uncertain post-send reads do not justify another broadcast. A later
			// invocation reconciles again and reuses the keeper's exact signed bytes.
			if !errors.Is(lastErr, validatorcomponent.ErrValidatorEvidenceAbsent) {
				break
			}
		}

		if lastErr != nil || published == 0 {
			return nil, errors.Join(fmt.Errorf("rollover %s has no exact finalized publication after %d of %d attempts", action.ID, attempts, p.MaximumAttempts), lastErr)
		}
		digest, err := member.Activation.Digest()
		if err != nil {
			return nil, err
		}
		publication := policyRolloverPublicationV2{Schema: "urnetwork-sim-policy-rollover-publication-v2", PlanHash: p.PlanHash, ValidatorID: member.ValidatorId, NoID: member.NoId, ActivationHash: fmt.Sprintf("0x%x", digest), PublishedBlock: published}
		if err := persistPolicyRolloverPostconditionV2(ctx, p, journal, action, &publication, limit); err != nil {
			return nil, err
		}
		publications = append(publications, publication)
	}
	return publications, ctx.Err()
}
