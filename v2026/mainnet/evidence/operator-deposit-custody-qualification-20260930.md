# Operator demand-deposit custody qualification

The server change at `45e11196ca98c191cc23487999cb7e19fc0a766d`
(tree `6e04c6f29dbfebb8ad3841a2e9fd923be5d1f092`) binds a new or
replacement demand-deposit attempt to the current operator's configured EVM
`depositSigner`, deposit hotkey, policy and coordinator graph before it stages
stake. Each operator loads only its own deposit, root-commit and artifact keys
from its own secrets vault. The on-chain settlement vault is claims custody,
not the source of those signing keys.

The worker first reconciles existing account intents, then reads a complete
canonical-hash operator/deposit snapshot. It keeps the governed deposit
principal exact and stages no more than the contract's two-rao reserve allowance
plus the native transfer's one-rao loss. A retained underfunded successful
stage is reported without silently creating another transfer. A retry cannot
replace previously retained calldata or make another demand credit for the
same epoch.

Sol medium independently qualified the frozen server source with **54 exact
roots in normal and race modes** (108 observed root executions): 12 new, 41
adjacent and one additional finalized-read root. Eight deterministic fault
controls reached their intended assertions, including an alternate operator
with a consistent stake response, rounding floor, retained intent and
canonical-hash call checks. The [sealed qualification report](/mnt/data/sn-testnet/sol-operator-deposit-20260930/QUALIFICATION.txt)
and [145-file checksum manifest](/mnt/data/sn-testnet/sol-operator-deposit-20260930/EVIDENCE-SHA256SUMS)
retain exact commands, streams, exits, patches, binaries and dependency graph;
the manifest SHA-256 is
`80416e6e9594d75c872221a6fd3c7401b740f210916ff524f5c3a018893bfb84`.
The graph pins SDK `516521fb`, Connect/SCTP `b163f9dd`, and the original SN
source `07e4327d`. Disposable PostgreSQL/Redis fixtures were removed.

This is source and fixture qualification, not a live operator-wallet deployment,
funding transaction, native-state proof, actual demand deposit or full contract
activation. The two real operator addresses, secrets-vault mounts and current
coordinator bindings still require independent mainnet verification. The
public-only monitor must not mount either operator's secrets resource.
