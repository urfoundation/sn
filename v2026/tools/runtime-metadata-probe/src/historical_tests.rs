//! Synthetic programs exercise the public decoder and exact pinned SDK hosts.
//! Parent proofs contain every fixture trie node; expected post-state roots
//! are independently built from complete maps, never copied from replay output.
//! These programs are not actual-runtime admission or fee-hook witnesses.

use super::*;
use sp_core::{
    storage::{ChildInfo, Storage, StorageChild},
    Pair, H256,
};
use sp_runtime::generic::{Digest, DigestItem};
use sp_state_machine::{prove_read_on_trie_backend, TestExternalities};
use std::{borrow::Cow, collections::BTreeMap};

#[path = "historical_capture_tests.rs"]
mod capture_tests;

#[path = "historical_extrinsics_root_tests.rs"]
mod extrinsics_root_tests;

#[path = "historical_global_alias_tests.rs"]
mod global_alias_tests;

#[path = "historical_host_snapshot_tests.rs"]
mod host_snapshot_tests;

#[path = "historical_epoch_layout_tests.rs"]
mod epoch_layout_tests;

#[path = "historical_metadata_scope_tests.rs"]
mod metadata_scope_tests;
#[path = "historical_recipient_layout_tests.rs"]
mod recipient_layout_tests;
#[path = "historical_storage_call_tests.rs"]
mod storage_call_tests;

#[path = "historical_native_execution_tests.rs"]
mod native_execution_tests;

const OWNER: &[u8] = b"synthetic-child";
const ACCOUNT: &[u8] = b"zzzz-account";
const READ: &str = "(drop (call $get (i64.const 51539609536)))";
const WRITE: &str = "(call $set (i64.const 51539609536) (i64.const 4294970400))";
const CHILD_WRITE: &str =
    "(call $child_set (i64.const 64424512512) (i64.const 4294970312) (i64.const 4294970400))";

/// Offsets are fixed fixture data. The normal program modifies one complete
/// state; no expected-root value or verifier branch is imported by the Wasm.
fn wasm(imports: &str, body: &str) -> Vec<u8> {
    wasm_with_heap(imports, body, 8192)
}

fn wasm_with_heap(imports: &str, body: &str, heap: u32) -> Vec<u8> {
    wasm_with_version(imports, body, heap, 1)
}

fn wasm_with_version(imports: &str, body: &str, heap: u32, system_version: u8) -> Vec<u8> {
    let version = RuntimeVersion {
        spec_name: Cow::Borrowed("synthetic-historical-runtime"),
        spec_version: 1,
        apis: Cow::Owned(vec![(sp_core::hashing::blake2_64(b"Core"), 4)]),
        transaction_version: 1,
        system_version,
        ..RuntimeVersion::default()
    }
    .encode();
    // The pinned SDK selects the SCALE layout from the advertised Core API.
    // Check the fixture before it enters the production replay decoder.
    let mut encoded_version = version.as_slice();
    let decoded_version = RuntimeVersion::decode(&mut encoded_version)
        .expect("synthetic Core4 runtime version decodes");
    assert!(encoded_version.is_empty());
    assert_eq!(decoded_version.encode(), version);
    let escaped = version
        .iter()
        .map(|byte| format!("\\{byte:02x}"))
        .collect::<String>();
    wat::parse_str(format!(r#"(module
        (import "env" "ext_storage_get_version_1" (func $get (param i64) (result i64)))
        (import "env" "ext_storage_set_version_1" (func $set (param i64 i64)))
        (import "env" "ext_storage_clear_version_1" (func $clear (param i64)))
        (import "env" "ext_storage_root_version_2" (func $root (param i32) (result i64)))
        (import "env" "ext_storage_start_transaction_version_1" (func $begin))
        (import "env" "ext_storage_commit_transaction_version_1" (func $commit))
        (import "env" "ext_storage_rollback_transaction_version_1" (func $rollback))
        (import "env" "ext_default_child_storage_get_version_1" (func $child_get (param i64 i64) (result i64)))
        (import "env" "ext_default_child_storage_set_version_1" (func $child_set (param i64 i64 i64)))
        (import "env" "ext_default_child_storage_clear_version_1" (func $child_clear (param i64 i64)))
        {imports}
        (memory (export "memory") 8)
        (global (export "__heap_base") i32 (i32.const {heap}))
        (data (i32.const 32) "{escaped}")
        (data (i32.const 1984) "zzzz-account")
        (data (i32.const 3072) "synthetic-child")
        (data (i32.const 3016) "k")
        (data (i32.const 3104) "v")
        (func (export "Core_version") (param i32 i32) (result i64) (i64.const {version_span}))
        (func (export "Core_execute_block") (param i32 i32) (result i64)
            (local $n i32) {body} (i64.const 0)))"#,
        version_span = (version.len() as u64) << 32 | 32
    )).expect("synthetic Wasm fixture compiles")
}

fn encoded(raw: &[u8]) -> String {
    format!("0x{}", hex::encode(raw))
}

fn parent_storage(code: &[u8]) -> Storage {
    let mut storage = Storage::default();
    storage
        .top
        .insert(well_known_keys::CODE.to_vec(), code.to_vec());
    storage.top.insert(ACCOUNT.to_vec(), vec![7; 96]);
    storage
        .top
        .insert(b"unrelated-original".to_vec(), vec![8; 96]);
    storage.children_default.insert(
        OWNER.to_vec(),
        StorageChild {
            child_info: ChildInfo::new_default(OWNER),
            data: [
                (b"k".to_vec(), vec![9; 96]),
                (b"other".to_vec(), vec![10; 96]),
            ]
            .into_iter()
            .collect(),
        },
    );
    storage
}

/// Complete raw snapshots retain all top-level, child, and hashed-value nodes.
/// The child expected map uses the SDK's full-state builder independently.
fn job(code: &[u8], change: impl FnOnce(&mut Storage)) -> HistoricalJob {
    job_with_storage(code, parent_storage(code), change)
}

fn job_with_storage(
    code: &[u8],
    initial: Storage,
    change: impl FnOnce(&mut Storage),
) -> HistoricalJob {
    let backing = TestExternalities::<Blake2Hasher>::new_with_code_and_state(
        code,
        initial.clone(),
        StateVersion::V1,
    );
    let (nodes, parent_root) = backing.into_raw_snapshot();
    let parent = NativeHeader::new(
        4,
        H256::repeat_byte(2),
        parent_root,
        H256::repeat_byte(1),
        Digest::default(),
    );
    let mut expected = initial;
    change(&mut expected);
    let backing = TestExternalities::<Blake2Hasher>::new_with_code_and_state(
        code,
        expected,
        StateVersion::V1,
    );
    let body = vec![vec![1u8, 2, 3].encode()];
    let child = NativeHeader::new(
        5,
        BlakeTwo256::ordered_trie_root(body.clone(), StateVersion::V0),
        *backing.backend.root(),
        parent.hash(),
        Digest::default(),
    );
    HistoricalJob {
        schema: HISTORICAL_SCHEMA.to_owned(),
        parent_header_hex: encoded(&parent.encode()),
        parent_hash: parent.hash().0,
        child_header_hex: encoded(&child.encode()),
        child_hash: child.hash().0,
        extrinsics_hex: body.iter().map(|bytes| encoded(bytes)).collect(),
        runtime_code_hex: encoded(code),
        runtime_code_sha256: sha2_256(code),
        runtime_code_blake2b_256: blake2_256(code),
        execution_state_version: 1,
        proof_nodes_hex: nodes
            .into_iter()
            .map(|(_, (value, _))| encoded(&value))
            .collect::<BTreeSet<_>>()
            .into_iter()
            .collect(),
        observation_profile: None,
        principal_queries: None,
        principal_effects: false,
    }
}

