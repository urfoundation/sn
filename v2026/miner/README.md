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
