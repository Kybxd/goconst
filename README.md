# goconst

A [`protoc`](https://protobuf.dev) / [`buf`](https://buf.build) plugin
(`protoc-gen-go-const`) that generates a **read-only struct view** for
every `message` in your `.proto` files, alongside the standard
`protoc-gen-go` output.

For each message `Foo`, it emits:

* `Foo_Const` — a Go **struct** wrapping a single unexported `p *Foo`
  pointer. Every field is re-exposed through a `GetName()` forwarder
  on the wrapper: scalar / enum / `bytes` fields forward verbatim to
  `c.p.GetName()`; message / `repeated` / `map` fields return the
  view-native type (`Foo_Const` / `goconst.Slice` / `Foo_ConstSlice`
  / `goconst.Map` / `Foo_ConstMap[K]`). Method names mirror the
  concrete getter on `*Foo` — no collision, because `Foo_Const` is
  a distinct Go type. Since the only field is unexported, a function
  that takes `Foo_Const` physically cannot mutate the message — every
  mutation path (`v.Field = x`, index-assign on the inner slice /
  map, append, etc.) is closed by the Go type system at compile time.
* `Foo_ConstSlice` / `Foo_ConstMap[K]` — Go 1.24 type aliases emitted
  alongside every `Foo_Const`. They bake the storage type `*Foo` into
  the underlying `goconst.Slice2` / `goconst.Map2` views so callers
  see short, intuitive return types on getters (`Address_ConstSlice`,
  `Address_ConstMap[int64]`) instead of the raw three-parameter
  `goconst.Slice2[Address_Const, *Address]` /
  `goconst.Map2[int64, Address_Const, *Address]` spelling. Since
  aliases are a pure front-end concept, the underlying types, method
  sets, size and ABI are unchanged — you can freely assign between
  the alias and the raw `goconst.Slice2` / `Map2` form.
* `func (x *Foo) AsConst() Foo_Const { return Foo_Const{p: x} }` — a
  zero-allocation cast. The wrapper is a single-pointer struct, returned
  by value in a register; there is no heap allocation, no indirection,
  and no interface boxing.
* `func (c Foo_Const) IsNil() bool { return c.p == nil }` — the only
  supported way to ask "is there a backing message?". A direct
  `view == nil` is a **compile error**: the wrapper is a struct type,
  not an interface, so the classic typed-nil footgun is removed at the
  language level rather than papered over by convention or a linter.
* `func (c Foo_Const) Clone() *Foo` — escape hatch out of the read-only
  view: returns a fresh, mutable deep copy of the underlying message
  (delegates to [`proto.Clone`][protoclone]; returns `nil` for a
  nil-backed view). Returns the **concrete** `*Foo` (not `Foo_Const`)
  on purpose: a copy you cannot mutate would defeat the point of
  cloning, and you can always re-wrap it via `clone.AsConst()` at zero
  cost.
* `func (c Foo_Const) String() string` — delegates to `fmt.Sprint(c.p)`
  so the view prints identically to the backing `*Foo` (and yields
  `<nil>` for a nil-backed view rather than panicking).

[protoclone]: https://pkg.go.dev/google.golang.org/protobuf/proto#Clone

Repeated and map fields are returned through
[`goconst.Slice`](goconst.go) / [`goconst.Slice2`](goconst.go) /
[`goconst.Map`](goconst.go) / [`goconst.Map2`](goconst.go) — small
read-only collection *structs* that preserve `len`, index / key lookup
and ranged iteration while denying mutation at the Go type level
(`s[i] = x`, `append(s, ...)`, `copy(s, ...)`, `clear(s)`, `m[k] = v`
and `delete(m, k)` all fail to compile). The `*2` flavours additionally
project each element through its `AsConst()` view on access, so the
element type seen by callers is the callee's `_Const` wrapper rather
than the concrete `*Message`.

The goal is to let API boundaries (service layers, caches, event handlers,
goroutine handoffs, …) express *"I only read this message"* at the type
level, without copying the protobuf or writing hand-maintained DTOs.

## Why

Generated `protoc-gen-go` structs expose every field as a mutable Go
field. Once a `*Message` crosses an API boundary the callee can write to
it, sort its slices in place, overwrite map values, etc. — and the
compiler will not stop them. `*_Const` views turn "please don't mutate
this" comments into a **compile-time contract**:

```go
func Render(user userpb.User_Const) string { // read-only at the type level
    return user.GetName() // ✅
    // user.Name = "x"    // ✗ struct has no exported field
    // user.p.Name = "x"  // ✗ p is unexported (cross-package invisible)
}

Render(u.AsConst()) // call site opts in — no copy, no allocation
```

## How it works

Given a message like

```proto
message Envelope {
  string id = 1;
  Address addr = 2;            // singular message
  repeated Address history = 3;// repeated message
  map<string, Address> by_tag = 4;
}
```

the plugin generates (roughly):

```go
import (
	fmt "fmt"
	goconst "github.com/Kybxd/goconst"
	proto "google.golang.org/protobuf/proto"
)

// Envelope_Const is a read-only wrapper view of *Envelope.
type Envelope_Const struct {
	goconst.DoNotCompare // makes `view == view` a compile error
	p *Envelope
}

type Envelope_ConstSlice             = goconst.Slice2[Envelope_Const, *Envelope]
type Envelope_ConstMap[K comparable] = goconst.Map2[K, Envelope_Const, *Envelope]

// AsConst returns x wrapped as its read-only Envelope_Const view.
func (x *Envelope) AsConst() Envelope_Const { return Envelope_Const{p: x} }

func (c Envelope_Const) GetId() string { return c.p.GetId() }

func (c Envelope_Const) GetAddr() Address_Const {
	return c.p.GetAddr().AsConst()
}

func (c Envelope_Const) GetHistory() Address_ConstSlice {
	return goconst.NewSlice2(c.p.GetHistory())
}

func (c Envelope_Const) GetByTag() Address_ConstMap[string] {
	return goconst.NewMap2(c.p.GetByTag())
}

func (c Envelope_Const) IsNil() bool { return c.p == nil }

func (c Envelope_Const) Clone() *Envelope {
	if c.p == nil {
		return nil
	}
	return proto.Clone(c.p).(*Envelope)
}

func (c Envelope_Const) String() string { return fmt.Sprint(c.p) }
```

(For a full end-to-end output including cross-package imports,
`*timestamppb.Timestamp` fields and `Slice` / `Map` over imported
messages, see [`examples/gen/go/importer/importer.const.pb.go`](examples/gen/go/importer/importer.const.pb.go).)

`goconst.Slice[T]` / `goconst.Slice2[T, E]` / `goconst.Map[K, V]` /
`goconst.Map2[K, V, E]` are defined in this repo's root package (see
[goconst.go](goconst.go)) as **concrete struct types** whose sole field
is an unexported backing slice / map. They offer:

