//! An original metadata pin is independent of selecting fee event callsites.
use super::{fee_events::EventLayout, observer::ObservationProfile, scale_exact};
use crate::ProbeError;
use frame_metadata::{META_RESERVED, RuntimeMetadata, RuntimeMetadataPrefixed};
use sp_core::hashing::sha2_256;

const MAXIMUM_METADATA: usize = 2 * 1024 * 1024;

pub(super) fn event_layout(
    profile: &ObservationProfile,
    raw: &[u8],
) -> Result<Option<EventLayout>, ProbeError> {
    let expected = profile
        .metadata_sha256
        .ok_or_else(|| ProbeError::new("original metadata scope lacks its pin"))?;
    if raw.len() > MAXIMUM_METADATA || expected == [0; 32] || sha2_256(raw) != expected {
        return Err(ProbeError::new(
            "runtime-generated metadata pin or bound differs",
        ));
    }
    let metadata: RuntimeMetadataPrefixed = scale_exact("generated original metadata", raw)?;
    if metadata.0 != META_RESERVED
        || !matches!(
            metadata.1,
            RuntimeMetadata::V14(_) | RuntimeMetadata::V15(_)
        )
    {
        return Err(ProbeError::new(
            "original metadata prefix or version differs",
        ));
    }
    if profile.rules.iter().any(|rule| {
        matches!(
            rule.purpose.as_str(),
            "fee-withdraw" | "fee-refund" | "ethereum-executed"
        )
    }) {
        EventLayout::from_generated(raw, expected).map(Some)
    } else {
        Ok(None)
    }
}
