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

const version = "0.5.0"

// protoPackage / anypbPackage / goconstPackage are the import paths
// referenced by the emitted methods on every Foo_Const wrapper:
// proto.Clone / proto.Equal back Clone() / Equal(); anypb.New backs
// ToAny(); goconst.{Slice,Slice2,Map,Map2,NewSlice…,NewMap…,DoNotCompare}
// back the read-only collection views and the wrapper struct itself.
const (
	protoPackage   = protogen.GoImportPath("google.golang.org/protobuf/proto")
	anypbPackage   = protogen.GoImportPath("google.golang.org/protobuf/types/known/anypb")
	goconstPackage = protogen.GoImportPath("github.com/Kybxd/goconst")
)

// builtinExcludePackagePatterns are doublestar globs always applied on
// top of user-supplied --exclude_packages. They cover packages whose
// .pb.go is produced upstream and therefore has no _Const / AsConst —
// generating projected references to them would fail to compile.
//
// Currently this is exactly the well-known-types subtree
// (google.golang.org/protobuf/types/known/**), covering timestamppb /
// durationpb / anypb / wrapperspb / structpb / fieldmaskpb / emptypb /
// apipb / sourcecontextpb / typepb (and any future nested
// subpackages). Users do not need to repeat this in --exclude_packages;
// an explicit entry stays harmless for backwards compatibility.
var builtinExcludePackagePatterns = []string{
	"google.golang.org/protobuf/types/known/**",
}

// main wires the plugin into protogen and runs one [Generator] per
// input .proto file with Generate == true. Each generator emits a
// foo.const.pb.go containing, for every message Foo:
//
//   - type Foo_Const struct { _ goconst.DoNotCompare; p *Foo }
//   - type Foo_ConstSlice / Foo_ConstMap[K] aliases for the projecting
//     collection views;
//   - func (*Foo) AsConst() Foo_Const — the zero-allocation entry into
//     the read-only world;
//   - func (Foo_Const) Get<Field>() — one forwarder per field, either
//     verbatim (scalar / enum / bytes / excluded-package message) or
//     materialising a view-native return type ([Slice] / [Slice2] /
//     [Map] / [Map2] / nested *_Const);
//   - func (Foo_Const) IsNil / Clone / Equal / ToAny / String — thin
//     forwards to the corresponding proto / anypb / *Foo operation.
//
// See genMessageConstAPI for the per-message emission, and the README
// for the design rationale (typed-nil footgun avoidance, why `==` is a
// compile error, etc.).
func main() {
	var flags pflag.FlagSet
	excludePackages := flags.StringSlice("exclude_packages", nil,
		"Repeatable doublestar glob patterns matched against each "+
			"field type's owning Go import path. Matched packages keep "+
			"the concrete *Type in enclosing _Const views (no AsConst() "+
			"projection, no _Const wrapper). The well-known-types subtree "+
			"(`google.golang.org/protobuf/types/known/**`) is excluded "+
			"automatically; see the plugin README for full glob syntax "+
			"and rationale.")

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

// Generator carries the per-file state for emitting one
// foo.const.pb.go. Exactly one Generator is constructed per input
// .proto file with Generate == true (see [main]).
type Generator struct {
	gen  *protogen.Plugin // plugin handle, used to create the output file lazily
	file *protogen.File   // input .proto being processed

	// once + genFile lazily create the output file on the first call
	// to g(), so a file whose every message is excluded leaves no
	// empty stub behind.
	once    sync.Once
	genFile *protogen.GeneratedFile

	// excludePackagePatterns is the list of doublestar globs matched
	// against a message's owning Go import path by matchExcludePattern.
	// A wildcard-free pattern degenerates to an exact-match check, so
	// the legacy "list of import paths" usage keeps working.
	excludePackagePatterns []string
}

// NewGenerator returns a Generator bound to one input file, with the
// trimmed user-supplied excludePackages concatenated with
// builtinExcludePackagePatterns into a single doublestar pattern list
// (order does not matter — matchExcludePattern short-circuits on the
// first hit).
func NewGenerator(gen *protogen.Plugin, file *protogen.File, excludePackages []string) *Generator {
	patterns := make([]string, 0, len(excludePackages)+len(builtinExcludePackagePatterns))
	for _, pkg := range excludePackages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		patterns = append(patterns, pkg)
	}
	patterns = append(patterns, builtinExcludePackagePatterns...)
	return &Generator{
		gen:                    gen,
		file:                   file,
		excludePackagePatterns: patterns,
	}
}

