// Package goconst provides read-only collection views used by code generated
// by protoc-gen-go-const.
//
// Concrete protobuf messages expose repeated/map fields as []T / map[K]V,
// which carry no "read-only" guarantee at the Go type level: callers can
// append, re-slice, or assign into them. protoc-gen-go-const generates
// Message_Const interface views where repeated/map field getters return
// [Slice] / [Map] instead, so a caller can:
//
//   - query length and look up by index / key without having to iterate;
//   - iterate via the standard range-over-func form using All();
//   - but cannot append, grow, shrink or overwrite the underlying
//     collection.
//
// The default [Slice] / [Map] implementations are provided by this package
// via [NewSlice] / [NewMap] (used for scalar element types and for message
// types from excluded packages) and [NewSlice2] / [NewMap2] (used for
// message element types whose concrete value implements [Constable], i.e.
// exposes an AsConst() view). The generator only emits thin getters that
// delegate to these constructors; no per-field wrapper struct is generated.
package goconst

import "iter"

// Slice is a read-only view over a repeated protobuf field of element type T.
//
// The value is cheap to pass around (a small struct holding the underlying
// slice) and supports direct length / index access as well as ranged
// iteration via All.
type Slice[T any] interface {
	// Len returns the number of elements in the underlying slice.
	Len() int
	// At returns the element at index i. It panics with an out-of-range
	// error if i is not in [0, Len()), matching built-in slice semantics.
	At(i int) T
	// All returns a range-over-func iterator yielding (index, element)
	// pairs in order, compatible with Go 1.23+ "for i, v := range s.All()".
	All() iter.Seq2[int, T]
}

// Map is a read-only view over a map protobuf field with key type K and
// value type V.
//
// It mirrors the subset of Go's built-in map operations that do not
// mutate the map: length, key lookup, presence check, and iteration.
type Map[K comparable, V any] interface {
	// Len returns the number of entries in the underlying map.
	Len() int
	// Get returns the value associated with key k and true if present,
	// or the zero V and false otherwise — the same shape as the comma-ok
	// form of a built-in map lookup.
	Get(k K) (V, bool)
	// Has reports whether key k is present in the underlying map.
	Has(k K) bool
	// All returns a range-over-func iterator yielding (key, value) pairs
	// in unspecified order, compatible with Go 1.23+
	// "for k, v := range m.All()".
	All() iter.Seq2[K, V]
}

// Constable is the constraint satisfied by any value that can be projected
// into a read-only view of type T via its AsConst method. protoc-gen-go-const
// emits an AsConst() <MessageName>_Const method on every generated message
// pointer type, so *FooPb satisfies Constable[FooPb_Const].
type Constable[T any] interface {
	AsConst() T
}

// NewSlice returns a read-only [Slice] view over s. Elements are returned
// as-is from At / All, so it is suitable for scalar element types and for
// message element types whose [Constable] projection is not desired
// (e.g. messages from excluded packages).
func NewSlice[T any](s []T) Slice[T] {
	return sliceImpl[T]{s: s}
}

// NewSlice2 returns a read-only [Slice] view over s whose elements are
// projected through the [Constable] AsConst method. It is used by the
// generator for repeated message fields whose element type has a _Const
// interface view.
func NewSlice2[T any, E Constable[T]](s []E) Slice[T] {
	return sliceAsConstImpl[T, E]{s: s}
}

// NewMap returns a read-only [Map] view over m. Values are returned as-is
// from Get / All, so it is suitable for scalar value types and for message
// value types whose [Constable] projection is not desired.
func NewMap[K comparable, V any](m map[K]V) Map[K, V] {
	return mapImpl[K, V]{m: m}
}

// NewMap2 returns a read-only [Map] view over m whose values are projected
// through the [Constable] AsConst method. It is used by the generator for
// map fields whose value type has a _Const interface view.
func NewMap2[K comparable, V any, E Constable[V]](m map[K]E) Map[K, V] {
	return mapAsConstImpl[K, V, E]{m: m}
}

// --- internal implementations -----------------------------------------------

type sliceImpl[T any] struct{ s []T }

func (c sliceImpl[T]) Len() int   { return len(c.s) }
func (c sliceImpl[T]) At(i int) T { return c.s[i] }
func (c sliceImpl[T]) All() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i, v := range c.s {
			if !yield(i, v) {
				return
			}
		}
	}
}

type sliceAsConstImpl[T any, E Constable[T]] struct{ s []E }

func (c sliceAsConstImpl[T, E]) Len() int   { return len(c.s) }
func (c sliceAsConstImpl[T, E]) At(i int) T { return c.s[i].AsConst() }
func (c sliceAsConstImpl[T, E]) All() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i, v := range c.s {
			if !yield(i, v.AsConst()) {
				return
			}
		}
	}
}

type mapImpl[K comparable, V any] struct{ m map[K]V }

func (c mapImpl[K, V]) Len() int { return len(c.m) }
func (c mapImpl[K, V]) Get(k K) (V, bool) {
	v, ok := c.m[k]
	return v, ok
}
func (c mapImpl[K, V]) Has(k K) bool {
	_, ok := c.m[k]
	return ok
}
func (c mapImpl[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for k, v := range c.m {
			if !yield(k, v) {
				return
			}
		}
	}
}

type mapAsConstImpl[K comparable, V any, E Constable[V]] struct{ m map[K]E }

func (c mapAsConstImpl[K, V, E]) Len() int { return len(c.m) }
func (c mapAsConstImpl[K, V, E]) Get(k K) (V, bool) {
	v, ok := c.m[k]
	if !ok {
		var zero V
		return zero, false
	}
	return v.AsConst(), true
}
func (c mapAsConstImpl[K, V, E]) Has(k K) bool {
	_, ok := c.m[k]
	return ok
}
func (c mapAsConstImpl[K, V, E]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for k, v := range c.m {
			if !yield(k, v.AsConst()) {
				return
			}
		}
	}
}
