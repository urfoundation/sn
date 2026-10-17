# Build version: pinned dependencies

SN's and server's `main` take the SDK, Connect, Connect's SCTP fork and the
gVisor fork from the sibling checkouts (`../sdk`, `../connect`,
`../connect/sctp`, `../gvisor`). A local release build
(`"dependency_mode": "local"`, see [RELEASE-BUILD.md](../../mainnet/RELEASE-BUILD.md))
compiles them from those working checkouts. A build version pins every
dependency instead. The build makes a copy of SN and server, runs `pin.sh` on
the copy, commits the result there and builds that commit with
`"dependency_mode": "pinned"`. Nobody runs `pin.sh` on `main` or pushes its
commits.

## What pin.sh changes in the copy

### SN

| File | Change |
| --- | --- |
| `go.mod` | Four `replace` directives become versioned (below), and their comments are rewritten. The `require` lines stay at `v0.0.0`. Every other replacement stays local: server, warp, proxy, userwireguard, glog and goidenticons are in the pinned census, and `./third_party/...` is inside SN. |
| `go.sum` | Eight lines: the `h1:` and `/go.mod h1:` sums of the four modules. |
| `scripts/mainnet-release-build/sources.go` | `validateReleaseModule`'s table. For sdk, connect and pion/sctp: the replacement path, version, `Sum` and `GoModSum`. |
| `scripts/mainnet-release-build/sources_test.go` | `buildFixturePromotedApiModules`, the same three rows. |
| `scripts/source-graph/source_graph_test.go` | The three pinned versions. |
| `mainnet/RELEASE-BUILD.md` | A table of the pins after the October 1 table, and a line on the gVisor fork. |

```
replace github.com/urnetwork/connect => github.com/urnetwork/connect <connect version>
replace github.com/urnetwork/sdk => github.com/urnetwork/sdk <sdk version>
replace github.com/pion/sctp => github.com/urnetwork/connect/sctp <connect/sctp version>
replace gvisor.dev/gvisor => github.com/urnetwork/gvisor <gvisor version>
```

The requires stay at `v0.0.0` because server, proxy and sdk require
`github.com/urnetwork/connect v0.0.0` and `github.com/urnetwork/sdk v0.0.0`,
and `v0.0.0` outranks any `v0.0.0-<time>-<hash>` pre-release. With
pseudo-version requires and no replacements, the graph selects
`connect@v0.0.0` and fails ("unknown revision v0.0.0"), and `pion/sctp` falls
back to upstream v1.11.1. A versioned replacement applies to every required
version.

gVisor is pinned too because pinned mode admits a local module only from its
census, which does not include gvisor. `pin.sh` pins the published fork,
`github.com/urnetwork/gvisor`, at the gvisor checkout's commit or at its third
argument.

### server

`go.mod` gets the same four versioned replacements and a rewritten comment.
`go.sum` gets the same eight lines. Pinned mode applies the same table to
server's module graph.

## How the values are derived

For each module, outside any module directory,
`go mod download -json <module>@<full commit>` prints these fields:

- `Version`, the pseudo-version
  `v0.0.0-<committer time, UTC, yyyymmddhhmmss>-<first 12 hex digits>`. The base
  is `v0.0.0` because the repositories' `v2026.*` tags belong to `/v2026`
  module paths.
- `Sum`, the `h1:` hash of the module zip: the commit's tree, without its
  nested modules.
- `GoModSum`, the `h1:` hash of the module's `go.mod`.
- `Origin.Hash`, the commit. `pin.sh` checks it.

`github.com/urnetwork/connect/sctp` is the `sctp/` directory of the same
Connect commit. The hashes depend on the bytes alone: the public proxy and a
direct git fetch return the same values, and sum.golang.org records them. Each
module's tree equals its sibling checkout's tree at that commit, apart from
nested modules, so a build version links the same bytes that the gate tests in
the sibling checkouts.

## Making a build version

You need Go 1.27.2 (`pin.sh` runs with the toolchain `go.mod` selects, under
`GOTOOLCHAIN=local`), git, python3 and HTTPS access to proxy.golang.org and
sum.golang.org, or to github.com (see below). The frozen commits must be pushed.
The build workspace holds fresh physical clones of the census that
RELEASE-BUILD.md lists, plus `sdk`, `connect` and `gvisor` checked out at the
frozen commits. `pin.sh` refuses a commit that is not its sibling's HEAD.

