// Consensus coverage is derived from original GRANDPA certificates and raw
// Frontier headers. A matching receipt root or an RPC's finalized label cannot
// enroll authority. Quiet vault blocks and original carry/root boundaries are
// part of the coverage census, including after their payloads become cold.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urnetwork/server/v2026/strecovery"
	"golang.org/x/crypto/blake2b"
)

// This compact head is reproduced from the complete held snapshot lineage.
// Certified may be ahead of the selected economic cursor; it never moves that
// cursor or declares the intervening economic work complete.
type economicConservationFinalityHead struct {
	AuthorityHash string                   `json:"original_authority_hash"`
	Windows       uint64                   `json:"certificate_windows"`
	WindowChain   string                   `json:"certificate_chain"`
	Certified     economicEmissionBoundary `json:"certified_tip"`
	VaultFrom     economicEmissionBoundary `json:"vault_from"`
	VaultThrough  economicEmissionBoundary `json:"vault_through"`
	VaultBlocks   uint64                   `json:"vault_blocks"`
	Required      uint64                   `json:"required_evm_boundaries"`
	RequiredChain string                   `json:"required_boundary_chain"`
	Covered       uint64                   `json:"covered_evm_boundaries"`
}

// Only this independently derived result can promote the summary's finality
// predicate. It grants no runtime, provider measurement, balance or fee proof.
type economicConservationFinalitySummary struct {
	Head     *economicConservationFinalityHead `json:"original_consensus_coverage"`
	Missing  uint64                            `json:"missing_evm_boundaries"`
	Complete bool                              `json:"complete_original_interval"`
}

// Cached entries contain a successfully verified immutable window. The cache
// belongs to the held archive owner, is charged once, and is discarded on close.
type economicVerifiedFinalityWindow struct {
	Next      strecovery.NativeFinalityCheckpoint
	Certified economicEmissionBoundary
	Headers   map[string]economicCertifiedHeader
}

// A native header commits a Frontier block hash. The EVM number is established
// by the complete original EVM header/body reader, never by native height.
type economicCertifiedHeader struct {
	Native economicEmissionBoundary
	Evm    string
}

// A candidate contains only its new facts and borrows an immutable admitted
// prefix. Successful archive publication merges that bounded delta once; no
// per-sample clone or scan of the complete historical map is required.
type economicFinalityIndex struct {
	prior      *economicFinalityIndex
	head       economicConservationFinalityHead
	checkpoint strecovery.NativeFinalityCheckpoint
	windows    map[string]bool
	headers    map[string]economicCertifiedHeader
	evm        map[string]economicEmissionBoundary
	required   map[string]economicEmissionBoundary
	covered    map[string]bool
}

// Fresh candidate maps are local until every certificate and original boundary
// has passed. The admitted prefix has at most one level and is never rewritten
// by an observation that later fails or is canceled.
func newEconomicFinalityIndex(prior *economicFinalityIndex) *economicFinalityIndex {
	value := &economicFinalityIndex{prior: prior, windows: map[string]bool{}, headers: map[string]economicCertifiedHeader{}, evm: map[string]economicEmissionBoundary{}, required: map[string]economicEmissionBoundary{}, covered: map[string]bool{}}
	if prior != nil {
		value.head, value.checkpoint = prior.head, prior.checkpoint
	}
	return value
}

// Each lookup visits the active bounded delta and its single admitted prefix.
// No timestamp, equal amount or caller-supplied mapping can create coverage.
func (self *economicFinalityIndex) header(hash string) (economicCertifiedHeader, bool) {
	for value := self; value != nil; value = value.prior {
		if header, ok := value.headers[hash]; ok {
			return header, true
		}
	}
	return economicCertifiedHeader{}, false
}

// A repeated EVM commitment may legitimately occur in more than one native
// header. Either verified ancestor authenticates the same exact EVM hash.
func (self *economicFinalityIndex) hasEvm(hash string) bool {
	for value := self; value != nil; value = value.prior {
		if _, ok := value.evm[hash]; ok {
			return true
		}
	}
	return false
}

// Membership is exact; a repeated proof never increments the window chain.
func (self *economicFinalityIndex) hasWindow(hash string) bool {
	for value := self; value != nil; value = value.prior {
		if value.windows[hash] {
			return true
		}
	}
	return false
}

// The requirement census retains conflicting annotations as contradictions.
// An unavailable mapping leaves a missing requirement, never an inferred zero.
func (self *economicFinalityIndex) require(boundary economicEmissionBoundary) error {
	if !rootCanonicalHash(boundary.Hash) {
		return errors.New("economic finality requirement lacks its original EVM boundary")
	}
	for value := self; value != nil; value = value.prior {
		if prior, ok := value.required[boundary.Hash]; ok {
			if prior != boundary {
				return errors.Join(errRpcIntegrity, errors.New("economic finality requirement changed the original EVM height"))
			}
			return nil
		}
	}
	self.required[boundary.Hash] = boundary
	self.head.Required++
	self.head.RequiredChain = rootObjectHash([]string{self.head.RequiredChain, rootObjectHash(boundary)})
	if self.hasEvm(boundary.Hash) {
		self.covered[boundary.Hash] = true
		self.head.Covered++
	}
	return nil
}

