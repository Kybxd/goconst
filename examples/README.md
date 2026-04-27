# Examples: testing protoc-gen-go-const with buf

These protos are hand-crafted to cover **every branch** of the generator
in [`../cmd/protoc-gen-go-const/main.go`](../cmd/protoc-gen-go-const/main.go).
They double as a living integration test: after any change to the
generator, regenerate here and inspect the diff under `gen/go/`.

For the *why* and *how* of the `*_Const` views, the plugin wiring, and
the `--exclude_packages` flag (including the rule about well-known
types), see the [root README](../README.md).

## What each proto covers

| proto file                               | What it exercises                                                                                                                                                                        |
| ---------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `proto/testdata/scalar/scalar.proto`     | every scalar kind + enum + proto3 `optional`                                                                                                                                             |
| `proto/testdata/nested/nested.proto`     | nested messages, repeated scalar/message, map with scalar/message value, recursion                                                                                                       |
| `proto/testdata/oneof/oneof.proto`       | `oneof` arms (scalar + cross-file message)                                                                                                                                               |
| `proto/testdata/external/external.proto` | a standalone package used as the `--exclude_packages` target                                                                                                                             |
| `proto/testdata/importer/importer.proto` | cross-package references to an excluded in-repo package, a non-excluded in-repo package, **and a well-known type (`google.protobuf.Timestamp`)**, in singular / repeated / map positions |

## Generate

```bash
cd examples
buf generate
```

You do **not** need to `go install protoc-gen-go` or `go build` this
repo's plugin beforehand — see the [root README](../README.md#prerequisites)
for the rationale and the pinned versions used by [buf.gen.yaml](buf.gen.yaml).

Output goes to `examples/gen/go/...`, two files per proto:

* `xxx.pb.go`       — standard protobuf Go structs
* `xxx.const.pb.go` — our `*_Const` read-only interface views

This directory intentionally has **no `.gitignore`**: the generated
files are checked in so they act as golden output for the generator.
After running `buf generate`, the diff in `examples/gen/` shows exactly
how your changes affect the produced code.

## Toggling `--exclude_packages`

[`buf.gen.yaml`](buf.gen.yaml) ships with two `exclude_packages` entries
enabled:

```yaml
- exclude_packages=github.com/Kybxd/goconst/examples/gen/go/testdata/external
- exclude_packages=google.golang.org/protobuf/types/known/timestamppb
```

With both on you can verify (mainly in
[`gen/go/testdata/importer/importer.const.pb.go`](gen/go/testdata/importer/importer.const.pb.go)):

* No `External_Const` interface is emitted for the `external` package
  (its `.const.pb.go` may be absent entirely).
* Inside `Envelope_Const`:
  * `GetExt()`        returns `*external.External` (concrete type).
  * `GetExtras()`     returns `goconst.Slice[*external.External]`.
  * `GetExtMap()`     returns `goconst.Map[string, *external.External]`.
  * `GetCreatedAt()`  returns `*timestamppb.Timestamp`.
  * `GetHistory()`    returns `goconst.Slice[*timestamppb.Timestamp]`.
  * `GetTsMap()`      returns `goconst.Map[string, *timestamppb.Timestamp]`.
  * None of the overridden getters call `.AsConst()` on excluded values.

To see the **opposite** behaviour for the in-repo `external` package,
comment its `exclude_packages=...` line out and rerun `buf generate`:
the same getters will then return `External_Const` views and call
`.AsConst()` under the hood.

> ⚠️ Do **not** remove the `timestamppb` entry without first removing
> the WKT fields from `importer.proto` — the output would reference a
> non-existent `timestamppb.Timestamp_Const` and fail to compile. See
> [root README → `--exclude_packages`](../README.md#flag---exclude_packages)
> for the general rule.
