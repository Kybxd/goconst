// Tests asserting that goconst.Slice / goconst.Slice2 / goconst.Map /
// goconst.Map2 default implementations satisfy fmt.Stringer and that
// their output is bit-for-bit identical to fmt.Sprint on the underlying
// []T / map[K]V — including the rich prototext-style output that
// protobuf messages produce out of the box for NewSlice2 / NewMap2.
package nested

import (
	"fmt"
	"testing"

	goconst "github.com/Kybxd/goconst"
)

// TestPerson_SliceStringer_Scalar asserts that printing the Slice view of
// a []string field produces the same text as printing the raw []string.
func TestPerson_SliceStringer_Scalar(t *testing.T) {
	p := newPerson()
	s := p.AsConst().GetTags()

	// Compile-time check: the default impl must satisfy fmt.Stringer.
	// (Direct assignment rather than a type assertion, because the
	// collection views are now concrete struct types — a struct with
	// a String() method implements fmt.Stringer implicitly.)
	var _ fmt.Stringer = s

	got := fmt.Sprint(s)
	want := fmt.Sprint(p.Tags)
	if got != want {
		t.Fatalf("Slice[string].String() mismatch:\n got = %q\nwant = %q", got, want)
	}
}

// TestPerson_SliceStringer_Message asserts that printing the Slice2 view of
// a repeated message field ([]*Address) produces the same text as printing
// the raw []*Address — so callers get the full prototext-style rendering
// of every proto.Message element out of the box, no slices.Collect or
// AsConst hop needed.
func TestPerson_SliceStringer_Message(t *testing.T) {
	p := newPerson()
	s := p.AsConst().GetPrevAddresses()

	var _ fmt.Stringer = s

	got := fmt.Sprint(s)
	want := fmt.Sprint(p.PrevAddresses)
	if got != want {
		t.Fatalf("Slice2[Address_Const, *Address].String() must match fmt.Sprint([]*Address):\n got = %q\nwant = %q", got, want)
	}
}

// TestPerson_MapStringer_Scalar asserts that printing the Map view of a
// map<string,string> field produces the same text as printing the raw
// map[string]string.
func TestPerson_MapStringer_Scalar(t *testing.T) {
	p := newPerson()
	m := p.AsConst().GetAttributes()

	var _ fmt.Stringer = m

	got := fmt.Sprint(m)
	want := fmt.Sprint(p.Attributes)
	if got != want {
		t.Fatalf("Map[string,string].String() mismatch:\n got = %q\nwant = %q", got, want)
	}
}

// TestPerson_MapStringer_Message asserts that printing the Map2 view of a
// map<int64,Address> field produces the same text as printing the raw
// map[int64]*Address — again preserving proto.Message's own String().
func TestPerson_MapStringer_Message(t *testing.T) {
	p := newPerson()
	m := p.AsConst().GetAddressBook()

	var _ fmt.Stringer = m

	got := fmt.Sprint(m)
	want := fmt.Sprint(p.AddressBook)
	if got != want {
		t.Fatalf("Map2[int64, Address_Const, *Address].String() must match fmt.Sprint(map[int64]*Address):\n got = %q\nwant = %q", got, want)
	}
}

// TestStringer_ConcreteDispatch verifies that fmt's Stringer detection
// fires on the collection view's concrete struct type — i.e. the
// String() method is visible on the dynamic type so "fmt.Println(s)"
// works without callers having to type-assert first.
//
// Historically this test lived under the name TestStringer_InterfaceDispatch
// and fixed a goconst.Slice / goconst.Map *interface* variable to
// exercise the method-set lookup through the interface boundary. That
// shape is no longer applicable: Slice / Slice2 / Map / Map2 are now
// concrete struct types, so the only dispatch that could happen is via
// fmt's reflection-based Stringer check — which is what we verify here.
func TestStringer_ConcreteDispatch(t *testing.T) {
	p := newPerson()

	var slc goconst.Slice2[Address_Const, *Address] = p.AsConst().GetPrevAddresses()
	gotSlice := fmt.Sprint(slc)
	wantSlice := fmt.Sprint(p.PrevAddresses)
	if gotSlice != wantSlice {
		t.Fatalf("fmt.Sprint via Slice2 concrete type:\n got = %q\nwant = %q", gotSlice, wantSlice)
	}

	var mp goconst.Map2[int64, Address_Const, *Address] = p.AsConst().GetAddressBook()
	gotMap := fmt.Sprint(mp)
	wantMap := fmt.Sprint(p.AddressBook)
	if gotMap != wantMap {
		t.Fatalf("fmt.Sprint via Map2 concrete type:\n got = %q\nwant = %q", gotMap, wantMap)
	}
}
