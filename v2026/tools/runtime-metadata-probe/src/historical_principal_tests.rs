//! Real original-Wasm API calls share the capture/replay parent proof. These
//! synthetic runtimes exercise source mechanics, not live runtime admission.

use super::*;
use crate::historical::principal::{PrincipalQuery, STAKE_API};

#[path = "historical_availability_tests.rs"]
mod availability_tests;

fn query() -> PrincipalQuery {
    PrincipalQuery {
        hotkey: [1; 32],
        coldkey: [2; 32],
        netuid: 25,
        availability: false,
    }
}

// The result's stake byte is loaded from the actual parent storage host result.
// Core_execute_block then changes that key, so the independently expected stock
// cannot be obtained by reading the child state or a supplied report amount.
fn principal_wasm(query_body: &str, execute_body: &str, result: &[u8]) -> Vec<u8> {
    let escaped = result
        .iter()
        .map(|byte| format!("\\{byte:02x}"))
        .collect::<String>();
    wasm(
        &format!(
            r#"(data (i32.const 4000) "{escaped}")
            (func (export "{STAKE_API}") (param i32 i32) (result i64)
              (local $value i32) {query_body} (i64.const {span}))"#,
            span = (result.len() as u64) << 32 | 4000
        ),
        execute_body,
    )
}

fn stake_result(stake: u64) -> Vec<u8> {
    let mut result = vec![1];
    result.extend_from_slice(&query().hotkey);
    result.extend_from_slice(&query().coldkey);
    result.extend_from_slice(&parity_scale_codec::Compact(query().netuid).encode());
    for value in [stake, 0, 0, 0, 0] {
        result.extend_from_slice(&parity_scale_codec::Compact(value).encode());
    }
    result.push(1);
    result
}

const READ_STAKE: &str = r#"
    (local.set $value (i32.wrap_i64 (call $get (i64.const 51539609536))))
    (if (i32.ne (i32.load8_u (local.get $value)) (i32.const 1)) (then unreachable))
    (i32.store8 (i32.const 4066)
      (i32.shl (i32.load8_u offset=3 (local.get $value)) (i32.const 2)))"#;

#[test]
fn historical_principal_capture_replays_original_opening_stock() {
    let code = principal_wasm(READ_STAKE, WRITE, &stake_result(0));
    let mut job = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
    });
    job.principal_queries = Some(vec![query()]);
    let parent = backend(&job);
    let before = *parent.root();
    let raw = serde_json::to_vec(&request(&job)).unwrap();
    let result = capture::capture_historical_on_backend(&raw, &parent, &AtomicBool::new(false))
        .expect("original parent API query and block must capture");
    let captured: CaptureReport = serde_json::from_slice(&result).unwrap();
    let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
    assert_eq!(exported.principal_queries, job.principal_queries);
    let replayed = run(&exported).expect("captured API paths must strictly replay");
    for report in [&captured.replay, &replayed] {
        let observations = report.opening_principals.as_ref().unwrap();
        assert_eq!(observations.len(), 1);
        assert_eq!(observations[0].query, query());
        assert_eq!(observations[0].opening_stake_alpha.as_deref(), Some("7"));
        assert_eq!(observations[0].result_hex, encoded(&stake_result(7)));
        assert_eq!(observations[0].registered, Some(true));
        assert!(!report.runtime_admitted && !report.production_selection);
    }
    assert_eq!(
        *parent.root(),
        before,
        "principal query changed parent root"
    );
    assert_eq!(parent.storage(ACCOUNT).unwrap(), Some(vec![7; 96]));
}

#[test]
fn historical_principal_absent_and_observed_zero_remain_distinct() {
    for (result, expected) in [(vec![0], None), (stake_result(0), Some("0"))] {
        let code = principal_wasm("", "", &result);
        let mut job = job(&code, |_| {});
        job.principal_queries = Some(vec![query()]);
        let captured = collect(&job).expect("valid explicit zero or absent API result");
        let observation = &captured.replay.opening_principals.as_ref().unwrap()[0];
        assert_eq!(observation.opening_stake_alpha.as_deref(), expected);
        assert_eq!(observation.registered, expected.map(|_| true));
        assert_eq!(observation.result_hex, encoded(&result));
    }
}

