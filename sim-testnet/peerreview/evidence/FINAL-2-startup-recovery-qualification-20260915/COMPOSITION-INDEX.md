# Startup recovery incremental qualification composition

This index composes only scoped receipts.  It does not represent a restarted full gate.

| Scope | Mode | Result and membership | Source / receipt |
| --- | --- | --- | --- |
| `crv4` affected runtime scope | normal | 21/21 pass; event and source/binary fences true | `23f391d7`; `r1/terra/normal/crv4` |
| `crv4` affected runtime scope | race | 21/21 pass; event and source/binary fences true | `23f391d7`; `r1/terra/race/crv4` |
| `sim-testnet` affected union | normal | 75/75 pass; event and source/binary fences true | `23f391d7`; `r1/terra/normal/sim-testnet` |
| `sim-testnet` affected union | race | 75/75 pass in 339.86 s; event and source/binary fences true | `23f391d7`; `r1/terra/race/sim-testnet` |
| validator retained scope | normal | 43 retained passes plus the two repaired controls pass | `23f391d7` retained receipt + `2510ef7` exact-two receipt |
| validator retained scope | race | 43 retained passes plus the two repaired controls pass | `23f391d7` retained receipt + `2510ef7` exact-two receipt |
| miner retained scope | normal | 12 retained passes plus final release-manifest assertion pass | `2510ef7` retained receipt + `53b0e950` final receipt |
| miner retained scope | race | 12 retained passes plus final release-manifest assertion pass | `2510ef7` retained receipt + `53b0e950` final receipt |

The final miner receipt is `/mnt/data/sn-testnet/qualification/runtime459-miner-final-20260915-r1/terra`: normal binary `08878b416a240caa12693430a24dc5a815e94a1a70ab7c5eb26a85dbba229ebe`, race binary `1fe11533d50caee28d4f655dc8bc66824da3b56f5ad919846ef79d0f5ac836a1`, exact selector `^TestFleetRuntimeArtifactMatchesReleaseManifests$`, one selected top-level pass and one package-pass event in each mode.  Its `SHA256SUMS` is `a01a35b90c6c82bfbe13d738f389048085a3439dba0a30aa273a5108b807fac4`.

## Causal controls

All causal sources were fenced clean and every binary/input fence held.  Each test process exits 1 because it deliberately contains the listed expected failures; each capture's qualification exit is 0 after exact event validation.

| Cohort | Source | Expected and observed |
| --- | --- | --- |
| native decoder and seven-artifact bound / `crv4` | `45e65d3c` | 4 fail, 3 pass |
| original 458 companion history / `sim-testnet` | `93616ee8` | 3 fail, 1 pass |
| configuration identity / `sim-testnet` | `35a8f20d` | 1 fail, 1 pass |
| current artifact / miner | `f3ef2c0` helper-only replacement of `3ebb6bf` | 2 fail, 1 pass |
| current artifact / validator | `3ebb6bf` | 1 fail, 0 pass |
| current artifact / `sim-testnet` | `3ebb6bf` | 1 fail, 0 pass |

Total causal membership is 12 expected top-level failures and 6 expected top-level passes across six normal-mode binaries.  Receipts are under `r2/terra/causal/`.

## Retained pre-fix outcomes

The `23f391d7` validator normal and race bodies each retained 43 top-level passes and the same two fixture failures before the two-control `2510ef7` correction.  The original `23f391d7` miner normal and race compilers retained the missing helper-type failures; `2510ef7` then retained 12 miner passes and the stale release-manifest expectation failure before the `53b0e950` one-root correction.  The initial current-artifact f3ef simulator and validator compilers exited 1 before body admission because their new worktree lacked sibling dependency links.  The exact 11-link layout was then verified; two follow-on f3ef groups were intentionally stopped before terminal when the original `3ebb6bf` source fence was selected, and the accepted current-artifact simulator and validator receipts were compiled from that clean source.
