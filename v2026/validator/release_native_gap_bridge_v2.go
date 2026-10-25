//go:build linux || darwin

package validator

// A native gap bridge lets a production validator resume after it missed
// native epochs without changing its signed config. The operator pins the
// exact bridge bytes by SHA-256 on the command line. Each edge it names admits
// one forward step only: from the retained applied intent at last_native_epoch
// whose measurement artifact is last_artifact_hash, to the first intent at
// native epoch first_native_epoch or later. Missed epochs have no intent,
// measurement or EMA observation; the head EMA folds the next actual input
// once. Every signature, cut, terminal and head-lineage check still applies.
//
// A v1 file names exactly one edge. A v2 file lists every edge in history
// order under the same flags and one pin, so a later gap can be bridged
// without dropping an edge the retained history has already crossed: each
// crossed edge stays exactly as it was, and only the last edge may be pending.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
)

const ReleaseNativeGapBridgeSchema = "urnetwork-validator-native-gap-bridge-v1"
const ReleaseNativeGapBridgeEdgesSchema = "urnetwork-validator-native-gap-bridge-v2"
const ReleaseNativeGapBridgeMaximumBytes = 4096

// The canonical wire form is json.MarshalIndent with a two-space indent, in
// this field order, followed by one newline. Pins cover those exact bytes.
// A v2 file decodes into the same type with the edge fields left zero; its
// edges are reachable only through Edges.
type ReleaseNativeGapBridge struct {
	Schema           string `json:"schema"`
	ChainID          uint64 `json:"chain_id"`
	DeploymentID     string `json:"deployment_id"`
	ValidatorID      uint64 `json:"validator_id"`
	LastNativeEpoch  uint64 `json:"last_native_epoch"`
	LastArtifactHash string `json:"last_artifact_hash"`
	FirstNativeEpoch uint64 `json:"first_native_epoch"`

	edges []ReleaseNativeGapBridgeEdge // v2 only, in history order
}

// One admitted forward step, with the same meaning as the v1 fields.
type ReleaseNativeGapBridgeEdge struct {
	LastNativeEpoch  uint64 `json:"last_native_epoch"`
	LastArtifactHash string `json:"last_artifact_hash"`
	FirstNativeEpoch uint64 `json:"first_native_epoch"`
}

// The v2 canonical wire form, under the same encoding rule as v1.
type releaseNativeGapBridgeEdgesWire struct {
	Schema       string                       `json:"schema"`
	ChainID      uint64                       `json:"chain_id"`
	DeploymentID string                       `json:"deployment_id"`
	ValidatorID  uint64                       `json:"validator_id"`
	Edges        []ReleaseNativeGapBridgeEdge `json:"edges"`
}

// Only startup replay creates this owner, after it finds every edge's applied
// predecessor in the retained intents. Its edge checks are stateless: once
// history moves past a predecessor, no later edge can satisfy its edge.
type releaseNativeGapBridgeV2 struct {
	bridge  ReleaseNativeGapBridge
	crossed bool // the last edge has been crossed
}

