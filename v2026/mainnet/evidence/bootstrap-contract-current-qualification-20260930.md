# Current bootstrap fields: scoped qualification

The bounded read-only increment is qualified at source
`2b87b13300f1b78e82026aa90659b99c4083f60a`, tree `a07a91771c1e9b0a211e8ac0fba53059a4effb0d`,
based on qualified receipt integration `fa73fc9cf22a680106b61d4c61ec528286c70caf`.
Astra max authored implementation, deterministic fixtures and causal patches and
performed formatting, compile-only and vet checks. Sol medium independently ran
every behavioral test on the exact frozen source.

The [command](../BOOTSTRAP-CONTRACT-CURRENT.md) first reauthenticates the eight
retained original receipts and then compares five distinct accounts with the
exact original bootstrap field profile at one finalized native/EVM mapping.
Every current code, getter and storage read uses the same canonical block-hash
selector and its own bounded retry budget. The selected mapping is reread and
the original inclusions and selected snapshot must remain canonical through a
later head; ordinary head advancement cannot relabel the state snapshot. Local
signed declarations and all eight original records are checked before output.

The report labels current state `owned-rpc-assertion`. It checks exact code,
proxy implementation/admin/beacon/owner and initializer state, original sole
policy and EVM policy-start block, clock at the selected EVM height, reserve/vault
links, original zero activity and the evidence immutable domain. It lists the
actual getters/slots. These are neither independently verified account-storage
proofs nor evidence of complete storage, absent hidden mappings or history.
The native header and Frontier-committed EVM header checks retain the owned-RPC
finality assumption and grant no current runtime source-to-Wasm approval.

All three focused and sixteen adjacent roots passed both normal and race modes:
38 positive root executions with exact top-level census, package PASS and exit
zero, without skipped roots, race reports, panic, timeout or build/setup failures.

| Group | Roots per mode | Normal package | Race package |
| --- | ---: | ---: | ---: |
| Focused light | 2 | 6.000 s | 43.227 s |
| Focused full graph | 1 | 42.566 s | 285.704 s |
| Adjacent light | 15 | 25.164 s | 191.649 s |
| Adjacent historical graph | 1 | 42.540 s | 294.285 s |

Two light focused roots distinguish current EVM height from native/original
heights, retain the original policy origin, refuse snapshots before deployment
and reject changed or absent original read custody and unsupported send options.
One representative full fixture runs all eight actual pinned deployments locally,
then checks the public current-state command under advancing finality. It covers
all five accounts' runtime mismatches, proxy owner/implementation/admin/policy/
clock/activity, reserve/vault links, evidence domain and foreign pointer; exact
selectors, renewed deadlines and changed final mapping also have deterministic
assertions. Original custody and the eight-write census remain unchanged.

A local VM call supplies an exact expected evidence pointer in a later state.
It supplies no qualifying anchor receipt or Safe history and asserts only the
current pointer observation. Adjacent roots cover original signed role/custody,
historical receipts and scan floor, original/snapshot canonical continuity and
exact native/EVM mapping integrity.

All six normal controls and exactly two selected light race controls were causal.
`evm_clock` and `predeployment_snapshot` ran both modes; `runtime`, `getter`,
`storage` and `final_mapping` ran normal only. The full positive fixture supplies
race behavior for those heavy paths. Every control reached its assigned named
assertion, exact selected root/package FAIL and exit one without a setup, build,
panic, timeout or race confounder. Frozen source, tests and mutation patches
remained unchanged; no behavioral source fix or oracle correction was required.

The first unfrozen author compile caught a misspelled generated fixture binding,
corrected before source freeze. Its log remains at
`/tmp/bootstrap-contract-current-author-compile-attempt1.log`; it is separate from
the final compile/vet and independent behavioral qualification evidence.

Sol's immutable evidence directory is
`/home/by/urnetwork/temp/bootstrap-contract-current-validation-2b87b133`.
Its final `manifest.json` SHA-256 is `342717a6162be19fc4ac17846161796e538db188bdf9f502104c1316342e3b93`;
the 23-entry `SHA256SUMS` SHA-256 is `5c18817f99e0239d2ea9b598fdedcac4b290fb3eb66ce9d3ca407982c5d4b7ed`.
The manifest binds terminal raw logs, exit statuses, exact selectors/census,
control results and complete source/dependency fences. The independent author
read-only audit is `/tmp/bootstrap-contract-current-final-audit.json`, SHA-256
`7dacee1b4b8fc868469e40cfdf589d5274661d50ef106fce84cbe82ad2e664c5`; it reruns no behavioral test.

The immutable handoff is `/tmp/bootstrap-contract-current-handoff-20260930`.
Its 14-entry static `PAYLOAD-SHA256SUMS` SHA-256 is
`09c2cbcdb214b88e19b443bba0ac391d8d3b4ae44c1d446e96ba8df19b9691b2`.
Every exact mutation compiles and passes vet in a separate author checkout,
restored clean to the frozen source. The additive 28-entry `SHA256SUMS` SHA-256 is
`4ce36ff7040630ef34cd9d46ee761081e7358b05508cab1a81053ea63fb1cb19`.
Both handoff seals and every final evidence checksum were independently verified.

The fence records `go version go1.26.6 linux/amd64`, unchanged go.mod
`2a948e40658bb403c440ea649140b5ffae53d8b338b0dfc61c90625c708dbc5c`, go.sum
`2a8d74108e1c331b4b9ebdd8595c02f4b8a98e76f51fedf4c7498c7b41d974ca` and module graph
`19db0607a41328b8dc09c13292e40a0ee7024650c80ffc3e85e9a870ae225f9b`. All six local replacement repositories are clean
at their exact recorded commits/trees; the Safe SDK proof oracle is unchanged.
Integration preserves every qualified non-Markdown byte, including all Go/test,
module and pinned contract files. Only documentation differs. Earlier historical
receipt and role qualification notes remain immutable.

This qualifies the original five-account, zero-activity bootstrap profile only;
new policies, operators, balances or governance transitions are refused rather
than silently generalized. An unset or exact expected evidence pointer is reported
without establishing anchor history. Complete storage, Safe authority, full
installation and activation remain unverified; all original pending phases stay.
Live chain identity, anchor/operator/role evidence, service activation, automatic
runtime compatibility and separate Safe current-policy approval/qualified public
route installation remain open. The original Safe history statement is retained
and unproven. No live RPC, production signing, transaction, public Safe capability
or activation is introduced. This report alone grants no transaction or service
authority.
