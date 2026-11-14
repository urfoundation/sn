//! Finite transition replay uses its own bounded storage hosts. The existing
//! stateless metadata probe is unchanged. A caller-owned subprocess deadline
//! must enclose this worker; no report is production selection authority.

use crate::{probe_blob, ExpectedRuntimeArtifact, ProbeError, POLKADOT_SDK_REVISION};
use parity_scale_codec::Encode;
use sc_executor::{HeapAllocStrategy, WasmExecutor};
use serde::{Deserialize, Serialize};
use sp_core::{
    hashing::sha2_256,
    storage::{well_known_keys::is_child_storage_key, StateVersion, Storage as NativeStorage},
    traits::{CallContext, CodeExecutor, Externalities, RuntimeCode, WrappedRuntimeCode},
    Blake2Hasher,
};
use sp_externalities::ExternalitiesExt;
use sp_runtime_interface::{pass_by::*, runtime_interface};
use sp_state_machine::TestExternalities;
use std::collections::BTreeMap;

#[cfg(test)]
#[path = "replay_tests.rs"]
mod tests;

pub const MAXIMUM_JOB_BYTES: usize = 48 * 1024 * 1024;
pub const REPLAY_SCHEMA: &str = "urnetwork-runtime-transition-replay-v1";
const MAXIMUM_STATE_BYTES: usize = 4 * 1024 * 1024;
const MAXIMUM_VALUE_BYTES: usize = 256 * 1024;

/// Input remains explicit evidence, not an assertion that it is complete chain state.
#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct ReplayArtifact {
    pub expected: ExpectedRuntimeArtifact,
    pub wasm_hex: String,
}

/// Canonically ordered complete map supplied to this finite case.
#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct ReplayEntry {
    pub key: String,
    pub value: String,
}

/// Both modules must reproduce a separately declared result and entire state.
#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct ReplayStep {
    pub method: String,
    pub input: String,
    pub expected_output: String,
    pub expected_state: Vec<ReplayEntry>,
}

/// Each case starts fresh; steps within a case retain their exact storage state.
#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct ReplayCase {
    pub name: String,
    pub initial_state: Vec<ReplayEntry>,
    pub steps: Vec<ReplayStep>,
}

/// Exact cases have their own digest. Semantic rules remain an independently
/// approved external identity; this finite executor does not verify their scope.
#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct ReplayJob {
    pub schema: String,
    pub policy_sha256: [u8; 32],
    pub source_build_evidence_sha256: [u8; 32],
    pub rules_sha256: [u8; 32],
    pub cases_sha256: [u8; 32],
    pub base: ReplayArtifact,
    pub candidate: ReplayArtifact,
    pub cases_json: String,
}

/// Digests bind the exact compared results; they assert no universal equivalence.
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ReplayReport {
    pub schema: String,
    pub job_sha256: [u8; 32],
    pub rules_sha256: [u8; 32],
    pub cases_sha256: [u8; 32],
    pub sdk_revision: String,
    pub cases: usize,
    pub steps: usize,
    pub outputs_sha256: [u8; 32],
    pub finite_replay_only: bool,
    pub semantic_rules_verified: bool,
    pub complete_semantic_equivalence: bool,
    pub production_selection: bool,
}

/// Per-case limits travel with SDK externalities, never a process-global counter.
#[derive(Default)]
struct ReplayLimits {
    calls: usize,
    depth: usize,
}

sp_externalities::decl_extension! {
    struct ReplayBudget(ReplayLimits);
}

/// Every installed state host consumes a finite operation budget.
fn charge(mut ext: &mut dyn Externalities) {
    let budget = ext
        .extension::<ReplayBudget>()
        .expect("replay budget absent");
    budget.0.calls += 1;
    assert!(budget.0.calls <= 4096, "replay storage operation limit");
}

/// An open transaction is an incomplete comparison, never implicit success.
fn transaction_depth(mut ext: &mut dyn Externalities) -> usize {
    ext.extension::<ReplayBudget>()
        .expect("replay budget absent")
        .0
        .depth
}

