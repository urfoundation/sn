//! Actual original-Wasm allocation exports. These synthetic programs close a
//! complete two-UID integer input census; they confer no deployed authority.

use super::*;

pub(super) fn declarations(mode: &str) -> String {
    let yuma3 = mode != "legacy";
    let liquid = mode == "liquid";
    let mut text = String::new();
    for (address, bytes) in [
        (8000, 2u16.to_le_bytes().to_vec()),
        (8016, words(&[101, 10, 300, 90, 0])),
        (8056, 1u16.to_le_bytes().to_vec()),
        (8064, words(&[0])),
        (
            8072,
            [32768u16.to_le_bytes(), 65535u16.to_le_bytes()].concat(),
        ),
        (8080, words(&[if yuma3 { 0 } else { 1_000_000 }])),
        (8100, vec![u8::from(yuma3), u8::from(liquid), 0, 0]),
        (
            8104,
            [
                16384u16.to_le_bytes(),
                49152u16.to_le_bytes(),
                0i16.to_le_bytes(),
            ]
            .concat(),
        ),
        (8112, 0u32.to_le_bytes().to_vec()),
        (8296, words(&[u64::MAX])),
        (8700, b"synthetic-original-yuma-input".to_vec()),
        (9000, words(&[1, 9])),
    ] {
        text.push_str(&segment(address, &bytes));
    }
    // The bounded long division uses only admitted MVP integer operations and
    // avoids overflow when a Q32 numerator has its whole-number bit set.
    text.push_str(r#"
      (func $yuma_div (param $x i64) (param $y i64) (result i64)
        (local $q i64) (local $r i64) (local $i i32)
        (if (i64.eqz (local.get $y)) (then (return (i64.const 0))))
        (local.set $q (i64.div_u (local.get $x) (local.get $y)))
        (local.set $r (i64.rem_u (local.get $x) (local.get $y)))
        (loop $bits
          (local.set $q (i64.shl (local.get $q) (i64.const 1)))
          (local.set $r (i64.shl (local.get $r) (i64.const 1)))
          (if (i64.ge_u (local.get $r) (local.get $y)) (then
            (local.set $r (i64.sub (local.get $r) (local.get $y)))
            (local.set $q (i64.or (local.get $q) (i64.const 1)))))
          (local.set $i (i32.add (local.get $i) (i32.const 1)))
          (br_if $bits (i32.lt_u (local.get $i) (i32.const 32))))
        (local.get $q))
      (func $yuma_meta (export "original_yuma_meta")
        (drop (call $get (i64.const 115964125684))))
      (func $yuma_settings (export "original_yuma_settings")
        (drop (call $get (i64.const 115964125684))))
      (func $yuma_node (export "original_yuma_node") (param $uid i32)
        (local $hotkey i32)
        (i32.store16 (i32.const 8200) (local.get $uid))
        (local.set $hotkey (i32.add (i32.const 4200) (i32.mul (local.get $uid) (i32.const 32))))
        (i64.store (i32.const 8220) (i64.load (local.get $hotkey)))
        (i64.store (i32.const 8228) (i64.load offset=8 (local.get $hotkey)))
        (i64.store (i32.const 8236) (i64.load offset=16 (local.get $hotkey)))
        (i64.store (i32.const 8244) (i64.load offset=24 (local.get $hotkey)))
        (i64.store (i32.const 8260) (i64.add (i64.const 20) (i64.extend_i32_u (local.get $uid))))
        (i64.store (i32.const 8268) (i64.const 99))
        (i32.store8 (i32.const 8276) (local.get $uid))
        (i64.store (i32.const 8280) (i64.mul (i64.const 100) (i64.extend_i32_u (local.get $uid))))
        (drop (call $get (i64.const 115964125684))))
      (func $yuma_weights (export "original_yuma_weights") (param $uid i32)
        (i32.store16 (i32.const 8200) (local.get $uid))
        (i32.store (i32.const 8320) (i32.mul (local.get $uid) (i32.const 2)))
        (i32.store (i32.const 8300) (i32.const 65536))
        (i32.store16 (i32.const 8310) (i32.wrap_i64 (i64.load (i32.const 9000))))
        (i32.store16 (i32.const 8312) (i32.wrap_i64 (i64.load (i32.const 9008))))
        (drop (call $get (i64.const 115964125684))))
      (func $yuma_bonds (export "original_yuma_bonds") (param $uid i32)
        (i32.store16 (i32.const 8200) (local.get $uid))
        (i32.store (i32.const 8320) (i32.mul (i32.sub (i32.const 1) (local.get $uid)) (i32.const 2)))
        (i32.store (i32.const 8300) (i32.const 65536))
        (i32.store (i32.const 8310) (i32.const 65537))
        (drop (call $get (i64.const 115964125684))))
      (func $yuma_compute
        (local $sum i64) (local $dividend i64) (local $offset i32)
        (i64.store (i32.const 8608) (i64.const 4294967296))
        (i64.store (i32.const 8628) (i64.const 4294967296))
        (local.set $sum (i64.add (i64.load (i32.const 9000)) (i64.load (i32.const 9008))))
        (i64.store (i32.const 8640) (call $yuma_div (i64.load (i32.const 9000)) (local.get $sum)))
        (i64.store (i32.const 8648) (call $yuma_div (i64.load (i32.const 9008)) (local.get $sum)))
        (local.set $sum (i64.add (i64.load (i32.const 8640)) (i64.load (i32.const 8648))))
        (i64.store (i32.const 4100) (call $yuma_div (i64.load (i32.const 8640)) (local.get $sum)))
        (i64.store (i32.const 4108) (call $yuma_div (i64.load (i32.const 8648)) (local.get $sum)))
        ;; With one permitted active validator, column-normalized nonzero
        ;; bonds are one. Legacy retains row zero; Yuma3 uses the active row.
        (local.set $sum (i64.add (i64.load (i32.const 4100)) (i64.load (i32.const 4108))))
        (local.set $dividend (call $yuma_div (local.get $sum) (local.get $sum)))
        (i64.store (i32.const 4120) (i64.const 0))
        (i64.store (i32.const 4128) (i64.const 0))
        (local.set $offset (i32.mul (i32.load8_u (i32.const 8100)) (i32.const 8)))
        (i64.store (i32.add (i32.const 4120) (local.get $offset)) (local.get $dividend))
        (i64.store (i32.add (i32.const 8660) (local.get $offset)) (call $yuma_div (local.get $dividend) (i64.add (local.get $sum) (local.get $dividend))))
        (i64.store (i32.add (i32.const 8680) (local.get $offset)) (i64.shr_u (i64.mul (i64.const 200) (i64.load (i32.add (i32.const 8660) (local.get $offset)))) (i64.const 32))))
    "#);
    // Use the exact declared storage span, with no hand-authored address math.
    text.replace(
        "115964125684",
        &span(8700, b"synthetic-original-yuma-input".len()).to_string(),
    )
}

pub(super) fn body() -> &'static str {
    "(call $yuma_meta) (call $yuma_settings) (call $yuma_node (i32.const 0)) (call $yuma_node (i32.const 1)) (call $yuma_weights (i32.const 0)) (call $yuma_weights (i32.const 1)) (call $yuma_bonds (i32.const 0)) (call $yuma_bonds (i32.const 1)) (call $epoch)"
}

