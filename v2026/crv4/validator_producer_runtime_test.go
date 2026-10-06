// Production capability tests use exact synthetic artifacts and synchronous
// read-only transcripts. No production approval, seed or network is involved.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Adds the consumed API declaration to the existing full-metadata fixture.
func validatorProducerRuntimeFixture(t *testing.T) *sourceRuntimeCapabilityTestFixture {
	t.Helper()
	fixture := newSourceRuntimeCapabilityTestFixture(t)
	client := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if err := original(ctx, result, method, args...); err != nil {
			return err
		}
		if method == "state_getRuntimeVersion" {
			version := fixture.identity.Version
			return setRuntimeIdentityTestResult(result, map[string]any{"specName": version.SpecName, "specVersion": version.SpecVersion,
				"transactionVersion": version.TransactionVersion, "stateVersion": version.StateVersion, "apis": []any{[]any{"0x8375104b299b74c5", 2}}})
		}
		return nil
	}
	return fixture
}

// Ambiguous wire cannot hide an incompatible API behind an accepted duplicate.
func TestValidatorProducerRuntimeRejectsAmbiguousApiDeclaration(t *testing.T) {
	t.Parallel()
	fixture := validatorProducerRuntimeFixture(t)
	artifact := fixture.bind(t)
	fixture.chain.API.Client.(*runtimeIdentityTestClient).callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if method != "state_getRuntimeVersion" {
			return fmt.Errorf("unexpected API proof read %s", method)
		}
		version := artifact.Version
		wire := fmt.Sprintf(`{"specName":%q,"specVersion":%d,"transactionVersion":1,"stateVersion":1,"apis":[["0x8375104b299b74c5",3]],"apis":[["0x8375104b299b74c5",2]]}`, version.SpecName, version.SpecVersion)
		*result.(*json.RawMessage) = json.RawMessage(wire)
		return ctx.Err()
	}
	if err := fixture.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err == nil {
		t.Fatal("duplicate API declaration acquired production signing capability")
	}
}

// Exact read authority alone cannot sign. Only the dedicated purpose binding
// survives the final signing boundary, and a later generic bind revokes it.
func TestValidatorProducerRuntimeRequiresPurposeBinding(t *testing.T) {
	t.Parallel()
	fixture := validatorProducerRuntimeFixture(t)
	artifact := fixture.bind(t)
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err == nil {
		t.Fatal("exact read-only binding acquired production signing capability")
	}
	if err := fixture.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
		t.Fatal(err)
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err != nil {
		t.Fatal(err)
	}
	changed := fixture.identity
	changed.CodeHash = types.Hash{0x83}.Hex()
	if err := fixture.chain.ValidateValidatorProducerRuntime(changed); err == nil {
		t.Fatal("producer purpose proof authorized a different configured artifact")
	}
	if err := fixture.chain.BindRuntimeArtifact(artifact); err != nil {
		t.Fatal(err)
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err == nil {
		t.Fatal("generic binding retained production signing authority")
	}
}

// Storage, source calls, receipt events and extensions are checked even when
// the operator supplied the exact new metadata hash. This is operation scope.
func TestValidatorProducerRuntimeRejectsConsumedChanges(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"storage-type", "storage-default", "call-index", "event-index", "extension-order"} {
		fixture := validatorProducerRuntimeFixture(t)
		metadata := fixture.metadata
		if fault == "extension-order" {
			extensions := metadata.AsMetadataV14.Extrinsic.SignedExtensions
			extensions[0], extensions[1] = extensions[1], extensions[0]
		} else {
			for index := range metadata.AsMetadataV14.Pallets {
				pallet := &metadata.AsMetadataV14.Pallets[index]
				if pallet.Name != PalletName {
					continue
				}
				switch fault {
				case "storage-type", "storage-default":
					for itemIndex := range pallet.Storage.Items {
						item := &pallet.Storage.Items[itemIndex]
						if item.Name == "LastUpdate" {
							if fault == "storage-type" {
								item.Type.AsMap.Value = item.Type.AsMap.Key
							} else {
								item.Fallback = append(item.Fallback, 1)
							}
						}
					}
				case "call-index", "event-index":
					typeId, name := pallet.Calls.Type, CallCommitTimelocked
					if fault == "event-index" {
						typeId, name = pallet.Events.Type, "TimelockedWeightsCommitted"
					}
					variants := metadata.AsMetadataV14.EfficientLookup[typeId.Int64()].Def.Variant.Variants
					for variantIndex := range variants {
						if string(variants[variantIndex].Name) == name {
							variants[variantIndex].Index++
						}
					}
				}
			}
		}
		artifact := fixture.bind(t)
		if err := fixture.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err == nil {
			t.Errorf("%s gained production capability", fault)
		}
		if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err == nil {
			t.Errorf("%s retained production capability after refusal", fault)
		}
	}
}

// A foreign proof, fabricated fields and API drift cannot be legitimized by
// warm metadata. Failure keeps a previously successful view intact.
func TestValidatorProducerRuntimeRejectsForeignProofAndApiDrift(t *testing.T) {
	t.Parallel()
	fixture := validatorProducerRuntimeFixture(t)
	artifact := fixture.bind(t)
	if err := fixture.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err != nil {
		t.Fatal(err)
	}
	foreign := validatorProducerRuntimeFixture(t)
	if err := foreign.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err == nil || foreign.calls != 0 {
		t.Fatal("foreign proof reached production purpose reads")
	}
	fabricated := artifact
	fabricated.authenticationProof = nil
	if err := fixture.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), fabricated); err == nil {
		t.Fatal("fabricated exported artifact acquired production authority")
	}
	client := fixture.chain.API.Client.(*runtimeIdentityTestClient)
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if method != "state_getRuntimeVersion" || len(args) != 1 || args[0] != artifact.BlockHash.Hex() {
			return fmt.Errorf("unexpected purpose call %s %v", method, args)
		}
		version := artifact.Version
		return setRuntimeIdentityTestResult(result, map[string]any{"specName": version.SpecName, "specVersion": version.SpecVersion,
			"transactionVersion": 1, "stateVersion": 1, "apis": []any{[]any{"0x8375104b299b74c5", 3}}})
	}
	if err := fixture.chain.BindValidatorProducerRuntimeArtifactContext(t.Context(), artifact); err == nil {
		t.Fatal("changed runtime API acquired production authority")
	}
	if err := fixture.chain.ValidateValidatorProducerRuntime(fixture.identity); err != nil {
		t.Fatalf("failed binding changed old view: %v", err)
	}
}

// Cancellation is forced inside the last purpose read and joined before the
// assertion; no metadata or signing capability is published afterward.
func TestValidatorProducerRuntimeCancellationRetainsPriorView(t *testing.T) {
	t.Parallel()
	fixture := validatorProducerRuntimeFixture(t)
	artifact := fixture.bind(t)
	oldMeta, oldRuntime := fixture.chain.Meta, fixture.chain.Runtime
	started := make(chan struct{})
	fixture.chain.API.Client.(*runtimeIdentityTestClient).callContext = func(ctx context.Context, result any, method string, args ...any) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- fixture.chain.BindValidatorProducerRuntimeArtifactContext(ctx, artifact) }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	if fixture.chain.Meta != oldMeta || fixture.chain.Runtime != oldRuntime || fixture.chain.ValidateValidatorProducerRuntime(fixture.identity) == nil {
		t.Fatal("canceled purpose authentication changed authority")
	}
}
