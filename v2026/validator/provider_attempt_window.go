//go:build linux || darwin

// Complete validator ownership and original stream replay are separate from
// operator artifact agreement. Every configured owner, including an idle one,
// must supply its original closed census before any provider counters escape.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

const ProviderAttemptWindowSchema = "urnetwork-provider-attempt-window-v1"

// A locator conveys no authority. Original activation, epoch geometry and
// public origins come from the separately supplied owner expectations.
type ProviderAttemptWindowMember struct {
	Hotkey   [32]byte                               `json:"hotkey"`
	Manifest ValidatorEvidencePublicationV2Manifest `json:"manifest"`
}

// Registry revisions and locators are portable originals. The real immutable
// v2 record/proof objects remain at the two retained public origins.
type ProviderAttemptWindow struct {
	Schema          string                             `json:"schema"`
	RegistryHistory []protocol.ProviderAttemptRegistry `json:"registry_history"`
	Members         []ProviderAttemptWindowMember      `json:"members"`
}

// The containing chain owner supplies these historical authorities before
// reading the candidate. Candidate headers never populate these expectations.
type ProviderAttemptValidatorOptions struct {
	Publication ValidatorEvidencePublicationV2ReadOptions
	Settlement  AttemptSettlementV2Options
	// Exact original transport bytes only; the full public decoder still runs.
	RetainedMetadata *[2]ValidatorEvidenceRetainedReplicaV2
}

// One explicit aggregate allowance bounds the complete cross-validator read;
// multiplying a per-validator limit does not grant another global allowance.
type ProviderAttemptWindowOptions struct {
	Registry         protocol.ProviderAttemptRegistryExpectation
	Window           protocol.ValidatorEvidenceWindow
	Validators       map[[32]byte]ProviderAttemptValidatorOptions
	MaxProviders     uint64
	MaxMetadataBytes uint64
	MaxRecordBytes   uint64
	MaxRecords       uint64
}

// Counts are recomputed from original signed terminal attempts. Network/wallet
// identity and chain eligibility are deliberately not invented from these rows.
type VerifiedProviderAttemptRow struct {
	NoId           uint64                      `json:"no_id"`
	ClientId       [16]byte                    `json:"client_id"`
	Assignments    uint64                      `json:"assignments"`
	Confirmations  uint64                      `json:"confirmations"`
	LatencyBuckets [statsLatencyBuckets]uint64 `json:"latency_buckets"`
}

// This is an operation-local result, never a transferable authentication token.
// CutCensusComplete proves the independently expected owner/stream census only.
// Lost pre-assignment requests need their distinct original request inventory.
type VerifiedProviderAttemptWindow struct {
	Domain                protocol.ProviderAttemptDomain   `json:"domain"`
	RegistryHash          [32]byte                         `json:"registry_hash"`
	WindowHash            [32]byte                         `json:"window_hash"`
	Window                protocol.ValidatorEvidenceWindow `json:"window"`
	CutCensusComplete     bool                             `json:"cut_census_complete"`
	OwnedRequestsComplete bool                             `json:"owned_requests_complete"`
	Providers             []VerifiedProviderAttemptRow     `json:"providers"`
	Validators            uint64                           `json:"validators"`
	OperatorLanes         uint64                           `json:"operator_lanes"`
	CompleteTrails        uint64                           `json:"complete_trails"`
	FailedTrails          uint64                           `json:"failed_trails"`
}

