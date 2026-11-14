// A lost submission acknowledgement consumes its original allowance before
// transmission; explicit recovery only scans original bytes and native bodies.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"path/filepath"
	"testing"
)

// Real CLI reopens real durable custody around one local RPC transmission.
func TestTreasuryPublicUncertainSubmissionCannotRenewOriginalAllowance(t *testing.T) {
	f := newTreasuryFixture(t)
	chain, rpc, request, _ := treasuryTestChain(t, f, "opened")
	chain.client.httpClient.CloseIdleConnections()
	rpc.stateLock.Lock()
	includedHead := rpc.finalized
	includedStorage := map[string]string{}
	for key, value := range rpc.storageKVs {
		includedStorage[key] = value
	}
	rpc.finalized = f.config.Action.BirthHash
	rpc.storageKVs = map[string]string{}
	for _, row := range f.input.Observation.Rows {
		if row.Raw != nil {
			rpc.storageKVs[row.Key] = *row.Raw
		}
	}
	rpc.fault = func(method string, _ []json.RawMessage, _ int) (any, bool) {
		if method == "author_submitExtrinsic" {
			return nil, true
		}
		return nil, false
	}
	rpc.stateLock.Unlock()
	dir := filepath.Dir(f.config.Action.StatePath)
	configPath, _ := treasuryTestJson(t, dir, "submission-config.json", f.config)
	metadataPath, _ := ownerRecycleTestFile(t, dir, "metadata.hex", []byte(f.input.Metadata))
	ledgerPath, _ := ownerRecycleTestFile(t, dir, "ledger.hex", []byte(f.input.LedgerMetadata))
	common := []string{"--config", configPath, "--approval-key", f.key, "--accept-action-hash", f.config.Action.RequestHash, "--production-authority-hash", f.config.AuthorityHash}
	run := func(mode string, extra ...string) ([]byte, int, string) {
		var out, diagnostic bytes.Buffer
		args := append([]string{mode}, common...)
		args = append(args, extra...)
		code := runTreasuryCommand(f.storage.Context, args, &out, &diagnostic)
		return bytes.Clone(out.Bytes()), code, diagnostic.String()
	}
	for _, mode := range []string{"reserve", "export"} {
		var extra []string
		if mode == "export" {
			extra = []string{"--metadata", metadataPath, "--ledger-metadata", ledgerPath}
		}
		if _, code, diagnostic := run(mode, extra...); code != 0 {
			t.Fatal(mode, diagnostic)
		}
	}
	reply, err := newTreasurySigningReply(request, ed25519.Sign(f.signer, f.config.Action.signingBytes()))
	if err != nil {
		t.Fatal(err)
	}
	replyPath, replyHash := treasuryTestJson(t, dir, "submission-reply.json", reply)
	if _, code, diagnostic := run("import-reply", "--reply", replyPath, "--reply-sha256", replyHash); code != 0 {
		t.Fatal(diagnostic)
	}
	if _, code, _ := run("submit"); code == 0 {
		t.Fatal("ambiguous RPC post claimed success")
	}
	if _, code, _ := run("submit"); code == 0 {
		t.Fatal("exhausted original approval was replenished")
	}
	raw, code, diagnostic := run("status")
	var record treasuryRecord
	if code != 0 || decodePlanJson(raw, &record) != nil || record.Attempts != 1 || record.RawExtrinsic != reply.RawExtrinsic || record.Phase != "signed" {
		t.Fatal("uncertain send lost original bytes/allowance", code, diagnostic)
	}
	rpc.stateLock.Lock()
	posts := rpc.counts["author_submitExtrinsic"]
	rpc.finalized = includedHead
	rpc.storageKVs = includedStorage
	rpc.stateLock.Unlock()
	if posts != 1 {
		t.Fatal("write retried without a new consumed allowance", posts)
	}
	raw, code, diagnostic = run("reconcile")
	if code != 0 || decodePlanJson(raw, &record) != nil || record.Attempts != 1 || record.Phase != "approval-recorded" || record.RawExtrinsic != reply.RawExtrinsic {
		t.Fatal("exhausted approval blocked original receipt recovery", code, diagnostic)
	}
	rpc.stateLock.Lock()
	posts = rpc.counts["author_submitExtrinsic"]
	rpc.stateLock.Unlock()
	if posts != 1 {
		t.Fatal("read-only reconciliation transmitted", posts)
	}
}
