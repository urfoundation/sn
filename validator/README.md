# Run an independent SN25 validator

This guide is for a third party running its own validator, including Yuma. Use
your own hotkey, credentials and persistent state. A new validator starts a new
evidence history; it does not need the URnetwork validator's state, historical
recovery files, coldkey or approval key.

The validator measures providers, publishes its signed evidence, and submits
native commit-reveal weights. Measurement-only mode does not submit weights.

The production setup below requires your own subnet 25 hotkey with sufficient
native stake and a validator permit. Supply it through
`configure --hotkey-seed-file`; no identity or key from an existing URnetwork
validator is required. If the hotkey is new, complete native enrollment with
your wallet tools before proceeding to activation.

## Install

Use the release archive for your machine: `linux-amd64` for an x86-64 server or
`linux-arm64` for an Arm server. Extract it and check the installed version:

```sh
tar -xzf validator-RELEASE-linux-amd64.tar.gz
./validator --version
./validator --help
```

The archive contains the executable, this guide and the inspectable bundled
public mainnet profile. Use the archive accompanying this handoff; an older
binary that lacks `configure --help` does not provide this setup flow.
Running a release binary does not require Go or sibling source checkouts.
Maintainers can build archives
from the normal source workspace with:

```sh
make -C validator package VALIDATOR_VERSION=RELEASE GOOS_ARCH='linux/amd64 linux/arm64'
```

Keep the configuration, key directory and mutable state on persistent storage.
Production storage must support the durable-volume preparation described below;
an empty directory or a container's temporary writable layer is insufficient.

## Inputs the runner supplies

Every command below accepts file paths rather than secret key contents. Do not
send seed files to URnetwork or put private seeds in command arguments.

| Input | CLI option | Meaning |
| --- | --- | --- |
| Validator identity | `configure --validator-id=<positive integer>` | Choose a positive ID for this independent validator and retain it. It is separate from the native UID resolved by hotkey. |
| Signing hotkey | `configure --hotkey-seed-file=<absolute path>` | Your sr25519 hotkey seed. Use the hotkey registered on subnet 25. |
| Persistent state | `configure --state-dir=<absolute path>` | A new, private state directory for this validator. Preserve it across restarts and upgrades. |
| Evidence and scratch | `configure --evidence-dir=<absolute path>` | Separate persistent evidence and scratch tree. It must be outside state and operator state. Omitted: `<state-dir>.evidence`. |
| Measurement key | `configure --client-key-seed-file=<absolute path>` | Your operator 1 Ed25519 measurement seed. The setup below keeps it outside the fresh runtime tree. |
| Configuration approval key | `configure --approval-key-file=<absolute path>` | Your private Ed25519 approval seed; used during setup to sign your configuration. It is separate from the native hotkey. |
| Configuration output | `configure --out=<new absolute path>` | Where to write the complete initial configuration and its adjacent signed approval. |
| EVM RPC | `configure --rpc=<https URL>` | Optional override for the bundled public EVM RPC route. |
| Native RPC | `configure --substrate=<wss URL>` | Optional override for the bundled public Subtensor route. It must support subscriptions and historical metadata, runtime code and state reads at the configuration and activation anchors. |
| Approval lifetime | `configure --approval-valid-for=<duration>` | Optional finite configuration approval window; default `720h` (30 days at the nominal native block rate). |
| Prepared storage | `--durable-volumes=<absolute path>` and `--durable-volumes-sha256=<sha256:digest>` | The external declaration returned by storage preparation. Activation and the daemon require a daemon declaration. |
| Native UID | `storage-prepare request --validator-uid=<integer>` | The registered hotkey's native UID, including zero. `status --config` reports it; activation independently verifies it. |
| Persistent filesystem | `storage-prepare request --mount-path=<path> --filesystem-uuid=<UUID> --filesystem-type=<type>` | Actual mount facts from the runner's host. Linux: `findmnt -no TARGET,UUID,FSTYPE --target <runtime-root>`. |
| Activation gas payer | `activate --relayer_key_file=<absolute path>` | Your funded secp256k1 EVM key. It pays publication gas and grants no validator authority. |

