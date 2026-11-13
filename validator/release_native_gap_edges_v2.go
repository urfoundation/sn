//go:build linux || darwin

// A missed native epoch used to wait for an operator, who read the applied intent
// from this validator's state, chose the next native epoch and pinned a file that
// repeats those values. Nothing in that file is a fact the validator lacks, and it
// is not signed. This owner applies the same rule itself, when the chain confirms
// what the operator would have read: the applied intent is the last decision this
// hotkey made public. The edge it records has the bridge's exact meaning and the
// next input folds once, as the head-summary specification says a gap does. A state
// restored from an older backup, an unfinished intent, a very long gap or an expired
// approval still holds for a person.
//
// Recorded edges live beside the intent history they describe, in the v2 bridge wire,
// and join any pinned edges at the next start. The process restarts to admit one, so
// every check that reads a bridge sees exactly what a pinned file would have given it.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

const (
	releaseNativeGapOwnEdgesName         = "native-gap-edges.json"
	releaseNativeGapOwnEdgesMaximumBytes = 64 * 1024
	// About twelve days. The longest gap ever bridged was 53 missed native epochs.
	// Past this a person decides, whatever the chain says.
	releaseNativeGapOwnEdgeMaximumEpochs = 240
)

// Why the rule did not record an edge. Closed labels, printed in the hold's line.
const (
	nativeGapEdgeNotPlainProduction = "not_plain_production"
	nativeGapEdgePredecessorStatus  = "predecessor_not_applied"
	nativeGapEdgeSourceSlot         = "source_slot_differs"
	nativeGapEdgeTooLong            = "gap_exceeds_bound"
	nativeGapEdgeApprovalWindow     = "approval_window_ends"
)

// Ends the steering loop once, so the next start reads the edge. It is the one
// process exit steering asks for on purpose, and the crossing it admits is logged.
type productionNativeGapEdgeRecorded struct {
	edge ReleaseNativeGapBridgeEdge
}

func (self *productionNativeGapEdgeRecorded) Error() string {
	return fmt.Sprintf("native gap edge recorded from native epoch %d (%s) to the first intent at native epoch %d or later; restarting to admit it", self.edge.LastNativeEpoch, self.edge.LastArtifactHash, self.edge.FirstNativeEpoch)
}

// The whole rule, on facts the caller has read: empty when an edge may be recorded.
func releaseNativeGapOwnEdgeRefusal(plainProduction bool, current *SteeringIntent, nativeEpoch uint64, approved func(uint64) bool, observed *crv4.FinalizedCommitment) string {
	if !plainProduction || approved == nil {
		return nativeGapEdgeNotPlainProduction
	}
	if current == nil || current.Status != "applied" || current.Prepared == nil || current.Prepared.SourceCommitment == nil || current.FinalizedBlock == 0 {
		return nativeGapEdgePredecessorStatus
	}
	if nativeEpoch <= current.SubnetEpoch+1 || nativeEpoch-current.SubnetEpoch > releaseNativeGapOwnEdgeMaximumEpochs {
		return nativeGapEdgeTooLong
	}
	if !approved(nativeEpoch + 1) {
		return nativeGapEdgeApprovalWindow
	}
	// No later decision of this hotkey is public: the slot still holds exactly
	// this intent's commitment, written at exactly its finalized block.
	if observed == nil || current.Prepared.SourceCommitment.Hash != releaseHex32(observed.Hash) || current.FinalizedBlock != observed.CommitmentBlock {
		return nativeGapEdgeSourceSlot
	}
	return ""
}

// Absent is ordinary. A present file must be exactly the canonical wire; anything
// else refuses startup rather than admitting or dropping an edge.
func readReleaseNativeGapOwnEdges(stateDir string) (*ReleaseNativeGapBridge, error) {
	path := filepath.Join(stateDir, releaseNativeGapOwnEdgesName)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("native gap own edges: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > releaseNativeGapOwnEdgesMaximumBytes {
		return nil, errors.New("native gap own edges are not a bounded regular file")
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("native gap own edges: %w", err)
	}
	bridge, err := decodeReleaseNativeGapBridgeEdges(encoded)
	if err != nil {
		return nil, fmt.Errorf("native gap own edges: %w", err)
	}
	return bridge, nil
}

