# Native custody, snapshot heads and owner recovery

MG-09 / PH-09 remains in progress. These exact isolated sources qualify local
custody behavior; they are not a composed production release or a deployment
approval. The frozen SN `258e25b4` / server `0aa1e244` source, image and unsigned
planning receipts remain unchanged. All ten unsigned actions stay blocked.

## Source and evidence boundaries

| Scope | Frozen source | Author normal / race | Independent normal / race |
| --- | --- | --- | --- |
| Native journal | SN `695f6683173889e66926150fed7cc06bd78d8a23` | 42 / 42, three-package vet | 42 / 42, three-package vet |
| Snapshot head and passive read composition | SN `a5c765c4b0c9342350acf185d35042bbab894dac` | 17 roots + 17 subtests per mode, vet | 17 roots + 17 subtests per mode, three-package vet |
| Miner fleet and claim custody | SN `b3c3d661849eeacbce7a395ed6829a8f45eb1e79` | 201 roots + 3 inherited subtests per mode, miner vet | 201 roots + 3 inherited subtests per mode, miner vet |
| Chain, validator-source and operator-source monitors | SN `f1b445f9e8dafca790f7bc147430bc9c242b65e5` | 114 / 114, mainnet vet | 114 / 114, mainnet vet |
| Owner attributes in backup inventories | Connect `0a5cda0ebe78f6200c4cff6fd3d1aa172a2b172c` | 57 / 57, durablevolume vet | Pending separately |

The new consumer sources use the previously qualified Connect `6cd720cf` v2
root-generation policy. The inventory-v3 candidate is a separate additive peer
source. Test counts overlap earlier scopes and must not be added into a full
package, full Connect, full SN or release claim. Independent execution does not
establish independent compiler or dependency provenance.

The following receipt files are copied byte-for-byte. Their raw command output,
source archives, exact module replacements and causal overlays remain external
at the paths bound inside each receipt.

| Receipt | SHA-256 |
| --- | --- |
| [Native author](durable-native-journal-author-20261002.json) | `c0569ceba8cf412f7447feeb60c1b90d7c01d31b08adc0354137454e8dbe57db` |
| [Native independent](durable-native-journal-independent-20261002.json) | `d0fc6021be65a0b0556989b398a8d5b3563da4d56ceb5a54e6e71a9eac67fba2` |
| [Snapshot author](durable-snapshot-readonly-author-20261002.json) | `67b836274acf3d6bb2331bfc1298768af699094429acc10e9407ab8ea7f94f66` |
| [Snapshot independent](durable-snapshot-readonly-independent-20261002.json) | `c714c4b75bb343b2885dd95859eca4bc2c6eaf44fcccadd3a26d28812948caa5` |
| [Miner author](durable-miner-author-20261002.json) | `fff73485fe63aaeeae1c9212d05b301c01a6fafd9ddb4d10832bf6a04fefabbc` |
| [Miner independent](durable-miner-independent-20261002.json) | `e28deeff9e1a0b48056f21a14fd25b0f8fc03cba466056a5a5879574d2f6d902` |
| [Monitor author](durable-monitor-author-20261002.json) | `7ff09e809b48333a3e80185cc78d32943d1d41a972d855ead17f793a32d6f11b` |
| [Monitor independent](durable-monitor-independent-20261002.json) | `a6e5a4d20bf464d6325b788e05c8dedcc12815d8ece75a0a4fe500d8ad2090d1` |
| [Inventory-v3 author](durable-inventory-v3-author-20261002.json) | `0cefc7bfb4b13239e2abe4406cbee93b88cdb4fb46d52c3e7b58fba6d25d114f` |

## Retained custody and bounded continuation

A volume UUID, private directory and retained inode do not establish that the
journal members survived a close/reopen. Native `695f6683` therefore requires a
preprovisioned journal/log/raw-directory census with the fixed
`user.urnetwork.native-journal-custody` anchor. Runtime admission never creates
missing authority. Descriptor-relative reads retain named-leaf identity and
can inspect completed bytes below the write reserve. Current request
cancellation is checked before signing and broadcast, independently of the
retained owner's context.

An actual write that loses its acknowledgement differs from refusal before a
write. The native journal retains a bounded pending commitment and reconciles
only its complete exact bytes. This includes a fully written/synced original
raw temporary file before its final no-replace rename. Partial, unknown,
colliding or missing bytes remain refused; recovery neither re-signs nor
re-broadcasts nor reconstructs missing history. The four native causal scopes
retain their distinct original/intermediate boundaries and 5 / 1 / 2 / 5
intended failures in each mode. Initial fixture failures remain recorded.

Snapshot v2 heads live on the already precreated lock descriptor when a lock is
specified. This avoids placing many unrelated owner anchors on one directory
xattr block. A declared committed-absent head permits only its explicit first
publication; losing a previously committed marker and record cannot become a
fresh owner. Writers retain exclusive locks. `OpenReadOnly` composes passive
shared-lock readers without upgrading them or performing reconciliation. The
exact `ae6ca602` alias control fails its shared-to-exclusive lock upgrade in
both modes. The older directory-anchor and capacity receipts remain unchanged.

