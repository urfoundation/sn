//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

func historyAdoptionRequestTest(t *testing.T) (*ReleaseHistoryAdoptionV2, string) {
	t.Helper()
	configPath := writeReleaseConfig(t, validReleaseConfig(t))
	encoded, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(filepath.Dir(configPath), "coordinator-state-v2")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return &ReleaseHistoryAdoptionV2{Schema: ReleaseHistoryAdoptionV2Schema, DeploymentID: "test-deployment", ValidatorID: 1,
		ApprovedPlanHash: "0x" + strings.Repeat("11", 32), SourcePlanHash: "0x" + strings.Repeat("22", 32),
		ConfigSHA256: ReleaseMeasurementContentHash(encoded), CoordinatorStateDir: root, IntentPrefixSHA256: ReleaseMeasurementContentHash(nil), FirstNativeEpoch: 1410}, configPath
}

func encodeHistoryAdoptionRequestTest(t *testing.T, request *ReleaseHistoryAdoptionV2) []byte {
	t.Helper()
	encoded, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(encoded, '\n')
}

func TestReleaseHistoryAdoptionV2PinsConfigNamespaceAndOptions(t *testing.T) {
	t.Parallel()
	request, configPath := historyAdoptionRequestTest(t)
	encoded := encodeHistoryAdoptionRequestTest(t, request)
	decoded, err := DecodeReleaseHistoryAdoptionV2(encoded, ReleaseMeasurementContentHash(encoded))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadReleaseConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	operators, evidence := append([]OperatorConfig(nil), cfg.Operators...), cfg.EvidenceV2
	if err := decoded.configure(cfg, configPath); err != nil {
		t.Fatal(err)
	}
	if cfg.StateDir != request.CoordinatorStateDir || cfg.ProvisionalDeferClosedNativeInput || !reflect.DeepEqual(cfg.Operators, operators) || !reflect.DeepEqual(cfg.EvidenceV2, evidence) {
		t.Fatal("adoption changed signed operator owners, activation inputs or strict mode")
	}
	decoded.FirstNativeEpoch++
	if cfg.historyAdoptionV2.FirstNativeEpoch != request.FirstNativeEpoch {
		t.Fatal("borrowed request mutation changed admitted options")
	}
	for _, mutate := range []func(*ReleaseHistoryAdoptionV2){
		func(r *ReleaseHistoryAdoptionV2) { r.FirstNativeEpoch++ },
		func(r *ReleaseHistoryAdoptionV2) { r.SourcePlanHash = r.ApprovedPlanHash },
		func(r *ReleaseHistoryAdoptionV2) { r.ConfigSHA256 = ReleaseMeasurementContentHash([]byte("changed")) },
		func(r *ReleaseHistoryAdoptionV2) { r.CoordinatorStateDir = filepath.Dir(r.CoordinatorStateDir) },
	} {
		changed := *request
		mutate(&changed)
		if _, err := DecodeReleaseHistoryAdoptionV2(encodeHistoryAdoptionRequestTest(t, &changed), ReleaseMeasurementContentHash(encoded)); err == nil {
			t.Fatal("changed adoption options retained the original approval pin")
		}
	}
	if _, err := DecodeReleaseHistoryAdoptionV2(append(bytes.Clone(encoded), ' '), ReleaseMeasurementContentHash(append(bytes.Clone(encoded), ' '))); err == nil {
		t.Fatal("noncanonical request was admitted")
	}
	for _, fault := range []string{"config", "namespace", "provisional", "mode"} {
		t.Run(fault, func(t *testing.T) {
			r, path := historyAdoptionRequestTest(t)
			c, err := LoadReleaseConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "config":
				r.ConfigSHA256 = ReleaseMeasurementContentHash([]byte("foreign config"))
			case "namespace":
				r.CoordinatorStateDir = filepath.Join(filepath.Dir(path), "state")
			case "provisional":
				c.ProvisionalDeferClosedNativeInput = true
			case "mode":
				if err := os.Chmod(r.CoordinatorStateDir, 0o755); err != nil { t.Fatal(err) }
			}
			if err := r.configure(c, path); err == nil {
				t.Fatal("changed source authority reached strict startup")
			}
		})
	}
}

