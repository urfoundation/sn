# Mainnet steady cadence candidate — 2026-09-28

Status: source integrated; **component normal/race, vet and causal controls
complete; final release composition pending**. No live network, signing, deployment
or existing policy bytes changed. Base: `59817899cc11d0d3d6086ea3c3d7e52d14130fc6`.
Worktree: `/mnt/data/sn-testnet/worktrees/sn-mainnet-steady-cadence-20260928`.

Source `e1d6644cee4501c212afcb48850a2b8588eb044b` is integrated as
`a58878ea`; the four code/schema/test files compare byte-identically.

## Retained component results

Terra medium ran the exact selectors below at the sealed source. Evidence is in
`/mnt/data/sn-testnet/qualification/mainnet-steady-cadence-20260928/e1d6644c`.

| Scope | Normal | Race |
| --- | --- | --- |
| Protocol | 11/11 passed; 0.028 s | 11/11 passed; 1.274 s |
| Validator | 13/13 passed; 3.200 s | 13/13 passed; 17.958 s |

All four package exits and `go vet ./protocol ./validator` exited zero. The
source remained clean at `e1d6644c`; before/after module-path output matched.
Physical replacement revisions were recorded **after** execution, with the
retrospective capture named accordingly. They are not a contemporaneous
pre-build content seal. This run consumed Connect `358cefae` and server
`0633780c`, rather than the earlier composed-release dependency family.
Preserve these passing component results and perform only affected interface
checks when composing the actual final release; this receipt does not attest
its future dependency bytes or deployment.

The retained result-stream SHA-256 values are:

| File | SHA-256 |
| --- | --- |
| `protocol-normal.jsonl` | `098f3274647ebbdf56b2f74f1c59d812a61d6188ca20059816a45e68aa3a46e9` |
| `protocol-race.jsonl` | `77314037737f70f5e67a4a3c2e715accb914c474671be68769216fa770fdbc6b` |
| `validator-normal.jsonl` | `f68d4f072c1d27fafd86c02983c697d110a232453916be13aa63e7216d53b778` |
| `validator-race.jsonl` | `cb5adec7f09f43a2fac413b12cc2e52496311193ca3351e54c226e10971fcb58` |

The three causal overlays also completed in both modes. Their six executions
reached the intended assertions; none is counted from a setup or compile error.
Evidence is retained separately in
`/mnt/data/sn-testnet/qualification/mainnet-steady-cadence-20260928/e1d6644c-causal`.

| Control | Intended assertion | Normal / race |
| --- | --- | --- |
| Exact predecessor policy parser | `steady mainnet cadence cannot be committed from epoch zero` | Both reproduced |
| Exact predecessor through the signed public-config loader | `steady mainnet policy cannot enter the actual production loader` | Both reproduced |
| Omitted steady-window equality | `initial-epoch acquired a steady mainnet policy hash` | Both reproduced |

Five invocations have retained shell exit 1. The original public-config normal
wrapper was orphaned before recording its shell exit; its JSON stream retains
both the named assertion and terminal failed test/package events. The separate
`old-policy-public-config-normal-shell-exit-limitation.txt` records that limit.
Its completed test body was reused without inventing a shell result or rerunning
it. Source and module-path fences remained unchanged.

## Cause and bounded correction

The planned coordinator constructor starts its UR settlement period at 50,400
blocks from epoch zero. Shared `protocol.Policy.Validate` required both a
positive accelerated count and a production period greater than the initial
period. No valid policy could express the intended initial state. The two
validator cadence readers were correct for the available policy representation;
changing their acceptance predicates would add no assurance.

`after_accelerated_epochs: 0` now denotes explicit steady mainnet cadence. All
four settlement/production windows must match, every existing geometric bound
still holds, and the mainnet period must be exactly 50,400. Positive transition
counts retain the prior longer-production requirement. Testnet cannot use zero.
A later independently approved steady policy may retain this representation;
only fresh installation separately requires `effective_epoch: 0`.

The public JSON schema permits zero only under mainnet with equal 50,400-block
periods. Its description points to shared Go admission for the remaining dynamic
cross-field equality; JSON schema does not silently invent window defaults.
Canonical serialization is unchanged. Old accelerated mainnet policies are still
readable, and existing testnet policy hashes/transitions remain unchanged.

## Authored deterministic coverage

- Four protocol roots cover parser/hash round-trip at epoch zero, later steady
  approval representability, every changed window, false/testnet transitions,
  existing accelerated history bytes, and the public schema mode.
- Three validator roots independently sign and load the actual schema-3 public
  production config, bind its exact approved producer artifact, read a real
  finalized/hash-pinned HTTP coordinator snapshot, and pass both unchanged
  `validatePinnedChains` and `validateReleaseDecisionChainV2Policy` at epoch zero.
- Actual ABI-encoded policy hash, all four windows, retention, binding horizon
  and cap mutations are refused by both consumers. Recomputing a policy hash
  without replacing the external complete-config signature remains refused.
- These are synthetic policy/runtime identities. They do not prove live runtime,
  economic acceptance, complete producer startup, or the installer's whole graph.

The source audit covered `AfterAcceleratedEpochs` consumers in protocol,
validator, simulator and server. The simulator's positive-count testnet scheduler
is unchanged; server callers consume shared `protocol.Policy` parsing/admission.
The installer author owns the explicit fresh-install profile and full-policy
approval integration. No mainnet graph files were changed in this candidate.

## Terra qualification handoff

Use the physical cache and a capture-specific physical temporary directory;
`GOWORK=off GOMAXPROCS=2 GOCACHE=/mnt/data/sn-testnet/gocache`. Author compile-only `go test ./protocol ./validator -run '^$' -count=1`
passed (no bodies executed). Preserve source/module fences, enumerate nonzero root
counts, collect every selected normal/race result, and do not rerun unchanged
prior startup bodies for this candidate.

```sh
go test ./protocol -run '^Test(PolicyMainnetSteadyCadence|PolicyCadenceWindowsFailClosed|CheckedInTestnetPoliciesKeepCanonicalHashes|PolicyRejectsOverflowing|LoadPolicyCanonicalHash|TestnetRateAmendment)' -count=1 -timeout=180s
go test ./validator -run '^Test(ReleaseMainnetSteadyCadence|ReleaseRateAmendment|ProductionRuntimeConfigAuthenticatesExactCurrentArtifact|ValidatorEvidenceDepositAuditV2ProductionCadenceKeepsPinnedProfile)' -count=1 -timeout=600s
# Repeat the same exact selectors with -race.
go vet ./protocol ./validator
```

Causal overlay manifest:
`/mnt/data/sn-testnet/qualification/mainnet-steady-cadence-20260928/causal/manifest.json`.
It restores the exact base `protocol/policy.go` and requires both the protocol
round-trip and actual public-config/snapshot root to fail their named intended
assertions, normal and race. A separate bounded mutation omits steady-window
equality; the changed-window control must reject that implementation, also in
both modes. Compile failures or unrelated fixture refusals are not causal passes.

The remaining launch gates include composed installer qualification, actual
mainnet policy/approval and runtime identity, live activation and economic
observation. This source fix does not close MG-04, MG-08 or MG-10 wholesale.
