//! Original call-path selection happens before memory capture, while the full
//! principal mutation census remains independent of the optional selection.
use super::super::storage_call::{Callsite, StorageCall};
use super::*;
use crate::observation_profile::{self, CallTarget};

pub(super) fn call(code: &[u8], export: &str, callee: u32) -> Callsite {
    let ordinary = observation_profile(code, export, "native-miner-credit");
    let index = ordinary.rules[0].function_index;
    let report = observation_profile::inspect_original(code, sha2_256(code), &[index]).unwrap();
    let function = report
        .functions
        .iter()
        .find(|function| function.function_index == index)
        .unwrap();
    let call=function.calls.as_ref().unwrap().iter().find(|call| matches!(call.target,CallTarget::Direct{function_index} if function_index==callee)).unwrap();
    Callsite {
        function_index: index,
        function_body_sha256: function.function_body_sha256,
        offset_start: call.offset,
        offset_end: call.end,
    }
}
fn fixture() -> HistoricalJob {
    let declarations = format!(
        r#"(func $leaf (export "leaf") {READ}
      (i32.store (i32.const 4000) (i32.const 4008))
      (i64.store (i32.const 4008) (i64.const 77)) {WRITE})
      (func $semantic (export "semantic") (call $leaf))"#
    );
    let code=wasm(&declarations,"(i32.store (i32.const 4000) (i32.const -1)) (call $leaf) (i32.store (i32.const 4000) (i32.const -1)) (call $semantic)");
    let leaf = call(&code, "leaf", 1);
    let parent = call(&code, "semantic", leaf.function_index);
    let mut profile = observation_profile(&code, "semantic", "native-miner-credit");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    profile.principal_storage_prefixes = Some(vec![encoded(ACCOUNT)]);
    let rule = &mut profile.rules[0];
    rule.offset_start = parent.offset_start;
    rule.offset_end = parent.offset_end;
    rule.storage_call = Some(StorageCall {
        operation: "set".to_owned(),
        path: vec![leaf, parent],
    });
    rule.memory = vec![observer::MemoryCapture {
        name: "gross".to_owned(),
        address: 4000,
        global: None,
        dereference_offsets: vec![0],
        bytes: 8,
        repeat: None,
    }];
    let mut input = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
    });
    input.observation_profile = Some(profile);
    input
}

#[test]
fn historical_storage_call_path_captures_only_exact_host_before_memory_access() {
    let input = fixture();
    let result =
        run(&input).expect("exact original path refused or sibling host read invalid memory");
    assert!(result.post_state_reproduced && !result.runtime_admitted);
    let trace = result.hook_observations.unwrap();
    assert_eq!(trace.observations.len(), 1);
    let record = &trace.observations[0];
    assert_eq!(record.operation, "set");
    assert_eq!(record.key_hex, encoded(ACCOUNT));
    assert_eq!(
        record.native.as_ref().unwrap().memory[0].bytes_hex,
        encoded(&77u64.to_le_bytes())
    );
    assert_eq!(
        trace.principal_mutations.unwrap().len(),
        2,
        "unselected sibling write vanished from complete principal census"
    );
}

#[test]
fn historical_storage_call_path_rejects_changed_body_boundaries_callees_and_abi() {
    let input = fixture();
    let code = hex_bytes("fixture", &input.runtime_code_hex, MAXIMUM_CODE_BYTES).unwrap();
    let profile = input.observation_profile.unwrap();
    assert!(observer::validate_profile(profile.clone(), sha2_256(&code), &code).is_ok());
    for kind in 0..8 {
        let mut changed = profile.clone();
        let rule = &mut changed.rules[0];
        let call = rule.storage_call.as_mut().unwrap();
        match kind {
            0 => call.path[0].function_body_sha256[0] ^= 1,
            1 => call.path[0].offset_start += 1,
            2 => call.path[0].offset_end += 1,
            3 => call.operation = "append".to_owned(),
            4 => call.path[0].function_index = call.path[1].function_index,
            5 => call.path.pop().map(|_| ()).unwrap(),
            6 => call.path = vec![call.path[0].clone(); 9],
            7 => rule.host_snapshot = Some("ext_allocator_malloc_version_1".to_owned()),
            _ => unreachable!(),
        }
        assert!(
            observer::validate_profile(changed, sha2_256(&code), &code).is_err(),
            "changed original path admitted in case{kind}"
        );
    }
}