// The path is read once from a bounded regular file that is not a symlink.
func ReadReleaseNativeGapBridge(path, expectedSHA256 string) (result *ReleaseNativeGapBridge, resultErr error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("native gap bridge path must be canonical and absolute")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("native gap bridge: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
		if resultErr != nil {
			result = nil
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("native gap bridge: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > ReleaseNativeGapBridgeMaximumBytes {
		return nil, errors.New("native gap bridge is not a bounded regular file")
	}
	encoded, err := io.ReadAll(io.LimitReader(file, ReleaseNativeGapBridgeMaximumBytes+1))
	if err != nil {
		return nil, fmt.Errorf("native gap bridge: %w", err)
	}
	return DecodeReleaseNativeGapBridge(encoded, expectedSHA256)
}

// Decoding checks the pin and the canonical wire. It grants nothing until the
// config owner and then the retained intent history match it.
func DecodeReleaseNativeGapBridge(encoded []byte, expectedSHA256 string) (*ReleaseNativeGapBridge, error) {
	if _, err := parseReleaseContentHash(expectedSHA256); err != nil {
		return nil, fmt.Errorf("native gap bridge sha256 pin: %w", err)
	}
	if len(encoded) == 0 || len(encoded) > ReleaseNativeGapBridgeMaximumBytes || ReleaseMeasurementContentHash(encoded) != expectedSHA256 {
		return nil, errors.New("native gap bridge bytes differ from the pinned sha256")
	}
	// Only an exact v2 schema selects the edge list; every other body takes
	// the unchanged v1 decoder and its refusals.
	var head struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(encoded, &head) == nil && head.Schema == ReleaseNativeGapBridgeEdgesSchema {
		return decodeReleaseNativeGapBridgeEdges(encoded)
	}
	var bridge ReleaseNativeGapBridge
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bridge); err != nil {
		return nil, fmt.Errorf("native gap bridge: %w", err)
	}
	canonical, err := json.MarshalIndent(&bridge, "", "  ")
	if err != nil || !bytes.Equal(encoded, append(canonical, '\n')) {
		return nil, errors.Join(errors.New("native gap bridge is not canonical JSON: two-space indent, schema field order, one trailing newline"), err)
	}
	if bridge.Schema != ReleaseNativeGapBridgeSchema || bridge.ChainID == 0 || strings.TrimSpace(bridge.DeploymentID) == "" || bridge.ValidatorID == 0 || bridge.LastNativeEpoch == 0 {
		return nil, errors.New("native gap bridge schema, chain, deployment, validator or last native epoch is absent")
	}
	if _, err := parseReleaseContentHash(bridge.LastArtifactHash); err != nil {
		return nil, fmt.Errorf("native gap bridge last_artifact_hash: %w", err)
	}
	if bridge.LastNativeEpoch >= ^uint64(0)-1 || bridge.FirstNativeEpoch <= bridge.LastNativeEpoch+1 || bridge.FirstNativeEpoch == ^uint64(0) {
		return nil, errors.New("native gap bridge first_native_epoch must be at least last_native_epoch + 2")
	}
	return &bridge, nil
}

// Every edge has the v1 edge rules. A later edge's predecessor is at or after
// the earlier edge's first native epoch, so edges cannot repeat or overlap.
func decodeReleaseNativeGapBridgeEdges(encoded []byte) (*ReleaseNativeGapBridge, error) {
	var wire releaseNativeGapBridgeEdgesWire
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return nil, fmt.Errorf("native gap bridge: %w", err)
	}
	canonical, err := json.MarshalIndent(&wire, "", "  ")
	if err != nil || !bytes.Equal(encoded, append(canonical, '\n')) {
		return nil, errors.Join(errors.New("native gap bridge is not canonical JSON: two-space indent, schema field order, one trailing newline"), err)
	}
	if wire.Schema != ReleaseNativeGapBridgeEdgesSchema || wire.ChainID == 0 || strings.TrimSpace(wire.DeploymentID) == "" || wire.ValidatorID == 0 || len(wire.Edges) == 0 {
		return nil, errors.New("native gap bridge schema, chain, deployment, validator or edges are absent")
	}
	for index, edge := range wire.Edges {
		if edge.LastNativeEpoch == 0 {
			return nil, fmt.Errorf("native gap bridge edge %d last native epoch is absent", index)
		}
		if _, err := parseReleaseContentHash(edge.LastArtifactHash); err != nil {
			return nil, fmt.Errorf("native gap bridge edge %d last_artifact_hash: %w", index, err)
		}
		if edge.LastNativeEpoch >= ^uint64(0)-1 || edge.FirstNativeEpoch <= edge.LastNativeEpoch+1 || edge.FirstNativeEpoch == ^uint64(0) {
			return nil, fmt.Errorf("native gap bridge edge %d first_native_epoch must be at least last_native_epoch + 2", index)
		}
		if index > 0 && edge.LastNativeEpoch < wire.Edges[index-1].FirstNativeEpoch {
			return nil, fmt.Errorf("native gap bridge edge %d starts at native epoch %d, before the previous edge's first native epoch %d", index, edge.LastNativeEpoch, wire.Edges[index-1].FirstNativeEpoch)
		}
	}
	return &ReleaseNativeGapBridge{Schema: wire.Schema, ChainID: wire.ChainID, DeploymentID: wire.DeploymentID, ValidatorID: wire.ValidatorID, edges: slices.Clone(wire.Edges)}, nil
}

// The admitted edges in history order: the v1 fields as one edge, or the v2
// list. Callers receive a copy.
func (self *ReleaseNativeGapBridge) Edges() []ReleaseNativeGapBridgeEdge {
	if self == nil {
		return nil
	}
	return slices.Clone(self.edgeList())
}

func (self *ReleaseNativeGapBridge) edgeList() []ReleaseNativeGapBridgeEdge {
	if len(self.edges) != 0 {
		return self.edges
	}
	return []ReleaseNativeGapBridgeEdge{{LastNativeEpoch: self.LastNativeEpoch, LastArtifactHash: self.LastArtifactHash, FirstNativeEpoch: self.FirstNativeEpoch}}
}

// Owners never share the edge list with the decoded value or each other.
func (self *ReleaseNativeGapBridge) clone() ReleaseNativeGapBridge {
	owned := *self
	owned.edges = slices.Clone(self.edges)
	return owned
}

// The bridge names its owner; the signed config, its hash and its approval
// are not changed. Other gap authorities cannot be combined with it.
func (self *ReleaseNativeGapBridge) configure(cfg *ReleaseConfig) error {
	if self == nil || cfg == nil {
		return errors.New("native gap bridge or its config is absent")
	}
	if self.ChainID != cfg.ChainID || self.DeploymentID != cfg.DeploymentID || self.ValidatorID != cfg.ValidatorID {
		return fmt.Errorf("native gap bridge names chain %d, deployment %q, validator %d; the config has chain %d, deployment %q, validator %d",
			self.ChainID, self.DeploymentID, self.ValidatorID, cfg.ChainID, cfg.DeploymentID, cfg.ValidatorID)
	}
	if cfg.ProvisionalDeferClosedNativeInput || cfg.ProvisionalRuntimeCompatibility != "" || cfg.historyAdoptionV2 != nil || cfg.nativeGapBridge != nil {
		return errors.New("native gap bridge cannot combine with provisional or adopted history gaps")
	}
	owned := self.clone()
	cfg.nativeGapBridge = &owned
	return nil
}

