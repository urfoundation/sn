// Exact synthetic approvals exercise real metadata construction and sr25519
// validation. No chain write, signing verdict or capability result is mocked.
package crv4

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"sync"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// Each fixture owns its connection, decoded metadata and exact approval. Rpc
// transcripts are read-only; concurrent controls never mutate these fields.
type sourceRuntimeCapabilityTestFixture struct {
	chain       *Chain
	block       types.Hash
	identity    RuntimeArtifactIdentity
	metadata    *types.Metadata
	metadataHex string
	calls       int
}

// The successor is outside the compiled catalog and has no provisional flag.
func newSourceRuntimeCapabilityTestFixture(t *testing.T) *sourceRuntimeCapabilityTestFixture {
	t.Helper()
	metadata, hash, encoded := provisionalRuntimeMetadataTest(t)
	self := &sourceRuntimeCapabilityTestFixture{
		block: types.Hash{5}, metadata: metadata, metadataHex: encoded,
		identity: RuntimeArtifactIdentity{
			Version:  RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: ReviewedRuntimeSpecVersion + 21, TransactionVersion: 1, StateVersion: 1},
			CodeHash: types.Hash{0x61}.Hex(), MetadataHash: hash,
		},
	}
	self.chain = &Chain{GenesisHash: types.Hash{8}}
	self.chain.API = &gsrpc.SubstrateAPI{Client: &runtimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		self.calls++
		expected := []any{self.block.Hex()}
		if method == "state_getStorageHash" {
			expected = []any{"0x3a636f6465", self.block.Hex()}
		}
		if !reflect.DeepEqual(args, expected) {
			return fmt.Errorf("source runtime Rpc %s is not exact-block: %v", method, args)
		}
		switch method {
		case "state_getRuntimeVersion":
			return setRuntimeIdentityTestResult(result, self.identity.Version)
		case "state_getStorageHash":
			return setRuntimeIdentityTestResult(result, self.identity.CodeHash)
		case "state_getMetadata":
			return setRuntimeIdentityTestResult(result, self.metadataHex)
		default:
			return fmt.Errorf("unexpected or mutating source runtime Rpc %s", method)
		}
	}}}
	return self
}