// shouldExcludeFile reports whether the input .proto's owning Go
// package matches any --exclude_packages pattern. Equivalent to
// shouldExcludeMessage on every top-level message (they all share
// the same Go import path), and lets [Generate] short-circuit before
// iterating message-by-message.
func (x *Generator) shouldExcludeFile(file *protogen.File) bool {
	return x.matchExcludePattern(string(file.GoImportPath))
}

// shouldExcludeMessage reports whether the plugin must skip the _Const
// wrapper for the given message and keep references to it as the
// concrete *Type. This is the per-reference variant: it keeps working
// for messages reached via field types from other packages (see
// fieldNeedsViewProjection, fieldElemConstType, messageConstGoType).
// Use [shouldExcludeFile] for the top-level "is the whole input file
// excluded?" question.
func (x *Generator) shouldExcludeMessage(message *protogen.Message) bool {
	return x.matchExcludePattern(string(message.GoIdent.GoImportPath))
}

// matchExcludePattern reports whether pkgPath matches any of the
// --exclude_packages doublestar globs. A malformed pattern is treated
// as a non-match (matching the previous exact-match implementation's
// behaviour on a typo'd entry).
func (x *Generator) matchExcludePattern(pkgPath string) bool {
	for _, pattern := range x.excludePackagePatterns {
		if ok, _ := doublestar.Match(pattern, pkgPath); ok {
			return true
		}
	}
	return false
}

// Generate walks every top-level message in the input file and emits
// its _Const API. Nested messages are recursed into by
// genMessageConstAPI rather than iterated here. The whole file is
// short-circuited up front via shouldExcludeFile.
func (x *Generator) Generate() {
	if x.shouldExcludeFile(x.file) {
		return
	}
	for _, message := range x.file.Messages {
		x.genMessageConstAPI(message)
	}
}

// g returns the output file, creating it (and writing its header) on
// first call. sync.Once means a Generator whose only messages are
// excluded never touches the filesystem.
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

