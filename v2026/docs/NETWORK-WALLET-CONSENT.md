# Two payout-wallet consent modes: per-provider and network (design, 2026-10-06)

This is the design record for the two payout-wallet consent modes: the per-provider mode, in which a coldkey owner signs a
consent for one provider client, and the network mode, in which the coldkey owner signs once for a network and every provider
client of that network earns to that coldkey. It records the decisions, the precedence between the two modes, the settlement
evidence (roster v2), the audit record and the items that remain open.

Owner: "we should have a sign-once per network mode and a per-provider signer mode. Let's support both. I see the embedded case
doesn't want to embed a key." Code read at server 0269f474, sn ff1919b2, connect ca2562ba, sdk 364fcb2d, examples
feat/provider-examples 29b78ff.

## 0. Blocker found while verifying the provisioning decision (child clients)

Provider installs as child clients (`source_client_id` set) do not work as public providers today. The FindProviders2 candidate
pool is built only from top-level clients: `UpdateClientScores` requires `network_client.source_client_id IS NULL` in both the
per-location and the per-location-group pools (server/model/network_client_location_model.go, around lines 5341 and 5452), and so
do the public provider counts (`UpdateClientLocations`, ~4069), the URL-probe and egress-probe eligibility
(model/provider_eligibility.go:73,113,129; model/provider_egress_location_model.go:811,946,1055,1143,1245) and provider status
(model/provider_status_model.go:347,813). A child client can register a public provide key and connect, but no public caller is ever
offered it, so it earns nothing. Consent works for child clients (both modes accept any active client of the network).
Options: (a) keep provider installs top-level (current contract) and resolve the 100-client cap separately, or
(b) a separate server change that admits child clients holding a public provide key into those pools, reviewed query by query.
Decided (owner, 2026-10-06): option (a). Provider installs stay top-level clients. `POST /network/auth-client` gets a
provider-intent flag that exempts the client from the 100 top-level client cap, and provider-intent clients are kept out of the
peer list (server, connect and sdk branches `feat/provider-intent`). This design is independent of that choice.

## 1. Decisions

### 1.1 Two modes, one chain each
- **Per-provider mode** (existing, unchanged byte for byte): a consent names domain, user, client, network and coldkey; one
  append-only chain per (domain, client). Schemas `urnetwork-provider-wallet-mapping-consent-v1` (legacy, not a release
  earning consent) and `-v2` (prospective). No change to its messages, storage, endpoints or verification semantics.
- **Network mode** (new): a consent names domain, user, network and coldkey, with no client, and covers every provider client of
  that network, current, future and re-created. One append-only chain per (domain, network). The coldkey owner signs once per
  network (and again only to replace it). No key in any app.

### 1.2 The network statement and its scope
New schema `urnetwork-network-wallet-mapping-consent-v1`, prospective from the start (the operator root key co-signs the
issuance boundary exactly as for per-provider v2). The signed message is a different first line plus canonical JSON:

```
Approve URnetwork network wallet mapping
{"schema":"urnetwork-network-wallet-mapping-consent-v1","scope":"network","domain":{...},"user_id":[...],"network_id":[...],
 "coldkey":[...],"generation":1,"previous_hash":[...],"nonce":[...],"issued_at":...,"expires_at":...,"from_epoch":...,
 "through_epoch":...,"prospective":{...}}
```

Scope is explicit three times: the first line (`network` vs `provider`), the schema and the `scope` field. Decoders require
their own prefix, refuse unknown fields (a network statement has `scope` and no `client_id`; a provider statement the reverse)
and require exact canonical re-encoding, so a signature over one kind can never verify as the other. Nonce: 32 random bytes,
single use; expiry 300 s after issuance; at most 16 open challenges per network; history at most 4096 generations; interval
`through_epoch - from_epoch <= 65535`; generation N+1 must start at a later epoch than generation N; acceptance replays of the
exact original are idempotent (same hash and generation), as for per-provider consents.

### 1.3 Who may request and submit
- Network consent challenge and acceptance: **network JWT only** (no `client_id` claim), i.e. the network owner's session. A
  client JWT is refused. The statement's user and network come from the session, never from the request body.
- Per-provider mode keeps its rules (a client JWT names only itself; a network JWT may name a client of its network).
- History endpoints stay public and require the caller's independently pinned head (no "latest" authority from SQL).

### 1.4 Precedence (settlement and display)
For provider client C in network N at earning epoch E:
1. If C's per-provider chain has a consent **effective at E** (the last generation with `from_epoch <= E`, not expired at E)
   that passes the prospective gate, that consent wins (mode `provider`). Independent signers on a shared network keep control.
2. Otherwise, if C's per-provider chain is **absent** (the roster pins no head for C) or verifies but has **no consent effective
   at E**, the network chain applies: its consent effective at E with the prospective gate (mode `network`).
