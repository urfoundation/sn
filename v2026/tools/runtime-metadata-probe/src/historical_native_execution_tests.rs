//! Real original-Wasm fixture for the Go execution/accounting boundary. The
//! program computes normalization and recipients; the host never accepts a
//! supplied amount report. These bytes carry no deployed-runtime authority.

#[path = "historical_native_yuma_tests.rs"]
mod yuma_tests;

#[path = "historical_native_yuma_capacity_tests.rs"]
mod yuma_capacity_tests;

#[path = "historical_native_yuma_populated_tests.rs"]
mod yuma_populated_tests;

#[path = "historical_native_capture_join_tests.rs"]
mod capture_join_tests;

#[path = "historical_native_fee_census_tests.rs"]
mod fee_census_tests;

#[path = "historical_native_runtime_renewal_tests.rs"]
mod runtime_renewal_tests;

#[path = "historical_native_treasury_tests.rs"]
mod treasury_tests;

use super::*;
use parity_scale_codec as codec;
use std::{fs::OpenOptions, io::Write, os::unix::fs::OpenOptionsExt, path::Path};

fn key(pallet: &[u8], item: &[u8], subnet: bool) -> Vec<u8> {
    let mut value = [
        sp_core::hashing::twox_128(pallet),
        sp_core::hashing::twox_128(item),
    ]
    .concat();
    if subnet {
        value.extend_from_slice(&25u16.to_le_bytes());
    }
    value
}

fn segment(address: u32, bytes: &[u8]) -> String {
    let escaped = bytes
        .iter()
        .map(|byte| format!("\\{byte:02x}"))
        .collect::<String>();
    format!("(data (i32.const {address}) \"{escaped}\")")
}

fn span(address: u32, length: usize) -> u64 {
    (length as u64) << 32 | u64::from(address)
}

fn words(values: &[u64]) -> Vec<u8> {
    values
        .iter()
        .flat_map(|value| value.to_le_bytes())
        .collect()
}

fn fixture() -> HistoricalJob {
    fixture_with_continuation(false).0
}

fn fixture_with_continuation(continuous: bool) -> (HistoricalJob, sp_core::storage::Storage) {
    fixture_with_principal(continuous, None)
}

fn fixture_with_principal(
    continuous: bool,
    principal: Option<Option<u64>>,
) -> (HistoricalJob, sp_core::storage::Storage) {
    fixture_with_principal_activation(continuous, principal, principal.is_some())
}

fn fixture_with_principal_effects(
    continuous: bool,
    principal: Option<Option<u64>>,
    effects: Option<&str>,
) -> (HistoricalJob, sp_core::storage::Storage) {
    fixture_with_allocation(continuous, principal, effects, None)
}

fn fixture_with_principal_activation(
    continuous: bool,
    principal: Option<Option<u64>>,
    drained_activation: bool,
) -> (HistoricalJob, sp_core::storage::Storage) {
    fixture_with_allocation_activation(continuous, principal, None, None, drained_activation)
}

fn fixture_with_allocation(
    continuous: bool,
    principal: Option<Option<u64>>,
    effects: Option<&str>,
    yuma: Option<&str>,
) -> (HistoricalJob, sp_core::storage::Storage) {
    fixture_with_allocation_activation(
        continuous,
        principal,
        effects,
        yuma,
        principal.is_some() || yuma.is_some(),
    )
}

fn fixture_with_allocation_activation(
    continuous: bool,
    principal: Option<Option<u64>>,
    effects: Option<&str>,
    yuma: Option<&str>,
    drained_activation: bool,
) -> (HistoricalJob, sp_core::storage::Storage) {
    fixture_with_fee_census(
        continuous,
        principal,
        effects,
        yuma,
        drained_activation,
        None,
    )
}

// Optional fee observations leave every existing original fixture unchanged.
fn fixture_with_fee_census(
    continuous: bool,
    principal: Option<Option<u64>>,
    effects: Option<&str>,
    yuma: Option<&str>,
    drained_activation: bool,
    fee_mode: Option<&str>,
) -> (HistoricalJob, sp_core::storage::Storage) {
    fixture_with_runtime_upgrade(
        continuous,
        principal,
        effects,
        yuma,
        drained_activation,
        fee_mode,
        None,
    )
}

// The renewal fixture's first program writes the complete next original Wasm
// only after the independently proved first epoch. Existing fixtures are exact.
fn fixture_with_runtime_upgrade(
    continuous: bool,
    principal: Option<Option<u64>>,
    effects: Option<&str>,
    yuma: Option<&str>,
    drained_activation: bool,
    fee_mode: Option<&str>,
    upgrade: Option<&[u8]>,
) -> (HistoricalJob, sp_core::storage::Storage) {
    fixture_with_treasury(
        continuous,
        principal,
        effects,
        yuma,
        drained_activation,
        fee_mode,
        upgrade,
        false,
    )
}