fn run(job: &HistoricalJob) -> Result<HistoricalReport, ProbeError> {
    let raw = replay_historical_json(&serde_json::to_vec(job).unwrap())?;
    Ok(serde_json::from_slice(&raw).expect("complete report decodes"))
}

fn replace_child(job: &mut HistoricalJob, mutate: impl FnOnce(&mut NativeHeader)) {
    let mut child: NativeHeader = scale_exact(
        "fixture child",
        &hex_bytes("fixture child", &job.child_header_hex, MAXIMUM_HEADER_BYTES).unwrap(),
    )
    .unwrap();
    mutate(&mut child);
    job.child_header_hex = encoded(&child.encode());
    job.child_hash = child.hash().0;
}

fn top_only_proof(job: &mut HistoricalJob, include_account: bool) {
    let code = hex_bytes("fixture code", &job.runtime_code_hex, MAXIMUM_CODE_BYTES).unwrap();
    let backing = TestExternalities::<Blake2Hasher>::new_with_code_and_state(
        &code,
        parent_storage(&code),
        StateVersion::V1,
    );
    let child = ChildInfo::new_default(OWNER);
    let mut keys = vec![
        well_known_keys::CODE.to_vec(),
        well_known_keys::HEAP_PAGES.to_vec(),
        child.prefixed_storage_key().as_slice().to_vec(),
    ];
    if include_account {
        keys.push(ACCOUNT.to_vec());
    }
    let proof = prove_read_on_trie_backend(&backing.backend, keys).unwrap();
    job.proof_nodes_hex = proof.into_iter_nodes().map(|node| encoded(&node)).collect();
}

/// Test-only callsite maps come from the unmodified program's export and code
/// sections. A label is deliberately not an actual-runtime semantic review.
fn observation_profile(code: &[u8], export: &str, purpose: &str) -> observer::ObservationProfile {
    let mut index = None;
    let mut imported = 0;
    let mut bodies = BTreeMap::new();
    for payload in wasmparser::Parser::new(0).parse_all(code) {
        match payload.unwrap() {
            wasmparser::Payload::ImportSection(section) => {
                for import in section {
                    if matches!(import.unwrap().ty, wasmparser::TypeRef::Func(_)) {
                        imported += 1;
                    }
                }
            }
            wasmparser::Payload::ExportSection(section) => {
                for entry in section {
                    let entry = entry.unwrap();
                    if entry.name == export {
                        assert_eq!(entry.kind, wasmparser::ExternalKind::Func);
                        assert!(index.replace(entry.index).is_none());
                    }
                }
            }
            wasmparser::Payload::CodeSectionEntry(body) => {
                bodies.insert(imported, code[body.range()].to_vec());
                imported += 1;
            }
            _ => {}
        }
    }
    let index = index.expect("synthetic observer export");
    let body = &bodies[&index];
    observer::ObservationProfile {
        schema: "urnetwork-original-wasm-hook-observation-v1".to_owned(),
        runtime_code_sha256: sha2_256(code),
        source_review_sha256: [71; 32],
        rules: vec![observer::HookRule {
            purpose: purpose.to_owned(),
            function_index: index,
            function_body_sha256: sha2_256(body),
            offset_start: 0,
            offset_end: body.len() as u32,
            memory: Vec::new(),
            host_snapshot: None,
            state_reads: Vec::new(),
            storage_call: None,
            recipient_owner: false,
        }],
        metadata_sha256: None,
        principal_storage_prefixes: None,
        original_globals: Vec::new(),
        epoch_layout: None,
        recipient_layout: None,
    }
}

#[test]
fn historical_original_wasm_stack_observes_real_host_calls_without_fee_authority() {
    let code = wasm("", &format!("{READ} {WRITE}"));
    let profile = observation_profile(&code, "Core_execute_block", "fee-withdraw");
    let function = profile.rules[0].function_index;
    let mut job = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
    });
    let original_code = job.runtime_code_hex.clone();
    job.observation_profile = Some(profile);
    let report = run(&job).expect("complete original-code host observation refused");
    assert_eq!(job.runtime_code_hex, original_code);
    let trace = report
        .hook_observations
        .expect("original stack trace absent");
    assert!(trace.original_function_bodies_preserved);
    assert_eq!(
        trace.authority,
        "caller-supplied-unapproved-callsite-profile"
    );
    assert_eq!(trace.observations.len(), 2);
    assert_eq!(trace.observations[0].operation, "get");
    assert_eq!(trace.observations[1].operation, "set");
    assert_eq!(trace.observations[1].value_hex.as_deref(), Some("0x76"));
    for event in trace.observations {
        assert_eq!(event.purpose, "fee-withdraw");
        assert_eq!(event.key_hex, encoded(ACCOUNT));
        assert!(event
            .stack
            .iter()
            .any(|frame| frame.function_index == function && frame.function_offset > 0));
    }
    assert_eq!(report.native_fee_debit, None);
    assert!(!report.native_fee_withdrawal_refund_observed && !report.runtime_admitted);
}

#[test]
fn historical_native_captures_actual_returns_and_original_memory() {
    let code = wasm("", &format!("(i64.store (i32.const 4000) (i64.mul (i64.const 7) (i64.const 11))) {READ} {WRITE} {READ} (call $clear (i64.const 51539609536)) {READ}"));
    let mut job = job(&code, |storage| {
        storage.top.remove(ACCOUNT);
    });
    let mut profile = observation_profile(&code, "Core_execute_block", "native-drain");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    profile.rules[0].memory = vec![observer::MemoryCapture {
        name: "computed".to_owned(),
        address: 4000,
        global: None,
        dereference_offsets: Vec::new(),
        bytes: 8,
        repeat: None,
    }];
    job.observation_profile = Some(profile);
    let report = run(&job).expect("native original-memory proof refused");
    assert!(report.post_state_reproduced && !report.runtime_admitted);
    let records = report.hook_observations.unwrap().observations;
    assert_eq!(records.len(), 5);
    assert_eq!(
        records[0]
            .storage_return
            .as_ref()
            .unwrap()
            .value_hex
            .as_deref(),
        Some(encoded(&vec![7; 96]).as_str())
    );
    assert_eq!(
        records[2]
            .storage_return
            .as_ref()
            .unwrap()
            .value_hex
            .as_deref(),
        Some("0x76")
    );
    let absent = records[4].storage_return.as_ref().unwrap();
    assert!(!absent.present && absent.value_hex.is_none());
    for record in records {
        let native = record.native.expect("native capture lost original memory");
        // Missing phase is retained, never promoted into Initialization.
        assert!(native.execution_phase_hex.is_none());
        assert_eq!(native.memory[0].bytes_hex, "0x4d00000000000000");
    }
}

