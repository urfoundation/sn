//! Reproduce one historical block against an authenticated partial trie. This
//! establishes facts relative to caller-supplied headers, not finality, admitted
//! runtime semantics, or transaction-specific native fee withdrawal/refund.

use crate::{ProbeError, POLKADOT_SDK_REVISION};
use parity_scale_codec::{Decode, Encode};
use sc_executor::{HeapAllocStrategy, WasmExecutor};
use serde::{Deserialize, Serialize};
use sp_core::{
    hashing::{blake2_256, sha2_256},
    storage::{well_known_keys, StateVersion},
    traits::{CallContext, RuntimeCode, WrappedRuntimeCode},
    Blake2Hasher,
};
use sp_externalities::Extensions;
use sp_runtime::{
    generic::{Block, Header},
    traits::{BlakeTwo256, Hash as HashT, Header as HeaderT},
    OpaqueExtrinsic,
};
use sp_state_machine::{create_proof_check_backend, Backend, OverlayedChanges, StateMachine};
use sp_trie::StorageProof;
use sp_version::RuntimeVersion;
use std::{any::TypeId, collections::BTreeSet, panic::AssertUnwindSafe};

#[path = "historical_capture.rs"]
pub mod capture;
#[path = "historical_epoch_layout.rs"]
mod epoch_layout;
#[path = "historical_fee_events.rs"]
pub mod fee_events;
#[path = "historical_global_alias.rs"]
pub mod global_alias;
#[path = "historical_recipient_layout.rs"]
mod recipient_layout;
#[path = "historical_storage_call.rs"]
mod storage_call;

#[path = "historical_host_snapshot.rs"]
mod host_snapshot;
#[path = "historical_hosts.rs"]
mod hosts;
#[path = "historical_metadata_scope.rs"]
mod metadata_scope;
#[path = "historical_observer.rs"]
pub mod observer;
#[path = "historical_principal.rs"]
pub mod principal;
#[path = "historical_pure_hosts.rs"]
mod pure_hosts;
#[path = "historical_backend.rs"]
mod strict;
#[cfg(test)]
#[path = "historical_tests.rs"]
mod tests;

pub const HISTORICAL_SCHEMA: &str = "urnetwork-historical-proof-replay-v1";
pub const MAXIMUM_HISTORICAL_JOB_BYTES: usize = 96 * 1024 * 1024;
pub const MAXIMUM_NATIVE_JOB_BYTES: usize = 192 * 1024 * 1024;
const MAXIMUM_NATIVE_PROOF_NODES: usize = 2 * (3 * (4 * 4096 + 4) + 1);
const MAXIMUM_NATIVE_PROOF_BYTES: usize = 64 * 1024 * 1024;

fn native_profile(profile: &Option<observer::ObservationProfile>) -> bool {
    profile
        .as_ref()
        .is_some_and(|profile| profile.schema == "urnetwork-original-wasm-native-observation-v2")
}

fn proof_node_limit(profile: &Option<observer::ObservationProfile>) -> usize {
    if native_profile(profile) {
        16 * 1024 * 1024
    } else {
        MAXIMUM_CODE_BYTES
    }
}

fn proof_limits(profile: &Option<observer::ObservationProfile>) -> (usize, usize, usize) {
    if native_profile(profile) {
        (
            MAXIMUM_NATIVE_JOB_BYTES,
            MAXIMUM_NATIVE_PROOF_NODES,
            MAXIMUM_NATIVE_PROOF_BYTES,
        )
    } else {
        (MAXIMUM_HISTORICAL_JOB_BYTES, 8192, MAXIMUM_PROOF_BYTES)
    }
}

const MAXIMUM_PROOF_BYTES: usize = 24 * 1024 * 1024;
const MAXIMUM_CODE_BYTES: usize = 8 * 1024 * 1024;
// Published real runtimes exceed the original synthetic 8 MiB expanded bound.
// The original compressed code/proof value bound remains independently fixed.
const MAXIMUM_EXPANDED_CODE_BYTES: usize = 32 * 1024 * 1024;
const MAXIMUM_BLOCK_BYTES: usize = 8 * 1024 * 1024;
const MAXIMUM_HEADER_BYTES: usize = 64 * 1024;
type NativeHeader = Header<u32, BlakeTwo256>;

