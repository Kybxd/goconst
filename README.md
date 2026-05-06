# goconst

A [`protoc`](https://protobuf.dev) / [`buf`](https://buf.build) plugin
(`protoc-gen-go-const`) that generates a **read-only interface view** for
every `message` in your `.proto` files, alongside the standard
`protoc-gen-go` output.

For each message `Foo`, it emits:

* `Foo_Const` — a Go interface with a read-only view of `Foo`. Scalar /
  enum / `bytes` fields keep their plain `GetName()` getter; message /
  `repeated` / `map` fields whose signatures differ from the concrete
  `*Foo` are exposed through companion `ConstName()` methods. A
  function that takes `Foo_Const` physically cannot mutate the message.
* `func (x *Foo) AsConst() Foo_Const { return x }` — a zero-allocation
  cast. `*Foo` itself implements `Foo_Const`, so there is no wrapper
  struct, no per-call allocation, and no indirection on scalar getters.
* `func (x *Foo) Const<Name>() ...` — one companion method per field
  whose signature had to change (singular message → `T_Const`, `[]T` →
  `goconst.Slice[T]`, `map[K]V` → `goconst.Map[K,V]`), attached directly
  to `*Foo` in the generated `foo.const.pb.go`.
* `func (x *Foo) Clone() *Foo` — escape hatch out of the read-only
  view: returns a fresh, mutable deep copy of the underlying message
  (delegates to [`proto.Clone`][protoclone]). Declared on `Foo_Const`
  too, so callers holding only the interface can still copy. Returns
  the **concrete** `*Foo` (not `Foo_Const`) on purpose: a copy you
  cannot mutate would defeat the point of cloning, and you can always
  re-wrap it via `clone.AsConst()` at zero cost.

[protoclone]: https://pkg.go.dev/google.golang.org/protobuf/proto#Clone

Repeated and map fields are returned through
[`goconst.Slice`](goconst.go) / [`goconst.Map`](goconst.go) — small
read-only collection interfaces that preserve `len`, index / key lookup
and ranged iteration without leaking mutation (no `append`, no slot
assignment).

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
    // user.Name = "x"    // ✗ interface has no such field
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
	goconst "github.com/Kybxd/goconst"
	proto "google.golang.org/protobuf/proto"
)

// Envelope_Const is a read-only interface view of Envelope.
type Envelope_Const interface {
	proto.Message
	goconst.DoNotCompare

	GetId() string
	ConstAddr() Address_Const
	ConstHistory() goconst.Slice[Address_Const]
	ConstByTag() goconst.Map[string, Address_Const]
	Clone() *Envelope
}

var _ Envelope_Const = (*Envelope)(nil)

// AsConst returns x as its read-only Envelope_Const view.
func (x *Envelope) AsConst() Envelope_Const { return x }

// IsNil reports whether x is nil.
func (x *Envelope) IsNil() bool { return x == nil }

// Clone returns a deep copy of x as a fresh, mutable *Envelope.
func (x *Envelope) Clone() *Envelope { return proto.Clone(x).(*Envelope) }

func (x *Envelope) ConstAddr() Address_Const {
	return x.GetAddr()
}

func (x *Envelope) ConstHistory() goconst.Slice[Address_Const] {
	return goconst.NewSlice2(x.GetHistory())
}

func (x *Envelope) ConstByTag() goconst.Map[string, Address_Const] {
	return goconst.NewMap2(x.GetByTag())
}
```

(For a full end-to-end output including cross-package imports,
`*timestamppb.Timestamp` fields and `Slice` / `Map` over imported
messages, see [`examples/gen/go/importer/importer.const.pb.go`](examples/gen/go/importer/importer.const.pb.go).)

`goconst.Slice[T]` / `goconst.Map[K, V]` are defined in this repo's
root package (see [goconst.go](goconst.go)) and offer:

```go
type DoNotCompare interface {
    IsNil() bool             // typed-nil-safe presence check; see "Typed-nil pitfall" below
}

