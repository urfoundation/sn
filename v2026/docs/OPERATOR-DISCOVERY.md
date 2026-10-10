# Operator discovery, all-operators mining and the root validator (design, 2026-10-06)

This is the design record and the shared contract for four connected changes:
- the machine-readable operator list at `ur.xyz/operators.yml`;
- validators and miners that refresh it and cover every listed operator;
- optional hotkey sign-in (auto-register);
- a payout consent the coldkey signs once for all operators;
- the root validator deployment on snow at an 18% take.

Every interface that crosses a package, repository or work stream is fixed here. Code that disagrees with this document is
wrong, or this document is updated first.

Owner, in order:
- "We will run the root validator and charge 18% fee. Anyone from a site like tao.com can child hotkey to our validator.
  We will deploy the root validator on snow with a new run-validator.sh that set up the root validator as a service to run
  in the background on mainnet using the list of network operators from ur.xyz. run-validator.sh should build a new local
  validator binary similar to how run-edges.sh builds a new warpctl binary."
- "The validator binary should have an option to periodically refresh the network operator list from ur.xyz. Add a
  machine readable ur.xyz/operators.yml file with a list of operator objects, where each operator has a domain, api url,
  and a connect url."
- List power: signed config gates. Validator credentials: auto-register.
- "Can miners also have an auto register option and an option to periodically refresh the operator list from ur.xyz and
  mine all operators? I'm thinking the miner should just have an `--all-operators` flag that replaces any
  operator-specific configuration."
- Payout: sign once, all operators. Sybil gate: any valid hotkey.
- "Registration should be optional. The old path of letting the miner just provider the bittensor hotkey to the operator
  should still work." "The old path should use the operators auth to register a new mining key, which does not require
  the miner to do any transaction on the bittensor chain."
- "Update the web/ur.xyz docs 'how to run a miner' and 'how to run a validator' with the auto options once the miner
  and validator auto ur.xyz/operators.yml changes land."

## 1. Decisions

1. **The list says where, never who gets paid or weighted.**
   - `operators.yml` names each operator's domain, API URL and Connect URL.
   - A validator weights only the operators pinned in its signed release config. A listed operator that isn't pinned is
     reported and may be measured for observation, never weighted.
   - Each operator still authenticates its own accounts and mining keys, and settles its own payouts.
2. **Canonical copy and publication.**
   - The canonical copy is `sn/operators.yml`.
   - ur.xyz publishes it at `https://ur.xyz/operators.yml`, synced into the site the way `price.yml` is.
   - Clients keep the last good copy on disk and use it while ur.xyz is unreachable.
3. **Credentials per operator, two paths.**
   - **Old path, the default:** the operator's own auth (auth code, or user and password) gives a network JWT. The
     mining key then registers through `POST /network/auth-client`, as today. No chain transaction.
   - **Auto-register, optional (`--auto-register --hotkey_seed_file=<path>`):** the hotkey signs in through the wallet
     sign-in every operator already serves, as a TAO wallet:
     1. `/auth/wallet-challenge`;
     2. `/auth/login`;
     3. on the first sign-in only, `/auth/network-create`.

     This needs no new operator API. Any valid hotkey may sign in, within the operator's existing wallet sign-in and
     account-creation rate limits.

   Earlier plans had a dedicated server registration endpoint and a network-hotkey binding table. Both are dropped: the
   existing wallet auth covers find-or-create, and payouts carry their own hotkey-signed evidence (section 6).
4. **`--all-operators` replaces operator-specific configuration.**
   - It is mutually exclusive with `--api_url`, `--connect_url` and `--provider-jwt`.
   - It ignores `network.json` (`choose_network`).
   - Every listed operator gets its own state directory under `<state_dir>/operators/<domain>/`.
5. **Payout across operators: one coldkey signature.**
   - A global hotkey wallet consent is signed by both the coldkey and the hotkey.
   - Each operator receives a per-network hotkey delegation, signed by the hotkey alone, which the miner signs
     automatically. The cold wallet signs once; the hot key does the per-operator work.
   - Precedence is per-provider consent, then network consent, then hotkey delegation.