```go
// Constable is the witness that *Message participates in the _Const scheme.
type Constable[T any] interface{ AsConst() T }

// Scalar / excluded-package elements — value type T is the stored type.
type Slice[T any] struct { /* embeds goconst.DoNotCompare; unexported: s []T */ }

func (c Slice[T]) Len() int
func (c Slice[T]) At(i int) T
func (c Slice[T]) All() iter.Seq2[int, T]   // for i, v := range s.All()
func (c Slice[T]) Values() iter.Seq[T]      // slices.Collect(s.Values()), slices.Sorted(...)
func (c Slice[T]) IsNil() bool
func (c Slice[T]) String() string           // prints the underlying []T

// Message elements — stored type E (e.g. *Address), projected to T (e.g. Address_Const) on access.
type Slice2[T any, E Constable[T]] struct { /* embeds goconst.DoNotCompare; unexported: s []E */ }

func (c Slice2[T, E]) Len() int
func (c Slice2[T, E]) At(i int) T           // returns c.s[i].AsConst()
func (c Slice2[T, E]) All() iter.Seq2[int, T]
func (c Slice2[T, E]) Values() iter.Seq[T]
func (c Slice2[T, E]) IsNil() bool
func (c Slice2[T, E]) String() string       // prints the raw []E (so messages render via their own String())

// Scalar / excluded-package values — value type V is the stored type.
type Map[K comparable, V any] struct { /* embeds goconst.DoNotCompare; unexported: m map[K]V */ }

func (c Map[K, V]) Len() int
func (c Map[K, V]) Get(k K) (V, bool)
func (c Map[K, V]) Has(k K) bool
func (c Map[K, V]) All() iter.Seq2[K, V]    // for k, v := range m.All()
func (c Map[K, V]) Keys() iter.Seq[K]       // maps.Keys-shape, pipe into slices.Collect / Sorted
func (c Map[K, V]) Values() iter.Seq[V]     // maps.Values-shape, same idea
func (c Map[K, V]) IsNil() bool
func (c Map[K, V]) String() string

// Message values — stored type E (e.g. *Address), projected to V (e.g. Address_Const) on access.
type Map2[K comparable, V any, E Constable[V]] struct { /* embeds goconst.DoNotCompare; unexported: m map[K]E */ }

func (c Map2[K, V, E]) Len() int
func (c Map2[K, V, E]) Get(k K) (V, bool)   // miss returns nil-backed AsConst() view + false
func (c Map2[K, V, E]) Has(k K) bool
func (c Map2[K, V, E]) All() iter.Seq2[K, V]
func (c Map2[K, V, E]) Keys() iter.Seq[K]
func (c Map2[K, V, E]) Values() iter.Seq[V]
func (c Map2[K, V, E]) IsNil() bool
func (c Map2[K, V, E]) String() string
```

