//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/protocol"
)

func archivePublicationV2TestAdd(t *testing.T, fixture *releaseArchiveV2TestFixture, publication *ValidatorEvidenceCensusV2Publication, manifest any, path string, audit bool) {
	t.Helper()
	raw, err := marshalAttemptSettlementV2JSON(t.Context(), manifest, fixture.options.Config.EvidenceV2.Bounds.MaxClosureBytes, true, true)
	if err != nil {
		t.Fatal(err)
	}
	name, err := filepath.Rel(fixture.options.Config.StateDir, path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.files[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: filepath.ToSlash(name)}] = raw
	kinds := [3]string{"closed-census", "signed-evidence", "terminal-payload"}
	if audit {
		kinds = [3]string{"audit-census", "signed-audit", "audit-payload"}
	}
	for _, origin := range fixture.options.Origins {
		fixture.files[ReleaseEvidenceV2CaptureSource{Kind: kinds[0], Name: attemptHex32(publication.CensusHash), Origin: origin}] = bytes.Clone(publication.Census)
		for _, member := range publication.Members {
			fixture.files[ReleaseEvidenceV2CaptureSource{Kind: kinds[1], Name: attemptHex32(member.SignedArtifactHash), Origin: origin}] = bytes.Clone(member.SignedArtifact)
			fixture.files[ReleaseEvidenceV2CaptureSource{Kind: kinds[2], Name: attemptHex32(sha256.Sum256(member.Payload)), Origin: origin}] = bytes.Clone(member.Payload)
		}
	}
	fixture.repin()
}

func archiveTerminalPublicationV2TestFixture(t *testing.T) (releaseArchiveV2TestFixture, *releasePublicationV2TestFixture) {
	t.Helper()
	fixture := newReleaseArchiveV2TestFixtureWithTrails(t, 1)
	publication := &releasePublicationV2TestFixture{startup: fixture.startup, closure: fixture.last, expected: map[uint64]AttemptCutV2Context{}, readOptions: ValidatorEvidencePublicationV2ReadOptions{Origins: fixture.options.Origins, Bounds: fixture.options.Config.EvidenceV2.Bounds}}
	boundary := fixture.last.Transitions[0].FromBoundary
	publication.readOptions.Window = protocol.ValidatorEvidenceWindow{Epoch: boundary.SettlementEpoch, StartBlock: boundary.EVMBlock - 499, EndBlock: boundary.EVMBlock + 1, FinalizedBlock: boundary.EVMBlock + 1}
	for _, input := range fixture.startup.inputs {
		publication.readOptions.Activations = append(publication.readOptions.Activations, input.Context.Activation)
	}
	for _, transition := range fixture.last.Transitions {
		publication.expected[transition.Identity.NoID] = transition.Cut.Context
	}
	var err error
	publication.publication, err = PublishValidatorEvidenceClosedCensusV2(t.Context(), fixture.last, publication.options(t))
	if err != nil {
		t.Fatal(err)
	}
	var ids []uint64
	for _, activation := range publication.readOptions.Activations {
		ids = append(ids, activation.NoID)
	}
	publication.manifest, err = WriteValidatorEvidencePublicationV2Manifest(t.Context(), fixture.options.Config.StateDir, publication.publication, ids, publication.readOptions.Bounds.MaxClosureBytes, publication.readOptions.Bounds.MaxParticipants)
	if err != nil {
		t.Fatal(err)
	}
	path, err := ValidatorEvidencePublicationV2ManifestPath(fixture.options.Config.StateDir, boundary.SettlementEpoch)
	if err != nil {
		t.Fatal(err)
	}
	archivePublicationV2TestAdd(t, &fixture, publication.publication, publication.manifest, path, false)
	return fixture, publication
}

func TestReleaseArchiveV2PublicationJoinsActualSignedTerminalAndBothReplicas(t *testing.T) {
	fixture, publication := archiveTerminalPublicationV2TestFixture(t)
	archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	}()
	observed, err := archive.terminalPublicationV2(t.Context(), publication.readOptions.Window)
	if err != nil || observed == nil {
		t.Fatalf("actual terminal archive join: %v", err)
	}
	if !sameReleaseArchivePublicationV2(observed.publication, publication.publication) {
		t.Fatal("archive replaced actual original terminal bytes")
	}
	if err := archive.AuthenticatePublicationsV2(t.Context(), nil, 8, 1, 2001, [32]byte{1}); err == nil {
		t.Fatal("signed public metadata substituted for an actual contract reader")
	}
}