/// The v1 wire retains the raw stateVersion/systemVersion RPC alias, not a
/// normalized trie layout. It admits only zero and one, as the Go request and
/// job validators do. Their storage layouts differ, but the SDK selects body
/// layout zero for both. Replay separately requires the original Core_version
/// to match this raw value exactly; equal storage layouts are not sufficient.
fn extrinsics_root_state_version(system_version: u8) -> Result<StateVersion, ProbeError> {
    // StateVersion::try_from also accepts raw two as V1. That conversion must
    // not silently expand this versioned job grammar or reinterpret old pins.
    if system_version > 1 {
        return Err(ProbeError::new("historical state version unsupported"));
    }
    Ok(RuntimeVersion {
        system_version,
        ..RuntimeVersion::default()
    }
    .extrinsics_root_state_version())
}

/// Roots come only from these completely decoded headers. The caller must
/// independently admit their hashes/finality before using any resulting fact.
#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct HistoricalJob {
    pub schema: String,
    pub parent_header_hex: String,
    pub parent_hash: [u8; 32],
    pub child_header_hex: String,
    pub child_hash: [u8; 32],
    pub extrinsics_hex: Vec<String>,
    pub runtime_code_hex: String,
    pub runtime_code_sha256: [u8; 32],
    pub runtime_code_blake2b_256: [u8; 32],
    pub execution_state_version: u8,
    pub proof_nodes_hex: Vec<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub observation_profile: Option<observer::ObservationProfile>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub principal_queries: Option<Vec<principal::PrincipalQuery>>,
    #[serde(default, skip_serializing_if = "principal::is_false")]
    pub principal_effects: bool,
}

/// A complete state-root reproduction is deliberately separate from economic
/// interpretation. This worker never invents a native debit from receipt gas.
#[derive(Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct HistoricalReport {
    pub schema: String,
    pub job_sha256: [u8; 32],
    pub sdk_revision: String,
    pub host_profile: String,
    pub parent_hash: [u8; 32],
    pub child_hash: [u8; 32],
    pub parent_state_root: [u8; 32],
    pub child_state_root: [u8; 32],
    pub runtime_code_sha256: [u8; 32],
    pub proof_sha256: [u8; 32],
    pub extrinsics: usize,
    pub proof_nodes: usize,
    pub proof_bytes: usize,
    pub storage_calls: usize,
    pub storage_io_bytes: usize,
    pub post_state_reproduced: bool,
    pub anchor_authority: String,
    pub runtime_admitted: bool,
    pub native_fee_debit: Option<String>,
    pub native_fee_withdrawal_refund_observed: bool,
    pub production_selection: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub hook_observations: Option<observer::ObservationReport>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub opening_principals: Option<Vec<principal::PrincipalObservation>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub closing_principals: Option<Vec<principal::PrincipalObservation>>,
}

/// Bound before allocation and reject alternate hexadecimal spellings.
fn hex_bytes(label: &str, value: &str, maximum: usize) -> Result<Vec<u8>, ProbeError> {
    if !value.starts_with("0x") || value.len() > maximum * 2 + 2 {
        return Err(ProbeError::new(format!("historical {label} byte bound")));
    }
    let bytes = hex::decode(&value[2..])
        .map_err(|_| ProbeError::new(format!("historical {label} hex differs")))?;
    if value != format!("0x{}", hex::encode(&bytes)) {
        return Err(ProbeError::new(format!(
            "historical {label} is not canonical hex"
        )));
    }
    Ok(bytes)
}

/// Round-trip equality also rejects noncanonical compact lengths, not only
/// trailing bytes. Bounded raw input encloses every SCALE allocation here.
fn scale_exact<T: Decode + Encode>(label: &str, raw: &[u8]) -> Result<T, ProbeError> {
    let mut input = raw;
    let value = T::decode(&mut input)
        .map_err(|e| ProbeError::new(format!("historical {label} SCALE: {e}")))?;
    if !input.is_empty() || value.encode() != raw {
        return Err(ProbeError::new(format!(
            "historical {label} SCALE is not exact"
        )));
    }
    Ok(value)
}

