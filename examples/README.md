# Examples: testing protoc-gen-go-const with buf

These protos are hand-crafted to cover **every branch** of the generator
in [`../cmd/protoc-gen-go-const/main.go`](../cmd/protoc-gen-go-const/main.go).
They double as a living integration test: the generated `.pb.go` /
`.const.pb.go` files are checked in as **golden output**, and each
generated Go package ships a `*_const_test.go` that exercises the
emitted `*_Const` interfaces (run with `go test ./examples/...`). After
any change to the generator, regenerate here and inspect the diff under
`gen/go/`, then run the tests.

For the *why* and *how* of the `*_Const` views, the plugin wiring, and
the `--exclude_packages` flag (including the rule about well-known
types), see the [root README](../README.md).

## What each proto covers

| proto file                      | What it exercises                                                                                                                                                                        |
| ------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `proto/scalar/scalar.proto`     | every scalar kind + enum + proto3 `optional`                                                                                                                                             |
| `proto/nested/nested.proto`     | nested messages, repeated scalar/message, map with scalar/message value, recursion                                                                                                       |
| `proto/oneof/oneof.proto`       | `oneof` arms (scalar + cross-file message)                                                                                                                                               |
| `proto/external/external.proto` | a standalone package used as the `--exclude_packages` target                                                                                                                             |
| `proto/importer/importer.proto` | cross-package references to an excluded in-repo package, a non-excluded in-repo package, **and a well-known type (`google.protobuf.Timestamp`)**, in singular / repeated / map positions |

## Generate

```bash
cd examples
buf generate
```

You do **not** need to `go install protoc-gen-go` or `go build` this
repo's plugin beforehand — see the [root README](../README.md#prerequisites)
for the rationale and the pinned versions used by [buf.gen.yaml](buf.gen.yaml).

Output goes to `examples/gen/go/<leaf>/`, two files per proto:

* `xxx.pb.go`       — standard protobuf Go structs
* `xxx.const.pb.go` — our `*_Const` read-only interface views

This directory intentionally has **no `.gitignore`**: the generated
files are checked in so they act as golden output for the generator.
After running `buf generate`, the diff in `examples/gen/` shows exactly
how your changes affect the produced code.

## Run the tests

From the repo root:

```bash
go test ./examples/...
```

Each generated Go package has a sibling `*_const_test.go` that exercises
the emitted `*_Const` interface against the concrete `*Message`.

## Toggling `--exclude_packages`

[`buf.gen.yaml`](buf.gen.yaml) ships with two `exclude_packages` entries
enabled — one exact path, one glob:

```yaml
- exclude_packages=github.com/Kybxd/goconst/examples/gen/go/external
- exclude_packages=google.golang.org/protobuf/types/known/*
```

Each entry is matched against a field's owning Go import path with
[`path.Match`][path.Match] semantics, so the second line excludes
**every** WKT subpackage (timestamppb, durationpb, anypb, wrapperspb, …)
in one line.

[path.Match]: https://pkg.go.dev/path#Match

With both on you can verify (mainly in
[`gen/go/importer/importer.const.pb.go`](gen/go/importer/importer.const.pb.go)):

* No `External_Const` interface is emitted for the `external` package
  (its `.const.pb.go` may be absent entirely).
* Inside `Envelope_Const`:
  * `GetExt()`           returns `*external.External` (concrete type,
    signature unchanged from the concrete getter, so no `Const` suffix).
  * `ConstExtras()`   returns `goconst.Slice[*external.External]`.
  * `ConstExtMap()`   returns `goconst.Map[string, *external.External]`.
  * `GetCreatedAt()`     returns `*timestamppb.Timestamp` (unchanged).
  * `ConstHistory()`  returns `goconst.Slice[*timestamppb.Timestamp]`.
  * `ConstTsMap()`    returns `goconst.Map[string, *timestamppb.Timestamp]`.
  * None of the emitted companions call `.AsConst()` on excluded values.

To see the **opposite** behaviour for the in-repo `external` package,
comment its `exclude_packages=...` line out and rerun `buf generate`:
the same getters will then return `External_Const` views (and be renamed
to `ConstExt() / ConstExtras() / ConstExtMap()`) with
`.AsConst()` chained under the hood.

> ⚠️ Do **not** narrow / remove the WKT glob without first removing
> the WKT fields from `importer.proto` — the output would reference a
> non-existent `timestamppb.Timestamp_Const` and fail to compile. See
> [root README → `--exclude_packages`](../README.md#flag---exclude_packages)
> for the general rule.
