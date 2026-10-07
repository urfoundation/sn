//! Real mixed native/fee execution. Independent expected state contains every
//! original event; complete body coverage is separate from selected EVM fees.

use super::*;

// Fixed synthetic original paths include an unrelated payer and a nonpaying
// body entry. Missing refund and observed zero are different program branches.
pub(super) struct Program {
    mode: String,
    events: Vec<(&'static str, Vec<u8>)>,
}

impl Program {
    pub(super) fn new(mode: &str) -> Self {
        assert!(matches!(
            mode,
            "complete" | "missing-refund" | "zero-refund" | "quiet"
        ));
        let source = [29u8; 20];
        let payer = blake2_256(&[b"evm:".as_slice(), &source].concat());
        let mut events = Vec::new();
        if mode != "quiet" {
            events.push((
                "fee-withdraw",
                fee_record(
                    0,
                    5,
                    &SyntheticBalanceEvents::Withdraw {
                        who: payer,
                        amount: 1000,
                    }
                    .encode(),
                ),
            ));
            if mode == "complete" {
                events.push((
                    "fee-refund",
                    fee_record(
                        0,
                        5,
                        &SyntheticBalanceEvents::Deposit {
                            who: payer,
                            amount: 250,
                        }
                        .encode(),
                    ),
                ));
            }
            events.push((
                "ethereum-executed",
                fee_record(
                    0,
                    18,
                    &SyntheticEthereumEvents::Executed {
                        from: source,
                        to: [31; 20],
                        transaction_hash: [41; 32],
                        exit_reason: SyntheticExitReason::Revert,
                        extra_data: vec![],
                    }
                    .encode(),
                ),
            ));
            events.push((
                "fee-withdraw",
                fee_record(
                    1,
                    5,
                    &SyntheticBalanceEvents::Withdraw {
                        who: [99; 32],
                        amount: 7,
                    }
                    .encode(),
                ),
            ));
            events.push((
                "fee-refund",
                fee_record(
                    1,
                    5,
                    &SyntheticBalanceEvents::Deposit {
                        who: [99; 32],
                        amount: 0,
                    }
                    .encode(),
                ),
            ));
        }
        Self {
            mode: mode.to_owned(),
            events,
        }
    }

    pub(super) fn declarations(&self) -> String {
        let metadata = fee_metadata().encode();
        let mut result = segment(40000, &metadata);
        result.push_str(&format!(
            "(func (export \"Metadata_metadata\") (param i32 i32) (result i64) (i64.const {}))",
            span(40000, metadata.len())
        ));
        result.push_str(&segment(7000, &key(b"System", b"ExecutionPhase", false)));
        result.push_str(&segment(7100, &[0, 2, 0, 0, 0]));
        result.push_str(&segment(7110, &[0, 0, 0, 0, 0]));
        result.push_str(&segment(7120, &[1]));
        result.push_str(&segment(7200, b"synthetic-original-fee-disposition"));
        result.push_str(&segment(7300, &[1])); // Pays::No in pinned SDK enum.
        result.push_str(&segment(7310, &0u64.to_le_bytes()));
        for (index, (_, event)) in self.events.iter().enumerate() {
            let address = 9000 + index as u32 * 256;
            result.push_str(&segment(address, event));
            result.push_str(&format!("(func $fee{index} (export \"fee{index}\") (call $append (i64.const {}) (i64.const {})))", span(1400, 32), span(address, event.len())));
        }
        // All approved paths exist even when this block does not take them.
        for export in ["unused_withdraw", "unused_refund", "unused_executed"] {
            result.push_str(&format!(
                "(func (export \"{export}\") (drop (call $get (i64.const {}))))",
                span(7200, b"synthetic-original-fee-disposition".len())
            ));
        }
        result.push_str(&format!("(func $exempt (export \"fee_exempt\") (drop (call $get (i64.const {})))) (func $zero (export \"fee_zero\") (drop (call $get (i64.const {}))))", span(7200, b"synthetic-original-fee-disposition".len()), span(7200, b"synthetic-original-fee-disposition".len())));
        result
    }

    pub(super) fn body(&self) -> String {
        let mut result = String::new();
        if self.mode != "quiet" {
            for index in 0..self.events.len() {
                result.push_str(&format!("(call $fee{index})"));
            }
            if self.mode == "zero-refund" {
                result.push_str(&format!(
                    "(call $set (i64.const {}) (i64.const {})) (call $zero)",
                    span(7000, 32),
                    span(7110, 5)
                ));
            }
            result.push_str(&format!(
                "(call $set (i64.const {}) (i64.const {})) (call $exempt)",
                span(7000, 32),
                span(7100, 5)
            ));
        }
        result.push_str(&format!(
            "(call $set (i64.const {}) (i64.const {}))",
            span(7000, 32),
            span(7120, 1)
        ));
        result
    }

