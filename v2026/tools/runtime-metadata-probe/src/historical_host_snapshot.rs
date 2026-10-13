//! Admit a selected original direct allocator boundary, without adding a Wasm
//! instruction or treating the resulting memory record as a storage mutation.
use super::observer::ObservationProfile;
use crate::ProbeError;
use wasmparser::{Operator, Parser, Payload, Type, TypeRef, ValType};

const MAXIMUM_ENTRIES: usize = 65536;
const MALLOC: &str = "ext_allocator_malloc_version_1";
const FREE: &str = "ext_allocator_free_version_1";

fn error(value: impl std::fmt::Display) -> ProbeError {
    ProbeError::new(format!("original host snapshot: {value}"))
}

pub(super) fn validate(profile: &ObservationProfile, wasm: &[u8]) -> Result<(), ProbeError> {
    let rules: Vec<_> = profile
        .rules
        .iter()
        .filter(|rule| rule.host_snapshot.is_some())
        .collect();
    if rules.is_empty() {
        return Ok(());
    }
    if profile.schema != "urnetwork-original-wasm-native-observation-v2"
        || rules.iter().any(|rule| {
            rule.purpose != "native-epoch"
                || rule.memory.is_empty()
                || !matches!(rule.host_snapshot.as_deref(), Some(MALLOC | FREE))
        })
    {
        return Err(error(
            "requires native epoch memory and an exact allocator name",
        ));
    }
    let mut types = Vec::new();
    let mut imports = Vec::new();
    let mut index = 0u32;
    let mut checked = vec![false; rules.len()];
    for payload in Parser::new(0).parse_all(wasm) {
        match payload.map_err(error)? {
            Payload::TypeSection(section) => {
                if section.count() as usize > MAXIMUM_ENTRIES {
                    return Err(error("type census exceeds bound"));
                }
                for ty in section {
                    if types.len() >= MAXIMUM_ENTRIES {
                        return Err(error("type census exceeds bound"));
                    }
                    let Type::Func(ty) = ty.map_err(error)?;
                    // Retain only the two permitted ABI shapes, not type bodies.
                    types.push(if ty.params() == [ValType::I32] {
                        if ty.results() == [ValType::I32] {
                            1u8
                        } else if ty.results().is_empty() {
                            2u8
                        } else {
                            0u8
                        }
                    } else {
                        0u8
                    });
                }
            }
            Payload::ImportSection(section) => {
                for import in section {
                    let import = import.map_err(error)?;
                    if let TypeRef::Func(ty) = import.ty {
                        if imports.len() >= MAXIMUM_ENTRIES {
                            return Err(error("import census exceeds bound"));
                        }
                        let valid = import.module == "env"
                            && matches!(
                                (import.name, types.get(ty as usize)),
                                (MALLOC, Some(1)) | (FREE, Some(2))
                            );
                        imports.push(valid.then_some(import.name));
                    }
                }
                index = u32::try_from(imports.len()).map_err(error)?;
            }
            Payload::CodeSectionEntry(body) => {
                if index as usize >= MAXIMUM_ENTRIES {
                    return Err(error("function census exceeds bound"));
                }
                let selected: Vec<_> = rules
                    .iter()
                    .enumerate()
                    .filter(|(_, rule)| rule.function_index == index)
                    .collect();
                if !selected.is_empty() {
                    let base = body.range().start;
                    let mut reader = body.get_operators_reader().map_err(error)?;
                    while !reader.eof() {
                        let start = reader.original_position() - base;
                        let operator = reader.read().map_err(error)?;
                        let end = reader.original_position() - base;
                        for (rule_index, rule) in &selected {
                            if start == rule.offset_start as usize
                                && end == rule.offset_end as usize
                            {
                                let Operator::Call { function_index } = operator else {
                                    return Err(error("selected instruction is not a direct call"));
                                };
                                if imports.get(function_index as usize).copied().flatten()
                                    != rule.host_snapshot.as_deref()
                                {
                                    return Err(error(
                                        "selected callee module, name or ABI differs",
                                    ));
                                }
                                checked[*rule_index] = true;
                            }
                        }
                    }
                }
                index = index
                    .checked_add(1)
                    .ok_or_else(|| error("function overflow"))?;
            }
            _ => {}
        }
    }
    if checked.iter().any(|value| !value) {
        return Err(error("selected range is not exactly one original call"));
    }
    Ok(())
}
