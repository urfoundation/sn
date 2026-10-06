// Submission approval consumes the original signed operator intent once; it
// cannot refresh mortality, substitute a quote for a cap or borrow another domain.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"testing"
	"time"
)

// Construct a valid public signed record without manufacturing a chain receipt.
func rootRegisterTestSignedRecord(t *testing.T, f *rootRegisterTestFixture) rootRegisterRecord {
	t.Helper()
	request, err := newRootRegisterSigningRequest(f.config, f.key, f.metadataHex, f.ledgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	signature := f.signature(t)
	raw, err := f.config.Action.signed(signature)
	if err != nil {
		t.Fatal(err)
	}
	record := rootRegisterRecord{Schema: rootRegisterRecordSchema, Config: f.config, ApprovalKey: f.key, Phase: "signed", Request: &request, Signature: hex.EncodeToString(signature), RawExtrinsic: "0x" + hex.EncodeToString(raw), ExtrinsicHash: rootExtrinsicHash(raw)}
	record.ContentHash = rootObjectHash(record)
	if err := record.validate(f.config, f.key); err != nil {
		t.Fatal(err)
	}
	return record
}

// Source review and native bytes do not supply the separate approval signature.
func TestRootRegisterSubmissionRequiresIndependentOriginalExposureConsent(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	record := rootRegisterTestSignedRecord(t, f)
	approval, err := rootRegisterSubmissionTemplate(record)
	if err != nil {
		t.Fatal(err)
	}
	approval.AuthorityHash = rootObjectHash("synthetic independent production authority")
	if approval.validate(record, f.key) == nil {
		t.Fatal("unsigned submission template became authority")
	}
	approval.Signature = hex.EncodeToString(ed25519.Sign(f.approval, approval.signingBytes()))
	if err := approval.validate(record, f.key); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*rootRegisterSubmissionApproval){
		func(a *rootRegisterSubmissionApproval) { a.Schema = ownerRecycleSubmissionSchema },
		func(a *rootRegisterSubmissionApproval) {
			a.ValidThroughBlock = record.Config.Action.BirthBlock + record.Config.Action.Period - 1
		},
		func(a *rootRegisterSubmissionApproval) { a.MaximumAttempts = 0 },
		func(a *rootRegisterSubmissionApproval) { a.MaximumAttempts = 9 },
		func(a *rootRegisterSubmissionApproval) { a.ResidualRisks = nil },
		func(a *rootRegisterSubmissionApproval) { a.ExtrinsicHash = record.Config.Action.Policy.Reserve },
		func(a *rootRegisterSubmissionApproval) { a.AuthorityHash = "" },
	} {
		changed := approval
		mutate(&changed)
		changed.Signature = hex.EncodeToString(ed25519.Sign(f.approval, changed.signingBytes()))
		if changed.validate(record, f.key) == nil {
			t.Fatal("re-signed unrelated scope replenished original root registration authority")
		}
	}
}

// A returned public signature is necessary even when the era later expires.
// Existing submission authority can never be replaced by a freshly planned cap.
func TestRootRegisterSubmissionTemplateDoesNotResetUnknownOrConsumedIntent(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	record := rootRegisterTestSignedRecord(t, f)
	exported := record
	exported.Phase = "exported"
	exported.Signature = ""
	exported.RawExtrinsic = ""
	exported.ExtrinsicHash = ""
	if _, err := rootRegisterSubmissionTemplate(exported); err == nil {
		t.Fatal("unreturned request became safely unsigned work")
	}
	approval, err := rootRegisterSubmissionTemplate(record)
	if err != nil {
		t.Fatal(err)
	}
	record.Submission = &rootRegisterSubmissionRecord{Approval: approval, ApprovalKey: f.key, Attempts: 1}
	if _, err := rootRegisterSubmissionTemplate(record); err == nil {
		t.Fatal("original post allowance was replenished")
	}
	record.Submission = nil
	record.Reconciliation = &rootRegisterReconciliation{FinalizedNumber: record.Config.Action.BirthBlock + record.Config.Action.Period - 1}
	if _, err := rootRegisterSubmissionTemplate(record); err == nil {
		t.Fatal("expired original era was silently extended")
	}
}

