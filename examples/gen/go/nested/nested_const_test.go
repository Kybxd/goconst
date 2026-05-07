// Unit tests and benchmarks for the generated *_Const views in the nested
// package. Exercises singular message fields, repeated scalar / message
// fields, map with scalar / message value, and a nested message type.
package nested

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	goconst "github.com/Kybxd/goconst"
)

// Compile-time assertions: every concrete *Message must be a
// goconst.Constable whose AsConst() produces the matching wrapper
// struct. Under the struct-wrapper scheme the view is a concrete
// struct (not an interface), so an "interface-satisfaction" assertion
// against *Address is no longer the right shape — we instead assert
// that the value returned by AsConst() is exactly the expected view
// type. A stray rename on either side fails the build here.
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

// TestPerson_SingularMessage checks that GetHome() on the _Const view
// returns an Address_Const whose concrete data matches the backing Address.
func TestPerson_SingularMessage(t *testing.T) {
	p := newPerson()
	c := p.AsConst()
	home := c.GetHome()
	if home.IsNil() {
		t.Fatal("GetHome() returned a nil-backed _Const view")
	}
	if home.GetStreet() != "Main 1" || home.GetCity() != "SF" || home.GetZip() != "94101" {
		t.Fatalf("Home mismatch: %+v", home)
	}
}

// TestPerson_NilSingularMessage confirms that calling AsConst() on a nil
// child pointer (common proto3 case) does not panic and that callers can
// still invoke scalar getters, which return zero values. Note that under
// the struct-wrapper scheme `home == nil` is a compile error — the view
// is always a concrete struct value — so the nil-ness question is asked
// via IsNil() instead.
func TestPerson_NilSingularMessage(t *testing.T) {
	p := &Person{Name: "no-home"}
	c := p.AsConst()
	home := c.GetHome()
	if !home.IsNil() {
		t.Fatal("GetHome() on missing field: IsNil() = false, want true")
	}
	if got := home.GetStreet(); got != "" {
		t.Errorf("Street on nil-backed Home: got %q, want \"\"", got)
	}
}

