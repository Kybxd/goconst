// Package goconst provides the runtime types used by code emitted by
// protoc-gen-go-const.
//
// For every protobuf message Foo the plugin emits a Foo_Const struct
// whose only payload is an unexported *Foo (plus a zero-width
// [DoNotCompare] marker that turns `view == view` into a compile
// error). Repeated and map fields are exposed via the [Slice] /
// [Slice2] / [Map] / [Map2] read-only collection views defined here:
// concrete struct wrappers around the underlying []T / []E /
// map[K]V / map[K]E whose payload is unexported, so the usual
// mutation operations (s[i] = …, append, copy, clear, m[k] = …,
// delete) all fail to compile. Iteration goes through the Go 1.23+
// range-over-func shapes ([iter.Seq] / [iter.Seq2]).
//
// The split between [Slice] / [Map] and [Slice2] / [Map2] is the
// element type contract: [Slice] / [Map] store T / V verbatim and are
// used for scalar / bytes / enum elements as well as messages from
// --exclude_packages, while [Slice2] / [Map2] store the concrete *E
// (e.g. *Address) and project each element through E.AsConst() (see
// [Constable]) on access so callers see the read-only wrapper view.
//
// All four views also expose a Clone() escape hatch returning a fresh,
// mutable native []T / []E / map[K]V / map[K]E whose header is
// independent of the view. [Slice2] and [Map2] dispatch each element
// through its own Clone() (statically required by [Cloneable]).
// [Slice] and [Map], whose element type is not statically [Cloneable],
// pick a per-element strategy once at entry by type-switching on
// any(zero T): [proto.Message] elements go through [proto.Clone],
// []byte elements through [bytes.Clone], everything else through a
// single bulk [copy] / [maps.Copy] (matching [slices.Clone] /
// [maps.Clone]).
package goconst

import (
	"bytes"
	"fmt"
	"iter"
	"maps"
	"slices"

	"google.golang.org/protobuf/proto"
)

// Constable is the witness that a value (typically *Foo) projects to a
// read-only view T (typically Foo_Const) via its AsConst method.
// protoc-gen-go-const emits AsConst() Foo_Const on every message
// pointer, so *Foo satisfies Constable[Foo_Const].
type Constable[T any] interface {
	AsConst() T
}

// Cloneable is the witness that a read-only view T (typically
// Foo_Const) deep-copies itself into a fresh, mutable concrete pointer
// E (typically *Foo) via its Clone method. Used by [Slice2] and [Map2]
// to dispatch per-element deep-copies statically — the element view's
// Clone is responsible for whatever copy strategy the wrapped concrete
// type requires (typically [proto.Clone]).
type Cloneable[E any] interface {
	Clone() E
}

// DoNotCompare is a zero-width, non-comparable marker (`[0]func()`)
// intended to be carried as a struct field to make the whole struct
// non-comparable at compile time: any `==` or `!=` on a struct that
// embeds it becomes a build error, while layout and zero value are
// otherwise unchanged.
//
// The recommended form is a blank-named field:
//
//	type Foo struct {
//		_ goconst.DoNotCompare
//		// ...other fields...
//	}
//
// Blank-naming preserves the non-comparability guarantee while making
// the field unreachable by selector and never promoting any future
// methods of DoNotCompare onto Foo. Plain embedding is still accepted
// and equivalent for the non-comparability guarantee, just less
// hygienic.
//
// Carried (in blank-named form) by every collection view defined here
// and by every Foo_Const wrapper produced by protoc-gen-go-const, so
// pointer-equality reasoning on view values — meaningless when the
// view's only semantic content is the forwarded-to message — surfaces
// as a build error rather than a silently-wrong check. Cf.
// https://pkg.go.dev/google.golang.org/protobuf/internal/pragma#DoNotCompare,
// from which this pattern is borrowed.
type DoNotCompare [0]func()

// ---------------------------------------------------------------------------
// Slice — read-only view over a repeated protobuf field.
// ---------------------------------------------------------------------------

