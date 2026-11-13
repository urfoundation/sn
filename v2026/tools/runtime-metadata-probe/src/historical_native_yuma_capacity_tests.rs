//! The original Wasm emits every input row and epoch cell at realistic UID
//! sizes. Additional zero-stake UIDs are observed identities, never omissions.
use super::*;

pub(super) fn count(mode: Option<&str>) -> usize {
    match mode {
        Some("capacity-1024" | "populated-1024") => 1024,
        Some("capacity-2048" | "populated-2048") => 2048,
        _ => 2,
    }
}

pub(super) fn arrays(count: usize) -> Vec<(&'static str, u32, u32, usize)> {
    let mut address = 65536u32;
    [
        ("incentive-q32", 4100, 8),
        ("dividends-q32", 4120, 8),
        ("normalized-q32", 4140, 8),
        ("emission", 4160, 8),
        ("uids", 4180, 2),
        ("hotkeys", 4200, 32),
        ("registered", 4280, 8),
        ("stake-q32", 8600, 8),
        ("active-stake-q32", 8620, 8),
        ("consensus-q32", 8640, 8),
        ("validator-normalized-q32", 8660, 8),
        ("validator-emission", 8680, 8),
    ]
    .into_iter()
    .map(|(name, old, width)| {
        let current = address;
        address += (count * width) as u32;
        (name, old, current, width)
    })
    .collect()
}

pub(super) fn expand(count: usize, declarations: &mut String, body: &mut String) {
    assert!(count == 1024 || count == 2048);
    // Relocate complete vectors beyond scalar inputs; keep the allocator above
    // all declared bytes so storage host calls cannot overwrite original rows.
    for (_, old, address, width) in arrays(count) {
        for offset in (0..2 * width).step_by(2) {
            let before = format!("(i32.const {})", old + offset as u32);
            let after = format!("(i32.const {})", address + offset as u32);
            *declarations = declarations.replace(&before, &after);
            *body = body.replace(&before, &after);
        }
    }
    // Original event bytes include all zero allocations and must not overlap
    // the original scalar input records or any full epoch vector.
    *declarations = declarations.replace("(i32.const 4800)", "(i32.const 32768)");
    let event_bytes = 5 + codec::Compact(count as u32).encode().len() + count * 8 + 1;
    *declarations = declarations.replace(
        &format!("(i64.const {})", span(4800, event_bytes)),
        &format!("(i64.const {})", span(32768, event_bytes)),
    );
    declarations.push_str(&segment(8000, &(count as u16).to_le_bytes()));
    for (name, _, address, _) in arrays(count) {
        let bytes = match name {
            "uids" => (0..count)
                .flat_map(|uid| (uid as u16).to_le_bytes())
                .collect::<Vec<_>>(),
            "hotkeys" => (0..count)
                .flat_map(|uid| {
                    if uid < 2 {
                        vec![if uid == 0 { 0x11 } else { 0x22 }; 32]
                    } else {
                        let mut key = vec![0x44; 32];
                        key[30..].copy_from_slice(&(uid as u16).to_le_bytes());
                        key
                    }
                })
                .collect(),
            "registered" => words(
                &(0..count)
                    .map(|uid| if uid == 1 { 21 } else { 20 })
                    .collect::<Vec<_>>(),
            ),
            _ => continue,
        };
        declarations.push_str(&segment(address, &bytes));
    }
    *declarations = declarations
        .replace(
            "(i64.add (i64.const 20) (i64.extend_i32_u (local.get $uid)))",
            "(i64.add (i64.const 20) (i64.extend_i32_u (i32.eq (local.get $uid) (i32.const 1))))",
        )
        .replace(
            "(i32.store8 (i32.const 8276) (local.get $uid))",
            "(i32.store8 (i32.const 8276) (i32.eq (local.get $uid) (i32.const 1)))",
        )
        .replace(
            "(i64.mul (i64.const 100) (i64.extend_i32_u (local.get $uid)))",
            "(i64.mul (i64.const 100) (i64.extend_i32_u (i32.eq (local.get $uid) (i32.const 1))))",
        )
        .replace(
            "(i32.mul (local.get $uid) (i32.const 2))",
            "(i32.mul (i32.eq (local.get $uid) (i32.const 1)) (i32.const 2))",
        )
        .replace(
            "(i32.mul (i32.sub (i32.const 1) (local.get $uid)) (i32.const 2))",
            "(i32.mul (i32.eqz (local.get $uid)) (i32.const 2))",
        );
    let mut calls = String::new();
    for name in ["node", "weights", "bonds"] {
        calls.push_str(&format!(
            r#"
        (local.set $uid (i32.const 0))
        (loop $rows_{name}
          (call $yuma_{name} (local.get $uid))
          (local.set $uid (i32.add (local.get $uid) (i32.const 1)))
          (br_if $rows_{name} (i32.lt_u (local.get $uid) (i32.const {count}))))"#
        ));
    }
    declarations.push_str(&format!(
        "(func $yuma_full_inputs (local $uid i32) (call $yuma_meta) (call $yuma_settings) {calls})"
    ));
    *body = body.replace(yuma_tests::body(), "(call $yuma_full_inputs) (call $epoch)");
}

