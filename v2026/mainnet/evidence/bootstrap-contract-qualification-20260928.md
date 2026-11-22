# Reserve bootstrap qualification

The executable reserve installation and unsigned approval preview are integrated
through `d7b54d1a`. Astra authored the implementation and deterministic fixtures;
Terra medium executed the following retained batches. These are local tests of
public custody, real HTTP boundaries and geth EVM execution, not a live mainnet
deployment or completion of the other eight installation actions.

## Scope and results

| Frozen source and scope | Result |
| --- | --- |
| `c10a2cac`, native `TestRootSubmission` adjacency | All 12 roots passed normally and with race detection. The encompassing package failed on the separately identified CREATE fixture. |
| `576dea58`, `^TestEvmCreate` | 19 of 20 roots passed in each mode; the lost-reply test failed a diagnostic-word assertion. Both original failed package results are preserved. |
| `576dea58`, six CREATE causal controls | Each reached its intended regression assertion in both modes: signed fields, deployed runtime, publication before send, unavailable mapping, advancing finalized head and retained scan prefix. |
| `7b23b1ca`, lost-reply correction plus eight offline/preview roots | All nine passed normally (2.376 seconds) and with race detection (12.598 seconds). No root was skipped. |
| `7b23b1ca`, five offline/preview causal controls | Each reached its intended assertion in both modes: created/reverted terminal status, unsigned preview, constructor equivalence and independent approval. |
| Generator | The original invocation passed 20 roots before a fixture panic. The corrected retained-bytecode root then passed in both modes. The six roots that never started in the original invocation passed separately, normal 0.044 seconds and race 1.277 seconds. All 27 roots have completed coverage; the original failed package is not relabeled as a pass. |

The unique CREATE/offline/preview coverage is 28 roots across the frozen
batches. Reused results keep their original source identity; this is not a
claim that one invocation passed all 28 on the final integrated source.
Final preview vet/build, source and 152-file consumed-input checks passed.
The corresponding earlier corrected CREATE checks passed with 154 consumed
files. No Solidity or generated binding source changed.

The first fixture encoded an approval signature with an extra `0x` prefix,
preventing every CREATE case from reaching its intended behavior. A second
generator fixture omitted the names required to preserve reviewed non-release
bytecode. Both were corrected without relaxing production authority or artifact
checks. The lost-reply assertion now verifies one original HTTP write, validated
retained signed bytes/hash and attempt count, then the same canonical inclusion
without a second write. Wording is no longer its evidence of durable recovery.

An early causal collection also reported zero tests for some race selectors;
those outputs establish no regression result. The corrected collector checks
the expected test name and assertion, continues independent stages after
failure, and retains every original capture.

## Integration and reproducibility

The author stack was `c10a2cac`, `072af331`, `576dea58`, `2fd53a3d`,
`6433da12`, `7b23b1ca`. Integrated commits are `e0190050`, `2b383dd7`,
`ca32b73c`, `b1cdff2f`, `d7cbeefb`, `d7b54d1a`. The sole merge conflict was
documentation; both the existing head-advancement rule and new installer scope
were preserved. Installer/generator source and module files match the sealed
author source byte-for-byte.

The consumed local package graph in these installer batches contains SN source
and versioned external modules; unrelated sibling repositories were not consumed.
Comparing the 152-file preview manifest with the integrated tree found one
changed dependency, `crv4/chain.go`, from the separately qualified receipt-reader
work. That change required a small composed CREATE/native-custody check;
unaffected bodies and controls do not need a blanket rerun.

That composed check subsequently passed on integrated `61a3a23d`:
`TestEvmCreateCommandExecutesReviewedReserveAndResumes` and
`TestRootSubmissionOfflineCustodyServiceComposition` both passed normally
(1.740 seconds) and with race detection (11.172 seconds). Compile-only checks
for all five affected packages passed separately. The integrated head and
consumed source hashes remained unchanged; concurrent root edits were confined
to documentation. See the [source-read receipt](source-read-cause-qualification-20260928.md)
for its input/result hashes and exact scope. This closes that composed
dependency check, not the remaining installation actions.

Raw directories:

- `/mnt/data/sn-testnet/evidence/mainnet-bootstrap-create-20260928`
- `/mnt/data/sn-testnet/evidence/mainnet-bootstrap-create-fix-20260928`
- `/mnt/data/sn-testnet/evidence/mainnet-bootstrap-contracts-preview-20260928`
- `/mnt/data/sn-testnet/qualification/source-finality-read-20260928/integrated`

| Retained input or result | SHA-256 |
| --- | --- |
| Corrected CREATE aggregate summary | `52d01a9936b70cdf89b4aead5a61191349f5f479c96ab89fbbf2a583dd1a4754` |
| Final preview source manifest | `148e6cd9fe36042f751d8a261033553c44c2f4876aa8bb7b5cf781d9bc6ff10b` |
| Final preview consumed-source manifest | `9f42be16787904560d4160e9ceec4581f97dfbdbba4ff338ba43f5a5bfd88d8a` |
| Final preview aggregate summary | `6909d5ab74e98acb27a708c7d5caab64bd019137d01044221730d8f98575b5c6` |
| Nine-root normal event stream | `8b783ad89a813dc33ff200c519e08cf25a80b6787da8c0cda5b28d49b04ad220` |
| Nine-root race event stream | `925c7a777ce75b8455a0e809987308a74df2c1fc1430d051ddf812e7f3cb4420` |

## Remaining launch work

This phase executes only the reserve CREATE. Vault, coordinator implementation,
escrow registration, initialized proxy, reserve/vault links, evidence contract
and genuine Safe anchoring remain required. The preview validates supplied
structure and artifacts; it does not authenticate live chain identity, custody
exclusivity, funding or runtime provenance. Mainnet identity, actual approval,
signing custody, service activation and measured 10/90 native outcomes remain
open. See [bootstrap operation](../BOOTSTRAP-CONTRACTS.md) and
[the launch plan](../MAINNET.md).