// genMessageConstAPI emits the full _Const API for one message:
//
//  1. type Foo_Const struct { _ goconst.DoNotCompare; p *Foo }
//  2. type Foo_ConstSlice / Foo_ConstMap[K] aliases for the projecting
//     [Slice2] / [Map2] views (also acting as compile-time witnesses
//     that *Foo satisfies goconst.Constable[Foo_Const], so a separate
//     `var _ Constable…` line is unnecessary).
//  3. AsConst on *Foo returning Foo_Const{p: x} — the zero-allocation
//     entry into the read-only world.
//  4. Get<Field> forwarders, dispatched to genPlainGetter (verbatim
//     forward) or genConstGetter (view-native return type) by
//     fieldNeedsViewProjection.
//  5. IsNil / Clone / Equal / ToAny / String — thin forwards to the
//     corresponding proto / anypb / *Foo operation.
//  6. Recursion into nested non-map-entry messages.
//
// See main's package-level comment for the design rationale of each
// emitted method.
func (x *Generator) genMessageConstAPI(message *protogen.Message) {
	g := x.g()
	msgName := message.GoIdent.GoName

	// (1) The _Const struct. The wrapped *Foo is unexported so
	// cross-package callers can only read it through the forwarders
	// emitted below. The blank-named goconst.DoNotCompare field is a
	// zero-width [0]func() marker that turns `view == view` into a
	// compile error (and the blank name keeps it unreachable by
	// selector and prevents method promotion).
	g.P("// ", msgName, "_Const is a read-only wrapper view of *", msgName, ".")
	g.P("type ", msgName, "_Const struct {")
	g.P("_ ", g.QualifiedGoIdent(goconstPackage.Ident("DoNotCompare")))
	g.P("p *", msgName)
	g.P("}")
	g.P()

	// (1b) Per-message Go 1.24 collection aliases that bake the
	// storage type *Foo into the projecting collection views, so
	// getter signatures stay short:
	//
	//   <Msg>_ConstSlice             = goconst.Slice2[<Msg>_Const, *<Msg>]
	//   <Msg>_ConstMap[K comparable] = goconst.Map2[K, <Msg>_Const, *<Msg>]
	//
	// Always emitted (regardless of whether this message is actually
	// used as a repeated / map element) so the surface of every
	// _Const view is uniform.
	g.P("type ", msgName, "_ConstSlice = ", g.QualifiedGoIdent(goconstPackage.Ident("Slice2")),
		"[", msgName, "_Const, *", msgName, "]")
	g.P("type ", msgName, "_ConstMap[K comparable] = ",
		g.QualifiedGoIdent(goconstPackage.Ident("Map2")),
		"[K, ", msgName, "_Const, *", msgName, "]")
	g.P()

	// (2) AsConst — zero-allocation cast (single-pointer struct
	// returned in a register), and the witness that *Foo satisfies
	// goconst.Constable[Foo_Const].
	g.P("// AsConst returns x wrapped as its read-only ", msgName, "_Const view.")
	g.P("func (x *", msgName, ") AsConst() ", msgName, "_Const {")
	g.P("return ", msgName, "_Const{p: x}")
	g.P("}")
	g.P()

	// (3) Get<Field> forwarders. genPlainGetter forwards verbatim
	// (scalar / enum / bytes / excluded-package message); genConstGetter
	// materialises a view-native return type (AsConst projection, or a
	// goconst.Slice / Slice2 / Map / Map2 collection wrapper). No
	// method-set collision with the concrete *Foo because Foo_Const
	// is a distinct Go type.
	for _, field := range message.Fields {
		if x.fieldNeedsViewProjection(field) {
			x.genConstGetter(message, field)
			continue
		}
		x.genPlainGetter(message, field)
	}

	// (4) IsNil / Clone / Equal / ToAny / String — thin forwards.
	// IsNil is the positive nil predicate (the `view == nil` and
	// `view == Foo_Const{}` spellings are both compile errors). Clone
	// / Equal / ToAny / String forward verbatim to proto.Clone /
	// proto.Equal / anypb.New / (*Foo).String() so the wrapper
	// behaviour matches each native call byte-for-byte (including
	// nil-tolerance on Equal and "<nil>" on String).
	g.P("func (c ", msgName, "_Const) IsNil() bool {")
	g.P("return c.p == nil")
	g.P("}")
	g.P()

	g.P("func (c ", msgName, "_Const) Clone() *", msgName, " {")
	g.P("return ", g.QualifiedGoIdent(protoPackage.Ident("Clone")), "(c.p).(*", msgName, ")")
	g.P("}")
	g.P()

	// Equal takes another Foo_Const (not a raw *Foo): it makes
	// view-vs-view comparison the canonical, lowest-cost spelling and
	// keeps both sides known-read-only. proto.Equal is itself
	// nil-tolerant, so no extra guard is needed.
	g.P("func (c ", msgName, "_Const) Equal(other ", msgName, "_Const) bool {")
	g.P("return ", g.QualifiedGoIdent(protoPackage.Ident("Equal")), "(c.p, other.p)")
	g.P("}")
	g.P()

	// ToAny is named after the Go "ToX" convention for allocating
	// type conversions (cf. time.Time.UTC, big.Int.String). Not
	// "MarshalAny": "Marshal" is reserved in protobuf-go for
	// "serialise to wire-format []byte", so a method that returns
	// a *Any (a Message, not a byte slice) under that name would be
	// a misleading namesake.
	g.P("func (c ", msgName, "_Const) ToAny() (*", g.QualifiedGoIdent(anypbPackage.Ident("Any")), ", error) {")
	g.P("return ", g.QualifiedGoIdent(anypbPackage.Ident("New")), "(c.p)")
	g.P("}")
	g.P()

	// String forwards verbatim to (*Foo).String(), which is itself
	// nil-safe (returns "<nil>" via protoimpl.X.MessageStringOf). The
	// contract is exact equivalence to the raw message — any wrapper
	// (fmt.Sprint, a hand-rolled nil branch, …) would be a silent
	// opportunity to diverge.
	g.P("func (c ", msgName, "_Const) String() string {")
	g.P("return c.p.String()")
	g.P("}")
	g.P()

	// (5) Recurse into nested messages, skipping synthetic
	// map-entry messages (proto3 plumbing for map<K, V>; never
	// referenced by user code).
	for _, nested := range message.Messages {
		if nested.Desc.IsMapEntry() {
			continue
		}
		x.genMessageConstAPI(nested)
	}
}

