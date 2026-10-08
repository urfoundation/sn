//! Original allocator calls supply the timing; no synthetic storage write is
//! inserted to make a pure epoch-memory observation available.
use super::*;
use crate::observation_profile::{self, CallTarget};

const MALLOC: &str = "ext_allocator_malloc_version_1";
const IMPORT: &str =
    r#"(import "env" "ext_allocator_malloc_version_1" (func $malloc (param i32) (result i32)))"#;

fn snapshot(code: &[u8], export: &str) -> observer::ObservationProfile {
    let mut profile = observation_profile(code, export, "native-epoch");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    let index = profile.rules[0].function_index;
    let inspected = observation_profile::inspect_original(code, sha2_256(code), &[index]).unwrap();
    let function = inspected
        .functions
        .iter()
        .find(|f| f.function_index == index)
        .unwrap();
    let call = function
        .calls
        .as_ref()
        .unwrap()
        .iter()
        .find(|call| matches!(call.target, CallTarget::Direct { function_index: 10 }))
        .unwrap();
    profile.rules[0].offset_start = call.offset;
    profile.rules[0].offset_end = call.end;
    profile.rules[0].host_snapshot = Some(MALLOC.to_owned());
    profile.rules[0].memory = vec![observer::MemoryCapture {
        name: "computed".to_owned(),
        address: 4008,
        global: None,
        dereference_offsets: Vec::new(),
        bytes: 8,
        repeat: None,
    }];
    profile
}

#[test]
fn historical_host_snapshot_reads_before_original_allocator_without_storage_surrogate() {
    let code = wasm(IMPORT, &format!("(i64.store (i32.const 4008) (i64.const 77)) (local.set $n (call $malloc (i32.const 8))) (i64.store (i32.const 4008) (i64.const 88)) (i64.store (local.get $n) (i64.const 99)) {WRITE}"));
    let mut input = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
    });
    input.observation_profile = Some(snapshot(&code, "Core_execute_block"));
    let report = run(&input).expect("exact original allocator snapshot refused");
    assert!(report.post_state_reproduced && !report.runtime_admitted);
    let trace = report.hook_observations.unwrap();
    assert_eq!(trace.observations.len(), 1);
    let record = &trace.observations[0];
    assert_eq!(record.operation, "host");
    assert_eq!(record.key_hex, encoded(MALLOC.as_bytes()));
    assert!(record.value_hex.is_none() && record.storage_return.is_none());
    assert_eq!(
        record.native.as_ref().unwrap().memory[0].bytes_hex,
        "0x4d00000000000000"
    );
    assert_eq!(
        trace.authority,
        "caller-supplied-unapproved-callsite-profile"
    );
    // Original execution, including allocator return and following store, also
    // completes when the optional snapshot is absent, but produces no record.
    input.observation_profile.as_mut().unwrap().rules[0].host_snapshot = None;
    assert!(run(&input)
        .unwrap()
        .hook_observations
        .unwrap()
        .observations
        .is_empty());
}

#[test]
fn historical_host_snapshot_rollback_discards_record_but_not_observation_budget() {
    let imports = format!(
        r#"{IMPORT} (func $epoch (export "epoch")
        (i64.store (i32.const 4008) (i64.const 77)) (drop (call $malloc (i32.const 8))))"#
    );
    let code = wasm(
        &imports,
        &format!("(call $begin) (call $epoch) (call $rollback) (call $epoch) {WRITE}"),
    );
    let mut input = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
    });
    input.observation_profile = Some(snapshot(&code, "epoch"));
    let trace = run(&input).unwrap().hook_observations.unwrap();
    assert_eq!(trace.observations.len(), 1);
    assert_eq!(trace.discarded_on_rollback, 1);
    assert!(trace.observations[0].ordinal > 1);
    assert_eq!(trace.observations[0].operation, "host");
}

#[test]
fn historical_host_snapshot_refuses_wrong_import_abi_and_nonexact_call_ranges() {
    let code = wasm(
        IMPORT,
        &format!("(drop (call $malloc (i32.const 8))) {WRITE}"),
    );
    let profile = snapshot(&code, "Core_execute_block");
    let mut changed = profile.clone();
    changed.rules[0].offset_start -= 1;
    assert!(observer::validate_profile(changed, sha2_256(&code), &code).is_err());
    let mut changed = profile.clone();
    changed.rules[0].offset_end += 1;
    assert!(observer::validate_profile(changed, sha2_256(&code), &code).is_err());
    for (imports, body) in [
        (
            IMPORT.replace("\"env\"", "\"foreign\""),
            "(drop (call $malloc (i32.const 8)))",
        ),
        (
            IMPORT.replace("malloc_version_1", "free_version_1"),
            "(drop (call $malloc (i32.const 8)))",
        ),
        (
            IMPORT.replace("param i32", "param i64"),
            "(drop (call $malloc (i64.const 8)))",
        ),
    ] {
        let original = wasm(&imports, &format!("{body} {WRITE}"));
        let candidate = snapshot(&original, "Core_execute_block");
        assert!(
            observer::validate_profile(candidate, sha2_256(&original), &original).is_err(),
            "foreign name, module or ABI was admitted"
        );
    }
    let mut changed = profile;
    changed.rules[0].host_snapshot = Some("ext_storage_get_version_1".to_owned());
    assert!(observer::validate_profile(changed, sha2_256(&code), &code).is_err());
}

#[test]
fn historical_host_snapshot_cannot_borrow_callee_frame_or_escape_failed_poststate() {
    let imports = format!(r#"{IMPORT} (func $nested (drop (call $malloc (i32.const 8))))"#);
    let code = wasm(&imports, &format!("(call $nested) {WRITE}"));
    let mut profile = observation_profile(&code, "Core_execute_block", "native-epoch");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    profile.rules[0].host_snapshot = Some(MALLOC.to_owned());
    profile.rules[0].memory = vec![observer::MemoryCapture {
        name: "computed".to_owned(),
        address: 4008,
        global: None,
        dereference_offsets: Vec::new(),
        bytes: 8,
        repeat: None,
    }];
    assert!(
        observer::validate_profile(profile, sha2_256(&code), &code).is_err(),
        "outer function inherited a nested allocator call"
    );
    let code = wasm(
        IMPORT,
        &format!("(drop (call $malloc (i32.const 8))) {WRITE}"),
    );
    let mut input = job(&code, |_| {}); // The actual write contradicts this root.
    input.observation_profile = Some(snapshot(&code, "Core_execute_block"));
    assert!(
        run(&input).is_err(),
        "incomplete original replay released a host snapshot"
    );
}
