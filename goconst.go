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
//   - iterate via the standard range-over-func form using All(), or get a
//     single-value iterator via Slice.Values / Map.Keys / Map.Values that
//     plugs straight into stdlib sinks (slices.Collect, slices.Sorted, …)
//     and any iter.Seq-aware third-party helper (e.g. samber/lo/it);
//   - but cannot append, grow, shrink or overwrite the underlying
//     collection.
//
// The default [Slice] / [Map] implementations are provided by this package
// via [NewSlice] / [NewMap] (used for scalar element types and for message
// types from excluded packages) and [NewSlice2] / [NewMap2] (used for
// message element types whose concrete value implements [Constable], i.e.
// exposes an AsConst() view). The generator only emits thin getters that
// delegate to these constructors; each constructor is a zero-copy type
// conversion into a named slice / map type that directly carries the
// read-only method set — no struct wrapper is allocated per call.
package goconst

import (
	"fmt"
	"iter"
	"maps"
	"slices"
)

// Slice is a read-only view over a repeated protobuf field of element type T.
//
// The value is cheap to pass around (a named slice type, no struct wrapper)
// and supports direct length / index access as well as ranged iteration via
// All / Values. Higher-level algorithms (ContainsBy, Find, MinBy, ...) are
// intentionally not methods on this interface: callers can reuse any
// [iter.Seq]-aware helper, e.g. github.com/samber/lo/it, directly against
// [Slice.Values] / [Slice.All].
//
// The default implementations also satisfy [fmt.Stringer]: passing a Slice
// to fmt.Print / log.Print / %v produces exactly the same output as
// printing the underlying []T directly — no extra "Slice[...]" wrapper —
// so debug logging does not have to go through slices.Collect first.
type Slice[T any] interface {
	// Len returns the number of elements in the underlying slice.
	Len() int
	// At returns the element at index i. It panics with an out-of-range
	// error if i is not in [0, Len()), matching built-in slice semantics.
	At(i int) T
	// All returns a range-over-func iterator yielding (index, element)
	// pairs in order, compatible with Go 1.23+ "for i, v := range s.All()".
	// Analogue of [slices.All].
	All() iter.Seq2[int, T]
	// Values returns a range-over-func iterator yielding just the elements
	// in order. It is the single-value companion to [Slice.All] and can be
	// fed directly into standard library sinks such as [slices.Collect] or
	// [slices.Sorted], or into iter-aware third-party helpers such as
	// github.com/samber/lo/it. Analogue of [slices.Values].
	Values() iter.Seq[T]
}

// Map is a read-only view over a map protobuf field with key type K and
// value type V.
//
// It mirrors the subset of Go's built-in map operations that do not
// mutate the map: length, key lookup, presence check, and iteration.
// Keys and Values return range-over-func iterators so callers can feed
// them directly into [slices.Collect], [slices.Sorted], or any other
// iter.Seq sink — matching the shape of the standard library's
// [maps.Keys] and [maps.Values].
//
// The default implementations also satisfy [fmt.Stringer]: passing a Map
// to fmt.Print / log.Print / %v produces exactly the same output as
// printing the underlying map[K]V directly — no extra "Map[...]" wrapper
// — so debug logging does not have to go through maps.Collect first.
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
	// "for k, v := range m.All()". Analogue of [maps.All].
	All() iter.Seq2[K, V]
	// Keys returns a range-over-func iterator yielding just the keys in
	// unspecified order. Analogue of [maps.Keys].
	Keys() iter.Seq[K]
	// Values returns a range-over-func iterator yielding just the values
	// in unspecified order. Analogue of [maps.Values].
	Values() iter.Seq[V]
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
	return _Slice[T](s)
}

// NewSlice2 returns a read-only [Slice] view over s whose elements are
// projected through the [Constable] AsConst method. It is used by the
// generator for repeated message fields whose element type has a _Const
// interface view.
func NewSlice2[T any, E Constable[T]](s []E) Slice[T] {
	return _Slice2[T, E](s)
}