// fieldNeedsViewProjection reports whether the field's view signature
// differs from its concrete *Foo getter (and therefore needs a
// view-native return type rather than a verbatim forwarder). Three
// kinds qualify:
//
//   - repeated fields:     []T → goconst.Slice[T] (or <T>_ConstSlice);
//   - map fields:          map[K]V → goconst.Map[K, V] (or <V>_ConstMap[K]);
//   - singular non-excluded message fields: *T → T_Const.
//
// Everything else (scalars, enums, bytes, excluded-package messages)
// has a view-native signature identical to the concrete getter and
// gets a verbatim forwarder.
func (x *Generator) fieldNeedsViewProjection(field *protogen.Field) bool {
	if field.Desc.IsList() || field.Desc.IsMap() {
		return true
	}
	if field.Desc.Kind() == protoreflect.MessageKind || field.Desc.Kind() == protoreflect.GroupKind {
		// Excluded-package messages have no _Const view, so the
		// view signature is identical to the concrete getter.
		return !x.shouldExcludeMessage(field.Message)
	}
	return false
}

// genPlainGetter emits `func (c Foo_Const) Get<Name>() <T>` that
// forwards verbatim to *Foo.Get<Name>(). Used for fields whose view
// signature matches the concrete getter exactly (scalars, enums,
// bytes, excluded-package singular messages).
//
// Nil-safety is inherited: protoc-gen-go's getters are themselves
// nil-receiver-safe, so a nil-backed Foo_Const view still reads as
// the zero value rather than panicking.
func (x *Generator) genPlainGetter(message *protogen.Message, field *protogen.Field) {
	g := x.g()
	msgName := message.GoIdent.GoName
	goType := x.fieldGoType(field)
	g.P("func (c ", msgName, "_Const) Get", field.GoName, "() ", goType, " {")
	g.P("return c.p.Get", field.GoName, "()")
	g.P("}")
	g.P()
}

