# Operator withdrawal ownership correction

Qualification of registration source `9a5b2638` completed 60 normal validator
roots: 58 passed and two failed. The admitted-invalid-refresh and live-revocation
public `RunRelease` roots both failed after ten pure canceled errors. Race
reproduced the same two failures; all original captures remain retained.

The root cause is the outer `SubmitOnce` ownership gate calling the active
reserved-upload replica constructor before reopening the retained native intent.
Correct live API withdrawal cancels that upload session. Its cancellation is
neither a contradiction in historical source authority nor service cancellation.

The correction separates configured census/source ownership from active session
readiness. Production's outer historical gate validates every configured owner,
concrete route/bound, original activation and private source signer without asking
the API session to be active. Actual publication constructors and callbacks keep
their original active-session checks; no write or rebroadcast authority follows
from historical observation. Existing config, intent, disk, chain and measurement
ownership checks remain. The other constructor consumers were traced: initial
attach admits newly constructed sessions; deposit audit, runtime publication,
terminal sealing and live collection are effects and retain active checks.

The two real public-root regressions retain the original nonempty store, exact
signed bytes, one broadcast and eventual applied receipt assertions. A private
copied transition observer orders the native response after actual API withdrawal
and any rejection-marker persistence attempt. Bounded diagnostic delivery is
checked separately. A new real startup/source census root closes every session,
proves historical ownership remains valid, proves active builders and previously
admitted callbacks refuse writes, and rejects missing/changed source, private key,
concrete writer, route and bounds even while sessions are closed.

Author compile/list and vet only; independent normal/race/causal qualification is
pending. No source bodies, live registration, deployment or chain actions are
performed in this author lane. Preserve the prior 58 normal passes and the
ongoing frozen race/control/full-model bodies; qualify only the changed roots
and relevant upload constructor/withdrawal boundaries on the exact successor.
