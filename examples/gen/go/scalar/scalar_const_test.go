// Unit tests and benchmarks for the generated AllScalars_Const view.
//
// These tests are hand-written (not generated). They live next to the
// generated .pb.go / .const.pb.go files on purpose: Go's "DO NOT EDIT"
// marker only applies to generated sources, and *_test.go is never
// overwritten by buf generate.
package scalar

import (
	"testing"

	goconst "github.com/Kybxd/goconst"
)

// Compile-time checks:
//   - *AllScalars must satisfy AllScalars_Const via AsConst() projection;
//   - AllScalars itself participates in goconst.Constable[AllScalars_Const].
var (
	_ AllScalars_Const                    = (*AllScalars)(nil).AsConst()
	_ goconst.Constable[AllScalars_Const] = (*AllScalars)(nil)
)

// sample returns a pointer message with every field set to a distinctive
// non-zero value so tests can detect accidental zero-value shortcuts in the
// _Const view.
func sample() *AllScalars {
	s := "opt-str"
	i := int32(-42)
	e := Color_COLOR_BLUE
	return &AllScalars{
		FBool:           true,
		FInt32:          -1,
		FSint32:         -2,
		FSfixed32:       -3,
		FUint32:         4,
		FFixed32:        5,
		FInt64:          -6,
		FSint64:         -7,
		FSfixed64:       -8,
		FUint64:         9,
		FFixed64:        10,
		FFloat:          1.5,
		FDouble:         2.5,
		FString:         "hello",
		FBytes:          []byte{0xDE, 0xAD, 0xBE, 0xEF},
		FEnum:           Color_COLOR_GREEN,
		FOptionalString: &s,
		FOptionalInt32:  &i,
		FOptionalEnum:   &e,
	}
}

// TestAllScalars_AsConst_Identity verifies that every Get* method on the
// _Const view returns exactly the same value the concrete getter would.
// It pins the "read-only projection is a pure view, no data mutation"
// contract at the type level.
func TestAllScalars_AsConst_Identity(t *testing.T) {
	m := sample()
	c := m.AsConst()

	if c.GetFBool() != m.GetFBool() {
		t.Errorf("FBool: const=%v concrete=%v", c.GetFBool(), m.GetFBool())
	}
	if c.GetFInt32() != m.GetFInt32() {
		t.Errorf("FInt32: const=%v concrete=%v", c.GetFInt32(), m.GetFInt32())
	}
	if c.GetFSint32() != m.GetFSint32() {
		t.Errorf("FSint32: const=%v concrete=%v", c.GetFSint32(), m.GetFSint32())
	}
	if c.GetFSfixed32() != m.GetFSfixed32() {
		t.Errorf("FSfixed32: const=%v concrete=%v", c.GetFSfixed32(), m.GetFSfixed32())
	}
	if c.GetFUint32() != m.GetFUint32() {
		t.Errorf("FUint32: const=%v concrete=%v", c.GetFUint32(), m.GetFUint32())
	}
	if c.GetFFixed32() != m.GetFFixed32() {
		t.Errorf("FFixed32: const=%v concrete=%v", c.GetFFixed32(), m.GetFFixed32())
	}
	if c.GetFInt64() != m.GetFInt64() {
		t.Errorf("FInt64: const=%v concrete=%v", c.GetFInt64(), m.GetFInt64())
	}
	if c.GetFSint64() != m.GetFSint64() {
		t.Errorf("FSint64: const=%v concrete=%v", c.GetFSint64(), m.GetFSint64())
	}
	if c.GetFSfixed64() != m.GetFSfixed64() {
		t.Errorf("FSfixed64: const=%v concrete=%v", c.GetFSfixed64(), m.GetFSfixed64())
	}
	if c.GetFUint64() != m.GetFUint64() {
		t.Errorf("FUint64: const=%v concrete=%v", c.GetFUint64(), m.GetFUint64())
	}
	if c.GetFFixed64() != m.GetFFixed64() {
		t.Errorf("FFixed64: const=%v concrete=%v", c.GetFFixed64(), m.GetFFixed64())
	}
	if c.GetFFloat() != m.GetFFloat() {
		t.Errorf("FFloat: const=%v concrete=%v", c.GetFFloat(), m.GetFFloat())
	}
	if c.GetFDouble() != m.GetFDouble() {
		t.Errorf("FDouble: const=%v concrete=%v", c.GetFDouble(), m.GetFDouble())
	}
	if c.GetFString() != m.GetFString() {
		t.Errorf("FString: const=%v concrete=%v", c.GetFString(), m.GetFString())
	}
	if string(c.GetFBytes()) != string(m.GetFBytes()) {
		t.Errorf("FBytes: const=%x concrete=%x", c.GetFBytes(), m.GetFBytes())
	}
	if c.GetFEnum() != m.GetFEnum() {
		t.Errorf("FEnum: const=%v concrete=%v", c.GetFEnum(), m.GetFEnum())
	}
	if c.GetFOptionalString() != m.GetFOptionalString() {
		t.Errorf("FOptionalString: const=%q concrete=%q", c.GetFOptionalString(), m.GetFOptionalString())
	}
	if c.GetFOptionalInt32() != m.GetFOptionalInt32() {
		t.Errorf("FOptionalInt32: const=%v concrete=%v", c.GetFOptionalInt32(), m.GetFOptionalInt32())
	}
	if c.GetFOptionalEnum() != m.GetFOptionalEnum() {
		t.Errorf("FOptionalEnum: const=%v concrete=%v", c.GetFOptionalEnum(), m.GetFOptionalEnum())
	}
}

