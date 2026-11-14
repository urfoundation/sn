//! Collect an execution witness from a retained, read-only parent trie.
//!
//! The same pinned SDK function backs node `ProofProvider::execution_proof`.
//! Merely receiving a read proof does not grant completeness: this helper runs
//! the whole original block, retains code/heap and root-write paths, and then
//! subjects the exported job to the separate strict proof replay. It never
//! commits the speculative overlay or admits source semantics/finality/fees.
//! Callers must enclose execution in the owned subprocess deadline; a backend
//! accessor must not perform unbounded blocking I/O outside that process.

use super::*;
use sp_core::H256;
use sp_state_machine::{TrieBackend, TrieBackendBuilder, TrieBackendStorage};
use std::collections::BTreeMap;
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Mutex,
};

#[cfg(target_os = "linux")]
#[path = "historical_capture_feed.rs"]
mod feed;
#[path = "historical_capture_scope.rs"]
mod scope;

#[cfg(target_os = "linux")]
use std::{
    fs::{File, OpenOptions},
    io::Read,
    os::{
        fd::AsRawFd,
        unix::fs::{MetadataExt, OpenOptionsExt},
    },
};

/// A bounded capture request omits proof nodes and obtains code from the actual
/// parent backend. Every selected body/header/code identity is caller-pinned.
pub const CAPTURE_SCHEMA: &str = "urnetwork-historical-execution-capture-v1";
pub const MAXIMUM_CAPTURE_REQUEST_BYTES: usize = 20 * 1024 * 1024;
pub const MAXIMUM_CAPTURE_REPORT_BYTES: usize = 104 * 1024 * 1024;

#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct CaptureRequest {
    pub schema: String,
    pub parent_header_hex: String,
    pub parent_hash: [u8; 32],
    pub child_header_hex: String,
    pub child_hash: [u8; 32],
    pub extrinsics_hex: Vec<String>,
    pub runtime_code_sha256: [u8; 32],
    pub runtime_code_blake2b_256: [u8; 32],
    pub execution_state_version: u8,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub observation_profile: Option<observer::ObservationProfile>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub principal_queries: Option<Vec<principal::PrincipalQuery>>,
    #[serde(default, skip_serializing_if = "principal::is_false")]
    pub principal_effects: bool,
}

/// Exact job JSON is preserved as bytes in a string, avoiding authority based
/// on another language's JSON re-encoding. The job digest is also in replay.
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct CaptureReport {
    pub schema: String,
    pub request_sha256: [u8; 32],
    pub sdk_revision: String,
    pub capture_method: String,
    pub backend_reads: usize,
    pub backend_read_bytes: usize,
    pub job_json: String,
    pub replay: HistoricalReport,
}

#[derive(Default)]
struct CapturedNodes {
    reads: usize,
    read_bytes: usize,
    bytes: usize,
    nodes: BTreeMap<H256, Vec<u8>>,
    failure: Option<String>,
}

/// The only parent-state surface is a read accessor. Keep an independent sticky
/// failure because SDK trie helpers may suppress an error and return old roots.
struct CaptureStorage<'a, S> {
    original: &'a S,
    canceled: &'a AtomicBool,
    captured: &'a Mutex<CapturedNodes>,
    maximum_nodes: usize,
    maximum_node_bytes: usize,
    maximum_proof_bytes: usize,
    maximum_reads: usize,
    maximum_read_bytes: usize,
}