// These inert intent values exercise only canonical prefix binding. They are
// not submitted to, or accepted by, any native/measurement authentication API.
func TestReleaseHistoryAdoptionV2RetainsExactPrefixAndOneFreshBridge(t *testing.T) {
	t.Parallel()
	request, _ := historyAdoptionRequestTest(t)
	all := make([]SteeringIntent, 3)
	for index, epoch := range []uint64{1401, 1404, 1405} {
		all[index] = SteeringIntent{SubnetEpoch: epoch, SettlementEpoch: []uint64{302, 306, 307}[index], Status: "applied",
			VectorHash: ReleaseMeasurementContentHash([]byte{byte(index)}), MeasurementArtifactHash: ReleaseMeasurementContentHash([]byte{byte(index + 3)}),
			Prepared: &crv4.PreparedSubmission{SourceCommitment: &crv4.PreparedSourceCommitment{Hash: "inert-prefix-binding-fixture"}}}
	}
	file := steeringIntentFile{Schema: steeringIntentSchema, History: all[:2], Current: &all[2]}
	encoded, err := marshalAttemptSettlementV2JSON(t.Context(), &file, 1<<20, true, true)
	if err != nil { t.Fatal(err) }
	request.IntentPrefixCount, request.IntentPrefixSHA256, request.LastNativeEpoch, request.LastArtifactHash = 3, ReleaseMeasurementContentHash(encoded), 1405, all[2].MeasurementArtifactHash
	owner, err := request.matchPrefix(t.Context(), &file, encoded, 1<<20)
	if err != nil { t.Fatal(err) }
	if !owner.allowsIntentEdge(&all[0], &all[1]) || !owner.allowsIntentEdge(&all[1], &all[2]) {
		t.Fatal("exact retained sparse edges were lost")
	}
	fresh := SteeringIntent{SubnetEpoch: request.FirstNativeEpoch, SettlementEpoch: 312}
	if !owner.allowsIntentEdge(&all[2], &fresh) || owner.requireFirstEpoch(&all[2], fresh.SubnetEpoch) != nil {
		t.Fatal("exact first fresh bridge was lost")
	}
	for _, epoch := range []uint64{1406, 1409, 1411, 1420} {
		changed := fresh
		changed.SubnetEpoch = epoch
		if owner.allowsIntentEdge(&all[2], &changed) || owner.requireFirstEpoch(&all[2], epoch) == nil {
			t.Fatalf("adoption authorized unapproved first epoch %d", epoch)
		}
	}
	later := SteeringIntent{SubnetEpoch: fresh.SubnetEpoch+2}
	if owner.allowsIntentEdge(&fresh, &later) || validateSteeringIntentSuccessorWithGapsV2(&fresh, &later, owner.allowsIntentEdge(&fresh, &later)) == nil {
		t.Fatal("first bridge became an unconditional future gap permission")
	}
	appended := steeringIntentFile{Schema: steeringIntentSchema, History: append(append([]SteeringIntent(nil), all...), fresh), Current: &later}
	if _, err := request.matchPrefix(t.Context(), &appended, []byte("later canonical file is checked by caller"), 1<<20); err != nil {
		t.Fatalf("append-only restart lost the original prefix: %v", err)
	}
	for _, fault := range []string{"receipt", "drop", "reorder", "source", "pending"} {
		copyAll := append([]SteeringIntent(nil), all...)
		switch fault {
		case "receipt": copyAll[1].ApplicationBlock++
		case "drop": copyAll = copyAll[1:]
		case "reorder": copyAll[0], copyAll[1] = copyAll[1], copyAll[0]
		case "source": copyAll[1].MeasurementArtifactHash = request.LastArtifactHash
		case "pending": copyAll[2].Status = "pending"
		}
		changed := steeringIntentFile{Schema: steeringIntentSchema, History: copyAll[:len(copyAll)-1], Current: &copyAll[len(copyAll)-1]}
		if _, err := request.matchPrefix(t.Context(), &changed, encoded, 1<<20); err == nil {
			t.Fatalf("%s changed the pinned historical prefix", fault)
		}
	}
	cancelled, cancel := context.WithCancel(t.Context()); cancel()
	if _, err := request.matchPrefix(cancelled, &file, encoded, 1<<20); !errors.Is(err, context.Canceled) {
		t.Fatalf("prefix ignored cancellation: %v", err)
	}
}

// The real bootstrap/native/EVM/two-replica startup remains strict when a
// source-pinned empty intent prefix is adopted. No provisional token is issued.
func TestReleaseHistoryAdoptionV2KeepsStrictStartupAuthentication(t *testing.T) {
	t.Parallel()
	for _, corrupt := range []bool{false, true} {
		fixture := newReleaseStartupV2TestFixture(t, false)
		fixture.cfg.historyAdoptionV2 = &ReleaseHistoryAdoptionV2{IntentPrefixSHA256: ReleaseMeasurementContentHash(nil), FirstNativeEpoch: 10}
		if corrupt {
			fixture.blocks[fixture.boundary.EVMBlock] = [32]byte{0xff}
		}
		err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO())
		if corrupt {
			if err == nil { t.Fatal("adoption bypassed changed canonical EVM history") }
			requireReleaseStartupV2Dormant(t, fixture.disk)
		} else if err != nil {
			t.Fatalf("real strict startup rejected exact empty prefix: %v", err)
		}
	}
}

func TestReleaseHistoryAdoptionV2RequiresCompleteActualApplicationRow(t *testing.T) {
	t.Parallel()
	intent := &SteeringIntent{UIDs: []uint16{3, 4, 7, 8}, Values: []uint16{65517, 65535, 24071, 32094}}
	row := []crv4.WeightPair{{UID: 8, Value: 32094}, {UID: 7, Value: 24071}, {UID: 4, Value: 65535}, {UID: 3, Value: 65517}}
	if err := matchAdoptedApplicationRowV2(intent, row); err != nil { t.Fatal(err) }
	for _, fault := range []string{"changed", "missing", "extra", "duplicate", "foreign"} {
		changed := append([]crv4.WeightPair(nil), row...)
		switch fault {
		case "changed": changed[0].Value--
		case "missing": changed = changed[:len(changed)-1]
		case "extra": changed = append(changed, crv4.WeightPair{UID: 9, Value: 0})
		case "duplicate": changed[1] = changed[0]
		case "foreign": changed[0].UID = types.U16(9)
		}
		if err := matchAdoptedApplicationRowV2(intent, changed); err == nil { t.Fatalf("%s actual row passed", fault) }
	}
}
