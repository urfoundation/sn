BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL statement_timeout='30s';
SELECT row_to_json(q) FROM (SELECT i.intent_id,i.generation,i.profile,i.deployment_id,i.deployment_key,i.chain_id,i.genesis_hash,i.from_address,i.to_address,i.nonce,a.attempt,a.kind,a.tx_hash,a.gas_limit,a.gas_price,a.gas_tip_cap,a.gas_fee_cap,encode(a.raw_transaction,'hex') AS private_raw_transaction_hex FROM st_transaction_intent i JOIN st_transaction_attempt a ON a.intent_id=i.intent_id WHERE a.tx_hash IN ('0xfa08bd40792856914fe333e534ac3fdde5f7d7916db6c07747993fbf1b1b0bb9','0x22b58e43ca511467a7f93543a588728f6d40e7d826d8eea32146ec4825d45c37','0x79b329e62d369064b46c56cb8b13c2853ccab16deccefd52064bcb7ecf306939','0xa316152e3aba405dfb5762337ca94f610ce27d9d9cb01b282f99ae1adf877d20','0xe090676b83cb4320e0016f333f2a210aafb1ea979e1555e310b1fc1b122ef075','0xf0ec2de8004b85ab20b3745287f05030d4bd4d070c9a34141b9c1c6a26b5db91') ORDER BY i.nonce,a.attempt) q;
COMMIT;
