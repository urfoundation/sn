# Offline bootstrap-chain qualification

The [offline chain preparation](../BOOTSTRAP-CHAIN.md) is integrated and
component qualified. `bootstrap-chain plan/apply/resume` binds the retained trim
review and two distinct protected UR generations to the existing signed
contract and root custody owners. Its parent journal recovers interrupted local
preparation without replacing child signatures or renewing their allowances.
Every result keeps chain execution, producer admission and activation pending.

Sol medium qualified sealed SN
`e17f91ac1a82bd0d709c2247f59951c3930e6619`, integrated as `76fc382a` on the
mainnet-hardening branch. The seven affected Go files match that sealed source
byte-for-byte. Unrelated Go and alert-rule files retain their pre-integration
bytes, including the separately qualified native-deadline observer. Integration
checked source equivalence and `git diff --check`; no behavioral tests were
rerun on the shared root workspace.

| Check | Result |
| --- | --- |
| Focused normal `^TestBootstrapChain` | Passed in 36.492s. |
| Full normal `./mainnet` | 369 top-level roots passed, zero skipped; JSON capture completed in 234.524s. An earlier full normal run passed in 233.169s. |
| Adjacent race coverage | All 87 selected roots passed across six disjoint shards: chain 14, root 11, EVM create 22, bounded trim 11, trim guard 13, remaining trim 16. The sorted union exactly matches the original selector; no race report. |
| Causal controls | Four fixed selections passed; four independently compiled mutants failed at their required assertion. |
| Source/module fences | All 49 fixed/control comparisons returned zero; actual physical modules and tracked-file hashes stayed unchanged. |

The controls independently bypass an exact UR input pin, reopen an atomically
substituted root config pathname, recreate a lost completed child, and admit a
duplicate UR role ID. Positive cases also cover signed-byte retention, local
exclusive ownership, interrupted claim/child/directory-sync boundaries, lost
output, stale transitive inputs, and resealed trim selection.

The original aggregate race attempt remains **failed/incomplete**: its global
ten-minute timeout fired at 600.066s while the final case of a 72-second trim
guard root was four seconds old. It is not counted as a pass. The bounded
replacement shards supply complete coverage without changing code, assertions
or operation deadlines. Raw commands, logs, exits, compiled binaries and this
failed attempt remain in the [executor evidence directory](/mnt/data/sn-testnet/evidence/mainnet-bootstrap-chain-20260928/RESULT.md).

Execution used Go 1.26.6, `GOWORK=off`, `GOTOOLCHAIN=local`,
`GOFLAGS=-mod=readonly`, and `GOMAXPROCS=2`. The actual tested physical graph
resolved Connect `358cefaef9b058cdd06ba5e9c4feeadef64ae1fb` at
`/home/by/urnetwork/connect`, server
`0633780cb5e97d29be423adfb19a8748a28e4895` at `/home/by/urnetwork/server`, and
SDK `42241118`; the remaining physical replacements are retained in the
before/after module manifests. This differs from the current composed
[registration/diagnostics release graph](registration-diagnostics-composed-qualification-20260928.md),
which uses Connect `b163f9dd`, server `5dc11761` and SDK `516521fb`.
This component receipt does **not** qualify that different dependency graph,
the combined release binary, artifact provenance or a full mainnet release.

| Retained artifact | SHA-256 |
| --- | --- |
| Executor `RESULT.md` | `c2fad0a7a1990952facd83dd5595e04eacdae78b15794edc15f3ee010bdc26f1` |
| Fixed tracked-file manifest | `8f07130b4dcbb939a1c9e2d3a98093300b6c01833dfd81859625cbc64a5d8af0` |
| Actual tested module graph | `1792221ecd2500aec7a73b1a8447fb4a43a6f4f317ccdad840fd59d320420e05` |
| Original author handoff | `756f24c647cae2b1e90309d337c92fcb2ccb78113430ef028840c291a23f474c` |
| Race scheduling addendum | `2c0a3f1f546fe5671dbc45011b5d7bd73b16ee21bfac1b2a757ef77932cb4d0d` |

MG-08 remains blocked for activation. Actual mainnet identity and runtime
approval, execution-time-safe owner trim and reconciliation, the complete
contract/evidence installation, two admitted standard UR validators and healthy
operators, current root authority and service activation, signing devices,
global custody fencing, Safe authority and bounded funding, and observed native
10% allocation / 90% recycle outcomes remain open. UR config files here are
byte-pinned preparation inputs; their producer semantics are not admitted by
this command. No live chain, signer, transaction, deployment or activation was
invoked during qualification or integration.
