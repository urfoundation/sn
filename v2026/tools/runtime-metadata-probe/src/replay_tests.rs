//! Synthetic modules exercise actual pinned SDK execution. They are finite
//! fixtures, not Subtensor releases or economic equivalence evidence.

use super::*;
use frame_metadata::v14::{ExtrinsicMetadata, RuntimeMetadataV14};
use scale_info::MetaType;
use sp_core::hashing::{blake2_256, blake2_64};
use sp_version::RuntimeVersion;
use std::borrow::Cow;

fn artifact(version: u32, imports: &str, body: &str) -> ReplayArtifact {
    let metadata: frame_metadata::RuntimeMetadataPrefixed = RuntimeMetadataV14::new(
        vec![],
        ExtrinsicMetadata {
            ty: MetaType::new::<()>(),
            version: 4,
            signed_extensions: vec![],
        },
        MetaType::new::<()>(),
    )
    .into();
    let metadata = metadata.encode();
    let opaque = metadata.encode();
    let runtime_version = RuntimeVersion {
        spec_name: Cow::Borrowed("synthetic-replay-runtime"),
        spec_version: version,
        apis: Cow::Owned(vec![(blake2_64(b"Core"), 4)]),
        transaction_version: 1,
        system_version: 1,
        ..RuntimeVersion::default()
    }
    .encode();
    let escaped = |raw: &[u8]| {
        raw.iter()
            .map(|byte| format!("\\{byte:02x}"))
            .collect::<String>()
    };
    let wasm = wat::parse_str(format!(r#"(module
        (import "env" "ext_storage_set_version_1" (func $set (param i64 i64)))
        (import "env" "ext_storage_clear_version_1" (func $clear (param i64)))
        (import "env" "ext_storage_exists_version_1" (func $exists (param i64) (result i32)))
        (import "env" "ext_storage_start_transaction_version_1" (func $begin))
        (import "env" "ext_storage_rollback_transaction_version_1" (func $rollback))
        (import "env" "ext_storage_commit_transaction_version_1" (func $commit))
        {imports}
        (memory (export "memory") 8)
        (global (export "__heap_base") i32 (i32.const 8192))
        (data (i32.const 32) "{version_data}")
        (data (i32.const 1024) "{metadata_data}")
        (data (i32.const 2048) "avbq")
        (data (i32.const 3072) "r")
        (func (export "Core_version") (param i32 i32) (result i64) (i64.const {version_result}))
        (func (export "Metadata_metadata") (param i32 i32) (result i64) (i64.const {metadata_result}))
        (func (export "BlockBuilder_apply_extrinsic") (param i32 i32) (result i64)
            (local $count i32) {body} (i64.const 4294970368)))"#,
        version_data = escaped(&runtime_version), metadata_data = escaped(&opaque),
        version_result = (runtime_version.len() as u64) << 32 | 32,
        metadata_result = (opaque.len() as u64) << 32 | 1024,
    )).expect("synthetic transition Wasm compiles");
    ReplayArtifact {
        expected: ExpectedRuntimeArtifact {
            spec_name: "synthetic-replay-runtime".to_owned(),
            spec_version: version,
            transaction_version: 1,
            state_version: 1,
            metadata_version: 14,
            code_size: wasm.len() as u64,
            metadata_size: metadata.len() as u64,
            code_sha256: sha2_256(&wasm),
            code_blake2b_256: blake2_256(&wasm),
            metadata_sha256: sha2_256(&metadata),
            metadata_blake2b_256: blake2_256(&metadata),
        },
        wasm_hex: format!("0x{}", hex::encode(wasm)),
    }
}

fn entry(key: &str, value: &str) -> ReplayEntry {
    ReplayEntry {
        key: format!("0x{}", hex::encode(key)),
        value: format!("0x{}", hex::encode(value)),
    }
}

