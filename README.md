# goconst

[![Go Reference](https://pkg.go.dev/badge/github.com/Kybxd/goconst.svg)](https://pkg.go.dev/github.com/Kybxd/goconst)

A [`protoc`](https://protobuf.dev) / [`buf`](https://buf.build) plugin
(`protoc-gen-go-const`) that generates a **read-only struct view** for
every `message` in your `.proto` files, alongside the standard
`protoc-gen-go` output. The goal is to let API boundaries (service
layers, caches, event handlers, goroutine handoffs, …) express *"I
only read this message"* at the type level, without copying the
protobuf or writing hand-maintained DTOs.

For each message `Foo` the plugin emits, in `foo.const.pb.go`:

| Symbol                              | What it is                                                                                  |
| ----------------------------------- | ------------------------------------------------------------------------------------------- |
| `type Foo_Const struct { … }`       | Read-only wrapper, one unexported `p *Foo` field — Go's type system closes every mutation path at compile time. |
| `Foo_ConstSlice` / `Foo_ConstMap[K]`| Go 1.24 type aliases for `goconst.Slice2[Foo_Const, *Foo]` / `goconst.Map2[K, Foo_Const, *Foo]` — short return types on getters. |
| `(*Foo).AsConst() Foo_Const`        | Zero-allocation cast (single-pointer struct returned in a register).                        |
| `(Foo_Const).Get<Field>()`          | One-line forwarder per field; scalar / `bytes` / enum keep their stdlib type, message / `repeated` / `map` return the view-native type. |
| `(Foo_Const).IsNil() bool`          | The only supported nil-check; `view == nil` is a **compile error**, not a typed-nil footgun. |
| `(Foo_Const).Clone() *Foo`          | Escape hatch — `proto.Clone(c.p).(*Foo)` deep copy (re-wrap via `clone.AsConst()` at zero cost). A nil-backed view returns a typed-nil `*Foo`, matching `proto.Clone`'s own behaviour. |
| `(Foo_Const).Equal(other Foo_Const) bool` | Semantic equality via `proto.Equal(c.p, other.p)` — the supported substitute for `==`, which is a compile error on view structs. |
| `(Foo_Const).ToAny() (*anypb.Any, error)` | One-line bridge to `*anypb.Any` via `anypb.New(c.p)` — useful when packing a read-only view into an `Any`-typed field without first re-exposing `*Foo`. |
| `(Foo_Const).String() string`       | Direct forward to `c.p.String()` — byte-for-byte identical to the raw message's prototext. |

Repeated and map fields go through
[`goconst.Slice`](goconst.go) / [`goconst.Slice2`](goconst.go) /
[`goconst.Map`](goconst.go) / [`goconst.Map2`](goconst.go) — small
read-only collection structs that preserve `len`, indexed / keyed
lookup, and ranged iteration while denying `s[i] = x`,
`append(s, …)`, `copy(s, …)`, `clear(s)`, `m[k] = v`, `delete(m, k)`
at the Go type level. The `*2` flavours additionally project each
element through its `AsConst()` view on access, so callers see the
callee's `_Const` wrapper rather than the concrete `*Message`.

## Why

Generated `protoc-gen-go` structs expose every field as a mutable Go
field. Once a `*Message` crosses an API boundary the callee can write
to it, sort its slices in place, overwrite map values, etc. — and the
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
	anypb "google.golang.org/protobuf/types/known/anypb"
)

// Envelope_Const is a read-only wrapper view of *Envelope.
type Envelope_Const struct {
	_ goconst.DoNotCompare // makes `view == view` a compile error; unreachable by name
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
	return proto.Clone(c.p).(*Envelope)
}

func (c Envelope_Const) Equal(other Envelope_Const) bool {
	return proto.Equal(c.p, other.p)
}

func (c Envelope_Const) ToAny() (*anypb.Any, error) {
	return anypb.New(c.p)
}

func (c Envelope_Const) String() string {
	return c.p.String()
}
```

(For a full end-to-end output including cross-package imports,
`*timestamppb.Timestamp` fields and `Slice` / `Map` over imported
messages, see [`examples/gen/go/importer/importer.const.pb.go`](examples/gen/go/importer/importer.const.pb.go).)

`goconst.Slice[T]` / `goconst.Slice2[T, E]` / `goconst.Map[K, V]` /
`goconst.Map2[K, V, E]` are concrete struct types (see
[goconst.go](goconst.go)) whose sole field is an unexported backing
slice / map. Their full surface is:

```**go**
// Constable is the witness that *Message participates in the _Const scheme.
type Constable[T any] interface{ AsConst() T }

// Cloneable is the witness that a wrapper view T can deep-copy itself into E.
type Cloneable[E any] interface{ Clone() E }

// Slice / Slice2 — Slice[T] stores T (scalar / excluded-package elements);
// Slice2[T, E] stores E (e.g. *Address) and projects to T (e.g. Address_Const).
func (Slice[T])     Len() int / At(i int) T / All() iter.Seq2[int, T] / Values() iter.Seq[T]
                  / IsNil() bool / String() string / Clone() []T
func (Slice2[T, E]) Len() int / At(i int) T / All() iter.Seq2[int, T] / Values() iter.Seq[T]
                  / IsNil() bool / String() string / Clone() []E

// Map / Map2 — same split: Map[K, V] stores V; Map2[K, V, E] stores E and projects to V.
// Get on a miss returns (zeroV, false); zeroV of a _Const view is nil-backed and safely readable.
func (Map[K, V])      Len() int / Get(k K) (V, bool) / Has(k K) bool / All() iter.Seq2[K, V]
                    / Keys() iter.Seq[K] / Values() iter.Seq[V]
                    / IsNil() bool / String() string / Clone() map[K]V
func (Map2[K, V, E])  Len() int / Get(k K) (V, bool) / Has(k K) bool / All() iter.Seq2[K, V]
                    / Keys() iter.Seq[K] / Values() iter.Seq[V]
                    / IsNil() bool / String() string / Clone() map[K]E

// Constructors — the plugin emits a one-liner per repeated / map field.
// Type arguments are recovered automatically by Go's constraint type inference.
func NewSlice [T any]                                       (s []T)    Slice[T]
func NewSlice2[T Cloneable[E], E Constable[T]]              (s []E)    Slice2[T, E]
func NewMap   [K comparable, V any]                         (m map[K]V) Map[K, V]
func NewMap2  [K comparable, V Cloneable[E], E Constable[V]](m map[K]E) Map2[K, V, E]
```

`Values()` / `Keys()` return `iter.Seq[…]` so the views plug straight
into stdlib sinks (`slices.Collect`, `slices.Sorted`, `maps.Collect`,
…) and any `iter.Seq`-aware third-party helper such as
`github.com/samber/lo/it` — higher-level algorithms (`ContainsBy`,
`Find`, `MinBy`, …) live there rather than on these types.

`Clone()` on every collection view returns a fresh, fully-independent
header whose mutation never reaches back into the view:

* **`Slice[T].Clone() []T` / `Map[K, V].Clone() map[K]V`** pick a
  per-element strategy once at entry by type-switching on the static
  element type — `proto.Message` elements (excluded-package or WKT
  messages) are deep-copied via `proto.Clone`, `[]byte` elements are
  detached via `bytes.Clone`, every other shape is bulk-copied
  (matching `slices.Clone` / `maps.Clone`).
* **`Slice2[T, E].Clone() []E` / `Map2[K, V, E].Clone() map[K]E`**
  deep-copy each element / value by routing through the wrapper's
  own `AsConst().Clone()` pair — fully static dispatch, no runtime
  type assertion, and the result is the concrete `[]*Foo` /
  `map[K]*Foo` ready to mutate. As an alternative, calling
  `parent.Clone()` on the enclosing `Foo_Const` deep-copies the whole
  message tree (including its nested `repeated` / `map` fields) in
  one step.

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

m := p.AsConst().GetAttributes()     // goconst.Map[string, string]
m["k"] = "v"                         // compile error: cannot index
delete(m, "k")                       // compile error: first argument to delete must be a map
clear(m)                             // compile error (same as above)
```

Short of `unsafe` / `reflect`, a consumer outside the `goconst` /
generated package has no syntactic way to reach the payload, so the
read-only contract is enforced by the Go type system rather than by
convention or a runtime check.

#### Limits: two leak boundaries

The guarantee above is **maximal, not absolute**. Two narrow
categories of fields return values whose Go type is itself mutable,
and the wrapper cannot interpose without changing the public return
type or paying a per-call deep-copy.

**1. `bytes` fields return a raw `[]byte` aliased to the message.**
The slice header is a fresh value copy, but the backing array is
shared — `view.GetFBytes()[0] = 0xFF` mutates the message in place.
This is the only mutation path `*_Const` does **not** close at the
type level. The alternative — `bytes.Clone(...)` on every getter —
would force a `make + memcpy` on every read, the wrong default for
a library whose other getters are zero-cost. Callers who need a
writable copy take it explicitly:

```go
b := bytes.Clone(view.GetFBytes())   // independent buffer
b[0] = 0xFF                          // safe
```

The same caveat applies to `[]byte` elements inside `repeated bytes`
and `map<…, bytes>`: `Slice[[]byte].At(i)` / `Map[K, []byte].Get(k)`
share their backing arrays. The collection-level escape hatch is
`Slice.Clone()` / `Map.Clone()`, which deep-copies every element via
`bytes.Clone`.

**2. Fields whose message type has no `*_Const` view.**
For fields whose message type comes from a package matched by
`--exclude_packages` (or from the auto-excluded
`google.golang.org/protobuf/types/known/**` subtree), the plugin has
no read-only handle to project to and forwards the **concrete
`*Message` pointer** verbatim:

```go
// timestamppb.Timestamp is auto-excluded → forwarded as *timestamppb.Timestamp.
func (c Envelope_Const) GetCreatedAt() *timestamppb.Timestamp {
    return c.p.GetCreatedAt()
}
```

The caller cannot reach `c.p`, but they *can* call
`view.GetCreatedAt().Seconds = 0` and mutate the underlying message.
The same applies to `repeated` / `map` fields whose element type is
excluded — they are returned as `goconst.Slice[*ExternalMsg]` /
`goconst.Map[K, *ExternalMsg]`, which prevents mutation of the
slice / map header but still hands out raw pointers element-wise.
Mitigation is `--exclude_packages` discipline: every excluded
package is one mutation surface that escapes the contract.

**Summary.** `goconst` provides the strongest read-only guarantee
Go's type system allows without copying or sacrificing zero-cost
forwards. It is not a sandbox — a determined caller with `unsafe`,
`reflect`, a raw `bytes` slice, or a pointer to an excluded message
can still reach through. Treat `*_Const` as the type-checked
spelling of "I will not mutate this", not as a runtime enforcement
boundary.

### `IsNil()` and the typed-nil footgun

Because `Foo_Const` is a struct (not an interface), `view == nil` is
a **compile error** rather than the classic Go typed-nil silent
mismatch. The only supported nil-check is `IsNil()`:

```go
home := p.AsConst().GetHome()        // Address_Const (struct value)
if home == nil { ... }               // ✗ compile error: cannot compare struct to nil

if home.IsNil() {                    // ✓
    // no Home set — fall back, skip, log, ...
} else {
    use(home.GetStreet())
}
```

Nil-safe reads are preserved: `home.GetStreet()` on a nil-backed
view returns `""` rather than panicking, because the scalar getter
forwards to `c.p.GetStreet()` and `protoc-gen-go` emits nil-safe
getters on concrete pointers.

For repeated / map fields, `IsNil()` reports whether the underlying
slice / map header is nil — i.e. the field is "absent" in the
proto-message sense. An empty-but-non-nil collection (e.g. one
explicitly assigned `[]T{}` / `map[K]V{}`) reports `false` because
it is "present, just empty". Use `Len() == 0` for the
"nothing to read" reading instead:

```go
if !envelope.GetHistory().IsNil() {
    for _, h := range envelope.GetHistory().All() { ... }
}

if envelope.GetHistory().Len() == 0 {
    // nothing to iterate, regardless of present-empty vs absent.
}
```

### `==` on views is a compile error

Every view struct — both the generated `<Message>_Const` wrappers and
the `Slice` / `Slice2` / `Map` / `Map2` collection views — embeds
`goconst.DoNotCompare`, a zero-width `[0]func()` marker. Because
`func` is not comparable, the whole containing struct becomes
non-comparable and `==` / `!=` is rejected at compile time:

```go
a := p.AsConst()
b := p.AsConst()
if a == b { ... }                    // ✗ compile error: struct containing
                                     //   goconst.DoNotCompare cannot be compared
```

Pointer-equality on a wrapper is rarely the question a caller actually
wants: two views of the *same* message are trivially equal, two views
of *semantically-equal* messages are not — and the latter is usually
what "are these views equal?" means. Letting the compiler reject the
spelling outright is cleaner than a runtime check or a linter.
For semantic equality, every generated wrapper exposes
`Equal(other Foo_Const) bool` — a one-line forwarder to
`proto.Equal(c.p, other.p)`. Callers who really want pointer identity
on the underlying message can still compare the two `*Foo` values
directly. The marker is zero-width, so it adds no memory and no
runtime cost.

### Miss-safe defaults: the zero value of a `_Const` view

`AsConst()` on a nil `*Foo` produces `Foo_Const{p: nil}`, and that
nil-backed view's scalar getters are still safe to call (forwarded to
nil-safe `protoc-gen-go` getters). The Go zero value of a `Foo_Const`
struct is the same `{p: nil}`, so anywhere a *zero* of the view type
appears it is already safely readable — no special helper required:

* **`Map[K, V].Get(k)` / `Map2[K, V, E].Get(k)` on a miss** return
  `(zeroV, false)`. `ok` is the authoritative presence flag; the
  first return value is deliberately a safely-readable zero (a nil
  concrete pointer for `Map[K, *Foo]`, a `Foo_Const{p: nil}` for
  `Map2`).
* **Fallbacks in `iter.Seq` helpers** (e.g. `lo/it.FindOrElse`): pass
  `Foo_Const{}` or a bare `var zero Foo_Const` as the default; scalar
  getters on the result are safe and `IsNil()` reports `true`.

```go
// A) iter.Seq helper with a zero-value fallback.
addr := loi.FindOrElse(
    s.GetPrevAddresses().Values(),
    Address_Const{}, // nil-backed; scalar getters safe
    func(a Address_Const) bool { return a.GetZip() == "12345" },
)
_ = addr.GetCity() // safe even on no match

// B) Map lookup — trust ok for presence, use v regardless.
if v, ok := m.Get(key); ok {
    use(v)
} else {
    _ = v.GetCity() // safe: v is a nil-backed view, not a raw nil pointer
}
```

### Performance

Every view type is a concrete struct fully visible at the call site,
so the Go inliner flows through the generic methods as if they were
hand-written on a native `[]T` / `map[K]V`. In practice:

* `AsConst()` and the four `New*` constructors are struct literals —
  **0 allocs**, returned in a register / kept on the stack.
* `Len()` / `At` / `Get` / `Has` / `IsNil()` and every scalar
  `Get<Field>` forwarder — **0 allocs**.
* `All()` / `Values()` / `Keys()` return `iter.Seq` / `iter.Seq2`
  funcvals that the inliner lifts into the caller's frame, so
  `for i, v := range view.All()` runs at **0 allocs** and matches the
  native `for i, v := range raw` baseline within noise.

This means there is no "hot-path escape hatch" to reach for: the
ergonomic `range view.All()` form and the indexed `Len()` + `At` form
have the same zero-allocation profile and essentially identical cost.
Pick whichever reads better.

Measured on `examples/gen/go/nested` (AMD EPYC 9754, Go 1.24,
3-element fixtures). `Len() + Get` on a map is omitted because it
would drive iteration off the raw map and then look up once per key,
which benchmarks the native map plus an extra lookup rather than a
view-native path.

| Container                      | `for range raw`     | `for range stdlib.All(raw)` | `Len()` + `At`        | `view.All()`           |
| ------------------------------ | ------------------- | --------------------------- | --------------------- | ---------------------- |
| `[]string` (Slice)             | 2.0 ns / 0 allocs   | 2.0 ns / 0 allocs           | 2.0 ns / 0 allocs     | **2.0 ns / 0 allocs**  |
| `[]*Address` (Slice2)          | 2.0 ns / 0 allocs   | 2.0 ns / 0 allocs           | 4.2 ns / 0 allocs     | **4.6 ns / 0 allocs**  |
| `map[string]string` (Map)      | 50 ns / 0 allocs    | 50 ns / 0 allocs            | —                     | **50 ns / 0 allocs**   |
| `map[int64]*Address` (Map2)    | 50 ns / 0 allocs    | 50 ns / 0 allocs            | —                     | **52 ns / 0 allocs**   |

The small gap on `Slice2` / `Map2` is the per-element `AsConst()`
projection — a single pointer-copy that does not allocate. Map
iteration is dominated by Go's own map iterator, not by the wrapper.

Reproduce with:

```bash
go test -bench='^BenchmarkNested_Range' -benchmem ./examples/gen/go/nested/...
```

The full benchmark matrix lives in
[`examples/gen/go/nested/nested_const_test.go`](examples/gen/go/nested/nested_const_test.go).

### Debug printing

Every view implements `fmt.Stringer` by forwarding to the underlying
`*Foo.String()` / `[]T` / `map[K]V`, so `fmt.Print*`, `log.Print*`
and `%v` produce exactly the same output as printing the raw value
would — no `Foo_Const{...}` / `Slice[...]` wrapper, no intermediate
`slices.Collect` step. For `Slice2` / `Map2` this means messages
render via their own prototext `String()` rather than as opaque
struct dumps.

## Installation & wiring

`protoc-gen-go-const` is a standard `protoc` plugin: it reads a
`CodeGeneratorRequest` from stdin and writes a `CodeGeneratorResponse`
to stdout, so any protoc-plugin host (`protoc`, [`buf`](https://buf.build),
or your own build tooling) can invoke it the same way it invokes
`protoc-gen-go`. Pick whichever workflow your project already uses —
no part of the plugin is buf-specific.

### Install the plugin

```bash
go install github.com/Kybxd/goconst/cmd/protoc-gen-go-const@latest
```

This drops a `protoc-gen-go-const` binary into `$(go env GOBIN)`
(falling back to `$(go env GOPATH)/bin`); make sure that directory is
on your `PATH` so `protoc` / `buf` can discover it like any other
`protoc-gen-*` plugin.

Consumers of the generated code must also have
`github.com/Kybxd/goconst` in their `go.mod` (a `go mod tidy` after
the first generation run picks it up automatically, since
`*.const.pb.go` imports it).

### Wire it into your generator

Run it alongside `protoc-gen-go` and write the output into the same
directory, so `foo.pb.go` and `foo.const.pb.go` land side by side.

**With `protoc`:**

```bash
protoc \
  --go_out=gen/go --go_opt=paths=source_relative \
  --go-const_out=gen/go --go-const_opt=paths=source_relative \
  -I proto \
  proto/foo.proto
```

**With `buf` (`buf.gen.yaml`):**

```yaml
version: v2
plugins:
  # Keep this tag in sync with google.golang.org/protobuf in your go.mod.
  - remote: buf.build/protocolbuffers/go:v1.36.11
    out: gen/go
    opt:
      - paths=source_relative

  - local: protoc-gen-go-const
    out: gen/go
    opt:
      - paths=source_relative
    strategy: all
```

(The `examples/` directory in this repo wires the plugin via
`local: ["go", "run", "../cmd/protoc-gen-go-const/main.go"]` so its
generated code always reflects the current source — that form is
useful when developing the plugin itself, but downstream projects
should prefer the installed binary above.)

For every `foo.proto` you will then get two files side by side:

* `foo.pb.go`       — standard protobuf Go structs (from `protoc-gen-go`)
* `foo.const.pb.go` — `*_Const` read-only struct views (from this plugin)

## Flag: `--exclude_packages`

Comma-separated / repeatable flag listing Go import path **glob
patterns** that should **not** get `*_Const` views. Each entry is
matched against the field's owning Go import path with
[doublestar][doublestar] (gitignore- / bash globstar-style)
semantics: a plain path matches exactly, `*` / `?` match within a
single path segment, and a recursive `**` matches any depth of
subpackages. When a field references a matching message, the plugin
keeps the concrete `*Type` signature on the wrapper's getter
(forwarding verbatim, no `AsConst()` / `Slice2` / `Map2` projection).

```yaml
opt:
  - exclude_packages=github.com/you/yourrepo/gen/go/proto/external
  - exclude_packages=github.com/somevendor/**
```

Typical reasons to exclude:

* **Third-party / vendored protos** you don't own and therefore don't
  run this generator against.
* **Project-internal boundary packages** that you want to keep on the
  concrete `*Message` API (e.g. a leaf whose callers all depend on
  `proto.Marshal` directly).

Remember that excluded packages are mutation surfaces that escape the
read-only contract (see "Limits" above) — exclude only what you must.

[doublestar]: https://github.com/bmatcuk/doublestar

### Built-in default: well-known types are auto-excluded

The plugin always applies one default exclude pattern on top of any
`--exclude_packages` you provide:

```
google.golang.org/protobuf/types/known/**
```

This recursive glob covers every WKT subpackage (`timestamppb`,
`durationpb`, `anypb`, `wrapperspb`, `structpb`, `fieldmaskpb`,
`emptypb`, …, including future additions). You never need to list
it; an explicit entry is accepted for backwards compatibility but
redundant.

WKTs are excluded by default because (a) they ship without any
`*_Const` / `AsConst()` — they're produced by the upstream
`protocolbuffers/go` plugin which this plugin does not run against,
so a wrapper referencing them would fail to compile, and (b) WKT
semantics live in hand-injected helpers (`AsTime`, `AsDuration`,
`UnmarshalTo`, `AsMap`, …) that a third-party generator cannot
reproduce — wrapping a WKT would strictly *lose* API surface
compared to the concrete pointer.

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