fn field(name: &str, address: u32, bytes: u32) -> observer::MemoryCapture {
    observer::MemoryCapture {
        name: name.to_owned(),
        address,
        global: None,
        dereference_offsets: Vec::new(),
        bytes,
        repeat: None,
    }
}

fn repeated(name: &str, address: u32, bytes: u32, count: u32) -> observer::MemoryCapture {
    let mut value = field(name, address, bytes);
    value.repeat = Some(observer::MemoryRepeat {
        count: observer::MemoryPointer {
            address: count,
            global: None,
            dereference_offsets: Vec::new(),
        },
        maximum: 4096,
        stride: bytes,
    });
    value
}

pub(super) fn profile(code: &[u8], profile: &mut observer::ObservationProfile) {
    let epoch = profile
        .rules
        .iter_mut()
        .find(|rule| rule.purpose == "native-epoch")
        .unwrap();
    for (name, address) in [
        ("stake-q32", 8600),
        ("active-stake-q32", 8620),
        ("consensus-q32", 8640),
        ("validator-normalized-q32", 8660),
        ("validator-emission", 8680),
    ] {
        epoch.memory.push(field(name, address, 16));
    }
    let meta = vec![
        field("netuid", 4000, 2),
        field("uid-count", 8000, 2),
        field("current-block", 8016, 8),
        field("tempo", 8024, 8),
        field("activity-cutoff", 8032, 8),
        field("last-step", 8040, 8),
        field("minimum-stake", 8048, 8),
        field("owner-uid", 8056, 2),
        field("tao-weight", 8064, 8),
        field("kappa", 8072, 2),
        field("bonds-penalty", 8074, 2),
        field("moving-average", 8080, 8),
    ];
    let settings = vec![
        field("netuid", 4000, 2),
        field("yuma3", 8100, 1),
        field("liquid-alpha", 8101, 1),
        field("commit-reveal", 8102, 1),
        field("consensus-mode", 8103, 1),
        field("alpha-low", 8104, 2),
        field("alpha-high", 8106, 2),
        field("steepness", 8108, 2),
        repeated("previous-consensus", 8500, 2, 8112),
    ];
    let node = vec![
        field("netuid", 4000, 2),
        field("uid", 8200, 2),
        field("hotkey", 8220, 32),
        field("registered", 8260, 8),
        field("last-update", 8268, 8),
        field("permit", 8276, 1),
        field("alpha", 8280, 8),
        field("tao", 8288, 8),
        field("commit-block", 8296, 8),
        repeated("parent-proportions", 8500, 8, 8112),
        repeated("parent-hotkeys", 8500, 32, 8112),
        repeated("parent-alpha", 8500, 8, 8112),
        repeated("parent-tao", 8500, 8, 8112),
        repeated("child-proportions", 8500, 8, 8112),
        repeated("child-hotkeys", 8500, 32, 8112),
    ];
    let row = vec![
        field("netuid", 4000, 2),
        field("uid", 8200, 2),
        repeated("columns", 8300, 2, 8320),
        repeated("values", 8310, 2, 8320),
    ];
    for (name, memory) in [
        ("meta", meta),
        ("settings", settings),
        ("node", node),
        ("weights", row.clone()),
        ("bonds", row),
    ] {
        let mut rule = observation_profile(
            code,
            &format!("original_yuma_{name}"),
            &format!("native-yuma-{name}"),
        )
        .rules
        .remove(0);
        rule.memory = memory;
        profile.rules.push(rule);
    }
}

