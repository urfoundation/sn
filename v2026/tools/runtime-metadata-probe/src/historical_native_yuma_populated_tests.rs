//! Complete populated original-Wasm witnesses. Every UID has nonzero raw stake
//! and one original parent/child share; 64 permitted rows each carry all N
//! nonzero weights and bonds. Symmetric inputs allow an independent compact
//! MVP implementation without importing the Go allocation model.
use super::*;

pub(super) fn selected(mode: Option<&str>) -> bool {
    matches!(mode, Some("populated-1024" | "populated-2048"))
}

fn replace_function(text: &mut String, name: &str, replacement: &str) {
    let start = text.find(&format!("(func ${name}")).unwrap();
    let mut depth = 0i32;
    let mut end = None;
    for (offset, value) in text[start..].char_indices() {
        match value {
            '(' => depth += 1,
            ')' => depth -= 1,
            _ => {}
        }
        if depth == 0 {
            end = Some(start + offset + 1);
            break;
        }
    }
    text.replace_range(start..end.unwrap(), replacement);
}

fn address(count: usize, name: &str) -> u32 {
    yuma_capacity_tests::arrays(count)
        .into_iter()
        .find(|item| item.0 == name)
        .unwrap()
        .2
}

pub(super) fn expand(count: usize, declarations: &mut String, body: &mut String) {
    let stake = address(count, "stake-q32");
    let active = address(count, "active-stake-q32");
    let consensus = address(count, "consensus-q32");
    let incentive = address(count, "incentive-q32");
    let dividend = address(count, "dividends-q32");
    let normalized = address(count, "normalized-q32");
    let emission = address(count, "emission");
    let validator = address(count, "validator-normalized-q32");
    let validator_emission = address(count, "validator-emission");
    let hotkeys = address(count, "hotkeys");
    let registered = address(count, "registered");
    declarations.push_str(&segment(1850, &words(&[count as u64 * 512])));
    declarations.push_str(&segment(8074, &32768u16.to_le_bytes()));
    declarations.push_str(&segment(318128, &words(&[u64::MAX / 2, 100, 10])));
    declarations.push_str(&segment(318152, &1u32.to_le_bytes()));
    declarations.push_str(&segment(319000, &[0x55; 32]));
    declarations.push_str(&segment(
        302000,
        &(0..count)
            .flat_map(|uid| (uid as u16).to_le_bytes())
            .collect::<Vec<_>>(),
    ));
    let storage_span = span(8700, b"synthetic-original-yuma-input".len());
    replace_function(
        declarations,
        "yuma_node",
        &format!(
            r#"
      (func $yuma_node (export "original_yuma_node") (param $uid i32)
        (local $hotkey i32) (local $parent i32) (local $child i32)
        (i32.store16 (i32.const 8200) (local.get $uid))
        (local.set $hotkey (i32.add (i32.const {hotkeys}) (i32.mul (local.get $uid) (i32.const 32))))
        (local.set $parent (i32.add (i32.const {hotkeys}) (i32.mul (i32.rem_u (i32.add (local.get $uid) (i32.const {previous})) (i32.const {count})) (i32.const 32))))
        (local.set $child (i32.add (i32.const {hotkeys}) (i32.mul (i32.rem_u (i32.add (local.get $uid) (i32.const 1)) (i32.const {count})) (i32.const 32))))
        {copies}
        (i64.store (i32.const 8260) (i64.load (i32.add (i32.const {registered}) (i32.mul (local.get $uid) (i32.const 8)))))
        (i64.store (i32.const 8268) (i64.const 99))
        (i32.store8 (i32.const 8276) (i32.lt_u (local.get $uid) (i32.const 64)))
        (i64.store (i32.const 8280) (i64.const 100))
        (i64.store (i32.const 8288) (i64.const 10))
        (drop (call $get (i64.const {storage_span}))))"#,
            previous = count - 1,
            copies = [("hotkey", 8220u32), ("parent", 318000), ("child", 318064)]
                .into_iter()
                .flat_map(|(name, to)| (0..32).step_by(8).map(move |offset| format!(
                    "(i64.store (i32.const {}) (i64.load offset={offset} (local.get ${name})))",
                    to + offset
                )))
                .collect::<String>()
        ),
    );
    for (name, value) in [("weights", 3), ("bonds", 2)] {
        replace_function(
            declarations,
            &format!("yuma_{name}"),
            &format!(
                r#"
          (func $yuma_{name} (export "original_yuma_{name}") (param $uid i32)
            (local $column i32)
            (i32.store16 (i32.const 8200) (local.get $uid))
            (i32.store (i32.const 8320) (i32.mul (i32.lt_u (local.get $uid) (i32.const 64)) (i32.const {count})))
            (if (i32.lt_u (local.get $uid) (i32.const 64)) (then
              (loop $columns
                (i32.store16 (i32.add (i32.const 307000) (i32.mul (local.get $column) (i32.const 2))) (i32.const {value}))
                (local.set $column (i32.add (local.get $column) (i32.const 1)))
                (br_if $columns (i32.lt_u (local.get $column) (i32.const {count}))))))
            (drop (call $get (i64.const {storage_span}))))"#
            ),
        );
    }
    // Source-normalized nonowner rows each have N-1 equal weights because
    // their self edge is masked. Owner row1 retains all N equal weights.
    // The 64 equal positive validator stakes are exactly representable; the
    // source-order median is the nonowner weight at every column. Thus clipping
    // is identity, even at the original nontrivial 32768/65535 bond penalty.
    // Legacy alpha=0 retains the complete old uniform 64-row bonds; after
    // column normalization each validator dividend is exactly 1/64. All fixed
    // multiplies/divides below retain the original Q32 truncation boundaries.
    replace_function(
        declarations,
        "yuma_compute",
        &format!(
            r#"
      (func $yuma_compute
        (local $column i32) (local $rank i64) (local $sum i64) (local $sum_incentive i64)
        (local $wa i64) (local $wb i64) (local $value i64) (local $offset i32) (local $validators i64)
        (local.set $wa (call $yuma_div (i64.const 1) (i64.const {nonowner_columns})))
        (local.set $wb (call $yuma_div (i64.const 1) (i64.const {count})))
        (loop $ranks
          (local.set $offset (i32.mul (local.get $column) (i32.const 8)))
          (i64.store (i32.add (i32.const {stake}) (local.get $offset)) (local.get $wb))
          (i64.store (i32.add (i32.const {active}) (local.get $offset)) (i64.mul (i64.extend_i32_u (i32.lt_u (local.get $column) (i32.const 64))) (i64.const 67108864)))
          (i64.store (i32.add (i32.const {consensus}) (local.get $offset)) (local.get $wa))
          (local.set $validators (i64.sub (i64.const 63) (i64.extend_i32_u (i32.and (i32.lt_u (local.get $column) (i32.const 64)) (i32.ne (local.get $column) (i32.const 1))))))
          (local.set $rank (i64.add (i64.mul (local.get $validators) (i64.shr_u (local.get $wa) (i64.const 6))) (i64.shr_u (local.get $wb) (i64.const 6))))
          (i64.store (i32.add (i32.const {incentive}) (local.get $offset)) (local.get $rank))
          (local.set $sum (i64.add (local.get $sum) (local.get $rank)))
          (local.set $column (i32.add (local.get $column) (i32.const 1)))
          (br_if $ranks (i32.lt_u (local.get $column) (i32.const {count}))))
        (local.set $column (i32.const 0))
        (loop $incentives
          (local.set $offset (i32.mul (local.get $column) (i32.const 8)))
          (local.set $value (call $yuma_div (i64.load (i32.add (i32.const {incentive}) (local.get $offset))) (local.get $sum)))
          (i64.store (i32.add (i32.const {incentive}) (local.get $offset)) (local.get $value))
          (local.set $sum_incentive (i64.add (local.get $sum_incentive) (local.get $value)))
          (local.set $column (i32.add (local.get $column) (i32.const 1)))
          (br_if $incentives (i32.lt_u (local.get $column) (i32.const {count}))))
        (local.set $column (i32.const 0))
        (loop $allocations
          (local.set $offset (i32.mul (local.get $column) (i32.const 8)))
          (local.set $value (call $yuma_div (i64.load (i32.add (i32.const {incentive}) (local.get $offset))) (i64.add (local.get $sum_incentive) (i64.const 4294967296))))
          (i64.store (i32.add (i32.const {normalized}) (local.get $offset)) (local.get $value))
          (i64.store (i32.add (i32.const {emission}) (local.get $offset)) (i64.shr_u (i64.mul (i64.const {total}) (local.get $value)) (i64.const 32)))
          (local.set $value (i64.mul (i64.extend_i32_u (i32.lt_u (local.get $column) (i32.const 64))) (i64.const 67108864)))
          (i64.store (i32.add (i32.const {dividend}) (local.get $offset)) (local.get $value))
          (local.set $value (call $yuma_div (local.get $value) (i64.add (local.get $sum_incentive) (i64.const 4294967296))))
          (i64.store (i32.add (i32.const {validator}) (local.get $offset)) (local.get $value))
          (i64.store (i32.add (i32.const {validator_emission}) (local.get $offset)) (i64.shr_u (i64.mul (i64.const {total}) (local.get $value)) (i64.const 32)))
          (local.set $column (i32.add (local.get $column) (i32.const 1)))
          (br_if $allocations (i32.lt_u (local.get $column) (i32.const {count})))) )"#,
            nonowner_columns = count - 1,
            total = count * 1024
        ),
    );
    replace_function(
        declarations,
        "epoch",
        &format!(
            r#"
      (func $epoch (export "native_epoch")
        (call $yuma_compute)
        (i64.store (i32.const 4048) (i64.add (i64.add (i64.load (i32.const 4024)) (i64.load (i32.const 4032))) (i64.load (i32.const 4040))))
        (call $set (i64.const {}) (i64.const {})))"#,
            span(1700, b"synthetic-native-epoch".len()),
            span(4048, 8)
        ),
    );
    let event_bytes = 5 + codec::Compact(count as u32).encode().len() + count * 8 + 1;
    replace_function(
        declarations,
        "emission",
        &format!(
            r#"
      (func $emission (export "native_emission") (local $column i32)
        (loop $events
          (i64.store (i32.add (i32.const {first}) (i32.mul (local.get $column) (i32.const 8))) (i64.load (i32.add (i32.const {emission}) (i32.mul (local.get $column) (i32.const 8)))))
          (local.set $column (i32.add (local.get $column) (i32.const 1)))
          (br_if $events (i32.lt_u (local.get $column) (i32.const {count}))))
        (call $append (i64.const {key}) (i64.const {value})))"#,
            first = 32768 + 5 + codec::Compact(count as u32).encode().len(),
            key = span(1400, key(b"System", b"Events", false).len()),
            value = span(32768, event_bytes)
        ),
    );
    declarations.push_str(&format!(r#"
      (func $populated_tail (export "native_populated_tail") (param $uid i32) (local $hotkey i32)
        (local.set $hotkey (i32.add (i32.const {hotkeys}) (i32.mul (local.get $uid) (i32.const 32))))
        {copies}
        (i64.store (i32.const 4368) (i64.load (i32.add (i32.const {emission}) (i32.mul (local.get $uid) (i32.const 8)))))
        (i64.store (i32.const 4376) (i64.const 0))
        (i64.store (i32.const 4384) (i64.load (i32.const 4368)))
        (call $set (i64.const {key_span}) (i64.const {value_span})))
      (func $populated_tails (local $uid i32)
        (local.set $uid (i32.const 2))
        (loop $recipients
          (call $populated_tail (local.get $uid))
          (local.set $uid (i32.add (local.get $uid) (i32.const 1)))
          (br_if $recipients (i32.lt_u (local.get $uid) (i32.const {count})))) )"#,
        copies=(0..32).step_by(8).map(|offset|format!("(i64.store (i32.const {}) (i64.load offset={offset} (local.get $hotkey))) (i64.store (i32.const {}) (i64.load (i32.const {})))",4300+offset,4332+offset,319000+offset)).collect::<String>(),key_span=span(4300,32),value_span=span(4368,24)));
    *body = body.replace("(call $owner)", "(call $owner) (call $populated_tails)");
}

