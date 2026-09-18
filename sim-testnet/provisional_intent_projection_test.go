//go:build linux || darwin

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// This fixture begins at the existing authenticated local observer boundary.
// No chain receipt or fully replayed measurement is asserted by the fixture.
func provisionalIntentProjectionTest(t *testing.T) (*ResolvedConfig, ValidatorObservation, string, validatorpkg.SteeringIntent, *validatorpkg.ReleaseMeasurementArtifact) {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true}}
	cfg.Config.Topology.HeadSlots = 1
	source := scenarioNativeSourceV2TestFixture()
	intent, artifact := source.References[0].Intent, source.References[0].Artifact
	intent.Schema, intent.Netuid, intent.PolicyHash = validatorpkg.SteeringIntentSchema, cfg.Netuid, cfg.PolicyHash
	intent.MaskedUIDs = []uint16{intent.SelfUID}
	intent.DepositAudits = []validatorpkg.DepositAudit{{NoID: 1, Epoch: 4, SourceEpoch: 3, Status: validatorpkg.DepositAuditCompliant, Compliant: true, Disposition: "pool_weight_eligible", RequiredDepositRao: "1", ObservedDepositRao: "1", ConvictionBeforeRao: "0", ArtifactHash: bytesSHA256([]byte("synthetic deposit audit"))}}
	artifact.Schema, artifact.DeploymentID, artifact.ChainID, artifact.GenesisHash = validatorpkg.ReleaseMeasurementSchemaV2, cfg.Config.Deployment.DeploymentID, cfg.ChainID, cfg.Public.Chain.GenesisHash
	artifact.Coordinator, artifact.SettlementVault = "0x"+strings.Repeat("11", 20), "0x"+strings.Repeat("22", 20)
	artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash, artifact.EVMSnapshotBlock, artifact.EVMSnapshotHash = intent.NativeSnapshotBlock, intent.NativeSnapshotHash, intent.EVMSnapshotBlock, intent.EVMSnapshotHash
	artifact.DepositAudits = append([]validatorpkg.DepositAudit(nil), intent.DepositAudits...)
	artifact.ValidatorID, artifact.Netuid, artifact.PolicyHash = intent.ValidatorID, intent.Netuid, intent.PolicyHash
	artifact.SubnetEpoch, artifact.SettlementEpoch, artifact.SelfUID = intent.SubnetEpoch, intent.SettlementEpoch, intent.SelfUID
	raw, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	intent.MeasurementArtifactHash = bytesSHA256(raw)
	intent.MeasurementArtifactSize = uint64(len(raw))
	intent.MeasurementArtifactPath = "measurements/" + strings.TrimPrefix(intent.MeasurementArtifactHash, "sha256:") + ".json"
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(stateDir, "measurements"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, intent.MeasurementArtifactPath), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	pair, err := crv4.KeypairFromSeed([32]byte{0x63})
	if err != nil {
		t.Fatal(err)
	}
	hotkey := pair.PublicKey()
	intent.Prepared = &crv4.PreparedSubmission{Netuid: intent.Netuid, SubnetEpoch: intent.SubnetEpoch, UIDs: append([]uint16(nil), intent.UIDs...), Values: append([]uint16(nil), intent.Values...), HotkeyHex: "0x" + hex.EncodeToString(hotkey[:]), ExtrinsicHash: "0x" + strings.Repeat("73", 32)}
	envelope := validatorpkg.ReleaseMeasurementEnvelope{
		Schema: validatorpkg.ReleaseMeasurementEnvelopeSchemaV2, MeasurementSchema: artifact.Schema,
		DeploymentID: artifact.DeploymentID, ChainID: artifact.ChainID, GenesisHash: artifact.GenesisHash,
		Coordinator: artifact.Coordinator, SettlementVault: artifact.SettlementVault, ValidatorID: artifact.ValidatorID,
		ValidatorHotkey: intent.Prepared.HotkeyHex, ValidatorUID: intent.SelfUID, Netuid: artifact.Netuid,
		SubnetEpoch: artifact.SubnetEpoch, SettlementEpoch: artifact.SettlementEpoch, PolicyHash: artifact.PolicyHash,
		NativeSnapshotBlock: artifact.NativeSnapshotBlock, NativeSnapshotHash: artifact.NativeSnapshotHash,
		EVMSnapshotBlock: artifact.EVMSnapshotBlock, EVMSnapshotHash: artifact.EVMSnapshotHash,
		MeasurementArtifactHash: intent.MeasurementArtifactHash, MeasurementArtifactSize: intent.MeasurementArtifactSize,
		PreparedExtrinsicHash: intent.Prepared.ExtrinsicHash, SignedAt: "2025-01-02T03:04:05Z", SignatureScheme: "sr25519",
	}
	writeProvisionalProjectionEnvelopeTest(t, stateDir, &intent, &envelope, true)
	intent.VectorHash, err = intent.ReconstructedVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	file := struct {
		Schema  string                        `json:"schema"`
		Current *validatorpkg.SteeringIntent  `json:"current,omitempty"`
		History []validatorpkg.SteeringIntent `json:"history"`
	}{Schema: validatorpkg.SteeringIntentSchema, Current: &intent, History: []validatorpkg.SteeringIntent{}}
	raw, err = json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	path := filepath.Join(stateDir, "steering-intents.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	finalized, applied := 1, 1
	observed := ValidatorObservation{ValidatorID: 1, LocalRuntimeIntents: &validatorpkg.ProvisionalIntentObservationV2{
		Scope: "local-runtime-observation", State: "observed", StateDirectory: stateDir,
		StoreSHA256: bytesSHA256(raw), HandoffSHA256: bytesSHA256([]byte("synthetic pinned handoff")),
		RecordedFinalizedIntents: &finalized, RecordedAppliedIntents: &applied,
		Receipts: []validatorpkg.ProvisionalIntentReceiptV2{{Status: intent.Status, VectorHash: intent.VectorHash, SubnetEpoch: intent.SubnetEpoch, SettlementEpoch: intent.SettlementEpoch, FinalizedBlock: intent.FinalizedBlock, ApplicationBlock: intent.ApplicationBlock}},
	}}
	return cfg, observed, path, intent, artifact
}