#[test]
fn historical_native_yuma_exports_original_complete_branches() {
    let mut jobs = Vec::new();
    for mode in ["legacy", "yuma3", "liquid"] {
        let (job, _) = fixture_with_allocation(false, None, None, Some(mode));
        // Each branch must prove the exact drained state exposed by the public
        // RPC fixture. The original next-block Wasm then accrues both inputs.
        let parent: NativeHeader = scale_exact(
            "Yuma parent",
            &hex_bytes("Yuma parent", &job.parent_header_hex, MAXIMUM_HEADER_BYTES).unwrap(),
        )
        .unwrap();
        let nodes = job
            .proof_nodes_hex
            .iter()
            .map(|node| hex_bytes("Yuma proof node", node, MAXIMUM_CODE_BYTES).unwrap());
        let backend = create_proof_check_backend::<Blake2Hasher>(
            *parent.state_root(),
            StorageProof::new(nodes),
        )
        .unwrap();
        for item in [
            b"PendingServerEmission".as_slice(),
            b"PendingValidatorEmission".as_slice(),
            b"PendingRootAlphaDivs".as_slice(),
        ] {
            assert_eq!(
                backend
                    .storage(&key(b"SubtensorModule", item, true))
                    .unwrap(),
                Some(words(&[0])),
                "original Yuma activation parent is not actually drained: {mode}"
            );
        }
        let captured = super::super::capture_tests::collect(&job)
            .expect("actual complete original Yuma capture");
        let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
        let replayed = run(&exported).expect("actual complete original Yuma replay");
        assert!(captured.replay.post_state_reproduced && replayed.post_state_reproduced);
        let records = &replayed.hook_observations.as_ref().unwrap().observations;
        assert_eq!(
            records
                .iter()
                .filter(|record| record.purpose.starts_with("native-yuma-"))
                .count(),
            8,
            "original complete Yuma input records were omitted"
        );
        let epoch = records
            .iter()
            .find(|record| record.purpose == "native-epoch")
            .unwrap();
        let values = &epoch.native.as_ref().unwrap().memory;
        assert_eq!(
            values
                .iter()
                .find(|value| value.name == "total-alpha")
                .unwrap()
                .bytes_hex,
            encoded(&words(&[200])),
            "original Yuma next-block accrual differs: {mode}"
        );
        assert_eq!(
            values
                .iter()
                .find(|value| value.name == "emission")
                .unwrap()
                .bytes_hex,
            encoded(&words(&[9, 89]))
        );
        assert_eq!(
            values
                .iter()
                .find(|value| value.name == "incentive-q32")
                .unwrap()
                .bytes_hex,
            encoded(&words(&[429496729, 3865470566]))
        );
        jobs.push((mode, exported));
    }
    if let Some(directory) = std::env::var_os("URNETWORK_NATIVE_YUMA_FIXTURE_OUT") {
        let directory = Path::new(&directory);
        assert!(directory.is_absolute() && directory.is_dir());
        for (mode, job) in jobs {
            let mut file = OpenOptions::new()
                .create_new(true)
                .write(true)
                .mode(0o600)
                .open(directory.join(format!("yuma-{mode}.json")))
                .unwrap();
            file.write_all(&serde_json::to_vec(&job).unwrap()).unwrap();
            file.sync_all().unwrap();
        }
        std::fs::File::open(directory).unwrap().sync_all().unwrap();
    }
}