/// Child roots are never interpreted as ordinary top-level storage keys.
fn key_allowed(key: &[u8]) -> bool {
    !key.is_empty() && key.len() <= 512 && !is_child_storage_key(key)
}

/// Check the prospective allocation before it reaches the SDK overlay.
fn before_write(ext: &mut dyn Externalities, key: &[u8], value: Option<&[u8]>) {
    charge(ext);
    assert!(key_allowed(key), "replay storage key limit");
    assert!(key != b":code", "replay runtime code is immutable");
    assert!(
        value.is_none_or(|v| v.len() <= MAXIMUM_VALUE_BYTES),
        "replay storage value limit"
    );
    let mut total = value.map_or(0, |v| key.len() + v.len());
    let mut count = usize::from(value.is_some());
    let mut cursor = Vec::new();
    while let Some(next) = ext.next_storage_key(&cursor) {
        if next != key && next != b":code" {
            total += next.len() + ext.storage(&next).expect("enumerated storage absent").len();
            count += 1;
        }
        cursor = next;
    }
    assert!(
        count <= 1024 && total <= MAXIMUM_STATE_BYTES,
        "replay storage aggregate limit"
    );
}

/// Only this explicit subset is installed. Child, prefix deletion, offchain,
/// keystore and all other omitted imports remain failing stubs.
#[runtime_interface]
pub trait Storage {
    fn get(
        &mut self,
        key: PassFatPointerAndRead<&[u8]>,
    ) -> AllocateAndReturnByCodec<Option<Vec<u8>>> {
        charge(*self);
        assert!(key_allowed(key), "replay storage key limit");
        self.storage(key)
    }
    fn read(
        &mut self,
        key: PassFatPointerAndRead<&[u8]>,
        output: PassFatPointerAndReadWrite<&mut [u8]>,
        offset: u32,
    ) -> AllocateAndReturnByCodec<Option<u32>> {
        charge(*self);
        assert!(
            key_allowed(key) && output.len() <= MAXIMUM_VALUE_BYTES,
            "replay storage read limit"
        );
        self.storage(key).map(|value| {
            let remaining = &value[(offset as usize).min(value.len())..];
            let count = output.len().min(remaining.len());
            output[..count].copy_from_slice(&remaining[..count]);
            remaining.len() as u32
        })
    }
    fn set(&mut self, key: PassFatPointerAndRead<&[u8]>, value: PassFatPointerAndRead<&[u8]>) {
        before_write(*self, key, Some(value));
        self.set_storage(key.to_vec(), value.to_vec());
    }
    fn clear(&mut self, key: PassFatPointerAndRead<&[u8]>) {
        before_write(*self, key, None);
        self.clear_storage(key);
    }
    fn exists(&mut self, key: PassFatPointerAndRead<&[u8]>) -> bool {
        charge(*self);
        assert!(key_allowed(key), "replay storage key limit");
        self.exists_storage(key)
    }
    fn next_key(
        &mut self,
        key: PassFatPointerAndRead<&[u8]>,
    ) -> AllocateAndReturnByCodec<Option<Vec<u8>>> {
        charge(*self);
        assert!(key.len() <= 512, "replay storage key limit");
        self.next_storage_key(key)
    }
    fn start_transaction(&mut self) {
        charge(*self);
        let budget = self
            .extension::<ReplayBudget>()
            .expect("replay budget absent");
        budget.0.depth += 1;
        assert!(budget.0.depth <= 16, "replay transaction depth limit");
        self.storage_start_transaction();
    }
    fn rollback_transaction(&mut self) {
        charge(*self);
        let budget = self
            .extension::<ReplayBudget>()
            .expect("replay budget absent");
        assert!(budget.0.depth > 0, "replay unbalanced transaction");
        budget.0.depth -= 1;
        self.storage_rollback_transaction()
            .expect("replay rollback failed");
    }
    fn commit_transaction(&mut self) {
        charge(*self);
        let budget = self
            .extension::<ReplayBudget>()
            .expect("replay budget absent");
        assert!(budget.0.depth > 0, "replay unbalanced transaction");
        budget.0.depth -= 1;
        self.storage_commit_transaction()
            .expect("replay commit failed");
    }
}

