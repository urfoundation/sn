//! Structural compiler tests use synthetic original modules, never fabricated
//! real-runtime layouts. Actual artifact inspection is a separate invocation.
use super::*;
use crate::historical::observer::{HookRule, MemoryPointer, MemoryRepeat};

fn fixture() -> (Vec<u8>, ProfileProposal) {
    let code = wat::parse_str(
        r#"(module
        (import "env" "fixture_host" (func $host))
        (memory (export "memory") 8)
        (global (export "__heap_base") i32 (i32.const 8192))
        (global $private (mut i32) (i32.const 512))
        (global (export "wide") i64 (i64.const 0))
        (func $selected (export "fixture_entry")
            (drop (i32.const 8192)) (call $host) (call $host)))"#,
    )
    .unwrap();
    let inspected = inspect_original(&code, sha2_256(&code), &[1]).unwrap();
    let function = &inspected.functions[0];
    let start = function.instructions.as_ref().unwrap()[0].offset;
    let rule = HookRule {
        host_snapshot: None,
        state_reads: Vec::new(),
        storage_call: None,
        recipient_owner: false,
        purpose: "native-epoch".to_owned(),
        function_index: 1,
        function_body_sha256: function.function_body_sha256,
        offset_start: start,
        offset_end: function.body_bytes,
        memory: vec![MemoryCapture {
            name: "netuid".to_owned(),
            address: 8,
            global: Some("__heap_base".to_owned()),
            dereference_offsets: Vec::new(),
            bytes: 2,
            repeat: None,
        }],
    };
    let proposal = ProfileProposal {
        schema: "urnetwork-original-wasm-profile-proposal-v1".to_owned(),
        reviewed_calls: vec![ReviewedCalls {
            function_index: 1,
            offset_start: start,
            offset_end: function.body_bytes,
            calls: function.calls.clone().unwrap(),
        }],
        profile: ObservationProfile {
            schema: "urnetwork-original-wasm-native-observation-v2".to_owned(),
            runtime_code_sha256: sha2_256(&code),
            source_review_sha256: [17; 32],
            rules: vec![rule],
            metadata_sha256: None,
            principal_storage_prefixes: None,
            original_globals: Vec::new(),
            epoch_layout: None,
            recipient_layout: None,
        },
    };
    (code, proposal)
}

#[test]
fn original_profile_inspection_preserves_body_indices_and_call_bytes() {
    let (code, _) = fixture();
    let inspected = inspect_original(&code, sha2_256(&code), &[1]).unwrap();
    assert_eq!(inspected.imported_functions[0].index, 0);
    assert_eq!(inspected.functions[0].function_index, 1);
    assert_eq!(inspected.functions[0].exports, ["fixture_entry"]);
    assert_eq!(inspected.functions[0].calls.as_ref().unwrap().len(), 2);
    for call in inspected.functions[0].calls.as_ref().unwrap() {
        let instruction = inspected.functions[0]
            .instructions
            .as_ref()
            .unwrap()
            .iter()
            .find(|instruction| instruction.offset == call.offset)
            .unwrap();
        assert_eq!(instruction.bytes_hex, "0x1000");
        assert_eq!(call.end, call.offset + 2);
        assert_eq!(call.target, CallTarget::Direct { function_index: 0 });
    }
    let private = &inspected.globals[1];
    assert!(private.mutable && private.exports.is_empty());
    assert_eq!(private.initialization, ["I32Const { value: 512 }", "End"]);
    assert_eq!(
        inspected.authority,
        "static-original-bytes-only-no-semantic-approval"
    );
}

#[test]
fn original_profile_assembly_emits_exact_existing_wire_bytes_without_approval() {
    let (code, proposal) = fixture();
    let raw = assemble_profile(&code, &proposal).unwrap();
    assert_eq!(raw, serde_json::to_vec(&proposal.profile).unwrap());
    let decoded: ObservationProfile = serde_json::from_slice(&raw).unwrap();
    validate_profile(decoded.clone(), sha2_256(&code), &code).unwrap();
    assert_eq!(decoded.source_review_sha256, [17; 32]);
    let fields: serde_json::Value = serde_json::from_slice(&raw).unwrap();
    assert!(fields.get("signature_ed25519").is_none());
    assert!(fields.get("approved").is_none());
}

#[test]
fn original_profile_rejects_wrong_code_body_and_callee() {
    let (code, proposal) = fixture();
    assert!(inspect_original(&code, [0; 32], &[1])
        .err()
        .unwrap()
        .to_string()
        .contains("code digest"));
    for fault in 0..3 {
        let mut changed = proposal.clone();
        match fault {
            0 => changed.profile.runtime_code_sha256[0] ^= 1,
            1 => changed.profile.rules[0].function_body_sha256[0] ^= 1,
            _ => {
                changed.reviewed_calls[0].calls[0].target = CallTarget::Direct { function_index: 1 }
            }
        }
        assert!(
            assemble_profile(&code, &changed).is_err(),
            "fault {fault} was admitted"
        );
    }
}

