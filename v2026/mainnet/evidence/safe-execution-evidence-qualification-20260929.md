# Pure Safe execution evidence qualification

Integrated source is `c648495f791d4538d2d0d62cf19fb58b214f3a5a`, tree
`bf5c28a647022d798b4a0672e7f5d4a558ac760e`, from the isolated worktree
`/home/by/urnetwork/temp/mg08-safe-execution-evidence-fixture-census-20260929/sn`.
The shared qualified base `14cd8e13` advances directly to this exact candidate.
The following documentation commit changes no Go source or module resolution.

Sol independently ran all thirteen focused and four adjacent roots in normal
and race modes. Each of the four streams has package PASS, exit 0, exact root
coverage and no failed, missing or skipped roots. Thirteen isolated causal
patches each compile and fail their exact named assertion in both modes,
with root/package FAIL and exit 1: twenty-six intended control executions.
The production helpers have no command, network client, signer or custody owner.

Evidence is retained at
`/mnt/data/sn-testnet/qualification/mg08-safe-execution-fixture-census-sol-20260929`:

| Artifact | SHA-256 |
| --- | --- |
| `SOL-RESULT.md` | `7522899adaa36562bbafa61a13b18467d91d8c5cbfa56a1323681da8fa22ffea` |
| `SOL-RESULT.json` | `37864b48ac4d321aad4d169354a55e8f9a80ebab429dc12c23c3502edbf8f681` |
| `SHA256SUMS` (125 files) | `e50abb78dca18bc9d93f9f37c0b6c2c57756f34f5110df4adb7a061825e9a3b4` |
| Author `HANDOFF.md` | `530e4c6b4078a67c5874a3a1689f3b28684b07f013a936328e7744ddd51d9951` |
| Author `SHA256SUMS` (32 files) | `c6e1ad5d5cfc5da8153fe0eaba8d0ddc1679d6e312a660492e51fceb2eadc546` |
| Exact module bytes | `a20f3918c6f8caa9e5634aeb03dc048e528b5742da90ac6f70dc020b21cbdee9` |

Before integration, Astra independently verified both manifests, the raw JSON
root/package terminals and exact control assertions, process result codes,
unchanged module bytes and every current physical path/root/head/tree. Ten
resolved local modules and the separately fenced unselected Warp path are clean.
The isolated graph uses Connect `b163f9dd`, SDK `516521fb` and server `cfcbfcba`;
shared server's later finality-capture commit is not silently substituted into
this receipt. Existing shared composition evidence remains separately scoped.

The oracle installs the exact selected published Safe/SafeL2 proxy and singleton
in a local in-memory EVM for versions 1.4.1 and 1.5.0. Its checks cover digest
fields, calldata, direct/personal-sign/high-s recovery, ordering and signature
bounds, versioned contract callbacks, unresolved approval state, outcome emitter
and digest, committed inner failure, outer rollback and nonce wrap semantics.
Synthetic signing exists only inside the test fixture.

The parent `e07484ab` remains an unqualified attempt. All twelve original focused
roots failed fixture setup with `unexpected end of JSON input`; no race, adjacent
or controls ran on it. The catalog contained an unselected singleton whose bytes
the selected archive reader correctly omitted, but the fixture decoded it before
filtering. C648 filters first and has a deterministic absent/empty/malformed
unselected-artifact regression and a control that restores the old ordering.
The original anomaly remains at
`/mnt/data/sn-testnet/qualification/mg08-safe-execution-sol-20260929/E074-ANOMALY.md`,
SHA-256 `b1b18f403317ae6091ce43064b050f862a16f35087323980d954ca983c8e8e7b`;
its `E074-SHA256SUMS` digest is
`cceb7a4c7575fd126b9f051448db53d25d14cea50a4dc9ea01ae8424e4ee9aba`.

Current Safe code/storage, initializer-owner binding, owner membership/threshold,
callback and approved-hash state, canonical inclusion, transaction-call binding,
global custody, approval to sign, installation and activation remain unverified.
These helpers neither make the unsigned successor executable nor authorize
receipt adoption. A different new Safe still requires separately approved
ownership migration from the retained initializer owner.

The prior artifact verifier's nine-root composed normal/race smoke on `18a88db4`
is documented in the [separate artifact receipt](safe-release-profile-qualification-20260929.md).
The SDK snapshot remains 543/618 package-backed race roots with 75 pending;
the MG-08 broader adjacent battery remains 150/270 in both modes with 120 unrun.
No full-package or live deployment claim follows from this scoped increment.

## Separate composed smoke

Sol also qualified exact `c648495f791d4538d2d0d62cf19fb58b214f3a5a`, tree
`bf5c28a647022d798b4a0672e7f5d4a558ac760e`, from the clean detached worktree
`/home/by/urnetwork/temp/mg08-safe-execution-composed-smoke-20260929/sn`.
All four selected roots pass normal and race with package PASS and exit 0:

- `TestSafeExecutionDigestMatchesPinnedProxyAndSingleton`
- `TestSafeExecutionOutcomesFollowPinnedContractAndNonce`
- `TestBootstrapContractSuccessorCommandAdoptsCompleteV3Custody`
- `TestMonitorReadIncidentCommandRecoveryRecurrenceAndRestart`

Evidence is retained at
`/mnt/data/sn-testnet/qualification/mg08-safe-execution-composed-sol-20260929`:

| Artifact | SHA-256 |
| --- | --- |
| `SOL-RESULT.md` | `0581ff700d1e1276e1ce55c97a8a7bad73e8977297c1ecab9c388b799c14dfe6` |
| `SOL-RESULT.json` | `c8b80c7385ab44f614fda804a912c0d2922a5da6dfab297f16c55a8b2fb65f77` |
| `SHA256SUMS` (18 files) | `b0910e161cae71c13012c5560648c2368b2b9a74fadcb06a5fef4c8efd34cf86` |
| Exact before/after module bytes | `0a84499a881e31ea229a437529b18aa7fa1c2d12f3493f1d6589b6d8198009cd` |
| Exact before/after source fence | `ad3c4a839ebf6f3d813aaf669d253dee7ce0b3d8715985602ab9d9d62fd1dd3c` |

Astra independently verified all eighteen manifest files, the raw root/package
terminals and process exits, identical before/after module/fence bytes and all
eleven actual clean physical roots. This smoke composes pure Safe calculations
with the original successor and incident commands. It predates the signed local
successor preparation source and cannot qualify that later source or merge.
The unrun broader scopes and all live authority limits remain unchanged.
