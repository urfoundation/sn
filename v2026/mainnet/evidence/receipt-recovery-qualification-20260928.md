# Complete receipt and miner recovery qualification

Terra medium qualified the sealed receipt source `9618a1cb` with its real-RPC
wire fixture correction `ed412c6c`, then miner continuation source `de034a4b`.
The integration branch contains these as `a50b5096`, `3fe28e19` and `d5af5654`.
This is component source qualification using synthetic chain responses and
fixture signatures. It establishes no live transaction, deployment or mainnet
acceptance.

Complete canonical headers and ordered extrinsics commitments now precede
receipt inclusion **and absence**. The miner uses the same reader before
persisting a native scan cursor. A signed semantic proof version permits safe
prefix reuse; legacy cursors without that proof require one rescan. Finalized
outcomes, exact signed bytes and their original authority remain retained.

| Selection | Normal | Race |
| --- | --- | --- |
| Corrected `crv4` receipt/location/verification selector | Pass, 0.026s | Pass, 1.156s |
| Corrected validator receipt/history selector | Pass, 36.602s | Pass, 258.810s |
| Validator shared-fixture adjacency selector | Pass, 28.928s | Prior unchanged source qualification reused |
| Miner candidate `crv4 ^TestReceipt` | 10 observed roots passed, 0.068s | 10 observed roots passed, 1.243s |
| Miner `^TestFleet(Mainnet\|Recovery)` | 63 observed roots passed, 65.939s | 63 observed roots passed, 589.249s |

The earlier corrected receipt logs are nonverbose: their rows establish
selector/package completion, not an observed per-root census. Miner captures
retain JSON events, compiled binaries, actual package working directories and
build information. No selected positive miner root skipped. Vet and source,
manifest and module-resolution checks passed.

Both predecessor controls failed for the intended reason in normal and race
modes. Original `a7e01568:crv4/chain.go` accepted a null response as absence;
original `ed412c6c:miner/fleet_recovery_native.go` advanced the durable attempt
after a truncated body. Neither control depends on a build failure, panic,
timeout or race. The positive malformed-body test also checks that the actual
body read was reached.

The first receipt candidate exposed invalid unprefixed-hex SDK fixture
serialization. Fixtures now use actual `0x` RPC quantities while retaining
their genuine SCALE commitments. The original failed captures are preserved.
Two manifest invocation mistakes are also retained as harness failures: an
appended manifest was restored, and an original-source check was rerun from
its correct worktree. Neither required changing production source or repeating
an unaffected passing body.

At clean integrated SN `d5af565455155347f5b20f9867940be0eaeffc87`, the four
packages `crv4`, `miner`, `validator` and `mainnet` compile together with
`go test -run '^$'`. This intentionally executes no test bodies. Its source
fences passed. Component dependencies resolved to the recorded physical active
checkouts, including Connect `358cefae`, server `0633780c` and Warp `7498864c`;
this is not the final frozen release composition.

Raw captures:

- `/mnt/data/sn-testnet/qualification/production-reconciliation-20260928/corrected-wire`
- `/mnt/data/sn-testnet/qualification/miner-receipt-prefix-20260928/captures`

The receipt source manifest SHA-256 is
`1992e10a15c89d4fbc30c3428948063b08cbfe123692e65562a9630c03e6236a`.
The unchanged supplied miner manifest is
`492268b37b45d9ce584e70ecc07473b3b60f18596d2690e54e620ddb77eb10b0`;
its separately extended source fence is
`4c0bb682fd7ed7e42efb3c3573accdd2cb734da8c811f2879202f7bb4c6f300e`.

PH-03 remains open for production read waits, same-boundary nonce comparison,
bounded durable scan chunks and concrete nonempty production continuation.
Recognizing a known trie layout grants no runtime execution authority.
