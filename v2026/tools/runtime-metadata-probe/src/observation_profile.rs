//! Compile an explicit callsite/layout review against original Wasm bytes.
//! Inspection and assembly execute no Wasm and confer no semantic or signing
//! authority. Names are navigation aids; every rule binds bytes and calls.

use crate::historical::observer::{validate_profile, MemoryCapture, ObservationProfile};
use crate::ProbeError;
use serde::{Deserialize, Serialize};
use sp_core::hashing::sha2_256;
use std::collections::{BTreeMap, BTreeSet};
use wasmparser::{
    ExternalKind, Name, NameSectionReader, Operator, Parser, Payload, TypeRef, ValType,
};

pub const MAXIMUM_CODE_BYTES: usize = 8 * 1024 * 1024;
pub const MAXIMUM_PROPOSAL_BYTES: usize = 64 * 1024;
pub const MAXIMUM_REPORT_BYTES: usize = 32 * 1024 * 1024;
const MAXIMUM_EXPANDED_BYTES: usize = 32 * 1024 * 1024;
const MAXIMUM_FUNCTIONS: usize = 65536;
const MAXIMUM_SELECTED: usize = 32;
const MAXIMUM_LISTED_INSTRUCTIONS: usize = 262144;
const MAXIMUM_RETAINED_LISTING_BYTES: usize = 16 * 1024 * 1024;

/// Exact direct/indirect call operands at a function-relative instruction.
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct Callsite {
    pub offset: u32,
    pub end: u32,
    pub target: CallTarget,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(tag = "kind", deny_unknown_fields)]
pub enum CallTarget {
    #[serde(rename = "direct")]
    Direct { function_index: u32 },
    #[serde(rename = "indirect")]
    Indirect { type_index: u32, table_index: u32 },
    #[serde(rename = "return_direct")]
    ReturnDirect { function_index: u32 },
    #[serde(rename = "return_indirect")]
    ReturnIndirect { type_index: u32, table_index: u32 },
}

#[derive(Serialize)]
pub struct Instruction {
    pub offset: u32,
    pub end: u32,
    pub bytes_hex: String,
    pub operator: String,
}

#[derive(Serialize)]
pub struct Function {
    pub function_index: u32,
    pub function_body_sha256: [u8; 32],
    pub body_bytes: u32,
    pub name: Option<String>,
    pub exports: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub instructions: Option<Vec<Instruction>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub calls: Option<Vec<Callsite>>,
}

#[derive(Serialize)]
pub struct Global {
    pub index: u32,
    pub value_type: String,
    pub mutable: bool,
    pub imported: bool,
    pub initialization: Vec<String>,
    pub exports: Vec<String>,
}

#[derive(Serialize)]
pub struct ImportedFunction {
    pub index: u32,
    pub module: String,
    pub name: String,
    pub type_index: u32,
}

#[derive(Serialize)]
pub struct Inspection {
    pub schema: String,
    pub authority: String,
    pub original_code_sha256: [u8; 32],
    pub expanded_code_sha256: [u8; 32],
    pub original_bytes: usize,
    pub expanded_bytes: usize,
    pub imported_functions: Vec<ImportedFunction>,
    pub functions: Vec<Function>,
    pub globals: Vec<Global>,
}

/// Reviewer-supplied semantic labels and memory recipes. Assembly checks their
/// structural applicability, not whether those labels mean real economic acts.
#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct ProfileProposal {
    pub schema: String,
    pub profile: ObservationProfile,
    pub reviewed_calls: Vec<ReviewedCalls>,
}

#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct ReviewedCalls {
    pub function_index: u32,
    pub offset_start: u32,
    pub offset_end: u32,
    pub calls: Vec<Callsite>,
}

fn retain_bytes(remaining: &mut usize, bytes: usize) -> Result<(), ProbeError> {
    *remaining = remaining
        .checked_sub(bytes)
        .ok_or_else(|| ProbeError::new("original listing exceeds retained byte bound"))?;
    Ok(())
}

fn bounded_debug(value: &impl std::fmt::Debug) -> Result<String, ProbeError> {
    struct Limited(String);
    impl std::fmt::Write for Limited {
        fn write_str(&mut self, value: &str) -> std::fmt::Result {
            if self.0.len().saturating_add(value.len()) > 4096 {
                return Err(std::fmt::Error);
            }
            self.0.push_str(value);
            Ok(())
        }
    }
    use std::fmt::Write;
    let mut output = Limited(String::new());
    write!(&mut output, "{value:?}")
        .map_err(|_| ProbeError::new("original operator diagnostic exceeds bound"))?;
    Ok(output.0)
}

