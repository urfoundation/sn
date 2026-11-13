//! Capture exercises the actual pinned SDK proof producer and then the strict
//! consumer. Synthetic complete maps independently determine expected roots.
//! None of these fixtures admits an actual runtime or independent finality.

use super::super::capture::{self, CaptureReport, CaptureRequest};
use super::*;
use sp_state_machine::{TrieBackend, TrieBackendBuilder, TrieBackendStorage};
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};

#[path = "historical_principal_tests.rs"]
mod principal_tests;

fn request(job: &HistoricalJob) -> CaptureRequest {
    CaptureRequest {
        schema: capture::CAPTURE_SCHEMA.to_owned(),
        parent_header_hex: job.parent_header_hex.clone(),
        parent_hash: job.parent_hash,
        child_header_hex: job.child_header_hex.clone(),
        child_hash: job.child_hash,
        extrinsics_hex: job.extrinsics_hex.clone(),
        runtime_code_sha256: job.runtime_code_sha256,
        runtime_code_blake2b_256: job.runtime_code_blake2b_256,
        execution_state_version: job.execution_state_version,
        observation_profile: job.observation_profile.clone(),
        principal_queries: job.principal_queries.clone(),
        principal_effects: job.principal_effects,
    }
}

fn backend(job: &HistoricalJob) -> TrieBackend<sp_trie::MemoryDB<Blake2Hasher>, Blake2Hasher> {
    let parent: NativeHeader = scale_exact(
        "capture fixture parent",
        &hex_bytes(
            "capture fixture parent",
            &job.parent_header_hex,
            MAXIMUM_HEADER_BYTES,
        )
        .unwrap(),
    )
    .unwrap();
    let nodes = job
        .proof_nodes_hex
        .iter()
        .map(|raw| hex_bytes("fixture node", raw, MAXIMUM_CODE_BYTES).unwrap());
    create_proof_check_backend(*parent.state_root(), StorageProof::new(nodes)).unwrap()
}

pub(super) fn collect(job: &HistoricalJob) -> Result<CaptureReport, ProbeError> {
    let raw = serde_json::to_vec(&request(job)).unwrap();
    let result =
        capture::capture_historical_on_backend(&raw, &backend(job), &AtomicBool::new(false))?;
    Ok(serde_json::from_slice(&result).expect("captured report is complete"))
}

#[test]
fn historical_capture_complete_parent_replays_top_and_child_without_committing() {
    let code = wasm("", &format!("{READ}{WRITE}{CHILD_WRITE}"));
    let job = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
        storage
            .children_default
            .get_mut(OWNER)
            .unwrap()
            .data
            .insert(b"k".to_vec(), b"v".to_vec());
    });
    let parent = backend(&job);
    let before_root = *parent.root();
    let before_account = parent.storage(ACCOUNT).unwrap();
    let before_child = parent
        .child_storage(&ChildInfo::new_default(OWNER), b"k")
        .unwrap();
    let raw = serde_json::to_vec(&request(&job)).unwrap();
    let result =
        capture::capture_historical_on_backend(&raw, &parent, &AtomicBool::new(false)).unwrap();
    let report: CaptureReport = serde_json::from_slice(&result).unwrap();
    assert_eq!(report.request_sha256, sha2_256(&raw));
    assert_eq!(
        report.capture_method,
        "pinned-sdk-execution-proof-plus-strict-replay"
    );
    assert!(report.backend_reads > 0 && report.backend_reads <= 65536);
    assert!(report.backend_read_bytes > 0 && report.backend_read_bytes <= 64 * 1024 * 1024);
    assert_eq!(report.replay.child_hash, job.child_hash);
    assert!(report.replay.post_state_reproduced);
    assert_eq!(
        report.replay.job_sha256,
        sha2_256(report.job_json.as_bytes())
    );
    assert!(!report.replay.runtime_admitted && !report.replay.production_selection);
    assert!(!report.replay.native_fee_withdrawal_refund_observed);
    assert_eq!(report.replay.native_fee_debit, None);
    let exported: HistoricalJob = serde_json::from_str(&report.job_json).unwrap();
    assert_eq!(exported.runtime_code_hex, job.runtime_code_hex);
    let second = run(&exported).expect("exported actual SDK proof must independently replay");
    assert_eq!(second.child_state_root, report.replay.child_state_root);
    assert_eq!(
        parent.root(),
        &before_root,
        "capture committed a speculative root"
    );
    assert_eq!(
        parent.storage(ACCOUNT).unwrap(),
        before_account,
        "capture committed a top-level write"
    );
    assert_eq!(
        parent
            .child_storage(&ChildInfo::new_default(OWNER), b"k")
            .unwrap(),
        before_child,
        "capture committed a child write"
    );
}

