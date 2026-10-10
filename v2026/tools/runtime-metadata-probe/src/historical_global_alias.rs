//! Read-only host visibility for explicitly reviewed original i32 globals.
//! Only export entries are appended. Runtime instructions, data, global state,
//! imports and original exports remain exact; no caller-frame layout is inferred.
use crate::ProbeError;
use serde::{Deserialize, Serialize};
use std::{borrow::Cow, collections::BTreeSet};
use wasmparser::{ExternalKind, Parser, Payload, TypeRef, ValType};

const MAXIMUM_SECTIONS: usize = 4096;

#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct OriginalGlobal {
    pub global_index: u32,
    pub export_name: String,
}

pub(crate) fn name(index: u32) -> String {
    format!("__urnetwork_observe_global_{index}")
}

fn error(value: impl std::fmt::Display) -> ProbeError {
    ProbeError::new(format!("original global alias: {value}"))
}

fn put_u32(mut value: u32, out: &mut Vec<u8>) {
    loop {
        let next = (value & 127) as u8;
        value >>= 7;
        out.push(if value == 0 { next } else { next | 128 });
        if value == 0 {
            break;
        }
    }
}

fn read_u32(raw: &[u8], position: &mut usize) -> Result<u32, ProbeError> {
    let mut value = 0u32;
    for shift in (0..35).step_by(7) {
        let byte = *raw
            .get(*position)
            .ok_or_else(|| error("truncated section length"))?;
        *position += 1;
        if shift == 28 && byte > 15 {
            return Err(error("section length overflow"));
        }
        value |= u32::from(byte & 127) << shift;
        if byte & 128 == 0 {
            return Ok(value);
        }
    }
    Err(error("unterminated section length"))
}

struct Section {
    kind: u8,
    start: usize,
    payload: usize,
    end: usize,
}
fn sections(wasm: &[u8]) -> Result<Vec<Section>, ProbeError> {
    if wasm.get(..8) != Some(b"\0asm\x01\0\0\0") {
        return Err(error("not original core Wasm"));
    }
    let mut position = 8;
    let mut result = Vec::new();
    while position < wasm.len() {
        if result.len() >= MAXIMUM_SECTIONS {
            return Err(error("section census exceeds bound"));
        }
        let start = position;
        let kind = wasm[position];
        position += 1;
        let length = read_u32(wasm, &mut position)? as usize;
        let end = position
            .checked_add(length)
            .filter(|end| *end <= wasm.len())
            .ok_or_else(|| error("section exceeds original bytes"))?;
        result.push(Section {
            kind,
            start,
            payload: position,
            end,
        });
        position = end;
    }
    Ok(result)
}

/// Empty declarations preserve the original bytes, including compression at
/// the caller. Nonempty aliases bind defined originals, never imported globals.
pub(crate) fn expose<'a>(
    wasm: &'a [u8],
    aliases: &[OriginalGlobal],
) -> Result<Cow<'a, [u8]>, ProbeError> {
    if aliases.is_empty() {
        return Ok(Cow::Borrowed(wasm));
    }
    if aliases.len() > 4
        || aliases.iter().enumerate().any(|(index, alias)| {
            alias.export_name != name(alias.global_index)
                || index != 0 && aliases[index - 1].global_index >= alias.global_index
        })
    {
        return Err(error(
            "aliases must be bounded, ordered and canonically named",
        ));
    }
    let original = sections(wasm)?;
    let mut globals = Vec::new();
    let mut exports = BTreeSet::new();
    for payload in Parser::new(0).parse_all(wasm) {
        if globals.len() > 65536 || exports.len() > 65536 {
            return Err(error("global or export census exceeds bound"));
        }
        match payload.map_err(error)? {
            Payload::ImportSection(section) => {
                for import in section {
                    if let TypeRef::Global(global) = import.map_err(error)?.ty {
                        if globals.len() >= 65536 {
                            return Err(error("global census exceeds bound"));
                        }
                        globals.push((true, global.content_type));
                    }
                }
            }
            Payload::GlobalSection(section) => {
                for global in section {
                    if globals.len() >= 65536 {
                        return Err(error("global census exceeds bound"));
                    }
                    globals.push((false, global.map_err(error)?.ty.content_type));
                }
            }
            Payload::ExportSection(section) => {
                for export in section {
                    let export = export.map_err(error)?;
                    if exports.len() >= 65536 || export.name.len() > 4096 {
                        return Err(error("export census exceeds bound"));
                    }
                    exports.insert(export.name.to_owned());
                }
            }
            _ => {}
        }
    }
    for alias in aliases {
        if globals.get(alias.global_index as usize) != Some(&(false, ValType::I32))
            || exports.contains(&alias.export_name)
        {
            return Err(error(
                "index is not defined i32 or reserved export collides",
            ));
        }
    }
    let mut result = wasm[..8].to_vec();
    let mut emitted = false;
    let append = |payload: &[u8], result: &mut Vec<u8>| -> Result<(), ProbeError> {
        let mut cursor = 0;
        let count = if payload.is_empty() {
            0
        } else {
            read_u32(payload, &mut cursor)?
        };
        let mut updated = Vec::new();
        put_u32(
            count
                .checked_add(aliases.len() as u32)
                .ok_or_else(|| error("export count overflow"))?,
            &mut updated,
        );
        updated.extend_from_slice(&payload[cursor..]);
        for alias in aliases {
            put_u32(alias.export_name.len() as u32, &mut updated);
            updated.extend_from_slice(alias.export_name.as_bytes());
            updated.push(3); // Existing global export, no new global or instruction.
            put_u32(alias.global_index, &mut updated);
        }
        result.push(7);
        put_u32(u32::try_from(updated.len()).map_err(error)?, result);
        result.extend_from_slice(&updated);
        Ok(())
    };
    for section in &original {
        if section.kind == 7 {
            if emitted {
                return Err(error("multiple export sections"));
            }
            append(&wasm[section.payload..section.end], &mut result)?;
            emitted = true;
        } else {
            if !emitted && section.kind > 7 {
                append(&[], &mut result)?;
                emitted = true;
            }
            result.extend_from_slice(&wasm[section.start..section.end]);
        }
    }
    if !emitted {
        append(&[], &mut result)?;
    }
    let transformed = sections(&result)?;
    if !original
        .iter()
        .filter(|section| section.kind != 7)
        .map(|section| &wasm[section.start..section.end])
        .eq(transformed
            .iter()
            .filter(|section| section.kind != 7)
            .map(|section| &result[section.start..section.end]))
    {
        return Err(error("non-export original section changed"));
    }
    let read_exports = |raw: &[u8]| -> Result<Vec<(String, ExternalKind, u32)>, ProbeError> {
        let mut result = Vec::new();
        for payload in Parser::new(0).parse_all(raw) {
            if let Payload::ExportSection(section) = payload.map_err(error)? {
                for export in section {
                    let export = export.map_err(error)?;
                    result.push((export.name.to_owned(), export.kind, export.index));
                }
            }
        }
        Ok(result)
    };
    let mut expected = read_exports(wasm)?;
    expected.extend(aliases.iter().map(|alias| {
        (
            alias.export_name.clone(),
            ExternalKind::Global,
            alias.global_index,
        )
    }));
    if read_exports(&result)? != expected {
        return Err(error("original export order or exact aliases changed"));
    }
    Ok(Cow::Owned(result))
}

