BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL statement_timeout = '30s';
SELECT row_to_json(evidence) FROM (
 SELECT i.intent_id, i.intent_key, i.logical_key, i.generation,
 i.profile, i.deployment_id, i.deployment_key, i.chain_id, i.genesis_hash,
 i.from_address, i.to_address, i.nonce, i.status AS intent_status,
 i.current_tx_hash, i.attempt_count,
 i.create_time AS intent_created_at, i.update_time AS intent_updated_at,
 a.attempt, a.kind, a.tx_hash, a.status AS attempt_status,
 COALESCE(octet_length(a.raw_transaction),0) AS signed_byte_count,
 encode(sha256(a.raw_transaction),'hex') AS raw_transaction_sha256,
 a.gas_limit,a.gas_price,a.gas_tip_cap,a.gas_fee_cap,
 a.inclusion_block,a.inclusion_hash,a.finalized_block,a.finalized_hash,
 a.create_time AS attempt_created_at,a.update_time AS attempt_updated_at
 FROM st_transaction_intent i LEFT JOIN st_transaction_attempt a ON a.intent_id=i.intent_id
 ORDER BY i.chain_id,i.genesis_hash,i.from_address,i.nonce,i.generation,a.attempt
) evidence;
COMMIT;
