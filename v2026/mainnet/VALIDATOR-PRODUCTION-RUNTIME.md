# Production validator runtime admission

The default `validator --config` loader accepts schema 3 only after authenticating
an independently selected owner-recycle production approval. Schema 2 runtime
observation and v1 owner-recycle observation approvals remain unable to authorize
production signing, submission or archive acceptance. Schema 3 does not grant
bootstrap `init`, registration or staking authority through their separate loader.

The production approval uses `urnetwork-owner-recycle-approval-v2`, its separate
signature domain, and `production.schema=urnetwork-owner-recycle-production-v1`.
It binds the complete resolved configuration, exact mainnet genesis, EVM chain
964, netuid 25, current runtime version/code/metadata, finite native block and
epoch windows, independent approval signer, validator and owner identities, and
`runtime_capability=urnetwork-validator-producer-interface-v1`. The config pins
the approval file by absolute path, byte count and SHA-256. The loader reads at
most 2 MiB of configuration and installs immutable private authority before key,
journal or service admission. Editing or relabeling a loaded config invalidates
that authority.

The production runtime-binding boundary checks the approved connection, fresh
native chain name/genesis/EVM identity, finalized height and canonical block hash. It then
authenticates the complete runtime tuple and metadata bytes at that exact block.
The purpose check covers consumed storage encodings and defaults, source batch
calls, receipt events, ordered signed extensions, and selective-metagraph API v2.
The source artifact review remains responsible for economic semantics; wire
compatibility alone is not approval. The owner-recycle decision path separately
checks its successor storage, census and activation prerequisites.

Only the dedicated production-purpose bind creates a signing capability. An
exact read-only artifact, matching exported version fields, a testnet provisional
view or another connection cannot create one. Generic rebinding clears it. A
failed read, capability check, cancellation or closing canonical check preserves
the previous view. Startup stake checks consume the already authenticated block.
Prepared replay requires both the original and current exact artifacts to match,
as well as the independently authenticated durable production intent; an old
signature is never rewritten for a replacement runtime.

Compatible configuration renewal uses the separate bounded
`production_authority_history` described in the
[production transition](OWNER-RECYCLE-PRODUCTION.md#durable-original-production-authority).
Old sidecars resolve their original complete config and signed approval, while
current signing remains under the new independent approval. Original authority
never becomes a new prepared grant. The signed original drain block and first
native epoch survive a later runtime window; another drain is not required.

`production_runtime_approvals` is optional for an initial deployment. When present,
it contains at most 64 private content-addressed files of at most 16 KiB each.
Each document uses `urnetwork-validator-production-runtime-history-v1`, the same
deployment/policy/network coordinates, a complete exact artifact, reviewed
source/review hashes, and `runtime_review_scope` equal to the producer capability
above. Revision 1 has no predecessor; later revisions name the immediately prior
file's SHA-256. Closed intervals are ordered, nonoverlapping and strictly earlier
than the current signed block window. Gaps grant no authority. A reviewed rollback
uses its own explicit interval; version-number ordering grants no history.
Complete original authority bundles also contribute their exact effective runtime
windows, so a renewal need not duplicate those facts in tuple-only documents.
Any overlapping explicit document must agree with the retained original bytes.

Historical startup, activation, decision and source readers preserve each original
artifact at its original block and retain the loaded config through their wrappers.
Current signing uses only the current signed window. Observation-history files cannot be
relabeled as production history, and compiled testnet artifacts are never added
implicitly. Content-addressed approval, bundle and runtime-document retention
supports restart after provisioning source loss and independent archive replay.
Native archive capture also retains the production owner census, validator
freshness and stake reads, and exact metadata and drain storage at the signed
activation hash; capture storage failures abort the read.

Public startup may initialize its read view at the independently signed activation
hash and authenticate retained original work before current preparation. That
historical initialization grants no fresh signing capability. New intents and
trail workers still wait for current eligibility and actual initial settlement
publication. Native writer routes remain explicitly signed WS/WSS because
submission uses a WebSocket subscription. HTTP read transport on an approved
node is not an implicit writer-route fallback.

These local synthetic tests do not establish a live runtime approval,
validator activation, successful transaction or economic outcome.
