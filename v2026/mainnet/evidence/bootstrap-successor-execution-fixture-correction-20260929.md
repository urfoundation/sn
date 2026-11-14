# Execution fixture private-input correction

Independent Sol normal testing of candidate
`00ec46db917d283e665dc0e6e813bcc792d87bb2`, tree
`ceec679bf8fe25cccdfe60e4cf6eb363382b84b3`, ran all twenty focused roots and
reported twenty failures, package exit 1, in 48.395 seconds. Every root stopped
in fixture setup with `successor Safe signature input differs` and
`bootstrap root directory is not owner-private`. These are failed prerequisite
executions, not causal evidence about the execution-owner assertions. No race
qualification was attempted on that fixture.

The original log is retained at
`/mnt/data/sn-testnet/qualification/mg08-successor-execution-sol-20260929/focused-normal.log`,
SHA-256 `613e08e926a92160709241b75e189d6ab9b60068e33df3ae8f30e55a62922c17`.

Astra source inspection found that Go 1.26.6's `testing.TempDir` creates its
numbered children using mode `0777`, subject to umask. The new binary fixture
writer used mode `0600` for each file but did not set its parent to `0700`.
`readBootstrapRootFile` correctly refused the shared parent before reading any
signature bytes.

The correction is limited to test source. A shared private-directory fixture
helper now explicitly sets `0700` for both signature-file parents, the model
execution request directory, and both registry setups. The remaining direct
temporary directory belongs to the intentional extra-hardlink refusal case and
is never admitted as an input parent. Production permission checks are unchanged.

`TestBootstrapSuccessorExecutionFixturesUsePrivateInputDirectories` verifies
literal bytes and their hash through the production reader, then deliberately
changes each parent to `0755` and requires the original owner-private refusal.
The focused inventory is now twenty-one roots. Compile-only `go test -c -vet=off
./mainnet`, `go vet ./mainnet` and `git diff --check` pass. Astra ran no behavioral
tests; corrected normal/race and causal results remain the independent tester's
responsibility.
