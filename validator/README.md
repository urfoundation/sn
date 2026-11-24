# Run an independent SN25 validator

This guide is for anyone who runs their own validator on Bittensor subnet 25.
You use your own Bittensor hotkey, your own host and your own state. You need
nothing from an existing URnetwork validator, and nothing secret from us.

The validator measures URnetwork providers through the network operator. It
publishes its signed evidence and commits its weights to the chain once per
tempo with commit-reveal. The mainnet defaults are bundled in the binary
(`mainnet-profile.json`), so setup needs only your hotkey.

## What you need

- **A permitted hotkey on subnet 25.** It needs a validator permit and stake
  at or above the subnet's threshold. Registration alone is not enough.
- **A little free TAO on the hotkey.** Each tempo, the hotkey signs one batch:
  a commitment and a timelocked weight commit. The batch pays an ordinary
  transaction fee.
- **The hotkey file.** Use a btcli wallet hotkey (btcli writes these
  unencrypted by default), or a file holding the raw 32-byte or hex sr25519
  seed. The file stays on your host with mode 0600. An encrypted keyfile is
  refused; decrypt it first.
- **A funded EVM key** (secp256k1, hex in a file) on the subtensor EVM. It
  pays the gas to publish your activation, once per activation, and receives
  no authority.
- **A Linux host** with an x86-64 or arm64 CPU and a persistent ext4, xfs or
  btrfs filesystem. See [Ports and resources](#ports-and-resources).
- **Chain endpoints.** You need a subtensor WebSocket endpoint and an EVM
  JSON-RPC endpoint.
  - Both must serve historical state. The EVM one must also serve `eth_getLogs`
    over 1000-block ranges.
  - The bundled defaults are `wss://archive.chain.opentensor.ai` and
    `https://archive.chain.opentensor.ai`. For reliability, run your own
    archive subtensor node with the EVM enabled.
  - Setup checks both endpoints and refuses one that would fail later.

## Install

Release archives contain the binary, this guide, `RELEASING.md` and the
bundled `mainnet-profile.json`. When a release is published, download the
archive for your platform, check it against the release's `SHA256SUMS`, and
put the binary on your path:

```sh
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf validator-VERSION-linux-amd64.tar.gz
sudo install -m 0755 validator /usr/local/bin/validator
validator --version
```

To build from source, you need Go (the version in `go.mod`). Clone these
public repositories side by side; `go.mod` replaces them by relative path:

```sh
mkdir urnetwork && cd urnetwork
git clone https://github.com/urfoundation/sn
for repo in connect sdk server proxy userwireguard glog quic-go goidenticons gvisor; do
  git clone https://github.com/urnetwork/$repo
done
git clone https://github.com/bringyour/warp
make -C sn/validator build GOOS_ARCH=linux/amd64 VALIDATOR_VERSION="$(git -C sn describe --always)"
sudo install -m 0755 sn/validator/build/linux/amd64/validator /usr/local/bin/validator
```

Build a tagged commit of `sn` for production. Build the other repositories at
their own main branches, or at the commits a release lists.

## Set up

Run every step as the account that will run the validator. Each step derives
its paths from one home directory, by default `~/.urnetwork/sn25-validator`.
Pass `--home=<dir>` to every step and to `validator run` to use another, such
as `/var/lib/sn25-validator` for a service account.

**1. Keys and configuration.**

```sh
validator setup --wallet.name=<wallet> --wallet.hotkey=<hotkey>
```

`--wallet.path` defaults to `~/.bittensor/wallets`; `--hotkey-file=<path>`
replaces the wallet flags. This step:
- checks the chain endpoints;
- creates the configuration approval key and the measurement key;
- writes the initial configuration, signed with the approval key;
- prints the hotkey's registration.

Look for `permit=true, eligible=true` and the hotkey's UID. If the hotkey is
not yet permitted, finish registration and staking with your wallet tools,
then run the same command again: it reuses what it made.

**2. Durable storage.** The runtime directory holds the validator's state and
evidence. Storage preparation declares that directory on its filesystem,
which must start empty:

```sh
validator setup storage --validator-uid=<UID> --fresh
validator setup storage --apply
```

The first command finds the filesystem's mount point, UUID and type (on Linux;
elsewhere pass `--mount-path`, `--filesystem-uuid` and `--filesystem-type`).
It then prints the preparation plan. Review the plan, then apply it. The
default reserve floors are 100 GiB and 100,000 inodes; change them with
`--min-available-bytes` and `--min-available-inodes`.