// Slice is a read-only view over a repeated protobuf field of element
// type T. Used for scalar / enum / bytes elements and for messages
// from --exclude_packages, where elements are kept as the concrete
// type rather than projected through AsConst() — see [Slice2] for the
// projecting variant.
//
// The unexported payload makes the read-only contract a compile-time
// property: s[i] = x, append(s, …), copy(s, …), clear(s) all fail to
// compile.
//
// Iteration goes through All / Values, which return the stdlib
// range-over-func shapes ([iter.Seq2] / [iter.Seq]) so callers can
// feed s.Values() directly into [slices.Collect] / [slices.Sorted] or
// any iter.Seq-aware helper. Slice satisfies [fmt.Stringer] by
// printing the underlying []T verbatim — no extra "Slice[…]" wrapper.
type Slice[T any] struct {
	_ DoNotCompare
	s []T
}

// NewSlice returns a read-only [Slice] view over s. Elements are
// returned as-is from At / All / Values; the projecting variant is
// [NewSlice2].
func NewSlice[T any](s []T) Slice[T] {
	return Slice[T]{s: s}
}

// Len returns the number of elements in the underlying slice.
func (c Slice[T]) Len() int { return len(c.s) }

// At returns the element at index i, panicking with an out-of-range
// error for i ∉ [0, Len()) — same semantics as built-in slice
// indexing.
func (c Slice[T]) At(i int) T { return c.s[i] }

// All returns an iterator yielding (index, element) pairs in order.
// Analogue of [slices.All].
func (c Slice[T]) All() iter.Seq2[int, T] { return slices.All(c.s) }

// Values returns an iterator yielding just the elements in order.
// Analogue of [slices.Values].
func (c Slice[T]) Values() iter.Seq[T] { return slices.Values(c.s) }

// IsNil reports whether the underlying slice header is nil. An empty
// but non-nil slice reports false; use Len() == 0 for the
// "nothing to read" reading.
func (c Slice[T]) IsNil() bool { return c.s == nil }

// String prints the underlying []T verbatim — no "Slice[…]" wrapper.
func (c Slice[T]) String() string { return fmt.Sprint(c.s) }

// Clone returns a fresh, mutable []T whose header is independent of
// the view's backing slice. Per-element strategy is selected once at
// entry by type-switching on any(zero T): [proto.Message] elements
// are deep-copied via [proto.Clone], []byte elements via
// [bytes.Clone], every other shape via a single bulk [copy] (matching
// [slices.Clone]'s shallow-element contract).
//
// The switch reads the *dynamic* type of any(zero) and is therefore
// valid even when zero is a typed nil (e.g. nil *timestamppb.Timestamp
// still reports proto.Message and selects the proto branch).
//
// A nil backing slice clones to a nil []T (matching [slices.Clone]).
func (c Slice[T]) Clone() []T {
	if c.s == nil {
		return nil
	}
	out := make([]T, len(c.s))
	var zero T
	switch any(zero).(type) {
	case proto.Message:
		for i, v := range c.s {
			out[i] = proto.Clone(any(v).(proto.Message)).(T)
		}
	case []byte:
		for i, v := range c.s {
			out[i] = any(bytes.Clone(any(v).([]byte))).(T)
		}
	default:
		copy(out, c.s)
	}
	return out
}

// ---------------------------------------------------------------------------
// Slice2 — read-only view over a repeated message field with AsConst projection.
// ---------------------------------------------------------------------------

// Slice2 is a read-only view over a repeated protobuf field of message
// element type *E, projected element-by-element through E.AsConst() to
// yield the read-only view type T (e.g. Address_Const). Compared to
// [Slice], callers see the read-only wrapper rather than a mutable
// pointer, so the type system also denies dereference-then-write.
//
// The two type parameters mutually constrain each other: T must be
// [Cloneable] into E (T.Clone() E), and E must be [Constable] into T
// (E.AsConst() T). protoc-gen-go-const emits both methods on every
// generated wrapper / pointer pair, so callers rarely spell out E
// themselves — Go 1.23+ constraint type inference recovers it from
// the [NewSlice2] argument. Same compile-time read-only contract as
// [Slice].
type Slice2[T Cloneable[E], E Constable[T]] struct {
	_ DoNotCompare
	s []E
}

// NewSlice2 returns a read-only [Slice2] view over s, projecting each
// element through E.AsConst() on access. Used by the generator for
// repeated message fields whose element type has a _Const view.
func NewSlice2[T Cloneable[E], E Constable[T]](s []E) Slice2[T, E] {
	return Slice2[T, E]{s: s}
}

// Len returns the number of elements in the underlying slice.
func (c Slice2[T, E]) Len() int { return len(c.s) }