// TestPerson_RepeatedScalar covers goconst.NewSlice for a []string field,
// exposed through GetTags() under the direct-style API.
func TestPerson_RepeatedScalar(t *testing.T) {
	c := newPerson().AsConst()
	s := c.GetTags()
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
// Exposed through GetPrevAddresses() under the direct-style API.
func TestPerson_RepeatedMessage(t *testing.T) {
	c := newPerson().AsConst()
	s := c.GetPrevAddresses()

	// Static type assertion: the getter must return Slice2[Address_Const, *Address].
	var _ goconst.Slice2[Address_Const, *Address] = s

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
// exposed through GetAttributes() under the direct-style API.
func TestPerson_MapScalar(t *testing.T) {
	c := newPerson().AsConst()
	m := c.GetAttributes()

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
// Exposed through GetAddressBook() under the direct-style API.
func TestPerson_MapMessage(t *testing.T) {
	c := newPerson().AsConst()
	m := c.GetAddressBook()

	// Static type assertion: value type must be the Address_Const wrapper.
	var _ goconst.Map2[int64, Address_Const, *Address] = m

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
	// Per NewMap2 contract, the value returned on a miss is a nil-backed
	// view: the struct wrapper is always a real value (so `zero == nil`
	// would not even compile), scalar getters are safe to call on it,
	// and the authoritative presence flag is the second return value.
	// See also TestPerson_Map_Zero.
	if !zero.IsNil() {
		t.Error("AddressBook[99] zero.IsNil(): got false, want true")
	}
	if got := zero.GetCity(); got != "" {
		t.Errorf("AddressBook[99] zero.GetCity(): got %q, want \"\"", got)
	}
}

// TestPerson_NestedType verifies recursion into nested message type
// Person_Contact, including its own repeated and map with message value.
func TestPerson_NestedType(t *testing.T) {
	c := newPerson().AsConst()
	contact := c.GetContact()
	if contact.IsNil() {
		t.Fatal("Contact: nil-backed _Const view")
	}
	if contact.GetEmail() != "a@x.com" {
		t.Errorf("Contact.Email: got %q", contact.GetEmail())
	}
	if contact.GetPhones().Len() != 2 {
		t.Errorf("Contact.Phones.Len: got %d want 2", contact.GetPhones().Len())
	}

	locs := contact.GetLocations()
	var _ goconst.Map2[string, Address_Const, *Address] = locs
	if locs.Len() != 2 {
		t.Fatalf("Contact.Locations.Len: got %d want 2", locs.Len())
	}
	home, ok := locs.Get("home")
	if !ok || home.GetStreet() != "Home 1" {
		t.Errorf("Contact.Locations[home]: got (%+v,%v)", home, ok)
	}
}

// ---- Benchmarks ------------------------------------------------------------

// benchNestedSink is deliberately NOT `any`: assigning a concrete struct
// view (goconst.Slice / Slice2 / Map / Map2) to an `any`-typed sink would
// box it into an interface header, adding a 24 B / 1 alloc measurement
// artefact that has nothing to do with the view itself. Typed per-kind
// sinks avoid that.
var (
	benchNestedSinkSliceString  goconst.Slice[string]
	benchNestedSinkSlice2Addr   goconst.Slice2[Address_Const, *Address]
	benchNestedSinkMapScalar    goconst.Map[string, string]
	benchNestedSinkMap2Addr     goconst.Map2[int64, Address_Const, *Address]
	benchNestedSinkInt          int
	benchNestedSinkAddrConst    Address_Const
)

func BenchmarkNested_NewSlice_Scalar(b *testing.B) {
	c := newPerson().AsConst()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchNestedSinkSliceString = c.GetTags()
	}
}

func BenchmarkNested_NewSlice2_Message(b *testing.B) {
	c := newPerson().AsConst()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchNestedSinkSlice2Addr = c.GetPrevAddresses()
	}
}

func BenchmarkNested_NewMap_Scalar(b *testing.B) {
	c := newPerson().AsConst()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchNestedSinkMapScalar = c.GetAttributes()
	}
}

func BenchmarkNested_NewMap2_Message(b *testing.B) {
	c := newPerson().AsConst()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchNestedSinkMap2Addr = c.GetAddressBook()
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
	benchNestedSinkInt = n
}

func BenchmarkNested_Iter_TagsViaAll(b *testing.B) {
	c := newPerson().AsConst()
	s := c.GetTags()
	b.ReportAllocs()
	b.ResetTimer()
	var n int
	for i := 0; i < b.N; i++ {
		for range s.All() {
			n++
		}
	}
	benchNestedSinkInt = n
}

func BenchmarkNested_Iter_PrevAddressesViaAll(b *testing.B) {
	c := newPerson().AsConst()
	s := c.GetPrevAddresses()
	b.ReportAllocs()
	b.ResetTimer()
	var n int
	for i := 0; i < b.N; i++ {
		for _, a := range s.All() {
			_ = a.GetStreet()
			n++
		}
	}
	benchNestedSinkInt = n
}

func BenchmarkNested_Map_GetHit(b *testing.B) {
	m := newPerson().AsConst().GetAddressBook()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v, _ := m.Get(1)
		benchNestedSinkAddrConst = v
	}
}