type Slice[T any] interface {
    DoNotCompare
    Len() int
    At(i int) T
    All() iter.Seq2[int, T]   // for i, v := range s.All()
    Values() iter.Seq[T]      // slices.Collect(s.Values()), slices.Sorted(...)
    Zero() T                  // miss-safe default; see "Miss-safe defaults" below
}

type Map[K comparable, V any] interface {
    DoNotCompare
    Len() int
    Get(k K) (V, bool)
    Has(k K) bool
    All() iter.Seq2[K, V]     // for k, v := range m.All()
    Keys() iter.Seq[K]        // maps.Keys-shape, pipe into slices.Collect / Sorted
    Values() iter.Seq[V]      // maps.Values-shape, same idea
    Zero() V                  // miss-safe default; see "Miss-safe defaults" below
}
```

so callers keep O(1) length / indexed / keyed access *and* the familiar
range-over-func syntax, but lose `append` / slot assignment at the type
level. Because `Values` / `Keys` return `iter.Seq[…]`, the read-only
view plugs straight into stdlib sinks (`slices.Collect`, `slices.Sorted`,
`maps.Collect`, …) and any `iter.Seq`-aware third-party helper such as
`github.com/samber/lo/it` — higher-level algorithms (`ContainsBy`,
`Find`, `MinBy`, …) live there rather than on this interface. The two
concrete implementations are provided by the `goconst` package itself
via

```go
// Scalar / excluded-package elements — pass values through unchanged.
func NewSlice[T any](s []T) Slice[T]
func NewMap[K comparable, V any](m map[K]V) Map[K, V]

// Message elements — project each element via its AsConst() method.
type Constable[T any] interface{ AsConst() T }
func NewSlice2[T any, E Constable[T]](s []E) Slice[T]
func NewMap2[K comparable, V any, E Constable[V]](m map[K]E) Map[K, V]
```

so the plugin only has to emit a **one-line companion getter** per
message / repeated / map field — *Message itself satisfies its _Const
interface, so no wrapper type is generated.

### Typed-nil pitfall and `IsNil()`

Every `Message_Const` emitted by this plugin is a Go *interface*, and
everything that returns one — the `AsConst()` cast, the singular-message
`ConstHome()` companion, `Slice[T_Const].At(i)`, `Map[K, V_Const].Get(k)`,
`Slice.Values()` / `Map.Values()` iterators, `Slice.Zero()` /
`Map.Zero()`, and so on — always wraps a concrete `*Message` pointer into
an interface value. That introduces the classic Go **typed-nil trap**: a
`(*Address)(nil)` boxed into an `Address_Const` is an interface value
whose itab is non-nil and whose data word is nil, so `view == nil`
evaluates to **`false`** — even though semantically there is no Address
behind the view.

This is a *language-level* fact, not a bug in this library. The library
deliberately leans into it to preserve the nil-safe-read guarantee:
because a typed-nil `*Address` still answers every scalar / enum /
`bytes` getter with the zero value (proto3 generates those to be nil-
receiver-safe), callers can chain `view.GetStreet()` without a
preceding nil check and get `""` on a missing field instead of a panic.
Trade-off: `view == nil` is **not** the right way to spell "is there
actually a value here?".

To make the correct spelling discoverable at the type level,
`goconst.DoNotCompare` is a tiny marker interface that exposes an
`IsNil() bool` method; **every** `Slice`, every `Map`, and every
generated `Message_Const` embeds it. The generator emits a matching
`func (x *Message) IsNil() bool { return x == nil }` on every message
pointer, and `Slice` / `Map` implementations report whether the
underlying slice / map has no elements.

```go
// ✗ Wrong: typed-nil views are != nil by design.
if p.ConstHome() == nil { ... }

// ✓ Right: IsNil() is evaluated against the concrete dynamic type
//   and returns what the caller actually meant.
if p.ConstHome().IsNil() {
    // no Home set — fall back, skip, log, ...
} else {
    use(p.ConstHome().GetStreet())
}

// For repeated / map fields, IsNil() doubles as an "empty?" predicate —
// a nil slice, an empty slice, a nil map and an empty map all report true.
if !envelope.ConstHistory().IsNil() {
    for _, h := range envelope.ConstHistory().All() { ... }
}