/// Refuse oversized initial/imported memories before Wasmtime compilation.
/// The executor additionally caps dynamic growth. Heap pages are proved state.
pub(crate) fn memory_bound(wasm: &[u8], heap_pages: Option<u64>) -> Result<(), ProbeError> {
    let mut memories = 0;
    let mut check = |memory: wasmparser::MemoryType| -> Result<(), ProbeError> {
        memories += 1;
        if memories > 1
            || memory.memory64
            || memory.shared
            || memory.initial.saturating_add(heap_pages.unwrap_or(0)) > 1024
        {
            return Err(ProbeError::new("historical Wasm memory bound"));
        }
        Ok(())
    };
    for payload in wasmparser::Parser::new(0).parse_all(wasm) {
        match payload.map_err(|e| ProbeError::new(format!("historical Wasm decode: {e}")))? {
            wasmparser::Payload::MemorySection(section) => {
                for memory in section {
                    check(memory.map_err(|e| ProbeError::new(format!("historical memory: {e}")))?)?;
                }
            }
            wasmparser::Payload::ImportSection(section) => {
                for import in section {
                    let import =
                        import.map_err(|e| ProbeError::new(format!("historical import: {e}")))?;
                    if let wasmparser::TypeRef::Memory(memory) = import.ty {
                        check(memory)?;
                    }
                }
            }
            _ => {}
        }
    }
    if memories != 1 {
        return Err(ProbeError::new(
            "historical Wasm requires one bounded memory",
        ));
    }
    Ok(())
}

