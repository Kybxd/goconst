package xpkgguard

import (
	"testing"

	"github.com/Kybxd/goconst/examples/gen/go/recursive"
	"github.com/Kybxd/goconst/examples/gen/go/xpkgpointer"
)

// Compiling this package at all is the regression; the assertions
// additionally pin that cross-package alias getters behave exactly
// like the expanded ones.

func TestCrossPackagePointsAtCycle(t *testing.T) {
	h := &xpkgpointer.Holder{
		Nodes: map[int64]*recursive.SelfMap{
			1: {Id: "n1", Children: map[int64]*recursive.SelfMap{11: {Id: "n11"}}},
		},
		Mutuals: map[int64]*recursive.MutualA{
			1: {Bs: map[int64]*recursive.MutualB{2: {}}},
		},
		History: []*recursive.SelfMap{{Id: "h1"}},
	}
	c := h.AsConst()

	if got := Widths(c); got != 3 {
		t.Fatalf("widths = %d, want 3", got)
	}
	if got := Chained(c); got != 1 {
		t.Fatalf("chained = %d, want 1", got)
	}
	if got := AliasTyped(c.GetNodes()); got != 1 {
		t.Fatalf("alias-typed param = %d, want 1", got)
	}

	// Reaching through the alias must yield the same projected views
	// as any other map getter.
	node, ok := c.GetNodes().Get(1)
	if !ok || node.GetId() != "n1" {
		t.Fatalf("node = %q ok=%v", node.GetId(), ok)
	}
	grand, ok := node.GetChildren().Get(11)
	if !ok || grand.GetId() != "n11" {
		t.Fatalf("grandchild = %q ok=%v", grand.GetId(), ok)
	}
	if got := c.GetHistory().At(0).GetId(); got != "h1" {
		t.Fatalf("history[0] = %q", got)
	}

	// Misses stay nil-backed and safely readable.
	miss, ok := c.GetNodes().Get(999)
	if ok || !miss.IsNil() || miss.GetId() != "" {
		t.Fatalf("miss = %v ok=%v", miss, ok)
	}
}

func TestCrossPackageNilBacked(t *testing.T) {
	var nilHolder *xpkgpointer.Holder
	c := nilHolder.AsConst()

	if !c.IsNil() || c.GetNodes().Len() != 0 || c.GetMutuals().Len() != 0 {
		t.Fatal("nil-backed holder should read as empty")
	}
	// Reading through a miss on a nil-backed alias map is still safe.
	if v, ok := c.GetNodes().Get(1); ok || !v.IsNil() {
		t.Fatal("nil-backed miss should be nil-backed")
	}
}