#[test]
fn original_profile_requires_instruction_boundaries_and_every_call() {
    let (code, proposal) = fixture();
    for fault in 0..5 {
        let mut changed = proposal.clone();
        match fault {
            0 => {
                changed.profile.rules[0].offset_start += 1;
                changed.reviewed_calls[0].offset_start += 1;
            }
            1 => {
                let end = changed.reviewed_calls[0].calls[0].offset + 1;
                changed.profile.rules[0].offset_end = end;
                changed.reviewed_calls[0].offset_end = end;
            }
            2 => {
                changed.reviewed_calls[0].calls.pop();
            }
            3 => {
                changed.reviewed_calls[0].calls.reverse();
            }
            _ => {
                changed.profile.rules[0].offset_start = 0;
                changed.reviewed_calls[0].offset_start = 0;
            }
        }
        assert!(
            assemble_profile(&code, &changed).is_err(),
            "fault {fault} was admitted"
        );
    }
}

#[test]
fn original_profile_refuses_unavailable_global_and_repeat_count_bases() {
    let (code, proposal) = fixture();
    for name in ["private", "__stack_pointer", "wide"] {
        let mut changed = proposal.clone();
        changed.profile.rules[0].memory[0].global = Some(name.to_owned());
        assert!(assemble_profile(&code, &changed)
            .unwrap_err()
            .to_string()
            .contains("original defined exported i32"));
    }
    let mut changed = proposal.clone();
    changed.profile.rules[0].memory[0].repeat = Some(MemoryRepeat {
        count: MemoryPointer {
            address: 0,
            global: Some("private".to_owned()),
            dereference_offsets: Vec::new(),
        },
        maximum: 4,
        stride: 2,
    });
    assert!(assemble_profile(&code, &changed).is_err());
}

#[test]
fn original_profile_reuses_runtime_bounds_and_refuses_empty_review() {
    let (code, proposal) = fixture();
    for fault in 0..4 {
        let mut changed = proposal.clone();
        match fault {
            0 => changed.profile.rules[0].memory[0].bytes = 256 * 1024 + 1,
            1 => changed.profile.rules[0].memory[0].dereference_offsets = vec![0; 5],
            2 => changed.profile.source_review_sha256 = [0; 32],
            _ => changed.profile.rules[0].purpose = "assumed-native-income".to_owned(),
        }
        assert!(
            assemble_profile(&code, &changed).is_err(),
            "fault {fault} was admitted"
        );
    }
    let mut changed = proposal;
    changed.reviewed_calls.clear();
    assert!(assemble_profile(&code, &changed).is_err());
}

#[test]
fn original_profile_selection_refuses_duplicates_imports_and_missing_functions() {
    let (code, _) = fixture();
    for selected in [vec![1, 1], vec![0], vec![2], vec![1; 33]] {
        assert!(inspect_original(&code, sha2_256(&code), &selected).is_err());
    }
    let census = inspect_original(&code, sha2_256(&code), &[]).unwrap();
    assert_eq!(census.functions.len(), 1);
    assert!(census.functions[0].instructions.is_none());
}

#[test]
fn original_profile_indirect_call_review_binds_table_and_type() {
    let code = wat::parse_str(
        r#"(module
        (type $unit (func)) (table 1 funcref) (memory (export "memory") 8)
        (global (export "__heap_base") i32 (i32.const 8192))
        (func $callee) (elem (i32.const 0) $callee)
        (func (export "entry") (i32.const 0) (call_indirect (type $unit))))"#,
    )
    .unwrap();
    let inspected = inspect_original(&code, sha2_256(&code), &[1]).unwrap();
    let function = &inspected.functions[1];
    let call = &function.calls.as_ref().unwrap()[0];
    assert_eq!(
        call.target,
        CallTarget::Indirect {
            type_index: 0,
            table_index: 0
        }
    );
    let proposal = ProfileProposal {
        schema: "urnetwork-original-wasm-profile-proposal-v1".to_owned(),
        profile: ObservationProfile {
            schema: "urnetwork-original-wasm-hook-observation-v1".to_owned(),
            runtime_code_sha256: sha2_256(&code),
            source_review_sha256: [29; 32],
            metadata_sha256: None,
            principal_storage_prefixes: None,
            original_globals: Vec::new(),
            epoch_layout: None,
            recipient_layout: None,
            rules: vec![HookRule {
                host_snapshot: None,
                state_reads: Vec::new(),
                storage_call: None,
                recipient_owner: false,
                purpose: "fee-withdraw".to_owned(),
                function_index: 1,
                function_body_sha256: function.function_body_sha256,
                offset_start: call.offset,
                offset_end: call.end,
                memory: Vec::new(),
            }],
        },
        reviewed_calls: vec![ReviewedCalls {
            function_index: 1,
            offset_start: call.offset,
            offset_end: call.end,
            calls: vec![call.clone()],
        }],
    };
    assert!(assemble_profile(&code, &proposal).is_ok());
    let mut changed = proposal;
    changed.reviewed_calls[0].calls[0].target = CallTarget::Indirect {
        type_index: 0,
        table_index: 1,
    };
    assert!(assemble_profile(&code, &changed).is_err());
}

