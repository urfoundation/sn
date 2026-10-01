# Owner-trim action qualification and optional proxy storage repair

The first isolated action candidate was
`403360920027c1b0d9e67f615d63a75a294fc9e7`, tree
`950d392dd3cf4471e441b6d9bd00b480c24f96ba`, on top of qualified readiness
SN `e35771ec47aa8d486ce2af03a83eb3edace9cbde`. Sol medium's focused normal
and race runs each failed
`TestOwnerTrimAuthorityCurrentPruningBoundaryNeverGrantsEnforcement` with
`state_getStorage: missing result`. The original source, failed receipts and
first handoff remain unchanged. The first selector includes 20 newly added
roots and two pre-existing command roots, for 22 selected roots total.
The sealed R1 census reports 21/22 focused and 225/226 expanded roots passing
in both modes, with the same sole failure. R1 remains unqualified.

The root cause is the new production `Proxy.Proxies` call site. Its synthetic
fixture already emits an explicit JSON `result: null` for an absent storage
key. The ordinary RPC call requires a concrete result and therefore rejects
that valid absence. The repair uses the existing explicit optional-storage
reader after authenticating the proxy metadata, map key and default tuple.
It retains the finite read budget and 1 MiB reply limit. The shared RPC
decoder and fixture are unchanged: a missing result field, wrong JSON type,
RPC error or transport failure cannot become a storage absence witness.

Three additional top-level regression roots cover the actual current-window
path: null versus stored empty tuple with distinct retained evidence; omitted
and wrongly typed results at exactly the proxy read after a valid census and
nonce; and present unresolved proxy rows, including nonzero deposits and
wrong tuple lengths. An adjacent source audit found all other optional storage
readers already use explicit nullable storage profiles. No blanket nullable
RPC policy was introduced.

Finished failure evidence under
`/mnt/data/sn-testnet/qualification/mg08-owner-trim-phase-20260929/`:

| Receipt | SHA-256 |
| --- | --- |
| `focus-normal.json` | `49a2a6373fc72682ffb967307711233b3c328a24d6fc6f24cbc501b92f5dc04e` |
| `focus-race.json` | `244f783c1fab0cee81a384520adbd14e9525b66c202c14181d046fab561577d0` |
| `expanded-normal.json` | `567927e7567e3dcce8c99cc808894408e5e23de226595415db6d2bb3e88ba54c` |
| `astra/HANDOFF.md` | `3bd4418fdbc4afb03dc1fcb632f2be83475dcc844d70a37d0ae897db182adf2b` |
| `R1-FAILURE-CENSUS.md` | `66779b1236ea9e3418a4c8df2cefc3cf15aa6cd1323b648c91cdff12e10f66ee` |

## Qualified successor and root integration

Shared SN was fast-forwarded from `e35771ec47aa8d486ce2af03a83eb3edace9cbde`
to the exact qualified successor `ce5673052ed51c6cfea2e74a57b603499bc30d14`,
with tree `86eb12c008c1ca8c6a576e9667c8c23c25feb129` verified before these
documentation updates. No merge resolution or source edit changed the qualified
candidate. The original isolated worktrees and failed evidence remain intact.

The exact source, compile-only checks and unchanged physical module pins are in
`/mnt/data/sn-testnet/qualification/mg08-owner-trim-phase-r2-20260929/astra/HANDOFF.md`.
SHA-256: `a7ec337c9e231a018d8c7a164aeb903cf47b2dd35a69692024514bebfb07e143`.
Sol medium's sealed
[R2 receipt](/mnt/data/sn-testnet/qualification/mg08-owner-trim-phase-r2-20260929/SOL-RESULT.md)
has SHA-256 `c03cc124535d4bc20153b29fd58066339b3fb9818dde486dbce70786d0fcbaae`.

| Exact scope | Sol result |
| --- | --- |
| Focused | 25/25 roots pass normal/race: 23 new roots relative to e35771ec plus two pre-existing command roots; no child events, skips or race reports |
| Expanded normal | 229/229 roots pass; 79 existing child events pass; no failures or skips |
| Expanded race | Exact 229/229 named roots pass in the recorded union; nine disjoint shards exit 0; no failures, skips or race reports |
| Causal controls | All eight requested controls fail their intended assertions; one extra null-absence flag control also fails as expected |
| Static checks and fences | Vet, formatting and diff checks pass; all nine physical heads/trees/clean statuses and before/after module graph match |

The redundant unsharded `race-chain` process passed 29 roots and was
intentionally terminated with exit 143 after the complete race union existed.
The three disjoint remaining bootstrap shards exited 0. No successful terminal
exit is claimed for that redundant process. The
[per-root race union](/mnt/data/sn-testnet/qualification/mg08-owner-trim-phase-r2-20260929/race-union-census.json)
has SHA-256 `54b5ed9cf144b4bcc565b3c713fef046a7b7edf9d537e2662e11b6d67831c894`.

Controls expose the original non-nullable call, fabricated omitted-result
absence, inclusive immunity boundary, absent enforcer treated as authority,
wrong native event association, changed protected generation, skipped census
continuation and bypassed independent action approval. Each mutant was isolated
and restored; positive roots passed before mutation. Astra ran no behavioral
tests during implementation or integration.

The external modfile SHA-256 remains
`42733c0a9450284fa8eaffd97b316b179746bb867bdbb86951dff5ceebab6370`;
both module graph snapshots hash to
`870de7451c6f298d5daa7e4b4d9b09850f06cee9b78495da1798831af4ae4d74`.
Both Sol source fences hash to
`7bf3514209c0dd431ed19612d2e6b320c9e37ea320e112725f98fc6befd05c00`.
Physical server `b7c8c743`, Connect `b163f9dd` and SDK `516521fb` are retained;
tracked SN module files are unchanged. These source and module checks qualify
the local composition, not a different image, dependency graph or deployment.

## Remaining live gates

This repair grants no signing or submission authority. Original v3 custody,
the independent action approval, original nonce/era/allowance and all protected
generation requirements remain intact. The absence of a future window enforcer
still blocks production execution; no pending best-effort risk-policy choice
is inferred. Financial finality remains separate from generation reconciliation
and explicit old-miner residuals. MG-08 and live launch acceptance remain open.