impl<S: TrieBackendStorage<Blake2Hasher>> TrieBackendStorage<Blake2Hasher>
    for CaptureStorage<'_, S>
{
    fn get(&self, key: &H256, prefix: (&[u8], Option<u8>)) -> Result<Option<Vec<u8>>, String> {
        let before = || -> Result<(), String> {
            let mut state = self
                .captured
                .lock()
                .map_err(|_| "historical capture lock poisoned")?;
            if let Some(error) = &state.failure {
                return Err(error.clone());
            }
            if self.canceled.load(Ordering::Acquire) {
                return Err("historical capture canceled".to_owned());
            }
            state.reads = state
                .reads
                .checked_add(1)
                .ok_or("historical capture read overflow")?;
            if state.reads > self.maximum_reads {
                return Err("historical capture read count bound".to_owned());
            }
            Ok(())
        };
        let outcome = (|| {
            before()?;
            let value = self.original.get(key, prefix)?;
            let raw = value
                .as_ref()
                .ok_or("historical capture parent trie node is absent")?;
            if raw.is_empty()
                || raw.len() > self.maximum_node_bytes
                || H256(blake2_256(raw)) != *key
            {
                return Err(
                    "historical capture trie node size or content identity differs".to_owned(),
                );
            }
            // Returned bytes can establish an actual contradiction. Preserve
            // that refusal even if the owner canceled during this read.
            if self.canceled.load(Ordering::Acquire) {
                return Err("historical capture canceled".to_owned());
            }
            let mut state = self
                .captured
                .lock()
                .map_err(|_| "historical capture lock poisoned")?;
            state.read_bytes = state
                .read_bytes
                .checked_add(raw.len())
                .ok_or("historical capture I/O overflow")?;
            if state.read_bytes > self.maximum_read_bytes {
                return Err("historical capture cumulative read byte bound".to_owned());
            }
            if !state.nodes.contains_key(key) {
                if state.nodes.len() >= self.maximum_nodes
                    || raw.len() > self.maximum_proof_bytes - state.bytes
                {
                    return Err("historical capture retained proof bound".to_owned());
                }
                state.bytes += raw.len();
                state.nodes.insert(*key, raw.clone());
            }
            Ok(value)
        })();
        if let Err(error) = &outcome {
            if let Ok(mut state) = self.captured.lock() {
                state.failure.get_or_insert_with(|| error.clone());
            }
        }
        outcome
    }
}

/// Inspect the retained accessor cause before SDK trie error translation. A
/// canceled read must not become an invalid-root claim, and an already
/// observed backend refusal must not be erased by a later cancellation.
fn check_failure(captured: &Mutex<CapturedNodes>) -> Result<(), ProbeError> {
    let state = captured
        .lock()
        .map_err(|_| ProbeError::new("historical capture lock poisoned"))?;
    if let Some(error) = &state.failure {
        return Err(ProbeError::new(error));
    }
    Ok(())
}

fn check_cancellation(canceled: &AtomicBool) -> Result<(), ProbeError> {
    if canceled.load(Ordering::Acquire) {
        return Err(ProbeError::new("historical capture canceled"));
    }
    Ok(())
}

fn check(canceled: &AtomicBool, captured: &Mutex<CapturedNodes>) -> Result<(), ProbeError> {
    check_failure(captured)?;
    check_cancellation(canceled)
}

/// Instance-local observation of completed work. The public entry has no
/// observer; tests use these boundaries without timers or global hooks.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) enum CapturePhase {
    Code,
    Heap,
    Execution,
    Root,
    Replay,
    Report,
}

/// Borrow an archive node's retained parent `state.as_trie_backend()`. This
/// uses only its content-addressed read surface, never its transaction writer,
/// runtime override provider, signer, offchain extensions or network services.
pub fn capture_historical_on_backend<S: TrieBackendStorage<Blake2Hasher>>(
    raw: &[u8],
    parent_backend: &TrieBackend<S, Blake2Hasher>,
    canceled: &AtomicBool,
) -> Result<Vec<u8>, ProbeError> {
    capture_historical_on_backend_observed(raw, parent_backend, canceled, |_| {})
}

pub(super) fn capture_historical_on_backend_observed<S: TrieBackendStorage<Blake2Hasher>>(
    raw: &[u8],
    parent_backend: &TrieBackend<S, Blake2Hasher>,
    canceled: &AtomicBool,
    observed: impl FnMut(CapturePhase),
) -> Result<Vec<u8>, ProbeError> {
    capture_historical_scoped(
        raw,
        parent_backend,
        canceled,
        observed,
        &scope::Scopes::default(),
    )
}