**3. Sign in.**

```sh
validator setup auth
```

The hotkey signs in to the network operator, which creates your validator's
network there on the first sign-in. Signing in accepts the operator's Terms of
Service.

**4. Activation.**

```sh
validator setup activate --relayer-key-file=<evm.key> --dry-run
validator setup activate --relayer-key-file=<evm.key> --apply
```

Activation publishes your validator's signed activation through the evidence
journal and renders the run configuration. It takes effect at the next
coordinator epoch, which on mainnet is weekly. Repeat the `--apply` command
when it reports `waiting`, until it reports completion. Then start the
validator promptly: its first weight decision must fall in the native epoch
that activation names.

**5. Run.**

```sh
validator run
```

`validator run` with no `--config` runs the home.

## Run with systemd

This unit runs the validator as the `sn25-validator` account with its home in
`/var/lib/sn25-validator`. Do the setup steps as that account with
`--home=/var/lib/sn25-validator`.

```ini
[Unit]
Description=SN25 validator
Wants=network-online.target
After=network-online.target
StartLimitIntervalSec=0

[Service]
Type=exec
User=sn25-validator
Group=sn25-validator
ExecStart=/usr/local/bin/validator run --home=/var/lib/sn25-validator
Restart=always
RestartSec=30
TimeoutStopSec=90
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true

[Install]
WantedBy=multi-user.target
```

Keep the hotkey file readable by that account only. Do not use
`ProtectSystem=strict`: storage preparation checks the declared filesystem's
real mounts, which strict mode hides behind bind mounts.

## Defaults and overrides

The order of precedence is:
1. command-line flags;
2. the files in the home;
3. the bundled mainnet profile.

`validator setup` takes:

| Flag | Default |
| --- | --- |
| `--home` | `~/.urnetwork/sn25-validator` |
| `--wallet.path` | `~/.bittensor/wallets` |
| `--validator-id` | derived from the hotkey; a local ID, not the UID |
| `--rpc`, `--substrate` | the profile's public archive endpoints |
| `--approval-valid-for` | `8760h` |

