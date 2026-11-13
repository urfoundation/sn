//! Two ordinary credits, original stake causes and coldkey-wide availability
//! come from one captured original program. Synthetic identities and source
//! mechanics never constitute mainnet runtime or economic outcome admission.

use super::*;

const STAKES: [&[u8]; 2] = [b"synthetic-treasury-stake-a", b"synthetic-treasury-stake-b"];
const LOCK: &[u8] = b"synthetic-treasury-lock";
const COLLATERAL: &[u8] = b"synthetic-treasury-collateral";

fn multisig() -> [u8; 32] {
    // Independent pinned pallet_multisig SCALE tuple, not the Go helper.
    blake2_256(&(b"modlpy/utilisuba", vec![[0x31u8; 32], [0x32u8; 32]], 2u16).encode())
}

pub(super) fn queries() -> Vec<principal::PrincipalQuery> {
    [0x11, 0x22]
        .into_iter()
        .map(|byte| principal::PrincipalQuery {
            hotkey: [byte; 32],
            coldkey: multisig(),
            netuid: 25,
            availability: true,
        })
        .collect()
}

pub(super) fn initial(storage: &mut sp_core::storage::Storage) {
    for (key, amount) in STAKES.into_iter().zip([14, 17]) {
        storage.top.insert(key.to_vec(), words(&[amount]));
    }
    storage.top.insert(LOCK.to_vec(), words(&[2]));
    storage.top.insert(COLLATERAL.to_vec(), words(&[0]));
}

pub(super) fn expected(storage: &mut sp_core::storage::Storage) {
    for (key, amount) in STAKES.into_iter().zip([23, 106]) {
        storage.top.insert(key.to_vec(), words(&[amount]));
    }
    storage.top.insert(COLLATERAL.to_vec(), words(&[3]));
    storage.top.insert(
        b"synthetic-native-owner-recycle".to_vec(),
        words(&[89, 0, 89]),
    );
}

