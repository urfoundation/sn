# Miner claim queue retained-byte qualification

Frozen successor `4a9b9e6a963ffa3fc38ba9948e44ca2612fe33ef` adds a 16 MiB
per-member retained queue limit to qualified ownership source
`b7e84b2f2dd2bf8cdb98c656e6e37da6ad848061`. Reads check the opened regular
file before allocation and use a limit-plus-one sentinel to detect later
growth. Publication refuses oversized payloads and oversized retained regular
files before temporary creation; repeated and changed saves preserve the
original bytes after a prior acknowledgement is invalidated.

Sol's [raw receipt](/mnt/data/sn-testnet/qualification/sol-claim-queue-capacity-20260929/RESULT.md)
(SHA-256 `3a69c8ee50aa6b4d4907822d2f849e7fe4690a21fbc2d621ad25ece54b07b405`)
records all six new and 76 affected roots passing normally and with race
detection, all 292 `./miner` roots passing in plain normal mode, and `go vet`
passing. Three independent controls fail their assigned roots in both modes:
removing the read sentinel, bypassing payload admission, and bypassing the
retained-file size guard. Refusal tests compare complete retained byte hashes
and preserve signed fields; startup refusal precedes custody and network work.
Source and dependency fences stayed clean. The real-schema sizing fixture
retains 1,024 signed epochs in 4,403,140 bytes, below half the limit.

Linux and FreeBSD test binaries compiled. Full miner qualification used plain
mode for the [preexisting stdout/stderr test premise](miner-claim-queue-owner-qualification-20260929.md);
the original JSON-mode failure and its parent/candidate controls remain retained.

The exact patches were integrated into `codex/mainnet-hardening-20260927` as
`ebd97a724ed115e0b6a9155d8a71c156a93a4cf6` (ownership), then
`f7a06f45e83adae67692faec2d64c3f4363b24f5` (retained-byte limit). All 14 changed
miner Go product/test files match the frozen successor byte-for-byte. Existing
mainnet evidence CREATE source and documentation were preserved. This receipt
and the status links are documentation-only additions after that comparison.

This qualifies local source behavior. It supplies no live route, key,
transaction, deployment or production acceptance. Global nonce ownership,
cross-host custody, aggregate fleet memory, large epoch-jump discovery,
oversized in-memory encoding, storage recovery and composed release
qualification remain separate work. No retained queue was compacted or migrated.