The hotkey must have the native stake and validator permit required by the live
chain. Registration alone does not establish eligibility. Native registration
and staking are optional setup operations if your hotkey already qualifies.

`configure` uses hyphenated option names. Existing commands such as `init` use
the underscore spellings shown in their examples; `--help` lists the accepted
spelling for each command.

## Create or select your keys

Set paths for your machine. Keep approval and private key files outside mutable
protocol state, and run subsequent commands as the account that will own that
state:

```sh
RUNTIME_DIR=/srv/urnetwork-validator/runtime
STATE_DIR="$RUNTIME_DIR/state"
EVIDENCE_DIR="$RUNTIME_DIR/evidence"
KEY_DIR=/srv/urnetwork-validator/keys
CONFIG_DIR=/srv/urnetwork-validator/config
HOTKEY_FILE="$KEY_DIR/hotkey.seed"
APPROVAL_KEY_FILE="$KEY_DIR/approval.seed"
INITIAL_CONFIG="$CONFIG_DIR/initial.yml"
ACTIVATION_DIR="$RUNTIME_DIR/activation"
RUN_CONFIG="$ACTIVATION_DIR/validator.yml"
PROGRESS_FILE="$CONFIG_DIR/progress.json"
install -d -m 700 "$RUNTIME_DIR" "$KEY_DIR" "$CONFIG_DIR" "$CONFIG_DIR/storage"
```

Select your own positive `VALIDATOR_ID`; it is a local configuration identity,
not your subnet UID. If you already have a registered hotkey, place its private
seed at `HOTKEY_FILE` with mode 0600. Run `init` in either case: it reuses an
existing hotkey, or creates one when missing, and creates the measurement and
approval keys needed by the next steps:

```sh
./validator init --state_dir="$KEY_DIR" \
  --hotkey_seed_file="$HOTKEY_FILE" --no_id=1 \
  --approval-key-file="$APPROVAL_KEY_FILE"
```

`init` reports only public keys and paths. It creates missing private seeds with
mode 0600 and reuses existing seeds. It also creates or reuses the independent
approval seed. A newly created hotkey still needs native
registration, stake and a permit before production startup.

The native hotkey reader accepts a raw 32-byte seed or a 64-character hex seed
with an optional `0x` prefix. Supply a seed file, rather than an encrypted wallet
JSON file. The measurement seed written by `init` is a raw 32-byte Ed25519 seed.

The approval key reader accepts a raw 32-byte seed or its 64-character hex
encoding. Keep it available for the activation setup, then keep it offline;
`validator run` does not need it.

Native enrollment is a separate prerequisite; this archive does not prepare a
coldkey wallet. The `register` and `stake add` commands shown by `--help` require
their own owner-local journal declaration. The daemon declaration below cannot
be used for those transactions.

## Generate the current mainnet configuration

Supply your own positive validator ID:

```sh
./validator configure \
  --validator-id="$VALIDATOR_ID" \
  --state-dir="$STATE_DIR" \
  --evidence-dir="$EVIDENCE_DIR" \
  --hotkey-seed-file="$HOTKEY_FILE" \
  --client-key-seed-file="$KEY_DIR/no-1/client.key" \
  --approval-key-file="$APPROVAL_KEY_FILE" \
  --out="$INITIAL_CONFIG"
```

To use your own RPC services, add `--rpc="$EVM_RPC"` and
`--substrate="$NATIVE_RPC"`. The public defaults are
`https://archive.chain.opentensor.ai` and
`wss://archive.chain.opentensor.ai`. Both routes need historical state;
a route that serves only the current head is insufficient.
The generated configuration selects the bundled
mainnet deployment, policy, current runtime and V2 contracts. It records your
own identity and paths and signs their complete configuration. Generation reads
chain state; it does not register a hotkey, publish activation or submit weights.