// ---- iteration-overhead benchmark matrix -----------------------------------
//
// One Benchmark per data shape, three or four sub-benchmarks per shape,
// all driven through b.Run so a single
// `go test -bench=BenchmarkNested_Range -benchmem` invocation produces
// the full grid in one pass.
//
// Data shapes (rows):
//
//   1. scalar slice        []string                      ([]T,        NewSlice)
//   2. struct slice        []*Address                    ([]*M,       NewSlice2)
//   3. scalar-value map    map[string]string             (map[K]V,    NewMap)
//   4. struct-value map    map[int64]*Address            (map[K]*M,   NewMap2)
//
// Iteration strategies (sub-benchmarks):
//
//   A. Raw       — `for ... range raw` against the proto-generated []T /
//                  map[K]V field directly. Reference baseline.
//   B. RawAll    — `for ... range slices.All(raw)` / `maps.All(raw)`.
//                  Verifies the stdlib rangefunc fast-path: when the
//                  generic helper is inlined and its returned func literal
//                  is consumed in-place by `for range`, escape analysis
//                  proves the closure doesn't escape and the loop runs
//                  with zero allocations.
//   C. LenAt     — slice-only: `for i := range s.Len() { _ = s.At(i) }`,
//                  the indexed escape hatch on goconst.Slice / Slice2.
//                  Omitted for maps: map has no integer index, and a
//                  "Len + Get on every key" variant ends up driving
//                  iteration off the raw map — it benchmarks the raw map
//                  + an extra Get, not a view-native iteration path —
//                  so it carries no information the other columns don't.
//   D. All       — `for ... range s.All()` / `m.All()` on the goconst
//                  view. The struct-wrapper design keeps the returned
//                  iter.Seq / iter.Seq2 funcval visible to the inliner
//                  at the call site, so the closure environment does not
//                  escape and the loop runs at zero allocations, on par
//                  with Raw / RawAll.
//
// Why the columns:
//
//   - Raw is the reference: anything we add must, at best, match it.
//   - RawAll proves the rangefunc protocol *itself* is not the cost;
//     when the compiler can inline the producer in place, the loop is
//     allocation-free. (Verified via go build -gcflags=-m=2:
//     "func literal does not escape" at the slices.All call site.)
//   - LenAt (slices only) exists because it is the only idiom on goconst
//     views that does not use rangefunc at all — useful as a regression
//     canary that the Len/At pair keeps inlining to the same codegen
//     quality as a native `for i := range s`.
//   - All is the ergonomic path. With the struct-wrapper design it
//     matches Raw / RawAll to within noise on every shape; the
//     benchmark exists to keep that guarantee, so that a future
//     regression (e.g. the iter.Seq funcval starting to escape) shows
//     up in CI numbers rather than silently.

// BenchmarkNested_RangeScalarSlice covers []string (NewSlice). Body work
// per element: read v, bump counter. The four strategies are equivalent
// in observable behaviour; only the iteration mechanism varies.
func BenchmarkNested_RangeScalarSlice(b *testing.B) {
	p := newPerson()
	s := p.AsConst().GetTags()

	b.Run("Raw", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for _, v := range p.Tags {
				_ = v
				n++
			}
		}
		benchNestedSinkInt = n
	})

	b.Run("RawAll", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for _, v := range slices.All(p.Tags) {
				_ = v
				n++
			}
		}
		benchNestedSinkInt = n
	})

	b.Run("LenAt", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for j := range s.Len() {
				_ = s.At(j)
				n++
			}
		}
		benchNestedSinkInt = n
	})

	b.Run("All", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for _, v := range s.All() {
				_ = v
				n++
			}
		}
		benchNestedSinkInt = n
	})
}

// BenchmarkNested_RangeStructSlice covers []*Address (NewSlice2). Body
// work per element: read one scalar field on the (possibly nil-backed)
// view. The All path additionally exercises the per-element AsConst
// projection that NewSlice2 stamps onto every read.
func BenchmarkNested_RangeStructSlice(b *testing.B) {
	p := newPerson()
	s := p.AsConst().GetPrevAddresses()

	b.Run("Raw", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for _, a := range p.PrevAddresses {
				_ = a.GetStreet()
				n++
			}
		}
		benchNestedSinkInt = n
	})

	b.Run("RawAll", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for _, a := range slices.All(p.PrevAddresses) {
				_ = a.GetStreet()
				n++
			}
		}
		benchNestedSinkInt = n
	})

	b.Run("LenAt", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for j := range s.Len() {
				_ = s.At(j).GetStreet()
				n++
			}
		}
		benchNestedSinkInt = n
	})

	b.Run("All", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for _, a := range s.All() {
				_ = a.GetStreet()
				n++
			}
		}
		benchNestedSinkInt = n
	})
}

