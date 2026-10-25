// A second classifier for callers whose causes arrive inside the standard
// library's own fmt.Errorf %w and errors.Join wrappers. It keeps ClassifyCause's
// rule that no arbitrary error method runs: those wrappers are opened only when
// a value has exactly their dynamic type, so the only Unwrap methods invoked are
// the standard library's, which return a stored field.
package diagnostics

import (
	"errors"
	"fmt"
	"io"
	"reflect"
)

// fmt and errors do not export these types; each is named by a value the
// standard library itself returned.
var (
	standardWrapType  = reflect.TypeOf(fmt.Errorf("%w", io.EOF))
	standardWrapsType = reflect.TypeOf(fmt.Errorf("%w%w", io.EOF, io.EOF))
	standardJoinType  = reflect.TypeOf(errors.Join(io.EOF))
)

// One inspection visits at most this many nodes, each at most this deep.
const (
	wrappedCauseMaximumNodes = 128
	wrappedCauseMaximumDepth = 32
)

// A CauseEnvelope opens one caller-owned node with concrete type switches and
// field reads only. It returns the node's children to inspect next, or no
// children and a final cause for a leaf it recognizes. owned is false for every
// value it does not own, which then stays unknown.
type CauseEnvelope func(err error) (children []error, cause Cause, owned bool)

// StandardWrapped returns the children of the standard library's own
// fmt.Errorf %w wrapper or errors.Join value. Every other value is left closed,
// including a type that embeds or imitates one, and none of its methods run.
// The returned slice belongs to the wrapper and must not be changed.
func StandardWrapped(err error) (children []error, ok bool) {
	if err == nil {
		return nil, false
	}
	kind := reflect.TypeOf(err)
	if kind != standardWrapType && kind != standardWrapsType && kind != standardJoinType {
		return nil, false
	}
	if reflect.ValueOf(err).IsNil() {
		return nil, false
	}
	if kind == standardWrapType {
		return []error{err.(interface{ Unwrap() error }).Unwrap()}, true
	}
	return err.(interface{ Unwrap() []error }).Unwrap(), true
}

// IsStandardWrapper reports whether err is exactly the standard library's own
// fmt.Errorf %w wrapper or errors.Join value, without calling any of its
// methods.
func IsStandardWrapper(err error) bool {
	if err == nil {
		return false
	}
	kind := reflect.TypeOf(err)
	return kind == standardWrapType || kind == standardWrapsType || kind == standardJoinType
}

// ClassifyWrappedCause is ClassifyCause that also follows the standard
// library's %w and Join wrappers and the caller's envelope. A node with several
// children has a cause only when every non-nil child has that same cause; an
// opaque child or a disagreement leaves it unknown. Like ClassifyCause it is
// never a retry or custody predicate.
func ClassifyWrappedCause(err error, envelope CauseEnvelope) Cause {
	if err == nil {
		return CauseNone
	}
	remaining := wrappedCauseMaximumNodes
	return classifyWrappedCause(err, envelope, &remaining, 0)
}

func classifyWrappedCause(err error, envelope CauseEnvelope, remaining *int, depth int) Cause {
	for ; depth < wrappedCauseMaximumDepth && err != nil; depth++ {
		if *remaining <= 0 {
			return CauseUnknown
		}
		*remaining--
		cause, next, follow := classifyConcreteCause(err)
		if follow {
			err = next
			continue
		}
		if cause != CauseUnknown {
			return cause
		}
		children, owned := StandardWrapped(err)
		if !owned && envelope != nil {
			children, cause, owned = envelope(err)
			if owned && len(children) == 0 {
				return cause
			}
		}
		if !owned || len(children) == 0 {
			return CauseUnknown
		}
		if len(children) == 1 {
			err = children[0]
			continue
		}
		verdict := CauseNone
		for _, child := range children {
			if child == nil {
				continue
			}
			found := classifyWrappedCause(child, envelope, remaining, depth+1)
			if found == CauseUnknown || verdict != CauseNone && found != verdict {
				return CauseUnknown
			}
			verdict = found
		}
		if verdict == CauseNone {
			return CauseUnknown
		}
		return verdict
	}
	return CauseUnknown
}
