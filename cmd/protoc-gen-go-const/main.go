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

const version = "0.4.0"

// protoPackage is the import path of the runtime proto package. It is
// referenced by the emitted Clone() method on each Message_Const wrapper
// (via proto.Clone).
const protoPackage = protogen.GoImportPath("google.golang.org/protobuf/proto")

// goconstPackage is the import path of this repo's runtime helper package,
// which exposes the read-only Slice / Slice2 / Map / Map2 struct-wrapper
// views and the NewSlice / NewSlice2 / NewMap / NewMap2 constructors used
// by generated *_Const views for repeated / map fields.
const goconstPackage = protogen.GoImportPath("github.com/Kybxd/goconst")

// ---------------------------------------------------------------------------
// Plugin entry point
// ---------------------------------------------------------------------------
//
// Generation shape (wrapper-struct design):
//
//   - For every message Foo, emit
//     `type Foo_Const struct { goconst.DoNotCompare; p *Foo }` —
//     a concrete struct whose sole payload is an unexported *Foo
//     pointer. The embedded goconst.DoNotCompare is a zero-width
//     [0]func() marker that makes `view == view` a compile error
//     (pointer-equality on wrapper values is meaningless: two views
//     of semantically-equal messages would compare unequal). Every
//     read on the view forwards through `c.p.<getter>()`, so the
//     whole view is nil-safe whenever the underlying protoc-gen-go
//     getter is nil-safe (which is the proto3 default).
//   - For every field, emit a `Get<Name>` method on Foo_Const. Scalar
//     / enum / bytes / excluded-package-message fields forward
//     verbatim to `c.p.Get<Name>()`; singular non-excluded message,
//     repeated, and map fields instead materialise the appropriate
//     AsConst projection or goconst.Slice / Slice2 / Map / Map2
//     read-only collection view. Foo_Const is a distinct Go type from
//     *Foo so there is no method-set collision with the concrete
//     getter on either side.
//   - Emit `func (x *Foo) AsConst() Foo_Const { return Foo_Const{p: x} }`
//     so *Foo satisfies goconst.Constable[Foo_Const]; this is how
//     parent messages project repeated / map fields through
//     goconst.NewSlice2 / NewMap2.
//   - Emit `func (c Foo_Const) IsNil() bool { return c.p == nil }` so
//     callers have a positive nil predicate (both the `view == nil`
//     spelling and the `view == Foo_Const{}` spelling are compile
//     errors — the former because Foo_Const is a struct, the latter
//     because of the embedded goconst.DoNotCompare marker).
//   - Emit `func (c Foo_Const) Clone() *Foo` as the escape hatch out
//     of the read-only world (delegates to proto.Clone with a nil
//     guard).
//   - Emit `func (c Foo_Const) String() string { return fmt.Sprint(c.p) }`
//     so printing the view via fmt / log reuses the underlying proto
//     message's prototext-style String (and safely prints "<nil>" on
//     a nil-backed view).
//
// Design rationale (vs. the previous interface-based shape):
//
//   - Foo_Const is no longer a Go interface, so the classic Go
//     typed-nil footgun — `view == nil` silently evaluating to false
//     for a nil-backed interface view — is gone. `view == nil` is now
//     a compile error; callers use IsNil() instead.
//   - The wrapped *Foo is unexported, so the view can only be read:
//     there is no exported handle the caller can use to reach back
//     into the underlying mutable message.
//   - AsConst() is `return Foo_Const{p: x}` — a single-word struct
//     literal returned by value. With the pointer fitting in a
//     register this is a zero-allocation cast, the same cost profile
//     as the interface-box version it replaces.
//   - Foo_Const no longer satisfies proto.Message. Callers who need to
//     pass the view into proto.Marshal or similar must go through
//     Clone() first to obtain a fresh mutable copy.
func main() {
	var flags pflag.FlagSet
	excludePackages := flags.StringSlice("exclude_packages", nil,
		"Repeatable flag listing Go package import path patterns that should "+
			"NOT receive *_Const wrappers. Each entry is matched against "+
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
// wrapper for the given message (and, when referenced from an enclosing
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
// that make up the wrapper-struct _Const shape:
//
//  1. The Message_Const struct type. It has a single unexported payload
//     field `p *Message` and embeds goconst.DoNotCompare (a zero-width
//     [0]func() marker that makes `view == view` a compile error); no
//     other embedding, so no method on *Message leaks onto the view
//     beyond what this generator explicitly forwards.
//  2. AsConst on *Message, returning Message_Const{p: x}. This is how
//     *Message satisfies goconst.Constable[Message_Const] and how
//     callers enter the read-only world at zero cost.
//  3. Forwarding methods on Message_Const for every field, all named
//     `Get<Name>` to mirror the concrete *Message getter spelling.
//     Scalar / enum / bytes / excluded-package-message fields forward
//     verbatim to `c.p.Get<Name>()`; non-excluded singular message,
//     repeated and map fields instead return the view-native type
//     (Foo_Const / goconst.Slice / Slice2 / Map / Map2). Method names
//     do not collide because Message_Const is a distinct Go type from
//     *Message.
//  4. IsNil, Clone, and String on Message_Const. IsNil reports whether
//     the wrapped pointer is nil; Clone forwards to proto.Clone with a
//     nil guard (so a nil-backed view yields a nil *Message rather
//     than a panic); String prints the underlying *Message via fmt,
//     so the view renders exactly like the raw message would and the
//     nil case prints "<nil>".
//  5. A compile-time witness `var _ goconst.Constable[Message_Const] =
//     (*Message)(nil)` so dropping or renaming AsConst on the concrete
//     pointer surfaces as a build error here rather than as a
//     constraint-not-satisfied diagnostic at a downstream
//     NewSlice2 / NewMap2 call site.
//  6. Recursion into nested (non-map-entry) messages, so that a nested
//     Address or Contact type emits its own _Const API in the same
//     file.
func (x *Generator) genMessageConstAPI(message *protogen.Message) {
	g := x.g()
	msgName := message.GoIdent.GoName

	// --- (1) The _Const struct --------------------------------------------
	//
	// The wrapped pointer is unexported on purpose: cross-package callers
	// cannot reach it, so the only way to read the underlying message is
	// through the forwarding methods emitted below. Consequently the
	// read-only contract is enforced by the Go type system itself, not
	// by convention.
	//
	// goconst.DoNotCompare is a zero-width [0]func() marker embedded so
	// that `a == b` on two Message_Const values is a compile error: two
	// views that wrap different *Message pointers would compare unequal
	// even when the wrapped messages are semantically equal, so pointer-
	// equality on views is never the question callers want to ask — and
	// letting the compiler reject it is cleaner than documenting around
	// it. Callers who really want identity on the underlying pointer
	// can compare *Message values directly.
	g.P("// ", msgName, "_Const is a read-only wrapper view of *", msgName, ".")
	g.P("type ", msgName, "_Const struct {")
	g.P(g.QualifiedGoIdent(goconstPackage.Ident("DoNotCompare")))
	g.P("p *", msgName)
	g.P("}")
	g.P()

	// --- (1b) Per-message collection aliases ------------------------------
	//
	// Emit two Go 1.24 type aliases that bake the storage type E (=*Message)
	// into the generic collection views, so callers see short, intuitive
	// return types on getters rather than the raw three-parameter
	// goconst.Slice2 / Map2 spelling:
	//
	//   type <Msg>_ConstSlice              = goconst.Slice2[<Msg>_Const, *<Msg>]
	//   type <Msg>_ConstMap[K comparable]  = goconst.Map2[K, <Msg>_Const, *<Msg>]
	//
	// These are *pure* aliases — at runtime they are the same types as the
	// RHS, with identical method sets, size, and ABI. The aliases only
	// exist to shorten generated getter signatures like
	// `GetAddressBook() <Msg>_ConstMap[int64]` instead of
	// `GetAddressBook() goconst.Map2[int64, <Msg>_Const, *<Msg>]`.
	//
	// They are always emitted (not guarded by "is this message actually
	// used as a repeated / map element?") so the surface of every _Const
	// view is uniform: callers can form
	// `var s <Msg>_ConstSlice = other.GetXs()` regardless of which parent
	// field they are pulling the collection out of.
	g.P("type ", msgName, "_ConstSlice = ", g.QualifiedGoIdent(goconstPackage.Ident("Slice2")),
		"[", msgName, "_Const, *", msgName, "]")
	g.P("type ", msgName, "_ConstMap[K comparable] = ",
		g.QualifiedGoIdent(goconstPackage.Ident("Map2")),
		"[K, ", msgName, "_Const, *", msgName, "]")
	g.P()

	// --- (2) AsConst: zero-allocation "cast" ------------------------------
	//
	// Returns a Message_Const by value. Because the struct holds a
	// single pointer it fits in a register, so the return path is a
	// zero-allocation cast: no heap traffic, no interface box. This is
	// also what makes *Message satisfy goconst.Constable[Message_Const],
	// which in turn is what the NewSlice2 / NewMap2 constructors rely on
	// to project repeated / map fields element-by-element.
	g.P("// AsConst returns x wrapped as its read-only ", msgName, "_Const view.")
	g.P("func (x *", msgName, ") AsConst() ", msgName, "_Const {")
	g.P("return ", msgName, "_Const{p: x}")
	g.P("}")
	g.P()

	// --- (3) Compile-time Constable witness -------------------------------
	//
	// `(*Message).AsConst() Message_Const` is the method that makes
	// *Message satisfy goconst.Constable[Message_Const]. Asserting the
	// constraint here pins that relationship at the file level, so a
	// typo or rename on AsConst fails the build here instead of
	// downstream at a NewSlice2 / NewMap2 instantiation site whose
	// error would point at the wrong line.
	g.P("var _ ", g.QualifiedGoIdent(goconstPackage.Ident("Constable")), "[",
		msgName, "_Const] = (*", msgName, ")(nil)")
	g.P()

	// --- (4) Forwarding methods for every field ---------------------------
	//
	// All forwarding methods are named Get<Name> to mirror the concrete
	// *Message getter spelling. The split between genPlainGetter and
	// genConstGetter is therefore purely about which body shape gets
	// emitted (verbatim forward vs. AsConst projection / NewSlice /
	// NewMap constructor), not about the method name. There is no
	// method-set collision with the concrete *Message because
	// Message_Const is a distinct Go type.
	for _, field := range message.Fields {
		if x.fieldNeedsViewProjection(field) {
			x.genConstGetter(message, field)
			continue
		}
		x.genPlainGetter(message, field)
	}

	// --- (5) IsNil / Clone / String on Message_Const ----------------------
	//
	// IsNil is the positive nil predicate. Because Message_Const is a
	// concrete struct (not an interface), `view == nil` is a compile
	// error: the type system rules out the classic Go typed-nil footgun
	// at the grammar level, and callers are steered to IsNil() instead.
	g.P("func (c ", msgName, "_Const) IsNil() bool {")
	g.P("return c.p == nil")
	g.P("}")
	g.P()

	// Clone is the escape hatch out of the read-only world. proto.Clone
	// is not documented to be nil-safe and a nil-backed view is a
	// legitimate state (proto3 missing-message fields resolve to one),
	// so guard the call explicitly: a nil-backed view clones to nil
	// rather than panicking.
	g.P("func (c ", msgName, "_Const) Clone() *", msgName, " {")
	g.P("if c.p == nil {")
	g.P("return nil")
	g.P("}")
	g.P("return ", g.QualifiedGoIdent(protoPackage.Ident("Clone")), "(c.p).(*", msgName, ")")
	g.P("}")
	g.P()

	// String forwards to fmt.Sprint on the wrapped pointer. fmt handles
	// a nil *Message by printing "<nil>", which is the friendliest thing
	// to do for log lines, and forwards to the message's own
	// prototext-style String() when the pointer is non-nil. Using
	// fmt.Sprint instead of c.p.String() keeps the nil path
	// defensive — some proto runtimes panic when their String() is
	// invoked on a nil receiver.
	g.P("func (c ", msgName, "_Const) String() string {")
	g.P("return ", g.QualifiedGoIdent(protogen.GoImportPath("fmt").Ident("Sprint")), "(c.p)")
	g.P("}")
	g.P()

	// --- (6) Recurse into nested messages ---------------------------------
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

// fieldNeedsViewProjection reports whether the field's view signature
// differs from its signature on the concrete *Message, and therefore
// whether the forwarding method on Message_Const must materialise a
// view-native return type (AsConst projection or a goconst.Slice /
// Slice2 / Map / Map2 collection wrapper) rather than forward verbatim.
//
// Three kinds of fields qualify:
//
//   - repeated fields: []T → goconst.Slice[T] (or <T>_ConstSlice, the
//     Go 1.24 alias for Slice2[T_Const, *T]);
//   - map fields:      map[K]V → goconst.Map[K, V] (or <V>_ConstMap[K],
//     the Go 1.24 alias for Map2[K, V_Const, *V]);
//   - singular messages from a non-excluded package: *T → T_Const.
//
// Everything else (scalars, enums, bytes, and messages from excluded
// packages) has a view-native signature that is identical to the
// concrete getter, so a verbatim forwarder is emitted instead. The
// method name is Get<Name> in both cases.
func (x *Generator) fieldNeedsViewProjection(field *protogen.Field) bool {
	if field.Desc.IsList() || field.Desc.IsMap() {
		return true
	}
	if field.Desc.Kind() == protoreflect.MessageKind || field.Desc.Kind() == protoreflect.GroupKind {
		// Excluded-package messages have no _Const view, so the view
		// signature is identical to the concrete getter.
		return !x.shouldExcludeMessage(field.Message)
	}
	return false
}

// genPlainGetter emits a `func (c Message_Const) Get<Name>() <T>` method
// that forwards verbatim to the concrete `*Message.Get<Name>()`. Used for
// every field whose view signature matches the concrete getter exactly
// (scalars, enums, bytes, excluded-package singular messages).
//
// The forwarding body relies on protoc-gen-go's nil-receiver-safe
// getters: `(*Message)(nil).Get<Name>()` is defined to return the scalar
// zero value / nil pointer, so a nil-backed Message_Const view still
// reads as the zero value rather than panicking. The guarantee is
// therefore inherited, not re-implemented here.
func (x *Generator) genPlainGetter(message *protogen.Message, field *protogen.Field) {
	g := x.g()
	msgName := message.GoIdent.GoName
	goType := x.fieldGoType(field)
	g.P("func (c ", msgName, "_Const) Get", field.GoName, "() ", goType, " {")
	g.P("return c.p.Get", field.GoName, "()")
	g.P("}")
	g.P()
}

// genConstGetter emits one `func (c Message_Const) Get<Name>() <ret-type>`
// method on the wrapper struct, matching the view-native signature. List
// and map fields delegate to the runtime constructors goconst.NewSlice /
// NewSlice2 / NewMap / NewMap2; singular non-excluded messages recurse
// through their own AsConst().
//
// The caller must only invoke this for fields where
// fieldNeedsViewProjection returned true — other fields get a verbatim
// forwarder from genPlainGetter. A direct call here for an
// excluded-package singular message is guarded defensively so that the
// generator never emits a reference to a non-existent _Const type.
func (x *Generator) genConstGetter(message *protogen.Message, field *protogen.Field) {
	g := x.g()
	msgName := message.GoIdent.GoName
	recv := fmt.Sprintf("(c %s_Const)", msgName)

	switch {
	case field.Desc.IsList():
		// Message elements that are NOT in an excluded package expose a
		// Constable[T_Const] view, so we pick NewSlice2 which projects each
		// element through AsConst(). Everything else (scalars, enums, and
		// message elements from excluded packages) passes through as-is
		// via NewSlice.
		wrapAsConst := x.isMessageElem(field) && !x.shouldExcludeMessage(field.Message)
		retType := x.sliceContainerType(field)

		g.P("func ", recv, " Get", field.GoName, "() ", retType, " {")
		if wrapAsConst {
			// Type arguments are omitted on purpose: Go 1.23+ constraint
			// type inference recovers both E (the slice element type) and
			// T (from the Constable[T] constraint on E) from the argument,
			// so spelling them out triggers the "unnecessary type
			// arguments" diagnostic under gopls / revive.
			g.P("return ", g.QualifiedGoIdent(goconstPackage.Ident("NewSlice2")),
				"(c.p.Get", field.GoName, "())")
		} else {
			g.P("return ", g.QualifiedGoIdent(goconstPackage.Ident("NewSlice")),
				"(c.p.Get", field.GoName, "())")
		}
		g.P("}")
		g.P()

	case field.Desc.IsMap():
		// Map fields in protogen are modeled as synthetic entry messages
		// with two fields ("key" at Fields[0], "value" at Fields[1]); the
		// entry's IsMapEntry() is true and it is excluded from recursion
		// in genMessageConstAPI.
		valField := field.Message.Fields[1]
		wrapAsConst := x.isMessageElem(valField) && !x.shouldExcludeMessage(valField.Message)
		retType := x.mapContainerType(field)

		g.P("func ", recv, " Get", field.GoName, "() ", retType, " {")
		if wrapAsConst {
			// Same type-inference rationale as NewSlice2 above.
			g.P("return ", g.QualifiedGoIdent(goconstPackage.Ident("NewMap2")),
				"(c.p.Get", field.GoName, "())")
		} else {
			g.P("return ", g.QualifiedGoIdent(goconstPackage.Ident("NewMap")),
				"(c.p.Get", field.GoName, "())")
		}
		g.P("}")
		g.P()

	case field.Desc.Kind() == protoreflect.MessageKind || field.Desc.Kind() == protoreflect.GroupKind:
		// Defensive: excluded-package messages should already have been
		// filtered out by fieldNeedsViewProjection. Keep the guard so an
		// accidental direct call here does not emit a reference to a
		// non-existent T_Const type.
		if x.shouldExcludeMessage(field.Message) {
			return
		}
		retType := x.messageConstGoType(field.Message)
		g.P("func ", recv, " Get", field.GoName, "() ", retType, " {")
		// c.p.Get<Name>() is proto3's nil-safe singular getter returning
		// a typed *Address (possibly a typed nil when the field is
		// unset). Routing that result through AsConst() gives an
		// Address_Const whose wrapped pointer may also be nil — that is
		// the defined miss sentinel, and its own scalar getters forward
		// through the nil-safe proto getters so they still yield zero
		// values. Under the struct-wrapper scheme this is an explicit
		// AsConst() hop, not the implicit interface conversion the
		// previous design relied on.
		g.P("return c.p.Get", field.GoName, "().AsConst()")
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

// sliceContainerType returns the goconst.Slice[...] / <Msg>_ConstSlice
// type string for a repeated field. For message elements from a
// non-excluded package the per-message Go 1.24 alias <Msg>_ConstSlice
// (= goconst.Slice2[<Msg>_Const, *<Msg>]) is used so the storage type
// stays hidden from getter signatures. For scalar / enum / bytes
// elements and for excluded-package message elements there is no
// AsConst projection needed, so the single-parameter Slice variant
// is used and E is never exposed in the signature.
func (x *Generator) sliceContainerType(field *protogen.Field) string {
	g := x.g()
	if x.isMessageElem(field) && !x.shouldExcludeMessage(field.Message) {
		return g.QualifiedGoIdent(protogen.GoIdent{
			GoName:       field.Message.GoIdent.GoName + "_ConstSlice",
			GoImportPath: field.Message.GoIdent.GoImportPath,
		})
	}
	return fmt.Sprintf("%s[%s]",
		g.QualifiedGoIdent(goconstPackage.Ident("Slice")), x.fieldElemConstType(field))
}

// mapContainerType returns the goconst.Map[...] / <ValueMsg>_ConstMap[K]
// type string for a map field. When the value is a message from a
// non-excluded package the per-message Go 1.24 generic alias
// <ValueMsg>_ConstMap[K] (= goconst.Map2[K, <ValueMsg>_Const, *<ValueMsg>])
// is used so the storage type stays hidden from getter signatures;
// only the key type has to be written explicitly. Keys are always
// scalar / enum / bytes in proto3 so no projection logic is needed on
// the key side.
func (x *Generator) mapContainerType(field *protogen.Field) string {
	g := x.g()
	keyField := field.Message.Fields[0]
	valField := field.Message.Fields[1]
	keyType := x.fieldGoType(keyField)
	if x.isMessageElem(valField) && !x.shouldExcludeMessage(valField.Message) {
		aliasIdent := g.QualifiedGoIdent(protogen.GoIdent{
			GoName:       valField.Message.GoIdent.GoName + "_ConstMap",
			GoImportPath: valField.Message.GoIdent.GoImportPath,
		})
		return fmt.Sprintf("%s[%s]", aliasIdent, keyType)
	}
	valType := x.fieldElemConstType(valField)
	return fmt.Sprintf("%s[%s, %s]",
		g.QualifiedGoIdent(goconstPackage.Ident("Map")), keyType, valType)
}

// fieldElemConstType returns the Go type string for one element of a
// repeated/map field (the element type for lists, the value type for maps).
// Message elements are projected to their _Const view; scalar / enum /
// bytes elements are returned as-is. Excluded-package messages keep the
// concrete *Type pointer.
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

// messageConstGoType returns the _Const wrapper Go type string for the
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
