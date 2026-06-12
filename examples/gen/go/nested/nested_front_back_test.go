// Tests for Slice.Front / Slice.Back and Slice2.Front / Slice2.Back
// using real generated message types so the AsConst() projection on
// the Slice2 path runs against the same E = *Foo storage shape callers
// see in production.
package nested

import (
	"testing"
)

// -----------------------------------------------------------------
// Slice.Front / Slice.Back via Person.Tags (repeated string).
// -----------------------------------------------------------------

func TestSlice_Front_Back_OnGeneratedView(t *testing.T) {
	p := &Person{Tags: []string{"a", "b", "c"}}
	s := p.AsConst().GetTags()

	if got, want := s.Front(), "a"; got != want {
		t.Errorf("Tags.Front() = %q, want %q", got, want)
	}
	if got, want := s.Back(), "c"; got != want {
		t.Errorf("Tags.Back() = %q, want %q", got, want)
	}
}

func TestSlice_Front_PanicsOnEmpty_OnGeneratedView(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("Tags.Front() on empty Slice did not panic")
		}
	}()
	_ = (&Person{}).AsConst().GetTags().Front()
}

func TestSlice_Back_PanicsOnEmpty_OnGeneratedView(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("Tags.Back() on empty Slice did not panic")
		}
	}()
	_ = (&Person{}).AsConst().GetTags().Back()
}

// -----------------------------------------------------------------
// Slice2.Front / Slice2.Back via Person.PrevAddresses (repeated
// message). Pins that Front/Back project through AsConst() just like
// At, so callers see Address_Const, not *Address.
// -----------------------------------------------------------------

func TestSlice2_Front_Back_OnGeneratedView(t *testing.T) {
	src := []*Address{
		{Street: "1 Main", City: "A", Zip: "00001"},
		{Street: "2 Oak", City: "B", Zip: "00002"},
		{Street: "3 Pine", City: "C", Zip: "00003"},
	}
	view := (&Person{PrevAddresses: src}).AsConst().GetPrevAddresses()

	if got, want := view.Front().GetStreet(), "1 Main"; got != want {
		t.Errorf("PrevAddresses.Front().GetStreet() = %q, want %q", got, want)
	}
	if got, want := view.Back().GetStreet(), "3 Pine"; got != want {
		t.Errorf("PrevAddresses.Back().GetStreet() = %q, want %q", got, want)
	}

	// And the static return type is the wrapper view, not the
	// concrete pointer — confirmed by exercising a *_Const-only
	// method on the result.
	if view.Front().IsNil() {
		t.Errorf("PrevAddresses.Front().IsNil() = true, want false")
	}
}

func TestSlice2_Front_PanicsOnEmpty_OnGeneratedView(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("PrevAddresses.Front() on empty Slice2 did not panic")
		}
	}()
	_ = (&Person{}).AsConst().GetPrevAddresses().Front()
}

func TestSlice2_Back_PanicsOnEmpty_OnGeneratedView(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("PrevAddresses.Back() on empty Slice2 did not panic")
		}
	}()
	_ = (&Person{}).AsConst().GetPrevAddresses().Back()
}