// The Map.Get miss sentinel — and the Slice/Map Zero() sentinel for
// Constable projections — are typed-nil views, so IsNil() returns true
// while scalar getters on the view still yield the zero value.
v, ok := m.Get(key)
if v.IsNil() { /* equivalent to !ok for Constable projections */ }
_ = v.GetCity() // always safe, with or without the IsNil() check
```

Because the correct spelling (`IsNil()` instead of `== nil`) is a rule
no type system can enforce on its own, this repo ships a small
go/analysis linter that flags the wrong spelling at compile time — see
[Static check: `cmd/nilcompare`](#static-check-cmdnilcompare) below.

### Miss-safe defaults: `Zero()` and `Map.Get`

`goconst.NewSlice2` / `NewMap2` project every element through its
`AsConst()` view, so the element type `T` is an **interface** (e.g.
`Address_Const`) rather than a concrete pointer. That has one
unpleasant corner: the *Go zero value of an interface is a nil
interface*, and calling any method on it panics with the classic
`invalid memory address or nil pointer dereference` — even though the
same call on a nil `*Address` would have been safe thanks to protobuf's
nil-receiver-friendly getters.

To keep the "nil receiver is safe" guarantee through the `_Const`
boundary, `Slice` and `Map` both expose a `Zero()` method and
`_Map2.Get` leans on it for its miss branch.

* **`Slice[T].Zero() T`** / **`Map[K, V].Zero() V`**
  * For scalar element / value types: the ordinary Go zero value
    (`""`, `0`, `false`, …).
  * For `Constable` projections (the `NewSlice2` / `NewMap2` flavour):
    a **typed-nil view** — an interface value whose itab is the
    concrete message pointer type and whose data word is `nil`. The
    interface comparison `v == nil` is therefore `false`, but every
    scalar / enum / `bytes` getter on `v` safely returns the zero value
    instead of panicking.
* **`Map[K, V].Get(k)` on a miss** returns `(m.Zero(), false)`. The
  second return value is the *authoritative* presence flag; the first
  is deliberately chosen so that `v.GetX()` is always safe to call,
  with or without a preceding `ok` check.

Two recommended miss-safe patterns fall out of this:

```go
// A) With an iter.Seq-aware helper (e.g. github.com/samber/lo/it):
//    pass Zero() as the fallback so the result is always a live view.
addr := loi.FindOrElse(
    s.ConstPrevAddresses().Values(),
    s.ConstPrevAddresses().Zero(),
    func(a Address_Const) bool { return a.GetZip() == "12345" },
)
_ = addr.GetCity() // safe even if no element matches

// B) With a plain Map lookup: trust ok for presence, use v regardless.
if v, ok := m.Get(key); ok {
    use(v)
} else {
    _ = v.GetCity() // safe: v is a typed-nil view, not a nil interface
}
```

Equivalently, a hand-rolled find over `Values()` / `All()` can use
`s.Zero()` as its loop-local default without importing any third-party
helper — this is exactly what `TestPerson_Slice_Zero` in
`examples/gen/go/nested/nested_const_test.go` exercises.

### Debug printing

`Slice` / `Map` values returned by `goconst.NewSlice(...)` /
`NewSlice2(...)` / `NewMap(...)` / `NewMap2(...)` all implement
`fmt.Stringer`. Printing one with `fmt.Print*`, `log.Print*`, or `%v`
produces exactly the same output as printing the raw `[]T` / `map[K]V`
would — no extra `Slice[...]` / `Map[...]` wrapper, no intermediate
`slices.Collect` / `maps.Collect` step needed:

```go
s := goconst.NewSlice2(p.GetPrevAddresses())
fmt.Println(s)                     // == fmt.Println(p.GetPrevAddresses())

