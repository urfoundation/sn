# BringYour Provider Binary

This package implements a provider binary.

Production claim daemon and swarm recovery require exclusive retained
[claim queue ownership](CLAIM-QUEUE-OWNERSHIP.md).

```
Usage:
    provider provide [--port=<port>] --user_auth=<user_auth> [--password=<password>]
        [--api_url=<api_url>]
        [--connect_url=<connect_url>]
```

It is set up to be build with `warpctl build` and push to the community build.

## Build and Run Locally

To build and run locally:

```
go build
./provider
```

## Community Build

BringYour hosts a build on DockerHub ([bringyour/community-provider](https://hub.docker.com/repository/docker/bringyour/community-provider/general)). You can set up Warp to follow this image so that when an image is pushed to block `g4`, your hosts will update to the new image and run the new code. Using the community build with Warp is completely optional, but it can be an easy way to keep your provider up to date with the latest. BringYour tests the builds on blocks `beta`, `g1`, `g2`, and `g3`. Of course you can follow those blocks also if you want to run more cutting edge code.

See [community-provider-warp-example/anyhost](community-provider-warp-example/anyhost) for example Warp systemd units. You will need to build and install `warpctl` on the target host.

## Pre-built binaries

Currently we do not sign the provider binaries on the [releases page](https://github.com/urnetwork/build/releases). You will need to add a signing exemption for various platforms, to allow the binary to be run.

**macOS**

```
xattr -d com.apple.quarantine provider
```


## Mining every listed operator

`provider provide --all-operators` mines every operator in the operator list
published at <https://ur.xyz/operators.yml>. It runs one provider process per
operator and follows the list as it changes. The list is refetched every hour;
`--operators-refresh=<duration>` changes that and `--operators-url=<url>`
follows another list. The last good copy is kept as `operators.yml` in the
provider state directory (`~/.urnetwork`, or `URNETWORK_STATE_DIR`), so mining
continues while ur.xyz is unreachable.

Each operator has its own state directory, `operators/<domain>/` below the
provider state directory, with its own `jwt`, provider key and client JWT. A
network saved with `choose_network` does not apply. `--all-operators` refuses
`--api_url`, `--connect_url`, `--provider-jwt`, `--wallet` and its consent
flags, `--adopt-legacy-provider-key`, `--test-egress-source-ip`, and the
capture and close-report flags, which stay single-operator.

The one-line auto mode signs in to every operator with your Bittensor hotkey,
as a TAO wallet. The first sign-in creates the hotkey's network on that
operator. No chain transaction is involved:

```
provider provide --all-operators --auto-register --hotkey_seed_file=<path>
```

The seed file holds the hotkey's 32-byte sr25519 seed, raw or as 64 hex
characters, in a private file that the miner never creates. In auto mode every
provider may register its new provider client: `--allow-client-registration`
is implied.

Or authenticate operators yourself, with an auth code, a user and password, or
the hotkey:

```
provider operators                                    # the list, and which operators await auth
provider auth --operator=<domain> <auth_code>
provider auth --operator=<domain> --user_auth=<user_auth>
provider auth --operator=<domain> --hotkey_seed_file=<path>
provider provide --all-operators --allow-client-registration
```

An operator authenticated with `provider auth --operator` has no provider
client yet, so its first run needs `--allow-client-registration`. Later runs
don't. An operator without a jwt gets no provider: the miner logs
`operator <domain> is awaiting auth: provider auth --operator=<domain>` and
checks again every five minutes.

- `--max-memory` is divided evenly between the providers. A provider restarts
  when its share changes.
- A proxy file (`provider proxy add`) in the provider state directory is copied
  into each operator's directory when its provider starts.
- A provider that exits restarts after 30 seconds, doubling up to 10 minutes.
- A delisted operator's provider gets SIGTERM, then SIGKILL after 60 seconds.
  Its directory is kept.
- SIGINT and SIGTERM stop every provider.
- `--port` serves the status of every operator. The providers themselves run
  without a status port.
- With `--all-operators`, the first provider process to take TCP 443 serves the
  extender for the host.


## Hotkey payout across operators

One coldkey signature pays the miner on every operator. The coldkey and the
hotkey both sign one global consent that maps the hotkey to the coldkey. It is
kept as a chain under `hotkey-wallet/` in the provider state directory. Each
authenticated operator then stores the chain, and the hotkey alone signs a
delegation of the miner's network there to the chain's head. The coldkey never
signs per operator.

```
provider wallet hotkey challenge <coldkey_ss58> --hotkey_seed_file=<path>    # prints the statement to sign
provider wallet hotkey set <coldkey_ss58> --hotkey_seed_file=<path> --message='<printed statement>' --signature=0x<128 hex chars>
provider wallet hotkey set <coldkey_ss58> --hotkey_seed_file=<path> --coldkey_seed_file=<path>    # or sign here
provider wallet hotkey status
```

`set` appends the generation, then stores the chain and the delegation at every
authenticated operator and reports each one. A failing operator never stops the
others. Setting the same `--message` and `--signature` again resends the chain,
and `provide --all-operators --hotkey_seed_file=<path>` keeps every
authenticated operator delegated at start and every hour. A later generation
changes the coldkey or the epochs. The first generation names the subnet that
the authenticated operators state, and they must agree. Each states its chain
id, genesis hash and netuid in `GET /sn/epoch`, which changes nothing at the
operator. Only an operator that predates the genesis hash there is asked for a
network consent challenge instead, which is read and never signed.

Epochs, unless `--wallet-from-epoch` and `--wallet-through-epoch` choose them:
generation 1 earns from epoch 0, and a later generation from the operators'
current epoch plus 1. Each delegation starts at the operator's current epoch
plus 2, past its prospective boundary. Every interval covers 65,536 epochs.

An operator pays a provider by precedence: the provider's own wallet consent
(`provider wallet set`), then a network consent, then the hotkey delegation.


## Subnet payout wallet (Bittensor coldkey)

`provider wallet set` authenticates the retained provider credential and obtains
the prospective earning-wallet statement from `POST /sn/wallet/consent`.
The coldkey signs its exact provider, deployment, predecessor and earning
interval before `POST /sn/wallet` accepts it. The default credential is
`.provider.jwt` in the provider state directory; `--provider-jwt=<path>` selects
another retained provider slot. A network bootstrap credential cannot substitute
for a provider. The CLI proves the coldkey one of two ways.

Headless, with the coldkey seed on the host:

```
provider wallet set <coldkey_ss58> --coldkey_seed_file=/path/to/coldkey.seed
provider provide --wallet=<coldkey_ss58> --coldkey_seed_file=/path/to/coldkey.seed
```

The seed file holds the 32-byte sr25519 mini secret, raw or as 64 hex chars
(optional `0x`), in a private file (`0600`, owned, no symlinked path
components) that the CLI never creates. The keypair is derived the way
`subkey`, polkadot-js and `btcli` derive it, and the set is refused unless
the seed derives `<coldkey_ss58>`. The CLI verifies the prospective statement,
signs it locally and posts the provider ID, address, original message and
signature; the seed never leaves the host. `provide --wallet` performs this
optional action after its actual direct or proxy slot authenticates; a wallet
failure is reported while traffic provisioning continues.

Signed elsewhere (btcli, a hardware wallet, a browser extension):

```
provider wallet challenge <coldkey_ss58>     # prints the message to sign
provider wallet set <coldkey_ss58> --message='<exact printed statement>' --signature=0x<128 hex chars>
```

Use the command printed by `wallet challenge`: it preserves the selected
credential and API URL. The bytes to sign are the UTF-8 statement exactly as
printed, with the sr25519 key in the
`"substrate"` signing context; a Polkadot extension `signRaw` of type
`"bytes"`, which wraps them in `<Bytes>...</Bytes>`, is accepted too. The
signature is the 64-byte sr25519 signature as hex (`0x` optional). The
challenge has a bounded lifetime of at most five minutes from issuance, so
sign and submit within that window. In `--message` a literal
`\n` stands for a line break.

Fresh provider consent always requires a signature. Its default interval starts
at the next earning epoch and covers 65,536 epochs. The paired
`--wallet-from-epoch=<epoch> --wallet-through-epoch=<epoch>` flags select an exact
shorter interval. The Server's prospective boundary remains authoritative.

Before sending, the command retains the signed original in the private sibling
directory `<provider-jwt-path>.wallet-consent/history.json`. Keep this directory
with the selected credential. A retry replays the exact nonce and signature,
even after the seed is unmounted; a pending original cannot be replaced by a
new challenge. Each retry reconciles the current Server response. The printed
original, hash and generation support independent earning-roster review; the
local journal is not a complete global wallet history or roster approval.

`--legacy-network-wallet` explicitly retains the older network login-proof
projection path. That projection does not establish a release earning-wallet
consent. Existing payment obligations retain their original destinations.

Claims also use the selected provider credential. Missing credentials or original
provider contributions remain not ready. `provider claim --legacy-coldkey=<ss58>`
explicitly selects committed legacy proof bytes with a network credential;
it does not infer historical ownership from the current network wallet. It is
available only for retained network-only epochs without a provider artifact.
The claim daemon accepts the equivalent `legacy_coldkey` configuration field.
Its `jwt_file` selects the provider token otherwise. Token reload and refresh
preserve the original provider identity; the daemon never rewrites that file.
Already signed queue entries can reconcile without a provider token, while new
proof reads and signing remain held until a usable credential is available.