pub(super) fn profile(count: usize, profile: &mut observer::ObservationProfile) {
    let epoch = profile
        .rules
        .iter_mut()
        .find(|rule| rule.purpose == "native-epoch")
        .unwrap();
    for capture in &mut epoch.memory {
        if let Some((_, _, address, width)) = arrays(count)
            .into_iter()
            .find(|(name, _, _, _)| *name == capture.name)
        {
            capture.address = address;
            capture.bytes = (count * width) as u32;
        }
    }
}

#[test]
fn historical_native_yuma_exports_complete_1024_and_2048_uid_jobs() {
    for count in [1024usize, 2048] {
        let mode = format!("capacity-{count}");
        let (job, _) = fixture_with_allocation(false, None, None, Some(&mode));
        let header: NativeHeader = scale_exact(
            "large parent",
            &hex_bytes("large parent", &job.parent_header_hex, MAXIMUM_HEADER_BYTES).unwrap(),
        )
        .unwrap();
        let nodes = job
            .proof_nodes_hex
            .iter()
            .map(|node| hex_bytes("large node", node, MAXIMUM_CODE_BYTES).unwrap());
        let parent = create_proof_check_backend::<Blake2Hasher>(
            *header.state_root(),
            StorageProof::new(nodes),
        )
        .unwrap();
        for item in [
            b"PendingServerEmission".as_slice(),
            b"PendingValidatorEmission".as_slice(),
            b"PendingRootAlphaDivs".as_slice(),
        ] {
            assert_eq!(
                parent
                    .storage(&key(b"SubtensorModule", item, true))
                    .unwrap(),
                Some(words(&[0])),
                "large original Yuma parent is not drained"
            );
        }
        let captured = super::super::capture_tests::collect(&job).expect("actual full UID capture");
        let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
        let replayed = run(&exported).expect("actual full UID replay");
        for report in [&captured.replay, &replayed] {
            assert!(report.post_state_reproduced);
            let records = &report.hook_observations.as_ref().unwrap().observations;
            assert_eq!(
                records
                    .iter()
                    .filter(|record| record.purpose.starts_with("native-yuma-"))
                    .count(),
                2 + 3 * count,
                "complete full UID row census differs"
            );
            let epoch = records
                .iter()
                .find(|record| record.purpose == "native-epoch")
                .unwrap();
            let emitted = epoch
                .native
                .as_ref()
                .unwrap()
                .memory
                .iter()
                .find(|value| value.name == "emission")
                .unwrap();
            let mut expected = vec![0; count];
            expected[0] = 9;
            expected[1] = 89;
            assert_eq!(
                emitted.bytes_hex,
                encoded(&words(&expected)),
                "full UID original emission differs"
            );
        }
        if let Some(directory) = std::env::var_os("URNETWORK_NATIVE_YUMA_CAPACITY_FIXTURE_OUT") {
            let directory = Path::new(&directory);
            assert!(directory.is_absolute() && directory.is_dir());
            let mut file = OpenOptions::new()
                .create_new(true)
                .write(true)
                .mode(0o600)
                .open(directory.join(format!("yuma-{mode}.json")))
                .unwrap();
            file.write_all(&serde_json::to_vec(&exported).unwrap())
                .unwrap();
            file.sync_all().unwrap();
            std::fs::File::open(directory).unwrap().sync_all().unwrap();
        }
    }
}
