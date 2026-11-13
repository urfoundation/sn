# Bounded scratch image qualification, October 1

The frozen source `0fe8c206` builds and reads back the exact Linux/amd64
`server-competitionworker` scratch image from the fresh-catalogue release
candidate. It leaves the parent manifest unchanged and writes a separate image
supplement. It neither loads nor runs the image, contacts the chain, or grants
deployment authority.

The [author handoff](/mnt/data/sn-testnet/image-composition-astra-20260930/evidence/HANDOFF.md)
is sealed by SHA256SUMS digest
`1bfd8bb580cdc8cd478e7adbb7983ec1f54746d54b34e8d1ecc19f27c5223090`.
Its 51 builder roots passed normal and race, vet passed, and four single-guard
causal mutations failed their targeted tests. A separate source-policy denial
failed before the image COPY step, showing that the installed BuildKit honors
the local source policy.

The [independent Sol receipt](/mnt/data/sn-testnet/image-composition-sol-20261001/evidence/RESULT.md)
is sealed by SHA256SUMS digest
`324b4aa8d4c12971074d4ed571e8056e940739ec72ecb91ca78c4ff9ed478f1f`.
It passed the same 51 roots in normal and race modes, vet and four causal
controls. A separate real build produced an OCI archive with SHA256
`d08ededb62482540dafb77508a4ea28fd081039bfb613485ee01fe44fc7776ff`,
byte-identical to the author's archive. Independent data-only readback checked
the parent manifest, retained inputs, platform/config/layer/diff ID, and sole
root-owned executable. The runnable platform digest is
`sha256:72ad21b129eda945475346abc6066577ddb733068219a0dff7da6fef2f647668`;
the embedded executable matches the parent binary exactly.

This closes only one local source-to-image subgate. The other seven image
recipes still reference an Ubuntu base and remote `ADD` inputs and were not
built. Aggregate source-to-image verification, reproducibility, release
completeness and deployment approval remain false. No production image has
been published or read back from a running host.
