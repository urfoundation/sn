# Composed mainnet source candidate, 2026-09-27

Status: offline source composition qualified for the listed checks. Mainnet activation and the complete release manifest remain open. No signing, deployment, UID change or transaction occurred.

Detailed logs, the test-name allowlist, runner and built observation binary are
retained under `/mnt/data/sn-testnet/evidence/mainnet-source-lock-v2-20260927/`;
`SHA256SUMS` verifies that bundle. The repo carries this summary and the
source-lock JSON so the source identity survives outside this host.

The clean detached SN source is `f321ba7cc5beacf4891d81dd43c8633603dbc118`; server is `9f86073104c56e7e7cca802b97853db14fa8e044`; Connect is `c68689c420e45bcf07ecd4713e5de6e6bab5437f`. `qualified-combined-source.json` records all nine Git repositories, ten local Go replacements, `go1.26.6`, `go.mod`/`go.sum`, and the built tool. Its content hash is `0x2c3506182c02fa8bb19c90cd15fc9f0713a3b92f3e1357e52c2486b3d727fd33`; JSON SHA256 is `c20a0f2452a43564dcc51e0450957207f575190f91383c61214b2fa28a538cfb`; binary SHA256 is `294b3ab0cb166d9604013242120dab657478bb7c6f123aca406f7047fe99ee26`. The release layout has no `go.work`; all checks set `GOWORK=off`.

The candidate includes integrated settlement recovery `41f99608` (cherry-picked as `3b39de98`), signed owner-recycle admission `7e801952` (as `0b6aad64`), and canonical root receipts `36dfba6d` (as `16177cb6`). Their causal and component qualification is retained separately in `mainnet-contract-settlement-20260927/`, `mainnet-owner-recycle-admission-20260927/`, and `mainnet-root-receipts-20260927/` evidence directories.

## Composed qualification

| Check | Result | Retained log |
| --- | --- | --- |
| Cross-module compile: miner, validator, mainnet, chain, CRv4, sim-testnet | PASS, all six | `compile.log` |
| Full mainnet normal package | PASS, 75.898s | `mainnet-normal.log` |
| Validator owner-recycle/config/historical selector normal | PASS, 33.933s | `validator-normal.log` |
| Same validator selector under race detection | PASS, 169.316s | `validator-race.log` |
| Mainnet and validator vet | PASS, no diagnostics | `vet.log` |
| Exact 21 receipt/custody cases normal | PASS, 32.889s | `receipt-normal.log`, `receipt-tests.txt` |
| Exact 21 receipt/custody cases under race detection | PASS, 330.789s | `receipt-race.log`, `receipt-tests.txt` |

The original integration test regex overselected 232 sim-testnet cases and was canceled without using it as qualification. `run_receipt_tests.py` constructs an exact anchored selector from the 21 retained names; the normal and race reruns are the authoritative receipt results. The live R48 supervisor was not touched.

This lock covers clean Git sources and local Go replacements. It is not a lock of the complete generated and ignored artifacts, Solidity compiler inputs and bytecode, configuration, images, migration execution, policy signatures or deployed runtime. Component tests and source inspection do not establish the actual mainnet Wasm mapping, receipt trust boundary or economic 10%/90% outcome. MG-01, MG-02, MG-06, MG-08 and other production gates remain open. Snow VPN still served testnet ID945 and the testnet genesis at 10:51 UTC.