m := goconst.NewMap2(p.GetAddressBook())
fmt.Println(m)                     // == fmt.Println(p.GetAddressBook())
```

For message-element variants (`NewSlice2` / `NewMap2`) the underlying
protobuf messages are printed directly, so you get their rich built-in
prototext-style `String()` rather than opaque interface addresses.

Key design points:

* **Scalars / enums / `bytes`** keep the stdlib Go type and reuse the
  concrete `*Message`'s getter — no companion is emitted and the
  interface lists the plain `GetName()` name.
* **Singular message fields** switch to the callee's `T_Const` view. A
  `ConstAddr()` companion on `*Message` is a one-liner returning
  `x.GetAddr()` directly — no explicit `.AsConst()` hop is emitted,
  because `*Address` itself implements `Address_Const`, so Go's implicit
  interface conversion performs the cast at zero cost. This also
  preserves proto3's nil-safe getter semantics: a typed-nil `*Address`
  becomes a non-nil `Address_Const` interface value whose scalar
  getters still return zero values instead of panicking.
* **Repeated fields** switch from `[]T` to `goconst.Slice[T_Const]` (or
  `goconst.Slice[T]` for scalar element types). The companion
  `ConstHistory()` delegates to `goconst.NewSlice2(...)` for message
  elements and to `goconst.NewSlice(...)` for scalar / excluded-package
  elements. Type arguments are omitted on purpose — Go 1.23+ constraint
  type inference recovers both the element type and the projected
  `_Const` type automatically.
* **Map fields** switch from `map[K]V` to `goconst.Map[K, V_Const]`
  (or `goconst.Map[K, V]` for scalar values), likewise delegating to
  `goconst.NewMap2(...)` or `goconst.NewMap(...)`.
* **`oneof`** is supported; each arm's getter is declared on the
  interface with the appropriate element type — scalar arms keep their
  plain `GetNote()` name, message arms get a `ConstLocation()`
  companion that returns the callee's `_Const` view.
* **Cross-package references** use `QualifiedGoIdent`, so imports for
  `*_Const` types from other generated packages are added automatically.
* **Zero-allocation cast**: because `*Message` itself implements
  `Message_Const`, `AsConst()` is literally `return x`. Benchmarks
  measure ~0.65 ns / 0 allocs for the cast, 0 allocs for singular
  message-field access, and ~3.5× faster map look-ups versus a wrapper-
  struct design that allocates a new view on every call.
* **`Clone()` escape hatch**: every `Message_Const` declares
  `Clone() *Message`, implemented on `*Message` as
  `return proto.Clone(x).(*Message)`. It is the canonical way to leave
  the read-only world — e.g. when a callee that received a `_Const`
  view needs an independent, mutable copy to hand to a writer-style
  API or to retain past the lifetime of the source. The return type is
  the concrete `*Message` (not `Message_Const`) by design: a copy that
  the caller cannot mutate would defeat the purpose, and the result
  can be re-wrapped via `clone.AsConst()` at zero cost when only a
  read-only handoff is needed downstream.

### Iteration performance: `Slice` / `Map.All()` vs raw range

`Slice[T].All()` / `Map[K, V].All()` return an `iter.Seq2` so that callers
can write the natural `for k, v := range view.All()` shape. That ergonomic
wrapper is **not free** — it carries a fixed, one-time setup cost on
every `All()` call, regardless of how many elements you then range over.
The two indexed-access escape hatches (`Len()` + `At(i)` for slices,
`Len()` + `Get(k)` for maps) keep the read-only contract while staying
fully zero-allocation.

Measured on `examples/gen/go/nested` (`go test -bench`, AMD EPYC 9754,
Go 1.23, 3-element fixtures), four iteration strategies × four container
shapes:

| Container                      | `for range raw`     | `for range stdlib.All(raw)` | `Len()` + `At/Get`    | `view.All()`           |
| ------------------------------ | ------------------- | --------------------------- | --------------------- | ---------------------- |
| `[]string` (NewSlice)          | 2.0 ns / 0 allocs   | 2.0 ns / 0 allocs           | 9.2 ns / 0 allocs     | **78 ns / 3 allocs**   |
| `[]*Address` (NewSlice2)       | 1.6 ns / 0 allocs   | 1.6 ns / 0 allocs           | 12.2 ns / 0 allocs    | **79 ns / 3 allocs**   |
| `map[string]string` (NewMap)   | 49 ns / 0 allocs    | 53 ns / 0 allocs            | 68 ns / 0 allocs      | **125 ns / 3 allocs**  |
| `map[int64]*Address` (NewMap2) | 49 ns / 0 allocs    | 51 ns / 0 allocs            | 65 ns / 0 allocs      | **128 ns / 3 allocs**  |

#### Why `view.All()` allocates 3 times

Look at the implementation in [`goconst.go`](goconst.go):

```go
func (c _Slice[T]) All() iter.Seq2[int, T] { return slices.All(c) }
func (c _Map[K, V]) All() iter.Seq2[K, V]  { return maps.All(c) }
// _Slice2 / _Map2 wrap an explicit `func(yield ...) { ... }` literal that
// projects each element through AsConst() before yielding it.
```

The free functions `slices.All` / `maps.All` are themselves zero-alloc
when used directly (`for _, v := range slices.All(p.Tags)` inlines the
returned closure into the caller's stack frame, as the **`for range
stdlib.All(raw)`** column above shows). What costs the 3 allocs is
forwarding the closure across a **generic method boundary**: by the time
the compiler is done instantiating `_Slice[T].All`, the inliner has
already given up on flowing the func literal through, so the closure
environment, the `iter.Seq2[…]` funcval header, and the wrapper
trampoline all escape. For `_Slice2` / `_Map2` the body is an explicit
projecting closure, which has the same fate for the same reason.

Crucially, **all three allocations are O(1) in the container length**:
each one is a small fixed-size header (closure env capturing the slice /
map descriptor, plus a couple of cursor words). `yield` itself is a
caller-frame value, so the per-element body of the loop runs zero-alloc
and the same 3 allocs / 80 B (slice) or 56 B (map) are observed whether
the container holds 1 element or 4096.

#### Recommendation

* **Default to `for k, v := range view.All()`.** The setup cost is
  ~80 ns and 3 allocs *per loop*, not per element. For a hot loop that
  iterates a few-element slice or map at human-request frequencies (RPC
  handlers, render paths, business logic), this is well under the noise
  floor of the surrounding work, and the readability win — same
  range-over-func shape as native slices and maps — is worth it.
* **Switch to `Len()` + `At(i)` / `Get(k)` only on the hot path.**
  Profile-guided rule of thumb: if a single goroutine is spinning the
  same `view.All()` loop millions of times per second over short
  containers (caches, per-tick game state, codec inner loops, tight
  reduction phases), the 3 allocs / call become visible in GC pause
  budgets long before the 80 ns / call shows up in CPU. Rewriting that
  one loop as
  ```go
  for i := range s.Len() {
      use(s.At(i))
  }
  // or, for maps, drive iteration off the underlying map and look each
  // key back up via the goconst view to keep its Get semantics:
  for k := range rawMap {
      v, _ := m.Get(k)
      use(k, v)
  }
  ```
  preserves the same read-only contract at zero allocation and ~10×
  lower per-call overhead.
* **Don't bypass the view to chase ns.** The direct-range-over-`raw`
  numbers in column one are listed for calibration only — using them in
  application code reintroduces the mutability that `_Const` exists to
  forbid. Stick with `view.All()` or the `Len()`/`At`/`Get` escape hatch.

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
* `foo.const.pb.go` — `*_Const` read-only interface views (from this plugin)

## Static check: `cmd/nilcompare`

`cmd/nilcompare` is a standalone [go/analysis] linter that rejects the
spelling the [typed-nil pitfall](#typed-nil-pitfall-and-isnil) warns
about at compile time:

```go
// diagnostic + auto-fix: use IsNil() instead
if p.ConstHome() == nil { ... }     // ✗ want `... use IsNil() instead`
if envelope.ConstHistory() != nil {} // ✗ want `... use !IsNil() instead`
switch view { case nil: ... }        // ✗ want `... use IsNil() in an if`

