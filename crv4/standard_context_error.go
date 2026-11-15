// The standard library reports some context expiries and cancellations with
// its own private error values. Each matches its context error only through
// an Is method, and the cause classifiers here and in the validator never call
// a foreign Is or As, so they refused these values as opaque matchers. A dial
// or lookup timeout then became a hard failure, and the validator crash-looped
// for two hours while api.bringyour.com did not resolve (2026-11-13).
package crv4

import (
	"context"
	"reflect"
)

// Returns the context error that an exact standard library value stands for,
// and true; otherwise nil and false:
//   - net's dial and lookup timeout ("i/o timeout"), which net maps from
//     context.DeadlineExceeded and returns inside OpError or as
//     DNSError.UnwrapErr;
//   - net/http's Client.Timeout and response-header timeout errors;
//   - net's "operation was canceled", which net maps from context.Canceled.
//
// Only these exact types qualify, named by package path and type name. No
// method of err is called, so a foreign error cannot claim either verdict.
func StandardContextError(err error) (error, bool) {
	if err == nil {
		return nil, false
	}
	value := reflect.ValueOf(err)
	errType := value.Type()
	if errType.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, false
		}
		errType = errType.Elem()
	}
	switch {
	case errType.PkgPath() == "net" && errType.Name() == "timeoutError",
		errType.PkgPath() == "net/http" && errType.Name() == "timeoutError":
		return context.DeadlineExceeded, true
	case errType.PkgPath() == "net" && errType.Name() == "canceledError":
		return context.Canceled, true
	}
	return nil, false
}
