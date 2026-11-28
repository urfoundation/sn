# Original contract receipts: scoped qualification

The read-only historical admission is qualified at source
`c6b31fdb276a3ecaa622cd6dcc1897223078add1`, tree `212b3b01634ba6908c6bfac1b599b52638818a7c`,
based on `e21993536fc892aceb59019be1b5f3b6289aa087`. Astra max authored implementation,
fixtures and causal patches and performed formatting, compile-only and vet checks.
Sol medium independently ran every behavioral test on the exact frozen source.

The [command](../BOOTSTRAP-CONTRACT-RECEIPTS.md) holds original preparation and
eight action markers read-only, reauthenticates exact original native/EVM receipts
and historical code/storage/getter postconditions, and retains full original
record hashes and counted attempts. Each receipt receives its own finite read
budget. The initial ancestry snapshot, original inclusions and later checked-through
head remain distinct; every original inclusion and the snapshot must still be
canonical at the end. Normal head advancement is accepted. Each signed nonzero
EVM scan floor must be no later than the earliest original EVM inclusion.

All five focused and sixteen adjacent roots passed both normal and race modes:
42 positive root executions, exact top-level census, package PASS and exit zero,
with no skipped root, race report, panic, timeout or build/setup confounder.

| Group | Roots per mode | Normal package | Race package |
| --- | ---: | ---: | ---: |
| Focused light | 4 | 7.997 s | 58.184 s |
| Focused full graph | 1 | 41.783 s | 294.016 s |
| Adjacent | 16 | 29.298 s | 200.385 s |

The full-graph fixture executes all eight pinned contracts locally using synthetic
signed authority and genuine native/EVM inclusion machinery. It exercises the
public read-only command, renewed per-receipt deadlines, moving canonical finality,
missing/swapped receipts, observed local replacement and unchanged journals and
write count. Light cases distinguish native and EVM heights and separately change
an original inclusion and the pinned snapshot while a later head remains unchanged.
Adjacent coverage includes signed contract-role declarations, original custody,
completion postconditions, predecessor lineage, ownership and restart authority.

All five normal controls and exactly three selected light race controls were
causal. `scan_floor`, `final_original_inclusion` and `final_snapshot` ran both modes;
`exact_historical_receipt` and `local_custody_checkpoint` ran normal only. The full
positive fixture supplies race behavior for those heavy paths. Every control
reached its assigned assertion, exact selected root/package FAIL and exit one,
without a build, setup, panic, timeout or race confounder. Source, tests and
mutation patches remained unchanged throughout qualification.

Sol's immutable evidence directory is
`/home/by/urnetwork/temp/bootstrap-contract-receipts-validation-c6b31fdb`.
Its final `manifest.json` SHA-256 is `0d28a5705350c5177cce6fe6c731cd57784027fbe970dc476b1fbc849216e5c6`;
the 21-entry `SHA256SUMS` SHA-256 is `cbe9b742d669dd026c102897fae39761995279cd0981e8a04422b4cbc6ac3b58`.
The manifest binds terminal raw logs, exit statuses, exact selectors/census,
control results and the complete source/dependency fence. The independent author
read-only audit is `/tmp/bootstrap-contract-receipts-final-audit.json`, SHA-256
`22cf8e5b92a7a42f9b738880294106bc8dd487cd9b9ec7ff95e420fba48c5811`; it reruns no behavioral test.

The immutable handoff is `/tmp/bootstrap-contract-receipts-handoff-20260930`.
Its 13-entry static `PAYLOAD-SHA256SUMS` SHA-256 is
`2a3d7e4f5395681d02689af1733aabc138f280313a6a63f88c1332072f106e42`.
All five mutation patches compile and pass vet in a separate author checkout;
the additive 25-entry `SHA256SUMS` SHA-256 is
`91d2fa3e457bf0f435b6621eefc32fba8c53c4d34b30101e0cb29a977ed5552e`.
Both handoff seals and every final evidence checksum were independently verified.

The fence records `go version go1.26.6 linux/amd64`, unchanged go.mod
`2a948e40658bb403c440ea649140b5ffae53d8b338b0dfc61c90625c708dbc5c`, go.sum
`2a8d74108e1c331b4b9ebdd8595c02f4b8a98e76f51fedf4c7498c7b41d974ca` and module graph
`19db0607a41328b8dc09c13292e40a0ee7024650c80ffc3e85e9a870ae225f9b`. All six local replacement repositories are
clean at their exact recorded commits/trees; the Safe SDK proof oracle is unchanged.
Integration preserves every qualified non-Markdown byte, including all Go/test,
module and pinned contract files. Only this documentation update differs.

This qualifies original historical-prefix admission only. Its finality assumption
remains `owned-rpc-assertion`; no independent consensus proof is established.
The scan-floor result excludes omission of this deployment prefix, not incomplete
indexing elsewhere. Current code/getters and roles, the evidence anchor, installed
services, activation, live chain identity and automatic runtime compatibility
remain open. All original pending phases remain. The separate Safe current-policy
approval and qualified public-route installation remain closed; the original
history claim is retained and unproven. No live RPC, real signing or mainnet
transaction was performed, and this report alone grants no activation authority.