// A local owned HTTP route exercises real absence reconciliation, exactly one
// original post, restart, inclusion and readback without an effect-shaped mock.
func TestRootRegisterOwnedSubmissionRetainsOnePostAndReconcilesOriginal(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	chain, fixture, _, _ := rootRegisterTestChain(t, f, 1, false)
	f.input.Route.RpcUrl = chain.client.url
	var err error
	f.config, err = prepareRootRegisterPlan(f.input)
	if err != nil {
		t.Fatal(err)
	}
	f.approve()
	chain.config = f.config
	custody, store, _ := rootRegisterSignedTestCustody(t, f)
	record, err := custody.load()
	if err != nil {
		t.Fatal(err)
	}
	approval, err := rootRegisterSubmissionTemplate(record)
	if err != nil {
		t.Fatal(err)
	}
	approval.AuthorityHash = rootObjectHash("synthetic independent registration send authority")
	approval.Signature = hex.EncodeToString(ed25519.Sign(f.approval, approval.signingBytes()))
	baseFault := fixture.fault
	fixture.fault = func(method string, params []json.RawMessage, n int) (any, bool) {
		if method == "author_submitExtrinsic" {
			var original string
			if len(params) != 1 || json.Unmarshal(params[0], &original) != nil || original != record.RawExtrinsic {
				t.Error("post replaced original public extrinsic")
				return nil, true
			}
			return record.ExtrinsicHash, true
		}
		return baseFault(method, params, n)
	}
	result, err := custody.submit(f.storage.Context, chain, approval, f.key)
	if err != nil || result.Phase != "signed" || result.RootSeatObserved || result.ActivationReady {
		t.Fatalf("first original post: %+v %v", result, err)
	}
	retained, err := custody.load()
	if err != nil || retained.Submission == nil || retained.Submission.Attempts != 1 {
		t.Fatal("post was not durably reserved", err)
	}
	if _, err := custody.submit(f.storage.Context, chain, approval, f.key); err == nil {
		t.Fatal("exhausted original approval posted twice")
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openRootRegisterStore(f.config, f.key, false, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	custody = &rootRegisterCustody{config: f.config, key: f.key, store: reopened}
	raw, err := hex.DecodeString(record.RawExtrinsic[2:])
	if err != nil {
		t.Fatal(err)
	}
	body := [][]byte{{8, 4, 0}, raw}
	previous := fixture.finalized
	number := f.config.Action.BirthBlock + 2
	header, hash := rootReceiptHeaderFixture(t, previous, number, body, false)
	observation := rootRegisterTestInclusionObservation(t, f, true, hash, number)
	eventKey, err := types.CreateStorageKey(f.metadata, "System", "Events")
	if err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	fixture.headers[hash], fixture.byHeight[number], fixture.finalized = header, hash, hash
	fixture.bodies[hash] = []string{"0x080400", record.RawExtrinsic}
	for _, row := range observation.Storage {
		if row.RawStorage != nil {
			fixture.storageKVs[row.Key] = *row.RawStorage
		} else {
			delete(fixture.storageKVs, row.Key)
		}
	}
	fixture.storageKVs[eventKey.Hex()] = "0x" + hex.EncodeToString(rootRegisterTestEvents(t, f, true))
	fixture.stateLock.Unlock()
	result, err = custody.reconcile(f.storage.Context, chain)
	if err != nil || result.Phase != "finalized" || !result.RootSeatObserved || result.Seat == nil || result.Seat.RegistrationBlock != number || result.ActivationReady {
		t.Fatalf("original pending post did not reconcile through restart: %+v %v", result, err)
	}
	if _, err := custody.submit(f.storage.Context, chain, approval, f.key); err != nil {
		t.Fatal("terminal reconciliation unexpectedly failed", err)
	}
	fixture.stateLock.Lock()
	posts := fixture.counts["author_submitExtrinsic"]
	fixture.stateLock.Unlock()
	if posts != 1 {
		t.Fatalf("original registration posted %d times", posts)
	}
}

// A newer quoted burn is checked only against the current original finalized
// state; preserving the birth quote cannot authorize a changed admission.
func TestRootRegisterSubmissionRechecksBurnBeforeReservingPost(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	chain, fixture, _, _ := rootRegisterTestChain(t, f, 1, false)
	f.input.Route.RpcUrl = chain.client.url
	var err error
	f.config, err = prepareRootRegisterPlan(f.input)
	if err != nil {
		t.Fatal(err)
	}
	f.approve()
	chain.config = f.config
	custody, _, _ := rootRegisterSignedTestCustody(t, f)
	record, err := custody.load()
	if err != nil {
		t.Fatal(err)
	}
	approval, err := rootRegisterSubmissionTemplate(record)
	if err != nil {
		t.Fatal(err)
	}
	approval.AuthorityHash = rootObjectHash("synthetic changed-quote production consent")
	approval.Signature = hex.EncodeToString(ed25519.Sign(f.approval, approval.signingBytes()))
	burnKey, err := types.CreateStorageKey(f.metadata, "SubtensorModule", "Burn", []byte{0, 0})
	if err != nil {
		t.Fatal(err)
	}
	baseFault := fixture.fault
	fixture.fault = func(method string, params []json.RawMessage, n int) (any, bool) {
		if method == "state_getStorage" && len(params) == 2 {
			var key, at string
			if json.Unmarshal(params[0], &key) == nil && json.Unmarshal(params[1], &at) == nil && key == burnKey.Hex() && at == fixture.finalized {
				return "0x" + hex.EncodeToString(binary.LittleEndian.AppendUint64(nil, f.config.Action.QuotedBurnLimitRao+1)), true
			}
		}
		return baseFault(method, params, n)
	}
	if _, err := custody.submit(f.storage.Context, chain, approval, f.key); err == nil {
		t.Fatal("changed burn quote bypassed preflight")
	}
	retained, err := custody.load()
	if err != nil || retained.Submission == nil || retained.Submission.Attempts != 0 {
		t.Fatal("failed preflight consumed or erased attempt custody", err)
	}
	fixture.stateLock.Lock()
	posts := fixture.counts["author_submitExtrinsic"]
	fixture.stateLock.Unlock()
	if posts != 0 {
		t.Fatal("changed quote reached native submission")
	}
}

// Closing-head lag retries only that read. Unavailable closure consumes no post;
// a healthy forward advance preserves the original quote/window and sends once.
func TestRootRegisterPrePostFinalityDoesNotWasteOriginalAllowance(t *testing.T) {
	for _, recoverHead := range []bool{false, true} {
		f := newRootRegisterTestFixture(t, false)
		chain, fixture, _, _ := rootRegisterTestChain(t, f, 1, false)
		f.input.Route.RpcUrl = chain.client.url
		var err error
		f.config, err = prepareRootRegisterPlan(f.input)
		if err != nil {
			t.Fatal(err)
		}
		f.approve()
		chain.config = f.config
		custody, _, _ := rootRegisterSignedTestCustody(t, f)
		record, err := custody.load()
		if err != nil {
			t.Fatal(err)
		}
		approval, err := rootRegisterSubmissionTemplate(record)
		if err != nil {
			t.Fatal(err)
		}
		approval.AuthorityHash = rootObjectHash("synthetic original finality-bounded submission")
		approval.Signature = hex.EncodeToString(ed25519.Sign(f.approval, approval.signingBytes()))
		operator, _ := hex.DecodeString(f.config.Action.Policy.Operator[2:])
		accountKey, err := types.CreateStorageKey(f.metadata, "System", "Account", operator)
		if err != nil {
			t.Fatal(err)
		}
		originalHead := fixture.finalized
		nextHeader, nextHash := rootReceiptHeaderFixture(t, originalHead, f.config.Action.BirthBlock+2, nil, false)
		fixture.headers[nextHash], fixture.byHeight[f.config.Action.BirthBlock+2] = nextHeader, nextHash
		operationCtx, cancel := context.WithCancel(f.storage.Context)
		currentAccountReads, closingReads, waits := 0, 0, 0
		baseFault := fixture.fault
		fixture.fault = func(method string, params []json.RawMessage, n int) (any, bool) {
			if method == "state_getStorage" && len(params) == 2 {
				var key, at string
				if json.Unmarshal(params[0], &key) == nil && json.Unmarshal(params[1], &at) == nil && key == accountKey.Hex() && at == originalHead {
					currentAccountReads++
				}
			}
			if method == "chain_getFinalizedHead" && currentAccountReads >= 2 {
				closingReads++
				if closingReads == 1 || !recoverHead {
					return f.config.Action.BirthHash, true
				}
				fixture.finalized = nextHash
				return nextHash, true
			}
			if method == "author_submitExtrinsic" {
				return record.ExtrinsicHash, true
			}
			return baseFault(method, params, n)
		}
		chain.client.retryWait = func(ctx context.Context, _ time.Duration) error {
			waits++
			if !recoverHead {
				cancel()
			}
			return ctx.Err()
		}
		result, err := custody.submit(operationCtx, chain, approval, f.key)
		cancel()
		retained, loadErr := custody.load()
		fixture.stateLock.Lock()
		posts := fixture.counts["author_submitExtrinsic"]
		accountReads, finalizedReads := currentAccountReads, closingReads
		fixture.stateLock.Unlock()
		if loadErr != nil || retained.Submission == nil || waits != 1 || accountReads != 2 {
			t.Fatalf("pre-post closure fixture missed its causal read: %+v reads%d waits%d %v", retained.Submission, accountReads, waits, loadErr)
		}
		if recoverHead {
			if err != nil || result.Phase != "signed" || posts != 1 || retained.Submission.Attempts != 1 || finalizedReads != 2 {
				t.Fatalf("healthy covering head did not preserve original post: %+v posts%d close%d %v", result, posts, finalizedReads, err)
			}
		} else if !errors.Is(err, context.Canceled) || posts != 0 || retained.Submission.Attempts != 0 {
			t.Fatalf("unavailable closing read consumed submission: posts%d attempts%d err%v", posts, retained.Submission.Attempts, err)
		}
	}
}
