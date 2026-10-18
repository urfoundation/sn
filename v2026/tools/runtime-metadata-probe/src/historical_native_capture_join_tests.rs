//! Original Wasm captures the transaction from its actual encoded body and
//! moves its computed pool stake. Receipt and contract execution are separate
//! Go consumers; these synthetic programs confer no deployed-runtime authority.

use super::*;

/// Each original capture reads its current stock, then commits one write. Both
/// host observations remain in the trace; only the independently censused set
/// is a financial effect. Rollback must discard the complete pair and mutation.
fn committed_captures(trace: &observer::ObservationReport) -> Vec<&observer::Observation> {
    let records: Vec<_> = trace
        .observations
        .iter()
        .filter(|value| value.purpose == "native-principal-vault-capture")
        .collect();
    assert_eq!(
        records.len() % 2,
        0,
        "original capture read/write pair absent"
    );
    let mutations = trace.principal_mutations.as_ref().unwrap();
    let mut captures = Vec::new();
    for pair in records.chunks_exact(2) {
        let (read, write) = (pair[0], pair[1]);
        assert_eq!(
            read.operation, "get",
            "original capture begins with actual read"
        );
        assert_eq!(
            write.operation, "set",
            "original capture ends with committed write"
        );
        assert!(
            read.ordinal < write.ordinal,
            "original capture host order changed"
        );
        assert_eq!(read.key_hex, write.key_hex);
        let native = write.native.as_ref().unwrap();
        let field = |name| {
            &native
                .memory
                .iter()
                .find(|value| value.name == name)
                .unwrap()
                .bytes_hex
        };
        let returned = read.storage_return.as_ref().unwrap();
        assert!(returned.present, "original capture stock read is absent");
        assert_eq!(
            returned.value_hex.as_ref(),
            Some(field("before")),
            "original capture before amount differs from actual storage read"
        );
        assert_eq!(
            write.value_hex.as_ref(),
            Some(field("after")),
            "original capture after amount differs from committed storage bytes"
        );
        assert_eq!(
            read.native.as_ref().unwrap().execution_phase_hex,
            native.execution_phase_hex,
            "original capture read/write Apply identity changed"
        );
        let matched: Vec<_> = mutations
            .iter()
            .filter(|value| value.ordinal == write.ordinal)
            .collect();
        assert_eq!(
            matched.len(),
            1,
            "original capture lacks its unique committed mutation"
        );
        let mutation = matched[0];
        assert_eq!(mutation.operation, write.operation);
        assert_eq!(mutation.key_hex, write.key_hex);
        let value = hex_bytes(
            "captured storage value",
            write.value_hex.as_ref().unwrap(),
            8,
        )
        .unwrap();
        assert_eq!(mutation.value_sha256, Some(sha2_256(&value)));
        assert!(
            mutations.iter().all(|value| value.ordinal != read.ordinal),
            "original capture read was counted as a committed mutation"
        );
        captures.push(write);
    }
    captures
}

#[test]
fn historical_native_vault_capture_exports_original_transaction_causes() {
    let mut jobs = Vec::new();
    for (name, after, committed, discarded) in [
        ("capture", 0, 2, 0),
        ("capture-causes", 0, 5, 0),
        ("capture-unclassified", 0, 4, 0),
        ("capture-rollback", 20, 1, 2),
    ] {
        let (job, _) = fixture_with_principal_effects(false, Some(Some(14)), Some(name));
        let captured = super::super::capture_tests::collect(&job)
            .expect("actual original vault capture program");
        let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
        let replay = run(&exported).expect("actual original vault replay program");
        for report in [&captured.replay, &replay] {
            assert!(report.post_state_reproduced);
            assert_eq!(report.extrinsics, 1);
            assert_eq!(
                report.closing_principals.as_ref().unwrap()[0].opening_stake_alpha,
                Some(after.to_string())
            );
            let trace = report.hook_observations.as_ref().unwrap();
            assert_eq!(trace.principal_mutations.as_ref().unwrap().len(), committed);
            assert_eq!(trace.discarded_on_rollback, discarded);
            let captures = committed_captures(trace);
            assert_eq!(
                captures.len(),
                usize::from(name != "capture-rollback"),
                "original committed capture census"
            );
            for value in captures {
                let native = value.native.as_ref().unwrap();
                assert_eq!(native.execution_phase_hex.as_deref(), Some("0x0000000000"));
                let transaction = native
                    .memory
                    .iter()
                    .find(|value| value.name == "transaction-hash")
                    .unwrap();
                assert_eq!(
                    transaction.bytes_hex,
                    encoded(&[0x99; 32]),
                    "transaction came from original body"
                );
            }
        }
        jobs.push((name, exported));
    }
    if let Some(directory) = std::env::var_os("URNETWORK_NATIVE_VAULT_CAPTURE_FIXTURE_OUT") {
        let directory = Path::new(&directory);
        assert!(directory.is_absolute() && directory.is_dir());
        for (name, job) in jobs {
            let mut file = OpenOptions::new()
                .create_new(true)
                .write(true)
                .mode(0o600)
                .open(directory.join(format!("principal-{name}.json")))
                .unwrap();
            file.write_all(&serde_json::to_vec(&job).unwrap()).unwrap();
            file.sync_all().unwrap();
        }
        std::fs::File::open(directory).unwrap().sync_all().unwrap();
    }
}

