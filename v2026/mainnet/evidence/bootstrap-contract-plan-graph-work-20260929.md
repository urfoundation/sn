# Contract projection graph work correction

Exact parent: `425b897e5506e83df15c982ac78f529889c6fba6`. The isolated source
branch is `codex/mainnet-evm-plan-graph-work-20260929`, at
`/home/by/urnetwork/temp/sn-mainnet-evm-plan-graph-work-20260929`.
The containing commit is a compile-only candidate. Sol owns all behavioral
qualification; no new speed, normal, race or control pass is claimed here.

## Observed problem and scope

The retained normal evidence logs measured 662.22 seconds for
`FencesSevenPredecessorCheckpoints`, 402.56 seconds for
`RechecksBoundVaultAtCurrentAndInclusion`, and 336.82 seconds for
`RechecksReserveProxyAndImplementation`. These are complete roots, not single
CLI command timings. The checkpoint root contains fourteen independent full
graph deployments and recoveries. The other two roots perform respectively
63 and 54 separately injected current/pending/inclusion observations through
the full command dispatcher.

The 35-minute checkpoint race timeout stack was in JSON encoding under
`rootObjectHash` and recursive `validateSelection`, while preparing another
genuine ancestor graph. The 30-minute bound-vault stack was in generic SHA-256
over a 106,746-byte approval configuration on that same recursive path. The
30-minute reserve/proxy stack was JSON-decoding a full config in recursive
`copyEvmCreatePlan`. Read-only process observations showed these race binaries
using roughly one CPU core each. No fixture sleep or retry loop explains those
three sampled stacks. The later checkpoint 60-minute package timer also expired
without a root assertion; its stack was in an ordinary ancestor receipt RPC.
These are retained timeout samples, not a statistical CPU profile or a claim
that every second was hashing.

Raw evidence remains under:

- `/mnt/data/sn-testnet/qualification/sol-evidence-create-20260929/race-shard1-checkpoint35m.log`
- `/mnt/data/sn-testnet/qualification/sol-evidence-admission-fix-20260929/race-shard5-bound-vault30m.json`
- `/mnt/data/sn-testnet/qualification/sol-evidence-admission-fix-20260929/race-shard5-reserve-proxy30m.json`
- `/mnt/data/sn-testnet/qualification/sol-evidence-admission-fix-20260929/race-checkpoint60m.json`
- The same latter directory's `normal-shard2.json` and `normal-shard6.json`.

Source analysis identifies production work expansion. The selected action-seven
graph has fourteen distinct projection objects: seven separately constructed
reserve objects, six intermediate actions, and the selected action. The previous
owner copy unfolded every shared predecessor path into 128 objects, each with a
full JSON config copy. One action-seven `validateSelection` made 254 full config
hashes. The seven selection steps together made 494; custody admission and each
historical receipt audit repeat validation again. The recurrence for the old
hash count is `H(n) = 2*n + sum(H(1)..H(n-1))`, with `H(1)=2`.
This affects the actual `plan`, `apply` and `resume` paths, not only tests.

The correction keeps all existing CLI cases and fixture assertions. Raising
timeouts alone has no measured safe bound on this host; the 60-minute checkpoint
failure is retained. The first successor timings must be measured independently.

## Product invariants

Only `evm_phase.go` and `evm_action.go` change product code:

- Each synchronous selection-validation call owns fresh pointer-keyed config
  hashes and successfully validated projections. Shared objects are checked
  once in that call. The next call recomputes all facts. There is no process,
  owner, action-index or approval-hash cache.
- Every original validation guard and diagnostic remains. A static comparison
  normalized the memoized hash/recursive-call spellings back to the parent and
  found the original guard body byte-identical. Distinct objects selecting the
  same action index still receive independent config and structural checks.
- The owner copy preserves source-object sharing inside a newly allocated
  private graph. It does not merge different source objects, including the
  separate reserves. All config/action-address pointers, runtime/getter/storage
  slices and constructor/binding pointers still detach from caller state.
  Invocation-specific predecessor records are still cleared and loaded under
  their held custody locks. The copy memo exists only during construction.
- `rootObjectHash`, JSON field definitions, SHA-256, approval bytes/signatures,
  plan/result/journal encoding and schema versions are unchanged. Receipt,
  ancestry, RPC, nonce, attempt/funding, signing and owner-gate paths are unchanged.

For the ordinary selected graph the intended work is fourteen config copies and
fourteen distinct config hashes per validation, with all graph-edge comparisons
retained. A caller-created expanded graph still checks all distinct objects;
memoization cannot treat a conflicting projection as an already approved index.
No artifact, binding, source lock, module file, root branch or active Sol checkout
was edited. No live action, Safe-inner operation or behavioral test was run by
the implementation owner.

## New roots and preserved coverage

Five new top-level roots use prefix `TestEvmCreatePlan`:

1. `CopyPreservesBoundedGraph`: fourteen source objects stay fourteen private
   objects, shared ancestors retain identity, distinct equal-index objects do
   not merge, invocation records disappear, and exact projection/approval JSON,
   signing bytes and hashes remain equal.
2. `CopyOwnsEveryProjection`: edits to caller data and a second owner's graph do
   not change the first owner's config, address pointers, constructor pointers
   or postcondition slices; its selected domain remains valid.
