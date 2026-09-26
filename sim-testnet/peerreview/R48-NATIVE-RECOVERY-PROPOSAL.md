# R48 native history recovery proposal

This is a reviewable recovery design, not an approved recovery request. No live
file, signer, process, plan, or validator state was changed. The implementation
branch starts at `dd0909a6` on `origin/codex/r46-integration-20260926`. Existing
authority does not grant the missing native edge, so no production permission
was widened. R47 has sealed and been invalidated after an early failure. Its last
observed block is **8,091,300**, before the planned terminal block **8,092,324**.
The signed reason is `execution-exited-before-completion`; six assertions include
five failures. Sealing does not supply the unobserved interval. Its failed result,
fault snapshot and native attempts remain unchanged. The bundle is committed as `7a4a26bb`
and its report as `6fd5fa1f` on the integration branch.

## Authenticated starting point

The read-only `ObserveReleaseNativeSourcesV2` reader successfully authenticated
both active validators' original measurement/envelope signatures, source
commitments, canonical finalized native inclusion, actual signing identity,
reveal/application lifecycle, and applied weight rows. Both contain exactly one
applied generation-2 intent: native epoch **1690**, settlement epoch **649**.
Both commits finalized at block **8,090,245**; application blocks are
**8,090,543** and **8,090,539**. Both compact EMA files retain epoch 1690.

The selected config, intent and EMA hashes were unchanged across these reads.
This authenticates the native source, not the complete settlement/public archive.
The active producer was not stopped, so these observations cannot serve as the
later exclusive capture or authorization.

The exact read-only evidence is retained outside the source repository at
`/mnt/data/sn-testnet/qualification/r48-native-recovery-20260926/`:

- `native-source-observation.json`: successful native source observations and
  independently authenticated runtime artifacts.
- `observed-basis.json`: selected process/config paths, byte counts, hashes,
  owner/mode/inode observations, state namespaces, source files and prior requests.
- `observe-source.go`: read-only collector; it opens no signer and changes no
  live state. Its compatibility observer retains results in memory.
- `native-pool-history-complete.json`: six exact-block pool/validator observations,
  with independently authenticated runtimes and rechecked canonical hashes.
- `SHA256SUMS`: exact evidence and qualification hashes.

Sanitized projections and their raw-source hashes are in the
[portable peer-review bundle](evidence/FINAL-3-R48-native-recovery-proposal-20260926/README.md).
No config bytes, private state, credentials or operational RPC address are copied.

The relevant existing plan identities are:

| Authority | Exact identity |
| --- | --- |
| Setup plan | `0x8bb92697db8f2164e46f6e58848d3407e509382fb61550b919f1d55391ad480e` |
| Setup config | `0x1dc4882e26b9311a0f4444872d15fc4a3361246e9ca44f24fe24b08a98eca7bd` |
| Policy | `0x41f0c7efe7e1b23b2fd22dac9352ca18be48d2e4d1fb5b41ce89660bc899b0dd` |
| Generation-2 activation plan | `0xaecc96a8238d6309bb3d611627194a198390f3b458b4f9e830cacdada99fa120` |
| Generation-2 source-role plan | `0xe58dab65ba701ef19af1af92c416fde144bd086d166d20f44fba66760c6fdd89` |
| Source-role signed handoff SHA-256 | `53421aca801bdc471b9b33c88fe0943f9a2e4d2102bcee5102084d4bb3b141c5` |

Selected validator source hashes, with the `sha256:` prefix omitted:

| Source | Validator 1 | Validator 2 |
| --- | --- | --- |
| Active config | `8584efe5090f1349bb66ca47fb834e299c9bbb5e1349cbada530ffe605101d88` | `2e3039f8a3bf0e7cbf12d272077264578af4bdaeaa442922fef43cfa3fc57e54` |
| Complete intent prefix | `c17241eb9a98de12cc2f3ef64799f89d791d3ff2c9ccca052353d1ae5bfa4ec9` | `8df8b8d7c8090f097ece805957c88b10be6c4b9ea93b96bb0f69e3a2b0d1a1ef` |
| Compact EMA | `ae95a09dca35e8b9fcbb91a3c6a2ff579f5df66077ee21fcf5f673671545dd4d` | `078aa739e6b4d1389c4db0a2de59a043c77368fdcecb914a746cf512b4d0dd4f` |
| Last measurement | `e0f514e5a368e575992de6adff7c7e1083202c0c1fd07a8187aeb7bd85748bb5` | `2043c465b0ffad004c8f0b2b15ecdb7415e77739877c2d5174fba2d0fee32743` |