Callers keep O(1) length / indexed / keyed access *and* the familiar
range-over-func syntax. Because `Values` / `Keys` return
`iter.Seq[…]`, the read-only view plugs straight into stdlib sinks
(`slices.Collect`, `slices.Sorted`, `maps.Collect`, …) and any
`iter.Seq`-aware third-party helper such as `github.com/samber/lo/it`
— higher-level algorithms (`ContainsBy`, `Find`, `MinBy`, …) live there
rather than on this type. The four constructors are provided by the
`goconst` package itself:

```go
// Scalar / excluded-package elements — pass values through unchanged.
func NewSlice[T any](s []T) Slice[T]
func NewMap[K comparable, V any](m map[K]V) Map[K, V]

// Message elements — project each element via its AsConst() method.
func NewSlice2[T any, E Constable[T]](s []E) Slice2[T, E]
func NewMap2[K comparable, V any, E Constable[V]](m map[K]E) Map2[K, V, E]
```

so the plugin only has to emit a **one-line companion getter** per
message / repeated / map field. Type arguments on the constructor call
are omitted on purpose — Go's constraint type inference recovers
both the element type and the projected `_Const` type automatically.

### Compile-time read-only enforcement

Both the per-message `Foo_Const` wrapper and the collection views
(`Slice` / `Slice2` / `Map` / `Map2`) are defined as `struct`s with a
**single unexported field** (`p *Foo` / `s []T` / `m map[K]V`) rather
than as named slice / map types or as interfaces wrapping one. That
single decision closes every Go-level mutation path at compile time, in
every consumer package:

```go
v := p.AsConst()                     // Person_Const
v.Name = "x"                          // compile error: Name undefined
v.p.Name = "x"                        // compile error: p is unexported

s := p.AsConst().GetTags()           // goconst.Slice[string]
s[0] = "x"                           // compile error: cannot index
s = append(s, "x")                   // compile error: first argument to append must be a slice
copy(s, tags)                        // compile error: first argument to copy must be a slice
clear(s)                             // compile error: argument must be a map, slice, or channel
for _, v := range s.s { ... }        // compile error: s.s is undefined (unexported)

m := p.AsConst().GetAttributes()     // goconst.Map[string, string]
m["k"] = "v"                         // compile error: cannot index
delete(m, "k")                       // compile error: first argument to delete must be a map
clear(m)                             // compile error (same as above)
```

