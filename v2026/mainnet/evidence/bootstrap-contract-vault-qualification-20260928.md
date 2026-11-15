# Settlement vault CREATE qualification

Astra max implemented the second approved contract-installation action,
`vault-create`, in frozen SN commit
`8b0b4dec8761233152e9074dd015aaf2508cb1ed`, integrated on the mainnet
hardening branch as `4a81f6a2`. It authenticates the exact settlement-vault
constructor, predicted address, release runtime and thirteen getter values.
The vault journal is bound to a completed reserve CREATE, retains the original
signed transaction and predecessor checkpoint, re-audits the reserve before
online progress and shares the approved graph's attempt/funding limits. The
default reserve action and its historical journal encoding remain unchanged.

Sol medium tested the frozen source in an isolated, pinned physical module
graph. All **51** focused `TestEvmVaultCreate*`, `TestEvmCreate*` and
`TestEvmPhasePreview*` roots passed normally and under race detection. A full
`./mainnet` normal run passed **426** top-level roots. Vet, formatting and
before/after source/module fences passed. Five causal controls each exposed
their intended missing guard: constructor netuid, canonical getter, reserve
re-audit, shared prior attempts and predecessor checkpoint continuity.

The [raw Sol receipt](/mnt/data/sn-testnet/qualification/sol-vault-mg03-20260928/RESULT-vault.md)
has SHA-256
`e682e058dab460a02f9e9bf150401374a270f36f33a2773504ed9bcaac55f3df`.
It also retains two unsuccessful first mutant attempts rather than counting
their compiler or independent-guard failures as discrimination. The integrated
`mainnet/*.go` bytes match the frozen tested commit. No live RPC, signing,
transaction or deployment occurred.

This is a source-level second action, not a complete installation. Coordinator,
registration, proxy, links, evidence deployment and separately authorized Safe
anchoring remain open. Live mainnet identity, approval, signer custody, deployer
funding and canonical receipts must be admitted before execution.