// New certified hashes resolve only their exact original missing requirement.
// Already covered historical boundaries are not rescanned on every sample.
func (self *economicFinalityIndex) addHeader(header economicCertifiedHeader) error {
	if prior, ok := self.header(header.Native.Hash); ok {
		if prior != header {
			return errors.Join(errRpcIntegrity, errors.New("economic certificate changed an original native mapping"))
		}
		return nil
	}
	self.headers[header.Native.Hash] = header
	if header.Evm == "" || self.hasEvm(header.Evm) {
		return nil
	}
	self.evm[header.Evm] = header.Native
	for value := self; value != nil; value = value.prior {
		if _, required := value.required[header.Evm]; required {
			self.covered[header.Evm] = true
			self.head.Covered++
			break
		}
	}
	return nil
}

// Canonical decoding and hashing use the same original SCALE header adapter as
// principal execution. The block number is never inferred from an EVM number.
func decodeEconomicFinalityHeader(encoded string) (rootReceiptHeader, economicEmissionBoundary, error) {
	raw, err := historicalReplayHex(encoded, 64*1024)
	if err != nil {
		return rootReceiptHeader{}, economicEmissionBoundary{}, err
	}
	var header types.Header
	if err := codec.Decode(raw, &header); err != nil {
		return rootReceiptHeader{}, economicEmissionBoundary{}, err
	}
	hash := blake2b.Sum256(raw)
	boundary := economicEmissionBoundary{Number: uint64(header.Number), Hash: fmt.Sprintf("0x%x", hash)}
	projection := nativePrincipalProjection{ParentHeaderHex: encoded, Parent: boundary}
	decoded, err := projection.parentHeader()
	return decoded, boundary, err
}

// The native verifier already authenticates all SCALE headers, certificate
// weights, ancestry and authority handoffs. Decode those same immutable bytes
// to derive the Frontier mapping; unsupported digests stay unavailable.
func economicFinalityHeader(encoded string) (economicCertifiedHeader, error) {
	var result economicCertifiedHeader
	header, boundary, err := decodeEconomicFinalityHeader(encoded)
	if err != nil {
		return result, err
	}
	result.Native = boundary
	mapping, err := finalizedFrontierPostLog(header)
	if err != nil {
		if errors.Is(err, errFinalizedMappingUnavailable) && !errors.Is(err, errRpcIntegrity) {
			return result, nil
		}
		return result, err
	}
	result.Evm = mapping.BlockHash
	return result, nil
}

