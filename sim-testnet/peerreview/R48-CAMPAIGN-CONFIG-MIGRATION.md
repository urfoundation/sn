# R48 campaign timeout migration

The existing campaign uses request timeout/p99 values of 10000/15000 ms. The
current driver requires a request timeout of at least 60000 ms and p99 at least
the request timeout. Editing the old YAML both loses its original bytes and
invalidates the persisted setup-plan hash and retained generation readers.

`campaign-config-migration` captures a separate reviewed transition from exactly
10000/15000 to exactly 60000/60000. It retains both original YAML byte strings in
the immutable request, recomputes both full configuration hashes against the
unchanged public/hyperparameter manifests, and compares every other config
field. The old config is decoded as historical evidence only; ordinary config
validation remains unchanged. The source setup archive's exact bytes are hashed
into the request.

The successor changes its configuration hash, appends the source plan to its
lineage, and binds `campaign_config_migration_hash`. Only the local
`config.render` and `topology.launch` stamps are rebound; each explicitly carries
its one original intent. Every transaction action, allowance, policy, route,
release lock, role and validator generation remains unchanged. The owner signs
the complete request and exact successor plan hash into a distinct receipt.
Unsigned previews cannot authorize retained execution: active, invocation and
runtime plan loaders reauthenticate the receipt. Strict release loading rejects
these provisional migration plans.

Capture uses the new YAML as the normal `--config` and the preserved old YAML as
the explicit historical source:

```sh
sim-testnet campaign-config-migration --config NEW_YAML --state-dir STATE \
  --provisional-resume --plan-hash OLD_PLAN \
  --config-migration-source-config OLD_YAML --format json
```

Capture archives review artifacts only; it neither signs nor switches the active
plan. Apply requires its separate reviewed request hash:

```sh
sim-testnet campaign-config-migration --config NEW_YAML --state-dir STATE \
  --provisional-resume --plan-hash OLD_PLAN --apply \
  --config-migration-hash REVIEWED_MIGRATION_HASH --format json
```

Both commands must use the existing approved owned-RPC route if the source plan
requires `--owned-rpc-authority`. Apply acquires the ordinary exclusive deployment
lock and requires the supervisor and both validators stopped. It authenticates
the review, snapshots, archived predecessor and current active plan; publishes
the immutable signed receipt; then atomically advances `plan.json`. An
interruption between receipt publication and pointer advancement retries using
the same verified receipt without loading a signing key. An already-advanced
pointer is likewise idempotent. Apply changes no chain state, validator YAML,
ledger, source-role receipt or runtime manifest.

Retained rollover and source-role readers obtain the authentic old parsed config
through `authenticatedCampaignConfigMigrationSource`. The runtime manifest must
keep the old config identity, authenticated by the same migration. Neither a
new render identity nor a source hash assigned to different config contents is
accepted. The signed migration source resolver supports an exact requested old
ancestor for retained generation reads; the native recovery adapter independently
requires the immediate predecessor and binds its sealed failed terminal source.

The live sequence is one stopped interval: review both code paths; stop the
existing owners; apply this config migration; capture and separately approve/apply
the exact native one-edge recovery under the new config and this receipt; then
restart once. Native epoch selection waits until the qualified composed binary
and both command paths are ready. These migration tests use generated identities
and never sign or apply against live state.

Regression coverage includes unsigned config edits, exact old/new hashes and
bytes, adjacent config/authority changes, owner-signature tampering, strict
acceptance rejection, generation/source-role preservation, retained runtime
identity and topology receipts, explicit absent/empty/malformed original
activation completion, and deterministic receipt-before-pointer interruption.

Qualification on 2026-09-26:

- `go test -timeout 30m -p 2 ./sim-testnet -run '^TestCampaignConfigMigration' -count=1`
  passed in 150.135s.
- The same command with `-race` passed in 401.988s.
- The widened selection
  `^(TestCampaignConfigMigration|TestPolicyRolloverRetained|TestProvisionalPlanAdoption|TestEvidenceRelaySourceExpansionRetainedManifest)`
  reached the default 10-minute timeout normally (600.173s) and with `-race`
  (600.534s), without an assertion failure. The normal run was constructing
  `TestPolicyRolloverRetainedRenewalSourceRoleKeepsOriginalApproval`, decoding
  an approximately 27 MB reviewed plan. The race run was constructing
  `TestPolicyRolloverRetainedRenewalRoundSevenPreservesGeneration`, likewise
  in `archiveReviewedSetupPlanBytes` / `decodePersistedPlanWire`.
  Those wider runs are incomplete, not passing qualification.

The config pathname is not part of the signed request. A dedicated target path
is valid when its exact YAML bytes, resolved public/hyperparameter manifests,
roles and owned-RPC route match the review. The absolute state directory remains
part of the signed request.