// The explicit treasury fixture leaves every old program and profile unchanged.
fn fixture_with_treasury(
    continuous: bool,
    principal: Option<Option<u64>>,
    effects: Option<&str>,
    yuma: Option<&str>,
    drained_activation: bool,
    fee_mode: Option<&str>,
    upgrade: Option<&[u8]>,
    treasury: bool,
) -> (HistoricalJob, sp_core::storage::Storage) {
    let allocation_count = yuma_capacity_tests::count(yuma);
    let capture = effects
        .map(|mode| mode.starts_with("capture"))
        .unwrap_or(false);
    let drains = [
        key(b"SubtensorModule", b"PendingServerEmission", true),
        key(b"SubtensorModule", b"PendingValidatorEmission", true),
        key(b"SubtensorModule", b"PendingRootAlphaDivs", true),
    ];
    let phase = key(b"System", b"ExecutionPhase", false);
    let events = key(b"System", b"Events", false);
    let provider = b"synthetic-native-provider-credit";
    let owner = b"synthetic-native-owner-recycle";
    let epoch = b"synthetic-native-epoch";
    // Synthetic event indices are deliberately fixture-local. The consumer
    // join below checks accounting; public metadata traversal has its own tests.
    let mut event = vec![2, 7, 250, 25, 0];
    event.extend_from_slice(&codec::Compact(allocation_count as u32).encode());
    let mut amounts = vec![0; allocation_count];
    amounts[0] = 9;
    amounts[1] = 89;
    event.extend_from_slice(&words(&amounts));
    event.push(0);
    let zero = vec![0; 8];
    let mut declarations =
        "(import \"env\" \"ext_storage_append_version_1\" (func $append (param i64 i64)))"
            .to_owned();
    let data: Vec<(u32, Vec<u8>)> = vec![
        (1000, drains[0].clone()),
        (1100, drains[1].clone()),
        (1200, drains[2].clone()),
        (1400, events.clone()),
        (1500, provider.to_vec()),
        (1600, owner.to_vec()),
        (1700, epoch.to_vec()),
        (1800, zero),
        (4000, vec![25, 0]),
        (4008, words(&[10, 3])),
        (4100, words(&[429496729, 3865470567])),
        (4120, words(&[1 << 32, 0])),
        (4180, vec![0, 0, 1, 0]),
        (4200, vec![0x11; 32]),
        (4232, vec![0x22; 32]),
        (4280, words(&[20, 21])),
        (4500, vec![0x33; 32]),
        (4532, vec![0x34; 32]),
        (4800, event.clone()),
    ];
    for (address, bytes) in data {
        declarations.push_str(&segment(address, &bytes));
    }
    // The pinned node excludes bulk-memory instructions. These fixed 32-byte
    // identity copies use MVP loads/stores over known nonoverlapping ranges.
    declarations.push_str(&format!(r#"
        (func $drain (export "native_drain") (param $key i64) (result i64) (call $get (local.get $key)))
        (func $epoch (export "native_epoch")
            (i64.store (i32.const 4048) (i64.add (i64.add (i64.load (i32.const 4024)) (i64.load (i32.const 4032))) (i64.load (i32.const 4040))))
            (i64.store (i32.const 4140) (i64.div_u (i64.shl (i64.load (i32.const 4100)) (i64.const 32)) (i64.const 8589934592)))
            (i64.store (i32.const 4148) (i64.div_u (i64.shl (i64.load (i32.const 4108)) (i64.const 32)) (i64.const 8589934592)))
            (i64.store (i32.const 4160) (i64.shr_u (i64.mul (i64.load (i32.const 4048)) (i64.load (i32.const 4140))) (i64.const 32)))
            (i64.store (i32.const 4168) (i64.shr_u (i64.mul (i64.load (i32.const 4048)) (i64.load (i32.const 4148))) (i64.const 32)))
            (call $set (i64.const {epoch_key}) (i64.const {epoch_value})))
        (func $emission (export "native_emission")
            (i64.store (i32.const 4806) (i64.load (i32.const 4160)))
            (i64.store (i32.const 4814) (i64.load (i32.const 4168)))
            (call $append (i64.const {events_key}) (i64.const {event_value})))
        (func $provider (export "native_provider")
            (i64.store (i32.const 4300) (i64.load (i32.const 4200)))
            (i64.store (i32.const 4308) (i64.load (i32.const 4208)))
            (i64.store (i32.const 4316) (i64.load (i32.const 4216)))
            (i64.store (i32.const 4324) (i64.load (i32.const 4224)))
            (i64.store (i32.const 4332) (i64.load (i32.const 4500)))
            (i64.store (i32.const 4340) (i64.load (i32.const 4508)))
            (i64.store (i32.const 4348) (i64.load (i32.const 4516)))
            (i64.store (i32.const 4356) (i64.load (i32.const 4524)))
            (i64.store (i32.const 4368) (i64.load (i32.const 4160)))
            (i64.store (i32.const 4376) (i64.const 3))
            (i64.store (i32.const 4384) (i64.sub (i64.load (i32.const 4368)) (i64.load (i32.const 4376))))
            (call $set (i64.const {provider_key}) (i64.const {provider_value})))
        (func $owner (export "native_owner")
            (i64.store (i32.const 4300) (i64.load (i32.const 4232)))
            (i64.store (i32.const 4308) (i64.load (i32.const 4240)))
            (i64.store (i32.const 4316) (i64.load (i32.const 4248)))
            (i64.store (i32.const 4324) (i64.load (i32.const 4256)))
            (i64.store (i32.const 4332) (i64.load (i32.const 4532)))
            (i64.store (i32.const 4340) (i64.load (i32.const 4540)))
            (i64.store (i32.const 4348) (i64.load (i32.const 4548)))
            (i64.store (i32.const 4356) (i64.load (i32.const 4556)))
            (i64.store (i32.const 4368) (i64.load (i32.const 4168)))
            (i64.store (i32.const 4392) (i64.load (i32.const 4368)))
            (call $set (i64.const {owner_key}) (i64.const {owner_value})))"#,
        epoch_key=span(1700,epoch.len()), epoch_value=span(4048,8), events_key=span(1400,events.len()), event_value=span(4800,event.len()), provider_key=span(1500,provider.len()),provider_value=span(4368,24),owner_key=span(1600,owner.len()),owner_value=span(4392,8)));
    let mut body = String::new();
    if drained_activation {
        // Actual parent proof and RPC state start drained. The original next
        // block accrues inputs before its observed drain and economic epoch.
        declarations.push_str(&segment(1850, &words(&[100])));
        for index in 0..2 {
            body.push_str(&format!(
                "(call $set (i64.const {}) (i64.const {}))",
                span(1000 + index as u32 * 100, drains[index].len()),
                span(1850, 8)
            ));
        }
    }
    for index in 0..3 {
        body.push_str(&format!("(local.set $n (i32.wrap_i64 (call $drain (i64.const {})))) (if (i32.ne (i32.load8_u (local.get $n)) (i32.const 1)) (then unreachable)) (if (i32.ne (i32.load8_u offset=1 (local.get $n)) (i32.const 32)) (then unreachable)) (i64.store (i32.const {}) (i64.load offset=2 (local.get $n))) (call $set (i64.const {}) (i64.const {}))",span(1000+index as u32*100,drains[index].len()),4024+index*8,span(1000+index as u32*100,drains[index].len()),span(1800,8)));
    }
    body.push_str("(call $epoch) (call $emission) (call $provider) (call $owner)");
    if continuous {
        // This check is outside the admitted native callsites. Ordinary empty
        // blocks perform no drain/epoch/allocation, rather than fabricating a
        // zero emission or spending the first block's recipient amounts again.
        declarations.push_str(&segment(1900, &[0]));
        if drained_activation {
            // The original pending inputs are zero before first-block accrual.
            // Their value cannot guard the body that creates them. The original
            // epoch key is absent only at this fixture's independently proved
            // opening state and remains present after the first actual epoch.
            body = format!(
                "(call $set (i64.const {}) (i64.const {})) (local.set $n (i32.wrap_i64 (call $get (i64.const {})))) (if (i32.eqz (i32.load8_u (local.get $n))) (then {}))",
                span(1400, events.len()), span(1900, 1), span(1700, epoch.len()), body
            );
        } else {
            // Keep the undrained standalone fixture's original program exact.
            body = format!(
                "(call $set (i64.const {}) (i64.const {})) (local.set $n (i32.wrap_i64 (call $get (i64.const {})))) (if (i64.ne (i64.load offset=2 (local.get $n)) (i64.const 0)) (then {}))",
                span(1400, events.len()), span(1900, 1), span(1000, drains[0].len()), body
            );
        }
    }
    let principal_key = b"synthetic-opening-stake";
    if principal.is_some() {
        let mut result = vec![1];
        result.extend_from_slice(&[0x11; 32]);
        result.extend_from_slice(&[0x33; 32]);
        result.extend_from_slice(&[100, 0, 0, 0, 0, 0, 1]);
        declarations.push_str(&segment(2000, principal_key));
        declarations.push_str(&segment(6000, &result));
        declarations.push_str(&segment(6200, &[0]));
        declarations.push_str(&format!(r#"
            (func (export "StakeInfoRuntimeApi_get_stake_info_for_hotkey_coldkey_netuid") (param i32 i32) (result i64)
              (local $value i32)
              (local.set $value (i32.wrap_i64 (call $get (i64.const {key}))))
              (if (i32.eqz (i32.load8_u (local.get $value))) (then (return (i64.const {absent}))))
              (if (i32.ne (i32.load8_u offset=1 (local.get $value)) (i32.const 32)) (then unreachable))
              (i32.store8 (i32.const 6066) (i32.shl (i32.load8_u offset=2 (local.get $value)) (i32.const 2)))
              (i64.const {present}))"#,
              key=span(2000,principal_key.len()),absent=span(6200,1),present=span(6000,result.len())));
        if principal.flatten().is_some() && effects.is_none() {
            body.push_str(&format!(r#"
              (local.set $n (i32.wrap_i64 (call $get (i64.const {key}))))
              (i64.store (i32.const 6100) (i64.add (i64.load offset=2 (local.get $n)) (i64.load (i32.const 4384))))
              (call $set (i64.const {key}) (i64.const {updated}))"#,
              key=span(2000,principal_key.len()),updated=span(6100,8)));
        }
    }
    if let Some(mode) = effects {
        for (name, operation) in [
            ("earning", "add"),
            ("deposit", "add"),
            ("withdrawal", "sub"),
            ("refund", "add"),
        ]
        .into_iter()
        .chain(capture.then_some(("vault_capture", "sub")))
        {
            declarations.push_str(&format!(r#"
              (func $stake_{name} (export "stake_{name}") (param $amount i64)
                (local $value i32)
                (local.set $value (i32.wrap_i64 (call $get (i64.const {key}))))
                (i64.store (i32.const 6400) (i64.load offset=2 (local.get $value)))
                (i64.store (i32.const 6408) (i64.{operation} (i64.load (i32.const 6400)) (local.get $amount)))
                (call $set (i64.const {key}) (i64.const {value})))"#,
                key=span(2000,principal_key.len()), value=span(6408,8)));
        }
        if principal.flatten().is_some() {
            body.push_str("(call $stake_earning (i64.load (i32.const 4384)))");
            match mode {
                "causes" | "capture-causes" => body.push_str("(call $stake_deposit (i64.const 5)) (call $stake_withdrawal (i64.const 3)) (call $stake_refund (i64.const 2))"),
                "rollback" => body.push_str("(call $begin) (call $stake_deposit (i64.const 5)) (call $rollback)"),
                "unclassified" | "residual" | "capture-unclassified" => {
                    // No selected callsite wraps these actual mutations. The
                    // independent storage census must retain both even at net zero.
                    body.push_str(&format!("(i64.store (i32.const 6408) (i64.add (i64.load (i32.const 6408)) (i64.const 1))) (call $set (i64.const {key}) (i64.const {value}))",key=span(2000,principal_key.len()),value=span(6408,8)));
                    if mode=="unclassified" || mode=="capture-unclassified" { body.push_str(&format!("(i64.store (i32.const 6408) (i64.sub (i64.load (i32.const 6408)) (i64.const 1))) (call $set (i64.const {key}) (i64.const {value}))",key=span(2000,principal_key.len()),value=span(6408,8))); }
                },
                "earning" | "capture" | "capture-rollback" | "capture-same-block" | "capture-next-block" => (),
                _ => panic!("unsupported synthetic principal effect mode"),
            }
        }
    }
    if capture {
        declarations.push_str(&segment(6600, &phase));
        declarations.push_str(&segment(6700, &[0, 0, 0, 0, 0]));
        declarations.push_str(&segment(6710, &[1]));
        if effects == Some("capture-next-block") {
            declarations.push_str(&segment(6720, &[2]));
            declarations.push_str(&segment(6730, &[0]));
            // Every actual block begins with a new event vector and the same
            // initial phase. Its proof still contains the prior committed state.
            body = format!(
                "(call $set (i64.const {}) (i64.const {})) (call $set (i64.const {}) (i64.const {})) {}",
                span(6600, phase.len()), span(6720, 1),
                span(1400, events.len()), span(6730, 1), body
            );
        }
        // The synthetic opaque extrinsic ends in its EVM transaction identity.
        // The original program reads that actual body input; no Go trace peer
        // supplies the captured memory or the committed stake delta.
        for offset in [0, 8, 16, 24] {
            let end = if effects == Some("capture-same-block") {
                65
            } else {
                32
            };
            body.push_str(&format!("(i64.store (i32.const {}) (i64.load (i32.add (local.get 0) (i32.sub (local.get 1) (i32.const {})))))",6500+offset,end-offset));
        }
        body.push_str(&format!(
            "(call $set (i64.const {}) (i64.const {}))",
            span(6600, phase.len()),
            span(6700, 5)
        ));
        if effects == Some("capture-rollback") {
            body.push_str("(call $begin)");
        }
        body.push_str(&format!("(local.set $n (i32.wrap_i64 (call $get (i64.const {})))) (call $stake_vault_capture (i64.load offset=2 (local.get $n)))",span(2000,principal_key.len())));
        if effects == Some("capture-rollback") {
            body.push_str("(call $rollback)");
        }
        if effects == Some("capture-same-block") {
            declarations.push_str(&segment(6720, &[0, 1, 0, 0, 0]));
            body.push_str("(call $stake_deposit (i64.const 5))");
            for offset in [0, 8, 16, 24] {
                body.push_str(&format!("(i64.store (i32.const {}) (i64.load (i32.add (local.get 0) (i32.sub (local.get 1) (i32.const {})))))",6500+offset,32-offset));
            }
            body.push_str(&format!(
                "(call $set (i64.const {}) (i64.const {})) (local.set $n (i32.wrap_i64 (call $get (i64.const {})))) (call $stake_vault_capture (i64.load offset=2 (local.get $n)))",
                span(6600, phase.len()), span(6720, 5), span(2000, principal_key.len())
            ));
        }
        body.push_str(&format!(
            "(call $set (i64.const {}) (i64.const {}))",
            span(6600, phase.len()),
            span(6710, 1)
        ));
    }
    if let Some(mode) = yuma {
        declarations.push_str(&yuma_tests::declarations(if allocation_count > 2 {
            "legacy"
        } else {
            mode
        }));
        declarations = declarations.replace(
            "(func $epoch (export \"native_epoch\")",
            "(func $epoch (export \"native_epoch\") (call $yuma_compute)",
        );
        declarations = declarations.replace("(i64.const 8589934592)", "(i64.add (i64.add (i64.load (i32.const 4100)) (i64.load (i32.const 4108))) (i64.add (i64.load (i32.const 4120)) (i64.load (i32.const 4128))))");
        body = body.replace("(call $epoch)", yuma_tests::body());
    }
    let fees = fee_mode.map(fee_census_tests::Program::new);
    if let Some(fees) = &fees {
        declarations.push_str(&fees.declarations());
        body.push_str(&fees.body());
    }
    if let Some(upgrade) = upgrade {
        assert!(upgrade.len() < 64 * 1024);
        declarations.push_str(&segment(10000, b":code"));
        declarations.push_str(&segment(11000, upgrade));
        body = format!(
            "(i32.store8 (i32.const 9000) (i32.load8_u (i32.wrap_i64 (call $get (i64.const {}))))) {} (if (i32.ne (i32.load8_u (i32.const 9000)) (i32.const 0)) (then (call $set (i64.const {}) (i64.const {}))))",
            span(1700, epoch.len()), body, span(10000, 5), span(11000, upgrade.len())
        );
    }
    if treasury {
        assert!(
            principal.is_none()
                && effects.is_none()
                && yuma.is_none()
                && fee_mode.is_none()
                && upgrade.is_none()
        );
        treasury_tests::program(&mut declarations, &mut body);
    }
    let code = if allocation_count > 2 {
        yuma_capacity_tests::expand(allocation_count, &mut declarations, &mut body);
        if yuma_populated_tests::selected(yuma) {
            yuma_populated_tests::expand(allocation_count, &mut declarations, &mut body);
        }
        wasm_with_heap(&declarations, &body, 400000)
    } else if upgrade.is_some() {
        wasm_with_heap(&declarations, &body, 128000)
    } else if fees.is_some() {
        wasm_with_heap(&declarations, &body, 60000)
    } else {
        wasm(&declarations, &body)
    };
    let mut initial = parent_storage(&code);
    for (index, drain) in drains.iter().enumerate() {
        initial.top.insert(
            drain.clone(),
            words(&[if drained_activation {
                0
            } else {
                [100, 100, 0][index]
            }]),
        );
    }
    initial.top.insert(phase.clone(), vec![2]);
    initial.top.insert(events.clone(), vec![0]);
    if fees.is_some() {
        initial
            .top
            .insert(b"synthetic-original-fee-disposition".to_vec(), vec![1]);
    }
    if let Some(stock) = principal.flatten() {
        initial.top.insert(principal_key.to_vec(), words(&[stock]));
    }
    if treasury {
        treasury_tests::initial(&mut initial);
    }
    let backing = TestExternalities::<Blake2Hasher>::new_with_code_and_state(
        &code,
        initial.clone(),
        StateVersion::V1,
    );
    let (nodes, parent_root) = backing.into_raw_snapshot();
    let parent = NativeHeader::new(
        100,
        H256::repeat_byte(2),
        parent_root,
        H256::repeat_byte(1),
        Digest::default(),
    );
    let mut expected = initial;
    for drain in drains {
        expected.top.insert(drain, words(&[0]));
    }
    expected.top.insert(epoch.to_vec(), words(&[200]));
    expected.top.insert(provider.to_vec(), words(&[9, 3, 6]));
    expected.top.insert(owner.to_vec(), words(&[89]));
    if let Some(fees) = &fees {
        expected
            .top
            .insert(events.clone(), fees.expected_events(&event));
        expected.top.insert(phase.clone(), vec![1]);
    } else {
        expected.top.insert(events, [vec![4], event].concat());
    }
    if capture {
        expected.top.insert(phase, vec![1]);
    }
    if let Some(stock) = principal.flatten() {
        expected.top.insert(
            principal_key.to_vec(),
            words(&[if capture && effects != Some("capture-rollback") {
                0
            } else {
                stock
                    + 6
                    + if effects == Some("causes") {
                        4
                    } else if effects == Some("residual") {
                        1
                    } else {
                        0
                    }
            }]),
        );
    }
    if yuma_populated_tests::selected(yuma) {
        yuma_populated_tests::expected(allocation_count, &mut expected);
    }
    if treasury {
        treasury_tests::expected(&mut expected);
    }
    let backing = TestExternalities::<Blake2Hasher>::new_with_code_and_state(
        &code,
        expected.clone(),
        StateVersion::V1,
    );
    let extrinsics: Vec<Vec<u8>> = if let Some(fees) = &fees {
        fees.extrinsics()
    } else if effects == Some("capture-same-block") {
        vec![vec![0x99u8; 32].encode(), vec![0x98u8; 32].encode()]
    } else if capture {
        vec![vec![0x99u8; 32].encode()]
    } else {
        Vec::new()
    };
    let child = NativeHeader::new(
        101,
        BlakeTwo256::ordered_trie_root(extrinsics.clone(), StateVersion::V0),
        *backing.backend.root(),
        parent.hash(),
        Digest::default(),
    );
    let mut profile = observation_profile(&code, "native_drain", "native-drain");
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    let fields = |items: &[(&str, u32, u32)]| {
        items
            .iter()
            .map(|(name, address, bytes)| observer::MemoryCapture {
                name: (*name).to_owned(),
                address: *address,
                global: None,
                dereference_offsets: Vec::new(),
                bytes: *bytes,
                repeat: None,
            })
            .collect()
    };
    profile.rules[0].memory = fields(&[
        ("netuid", 4000, 2),
        ("subnet-registered", 4008, 8),
        ("subnet-generation", 4016, 8),
    ]);
    for (export, purpose, items) in [
        (
            "native_epoch",
            "native-epoch",
            vec![
                ("netuid", 4000, 2),
                ("total-alpha", 4048, 8),
                ("incentive-q32", 4100, 16),
                ("dividends-q32", 4120, 16),
                ("normalized-q32", 4140, 16),
                ("emission", 4160, 16),
                ("uids", 4180, 4),
                ("hotkeys", 4200, 64),
                ("registered", 4280, 16),
            ],
        ),
        (
            "native_emission",
            "native-emission",
            vec![("netuid", 4000, 2)],
        ),
        (
            "native_provider",
            "native-miner-credit",
            vec![
                ("netuid", 4000, 2),
                ("hotkey", 4300, 32),
                ("coldkey", 4332, 32),
                ("gross", 4368, 8),
                ("captured", 4376, 8),
                ("liquid", 4384, 8),
            ],
        ),
        (
            "native_owner",
            "native-owner-recycle",
            vec![
                ("netuid", 4000, 2),
                ("hotkey", 4300, 32),
                ("coldkey", 4332, 32),
                ("gross", 4368, 8),
                ("recycled", 4392, 8),
            ],
        ),
    ] {
        let mut rule = observation_profile(&code, export, purpose).rules.remove(0);
        rule.memory = fields(&items);
        profile.rules.push(rule);
    }
    if yuma.is_some() {
        yuma_tests::profile(&code, &mut profile);
        if allocation_count > 2 {
            yuma_capacity_tests::profile(allocation_count, &mut profile);
            if yuma_populated_tests::selected(yuma) {
                yuma_populated_tests::profile(&code, &mut profile);
            }
        }
    }
    if effects.is_some() {
        profile.principal_storage_prefixes = Some(vec![encoded(principal_key)]);
        for name in ["earning", "deposit", "withdrawal", "refund"]
            .into_iter()
            .chain(capture.then_some("vault_capture"))
        {
            let mut rule = observation_profile(
                &code,
                &format!("stake_{name}"),
                &format!("native-principal-{}", name.replace('_', "-")),
            )
            .rules
            .remove(0);
            rule.memory = fields(&[
                ("netuid", 4000, 2),
                ("hotkey", 4200, 32),
                ("coldkey", 4500, 32),
                ("before", 6400, 8),
                ("after", 6408, 8),
            ]);
            if name == "vault_capture" {
                rule.memory
                    .extend(fields(&[("transaction-hash", 6500, 32)]));
            }
            profile.rules.push(rule);
        }
    }
    if let Some(fees) = &fees {
        fees.profile(&code, &mut profile);
    }
    if treasury {
        treasury_tests::profile(&code, &mut profile);
    }
    (
        HistoricalJob {
            schema: HISTORICAL_SCHEMA.to_owned(),
            parent_header_hex: encoded(&parent.encode()),
            parent_hash: parent.hash().0,
            child_header_hex: encoded(&child.encode()),
            child_hash: child.hash().0,
            extrinsics_hex: extrinsics.iter().map(|raw| encoded(raw)).collect(),
            runtime_code_hex: encoded(&code),
            runtime_code_sha256: sha2_256(&code),
            runtime_code_blake2b_256: blake2_256(&code),
            execution_state_version: 1,
            proof_nodes_hex: nodes
                .into_iter()
                .map(|(_, (value, _))| encoded(&value))
                .collect::<BTreeSet<_>>()
                .into_iter()
                .collect(),
            observation_profile: Some(profile),
            principal_effects: effects.is_some() || treasury,
            principal_queries: if treasury {
                Some(treasury_tests::queries())
            } else {
                principal.map(|_| {
                    vec![principal::PrincipalQuery {
                        hotkey: [0x11; 32],
                        coldkey: [0x33; 32],
                        netuid: 25,
                        availability: false,
                    }]
                })
            },
        },
        expected,
    )
}

