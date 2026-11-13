//! Read-only census of exact retained Wasm under the production parser and
//! pinned SDK normalization. Successful parsing grants no runtime authority.

use sc_executor_common::runtime_blob::RuntimeBlob;
use serde_json::json;
use std::{collections::BTreeMap, error::Error, fs, path::Path};
use wasmparser::{Operator, Parser, Payload, Validator, WasmFeatures};

macro_rules! operator_families {
    ($(@$proposal:ident $op:ident $({ $($arg:ident: $argty:ty),* })? => $visit:ident)*) => {
        fn family(operator: &Operator<'_>) -> &'static str {
            match operator {
                $(Operator::$op $({ $($arg: _),* })? => stringify!($proposal),)*
                _ => "unknown-future-family",
            }
        }
    }
}
wasmparser::for_each_operator!(operator_families);

fn bodies(wasm: &[u8]) -> Result<Vec<Vec<u8>>, Box<dyn Error>> {
    let mut result = Vec::new();
    for payload in Parser::new(0).parse_all(wasm) {
        if let Payload::CodeSectionEntry(body) = payload? {
            result.push(wasm[body.range()].to_vec());
        }
    }
    Ok(result)
}

fn inspect(path: &Path) -> Result<serde_json::Value, Box<dyn Error>> {
    if !fs::metadata(path)?.is_file() || fs::metadata(path)?.len() > 32 * 1024 * 1024 {
        return Err("retained input is not a bounded regular file".into());
    }
    let raw = fs::read(path)?;
    let wasm = sp_maybe_compressed_blob::decompress(&raw, 32 * 1024 * 1024)
        .map_err(|error| format!("retained code decompression: {error:?}"))?;
    let mut families = BTreeMap::<&str, u64>::new();
    let mut function_bodies = 0u64;
    let mut function_imports = 0u64;
    let mut memory_imports = 0u64;
    for payload in Parser::new(0).parse_all(&wasm) {
        match payload? {
            Payload::ImportSection(section) => {
                for import in section {
                    match import?.ty {
                        wasmparser::TypeRef::Func(_) => function_imports += 1,
                        wasmparser::TypeRef::Memory(_) => memory_imports += 1,
                        _ => {}
                    }
                }
            }
            Payload::CodeSectionEntry(body) => {
                function_bodies += 1;
                for operator in body.get_operators_reader()? {
                    *families.entry(family(&operator?)).or_default() += 1;
                }
            }
            _ => {}
        }
    }
    // This is the effective intersection of the pinned RuntimeBlob parser and
    // node execution flags; the node's enabled Wasmtime defaults alone are not
    // sufficient when the preceding SDK parser refuses an instruction family.
    let features = WasmFeatures {
        mutable_global: true,
        saturating_float_to_int: false,
        sign_extension: false,
        reference_types: false,
        multi_value: false,
        bulk_memory: false,
        simd: false,
        relaxed_simd: false,
        threads: false,
        tail_call: false,
        floats: true,
        multi_memory: false,
        exceptions: false,
        memory64: false,
        extended_const: false,
        component_model: false,
        function_references: false,
        memory_control: false,
    };
    let feature_refusal = Validator::new_with_features(features)
        .validate_all(&wasm)
        .err()
        .map(|error| error.to_string());
    let original = bodies(&wasm)?;
    let normalization = (|| -> Result<bool, Box<dyn Error>> {
        let mut normalized = RuntimeBlob::uncompress_if_needed(&raw)?;
        normalized.convert_memory_import_into_export()?;
        normalized.setup_memory_according_to_heap_alloc_strategy(
            sc_executor::HeapAllocStrategy::Dynamic {
                maximum_pages: Some(1024),
            },
        )?;
        Ok(bodies(&normalized.serialize())? == original)
    })();
    let (normalized_bodies_preserved, normalization_refusal) = match normalization {
        Ok(preserved) => (Some(preserved), None),
        Err(error) => (None, Some(error.to_string())),
    };
    Ok(json!({
        "path": path,
        "input_bytes": raw.len(),
        "input_sha256": hex::encode(sp_core::hashing::sha2_256(&raw)),
        "expanded_bytes": wasm.len(),
        "expanded_sha256": hex::encode(sp_core::hashing::sha2_256(&wasm)),
        "function_imports": function_imports,
        "memory_imports": memory_imports,
        "function_bodies": function_bodies,
        "operator_families": families,
        "pinned_feature_refusal": feature_refusal,
        "normalized_bodies_preserved": normalized_bodies_preserved,
        "normalization_refusal": normalization_refusal,
        "runtime_admitted": false,
        "authority": "read-only-retained-byte-census"
    }))
}

fn main() -> Result<(), Box<dyn Error>> {
    let paths = std::env::args_os().skip(1).collect::<Vec<_>>();
    if paths.len() != 2 {
        return Err("two exact retained Wasm paths are required".into());
    }
    for path in paths {
        println!("{}", inspect(Path::new(&path))?);
    }
    Ok(())
}
