# Explicit best-effort owner trim

This workflow can submit only an already imported owner-signed transaction under
a separate signed risk policy. It supplies no owner key, device, live approval,
registration setter, or validator activation authority. Existing strict v1/v2
actions keep their original enforced-window requirement.

The implementation was authorized on October 2. No residual risk was accepted
for live use, and no live transaction was signed or submitted. The earlier v470
artifact exception remains **planning only**. Select a release containing this
source increment before preparing a new action; the frozen `3d1e2ecf` release
does not contain it.

## Original custody and independent approvals

A new action uses execution schema
`urnetwork-mainnet-owner-trim-best-effort-execution-v1`, action schema
`urnetwork-mainnet-owner-trim-best-effort-action-v1`, and selection rule
`reviewed-current-selection-with-explicit-inclusion-risks`. It uses the existing
Ed25519 Ledger metadata-hash envelope and portable [owner signing](OWNER-SIGNING.md)
workflow. It requires new independent approval of the exact action and route.
The original preparation can be legacy v3 or passive-root v4.

An already claimed strict action cannot be relabeled. Keep its original nonce,
era, signature, journal, marker and consumed allowance. This increment provides
no custody migration or renewal path.

The additional fixed owner-trim journal is exclusive. Its owner checks the
original physical directory, all borrowed preparation markers, its own marker
inode and exact bytes, and the exact preceding journal around every read and
publication. Observed marker replacement, missing or changed journal bytes,
malformed state, or predecessor rollback permanently poisons that owner instance.
Restoring a pathname cannot heal it.
Only an interrupted initial unsigned reservation under the original incomplete
marker is recoverable. A completed missing journal is never recreated.

## Offline review and one guarded submission

All commands retain the original `--config`, `--run-dir`, `--accept-plan-hash`,
`--trim-config`, and independently supplied `--trim-approval-key`.

1. Use `bootstrap-chain trim-plan` with the fresh schema, exact metadata and
   `--ledger-metadata`. Independently approve the resulting action/route config.
2. `trim-apply` claims local custody; `trim-export` with the same exact metadata
   and `--ledger-metadata` produces the portable request.
   The owner reviews and signs on their separately qualified device. Import only
   the exact original reply with `trim-import-reply`. No host signer is installed.
3. `trim-submit-plan` outputs an unsigned `approval_template` and exact hex
   `signing_bytes`. These bind the original signed extrinsic, complete config,
   runtime, reviewed census, protected/root generations, original mortality and
   consumed attempts. They include mandatory governance/privileged-action,
   inclusion-selection, fee-overrun, cross-host custody, provenance and outcome
   residuals. A fee reserve is not an on-chain fee maximum.
4. Obtain a separate Ed25519 approval over those exact signing bytes. Keep the
   policy as a private JSON file containing the completed approval object, then
   pin its file SHA-256. Supply the submission approval public key independently.
5. Only `trim-submit` accepts `--submission-policy`,
   `--submission-policy-sha256`, and `--submission-approval-key`. It reconciles
   canonical original bytes, rereads the original/current censuses and exact
   runtime/nonce/proxy predicates, then reserves at most one transport post.
   It rechecks finalized mapping and physical custody immediately before posting.

The submission policy is immutable once attached. A new signature over different
choices, expiry or allowance cannot replace it. Draft flags never authorize a
send. The node's acknowledgement does not establish inclusion or completion.
A lost reply retains the same bytes and consumes the numbered attempt; the next
invocation reconciles before considering another bounded attempt. `trim-reconcile`
remains read-only, including after terminal financial settlement or exhausted
allowance. No path refreshes a nonce or mortal era.

## Explicit pruning and registration choices

`trim-submit-plan` defaults to these conservative choices. Empty fields in an
older signed best-effort policy preserve the same checks and original bytes.

| Draft option | Default | Separate optional acceptance |
| --- | --- | --- |
| `--public-pruning-policy` | `require-public-pruning-immunity-through-original-expiry` | `accept-public-pruning-and-netuid-reuse-risk` |
| `--registration-policy` | `require-observed-closed-registration` | `accept-competing-registration-and-reentry-risk` |

Each acceptance adds its own exact mandatory residual to the signed policy.
Accepting one cannot accept the other. Public pruning can remove the subnet and
reuse its netuid between the last read and inclusion; the native call does not
bind a subnet generation atomically. Competing registration, swaps and re-entry
can alter the selection in the same interval. These are explicit risks, not
claims of enforcement.

Even with both acceptances, current registration flags must equal the reviewed
flags, and every other census, protected-generation, ownership, root membership,
runtime, nonce, proxy and physical-custody check must pass. Observed drift refuses
the send. These choices do not invent permission to close registration.

The [October 2 read-only check](evidence/owner-trim-retained-intent-20261002.md)
found SN25 pruning immunity already expired and both stored registration flags
true. Exact v470 source requires chain Root for the ordinary registration setter
and per-block registration limit; the PoW setter always returns
`POWRegistrationDisabled`. A stored PoW flag does not prove that PoW registration
is executable. A netuid-0 validator seat does not confer chain Root.

## Outcomes and remaining launch gates

Canonical dispatch and actual fee settlement survive a missing later census.
Before/after correspondence records actual original generations, residual miners,
survivor UID mappings, protected losses and unexpected re-entry. In this fresh
best-effort domain, an open registration flag alone does not erase observed
generation correspondence; changed or unapproved generations still conflict.
Strict v1/v2 retains its original correspondence rule. Whole-block observations
never prove that every absence was caused by this one call.

Every result keeps `full_reset_completed=false` and `activation_ready=false`.
Actual device/account/digest qualification, a classified current census, both
independent approvals, external custody of the coldkey and outstanding signatures,
original receipt/outcome review, any retained-miner disposition, contract
completion and separate UR/passive-root host activation remain required.

The selected v470 root role is passive. Its profile requires the retired
`set_root_weights` call and old weight gates to be absent. `rootActionStore` has
only test callers; the legacy root-service mutation ports remain nil. Enabling
that separate native root path would require its own custody and signer review,
but it is not a missing capability for the selected passive observer.
