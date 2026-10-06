//! Real SDK iterators keep the caller's borrowed storage while operation
//! guards end before the next top/child request. No static backend is needed.

use super::*;
use sp_core::storage::{Storage, StorageChild};
use sp_state_machine::{InMemoryBackend, TrieBackendBuilder, TrieBackendStorage};

struct BorrowedStorage<'a, S> {
    inner: &'a S,
    scopes: &'a Scopes,
    observed: &'a Mutex<Vec<Operation>>,
}

impl<S: TrieBackendStorage<Blake2Hasher>> TrieBackendStorage<Blake2Hasher>
    for BorrowedStorage<'_, S>
{
    fn get(&self, key: &H256, prefix: (&[u8], Option<u8>)) -> Result<Option<Vec<u8>>, String> {
        let operation = self.scopes.current()?;
        self.observed
            .lock()
            .map_err(|_| "borrowed fixture observations poisoned")?
            .push(operation);
        self.inner.get(key, prefix)
    }
}

#[test]
fn scoped_iterators_read_borrowed_top_and_child_backends_without_retaining_scope() {
    let child = ChildInfo::new_default(b"synthetic-borrowed-child");
    let mut storage = Storage::default();
    storage
        .top
        .insert(b"top-key".to_vec(), b"top-value".to_vec());
    storage.children_default.insert(
        b"synthetic-borrowed-child".to_vec(),
        StorageChild {
            data: [(b"child-key".to_vec(), b"child-value".to_vec())].into(),
            child_info: child.clone(),
        },
    );
    let parent: InMemoryBackend<Blake2Hasher> = (storage, StateVersion::V1).into();
    let original_root = *parent.root();
    let scopes = Scopes::default();
    let observed = Mutex::new(Vec::new());
    {
        let backend = ScopedBackend {
            inner: TrieBackendBuilder::new(
                BorrowedStorage {
                    inner: parent.backend_storage(),
                    scopes: &scopes,
                    observed: &observed,
                },
                original_root,
            )
            .build(),
            scopes: &scopes,
        };
        let mut top_args = IterArgs::default();
        top_args.prefix = Some(b"top-");
        let mut top = backend.raw_iter(top_args).unwrap();
        assert!(
            scopes.current().is_err(),
            "iterator-open retained its scope"
        );
        scopes.check().unwrap();

        let mut child_args = IterArgs::default();
        child_args.child_info = Some(child.clone());
        let mut child_iterator = backend.raw_iter(child_args).unwrap();
        assert!(scopes.current().is_err(), "child-open retained its scope");
        scopes.check().unwrap();

        assert_eq!(
            top.next_pair(&backend).unwrap().unwrap(),
            (b"top-key".to_vec(), b"top-value".to_vec())
        );
        assert!(
            scopes.current().is_err(),
            "top iteration retained its scope"
        );
        assert_eq!(
            child_iterator.next_pair(&backend).unwrap().unwrap(),
            (b"child-key".to_vec(), b"child-value".to_vec())
        );
        assert!(
            scopes.current().is_err(),
            "child iteration retained its scope"
        );
        assert!(top.next_key(&backend).is_none());
        assert!(child_iterator.next_key(&backend).is_none());
        assert!(top.was_complete() && child_iterator.was_complete());
        scopes.check().unwrap();
    }
    let observed = observed.into_inner().unwrap();
    assert!(observed.iter().any(|operation| {
        operation.method.starts_with("iterator-") && operation.child.is_none()
    }));
    assert!(observed.iter().any(|operation| {
        operation.method.starts_with("iterator-") && operation.child.as_ref() == Some(&child)
    }));
    assert!(observed
        .iter()
        .any(|operation| { operation.method == "child-root" && operation.child.is_none() }));
    assert!(
        scopes.current().is_err(),
        "completed iterators retained a scope"
    );
    scopes.check().unwrap();
    assert_eq!(parent.root(), &original_root);
    assert_eq!(
        parent.storage(b"top-key").unwrap(),
        Some(b"top-value".to_vec())
    );
    assert_eq!(
        parent.child_storage(&child, b"child-key").unwrap(),
        Some(b"child-value".to_vec())
    );
}
