//! Executes reviewed Subtensor runtime artifacts without chain state.
//!
//! Only allocator, logging, and hashing host functions are installed. The
//! executor may link other imports so that normal runtime blobs instantiate,
//! but invoking any omitted import returns an execution error. In particular,
//! storage and offchain access cannot silently derive block-dependent metadata.

#![deny(unsafe_code)]

use frame_metadata::{RuntimeMetadataPrefixed, META_RESERVED};
use parity_scale_codec::Decode;
use sc_executor::WasmExecutor;
use sp_core::{
    blake2_256,
    hashing::sha2_256,
    traits::{CallContext, CodeExecutor, RuntimeCode, WrappedRuntimeCode},
    OpaqueMetadata,
};
use sp_state_machine::BasicExternalities;
use sp_version::RuntimeVersion;
use std::{error::Error, fmt, fs, path::Path};

/// The exact polkadot-sdk revision whose executor semantics this tool uses.
pub const POLKADOT_SDK_REVISION: &str = "cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a";

/// Release-bound runtime identity and exact artifact digests.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExpectedRuntimeArtifact {
    /// Runtime family reported by `Core_version`.
    pub spec_name: String,
    /// Runtime specification version reported by `Core_version`.
    pub spec_version: u32,
    /// Signed-transaction format version reported by `Core_version`.
    pub transaction_version: u32,
    /// State trie version reported by `Core_version` as `system_version`.
    pub state_version: u8,
    /// FRAME metadata version after complete decoding.
    pub metadata_version: u32,
    /// Exact length of the original, possibly compressed Wasm blob.
    pub code_size: u64,
    /// Exact length of the decoded opaque metadata bytes.
    pub metadata_size: u64,
    /// SHA-256 of the original, possibly compressed Wasm blob.
    pub code_sha256: [u8; 32],
    /// BLAKE2b-256 of the original, possibly compressed Wasm blob.
    pub code_blake2b_256: [u8; 32],
    /// SHA-256 of the decoded opaque metadata bytes.
    pub metadata_sha256: [u8; 32],
    /// BLAKE2b-256 of the decoded opaque metadata bytes.
    pub metadata_blake2b_256: [u8; 32],
}

/// Authenticated facts emitted after both runtime calls and complete decoding.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProbeReport {
    /// Exact length of the original input bytes.
    pub code_size: u64,
    /// Exact length of the decoded opaque metadata bytes.
    pub metadata_size: u64,
    /// SHA-256 of the original input bytes.
    pub code_sha256: [u8; 32],
    /// BLAKE2b-256 of the original input bytes.
    pub code_blake2b_256: [u8; 32],
    /// SHA-256 of the metadata returned by the runtime.
    pub metadata_sha256: [u8; 32],
    /// BLAKE2b-256 of the metadata returned by the runtime.
    pub metadata_blake2b_256: [u8; 32],
    /// Fully decoded FRAME metadata version.
    pub metadata_version: u32,
}

/// A verification or restricted-execution failure.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ProbeError(String);

impl ProbeError {
    fn new(message: impl Into<String>) -> Self {
        Self(message.into())
    }
}

impl fmt::Display for ProbeError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.0)
    }
}

impl Error for ProbeError {}

// Every other import is linked as a failing stub. Storage and offchain host
// functions are intentionally absent from this tuple.
type StatelessMetadataHostFunctions = (
    sp_io::allocator::HostFunctions,
    sp_io::logging::HostFunctions,
    sp_io::hashing::HostFunctions,
);

/// Parses one canonical 32-byte, `0x`-prefixed hexadecimal digest.
pub fn parse_digest(label: &str, value: &str) -> Result<[u8; 32], ProbeError> {
    if value.len() != 66 || !value.starts_with("0x") {
        return Err(ProbeError::new(format!(
            "{label} is not a 32-byte 0x-prefixed digest"
        )));
    }
    let decoded = hex::decode(&value[2..]).map_err(|error| {
        ProbeError::new(format!(
            "{label} is not a 32-byte 0x-prefixed digest: {error}"
        ))
    })?;
    decoded
        .try_into()
        .map_err(|_| ProbeError::new(format!("{label} is not a 32-byte digest")))
}

/// Formats one digest for manifests and diagnostics.
pub fn format_digest(digest: &[u8; 32]) -> String {
    format!("0x{}", hex::encode(digest))
}