#[test]
fn historical_capture_incomplete_read_proof_never_claims_completeness() {
    for child in [false, true] {
        let code = wasm(
            "",
            if child {
                "(drop (call $child_get (i64.const 64424512512) (i64.const 4294970312)))"
            } else {
                READ
            },
        );
        let mut job = job(&code, |_| {});
        assert!(
            collect(&job).is_ok(),
            "complete parent fixture must pass before omission"
        );
        top_only_proof(&mut job, false);
        let error = collect(&job)
            .err()
            .expect("a key-list read proof was mislabelled complete execution");
        assert!(error.to_string().contains("capture"), "{error}");
    }
}

#[test]
fn historical_capture_missing_write_paths_cannot_reuse_declared_parent_root() {
    for child in [false, true] {
        let code = wasm("", if child { CHILD_WRITE } else { WRITE });
        let mut job = job(&code, |storage| {
            if child {
                storage
                    .children_default
                    .get_mut(OWNER)
                    .unwrap()
                    .data
                    .insert(b"k".to_vec(), b"v".to_vec());
            } else {
                storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
            }
        });
        assert!(collect(&job).is_ok(), "complete write parent refused");
        let parent: NativeHeader = scale_exact(
            "fixture parent",
            &hex_bytes(
                "fixture parent",
                &job.parent_header_hex,
                MAXIMUM_HEADER_BYTES,
            )
            .unwrap(),
        )
        .unwrap();
        replace_child(&mut job, |header| {
            header.set_state_root(*parent.state_root())
        });
        top_only_proof(&mut job, false);
        let error = collect(&job)
            .err()
            .expect("missing write path became a reproduced unchanged state root");
        assert!(error.to_string().contains("capture"), "{error}");
    }
}

struct ObservedStorage<'a, S> {
    inner: &'a S,
    reads: &'a AtomicUsize,
    cancel_after_read: Option<&'a AtomicBool>,
}

impl<S: TrieBackendStorage<Blake2Hasher>> TrieBackendStorage<Blake2Hasher>
    for ObservedStorage<'_, S>
{
    fn get(&self, key: &H256, prefix: (&[u8], Option<u8>)) -> Result<Option<Vec<u8>>, String> {
        self.reads.fetch_add(1, Ordering::AcqRel);
        let raw = self.inner.get(key, prefix)?;
        if let Some(canceled) = self.cancel_after_read {
            canceled.store(true, Ordering::Release);
        }
        Ok(raw)
    }
}

#[test]
fn historical_capture_parent_body_and_runtime_identity_are_independent() {
    let code = wasm("", "");
    let job = job(&code, |_| {});
    let parent = backend(&job);
    for kind in 0..5 {
        let reads = AtomicUsize::new(0);
        let mut input = request(&job);
        match kind {
            0 => input.parent_hash[0] ^= 1,
            1 => input.child_hash[0] ^= 1,
            2 => input.extrinsics_hex.clear(),
            3 => input.runtime_code_sha256[0] ^= 1,
            4 => input.runtime_code_blake2b_256[0] ^= 1,
            _ => unreachable!(),
        }
        let observed = TrieBackendBuilder::new(
            ObservedStorage {
                inner: parent.backend_storage(),
                reads: &reads,
                cancel_after_read: None,
            },
            *parent.root(),
        )
        .build();
        let error = capture::capture_historical_on_backend(
            &serde_json::to_vec(&input).unwrap(),
            &observed,
            &AtomicBool::new(false),
        )
        .expect_err("changed historical input was accepted");
        if kind < 3 {
            assert_eq!(
                reads.load(Ordering::Acquire),
                0,
                "foreign header/body read parent state"
            );
        } else {
            assert!(
                reads.load(Ordering::Acquire) > 0,
                "runtime check did not read original parent code"
            );
        }
        assert!(error.to_string().contains("differs"), "{error}");
    }
}