#[test]
fn historical_native_read_retains_full_value_and_slice_provenance() {
    let code = wasm("(import \"env\" \"ext_storage_read_version_1\" (func $read (param i64 i64 i32) (result i64)))", "(drop (call $read (i64.const 51539609536) (i64.const 12884905888) (i32.const 5)))");
    let mut job = job(&code, |_| {});
    let mut profile = observation_profile(&code, "Core_execute_block", "native-drain");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    job.observation_profile = Some(profile);
    let record = run(&job)
        .unwrap()
        .hook_observations
        .unwrap()
        .observations
        .remove(0);
    let returned = record.storage_return.unwrap();
    assert!(returned.present);
    assert_eq!(returned.offset, Some(5));
    assert_eq!(returned.output_length, Some(3));
    assert_eq!(returned.value_hex, Some(encoded(&vec![7; 96])));
}

#[test]
fn historical_native_capture_refuses_outside_memory_and_missing_global() {
    let code = wasm("", READ);
    for global in [None, Some("not_an_original_global".to_owned())] {
        let mut job = job(&code, |_| {});
        let mut profile = observation_profile(&code, "Core_execute_block", "native-drain");
        profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
        profile.rules[0].memory = vec![observer::MemoryCapture {
            name: "tranche".to_owned(),
            address: u32::MAX,
            global,
            dereference_offsets: Vec::new(),
            bytes: 8,
            repeat: None,
        }];
        job.observation_profile = Some(profile);
        assert!(
            run(&job).is_err(),
            "unobserved memory acquired proof authority"
        );
    }
}

#[test]
fn historical_native_rollback_discards_return_and_memory_together() {
    let code = wasm(
        "",
        &format!("(call $begin) {WRITE} {READ} (call $rollback) {READ}"),
    );
    let mut job = job(&code, |_| {});
    let mut profile = observation_profile(&code, "Core_execute_block", "native-drain");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    job.observation_profile = Some(profile);
    let trace = run(&job).unwrap().hook_observations.unwrap();
    assert_eq!(trace.discarded_on_rollback, 2);
    assert_eq!(trace.observations.len(), 1);
    assert_eq!(trace.observations[0].ordinal, 3);
    assert_eq!(
        trace.observations[0]
            .storage_return
            .as_ref()
            .unwrap()
            .value_hex,
        Some(encoded(&vec![7; 96]))
    );
}

#[test]
fn historical_native_repeated_capture_uses_original_count_and_tuple_stride() {
    let code = wasm("", &format!("(i32.store (i32.const 4000) (i32.const 2)) (i32.store16 (i32.const 4016) (i32.const 17)) (i32.store16 (i32.const 4020) (i32.const 29)) (i32.store16 (i32.const 4024) (i32.const 43)) {READ} (i32.store (i32.const 4000) (i32.const 3)) {READ} (i32.store (i32.const 4000) (i32.const 0)) {READ}"));
    let mut job = job(&code, |_| {});
    let mut profile = observation_profile(&code, "Core_execute_block", "native-epoch");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    profile.rules[0].memory = vec![observer::MemoryCapture {
        name: "uids".to_owned(),
        address: 4016,
        global: None,
        dereference_offsets: Vec::new(),
        bytes: 2,
        repeat: Some(observer::MemoryRepeat {
            count: observer::MemoryPointer {
                address: 4000,
                global: None,
                dereference_offsets: Vec::new(),
            },
            maximum: 4096,
            stride: 4,
        }),
    }];
    job.observation_profile = Some(profile);
    let report = run(&job).expect("original tuple/count capture refused");
    assert!(report.post_state_reproduced && !report.runtime_admitted);
    let records = report.hook_observations.unwrap().observations;
    assert_eq!(records.len(), 3);
    for (index, (count, bytes)) in [(2, "0x11001d00"), (3, "0x11001d002b00"), (0, "0x")]
        .into_iter()
        .enumerate()
    {
        let capture = &records[index].native.as_ref().unwrap().memory[0];
        assert_eq!(capture.element_count, Some(count));
        assert_eq!(capture.bytes_hex, bytes);
    }
}

#[test]
fn historical_native_vector_accepts_4096_accounts_and_refuses_actual_overflow() {
    for count in [4096, 4097] {
        let code = wasm(
            "",
            &format!("(i32.store (i32.const 4000) (i32.const {count})) {READ}"),
        );
        let mut job = job(&code, |_| {});
        let mut profile = observation_profile(&code, "Core_execute_block", "native-epoch");
        profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
        profile.rules[0].memory = vec![observer::MemoryCapture {
            name: "hotkeys".to_owned(),
            address: 65536,
            global: None,
            dereference_offsets: Vec::new(),
            bytes: 32,
            repeat: Some(observer::MemoryRepeat {
                count: observer::MemoryPointer {
                    address: 4000,
                    global: None,
                    dereference_offsets: Vec::new(),
                },
                maximum: 4096,
                stride: 32,
            }),
        }];
        job.observation_profile = Some(profile);
        let result = run(&job);
        if count == 4097 {
            assert!(result.is_err(), "actual overbound count was narrowed");
        } else {
            let report = result.expect("accepted full UID profile was narrowed");
            let records = report.hook_observations.unwrap().observations;
            let capture = &records[0].native.as_ref().unwrap().memory[0];
            assert_eq!(capture.element_count, Some(4096));
            assert_eq!(capture.bytes_hex.len(), 2 + 4096 * 32 * 2);
        }
    }
}

#[test]
fn historical_original_wasm_observer_refuses_relabelled_body_code_and_range() {
    let code = wasm("", READ);
    let profile = observation_profile(&code, "Core_execute_block", "fee-withdraw");
    for variant in 0..5 {
        let mut job = job(&code, |_| {});
        let mut profile = profile.clone();
        match variant {
            0 => profile.runtime_code_sha256[0] ^= 1,
            1 => profile.rules[0].function_body_sha256[0] ^= 1,
            2 => profile.rules[0].offset_end = u32::MAX,
            3 => profile.rules[0].purpose = "unreviewed-purpose".to_owned(),
            4 => profile.rules.push(profile.rules[0].clone()),
            _ => unreachable!(),
        }
        job.observation_profile = Some(profile);
        let error = run(&job).expect_err("substituted observer identity was accepted");
        assert!(error.to_string().contains("observer"), "{variant}: {error}");
    }
}

#[test]
fn historical_original_wasm_observer_rollback_discards_effect_not_work() {
    let code = wasm(
        "",
        &format!("(call $begin) {WRITE} (call $rollback) {READ}"),
    );
    let mut job = job(&code, |_| {});
    job.observation_profile = Some(observation_profile(
        &code,
        "Core_execute_block",
        "fee-refund",
    ));
    let report = run(&job).expect("complete rollback observation refused");
    let trace = report.hook_observations.unwrap();
    assert_eq!(trace.host_calls, 2);
    assert_eq!(trace.discarded_on_rollback, 1);
    assert_eq!(trace.observations.len(), 1);
    assert_eq!(trace.observations[0].ordinal, 2);
    assert_eq!(trace.observations[0].operation, "get");
    assert_eq!(report.parent_state_root, report.child_state_root);
    assert!(!report.native_fee_withdrawal_refund_observed);
}

