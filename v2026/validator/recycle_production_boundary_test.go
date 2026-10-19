// Current writer admission and legacy observation refusal have distinct
// diagnostics. Neither wording nor a schema field can create authority.
package validator

import (
	"strings"
	"testing"
)

// A real independently signed observation approval still cannot construct a
// writer. The refusal identifies its scope rather than denying current support.
func TestOwnerRecycleLegacyWriterRefusalNamesProductionAuthority(t *testing.T) {
	fixture := newRecycleAdmissionFixture(t, nil)
	writer, err := NewReleaseSteerer(fixture.cfg, nil, nil, nil, nil)
	if writer != nil || err == nil || !strings.Contains(err.Error(), "legacy configuration") || !strings.Contains(err.Error(), "schema-3 production authority") || strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("legacy observation selected a writer or misreported production support: %v", err)
	}
	forged := *fixture.cfg
	forged.SchemaVersion = ReleaseMainnetProductionSchemaVersion
	writer, err = NewReleaseSteerer(&forged, nil, nil, nil, nil)
	if writer != nil || err == nil || !strings.Contains(err.Error(), "lacks independently loaded authority") {
		t.Fatalf("changing only schema manufactured current production authority: %v", err)
	}
}

// Genuine current approval passes the boundary but cannot use the old writer.
// An exact original retained owner remains ineligible for current submission.
func TestOwnerRecycleProductionBoundaryKeepsCurrentAndHistoricalOwnersDistinct(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	if err := ownerRecycleProductionBoundary(fixture.cfg); err != nil {
		t.Fatal("authenticated current producer hit the legacy refusal", err)
	}
	writer, err := NewReleaseSteerer(fixture.cfg, nil, nil, nil, nil)
	if writer != nil || err == nil || !strings.Contains(err.Error(), "concrete V2 runtime and durable intent owner") {
		t.Fatalf("current approval bypassed the actual V2 runtime owner: %v", err)
	}
	historical := *fixture.cfg
	owner := *fixture.cfg.ownerRecycleProduction
	owner.historicalOnly = true
	historical.ownerRecycleProduction = &owner
	if err := ownerRecycleProductionBoundary(&historical); err == nil || !strings.Contains(err.Error(), "cannot start a current writer") {
		t.Fatal("historical production approval became fresh writer authority", err)
	}
	if err := ownerRecycleProductionBoundary(fixture.cfg); err != nil {
		t.Fatal("historical refusal mutated the original current owner", err)
	}
}