// genConstGetter emits one Get<Name> forwarder with a view-native
// return type. List / map fields delegate to goconst.NewSlice /
// NewSlice2 / NewMap / NewMap2; singular non-excluded messages
// recurse through their own AsConst().
//
// Caller must only invoke this for fields where
// fieldNeedsViewProjection returned true. The singular-message branch
// guards excluded packages defensively so an accidental direct call
// never emits a reference to a non-existent _Const type.
func (x *Generator) genConstGetter(message *protogen.Message, field *protogen.Field) {
	g := x.g()
	msgName := message.GoIdent.GoName
	recv := fmt.Sprintf("(c %s_Const)", msgName)

	switch {
	case field.Desc.IsList():
		// Non-excluded message elements expose a Constable[T_Const]
		// view → NewSlice2 (projects each element through AsConst).
		// Everything else (scalars / enums / excluded-package
		// messages) passes through as-is via NewSlice.
		wrapAsConst := x.isMessageElem(field) && !x.shouldExcludeMessage(field.Message)
		retType := x.sliceContainerType(field)

		g.P("func ", recv, " Get", field.GoName, "() ", retType, " {")
		if wrapAsConst {
			// Type arguments omitted on purpose: Go 1.23+ constraint
			// type inference recovers them from the argument, and
			// spelling them out triggers the "unnecessary type
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
		// protogen models map fields as a synthetic entry message
		// with Fields[0] = key and Fields[1] = value; the entry's
		// IsMapEntry() is true and is excluded from recursion in
		// genMessageConstAPI.
		valField := field.Message.Fields[1]
		wrapAsConst := x.isMessageElem(valField) && !x.shouldExcludeMessage(valField.Message)
		retType := x.mapContainerType(field)

		g.P("func ", recv, " Get", field.GoName, "() ", retType, " {")
		if wrapAsConst {
			g.P("return ", g.QualifiedGoIdent(goconstPackage.Ident("NewMap2")),
				"(c.p.Get", field.GoName, "())")
		} else {
			g.P("return ", g.QualifiedGoIdent(goconstPackage.Ident("NewMap")),
				"(c.p.Get", field.GoName, "())")
		}
		g.P("}")
		g.P()

	case field.Desc.Kind() == protoreflect.MessageKind || field.Desc.Kind() == protoreflect.GroupKind:
		// Defensive: excluded-package messages should already have
		// been filtered out by fieldNeedsViewProjection.
		if x.shouldExcludeMessage(field.Message) {
			return
		}
		retType := x.messageConstGoType(field.Message)
		g.P("func ", recv, " Get", field.GoName, "() ", retType, " {")
		// c.p.Get<Name>() is proto3's nil-safe singular getter; a
		// nil receiver gives a typed-nil *Address whose AsConst()
		// in turn produces an Address_Const{p: nil}. That nil-backed
		// view's own scalar getters stay nil-safe by forwarding
		// through the same protoc-gen-go nil-safe getters.
		g.P("return c.p.Get", field.GoName, "().AsConst()")
		g.P("}")
		g.P()
	}
}

// isMessageElem reports whether a repeated/map field's element type
// is a protobuf message (vs. scalar / enum / bytes). Used to decide
// between NewSlice / NewSlice2 (and NewMap / NewMap2).
func (x *Generator) isMessageElem(field *protogen.Field) bool {
	return field.Desc.Kind() == protoreflect.MessageKind || field.Desc.Kind() == protoreflect.GroupKind
}

// ---------------------------------------------------------------------------
// Type-string helpers
// ---------------------------------------------------------------------------

// sliceContainerType returns the goconst.Slice[…] / <Msg>_ConstSlice
// type string for a repeated field. Non-excluded message elements use
// the per-message Go 1.24 alias <Msg>_ConstSlice; everything else uses
// the single-parameter goconst.Slice[…].
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

// mapContainerType returns the goconst.Map[…] / <ValueMsg>_ConstMap[K]
// type string for a map field. Non-excluded message values use the
// per-message Go 1.24 generic alias <ValueMsg>_ConstMap[K]; everything
// else uses goconst.Map[K, V]. Keys are always scalar / enum / bytes
// in proto3, so no projection logic is needed on the key side.
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
// repeated / map field (the value type for maps). Non-excluded message
// elements project to their _Const view; excluded ones keep *Type;
// scalar / enum / bytes elements are returned as-is.
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

// messageConstGoType returns the _Const Go type string for the given
// message, routed through QualifiedGoIdent so cross-package references
// register the correct import. Excluded packages fall back to *Type.
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

// fieldGoType returns the Go type string used for the field on the
// concrete *Foo, following protoc-gen-go's own conventions. List / map
// modifiers are layered on top of the element type returned by
// scalarFieldGoType.
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

// scalarFieldGoType returns the Go type string for the field's
// element kind only — list / map modifiers are NOT applied here
// (fieldGoType wraps them on top). The mapping mirrors the one used
// inside protogen for consistency with the .pb.go sibling file.
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