#[test]
fn historical_original_wasm_nested_ambiguous_attribution_refuses_complete_block() {
    let code = wasm(
        &format!("(func $nested (export \"nested\") {READ})"),
        "(call $nested)",
    );
    let mut profile = observation_profile(&code, "Core_execute_block", "fee-withdraw");
    profile
        .rules
        .extend(observation_profile(&code, "nested", "fee-refund").rules);
    let mut job = job(&code, |_| {});
    job.observation_profile = Some(profile);
    let error = run(&job).expect_err("two active fee purposes became one fee fact");
    assert!(
        error.to_string().contains("execute block refused"),
        "{error}"
    );
}

#[test]
fn historical_original_wasm_unobserved_calls_and_zero_events_remain_unknown() {
    let code = wasm("", READ);
    let mut job = job(&code, |_| {});
    job.observation_profile = Some(observation_profile(&code, "Core_version", "fee-refund"));
    let report = run(&job).expect("unmatched original function refused");
    let trace = report.hook_observations.unwrap();
    assert_eq!(trace.host_calls, 1);
    assert!(trace.observations.is_empty());
    assert_eq!(report.native_fee_debit, None);
    assert!(!report.native_fee_withdrawal_refund_observed);
}

#[test]
fn historical_original_wasm_observer_cumulative_bound_survives_rollback() {
    let code = wasm("", &format!("(loop $again (call $begin) {WRITE} (call $rollback) (local.set $n (i32.add (local.get $n) (i32.const 1))) (br_if $again (i32.lt_u (local.get $n) (i32.const 4097))))"));
    let mut job = job(&code, |_| {});
    job.observation_profile = Some(observation_profile(
        &code,
        "Core_execute_block",
        "fee-withdraw",
    ));
    let error = run(&job).expect_err("rolled-back trace bypassed cumulative work bound");
    assert!(
        error.to_string().contains("execute block refused"),
        "{error}"
    );
}

#[test]
fn historical_original_wasm_observer_never_publishes_incomplete_poststate() {
    let code = wasm("", WRITE);
    let mut job = job(&code, |_| {});
    job.observation_profile = Some(observation_profile(
        &code,
        "Core_execute_block",
        "fee-withdraw",
    ));
    let error = run(&job).expect_err("host trace escaped a changed child root");
    assert!(
        error
            .to_string()
            .contains("reproduced child state root differs"),
        "{error}"
    );
}

#[derive(Encode, scale_info::TypeInfo)]
enum SyntheticBalanceEvents {
    #[codec(index = 7)]
    Withdraw { who: [u8; 32], amount: u64 },
    #[codec(index = 5)]
    Deposit { who: [u8; 32], amount: u64 },
}

#[derive(Encode, scale_info::TypeInfo)]
enum SyntheticExitReason {
    Succeed,
    Revert,
}

#[derive(Encode, scale_info::TypeInfo)]
enum SyntheticEthereumEvents {
    #[codec(index = 3)]
    Executed {
        from: [u8; 20],
        to: [u8; 20],
        transaction_hash: [u8; 32],
        exit_reason: SyntheticExitReason,
        extra_data: Vec<u8>,
    },
}

/// This metadata is embedded in and returned by the same synthetic Wasm;
/// production has no caller-provided replacement-metadata input.
fn fee_metadata() -> Vec<u8> {
    use frame_metadata::v14::{
        ExtrinsicMetadata, PalletEventMetadata, PalletMetadata, RuntimeMetadataV14,
    };
    let balance = PalletMetadata {
        name: "Balances",
        storage: None,
        calls: None,
        event: Some(PalletEventMetadata {
            ty: scale_info::meta_type::<SyntheticBalanceEvents>(),
        }),
        constants: vec![],
        error: None,
        index: 5,
    };
    let ethereum = PalletMetadata {
        name: "Ethereum",
        storage: None,
        calls: None,
        event: Some(PalletEventMetadata {
            ty: scale_info::meta_type::<SyntheticEthereumEvents>(),
        }),
        constants: vec![],
        error: None,
        index: 18,
    };
    let metadata = RuntimeMetadataV14::new(
        vec![balance, ethereum],
        ExtrinsicMetadata {
            ty: scale_info::meta_type::<()>(),
            version: 4,
            signed_extensions: vec![],
        },
        scale_info::meta_type::<()>(),
    );
    frame_metadata::RuntimeMetadataPrefixed::from(metadata).encode()
}

fn fee_record(index: u32, pallet: u8, event: &[u8]) -> Vec<u8> {
    let mut raw = vec![0];
    raw.extend(index.to_le_bytes());
    raw.push(pallet);
    raw.extend(event);
    raw.push(0); // canonical empty topic vector
    raw
}

/// Each balance event is generated inside a separate original-Wasm function.
/// An unrelated same-payer deposit is optionally executed outside those exact
/// callsite ranges, reproducing the precompile-attribution ambiguity.
fn fee_job(refund: Option<u64>, foreign_payer: bool, unrelated_deposit: bool) -> HistoricalJob {
    fee_job_changed(refund, foreign_payer, unrelated_deposit, |_, _| {})
}

