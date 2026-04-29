// Tests asserting that goconst.Slice / goconst.Map default implementations
// satisfy fmt.Stringer and that their output is bit-for-bit identical to
// fmt.Sprint on the underlying []T / map[K]V — including the rich
// prototext-style output that protobuf messages produce out of the box for
// NewSlice2 / NewMap2.
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
	s := p.AsConst().ConstTags()

	// Compile-time check: the default impl must satisfy fmt.Stringer.
	var _ fmt.Stringer = s.(fmt.Stringer)

	got := fmt.Sprint(s)
	want := fmt.Sprint(p.Tags)
	if got != want {
		t.Fatalf("Slice[string].String() mismatch:\n got = %q\nwant = %q", got, want)
	}
}

// TestPerson_SliceStringer_Message asserts that printing the Slice view of
// a repeated message field ([]*Address) produces the same text as printing
// the raw []*Address — so callers get the full prototext-style rendering
// of every proto.Message element out of the box, no slices.Collect or
// AsConst hop needed.
func TestPerson_SliceStringer_Message(t *testing.T) {
	p := newPerson()
	s := p.AsConst().ConstPrevAddresses()

	var _ fmt.Stringer = s.(fmt.Stringer)

	got := fmt.Sprint(s)
	want := fmt.Sprint(p.PrevAddresses)
	if got != want {
		t.Fatalf("Slice[Address_Const].String() must match fmt.Sprint([]*Address):\n got = %q\nwant = %q", got, want)
	}
}

// TestPerson_MapStringer_Scalar asserts that printing the Map view of a
// map<string,string> field produces the same text as printing the raw
// map[string]string.
func TestPerson_MapStringer_Scalar(t *testing.T) {
	p := newPerson()
	m := p.AsConst().ConstAttributes()

	var _ fmt.Stringer = m.(fmt.Stringer)

	got := fmt.Sprint(m)
	want := fmt.Sprint(p.Attributes)
	if got != want {
		t.Fatalf("Map[string,string].String() mismatch:\n got = %q\nwant = %q", got, want)
	}
}

// TestPerson_MapStringer_Message asserts that printing the Map view of a
// map<int64,Address> field produces the same text as printing the raw
// map[int64]*Address — again preserving proto.Message's own String().
func TestPerson_MapStringer_Message(t *testing.T) {
	p := newPerson()
	m := p.AsConst().ConstAddressBook()

	var _ fmt.Stringer = m.(fmt.Stringer)

	got := fmt.Sprint(m)
	want := fmt.Sprint(p.AddressBook)
	if got != want {
		t.Fatalf("Map[int64,Address_Const].String() must match fmt.Sprint(map[int64]*Address):\n got = %q\nwant = %q", got, want)
	}
}

// TestStringer_InterfaceDispatch verifies that fmt's Stringer detection
// fires through the goconst.Slice / goconst.Map *interface* types too —
// i.e. the String() method is visible on the dynamic type, not just on
// the concrete impl. This is what makes "fmt.Println(s)" work without
// callers having to type-assert first.
func TestStringer_InterfaceDispatch(t *testing.T) {
	p := newPerson()

	var sliceIface goconst.Slice[Address_Const] = p.AsConst().ConstPrevAddresses()
	gotSlice := fmt.Sprint(sliceIface)
	wantSlice := fmt.Sprint(p.PrevAddresses)
	if gotSlice != wantSlice {
		t.Fatalf("fmt.Sprint via Slice interface:\n got = %q\nwant = %q", gotSlice, wantSlice)
	}

	var mapIface goconst.Map[int64, Address_Const] = p.AsConst().ConstAddressBook()
	gotMap := fmt.Sprint(mapIface)
	wantMap := fmt.Sprint(p.AddressBook)
	if gotMap != wantMap {
		t.Fatalf("fmt.Sprint via Map interface:\n got = %q\nwant = %q", gotMap, wantMap)
	}
}
