# Owner trim: original custody and explicit best-effort submission

Date: 2026-10-02. Verdict: scoped source qualification. Implementation was
authorized; no live residual risk, owner signature, submission, deployment or
service start was authorized or performed. The v470 artifact exception remains
planning-only. The frozen `3d1e2ecf` release excludes this later source increment.

## Exact source and effect scope

| Pin | Purpose |
| --- | --- |
| `e12850f0842f4fa97367ea8404c3add44216fabf` | Unmodified production baseline for the four custody failures |
| `a792c63688e8761714d85fbfdfad343bac8cd6d3` | Additional exclusive trim marker and journal custody fix |
| `21640419806f21f37a1f144bcb978770ac7ef962` | Fresh best-effort action domain, original offline signature and independent submission policy |
| `0f7c869869a5201e8ffbbf4e053cc0ca20a12010` | Independent signed public-pruning and registration/re-entry risk options; final source |
| `ff7869d4` | Docs-only main reconciliation; previous release evidence retained |
| `ac86855df7f63a1d85a1c57a70298f9c7f9ced4d` | Unchanged server dependency |

The retained source fences name all six clean sibling checkouts, module hashes,
Go version, patches, bundles and changed-file SHA-256 values. No dependency or
root native-action source changed. All new scratch/cache was on `/mnt/data`;
no source read error was observed during this qualification.

The additional owner-trim store previously retained an advisory lock on an open
inode while trusting the named path, and could recreate a deleted signed journal
or accept an earlier valid predecessor. It now retains the physical directory,
its original marker inode/bytes, borrowed preparation markers and exact preceding
journal bytes. Reads and publications check them again. Observed custody loss
is a permanent integrity failure in that owner instance; restoring a pathname
cannot clear it. A completed missing journal is never recreated. The interrupted
initial unsigned claim remains recoverable under the original incomplete marker.

The [operator workflow](../OWNER-TRIM-BEST-EFFORT.md) retains the existing portable
Ledger request/reply boundary. Only a fresh best-effort action may attach a
separate independently signed policy naming the original signed extrinsic,
complete config, runtime, census, protected/root generations, mortality and
consumed attempts. The policy names exact unenforceable governance,
privileged-action, inclusion-selection/fee and external-custody residuals.
`trim-submit` installs the guarded original-byte adapter only after those inputs;
the native signer stays nil. Strict v1/v2 admission and already claimed custody
retain their semantics. No policy can renew an original nonce, signature, era or
allowance, replace a retained policy, or reuse a consumed post reservation.

Before each bounded post, the adapter reconciles original bytes, rereads complete
current/original censuses, runtime/call, nonce and proxy predicates, retains the
post reservation, then rechecks finalized mapping and custody. A lost response
consumes that attempt and requires reconciliation before another same-byte attempt.
Financial finality can settle while a later census remains unavailable.
Best-effort generation correspondence does not let an open registration flag erase
already observed generations; changed protected or unexpected generations still
conflict. It never establishes full reset, individual removal attribution or
validator activation.

## Qualification and causal evidence

All tests use synthetic identities and deterministic filesystem/RPC state
transitions. Fixed-source suites run `go test [-race] -vet=off ./mainnet` with
exact root selectors, `-count=1`, and explicit package timeouts; `go vet ./mainnet`
is separate. Selectors, commands, logs and root censuses are retained.

| Source and scope | Normal | Race | Vet |
| --- | --- | --- | --- |
| Author `21640419`: 16 new + 21 adjacent selected roots | 36 PASS, 1 SKIP; 126.922s | 36 PASS, 1 SKIP; 948.318s | PASS |
| Author `0f7c8698`: six new risk + ten prior best-effort + six adjacent roots | 22 PASS; 110.514s | 22 PASS; 818.820s | PASS |
| Author `0f7c8698`: separate retained native SDK trim codec opt-in | 1 PASS; 0.580s | 1 PASS; 2.931s | Covered above |
| Independent Sol `21640419`: 16 new + four adjacent normal; 16 new race | 20 PASS | 16 PASS | PASS |
| Independent Sol `0f7c8698`: exact successor selector in its receipt | 21 PASS | 21 PASS | PASS |

The original author ancestor suites **skipped**
`TestOwnerSigningNativeSdkTrimMetadataHashEnabled` because its four opt-in inputs
were absent. Those results remain skips. The later separate opt-in runs bind
the existing Linux extension
`77ffa6ac04459bc5d9895d225c2775827473d0db99c25802ea7aa32ae750a8d2`, clean SDK source
`67dcf7f791dc495064c293f080a0702cb433e51e` and `/usr/bin/python3` 3.12.3. They exercise
the real extension with synthetic metadata/fixture boundaries, not an actual
owner device or deployed runtime digest.

