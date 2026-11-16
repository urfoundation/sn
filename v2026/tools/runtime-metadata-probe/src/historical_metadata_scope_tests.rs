use super::*;

fn native_metadata_job(remove_fee_pallets: bool) -> HistoricalJob {
    let mut job = fee_job_changed(Some(250), false, false, |_, raw| {
        if remove_fee_pallets {
            let mut metadata: frame_metadata::RuntimeMetadataPrefixed =
                scale_exact("fixture metadata", raw).unwrap();
            let frame_metadata::RuntimeMetadata::V14(ref mut value) = metadata.1 else {
                panic!("fixture version")
            };
            value.pallets.clear();
            *raw = metadata.encode();
        }
    });
    let profile = job.observation_profile.as_mut().unwrap();
    profile.schema = "urnetwork-original-wasm-native-observation-v2".to_owned();
    for rule in &mut profile.rules {
        rule.purpose = "native-emission".to_owned();
    }
    job
}

#[test]
fn historical_native_metadata_pin_does_not_request_fee_layout() {
    for remove_fee_pallets in [false, true] {
        let job = native_metadata_job(remove_fee_pallets);
        let report = run(&job).expect("native metadata pin required unrelated fee pallets");
        assert!(report.post_state_reproduced && !report.runtime_admitted);
        assert_eq!(report.native_fee_debit, None);
        let trace = report.hook_observations.unwrap();
        assert!(trace.fee_events.is_none());
        assert_eq!(trace.observations.len(), 3);
        assert!(
            trace
                .observations
                .iter()
                .all(|record| record.purpose == "native-emission")
        );
    }
}

#[test]
fn historical_native_metadata_scope_rejects_substituted_pin_and_fee_layout() {
    let mut changed_pin = native_metadata_job(true);
    changed_pin
        .observation_profile
        .as_mut()
        .unwrap()
        .metadata_sha256
        .as_mut()
        .unwrap()[0] ^= 1;
    let error = run(&changed_pin).expect_err("native metadata pin was ignored");
    assert!(error.to_string().contains("runtime-generated metadata pin"));

    let mut fee_scope = native_metadata_job(true);
    fee_scope.observation_profile.as_mut().unwrap().rules[0].purpose = "fee-withdraw".to_owned();
    let error = run(&fee_scope).expect_err("selected fee callsite borrowed a non-fee layout");
    assert!(error.to_string().contains("pallet absent"), "{error}");
}

#[test]
fn historical_native_metadata_nil_wire_keeps_original_observation_scope() {
    let mut job = native_metadata_job(true);
    job.observation_profile.as_mut().unwrap().metadata_sha256 = None;
    let raw = serde_json::to_string(job.observation_profile.as_ref().unwrap()).unwrap();
    assert!(!raw.contains("metadata_sha256"));
    let report = run(&job).expect("legacy nil metadata began requiring metadata or fee scope");
    assert!(report.post_state_reproduced && !report.runtime_admitted);
    assert!(report.hook_observations.unwrap().fee_events.is_none());
}
