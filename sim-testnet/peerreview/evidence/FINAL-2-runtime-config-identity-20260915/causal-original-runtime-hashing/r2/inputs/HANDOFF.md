# Explicit runtime configuration identity migration

Frozen formatted source: `85c09583ce8a0728b4b33b04b19c9e7d7ea1272a`, raw parent
`c4b6a6c0f5dbcb23fb67ac6f686a1022e31c05fb`, based on published `eccfae8`.
Checkout: `sn/` beside this note. Root reviewed the production diff and six new
roots. Terra formatted the declared eleven Go paths; only the new test file
had five mechanical insertions/deletions. All production bytes are unchanged.
Astra ran no tests, builds, formatters or RPC for this correction.

## Cause and exact binding

The unchanged native setup at
`sn-infrastructure-release-integration-20260914/native-setup-preview-eccfae8`
failed at the original `authenticateCoordinatorRepairCarry` ConfigHash equality:
`coordinator repair configured strict domain differs`. State, lock and binary
fences stayed unchanged. PublicManifest.expected_runtime_spec changed455 to458;
releaseConfigHash previously hashed it directly, while the retained signed
repair and original plan still bind the genuine455 configuration hash.

The optional public `chain.config_identity_runtime_spec: 455` explicitly opts
into the reviewed455-to458 identity transition. It is omitted at zero in both
YAML and JSON, preserving exact old wire/hashes. Hashing copies PublicManifest,
sets only its copied expected runtime to455 and clears the copied optional pin.
Unpinned configurations retain ordinary hashing; other predecessor/current,
transaction/state, chain/genesis, schema or profile tuples cannot opt in.

The corresponding optional `SetupPlan.config_identity_runtime_spec` is included
in `plan_hash` and is never normalized from approval. `release_lock_hash` binds
the exact current458 code, metadata and source. Current config validation,
current persisted-plan loading, executor admission and detached final authority
require that approved pin and current458 release. ResolvedInputsHash, action
intents, roles, contract custody and economic limits are unchanged. Historical
plan/request/result readers retain omitted-pin original bytes and their hashes;
the original repair, companion, activation, probe and relay equality checks are
not relaxed. The genuine predecessor can seed setup revision but cannot become
current launch/resume authority without the new plan approval.

## Qualification operands

Use `handoff/selection.json`: exact70 top-level roots, disjoint migration20 and
adjacent50, each in package `github.com/urfoundation/sn/sim-testnet`. Per-partition
selector/root lists and the complete declaration map are beside it. Normal and
race each retain10-minute body/660-second outer limits. Compile each mode once,
then execute the two disjoint partitions with the existing streaming owners.
Package CWD is `sn/sim-testnet`; standard testnet.yml, policy/release fixtures,
coordinator-repair-budget-v1 testdata and sibling source projections are unchanged.
Use the same13 reviewed repository snapshots as eccfae8; no dependency changed.
Retain exact compiled lists, test.v events, actual numeric exits, complete source,
dependency and binary pre/post fences and joined owners. No new runner is needed.

Root-only outcome files are named `*-root-outcomes.tsv`; they are not substitutes
for full descendant metadata. Retained existing roots contain t.Run descendants;
use the exact source-derived `descendants.source.json` and
`*-full-outcomes.tsv` files (seven parents,58 children:29 per partition). All six new roots, all seven
historical-lock roots, and the changed FinalValidatorAuthorityV2 root have no
descendants. That final authority root's former three children are now a plain
four-case loop, including an equal-ConfigHash omitted-pin substitution refusal.

After normal migration20 passes, run the single root in
`handoff/confirmation-selector.txt` in fresh normal p2 then p3 on the same binary
and frozen source. The actual completed-repair root's passing matrix p1 counts.
Race20+50 is the affected race matrix; completed prior181/36 scopes and streaks
remain retained and are not reset.

## Deterministic causal

Use a separate disposable checkout of the final formatted head. Apply
`handoff/CAUSAL-original-runtime-hashing.patch` FORWARD. Only config.go becomes
dirty: delete ExpectedRuntimeSpec substitution while retaining optional-pin
clearing. This restores exactly the original unpinned458 hash behavior, rather
than hashing a newly added property. Record exact diff/path and reverse-check.
Production changes require one separate normal mutant binary using the existing
mutant owner; never mutate the qualified source or reuse its binary for this.

`causal-selector.txt` and `causal-roots.txt` select exact5 roots with no children.
Expected body exit1: repair/hash/plan identity roots FAIL, current-authority and
legacy-wire roots PASS. `causal-outcomes.tsv` and `causal-literals.tsv` are sorted,
with one literal per failure. No race mutant or broad pre-fix rerun is required.
Use the same10-minute/660-second limits, exact list and all ordinary fences.

## Adjacent review and integration

Reviewed the full source path from RunCommand/loadPersistedPlan through
BuildPlanForState/BuildPlanRevision, validator companion authentication,
observeCoordinatorRepairCarry and the exact signed source/receipt verifier,
then pure plan construction and current executor admission. The shared original
configuration equality also protects runtime_evidence_carry_v2.go prepared and
completed activation inputs, precompile_probe_successor.go custody/source/native
receipts, precompile_conformance.go, evidence_relay_continuation and campaign
succession. Their comparisons remain unchanged; normalization is one explicit
configuration identity boundary rather than a per-caller historical bypass.

The real signed-repair regression captures the synthetic public manifest before
HTTP fixtures substitute transport endpoints, constructs the original455 lock
before signing, reopens its omitted-pin approval, verifies current458 continuation,
and rejects unpinned/changed-cadence callers. Original journal/request/result/
source bytes remain unchanged and no write method is invoked. The shared
historical-lock fixture now models true public455/lock455 to public458/pin455/
lock458, covering restart, lineage, original payload/hash and budget controls.
Existing testResolvedConfig is explicitly unpinned and does not import today's
public.yml. Historical publication copies explicitly omit the pin. The native
activation fixture likewise starts from that unpinned synthetic constructor;
its historical454 transport pins do not inherit the new YAML default.

All six new names belong to the actual producer capture selector's existing
CoordinatorRepairCarry prefix; the selected capture census guard checks exact
ownership. No gate selector or frozen semantic census changes are needed.
The existing semantic authority root keeps its name and tests all four cases.

Production Go and the public YAML change, so source/protocol hashes, release lock
and final stamped CLI must be refreshed once by root. The old eccfae8 parser
cannot read the new strictYAML field; TF therefore builds one updated read-only
renderer from this final source before lock rendering. No implicit old-parser
fallback, state reset, signed archive rewrite, custody change or allowance
increase is authorized by this correction.
