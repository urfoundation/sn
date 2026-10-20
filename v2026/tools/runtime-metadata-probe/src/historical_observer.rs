//! Observe original compiled-Wasm locations at host calls. No instruction is
//! inserted into the runtime. These records are proof-relative evidence for a
//! separately reviewed fee/native decoder, not amounts or runtime admission.

use crate::ProbeError;
use serde::{Deserialize, Serialize};
use sp_core::hashing::sha2_256;
use sp_externalities::ExternalitiesExt;
use sp_wasm_interface::{Function, FunctionContext, HostFunctionRegistry, HostFunctions};
use std::collections::BTreeMap;

const MAXIMUM_RULES: usize = 32;
const MAXIMUM_RECORDS: usize = 4096;
const MAXIMUM_RETAINED_BYTES: usize = 2 * 1024 * 1024;
const MAXIMUM_STACK: usize = 64;
const NATIVE_RECORDS: usize = 16384;
const NATIVE_RETAINED_BYTES: usize = 32 * 1024 * 1024;
const NATIVE_CAPTURE_BYTES: u32 = 256 * 1024;

#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct HookRule {
    pub purpose: String,
    pub function_index: u32,
    pub function_body_sha256: [u8; 32],
    pub offset_start: u32,
    pub offset_end: u32,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub memory: Vec<MemoryCapture>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub host_snapshot: Option<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub state_reads: Vec<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub storage_call: Option<super::storage_call::StorageCall>,
    #[serde(default, skip_serializing_if = "super::principal::is_false")]
    pub recipient_owner: bool,
}

/// An admitted original callsite supplies the layout, never the captured bytes.
/// Address arithmetic and every pointer read are checked against actual memory.
/// Existing exported globals or explicitly declared export-only aliases can
/// anchor a reviewed layout; no original Wasm instruction or global is added.
#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct MemoryCapture {
    pub name: String,
    pub address: u32,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub global: Option<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub dereference_offsets: Vec<u32>,
    pub bytes: u32,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub repeat: Option<MemoryRepeat>,
}

#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct MemoryRepeat {
    pub count: MemoryPointer,
    pub maximum: u32,
    pub stride: u32,
}

#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct MemoryPointer {
    pub address: u32,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub global: Option<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub dereference_offsets: Vec<u32>,
}

fn pointer_valid(global: &Option<String>, offsets: &[u32]) -> bool {
    offsets.len() <= 4
        && global
            .as_ref()
            .is_none_or(|name| !name.is_empty() && name.len() <= 64)
}

impl MemoryCapture {
    fn maximum_bytes(&self) -> u64 {
        u64::from(self.bytes) * u64::from(self.repeat.as_ref().map_or(1, |repeat| repeat.maximum))
    }