fn fee_job_changed(
    refund: Option<u64>,
    foreign_payer: bool,
    unrelated_deposit: bool,
    change: impl FnOnce(&mut Vec<(Option<&'static str>, Vec<u8>)>, &mut Vec<u8>),
) -> HistoricalJob {
    let source = [29u8; 20];
    let mut mapping = b"evm:".to_vec();
    mapping.extend(source);
    let payer = blake2_256(&mapping);
    let observed_payer = if foreign_payer { [99; 32] } else { payer };
    let mut events = vec![(
        Some("fee-withdraw"),
        fee_record(
            0,
            5,
            &SyntheticBalanceEvents::Withdraw {
                who: observed_payer,
                amount: 1000,
            }
            .encode(),
        ),
    )];
    if unrelated_deposit {
        events.push((
            None,
            fee_record(
                0,
                5,
                &SyntheticBalanceEvents::Deposit {
                    who: payer,
                    amount: 9000,
                }
                .encode(),
            ),
        ));
    }
    if let Some(amount) = refund {
        events.push((
            Some("fee-refund"),
            fee_record(
                0,
                5,
                &SyntheticBalanceEvents::Deposit { who: payer, amount }.encode(),
            ),
        ));
    }
    events.push((
        Some("ethereum-executed"),
        fee_record(
            0,
            18,
            &SyntheticEthereumEvents::Executed {
                from: source,
                to: [31; 20],
                transaction_hash: [41; 32],
                exit_reason: SyntheticExitReason::Revert,
                extra_data: vec![1, 2],
            }
            .encode(),
        ),
    ));
    let mut metadata = fee_metadata();
    change(&mut events, &mut metadata);
    let metadata_opaque = metadata.encode();
    let escape = |bytes: &[u8]| {
        bytes
            .iter()
            .map(|byte| format!("\\{byte:02x}"))
            .collect::<String>()
    };
    let mut key = sp_core::hashing::twox_128(b"System").to_vec();
    key.extend(sp_core::hashing::twox_128(b"Events"));
    let mut imports = format!(
        r#"(import "env" "ext_storage_append_version_1" (func $append (param i64 i64)))
        (data (i32.const 4500) "{}")
        (data (i32.const 40000) "{}")
        (func (export "Metadata_metadata") (param i32 i32) (result i64) (i64.const {}))"#,
        escape(&key),
        escape(&metadata_opaque),
        ((metadata_opaque.len() as u64) << 32) | 40000
    );
    let mut body = String::new();
    for (index, (_, event)) in events.iter().enumerate() {
        let offset = 5000 + index * 256;
        imports.push_str(&format!(r#"(data (i32.const {offset}) "{}")
            (func $event{index} (export "event{index}") (call $append (i64.const {}) (i64.const {})))"#,
            escape(event), (32u64 << 32) | 4500, ((event.len() as u64) << 32) | offset as u64));
        body.push_str(&format!("(call $event{index})"));
    }
    let code = wasm(&imports, &body);
    let mut final_events = parity_scale_codec::Compact(events.len() as u32).encode();
    for (_, event) in &events {
        final_events.extend(event);
    }
    let mut job = job(&code, |storage| {
        storage.top.insert(key, final_events);
    });
    let mut profile = observation_profile(&code, "event0", "fee-withdraw");
    profile.rules.clear();
    for (index, (purpose, _)) in events.iter().enumerate() {
        if let Some(purpose) = purpose {
            profile
                .rules
                .extend(observation_profile(&code, &format!("event{index}"), purpose).rules);
        }
    }
    profile.metadata_sha256 = Some(sha2_256(&metadata));
    job.observation_profile = Some(profile);
    job
}

#[test]
fn historical_fee_original_metadata_separates_actual_refund_from_same_phase_effects() {
    let job = fee_job(Some(250), false, true);
    let report = run(&job).expect("original metadata and exact callsite event pair refused");
    let trace = report.hook_observations.unwrap();
    let events = trace.fee_events.unwrap();
    assert_eq!(events.events.len(), 3); // the unlabelled 9000 deposit is not gas
    assert_eq!(events.candidates.len(), 1);
    assert_eq!(events.unmatched_fee_events, 0);
    let candidate = &events.candidates[0];
    assert_eq!(candidate.withdrawal_rao.as_deref(), Some("1000"));
    assert_eq!(candidate.refund_rao.as_deref(), Some("250"));
    assert_eq!(candidate.debit_rao.as_deref(), Some("750"));
    assert_eq!(candidate.extrinsic_index, Some(0));
    assert_eq!(candidate.transaction_hash, [41; 32]);
    assert_eq!(candidate.status, "observed-pair-unadmitted");
    assert_eq!(report.native_fee_debit, None);
    assert!(!report.runtime_admitted && !report.native_fee_withdrawal_refund_observed);
}

#[test]
fn historical_fee_missing_refund_never_relabels_prior_deposit_or_zero() {
    let job = fee_job(None, false, true);
    let report = run(&job).expect("missing refund must retain unresolved evidence");
    let events = report.hook_observations.unwrap().fee_events.unwrap();
    assert_eq!(events.candidates.len(), 1);
    let candidate = &events.candidates[0];
    assert_eq!(candidate.withdrawal_rao.as_deref(), Some("1000"));
    assert_eq!(candidate.refund_rao, None);
    assert_eq!(candidate.debit_rao, None);
    assert_eq!(candidate.status, "refund-unobserved");
}

#[test]
fn historical_fee_observed_zero_refund_differs_from_missing() {
    let job = fee_job(Some(0), false, false);
    let report = run(&job).expect("actual zero deposit event refused");
    let events = report.hook_observations.unwrap().fee_events.unwrap();
    assert_eq!(events.candidates[0].refund_rao.as_deref(), Some("0"));
    assert_eq!(events.candidates[0].debit_rao.as_deref(), Some("1000"));
}

#[test]
fn historical_fee_foreign_payer_and_excess_refund_refuse_attribution() {
    for job in [
        fee_job(Some(250), true, false),
        fee_job(Some(1001), false, false),
    ] {
        let error = run(&job).expect_err("foreign or impossible fee event became expenditure");
        assert!(
            error.to_string().contains("payer differs")
                || error.to_string().contains("refund exceeds"),
            "{error}"
        );
    }
}

#[test]
fn historical_fee_runtime_metadata_pin_cannot_be_substituted() {
    let mut job = fee_job(Some(250), false, false);
    job.observation_profile
        .as_mut()
        .unwrap()
        .metadata_sha256
        .as_mut()
        .unwrap()[0] ^= 1;
    let error = run(&job).expect_err("different runtime metadata was admitted");
    assert!(
        error.to_string().contains("runtime-generated metadata pin"),
        "{error}"
    );
}

#[test]
fn historical_fee_exact_metadata_rejects_changed_amount_units_and_duplicate_pallets() {
    for fault in ["amount-width", "pallet-index", "event-name"] {
        let job = fee_job_changed(Some(250), false, false, |_, raw| {
            let mut metadata: frame_metadata::RuntimeMetadataPrefixed =
                scale_exact("fixture metadata", raw).unwrap();
            let frame_metadata::RuntimeMetadata::V14(ref mut value) = metadata.1 else {
                panic!("fixture version")
            };
            match fault {
                "amount-width" => {
                    let balance_type = value.pallets[0].event.as_ref().unwrap().ty.id;
                    let scale_info::TypeDef::Variant(events) =
                        &value.types.resolve(balance_type).unwrap().type_def
                    else {
                        panic!("fixture event type")
                    };
                    let amount_type = events.variants[0].fields[1].ty.id;
                    value.types.types[amount_type as usize].ty.type_def =
                        scale_info::TypeDef::Primitive(scale_info::TypeDefPrimitive::U128);
                }
                "pallet-index" => value.pallets[1].index = value.pallets[0].index,
                "event-name" => {
                    let balance_type = value.pallets[0].event.as_ref().unwrap().ty.id;
                    let scale_info::TypeDef::Variant(events) =
                        &mut value.types.types[balance_type as usize].ty.type_def
                    else {
                        panic!("fixture event type")
                    };
                    events.variants[0].fields[1].name = Some("requested_amount".to_owned());
                }
                _ => unreachable!(),
            }
            *raw = metadata.encode();
        });
        let error =
            run(&job).expect_err("original runtime emitted an unsupported or ambiguous fee layout");
        assert!(
            error
                .to_string()
                .contains("native payer or amount layout differs")
                || error.to_string().contains("ambiguous pallet identity"),
            "{fault}: {error}"
        );
    }
}

#[test]
fn historical_fee_original_event_bytes_reject_bad_phase_topics_and_duplicate_execution() {
    for fault in [
        "phase",
        "index",
        "topics",
        "duplicate-execution",
        "duplicate-refund",
        "purpose",
        "refund-order",
    ] {
        let job = fee_job_changed(Some(250), false, false, |events, _| match fault {
            "phase" => events[0].1[0] = 3,
            "index" => events[0].1[1..5].copy_from_slice(&1u32.to_le_bytes()),
            "topics" => events[0].1.push(0),
            "duplicate-execution" => events.push(events.last().unwrap().clone()),
            "duplicate-refund" => events.insert(2, events[1].clone()),
            "purpose" => events[1].0 = Some("fee-withdraw"),
            "refund-order" => events.swap(0, 1),
            _ => unreachable!(),
        });
        let error =
            run(&job).expect_err("runtime fee bytes with contradictory placement became a fact");
        assert!(
            error.to_string().contains("historical fee event"),
            "{fault}: {error}"
        );
    }
}

#[test]
fn historical_fee_init_phase_and_late_refund_remain_unassigned() {
    let late = fee_job_changed(Some(250), false, false, |events, _| {
        events.swap(1, 2);
    });
    let report = run(&late).expect("late refund evidence should remain retained and unassigned");
    let events = report.hook_observations.unwrap().fee_events.unwrap();
    assert_eq!(events.candidates[0].refund_rao, None);
    assert_eq!(events.candidates[0].debit_rao, None);
    assert_eq!(events.unmatched_fee_events, 1);
    let initialize = fee_job_changed(Some(250), false, false, |events, _| {
        for (_, raw) in events {
            raw[0] = 2;
            raw.drain(1..5);
        }
    });
    let report = run(&initialize).expect("initialization placement should be explicit unknown");
    let events = report.hook_observations.unwrap().fee_events.unwrap();
    assert_eq!(events.candidates[0].status, "placement-unresolved");
    assert_eq!(events.candidates[0].debit_rao, None);
    assert_eq!(events.unmatched_fee_events, 2);
}

#[test]
fn historical_complete_parent_proof_replays_top_and_child_writes() {
    let code = wasm(
        "",
        &format!("{READ} {WRITE} {CHILD_WRITE} (drop (call $root (i32.const 1)))"),
    );
    let job = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
        storage
            .children_default
            .get_mut(OWNER)
            .unwrap()
            .data
            .insert(b"k".to_vec(), b"v".to_vec());
    });
    let report = run(&job).expect("complete authenticated parent state refused");
    assert!(report.post_state_reproduced && report.storage_calls >= 4);
    assert_ne!(report.parent_state_root, report.child_state_root);
    assert_eq!(report.parent_hash, job.parent_hash);
    assert_eq!(report.child_hash, job.child_hash);
    assert_eq!(report.anchor_authority, "caller-supplied-unapproved");
    assert!(!report.runtime_admitted && !report.production_selection);
    assert_eq!(report.native_fee_debit, None);
    assert!(!report.native_fee_withdrawal_refund_observed);
}

