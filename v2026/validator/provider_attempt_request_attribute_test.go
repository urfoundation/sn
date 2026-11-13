//go:build linux || darwin

// The request checkpoint attribute is one fixed-width record on the shared
// ledger root inode. Its budget, codec, crash recovery and v1 reading are
// exercised against the actual runtime, offline verifier and restore.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// A complete synthetic original preparation for one deployment name.
func providerRequestTestPreparation(deployment string) ProviderAttemptRequestPreparation {
	public := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{5}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	ledger := AttemptLedgerIdentity{DeploymentID: deployment, ChainID: 964, GenesisHash: attemptHex32([32]byte{1}), Netuid: 25, ValidatorID: 1, ValidatorUID: 1, NoID: 1, ValidatorVPK: attemptHex32([32]byte(public))}
	return ProviderAttemptRequestPreparation{Identity: ProviderAttemptRequestIdentity{Ledger: ledger, Coordinator: "0x" + strings.Repeat("ab", 20), ClientId: connect.Id{7}, PolicyHash: [32]byte{9}},
		Limits: ProviderAttemptRequestLimits{MaxRecords: 8, MaxRecordBytes: 8192, MaxJournalBytes: 64 * 1024}, Birth: attemptLedgerTestBoundary()}
}

// Grow only the deployment name until the widest custody checkpoint has
// exactly custodyBytes.
func providerRequestBudgetPreparation(t *testing.T, custodyBytes int) ProviderAttemptRequestPreparation {
	t.Helper()
	preparation := providerRequestTestPreparation("d")
	custody, _, err := providerAttemptRequestWidestRootAttributes(preparation)
	if err != nil || len(custody) > custodyBytes {
		t.Fatal("budget fixture identity is already wider than requested", len(custody), err)
	}
	preparation.Identity.Ledger.DeploymentID += strings.Repeat("d", custodyBytes-len(custody))
	if custody, _, err = providerAttemptRequestWidestRootAttributes(preparation); err != nil || len(custody) != custodyBytes {
		t.Fatal("budget fixture missed its exact custody width", len(custody), err)
	}
	return preparation
}

// Every field at its widest, with the pending and closed sections present.
func providerRequestWidestTestCheckpoint(preparation ProviderAttemptRequestPreparation) ProviderAttemptRequestCheckpoint {
	hash := [32]byte(bytes.Repeat([]byte{255}, 32))
	head := ProviderAttemptRequestHead{Sequence: ^uint64(0), Hash: hash, Bytes: ^uint64(0), LastBoundary: AttemptBoundary{SettlementEpoch: ^uint64(0), EVMBlock: ^uint64(0), EVMBlockHash: attemptHex32(hash)}}
	window := protocol.ValidatorEvidenceWindow{Epoch: ^uint64(0), StartBlock: ^uint64(0), EndBlock: ^uint64(0), FinalizedBlock: ^uint64(0), Subject: protocol.ValidatorEvidenceSubject{ObservationEpoch: ^uint64(0), NativeEpoch: ^uint64(0)}}
	closed := &ProviderAttemptRequestClosedHead{Schema: ProviderAttemptRequestWindowSchema, Preparation: preparation, Window: window, PreviousCutHash: hash, Begin: head, End: head, RecordsHash: hash, Signature: bytes.Repeat([]byte{255}, ed25519.SignatureSize)}
	return ProviderAttemptRequestCheckpoint{Schema: ProviderAttemptRequestCheckpointSchema, Identity: preparation.Identity, Limits: preparation.Limits, Birth: preparation.Birth, DirectoryInode: ^uint64(0), FileInode: ^uint64(0),
		Committed: head, Pending: &ProviderAttemptRequestPending{Bytes: ^uint64(0), Hash: hash}, Closed: closed}
}