// NewMap returns a read-only [Map] view over m. Values are returned as-is
// from Get / All, so it is suitable for scalar value types and for message
// value types whose [Constable] projection is not desired.
func NewMap[K comparable, V any](m map[K]V) Map[K, V] {
	return _Map[K, V](m)
}

// NewMap2 returns a read-only [Map] view over m whose values are projected
// through the [Constable] AsConst method. It is used by the generator for
// map fields whose value type has a _Const interface view.
func NewMap2[K comparable, V any, E Constable[V]](m map[K]E) Map[K, V] {
	return _Map2[K, V, E](m)
}

// --- internal implementations -----------------------------------------------
//
// Each impl is a named type whose underlying type is the raw slice or map,
// not a struct wrapper around it. This makes the constructors above pure
// type conversions (no extra allocation, no extra pointer indirection on
// method dispatch) while still attaching the read-only method set.

type _Slice[T any] []T

func (c _Slice[T]) Len() int               { return len(c) }
func (c _Slice[T]) At(i int) T             { return c[i] }
func (c _Slice[T]) All() iter.Seq2[int, T] { return slices.All(c) }
func (c _Slice[T]) Values() iter.Seq[T]    { return slices.Values(c) }

type _Slice2[T any, E Constable[T]] []E

func (c _Slice2[T, E]) Len() int   { return len(c) }
func (c _Slice2[T, E]) At(i int) T { return c[i].AsConst() }
func (c _Slice2[T, E]) All() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i, v := range c {
			if !yield(i, v.AsConst()) {
				return
			}
		}
	}
}
func (c _Slice2[T, E]) Values() iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, v := range c {
			if !yield(v.AsConst()) {
				return
			}
		}
	}
}

type _Map[K comparable, V any] map[K]V

func (c _Map[K, V]) Len() int { return len(c) }
func (c _Map[K, V]) Get(k K) (V, bool) {
	v, ok := c[k]
	return v, ok
}
func (c _Map[K, V]) Has(k K) bool {
	_, ok := c[k]
	return ok
}
func (c _Map[K, V]) All() iter.Seq2[K, V] { return maps.All(c) }
func (c _Map[K, V]) Keys() iter.Seq[K]    { return maps.Keys(c) }
func (c _Map[K, V]) Values() iter.Seq[V]  { return maps.Values(c) }

type _Map2[K comparable, V any, E Constable[V]] map[K]E

func (c _Map2[K, V, E]) Len() int { return len(c) }
func (c _Map2[K, V, E]) Get(k K) (V, bool) {
	v, ok := c[k]
	if !ok {
		var zero V
		return zero, false
	}
	return v.AsConst(), true
}
func (c _Map2[K, V, E]) Has(k K) bool {
	_, ok := c[k]
	return ok
}
func (c _Map2[K, V, E]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for k, v := range c {
			if !yield(k, v.AsConst()) {
				return
			}
		}
	}
}
func (c _Map2[K, V, E]) Keys() iter.Seq[K] { return maps.Keys(c) }
func (c _Map2[K, V, E]) Values() iter.Seq[V] {
	return func(yield func(V) bool) {
		for _, v := range c {
			if !yield(v.AsConst()) {
				return
			}
		}
	}
}

// String implementations for debug-friendly printing.
//
// Each impl delegates to fmt.Sprint on its underlying slice / map type, so
// "fmt.Println(s)" (or "%v" / "%s" in a format string) produces exactly the
// same output as printing the raw []T / map[K]V would — including the
// built-in prototext-style String() of protobuf messages for _Slice2 /
// _Map2. There is no extra "Slice[...]" / "Map[...]" wrapper in the output.
//
// Note that _Slice2 / _Map2 print the *underlying* elements E, not the
// projected T views, so that protobuf messages (whose pointer type already
// has a rich String()) render in full rather than as opaque interface
// addresses.

func (c _Slice[T]) String() string      { return fmt.Sprint([]T(c)) }
func (c _Slice2[T, E]) String() string  { return fmt.Sprint([]E(c)) }
func (c _Map[K, V]) String() string     { return fmt.Sprint(map[K]V(c)) }
func (c _Map2[K, V, E]) String() string { return fmt.Sprint(map[K]E(c)) }