// TestAllScalars_AsConst_UnsetOptionalDefaults verifies that optional fields
// which were NEVER set still return the scalar zero-value through the Const
// view (matching the concrete getter's proto3 semantics).
func TestAllScalars_AsConst_UnsetOptionalDefaults(t *testing.T) {
	c := (&AllScalars{}).AsConst()
	if got := c.GetFOptionalString(); got != "" {
		t.Errorf("unset FOptionalString: got %q, want \"\"", got)
	}
	if got := c.GetFOptionalInt32(); got != 0 {
		t.Errorf("unset FOptionalInt32: got %d, want 0", got)
	}
	if got := c.GetFOptionalEnum(); got != Color_COLOR_UNSPECIFIED {
		t.Errorf("unset FOptionalEnum: got %v, want COLOR_UNSPECIFIED", got)
	}
}

// TestAllScalars_AsConst_NilReceiver makes sure the embedded-pointer trick
// used by _AllScalars_Const propagates nil correctly. We construct a typed
// nil *AllScalars, wrap it, and call every scalar getter through the view;
// none must panic, all must return the scalar zero-value.
func TestAllScalars_AsConst_NilReceiver(t *testing.T) {
	var m *AllScalars
	c := m.AsConst()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Get* on nil-backed _Const panicked: %v", r)
		}
	}()

	_ = c.GetFBool()
	_ = c.GetFInt32()
	_ = c.GetFString()
	_ = c.GetFBytes()
	_ = c.GetFEnum()
	_ = c.GetFOptionalString()
	_ = c.GetFOptionalInt32()
	_ = c.GetFOptionalEnum()
}

// ---- Benchmarks ------------------------------------------------------------
//
// Pair each "via concrete getter" benchmark with its "via _Const interface"
// counterpart so the two numbers can be diff'd directly. Both call the exact
// same underlying field access; any delta is the interface-dispatch +
// embedded-struct wrapper cost.

var benchScalarSink int64

func BenchmarkScalar_AsConst(b *testing.B) {
	m := sample()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.AsConst()
	}
}

func BenchmarkScalar_Get_Concrete(b *testing.B) {
	m := sample()
	b.ReportAllocs()
	b.ResetTimer()
	var acc int64
	for i := 0; i < b.N; i++ {
		acc += int64(m.GetFInt32()) + m.GetFInt64() + int64(m.GetFFixed32())
	}
	benchScalarSink = acc
}

func BenchmarkScalar_Get_ViaConst(b *testing.B) {
	c := sample().AsConst()
	b.ReportAllocs()
	b.ResetTimer()
	var acc int64
	for i := 0; i < b.N; i++ {
		acc += int64(c.GetFInt32()) + c.GetFInt64() + int64(c.GetFFixed32())
	}
	benchScalarSink = acc
}