    fn valid(&self) -> bool {
        self.bytes > 0
            && self.maximum_bytes() <= u64::from(NATIVE_CAPTURE_BYTES)
            && pointer_valid(&self.global, &self.dereference_offsets)
            && self.repeat.as_ref().is_none_or(|repeat| {
                repeat.maximum > 0
                    && repeat.maximum <= 4096
                    && repeat.stride >= self.bytes
                    && repeat.stride <= NATIVE_CAPTURE_BYTES
                    && pointer_valid(&repeat.count.global, &repeat.count.dereference_offsets)
            })
    }
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct MemoryObservation {
    pub name: String,
    pub address: u32,
    pub bytes_hex: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub element_count: Option<u32>,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct StorageReturn {
    pub present: bool,
    pub value_hex: Option<String>,
    pub offset: Option<u32>,
    pub output_length: Option<u32>,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct ExecutionStateValue {
    pub key_hex: String,
    pub value_hex: Option<String>,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct NativeObservation {
    pub execution_phase_hex: Option<String>,
    pub memory: Vec<MemoryObservation>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub execution_state: Option<Vec<ExecutionStateValue>>,
}

/// Digests bind an independently supplied review reference. They do not prove
/// that a reviewer signed it or that its labels identify deployed fee semantics.
#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct ObservationProfile {
    pub schema: String,
    pub runtime_code_sha256: [u8; 32],
    pub source_review_sha256: [u8; 32],
    pub rules: Vec<HookRule>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub metadata_sha256: Option<[u8; 32]>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub principal_storage_prefixes: Option<Vec<String>>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub original_globals: Vec<super::global_alias::OriginalGlobal>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub epoch_layout: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub recipient_layout: Option<String>,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Frame {
    pub function_index: u32,
    pub function_offset: u32,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Observation {
    pub ordinal: usize,
    pub purpose: String,
    pub operation: String,
    pub key_hex: String,
    pub value_hex: Option<String>,
    pub stack: Vec<Frame>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub storage_return: Option<StorageReturn>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub native: Option<NativeObservation>,
}

// A complete committed top-storage mutation census is independent of selected
// callsites. Unlabelled writes stay visible even if their net stock change is zero.
#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct PrincipalMutation {
    pub ordinal: usize,
    pub operation: String,
    pub key_hex: String,
    pub value_sha256: Option<[u8; 32]>,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct ObservationReport {
    pub profile_sha256: [u8; 32],
    pub source_review_sha256: [u8; 32],
    pub authority: String,
    pub original_function_bodies_preserved: bool,
    pub host_calls: usize,
    pub discarded_on_rollback: usize,
    pub observations: Vec<Observation>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub fee_events: Option<super::fee_events::FeeEventReport>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub principal_mutations: Option<Vec<PrincipalMutation>>,
}

pub(super) struct Observer {
    profile: ObservationProfile,
    stack: Vec<Frame>,
    records: Vec<Observation>,
    transactions: Vec<(usize, usize)>,
    principal_prefixes: Vec<Vec<u8>>,
    principal_mutations: Vec<PrincipalMutation>,
    principal_attempts: usize,
    total_records: usize,
    total_bytes: usize,
    calls: usize,
    discarded: usize,
    memory: Vec<MemoryObservation>,
}
sp_externalities::decl_extension! { pub(super) struct HistoricalObserver(Observer); }

/// Memory import/export normalization may move module offsets. Function-relative
/// positions remain admissible only if every code body is byte-identical after
/// the exact pinned SDK normalization used by this executor configuration.
fn bodies(wasm: &[u8]) -> Result<BTreeMap<u32, Vec<u8>>, ProbeError> {
    let mut functions = 0u32;
    let mut result = BTreeMap::new();
    for payload in wasmparser::Parser::new(0).parse_all(wasm) {
        match payload.map_err(|e| ProbeError::new(format!("observer Wasm parse: {e}")))? {
            wasmparser::Payload::ImportSection(section) => {
                for import in section {
                    if matches!(
                        import
                            .map_err(|e| ProbeError::new(format!("observer import: {e}")))?
                            .ty,
                        wasmparser::TypeRef::Func(_)
                    ) {
                        functions = functions
                            .checked_add(1)
                            .ok_or_else(|| ProbeError::new("observer function overflow"))?;
                    }
                }
            }
            wasmparser::Payload::CodeSectionEntry(body) => {
                result.insert(functions, wasm[body.range()].to_vec());
                functions = functions
                    .checked_add(1)
                    .ok_or_else(|| ProbeError::new("observer function overflow"))?;
            }
            _ => {}
        }
    }
    Ok(result)
}

/// Static profile construction shares the replay admission checks without
/// exposing an observer or conferring execution/semantic approval.
pub(crate) fn validate_profile(
    profile: ObservationProfile,
    code_hash: [u8; 32],
    wasm: &[u8],
) -> Result<(), ProbeError> {
    HistoricalObserver::new(profile, code_hash, wasm, None).map(|_| ())
}

impl HistoricalObserver {
    pub(super) fn new(
        profile: ObservationProfile,
        code_hash: [u8; 32],
        wasm: &[u8],
        heap_pages: Option<u64>,
    ) -> Result<Self, ProbeError> {
        if !matches!(
            profile.schema.as_str(),
            "urnetwork-original-wasm-hook-observation-v1"
                | "urnetwork-original-wasm-native-observation-v2"
        ) || profile.runtime_code_sha256 != code_hash
            || profile.source_review_sha256 == [0; 32]
            || profile.rules.is_empty()
            || profile.rules.len() > MAXIMUM_RULES
        {
            return Err(ProbeError::new(
                "observer profile, code or review reference differs",
            ));
        }
        super::epoch_layout::state_keys(&profile)?;
        super::recipient_layout::validate(&profile)?;
        let mut principal_prefixes: Vec<Vec<u8>> = Vec::new();
        if let Some(prefixes) = &profile.principal_storage_prefixes {
            if profile.schema != "urnetwork-original-wasm-native-observation-v2"
                || prefixes.is_empty()
                || prefixes.len() > 16
            {
                return Err(ProbeError::new(
                    "observer principal storage scope is empty or exceeds bound",
                ));
            }
            for value in prefixes {
                let prefix = super::hex_bytes("principal storage prefix", value, 64)?;
                if prefix.is_empty()
                    || principal_prefixes
                        .iter()
                        .any(|prior| prefix.starts_with(prior) || prior.starts_with(&prefix))
                {
                    return Err(ProbeError::new(
                        "observer principal storage prefixes overlap",
                    ));
                }
                principal_prefixes.push(prefix);
            }
        }
        for capture in profile.rules.iter().flat_map(|rule| &rule.memory) {
            for global in [
                capture.global.as_ref(),
                capture
                    .repeat
                    .as_ref()
                    .and_then(|repeat| repeat.count.global.as_ref()),
            ]
            .into_iter()
            .flatten()
            {
                if global.starts_with("__urnetwork_observe_global_")
                    && !profile
                        .original_globals
                        .iter()
                        .any(|alias| alias.export_name == *global)
                {
                    return Err(ProbeError::new("original capture alias is undeclared"));
                }
            }
        }
        for alias in &profile.original_globals {
            let used = profile
                .rules
                .iter()
                .flat_map(|rule| &rule.memory)
                .any(|capture| {
                    capture.global.as_ref() == Some(&alias.export_name)
                        || capture.repeat.as_ref().is_some_and(|repeat| {
                            repeat.count.global.as_ref() == Some(&alias.export_name)
                        })
                });
            if profile.schema != "urnetwork-original-wasm-native-observation-v2" || !used {
                return Err(ProbeError::new(
                    "original global alias requires an exact native capture use",
                ));
            }
        }
        super::host_snapshot::validate(&profile, wasm)?;
        super::storage_call::validate(&profile, wasm)?;
        let observed = super::global_alias::expose(wasm, &profile.original_globals)?;
        let original = bodies(wasm)?;
        let mut normalized =
            sc_executor_common::runtime_blob::RuntimeBlob::uncompress_if_needed(&observed)
                .map_err(|e| ProbeError::new(format!("observer SDK runtime normalization: {e}")))?;
        normalized
            .convert_memory_import_into_export()
            .map_err(|e| ProbeError::new(format!("observer SDK memory normalization: {e}")))?;
        let heap = heap_pages
            .map(|pages| sc_executor::HeapAllocStrategy::Static {
                extra_pages: pages as u32,
            })
            .unwrap_or(sc_executor::HeapAllocStrategy::Dynamic {
                maximum_pages: Some(1024),
            });
        normalized
            .setup_memory_according_to_heap_alloc_strategy(heap)
            .map_err(|e| ProbeError::new(format!("observer SDK heap normalization: {e}")))?;
        if bodies(&normalized.serialize())? != original {
            return Err(ProbeError::new(
                "observer SDK normalization changed original function bytes",
            ));
        }
        for (index, rule) in profile.rules.iter().enumerate() {
            let body = original
                .get(&rule.function_index)
                .ok_or_else(|| ProbeError::new("observer callsite function absent"))?;
            let native = rule.purpose.starts_with("native-");
            if !(matches!(
                rule.purpose.as_str(),
                "fee-withdraw" | "fee-refund" | "ethereum-executed"
            ) || profile.schema == "urnetwork-original-wasm-native-observation-v2"
                && matches!(
                    rule.purpose.as_str(),
                    "native-uid-census"
                        | "native-epoch-index"
                        | "native-fee-exempt"
                        | "native-fee-refund-zero"
                        | "native-drain"
                        | "native-epoch"
                        | "native-emission"
                        | "native-miner-credit"
                        | "native-miner-capture"
                        | "native-recipient-owner-hotkey"
                        | "native-recipient-auto-stake"
                        | "native-recipient-owner"
                        | "native-owner-recycle"
                        | "native-yuma-meta"
                        | "native-yuma-settings"
                        | "native-yuma-node"
                        | "native-yuma-weights"
                        | "native-yuma-bonds"
                        | "native-principal-deposit"
                        | "native-principal-withdrawal"
                        | "native-principal-refund"
                        | "native-principal-vault-capture"
                        | "native-principal-earning"
                        | "native-principal-support"
                ))
                || !native && !rule.memory.is_empty()
                || rule.memory.len() > 16
                || rule.memory.iter().enumerate().any(|(index, capture)| {
                    capture.name.is_empty()
                        || capture.name.len() > 48
                        || !capture
                            .name
                            .bytes()
                            .all(|v| v.is_ascii_lowercase() || v.is_ascii_digit() || v == b'-')
                        || !capture.valid()
                        || rule.memory[..index]
                            .iter()
                            .any(|prior| prior.name == capture.name)
                })
                || rule
                    .memory
                    .iter()
                    .map(MemoryCapture::maximum_bytes)
                    .sum::<u64>()
                    > 512 * 1024
                || sha2_256(body) != rule.function_body_sha256
                || rule.offset_start >= rule.offset_end
                || rule.offset_end as usize > body.len()
                || profile.rules[..index].iter().any(|prior| {
                    ((prior.storage_call.is_some() && rule.storage_call.is_some())
                        || prior.function_index == rule.function_index
                            && prior.offset_start < rule.offset_end
                            && rule.offset_start < prior.offset_end)
                        && super::storage_call::overlaps(prior, rule)
                })
            {
                return Err(ProbeError::new(
                    "observer original body, callsite range or unique purpose differs",
                ));
            }
        }
        Ok(Self(Observer {
            profile,
            stack: Vec::new(),
            records: Vec::new(),
            transactions: Vec::new(),
            principal_prefixes,
            principal_mutations: Vec::new(),
            principal_attempts: 0,
            total_records: 0,
            total_bytes: 0,
            calls: 0,
            discarded: 0,
            memory: Vec::new(),
        }))
    }

    pub(super) fn finish(
        &mut self,
        layout: Option<&super::fee_events::EventLayout>,
        extrinsics: usize,
    ) -> Result<ObservationReport, ProbeError> {
        if !self.0.transactions.is_empty() {
            return Err(ProbeError::new("observer unfinished storage transaction"));
        }
        let profile_bytes = serde_json::to_vec(&self.0.profile)
            .map_err(|e| ProbeError::new(format!("observer profile encoding: {e}")))?;
        let principal_mutations = if self.0.profile.principal_storage_prefixes.is_some() {
            Some(std::mem::take(&mut self.0.principal_mutations))
        } else {
            None
        };
        Ok(ObservationReport {
            profile_sha256: sha2_256(&profile_bytes),
            source_review_sha256: self.0.profile.source_review_sha256,
            authority: "caller-supplied-unapproved-callsite-profile".to_owned(),
            original_function_bodies_preserved: true,
            host_calls: self.0.calls,
            discarded_on_rollback: self.0.discarded,
            fee_events: layout
                .map(|layout| layout.decode(&self.0.records, extrinsics))
                .transpose()?,
            observations: std::mem::take(&mut self.0.records),
            principal_mutations,
        })
    }
}

/// The extension is job-owned; no global trace/budget can cross concurrent
/// replays. The complete block/postroot must succeed before any trace escapes.
pub(super) fn observe(
    ext: &mut dyn sp_core::traits::Externalities,
    operation: &str,
    key: &[u8],
    value: Option<&[u8]>,
) {
    observe_value(ext, operation, key, value, None);
}

/// A missing return record is not an observed absence. Legacy profiles retain
/// their exact request-only wire grammar; v2 records the full actual storage
/// value before applying read offset/output slicing.
pub(super) fn observe_return(
    mut ext: &mut dyn sp_core::traits::Externalities,
    operation: &str,
    key: &[u8],
    value: Option<&[u8]>,
    slice: Option<(u32, u32)>,
) {
    if !ext
        .extension::<HistoricalObserver>()
        .is_some_and(|observer| {
            observer.0.profile.schema == "urnetwork-original-wasm-native-observation-v2"
                && selected_purpose(&observer.0).is_some()
        })
    {
        observe(ext, operation, key, None);
        return;
    }
    assert!(
        value.is_none_or(|bytes| bytes.len() <= 1024 * 1024),
        "observer storage return bound"
    );
    let returned = StorageReturn {
        present: value.is_some(),
        value_hex: value.map(|bytes| format!("0x{}", hex::encode(bytes))),
        offset: slice.map(|(offset, _)| offset),
        output_length: slice.map(|(_, length)| length),
    };
    observe_value(ext, operation, key, None, Some(returned));
}

fn observe_value(
    mut ext: &mut dyn sp_core::traits::Externalities,
    operation: &str,
    key: &[u8],
    value: Option<&[u8]>,
    mut returned: Option<StorageReturn>,
) {
    let (native_profile, selected) = ext
        .extension::<HistoricalObserver>()
        .map(|observer| {
            (
                observer.0.profile.schema == "urnetwork-original-wasm-native-observation-v2",
                selected_purpose(&observer.0),
            )
        })
        .unwrap_or((false, None));
    // This is an actual execution-state read, checked by the same strict proof
    // backend. A missing phase remains absent and cannot become Initialization.
    let phase = if native_profile
        && selected
            .as_ref()
            .is_some_and(|purpose| purpose.starts_with("native-"))
    {
        let key = [
            sp_core::hashing::twox_128(b"System"),
            sp_core::hashing::twox_128(b"ExecutionPhase"),
        ]
        .concat();
        let raw = ext.storage(&key);
        assert!(
            raw.as_ref().is_none_or(|value| value.len() <= 5),
            "observer execution phase bound"
        );
        raw.map(|value| format!("0x{}", hex::encode(value)))
    } else {
        if !native_profile {
            returned = None;
        }
        None
    };
    // These are explicit observer reads of the SAME live proof/overlay. They
    // are not relabeled runtime get calls or values from a later RPC reply.
    let state_keys = ext
        .extension::<HistoricalObserver>()
        .map(|observer| {
            observer
                .0
                .profile
                .rules
                .iter()
                .find(|rule| super::storage_call::matches(rule, &observer.0.stack))
                .map(|rule| {
                    if let Some(call) = &rule.storage_call {
                        assert_eq!(
                            operation, call.operation,
                            "observer original storage operation differs"
                        );
                    }
                    if rule.recipient_owner {
                        vec![(
                            super::recipient_layout::owner_key(&observer.0.memory)
                                .expect("observer original hotkey differs"),
                            32usize,
                            true,
                        )]
                    } else {
                        rule.state_reads
                            .iter()
                            .map(|key| {
                                (
                                    hex::decode(&key[2..]).expect("observer validated state key"),
                                    8usize,
                                    false,
                                )
                            })
                            .collect()
                    }
                })
                .unwrap_or_default()
        })
        .unwrap_or_default();
    let execution_state =
        if state_keys.is_empty() {
            None
        } else {
            Some(
                state_keys
                    .into_iter()
                    .map(|(key, maximum, exact)| {
                        super::hosts::charge(ext, key.len());
                        let value = ext.storage(&key);
                        assert!(
                            value.as_ref().is_none_or(|bytes| bytes.len() <= maximum
                                && (!exact || bytes.len() == maximum)),
                            "observer execution-state value bound"
                        );
                        if let Some(raw) = &value {
                            super::hosts::charge(ext, raw.len());
                        }
                        ExecutionStateValue {
                            key_hex: format!("0x{}", hex::encode(&key)),
                            value_hex: value.map(|bytes| format!("0x{}", hex::encode(bytes))),
                        }
                    })
                    .collect(),
            )
        };
    let Some(observer) = ext.extension::<HistoricalObserver>() else {
        return;
    };
    let observer = &mut observer.0;
    observer.calls += 1;
    assert!(observer.calls <= 65536, "observer host work bound");
    if matches!(operation, "set" | "clear" | "append" | "clear_prefix")
        && observer.principal_prefixes.iter().any(|prefix| {
            key.starts_with(prefix) || operation == "clear_prefix" && prefix.starts_with(key)
        })
    {
        observer.principal_attempts += 1;
        assert!(
            observer.principal_attempts <= NATIVE_RECORDS && key.len() <= 512,
            "observer principal mutation census bound"
        );
        observer.principal_mutations.push(PrincipalMutation {
            ordinal: observer.calls,
            operation: operation.to_owned(),
            key_hex: format!("0x{}", hex::encode(key)),
            value_sha256: value.map(sha2_256),
        });
    }
    if let Some(purpose) = selected {
        // Hex encoding expands payloads; refuse before allocating that copy.
        let payload_bytes = key
            .len()
            .checked_add(value.map_or(0, <[u8]>::len))
            .expect("observer input overflow");
        assert!(
            payload_bytes <= MAXIMUM_RETAINED_BYTES / 2,
            "observer retained input bound"
        );
        let native = purpose.starts_with("native-");
        let record = Observation {
            ordinal: observer.calls,
            purpose,
            operation: operation.to_owned(),
            key_hex: format!("0x{}", hex::encode(key)),
            value_hex: value.map(|bytes| format!("0x{}", hex::encode(bytes))),
            stack: observer.stack.clone(),
            storage_return: returned,
            native: if native {
                Some(NativeObservation {
                    execution_phase_hex: phase,
                    memory: observer.memory.clone(),
                    execution_state,
                })
            } else {
                None
            },
        };
        let size = serde_json::to_vec(&record)
            .expect("observer bounded record encoding")
            .len();
        observer.total_bytes = observer
            .total_bytes
            .checked_add(size)
            .expect("observer byte overflow");
        observer.total_records += 1;
        let (maximum_records, maximum_bytes) = if native_profile {
            (
                if observer
                    .profile
                    .rules
                    .iter()
                    .any(|rule| rule.purpose.starts_with("native-yuma-"))
                {
                    6 * 4096
                } else {
                    NATIVE_RECORDS
                },
                NATIVE_RETAINED_BYTES,
            )
        } else {
            (MAXIMUM_RECORDS, MAXIMUM_RETAINED_BYTES)
        };
        assert!(
            observer.total_records <= maximum_records && observer.total_bytes <= maximum_bytes,
            "observer retained evidence bound"
        );
        observer.records.push(record);
    }
}

fn selected_purpose(observer: &Observer) -> Option<String> {
    let mut selected = None;
    for rule in &observer.profile.rules {
        if super::storage_call::matches(rule, &observer.stack) {
            assert!(
                selected.is_none(),
                "observer ambiguous original callsite purpose"
            );
            selected = Some(rule.purpose.clone());
        }
    }
    selected
}

fn memory_address<State>(
    caller: &mut wasmtime::Caller<'_, State>,
    mut address: u32,
    global: &Option<String>,
    offsets: &[u32],
) -> u32 {
    if let Some(name) = global {
        let global = caller
            .get_export(name)
            .and_then(|value| value.into_global())
            .expect("observer original memory base global is absent");
        let base = global
            .get(&mut *caller)
            .i32()
            .expect("observer memory base is not i32") as u32;
        address = base
            .checked_add(address)
            .expect("observer memory base overflow");
    }
    let actual = caller
        .get_export("memory")
        .and_then(|value| value.into_memory())
        .expect("observer original memory export is absent");
    let bytes = actual.data(&*caller);
    for offset in offsets {
        let end = (address as usize)
            .checked_add(4)
            .expect("observer pointer overflow");
        let pointer = bytes
            .get(address as usize..end)
            .expect("observer pointer outside original memory");
        address = u32::from_le_bytes(pointer.try_into().expect("observer pointer width"))
            .checked_add(*offset)
            .expect("observer pointer addition overflow");
    }
    address
}

pub(super) fn transaction(mut ext: &mut dyn sp_core::traits::Externalities, operation: &str) {
    let Some(observer) = ext.extension::<HistoricalObserver>() else {
        return;
    };
    let observer = &mut observer.0;
    match operation {
        "start" => {
            assert!(
                observer.transactions.len() < 32,
                "observer transaction depth bound"
            );
            observer
                .transactions
                .push((observer.records.len(), observer.principal_mutations.len()));
        }
        "commit" => {
            observer
                .transactions
                .pop()
                .expect("observer transaction imbalance");
        }
        "rollback" => {
            let retained = observer
                .transactions
                .pop()
                .expect("observer transaction imbalance");
            observer.discarded += observer.records.len() - retained.0;
            observer.records.truncate(retained.0);
            observer.principal_mutations.truncate(retained.1);
        }
        _ => panic!("observer unsupported transaction operation"),
    }
}

/// Wrap registration, not runtime code. The SDK executes the same host body;
/// Wasmtime supplies function-relative positions from the actual active stack.
pub(super) struct ObservedHosts<H>(std::marker::PhantomData<H>);
impl<H: HostFunctions> HostFunctions for ObservedHosts<H> {
    fn host_functions() -> Vec<&'static dyn Function> {
        H::host_functions()
    }
    fn register_static<T: HostFunctionRegistry>(registry: &mut T) -> Result<(), T::Error> {
        struct Registry<'a, T>(&'a mut T);
        impl<T: HostFunctionRegistry> HostFunctionRegistry for Registry<'_, T> {
            type State = T::State;
            type Error = T::Error;
            type FunctionContext = T::FunctionContext;
            fn with_function_context<R>(
                mut caller: wasmtime::Caller<Self::State>,
                callback: impl FnOnce(&mut dyn FunctionContext) -> R,
            ) -> R {
                let enabled = sp_externalities::with_externalities(|mut ext| {
                    ext.extension::<HistoricalObserver>().is_some()
                })
                .unwrap_or(false);
                let stack = if enabled {
                    let trace = wasmtime::WasmBacktrace::force_capture(&caller);
                    assert!(
                        !trace.frames().is_empty() && trace.frames().len() <= MAXIMUM_STACK,
                        "observer Wasm stack bound"
                    );
                    trace
                        .frames()
                        .iter()
                        .map(|frame| Frame {
                            function_index: frame.func_index(),
                            function_offset: u32::try_from(
                                frame
                                    .func_offset()
                                    .expect("observer original code location missing"),
                            )
                            .expect("observer offset overflow"),
                        })
                        .collect()
                } else {
                    Vec::new()
                };
                let captures = sp_externalities::with_externalities(|mut ext| {
                    let Some(observer) = ext.extension::<HistoricalObserver>() else {
                        return Vec::new();
                    };
                    let rules: Vec<_> = observer
                        .0
                        .profile
                        .rules
                        .iter()
                        .filter(|rule| super::storage_call::matches(rule, &stack))
                        .collect();
                    assert!(rules.len() <= 1, "observer ambiguous memory callsite");
                    rules
                        .first()
                        .map(|rule| rule.memory.clone())
                        .unwrap_or_default()
                })
                .unwrap_or_default();
                let mut memory = Vec::with_capacity(captures.len());
                for capture in captures {
                    let address = memory_address(
                        &mut caller,
                        capture.address,
                        &capture.global,
                        &capture.dereference_offsets,
                    );
                    let element_count = capture.repeat.as_ref().map(|repeat| {
                        let pointer = &repeat.count;
                        let address = memory_address(
                            &mut caller,
                            pointer.address,
                            &pointer.global,
                            &pointer.dereference_offsets,
                        );
                        let actual = caller
                            .get_export("memory")
                            .and_then(|value| value.into_memory())
                            .expect("observer original memory export is absent");
                        let bytes = actual.data(&caller);
                        let end = (address as usize)
                            .checked_add(4)
                            .expect("observer count pointer overflow");
                        let count = u32::from_le_bytes(
                            bytes
                                .get(address as usize..end)
                                .expect("observer count outside original memory")
                                .try_into()
                                .expect("observer count width"),
                        );
                        assert!(
                            count <= repeat.maximum,
                            "observer vector exceeds original count bound"
                        );
                        count
                    });
                    let count = element_count.unwrap_or(1);
                    let stride = capture
                        .repeat
                        .as_ref()
                        .map_or(capture.bytes, |repeat| repeat.stride);
                    let actual = caller
                        .get_export("memory")
                        .and_then(|value| value.into_memory())
                        .expect("observer original memory export is absent");
                    let bytes = actual.data(&caller);
                    let size = (count as usize)
                        .checked_mul(capture.bytes as usize)
                        .expect("observer output length overflow");
                    assert!(
                        size <= NATIVE_CAPTURE_BYTES as usize,
                        "observer vector output bound"
                    );
                    let mut observed = Vec::with_capacity(size);
                    for index in 0..count {
                        let start = u64::from(address) + u64::from(index) * u64::from(stride);
                        let end = start + u64::from(capture.bytes);
                        let start =
                            usize::try_from(start).expect("observer vector address overflow");
                        let end = usize::try_from(end).expect("observer vector length overflow");
                        observed.extend_from_slice(
                            bytes
                                .get(start..end)
                                .expect("observer slice outside original memory"),
                        );
                    }
                    memory.push(MemoryObservation {
                        name: capture.name,
                        address,
                        bytes_hex: format!("0x{}", hex::encode(observed)),
                        element_count,
                    });
                }
                T::with_function_context(caller, |context| {
                    if enabled {
                        sp_externalities::with_externalities(|mut ext| {
                            let observer = &mut ext
                                .extension::<HistoricalObserver>()
                                .expect("observer disappeared")
                                .0;
                            observer.stack = stack;
                            observer.memory = memory;
                            let snapshot = observer.profile.rules.iter().find_map(|rule| {
                                let frame = observer.stack.first()?;
                                (rule.function_index == frame.function_index
                                    && frame.function_offset >= rule.offset_start
                                    && frame.function_offset < rule.offset_end)
                                    .then(|| rule.host_snapshot.clone())
                                    .flatten()
                            });
                            if let Some(name) = snapshot {
                                observe(ext, "host", name.as_bytes(), None);
                            }
                        })
                        .expect("observer execution context absent");
                    }
                    callback(context)
                })
            }
            fn register_static<Params, Results>(
                &mut self,
                name: &str,
                function: impl wasmtime::IntoFunc<Self::State, Params, Results> + 'static,
            ) -> Result<(), Self::Error> {
                self.0.register_static(name, function)
            }
        }
        H::register_static(&mut Registry(registry))
    }
}