func providerRequestTestAttribute(t *testing.T, path string) []byte {
	t.Helper()
	raw := make([]byte, 4096)
	count, err := unix.Getxattr(path, ProviderAttemptRequestAttribute, raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw[:count]
}

// One more actual original from the journal's fixed client and signing key.
func providerRequestTestAppend(t *testing.T, journal *ProviderAttemptRequestJournal, key ed25519.PrivateKey, preparation ProviderAttemptRequestPreparation, nonce byte) (*ProviderAttemptRequestRecord, error) {
	t.Helper()
	body, message, signature := attemptRequestRestoreSeed(t, key, preparation.Identity.ClientId, nonce)
	return journal.Append(t.Context(), preparation.Birth, connect.Id{11}, body, message, signature)
}

func providerRequestTestWindow(preparation ProviderAttemptRequestPreparation) protocol.ValidatorEvidenceWindow {
	birth := preparation.Birth
	return protocol.ValidatorEvidenceWindow{Epoch: birth.SettlementEpoch, StartBlock: birth.EVMBlock, EndBlock: birth.EVMBlock + 1, FinalizedBlock: birth.EVMBlock + 1}
}

// The forecast is ext4's own block layout: a 32-byte header, a 4-byte
// terminator, 16-byte entries with names padded to four bytes, and values
// padded to four bytes. The entries for attempt-ledger-custody,
// storage-preparation, durable-root-generation and validator.requests.v1 take
// 48, 48, 52 and 48 bytes; the request record (599 -> 600), the generation
// (32) and the widest preparation anchor (387 -> 388) leave the custody
// checkpoint exactly 4096 - 36 - 196 - 600 - 32 - 388 = 2,844 bytes.
func TestProviderRequestPreparationBudgetIsOneExt4AttributeBlock(t *testing.T) {
	_, anchor, err := providerAttemptRequestWidestRootAttributes(providerRequestTestPreparation("d"))
	if err != nil || len(anchor) != 387 || providerAttemptRequestAttributeBytes != 599 || durablevolume.RootGenerationBytes != 32 {
		t.Fatal("forecast inputs changed", len(anchor), providerAttemptRequestAttributeBytes, err)
	}
	if err := providerRequestBudgetPreparation(t, 2844).Validate(); err != nil {
		t.Fatal("custody checkpoint filling the block exactly was refused", err)
	}
	if err := providerRequestBudgetPreparation(t, 2845).Validate(); !errors.Is(err, protocol.ErrProviderAttemptsCapacity) {
		t.Fatal("custody checkpoint one byte over the block was admitted", err)
	}
}

// Every reachable checkpoint state, and the widest one, is written as the
// same 599-byte record and decodes to itself. The v1 JSON of the same state
// still decodes, reported as v1.
func TestProviderRequestCheckpointEncodesEveryStateAtFixedWidth(t *testing.T) {
	engine, journal, preparation, directory := providerRequestTestOwner(t, 32)
	var states []ProviderAttemptRequestCheckpoint
	observe := func(state string) {
		t.Helper()
		raw := providerRequestTestAttribute(t, directory)
		value, legacy, err := decodeProviderAttemptRequestCheckpoint(raw, preparation)
		if err != nil || legacy || len(raw) != providerAttemptRequestAttributeBytes || !reflect.DeepEqual(value, *journal.checkpoint) {
			t.Fatal("runtime checkpoint is not the fixed-width record of its state", state, len(raw), err)
		}
		states = append(states, value)
	}
	observe("fresh")
	if _, err := providerRequestTestAppend(t, journal, engine.vsk, preparation, 1); err != nil {
		t.Fatal(err)
	}
	observe("committed")
	interrupted := errors.New("synthetic stop after the journal line")
	journal.step = func(stage string) error {
		if stage == "after-request-file-sync" {
			return interrupted
		}
		return nil
	}
	if _, err := providerRequestTestAppend(t, journal, engine.vsk, preparation, 2); !errors.Is(err, interrupted) {
		t.Fatal(err)
	}
	observe("pending")
	journal.step = nil
	if _, err := journal.SealWindow(t.Context(), providerRequestTestWindow(preparation), 1024*1024); err != nil {
		t.Fatal(err)
	}
	observe("closed")
	if states[2].Pending == nil || states[3].Closed == nil {
		t.Fatal("fixture did not reach pending and closed states")
	}
	states = append(states, providerRequestWidestTestCheckpoint(preparation))
	for index, state := range states {
		raw, err := encodeProviderAttemptRequestCheckpoint(state)
		if err != nil || len(raw) != providerAttemptRequestAttributeBytes {
			t.Fatal("checkpoint state has another width", index, len(raw), err)
		}
		decoded, legacy, err := decodeProviderAttemptRequestCheckpoint(raw, preparation)
		if err != nil || legacy || !reflect.DeepEqual(decoded, state) {
			t.Fatal("fixed-width record does not decode to its state", index, err)
		}
		original, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		decoded, legacy, err = decodeProviderAttemptRequestCheckpoint(original, preparation)
		if err != nil || !legacy || !reflect.DeepEqual(decoded, state) {
			t.Fatal("v1 checkpoint is no longer read", index, err)
		}
	}
}

// A value the fixed-width record cannot carry exactly is refused, never
// normalized: another schema, a closed head with another schema, preparation
// or signature width, or a head block hash that is not canonical.
func TestProviderRequestCheckpointRefusesUnrepresentableValues(t *testing.T) {
	preparation := providerRequestTestPreparation("d")
	for name, change := range map[string]func(*ProviderAttemptRequestCheckpoint){
		"schema":             func(value *ProviderAttemptRequestCheckpoint) { value.Schema += "x" },
		"closed schema":      func(value *ProviderAttemptRequestCheckpoint) { value.Closed.Schema += "x" },
		"closed preparation": func(value *ProviderAttemptRequestCheckpoint) { value.Closed.Preparation.Birth.EVMBlock++ },
		"closed signature":   func(value *ProviderAttemptRequestCheckpoint) { value.Closed.Signature = value.Closed.Signature[1:] },
		"committed block hash": func(value *ProviderAttemptRequestCheckpoint) {
			value.Committed.LastBoundary.EVMBlockHash = strings.ToUpper(value.Committed.LastBoundary.EVMBlockHash)
		},
		"closed zero block hash": func(value *ProviderAttemptRequestCheckpoint) {
			value.Closed.Begin.LastBoundary.EVMBlockHash = attemptHex32([32]byte{})
		},
	} {
		value := providerRequestWidestTestCheckpoint(preparation)
		change(&value)
		if raw, err := encodeProviderAttemptRequestCheckpoint(value); !errors.Is(err, protocol.ErrProviderAttemptsIntegrity) || raw != nil {
			t.Fatal("unrepresentable checkpoint was encoded", name, err)
		}
	}
}

// The digest refuses another original preparation; width, flags and absent
// sections must be canonical; a tampered head, signature or inode fails the
// unchanged runtime admission; and rewriting an admitted v2 value as v1 is a
// custody change.
func TestProviderRequestCheckpointRefusesForeignOrTamperedAttribute(t *testing.T) {
	engine, journal, preparation, directory := providerRequestTestOwner(t, 32)
	if _, err := providerRequestTestAppend(t, journal, engine.vsk, preparation, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.SealWindow(t.Context(), providerRequestTestWindow(preparation), 1024*1024); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	raw := providerRequestTestAttribute(t, directory)
	directoryOffset := len(providerAttemptRequestAttributeSchema) + 32
	committedHashOffset := directoryOffset + 16 + 8
	pendingOffset := directoryOffset + 16 + providerAttemptRequestHeadBytes
	closedOffset := pendingOffset + 1 + 8 + 32
	if raw[pendingOffset] != 0 || raw[closedOffset] != 1 {
		t.Fatal("fixture offsets do not name the pending and closed flags")
	}
	foreign := preparation
	foreign.Identity.ClientId = connect.Id{99}
	changed := func(change func([]byte) []byte) []byte { return change(bytes.Clone(raw)) }
	for name, value := range map[string][]byte{
		"truncated":              raw[:len(raw)-1],
		"extended":               append(bytes.Clone(raw), 0),
		"pending flag":           changed(func(value []byte) []byte { value[pendingOffset] = 2; return value }),
		"absent pending section": changed(func(value []byte) []byte { value[pendingOffset+1] = 1; return value }),
		"closed flag":            changed(func(value []byte) []byte { value[closedOffset] = 2; return value }),
		"absent closed section":  changed(func(value []byte) []byte { value[closedOffset] = 0; return value }),
	} {
		if _, _, err := decodeProviderAttemptRequestCheckpoint(value, preparation); !errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatal("noncanonical request checkpoint decoded", name, err)
		}
	}
	if _, _, err := decodeProviderAttemptRequestCheckpoint(raw, foreign); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("foreign preparation decoded the original checkpoint", err)
	}
	if owner, err := OpenProviderAttemptRequestJournal(context.Background(), directory, foreign, engine.vsk); owner != nil || !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("foreign original preparation opened the request owner", err)
	}
	for name, test := range map[string]struct {
		value []byte
		want  error
	}{
		"committed hash":  {changed(func(value []byte) []byte { value[committedHashOffset] ^= 1; return value }), durablevolume.ErrIdentity},
		"close signature": {changed(func(value []byte) []byte { value[len(value)-1] ^= 1; return value }), protocol.ErrProviderAttemptsIntegrity},
		"directory inode": {changed(func(value []byte) []byte { value[directoryOffset+7] ^= 1; return value }), durablevolume.ErrIdentity},
	} {
		if err := unix.Setxattr(directory, ProviderAttemptRequestAttribute, test.value, unix.XATTR_REPLACE); err != nil {
			t.Fatal(err)
		}
		if owner, err := OpenProviderAttemptRequestJournal(context.Background(), directory, preparation, engine.vsk); owner != nil || !errors.Is(err, test.want) {
			t.Fatal("tampered request checkpoint opened", name, err)
		}
	}
	if err := unix.Setxattr(directory, ProviderAttemptRequestAttribute, raw, unix.XATTR_REPLACE); err != nil {
		t.Fatal(err)
	}
	owner, err := OpenProviderAttemptRequestJournal(context.Background(), directory, preparation, engine.vsk)
	if err != nil {
		t.Fatal("restored original checkpoint no longer opens", err)
	}
	defer owner.Close()
	setProviderRequestTestLegacyAttribute(t, directory, *owner.checkpoint)
	if _, _, err := owner.Head(t.Context()); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("v1 rewrite of admitted custody went unnoticed", err)
	}
}

