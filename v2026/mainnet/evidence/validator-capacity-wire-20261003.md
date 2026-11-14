# Capacity signing document follow-up

The frozen `ec396f3a` fixture correction leaves a distinct pre-sidecar failure: the recycle helper signs an in-memory configuration whose nil slices become concrete empty slices when written as YAML. The public loader correctly refuses the changed config hash. The successor completes strict document decoding and lower-case address normalization before the fixture's original production approval; it verifies a second wire round-trip is idempotent. No retained approval or sidecar is rewritten to pass.

The production preview now explicitly round-trips the exact exported JSON config through the strict document decoder before calculating the approval message. Any hash change refuses the unsigned preview. This is an additional pre-sign validation, not evidence that the previously passing public preview produced an invalid signature.

The actual command control signs its returned message bytes directly, writes the envelope/config, then uses `LoadReleaseConfig` and checks the exact approved hash. It no longer calls a fixture helper that could normalize and recalculate the proposal before signing. The separate retained-sidecar control verifies the original proof and historical signing refusal after the resource revision. Both controls require independent qualification; the failed `b1` and `ec396` results remain unchanged.

This continues the initial validator-only resource revision. Affected owners are quiesced for physical census and adoption; healthy peers need not stop. It does not implement all-role archive/rollover, unattended renewal or live activation.