#[test]
fn historical_storage_call_path_disjoint_get_and_set_share_semantic_ancestor() {
    let mut input = fixture();
    let code = hex_bytes("fixture", &input.runtime_code_hex, MAXIMUM_CODE_BYTES).unwrap();
    let profile = input.observation_profile.as_mut().unwrap();
    let mut get = profile.rules[0].clone();
    get.memory.clear();
    get.storage_call.as_mut().unwrap().operation = "get".to_owned();
    get.storage_call.as_mut().unwrap().path[0] = call(&code, "leaf", 0);
    profile.rules.push(get.clone());
    let trace = run(&input)
        .expect("disjoint get/set paths were treated as overlapping parents")
        .hook_observations
        .unwrap();
    assert_eq!(trace.observations.len(), 2);
    assert_eq!(trace.observations[0].operation, "get");
    assert_eq!(trace.observations[1].operation, "set");
    input.observation_profile.as_mut().unwrap().rules.push(get);
    assert!(
        run(&input).is_err(),
        "identical original path acquired two purposes"
    );
}

#[test]
fn historical_storage_call_path_rollback_and_failed_poststate_never_release_credit() {
    let mut input = fixture();
    replace_child(&mut input, |header| {
        header.set_state_root(H256::repeat_byte(0x75))
    });
    assert!(
        run(&input).is_err(),
        "failed poststate released a selected credit"
    );
    // The same selected calls are subject to the existing transaction owner:
    // a rolled-back original write cannot survive merely because its path fits.
    let original = hex_bytes("fixture", &fixture().runtime_code_hex, MAXIMUM_CODE_BYTES).unwrap();
    let declarations = format!(
        r#"(func $leaf (export "leaf") {WRITE}) (func $semantic (export "semantic") (call $leaf))"#
    );
    let code = wasm(
        &declarations,
        "(call $begin) (call $semantic) (call $rollback)",
    );
    assert_ne!(code, original);
    let leaf = call(&code, "leaf", 1);
    let parent = call(&code, "semantic", leaf.function_index);
    let mut profile = observation_profile(&code, "semantic", "native-miner-credit");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    profile.rules[0].offset_start = parent.offset_start;
    profile.rules[0].offset_end = parent.offset_end;
    profile.rules[0].storage_call = Some(StorageCall {
        operation: "set".to_owned(),
        path: vec![leaf, parent],
    });
    let mut input = job(&code, |_| {});
    input.observation_profile = Some(profile);
    let trace = run(&input).unwrap().hook_observations.unwrap();
    assert!(trace.observations.is_empty());
    assert_eq!(trace.discarded_on_rollback, 1);
}

#[test]
fn historical_storage_call_path_rejects_intersecting_prefixes_at_different_depths() {
    let input = fixture();
    let code = hex_bytes("fixture", &input.runtime_code_hex, MAXIMUM_CODE_BYTES).unwrap();
    let mut profile = input.observation_profile.unwrap();
    let mut longer = profile.rules[0].clone();
    let parent = call(&code, "Core_execute_block", longer.function_index);
    longer.function_index = parent.function_index;
    longer.function_body_sha256 = parent.function_body_sha256;
    longer.offset_start = parent.offset_start;
    longer.offset_end = parent.offset_end;
    longer.storage_call.as_mut().unwrap().path.push(parent);
    profile.rules.push(longer);
    assert!(
        observer::validate_profile(profile, sha2_256(&code), &code).is_err(),
        "a shorter semantic prefix and its longer path matched the same callback"
    );
}

#[test]
fn historical_storage_call_path_observes_exact_original_append_with_ancestor() {
    let declarations = r#"(import "env" "ext_storage_append_version_1" (func $append (param i64 i64)))
      (func $leaf (export "leaf") (call $append (i64.const 51539609536) (i64.const 4294970400)))
      (func $semantic (export "semantic") (call $leaf))"#;
    let code = wasm(declarations, "(call $semantic)");
    let leaf = call(&code, "leaf", 10);
    let parent = call(&code, "semantic", leaf.function_index);
    let mut profile = observation_profile(&code, "semantic", "native-emission");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    profile.rules[0].offset_start = parent.offset_start;
    profile.rules[0].offset_end = parent.offset_end;
    profile.rules[0].storage_call = Some(StorageCall {
        operation: "append".to_owned(),
        path: vec![leaf, parent],
    });
    let mut initial = parent_storage(&code);
    initial.top.insert(ACCOUNT.to_vec(), vec![0]);
    let mut input = job_with_storage(&code, initial, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), vec![4, b'v']);
    });
    input.observation_profile = Some(profile);
    let report = run(&input).expect("original SCALE append path refused");
    let records = report.hook_observations.unwrap().observations;
    assert_eq!(records.len(), 1);
    assert_eq!(records[0].operation, "append");
    assert_eq!(records[0].value_hex, Some(encoded(b"v")));
    assert!(records[0].storage_return.is_none());
}
