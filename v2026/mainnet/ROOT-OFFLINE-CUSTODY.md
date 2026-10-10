# Offline root custody handoff

[root_offline_custody.go](root_offline_custody.go) implements the public-receipt
side of the existing-seat [root action owner](ROOT-ACTION.md). It retains one
independently approved request, imports a verified native signature and recovers
the same signed transaction after restart. The previous action owner had only a
test custody adapter; this supplies a concrete durable offline handoff.

The [bootstrap local phase](BOOTSTRAP-ROOT.md) now calls this adapter through
concrete plan/apply/resume commands, with exact public input pins and durable
child-state reconciliation. There is no secret loader, native signer, device
transport, submission command or active root service. It cannot establish current eligibility, authenticate custody's
global nonce fence or authorize live operation. Its successful return means
that the specified public artifact was verified and retained locally.

## Exact approval and request

The caller provisions `rootOfflineCustodyTrust` independently of imported files.
It pins the native chain, nonzero genesis, EVM chain ID **964**, hotkey, custody
ID, policy hash, Ed25519 approval public key and canonical private journal path.
ID 945 is refused. The approval key is distinct from the native sr25519 role;
neither key's secret is loaded by this implementation. Trust must come from the
approved operational authority, not from the packet being checked.

An approval has schema `urnetwork-mainnet-root-offline-approval-v1`, an embedded
root action, and `approval_signature_ed25519` as lowercase 64-byte hex without
`0x`. Its embedded action contains every field of the prepared request except
that `scope.approval_hash` and `request_hash` are empty. Those two fields are
empty only to break the approval/request hash cycle. All other fields, including
native payload, exact runtime/source, generation, nonce, mortal checkpoint,
weights, fee reservation, broadcast limit, custody and action-state path, are
covered by the approval signature.

The bytes approved are the literal schema string, one zero byte, then Go
`encoding/json.Marshal` of the typed approval with its signature field empty.
The approval signature is not a signature over arbitrary JSON whitespace or an
untyped caller-selected digest. `signingBytes` exposes the exact bytes to an
independent approval implementation; this code cannot issue that approval.

The complete signed approval's `rootObjectHash` becomes
`action.scope.approval_hash`; the action's request hash is then recomputed with
its own hash field empty. `newRootOfflineCustodyPacket` checks the original
prepared native encoding, the exact normalized action, independent signature,
trust and content hashes. Changing and rehashing an action or approval does not
pass verification. Native metadata preparation still uses the reviewed
`67dcf7f791dc495064c293f080a0702cb433e51e` profile; this format does not replace
source-to-Wasm or independently approved current metadata/code provenance.

The packet schema is `urnetwork-mainnet-root-offline-request-v1`. It contains
the full action, approval and independently configured trust hash. The action
and custody journals must use distinct paths and cannot overlap either file's
`.lock` marker. Each is a separate lifetime obligation.

## Durable handoff and recovery

1. Prepare and independently approve the exact action. Create the private
   custody journal explicitly with `openRootOfflineCustodyStore(trust, &packet)`.
   Its immutable marker binds the trust and packet hashes; the initial
   `requested` record and containing directory are synced before open succeeds.
2. Construct `newRootOfflineCustody` from that journal. Its `packet` method
   exports a copy of the retained proposal. Export never grants fresh signing
   authority. The separately approved external custodian must verify its own
   current authority, global fence, nonce ownership, expiry and cost limits
   before producing any signature.
3. An external device's public response uses schema
   `urnetwork-mainnet-root-offline-signature-v1`, `packet_hash`, and
   `signature_sr25519` as lowercase 64-byte hex without `0x`. `importSignature`
   checks the packet identity and verifies the native signature with the exact
   approved hotkey and payload, including Substrate's Blake2b-256 rule for
   payloads exceeding 256 bytes. It assembles the exact extrinsic and syncs its
   signature/hash record before returning success.
