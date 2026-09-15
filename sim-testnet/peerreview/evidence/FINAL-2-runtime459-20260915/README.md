# Runtime459 retained evidence

This bundle records the owned LAN observation at block **8,013,770** and the closed offline metadata probe. The runtime tuple is `node-subtensor/459/1/1`; provenance remains **`independent_rpc=false`**. The requests explicitly pin block `0xab776262d2a5ac8ac3acf0fad5e33ac3be431628d386457f903d85eb4e8d9225`. Genesis, block, version, and storage-hash request/response bytes are retained in `lan/`; large code and metadata responses are identified in `omitted-artifacts.json`.

The 2,524,988-byte code has SHA256 `c78bef5489149655254d5fb01a0e8c5c61846b0b322a54cb9ca2c86a14df8284` and BLAKE2b256 `558275958401c026fa4a4159466d49eabd08c761f0c801390593fcba91dee69b`, matching the pinned storage hash. The 336,358-byte metadata has SHA256 `52256b0b4a5c682e94e1d68a7b7a5dc1ba4114cfde4fb39057be808a8443673d`.

GitHub artifact **10418002729**, workflow **35022000542**, links the CI runtime to source commit `70378404b56c12a85bc8cd163aca2f32cf4d1b80`. The retained artifact API record gives ZIP digest `sha256:8c77e9d3d82384b85b0f2f21839207dd678c700966f500edeaae44821fd40024`. The existing capture obtained the ZIP through a public mirror after the unauthenticated API archive returned401; its digest matched that API record. The retained srtool digest links the extracted exact code to the source commit, production profile, srtool0.18.3 and Rust1.89.0. API and srtool records are copied unchanged under `upstream/`.

The existing offline probe finished **2026-09-15T21:36:26Z**, probe/outer exits **0/0**, with unchanged inputs. Its command description, exact stdout, terminal receipts and portable result record are under `offline-probe/`. It reproduced the exact version and metadata from the retained code using SDK revision `cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`.

`source-links.json` pins the accompanying committed artifact manifest, 71-path upstream source manifest, static metadata manifest, review, and gzip/base64 metadata fixture. Full Wasm, ZIP and metadata bytes are not duplicated here. `provenance.json` identifies every unchanged copied record; `SHA256SUMS` hashes all bundle files.

This is source/artifact and offline-probe evidence. It does not establish live campaign completion or new transaction finalization, and does not assert unchanged economics. Runtime459 changes share-pool, child-stake, root-pot and proxy behavior; the accompanying `docs/spec/runtime-459-audit.md` describes the reviewed scope. Original458 historical authority remains separate. Packaging ran no new RPC, test, build, probe or source mutation.
