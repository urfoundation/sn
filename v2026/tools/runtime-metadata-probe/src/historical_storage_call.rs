//! A reviewed semantic call selects one exact original storage host path.
//! Match it before reading memory: sibling hosts can have different frames.
use super::observer::{Frame, HookRule, ObservationProfile};
use crate::ProbeError;
use serde::{Deserialize, Serialize};
use sp_core::hashing::sha2_256;
use std::collections::BTreeMap;
use wasmparser::{Operator, Parser, Payload, Type, TypeRef, ValType};

const MAXIMUM_ENTRIES: usize = 65536;

#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Callsite {
    pub function_index: u32,
    pub function_body_sha256: [u8; 32],
    pub offset_start: u32,
    pub offset_end: u32,
}
#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct StorageCall {
    pub operation: String,
    pub path: Vec<Callsite>,
}
fn error(value: impl std::fmt::Display) -> ProbeError {
    ProbeError::new(format!("original storage call path: {value}"))
}

pub(super) fn matches(rule: &HookRule, stack: &[Frame]) -> bool {
    if let Some(call) = &rule.storage_call {
        return !call.path.is_empty()
            && stack.len() >= call.path.len()
            && call.path.iter().zip(stack).all(|(selected, actual)| {
                selected.function_index == actual.function_index
                    && actual.function_offset >= selected.offset_start
                    && actual.function_offset < selected.offset_end
            });
    }
    stack.iter().enumerate().any(|(index, frame)| {
        (rule.host_snapshot.is_none() || index == 0)
            && rule.function_index == frame.function_index
            && frame.function_offset >= rule.offset_start
            && frame.function_offset < rule.offset_end
    })
}

pub(super) fn overlaps(left: &HookRule, right: &HookRule) -> bool {
    let (Some(left), Some(right)) = (&left.storage_call, &right.storage_call) else {
        return true;
    };
    left.path.iter().zip(&right.path).all(|(a, b)| {
        a.function_index == b.function_index
            && a.offset_start < b.offset_end
            && b.offset_start < a.offset_end
    })
}

pub(super) fn validate(profile: &ObservationProfile, wasm: &[u8]) -> Result<(), ProbeError> {
    let mut selected = Vec::new();
    let mut by_function = BTreeMap::<u32, Vec<usize>>::new();
    if profile.rules.len() > 32 {
        return Err(error("rule census exceeds bound"));
    }
    for rule in &profile.rules {
        let Some(call) = &rule.storage_call else {
            continue;
        };
        if profile.schema != "urnetwork-original-wasm-native-observation-v2"
            || rule.host_snapshot.is_some()
            || call.path.is_empty()
            || call.path.len() > 8
            || !matches!(call.operation.as_str(), "get" | "set" | "append")
        {
            return Err(error("scope, operation or path bound differs"));
        }
        let last = call.path.last().expect("nonempty bounded path");
        if last.function_index != rule.function_index
            || last.function_body_sha256 != rule.function_body_sha256
            || last.offset_start != rule.offset_start
            || last.offset_end != rule.offset_end
        {
            return Err(error("semantic ancestor differs from selected path"));
        }
        for (index, frame) in call.path.iter().enumerate() {
            if frame.function_body_sha256 == [0; 32] || frame.offset_start >= frame.offset_end {
                return Err(error("original frame identity or range differs"));
            }
            let id = selected.len();
            selected.push((
                frame,
                if index == 0 {
                    None
                } else {
                    Some(call.path[index - 1].function_index)
                },
                call.operation.as_str(),
            ));
            by_function
                .entry(frame.function_index)
                .or_default()
                .push(id);
        }
    }
    if selected.is_empty() {
        return Ok(());
    }
    let mut types = Vec::new();
    let mut imports = Vec::new();
    let mut index = 0u32;
    let mut checked = vec![false; selected.len()];
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
                    types.push(
                        if ty.params() == [ValType::I64] && ty.results() == [ValType::I64] {
                            1u8
                        } else if ty.params() == [ValType::I64, ValType::I64]
                            && ty.results().is_empty()
                        {
                            2u8
                        } else {
                            0u8
                        },
                    );
                }
            }
            Payload::ImportSection(section) => {
                for import in section {
                    let import = import.map_err(error)?;
                    if let TypeRef::Func(ty) = import.ty {
                        if imports.len() >= MAXIMUM_ENTRIES {
                            return Err(error("import census exceeds bound"));
                        }
                        let operation = if import.module == "env" {
                            match (import.name, types.get(ty as usize)) {
                                ("ext_storage_get_version_1", Some(1)) => Some("get"),
                                ("ext_storage_set_version_1", Some(2)) => Some("set"),
                                ("ext_storage_append_version_1", Some(2)) => Some("append"),
                                _ => None,
                            }
                        } else {
                            None
                        };
                        imports.push(operation);
                    }
                }
                index = u32::try_from(imports.len()).map_err(error)?;
            }
            Payload::CodeSectionEntry(body) => {
                if index as usize >= MAXIMUM_ENTRIES {
                    return Err(error("function census exceeds bound"));
                }
                if let Some(ids) = by_function.get(&index) {
                    let digest = sha2_256(&wasm[body.range()]);
                    let mut offsets = BTreeMap::<(usize, usize), Vec<usize>>::new();
                    for id in ids {
                        let (frame, _, _) = selected[*id];
                        if frame.function_body_sha256 != digest
                            || frame.offset_end as usize > body.range().len()
                        {
                            return Err(error("original body hash or frame range differs"));
                        }
                        offsets
                            .entry((frame.offset_start as usize, frame.offset_end as usize))
                            .or_default()
                            .push(*id);
                    }
                    let base = body.range().start;
                    let mut reader = body.get_operators_reader().map_err(error)?;
                    while !reader.eof() {
                        let start = reader.original_position() - base;
                        let operator = reader.read().map_err(error)?;
                        let end = reader.original_position() - base;
                        if let Some(ids) = offsets.get(&(start, end)) {
                            let Operator::Call { function_index } = operator else {
                                return Err(error("selected frame is not exactly one direct call"));
                            };
                            for id in ids {
                                let (_, callee, operation) = selected[*id];
                                if let Some(callee) = callee {
                                    if function_index != callee {
                                        return Err(error(
                                            "original ancestor direct callee differs",
                                        ));
                                    }
                                } else if imports.get(function_index as usize).copied().flatten()
                                    != Some(operation)
                                {
                                    return Err(error(
                                        "original host module, operation or ABI differs",
                                    ));
                                }
                                checked[*id] = true;
                            }
                        }
                    }
                }
                index = index
                    .checked_add(1)
                    .ok_or_else(|| error("function index overflow"))?;
            }
            _ => {}
        }
    }
    if checked.iter().any(|value| !value) {
        return Err(error(
            "selected call path contains absent instruction boundaries",
        ));
    }
    Ok(())
}