4. The adapter supplies `rootActionSigner`: `signOnce` and `recoverSignature`
   return only this retained receipt. They do not cause native signing. The
   action owner can retain the original bytes after restart, reconcile an old
   finalized receipt and preserve its original fee/nonce reservation. New
   broadcasting still requires that owner's separate current authority checks.

Reimporting the exact receipt is idempotent. A second valid signature over the
same payload cannot replace the already retained bytes through this owner.
There is no renewal, new nonce, era replacement, reset or automatic retirement.
The action journal continues to own broadcast counts and canonical outcomes;
the custody journal continues to retain the original public signature.

The receipt's `packet_hash` correlates the local import with this journal. It is
not an additional field in the native signed payload and does not independently
prove that a device received this packet or followed its custody policy. The
separate approval covers the complete action; native verification proves only
the payload/hotkey signature. Device receipt provenance remains an external
custody qualification gate.

A missing receipt always means **issuance unresolved**, including after device
timeout or restart. It never returns `errRootSignatureNotIssued`. Only a
separately authenticated, globally fenced custody service could attest that no
signature has ever been issued. The local absence of a receipt is insufficient.

Operations serialize with cancellation-aware ownership. Canceled waiters do not
read or mutate the journal. An import interrupted after persistence can return
an error while the receipt is already durable; retry/reopen recovers that exact
receipt. A storage or integrity error poisons the instance, requiring reopen.
No background worker is detached; callers join all operations before closing
the store. Explicit creation cannot reuse a retained marker, even when state
was lost or creation interrupted.

The [store](root_offline_custody_store.go) requires a precreated private directory
without symlink traversal, private regular files, an exclusive process lock,
strict JSON without duplicate/unknown/trailing fields and a 512 KiB record cap.
It syncs the replacement file, atomically renames it, then syncs the directory.
Both unsigned and signed records retain independent approval verification.

## Remaining authority and custody gates

This is local durable ownership. A hostile host can replace marker and state or
restore an older valid directory. This code does not prove device-side
idempotency, distributed exclusion, rollback resistance, approval revocation or
ownership of the whole hotkey's nonce lane. A valid signature may also have been
broadcast externally. Production must authenticate those custody properties and
reconcile the original signed action; creating another directory is not a fresh
allowance. The root action owner remains the canonical reconciliation owner.

The offline Ed25519 approval authenticates an exact proposal. It does not prove
live root eligibility, protected seat continuity, global custody or enforceable
fee exposure. The signed native call does not bind registration generation,
local source/code hashes or a maximum fee. Same-version runtime changes,
inclusion-time seat churn and native payment exposure retain the limitations in
[ROOT-ACTION.md](ROOT-ACTION.md). The [service decision owner](ROOT-SERVICE.md)
now supplies a composite decision/intent journal and finite joined supervisor.
The [owned submission adapter](ROOT-SUBMISSION.md) now supplies separately
approved native HTTP transport and durable uncertain-send reconciliation.
Production current authority and actual native signer integration remain absent;
the existing read-only canonical
chain port's `submit` method remains unconditionally disabled.

The proposed `accumulate_in_place` strategy still needs no native heartbeat.
These types apply only to a separately approved `explicit_root_weights` action
for an existing owned root seat. SN25 owner authority or majority SN25 validator
weight supplies no root basket authority, protected root registration or chain
Root origin.

## Qualification

`root_offline_custody_test.go` uses synthetic Ed25519/sr25519 keys, local private
directories, in-memory chain/authority fixtures and explicit channel barriers.
It covers independent approval/trust substitution, exact-byte restart and root
receipt recovery, unchanged broadcast authority, unknown issuance, signature
replacement, both sides of an ambiguous durable write, cancellation/concurrent
imports, one owner and missing/empty/replaced/malformed/private/special-file
state. No live key, signer, RPC call or transaction is used. Retained results are
in [the custody handoff qualification](evidence/root-offline-custody-20260927.md).
