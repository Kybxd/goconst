// Unit tests and benchmarks for the generated *_Const views in the nested
// package. Exercises singular message fields, repeated scalar / message
// fields, map with scalar / message value, and a nested message type.
package nested

import (
	"maps"
	"slices"
	"testing"

	goconst "github.com/Kybxd/goconst"
)

// Compile-time assertions: every generated struct must satisfy both its
// *_Const interface and goconst.Constable[<Iface>]. *Message values are
// also expected to satisfy goconst.DoNotCompare directly, since the
// generator emits an IsNil() method on every message pointer type to
// back the DoNotCompare contract.
var (
	_ Address_Const                           = (*Address)(nil).AsConst()
	_ Person_Const                            = (*Person)(nil).AsConst()
	_ Person_Contact_Const                    = (*Person_Contact)(nil).AsConst()
	_ goconst.Constable[Address_Const]        = (*Address)(nil)
	_ goconst.Constable[Person_Const]         = (*Person)(nil)
	_ goconst.Constable[Person_Contact_Const] = (*Person_Contact)(nil)
	_ goconst.DoNotCompare                    = (*Address)(nil)
	_ goconst.DoNotCompare                    = (*Person)(nil)
	_ goconst.DoNotCompare                    = (*Person_Contact)(nil)
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
	// Per NewMap2 contract, the value returned on a miss is a typed-nil
	// view: non-nil at the interface level so scalar getters are safe to
	// call, while the authoritative presence flag is the second return
	// value. See also TestPerson_Map_Zero.
	if zero == nil {
		t.Error("AddressBook[99] zero: got nil interface, want typed-nil view")
	}
	if got := zero.GetCity(); got != "" {
		t.Errorf("AddressBook[99] zero.GetCity(): got %q, want \"\"", got)
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

// ---- iteration-overhead benchmark matrix -----------------------------------
//
// One Benchmark per data shape, four sub-benchmarks per shape, all driven
// through b.Run so a single `go test -bench=BenchmarkNested_Range -benchmem`
// invocation produces the full 4×4 grid in one pass.
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
//   C. LenAt /   — `for i := range s.Len() { _ = s.At(i) }` for slices,
//      LenKeys     `for k := range m.Keys() { _ = m.Get(k) }` is *not*
//                  used (Keys() is itself a rangefunc, see D); for maps
//                  the index-style equivalent doesn't exist, so this
//                  slot reuses the raw map but iterates via the goconst
//                  view's Get on every key from the underlying map —
//                  see the per-shape comment for the exact wiring.
//   D. All       — `for ... range s.All()` / `m.All()` on the goconst
//                  view. Hits the slow path: methods returning a
//                  func literal don't get the same in-caller closure
//                  outline that stdlib free functions enjoy, so the
//                  closure escapes and each loop pays the rangefunc
//                  protocol cost (a fixed handful of allocs/loop,
//                  independent of element count).
//
// Why the four columns:
//
//   - Raw is the reference: anything we add must, at best, match it.
//   - RawAll proves the rangefunc protocol *itself* is not the cost;
//     when the compiler can inline the producer in place, the loop is
//     allocation-free. (Verified via go build -gcflags=-m=2:
//     "func literal does not escape" at the slices.All call site.)
//   - LenAt / Get is the recommended idiom for goconst.Slice in tight
//     loops — the Len/At/Get methods inline (cost well within budget,
//     verified the same way), so this path matches Raw within noise
//     and stays at zero allocs.
//   - All is the ergonomic-but-pricier path. It is shipped because it
//     composes naturally with iter.Seq consumers (slices.Collect,
//     lo/it helpers, etc.), but its per-loop cost is non-zero today
//     because the method-returning-closure form loses the in-caller
//     outline that stdlib free functions get. The benchmark exists
//     to (a) make that cost legible, (b) act as a regression canary
//     when Go improves rangefunc lowering — when the All sub-bench
//     drops to RawAll's numbers, that is a real Go-toolchain win
//     worth recording.

// BenchmarkNested_RangeScalarSlice covers []string (NewSlice). Body work
// per element: read v, bump counter. The four strategies are equivalent
// in observable behaviour; only the iteration mechanism varies.
func BenchmarkNested_RangeScalarSlice(b *testing.B) {
	p := newPerson()
	s := p.AsConst().ConstTags()

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
		benchNestedSink = n
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
		benchNestedSink = n
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
		benchNestedSink = n
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
		benchNestedSink = n
	})
}

