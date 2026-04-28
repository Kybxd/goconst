// Unit tests and benchmarks for the generated *_Const views in the nested
// package. Exercises singular message fields, repeated scalar / message
// fields, map with scalar / message value, and a nested message type.
package nested

import (
	"testing"

	goconst "github.com/Kybxd/goconst"
)

// Compile-time assertions: every generated struct must satisfy both its
// *_Const interface and goconst.Constable[<Iface>].
var (
	_ Address_Const                           = (*Address)(nil).AsConst()
	_ Person_Const                            = (*Person)(nil).AsConst()
	_ Person_Contact_Const                    = (*Person_Contact)(nil).AsConst()
	_ goconst.Constable[Address_Const]        = (*Address)(nil)
	_ goconst.Constable[Person_Const]         = (*Person)(nil)
	_ goconst.Constable[Person_Contact_Const] = (*Person_Contact)(nil)
)

func newPerson() *Person {
	return &Person{
		Name: "alice",
		Age:  30,
		Home: &Address{Street: "Main 1", City: "SF", Zip: "94101"},
		Tags: []string{"a", "b", "c"},
		PrevAddresses: []*Address{
			{Street: "Old 1", City: "NYC", Zip: "10001"},
			{Street: "Old 2", City: "LA", Zip: "90001"},
		},
		Attributes: map[string]string{
			"role": "admin",
			"team": "infra",
		},
		AddressBook: map[int64]*Address{
			1: {Street: "Book 1", City: "SF"},
			2: {Street: "Book 2", City: "NYC"},
		},
		Contact: &Person_Contact{
			Email:  "a@x.com",
			Phones: []string{"111", "222"},
			Locations: map[string]*Address{
				"home": {Street: "Home 1", City: "SF"},
				"work": {Street: "Work 1", City: "SF"},
			},
		},
	}
}

// TestAddress_AsConst verifies the simplest projection: no slices, no maps.
func TestAddress_AsConst(t *testing.T) {
	a := &Address{Street: "s", City: "c", Zip: "z"}
	c := a.AsConst()
	if c.GetStreet() != "s" || c.GetCity() != "c" || c.GetZip() != "z" {
		t.Fatalf("Address_Const getters mismatch: %q / %q / %q",
			c.GetStreet(), c.GetCity(), c.GetZip())
	}
}

// TestPerson_SingularMessage checks that ConstHome() on the _Const view
// returns an Address_Const whose concrete data matches the backing Address.
func TestPerson_SingularMessage(t *testing.T) {
	p := newPerson()
	c := p.AsConst()
	home := c.ConstHome()
	if home == nil {
		t.Fatal("ConstHome() returned nil _Const view")
	}
	if home.GetStreet() != "Main 1" || home.GetCity() != "SF" || home.GetZip() != "94101" {
		t.Fatalf("Home mismatch: %+v", home)
	}
}

// TestPerson_NilSingularMessage confirms that calling AsConst() on a nil
// child pointer (common proto3 case) does not panic and that callers can
// still invoke scalar getters, which return zero values.
func TestPerson_NilSingularMessage(t *testing.T) {
	p := &Person{Name: "no-home"}
	c := p.AsConst()
	home := c.ConstHome()
	if home == nil {
		t.Fatal("ConstHome() on missing field must return a typed non-nil view")
	}
	if got := home.GetStreet(); got != "" {
		t.Errorf("Street on nil-backed Home: got %q, want \"\"", got)
	}
}

// TestPerson_RepeatedScalar covers goconst.NewSlice for a []string field,
// exposed through ConstTags() under the direct-style API.
func TestPerson_RepeatedScalar(t *testing.T) {
	c := newPerson().AsConst()
	s := c.ConstTags()
	if got := s.Len(); got != 3 {
		t.Fatalf("Tags.Len: got %d want 3", got)
	}
	if got := s.At(1); got != "b" {
		t.Errorf("Tags.At(1): got %q want %q", got, "b")
	}

	var collected []string
	for i, v := range s.All() {
		if i != len(collected) {
			t.Errorf("Tags.All index: got %d want %d", i, len(collected))
		}
		collected = append(collected, v)
	}
	if len(collected) != 3 || collected[0] != "a" || collected[2] != "c" {
		t.Errorf("Tags.All order: got %v", collected)
	}
}

// TestPerson_RepeatedMessage covers goconst.NewSlice2: every element is
// projected through AsConst, so At(i) returns Address_Const (not *Address).
// Exposed through ConstPrevAddresses() under the direct-style API.
func TestPerson_RepeatedMessage(t *testing.T) {
	c := newPerson().AsConst()
	s := c.ConstPrevAddresses()

	// Static type assertion: the getter must return Slice[Address_Const].
	var _ goconst.Slice[Address_Const] = s

	if got := s.Len(); got != 2 {
		t.Fatalf("PrevAddresses.Len: got %d want 2", got)
	}
	first := s.At(0)
	if first.GetCity() != "NYC" {
		t.Errorf("PrevAddresses[0].City: got %q want NYC", first.GetCity())
	}

	seen := 0
	for _, a := range s.All() {
		seen++
		_ = a.GetStreet() // must not panic
	}
	if seen != 2 {
		t.Errorf("PrevAddresses.All yielded %d items, want 2", seen)
	}
}