// Independent expected storage uses widened host integers; the actual Wasm
// only sees original inputs and performs its own MVP fixed operations. Public
// Go qualification compares every stage against the general full source model.
pub(super) fn expected(count: usize, storage: &mut sp_core::storage::Storage) {
    let one = 1u128 << 32;
    let wa = one / (count as u128 - 1);
    let wb = one / count as u128;
    let ranks = (0..count)
        .map(|uid| (if uid < 64 && uid != 1 { 62 } else { 63 }) * (wa >> 6) + (wb >> 6))
        .collect::<Vec<_>>();
    let rank_sum: u128 = ranks.iter().sum();
    let incentives = ranks
        .iter()
        .map(|value| (value << 32) / rank_sum)
        .collect::<Vec<_>>();
    let incentive_sum: u128 = incentives.iter().sum();
    let emitted = incentives
        .iter()
        .map(|value| {
            ((((value << 32) / (incentive_sum + one)) * count as u128 * 1024) >> 32) as u64
        })
        .collect::<Vec<_>>();
    storage.top.insert(
        b"synthetic-native-epoch".to_vec(),
        words(&[count as u64 * 1024]),
    );
    storage.top.insert(
        b"synthetic-native-provider-credit".to_vec(),
        words(&[emitted[0], 3, emitted[0] - 3]),
    );
    storage.top.insert(
        b"synthetic-native-owner-recycle".to_vec(),
        words(&[emitted[1]]),
    );
    let mut event = vec![4, 2, 7, 250, 25, 0];
    event.extend_from_slice(&codec::Compact(count as u32).encode());
    event.extend_from_slice(&words(&emitted));
    event.push(0);
    storage.top.insert(key(b"System", b"Events", false), event);
    for uid in 2..count {
        let mut hotkey = vec![0x44; 32];
        hotkey[30..].copy_from_slice(&(uid as u16).to_le_bytes());
        storage
            .top
            .insert(hotkey, words(&[emitted[uid], 0, emitted[uid]]));
    }
}

