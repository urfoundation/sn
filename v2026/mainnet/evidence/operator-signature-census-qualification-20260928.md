# Operator signature census source qualification

Astra max implemented the additive server `strecovery` census and restoration
command on branch `codex/mg03-operator-census-20260928`, frozen at
`71efeb1f30d254de2f5282d747194b33935545ec`. It reads explicitly selected
operator databases in repeatable-read snapshots without filtering by status,
joins retained hash-named signed-RLP stores, and records the provenance and
exclusions of original, replacement and cancellation attempts. A private
archive can be replayed and original bytes restored with create-only writes
after an interrupted prefix. The archive is evidence, not spending authority;
the command has no RPC, signing or broadcast port.

Sol medium qualified the frozen server source in an isolated physical module
graph. All **23** new top-level roots passed normally and under race detection,
including three real private-PostgreSQL snapshot roots. Adjacent **19**
controller account/receipt and **six** model transaction roots passed in both
modes. Vet, formatting and before/after source/module fences passed. Three
causal controls showed that status filtering loses terminal originals,
read-committed isolation admits a changed snapshot, and removing the archive
conflict preflight can publish a restoration prefix before a later conflict.
The private PostgreSQL/Redis containers were removed after the gate.

The [raw Sol receipt](/mnt/data/sn-testnet/qualification/sol-vault-mg03-20260928/RESULT-server.md)
has SHA-256
`8b98562d4cfceaee2ede84c3fd2fc0ddbd4fa3da31dcda93c85e33560fecb3d0`.
It retains an initial fixture-attestation failure that occurred before tests
ran. No live operator database, custody file, RPC or chain state was changed.

This closes the source-level status-independent discovery and byte-preserving
local restoration slice of PF-03. Canonical receipt/finality joins, actual fee
accounting, distributed custody ownership, deployed operator recovery and
release composition remain open. This feature branch is not an accepted or
deployed server image.
