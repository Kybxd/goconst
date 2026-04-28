// Unit tests for the generated Envelope_Const view. The primary goal here
// is to pin the --exclude_packages contract at the Go type level:
//
//   - testdata.external  is excluded:  getters keep *external.External
//   - google.protobuf.*  is excluded:  getters keep *timestamppb.Timestamp
//   - testdata.nested    is NOT excluded: getters return Address_Const
//
// The static "var _ T = ..." assertions inside each test enforce these
// return types at compile time; the runtime body additionally verifies
// that the same backing pointer is handed through (no copy, no wrapper
// for excluded packages).
package importer

import (
	"testing"
	"time"

	goconst "github.com/Kybxd/goconst"
	external "github.com/Kybxd/goconst/examples/gen/go/external"
	nested "github.com/Kybxd/goconst/examples/gen/go/nested"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	_ Envelope_Const                    = (*Envelope)(nil).AsConst()
	_ goconst.Constable[Envelope_Const] = (*Envelope)(nil)
)

func newEnvelope() (*Envelope, *external.External, *timestamppb.Timestamp) {
	ext := &external.External{Token: "tok", Ts: 1}
	ts := timestamppb.New(time.Unix(1700000000, 0))
	env := &Envelope{
		Id:        "env-1",
		Addr:      &nested.Address{Street: "S", City: "C", Zip: "Z"},
		Ext:       ext,
		Extras:    []*external.External{ext, {Token: "other", Ts: 2}},
		ExtMap:    map[string]*external.External{"a": ext},
		CreatedAt: ts,
		History:   []*timestamppb.Timestamp{ts},
		TsMap:     map[string]*timestamppb.Timestamp{"t": ts},
	}
	return env, ext, ts
}

// TestEnvelope_NonExcluded_ReturnsConst: `addr` references nested.Address,
// which is NOT in --exclude_packages, so ConstAddr() must hand back a
// nested.Address_Const projection (not a concrete *nested.Address).
func TestEnvelope_NonExcluded_ReturnsConst(t *testing.T) {
	env, _, _ := newEnvelope()
	c := env.AsConst()

	got := c.ConstAddr()
	var _ nested.Address_Const = got // compile-time contract

	if got.GetCity() != "C" {
		t.Errorf("Addr.City: got %q want C", got.GetCity())
	}
}

// TestEnvelope_Excluded_SingularKeepsConcrete: `ext` references
// testdata.external.External, listed in --exclude_packages, so the Const
// view must return the concrete *external.External pointer unchanged.
func TestEnvelope_Excluded_SingularKeepsConcrete(t *testing.T) {
	env, ext, _ := newEnvelope()
	c := env.AsConst()

	got := c.GetExt()
	var _ *external.External = got // compile-time contract: no _Const wrapping

	if got != ext {
		t.Errorf("GetExt must return the same backing pointer (got=%p want=%p)", got, ext)
	}
}

// TestEnvelope_Excluded_RepeatedKeepsConcrete: NewSlice (not NewSlice2) is
// used, so Slice[*external.External].At returns the concrete pointer.
func TestEnvelope_Excluded_RepeatedKeepsConcrete(t *testing.T) {
	env, ext, _ := newEnvelope()
	c := env.AsConst()

	s := c.ConstExtras()
	var _ goconst.Slice[*external.External] = s // compile-time contract

	if s.Len() != 2 {
		t.Fatalf("Extras.Len: got %d want 2", s.Len())
	}
	if s.At(0) != ext {
		t.Errorf("Extras[0] must be the same pointer: got=%p want=%p", s.At(0), ext)
	}
	if s.At(1).Token != "other" {
		t.Errorf("Extras[1].Token: got %q want other", s.At(1).Token)
	}
}

// TestEnvelope_Excluded_MapKeepsConcrete: NewMap (not NewMap2) is used, so
// Map[string, *external.External].Get returns the concrete pointer.
func TestEnvelope_Excluded_MapKeepsConcrete(t *testing.T) {
	env, ext, _ := newEnvelope()
	c := env.AsConst()

	m := c.ConstExtMap()
	var _ goconst.Map[string, *external.External] = m // compile-time contract

	v, ok := m.Get("a")
	if !ok || v != ext {
		t.Errorf("ExtMap[a]: got (%p,%v) want (%p,true)", v, ok, ext)
	}
	if _, ok := m.Get("missing"); ok {
		t.Error("ExtMap[missing] ok=true, want false")
	}
}

// TestEnvelope_WKT_TimestampExcluded pins the same contract for WKTs:
// google.protobuf.Timestamp has no _Const of its own, so it must be
// present in --exclude_packages and its getters must therefore keep the
// concrete *timestamppb.Timestamp type in singular / repeated / map
// positions.
func TestEnvelope_WKT_TimestampExcluded(t *testing.T) {
	env, _, ts := newEnvelope()
	c := env.AsConst()

	// singular
	var singular *timestamppb.Timestamp = c.GetCreatedAt()
	if singular != ts {
		t.Errorf("CreatedAt must be the same pointer: got=%p want=%p", singular, ts)
	}

	// repeated
	hs := c.ConstHistory()
	var _ goconst.Slice[*timestamppb.Timestamp] = hs
	if hs.Len() != 1 || hs.At(0) != ts {
		t.Errorf("History[0] must be the same pointer: got=%p want=%p", hs.At(0), ts)
	}

	// map
	mp := c.ConstTsMap()
	var _ goconst.Map[string, *timestamppb.Timestamp] = mp
	v, ok := mp.Get("t")
	if !ok || v != ts {
		t.Errorf("TsMap[t]: got (%p,%v) want (%p,true)", v, ok, ts)
	}
}