// Five actual original-program jobs allow the Go producer to restart while a
// retained GRANDPA window certifies a later head. Only the first block emits;
// the next clears its events and the later blocks reproduce unchanged state.
#[test]
fn historical_native_producer_exports_contiguous_original_jobs() {
    let (first, post_storage) = fixture_with_continuation(true);
    export_contiguous_jobs(
        first,
        post_storage,
        "URNETWORK_NATIVE_PRODUCER_FIXTURE_OUT",
        false,
    );
}

// Combined accounting starts at the proof-drained parent, before the original
// next-block accrual. Standalone producer exports retain their original input.
#[test]
fn historical_native_conservation_exports_proof_drained_contiguous_jobs() {
    let (first, post_storage) = fixture_with_principal_activation(true, None, true);
    let parent: NativeHeader = scale_exact(
        "parent",
        &hex_bytes("parent", &first.parent_header_hex, MAXIMUM_HEADER_BYTES).unwrap(),
    )
    .unwrap();
    let nodes = first
        .proof_nodes_hex
        .iter()
        .map(|node| hex_bytes("node", node, MAXIMUM_CODE_BYTES).unwrap());
    let backend =
        create_proof_check_backend::<Blake2Hasher>(*parent.state_root(), StorageProof::new(nodes))
            .unwrap();
    assert_eq!(
        backend.storage(b"synthetic-native-epoch").unwrap(),
        None,
        "combined original activation parent already contains an epoch"
    );
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
            "combined original activation parent must be proof-drained"
        );
    }
    export_contiguous_jobs(
        first,
        post_storage,
        "URNETWORK_NATIVE_CONSERVATION_FIXTURE_OUT",
        true,
    );
}