fn capture_historical_scoped<S: TrieBackendStorage<Blake2Hasher>>(
    raw: &[u8],
    parent_backend: &TrieBackend<S, Blake2Hasher>,
    canceled: &AtomicBool,
    mut observed: impl FnMut(CapturePhase),
    scopes: &scope::Scopes,
) -> Result<Vec<u8>, ProbeError> {
    let captured = Mutex::new(CapturedNodes::default());
    check(canceled, &captured)?;
    if raw.is_empty() || raw.len() > MAXIMUM_CAPTURE_REQUEST_BYTES {
        return Err(ProbeError::new("historical capture request byte bound"));
    }
    let request: CaptureRequest = serde_json::from_slice(raw)
        .map_err(|e| ProbeError::new(format!("historical capture request JSON: {e}")))?;
    principal::validate(&request.principal_queries)?;
    principal::validate_effects(request.principal_effects, &request.principal_queries)?;
    if request.schema != CAPTURE_SCHEMA || request.extrinsics_hex.len() > 16384 {
        return Err(ProbeError::new(
            "historical capture schema or body item bound",
        ));
    }
    let parent: NativeHeader = scale_exact(
        "capture parent header",
        &hex_bytes(
            "capture parent header",
            &request.parent_header_hex,
            MAXIMUM_HEADER_BYTES,
        )?,
    )?;
    let child: NativeHeader = scale_exact(
        "capture child header",
        &hex_bytes(
            "capture child header",
            &request.child_header_hex,
            MAXIMUM_HEADER_BYTES,
        )?,
    )?;
    if parent.hash().0 != request.parent_hash
        || child.hash().0 != request.child_hash
        || child.parent_hash() != &parent.hash()
        || parent.number().checked_add(1) != Some(*child.number())
        || parent.state_root() != parent_backend.root()
        || parent.digest().logs.len() > 64
        || child.digest().logs.len() > 64
    {
        return Err(ProbeError::new(
            "historical capture parent/child/backend identity differs",
        ));
    }
    let state_version = StateVersion::try_from(request.execution_state_version)
        .map_err(|_| ProbeError::new("historical capture state version unsupported"))?;
    let mut body = Vec::new();
    let mut body_bytes = 0;
    for encoded in &request.extrinsics_hex {
        check(canceled, &captured)?;
        let bytes = hex_bytes("capture extrinsic", encoded, MAXIMUM_BLOCK_BYTES)?;
        body_bytes += bytes.len();
        if body_bytes > MAXIMUM_BLOCK_BYTES {
            return Err(ProbeError::new("historical capture block byte bound"));
        }
        body.push(scale_exact::<OpaqueExtrinsic>("capture extrinsic", &bytes)?);
    }
    if BlakeTwo256::ordered_trie_root(
        body.iter().map(Encode::encode).collect(),
        extrinsics_root_state_version(request.execution_state_version)?,
    ) != *child.extrinsics_root()
    {
        return Err(ProbeError::new(
            "historical capture extrinsics root differs",
        ));
    }
    let (_, maximum_nodes, maximum_proof_bytes) = proof_limits(&request.observation_profile);
    let native = native_profile(&request.observation_profile);
    let (maximum_reads, maximum_read_bytes, maximum_report_bytes) = if native {
        (4 * 65536, 256 * 1024 * 1024, 272 * 1024 * 1024)
    } else {
        (65536, 64 * 1024 * 1024, MAXIMUM_CAPTURE_REPORT_BYTES)
    };
    let original = TrieBackendBuilder::new(
        CaptureStorage {
            original: parent_backend.backend_storage(),
            canceled,
            captured: &captured,
            maximum_nodes,
            maximum_node_bytes: proof_node_limit(&request.observation_profile),
            maximum_proof_bytes,
            maximum_reads,
            maximum_read_bytes,
        },
        *parent.state_root(),
    )
    .with_recorder(Default::default())
    .build();
    let backend = scope::ScopedBackend {
        inner: original,
        scopes,
    };
    let code = backend.storage(well_known_keys::CODE);
    observed(CapturePhase::Code);
    check_failure(&captured)?;
    let code = code
        .map_err(|e| ProbeError::new(format!("historical capture parent code: {e}")))?
        .ok_or_else(|| ProbeError::new("historical capture parent code absent"))?;
    if code.len() > MAXIMUM_CODE_BYTES
        || sha2_256(&code) != request.runtime_code_sha256
        || blake2_256(&code) != request.runtime_code_blake2b_256
    {
        return Err(ProbeError::new(
            "historical capture parent runtime code differs",
        ));
    }
    check(canceled, &captured)?;
    let heap_pages = backend.storage(well_known_keys::HEAP_PAGES);
    observed(CapturePhase::Heap);
    check_failure(&captured)?;
    let heap_pages = heap_pages
        .map_err(|e| ProbeError::new(format!("historical capture heap proof: {e}")))?
        .map(|bytes| scale_exact::<u64>("capture heap pages", &bytes))
        .transpose()?;
    check(canceled, &captured)?;
    let wasm = sp_maybe_compressed_blob::decompress(&code, MAXIMUM_EXPANDED_CODE_BYTES)
        .map_err(|e| ProbeError::new(format!("historical capture code decompression: {e}")))?;
    memory_bound(&wasm, heap_pages)?;
    if let Some(profile) = &request.observation_profile {
        for key in epoch_layout::state_keys(profile)? {
            // Retain parent inclusion/absence paths even when the original
            // runtime never reads this observer-only key. Replay samples the
            // actual live overlay at the selected callback, not this value.
            let value = backend.storage(&key);
            check_failure(&captured)?;
            value.map_err(|e| {
                ProbeError::new(format!("historical capture observation-state proof: {e}"))
            })?;
            check(canceled, &captured)?;
        }
    }
    // The capture pass must record observer-only proof paths too. The same
    // reviewed aliases expose original globals without changing instructions;
    // original code identity stays in the request/job, with a separate cache.
    let aliases = request
        .observation_profile
        .as_ref()
        .map(|profile| profile.original_globals.as_slice())
        .unwrap_or_default();
    let observed_code = global_alias::expose(&wasm, aliases)?;
    let runtime_bytes = if aliases.is_empty() {
        code.as_slice()
    } else {
        observed_code.as_ref()
    };
    let wrapped = WrappedRuntimeCode(runtime_bytes.into());
    let runtime = RuntimeCode {
        code_fetcher: &wrapped,
        heap_pages,
        hash: if aliases.is_empty() {
            request.runtime_code_blake2b_256.to_vec()
        } else {
            blake2_256(runtime_bytes).to_vec()
        },
    };
    let observation = request
        .observation_profile
        .clone()
        .map(|profile| {
            observer::HistoricalObserver::new(
                profile,
                request.runtime_code_sha256,
                &wasm,
                heap_pages,
            )
        })
        .transpose()?;
    let executor =
        WasmExecutor::<observer::ObservedHosts<hosts::HistoricalHostFunctions>>::builder()
            .with_allow_missing_host_functions(true)
            .with_onchain_heap_alloc_strategy(HeapAllocStrategy::Dynamic {
                maximum_pages: Some(1024),
            })
            .with_offchain_heap_alloc_strategy(HeapAllocStrategy::Dynamic {
                maximum_pages: Some(1024),
            })
            .build();
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
        return Err(ProbeError::new("historical capture seal ordering differs"));
    }
    let block = Block {
        header: execution_header,
        extrinsics: body,
    };
    let mut overlay = OverlayedChanges::<Blake2Hasher>::default();
    let mut extensions = Extensions::default();
    extensions.register(hosts::HistoricalBudget(hosts::Budget::default()));
    let _ = principal::observe(
        &request.principal_queries,
        &backend,
        &executor,
        &mut extensions,
        &runtime,
        parent.hash(),
    )?;
    check(canceled, &captured)?;
    if let Some(observation) = observation {
        extensions.register(observation);
    }
    // Use the pinned helper's public recorder/StateMachine primitives directly.
    // Converting this owner via AsTrieBackend would discard operation scope.
    // Execute the block onchain, as strict replay does. The SDK's offchain
    // context ignores the proved heap-pages override and can change execution.
    let execution = std::panic::catch_unwind(AssertUnwindSafe(|| {
        StateMachine::new(
            &backend,
            &mut overlay,
            &executor,
            "Core_execute_block",
            &block.encode(),
            &mut extensions,
            &runtime,
            CallContext::Onchain,
        )
        .execute()
    }));
    observed(CapturePhase::Execution);
    check_failure(&captured)?;
    scopes.check().map_err(ProbeError::new)?;
    let output = execution
        .map_err(|_| ProbeError::new("historical capture execution panicked on parent proof"))?
        .map_err(|e| ProbeError::new(format!("historical capture execution refused: {e}")))?;
    if !output.is_empty()
        || overlay.transaction_depth() != 0
        || extensions
            .get_mut(TypeId::of::<hosts::HistoricalBudget>())
            .and_then(|value| value.downcast_mut::<hosts::HistoricalBudget>())
            .is_none_or(|value| value.0.depth != 0)
    {
        return Err(ProbeError::new(
            "historical capture output or transaction balance differs",
        ));
    }
    check(canceled, &captured)?;
    // Record paths needed to materialize writes too. A runtime need not ask
    // the root itself. A suppressed SDK read/root error remains sticky above,
    // and strict replay below independently rejects incomplete write paths.
    let root = std::panic::catch_unwind(AssertUnwindSafe(|| {
        overlay.storage_root(&backend, state_version).0
    }));
    observed(CapturePhase::Root);
    check_failure(&captured)?;
    scopes.check().map_err(ProbeError::new)?;
    let root =
        root.map_err(|_| ProbeError::new("historical capture root materialization panicked"))?;
    if root != *child.state_root() {
        return Err(ProbeError::new(
            "historical capture child state root differs",
        ));
    }
    check(canceled, &captured)?;
    // Capture observations are provisional: finish their bounded transaction
    // owner and discard them. Only independent strict replay publishes evidence.
    if let Some(observer) = extensions
        .get_mut(TypeId::of::<observer::HistoricalObserver>())
        .and_then(|value| value.downcast_mut::<observer::HistoricalObserver>())
    {
        let _ = observer.finish(None, request.extrinsics_hex.len())?;
    }
    extensions.deregister(TypeId::of::<observer::HistoricalObserver>());
    let _ = principal::observe_execution(
        request.principal_effects,
        &request.principal_queries,
        &backend,
        &mut overlay,
        &executor,
        &mut extensions,
        &runtime,
        child.hash(),
        state_version,
        root,
    )?;
    check(canceled, &captured)?;
    check_failure(&captured)?;
    scopes.check().map_err(ProbeError::new)?;
    let proof = backend
        .inner
        .extract_proof()
        .ok_or_else(|| ProbeError::new("historical capture recorder is absent"))?;
    let state = captured
        .into_inner()
        .map_err(|_| ProbeError::new("historical capture lock poisoned"))?;
    let mut nodes: BTreeSet<Vec<u8>> = state.nodes.into_values().collect();
    nodes.extend(proof.into_iter_nodes());
    if nodes.len() > maximum_nodes
        || nodes.iter().map(Vec::len).sum::<usize>() > maximum_proof_bytes
    {
        return Err(ProbeError::new("historical capture combined proof bound"));
    }
    let job = HistoricalJob {
        schema: HISTORICAL_SCHEMA.to_owned(),
        parent_header_hex: request.parent_header_hex,
        parent_hash: request.parent_hash,
        child_header_hex: request.child_header_hex,
        child_hash: request.child_hash,
        extrinsics_hex: request.extrinsics_hex,
        runtime_code_hex: format!("0x{}", hex::encode(code)),
        runtime_code_sha256: request.runtime_code_sha256,
        runtime_code_blake2b_256: request.runtime_code_blake2b_256,
        execution_state_version: request.execution_state_version,
        proof_nodes_hex: nodes
            .iter()
            .map(|node| format!("0x{}", hex::encode(node)))
            .collect(),
        observation_profile: request.observation_profile,
        principal_queries: request.principal_queries,
        principal_effects: request.principal_effects,
    };
    let job_json = serde_json::to_string(&job)
        .map_err(|e| ProbeError::new(format!("historical capture job JSON: {e}")))?;
    check_cancellation(canceled)?;
    let replay = replay_historical_json(job_json.as_bytes());
    observed(CapturePhase::Replay);
    // A completed strict replay refusal is evidence already obtained; owner
    // cancellation must not erase it or publish a successful report instead.
    let replay = replay?;
    check_cancellation(canceled)?;
    let report = CaptureReport {
        schema: CAPTURE_SCHEMA.to_owned(),
        request_sha256: sha2_256(raw),
        sdk_revision: POLKADOT_SDK_REVISION.to_owned(),
        capture_method: "pinned-sdk-execution-proof-plus-strict-replay".to_owned(),
        backend_reads: state.reads,
        backend_read_bytes: state.read_bytes,
        job_json,
        replay: serde_json::from_slice(&replay)
            .map_err(|e| ProbeError::new(format!("historical capture replay JSON: {e}")))?,
    };
    let encoded = serde_json::to_vec(&report)
        .map_err(|e| ProbeError::new(format!("historical capture report JSON: {e}")))?;
    observed(CapturePhase::Report);
    if encoded.len() > maximum_report_bytes {
        return Err(ProbeError::new("historical capture report byte bound"));
    }
    check_cancellation(canceled)?;
    Ok(encoded)
}

