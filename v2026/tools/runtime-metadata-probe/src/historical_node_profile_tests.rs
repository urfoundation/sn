//! Exact original code outside the pinned node's parser/VM profile is refused;
//! observer labels, complete proofs and unchanged target roots cannot admit it.

use super::*;

fn unsupported_programs() -> Vec<(&'static str, Vec<u8>)> {
    [
        ("bulk-copy", "", "(memory.copy (i32.const 5000) (i32.const 4200) (i32.const 32))"),
        ("bulk-fill", "", "(memory.fill (i32.const 5000) (i32.const 0) (i32.const 32))"),
        ("bulk-init", "(data $passive \"synthetic\")", "(memory.init $passive (i32.const 5000) (i32.const 0) (i32.const 9)) (data.drop $passive)"),
        ("saturating-conversion", "", "(drop (i32.trunc_sat_f32_s (f32.const 0)))"),
        ("sign-extension", "", "(drop (i32.extend8_s (i32.const 0)))"),
        ("simd", "", "(drop (v128.const i32x4 0 0 0 0))"),
        ("reference-types", "", "(drop (ref.null func))"),
        ("multi-value", "(func $pair (result i32 i32) (i32.const 0) (i32.const 0))", "call $pair drop drop"),
    ]
    .into_iter()
    .map(|(name, declarations, body)| (name, wasm(declarations, body)))
    .collect()
}

#[test]
fn historical_replay_node_profile_refuses_unsupported_wasm_without_rewriting_original() {
    for (name, code) in unsupported_programs() {
        for observed in [false, true] {
            let mut original = job(&code, |_| {});
            if observed {
                original.observation_profile = Some(observation_profile(
                    &code,
                    "Core_execute_block",
                    "fee-withdraw",
                ));
            }
            let before = serde_json::to_vec(&original).unwrap();
            let error = run(&original)
                .err()
                .expect("unsupported original Wasm was admitted");
            assert!(
                error.to_string().contains("cannot deserialize module"),
                "{name}: {error}"
            );
            assert_eq!(
                serde_json::to_vec(&original).unwrap(),
                before,
                "{name}: original job rewritten"
            );
        }
    }
}

#[test]
fn historical_capture_node_profile_refuses_unsupported_wasm_without_committing_parent() {
    for (name, code) in unsupported_programs() {
        let original = job(&code, |_| {});
        let parent = backend(&original);
        let root = *parent.root();
        let account = parent.storage(ACCOUNT).unwrap();
        let child = parent
            .child_storage(&ChildInfo::new_default(OWNER), b"k")
            .unwrap();
        let raw = serde_json::to_vec(&request(&original)).unwrap();
        let error = capture::capture_historical_on_backend(&raw, &parent, &AtomicBool::new(false))
            .err()
            .expect("unsupported original capture was admitted");
        assert!(
            error.to_string().contains("cannot deserialize module"),
            "{name}: {error}"
        );
        assert_eq!(*parent.root(), root, "{name}: parent root changed");
        assert_eq!(
            parent.storage(ACCOUNT).unwrap(),
            account,
            "{name}: top state changed"
        );
        assert_eq!(
            parent
                .child_storage(&ChildInfo::new_default(OWNER), b"k")
                .unwrap(),
            child,
            "{name}: child state changed"
        );
    }
}
