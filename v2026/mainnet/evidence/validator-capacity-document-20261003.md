The capacity preview emits `config_document` in the strict YAML grammar consumed
by `LoadReleaseConfig`, alongside its structured `config` view. The exact
document is decoded and hash-checked before signing bytes are returned. Default
Go JSON names of nested disk bounds (for example `MaxRecordBytes`) are not the
document's established YAML names (`maxrecordbytes`). The prior e606 guard
incorrectly assumed those representations were interchangeable; its two failed
normal/race roots remain separate evidence.

After an independent approval is returned, the read-only
`validator-capacity-config --preview FILE --preview-sha256 HASH --approval FILE
--approval-sha256 HASH --config-path FUTURE_PATH` command fills only the approval
file descriptor fields excluded by the existing config hash grammar. It checks
the view, exact document, emitted signing message, original history and actual
public signature through the full loader, then emits YAML to stdout. It reads
no private key, creates no output file and requires no stopped durable owner.
Adoption still requires the affected owner to join and reopen its original
custody under the separately approved successor.

The retained-sidecar fixture now selects a complete publicly loadable evidence
profile after all measurement-helper overrides, then checks its bounds,
operator/reference paths and policy before the original approval. The old
fixture accidentally combined a 1 MiB artifact allowance with a 4 MiB closure
allowance. No production bound or signature check is weakened.

Qualification is pending on this source. The actual public preview/sign/config
completion/load control and retained-sidecar control remain distinct. This is
the initial offline validator resource revision; other roles, archive catalog
revision, current composed dependency qualification and live adoption remain
open.