// BenchmarkNested_RangeStructSlice covers []*Address (NewSlice2). Body
// work per element: read one scalar field on the (possibly typed-nil)
// view. The All path additionally exercises the per-element AsConst
// projection that NewSlice2 stamps onto every read.
func BenchmarkNested_RangeStructSlice(b *testing.B) {
	p := newPerson()
	s := p.AsConst().ConstPrevAddresses()

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
		benchNestedSink = n
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
		benchNestedSink = n
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
		benchNestedSink = n
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
		benchNestedSink = n
	})
}

// BenchmarkNested_RangeScalarMap covers map[string]string (NewMap). Body
// work per element: read k and v, bump counter.
//
// LenGet column note: a map has no integer index, so the closest
// "index-style" idiom is to drive iteration off the underlying map's
// own range and look each key back up via the goconst view's Get.
// That doubles the per-element work (one native lookup + one Get
// lookup), which is intentional — it captures what users actually
// pay if they want the goconst Get semantics in a hot map loop.
func BenchmarkNested_RangeScalarMap(b *testing.B) {
	p := newPerson()
	m := p.AsConst().ConstAttributes()

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
		benchNestedSink = n
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
		benchNestedSink = n
	})

	b.Run("LenGet", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for k := range p.Attributes {
				v, _ := m.Get(k)
				_, _ = k, v
				n++
			}
		}
		benchNestedSink = n
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
		benchNestedSink = n
	})
}

// BenchmarkNested_RangeStructMap covers map[int64]*Address (NewMap2).
// Body work per element: read one scalar field on the value-side view.
// The All path exercises the per-value AsConst projection NewMap2 adds.
func BenchmarkNested_RangeStructMap(b *testing.B) {
	p := newPerson()
	m := p.AsConst().ConstAddressBook()

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
		benchNestedSink = n
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
		benchNestedSink = n
	})

	b.Run("LenGet", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		var n int
		for i := 0; i < b.N; i++ {
			for k := range p.AddressBook {
				a, _ := m.Get(k)
				_ = k
				_ = a.GetCity()
				n++
			}
		}
		benchNestedSink = n
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
		benchNestedSink = n
	})
}

// TestPerson_Slice_Zero exercises the Zero() method on every Slice flavour:
//
//   - Slice[scalar] (built via NewSlice) returns the plain Go zero value.
//   - Slice[_ConstIface] (built via NewSlice2) returns a typed-nil view
//     whose scalar getters are safe to call — the key property that makes
//     Zero() usable as a default sentinel for third-party helpers like
//     lo/it.FindOrElse without risking a nil-interface panic.
func TestPerson_Slice_Zero(t *testing.T) {
	c := newPerson().AsConst()

	// Scalar Slice: Zero is the untyped string zero value.
	if got := c.ConstTags().Zero(); got != "" {
		t.Errorf("ConstTags().Zero(): got %q, want \"\"", got)
	}

	// Constable Slice: Zero is a non-nil typed-nil view, and every scalar
	// getter on it must return the zero value without panicking — the
	// same contract exercised in TestPerson_NilSingularMessage, but for
	// the "miss" sentinel returned by Zero() instead of a nil struct
	// field.
	z := c.ConstPrevAddresses().Zero()
	if z == nil {
		t.Fatal("ConstPrevAddresses().Zero(): got nil interface, want typed-nil view")
	}
	if got := z.GetStreet(); got != "" {
		t.Errorf("Zero().GetStreet(): got %q, want \"\"", got)
	}
	if got := z.GetCity(); got != "" {
		t.Errorf("Zero().GetCity(): got %q, want \"\"", got)
	}
	if got := z.GetZip(); got != "" {
		t.Errorf("Zero().GetZip(): got %q, want \"\"", got)
	}

	// Zero() is also a handy default when folding over Values() without
	// an external helper. Here we locate the element with Zip == "12345"
	// (missing) and fall back to Zero(), mirroring how a caller would use
	// lo/it.FindOrElse(s.Values(), s.Zero(), pred) externally. The key
	// point: the subsequent .GetCity() call does not panic.
	s := c.ConstPrevAddresses()
	found := s.Zero()
	for _, a := range s.All() {
		if a.GetZip() == "12345" {
			found = a
			break
		}
	}
	if got := found.GetCity(); got != "" {
		t.Errorf("Zero()-fallback .GetCity(): got %q, want \"\"", got)
	}
}

