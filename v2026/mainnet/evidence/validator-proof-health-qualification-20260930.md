# Validator proof-prefix and worker-health qualification

The frozen source is `2445f8d19e6c3ef57ffc29bfa314c64364147c1b`
(tree `42a0f0ca6be4183166ab2c4f3e9d50b20351aee4`). All eight changed
source/document files in integrated cherry-pick `4080ff77` are byte-identical
to that candidate. The independent source index and hashes match; the frozen
worktree is clean.

Sol medium verified **18 focused roots** normally and under race detection
with both umask 0002 and 0077; **109 adjacent roots** passed normally.
All 52 validator adjacent roots and 20 selected of 57 mainnet adjacent roots
passed under race detection. Repeated custody, cancellation and retry suites
passed normally and under race detection (15, 10 and 10 events in each mode).
Three separate mutations failed at their named assertions: bypassed proof
replay, ownership failure misclassified as unavailable, and bypassed native
stake admission. Public start remains closed under its selected regression.
There were 323 positive PASS events across the audited cells.

The [sealed independent receipt](/mnt/data/sn-testnet/validator-proof-health-sol-20260930/receipt.json)
and [99-file checksum manifest](/mnt/data/sn-testnet/validator-proof-health-sol-20260930/evidence-SHA256SUMS)
retain exact selectors, package exits, logs, executed binary hashes, source
fences and control patches. The manifest SHA-256 is
`6be69847b4d9d8e5fb411847022fcf971ce8ae95db9dcd39171fc02fd3ce160a`;
the author handoff manifest SHA-256 is
`7f22bd1f98b783fe4c83ed0789842defafd71c01b2531f778b2577b80e731ef9`.

`admit-health` replays each original pinned operator activation prefix through
the existing proof/EMA verifier, composes current native, contract, client-key
and stake observations, and retains completed per-role checkpoints if a later
read fails. It reads protected standard validator progress separately and
classifies missing/stale progress as a warning without softening ownership or
proof-integrity failures. It does not invent an empty mutable namespace or
relax service-UID ownership.

These synthetic local fixtures do not establish current mutable-prefix
completeness, per-operator live-worker attestation, global signer custody,
applied majority influence or signed launch authority. Public fresh start
remains nil and closed. No live RPC/API, signer, transaction, deployment or
service start was used.