// fine — concrete pointers behave as you expect
if (*pb.Person)(nil) == nil { ... }  // ✓
// fine — type switches ask the dynamic-type question, not the value question
switch v.(type) { case nil: ... }    // ✓
```

Matching is **nominal** rather than structural: an interface is flagged
only if its declaration transitively embeds `goconst.DoNotCompare` via
an `EmbeddedType` chain. Interfaces that merely happen to declare an
`IsNil() bool` method on their own are *not* flagged, so custom types
that coincidentally share the shape are left alone.

Every reported diagnostic carries a machine-applicable `SuggestedFix`:
`x == nil` rewrites to `x.IsNil()`, `x != nil` rewrites to
`!x.IsNil()`. Switch-case uses of `case nil:` are reported without a
fix (the correct rewrite depends on whether the author wanted an
if/else chain), so they show up as manual TODOs.

### Ways to run it

```bash
# 1. Standalone binary (go vet-compatible)
go install github.com/Kybxd/goconst/cmd/nilcompare@latest
nilcompare ./...
# or, as a vet tool:
go vet -vettool=$(which nilcompare) ./...

# 2. Directly from source — no install needed
go run github.com/Kybxd/goconst/cmd/nilcompare ./...
```

**golangci-lint v2 module plugin.** Register the plugin package in
`.custom-gcl.yml` and enable it in `.golangci.yml`:

```yaml
# .custom-gcl.yml — build a custom golangci-lint binary that embeds the plugin
version: v2.1.0
name: golangci-lint-nilcompare
destination: ./bin
plugins:
  - module: github.com/Kybxd/goconst
    import: github.com/Kybxd/goconst/cmd/nilcompare/plugin
    version: latest        # or pin to a tagged release / pseudo-version
