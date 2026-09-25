# R44 sealed outcome and scoped exception

This bundle copies the exact original-owner result and read-only post-owner
evidence captured on 2026-09-25. `SHA256SUMS` binds each copied file to the
retained originals under
`/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/`.
The result records 85 failed assertions of 182, `result=fail`,
`final_acceptance=false`, and end finalized block 8,082,861, hash
`0xac30508c90362291e2326eb14b174a1ecc1586223766ef28e8b774e93d885fc9`.
Read `owner-result.json` as the original owner's decision; the other files
cannot override it.

`known-exception.json` names the approved lifecycle bypass. The missing
terminal-effective mutation leaves the companion filter's conditional restore
unproved. `hard-restore-observation.json` and
`signed-hard-restore.evidence.json` retain its scheduled restore at finalized
block 8,082,634 and `RestoreConditionMet=false`. The signed file is copied
byte-for-byte; this bundle does not independently verify its signature.

`post-owner-diagnostic.json` is a read-only collector result: 14 pass, 14 fail,
one exception, eight unavailable. It ran partly after the official fleet stop
and successor-plan archive, so later source-health and receipt checks can
reflect changed availability. The owner result, pre-stop fourth diagnostic
(external SHA-256 `affad93c70b533e1a55f6e439dcec89f5243f5957d6ad76dfbd7ed984678d736`),
and this post-owner report should be read as separate evidence cuts.

The on-chain block/hash references can be rechecked against the LAN archive
RPC at `http://192.168.1.162:9944` using `chain_getBlockHash` or
`chain_getHeader`; these files do not claim independent public-RPC
reproduction of the R44 run. Off-chain assertion messages remain original
owner claims unless separately corroborated by source artifacts.
