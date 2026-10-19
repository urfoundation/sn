//! Read opening stake through the original parent runtime and strict backend.
//! Every query has an isolated overlay, shares the finite execution budget and
//! refuses writes. Returned stake is opening stock, not newly earned income.

use super::*;
use parity_scale_codec::Compact;
use sp_core::traits::CodeExecutor;
use std::collections::BTreeMap;

pub const STAKE_API: &str = "StakeInfoRuntimeApi_get_stake_info_for_hotkey_coldkey_netuid";
pub const AVAILABILITY_API: &str = "StakeInfoRuntimeApi_get_stake_availability_for_coldkeys";

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq, PartialOrd, Ord)]
#[serde(deny_unknown_fields)]
pub struct PrincipalQuery {
    pub hotkey: [u8; 32],
    pub coldkey: [u8; 32],
    pub netuid: u16,
    #[serde(default, skip_serializing_if = "is_false")]
    pub availability: bool,
}

#[derive(Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct PrincipalObservation {
    pub query: PrincipalQuery,
    pub result_hex: String,
    pub opening_stake_alpha: Option<String>,
    pub registered: Option<bool>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub availability: Option<StakeAvailabilityObservation>,
}

// This is coldkey-wide capacity, never a per-hotkey credit or free-TAO balance.
// The runtime omits an empty/nonexistent subnet instead of reporting zero.
#[derive(Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct StakeAvailabilityObservation {
    pub result_hex: String,
    pub total_alpha: Option<String>,
    pub locked_alpha: Option<String>,
    pub available_alpha: Option<String>,
}

// Pinned v470 stake_info.rs:27; NetUid map keys are plain u16, amounts compact.
#[derive(Decode, Encode)]
struct StakeAvailability {
    total: Compact<u64>,
    locked: Compact<u64>,
    available: Compact<u64>,
}

// This exact SCALE layout has a separately reviewable source identity. The
// caller still admits that layout and runtime API in its independent policy.
#[derive(Decode, Encode)]
struct StakeInfo {
    hotkey: [u8; 32],
    coldkey: [u8; 32],
    netuid: Compact<u16>,
    stake: Compact<u64>,
    locked: Compact<u64>,
    emission: Compact<u64>,
    tao_emission: Compact<u64>,
    drain: Compact<u64>,
    registered: bool,
}

pub(super) fn validate(queries: &Option<Vec<PrincipalQuery>>) -> Result<(), ProbeError> {
    if let Some(queries) = queries {
        if queries.is_empty()
            || queries.len() > 4096
            || queries.iter().any(|query| {
                query.netuid == 0 || query.hotkey == [0; 32] || query.coldkey == [0; 32]
            })
            || queries.windows(2).any(|pair| {
                (pair[0].hotkey, pair[0].coldkey, pair[0].netuid)
                    >= (pair[1].hotkey, pair[1].coldkey, pair[1].netuid)
            })
        {
            return Err(ProbeError::new(
                "historical principal census is empty, unordered, repeated or exceeds bound",
            ));
        }
    }
    Ok(())
}

pub(super) fn observe<B, E>(
    queries: &Option<Vec<PrincipalQuery>>,
    backend: &B,
    executor: &E,
    extensions: &mut Extensions,
    runtime: &RuntimeCode,
    parent: sp_core::H256,
) -> Result<Option<Vec<PrincipalObservation>>, ProbeError>
where
    B: Backend<Blake2Hasher>,
    E: CodeExecutor + Clone + 'static,
{
    let mut overlay = OverlayedChanges::<Blake2Hasher>::default();
    observe_overlay(
        queries,
        backend,
        &mut overlay,
        executor,
        extensions,
        runtime,
        parent,
        None,
    )
}

// False is omitted so every pre-effects job keeps its original wire identity.
pub(super) fn is_false(value: &bool) -> bool {
    !*value
}

pub(super) fn validate_effects(
    enabled: bool,
    queries: &Option<Vec<PrincipalQuery>>,
) -> Result<(), ProbeError> {
    if enabled && queries.is_none() {
        return Err(ProbeError::new(
            "historical principal effects omitted their original query census",
        ));
    }
    Ok(())
}

// This borrows the exact completed Core_execute_block overlay. A fresh parent
// overlay would silently report the old stock again and is never used here.
pub(super) fn observe_execution<B, E>(
    enabled: bool,
    queries: &Option<Vec<PrincipalQuery>>,
    backend: &B,
    overlay: &mut OverlayedChanges<Blake2Hasher>,
    executor: &E,
    extensions: &mut Extensions,
    runtime: &RuntimeCode,
    child: sp_core::H256,
    state_version: StateVersion,
    expected_root: sp_core::H256,
) -> Result<Option<Vec<PrincipalObservation>>, ProbeError>
where
    B: Backend<Blake2Hasher>,
    E: CodeExecutor + Clone + 'static,
{
    validate_effects(enabled, queries)?;
    if !enabled {
        return Ok(None);
    }
    observe_overlay(
        queries,
        backend,
        overlay,
        executor,
        extensions,
        runtime,
        child,
        Some((state_version, expected_root)),
    )
}