// TestPerson_Map_Zero mirrors TestPerson_Slice_Zero for Map, and also
// pins down the key guarantee of _Map2.Get on a miss: the returned value
// is equal in spirit to m.Zero() — a typed-nil view — so callers can
// safely invoke scalar getters on it without a nil-check, as long as
// they treat the second return value as the authoritative presence flag.
func TestPerson_Map_Zero(t *testing.T) {
	c := newPerson().AsConst()

	// Scalar Map: Zero is the untyped string zero value.
	if got := c.ConstAttributes().Zero(); got != "" {
		t.Errorf("ConstAttributes().Zero(): got %q, want \"\"", got)
	}

	m := c.ConstAddressBook()
	z := m.Zero()
	if z == nil {
		t.Fatal("ConstAddressBook().Zero(): got nil interface, want typed-nil view")
	}
	if got := z.GetCity(); got != "" {
		t.Errorf("Zero().GetCity(): got %q, want \"\"", got)
	}

	// Get on a missing key must return a view that behaves identically
	// to Zero() — non-nil interface, scalar getters yield the zero
	// value. This is the main regression-guard: a previous version
	// returned a bare "var zero V" (nil interface) and would panic here.
	v, ok := m.Get(99)
	if ok {
		t.Fatal("AddressBook[99]: ok=true, want false")
	}
	if v == nil {
		t.Fatal("AddressBook[99]: got nil interface, want typed-nil view")
	}
	if got := v.GetCity(); got != "" {
		t.Errorf("AddressBook[99].GetCity(): got %q, want \"\"", got)
	}
}

// TestPerson_TypedNil pins down the classic Go typed-nil behaviour at the
// _Const boundary and documents it as an accepted, library-level contract
// rather than a bug. It exists to prevent a future refactor from silently
// changing the semantics that callers are expected to code against.
//
// Setup: a *Person with Home == nil. Then:
//
//   - p.GetHome() is a proto3 nil-safe getter returning a typed *Address;
//     because the static return type is a concrete pointer, the caller's
//     == nil comparison does agree with "no Home set" here.
//
//   - p.ConstHome() returns an Address_Const (interface). Go's implicit
//     interface conversion boxes a typed (*Address)(nil) into a non-nil
//     interface value (itab != nil, data == nil). The `view == nil`
//     comparison therefore evaluates to false even though there is no
//     Address behind it.
//
// The protoc-gen-go-const design deliberately trades this away for
// nil-safe scalar reads — i.e. view.GetStreet() still returns "" rather
// than panicking. Callers who need the "is there actually an Address"
// signal must go through IsNil() instead of == nil; see TestPerson_IsNil.
func TestPerson_TypedNil(t *testing.T) {
	p := &Person{Name: "no-home"} // Home == nil

	// Concrete-pointer getter from the generated .pb.go: comparison to
	// nil does agree with "no Home set" here.
	if p.GetHome() != nil {
		t.Fatal("GetHome(): want nil for unset Home field")
	}

	// Const-view getter: interface boxing of a typed nil pointer.
	home := p.ConstHome()
	if home == nil {
		t.Fatal("ConstHome() == nil: got true, want false (typed-nil view is != nil by design)")
	}

	// Nil-safe scalar read is still the whole point — this must not
	// panic and must yield the zero value.
	if got := home.GetStreet(); got != "" {
		t.Errorf("ConstHome().GetStreet() on typed-nil view: got %q, want \"\"", got)
	}

	// The correct way to ask the question users usually mean by `== nil`.
	if !home.IsNil() {
		t.Error("ConstHome().IsNil() on unset Home: got false, want true")
	}
}

