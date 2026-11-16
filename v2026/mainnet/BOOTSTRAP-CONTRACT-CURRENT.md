# Current bootstrap fields at one finalized snapshot

`bootstrap-chain contract-current-state` compares five deployed accounts with the
original bootstrap field profile after reauthenticating all eight retained
[historical receipts](BOOTSTRAP-CONTRACT-RECEIPTS.md).

```sh
sn-mainnet bootstrap-chain contract-current-state \
  --config /private/chain.json --run-dir /private/original-run \
  --accept-plan-hash sha256:<original-preparation-hash> --online
```

The result is an **owned-RPC assertion** about the exact reported block. Code,
getters and storage replies are not independently verified account-storage proofs.
Native header bytes and the Frontier-committed EVM header are checked through the
existing mapping reader; native finality/canonicality remains the owned route's
assertion. No current runtime source-to-Wasm approval is created by this read.

The command borrows all original custody under shared locks. Historical receipts
retain their exact inclusion blocks and original attempt allowance. A subsequent
finalized native/EVM mapping supplies one `blockHash`/`requireCanonical` selector
for every current code, getter and storage read. Each read has its own bounded
retry budget under caller cancellation. The mapping is reread after the field
checks, then the snapshot and every original inclusion must remain canonical
through a later head. Ordinary head advancement is allowed; it cannot relabel
the state snapshot. Signed declarations and all eight original records are
reread before emitting a sealed result.

The `original-five-account-bootstrap-fields-v1` profile checks:

- Coordinator implementation: exact pinned runtime and constructor projections.
- Coordinator proxy: exact runtime, implementation/admin/beacon/initialization
  and owner slots, original owner/guardian/oracle, original sole policy and its
  original EVM initialization block. The clock uses the selected EVM height.
- Reserve: exact runtime, immutable domain, fixed proxy recorder and zero
  bootstrap principal.
- Vault: exact runtime, immutable domain, fixed proxy coordinator, packed escrow
  registration and original zero accounting/guard fields.
- Evidence journal: exact patched runtime, immutable deployment domain and the
  two declared mapping-root words.

Every checked getter and slot is listed in the report. Zero mapping-root words
do not prove that hashed entries are absent. The profile also retains the original
zero activity, no scheduled governance and one-policy requirements. It is a
bootstrap admission, not a general steady-state observer that silently accepts
new policies, operators, balances or governance transitions.

The proxy's evidence pointer may be unset or equal the exact approved journal;
the result distinguishes `unset` from `expected-journal-observed`. Observing the
expected pointer does not authenticate an anchor receipt or prove its governance
history. `complete_storage_verified`, `evidence_anchor_verified`,
`safe_authority_verified`, `installation_complete`, `activation_ready` and
`network_effects` remain false, and every original pending phase remains.

This port has no signing, signature-import, pending-state, submission or service
start option. Invalid input or original custody returns two; unresolved observation
or output failure returns one. Exit zero means only that the reported current
bootstrap fields matched. Admission refusal emits no partial report. No consumer
may treat this report alone as service or transaction authority.

The `2b87b133` [qualification receipt](evidence/bootstrap-contract-current-qualification-20260930.md)
records three focused and sixteen adjacent roots passing normal/race, six normal
causal controls and exactly two selected light race controls. Astra max authored
implementation, deterministic fixtures and patches and performed formatting,
compile-only and vet checks; Sol medium ran every behavioral test. Fixtures use
real pinned bytecode and synthetic local authority. No live RPC or mainnet action
is claimed. Complete storage/history, anchor/role evidence, service activation,
live chain identity and the independent Safe policy/public-route gates remain open.