Setup records its choices in the signed configuration. To change one, set up
a new home; see [Rotation](#rotation).

`validator run` takes from the home:

| Flag | Default from the home |
| --- | --- |
| `--config` | `runtime/activation/validator.yml` |
| `--durable-volumes`, `--durable-volumes-sha256` | the declaration in `config/storage/result.json` |
| `--progress-file` | `config/progress.json` |
| `--steering-operation-timeout` | `60m` |
| `--startup-verified-progress` | always on |

`--config=<path>` runs a configuration made by hand instead; it excludes
`--home`. A home that is missing, or not yet activated, is an error that names
the missing step.

The home holds:
- `keys/`: the approval and measurement keys.
- `config/`: the signed initial configuration, the storage request, plan and
  result, and `progress.json`.
- `runtime/`: the declared state, evidence and activation files.

Back up the home and the hotkey. Never copy a home to a second host: two
validators signing with one hotkey race their nonces and stall.

## Ports and resources

- **Inbound:** none. The validator listens on no port.
- **Outbound:** TCP 443 to the network operator (`api.bringyour.com`,
  `connect.bringyour.com`), `ur.xyz` and `api.drand.sh`. UDP 443 may also be
  used. Your chain endpoints need their own ports.
- **CPU and memory:** 4 or more cores, and 8 GiB or more of memory. A restart
  replays the retained history before steering resumes, which can take half an
  hour on a busy validator.
- **Disk:** the declared filesystem keeps at least 100 GiB and 100,000 inodes
  free. Bounded evidence and state use tens of GiB.

## Monitoring and logs

The validator logs to standard error, so under systemd use
`journalctl -u sn25-validator`. `config/progress.json` is a bounded status
file that the validator rewrites as it works. Check these together:

- `heartbeat_at` advances, and `publisher.outcome` reports the last write.
- Each tempo's intent reaches `finalized`, then `applied` after its reveal
  block. A finalized commit waits in `reveal_wait` until then.
- `steering.outcome` is not `hard_error` for long, and settlement reaches its
  target.
- On chain, your hotkey's UID has fresh weights each tempo.
- Diagnostic drop counters do not grow. A quiet error log alone proves
  nothing.

A line beginning `validator steering hold:` means the chain runs a runtime
this build does not admit. Steering signs nothing until you upgrade, and the
line names the runtime. Trails, settlement and the audit keep running, and the
process does not restart itself.

## Upgrades

- **Validator releases.** Replace the binary and restart the service. Keep the
  home: a new home is a new validator, with a new activation and a week's wait.
  Expect the startup replay after each restart.
- **Subtensor runtime upgrades.** A new runtime is admitted automatically when
  every interface the validator consumes is unchanged. A runtime whose
  interfaces still need review holds steering, as above, until a release
  admits it. Upgrade promptly when one is announced: a validator that stops
  setting weights loses its dividends. `RELEASING.md` describes how releases
  are made.
- **Protocol changes.** These arrive as releases too. A settlement amendment
  that takes effect before its signed entry may need
  `--amendments-fallback=<file>` on `validator run`. Use it only with the
  exact lines the subnet owner publishes, and remove it once the signed entry
  is published (VALIDATOR.md §11.6).

### Rotation

The signed configuration's approval expires after `--approval-valid-for`
(8760h by default), and steering stops at the window's end. To move to a new
configuration, other endpoints or a new approval, set up a second home with
the same hotkey:
1. Set up the new home, then prepare its storage, sign in and activate it,
   while the old home keeps running.
2. When the new activation completes, stop the old service and start the new
   one.

Only one home may run at a time. A command that renews a configuration in
place is planned.

## What to ask us for

Nothing secret. Contact us (support@ur.io, or your URnetwork contact) before
activation with:
- your hotkey's ss58 address;
- your host's egress IP addresses.

The network operator provisions each validator's capacity for uploading its
evidence. Its api limits requests per address. Once your activation is
admitted, the validator also sends a short-lived validator class token with
its client sign-in on each api request. The operator charges those requests
to a validator rate class sized for a restart's replay. A
`validator diagnostic:` line with `code=validator_class_ready` reports that
the class was gained, and `code=validator_class_unavailable` that it was not;
`cause=hard_error` there means the operator refused it. Without the class,
requests are limited as they were before the class existed. Measurement
traffic through providers is carried like any network's.

We announce releases, runtime reviews and protocol changes to registered
validators.

## SN25 rules for validators

- Run an unmodified release, and only one signer per hotkey. Do not run
  another weight setter beside the validator.
- Weights are set once per tempo, by commit-reveal with timelocked commits,
  under the subnet's weights version key. The validator does this itself.
  Never copy another validator's weights.
- Keep the validator permit, stake at or above the threshold, and enough free
  TAO for each tempo's fee.
- Keep the configuration's approval window current, and upgrade promptly when
  a release is announced.

## Other modes and manual setup

`validator run --measurement-only` and `validator run --all-operators` measure
without setting weights. They are useful to check connectivity while
activation waits. VALIDATOR.md §12 describes them.

The setup steps wrap commands you can also run by hand. See `--help` for
each:
- `validator init`
- `validator configure`
- `validator status --config`
- `validator storage-prepare`
- `validator auth --config`
- `validator activate`
- `validator run --config`

`validator register` and `validator stake add` sign with a coldkey seed file
and need their own journal declaration. Most operators use their wallet tools
for these instead.