```sh
W=/path/to/build-workspace
git -C "$W/sdk" checkout --detach <sdk-commit>
git -C "$W/connect" checkout --detach <connect-commit>
git -C "$W/gvisor" checkout --detach <gvisor-commit>
git -C "$W/sn" checkout --detach <sn-commit>
git -C "$W/server" checkout --detach <server-commit>
"$W/sn/scripts/mainnet-release-build/pin.sh" <sdk-commit> <connect-commit>
git -C "$W/sn" commit -a -F "$W/pin-sn.msg"
"$W/sn/scripts/mainnet-release-build/pin.sh" -C "$W/server" <sdk-commit> <connect-commit>
git -C "$W/server" commit -a -F "$W/pin-server.msg"
(cd "$W/sn" && GOWORK=off go mod download) && (cd "$W/server" && GOWORK=off go mod download)
```

Then build with `"dependency_mode": "pinned"` and the ten pinned-census
repositories, recording SN and server at their pin commits and trees. The
builder runs with `GOPROXY=off`, so the module cache of the user who runs it
must already hold the modules: the last line fills it.

`pin.sh` refuses a dirty checkout, a module that resolves to another commit and
a result that is not tidy. It then runs `scripts/source-graph` and the
builder's tests in the copy. Each commit message lists every module's commit,
version and both sums, and the module environment they were fetched with. The
messages are written beside the checkouts, so the copies stay clean until they
are committed.

The manifest records each pinned copy's commit and tree. Running `pin.sh` with
the same module commits on the same base commits reproduces the same trees;
only the commit metadata differs.

## Module resolution

All four modules are public GitHub repositories, and the default environment
resolves them: `GOPROXY=https://proxy.golang.org,direct` and
`GOSUMDB=sum.golang.org`, with `GOPRIVATE`, `GONOPROXY` and `GONOSUMDB` empty.
No credentials are needed. `pin.sh` sets `GOWORK=off` and an empty `GOFLAGS`
itself, and leaves `GOPROXY`, `GOSUMDB` and `GOPRIVATE` to the caller.

The first proxy request for a commit pushed minutes earlier can return 404
"not found: fetch timed out" while the proxy fetches it. Connect's repository
is large. `pin.sh` tries four times, 30 seconds apart. If the proxy keeps
failing, fetch from GitHub with git and compute the sums locally:

```sh
GOPRIVATE=github.com/urnetwork "$W/sn/scripts/mainnet-release-build/pin.sh" <sdk-commit> <connect-commit>
```

This skips the checksum database for these modules. Cross-check the sums once
the proxy has the commits:
`curl -s https://sum.golang.org/lookup/github.com/urnetwork/sdk@<version>` must
print the same two lines as `go.sum`. Never use `GOSUMDB=off` or `GOINSECURE`.

## Rehearsal, 2026-10-13

| Module | Commit | Version | `Sum` | `GoModSum` |
| --- | --- | --- | --- | --- |
| sdk | 8a039522dd8cbf8f47e94ae33de440c9f36ee61e | v0.0.0-20261013213142-8a039522dd8c | h1:AKT78FedFetlwBD5sXHdoSkGMzxYF33v1CMiAJqobUE= | h1:civ/u2dKObRB2kvT80Ov+hZAXTqZwdlbMwaWcU1RzZg= |
| connect | f7318bca606c266b6d581f35b28c7a8c68f7cf95 | v0.0.0-20261013171400-f7318bca606c | h1:RB/BYg+LKktCDjf4COw1n/X98hxdjWKQwdb5sSBJ8Io= | h1:ijBmhQcFTkLp9/fbXTfDBZg1bjfjoT7T9SqSCbmwsQ0= |
| connect/sctp | f7318bca606c266b6d581f35b28c7a8c68f7cf95 | v0.0.0-20261013171400-f7318bca606c | h1:2UjZHeSD7tgvPn6lP1BkxSqXEwA2BNqkEmL5su5oiw8= | h1:7KFmTwLcoYgJs/Z+99nJvsWL0qDpuyloSI0RbAqlrz0= |
| gvisor | c0783dba2eeff7b79692a7f122ffee5f8bd837c1 | v0.0.0-20261004152605-c0783dba2eef | h1:5o+/eARHbukXt1AkZW8MqCJ/o8hfuhYOllPaZv1Zdts= | h1:We1Jh3kDvpcx13qPcI0LTjx3U6pHpis/KFoU/UtJTkM= |

c0783dba is the tip of urnetwork/gvisor's default branch, `go`, and the commit
that server's `local/source-graph/lock.yml` names. The pinned copies of SN and
server passed the source-graph tests, the builder's tests, tidy, vet and the
builder's pinned admission of both main modules. The proxy's first request for
sdk 8a039522 and connect f7318bca timed out, and a retry about two minutes
later succeeded. A direct git fetch took 2.5 minutes and 1.3 GB of module
cache, and produced the same sums.
