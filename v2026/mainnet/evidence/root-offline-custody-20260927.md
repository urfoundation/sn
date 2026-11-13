# Offline root custody handoff qualification — 2026-09-27

The existing-seat root action owner now has a concrete offline public-signature
handoff in `root_offline_custody.go` and `root_offline_custody_store.go`.
Independently provisioned Ed25519 approval binds one exact action; a matching
native sr25519 signature is verified, durably retained and recovered byte for
byte. Missing receipts remain unresolved and never mean never-issued.

This is a package-local adapter, not a signing/submission CLI or active root
service. It loads no native secret and makes no network call. The existing
read-only canonical chain port still refuses every submission. No live key,
RPC call, native signing, transaction, deployment, merge or push was used.

Qualified worktree: `/home/by/urnetwork/sn-mg08-root-submit-20260927`.
Branch: `codex/mg08-root-submit-20260927`.
Base: `c16eddea3a05b1d94910dcc47b47eaf0dc8cabf2`.
Toolchain: Go 1.26.6 linux/amd64.

Final-source results, bound by `source-final.sha256` and `source.patch`:

| Command | Result |
| --- | --- |
| `go test ./mainnet -count=1 -timeout=15m -json` | 233 roots and 41 subtests passed; 164.329s reported package time |
| `go test -race ./mainnet -run '^TestRootOfflineCustody' -count=1 -timeout=15m -json` | All 11 changed custody roots passed; 48.263s reported package time |
| `go vet ./mainnet` | Passed |
| Final source hash verification; staged/unstaged `git diff --check` | Passed |

The earlier broader race command
`go test -race ./mainnet -run '^(TestRootOfflineCustody|TestRootAction)' -count=1 -timeout=15m -json`
passed 34 roots in 257.864s. Its 23 existing root-action roots are unchanged and
remain adjacent coverage. The final change added a fixed 64-byte hex admission
bound before allocation and an overlength receipt case, only in
`root_offline_custody.go` and `root_offline_custody_test.go`. All 11 affected
custody roots and the entire normal package were rerun on that final source.
This is not a single final-source 34-root race invocation. The prior full normal
run also passed 233 roots plus 41 subtests in 169.324s, with vet passing. There
were no test failures, race reports or timeouts in any qualification run.

`results.json` and per-command metadata preserve exact commands, working
directory, UTC start/end, process elapsed time and exit status. JSON event logs
and passed-test lists preserve terminal test results. `source.sha256` and
original source copies retain the broader race version; `source-final.sha256`,
`final-source-delta.txt`, final copies and the staged code patch identify the
final implementation. Both normal and race test binaries were rebuilt from
this unchanged final source and retained with build metadata; the qualification
commands themselves used Go's transient test binaries. These retained binaries
are not misrepresented as separately executed qualification runs.

The 11 deterministic test roots cover:

- Independent approval and native signing domains; changed nonce, era, weights,
  generation, identity, runtime, limits or state path cannot be rehashed into
  approval. Wrong chain 945, trust, key, custody or policy is rejected.
- One exact signed intent, pending issuance across restart, packet/vector
  ownership and original-byte recovery through `rootActionOwner`. A signature
  receipt cannot supply live authority or enable broadcasting. Old finalized
  receipts remain recoverable after authority removal.
- Corrupt/foreign/short/overlength/noncanonical signatures, idempotent exact
  import and refusal to replace an already retained signature with another
  valid signature for the same native payload.
- Explicit barriers for canceled waiters and interrupted save responses;
  concurrent exact imports write once. Pre-commit and post-commit failures
  poison the current instance; reopening uses the complete surviving record.
- Integrity failures require reopening even if the file is restored. A single
  owner, immutable marker and private bounded strict JSON reject missing,
  empty, reapproved/rehashed replacement, unknown/duplicate/trailing fields,
  oversized state, public permissions, symlink and FIFO state/marker files.

The adjacent review inspected `root_action.go`, `root_action_store.go`,
`root_signing.go`, the canonical receipt adapter and shared strict JSON decoder.
The new adapter preserves the existing one-request/nonce reservation and
never-issued distinction instead of inferring a fresh allowance from a missing
external-signing receipt. It reuses the existing pinned codec source
`67dcf7f791dc495064c293f080a0702cb433e51e`; no new source-to-Wasm or live runtime
provenance was established.

Remaining activation gates are independent current root authority/eligibility,
approved mainnet pins/source-to-Wasm, real native signer integration, device
receipt provenance, globally fenced custody/nonce ownership and rollback
protection, bounded fee exposure, protected seat continuity, approved canonical
RPC trust, actual bounded submission and the supervisor. Packet hash is local
receipt correlation; the native signature does not authenticate that field or
prove custody policy compliance. Local checksums/locks are not global fences.
Native payloads do not bind local runtime-code/policy hashes, registration
generation or a hard maximum fee. `accumulate_in_place` still needs no heartbeat;
this adapter concerns only a separately approved `explicit_root_weights`
action on an existing owned root seat. MG-08 remains blocked for activation.

Retained evidence: [`RESULT.md`](/mnt/data/sn-testnet/evidence/mainnet-root-offline-custody-20260927/RESULT.md).
All 40 bundle checksums verified. `SHA256SUMS` SHA256:
`929092eab7d7a900a9e628b9520f930859bc999dfaad84676130304dd111bafb`.
