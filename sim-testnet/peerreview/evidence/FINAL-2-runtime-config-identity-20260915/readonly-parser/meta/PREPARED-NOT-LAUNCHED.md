# Prepared read-only renderer

Copied from the accepted noncanonical runtime-458 renderer r2 and adjusted only
for this candidate base, xops `42bfe0b`, a dynamic `EXPECTED_SN` admission value,
and this output path. The candidate remains mutable, so no compiler has been
started and no source-pair has been created.

At freeze, invoke only:

```sh
EXPECTED_SN=<final-formatted-40-hex-SN-head> \
  capture/renderer-cli-build.capture.sh
```

The body takes fresh before/after 13-row source observations, builds exactly once
with Go 1.26.6 and the warmed offline caches, and records build metadata without
requiring a VCS stamp or `origin/main` equality. It has no test, RPC, native, or
release-lock-apply action. Root uses its produced binary solely for a subsequent
release-lock preview.