#[test]
fn historical_complete_parent_proof_retains_unread_top_and_child_nodes() {
    let code = wasm("", "");
    let job = job(&code, |_| {});
    let nodes = job
        .proof_nodes_hex
        .iter()
        .map(|node| hex_bytes("node", node, MAXIMUM_CODE_BYTES).unwrap());
    let parent: NativeHeader = scale_exact(
        "parent",
        &hex_bytes("parent", &job.parent_header_hex, MAXIMUM_HEADER_BYTES).unwrap(),
    )
    .unwrap();
    let backend =
        create_proof_check_backend::<Blake2Hasher>(*parent.state_root(), StorageProof::new(nodes))
            .unwrap();
    for (key, expected) in parent_storage(&code).top {
        assert_eq!(backend.storage(&key).unwrap(), Some(expected));
    }
    for (key, expected) in &parent_storage(&code).children_default[OWNER].data {
        assert_eq!(
            backend
                .child_storage(&ChildInfo::new_default(OWNER), key)
                .unwrap()
                .as_ref(),
            Some(expected)
        );
    }
    assert_eq!(backend.storage(b"missing").unwrap(), None);
    assert_eq!(
        backend
            .child_storage(&ChildInfo::new_default(OWNER), b"missing")
            .unwrap(),
        None
    );
    assert!(run(&job).unwrap().post_state_reproduced);
}

#[test]
fn historical_missing_read_node_is_not_authenticated_absence() {
    let code = wasm("", READ);
    let mut job = job(&code, |_| {});
    top_only_proof(&mut job, false);
    let error = run(&job).expect_err("missing account proof became empty storage");
    assert!(
        error.to_string().contains("execute block refused"),
        "{error}"
    );
}

#[test]
fn historical_missing_write_path_refuses_unchanged_declared_root() {
    let code = wasm("", WRITE);
    let mut job = job(&code, |_| {});
    top_only_proof(&mut job, false);
    let error = run(&job).expect_err("missing write path accepted the old root");
    assert!(
        error.to_string().contains("post-state proof incomplete"),
        "{error}"
    );
}

#[test]
fn historical_missing_child_write_path_refuses_unchanged_declared_root() {
    let code = wasm("", CHILD_WRITE);
    let mut job = job(&code, |_| {});
    top_only_proof(&mut job, true);
    let error = run(&job).expect_err("missing child proof accepted original child/root");
    assert!(
        error.to_string().contains("post-state proof incomplete"),
        "{error}"
    );
}

#[test]
fn historical_missing_child_read_refuses_before_empty_value() {
    let code = wasm(
        "",
        "(drop (call $child_get (i64.const 64424512512) (i64.const 4294970312)))",
    );
    let mut job = job(&code, |_| {});
    top_only_proof(&mut job, true);
    let error = run(&job).expect_err("missing child node became an absent value");
    assert!(
        error.to_string().contains("execute block refused"),
        "{error}"
    );
}

#[test]
fn historical_complete_proof_supports_same_key_rollback_and_child_delete() {
    let code = wasm("", &format!("(call $begin) {WRITE} (call $rollback) {WRITE} {WRITE} (call $child_clear (i64.const 64424512512) (i64.const 4294970312))"));
    let job = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
        storage
            .children_default
            .get_mut(OWNER)
            .unwrap()
            .data
            .remove(b"k".as_slice());
    });
    assert!(
        run(&job)
            .expect("same-key/child deletion with complete proof refused")
            .post_state_reproduced
    );
}

#[test]
fn historical_empty_block_and_sealed_header_keep_original_identity() {
    let code = wasm("", "");
    let mut job = job(&code, |_| {});
    job.extrinsics_hex.clear();
    replace_child(&mut job, |header| {
        header.set_extrinsics_root(BlakeTwo256::ordered_trie_root(Vec::new(), StateVersion::V0));
        header
            .digest_mut()
            .push(DigestItem::Seal(*b"FAKE", vec![17; 64]));
    });
    let report = run(&job).expect("empty block with external seal refused");
    assert_eq!(report.child_hash, job.child_hash);
    assert_eq!(report.parent_state_root, report.child_state_root);
    assert_eq!(report.extrinsics, 0);
    assert!(!report.runtime_admitted);
}

#[test]
fn historical_header_body_parent_code_and_post_root_are_independent_guards() {
    let code = wasm("", WRITE);
    let valid = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
    });
    for fault in [
        "parent",
        "child",
        "body",
        "code",
        "code-proof",
        "post-root",
        "version",
    ] {
        let mut changed = valid.clone();
        match fault {
            "parent" => changed.parent_hash[0] ^= 1,
            "child" => changed.child_hash[0] ^= 1,
            "body" => changed.extrinsics_hex[0] = encoded(&vec![4u8, 5, 6].encode()),
            "code" => changed.runtime_code_sha256[0] ^= 1,
            "code-proof" => {
                let other = wasm("", "");
                changed.runtime_code_hex = encoded(&other);
                changed.runtime_code_sha256 = sha2_256(&other);
                changed.runtime_code_blake2b_256 = blake2_256(&other);
            }
            "post-root" => replace_child(&mut changed, |header| {
                header.set_state_root(H256::repeat_byte(19))
            }),
            "version" => changed.execution_state_version = 0,
            _ => unreachable!(),
        }
        let error = run(&changed).expect_err("substituted replay input accepted");
        assert!(error.to_string().contains("differs"), "{fault}: {error}");
    }
}

