//! Offline proof worker. Its owner must enclose this subprocess in a finite
//! deadline, kill and join on cancellation, and bound both output pipes. This
//! raw worker is not a runtime admission or transaction fee verification CLI.

#![deny(unsafe_code)]

use runtime_metadata_probe::historical::{replay_historical_json, MAXIMUM_NATIVE_JOB_BYTES};
use std::io::{Read, Write};

fn main() {
    if std::env::args().skip(1).collect::<Vec<_>>() != ["--historical-proof-replay-v1"] {
        eprintln!("runtime-historical-proof: fixed invocation argument required");
        std::process::exit(1);
    }
    let mut raw = Vec::new();
    let outcome = std::io::stdin()
        .take((MAXIMUM_NATIVE_JOB_BYTES + 1) as u64)
        .read_to_end(&mut raw)
        .map_err(|e| e.to_string())
        .and_then(|_| replay_historical_json(&raw).map_err(|e| e.to_string()));
    match outcome {
        Ok(report) => {
            if std::io::stdout().write_all(&report).is_err() {
                std::process::exit(1);
            }
        }
        Err(error) => {
            eprintln!("runtime-historical-proof: {error}");
            std::process::exit(1);
        }
    }
}