// Preserve each original parent proof and execute every linked job before
// publishing any fixture. Export paths are exclusive, then durably synced.
fn export_contiguous_jobs(
    first: HistoricalJob,
    mut post_storage: sp_core::storage::Storage,
    output_variable: &str,
    proof_drained: bool,
) {
    let mut jobs = vec![first];
    let code = hex_bytes(
        "producer code",
        &jobs[0].runtime_code_hex,
        MAXIMUM_CODE_BYTES,
    )
    .expect("original producer code");
    for number in 102..=105 {
        let backing = TestExternalities::<Blake2Hasher>::new_with_code_and_state(
            &code,
            post_storage.clone(),
            StateVersion::V1,
        );
        let (nodes, _) = backing.into_raw_snapshot();
        let proof: Vec<String> = nodes
            .into_iter()
            .map(|(_, (value, _))| encoded(&value))
            .collect::<BTreeSet<_>>()
            .into_iter()
            .collect();
        post_storage
            .top
            .insert(key(b"System", b"Events", false), vec![0]);
        let expected = TestExternalities::<Blake2Hasher>::new_with_code_and_state(
            &code,
            post_storage.clone(),
            StateVersion::V1,
        );
        let mut job = jobs[0].clone();
        let previous = jobs.last().unwrap();
        let child = NativeHeader::new(
            number,
            BlakeTwo256::ordered_trie_root(Vec::<Vec<u8>>::new(), StateVersion::V0),
            *expected.backend.root(),
            H256(previous.child_hash),
            Digest::default(),
        );
        job.parent_header_hex = previous.child_header_hex.clone();
        job.parent_hash = previous.child_hash;
        job.child_header_hex = encoded(&child.encode());
        job.child_hash = child.hash().0;
        job.proof_nodes_hex = proof.clone();
        jobs.push(job);
    }
    // Every child root above is built from an independent complete map before
    // any replay runs. Proof checks also preserve the original first/later
    // dispatch distinction; returned replay roots never supply expectations.
    for (index, job) in jobs.iter().enumerate() {
        let parent: NativeHeader = scale_exact(
            "contiguous parent",
            &hex_bytes(
                "contiguous parent",
                &job.parent_header_hex,
                MAXIMUM_HEADER_BYTES,
            )
            .unwrap(),
        )
        .unwrap();
        let nodes = job
            .proof_nodes_hex
            .iter()
            .map(|node| hex_bytes("contiguous node", node, MAXIMUM_CODE_BYTES).unwrap());
        let backend = create_proof_check_backend::<Blake2Hasher>(
            *parent.state_root(),
            StorageProof::new(nodes),
        )
        .unwrap();
        assert_eq!(
            parent.hash().0,
            job.parent_hash,
            "contiguous original parent identity changed"
        );
        assert_eq!(
            backend.storage(b"synthetic-native-epoch").unwrap(),
            if index == 0 {
                None
            } else {
                Some(words(&[200]))
            },
            "contiguous original epoch marker is not the first/later proof state"
        );
        for (offset, item) in [
            b"PendingServerEmission".as_slice(),
            b"PendingValidatorEmission".as_slice(),
            b"PendingRootAlphaDivs".as_slice(),
        ]
        .into_iter()
        .enumerate()
        {
            assert_eq!(
                backend
                    .storage(&key(b"SubtensorModule", item, true))
                    .unwrap(),
                Some(words(&[if index == 0 && !proof_drained && offset < 2 {
                    100
                } else {
                    0
                }])),
                "contiguous original pending parent differs from its activation"
            );
        }
        if index > 0 {
            assert_eq!(
                backend
                    .storage(b"synthetic-native-provider-credit")
                    .unwrap(),
                Some(words(&[9, 3, 6])),
                "empty continuation lost original provider effects"
            );
            assert_eq!(
                backend.storage(b"synthetic-native-owner-recycle").unwrap(),
                Some(words(&[89])),
                "empty continuation lost original owner effects"
            );
        }
        let report = run(job).unwrap_or_else(|error| {
            panic!(
                "contiguous original block {} failed independently declared post-state: {error}",
                index + 101
            )
        });
        assert!(report.post_state_reproduced && !report.runtime_admitted);
        if proof_drained {
            let captured = super::capture_tests::collect(job).unwrap_or_else(|error| {
                panic!(
                    "proof-drained original block {} capture failed: {error}",
                    index + 101
                )
            });
            let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
            assert_eq!(
                exported.parent_header_hex, job.parent_header_hex,
                "capture replaced the independently declared parent"
            );
            assert_eq!(
                exported.child_header_hex, job.child_header_hex,
                "capture replaced the independently declared child"
            );
            assert_eq!(
                exported.runtime_code_sha256, job.runtime_code_sha256,
                "capture replaced the original drained program"
            );
            let replayed = run(&exported).unwrap_or_else(|error| {
                panic!(
                    "proof-drained original block {} reduced replay failed: {error}",
                    index + 101
                )
            });
            for actual in [&captured.replay, &replayed] {
                assert!(actual.post_state_reproduced && !actual.runtime_admitted);
                assert_eq!(
                    actual
                        .hook_observations
                        .as_ref()
                        .unwrap()
                        .observations
                        .len(),
                    if index == 0 { 7 } else { 0 },
                    "proof-drained capture/replay changed the first/later original census"
                );
            }
        }
        let records = &report.hook_observations.as_ref().unwrap().observations;
        assert_eq!(
            records.len(),
            if index == 0 { 7 } else { 0 },
            "contiguous original first/later observation census changed"
        );
        if index == 0 {
            for (offset, amount) in [100u64, 100, 0].into_iter().enumerate() {
                assert_eq!(
                    records[offset].storage_return.as_ref().unwrap().value_hex,
                    Some(encoded(&words(&[amount]))),
                    "first original epoch did not drain its witnessed next-block accrual"
                );
            }
            let emitted = records[3]
                .native
                .as_ref()
                .unwrap()
                .memory
                .iter()
                .find(|value| value.name == "emission")
                .unwrap();
            assert_eq!(emitted.bytes_hex, encoded(&words(&[9, 89])));
        }
    }
    if let Some(directory) = std::env::var_os(output_variable) {
        let directory = Path::new(&directory);
        assert!(directory.is_absolute() && directory.is_dir());
        for (index, job) in jobs.iter().enumerate() {
            let mut file = OpenOptions::new()
                .create_new(true)
                .write(true)
                .mode(0o600)
                .open(directory.join(format!("native-job-{}.json", index + 101)))
                .expect("exclusive contiguous producer fixture");
            file.write_all(&serde_json::to_vec(job).unwrap())
                .expect("complete producer fixture");
            file.sync_all().expect("durable producer fixture");
        }
        std::fs::File::open(directory)
            .expect("producer fixture directory")
            .sync_all()
            .expect("durable producer fixture names");
    }
}

