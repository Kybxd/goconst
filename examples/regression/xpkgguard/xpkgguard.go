// Package xpkgguard is the cross-package half of the golang/go#79711
// regression, complementing ../aliascycle.
//
// It imports the views generated from
// ../../proto/xpkgpointer/xpkgpointer.proto, whose map getters return
// *another package's* generic alias (recursive.SelfMap_ConstMap[int64])
// because their edge is not on a cycle — even though the alias's type
// arguments are cycle members.
//
// That shape is precisely what the generator's per-file cycle graph
// relies on being safe: cycles cannot span .proto files (protoc
// rejects cyclic imports), so an edge leaving a file is never on a
// cycle and keeps the short alias. If that reasoning were wrong, or if
// the generator started expanding these edges unnecessarily, this
// package is where it would show up — the former as a compiler
// deadlock, the latter as an unexplained diff in the golden output.
package xpkgguard

import (
	"github.com/Kybxd/goconst/examples/gen/go/recursive"
	"github.com/Kybxd/goconst/examples/gen/go/xpkgpointer"
)

// Widths touches both cross-package alias-returning getters.
func Widths(c xpkgpointer.Holder_Const) int {
	return c.GetNodes().Len() + c.GetMutuals().Len() + c.GetHistory().Len()
}

// Chained reaches through the alias into the value type's *own* cyclic
// getter, which does carry the expansion — the step that makes the
// compiler's walk terminate.
func Chained(c xpkgpointer.Holder_Const) int {
	n, ok := c.GetNodes().Get(1)
	if !ok {
		return 0
	}
	return n.GetChildren().Len()
}

// AliasTyped pins that another package's alias is usable as an
// ordinary type here, not just as an inferred return value.
func AliasTyped(nodes recursive.SelfMap_ConstMap[int64]) int {
	return nodes.Len()
}