Validator 1 runs the original generation-2 `validator.yml`. Validator 2 runs
`policy-rollover/source-role/generation-00000000000000000002/validator-2/validator.yml`.
Both coordinator directories remain under their respective
`runtime/validator-N/evidence-generations/generation-00000000000000000002/`.
Validator 2's selected config and coordinator are consequently **not siblings**.
Its source-role descriptor remains an immutable proof of the preceding native
slot; it imports no intent, EMA or ledger history and grants no gap permission.

Both selected configs retain the reviewed runtime **467/1/1** and the explicit
`urnetwork-subtensor-consumed-interface-v1` profile. The actual signed native
source and the independently read chain use **node-subtensor/471/1/1**:

| Runtime artifact | Independently authenticated value |
| --- | --- |
| Code hash | `0x5b0168d2878c1fdcdc424dddca29111b8fe8960bd169742d1c14ce16b22ae381` |
| Metadata hash | `0x78d6038b7e6d154114e294ffb664619e64701f11eb7e8ea79a8deedf6c8fabd6` |

Observed interface compatibility is not a reviewed runtime release. Clearing
the original profile, rewriting its runtime fields, or changing signed source
bytes would destroy the retained identity instead of establishing strict replay.

## Pool emission is a separate recovery gate

The retained vector's meaning matters as much as continuity. Both authenticated
native1690 measurements explicitly audited source settlement epoch **648** as
`artifact_unavailable`, `compliant=false`, `disposition=zero_pool_weight`, with
`source payout root is not committed on chain`. Both actual applied rows omit
pool UIDs **3** and **4** entirely. Validator 1 also legitimately controls NO1,
so its pool3 weight remains masked independently; validator 2 controls neither NO.
A fresh applied intent can therefore still leave both operator pools unfunded.

Exact LAN native reads distinguish an earlier inactive-weight interval from this
later active-but-zero-pool interval:

| Blocks | Validator activity and stored pool weights | Pool emission/incentive |
| --- | --- | --- |
| 8,084,976–8,084,977, around the last nonzero epoch631 capture | UID255 active; weights UID3=65534, UID4=65535; UID254 inactive | 73,799,848,706 and 73,800,974,879 rao; both incentives 32767 |
| 8,085,276–8,085,277, around epoch632 capture | Both validators inactive; UID255 still stores those same positive pool weights | Both pool emissions and incentives zero |
| 8,090,544 and 8,091,127, after native1690 application | Both validators active; both rows omit pool3 and pool4 | Both pool emissions and incentives zero |

At the first two pairs, UID255 `LastUpdate=8,079,823` and `ActivityCutoff=5000`.
The active-vector transition and retained positive weights support activity expiry
after the long earlier steering silence as the epoch632 cause; this predates
native1691's outage. At the last two blocks both validators instead have
`LastUpdate=8,090,245`. Their new zero-pool rows are directly supported by the
signed deposit audits. These facts establish separate upstream allocation
problems without attributing every historic capture to the later outage.

The parent's independent EVM scan found all 28 `EmissionCaptured` events over
blocks 8,087,000–8,091,200 zero; epoch651 captured zero at 8,090,977 and finalized
zero entitlements at 8,091,127 for both NOs. Those EVM receipts are retained by
the parent; the portable bundle here contains this investigation's native reads.
[`captureEmission`](../../evm/src/STSettlementVault.sol) emits `EmissionCaptured`
with amount zero only when the exact `(pool hotkey, vault selfColdkey, netuid)`
stake is zero. Subfloor dust and missed capture windows instead emit
`EmissionDeferred`. Final entitlement total is captured funding plus prior carry.
Thus a nonzero payout root or usage/deposit observation alone is not payout funding.

