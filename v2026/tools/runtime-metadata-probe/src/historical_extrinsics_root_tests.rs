//! Storage and body tries follow separate pinned SDK version rules. Synthetic
//! long bodies exercise both public paths; original incident bytes stay in an
//! independently hash-pinned external vector, never in repository fixtures.

use super::*;

/// Include values on both sides of the value-hashing threshold, with distinct
/// bytes so reordering is observable. Expected roots use the SDK directly.
fn long_body_job() -> HistoricalJob {
    let code = wasm("", &format!("{READ}{WRITE}"));
    let mut input = job(&code, |storage| {
        storage.top.insert(ACCOUNT.to_vec(), b"v".to_vec());
    });
    let body: Vec<_> = [10, 31, 32, 64, 1396]
        .into_iter()
        .enumerate()
        .map(|(index, length)| vec![17 + index as u8; length].encode())
        .collect();
    let root = BlakeTwo256::ordered_trie_root(body.clone(), StateVersion::V0);
    assert_ne!(
        root,
        BlakeTwo256::ordered_trie_root(body.clone(), StateVersion::V1),
        "fixture must distinguish body layouts"
    );
    input.extrinsics_hex = body.iter().map(|value| encoded(value)).collect();
    replace_child(&mut input, |header| header.set_extrinsics_root(root));
    input
}

#[test]
fn historical_extrinsics_root_uses_sdk_system_version_scope() {
    for system_version in [0, 1] {
        let version = RuntimeVersion {
            system_version,
            ..RuntimeVersion::default()
        };
        assert_eq!(
            version.state_version(),
            StateVersion::try_from(system_version).unwrap()
        );
        assert_eq!(version.extrinsics_root_state_version(), StateVersion::V0);
        assert_eq!(
            extrinsics_root_state_version(system_version).unwrap(),
            version.extrinsics_root_state_version()
        );
    }
    for unsupported in 2..=u8::MAX {
        assert!(extrinsics_root_state_version(unsupported).is_err());
    }
    // The SDK's storage enum itself accepts raw two. The v1 product wire
    // deliberately has a narrower scope, and must not confuse these policies.
    assert_eq!(StateVersion::try_from(2).unwrap(), StateVersion::V1);
    let future = RuntimeVersion {
        system_version: 2,
        ..RuntimeVersion::default()
    };
    assert_eq!(future.state_version(), StateVersion::V1);
    assert_eq!(future.extrinsics_root_state_version(), StateVersion::V1);
}

#[test]
fn historical_extrinsics_root_capture_and_replay_long_body_keep_storage_v1() {
    let input = long_body_job();
    let replay = run(&input).expect("storage V1 with original body V0 must replay");
    assert!(replay.post_state_reproduced);
    assert_eq!(replay.extrinsics, input.extrinsics_hex.len());
    let captured = capture_tests::collect(&input)
        .expect("capture must use body V0 while reproducing storage V1");
    assert!(captured.replay.post_state_reproduced);
    let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
    assert_eq!(exported.execution_state_version, 1);
    assert_eq!(exported.parent_header_hex, input.parent_header_hex);
    assert_eq!(exported.child_header_hex, input.child_header_hex);
    assert_eq!(exported.extrinsics_hex, input.extrinsics_hex);
    assert_eq!(exported.runtime_code_hex, input.runtime_code_hex);
    assert!(run(&exported).unwrap().post_state_reproduced);
}

#[test]
fn historical_extrinsics_root_capture_and_replay_refuse_storage_layout_header() {
    let mut input = long_body_job();
    let body = input
        .extrinsics_hex
        .iter()
        .map(|value| hex_bytes("fixture extrinsic", value, MAXIMUM_BLOCK_BYTES).unwrap())
        .collect();
    replace_child(&mut input, |header| {
        header.set_extrinsics_root(BlakeTwo256::ordered_trie_root(body, StateVersion::V1));
    });
    for result in [
        run(&input).map(|_| ()),
        capture_tests::collect(&input).map(|_| ()),
    ] {
        let error =
            result.expect_err("storage-layout body root must not be an alternate admission");
        assert!(
            error.to_string().contains("extrinsics root differs"),
            "{error}"
        );
    }
}

