# Owned native fee verification

`Invoke` executes a separately approved `sn-mainnet` ELF. `Authority` pins that
ELF and the existing native semantics approval key, network, replay engine,
checkpoint, source review and original callsite profile. The request supplies
original proof input references; it cannot replace those independent roots.

The `verify-native-fee-outcome` command calls the existing
`runEconomicNativeFeeEvidence` producer. That producer verifies the signed
semantics approval, original signed EVM receipts and GRANDPA/native ancestry,
then executes the pinned original runtime replay. The command selects one
authenticated withdrawal/refund pair and retains its exact original signed
transaction, native payer, runtime, parent/child roots and finalized context.
An absent refund stays unknown, including when a receipt reports success.

The invoking owner seals the selected ELF, bounds stdout/stderr and owns a
dedicated subprocess supervisor. Cancellation, failed exit, extra output,
context mismatch or unreaped descendants return no `Verified` result. Only
the completed owned invocation can construct that type. `Facts` returns a
detached copy; JSON output is transport data and cannot be imported as a
`Verified` result. Completed results retain their original authority without
requiring an executable file to remain installed after the worker joins.

`RetainOriginals` streams the exact request, approval, signature archive,
receipt collection, checkpoint, finality proof and replay job to the durable
consumer. It verifies bounded complete bytes and their original hashes without
loading an entire proof into memory. The Server stores those originals in the
same transaction as credit release. Missing, changed or incompletely retained
proof bytes prevent a new settlement; a known conflict retains a conservative
hold. Original executable artifacts remain owned independently by their pins.

The Server operator settlement consumer additionally requires an independently
signed and pinned denomination policy. It never derives native debit from gas
used or releases a fee ceiling from a receipt alone. No Rao-to-wei conversion,
runtime admission, approval signature or production executable pin is supplied
by this package. Miner and validator fixed per-action signing limits have no
adjustable fee-credit ledger and do not acquire one through this change.

New deterministic Go roots cover owned invocation, original receipt/finality
joins, independent policy rejection, missing refund, reverted-call fees,
signature retention, process failure, cancellation, immutable executable
retention and descendant reaping. Their protocol/runtime peers are explicitly
synthetic. These tests were authored without execution; they do not qualify an
original native Wasm engine, signed production policy or live fee outcome.
`TestNativeFeeOutcomeExportSettlementFixtures` supplies the explicit Server
public-model join fixture. For qualification, select an existing private
persistent directory with `URNETWORK_NATIVE_FEE_EXPORT_DIRECTORY`; the exporter
publishes `pair.json`, `missing.json` and `fractional-conflict.json`. All three
retain the same original signed transaction. The third supplies a contradictory
complete native debit of 751 Rao so the public settlement test can retain a
conservative hold when a signed denomination requires exact division by two.
An ordinary package test uses its own
temporary directory and performs the same proof and original-custody checks.
The Server consumer separately pins the selected `mainnet.test` ELF.