fn parse_error(error: impl std::fmt::Display) -> ProbeError {
    ProbeError::new(format!("original profile Wasm: {error}"))
}

fn u32_offset(value: usize) -> Result<u32, ProbeError> {
    u32::try_from(value).map_err(|_| ProbeError::new("original instruction offset exceeds u32"))
}

/// The original digest is checked before decompression or parsing. Only selected
/// functions retain instruction listings, keeping a complete census bounded.
pub fn inspect_original(
    code: &[u8],
    expected_sha256: [u8; 32],
    selected: &[u32],
) -> Result<Inspection, ProbeError> {
    inspect_with_limits(
        code,
        expected_sha256,
        selected,
        MAXIMUM_LISTED_INSTRUCTIONS,
        MAXIMUM_RETAINED_LISTING_BYTES,
    )
}

// Limits apply before retained allocations, not only after JSON serialization.
fn inspect_with_limits(
    code: &[u8],
    expected_sha256: [u8; 32],
    selected: &[u32],
    mut instructions_left: usize,
    mut listing_bytes_left: usize,
) -> Result<Inspection, ProbeError> {
    if code.is_empty() || code.len() > MAXIMUM_CODE_BYTES || sha2_256(code) != expected_sha256 {
        return Err(ProbeError::new(
            "original profile code digest or size differs",
        ));
    }
    let selected_set: BTreeSet<_> = selected.iter().copied().collect();
    if selected.len() > MAXIMUM_SELECTED || selected_set.len() != selected.len() {
        return Err(ProbeError::new(
            "original profile selection duplicates or exceeds bound",
        ));
    }
    let wasm =
        sp_maybe_compressed_blob::decompress(code, MAXIMUM_EXPANDED_BYTES).map_err(parse_error)?;
    wasmparser::Validator::new()
        .validate_all(&wasm)
        .map_err(parse_error)?;
    let mut imported_functions = Vec::new();
    let mut functions = Vec::new();
    let mut globals = Vec::new();
    let mut names = BTreeMap::new();
    let mut function_exports: BTreeMap<u32, Vec<String>> = BTreeMap::new();
    let mut global_exports: BTreeMap<u32, Vec<String>> = BTreeMap::new();
    let mut exports_count = 0usize;
    let mut function_index = 0u32;
    for payload in Parser::new(0).parse_all(&wasm) {
        match payload.map_err(parse_error)? {
            Payload::ImportSection(section) => {
                for import in section {
                    let import = import.map_err(parse_error)?;
                    retain_bytes(
                        &mut listing_bytes_left,
                        import.module.len() + import.name.len() + 128,
                    )?;
                    if imported_functions.len() + globals.len() >= MAXIMUM_FUNCTIONS {
                        return Err(ProbeError::new("original import census exceeds bound"));
                    }
                    match import.ty {
                        TypeRef::Func(type_index) => {
                            imported_functions.push(ImportedFunction {
                                index: function_index,
                                module: import.module.to_owned(),
                                name: import.name.to_owned(),
                                type_index,
                            });
                            function_index += 1;
                        }
                        TypeRef::Global(global) => globals.push(Global {
                            index: globals.len() as u32,
                            value_type: format!("{:?}", global.content_type),
                            mutable: global.mutable,
                            imported: true,
                            initialization: Vec::new(),
                            exports: Vec::new(),
                        }),
                        _ => {}
                    }
                }
            }
            Payload::GlobalSection(section) => {
                for global in section {
                    let global = global.map_err(parse_error)?;
                    if globals.len() >= MAXIMUM_FUNCTIONS {
                        return Err(ProbeError::new("original global census exceeds bound"));
                    }
                    retain_bytes(&mut listing_bytes_left, 256)?;
                    globals.push(Global {
                        index: globals.len() as u32,
                        value_type: format!("{:?}", global.ty.content_type),
                        mutable: global.ty.mutable,
                        imported: false,
                        initialization: global
                            .init_expr
                            .get_operators_reader()
                            .into_iter()
                            .map(|value| {
                                value
                                    .map_err(parse_error)
                                    .and_then(|value| bounded_debug(&value))
                            })
                            .collect::<Result<_, _>>()?,
                        exports: Vec::new(),
                    });
                }
            }
            Payload::ExportSection(section) => {
                for export in section {
                    let export = export.map_err(parse_error)?;
                    exports_count += 1;
                    if exports_count > MAXIMUM_FUNCTIONS {
                        return Err(ProbeError::new("original export census exceeds bound"));
                    }
                    retain_bytes(&mut listing_bytes_left, export.name.len() + 64)?;
                    match export.kind {
                        ExternalKind::Func => function_exports
                            .entry(export.index)
                            .or_default()
                            .push(export.name.to_owned()),
                        ExternalKind::Global => global_exports
                            .entry(export.index)
                            .or_default()
                            .push(export.name.to_owned()),
                        _ => {}
                    }
                }
            }
            Payload::CustomSection(section) if section.name() == "name" => {
                for name in NameSectionReader::new(section.data(), section.data_offset()) {
                    if let Name::Function(section) = name.map_err(parse_error)? {
                        for name in section {
                            let name = name.map_err(parse_error)?;
                            retain_bytes(&mut listing_bytes_left, name.name.len() + 64)?;
                            if names.len() >= MAXIMUM_FUNCTIONS
                                || name.name.len() > 4096
                                || names.insert(name.index, name.name.to_owned()).is_some()
                            {
                                return Err(ProbeError::new(
                                    "original function names duplicate or exceed bound",
                                ));
                            }
                        }
                    }
                }
            }
            Payload::CodeSectionEntry(body) => {
                if functions.len() + imported_functions.len() >= MAXIMUM_FUNCTIONS {
                    return Err(ProbeError::new("original function census exceeds bound"));
                }
                retain_bytes(&mut listing_bytes_left, 256)?;
                let range = body.range();
                let mut instructions = None;
                let mut calls = None;
                if selected_set.contains(&function_index) {
                    let mut selected_instructions = Vec::new();
                    let mut selected_calls = Vec::new();
                    let mut reader = body.get_operators_reader().map_err(parse_error)?;
                    while !reader.eof() {
                        instructions_left = instructions_left.checked_sub(1).ok_or_else(|| {
                            ProbeError::new("original instruction listing exceeds count bound")
                        })?;
                        let start = reader.original_position();
                        let operator = reader.read().map_err(parse_error)?;
                        let end = reader.original_position();
                        let operator_text = bounded_debug(&operator)?;
                        retain_bytes(
                            &mut listing_bytes_left,
                            operator_text.len() + (end - start) * 2 + 128,
                        )?;
                        let offset = u32_offset(start - range.start)?;
                        let relative_end = u32_offset(end - range.start)?;
                        let target = match &operator {
                            Operator::Call { function_index } => Some(CallTarget::Direct {
                                function_index: *function_index,
                            }),
                            Operator::CallIndirect {
                                type_index,
                                table_index,
                                ..
                            } => Some(CallTarget::Indirect {
                                type_index: *type_index,
                                table_index: *table_index,
                            }),
                            Operator::ReturnCall { function_index } => {
                                Some(CallTarget::ReturnDirect {
                                    function_index: *function_index,
                                })
                            }
                            Operator::ReturnCallIndirect {
                                type_index,
                                table_index,
                            } => Some(CallTarget::ReturnIndirect {
                                type_index: *type_index,
                                table_index: *table_index,
                            }),
                            Operator::CallRef { .. } | Operator::ReturnCallRef { .. } => {
                                return Err(ProbeError::new(
                                    "reference call profile is unsupported",
                                ))
                            }
                            _ => None,
                        };
                        if let Some(target) = target {
                            selected_calls.push(Callsite {
                                offset,
                                end: relative_end,
                                target,
                            });
                        }
                        selected_instructions.push(Instruction {
                            offset,
                            end: relative_end,
                            bytes_hex: format!("0x{}", hex::encode(&wasm[start..end])),
                            operator: operator_text,
                        });
                    }
                    instructions = Some(selected_instructions);
                    calls = Some(selected_calls);
                }
                functions.push(Function {
                    function_index,
                    function_body_sha256: sha2_256(&wasm[range.clone()]),
                    body_bytes: u32_offset(range.len())?,
                    name: None,
                    exports: Vec::new(),
                    instructions,
                    calls,
                });
                function_index += 1;
            }
            _ => {}
        }
    }
    for function in &mut functions {
        function.name = names.remove(&function.function_index);
        function.exports = function_exports
            .remove(&function.function_index)
            .unwrap_or_default();
    }
    for global in &mut globals {
        global.exports = global_exports.remove(&global.index).unwrap_or_default();
    }
    if selected_set.iter().any(|selected| {
        !functions
            .iter()
            .any(|function| function.function_index == *selected)
    }) {
        return Err(ProbeError::new(
            "selected original function absent or imported",
        ));
    }
    Ok(Inspection {
        schema: "urnetwork-original-wasm-profile-inspection-v1".to_owned(),
        authority: "static-original-bytes-only-no-semantic-approval".to_owned(),
        original_code_sha256: expected_sha256,
        expanded_code_sha256: sha2_256(&wasm),
        original_bytes: code.len(),
        expanded_bytes: wasm.len(),
        imported_functions,
        functions,
        globals,
    })
}