#[test]
fn historical_extrinsics_root_original_order_bytes_and_prefixes_remain_required() {
    let original = long_body_job();
    for fault in ["order", "omission", "payload", "trailing", "prefix"] {
        let mut changed = original.clone();
        match fault {
            "order" => changed.extrinsics_hex.swap(0, 1),
            "omission" => {
                changed.extrinsics_hex.pop();
            }
            "payload" => {
                let mut raw = hex_bytes(
                    "fixture body",
                    &changed.extrinsics_hex[4],
                    MAXIMUM_BLOCK_BYTES,
                )
                .unwrap();
                *raw.last_mut().unwrap() ^= 1;
                changed.extrinsics_hex[4] = encoded(&raw);
            }
            "trailing" => changed.extrinsics_hex[0].push_str("00"),
            "prefix" => {
                let raw = hex_bytes(
                    "fixture body",
                    &changed.extrinsics_hex[0],
                    MAXIMUM_BLOCK_BYTES,
                )
                .unwrap();
                changed.extrinsics_hex[0] = encoded(&raw[1..]);
            }
            _ => unreachable!(),
        }
        assert_eq!(changed.child_header_hex, original.child_header_hex);
        for result in [
            run(&changed).map(|_| ()),
            capture_tests::collect(&changed).map(|_| ()),
        ] {
            let error = result.expect_err("original body mutation was accepted");
            assert!(error.to_string().contains("extrinsic"), "{fault}: {error}");
        }
    }
}

#[test]
fn historical_extrinsics_root_preserves_actual_runtime_version_guard() {
    for (actual, declared) in [(1, 0), (2, 1)] {
        let code = wasm_with_version("", "", 8192, actual);
        let mut input = job(&code, |_| {});
        let body = input
            .extrinsics_hex
            .iter()
            .map(|value| hex_bytes("fixture extrinsic", value, MAXIMUM_BLOCK_BYTES).unwrap())
            .collect::<Vec<_>>();
        assert_eq!(
            BlakeTwo256::ordered_trie_root(body.clone(), StateVersion::V0),
            BlakeTwo256::ordered_trie_root(body, StateVersion::V1),
        );
        // The short body has the same root under both layouts. Original code
        // is proved at the parent and returns the distinct actual raw version.
        input.execution_state_version = declared;
        let mut results = vec![run(&input).map(|_| ())];
        // The critical raw1/raw2 case also shares storage V1, and the block
        // makes no writes. Capture reaches strict replay's raw-version guard
        // without a different storage layout masking the intended refusal.
        if actual == 2 {
            results.push(capture_tests::collect(&input).map(|_| ()));
        }
        for result in results {
            let error =
                result.expect_err("body-layout agreement hid a runtime version contradiction");
            assert!(
                error.to_string().contains("executing state version"),
                "actual {actual}, declared {declared}: {error}"
            );
        }
    }
}

#[test]
fn historical_extrinsics_root_capture_and_replay_refuse_unsupported_raw_wire() {
    let original = long_body_job();
    for declared in 2..=u8::MAX {
        let mut input = original.clone();
        input.execution_state_version = declared;
        for result in [
            run(&input).map(|_| ()),
            capture_tests::collect(&input).map(|_| ()),
        ] {
            let error = result.expect_err("unsupported raw system version entered the v1 wire");
            assert!(
                error.to_string().contains("state version unsupported"),
                "{error}"
            );
        }
    }
}

// Read the two original RPC aliases without depending on an optional serde
// feature of sp-version. The SDK selector below still consumes RuntimeVersion.
#[derive(Deserialize)]
struct OriginalRuntimeVersion {
    #[serde(rename = "systemVersion")]
    system_version: u8,
    #[serde(rename = "stateVersion")]
    state_version: u8,
}

#[derive(Deserialize)]
struct OriginalBodyVector {
    schema: String,
    block_number: u32,
    block_hash: String,
    parent_hash: String,
    header_scale_hex: String,
    parent_runtime_version: OriginalRuntimeVersion,
    extrinsics_hex: Vec<String>,
    expected_ordered_root_v0: String,
    expected_ordered_root_v1: String,
}