// Re-encodes the fixture's explicit approval after an intentional interface
// mutation. Exact artifact approval and capability approval remain separate.
func (self *sourceRuntimeCapabilityTestFixture) bind(t *testing.T) AuthenticatedRuntimeArtifact {
	t.Helper()
	for index := range self.metadata.AsMetadataV14.Lookup.Types {
		entry := &self.metadata.AsMetadataV14.Lookup.Types[index]
		if value := self.metadata.AsMetadataV14.EfficientLookup[entry.ID.Int64()]; value != nil {
			entry.Type = *value
		}
	}
	encoded, err := codec.EncodeToHex(self.metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, hash, err := DecodeRuntimeMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	self.metadataHex, self.identity.MetadataHash = encoded, hash
	artifact, err := AuthenticateRuntimeArtifactAtContext(t.Context(), self.chain, self.block, self.identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := self.chain.BindRuntimeArtifact(artifact); err != nil {
		t.Fatal(err)
	}
	return artifact
}

// Real signatures retain the fixture's exact spec and genesis. These are new
// synthetic attempts; production recovery never re-signs a retained record.
func (self *sourceRuntimeCapabilityTestFixture) prepared(t *testing.T, mecid *uint8) *PreparedSubmission {
	t.Helper()
	prepared, key := sourcePreparedTest(t)
	prepared.SourceCommitment.RuntimeSpec = self.identity.Version.SpecVersion
	prepared.SourceCommitment.GenesisHash = self.chain.GenesisHash.Hex()
	prepared.Mecid = mecid
	signSourcePreparedTest(t, prepared, key, nil)
	return prepared
}

// A live call uses the selected exact artifact even when its spec is unknown
// to the binary. The independent fixed bytes match the source schema.
func TestSourceRuntimeCapabilityBuildsApprovedFutureCall(t *testing.T) {
	t.Parallel()
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	fixture.bind(t)
	for _, mecid := range []*uint8{nil, new(uint8)} {
		call, err := fixture.chain.newSourceCommitmentBatchCall(521, mecid, [32]byte{9}, []byte{1, 2}, 22, 4)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := codec.Encode(call)
		if err != nil {
			t.Fatal(err)
		}
		expected := append([]byte{11, 2, 8, 18, 0, 9, 2, 4, 131}, bytes.Repeat([]byte{0}, 32)...)
		expected[9] = 9
		if mecid == nil {
			expected = append(expected, 7, 113, 9, 2)
		} else {
			expected = append(expected, 7, 118, 9, 2, *mecid)
		}
		expected = append(expected, 8, 1, 2)
		expected = binary.LittleEndian.AppendUint64(expected, 22)
		expected = binary.LittleEndian.AppendUint16(expected, 4)
		if !bytes.Equal(actual, expected) {
			t.Fatalf("successor source call differs: %x, want %x", actual, expected)
		}
	}
}

// Exact future signed bytes survive serialization and cold reauthentication;
// later blocks with the same artifact do not require re-signing those bytes.
func TestSourceRuntimeCapabilityRetainsFutureSignedBytes(t *testing.T) {
	t.Parallel()
	for _, mecid := range []*uint8{nil, new(uint8)} {
		fixture := newSourceRuntimeCapabilityTestFixture(t)
		fixture.bind(t)
		prepared := fixture.prepared(t, mecid)
		original := prepared.ExtrinsicHex
		if _, err := prepared.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := fixture.chain.ValidatePreparedSource(prepared); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(prepared)
		if err != nil {
			t.Fatal(err)
		}
		var reloaded PreparedSubmission
		if err := json.Unmarshal(encoded, &reloaded); err != nil {
			t.Fatal(err)
		}
		cold := newSourceRuntimeCapabilityTestFixture(t)
		cold.block = types.Hash{6}
		cold.bind(t)
		if err := cold.chain.ValidatePreparedSource(&reloaded); err != nil || reloaded.ExtrinsicHex != original {
			t.Fatalf("cold same-artifact view lost signed history: %v", err)
		}
		if cold.chain.ProvisionalRuntimeCompatibilityEnabled() || reloaded.SourceCommitment.CompatibilityProfile != "" {
			t.Fatal("strict source validation acquired testnet authority")
		}
	}
}

// A correctly encoded/signed candidate does not authorize its own runtime.
// Unproved metadata and a fabricated exported artifact both fail live use.
func TestSourceRuntimeCapabilityOfflineSchemaDoesNotGrantAuthority(t *testing.T) {
	t.Parallel()
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	prepared := fixture.prepared(t, nil)
	if _, err := prepared.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bind := range []bool{false, true} {
		view := *fixture.chain
		view.Meta = fixture.metadata
		view.Runtime = &types.RuntimeVersion{SpecName: fixture.identity.Version.SpecName, SpecVersion: types.U32(fixture.identity.Version.SpecVersion), TransactionVersion: 1}
		if bind {
			artifact := AuthenticatedRuntimeArtifact{BlockHash: fixture.block, Version: fixture.identity.Version, CodeHash: fixture.identity.CodeHash, MetadataHash: fixture.identity.MetadataHash, Metadata: fixture.metadata}
			if err := view.BindRuntimeArtifact(artifact); err != nil {
				t.Fatal(err)
			}
		}
		if err := view.ValidatePreparedSource(prepared); err == nil {
			t.Fatalf("unproved bound=%t view accepted the candidate", bind)
		}
		if _, err := SubmitPrepared(t.Context(), &view, prepared); err == nil || fixture.calls != 0 {
			t.Fatalf("candidate reached submission through bound=%t view: %v calls=%d", bind, err, fixture.calls)
		}
	}
}

// The opaque proof binds block, owner and every exact artifact coordinate.
// Failure preserves the old usable view; proof fields are never serialized.
func TestSourceRuntimeCapabilityRejectsWitnessRelabeling(t *testing.T) {
	t.Parallel()
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	artifact := fixture.bind(t)
	prepared := fixture.prepared(t, nil)
	for _, fault := range []string{"block", "version", "code", "metadata", "pointer", "genesis"} {
		changed := artifact
		switch fault {
		case "block":
			changed.BlockHash = types.Hash{0x51}
		case "version":
			changed.Version.SpecVersion++
		case "code":
			changed.CodeHash = types.Hash{0x52}.Hex()
		case "metadata":
			changed.MetadataHash = types.Hash{0x53}.Hex()
		case "pointer":
			changed.Metadata = fixture.metadata
		case "genesis":
			changed.GenesisHash = types.Hash{0x54}
		}
		if err := fixture.chain.BindRuntimeArtifact(changed); err == nil {
			t.Errorf("changed %s inherited the original proof", fault)
		}
		if err := fixture.chain.ValidatePreparedSource(prepared); err != nil {
			t.Fatalf("failed %s binding changed the prior view: %v", fault, err)
		}
	}
	foreign := newSourceRuntimeCapabilityTestFixture(t)
	if err := foreign.chain.BindRuntimeArtifact(artifact); err == nil {
		t.Fatal("another connection inherited strict artifact authority")
	}
	copyView := *fixture.chain
	copyView.API = foreign.chain.API
	if err := copyView.ValidatePreparedSource(prepared); err == nil {
		t.Fatal("replacing the connection retained the old source capability")
	}
}

// Exact approval cannot make changed call indices, argument types or complete
// ordered signing extensions compatible with this source wire schema.
func TestSourceRuntimeCapabilityRejectsConsumedChanges(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"call-index", "call-argument", "pallet-index", "extension-order", "extension-name", "extra-extension", "extrinsic-version"} {
		fixture := newSourceRuntimeCapabilityTestFixture(t)
		metadata := fixture.metadata
		switch fault {
		case "extension-order":
			extensions := metadata.AsMetadataV14.Extrinsic.SignedExtensions
			extensions[0], extensions[1] = extensions[1], extensions[0]
		case "extension-name":
			metadata.AsMetadataV14.Extrinsic.SignedExtensions[0].Identifier = "SyntheticUnsupportedExtension"
		case "extra-extension":
			extra := metadata.AsMetadataV14.Extrinsic.SignedExtensions[0]
			extra.Identifier = "SyntheticZeroSizeExtension"
			metadata.AsMetadataV14.Extrinsic.SignedExtensions = append(metadata.AsMetadataV14.Extrinsic.SignedExtensions, extra)
		case "extrinsic-version":
			metadata.AsMetadataV14.Extrinsic.Version = 5
		default:
			for index := range metadata.AsMetadataV14.Pallets {
				pallet := &metadata.AsMetadataV14.Pallets[index]
				if pallet.Name != PalletName {
					continue
				}
				if fault == "pallet-index" {
					pallet.Index++
				}
				lookup := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
				for variantIndex := range lookup.Def.Variant.Variants {
					variant := &lookup.Def.Variant.Variants[variantIndex]
					if variant.Name == CallCommitTimelocked {
						if fault == "call-index" {
							variant.Index++
						} else if fault == "call-argument" {
							variant.Fields[0].Type = variant.Fields[1].Type
						}
					}
				}
			}
		}
		fixture.bind(t)
		before := fixture.calls
		if _, err := fixture.chain.newSourceCommitmentBatchCall(521, nil, [32]byte{9}, []byte{1, 2}, 22, 4); err == nil || fixture.calls != before {
			t.Errorf("changed %s escaped source capability: %v", fault, err)
		}
	}
}

// Only the selected commit variant participates. Read-only storage and events
// are not source-call/signing capabilities; they need their own read profiles.
func TestSourceRuntimeCapabilityScopesSelectedCall(t *testing.T) {
	t.Parallel()
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	var pallets []types.PalletMetadataV14
	for _, pallet := range fixture.metadata.AsMetadataV14.Pallets {
		if pallet.Name != "Utility" && pallet.Name != "Commitments" && pallet.Name != PalletName {
			continue
		}
		pallet.HasStorage, pallet.HasEvents = false, false
		if pallet.Name == PalletName {
			lookup := fixture.metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
			for index := range lookup.Def.Variant.Variants {
				variant := &lookup.Def.Variant.Variants[index]
				if variant.Name == CallCommitTimelockedMech {
					variant.Index++
				}
			}
		}
		pallets = append(pallets, pallet)
	}
	fixture.metadata.AsMetadataV14.Pallets = pallets
	fixture.bind(t)
	if _, err := fixture.chain.newSourceCommitmentBatchCall(521, nil, [32]byte{9}, []byte{1, 2}, 22, 4); err != nil {
		t.Fatalf("unconsumed metadata change rejected normal source call: %v", err)
	}
	if _, err := fixture.chain.newSourceCommitmentBatchCall(521, new(uint8), [32]byte{9}, []byte{1, 2}, 22, 4); err == nil {
		t.Fatal("changed selected mechanism call escaped its own capability")
	}
}

// The actual public preparation boundary refuses a stale view before storage,
// encryption or nonce allocation. No scheduler race or timing wait is needed.
func TestSourceRuntimeCapabilityRejectsStalePreparationBeforeIo(t *testing.T) {
	t.Parallel()
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	fixture.bind(t)
	_, key := sourcePreparedTest(t)
	before := fixture.calls
	prepared, err := PrepareWeightsCRv4ExactAtContext(t.Context(), fixture.chain, key, 521, []uint16{1}, []*big.Rat{big.NewRat(1, 1)}, SubmitOptions{SourceHash: [32]byte{9}}, types.Hash{0x62})
	if err == nil || !strings.Contains(err.Error(), "exact authenticated block view") || prepared != nil || fixture.calls != before {
		t.Fatalf("stale preparation escaped: prepared=%v error=%v calls=%d", prepared != nil, err, fixture.calls-before)
	}
}

// A valid compatible successor does not validate old signatures in its new
// signing domain, and mutating retained header fields still fails sr25519.
func TestSourceRuntimeCapabilityPreservesOriginalSigningDomain(t *testing.T) {
	t.Parallel()
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	fixture.bind(t)
	prepared := fixture.prepared(t, nil)
	original := prepared.ExtrinsicHex
	next := newSourceRuntimeCapabilityTestFixture(t)
	next.identity.Version.SpecVersion++
	next.bind(t)
	if err := next.chain.ValidatePreparedSource(prepared); err == nil {
		t.Fatal("a new authenticated domain admitted old signed bytes")
	}
	prepared.SourceCommitment.RuntimeSpec++
	if _, err := prepared.Validate(); err == nil || prepared.ExtrinsicHex != original {
		t.Fatal("relabeling altered or authorized the original signed bytes")
	}
}

// A retained strict witness outlives bounded metadata eviction while another
// reader validates the same immutable signed bytes. Both workers are joined.
func TestSourceRuntimeCapabilitySurvivesConcurrentMetadataEviction(t *testing.T) {
	t.Parallel()
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	artifact := fixture.bind(t)
	prepared := fixture.prepared(t, nil)
	cache := fixture.chain.runtimeMetadataArtifactCache()
	start := make(chan struct{})
	var workers sync.WaitGroup
	failures := make(chan error, 2)
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		for index := range maximumRuntimeMetadataCacheEntries + 1 {
			identity := fixture.identity
			identity.Version.SpecVersion += uint32(index + 1)
			if _, _, err := cache.load(t.Context(), identity, func(context.Context) (*types.Metadata, string, error) {
				return artifact.Metadata, identity.MetadataHash, nil
			}); err != nil {
				failures <- err
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for range 16 {
			if err := fixture.chain.ValidatePreparedSource(prepared); err != nil {
				failures <- err
				return
			}
		}
	}()
	close(start)
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if _, exists := cache.identityEntries[fixture.identity]; exists || len(cache.identityEntries) != maximumRuntimeMetadataCacheEntries {
		t.Fatal("fixture did not force bounded eviction of the held artifact")
	}
	if err := fixture.chain.BindRuntimeArtifact(artifact); err != nil {
		t.Fatal(err)
	}
	if err := fixture.chain.ValidatePreparedSource(prepared); err != nil {
		t.Fatalf("eviction revoked held source authority: %v", err)
	}
}

// Even canceled successful-looking Rpc responses cannot publish a new strict
// witness. The caller receives the original cancellation cause.
func TestSourceRuntimeCapabilityCanceledAuthenticationHasNoProof(t *testing.T) {
	t.Parallel()
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		err := original(ctx, result, method, args...)
		if method == "state_getMetadata" {
			cancel()
		}
		return err
	}
	artifact, err := AuthenticateRuntimeArtifactAtContext(ctx, fixture.chain, fixture.block, fixture.identity)
	if !errors.Is(err, context.Canceled) || artifact.authenticationProof != nil {
		t.Fatalf("canceled authentication published authority: %v", err)
	}
}