/// Reads and verifies one exact runtime artifact from disk.
pub fn probe_file(
    wasm_path: &Path,
    expected: &ExpectedRuntimeArtifact,
) -> Result<ProbeReport, ProbeError> {
    let wasm = fs::read(wasm_path).map_err(|error| {
        ProbeError::new(format!(
            "read runtime artifact {}: {error}",
            wasm_path.display()
        ))
    })?;
    probe_blob(&wasm, expected)
}

/// Verifies and executes one exact in-memory runtime artifact.
///
/// The input digest is checked before Wasm parsing or execution. Both runtime
/// API results must be canonical SCALE values with no trailing bytes.
pub fn probe_blob(
    wasm: &[u8],
    expected: &ExpectedRuntimeArtifact,
) -> Result<ProbeReport, ProbeError> {
    if expected.spec_name.is_empty() {
        return Err(ProbeError::new("expected runtime spec name is empty"));
    }

    let code_size = u64::try_from(wasm.len())
        .map_err(|_| ProbeError::new("runtime code size exceeds uint64"))?;
    if code_size != expected.code_size {
        return Err(ProbeError::new(format!(
            "runtime code size {code_size}, want {}",
            expected.code_size
        )));
    }
    let code_sha256 = sha2_256(wasm);
    if code_sha256 != expected.code_sha256 {
        return Err(ProbeError::new(format!(
            "runtime code SHA-256 {}, want {}",
            format_digest(&code_sha256),
            format_digest(&expected.code_sha256)
        )));
    }
    let code_blake2b_256 = blake2_256(wasm);
    if code_blake2b_256 != expected.code_blake2b_256 {
        return Err(ProbeError::new(format!(
            "runtime code BLAKE2b-256 {}, want {}",
            format_digest(&code_blake2b_256),
            format_digest(&expected.code_blake2b_256)
        )));
    }

    let executor = WasmExecutor::<StatelessMetadataHostFunctions>::builder()
        .with_allow_missing_host_functions(true)
        .build();
    let wrapped_runtime_code = WrappedRuntimeCode(wasm.into());
    let runtime_code = RuntimeCode {
        code_fetcher: &wrapped_runtime_code,
        heap_pages: None,
        hash: code_blake2b_256.to_vec(),
    };

    let encoded_version = execute_runtime_api(&executor, &runtime_code, "Core_version")?;
    let runtime_version = decode_exact::<RuntimeVersion>("Core_version", &encoded_version)?;
    verify_runtime_version(&runtime_version, expected)?;

    let encoded_metadata = execute_runtime_api(&executor, &runtime_code, "Metadata_metadata")?;
    let opaque_metadata = decode_exact::<OpaqueMetadata>("Metadata_metadata", &encoded_metadata)?;
    let metadata_size = u64::try_from(opaque_metadata.len())
        .map_err(|_| ProbeError::new("runtime metadata size exceeds uint64"))?;
    let metadata_sha256 = sha2_256(opaque_metadata.as_slice());
    let metadata_blake2b_256 = blake2_256(opaque_metadata.as_slice());
    verify_metadata_bytes(
        metadata_size,
        &metadata_sha256,
        &metadata_blake2b_256,
        expected,
    )?;

    let metadata = decode_exact::<RuntimeMetadataPrefixed>(
        "decoded Metadata_metadata payload",
        opaque_metadata.as_slice(),
    )?;
    if metadata.0 != META_RESERVED {
        return Err(ProbeError::new(format!(
            "runtime metadata prefix 0x{:08x}, want 0x{META_RESERVED:08x}",
            metadata.0
        )));
    }
    let metadata_version = metadata.1.version();
    verify_metadata_version(metadata_version, expected)?;

    Ok(ProbeReport {
        code_size,
        metadata_size,
        code_sha256,
        code_blake2b_256,
        metadata_sha256,
        metadata_blake2b_256,
        metadata_version,
    })
}

fn execute_runtime_api(
    executor: &WasmExecutor<StatelessMetadataHostFunctions>,
    runtime_code: &RuntimeCode<'_>,
    method: &str,
) -> Result<Vec<u8>, ProbeError> {
    // Externalities are required by the executor interface, but no host
    // function capable of reading or writing them is installed above.
    let mut externalities = BasicExternalities::new_empty();
    executor
        .call(
            &mut externalities,
            runtime_code,
            method,
            &[],
            CallContext::Offchain,
        )
        .0
        .map_err(|error| ProbeError::new(format!("execute {method}: {error}")))
}