#[test]
fn historical_native_execution_exports_actual_original_program_for_go_consumer() {
    let job = fixture();
    let report = run(&job).expect("actual original native fixture refused");
    assert!(
        report.post_state_reproduced
            && !report.runtime_admitted
            && report.native_fee_debit.is_none()
    );
    let records = &report.hook_observations.as_ref().unwrap().observations;
    assert_eq!(records.len(), 7);
    assert_eq!(
        records[0].storage_return.as_ref().unwrap().value_hex,
        Some(encoded(&words(&[100])))
    );
    assert_eq!(
        records[3]
            .native
            .as_ref()
            .unwrap()
            .memory
            .iter()
            .find(|value| value.name == "emission")
            .unwrap()
            .bytes_hex,
        encoded(&words(&[9, 89]))
    );
    for (index, hotkey, coldkey, gross) in [(5, 0x11, 0x33, 9), (6, 0x22, 0x34, 89)] {
        let memory = &records[index].native.as_ref().unwrap().memory;
        for (name, expected) in [
            ("hotkey", vec![hotkey; 32]),
            ("coldkey", vec![coldkey; 32]),
            ("gross", words(&[gross])),
        ] {
            assert_eq!(
                memory
                    .iter()
                    .find(|value| value.name == name)
                    .unwrap()
                    .bytes_hex,
                encoded(&expected),
                "original economic recipient identity or amount changed: {name}"
            );
        }
    }
    if let Some(directory) = std::env::var_os("URNETWORK_NATIVE_EXECUTION_FIXTURE_OUT") {
        let directory = Path::new(&directory);
        assert!(directory.is_absolute() && directory.is_dir());
        let mut file = OpenOptions::new()
            .create_new(true)
            .write(true)
            .mode(0o600)
            .open(directory.join("native-job.json"))
            .expect("exclusive original fixture output");
        file.write_all(&serde_json::to_vec(&job).unwrap())
            .expect("complete original fixture output");
        file.sync_all().expect("sync original fixture output");
    }
}