// One new edge after those already recorded, written atomically in the v2 wire.
func recordReleaseNativeGapOwnEdge(cfg *ReleaseConfig, edge ReleaseNativeGapBridgeEdge) error {
	if cfg == nil || !filepath.IsAbs(cfg.StateDir) {
		return errors.New("native gap own edge has no absolute state directory")
	}
	wire := releaseNativeGapBridgeEdgesWire{Schema: ReleaseNativeGapBridgeEdgesSchema, ChainID: cfg.ChainID, DeploymentID: cfg.DeploymentID, ValidatorID: cfg.ValidatorID}
	existing, err := readReleaseNativeGapOwnEdges(cfg.StateDir)
	if err != nil {
		return err
	}
	if existing != nil {
		wire.Edges = existing.Edges()
	}
	wire.Edges = append(wire.Edges, edge)
	encoded, err := json.MarshalIndent(&wire, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if len(encoded) > releaseNativeGapOwnEdgesMaximumBytes {
		return errors.New("native gap own edges exceed their bound")
	}
	// The same decoder the next start uses refuses an edge it would not admit.
	if _, err := decodeReleaseNativeGapBridgeEdges(bytes.Clone(encoded)); err != nil {
		return err
	}
	return atomicStateWrite(filepath.Join(cfg.StateDir, releaseNativeGapOwnEdgesName), encoded, 0o600)
}

// Pinned and recorded edges in history order. The same predecessor listed twice
// keeps the earlier first epoch, because either edge alone admits an intent from
// there on; two different artifacts for one predecessor refuse startup.
func mergeReleaseNativeGapBridges(pinned, own *ReleaseNativeGapBridge) (*ReleaseNativeGapBridge, error) {
	if own == nil {
		return pinned, nil
	}
	if pinned == nil {
		return own, nil
	}
	if pinned.ChainID != own.ChainID || pinned.DeploymentID != own.DeploymentID || pinned.ValidatorID != own.ValidatorID {
		return nil, errors.New("native gap own edges name another chain, deployment or validator than the pinned bridge")
	}
	edges := append(pinned.Edges(), own.Edges()...)
	sort.SliceStable(edges, func(i, j int) bool { return edges[i].LastNativeEpoch < edges[j].LastNativeEpoch })
	merged := make([]ReleaseNativeGapBridgeEdge, 0, len(edges))
	for _, edge := range edges {
		if len(merged) == 0 {
			merged = append(merged, edge)
			continue
		}
		last := &merged[len(merged)-1]
		if last.LastNativeEpoch == edge.LastNativeEpoch {
			if last.LastArtifactHash != edge.LastArtifactHash {
				return nil, fmt.Errorf("native gap edges name two artifacts for native epoch %d", edge.LastNativeEpoch)
			}
			last.FirstNativeEpoch = min(last.FirstNativeEpoch, edge.FirstNativeEpoch)
			continue
		}
		if edge.LastNativeEpoch < last.FirstNativeEpoch {
			return nil, fmt.Errorf("native gap edge from native epoch %d starts before the previous edge's first native epoch %d", edge.LastNativeEpoch, last.FirstNativeEpoch)
		}
		merged = append(merged, edge)
	}
	return &ReleaseNativeGapBridge{Schema: ReleaseNativeGapBridgeEdgesSchema, ChainID: pinned.ChainID, DeploymentID: pinned.DeploymentID, ValidatorID: pinned.ValidatorID, edges: slices.Clone(merged)}, nil
}

// Records this gap's edge when the rule admits it. A slot that cannot be read
// retries on the next poll, without spending failures. A refusal stays a hold, and
// its line says why. The returned marker ends the process so the edge takes effect.
func (self *ReleaseSteerer) recordNativeGapEdge(ctx context.Context, current *SteeringIntent, hold *productionNativeGapHold, nativeHash types.Hash) error {
	if hold == nil {
		return nil
	}
	plain := isOwnerRecycleProductionConfig(self.cfg) && self.cfg.historyAdoptionV2 == nil && !self.cfg.ProvisionalDeferClosedNativeInput && self.runtimeV2 != nil && self.runtimeV2.history != nil && !self.runtimeV2.history.retainedStartup
	var approved func(uint64) bool
	if plain {
		envelope, err := ownerRecycleProductionApproval(self.cfg)
		if err != nil {
			return hold.attemptFailed(err)
		}
		approved = func(epoch uint64) bool { return ownerRecycleDecisionEpochApproved(&envelope.Approval, epoch) }
	}
	observed, err := self.native.SourceCommitmentSlotAtContext(ctx, self.cfg.Netuid, self.hotkey.PublicKey(), nativeHash)
	if err != nil {
		return hold.attemptFailed(err)
	}
	hold.edgeRefused = releaseNativeGapOwnEdgeRefusal(plain, current, hold.nativeEpoch, approved, observed)
	if hold.edgeRefused != "" {
		return nil
	}
	edge := ReleaseNativeGapBridgeEdge{LastNativeEpoch: current.SubnetEpoch, LastArtifactHash: current.MeasurementArtifactHash, FirstNativeEpoch: hold.nativeEpoch + 1}
	if err := recordReleaseNativeGapOwnEdge(self.cfg, edge); err != nil {
		return err
	}
	return &productionNativeGapEdgeRecorded{edge: edge}
}