// BenchmarkNested_RangeScalarMap covers map[string]string (NewMap). Body
// work per element: read k and v, bump counter.
func BenchmarkNested_RangeScalarMap(b *testing.B) {
	p := newPerson()
	m := p.AsConst().GetAttributes()

	b.Run("Raw", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for k, v := range p.Attributes {
				_, _ = k, v
				n++
			}
		}
		benchNestedSinkInt = n
	})

	b.Run("RawAll", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for k, v := range maps.All(p.Attributes) {
				_, _ = k, v
				n++
			}
		}
		benchNestedSinkInt = n
	})

	b.Run("All", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for k, v := range m.All() {
				_, _ = k, v
				n++
			}
		}
		benchNestedSinkInt = n
	})
}

// BenchmarkNested_RangeStructMap covers map[int64]*Address (NewMap2).
// Body work per element: read one scalar field on the value-side view.
// The All path exercises the per-value AsConst projection NewMap2 adds.
func BenchmarkNested_RangeStructMap(b *testing.B) {
	p := newPerson()
	m := p.AsConst().GetAddressBook()

	b.Run("Raw", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for k, a := range p.AddressBook {
				_ = k
				_ = a.GetCity()
				n++
			}
		}
		benchNestedSinkInt = n
	})

	b.Run("RawAll", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for k, a := range maps.All(p.AddressBook) {
				_ = k
				_ = a.GetCity()
				n++
			}
		}
		benchNestedSinkInt = n
	})

	b.Run("All", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for k, a := range m.All() {
				_ = k
				_ = a.GetCity()
				n++
			}
		}
		benchNestedSinkInt = n
	})
}

// TestPerson_Map2_Get_Miss pins down the key guarantee of Map2.Get on
// a miss: the returned value is the Go zero value of the view struct,
// i.e. a view whose wrapped *Message pointer is nil. Callers can
// therefore safely invoke scalar getters on it without a nil-check, as
// long as they treat the second return value as the authoritative
// presence flag. This is the main regression-guard: an earlier
// interface-based scheme returned a bare nil view and scalar getters
// would panic here.
func TestPerson_Map2_Get_Miss(t *testing.T) {
	c := newPerson().AsConst()
	m := c.GetAddressBook()

	v, ok := m.Get(99)
	if ok {
		t.Fatal("AddressBook[99]: ok=true, want false")
	}
	if !v.IsNil() {
		t.Fatal("AddressBook[99].IsNil(): got false, want true")
	}
	if got := v.GetCity(); got != "" {
		t.Errorf("AddressBook[99].GetCity(): got %q, want \"\"", got)
	}
}