#[test]
fn historical_capture_cancellation_before_and_after_real_backend_read_refuses_output() {
    let code = wasm("", READ);
    let job = job(&code, |_| {});
    let parent = backend(&job);
    let raw = serde_json::to_vec(&request(&job)).unwrap();
    for after_read in [false, true] {
        let canceled = AtomicBool::new(!after_read);
        let reads = AtomicUsize::new(0);
        let observed = TrieBackendBuilder::new(
            ObservedStorage {
                inner: parent.backend_storage(),
                reads: &reads,
                cancel_after_read: Some(&canceled),
            },
            *parent.root(),
        )
        .build();
        let error = capture::capture_historical_on_backend(&raw, &observed, &canceled)
            .expect_err("canceled capture produced a proof job");
        assert!(error.to_string().contains("canceled"), "{error}");
        assert_eq!(reads.load(Ordering::Acquire), usize::from(after_read));
    }
}

#[test]
fn historical_capture_cancellation_after_completed_phases_never_publishes() {
    use capture::CapturePhase;

    let code = wasm("", &format!("{READ}{WRITE}{CHILD_WRITE}"));
    let job = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
        storage
            .children_default
            .get_mut(OWNER)
            .unwrap()
            .data
            .insert(b"k".to_vec(), b"v".to_vec());
    });
    collect(&job).expect("complete phase fixture must reach strict replay before cancellation");
    let raw = serde_json::to_vec(&request(&job)).unwrap();
    let phases = [
        CapturePhase::Code,
        CapturePhase::Heap,
        CapturePhase::Execution,
        CapturePhase::Root,
        CapturePhase::Replay,
        CapturePhase::Report,
    ];
    for (index, phase) in phases.iter().enumerate() {
        let canceled = AtomicBool::new(false);
        let mut reached = Vec::new();
        let error = capture::capture_historical_on_backend_observed(
            &raw,
            &backend(&job),
            &canceled,
            |completed| {
                reached.push(completed);
                if completed == *phase {
                    canceled.store(true, Ordering::Release);
                }
            },
        )
        .expect_err("completed-phase cancellation published a historical proof job");
        assert_eq!(reached, phases[..=index], "phase {phase:?}");
        assert_eq!(
            error.to_string(),
            "historical capture canceled",
            "phase {phase:?}"
        );
    }
}

struct PhaseCanceledStorage<'a, S> {
    inner: &'a S,
    canceled: &'a AtomicBool,
    armed: &'a AtomicBool,
    canceled_reads: &'a AtomicUsize,
}

impl<S: TrieBackendStorage<Blake2Hasher>> TrieBackendStorage<Blake2Hasher>
    for PhaseCanceledStorage<'_, S>
{
    fn get(&self, key: &H256, prefix: (&[u8], Option<u8>)) -> Result<Option<Vec<u8>>, String> {
        let raw = self.inner.get(key, prefix)?;
        if self.armed.swap(false, Ordering::AcqRel) {
            self.canceled_reads.fetch_add(1, Ordering::AcqRel);
            self.canceled.store(true, Ordering::Release);
        }
        Ok(raw)
    }
}

