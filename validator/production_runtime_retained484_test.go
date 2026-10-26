// A dependency recut must retain previously approved source capabilities while
// the normal signed loader still authenticates every synthetic config byte.
package validator

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/urfoundation/sn/crv4"
)

// The signed current approval remains readable without constructing a signer.
func TestProductionRuntimeRetainsApprovedRuntime484Config(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	fixture.cfg.RuntimeSpec = 484
	fixture.approval.Proposal.Runtime.Version.SpecVersion = 484
	fixture.approval.Proposal.Runtime.SourceCommit = crv4.NativeOwnerSource484
	fixture.signConfig(t)
	before, err := os.ReadFile(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadReleaseConfig(fixture.path)
	if err != nil {
		t.Fatalf("normal loader lost the previously approved source capability: %v", err)
	}
	if loaded.RuntimeSpec != 484 {
		t.Fatal("normal loader changed the approved runtime tuple")
	}
	after, err := os.ReadFile(fixture.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("normal loader changed the signed source config", err)
	}
	if _, err := os.Stat(loaded.HotkeySeedFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("normal config parsing constructed signer custody", err)
	}
}

// API staging uses its actual retained-approval loader before any refresh.
func TestValidatorUploadProductionRuntimeRetainsApprovedRuntime484Config(t *testing.T) {
	fixture := newValidatorUploadProductionTestFixture(t)
	production := fixture.production
	production.cfg.RuntimeSpec = 484
	production.approval.Proposal.Runtime.Version.SpecVersion = 484
	production.approval.Proposal.Runtime.SourceCommit = crv4.NativeOwnerSource484
	production.signConfig(t)
	raw, err := os.ReadFile(production.path)
	if err != nil {
		t.Fatal(err)
	}
	reference := mainnetRuntimeTestWriteBytes(t, fixture.config.ProductionRuntimeConfig.Path, raw)
	deployment := fixture.config.Deployment
	deployment.NativeRuntime = releaseNativeRuntimeIdentity(production.cfg)
	loaded, err := loadValidatorUploadRuntimeContext(t.Context(), deployment, reference)
	if err != nil {
		t.Fatalf("normal staging loader lost the retained source capability: %v", err)
	}
	if _, err := loaded.runtimeAuthority(); err != nil {
		t.Fatal("normal staging loader omitted its authenticated projection", err)
	}
}

// The preservation grants no capability to another source, even when signed.
func TestProductionRuntimeRetainedRuntime484RefusesForeignSource(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	fixture.cfg.RuntimeSpec = 484
	fixture.approval.Proposal.Runtime.Version.SpecVersion = 484
	fixture.approval.Proposal.Runtime.SourceCommit = strings.Repeat("a", 40)
	fixture.signConfig(t)
	if _, err := LoadReleaseConfig(fixture.path); err == nil {
		t.Fatal("a signed but unreviewed source acquired preserved capability")
	}
}
