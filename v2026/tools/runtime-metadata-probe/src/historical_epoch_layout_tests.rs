//! Exact observer reads use the same strict live overlay. Expected roots come
//! from complete independent maps, and missing proof never becomes absence.
use super::*;

const MODE: &str = "single-mechanism-original-keys-v1";

fn registration() -> Vec<u8> {
    vec![0x11; 60]
}
fn generation() -> Vec<u8> {
    vec![0x22; 60]
}
fn span(address: u32, length: usize) -> u64 {
    ((length as u64) << 32) | u64::from(address)
}
fn segment(address: u32, bytes: &[u8]) -> String {
    let escaped = bytes
        .iter()
        .map(|byte| format!("\\{byte:02x}"))
        .collect::<String>();
    format!(r#"(data (i32.const {address}) "{escaped}")"#)
}
fn fixture(body: &str, change: impl FnOnce(&mut Storage)) -> HistoricalJob {
    let declarations = format!(
        r#"{} {} (func $drain (export "drain") {READ})"#,
        segment(4000, &generation()),
        segment(4072, &4u64.to_le_bytes())
    );
    let code = wasm(&declarations, body);
    let mut initial = parent_storage(&code);
    let phase = [
        sp_core::hashing::twox_128(b"System"),
        sp_core::hashing::twox_128(b"ExecutionPhase"),
    ]
    .concat();
    initial.top.insert(phase, vec![2]);
    initial
        .top
        .insert(registration(), 10u64.to_le_bytes().to_vec());
    initial
        .top
        .insert(generation(), 3u64.to_le_bytes().to_vec());
    let mut input = job_with_storage(&code, initial, change);
    let mut profile = observation_profile(&code, "drain", "native-drain");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    profile.epoch_layout = Some(MODE.to_owned());
    profile.rules[0].state_reads = vec![encoded(&registration()), encoded(&generation())];
    input.observation_profile = Some(profile);
    input
}
fn write_generation() -> String {
    format!(
        "(call $set (i64.const {}) (i64.const {}))",
        span(4000, 60),
        span(4072, 8)
    )
}

#[test]
fn historical_epoch_state_reads_use_exact_live_overlay_before_original_drain() {
    let input = fixture(
        &format!("{} (call $drain)", write_generation()),
        |storage| {
            storage
                .top
                .insert(generation(), 4u64.to_le_bytes().to_vec());
        },
    );
    let report = run(&input).expect("complete live-overlay fixture refused");
    assert!(report.post_state_reproduced && !report.runtime_admitted);
    let trace = report.hook_observations.unwrap();
    assert_eq!(trace.observations.len(), 1);
    let record = &trace.observations[0];
    assert_eq!(record.operation, "get");
    assert_eq!(record.key_hex, encoded(ACCOUNT));
    assert_eq!(
        record.storage_return.as_ref().unwrap().value_hex,
        Some(encoded(&vec![7; 96]))
    );
    let native = record.native.as_ref().unwrap();
    assert_eq!(native.execution_phase_hex.as_deref(), Some("0x02"));
    assert!(native.memory.is_empty());
    let values = native.execution_state.as_ref().unwrap();
    assert_eq!(values[0].key_hex, encoded(&registration()));
    assert_eq!(values[0].value_hex, Some(encoded(&10u64.to_le_bytes())));
    assert_eq!(values[1].key_hex, encoded(&generation()));
    assert_eq!(
        values[1].value_hex,
        Some(encoded(&4u64.to_le_bytes())),
        "observer borrowed parent3 instead of actual overlay4"
    );
}

#[test]
fn historical_epoch_state_read_rollback_discards_evidence_but_retains_work() {
    let mut input = fixture(
        &format!(
            "(call $begin) {} (call $drain) (call $rollback) (call $drain)",
            write_generation()
        ),
        |_| {},
    );
    let report = run(&input).expect("rollback fixture refused");
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
            .unwrap()[1]
            .value_hex,
        Some(encoded(&3u64.to_le_bytes()))
    );
    input.observation_profile = None;
    let plain = run(&input).expect("same original execution without optional observations refused");
    assert!(
        report.storage_calls >= plain.storage_calls + 8,
        "two reads and their returns per callback escaped work charging"
    );
    assert!(
        report.storage_io_bytes >= plain.storage_io_bytes + 2 * (60 + 60 + 8 + 8),
        "rolled-back state reads escaped byte charging"
    );
}

