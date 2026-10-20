# Settlement v2 launch examples

These are templates, and two command sequences, for the steps of the settlement
v2 launch runbook, `VERSION2-LAUNCH.md`, that had none written. Replace every
`<...>` placeholder. A placeholder starting `O` and a number names an owner
input of the runbook's section 3; the others name a value that a step produces
or that is derived. In JSON and YAML a placeholder is a quoted string, also
where a number belongs: write numbers bare. Paths follow the runbook's
`/secure/ur-mainnet` layout. A test fills each file's placeholders with
synthetic values and runs it through the real parser.

Two rules hold for every file here. Work under `umask 077`:
`bootstrap-contracts` refuses an input that its group or others can read, and
`storage-prepare` refuses a directory that is not its owner's alone and an
ancestor that group or others can write. And a value called `sha256`, in a
file or on a `--…-sha256` flag, is `sha256:` followed by the 64 lowercase hex
digits; a bare digest is refused.

| File | Step | Owner inputs | Test |
| --- | --- | --- | --- |
| [settlement-v2.unsigned.example.json](settlement-v2.unsigned.example.json) | 8.3 | O2 to O12 | `TestSettlementV2LaunchPhaseConfigExample` (SV-8) |
| [SETTLEMENT-V2-FILL.md](SETTLEMENT-V2-FILL.md), the command sequence that fills the phase config | 8.3 | O2 to O12 | none: its `bash` blocks were run in order against `sn-mainnet` built from `29c8bd70` and `c844f308` |
| [settlement-v2-custody-request.example.json](settlement-v2-custody-request.example.json), [settlement-v2-custody-fence.example.json](settlement-v2-custody-fence.example.json) | 8.2 | O7 (run directory), O11 | `TestSettlementV2LaunchCustodyRequestExample` (SV-8) |
| [SETTLEMENT-V2-ADOPTION.md](SETTLEMENT-V2-ADOPTION.md), the command sequence | 10 | O14, O15 | `TestSettlementV2AdoptionDocumentIsTheVA6Sequence`, `validator/TestSettlementV2AdoptionDocumentNamesTheLoaderRefusals` (VA-6) |
| [validator-ledger-auditor.example.conf](validator-ledger-auditor.example.conf) | 10 | O10, O15 | `TestSettlementV2LaunchAuditorDropInExample`, `validator/TestSettlementV2LaunchAuditorDropInParses`, `validator/TestSettlementV2AdoptionDocumentRestartIsTheDropIn` (VA-3) |
| [claim-daemon-v2.example.yml](claim-daemon-v2.example.yml) | 13 | O2, O11, O22 | `miner/TestClaimV2LaunchConfigExample` (E-5) |
| [settlement-v2-watcher.example.service](settlement-v2-watcher.example.service), [settlement-v2-watcher.example.timer](settlement-v2-watcher.example.timer) | 13 | O2, O21 | `miner/onchain/TestV2WatcherUnitExample` (CL-1) |

The tests without a directory are in `settlement_v2_launch_examples_test.go`
and `settlement_v2_adoption_test.go`.

## Contract phase config (step 8.3)

- `<O2-genesis-hash>` and `<O2-evm-rpc-url>`, the route. An `http://` route
  is a literal IP and port; an `https://` one needs `tls_spki_sha256`, the
  leaf SPKI pin, added to `route`. `native_chain` is `Bittensor`, the name the
  Finney route reports.
- `<O3-runtime-source-commit>`, `<O3-spec-version>`, `<O3-runtime-code-hash>`
  and `<O3-runtime-metadata-hash>`: the runtime step 2 observes. The spec name
  is `node-subtensor`, and the transaction and state versions are 1.
- `<O4-deployer-address>` and `<O4-nonce>`, its next nonce N.
  `<O4-nonce+k>` is N + k.
- `<O6-escrow-value-wei>`: the burn cap O6 times 10^9.
- O7: `<O7-fee-cap-wei>` and `<O7-tip-cap-wei>` of every action;
  `<O7-maximum-total-wei>`, at least the sum of gas times fee cap plus the
  escrow value; `<O7-start-native-number>` and `<O7-start-native-hash>`, a
  finalized checkpoint; `<O7-valid-through-native>`, at most 7,200 blocks
  later; the `sha256:` pins `<O7-source-lock-hash>`,
  `<O7-cutover-evidence-hash>` and `<O7-custody-fence-hash>`; and the label
  `<O7-custody-id>`. The gas limits are BOOTSTRAP-CONTRACTS.md's starting
  limits, `maximum_attempts` is 8 and the run directory
  `/secure/ur-mainnet/run-v2`.
- `<release-file-sha256>`: `sha256:` and the SHA-256 of step 8.1's release
  file.
- `<approval-public-key-ed25519>`: the approval key's public key, which
  LAUNCH.md:375 names.

The derived fields, with D the deployer, N its nonce and each contract's
`creation` hex from the release file:

| Placeholder | Value |
| --- | --- |
| `<vault2-address>` | the CREATE address of D at N |
| `<vault2-create-data>` | `0x`, SettlementVault2's creation, then the ABI words `(uint16 25, bytes32 O5, bytes32 BLAKE2b-256("evm:" ‖ vault2's 20 bytes), address D, uint256 O12)`, O12 being 750000000000000 wei |
| `<escrow2-register-data>` | `registerEscrow(uint64 O6)` |
| `<settlement2-create-data>` | `0x` and Settlement2's creation |
| `<proxy2-create-data>` | `0x`, ERC1967Proxy's creation, then the ABI words `(address CREATE(D, N+2), bytes initialize(O8, O9, vault2, [O10], 1))` |
| `<vault2-link-data>` | `setCoordinatorOnce(address CREATE(D, N+3))` |
| `<batcher-create-data>` | `0x` and ClaimBatcher's creation |

Without O11, delete the `batcher-create` action and its 400,000 gas from the
total; the custody request may then drop `batcher-create.json`. Preview rebuilds
every derived word and refuses one that differs; `newLaunchPhase` in the test
builds them this way. Preview refuses the example while any placeholder is
left. It checks only that `native_chain` and `specName` are not empty, so
review those by eye: only the online steps compare them with the chain, after
the approval is signed.

[SETTLEMENT-V2-FILL.md](SETTLEMENT-V2-FILL.md) derives these fields with
`cast`, `jq`, `b2sum` and `xxd`, fills this example, runs `preview` and
compares its answer with the owner inputs. Preview accepts any well-formed
wrong value, so read that comparison before signing.

## Custody request and fence (step 8.2)

- `<mount-path>`, `<filesystem-uuid>` and `<filesystem-type>`: the mount that
  holds `/secure/ur-mainnet` (`findmnt -no TARGET,UUID,FSTYPE --target
  /secure/ur-mainnet`), ext4, xfs or btrfs.
- Before `plan`, under `umask 077`, create `run-v2` (empty), `run-v2-custody`
  and `run-v2-staging` under `/secure/ur-mainnet`, each mode 0700, and write
  the fence as `run-v2-custody/former-writer.json`. `<root-inode>` is the inode
  of `run-v2` (`stat -c %i`). `<fence-evidence>` says why no earlier writer
  exists; the parser cannot check that text.
- `<fence-file-sha256>`: `sha256:` and the fence file's SHA-256.

`root_path` is the phase config's `run_directory`, and the owners are its six
journals. Keep the request outside `run-v2`, for example as
`/secure/ur-mainnet/run-v2-preparation-request.json`. The test plans and
applies it, then opens vault2's journal with an offline `bootstrap-contracts
apply` under the declaration it returns.

## Validator auditor drop-in (step 10)

[SETTLEMENT-V2-ADOPTION.md](SETTLEMENT-V2-ADOPTION.md) gives step 10's command
sequence, which ends with this drop-in. Install it as
`/etc/systemd/system/sn-mainnet-validator-<role>.service.d/ledger-auditor.conf`,
run `systemctl daemon-reload` and restart the validator (step 10.5). The
second `ExecStart` is the command line the repository renders for the unit,
naming the adopted config (O15): `<validator-binary>`, `<adopted-config>`,
`<progress-file>`, `<validator-declaration>` and
`<validator-declaration-sha256>`, which keeps its `sha256:` prefix.
`<O10-auditor-key-file>` is the absolute
path of the auditor's hex secp256k1 key, whose address the initializer names.
Root owns it, its group is the service's GID and its mode 0440, in a directory
the service cannot write, as VALIDATOR-ACTIVATION.md:183-184 keeps the
runtime config.

Without the flag the validator sends no approval, challenge or demand
(`validator/run.go:142-143`), so no root is approved. The repository's repair
and activation controllers refuse a unit with any drop-in and require its
loaded `ExecStart` to be exactly the rendered one
(`repair_validator_host.go:370-402`; `TestRepairValidatorReleaseAndCgroupRefusal`),
so with this drop-in they refuse the validator.

## Claim daemon config (step 13)

- `<vault2-address>` and `<claim-batcher-address>`: step 8.5's
  `vault2_address` and `claim_batcher_address`, lowercase. Without O11 delete
  `batcher`.
- `<O2-evm-rpc-url>`, one line per RPC, and `<operator-api-url>`, the
  operator API.
- O22: `<O22-coldkey-ss58>`, one line per coldkey; `<O22-relayer-key-file>`,
  the relayer's hex secp256k1 key, which must exist when the daemon loads; and
  `<O22-state-dir>`.

The threshold is the `--threshold-rao` flag. The state directory is a durable
root of its own, prepared with a daemon-scope `storage-prepare` request whose
owner kind is `provider-claim-queue`; the daemon takes its declaration with
`--durable-volumes` and `--durable-volumes-sha256`. The loader accepts any
`api_url`, `rpc` and `state_dir` that is not empty, so a wrong one is refused
only when the daemon starts.

## Watcher service and timer (step 13)

Install both on a host other than the validator's as
`/etc/systemd/system/sn-mainnet-settlement-v2-watcher.service` and `.timer`,
and enable the timer. The service runs as the system user `snclaim-watcher`.

- `<vault2-address>`: step 8.5's `vault2_address`.
- `<O21-evm-rpc-url>` (repeat `--rpc` for failover) and `<operator-api-url>`.
- `<O21-watcher-key-file>`: the absolute path of the watcher's hex secp256k1
  key, funded for demands at 750,000 rao each plus gas. Root owns it, its group
  is `snclaim-watcher` and its mode 0440.

`--no_id=1` is the UR pool (LAUNCH.md:49), and `--chain_id=964` refuses an RPC
on another chain. The timer runs the watcher hourly. That is about 300 blocks
at 12 seconds, so a root's 14,400-block challenge window (48 hours) holds about
48 runs: the first within about 2% of the window, and later ones through an
outage of the host, its RPC or the operator API of a day or more. A run stops
demanding when the window closes, and the timer never starts a run while one
is still sending. A failed run leaves the service failed; alert on that.