6. **Root validator.**
   - **As launched (October 8).** The sole SN25 validator is the SN25 owner hotkey, UID 1
     (`5CyQFykVpfa9xgZVGgBsiNUpjbo1EihBTSkPvDC1FZbL1wxG`), whose coldkey is `ur-owner`. `ur-mainnet` keeps its root
     seat, UID 21, through the passive root service. The rest of this item is the October 6–7 design; its SN25
     validator role for `ur-mainnet`, and the `set_children` that served that role, are superseded
     ([launch record](../mainnet/LAUNCH.md#as-launched--october-8)).
   - A dedicated validator hotkey, `ur-mainnet`, runs on root (netuid 0) and validates SN25. The SN25 owner hotkey
     doesn't run it, and the owner Ledger signs none of its operations (owner decision). Others may child-hotkey to it
     on SN25, and we keep 18% of what their stake earns through it. Added October 7: `ur-owner` signs one
     `set_children` on SN25 naming `ur-mainnet` as the SN25 owner hotkey's child at 100%, so the owner hotkey's stake
     weight counts for the validator. No ownership or alpha moves.
   - Its coldkey is the `ur-mainnet` 2-of-3 native multisig `5C9z2rXL1WFLVF78EVg7LZJ8zSi4FheXmbj8omrVhRZCxnQ3`, with
     signatories `brien-ur-mainnet` (Brien's Ledger, `m/44'/354'/10'/0'/0'`), `jack-ur` and `keith-ur`. Every coldkey
     step is a btcli multisig call, and no coldkey material reaches snow.
   - Auto parent delegation is disabled before root registration. Otherwise `root_register` would make the hotkey the
     full-weight parent of every subnet owner hotkey, moving its SN25 weight to the SN25 owner hotkey, which doesn't run
     the validator.
   - The delegate take and the SN25 childkey take are both 18% (11,796/65,535, the chain maximum for childkey take).
   - It runs on snow as a systemd service installed by `xops/main/ansible/run-validator.sh`, which builds the validator
     binary locally the way `run-edges.sh` builds `warpctl`.

## 2. The list and its client

`sn/operators.yml` (schema `urnetwork-operators-v1`):

```yaml
schema: urnetwork-operators-v1
operators:
  - domain: bringyour.com
    api_url: https://api.bringyour.com
    connect_url: wss://connect.bringyour.com
```

Package `github.com/urfoundation/sn/operatorlist` is implemented and tested:
- **Parse:** `Parse(raw) (List, error)` is strict:
  - at most 64 KiB, one YAML document, known fields only, the exact schema;
  - 1–256 operators;
  - lowercase DNS domains;
  - https/wss URLs, with http/ws allowed only for literal loopback hosts;
  - no credentials, query or fragment, and a canonical path;
  - domains and both endpoints unique.
- **Lookup:** `List.Operator(domain)`.
- **Refresh:** `NewRefresher(ctx, RefreshSettings{Url, Interval, CachePath, FetchTimeout, Client})` behaves as follows.
  - Interval is at least 1 minute, with ±10% jitter.
  - It loads the cache first, fetches immediately, and publishes a new immutable `*Snapshot{List, Sha256, FetchedAt, Cached}`
    only when the fetched bytes change.
  - It writes the cache atomically, with mode 0600 and fsync, before publishing.
  - On failure it keeps the last good list and backs off from 30s, capped at the interval.
  - Methods: `Snapshot() (*Snapshot, chan struct{})` (the channel closes at the next change), `Wait(ctx)`, `LastError()`,
    `Close()`.
- **One-shot:** `Load(ctx, settings)` fetches once and falls back to the cache.
- **Defaults:** `DefaultUrl` is `https://ur.xyz/operators.yml`.
- **State:** `DomainStateDir(base, domain) = <base>/operators/<domain>`.

Clients cache the list at `<state_dir>/operators.yml`. The default refresh interval for both `--operators-refresh` flags
is `1h`, written as a Go duration.

## 3. Hotkey sign-in

Package `github.com/urfoundation/sn/hotkeyauth` is implemented and tested:
- `SignIn(ctx, Settings{ApiUrl, Hotkey *crv4.Keypair, NetworkName, Client}) (*Network{ByJwt, Created}, error)`.
- It logs in with the hotkey as a TAO wallet, or creates the hotkey's network on the first sign-in.
- The network name is `NetworkName(hotkey)` = `tao-` plus the first 24 hex characters of sha256(public key). If the
  operator refuses a name as taken, it tries `-2`, `-3`, then `-4`.
- The hotkey signs only a challenge that passes `ValidateChallenge`: exactly the three-line `Sign in to URnetwork` format.
  It signs that challenge wrapped in `<Bytes></Bytes>`. A hostile operator therefore cannot obtain a hotkey signature over
  a transaction or another statement.
- The hotkey seed file is loaded with `crv4.LoadSeedFile` (64 hex characters or 32 raw bytes, private file). A client
  never creates a hotkey for `--auto-register`: a missing seed file is an error that names the path.

The returned network JWT is written with `clientauth.WriteToken` to the operator's `jwt` path (sections 4 and 5).

## 4. Validator contract

### 4.1 Flag mode (measurement, no weights)

```
validator run --all-operators [--operators-url=<url>] [--operators-refresh=<duration>]
    [--auto-register --hotkey_seed_file=<path>]
    [--concurrency=<n>] [--m=<depth>] [--rpc=<rpc_url>]... [--contract=<addr>] [--state_dir=<path>]
    [--adopt-legacy-measurement-key] [-v...]
```

- `--all-operators` excludes `--api_url` and `--connect_url`.
- One measurement runner per listed operator, on the operator's `api_url` and `connect_url`:
  - state directory `DomainStateDir(state_dir, domain)`;
  - network JWT at `<that directory>/jwt`;
  - the existing measurement key and client JWT files inside that directory.
- When the list changes:
  - a new operator starts its runner;
  - a delisted operator's runner is cancelled and joined, and its state directory is kept;
  - a changed URL restarts that runner.
- **No JWT:** with `--auto-register`, `hotkeyauth.SignIn` runs with the hotkey and writes the JWT. Without it, the
  validator logs `operator <domain> is awaiting auth: validator auth --operator=<domain>` and checks again at least every
  5 minutes. One unauthenticated or failing operator never stops the others.
- A runner that fails restarts with exponential backoff (30s doubling to a 10m cap). It does not exit the process.
- **Measurement identity.** `--auto-register` is the separately approved provisioning workflow that
  `validator/MEASUREMENT-CLIENT-AUTH.md` anticipates.
  - For a pristine operator directory, one with no `.validator*` entry at all,
    `clientauth.ProvisionValidatorMeasurementClientKey` creates the measurement key, its identity marker and a
    provisioning marker, then the one direct measurement registration. Creation resumes after a crash until a
    registration record exists; replay and refresh rules apply from then on.
  - It never recovers, adopts or replaces an existing identity. Every other remnant combination keeps today's refusals.
  - Without `--auto-register`, a pristine directory waits, with backoff, for provisioning.
  - The plain run path still can't create a client.

### 4.2 Production mode (weights only from the signed config)

```
validator run --config=<path> [--progress-file=<path>] [--durable-volumes=<path> --durable-volumes-sha256=<hash>]
    [--operators-refresh=<duration> [--operators-url=<url>] [--observe-unpinned-operators]] [-v...]
```

- **No effect on what is signed or weighted.** `--operators-refresh` never changes the signed config's operators,
  weights, evidence or any protocol state.
- **Drift report.** At startup and at each list change, the validator logs a report and writes it to its own bounded
  file, `operators-report.json` beside the list cache (schema `urnetwork-validator-operator-list-report-v1`, atomic,
  0600, at most 16 pinned and 16 unpinned entries plus omitted counts). The progress wire type
  (`protocol.ValidatorProgress`) is unchanged, because the mainnet monitor and older validators decode it strictly. The
  report covers:
  - each pinned operator, matched to the list by normalized `api_url`, with listed or delisted status and any
    `connect_url` mismatch;
  - each listed operator that the config does not pin.
- **`--observe-unpinned-operators`.** It runs the flag-mode runner from 4.1 for each listed, unpinned operator:
  - in observation only;
  - under `<config.state_dir>/observed-operators/<domain>/`;
  - authenticated through `hotkeyauth.SignIn` with the config's `hotkey_seed_file`.

  Its results never reach weights, evidence or production state.
- **Pinned operators** keep their configured `network_jwt_file`. A missing pinned JWT is reported, as today; it is never
  created implicitly in production mode.

### 4.3 Auth

```
validator auth ([<auth_code>] | --user_auth=<user_auth> [--password=<password>] | --hotkey_seed_file=<path>) [-f]
    [--api_url=<api_url> | --operator=<domain> [--operators-url=<url>] [--state_dir=<path>]] [-v...]
```

- `--operator=<domain>` resolves the operator from the list via `operatorlist.Load`, cached at `<state_dir>/operators.yml`.
  It authenticates against that operator's `api_url` and writes `DomainStateDir(state_dir, domain)/jwt`.
- `--hotkey_seed_file` authenticates through `hotkeyauth.SignIn` instead of an auth code or password.
- Without `--operator`, today's behavior is unchanged.

## 5. Miner contract

### 5.1 Commands

```
provider provide --all-operators [--operators-url=<url>] [--operators-refresh=<duration>]
    [--auto-register --hotkey_seed_file=<path> | --hotkey_seed_file=<path>]
    [--port=<port>] [--max-memory=<mem>] [--allow-client-registration] [-v...]
provider auth --operator=<domain> ([<auth_code>] | --user_auth=<user_auth> [--password=<password>] | --hotkey_seed_file=<path>) [-f]
    [--operators-url=<url>] [-v...]
provider operators [--operators-url=<url>] [-v...]
```

- **Exclusions.** `--all-operators` excludes:
  - `--api_url`, `--connect_url`, `--provider-jwt`;
  - `--wallet` and its consent flags (all-operators payout is section 6);
  - `--test-egress-source-ip`;
  - the capture and close-report flags, which stay single-operator.
- **Supervision.** A supervisor in the `provide` process runs one child `provider provide` process per listed operator.
  Each child gets:
  - `URNETWORK_STATE_DIR=DomainStateDir(base, domain)`, where base is today's `providerStateDir()`;
  - `--api_url` and `--connect_url` from the list;
  - `--port=0`;
  - an equal share of `--max-memory`;
  - the same `-v` level and `--allow-client-registration`.
- **Old path inside a child.** Each child keeps today's single-operator behavior unchanged: the mining key from that
  directory's `jwt` via `/network/auth-client`, client JWT refresh, and provider custody.
- **Restarts and list changes.**
  - A child that exits restarts with backoff (30s doubling to a 10m cap).
  - A delisted operator's child gets SIGTERM, then is waited for with a 60s grace period before SIGKILL. Its state
    directory is kept.
  - A changed URL restarts that child.
  - The supervisor forwards SIGINT/SIGTERM to every child and waits for them.
- **Auto mode.** With `--auto-register`, every child also gets `--allow-client-registration`, so a new operator directory
  can create its first provider client. The flag never replaces a retained identity. Without `--auto-register`, a child
  gets it only when the user passed it. The one-line auto mode is
  `provider provide --all-operators --auto-register --hotkey_seed_file=<path>`.
- **Missing JWT** for an operator:
  - with `--auto-register`, `hotkeyauth.SignIn` runs and writes it;
  - otherwise the supervisor logs `operator <domain> is awaiting auth: provider auth --operator=<domain>`, checks again at
    least every 5 minutes, and does not start that child.
- **Network JWT renewal.** Each child renews its operator directory's `jwt` at its half-life
  (`POST /auth/network-refresh`, miner/PROVIDER-REGISTRATION.md "Network sign-in renewal"); the supervisor never renews.
  The hourly hotkey wallet upkeep reads the renewed file. An operator that answers the upkeep with 401 logs
  `operator <domain>: the network sign-in was rejected or has expired; run `provider auth --operator=<domain>` to sign in
  again`.
- **A rejected network JWT with `--auto-register`.** Each child then gets the internal `--quarantine-rejected-sign-in`.
  When the child's renewal call itself is rejected (401), the child sets the JWT aside under its owner lock (only while
  the file still holds that token, so a sign-in that landed first is never set aside) as `jwt.rejected-<unixnano>-<n>`,
  `n` counting rejections less than 25 h apart, keeps the newest 3 such files, reports `network_sign_in_quarantined`
  naming the file, and exits with status 75. Nothing else sets the JWT aside, and without `--auto-register` renewal
  stops as before.
- **Prompt sign-in after a child exits without its JWT.** The supervisor skips the crash backoff and checks the
  credentials at once; with `--auto-register` it signs in with the hotkey (through the owner lock, as an explicit sign-in)
  and starts the child. The automatic sign-in after a set-aside JWT is at once for the first rejection in a while, then
  1 h after the latest rejection, doubling with each one in a row up to 24 h. The limit comes from the set-aside files, so
  it survives a supervisor restart; while it holds the supervisor logs, once per change, the next allowed time and
  `provider auth --operator=<domain>` as the manual path, and still notices a manual sign-in every 5 minutes.
- **Revoked clients stay revoked.** Provider custody blocks a rejected client on any rejection marker, whatever network
  JWT follows, so an automatic sign-in never recreates a revoked provider client.
- **The proxy file** in the base directory, if present, is copied into each operator directory before its child starts.
- **`provider auth --operator`** writes `DomainStateDir(base, domain)/jwt`. With `--hotkey_seed_file` it uses
  `hotkeyauth.SignIn`.
- **`provider operators`** prints each listed operator with its credential state (`jwt present`, `awaiting auth`) and the
  list's source (fetched or cached) and digest.

### 5.2 All-operators payout (section 6)

```
provider wallet hotkey challenge <coldkey_ss58> --hotkey_seed_file=<path>
    [--wallet-from-epoch=<epoch> --wallet-through-epoch=<epoch>] [--operators-url=<url>] [-v...]
provider wallet hotkey set <coldkey_ss58> --hotkey_seed_file=<path>
    [--coldkey_seed_file=<path> | --message=<text> --signature=<hex>]
    [--wallet-from-epoch=<epoch> --wallet-through-epoch=<epoch>] [--operators-url=<url>] [-v...]
provider wallet hotkey status [--operators-url=<url>] [-v...]
```

- `challenge` builds the next global consent statement and prints the exact message for an offline coldkey to sign. It
  keeps the statement as pending in `<base>/hotkey-wallet/pending.json`.
- `set` takes the coldkey signature in one of two ways: it signs with `--coldkey_seed_file`, or it verifies
  `--message`/`--signature` against the pending statement. Then it:
  1. adds the hotkey signature;
  2. verifies the complete chain;
  3. appends the original to `<base>/hotkey-wallet/originals.json`;
  4. submits the chain to every operator that has credentials;
  5. ensures every such operator's delegation adopts the new head.
- `provide --all-operators` with `--hotkey_seed_file`, when `originals.json` exists, ensures at each start and at least
  every hour that each authenticated operator has the chain and a delegation adopting its head.
- Library: package `github.com/urfoundation/sn/hotkeywallet` (section 6.6). The CLI only wires it.

## 6. All-operators payout: global hotkey consent and per-operator delegation

### 6.1 Global hotkey wallet consent (protocol, `sn/protocol/hotkey_wallet_mapping.go`)

```go
const HotkeyWalletMappingConsentSchema = "urnetwork-hotkey-wallet-mapping-consent-v1"
const HotkeyWalletMappingConsentPrefix = "Approve URnetwork hotkey wallet mapping\n"
const HotkeyWalletMappingScope = "hotkey"

// The subnet the consent applies to, on every operator of it.
type HotkeyWalletMappingSubnet struct {
	ChainID     uint64   `json:"chain_id"`
	GenesisHash [32]byte `json:"genesis_hash"`
	Netuid      uint16   `json:"netuid"`
}
func (self HotkeyWalletMappingSubnet) Validate() error           // every field nonzero
func (self ClientKeyHistoryDomain) HotkeySubnet() HotkeyWalletMappingSubnet

type HotkeyWalletMappingStatement struct {
	Schema       string                    `json:"schema"`
	Scope        string                    `json:"scope"`
	Subnet       HotkeyWalletMappingSubnet `json:"subnet"`
	Hotkey       [32]byte                  `json:"hotkey"`
	Coldkey      [32]byte                  `json:"coldkey"`
	Generation   uint64                    `json:"generation"`
	PreviousHash [32]byte                  `json:"previous_hash"`
	Nonce        [32]byte                  `json:"nonce"`
	IssuedAt     int64                     `json:"issued_at"`
	FromEpoch    uint64                    `json:"from_epoch"`
	ThroughEpoch uint64                    `json:"through_epoch"`
}
func (self HotkeyWalletMappingStatement) Message() (string, error)
func DecodeHotkeyWalletMappingStatement(message string) (*HotkeyWalletMappingStatement, error)

// Both sr25519 signatures in the substrate context over the exact message, raw or <Bytes>-wrapped.
type HotkeyWalletMappingConsent struct {
	Message          string   `json:"message"`
	ColdkeySignature [64]byte `json:"coldkey_signature"`
	HotkeySignature  [64]byte `json:"hotkey_signature"`
}
func VerifyHotkeyWalletMappingConsent(ctx context.Context, original HotkeyWalletMappingConsent) (*HotkeyWalletMappingStatement, [32]byte, error)
// Complete lineage from generation 1; returns the head statement and hash. Used when accepting and storing a chain.
func VerifyHotkeyWalletMappingLineage(ctx context.Context, originals []HotkeyWalletMappingConsent) (*HotkeyWalletMappingStatement, [32]byte, error)

type HotkeyWalletMappingHistoryExpectation struct {
	Subnet     HotkeyWalletMappingSubnet
	Hotkey     [32]byte
	HeadHash   [32]byte
	Generation uint64
	Epoch      uint64
}
type VerifiedHotkeyWalletMapping struct {
	Statement    HotkeyWalletMappingStatement
	OriginalHash [32]byte
	HeadHash     [32]byte
	Generation   uint64
}
func VerifyHotkeyWalletMappingHistory(ctx context.Context, originals []HotkeyWalletMappingConsent, expected HotkeyWalletMappingHistoryExpectation) (*VerifiedHotkeyWalletMapping, error)
```

Rules:
- **Field validity:**
  - exact schema and scope;
  - valid subnet;
  - nonzero hotkey, coldkey and nonce;
  - generation 1 to `MaxWalletMappingHistory`, with the previous hash zero exactly at generation 1;
  - `IssuedAt > 0`;
  - `FromEpoch <= ThroughEpoch` and `ThroughEpoch - FromEpoch <= 65535`.
- **Encoding:** canonical JSON, exact re-encoding, `ValidateUniqueJsonKeys` and `DisallowUnknownFields`, as for the other
  kinds. The original hash is sha256 of the JSON-encoded original.
- **Lineage:**
  - every generation has the same subnet and hotkey;
  - each previous hash is the hash of the previous original;
  - `FromEpoch` strictly increases;
  - the coldkey may change between generations, since replacement is how a coldkey changes.
- **Selection at an epoch:** the last generation with `FromEpoch <= Epoch`. When none applies, or its `ThroughEpoch` is
  earlier than the epoch, the result is `ErrWalletMappingNotEffective` joined with `ErrWalletMappingUnavailable`. Missing
  originals give `ErrWalletMappingUnavailable` alone.
- **No expiry and no operator signature.** The global consent is not prospective by itself; per-operator prospectiveness
  comes from the delegation (6.2), which pins a specific consent head.

### 6.2 Per-operator hotkey delegation (protocol, `sn/protocol/hotkey_network_delegation.go`)

This is the network consent of `NETWORK-WALLET-CONSENT.md` with three differences:
- it is signed by a hotkey, not a coldkey;
- it names a hotkey and a global consent head in place of a coldkey;
- it has its own prefix, schema and scope.

```go
const HotkeyNetworkDelegationSchema = "urnetwork-hotkey-network-delegation-v1"
const HotkeyNetworkDelegationPrefix = "Delegate URnetwork network wallet to hotkey\n"
const HotkeyNetworkDelegationScope = "hotkey-network"

type HotkeyNetworkDelegationStatement struct {
	Schema            string                           `json:"schema"`
	Scope             string                           `json:"scope"`
	Domain            ClientKeyHistoryDomain           `json:"domain"`
	UserId            [16]byte                         `json:"user_id"`
	NetworkId         [16]byte                         `json:"network_id"`
	Hotkey            [32]byte                         `json:"hotkey"`
	ConsentHeadHash   [32]byte                         `json:"consent_head_hash"`
	ConsentGeneration uint64                           `json:"consent_generation"`
	Generation        uint64                           `json:"generation"`
	PreviousHash      [32]byte                         `json:"previous_hash"`
	Nonce             [32]byte                         `json:"nonce"`
	IssuedAt          int64                            `json:"issued_at"`
	ExpiresAt         int64                            `json:"expires_at"`
	FromEpoch         uint64                           `json:"from_epoch"`
	ThroughEpoch      uint64                           `json:"through_epoch"`
	Prospective       WalletMappingProspectiveApproval `json:"prospective"`
}
func (self HotkeyNetworkDelegationStatement) Message() (string, error)
func SignProspectiveHotkeyNetworkDelegation(statement *HotkeyNetworkDelegationStatement, boundary ClientKeyEffectiveBoundary, key *ecdsa.PrivateKey) error
func (self HotkeyNetworkDelegationStatement) VerifyProspectiveSignature() error
func DecodeHotkeyNetworkDelegationStatement(message string) (*HotkeyNetworkDelegationStatement, error)
// The original is a WalletMappingConsent whose Signature is the hotkey's (raw or <Bytes>-wrapped message).
func VerifyHotkeyNetworkDelegation(ctx context.Context, original WalletMappingConsent) (*HotkeyNetworkDelegationStatement, [32]byte, error)

type HotkeyNetworkDelegationHistoryExpectation struct {
	Domain     ClientKeyHistoryDomain
	NetworkId  [16]byte
	HeadHash   [32]byte
	Generation uint64
	Epoch      uint64
}
type VerifiedHotkeyNetworkDelegation struct {
	Statement    HotkeyNetworkDelegationStatement
	OriginalHash [32]byte
	HeadHash     [32]byte
	Generation   uint64
}
func VerifyHotkeyNetworkDelegationHistory(ctx context.Context, originals []WalletMappingConsent, expected HotkeyNetworkDelegationHistoryExpectation) (*VerifiedHotkeyNetworkDelegation, error)
func VerifyProspectiveHotkeyNetworkDelegation(ctx context.Context, delegation *VerifiedHotkeyNetworkDelegation, signer common.Address, startBlock uint64, startUnix int64) error
```

Rules:
- **Inherited from the network consent:**
  - validity, with a nonzero hotkey and a nonzero consent head hash, and a consent generation of at least 1;
  - the 300s acceptance window;
  - interval bounds;
  - the prospective digest and signature;
  - lineage, with `FromEpoch` strictly increasing per (domain, network);
  - selection;
  - the earning gate, with the same signer, boundary-block and `ExpiresAt <= startUnix` rules and the same sentinels.
- The hotkey may change between generations: the network owner may move the network to another hotkey, and the new
  hotkey signs.
- Each decoder requires its own prefix and refuses the other kinds' fields. Refusal tests must cover all four kinds in
  both directions: a provider, network, global hotkey or delegation message never decodes or verifies as any other kind.

### 6.3 Resolution and precedence (protocol, `sn/protocol/earning_wallet.go`)

```go
const EarningWalletModeHotkey = "hotkey"
// EarningWallet gains: Hotkey [32]byte; ConsentOriginalHash [32]byte; ConsentGeneration uint64;
// ConsentHeadHash [32]byte; ConsentHeadGeneration uint64. For the hotkey mode OriginalHash, Generation,
// HeadHash and HeadGeneration describe the delegation chain (the chain the roster pins).
func HotkeyEarningWallet(clientId [16]byte, delegation *VerifiedHotkeyNetworkDelegation, consent *VerifiedHotkeyWalletMapping) *EarningWallet

type HotkeyEarningWalletEvidence struct {
	Delegations        []WalletMappingConsent
	DelegationExpected HotkeyNetworkDelegationHistoryExpectation
	// the global chain from generation 1 through the delegation's consent head
	Consents []HotkeyWalletMappingConsent
}
// delegation history -> prospective gate -> global chain through the delegation's ConsentHeadHash and
// ConsentGeneration, with subnet DelegationExpected.Domain.HotkeySubnet(), the delegation's hotkey and the same epoch.
func ResolveHotkeyEarningWallet(ctx context.Context, clientId [16]byte, evidence HotkeyEarningWalletEvidence, signer common.Address, startBlock uint64, startUnix int64) (*EarningWallet, error)

// provider > network > hotkey. Falls back from a mode only when that mode's error is ErrWalletMappingAbsent or
// ErrWalletMappingNotEffective; any other error is returned unchanged. hotkey is evaluated only after network
// falls back; nil hotkey behaves exactly like SelectEarningWallet.
func SelectEarningWalletWithHotkey(provider *EarningWallet, providerErr error, network func() (*EarningWallet, error), hotkey func() (*EarningWallet, error)) (*EarningWallet, error)
```

`SelectEarningWallet` keeps its signature and behavior. A network without a pinned delegation resolves hotkey mode to
`WalletMappingAbsentError()`. Settlement and the economic verifier both call `ResolveHotkeyEarningWallet` and
`SelectEarningWalletWithHotkey`, so the two cannot diverge.

### 6.4 Roster v3 (`sn/payoutartifact`)

- **Field.** New optional top-level list
  `hotkey_delegations: [{network_id, delegation_head_hash, delegation_generation}]`, mirroring `network_wallets`:
  - type `WholeWorkHotkeyDelegation{NetworkId [16]byte, DelegationHeadHash string, DelegationGeneration uint64}`;
  - `DelegationHeadHash` is 64 lowercase hex characters with no prefix, like `wallet_head_hash`;
  - accessor `WholeWorkAuthority.HotkeyDelegation(networkId)`;
  - strictly sorted by `network_id`;
  - each network referenced by an expected provider;
  - a v3 roster's `network_wallets` are optional.
- **History bodies.** Exactly the shapes the readers send:
  - delegation history: `{domain, network_id, head_hash, generation}`, with `network_id` as a server id string;
  - consent history: `{hotkey_ss58, head_hash, generation}`.

  Each original is bounded at `MaxWalletMappingConsentBytes`.
- **Schema.** A roster with any hotkey delegation has schema `urnetwork-whole-work-authority-v3`. Without one, v2 and v1
  keep their exact canonical bytes. Old verifiers refuse v3 (fail closed).
- **Per-provider wallet head.** An expected provider's `wallet_head_hash` may be empty when its network has a network
  wallet or a hotkey delegation in the roster.
- **Readers.** `validator.HttpWalletMappingReader` gains bounded readers for the delegation history and the global chain:
  - `ReadHotkeyDelegationBounded`;
  - `ReadHotkeyConsentBounded`.
- **Economic verifier.** The verifier (`sn/mainnet`) retains both as evidence (`hotkey_delegations` and `hotkey_consents`,
  omitted when empty so old evidence hashes are unchanged) and resolves through 6.3.
- **Producer.** `payoutroster` emits v3 when delegation heads are present, and v2 or v1 otherwise.

### 6.5 Operator API (server, migrations numbered 789 or later)

Server migration versions are positional. The hotkey migration is appended after the pending provider-intent migrations
(787 and 788) and labeled 789 in the code, `SIGNALS.md` and the monitor contracts. It widens the
`st_payout_wallet_resolution.mode` check to `provider`, `network` and `hotkey`. The server branch merges to main only after
787 and 788 are there, and the head migration is rechecked right before the merge.

Wire encodings:
- In the new routes' own bodies, hashes are `[32]byte`, written as 32-integer JSON arrays, as in the existing history
  `head_hash`.
- Originals use the protocol JSON encoding, so signatures are 64-integer arrays.
- The delegation challenge result reuses `SnWalletMappingChallengeResult` (`{message}`), and the delegation history result
  reuses `SnWalletMappingHistoryResult` (`{originals}`).
- The delegation accept reuses the existing accept route and `SnSetWalletArgs` without `client_id`:
  - `message` is the delegation;
  - `signature` is the hotkey's sr25519 signature in hex;
  - `coldkey_ss58` carries the signing hotkey's ss58 (the signer address) and must equal the statement's hotkey.

  The server tells the kinds apart by message prefix. The result's `mapping_hash` stays unprefixed hex, as for the
  existing kinds.
- The `GET /sn/wallet` entry's hashes are strings: `"0x"` plus 64 lowercase hex characters.
- Spec operation IDs:
  - "Sn Hotkey Wallet Mapping Consent";
  - "Sn Hotkey Wallet Mapping History";
  - "Sn Hotkey Network Delegation Challenge";
  - "Sn Hotkey Network Delegation History".

| Route | Auth | Request | Response |
| --- | --- | --- | --- |
| `POST /sn/wallet/hotkey-consent` | network JWT | `{originals: [HotkeyWalletMappingConsent...]}`, the complete chain from generation 1 | `{hotkey_ss58, head_hash, generation}` |
| `POST /sn/wallet/hotkey-consent/history` | public | `{hotkey_ss58, head_hash, generation}` | `{originals}` |
| `POST /sn/wallet/hotkey-delegation` | network JWT, no `client_id` | `{hotkey_ss58, consent_head_hash, consent_generation, from_epoch, through_epoch}` | `{message}` (the challenge) |
| `POST /sn/wallet` (existing accept) | network JWT, no `client_id` | delegation message plus the hotkey `signature` (hex), in the same body shape as a network consent | `{mapping_hash, mapping_generation}` |
| `POST /sn/wallet/hotkey-delegation/history` | public | `{domain, network_id, head_hash, generation}` | `{originals}` |

- **`POST /sn/wallet/hotkey-consent`:**
  - verifies the lineage and that the subnet equals the operator domain's subnet;
  - stores the chain idempotently;
  - refuses a fork (a different original at a stored generation) with `ErrWalletMappingIntegrity`.
- **`POST /sn/wallet/hotkey-delegation`:**
  - requires that the chain through that head is stored, that its hotkey equals `hotkey_ss58`, and that the session user
    still administers the network;
  - issues a prospective challenge exactly like the network consent challenge.
- **`GET /sn/wallet`:** a network with an accepted delegation lists one network-level hotkey entry. It has no
  `client_id`, like the network consent entry:

  ```json
  {"coldkey_ss58": "<coldkey>", "set_at_millis": 0, "consent_scope": "hotkey", "hotkey_ss58": "<hotkey>",
   "from_epoch": 0, "through_epoch": 0, "consent_head_hash": "0x<64 hex>", "consent_generation": 1,
   "mapping_hash": "0x<64 hex>", "mapping_generation": 1}
  ```

  - `coldkey_ss58` is the coldkey of the global consent generation effective at the current epoch. When no generation is
    effective yet, it is the head generation's coldkey.
  - `from_epoch` and `through_epoch` are the delegation's earning epochs.
  - `mapping_hash` and `mapping_generation` identify the delegation chain head.
  - The new fields are omitted for the other scopes.
  - The entry is listed after the network consent and before the legacy side copy. An old client that takes the first
    network-level entry therefore follows settlement's order.
  - The effective `wallet`:
    - for a client session: its own provider consent, then the network consent, then the hotkey entry, then the client's
      own non-consent wallet, then the side copy;
    - for a network session: the network consent, then the hotkey entry, then the side copy.
  - The SDK's wallet pick follows the same order.
- **Settlement.** `GetStProviderWalletsForEpoch` applies 6.3 with roster v3.
- **Audit.** `st_payout_wallet_resolution` records mode `hotkey` with the delegation and the selected global consent
  (hash and generation).
- **Hotkey consents don't write `st_wallet`.**
- **Spec.** The spec registry and `connect/api/bringyour.yml` document every route and the `hotkey` scope.

### 6.6 Client library (`sn/hotkeywallet`)

```go
// <base>/hotkey-wallet/originals.json (the chain, protocol JSON) and pending.json, written atomically, 0600.
type Store struct{ Directory string }
func (self Store) Chain() ([]protocol.HotkeyWalletMappingConsent, error)
func (self Store) Append(original protocol.HotkeyWalletMappingConsent) error // verifies the full chain first
func (self Store) SetPending(statement protocol.HotkeyWalletMappingStatement) error
func (self Store) Pending() (*protocol.HotkeyWalletMappingStatement, error)
func NextStatement(chain []protocol.HotkeyWalletMappingConsent, subnet protocol.HotkeyWalletMappingSubnet, hotkey, coldkey [32]byte, fromEpoch, throughEpoch uint64, now time.Time) (*protocol.HotkeyWalletMappingStatement, error)
func Sign(key *crv4.Keypair, message string) ([64]byte, error) // signs "<Bytes>" + message + "</Bytes>"
type Operator struct{ ApiUrl, ByJwt string; Client *http.Client }
func (self Operator) SubmitChain(ctx context.Context, chain []protocol.HotkeyWalletMappingConsent) (headHash [32]byte, generation uint64, err error)
// Requests a delegation challenge, checks every requested field and the JWT's network, signs with the hotkey
// and accepts. A delegation that already adopts the head at the operator is left as is.
func (self Operator) EnsureDelegation(ctx context.Context, hotkey *crv4.Keypair, headHash [32]byte, generation uint64, fromEpoch, throughEpoch uint64) error
```

The hotkey signs a delegation challenge only after `protocol.DecodeHotkeyNetworkDelegationStatement` accepts it and
every requested field matches: hotkey, consent head and generation, epochs, and the network named in the JWT.

## 7. Root validator

### 7.1 Takes

New `validator take` commands sign with the coldkey and follow `validator stake add` exactly:
- the native extrinsic path;
- `--apply` versus a dry run that reads the live values;
- `--fee_limit_rao`;
- journaling under `--durable-volumes`.

The commands:
- `validator take status --config=<path>`: the delegate take and the childkey take per netuid for the config hotkey.
- `validator take set --take=<percent> --config=<path> --coldkey_seed_file=<path>`: `decrease_take` or `increase_take`,
  within the chain's limits and rate limit.
- `validator take childkey --netuid=<id> --take=<percent> --config=<path> --coldkey_seed_file=<path>`:
  `set_childkey_take`.

18% is 11,796/65,535. Any root registration the hotkey still needs uses the existing register path, extended for
netuid 0 if necessary.

### 7.2 Deployment on snow (`xops`)

- **`main/ansible/run-validator.sh`**, modeled on `run-edges.sh`:
  - builds the `validator` binary from `sn/cli/validator` for linux/amd64 and linux/arm64 with an isolated `GOCACHE`
    and `-trimpath`;
  - stamps the version from `git describe`;
  - generates the systemd unit;
  - runs `playbook-validator.yml` against snow through `run-playbook.sh`.
- **The unit** runs `validator run --config=<path> --progress-file=<path> --durable-volumes=<path>
  --durable-volumes-sha256=<hash> --operators-refresh=1h` as a dedicated user, with `Restart=always`. The paths and hash
  are inventory variables, and nothing secret is committed.
- **Before starting**, the playbook checks that the config, seed and durable-volume files exist, with mode and owner,
  and refuses to start otherwise.
- **As launched (October 8):** the validator tree is `/srv/sn25/sn-validator`, and `/etc/sn-validator` holds the pending
  and final configs and the durable-volumes declaration, on snow's root filesystem.

## 8. ur.xyz

- **Publication:**
  - follows the `price.yml` model:
    - a tracked canonical copy in the site, `ur.xyz/operators/operators.yml`, lets a build without the sn checkout still
      publish the list;
    - `web/ur.xyz/scripts/sync-operators.mjs`, modeled on `sync-price.mjs` and run by hand like it, refreshes that copy
      from `sn/operators.yml`;
  - the site serves the tracked copy at `/operators.yml`, from `react/public`, which `sync-public` mirrors into
    `astro/public`;
  - the nginx ur.xyz server block serves `/operators.yml` as `application/yaml`;
  - `nginx-smoke-test.sh` checks it.
- **Docs:** `web/ur.xyz/docs/miner/README.md` and `docs/validator/README.md` document `--all-operators`,
  `--operators-refresh`, `--auto-register` and `auth --operator`, plus the hotkey payout. This happens after the miner and
  validator changes land, against the landed usage text. The miner guide's 10/90 and momentum section is not reworded
  without the owner.

## 9. Work streams

| Stream | Repo and location | Owns |
| --- | --- | --- |
| validator-ops | sn worktree branch `feat/operator-discovery-validator` | `validator/run.go`, new `validator/operators_*.go`, `VALIDATOR.md` |
| miner-ops | sn worktree branch `feat/operator-discovery-miner` | `miner/run.go`, new `miner/operators_*.go`, `miner/hotkey_wallet_cmd*.go`, miner docs |
| take-root | sn worktree branch `feat/operator-discovery-take` | new `validator/take*.go` and its usage lines, `mainnet/LAUNCH.md` root validator section, `MAINNET.md` records |
| protocol-hotkey | sn main checkout | `protocol/hotkey_*.go`, `protocol/earning_wallet*.go` |
| roster-verifier | sn main checkout | `payoutartifact`, `validator/provider_wallet_mapping*.go`, `mainnet/economic_conservation_*`, `payoutroster` |
| hotkey-wallet | sn main checkout | new `hotkeywallet/` |
| server-hotkey | server checkout `feat/operator-discovery` | migrations ≥789, hotkey consent model, controller, routes, settlement, audit, spec registry |
| sdk-connect | sdk and connect checkouts `feat/operator-discovery` | sdk wallet pick precedence; connect `api/bringyour.yml` |
| web-ur | web worktree branch `feat/operators-yml` | sync script, public copies, nginx, smoke test, then the docs |
| xops-validator | xops worktree branch `feat/root-validator` | `run-validator.sh`, `playbook-validator.yml`, templates, inventory variables, a runbook |

Rules for every stream:
- **Ownership.** Edit only owned files. Leave changes uncommitted. Never rebase, never rewrite history, and never add AI
  attribution.
- **Builds.** Keep each saved file compilable. If a build fails in a file another stream owns, wait and retry; don't edit
  it.
- **Style.** Follow `connect/CODESTYLE.md`:
  - `self` receivers;
  - `Id`/`Url`-style acronyms in new identifiers;
  - no `t.Run` for positive tests;
  - deterministic tests without sleeps.
- **Bug fixes.** Each bug fix gets a regression test that fails before and passes after.
- **Formatting.** Use `gofmt` on edited files only.