#[test]
fn historical_extrinsics_root_retained_original_body_matches_sdk_v0_only() {
    use std::io::Read;
    let path = std::env::var_os("URNETWORK_HISTORICAL_BODY_ROOT_VECTOR")
        .expect("selected actual-body qualification requires retained original vector");
    let digest = std::env::var("URNETWORK_HISTORICAL_BODY_ROOT_VECTOR_SHA256")
        .expect("selected actual-body qualification requires original vector SHA256");
    assert_eq!(
        digest.len(),
        64,
        "external SHA256 must be bare canonical hex"
    );
    let expected = hex::decode(&digest).expect("external SHA256 is hex");
    assert_eq!(
        hex::encode(&expected),
        digest,
        "external SHA256 must be lowercase"
    );
    let file = std::fs::File::open(path).expect("retained original vector opens");
    assert!(file.metadata().unwrap().is_file());
    let mut raw = Vec::new();
    file.take(256 * 1024 + 1).read_to_end(&mut raw).unwrap();
    assert!(!raw.is_empty() && raw.len() <= 256 * 1024);
    assert_eq!(
        sha2_256(&raw).as_slice(),
        expected.as_slice(),
        "original vector digest differs"
    );
    let original: OriginalBodyVector = serde_json::from_slice(&raw).unwrap();
    assert_eq!(
        original.schema,
        "runtime473-original-block-body-root-vector-v1"
    );
    assert_eq!(original.extrinsics_hex.len(), 15);
    assert_eq!(original.parent_runtime_version.system_version, 1);
    assert_eq!(
        original.parent_runtime_version.state_version,
        original.parent_runtime_version.system_version
    );
    let version = RuntimeVersion {
        system_version: original.parent_runtime_version.system_version,
        ..RuntimeVersion::default()
    };
    assert_eq!(version.state_version(), StateVersion::V1);
    let header: NativeHeader = scale_exact(
        "original child header",
        &hex_bytes(
            "original child header",
            &original.header_scale_hex,
            MAXIMUM_HEADER_BYTES,
        )
        .unwrap(),
    )
    .unwrap();
    assert_eq!(*header.number(), original.block_number);
    assert_eq!(encoded(header.hash().as_bytes()), original.block_hash);
    assert_eq!(
        encoded(header.parent_hash().as_bytes()),
        original.parent_hash
    );
    let body: Vec<_> = original
        .extrinsics_hex
        .iter()
        .map(|value| {
            let raw = hex_bytes("original extrinsic", value, MAXIMUM_BLOCK_BYTES).unwrap();
            let opaque: OpaqueExtrinsic = scale_exact("original extrinsic", &raw).unwrap();
            assert_eq!(
                opaque.encode(),
                raw,
                "original SCALE prefix/payload changed"
            );
            raw
        })
        .collect();
    let root_v0 = BlakeTwo256::ordered_trie_root(body.clone(), StateVersion::V0);
    let root_v1 = BlakeTwo256::ordered_trie_root(body.clone(), StateVersion::V1);
    assert_eq!(
        encoded(root_v0.as_bytes()),
        original.expected_ordered_root_v0
    );
    assert_eq!(
        encoded(root_v1.as_bytes()),
        original.expected_ordered_root_v1
    );
    assert_eq!(root_v0, *header.extrinsics_root());
    assert_ne!(root_v1, *header.extrinsics_root());
    let selected =
        extrinsics_root_state_version(original.parent_runtime_version.system_version).unwrap();
    assert_eq!(selected, version.extrinsics_root_state_version());
    assert_eq!(
        BlakeTwo256::ordered_trie_root(body.clone(), selected),
        *header.extrinsics_root()
    );
    for fault in ["order", "omission", "payload", "prefix"] {
        let mut changed = body.clone();
        match fault {
            "order" => changed.swap(0, 1),
            "omission" => {
                changed.pop();
            }
            "payload" => {
                *changed[1].last_mut().unwrap() ^= 1;
            }
            "prefix" => {
                changed[0].remove(0);
            }
            _ => unreachable!(),
        }
        assert_ne!(
            BlakeTwo256::ordered_trie_root(changed, selected),
            *header.extrinsics_root(),
            "{fault}"
        );
    }
}
