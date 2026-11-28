//! Bounded, non-executing inspection/assembly of an original Wasm callsite review.
#![deny(unsafe_code)]
use runtime_metadata_probe::{
    observation_profile::{self, ProfileProposal},
    parse_digest,
};
use std::{
    env,
    error::Error,
    fs::File,
    io::{self, Read, Write},
    process::ExitCode,
};

fn read(path: &str, bound: usize) -> Result<Vec<u8>, Box<dyn Error>> {
    let mut bytes = Vec::new();
    File::open(path)?
        .take(bound as u64 + 1)
        .read_to_end(&mut bytes)?;
    if bytes.len() > bound {
        return Err(io::Error::other("profile input exceeds bound").into());
    }
    Ok(bytes)
}

fn run() -> Result<(), Box<dyn Error>> {
    let args: Vec<_> = env::args().skip(1).collect();
    let usage = "runtime-observation-profile inspect WASM CODE_SHA256 [FUNCTION_INDEX ...] | assemble WASM PROPOSAL_JSON PROPOSAL_SHA256";
    if args.len() < 3 {
        return Err(io::Error::other(usage).into());
    }
    let output = match args[0].as_str() {
        "inspect" => {
            if args.len() > 35 {
                return Err(io::Error::other("at most32 selected functions").into());
            }
            let digest = parse_digest("original code", &args[2])?;
            let selected = args[3..]
                .iter()
                .map(|value| value.parse::<u32>())
                .collect::<Result<Vec<_>, _>>()?;
            let code = read(&args[1], observation_profile::MAXIMUM_CODE_BYTES)?;
            serde_json::to_vec(&observation_profile::inspect_original(
                &code, digest, &selected,
            )?)?
        }
        "assemble" if args.len() == 4 => {
            let proposal = read(&args[2], observation_profile::MAXIMUM_PROPOSAL_BYTES)?;
            if sp_core::hashing::sha2_256(&proposal) != parse_digest("review proposal", &args[3])? {
                return Err(io::Error::other("review proposal digest differs").into());
            }
            let proposal: ProfileProposal = serde_json::from_slice(&proposal)?;
            let code = read(&args[1], observation_profile::MAXIMUM_CODE_BYTES)?;
            observation_profile::assemble_profile(&code, &proposal)?
        }
        _ => return Err(io::Error::other(usage).into()),
    };
    if output.len() > observation_profile::MAXIMUM_REPORT_BYTES {
        return Err(io::Error::other("profile output exceeds bound").into());
    }
    let mut stdout = io::stdout().lock();
    stdout.write_all(&output)?;
    Ok(())
}

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("runtime-observation-profile: {error}");
            ExitCode::FAILURE
        }
    }
}
