# Native monitor complete archive restore candidate

`monitor-native-archive restore-request --request FILE --request-sha256 HASH`
is a read-only request builder for the existing `storage-prepare plan` and
`storage-prepare apply` commands. It decodes the independently expected policy,
the exact original active checkpoint and every referenced original segment from
the reviewed physical archive. All retained signed catalog revisions and the
complete prefix must authenticate. It derives fixed snapshot owners by exact
name/reference, never by prefix or glob. Additional co-owners remain explicit
in the preparation request; the core planner requires complete union coverage.

The original logical paths remain unchanged. Only the physical restore planner
may derive new inode-bound snapshot metadata. Original economic checkpoints,
pending ranges, runtime reviews and capacity signatures remain byte-identical.
The emitted request grants no restart, signing or service activation authority.
Planning/applying requires the stopped and joined source fence plus the separate
reviewed target fence. Healthy owners outside the affected root keep their leases.

The optional preparation capacity profile
`urnetwork-preparation-many-owners-v1` permits up to 2048 owner profiles and
2048 owner attributes. An omitted profile retains the original 32/128 limits.
These are independent count ceilings, not a claim that every combination fits:
the request remains at most 1 MiB, the accepted plan 8 MiB, each attribute 4096
bytes, each control record 64 KiB and the retained control journal 64 MiB.
The native request builder requires explicit two-times inventory entry, byte,
attribute and inode margins before returning output. The actual planner joins
all serialized sizes before target effects. No existing signed limit is edited.

New controls include 512 retained segments plus active native and chain heads,
actual public export/request/plan/apply/reopen, an RPC-produced pending outcome,
lost completion output, copied-head loss, wrong authority and incomplete reserve
refusal. The large count control uses synthetic retained observations through
the real state transition and snapshot grammar; it does not prove historical RPC
truth. Its private scratch root must be short enough for all 512 actual absolute
references to fit the independently enforced 128-KiB signed catalog byte bound.

This candidate covers one complete original root. The existing archive command
also permits segments in separately declared roots. Cohort-consistent preflight,
partial cohort restoration and later exact public reopening across those roots
remain required; a same-root pass does not qualify that accepted namespace.
Claim/EVM archive adapters, indefinite authenticated paging, current-source
composition, actual backup medium and live activation remain separate gates.
