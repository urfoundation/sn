//! Independently rooted original programs cross a real :code write. The first
//! runtime executes 101/102; the second executes 103 onward with the same stake
//! authority. No reported replay root supplies an expected child root.

use super::*;

/// Keep each original read distinct from the one committed financial effect,
/// including the zero-earning write after the first epoch has completed.
fn assert_renewed_principal_effects(report: &HistoricalReport, opening: u64, earning: u64) {
    let opening_principals = report.opening_principals.as_ref().unwrap();
    let closing_principals = report.closing_principals.as_ref().unwrap();
    assert_eq!(opening_principals.len(), 1);
    assert_eq!(closing_principals.len(), 1);
    assert_eq!(
        opening_principals[0].opening_stake_alpha,
        Some(opening.to_string())
    );
    assert_eq!(
        closing_principals[0].opening_stake_alpha,
        Some((opening + earning + 4).to_string())
    );
    let trace = report.hook_observations.as_ref().unwrap();
    let mutations = trace.principal_mutations.as_ref().unwrap();
    assert_eq!(trace.discarded_on_rollback, 0);
    assert_eq!(mutations.len(), 4);
    let key = encoded(b"synthetic-opening-stake");
    let mut previous = 0;
    for (index, (purpose, before, after)) in [
        ("native-principal-earning", opening, opening + earning),
        (
            "native-principal-deposit",
            opening + earning,
            opening + earning + 5,
        ),
        (
            "native-principal-withdrawal",
            opening + earning + 5,
            opening + earning + 2,
        ),
        (
            "native-principal-refund",
            opening + earning + 2,
            opening + earning + 4,
        ),
    ]
    .into_iter()
    .enumerate()
    {
        let records: Vec<_> = trace
            .observations
            .iter()
            .filter(|record| record.purpose == purpose)
            .collect();
        assert_eq!(records.len(), 2, "original get/set pair missing: {purpose}");
        let (read, write) = (records[0], records[1]);
        assert_eq!(read.operation, "get", "{purpose}");
        assert_eq!(write.operation, "set", "{purpose}");
        assert!(previous < read.ordinal && read.ordinal < write.ordinal);
        previous = write.ordinal;
        assert_eq!(read.key_hex, key);
        assert_eq!(write.key_hex, key);
        assert!(read.value_hex.is_none());
        let returned = read.storage_return.as_ref().unwrap();
        assert!(returned.present);
        assert_eq!(returned.value_hex, Some(encoded(&words(&[before]))));
        assert!(write.storage_return.is_none());
        assert_eq!(write.value_hex, Some(encoded(&words(&[after]))));
        let native = write.native.as_ref().unwrap();
        assert_eq!(native.execution_phase_hex.as_deref(), Some("0x02"));
        assert_eq!(
            read.native.as_ref().unwrap().execution_phase_hex,
            native.execution_phase_hex
        );
        for (name, value) in [
            ("netuid", vec![25, 0]),
            ("hotkey", vec![0x11; 32]),
            ("coldkey", vec![0x33; 32]),
            ("before", words(&[before])),
            ("after", words(&[after])),
        ] {
            assert_eq!(
                native
                    .memory
                    .iter()
                    .find(|field| field.name == name)
                    .unwrap()
                    .bytes_hex,
                encoded(&value),
                "original principal memory differs: {purpose}/{name}"
            );
        }
        let mutation = &mutations[index];
        assert_eq!(mutation.ordinal, write.ordinal);
        assert_eq!(mutation.operation, write.operation);
        assert_eq!(mutation.key_hex, write.key_hex);
        assert_eq!(mutation.value_sha256, Some(sha2_256(&words(&[after]))));
        assert!(mutations.iter().all(|item| item.ordinal != read.ordinal));
    }
}

