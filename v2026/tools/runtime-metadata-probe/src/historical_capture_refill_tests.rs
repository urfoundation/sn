//! Exercise the public directory/feed entry with one SDK-proved node per ack.
//! Complete maps construct the two roots independently. The broker never sends
//! a value or iterator end, and never dumps the complete parent proof.

use super::*;
use sp_state_machine::prove_child_read_on_trie_backend;
use std::{
    fs::{self, File, OpenOptions},
    io::{ErrorKind, Read, Write},
    os::{fd::OwnedFd, unix::fs::PermissionsExt, unix::net::UnixStream},
    path::PathBuf,
};

const SCHEMA: &str = "urnetwork-native-parent-trie-refill-v1";

#[derive(Clone, Debug, serde::Deserialize)]
#[serde(deny_unknown_fields)]
struct RefillRequest {
    schema: String,
    id: u64,
    request_sha256: String,
    parent_hash: String,
    parent_state_root: String,
    operation: String,
    child_storage_key: String,
    prefix_hex: String,
    prefix_nibble: Option<u8>,
    missing_hash: String,
}

struct OwnedDirectory(PathBuf);

impl OwnedDirectory {
    fn new() -> Self {
        static NEXT: AtomicUsize = AtomicUsize::new(0);
        let path = std::env::temp_dir().join(format!(
            "historical-refill-{}-{}",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&path).expect("new owned refill directory");
        fs::set_permissions(&path, fs::Permissions::from_mode(0o700)).unwrap();
        Self(path)
    }
}

impl Drop for OwnedDirectory {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

#[derive(Clone, Copy)]
enum Fault {
    None,
    AcknowledgeAbsent(&'static str),
    ForeignRequestAck,
}

fn parent_header(job: &HistoricalJob) -> NativeHeader {
    scale_exact(
        "refill fixture parent",
        &hex_bytes(
            "refill fixture parent",
            &job.parent_header_hex,
            MAXIMUM_HEADER_BYTES,
        )
        .unwrap(),
    )
    .unwrap()
}

fn native_job(code: &[u8], initial: Storage, change: impl FnOnce(&mut Storage)) -> HistoricalJob {
    let mut job = job(code, |_| {});
    let backing = TestExternalities::<Blake2Hasher>::new_with_code_and_state(
        code,
        initial.clone(),
        StateVersion::V1,
    );
    let (nodes, root) = backing.into_raw_snapshot();
    let mut parent = parent_header(&job);
    parent.set_state_root(root);
    job.parent_header_hex = encoded(&parent.encode());
    job.parent_hash = parent.hash().0;
    job.proof_nodes_hex = nodes
        .into_iter()
        .map(|(_, (value, _))| encoded(&value))
        .collect::<BTreeSet<_>>()
        .into_iter()
        .collect();
    let mut expected = initial;
    change(&mut expected);
    let backing = TestExternalities::<Blake2Hasher>::new_with_code_and_state(
        code,
        expected,
        StateVersion::V1,
    );
    replace_child(&mut job, |child| {
        child.set_parent_hash(parent.hash());
        child.set_state_root(*backing.backend.root());
    });
    let mut profile = observation_profile(code, "Core_execute_block", "native-drain");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    job.observation_profile = Some(profile);
    job
}

fn refill(
    job: &HistoricalJob,
    directory: &OwnedDirectory,
    fault: Fault,
) -> (Result<CaptureReport, ProbeError>, Vec<RefillRequest>, bool) {
    let raw = serde_json::to_vec(&request(job)).unwrap();
    let root = File::open(&directory.0).unwrap();
    std::thread::scope(|threads| {
        // Owned stream descriptors carry the same bounded frames as inherited
        // worker pipes. Dropping either side also releases its waiting peer.
        let (worker_request, mut broker_request) = UnixStream::pair().unwrap();
        let (worker_response, mut broker_response) = UnixStream::pair().unwrap();
        let worker_request = File::from(OwnedFd::from(worker_request));
        let worker_response = File::from(OwnedFd::from(worker_response));
        let raw_request = &raw;
        let broker = threads.spawn(move || {
            let parent = backend(job);
            let mut requests = Vec::new();
            let mut fault_hit = false;
            loop {
                let mut size = [0_u8; 4];
                match broker_request.read_exact(&mut size) {
                    Ok(()) => {}
                    Err(error) if error.kind() == ErrorKind::UnexpectedEof => break,
                    Err(error) => panic!("refill request length: {error}"),
                }
                let size = u32::from_be_bytes(size) as usize;
                assert!(size > 0 && size <= 16 * 1024);
                let mut bytes = vec![0; size];
                broker_request.read_exact(&mut bytes).unwrap();
                let request: RefillRequest = serde_json::from_slice(&bytes).unwrap();
                assert!(
                    requests.len() < 128,
                    "fixture refill failed to make progress"
                );
                assert_eq!(request.schema, SCHEMA);
                assert_eq!(request.id, requests.len() as u64 + 1);
                assert_eq!(
                    request.request_sha256,
                    format!("sha256:{}", hex::encode(sha2_256(raw_request)))
                );
                assert_eq!(request.parent_hash, encoded(&job.parent_hash));
                assert_eq!(request.parent_state_root, encoded(parent.root().as_bytes()));
                let mut probe = hex_bytes("refill prefix", &request.prefix_hex, 1024).unwrap();
                if let Some(nibble) = request.prefix_nibble {
                    assert_eq!(nibble & 0x0f, 0, "SDK odd prefix is not a high nibble");
                    probe.push(nibble);
                }
                let proof = if request.child_storage_key == "0x" {
                    prove_read_on_trie_backend(&parent, [&probe]).unwrap()
                } else {
                    let child = ChildInfo::new_default(OWNER);
                    assert_eq!(
                        request.child_storage_key,
                        encoded(child.prefixed_storage_key().as_slice())
                    );
                    prove_child_read_on_trie_backend(&parent, &child, [&probe]).unwrap()
                };
                // This is the archive RPC's actual SDK read-proof algorithm.
                // Membership must follow from the exact requested namespace and
                // prefix; the complete fixture node bag is never a fallback.
                let node = proof
                    .into_iter_nodes()
                    .find(|node| encoded(&blake2_256(node)) == request.missing_hash)
                    .unwrap_or_else(|| panic!("exact SDK proof omitted requested node: {request:?}"));
                let omit = matches!(fault, Fault::AcknowledgeAbsent(operation) if request.operation == operation);
                if !omit {
                    let path = directory.0.join(hex::encode(blake2_256(&node)));
                    let mut file = OpenOptions::new()
                        .write(true)
                        .create_new(true)
                        .open(path)
                        .unwrap();
                    file.write_all(&node).unwrap();
                }
                let mut response = serde_json::json!({
                    "schema": SCHEMA,
                    "id": request.id,
                    "request_sha256": request.request_sha256,
                    "missing_hash": request.missing_hash,
                    "retained": true,
                });
                if matches!(fault, Fault::ForeignRequestAck) {
                    response["request_sha256"] =
                        serde_json::json!(format!("sha256:{}", "00".repeat(32)));
                }
                let response = serde_json::to_vec(&response).unwrap();
                broker_response
                    .write_all(&(response.len() as u32).to_be_bytes())
                    .unwrap();
                broker_response.write_all(&response).unwrap();
                requests.push(request);
                if omit || matches!(fault, Fault::ForeignRequestAck) {
                    fault_hit = true;
                    break;
                }
            }
            (requests, fault_hit)
        });
        let result = capture::capture_historical_directory_feed_json(
            &raw,
            &root,
            Some((&worker_request, &worker_response)),
        )
        .map(|report| serde_json::from_slice(&report).expect("complete capture report"));
        drop(worker_request);
        drop(worker_response);
        let (requests, fault_hit) = broker.join().expect("SDK proof broker completed");
        (result, requests, fault_hit)
    })
}

fn assert_replay(job: &HistoricalJob, report: &CaptureReport) {
    assert!(report.replay.post_state_reproduced);
    assert_eq!(report.replay.child_hash, job.child_hash);
    let exported: HistoricalJob = serde_json::from_str(&report.job_json).unwrap();
    assert_eq!(exported.parent_hash, job.parent_hash);
    assert!(run(&exported).unwrap().post_state_reproduced);
}

#[test]
fn historical_capture_incremental_refill_proves_top_and_child_reads_and_writes() {
    let code = wasm("", &format!("{READ} (drop (call $child_get (i64.const 64424512512) (i64.const 4294970312))) {WRITE} {CHILD_WRITE}"));
    let job = native_job(&code, parent_storage(&code), |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
        storage
            .children_default
            .get_mut(OWNER)
            .unwrap()
            .data
            .insert(b"k".to_vec(), b"v".to_vec());
    });
    let (result, requests, fault) = refill(&job, &OwnedDirectory::new(), Fault::None);
    assert_replay(&job, &result.unwrap());
    assert!(!fault && requests.len() > 4);
    assert!(requests
        .iter()
        .any(|request| request.operation == "storage" && request.child_storage_key == "0x"));
    assert!(requests
        .iter()
        .any(|request| request.operation == "child-storage" && request.child_storage_key != "0x"));
}

#[test]
fn historical_capture_incremental_refill_deletion_proves_unread_top_and_child_siblings() {
    for child in [false, true] {
        let code = wasm(
            "",
            if child {
                "(call $child_clear (i64.const 64424512512) (i64.const 4294970312))"
            } else {
                "(call $clear (i64.const 51539609536))"
            },
        );
        let target: &[u8] = if child { b"k" } else { ACCOUNT };
        let sibling = [target, b"-unread"].concat();
        let mut initial = parent_storage(&code);
        if child {
            initial
                .children_default
                .get_mut(OWNER)
                .unwrap()
                .data
                .insert(sibling.clone(), vec![15; 96]);
        } else {
            initial.top.insert(sibling.clone(), vec![15; 96]);
        }
        let job = native_job(&code, initial, |storage| {
            if child {
                storage
                    .children_default
                    .get_mut(OWNER)
                    .unwrap()
                    .data
                    .remove(target);
            } else {
                storage.top.remove(target);
            }
        });
        let parent = backend(&job);
        let read_proof = |key: &[u8]| {
            let proof = if child {
                prove_child_read_on_trie_backend(&parent, &ChildInfo::new_default(OWNER), [key])
                    .unwrap()
            } else {
                prove_read_on_trie_backend(&parent, [key]).unwrap()
            };
            proof
                .into_iter_nodes()
                .map(|node| encoded(&blake2_256(&node)))
                .collect::<BTreeSet<_>>()
        };
        let target_nodes = read_proof(target);
        let sibling_nodes = read_proof(&sibling);
        let operation = if child {
            "child-storage-root"
        } else {
            "storage-root"
        };
        let (result, requests, fault) = refill(&job, &OwnedDirectory::new(), Fault::None);
        assert_replay(&job, &result.unwrap());
        assert!(!fault);
        assert!(
            requests.iter().any(|request| request.operation == operation
                && sibling_nodes.contains(&request.missing_hash)
                && !target_nodes.contains(&request.missing_hash)),
            "root collapse did not request an unread sibling: {requests:?}"
        );
    }
}

fn range_job(child: bool) -> HistoricalJob {
    let imports = format!(
        "{PREFIX_HOST} {KILL_HOST} {} {}",
        host_data(4096, &Option::<u32>::None.encode()),
        host_data(4200, b"range-")
    );
    let (function, key, count) = if child {
        ("kill", host_span(3072, OWNER.len()), 4)
    } else {
        ("prefix", host_span(4200, 6), 2)
    };
    let body = format!(
        r#"(local $result i32)
        (local.set $result (i32.wrap_i64 (call ${function} (i64.const {key}) (i64.const {}))))
        (if (i32.load8_u (local.get $result)) (then unreachable))
        (if (i32.ne (i32.load offset=1 (local.get $result)) (i32.const {count})) (then unreachable))"#,
        host_span(4096, 1)
    );
    let code = wasm(&imports, &body);
    let mut initial = parent_storage(&code);
    initial.top.insert(b"range-\x10".to_vec(), vec![11; 96]);
    initial.top.insert(b"range-\x20".to_vec(), vec![12; 96]);
    let children = &mut initial.children_default.get_mut(OWNER).unwrap().data;
    children.insert(b"\x10-a".to_vec(), vec![13; 96]);
    children.insert(b"\x20-b".to_vec(), vec![14; 96]);
    native_job(&code, initial, |storage| {
        if child {
            storage
                .children_default
                .get_mut(OWNER)
                .unwrap()
                .data
                .clear();
        } else {
            storage.top.retain(|key, _| !key.starts_with(b"range-"));
        }
    })
}

#[test]
fn historical_capture_incremental_refill_completes_top_and_child_ranges_with_odd_prefixes() {
    for child in [false, true] {
        let job = range_job(child);
        let (result, requests, fault) = refill(&job, &OwnedDirectory::new(), Fault::None);
        assert_replay(&job, &result.unwrap());
        assert!(!fault);
        assert!(requests
            .iter()
            .any(|request| request.operation.starts_with("iterator-")
                && (request.child_storage_key != "0x") == child));
        assert!(
            requests
                .iter()
                .any(|request| request.prefix_nibble.is_some()
                    && (request.child_storage_key != "0x") == child),
            "fixture did not exercise an odd trie prefix: {requests:?}"
        );
    }
}

#[test]
fn historical_capture_incremental_refill_missing_range_node_is_not_iterator_end() {
    for child in [false, true] {
        let mut job = range_job(child);
        assert!(run(&job).unwrap().post_state_reproduced);
        let root = *parent_header(&job).state_root();
        replace_child(&mut job, |header| header.set_state_root(root));
        let (result, requests, fault) = refill(
            &job,
            &OwnedDirectory::new(),
            Fault::AcknowledgeAbsent("iterator-key"),
        );
        assert!(fault && requests.last().unwrap().operation == "iterator-key");
        let error = result
            .err()
            .expect("acknowledged missing range node became iterator end");
        assert!(
            error.to_string().contains("acknowledged node read"),
            "{error}"
        );
    }
}

#[test]
fn historical_capture_incremental_refill_root_error_cannot_reuse_parent_root() {
    for child in [false, true] {
        let code = wasm("", if child { CHILD_WRITE } else { WRITE });
        let mut job = native_job(&code, parent_storage(&code), |storage| {
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
        assert!(run(&job).unwrap().post_state_reproduced);
        let root = *parent_header(&job).state_root();
        replace_child(&mut job, |header| header.set_state_root(root));
        let operation = if child {
            "child-storage-root"
        } else {
            "storage-root"
        };
        let (result, requests, fault) = refill(
            &job,
            &OwnedDirectory::new(),
            Fault::AcknowledgeAbsent(operation),
        );
        assert!(fault && requests.last().unwrap().operation == operation);
        let error = result
            .err()
            .expect("SDK root error became a reproduced parent root");
        assert!(
            error.to_string().contains("acknowledged node read"),
            "{error}"
        );
    }
}

#[test]
fn historical_capture_incremental_refill_foreign_ack_refuses_then_exact_restart_reuses_nodes() {
    let code = wasm("", READ);
    let job = native_job(&code, parent_storage(&code), |_| {});
    let directory = OwnedDirectory::new();
    let (refused, first, fault) = refill(&job, &directory, Fault::ForeignRequestAck);
    assert!(fault && first.len() == 1);
    let error = refused
        .err()
        .expect("foreign request acknowledgement accepted");
    assert!(
        error.to_string().contains("acknowledgement differs"),
        "{error}"
    );
    let (result, resumed, fault) = refill(&job, &directory, Fault::None);
    assert_replay(&job, &result.unwrap());
    assert!(!fault && !resumed.is_empty());
    assert!(resumed
        .iter()
        .all(|request| request.request_sha256 == first[0].request_sha256
            && request.missing_hash != first[0].missing_hash));
}
