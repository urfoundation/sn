//! Explicit original-state joins stay separate from Wasm memory and original
//! runtime get calls. Only a bounded selected drain may sample this key pair.
use super::observer::ObservationProfile;
use crate::ProbeError;
use std::collections::BTreeSet;

const MODE: &str = "single-mechanism-original-keys-v1";

pub(super) fn state_keys(profile: &ObservationProfile) -> Result<Vec<Vec<u8>>, ProbeError> {
    let fail = || ProbeError::new("observer epoch layout or state-read scope differs");
    if profile.rules.len() > 32
        || profile.epoch_layout.as_deref().is_some_and(|mode| {
            mode != MODE || profile.schema != "urnetwork-original-wasm-native-observation-v2"
        })
    {
        return Err(fail());
    }
    let mut keys = BTreeSet::new();
    for rule in &profile.rules {
        if matches!(
            rule.purpose.as_str(),
            "native-uid-census" | "native-epoch-index"
        ) && (profile.epoch_layout.is_none() || !rule.memory.is_empty())
        {
            return Err(fail());
        }
        if profile.epoch_layout.is_some() && rule.purpose == "native-epoch" {
            if rule.host_snapshot.is_none()
                || rule.memory.iter().any(|capture| {
                    matches!(
                        capture.name.as_str(),
                        "total-alpha" | "hotkeys" | "uids" | "subnet-epoch"
                    )
                })
            {
                return Err(fail());
            }
        }
        if !rule.state_reads.is_empty() {
            if profile.epoch_layout.is_none()
                || rule.purpose != "native-drain"
                || rule.state_reads.len() != 2
                || rule.state_reads[0] == rule.state_reads[1]
            {
                return Err(fail());
            }
            for value in &rule.state_reads {
                if value.len() < 4 || value.len() > 130 || !value.starts_with("0x") {
                    return Err(fail());
                }
                let key = hex::decode(&value[2..]).map_err(|_| fail())?;
                if format!("0x{}", hex::encode(&key)) != *value {
                    return Err(fail());
                }
                keys.insert(key);
                if keys.len() > 2 {
                    return Err(fail());
                }
            }
        }
    }
    Ok(keys.into_iter().collect())
}
