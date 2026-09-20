# Examples: testing protoc-gen-go-const with buf

These protos are hand-crafted to cover **every branch** of the generator
in [`../cmd/protoc-gen-go-const/main.go`](../cmd/protoc-gen-go-const/main.go).
They double as a living integration test: the generated `.pb.go` /
`.const.pb.go` files are checked in as **golden output**, and each
generated Go package ships a `*_const_test.go` that exercises the
emitted `*_Const` struct wrappers (run with `go test ./examples/...`).
After any change to the generator, regenerate here and inspect the diff
under `gen/go/`, then run the tests.

For the *why* and *how* of the `*_Const` views, the plugin wiring, and
the `--exclude_packages` flag (including the rule about well-known
types), see the [root README](../README.md).

## What each proto covers

| proto file                      | What it exercises                                                                                                                                                                        |
| ------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `proto/scalar/scalar.proto`     | every scalar kind + enum + proto3 `optional`                                                                                                                                             |
| `proto/nested/nested.proto`     | nested messages, repeated scalar/message, map with scalar/message value, recursion                                                                                                       |
| `proto/recursive/recursive.proto` | messages whose `map` values close an instantiation cycle (direct `map<K, Self>`, indirect `A → B → A`, nested-type back-reference) — pins the [golang/go#79711][gobug] workaround   |
| `proto/oneof/oneof.proto`       | `oneof` arms (scalar + cross-file message)                                                                                                                                               |
| `proto/external/external.proto` | a standalone package used as the `--exclude_packages` target                                                                                                                             |
| `proto/importer/importer.proto` | cross-package references to an excluded in-repo package, a non-excluded in-repo package, **and a well-known type (`google.protobuf.Timestamp`)**, in singular / repeated / map positions |
| `proto/editions/proto2/proto2.proto`             | proto2: `required` / `optional` with `[default = …]`, proto2-only `group`, nested message, repeated / map-with-message                                                 |
| `proto/editions/edition2023/edition2023.proto`   | Edition 2023 (OPEN API): `features.field_presence` matrix (EXPLICIT / IMPLICIT / LEGACY_REQUIRED) + `features.enum_type = CLOSED`                                      |
| `proto/editions/edition2024/edition2024.proto`   | Edition 2024 (OPAQUE API by default): same feature matrix as 2023; pins that the const layer is API-level-agnostic — only `Get*` is forwarded, so OPAQUE works too     |

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
* `xxx.const.pb.go` — our `*_Const` read-only struct views

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
the emitted `*_Const` struct wrapper against the concrete `*Message`.

## `regression/` — guards that must *compile*

[`regression/aliascycle`](regression/aliascycle) is hand-written (not
generated) and lives outside `gen/go/` on purpose. It imports the
views generated from `proto/recursive/recursive.proto` and touches
their map getters.

Its value is the compilation itself. Instantiating a generic alias
such as `SelfMap_ConstMap[int64]` in a generated method signature
trips [golang/go#79711][gobug]: the **defining** package still builds,
but any package importing it crashes the compiler with

```
fatal error: all goroutines are asleep - deadlock!
cmd/compile/internal/types2.(*Named).unpack(...)
```

So a test next to `gen/go/recursive/` would pass even with the bug
present — the guard has to be a separate compilation unit. It is
covered by the ordinary `go build ./...` / `go test ./...` above.

Two things to know when touching this area:

* This is a **compiler crash, not a type error**. `go vet`, `gopls`
  and every `go/types`-based linter report success regardless, so CI
  must actually build the package.
* The generator's countermeasure is to write map getter return types
  out in full (`goconst.Map2[K, V_Const, *V]`) instead of using the
  `V_ConstMap[K]` alias. See
  [root README → Why map getters spell out `goconst.Map2`](../README.md#why-map-getters-spell-out-goconstmap2).
  The aliases themselves are still generated and remain safe in
  hand-written code.

[gobug]: https://github.com/golang/go/issues/79711

## Toggling `--exclude_packages`

[`buf.gen.yaml`](buf.gen.yaml) ships with one `exclude_packages` entry
enabled — an exact path covering the in-repo `external` package:

```yaml
- exclude_packages=github.com/Kybxd/goconst/examples/gen/go/external
```

The well-known-types subtree
(`google.golang.org/protobuf/types/known/**`) is excluded
*automatically* by the plugin and does not appear in `buf.gen.yaml`.
See the [root README → `--exclude_packages`](../README.md#flag---exclude_packages)
for why that default is hard-coded.

Each entry is matched against a field's owning Go import path with
[doublestar][doublestar] (gitignore- / bash globstar-style) semantics.

[doublestar]: https://github.com/bmatcuk/doublestar

With the explicit `external` exclude on (and the WKT subtree implicitly
excluded by the built-in default), you can verify (mainly in
[`gen/go/importer/importer.const.pb.go`](gen/go/importer/importer.const.pb.go)):

* No `External_Const` type is emitted for the `external` package
  (its `.const.pb.go` may be absent entirely).
* Inside `Envelope_Const`:
  * `GetExt()`           returns `*external.External` (concrete type,
    signature unchanged from the concrete getter, verbatim forward).
  * `GetExtras()`   returns `goconst.Slice[*external.External]`.
  * `GetExtMap()`   returns `goconst.Map[string, *external.External]`.
  * `GetCreatedAt()`     returns `*timestamppb.Timestamp` (unchanged
    — covered by the built-in WKT default exclude, no `--exclude_packages`
    entry needed).
  * `GetHistory()`  returns `goconst.Slice[*timestamppb.Timestamp]`.
  * `GetTsMap()`    returns `goconst.Map[string, *timestamppb.Timestamp]`.
  * None of the emitted forwarders call `.AsConst()` on excluded values.
  * `Clone()`            returns a fresh, mutable `*Envelope` (delegates
    to `proto.Clone`); the copy is independent of the source even
    across the excluded `*external.External` and WKT pointer fields,
    because `proto.Clone` walks the message via the protobuf
    reflection runtime and is unaffected by `--exclude_packages`.

To see the **opposite** behaviour for the in-repo `external` package,
comment its `exclude_packages=...` line out and rerun `buf generate`:
the same `GetExt()` / `GetExtras()` / `GetExtMap()` methods will then
return `External_Const` views with `.AsConst()` chained under the
hood, and the collection return types switch to their projecting
`Slice2` / `Map2` forms — `GetExtras()` to the alias
`External_ConstSlice` (= `goconst.Slice2[External_Const, *external.External]`)
and `GetExtMap()` to the written-out
`goconst.Map2[string, External_Const, *external.External]`.

The rule is: **`Slice2` / `Map2` (projecting) appear whenever the
element / value is a message from a non-excluded package**, and
`Slice` / `Map` appear for scalar element / value types and for
messages from excluded packages.

Note the spelling asymmetry — repeated getters use the
`_ConstSlice` alias, map getters write `goconst.Map2[…]` out in full.
Both denote the same types as the corresponding aliases; the map side
avoids `_ConstMap[K]` to dodge [golang/go#79711][gobug], as described
under [`regression/`](#regression--guards-that-must-compile) above.

> ℹ️ The WKT fields on `importer.proto` (`created_at`, `history`,
> `ts_map`) are kept on the concrete `*timestamppb.Timestamp` type by
> the plugin's built-in default exclude — there is no way to "narrow"
> that default from `buf.gen.yaml`, so removing the WKT fields from
> the proto is the only way to remove them from the generated output.