#[test]
fn original_profile_tail_calls_are_distinct_in_complete_inspection() {
    let code = wat::parse_str(
        r#"(module
        (type $unit (func)) (table 1 funcref)
        (func $callee) (elem (i32.const 0) $callee)
        (func (return_call $callee))
        (func (i32.const 0) (return_call_indirect (type $unit))))"#,
    )
    .unwrap();
    let inspected = inspect_original(&code, sha2_256(&code), &[1, 2]).unwrap();
    assert_eq!(
        inspected.functions[1].calls.as_ref().unwrap()[0].target,
        CallTarget::ReturnDirect { function_index: 0 }
    );
    assert_eq!(
        inspected.functions[2].calls.as_ref().unwrap()[0].target,
        CallTarget::ReturnIndirect {
            type_index: 0,
            table_index: 0
        }
    );
}

#[test]
fn original_profile_listing_refuses_before_instruction_and_byte_growth() {
    let (code, _) = fixture();
    assert!(inspect_with_limits(
        &code,
        sha2_256(&code),
        &[1],
        1,
        MAXIMUM_RETAINED_LISTING_BYTES
    )
    .err()
    .unwrap()
    .to_string()
    .contains("count bound"));
    assert!(
        inspect_with_limits(&code, sha2_256(&code), &[1], MAXIMUM_LISTED_INSTRUCTIONS, 1)
            .err()
            .unwrap()
            .to_string()
            .contains("byte bound")
    );
}

#[test]
fn original_profile_assembly_preserves_replay_memory_admission() {
    for memory in [
        "(memory 1025)",
        "(import \"env\" \"memory\" (memory 1025))",
        "",
    ] {
        let code = wat::parse_str(format!(
            r#"(module
            (import "env" "fixture_host" (func $host)) {memory}
            (global (export "__heap_base") i32 (i32.const 8192))
            (func (call $host)))"#
        ))
        .unwrap();
        let inspected = inspect_original(&code, sha2_256(&code), &[1]).unwrap();
        let function = &inspected.functions[0];
        let call = function.calls.as_ref().unwrap()[0].clone();
        let proposal = ProfileProposal {
            schema: "urnetwork-original-wasm-profile-proposal-v1".to_owned(),
            profile: ObservationProfile {
                schema: "urnetwork-original-wasm-hook-observation-v1".to_owned(),
                runtime_code_sha256: sha2_256(&code),
                source_review_sha256: [13; 32],
                metadata_sha256: None,
                principal_storage_prefixes: None,
                original_globals: Vec::new(),
                epoch_layout: None,
                recipient_layout: None,
                rules: vec![HookRule {
                    host_snapshot: None,
                    state_reads: Vec::new(),
                    storage_call: None,
                    recipient_owner: false,
                    purpose: "fee-withdraw".to_owned(),
                    function_index: 1,
                    function_body_sha256: function.function_body_sha256,
                    offset_start: call.offset,
                    offset_end: call.end,
                    memory: Vec::new(),
                }],
            },
            reviewed_calls: vec![ReviewedCalls {
                function_index: 1,
                offset_start: call.offset,
                offset_end: call.end,
                calls: vec![call],
            }],
        };
        assert!(
            assemble_profile(&code, &proposal)
                .unwrap_err()
                .to_string()
                .contains("memory"),
            "memory shape admitted: {memory}"
        );
    }
}

#[test]
fn original_profile_reserved_export_cannot_substitute_for_alias_declaration() {
    let (_, mut proposal) = fixture();
    // A separately parsed original has an existing reserved export and the same
    // function body. That original export cannot bypass alias declarations.
    let code = wat::parse_str(
        r#"(module
        (import "env" "fixture_host" (func $host))
        (memory (export "memory") 8)
        (global (export "__heap_base") i32 (i32.const 8192))
        (global (export "__urnetwork_observe_global_0") i32 (i32.const 32))
        (func (drop (i32.const 8192)) (call $host) (call $host)))"#,
    )
    .unwrap();
    proposal.profile.runtime_code_sha256 = sha2_256(&code);
    proposal.profile.rules[0].memory[0].global = Some("__urnetwork_observe_global_0".to_owned());
    assert!(assemble_profile(&code, &proposal)
        .unwrap_err()
        .to_string()
        .contains("undeclared"));
    proposal.profile.rules[0].memory[0].global = None;
    proposal.profile.rules[0].memory[0].repeat = Some(MemoryRepeat {
        count: MemoryPointer {
            address: 0,
            global: Some("__urnetwork_observe_global_0".to_owned()),
            dereference_offsets: Vec::new(),
        },
        maximum: 1,
        stride: 2,
    });
    assert!(assemble_profile(&code, &proposal)
        .unwrap_err()
        .to_string()
        .contains("undeclared"));
}