// TestPerson_TypedNil documents the compile-time guarantee that removed
// the classic Go typed-nil footgun from the _Const boundary.
//
// Under the struct-wrapper scheme, a _Const view is a concrete struct
// value holding an unexported `p *Message` pointer. That means:
//
//   - `view == nil` is a *compile error* (untyped nil cannot be compared
//     to a struct type), so callers cannot accidentally write a check
//     that silently disagrees with "is the backing message set".
//
//   - The only way to ask the nil-ness question is IsNil(), which
//     inspects the underlying pointer directly — no interface boxing,
//     no itab/data split, no surprises.
//
//   - Scalar getters still forward to the nil-safe protoc-gen-go
//     getters on the concrete pointer, so reads on a nil-backed view
//     never panic and yield the zero value.
//
// The three assertions below pin this down. A future refactor that
// reintroduced an interface view would immediately fail
// `TestPerson_TypedNil_CompileError` at build time (we keep the check
// deliberately indirect via IsNil to keep `go vet` and humans happy).
func TestPerson_TypedNil(t *testing.T) {
	p := &Person{Name: "no-home"} // Home == nil

	// Concrete-pointer getter from the generated .pb.go: comparison to
	// nil does agree with "no Home set" here.
	if p.GetHome() != nil {
		t.Fatal("GetHome(): want nil for unset Home field")
	}

	// Const-view getter: the view is a struct value. `home == nil`
	// would not compile. IsNil() is the only supported nil-check and
	// must report true for an unset message field.
	home := p.AsConst().GetHome()
	if !home.IsNil() {
		t.Fatal("GetHome().IsNil() on unset Home: got false, want true")
	}

	// Nil-safe scalar read is still the whole point — this must not
	// panic and must yield the zero value.
	if got := home.GetStreet(); got != "" {
		t.Errorf("GetHome().GetStreet() on nil-backed view: got %q, want \"\"", got)
	}
}

// TestPerson_IsNil fixes the contract IsNil() must satisfy across every
// flavour of _Const view the library ships — message struct wrappers
// (both alive and nil-backed), Slice (scalar and Constable element
// types), and Map (scalar and Constable value types) — including the
// Map2.Get miss sentinel.
//
// Under the struct-wrapper scheme IsNil() is also the *only* way to ask
// the nil-ness question: a direct `view == nil` is a compile error. See
// TestPerson_TypedNil for the matching compile-time-guarantee test.
func TestPerson_IsNil(t *testing.T) {
	// --- live *Message: IsNil() == false. --------------------------------
	alive := newPerson().AsConst()
	if alive.IsNil() {
		t.Error("alive Person.IsNil(): got true, want false")
	}
	if alive.GetHome().IsNil() {
		t.Error("alive Person.GetHome().IsNil(): got true, want false")
	}

	// --- unset child *Message: IsNil() on the child == true. -------------
	noHome := (&Person{}).AsConst()
	if noHome.IsNil() {
		t.Error("alive-but-empty Person.IsNil(): got true, want false")
	}
	if !noHome.GetHome().IsNil() {
		t.Error("noHome.GetHome().IsNil(): got false, want true")
	}
	// Nil-backed view must still be safely callable.
	if got := noHome.GetHome().GetStreet(); got != "" {
		t.Errorf("noHome.GetHome().GetStreet(): got %q, want \"\"", got)
	}

	// --- (*Person)(nil) at the root: AsConst() on nil receiver is safe
	// (it is a plain "return Person_Const{p: x}"), and IsNil() on the
	// resulting view reports true.
	var nilPerson *Person
	nv := nilPerson.AsConst()
	if !nv.IsNil() {
		t.Error("(*Person)(nil).AsConst().IsNil(): got false, want true")
	}

	// --- Slice[T]: IsNil() reports empty/nil underlying slice. ------------
	c := newPerson().AsConst()
	if c.GetTags().IsNil() {
		t.Error("populated GetTags().IsNil(): got true, want false")
	}
	if c.GetPrevAddresses().IsNil() {
		t.Error("populated GetPrevAddresses().IsNil(): got true, want false")
	}
	emptySlice := (&Person{}).AsConst().GetTags()
	if !emptySlice.IsNil() {
		t.Error("empty GetTags().IsNil(): got false, want true")
	}
	emptySlice2 := (&Person{}).AsConst().GetPrevAddresses()
	if !emptySlice2.IsNil() {
		t.Error("empty GetPrevAddresses().IsNil(): got false, want true")
	}

	// --- Map[K, V]: IsNil() reports empty/nil underlying map. -------------
	if c.GetAttributes().IsNil() {
		t.Error("populated GetAttributes().IsNil(): got true, want false")
	}
	if c.GetAddressBook().IsNil() {
		t.Error("populated GetAddressBook().IsNil(): got true, want false")
	}
	emptyMap := (&Person{}).AsConst().GetAttributes()
	if !emptyMap.IsNil() {
		t.Error("empty GetAttributes().IsNil(): got false, want true")
	}
	emptyMap2 := (&Person{}).AsConst().GetAddressBook()
	if !emptyMap2.IsNil() {
		t.Error("empty GetAddressBook().IsNil(): got false, want true")
	}

	// --- Zero-value (nil-backed) views: for Constable projections the
	// Go zero value of the view struct is already a nil-backed view, so
	// IsNil() must report true on it — no helper required. ------------
	var zeroAddr Address_Const
	if !zeroAddr.IsNil() {
		t.Error("var zero Address_Const .IsNil(): got false, want true")
	}

	// --- Map2.Get on a miss must return a view for which IsNil() is
	// true and subsequent scalar getters are still safe. -----------------
	m := c.GetAddressBook()
	miss, ok := m.Get(99)
	if ok {
		t.Fatal("AddressBook[99]: ok=true, want false")
	}
	if !miss.IsNil() {
		t.Error("AddressBook[99].IsNil(): got false, want true")
	}
	if got := miss.GetCity(); got != "" {
		t.Errorf("AddressBook[99].GetCity(): got %q, want \"\"", got)
	}

	// --- Alive hit: IsNil() must be false. -------------------------------
	hit, ok := m.Get(1)
	if !ok {
		t.Fatal("AddressBook[1]: ok=false, want true")
	}
	if hit.IsNil() {
		t.Error("AddressBook[1].IsNil(): got true, want false")
	}
}