// TestPerson_IsNil is the positive-side companion of TestPerson_TypedNil:
// it fixes the contract IsNil() must satisfy across every flavour of
// _Const view the library ships — message pointers (both alive and nil),
// Slice (scalar and Constable element types), and Map (scalar and
// Constable value types) — including the *Map2.Get miss sentinel and the
// Slice.Zero / Map.Zero sentinels.
func TestPerson_IsNil(t *testing.T) {
	// --- live *Message: IsNil() == false, view != nil. -------------------
	alive := newPerson().AsConst()
	if alive.IsNil() {
		t.Error("alive Person.IsNil(): got true, want false")
	}
	if alive.ConstHome().IsNil() {
		t.Error("alive Person.ConstHome().IsNil(): got true, want false")
	}

	// --- unset child *Message: IsNil() == true, view != nil. -------------
	noHome := (&Person{}).AsConst()
	if noHome.IsNil() {
		t.Error("alive-but-empty Person.IsNil(): got true, want false")
	}
	if !noHome.ConstHome().IsNil() {
		t.Error("noHome.ConstHome().IsNil(): got false, want true")
	}
	// Typed-nil view must still be safely callable.
	if got := noHome.ConstHome().GetStreet(); got != "" {
		t.Errorf("noHome.ConstHome().GetStreet(): got %q, want \"\"", got)
	}

	// --- (*Person)(nil) at the root: AsConst() on nil receiver is safe
	// (it is a plain "return x"), and IsNil() on the resulting view
	// reports true.
	var nilPerson *Person
	nv := nilPerson.AsConst()
	if !nv.IsNil() {
		t.Error("(*Person)(nil).AsConst().IsNil(): got false, want true")
	}

	// --- Slice[T]: IsNil() reports empty/nil underlying slice. ------------
	c := newPerson().AsConst()
	if c.ConstTags().IsNil() {
		t.Error("populated ConstTags().IsNil(): got true, want false")
	}
	if c.ConstPrevAddresses().IsNil() {
		t.Error("populated ConstPrevAddresses().IsNil(): got true, want false")
	}
	emptySlice := (&Person{}).AsConst().ConstTags()
	if !emptySlice.IsNil() {
		t.Error("empty ConstTags().IsNil(): got false, want true")
	}
	emptySlice2 := (&Person{}).AsConst().ConstPrevAddresses()
	if !emptySlice2.IsNil() {
		t.Error("empty ConstPrevAddresses().IsNil(): got false, want true")
	}

	// --- Map[K, V]: IsNil() reports empty/nil underlying map. -------------
	if c.ConstAttributes().IsNil() {
		t.Error("populated ConstAttributes().IsNil(): got true, want false")
	}
	if c.ConstAddressBook().IsNil() {
		t.Error("populated ConstAddressBook().IsNil(): got true, want false")
	}
	emptyMap := (&Person{}).AsConst().ConstAttributes()
	if !emptyMap.IsNil() {
		t.Error("empty ConstAttributes().IsNil(): got false, want true")
	}
	emptyMap2 := (&Person{}).AsConst().ConstAddressBook()
	if !emptyMap2.IsNil() {
		t.Error("empty ConstAddressBook().IsNil(): got false, want true")
	}

	// --- Zero() sentinels: for Constable projections the element is a
	// typed-nil view, so IsNil() must report true. -----------------------
	if !c.ConstPrevAddresses().Zero().IsNil() {
		t.Error("ConstPrevAddresses().Zero().IsNil(): got false, want true")
	}
	if !c.ConstAddressBook().Zero().IsNil() {
		t.Error("ConstAddressBook().Zero().IsNil(): got false, want true")
	}

	// --- _Map2.Get on a miss must return a view for which IsNil() is
	// true and subsequent scalar getters are still safe. -----------------
	m := c.ConstAddressBook()
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