#[test]
fn historical_capture_backend_cancellation_survives_heap_execution_and_root_translation() {
    use capture::CapturePhase;

    let code = wasm("", &format!("{READ}{WRITE}{CHILD_WRITE}"));
    let job = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
        storage
            .children_default
            .get_mut(OWNER)
            .unwrap()
            .data
            .insert(b"k".to_vec(), b"v".to_vec());
    });
    collect(&job).expect("complete adjacent-read fixture must replay");
    let raw = serde_json::to_vec(&request(&job)).unwrap();
    for (previous, target) in [
        (None, CapturePhase::Code),
        (Some(CapturePhase::Code), CapturePhase::Heap),
        (Some(CapturePhase::Heap), CapturePhase::Execution),
        (Some(CapturePhase::Execution), CapturePhase::Root),
    ] {
        let parent = backend(&job);
        let canceled = AtomicBool::new(false);
        let armed = AtomicBool::new(previous.is_none());
        let canceled_reads = AtomicUsize::new(0);
        let observed = TrieBackendBuilder::new(
            PhaseCanceledStorage {
                inner: parent.backend_storage(),
                canceled: &canceled,
                armed: &armed,
                canceled_reads: &canceled_reads,
            },
            *parent.root(),
        )
        .build();
        let mut reached = Vec::new();
        let error = capture::capture_historical_on_backend_observed(
            &raw,
            &observed,
            &canceled,
            |completed| {
                reached.push(completed);
                if Some(completed) == previous {
                    armed.store(true, Ordering::Release);
                }
            },
        )
        .expect_err("canceled parent read became a historical proof job");
        assert_eq!(
            canceled_reads.load(Ordering::Acquire),
            1,
            "target {target:?}"
        );
        assert_eq!(
            reached.last(),
            Some(&target),
            "actual read missed target {target:?}"
        );
        assert_eq!(
            error.to_string(),
            "historical capture canceled",
            "target {target:?}"
        );
    }
}

/// Return a real node before injecting one concrete integrity fault and an
/// owner cancellation. The caller must not mistake that returned evidence for
/// the SDK's later generic invalid-root translation or erase its first cause.
struct InvalidatedStorage<'a, S> {
    inner: &'a S,
    reads: &'a AtomicUsize,
    canceled: &'a AtomicBool,
    fault: u8,
}

impl<S: TrieBackendStorage<Blake2Hasher>> TrieBackendStorage<Blake2Hasher>
    for InvalidatedStorage<'_, S>
{
    fn get(&self, key: &H256, prefix: (&[u8], Option<u8>)) -> Result<Option<Vec<u8>>, String> {
        self.reads.fetch_add(1, Ordering::AcqRel);
        let mut raw = self.inner.get(key, prefix)?;
        assert!(raw.as_ref().is_some_and(|value| !value.is_empty()));
        match self.fault {
            0 => raw.as_mut().unwrap()[0] ^= 1,
            1 => raw.as_mut().unwrap().clear(),
            2 => raw = None,
            _ => unreachable!(),
        }
        self.canceled.store(true, Ordering::Release);
        Ok(raw)
    }
}

#[test]
fn historical_capture_returned_node_refusal_precedes_later_cancellation() {
    let job = job(&wasm("", READ), |_| {});
    collect(&job).expect("complete node fixture must replay before invalidation");
    let raw = serde_json::to_vec(&request(&job)).unwrap();
    let parent = backend(&job);
    let original_code = parent.storage(well_known_keys::CODE).unwrap();
    for fault in 0..3 {
        let canceled = AtomicBool::new(false);
        let reads = AtomicUsize::new(0);
        let observed = TrieBackendBuilder::new(
            InvalidatedStorage {
                inner: parent.backend_storage(),
                reads: &reads,
                canceled: &canceled,
                fault,
            },
            *parent.root(),
        )
        .build();
        let error = capture::capture_historical_on_backend(&raw, &observed, &canceled)
            .expect_err("returned invalid node became proof evidence");
        assert_eq!(reads.load(Ordering::Acquire), 1);
        assert!(canceled.load(Ordering::Acquire));
        assert_eq!(
            error.to_string(),
            if fault == 2 {
                "historical capture parent trie node is absent"
            } else {
                "historical capture trie node size or content identity differs"
            },
            "returned integrity evidence was erased or translated"
        );
        assert_eq!(
            parent.storage(well_known_keys::CODE).unwrap(),
            original_code
        );
    }
}