type ReplayHostFunctions = (
    sp_io::allocator::HostFunctions,
    sp_io::logging::HostFunctions,
    sp_io::hashing::HostFunctions,
    storage::HostFunctions,
);

/// No alternate hex spelling can change a retained evidence identity.
fn bytes(label: &str, value: &str, maximum: usize) -> Result<Vec<u8>, ProbeError> {
    if !value.starts_with("0x") || value.len() > maximum * 2 + 2 {
        return Err(ProbeError::new(format!(
            "{label} is malformed or oversized"
        )));
    }
    let raw =
        hex::decode(&value[2..]).map_err(|_| ProbeError::new(format!("{label} hex differs")))?;
    if value != format!("0x{}", hex::encode(&raw)) {
        return Err(ProbeError::new(format!("{label} hex is not canonical")));
    }
    Ok(raw)
}

/// Strict ordering refuses duplicate keys rather than silently overwriting them.
fn state(entries: &[ReplayEntry]) -> Result<BTreeMap<Vec<u8>, Vec<u8>>, ProbeError> {
    let mut result = BTreeMap::new();
    let mut previous = Vec::new();
    let mut total = 0;
    for entry in entries {
        let key = bytes("state key", &entry.key, 512)?;
        let value = bytes("state value", &entry.value, MAXIMUM_VALUE_BYTES)?;
        if !key_allowed(&key) || key == b":code" || key <= previous {
            return Err(ProbeError::new(
                "replay state keys are invalid, duplicate or unsorted",
            ));
        }
        total += key.len() + value.len();
        previous = key.clone();
        result.insert(key, value);
    }
    if result.len() > 1024 || total > MAXIMUM_STATE_BYTES {
        return Err(ProbeError::new(
            "replay initial or expected state exceeds bound",
        ));
    }
    Ok(result)
}