#[test]
fn historical_unsupported_host_is_not_a_successful_noop() {
    let code = wasm(
        "(import \"env\" \"ext_offchain_timestamp_version_1\" (func $clock (result i64)))",
        "(drop (call $clock))",
    );
    let error = run(&job(&code, |_| {})).expect_err("unsupported clock host silently executed");
    assert!(
        error.to_string().contains("execute block refused"),
        "{error}"
    );
}

#[test]
fn historical_unfinished_transaction_and_work_exhaustion_publish_no_fact() {
    for body in ["(call $begin)", "(loop $again (drop (call $get (i64.const 51539609536))) (local.set $n (i32.add (local.get $n) (i32.const 1))) (br_if $again (i32.lt_u (local.get $n) (i32.const 70000))))"] {
        let code = wasm("", body);
        let error = run(&job(&code, |_| {})).expect_err("unfinished or unbounded storage work accepted");
        assert!(error.to_string().contains("unfinished transaction") || error.to_string().contains("storage work bound"), "{error}");
    }
}

#[test]
fn historical_public_decoder_refuses_unknown_trailing_duplicate_and_bounded_inputs() {
    let code = wasm("", "");
    let valid = job(&code, |_| {});
    let mut raw: serde_json::Value = serde_json::to_value(&valid).unwrap();
    raw["invented_fee_verified"] = serde_json::Value::Bool(true);
    assert!(replay_historical_json(&serde_json::to_vec(&raw).unwrap()).is_err());
    for fault in [
        "trailing-header",
        "trailing-extrinsic",
        "duplicate-node",
        "node-count",
        "schema",
        "memory",
    ] {
        let mut changed = valid.clone();
        match fault {
            "trailing-header" => changed.parent_header_hex.push_str("00"),
            "trailing-extrinsic" => changed.extrinsics_hex[0].push_str("00"),
            "duplicate-node" => changed
                .proof_nodes_hex
                .push(changed.proof_nodes_hex[0].clone()),
            "node-count" => changed.proof_nodes_hex = vec!["0x01".to_owned(); 8193],
            "schema" => changed.schema.push('x'),
            "memory" => {
                assert!(
                    memory_bound(&wat::parse_str("(module (memory 2048))").unwrap(), None).is_err()
                );
                continue;
            }
            _ => unreachable!(),
        }
        assert!(run(&changed).is_err(), "malformed input accepted: {fault}");
    }
}

fn host_data(offset: u32, raw: &[u8]) -> String {
    format!(
        "(data (i32.const {offset}) \"{}\")",
        raw.iter()
            .map(|byte| format!("\\{byte:02x}"))
            .collect::<String>()
    )
}

fn host_span(offset: u32, size: usize) -> u64 {
    (size as u64) << 32 | u64::from(offset)
}

#[test]
fn historical_hosts_ed25519_uses_exact_sdk_valid_and_invalid_messages() {
    let pair = sp_core::ed25519::Pair::from_seed(&[7; 32]);
    let message = b"original bounded historical message";
    let signature = pair.sign(message);
    let imports = format!(
        r#"(import "env" "ext_crypto_ed25519_verify_version_1" (func $verify (param i32 i64 i32) (result i32))) {} {} {}"#,
        host_data(4096, signature.as_ref()),
        host_data(4200, pair.public().as_ref()),
        host_data(4300, message)
    );
    let body = format!(
        r#"(if (i32.eqz (call $verify (i32.const 4096) (i64.const {}) (i32.const 4200))) (then unreachable))
        (i32.store8 (i32.const 4300) (i32.const 255))
        (if (call $verify (i32.const 4096) (i64.const {}) (i32.const 4200)) (then unreachable))"#,
        host_span(4300, message.len()),
        host_span(4300, message.len())
    );
    let code = wasm(&imports, &body);
    let report = run(&job(&code, |_| {}))
        .expect("SDK ed25519 host should verify exact original message and reject mutation");
    assert_eq!(report.host_profile, "substrate-proof-bounded-hosts-v2");
    assert!(report.storage_calls >= 8 && report.storage_io_bytes >= message.len() * 2 + 192);
    assert!(!report.runtime_admitted && report.native_fee_debit.is_none());
}

#[test]
fn historical_hosts_sr25519_version_two_preserves_sdk_verification() {
    let pair = sp_core::sr25519::Pair::from_seed(&[9; 32]);
    let message = b"historical sr25519 context";
    let signature = pair.sign(message);
    let imports = format!(
        r#"(import "env" "ext_crypto_sr25519_verify_version_2" (func $verify (param i32 i64 i32) (result i32))) {} {} {}"#,
        host_data(4096, signature.as_ref()),
        host_data(4200, pair.public().as_ref()),
        host_data(4300, message)
    );
    let body = format!(
        r#"(if (i32.eqz (call $verify (i32.const 4096) (i64.const {}) (i32.const 4200))) (then unreachable))
        (i32.store8 (i32.const 4096) (i32.xor (i32.load8_u (i32.const 4096)) (i32.const 1)))
        (if (call $verify (i32.const 4096) (i64.const {}) (i32.const 4200)) (then unreachable))"#,
        host_span(4300, message.len()),
        host_span(4300, message.len())
    );
    let code = wasm(&imports, &body);
    assert!(
        run(&job(&code, |_| {}))
            .expect("SDK sr25519 v2 host outcome differs")
            .post_state_reproduced
    );
}

#[test]
fn historical_hosts_secp_recovery_keeps_sdk_bytes_and_bad_recovery_id() {
    let pair = sp_core::ecdsa::Pair::from_seed(&[11; 32]);
    let message = blake2_256(b"original secp256k1 digest");
    let signature = pair.sign_prehashed(&message);
    let imports = format!(
        r#"(import "env" "ext_crypto_secp256k1_ecdsa_recover_compressed_version_2" (func $recover (param i32 i32) (result i64))) {} {} {}"#,
        host_data(4096, signature.as_ref()),
        host_data(4200, &message),
        host_data(4300, pair.public().as_ref())
    );
    let body = r#"(local $pointer i32)
        (local.set $pointer (i32.wrap_i64 (call $recover (i32.const 4096) (i32.const 4200))))
        (if (i32.load8_u (local.get $pointer)) (then unreachable))
        (local.set $n (i32.const 0))
        (loop $bytes
            (if (i32.ne (i32.load8_u offset=1 (i32.add (local.get $pointer) (local.get $n)))
                (i32.load8_u (i32.add (i32.const 4300) (local.get $n)))) (then unreachable))
            (local.set $n (i32.add (local.get $n) (i32.const 1)))
            (br_if $bytes (i32.lt_u (local.get $n) (i32.const 33))))
        (i32.store8 (i32.const 4160) (i32.const 255))
        (local.set $pointer (i32.wrap_i64 (call $recover (i32.const 4096) (i32.const 4200))))
        (if (i32.ne (i32.load8_u (local.get $pointer)) (i32.const 1)) (then unreachable))
        (if (i32.ne (i32.load8_u offset=1 (local.get $pointer)) (i32.const 1)) (then unreachable))"#;
    let code = wasm(&imports, body);
    assert!(
        run(&job(&code, |_| {}))
            .expect("SDK recovery bytes or BadV result differ")
            .post_state_reproduced
    );
}

