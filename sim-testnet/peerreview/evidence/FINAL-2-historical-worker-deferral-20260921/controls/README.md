The controls use external `go test -overlay` source replacements. Every expected
negative result must reach its named assertion; compilation/setup failure is not
accepted as a causal result. The initial supplemental pair in `invalid-fixture-v1/`
failed while trying to persist fixture receipts in read-only mode, before reaching
the tested boundary, and is excluded from qualification. Its corrected v2 pair
sets read-only mode only after fixture construction.

`archive-candidate-v2` exercises the provisional branch with receipts eligible
for a strict audit; its passing assertion requires zero archive requests. The
paired `strict-archive-control-v2` disables only that branch and reaches the real
RPC client against the same synthetic unavailable archive. Its failure preserves
the checkpoint error and both blocked-action errors after four bounded attempts.
The original `strict-in-provisional` control has the narrower meaning documented
in the parent report: its fixture's current-plan receipts are skipped by strict
setup, so it proves missing inventory without proving an archive request.

External overlay JSON and Go source, including the separately preserved invalid
first fixture, are in:
`/mnt/data/sn-testnet/qualification/historical-worker-deferral-20260921/controls/`.