// TestPerson_NonComparable pins down the compile-time guarantee that
// every view type the library produces — both the generated
// <Message>_Const wrappers and the goconst.Slice / Slice2 / Map / Map2
// collection views — is *not* comparable with `==`.
//
// The guarantee is delivered by embedding goconst.DoNotCompare
// ([0]func()) in every view struct. A plain `a == b` on two such
// values would be a compile error (and a future refactor that dropped
// the marker would silently start compiling again), so we can't pin
// the contract by writing an `==` line here directly. Instead we ask
// the runtime-reflection equivalent via reflect.Type.Comparable(),
// which returns false for exactly the same set of types the compiler
// rejects `==` on.
//
// For Slice / Map this is partly already implied by the []T / map[K]V
// payload field (slices and maps are themselves not comparable), but
// the DoNotCompare embed makes it explicit at the type level and
// keeps the contract stable even if the payload shape is ever
// swapped out.
func TestPerson_NonComparable(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
	}{
		// Generated message wrappers — the interesting case: the
		// payload is a single *Message pointer, which *is* comparable,
		// so non-comparability comes exclusively from the embedded
		// goconst.DoNotCompare marker.
		{"Address_Const", reflect.TypeOf(Address_Const{})},
		{"Person_Const", reflect.TypeOf(Person_Const{})},
		{"Person_Contact_Const", reflect.TypeOf(Person_Contact_Const{})},

		// Collection views — documentary: the backing []T / map[K]V
		// payload is already non-comparable on its own, but the
		// embedded DoNotCompare marker pins the intent at the type
		// level regardless.
		{"Slice[string]", reflect.TypeOf(goconst.NewSlice[string](nil))},
		{"Slice2[Address_Const,*Address]", reflect.TypeOf(goconst.NewSlice2[Address_Const, *Address](nil))},
		{"Map[string,string]", reflect.TypeOf(goconst.NewMap[string, string](nil))},
		{"Map2[int32,Address_Const,*Address]", reflect.TypeOf(goconst.NewMap2[int32, Address_Const, *Address](nil))},
	}
	for _, tc := range cases {
		if tc.typ.Comparable() {
			t.Errorf("%s: Comparable()=true, want false (missing goconst.DoNotCompare embed?)", tc.name)
		}
	}
}