fn validate_global(
    global: &Option<String>,
    inspection: &Inspection,
    profile: &ObservationProfile,
) -> Result<(), ProbeError> {
    if let Some(name) = global {
        if !inspection.globals.iter().any(|global| {
            global.value_type == format!("{:?}", ValType::I32)
                && !global.imported
                && (global.exports.contains(name)
                    || profile.original_globals.iter().any(|alias| {
                        alias.global_index == global.index && alias.export_name == *name
                    }))
        }) {
            return Err(ProbeError::new(
                "capture base is not an original defined exported i32 global",
            ));
        }
    }
    Ok(())
}

fn validate_memory(
    memory: &[MemoryCapture],
    inspection: &Inspection,
    profile: &ObservationProfile,
) -> Result<(), ProbeError> {
    for capture in memory {
        validate_global(&capture.global, inspection, profile)?;
        if let Some(repeat) = &capture.repeat {
            validate_global(&repeat.count.global, inspection, profile)?;
        }
    }
    Ok(())
}

/// Emit exactly the existing wire profile. Neither source_review_sha256 nor
/// assembly success substitutes for independent signed producer authority.
pub fn assemble_profile(code: &[u8], proposal: &ProfileProposal) -> Result<Vec<u8>, ProbeError> {
    if proposal.schema != "urnetwork-original-wasm-profile-proposal-v1"
        || proposal.profile.rules.len() != proposal.reviewed_calls.len()
    {
        return Err(ProbeError::new(
            "profile proposal schema or exact rule review differs",
        ));
    }
    let selected: Vec<_> = proposal
        .profile
        .rules
        .iter()
        .map(|rule| rule.function_index)
        .collect::<BTreeSet<_>>()
        .into_iter()
        .collect();
    let inspection = inspect_original(code, proposal.profile.runtime_code_sha256, &selected)?;
    for (rule, reviewed) in proposal.profile.rules.iter().zip(&proposal.reviewed_calls) {
        let function = inspection
            .functions
            .iter()
            .find(|function| function.function_index == rule.function_index)
            .ok_or_else(|| ProbeError::new("reviewed original function absent"))?;
        let instructions = function
            .instructions
            .as_ref()
            .ok_or_else(|| ProbeError::new("reviewed instructions absent"))?;
        let calls: Vec<_> = function
            .calls
            .as_ref()
            .ok_or_else(|| ProbeError::new("reviewed calls absent"))?
            .iter()
            .filter(|call| call.offset >= rule.offset_start && call.offset < rule.offset_end)
            .cloned()
            .collect();
        if rule.function_body_sha256 != function.function_body_sha256
            || reviewed.function_index != rule.function_index
            || reviewed.offset_start != rule.offset_start
            || reviewed.offset_end != rule.offset_end
            || !instructions
                .iter()
                .any(|instruction| instruction.offset == rule.offset_start)
            || !(rule.offset_end == function.body_bytes
                || instructions
                    .iter()
                    .any(|instruction| instruction.offset == rule.offset_end))
            || calls.is_empty()
            || calls != reviewed.calls
        {
            return Err(ProbeError::new(
                "profile instruction boundary, original body or complete call review differs",
            ));
        }
        validate_memory(&rule.memory, &inspection, &proposal.profile)?;
    }
    let wasm =
        sp_maybe_compressed_blob::decompress(code, MAXIMUM_EXPANDED_BYTES).map_err(parse_error)?;
    crate::historical::memory_bound(&wasm, None)?;
    validate_profile(
        proposal.profile.clone(),
        proposal.profile.runtime_code_sha256,
        &wasm,
    )?;
    let profile = serde_json::to_vec(&proposal.profile).map_err(parse_error)?;
    if profile.len() > MAXIMUM_PROPOSAL_BYTES {
        return Err(ProbeError::new(
            "assembled profile exceeds public capture input bound",
        ));
    }
    Ok(profile)
}

#[cfg(test)]
#[path = "observation_profile_tests.rs"]
mod tests;