// TestPerson_MapScalar covers goconst.NewMap for map<string,string>,
// exposed through ConstAttributes() under the direct-style API.
func TestPerson_MapScalar(t *testing.T) {
	c := newPerson().AsConst()
	m := c.ConstAttributes()

	if got := m.Len(); got != 2 {
		t.Fatalf("Attributes.Len: got %d want 2", got)
	}
	v, ok := m.Get("role")
	if !ok || v != "admin" {
		t.Errorf("Attributes[role]: got (%q,%v) want (admin,true)", v, ok)
	}
	if _, ok := m.Get("missing"); ok {
		t.Errorf("Attributes[missing]: ok=true, want false")
	}
	if m.Has("missing") {
		t.Error("Attributes.Has(missing) = true, want false")
	}
	if !m.Has("team") {
		t.Error("Attributes.Has(team) = false, want true")
	}

	seen := map[string]string{}
	for k, v := range m.All() {
		seen[k] = v
	}
	if len(seen) != 2 || seen["role"] != "admin" || seen["team"] != "infra" {
		t.Errorf("Attributes.All: got %v", seen)
	}
}

// TestPerson_MapMessage covers goconst.NewMap2 for map<int64,Address>:
// values come out as Address_Const, concrete Address is not exposed.
// Exposed through ConstAddressBook() under the direct-style API.
func TestPerson_MapMessage(t *testing.T) {
	c := newPerson().AsConst()
	m := c.ConstAddressBook()

	// Static type assertion: value type must be the _Const interface.
	var _ goconst.Map[int64, Address_Const] = m

	if got := m.Len(); got != 2 {
		t.Fatalf("AddressBook.Len: got %d want 2", got)
	}
	v, ok := m.Get(1)
	if !ok {
		t.Fatal("AddressBook[1]: missing")
	}
	if v.GetCity() != "SF" {
		t.Errorf("AddressBook[1].City: got %q want SF", v.GetCity())
	}

	zero, ok := m.Get(99)
	if ok {
		t.Error("AddressBook[99]: ok=true, want false")
	}
	// Per NewMap2 contract, zero value for a missing key is an interface zero (nil).
	if zero != nil {
		t.Errorf("AddressBook[99] zero: got %v, want nil interface", zero)
	}
}

// TestPerson_NestedType verifies recursion into nested message type
// Person_Contact, including its own repeated and map with message value.
func TestPerson_NestedType(t *testing.T) {
	c := newPerson().AsConst()
	contact := c.ConstContact()
	if contact == nil {
		t.Fatal("Contact: nil _Const")
	}
	if contact.GetEmail() != "a@x.com" {
		t.Errorf("Contact.Email: got %q", contact.GetEmail())
	}
	if contact.ConstPhones().Len() != 2 {
		t.Errorf("Contact.Phones.Len: got %d want 2", contact.ConstPhones().Len())
	}

	locs := contact.ConstLocations()
	var _ goconst.Map[string, Address_Const] = locs
	if locs.Len() != 2 {
		t.Fatalf("Contact.Locations.Len: got %d want 2", locs.Len())
	}
	home, ok := locs.Get("home")
	if !ok || home.GetStreet() != "Home 1" {
		t.Errorf("Contact.Locations[home]: got (%+v,%v)", home, ok)
	}
}

// ---- Benchmarks ------------------------------------------------------------

var benchNestedSink any

func BenchmarkNested_NewSlice_Scalar(b *testing.B) {
	c := newPerson().AsConst()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchNestedSink = c.ConstTags()
	}
}

func BenchmarkNested_NewSlice2_Message(b *testing.B) {
	c := newPerson().AsConst()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchNestedSink = c.ConstPrevAddresses()
	}
}

func BenchmarkNested_NewMap_Scalar(b *testing.B) {
	c := newPerson().AsConst()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchNestedSink = c.ConstAttributes()
	}
}

func BenchmarkNested_NewMap2_Message(b *testing.B) {
	c := newPerson().AsConst()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchNestedSink = c.ConstAddressBook()
	}
}

// Compare native slice range vs Slice[T].All() iteration.
func BenchmarkNested_Iter_TagsRaw(b *testing.B) {
	p := newPerson()
	b.ReportAllocs()
	b.ResetTimer()
	var n int
	for i := 0; i < b.N; i++ {
		for range p.Tags {
			n++
		}
	}
	benchNestedSink = n
}

func BenchmarkNested_Iter_TagsViaAll(b *testing.B) {
	c := newPerson().AsConst()
	s := c.ConstTags()
	b.ReportAllocs()
	b.ResetTimer()
	var n int
	for i := 0; i < b.N; i++ {
		for range s.All() {
			n++
		}
	}
	benchNestedSink = n
}

func BenchmarkNested_Iter_PrevAddressesViaAll(b *testing.B) {
	c := newPerson().AsConst()
	s := c.ConstPrevAddresses()
	b.ReportAllocs()
	b.ResetTimer()
	var n int
	for i := 0; i < b.N; i++ {
		for _, a := range s.All() {
			_ = a.GetStreet()
			n++
		}
	}
	benchNestedSink = n
}

func BenchmarkNested_Map_GetHit(b *testing.B) {
	m := newPerson().AsConst().ConstAddressBook()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v, _ := m.Get(1)
		benchNestedSink = v
	}
}