#[test]
fn historical_principal_missing_parent_path_never_becomes_zero() {
    let code = principal_wasm(READ_STAKE, "", &stake_result(0));
    let mut job = job(&code, |_| {});
    job.principal_queries = Some(vec![query()]);
    assert!(
        collect(&job).is_ok(),
        "complete principal parent must capture"
    );
    top_only_proof(&mut job, false);
    for refused in [run(&job).map(|_| ()), collect(&job).map(|_| ())] {
        let error = refused.expect_err("omitted original principal proof was accepted");
        assert!(error.to_string().contains("principal"), "{error}");
    }
}

#[test]
fn historical_principal_top_child_and_rolled_back_writes_are_refused() {
    for mutation in [
        WRITE.to_owned(),
        CHILD_WRITE.to_owned(),
        format!("(call $begin){WRITE}(call $rollback)"),
    ] {
        let code = principal_wasm(&mutation, "", &stake_result(7));
        let mut job = job(&code, |_| {});
        job.principal_queries = Some(vec![query()]);
        for refused in [run(&job).map(|_| ()), collect(&job).map(|_| ())] {
            let error =
                refused.expect_err("principal API was allowed to manufacture transient stock");
            assert!(error.to_string().contains("principal"), "{error}");
        }
        assert_eq!(backend(&job).storage(ACCOUNT).unwrap(), Some(vec![7; 96]));
    }
}

#[test]
fn historical_principal_query_census_refuses_substitution_and_duplicates() {
    let code = principal_wasm("", "", &stake_result(7));
    let mut job = job(&code, |_| {});
    let mut other = query();
    other.hotkey = [3; 32];
    for queries in [
        vec![],
        vec![query(), query()],
        vec![other.clone(), query()],
        vec![query(); 4097],
        vec![other],
    ] {
        job.principal_queries = Some(queries);
        for refused in [run(&job).map(|_| ()), collect(&job).map(|_| ())] {
            let error =
                refused.expect_err("principal query identity or bounded census was replaced");
            assert!(error.to_string().contains("principal"), "{error}");
        }
    }
}

#[test]
fn historical_principal_api_and_canonical_layout_are_required() {
    let mut malformed = stake_result(7);
    // Seven encoded with a two-byte compact mode instead of its canonical byte.
    malformed.splice(66..67, [29, 0]);
    let mut trailing = stake_result(7);
    trailing.push(0);
    for code in [
        wasm("", ""),
        principal_wasm("", "", &malformed),
        principal_wasm("", "", &trailing),
    ] {
        let mut job = job(&code, |_| {});
        job.principal_queries = Some(vec![query()]);
        for refused in [run(&job).map(|_| ()), collect(&job).map(|_| ())] {
            assert!(
                refused.is_err(),
                "missing API or noncanonical principal result was accepted"
            );
        }
    }
}

// Only the post-execution API attempts a write. Parent capture succeeds first;
// the completed overlay query must refuse even a transaction rolled back to it.
#[test]
fn historical_principal_completed_overlay_refuses_top_child_and_rollback_writes() {
    for mutation in [
        WRITE.to_owned(),
        CHILD_WRITE.to_owned(),
        format!("(call $begin){WRITE}(call $rollback)"),
    ] {
        let query_body = format!(
            r#"
          (local.set $value (i32.wrap_i64 (call $get (i64.const 51539609536))))
          (if (i32.ne (i32.load8_u offset=1 (local.get $value)) (i32.const 129)) (then {mutation}))"#
        );
        let code = principal_wasm(&query_body, WRITE, &stake_result(7));
        let mut original = job(&code, |storage| {
            storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
        });
        original.principal_queries = Some(vec![query()]);
        assert!(
            collect(&original).is_ok(),
            "original parent-only query must reach valid execution"
        );
        original.principal_effects = true;
        for result in [collect(&original).map(|_| ()), run(&original).map(|_| ())] {
            let error = result.expect_err("completed overlay accepted a mutating principal API");
            assert!(error.to_string().contains("principal"), "{error}");
        }
    }
}

#[test]
fn historical_principal_effects_require_explicit_nonempty_queries() {
    let code = wasm("", "");
    let mut original = job(&code, |_| {});
    assert!(collect(&original).is_ok(), "legacy block must capture");
    original.principal_effects = true;
    for result in [collect(&original).map(|_| ()), run(&original).map(|_| ())] {
        assert!(result
            .expect_err("effect request omitted query census")
            .to_string()
            .contains("query census"));
    }
}
