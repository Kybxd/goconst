// Temporary equivalence tests: verify that Clone() and ToAny() emitted
// on the *_Const views produce results indistinguishable from calling
// the native proto.CloneOf / anypb.New on the underlying *Message.
//
// These tests are intentionally narrow and disposable — they exist to
// give us confidence in the newly added Clone/ToAny shims and can be
// deleted (or folded into the main test file) once the API is
// considered stable.
//
// Style note: every test function here follows the same table-driven
// shape — a `fields` struct describing the per-case input domain, a
// `tests` slice of cases, and a single `for _, tt := range tests`
// driver that delegates to t.Run(tt.name, ...). New cases are added
// by appending a row to `tests`; the assertion body stays shared.
//
// Scope note: we only care that the wrapper's nil-ness agrees with the
// native call's nil-ness — i.e. (gotView == nil) == (gotNative == nil).
// We do NOT re-test deep-copy independence: the wrapper's Clone() body
// is literally `proto.CloneOf(c.p)`, so independence is a property of
// proto.CloneOf itself, not of the shim.
package nested

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// TestClone_EquivalentToProtoClone verifies that view.Clone() agrees
// with proto.CloneOf on (a) nil-ness and (b) when both are non-nil,
// semantic equality. The wrapper's Clone() is a thin forward to
// proto.CloneOf, so this test pins that contract.
func TestClone_EquivalentToProtoClone(t *testing.T) {
	type fields struct {
		src *Person
	}
	tests := []struct {
		name   string
		fields fields
	}{
		{
			name:   "non-nil_view",
			fields: fields{src: newPerson()},
		},
		{
			name:   "nil_view",
			fields: fields{src: nil},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.fields.src
			view := p.AsConst()

			gotView := view.Clone()
			gotNative := proto.CloneOf(p)

			// Nil-ness must agree with the native call.
			if (gotView == nil) != (gotNative == nil) {
				t.Fatalf("nil-ness mismatch: view.Clone()==nil is %v, proto.CloneOf(p)==nil is %v",
					gotView == nil, gotNative == nil)
			}

			// Both non-nil: must be semantically equal, and must
			// equal the source.
			if !proto.Equal(gotView, gotNative) {
				t.Fatalf("Clone divergence:\n view-clone   = %v\n native-clone = %v", gotView, gotNative)
			}
			if !proto.Equal(gotView, p) {
				t.Fatalf("view.Clone() not equal to source: %v vs %v", gotView, p)
			}
		})
	}
}

// TestToAny_EquivalentToAnypbNew verifies that view.ToAny() agrees
// with anypb.New on (a) nil-ness and (b) when both are non-nil,
// semantic equality of the decoded payload. The wrapper's ToAny()
// is a thin forward to anypb.New, so this test pins that contract.
//
// For the non-nil case we compare via UnmarshalTo + proto.Equal
// rather than byte-level Value: protobuf wire format does not
// guarantee map-field ordering across independent Marshal calls, so
// bytes.Equal would be flaky.
func TestToAny_EquivalentToAnypbNew(t *testing.T) {
	type fields struct {
		src *Person
	}
	tests := []struct {
		name   string
		fields fields
	}{
		{
			name:   "non-nil_view",
			fields: fields{src: newPerson()},
		},
		{
			name:   "nil_view",
			fields: fields{src: nil},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.fields.src
			view := p.AsConst()

			gotView, err := view.ToAny()
			if err != nil {
				t.Fatalf("view.ToAny() error: %v", err)
			}
			gotNative, err := anypb.New(p)
			if err != nil {
				t.Fatalf("anypb.New(p) error: %v", err)
			}

			// Nil-ness must agree with the native call.
			if (gotView == nil) != (gotNative == nil) {
				t.Fatalf("nil-ness mismatch: view.ToAny()==nil is %v, anypb.New(p)==nil is %v",
					gotView == nil, gotNative == nil)
			}

			// TypeUrl must match exactly — it is derived from the
			// descriptor FullName, so no nondeterminism to worry
			// about.
			if gotView.TypeUrl != gotNative.TypeUrl {
				t.Fatalf("TypeUrl mismatch: view=%q native=%q", gotView.TypeUrl, gotNative.TypeUrl)
			}

			// Value-length must agree. For the typed-nil case both
			// sides produce an empty Value; for the non-nil case
			// the lengths must match (the bytes themselves can
			// permute on map fields, see below).
			if len(gotView.Value) != len(gotNative.Value) {
				t.Fatalf("Value length mismatch: view=%d native=%d", len(gotView.Value), len(gotNative.Value))
			}
			if len(gotView.Value) == 0 {
				// Empty payload (typed-nil source): nothing more
				// to compare semantically.
				return
			}

			// Compare via UnmarshalTo, not byte-level Value.
			viewBack := &Person{}
			if err := gotView.UnmarshalTo(viewBack); err != nil {
				t.Fatalf("UnmarshalTo(view-any): %v", err)
			}
			nativeBack := &Person{}
			if err := gotNative.UnmarshalTo(nativeBack); err != nil {
				t.Fatalf("UnmarshalTo(native-any): %v", err)
			}
			if !proto.Equal(viewBack, nativeBack) {
				t.Fatalf("ToAny semantic divergence:\n view-decoded   = %v\n native-decoded = %v", viewBack, nativeBack)
			}
			if !proto.Equal(viewBack, p) {
				t.Fatalf("Any round-trip mismatch:\n got  = %v\n want = %v", viewBack, p)
			}
		})
	}
}
