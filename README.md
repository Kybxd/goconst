# goconst

A [`protoc`](https://protobuf.dev) / [`buf`](https://buf.build) plugin
(`protoc-gen-go-const`) that generates a **read-only interface view** for
every `message` in your `.proto` files, alongside the standard
`protoc-gen-go` output.

For each message `Foo`, it emits:

* `Foo_Const` — a Go interface with only the `Get*` accessors of `Foo`, so a
  function that takes `Foo_Const` physically cannot mutate the message.
* `func (x *Foo) AsConst() Foo_Const` — a zero-cost adapter (an embedded
  wrapper struct) to obtain that view from a concrete `*Foo`.

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

Render(u.AsConst()) // call site opts in, no copy
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

// Envelope_Const is a read-only interface of Envelope.
type Envelope_Const interface {
	proto.Message

	GetId() string
	GetAddr() Address_Const
	GetHistory() goconst.Slice[Address_Const]
	GetByTag() goconst.Map[string, Address_Const]
}

type _Envelope_Const struct{ *Envelope } // embeds concrete type

var _ Envelope_Const = (*_Envelope_Const)(nil)

func (x *Envelope) AsConst() Envelope_Const { return &_Envelope_Const{x} }

// Only getters whose signature differs from the embedded *Envelope's
// getters are overridden — all of them delegate straight to runtime
// constructors in the goconst package.
func (x *_Envelope_Const) GetAddr() Address_Const {
	return x.Envelope.GetAddr().AsConst()
}
func (x *_Envelope_Const) GetHistory() goconst.Slice[Address_Const] {
	return goconst.NewSlice2(x.Envelope.GetHistory())
}
func (x *_Envelope_Const) GetByTag() goconst.Map[string, Address_Const] {
	return goconst.NewMap2(x.Envelope.GetByTag())
}
```

`goconst.Slice[T]` / `goconst.Map[K, V]` are defined in this repo's
root package (see [goconst.go](goconst.go)) and offer:

```go
type Slice[T any] interface {
    Len() int
    At(i int) T
    All() iter.Seq2[int, T]   // for i, v := range s.All()
}

type Map[K comparable, V any] interface {
    Len() int
    Get(k K) (V, bool)
    Has(k K) bool
    All() iter.Seq2[K, V]     // for k, v := range m.All()
}
```

so callers keep O(1) length / indexed / keyed access *and* the familiar
range-over-func syntax, but lose `append` / slot assignment at the type
level. The two concrete implementations are provided by the `goconst`
package itself via

```go
// Scalar / excluded-package elements — pass values through unchanged.
func NewSlice[T any](s []T) Slice[T]
func NewMap[K comparable, V any](m map[K]V) Map[K, V]

// Message elements — project each element via its AsConst() method.
type Constable[T any] interface{ AsConst() T }
func NewSlice2[T any, E Constable[T]](s []E) Slice[T]
func NewMap2[K comparable, V any, E Constable[V]](m map[K]E) Map[K, V]
```

so the plugin only has to emit a **one-line override getter** per
repeated / map field — no per-field wrapper struct is generated.

Key design points:

* **Scalars / enums / `bytes`** keep the stdlib Go type and reuse the
  embedded `*Envelope`'s getter — no override is emitted.
* **Singular message fields** are lifted to the callee's `*_Const` view,
  and an override calls `x.Envelope.GetAddr().AsConst()`.
* **Repeated fields** switch from `[]T` to `goconst.Slice[T_Const]` (or
  `goconst.Slice[T]` for scalar element types). The override delegates
  to `goconst.NewSlice2(...)` for message elements and to
  `goconst.NewSlice(...)` for scalar / excluded-package elements.
  Type arguments are omitted on purpose — Go 1.21+ constraint type
  inference recovers both the element type and the projected `_Const`
  type automatically.
* **Map fields** switch from `map[K]V` to `goconst.Map[K, V_Const]`
  (or `goconst.Map[K, V]` for scalar values), likewise delegating to
  `goconst.NewMap2(...)` or `goconst.NewMap(...)`.
* **`oneof`** is supported; each arm's getter is declared on the
  interface with the appropriate element type (concrete for scalars,
  `_Const` for messages).
* **Cross-package references** use `QualifiedGoIdent`, so imports for
  `*_Const` types from other generated packages are added automatically.

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
interface (and skips the `.AsConst()` call in any emitted override):

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