// The checkpoint remains the one commit point. A crash after any write of an
// append, a seal or a v1 migration, with that write kept or lost, reopens to
// the exact original request or signed close and continues. A lost journal
// line is the committed prefix alone, as a crash before its fsync leaves it.
func TestProviderRequestCheckpointCrashAtEveryWriteRecovers(t *testing.T) {
	for _, crash := range []struct {
		name       string
		legacy     bool
		seal       bool
		stage      string
		occurrence int
		lost       bool
	}{
		{name: "pending checkpoint kept", stage: "provider-request-checkpoint"},
		{name: "pending checkpoint lost", stage: "provider-request-checkpoint", lost: true},
		{name: "journal line kept", stage: "after-request-file-sync"},
		{name: "journal line lost", stage: "after-request-file-sync", lost: true},
		{name: "committed checkpoint kept", stage: "provider-request-checkpoint", occurrence: 1},
		{name: "committed checkpoint lost", stage: "provider-request-checkpoint", occurrence: 1, lost: true},
		{name: "pending removal kept", stage: "provider-request-pending-remove"},
		{name: "pending removal lost", stage: "provider-request-pending-remove", lost: true},
		{name: "closed checkpoint kept", seal: true, stage: "provider-request-checkpoint"},
		{name: "closed checkpoint lost", seal: true, stage: "provider-request-checkpoint", lost: true},
		{name: "v1 migration kept", legacy: true, stage: "provider-request-checkpoint"},
		{name: "v1 migration lost", legacy: true, stage: "provider-request-checkpoint", lost: true},
	} {
		t.Run(crash.name, func(t *testing.T) {
			engine, journal, preparation, directory := providerRequestTestOwner(t, 32)
			pendingPath := filepath.Join(directory, ProviderAttemptRequestPendingName)
			journalPath := filepath.Join(directory, ProviderAttemptRequestJournalName)
			reopen := func() *ProviderAttemptRequestJournal {
				t.Helper()
				owner, err := OpenProviderAttemptRequestJournal(context.Background(), directory, preparation, engine.vsk)
				if err != nil {
					t.Fatal("request owner did not reopen", err)
				}
				t.Cleanup(func() { _ = owner.Close() })
				return owner
			}
			if _, err := providerRequestTestAppend(t, journal, engine.vsk, preparation, 1); err != nil {
				t.Fatal(err)
			}
			if crash.legacy {
				if err := journal.Close(); err != nil {
					t.Fatal(err)
				}
				setProviderRequestTestLegacyAttribute(t, directory, *journal.checkpoint)
				if journal = reopen(); !journal.legacy {
					t.Fatal("v1 attribute was not admitted as v1")
				}
			}
			before := providerRequestTestAttribute(t, directory)
			prefix, err := os.Stat(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			crashed := errors.New("synthetic crash before the write barrier")
			var pending, written []byte
			count := 0
			stop := func(stage string) error {
				if stage != crash.stage {
					return nil
				}
				if count++; count-1 != crash.occurrence {
					return nil
				}
				written = providerRequestTestAttribute(t, directory)
				return crashed
			}
			journal.directory.step = func(operation, stage string) error {
				if operation != "directory-sync" {
					return nil
				}
				if stage == "provider-request-checkpoint" && pending == nil {
					pending, _ = os.ReadFile(pendingPath)
				}
				return stop(stage)
			}
			journal.step = stop
			if crash.seal {
				_, err = journal.SealWindow(t.Context(), providerRequestTestWindow(preparation), 1024*1024)
			} else {
				_, err = providerRequestTestAppend(t, journal, engine.vsk, preparation, 2)
			}
			if !errors.Is(err, crashed) {
				t.Fatal("crash point was not reached", err)
			}
			err = journal.Close()
			switch {
			case err != nil || !crash.lost:
			case crash.stage == "provider-request-pending-remove":
				err = os.WriteFile(pendingPath, pending, 0o600)
			case crash.stage == "after-request-file-sync":
				err = os.Truncate(journalPath, prefix.Size())
			default:
				err = unix.Setxattr(directory, ProviderAttemptRequestAttribute, before, unix.XATTR_REPLACE)
			}
			if err != nil {
				t.Fatal(err)
			}
			recovered := reopen()
			if crash.seal {
				interrupted, err := DecodeProviderAttemptRequestCheckpoint(written, preparation)
				if err != nil || interrupted.Closed == nil {
					t.Fatal("crash did not interrupt the closed checkpoint", err)
				}
				cut, err := recovered.SealWindow(t.Context(), providerRequestTestWindow(preparation), 1024*1024)
				if err != nil || cut == nil || !reflect.DeepEqual(cut.Header, *interrupted.Closed) {
					t.Fatal("recovered close is not the interrupted signed header", err)
				}
			} else {
				head, _, err := recovered.Head(t.Context())
				if err != nil || head.Sequence != 2 {
					t.Fatal("recovery lost or duplicated the interrupted original", head, err)
				}
				var second []byte
				if err := recovered.Walk(t.Context(), func(record ProviderAttemptRequestRecord) error {
					if record.Sequence != 2 {
						return nil
					}
					var err error
					second, err = json.Marshal(record)
					return err
				}); err != nil || len(pending) == 0 || !bytes.Equal(second, pending) {
					t.Fatal("recovery changed the interrupted original bytes", err)
				}
				if _, err := os.Lstat(pendingPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("recovery kept the pending original", err)
				}
				if record, err := providerRequestTestAppend(t, recovered, engine.vsk, preparation, 3); err != nil || record.Sequence != 3 {
					t.Fatal("recovered owner did not continue", err)
				}
			}
			if raw := providerRequestTestAttribute(t, directory); !bytes.HasPrefix(raw, []byte(providerAttemptRequestAttributeSchema)) || recovered.legacy {
				t.Fatal("recovered checkpoint is not the fixed-width record")
			}
		})
	}
}

// The runtime admits today's v1 attribute without rewriting it, compares it
// as v1 while open, and its next save replaces it with v2 in one write.
func TestProviderRequestLegacyCheckpointIsAcceptedAndMigrated(t *testing.T) {
	engine, journal, preparation, directory := providerRequestTestOwner(t, 32)
	if _, err := providerRequestTestAppend(t, journal, engine.vsk, preparation, 1); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	legacy := setProviderRequestTestLegacyAttribute(t, directory, *journal.checkpoint)
	owner, err := OpenProviderAttemptRequestJournal(context.Background(), directory, preparation, engine.vsk)
	if err != nil || !owner.legacy {
		t.Fatal("v1 request checkpoint was not admitted", err)
	}
	defer owner.Close()
	head, birth, err := owner.Head(t.Context())
	if err != nil || head.Sequence != 1 || birth != preparation.Birth || !bytes.Equal(providerRequestTestAttribute(t, directory), legacy) {
		t.Fatal("v1 admission changed custody or its head", head, err)
	}
	if _, err := providerRequestTestAppend(t, owner, engine.vsk, preparation, 2); err != nil {
		t.Fatal(err)
	}
	raw := providerRequestTestAttribute(t, directory)
	migrated, legacyAgain, err := decodeProviderAttemptRequestCheckpoint(raw, preparation)
	if err != nil || legacyAgain || owner.legacy || len(raw) != providerAttemptRequestAttributeBytes || !reflect.DeepEqual(migrated, *owner.checkpoint) || migrated.Committed.Sequence != 2 {
		t.Fatal("next save did not migrate the checkpoint to v2", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenProviderAttemptRequestJournal(context.Background(), directory, preparation, engine.vsk)
	if err != nil || reopened.legacy {
		t.Fatal("migrated request checkpoint did not reopen as v2", err)
	}
	defer reopened.Close()
	if head, _, err := reopened.Head(t.Context()); err != nil || head != migrated.Committed {
		t.Fatal("migration changed the committed head", head, err)
	}
}

// v1 spells out the preparation that v2 binds by digest. The exported decoder
// refuses a v1 checkpoint under another identity, limits or birth, or one
// whose signed close names another preparation, as it refuses a v2 digest.
func TestProviderRequestLegacyCheckpointRefusesAnotherPreparation(t *testing.T) {
	engine, journal, preparation, _ := providerRequestTestOwner(t, 32)
	if _, err := providerRequestTestAppend(t, journal, engine.vsk, preparation, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.SealWindow(t.Context(), providerRequestTestWindow(preparation), 1024*1024); err != nil {
		t.Fatal(err)
	}
	checkpoint := *journal.checkpoint
	legacy, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := DecodeProviderAttemptRequestCheckpoint(legacy, preparation); err != nil || !reflect.DeepEqual(decoded, checkpoint) {
		t.Fatal("v1 checkpoint of its own preparation was refused", err)
	}
	for name, change := range map[string]func(*ProviderAttemptRequestPreparation){
		"identity": func(value *ProviderAttemptRequestPreparation) { value.Identity.ClientId = connect.Id{99} },
		"limits":   func(value *ProviderAttemptRequestPreparation) { value.Limits.MaxRecords++ },
		"birth":    func(value *ProviderAttemptRequestPreparation) { value.Birth.EVMBlock-- },
	} {
		other := preparation
		change(&other)
		if value, err := DecodeProviderAttemptRequestCheckpoint(legacy, other); !errors.Is(err, durablevolume.ErrIdentity) || value.Schema != "" {
			t.Fatal("v1 checkpoint decoded under another preparation", name, err)
		}
	}
	closed := copyClosedProviderRequestTestHead(*checkpoint.Closed)
	closed.Preparation.Birth.EVMBlock--
	checkpoint.Closed = &closed
	foreign, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := DecodeProviderAttemptRequestCheckpoint(foreign, preparation); !errors.Is(err, durablevolume.ErrIdentity) || value.Schema != "" {
		t.Fatal("v1 checkpoint decoded with a close of another preparation", err)
	}
}

// Offline inspection accepts a v1 anchor that holds exactly the verified
// checkpoint and returns it unchanged, as it returns a v2 anchor.
func TestAttemptLedgerRequestPreparationKeepsEquivalentLegacyAnchor(t *testing.T) {
	f, original, _ := newAttemptRequestRestoreFixture(t, false, false)
	path := f.root + ".original-held"
	for _, legacy := range []bool{false, true} {
		want := providerRequestTestAttribute(t, path)
		if legacy {
			want = setProviderRequestTestLegacyAttribute(t, path, original)
		}
		root, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		census, err := InspectAttemptLedgerPreparation(t.Context(), root, f.scope)
		var raw []byte
		if err == nil {
			raw, err = BuildAttemptLedgerPreparationRequestCheckpoint(t.Context(), root, f.scope, census)
		}
		if err := errors.Join(err, root.Close()); err != nil || !bytes.Equal(raw, want) {
			t.Fatal("offline verifier did not keep the retained request anchor", legacy, err)
		}
	}
}

// Re-inspecting a restored target accepts its retained anchor in either
// format when it holds exactly the rebound checkpoint, and refuses any other.
func TestAttemptLedgerRequestRestoreReinspectionKeepsEquivalentLegacyAnchor(t *testing.T) {
	f, _, _ := newAttemptRequestRestoreFixture(t, false, false)
	f.apply(t)
	inputs, err := json.Marshal(f.scope)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanAttemptLedgerRestore(t.Context(), "exact-stage", durablevolume.PreparationOwner{Kind: AttemptLedgerPreparationKind, RelativePath: ".", Purpose: "restore", Inputs: inputs}, f.report)
	if err != nil {
		t.Fatal(err)
	}
	inspect := func() ([]byte, error) {
		root, err := os.Open(f.root)
		if err != nil {
			t.Fatal(err)
		}
		attributes, err := InspectAttemptLedgerRestore(t.Context(), root, plan, f.report)
		if err := errors.Join(err, root.Close()); err != nil {
			return nil, err
		}
		return attributes[1].Raw, nil
	}
	restored := providerRequestTestAttribute(t, f.root)
	rebound, err := DecodeProviderAttemptRequestCheckpoint(restored, f.scope.Requests.Preparation)
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := inspect(); err != nil || !bytes.Equal(raw, restored) {
		t.Fatal("restored v2 anchor was not kept", err)
	}
	legacy := setProviderRequestTestLegacyAttribute(t, f.root, rebound)
	if raw, err := inspect(); err != nil || !bytes.Equal(raw, legacy) {
		t.Fatal("equivalent v1 anchor was not kept", err)
	}
	other := rebound
	other.FileInode++
	foreign, err := encodeProviderAttemptRequestCheckpoint(other)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string][]byte{"other checkpoint": foreign, "malformed": []byte("x")} {
		if err := unix.Setxattr(f.root, ProviderAttemptRequestAttribute, value, unix.XATTR_REPLACE); err != nil {
			t.Fatal(err)
		}
		if raw, err := inspect(); !errors.Is(err, durablevolume.ErrIdentity) || raw != nil {
			t.Fatal("restored target kept a different request anchor", name, err)
		}
	}
}

// The anchor storage-prepare actually writes decodes strictly into the
// forecast's mirror and is no wider than its widest value.
func TestProviderRequestBudgetMirrorsActualStoragePreparationAnchor(t *testing.T) {
	f, _, _ := newAttemptRequestRestoreFixture(t, false, false)
	f.apply(t)
	raw := make([]byte, 4096)
	count, err := unix.Getxattr(f.root, durablevolume.PreparationAttribute, raw)
	if err != nil {
		t.Fatal(err)
	}
	var anchor providerAttemptRequestPreparationAnchor
	if err := attemptStoreDecode(raw[:count], &anchor); err != nil || anchor.Schema != providerAttemptRequestPreparationAnchorSchema {
		t.Fatal("storage-preparation anchor differs from the forecast mirror", err)
	}
	if _, widest, err := providerAttemptRequestWidestRootAttributes(f.scope.Requests.Preparation); err != nil || count > len(widest) {
		t.Fatal("storage-preparation anchor is wider than the forecast", count, err)
	}
}

// durablevolume lets a fresh native journal, SDK work outbox or successor-
// members owner share the ledger root and attach an attribute to it. A request
// owner refuses such a root at runtime and in preparation. A snapshot owner's
// attribute on its own lock file, and a ledger root without requests, are
// admitted as they are today.
func TestProviderRequestRootRefusesAnotherOwnerAttribute(t *testing.T) {
	foreign := []string{"user.urnetwork.native-journal-custody", connect.OriginalWorkOutboxAttribute, "user.urnetwork.snapshot." + strings.Repeat("ab", 32)}
	engine, journal, preparation, directory := providerRequestTestOwner(t, 32)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range foreign {
		if err := unix.Setxattr(directory, name, []byte{1}, unix.XATTR_CREATE); err != nil {
			t.Fatal(err)
		}
		if owner, err := OpenProviderAttemptRequestJournal(context.Background(), directory, preparation, engine.vsk); owner != nil || !errors.Is(err, protocol.ErrProviderAttemptsCapacity) {
			t.Fatal("request owner opened beside another owner's root attribute", name, err)
		}
		if err := unix.Removexattr(directory, name); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := OpenProviderAttemptRequestJournal(context.Background(), directory, preparation, engine.vsk)
	if err != nil {
		t.Fatal("request owner refused its own root attributes", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	inspect := func(f *attemptLedgerRestoreFixture, path, name string) error {
		t.Helper()
		if err := unix.Setxattr(path, name, []byte{1}, unix.XATTR_CREATE); err != nil {
			t.Fatal(err)
		}
		root, err := os.Open(f.root + ".original-held")
		if err != nil {
			t.Fatal(err)
		}
		_, err = InspectAttemptLedgerPreparation(t.Context(), root, f.scope)
		return errors.Join(err, root.Close())
	}
	requests, _, _ := newAttemptRequestRestoreFixture(t, false, false)
	held := requests.root + ".original-held"
	lock := filepath.Join(held, "head.json.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := inspect(requests, lock, foreign[2]); err != nil {
		t.Fatal("request preparation refused a snapshot attribute on its own lock file", err)
	}
	if err := inspect(requests, held, foreign[0]); !errors.Is(err, protocol.ErrProviderAttemptsCapacity) {
		t.Fatal("request preparation admitted another owner's root attribute", err)
	}
	plain := newAttemptLedgerRestoreFixture(t, newAttemptRecordStoreTestFixture(t, 1), false, "")
	if err := inspect(plain, plain.root+".original-held", foreign[0]); err != nil {
		t.Fatal("ledger preparation without requests refused a shared root", err)
	}
}