A consumer outside the `goconst` / generated package has no syntactic
way to reach the payload short of `unsafe`/`reflect`, so the read-only
contract is enforced by the Go type system rather than by convention
or by a runtime check.

### Typed-nil footgun, eliminated at compile time

Earlier iterations of this plugin exposed `Message_Const` as a Go
**interface**. That design ran into the classic Go typed-nil trap: a
`(*Address)(nil)` boxed into an `Address_Const` interface value had a
non-nil itab and a nil data word, so `view == nil` evaluated to
**false** even though there was no Address behind it. A dedicated
linter (`cmd/nilcompare`) existed solely to flag the wrong spelling.

The current struct-wrapper design removes the trap outright:

```go
home := p.AsConst().GetHome()        // Address_Const (struct value)
if home == nil { ... }               // ✗ compile error: cannot compare struct to nil

if home.IsNil() {                    // ✓ the only supported nil-check
    // no Home set — fall back, skip, log, ...
} else {
    use(home.GetStreet())
}
```

The nil-safe-read guarantee is preserved: `home.GetStreet()` on a
nil-backed view still returns `""` rather than panicking, because the
scalar getter forwards to `c.p.GetStreet()` and protoc-gen-go emits
nil-safe getters on concrete pointers. The difference is that callers
can no longer accidentally write `view == nil` and silently disagree
with reality — the question has to be spelled `IsNil()`, because the
type system allows no other spelling.

For repeated / map fields, `IsNil()` doubles as an "empty?" predicate —
a nil slice, an empty slice, a nil map and an empty map all report true:

```go
if !envelope.GetHistory().IsNil() {
    for _, h := range envelope.GetHistory().All() { ... }
}
```

### `==` on views is a compile error

Every view struct this library produces — both the generated
`<Message>_Const` wrappers and the `goconst.Slice` / `Slice2` / `Map` /
`Map2` collection views — embeds `goconst.DoNotCompare`, a zero-width
`[0]func()` marker borrowed from
`google.golang.org/protobuf/internal/pragma`:

```go
type DoNotCompare [0]func()
```

Because `func` is not comparable, the whole containing struct becomes
non-comparable, so `==` / `!=` on two view values is rejected by the
compiler:

```go
a := p.AsConst()
b := p.AsConst()
if a == b { ... }                    // ✗ compile error: struct containing
                                     //   goconst.DoNotCompare cannot be compared
```

Pointer-equality on a wrapper value is never the question a caller
actually wants to ask: two views of the *same* underlying message are
trivially equal on the wrapped pointer, but two views of
*semantically-equal* messages are not — and the latter is the question
one usually means by "are these views equal?". Letting the compiler
reject the spelling outright is cleaner than relying on documentation
or a linter; callers who really want identity on the underlying
message can compare the two `*Message` values directly.

The marker is zero-width, so it adds no memory and no runtime cost;
see `TestPerson_NonComparable` in the examples for a
`reflect.Type.Comparable()` regression test that pins the guarantee.

### Miss-safe defaults: the Go zero value of a `_Const` view

`goconst.NewSlice2` / `NewMap2` project every element through its
`AsConst()` view, so the element type `T` is a `Foo_Const` struct
value whose inner `p` pointer is the backing `*Foo`. The Go zero value
of that struct is `Foo_Const{p: nil}` — i.e. the exact same
nil-backed view that `AsConst()` on a nil `*Foo` would produce, which
means every scalar getter on it is still safe to call.

That property is enough to cover every miss-safe default pattern
without a dedicated helper:

* **`Map2[K, V, E].Get(k)` on a miss** returns `(V{}, false)` — the
  zero value of `V` (e.g. `Foo_Const{p: nil}`). The second return
  value is the *authoritative* presence flag; the first is
  deliberately chosen so that `v.GetX()` is always safe to call, with
  or without a preceding `ok` check.
* **`Map[K, V].Get(k)` on a miss** returns `(zeroV, false)` with the
  plain Go zero value of `V`. For scalar / enum / `bytes` element
  types this is `""` / `0` / `false` / `nil`; for excluded-package
  message types it is a nil concrete pointer, whose own scalar
  getters are nil-safe by virtue of protoc-gen-go's generated code.
* **Fallback in third-party helpers** (e.g. `lo/it.FindOrElse`): pass
  a bare zero value of the element type — `var zero Foo_Const` or
  the literal `Foo_Const{}` — as the default; `IsNil()` on the
  result reports `true`, and scalar getters are safe regardless.

Two recommended miss-safe patterns fall out of this:

```go
// A) With an iter.Seq-aware helper (e.g. github.com/samber/lo/it):
//    pass the zero value of the view as the fallback so the result is
//    always a live, safely-readable view.
addr := loi.FindOrElse(
    s.GetPrevAddresses().Values(),
    Address_Const{}, // zero-value view: nil-backed, scalar getters safe
    func(a Address_Const) bool { return a.GetZip() == "12345" },
)
_ = addr.GetCity() // safe even if no element matches

// B) With a plain Map lookup: trust ok for presence, use v regardless.
if v, ok := m.Get(key); ok {
    use(v)
} else {
    _ = v.GetCity() // safe: v is a nil-backed view, not a raw nil pointer
}
```

### Zero allocation, full inlining

Because every view type is a concrete struct fully visible at the call
site, the Go inliner flows through the generic methods as if they were
hand-written on a native `[]T` / `map[K]V`:

* `AsConst()` returns a `Foo_Const{p: x}` struct literal by value —
  **0 allocs**, the value lives in a register.
* `goconst.NewSlice(...)` / `NewSlice2(...)` / `NewMap(...)` /
  `NewMap2(...)` are struct literals whose sole field is the caller's
  slice / map header — **0 allocs**, the value stays on the stack.
* `Len()`, `At(i)`, `Get(k)`, `Has(k)`, `IsNil()`, scalar `Get<Field>`
  forwarders — **0 allocs**.
* `All()`, `Values()`, `Keys()` return `iter.Seq` / `iter.Seq2`
  range-over-func values that the inliner lifts directly into the
  caller's frame, so `for i, v := range view.All()` runs at **0 allocs
  and ~1–6 ns/op for slices, ~50 ns/op for maps** (dominated by Go's
  map-iteration cost itself). See the benchmark table below for the
  full matrix.

The practical upshot: there is no "hot-path escape hatch" you need to
reach for. The ergonomic `range view.All()` form and the indexed
`for i := range view.Len() { use(view.At(i)) }` form have the same
allocation profile (zero) and essentially identical iteration cost.
Pick whichever reads better at the call site.

### Debug printing

`Foo_Const` implements `fmt.Stringer` via `fmt.Sprint(c.p)`, and
`Slice` / `Map` values returned by `goconst.NewSlice(...)` /
`NewSlice2(...)` / `NewMap(...)` / `NewMap2(...)` do the same for
their underlying slice / map. Printing one with `fmt.Print*`,
`log.Print*`, or `%v` produces exactly the same output as printing
the raw `*Foo` / `[]T` / `map[K]V` would — no extra `Foo_Const{...}`
/ `Slice[...]` / `Map[...]` wrapper, no intermediate `slices.Collect`
/ `maps.Collect` step needed:

```go
c := envelope.AsConst()
fmt.Println(c)                     // == fmt.Println(envelope)       (or "<nil>" if p == nil)

s := goconst.NewSlice2(p.GetPrevAddresses())
fmt.Println(s)                     // == fmt.Println(p.GetPrevAddresses())

m := goconst.NewMap2(p.GetAddressBook())
fmt.Println(m)                     // == fmt.Println(p.GetAddressBook())
```

For message-element variants (`NewSlice2` / `NewMap2`) the underlying
protobuf messages are printed directly, so you get their rich built-in
prototext-style `String()` rather than opaque struct-dump output.

Key design points:

* **Scalars / enums / `bytes`** keep the stdlib Go type; the generator
  emits a one-line `func (c Foo_Const) GetName() T { return c.p.GetName() }`
  forwarder that the inliner turns into a direct pointer-read.
* **Singular message fields** switch to the callee's `T_Const` view.
  `GetAddr()` on the wrapper is a one-liner returning
  `c.p.GetAddr().AsConst()`. This also preserves proto3's nil-safe
  getter semantics: if `Addr` is unset, `c.p.GetAddr()` returns
  `(*Address)(nil)`, `AsConst()` wraps that into `Address_Const{p: nil}`,
  and every scalar getter on the resulting view still returns zero
  values instead of panicking.
* **Repeated fields** switch from `[]T` to `T_ConstSlice` (a Go 1.24
  type alias for `goconst.Slice2[T_Const, *T]`) for message elements,
  or `goconst.Slice[T]` for scalar / excluded-package elements.
  `GetHistory()` on the wrapper delegates to `goconst.NewSlice2(...)`
  or `goconst.NewSlice(...)` respectively.
* **Map fields** switch from `map[K]V` to `V_ConstMap[K]` (a Go 1.24
  generic type alias for `goconst.Map2[K, V_Const, *V]`) for message
  values, or `goconst.Map[K, V]` for scalar / excluded-package values,
  likewise delegating to `goconst.NewMap2(...)` or
  `goconst.NewMap(...)`.
* **`oneof`** is supported; each arm's getter is emitted with the
  appropriate element type — scalar arms keep their plain `GetNote()`
  forwarder, message arms return the callee's `_Const` view
  (`GetLocation()` returns `Address_Const`).
* **Cross-package references** use `QualifiedGoIdent`, so imports for
  `*_Const` types from other generated packages are added automatically.
* **Zero-allocation cast**: `AsConst()` returns a single-pointer struct
  by value. Benchmarks measure 0 allocs for the cast, 0 allocs for
  singular message-field access, and 0 allocs across all iteration
  paths.
* **`Clone()` escape hatch**: every `Foo_Const` exposes
  `Clone() *Foo`, implemented as `return proto.Clone(c.p).(*Foo)`
  (with an explicit nil guard so a nil-backed view clones to `nil`
  rather than panicking). It is the canonical way to leave the
  read-only world — e.g. when a callee that received a `_Const` view
  needs an independent, mutable copy to hand to a writer-style API or
  to retain past the lifetime of the source. The return type is the
  concrete `*Foo` (not `Foo_Const`) by design: a copy that the caller
  cannot mutate would defeat the purpose, and the result can be
  re-wrapped via `clone.AsConst()` at zero cost when only a read-only
  handoff is needed downstream.

### Iteration performance

`Slice` / `Slice2` / `Map` / `Map2` are concrete struct types whose sole
field is an unexported backing slice / map, so the Go inliner flows
through every method — including the range-over-func shapes returned by
`All()` / `Values()` / `Keys()` — directly into the caller's frame. The
practical consequence is that **every iteration strategy runs at zero
allocation**, whether you use the ergonomic `range view.All()` form or
the indexed `Len()` + `At(i)` / `Get(k)` escape hatch.

Measured on `examples/gen/go/nested` (`go test -bench`, AMD EPYC 9754,
Go 1.24, 3-element fixtures). Slices run four iteration strategies;
maps run three — `Len() + Get` on a map would have to drive iteration
off the raw map and then Get once per key, so it benchmarks the raw
map plus an extra lookup rather than a view-native path, and is
omitted.

