//! Recipient storage joins use exact original call paths. The only dynamic
//! observer key is Owner of the original captured hotkey, never arbitrary I/O.
use super::observer::{MemoryObservation, ObservationProfile};
use crate::ProbeError;

const MODE: &str = "original-storage-credit-pairs-v1";

pub(super) fn validate(profile: &ObservationProfile) -> Result<(), ProbeError> {
    let fail = || ProbeError::new("observer recipient layout or Owner read scope differs");
    if profile.recipient_layout.as_deref().is_some_and(|mode| {
        mode != MODE || profile.schema != "urnetwork-original-wasm-native-observation-v2"
    }) {
        return Err(fail());
    }
    for rule in &profile.rules {
        let context = matches!(
            rule.purpose.as_str(),
            "native-recipient-owner-hotkey"
                | "native-recipient-auto-stake"
                | "native-recipient-owner"
        );
        let component = matches!(
            rule.purpose.as_str(),
            "native-miner-capture" | "native-miner-credit" | "native-owner-recycle"
        );
        if (context || rule.purpose == "native-miner-capture" || rule.recipient_owner)
            && profile.recipient_layout.is_none()
        {
            return Err(fail());
        }
        if profile.recipient_layout.is_some() && (context || component) {
            let Some(call) = &rule.storage_call else {
                return Err(fail());
            };
            if rule.host_snapshot.is_some()
                || !rule.state_reads.is_empty()
                || (context && call.operation != "get")
                || (component && !matches!(call.operation.as_str(), "get" | "set"))
                || rule.recipient_owner != (rule.purpose == "native-owner-recycle")
            {
                return Err(fail());
            }
        }
        if profile.recipient_layout.is_some()
            && (context || component)
            && rule.memory.iter().any(|capture| {
                matches!(
                    capture.name.as_str(),
                    "captured" | "subnet-owner-hotkey" | "auto-stake-destination"
                ) || rule.purpose == "native-owner-recycle" && capture.name == "coldkey"
            })
        {
            return Err(fail());
        }
        if rule.recipient_owner {
            if rule.purpose != "native-owner-recycle"
                || !rule.memory.iter().any(|capture| {
                    capture.name == "hotkey" && capture.bytes == 32 && capture.repeat.is_none()
                })
            {
                return Err(fail());
            }
        }
    }
    Ok(())
}

pub(super) fn owner_key(memory: &[MemoryObservation]) -> Result<Vec<u8>, ProbeError> {
    let value = memory
        .iter()
        .find(|value| value.name == "hotkey")
        .ok_or_else(|| ProbeError::new("observer Owner hotkey capture absent"))?;
    if value.bytes_hex.len() != 66
        || !value.bytes_hex.starts_with("0x")
        || value.element_count.is_some()
    {
        return Err(ProbeError::new("observer Owner hotkey width differs"));
    }
    let hotkey = hex::decode(&value.bytes_hex[2..])
        .map_err(|_| ProbeError::new("observer Owner hotkey encoding differs"))?;
    Ok([
        sp_core::hashing::twox_128(b"SubtensorModule").as_slice(),
        sp_core::hashing::twox_128(b"Owner").as_slice(),
        sp_core::hashing::blake2_128(&hotkey).as_slice(),
        hotkey.as_slice(),
    ]
    .concat())
}
