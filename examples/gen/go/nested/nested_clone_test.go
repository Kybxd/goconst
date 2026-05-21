// Tests for Slice2.Clone and Map2.Clone — the message-bearing
// collection views — using real generated message types so the
// per-element proto.Clone path runs against the same E = *Foo storage
// shape callers see in production.
package nested

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

// -----------------------------------------------------------------
// Slice2.Clone — independent header + per-element proto.Clone.
// -----------------------------------------------------------------

func TestSlice2_Clone_DeepCopiesElements(t *testing.T) {
	src := []*Address{
		{Street: "1 Main", City: "A", Zip: "00001"},
		{Street: "2 Oak", City: "B", Zip: "00002"},
	}
	view := (&Person{PrevAddresses: src}).AsConst().GetPrevAddresses()

	got := view.Clone()
	if len(got) != len(src) {
		t.Fatalf("Clone len = %d, want %d", len(got), len(src))
	}
	for i := range got {
		if !proto.Equal(got[i], src[i]) {
			t.Errorf("Clone[%d] not proto.Equal to src[%d]", i, i)
		}
		if got[i] == src[i] {
			t.Errorf("Clone[%d] aliases src[%d]; want deep copy", i, i)
		}
	}

	// Mutate the clone's first element; the view's first element
	// must not change.
	got[0].Street = "MUTATED"
	if view.At(0).GetStreet() != "1 Main" {
		t.Fatalf("view.At(0).Street = %q after mutating clone, want %q",
			view.At(0).GetStreet(), "1 Main")
	}
}

func TestSlice2_Clone_NilSlice(t *testing.T) {
	view := (&Person{}).AsConst().GetPrevAddresses()
	got := view.Clone()
	if got != nil {
		t.Fatalf("Clone of nil-backed Slice2 = %v, want nil", got)
	}
}

// -----------------------------------------------------------------
// Map2.Clone — independent header + per-value proto.Clone.
// -----------------------------------------------------------------

func TestMap2_Clone_DeepCopiesValues(t *testing.T) {
	src := map[int64]*Address{
		1: {Street: "1 Main", City: "A", Zip: "00001"},
		2: {Street: "2 Oak", City: "B", Zip: "00002"},
	}
	view := (&Person{AddressBook: src}).AsConst().GetAddressBook()

	got := view.Clone()
	if len(got) != len(src) {
		t.Fatalf("Clone len = %d, want %d", len(got), len(src))
	}
	for k, v := range got {
		if !proto.Equal(v, src[k]) {
			t.Errorf("Clone[%d] not proto.Equal to src[%d]", k, k)
		}
		if v == src[k] {
			t.Errorf("Clone[%d] aliases src[%d]; want deep copy", k, k)
		}
	}

	// Mutate the clone's "1" value; the view's value must not change.
	got[1].Street = "MUTATED"
	if vv, _ := view.Get(1); vv.GetStreet() != "1 Main" {
		t.Fatalf("view.Get(1).Street = %q after mutating clone, want %q",
			vv.GetStreet(), "1 Main")
	}
}

func TestMap2_Clone_NilMap(t *testing.T) {
	view := (&Person{}).AsConst().GetAddressBook()
	got := view.Clone()
	if got != nil {
		t.Fatalf("Clone of nil-backed Map2 = %v, want nil", got)
	}
}
