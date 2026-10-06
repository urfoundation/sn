//! Read-only complete-witness collector. Its parent owns the retained node
//! directory on fd4, exact executable/input pins, output bounds and deadline.

#![deny(unsafe_code)]

use runtime_metadata_probe::historical::capture::{
    capture_historical_directory_feed_json, MAXIMUM_CAPTURE_REQUEST_BYTES,
};
use std::{
    fs::{File, OpenOptions},
    io::{Read, Write},
    os::unix::fs::FileTypeExt,
};

fn main() {
    let arguments = std::env::args().skip(1).collect::<Vec<_>>();
    let feed = arguments == ["--historical-proof-capture-feed-v1"];
    if !feed && arguments != ["--historical-proof-capture-v1"] {
        eprintln!("runtime-historical-capture: fixed invocation argument required");
        std::process::exit(1);
    }
    let mut raw = Vec::new();
    let result = (|| {
        let root = File::open("/proc/self/fd/4").map_err(|e| e.to_string())?;
        std::io::stdin()
            .take((MAXIMUM_CAPTURE_REQUEST_BYTES + 1) as u64)
            .read_to_end(&mut raw)
            .map_err(|e| e.to_string())?;
        let pipes = if feed {
            let request = OpenOptions::new()
                .write(true)
                .open("/proc/self/fd/5")
                .map_err(|e| e.to_string())?;
            let response = File::open("/proc/self/fd/6").map_err(|e| e.to_string())?;
            for pipe in [&request, &response] {
                if !pipe
                    .metadata()
                    .map_err(|e| e.to_string())?
                    .file_type()
                    .is_fifo()
                {
                    return Err(
                        "historical capture feed descriptor is not an owned pipe".to_owned()
                    );
                }
            }
            Some((request, response))
        } else {
            None
        };
        capture_historical_directory_feed_json(
            &raw,
            &root,
            pipes
                .as_ref()
                .map(|(request, response)| (request, response)),
        )
        .map_err(|e| e.to_string())
    })();
    match result {
        Ok(report) => {
            if std::io::stdout().write_all(&report).is_err() {
                std::process::exit(1);
            }
        }
        Err(error) => {
            eprintln!("runtime-historical-capture: {error}");
            std::process::exit(1);
        }
    }
}
