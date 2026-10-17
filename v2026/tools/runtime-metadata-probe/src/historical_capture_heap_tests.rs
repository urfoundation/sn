//! Capture and strict replay must execute with the same proved onchain heap.
//! Expected roots come from complete maps, independently of either execution.

use super::*;

/// Build a parent with an explicit heap override and its independently updated
/// child. The ordinary fixture intentionally has no heap override.
fn job_with_heap(code: &[u8], heap_pages: u64) -> HistoricalJob {
    let mut initial = parent_storage(code);
    initial
        .top
        .insert(well_known_keys::HEAP_PAGES.to_vec(), heap_pages.encode());
    let backing = TestExternalities::<Blake2Hasher>::new_with_code_and_state(
        code,
        initial.clone(),
        StateVersion::V1,
    );
    let (nodes, parent_root) = backing.into_raw_snapshot();
    initial.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
    let expected =
        TestExternalities::<Blake2Hasher>::new_with_code_and_state(code, initial, StateVersion::V1);
    let mut job = job(code, |_| {});
    let mut parent: NativeHeader = scale_exact(
        "heap fixture parent",
        &hex_bytes(
            "heap fixture parent",
            &job.parent_header_hex,
            MAXIMUM_HEADER_BYTES,
        )
        .unwrap(),
    )
    .unwrap();
    parent.set_state_root(parent_root);
    job.parent_header_hex = encoded(&parent.encode());
    job.parent_hash = parent.hash().0;
    replace_child(&mut job, |header| {
        header.set_parent_hash(parent.hash());
        header.set_state_root(*expected.backend.root());
    });
    job.proof_nodes_hex = nodes
        .into_iter()
        .map(|(_, (value, _))| encoded(&value))
        .collect::<BTreeSet<_>>()
        .into_iter()
        .collect();
    job
}

#[test]
fn historical_capture_executes_with_proved_onchain_heap_pages() {
    // In the pinned SDK, an onchain heap override makes the initial and
    // maximum memory both 8 + 1 pages. Offchain instead ignores that proof.
    let code = wasm(
        "",
        &format!(
            r#"(if (i32.ne (memory.size) (i32.const 9)) (then unreachable))
            (if (i32.ne (memory.grow (i32.const 1)) (i32.const -1)) (then unreachable))
            {WRITE}"#
        ),
    );
    let job = job_with_heap(&code, 1);
    let replay = run(&job).expect("complete original onchain heap fixture must replay");
    let report = collect(&job).expect("capture ignored the proved onchain heap configuration");
    assert!(report.replay.post_state_reproduced);
    assert_eq!(report.replay.child_state_root, replay.child_state_root);
    assert_eq!(report.replay.parent_state_root, replay.parent_state_root);
    let exported: HistoricalJob = serde_json::from_str(&report.job_json).unwrap();
    let retained = backend(&exported);
    assert_eq!(
        retained.storage(well_known_keys::HEAP_PAGES).unwrap(),
        Some(1_u64.encode())
    );
}

#[test]
fn historical_capture_without_heap_override_preserves_dynamic_growth() {
    let code = wasm(
        "",
        &format!(
            r#"(if (i32.ne (memory.size) (i32.const 8)) (then unreachable))
            (if (i32.ne (memory.grow (i32.const 1)) (i32.const 8)) (then unreachable))
            (if (i32.ne (memory.size) (i32.const 9)) (then unreachable))
            {WRITE}"#
        ),
    );
    let job = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
    });
    let replay = run(&job).expect("complete original dynamic heap fixture must replay");
    let report = collect(&job).expect("capture lost the bounded default dynamic heap");
    assert_eq!(report.replay.child_state_root, replay.child_state_root);
    let exported: HistoricalJob = serde_json::from_str(&report.job_json).unwrap();
    assert_eq!(
        backend(&exported)
            .storage(well_known_keys::HEAP_PAGES)
            .unwrap(),
        None
    );
}

#[test]
fn historical_capture_proved_heap_override_keeps_original_memory_bound() {
    let job = job_with_heap(&wasm("", WRITE), 1017);
    for error in [run(&job).unwrap_err(), collect(&job).err().unwrap()] {
        assert!(
            error.to_string().contains("historical Wasm memory bound"),
            "{error}"
        );
    }
}