// At returns the AsConst() projection of the element at index i,
// panicking with an out-of-range error for i ∉ [0, Len()).
func (c Slice2[T, E]) At(i int) T { return c.s[i].AsConst() }

// All returns an iterator yielding (index, projected element) pairs
// in order. The yielded value type is the read-only view T, not the
// concrete E.
func (c Slice2[T, E]) All() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i, v := range c.s {
			if !yield(i, v.AsConst()) {
				return
			}
		}
	}
}

// Values returns an iterator yielding just the projected elements in
// order.
func (c Slice2[T, E]) Values() iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, v := range c.s {
			if !yield(v.AsConst()) {
				return
			}
		}
	}
}

// IsNil reports whether the underlying slice header is nil. An empty
// but non-nil slice reports false; use Len() == 0 for the "nothing to
// read" reading.
func (c Slice2[T, E]) IsNil() bool { return c.s == nil }

// String prints the underlying []E (the raw []*Message before AsConst
// projection) so messages render via their own prototext String()
// rather than as opaque addresses.
func (c Slice2[T, E]) String() string { return fmt.Sprint(c.s) }

// Clone returns a fresh, mutable []E whose elements are independent
// deep copies, produced by routing through the per-element
// AsConst().Clone() pair (statically dispatched, no proto import here).
// A nil backing slice clones to a nil []E.
func (c Slice2[T, E]) Clone() []E {
	if c.s == nil {
		return nil
	}
	out := make([]E, len(c.s))
	for i, v := range c.s {
		out[i] = v.AsConst().Clone()
	}
	return out
}

// ---------------------------------------------------------------------------
// Map — read-only view over a map protobuf field.
// ---------------------------------------------------------------------------

// Map is a read-only view over a map<K, V> protobuf field. Used for
// scalar / enum / bytes value types and for messages from
// --exclude_packages — see [Map2] for the projecting variant.
//
// Same compile-time read-only contract as [Slice]: m[k] = …,
// delete(m, k), clear(m) all fail to compile. Iteration goes through
// All / Keys / Values, the [maps.All] / [maps.Keys] / [maps.Values]
// shapes. Map satisfies [fmt.Stringer] by printing the underlying
// map[K]V verbatim — no extra "Map[…]" wrapper.
type Map[K comparable, V any] struct {
	_ DoNotCompare
	m map[K]V
}

// NewMap returns a read-only [Map] view over m. Values are returned
// as-is from Get / All / Values; the projecting variant is [NewMap2].
func NewMap[K comparable, V any](m map[K]V) Map[K, V] {
	return Map[K, V]{m: m}
}

// Len returns the number of entries in the underlying map.
func (c Map[K, V]) Len() int { return len(c.m) }

// Get returns the value for key k and true if present, or the Go
// zero value of V and false otherwise — same shape as the comma-ok
// form on a built-in map.
func (c Map[K, V]) Get(k K) (V, bool) {
	v, ok := c.m[k]
	return v, ok
}

// Has reports whether key k is present.
func (c Map[K, V]) Has(k K) bool {
	_, ok := c.m[k]
	return ok
}

// All returns an iterator yielding (key, value) pairs in unspecified
// order. Analogue of [maps.All].
func (c Map[K, V]) All() iter.Seq2[K, V] { return maps.All(c.m) }

// Keys returns an iterator yielding just the keys in unspecified
// order. Analogue of [maps.Keys].
func (c Map[K, V]) Keys() iter.Seq[K] { return maps.Keys(c.m) }

// Values returns an iterator yielding just the values in unspecified
// order. Analogue of [maps.Values].
func (c Map[K, V]) Values() iter.Seq[V] { return maps.Values(c.m) }

// IsNil reports whether the underlying map header is nil. An empty
// but non-nil map reports false; use Len() == 0 for the "nothing to
// read" reading.
func (c Map[K, V]) IsNil() bool { return c.m == nil }

// String prints the underlying map[K]V verbatim — no "Map[…]"
// wrapper.
func (c Map[K, V]) String() string { return fmt.Sprint(c.m) }