pub(super) fn profile(code: &[u8], profile: &mut observer::ObservationProfile) {
    for rule in &mut profile.rules {
        if rule.purpose == "native-yuma-node" {
            for capture in &mut rule.memory {
                capture.address = match capture.name.as_str() {
                    "parent-hotkeys" => 318000,
                    "child-hotkeys" => 318064,
                    "parent-proportions" | "child-proportions" => 318128,
                    "parent-alpha" => 318136,
                    "parent-tao" => 318144,
                    _ => continue,
                };
                capture.repeat.as_mut().unwrap().count.address = 318152;
            }
        }
        if rule.purpose == "native-yuma-weights" || rule.purpose == "native-yuma-bonds" {
            for capture in &mut rule.memory {
                if capture.name == "columns" {
                    capture.address = 302000;
                }
                if capture.name == "values" {
                    capture.address = 307000;
                }
            }
        }
    }
    let memory = profile
        .rules
        .iter()
        .find(|rule| rule.purpose == "native-miner-credit")
        .unwrap()
        .memory
        .clone();
    let mut rule = observation_profile(code, "native_populated_tail", "native-miner-credit")
        .rules
        .remove(0);
    rule.memory = memory;
    profile.rules.push(rule);
}

#[test]
fn historical_native_yuma_exports_populated_64_by_1024_and_2048() {
    for count in [1024usize, 2048] {
        let mode = format!("populated-{count}");
        let (job, _) = fixture_with_allocation(false, None, None, Some(&mode));
        let header: NativeHeader = scale_exact(
            "populated parent",
            &hex_bytes("parent", &job.parent_header_hex, MAXIMUM_HEADER_BYTES).unwrap(),
        )
        .unwrap();
        let nodes = job
            .proof_nodes_hex
            .iter()
            .map(|node| hex_bytes("populated node", node, MAXIMUM_CODE_BYTES).unwrap());
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
                "populated original parent must be drained"
            );
        }
        let captured =
            super::super::capture_tests::collect(&job).expect("actual populated original capture");
        let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
        let replayed = run(&exported).expect("actual populated original replay");
        let mut emitted = Vec::new();
        for report in [&captured.replay, &replayed] {
            assert!(report.post_state_reproduced);
            let records = &report.hook_observations.as_ref().unwrap().observations;
            assert_eq!(
                records
                    .iter()
                    .filter(|record| record.purpose.starts_with("native-yuma-"))
                    .count(),
                2 + 3 * count,
                "populated original complete row census"
            );
            for record in records
                .iter()
                .filter(|record| record.purpose == "native-yuma-node")
            {
                let memory = &record.native.as_ref().unwrap().memory;
                for (name, value) in [("alpha", 100), ("tao", 10)] {
                    assert_eq!(
                        memory
                            .iter()
                            .find(|field| field.name == name)
                            .unwrap()
                            .bytes_hex,
                        encoded(&words(&[value])),
                        "populated original nonzero stake"
                    );
                }
                for name in ["parent-proportions", "child-proportions"] {
                    assert_eq!(
                        memory
                            .iter()
                            .find(|field| field.name == name)
                            .unwrap()
                            .bytes_hex,
                        encoded(&words(&[u64::MAX / 2])),
                        "populated original inherited shares"
                    );
                }
            }
            for purpose in ["native-yuma-weights", "native-yuma-bonds"] {
                let edges: usize = records
                    .iter()
                    .filter(|record| record.purpose == purpose)
                    .map(|record| {
                        record
                            .native
                            .as_ref()
                            .unwrap()
                            .memory
                            .iter()
                            .find(|field| field.name == "columns")
                            .unwrap()
                            .element_count
                            .unwrap() as usize
                    })
                    .sum();
                assert_eq!(edges, 64 * count, "populated original matrix edge census");
            }
            let epoch = records
                .iter()
                .find(|record| record.purpose == "native-epoch")
                .unwrap();
            let raw = hex_bytes(
                "populated emission",
                &epoch
                    .native
                    .as_ref()
                    .unwrap()
                    .memory
                    .iter()
                    .find(|field| field.name == "emission")
                    .unwrap()
                    .bytes_hex,
                8 * count,
            )
            .unwrap();
            let amounts = raw
                .chunks_exact(8)
                .map(|raw| u64::from_le_bytes(raw.try_into().unwrap()))
                .collect::<Vec<_>>();
            assert_eq!(amounts.len(), count);
            assert!(
                amounts.iter().all(|value| *value > 0),
                "populated original allocation may not be padded with zero recipients"
            );
            assert_eq!(
                records
                    .iter()
                    .filter(|record| record.purpose == "native-miner-credit"
                        || record.purpose == "native-owner-recycle")
                    .count(),
                count,
                "every nonzero original recipient must execute"
            );
            if emitted.is_empty() {
                emitted = amounts;
            } else {
                assert_eq!(
                    emitted, amounts,
                    "capture/replay populated original allocations differ"
                );
            }
        }
        if let Some(directory) = std::env::var_os("URNETWORK_NATIVE_YUMA_POPULATED_FIXTURE_OUT") {
            let directory = Path::new(&directory);
            assert!(directory.is_absolute() && directory.is_dir());
            let expectation = serde_json::json!({"runtime_code_sha256":exported.runtime_code_sha256,"uids":count,"validators":64,"tranche":count as u64*512,"emissions":emitted});
            for (name, bytes) in [
                (
                    format!("yuma-{mode}.json"),
                    serde_json::to_vec(&exported).unwrap(),
                ),
                (
                    format!("yuma-{mode}-original-events.json"),
                    serde_json::to_vec(&expectation).unwrap(),
                ),
            ] {
                let mut file = OpenOptions::new()
                    .create_new(true)
                    .write(true)
                    .mode(0o600)
                    .open(directory.join(name))
                    .unwrap();
                file.write_all(&bytes).unwrap();
                file.sync_all().unwrap();
            }
            std::fs::File::open(directory).unwrap().sync_all().unwrap();
        }
    }
}