// A separate export leaves all old fixture bytes and selectors unchanged. The
// public Go producer captures these real parent paths and runs two owned ELFs.
#[test]
fn historical_native_principal_exports_original_parent_jobs() {
    let mut jobs = Vec::new();
    for (name, stock) in [("present", Some(14)), ("zero", Some(0)), ("absent", None)] {
        let (job, expected) = fixture_with_principal(false, Some(stock));
        let captured =
            super::capture_tests::collect(&job).expect("actual original native principal capture");
        let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
        let replayed = run(&exported).expect("actual original native principal replay");
        for report in [&captured.replay, &replayed] {
            assert!(report.post_state_reproduced && !report.runtime_admitted);
            let observation = &report.opening_principals.as_ref().unwrap()[0];
            assert_eq!(
                observation.opening_stake_alpha,
                stock.map(|value| value.to_string())
            );
            assert_eq!(observation.registered, stock.map(|_| true));
            assert_eq!(
                report
                    .hook_observations
                    .as_ref()
                    .unwrap()
                    .observations
                    .len(),
                7
            );
        }
        assert_eq!(
            expected.top.get(b"synthetic-opening-stake".as_slice()),
            stock.map(|value| words(&[value + 6])).as_ref()
        );
        jobs.push((name, exported));
    }
    let mut missing = fixture_with_allocation_activation(false, None, None, None, true).0;
    missing.principal_queries = Some(vec![principal::PrincipalQuery {
        hotkey: [0x11; 32],
        coldkey: [0x33; 32],
        netuid: 25,
        availability: false,
    }]);
    let error = super::capture_tests::collect(&missing)
        .err()
        .expect("missing principal API was accepted");
    assert!(error.to_string().contains("principal"), "{error}");
    jobs.push(("missing-api", missing));
    if let Some(directory) = std::env::var_os("URNETWORK_NATIVE_PRINCIPAL_FIXTURE_OUT") {
        let directory = Path::new(&directory);
        assert!(directory.is_absolute() && directory.is_dir());
        for (name, job) in jobs {
            let mut file = OpenOptions::new()
                .create_new(true)
                .write(true)
                .mode(0o600)
                .open(directory.join(format!("principal-{name}.json")))
                .expect("exclusive principal export");
            file.write_all(&serde_json::to_vec(&job).unwrap())
                .expect("complete principal export");
            file.sync_all().expect("durable principal export");
        }
        std::fs::File::open(directory)
            .unwrap()
            .sync_all()
            .expect("durable principal export directory");
    }
}