The bundled public profile is `mainnet-profile.json`. Identity, keys, approval
and local paths are supplied through the CLI, rather than copied from an
existing validator. Always run with your generated `--config`: omitting it
selects the intentionally incomplete `release.yml` template.

The initial configuration has activation-pending evidence inputs. It is used for
setup. After activation, start the separately rendered `RUN_CONFIG`.

## Prepare fresh durable storage

This step is for a new, empty `RUNTIME_DIR` only. Keep keys, initial configuration,
preparation files and any old validator state outside it. Leave `ACTIVATION_DIR`
absent until activation writes it after preparation. This step prepares the
parent directories, an identity-bound empty operator ledger, and evidence and
scratch directories together. It does not reset or migrate retained state.

First read your registered hotkey's UID:

```sh
./validator status --config="$INITIAL_CONFIG"
```

Set `VALIDATOR_UID` from that registration result. Obtain the persistent
filesystem facts from `findmnt`, and supply them explicitly as `MOUNT_PATH`,
`FILESYSTEM_UUID` and `FILESYSTEM_TYPE`. An overlay filesystem, temporary
container layer, missing UUID or unsupported filesystem will be refused by the
normal physical storage checks.

Require an actual registration line showing `permit=true, eligible=true`
before activation. `status` also reports read failures without a failing exit
code, so exit status alone is insufficient. If your hotkey is not registered or
eligible, finish native registration and staking with your normal wallet tools,
then repeat `status`. Creating a hotkey does not grant a validator permit.

```sh
REQUEST="$CONFIG_DIR/storage/request.json"
PLAN="$CONFIG_DIR/storage/plan.json"
RESULT="$CONFIG_DIR/storage/result.json"

./validator storage-prepare request --config="$INITIAL_CONFIG" \
  --root-dir="$RUNTIME_DIR" --validator-uid="$VALIDATOR_UID" \
  --mount-path="$MOUNT_PATH" --filesystem-uuid="$FILESYSTEM_UUID" \
  --filesystem-type="$FILESYSTEM_TYPE" --fresh --out="$REQUEST"

REQUEST_SHA256="sha256:$(sha256sum "$REQUEST" | awk '{print $1}')"
./validator storage-prepare plan \
  --request="$REQUEST" --request-sha256="$REQUEST_SHA256" > "$PLAN"

PLAN_SHA256="sha256:$(sha256sum "$PLAN" | awk '{print $1}')"
./validator storage-prepare apply \
  --plan="$PLAN" --plan-sha256="$PLAN_SHA256" > "$RESULT"
```

Inspect the plan before applying it. `--fresh` asserts that this particular new
root has no previous writer or retained state; an empty-looking directory is
not permission to discard an old validator's state. Keep the original request,
plan, staging directory and result. If apply output is lost, repeat the same
apply command with the original plan and hash.

The result contains `declaration.path` and `declaration.sha256`. Set
`DURABLE_VOLUMES` and `DURABLE_VOLUMES_SHA256` to those exact values for activation
and the daemon. The default free-space floors are 100 GiB and 100,000 inodes;
override with `request --min-available-bytes=<n>` and
`--min-available-inodes=<n>` for your capacity plan. These are reserve floors,
not storage quotas. On macOS, use `shasum -a 256` in place of `sha256sum` and the
actual local APFS volume identity.

The declaration is for the daemon. Optional coldkey registration/staking
commands require a separate owner-local declaration and journal; do not use
the daemon declaration for them. An existing eligible hotkey can skip those
operations completely.

## Activate and start

After preparing the daemon's durable storage, set `DURABLE_VOLUMES` and
`DURABLE_VOLUMES_SHA256` from the returned declaration. Set `RELAYER_KEY_FILE` to
your funded EVM gas payer. Preview activation first:

