# Operator payout roster

`payoutroster` prepares, signs, retains and publishes the independently approved
whole-work roster for one deployment domain and epoch. Run it on a dedicated
operator roster-authority host. Its EVM signing key and retained state belong to
that authority, separately from the payout artifact publisher. The configuration
rejects an `authority_signer` equal to `artifact_signer`.

The operator supplies the complete SDK owner and provider population. An
enrollment proves a named SDK generation's identity; a consent proves a named
wallet mapping. Neither an API index nor a SQL query determines that population
is complete. Include idle providers, unmapped providers and all required old SDK
generations after rotation. The artifact publisher consumes the signed roster
and cannot choose a smaller population from observed paid work.

The executable is built from `./cli/payoutroster`. Its four commands are
`prepare`, `sign`, `once` and `run`. All require an explicit `--config` path.
The production host is Linux; private custody and atomic queue retirement use
Linux descriptor-relative filesystem operations.

## Authority host and configuration

Provision `vault/main/payout_roster.yml` through the authority host's existing
Vault configuration workflow. `key_file` names a separately provisioned private
key file; private key material never appears inline in this YAML. The file holds
one 32-byte secp256k1 key as 64 hexadecimal characters, with an optional `0x`
prefix and surrounding whitespace. It must be a regular, single-link file owned
by the service user with mode `0600`.

The following is a complete **synthetic public configuration example**. Replace
its deployment identity, public keys, addresses, endpoint and paths with the
independently approved production values. It contains no signing key.

```yaml
schema: urnetwork-payout-roster-config-v1
domain:
  chain_id: 1
  genesis_hash: "0x1111111111111111111111111111111111111111111111111111111111111111"
  netuid: 1
  coordinator: "0x2222222222222222222222222222222222222222"
  settlement_vault: "0x3333333333333333333333333333333333333333"
  deployment_id_hash: "0x4444444444444444444444444444444444444444444444444444444444444444"
  policy_hash: "0x5555555555555555555555555555555555555555555555555555555555555555"
  no_id: 1
request_public_key: "0x6666666666666666666666666666666666666666666666666666666666666666"
authority_signer: "0x7777777777777777777777777777777777777777"
artifact_signer: "0x8888888888888888888888888888888888888888"
client_key_root_signer: "0x9999999999999999999999999999999999999999"
api_base: https://api.example.invalid
key_file: /srv/urnetwork/vault/main/payout-roster-authority.key
state_directory: /srv/urnetwork/payout-roster/state
inbox_directory: /srv/urnetwork/payout-roster/inbox
```