No new treasury funding requirement is established by these observations. The
next run must separately establish the current pool UID/hotkey and vault coldkey
ownership, a valid lagged payout root/artifact, exact compliant deposit and
positive unbound-provider quality, an eligible **positive** pool allocation from
an uncontrolled validator, a real applied row and active validator, then automatic
pool emission/stake growth at the native payout boundary and a timely **nonzero**
vault capture. Claims must subsequently reconcile to funded entitlement and
conservation. No manual stake injection or new spend is authorized by the
native-history recovery, and such an injection would not prove native reward
causality. If any link fails, diagnose that link before admitting measured R48
financial coverage.

## Why the existing commands cannot authorize this edge

The original [history adoption](../../validator/release_history_adoption_v2.go)
already provides the required mathematical semantics: one fixed
`LastNativeEpoch → FirstNativeEpoch`, an exact applied intent prefix, one actual
EMA fold, append-only restart, complete intermediate settlement replay, and
strict consecutive native epochs after that edge. It never creates the missing
epoch's intent or native receipt.

Its existing launch path is intentionally narrower than this state:

1. `ReleaseHistoryAdoptionV2.configure` and `CheckReleaseHistoryAdoptionV2Source`
   reject runtime compatibility. They require the coordinator directory beside
   the request's config; validator 2's source-role overlay violates that shape.
2. The simulator's capture/render path targets original root validator configs
   and coordinator directories, rather than the selected generation and overlay.
3. [Process launch](../process.go) selects rollover **instead of** strict
   adoption when a rollover exists. The rollover projection strips old adoption
   arguments. Retained provisional supervisor admission also rejects strict
   adoption arguments.
4. [Final adoption verification](../final_semantic_adoption_v2.go) hard-codes the
   original root namespace. Its source-plan comparison expects the original
   activation lineage, not a new generation-bound recovery subplan.
5. [Historical config validation](../../validator/config.go) separately rejects
   every provisional runtime-compatibility config before final archive replay.

No live argv grants history adoption or closed-native-input deferral. Existing
saved adoption requests name native epochs 1467–1492 and other approvals.
Runtime compatibility is a separate permission. Setting
`provisional_defer_closed_native_input` alone also cannot authorize a generation-2
gap: intent and EMA gap admission additionally require authenticated retained
startup, which the fresh generation deliberately does not inherit.

## Smallest separate native recovery change

Use a new **generation-bound, owner-signed one-edge recovery subplan**, with a
dedicated decoder/constructor and launcher handoff. Reuse the existing private
`releaseHistoryAdoptionV2` edge semantics after admission. Do not weaken the
existing strict-adoption decoder, add an unrestricted `allow_gaps` option, or
turn generation-2 startup into provisional retained-history reuse.

The new subplan must bind these exact fields:

| Bound data | Required content |
| --- | --- |
| Current authority | Exact setup plan, deployment/chain/genesis/netuid, config and policy hashes |
| Predecessor | R47 recovery generation 47; sealed result SHA-256 `1f89032d92aa3ac694288742880c11e86f021adbed2b05c4a79a626beaa9e951`; signed invalidation SHA-256 `bb4c9f5f3fb540c1f3dc2dad28002ef1df0795188fcfc3aa59001cf9f5c27a2b` |
| Selected generation | Generation 2, original rollover plan and handoff hashes, selected source-role plan and signed handoff hashes |
| Executable | Qualified source revision, binary hash, and relevant verifier version |
| Each validator | Selected config path/bytes/hash, exact coordinator/client namespaces, retained source-role descriptor, identity, complete applied intent prefix count/hash, terminal native epoch and artifact hash, compact EMA hash |
| Retained history | Bounded immutable input/terminal/publication census, required public objects, no unresolved publication/candidate marker; authenticated source/native receipt census |
| Runtime | Original config/profile preserved; independently pinned 471/1/1 code and metadata above; no wildcard successor permission from this recovery |
| Edge | One shared, explicitly chosen future `FirstNativeEpoch=N`; per-validator `LastNativeEpoch=1690` if unchanged at exclusive capture; missing native range `[1691,N-1]` recorded as absent |
| Scope | `provisional=true`, `final_acceptance=false`, no state import, no setup replay, no additional allowance or acceptance waiver |

`N` remains intentionally **unassigned** after R47 sealed. The final owner
must acquire stopped-topology exclusivity, recapture the exact source,
read a canonical finalized native schedule, and choose an epoch with enough
time for measured full startup. A source or epoch change requires a newly
reviewed hash; this draft cannot mint that approval.