// Every exported case executes the original program in capture and strict
// replay. Mutation counts are independent of selected causal observations.
#[test]
fn historical_native_principal_effects_export_original_causal_jobs() {
    let mut jobs = Vec::new();
    for (name, mode, stock, after, mutations, discarded) in [
        ("earning", "earning", Some(14), Some(20), 1, 0),
        ("causes", "causes", Some(14), Some(24), 4, 0),
        ("zero", "earning", Some(0), Some(6), 1, 0),
        ("absent", "earning", None, None, 0, 0),
        ("unclassified", "unclassified", Some(14), Some(20), 3, 0),
        ("residual", "residual", Some(14), Some(21), 2, 0),
        ("rollback", "rollback", Some(14), Some(20), 1, 2),
    ] {
        let (job, _) = fixture_with_principal_effects(false, Some(stock), Some(mode));
        let captured =
            super::capture_tests::collect(&job).expect("actual original principal effect capture");
        let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
        let replayed = run(&exported).expect("actual original principal effect replay");
        for report in [&captured.replay, &replayed] {
            assert!(report.post_state_reproduced);
            assert_eq!(
                report.opening_principals.as_ref().unwrap()[0].opening_stake_alpha,
                stock.map(|n| n.to_string())
            );
            assert_eq!(
                report.closing_principals.as_ref().unwrap()[0].opening_stake_alpha,
                after.map(|n| n.to_string())
            );
            let trace = report.hook_observations.as_ref().unwrap();
            assert_eq!(
                trace.principal_mutations.as_ref().unwrap().len(),
                mutations,
                "{name}"
            );
            assert_eq!(trace.discarded_on_rollback, discarded, "{name}");
        }
        jobs.push((name, exported));
    }
    if let Some(directory) = std::env::var_os("URNETWORK_NATIVE_PRINCIPAL_EFFECTS_FIXTURE_OUT") {
        let directory = Path::new(&directory);
        assert!(directory.is_absolute() && directory.is_dir());
        for (name, job) in jobs {
            let mut file = OpenOptions::new()
                .create_new(true)
                .write(true)
                .mode(0o600)
                .open(directory.join(format!("principal-effects-{name}.json")))
                .expect("exclusive original effect fixture");
            file.write_all(&serde_json::to_vec(&job).unwrap()).unwrap();
            file.sync_all().unwrap();
        }
        std::fs::File::open(directory).unwrap().sync_all().unwrap();
    }
}