```

```yaml
# .golangci.yml — enable the plugin like any other linter
linters-settings:
  custom:
    nilcompare:
      type: module
      description: forbid comparing DoNotCompare-bearing interfaces to nil
linters:
  enable:
    - nilcompare
```

The analyzer is a no-op on packages that do not (transitively) import
`github.com/Kybxd/goconst`, so enabling it repo-wide is cheap.

[go/analysis]: https://pkg.go.dev/golang.org/x/tools/go/analysis

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
keeps the concrete `*Type` in the enclosing `_Const` interface (and
therefore emits no `Const<Name>` companion for it at all, since the
signature already matches the concrete getter):

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
   package out of `exclude_packages`, the generated `_Const` interface
   will declare a getter returning e.g. `timestamppb.Timestamp_Const` —
   a type that does not exist — and the file will not compile.

**Rule of thumb:** exclude every WKT package you import. The single
recursive glob `google.golang.org/protobuf/types/known/**` matches all
of them (`.../timestamppb`, `.../durationpb`, `.../anypb`,
`.../wrapperspb`, …, including any nested subpackages) and is the
recommended default for projects that touch any WKT.

## Project layout

```
.
├── goconst.go                  # runtime Slice / Map interfaces (imported by generated code)
├── cmd/
│   ├── protoc-gen-go-const/    # the protobuf plugin binary (package main)
│   └── nilcompare/             # static-check linter for `view == nil` misuse
│       ├── main.go             # singlechecker / go vet-compatible driver
│       ├── analyzer/           # the go/analysis Analyzer + analysistest suite
│       └── plugin/             # golangci-lint v2 module-plugin entrypoint
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
| Go                             | 1.23.0 (for stdlib [`iter`](https://pkg.go.dev/iter))   |
| `google.golang.org/protobuf`   | v1.36.11                                                |
| `buf.build/protocolbuffers/go` | v1.36.11 (kept in sync with the above)                  |
| proto editions supported       | proto2 → edition 2024 (via `FEATURE_SUPPORTS_EDITIONS`) |

When bumping `google.golang.org/protobuf` in `go.mod`, bump the
`protocolbuffers/go` remote tag in your `buf.gen.yaml` to the same
version so the generated `.pb.go` and the ambient runtime stay aligned.

## License

[MIT](LICENSE)