# Startup interruption, September 15, 2026

Strict resume ran from 20:14:29 to 21:30:06 UTC and was deliberately
interrupted during local configuration rendering. Its joined exit is 137;
this is not a successful startup or campaign. The supervisor remained stopped.
The five watched plan/configuration/supervisor/identity files were retained;
only the journal changed among the six watched files. The executable and
release-lock comparisons passed. `state.*.sha256` gives hashes, not private files.

The journal projection records 13 intents and 12 verified postconditions.
One action, `evm.fund-deployer`, submitted a native Substrate extrinsic:
`0x800b72a73f4ce722a6d125541f2bcaf53593a218cfde7327a58171348a5f4279`.
It is extrinsic index 7 of 8 in block **8,013,647**, hash
`0x561e87243fc6d205cceb5369bdb50a342744951ddae440cfbb42c62a1ee40226`.
The owned LAN node reproduces the canonical block hash and full extrinsic;
`funding-extrinsic-hashes.tsv` contains BLAKE2b-256 hashes independently computed
with `xxd -r -p | b2sum -l 256`. The journal reports finality and successful
postcondition verification at 21:00:38. This bundle independently reproduces
transaction inclusion; it does not independently decode dispatch events or the
funding amount. The action name does not make this an EVM transaction. Its
finalized result must be retained when resuming.

At **21:25:14 UTC**, the native finalized head was **8,013,770**, hash
`0xab776262d2a5ac8ac3acf0fad5e33ac3be431628d386457f903d85eb4e8d9225`.
The adopted continuation's fixed end, 8,021,242, then left **7,472 blocks**,
less than the configured full-campaign requirement of **7,570**. Its native
runtime is **459**, transaction version 1, from a second query pinned to that
exact finalized hash. The earlier funding block still reports runtime **458**.
The genesis remains
`0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105`.

Startup also repeated the same signed ledger replay at nine call sites.
SIGTERM at 21:27:53 did not stop the current background-context replay;
SIGKILL at 21:30:06 terminated that exact local renderer after confirming no
children and the same pending configuration-render intent. The outer process
then joined and wrote its terminal receipts. The ledger cache, continuation
refresh, cancellation propagation and runtime459 compatibility fixes are being
qualified in parallel; no deployment or passing qualification is claimed here.

All chain requests in this bundle used `http://192.168.1.162:9944`:
**`independent_rpc=false`**. The raw requests and responses distinguish chain
observations from journal and process claims. Private plans, configuration,
keys and full journals are excluded. The actual observation time is recorded
in `runway-OBSERVATION.json`.
