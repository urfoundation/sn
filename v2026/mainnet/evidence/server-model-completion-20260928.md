# Full server model census and qualified corrections

The retained Linux model binary finished naturally on 2026-09-28 at
01:43:13 UTC after 5,694.863 seconds. Every one of its 1,118 listed test roots
has a terminal outcome: **1,108 passed, three failed and seven skipped**.
The package's terminal result is **fail**. No test body was stopped or repeated
to obtain this completion.

The complete body used frozen physical worktrees: server `4468a696`, SN
`615a7675`, Connect/SCTP `c68689c4` and the remaining exact module graph in
`source-manifest.json`. Execution used the actual `server/model` working
directory. The earlier invocation from the wrong directory is retained as a
disqualified diagnostic prefix.

| Failed root in the frozen body | Cause and separately qualified correction |
| --- | --- |
| `TestGetProviderEgressLocationDueOrderingIsStableAcrossLimits` | Its historical location fixture used a current update timestamp, accidentally requesting a fresh client-verdict probe. Server `d62f6fcc` fixes chronology and retains the real later-verdict behavior. Fifteen affected roots pass normal/race. |
| `TestAssignStragglerReapTimeRespectsBudget` | Its inserted terminal contract lacked the original close time and complete usage required by the custody guard. Server `936c3d95` supplies the original inputs. |
| `TestRemoveContractBatchesDrainsDuplicateCandidates` | The same terminal-fixture omission; its duplicate rows now split the recorded usage without changing drain assertions. Server `936c3d95` fixes it. |

The two corrected retention roots and six custody guards pass normal/race.
Original-fixture controls fail at the intended guards. These corrections are
integrated into the composed server through `936c3d95`; their
[integration receipt](server-model-fixture-integration-20260928.md) links exact
commits and checks. Production custody, retention and probe policy were not
relaxed. The combination supplies a complete executed census plus qualified
corrections, **not a claim that the frozen full invocation passed**.

Seven optional-configuration roots skipped and remain uncovered in this body:

- `TestAddReferralBonusesGrantsBothSides`
- `TestGeoLite2CitiesAsLegacyRowsResolveToThemselves`
- `TestNetworkReferral`
- `TestNetworkReferralCode`
- `TestOnboardingRegistryFile`
- `TestProConfig`
- `TestReferralBonusCount`

## Capture custody and cleanup

The original shell wrapper disappeared while its retained timeout/test process
continued under PID 1. Therefore its shell exit code was not collected; it is
not reconstructed from the package result. The full JSON event stream and
terminal census remain available. Future launches should use the maintained
qualification owner so terminal collection and service custody survive the
entire body.

After all original body processes had exited, recovery checked the binary and
both runner input manifests and removed only the recorded disposable PostgreSQL
and Redis container IDs after the maintained owner/label checks. Cleanup and
all input checks returned zero. The first source-guard invocation was refused
because recovery ran it outside the required server working directory. That
failed check is preserved; the correctly scoped invocation with the original
environment returned zero and verified the frozen source/module graph. No test
rerun was needed for either capture issue.

Raw evidence is under
`/mnt/data/sn-testnet/evidence/server-model-final-20260927/model-run/`, with
terminal recovery in `recovered-finalization/`. Important SHA-256 values:

| Artifact | SHA-256 |
| --- | --- |
| Full JSON event stream | `5227b047afc1b1f4c8ca7fb6ff8be7be62aa71f9023cc756c8a5bace74948940` |
| Terminal census JSON | `b1bf4ee354f0c383cf04a9cb3abb3bf4c9a2f127cd97ec3c57379767de6e9f71` |
| Executed model binary | `75b00cc01d675244770f7303bb12092ef909e08213a04ec8e080e5afa4eeb2f6` |
| Binary-listed test census | `babc86fc15321a4f3c0ceee4ebe22171cc174f3f863bef5d47921b3ac07e50ce` |

This result does not qualify later source changes, the final release
composition, production capacity or a live mainnet deployment. Reuse completed
unchanged scopes and test changed dependencies; a full restart adds no evidence
for the already completed bodies.
