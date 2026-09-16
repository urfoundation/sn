BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL statement_timeout = '30s';
SELECT row_to_json(scope)
FROM (
  SELECT profile, deployment_id, deployment_key, chain_id, count(*) AS intent_count
  FROM st_transaction_intent
  WHERE chain_id = 945
    AND deployment_key = '945:0x8e7d2f9a77fec95c7e4875b0bd858d5de2b6def8'
  GROUP BY profile, deployment_id, deployment_key, chain_id
) scope;
WITH selected AS (
  SELECT i.* FROM st_transaction_intent i
  WHERE i.profile = 'testnet'
    AND i.deployment_id = 'ur-subnet-testnet-v1'
    AND i.chain_id = 945
    AND i.deployment_key = '945:0x8e7d2f9a77fec95c7e4875b0bd858d5de2b6def8'
    AND (
      i.create_time >= TIMESTAMPTZ '2026-09-16 05:47:35+00'
      OR i.update_time >= TIMESTAMPTZ '2026-09-16 05:47:35+00'
      OR i.status NOT IN ('finalized','reverted','canceled','superseded')
      OR EXISTS (
        SELECT 1 FROM st_transaction_attempt x WHERE x.intent_id = i.intent_id
          AND (x.create_time >= TIMESTAMPTZ '2026-09-16 05:47:35+00'
            OR x.update_time >= TIMESTAMPTZ '2026-09-16 05:47:35+00'
            OR (NULLIF(x.tx_hash,'') IS NOT NULL AND (i.status IN ('failed','canceled') OR x.status IN ('failed','canceled'))))
      )
    )
)
SELECT row_to_json(evidence)
FROM (
  SELECT i.intent_id, i.intent_key, i.logical_key, i.generation,
         i.profile, i.deployment_id, i.deployment_key, i.chain_id, i.genesis_hash,
         i.from_address, i.to_address, i.nonce, i.status AS intent_status,
         i.current_tx_hash, i.attempt_count,
         i.create_time AS intent_created_at, i.update_time AS intent_updated_at,
         a.attempt, a.kind, a.tx_hash, a.status AS attempt_status,
         COALESCE(octet_length(a.raw_transaction),0) AS signed_byte_count,
         a.gas_limit, a.gas_price, a.gas_tip_cap, a.gas_fee_cap,
         a.inclusion_block, a.inclusion_hash, a.finalized_block, a.finalized_hash,
         a.create_time AS attempt_created_at, a.update_time AS attempt_updated_at
  FROM selected i LEFT JOIN st_transaction_attempt a ON a.intent_id = i.intent_id
  ORDER BY i.from_address, i.nonce, i.generation, a.attempt
) evidence;
COMMIT;
