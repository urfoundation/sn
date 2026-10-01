// Public host/owner handoff tests use actual private-file custody and synthetic
// public signatures. No hardware device or external chain is contacted.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Export starts from the real original five-journal preparation and its sixth
// separately approved owner journal. Every input remains synthetic.
func ownerSigningPreparedFixture(t *testing.T) (*bootstrapChainFixture, *ownerTrimTestFixture, string, string, ownerSigningRequest, ownerSigningTrust) {
	t.Helper()
	chain, f := ownerTrimPreparedTestFixture(t, ownerSigningTestKey().Public().(ed25519.PublicKey))
	ownerSigningTestLedgerConfig(t, f)
	configPath := filepath.Join(filepath.Dir(chain.path), "owner-ed25519-config.json")
	metadataPath := filepath.Join(filepath.Dir(chain.path), "owner-runtime.hex")
	bootstrapRootTestWrite(t, configPath, f.config)
	if err := os.WriteFile(metadataPath, []byte(f.metadata+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath+".v15", []byte(f.ledgerMetadata+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := chain.command(t.Context(), "trim-apply", &stdout, &stderr, "--trim-config", configPath, "--trim-approval-key", f.key); code != 0 {
		t.Fatalf("claim owner v2: %d %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := chain.command(t.Context(), "trim-export", &stdout, &stderr, "--trim-config", configPath, "--trim-approval-key", f.key, "--metadata", metadataPath, "--ledger-metadata", metadataPath+".v15"); code != 0 {
		t.Fatalf("export owner v2: %d %s", code, stderr.String())
	}
	var request ownerSigningRequest
	if err := decodePlanJson(stdout.Bytes(), &request); err != nil {
		t.Fatal(err)
	}
	trust := ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: f.key, Owner: f.config.Action.Coldkey, Genesis: f.config.Action.Network.GenesisHash}
	return chain, f, configPath, metadataPath, request, trust
}

// Public signature import/reopen/reimport preserve exact nonce, era, envelope,
// approval lineage and all five original journals without increasing allowance.
func TestOwnerSigningImportPreservesOriginalCustodyAndRecovery(t *testing.T) {
	chain, f, configPath, metadataPath, request, trust := ownerSigningPreparedFixture(t)
	original := chain.journals(t)
	reads := chain.census.count("state_getStorage")
	ownerDirectory := ownerSigningTestDirectory(t)
	requestPath := filepath.Join(ownerDirectory, "portable-request.json")
	bootstrapRootTestWrite(t, requestPath, request)
	responsePath := filepath.Join(ownerDirectory, "emulated-response.hex")
	if err := os.WriteFile(responsePath, []byte(hex.EncodeToString(ownerSigningTestLedgerResponse(t, request))), 0600); err != nil {
		t.Fatal(err)
	}
	raw, diagnostic, code := ownerSigningTestCommand(t, "reply", requestPath, trust, "--ledger-response", responsePath)
	var reply ownerSigningReply
	if code != 0 || decodePlanJson(raw, &reply) != nil {
		t.Fatalf("owner-side reply: %d %s", code, diagnostic)
	}
	replyRef := bootstrapRootTestWrite(t, filepath.Join(ownerDirectory, "portable-reply.json"), reply)
	var stdout, stderr bytes.Buffer
	args := []string{"--trim-config", configPath, "--trim-approval-key", f.key, "--request", requestPath,
		"--accept-request-hash", trust.RequestHash, "--reply", replyRef.Path, "--reply-sha256", replyRef.Sha256}
	for attempt := 0; attempt < 2; attempt++ {
		stdout.Reset()
		stderr.Reset()
		if code := chain.command(t.Context(), "trim-import-reply", &stdout, &stderr, args...); code != 0 {
			t.Fatalf("original reply import %d: %d %s", attempt, code, stderr.String())
		}
		store, err := openOwnerTrimStore(t.Context(), chain.preparation, f.config, f.key, false)
		if err != nil {
			t.Fatal(err)
		}
		record, err := store.load()
		store.close()
		if err != nil || record.Phase != "signed" || record.Broadcasts != 0 || record.Signature != reply.Signature || record.RawExtrinsic != reply.RawExtrinsic ||
			record.ExtrinsicHash != reply.ExtrinsicHash || !reflect.DeepEqual(record.Config, f.config) {
			t.Fatal("owner import/reopen changed original signature, nonce, era or allowance", err)
		}
	}
	before, err := os.ReadFile(f.config.Action.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := chain.command(t.Context(), "trim-export", &stdout, &stderr, "--trim-config", configPath, "--trim-approval-key", f.key, "--metadata", metadataPath, "--ledger-metadata", metadataPath+".v15"); code != 3 {
		t.Fatal("signed action was exported as a fresh signing request", code)
	}
	badArgs := append([]string(nil), args...)
	badArgs[len(badArgs)-1] = rootObjectHash("another reply file")
	if code := chain.command(t.Context(), "trim-import-reply", &stdout, &stderr, badArgs...); code != 2 {
		t.Fatal("wrong signed reply file pin admitted", code)
	}
	changed := reply
	changed.RequestHash = rootObjectHash("another portable request")
	changedRef := bootstrapRootTestWrite(t, filepath.Join(ownerDirectory, "changed-reply.json"), changed)
	badArgs = append([]string(nil), args...)
	badArgs[len(badArgs)-3], badArgs[len(badArgs)-1] = changedRef.Path, changedRef.Sha256
	if code := chain.command(t.Context(), "trim-import-reply", &stdout, &stderr, badArgs...); code != 2 {
		t.Fatal("same native signature accepted a different portable request", code)
	}
	after, err := os.ReadFile(f.config.Action.StatePath)
	if err != nil || !bytes.Equal(before, after) || !reflect.DeepEqual(original, chain.journals(t)) || chain.census.count("state_getStorage") != reads {
		t.Fatal("offline public handoff mutated original custody or accessed chain state", err)
	}
}

// A valid externally supplied signature cannot settle an ambiguous local signer
// phase. The existing original-custody recovery contract remains unchanged.
func TestOwnerSigningReplyCannotResolveUnknownSigningCustody(t *testing.T) {
	chain, f, configPath, metadataPath, request, trust := ownerSigningPreparedFixture(t)
	store, err := openOwnerTrimStore(t.Context(), chain.preparation, f.config, f.key, false)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	record.Phase = "signing"
	owner := ownerTrimExecutor{config: f.config, key: f.key, store: store}
	if err := owner.persist(record); err != nil {
		t.Fatal(err)
	}
	store.close()
	original, err := os.ReadFile(f.config.Action.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	requestRef := bootstrapRootTestWrite(t, filepath.Join(ownerSigningTestDirectory(t), "request.json"), request)
	reply, err := newOwnerSigningReply(request, ed25519.Sign(ownerSigningTestKey(), ownerSigningBytes(f.config.Action)))
	if err != nil {
		t.Fatal(err)
	}
	replyRef := bootstrapRootTestWrite(t, filepath.Join(ownerSigningTestDirectory(t), "reply.json"), reply)
	var stdout, stderr bytes.Buffer
	if code := chain.command(t.Context(), "trim-import-reply", &stdout, &stderr, "--trim-config", configPath, "--trim-approval-key", f.key,
		"--request", requestRef.Path, "--accept-request-hash", trust.RequestHash, "--reply", replyRef.Path, "--reply-sha256", replyRef.Sha256); code != 3 {
		t.Fatal("public reply resolved unknown original signing custody", code)
	}
	if code := chain.command(t.Context(), "trim-export", &stdout, &stderr, "--trim-config", configPath, "--trim-approval-key", f.key, "--metadata", metadataPath, "--ledger-metadata", metadataPath+".v15"); code != 3 {
		t.Fatal("unknown signing custody exported a fresh signing request", code)
	}
	retained, err := os.ReadFile(f.config.Action.StatePath)
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatal("refused reply changed unknown original custody", err)
	}
}

// Even a valid newly approved Ed25519 action cannot repurpose a claimed v1
// journal. Custody migration requires its own explicit workflow and evidence.
func TestOwnerSigningV2CannotReplaceClaimedV1(t *testing.T) {
	chain, f := ownerTrimPreparedTestFixture(t, ownerSigningTestKey().Public().(ed25519.PublicKey))
	store, err := openOwnerTrimStore(t.Context(), chain.preparation, f.config, f.key, true)
	if err != nil {
		t.Fatal(err)
	}
	store.close()
	original, err := os.ReadFile(f.config.Action.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	ownerSigningTestLedgerConfig(t, f)
	for _, create := range []bool{false, true} {
		if owner, err := openOwnerTrimStore(t.Context(), chain.preparation, f.config, f.key, create); err == nil {
			owner.close()
			t.Fatal("new v2 signature domain replaced claimed v1 custody")
		}
	}
	retained, err := os.ReadFile(f.config.Action.StatePath)
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatal("refused codec migration changed original journal", err)
	}
}

// Existing sr25519 custody can use portable recovery without becoming a Ledger
// action. This exercises the public handoff on both supported native schemas.
func TestOwnerSigningV1PortableReplyImport(t *testing.T) {
	native := newOwnerTrimActionTestFixture(t)
	chain, f := ownerTrimPreparedTestFixture(t, native.pair.Public())
	configRef := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(chain.path), "original-v1-config.json"), f.config)
	store, err := openOwnerTrimStore(t.Context(), chain.preparation, f.config, f.key, true)
	if err != nil {
		t.Fatal(err)
	}
	store.close()
	request, err := newOwnerSigningRequest(f.config, f.key, f.metadata)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := native.pair.Sign(ownerSigningBytes(f.config.Action))
	if err != nil {
		t.Fatal(err)
	}
	reply, err := newOwnerSigningReply(request, signature)
	if err != nil || reply.SignatureScheme != "sr25519" {
		t.Fatal("legacy owner portable reply lost original scheme", err)
	}
	requestRef := bootstrapRootTestWrite(t, filepath.Join(ownerSigningTestDirectory(t), "request.json"), request)
	replyRef := bootstrapRootTestWrite(t, filepath.Join(ownerSigningTestDirectory(t), "reply.json"), reply)
	var stdout, stderr bytes.Buffer
	if code := chain.command(t.Context(), "trim-import-reply", &stdout, &stderr, "--trim-config", configRef.Path, "--trim-approval-key", f.key,
		"--request", requestRef.Path, "--accept-request-hash", request.ContentHash, "--reply", replyRef.Path, "--reply-sha256", replyRef.Sha256); code != 0 {
		t.Fatalf("v1 portable reply import: %d %s", code, stderr.String())
	}
}
