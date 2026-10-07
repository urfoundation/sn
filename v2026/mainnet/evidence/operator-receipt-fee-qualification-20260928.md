# Operator receipt and fee reconciliation source qualification

Astra max implemented an additive offline `strecovery reconcile` path in
server commit `26008e5859991b0649896466b094aff50b4a3440`, based on the
qualified status-independent census at `71efeb1f`. It joins supplied
observations to every archived original, replacement and cancellation
signature, retaining missing, malformed, orphaned and conflicting outcomes.
It computes conditional gas fees once for a resolved nonce winner from receipt
gas used, without treating a maximum fee envelope as actual spend. The command
does not sign, broadcast, edit an operator database or restore live custody.

Sol medium tested the frozen server source in an isolated physical graph.
All **38** `strecovery`/CLI roots and **28** adjacent controller/model roots
(22 controller and 6 model) passed normally and under race detection; the **15** new roots also
passed separately in both modes. Vet, formatting, source/module fences and
private PostgreSQL/Redis cleanup passed. Four isolated causal controls exposed
the intended incomplete-sibling, gas-used, canonical-block and inclusion-slot
failures. An ineffective first slot mutation was retained and excluded from
the passing control count.

The [raw Sol receipt](/mnt/data/sn-testnet/qualification/sol-mg03-receipt-fees-20260928/RESULT.md)
has SHA-256
`9b0267a1d078eea5585c3d513a2f585449f7826346ab4f0ed29d421aad5e4d62`.
No live RPC, signer, production database, custody file or chain state was used.

This is conditional offline accounting, not authenticated production finality
or actual chain-fee proof. A trusted native-to-EVM mapping and receipt
observation producer, current authority, release integration and live restart
remain open. The candidate keeps finality, canonical receipt reconciliation,
actual fees and spending authorization false until those separate gates close.