pub(super) fn program(declarations: &mut String, body: &mut String) {
    let coldkey = multisig();
    for (address, previous) in [(4500, [0x33; 32]), (4532, [0x34; 32])] {
        let old = segment(address, &previous);
        assert_eq!(declarations.matches(&old).count(), 1);
        *declarations = declarations.replace(&old, &segment(address, &coldkey));
    }
    let recycled = "(i64.store (i32.const 4392) (i64.load (i32.const 4368)))";
    assert_eq!(declarations.matches(recycled).count(), 1);
    *declarations = declarations.replace(recycled,
        "(i64.store (i32.const 4376) (i64.const 0)) (i64.store (i32.const 4384) (i64.load (i32.const 4368)))");
    let old = format!(
        "(call $set (i64.const {}) (i64.const {}))",
        span(1600, b"synthetic-native-owner-recycle".len()),
        span(4392, 8)
    );
    let new = format!(
        "(call $set (i64.const {}) (i64.const {}))",
        span(1600, b"synthetic-native-owner-recycle".len()),
        span(4368, 24)
    );
    assert_eq!(declarations.matches(&old).count(), 1);
    *declarations = declarations.replace(&old, &new);

    let mut stake = vec![1];
    stake.extend_from_slice(&[0x11; 32]);
    stake.extend_from_slice(&coldkey);
    stake.extend_from_slice(&[100, 0, 0, 0, 0, 0, 1]);
    let mut availability = vec![4];
    availability.extend_from_slice(&coldkey);
    availability.extend_from_slice(&[4, 25, 0]);
    for (address, value) in [
        (2000, STAKES[0].to_vec()),
        (2100, STAKES[1].to_vec()),
        (2200, LOCK.to_vec()),
        (2300, COLLATERAL.to_vec()),
        (5200, words(&[8])),
        (5210, vec![0x70; 32]),
        (5250, [vec![1], vec![0x71; 32]].concat()),
        (5300, vec![0x71; 32]),
        (5340, vec![0]),
        (5400, words(&[3])),
        (6000, stake),
        (6200, availability),
    ] {
        declarations.push_str(&segment(address, &value));
    }

    declarations.push_str(r#"
      (func $treasury_read (param $key i64) (result i64)
        (local $value i32)
        (local.set $value (i32.wrap_i64 (call $get (local.get $key))))
        (if (i32.ne (i32.load8_u (local.get $value)) (i32.const 1)) (then unreachable))
        (if (i32.ne (i32.load8_u offset=1 (local.get $value)) (i32.const 32)) (then unreachable))
        (i64.load offset=2 (local.get $value)))
      (func $treasury_compact (param $at i32) (param $value i64) (result i32)
        (if (result i32) (i64.lt_u (local.get $value) (i64.const 64))
          (then (i32.store8 (local.get $at) (i32.wrap_i64 (i64.shl (local.get $value) (i64.const 2)))) (i32.add (local.get $at) (i32.const 1)))
          (else (if (i64.ge_u (local.get $value) (i64.const 16384)) (then unreachable))
            (i32.store16 (local.get $at) (i32.or (i32.wrap_i64 (i64.shl (local.get $value) (i64.const 2))) (i32.const 1)))
            (i32.add (local.get $at) (i32.const 2)))))
      (func $treasury_sub (param $left i64) (param $right i64) (result i64)
        (if (result i64) (i64.gt_u (local.get $left) (local.get $right))
          (then (i64.sub (local.get $left) (local.get $right))) (else (i64.const 0))))
      (func $treasury_coldkey (param $at i32)
    "#);
    for offset in [0, 8, 16, 24] {
        declarations.push_str(&format!("(if (i64.ne (i64.load offset={offset} (local.get $at)) (i64.load (i32.const {}))) (then unreachable))", 4500 + offset));
    }
    declarations.push_str(")");
    declarations.push_str(&format!(r#"
      (func (export "StakeInfoRuntimeApi_get_stake_info_for_hotkey_coldkey_netuid") (param $args i32) (param $size i32) (result i64)
        (local $key i64) (local $pattern i64) (local $at i32)
        (if (i32.ne (local.get $size) (i32.const 66)) (then unreachable))
        (call $treasury_coldkey (i32.add (local.get $args) (i32.const 32)))
        (if (i32.ne (i32.load16_u offset=64 (local.get $args)) (i32.const 25)) (then unreachable))
        (if (i32.eq (i32.load8_u (local.get $args)) (i32.const 17))
          (then (local.set $key (i64.const {first})) (local.set $pattern (i64.const 1229782938247303441)))
          (else (if (i32.ne (i32.load8_u (local.get $args)) (i32.const 34)) (then unreachable))
            (local.set $key (i64.const {second})) (local.set $pattern (i64.const 2459565876494606882))))
    "#, first=span(2000, STAKES[0].len()), second=span(2100, STAKES[1].len())));
    for offset in [0, 8, 16, 24] {
        declarations.push_str(&format!("(if (i64.ne (i64.load offset={offset} (local.get $args)) (local.get $pattern)) (then unreachable))"));
    }
    for offset in [0, 8, 16, 24, 32, 40, 48, 56] {
        declarations.push_str(&format!(
            "(i64.store (i32.const {}) (i64.load offset={offset} (local.get $args)))",
            6001 + offset
        ));
    }
    declarations.push_str(r#"
        (local.set $at (call $treasury_compact (i32.const 6066) (call $treasury_read (local.get $key))))
        (i32.store (local.get $at) (i32.const 0))
        (i32.store8 offset=4 (local.get $at) (i32.const 1))
        (i64.or (i64.shl (i64.extend_i32_u (i32.sub (i32.add (local.get $at) (i32.const 5)) (i32.const 6000))) (i64.const 32)) (i64.const 6000)))
    "#);
    declarations.push_str(&format!(r#"
      (func (export "StakeInfoRuntimeApi_get_stake_availability_for_coldkeys") (param $args i32) (param $size i32) (result i64)
        (local $at i32) (local $total i64) (local $locked i64) (local $available i64)
        (if (i32.ne (local.get $size) (i32.const 37)) (then unreachable))
        (if (i32.ne (i32.load8_u (local.get $args)) (i32.const 4)) (then unreachable))
        (call $treasury_coldkey (i32.add (local.get $args) (i32.const 1)))
        (if (i32.ne (i32.load8_u offset=33 (local.get $args)) (i32.const 1)) (then unreachable))
        (if (i32.ne (i32.load8_u offset=34 (local.get $args)) (i32.const 4)) (then unreachable))
        (if (i32.ne (i32.load16_u offset=35 (local.get $args)) (i32.const 25)) (then unreachable))
        (local.set $total (i64.add (call $treasury_read (i64.const {first})) (call $treasury_read (i64.const {second}))))
        (local.set $locked (call $treasury_read (i64.const {locked})))
        (local.set $available (call $treasury_sub (call $treasury_sub (local.get $total) (local.get $locked)) (call $treasury_read (i64.const {collateral}))))
        (local.set $at (call $treasury_compact (i32.const 6236) (local.get $total)))
        (local.set $at (call $treasury_compact (local.get $at) (local.get $locked)))
        (local.set $at (call $treasury_compact (local.get $at) (local.get $available)))
        (i64.or (i64.shl (i64.extend_i32_u (i32.sub (local.get $at) (i32.const 6200))) (i64.const 32)) (i64.const 6200)))
    "#, first=span(2000,STAKES[0].len()), second=span(2100,STAKES[1].len()), locked=span(2200,LOCK.len()), collateral=span(2300,COLLATERAL.len())));
    for (index, key) in STAKES.iter().enumerate() {
        declarations.push_str(&format!(
            r#"
          (func $treasury_earning_{index} (export "treasury_earning_{index}") (param $amount i64)
            (i64.store (i32.const 6400) (call $treasury_read (i64.const {key})))
            (i64.store (i32.const 6408) (i64.add (i64.load (i32.const 6400)) (local.get $amount)))
            (call $set (i64.const {key}) (i64.const {value})))
        "#,
            key = span(2000 + index as u32 * 100, key.len()),
            value = span(6408, 8)
        ));
        body.push_str(&format!(
            "(call $treasury_earning_{index} (i64.load (i32.const {})))",
            4160 + index * 8
        ));
    }
    body.push_str(&format!(
        "(call $set (i64.const {}) (i64.const {}))",
        span(2300, COLLATERAL.len()),
        span(5400, 8)
    ));
}

pub(super) fn profile(code: &[u8], profile: &mut observer::ObservationProfile) {
    let field = |name: &str, address: u32, bytes: u32| observer::MemoryCapture {
        name: name.to_owned(),
        address,
        global: None,
        dereference_offsets: Vec::new(),
        bytes,
        repeat: None,
    };
    for rule in &mut profile.rules {
        if rule.purpose == "native-epoch" {
            rule.memory.push(field("subnet-epoch", 5200, 8));
        }
        if rule.purpose == "native-owner-recycle" {
            rule.purpose = "native-miner-credit".to_owned();
            rule.memory.retain(|value| value.name != "recycled");
            rule.memory
                .extend([field("captured", 4376, 8), field("liquid", 4384, 8)]);
        }
        if rule.purpose == "native-miner-credit" {
            rule.memory.extend([
                field("subnet-owner", 5210, 32),
                field("subnet-owner-hotkey", 5250, 33),
                field("owner-hotkeys", 5300, 32),
                field("auto-stake-destination", 5340, 1),
                field("stake-destination", 4300, 32),
            ]);
        }
    }
    profile.principal_storage_prefixes = Some(vec![encoded(b"synthetic-treasury-stake-")]);
    for index in 0..2 {
        let mut rule = observation_profile(
            code,
            &format!("treasury_earning_{index}"),
            "native-principal-earning",
        )
        .rules
        .remove(0);
        rule.memory = vec![
            field("netuid", 4000, 2),
            field("hotkey", 4200 + index * 32, 32),
            field("coldkey", 4500, 32),
            field("before", 6400, 8),
            field("after", 6408, 8),
        ];
        profile.rules.push(rule);
    }
}

fn availability_bytes(total: u64, available: u64) -> Vec<u8> {
    let mut raw = vec![4];
    raw.extend_from_slice(&multisig());
    raw.extend_from_slice(&[4, 25, 0]);
    for value in [total, 2, available] {
        raw.extend_from_slice(&codec::Compact(value).encode());
    }
    raw
}

#[test]
fn historical_native_treasury_exports_original_credit_and_custody_jobs() {
    let (original, _) = fixture_with_treasury(false, None, None, None, true, None, None, true);
    let captured =
        super::super::capture_tests::collect(&original).expect("original treasury capture");
    let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
    let replayed = run(&exported).expect("original treasury strict replay");
    for report in [&captured.replay, &replayed] {
        assert!(
            report.post_state_reproduced
                && !report.runtime_admitted
                && !report.production_selection
        );
        for (observations, amounts, total, available) in [
            (&report.opening_principals, [14u64, 17], 31, 29),
            (&report.closing_principals, [23u64, 106], 129, 124),
        ] {
            let observations = observations.as_ref().unwrap();
            assert_eq!(observations.len(), 2);
            for (index, observation) in observations.iter().enumerate() {
                assert_eq!(observation.query, queries()[index]);
                assert_eq!(
                    observation.opening_stake_alpha,
                    Some(amounts[index].to_string())
                );
                let value = observation.availability.as_ref().unwrap();
                assert_eq!(
                    value.result_hex,
                    encoded(&availability_bytes(total, available))
                );
                assert_eq!(value.available_alpha, Some(available.to_string()));
            }
        }
        let trace = report.hook_observations.as_ref().unwrap();
        assert_eq!(trace.principal_mutations.as_ref().unwrap().len(), 2);
        assert_eq!(trace.discarded_on_rollback, 0);
        let credits: Vec<_> = trace
            .observations
            .iter()
            .filter(|v| v.purpose == "native-miner-credit" && v.operation == "set")
            .collect();
        assert_eq!(credits.len(), 2);
        for (index, credit) in credits.iter().enumerate() {
            let native = credit.native.as_ref().unwrap();
            assert_eq!(native.execution_phase_hex.as_deref(), Some("0x02"));
            for (name, expected) in [
                ("hotkey", queries()[index].hotkey.to_vec()),
                ("coldkey", multisig().to_vec()),
                ("gross", words(&[[9, 89][index]])),
                ("captured", words(&[[3, 0][index]])),
                ("liquid", words(&[[6, 89][index]])),
                ("subnet-owner", vec![0x70; 32]),
                ("subnet-owner-hotkey", [vec![1], vec![0x71; 32]].concat()),
                ("owner-hotkeys", vec![0x71; 32]),
                ("auto-stake-destination", vec![0]),
                ("stake-destination", queries()[index].hotkey.to_vec()),
            ] {
                assert_eq!(
                    native
                        .memory
                        .iter()
                        .find(|v| v.name == name)
                        .unwrap()
                        .bytes_hex,
                    encoded(&expected),
                    "{name}"
                );
            }
        }
        let effects: Vec<_> = trace
            .observations
            .iter()
            .filter(|v| v.purpose == "native-principal-earning" && v.operation == "set")
            .collect();
        assert_eq!(effects.len(), 2);
        for (index, effect) in effects.iter().enumerate() {
            assert_eq!(effect.key_hex, encoded(STAKES[index]));
            assert_eq!(effect.value_hex, Some(encoded(&words(&[[23, 106][index]]))));
            let mutation = &trace.principal_mutations.as_ref().unwrap()[index];
            assert_eq!(mutation.ordinal, effect.ordinal);
            assert_eq!(
                mutation.value_sha256,
                Some(sha2_256(&words(&[[23, 106][index]])))
            );
        }
        assert!(trace
            .observations
            .iter()
            .all(|v| v.purpose != "native-owner-recycle"));
    }

    // Retain the real runtime and headers but omit every treasury dependency
    // from the produced parent proof; missing original state is never zero.
    let mut missing = exported.clone();
    let parent: NativeHeader = scale_exact(
        "treasury parent",
        &hex_bytes("parent", &missing.parent_header_hex, MAXIMUM_HEADER_BYTES).unwrap(),
    )
    .unwrap();
    let nodes = missing
        .proof_nodes_hex
        .iter()
        .map(|raw| hex_bytes("node", raw, MAXIMUM_CODE_BYTES).unwrap());
    let backend =
        create_proof_check_backend::<Blake2Hasher>(*parent.state_root(), StorageProof::new(nodes))
            .unwrap();
    let mut keys = vec![
        well_known_keys::CODE.to_vec(),
        well_known_keys::HEAP_PAGES.to_vec(),
        key(b"System", b"ExecutionPhase", false),
        key(b"System", b"Events", false),
    ];
    for item in [
        b"PendingServerEmission".as_slice(),
        b"PendingValidatorEmission".as_slice(),
        b"PendingRootAlphaDivs".as_slice(),
    ] {
        keys.push(key(b"SubtensorModule", item, true));
    }
    missing.proof_nodes_hex = prove_read_on_trie_backend(&backend, keys)
        .unwrap()
        .into_iter_nodes()
        .map(|node| encoded(&node))
        .collect();
    for result in [
        super::super::capture_tests::collect(&missing).map(|_| ()),
        run(&missing).map(|_| ()),
    ] {
        let error = result.expect_err("treasury missing original custody proof was accepted");
        assert!(error.to_string().contains("principal"), "{error}");
    }
    if let Some(directory) = std::env::var_os("URNETWORK_NATIVE_TREASURY_FIXTURE_OUT") {
        let directory = Path::new(&directory);
        assert!(directory.is_absolute() && directory.is_dir());
        for (name, job) in [
            ("treasury-credit.json", exported),
            ("treasury-missing-original.json", missing),
        ] {
            let mut file = OpenOptions::new()
                .create_new(true)
                .write(true)
                .mode(0o600)
                .open(directory.join(name))
                .expect("exclusive treasury fixture export");
            file.write_all(&serde_json::to_vec(&job).unwrap()).unwrap();
            file.sync_all().unwrap();
        }
        std::fs::File::open(directory).unwrap().sync_all().unwrap();
    }
}
