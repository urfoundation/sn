//! Retain the exact top/child operation while the SDK requests raw trie nodes.
//! The hash-db prefix alone does not identify a child. Scope guards release
//! their lock before reads/refills, and errors remain sticky across SDK root
//! methods that otherwise log a failure and return an old/default root.

use sp_core::{
    storage::{ChildInfo, StateVersion},
    Blake2Hasher, H256,
};
use sp_state_machine::{
    Backend, BackendTransaction, IterArgs, StateMachineStats, StorageIterator, UsageInfo,
};
use sp_trie::MerkleValue;
use std::{fmt, marker::PhantomData, sync::Mutex, thread::ThreadId};

#[derive(Clone, Debug)]
pub(super) struct Operation {
    pub method: &'static str,
    pub child: Option<ChildInfo>,
}

#[derive(Default)]
struct ScopeState {
    active: Vec<Operation>,
    owner: Option<ThreadId>,
    failure: Option<String>,
}

#[derive(Default)]
pub(super) struct Scopes(Mutex<ScopeState>);

impl Scopes {
    pub fn check(&self) -> Result<(), String> {
        let state = self
            .0
            .lock()
            .map_err(|_| "capture operation scope poisoned")?;
        match &state.failure {
            Some(error) => Err(error.clone()),
            None => Ok(()),
        }
    }

    pub fn fail(&self, error: String) {
        if let Ok(mut state) = self.0.lock() {
            state.failure.get_or_insert(error);
        }
    }

    pub fn current(&self) -> Result<Operation, String> {
        self.check()?;
        let state = self
            .0
            .lock()
            .map_err(|_| "capture operation scope poisoned")?;
        if state.owner != Some(std::thread::current().id()) {
            return Err("capture node read has no matching operation owner".to_owned());
        }
        state
            .active
            .last()
            .cloned()
            .ok_or_else(|| "capture node read has no operation scope".to_owned())
    }

    fn enter(&self, operation: Operation) -> Result<ScopeGuard<'_>, String> {
        self.check()?;
        let mut state = self
            .0
            .lock()
            .map_err(|_| "capture operation scope poisoned")?;
        let owner = std::thread::current().id();
        if state.active.len() >= 8 || state.owner.is_some_and(|current| current != owner) {
            let error = "capture operation scope is concurrent or too deep".to_owned();
            state.failure.get_or_insert(error.clone());
            return Err(error);
        }
        if operation
            .child
            .as_ref()
            .is_some_and(|child| child.prefixed_storage_key().as_slice().len() > 1024)
        {
            let error = "capture child scope exceeds its original key bound".to_owned();
            state.failure.get_or_insert(error.clone());
            return Err(error);
        }
        state.owner = Some(owner);
        state.active.push(operation);
        Ok(ScopeGuard {
            scopes: self,
            depth: state.active.len(),
        })
    }
}

struct ScopeGuard<'a> {
    scopes: &'a Scopes,
    depth: usize,
}

impl Drop for ScopeGuard<'_> {
    fn drop(&mut self) {
        if let Ok(mut state) = self.scopes.0.lock() {
            if state.active.len() != self.depth || state.owner != Some(std::thread::current().id())
            {
                state.failure.get_or_insert_with(|| {
                    "capture operation scope exited out of order".to_owned()
                });
                return;
            }
            state.active.pop();
            if state.active.is_empty() {
                state.owner = None;
            }
        }
    }
}

pub(super) struct ScopedBackend<'a, B> {
    pub inner: B,
    pub scopes: &'a Scopes,
}

impl<B> fmt::Debug for ScopedBackend<'_, B> {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("ScopedHistoricalBackend")
    }
}

impl<B: Backend<Blake2Hasher, Error = String>> ScopedBackend<'_, B> {
    fn enter(
        &self,
        method: &'static str,
        child: Option<&ChildInfo>,
    ) -> Result<ScopeGuard<'_>, String> {
        if let Some(child) = child {
            // The child root lives in the top trie. Complete this read before
            // child keyspace is installed; later cached root lookups stay local.
            let _guard = self.scopes.enter(Operation {
                method: "child-root",
                child: None,
            })?;
            self.inner
                .storage(child.prefixed_storage_key().as_slice())?;
        }
        self.scopes.enter(Operation {
            method,
            child: child.cloned(),
        })
    }
}

pub(super) struct ScopedIterator<'a, B: Backend<Blake2Hasher, Error = String>> {
    inner: B::RawIter,
    child: Option<ChildInfo>,
    marker: PhantomData<&'a B>,
}

