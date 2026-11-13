// The complete reviewed release history remains usable with a finite cache.
package crv4

import (
	"context"
	"errors"
	"fmt"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Reviewed authorities reach the actual reader and retain hot metadata while
// they fit; oversized, duplicate and incomplete lists fail before another read.
func TestRuntimeArtifactMetadataAuthenticatesCompleteReviewedIdentityHistory(t *testing.T) {
	metadataHex, metadataHash := runtimeIdentityTestMetadata(t)
	var identities []RuntimeArtifactIdentity
	for _, artifact := range ReviewedRuntimeArtifacts() {
		spec := artifact.Version.SpecVersion
		identities = append(identities, RuntimeArtifactIdentity{
			Version:  RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: spec, TransactionVersion: 1, StateVersion: 1},
			CodeHash: fmt.Sprintf("0x%064x", spec), MetadataHash: metadataHash,
		})
	}
	calls, metadataCalls := 0, 0
	client := &runtimeIdentityTestClient{callContext: func(_ context.Context, result any, method string, args ...any) error {
		calls++
		if len(args) == 0 {
			return errors.New("synthetic history request has no exact block")
		}
		blockHex, ok := args[len(args)-1].(string)
		if !ok {
			return errors.New("synthetic history block has the wrong type")
		}
		block, err := types.NewHashFromHexString(blockHex)
		if err != nil || block[0] == 0 || int(block[0]) > len(identities) {
			return errors.New("synthetic history block is outside its exact census")
		}
		identity := identities[int(block[0])-1]
		switch method {
		case "state_getRuntimeVersion":
			return setRuntimeIdentityTestResult(result, identity.Version)
		case "state_getStorageHash":
			if len(args) != 2 || args[0] != "0x3a636f6465" {
				return errors.New("synthetic history requested another storage key")
			}
			return setRuntimeIdentityTestResult(result, identity.CodeHash)
		case "state_getMetadata":
			metadataCalls++
			return setRuntimeIdentityTestResult(result, metadataHex)
		default:
			return errors.New("synthetic history requested another method")
		}
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	for pass := 0; pass < 2; pass++ {
		for index, identity := range identities {
			block := types.Hash{byte(index + 1)}
			observed, err := AuthenticateRuntimeArtifactAtContext(context.Background(), chain, block, identities...)
			if err != nil || observed.BlockHash != block || observed.Version != identity.Version || observed.CodeHash != identity.CodeHash || observed.MetadataHash != identity.MetadataHash || observed.Metadata == nil {
				t.Fatalf("complete reviewed-identity history failed at pass%d spec%d: %v", pass, identity.Version.SpecVersion, err)
			}
		}
	}
	expectedMetadataCalls := len(identities)
	if len(identities) > maximumRuntimeMetadataCacheEntries {
		// The second complete scan reauthenticates evicted history.
		expectedMetadataCalls *= 2
	}
	if metadataCalls != expectedMetadataCalls || calls != 4*len(identities)+expectedMetadataCalls {
		t.Fatalf("full history exact reads: metadata=%d calls=%d, want %d/%d", metadataCalls, calls, expectedMetadataCalls, 4*len(identities)+expectedMetadataCalls)
	}
	for _, change := range []func([]RuntimeArtifactIdentity) []RuntimeArtifactIdentity{
		func(values []RuntimeArtifactIdentity) []RuntimeArtifactIdentity {
			extra := values[0]
			extra.Version.SpecVersion = 462
			return append(values, extra)
		},
		func(values []RuntimeArtifactIdentity) []RuntimeArtifactIdentity {
			values[len(values)-1] = values[0]
			return values
		},
		func(values []RuntimeArtifactIdentity) []RuntimeArtifactIdentity {
			values[len(values)-1].MetadataHash = ""
			return values
		},
	} {
		changed := change(append([]RuntimeArtifactIdentity(nil), identities...))
		before := calls
		if _, err := AuthenticateRuntimeArtifactAtContext(context.Background(), chain, types.Hash{7}, changed...); err == nil || calls != before {
			t.Fatal("invalid complete-history authority reached a provider")
		}
	}
}
