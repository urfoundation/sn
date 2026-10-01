// Discovery consumes unapproved observation pins, never an owner or removal
// policy. Imported runtime bytes and envelope seals are checked before any RPC.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/urfoundation/sn/v2026/crv4"
	"golang.org/x/crypto/blake2b"
)

const maximumSubnetDiscoveryInputBytes = 2*(maximumRuntimeSnapshotCodeBytes+maximumRuntimeSnapshotMetadataBytes) + 2*maxRpcReplyBytes

// Both public snapshot formats carry the same runtime observation. The parent
// seal remains evidence provenance, not a signature or source-build approval.
func readSubnetDiscoveryInput(path string) (runtimeSnapshot, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return runtimeSnapshot{}, "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maximumSubnetDiscoveryInputBytes+1))
	if err != nil || len(raw) > maximumSubnetDiscoveryInputBytes {
		return runtimeSnapshot{}, "", errors.Join(errors.New("discovery snapshot exceeds bounded input or cannot be read"), err)
	}
	var probe struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return runtimeSnapshot{}, "", err
	}
	var runtime runtimeSnapshot
	if probe.Schema == finalizedSnapshotSchema {
		var envelope finalizedSnapshotEnvelope
		if err := decodePlanJson(raw, &envelope); err != nil {
			return runtimeSnapshot{}, "", err
		}
		sealed, err := sealFinalizedSnapshot(envelope.finalizedSnapshot)
		if err != nil || sealed.ContentHash != envelope.ContentHash {
			return runtimeSnapshot{}, "", errors.Join(errors.New("discovery finalized snapshot seal or scope differs"), err)
		}
		runtime = envelope.Runtime
	} else {
		var envelope runtimeSnapshotEnvelope
		if err := decodePlanJson(raw, &envelope); err != nil {
			return runtimeSnapshot{}, "", err
		}
		sealed, err := sealRuntimeSnapshot(envelope.Snapshot)
		if err != nil || sealed.ContentHash != envelope.ContentHash {
			return runtimeSnapshot{}, "", errors.Join(errors.New("discovery runtime snapshot seal differs"), err)
		}
		runtime = envelope.Snapshot
	}
	if err := validateSubnetDiscoveryRuntime(runtime); err != nil {
		return runtimeSnapshot{}, "", err
	}
	digest := sha256.Sum256(raw)
	return runtime, "sha256:" + hex.EncodeToString(digest[:]), nil
}

// The v470 observation codec has a finite scope. Matching these self-contained
// pins permits only reads; even correct official bytes do not approve mainnet.
func validateSubnetDiscoveryRuntime(snapshot runtimeSnapshot) error {
	version := crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 470, TransactionVersion: 1, StateVersion: 1}
	identity := snapshot.Identity
	if snapshot.Schema != runtimeSnapshotSchema || snapshot.Admission != "unapproved_observation" ||
		identity.Schema != identitySchema || identity.NativeChain == "" || identity.EvmChainId != 964 ||
		!rootCanonicalHash(identity.GenesisHash) || !rootCanonicalHash(identity.FinalizedHash) ||
		snapshot.Version != version || identity.RuntimeSpec != uint64(version.SpecVersion) || identity.RuntimeTx != uint64(version.TransactionVersion) {
		return errors.New("discovery requires an unapproved EVM-964 runtime470 snapshot with one complete identity")
	}
	for _, artifact := range []struct {
		name    string
		encoded string
		hash    string
		limit   int
	}{
		{name: "code", encoded: snapshot.CodeHex, hash: snapshot.CodeHash, limit: maximumRuntimeSnapshotCodeBytes},
		{name: "metadata", encoded: snapshot.MetadataHex, hash: snapshot.MetadataHash, limit: maximumRuntimeSnapshotMetadataBytes},
	} {
		raw, _, err := decodeRuntimeSnapshotHex(artifact.name, artifact.encoded, artifact.limit)
		if err != nil {
			return err
		}
		digest := blake2b.Sum256(raw)
		if !rootCanonicalHash(artifact.hash) || artifact.hash != "0x"+hex.EncodeToString(digest[:]) {
			return fmt.Errorf("discovery %s bytes differ from retained hash", artifact.name)
		}
	}
	if _, _, err := crv4.DecodeRuntimeMetadata(strings.ToLower(snapshot.MetadataHex)); err != nil {
		return fmt.Errorf("discovery snapshot metadata: %w", err)
	}
	return nil
}
