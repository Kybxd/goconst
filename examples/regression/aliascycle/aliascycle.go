// Package aliascycle is the cross-package half of the golang/go#79711
// regression pinned by ../../proto/recursive/recursive.proto.
//
// The compiler bug only fires when a package *other* than the
// defining one imports a _Const view whose method signature
// instantiates a generic alias with cyclic type arguments. The
// defining package (gen/go/recursive) compiles cleanly either way, so
// a test living next to the generated code would not catch a
// regression. This package exists purely to be a separate compilation
// unit that imports and touches those views.
//
// If the generator ever reverts to emitting <Msg>_ConstMap[K] in map
// getter signatures, building this package fails with:
//
//	fatal error: all goroutines are asleep - deadlock!
//	cmd/compile/internal/types2.(*Named).unpack(...)
//
// Note this is a hard compiler crash, not a type error — `go vet` and
// gopls are unaffected, so only an actual build catches it.
package aliascycle

import (
	"github.com/Kybxd/goconst/examples/gen/go/recursive"
)

// SelfMapChildIDs exercises the direct map<K, Self> cycle.
func SelfMapChildIDs(c recursive.SelfMap_Const) []string {
	out := make([]string, 0, c.GetChildren().Len())
	for _, child := range c.GetChildren().All() {
		out = append(out, child.GetId())
	}
	return out
}

// SelfMapNonCyclic touches the sibling field shapes that keep their
// short alias / plain forms, pinning that the fix is not over-applied.
func SelfMapNonCyclic(c recursive.SelfMap_Const) (int, int, string) {
	return c.GetFlags().Len(), c.GetSiblings().Len(), c.GetParent().GetId()
}

// MutualDepth exercises the indirect MutualA → MutualB → MutualA cycle.
func MutualDepth(c recursive.MutualA_Const) int {
	n := 0
	for _, b := range c.GetBs().All() {
		n += b.GetAs().Len()
	}
	return n
}

// NestedSelfWidth exercises a nested type whose map points back at the
// enclosing message.
func NestedSelfWidth(c recursive.NestedSelf_Const) int {
	return c.GetInner().GetOuters().Len() + c.GetInner().GetInners().Len()
}

// PointsAtCycleWidth exercises the precision case: these getters keep
// the short <Msg>_ConstMap[K] alias because the edge is not itself on
// a cycle, even though the *value* types are cycle members. Touching
// them from this package is what proves the alias is genuinely safe
// there — instantiating the alias only re-enters the value type's
// unpack, and the value type's own cyclic getters use the expansion,
// so the walk terminates.
func PointsAtCycleWidth(c recursive.PointsAtCycle_Const) int {
	return c.GetNodes().Len() + c.GetMutuals().Len()
}

// PointsAtCycleAliasTypes pins that the aliases are usable as ordinary
// types in a consumer package, not just as inferred return values.
func PointsAtCycleAliasTypes(
	nodes recursive.SelfMap_ConstMap[int64],
	outers recursive.NestedSelf_ConstMap[string],
) int {
	return nodes.Len() + outers.Len()
}