#[test]
fn historical_capture_completed_refusals_survive_later_phase_cancellation() {
    use capture::CapturePhase;

    let good = job(&wasm("", ""), |_| {});
    collect(&good).expect("otherwise complete phase fixture must replay");
    for (phase, expected) in [
        (
            CapturePhase::Code,
            "historical capture parent runtime code differs",
        ),
        (
            CapturePhase::Execution,
            "historical capture execution refused:",
        ),
        (
            CapturePhase::Root,
            "historical capture child state root differs",
        ),
    ] {
        let mut job = if phase == CapturePhase::Execution {
            job(&wasm("", "unreachable"), |_| {})
        } else {
            good.clone()
        };
        if phase == CapturePhase::Root {
            replace_child(&mut job, |header| {
                let mut root = *header.state_root();
                root.0[0] ^= 1;
                header.set_state_root(root);
            });
        }
        let mut input = request(&job);
        if phase == CapturePhase::Code {
            input.runtime_code_sha256[0] ^= 1;
        }
        let canceled = AtomicBool::new(false);
        let mut reached = false;
        let error = capture::capture_historical_on_backend_observed(
            &serde_json::to_vec(&input).unwrap(),
            &backend(&job),
            &canceled,
            |completed| {
                if completed == phase {
                    reached = true;
                    canceled.store(true, Ordering::Release);
                }
            },
        )
        .expect_err("completed integrity refusal became proof evidence");
        assert!(
            reached && canceled.load(Ordering::Acquire),
            "phase {phase:?}: {error}"
        );
        assert!(
            error.to_string().starts_with(expected),
            "phase {phase:?}: {error}"
        );
        assert!(
            !error.to_string().contains("canceled"),
            "phase {phase:?}: {error}"
        );
    }
}

#[test]
fn historical_capture_missing_refund_and_observed_zero_remain_distinct_and_unadmitted() {
    for refund in [None, Some(0), Some(250)] {
        let job = fee_job(refund, false, true);
        let report =
            collect(&job).expect("complete original event runtime must capture and replay");
        assert_eq!(report.replay.native_fee_debit, None);
        assert!(
            !report.replay.native_fee_withdrawal_refund_observed && !report.replay.runtime_admitted
        );
        let fee = report.replay.hook_observations.unwrap().fee_events.unwrap();
        assert_eq!(fee.candidates.len(), 1);
        let candidate = &fee.candidates[0];
        assert_eq!(candidate.withdrawal_rao.as_deref(), Some("1000"));
        assert_eq!(candidate.refund_rao, refund.map(|value| value.to_string()));
        assert_eq!(
            candidate.debit_rao,
            refund.map(|value| (1000 - value).to_string())
        );
        assert_eq!(candidate.extrinsic_index, Some(0));
        assert_eq!(candidate.transaction_hash, [41; 32]);
        assert_eq!(
            candidate.status,
            if refund.is_some() {
                "observed-pair-unadmitted"
            } else {
                "refund-unobserved"
            }
        );
    }
}

#[test]
fn historical_capture_exact_decoder_refuses_unknown_duplicate_trailing_and_bounds() {
    let job = job(&wasm("", ""), |_| {});
    let parent = backend(&job);
    let good = serde_json::to_vec(&request(&job)).unwrap();
    let text = String::from_utf8(good.clone()).unwrap();
    let invalid = [
        format!("{{\"schema\":\"duplicate\",{}", &text[1..]).into_bytes(),
        format!("{{\"unknown\":true,{}", &text[1..]).into_bytes(),
        [good, b"{}".to_vec()].concat(),
        vec![b' '; capture::MAXIMUM_CAPTURE_REQUEST_BYTES + 1],
    ];
    for raw in invalid {
        assert!(
            capture::capture_historical_on_backend(&raw, &parent, &AtomicBool::new(false)).is_err(),
            "malformed capture input became proof evidence"
        );
    }
}