fn decode_exact<T: Decode>(label: &str, encoded: &[u8]) -> Result<T, ProbeError> {
    let mut input = encoded;
    let decoded = T::decode(&mut input)
        .map_err(|error| ProbeError::new(format!("decode {label}: {error}")))?;
    if !input.is_empty() {
        return Err(ProbeError::new(format!(
            "decode {label}: {} trailing bytes",
            input.len()
        )));
    }
    Ok(decoded)
}

fn verify_runtime_version(
    observed: &RuntimeVersion,
    expected: &ExpectedRuntimeArtifact,
) -> Result<(), ProbeError> {
    if observed.spec_name.as_ref() != expected.spec_name {
        return Err(ProbeError::new(format!(
            "runtime spec name {}, want {}",
            observed.spec_name, expected.spec_name
        )));
    }
    if observed.spec_version != expected.spec_version {
        return Err(ProbeError::new(format!(
            "runtime spec version {}, want {}",
            observed.spec_version, expected.spec_version
        )));
    }
    if observed.transaction_version != expected.transaction_version {
        return Err(ProbeError::new(format!(
            "runtime transaction version {}, want {}",
            observed.transaction_version, expected.transaction_version
        )));
    }
    if observed.system_version != expected.state_version {
        return Err(ProbeError::new(format!(
            "runtime state version {}, want {}",
            observed.system_version, expected.state_version
        )));
    }
    Ok(())
}

fn verify_metadata_bytes(
    observed_size: u64,
    observed_sha256: &[u8; 32],
    observed_blake2b_256: &[u8; 32],
    expected: &ExpectedRuntimeArtifact,
) -> Result<(), ProbeError> {
    if observed_size != expected.metadata_size {
        return Err(ProbeError::new(format!(
            "runtime metadata size {observed_size}, want {}",
            expected.metadata_size
        )));
    }
    if observed_sha256 != &expected.metadata_sha256 {
        return Err(ProbeError::new(format!(
            "runtime metadata SHA-256 {}, want {}",
            format_digest(observed_sha256),
            format_digest(&expected.metadata_sha256)
        )));
    }
    if observed_blake2b_256 != &expected.metadata_blake2b_256 {
        return Err(ProbeError::new(format!(
            "runtime metadata BLAKE2b-256 {}, want {}",
            format_digest(observed_blake2b_256),
            format_digest(&expected.metadata_blake2b_256)
        )));
    }
    Ok(())
}

