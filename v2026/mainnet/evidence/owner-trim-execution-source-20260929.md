# Owner-trim durable action source checkpoint

This MG-08 increment starts from SN `e35771ec47aa8d486ce2af03a83eb3edace9cbde`.
It adds a complete one-action executor, original v3 custody binding, independent
action/route approval, native encoding, signature recovery, durable send allowance,
canonical financial receipts and actual-subset before/after reconciliation.
Public commands support planning, custody claim/resume, original public-signature
import and read-only reconciliation. They supply no production signer or future
window enforcement adapter. No live signing, RPC, deployment or activation was
performed by this implementation work.

The original v3 config and five journals remain unchanged. The sixth fixed action
holds their read-only locks and its own exclusive lock; its fresh approval binds
their exact retained state, runtime, policy/review, coldkey/subnet generation,
capacity, nonce, mortal anchor, custody, fee reserve, broadcast limit and route.
Missing completed state, new valid replacement approval and uncertain signing
cannot renew that allowance. Financial finality with missing census stays
generation-outcome-unresolved and supports only receipt-preserving readback.

The [pinned owner call](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/admin-utils/src/lib.rs)
accepts netuid/capacity with owner rate limits and an admin window; it carries no
atomic registration-generation predicate. The
[trim algorithm](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/uids.rs)
compresses surviving UIDs and provides no individual removal receipt. Therefore
the result reports exact dispatch/fee separately from whole-block correspondence,
explicit old-miner residuals and any changed protected generation; neither a
lower census count nor an absent hotkey proves a full native reset.

The concrete admission reader checks exact current nonce/call metadata, the
existing bounded safe set, proxy absence, original UR/root generations and
network immunity through original expiry. The
[pinned public subnet pruning rule](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/root.rs)
skips a network while current block is strictly less than registered-at plus
the saturating immunity period. This observable predicate cannot freeze a later
Root/governance change. Independently enforceable future owner/privileged/root
scope, global coldkey custody, source-to-Wasm mapping and fee exposure remain
required. A signed best-effort risk policy is not inferred or enabled here.

Astra max performed source implementation and compile/vet/format checks only.
The first frozen source `4033609`, identified in the immutable
`/mnt/data/sn-testnet/qualification/mg08-owner-trim-phase-20260929/astra/HANDOFF.md`,
failed Sol's normal/race qualification on the current-window proxy read:
21/22 focused and 225/226 expanded roots passed in each mode. The
[qualified repair and complete evidence](owner-trim-null-storage-repair-20260929.md)
preserve that failure and integrate exact successor `ce567305`; 25/25 focused
and 229/229 expanded roots pass normal/race, with eight causal controls.
New deterministic roots cover action/route binding, metadata reindexing and root
wire equivalence, event association, lost signer response, uncertain sends,
nonce conflict/expiry, durable-store errors, original v3 locking and restart,
post-finality census continuation, residual/protected generations, real canonical
receipt conflicts, proxy metadata, public-pruning boundary and semantic/current
nonce conflicts. The adjacent scope includes existing bootstrap, subnet/trim,
root custody/service/signing/action/receipt/submission roots.

The external compile modfile retains physical Connect `b163f9dd`, SDK `516521fb`
and composed server `b7c8c743`; tracked module files are unchanged. The earlier
readiness compile failures and original qualification snapshots remain preserved.
Its redundant broad race process was intentionally terminated; the exact
148-root disjoint race union remains qualified. The later
[composed readiness receipt](/mnt/data/sn-testnet/qualification/mg08-bootstrap-readiness-20260929/COMPOSED-RESULT.md),
SHA-256 `a68f0b1f755504128733be1954115a9e68e16c7cd4f92e6317ba6868440bda0f`,
passes all ten focused roots normal/race, vet and fences for SN `e35771ec` with
server `b7c8c743`. The owner-trim successor has its own separate Sol receipt;
the older readiness result is not reused as action qualification.

The separate owned-route check reported HTTP 502 at 06:59 UTC on September 29.
No current mainnet identity or authority was established. MG-08 remains open
for actual trim authority/execution, full contracts, two admitted healthy UR
validators, the separate root service and observed native 10/90 economics.