// The second block is backed by the first block's exact post-state. Two
// transactions in one block instead retain distinct original Apply indices.
#[test]
fn historical_native_vault_capture_exports_original_capture_sequences() {
    let (same, _) =
        fixture_with_principal_effects(false, Some(Some(14)), Some("capture-same-block"));
    let (first, post_storage) =
        fixture_with_principal_effects(false, Some(Some(14)), Some("capture-next-block"));
    let code = hex_bytes("sequence code", &first.runtime_code_hex, MAXIMUM_CODE_BYTES).unwrap();
    let backing = TestExternalities::<Blake2Hasher>::new_with_code_and_state(
        &code,
        post_storage,
        StateVersion::V1,
    );
    let (nodes, root) = backing.into_raw_snapshot();
    let parent: NativeHeader = scale_exact(
        "parent",
        &hex_bytes("parent", &first.child_header_hex, MAXIMUM_HEADER_BYTES).unwrap(),
    )
    .unwrap();
    assert_eq!(root, *parent.state_root());
    let mut second = first.clone();
    second.parent_header_hex = first.child_header_hex.clone();
    second.parent_hash = first.child_hash;
    second.extrinsics_hex = vec![encoded(&vec![0x98u8; 32].encode())];
    let child = NativeHeader::new(
        102,
        BlakeTwo256::ordered_trie_root(vec![vec![0x98u8; 32].encode()], StateVersion::V0),
        root,
        H256(first.child_hash),
        Digest::default(),
    );
    second.child_header_hex = encoded(&child.encode());
    second.child_hash = child.hash().0;
    second.proof_nodes_hex = nodes
        .into_iter()
        .map(|(_, (value, _))| encoded(&value))
        .collect::<BTreeSet<_>>()
        .into_iter()
        .collect();
    let mut jobs = Vec::new();
    for (name, job, stock, amounts, transactions) in [
        (
            "capture-same-block",
            same,
            "14",
            vec!["20", "5"],
            vec![0x99, 0x98],
        ),
        ("capture-next-block", first, "14", vec!["20"], vec![0x99]),
        ("capture-next-block-102", second, "0", vec!["6"], vec![0x98]),
    ] {
        let captured =
            super::super::capture_tests::collect(&job).expect("actual original capture sequence");
        let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
        let replay = run(&exported).expect("strict original capture sequence replay");
        for report in [&captured.replay, &replay] {
            assert!(report.post_state_reproduced);
            assert_eq!(report.extrinsics, transactions.len());
            assert_eq!(
                report.opening_principals.as_ref().unwrap()[0]
                    .opening_stake_alpha
                    .as_deref(),
                Some(stock)
            );
            assert_eq!(
                report.closing_principals.as_ref().unwrap()[0]
                    .opening_stake_alpha
                    .as_deref(),
                Some("0")
            );
            let trace = report.hook_observations.as_ref().unwrap();
            let captures = committed_captures(trace);
            assert_eq!(
                captures.len(),
                amounts.len(),
                "complete original capture sequence census"
            );
            assert_eq!(
                trace.principal_mutations.as_ref().unwrap().len(),
                if amounts.len() == 2 { 4 } else { 2 }
            );
            for (index, capture) in captures.iter().enumerate() {
                let native = capture.native.as_ref().unwrap();
                let phase = [vec![0], (index as u32).to_le_bytes().to_vec()].concat();
                assert_eq!(
                    native.execution_phase_hex.as_deref(),
                    Some(encoded(&phase).as_str()),
                    "original capture Apply index"
                );
                let field = |name| {
                    &native
                        .memory
                        .iter()
                        .find(|value| value.name == name)
                        .unwrap()
                        .bytes_hex
                };
                assert_eq!(
                    field("transaction-hash"),
                    &encoded(&[transactions[index]; 32])
                );
                assert_eq!(
                    field("before"),
                    &encoded(&words(&[amounts[index].parse::<u64>().unwrap()]))
                );
                assert_eq!(field("after"), &encoded(&words(&[0])));
            }
        }
        jobs.push((name, exported));
    }
    if let Some(directory) = std::env::var_os("URNETWORK_NATIVE_VAULT_CAPTURE_SEQUENCE_FIXTURE_OUT")
    {
        let directory = Path::new(&directory);
        assert!(directory.is_absolute() && directory.is_dir());
        for (name, job) in jobs {
            let mut file = OpenOptions::new()
                .create_new(true)
                .write(true)
                .mode(0o600)
                .open(directory.join(format!("principal-{name}.json")))
                .unwrap();
            file.write_all(&serde_json::to_vec(&job).unwrap()).unwrap();
            file.sync_all().unwrap();
        }
        std::fs::File::open(directory).unwrap().sync_all().unwrap();
    }
}