#[test]
fn historical_epoch_capture_retains_observer_only_paths_and_rejects_missing_proof() {
    let input = fixture("(call $drain)", |_| {});
    let mut ordinary = input.clone();
    ordinary.observation_profile = None;
    let ordinary_capture =
        capture_tests::collect(&ordinary).expect("ordinary runtime-only capture refused");
    let mut incomplete: HistoricalJob = serde_json::from_str(&ordinary_capture.job_json).unwrap();
    assert!(
        run(&incomplete).is_ok(),
        "baseline original execution proof failed"
    );
    incomplete.observation_profile = input.observation_profile.clone();
    assert!(
        run(&incomplete).is_err(),
        "runtime-only proof invented observer-only inclusion/absence"
    );
    let complete =
        capture_tests::collect(&input).expect("capture omitted explicit observer-only paths");
    let exported: HistoricalJob = serde_json::from_str(&complete.job_json).unwrap();
    let replay = run(&exported).expect("exported observer proof cannot independently replay");
    let values = replay.hook_observations.unwrap().observations[0]
        .native
        .as_ref()
        .unwrap()
        .execution_state
        .clone()
        .unwrap();
    assert_eq!(values[0].value_hex, Some(encoded(&10u64.to_le_bytes())));
    assert_eq!(values[1].value_hex, Some(encoded(&3u64.to_le_bytes())));
    assert!(complete.replay.post_state_reproduced);
}

#[test]
fn historical_epoch_state_reads_preserve_absence_and_refuse_oversized_values() {
    let base = fixture("(call $drain)", |_| {});
    let code = hex_bytes("fixture", &base.runtime_code_hex, MAXIMUM_CODE_BYTES).unwrap();
    let phase = [
        sp_core::hashing::twox_128(b"System"),
        sp_core::hashing::twox_128(b"ExecutionPhase"),
    ]
    .concat();
    for oversized in [false, true] {
        let mut initial = parent_storage(&code);
        initial.top.insert(phase.clone(), vec![2]);
        initial
            .top
            .insert(registration(), vec![0; if oversized { 9 } else { 8 }]);
        let mut input = job_with_storage(&code, initial, |_| {});
        input.observation_profile = base.observation_profile.clone();
        if oversized {
            assert!(
                run(&input).is_err(),
                "oversized execution-state value escaped bounded copy"
            );
            continue;
        }
        let report =
            capture_tests::collect(&input).expect("explicit observer absence proof refused");
        let trace = report.replay.hook_observations.unwrap();
        let values = trace.observations[0]
            .native
            .as_ref()
            .unwrap()
            .execution_state
            .as_ref()
            .unwrap();
        assert_eq!(values[0].value_hex, Some(encoded(&[0; 8])));
        assert_eq!(
            values[1].value_hex, None,
            "proven absence acquired an invented zero value"
        );
    }
}

#[test]
fn historical_epoch_layout_refuses_legacy_memory_and_unbounded_state_scope() {
    let input = fixture("(call $drain)", |_| {});
    let profile = input.observation_profile.unwrap();
    assert_eq!(
        super::super::epoch_layout::state_keys(&profile)
            .unwrap()
            .len(),
        2
    );
    for kind in 0..7 {
        let mut changed = profile.clone();
        match kind {
            0 => changed.epoch_layout = None,
            1 => changed.rules[0].state_reads.push("0x01".to_owned()),
            2 => changed.rules[0].state_reads[1] = changed.rules[0].state_reads[0].clone(),
            3 => changed.rules[0].state_reads[0] = "0xAA".to_owned(),
            4 => {
                let mut extra = changed.rules[0].clone();
                extra.state_reads = vec!["0x01".to_owned(), "0x02".to_owned()];
                changed.rules.push(extra);
            }
            5 => {
                changed.rules[0].state_reads.clear();
                changed.rules[0].purpose = "native-epoch".to_owned();
            }
            6 => {
                changed.rules[0].state_reads.clear();
                changed.rules[0].purpose = "native-epoch".to_owned();
                changed.rules[0].host_snapshot = Some("ext_allocator_malloc_version_1".to_owned());
                changed.rules[0].memory = vec![observer::MemoryCapture {
                    name: "hotkeys".to_owned(),
                    address: 1,
                    global: None,
                    dereference_offsets: Vec::new(),
                    bytes: 32,
                    repeat: None,
                }];
            }
            _ => unreachable!(),
        }
        assert!(
            super::super::epoch_layout::state_keys(&changed).is_err(),
            "legacy/unbounded grammar accepted at case{kind}"
        );
    }
}
