# MG-03 bounded native StorageProof qualification

The exact qualified server source
`6201504ec18cd42b54083681c5dc5ca62fdcdb41`, tree
`6030ed7b20c8f6a3e400e21ebbcce38b4fc280cd`, was fast-forwarded from qualified
native finality capture `5ff7bf0264b775920049910896c23ec58862429f` onto
`codex/mainnet-server-hardening-20260927`. The non-force push, clean shared
checkout and exact remote head were verified. Its
[source contract](https://github.com/urnetwork/server/blob/6201504ec18cd42b54083681c5dc5ca62fdcdb41/strecovery/NATIVE-STORAGE-PROOFS.md)
adds an offline library verifier and private, byte-pinned witness loader.
No preexisting production Go file or Go module definition changed.

`VerifyReceiptNativeState` replays the existing receipt/finality verifier and
derives the state root from the original collection boundary's authenticated
native header. A certificate on a later descendant cannot substitute its state
root. The witness binds the exact collection, checkpoint, finality proof and
native block. The checkpoint remains a supplied, independently unapproved trust
input; proof consistency alone does not establish mainnet identity.

The raw Substrate `StorageProof` reader uses the pinned SDK's no-extension
Blake2-256 LayoutV0/LayoutV1 node encoding, including inline and externally
hashed values. It distinguishes absence from a present empty value, rejects
incomplete proofs and substituted roots/keys/values, and bounds counts, decoded
bytes, key length and traversal. Duplicate nodes/keys and malformed canonical
nodes fail. Bounded unused blobs and arbitrary node order remain valid for proof
unions. Runtime-aware decoding is absent; the result authenticates raw bytes only.

The independently rebuilt Rust oracle at server
`a2dd62b54ca5c122bd2f4fb28d9bbb26c6cec2ef`, tree
`40c0eca6b9fc97098ac9d992f71f6e77efd242f7`, uses the physical SDK checkout
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`, tree
`d2a88f2922e812acfea1791c6994065c450443a0`. Sol executed it offline with its
locked dependencies and verified all 18 raw-proof vectors; the rebuilt binary's
repeat output was byte-identical. The fixture SHA-256 is
`b875b9eb1aa497233f68bb4cae02b4331320fcaeeaaefbefe867658f2aebaecd`.
Fourteen cases distinguish raw storage proofs from `generate_trie_proof`
encoding; four trivial cases have identical bytes. The exact independent fixture
is retained unchanged in the Go source.

Sol qualified all 16 focused roots plus six adjacent finality/capture roots in
normal and race modes. All four streams had exact selected-root census, package
PASS and process exit zero, with no missing, failed, skipped or extra roots. All
21 isolated causal patches compiled and reached their exact named assertion,
root/package FAIL and process exit one in both modes: 42/42 discriminating
executions, including the repaired file-pin control. Author checks passed
compilation, vet and all 21 mutant compilations; Astra did not execute behavioral
tests. The Go graph
pins isolated Connect `b163f9dd`, SDK `516521fb` and SN `5198f6c9`. The author
source fence records 12 logical rows across 11 clean physical Git roots, including
the oracle and Rust SDK; nested SCTP shares Connect's root and npipe is unselected.
Sol's post-test fences remained clean and the module graph stayed byte-identical.
Astra independently checked all 188 receipt-manifest entries, the four positive
streams, all 42 exact causal outcomes and actual mutant source bytes, then
rechecked the physical roots, resolved Go graph, production file pins and oracle
bytes before integration. This is a scoped qualification, not a complete package
or production service coverage claim.

Two earlier attempts remain separate evidence. On `b0ca782a`, 14 of 15 focused
roots failed fixture setup because protocol digests use `sha256:<hex>` while the
oracle manifest uses bare hex. The remaining root passed and the package failed;
race, adjacent and causal runs did not follow. The fixture now uses explicit
standard SHA-256 for the external file pin and tests both digest domains.
On `4ffecfb3`, all 16 focused and six adjacent roots passed normal/race, but the
file-pin causal mutation passed its selected root in both modes: a malformed
bare-hex negative pin was rejected before the byte comparison. Only 40/42 causal
executions discriminated. The corrected fixture requires a canonical mismatched
`sha256:` pin and the exact byte-comparison error; a separate malformed-pin case
checks syntax rejection. Both production verifier files are byte-identical
across these attempts. The complete independent rerun is sealed on corrected
source `6201504e`; no source claims were borrowed from the earlier results.

| Retained receipt | SHA-256 |
| --- | --- |
| [Corrected Sol result](/mnt/data/sn-testnet/qualification/native-storage-proof-verifier-sol-20260929/frozen-620/SOL-RESULT.md) | `41169cc7a251a291b0c717da320a53b2d379dc83d887a0697cf61ebaa402af77` |
| [188-file qualification manifest](/mnt/data/sn-testnet/qualification/native-storage-proof-verifier-sol-20260929/frozen-620/SHA256SUMS) | `d57366a984447123997716d315a19c0a33ef79506785ab326aeebb6f343da35a` |
| [Independent oracle manifest](/mnt/data/sn-testnet/qualification/native-storage-proof-oracle-sol-20260929/SHA256SUMS) | `ba84714e74239278f8f9f68d379b664eaf31dfa8213a9076ff84a50b1495164a` |
| [Corrected author handoff](/home/by/urnetwork/temp/server-native-storage-proof-20260929/pin-mismatch-evidence/HANDOFF.md) | `94763f190a1759c5d4f650f386d15c7b20ac7d222b21f8e72c7205a7839bdbdd` |
| [144-file author manifest](/home/by/urnetwork/temp/server-native-storage-proof-20260929/pin-mismatch-evidence/SHA256SUMS) | `403bb3f52a352d66060592564ec169a76ff58f74952996424a1bf17e6204bf5b` |
| [Production byte-identity audit](/home/by/urnetwork/temp/server-native-storage-proof-20260929/pin-mismatch-evidence/PRODUCTION-IDENTITY.json) | `d60276dfd1866414049d9f834b8b56ed4df7387692f067b28fa3860c8418a20d` |
| [b0ca fixture anomaly manifest](/mnt/data/sn-testnet/qualification/native-storage-proof-verifier-sol-20260929/frozen-b0ca/B0CA-SHA256SUMS) | `a296fd109912ee5dd561499f72ae2cb0d9210c39187c1650dd8429211784de06` |
| [4ffe partial/anomaly manifest](/mnt/data/sn-testnet/qualification/native-storage-proof-verifier-sol-20260929/frozen-4ffe/SHA256SUMS) | `3671eb5eddbfb2c2418b4f226e31e8d7ba3945f5b135d7ad93e81d21e66efd64` |

This slice authenticates raw reads at one original collection boundary. It does
not yet support earlier receipt roots or linked parent roots for historical
runtime/fee evidence, collect storage proofs, decode account nonces or native
debits/refunds, or expose a new CLI. All checkpoint, genesis, runtime decoding,
live finality, owner-window, global-custody, actual-fee, fee-exposure and spending
authority flags remain false. Original receipt histories and null actual fees
are preserved. Runtime-qualified attribution, approved live identity/checkpoint,
service adoption, release composition and live custody remain MG-03/PF-03 gates.
The separate SN SDK coverage remains 543/618 package-backed race roots with 75
pending, and MG08 adjacent coverage remains 150/270 roots in both modes with 120
unrun. No full-suite or mainnet activation claim follows from this work.