// Stage detached authority and every locator before external reads. No child
// shares a scratch namespace or a mutable map with another replay owner.
func ownProviderAttemptWindow(ctx context.Context, candidate ProviderAttemptWindow, expected ProviderAttemptWindowOptions) (*protocol.ProviderAttemptRegistry, []ProviderAttemptWindowMember, map[[32]byte]ProviderAttemptValidatorOptions, error) {
	if ctx == nil {
		return nil, nil, nil, errors.New("provider attempt window context is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	window := expected.Window
	if window.StartBlock == 0 || window.EndBlock <= window.StartBlock || window.FinalizedBlock < window.EndBlock || window.Subject != (protocol.ValidatorEvidenceSubject{}) || candidate.Schema != ProviderAttemptWindowSchema {
		return nil, nil, nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider attempt window is not the exact closed epoch"))
	}
	registry, err := protocol.VerifyProviderAttemptRegistry(ctx, candidate.RegistryHistory, window.Epoch, expected.Registry)
	if err != nil {
		return nil, nil, nil, err
	}
	if expected.MaxProviders == 0 || expected.MaxMetadataBytes == 0 || expected.MaxRecordBytes == 0 || expected.MaxRecords == 0 {
		return nil, nil, nil, protocol.ErrProviderAttemptsCapacity
	}
	if len(candidate.Members) < len(registry.Owners) || len(expected.Validators) < len(registry.Owners) {
		return nil, nil, nil, protocol.ErrProviderAttemptsUnavailable
	}
	if len(candidate.Members) != len(registry.Owners) || len(expected.Validators) != len(registry.Owners) {
		return nil, nil, nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider attempt validator census adds an owner"))
	}
	owned := make([]ProviderAttemptWindowMember, len(candidate.Members))
	options := make(map[[32]byte]ProviderAttemptValidatorOptions, len(expected.Validators))
	scratchKVs := map[string]bool{}
	type laneKey struct {
		noId uint64
		vpk  [32]byte
	}
	laneOwners := map[laneKey][32]byte{}
	var metadataBytes uint64
	var terminalHash string
	for index, owner := range registry.Owners {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		member := candidate.Members[index]
		option, exists := expected.Validators[owner.Hotkey]
		if !exists {
			return nil, nil, nil, protocol.ErrProviderAttemptsUnavailable
		}
		if member.Hotkey != owner.Hotkey || option.Publication.Window != window || member.Manifest.Epoch != window.Epoch || member.Manifest.Kind != protocol.ValidatorEvidenceClosedCensus || member.Manifest.Origins != option.Publication.Origins {
			return nil, nil, nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider attempt owner, clock or origins differ"))
		}
		if len(option.Publication.Activations) != len(owner.NoIds) || len(option.Settlement.Operators) != len(owner.NoIds) || len(member.Manifest.Members) != len(owner.NoIds) {
			return nil, nil, nil, protocol.ErrProviderAttemptsUnavailable
		}
		option.Publication.Activations = slices.Clone(option.Publication.Activations)
		if option.Publication.Policy != nil {
			policy := *option.Publication.Policy
			policy.Deposit.Tiers = slices.Clone(policy.Deposit.Tiers)
			option.Publication.Policy = &policy
		}
		if option.Publication.PreviousPolicy != nil {
			policy := *option.Publication.PreviousPolicy
			policy.Deposit.Tiers = slices.Clone(policy.Deposit.Tiers)
			option.Publication.PreviousPolicy = &policy
		}
		option.Settlement, err = ownAttemptSettlementV2Options(ctx, option.Settlement)
		if err != nil {
			return nil, nil, nil, err
		}
		for position, noId := range owner.NoIds {
			activation := option.Publication.Activations[position]
			operator, exists := option.Settlement.Operators[noId]
			domain, domainErr := activation.EvidenceDomain()
			if !exists || activation.NoID != noId || activation.Hotkey != owner.Hotkey || member.Manifest.Members[position].NoId != noId || domainErr != nil || !expected.Registry.Domain.MatchesEvidence(domain) || operator.Expected.Activation.Domain != domain || operator.Expected.Activation.Hotkey != owner.Hotkey || operator.Expected.Identity.ValidatorVPK != attemptHex32(activation.VPK) || operator.Expected.Boundary.SettlementEpoch != window.Epoch || operator.Expected.Boundary.EVMBlock != window.EndBlock-1 {
				return nil, nil, nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, domainErr, errors.New("provider attempt activation or complete operator lane differs"))
			}
			if terminalHash != "" && terminalHash != operator.Expected.Boundary.EVMBlockHash {
				return nil, nil, nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider attempt owners use different original terminal block hashes"))
			}
			terminalHash = operator.Expected.Boundary.EVMBlockHash
			path := operator.Measurement.Replay.ScratchDirectory
			if scratchKVs[path] {
				return nil, nil, nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider attempt owners share replay scratch"))
			}
			scratchKVs[path] = true
			key := laneKey{noId: noId, vpk: activation.VPK}
			if prior, exists := laneOwners[key]; exists && prior != owner.Hotkey {
				return nil, nil, nil, errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider attempt signing lane belongs to multiple validator owners"))
			}
			laneOwners[key] = owner.Hotkey
		}
		if err := member.Manifest.validate(option.Publication.Bounds.MaxClosureBytes, option.Publication.Bounds.MaxParticipants); err != nil {
			return nil, nil, nil, err
		}
		encoded, err := json.Marshal(member.Manifest)
		if err != nil {
			return nil, nil, nil, err
		}
		if uint64(len(encoded)) > expected.MaxMetadataBytes-metadataBytes {
			return nil, nil, nil, protocol.ErrProviderAttemptsCapacity
		}
		metadataBytes += uint64(len(encoded))
		member.Manifest.Members = slices.Clone(member.Manifest.Members)
		owned[index], options[owner.Hotkey] = member, option
	}
	return registry, owned, options, ctx.Err()
}

// Read both original metadata replicas and replay every actual record/proof
// stream. An error/cancellation at the final owner returns no partial counters.
func VerifyProviderAttemptWindow(ctx context.Context, candidate ProviderAttemptWindow, expected ProviderAttemptWindowOptions) (result *VerifiedProviderAttemptWindow, resultErr error) {
	if ctx == nil {
		return nil, errors.New("provider attempt window context is absent")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	registry, members, options, err := ownProviderAttemptWindow(ctx, candidate, expected)
	if err != nil {
		return nil, err
	}
	registryHash, err := registry.Hash()
	if err != nil {
		return nil, err
	}
	result = &VerifiedProviderAttemptWindow{Domain: expected.Registry.Domain, RegistryHash: registryHash, Window: expected.Window, Validators: uint64(len(members))}
	type providerKey struct {
		noId     uint64
		clientId connect.Id
	}
	providerKVs := map[providerKey]*VerifiedProviderAttemptRow{}
	type terminalKey struct {
		noId    uint64
		trailId connect.Id
	}
	terminalOwners := map[terminalKey][32]byte{}
	var metadataBytes, recordBytes, recordCount uint64
	commitment := sha256.New()
	_, _ = commitment.Write([]byte(ProviderAttemptWindowSchema + "\x00"))
	_, _ = commitment.Write(registryHash[:])
	// Later finality observations do not rename the same original closed window.
	committedWindow := expected.Window
	committedWindow.FinalizedBlock = committedWindow.EndBlock
	windowBytes, err := json.Marshal(committedWindow)
	if err != nil {
		return nil, err
	}
	_, _ = commitment.Write(windowBytes)
	for _, member := range members {
		option := options[member.Hotkey]
		publication, err := readValidatorEvidencePublicationV2(ctx, &member.Manifest, option.Publication, option.RetainedMetadata)
		if err != nil {
			return nil, err
		}
		if uint64(len(publication.Census)) > expected.MaxMetadataBytes-metadataBytes {
			return nil, protocol.ErrProviderAttemptsCapacity
		}
		metadataBytes += uint64(len(publication.Census))
		_, _ = commitment.Write(member.Hotkey[:])
		_, _ = commitment.Write(publication.CensusHash[:])
		closure := &AttemptSettlementClosureV2{Schema: AttemptSettlementClosureV2Schema, Epoch: expected.Window.Epoch}
		for _, payload := range publication.Members {
			for _, raw := range [][]byte{payload.Payload, payload.SignedArtifact, payload.Calldata} {
				if uint64(len(raw)) > expected.MaxMetadataBytes-metadataBytes {
					return nil, protocol.ErrProviderAttemptsCapacity
				}
				metadataBytes += uint64(len(raw))
			}
			var transition AttemptSettlementTransitionV2
			if err := decodeValidatorEvidencePublicationV2Json(ctx, payload.Payload, option.Settlement.MaxTransitionBytes, &transition); err != nil {
				return nil, err
			}
			for _, reference := range []AttemptStreamV2Reference{transition.Cut.Records, transition.Cut.Proofs} {
				if reference.DataBytes > expected.MaxRecordBytes-recordBytes || reference.ItemCount > expected.MaxRecords-recordCount {
					return nil, protocol.ErrProviderAttemptsCapacity
				}
				recordBytes += reference.DataBytes
				recordCount += reference.ItemCount
			}
			closure.Transitions = append(closure.Transitions, &transition)
		}
		verified, err := verifyAttemptSettlementClosureV2(ctx, closure, option.Settlement, func(noId uint64, record AttemptRecord) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if record.Disposition == AttemptDispositionPending {
				return nil
			}
			key := terminalKey{noId: noId, trailId: record.TrailID}
			if _, exists := terminalOwners[key]; exists {
				return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider attempt original terminal appears under multiple owners"))
			}
			if uint64(len(terminalOwners)) >= expected.MaxRecords {
				return protocol.ErrProviderAttemptsCapacity
			}
			terminalOwners[key] = member.Hotkey
			for _, assignment := range record.Assignments {
				key := providerKey{noId: noId, clientId: assignment.NextHop}
				row := providerKVs[key]
				if row == nil {
					if uint64(len(providerKVs)) >= expected.MaxProviders {
						return protocol.ErrProviderAttemptsCapacity
					}
					row = &VerifiedProviderAttemptRow{NoId: noId, ClientId: [16]byte(assignment.NextHop)}
					providerKVs[key] = row
				}
				if row.Assignments == ^uint64(0) {
					return protocol.ErrProviderAttemptsCapacity
				}
				row.Assignments++
				if assignment.Confirmed {
					if !assignment.HasLatency || int(assignment.LatencyBucket) >= len(row.LatencyBuckets) || row.Confirmations == ^uint64(0) || row.LatencyBuckets[assignment.LatencyBucket] == ^uint64(0) {
						return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider confirmation histogram differs"))
					}
					row.Confirmations++
					row.LatencyBuckets[assignment.LatencyBucket]++
				}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("provider attempt validator %x: %w", member.Hotkey, err)
		}
		result.OperatorLanes += uint64(len(verified.Operators))
		for _, operator := range verified.Operators {
			if operator.Replay.CompleteCount > ^uint64(0)-result.CompleteTrails || operator.Replay.FailedCount > ^uint64(0)-result.FailedTrails {
				return nil, protocol.ErrProviderAttemptsCapacity
			}
			result.CompleteTrails += operator.Replay.CompleteCount
			result.FailedTrails += operator.Replay.FailedCount
		}
	}
	result.Providers = make([]VerifiedProviderAttemptRow, 0, len(providerKVs))
	for _, row := range providerKVs {
		result.Providers = append(result.Providers, *row)
	}
	sort.Slice(result.Providers, func(i, j int) bool {
		left, right := result.Providers[i], result.Providers[j]
		if left.NoId != right.NoId {
			return left.NoId < right.NoId
		}
		return bytes.Compare(left.ClientId[:], right.ClientId[:]) < 0
	})
	copy(result.WindowHash[:], commitment.Sum(nil))
	result.CutCensusComplete = true
	return result, nil
}