/// Prove each independently rooted block's cause census and stock progression
/// while both capture and replay follow the original runtime-code transition.
#[test]
fn historical_native_runtime_renewal_exports_actual_upgrade_and_principal_jobs() {
    let (second, _) = fixture_with_principal_effects(true, Some(Some(14)), Some("causes"));
    let second_code = hex_bytes(
        "second original program",
        &second.runtime_code_hex,
        MAXIMUM_CODE_BYTES,
    )
    .unwrap();
    let (first, mut stock) = fixture_with_runtime_upgrade(
        true,
        Some(Some(14)),
        Some("causes"),
        None,
        true,
        None,
        Some(&second_code),
    );
    let first_code = hex_bytes(
        "first original program",
        &first.runtime_code_hex,
        MAXIMUM_CODE_BYTES,
    )
    .unwrap();
    assert_ne!(first.runtime_code_sha256, second.runtime_code_sha256);
    assert_ne!(
        first.runtime_code_blake2b_256,
        second.runtime_code_blake2b_256
    );
    let mut jobs = vec![first];
    for number in 102..=105 {
        let original = if number == 102 { &jobs[0] } else { &second };
        let code = if number == 102 {
            &first_code
        } else {
            &second_code
        };
        let backing = TestExternalities::<Blake2Hasher>::new_with_code_and_state(
            code,
            stock.clone(),
            StateVersion::V1,
        );
        let (nodes, parent_root) = backing.into_raw_snapshot();
        let previous = jobs.last().unwrap();
        let parent: NativeHeader = scale_exact(
            "upgrade parent",
            &hex_bytes(
                "upgrade parent",
                &previous.child_header_hex,
                MAXIMUM_HEADER_BYTES,
            )
            .unwrap(),
        )
        .unwrap();
        assert_eq!(parent_root, *parent.state_root());
        stock.top.insert(key(b"System", b"Events", false), vec![0]);
        stock.top.insert(
            b"synthetic-opening-stake".to_vec(),
            words(&[24 + 4 * u64::from(number - 101)]),
        );
        stock.top.insert(b":code".to_vec(), second_code.clone());
        let expected = TestExternalities::<Blake2Hasher>::new_with_code_and_state(
            &second_code,
            stock.clone(),
            StateVersion::V1,
        );
        let child = NativeHeader::new(
            number,
            BlakeTwo256::ordered_trie_root(Vec::<Vec<u8>>::new(), StateVersion::V0),
            *expected.backend.root(),
            H256(previous.child_hash),
            Digest::default(),
        );
        let mut job = original.clone();
        job.parent_header_hex = previous.child_header_hex.clone();
        job.parent_hash = previous.child_hash;
        job.child_header_hex = encoded(&child.encode());
        job.child_hash = child.hash().0;
        job.proof_nodes_hex = nodes
            .into_iter()
            .map(|(_, (value, _))| encoded(&value))
            .collect::<BTreeSet<_>>()
            .into_iter()
            .collect();
        jobs.push(job);
    }
    for (index, job) in jobs.iter().enumerate() {
        let original = run(job).expect("independently rooted original runtime renewal job");
        assert!(original.post_state_reproduced && !original.runtime_admitted);
        let captured =
            super::super::capture_tests::collect(job).expect("actual renewed original capture");
        let captured_job: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
        let replayed = run(&captured_job).expect("independent replay of renewed captured original");
        assert_eq!(
            serde_json::to_value(&captured.replay).unwrap(),
            serde_json::to_value(&replayed).unwrap()
        );
        assert_eq!(captured_job.runtime_code_sha256, job.runtime_code_sha256);
        assert_eq!(captured_job.parent_header_hex, job.parent_header_hex);
        assert_eq!(captured_job.child_header_hex, job.child_header_hex);
        let opening = if index == 0 {
            14
        } else {
            20 + 4 * index as u64
        };
        let earning = if index == 0 { 6 } else { 0 };
        for report in [&original, &captured.replay, &replayed] {
            assert_renewed_principal_effects(report, opening, earning);
            let records = &report.hook_observations.as_ref().unwrap().observations;
            assert_eq!(
                records
                    .iter()
                    .filter(|item| item.purpose == "native-epoch")
                    .count(),
                usize::from(index == 0)
            );
        }
    }
    // A next runtime cannot execute a parent that still proves the old code.
    let mut wrong = jobs[1].clone();
    wrong.runtime_code_hex = second.runtime_code_hex.clone();
    wrong.runtime_code_sha256 = second.runtime_code_sha256;
    wrong.runtime_code_blake2b_256 = second.runtime_code_blake2b_256;
    wrong.observation_profile = second.observation_profile.clone();
    assert!(run(&wrong).is_err());
    if let Some(directory) = std::env::var_os("URNETWORK_NATIVE_RUNTIME_RENEWAL_FIXTURE_OUT") {
        let directory = Path::new(&directory);
        assert!(directory.is_absolute() && directory.is_dir());
        for (index, job) in jobs.iter().enumerate() {
            let mut file = OpenOptions::new()
                .create_new(true)
                .write(true)
                .mode(0o600)
                .open(directory.join(format!("native-job-{}.json", index + 101)))
                .unwrap();
            file.write_all(&serde_json::to_vec(job).unwrap()).unwrap();
            file.sync_all().unwrap();
        }
        std::fs::File::open(directory).unwrap().sync_all().unwrap();
    }
}
