//go:build linux || darwin

// The census and every eligibility read authenticate the runtime at one
// refresh's current hash. On a successor-path runtime each authentication
// reads the runtime version and code hash, and the first in a process also
// reads and hashes the full metadata; through the api's forward each read is
// a round trip to the archive. A refresh now authenticates its current hash
// once and says on its log line where its time went.
package validator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/crv4"
)

// Two refreshes at a successor runtime's head: the first admits the successor
// and reads its metadata at most once, the second reads none, and each
// authenticates the head's runtime once. Before, the native observer, the
// census and each eligibility read authenticated it again.
func TestValidatorUploadRefreshAuthenticatesTheCurrentRuntimeOnce(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadProductionTestFixture(t)
	production := fixture.production
	production.cfg.RuntimeSuccessorProfile = crv4.ValidatorProducerRuntimeProfile
	production.resignAndLoad(t)
	raw, err := os.ReadFile(production.path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.config.ProductionRuntimeConfig = mainnetRuntimeTestWriteBytes(t, filepath.Join(identityTestStateDir(t), "successor-production.yml"), raw)
	successor := productionUpgradeTestMetadata(t, production.rpc.metadata, nil)
	head := uint64(170)
	counts := map[string]int{}
	inner := production.rpc.client.callContext
	production.rpc.client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		if (method == "state_getRuntimeVersion" || method == "state_getStorageHash" || method == "state_getMetadata") && len(args) != 0 {
			hash, err := types.NewHashFromHexString(fmt.Sprint(args[len(args)-1]))
			if err != nil {
				return err
			}
			if number, err := mainnetRuntimeTestNumber(hash); err == nil && number >= productionSuccessorTestUpgradeBlock {
				if number == head {
					counts[method]++
				}
				switch method {
				case "state_getRuntimeVersion":
					return setValidatorRuntimeIdentityTestResult(result, map[string]any{"specName": "node-subtensor", "specVersion": production.cfg.RuntimeSpec + 1,
						"transactionVersion": 1, "stateVersion": 1, "apis": []any{[]any{"0x8375104b299b74c5", 2}}})
				case "state_getStorageHash":
					return setValidatorRuntimeIdentityTestResult(result, releaseHex32([32]byte{0x47, 0x04}))
				default:
					return setValidatorRuntimeIdentityTestResult(result, successor)
				}
			}
		}
		return inner(ctx, result, method, args...)
	}
	owner := fixture.owner(t)
	var reports []validatorUploadRefreshReport
	owner.refreshReport = func(report validatorUploadRefreshReport) { reports = append(reports, report) }
	if err := owner.refresh(t.Context(), fixture.upload.now); err != nil || len(owner.entries) != 1 {
		t.Fatalf("prerequisite: the approved runtime admits: %v", err)
	}
	production.rpc.head = head
	for refresh, maximumFetches := range []int{1, 0} {
		clear(counts)
		if err := owner.refresh(t.Context(), fixture.upload.now); err != nil || len(owner.entries) != 1 {
			t.Fatalf("refresh %d at the successor head: %v", refresh, err)
		}
		if counts["state_getMetadata"] > maximumFetches {
			t.Fatalf("refresh %d read the successor's metadata %d times, at most %d", refresh, counts["state_getMetadata"], maximumFetches)
		}
		// One successor-path authentication reads the code hash twice: for
		// the successor gate and for the selected artifact. The stake read's
		// API capability check still reads the version itself.
		if counts["state_getStorageHash"] != 2 {
			t.Fatalf("refresh %d read the current runtime's code hash %d times, not once per one authentication (2)", refresh, counts["state_getStorageHash"])
		}
		report := reports[len(reports)-1]
		if report.runtime.Authenticated != 1 || report.runtime.Reused == 0 || report.runtime.MetadataFetches != counts["state_getMetadata"] {
			t.Fatalf("refresh %d reports runtime authentication %+v for %v", refresh, report.runtime, counts)
		}
	}
}

// Every refresh line, published or failed, names its phases.
func TestValidatorUploadRefreshLineNamesItsPhases(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	// The second refresh must read its range again to meet the denied scan.
	fixture.keepDiscoveryRangeOpen()
	owner, reports := validatorUploadSilentDropTestState(t, fixture)
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		t.Fatal(err)
	}
	fixture.fault = "logs-denied"
	if err := owner.refresh(t.Context(), fixture.now); err == nil {
		t.Fatal("prerequisite: a denied event scan fails the refresh")
	}
	for index, report := range *reports {
		line := validatorUploadRefreshReportLine(report)
		for _, phase := range []string{" ranges (", " read), ", "phases: census ", ", discovery ", " ranges kept, eligibility ", ", runtime ", " authentications, ", " reused, ", " metadata fetches"} {
			if !strings.Contains(line, phase) {
				t.Fatalf("refresh %d line lacks %q: %s", index, phase, line)
			}
		}
		if report.censusTook <= 0 || report.discoveryTook <= 0 || report.runtime.Authenticated == 0 {
			t.Fatalf("refresh %d did not time its census, discovery and runtime: %+v", index, report)
		}
	}
	if (*reports)[0].eligibilityTook <= 0 {
		t.Fatal("the publishing refresh did not time its eligibility reads")
	}
}