/// A retained directory is an interchange adapter for raw trie nodes exported
/// by an archive node. Files are named by lowercase Blake2-256 and opened only
/// through its held directory descriptor. It is not a node database parser;
/// embedded node users borrow the actual SDK TrieBackend above directly.
#[cfg(target_os = "linux")]
struct NodeDirectory<'a> {
    root: &'a File,
    feed: Option<&'a feed::Refill<'a>>,
    maximum_node_bytes: usize,
}

#[cfg(target_os = "linux")]
impl TrieBackendStorage<Blake2Hasher> for NodeDirectory<'_> {
    fn get(&self, key: &H256, prefix: (&[u8], Option<u8>)) -> Result<Option<Vec<u8>>, String> {
        let path = format!(
            "/proc/self/fd/{}/{}",
            self.root.as_raw_fd(),
            hex::encode(key.0)
        );
        let open = || {
            OpenOptions::new()
                .read(true)
                .custom_flags(libc::O_NOFOLLOW | libc::O_NONBLOCK)
                .open(&path)
        };
        let mut file = match open() {
            Ok(file) => file,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound && self.feed.is_some() => {
                self.feed
                    .ok_or("historical capture refill owner absent")?
                    .get(key, prefix)?;
                open().map_err(|e| format!("historical capture acknowledged node read: {e}"))?
            }
            Err(error) => return Err(format!("historical capture node read: {error}")),
        };
        let before = file
            .metadata()
            .map_err(|e| format!("historical capture node stat: {e}"))?;
        if !before.is_file()
            || before.len() == 0
            || before.len() > self.maximum_node_bytes as u64
            || before.nlink() != 1
        {
            return Err("historical capture node is not a bounded unique regular file".to_owned());
        }
        let mut raw = Vec::with_capacity(before.len() as usize);
        (&mut file)
            .take((self.maximum_node_bytes + 1) as u64)
            .read_to_end(&mut raw)
            .map_err(|e| format!("historical capture node payload: {e}"))?;
        let after = file
            .metadata()
            .map_err(|e| format!("historical capture node closing stat: {e}"))?;
        let identity = |value: &std::fs::Metadata| {
            (
                value.dev(),
                value.ino(),
                value.len(),
                value.mode(),
                value.nlink(),
                value.mtime(),
                value.mtime_nsec(),
                value.ctime(),
                value.ctime_nsec(),
            )
        };
        if identity(&before) != identity(&after)
            || raw.len() != before.len() as usize
            || blake2_256(&raw) != key.0
        {
            return Err(
                "historical capture node changed or differs from its content address".to_owned(),
            );
        }
        Ok(Some(raw))
    }
}

