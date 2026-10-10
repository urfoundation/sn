# Owner Ledger native SDK artifact qualification

The frozen candidate `23e9e7c0bbc71ea056578dd515392973f500300d`
(tree `2fd3180d22aa93ac45f427298b51948b37209f6f`) is integrated at
`73361e6a` with all three changed files byte-identical. Production owner
signing Go/Python bytes were unchanged. It pins Subtensor source commit
`67dcf7f791dc495064c293f080a0702cb433e51e`, its locked dependencies,
the Linux x86_64 build toolchain and the actual native extension SHA-256
`77ffa6ac04459bc5d9895d225c2775827473d0db99c25802ea7aa32ae750a8d2`.
Three exact-source offline builds succeeded. Rebuilding at the same canonical
target path reproduced wheel and extension bytes; a different path changed
vendored OpenSSL embedded installation strings. The artifact requires glibc
2.38 and is not a portable macOS/Windows or manylinux qualification.

Sol medium independently ran the **five native SDK roots** and **20 adjacent
roots** normally and under race detection: **50/50 named executions** passed
with package PASS, exit 0, no skip, race report or timeout. These tests loaded
the real pinned extension and exercised enabled metadata-hash owner-trim proof
construction while substituting only the physical Ledger device boundary.
The C01 backend-hash mutation failed its named artifact-substitution assertion
in both normal and race modes; restored baselines passed. Author controls
C02–C08 were **not run** and are not claimed.

The [sealed independent receipt](/mnt/data/sn-testnet/owner-ledger-sdk-sol-20260930/evidence/RESULT.md)
and [69-entry manifest](/mnt/data/sn-testnet/owner-ledger-sdk-sol-20260930/evidence/SHA256SUMS)
retain exact source/artifact/tool/dependency fences, test binaries, raw logs,
exits and the C01 patch. The evidence manifest SHA-256 is
`229fd4bd6866206bb93c5d844227a93214efb2fd57d93a3db1a03924a0e200a1`;
the author handoff manifest SHA-256 is
`e94218226255bafdfe70eb72ca2c2a091d3ade9e94826f243e80753683196cee`.

This is a Linux software-artifact and synthetic-device-boundary result. The
owners' actual platform, physical Ledger/app/firmware, on-chain owner account
and derivation path, deployed runtime metadata digest and live owner authority
remain unqualified. No device signature, live RPC, transaction or deployment
was used.
