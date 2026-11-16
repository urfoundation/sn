# Safe-history native trace qualification

The frozen candidate is `64362aeeacd30c8eee189ebbbb8bdcad8f453574`
(tree `e3d83d26c90fda36fe43f0902732c861bbcf79ff`). Its isolated integration
is `7624cb741f75d5a7acf8955379f0f2ed7e199af6` (tree
`f340a2aa1513d0881eb59f820deff2fd0190a73d`). The nine changed candidate
files in shared cherry-pick `3b9a947a` are byte-identical to the independently
tested integration and frozen candidate. Its dispatcher is unchanged from the
integration parent.

Sol medium independently verified **10 focused and 22 adjacent roots** in
normal and race modes: 64 selected executions with exact RUN/PASS, package PASS
and exit 0. All ten normal mutation checks and five selected race checks were
causal at their named assertions. `go vet ./mainnet` passed. The large aggregate
witness race case completed in 100.28 seconds; the aggregate-budget race fault
check completed in 149.02 seconds. No timeout or race report was accepted.

The merged build separately passed **nine integration roots** in normal and
race modes, plus five normal and three selected race causal controls, and vet.
The [frozen-source receipt](/home/by/urnetwork/temp/sol-safe-history-qualification-20260930/evidence/RESULT.md)
and [integration receipt](/home/by/urnetwork/temp/sol-safe-history-integration-qualification-20260930/evidence/RESULT.md)
retain raw logs, executed binaries/build information, exact patches, source and
SDK hashes, exits and clean-checkout checks. Their evidence manifest SHA-256
values are `74f266cc56eaae8a76e37f6c94200b7544db8a3bf010882de74522718988bf5c`
and `446ed8e8b1318616602d3689bc326da97a109cb7f29a473d8a9b7a87ed8e7c9f`.
The author handoff seal SHA-256 is
`b17cd6c4dee1120fafdfbcddf7feb26b423ee30adee3c965f0f900d1f1cc2bc5`;
the integration handoff seal is
`283808db2a186eda105fc17ed16bf39983e9208b449edf0f79366316273e8145`.

The read-only capture now retains bounded SDK native traces and authenticated
parent runtime code proofs, with canonical closing checks and explicit false
coverage flags where the SDK filters keyless ClearPrefix/root events or omits
rollback and inner/reverted EVM execution. It does not establish a complete
Safe history, full parent state/source proof, pending authority or public
successor submission. The submit gate remains closed. No live RPC, signer,
transaction or deployment was used.