impl<'a, B: Backend<Blake2Hasher, Error = String>> StorageIterator<Blake2Hasher>
    for ScopedIterator<'a, B>
{
    type Backend = ScopedBackend<'a, B>;
    type Error = String;
    fn next_key(&mut self, backend: &Self::Backend) -> Option<Result<Vec<u8>, String>> {
        let _guard = match backend.enter("iterator-key", self.child.as_ref()) {
            Ok(guard) => guard,
            Err(error) => {
                backend.scopes.fail(error.clone());
                return Some(Err(error));
            }
        };
        let result = self.inner.next_key(&backend.inner);
        if let Some(Err(error)) = &result {
            backend.scopes.fail(error.clone());
        }
        result
    }
    fn next_pair(&mut self, backend: &Self::Backend) -> Option<Result<(Vec<u8>, Vec<u8>), String>> {
        let _guard = match backend.enter("iterator-pair", self.child.as_ref()) {
            Ok(guard) => guard,
            Err(error) => {
                backend.scopes.fail(error.clone());
                return Some(Err(error));
            }
        };
        let result = self.inner.next_pair(&backend.inner);
        if let Some(Err(error)) = &result {
            backend.scopes.fail(error.clone());
        }
        result
    }
    fn was_complete(&self) -> bool {
        self.inner.was_complete()
    }
}

// The associated iterator carries a borrow of B for the operation scope's
// lifetime. B may itself borrow the retained parent and cancellation state.
impl<'a, B: Backend<Blake2Hasher, Error = String> + 'a> Backend<Blake2Hasher>
    for ScopedBackend<'a, B>
{
    type Error = String;
    type TrieBackendStorage = B::TrieBackendStorage;
    type RawIter = ScopedIterator<'a, B>;

    fn storage(&self, key: &[u8]) -> Result<Option<Vec<u8>>, String> {
        let _guard = self.enter("storage", None)?;
        self.inner.storage(key)
    }
    fn storage_hash(&self, key: &[u8]) -> Result<Option<H256>, String> {
        let _guard = self.enter("storage-hash", None)?;
        self.inner.storage_hash(key)
    }
    fn closest_merkle_value(&self, key: &[u8]) -> Result<Option<MerkleValue<H256>>, String> {
        let _guard = self.enter("closest-merkle", None)?;
        self.inner.closest_merkle_value(key)
    }
    fn child_closest_merkle_value(
        &self,
        child: &ChildInfo,
        key: &[u8],
    ) -> Result<Option<MerkleValue<H256>>, String> {
        let _guard = self.enter("child-closest-merkle", Some(child))?;
        self.inner.child_closest_merkle_value(child, key)
    }
    fn child_storage(&self, child: &ChildInfo, key: &[u8]) -> Result<Option<Vec<u8>>, String> {
        let _guard = self.enter("child-storage", Some(child))?;
        self.inner.child_storage(child, key)
    }
    fn child_storage_hash(&self, child: &ChildInfo, key: &[u8]) -> Result<Option<H256>, String> {
        let _guard = self.enter("child-storage-hash", Some(child))?;
        self.inner.child_storage_hash(child, key)
    }
    fn next_storage_key(&self, key: &[u8]) -> Result<Option<Vec<u8>>, String> {
        let _guard = self.enter("next-key", None)?;
        self.inner.next_storage_key(key)
    }
    fn next_child_storage_key(
        &self,
        child: &ChildInfo,
        key: &[u8],
    ) -> Result<Option<Vec<u8>>, String> {
        let _guard = self.enter("child-next-key", Some(child))?;
        self.inner.next_child_storage_key(child, key)
    }
    fn raw_iter(&self, mut args: IterArgs) -> Result<Self::RawIter, String> {
        let child = args.child_info.clone();
        let _guard = self.enter("iterator-open", child.as_ref())?;
        args.stop_on_incomplete_database = false;
        Ok(ScopedIterator {
            inner: self.inner.raw_iter(args)?,
            child,
            marker: PhantomData,
        })
    }
    fn storage_root<'b>(
        &self,
        delta: impl Iterator<Item = (&'b [u8], Option<&'b [u8]>)>,
        version: StateVersion,
    ) -> (H256, BackendTransaction<Blake2Hasher>) {
        let _guard = self.enter("storage-root", None).unwrap_or_else(|error| {
            self.scopes.fail(error.clone());
            panic!("{error}")
        });
        let result = self.inner.storage_root(delta, version);
        self.scopes
            .check()
            .unwrap_or_else(|error| panic!("{error}"));
        result
    }
    fn child_storage_root<'b>(
        &self,
        child: &ChildInfo,
        delta: impl Iterator<Item = (&'b [u8], Option<&'b [u8]>)>,
        version: StateVersion,
    ) -> (H256, bool, BackendTransaction<Blake2Hasher>) {
        let _guard = self
            .enter("child-storage-root", Some(child))
            .unwrap_or_else(|error| {
                self.scopes.fail(error.clone());
                panic!("{error}")
            });
        let result = self.inner.child_storage_root(child, delta, version);
        self.scopes
            .check()
            .unwrap_or_else(|error| panic!("{error}"));
        result
    }
    fn register_overlay_stats(&self, stats: &StateMachineStats) {
        self.inner.register_overlay_stats(stats)
    }
    fn usage_info(&self) -> UsageInfo {
        self.inner.usage_info()
    }
    fn proof_size(&self) -> Option<u32> {
        self.inner.proof_size()
    }
}

#[cfg(test)]
#[path = "historical_capture_scope_tests.rs"]
mod tests;