func TestProvisionalIntentProjectionPreservesOperationalFieldsWithoutPromotion(t *testing.T) {
	t.Parallel()
	cfg, observed, path, intent, artifact := provisionalIntentProjectionTest(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	got := inspectProvisionalValidatorIntentObserved(context.Background(), cfg, 1, func() (ValidatorObservation, string) {
		calls++
		copy := *observed.LocalRuntimeIntents
		copy.ObservedAt = strings.Repeat("x", calls)
		value := observed
		value.LocalRuntimeIntents = &copy
		return value, "synthetic-supervisor-and-validator-generation"
	})
	if got.Error != "" || got.ValuesHash == "" || got.VectorHash != intent.VectorHash || got.SelfUID != intent.SelfUID || len(got.DepositAudits) != 1 || len(got.AppliedWeights) != 2 || len(got.HeadDecisions) != 1 || len(got.HeadDecisions[0].CandidateFleetUIDs) != 2 || len(got.SelectedHeadUIDs) != 1 || len(got.IntentHashes) != 1 {
		t.Fatalf("provisional local projection lost operational evidence: %+v", got)
	}
	if calls != 2 || got.LocalRuntimeIntents == nil || got.LocalRuntimeIntents.FinalAcceptance || got.LocalRuntimeIntents.Scope != "local-runtime-observation" || got.FinalizedIntents != 0 || got.AppliedIntents != 0 || got.NativeSourceScopeV2 != "" || got.NativeSourceStoreSHA256 != "" || len(got.NativeCommitsV2) != 0 || got.HeadDecisions[0].CommitNativeEpoch != 0 {
		t.Fatal("local projection promoted local claims or omitted its second observation")
	}
	strict := projectValidatorIntent([]validatorpkg.SteeringIntent{intent}, 1, cfg.Config.Topology.HeadSlots, cfg.Config.Topology.fleetCandidates(), func(*validatorpkg.SteeringIntent) (*validatorpkg.ReleaseMeasurementArtifact, error) {
		return artifact, nil
	})
	if strict.FinalizedIntents != 1 || strict.AppliedIntents != 1 || strict.LocalRuntimeIntents != nil {
		t.Fatal("shared projection changed strict counters")
	}
	strict.FinalizedIntents, strict.AppliedIntents = 0, 0
	strict.LocalRuntimeIntents = got.LocalRuntimeIntents
	if !reflect.DeepEqual(strict, got) {
		t.Fatal("local projection changed operational thresholds or evidence")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("observation changed original local store", err)
	}
}

func TestProvisionalIntentProjectionRejectsStoreHandoffAndGenerationChanges(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"store", "handoff", "receipt", "generation", "identity", "final-acceptance", "scope", "projection-bytes", "measurement-bytes"} {
		cfg, observed, path, intent, _ := provisionalIntentProjectionTest(t)
		calls := 0
		got := inspectProvisionalValidatorIntentObserved(context.Background(), cfg, 1, func() (ValidatorObservation, string) {
			calls++
			value := observed
			copy := *observed.LocalRuntimeIntents
			copy.Receipts = append([]validatorpkg.ProvisionalIntentReceiptV2(nil), copy.Receipts...)
			value.LocalRuntimeIntents = &copy
			generation := "synthetic-generation"
			if calls == 1 && fault == "projection-bytes" {
				if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if calls == 1 && fault == "measurement-bytes" {
				if err := os.WriteFile(filepath.Join(filepath.Dir(path), intent.MeasurementArtifactPath), []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if calls == 2 {
				switch fault {
				case "store":
					copy.StoreSHA256 = bytesSHA256([]byte("changed store"))
				case "handoff":
					copy.HandoffSHA256 = bytesSHA256([]byte("changed handoff"))
				case "receipt":
					copy.Receipts[0].ApplicationBlock++
				case "generation":
					generation = "next-generation"
				case "identity":
					value.ValidatorID++
				case "final-acceptance":
					copy.FinalAcceptance = true
				case "scope":
					copy.Scope = scenarioNativeSourceScopeV2
				}
			}
			return value, generation
		})
		if got.Error == "" || got.ValuesHash != "" || len(got.DepositAudits) != 0 || len(got.AppliedWeights) != 0 || len(got.HeadDecisions) != 0 || got.FinalizedIntents != 0 || got.AppliedIntents != 0 || got.LocalRuntimeIntents.State != "unknown" || got.LocalRuntimeIntents.RecordedAppliedIntents != nil {
			t.Fatalf("%s retained partial local projection after an ownership change: %+v", fault, got)
		}
	}
}

func TestProvisionalIntentProjectionRejectsWrongIdentityAndOrdinaryMode(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"ordinary", "validator", "policy", "netuid", "promoted"} {
		cfg, observed, _, _, _ := provisionalIntentProjectionTest(t)
		switch fault {
		case "ordinary":
			cfg.provisionalResume = nil
		case "validator":
			observed.ValidatorID++
		case "policy":
			cfg.PolicyHash = bytesSHA256([]byte("different policy"))
		case "netuid":
			cfg.Netuid++
		case "promoted":
			cfg.provisionalResume.Record.FinalAcceptance = true
		}
		calls := 0
		got := inspectProvisionalValidatorIntentObserved(context.Background(), cfg, 1, func() (ValidatorObservation, string) { calls++; return observed, "synthetic-generation" })
		if got.Error == "" || got.ValuesHash != "" || len(got.AppliedWeights) != 0 || got.LocalRuntimeIntents.State != "unknown" {
			t.Fatalf("%s entered local operational projection", fault)
		}
		if (fault == "ordinary" || fault == "promoted") && calls != 0 {
			t.Fatalf("%s consumed provisional observation authority", fault)
		}
	}
	cfg, observed, _, _, _ := provisionalIntentProjectionTest(t)
	observed.LocalRuntimeIntents.State = "absent"
	observed.LocalRuntimeIntents.StoreSHA256 = ""
	observed.LocalRuntimeIntents.Receipts = nil
	zero := 0
	observed.LocalRuntimeIntents.RecordedAppliedIntents, observed.LocalRuntimeIntents.RecordedFinalizedIntents = &zero, &zero
	got := inspectProvisionalValidatorIntentObserved(context.Background(), cfg, 1, func() (ValidatorObservation, string) { return observed, "synthetic-generation" })
	if got.Error != "" || got.ValuesHash != "" || len(got.AppliedWeights) != 0 || got.LocalRuntimeIntents.State != "absent" {
		t.Fatal("absent store invented evidence or failed its honest observation")
	}
}

func writeProvisionalProjectionEnvelopeTest(t *testing.T, stateDir string, intent *validatorpkg.SteeringIntent, envelope *validatorpkg.ReleaseMeasurementEnvelope, sign bool) {
	t.Helper()
	if sign {
		envelope.SigningHash, envelope.Signature = "", ""
		unsigned, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(append([]byte(validatorpkg.ReleaseMeasurementEnvelopeSigningDomainV2+"\x00"), unsigned...))
		pair, err := crv4.KeypairFromSeed([32]byte{0x63})
		if err != nil {
			t.Fatal(err)
		}
		signature, err := pair.Sign(digest[:])
		if err != nil {
			t.Fatal(err)
		}
		envelope.SigningHash, envelope.Signature = "sha256:"+hex.EncodeToString(digest[:]), "0x"+hex.EncodeToString(signature)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	intent.MeasurementEnvelopeHash, intent.MeasurementEnvelopeSize = bytesSHA256(raw), uint64(len(raw))
	intent.MeasurementEnvelopePath = "measurements/envelopes/" + strings.TrimPrefix(intent.MeasurementEnvelopeHash, "sha256:") + ".json"
	if err := os.MkdirAll(filepath.Join(stateDir, "measurements", "envelopes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, intent.MeasurementEnvelopePath), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProvisionalIntentMeasurementRejectsNoncanonicalCorruptAndMismatchedEvidence(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"noncanonical", "snapshot", "decision", "signature", "envelope-binding", "hotkey", "artifact-hash", "prepared-hash"} {
		cfg, observed, _, intent, artifact := provisionalIntentProjectionTest(t)
		dir := observed.LocalRuntimeIntents.StateDirectory
		var want string
		switch fault {
		case "noncanonical":
			raw, err := json.MarshalIndent(artifact, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			raw = append(raw, '\n')
			intent.MeasurementArtifactHash, intent.MeasurementArtifactSize = bytesSHA256(raw), uint64(len(raw))
			intent.MeasurementArtifactPath = "measurements/" + strings.TrimPrefix(intent.MeasurementArtifactHash, "sha256:") + ".json"
			if err := os.WriteFile(filepath.Join(dir, intent.MeasurementArtifactPath), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			want = "local measurement bytes are not canonical V2"
		case "snapshot":
			intent.NativeSnapshotBlock++
			want = "local measurement identity or decision differs"
		case "decision":
			intent.Prepared.Values = []uint16{1, 2}
			want = "local measurement prepared decision differs"
		default:
			raw, err := os.ReadFile(filepath.Join(dir, intent.MeasurementEnvelopePath))
			if err != nil {
				t.Fatal(err)
			}
			var envelope validatorpkg.ReleaseMeasurementEnvelope
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "signature":
				envelope.Signature = "0x" + strings.Repeat("00", 64)
				want = "signature is invalid"
			case "envelope-binding":
				envelope.ValidatorUID++
				want = "signed envelope binding differs"
			case "hotkey":
				intent.Prepared.HotkeyHex = "0x" + strings.Repeat("43", 32)
				want = "signed envelope binding differs"
			case "artifact-hash":
				envelope.MeasurementArtifactHash = bytesSHA256([]byte("other measurement"))
				want = "signed envelope binding differs"
			case "prepared-hash":
				envelope.PreparedExtrinsicHash = "0x" + strings.Repeat("53", 32)
				want = "signed envelope binding differs"
			}
			writeProvisionalProjectionEnvelopeTest(t, dir, &intent, &envelope, fault != "signature")
		}
		got, err := readProvisionalIntentMeasurement(context.Background(), cfg, dir, &intent)
		if err == nil || got != nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s local signed content admitted or wrong refusal: %v", fault, err)
		}
	}
}
