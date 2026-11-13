//! One synchronous, bounded request/ack channel owned by the Go supervisor.
//! The broker publishes hash-checked nodes before acknowledging. The SDK then
//! resumes the same read from the original parent root; RPC never supplies an
//! authoritative value, iterator end or replacement state root.

use super::{scope, H256, MAXIMUM_NATIVE_PROOF_NODES};
use serde::{Deserialize, Serialize};
use sp_core::hashing::sha2_256;
use std::{
    fs::File,
    io::{Read, Write},
    sync::Mutex,
};

const SCHEMA: &str = "urnetwork-native-parent-trie-refill-v1";
const MAXIMUM_FRAME: usize = 16 * 1024;

#[derive(Serialize)]
struct Request {
    schema: &'static str,
    id: u64,
    request_sha256: String,
    parent_hash: String,
    parent_state_root: String,
    operation: &'static str,
    child_storage_key: String,
    prefix_hex: String,
    prefix_nibble: Option<u8>,
    missing_hash: String,
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Response {
    schema: String,
    id: u64,
    request_sha256: String,
    missing_hash: String,
    retained: bool,
}

struct Pipes<'a> {
    request: &'a File,
    response: &'a File,
    next: u64,
}

pub(super) struct Refill<'a> {
    pipes: Mutex<Pipes<'a>>,
    scopes: &'a scope::Scopes,
    request_sha256: String,
    parent: H256,
    root: H256,
}

impl<'a> Refill<'a> {
    pub fn new(
        raw: &[u8],
        parent: H256,
        root: H256,
        scopes: &'a scope::Scopes,
        request: &'a File,
        response: &'a File,
    ) -> Self {
        Self {
            pipes: Mutex::new(Pipes {
                request,
                response,
                next: 1,
            }),
            scopes,
            request_sha256: format!("sha256:{}", hex::encode(sha2_256(raw))),
            parent,
            root,
        }
    }

    pub fn get(&self, hash: &H256, prefix: (&[u8], Option<u8>)) -> Result<(), String> {
        let outcome = self.refill(hash, prefix);
        if let Err(error) = &outcome {
            self.scopes.fail(error.clone());
        }
        outcome
    }

    fn refill(&self, hash: &H256, prefix: (&[u8], Option<u8>)) -> Result<(), String> {
        let scope = self.scopes.current()?;
        let mut bytes = prefix.0;
        let child = if let Some(child) = &scope.child {
            // KeySpacedDB prepends this exact child keyspace to the hash-db
            // prefix. Never infer a split from an arbitrary byte sequence.
            bytes = bytes
                .strip_prefix(child.keyspace())
                .ok_or("capture child prefix differs from active child scope")?;
            format!("0x{}", hex::encode(child.prefixed_storage_key().as_slice()))
        } else {
            "0x".to_owned()
        };
        if bytes.len() > 1024
            || prefix.1.is_some_and(|nibble| nibble & 0x0f != 0)
            || bytes.len() == 1024 && prefix.1.is_some()
        {
            return Err("capture refill prefix exceeds canonical key profile".to_owned());
        }
        let mut pipes = self
            .pipes
            .lock()
            .map_err(|_| "capture refill pipes poisoned")?;
        if pipes.next > 2 * MAXIMUM_NATIVE_PROOF_NODES as u64 {
            return Err("capture refill request count exhausted".to_owned());
        }
        let request = Request {
            schema: SCHEMA,
            id: pipes.next,
            request_sha256: self.request_sha256.clone(),
            parent_hash: format!("0x{}", hex::encode(self.parent.0)),
            parent_state_root: format!("0x{}", hex::encode(self.root.0)),
            operation: scope.method,
            child_storage_key: child,
            prefix_hex: format!("0x{}", hex::encode(bytes)),
            prefix_nibble: prefix.1,
            missing_hash: format!("0x{}", hex::encode(hash.0)),
        };
        let raw = serde_json::to_vec(&request)
            .map_err(|e| format!("capture refill request encode: {e}"))?;
        if raw.len() > MAXIMUM_FRAME {
            return Err("capture refill request frame exceeded".to_owned());
        }
        pipes
            .request
            .write_all(&(raw.len() as u32).to_be_bytes())
            .and_then(|_| pipes.request.write_all(&raw))
            .map_err(|e| format!("capture refill request write: {e}"))?;
        let mut size = [0_u8; 4];
        pipes
            .response
            .read_exact(&mut size)
            .map_err(|e| format!("capture refill response length: {e}"))?;
        let size = u32::from_be_bytes(size) as usize;
        if size == 0 || size > MAXIMUM_FRAME {
            return Err("capture refill response frame exceeded".to_owned());
        }
        let mut raw = vec![0; size];
        pipes
            .response
            .read_exact(&mut raw)
            .map_err(|e| format!("capture refill response read: {e}"))?;
        let response: Response = serde_json::from_slice(&raw)
            .map_err(|e| format!("capture refill response decode: {e}"))?;
        if response.schema != SCHEMA
            || response.id != request.id
            || response.request_sha256 != request.request_sha256
            || response.missing_hash != request.missing_hash
            || !response.retained
        {
            return Err("capture refill acknowledgement differs from exact request".to_owned());
        }
        pipes.next += 1;
        Ok(())
    }
}
