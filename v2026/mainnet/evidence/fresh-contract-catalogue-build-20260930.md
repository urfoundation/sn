# Fresh contract catalogue qualification

The frozen builder source `f794008c26e220e46141f7ec134fa3ed56aa0a11`
(tree `cd181c8634016b2c3abbaec3af046b1891fe9090`, parent `4c8c7b2c`)
produced an explicit fresh-catalogue composition from clean physical clones.
All seventeen executable builds, the full production/probe contract source
build and the catalogue checks completed with exit 0. Eight image contexts
were prepared; no OCI image was built or read back.

The [candidate manifest](/mnt/data/sn-testnet/mainnet-release-composition-20260930/candidate-fresh-a/manifest.json)
has SHA256 `1706c101364c7390e4b6bce1bb1984ece57f9b7416efd7d08772383186c4dc89`
and domain-separated content hash
`sha256:254ade3e2dd642659f5332f682c3e5cb9fefd0e6b449f24db7cc76d6bc837627`.
Its config selects `contract_catalog="fresh"`; all five selected creation/runtime
pairs exactly match the rebuilt Foundry compiler artifacts, making
`source_to_bytecode_exact=true`. All 170 declared artifact paths passed hash and
length readback. The seventeen-role census matches candidate-c.

| Catalogue | SHA256 |
| --- | --- |
| Selected `inputs/contracts-release.json` | `60873ba2a29537accee9c2c5067c71321bb5b9f5d91e99ea32605bf56039cebd` |
| Historical `inputs/contracts-retained.json` | `2a54ebc58c248e80daf2fbead0fd9e97746aa3ebfd359d3e82868060b845b85d` |
| Unchanged historical `inputs/contracts_gen.go` | `91d4c21837321d627e88af7eb98c8eee125384d2a5d96c2fe70c91c989b8398e` |

All five ABIs, constructors, normalized storage-layout hashes and semantic
immutable references match the historical catalogue. Coordinator and
ValidatorEvidence select new compiler metadata digests; the other three
contracts retain identical bytes. Direct comparison confines each historical
creation/runtime difference to the 32-byte IPFS digest. Coordinator runtime
remains 24,564 bytes, twelve below EIP-170's limit. The old catalogue, checked-in
binding and candidate-c remain byte-identical to their prior captures.

The [independent Sol receipt](/mnt/data/sn-testnet/fresh-catalogue-sol-20260930/evidence/RESULT.md)
qualifies all 31 builder roots in normal and race modes, vet, four independent
single-guard causal controls and the complete real candidate's manifest,
artifacts, command exits and five contract comparisons. Its SHA256SUMS digest is
`2e4b5968ffb74b024c3d5899a01d13a581821f95ba554e449d79c76f1a715f37`.
The [author handoff](/mnt/data/sn-testnet/mainnet-release-composition-20260930/evidence/fresh-catalogue/HANDOFF.md)
retains the separate author logs, eight normal/race causal controls,
`catalogue-proof.json`, `historical-metadata-proof.json`, clone identities and
the exact config. This is independent inspection of the local composition,
not a second independent compiler build.

The [path/hash reference](/mnt/data/sn-testnet/mainnet-release-composition-20260930/evidence/fresh-catalogue/artifacts-reference.json)
is reviewable input to the future unsigned bootstrap contract specification's
`artifacts` field. No signed mainnet deployment plan has been evidenced. The
release owner must still check for any externally held signed commitment and
preserve/reconcile it before selection changes. The historical catalogue is
release/testnet material, not proof of mainnet authority. The metadata-equivalence
proposal remains an unselected conditional fallback.

The manifest keeps `release_complete`, `reproducibility_verified`,
`source_to_image_verified` and `deployment_approved` false. Independent rebuild,
compiler-installation attestation, OCI build/readback, approved production
configuration/policy, actual mainnet identity and applicable launch gates remain
open. This work performed no signing, chain operation, image push or deployment.