fn job(body: &str, initial: Vec<ReplayEntry>, expected: Vec<ReplayEntry>) -> ReplayJob {
    let cases = vec![ReplayCase {
        name: "synthetic-transition".to_owned(),
        initial_state: initial,
        steps: vec![ReplayStep {
            method: "BlockBuilder_apply_extrinsic".to_owned(),
            input: "0x01".to_owned(),
            expected_output: "0x72".to_owned(),
            expected_state: expected,
        }],
    }];
    let cases_json = serde_json::to_string(&cases).unwrap();
    ReplayJob {
        schema: REPLAY_SCHEMA.to_owned(),
        policy_sha256: [1; 32],
        source_build_evidence_sha256: [2; 32],
        rules_sha256: [3; 32],
        cases_sha256: sha2_256(cases_json.as_bytes()),
        base: artifact(11, "", body),
        candidate: artifact(12, "", body),
        cases_json,
    }
}

fn run(job: &ReplayJob) -> Result<Vec<u8>, ProbeError> {
    replay_json(&serde_json::to_vec(job).unwrap())
}

const INSERT_DELETE: &str = "(call $set (i64.const 4294969344) (i64.const 4294969345)) (call $clear (i64.const 4294969346))";

#[test]
fn replay_compares_insertions_and_deletions_for_both_artifacts() {
    let job = job(INSERT_DELETE, vec![entry("b", "q")], vec![entry("a", "v")]);
    let raw = serde_json::to_vec(&job).unwrap();
    let report: ReplayReport =
        serde_json::from_slice(&replay_json(&raw).expect("exact state transition refused"))
            .unwrap();
    assert_eq!(report.job_sha256, sha2_256(&raw));
    assert_eq!(report.rules_sha256, job.rules_sha256);
    assert_eq!(report.cases_sha256, sha2_256(job.cases_json.as_bytes()));
    assert_ne!(report.cases_sha256, report.rules_sha256);
    assert_eq!((report.cases, report.steps), (1, 1));
    assert!(
        report.finite_replay_only
            && !report.semantic_rules_verified
            && !report.complete_semantic_equivalence
            && !report.production_selection
    );
    assert_ne!(report.outputs_sha256, [0; 32]);
}

#[test]
fn replay_rejects_same_output_with_changed_or_deleted_storage() {
    for body in [
        "",
        "(call $set (i64.const 4294969344) (i64.const 4294969345))",
        "(call $clear (i64.const 4294969346))",
    ] {
        let mut job = job(INSERT_DELETE, vec![entry("b", "q")], vec![entry("a", "v")]);
        job.candidate = artifact(12, "", body);
        let error =
            run(&job).expect_err("same-output candidate escaped complete storage comparison");
        assert!(
            error
                .to_string()
                .contains("candidate synthetic-transition output or storage differs"),
            "{error}"
        );
    }
}

#[test]
fn replay_retains_state_between_steps_and_balances_transactions() {
    let body = r#"(if (i32.eq (i32.load8_u (local.get 0)) (i32.const 1))
        (then (call $begin) (call $set (i64.const 4294969344) (i64.const 4294969345)) (call $rollback)
            (if (call $exists (i64.const 4294969344)) (then unreachable))
            (call $begin) (call $set (i64.const 4294969344) (i64.const 4294969345)) (call $commit))
        (else (if (i32.eqz (call $exists (i64.const 4294969344))) (then unreachable))
            (call $clear (i64.const 4294969344))))"#;
    let mut job = job(body, vec![], vec![entry("a", "v")]);
    let mut cases: Vec<ReplayCase> = serde_json::from_str(&job.cases_json).unwrap();
    let mut second = cases[0].steps[0].clone();
    second.input = "0x02".to_owned();
    second.expected_state.clear();
    cases[0].steps.push(second);
    job.cases_json = serde_json::to_string(&cases).unwrap();
    job.cases_sha256 = sha2_256(job.cases_json.as_bytes());
    let report: ReplayReport =
        serde_json::from_slice(&run(&job).expect("ordered commit and rollback replay refused"))
            .unwrap();
    assert_eq!(report.steps, 2);
    let unfinished = self::job("(call $begin)", vec![], vec![]);
    assert!(run(&unfinished)
        .unwrap_err()
        .to_string()
        .contains("unfinished storage transaction"));
}