/// The complete body is opaque bytes, not a vector of inferred integer words.
/// Check the independent SCALE frame before relying on capture/replay output.
#[test]
fn historical_native_vault_capture_extrinsics_are_exact_opaque_bytes() {
    for (mode, transactions) in [
        ("capture", vec![0x99u8]),
        ("capture-same-block", vec![0x99u8, 0x98u8]),
        ("capture-next-block", vec![0x99u8]),
    ] {
        let (job, _) = fixture_with_principal_effects(false, Some(Some(14)), Some(mode));
        assert_eq!(job.extrinsics_hex.len(), transactions.len());
        let mut expected_body = Vec::new();
        for (raw, transaction) in job.extrinsics_hex.iter().zip(transactions) {
            let bytes = hex_bytes("fixture extrinsic", raw, MAXIMUM_BLOCK_BYTES).unwrap();
            let expected = [vec![0x80u8], vec![transaction; 32]].concat();
            assert_eq!(
                bytes, expected,
                "original capture body must encode exact bytes"
            );
            let decoded: OpaqueExtrinsic =
                scale_exact("fixture extrinsic", &bytes).expect("canonical fixture extrinsic");
            assert_eq!(decoded.encode(), expected);
            expected_body.push(expected);
        }
        let child: NativeHeader = scale_exact(
            "fixture child",
            &hex_bytes("fixture child", &job.child_header_hex, MAXIMUM_HEADER_BYTES).unwrap(),
        )
        .unwrap();
        assert_eq!(
            *child.extrinsics_root(),
            BlakeTwo256::ordered_trie_root(expected_body, StateVersion::V0),
            "original capture header must commit the exact byte body"
        );
    }
}

/// Both real entry points reject malformed SCALE before executing original
/// Wasm. The former inferred-i32 encoding is one explicit regression vector.
#[test]
fn historical_native_vault_capture_and_replay_refuse_nonexact_extrinsics() {
    let (original, _) = fixture_with_principal_effects(false, Some(Some(14)), Some("capture"));
    let canonical = [vec![0x80u8], vec![0x99u8; 32]].concat();
    assert_eq!(original.extrinsics_hex, vec![encoded(&canonical)]);
    let mut trailing = canonical.clone();
    trailing.push(0);
    for (name, malformed) in [
        ("inferred integer words", vec![0x99i32; 32].encode()),
        ("truncated payload", canonical[..32].to_vec()),
        ("trailing payload", trailing),
        (
            "nonminimal length",
            [vec![0x81u8, 0], vec![0x99u8; 32]].concat(),
        ),
    ] {
        let mut changed = original.clone();
        changed.extrinsics_hex = vec![encoded(&malformed)];
        let capture_error = super::super::capture_tests::collect(&changed)
            .err()
            .expect("nonexact capture body was admitted");
        assert!(
            capture_error
                .to_string()
                .contains("capture extrinsic SCALE"),
            "{name}: {capture_error}"
        );
        let replay_error = run(&changed)
            .err()
            .expect("nonexact replay body was admitted");
        assert!(
            replay_error.to_string().contains("extrinsic SCALE"),
            "{name}: {replay_error}"
        );
    }
}
