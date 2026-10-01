# Validator current operator and contract evidence qualification

The first frozen candidate `8bb2f38a8ca990fb476451a1fce6a63f02627ecd`
(tree `29b3e9676a3a9afbdc8e5f2983d8359f6f675646`) exposed a deterministic
RPC profile defect: its contract observer requested `eth_getCode`, `eth_call`
and `eth_getStorageAt` through the native-only read allowlist. Three of five
focused mainnet roots failed with `RPC method is outside the read-only mainnet
profile`. The [sealed failure receipt](/home/by/urnetwork/temp/sol-validator-authority-evidence-20260930/evidence/RESULT.md)
is retained with 121-entry manifest SHA-256
`a6ccc8a66dd592ec70b8ca3fe6aa563d76e465a280d07e2c8c7c790588779b7e`.
Its unaffected validator source passed six focused roots normally and under
race at both tested umasks, 67 adjacent roots normally, 21 selected adjacent
roots under race, and repeated cancellation/canonical and public-gate checks.

The corrected frozen candidate is `1f8a133c58b85dd33c09d283c109ae251db2fa46`
(tree `b5c7406977b928c9cd9a26ad9a41ca5e28f2bae0`). It routes the four
contract observations through the existing bounded EVM read profile without
loosening the native-only method list. Its validator subtree is byte-identical
to the first candidate. The changed files in shared cherry-picks `003dfc2f`
and `b03ecaec` are byte-identical to the corrected candidate.

Sol medium independently verified all **eight focused mainnet roots** normally
and under race detection with umask 0002 and 0077 (32 selected executions),
including the three formerly failing contract fixtures. All **53 adjacent
mainnet roots** passed normally; **40 distinct adjacent roots** passed under
race detection in bounded package-pass shards. The remaining 13 adjacent
race roots are unqualified: the broad shard was stopped after four RUN and
three body PASS events, before package completion. Its log and exit -15 are
retained without counting those body passes. The scoped read-profile roots
passed 30/30 normal/race repetitions; the executed-contract root passed 5/5
normal/race repetitions. A reverse-routing mutation reproduced the original
read-profile error at the intended assertion. Vet passed.

The [sealed corrected receipt](/home/by/urnetwork/temp/sol-validator-authority-rpc-fix-20260930/evidence/RESULT.md)
retains source/dependency fences, exact selectors, missing race-root inventory,
executed binaries, raw logs and control patch. Its 155-entry manifest SHA-256
is `e6f35746065361eb172e5ea4e62b3e36da1e779adcca573f2de9449540a8f49d`;
the author corrected handoff manifest SHA-256 is
`ff1d448aad9c5e903db9da4cd2519b27ec847c2e053659415933035dbb8ffb26`.

These local HTTP/RPC, signature and executed-EVM fixtures qualify bounded
partial observation. They do not establish combined live operator/contract
admission, complete proof-prefix/EMA replay, broad worker health, deployment
provenance, global signer custody, effective majority, or a public fresh start.
The public authority remains nil and closed. No live RPC/API, signer,
transaction, deployment or service start was used.