/// The standalone worker receives an already retained directory from its Go
/// owner. Directory replacement cannot retarget it; each payload additionally
/// authenticates its own content address and the request's parent state root.
#[cfg(target_os = "linux")]
pub fn capture_historical_directory_json(raw: &[u8], root: &File) -> Result<Vec<u8>, ProbeError> {
    capture_historical_directory_feed_json(raw, root, None)
}

/// The optional inherited pipes grant only bounded proof refill for this exact
/// request. They do not select a route or grant signer/finality authority.
#[cfg(target_os = "linux")]
pub fn capture_historical_directory_feed_json(
    raw: &[u8],
    root: &File,
    pipes: Option<(&File, &File)>,
) -> Result<Vec<u8>, ProbeError> {
    if raw.is_empty() || raw.len() > MAXIMUM_CAPTURE_REQUEST_BYTES {
        return Err(ProbeError::new("historical capture request byte bound"));
    }
    let request: CaptureRequest = serde_json::from_slice(raw)
        .map_err(|e| ProbeError::new(format!("historical capture request JSON: {e}")))?;
    let parent: NativeHeader = scale_exact(
        "capture parent header",
        &hex_bytes(
            "capture parent header",
            &request.parent_header_hex,
            MAXIMUM_HEADER_BYTES,
        )?,
    )?;
    if !root
        .metadata()
        .map_err(|e| ProbeError::new(format!("historical capture node directory: {e}")))?
        .is_dir()
    {
        return Err(ProbeError::new(
            "historical capture node directory is absent",
        ));
    }
    if pipes.is_some() && !native_profile(&request.observation_profile) {
        return Err(ProbeError::new(
            "historical refill requires the native execution profile",
        ));
    }
    let scopes = scope::Scopes::default();
    let refill = pipes.map(|(request_pipe, response_pipe)| {
        feed::Refill::new(
            raw,
            parent.hash(),
            *parent.state_root(),
            &scopes,
            request_pipe,
            response_pipe,
        )
    });
    let backend = TrieBackendBuilder::new(
        NodeDirectory {
            root,
            feed: refill.as_ref(),
            maximum_node_bytes: proof_node_limit(&request.observation_profile),
        },
        *parent.state_root(),
    )
    .build();
    capture_historical_scoped(raw, &backend, &AtomicBool::new(false), |_| {}, &scopes)
}
