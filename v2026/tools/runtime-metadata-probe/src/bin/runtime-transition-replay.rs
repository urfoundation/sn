//! Offline worker. Its production caller owns the deadline, child process and
//! bounded output pipes; direct invocation alone supplies no wall-clock limit.

#![deny(unsafe_code)]

use runtime_metadata_probe::replay::{replay_json, MAXIMUM_JOB_BYTES};
use std::io::{Read, Write};

fn main() {
    if std::env::args().skip(1).collect::<Vec<_>>() != ["--runtime-transition-replay-v1"] {
        eprintln!("runtime-transition-replay: fixed invocation argument required");
        std::process::exit(1);
    }
    let mut raw = Vec::new();
    let outcome = std::io::stdin()
        .take((MAXIMUM_JOB_BYTES + 1) as u64)
        .read_to_end(&mut raw)
        .map_err(|e| e.to_string())
        .and_then(|_| replay_json(&raw).map_err(|e| e.to_string()));
    match outcome {
        Ok(report) => {
            if std::io::stdout().write_all(&report).is_err() {
                std::process::exit(1);
            }
        }
        Err(error) => {
            eprintln!("runtime-transition-replay: {error}");
            std::process::exit(1);
        }
    }
}
