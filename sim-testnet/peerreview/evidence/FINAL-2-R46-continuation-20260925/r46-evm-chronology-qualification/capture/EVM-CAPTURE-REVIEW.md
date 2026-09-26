The bounded LAN capture completed against source `92c14e3f8bdd2419734d831da00cc0483f1bc993`; its following timeline verification failed on the known legacy-plan precondition. This is preserved as a failed replay with a complete reusable EVM capture, not final acceptance.

The helper authenticated the copied sealed V3 foundation: 125 plans, 59,979 journal entries, and 1,371 signed relay requests. Four legacy predecessors have no deployment manifest; their only finalized coordinator operation is implementation preparation. The revised query census retains those plans and all their actions while requiring coverage for every finalized proxy initialization or activation. Its original zero-proxy condition failed a synthetic control; the corrected control, two refusal controls, and two transport controls passed. Production source was unchanged.

The capture queried 196 fixed intervals covering blocks 7,888,670–8,084,595 at the two authenticated coordinator proxies, filtering only `Upgraded(address)`. All 11 events matched successful canonical transaction receipts and exactly 11 finalized journal transitions: two initializations, eight ordinary activations, and the signed rounding repair. Historical slot and runtime-code reads authenticated both initialization baselines. Original campaign block 8,084,596 and terminal block 8,086,545 matched their sealed hashes and were rechecked after capture.

The durable transcript contains 236 read exchanges: 196 log queries, 22 headers, 11 receipts, two storage-slot reads, four code reads, and one chain-ID read. There were no transport or RPC errors and no retries. The exact endpoint was the authenticated LAN authority. The 512-request diagnostic bound was not reached. No wallet, chain-write, native-state, or validator-capture method was invoked.

The old production timeline then refused `historical coordinator timeline has an unapproved plan`. The run ended after 1,016.72 seconds. The capture and transcript are copied under exact hashes into the separate offline replay input directory; the qualified predeployment fix can consume them without repeating LAN reads.

| Artifact | SHA-256 |
| --- | --- |
| Qualification index | `70091e1fa7389c2d44382b5bdae8c6b27dc58577340df3215cb4de55e178adca` |
| Capture | `cae745c3c0f29aa81fac8d61232052d7d2fcbe7843c0f28e6b01568020328dcd` |
| RPC transcript | `136800ef55a703cee56d7cbee00f1ab5369fb6349c1ce35fa571db68671ad894` |
| Failed replay receipt | `cc75941dee87958a013bd4cf808f522cdd116f8fbe7376c5bc4f0e4683986312` |
| Input manifest | `0a7d2d8535afdae2f1acca21bfb9ca8ccc7bf98b1067b2bb6f97c83c14fb58a9` |
| Capture binary | `ecf0f3d590256d133d189241e484f78009d85cdaca1a21131e04554599d0cffc` |

This receipt covers historical coordinator chronology inputs only. It does not establish full canonical-contract/native-reward acceptance, validator-source acceptance, or a new R46 scenario verdict. The original failed R46 result and sealed V3 report remain unchanged.