fn verify_metadata_version(
    observed_version: u32,
    expected: &ExpectedRuntimeArtifact,
) -> Result<(), ProbeError> {
    if observed_version != expected.metadata_version {
        return Err(ProbeError::new(format!(
            "runtime metadata version {observed_version}, want {}",
            expected.metadata_version
        )));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::borrow::Cow;

    fn expected(code: &[u8]) -> ExpectedRuntimeArtifact {
        ExpectedRuntimeArtifact {
            spec_name: "node-subtensor".to_owned(),
            spec_version: 453,
            transaction_version: 1,
            state_version: 1,
            metadata_version: 14,
            code_size: code.len() as u64,
            metadata_size: 123,
            code_sha256: sha2_256(code),
            code_blake2b_256: blake2_256(code),
            metadata_sha256: [2; 32],
            metadata_blake2b_256: [3; 32],
        }
    }

    #[test]
    fn digest_parser_rejects_noncanonical_values() {
        let invalid_values = [
            "",
            "00",
            "0X0000000000000000000000000000000000000000000000000000000000000000",
            "0x00",
            "0xgg00000000000000000000000000000000000000000000000000000000000000",
            "0x000000000000000000000000000000000000000000000000000000000000000000",
        ];
        for invalid_value in invalid_values {
            assert!(parse_digest("test digest", invalid_value).is_err());
        }
    }

    #[test]
    fn digest_parser_round_trips_exact_bytes() {
        let digest = [0xab; 32];
        let formatted = format_digest(&digest);
        assert_eq!(parse_digest("test digest", &formatted), Ok(digest));
    }

    #[test]
    fn code_blake2b_256_mismatch_stops_before_wasm_execution() {
        let invalid_wasm = b"not wasm";
        let mut expected = expected(invalid_wasm);
        expected.code_blake2b_256 = [0; 32];
        let error = probe_blob(invalid_wasm, &expected).unwrap_err();
        assert!(error.to_string().starts_with("runtime code BLAKE2b-256 "));
    }

    #[test]
    fn code_size_mismatch_stops_before_wasm_execution() {
        let invalid_wasm = b"not wasm";
        let mut expected = expected(invalid_wasm);
        expected.code_size += 1;
        let error = probe_blob(invalid_wasm, &expected).unwrap_err();
        assert_eq!(error.to_string(), "runtime code size 8, want 9");
    }

    #[test]
    fn code_sha256_mismatch_stops_before_wasm_execution() {
        let invalid_wasm = b"not wasm";
        let mut expected = expected(invalid_wasm);
        expected.code_sha256 = [0; 32];
        let error = probe_blob(invalid_wasm, &expected).unwrap_err();
        assert!(error.to_string().starts_with("runtime code SHA-256 "));
    }

    #[test]
    fn exact_decoder_rejects_trailing_bytes() {
        let error = decode_exact::<u8>("test value", &[7, 8]).unwrap_err();
        assert_eq!(error.to_string(), "decode test value: 1 trailing bytes");
    }

    #[test]
    fn metadata_size_mismatch_is_rejected() {
        let expected = expected(b"wasm");
        let error = verify_metadata_bytes(
            expected.metadata_size + 1,
            &expected.metadata_sha256,
            &expected.metadata_blake2b_256,
            &expected,
        )
        .unwrap_err();
        assert_eq!(error.to_string(), "runtime metadata size 124, want 123");
    }

    #[test]
    fn metadata_sha256_mismatch_is_rejected() {
        let expected = expected(b"wasm");
        let error = verify_metadata_bytes(
            expected.metadata_size,
            &[4; 32],
            &expected.metadata_blake2b_256,
            &expected,
        )
        .unwrap_err();
        assert!(error.to_string().starts_with("runtime metadata SHA-256 "));
    }

    #[test]
    fn metadata_blake2b_256_mismatch_is_rejected() {
        let expected = expected(b"wasm");
        let error = verify_metadata_bytes(
            expected.metadata_size,
            &expected.metadata_sha256,
            &[4; 32],
            &expected,
        )
        .unwrap_err();
        assert!(error
            .to_string()
            .starts_with("runtime metadata BLAKE2b-256 "));
    }

    #[test]
    fn metadata_version_mismatch_is_rejected() {
        let expected = expected(b"wasm");
        let error = verify_metadata_version(15, &expected).unwrap_err();
        assert_eq!(error.to_string(), "runtime metadata version 15, want 14");
    }

    #[test]
    fn full_release_version_tuple_is_required() {
        let expected = expected(b"wasm");
        let mut observed = RuntimeVersion {
            spec_name: Cow::Borrowed("node-subtensor"),
            spec_version: 453,
            transaction_version: 1,
            system_version: 1,
            ..RuntimeVersion::default()
        };
        assert_eq!(verify_runtime_version(&observed, &expected), Ok(()));

        observed.system_version = 2;
        let error = verify_runtime_version(&observed, &expected).unwrap_err();
        assert_eq!(error.to_string(), "runtime state version 2, want 1");
    }

    #[test]
    fn invoked_stateful_host_function_is_a_hard_failure() {
        let wasm = wat::parse_str(
            r#"
                (module
                    (import "env" "ext_storage_get_version_1"
                        (func $storage_get (param i64) (result i64)))
                    (memory (export "memory") 1)
                    (global (export "__heap_base") i32 (i32.const 16))
                    (func (export "Core_version") (param i32 i32) (result i64)
                        (drop (call $storage_get (i64.const 0)))
                        (i64.const 0)))
            "#,
        )
        .expect("test Wasm is valid");
        let code_hash = blake2_256(&wasm);
        let wrapped_runtime_code = WrappedRuntimeCode(wasm.into());
        let runtime_code = RuntimeCode {
            code_fetcher: &wrapped_runtime_code,
            heap_pages: None,
            hash: code_hash.to_vec(),
        };
        let executor = WasmExecutor::<StatelessMetadataHostFunctions>::builder()
            .with_allow_missing_host_functions(true)
            .build();

        let error = execute_runtime_api(&executor, &runtime_code, "Core_version").unwrap_err();
        assert!(
            error
                .to_string()
                .contains("missing function env:ext_storage_get_version_1"),
            "unexpected execution error: {error}"
        );
    }
}
