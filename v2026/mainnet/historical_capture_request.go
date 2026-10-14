// The request collector binds real RPC observations to a selected historical
// child and its parent execution runtime. It collects no signing authority and
// never mistakes a read-proof key list for a complete execution witness.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

const historicalCaptureObservationSchema = "urnetwork-historical-capture-observation-v1"

type historicalCaptureObservation struct {
	Schema          string          `json:"schema"`
	Authority       string          `json:"authority"`
	Identity        chainIdentity   `json:"child_identity"`
	ParentRuntime   runtimeSnapshot `json:"parent_runtime"`
	RequestJSON     string          `json:"request_json"`
	RequestSha256   string          `json:"request_sha256"`
	RuntimeAdmitted bool            `json:"runtime_admitted"`
	FeeAdmitted     bool            `json:"native_fee_admitted"`
}

type historicalCaptureObservationEnvelope struct {
	Observation historicalCaptureObservation `json:"observation"`
	ContentHash string                       `json:"content_hash"`
}

// The existing header authenticator owns digest semantics and exact SCALE
// hashing. This bounded re-encoding retains those same bytes for the Rust job.
func historicalCaptureHeaderHex(header rootReceiptHeader, hash string) (string, error) {
	number, err := header.authenticate(hash)
	if err != nil || len(header.Digest.Logs) > 64 {
		return "", errors.Join(errors.New("historical capture header is not admitted by the bounded replay grammar"), err)
	}
	parent, err := rootReceiptHex(header.ParentHash, 32)
	if err != nil {
		return "", err
	}
	state, err := rootReceiptHex(header.StateRoot, 32)
	if err != nil {
		return "", err
	}
	body, err := rootReceiptHex(header.ExtrinsicsRoot, 32)
	if err != nil {
		return "", err
	}
	raw := append(parent, rootCompact(number)...)
	raw = append(raw, state...)
	raw = append(raw, body...)
	raw = append(raw, rootCompact(uint64(len(header.Digest.Logs)))...)
	for _, value := range header.Digest.Logs {
		log, err := rootReceiptHex(value, 64*1024-len(raw))
		if err != nil {
			return "", err
		}
		raw = append(raw, log...)
	}
	if len(raw) > 64*1024 || rootExtrinsicHash(raw) != hash {
		return "", errors.New("historical capture encoded header differs")
	}
	return "0x" + hex.EncodeToString(raw), nil
}

func historicalCaptureHash(raw string) (historicalReplayDigest, error) {
	var result historicalReplayDigest
	value, err := rootReceiptHex(raw, 32)
	if err != nil || len(value) != len(result) {
		return result, errors.Join(errors.New("historical capture hash width differs"), err)
	}
	copy(result[:], value)
	return result, nil
}

