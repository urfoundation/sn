// Native read classifiers share finite error-tree ownership. Opaque transport
// markers still retain their own complete cause; custom matchers grant nothing.
package crv4

import "reflect"

// One traversal owns this counter across joins, wrappers and native markers.
// Depth bounds cycles without assuming error values are comparable map keys.
type substrateReadCauseBudget struct {
	remaining int
}

// Typed nil errors cannot supply an observation or be safely unwrapped.
func (self *substrateReadCauseBudget) admit(err error, depth int) bool {
	if err == nil || depth > 32 || self.remaining <= 0 {
		return false
	}
	self.remaining--
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return !value.IsNil()
	default:
		return true
	}
}

// An empty or oversized branch is incomplete evidence, even if its first
// member would otherwise admit a transient failure.
func (self *substrateReadCauseBudget) admitsChildren(causes []error) bool {
	return len(causes) != 0 && len(causes) <= self.remaining
}

// Concrete trusted errno and transport leaves are handled before this guard.
// Arbitrary Is/As implementations must not override the complete cause tree.
func substrateReadCustomMatcher(err error) bool {
	switch err.(type) {
	case interface{ Is(error) bool }, interface{ As(any) bool }:
		return true
	default:
		return false
	}
}
