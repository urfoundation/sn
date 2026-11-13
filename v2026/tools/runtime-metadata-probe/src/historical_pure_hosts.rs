//! Select exact pinned SDK verification/recovery functions. No signing,
//! keystore, RNG, network or filesystem host is registered by this adapter.
//! Both static Wasmtime dispatch and the dynamic interface meter arguments
//! before allocation. Host semantics, including versioned signature rules,
//! remain those of the SDK rather than a second crypto implementation.

use super::hosts::charge_active;
use sp_wasm_interface::{
    Function, FunctionContext, HostFunctionRegistry, HostFunctions, Pointer, Signature, Value,
    WordSize,
};
use std::sync::OnceLock;

const MAXIMUM_CRYPTO_BYTES: usize = 8 * 1024 * 1024;

fn selected(name: &str) -> bool {
    matches!(
        name,
        "ext_crypto_ed25519_verify_version_1"
            | "ext_crypto_sr25519_verify_version_1"
            | "ext_crypto_sr25519_verify_version_2"
            | "ext_crypto_secp256k1_ecdsa_recover_version_1"
            | "ext_crypto_secp256k1_ecdsa_recover_version_2"
            | "ext_crypto_secp256k1_ecdsa_recover_compressed_version_1"
            | "ext_crypto_secp256k1_ecdsa_recover_compressed_version_2"
    )
}

fn memory_work(bytes: usize) -> sp_wasm_interface::Result<()> {
    if bytes > MAXIMUM_CRYPTO_BYTES {
        return Err("historical crypto argument byte bound".to_owned());
    }
    charge_active(bytes);
    Ok(())
}

struct Memory<'a>(&'a mut dyn FunctionContext);
impl FunctionContext for Memory<'_> {
    fn read_memory(
        &self,
        address: Pointer<u8>,
        size: WordSize,
    ) -> sp_wasm_interface::Result<Vec<u8>> {
        memory_work(size as usize)?;
        self.0.read_memory(address, size)
    }
    fn read_memory_into(
        &self,
        address: Pointer<u8>,
        output: &mut [u8],
    ) -> sp_wasm_interface::Result<()> {
        memory_work(output.len())?;
        self.0.read_memory_into(address, output)
    }
    fn write_memory(
        &mut self,
        address: Pointer<u8>,
        bytes: &[u8],
    ) -> sp_wasm_interface::Result<()> {
        memory_work(bytes.len())?;
        self.0.write_memory(address, bytes)
    }
    fn allocate_memory(&mut self, size: WordSize) -> sp_wasm_interface::Result<Pointer<u8>> {
        memory_work(size as usize)?;
        self.0.allocate_memory(size)
    }
    fn deallocate_memory(&mut self, pointer: Pointer<u8>) -> sp_wasm_interface::Result<()> {
        charge_active(0);
        self.0.deallocate_memory(pointer)
    }
    fn register_panic_error_message(&mut self, message: &str) {
        memory_work(message.len()).expect("historical crypto diagnostic bound");
        self.0.register_panic_error_message(message);
    }
}

struct MeteredFunction(&'static dyn Function);
impl Function for MeteredFunction {
    fn name(&self) -> &str {
        self.0.name()
    }
    fn signature(&self) -> Signature {
        self.0.signature()
    }
    fn execute(
        &self,
        context: &mut dyn FunctionContext,
        arguments: &mut dyn Iterator<Item = Value>,
    ) -> sp_wasm_interface::Result<Option<Value>> {
        charge_active(0);
        self.0.execute(&mut Memory(context), arguments)
    }
}

pub(super) struct CryptoHosts;
impl HostFunctions for CryptoHosts {
    fn host_functions() -> Vec<&'static dyn Function> {
        // The finite SDK function set is constructed once, not once per job.
        static FUNCTIONS: OnceLock<Vec<&'static dyn Function>> = OnceLock::new();
        FUNCTIONS
            .get_or_init(|| {
                sp_io::crypto::HostFunctions::host_functions()
                    .into_iter()
                    .filter(|function| selected(function.name()))
                    .map(|function| {
                        Box::leak(Box::new(MeteredFunction(function))) as &'static dyn Function
                    })
                    .collect()
            })
            .clone()
    }
    fn register_static<T: HostFunctionRegistry>(registry: &mut T) -> Result<(), T::Error> {
        struct Registry<'a, T>(&'a mut T);
        impl<T: HostFunctionRegistry> HostFunctionRegistry for Registry<'_, T> {
            type State = T::State;
            type Error = T::Error;
            type FunctionContext = T::FunctionContext;
            fn with_function_context<R>(
                caller: wasmtime::Caller<Self::State>,
                callback: impl FnOnce(&mut dyn FunctionContext) -> R,
            ) -> R {
                T::with_function_context(caller, |context| {
                    charge_active(0);
                    callback(&mut Memory(context))
                })
            }
            fn register_static<Params, Results>(
                &mut self,
                name: &str,
                function: impl wasmtime::IntoFunc<Self::State, Params, Results> + 'static,
            ) -> Result<(), Self::Error> {
                if selected(name) {
                    self.0.register_static(name, function)
                } else {
                    Ok(())
                }
            }
        }
        sp_io::crypto::HostFunctions::register_static(&mut Registry(registry))
    }
}