| Container                      | `for range raw`     | `for range stdlib.All(raw)` | `Len()` + `At`        | `view.All()`           |
| ------------------------------ | ------------------- | --------------------------- | --------------------- | ---------------------- |
| `[]string` (Slice)             | 2.0 ns / 0 allocs   | 2.0 ns / 0 allocs           | 2.0 ns / 0 allocs     | **2.0 ns / 0 allocs**  |
| `[]*Address` (Slice2)          | 2.0 ns / 0 allocs   | 2.0 ns / 0 allocs           | 4.2 ns / 0 allocs     | **4.6 ns / 0 allocs**  |
| `map[string]string` (Map)      | 50 ns / 0 allocs    | 50 ns / 0 allocs            | —                     | **50 ns / 0 allocs**   |
| `map[int64]*Address` (Map2)    | 50 ns / 0 allocs    | 50 ns / 0 allocs            | —                     | **52 ns / 0 allocs**   |

Iteration via `view.All()` is essentially free — the struct-wrapper
design means the `iter.Seq2[…]` funcval is inlined into the caller's
frame, so the closure environment does not escape. For slices, the
`All()` path matches the native `for i := range raw` baseline to
within noise; the small gap on `Slice2` / `Map2` comes from the
per-element `AsConst()` projection, which is a single pointer-copy
and does not allocate. For maps, `view.All()` is within a percent or
two of ranging the underlying native map directly — the overhead is
dominated by Go's own map iterator, not by the goconst wrapper.

**Recommendation:** default to the `range view.All()` form everywhere —
it is the most readable, it matches the native slice / map iteration
shape, and it is already at the performance floor. The `Len()` + `At` /
`Get` escape hatch exists for the occasional call site where indexed
access reads better, not as a hot-path optimisation.

The full benchmark matrix lives in
[`examples/gen/go/nested/nested_const_test.go`](examples/gen/go/nested/nested_const_test.go)
(`BenchmarkNested_RangeScalarSlice` / `…StructSlice` / `…ScalarMap` /
`…StructMap`); reproduce with

```bash
go test -bench='^BenchmarkNested_Range' -benchmem ./examples/gen/go/nested/...
```

## Installation & wiring

### Prerequisites

```bash
# buf CLI (the only binary you need on your machine)
go install github.com/bufbuild/buf/cmd/buf@latest
```

You do **not** need to `go install protoc-gen-go` or `go build` this
plugin:

* **`protocolbuffers/go`** — the stock Go message generator — runs as a
  Buf *remote* plugin pinned by tag (see below); buf fetches and
  executes it for you.
* **`protoc-gen-go-const`** — this repo's plugin — runs as a *local*
  plugin via `go run ./cmd/protoc-gen-go-const/main.go`, so buf compiles
  and executes it straight from source on every invocation.

### Minimal `buf.gen.yaml`

Add this plugin as a second `go run` local plugin, writing into the same
`out` directory as `protocolbuffers/go` so both files land next to each
other:

```yaml
version: v2
plugins:
  # Keep this tag in sync with google.golang.org/protobuf in your go.mod.
  - remote: buf.build/protocolbuffers/go:v1.36.11
    out: gen/go
    opt:
      - paths=source_relative

  - local: [ "go", "run", "github.com/Kybxd/goconst/cmd/protoc-gen-go-const" ]
    out: gen/go
    opt:
      - paths=source_relative
      # Optional, see "exclude_packages" below. Each entry is a doublestar
      # glob, so the line below recursively excludes every WKT subpackage.
      # - exclude_packages=google.golang.org/protobuf/types/known/**
    strategy: all
```

Consumers of the generated code must also have
`github.com/Kybxd/goconst` in their `go.mod` (a `go mod tidy` after the
first `buf generate` will add it automatically, since `*.const.pb.go`
imports it).