// Startup binds the bridge to the retained intents before any worker starts.
// Each edge's applied predecessor appears once, in edge order. Every edge but
// the last must already be crossed: its predecessor is followed directly by
// the bridged intent at first_native_epoch or later. The last edge's
// predecessor is either the current intent, so that edge is still pending, or
// crossed the same way. Any other history refuses startup while the bridge is
// supplied, and the ordinary successor replay still refuses any gap that no
// listed edge names.
func (self *ReleaseNativeGapBridge) matchHistory(ctx context.Context, all []*SteeringIntent) (*releaseNativeGapBridgeV2, error) {
	if self == nil {
		return nil, nil
	}
	if ctx == nil {
		return nil, errors.New("native gap bridge history owner is incomplete")
	}
	edges := self.edgeList()
	owner := &releaseNativeGapBridgeV2{bridge: self.clone()}
	after := 0
	for edgeIndex, edge := range edges {
		found := -1
		for index, intent := range all {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if intent != nil && intent.SubnetEpoch == edge.LastNativeEpoch && intent.MeasurementArtifactHash == edge.LastArtifactHash {
				if found >= 0 {
					return nil, errors.New("native gap bridge predecessor repeats in the retained intent history")
				}
				found = index
			}
		}
		if found < 0 {
			return nil, fmt.Errorf("native gap bridge predecessor (native epoch %d, %s) is not in the retained intent history", edge.LastNativeEpoch, edge.LastArtifactHash)
		}
		if found < after {
			return nil, fmt.Errorf("native gap bridge edge from native epoch %d is out of retained history order", edge.LastNativeEpoch)
		}
		if all[found].Status != "applied" {
			return nil, fmt.Errorf("native gap bridge predecessor at native epoch %d is %q, not applied", edge.LastNativeEpoch, all[found].Status)
		}
		crossed := found+1 < len(all)
		if crossed {
			if next := all[found+1]; next == nil || next.SubnetEpoch < edge.FirstNativeEpoch {
				return nil, fmt.Errorf("native gap bridge predecessor is not followed by an intent at native epoch %d or later", edge.FirstNativeEpoch)
			}
		} else if edgeIndex+1 < len(edges) {
			return nil, fmt.Errorf("native gap bridge edge from native epoch %d is not crossed, but a later edge follows it", edge.LastNativeEpoch)
		}
		after, owner.crossed = found+1, crossed
	}
	return owner, ctx.Err()
}

// The listed intent edges. Retries, consecutive successors and any unlisted
// gap use the ordinary successor rules.
func (self *releaseNativeGapBridgeV2) allowsIntentEdge(previous, current *SteeringIntent) bool {
	if self == nil || previous == nil || current == nil || previous.Status != "applied" {
		return false
	}
	for _, edge := range self.bridge.edgeList() {
		if previous.SubnetEpoch == edge.LastNativeEpoch && previous.MeasurementArtifactHash == edge.LastArtifactHash && current.SubnetEpoch >= edge.FirstNativeEpoch {
			return true
		}
	}
	return false
}

// The same edges between the signed artifacts. The caller still authenticates
// both endpoints, their cuts, terminals and head EMA lineage.
func (self *releaseNativeGapBridgeV2) allowsArtifactEdge(previousHash string, previous, current *ReleaseMeasurementArtifact) bool {
	if self == nil || previous == nil || current == nil || current.PreviousArtifactHash != previousHash {
		return false
	}
	for _, edge := range self.bridge.edgeList() {
		if previousHash == edge.LastArtifactHash && previous.SubnetEpoch == edge.LastNativeEpoch && current.SubnetEpoch >= edge.FirstNativeEpoch {
			return true
		}
	}
	return false
}

// Startup admits this owner only while the store's state at an edge's
// last_native_epoch belongs to its matched predecessor; committing the
// bridged epoch consumes that edge.
func (self *releaseNativeGapBridgeV2) allowsHeadEMAEdge(last, epoch uint64) bool {
	if self == nil {
		return false
	}
	for _, edge := range self.bridge.edgeList() {
		if last == edge.LastNativeEpoch && epoch >= edge.FirstNativeEpoch {
			return true
		}
	}
	return false
}

// Before the pending edge's first_native_epoch its predecessor stays the
// final decision: release idles with the ordinary already-final result
// instead of failing. Startup has already required every earlier edge to be
// crossed, so only the last edge can be pending.
func (self *releaseNativeGapBridgeV2) requireFirstEpoch(current *SteeringIntent, epoch uint64) error {
	if self == nil || current == nil {
		return nil
	}
	edges := self.bridge.edgeList()
	pending := edges[len(edges)-1]
	if current.SubnetEpoch != pending.LastNativeEpoch || current.MeasurementArtifactHash != pending.LastArtifactHash {
		return nil
	}
	if epoch < pending.FirstNativeEpoch {
		return ErrSteeringAlreadyFinal
	}
	return nil
}
