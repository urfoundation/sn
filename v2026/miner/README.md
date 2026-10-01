# BringYour Provider Binary

This package implements a provider binary.

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

Pool payouts on the subnet are claimable by the ss58 coldkey the network
registers with `provider wallet set` (`POST /sn/wallet`). The main network
refuses a pasted address: the set must carry the coldkey's sr25519 signature
over a single-use challenge issued by `POST /auth/wallet-challenge` — the same
proof the ur.io app's wallet bridge sends — so a leaked network JWT alone can
never redirect a payout. The CLI proves the coldkey one of two ways.

Headless, with the coldkey seed on the host:

```
provider wallet set <coldkey_ss58> --coldkey_seed_file=/path/to/coldkey.seed
provider provide --wallet=<coldkey_ss58> --coldkey_seed_file=/path/to/coldkey.seed
```

The seed file holds the 32-byte sr25519 mini secret, raw or as 64 hex chars
(optional `0x`), in a private file (`0600`, owned, no symlinked path
components) that the CLI never creates. The keypair is derived the way
`subkey`, polkadot-js and `btcli` derive it, and the set is refused unless
the seed derives `<coldkey_ss58>`. The CLI fetches the challenge (blockchain
`TAO`, purpose `connect`, pinned to the address), signs it locally and posts
address, message and signature; the seed never leaves the host.

Signed elsewhere (btcli, a hardware wallet, a browser extension):

```
provider wallet challenge <coldkey_ss58>     # prints the message to sign
provider wallet set <coldkey_ss58> --message='Sign in to URnetwork\nChallenge: ...\nTimestamp: ...' --signature=0x<128 hex chars>
```

The bytes to sign are the UTF-8 challenge text exactly as printed (three
lines, LF line endings, no trailing newline), with the sr25519 key in the
`"substrate"` signing context; a Polkadot extension `signRaw` of type
`"bytes"`, which wraps them in `<Bytes>...</Bytes>`, is accepted too. The
signature is the 64-byte sr25519 signature as hex (`0x` optional). The
challenge is single-use, bound to the coldkey, and expires 5 minutes after it
is printed, so sign and submit within that window. In `--message` a literal
`\n` stands for a line break.

Without either option the set is sent unsigned, which only a deployment with
the `wallet_allow_unsigned` policy accepts (never the main network); the
refusal names both ways to sign.