func TestReleaseArchiveV2PublicationRejectsMissingReplicaAndResignedCount(t *testing.T) {
	for _, mutation := range []string{"missing-second-payload", "signed-wrong-count"} {
		t.Run(mutation, func(t *testing.T) {
			fixture, publication := archiveTerminalPublicationV2TestFixture(t)
			if mutation == "missing-second-payload" {
				member := publication.publication.Members[len(publication.publication.Members)-1]
				delete(fixture.files, ReleaseEvidenceV2CaptureSource{Kind: "terminal-payload", Name: attemptHex32(sha256.Sum256(member.Payload)), Origin: fixture.options.Origins[1]})
			} else {
				// The attacker owns genuine original keys. Valid fresh consents for
				// a changed unsigned census still cannot replace replayed counts.
				var census ValidatorEvidenceCensusV2
				if err := json.Unmarshal(publication.publication.Census, &census); err != nil {
					t.Fatal(err)
				}
				census.Members[0].RecordCount++
				raw, err := json.Marshal(census)
				if err != nil {
					t.Fatal(err)
				}
				publication.publication.Census = append(raw, '\n')
				publication.publication.CensusHash = sha256.Sum256(publication.publication.Census)
				publication.manifest.CensusHash, publication.manifest.CensusBytes = publication.publication.CensusHash, uint64(len(publication.publication.Census))
				options := publication.options(t)
				for index := range publication.publication.Members {
					member := &publication.publication.Members[index]
					member.Evidence.Header.CensusHash = publication.publication.CensusHash
					member.Evidence.VPKSignature, err = member.Evidence.Header.SignVPK(options.PrivateKeys[member.Evidence.Header.NoID])
					if err != nil {
						t.Fatal(err)
					}
					digest, err := member.Evidence.Header.Digest()
					if err != nil {
						t.Fatal(err)
					}
					member.Evidence.HotkeySignature, err = options.Hotkey.Sign(digest[:])
					if err != nil {
						t.Fatal(err)
					}
					member.SignedArtifact, err = marshalAttemptSettlementV2JSON(t.Context(), member.Evidence, publication.readOptions.Bounds.Cut.MaxHeaderBytes, false, true)
					if err != nil {
						t.Fatal(err)
					}
					member.SignedArtifactHash = sha256.Sum256(member.SignedArtifact)
					publication.manifest.Members[index].SignedArtifactHash = member.SignedArtifactHash
					publication.manifest.Members[index].SignedArtifactBytes = uint64(len(member.SignedArtifact))
				}
				path, _ := ValidatorEvidencePublicationV2ManifestPath(fixture.options.Config.StateDir, publication.manifest.Epoch)
				archivePublicationV2TestAdd(t, &fixture, publication.publication, publication.manifest, path, false)
			}
			fixture.repin()
			archive, err := openReleaseEvidenceV2ArchiveHistory(t.Context(), fixture.options)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := archive.Close(); err != nil {
					t.Error(err)
				}
			}()
			_, err = archive.terminalPublicationV2(t.Context(), publication.readOptions.Window)
			if err == nil {
				t.Fatal("altered publication passed complete original terminal replay")
			}
			if mutation == "signed-wrong-count" && !strings.Contains(err.Error(), "differs from complete terminal replay") {
				t.Fatalf("valid consents did not reach count join: %v", err)
			}
		})
	}
}

func TestReleaseArchiveV2AuditJoinsOriginalPayoutObservation(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	manifest, _ := fixture.retained(t)
	publication, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	cfg := fixture.base.runtime.cfg
	files := map[ReleaseEvidenceV2CaptureSource][]byte{}
	archiveFixture := releaseArchiveV2TestFixture{files: files, options: ReleaseEvidenceV2ArchiveOptions{Config: &cfg, Origins: fixture.options.Origins}}
	path, err := ValidatorEvidenceDepositAuditV2ManifestPath(cfg.StateDir, manifest.Epoch, manifest.Subject)
	if err != nil {
		t.Fatal(err)
	}
	archivePublicationV2TestAdd(t, &archiveFixture, publication, manifest, path, true)
	entries, err := os.ReadDir(filepath.Join(cfg.StateDir, "artifact-http-observations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(cfg.StateDir, "artifact-http-observations", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: "artifact-http-observations/" + entry.Name()}] = raw
	}
	archiveFixture.repin()
	owner := &releaseEvidenceV2ArchiveOwner{ctx: t.Context(), cfg: cfg, origins: fixture.options.Origins, root: newAttemptSettlementRuntimeV2TestStateDir(t), sources: map[ReleaseEvidenceV2CaptureSource]ReleaseEvidenceV2ArchiveSource{}}
	owner.anchor, err = os.Lstat(owner.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range archiveFixture.options.Sources {
		owner.sources[source.Source] = source
	}
	owner.readSource = func(_ context.Context, source ReleaseEvidenceV2CaptureSource) ([]byte, error) {
		return bytes.Clone(files[source]), nil
	}
	archive := &ReleaseEvidenceV2Archive{owner: owner}
	for _, input := range fixture.base.startup.inputs {
		archive.inputs = append(archive.inputs, input)
	}
	observed, err := archive.auditPublicationV2(t.Context(), fixture.artifact, fixture.options.Window)
	if err != nil || !sameReleaseArchivePublicationV2(observed.publication, publication) {
		t.Fatalf("actual signed audit and original payout join: %v", err)
	}
	body, err := archive.payoutSourceForMeasurementV2(t.Context(), fixture.artifact, fixture.artifact.DepositAudits[0].NoID)
	if err != nil || len(body) == 0 {
		t.Fatalf("original signed payout body: %v", err)
	}
	saved := bytes.Clone(body)
	body[0] ^= 1
	again, err := archive.payoutSourceForMeasurementV2(t.Context(), fixture.artifact, fixture.artifact.DepositAudits[0].NoID)
	if err != nil || !bytes.Equal(saved, again) {
		t.Fatal("returned payout aliased original closed source")
	}
	if _, err := archive.payoutSourceForMeasurementV2(t.Context(), fixture.artifact, 9999); err == nil {
		t.Fatal("absent payout subject escaped closed source authority")
	}
	changed := *fixture.artifact
	changed.DepositAudits = append([]DepositAudit(nil), fixture.artifact.DepositAudits...)
	changed.DepositAudits[0].RequiredDepositRao = "99999"
	if _, err := archive.auditPublicationV2(t.Context(), &changed, fixture.options.Window); err == nil {
		t.Fatal("signed audit replaced independent original deposit observations")
	}
}