    pub(super) fn expected_events(&self, original: &[u8]) -> Vec<u8> {
        let mut result = codec::Compact(1 + self.events.len() as u32).encode();
        result.extend_from_slice(original);
        for (_, event) in &self.events {
            result.extend_from_slice(event);
        }
        result
    }

    pub(super) fn extrinsics(&self) -> Vec<Vec<u8>> {
        if self.mode == "quiet" {
            Vec::new()
        } else {
            vec![
                vec![71u8].encode(),
                vec![72u8].encode(),
                vec![73u8].encode(),
            ]
        }
    }

    pub(super) fn profile(&self, code: &[u8], profile: &mut observer::ObservationProfile) {
        profile.metadata_sha256 = Some(sha2_256(&fee_metadata()));
        for (index, (purpose, _)) in self.events.iter().enumerate() {
            profile
                .rules
                .extend(observation_profile(code, &format!("fee{index}"), purpose).rules);
        }
        for (export, purpose) in [
            ("unused_withdraw", "fee-withdraw"),
            ("unused_refund", "fee-refund"),
            ("unused_executed", "ethereum-executed"),
        ] {
            profile
                .rules
                .extend(observation_profile(code, export, purpose).rules);
        }
        for (export, purpose, name, address, bytes) in [
            ("fee_exempt", "native-fee-exempt", "pays-fee", 7300, 1),
            ("fee_zero", "native-fee-refund-zero", "refund", 7310, 8),
        ] {
            let mut rule = observation_profile(code, export, purpose).rules.remove(0);
            rule.memory = vec![observer::MemoryCapture {
                name: name.to_owned(),
                address,
                global: None,
                dereference_offsets: vec![],
                bytes,
                repeat: None,
            }];
            profile.rules.push(rule);
        }
    }
}

// Exported bytes pass capture, reduced-proof replay and independently computed
// child root before any Go consumer can use them as original evidence.
#[test]
fn historical_native_whole_fee_exports_complete_and_unresolved_original_jobs() {
    let mut jobs = Vec::new();
    for mode in ["complete", "missing-refund", "zero-refund", "quiet"] {
        let (job, expected) =
            fixture_with_fee_census(false, Some(Some(14)), None, None, true, Some(mode));
        let captured =
            super::super::capture_tests::collect(&job).expect("actual mixed native fee capture");
        let exported: HistoricalJob = serde_json::from_str(&captured.job_json).unwrap();
        let replayed = run(&exported).expect("actual mixed native fee reduced replay");
        for report in [&captured.replay, &replayed] {
            assert!(report.post_state_reproduced && !report.runtime_admitted);
            let trace = report.hook_observations.as_ref().unwrap();
            assert_eq!(
                trace
                    .observations
                    .iter()
                    .filter(|record| record.purpose == "native-emission")
                    .count(),
                1,
                "original native emission was lost"
            );
            let fees = trace.fee_events.as_ref().unwrap();
            assert_eq!(
                fees.events.len(),
                if mode == "quiet" {
                    0
                } else if mode == "complete" {
                    5
                } else {
                    4
                },
                "selected original fee append census differs"
            );
            assert_eq!(fees.candidates.len(), usize::from(mode != "quiet"));
            assert_eq!(
                trace
                    .observations
                    .iter()
                    .filter(|record| record.purpose == "native-fee-exempt")
                    .count(),
                usize::from(mode != "quiet")
            );
            assert_eq!(
                trace
                    .observations
                    .iter()
                    .filter(|record| record.purpose == "native-fee-refund-zero")
                    .count(),
                usize::from(mode == "zero-refund")
            );
        }
        assert_eq!(
            expected.top.get(b"synthetic-opening-stake".as_slice()),
            Some(&words(&[20]))
        );
        jobs.push((mode, exported));
    }
    if let Some(directory) = std::env::var_os("URNETWORK_NATIVE_WHOLE_FEE_FIXTURE_OUT") {
        let directory = Path::new(&directory);
        assert!(directory.is_absolute() && directory.is_dir());
        for (name, job) in jobs {
            let mut file = OpenOptions::new()
                .create_new(true)
                .write(true)
                .mode(0o600)
                .open(directory.join(format!("whole-fee-{name}.json")))
                .expect("new private original whole-fee fixture");
            file.write_all(&serde_json::to_vec(&job).unwrap()).unwrap();
            file.sync_all().unwrap();
        }
    }
}

// Native appends are retained in the full trace, not decoded as fee variants.
#[test]
fn historical_native_whole_fee_decoder_rejects_selected_foreign_event() {
    let job = fee_job_changed(Some(250), false, false, |events, _| {
        events.push((Some("fee-refund"), fee_record(0, 7, &[250, 25, 0, 0])));
    });
    let error = run(&job).expect_err("foreign event acquired approved fee semantics");
    assert!(error.to_string().contains("event"), "{error}");
}