/// The caller owns a finite subprocess deadline and cancellation/join. No
/// external I/O is supplied to the runtime; omitted hosts trap when invoked.
/// Nothing, including a partially computed report, is published on failure.
pub fn replay_historical_json(raw: &[u8]) -> Result<Vec<u8>, ProbeError> {
    if raw.is_empty() || raw.len() > MAXIMUM_NATIVE_JOB_BYTES {
        return Err(ProbeError::new("historical job byte bound"));
    }
    let job: HistoricalJob = serde_json::from_slice(raw)
        .map_err(|e| ProbeError::new(format!("historical job JSON: {e}")))?;
    principal::validate(&job.principal_queries)?;
    principal::validate_effects(job.principal_effects, &job.principal_queries)?;
    let (maximum_job_bytes, maximum_nodes, maximum_proof_bytes) =
        proof_limits(&job.observation_profile);
    if raw.len() > maximum_job_bytes
        || job.schema != HISTORICAL_SCHEMA
        || job.proof_nodes_hex.is_empty()
        || job.proof_nodes_hex.len() > maximum_nodes
        || job.extrinsics_hex.len() > 16384
    {
        return Err(ProbeError::new("historical schema or item bound"));
    }
    let parent: NativeHeader = scale_exact(
        "parent header",
        &hex_bytes(
            "parent header",
            &job.parent_header_hex,
            MAXIMUM_HEADER_BYTES,
        )?,
    )?;
    let child: NativeHeader = scale_exact(
        "child header",
        &hex_bytes("child header", &job.child_header_hex, MAXIMUM_HEADER_BYTES)?,
    )?;
    if parent.hash().0 != job.parent_hash
        || child.hash().0 != job.child_hash
        || child.parent_hash() != &parent.hash()
        || parent.number().checked_add(1) != Some(*child.number())
        || parent.digest().logs.len() > 64
        || child.digest().logs.len() > 64
    {
        return Err(ProbeError::new(
            "historical parent/child header binding differs",
        ));
    }
    let state_version = StateVersion::try_from(job.execution_state_version)
        .map_err(|_| ProbeError::new("historical state version unsupported"))?;
    let mut extrinsics = Vec::new();
    let mut body_bytes = 0;
    for encoded in &job.extrinsics_hex {
        let bytes = hex_bytes("extrinsic", encoded, MAXIMUM_BLOCK_BYTES)?;
        body_bytes += bytes.len();
        if body_bytes > MAXIMUM_BLOCK_BYTES {
            return Err(ProbeError::new("historical block byte bound"));
        }
        extrinsics.push(scale_exact::<OpaqueExtrinsic>("extrinsic", &bytes)?);
    }
    if BlakeTwo256::ordered_trie_root(
        extrinsics.iter().map(Encode::encode).collect(),
        extrinsics_root_state_version(job.execution_state_version)?,
    ) != *child.extrinsics_root()
    {
        return Err(ProbeError::new("historical extrinsics root differs"));
    }
    let code = hex_bytes("runtime code", &job.runtime_code_hex, MAXIMUM_CODE_BYTES)?;
    if sha2_256(&code) != job.runtime_code_sha256
        || blake2_256(&code) != job.runtime_code_blake2b_256
    {
        return Err(ProbeError::new("historical runtime code digest differs"));
    }
    let mut proof_bytes = 0;
    let mut proof_nodes = BTreeSet::new();
    for node in &job.proof_nodes_hex {
        let node = hex_bytes(
            "proof node",
            node,
            proof_node_limit(&job.observation_profile),
        )?;
        proof_bytes += node.len();
        if node.is_empty() || proof_bytes > maximum_proof_bytes || !proof_nodes.insert(node) {
            return Err(ProbeError::new("historical proof bound or duplicate node"));
        }
    }
    let proof_sha256 = sha2_256(&proof_nodes.encode());
    let proof_nodes_count = proof_nodes.len();
    let work = std::sync::Arc::new(hosts::Work::default());
    let backend = strict::StrictBackend {
        work: work.clone(),
        inner: create_proof_check_backend::<Blake2Hasher>(
            *parent.state_root(),
            StorageProof::new(proof_nodes),
        )
        .map_err(|e| ProbeError::new(format!("historical parent proof: {e}")))?,
    };
    let proved_code = backend
        .storage(well_known_keys::CODE)
        .map_err(|e| ProbeError::new(format!("historical runtime code proof missing: {e}")))?;
    if proved_code.as_deref() != Some(code.as_slice()) {
        return Err(ProbeError::new("historical parent runtime code differs"));
    }
    let heap_pages = backend
        .storage(well_known_keys::HEAP_PAGES)
        .map_err(|e| ProbeError::new(format!("historical heap pages proof missing: {e}")))?
        .map(|bytes| scale_exact::<u64>("heap pages", &bytes))
        .transpose()?;
    let wasm = sp_maybe_compressed_blob::decompress(&code, MAXIMUM_EXPANDED_CODE_BYTES)
        .map_err(|e| ProbeError::new(format!("historical code decompression: {e}")))?;
    memory_bound(&wasm, heap_pages)?;
    // Original :code was authenticated above. A reviewed export-only view has
    // its own executor cache identity; it never replaces the job's original.
    let aliases = job
        .observation_profile
        .as_ref()
        .map(|profile| profile.original_globals.as_slice())
        .unwrap_or_default();
    let observed = global_alias::expose(&wasm, aliases)?;
    let runtime_bytes = if aliases.is_empty() {
        code.as_slice()
    } else {
        observed.as_ref()
    };
    let wrapped = WrappedRuntimeCode(runtime_bytes.into());
    let runtime = RuntimeCode {
        code_fetcher: &wrapped,
        heap_pages,
        hash: if aliases.is_empty() {
            job.runtime_code_blake2b_256.to_vec()
        } else {
            blake2_256(runtime_bytes).to_vec()
        },
    };
    let observation = job
        .observation_profile
        .clone()
        .map(|profile| {
            observer::HistoricalObserver::new(profile, job.runtime_code_sha256, &wasm, heap_pages)
        })
        .transpose()?;
    let executor =
        WasmExecutor::<observer::ObservedHosts<hosts::HistoricalHostFunctions>>::builder()
            .with_allow_missing_host_functions(true)
            .with_onchain_heap_alloc_strategy(HeapAllocStrategy::Dynamic {
                maximum_pages: Some(1024),
            })
            .build();
    let mut extensions = Extensions::default();
    extensions.register(hosts::HistoricalBudget(hosts::Budget {
        work,
        depth: 0,
        read_only: false,
    }));
    let mut version_overlay = OverlayedChanges::<Blake2Hasher>::default();
    let version_raw = StateMachine::new(
        &backend,
        &mut version_overlay,
        &executor,
        "Core_version",
        &[],
        &mut extensions,
        &runtime,
        CallContext::Onchain,
    )
    .execute()
    .map_err(|e| ProbeError::new(format!("historical executing runtime version: {e}")))?;
    let version: RuntimeVersion = scale_exact("runtime version", &version_raw)?;
    if version.system_version != job.execution_state_version
        || version_overlay.changes().next().is_some()
        || version_overlay.children().next().is_some()
        || extensions
            .get_mut(TypeId::of::<hosts::HistoricalBudget>())
            .and_then(|value| value.downcast_mut::<hosts::HistoricalBudget>())
            .is_none_or(|value| value.0.depth != 0)
    {
        return Err(ProbeError::new(
            "historical executing state version or version side effect differs",
        ));
    }
    let opening_principals = principal::observe(
        &job.principal_queries,
        &backend,
        &executor,
        &mut extensions,
        &runtime,
        parent.hash(),
    )?;
    // Decode metadata from the same original code with storage/offchain hosts
    // absent. The pin does not select fee accounting or require fee pallets
    // when only native storage and allocation callsites are selected.
    let event_layout = job
        .observation_profile
        .as_ref()
        .filter(|profile| profile.metadata_sha256.is_some())
        .map(|profile| {
            let metadata_executor =
                WasmExecutor::<crate::StatelessMetadataHostFunctions>::builder()
                    .with_allow_missing_host_functions(true)
                    .with_offchain_heap_alloc_strategy(HeapAllocStrategy::Dynamic {
                        maximum_pages: Some(1024),
                    })
                    .build();
            let raw =
                crate::execute_runtime_api(&metadata_executor, &runtime, "Metadata_metadata")?;
            let metadata: sp_core::OpaqueMetadata = scale_exact("original runtime metadata", &raw)?;
            metadata_scope::event_layout(profile, metadata.as_slice())
        })
        .transpose()?
        .flatten();
    // Consensus seals are external to runtime execution. Keep their original
    // header hash in the report and refuse seals placed between runtime items.
    let mut execution_header = child.clone();
    while execution_header
        .digest()
        .logs
        .last()
        .is_some_and(|item| item.as_seal().is_some())
    {
        execution_header.digest_mut().pop();
    }
    if execution_header
        .digest()
        .logs
        .iter()
        .any(|item| item.as_seal().is_some())
    {
        return Err(ProbeError::new(
            "historical consensus seal ordering differs",
        ));
    }
    let block = Block {
        header: execution_header,
        extrinsics,
    };
    let mut overlay = OverlayedChanges::<Blake2Hasher>::default();
    if let Some(observation) = observation {
        extensions.register(observation);
    }
    let output = StateMachine::new(
        &backend,
        &mut overlay,
        &executor,
        "Core_execute_block",
        &block.encode(),
        &mut extensions,
        &runtime,
        CallContext::Onchain,
    )
    .set_parent_hash(parent.hash())
    .execute()
    .map_err(|e| ProbeError::new(format!("historical execute block refused: {e}")))?;
    if !output.is_empty()
        || overlay.transaction_depth() != 0
        || extensions
            .get_mut(TypeId::of::<hosts::HistoricalBudget>())
            .and_then(|value| value.downcast_mut::<hosts::HistoricalBudget>())
            .is_none_or(|value| value.0.depth != 0)
    {
        return Err(ProbeError::new(
            "historical block output or unfinished transaction differs",
        ));
    }
    // Our strict backend traps incomplete write paths instead of inheriting
    // the SDK's old-root fallback. Such refusal publishes no result.
    let root = std::panic::catch_unwind(AssertUnwindSafe(|| {
        overlay.storage_root(&backend, state_version).0
    }))
    .map_err(|_| ProbeError::new("historical post-state proof incomplete"))?;
    if root != *child.state_root() {
        return Err(ProbeError::new(
            "historical reproduced child state root differs",
        ));
    }
    let hook_observations = extensions
        .get_mut(TypeId::of::<observer::HistoricalObserver>())
        .and_then(|value| value.downcast_mut::<observer::HistoricalObserver>())
        .map(|observer| observer.finish(event_layout.as_ref(), job.extrinsics_hex.len()))
        .transpose()?;
    // API queries are not block callsite observations. Finish and remove the
    // observer before querying the same original runtime on its completed overlay.
    extensions.deregister(TypeId::of::<observer::HistoricalObserver>());
    let closing_principals = principal::observe_execution(
        job.principal_effects,
        &job.principal_queries,
        &backend,
        &mut overlay,
        &executor,
        &mut extensions,
        &runtime,
        child.hash(),
        state_version,
        root,
    )?;
    let budget = extensions
        .get_mut(TypeId::of::<hosts::HistoricalBudget>())
        .and_then(|value| value.downcast_mut::<hosts::HistoricalBudget>())
        .ok_or_else(|| ProbeError::new("historical storage budget absent"))?;
    serde_json::to_vec(&HistoricalReport {
        schema: HISTORICAL_SCHEMA.to_owned(),
        job_sha256: sha2_256(raw),
        sdk_revision: POLKADOT_SDK_REVISION.to_owned(),
        host_profile: hosts::HOST_PROFILE.to_owned(),
        parent_hash: parent.hash().0,
        child_hash: child.hash().0,
        parent_state_root: parent.state_root().0,
        child_state_root: root.0,
        runtime_code_sha256: job.runtime_code_sha256,
        proof_sha256,
        extrinsics: job.extrinsics_hex.len(),
        proof_nodes: proof_nodes_count,
        proof_bytes,
        storage_calls: budget.0.work.counts().0,
        storage_io_bytes: budget.0.work.counts().1,
        post_state_reproduced: true,
        anchor_authority: "caller-supplied-unapproved".to_owned(),
        runtime_admitted: false,
        native_fee_debit: None,
        native_fee_withdrawal_refund_observed: false,
        production_selection: false,
        hook_observations,
        opening_principals,
        closing_principals,
    })
    .map_err(|e| ProbeError::new(format!("historical report JSON: {e}")))
}
