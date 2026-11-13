// Revert diagnostics inspect the same finite cause graph as read admission.
// Only geth's decoded remote error may supply data; original errors stay owned.
package onchain

import (
	"reflect"

	"github.com/ethereum/go-ethereum/rpc"
)

// Every edge must fit one inspection allowance before optional diagnostics are
// decoded. Conflicting data or incomplete graphs leave the original error alone.
func onchainRevertData(err error) []byte {
	remaining := 128
	var encoded string
	var found bool
	var visit func(error, int) bool
	visit = func(err error, depth int) bool {
		remaining--
		if err == nil || remaining < 0 || depth > 32 {
			return false
		}
		value := reflect.ValueOf(err)
		switch value.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
			if value.IsNil() {
				return false
			}
		}
		typeOf := reflect.TypeOf(err)
		if typeOf.Kind() == reflect.Pointer {
			typeOf = typeOf.Elem()
		}
		if typeOf.PkgPath() == "github.com/ethereum/go-ethereum/rpc" && typeOf.Name() == "jsonError" {
			if remote, ok := err.(rpc.DataError); ok {
				if data, ok := remote.ErrorData().(string); ok && data != "" {
					if found && encoded != data {
						return false
					}
					encoded, found = data, true
				}
			}
			return true
		}
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			causes := joined.Unwrap()
			if len(causes) == 0 || len(causes) > remaining {
				return false
			}
			for _, cause := range causes {
				if !visit(cause, depth+1) {
					return false
				}
			}
			return true
		}
		if wrapped, ok := err.(interface{ Unwrap() error }); ok {
			return visit(wrapped.Unwrap(), depth+1)
		}
		return true
	}
	if !visit(err, 0) || !found {
		return nil
	}
	return hexErrorData(encoded)
}
