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
* `func (x *Foo) Get<Msg|Repeated|Map>Const() ...` — one companion
  method per field whose signature had to change (singular message →
  `T_Const`, `[]T` → `goconst.Slice[T]`, `map[K]V` → `goconst.Map[K,V]`),
  attached directly to `*Foo` in the generated `foo.const.pb.go`.

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
import "github.com/Kybxd/goconst"

// Envelope_Const is a read-only interface view of Envelope.
//
// *Envelope itself satisfies this interface: scalar getters are inherited
// from the concrete type, and the message / repeated / map getters whose
// signatures differ are exposed via Const<Name> methods below.
type Envelope_Const interface {
	proto.Message

	GetId() string                                 // scalar: concrete getter reused
	ConstAddr() Address_Const                   // singular message
	ConstHistory() goconst.Slice[Address_Const] // repeated
	ConstByTag() goconst.Map[string, Address_Const] // map
}

// Compile-time assertion: dropping a field on the proto side turns into a
// build error instead of an interface-not-implemented runtime surprise.
var _ Envelope_Const = (*Envelope)(nil)

// AsConst is a zero-allocation cast: *Envelope already implements
// Envelope_Const, so the receiver is returned unchanged. The method is
// kept for readability and to give *Envelope a Constable[Envelope_Const]
// witness that parent messages can feed into goconst.NewSlice2 / NewMap2.
func (x *Envelope) AsConst() Envelope_Const { return x }

// Const<Name> companions are attached directly to *Envelope. They
// delegate to runtime constructors in the goconst package; type
// arguments are omitted so that Go 1.21+ constraint type inference
// recovers both the element type and its _Const projection.
func (x *Envelope) ConstAddr() Address_Const {
	return x.GetAddr().AsConst()
}
func (x *Envelope) ConstHistory() goconst.Slice[Address_Const] {
	return goconst.NewSlice2(x.GetHistory())
}
func (x *Envelope) ConstByTag() goconst.Map[string, Address_Const] {
	return goconst.NewMap2(x.GetByTag())
}
```

`goconst.Slice[T]` / `goconst.Map[K, V]` are defined in this repo's
root package (see [goconst.go](goconst.go)) and offer:

```go
type Slice[T any] interface {
    Len() int
    At(i int) T
    All() iter.Seq2[int, T]   // for i, v := range s.All()
    Values() iter.Seq[T]      // slices.Collect(s.Values()), slices.Sorted(...)
}

type Map[K comparable, V any] interface {
    Len() int
    Get(k K) (V, bool)
    Has(k K) bool
    All() iter.Seq2[K, V]     // for k, v := range m.All()
    Keys() iter.Seq[K]        // maps.Keys-shape, pipe into slices.Collect / Sorted
    Values() iter.Seq[V]      // maps.Values-shape, same idea
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

Key design points:

* **Scalars / enums / `bytes`** keep the stdlib Go type and reuse the
  concrete `*Message`'s getter — no companion is emitted and the
  interface lists the plain `GetName()` name.
* **Singular message fields** switch to the callee's `T_Const` view. A
  `ConstAddr()` companion on `*Message` calls
  `x.GetAddr().AsConst()`; because `AsConst()` is itself a return-x
  cast, the whole chain compiles to a single pointer load.
* **Repeated fields** switch from `[]T` to `goconst.Slice[T_Const]` (or
  `goconst.Slice[T]` for scalar element types). The companion
  `ConstHistory()` delegates to `goconst.NewSlice2(...)` for message
  elements and to `goconst.NewSlice(...)` for scalar / excluded-package
  elements. Type arguments are omitted on purpose — Go 1.21+ constraint
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
      # Optional, see "exclude_packages" below.
      # - exclude_packages=google.golang.org/protobuf/types/known/timestamppb
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

## Flag: `--exclude_packages`

Comma/repeat-style flag listing Go import paths that should **not** get
`*_Const` views. When a field references a message from an excluded
package, the plugin keeps the concrete `*Type` in the enclosing `_Const`
interface (and skips the `.AsConst()` hop on any emitted `Const<Name>`
companion getter):

```yaml
opt:
  - exclude_packages=github.com/you/yourrepo/gen/go/proto/external
  - exclude_packages=google.golang.org/protobuf/types/known/timestamppb
```

Typical use cases:

1. **Third-party / vendored protos** you don't own and therefore don't
   run this generator against.
2. **Well-known types** (`google.protobuf.Timestamp`, `Duration`, `Any`,
   `Wrappers*`, …). These are produced by the upstream
   `protocolbuffers/go` plugin and **ship without any `*_Const` /
   `AsConst()`**. If you import a WKT in your own proto and leave its Go
   package out of `exclude_packages`, generated code will reference e.g.
   `timestamppb.Timestamp_Const` and call `.AsConst()` on a
   `*timestamppb.Timestamp`, which will not compile.

**Rule of thumb:** for every WKT you import, add its Go import path to
`exclude_packages` (e.g. `.../timestamppb`, `.../durationpb`,
`.../anypb`, `.../wrapperspb`, …).

## Project layout

```
.
├── goconst.go                  # runtime Slice / Map interfaces (imported by generated code)
├── cmd/protoc-gen-go-const/    # the plugin binary (package main)
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
| Go                             | 1.24.5 (for stdlib [`iter`](https://pkg.go.dev/iter))   |
| `google.golang.org/protobuf`   | v1.36.11                                                |
| `buf.build/protocolbuffers/go` | v1.36.11 (kept in sync with the above)                  |
| proto editions supported       | proto2 → edition 2024 (via `FEATURE_SUPPORTS_EDITIONS`) |

When bumping `google.golang.org/protobuf` in `go.mod`, bump the
`protocolbuffers/go` remote tag in your `buf.gen.yaml` to the same
version so the generated `.pb.go` and the ambient runtime stay aligned.

## License

[MIT](LICENSE)