The domain is the exact `protocol.ClientKeyHistoryDomain` used by the deployment.
Its digest must match Server's `provider_work.yml` `domain_hash`.
`request_public_key` is the approved Ed25519 boundary-request public key;
`client_key_root_signer` is the approved EVM signer of client-key registrations
and prospective wallet approvals. Configure the matching public pins on the API
and payout worker as described in
[Server's custody guide](../../server/PROVIDER-WORK-CUSTODY.md). Those processes
receive the authority's public address.

The three 32-byte domain hashes and `request_public_key` accept exactly 64 hex
characters, optionally prefixed with `0x`. Addresses use the usual `0x` plus 40
hex characters. YAML rejects unknown fields, duplicate keys and extra documents.
The JSON protocol objects used below retain their own encoding: fixed byte
arrays are numeric arrays and `[]byte` originals are base64 strings.

`api_base` must be an absolute HTTPS URL without credentials, query parameters or
a fragment. A base path is preserved. `key_file`, `state_directory` and
`inbox_directory` must be canonical absolute paths. Keep state and inbox in
separate directories, with neither containing the other or the key. Use physical
paths without symlinks.

Provision private directories as the user that will run the command:

```sh
install -d -m 0700 /srv/urnetwork/payout-roster/review
install -d -m 0700 /srv/urnetwork/payout-roster/state
install -d -m 0700 /srv/urnetwork/payout-roster/inbox
```

The review output's parent and the inbox must already exist, be owned by that
user and have mode `0700`. Requests are regular, single-link, user-owned `0600`
files. Retained state also uses a private `0700` directory and `0600` files.

## Assemble the explicit complete input

Create a reviewed inventory outside the artifact publisher. It must name every
required `(client, SDK generation, public key)` owner and every provider's
`(client, network)` identity for the epoch. Reconcile that inventory with the
operator's enrollment and lifecycle records, including retired generations whose
original work still belongs to the window. Set `complete: true` only after this
review. It is the operator's assertion; the program cannot infer completeness
from the evidence it receives.

Each provider must also have an SDK owner in the same network. Owner tuples are
unique by client and SDK generation; provider clients are unique. Keep distinct
SDK generations as distinct owner entries even when their client or key repeats.

Collect originals for the already selected identities:

| Input | Original source and selection |
| --- | --- |
| SDK enrollment | `GET /provider-work/v1/owners?domain=<lowerhex32>&client=<lowerhex16>&key=<lowerhex32>&generation=<lowerhex16>` returns the exact canonical signed enrollment. Omit only `generation` to retrieve the bounded historical index for that explicitly selected client/key. The index's `owners` are base64 original byte strings. Select every required generation explicitly. |
| Client-key registration | The admission record retained with the SDK enrollment, or `GET /key/<client-UUID>/history`. The latter returns `history`, an array of base64 signed registration bytes. Decode a selected original into a `protocol.ClientKeyRegistration` object; retain its signature and all fields. `prepare` verifies the configured root signer, exact domain, client, key and present registration. |
| Provider wallet consent | `POST /sn/wallet/consent/history` with `domain`, `client_id`, `head_hash` and `generation` returns `originals`. The domain is the protocol JSON object, `client_id` is the Server UUID string, `head_hash` is a 32-element numeric byte array, and `generation` is the independently selected full history head. Retain every original from generation one through that head. |
| Network wallet consent | `POST /sn/wallet/network-consent/history` with `domain`, `network_id`, `head_hash` and `generation` returns `originals`. Use the protocol domain, the Server network UUID string and the independently selected 32-byte numeric-array head and generation from the network acceptance record. Retain the complete network chain. |
| Hotkey delegation | `POST /sn/wallet/hotkey-delegation/history` takes the same `domain`, `network_id`, `head_hash` and `generation` fields and returns `originals`. The head and generation come from the delegation's acceptance record. Retain the complete delegation chain. |
| Global hotkey consent | `POST /sn/wallet/hotkey-consent/history` with `hotkey_ss58`, `head_hash` and `generation` returns `originals`. Select the head that the delegation effective at the epoch names; [Hotkey delegations](#hotkey-delegations-and-the-third-earning-mode) explains how. Retain the chain from generation one through exactly that head. |
| Epoch clock | Independently selected start/end boundaries and their exact committed RLP headers, supplied as `payoutartifact.ClosedWorkWindowClock`. The production `header_profile` is `frontier-legacy-rlp15-milliseconds`. |
| Prior contracts | The explicit reconciled `WholeWorkPriorContract` list from verified original predecessor artifacts and whole-work witnesses. Use `[]` for a reviewed first prospective window. |
| Work sources | The explicitly approved `protocol.ProviderWorkSourceAuthority` list of source identities, public keys, time bounds and capacities for the window, if used. The roster signature binds these declarations. |

The owner query requires each selector exactly once and accepts no additional
parameters. Its domain selector is `ClientKeyHistoryDomain.Digest()`, not a hash
of a JSON rendering. The client-key history endpoint serves current consumer
lookups with a 64-registration bound; a retired, unavailable or refused census
can appear as `history: []`. That response cannot justify dropping an owner.
Obtain its original admission registration from retained custody before preparing
the roster.

For example, retrieve one already selected SDK generation and its client's
published registrations with the following public values. Replace every
`APPROVED_...` value from the reviewed inventory; the SDK generation is a 16-byte
lifecycle identifier, distinct from the registration's integer generation.

```sh
roster_api='https://api.example.invalid'
roster_domain='APPROVED_LOWERCASE_DOMAIN_HASH'
roster_client='APPROVED_LOWERCASE_CLIENT_HEX16'
roster_client_uuid='APPROVED_CLIENT_UUID'
roster_generation='APPROVED_LOWERCASE_SDK_GENERATION_HEX16'
roster_public_key='APPROVED_LOWERCASE_PUBLIC_KEY_HEX32'

curl --proto '=https' --fail --silent --show-error --max-time 300 \
  --get "$roster_api/provider-work/v1/owners" \
  --data-urlencode "domain=$roster_domain" \
  --data-urlencode "client=$roster_client" \
  --data-urlencode "key=$roster_public_key" \
  --data-urlencode "generation=$roster_generation" \
  --output owner-1.json

curl --proto '=https' --fail --silent --show-error --max-time 300 \
  "$roster_api/key/$roster_client_uuid/history" \
  --output client-key-history.json
```

Extract the published registration originals without rewriting them, then select
the exact reviewed admission registration as `registration-1.json`:

```sh
python3 - <<'PY'
import base64
import json
import os
from pathlib import Path

os.umask(0o077)
history = json.loads(Path("client-key-history.json").read_bytes())["history"]
for index, original in enumerate(history, start=1):
    with open(f"client-registration-{index}.json", "xb") as output:
        output.write(base64.b64decode(original, validate=True))
PY
```

For each independently pinned provider wallet head, save the JSON body described
in the source table as `provider-1-history-request.json` and fetch its originals:

```sh
curl --proto '=https' --fail --silent --show-error --max-time 300 \
  --header 'Content-Type: application/json' \
  --data-binary @provider-1-history-request.json \
  "$roster_api/sn/wallet/consent/history" \
  --output provider-1-wallet-history.json
```

For a selected network head, use the analogous network request body and route:

```sh
curl --proto '=https' --fail --silent --show-error --max-time 300 \
  --header 'Content-Type: application/json' \
  --data-binary @network-1-history-request.json \
  "$roster_api/sn/wallet/network-consent/history" \
  --output network-1-wallet-history.json
```

A selected delegation head uses the same request body on its own route. The
global consent request that follows it is described in
[Hotkey delegations](#hotkey-delegations-and-the-third-earning-mode):

```sh
curl --proto '=https' --fail --silent --show-error --max-time 300 \
  --header 'Content-Type: application/json' \
  --data-binary @delegation-1-history-request.json \
  "$roster_api/sn/wallet/hotkey-delegation/history" \
  --output delegation-1-history.json

curl --proto '=https' --fail --silent --show-error --max-time 300 \
  --header 'Content-Type: application/json' \
  --data-binary @hotkey-consent-1-history-request.json \
  "$roster_api/sn/wallet/hotkey-consent/history" \
  --output hotkey-consent-1-history.json
```

The head selectors come from the reviewed `mapping_hash` and
`mapping_generation` returned by consent acceptance. The history routes return
originals for that selection; they do not choose a latest head. Supply each full
chain, including retained rotations that become effective after this epoch.
The producer derives the head from the originals and checks continuity, domain,
identities and signatures. Provider, network and delegation originals share
the `WalletMappingConsent` JSON container, but their signed `message` prefixes,
schemas and scopes differ. Copy them unchanged into their corresponding input
arrays; they are not interchangeable. Before approval, compare every derived
`wallet_head_hash`, `wallet_generation`, `delegation_head_hash` and
`delegation_generation` with the independently recorded acceptance selection.

An empty provider `wallet_consents` array asserts independently reviewed absence
of an own-consent chain. A failed history fetch or a missing known original must
remain pending; it cannot be converted into an empty array to enable network
fallback. Retain unmapped providers in the population, and resolve missing
originals without substituting unsigned wallet database rows. With any network
head or delegation present, every provider must supply either its full array or
explicit `[]`; an omitted or `null` `wallet_consents` field is unknown and is
refused. Existing v1 input handling is unchanged when neither is supplied.

The clock object has `start` and `end` objects containing `number` and `hash`,
RFC3339 `start_time` and `end_time`, and base64 `start_header` and `end_header`
originals. Preparation checks each header's hash, block number and timestamp
against its selected boundary. A displayed timestamp or hash without the original
header bytes is insufficient.

The program accepts the strict JSON
[`payoutroster.Input`](../payoutroster/types.go) shape. `owners`, `providers` and
`prior_contracts` must be explicit arrays, including `[]` for a reviewed empty
set. Each owner contains base64 `enrollment` bytes and a `registration` object;
each provider contains numeric-array `client_id` and `network_id` fields and a
`wallet_consents` array. Optional `network_wallets` entries each contain a
numeric-array `network_id` and a complete `wallet_consents` array. Optional
`hotkey_delegations` entries each contain a numeric-array `network_id`, a
boolean `network_wallet_absent`, a complete `delegation_consents` array and a
`hotkey_consents` array. Each network must be referenced by an expected provider
and occur at most once in each list. Input and prepared requests are bounded to
64 MiB.
Duplicate or unknown JSON members are rejected. A capacity refusal requires
resolving the bound; truncating a population or consent chain would change the
independently approved input.

For an offline assembly workflow, put the selected originals in a private review
directory and create `selection.json` there. This is a local assembly manifest,
not an additional API or approval format. The example paths name files already
collected from the sources above; replace the example identities and epoch with
the reviewed inventory. To assert reviewed absence of a provider's own chain,
set both `"wallet_history": null` and `"own_history_absent": true` on that
provider. The helper then emits explicit `wallet_consents: []`, permitting its
network to supply the effective consent. A missing marker or a null/empty
`originals` response is refused.

```json
{
  "complete": true,
  "epoch": 123,
  "clock": "clock.json",
  "owners": [
    {
      "enrollment": "owner-1.json",
      "registration": "registration-1.json"
    }
  ],
  "providers": [
    {
      "client_id": "11111111111111111111111111111111",
      "network_id": "22222222222222222222222222222222",
      "wallet_history": "provider-1-wallet-history.json"
    }
  ],
  "network_wallets": [
    {
      "network_id": "22222222222222222222222222222222",
      "wallet_history": "network-1-wallet-history.json"
    }
  ],
  "hotkey_delegations": [
    {
      "network_id": "22222222222222222222222222222222",
      "network_wallet_absent": false,
      "delegation_history": "delegation-1-history.json",
      "hotkey_consent_history": "hotkey-consent-1-history.json"
    }
  ],
  "prior_contracts": "prior-contracts.json",
  "work_sources": "work-sources.json"
}
```

`registration-1.json` is the decoded signed registration object, not the history
response wrapper. `provider-1-wallet-history.json` is the consent-history response
with its `originals` array; `network-1-wallet-history.json` is the corresponding
network-history response. Omit the manifest's `network_wallets` field, or use
`[]`, when no network heads are approved. `delegation-1-history.json` and
`hotkey-consent-1-history.json` are the delegation-history and global
consent-history responses. Omit `hotkey_delegations`, or use `[]`, when no
delegation heads are approved. Set `network_wallet_absent` to `true` only after
reviewing that the delegated network has no network consent chain. Set
`hotkey_consent_history` to `null` only when no delegation is effective at the
epoch. `prior-contracts.json` and `work-sources.json` are JSON arrays, with `[]`
only when the independently reviewed list is empty.

Run this helper from that directory to produce a new `input.json`. It embeds
exact enrollment bytes, converts the explicitly selected hexadecimal identities
to protocol arrays and retains each full consent response. It performs no
discovery or signing; `prepare` performs the cryptographic checks.

```sh
python3 - <<'PY'
import base64
import json
import os
from pathlib import Path

os.umask(0o077)

def read_json(name):
    return json.loads(Path(name).read_bytes())

def byte_array(value, size):
    raw = bytes.fromhex(value)
    if len(raw) != size:
        raise ValueError("incorrect selected identity length")
    return list(raw)

def consent_history(name):
    originals = read_json(name)["originals"]
    if not isinstance(originals, list) or not originals:
        raise ValueError("selected history must contain a nonempty originals array")
    return originals

selection = read_json("selection.json")
if selection["complete"] is not True:
    raise ValueError("the operator must review the complete inventory first")

owners = []
for owner in selection["owners"]:
    owners.append({
        "enrollment": base64.b64encode(
            Path(owner["enrollment"]).read_bytes()).decode("ascii"),
        "registration": read_json(owner["registration"]),
    })

providers = []
for provider in selection["providers"]:
    history_file = provider["wallet_history"]
    own_absent = provider.get("own_history_absent", False)
    if type(own_absent) is not bool:
        raise ValueError("own_history_absent must be an explicit boolean")
    if history_file is None:
        if not own_absent:
            raise ValueError("absent own history requires explicit reviewed approval")
        originals = []
    else:
        if own_absent:
            raise ValueError("a supplied own history contradicts reviewed absence")
        originals = consent_history(history_file)
    providers.append({
        "client_id": byte_array(provider["client_id"], 16),
        "network_id": byte_array(provider["network_id"], 16),
        "wallet_consents": originals,
    })

network_wallets = []
for network in selection.get("network_wallets", []):
    network_wallets.append({
        "network_id": byte_array(network["network_id"], 16),
        "wallet_consents": consent_history(network["wallet_history"]),
    })

hotkey_delegations = []
for delegation in selection.get("hotkey_delegations", []):
    network_absent = delegation["network_wallet_absent"]
    if type(network_absent) is not bool:
        raise ValueError("network_wallet_absent must be an explicit boolean")
    consent_file = delegation["hotkey_consent_history"]
    hotkey_delegations.append({
        "network_id": byte_array(delegation["network_id"], 16),
        "network_wallet_absent": network_absent,
        "delegation_consents": consent_history(delegation["delegation_history"]),
        "hotkey_consents": [] if consent_file is None else consent_history(consent_file),
    })

value = {
    "schema": "urnetwork-payout-roster-input-v1",
    "complete": selection["complete"],
    "epoch": selection["epoch"],
    "clock": read_json(selection["clock"]),
    "owners": owners,
    "providers": providers,
    "prior_contracts": read_json(selection["prior_contracts"]),
    "work_sources": read_json(selection["work_sources"]),
}
if network_wallets:
    value["network_wallets"] = network_wallets
if hotkey_delegations:
    value["hotkey_delegations"] = hotkey_delegations
with open("input.json", "x") as output:
    json.dump(value, output, separators=(",", ":"))
    output.write("\n")
PY
```

## Network heads and earning-wallet precedence

A nonempty network input produces
`urnetwork-whole-work-authority-v2`, with sorted `network_wallets` entries
containing `network_id`, `wallet_head_hash` and `wallet_generation`. The head pins
the full retained chain, including future rotations; the consent selected for
this epoch can be an earlier generation. Without network inputs, the producer
keeps the v1 authority schema and canonical bytes. Use API, payout worker and
verifier releases that support network consent for v2; older verifiers refuse
that schema.

The producer uses the shared `protocol.SelectEarningWallet` rules also used by
Server settlement and the independent verifier:

| Provider's own-consent result for the epoch | Resolution |
| --- | --- |
| Effective consent that passes its prospective gate | The provider's own coldkey wins, including when a network head is present. |
| Reviewed absent chain (`ErrWalletMappingAbsent`), or a fully verified chain with no effective consent (`ErrWalletMappingNotEffective`) | Try the network's effective consent and prospective gate. |
| Missing originals, broken lineage, invalid signatures, selected legacy consent, or a failed prospective gate | No network fallback. A generic `ErrWalletMappingUnavailable` is insufficient to permit fallback. |
| Neither eligible chain supplies an effective consent | With a delegation of the network, hotkey mode is tried next (see [Hotkey delegations](#hotkey-delegations-and-the-third-earning-mode)). Otherwise the provider remains explicitly unmapped; it is retained in the roster. |

Earning intervals are inclusive `from_epoch` through `through_epoch`. The
five-minute challenge expiry governs acceptance; it does not shorten that
earning interval. Both modes also require the issuance boundary before the
epoch's start and the acceptance expiry no later than that start. An expired
own earning interval can permit network fallback; failed prospective evidence
cannot. The assembly helper only copies originals. It does not implement a
second wallet selector or choose a coldkey from the current wallet projection.
See [the network-consent design](../docs/NETWORK-WALLET-CONSENT.md) for the signed
statement format and consent issuance workflow.

For a request with network or delegation input, `prepare` also adds derived
`wallet_selections` review metadata. Each entry identifies `client_id` and
`network_id`, then has either `wallet` or `unavailable: true`. The wallet uses
the shared protocol object's field names: `Mode`, `ClientId`, `NetworkId`,
`Coldkey`, `OriginalHash`, `Generation`, `HeadHash` and `HeadGeneration`. `Mode`
is `provider`, `network` or `hotkey`; the selected original and generation can
precede the pinned chain head. A selected legacy own consent remains unavailable
even when a valid network consent exists.

Review this preview with the originals; let `prepare` generate it. Signing
rederives it and refuses changed request bytes. Settlement and verification
resolve wallets from the signed roster and original histories, so this preview
is not an additional authority. Requests without network or delegation inputs
omit `network_wallets`, `hotkey_delegations` and `wallet_selections` and
preserve their v1 encoding.

## Hotkey delegations and the third earning mode

All-operators miners earn through a global hotkey wallet consent and per-operator
delegations. The coldkey and the hotkey sign the global consent once, for every
operator of the subnet. On each operator, the hotkey signs a delegation of its
network that names one head of that global chain, and the operator co-signs
the delegation's issuance boundary as it does for a network consent. See
[the operator discovery design](../docs/OPERATOR-DISCOVERY.md) for the statement
formats and the client workflow.

A nonempty `hotkey_delegations` input produces
`urnetwork-whole-work-authority-v3`, with sorted `hotkey_delegations` entries
containing `network_id`, `delegation_head_hash` and `delegation_generation`. A v3
roster may also carry `network_wallets`. The roster pins only the delegation
chain; each delegation pins the global consent head it names. Without delegation
inputs, the producer keeps the v2 or v1 schema and its canonical bytes. Use API,
payout worker and verifier releases that support hotkey delegations for v3; older
verifiers refuse that schema.

Hotkey mode is the last of the three earning modes. The producer applies the
shared `protocol.SelectEarningWalletWithHotkey` rules, which Server settlement
and the independent verifier also use, and resolves hotkey mode with
`protocol.ResolveHotkeyEarningWallet`.

The network chain is consulted only when the provider's own chain falls back,
as above. Its result decides whether hotkey mode is tried:

| Network chain result for the epoch | Resolution |
| --- | --- |
| Effective consent that passes its prospective gate | The network consent's coldkey wins, including when a delegation head is present. |
| Absent from the roster, or a fully verified chain with no effective consent | Try the delegation effective at the epoch, its prospective gate, then the global consent effective at the epoch in the chain through the head the delegation names. |
| Missing originals, broken lineage, invalid signatures, or a failed prospective gate | No hotkey fallback. |

When hotkey mode is tried, a network without a delegation head, or with no
delegation or global consent effective at the epoch, leaves the provider
explicitly unmapped; it is retained in the roster. A delegation whose acceptance
may cross the epoch's start is unknown and also leaves the provider unmapped.

The delegation passes the same gate as a network consent: the configured
`client_key_root_signer` issued it, its issuance boundary precedes the epoch's
start, and its acceptance expiry is no later than that start. The global consent
has no operator signature and no expiry; the delegation that names it supplies
both.

Collect the evidence for each delegated network:

1. Fetch the complete delegation chain through the head in the delegation's
   acceptance record. The record is the `mapping_hash` and `mapping_generation`
   returned when the delegation was accepted through `POST /sn/wallet`.
2. Find the delegation effective at the epoch: the last generation whose
   `from_epoch` is at most the epoch. None is effective when no generation
   qualifies or when that generation's `through_epoch` is earlier than the
   epoch.
3. If one is effective, fetch the global consent chain through the
   `consent_head_hash` and `consent_generation` it names, with its `hotkey` as an
   SS58 address (prefix 42). Supply exactly that chain as `hotkey_consents`.
   A later delegation generation can name a later global head; it stays in
   `delegation_consents`, but its global chain does not replace the one this
   epoch uses.
4. If none is effective, supply `hotkey_consents` as `[]`.
5. State the network's own consent chain: supply it in `network_wallets`, or set
   `network_wallet_absent: true` after reviewing that the network has none.
   Without either, `prepare` refuses the request, because a missing network
   chain would otherwise let hotkey mode displace a network consent. A network
   chain supplied together with `network_wallet_absent: true` is a
   contradiction and is also refused.

This helper performs steps 2 and 3 for one fetched delegation history. It
writes the global consent request body, or reports that no delegation is
effective. Replace the epoch with the reviewed epoch:

```sh
python3 - <<'PY'
import hashlib
import json
import os
from pathlib import Path

os.umask(0o077)
epoch = 123
alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

def ss58(public_key):
    payload = bytes([42]) + bytes(public_key)
    checksum = hashlib.blake2b(b"SS58PRE" + payload, digest_size=64).digest()[:2]
    number = int.from_bytes(payload + checksum, "big")
    encoded = ""
    while number:
        number, remainder = divmod(number, 58)
        encoded = alphabet[remainder] + encoded
    return encoded

prefix = "Delegate URnetwork network wallet to hotkey\n"
selected = None
for original in json.loads(Path("delegation-1-history.json").read_bytes())["originals"]:
    if not original["message"].startswith(prefix):
        raise ValueError("history contains a message that is not a hotkey delegation")
    statement = json.loads(original["message"][len(prefix):])
    if statement["from_epoch"] <= epoch:
        selected = statement
if selected is None or selected["through_epoch"] < epoch:
    print("no delegation is effective at the epoch; supply hotkey_consents []")
else:
    with open("hotkey-consent-1-history-request.json", "x") as output:
        json.dump({
            "hotkey_ss58": ss58(selected["hotkey"]),
            "head_hash": selected["consent_head_hash"],
            "generation": selected["consent_generation"],
        }, output, separators=(",", ":"))
PY
```

The helper only selects a request. `prepare` checks every original again. It
refuses a global chain that is missing while a delegation is effective, one that
does not end at the named head, one of another hotkey or subnet, and any global
chain when no delegation is effective. A delegation must name its own network,
the configured domain and root signer, and carry every generation from the
first.

In `wallet_selections`, a hotkey-mode wallet's `Coldkey` is the global
consent's coldkey. `OriginalHash`, `Generation`, `HeadHash` and `HeadGeneration`
describe the delegation chain, the chain the roster pins. Five more fields
describe the global consent: `Hotkey`, `ConsentOriginalHash`,
`ConsentGeneration`, `ConsentHeadHash` and `ConsentHeadGeneration`. Provider- and
network-mode wallets omit these fields, so existing requests keep their bytes.
Review that the hotkey, the global consent's coldkey and both heads match the
miner's approved consent before pinning the request.

## Prepare, review and pin

Preparation is offline. It validates originals and the complete derived roster,
writes a new canonical unsigned request with mode `0600`, syncs the file and
directory, and prints JSON containing `request_sha256`. It refuses an existing
output filename. It does not open the signing key or contact the API.

```sh
payoutroster prepare \
  --config /srv/urnetwork/vault/main/payout_roster.yml \
  --input /srv/urnetwork/payout-roster/review/input.json \
  --output /srv/urnetwork/payout-roster/review/request.json
sha256sum /srv/urnetwork/payout-roster/review/request.json
```

Independently review both `input` originals and the derived `authority` in that
request, including the complete population, domain, epoch, clock, consent and
delegation heads and prior-contract checkpoint, plus `wallet_selections` when
present. Record the exact lowercase 64-character SHA-256 of the approved file.
The digest printed by `prepare` identifies the bytes; it does not supply the
independent approval.
Preserve the canonical file unchanged:
pretty-printing, adding a newline or editing any field changes its identity.

`sign` durably retains the approved request and signed authority locally.
`once` performs the same work and publishes the retained authority. Use the
recorded review digest in the following commands:

```sh
payoutroster sign \
  --config /srv/urnetwork/vault/main/payout_roster.yml \
  --request /srv/urnetwork/payout-roster/review/request.json \
  --request-sha256 APPROVED_LOWERCASE_SHA256

payoutroster once \
  --config /srv/urnetwork/vault/main/payout_roster.yml \
  --request /srv/urnetwork/payout-roster/review/request.json \
  --request-sha256 APPROVED_LOWERCASE_SHA256
```

`once` can also be used directly after review. Both commands rederive the
authority from the retained input and check the supplied digest before loading
a key. Their successful JSON result contains `domain_hash`, `epoch`,
`request_sha256`, `authority_sha256` and `published`. For `sign`, `published` can
already be true if an earlier execution retained a publication acknowledgment.

## Continuous approved inbox

Place a byte-for-byte copy of each approved request in `inbox_directory` with
the exact filename `<approved-lowercase-request-sha256>.json`, owned by the
service user with mode `0600` and a single link. Publish the complete file into
the inbox atomically from a staging directory on the same filesystem. Do not
write an approved filename progressively or hard-link it to the review copy.
Keep the independent approval record with the review originals.

```sh
payoutroster run --config /srv/urnetwork/vault/main/payout_roster.yml
```

`run` scans at most 128 directory entries per poll, processes matching files
sequentially in filename order within that batch, then waits 15 seconds before
the next poll. The directory cursor advances through subsequent batches and
starts a new pass at the end. Each job has a 300-second deadline. A failed job is
logged as pending while processing continues to other jobs and later batches;
later passes retry it. There is no automatic population discovery, epoch
generation or approval in this loop.

After a durable publication acknowledgment, the command moves the request to
`inbox_directory/completed/<request-sha256>.<unique-suffix>.json` without
overwriting an existing file. It creates or verifies the service user's private
`0700` completed directory and preserves the request's `0600` contents. Completed
requests leave the active queue; their exact request, signed authority and
acknowledgment also remain in `state_directory`. A large history or a failing
job does not prevent the cursor from reaching later approvals.

An archival error is reported as pending even if publication already succeeded.
Use the retained state and completed directory to reconcile that outcome; `once`
with the original reviewed request safely confirms the durable acknowledgment.
Keep the review originals and durable state when archiving old completed files.
SIGINT or SIGTERM cancels the current operation and stops the loop.

## Durable publication and recovery

The authority host holds an exclusive filesystem lock across request retention,
signing and publication. Each domain/epoch has one immutable request, one signed
authority and an optional publication acknowledgment in `state_directory`:

| Suffix after `<domain-hash>-<epoch>.` | Retained content |
| --- | --- |
| `request.json` | The exact independently approved canonical request. |
| `authority.json` | The exact signed whole-work authority, synced before POST. |
| `published` | The 32-byte authority SHA-256 acknowledged by the API. |

The command posts the same signed bytes to
`<api_base>/provider-work/v1/authorities`. It accepts success only over verified
TLS with HTTP 200 and the canonical JSON receipt whose `authority_hash` matches
those bytes. The Server's domain/epoch admission is idempotent for identical
originals and rejects a different authority for an already retained epoch.

Transient network errors, HTTP 408, HTTP 429 and HTTP 5xx are retried with the
same original. Transport defaults are a 300-second total budget, a 30-second
attempt timeout and a one-second paced retry delay; the enclosing command or
inbox-job deadline can shorten the remaining budget. TLS or receipt integrity
failures and other HTTP refusals end that publication attempt. `run` retains the
job for the next poll and reports its error.

After a timeout, crash or lost response, rerun `once` with the same request and
review digest, or restart `run` with the same configuration, state and inbox.
An existing authority is loaded and verified without reopening the signing key.
If the durable acknowledgment is absent, it is safe to POST that same authority
again. If the matching acknowledgment is present, no POST is needed.

If only the retained request is missing, the command verifies that the supplied
approved request derives the surviving signed authority before restoring the
request; it does not need the signing key. If an acknowledged authority file is
missing, the original approved request and fixed signing key can reconstruct the
same authority deterministically. Its SHA-256 must match the surviving durable
acknowledgment before either missing request or authority is retained; no POST
occurs. A conflicting reviewed proposal cannot fill those missing slots.

Preserve state across host recovery, including requests that were retained before
signing failed and authorities whose publication outcome is uncertain. A
different request for the same domain/epoch is a custody conflict. Resolve the
original request and publication record; deleting state or inventing a new
request is not a retry. The store removes its own incomplete temporary names
under the exclusive lock during recovery. Keep durable backups of the original
state and review records so a restored host retains the same authority history.
