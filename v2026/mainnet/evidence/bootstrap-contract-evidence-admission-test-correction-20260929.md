# Evidence admission test exit-code correction

Exact parent: `df139093fc79100cb2ad5fe8bd42ade2d1d74c90`. The containing commit
on `codex/mainnet-evidence-admission-test-fix-20260929` is the narrow successor
at `/home/by/urnetwork/temp/sn-mainnet-evidence-admission-test-fix-20260929`.
Only two test files and this handoff change. Production admission guards, CLI
exit semantics, approved payloads, historical journals, generated artifacts,
bindings and committed module files are unchanged.

Sol's original 28-root normal invocation reported two real test assertions:
`TestEvmEvidenceCreateRejectsChangedConstructor` and
`TestEvmEvidenceCreateBindsApprovedDeploymentDomain`. Both commands correctly
returned `2 contract phase selected action: ...`; the tests incorrectly required
exit 1 and labeled the valid rejection as reaching custody or reusing a domain.
The first table stopped on its initial `target` fault. The later 40-minute
package timeout happened during `TestEvmEvidenceCreateClaimRecoveryKeepsEightLocks`
and is separate evidence, not another established defect or a pass.

The original raw log and Sol's deterministic two-root JSON reproduction remain
under `/mnt/data/sn-testnet/qualification/sol-evidence-create-20260929/`:
`focus-normal.log` and `repro-two-normal.json`. Neither is rewritten or waived.
The isolated reproduction failed both roots in under two seconds. The author
did not run that reproduction or any other behavioral test.

`runBootstrapContractCommand` returns 2 immediately after a selected-plan
construction error. The selection branch precedes all action stores, journal
locks and RPC adapter construction. Signed-file hash/envelope validation also
returns 2 before custody. Exit 1 belongs to operational/output errors; changing
the product to emit 1 here would alter established CLI semantics and is not the
correction. Existing vault/proxy constructors and escrow/reserve/vault envelope
admission tests already require exit 2 at this boundary.

The constructor root now requires exit 2 and the selected-action diagnostic for
all twelve independently approved mutations. After every rejected `apply`, it
directly requires all eight journal files and all eight lock files to remain
absent. Restoring the exact original approved constructor must make `plan`
succeed, with no RPC read or write observed throughout the sequence.

The deployment-domain root similarly requires selected-action exit 2 for both
`plan` and `apply` with a newly approved ID but unchanged old constructor bytes.
Both calls must leave every journal and lock absent. Restoring the original ID
must make review succeed without RPC activity. The SHA-256 rule and complete
approved-byte comparison are unchanged.

Adjacent review found a third occurrence of the same expectation error in
`TestEvmEvidenceCreateRejectsOtherSignatureAndEightCustodyAliases`. It expected
exit 1 when importing the vault-binding signature as evidence CREATE. This
successor requires exit 2 with the signed-envelope diagnostic, snapshots all
sixteen custody files before any invalid input, then requires every byte to be
unchanged after the alias and wrong-signature attempts. Offline reopen must
still show unsigned preparation with zero attempts and no receipt. Import of
the actual evidence signature must then succeed, still without RPC or another
spend. The adjacent online-error and output-failure expectations remain 1.

The author ran gofmt, `git diff --check` and compile-only `go test -c -p=2`.
The external module files, compile log and unexecuted binary are under
`/mnt/data/sn-testnet/worktrees/sn-mainnet-evidence-admission-test-fix-20260929/`.
The eight physical dependency pins remain those in
`bootstrap-contract-evidence-source-20260928.md`. No behavioral test, live
signing, RPC, DB, chain or Safe-inner action was executed by the author.

Sol should qualify these three exact roots normally and under race:

```
^TestEvmEvidenceCreate(RejectsChangedConstructor|BindsApprovedDeploymentDomain|RejectsOtherSignatureAndEightCustodyAliases)$
```

For the regression control, restore only the former expected exit 1 in each
root while retaining the strengthened no-custody assertions. Each must fail
on the existing valid exit 2 at its intended boundary. For the product guard
controls already assigned in the evidence handoff, bypass only the index-7
approved creation-byte comparison in `selectEvmCreatePlan`; the constructor and
deployment-domain roots must then reject the mutant for losing selection
admission. A separate signed-input control can bypass the evidence action's
pre-custody `action.signed(raw)` check in the command; the import root must fail
because rejection moves past the required input boundary. Keep production
guards restored for positive runs and preserve every control's exact diff.

The corrected successor retains 28 evidence roots. All original evidence,
adjacent, race, causal and full-package qualification remains separately owned
by Sol. Passing these three focused roots alone does not qualify evidence CREATE
or convert preserved timeouts into passing runs.
