//! Real pinned-engine execution of synthetic originals proves export-only
//! visibility and exact function-relative call selection, not runtime473 labels.
use super::*;
use crate::{historical::global_alias::OriginalGlobal, observation_profile};

#[test]
fn historical_original_global_alias_reads_live_original_memory_and_exact_call() {
    let code = wasm("(global $original (mut i32) (i32.const 32))", &format!("(global.set $original (i32.const 4000)) (i64.store (i32.const 4008) (i64.const 77)) {READ} {WRITE}"));
    let mut profile = observation_profile(&code, "Core_execute_block", "native-epoch");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    let index = profile.rules[0].function_index;
    let inspected =
        observation_profile::inspect_original(&code, sha2_256(&code), &[index]).unwrap();
    let function = inspected
        .functions
        .iter()
        .find(|function| function.function_index == index)
        .unwrap();
    let write = function
        .calls
        .as_ref()
        .unwrap()
        .iter()
        .find(|call| call.target == observation_profile::CallTarget::Direct { function_index: 1 })
        .unwrap()
        .clone();
    profile.rules[0].offset_start = write.offset;
    profile.rules[0].offset_end = write.end;
    profile.rules[0].memory = vec![observer::MemoryCapture {
        name: "computed".to_owned(),
        address: 8,
        global: Some("__urnetwork_observe_global_0".to_owned()),
        dereference_offsets: Vec::new(),
        bytes: 8,
        repeat: None,
    }];
    profile.original_globals = vec![OriginalGlobal {
        global_index: 0,
        export_name: "__urnetwork_observe_global_0".to_owned(),
    }];
    let proposal = observation_profile::ProfileProposal {
        schema: "urnetwork-original-wasm-profile-proposal-v1".to_owned(),
        reviewed_calls: vec![observation_profile::ReviewedCalls {
            function_index: index,
            offset_start: write.offset,
            offset_end: write.end,
            calls: vec![write.clone()],
        }],
        profile: profile.clone(),
    };
    assert_eq!(
        observation_profile::assemble_profile(&code, &proposal).unwrap(),
        serde_json::to_vec(&profile).unwrap()
    );
    let mut input = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
    });
    let original_code = input.runtime_code_hex.clone();
    input.observation_profile = Some(profile);
    let report = run(&input).expect("original-global capture refused");
    assert_eq!(input.runtime_code_hex, original_code);
    assert!(report.post_state_reproduced && !report.runtime_admitted);
    let trace = report.hook_observations.unwrap();
    assert_eq!(trace.observations.len(), 1);
    let observation = &trace.observations[0];
    assert_eq!(observation.operation, "set");
    assert!(observation
        .stack
        .iter()
        .any(|frame| frame.function_index == index
            && frame.function_offset >= write.offset
            && frame.function_offset < write.end));
    let capture = &observation.native.as_ref().unwrap().memory[0];
    assert_eq!(capture.address, 4008);
    assert_eq!(capture.bytes_hex, "0x4d00000000000000");
    assert_eq!(
        trace.authority,
        "caller-supplied-unapproved-callsite-profile"
    );
}

#[test]
fn historical_original_global_alias_does_not_replace_missing_or_outside_memory() {
    let code = wasm("(global $original (mut i32) (i32.const 524288))", WRITE);
    let mut profile = observation_profile(&code, "Core_execute_block", "native-epoch");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    profile.rules[0].memory = vec![observer::MemoryCapture {
        name: "outside".to_owned(),
        address: u32::MAX,
        global: Some("__urnetwork_observe_global_0".to_owned()),
        dereference_offsets: Vec::new(),
        bytes: 8,
        repeat: None,
    }];
    let mut input = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
    });
    input.observation_profile = Some(profile.clone());
    assert!(
        run(&input).is_err(),
        "missing export alias silently became a base"
    );
    profile.original_globals = vec![OriginalGlobal {
        global_index: 0,
        export_name: "__urnetwork_observe_global_0".to_owned(),
    }];
    input.observation_profile = Some(profile);
    assert!(
        run(&input).is_err(),
        "overflowing original pointer became evidence"
    );
}
