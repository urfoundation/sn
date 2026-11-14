//! The availability API must read the same original proof and completed overlay
//! as principal stock. Synthetic Wasm is not admission of the production runtime.

use super::*;
use crate::historical::principal::AVAILABILITY_API;

fn availability_query() -> PrincipalQuery {
    PrincipalQuery {
        availability: true,
        ..query()
    }
}

fn availability_result(total: u64, locked: u64, available: u64) -> Vec<u8> {
    let mut raw = vec![4]; // One coldkey, one subnet; plain u16 key.
    raw.extend_from_slice(&query().coldkey);
    raw.extend_from_slice(&[4, 25, 0]);
    for value in [total, locked, available] {
        raw.extend_from_slice(&parity_scale_codec::Compact(value).encode());
    }
    raw
}

// Both boundary results load total from the actual ACCOUNT storage value.
// Collateral is two and conviction lock is two, so available = total - four.
const READ_AVAILABILITY: &str = r#"
    (local.set $value (i32.wrap_i64 (call $get (i64.const 51539609536))))
    (if (i32.ne (i32.load8_u (local.get $value)) (i32.const 1)) (then unreachable))
    (i32.store8 (i32.const 5036)
      (i32.shl (i32.load8_u offset=3 (local.get $value)) (i32.const 2)))
    (i32.store8 (i32.const 5038)
      (i32.shl (i32.sub (i32.load8_u offset=3 (local.get $value)) (i32.const 4)) (i32.const 2)))"#;

const UPDATE_AVAILABILITY: &str = "(call $set (i64.const 51539609536) (i64.const 412316866416))";

fn availability_wasm(query_body: &str, execute_body: &str, result: &[u8]) -> Vec<u8> {
    let escaped = |bytes: &[u8]| {
        bytes
            .iter()
            .map(|byte| format!("\\{byte:02x}"))
            .collect::<String>()
    };
    let stake = stake_result(7);
    wasm(
        &format!(
            r#"(data (i32.const 4000) "{stake}")
            (data (i32.const 5000) "{availability}")
            (data (i32.const 6000) "{updated}")
            (func (export "{STAKE_API}") (param i32 i32) (result i64)
              (i64.const {stake_span}))
            (func (export "{AVAILABILITY_API}") (param $args i32) (param $size i32) (result i64)
              (local $value i32)
              (if (i32.ne (local.get $size) (i32.const 37)) (then unreachable))
              (if (i32.ne (i32.load8_u (local.get $args)) (i32.const 4)) (then unreachable))
              (if (i64.ne (i64.load offset=1 (local.get $args)) (i64.const 144680345676153346)) (then unreachable))
              (if (i64.ne (i64.load offset=9 (local.get $args)) (i64.const 144680345676153346)) (then unreachable))
              (if (i64.ne (i64.load offset=17 (local.get $args)) (i64.const 144680345676153346)) (then unreachable))
              (if (i64.ne (i64.load offset=25 (local.get $args)) (i64.const 144680345676153346)) (then unreachable))
              (if (i32.ne (i32.load8_u offset=33 (local.get $args)) (i32.const 1)) (then unreachable))
              (if (i32.ne (i32.load8_u offset=34 (local.get $args)) (i32.const 4)) (then unreachable))
              (if (i32.ne (i32.load16_u offset=35 (local.get $args)) (i32.const 25)) (then unreachable))
              {query_body}
              (i64.const {availability_span}))"#,
            stake = escaped(&stake),
            availability = escaped(result),
            updated = escaped(&[11; 96]),
            stake_span = (stake.len() as u64) << 32 | 4000,
            availability_span = (result.len() as u64) << 32 | 5000,
        ),
        execute_body,
    )
}

#[test]
fn historical_availability_capture_replays_original_and_completed_custody() {
    let code = availability_wasm(
        READ_AVAILABILITY,
        UPDATE_AVAILABILITY,
        &availability_result(0, 2, 0),
    );
    let mut original = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), vec![11; 96]);
    });
    original.principal_queries = Some(vec![availability_query()]);
    original.principal_effects = true;
    let captured = collect(&original).expect("both original availability boundaries capture");
    let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
    let replayed = run(&exported).expect("captured availability paths strictly replay");
    for report in [&captured.replay, &replayed] {
        for (observations, total, available) in [
            (&report.opening_principals, 7, 3),
            (&report.closing_principals, 11, 7),
        ] {
            let result = observations.as_ref().unwrap()[0]
                .availability
                .as_ref()
                .unwrap();
            assert_eq!(
                result.result_hex,
                encoded(&availability_result(total, 2, available))
            );
            assert_eq!(
                result.total_alpha.as_deref(),
                Some(total.to_string().as_str())
            );
            assert_eq!(result.locked_alpha.as_deref(), Some("2"));
            assert_eq!(
                result.available_alpha.as_deref(),
                Some(available.to_string().as_str())
            );
        }
        assert!(!report.runtime_admitted && !report.production_selection);
    }
    assert_eq!(
        backend(&original).storage(ACCOUNT).unwrap(),
        Some(vec![7; 96])
    );
    // The same original job without selection never enrolls an availability
    // result or adds a false-valued field to its historical query identity.
    original.principal_queries = Some(vec![query()]);
    let legacy = collect(&original).unwrap();
    assert!(!serde_json::to_string(&query())
        .unwrap()
        .contains("availability"));
    assert!(legacy.replay.opening_principals.unwrap()[0]
        .availability
        .is_none());
}

#[test]
fn historical_availability_requires_original_dependency_and_read_only_execution() {
    let code = availability_wasm(READ_AVAILABILITY, "", &availability_result(0, 2, 0));
    let mut original = job(&code, |_| {});
    original.principal_queries = Some(vec![availability_query()]);
    assert!(
        collect(&original).is_ok(),
        "complete original availability control"
    );
    top_only_proof(&mut original, false);
    for result in [collect(&original).map(|_| ()), run(&original).map(|_| ())] {
        assert!(
            result.is_err(),
            "missing original availability path became a scalar"
        );
    }
    for code in [
        principal_wasm("", "", &stake_result(7)),
        availability_wasm(WRITE, "", &availability_result(7, 2, 3)),
        availability_wasm(
            &format!("(call $begin){WRITE}(call $rollback)"),
            "",
            &availability_result(7, 2, 3),
        ),
        availability_wasm("", "", &availability_result(7, 2, 6)),
    ] {
        let mut original = job(&code, |_| {});
        original.principal_queries = Some(vec![availability_query()]);
        for result in [collect(&original).map(|_| ()), run(&original).map(|_| ())] {
            assert!(
                result.is_err(),
                "missing API, transient write or impossible capacity was accepted"
            );
        }
    }
    assert!(
        crate::historical::principal::validate(&Some(vec![query(), availability_query()])).is_err()
    );
}