Miner claim daemons require the strict daemon declaration at actual public
stateful entry points. Fleet signing uses the separately selected owner-local
policy; stateless inspection remains portable. Missing ambient context never
selects an unguarded writer. Signed claim replay checks retained original bytes
again after finalized RPC work and before send. A completed Prepared callback
that loses its acknowledgement still retains the shared nonce floor.

Claim and monitor reserve pressure pause the affected owner without discarding
completed bytes. Uncertain snapshot publication joins/closes the old owner and
may reopen only the exact retained pending generation, at most three times per
controller/process lifetime. A terminal claim member or monitor role does not
cancel healthy peers. Stopped monitor roles release their metrics and checkpoint
locks before reporting completion; explicit parent cancellation joins all
remaining workers. Operator terminal events report `ownership-error` instead
of describing a stopped worker as `retrying`. The recovery budget is not a
durable campaign-wide allowance and does not prove anti-rollback if all valid
predecessor bytes and local authority are replaced together.

Miner replay/swarm/nonce-floor controls and monitor late-close/global-cancel/
recovery controls use the exact prior function bodies with their documented
compatibility adaptations. These are causal body substitutions against current
guarded storage, not clean historical whole-program builds. Monitor controls
fail 2 / 1 / 1 roots in each mode. The earlier monitor `18dc7ae1` 109-pass /
3-failure run and `441f265f` 113-pass / 1-failure run remain failed attempts.
The terminal monitor gate uses one uniquely owned, bounded tmpfs PostgreSQL 18
fixture with random loopback port, pinned image identity and successful cleanup;
resident services were untouched.

## Backup evidence and remaining preparation

The monitor source also qualifies actual `storage-inventory` / `storage-verify`
dispatch: exact declaration/hash arguments, absent/canceled admission refusal
and short-output failure. That source still uses Connect `6cd720cf` inventory-v2,
whose scope omits the new owner attributes. It must not claim complete owner
custody restoration.

Separate Connect `0a5cda0e` advances inventories to v3. It records exact opaque
native-journal, validator-attempt-ledger and snapshot attribute values and
SHA-256 through the real no-follow descriptors. Root generation stays separately
bound. Two required limits bound owner-attribute count and value bytes; their
ceilings are 10,000 and 16 MiB, with each known owner value at most 4,096 bytes.
Encoded entry metadata is separately capped at 60 MiB within the 64 MiB evidence
reader bound. Unknown or malformed `user.urnetwork.*` metadata prevents a
complete report. Arbitrary non-owner attributes, ACLs and security metadata are
not covered.

Three test-only controls on unchanged Connect `6cd720cf` reproduce omitted known
attributes, a removed retained anchor passing restore comparison, and unknown
custody attributes being silently ignored. All three fail normally and under
race. The v3 successor distinguishes transient observation failure from proven
concurrent metadata change, preserves cancellation, and keeps a closed caller
descriptor as a caller error. Its original 56-pass / one-test-expectation-failure
run remains separate from the terminal 57-root scope.

Rebound comparison requires an explicitly reviewed target root declaration and
the exact original opaque owner attributes. Copying an anchor whose contents
bind an old inode does not make that anchor valid for the new writer. Inventory
and verification always return `restart_authorized=false`; they validate no
protocol signature, pending-head semantics, cross-host restoration, PostgreSQL,
Redis or remote MinIO recovery.

Monitor independent checking is sealed. The [inventory CLI successor](durable-inventory-cli-qualification-20261002.md) now passes nine author normal/race roots and three-package vet with the v3 limits and explicit owner-local/rebound reporting. Independent inventory-v3/CLI checks, broader validator/bootstrap/root/server composition and deployment declaration assets remain separate gates. A production offline preparation
plan/apply command is still missing. It must explicitly distinguish fresh and
retained owner kinds, bind former-writer stop/join evidence, provision reviewed
roots/leases/generation and owner anchors, and refuse runtime enrollment or
history reconstruction. Test fixture provisioning is not that workflow.

Native 16 MiB log / 64 MiB raw / 10,000-member limits and fleet/claim 16 MiB
snapshot limits are finite refusal bounds, not a production capacity forecast.
Headroom alerts, reviewed retention/rotation, copied-root rebind, recovery policy
and actual host/device/backup rehearsal remain open before unattended operation.

## Qualification storage checkpoint

At the 110 GiB data-volume floor, new independent/adopter phases were held while
already running tests joined. Two completed `1d580d5e` release Go build caches
were then reclaimed after a privileged census of 386 processes found no live
command/environment/mapping/descriptor references and the sealed manifest
excluded both cache paths. Source, module caches, images, raw evidence and
receipts remained intact. The [cleanup receipt](durable-custody-cache-reclaim-20261002.json)
retains SHA-256
`147595482f517fc7a7fcbfe061f6fe31bb5e3163962cb582eaa1152c3e9bec6d`;
available bytes rose from 118,836,633,600 to 123,270,598,656. The first guard
refusal observed the preparation shell's own cache-path references and is
retained separately; retry ran after that shell joined. This build-host cleanup
does not close production storage capacity or media/restore gates.

## Separate validator/root/bootstrap/blob addendum

The [bounded adopter source](durable-adopter-qualification-20261002.md) now has
its own 111-root author normal/race gate, vet and real service-credential test.
Its independent checking and composed integration remain separate from the
primitive/miner/monitor receipts above.