3. `ValidationRejectsDistinctConflictingProjection`: after a valid top-level
   vault is visited, a different nested vault at the same action index must still
   reject a changed approval signature or a forbidden descendant.
4. `ValidationRechecksMutations`: a later call rejects changed approval and
   constructor data at the same object addresses; restoring bytes restores
   admission.
5. `ValidationBoundsSharedWork`: the same valid values are checked as a shared
   fourteen-object graph and a JSON-expanded 128-object graph. Allocations per
   call for the shared graph must be less than half the expanded graph. The
   relative allocation bound avoids elapsed-time assertions and logs both
   measurements. Both graphs execute all their legitimate validation checks.

All twenty-eight `TestEvmEvidenceCreate*` roots and their existing fixtures are
byte-identical to the parent. The new graph roots select real release-backed
fixtures without executing the EVM or ancestor RPC sequence. Static source
inventory contains 585 top-level mainnet roots, five more than the exact parent.

Sol should first run `^TestEvmCreatePlan` normally and under race, then the three
original long evidence roots unchanged, followed by the evidence union and
adjacent `TestEvm(Create|VaultCreate|CoordinatorCreate|EscrowRegister|ProxyCreate|ReserveLink|VaultLink|PhasePreview)`
and appropriate full/static gates. Prior positive evidence belongs to its exact
parent, not this new product commit. Retain old timeout logs, exact selectors,
source/module fences and per-root JSON timing for any speed comparison.

## Isolated causal controls

Establish the unchanged positive root first, apply one mutation per isolated
checkout, and retain the exact patch and intended assertion. None below was
applied or executed by the implementation owner. A compile failure, unrelated
ancestor failure or timeout does not discriminate the requested boundary.

| Control | Exact mutation | Root and expected failure |
| --- | --- | --- |
| G01 | In `copyEvmCreateProjection`, change `if copied, ok := copiedPlans[source]; ok` to `...; ok && false`. Keep map writes and clone logic. | `CopyPreservesBoundedGraph` must report 128 copied objects instead of fourteen. |
| G02 | In `evmSelectionValidation.validate`, gate the successful-visit lookup with `&& false`; in `configHash`, gate its `ok` lookup with `&& false`. Keep both map writes. | `ValidationBoundsSharedWork` loses the twofold allocation advantage because both graphs traverse the same expanded paths. |
| G03 | Replace only the lookup in `evmSelectionValidation.validate` with `for previous := range self.validatedPlans { if previous.ActionIndex == plan.ActionIndex { return nil } }`. Keep pointer-keyed config hashes. | `ValidationRejectsDistinctConflictingProjection` still rejects its config fault, then incorrectly accepts its distinct descendant fault. |
| G04 | Move the `evmSelectionValidation{...}` initialization from the public wrapper into one package variable, and use that same variable for each wrapper call. Keep pointer keys. | `ValidationRechecksMutations` incorrectly accepts the changed vault approval using earlier cached child facts. This intentionally invalid control is isolated; no global cache belongs in the product. |
| G05 | Only inside `copyEvmCreateProjection`, replace `plan.Config = copyEvmPhaseConfig(plan.Config)` with `plan.Config = source.Config`. | `CopyOwnsEveryProjection` observes caller action/address edits through the copied config's shared slice. |
| G06 | In `evmSelectionValidation.configHash`, replace `rootObjectHash(plan.Config)` with `plan.Config.Plan.hash()`. | `ValidationRejectsDistinctConflictingProjection` wrongly accepts its changed-signature config fault: the shortened hash has lost the complete approved config identity. |

The existing evidence C01-C16/P01-P02/U01-U04 map remains at
`/mnt/data/sn-testnet/worktrees/sn-mainnet-evidence-causal-map-20260929/CONTROL-MAP.md`.
Its product guard expressions remain, but line numbers in `evm_phase.go` and
`evm_action.go` move. Keep its completed-control claims separate from successor
qualification; no redundant rerun is represented as new graph coverage.

## Static handoff

Gofmt and `git diff --check` passed. Normal `go test -c -p=2` compiled all mainnet
product/test source with no behavioral execution. The log, unexecuted binary
and external `compile.mod`/`compile.sum` are under
`/mnt/data/sn-testnet/worktrees/sn-mainnet-evm-plan-graph-work-20260929/`.

The physical dependencies remain under
`/mnt/data/sn-testnet/qualification/mg08-ur-offline-admission-20260928/source`:

| Module | Commit |
| --- | --- |
| Connect | `b163f9dd9ac374942fe97331f26631248a9c1f81` |
| Server | `5dc11761373580b5a0ddd9757cd6e4eb94140e27` |
| SDK | `516521fb16da46c9f4bff0b58221e1941694f616` |
| Proxy | `6204ae7df2a9868bbb3a7b61231917a36e4f5c9f` |
| UserWireguard | `85fb1ca4086fa5dbfcda526bec7a17a894e691b9` |
| Warp | `7498864c7cd3605aad3c43eabfab9008ed7f7228` |
| Glog | `892ade4a6be396b32ea82a550f243190b5992180` |
| Goidenticons | `325750b38314313dc5f44c880ab6f12f6c1ecb3c` |

The external replacement for SCTP points at Connect's pinned `sctp` directory;
the npipe replacement points at this candidate's unchanged `third_party/npipe`.
