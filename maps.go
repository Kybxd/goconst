// Package goconst: map algorithm extensions.
//
// This file isolates the "algorithms on read-only maps" method set from
// the bare view methods (Len / Get / Has / All) defined in goconst.go.
// Splitting them keeps the core [Map] contract minimal while leaving an
// obvious place to grow the algorithm surface over time, symmetric with
// [SliceAlgo] in slices.go.
//
// Method selection rule: only include algorithms from github.com/samber/lo that
//   - are strictly read-only (no mutation of the underlying map), and
//   - do not require an extra type parameter on the method itself (Go
//     disallows method-level type parameters on interfaces), and
//   - do not require the value type V to be comparable.
package goconst

import "github.com/samber/lo"

// MapAlgo is the read-only algorithm method set implemented by every [Map]
// view. It mirrors a curated subset of [github.com/samber/lo], restricted
// to methods that are read-only and expressible under the interface's
// fixed type parameters (K comparable, V any).
type MapAlgo[K comparable, V any] interface {
	// Keys returns a snapshot of all keys in unspecified order. The
	// returned slice is freshly allocated and owned by the caller.
	// Analogue of [lo.Keys] (single-map variant).
	Keys() []K

	// Values returns a snapshot of all values in unspecified order. The
	// returned slice is freshly allocated and owned by the caller.
	// Analogue of [lo.Values] (single-map variant).
	Values() []V
}

// ---------------------------------------------------------------------------
// _Map[K, V] implementations — delegate directly to lo since the underlying
// type is exactly map[K]V.
// ---------------------------------------------------------------------------

func (c _Map[K, V]) Keys() []K {
	return lo.Keys(c)
}

func (c _Map[K, V]) Values() []V {
	return lo.Values(c)
}

// ---------------------------------------------------------------------------
// _Map2[K, V, E] implementations — the underlying map is map[K]E (raw
// message pointer value type) while the interface exposes V (the projected
// _Const view). Keys pass through unchanged; values are projected via
// AsConst on the way out.
// ---------------------------------------------------------------------------

func (c _Map2[K, V, E]) Keys() []K {
	return lo.Keys(c)
}

func (c _Map2[K, V, E]) Values() []V {
	return lo.Map(lo.Values(c), func(e E, _ int) V {
		return e.AsConst()
	})
}