Canonical subplan bytes receive a new plan hash. A separate exact-hash apply
must require both the retained setup approval and that new hash, then sign the
existing `ReleaseEvidenceEnvelope` form under a distinct native-recovery kind,
with `RunID` equal to the new subplan hash and the existing deployment owner as
signer. The signed receipt is local authorization, not a chain transaction.
The source-role receipt must remain unmodified and cannot be reused as that
signature.

The implementation should make four small, separately testable ownership changes:

1. **Capture and apply:** resolve the generation through the ordinary authenticated
   rollover/source-role readers. Capture only while the supervisor and validator
   owners are stopped; retain the deployment writer lock across final recheck
   and receipt selection. Use bounded, anchored private readers, join all Close
   results, and reject changed bytes, path aliases, mode/owner changes, partial
   publication or unfinished intents. Create only the new immutable receipt and
   request files; preserve all configs, ledgers, inputs, intents and EMA bytes.
2. **Validator startup:** a dedicated hash-pinned native-recovery child request
   verifies the signed selection's exact config and state owner, allowing the
   known overlay/config separation only through those pins. It preserves runtime
   compatibility and installs the existing one-edge adoption owner. Keep
   `retainedStartup=false` and closed-input deferral disabled. Run complete
   activation, public-stream, signed-input, settlement, source/application and
   EMA authentication before exposing workers.
3. **Process and restart:** attach this request *after* the authenticated generation
   projection in both fresh and retained launch paths. Record its exact path/hash
   in supervisor argv and preserve it across authorized restarts. Recheck the
   original append-only prefix every time. Before N, submit no native input;
   after an unused N, fail closed. After a real N intent exists, reconcile its
   existing lifecycle and permit only N+1 next. Never choose a new epoch merely
   because startup was slow.
4. **Evidence:** capture the owner-signed subplan, child requests, original configs,
   selected generation/source-role provenance, missing range and actual native
   receipts. The ordinary collector still reconciles existing signed inputs and
   releases eligible unsigned reservations before its native-gap refusal. Keep
   the failed R47 attempts and all native coverage gaps visible to final checks.

The runtime need not mutate or clear the old EMA. The new actual measurement
uses native1690's retained transcript as its predecessor and folds exactly once
at N. It must authenticate every real settlement terminal between settlement649
and its new settlement. If that history is missing or inconsistent, recovery
remains refused; this subplan is not authority to synthesize it.

## Separate condition for strict final archive acceptance

The native recovery above only resumes future provisional steering. It does
**not** close the historical runtime/archive gate or R47's failed coverage.
Strict finalization needs one of the following separately reviewed authorities:

- **Runtime migration:** independently review the exact runtime471 code,
  metadata, signed extensions, consumed native/storage interfaces and economic
  behavior; update the release/runtime authority through the existing signed
  plan/config migration. Preserve original 467 configs and all profile-tagged
  471 source bytes as historical provenance. Add a historical, read-only adapter
  bound to those captured config/source hashes and the exact approved runtime
  artifact. Fresh signing and historical decoding must remain separate. The
  adapter permits full replay; it supplies no passing verdict.
- **Scoped historical-runtime exception:** if accepting the consumed-interface
  evidence instead of a reviewed runtime release is intended, explicitly approve
  a new owner-signed exception covering only the two exact config hashes, original
  profile, exact 471/1/1 code/metadata and captured generation/source graph. It
  may authorize complete archive replay of those sources, never skip signatures,
  native receipts, cuts, math, conservation, coverage or any other final assertion.
  Record that exception in acceptance evidence. It is separate from the native
  edge approval and no existing R44 lifecycle exception implies it.

Neither option is currently implemented or approved for this source. Changing
`ValidateHistorical` to accept all compatibility profiles would broaden authority
to later unreviewed runtimes. Merely advancing the reviewed spec constant is also
insufficient: [prepared source decoding](../../crv4/source_commitment.go) and
[compatibility authentication](../../crv4/runtime_compatibility.go) currently
require a profile-tagged source's spec to exceed the *current* reviewed maximum.
Raising that maximum to 471 can therefore make the original immutable
profile-tagged 471 signatures undecodable. Migration must preserve their exact
historical encoding/provenance without relabeling them or granting live replay of
old unfinalized transactions.