/// An explicit qualification-only exporter lets the public Go command execute
/// the real capture ELF over the same complete trie. Normal test selection
/// never writes artifacts or requires this environment variable.
#[test]
#[ignore = "requires an explicit new private capture export directory"]
fn historical_capture_export_cross_engine_fixture() {
    use std::fs;
    use std::os::unix::fs::PermissionsExt;
    let root = std::path::PathBuf::from(
        std::env::var_os("URNETWORK_HISTORICAL_CAPTURE_EXPORT")
            .expect("explicit capture export path"),
    );
    assert!(root.is_absolute());
    fs::create_dir(&root).expect("new capture export directory");
    fs::set_permissions(&root, fs::Permissions::from_mode(0o700)).unwrap();
    for (name, refund) in [("pair", Some(250)), ("missing", None), ("zero", Some(0))] {
        let directory = root.join(name);
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let nodes = directory.join("nodes");
        fs::create_dir(&nodes).unwrap();
        fs::set_permissions(&nodes, fs::Permissions::from_mode(0o700)).unwrap();
        let job = fee_job(refund, false, true);
        for value in &job.proof_nodes_hex {
            let raw = hex_bytes("fixture node", value, MAXIMUM_CODE_BYTES).unwrap();
            let path = nodes.join(hex::encode(blake2_256(&raw)));
            fs::write(&path, raw).unwrap();
            fs::set_permissions(&path, fs::Permissions::from_mode(0o600)).unwrap();
        }
        let raw = serde_json::to_vec(&request(&job)).unwrap();
        let path = directory.join("request.json");
        fs::write(&path, &raw).unwrap();
        fs::set_permissions(&path, fs::Permissions::from_mode(0o600)).unwrap();
        let expected =
            collect(&job).expect("export fixture must reach actual capture and strict replay");
        let path = directory.join("expected-report.json");
        fs::write(&path, serde_json::to_vec(&expected).unwrap()).unwrap();
        fs::set_permissions(&path, fs::Permissions::from_mode(0o600)).unwrap();
    }
}

#[cfg(target_os = "linux")]
#[test]
fn historical_capture_retained_directory_uses_content_addressed_nodes_without_writes() {
    use std::{
        fs,
        os::unix::fs::{symlink, PermissionsExt},
    };
    // This test owns one unique directory. It exports a complete synthetic
    // parent through the same raw-node interchange used by the public worker.
    let path = std::env::temp_dir().join(format!(
        "historical-capture-directory-{}",
        std::process::id()
    ));
    fs::create_dir(&path).expect("new test-owned directory");
    fs::set_permissions(&path, fs::Permissions::from_mode(0o700)).unwrap();
    struct OwnedDirectory(std::path::PathBuf);
    impl Drop for OwnedDirectory {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }
    let _owned = OwnedDirectory(path.clone());
    let job = job(&wasm("", READ), |_| {});
    for value in &job.proof_nodes_hex {
        let raw = hex_bytes("fixture node", value, MAXIMUM_CODE_BYTES).unwrap();
        fs::write(path.join(hex::encode(blake2_256(&raw))), raw).unwrap();
    }
    let root = fs::File::open(&path).unwrap();
    let raw = serde_json::to_vec(&request(&job)).unwrap();
    let report = capture::capture_historical_directory_json(&raw, &root).unwrap();
    let report: CaptureReport = serde_json::from_slice(&report).unwrap();
    assert!(report.replay.post_state_reproduced);
    let parent: NativeHeader = scale_exact(
        "fixture parent",
        &hex_bytes(
            "fixture parent",
            &job.parent_header_hex,
            MAXIMUM_HEADER_BYTES,
        )
        .unwrap(),
    )
    .unwrap();
    let node = path.join(hex::encode(parent.state_root().0));
    let original = fs::read(&node).unwrap();
    fs::write(&node, b"wrong content with an original hash name").unwrap();
    assert!(
        capture::capture_historical_directory_json(&raw, &root).is_err(),
        "changed node bytes became parent state"
    );
    fs::remove_file(&node).unwrap();
    assert!(
        capture::capture_historical_directory_json(&raw, &root).is_err(),
        "missing parent node became a proof"
    );
    let target = path.join("unaddressed-node");
    fs::write(&target, original).unwrap();
    symlink(&target, &node).unwrap();
    assert!(
        capture::capture_historical_directory_json(&raw, &root).is_err(),
        "node symlink was followed"
    );
}

#[path = "historical_node_profile_tests.rs"]
mod node_profile_tests;

#[path = "historical_capture_heap_tests.rs"]
mod heap_tests;

#[cfg(target_os = "linux")]
#[path = "historical_capture_refill_tests.rs"]
mod refill_tests;