```sh
./validator activate --config="$INITIAL_CONFIG" \
  --relayer_key_file="$RELAYER_KEY_FILE" \
  --rendered-config="$RUN_CONFIG" \
  --successor-approval="$ACTIVATION_DIR/activation-approval.json" \
  --original-authority="$ACTIVATION_DIR/initial-authority.json" \
  --approval-key-file="$APPROVAL_KEY_FILE" \
  --durable-volumes="$DURABLE_VOLUMES" \
  --durable-volumes-sha256="$DURABLE_VOLUMES_SHA256" \
  --dry-run
```

Run the same command with `--apply` in place of `--dry-run` to publish the
activation and render the production configuration. Activation may wait for the
next coordinator epoch; mainnet currently uses a weekly settlement cadence.
Each invocation makes one bounded pass. If it reports `waiting`, rerun the
same `--apply` command after the stated boundary until it reports completion
and writes `RUN_CONFIG`. Preserve all original paths and setup files between
passes. Follow its reported stage rather than deleting state or restarting
setup with a new state directory.

Rendered configuration and approval files stay under the declared runtime root
at `ACTIVATION_DIR`, alongside state and evidence. The original signed setup
configuration and all private keys remain outside that fresh root.

After activation has rendered `RUN_CONFIG`, authenticate the configured
operator. This writes the credential to the exact production path:

```sh
./validator auth --config="$RUN_CONFIG" --no_id=1
```

The current mainnet profile includes operator 1. Its measurement state is
isolated under `STATE_DIR/no-1`; the selected measurement seed stays at
`KEY_DIR/no-1/client.key`. The global `~/.urnetwork/jwt` used by measurement-only
commands is not the production credential store.

Start the validator with the rendered configuration:

```sh
./validator run --config="$RUN_CONFIG" \
  --durable-volumes="$DURABLE_VOLUMES" \
  --durable-volumes-sha256="$DURABLE_VOLUMES_SHA256" \
  --progress-file="$PROGRESS_FILE" \
  --startup-verified-progress
```

The progress path above is outside protocol state and has an existing private
parent. Keep the same command, configuration and persistent volumes across
normal restarts.
The whole steering operation has a 30-minute default budget. Override it with
`--steering-operation-timeout=30m` or a longer appropriate duration; the minimum
is five minutes. This does not change native deadlines or HTTP timeouts.

## Check that the validator is working

Check the process, the progress file and the actual native row together:

- `heartbeat_at` should advance and `publisher.outcome` should report published.
- Compare observation timestamps with wall time; `current: true` alone does not
  prove a sample is recent.
- Settlement should reach its current target without an increasing publication
  backlog.
- Native steering can legitimately report `reveal_wait` after a finalized
  commit. Confirm the intent becomes applied after its stated reveal block.
- Confirm the native weights belong to your hotkey's resolved UID. A running
  process or finalized commit alone does not prove weight application.
- Inspect diagnostic drop counters. An increasing counter means some log
  records were lost, so a quiet error log is insufficient evidence of health.

The configured validator masks its own live UID. Fleet/head destinations and
pool destinations are resolved from current authenticated state; do not copy a
historical fixed UID split into a monitoring rule.

## Optional modes

`validator run --all-operators` and `validator run --measurement-only` collect
measurements without steering production weights. `--operators-refresh=1h` on
a production run reports operator-list changes; it does not silently enroll
new production operators or replace the signed configuration.

The generated approval has explicit native block and epoch ceilings. The
default `--approval-valid-for=720h` is finite and does not automatically renew;
choose the intended approval window at configuration time and monitor those
ceilings. A runtime upgrade or approval expiry requires an approved successor;
do not regenerate a fresh identity or erase state to resume an existing one.

An independent validator does not need `--ledger_auditor_key_file`, a subnet
owner key, a treasury signing key or a native gap bridge. Those are separate
roles or recovery operations.
