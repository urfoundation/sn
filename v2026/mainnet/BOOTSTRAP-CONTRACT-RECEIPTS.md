# Read-only original contract receipt admission

`bootstrap-chain contract-installation-receipts` reauthenticates the exact eight
completed original contract actions under their original accepted preparation.
It composes the signed [contract-role declarations](BOOTSTRAP-CONTRACT-ROLES.md)
with retained deployment history; it does not establish current installation.

```sh
sn-mainnet bootstrap-chain contract-installation-receipts \
  --config /private/chain.json --run-dir /private/original-run \
  --accept-plan-hash sha256:<original-preparation-hash> --online
```

All original preparation and action markers remain held read-only during the
observation. Missing, changed, incomplete or foreign custody fails before chain
reads. The command uses only the original approved owned RPC route, never imports
a signature, sends a transaction or rewrites a journal. Each action retains its
full record hash, signed transaction hash, counted attempts and distinct native
and EVM inclusion. The original attempt allowance and pending phases remain.

The existing canonical reconciler checks each exact native/EVM receipt and its
historical code, storage and getter postconditions. Each receipt receives its own
bounded read budget under caller cancellation. The initial finalized head is a
canonical ancestry snapshot; it is not the block at which historical contract
storage was read. Normal head advancement is allowed. At the end, the original
approved start, the snapshot and every original inclusion must remain canonical,
and the eight original action records and signed declarations are reread.

`deployment_scan_floors_verified` means each signed, nonzero EVM scan floor is no
later than the earliest original EVM inclusion. It prevents omitting this
deployment prefix. It does not prove complete event indexing, current service
state, the later evidence anchor or all history needed by a future activation.
Native block numbers cannot substitute for EVM indexing heights.

The sealed report separates the original receipts, their historical checks,
the ancestry snapshot and the later checked-through head. Its finality assumption
is explicitly `owned-rpc-assertion`; it is not an independent consensus proof.
`current_state_verified`, `evidence_anchor_verified`, `installation_complete`,
`activation_ready` and `network_effects` remain false. No consumer may use this
report alone as production activation authority. Exit zero means the scoped
historical checks passed, two means invalid input or unresolved local custody,
and one means unresolved online admission or output failure. Refusal emits no
partial report.

The `c6b31fdb` [qualification receipt](evidence/bootstrap-contract-receipts-qualification-20260930.md)
records five focused and sixteen adjacent roots passing normal/race, five normal
causal controls and exactly three selected light race controls. Astra max authored
the change and ran formatting, compile-only and vet checks; Sol medium ran all
behavioral qualification. Fixtures use synthetic local authority and pinned
bytecode; no live chain evidence is claimed.
Current contract/role checks, the anchored evidence journal, service admission,
live chain identity, and the separate Safe policy/public-route gates remain open.