Then run `buf generate` as usual. For every `foo.proto` you will get two
files side by side:

* `foo.pb.go`       — standard protobuf Go structs (from `protocolbuffers/go`)
* `foo.const.pb.go` — `*_Const` read-only struct views (from this plugin)

## Flag: `--exclude_packages`

Comma/repeat-style flag listing Go import path **glob patterns** that
should **not** get `*_Const` views. Each entry is matched against the
field's owning Go import path with [doublestar][doublestar] (gitignore- /
bash globstar-style) semantics:

* a plain path matches exactly, so the legacy "list of Go import paths"
  usage keeps working unchanged;
* `*` / `?` match within a single `/`-separated path segment;
* a recursive `**` segment matches any number of subpackages, including
  nested ones — use this to bulk-exclude an entire subtree.

When a field references a message from a matching package, the plugin
keeps the concrete `*Type` signature on the `_Const` wrapper's
`Get<Name>` method (forwarding verbatim to the concrete getter rather
than materialising an AsConst projection or a `Slice2` / `Map2`
collection view):

```yaml
opt:
  # Exact path — the legacy "list of Go import paths" usage.
  - exclude_packages=github.com/you/yourrepo/gen/go/proto/external
  # Recursive glob — covers every WKT subpackage (timestamppb, durationpb,
  # anypb, wrapperspb, structpb, fieldmaskpb, emptypb, …) in one line,
  # including any nested subpackages.
  - exclude_packages=google.golang.org/protobuf/types/known/**
```

[doublestar]: https://github.com/bmatcuk/doublestar

Typical use cases:

1. **Third-party / vendored protos** you don't own and therefore don't
   run this generator against.
2. **Well-known types** (`google.protobuf.Timestamp`, `Duration`, `Any`,
   `Wrappers*`, …). These are produced by the upstream
   `protocolbuffers/go` plugin and **ship without any `*_Const` /
   `AsConst()`**. If you import a WKT in your own proto and leave its Go
   package out of `exclude_packages`, the generated `_Const` wrapper
   will project a field through a nonexistent
   `timestamppb.Timestamp_Const` type and the file will not compile.

**Rule of thumb:** exclude every WKT package you import. The single
recursive glob `google.golang.org/protobuf/types/known/**` matches all
of them (`.../timestamppb`, `.../durationpb`, `.../anypb`,
`.../wrapperspb`, …, including any nested subpackages) and is the
recommended default for projects that touch any WKT.

## Project layout

```
.
├── goconst.go                  # runtime Slice / Slice2 / Map / Map2 types (imported by generated code)
├── cmd/
│   └── protoc-gen-go-const/    # the protobuf plugin binary (package main)
├── examples/                   # hand-crafted protos exercising every branch
│   ├── proto/<leaf>/           # source .proto files
│   ├── gen/go/<leaf>/          # generated .pb.go + .const.pb.go (checked in as golden)
│   ├── buf.yaml
│   └── buf.gen.yaml
├── go.mod
└── README.md                   # this file
```

See [examples/README.md](examples/README.md) for what each example proto
exercises and how to regenerate them locally.

## Version compatibility

| Component                      | Pinned to                                               |
| ------------------------------ | ------------------------------------------------------- |
| Go                             | 1.24.0 (for generic [type aliases](https://go.dev/doc/go1.24#language) and stdlib [`iter`](https://pkg.go.dev/iter)) |
| `google.golang.org/protobuf`   | v1.36.11                                                |
| `buf.build/protocolbuffers/go` | v1.36.11 (kept in sync with the above)                  |
| proto editions supported       | proto2 → edition 2024 (via `FEATURE_SUPPORTS_EDITIONS`) |

When bumping `google.golang.org/protobuf` in `go.mod`, bump the
`protocolbuffers/go` remote tag in your `buf.gen.yaml` to the same
version so the generated `.pb.go` and the ambient runtime stay aligned.

## License

[MIT](LICENSE)
