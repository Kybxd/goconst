package aliascycle

import (
	"testing"

	"github.com/Kybxd/goconst/examples/gen/go/recursive"
)

// The real regression is that this file *compiles* at all; the
// assertions below additionally pin the runtime behaviour of views
// over recursive messages.

func TestSelfMapCycle(t *testing.T) {
	m := &recursive.SelfMap{
		Id: "root",
		Children: map[int64]*recursive.SelfMap{
			1: {Id: "c1", Children: map[int64]*recursive.SelfMap{11: {Id: "c11"}}},
		},
		Flags:    map[int64]bool{7: true},
		Siblings: []*recursive.SelfMap{{Id: "s1"}},
		Parent:   &recursive.SelfMap{Id: "p"},
	}
	c := m.AsConst()

	if got := SelfMapChildIDs(c); len(got) != 1 || got[0] != "c1" {
		t.Fatalf("child ids = %v", got)
	}

	child, ok := c.GetChildren().Get(1)
	if !ok {
		t.Fatal("child 1 missing")
	}
	grand, ok := child.GetChildren().Get(11)
	if !ok || grand.GetId() != "c11" {
		t.Fatalf("grandchild = %q, ok=%v", grand.GetId(), ok)
	}

	flags, siblings, parent := SelfMapNonCyclic(c)
	if flags != 1 || siblings != 1 || parent != "p" {
		t.Fatalf("non-cyclic shapes = %d %d %q", flags, siblings, parent)
	}

	// Miss on a recursive map yields a nil-backed view, still readable.
	miss, ok := c.GetChildren().Get(999)
	if ok || !miss.IsNil() || miss.GetId() != "" {
		t.Fatalf("miss = %v ok=%v", miss, ok)
	}

	// Clone must detach the whole recursive subtree.
	cloned := c.GetChildren().Clone()
	cloned[1].Children[11].Id = "mutated"
	if grand.GetId() != "c11" {
		t.Fatal("clone aliased the original subtree")
	}
}

func TestNilBackedRecursiveChain(t *testing.T) {
	var nilMap *recursive.SelfMap
	c := nilMap.AsConst()

	if !c.IsNil() || c.GetChildren().Len() != 0 || !c.GetChildren().IsNil() {
		t.Fatal("nil-backed view should read as empty")
	}
	if !c.GetParent().GetParent().IsNil() {
		t.Fatal("nil chain should stay nil")
	}
}

func TestMutualAndNestedCycles(t *testing.T) {
	a := &recursive.MutualA{
		Bs: map[int64]*recursive.MutualB{
			1: {As: map[int64]*recursive.MutualA{2: {}, 3: {}}},
		},
	}
	if got := MutualDepth(a.AsConst()); got != 2 {
		t.Fatalf("mutual depth = %d", got)
	}

	n := &recursive.NestedSelf{
		Inner: &recursive.NestedSelf_Inner{
			Outers: map[string]*recursive.NestedSelf{"o": {}},
			Inners: map[string]*recursive.NestedSelf_Inner{"i": {}},
		},
	}
	if got := NestedSelfWidth(n.AsConst()); got != 2 {
		t.Fatalf("nested width = %d", got)
	}
}

// TestPointsAtCycle covers the precision case: getters that keep the
// short alias because their edge is not on a cycle, even though the
// value type is a cycle member. Behaviour must be identical to the
// expanded form.
func TestPointsAtCycle(t *testing.T) {
	p := &recursive.PointsAtCycle{
		Nodes: map[int64]*recursive.SelfMap{
			1: {Id: "n1", Children: map[int64]*recursive.SelfMap{11: {Id: "n11"}}},
		},
		Mutuals: map[int64]*recursive.MutualA{
			1: {Bs: map[int64]*recursive.MutualB{2: {}}},
		},
	}
	c := p.AsConst()

	if got := PointsAtCycleWidth(c); got != 2 {
		t.Fatalf("width = %d", got)
	}

	// The alias-returning getter still projects to a _Const view and
	// still reaches the expanded, cyclic getter underneath it.
	node, ok := c.GetNodes().Get(1)
	if !ok || node.GetId() != "n1" {
		t.Fatalf("node = %q ok=%v", node.GetId(), ok)
	}
	grand, ok := node.GetChildren().Get(11)
	if !ok || grand.GetId() != "n11" {
		t.Fatalf("grandchild = %q ok=%v", grand.GetId(), ok)
	}

	// Aliases are usable as plain types from a consumer package.
	if got := PointsAtCycleAliasTypes(c.GetNodes(), recursive.NestedSelf_ConstMap[string]{}); got != 1 {
		t.Fatalf("alias-typed params = %d", got)
	}

	// Miss on an alias-form map is equally nil-backed and safe.
	miss, ok := c.GetNodes().Get(999)
	if ok || !miss.IsNil() || miss.GetId() != "" {
		t.Fatalf("miss = %v ok=%v", miss, ok)
	}
}