// Clone returns a fresh, mutable map[K]V whose header is independent
// of the view's backing map. Per-value strategy is selected once at
// entry by type-switching on any(zero V): [proto.Message] values are
// deep-copied via [proto.Clone], []byte values via [bytes.Clone],
// every other shape via a single bulk [maps.Copy] (matching
// [maps.Clone]'s shallow-value contract). A nil backing map clones to
// a nil map[K]V.
func (c Map[K, V]) Clone() map[K]V {
	if c.m == nil {
		return nil
	}
	out := make(map[K]V, len(c.m))
	var zero V
	switch any(zero).(type) {
	case proto.Message:
		for k, v := range c.m {
			out[k] = proto.Clone(any(v).(proto.Message)).(V)
		}
	case []byte:
		for k, v := range c.m {
			out[k] = any(bytes.Clone(any(v).([]byte))).(V)
		}
	default:
		maps.Copy(out, c.m)
	}
	return out
}

// ---------------------------------------------------------------------------
// Map2 — read-only view over a map message-valued field with AsConst projection.
// ---------------------------------------------------------------------------

// Map2 is a read-only view over a map<K, *E> protobuf field, projected
// value-by-value through E.AsConst() to yield the read-only view type
// V (e.g. Address_Const). The two value-side type parameters mutually
// constrain each other exactly as in [Slice2]: V must be [Cloneable]
// into E, and E must be [Constable] into V. Same compile-time
// read-only contract as [Map].
type Map2[K comparable, V Cloneable[E], E Constable[V]] struct {
	_ DoNotCompare
	m map[K]E
}

// NewMap2 returns a read-only [Map2] view over m, projecting each
// value through E.AsConst() on access. Used by the generator for map
// fields whose value type has a _Const view.
func NewMap2[K comparable, V Cloneable[E], E Constable[V]](m map[K]E) Map2[K, V, E] {
	return Map2[K, V, E]{m: m}
}

// Len returns the number of entries in the underlying map.
func (c Map2[K, V, E]) Len() int { return len(c.m) }

// Get returns the AsConst() projection of the value for key k and
// true if present, or a miss-safe nil-backed view of V and false
// otherwise. The second return value is the *authoritative* presence
// flag; the first is deliberately chosen so that v.GetX() is safe to
// call regardless of ok.
//
// On a miss the lookup yields the Go zero value of E (a nil *Foo);
// AsConst is a pointer-receiver method that just wraps its receiver
// without dereferencing it, so it produces exactly the nil-backed
// view Foo_Const{p: nil} we want — no branch needed.
func (c Map2[K, V, E]) Get(k K) (V, bool) {
	v, ok := c.m[k]
	return v.AsConst(), ok
}

// Has reports whether key k is present.
func (c Map2[K, V, E]) Has(k K) bool {
	_, ok := c.m[k]
	return ok
}

// All returns an iterator yielding (key, projected value) pairs in
// unspecified order. The yielded value type is the read-only view V,
// not the concrete E.
func (c Map2[K, V, E]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for k, v := range c.m {
			if !yield(k, v.AsConst()) {
				return
			}
		}
	}
}

// Keys returns an iterator yielding just the keys in unspecified
// order. Analogue of [maps.Keys].
func (c Map2[K, V, E]) Keys() iter.Seq[K] { return maps.Keys(c.m) }

// Values returns an iterator yielding just the projected values in
// unspecified order.
func (c Map2[K, V, E]) Values() iter.Seq[V] {
	return func(yield func(V) bool) {
		for _, v := range c.m {
			if !yield(v.AsConst()) {
				return
			}
		}
	}
}

// IsNil reports whether the underlying map header is nil. An empty
// but non-nil map reports false; use Len() == 0 for the "nothing to
// read" reading.
func (c Map2[K, V, E]) IsNil() bool { return c.m == nil }

// String prints the underlying map[K]E (the raw map[K]*Message before
// AsConst projection) so messages render via their own prototext
// String() rather than as opaque addresses.
func (c Map2[K, V, E]) String() string { return fmt.Sprint(c.m) }

// Clone returns a fresh, mutable map[K]E whose values are independent
// deep copies, produced by routing through the per-value
// AsConst().Clone() pair (statically dispatched, no proto import here).
// A nil backing map clones to a nil map[K]E.
func (c Map2[K, V, E]) Clone() map[K]E {
	if c.m == nil {
		return nil
	}
	out := make(map[K]E, len(c.m))
	for k, v := range c.m {
		out[k] = v.AsConst().Clone()
	}
	return out
}
