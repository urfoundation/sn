// An already issued signature is a liability independently of a local basket
// decision. Recovery retains its exact bytes without signing or broadcasting.
package main

import (
	"context"
	"encoding/hex"
	"errors"
)

// The caller supplies only authenticated public bytes from original custody or
// submission state. A larger durable broadcast floor preserves spent attempts;
// it never grants the receipt-only recovery branch another transport allowance.
func (self *rootServiceOwner) retainIssuedSignature(ctx context.Context, signature []byte, broadcasts uint8) error {
	if ctx == nil {
		return errors.New("root signature recovery requires a context")
	}
	select {
	case self.ownerCh <- struct{}{}:
		defer func() { <-self.ownerCh }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	record, err := self.load()
	if err != nil {
		return err
	}
	action := &record.Action
	raw, err := action.Action.signed(signature)
	if err != nil || broadcasts > action.Action.Scope.MaxBroadcasts {
		return errors.Join(errors.New("root recovered signature or broadcast floor differs from the original approved action"), err)
	}
	encoded := hex.EncodeToString(signature)
	if action.Signature != "" {
		if action.Signature != encoded {
			return errors.New("root recovery cannot replace the original native signature")
		}
		if broadcasts <= action.Broadcasts {
			return ctx.Err()
		}
	} else {
		action.Signature, action.RawExtrinsic, action.ExtrinsicHash = encoded, "0x"+hex.EncodeToString(raw), rootExtrinsicHash(raw)
		action.Phase = "signed"
	}
	action.Broadcasts = max(action.Broadcasts, broadcasts)
	if action.Reconciliation == nil {
		record.Phase = "active"
		if action.Broadcasts != 0 {
			action.Phase = "pending"
		}
	}
	if record.Decision != nil && action.LastFinalized == 0 {
		action.LastFinalized = record.Decision.Observation.Position.FinalizedNumber
		action.LastFinalizedHash = record.Decision.Observation.Position.FinalizedHash
	}
	record.RecoveredSignature = true
	action.ContentHash = ""
	action.ContentHash = rootObjectHash(*action)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := self.persist(record); err != nil {
		return err
	}
	return ctx.Err()
}
