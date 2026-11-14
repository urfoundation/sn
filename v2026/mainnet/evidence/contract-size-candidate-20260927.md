# Contract size candidate, 2026-09-27

The clean v7 source at `7175313d` (EVM tree
`b19dcdde1046bc4d8d22978d129c11338251e598`) passes
`forge test --root evm -q` and `forge build --root evm --sizes` with pinned
Foundry 1.7.1, solc 0.8.24 and three pinned Forge/OpenZeppelin libraries.
The exact local build-input observation is retained with the build logs. This
is a source/build check, not an approved mainnet deployment, a reproducible
build attestation or an attestation of the live Subtensor EVM limit.

| Deployable contract | Runtime bytes | Foundry margin to 24,576 bytes |
| --- | ---: | ---: |
| `STCoordinator` | 24,564 | **12** |
| `STReserveSink` | 1,558 | 23,018 |
| `STSettlementVault` | 9,486 | 15,090 |
| `STValidatorEvidence` | 12,192 | 12,384 |

The coordinator has effectively no size headroom. Freeze its exact build
inputs and generated deployment bytecode in the approved release manifest;
rerun this check after any source, compiler, optimizer or dependency change.
Before mainnet deployment, confirm the selected live runtime enforces the
expected code-size rule and exercise creation/readback with the exact artifact.
A successful local build alone does not establish that the live deployment will
succeed.

Raw build output, the 226-test Forge result, toolchain observation and artifact
checksums are retained under
`/mnt/data/sn-testnet/evidence/mainnet-source-lock-v7-20260927/`.
The `STCoordinator.json` SHA-256 is
`5a51b1f4a426cfe36e760eb5312947abc80fa9a7e4e4a517210d3da0e5278ba3`.
The raw `forge-build-sizes.log` SHA-256 is
`f39ac09511ce99cd1965ca01c4da1ecae915c7d993b76b87a0447c00d6389ad1`.