#[cfg(test)]
mod tests {
    use super::*;
    fn alias(index: u32) -> OriginalGlobal {
        OriginalGlobal {
            global_index: index,
            export_name: name(index),
        }
    }

    #[test]
    fn original_global_alias_preserves_every_nonexport_byte_and_original_export() {
        let code = wat::parse_str(r#"(module (memory (export "memory") 1)
            (global $private (mut i32) (i32.const 1048576))
            (global (export "existing") i32 (i32.const 32))
            (data (i32.const 0) "original-data") (func (export "entry") (drop (global.get $private))))"#).unwrap();
        let result = expose(&code, &[alias(0)]).unwrap();
        assert_ne!(result.as_ref(), code);
        wasmparser::Validator::new().validate_all(&result).unwrap();
        assert!(sections(&code)
            .unwrap()
            .iter()
            .filter(|s| s.kind != 7)
            .map(|s| &code[s.start..s.end])
            .eq(sections(&result)
                .unwrap()
                .iter()
                .filter(|s| s.kind != 7)
                .map(|s| &result[s.start..s.end])));
        assert_ne!(sp_core::blake2_256(&result), sp_core::blake2_256(&code));
        assert!(matches!(expose(&code, &[]).unwrap(), Cow::Borrowed(_)));
    }

    #[test]
    fn original_global_alias_refuses_imports_wrong_types_missing_and_colliding_exports() {
        let code = wat::parse_str(
            r#"(module (import "env" "original" (global i32))
            (global i64 (i64.const 0)) (global i32 (i32.const 0))
            (export "__urnetwork_observe_global_2" (global 2)))"#,
        )
        .unwrap();
        for index in [0, 1, 2, 3] {
            assert!(expose(&code, &[alias(index)]).is_err(), "index {index}");
        }
        let code = wat::parse_str("(module (global i32 (i32.const 0)))").unwrap();
        for aliases in [
            vec![alias(0), alias(0)],
            vec![alias(0); 5],
            vec![OriginalGlobal {
                global_index: 0,
                export_name: "other".to_owned(),
            }],
        ] {
            assert!(expose(&code, &aliases).is_err());
        }
        let result = expose(&code, &[alias(0)]).unwrap();
        wasmparser::Validator::new().validate_all(&result).unwrap();
    }
    #[test]
    fn original_global_alias_sections_stop_at_exact_retention_bound() {
        let mut code = b"\0asm\x01\0\0\0".to_vec();
        for _ in 0..MAXIMUM_SECTIONS {
            code.extend_from_slice(&[0, 1, 0]);
        }
        assert_eq!(sections(&code).unwrap().len(), MAXIMUM_SECTIONS);
        code.extend_from_slice(&[0, 1, 0]);
        assert!(sections(&code)
            .err()
            .unwrap()
            .to_string()
            .contains("section census"));
        assert!(expose(&code, &[alias(0)])
            .unwrap_err()
            .to_string()
            .contains("section census"));
    }
}