Final capture/replay must also authenticate the selected generation and overlay
namespace rather than applying the original root-only adoption rule. These
changes need their own source migration and qualification. Native one-edge
recovery must not silently authorize them.

## Deterministic qualification

The original proposal qualification covered exact prefix mutation, one permitted bridge,
wrong first epoch, one actual EMA fold, same-epoch replay, restart after the edge,
changed custody, full interior-terminal authentication, pending lifecycle,
separate runtime permission and signed-input/reservation cleanup. They use
synthetic data. The original evidence bundle preserves that baseline unchanged.

Focused validation uses Go 1.26.6, `GOMAXPROCS=2`, `nice -n 15`, `-p=2`, and
`-count=1`; the private workspace is in the evidence directory. Logs there retain
the complete result. Validator normal passed in 12.391 seconds, validator race
passed in 69.643 seconds, simulator normal passed in 49.404 seconds, and simulator
race passed in 167.130 seconds. This validates the current refusal and adoption
boundaries. The [implementation qualification bundle](evidence/FINAL-3-R48-native-recovery-implementation-20260926/README.md)
records the new launcher, exact-approval, early-sealed-source, custody and restart
regressions. Its focused validator normal/race suites passed in 13.710/85.100
seconds; simulator normal/race passed in 91.708/241.275 seconds.

The implementation's completion gate must add deterministic red/green tests for
both native recovery and each newly admitted boundary: a real generation-2
source-role config; both launch/restart projections; absent/foreign/tampered
owner approvals; wrong generation or current plan; changed config, prefix, EMA,
marker or inode; missing intermediate terminal; missed N; pending/partially
published N across restart; N+2 refusal after successful N; independent terminal
settlement cleanup; and the exact old runtime471 profile after a reviewed runtime
migration. Normal and race runs must pass without weakening existing negative
tests or final acceptance assertions.

## Next-run sequence

1. Preserve the sealed R47 result and signed invalidation. The run is
   `20260926T112551.425107945Z-release-1.0`; its original signed source is
   `campaign-attempts/release-1.0.recovery.47.evidence.json`. Its observed boundary
   is partial and failed; the validator source still needs exclusive recapture.
2. Qualify the separate recovery implementation and runtime-archive decision.
   Under the authorized stopped-topology path, freeze and recapture the selected
   generation-2 sources, all required settlement history and native schedule.
3. Select N and emit the complete canonical subplan; review its exact new hash
   and scope. Only then apply the owner-signed one-edge selection. The current
   setup plan and source-role hashes alone do not satisfy this approval.
4. Launch the qualified successor with its exact child request pins and unchanged
   state. Verify strict startup, a real finalized/applied N source, exact EMA fold,
   retained prefix, restart behavior and N+1 continuity. Verify positive eligible
   pool weights, actual automatic emissions/stake growth and a nonzero timely
   capture before counting fresh R48 financial coverage.
5. Run every ordinary terminal/public/native/conservation/archive/final assertion.
   Preserve all failed predecessor evidence. Final success remains conditional on
   those checks and the separately approved historical-runtime treatment.

## Offline implementation and exact review workflow

The isolated implementation adds `native-history-recovery`, a distinct provisional
child decoder, a separately owner-signed immutable plan, and launcher admission
after generation/source-role selection. It leaves the original strict adoption
API and historical-runtime catalog unchanged. Dry-run takes the existing
deployment lock through a read-only descriptor, requires a stopped supervisor,
validators and restored perturbations, authenticates the sealed predecessor and
the actual native sources, and prints canonical review data without writing live
provenance or runtime observations. Apply creates only new immutable plan, child
request and signed receipt files; it has no transaction executor.

The sealed predecessor may be an interrupted partial interval. Admission requires
the exact owner-signed invalidation, a completed failed result with matching
identity, observed boundary, window and unchanged fault snapshot, and consistent
failure counts. A partial interval must retain the failed
`acceptance_interval_observed` assertion. Historical active/pending faults and
cleanup after its last observed block remain visible; current perturbation
restoration is checked separately before recovery publication.

After the qualified executable and stopped source have been pinned, the exact
command shape is:

```sh
$R48_DRIVER native-history-recovery --config "$R48_CONFIG" --state-dir "$R48_STATE" \
  --provisional-resume --plan-hash "$R48_BASE_PLAN" --owned-rpc-authority "$R48_RPC" \
  --native-history-recovery-source "$R48_STATE/campaign-attempts/release-1.0.recovery.47.evidence.json" \
  --first-native-epoch "$R48_FIRST_NATIVE_EPOCH" --format json > "$R48_REVIEW_PLAN"
```

`R48_FIRST_NATIVE_EPOCH` must be selected from a fresh finalized native schedule
with measured startup time available. The reviewed plan includes its actual
`plan_hash`; capture does not constitute permission to apply it. An exact-hash
approval is still required for:

```sh
$R48_DRIVER native-history-recovery --config "$R48_CONFIG" --state-dir "$R48_STATE" \
  --provisional-resume --plan-hash "$R48_BASE_PLAN" --owned-rpc-authority "$R48_RPC" \
  --apply --native-history-recovery-plan "$R48_REVIEW_PLAN" \
  --native-history-recovery-plan-hash "$R48_RECOVERY_PLAN_HASH" --format json
```

The returned receipt path and SHA-256 select the recovery on the separately
authorized provisional resume using `--native-history-recovery-handoff` and
`--native-history-recovery-handoff-sha256`. Retained restarts verify the same
signed plan and exact child pins from the supervisor. A missed unused N fails
closed; recapture and a newly reviewed plan are required to select another N.
The original prefix is never rewritten and no successful native1691 is created.

This does not qualify a final archive or satisfy the economic gate. The exact
selected configs retain operator concurrency 4. The observed completed usage
shortfall and any proposed concurrency change require a separate source/config
migration and readiness review. After native recovery, observe signed lagged
root/artifact availability, exact deposit compliance, positive pool weight from
an uncontrolled eligible active validator, native emission and vault-owned stake
growth, then a timely nonzero vault capture. Keep a failed gate as a failed gate.

## Separate campaign timeout migration

The composed successor driver rejects the old harness values
`request_timeout_milliseconds: 10000` and
`maximum_p99_latency_milliseconds: 15000`. The user-required successor values
are 60000 and 60000. This changes the harness ConfigHash and needs a separate
exact-field owner-approved migration, while preserving both selected validator
config files, generation-2 coordinator/client namespaces, source-role receipt,
policy, custody, and operational route. It does not authorize a concurrency
change.

Current retained-generation readers require equal source/current ConfigHash;
current native recovery also binds the R47 terminal to its actual source plan and
ConfigHash. Neither comparison may be relaxed based on ancestry alone. An
authenticated migration resolver must load the real archived old configuration,
recompute its old hash, verify the exact two-field transition and owner receipt,
and return both actual source/current authorities. Native recovery can then bind
that receipt explicitly in a separate integration change. Until that receipt and
adapter exist, a changed harness configuration fails closed.

## Current read-only readiness observations

At finalized block **8,091,767** (`0x28d569cb11b9d97089da28a4e529fe9406955179c400b0c951be21f45f2960cc`),
the exact native schedule reader used by capture authenticated both selected
467/1/1 config anchors against runtime471/1/1 through the consumed-interface
profile. UIDs 254/255 satisfy native non-self stake and permit; current native epoch
is 1694 while both retained applied sources remain 1690. Config, intent and EMA
digests remained unchanged. This diagnostic is not exclusive capture and does
not choose N; the later approved N must exceed the newly captured current epoch.

At finalized block **8,091,801** (`0xdfb5ccefbc5da14a10ab140afe2eb56113d76a14ec5cafa6a8a3690601386f21`),
selected validator 2's unchanged config authenticated current settlement 654/source 653
payout commitments for pools 3/4, both committed at 8,091,580. Both public signed
artifacts passed the existing exact deposit audit: operator 1 required and observed
208,721,995 rao for 9,338,064 bytes; operator 2 required and observed 207,305,833 rao
for 9,274,706 bytes. Validator 2 controls neither operator. This is a later prerequisite
observation, distinct from native1690's failed source648 audit. Fresh stats and
quality, actual positive weights, subsequent emission and nonzero vault capture
remain separate observation gates. These facts must be rechecked at cutover.