// One inherited deadline spans every observation. The finality statements are
// owned-RPC assertions; imported observations cannot reconstruct live custody.
func (self *rpcClient) readHistoricalCaptureRequest(ctx context.Context, expected identityExpectation, childHash string, profile *historicalReplayObservationProfile) (result historicalCaptureObservation, resultErr error) {
	if !rootCanonicalHash(childHash) {
		return result, errors.New("historical capture requires an exact child block hash")
	}
	owner, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, owner.Err())
		if resultErr != nil {
			result = historicalCaptureObservation{}
		}
	}()
	identity, err := self.readIdentityAt(owner, childHash)
	if err != nil {
		return result, err
	}
	if err := expected.match(identity); err != nil {
		return result, err
	}
	chain := &rootCanonicalChain{client: self}
	child, number, err := chain.header(owner, childHash)
	if err != nil {
		return result, err
	}
	if number == 0 || number != identity.FinalizedNumber {
		return result, fmt.Errorf("%w: capture child has no exact parent", errRpcIntegrity)
	}
	parentIdentity, err := self.readIdentityAt(owner, child.ParentHash)
	if err != nil {
		return result, err
	}
	if err := expected.match(parentIdentity); err != nil {
		return result, err
	}
	parent, parentNumber, err := chain.header(owner, child.ParentHash)
	if err != nil {
		return result, err
	}
	if parentNumber+1 != number || parentIdentity.FinalizedNumber != parentNumber {
		return result, fmt.Errorf("%w: capture parent and child are not adjacent", errRpcIntegrity)
	}
	// An upgrade child may publish a new runtime. Its execution still uses the
	// parent's original code, version, metadata and optional callsite profile.
	runtime, err := self.readRuntimeSnapshotAtIdentity(owner, parentIdentity)
	if err != nil {
		return result, err
	}
	code, _, err := decodeRuntimeSnapshotHex("capture parent code", runtime.CodeHex, maximumRuntimeSnapshotCodeBytes)
	if err != nil {
		return result, err
	}
	parentRaw, err := historicalCaptureHeaderHex(parent, child.ParentHash)
	if err != nil {
		return result, err
	}
	childRaw, err := historicalCaptureHeaderHex(child, childHash)
	if err != nil {
		return result, err
	}
	parentDigest, err := historicalCaptureHash(child.ParentHash)
	if err != nil {
		return result, err
	}
	childDigest, err := historicalCaptureHash(childHash)
	if err != nil {
		return result, err
	}
	codeDigest, err := historicalCaptureHash(runtime.CodeHash)
	if err != nil {
		return result, err
	}
	var block struct {
		Block *struct {
			Header     rootReceiptHeader `json:"header"`
			Extrinsics *[]string         `json:"extrinsics"`
		} `json:"block"`
	}
	if err := self.callBoundedRead(owner, "chain_getBlock", []any{childHash}, &block, false, 2*8*1024*1024+maxRpcReplyBytes); err != nil {
		return result, err
	}
	if block.Block == nil || block.Block.Extrinsics == nil || len(*block.Block.Extrinsics) > 16384 {
		return result, fmt.Errorf("%w: capture complete block body absent or exceeds bound", errRpcIntegrity)
	}
	block.Block.Header.normalizeHashes()
	if _, err := block.Block.Header.authenticate(childHash); err != nil {
		return result, fmt.Errorf("%w: capture body header: %v", errRpcIntegrity, err)
	}
	if _, err := authenticateRootReceiptBody(child, *block.Block.Extrinsics); err != nil {
		return result, fmt.Errorf("%w: capture complete body: %v", errRpcIntegrity, err)
	}
	input := historicalCaptureInput{Schema: historicalCaptureSchema, ParentHeaderHex: parentRaw, ParentHash: parentDigest, ChildHeaderHex: childRaw, ChildHash: childDigest, ExtrinsicsHex: *block.Block.Extrinsics, RuntimeCodeSha256: historicalReplayDigest(sha256.Sum256(code)), RuntimeCodeBlake2b256: codeDigest, ExecutionStateVersion: runtime.Version.StateVersion, ObservationProfile: profile}
	if err := input.validate(); err != nil {
		return result, err
	}
	if profile != nil && profile.MetadataSha256 != nil {
		metadata, _, err := decodeRuntimeSnapshotHex("capture parent metadata", runtime.MetadataHex, maximumRuntimeSnapshotMetadataBytes)
		if err != nil || historicalReplayDigest(sha256.Sum256(metadata)) != *profile.MetadataSha256 {
			return result, errors.Join(errors.New("historical capture callsite profile metadata differs from parent"), err)
		}
	}
	if err := self.closeSnapshotFinality(owner, parentIdentity); err != nil {
		return result, err
	}
	if err := self.closeSnapshotFinality(owner, identity); err != nil {
		return result, err
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > historicalCaptureRequestLimit {
		return result, errors.Join(errors.New("historical capture serialized request exceeds bound"), err)
	}
	return historicalCaptureObservation{Schema: historicalCaptureObservationSchema, Authority: "owned-rpc-assertion", Identity: identity, ParentRuntime: runtime, RequestJSON: string(raw), RequestSha256: monitorReadDigest(raw)}, nil
}