fn observe_overlay<B, E>(
    queries: &Option<Vec<PrincipalQuery>>,
    backend: &B,
    overlay: &mut OverlayedChanges<Blake2Hasher>,
    executor: &E,
    extensions: &mut Extensions,
    runtime: &RuntimeCode,
    block: sp_core::H256,
    post_state: Option<(StateVersion, sp_core::H256)>,
) -> Result<Option<Vec<PrincipalObservation>>, ProbeError>
where
    B: Backend<Blake2Hasher>,
    E: CodeExecutor + Clone + 'static,
{
    validate(queries)?;
    let Some(queries) = queries else {
        return Ok(None);
    };
    let mut results = Vec::with_capacity(queries.len());
    for query in queries {
        let args = (query.hotkey, query.coldkey, query.netuid).encode();
        let raw = query_runtime(
            STAKE_API, &args, backend, overlay, executor, extensions, runtime, block, post_state,
        )?;
        let value: Option<StakeInfo> = scale_exact("opening stake API result", &raw)?;
        if value.as_ref().is_some_and(|value| {
            value.hotkey != query.hotkey
                || value.coldkey != query.coldkey
                || value.netuid.0 != query.netuid
        }) {
            return Err(ProbeError::new(
                "historical opening principal API substituted requested identity",
            ));
        }
        if value.encode() != raw {
            return Err(ProbeError::new(
                "historical opening principal API returned noncanonical SCALE",
            ));
        }
        let availability = if query.availability {
            // A singleton filtered request never scans another coldkey/subnet.
            let args = (vec![query.coldkey], Some(vec![query.netuid])).encode();
            let raw = query_runtime(
                AVAILABILITY_API,
                &args,
                backend,
                overlay,
                executor,
                extensions,
                runtime,
                block,
                post_state,
            )?;
            Some(decode_availability(query, &raw)?)
        } else {
            None
        };
        results.push(PrincipalObservation {
            query: query.clone(),
            result_hex: format!("0x{}", hex::encode(raw)),
            opening_stake_alpha: value.as_ref().map(|value| value.stake.0.to_string()),
            registered: value.map(|value| value.registered),
            availability,
        });
    }
    Ok(Some(results))
}

// Both APIs share original proof, completed overlay, finite budget and read-only
// hosts. Even a rolled-back write is refused, not hidden by equal final roots.
fn query_runtime<B, E>(
    api: &str,
    args: &[u8],
    backend: &B,
    overlay: &mut OverlayedChanges<Blake2Hasher>,
    executor: &E,
    extensions: &mut Extensions,
    runtime: &RuntimeCode,
    block: sp_core::H256,
    post_state: Option<(StateVersion, sp_core::H256)>,
) -> Result<Vec<u8>, ProbeError>
where
    B: Backend<Blake2Hasher>,
    E: CodeExecutor + Clone + 'static,
{
    extensions
        .get_mut(TypeId::of::<hosts::HistoricalBudget>())
        .and_then(|value| value.downcast_mut::<hosts::HistoricalBudget>())
        .ok_or_else(|| ProbeError::new("historical principal budget absent"))?
        .0
        .read_only = true;
    let execution = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        StateMachine::new(
            backend,
            &mut *overlay,
            executor,
            api,
            args,
            extensions,
            runtime,
            CallContext::Onchain,
        )
        .set_parent_hash(block)
        .execute()
    }));
    extensions
        .get_mut(TypeId::of::<hosts::HistoricalBudget>())
        .and_then(|value| value.downcast_mut::<hosts::HistoricalBudget>())
        .ok_or_else(|| ProbeError::new("historical principal budget absent"))?
        .0
        .read_only = false;
    let raw = execution
        .map_err(|_| ProbeError::new("historical principal query panicked on original proof"))?
        .map_err(|e| ProbeError::new(format!("historical principal query {api}: {e}")))?;
    if raw.len() > 256
        || post_state.is_none()
            && (overlay.changes().next().is_some() || overlay.children().next().is_some())
        || overlay.transaction_depth() != 0
        || extensions
            .get_mut(TypeId::of::<hosts::HistoricalBudget>())
            .and_then(|value| value.downcast_mut::<hosts::HistoricalBudget>())
            .is_none_or(|value| value.0.depth != 0)
    {
        return Err(ProbeError::new(
            "historical principal query changed state, left a transaction or exceeded bound",
        ));
    }
    if let Some((version, expected)) = post_state {
        let root = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            overlay.storage_root(backend, version).0
        }))
        .map_err(|_| ProbeError::new("historical closing principal post-state proof incomplete"))?;
        if root != expected {
            return Err(ProbeError::new(
                "historical closing principal query changed the completed execution state",
            ));
        }
    }
    Ok(raw)
}

fn decode_availability(
    query: &PrincipalQuery,
    raw: &[u8],
) -> Result<StakeAvailabilityObservation, ProbeError> {
    let result: BTreeMap<[u8; 32], BTreeMap<u16, StakeAvailability>> =
        scale_exact("stake availability API result", raw)?;
    let subnets = result.get(&query.coldkey).ok_or_else(|| {
        ProbeError::new("historical availability API substituted requested coldkey")
    })?;
    if result.len() != 1
        || subnets.len() > 1
        || subnets.keys().any(|netuid| *netuid != query.netuid)
    {
        return Err(ProbeError::new(
            "historical availability API changed singleton coldkey/subnet scope",
        ));
    }
    let value = subnets.get(&query.netuid);
    if value.is_some_and(|value| value.available.0 > value.total.0.saturating_sub(value.locked.0)) {
        return Err(ProbeError::new(
            "historical availability API returned impossible unstake capacity",
        ));
    }
    Ok(StakeAvailabilityObservation {
        result_hex: format!("0x{}", hex::encode(raw)),
        total_alpha: value.map(|value| value.total.0.to_string()),
        locked_alpha: value.map(|value| value.locked.0.to_string()),
        available_alpha: value.map(|value| value.available.0.to_string()),
    })
}
