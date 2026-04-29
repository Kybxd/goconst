// Package goconst: slice algorithm extensions.
//
// This file isolates the "algorithms on read-only slices" method set from
// the bare view methods (Len / At / All) defined in goconst.go. Splitting
// them keeps the core [Slice] contract minimal while leaving an obvious
// place to grow the algorithm surface over time.
//
// Method selection rule: only include algorithms from github.com/samber/lo that
//   - are strictly read-only (no mutation of the underlying slice), and
//   - do not require the element type to be comparable.
//
// The second rule is what keeps [SliceAlgo] parameterised over `T any`
// (rather than `T comparable`) — callers supply the predicate themselves
// via a closure, so message-element slices whose T is an interface type
// (e.g. Slice[Sub_Const]) still get the full method set.
package goconst

import "github.com/samber/lo"

// SliceAlgo is the read-only algorithm method set implemented by every
// [Slice] view. It mirrors a curated subset of [github.com/samber/lo],
// restricted to methods that are both read-only and usable without a
// comparable constraint on T.
type SliceAlgo[T any] interface {
	// ContainsBy reports whether at least one element satisfies predicate.
	// Short-circuits on the first match.
	// Analogue of [lo.ContainsBy].
	ContainsBy(predicate func(item T) bool) bool

	// CountBy returns the number of elements for which predicate returns true.
	// Analogue of [lo.CountBy].
	CountBy(predicate func(item T) bool) int

	// EveryBy reports whether every element satisfies predicate.
	// Returns true on an empty slice.
	// Analogue of [lo.EveryBy].
	EveryBy(predicate func(item T) bool) bool

	// NoneBy reports whether no element satisfies predicate.
	// Returns true on an empty slice.
	// Analogue of [lo.NoneBy].
	NoneBy(predicate func(item T) bool) bool

	// Find returns the first element satisfying predicate and true.
	// If no element matches, it returns the zero value of T and false.
	// Analogue of [lo.Find].
	Find(predicate func(item T) bool) (T, bool)

	// MinBy returns the minimum element using less to determine order:
	// less(a, b) should return true when a is considered smaller than b.
	// Returns the zero value of T on an empty slice.
	// Analogue of [lo.MinBy].
	MinBy(less func(a, b T) bool) T

	// MaxBy returns the maximum element using greater to determine order:
	// greater(a, b) should return true when a is considered larger than b.
	// Returns the zero value of T on an empty slice.
	// Analogue of [lo.MaxBy].
	MaxBy(greater func(a, b T) bool) T
}

// ---------------------------------------------------------------------------
// _Slice[T] implementations — delegate directly to lo since the underlying
// type is exactly []T.
// ---------------------------------------------------------------------------

func (c _Slice[T]) ContainsBy(predicate func(item T) bool) bool {
	return lo.ContainsBy(c, predicate)
}

func (c _Slice[T]) CountBy(predicate func(item T) bool) int {
	return lo.CountBy(c, predicate)
}

func (c _Slice[T]) EveryBy(predicate func(item T) bool) bool {
	return lo.EveryBy(c, predicate)
}

func (c _Slice[T]) NoneBy(predicate func(item T) bool) bool {
	return lo.NoneBy(c, predicate)
}

func (c _Slice[T]) Find(predicate func(item T) bool) (T, bool) {
	return lo.Find(c, predicate)
}

func (c _Slice[T]) MinBy(less func(a, b T) bool) T {
	return lo.MinBy(c, less)
}

func (c _Slice[T]) MaxBy(greater func(a, b T) bool) T {
	return lo.MaxBy(c, greater)
}

// ---------------------------------------------------------------------------
// _Slice2[T, E] implementations — the underlying slice is []E (raw message
// pointer type) while the interface exposes T (the projected _Const view).
// Each method wraps the predicate / comparison in a closure that applies
// AsConst() on the way in, then delegates to lo for the actual loop.
// ---------------------------------------------------------------------------

func (c _Slice2[T, E]) ContainsBy(predicate func(item T) bool) bool {
	return lo.ContainsBy(c, func(e E) bool {
		return predicate(e.AsConst())
	})
}

func (c _Slice2[T, E]) CountBy(predicate func(item T) bool) int {
	return lo.CountBy(c, func(e E) bool {
		return predicate(e.AsConst())
	})
}

func (c _Slice2[T, E]) EveryBy(predicate func(item T) bool) bool {
	return lo.EveryBy(c, func(e E) bool {
		return predicate(e.AsConst())
	})
}

func (c _Slice2[T, E]) NoneBy(predicate func(item T) bool) bool {
	return lo.NoneBy(c, func(e E) bool {
		return predicate(e.AsConst())
	})
}

func (c _Slice2[T, E]) Find(predicate func(item T) bool) (T, bool) {
	e, ok := lo.Find(c, func(e E) bool {
		return predicate(e.AsConst())
	})
	if !ok {
		var zero T
		return zero, false
	}
	return e.AsConst(), true
}

func (c _Slice2[T, E]) MinBy(less func(a, b T) bool) T {
	e := lo.MinBy(c, func(a, b E) bool {
		return less(a.AsConst(), b.AsConst())
	})
	return e.AsConst()
}

func (c _Slice2[T, E]) MaxBy(greater func(a, b T) bool) T {
	e := lo.MaxBy(c, func(a, b E) bool {
		return greater(a.AsConst(), b.AsConst())
	})
	return e.AsConst()
}