// Read the parent from its actual proof before either engine executes. The
// original next-block program must still compute and emit the same 9/89 pair.
#[test]
fn historical_native_principal_activation_is_proof_drained_before_original_accrual() {
    let (job, _) = fixture_with_principal(false, Some(Some(14)));
    let parent: NativeHeader = scale_exact(
        "parent",
        &hex_bytes("parent", &job.parent_header_hex, MAXIMUM_HEADER_BYTES).unwrap(),
    )
    .unwrap();
    let nodes = job
        .proof_nodes_hex
        .iter()
        .map(|node| hex_bytes("node", node, MAXIMUM_CODE_BYTES).unwrap());
    let backend =
        create_proof_check_backend::<Blake2Hasher>(*parent.state_root(), StorageProof::new(nodes))
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
            "original activation parent is not actually drained"
        );
    }
    let captured =
        super::capture_tests::collect(&job).expect("original accrued activation capture");
    let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
    let replayed = run(&exported).expect("original accrued activation replay");
    for report in [&captured.replay, &replayed] {
        assert!(report.post_state_reproduced);
        let trace = report.hook_observations.as_ref().unwrap();
        let epoch = trace
            .observations
            .iter()
            .find(|value| value.purpose == "native-epoch")
            .unwrap();
        let emitted = epoch
            .native
            .as_ref()
            .unwrap()
            .memory
            .iter()
            .find(|value| value.name == "emission")
            .unwrap();
        assert_eq!(
            emitted.bytes_hex,
            encoded(&words(&[9, 89])),
            "original accrual failed to preserve full source allocation"
        );
    }
}
