# Finalized native/EVM observation

`finalized-mapping` captures an owned RPC's finalized native header and the
EVM header committed in its Frontier digest. It authenticates both header
hashes and corroborates canonicality without assuming equal block numbers.
It is signer-free and always emits `admission: unapproved_observation`.

```sh
sn-mainnet finalized-mapping --rpc http://rpc.example:9944
```

Supply `--expected-chain`, `--expected-genesis` and `--expected-evm-chain-id`
together to check independently approved network identity before EVM reads.
The command never obtains approval from the endpoint. The default retry budget
is 300 seconds for the entire observation; the configurable bound is 60 seconds
to 15 minutes. Transient transport/overload/timeouts reuse the shared bounded
read policy. Cancellation or error publishes no partial proof.

## Verification and retained evidence

1. Read the finalized native hash, complete header and runtime identity. Hash
   the native SCALE header and confirm its canonical native height.
2. Re-read that exact header, normalize equivalent hex spelling and require
   the same full header commitment. Parse exactly one `Consensus(fron, PostLog)`
   digest. The supported payloads are variant1 `Hashes { block_hash,
   transaction_hashes: Vec<H256> }` and variant3 `BlockHash(H256)`. Both compact
   lengths and all payload bytes are consumed; duplicate logs or malformed
   vectors fail. Other variants remain mapping-specific compatibility gates.
3. Fetch `debug_getRawHeader` with the object selector
   `{"blockHash":"<digest EVM hash>","requireCanonical":true}`. Require exact
   Keccak-256 of the retained RLP to equal the digest's block hash, then decode
   the reviewed fifteen-field Frontier header and obtain its own EVM number.
   Only an unsupported method/selector (`-32601`/`-32602`) permits the bounded
   public reconstruction below. Null or corrupt raw evidence does not.
4. Query `eth_getBlockByNumber(<decoded EVM number>, false)` and require the
   same EVM hash/number. Variant1 also requires the complete ordered transaction
   hash vector to agree with the native commitment. Recheck native genesis,
   native canonical hash and both network names/IDs, then repeat the EVM
   canonical lookup to catch a changed index or proxy view during the sample.
5. Retain the complete native header/digests, exact raw EVM RLP hex, both block
   numbers/hashes, full observed runtime tuple and a domain-separated SHA256
   envelope. An independent reader can reproduce the native and EVM hashes.

No native-height lookup selects an EVM candidate. The deterministic fixture
uses native height 100 and EVM height 37 to enforce that distinction.

Exact EVM header bytes remain essential. When the raw method or object selector
is unsupported, the verifier requests
`eth_getBlockByHash(<digest EVM hash>, false)`. The reviewed Frontier renderer
divides its stored millisecond timestamp by 1000, omits the pallet's zero
`mix_hash`, and adds runtime-derived `baseFeePerGas` outside the stored header.
The verifier reconstructs only that fixed fifteen-field profile, trying at
most 1,000 millisecond remainders within the displayed second. The complete
candidate RLP must hash to the native digest commitment; the RPC's `hash` echo
alone is insufficient. The same native/network and twice-repeated canonical
EVM number/hash/vector checks then apply.

Every displayed serialized field is required, including `miner`, `nonce` and
empty `extraData`; missing or null values are never defaulted. Omitted
`mixHash` is the one reviewed exception and supplies only zero. An explicit
`mixHash` is used verbatim, and an optional `author` must equal `miner`.
`baseFeePerGas` is always an external annotation in this profile, never a
conditional header-layout choice. Unknown fields and non-null later-fork
header extensions are refused. There is no timestamp-unit guess, native-height
selection, alternate header layout or nonzero omitted-field search. Overflow,
malformed values and a failure to reproduce the exact commitment stop capture.

The recovered RLP uses the existing `frontier-legacy-rlp15` format and unchanged
envelope schemas. Independent consumers replay its bytes exactly as they do a
raw response. Recovery proves those bytes against the digest; it adds no runtime
source, consensus, execution or signing authority.

## Exact reviewed source

Codec reference: Subtensor commit
`67dcf7f791dc495064c293f080a0702cb433e51e`:

- [Frontier consensus log definitions](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/vendor/frontier/primitives/consensus/src/lib.rs):
  engine `fron`, PostLog indices 1/3, ordered transaction-hash vector and log
  uniqueness.
- [Ethereum pallet](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/vendor/frontier/frame/ethereum/src/lib.rs):
  `on_finalize` calls `store_block`, which stores the Ethereum block and emits
  its corresponding post-log. The stored partial header fixes `mix_hash` to
  zero and uses the millisecond timestamp directly.
- [Raw debug RPC](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/vendor/frontier/client/rpc/src/debug.rs):
  `raw_header` returns the stored header's `rlp_bytes()`.
- [Ethereum JSON renderer](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/vendor/frontier/client/rpc/src/eth/mod.rs):
  `rich_block_build` converts timestamp units and adds the separate base fee.

This reference describes the parsed wire profile. It does **not** prove that
the observed runtime Wasm was compiled from that commit. The output explicitly
sets `runtime_source_proven: false`; source-to-Wasm qualification and runtime
approval remain separate mainnet launch gates.

## Availability and limits

The mapping-specific read profile alone permits `debug_getRawHeader`,
`eth_getBlockByHash` and `eth_getBlockByNumber`. None is added to ordinary
identity/storage reader admission. Mutation methods remain refused. RPC replies
are bounded to 1 MiB, exact RLP to 64 KiB, and native transaction-hash vectors to
2048 with the existing tighter 64 KiB-per-digest limit. Public recovery checks
cancellation during its bounded timestamp search. Retained native headers keep
their existing count/byte bounds.

Exit 0 means a complete unapproved observation; exit 2 is invalid arguments;
exit 3 is an independently supplied network mismatch; exit 4 means the mapping
is unavailable because both header capabilities, required projection fields,
the reviewed post-log/header profile or the canonical block are missing.
Other integrity/read failures, including a public projection that cannot
reproduce its native-committed hash, exit 1. The public fallback requires an
explicit unsupported raw method/selector; null raw replies, malformed bytes,
unrelated RPC errors and transport failures retain their original refusal.

This header-only fallback applies to `finalized-mapping` and its composed
`finalized-snapshot` reader. [Safe archive capture](SAFE-HISTORY-CAPTURE.md)
continues to require raw header, complete block and receipt capabilities; a
public block header cannot replace those body and receipt witnesses.

The native finalized selection and canonical EVM lookup are still assertions
of the owned RPC. The proof authenticates the two linked header commitments;
it is not a GRANDPA proof, storage proof, transaction-body/receipt proof, source
attestation, or mainnet readiness claim. Separate canonical reads are not an
atomic consensus snapshot. A future Frontier post-log or EVM header codec can
block this mapping command without adding its unsupported profile to general
identity checks.

Owned Snow exposed the required methods on 2026-09-27, and the exact hash-object
selector returned hash-reproducible raw RLP. Its observed EVM chain remains 945
(testnet), so these successful checks grant no mainnet authority.
