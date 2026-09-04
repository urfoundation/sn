//! Command-line entry point for exact, stateless runtime metadata verification.

#![deny(unsafe_code)]

use runtime_metadata_probe::{
    format_digest, parse_digest, probe_file, ExpectedRuntimeArtifact, POLKADOT_SDK_REVISION,
};
use std::{env, error::Error, io, path::PathBuf, process::ExitCode};

const USAGE: &str = "usage: runtime-metadata-probe <wasm-path> <spec-name> <spec-version> <transaction-version> <state-version> <metadata-version> <code-size> <metadata-size> <code-sha256> <code-blake2b-256> <metadata-sha256> <metadata-blake2b-256>";

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("runtime-metadata-probe: {error}");
            ExitCode::FAILURE
        }
    }
}

fn run() -> Result<(), Box<dyn Error>> {
    let arguments: Vec<String> = env::args().skip(1).collect();
    if arguments.len() != 12 {
        return Err(io::Error::new(io::ErrorKind::InvalidInput, USAGE).into());
    }

    let wasm_path = PathBuf::from(&arguments[0]);
    let expected = ExpectedRuntimeArtifact {
        spec_name: arguments[1].clone(),
        spec_version: parse_unsigned("spec-version", &arguments[2])?,
        transaction_version: parse_unsigned("transaction-version", &arguments[3])?,
        state_version: parse_unsigned("state-version", &arguments[4])?,
        metadata_version: parse_unsigned("metadata-version", &arguments[5])?,
        code_size: parse_unsigned("code-size", &arguments[6])?,
        metadata_size: parse_unsigned("metadata-size", &arguments[7])?,
        code_sha256: parse_digest("code-sha256", &arguments[8])?,
        code_blake2b_256: parse_digest("code-blake2b-256", &arguments[9])?,
        metadata_sha256: parse_digest("metadata-sha256", &arguments[10])?,
        metadata_blake2b_256: parse_digest("metadata-blake2b-256", &arguments[11])?,
    };
    let report = probe_file(&wasm_path, &expected)?;

    println!(
        "runtime metadata verified sdk_revision={} spec_name={} spec_version={} transaction_version={} state_version={} metadata_version={} code_size={} metadata_size={} code_sha256={} code_blake2b_256={} metadata_sha256={} metadata_blake2b_256={}",
        POLKADOT_SDK_REVISION,
        expected.spec_name,
        expected.spec_version,
        expected.transaction_version,
        expected.state_version,
        report.metadata_version,
        report.code_size,
        report.metadata_size,
        format_digest(&report.code_sha256),
        format_digest(&report.code_blake2b_256),
        format_digest(&report.metadata_sha256),
        format_digest(&report.metadata_blake2b_256)
    );
    Ok(())
}

fn parse_unsigned<T>(label: &str, value: &str) -> Result<T, io::Error>
where
    T: std::str::FromStr,
    T::Err: std::fmt::Display,
{
    if value.is_empty()
        || value.len() > 1 && value.starts_with('0')
        || !value.bytes().all(|byte| byte.is_ascii_digit())
    {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            format!("{label} is not a canonical unsigned integer: {value}"),
        ));
    }
    value.parse().map_err(|error| {
        io::Error::new(
            io::ErrorKind::InvalidInput,
            format!("{label} is outside its supported range: {error}"),
        )
    })
}
