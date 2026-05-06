package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/pflag"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

const version = "0.2.0"

// protoPackage is the import path of the runtime proto package. It is
// referenced by the generated _Const interfaces via an embedded
// proto.Message, so every _Const value is also a proto.Message.
const protoPackage = protogen.GoImportPath("google.golang.org/protobuf/proto")

// goconstPackage is the import path of this repo's runtime helper package,
// which exposes the read-only Slice / Map interfaces and the
// NewSlice / NewSlice2 / NewMap / NewMap2 constructors used by generated
// *_Const views for repeated / map fields.
const goconstPackage = protogen.GoImportPath("github.com/Kybxd/goconst")

// ---------------------------------------------------------------------------
// Plugin entry point
// ---------------------------------------------------------------------------
//
// Generation shape (single, no style flag):
//
//   - For every message Foo, emit `type Foo_Const interface { proto.Message;
//     Get<scalar>() T; Const<msg|list|map>() <const-type> }` and a
//     compile-time assertion `var _ Foo_Const = (*Foo)(nil)`.
//   - Make *Foo itself satisfy Foo_Const:
//   - Scalar / enum / bytes / excluded-package-message getters keep their
//     plain names and reuse the concrete message's existing method set.
//   - Fields whose signature differs (singular non-excluded message,
//     repeated, map) get a dedicated `Const<Name>` companion method
//     attached directly to *Foo in the generated `.const.pb.go` file.
//   - `func (x *Foo) AsConst() Foo_Const { return x }` — kept as an explicit,
//     zero-allocation "cast" entry point for readability and for code that
//     wants to pass a Constable[Foo_Const] into goconst.NewSlice2 / NewMap2.
//
// Having *Foo implement Foo_Const directly (instead of wrapping it in an
// unexported struct that embeds *Foo) is what makes AsConst() a pure
// return-receiver: benchmarks show 0 allocs / ~0.65 ns for AsConst(),
// ~3.5x faster Map.Get hits, and ~31% faster repeated-message iteration
// compared to a wrapper-struct design that allocated a fresh view on
// every AsConst() / repeated-message element access.
func main() {
	var flags pflag.FlagSet
	excludePackages := flags.StringSlice("exclude_packages", nil,
		"Repeatable flag listing Go package import path patterns that should "+
			"NOT receive *_Const interfaces. Each entry is matched against "+
			"the field's owning Go import path with doublestar (gitignore- / "+
			"bash globstar-style) semantics, so plain paths work as exact "+
			"matches, `*` / `?` match within a single path segment, and a "+
			"recursive `**` segment matches any number of subpackages (e.g. "+
			"`google.golang.org/protobuf/types/known/**` excludes every WKT "+
			"package, including nested ones, in one line). Fields whose "+
			"type comes from a matching package keep their concrete *Type "+
			"in the enclosing _Const view and do not get .AsConst() called "+
			"on them.")

	protogen.Options{
		ParamFunc: flags.Set,
	}.Run(func(gen *protogen.Plugin) error {
		gen.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL |
			pluginpb.CodeGeneratorResponse_FEATURE_SUPPORTS_EDITIONS)
		gen.SupportedEditionsMinimum = descriptorpb.Edition_EDITION_PROTO2
		gen.SupportedEditionsMaximum = descriptorpb.Edition_EDITION_2024

		for _, f := range gen.Files {
			if !f.Generate {
				// Transitive dependency: only types are needed, do not
				// emit a .const.pb.go for it.
				continue
			}
			NewGenerator(gen, f, *excludePackages).Generate()
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// Generator
// ---------------------------------------------------------------------------

// Generator carries the per-file state for emitting one .const.pb.go.
// Exactly one Generator is constructed per input .proto file that has
// protogen.File.Generate == true (see main above).
type Generator struct {
	// gen is the plugin-level handle, used to create the generated file
	// lazily in g() and to look up the invoking protoc's version.
	gen *protogen.Plugin
	// file is the input .proto being processed.
	file *protogen.File

	// once + genFile lazily create the output file on the first call to g()
	// so that proto files containing only excluded messages (and therefore
	// producing no _Const output) do not leave behind an empty stub.
	once    sync.Once
	genFile *protogen.GeneratedFile

	// excludePackagePatterns is the list of Go import path doublestar
	// glob patterns whose messages should be left as concrete *Type
	// references. Populated from the --exclude_packages flag; matched
	// with doublestar.Match in matchExcludePattern (used by both
	// shouldExcludeFile and shouldExcludeMessage). A plain (wildcard-
	// free) pattern degenerates to an exact-match check, so the legacy
	// "list of import paths" usage keeps working unchanged.
	excludePackagePatterns []string
}

// NewGenerator returns a Generator bound to a single input file. The
// excludePackages slice is trimmed and stored as a list of doublestar
// glob patterns.
func NewGenerator(gen *protogen.Plugin, file *protogen.File, excludePackages []string) *Generator {
	patterns := make([]string, 0, len(excludePackages))
	for _, pkg := range excludePackages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		patterns = append(patterns, pkg)
	}
	return &Generator{
		gen:                    gen,
		file:                   file,
		excludePackagePatterns: patterns,
	}
}

// shouldExcludeFile reports whether the input .proto's owning Go package
// is matched by any --exclude_packages pattern. Equivalent to calling
// shouldExcludeMessage on every top-level message in the file (they all
// share the same Go import path), so callers that want to short-circuit
// before iterating message-by-message can use this instead.
func (x *Generator) shouldExcludeFile(file *protogen.File) bool {
	return x.matchExcludePattern(string(file.GoImportPath))
}

// shouldExcludeMessage reports whether the plugin must NOT generate a _Const
// interface for the given message (and, when referenced from an enclosing
// message, must keep the concrete *Type signature without an AsConst()
// projection). Look-up is by the message's owning Go import path, matched
// against every pattern from --exclude_packages with doublestar semantics:
// `*` and `?` match within a single `/`-separated segment, and a recursive
// `**` segment matches any number of subpackages (e.g.
// `google.golang.org/protobuf/types/known/**` covers every WKT package,
// including nested ones).
//
// This is the per-reference variant: it must keep working for messages
// from other packages reached via field types (see fieldNeedsConstPrefix,
// fieldElemConstType, messageConstGoType), where the target package is
// not necessarily x.file's own package. For the top-level "is the whole
// input file excluded?" question, prefer [shouldExcludeFile].
func (x *Generator) shouldExcludeMessage(message *protogen.Message) bool {
	return x.matchExcludePattern(string(message.GoIdent.GoImportPath))
}

// matchExcludePattern reports whether pkgPath matches any of the
// --exclude_packages doublestar glob patterns. Shared by
// shouldExcludeFile and shouldExcludeMessage.
func (x *Generator) matchExcludePattern(pkgPath string) bool {
	for _, pattern := range x.excludePackagePatterns {
		// doublestar.Match only fails on a malformed pattern. We treat
		// that as "does not match" — the plugin already validated
		// nothing at flag parse time, so silently skipping a bad
		// pattern matches what the previous exact-match implementation
		// did with a typo'd entry.
		if ok, _ := doublestar.Match(pattern, pkgPath); ok {
			return true
		}
	}
	return false
}

// Generate walks every top-level message in the input file and emits its
// _Const API. The whole file is short-circuited up front via
// shouldExcludeFile (every top-level message in a .proto shares the same
// Go import path, so per-message exclusion would be redundant here).
// Nested messages are recursed into by genMessageConstAPI, so they are
// NOT iterated here.
func (x *Generator) Generate() {
	if x.shouldExcludeFile(x.file) {
		return
	}
	for _, message := range x.file.Messages {
		x.genMessageConstAPI(message)
	}
}

// g returns the output file, creating it (and writing its header) on the
// first call. Using sync.Once means a Generator whose only messages are
// excluded ends up never touching the filesystem — see NewGeneratedFile
// semantics in google.golang.org/protobuf/compiler/protogen.
func (x *Generator) g() *protogen.GeneratedFile {
	x.once.Do(func() {
		filename := x.file.GeneratedFilenamePrefix + ".const.pb.go"
		x.genFile = x.gen.NewGeneratedFile(filename, x.file.GoImportPath)
		x.genFile.P("// Code generated by protoc-gen-go-const. DO NOT EDIT.")
		x.genFile.P("// versions:")
		x.genFile.P("//  protoc-gen-go-const v", version)
		x.genFile.P("//  protoc              ", x.protocVersion())
		if x.file.Proto.GetOptions().GetDeprecated() {
			x.genFile.P("// ", x.file.Desc.Path(), " is a deprecated file.")
		} else {
			x.genFile.P("// source: ", x.file.Desc.Path())
		}
		x.genFile.P()
		x.genFile.P("package ", x.file.GoPackageName)
		x.genFile.P()
	})
	return x.genFile
}

// protocVersion formats the invoking protoc's version string for the
// generated file header, matching the shape protoc-gen-go itself prints.
func (x *Generator) protocVersion() string {
	v := x.gen.Request.GetCompilerVersion()
	if v == nil {
		return "(unknown)"
	}
	var suffix string
	if s := v.GetSuffix(); s != "" {
		suffix = "-" + s
	}
	return fmt.Sprintf("v%d.%d.%d%s", v.GetMajor(), v.GetMinor(), v.GetPatch(), suffix)
}

// ---------------------------------------------------------------------------
// Core emission
// ---------------------------------------------------------------------------

// genMessageConstAPI emits, for one message, the full set of declarations
// that make up the "direct" _Const shape:
//
//  1. The Message_Const interface. It embeds proto.Message (so every
//     _Const is also a proto.Message) and goconst.DoNotCompare (so every
//     _Const exposes an IsNil() predicate at the type level, steering
//     callers away from the typed-nil `view == nil` footgun). It then
//     lists every field as either its concrete getter (scalars / enums /
//     bytes / excluded-package messages) or a Const<Name> companion
//     (non-excluded message / repeated / map).
//  2. A compile-time assertion `var _ Message_Const = (*Message)(nil)` so
//     that dropping a field on the proto side surfaces as a build error
//     rather than an interface-not-implemented runtime surprise.
//  3. The AsConst() method, declared on *Message itself. Under this shape
//     it is a no-op cast (`return x`) — its sole purpose is readability
//     at call sites and satisfying goconst.Constable so that *Message can
//     be fed into goconst.NewSlice2 / NewMap2 by parent messages.
//  4. The IsNil() method, declared on *Message itself. It is the concrete
//     witness for the DoNotCompare interface embedded in Message_Const and
//     returns `x == nil` against the concrete receiver — which, unlike
//     the interface-level `view == nil`, reports the answer the caller
//     usually means.
//  5. One Const<Name> method per field that needs a companion getter.
//  6. Recursion into nested (non-map-entry) messages, so that a nested
//     Address or Contact type emits its own _Const API in the same file.
func (x *Generator) genMessageConstAPI(message *protogen.Message) {
	g := x.g()
	msgName := message.GoIdent.GoName

	// --- (1) The _Const interface -----------------------------------------
	//
	// Scalars / enums / bytes keep the concrete type: their _Const getter
	// and the concrete *Message getter have identical signatures, so the
	// message's existing method set satisfies the interface without any
	// new code emitted on our side.
	//
	// Fields whose signature differs (single messages projected into a
	// _Const view, repeated/map fields projected into goconst.Slice / Map)
	// are exposed through a Const<Name> companion, emitted further down.
	//
	// The emitted godoc is intentionally a single-line summary; the
	// "why" (typed-nil trap, satisfies-via-concrete *Message, etc.) lives
	// in the README so generated files stay diff-friendly.
	g.P("// ", msgName, "_Const is a read-only interface view of ", msgName, ".")
	g.P("type ", msgName, "_Const interface {")
	g.P(protoPackage.Ident("Message"))
	g.P(goconstPackage.Ident("DoNotCompare"))
	g.P()
	for _, field := range message.Fields {
		if x.fieldNeedsConstPrefix(field) {
			// Naming convention: fields whose signature differs from the
			// concrete *Message use a `Const<Name>` method, so the read-only
			// projection reads as a prefix qualifier rather than a suffix on
			// top of the `Get<Name>` family. Scalars / enums / bytes stay on
			// the standard `Get<Name>` getter inherited from the concrete
			// type and the interface lists them as such.
			g.P("Const", field.GoName, "() ", x.fieldConstType(field))
			continue
		}
		g.P("Get", field.GoName, "() ", x.fieldGoType(field))
	}
	// Clone returns a fresh, mutable deep copy of the underlying *Message
	// — the standard "escape hatch" out of the read-only view. The return
	// type is the concrete *Message (not Message_Const) on purpose: a
	// copy that the caller cannot mutate would be useless, and a *Message
	// can still be re-wrapped via AsConst() if desired.
	g.P("Clone() *", g.QualifiedGoIdent(message.GoIdent))
	g.P("}")
	g.P()

	// --- (2) Compile-time interface assertion -----------------------------
	g.P("var _ ", msgName, "_Const = (*", msgName, ")(nil)")
	g.P()

	// --- (3) AsConst: zero-allocation "cast" ------------------------------
	//
	// Because *Message already implements Message_Const, AsConst just
	// returns its receiver. Keeping the method (instead of asking callers
	// to spell out `Foo_Const(p)` at the call site) has two benefits:
	//   - it communicates intent ("I want the read-only view");
	//   - it gives *Message a Constable[Message_Const] witness so parent
	//     messages can project repeated/map fields via NewSlice2/NewMap2.
	//
	// Emitted godoc is a single-line summary by design (see (1) above).
	g.P("// AsConst returns x as its read-only ", msgName, "_Const view.")
	g.P("func (x *", msgName, ") AsConst() ", msgName, "_Const {")
	g.P("return x")
	g.P("}")
	g.P()

	// IsNil satisfies goconst.DoNotCompare. It is the exported nil-check
	// hook: comparing a *_Const interface value against the untyped nil
	// literal is almost always wrong under the direct-style scheme (a
	// nil *Message boxed into the interface yields a typed-nil iface
	// value that is != nil yet whose scalar getters still return zero
	// values), so the generator emits this method on *Message itself
	// and asks readers to use it instead.
	//
	// Emitted godoc is a single-line summary by design (see (1) above).
	g.P("// IsNil reports whether x is nil.")
	g.P("func (x *", msgName, ") IsNil() bool {")
	g.P("return x == nil")
	g.P("}")
	g.P()

	// Clone is the concrete witness for the Clone() method on
	// Message_Const. It delegates to proto.Clone, which performs a
	// proper deep copy through the protobuf reflection runtime
	// (handling unknown fields, extensions, and nested messages
	// correctly), and asserts the result back to *Message because
	// proto.Clone's static return type is the proto.Message interface.
	//
	// Emitted godoc is a single-line summary by design (see (1) above).
	g.P("// Clone returns a deep copy of x as a fresh, mutable *", msgName, ".")
	g.P("func (x *", msgName, ") Clone() *", msgName, " {")
	g.P("return ", protoPackage.Ident("Clone"), "(x).(*", msgName, ")")
	g.P("}")
	g.P()

	// --- (4) Const<Name> companions --------------------------------------
	for _, field := range message.Fields {
		if !x.fieldNeedsConstPrefix(field) {
			continue
		}
		x.genConstGetter(message, field)
	}

	// --- (5) Recurse into nested messages ---------------------------------
	//
	// Skip synthetic map-entry messages (e.g. Foo.AttributesEntry): they
	// are plumbing for the map<K,V> syntax in proto3 and are never meant
	// to be referenced directly by user code, so generating a _Const view
	// for them would be both useless and noisy.
	for _, nested := range message.Messages {
		if nested.Desc.IsMapEntry() {
			continue
		}
		x.genMessageConstAPI(nested)
	}
}

// fieldNeedsConstPrefix reports whether the field's signature on the _Const
// interface differs from its signature on the concrete *Message, and
// therefore whether a dedicated Const<Name> companion must be emitted.
//
// Three kinds of fields qualify:
//
//   - repeated fields: []T → goconst.Slice[T] (or Slice[T_Const]);
//   - map fields:      map[K]V → goconst.Map[K, V] (or Map[K, V_Const]);
//   - singular messages from a non-excluded package: *T → T_Const.
//
// Everything else (scalars, enums, bytes, and messages from excluded
// packages) has a signature-compatible concrete getter, so no companion
// method is needed and the interface simply lists the plain getter name.
func (x *Generator) fieldNeedsConstPrefix(field *protogen.Field) bool {
	if field.Desc.IsList() || field.Desc.IsMap() {
		return true
	}
	if field.Desc.Kind() == protoreflect.MessageKind || field.Desc.Kind() == protoreflect.GroupKind {
		// Excluded-package messages have no _Const view, so the signature
		// is identical to the concrete getter and no companion is needed.
		return !x.shouldExcludeMessage(field.Message)
	}
	return false
}

// genConstGetter emits one `func (x *Message) Const<Name>() <ret-type>`
// method on the concrete *Message, matching the signature declared on the
// _Const interface. List and map fields delegate to the runtime
// constructors goconst.NewSlice / NewSlice2 / NewMap / NewMap2; singular
// non-excluded messages recurse through their own AsConst().
//
// The caller must only invoke this for fields where fieldNeedsConstPrefix
// returned true — other fields satisfy the interface via the concrete
// getter and no emission is needed (or desired: emitting a duplicate
// Get<Name> method would be a build error).
func (x *Generator) genConstGetter(message *protogen.Message, field *protogen.Field) {
	g := x.g()
	msgName := message.GoIdent.GoName
	recv := fmt.Sprintf("(x *%s)", msgName)

	switch {
	case field.Desc.IsList():
		elemConstType := x.fieldElemConstType(field)
		// Message elements that are NOT in an excluded package expose a
		// Constable[T_Const] view, so we pick NewSlice2 which projects each
		// element through AsConst(). Everything else (scalars, enums, and
		// message elements from excluded packages) passes through as-is
		// via NewSlice.
		wrapAsConst := x.isMessageElem(field) && !x.shouldExcludeMessage(field.Message)
		retType := fmt.Sprintf("%s[%s]",
			g.QualifiedGoIdent(goconstPackage.Ident("Slice")), elemConstType)

		g.P("func ", recv, " Const", field.GoName, "() ", retType, " {")
		if wrapAsConst {
			// Type arguments are omitted on purpose: Go 1.23+ constraint
			// type inference recovers both E (the slice element type) and
			// T (from the Constable[T] constraint on E) from the argument,
			// so spelling them out triggers the "unnecessary type
			// arguments" diagnostic under gopls / revive.
			g.P("return ", g.QualifiedGoIdent(goconstPackage.Ident("NewSlice2")),
				"(x.Get", field.GoName, "())")
		} else {
			g.P("return ", g.QualifiedGoIdent(goconstPackage.Ident("NewSlice")),
				"(x.Get", field.GoName, "())")
		}
		g.P("}")
		g.P()

	case field.Desc.IsMap():
		// Map fields in protogen are modeled as synthetic entry messages
		// with two fields ("key" at Fields[0], "value" at Fields[1]); the
		// entry's IsMapEntry() is true and it is excluded from recursion
		// in genMessageConstAPI.
		keyField := field.Message.Fields[0]
		valField := field.Message.Fields[1]
		keyType := x.fieldGoType(keyField)
		valConstType := x.fieldElemConstType(valField)
		wrapAsConst := x.isMessageElem(valField) && !x.shouldExcludeMessage(valField.Message)
		retType := fmt.Sprintf("%s[%s, %s]",
			g.QualifiedGoIdent(goconstPackage.Ident("Map")), keyType, valConstType)

		g.P("func ", recv, " Const", field.GoName, "() ", retType, " {")
		if wrapAsConst {
			// Same type-inference rationale as NewSlice2 above.
			g.P("return ", g.QualifiedGoIdent(goconstPackage.Ident("NewMap2")),
				"(x.Get", field.GoName, "())")
		} else {
			g.P("return ", g.QualifiedGoIdent(goconstPackage.Ident("NewMap")),
				"(x.Get", field.GoName, "())")
		}
		g.P("}")
		g.P()

	case field.Desc.Kind() == protoreflect.MessageKind || field.Desc.Kind() == protoreflect.GroupKind:
		// Defensive: excluded-package messages should already have been
		// filtered out by fieldNeedsConstPrefix. Keep the guard so an
		// accidental direct call here does not emit a reference to a
		// non-existent T_Const type.
		if x.shouldExcludeMessage(field.Message) {
			return
		}
		retType := x.messageConstGoType(field.Message)
		g.P("func ", recv, " Const", field.GoName, "() ", retType, " {")
		// x.Get<Name>() is proto3's nil-safe singular getter returning a
		// typed *Address (possibly a typed nil when the field is unset).
		// Because *Address itself implements Address_Const under the
		// direct-style scheme, the return statement relies on Go's
		// implicit interface conversion — a typed-nil *Address becomes a
		// non-nil Address_Const interface value whose scalar getters
		// return zero values, preserving proto3's zero-on-unset behaviour
		// without an explicit .AsConst() hop.
		g.P("return x.Get", field.GoName, "()")
		g.P("}")
		g.P()
	}
}

// isMessageElem reports whether a repeated/map field's element type is a
// protobuf message (as opposed to a scalar, enum, or bytes). Used to decide
// between NewSlice / NewSlice2 (and NewMap / NewMap2).
func (x *Generator) isMessageElem(field *protogen.Field) bool {
	return field.Desc.Kind() == protoreflect.MessageKind || field.Desc.Kind() == protoreflect.GroupKind
}

// ---------------------------------------------------------------------------
// Type-string helpers
// ---------------------------------------------------------------------------

// fieldConstType returns the Go type string used as the return type for the
// given field's getter on the _Const interface. For repeated/map fields this
// is the matching goconst.Slice / goconst.Map instantiation; for singular
// messages it is the _Const interface type (or the concrete *Type for
// excluded packages); for scalars/enums/bytes it falls through to the
// concrete type.
func (x *Generator) fieldConstType(field *protogen.Field) string {
	g := x.g()
	switch {
	case field.Desc.IsList():
		elem := x.fieldElemConstType(field)
		return fmt.Sprintf("%s[%s]",
			g.QualifiedGoIdent(goconstPackage.Ident("Slice")), elem)
	case field.Desc.IsMap():
		keyField := field.Message.Fields[0]
		valField := field.Message.Fields[1]
		keyType := x.fieldGoType(keyField)
		valType := x.fieldElemConstType(valField)
		return fmt.Sprintf("%s[%s, %s]",
			g.QualifiedGoIdent(goconstPackage.Ident("Map")), keyType, valType)
	case field.Desc.Kind() == protoreflect.MessageKind || field.Desc.Kind() == protoreflect.GroupKind:
		return x.messageConstGoType(field.Message)
	default:
		return x.fieldGoType(field)
	}
}

// fieldElemConstType returns the Go type string for one element of a
// repeated/map field (the element type for lists, the value type for maps).
// Message elements are projected to their _Const interface view; scalar /
// enum / bytes elements are returned as-is. Excluded-package messages keep
// the concrete *Type pointer.
func (x *Generator) fieldElemConstType(field *protogen.Field) string {
	switch field.Desc.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if x.shouldExcludeMessage(field.Message) {
			return "*" + x.g().QualifiedGoIdent(field.Message.GoIdent)
		}
		return x.messageConstGoType(field.Message)
	default:
		return x.scalarFieldGoType(field)
	}
}

// messageConstGoType returns the _Const interface Go type string for the
// given message, routed through QualifiedGoIdent so that cross-package
// references trigger the correct import to be added to the generated file.
// Excluded packages fall back to the concrete *Type pointer.
func (x *Generator) messageConstGoType(msg *protogen.Message) string {
	g := x.g()
	if x.shouldExcludeMessage(msg) {
		return "*" + g.QualifiedGoIdent(msg.GoIdent)
	}
	return g.QualifiedGoIdent(protogen.GoIdent{
		GoName:       msg.GoIdent.GoName + "_Const",
		GoImportPath: msg.GoIdent.GoImportPath,
	})
}

// fieldGoType returns the Go type string used for the given field on the
// concrete *Message, following the same conventions as protoc-gen-go. For
// list/map fields the slice / map wrapper is layered on top of the element
// type returned by scalarFieldGoType.
func (x *Generator) fieldGoType(field *protogen.Field) string {
	g := x.g()
	goType := x.scalarFieldGoType(field)
	if field.Desc.Kind() == protoreflect.MessageKind || field.Desc.Kind() == protoreflect.GroupKind {
		goType = "*" + g.QualifiedGoIdent(field.Message.GoIdent)
	}
	switch {
	case field.Desc.IsList():
		return "[]" + goType
	case field.Desc.IsMap():
		keyType := x.fieldGoType(field.Message.Fields[0])
		valType := x.fieldGoType(field.Message.Fields[1])
		return fmt.Sprintf("map[%s]%s", keyType, valType)
	}
	return goType
}

// scalarFieldGoType returns the Go type string for the field's element kind
// only — list/map modifiers are NOT applied here; fieldGoType wraps them on
// top. The mapping mirrors the one used inside google.golang.org/protobuf
// /compiler/protogen for consistency with the .pb.go sibling file.
func (x *Generator) scalarFieldGoType(field *protogen.Field) string {
	g := x.g()
	switch field.Desc.Kind() {
	case protoreflect.BoolKind:
		return "bool"
	case protoreflect.EnumKind:
		return g.QualifiedGoIdent(field.Enum.GoIdent)
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "int32"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "uint32"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return "int64"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "uint64"
	case protoreflect.FloatKind:
		return "float32"
	case protoreflect.DoubleKind:
		return "float64"
	case protoreflect.StringKind:
		return "string"
	case protoreflect.BytesKind:
		return "[]byte"
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return "*" + g.QualifiedGoIdent(field.Message.GoIdent)
	}
	return ""
}