/// The subprocess owner supplies cancellation. This worker performs no network,
/// disk mutation, signing or production selection and emits only complete results.
pub fn replay_json(raw: &[u8]) -> Result<Vec<u8>, ProbeError> {
    if raw.is_empty() || raw.len() > MAXIMUM_JOB_BYTES {
        return Err(ProbeError::new("replay job byte bound"));
    }
    let job: ReplayJob = serde_json::from_slice(raw)
        .map_err(|e| ProbeError::new(format!("replay job JSON: {e}")))?;
    if job.schema != REPLAY_SCHEMA
        || job.policy_sha256 == [0; 32]
        || job.source_build_evidence_sha256 == [0; 32]
        || job.rules_sha256 == [0; 32]
        || sha2_256(job.cases_json.as_bytes()) != job.cases_sha256
    {
        return Err(ProbeError::new(
            "replay policy, source or exact rules binding differs",
        ));
    }
    let cases: Vec<ReplayCase> = serde_json::from_str(&job.cases_json)
        .map_err(|e| ProbeError::new(format!("replay cases JSON: {e}")))?;
    if cases.is_empty() || cases.len() > 16 {
        return Err(ProbeError::new("replay case count bound"));
    }
    let mut names = std::collections::BTreeSet::new();
    let mut steps = 0;
    for case in &cases {
        if case.name.is_empty()
            || case.name.len() > 128
            || !names.insert(&case.name)
            || case.steps.is_empty()
            || case.steps.len() > 16
        {
            return Err(ProbeError::new("replay case identity or step bound"));
        }
        state(&case.initial_state)?;
        for step in &case.steps {
            if !matches!(
                step.method.as_str(),
                "BlockBuilder_apply_extrinsic"
                    | "Core_initialize_block"
                    | "Core_execute_block"
                    | "TaggedTransactionQueue_validate_transaction"
                    | "TransactionPaymentApi_query_info"
                    | "TransactionPaymentApi_query_fee_details"
            ) {
                return Err(ProbeError::new(
                    "replay export is outside the transition profile",
                ));
            }
            bytes("step input", &step.input, MAXIMUM_VALUE_BYTES)?;
            bytes(
                "expected output",
                &step.expected_output,
                MAXIMUM_VALUE_BYTES,
            )?;
            state(&step.expected_state)?;
            steps += 1;
        }
    }
    let mut digests = Vec::new();
    for (label, artifact) in [("base", &job.base), ("candidate", &job.candidate)] {
        let wasm = bytes("runtime Wasm", &artifact.wasm_hex, 8 * 1024 * 1024)?;
        probe_blob(&wasm, &artifact.expected)
            .map_err(|e| ProbeError::new(format!("replay {label} artifact: {e}")))?;
        let wrapped = WrappedRuntimeCode(wasm.as_slice().into());
        let code = RuntimeCode {
            code_fetcher: &wrapped,
            heap_pages: None,
            hash: artifact.expected.code_blake2b_256.to_vec(),
        };
        let executor = WasmExecutor::<ReplayHostFunctions>::builder()
            .with_allow_missing_host_functions(true)
            .with_onchain_heap_alloc_strategy(HeapAllocStrategy::Dynamic {
                maximum_pages: Some(1024),
            })
            .build();
        let mut observed = Vec::new();
        for case in &cases {
            let initial = NativeStorage {
                top: state(&case.initial_state)?,
                children_default: Default::default(),
            };
            let version = StateVersion::try_from(artifact.expected.state_version)
                .map_err(|_| ProbeError::new("replay state version unsupported"))?;
            // The executing artifact is also the exact :code value visible to
            // the runtime. It is verified separately from declared application state.
            let mut backing =
                TestExternalities::<Blake2Hasher>::new_with_code_and_state(&wasm, initial, version);
            backing.register_extension(ReplayBudget(ReplayLimits::default()));
            for step in &case.steps {
                let mut ext = backing.ext();
                let result = executor
                    .call(
                        &mut ext,
                        &code,
                        &step.method,
                        &bytes("step input", &step.input, MAXIMUM_VALUE_BYTES)?,
                        CallContext::Onchain,
                    )
                    .0
                    .map_err(|e| {
                        ProbeError::new(format!(
                            "replay {label} {} {}: {e}",
                            case.name, step.method
                        ))
                    })?;
                if transaction_depth(&mut ext) != 0 {
                    return Err(ProbeError::new("replay unfinished storage transaction"));
                }
                let mut actual = BTreeMap::new();
                let mut cursor = Vec::new();
                while let Some(key) = ext.next_storage_key(&cursor) {
                    let value = ext.storage(&key).expect("enumerated storage absent");
                    if key == b":code" {
                        if value != wasm {
                            return Err(ProbeError::new("replay runtime code changed"));
                        }
                    } else {
                        actual.insert(key.clone(), value);
                    }
                    cursor = key;
                }
                if result
                    != bytes(
                        "expected output",
                        &step.expected_output,
                        MAXIMUM_VALUE_BYTES,
                    )?
                    || actual != state(&step.expected_state)?
                {
                    return Err(ProbeError::new(format!(
                        "replay {label} {} output or storage differs",
                        case.name
                    )));
                }
                observed.push(sha2_256(&(result, actual).encode()));
            }
        }
        digests.push(sha2_256(&observed.encode()));
    }
    if digests[0] != digests[1] {
        return Err(ProbeError::new("replay old/new outcome differs"));
    }
    serde_json::to_vec(&ReplayReport {
        schema: REPLAY_SCHEMA.to_owned(),
        job_sha256: sha2_256(raw),
        rules_sha256: job.rules_sha256,
        cases_sha256: job.cases_sha256,
        sdk_revision: POLKADOT_SDK_REVISION.to_owned(),
        cases: cases.len(),
        steps,
        outputs_sha256: digests[0],
        finite_replay_only: true,
        semantic_rules_verified: false,
        complete_semantic_equivalence: false,
        production_selection: false,
    })
    .map_err(|e| ProbeError::new(format!("replay report JSON: {e}")))
}
