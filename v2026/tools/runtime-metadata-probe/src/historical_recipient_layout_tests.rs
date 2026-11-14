//! Dynamic Owner proof paths are captured from the same live overlay as the
//! selected original callback, including aliases, rollback and explicit absence.
use super::super::storage_call::StorageCall;
use super::*;

fn owner_key() -> Vec<u8> {
    super::super::recipient_layout::owner_key(&[observer::MemoryObservation {
        name: "hotkey".to_owned(),
        address: 4000,
        bytes_hex: encoded(&[0x39; 32]),
        element_count: None,
    }])
    .unwrap()
}
fn segment(address: u32, bytes: &[u8]) -> String {
    let bytes = bytes
        .iter()
        .map(|byte| format!("\\{byte:02x}"))
        .collect::<String>();
    format!(r#"(data (i32.const {address}) "{bytes}")"#)
}
fn write_owner() -> String {
    format!(
        "(call $set (i64.const {}) (i64.const {}))",
        (80u64 << 32) | 4500,
        (32u64 << 32) | 4600
    )
}
fn fixture(owner: Option<Vec<u8>>, body: &str, change: impl FnOnce(&mut Storage)) -> HistoricalJob {
    let declarations = format!(
        r#"(global $frame (mut i32) (i32.const 4000)) {} {} {}
        (func $leaf (export "leaf") {READ})
        (func $semantic (export "semantic") (call $leaf))"#,
        segment(4000, &[0x39; 32]),
        segment(4500, &owner_key()),
        segment(4600, &[0x77; 32])
    );
    let code = wasm(&declarations, body);
    let leaf = storage_call_tests::call(&code, "leaf", 0);
    let parent = storage_call_tests::call(&code, "semantic", leaf.function_index);
    let mut profile = observation_profile(&code, "semantic", "native-owner-recycle");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    profile.recipient_layout = Some("original-storage-credit-pairs-v1".to_owned());
    profile.original_globals = vec![super::super::global_alias::OriginalGlobal {
        global_index: 0,
        export_name: "__urnetwork_observe_global_0".to_owned(),
    }];
    let rule = &mut profile.rules[0];
    rule.offset_start = parent.offset_start;
    rule.offset_end = parent.offset_end;
    rule.storage_call = Some(StorageCall {
        operation: "get".to_owned(),
        path: vec![leaf, parent],
    });
    rule.recipient_owner = true;
    rule.memory = vec![observer::MemoryCapture {
        name: "hotkey".to_owned(),
        address: 0,
        global: Some("__urnetwork_observe_global_0".to_owned()),
        dereference_offsets: Vec::new(),
        bytes: 32,
        repeat: None,
    }];
    let mut initial = parent_storage(&code);
    let phase = [
        sp_core::hashing::twox_128(b"System"),
        sp_core::hashing::twox_128(b"ExecutionPhase"),
    ]
    .concat();
    initial.top.insert(phase, vec![2]);
    if let Some(owner) = owner {
        initial.top.insert(owner_key(), owner);
    }
    let mut input = job_with_storage(&code, initial, change);
    input.observation_profile = Some(profile);
    input
}

#[test]
fn historical_recipient_owner_samples_exact_live_overlay_without_runtime_get() {
    let input = fixture(
        Some(vec![0x55; 32]),
        &format!("{} (call $semantic)", write_owner()),
        |storage| {
            storage.top.insert(owner_key(), vec![0x77; 32]);
        },
    );
    let report = run(&input).expect("dynamic same-overlay Owner lookup refused");
    let record = &report.hook_observations.as_ref().unwrap().observations[0];
    assert_eq!(
        record.key_hex,
        encoded(ACCOUNT),
        "observer read was relabelled as runtime get"
    );
    let native = record.native.as_ref().unwrap();
    assert_eq!(native.execution_phase_hex.as_deref(), Some("0x02"));
    assert_eq!(native.memory[0].bytes_hex, encoded(&[0x39; 32]));
    let state = native.execution_state.as_ref().unwrap();
    assert_eq!(state.len(), 1);
    assert_eq!(state[0].key_hex, encoded(&owner_key()));
    assert_eq!(
        state[0].value_hex,
        Some(encoded(&[0x77; 32])),
        "parent Owner was substituted for current overlay"
    );
}

#[test]
fn historical_recipient_capture_retains_dynamic_only_proof_and_original_code() {
    let input = fixture(Some(vec![0x55; 32]), "(call $semantic)", |_| {});
    let mut ordinary = input.clone();
    ordinary.observation_profile = None;
    let ordinary_capture = capture_tests::collect(&ordinary).expect("ordinary capture refused");
    let mut incomplete: HistoricalJob = serde_json::from_str(&ordinary_capture.job_json).unwrap();
    assert!(run(&incomplete).is_ok());
    incomplete.observation_profile = input.observation_profile.clone();
    assert!(
        run(&incomplete).is_err(),
        "missing observer-only paths became proven absence"
    );
    let captured = capture_tests::collect(&input)
        .expect("capture omitted dynamic proof or original global alias");
    let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
    assert_eq!(exported.runtime_code_hex, input.runtime_code_hex);
    assert_eq!(exported.runtime_code_sha256, input.runtime_code_sha256);
    assert_eq!(
        exported.runtime_code_blake2b_256,
        input.runtime_code_blake2b_256
    );
    let replay = run(&exported).expect("captured proof cannot independently replay");
    assert_eq!(
        replay.hook_observations.unwrap().observations[0]
            .native
            .as_ref()
            .unwrap()
            .execution_state
            .as_ref()
            .unwrap()[0]
            .value_hex,
        Some(encoded(&[0x55; 32]))
    );
}

#[test]
fn historical_recipient_owner_preserves_absence_and_rejects_malformed_or_unbounded_reads() {
    let absent = fixture(None, "(call $semantic)", |_| {});
    let captured = capture_tests::collect(&absent).expect("explicit Owner absence capture refused");
    assert_eq!(
        captured.replay.hook_observations.unwrap().observations[0]
            .native
            .as_ref()
            .unwrap()
            .execution_state
            .as_ref()
            .unwrap()[0]
            .value_hex,
        None
    );
    for width in [0, 31, 33] {
        let input = fixture(Some(vec![0x55; width]), "(call $semantic)", |_| {});
        assert!(
            run(&input).is_err(),
            "Owner width{width} was silently normalized"
        );
    }
    for kind in 0..10 {
        let mut changed = absent.clone();
        let profile = changed.observation_profile.as_mut().unwrap();
        match kind {
            0 => profile.recipient_layout = None,
            1 => profile.rules[0].purpose = "native-miner-credit".to_owned(),
            2 => profile.rules[0].memory[0].bytes = 33,
            3 => profile.rules[0].recipient_owner = false,
            4 => profile.rules[0].state_reads = vec!["0x00".to_owned()],
            5 => profile.rules[0].storage_call = None,
            6..=9 => {
                let name = [
                    "captured",
                    "subnet-owner-hotkey",
                    "auto-stake-destination",
                    "coldkey",
                ][kind - 6];
                profile.rules[0].memory.push(observer::MemoryCapture {
                    name: name.to_owned(),
                    address: 0,
                    global: None,
                    dereference_offsets: Vec::new(),
                    bytes: 32,
                    repeat: None,
                });
            }
            _ => unreachable!(),
        }
        assert!(
            run(&changed).is_err(),
            "dynamic Owner scope substitution accepted case{kind}"
        );
    }
}

#[test]
fn historical_recipient_owner_rollback_discards_evidence_keeps_charges_and_refuses_bad_postroot() {
    let mut input = fixture(
        Some(vec![0x55; 32]),
        &format!(
            "(call $begin) {} (call $semantic) (call $rollback) (call $semantic)",
            write_owner()
        ),
        |_| {},
    );
    let report = run(&input).expect("Owner rollback fixture refused");
    let trace = report.hook_observations.as_ref().unwrap();
    assert_eq!(trace.discarded_on_rollback, 1);
    assert_eq!(trace.observations.len(), 1);
    assert_eq!(
        trace.observations[0]
            .native
            .as_ref()
            .unwrap()
            .execution_state
            .as_ref()
            .unwrap()[0]
            .value_hex,
        Some(encoded(&[0x55; 32]))
    );
    let mut plain = input.clone();
    plain.observation_profile = None;
    let plain = run(&plain).unwrap();
    assert!(report.storage_calls >= plain.storage_calls + 4);
    assert!(report.storage_io_bytes >= plain.storage_io_bytes + 2 * (80 + 32));
    replace_child(&mut input, |header| {
        header.set_state_root(H256::repeat_byte(0x63))
    });
    assert!(
        run(&input).is_err(),
        "bad poststate released dynamic Owner evidence"
    );
}
