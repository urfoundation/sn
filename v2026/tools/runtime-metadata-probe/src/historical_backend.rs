//! SDK root writers log incomplete-trie errors and can return the original root.
//! A proof verifier must refuse instead. SDK bulk-deletion also suppresses
//! iterator errors; trap those before partial deletion can be called complete.
//! Exact read/iterator ordering is delegated;
//! every root mutation first executes the fallible trie API over the same nodes.

use super::hosts::Work;
use parity_scale_codec::{Decode, Encode};
use sp_core::{
    storage::{ChildInfo, StateVersion},
    Blake2Hasher, H256,
};
use sp_state_machine::{
    Backend, BackendTransaction, IterArgs, StateMachineStats, StorageIterator, TrieBackend,
    UsageInfo,
};
use sp_trie::{
    child_delta_trie_root, delta_trie_root, empty_child_trie_root, LayoutV0, LayoutV1, MemoryDB,
    MerkleValue,
};
use std::sync::Arc;

type Inner = TrieBackend<MemoryDB<Blake2Hasher>, Blake2Hasher>;

/// Owned immutable parent proof; speculative writes are never committed here.
#[derive(Debug)]
pub(super) struct StrictBackend {
    pub inner: Inner,
    pub work: Arc<Work>,
}

/// Preserve the iterator's missing-node errors while changing its owner type.
pub(super) struct StrictIterator {
    inner: <Inner as Backend<Blake2Hasher>>::RawIter,
}

impl StorageIterator<Blake2Hasher> for StrictIterator {
    type Backend = StrictBackend;
    type Error = String;
    fn next_key(&mut self, backend: &Self::Backend) -> Option<Result<Vec<u8>, String>> {
        backend.work.charge(0);
        self.inner.next_key(&backend.inner).map(|result| {
            let key = result.expect("historical iterator proof incomplete");
            assert!(key.len() <= 512, "historical iterator key bound");
            backend.work.charge(key.len());
            Ok(key)
        })
    }
    fn next_pair(&mut self, backend: &Self::Backend) -> Option<Result<(Vec<u8>, Vec<u8>), String>> {
        backend.work.charge(0);
        self.inner.next_pair(&backend.inner).map(|result| {
            let (key, value) = result.expect("historical iterator proof incomplete");
            assert!(
                key.len() <= 512 && value.len() <= 8 * 1024 * 1024,
                "historical iterator value bound"
            );
            backend.work.charge(key.len() + value.len());
            Ok((key, value))
        })
    }
    fn was_complete(&self) -> bool {
        self.inner.was_complete()
    }
}

impl Backend<Blake2Hasher> for StrictBackend {
    type Error = String;
    type TrieBackendStorage = MemoryDB<Blake2Hasher>;
    type RawIter = StrictIterator;

    fn storage(&self, key: &[u8]) -> Result<Option<Vec<u8>>, String> {
        self.inner.storage(key)
    }
    fn storage_hash(&self, key: &[u8]) -> Result<Option<H256>, String> {
        self.inner.storage_hash(key)
    }
    fn closest_merkle_value(&self, key: &[u8]) -> Result<Option<MerkleValue<H256>>, String> {
        self.inner.closest_merkle_value(key)
    }
    fn child_closest_merkle_value(
        &self,
        child: &ChildInfo,
        key: &[u8],
    ) -> Result<Option<MerkleValue<H256>>, String> {
        self.inner.child_closest_merkle_value(child, key)
    }
    fn child_storage(&self, child: &ChildInfo, key: &[u8]) -> Result<Option<Vec<u8>>, String> {
        self.inner.child_storage(child, key)
    }
    fn child_storage_hash(&self, child: &ChildInfo, key: &[u8]) -> Result<Option<H256>, String> {
        self.inner.child_storage_hash(child, key)
    }
    fn next_storage_key(&self, key: &[u8]) -> Result<Option<Vec<u8>>, String> {
        self.inner.next_storage_key(key)
    }
    fn next_child_storage_key(
        &self,
        child: &ChildInfo,
        key: &[u8],
    ) -> Result<Option<Vec<u8>>, String> {
        self.inner.next_child_storage_key(child, key)
    }
    fn raw_iter(&self, mut args: IterArgs) -> Result<Self::RawIter, String> {
        self.work.charge(0);
        args.stop_on_incomplete_database = false;
        Ok(StrictIterator {
            inner: self
                .inner
                .raw_iter(args)
                .expect("historical iterator proof incomplete"),
        })
    }
    fn register_overlay_stats(&self, stats: &StateMachineStats) {
        self.inner.register_overlay_stats(stats)
    }
    fn usage_info(&self) -> UsageInfo {
        self.inner.usage_info()
    }

    fn storage_root<'a>(
        &self,
        delta: impl Iterator<Item = (&'a [u8], Option<&'a [u8]>)>,
        version: StateVersion,
    ) -> (H256, BackendTransaction<Blake2Hasher>) {
        let values: Vec<_> = delta.collect();
        let mut db = self.inner.backend_storage().clone();
        let checked = match version {
            StateVersion::V0 => delta_trie_root::<LayoutV0<Blake2Hasher>, _, _, _, _, _>(
                &mut db,
                *self.inner.root(),
                values.iter().copied(),
                None,
                None,
            ),
            StateVersion::V1 => delta_trie_root::<LayoutV1<Blake2Hasher>, _, _, _, _, _>(
                &mut db,
                *self.inner.root(),
                values.iter().copied(),
                None,
                None,
            ),
        }
        .expect("historical top-level write proof incomplete");
        let result = self.inner.storage_root(values.into_iter(), version);
        assert_eq!(
            result.0, checked,
            "historical top-level root writer differs"
        );
        result
    }

    fn child_storage_root<'a>(
        &self,
        child: &ChildInfo,
        delta: impl Iterator<Item = (&'a [u8], Option<&'a [u8]>)>,
        version: StateVersion,
    ) -> (H256, bool, BackendTransaction<Blake2Hasher>) {
        let empty = empty_child_trie_root::<LayoutV1<Blake2Hasher>>();
        let original = self
            .inner
            .storage(child.prefixed_storage_key().as_slice())
            .expect("historical child root proof incomplete")
            .map(|raw| {
                let root =
                    H256::decode(&mut raw.as_slice()).expect("historical child root malformed");
                assert_eq!(raw, root.encode(), "historical child root trailing bytes");
                root
            })
            .unwrap_or(empty);
        let values: Vec<_> = delta.collect();
        let mut db = self.inner.backend_storage().clone();
        let checked = match version {
            StateVersion::V0 => child_delta_trie_root::<LayoutV0<Blake2Hasher>, _, _, _, _, _, _>(
                child.keyspace(),
                &mut db,
                original,
                values.iter().copied(),
                None,
                None,
            ),
            StateVersion::V1 => child_delta_trie_root::<LayoutV1<Blake2Hasher>, _, _, _, _, _, _>(
                child.keyspace(),
                &mut db,
                original,
                values.iter().copied(),
                None,
                None,
            ),
        }
        .expect("historical child write proof incomplete");
        let result = self
            .inner
            .child_storage_root(child, values.into_iter(), version);
        assert_eq!(result.0, checked, "historical child root writer differs");
        assert_eq!(
            result.1,
            checked == empty,
            "historical child empty root differs"
        );
        result
    }
}