#[test]
fn historical_hosts_crypto_refuses_before_oversized_argument_allocation() {
    let code = wasm(
        r#"(import "env" "ext_crypto_ed25519_verify_version_1" (func $verify (param i32 i64 i32) (result i32)))"#,
        &format!(
            "(drop (call $verify (i32.const 4096) (i64.const {}) (i32.const 4200)))",
            host_span(4300, 8 * 1024 * 1024 + 1)
        ),
    );
    let error =
        run(&job(&code, |_| {})).expect_err("oversized crypto argument reached SDK allocation");
    assert!(
        error
            .to_string()
            .contains("historical crypto argument byte bound"),
        "{error}"
    );
}

#[test]
fn historical_hosts_crypto_rollback_does_not_refund_shared_work_budget() {
    let code = wasm(
        r#"(import "env" "ext_crypto_ed25519_verify_version_1" (func $verify (param i32 i64 i32) (result i32)))"#,
        r#"(loop $calls (call $begin)
            (drop (call $verify (i32.const 4096) (i64.const 0) (i32.const 4200)))
            (call $rollback)
            (local.set $n (i32.add (local.get $n) (i32.const 1)))
            (br_if $calls (i32.lt_u (local.get $n) (i32.const 20000))))"#,
    );
    let error =
        run(&job(&code, |_| {})).expect_err("crypto work survived rollback without accounting");
    assert!(
        error.to_string().contains("historical storage work bound"),
        "{error}"
    );
}

#[test]
fn historical_hosts_key_creation_and_external_io_remain_unavailable() {
    for (import, call) in [
        (
            r#"(import "env" "ext_crypto_ed25519_generate_version_1" (func $side (param i32 i64) (result i32)))"#,
            "(drop (call $side (i32.const 0) (i64.const 4294971392)))",
        ),
        (
            r#"(import "env" "ext_offchain_timestamp_version_1" (func $side (result i64)))"#,
            "(drop (call $side))",
        ),
    ] {
        let code = wasm(import, call);
        let error =
            run(&job(&code, |_| {})).expect_err("unapproved external-effect host was admitted");
        assert!(
            error.to_string().contains("missing") || error.to_string().contains("not found"),
            "{error}"
        );
    }
}

const PREFIX_HOST: &str = r#"(import "env" "ext_storage_clear_prefix_version_2" (func $prefix (param i64 i64) (result i64)))"#;
const KILL_HOST: &str = r#"(import "env" "ext_default_child_storage_storage_kill_version_3" (func $kill (param i64 i64) (result i64)))"#;

#[test]
fn historical_hosts_prefix_zero_limit_keeps_backend_and_removes_overlay() {
    let imports = format!(
        "{PREFIX_HOST} {} {}",
        host_data(4096, &Some(0u32).encode()),
        host_data(4200, b"zzzz-new")
    );
    let body = format!(
        r#"(local $result i32)
        (call $set (i64.const {}) (i64.const 4294970400))
        (local.set $result (i32.wrap_i64 (call $prefix (i64.const {}) (i64.const {}))))
        (if (i32.ne (i32.load8_u (local.get $result)) (i32.const 1)) (then unreachable))
        (if (i32.load offset=1 (local.get $result)) (then unreachable))"#,
        host_span(4200, 8),
        host_span(1984, 5),
        host_span(4096, 5)
    );
    let code = wasm(&imports, &body);
    assert!(
        run(&job(&code, |_| {}))
            .expect("zero backend limit lost original state or retained overlay key")
            .post_state_reproduced
    );
}

#[test]
fn historical_hosts_prefix_complete_result_preserves_unrelated_state() {
    let imports = format!(
        "{PREFIX_HOST} {}",
        host_data(4096, &Option::<u32>::None.encode())
    );
    let body = format!(
        r#"(local $result i32)
        (local.set $result (i32.wrap_i64 (call $prefix (i64.const {}) (i64.const {}))))
        (if (i32.load8_u (local.get $result)) (then unreachable))
        (if (i32.ne (i32.load offset=1 (local.get $result)) (i32.const 1)) (then unreachable))"#,
        host_span(1984, 5),
        host_span(4096, 1)
    );
    let code = wasm(&imports, &body);
    let report = run(&job(&code, |storage| {
        storage.top.remove(ACCOUNT);
    }))
    .expect("complete prefix deletion differs from exact SDK outcome");
    assert!(report.post_state_reproduced && report.storage_calls >= 4);
}

#[test]
fn historical_hosts_child_limit_counts_backend_not_repeated_overlay() {
    let imports = format!("{KILL_HOST} {}", host_data(4096, &Some(1u32).encode()));
    let call = format!(
        r#"(local.set $result (i32.wrap_i64 (call $kill (i64.const {}) (i64.const {}))))
        (if (i32.ne (i32.load8_u (local.get $result)) (i32.const 1)) (then unreachable))
        (if (i32.ne (i32.load offset=1 (local.get $result)) (i32.const 1)) (then unreachable))"#,
        host_span(3072, OWNER.len()),
        host_span(4096, 5)
    );
    let code = wasm(&imports, &format!("(local $result i32) {call} {call}"));
    assert!(
        run(&job(&code, |storage| {
            storage
                .children_default
                .get_mut(OWNER)
                .unwrap()
                .data
                .remove(b"k".as_slice());
        }))
        .expect("repeated limited child deletion consumed a different backend page")
        .post_state_reproduced
    );
}

#[test]
fn historical_hosts_missing_prefix_proof_cannot_publish_unchanged_parent_root() {
    let imports = format!(
        "{PREFIX_HOST} {}",
        host_data(4096, &Option::<u32>::None.encode())
    );
    let code = wasm(
        &imports,
        &format!(
            "(drop (call $prefix (i64.const {}) (i64.const {})))",
            host_span(1984, 5),
            host_span(4096, 1)
        ),
    );
    let mut incomplete = job(&code, |_| {});
    top_only_proof(&mut incomplete, false);
    let error = run(&incomplete)
        .expect_err("incomplete prefix iterator published the unchanged parent root");
    assert!(
        error
            .to_string()
            .contains("historical iterator proof incomplete"),
        "{error}"
    );
}

#[test]
fn historical_hosts_missing_child_iteration_cannot_publish_unchanged_parent_root() {
    let imports = format!(
        "{KILL_HOST} {}",
        host_data(4096, &Option::<u32>::None.encode())
    );
    let code = wasm(
        &imports,
        &format!(
            "(drop (call $kill (i64.const {}) (i64.const {})))",
            host_span(3072, OWNER.len()),
            host_span(4096, 1)
        ),
    );
    let mut incomplete = job(&code, |_| {});
    top_only_proof(&mut incomplete, true);
    let error = run(&incomplete)
        .expect_err("incomplete child iterator published the unchanged parent root");
    assert!(
        error
            .to_string()
            .contains("historical iterator proof incomplete"),
        "{error}"
    );
}

#[test]
fn historical_hosts_proof_size_without_recorder_is_unknown_not_zero() {
    let code = wasm(
        r#"(import "env" "ext_storage_proof_size_storage_proof_size_version_1" (func $size (result i64)))"#,
        "(if (i64.ne (call $size) (i64.const -1)) (then unreachable))",
    );
    assert!(
        run(&job(&code, |_| {}))
            .expect("proof-size sentinel was replaced by fabricated measured bytes")
            .post_state_reproduced
    );
}