// A verified cache result is keyed by all original proof and anchor bytes.
// Its use still requires exact agreement with the rolling admitted checkpoint.
func (self *economicConservationArchiveView) verifyFinalityWindow(ctx context.Context, window nativeExecutionFinalityWindow) (*economicVerifiedFinalityWindow, error) {
	if err := self.checkAdmission(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := rootObjectHash(window)
	if value := self.finalityVerified[key]; value != nil {
		return value, errors.Join(ctx.Err(), self.checkAdmission())
	}
	raw, err := json.Marshal(window.Proof)
	if err != nil {
		return nil, err
	}
	if len(raw) > strecovery.MaximumReceiptFinalityBytes {
		return nil, errMonitorEconomicCapacity
	}
	if self.finalityPolicy == nil {
		return nil, errors.New("economic certificates lack original producer authority")
	}
	verified, err := strecovery.VerifyNativeExecutionFinality(ctx, self.finalityPolicy.Network.GenesisHash, &window.Anchor, &window.Proof)
	if err != nil {
		return nil, err
	}
	result := &economicVerifiedFinalityWindow{Next: verified.NextCheckpoint, Certified: economicEmissionBoundary{Number: verified.Certified.Number, Hash: verified.Certified.Hash}, Headers: map[string]economicCertifiedHeader{}}
	appendHeader := func(encoded string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := economicFinalityHeader(encoded)
		if err != nil {
			return err
		}
		if prior, ok := result.Headers[header.Native.Hash]; ok && prior != header {
			return errors.Join(errRpcIntegrity, errors.New("economic certified header repeated with different identity"))
		}
		result.Headers[header.Native.Hash] = header
		return nil
	}
	if err := appendHeader(window.Anchor.HeaderScale); err != nil {
		return nil, err
	}
	for _, segment := range window.Proof.Segments {
		for _, encoded := range segment.Headers {
			if err := appendHeader(encoded); err != nil {
				return nil, err
			}
		}
	}
	if self.finalityWork != nil {
		self.finalityWork(ctx)
	}
	if err := errors.Join(ctx.Err(), self.checkAdmission()); err != nil {
		return nil, err
	}
	charged := *self
	if err := charged.charge(result); err != nil {
		return nil, err
	}
	if err := errors.Join(ctx.Err(), self.checkAdmission()); err != nil {
		return nil, err
	}
	self.entries, self.bytes = charged.entries, charged.bytes
	self.finalityVerified[key] = result
	return result, nil
}

// The first checkpoint is read from the already independently signed original
// native approval. A later caller cannot substitute a convenient authority set.
func (self *economicConservationArchiveView) initialFinality(ctx context.Context, raw []byte) (*strecovery.NativeFinalityCheckpoint, error) {
	if err := errors.Join(ctx.Err(), self.checkAdmission()); err != nil {
		return nil, err
	}
	if self.finalityPolicy == nil || self.finalityPolicy.Execution == nil || self.finalityPolicy.Execution.Producer == nil {
		return nil, errors.New("economic finality requires the original signed native producer checkpoint")
	}
	reference := self.finalityPolicy.Execution.Producer.Authority
	if len(raw) == 0 || len(raw) > nativeProducerAuthorityMaximum(self.finalityPolicy.Execution.FeeCensus) || monitorReadDigest(raw) != reference.Sha256 {
		return nil, errors.Join(errRpcIntegrity, errors.New("economic consensus changed its original signed authority bytes"))
	}
	if self.finalityAnchor != nil {
		if self.finalityApprovalHash != reference.Sha256 {
			return nil, errors.New("economic consensus authority cache differs")
		}
		return self.finalityAnchor, errors.Join(ctx.Err(), self.checkAdmission())
	}
	authority, err := readNativeProducerAuthority(ctx, *self.finalityPolicy, func(ctx context.Context, selected planFileReference) ([]byte, error) {
		if selected != reference {
			return nil, errors.New("economic consensus selected another original approval")
		}
		return raw, ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	if err := errors.Join(ctx.Err(), self.checkAdmission()); err != nil {
		return nil, err
	}
	charged := *self
	if err := charged.charge(struct {
		Authority  string
		Checkpoint strecovery.NativeFinalityCheckpoint
	}{Authority: reference.Sha256, Checkpoint: authority.Checkpoint}); err != nil {
		return nil, err
	}
	if err := errors.Join(ctx.Err(), self.checkAdmission()); err != nil {
		return nil, err
	}
	self.entries, self.bytes = charged.entries, charged.bytes
	self.finalityAnchor = &authority.Checkpoint
	self.finalityApprovalHash = reference.Sha256
	return self.finalityAnchor, nil
}

// The temporary result borrows only the held immutable prefix. Admission and
// final publication fences surround its use; failed candidates expose nothing.
func (self *economicConservationState) prepareFinality(ctx context.Context) (*economicFinalityIndex, error) {
	view := self.archiveView
	if view == nil {
		return nil, errors.New("economic finality requires original archive admission")
	}
	if err := errors.Join(ctx.Err(), view.checkAdmission()); err != nil {
		return nil, err
	}
	result := newEconomicFinalityIndex(view.finality)
	if self.Archive != nil && !reflect.DeepEqual(self.Archive.Finality, economicFinalityHead(view.finality)) {
		return nil, errors.New("economic checkpoint changed original archived certificate lineage")
	}
	if len(self.FinalityApproval) != 0 {
		if _, err := view.initialFinality(ctx, self.FinalityApproval); err != nil {
			return nil, err
		}
	}
	for _, window := range self.FinalityWindows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := rootObjectHash(window)
		if result.hasWindow(key) {
			continue
		}
		if result.head.Windows == 0 {
			anchor := view.finalityAnchor
			if anchor == nil {
				return nil, errors.New("economic consensus lost original anchor admission")
			}
			result.checkpoint = *anchor
			result.head.AuthorityHash = view.finalityPolicy.Execution.Producer.Authority.Sha256
			result.head.WindowChain = result.head.AuthorityHash
		}
		if window.Anchor.Hash() != result.checkpoint.Hash() || window.Proof.CheckpointHash != result.checkpoint.Hash() {
			return nil, errors.Join(errRpcIntegrity, errors.New("economic finality window skipped its original certified authority checkpoint"))
		}
		verified := view.finalityVerified[key]
		if verified == nil {
			var err error
			verified, err = view.verifyFinalityWindow(ctx, window)
			if err != nil {
				return nil, err
			}
		}
		for _, header := range verified.Headers {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := result.addHeader(header); err != nil {
				return nil, err
			}
		}
		result.windows[key] = true
		result.checkpoint = verified.Next
		result.head.Windows++
		result.head.WindowChain = rootObjectHash([]string{result.head.WindowChain, key})
		result.head.Certified = verified.Certified
	}
	if err := result.requireOriginalBoundaries(ctx, self); err != nil {
		return nil, err
	}
	return result, errors.Join(ctx.Err(), view.checkAdmission())
}

// Nil is the exact legacy wire representation. A nonempty derived head cannot
// be constructed merely from a caller-supplied certificate count.
func economicFinalityHead(index *economicFinalityIndex) *economicConservationFinalityHead {
	if index == nil || index.head == (economicConservationFinalityHead{}) {
		return nil
	}
	value := index.head
	return &value
}