#[test]
fn replay_requires_exact_old_and_new_code_metadata_and_version() {
    for base in [true, false] {
        for fault in ["code", "blake", "metadata", "version"] {
            let mut job = job("", vec![], vec![]);
            let selected = if base {
                &mut job.base
            } else {
                &mut job.candidate
            };
            match fault {
                "code" => selected.expected.code_sha256[0] ^= 1,
                "blake" => selected.expected.code_blake2b_256[0] ^= 1,
                "metadata" => selected.expected.metadata_blake2b_256[0] ^= 1,
                "version" => selected.expected.spec_version += 1,
                _ => unreachable!(),
            }
            let error = run(&job).expect_err("unbound old/new runtime artifact accepted");
            assert!(
                error.to_string().contains(if base {
                    "replay base artifact"
                } else {
                    "replay candidate artifact"
                }),
                "{fault}: {error}"
            );
        }
    }
}

#[test]
fn replay_refuses_offchain_child_and_unimplemented_storage_hosts() {
    for (name, signature, call) in [
        (
            "ext_offchain_timestamp_version_1",
            "(result i64)",
            "(drop (call $denied))",
        ),
        (
            "ext_default_child_storage_get_version_1",
            "(param i64 i64) (result i64)",
            "(drop (call $denied (i64.const 0) (i64.const 0)))",
        ),
        (
            "ext_storage_clear_prefix_version_1",
            "(param i64)",
            "(call $denied (i64.const 0))",
        ),
    ] {
        let mut job = job("", vec![], vec![]);
        job.candidate = artifact(
            12,
            &format!("(import \"env\" \"{name}\" (func $denied {signature}))"),
            call,
        );
        let error = run(&job).expect_err("unqualified host was executed");
        assert!(
            error
                .to_string()
                .contains(&format!("missing function env:{name}")),
            "{error}"
        );
    }
}

#[test]
fn replay_bounds_host_operations_values_keys_and_transactions() {
    for (body, message) in [
        (
            "(call $set (i64.const 4294969344) (i64.const 1125904201818112))",
            "storage value limit",
        ),
        (
            "(call $set (i64.const 2203318224896) (i64.const 4294969345))",
            "storage key limit",
        ),
        (
            "(loop $again (drop (call $exists (i64.const 4294969344))) (br $again))",
            "storage operation limit",
        ),
        (
            "(loop $again (call $begin) (br $again))",
            "transaction depth limit",
        ),
        ("(call $rollback)", "unbalanced transaction"),
    ] {
        let job = job(body, vec![], vec![]);
        let error = run(&job).expect_err("unbounded replay host accepted");
        assert!(
            error.to_string().contains(message),
            "expected {message}: {error}"
        );
    }
}

#[test]
fn replay_rejects_ambiguous_rules_and_runtime_code_mutation() {
    let mut job = job("", vec![], vec![]);
    job.cases_json.push(' ');
    assert!(run(&job)
        .unwrap_err()
        .to_string()
        .contains("exact rules binding differs"));
    let duplicate = self::job("", vec![entry("a", "v"), entry("a", "v")], vec![]);
    assert!(run(&duplicate)
        .unwrap_err()
        .to_string()
        .contains("duplicate or unsorted"));
    let code = self::job("", vec![entry(":code", "fake")], vec![]);
    assert!(run(&code)
        .unwrap_err()
        .to_string()
        .contains("state keys are invalid"));
    let mut mutation = self::job("", vec![], vec![]);
    mutation.candidate = artifact(12, "", "(i32.store (i32.const 4096) (i32.const 1685021498)) (i32.store8 (i32.const 4100) (i32.const 101)) (call $clear (i64.const 21474840576))");
    assert!(run(&mutation)
        .unwrap_err()
        .to_string()
        .contains("runtime code is immutable"));
}