3. Any other per-provider outcome (missing originals, broken lineage, bad signature, a legacy v1 consent selected for E, a
   consent that fails the prospective gate) is an error: no fallback to the network consent, the epoch's wallet resolution
   stays unavailable, exactly as such a provider blocks resolution today. Falling back on bad evidence could misroute earnings.
4. Neither chain effective at E: unavailable (today's behavior for a provider without a wallet).
The protocol exposes this as one shared function (`protocol.SelectEarningWallet`) used by the server's settlement and by the
independent economic verifier, so they cannot diverge. New sentinel errors `ErrWalletMappingAbsent` and
`ErrWalletMappingNotEffective` are joined with `ErrWalletMappingUnavailable`, so every existing `errors.Is(..., Unavailable)`
check is unchanged.

### 1.5 Time, replacement and revocation
- A consent is effective for earning epochs `[from_epoch, through_epoch]` and only prospectively: `from_epoch` is after the
  operator's finalized boundary at issuance, and settlement accepts it only if it expired before the epoch's start time (the
  existing gate, applied to both modes).
- **Replacement** (both modes): a new generation signed by the new coldkey, effective from its later `from_epoch`; earlier
  epochs keep the earlier generation. The roster pins the head that settlement uses, so a later generation never rewrites a
  closed or approved epoch.
- **Revocation**: no unsigned revocation in either mode. A mapping ends by replacement or at its `through_epoch`. When a
  per-provider consent ends, the client falls back to the network consent from the next epoch (rule 2). To move a client
  from its own coldkey to the network consent early, replace its per-provider consent with one signed by the network's
  coldkey, or let it expire. Rationale: an unsigned revoke would let a compromised backend reroute an independent signer's
  earnings to the network coldkey; replacement already requires the new coldkey's signature.

### 1.6 Settlement evidence and artifacts
- The independently signed whole-work roster (`payoutartifact.WholeWorkAuthority`) pins heads. New optional top-level list
  `network_wallets: [{network_id, wallet_head_hash, wallet_generation}]`, strictly sorted, each network referenced by an expected
  provider. A roster with network wallets has schema `urnetwork-whole-work-authority-v2`; one without keeps `-v1` and identical
  canonical bytes, so every existing roster and artifact (old epochs) is unchanged. Old verifiers refuse v2 (fail closed).
- Per-provider `wallet_head_hash` stays per expected provider; it may now be empty when the provider's network has a network
  wallet in the roster.
- Server settlement (`GetStProviderWalletsForEpoch`) verifies each pinned chain (network chains once per network) and applies
  1.4. The payout artifact schema does not change (leaves and provider inputs still carry the coldkey only); the mode is
  derivable from the published roster plus the public histories.
- **Audit record**: a new append-only table `st_payout_wallet_resolution` records, per published release epoch and provider,
  the mode, coldkey, selected consent hash and generation, and the pinned head, written with the payout artifact.
- The economic conservation verifier (sn/mainnet) retains network originals as evidence (`network_wallets`, omitted when empty
  so old evidence and its hashes are unchanged), verifies them with the same prospective gate and resolves with
  `SelectEarningWallet`.
- The external roster approver must emit v2 rosters with network heads for networks that use network mode. Until it does, a
  network-mode provider without a per-provider head leaves the epoch's wallet resolution unavailable, the same as an unmapped
  provider today. Heads reach the approver from the accept responses (`mapping_hash`, `mapping_generation`), as today.

### 1.7 The side-copy network wallet (`st_wallet`)
Unchanged: it is written by per-provider acceptances and the legacy login-proof path, and is still not a consent. Network
consents do not write it (that would change the legacy settlement path's wallet).

### 1.8 API, spec, SDK and examples
- `POST /sn/wallet/network-consent` (network JWT): `{coldkey_ss58, from_epoch, through_epoch}` -> `{message}`.
- `POST /sn/wallet` (existing) accepts a network consent when the message starts with the network prefix (network JWT, no
  `client_id`); returns `{mapping_hash, mapping_generation}` like per-provider consents; `signature_mismatch` as today.
- `POST /sn/wallet/network-consent/history` (public): `{domain, network_id, head_hash, generation}` -> `{originals}`.
- `GET /sn/wallet`: wallet entries gain `consent_scope` (`provider` or `network`; absent for the legacy side copy) and the network
  consent appears as an entry without `client_id`. The effective `wallet` for a client session: its own provider consent, else
  the network consent, else the legacy side copy; for a network session: the network consent, else the side copy.
- connect/api/bringyour.yml documents the routes; connect and sdk Api bindings gain the network challenge; the SDK's wallet pick
  follows the same precedence; the C ABI and language bindings are regenerated for the new Api methods.
- Examples: the Go wallet tool gets `network-challenge`/`network-accept`; PROVIDER_CONTRACT.md documents both modes and
  recommends network mode for embedded apps (one offline signature for the network) and per-provider mode for independent
  signers.

## 2. Repos and branches
| Repo | Branch | Base | Content |
| --- | --- | --- | --- |
| sn | fix/network-wallet-consent | origin/main ff1919b2 | protocol network consent + selection; roster v2; validator network reader; economic verifier |
| server | fix/network-wallet-consent | origin/main 0269f474 | tables (migration + monitor contracts), model, endpoints, GET /sn/wallet, settlement precedence, audit table |
| connect | fix/network-wallet-consent | origin/main ca2562ba | bringyour.yml routes; Go API client for the network challenge |
| sdk | fix/network-wallet-consent | origin/main 364fcb2d | Api bindings, wallet pick precedence, regenerated C ABI and bindings |
| examples | fix/network-wallet-consent | feat/provider-examples 29b78ff | wallet tool both modes; contract text |

## 3. Tests (deterministic, fail before / pass after)
- sn: network statement round trip and refusals (wrong prefix, provider statement as network and the reverse, unknown fields,
  interval bounds, prospective signature); history lineage (generations, previous hash, increasing from_epoch, effective
  selection, not effective vs absent sentinels); `SelectEarningWallet` table (provider wins, not effective -> network, absent ->
  network, provider integrity error -> no fallback, neither -> unavailable); roster v1 bytes unchanged, v2 validation; validator
  network reader against an httptest server; economic admission with a network-mode provider.
- server (local pg/redis): challenge/accept for network consent; client JWT refused; cross-kind refusals; idempotent replay;
  history read; GET /sn/wallet effective wallet per session kind; `GetStProviderWalletsForEpoch` precedence cases with v2 rosters;
  audit rows; migration catalog contracts for the new versions.
- examples: wallet tool self-test for both modes against the mock API.

## 4. Implementation (2026-10-06, all on `fix/network-wallet-consent`)
| Repo | Commit | Content |
| --- | --- | --- |
| sn | 97c8cec5 | protocol network statement + `SelectEarningWallet` + sentinels; roster v2 `network_wallets`; validator `ReadNetworkBounded`; economic verifier network evidence |
| server | c5144551 | migration 786 (+ monitor contract, SIGNALS.md), model + controller + routes, settlement precedence, `st_payout_wallet_resolution` written before the artifact record, GET /sn/wallet scopes, spec registry |
| connect | e5552f0d | bringyour.yml: both consent routes and their history routes (the provider ones were undocumented), wallet schemas; `SnNetworkWalletMappingChallenge(Sync)` |
| sdk | bc25b3ba | `Api.SnNetworkWalletMappingChallengeSync(WithContext)`, `SnWallet.ConsentScope/ThroughEpoch`, pick precedence; C ABI, five bindings, JS client/types regenerated |
| examples | 559a019 | contract (both modes, top-level provisioning, extender settings), Go wallet tool `network-challenge`/`network-accept` |

Refinements made while implementing:
- GET /sn/wallet effective wallet for a client session: own provider consent, else network consent, else the client's own
  non-consent wallet (login-proof projection), else the side copy. Network consent is listed before the side copy so old
  SDKs that take the first network-level entry get the consent.
- Network consent issuance also requires that the session user still administers the network (`network.admin_user_id`,
  `FOR SHARE` in the same transaction); a JWT for a non-admin user is already refused by auth (401).
- Resolution rows: `AddStPayoutWalletResolutions` is idempotent for an exact retry and refuses a differing row
  (`ErrWalletMappingIntegrity`), never overwrites.

Child clients, verified: the existing server test `TestUpdateClientScoresExcludesDerivedAndInactiveClients` (passes)
connects a child client with public and network provide keys and a location and shows it is never published as provider
supply (location and location-group pools, public counts). The exclusion is deliberate ("derived window clients ... are
not provider supply"). Consent works for child clients in both modes; providing does not. Examples keep top-level
provisioning.

The operator producer is implemented in this repository at `cli/payoutroster`, with reusable code in `payoutroster`.
The [payout roster operator guide](../mainnet/PAYOUT-ROSTER.md) describes its dedicated authority host and key, Vault
configuration, explicit complete inventory, original-history assembly, review and digest pinning, signing, publication,
approved inbox and durable recovery. Network heads from reviewed acceptance records select the full consent histories;
the producer derives and signs a v2 roster when network inputs are present and retains v1 bytes otherwise. It uses the
shared wallet selector, preserves own-consent precedence and retains unmapped providers. This is a local operator tool;
production host provisioning, key distribution and service activation remain deployment work.