The exact `e12850f0` production baseline with only the copied test overlay fails
all four custody roots in 9.834s: marker replacement admits a send; deleting the
signed journal recreates it and sends; deleting a counted journal admits a send;
restoring an earlier valid reservation erases retained signature state. Independent
Sol reproduced those same four named failures in its own checkout. The fixed
roots and the valid incomplete-claim restart control pass.

Six isolated `21640419` omission controls remove one retained-intent guard each:
final custody, finalized head, protected-generation comparison, numbered post
reservation, approved current window and retained finalized continuity. Six
isolated `0f7c8698` controls remove one risk boundary or restore old behavior:
independent pruning choice, independent registration choice, unchanged observed
registration flags, other census blockers, exact signed residual text and observed
generation correspondence. Every control reaches its one intended test assertion;
compilation failures and skips do not count as causal proof. Full patches and
failure outputs are retained. These are bounded controls, not a whole-package or
release qualification.

## Current read-only prerequisite check

At 2026-10-02 01:02:27 UTC, the retained public-archive observation captured native
block **9,191,688**, hash
`0xb2ecafc53c28637c3eb7b8123b731b17f27538bf67038ff3ce962d442c0de84c`, runtime spec
470, code hash `0x5675b684d69a07f6f224c2ba9cabef719804911fba40fbe1a2295198c9cb7c47`.
Exact metadata/type/default checks read SN25 `NetworkRegisteredAt=2,998,801`,
`NetworkImmunityPeriod=864,000`, and absent/default `RegisteredSubnetCounter=0`.
Immunity ended before block **3,862,801**, already 5,328,887 blocks earlier; no
new supported 4–256-block mortal era can fit that observed immunity. No original
signed mainnet action was supplied.

The same complete, **unclassified** census retained 256 SN25 and 64 root members;
both stored registration flags were true. This is an owned-RPC observation,
not independent state proof or approved role classification. Exact inspected
v470 source `923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d` requires chain Root in
`AdminUtils.sudo_set_network_registration_allowed` and
`sudo_set_max_registrations_per_block`; the PoW setter unconditionally returns
`POWRegistrationDisabled`. The stale ordinary-setter comment does not grant
owner authority. A stored PoW flag does not prove a usable PoW registration path.
The retained source snippets and caller census bind that finding.

Conservative defaults therefore cannot make this observed owner reset usable.
The successor adds separately signed acceptance of public pruning/netuid reuse
and competing registration/re-entry. Each choice needs its own exact residual;
accepting one does not accept the other. Both choices still refuse observed
registration-flag changes, protected/root/subnet generation drift, owner/proxy,
nonce/runtime or original-custody conflicts. The last read cannot fence actions
before inclusion; no atomic native predicate was invented.

## Receipts and remaining gates

Author [receipt](/mnt/data/sn-testnet/native-owner-retained-intent-20261002/evidence/owner-trim-retained-intent-receipt.json)
SHA-256: `7a13eb21ef2ed7be97f62418213fd9b78618f0c08230ce8900dcaff0ac14008b`.
The private [evidence bundle](/mnt/data/sn-testnet/native-owner-retained-intent-20261002/owner-trim-retained-intent-author-evidence.tar.gz)
SHA-256 is `2045c6511d8c2999bfca052a0071b9f62b6ce47b3664d4c271b668110d702a24`.
Its manifest binds the raw read-only census,
source fences, logs, selectors and causal patches. Raw chain identities remain
outside the repository and are not test fixtures.

Independent ancestor [receipt](/mnt/data/sn-testnet/sol-mainnet-owner-retained-independent-20261002/receipt.json)
SHA-256: `272f9af24b39f325cabcf05b2d466ce9b9860c92f04935966ef365896e5172ce`.
Independent successor [receipt](/mnt/data/sn-testnet/sol-mainnet-owner-risk-independent-20261002/receipt.json)
SHA-256: `7ea4eb83aeef6f9e405b56836b6c804414b22372e89ae694717ab5e6732799bd`.
These separate scopes do not qualify a new release
artifact or constitute independent compiler reproduction from shared caches.

Still required: a successor release, approved exact runtime/source authority,
classified current census and protected generations, independently approved
fresh action and explicit residual policy, actual owner Ledger/account/digest
qualification and signature, exclusive external custody of all outstanding owner
actions, canonical receipt/outcome review and every retained-miner disposition.
Contract completion and separate two-UR/passive-root host acceptance and activation
remain distinct. `full_reset_completed` and `activation_ready` stay false.

The adjacent caller audit finds `openRootActionStore` only in its definition and
tests. Legacy root service constructs submission with nil current authority and
omits native `Authority`/`Submitter` ports. The selected passive-root profile
requires the retired root-weight call and gates to be absent and uses no native
signer. Enabling a future active native root-action path requires a separate
physical-custody/signing qualification; this receipt does not cover it.
